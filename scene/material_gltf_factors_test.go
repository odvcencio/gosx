package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

func gltfFactorColor(value [3]float64) *[3]float64 { return &value }

func TestImportedPBRFactorsSurviveSceneIRMaterialLowering(t *testing.T) {
	var record ObjectIR
	applyMaterialProps(&record, map[string]any{
		"materialKind":      "standard",
		"emissive":          float64(4),
		"emissiveColor":     []float64{0.8, 0.15, 0.05},
		"normalScale":       float64(0),
		"occlusionStrength": float64(0),
	})

	wire, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal material record: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(wire, &got); err != nil {
		t.Fatalf("unmarshal material record: %v", err)
	}
	if got["emissive"] != float64(4) {
		t.Fatalf("emissive strength = %v, want 4", got["emissive"])
	}
	if color, ok := got["emissiveColor"].([]any); !ok || len(color) != 3 || color[0] != 0.8 || color[1] != 0.15 || color[2] != 0.05 {
		t.Fatalf("emissiveColor = %v, want [0.8 0.15 0.05]", got["emissiveColor"])
	}
	if got["normalScale"] != float64(0) {
		t.Fatalf("normalScale = %v, want explicit zero", got["normalScale"])
	}
	if got["occlusionStrength"] != float64(0) {
		t.Fatalf("occlusionStrength = %v, want explicit zero", got["occlusionStrength"])
	}
}

func TestStandardMaterialGLTFFactorsLowerAcrossSceneIRRecords(t *testing.T) {
	color := gltfFactorColor([3]float64{0.8, 0.15, 0.05})
	material := StandardMaterial{
		Color:             "#ffffff",
		Emissive:          4,
		EmissiveColor:     color,
		NormalScale:       Float(0),
		OcclusionStrength: Float(0),
	}
	graph := NewGraph(
		Mesh{ID: "mesh", Geometry: CubeGeometry{Size: 1}, Material: material},
		Model{ID: "model", Src: "/actor.glb", Material: material},
		InstancedMesh{ID: "instances", Count: 1, Geometry: CubeGeometry{Size: 1}, Material: material},
		InstancedGLBMesh{ID: "glb-instances", Src: "/actors.glb", Material: material, Instances: []MeshInstance{{ID: "a"}}},
	)
	ir := (Props{Graph: graph}).SceneIR()
	if len(ir.Objects) != 1 || len(ir.Models) != 1 || len(ir.InstancedMeshes) != 1 || len(ir.InstancedGLBMeshes) != 1 {
		t.Fatalf("typed lowering lost records: %d/%d/%d/%d", len(ir.Objects), len(ir.Models), len(ir.InstancedMeshes), len(ir.InstancedGLBMeshes))
	}
	check := func(label string, emissive *float64, gotColor *[3]float64, normalScale, occlusionStrength *float64) {
		t.Helper()
		if emissive == nil || *emissive != 4 {
			t.Errorf("%s emissive = %v, want 4", label, emissive)
		}
		if gotColor == nil || *gotColor != *color {
			t.Errorf("%s emissiveColor = %v, want %v", label, gotColor, *color)
		}
		if normalScale == nil || *normalScale != 0 {
			t.Errorf("%s normalScale = %v, want explicit 0", label, normalScale)
		}
		if occlusionStrength == nil || *occlusionStrength != 0 {
			t.Errorf("%s occlusionStrength = %v, want explicit 0", label, occlusionStrength)
		}
	}
	check("object", ir.Objects[0].Emissive, ir.Objects[0].EmissiveColor, ir.Objects[0].NormalScale, ir.Objects[0].OcclusionStrength)
	check("model", ir.Models[0].Emissive, ir.Models[0].EmissiveColor, ir.Models[0].NormalScale, ir.Models[0].OcclusionStrength)
	check("instanced mesh", ir.InstancedMeshes[0].Emissive, ir.InstancedMeshes[0].EmissiveColor, ir.InstancedMeshes[0].NormalScale, ir.InstancedMeshes[0].OcclusionStrength)
	check("instanced GLB", ir.InstancedGLBMeshes[0].Emissive, ir.InstancedGLBMeshes[0].EmissiveColor, ir.InstancedGLBMeshes[0].NormalScale, ir.InstancedGLBMeshes[0].OcclusionStrength)

	wire, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("marshal SceneIR: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(wire, &raw); err != nil {
		t.Fatalf("unmarshal SceneIR: %v", err)
	}
	model := raw["models"].([]any)[0].(map[string]any)
	if model["emissive"] != float64(4) || model["normalScale"] != float64(0) || model["occlusionStrength"] != float64(0) {
		t.Fatalf("model material factors lost on wire: %#v", model)
	}
	if got := model["emissiveColor"].([]any); len(got) != 3 || got[0] != 0.8 || got[1] != 0.15 || got[2] != 0.05 {
		t.Fatalf("model emissiveColor = %v, want [0.8 0.15 0.05]", got)
	}
}

func TestStandardMaterialKeepsAbsentEmissionColorDistinctFromBlack(t *testing.T) {
	absent := (StandardMaterial{Emissive: 1}).legacyMaterial()
	if _, ok := absent["emissiveColor"]; ok {
		t.Fatalf("absent emissiveColor emitted legacy key: %#v", absent)
	}
	black := gltfFactorColor([3]float64{0, 0, 0})
	legacy := (StandardMaterial{Emissive: 1, EmissiveColor: black}).legacyMaterial()
	if got, ok := legacy["emissiveColor"].([]float64); !ok || len(got) != 3 || got[0] != 0 || got[1] != 0 || got[2] != 0 {
		t.Fatalf("explicit black emissiveColor lowered to %v", legacy["emissiveColor"])
	}

	wire, err := json.Marshal(ObjectIR{ID: "black", Emissive: Float(1), EmissiveColor: black})
	if err != nil {
		t.Fatalf("marshal explicit black ObjectIR: %v", err)
	}
	if !json.Valid(wire) || len(wire) == 0 || !containsJSONField(wire, `"emissiveColor":[0,0,0]`) {
		t.Fatalf("explicit black emissiveColor missing from JSON: %s", wire)
	}
}

func containsJSONField(data []byte, value string) bool {
	return strings.Contains(string(data), value)
}
