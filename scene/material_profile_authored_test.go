package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestMaterialProfileEnvelopeRoundTrip verifies that IRMaterial carries all
// authored-shader envelope fields through JSON marshal/unmarshal without loss,
// and that absent fields produce no JSON keys (omitempty).
func TestMaterialProfileEnvelopeRoundTrip(t *testing.T) {
	mat := IRMaterial{
		Name:               "stars",
		Color:              "var(--galaxy-star-color)",
		Opacity:            0.85,
		BlendMode:          "additive",
		CustomVertexWGSL:   "@vertex fn vertexMain() -> @builtin(position) vec4<f32> { return vec4<f32>(0.0,0.0,0.0,1.0); }",
		CustomFragmentWGSL: "@fragment fn fragmentMain() -> @location(0) vec4<f32> { return vec4<f32>(1.0,1.0,0.0,1.0); }",
		CustomVertex:       "void main() { gl_Position = vec4(0.0); }",
		CustomFragment:     "void main() { gl_FragColor = vec4(1.0); }",
		ShaderBackend:      "selena",
		ShaderLayout:       map[string]any{"material": "StarPoints"},
		CustomUniforms:     map[string]any{"brightness": float64(1.5)},
	}

	data, err := json.Marshal(mat)
	if err != nil {
		t.Fatalf("marshal IRMaterial: %v", err)
	}

	var out IRMaterial
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal IRMaterial: %v", err)
	}

	if out.Name != "stars" {
		t.Errorf("Name = %q, want stars", out.Name)
	}
	if out.CustomVertexWGSL == "" {
		t.Error("CustomVertexWGSL lost after round-trip")
	}
	if out.CustomFragmentWGSL == "" {
		t.Error("CustomFragmentWGSL lost after round-trip")
	}
	if out.CustomVertex == "" {
		t.Error("CustomVertex lost after round-trip")
	}
	if out.CustomFragment == "" {
		t.Error("CustomFragment lost after round-trip")
	}
	if out.ShaderBackend != "selena" {
		t.Errorf("ShaderBackend = %q, want selena", out.ShaderBackend)
	}
	if got := out.ShaderLayout["material"]; got != "StarPoints" {
		t.Errorf("ShaderLayout[material] = %v, want StarPoints", got)
	}
	if got := out.CustomUniforms["brightness"]; got != float64(1.5) {
		t.Errorf("CustomUniforms[brightness] = %v, want float64(1.5)", got)
	}
}

// TestMaterialProfileAbsentEnvelopeNoExtra verifies that an IRMaterial without
// any authored-shader fields emits no extra JSON keys for those fields.
func TestMaterialProfileAbsentEnvelopeNoExtra(t *testing.T) {
	mat := IRMaterial{
		Name:      "stars",
		Color:     "#ffffff",
		Opacity:   1.0,
		BlendMode: "additive",
	}
	data, err := json.Marshal(mat)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for _, key := range []string{
		"customVertex", "customFragment",
		"customVertexWGSL", "customFragmentWGSL",
		"shaderBackend", "shaderLayout", "customUniforms",
	} {
		if _, found := raw[key]; found {
			t.Errorf("absent authored field should not emit JSON key %q", key)
		}
	}
}

// TestMaterialProfileShaderLibDedup verifies that applyShaderLib hoists
// duplicate authored WGSL strings from the "materials" collection when the
// same shader source appears in ≥2 named material profile entries.
//
// This mirrors the galaxy use case: all ~21 named material profiles share one
// .sel-compiled WGSL program, so the dedup produces exactly one shaderLib entry.
func TestMaterialProfileShaderLibDedup(t *testing.T) {
	// Build a shader source that exceeds shaderLibThreshold.
	pad := strings.Repeat("// mat profile dedup padding\n", (shaderLibThreshold/28)+1)
	bigWGSL := testWGSLSource + pad

	// Simulate the wire map shape produced by the composable <Material> path:
	// fileprogram.go appends each <Material name=... customVertexWGSL=...> as an
	// entry in scene["materials"].
	const nProfiles = 4
	profiles := make([]any, nProfiles)
	for i := 0; i < nProfiles; i++ {
		profiles[i] = map[string]any{
			"name":               "mat-" + string(rune('a'+i)),
			"color":              "#ffffff",
			"opacity":            float64(0.8),
			"blendMode":          "additive",
			"customVertexWGSL":   bigWGSL,
			"customFragmentWGSL": bigWGSL,
		}
	}
	scene := map[string]any{
		"materials": profiles,
	}

	// applyShaderLib is the function under test.
	result := applyShaderLib(scene)

	// All 4 profiles should have refs, no inline WGSL.
	mats, ok := result["materials"].([]any)
	if !ok {
		t.Fatal("materials key missing from result")
	}
	if len(mats) != nProfiles {
		t.Fatalf("materials length = %d, want %d", len(mats), nProfiles)
	}
	for i, m := range mats {
		mat, ok := m.(map[string]any)
		if !ok {
			t.Fatalf("materials[%d] not a map", i)
		}
		if mat["customVertexWGSL"] != nil {
			t.Errorf("materials[%d] still has inline customVertexWGSL after dedup", i)
		}
		if mat["customFragmentWGSL"] != nil {
			t.Errorf("materials[%d] still has inline customFragmentWGSL after dedup", i)
		}
		if mat["customVertexWGSLRef"] == nil {
			t.Errorf("materials[%d] missing customVertexWGSLRef", i)
		}
		if mat["customFragmentWGSLRef"] == nil {
			t.Errorf("materials[%d] missing customFragmentWGSLRef", i)
		}
	}

	// One shaderLib entry (both vertex and fragment are identical source).
	lib, ok := result["shaderLib"].(map[string]string)
	if !ok {
		t.Fatal("shaderLib missing or wrong type after applyShaderLib")
	}
	if len(lib) == 0 {
		t.Error("shaderLib is empty, expected at least one deduped entry")
	}
	for k := range lib {
		if !strings.HasPrefix(k, "sl:") {
			t.Errorf("shaderLib key %q must start with sl:", k)
		}
	}
}

// TestMaterialProfileShaderLibInflate verifies that inflateShaderLib correctly
// restores customVertexWGSL and customFragmentWGSL in "materials" entries from
// shaderLib refs.
func TestMaterialProfileShaderLibInflate(t *testing.T) {
	const src = "// materials inflate test shader source (long enough if needed)"
	pad := strings.Repeat("// padding\n", (shaderLibThreshold/10)+1)
	fullSrc := src + pad

	libID := shaderLibID(fullSrc)
	scene := map[string]any{
		"shaderLib": map[string]any{
			libID: fullSrc,
		},
		"materials": []any{
			map[string]any{
				"name":                  "stars",
				"customVertexWGSLRef":   libID,
				"customFragmentWGSLRef": libID,
			},
		},
	}

	inflateShaderLib(scene)

	mats, ok := scene["materials"].([]any)
	if !ok || len(mats) == 0 {
		t.Fatal("materials missing after inflate")
	}
	mat, ok := mats[0].(map[string]any)
	if !ok {
		t.Fatal("materials[0] not a map")
	}
	if mat["customVertexWGSL"] != fullSrc {
		t.Errorf("customVertexWGSL not restored; got %v", mat["customVertexWGSL"])
	}
	if mat["customFragmentWGSL"] != fullSrc {
		t.Errorf("customFragmentWGSL not restored; got %v", mat["customFragmentWGSL"])
	}
	// Ref keys must be removed.
	if _, has := mat["customVertexWGSLRef"]; has {
		t.Error("customVertexWGSLRef not removed after inflate")
	}
	if _, has := mat["customFragmentWGSLRef"]; has {
		t.Error("customFragmentWGSLRef not removed after inflate")
	}
	// shaderLib key must be removed.
	if _, has := scene["shaderLib"]; has {
		t.Error("shaderLib key should be removed after inflate")
	}
}
