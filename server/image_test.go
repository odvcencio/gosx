package server

import (
	"bytes"
	"encoding/base64"
	"fmt"
	stdhtml "html"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestImageHelperBuildsResponsiveMarkup(t *testing.T) {
	node := Image(ImageProps{
		Src:     "/hero.png",
		Alt:     "Hero",
		Width:   960,
		Height:  540,
		Widths:  []int{320, 640, 960},
		Sizes:   "(max-width: 900px) 100vw, 50vw",
		Quality: 78,
	}, gosx.Attrs(gosx.Attr("class", "hero-image")))

	html := gosx.RenderHTML(node)
	for _, snippet := range []string{
		`src="/_gosx/image?`,
		`srcset="/_gosx/image?`,
		`w=320`,
		`w=640`,
		`w=960`,
		`alt="Hero"`,
		`loading="lazy"`,
		`decoding="async"`,
		`width="960"`,
		`height="540"`,
		`class="hero-image"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}

func TestImageHelperRendersOrderedArtDirectionSources(t *testing.T) {
	node := Image(ImageProps{
		Src:    "https://images.example.com/hero-1600.jpg",
		Alt:    "Ceramic bowl on a worktable",
		Width:  1600,
		Height: 900,
		Sources: []ImageSource{
			{Media: "(max-width: 600px)", SrcSet: "https://images.example.com/hero-mobile-480.jpg 480w, https://images.example.com/hero-mobile-800.jpg 800w", Sizes: "100vw", Type: "image/jpeg", Width: 800, Height: 1000},
			{Media: "(max-width: 1000px)", SrcSet: "https://images.example.com/hero-tablet-1000.jpg 1000w"},
		},
		PictureAttrs: gosx.Attrs(
			gosx.Attr("class", "responsive-picture"),
			gosx.Attr("data-layout", "wide & narrow"),
		),
	}, gosx.Attrs(gosx.Attr("class", "hero-image")))

	html := gosx.RenderHTML(node)
	if !strings.HasPrefix(html, `<picture class="responsive-picture" data-layout="wide &amp; narrow">`) || !strings.HasSuffix(html, "</picture>") {
		t.Fatalf("expected sources to opt into a picture wrapper, got %q", html)
	}
	mobile := strings.Index(html, `media="(max-width: 600px)"`)
	tablet := strings.Index(html, `media="(max-width: 1000px)"`)
	img := strings.Index(html, "<img")
	if mobile < 0 || tablet < 0 || img < 0 || !(mobile < tablet && tablet < img) {
		t.Fatalf("expected authored source order before the fallback img, got %q", html)
	}
	for _, snippet := range []string{
		`srcset="https://images.example.com/hero-mobile-480.jpg 480w, https://images.example.com/hero-mobile-800.jpg 800w"`,
		`sizes="100vw"`,
		`type="image/jpeg"`,
		`width="800"`,
		`height="1000"`,
		`src="https://images.example.com/hero-1600.jpg"`,
		`width="1600"`,
		`height="900"`,
		`class="hero-image"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}

func TestImageHelperSkipsBlankArtDirectionSources(t *testing.T) {
	html := gosx.RenderHTML(Image(ImageProps{
		Src:          "/hero.png",
		Alt:          "Hero",
		Sources:      []ImageSource{{Media: "(max-width: 600px)", SrcSet: "  ", Width: 400, Height: 700}},
		PictureAttrs: gosx.Attrs(gosx.Attr("class", "responsive-picture")),
	}, gosx.Attrs(gosx.Attr("class", "hero-image"))))
	if strings.Contains(html, "<picture") || strings.Contains(html, "<source") {
		t.Fatalf("expected blank sources not to change the existing img-only contract, got %q", html)
	}
	if strings.Contains(html, "responsive-picture") || !strings.Contains(html, `class="hero-image"`) {
		t.Fatalf("expected wrapper attrs to be ignored without a wrapper and ordinary attrs to remain on img, got %q", html)
	}
}

func TestImageHelperOmitsNonPositiveSourceDimensions(t *testing.T) {
	html := gosx.RenderHTML(Image(ImageProps{
		Src:     "/hero.png",
		Alt:     "Hero",
		Sources: []ImageSource{{Media: "(max-width: 600px)", SrcSet: "/mobile.png 400w", Width: 0, Height: -1}},
	}))
	sourceStart := strings.Index(html, "<source")
	sourceEnd := strings.Index(html[sourceStart:], ">")
	sourceTag := html[sourceStart : sourceStart+sourceEnd]
	if strings.Contains(sourceTag, " width=") || strings.Contains(sourceTag, " height=") {
		t.Fatalf("expected non-positive source dimensions to be omitted, got %q", sourceTag)
	}
}

func TestImageHelperBypassesOptimizerForSVG(t *testing.T) {
	html := gosx.RenderHTML(Image(ImageProps{
		Src: "/mark.svg",
		Alt: "Mark",
	}))

	if strings.Contains(html, defaultImageEndpoint) {
		t.Fatalf("expected svg source to bypass optimizer, got %q", html)
	}
	if !strings.Contains(html, `src="/mark.svg"`) {
		t.Fatalf("expected raw svg src, got %q", html)
	}
}

func TestImageHelperNormalizesRelativePublicPaths(t *testing.T) {
	html := gosx.RenderHTML(Image(ImageProps{
		Src: "images/hero.png",
		Alt: "Hero",
	}))

	if !strings.Contains(html, `src="/images/hero.png"`) {
		t.Fatalf("expected normalized public asset path, got %q", html)
	}
}

func TestImageHelperSupportsCustomResolver(t *testing.T) {
	resolverName := "test-resolver-" + strings.ReplaceAll(t.Name(), "/", "-")
	if err := RegisterImageResolver(resolverName, ImageResolverFunc(func(src string, transform ImageTransform) (string, bool) {
		return fmt.Sprintf("https://img.example.com%s?w=%d", src, transform.Width), true
	})); err != nil {
		t.Fatal(err)
	}

	html := gosx.RenderHTML(Image(ImageProps{
		Src:      "/hero.png",
		Alt:      "Hero",
		Width:    640,
		Widths:   []int{320, 640},
		Resolver: resolverName,
	}))

	for _, snippet := range []string{
		`src="https://img.example.com/hero.png?w=640"`,
		`srcset="https://img.example.com/hero.png?w=320 320w, https://img.example.com/hero.png?w=640 640w"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}

func TestMustRegisterImageResolverReturnsErrorInsteadOfPanicking(t *testing.T) {
	if err := MustRegisterImageResolver("", ImageResolverFunc(func(string, ImageTransform) (string, bool) {
		return "", false
	})); err == nil {
		t.Fatal("expected missing resolver name error")
	}
	if err := MustRegisterImageResolver("nil-resolver", nil); err == nil {
		t.Fatal("expected nil resolver error")
	}
}

func TestImageHelperBuildsAutomaticResponsiveMarkup(t *testing.T) {
	html := gosx.RenderHTML(Image(ImageProps{
		Src:        "/hero.jpg",
		Alt:        "Hero",
		Width:      960,
		Height:     540,
		Responsive: true,
		Priority:   true,
	}))

	for _, snippet := range []string{
		`srcset="/_gosx/image?`,
		`w=320`,
		`w=828`,
		`w=960`,
		`sizes="100vw"`,
		`loading="eager"`,
		`fetchpriority="high"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}

func TestImageHelperBypassesOptimizerDuringStaticExport(t *testing.T) {
	if err := os.Setenv("GOSX_STATIC_EXPORT", "1"); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv("GOSX_STATIC_EXPORT")

	html := gosx.RenderHTML(Image(ImageProps{
		Src:    "/hero.png",
		Alt:    "Hero",
		Width:  640,
		Height: 360,
	}))
	if strings.Contains(html, defaultImageEndpoint) {
		t.Fatalf("expected static export image markup to bypass optimizer, got %q", html)
	}
	if !strings.Contains(html, `src="/hero.png"`) {
		t.Fatalf("expected raw image src during static export, got %q", html)
	}
}

func TestAppServesOptimizedPNGVariant(t *testing.T) {
	dir := t.TempDir()
	publicDir := filepath.Join(dir, "public")
	if err := os.MkdirAll(publicDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeTestPNG(filepath.Join(publicDir, "hero.png"), 120, 60); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.SetPublicDir(publicDir)
	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, defaultImageEndpoint+"?src=/hero.png&w=40", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("expected png content type, got %q", got)
	}
	if cache := w.Header().Get("Cache-Control"); !strings.Contains(cache, "immutable") {
		t.Fatalf("expected immutable cache header, got %q", cache)
	}

	img, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode optimized png: %v", err)
	}
	if got := img.Bounds().Dx(); got != 40 {
		t.Fatalf("expected width 40, got %d", got)
	}
	if got := img.Bounds().Dy(); got != 20 {
		t.Fatalf("expected height 20, got %d", got)
	}
}

func TestAppRejectsImageTraversal(t *testing.T) {
	app := New()
	app.SetPublicDir(t.TempDir())
	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, defaultImageEndpoint+"?src=/../secret.png&w=40", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAppRejectsOversizedImageDimensionsBeforeDecoding(t *testing.T) {
	app := New()
	app.SetPublicDir(t.TempDir())
	handler := app.Build()

	for _, query := range []string{
		"?src=/missing.png&w=100000&h=100000",
		"?src=/missing.png&w=4096&h=4096",
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, defaultImageEndpoint+query, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d; want 400", query, w.Code)
		}
		if !strings.Contains(w.Body.String(), "image dimensions exceed") {
			t.Fatalf("%s body = %q", query, w.Body.String())
		}
	}
}

func TestImageHeadDoesNotDecodeOrTransformSource(t *testing.T) {
	publicDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(publicDir, "broken.png"), []byte("not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := New()
	app.SetPublicDir(publicDir)
	handler := app.Build()

	head := httptest.NewRecorder()
	handler.ServeHTTP(head, httptest.NewRequest(http.MethodHead, defaultImageEndpoint+"?src=/broken.png&w=320", nil))
	if head.Code != http.StatusOK || head.Header().Get("Content-Type") != "image/png" || head.Body.Len() != 0 {
		t.Fatalf("HEAD optimized image = %d type=%q body=%q", head.Code, head.Header().Get("Content-Type"), head.Body.String())
	}

	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, defaultImageEndpoint+"?src=/broken.png&w=320", nil))
	if get.Code != http.StatusInternalServerError {
		t.Fatalf("GET corrupt image = %d; want 500", get.Code)
	}
}

func TestImageTransformConcurrencyIsBounded(t *testing.T) {
	releases := make([]func(), 0, maxConcurrentImageVariants)
	for range maxConcurrentImageVariants {
		release, ok := acquireImageTransform()
		if !ok {
			t.Fatal("image transform slot unavailable before capacity")
		}
		releases = append(releases, release)
	}
	if release, ok := acquireImageTransform(); ok {
		release()
		t.Fatal("image transform capacity was not enforced")
	}
	for _, release := range releases {
		release()
	}
}

func TestTargetImageSizeNeverUpscalesTwoDimensionRequest(t *testing.T) {
	width, height := targetImageSize(960, 600, 4096, 4096)
	if width != 960 || height != 600 {
		t.Fatalf("target size = %dx%d; want source-bound 960x600", width, height)
	}
}

// TestLiveHandlerFixesSrcsetLadderAspectDistortion reproduces the gosx#199
// distortion: image.go's srcset loop used to copy props.Height into every
// ladder entry, so a 320w candidate cut from a 1200x800 source carried
// h=800 and the live handler rendered it as 320x800 instead of the
// proportional 320x213. This renders the real <Image> markup, extracts the
// actual 320w srcset URL, and serves it through the real optimizer handler
// (the same one App.Build() wires at /_gosx/image) to prove both halves of
// the fix: the URL is height-free, and the handler now derives height
// proportionally.
func TestLiveHandlerFixesSrcsetLadderAspectDistortion(t *testing.T) {
	publicDir := t.TempDir()
	if err := writeTestPNG(filepath.Join(publicDir, "hero.png"), 1200, 800); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.SetPublicDir(publicDir)
	handler := app.Build()

	rendered := gosx.RenderHTML(Image(ImageProps{
		Src:    "/hero.png",
		Alt:    "Hero",
		Width:  1200,
		Height: 800,
		Widths: []int{320, 1200},
	}))

	entry320 := srcsetEntryURL(t, rendered, 320)
	if strings.Contains(entry320, "h=") {
		t.Fatalf("expected the 320w srcset entry to omit height, got %q", entry320)
	}

	req := httptest.NewRequest(http.MethodGet, entry320, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s = %d; want 200, body %q", entry320, w.Code, w.Body.String())
	}

	img, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode 320w variant: %v", err)
	}
	if got := img.Bounds().Dx(); got != 320 {
		t.Fatalf("320w variant width = %d; want 320", got)
	}
	if got := img.Bounds().Dy(); got != 213 {
		t.Fatalf("320w variant height = %d; want 213 (proportional from a 1200x800 source, not the distorted 800 the bug produced)", got)
	}
}

// srcsetEntryURL extracts the URL for a specific width candidate out of a
// rendered <img srcset="..."> attribute.
func srcsetEntryURL(t *testing.T, rendered string, width int) string {
	t.Helper()
	const marker = `srcset="`
	start := strings.Index(rendered, marker)
	if start == -1 {
		t.Fatalf("no srcset attribute in %q", rendered)
	}
	start += len(marker)
	end := strings.Index(rendered[start:], `"`)
	if end == -1 {
		t.Fatalf("unterminated srcset attribute in %q", rendered)
	}
	srcset := stdhtml.UnescapeString(rendered[start : start+end])
	suffix := fmt.Sprintf(" %dw", width)
	for _, entry := range strings.Split(srcset, ", ") {
		if strings.HasSuffix(entry, suffix) {
			return strings.TrimSuffix(entry, suffix)
		}
	}
	t.Fatalf("no %dw entry in srcset %q", width, srcset)
	return ""
}

// TestImageRejectsNonProducibleFormatAtRenderTime covers gosx#199: a format
// the optimizer handler can never encode must panic at Image() render time
// with a clear, format-naming message, instead of shipping a fmt=webp URL
// that only fails once a browser requests it.
func TestImageRejectsNonProducibleFormatAtRenderTime(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected Image to panic for an unproducible format")
		}
		if msg := fmt.Sprint(r); !strings.Contains(msg, "webp") {
			t.Fatalf("expected the panic message to name the rejected format, got %q", msg)
		}
	}()
	Image(ImageProps{Src: "/hero.png", Alt: "Hero", Format: "webp"})
	t.Fatal("unreachable: Image did not panic")
}

// TestImageAllowsProducibleFormatsAtRenderTime is the positive control for
// TestImageRejectsNonProducibleFormatAtRenderTime: every format the handler
// can actually encode, plus an unset format, must render without panicking.
func TestImageAllowsProducibleFormatsAtRenderTime(t *testing.T) {
	for _, format := range []string{"", "jpeg", "jpg", "png", "gif"} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("format %q: unexpected panic: %v", format, r)
				}
			}()
			Image(ImageProps{Src: "/hero.png", Alt: "Hero", Format: format})
		}()
	}
}

// TestSelectTargetImageFormatRejectsWebpAsOutputEvenAfterDecoderRegistration
// covers gosx#199's decoder/encoder split: registering the WebP decoder
// makes WebP a decodable SOURCE format, but the handler still has no WebP
// encoder, so the render-time and request-time allowlists must both keep
// rejecting fmt=webp as an OUTPUT format.
func TestSelectTargetImageFormatRejectsWebpAsOutputEvenAfterDecoderRegistration(t *testing.T) {
	if _, err := selectTargetImageFormat("png", "webp"); err == nil {
		t.Fatal("expected an explicit fmt=webp request to be rejected as an output format")
	}
	got, err := selectTargetImageFormat("webp", "")
	if err != nil {
		t.Fatalf("expected a webp source with no explicit format to fall back to a producible output, got err: %v", err)
	}
	if got != "png" {
		t.Fatalf("expected a webp source to default to png output (no webp encoder), got %q", got)
	}
}

// TestShouldOptimizeImageSourceAcceptsWebp covers gosx#199: a .webp source
// must be eligible for resize-only optimizer transforms now that its
// decoder is registered.
func TestShouldOptimizeImageSourceAcceptsWebp(t *testing.T) {
	if !shouldOptimizeImageSource("/photos/hero.webp") {
		t.Fatal("expected a .webp source to be eligible for optimization")
	}
}

// gopherDocWebpBase64 is gopher-doc.1bpp.lossless.webp (75x100) from the
// golang.org/x/image/webp package's own test corpus (BSD-3-Clause,
// https://cs.opensource.google/go/x/image, LICENSE), already a transitive
// build input via the golang.org/x/image module dependency. Embedded here so
// the webp decode/probe tests are self-contained and do not depend on the
// module cache layout.
const gopherDocWebpBase64 = "UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA=="

func writeGopherDocWebpFixture(t *testing.T, path string) {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(gopherDocWebpBase64)
	if err != nil {
		t.Fatalf("decode embedded webp fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestWebpDecoderIsRegisteredForDimensionProbing proves the blank import of
// golang.org/x/image/webp (gosx#199) registers the WebP decoder with the
// standard image package, so image.DecodeConfig can probe WebP dimensions
// without decoding the full image.
func TestWebpDecoderIsRegisteredForDimensionProbing(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString(gopherDocWebpBase64)
	if err != nil {
		t.Fatalf("decode embedded webp fixture: %v", err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("image.DecodeConfig: %v (webp decoder not registered?)", err)
	}
	if format != "webp" {
		t.Fatalf("format = %q; want webp", format)
	}
	if cfg.Width != 75 || cfg.Height != 100 {
		t.Fatalf("dimensions = %dx%d; want 75x100", cfg.Width, cfg.Height)
	}
}

// TestImageHandlerDecodesWebpSourceAndProbesDimensions covers gosx#199 end
// to end through the live optimizer handler: a WebP source now decodes (HEAD
// and GET), and resizes proportionally, falling back to a png-encoded
// output since there is no WebP encoder.
func TestImageHandlerDecodesWebpSourceAndProbesDimensions(t *testing.T) {
	publicDir := t.TempDir()
	writeGopherDocWebpFixture(t, filepath.Join(publicDir, "gopher.webp"))

	app := New()
	app.SetPublicDir(publicDir)
	handler := app.Build()

	head := httptest.NewRecorder()
	handler.ServeHTTP(head, httptest.NewRequest(http.MethodHead, defaultImageEndpoint+"?src=/gopher.webp&w=40", nil))
	if head.Code != http.StatusOK {
		t.Fatalf("HEAD webp source = %d; want 200, body %q", head.Code, head.Body.String())
	}
	if got := head.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("HEAD webp source content type = %q; want image/png", got)
	}

	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, defaultImageEndpoint+"?src=/gopher.webp&w=40", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("GET webp source = %d; want 200, body %q", get.Code, get.Body.String())
	}
	if got := get.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("GET webp source content type = %q; want image/png", got)
	}

	img, err := png.Decode(bytes.NewReader(get.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode webp-sourced variant: %v", err)
	}
	if got := img.Bounds().Dx(); got != 40 {
		t.Fatalf("webp-sourced variant width = %d; want 40", got)
	}
	if got := img.Bounds().Dy(); got != 53 {
		t.Fatalf("webp-sourced variant height = %d; want 53 (proportional from a 75x100 source)", got)
	}
}

// TestImageOptimizerRejectsUnproducibleFormatWithClientSafeMessage covers
// gosx#199: even though a webp SOURCE now decodes, fmt=webp as a requested
// OUTPUT format must still 400 with the existing client-safe message (no
// producible-format allowlist regression from registering the decoder).
func TestImageOptimizerRejectsUnproducibleFormatWithClientSafeMessage(t *testing.T) {
	publicDir := t.TempDir()
	if err := writeTestPNG(filepath.Join(publicDir, "hero.png"), 40, 40); err != nil {
		t.Fatal(err)
	}
	app := New()
	app.SetPublicDir(publicDir)
	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, defaultImageEndpoint+"?src=/hero.png&w=20&fmt=webp", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("fmt=webp request = %d; want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "unsupported image format") {
		t.Fatalf("expected the client-safe unsupported-format message, got %q", w.Body.String())
	}
}

// TestImageOptimizerNotFoundResponseNeverLeaksHostFilesystemPath covers
// gosx#199: a missing source must 404 with a generic message, while the
// real error — which wraps the resolved absolute host path — goes only to
// the server log.
func TestImageOptimizerNotFoundResponseNeverLeaksHostFilesystemPath(t *testing.T) {
	publicDir := t.TempDir()
	app := New()
	app.SetPublicDir(publicDir)
	handler := app.Build()

	var logBuf bytes.Buffer
	prevLogger := Logger()
	SetLogger(slog.New(slog.NewJSONHandler(&logBuf, nil)))
	defer SetLogger(prevLogger)

	req := httptest.NewRequest(http.MethodGet, defaultImageEndpoint+"?src=/missing.png&w=40", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("missing source = %d; want 404", w.Code)
	}
	if got, want := w.Body.String(), "image not found\n"; got != want {
		t.Fatalf("404 body = %q; want the generic message %q", got, want)
	}
	if strings.Contains(w.Body.String(), publicDir) {
		t.Fatalf("404 body leaked the host filesystem path: %q", w.Body.String())
	}
	if !strings.Contains(logBuf.String(), publicDir) {
		t.Fatalf("expected the real error (with the host path) to reach the server log, got %q", logBuf.String())
	}
}

// TestImageOptimizerInternalErrorResponseNeverLeaksHostFilesystemPath covers
// gosx#199 for the decode-failure path: a corrupt source must 500 with a
// generic message, while the real decode error — which wraps the resolved
// absolute host path via os.Open — goes only to the server log.
func TestImageOptimizerInternalErrorResponseNeverLeaksHostFilesystemPath(t *testing.T) {
	publicDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(publicDir, "broken.png"), []byte("not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := New()
	app.SetPublicDir(publicDir)
	handler := app.Build()

	var logBuf bytes.Buffer
	prevLogger := Logger()
	SetLogger(slog.New(slog.NewJSONHandler(&logBuf, nil)))
	defer SetLogger(prevLogger)

	req := httptest.NewRequest(http.MethodGet, defaultImageEndpoint+"?src=/broken.png&w=40", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("corrupt source = %d; want 500", w.Code)
	}
	if got, want := w.Body.String(), "image optimizer failed to process image\n"; got != want {
		t.Fatalf("500 body = %q; want the generic message %q", got, want)
	}
	if strings.Contains(w.Body.String(), publicDir) {
		t.Fatalf("500 body leaked the host filesystem path: %q", w.Body.String())
	}
	if !strings.Contains(logBuf.String(), publicDir) {
		t.Fatalf("expected the real error (with the host path) to reach the server log, got %q", logBuf.String())
	}
}

func writeTestPNG(path string, width, height int) error {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8((x * 255) / max(1, width-1)),
				G: uint8((y * 255) / max(1, height-1)),
				B: 140,
				A: 255,
			})
		}
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return png.Encode(file, img)
}
