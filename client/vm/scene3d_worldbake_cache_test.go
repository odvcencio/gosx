package vm

import (
	"testing"

	rootengine "m31labs.dev/gosx/engine"
	islandprogram "m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// staticBoxProgram builds a single STATIC box (no spin, no drift) whose world
// positions are camera-independent and time-independent, making it eligible for
// the per-object world-bake cache.
func staticBoxProgram() *rootengine.Program {
	prog := &rootengine.Program{Name: "StaticBox"}
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
		"rotationX": addF("0.3"), "rotationY": addF("0.6"), "rotationZ": addF("0.1"),
	}}}
	prog.Exprs = exprs
	return prog
}

func worldPositionsEqual(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestWorldBakeCacheHitIdentity asserts that rebuilding a static scene with NOTHING
// changed serves the world positions/colors/bounds from cache (a recorded hit) and
// the two bundles are bit-identical.
func TestWorldBakeCacheHitIdentity(t *testing.T) {
	rt := NewSceneAdapter(staticBoxProgram(), `{}`)
	rt.Reconcile()

	a := rt.RenderBundle(1280, 720, 1.0)
	hitsBefore := rt.bakeHits
	missesBefore := rt.bakeMisses
	if missesBefore == 0 {
		t.Fatalf("expected at least one bake miss on first build, got %d", missesBefore)
	}

	b := rt.RenderBundle(1280, 720, 1.0)
	if rt.bakeHits == hitsBefore {
		t.Fatalf("expected a cache HIT on the second identical build, hits unchanged at %d", rt.bakeHits)
	}
	if rt.bakeMisses != missesBefore {
		t.Fatalf("expected no new misses on identical rebuild, misses %d -> %d", missesBefore, rt.bakeMisses)
	}

	if !worldPositionsEqual(a.WorldPositions, b.WorldPositions) {
		t.Fatalf("cache-hit world positions diverged from fresh bake")
	}
	if !worldPositionsEqual(a.WorldColors, b.WorldColors) {
		t.Fatalf("cache-hit world colors diverged from fresh bake")
	}
	if len(a.Objects) != 1 || len(b.Objects) != 1 {
		t.Fatalf("object count a=%d b=%d, want 1", len(a.Objects), len(b.Objects))
	}
	if a.Objects[0].Bounds != b.Objects[0].Bounds {
		t.Fatalf("cache-hit bounds %#v != fresh %#v", b.Objects[0].Bounds, a.Objects[0].Bounds)
	}
}

// TestWorldBakeCacheCameraMoveKeepsGeometry asserts moving ONLY the camera keeps
// the world positions identical (camera-independent) and the cache still hits.
func TestWorldBakeCacheCameraMoveKeepsGeometry(t *testing.T) {
	rt := NewSceneAdapter(staticBoxProgram(), `{}`)
	rt.Reconcile()

	a := rt.RenderBundle(1280, 720, 1.0)
	hitsBefore := rt.bakeHits

	// Move ONLY the camera, keeping the box fully in front of the near plane so no
	// near-plane CLIPPING changes (clipping is camera-dependent; the underlying
	// world BAKE is not). The cache must hit and the emitted world geometry must
	// be unchanged.
	rt.props["camera"] = map[string]any{"x": 1.5, "y": 0.5, "rotationY": 0.2}
	b := rt.RenderBundle(1280, 720, 1.0)

	if rt.bakeHits == hitsBefore {
		t.Fatalf("camera move must NOT invalidate the world bake; expected a cache hit")
	}
	if !worldPositionsEqual(a.WorldPositions, b.WorldPositions) {
		t.Fatalf("camera move changed world positions; world bake must be camera-independent")
	}
	if a.Objects[0].Bounds != b.Objects[0].Bounds {
		t.Fatalf("camera move changed world bounds; bounds must be camera-independent")
	}
}

// TestWorldBakeCachePropChangeInvalidates is the load-bearing correctness test: a
// position change driven through the reconcile path (advancing the node generation)
// must invalidate the cache and rebake, so the new world positions reflect the new
// position. A stale serve here would be a correctness hazard.
func TestWorldBakeCachePropChangeInvalidates(t *testing.T) {
	prog := &rootengine.Program{
		Name: "SignalBox",
		EngineNodes: []rootengine.Node{{
			Kind:     "mesh",
			Geometry: "box",
			Props: map[string]islandprogram.ExprID{
				"kind": 0, "x": 1, "width": 2, "height": 3, "depth": 4, "color": 5,
			},
		}},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitString, Value: "box", Type: islandprogram.TypeString},
			{Op: islandprogram.OpSignalGet, Value: "$scene.x", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$scene.x", Type: islandprogram.TypeFloat, Init: 6},
		},
	}
	rt := NewSceneAdapter(prog, `{}`)
	xSig := signal.New(FloatVal(0))
	rt.SetSharedSignal("$scene.x", xSig)
	rt.Reconcile()

	a := rt.RenderBundle(1280, 720, 0.0)
	// Cache-hit confirms the object is eligible and being served from cache.
	hitsBefore := rt.bakeHits
	_ = rt.RenderBundle(1280, 720, 0.0)
	if rt.bakeHits == hitsBefore {
		t.Fatalf("static object should be served from cache before the prop change")
	}

	// Change x through the reconcile path: marks the node dirty, advancing nodeGen.
	xSig.Set(FloatVal(5.0))
	missesBefore := rt.bakeMisses
	b := rt.RenderBundle(1280, 720, 0.0)

	if rt.bakeMisses == missesBefore {
		t.Fatalf("prop change must invalidate the cache (force a rebake miss); misses unchanged at %d", rt.bakeMisses)
	}
	if worldPositionsEqual(a.WorldPositions, b.WorldPositions) {
		t.Fatalf("prop change was NOT reflected: world positions identical after moving x by 5 (STALE GEOMETRY)")
	}
	// Every X world coordinate should have shifted by exactly +5 (camera unchanged,
	// only the object's translate changed). The bake is invalidated, not stale.
	if len(a.WorldPositions) != len(b.WorldPositions) || len(a.WorldPositions)%3 != 0 {
		t.Fatalf("world position length mismatch a=%d b=%d", len(a.WorldPositions), len(b.WorldPositions))
	}
	for i := 0; i < len(a.WorldPositions); i += 3 {
		if got := b.WorldPositions[i] - a.WorldPositions[i]; got != 5.0 {
			t.Fatalf("world X[%d] shifted by %v, want 5.0 (rebake must reflect new position exactly)", i, got)
		}
		if b.WorldPositions[i+1] != a.WorldPositions[i+1] || b.WorldPositions[i+2] != a.WorldPositions[i+2] {
			t.Fatalf("Y/Z changed at %d when only X moved", i)
		}
	}
}

// TestWorldBakeAnimatedObjectNeverCaches asserts a spinning object is rebaked every
// frame (its world positions change with time) and never served stale from cache.
func TestWorldBakeAnimatedObjectNeverCaches(t *testing.T) {
	prog := &rootengine.Program{Name: "SpinBox"}
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
		"width": addF("1"), "height": addF("1"), "depth": addF("1"), "color": addS("#8de1ff"),
		"rotationY": addF("0.2"), "spinY": addF("0.5"),
	}}}
	prog.Exprs = exprs

	rt := NewSceneAdapter(prog, `{}`)
	rt.Reconcile()

	a := rt.RenderBundle(1280, 720, 0.0)
	missesBefore := rt.bakeMisses
	hitsBefore := rt.bakeHits
	b := rt.RenderBundle(1280, 720, 1.0) // advance time; spin changes world positions

	if rt.bakeHits != hitsBefore {
		t.Fatalf("animated (spinning) object must NOT be served from cache; got a hit")
	}
	if rt.bakeMisses != missesBefore {
		// Animated objects skip the cache entirely (eligibility fails), so they
		// record neither a hit nor a miss — they just bake fresh.
		t.Fatalf("animated object should bypass the cache store entirely (no miss recorded), misses %d -> %d", missesBefore, rt.bakeMisses)
	}
	if worldPositionsEqual(a.WorldPositions, b.WorldPositions) {
		t.Fatalf("spinning object world positions identical across time; animation was served stale")
	}
}
