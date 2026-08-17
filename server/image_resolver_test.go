package server

import (
	"os"
	"path/filepath"
	"testing"
)

// staticExportManifestJSON is a minimal build.json body carrying one
// buildmanifest.Manifest.Images entry: /hero.png at 1200x800, with webp and
// jpeg variants at width 320 plus a webp variant at its own intrinsic width
// 1200. It exists so this file's tests never need package imagepipe (or its
// encoder) to prove the resolver reads recorded manifest data correctly --
// exactly the isolation TestServerPackageTreeNeverImportsImagepipe (repo
// root) enforces.
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
        {"width": 320, "format": "webp", "file": "hero-320w.aaaaaaaa.webp", "hash": "aaaaaaaa", "size": 1000},
        {"width": 320, "format": "jpeg", "file": "hero-320w.bbbbbbbb.jpg", "hash": "bbbbbbbb", "size": 1500},
        {"width": 1200, "format": "webp", "file": "hero-1200w.cccccccc.webp", "hash": "cccccccc", "size": 9000}
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

	// No format requested defaults to webp -- the build pipeline's own
	// default output.
	got, ok = app.staticExportImageVariant("/hero.png", ImageTransform{Width: 320})
	if !ok {
		t.Fatal("expected a matching variant for /hero.png at 320px with no format")
	}
	if want := "/gosx/assets/images/hero-320w.aaaaaaaa.webp"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// The intrinsic-width rung resolves too.
	got, ok = app.staticExportImageVariant("/hero.png", ImageTransform{Width: 1200})
	if !ok {
		t.Fatal("expected a matching variant for /hero.png at its own intrinsic width 1200px")
	}
	if want := "/gosx/assets/images/hero-1200w.cccccccc.webp"; got != want {
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
