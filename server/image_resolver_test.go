package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageEndpointResolverScopesDynamicMediaRoot(t *testing.T) {
	resolver, err := NewImageEndpointResolver("/_gosx/cms-image", "/media/uploads")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := resolver.Resolve("/media/uploads/hero photo.jpg", ImageTransform{Width: 640, Quality: 78})
	if !ok {
		t.Fatal("expected upload source to resolve")
	}
	for _, want := range []string{"/_gosx/cms-image?", "src=%2Fhero+photo.jpg", "w=640", "q=78"} {
		if !strings.Contains(got, want) {
			t.Fatalf("resolved URL %q lacks %q", got, want)
		}
	}
	if _, ok := resolver.Resolve("/media/elsewhere/hero.jpg", ImageTransform{Width: 640}); ok {
		t.Fatal("resolver escaped its declared source prefix")
	}
	if _, ok := resolver.Resolve("https://example.com/media/uploads/hero.jpg", ImageTransform{Width: 640}); ok {
		t.Fatal("resolver accepted a remote source")
	}
}

func TestImageEndpointResolverRejectsUnsafeConfiguration(t *testing.T) {
	for _, tc := range []struct {
		endpoint string
		prefix   string
	}{
		{"https://images.example.com/resize", "/media/uploads"},
		{"/_gosx/image?tenant=other", "/media/uploads"},
		{"/", "/media/uploads"},
		{"/_gosx/image", "/"},
	} {
		if _, err := NewImageEndpointResolver(tc.endpoint, tc.prefix); err == nil {
			t.Fatalf("NewImageEndpointResolver(%q, %q) unexpectedly succeeded", tc.endpoint, tc.prefix)
		}
	}
}

// staticExportManifestJSON is a minimal build.json body carrying one
// buildmanifest.Manifest.Images entry: /hero.png at 1200x800, with jpeg and
// webp variants at width 320 (jpeg listed first -- gosx build's own
// generation order lists a source's native format first; a webp entry at
// all only occurs when a project has registered its own WebP
// imagepipe.Encoder, since gosx ships none in-tree) plus a jpeg variant at
// its own intrinsic width 1200. It exists so this file's tests never need
// package imagepipe (or a registered encoder) to prove the resolver reads
// recorded manifest data correctly -- exactly the isolation
// TestServerPackageTreeNeverImportsImagepipe (repo root) enforces.
const staticExportManifestJSON = `{
  "runtime": {"wasm": {"file": "gosx-runtime.11111111.wasm", "hash": "11111111", "size": 10}},
  "islands": [],
  "css": [],
  "images": [
    {
      "source": "/hero.png",
      "width": 1200,
      "height": 800,
      "variants": [
        {"width": 320, "format": "jpeg", "file": "hero-320w.bbbbbbbb.jpg", "hash": "bbbbbbbb", "size": 1500},
        {"width": 320, "format": "webp", "file": "hero-320w.aaaaaaaa.webp", "hash": "aaaaaaaa", "size": 1000},
        {"width": 1200, "format": "jpeg", "file": "hero-1200w.cccccccc.jpg", "hash": "cccccccc", "size": 9000}
      ]
    }
  ]
}`

func writeStaticExportManifest(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.json"), []byte(staticExportManifestJSON), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestStaticExportImageVariantResolvesRealVariantFromManifest(t *testing.T) {
	root := t.TempDir()
	writeStaticExportManifest(t, root)

	app := New()
	app.SetRuntimeRoot(root)

	// An explicit format resolves to that format's variant.
	got, ok := app.staticExportImageVariant("/hero.png", ImageTransform{Width: 320, Format: "jpeg"})
	if !ok {
		t.Fatal("expected a matching variant for /hero.png at 320px jpeg")
	}
	if want := "/gosx/assets/images/hero-320w.bbbbbbbb.jpg"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// No format requested matches the first variant recorded at that width
	// -- gosx build's own native format, since it ships no WebP encoder
	// and so never lists webp first.
	got, ok = app.staticExportImageVariant("/hero.png", ImageTransform{Width: 320})
	if !ok {
		t.Fatal("expected a matching variant for /hero.png at 320px with no format")
	}
	if want := "/gosx/assets/images/hero-320w.bbbbbbbb.jpg"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// The intrinsic-width rung resolves too.
	got, ok = app.staticExportImageVariant("/hero.png", ImageTransform{Width: 1200})
	if !ok {
		t.Fatal("expected a matching variant for /hero.png at its own intrinsic width 1200px")
	}
	if want := "/gosx/assets/images/hero-1200w.cccccccc.jpg"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStaticExportImageVariantMissesWidthTheLadderNeverGenerated(t *testing.T) {
	root := t.TempDir()
	writeStaticExportManifest(t, root)

	app := New()
	app.SetRuntimeRoot(root)

	// 1920px exceeds hero.png's own intrinsic width (1200) -- gosx build's
	// ladder never generates a rung above the source's own width, so there
	// is nothing to find here.
	if _, ok := app.staticExportImageVariant("/hero.png", ImageTransform{Width: 1920}); ok {
		t.Fatal("unexpectedly matched a width above the source's intrinsic width")
	}
}

func TestStaticExportImageVariantMissesUnknownSource(t *testing.T) {
	root := t.TempDir()
	writeStaticExportManifest(t, root)

	app := New()
	app.SetRuntimeRoot(root)

	if _, ok := app.staticExportImageVariant("/no-such-image.png", ImageTransform{Width: 320}); ok {
		t.Fatal("unexpectedly matched a source with no recorded ImageAsset")
	}
}

func TestStaticExportImageVariantFalseWithoutManifest(t *testing.T) {
	app := New()
	app.SetRuntimeRoot(t.TempDir()) // no build.json here

	if _, ok := app.staticExportImageVariant("/hero.png", ImageTransform{Width: 320}); ok {
		t.Fatal("unexpectedly matched with no build.json manifest present")
	}
}

// TestRegisterStaticExportImageResolverEndToEnd exercises the exact
// mechanism server.Image (server/image.go, untouched by issue #200) already
// calls -- ImageURLWithResolver's lookup of the named "local" resolver --
// proving registerStaticExportImageResolver's override reaches it without
// requiring any change to image.go itself.
func TestRegisterStaticExportImageResolverEndToEnd(t *testing.T) {
	t.Cleanup(func() {
		_ = RegisterImageResolver("local", ImageResolverFunc(resolveLocalImageURL))
	})

	root := t.TempDir()
	writeStaticExportManifest(t, root)

	app := New()
	app.SetRuntimeRoot(root)
	registerStaticExportImageResolver(app)

	t.Setenv("GOSX_STATIC_EXPORT", "1")

	got := ImageURLWithResolver("", "/hero.png", ImageTransform{Width: 320, Format: "webp"})
	if want := "/gosx/assets/images/hero-320w.aaaaaaaa.webp"; got != want {
		t.Fatalf("ImageURLWithResolver = %q, want %q", got, want)
	}
}

// TestRegisterStaticExportImageResolverFallsBackWithoutManifest proves the
// override is purely additive: an app with no recorded image variants (no
// build.json, or a build.json predating issue #200) still gets exactly the
// old passthrough behavior during static export.
func TestRegisterStaticExportImageResolverFallsBackWithoutManifest(t *testing.T) {
	t.Cleanup(func() {
		_ = RegisterImageResolver("local", ImageResolverFunc(resolveLocalImageURL))
	})

	app := New()
	app.SetRuntimeRoot(t.TempDir()) // no build.json here
	registerStaticExportImageResolver(app)

	t.Setenv("GOSX_STATIC_EXPORT", "1")

	got := ImageURLWithResolver("", "/hero.png", ImageTransform{Width: 320, Format: "webp"})
	if want := "/hero.png"; got != want {
		t.Fatalf("ImageURLWithResolver = %q, want unchanged passthrough %q", got, want)
	}
}
