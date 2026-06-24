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
// at t=1.0, captured before the rotation-trig hoist. The hoist must reproduce
// these floats bit-for-bit (==, not approx); any FP reordering would break this.
var goldenBoxWorldPositions = []float64{
	-0.6794192470581879, -0.6768926780597374, 0.18256721807597753,
	-0.22808921688578138, -0.6316086274800725, -0.7086401419854579,
	-0.22808921688578138, -0.6316086274800725, -0.7086401419854579,
	0.021930071452600064, 0.8336767093541216, -0.5075699126687548,
	0.021930071452600064, 0.8336767093541216, -0.5075699126687548,
	-0.42939995871980646, 0.7883926587744566, 0.3836374473926805,
	-0.42939995871980646, 0.7883926587744566, 0.3836374473926805,
	-0.6794192470581879, -0.6768926780597374, 0.18256721807597753,
	-0.021930071452600064, -0.8336767093541216, 0.5075699126687548,
	0.42939995871980646, -0.7883926587744566, -0.3836374473926805,
	0.42939995871980646, -0.7883926587744566, -0.3836374473926805,
	0.6794192470581879, 0.6768926780597374, -0.18256721807597753,
	0.6794192470581879, 0.6768926780597374, -0.18256721807597753,
	0.22808921688578138, 0.6316086274800725, 0.7086401419854579,
	0.22808921688578138, 0.6316086274800725, 0.7086401419854579,
	-0.021930071452600064, -0.8336767093541216, 0.5075699126687548,
	-0.6794192470581879, -0.6768926780597374, 0.18256721807597753,
	-0.021930071452600064, -0.8336767093541216, 0.5075699126687548,
	-0.22808921688578138, -0.6316086274800725, -0.7086401419854579,
	0.42939995871980646, -0.7883926587744566, -0.3836374473926805,
	0.021930071452600064, 0.8336767093541216, -0.5075699126687548,
	0.6794192470581879, 0.6768926780597374, -0.18256721807597753,
	-0.42939995871980646, 0.7883926587744566, 0.3836374473926805,
	0.22808921688578138, 0.6316086274800725, 0.7086401419854579,
}

var goldenBoxBounds = rootengine.RenderBounds{
	MinX: -0.6794192470581879, MinY: -0.8336767093541216, MinZ: -0.7086401419854579,
	MaxX: 0.6794192470581879, MaxY: 0.8336767093541216, MaxZ: 0.7086401419854579,
}

// TestRenderBundleGoldenBitIdentical asserts the post-hoist RenderBundle world
// positions and object bounds are EXACTLY equal to the pre-hoist golden values.
// This locks the bit-identical guarantee of the trig hoist end-to-end.
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
