package hydrate

import (
	"encoding/json"
	"testing"

	"m31labs.dev/gosx/assetpipe"
)

// buildReport returns a report with one texture asset that has a built variant,
// a planned variant, and a sidecar.
//
// The shapes match what the pipeline really produces: Execute sets State to
// "built" after it wrote the file, and leaves a planned variant alone.
func buildReport() assetpipe.Report {
	return assetpipe.Report{
		Assets: []assetpipe.Asset{
			{
				Path: "assets/wood.png",
				Kind: "texture",
				Variants: []assetpipe.Variant{
					{
						URI:                  "assets/wood.bc7.ktx2",
						Kind:                 "texture",
						Quality:              "high",
						Bytes:                42529,
						State:                assetpipe.VariantBuilt,
						RequiredCapabilities: []string{"container:ktx2", "texture-format:bc7-rgba-unorm-srgb", "device-feature:texture-compression-bc"},
					},
					{
						URI:                  "assets/wood.rgba8.ktx2",
						Kind:                 "texture",
						Quality:              "standard",
						Bytes:                502374,
						State:                assetpipe.VariantBuilt,
						RequiredCapabilities: []string{"container:ktx2", "texture-format:rgba8unorm-srgb"},
					},
					{
						// A planned variant names work the pipeline did not do.
						URI:                  "assets/wood.astc.ktx2",
						Kind:                 "texture",
						Quality:              "high",
						RequiredCapabilities: []string{"device-feature:texture-compression-astc"},
					},
					{
						URI:  "assets/wood.texture.json",
						Kind: "texture-metadata",
					},
				},
			},
		},
	}
}

// TestSetTextureVariantsCarriesOnlyBuiltTextures is the load-bearing rule.
//
// A selector that names an unbuilt file produces a runtime 404, and the browser
// has no way to tell that from a network fault. The Go side already refuses a
// planned variant; the page manifest must not reintroduce one.
func TestSetTextureVariantsCarriesOnlyBuiltTextures(t *testing.T) {
	manifest := NewManifest()
	manifest.SetTextureVariants(assetpipe.BuildVariantManifest(buildReport()))

	refs := manifest.TextureVariants["assets/wood.png"]
	if len(refs) != 2 {
		t.Fatalf("the manifest carries %d variants, want 2", len(refs))
	}
	for _, ref := range refs {
		if ref.URI == "assets/wood.astc.ktx2" {
			t.Error("a planned variant reached the page manifest, which would 404 in the browser")
		}
		if ref.URI == "assets/wood.texture.json" {
			t.Error("a metadata sidecar reached the texture table, which holds no pixels")
		}
	}
	if refs[0].URI != "assets/wood.bc7.ktx2" {
		t.Errorf("the first variant is %q, want the high tier first", refs[0].URI)
	}
	if refs[0].Bytes != 42529 || refs[0].Quality != "high" {
		t.Errorf("the first variant carries bytes %d and quality %q", refs[0].Bytes, refs[0].Quality)
	}
	if len(refs[0].RequiredCapabilities) != 3 {
		t.Errorf("the first variant carries %d capabilities, want 3", len(refs[0].RequiredCapabilities))
	}
}

// TestSetTextureVariantsSkipsNonTextureKinds proves the kind filter.
//
// A model, an environment map and an audio track need different consumers. A
// texture binding handed one of them would upload something that holds no
// pixels.
func TestSetTextureVariantsSkipsNonTextureKinds(t *testing.T) {
	report := assetpipe.Report{
		Assets: []assetpipe.Asset{
			{
				Path: "assets/city.glb",
				Kind: "model",
				Variants: []assetpipe.Variant{
					{URI: "assets/city.draco.glb", Kind: "model", State: assetpipe.VariantBuilt},
				},
			},
			{
				Path: "assets/room.hdr",
				Kind: "environment",
				Variants: []assetpipe.Variant{
					{URI: "assets/room.env.ktx2", Kind: "environment", State: assetpipe.VariantBuilt},
				},
			},
		},
	}
	manifest := NewManifest()
	manifest.SetTextureVariants(assetpipe.BuildVariantManifest(report))
	if manifest.TextureVariants != nil {
		t.Errorf("the texture table holds %v, want nothing", manifest.TextureVariants)
	}
}

// TestTextureVariantsSurviveTheJSONRoundTrip proves the client can read what the
// server wrote. The browser reads the manifest through loadManifest, so a field
// with the wrong JSON name is invisible there and every Go test still passes.
func TestTextureVariantsSurviveTheJSONRoundTrip(t *testing.T) {
	manifest := NewManifest()
	manifest.SetTextureVariants(assetpipe.BuildVariantManifest(buildReport()))
	data, err := manifest.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	table, ok := raw["textureVariants"].(map[string]any)
	if !ok {
		t.Fatalf("the JSON has no textureVariants object, it has keys %v", keysOf(raw))
	}
	entries, ok := table["assets/wood.png"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("the asset row is %v", table["assets/wood.png"])
	}
	first, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("the first variant is %v", entries[0])
	}
	for _, field := range []string{"uri", "quality", "bytes", "requiredCapabilities"} {
		if _, present := first[field]; !present {
			t.Errorf("the variant JSON has no %q field, it has keys %v", field, keysOf(first))
		}
	}

	// An empty table must stay absent, so a page with no processed texture pays
	// no manifest bytes at all.
	empty := NewManifest()
	emptyData, err := empty.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var emptyRaw map[string]any
	if err := json.Unmarshal(emptyData, &emptyRaw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, present := emptyRaw["textureVariants"]; present {
		t.Error("a manifest with no texture variants still wrote the textureVariants key")
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}
