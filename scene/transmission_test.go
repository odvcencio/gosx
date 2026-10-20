package scene

import (
	"encoding/json"
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

func TestTransmissionVolumeSurvivesLowering(t *testing.T) {
	tint := [3]float64{0, 0.75, 1}
	material := StandardMaterial{Transmission: 0.9, Thickness: 2.5, AttenuationDistance: 4, AttenuationColor: &tint, IOR: Float(1.48)}
	graph := NewGraph(Mesh{ID: "glass", Geometry: BoxGeometry{}, Material: material})
	ir := graph.SceneIR()
	if len(ir.Objects) != 1 {
		t.Fatalf("objects: %d", len(ir.Objects))
	}
	o := ir.Objects[0]
	if o.Thickness != 2.5 || o.AttenuationDistance != 4 || o.AttenuationColor == nil || *o.AttenuationColor != tint {
		t.Fatalf("volume fields dropped: %+v", o)
	}
	normalized := materialFromObjectIR(o)
	if normalized.Thickness != o.Thickness || normalized.AttenuationDistance != o.AttenuationDistance || *normalized.AttenuationColor != tint {
		t.Fatal("typed IR drops volume controls")
	}
	raw, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	props := ir.legacyProps()
	objects := props["objects"].([]map[string]any)
	fields := objects[0]
	if fields["thickness"] != 2.5 || fields["attenuationDistance"] != float64(4) {
		t.Fatalf("legacy volume dropped: %v", fields)
	}
	features := collectFeatures(ir)
	found := false
	for _, f := range features {
		if f == capability.FeatureTransmission {
			found = true
		}
	}
	if !found {
		t.Fatal("transmission capability missing")
	}
	// Lowering must own the tint so a later author edit cannot change a frame.
	tint[1] = 0.1
	if normalized.AttenuationColor[1] != 0.75 {
		t.Fatal("volume tint aliases author memory")
	}
}

func TestModelTransmissionDeclaresCapability(t *testing.T) {
	graph := NewGraph(Model{ID: "glass", Src: "/glass.glb", Material: StandardMaterial{Transmission: 1, Thickness: 2}})
	ir := graph.SceneIR()
	if len(ir.Models) != 1 || ir.Models[0].Thickness != 2 {
		t.Fatal("model volume override was dropped")
	}
	for _, feature := range collectFeatures(ir) {
		if feature == capability.FeatureTransmission {
			return
		}
	}
	t.Fatal("model transmission capability missing")
}
