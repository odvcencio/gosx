package server

import (
	"bytes"
	"errors"
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
	_ "golang.org/x/image/webp" // register the WebP decoder (gosx#199)

	"m31labs.dev/gosx"
	tqwebp "m31labs.dev/tqwebp"
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
	Responsive    bool
	Sizes         string
	Loading       string
	Decoding      string
	FetchPriority string
	Priority      bool
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
	// Fail closed at render time: a format the handler cannot produce would
	// otherwise ship as a fmt= URL that 400s only when a browser requests it
	// (gosx#199). Check before any URL is built.
	if err := ValidateProducibleImageFormat(props.Format); err != nil {
		panic(err)
	}
	props.Src = AssetURL(props.Src)
	src := props.Src
	widths := normalizeResponsiveWidths(props.Widths)
	if props.Responsive && len(widths) == 0 {
		widths = AutoImageWidths(props.Width)
	}
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
			// Ladder entries carry width only. Propagating props.Height here
			// would request that exact height at every narrower width too,
			// and targetImageSize would honor both dimensions literally
			// instead of deriving height proportionally — distorting every
			// entry narrower than the full box (gosx#199). The width/height
			// attributes on the emitted <img> still carry the full box for
			// layout; only the candidate URLs stay width-only so each one
			// preserves the source aspect ratio.
			srcset = append(srcset, fmt.Sprintf("%s %dw", ImageURLWithResolver(props.Resolver, props.Src, ImageTransform{
				Width:   width,
				Quality: props.Quality,
				Format:  props.Format,
			}), width))
		}
		baseAttrs = append(baseAttrs, gosx.Attrs(gosx.Attr("srcset", strings.Join(srcset, ", "))))
		sizes := strings.TrimSpace(props.Sizes)
		if sizes == "" && props.Responsive {
			sizes = "100vw"
		}
		if sizes != "" {
			baseAttrs = append(baseAttrs, gosx.Attrs(gosx.Attr("sizes", sizes)))
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
	} else if props.Priority {
		baseAttrs = append(baseAttrs, gosx.Attrs(gosx.Attr("loading", "eager")))
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
	} else if props.Priority {
		baseAttrs = append(baseAttrs, gosx.Attrs(gosx.Attr("fetchpriority", "high")))
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
		if r.Method == http.MethodHead {
			contentType, err := imageVariantContentType(path, req.Format)
			if err != nil {
				respondImageOptimizerError(w, path, req, err)
				return
			}
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.WriteHeader(http.StatusOK)
			return
		}

		release, ok := acquireImageTransform()
		if !ok {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "image optimizer is busy", http.StatusServiceUnavailable)
			return
		}
		defer release()

		variant, err := renderImageVariant(path, req)
		if err != nil {
			respondImageOptimizerError(w, path, req, err)
			return
		}

		w.Header().Set("Content-Type", variant.contentType)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.WriteHeader(http.StatusOK)
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

const (
	maxImageDimension          = 4096
	maxImagePixels             = 8 * 1024 * 1024
	maxImageOutputBytes        = 32 * 1024 * 1024
	maxConcurrentImageVariants = 2
)

var imageTransformSlots = make(chan struct{}, maxConcurrentImageVariants)

func acquireImageTransform() (func(), bool) {
	select {
	case imageTransformSlots <- struct{}{}:
		return func() { <-imageTransformSlots }, true
	default:
		return nil, false
	}
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
	if err := validateImageDimensions(width, height); err != nil {
		return imageRequest{}, err
	}

	return imageRequest{
		Src:     src,
		Width:   width,
		Height:  height,
		Quality: quality,
		Format:  normalizeImageFormat(query.Get("fmt")),
	}, nil
}

func validateImageDimensions(width, height int) error {
	if width > maxImageDimension || height > maxImageDimension {
		return fmt.Errorf("image dimensions exceed %d pixels", maxImageDimension)
	}
	if width > 0 && height > 0 && int64(width)*int64(height) > int64(maxImagePixels) {
		return fmt.Errorf("image dimensions exceed %d total pixels", maxImagePixels)
	}
	return nil
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
	if err := validateImageDimensions(targetWidth, targetHeight); err != nil {
		return imageVariant{}, err
	}
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
	case "webp":
		if quality < 0 {
			quality = 0
		} else if quality > 100 {
			quality = 100
		}
		err := tqwebp.EncodeWithLimits(buf, img, &tqwebp.Options{Quality: quality}, tqwebp.Limits{
			MaxWidth:       maxImageDimension,
			MaxHeight:      maxImageDimension,
			MaxPixels:      maxImagePixels,
			MaxOutputBytes: maxImageOutputBytes,
		})
		if err != nil {
			return "", fmt.Errorf("encode webp: %w", err)
		}
		return "image/webp", nil
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
		return min(requestedWidth, sourceWidth), min(requestedHeight, sourceHeight)
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

func imageVariantContentType(filePath, requestedFormat string) (string, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("image source is not a regular file")
	}
	sourceFormat := strings.TrimPrefix(strings.ToLower(filepath.Ext(filePath)), ".")
	format, err := selectTargetImageFormat(sourceFormat, requestedFormat)
	if err != nil {
		return "", err
	}
	switch format {
	case "jpeg":
		return "image/jpeg", nil
	case "png":
		return "image/png", nil
	case "gif":
		return "image/gif", nil
	case "webp":
		return "image/webp", nil
	default:
		return "", fmt.Errorf("unsupported image format %q", format)
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
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
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

// AutoImageWidths returns a deterministic responsive width ladder up to maxWidth.
// A maxWidth of zero returns the full default ladder.
func AutoImageWidths(maxWidth int) []int {
	candidates := []int{320, 480, 640, 750, 828, 1080, 1200, 1920, 2048, 3840}
	if maxWidth <= 0 {
		return append([]int(nil), candidates...)
	}
	widths := make([]int, 0, len(candidates)+1)
	for _, width := range candidates {
		if width <= maxWidth {
			widths = append(widths, width)
		}
	}
	if len(widths) == 0 || widths[len(widths)-1] != maxWidth {
		widths = append(widths, maxWidth)
	}
	return normalizeResponsiveWidths(widths)
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
	case "webp":
		return "webp"
	default:
		return strings.ToLower(strings.TrimSpace(format))
	}
}

// producibleImageFormats lists the output formats the optimizer handler can
// encode. WebP uses the pure-Go tqwebp encoder.
var producibleImageFormats = []string{"jpeg", "png", "gif", "webp"}

// ValidateProducibleImageFormat rejects an Image format prop the optimizer
// handler could never encode. An empty format defers to the source format
// and always passes.
//
// Exported (gosx#201) so strictcheck's check-time <Image> contract can reject
// the same unproducible format value before a render ever happens, using the
// exact same allowlist and message this render-time check already uses —
// one source of truth for both call sites, never two allowlists that could
// drift apart.
func ValidateProducibleImageFormat(format string) error {
	format = strings.TrimSpace(format)
	if format == "" {
		return nil
	}
	normalized := normalizeImageFormat(format)
	if !slices.Contains(producibleImageFormats, normalized) {
		return fmt.Errorf("gosx: Image format %q is not a producible output format (want jpeg, png, gif, or webp)", format)
	}
	return nil
}

func selectTargetImageFormat(sourceFormat, requestedFormat string) (string, error) {
	if requestedFormat != "" {
		format := normalizeImageFormat(requestedFormat)
		if !slices.Contains(producibleImageFormats, format) {
			return "", fmt.Errorf("unsupported image format %q", requestedFormat)
		}
		return format, nil
	}

	switch normalizeImageFormat(sourceFormat) {
	case "jpeg":
		return "webp", nil
	case "png":
		return "png", nil
	case "gif":
		return "png", nil
	case "webp":
		// WebP sources may carry alpha. Keep the lossless PNG fallback unless
		// the caller explicitly asks for WebP and accepts tqwebp's current
		// opaque-only contract.
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
	if errors.Is(err, tqwebp.ErrAlphaUnsupported) || errors.Is(err, tqwebp.ErrLimitExceeded) {
		return true
	}
	for _, snippet := range []string{
		"missing src",
		"invalid image src",
		"must be local",
		"must be a root-relative path",
		"must reference a file",
		"escapes source directory",
		"unsupported",
		"image dimensions exceed",
	} {
		if strings.Contains(err.Error(), snippet) {
			return true
		}
	}
	return false
}

// respondImageOptimizerError writes a client-safe error response and logs the
// real error server-side. os.Open/os.Stat failures wrap the resolved host
// filesystem path in err.Error() (e.g. "open /home/app/public/x.png: no such
// file or directory"); that string must never reach the response body
// (gosx#199). isImageClientError already vets a fixed set of path-free,
// caller-facing messages (bad src, unsupported format, oversized request),
// so only those are echoed back verbatim.
func respondImageOptimizerError(w http.ResponseWriter, path string, req imageRequest, err error) {
	switch {
	case os.IsNotExist(err):
		Logger().Error("gosx: image optimizer source not found", "path", path, "src", req.Src, "err", err)
		http.Error(w, "image not found", http.StatusNotFound)
	case isImageClientError(err):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		Logger().Error("gosx: image optimizer failed to process image", "path", path, "src", req.Src, "err", err)
		http.Error(w, "image optimizer failed to process image", http.StatusInternalServerError)
	}
}
