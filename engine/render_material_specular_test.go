package engine_test

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
)

// TestRenderMaterialSpecularJSON pins the absent-vs-explicit contract on the
// engine render material and proves HDR color factors survive transport
// without upper clamping.
func TestRenderMaterialSpecularJSON(t *testing.T) {
	zero := 0.0
	data, err := json.Marshal(engine.RenderMaterial{SpecularIntensity: &zero, SpecularColor: &[3]float64{2.5, 1, 0.5}})
	if err != nil {
		t.Fatalf("marshal RenderMaterial: %v", err)
	}
	if !strings.Contains(string(data), `"specularIntensity":0`) {
		t.Fatalf("explicit zero specularIntensity lost in JSON: %s", data)
	}
	if !strings.Contains(string(data), `"specularColor":[2.5,1,0.5]`) {
		t.Fatalf("HDR specularColor clamped or lost in JSON: %s", data)
	}
	absent, err := json.Marshal(engine.RenderMaterial{})
	if err != nil {
		t.Fatalf("marshal RenderMaterial: %v", err)
	}
	if strings.Contains(string(absent), "specular") {
		t.Fatalf("absent specular fields emitted JSON keys: %s", absent)
	}
}
