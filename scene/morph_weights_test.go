package scene

import (
	"encoding/json"
	"math"
	"testing"
)

func TestPatchModelsCommandWire(t *testing.T) {
	cmd := PatchModelsCommand([]ModelPatch{
		{ID: "home-00", MorphWeights: map[string]float64{"mass": .8, "bad": math.Inf(1)}, Scale: &Vector3{X: 1.02, Y: 1.04, Z: 1.02}},
		{ID: "home-01", MorphWeights: map[string]float64{}, Visible: Bool(false)},
		{ID: "home-02"},
		{MorphWeights: map[string]float64{"mass": 1}},
	})
	if cmd.Kind != 16 {
		t.Fatalf("kind = %d, want 16", cmd.Kind)
	}
	raw, err := json.Marshal(cmd)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kind":16,"data":{"models":[` +
		`{"id":"home-00","morphWeights":{"mass":0.8},"scaleX":1.02,"scaleY":1.04,"scaleZ":1.02},` +
		`{"id":"home-01","morphWeights":{},"visible":false},` +
		`{"id":"home-02"}]}}`
	if string(raw) != want {
		t.Fatalf("wire =\n%s\nwant\n%s", raw, want)
	}
}
