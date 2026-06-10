package engine

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRenderMaterialCustomShaderFieldsRoundTrip(t *testing.T) {
	source := RenderMaterial{
		Kind:               "custom",
		Color:              "#f5c76b",
		Roughness:          0.32,
		Metalness:          0.8,
		Clearcoat:          0.35,
		Sheen:              0.2,
		Transmission:       0.12,
		Iridescence:        0.18,
		Anisotropy:         -0.25,
		NormalMap:          "/normal.webp",
		RoughnessMap:       "/roughness.webp",
		MetalnessMap:       "/metalness.webp",
		EmissiveMap:        "/emissive.webp",
		CustomVertexWGSL:   "fn gosx_vertex() {}",
		CustomFragmentWGSL: "fn gosx_fragment() -> vec4f { return vec4f(1.0); }",
		CustomUniforms: map[string]any{
			"pulse": 0.75,
		},
	}
	payload, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded RenderMaterial
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.CustomVertexWGSL != source.CustomVertexWGSL {
		t.Fatalf("CustomVertexWGSL = %q", decoded.CustomVertexWGSL)
	}
	if decoded.CustomFragmentWGSL != source.CustomFragmentWGSL {
		t.Fatalf("CustomFragmentWGSL = %q", decoded.CustomFragmentWGSL)
	}
	if decoded.Roughness != 0.32 || decoded.Metalness != 0.8 || decoded.Clearcoat != 0.35 || decoded.Sheen != 0.2 || decoded.Transmission != 0.12 || decoded.Iridescence != 0.18 || decoded.Anisotropy != -0.25 || decoded.NormalMap != "/normal.webp" || decoded.RoughnessMap != "/roughness.webp" || decoded.MetalnessMap != "/metalness.webp" || decoded.EmissiveMap != "/emissive.webp" {
		t.Fatalf("PBR fields = %#v", decoded)
	}
	if decoded.CustomUniforms["pulse"] != 0.75 {
		t.Fatalf("CustomUniforms = %#v", decoded.CustomUniforms)
	}
}

func TestRenderBundleDiagnosticsRoundTrip(t *testing.T) {
	source := RenderBundle{
		Animations: []RenderAnimation{{
			Name:     "pulse",
			Duration: 1.5,
			Channels: []RenderAnimationChannel{{
				TargetID:      "hero",
				Property:      "rotationY",
				Times:         []float64{0, 1.5},
				Values:        []float64{0, 3.14},
				Interpolation: "LINEAR",
			}},
		}},
		PostEffects: []RenderPostEffect{{Kind: "toneMapping", Mode: "aces", Intensity: 0.5, Params: map[string]float64{"exposure": 1.2}}},
		Diagnostics: []RenderDiagnostic{{
			Severity: "info",
			Code:     "native-postfx-unsupported",
			Backend:  "enginevm",
			Target:   "dof",
			Message:  "unsupported native post effect",
		}},
	}
	payload, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded RenderBundle
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(decoded.PostEffects) != 1 || decoded.PostEffects[0].Mode != "aces" || decoded.PostEffects[0].Params["exposure"] != 1.2 {
		t.Fatalf("PostEffects = %#v", decoded.PostEffects)
	}
	if len(decoded.Animations) != 1 || decoded.Animations[0].Channels[0].TargetID != "hero" || decoded.Animations[0].Channels[0].Values[1] != 3.14 {
		t.Fatalf("Animations = %#v", decoded.Animations)
	}
	if len(decoded.Diagnostics) != 1 {
		t.Fatalf("Diagnostics = %#v", decoded.Diagnostics)
	}
	if decoded.Diagnostics[0].Code != "native-postfx-unsupported" || decoded.Diagnostics[0].Target != "dof" {
		t.Fatalf("diagnostic = %#v", decoded.Diagnostics[0])
	}
}

// TestRenderComputeParticlesKernelFieldsRoundTrip verifies that the three
// Elio/custom kernel override fields on RenderComputeParticles survive a
// JSON marshal→unmarshal cycle (the path used by the WASM bridge).
func TestRenderComputeParticlesKernelFieldsRoundTrip(t *testing.T) {
	const wgsl = "@compute @workgroup_size(64) fn update() {}"
	source := RenderBundle{
		ComputeParticles: []RenderComputeParticles{
			{
				ID:             "galaxy",
				Count:          1000,
				ComputeWGSL:    wgsl,
				ComputeEntry:   "update",
				ComputeBackend: "elio",
			},
		},
	}
	payload, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded RenderBundle
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(decoded.ComputeParticles) != 1 {
		t.Fatalf("ComputeParticles length = %d, want 1", len(decoded.ComputeParticles))
	}
	cp := decoded.ComputeParticles[0]
	if cp.ComputeWGSL != wgsl {
		t.Errorf("ComputeWGSL = %q, want %q", cp.ComputeWGSL, wgsl)
	}
	if cp.ComputeEntry != "update" {
		t.Errorf("ComputeEntry = %q, want 'update'", cp.ComputeEntry)
	}
	if cp.ComputeBackend != "elio" {
		t.Errorf("ComputeBackend = %q, want 'elio'", cp.ComputeBackend)
	}
	// Zero-value path: absent fields must not be present when not set.
	if !bytes.Contains(payload, []byte(`"computeWGSL"`)) {
		t.Errorf("expected computeWGSL in JSON payload")
	}
}

// TestRenderComputeParticlesKernelFieldsAbsentWhenEmpty confirms omitempty:
// when the kernel fields are zero-valued they are not written to JSON.
func TestRenderComputeParticlesKernelFieldsAbsentWhenEmpty(t *testing.T) {
	source := RenderBundle{
		ComputeParticles: []RenderComputeParticles{
			{ID: "plain", Count: 10},
		},
	}
	payload, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if bytes.Contains(payload, []byte("computeWGSL")) {
		t.Errorf("computeWGSL must be absent when empty; payload: %s", payload)
	}
	if bytes.Contains(payload, []byte("computeEntry")) {
		t.Errorf("computeEntry must be absent when empty; payload: %s", payload)
	}
	if bytes.Contains(payload, []byte("computeBackend")) {
		t.Errorf("computeBackend must be absent when empty; payload: %s", payload)
	}
}

func TestRenderBundleHTMLRoundTrip(t *testing.T) {
	source := RenderBundle{
		HTML: []RenderHTML{{
			ID:            "page",
			Markup:        "<b>x</b>",
			X:             0,
			Y:             0,
			Width:         1280,
			Height:        720,
			PointerEvents: "auto",
		}},
	}
	payload, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Contains(payload, []byte(`"html"`)) {
		t.Fatalf("marshaled JSON missing key %q: %s", "html", payload)
	}
	var decoded RenderBundle
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(decoded.HTML) != 1 {
		t.Fatalf("HTML length = %d, want 1", len(decoded.HTML))
	}
	h := decoded.HTML[0]
	if h.ID != "page" || h.Markup != "<b>x</b>" || h.Width != 1280 || h.Height != 720 || h.PointerEvents != "auto" {
		t.Fatalf("HTML[0] = %#v", h)
	}
}
