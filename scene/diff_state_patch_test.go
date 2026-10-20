package scene

import (
	"encoding/json"
	"testing"
)

func TestObjectStatePatchResetsInteractionAndKeepsGeometry(t *testing.T) {
	previous := ObjectIR{ID: "tile", Kind: "mesh", Interactive: true, Label: "Play", InteractiveOrder: 9, Selected: true, Pickable: Bool(true), Visible: Bool(false), X: 2, ScaleY: 2, Vertices: &MeshVertices{Positions: []float64{0, 0, 0, 1, 0, 0, 0, 1, 0}}}
	next := previous
	next.Interactive, next.Label, next.InteractiveOrder, next.Selected = false, "", 0, false
	next.Pickable, next.Visible, next.X, next.ScaleY = Bool(false), nil, 0, 0
	commands := DiffScene(SceneIR{Objects: []ObjectIR{previous}}, SceneIR{Objects: []ObjectIR{next}}, DiffOptions{PatchObjectState: true}).Commands
	if len(commands) != 1 || commands[0].Kind != CommandSetTransform {
		t.Fatalf("want one object patch, got %#v", commands)
	}
	payload, err := json.Marshal(commands[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"interactive": false, "label": "", "interactiveOrder": float64(0), "pickable": false, "visible": true, "selected": false, "x": float64(0), "scaleY": float64(1), "parentMatrix": nil} {
		got, ok := decoded[key]
		if !ok || got != want {
			t.Errorf("%s = %v (present %v), want %v", key, got, ok, want)
		}
	}
	if _, ok := decoded["vertices"]; ok {
		t.Fatal("unchanged geometry was serialized")
	}
	if len(DiffCommands(SceneIR{Objects: []ObjectIR{previous}}, SceneIR{Objects: []ObjectIR{next}})) != 2 {
		t.Fatal("default diff changed")
	}
}

func TestObjectStatePatchDeclinesUnrepresentableOrAdditionalEdits(t *testing.T) {
	for name, mutate := range map[string]func(*ObjectIR){
		"geometry":       func(o *ObjectIR) { o.Width = 3 },
		"material":       func(o *ObjectIR) { o.Roughness = .8 },
		"nested uniform": func(o *ObjectIR) { o.CustomUniforms = map[string]any{"pulse": 1} },
		"picking reset":  func(o *ObjectIR) { o.Interactive = false; o.Pickable = nil },
		"invalid parent": func(o *ObjectIR) { o.ParentMatrix = []float64{1} },
	} {
		t.Run(name, func(t *testing.T) {
			previous := ObjectIR{ID: "tile", Kind: "mesh", Interactive: true, Pickable: Bool(true)}
			next := previous
			next.X = 1
			next.Label = "New"
			mutate(&next)
			commands := DiffScene(SceneIR{Objects: []ObjectIR{previous}}, SceneIR{Objects: []ObjectIR{next}}, DiffOptions{PatchObjectState: true}).Commands
			if len(commands) != 2 || commands[0].Kind != CommandRemoveObject || commands[1].Kind != CommandCreateObject {
				t.Fatalf("lost an edit: %#v", commands)
			}
		})
	}
}

func TestObjectStatePatchLeavesUnauthoredPickingUnspecified(t *testing.T) {
	previous := ObjectIR{ID: "tile", Kind: "box"}
	next := previous
	next.Selected = true
	commands := DiffScene(SceneIR{Objects: []ObjectIR{previous}}, SceneIR{Objects: []ObjectIR{next}}, DiffOptions{PatchObjectState: true}).Commands
	if len(commands) != 1 {
		t.Fatalf("commands = %#v", commands)
	}
	payload, err := json.Marshal(commands[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["pickable"]; ok {
		t.Fatal("unauthored picking became explicit")
	}
}

func BenchmarkObjectStateCommandEncoding(b *testing.B) {
	positions := make([]float64, 30000)
	for i := range positions {
		positions[i] = float64(i) / 100
	}
	previous := SceneIR{Objects: []ObjectIR{{ID: "tile", Kind: "mesh", Pickable: Bool(false), Vertices: &MeshVertices{Positions: positions}}}}
	next := previous
	next.Objects = append([]ObjectIR(nil), previous.Objects...)
	next.Objects[0].Interactive = true
	for _, patches := range []bool{false, true} {
		name := "replace"
		if patches {
			name = "patch"
		}
		b.Run(name, func(b *testing.B) {
			commands := DiffScene(previous, next, DiffOptions{PatchObjectState: patches}).Commands
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				data, err := MarshalCommands(commands)
				if err != nil {
					b.Fatal(err)
				}
				b.SetBytes(int64(len(data)))
			}
		})
	}
}
