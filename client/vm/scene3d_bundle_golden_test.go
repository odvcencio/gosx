package vm

import (
	"testing"

	rootengine "m31labs.dev/gosx/engine"
	islandprogram "m31labs.dev/gosx/island/program"
)

// goldenBoxProgram builds a single centered box with rotation + spin so the
// per-vertex world-position bake exercises the rotation trig hoist.
func goldenBoxProgram() *rootengine.Program {
	prog := &rootengine.Program{Name: "GoldenBox"}
	var exprs []islandprogram.Expr
	addF := func(v string) islandprogram.ExprID {
		id := islandprogram.ExprID(len(exprs))
		exprs = append(exprs, islandprogram.Expr{Op: islandprogram.OpLitFloat, Type: islandprogram.TypeFloat, Value: v})
		return id
	}
	addS := func(v string) islandprogram.ExprID {
		id := islandprogram.ExprID(len(exprs))
		exprs = append(exprs, islandprogram.Expr{Op: islandprogram.OpLitString, Type: islandprogram.TypeString, Value: v})
		return id
	}
	prog.EngineNodes = []rootengine.Node{{Kind: "mesh", Geometry: "box", Props: map[string]islandprogram.ExprID{
		"kind": addS("box"), "x": addF("0"), "y": addF("0"), "z": addF("0"),
		"width": addF("1"), "height": addF("1.5"), "depth": addF("0.75"), "color": addS("#8de1ff"),
		"rotationX": addF("0.3"), "rotationY": addF("0.6"), "rotationZ": addF("0.1"), "spinY": addF("0.5"),
	}}}
	prog.Exprs = exprs
	return prog
}

// goldenBoxWorldPositions is the EXACT baked WorldPositions of goldenBoxProgram
// at t=1.0, captured under the merged motion-semantics path (base Euler orientation
// then spin quaternion, NOT the old perf Euler-add path). The motion path applies
// rotatePoint(p, rx, ry, rz) first, then QuatFromEuler(0, spinY*t, 0); the perf
// path used rotatePoint(p, rx, ry+spinY*t, rz). These are semantically different
// even for single-axis spin because Euler rotation is non-commutative: rotating
// around Y by (ry+spinY*t) ≠ rotating by ry then composing spinY*t-around-Y. This
// regeneration reflects the deliberate semantics change in feat/unified-motion-core.
var goldenBoxWorldPositions = []float64{
	-0.6872916281898391, -0.676543021254956, 0.15180500061386615,
	-0.23731399056954966, -0.5941469469382119, -0.737425572735281,
	-0.23731399056954966, -0.5941469469382119, -0.737425572735281,
	0.03109482237902103, 0.8566865260468388, -0.46716839374150315,
	0.03109482237902103, 0.8566865260468388, -0.46716839374150315,
	-0.41888281524126847, 0.7742904517300948, 0.42206217960764403,
	-0.41888281524126847, 0.7742904517300948, 0.42206217960764403,
	-0.6872916281898391, -0.676543021254956, 0.15180500061386615,
	-0.03109482237902103, -0.8566865260468388, 0.46716839374150315,
	0.41888281524126847, -0.7742904517300948, -0.42206217960764403,
	0.41888281524126847, -0.7742904517300948, -0.42206217960764403,
	0.6872916281898391, 0.676543021254956, -0.15180500061386615,
	0.6872916281898391, 0.676543021254956, -0.15180500061386615,
	0.23731399056954966, 0.5941469469382119, 0.737425572735281,
	0.23731399056954966, 0.5941469469382119, 0.737425572735281,
	-0.03109482237902103, -0.8566865260468388, 0.46716839374150315,
	-0.6872916281898391, -0.676543021254956, 0.15180500061386615,
	-0.03109482237902103, -0.8566865260468388, 0.46716839374150315,
	-0.23731399056954966, -0.5941469469382119, -0.737425572735281,
	0.41888281524126847, -0.7742904517300948, -0.42206217960764403,
	0.03109482237902103, 0.8566865260468388, -0.46716839374150315,
	0.6872916281898391, 0.676543021254956, -0.15180500061386615,
	-0.41888281524126847, 0.7742904517300948, 0.42206217960764403,
	0.23731399056954966, 0.5941469469382119, 0.737425572735281,
}

var goldenBoxBounds = rootengine.RenderBounds{
	MinX: -0.6872916281898391, MinY: -0.8566865260468388, MinZ: -0.737425572735281,
	MaxX: 0.6872916281898391, MaxY: 0.8566865260468388, MaxZ: 0.737425572735281,
}

// TestRenderBundleGoldenBitIdentical asserts the merged-motion-semantics RenderBundle
// world positions and object bounds are EXACTLY equal to the golden values captured
// under the motion path (base Euler + spin quaternion). This locks the bit-identical
// guarantee of the motion-semantics path end-to-end.
func TestRenderBundleGoldenBitIdentical(t *testing.T) {
	rt := NewSceneAdapter(goldenBoxProgram(), `{}`)
	rt.Reconcile()
	bundle := rt.RenderBundle(1280, 720, 1.0)

	if len(bundle.WorldPositions) != len(goldenBoxWorldPositions) {
		t.Fatalf("world position length = %d, want %d", len(bundle.WorldPositions), len(goldenBoxWorldPositions))
	}
	for i := range goldenBoxWorldPositions {
		if bundle.WorldPositions[i] != goldenBoxWorldPositions[i] {
			t.Fatalf("world position[%d] = %v, want %v (NOT bit-identical)", i, bundle.WorldPositions[i], goldenBoxWorldPositions[i])
		}
	}
	if len(bundle.Objects) != 1 {
		t.Fatalf("object count = %d, want 1", len(bundle.Objects))
	}
	if bundle.Objects[0].Bounds != goldenBoxBounds {
		t.Fatalf("bounds = %#v, want %#v (NOT bit-identical)", bundle.Objects[0].Bounds, goldenBoxBounds)
	}
}

// TestRenderBundleDeterministic asserts repeated bakes of the same scene yield
// identical world positions (no nondeterminism introduced by the hoist).
func TestRenderBundleDeterministic(t *testing.T) {
	rt := NewSceneAdapter(goldenBoxProgram(), `{}`)
	rt.Reconcile()
	a := rt.RenderBundle(1280, 720, 1.0)
	b := rt.RenderBundle(1280, 720, 1.0)
	if len(a.WorldPositions) != len(b.WorldPositions) {
		t.Fatalf("nondeterministic length: %d vs %d", len(a.WorldPositions), len(b.WorldPositions))
	}
	for i := range a.WorldPositions {
		if a.WorldPositions[i] != b.WorldPositions[i] {
			t.Fatalf("nondeterministic world position[%d]: %v vs %v", i, a.WorldPositions[i], b.WorldPositions[i])
		}
	}
}
