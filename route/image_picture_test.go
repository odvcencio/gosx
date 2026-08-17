package route

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/server"
)

// manifestImageTestJSON is a build.json fixture carrying three
// buildmanifest.Manifest.Images entries this file's tests exercise:
//
//   - /manifest-hero.jpg: a JPEG source with both webp and jpeg variants at
//     three widths -- the common "full <picture>" case.
//   - /manifest-only.webp: a WebP source with only webp variants -- imagepipe
//     never generates a redundant same-format fallback for one.
//
// Every source path here is unique to this file (the "manifest-" prefix),
// so registering it as the process-global App (server.registerImageManifestLookup,
// the "last App built wins" convention every image_resolver.go test already
// relies on) can never change what an unrelated test elsewhere in this
// package observes for its own "/hero.png"-style fixture paths.
const manifestImageTestJSON = `{
  "runtime": {"wasm": {"file": "gosx-runtime.11111111.wasm", "hash": "11111111", "size": 10}},
  "islands": [],
  "css": [],
  "images": [
    {
      "source": "/manifest-hero.jpg",
      "width": 1200,
      "height": 800,
      "variants": [
        {"width": 400, "format": "webp", "file": "hero-400w.aaaaaaaa.webp", "hash": "aaaaaaaa", "size": 1000},
        {"width": 800, "format": "webp", "file": "hero-800w.bbbbbbbb.webp", "hash": "bbbbbbbb", "size": 2000},
        {"width": 1200, "format": "webp", "file": "hero-1200w.cccccccc.webp", "hash": "cccccccc", "size": 3000},
        {"width": 400, "format": "jpeg", "file": "hero-400w.dddddddd.jpg", "hash": "dddddddd", "size": 1500},
        {"width": 800, "format": "jpeg", "file": "hero-800w.eeeeeeee.jpg", "hash": "eeeeeeee", "size": 2500},
        {"width": 1200, "format": "jpeg", "file": "hero-1200w.ffffffff.jpg", "hash": "ffffffff", "size": 3500}
      ]
    },
    {
      "source": "/manifest-only.webp",
      "width": 600,
      "height": 300,
      "variants": [
        {"width": 300, "format": "webp", "file": "only-300w.11111112.webp", "hash": "11111112", "size": 700},
        {"width": 600, "format": "webp", "file": "only-600w.11111113.webp", "hash": "11111113", "size": 1400}
      ]
    }
  ]
}`

// buildManifestApp writes manifestImageTestJSON as build.json under a fresh
// temp root, builds a *server.App pointed at that root, and registers it as
// the process-global manifest lookup (via App.Build's call to
// registerImageManifestLookup) so route's LookupImageManifestAsset calls
// resolve against it.
func buildManifestApp(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "build.json"), []byte(manifestImageTestJSON), 0644); err != nil {
		t.Fatal(err)
	}
	app := server.New()
	app.SetRuntimeRoot(root)
	app.Build()
}

func TestFileRendererImageEmitsPictureWhenManifestHasBothFormats(t *testing.T) {
	buildManifestApp(t)
	html := compileImageFixture(t, `src="/manifest-hero.jpg" alt="Hero"`)

	if !strings.HasPrefix(html, "<picture>") || !strings.HasSuffix(html, "</picture>") {
		t.Fatalf("expected a <picture> wrapper, got %q", html)
	}
	for _, snippet := range []string{
		`<source type="image/webp" srcset="/gosx/assets/images/hero-400w.aaaaaaaa.webp 400w, /gosx/assets/images/hero-800w.bbbbbbbb.webp 800w, /gosx/assets/images/hero-1200w.cccccccc.webp 1200w" sizes="100vw" />`,
		`src="/gosx/assets/images/hero-1200w.ffffffff.jpg"`,
		`srcset="/gosx/assets/images/hero-400w.dddddddd.jpg 400w, /gosx/assets/images/hero-800w.eeeeeeee.jpg 800w, /gosx/assets/images/hero-1200w.ffffffff.jpg 1200w"`,
		`sizes="100vw"`,
		`alt="Hero"`,
		// No explicit width/height: the source's own intrinsic dimensions
		// are injected automatically (gosx#201's whole point).
		`width="1200"`,
		`height="800"`,
		`loading="lazy"`,
		`decoding="async"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
	if strings.Contains(html, "fetchpriority") {
		t.Fatalf("expected no fetchpriority without priority, got %q", html)
	}
}

func TestFileRendererImagePictureInjectsProportionalHeightFromExplicitWidth(t *testing.T) {
	buildManifestApp(t)
	html := compileImageFixture(t, `src="/manifest-hero.jpg" alt="Hero" width={600}`)

	// Intrinsic 1200x800 (3:2). An explicit width alone derives height
	// proportionally: 800 * (600/1200) = 400.
	for _, snippet := range []string{`width="600"`, `height="400"`} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}

func TestFileRendererImagePictureInjectsProportionalWidthFromExplicitHeight(t *testing.T) {
	buildManifestApp(t)
	html := compileImageFixture(t, `src="/manifest-hero.jpg" alt="Hero" height={200}`)

	// Intrinsic 1200x800 (3:2). An explicit height alone derives width
	// proportionally: 1200 * (200/800) = 300.
	for _, snippet := range []string{`width="300"`, `height="200"`} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}

func TestFileRendererImagePictureKeepsBothExplicitDimensionsVerbatim(t *testing.T) {
	buildManifestApp(t)
	html := compileImageFixture(t, `src="/manifest-hero.jpg" alt="Hero" width={500} height={500}`)

	// Both dimensions explicit: used verbatim, even off the source's own
	// aspect ratio -- matching server.Image's existing "exact rectangle"
	// contract for the non-manifest path.
	for _, snippet := range []string{`width="500"`, `height="500"`} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}

func TestFileRendererImagePicturePriorityFlipsLoadingAndFetchPriority(t *testing.T) {
	buildManifestApp(t)
	html := compileImageFixture(t, `src="/manifest-hero.jpg" alt="Hero" priority`)

	for _, snippet := range []string{`loading="eager"`, `fetchpriority="high"`} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
	if strings.Contains(html, `loading="lazy"`) {
		t.Fatalf("expected eager to override the default lazy loading, got %q", html)
	}
}

func TestFileRendererImagePictureExtraAttrsLandOnImgFallback(t *testing.T) {
	buildManifestApp(t)
	html := compileImageFixture(t, `src="/manifest-hero.jpg" alt="Hero" class="demo-image"`)

	if !strings.Contains(html, `<img`) {
		t.Fatalf("expected an <img> fallback, got %q", html)
	}
	imgStart := strings.Index(html, "<img")
	imgEnd := strings.Index(html[imgStart:], ">")
	imgTag := html[imgStart : imgStart+imgEnd]
	if !strings.Contains(imgTag, `class="demo-image"`) {
		t.Fatalf("expected class on the <img> fallback, got %q", imgTag)
	}
	sourceStart := strings.Index(html, "<source")
	sourceEnd := strings.Index(html[sourceStart:], ">")
	sourceTag := html[sourceStart : sourceStart+sourceEnd]
	if strings.Contains(sourceTag, "demo-image") {
		t.Fatalf("expected class not to leak onto <source>, got %q", sourceTag)
	}
}

func TestFileRendererImageWebPSourceSkipsPictureWrapper(t *testing.T) {
	buildManifestApp(t)
	html := compileImageFixture(t, `src="/manifest-only.webp" alt="Only"`)

	if strings.Contains(html, "<picture>") || strings.Contains(html, "<source") {
		t.Fatalf("expected a plain <img>, no <picture>/<source> wrapper for a WebP-native source, got %q", html)
	}
	for _, snippet := range []string{
		`<img`,
		`src="/gosx/assets/images/only-600w.11111113.webp"`,
		`srcset="/gosx/assets/images/only-300w.11111112.webp 300w, /gosx/assets/images/only-600w.11111113.webp 600w"`,
		`width="600"`,
		`height="300"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}

func TestFileRendererImageFallsBackWhenSrcHasNoManifestEntry(t *testing.T) {
	buildManifestApp(t)
	// The App above only has entries for /manifest-hero.jpg and
	// /manifest-only.webp; this src is absent even though a manifest lookup
	// IS registered, proving the fallback decision is per-src, not a global
	// "manifest present or not" switch. No transform prop is set, so
	// server.Image's own #199 behavior leaves the src unmodified (see
	// TestFileRendererImageWithoutResponsiveOmitsSrcset for the sibling
	// case with a width set instead).
	html := compileImageFixture(t, `src="/not-in-manifest.png" alt="Untracked"`)

	if strings.Contains(html, "<picture>") {
		t.Fatalf("expected the #199 fallback path, not a <picture>, got %q", html)
	}
	if !strings.Contains(html, `src="/not-in-manifest.png"`) {
		t.Fatalf("expected the plain passthrough src, got %q", html)
	}
}

func TestFileRendererImageExplicitFormatOptsOutOfManifestPicture(t *testing.T) {
	buildManifestApp(t)
	html := compileImageFixture(t, `src="/manifest-hero.jpg" alt="Hero" format="png"`)

	if strings.Contains(html, "<picture>") {
		t.Fatalf("expected an explicit format to opt out of manifest mode, got %q", html)
	}
	if !strings.Contains(html, "fmt=png") {
		t.Fatalf("expected the runtime optimizer to receive the explicit format, got %q", html)
	}
}

func TestFileRendererImageExplicitQualityOptsOutOfManifestPicture(t *testing.T) {
	buildManifestApp(t)
	html := compileImageFixture(t, `src="/manifest-hero.jpg" alt="Hero" quality={55}`)

	if strings.Contains(html, "<picture>") {
		t.Fatalf("expected an explicit quality to opt out of manifest mode, got %q", html)
	}
	if !strings.Contains(html, "q=55") {
		t.Fatalf("expected the runtime optimizer to receive the explicit quality, got %q", html)
	}
}

func TestFileRendererImageExternalSrcIgnoresRegisteredManifest(t *testing.T) {
	buildManifestApp(t)
	html := compileImageFixture(t, `src="https://cdn.example.com/manifest-hero.jpg" alt="External" width={400} height={300}`)

	if strings.Contains(html, "<picture>") {
		t.Fatalf("expected an external src never to resolve through the manifest, got %q", html)
	}
	if !strings.Contains(html, `src="https://cdn.example.com/manifest-hero.jpg"`) {
		t.Fatalf("expected the external URL to pass through unchanged, got %q", html)
	}
}

// TestFileRendererImageWithoutRegisteredAppStaysOnFallbackPath proves dev
// mode (no App has ever called Build in this process) never takes the
// <picture> path -- it exercises the same fixture path this file's own
// manifest-backed tests use, through gosx.Compile + RenderProgramComponent
// directly (no *server.App at all), the same shape cmd/gosx render and
// every pre-existing image_attrs_test.go case already use.
func TestFileRendererImageWithoutRegisteredAppStaysOnFallbackPath(t *testing.T) {
	prog, err := gosx.Compile([]byte("package docs\n\nfunc Page() Node {\n\treturn <Image src=\"/never-built.png\" alt=\"Never built\" />\n}\n"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(html, "<picture>") {
		t.Fatalf("expected no <picture> with no manifest registered, got %q", html)
	}
}
