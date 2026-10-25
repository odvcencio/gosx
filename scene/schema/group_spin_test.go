package schema

import (
	"encoding/json"
	"testing"

	"m31labs.dev/gosx/scene"
)

func TestGroupSpinSchemaAcceptsRetainedSubtree(t *testing.T) {
	ir := (scene.Props{Graph: scene.NewGraph(scene.Group{ID: "rotor", Spin: scene.Rotate(4, 0, 0), Children: []scene.Node{
		scene.Mesh{ID: "tip", Position: scene.Vec3(0, 2, 0), Geometry: scene.BoxGeometry{}},
		scene.Label{ID: "caption", Target: "rotor", Text: "Rotor"},
	}})}).SceneIR()
	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	report := ValidateJSON(data, Options{Strict: true})
	if !report.Valid {
		t.Fatalf("typed rotor rejected: %+v", report.Diagnostics)
	}
}

func TestGroupSpinSchemaRejectsBrokenHierarchy(t *testing.T) {
	for _, source := range []string{
		`{"groups":[{"id":"a","parentNode":"b"},{"id":"b","parentNode":"a"}]}`,
		`{"groups":[{"id":"a"},{"id":"a"}]}`,
		`{"groups":[{"id":"a","parentNode":"missing"}]}`,
		`{"groups":[{"id":"a"}],"objects":[{"id":"tip","kind":"box","parentNode":"missing"}]}`,
		`{"groups":[{"id":"a"}],"objects":[{"id":"a","kind":"box"}]}`,
	} {
		if report := ValidateJSON([]byte(source), Options{Strict: true}); report.Valid {
			t.Errorf("invalid hierarchy accepted: %s", source)
		}
	}
}
