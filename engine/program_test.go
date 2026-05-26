package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	islandprogram "m31labs.dev/gosx/island/program"
)

func TestProgramJSONRoundTrip(t *testing.T) {
	original := &Program{
		Name: "GeometryZoo",
		EngineNodes: []Node{
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
	if len(decoded.EngineNodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(decoded.EngineNodes))
	}
	if decoded.EngineNodes[0].Props["color"] != 2 {
		t.Fatalf("expected color expr 2, got %d", decoded.EngineNodes[0].Props["color"])
	}
	if len(decoded.Signals) != 1 || decoded.Signals[0].Name != "$scene.color" {
		t.Fatalf("unexpected signals: %#v", decoded.Signals)
	}
}

func TestDecodeProgramJSONInjectsSurfaceScene3D(t *testing.T) {
	data := []byte(`{"engineNodes":[],"exprs":[]}`)
	p, err := DecodeProgramJSON(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Surface != islandprogram.SurfaceScene3D {
		t.Errorf("Surface = %v, want SurfaceScene3D", p.Surface)
	}
}

// TestEngineFixturesRoundTrip is the engine-side wire-format contract test
// (ADR 0001 §"Test contract"). A failure on the captured fixture is the
// signal that the per-decoder surface-injection design is insufficient and a
// v2 wire envelope is required sooner than planned. STOP and file an ADR.
func TestEngineFixturesRoundTrip(t *testing.T) {
	entries, err := os.ReadDir("testdata/fixtures")
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no fixtures found; Task 7.1 must seed at least three")
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata/fixtures", name))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			p, err := DecodeProgramJSON(data)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if p.Surface != islandprogram.SurfaceScene3D {
				t.Fatalf("Surface = %v, want SurfaceScene3D", p.Surface)
			}
			out, err := EncodeProgramJSON(p)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			p2, err := DecodeProgramJSON(out)
			if err != nil {
				t.Fatalf("re-decode: %v", err)
			}
			canonA, _ := EncodeProgramJSON(p)
			canonB, _ := EncodeProgramJSON(p2)
			if string(canonA) != string(canonB) {
				t.Errorf("round-trip mismatch:\n got: %s\nwant: %s", canonB, canonA)
			}
		})
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
