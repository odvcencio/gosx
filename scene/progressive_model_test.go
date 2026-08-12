package scene

import (
	"encoding/json"
	"testing"
)

func TestProgressiveModelLowersToSceneIR(t *testing.T) {
	ir := Props{Graph: NewGraph(Model{
		ID:          "hero",
		PreviewSrc:  "/models/hero-preview.glb",
		FullSrc:     "/models/hero-full.glb",
		Progressive: true,
		Scale:       Vector3{X: 1, Y: 1, Z: 1},
	})}.SceneIR()

	if len(ir.Models) != 1 {
		t.Fatalf("models = %d, want 1", len(ir.Models))
	}
	model := ir.Models[0]
	if model.Src != "/models/hero-full.glb" {
		t.Fatalf("src = %q", model.Src)
	}
	if model.PreviewSrc != "/models/hero-preview.glb" || model.FullSrc != "/models/hero-full.glb" {
		t.Fatalf("progressive sources = %q/%q", model.PreviewSrc, model.FullSrc)
	}
	if !model.Progressive {
		t.Fatal("progressive flag was not lowered")
	}

	data, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["previewSrc"] != "/models/hero-preview.glb" || wire["fullSrc"] != "/models/hero-full.glb" {
		t.Fatalf("wire progressive sources missing: %s", data)
	}
	if wire["progressive"] != true {
		t.Fatalf("wire progressive flag missing: %s", data)
	}
}
