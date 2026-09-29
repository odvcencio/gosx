package scene

import "testing"

func TestInteractiveMeshAndModelLowerToSceneIR(t *testing.T) {
	ir := Props{Graph: NewGraph(
		Model{ID: "model-focus", Src: "/model.glb", Interactive: true, Label: "Model label"},
		Mesh{ID: "mesh-focus", Geometry: BoxGeometry{}, Interactive: true, Label: "Mesh label"},
	)}.SceneIR()
	if len(ir.Objects) != 1 || !ir.Objects[0].Interactive || ir.Objects[0].Label != "Mesh label" {
		t.Fatalf("mesh focus props were not lowered: %#v", ir.Objects)
	}
	if len(ir.Models) != 1 || !ir.Models[0].Interactive || ir.Models[0].Label != "Model label" {
		t.Fatalf("model focus props were not lowered: %#v", ir.Models)
	}
	if ir.Objects[0].InteractiveOrder != 2 || ir.Models[0].InteractiveOrder != 1 {
		t.Fatalf("interactive source order was not preserved: mesh=%d model=%d", ir.Objects[0].InteractiveOrder, ir.Models[0].InteractiveOrder)
	}
	if ir.Objects[0].Pickable == nil || !*ir.Objects[0].Pickable || ir.Models[0].Pickable == nil || !*ir.Models[0].Pickable {
		t.Fatalf("interactive nodes must lower as pickable: mesh=%#v model=%#v", ir.Objects[0].Pickable, ir.Models[0].Pickable)
	}
}
