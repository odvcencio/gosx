package texture

import (
	"image/color"
	"testing"

	"m31labs.dev/gosx/render/bundle/ktx2"
)

// TestBuildEmitsDistinctTiers checks the ladder collapses duplicates.
//
// A 2048 pixel source resolves to three different sizes, so three tiers are
// three files. A 256 pixel source resolves to 256 at every ceiling, so writing
// three identical files under three names would triple the build output and the
// manifest for nothing.
func TestBuildEmitsDistinctTiers(t *testing.T) {
	big := srgbPNG(t, 512, 512, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x), G: uint8(y), B: 128, A: 255}
	})
	result, err := Build(big, BuildOptions{ColorSpace: SRGB, Supercompress: true, Source: "big.png"})
	if err != nil {
		t.Fatal(err)
	}
	sizes := map[int]bool{}
	for _, variant := range result.Variants {
		sizes[variant.Width] = true
	}
	// 512 caps at 512, 1024, and 2048 all resolve to 512, so only the low
	// tier at 512 differs... in fact all three resolve to 512 here.
	if len(sizes) != 1 || !sizes[512] {
		t.Fatalf("a 512 source produced sizes %v, want only 512", sizes)
	}
	if len(result.Variants) != 1 {
		t.Fatalf("a 512 source produced %d variants, want 1", len(result.Variants))
	}

	small := srgbPNG(t, 2048, 64, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x), G: uint8(y), B: 200, A: 255}
	})
	result, err = Build(small, BuildOptions{ColorSpace: SRGB, Supercompress: true, Source: "wide.png"})
	if err != nil {
		t.Fatal(err)
	}
	widths := map[int]bool{}
	for _, variant := range result.Variants {
		widths[variant.Width] = true
	}
	if len(widths) != 3 || !widths[2048] || !widths[1024] || !widths[512] {
		t.Fatalf("a 2048 wide source produced widths %v, want 2048, 1024, and 512", widths)
	}
	// Variants come back largest first, which is the order a selector wants.
	if result.Variants[0].Width < result.Variants[len(result.Variants)-1].Width {
		t.Fatalf("variants are not ordered largest first: %+v", widths)
	}
}

// TestBuildTiersCarryTheCorrectTierName checks the label a selector reads.
// The source is 2048 by 256 rather than square. The ceiling applies to both
// edges, so the three tiers still resolve to three distinct sizes, at an eighth
// of the pixels and therefore an eighth of the test time under the race
// detector.
func TestBuildTiersCarryTheCorrectTierName(t *testing.T) {
	data := srgbPNG(t, 2048, 256, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x), G: uint8(y), B: 90, A: 255}
	})
	result, err := Build(data, BuildOptions{ColorSpace: SRGB, Supercompress: true, Source: "hero.png"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]int{"high": {2048, 256}, "standard": {1024, 256}, "low": {512, 256}}
	seen := map[string]bool{}
	for _, variant := range result.Variants {
		size, ok := want[variant.Tier]
		if !ok {
			t.Fatalf("unknown tier %q", variant.Tier)
		}
		if variant.Width != size[0] || variant.Height != size[1] {
			t.Errorf("tier %q is %dx%d, want %dx%d", variant.Tier, variant.Width, variant.Height, size[0], size[1])
		}
		if variant.Levels != MipLevelCount(variant.Width, variant.Height) {
			t.Errorf("tier %q holds %d levels, want %d", variant.Tier, variant.Levels, MipLevelCount(variant.Width, variant.Height))
		}
		seen[variant.Tier] = true
	}
	for tier := range want {
		if !seen[tier] {
			t.Errorf("tier %q is missing", tier)
		}
	}
}

// TestBuildPrunesAnOpaqueGrayscaleToR8 measures the prune.
//
// A grayscale mask with an unused alpha channel needs one byte per texel, not
// four. Both WebGPU and WebGL2 sample r8unorm, so the saving is portable, and
// it applies to the GPU allocation, not only to the file.
func TestBuildPrunesAnOpaqueGrayscaleToR8(t *testing.T) {
	data := srgbPNG(t, 256, 256, func(x, y int) color.NRGBA {
		v := uint8((x + y) % 256)
		return color.NRGBA{R: v, G: v, B: v, A: 255}
	})
	result, err := Build(data, BuildOptions{
		ColorSpace:         Linear,
		Supercompress:      true,
		PruneConstantAlpha: true,
		Source:             "mask.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Grayscale || !result.Alpha.Opaque {
		t.Fatalf("the source did not classify as opaque grayscale: %+v", result)
	}
	if result.PrunedChannel == "" {
		t.Fatal("the prune did not record which channels it dropped")
	}
	for _, variant := range result.Variants {
		if variant.Channels != 1 || variant.Format != "r8unorm" {
			t.Fatalf("variant %+v did not prune to r8unorm", variant)
		}
		if variant.VkFormat != ktx2.VkFormatR8Unorm {
			t.Fatalf("variant reports vkFormat %d, want %d", variant.VkFormat, ktx2.VkFormatR8Unorm)
		}
	}

	// Measure the prune against the same build with the prune turned off.
	kept, err := Build(data, BuildOptions{ColorSpace: Linear, Supercompress: true, Source: "mask.png"})
	if err != nil {
		t.Fatal(err)
	}
	if kept.OutputBytes <= result.OutputBytes {
		t.Fatalf("the prune did not save bytes: %d pruned against %d kept", result.OutputBytes, kept.OutputBytes)
	}
	t.Logf("r8unorm prune: %d bytes against %d, ratio %.4f",
		result.OutputBytes, kept.OutputBytes, float64(result.OutputBytes)/float64(kept.OutputBytes))
}

// TestBuildEmitsTheThreeChannelVariantForAnOpaqueColourTexture checks the second
// capability-gated output.
//
// An opaque colour texture needs no alpha channel. WebGL2 can upload the
// three-channel form and WebGPU cannot, so the build emits both and the selector
// decides. The pair is the smallest complete demonstration of the variant
// system: two files, one asset, one honest capability gate.
func TestBuildEmitsTheThreeChannelVariantForAnOpaqueColourTexture(t *testing.T) {
	data := srgbPNG(t, 256, 256, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255}
	})
	result, err := Build(data, BuildOptions{
		ColorSpace:         SRGB,
		Supercompress:      true,
		PruneConstantAlpha: true,
		Source:             "albedo.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	var portable, pruned *VariantPlan
	for i := range result.Variants {
		switch result.Variants[i].Channels {
		case 4:
			portable = &result.Variants[i]
		case 3:
			pruned = &result.Variants[i]
		}
	}
	if portable == nil || pruned == nil {
		t.Fatalf("expected a four-channel and a three-channel variant, got %+v", result.Variants)
	}
	if portable.Format != "rgba8unorm-srgb" || pruned.Format != "rgb8unorm-srgb" {
		t.Fatalf("formats are %q and %q", portable.Format, pruned.Format)
	}
	if pruned.Bytes >= portable.Bytes {
		t.Fatalf("the pruned variant is %d bytes against %d; the prune saved nothing",
			pruned.Bytes, portable.Bytes)
	}
	t.Logf("alpha prune with zlib: %d bytes against %d, ratio %.4f",
		pruned.Bytes, portable.Bytes, float64(pruned.Bytes)/float64(portable.Bytes))

	// PortableOnly must suppress the three-channel form.
	only, err := Build(data, BuildOptions{
		ColorSpace:         SRGB,
		Supercompress:      true,
		PruneConstantAlpha: true,
		EmitPortableOnly:   true,
		Source:             "albedo.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range only.Variants {
		if variant.Channels == 3 {
			t.Fatal("EmitPortableOnly must suppress the three-channel variant")
		}
	}
}

// TestBuildKeepsAlphaWhenTheTextureUsesIt checks the negative case.
func TestBuildKeepsAlphaWhenTheTextureUsesIt(t *testing.T) {
	data := srgbPNG(t, 128, 128, func(x, y int) color.NRGBA {
		return color.NRGBA{R: 255, G: 128, B: 0, A: uint8(x * 2)}
	})
	result, err := Build(data, BuildOptions{
		ColorSpace:         SRGB,
		Supercompress:      true,
		PruneConstantAlpha: true,
		Source:             "leaf.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AlphaMode != "blend" {
		t.Fatalf("alpha mode is %q, want blend", result.AlphaMode)
	}
	if result.PrunedChannel != "" {
		t.Fatalf("the build pruned %q from a texture that uses alpha", result.PrunedChannel)
	}
	for _, variant := range result.Variants {
		if variant.Channels != 4 {
			t.Fatalf("variant %+v dropped a channel a blended texture needs", variant)
		}
	}
}

// TestBuildReportsRatiosAndTimings checks the measurement contract.
func TestBuildReportsRatiosAndTimings(t *testing.T) {
	data := srgbPNG(t, 512, 512, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x), G: uint8(y), B: uint8(x + y), A: 255}
	})
	result, err := Build(data, BuildOptions{ColorSpace: SRGB, Supercompress: true, Source: "measure.png"})
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceBytes != len(data) || result.OutputBytes == 0 {
		t.Fatalf("byte counts are %d in and %d out", result.SourceBytes, result.OutputBytes)
	}
	for _, variant := range result.Variants {
		if variant.Ratio <= 0 {
			t.Fatalf("variant %s has no ratio", variant.Format)
		}
		if variant.Bytes != len(variant.Data) {
			t.Fatalf("variant %s reports %d bytes and carries %d", variant.Format, variant.Bytes, len(variant.Data))
		}
	}
	if result.DurationMS < 0 || result.DecodeMS < 0 {
		t.Fatal("timings must not be negative")
	}
}

// TestColorSpaceForName pins the file-name heuristic, including the cases it
// deliberately errs toward Linear.
func TestColorSpaceForName(t *testing.T) {
	linear := []string{
		"tex/wall_normal.png", "brick-NRM.png", "metal_roughness.jpg",
		"body_metalness.png", "wood_orm.png", "plate_ao.png",
		"terrain_height.png", "mask.png", "cloth_specular.png",
	}
	srgb := []string{
		"tex/wall_albedo.png", "hero_basecolor.png", "flame_emissive.png",
		"logo.png", "photo.jpg",
	}
	for _, name := range linear {
		if got := ColorSpaceForName(name); got != Linear {
			t.Errorf("ColorSpaceForName(%q) = %s, want linear", name, got)
		}
	}
	for _, name := range srgb {
		if got := ColorSpaceForName(name); got != SRGB {
			t.Errorf("ColorSpaceForName(%q) = %s, want srgb", name, got)
		}
	}
}

// TestBuildDropsTheThreeChannelVariantWhenItIsLarger records the measured, and
// counter-intuitive, case.
//
// Dropping a constant alpha channel removes a quarter of the plain payload. It
// does not always remove a quarter of the compressed payload. Zlib codes a byte
// that repeats every four bytes almost for free, and it codes a three-byte
// stride worse than a four-byte one, so on some images the three-channel
// container comes out LARGER. The builder measures and keeps the smaller of the
// two, and it records the rejected size so the decision is auditable.
//
// The input below is a smooth four-bit gradient, which is exactly the shape that
// makes the four-byte stride compress better.
func TestBuildDropsTheThreeChannelVariantWhenItIsLarger(t *testing.T) {
	data := srgbPNG(t, 512, 512, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x / 4), G: uint8(y / 4), B: uint8((x ^ y) / 4), A: 255}
	})
	result, err := Build(data, BuildOptions{
		ColorSpace:         SRGB,
		Supercompress:      true,
		PruneConstantAlpha: true,
		Source:             "gradient.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range result.Variants {
		if variant.Channels == 3 {
			t.Fatalf("the builder kept a three-channel variant of %d bytes against the "+
				"four-channel form; check the comparison", variant.Bytes)
		}
	}
	rejected := false
	for _, variant := range result.Variants {
		if variant.AlphaPruneRejected {
			rejected = true
			if variant.AlphaPruneBytes < variant.Bytes {
				t.Fatalf("the rejected form is %d bytes against %d; it should have been kept",
					variant.AlphaPruneBytes, variant.Bytes)
			}
			t.Logf("tier %s: kept rgba8 at %d bytes, rejected rgb8 at %d bytes",
				variant.Tier, variant.Bytes, variant.AlphaPruneBytes)
		}
	}
	if !rejected {
		t.Fatal("this input should reject the three-channel form on at least one tier")
	}
}
