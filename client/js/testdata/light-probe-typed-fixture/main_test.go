package main

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
)

// The tests below run the real emit path — scene.Props -> SceneIR ->
// encoding/json — and assert the wire shape the native browser probe
// depends on: nine coefficients in basis order 0..8, json:omitempty
// components that omit zero channels, and an all-zero nine preserved.

func emitArray(t *testing.T, probe scene.LightProbe) ([]map[string]any, string) {
	t.Helper()
	raw, err := emit(probe)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	var lights []map[string]any
	if err := json.Unmarshal(raw, &lights); err != nil {
		t.Fatalf("unmarshal lights: %v", err)
	}
	return lights, string(raw)
}

func TestEmitSparseCoefficients(t *testing.T) {
	probe := scene.LightProbe{ID: "probe", Color: "#0000ff", Intensity: 0.5,
		Coefficients: make([]scene.Vector3, 9)}
	probe.Coefficients[0] = scene.Vector3{X: 1, Y: 0.5}

	lights, raw := emitArray(t, probe)
	if len(lights) != 1 {
		t.Fatalf("lights length %d, want 1", len(lights))
	}
	light := lights[0]
	if light["color"] != "#0000ff" {
		t.Fatalf("color = %v, want #0000ff", light["color"])
	}
	if light["intensity"] != float64(0.5) {
		t.Fatalf("intensity = %v, want 0.5", light["intensity"])
	}
	coefs, ok := light["coefficients"].([]any)
	if !ok || len(coefs) != 9 {
		t.Fatalf("coefficients = %T %v, want a JSON array of length 9", light["coefficients"], light["coefficients"])
	}
	// Basis order 0..8: the typed non-zero entry lands at index 0, and the
	// zero channels of its Vector3 are omitted by json:omitempty.
	first, ok := coefs[0].(map[string]any)
	if !ok {
		t.Fatalf("coefficient 0 = %T, want an object", coefs[0])
	}
	if first["x"] != float64(1) || first["y"] != float64(0.5) {
		t.Fatalf("coefficient 0 = %v, want x=1 y=0.5", first)
	}
	if _, ok := first["z"]; ok {
		t.Fatalf("coefficient 0 must omit the zero z channel: %v", first)
	}
	for i := 1; i < 9; i++ {
		c, ok := coefs[i].(map[string]any)
		if !ok || len(c) != 0 {
			t.Fatalf("coefficient %d = %v, want an empty object (all channels omitted)", i, coefs[i])
		}
	}
	if strings.Contains(raw, `"z"`) {
		t.Fatalf("the sparse payload must contain no z key: %s", raw)
	}
}

func TestEmitZeroCoefficientsPreserved(t *testing.T) {
	probe := scene.LightProbe{ID: "probe", Color: "#0000ff", Intensity: 0.5,
		Coefficients: make([]scene.Vector3, 9)}

	lights, raw := emitArray(t, probe)
	if len(lights) != 1 {
		t.Fatalf("lights length %d, want 1", len(lights))
	}
	coefs, ok := lights[0]["coefficients"].([]any)
	if !ok {
		t.Fatalf("coefficients = %T, want a JSON array", lights[0]["coefficients"])
	}
	// The all-zero nine must survive the wire: nine empty objects, not a
	// dropped or truncated array.
	if len(coefs) != 9 {
		t.Fatalf("all-zero coefficients length %d, want 9", len(coefs))
	}
	for i, c := range coefs {
		m, ok := c.(map[string]any)
		if !ok || len(m) != 0 {
			t.Fatalf("coefficient %d = %v, want an empty object", i, c)
		}
	}
	if !strings.Contains(raw, "[{},{},{},{},{},{},{},{},{}]") {
		t.Fatalf("the zero payload must serialize nine empty objects, got %s", raw)
	}
}
