package server

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/image/draw"

	"github.com/odvcencio/gosx"
)

const defaultImageEndpoint = "/_gosx/image"

// ImageTransform describes an optimized image variant.
type ImageTransform struct {
	Width   int
	Height  int
	Quality int
	Format  string
}

// ImageProps configures the server.Image helper.
type ImageProps struct {
	Src           string
	Alt           string
	Width         int
	Height        int
	Widths        []int
	Sizes         string
	Loading       string
	Decoding      string
	FetchPriority string
	Quality       int
	Format        string
	Resolver      string
}

// ImageURL builds an optimizer URL for a local public image source.
func ImageURL(src string, transform ImageTransform) string {
	return ImageURLWithResolver("local", src, transform)
}

// Image renders an optimized image tag for local public assets and falls back
// to a plain <img> for unsupported sources such as remote URLs or SVGs.
func Image(props ImageProps, args ...any) gosx.Node {
	props.Src = AssetURL(props.Src)
	src := props.Src
	widths := normalizeResponsiveWidths(props.Widths)
	shouldOptimize := shouldOptimizeImageSource(src) || strings.TrimSpace(props.Resolver) != ""

	if shouldOptimize {
		switch {
		case len(widths) > 0:
			src = ImageURLWithResolver(props.Resolver, src, ImageTransform{
				Width:   widths[len(widths)-1],
				Height:  props.Height,
				Quality: props.Quality,
				Format:  props.Format,
			})
		case props.Width > 0 || props.Height > 0 || props.Quality > 0 || strings.TrimSpace(props.Format) != "":
			src = ImageURLWithResolver(props.Resolver, src, ImageTransform{
				Width:   props.Width,
				Height:  props.Height,
				Quality: props.Quality,
				Format:  props.Format,
			})
		}
	}

	baseAttrs := []any{
		gosx.Attrs(
			gosx.Attr("src", src),
			gosx.Attr("alt", props.Alt),
		),
	}

	if len(widths) > 0 && shouldOptimize {
		srcset := make([]string, 0, len(widths))
		for _, width := range widths {
			srcset = append(srcset, fmt.Sprintf("%s %dw", ImageURLWithResolver(props.Resolver, props.Src, ImageTransform{
				Width:   width,
				Height:  props.Height,
				Quality: props.Quality,
				Format:  props.Format,
			}), width))
		}
		baseAttrs = append(baseAttrs, gosx.Attrs(gosx.Attr("srcset", strings.Join(srcset, ", "))))
		if props.Sizes != "" {
			baseAttrs = append(baseAttrs, gosx.Attrs(gosx.Attr("sizes", props.Sizes)))
		}
	}

	if props.Width > 0 {
		baseAttrs = append(baseAttrs, gosx.Attrs(gosx.Attr("width", props.Width)))
	}
	if props.Height > 0 {
		baseAttrs = append(baseAttrs, gosx.Attrs(gosx.Attr("height", props.Height)))
	}
	if loading := strings.TrimSpace(props.Loading); loading != "" {
		baseAttrs = append(baseAttrs, gosx.Attrs(gosx.Attr("loading", loading)))
	} else {
		baseAttrs = append(baseAttrs, gosx.Attrs(gosx.Attr("loading", "lazy")))
	}
	if decoding := strings.TrimSpace(props.Decoding); decoding != "" {
		baseAttrs = append(baseAttrs, gosx.Attrs(gosx.Attr("decoding", decoding)))
	} else {
		baseAttrs = append(baseAttrs, gosx.Attrs(gosx.Attr("decoding", "async")))
	}
	if priority := strings.TrimSpace(props.FetchPriority); priority != "" {
		baseAttrs = append(baseAttrs, gosx.Attrs(gosx.Attr("fetchpriority", priority)))
	}

	baseAttrs = append(baseAttrs, args...)
	return gosx.El("img", baseAttrs...)
}

// ImageHandler serves optimized local images from a source directory.
func ImageHandler(rootDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		req, err := parseImageRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		path, err := resolveImagePath(rootDir, req.Src)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		variant, err := renderImageVariant(path, req)
		if err != nil {
			status := http.StatusInternalServerError
			if os.IsNotExist(err) {
				status = http.StatusNotFound
			} else if isImageClientError(err) {
				status = http.StatusBadRequest
			}
			http.Error(w, err.Error(), status)
			return
		}

		w.Header().Set("Content-Type", variant.contentType)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(variant.data)
	})
}

type imageRequest struct {
	Src     string
	Width   int
	Height  int
	Quality int
	Format  string
}

type imageVariant struct {
	data        []byte
	contentType string
}

func parseImageRequest(r *http.Request) (imageRequest, error) {
	query := r.URL.Query()
	src := strings.TrimSpace(query.Get("src"))
	if src == "" {
		return imageRequest{}, fmt.Errorf("missing src")
	}

	width, err := parseOptionalPositiveInt(query.Get("w"))
	if err != nil {
		return imageRequest{}, fmt.Errorf("invalid width: %w", err)
	}
	height, err := parseOptionalPositiveInt(query.Get("h"))
	if err != nil {
		return imageRequest{}, fmt.Errorf("invalid height: %w", err)
	}
	quality, err := parseOptionalPositiveInt(query.Get("q"))
	if err != nil {
		return imageRequest{}, fmt.Errorf("invalid quality: %w", err)
	}

	return imageRequest{
		Src:     src,
		Width:   width,
		Height:  height,
		Quality: quality,
		Format:  normalizeImageFormat(query.Get("fmt")),
	}, nil
}

func renderImageVariant(filePath string, req imageRequest) (imageVariant, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return imageVariant{}, err
	}
	defer file.Close()

	srcImage, sourceFormat, err := image.Decode(file)
	if err != nil {
		return imageVariant{}, fmt.Errorf("decode image: %w", err)
	}

	targetFormat, err := selectTargetImageFormat(sourceFormat, req.Format)
	if err != nil {
		return imageVariant{}, err
	}

	bounds := srcImage.Bounds()
	targetWidth, targetHeight := targetImageSize(bounds.Dx(), bounds.Dy(), req.Width, req.Height)
	if targetWidth != bounds.Dx() || targetHeight != bounds.Dy() {
		dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
		draw.CatmullRom.Scale(dst, dst.Bounds(), srcImage, bounds, draw.Over, nil)
		srcImage = dst
	}

	var buf bytes.Buffer
	contentType, err := encodeImageVariant(&buf, srcImage, targetFormat, req.Quality)
	if err != nil {
		return imageVariant{}, err
	}
	return imageVariant{
		data:        buf.Bytes(),
		contentType: contentType,
	}, nil
}

func encodeImageVariant(buf *bytes.Buffer, img image.Image, format string, quality int) (string, error) {
	switch normalizeImageFormat(format) {
	case "jpeg":
		if quality == 0 {
			quality = 82
		}
		if quality < 1 {
			quality = 1
		}
		if quality > 100 {
			quality = 100
		}
		if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: quality}); err != nil {
			return "", fmt.Errorf("encode jpeg: %w", err)
		}
		return "image/jpeg", nil
	case "gif":
		if err := gif.Encode(buf, img, nil); err != nil {
			return "", fmt.Errorf("encode gif: %w", err)
		}
		return "image/gif", nil
	case "png":
		if err := png.Encode(buf, img); err != nil {
			return "", fmt.Errorf("encode png: %w", err)
		}
		return "image/png", nil
	default:
		return "", fmt.Errorf("unsupported image format %q", format)
	}
}

func targetImageSize(sourceWidth, sourceHeight, requestedWidth, requestedHeight int) (int, int) {
	if sourceWidth <= 0 || sourceHeight <= 0 {
		return 1, 1
	}

	switch {
	case requestedWidth > 0 && requestedHeight > 0:
		return requestedWidth, requestedHeight
	case requestedWidth > 0:
		if requestedWidth > sourceWidth {
			requestedWidth = sourceWidth
		}
		return requestedWidth, max(1, int(float64(sourceHeight)*(float64(requestedWidth)/float64(sourceWidth))))
	case requestedHeight > 0:
		if requestedHeight > sourceHeight {
			requestedHeight = sourceHeight
		}
		return max(1, int(float64(sourceWidth)*(float64(requestedHeight)/float64(sourceHeight)))), requestedHeight
	default:
		return sourceWidth, sourceHeight
	}
}

func resolveImagePath(rootDir, src string) (string, error) {
	if strings.TrimSpace(rootDir) == "" {
		return "", fmt.Errorf("image source directory is not configured")
	}
	if !strings.HasPrefix(src, "/") || strings.HasPrefix(src, "//") {
		return "", fmt.Errorf("image src must be a root-relative path")
	}

	parsed, err := neturl.Parse(src)
	if err != nil {
		return "", fmt.Errorf("invalid image src: %w", err)
	}
	if parsed.Scheme != "" || parsed.Host != "" {
		return "", fmt.Errorf("image src must be local")
	}
	if strings.Contains(parsed.Path, "..") {
		return "", fmt.Errorf("image src escapes source directory")
	}

	cleanPath := path.Clean("/" + parsed.Path)
	if cleanPath == "/" {
		return "", fmt.Errorf("image src must reference a file")
	}

	rootAbs, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("resolve image directory: %w", err)
	}
	filePath := filepath.Join(rootAbs, filepath.FromSlash(strings.TrimPrefix(cleanPath, "/")))
	fileAbs, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("resolve image file: %w", err)
	}

	if fileAbs != rootAbs && !strings.HasPrefix(fileAbs, rootAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("image src escapes source directory")
	}
	return fileAbs, nil
}

func shouldOptimizeImageSource(src string) bool {
	src = strings.TrimSpace(src)
	if src == "" || strings.HasPrefix(src, "data:") || strings.HasPrefix(src, "//") || !strings.HasPrefix(src, "/") {
		return false
	}

	parsed, err := neturl.Parse(src)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" {
		return false
	}

	switch ext := strings.ToLower(path.Ext(parsed.Path)); ext {
	case ".png", ".jpg", ".jpeg", ".gif":
		return true
	default:
		return false
	}
}

func normalizeResponsiveWidths(widths []int) []int {
	if len(widths) == 0 {
		return nil
	}

	out := make([]int, 0, len(widths))
	seen := make(map[int]bool, len(widths))
	for _, width := range widths {
		if width <= 0 || seen[width] {
			continue
		}
		seen[width] = true
		out = append(out, width)
	}
	sort.Ints(out)
	return out
}

func normalizeImageFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "jpg", "jpeg":
		if strings.EqualFold(strings.TrimSpace(format), "") {
			return ""
		}
		return "jpeg"
	case "png":
		return "png"
	case "gif":
		return "gif"
	default:
		return strings.ToLower(strings.TrimSpace(format))
	}
}

func selectTargetImageFormat(sourceFormat, requestedFormat string) (string, error) {
	if requestedFormat != "" {
		format := normalizeImageFormat(requestedFormat)
		if !slices.Contains([]string{"jpeg", "png", "gif"}, format) {
			return "", fmt.Errorf("unsupported image format %q", requestedFormat)
		}
		return format, nil
	}

	switch normalizeImageFormat(sourceFormat) {
	case "jpeg":
		return "jpeg", nil
	case "png":
		return "png", nil
	case "gif":
		return "png", nil
	default:
		return "", fmt.Errorf("unsupported source image format %q", sourceFormat)
	}
}

func parseOptionalPositiveInt(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("must be positive")
	}
	return parsed, nil
}

func isImageClientError(err error) bool {
	if err == nil {
		return false
	}
	for _, snippet := range []string{
		"missing src",
		"invalid image src",
		"must be local",
		"must be a root-relative path",
		"must reference a file",
		"escapes source directory",
		"unsupported",
	} {
		if strings.Contains(err.Error(), snippet) {
			return true
		}
	}
	return false
}
