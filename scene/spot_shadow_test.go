package scene

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func typedSpotShadow(position, direction Vector3, angle float64, cast bool) Props {
	return Props{Graph: NewGraph(SpotLight{
		ID:             "key",
		Color:          "#fff4d6",
		Intensity:      2,
		Position:       position,
		Direction:      direction,
		Angle:          angle,
		Penumbra:       0.2,
		Range:          18,
		Decay:          2,
		CastShadow:     cast,
		ShadowBias:     0.001,
		ShadowSize:     1024,
		ShadowSoftness: 0.03,
	})}
}

func TestSpotShadowTypedAndCanonicalIdentity(t *testing.T) {
	props := typedSpotShadow(Vec3(2, 4, 3), Vec3(-1, -2, -1), 0.6, true)
	compat := props.SceneIR()
	if len(compat.Lights) != 1 {
		t.Fatalf("compat lights = %d, want 1", len(compat.Lights))
	}
	light := compat.Lights[0]
	if light.Kind != "spot" || !light.CastShadow || light.Angle != 0.6 || light.ShadowSize != 1024 {
		t.Fatalf("compat spot shadow fields = %#v", light)
	}

	canonical := props.CanonicalIR()
	if err := canonical.Validate(); err != nil {
		t.Fatalf("canonical spot shadow rejected: %v", err)
	}
	if len(canonical.Lights) != 1 {
		t.Fatalf("canonical lights = %d, want 1", len(canonical.Lights))
	}
	got := canonical.Lights[0]
	if got.Kind != light.Kind || got.X != light.X || got.Y != light.Y || got.Z != light.Z ||
		got.DirectionX != light.DirectionX || got.DirectionY != light.DirectionY || got.DirectionZ != light.DirectionZ ||
		got.Angle != light.Angle || got.Range != light.Range || got.CastShadow != light.CastShadow ||
		got.ShadowBias != light.ShadowBias || got.ShadowSize != light.ShadowSize || got.ShadowSoftness != light.ShadowSoftness {
		t.Fatalf("canonical spot shadow lost identity: compat=%#v canonical=%#v", light, got)
	}

	base, err := json.Marshal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*IRLight){
		"position":  func(l *IRLight) { l.X += 0.25 },
		"direction": func(l *IRLight) { l.DirectionZ -= 0.25 },
		"angle":     func(l *IRLight) { l.Angle += 0.05 },
		"range":     func(l *IRLight) { l.Range += 1 },
		"cast":      func(l *IRLight) { l.CastShadow = false },
		"bias":      func(l *IRLight) { l.ShadowBias += 0.0001 },
		"size":      func(l *IRLight) { l.ShadowSize = 512 },
		"softness":  func(l *IRLight) { l.ShadowSoftness += 0.01 },
	} {
		t.Run(name, func(t *testing.T) {
			next := canonical
			next.Lights = append([]IRLight(nil), canonical.Lights...)
			mutate(&next.Lights[0])
			encoded, err := json.Marshal(next)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(base, encoded) {
				t.Fatalf("%s mutation did not change canonical identity", name)
			}
		})
	}
}

func TestCanonicalIRValidatesSpotShadowProjection(t *testing.T) {
	valid := []IRLight{
		{Kind: "spot", CastShadow: true, Angle: 0}, // established 30-degree runtime default
		{Kind: "spot", CastShadow: true, Angle: 0.5, ShadowCascades: 1},
		{Kind: "spot", CastShadow: true, Angle: math.Nextafter(math.Pi/2, 0)},
		{Kind: "spot", CastShadow: false, Angle: math.Pi},
	}
	for i, light := range valid {
		ir := IR{Version: IRVersion, Lights: []IRLight{light}}
		if err := ir.Validate(); err != nil {
			t.Fatalf("valid case %d rejected: %v", i, err)
		}
	}

	invalid := []struct {
		name string
		set  func(*IRLight)
		want string
	}{
		{"wide", func(l *IRLight) { l.Angle = math.Pi / 2 }, "lights[0].angle must be less than pi/2"},
		{"cascades", func(l *IRLight) { l.ShadowCascades = 2 }, "lights[0].shadowCascades must be 0 or 1"},
		{"nonfinite-position", func(l *IRLight) { l.X = math.NaN() }, "lights[0].x must be finite"},
		{"nonfinite-direction", func(l *IRLight) { l.DirectionY = math.Inf(1) }, "lights[0].directionY must be finite"},
		{"negative-softness", func(l *IRLight) { l.ShadowSoftness = -0.1 }, "lights[0].shadowSoftness must not be negative"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			light := IRLight{Kind: "spot", CastShadow: true, Angle: 0.5}
			tc.set(&light)
			err := (&IR{Version: IRVersion, Lights: []IRLight{light}}).Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSpotShadowDiffCommandsPreserveLiveFields(t *testing.T) {
	base := typedSpotShadow(Vec3(2, 4, 3), Vec3(-1, -2, -1), 0.6, false).SceneIR()
	updates := []SceneIR{
		typedSpotShadow(Vec3(2, 4, 3), Vec3(-1, -2, -1), 0.6, true).SceneIR(),
		typedSpotShadow(Vec3(3, 4, 3), Vec3(-1, -2, -1), 0.6, true).SceneIR(),
		typedSpotShadow(Vec3(3, 4, 3), Vec3(-2, -2, -1), 0.45, true).SceneIR(),
	}
	for index, next := range updates {
		commands := DiffCommands(base, next)
		if len(commands) != 2 || commands[0].Kind != CommandRemoveObject || commands[1].Kind != CommandCreateObject {
			t.Fatalf("update %d commands = %#v, want remove/create", index, commands)
		}
		payload, ok := commands[1].Data.(CommandPayload)
		if !ok || payload.Kind != "light" {
			t.Fatalf("update %d payload = %#v", index, commands[1].Data)
		}
		created, ok := payload.Props.(LightIR)
		if !ok || !reflect.DeepEqual(created, next.Lights[0]) {
			t.Fatalf("update %d created light = %#v, want %#v", index, created, next.Lights[0])
		}
		base = next
	}
}
