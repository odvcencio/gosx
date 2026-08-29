package scene

import (
	"encoding/json"
	"math"
	"testing"
)

func TestPointLightShadowLowering(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Group{
				Position: Vec3(0, 2, 0),
				Children: []Node{
					PointLight{
						ID:             "bulb",
						Color:          "#fff4e0",
						Intensity:      3,
						Position:       Vec3(1, 0, 0),
						Range:          25,
						Decay:          2,
						CastShadow:     true,
						ShadowBias:     0.004,
						ShadowSize:     512,
						ShadowSoftness: 0.2,
					},
				},
			},
			PointLight{ID: "neg-soft", ShadowSoftness: -1},
			PointLight{ID: "inf-soft", ShadowSoftness: math.Inf(1)},
			PointLight{ID: "plain"},
		),
	}

	ir := props.SceneIR()
	if len(ir.Lights) != 4 {
		t.Fatalf("expected four lowered lights, got %d: %#v", len(ir.Lights), ir.Lights)
	}
	point := ir.Lights[0]
	if point.Kind != "point" || point.ID != "bulb" {
		t.Fatalf("expected point light, got %#v", point)
	}
	// Position (1,0,0) under a group at (0,2,0) transforms to (1,2,0).
	if math.Abs(point.X-1) > 1e-9 || math.Abs(point.Y-2) > 1e-9 || math.Abs(point.Z) > 1e-9 {
		t.Fatalf("expected transformed point position (1,2,0), got (%v,%v,%v)", point.X, point.Y, point.Z)
	}
	if point.Range != 25 || point.Decay != 2 || point.Intensity != 3 {
		t.Fatalf("expected range/decay/intensity preserved, got %#v", point)
	}
	if !point.CastShadow || point.ShadowBias != 0.004 || point.ShadowSize != 512 || point.ShadowSoftness != 0.2 {
		t.Fatalf("expected point shadow fields in SceneIR, got %#v", point)
	}
	if ir.Lights[1].ShadowSoftness != 0 {
		t.Fatalf("expected negative point softness to clamp to 0, got %v", ir.Lights[1].ShadowSoftness)
	}
	if ir.Lights[2].ShadowSoftness != 0 {
		t.Fatalf("expected non-finite point softness to clamp to 0, got %v", ir.Lights[2].ShadowSoftness)
	}

	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	lights, ok := sceneValue["lights"].([]map[string]any)
	if !ok || len(lights) != 4 {
		t.Fatalf("expected four lights in legacy props, got %#v", sceneValue["lights"])
	}
	first := lights[0]
	if first["kind"] != "point" {
		t.Fatalf("expected point kind in legacy, got %#v", first["kind"])
	}
	if first["x"] != 1.0 || first["y"] != 2.0 {
		t.Fatalf("expected transformed legacy position (1,2), got %#v", first)
	}
	if first["castShadow"] != true {
		t.Fatalf("expected castShadow in legacy, got %#v", first["castShadow"])
	}
	if first["shadowBias"] != 0.004 {
		t.Fatalf("expected shadowBias in legacy, got %#v", first["shadowBias"])
	}
	if first["shadowSize"] != 512 {
		t.Fatalf("expected shadowSize in legacy, got %#v", first["shadowSize"])
	}
	if first["shadowSoftness"] != 0.2 {
		t.Fatalf("expected shadowSoftness in legacy, got %#v", first["shadowSoftness"])
	}
	plain := lights[3]
	for _, key := range []string{"castShadow", "shadowBias", "shadowSize", "shadowSoftness"} {
		if _, present := plain[key]; present {
			t.Fatalf("expected %q omitted for default point light, got %#v", key, plain[key])
		}
	}

	var raw struct {
		Scene struct {
			Lights []struct {
				Kind           string   `json:"kind"`
				ID             string   `json:"id"`
				X              float64  `json:"x"`
				Y              float64  `json:"y"`
				Z              float64  `json:"z"`
				CastShadow     *bool    `json:"castShadow"`
				ShadowBias     *float64 `json:"shadowBias"`
				ShadowSize     *float64 `json:"shadowSize"`
				ShadowSoftness *float64 `json:"shadowSoftness"`
			} `json:"lights"`
		} `json:"scene"`
	}
	if err := json.Unmarshal(props.RawPropsJSON(), &raw); err != nil {
		t.Fatalf("failed to parse raw props JSON: %v", err)
	}
	rawLights := raw.Scene.Lights
	if len(rawLights) != 4 {
		t.Fatalf("expected four lights in raw props JSON, got %d", len(rawLights))
	}
	firstRaw := rawLights[0]
	if firstRaw.Kind != "point" {
		t.Fatalf("expected kind point for the configured light, got %q", firstRaw.Kind)
	}
	if firstRaw.ID == "" {
		t.Fatalf("expected non-empty id for the configured light, got %#v", firstRaw)
	}
	if firstRaw.X != 1 || firstRaw.Y != 2 || firstRaw.Z != 0 {
		t.Fatalf("expected transformed position (1, 2, 0) for the configured light, got %#v", firstRaw)
	}
	if firstRaw.CastShadow == nil || !*firstRaw.CastShadow {
		t.Fatalf("expected castShadow true for the configured light, got %#v", firstRaw)
	}
	if firstRaw.ShadowBias == nil || *firstRaw.ShadowBias != 0.004 {
		t.Fatalf("expected shadowBias 0.004 for the configured light, got %#v", firstRaw)
	}
	if firstRaw.ShadowSize == nil || *firstRaw.ShadowSize != 512 {
		t.Fatalf("expected shadowSize 512 for the configured light, got %#v", firstRaw)
	}
	if firstRaw.ShadowSoftness == nil || *firstRaw.ShadowSoftness != 0.2 {
		t.Fatalf("expected shadowSoftness 0.2 for the configured light, got %#v", firstRaw)
	}
	for i := 1; i < len(rawLights); i++ {
		other := rawLights[i]
		if other.CastShadow != nil || other.ShadowBias != nil || other.ShadowSize != nil || other.ShadowSoftness != nil {
			t.Fatalf("expected default/clamped shadow fields omitted for light %d, got %#v", i, other)
		}
	}
}
