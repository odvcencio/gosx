package engine

import (
	"encoding/json"
	"testing"

	islandprogram "github.com/odvcencio/gosx/island/program"
)

func TestProgramJSONRoundTrip(t *testing.T) {
	original := &Program{
		Name: "GeometryZoo",
		Nodes: []Node{
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"position": 1,
					"color":    2,
				},
			},
			{
				Kind:     "camera",
				Children: []int{0},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$scene.color", Type: islandprogram.TypeString, Init: 1},
		},
	}

	data, err := EncodeProgramJSON(original)
	if err != nil {
		t.Fatalf("encode program json: %v", err)
	}

	decoded, err := DecodeProgramJSON(data)
	if err != nil {
		t.Fatalf("decode program json: %v", err)
	}

	if decoded.Name != original.Name {
		t.Fatalf("expected name %q, got %q", original.Name, decoded.Name)
	}
	if len(decoded.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(decoded.Nodes))
	}
	if decoded.Nodes[0].Props["color"] != 2 {
		t.Fatalf("expected color expr 2, got %d", decoded.Nodes[0].Props["color"])
	}
	if len(decoded.Signals) != 1 || decoded.Signals[0].Name != "$scene.color" {
		t.Fatalf("unexpected signals: %#v", decoded.Signals)
	}
}

func TestCommandJSONRoundTrip(t *testing.T) {
	original := Command{
		Kind:     CommandSetTransform,
		ObjectID: 4,
		Data:     json.RawMessage(`{"position":[1,2,3]}`),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal command: %v", err)
	}

	var decoded Command
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal command: %v", err)
	}

	if decoded.Kind != CommandSetTransform {
		t.Fatalf("expected CommandSetTransform, got %v", decoded.Kind)
	}
	if decoded.ObjectID != 4 {
		t.Fatalf("expected object id 4, got %d", decoded.ObjectID)
	}
	if string(decoded.Data) != `{"position":[1,2,3]}` {
		t.Fatalf("unexpected command data: %s", decoded.Data)
	}
}
