package server

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
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
