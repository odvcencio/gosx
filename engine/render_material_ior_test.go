package engine_test

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
)

// TestRenderMaterialIORJSON pins the absent-vs-explicit-zero contract on the
// engine render material: an unset IOR emits no ior key, while an explicitly
// authored zero survives as a present zero.
func TestRenderMaterialIORJSON(t *testing.T) {
	zero := 0.0
	data, err := json.Marshal(engine.RenderMaterial{IOR: &zero})
	if err != nil {
		t.Fatalf("marshal RenderMaterial: %v", err)
	}
	if !strings.Contains(string(data), `"ior":0`) {
		t.Fatalf("explicit zero IOR lost in JSON: %s", data)
	}
	absent, err := json.Marshal(engine.RenderMaterial{})
	if err != nil {
		t.Fatalf("marshal RenderMaterial: %v", err)
	}
	if strings.Contains(string(absent), "ior") {
		t.Fatalf("absent IOR emitted a JSON key: %s", absent)
	}
}
