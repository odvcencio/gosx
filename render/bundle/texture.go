package bundle

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"
	"strings"

	"m31labs.dev/gosx/render/gpu"
)

// textureResources holds a GPU texture + its default view. Samplers are
// shared at the Renderer level, not per-texture.
type textureResources struct {
	tex           gpu.Texture
	view          gpu.TextureView
	viewDimension gpu.TextureViewDimension
}

// checkerBytes generates a size×size sRGB checkerboard as RGBA8 bytes. Used
// as the default R2 material texture when a bundle material references a
// Texture URL but real image loading hasn't landed yet (R3). The pattern is
// visible enough that missing materials are obvious.
func checkerBytes(size int, a, b [3]byte) []byte {
	out := make([]byte, size*size*4)
	half := size / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var c [3]byte
			if (x < half) == (y < half) {
				c = a
			} else {
				c = b
			}
			idx := (y*size + x) * 4
			out[idx+0] = c[0]
			out[idx+1] = c[1]
			out[idx+2] = c[2]
			out[idx+3] = 255
		}
	}
	return out
}

// whitePixelBytes is a 1×1 RGBA white, used as the fallback bound on the
// material slot when a material has no explicit texture. Keeps the bind
// group layout static — group slots are always filled.
func whitePixelBytes() []byte {
	return []byte{255, 255, 255, 255}
}

// createTextureFromRGBA allocates a 2D sRGB texture sized for RGBA8 input
// and uploads the pixel bytes. Size must match len(rgba)/4.
func (r *Renderer) createTextureFromRGBA(rgba []byte, width, height int, label string) (*textureResources, error) {
	if width*height*4 != len(rgba) {
		return nil, fmt.Errorf("bundle.createTextureFromRGBA: data len %d != %dx%dx4",
			len(rgba), width, height)
	}
	mips := generateRGBAMipChain(rgba, width, height)
	tex, err := r.device.CreateTexture(gpu.TextureDesc{
		Width:         width,
		Height:        height,
		Format:        gpu.FormatRGBA8UnormSRGB,
		Usage:         gpu.TextureUsageTextureBinding | gpu.TextureUsageCopyDst,
		MipLevelCount: len(mips),
		Label:         label,
	})
	if err != nil {
		return nil, fmt.Errorf("bundle.createTextureFromRGBA: %w", err)
	}
	for level, mip := range mips {
		r.device.Queue().WriteTextureLevel(tex, level, mip.Pixels, mip.Width*4, mip.Width, mip.Height)
	}
	return &textureResources{tex: tex, view: tex.CreateView(), viewDimension: gpu.TextureViewDimension2D}, nil
}

// RegisterRGBATexture uploads caller-supplied RGBA8 pixels and stores them in
// the material texture cache under key. Browser hosts can use this to hand the
// native renderer already-decoded image, video, canvas, or HTML texture bytes.
func (r *Renderer) RegisterRGBATexture(key string, rgba []byte, width, height int) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("bundle.RegisterRGBATexture: key is required")
	}
	res, err := r.createTextureFromRGBA(rgba, width, height, "bundle.texture:"+key)
	if err != nil {
		return err
	}
	if old := r.textureCache[key]; old != nil && old.tex != nil {
		old.tex.Destroy()
	}
	r.textureCache[key] = res
	r.envBindGroupKey = ""
	return nil
}

// LoadImageTexture decodes PNG/JPEG/GIF bytes through Go's image package and
// uploads the result as RGBA8. WebP/AVIF and SVG remain host-decoded inputs
// through RegisterRGBATexture or KTX2 variants.
func (r *Renderer) LoadImageTexture(data []byte, label string) (*textureResources, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("bundle.LoadImageTexture: decode: %w", err)
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("bundle.LoadImageTexture: empty image %dx%d", width, height)
	}
	rgba := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			cr, cg, cb, ca := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			idx := (y*width + x) * 4
			rgba[idx+0] = byte(cr >> 8)
			rgba[idx+1] = byte(cg >> 8)
			rgba[idx+2] = byte(cb >> 8)
			rgba[idx+3] = byte(ca >> 8)
		}
	}
	if strings.TrimSpace(label) == "" {
		label = "bundle.texture.image"
	}
	return r.createTextureFromRGBA(rgba, width, height, label)
}

type rgbaMipLevel struct {
	Width, Height int
	Pixels        []byte
}

func generateRGBAMipChain(base []byte, width, height int) []rgbaMipLevel {
	if width <= 0 || height <= 0 || len(base) != width*height*4 {
		return nil
	}
	out := []rgbaMipLevel{{Width: width, Height: height, Pixels: append([]byte(nil), base...)}}
	for width > 1 || height > 1 {
		prev := out[len(out)-1]
		nextW := max(1, prev.Width/2)
		nextH := max(1, prev.Height/2)
		next := rgbaMipLevel{Width: nextW, Height: nextH, Pixels: make([]byte, nextW*nextH*4)}
		for y := 0; y < nextH; y++ {
			for x := 0; x < nextW; x++ {
				writeDownsampledPixel(next.Pixels, x, y, nextW, prev.Pixels, prev.Width, prev.Height)
			}
		}
		out = append(out, next)
		width, height = nextW, nextH
	}
	return out
}

func writeDownsampledPixel(dst []byte, x, y, dstW int, src []byte, srcW, srcH int) {
	srcX := x * 2
	srcY := y * 2
	var sum [4]int
	var samples int
	for oy := 0; oy < 2; oy++ {
		sy := min(srcY+oy, srcH-1)
		for ox := 0; ox < 2; ox++ {
			sx := min(srcX+ox, srcW-1)
			idx := (sy*srcW + sx) * 4
			if idx+3 >= len(src) {
				continue
			}
			sum[0] += int(src[idx+0])
			sum[1] += int(src[idx+1])
			sum[2] += int(src[idx+2])
			sum[3] += int(src[idx+3])
			samples++
		}
	}
	if samples == 0 {
		return
	}
	dstIdx := (y*dstW + x) * 4
	dst[dstIdx+0] = uint8(sum[0] / samples)
	dst[dstIdx+1] = uint8(sum[1] / samples)
	dst[dstIdx+2] = uint8(sum[2] / samples)
	dst[dstIdx+3] = uint8(sum[3] / samples)
}

// ensureFallbackTexture returns the shared 1×1 white texture used when a
// material lacks a Texture URL. Created lazily on first call.
func (r *Renderer) ensureFallbackTexture() (*textureResources, error) {
	if r.fallbackTexture != nil {
		return r.fallbackTexture, nil
	}
	res, err := r.createTextureFromRGBA(whitePixelBytes(), 1, 1, "bundle.texture.fallback")
	if err != nil {
		return nil, err
	}
	r.fallbackTexture = res
	return res, nil
}

// ensureMaterialTexture vends a textureResources for a material's Texture URL.
// Procedural URL-keyed checkerboards keep the material texture path exercised
// in pure-Go tests; KTX2 payloads enter through LoadKTX2Texture.
func (r *Renderer) ensureMaterialTexture(url string) (*textureResources, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return r.ensureFallbackTexture()
	}
	if cached, ok := r.textureCache[url]; ok {
		return cached, nil
	}
	if res, ok, err := r.tryLoadTextureSource(url); err != nil {
		return nil, err
	} else if ok {
		r.textureCache[url] = res
		return res, nil
	}
	// Seed a checker pattern from the URL's hash so different URLs render
	// visibly different without needing a real decoder.
	a, b := colorPairFromString(url)
	res, err := r.createTextureFromRGBA(checkerBytes(64, a, b), 64, 64, "bundle.texture:"+url)
	if err != nil {
		return nil, err
	}
	r.textureCache[url] = res
	return res, nil
}

func (r *Renderer) tryLoadTextureSource(key string) (*textureResources, bool, error) {
	if data, ok := decodeDataURLBytes(key); ok {
		res, err := r.LoadImageTexture(data, "bundle.texture:data")
		if err != nil {
			// SVG/WebP/AVIF data URLs are expected until the host supplies
			// decoded bytes. Keep the visible checker fallback instead of
			// failing the frame for unsupported source formats.
			return nil, false, nil
		}
		return res, true, nil
	}
	if looksRemoteTextureURL(key) {
		return nil, false, nil
	}
	path := strings.TrimPrefix(key, "file://")
	if strings.HasPrefix(key, "file://") {
		if parsed, err := url.Parse(key); err == nil {
			path = parsed.Path
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, nil
	}
	res, err := r.LoadImageTexture(data, "bundle.texture:"+key)
	if err != nil {
		return nil, false, nil
	}
	return res, true, nil
}

func decodeDataURLBytes(value string) ([]byte, bool) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "data:") {
		return nil, false
	}
	comma := strings.IndexByte(value, ',')
	if comma < 0 {
		return nil, false
	}
	meta := strings.ToLower(value[5:comma])
	if strings.Contains(meta, "image/svg") || strings.Contains(meta, "image/webp") || strings.Contains(meta, "image/avif") {
		return nil, false
	}
	payload := value[comma+1:]
	if strings.Contains(meta, ";base64") {
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, false
		}
		return data, true
	}
	decoded, err := url.QueryUnescape(payload)
	if err != nil {
		return nil, false
	}
	return []byte(decoded), true
}

func looksRemoteTextureURL(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "blob:") ||
		strings.HasPrefix(lower, "gosx-html://")
}

// colorPairFromString produces two deterministic contrasting RGB triplets
// from an arbitrary string — FNV-1a hash split into two color channels. The
// goal is visual distinguishability across URLs, not a cryptographic map.
func colorPairFromString(s string) ([3]byte, [3]byte) {
	const offset = 14695981039346656037
	const prime = 1099511628211
	var h uint64 = offset
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime
	}
	a := [3]byte{byte(h), byte(h >> 8), byte(h >> 16)}
	b := [3]byte{byte(h >> 24), byte(h >> 32), byte(h >> 40)}
	// Ensure both colors are bright enough to read.
	for i := range a {
		if a[i] < 80 {
			a[i] += 80
		}
	}
	for i := range b {
		if b[i] < 80 {
			b[i] += 80
		}
	}
	return a, b
}
