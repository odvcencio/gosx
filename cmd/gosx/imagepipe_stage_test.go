package main

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx/imagepipe"
)

// writeTestSourcePNG writes a deterministic width x height PNG to path --
// every pixel a pure function of its own coordinates, so repeat calls
// produce byte-identical fixtures with no external asset.
func writeTestSourcePNG(t *testing.T, path string, width, height int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// writeTestSourceJPEG writes a deterministic width x height JPEG to path,
// mirroring writeTestSourcePNG's own convention.
func writeTestSourceJPEG(t *testing.T, path string, width, height int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 64, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
}

func TestImagePipeNativeFormat(t *testing.T) {
	tests := []struct {
		ext    string
		want   imagepipe.Format
		wantOK bool
	}{
		{".jpg", imagepipe.FormatJPEG, true},
		{".JPEG", imagepipe.FormatJPEG, true},
		{".png", imagepipe.FormatPNG, true},
		{".gif", imagepipe.FormatPNG, true},
		// WebP retains a PNG fallback because the source may carry alpha.
		{".webp", imagepipe.FormatPNG, true},
		{".bmp", "", false},
	}
	for _, test := range tests {
		got, ok := imagePipeNativeFormat(test.ext)
		if ok != test.wantOK || got != test.want {
			t.Errorf("imagePipeNativeFormat(%q) = (%q, %v), want (%q, %v)", test.ext, got, ok, test.want, test.wantOK)
		}
	}
}

func TestImageAssetBaseName(t *testing.T) {
	tests := map[string]string{
		"/photo.jpg":            "photo",
		"/photos/team hero.png": "photos_team_hero",
		"/a/b/c.gif":            "a_b_c",
		"/":                     "image",
		"":                      "image",
	}
	for source, want := range tests {
		if got := imageAssetBaseName(source); got != want {
			t.Errorf("imageAssetBaseName(%q) = %q, want %q", source, got, want)
		}
	}
}

// TestStageImageVariantsWritesHashedVariantsAndPopulatesManifest is this
// package's focused stage test for issue #200's build wiring: it exercises
// stageImageVariants directly (the same function stageDeploymentBundle
// calls beside its public/ copy) against a real temp public/ directory,
// without paying for a full `gosx build` run through the WASM/TinyGo
// toolchain (see tinygo_build_test.go's own preference for targeted
// function tests over a full build for the same reason).
//
// The opaque PNG gets both its native fallback and tqwebp ladders.
func TestStageImageVariantsWritesHashedVariantsAndPopulatesManifest(t *testing.T) {
	projectDir := t.TempDir()
	distDir := filepath.Join(t.TempDir(), "dist")
	writeTestSourcePNG(t, filepath.Join(projectDir, "public", "hero.png"), 1200, 800)

	assets, err := stageImageVariants(projectDir, distDir)
	if err != nil {
		t.Fatalf("stageImageVariants: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("got %d image assets, want 1: %+v", len(assets), assets)
	}

	asset := assets[0]
	if asset.Source != "/hero.png" {
		t.Errorf("Source = %q, want /hero.png", asset.Source)
	}
	if asset.Width != 1200 || asset.Height != 800 {
		t.Errorf("dims = %dx%d, want 1200x800", asset.Width, asset.Height)
	}

	wantWidths := imagepipe.Ladder(1200, []int{320, 480, 640, 750, 828, 1080, 1200, 1920, 2048, 3840})
	if len(asset.Variants) != len(wantWidths)*2 {
		t.Fatalf("got %d variants, want %d (%d ladder widths x native+webp): %+v", len(asset.Variants), len(wantWidths)*2, len(wantWidths), asset.Variants)
	}

	formatsByWidth := make(map[int]map[string]bool, len(wantWidths))
	for _, variant := range asset.Variants {
		if variant.Width > asset.Width {
			t.Errorf("variant width %d exceeds intrinsic width %d (upscale)", variant.Width, asset.Width)
		}
		if variant.Format != "png" && variant.Format != "webp" {
			t.Errorf("unexpected variant format %q, want png or webp", variant.Format)
		}
		if formatsByWidth[variant.Width] == nil {
			formatsByWidth[variant.Width] = map[string]bool{}
		}
		formatsByWidth[variant.Width][variant.Format] = true
		path := filepath.Join(distDir, "assets", "images", variant.File)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("variant file %s missing on disk: %v", path, err)
		}
		if info.Size() != variant.Size {
			t.Errorf("variant %s on-disk size %d != recorded Size %d", variant.File, info.Size(), variant.Size)
		}
		// writeHashedWithoutCompressedSidecars must not have left gzip/brotli
		// sidecars behind for image bytes (issue #200: they waste build time
		// re-compressing already-compressed image bytes).
		for _, sidecarExt := range []string{".gz", ".br"} {
			if _, err := os.Stat(path + sidecarExt); !os.IsNotExist(err) {
				t.Errorf("unexpected compressed sidecar %s%s for an image variant", path, sidecarExt)
			}
		}
	}
	for _, width := range wantWidths {
		if !formatsByWidth[width]["png"] || !formatsByWidth[width]["webp"] {
			t.Errorf("width %d formats = %v, want png and webp", width, formatsByWidth[width])
		}
	}

	// The largest ladder rung equals the intrinsic width -- no upscaling,
	// and the ladder always reaches the source's own resolution.
	maxWidth := 0
	for _, variant := range asset.Variants {
		if variant.Width > maxWidth {
			maxWidth = variant.Width
		}
	}
	if maxWidth != 1200 {
		t.Errorf("largest variant width = %d, want 1200 (the source's own intrinsic width)", maxWidth)
	}
}

func TestStageImageVariantsProducesWebPByDefault(t *testing.T) {
	projectDir := t.TempDir()
	distDir := filepath.Join(t.TempDir(), "dist")
	writeTestSourceJPEG(t, filepath.Join(projectDir, "public", "photo.jpg"), 600, 400)
	writeTestSourcePNG(t, filepath.Join(projectDir, "public", "logo.png"), 300, 300)

	assets, err := stageImageVariants(projectDir, distDir)
	if err != nil {
		t.Fatalf("stageImageVariants: %v", err)
	}
	if len(assets) != 2 {
		t.Fatalf("got %d image assets, want 2: %+v", len(assets), assets)
	}

	for _, asset := range assets {
		wantFormat := "png"
		if asset.Source == "/photo.jpg" {
			wantFormat = "jpeg"
		}
		sawNative, sawWebP := false, false
		for _, variant := range asset.Variants {
			switch variant.Format {
			case "webp":
				sawWebP = true
			case wantFormat:
				sawNative = true
			default:
				t.Errorf("source %s variant format = %q, want %q or webp", asset.Source, variant.Format, wantFormat)
			}
		}
		if !sawNative || !sawWebP {
			t.Errorf("source %s formats: native=%v webp=%v, want both", asset.Source, sawNative, sawWebP)
		}
	}
}

// A registered WebP encoder remains a supported override of tqwebp.
func TestStageImageVariantsUsesRegisteredWebPOverride(t *testing.T) {
	stub := imagepipe.EncoderFunc(func(image.Image, imagepipe.EncodeOptions) ([]byte, error) {
		return []byte("stub-webp-bytes"), nil
	})
	if err := imagepipe.RegisterEncoder(imagepipe.FormatWebP, stub); err != nil {
		t.Fatalf("RegisterEncoder: %v", err)
	}
	t.Cleanup(func() { imagepipe.UnregisterEncoder(imagepipe.FormatWebP) })

	projectDir := t.TempDir()
	distDir := filepath.Join(t.TempDir(), "dist")
	writeTestSourcePNG(t, filepath.Join(projectDir, "public", "hero.png"), 320, 200)

	assets, err := stageImageVariants(projectDir, distDir)
	if err != nil {
		t.Fatalf("stageImageVariants: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("got %d image assets, want 1: %+v", len(assets), assets)
	}

	sawWebP, sawPNG := false, false
	for _, variant := range assets[0].Variants {
		switch variant.Format {
		case "webp":
			sawWebP = true
			path := filepath.Join(distDir, "assets", "images", variant.File)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "stub-webp-bytes" {
				t.Fatalf("registered WebP override wrote %q", data)
			}
		case "png":
			sawPNG = true
		}
	}
	if !sawWebP {
		t.Errorf("expected a webp variant, got %+v", assets[0].Variants)
	}
	if !sawPNG {
		t.Errorf("expected the native png variant to stay alongside the registered webp one, got %+v", assets[0].Variants)
	}
}

func TestStageImageVariantsKeepsTransparentPNGNativeOnly(t *testing.T) {
	projectDir := t.TempDir()
	distDir := filepath.Join(t.TempDir(), "dist")
	path := filepath.Join(projectDir, "public", "alpha.png")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 32, 20))
	for y := range 20 {
		for x := range 32 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 7), G: uint8(y * 11), B: 90, A: 128})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	assets, err := stageImageVariants(projectDir, distDir)
	if err != nil {
		t.Fatalf("stageImageVariants: %v", err)
	}
	if len(assets) != 1 || len(assets[0].Variants) == 0 {
		t.Fatalf("assets = %+v, want native PNG variants", assets)
	}
	for _, variant := range assets[0].Variants {
		if variant.Format != "png" {
			t.Fatalf("transparent source produced unsafe %q variant", variant.Format)
		}
	}
}

func TestStageImageVariantsNoPublicDirReturnsNilWithoutError(t *testing.T) {
	projectDir := t.TempDir()
	distDir := filepath.Join(t.TempDir(), "dist")

	assets, err := stageImageVariants(projectDir, distDir)
	if err != nil {
		t.Fatalf("stageImageVariants: %v", err)
	}
	if assets != nil {
		t.Fatalf("expected nil assets with no public/ dir, got %+v", assets)
	}
}

func TestStageImageVariantsSkipsUnusableSourceWithoutFailingBuild(t *testing.T) {
	projectDir := t.TempDir()
	distDir := filepath.Join(t.TempDir(), "dist")
	mustWriteFile(t, filepath.Join(projectDir, "public", "broken.png"), "this is not a png")
	writeTestSourcePNG(t, filepath.Join(projectDir, "public", "good.png"), 400, 300)

	assets, err := stageImageVariants(projectDir, distDir)
	if err != nil {
		t.Fatalf("stageImageVariants should not fail the build over one bad image: %v", err)
	}
	if len(assets) != 1 || assets[0].Source != "/good.png" {
		t.Fatalf("expected only /good.png to produce an asset, got %+v", assets)
	}
}

func TestStageImageVariantsIgnoresNonImageFiles(t *testing.T) {
	projectDir := t.TempDir()
	distDir := filepath.Join(t.TempDir(), "dist")
	mustWriteFile(t, filepath.Join(projectDir, "public", "styles.css"), "body {}\n")
	mustWriteFile(t, filepath.Join(projectDir, "public", "favicon.ico"), "\x00\x00\x01\x00")

	assets, err := stageImageVariants(projectDir, distDir)
	if err != nil {
		t.Fatalf("stageImageVariants: %v", err)
	}
	if len(assets) != 0 {
		t.Fatalf("expected no image assets for non-image files, got %+v", assets)
	}
}
