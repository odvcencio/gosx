package assetpipe

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/assetpipe/texture"
	"m31labs.dev/gosx/assetpipe/variantsel"
	"m31labs.dev/gosx/render/bundle/ktx2"
	"m31labs.dev/gosx/scene/capability"
)

// writeTestPNG builds an sRGB PNG from a pixel function.
func writeTestPNG(t *testing.T, width, height int, at func(x, y int) color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, at(x, y))
		}
	}
	var buf strings.Builder
	if err := png.Encode(&pngWriter{&buf}, img); err != nil {
		t.Fatal(err)
	}
	return []byte(buf.String())
}

// pngWriter adapts a strings.Builder to io.Writer for png.Encode.
type pngWriter struct{ b *strings.Builder }

func (w *pngWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

// TestExecuteBuildsTextureVariants runs the whole stage through Plan and Execute
// and checks that every claim the report makes is true on disk.
func TestExecuteBuildsTextureVariants(t *testing.T) {
	dir := t.TempDir()
	// 1024 by 256, not square. The tier ceiling applies to both edges, so the
	// high and standard steps both land on 1024 and collapse to one file, while
	// the low step stays distinct. A quarter of the pixels of a square source
	// keeps the run fast under the race detector.
	source := writeTestPNG(t, 1024, 256, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x / 4), G: uint8(y / 4), B: uint8((x ^ y) / 4), A: 255}
	})
	mustWriteBytes(t, filepath.Join(dir, "public", "tex", "albedo.png"), source)

	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	before := findAsset(t, report, "public/tex/albedo.png")
	for _, variant := range before.Variants {
		if variant.Exists() {
			t.Fatalf("plan variant %q claims to exist", variant.URI)
		}
	}

	executed, execReport, err := Execute(report, ExecuteOptions{
		Root: dir,
		Only: []string{"texture-transcode-ktx2", "generate-mips"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if execReport.Totals.Executed != 1 {
		t.Fatalf("unexpected totals: %+v (%+v)", execReport.Totals, execReport.Results)
	}
	if execReport.Totals.Skipped != 1 {
		t.Fatalf("generate-mips must report a deliberate skip: %+v", execReport.Results)
	}

	after := findAsset(t, executed, "public/tex/albedo.png")
	built := 0
	for _, variant := range after.Variants {
		if !variant.Exists() {
			// A planned variant of an action that did not run may remain.
			if variant.SourceAction == "texture-transcode-ktx2" {
				t.Fatalf("variant %q stayed a plan after the stage ran", variant.URI)
			}
			continue
		}
		built++
		info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(variant.URI)))
		if err != nil {
			t.Fatalf("variant %q: %v", variant.URI, err)
		}
		if info.Size() != variant.Bytes {
			t.Fatalf("variant %q reports %d bytes, the file holds %d", variant.URI, variant.Bytes, info.Size())
		}
	}
	if built == 0 {
		t.Fatal("the stage built nothing")
	}

	// No block-compressed variant may survive. The plan named bc7, astc, and
	// etc2; none of the three can be built, so none may claim a file.
	for _, variant := range after.Variants {
		for _, marker := range []string{"bc7", "astc", "etc2"} {
			if strings.Contains(variant.Compression, marker) && variant.Exists() {
				t.Fatalf("variant %q claims a built %s file", variant.URI, marker)
			}
		}
	}

	// Two distinct tiers, because 1024 caps at 1024, 1024, and 512. The high
	// and standard ceilings both land on 1024, so the ladder collapses them.
	tiers := map[string]bool{}
	formats := map[string]bool{}
	for _, variant := range after.Variants {
		if variant.Kind != "texture" || !variant.Exists() {
			continue
		}
		tiers[variant.Quality] = true
		formats[variant.Compression] = true
	}
	if !tiers["high"] || !tiers["low"] {
		t.Fatalf("expected a high and a low tier, got %v", tiers)
	}
	if len(tiers) != 2 {
		t.Fatalf("expected two distinct tiers, got %v", tiers)
	}
	if !formats["ktx2-rgba8unorm-srgb-zlib"] {
		t.Fatalf("expected the portable four-channel form, got %v", formats)
	}

	// The container must parse, carry the whole mip chain, and name the tier.
	data, err := os.ReadFile(filepath.Join(dir, "public", "tex", "albedo.high.rgba8unorm-srgb.ktx2"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ktx2.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Width != 1024 || parsed.Height != 256 {
		t.Fatalf("the high tier is %dx%d, want 1024x256", parsed.Width, parsed.Height)
	}
	if len(parsed.Levels) != 11 {
		t.Fatalf("the high tier holds %d levels, want 11", len(parsed.Levels))
	}
	if parsed.Format != ktx2.VkFormatR8G8B8A8SRGB {
		t.Fatalf("the high tier is vkFormat %d, want %d", parsed.Format, ktx2.VkFormatR8G8B8A8SRGB)
	}
	keys, err := ktx2.KeyValues(data)
	if err != nil {
		t.Fatal(err)
	}
	if keys["GoSXtextureTier"] != "high" || keys["GoSXtextureMipSpace"] != "linear" {
		t.Fatalf("container metadata is %v", keys)
	}
	if keys["GoSXtextureColorSpace"] != "srgb" {
		t.Fatalf("container colour space is %q, want srgb", keys["GoSXtextureColorSpace"])
	}

	// The sidecar must record the measurements and the refused targets.
	sidecarBytes, err := os.ReadFile(filepath.Join(dir, "public", "tex", "albedo.textures.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sidecar textureSidecar
	if err := json.Unmarshal(sidecarBytes, &sidecar); err != nil {
		t.Fatal(err)
	}
	if sidecar.Build.SourceBytes != len(source) {
		t.Fatalf("the sidecar reports %d source bytes, the file holds %d", sidecar.Build.SourceBytes, len(source))
	}
	if len(sidecar.RefusedFormats) != 3 {
		t.Fatalf("the sidecar lists %d refused targets, want 3", len(sidecar.RefusedFormats))
	}
	for _, variant := range sidecar.Build.Variants {
		if variant.Ratio <= 0 || variant.Levels == 0 {
			t.Fatalf("sidecar variant %+v is missing a measurement", variant)
		}
	}

	t.Logf("source %d bytes; variants:", len(source))
	for _, result := range execReport.Results {
		if result.Action != "texture-transcode-ktx2" {
			continue
		}
		t.Logf("  %s %s %dms, %d output bytes, ratio %s",
			result.Action, result.Status, result.DurationMS, result.OutputBytes, result.Metrics["outputRatio"])
		for key, value := range result.Metrics {
			if strings.HasPrefix(key, "tier.") {
				t.Logf("    %s: %s", key, value)
			}
		}
	}
}

// TestExecuteSkipsAnUndecodableTexture checks the honest refusal path.
//
// A WebP source has no pure-Go decoder in the standard library. The stage must
// report a skip with a reason, and must not write a variant, because a variant
// that names a file nobody wrote is the failure this package avoids.
func TestExecuteSkipsAnUndecodableTexture(t *testing.T) {
	dir := t.TempDir()
	webp := []byte("RIFF\x24\x00\x00\x00WEBPVP8 \x18\x00\x00\x00")
	mustWriteBytes(t, filepath.Join(dir, "tex", "photo.webp"), webp)

	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	executed, execReport, err := Execute(report, ExecuteOptions{Root: dir, Only: []string{"texture-transcode-ktx2"}})
	if err != nil {
		t.Fatal(err)
	}
	if execReport.Totals.Executed != 0 || execReport.Totals.Skipped != 1 {
		t.Fatalf("unexpected totals: %+v", execReport.Totals)
	}
	if !strings.Contains(execReport.Results[0].Reason, "no pure-Go decoder") {
		t.Fatalf("skip reason is %q", execReport.Results[0].Reason)
	}
	asset := findAsset(t, executed, "tex/photo.webp")
	for _, variant := range asset.Variants {
		if variant.Exists() {
			t.Fatalf("a skipped stage produced a built variant %q", variant.URI)
		}
	}
	// The action keeps its plan status, so a later build can retry.
	for _, action := range asset.Actions {
		if action.Name == "texture-transcode-ktx2" && action.Status != StatusCandidate {
			t.Fatalf("a skipped action changed status to %q", action.Status)
		}
	}
}

// TestExecuteTexturePrunesAnOpaqueGrayscaleMask measures the prune end to end.
func TestExecuteTexturePrunesAnOpaqueGrayscaleMask(t *testing.T) {
	dir := t.TempDir()
	source := writeTestPNG(t, 512, 512, func(x, y int) color.NRGBA {
		v := uint8((x*x + y*y) % 256)
		return color.NRGBA{R: v, G: v, B: v, A: 255}
	})
	mustWriteBytes(t, filepath.Join(dir, "tex", "wall_ao.png"), source)

	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	executed, execReport, err := Execute(report, ExecuteOptions{Root: dir, Only: []string{"texture-transcode-ktx2"}})
	if err != nil {
		t.Fatal(err)
	}
	asset := findAsset(t, executed, "tex/wall_ao.png")
	for _, variant := range asset.Variants {
		if variant.Kind != "texture" || !variant.Exists() {
			continue
		}
		if variant.Compression != "ktx2-r8unorm-zlib" {
			t.Fatalf("an opaque grayscale mask produced %q, want ktx2-r8unorm-zlib", variant.Compression)
		}
		if len(variant.RequiredCapabilities) == 0 {
			t.Fatal("a built variant must name its capabilities")
		}
	}
	// The file name marks it as data, so the stage must not apply the sRGB
	// transfer function. A roughness map decoded as sRGB shades wrong
	// everywhere and no later stage can detect it.
	for _, result := range execReport.Results {
		if result.Action == "texture-transcode-ktx2" && result.Metrics["colorSpace"] != "linear" {
			t.Fatalf("wall_ao.png decoded as %q, want linear", result.Metrics["colorSpace"])
		}
	}
}

// TestSelectVariantRefusesPlannedVariants is the honesty test of the selector.
//
// Plan names bc7, astc, and etc2 variants that no GoSX build step can produce.
// A selector that matched on capabilities alone would hand a WebGPU page the bc7
// URI and produce a 404. The State check is what stops it.
func TestSelectVariantRefusesPlannedVariants(t *testing.T) {
	asset := Asset{
		Path:     "tex/albedo.png",
		Kind:     "texture",
		Variants: textureVariants("tex/albedo.png"),
	}
	// Give the selector every capability a bc7 variant asks for.
	caps := variantsel.NewSet(
		"webgpu:texture-compression-bc",
		"webgpu:texture-compression-astc",
		"webgl2",
		variantsel.ContainerKTX2,
	)
	if variant, ok := SelectVariant(asset, "", caps); ok {
		t.Fatalf("the selector chose the planned variant %q; it has no file", variant.URI)
	}

	// Add one built variant and the selector finds it.
	asset.Variants = append(asset.Variants, Variant{
		URI:                  "tex/albedo.low.rgba8unorm-srgb.ktx2",
		Kind:                 "texture",
		Quality:              "low",
		State:                VariantBuilt,
		Bytes:                1234,
		RequiredCapabilities: []string{string(variantsel.ContainerKTX2)},
	})
	variant, ok := SelectVariant(asset, "texture", caps)
	if !ok || variant.URI != "tex/albedo.low.rgba8unorm-srgb.ktx2" {
		t.Fatalf("the selector chose %+v, want the built low variant", variant)
	}
}

// TestSelectVariantPicksTheHighestAffordableTier checks the ranking.
func TestSelectVariantPicksTheHighestAffordableTier(t *testing.T) {
	asset := Asset{Path: "tex/hero.png", Kind: "texture"}
	for _, tier := range []struct {
		name  string
		bytes int64
	}{{"high", 900000}, {"standard", 300000}, {"low", 90000}} {
		asset.Variants = append(asset.Variants, Variant{
			URI:     "tex/hero." + tier.name + ".rgba8unorm-srgb.ktx2",
			Kind:    "texture",
			Quality: tier.name,
			State:   VariantBuilt,
			Bytes:   tier.bytes,
			RequiredCapabilities: variantsel.Strings(append([]variantsel.Token{
				variantsel.ContainerKTX2, variantsel.FormatRGBA8UnormSRGB,
			}, variantsel.TierTokens(tier.name)...)...),
		})
	}
	base := variantsel.FromBackendCaps(capability.BackendCaps{
		Capable: []capability.Backend{capability.BackendWebGPU, capability.BackendWebGL},
	})

	// A high budget takes the high tier.
	high := cloneSet(base)
	high.Add(variantsel.BudgetHigh)
	if variant, ok := SelectVariant(asset, "texture", high); !ok || variant.Quality != "high" {
		t.Fatalf("a high budget chose %+v", variant)
	}
	// A standard budget cannot take the high tier, so it takes the standard one.
	standard := cloneSet(base)
	standard.Add(variantsel.BudgetStandard)
	if variant, ok := SelectVariant(asset, "texture", standard); !ok || variant.Quality != "standard" {
		t.Fatalf("a standard budget chose %+v", variant)
	}
	// A data-saver request also lands on the standard tier, because the tier
	// ladder only gates the high step. Gating the low step too would leave a
	// data-saver page with no texture at all.
	low := cloneSet(base)
	low.Add(variantsel.BudgetLow)
	if variant, ok := SelectVariant(asset, "texture", low); !ok || variant.Quality != "standard" {
		t.Fatalf("a low budget chose %+v", variant)
	}
}

// TestSelectVariantPrefersTheSmallerFormAtOneTier checks the second ranking key.
//
// A WebGL2-only page can upload both the four-channel and the three-channel
// form of an opaque texture. It should take the smaller one. A page that may
// still fall back to WebGL2 from WebGPU must take the four-channel form, because
// WebGPU has no rgb8unorm at all.
func TestSelectVariantPrefersTheSmallerFormAtOneTier(t *testing.T) {
	asset := Asset{Path: "tex/wall.png", Kind: "texture", Variants: []Variant{
		{
			URI: "tex/wall.low.rgba8unorm-srgb.ktx2", Kind: "texture", Quality: "low",
			State: VariantBuilt, Bytes: 100000,
			RequiredCapabilities: variantsel.Strings(variantsel.ContainerKTX2, variantsel.FormatRGBA8UnormSRGB),
		},
		{
			URI: "tex/wall.low.rgb8unorm-srgb.ktx2", Kind: "texture", Quality: "low",
			State: VariantBuilt, Bytes: 78000,
			RequiredCapabilities: variantsel.Strings(variantsel.ContainerKTX2, variantsel.FormatRGB8UnormSRGB),
		},
	}}

	webglOnly := variantsel.FromBackendCaps(capability.BackendCaps{
		Capable: []capability.Backend{capability.BackendWebGL},
	})
	variant, ok := SelectVariant(asset, "texture", webglOnly)
	if !ok || variant.URI != "tex/wall.low.rgb8unorm-srgb.ktx2" {
		t.Fatalf("a WebGL2-only page chose %+v, want the three-channel form", variant)
	}

	bothGPU := variantsel.FromBackendCaps(capability.BackendCaps{
		Capable: []capability.Backend{capability.BackendWebGPU, capability.BackendWebGL},
	})
	variant, ok = SelectVariant(asset, "texture", bothGPU)
	if !ok || variant.URI != "tex/wall.low.rgba8unorm-srgb.ktx2" {
		t.Fatalf("a page that may fall back chose %+v, want the four-channel form", variant)
	}

	canvasOnly := variantsel.FromBackendCaps(capability.BackendCaps{
		Capable: []capability.Backend{capability.BackendCanvas2D},
	})
	if variant, ok := SelectVariant(asset, "texture", canvasOnly); ok {
		t.Fatalf("a Canvas2D-only page chose %+v; it uploads no GPU texture", variant)
	}
}

func cloneSet(in variantsel.Set) variantsel.Set {
	out := variantsel.Set{}
	for token := range in {
		out.Add(token)
	}
	return out
}

// TestBuildVariantManifestOnlyPublishesBuiltFiles is the manifest honesty test.
func TestBuildVariantManifestOnlyPublishesBuiltFiles(t *testing.T) {
	dir := t.TempDir()
	mustWriteBytes(t, filepath.Join(dir, "tex", "albedo.png"), writeTestPNG(t, 256, 256, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x), G: uint8(y), B: 40, A: 255}
	}))
	mustWriteBytes(t, filepath.Join(dir, "models", "grid.glb"), buildGridGLB(t, 4))

	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// The plan alone must publish nothing at all.
	planned := BuildVariantManifest(report)
	if len(planned.Assets) != 0 {
		t.Fatalf("a plan produced %d manifest assets, want 0", len(planned.Assets))
	}

	executed, _, err := Execute(report, ExecuteOptions{Root: dir, Only: []string{"texture-transcode-ktx2"}})
	if err != nil {
		t.Fatal(err)
	}
	manifest := BuildVariantManifest(executed)
	if len(manifest.Assets) != 1 {
		t.Fatalf("the manifest holds %d assets, want 1", len(manifest.Assets))
	}
	entry := manifest.Assets[0]
	if entry.Path != "tex/albedo.png" {
		t.Fatalf("manifest asset path is %q", entry.Path)
	}
	if entry.MetadataURI != "tex/albedo.textures.json" {
		t.Fatalf("manifest metadata uri is %q", entry.MetadataURI)
	}
	for _, variant := range entry.Variants {
		if variant.Kind == "texture-metadata" {
			t.Fatal("a sidecar must not appear as an uploadable variant")
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(variant.URI))); err != nil {
			t.Fatalf("manifest names %q, which does not exist: %v", variant.URI, err)
		}
	}
	if len(manifest.Vocabulary) == 0 {
		t.Fatal("the manifest must publish its capability vocabulary")
	}
	for _, token := range manifest.Vocabulary {
		if !strings.Contains(token, ":") {
			t.Fatalf("vocabulary token %q is not namespaced", token)
		}
	}

	// SelectFromManifest must agree with SelectVariant on the same inputs.
	caps := variantsel.FromBackendCaps(capability.BackendCaps{
		Capable: []capability.Backend{capability.BackendWebGPU, capability.BackendWebGL},
	})
	fromManifest, okManifest := SelectFromManifest(manifest, "tex/albedo.png", "texture", caps)
	fromAsset, okAsset := SelectVariant(findAsset(t, executed, "tex/albedo.png"), "texture", caps)
	if okManifest != okAsset {
		t.Fatalf("the two selectors disagree on availability: %v against %v", okManifest, okAsset)
	}
	if fromManifest.URI != fromAsset.URI {
		t.Fatalf("the two selectors chose %q and %q", fromManifest.URI, fromAsset.URI)
	}

	// The manifest must round trip as JSON.
	data, err := MarshalVariantManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var reloaded VariantManifest
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Assets) != len(manifest.Assets) {
		t.Fatal("the manifest did not round trip")
	}
}

// TestTextureSupportedActions checks the registration reached the table.
func TestTextureSupportedActions(t *testing.T) {
	actions := map[string]bool{}
	for _, action := range SupportedActions() {
		actions[action] = true
	}
	for _, want := range []string{"texture-transcode-ktx2", "generate-mips"} {
		if !actions[want] {
			t.Errorf("SupportedActions is missing %q", want)
		}
	}
}

// TestBuildTextureIsSideEffectFree checks the exported entry point writes nothing.
func TestBuildTextureIsSideEffectFree(t *testing.T) {
	dir := t.TempDir()
	source := writeTestPNG(t, 64, 64, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x * 4), G: 0, B: 0, A: 255}
	})
	result, variants, err := BuildTexture("tex/a.png", source, TextureOptions{
		Tiers: []texture.Tier{{Name: "low", MaxEdge: 32}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(variants) != len(result.Variants) || len(variants) == 0 {
		t.Fatalf("BuildTexture returned %d variants for %d plans", len(variants), len(result.Variants))
	}
	for i, variant := range variants {
		if variant.Bytes != int64(result.Variants[i].Bytes) {
			t.Fatalf("variant %d reports %d bytes, the plan holds %d", i, variant.Bytes, result.Variants[i].Bytes)
		}
	}
	if files := listFiles(t, dir); len(files) != 0 {
		t.Fatalf("BuildTexture wrote %v", files)
	}
}
