package scene

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestShaderWarmupLowersMaterialsAndVariants(t *testing.T) {
	// A prebuilt artifact, so the test also runs where js/wasm builds link no
	// Selena compiler.
	artifactData, err := os.ReadFile("testdata/selena/glow.sel.json")
	if err != nil {
		t.Fatal(err)
	}
	file, err := DecodeSelenaArtifact(artifactData)
	if err != nil {
		t.Fatal(err)
	}
	material, _, err := MaterialFromSelenaArtifact(file, SelenaMaterialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	props := Props{ShaderWarmup: &ShaderWarmup{Materials: []WarmupMaterial{
		{Material: material},
		{Material: material, Skinned: true, DoubleSided: true, CastShadow: true},
		{Material: StandardMaterial{Color: "#ff0000", Opacity: Float(0.5)}, Instances: 512},
		{}, // a nil material is skipped, not sent as an empty record
	}}}
	data, err := props.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		ShaderWarmup struct {
			Materials []struct {
				Material    map[string]any `json:"material"`
				Skinned     bool           `json:"skinned"`
				DoubleSided bool           `json:"doubleSided"`
				CastShadow  bool           `json:"castShadow"`
				Instances   int            `json:"instances"`
			} `json:"materials"`
		} `json:"shaderWarmup"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	got := decoded.ShaderWarmup.Materials
	if len(got) != 3 {
		t.Fatalf("got %d warm-up records, want 3: %s", len(got), data)
	}
	selena := got[0].Material
	if selena["shaderBackend"] != "selena" || selena["materialKind"] != "custom" {
		t.Fatalf("selena record lost its backend: %v", selena)
	}
	if v, _ := selena["customFragment"].(string); !strings.Contains(v, "void main") {
		t.Fatalf("selena record has no GLSL fragment: %v", selena["customFragment"])
	}
	if v, _ := selena["customFragmentWGSL"].(string); !strings.Contains(v, "@fragment") {
		t.Fatalf("selena record has no WGSL: %v", selena["customFragmentWGSL"])
	}
	if _, ok := selena["shaderLayout"].(map[string]any); !ok {
		t.Fatalf("selena record has no shader layout")
	}
	if got[0].Skinned || got[0].DoubleSided || got[0].CastShadow || got[0].Instances != 0 {
		t.Fatalf("first record carries variant flags it did not ask for: %+v", got[0])
	}
	if !got[1].Skinned || !got[1].DoubleSided || !got[1].CastShadow {
		t.Fatalf("second record lost its variant flags: %+v", got[1])
	}
	if got[2].Instances != 512 || got[2].Material["color"] != "#ff0000" {
		t.Fatalf("instanced standard record = %+v", got[2])
	}
}

func TestShaderWarmupOmittedWhenEmpty(t *testing.T) {
	for name, warmup := range map[string]*ShaderWarmup{
		"nil":         nil,
		"empty":       {},
		"nil entries": {Materials: []WarmupMaterial{{}}},
	} {
		data, err := Props{ShaderWarmup: warmup}.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "shaderWarmup") {
			t.Fatalf("%s: props carry shaderWarmup: %s", name, data)
		}
	}
}

func TestShaderWarmupGateCountsFinishOnlyWhenReady(t *testing.T) {
	for _, tc := range []struct {
		phase               string
		done, total         int
		wantDone, wantTotal int
	}{
		{"", 0, 0, 0, 1},
		{"compiling", 0, 0, 0, 1},
		{"compiling", 3, 7, 3, 7},
		{"compiling", 7, 7, 6, 7}, // linked but not yet drawn
		{"ready", 7, 7, 7, 7},
		{"ready", 0, 0, 1, 1},
	} {
		d, total := shaderWarmupGateCounts(tc.phase, tc.done, tc.total)
		if d != tc.wantDone || total != tc.wantTotal {
			t.Errorf("shaderWarmupGateCounts(%q, %d, %d) = %d, %d; want %d, %d", tc.phase, tc.done, tc.total, d, total, tc.wantDone, tc.wantTotal)
		}
		if tc.phase != "ready" && d >= total {
			t.Errorf("phase %q finished the gate task", tc.phase)
		}
	}
}
