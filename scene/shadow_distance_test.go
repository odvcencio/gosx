package scene

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestDirectionalLightShadowDistanceLowers(t *testing.T) {
	ir := NewGraph(DirectionalLight{ID: "sun", Direction: Vec3(-0.3, -1, -0.2), CastShadow: true, ShadowCascades: 4, ShadowDistance: 60}).SceneIR()
	if len(ir.Lights) != 1 || ir.Lights[0].ShadowDistance != 60 {
		t.Fatalf("scene IR lost shadowDistance: %+v", ir.Lights)
	}
	data, err := json.Marshal(ir.Lights[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"shadowDistance":60`) {
		t.Fatalf("wire lost shadowDistance: %s", data)
	}
	if got := ir.legacyProps()["lights"]; !strings.Contains(mustJSON(t, got), `"shadowDistance":60`) {
		t.Fatalf("legacy light record lost shadowDistance: %s", mustJSON(t, got))
	}
}

func TestIRLightRejectsInvalidShadowDistance(t *testing.T) {
	for _, value := range []float64{-1, math.NaN(), math.Inf(1)} {
		problems := validateIRLight(0, IRLight{Kind: "directional", ShadowDistance: value})
		if !strings.Contains(strings.Join(problems, "; "), "shadowDistance") {
			t.Errorf("shadowDistance %v accepted: %v", value, problems)
		}
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
