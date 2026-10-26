package vm

import (
	"reflect"
	"testing"

	islandprogram "m31labs.dev/gosx/island/program"
)

func TestSceneLineLightingActivation(t *testing.T) {
	for _, animated := range []bool{false, true} {
		name := "cached"
		if animated {
			name = "animated"
		}
		t.Run(name, func(t *testing.T) {
			prog := staticBoxProgram()
			if animated {
				id := islandprogram.ExprID(len(prog.Exprs))
				prog.Exprs = append(prog.Exprs, islandprogram.Expr{Op: islandprogram.OpLitFloat, Type: islandprogram.TypeFloat, Value: "0.7"})
				prog.EngineNodes[0].Props["spinY"] = id
			}
			rt := NewSceneAdapter(prog, `{}`)
			rt.Reconcile()
			var positions []float64
			base := [4]float64{141.0 / 255, 225.0 / 255, 1, 1}
			for _, phase := range []struct {
				name   string
				env    map[string]any
				lights []any
				lit    bool
			}{
				{name: "unlit"},
				{name: "ambient", env: map[string]any{"ambientIntensity": .3}, lit: true},
				{name: "hemisphere", env: map[string]any{"skyIntensity": .4, "groundIntensity": .1}, lit: true},
				{name: "directional", lights: []any{map[string]any{"kind": "directional", "directionY": -1., "intensity": 1.}}, lit: true},
				{name: "point", lights: []any{map[string]any{"kind": "point", "x": 2., "y": 3., "z": 4., "intensity": 1.}}, lit: true},
				{name: "unlit-again"},
			} {
				props := map[string]any{"environment": phase.env, "lights": phase.lights}
				rt.props = props
				got := rt.RenderBundle(1280, 720, .75)
				fresh := NewSceneAdapter(prog, `{}`)
				fresh.Reconcile()
				fresh.props = props
				if want := fresh.RenderBundle(1280, 720, .75); !reflect.DeepEqual(got, want) {
					t.Fatalf("%s: retained renderer differs from a fresh renderer", phase.name)
				}
				if len(got.WorldPositions) == 0 || len(got.WorldColors) == 0 {
					t.Fatalf("%s: missing line streams", phase.name)
				}
				if positions == nil {
					positions = append([]float64(nil), got.WorldPositions...)
				} else if !reflect.DeepEqual(positions, got.WorldPositions) {
					t.Fatalf("%s: lighting changed geometry", phase.name)
				}
				changed := false
				for i, component := range got.WorldColors {
					changed = changed || component != base[i%4]
				}
				if changed != phase.lit {
					t.Fatalf("%s: colors changed from the base material=%v, want %v", phase.name, changed, phase.lit)
				}
				hits := rt.bakeHits
				if repeated := rt.RenderBundle(1280, 720, .75); !reflect.DeepEqual(got, repeated) {
					t.Fatalf("%s: repeated frame changed", phase.name)
				}
				if !animated && rt.bakeHits <= hits {
					t.Fatalf("%s: static geometry did not reuse its world bake", phase.name)
				}
			}
		})
	}
}
