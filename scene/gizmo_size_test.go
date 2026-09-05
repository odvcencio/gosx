package scene

import (
	"encoding/json"
	"testing"
)

func TestTransformControlsSizeMetadata(t *testing.T) {
	for _, size := range []float64{0, .25, 2} {
		ir := (Props{Graph: NewGraph(TransformControls{ID: "controls", Size: size})}).SceneIR()
		want := size
		if want == 0 {
			want = 1.25
		}
		data, err := json.Marshal(ir)
		if err != nil {
			t.Fatal(err)
		}
		var decoded SceneIR
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if len(decoded.Objects) != 7 {
			t.Fatalf("helper count: %d", len(decoded.Objects))
		}
		for _, obj := range decoded.Objects {
			legacy := obj.legacyProps()
			if legacy["gizmoControlID"] != "controls" || legacy["gizmoSize"] != want {
				t.Fatalf("map metadata: %#v", legacy)
			}
			if obj.GizmoControlID != "controls" || obj.GizmoSize != want {
				t.Fatalf("metadata: %#v", obj)
			}
		}
	}
}

func TestTransformControlsDuplicateGroupsSurviveJSON(t *testing.T) {
	for _, pair := range [][2]TransformControls{
		{{ID: "same", Size: 1}, {ID: "same", Size: 1}},
		{{ID: "same", Size: 1}, {ID: "same", Size: 2}},
		{{}, {ID: "scene-transform-controls-1"}},
		{{ID: "scene-transform-controls-1"}, {}},
		{{ID: "same"}, {ID: "same-scale"}},
	} {
		ir := (Props{Graph: NewGraph(pair[0], pair[1])}).SceneIR()
		data, err := json.Marshal(ir)
		if err != nil {
			t.Fatal(err)
		}
		var decoded SceneIR
		if err = json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		ids := map[string]bool{}
		groups := map[string]int{}
		for _, obj := range decoded.Objects {
			if ids[obj.ID] {
				t.Fatalf("helper overwritten: %q for %#v", obj.ID, pair)
			}
			ids[obj.ID] = true
			groups[obj.GizmoControlID]++
		}
		if len(ids) != 14 || len(groups) != 2 {
			t.Fatalf("lost groups: %v %v", ids, groups)
		}
		for _, n := range groups {
			if n != 7 {
				t.Fatalf("incomplete: %v", groups)
			}
		}
		again, _ := json.Marshal((Props{Graph: NewGraph(pair[0], pair[1])}).SceneIR())
		if string(again) != string(data) {
			t.Fatal("unstable lowering")
		}
	}
}
