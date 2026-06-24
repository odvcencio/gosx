package vm

import (
	"math"
	"strings"
	"testing"

	rootengine "m31labs.dev/gosx/engine"
	islandprogram "m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/motion"
	"m31labs.dev/gosx/signal"
)

func TestSceneAdapterInitialReconcileCreatesObjects(t *testing.T) {
	prog := &rootengine.Program{
		Name: "GeometryZoo",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"x": 0,
					"y": 1,
					"z": 2,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":     3,
					"color": 4,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "-1.2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
		},
	}

	rt := NewSceneAdapter(prog, `{}`)
	commands := rt.Reconcile()
	if len(commands) != 2 {
		t.Fatalf("expected 2 create commands, got %d", len(commands))
	}
	if commands[0].Kind != rootengine.CommandCreateObject || commands[1].Kind != rootengine.CommandCreateObject {
		t.Fatalf("expected create commands, got %#v", commands)
	}
}

func TestSceneAdapterTickProducesIncrementalMaterialAndTransformCommands(t *testing.T) {
	prog := &rootengine.Program{
		Name: "GeometryZoo",
		EngineNodes: []rootengine.Node{
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":     0,
					"color": 1,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpSignalGet, Value: "$scene.x", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: "$scene.color", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$scene.x", Type: islandprogram.TypeFloat, Init: 2},
			{Name: "$scene.color", Type: islandprogram.TypeString, Init: 3},
		},
	}

	rt := NewSceneAdapter(prog, `{}`)
	xSig := signal.New(FloatVal(0))
	colorSig := signal.New(StringVal("#8de1ff"))
	rt.SetSharedSignal("$scene.x", xSig)
	rt.SetSharedSignal("$scene.color", colorSig)

	if commands := rt.Reconcile(); len(commands) != 1 {
		t.Fatalf("expected initial create command, got %d", len(commands))
	}

	xSig.Set(FloatVal(3.25))
	colorSig.Set(StringVal("#ff8f6b"))
	commands := rt.Reconcile()
	if len(commands) != 2 {
		t.Fatalf("expected transform + material commands, got %#v", commands)
	}
	if commands[0].Kind != rootengine.CommandSetTransform {
		t.Fatalf("expected first command to be transform, got %v", commands[0].Kind)
	}
	if commands[1].Kind != rootengine.CommandSetMaterial {
		t.Fatalf("expected second command to be material, got %v", commands[1].Kind)
	}
}

func TestSceneAdapterTickProducesIncrementalLightCommands(t *testing.T) {
	prog := &rootengine.Program{
		Name: "SceneLights",
		EngineNodes: []rootengine.Node{
			{
				Kind: "light",
				Props: map[string]islandprogram.ExprID{
					"kind":      0,
					"color":     1,
					"intensity": 2,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitString, Value: "directional", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "#f4fbff", Type: islandprogram.TypeString},
			{Op: islandprogram.OpSignalGet, Value: "$scene.light", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.8", Type: islandprogram.TypeFloat},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$scene.light", Type: islandprogram.TypeFloat, Init: 3},
		},
	}

	rt := NewSceneAdapter(prog, `{}`)
	intensitySig := signal.New(FloatVal(0.8))
	rt.SetSharedSignal("$scene.light", intensitySig)

	if commands := rt.Reconcile(); len(commands) != 1 || commands[0].Kind != rootengine.CommandCreateObject {
		t.Fatalf("expected initial create command, got %#v", commands)
	}

	intensitySig.Set(FloatVal(1.6))
	commands := rt.Reconcile()
	if len(commands) != 1 {
		t.Fatalf("expected one light command, got %#v", commands)
	}
	if commands[0].Kind != rootengine.CommandSetLight {
		t.Fatalf("expected SetLight command, got %v", commands[0].Kind)
	}
}

func TestSceneAdapterMarksOnlyDependentNodesDirty(t *testing.T) {
	prog := &rootengine.Program{
		Name: "DirtyTracking",
		EngineNodes: []rootengine.Node{
			{
				Kind: "mesh",
				Props: map[string]islandprogram.ExprID{
					"x": 0,
				},
			},
			{
				Kind: "mesh",
				Props: map[string]islandprogram.ExprID{
					"y": 1,
				},
			},
			{
				Kind:   "mesh",
				Static: true,
				Props: map[string]islandprogram.ExprID{
					"z": 2,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpSignalGet, Value: "$scene.x", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: "$scene.y", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: "$scene.static", Type: islandprogram.TypeFloat},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$scene.x", Type: islandprogram.TypeFloat, Init: 0},
			{Name: "$scene.y", Type: islandprogram.TypeFloat, Init: 1},
			{Name: "$scene.static", Type: islandprogram.TypeFloat, Init: 2},
		},
	}

	rt := NewSceneAdapter(prog, `{}`)
	rt.Reconcile()
	if got := rt.dirty; got[0] || got[1] || got[2] {
		t.Fatalf("expected clean runtime after initial reconcile, got %#v", got)
	}

	xSig := signal.New(FloatVal(0))
	rt.SetSharedSignal("$scene.x", xSig)
	clearDirty(rt.dirty)

	xSig.Set(FloatVal(2.5))
	if !rt.dirty[0] {
		t.Fatal("expected first node to be dirty after $scene.x change")
	}
	if rt.dirty[1] {
		t.Fatal("expected second node to remain clean after $scene.x change")
	}
	if rt.dirty[2] {
		t.Fatal("expected static node to remain clean after $scene.x change")
	}
}

func TestSceneAdapterClearsDirtyFlagsAfterReconcile(t *testing.T) {
	prog := &rootengine.Program{
		Name: "DirtyReconcile",
		EngineNodes: []rootengine.Node{
			{
				Kind: "mesh",
				Props: map[string]islandprogram.ExprID{
					"x":     0,
					"color": 1,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpSignalGet, Value: "$scene.x", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: "$scene.color", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$scene.x", Type: islandprogram.TypeFloat, Init: 2},
			{Name: "$scene.color", Type: islandprogram.TypeString, Init: 3},
		},
	}

	rt := NewSceneAdapter(prog, `{}`)
	xSig := signal.New(FloatVal(0))
	colorSig := signal.New(StringVal("#8de1ff"))
	rt.SetSharedSignal("$scene.x", xSig)
	rt.SetSharedSignal("$scene.color", colorSig)
	rt.Reconcile()

	xSig.Set(FloatVal(1.25))
	colorSig.Set(StringVal("#ffd48f"))
	if !rt.dirty[0] {
		t.Fatal("expected node to be dirty after shared signal changes")
	}

	commands := rt.Reconcile()
	if len(commands) != 2 {
		t.Fatalf("expected transform + material commands, got %#v", commands)
	}
	if rt.dirty[0] {
		t.Fatal("expected dirty flag to clear after reconcile")
	}
}

func TestSceneAdapterRenderBundleAppliesSceneMotionOffsets(t *testing.T) {
	prog := &rootengine.Program{
		Name: "MotionOffsets",
		EngineNodes: []rootengine.Node{
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":          0,
					"y":          1,
					"z":          2,
					"shiftX":     3,
					"shiftY":     4,
					"shiftZ":     5,
					"driftSpeed": 6,
					"driftPhase": 7,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.4", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.55", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.9", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.8", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.25", Type: islandprogram.TypeFloat},
		},
	}

	rt := NewSceneAdapter(prog, `{}`)
	start := rt.RenderBundle(640, 360, 0)
	later := rt.RenderBundle(640, 360, 1.8)
	if len(start.Objects) != 1 || len(later.Objects) != 1 {
		t.Fatalf("expected one render object in each bundle, got %#v and %#v", start.Objects, later.Objects)
	}

	startBounds := start.Objects[0].Bounds
	laterBounds := later.Objects[0].Bounds
	startCenterX := (startBounds.MinX + startBounds.MaxX) / 2
	startCenterY := (startBounds.MinY + startBounds.MaxY) / 2
	startCenterZ := (startBounds.MinZ + startBounds.MaxZ) / 2
	laterCenterX := (laterBounds.MinX + laterBounds.MaxX) / 2
	laterCenterY := (laterBounds.MinY + laterBounds.MaxY) / 2
	laterCenterZ := (laterBounds.MinZ + laterBounds.MaxZ) / 2

	if math.Abs(startCenterX-laterCenterX) < 0.001 {
		t.Fatalf("expected X center to drift, got start=%f later=%f", startCenterX, laterCenterX)
	}
	if math.Abs(startCenterY-laterCenterY) < 0.001 {
		t.Fatalf("expected Y center to drift, got start=%f later=%f", startCenterY, laterCenterY)
	}
	if math.Abs(startCenterZ-laterCenterZ) < 0.001 {
		t.Fatalf("expected Z center to drift, got start=%f later=%f", startCenterZ, laterCenterZ)
	}
}

// oldRotatePointEulerSpin replicates the PRE-unification spin path: base Euler
// rotation plus per-axis spin folded into the same intrinsic Euler rotation.
// Used purely as the regression oracle inside this test file.
func oldRotatePointEulerSpin(p point3, o sceneObject, t float64) point3 {
	return rotatePoint(p,
		o.RotationX+o.SpinX*t,
		o.RotationY+o.SpinY*t,
		o.RotationZ+o.SpinZ*t,
	)
}

// TestSpinQuatSingleAxisMatchesOldEulerPath is the REGRESSION ANCHOR.
// For single-axis spin the new motion.Eval-sourced quaternion path must produce
// vertex world positions byte-or-1e-9 identical to the old Euler-add path.
func TestSpinQuatSingleAxisMatchesOldEulerPath(t *testing.T) {
	const ts = 0.37
	const spinY = 0.9
	obj := sceneObject{
		ID:     "spinner",
		Kind:   "box",
		Width:  1.4,
		Height: 0.8,
		Depth:  1.1,
		X:      0.5,
		Y:      -0.25,
		Z:      0.75,
		SpinY:  spinY,
	}
	spinQ := spinQuatForObject(obj, ts)

	pts := []point3{
		{X: -0.7, Y: -0.4, Z: -0.55},
		{X: 0.7, Y: 0.4, Z: 0.55},
		{X: 0.3, Y: -0.2, Z: 0.5},
		{X: 0, Y: 0, Z: 0},
	}
	for _, p := range pts {
		got := translatePoint(p, obj, spinQ, clipTRS{}, ts)
		// Old path: base+spin Euler rotation, then translation, plus drift (none here).
		oldRot := oldRotatePointEulerSpin(p, obj, ts)
		want := point3{X: oldRot.X + obj.X, Y: oldRot.Y + obj.Y, Z: oldRot.Z + obj.Z}
		if math.Abs(got.X-want.X) > 1e-9 || math.Abs(got.Y-want.Y) > 1e-9 || math.Abs(got.Z-want.Z) > 1e-9 {
			t.Fatalf("single-axis spin diverged from old Euler path: p=%v got=%v want=%v", p, got, want)
		}
	}
}

// TestSpinQuatSourcedFromMotionEval asserts the spin quaternion the bundle uses
// is precisely the one the canonical motion evaluator (GenSpin) produces, and
// that applying it equals QuatFromEuler(0, spinY*t, 0) rotation of a vertex.
func TestSpinQuatSourcedFromMotionEval(t *testing.T) {
	const ts = 0.37
	const spinY = 0.9
	obj := sceneObject{ID: "spinner", Kind: "box", Width: 1, Height: 1, Depth: 1, SpinY: spinY}

	spinQ := spinQuatForObject(obj, ts)
	wantQ := motion.QuatFromEuler(0, spinY*ts, 0)
	if math.Abs(spinQ.X-wantQ.X) > 1e-12 || math.Abs(spinQ.Y-wantQ.Y) > 1e-12 ||
		math.Abs(spinQ.Z-wantQ.Z) > 1e-12 || math.Abs(spinQ.W-wantQ.W) > 1e-12 {
		t.Fatalf("spin quat not sourced from motion.Eval GenSpin: got %+v want %+v", spinQ, wantQ)
	}

	p := point3{X: 0.5, Y: 0.25, Z: -0.5}
	gx, gy, gz := motion.RotateVec3(spinQ, p.X, p.Y, p.Z)
	ex, ey, ez := motion.RotateVec3(motion.QuatFromEuler(0, spinY*ts, 0), p.X, p.Y, p.Z)
	if math.Abs(gx-ex) > 1e-12 || math.Abs(gy-ey) > 1e-12 || math.Abs(gz-ez) > 1e-12 {
		t.Fatalf("applied spin not from canonical evaluator: got (%v,%v,%v) want (%v,%v,%v)", gx, gy, gz, ex, ey, ez)
	}
}

// TestSpinQuatZeroTimeIsIdentity: at t=0 the spin quaternion is identity and the
// output equals base-rotation-only (unchanged endpoint behavior).
func TestSpinQuatZeroTimeIsIdentity(t *testing.T) {
	obj := sceneObject{ID: "s", Kind: "box", Width: 1, Height: 1, Depth: 1, SpinX: 1.3, SpinY: -0.7, SpinZ: 2.1, RotationY: 0.4}
	spinQ := spinQuatForObject(obj, 0)
	ident := motion.Quat{X: 0, Y: 0, Z: 0, W: 1}
	if math.Abs(spinQ.X-ident.X) > 1e-12 || math.Abs(spinQ.Y-ident.Y) > 1e-12 ||
		math.Abs(spinQ.Z-ident.Z) > 1e-12 || math.Abs(spinQ.W-ident.W) > 1e-12 {
		t.Fatalf("t=0 spin quat not identity: %+v", spinQ)
	}
	p := point3{X: 0.5, Y: -0.3, Z: 0.2}
	got := translatePoint(p, obj, spinQ, clipTRS{}, 0)
	base := rotatePoint(p, obj.RotationX, obj.RotationY, obj.RotationZ)
	want := point3{X: base.X + obj.X, Y: base.Y + obj.Y, Z: base.Z + obj.Z}
	if math.Abs(got.X-want.X) > 1e-12 || math.Abs(got.Y-want.Y) > 1e-12 || math.Abs(got.Z-want.Z) > 1e-12 {
		t.Fatalf("t=0 output not base-rotation-only: got=%v want=%v", got, want)
	}
}

// TestSpinQuatBaseRotationPreserved: an object with base rotation and no spin
// produces output identical to the base rotatePoint path (base path untouched).
func TestSpinQuatBaseRotationPreserved(t *testing.T) {
	const ts = 1.25
	obj := sceneObject{ID: "b", Kind: "box", Width: 1, Height: 1, Depth: 1, X: 1, Y: 2, Z: 3, RotationX: 0.3, RotationY: 0.6, RotationZ: -0.4}
	spinQ := spinQuatForObject(obj, ts) // no spin → identity
	if spinQ != (motion.Quat{X: 0, Y: 0, Z: 0, W: 1}) {
		t.Fatalf("no-spin object must yield identity quat, got %+v", spinQ)
	}
	p := point3{X: -0.5, Y: 0.5, Z: 0.5}
	got := translatePoint(p, obj, spinQ, clipTRS{}, ts)
	base := rotatePoint(p, obj.RotationX, obj.RotationY, obj.RotationZ)
	want := point3{X: base.X + obj.X, Y: base.Y + obj.Y, Z: base.Z + obj.Z}
	if math.Abs(got.X-want.X) > 1e-12 || math.Abs(got.Y-want.Y) > 1e-12 || math.Abs(got.Z-want.Z) > 1e-12 {
		t.Fatalf("base rotation not preserved: got=%v want=%v", got, want)
	}
}

// TestSpinQuatNormalSingleAxisMatchesOld: world normals under single-axis spin
// must match the old Euler-spin path (normals get base+spin, no translation).
func TestSpinQuatNormalSingleAxisMatchesOld(t *testing.T) {
	const ts = 0.37
	const spinX = 1.1
	obj := sceneObject{ID: "n", Kind: "box", Width: 1.4, Height: 0.8, Depth: 1.1, SpinX: spinX}
	spinQ := spinQuatForObject(obj, ts)

	p := point3{X: 0.7, Y: 0.1, Z: -0.2} // off-axis so a definite face normal is picked
	got := sceneObjectWorldNormal(obj, p, spinQ, clipTRS{})
	// Old path: local normal then base+spin Euler rotation, normalized.
	local := sceneObjectLocalNormal(obj, p)
	want := normalizePoint3(oldRotatePointEulerSpin(local, obj, ts))
	if math.Abs(got.X-want.X) > 1e-9 || math.Abs(got.Y-want.Y) > 1e-9 || math.Abs(got.Z-want.Z) > 1e-9 {
		t.Fatalf("world normal single-axis spin diverged: got=%v want=%v", got, want)
	}
}

// TestSpinQuatMultiAxisIsCanonical documents the INTENDED change: multi-axis
// spin now follows canonical quaternion order (qx*qy*qz), NOT the old Euler-add.
// Assert equality with the canonical evaluator result (not the old path).
func TestSpinQuatMultiAxisIsCanonical(t *testing.T) {
	const ts = 0.37
	obj := sceneObject{ID: "m", Kind: "box", Width: 1, Height: 1, Depth: 1, SpinX: 0.5, SpinY: 0.9, SpinZ: -0.7}
	spinQ := spinQuatForObject(obj, ts)
	canonical := motion.QuatFromEuler(0.5*ts, 0.9*ts, -0.7*ts)
	if math.Abs(spinQ.X-canonical.X) > 1e-12 || math.Abs(spinQ.Y-canonical.Y) > 1e-12 ||
		math.Abs(spinQ.Z-canonical.Z) > 1e-12 || math.Abs(spinQ.W-canonical.W) > 1e-12 {
		t.Fatalf("multi-axis spin quat not canonical qx*qy*qz: got %+v want %+v", spinQ, canonical)
	}

	p := point3{X: 0.3, Y: -0.4, Z: 0.5}
	gx, gy, gz := motion.RotateVec3(spinQ, p.X, p.Y, p.Z)
	wx, wy, wz := motion.RotateVec3(canonical, p.X, p.Y, p.Z)
	if math.Abs(gx-wx) > 1e-12 || math.Abs(gy-wy) > 1e-12 || math.Abs(gz-wz) > 1e-12 {
		t.Fatalf("multi-axis applied spin not canonical: got (%v,%v,%v) want (%v,%v,%v)", gx, gy, gz, wx, wy, wz)
	}

	// And it must DIFFER from the old Euler-add path (this is the intended change).
	old := oldRotatePointEulerSpin(p, obj, ts)
	if math.Abs(gx-old.X) < 1e-6 && math.Abs(gy-old.Y) < 1e-6 && math.Abs(gz-old.Z) < 1e-6 {
		t.Fatalf("multi-axis spin unexpectedly matched old Euler path; order change not observed")
	}
}

// TestSceneAdapterRenderBundleSingleAxisSpinUnchanged exercises the FULL
// production bundle path: a single-axis spinY object's bundle bounds must equal
// the bounds computed from the old Euler-spin path applied to box geometry.
func TestSceneAdapterRenderBundleSingleAxisSpinUnchanged(t *testing.T) {
	const ts = 0.37
	const spinY = 0.9
	prog := &rootengine.Program{
		Name: "SingleAxisSpin",
		EngineNodes: []rootengine.Node{
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":      0,
					"y":      1,
					"z":      2,
					"width":  3,
					"height": 4,
					"depth":  5,
					"spinY":  6,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "0.5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "-0.25", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.75", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.4", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.8", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.9", Type: islandprogram.TypeFloat},
		},
	}
	rt := NewSceneAdapter(prog, `{}`)
	bundle := rt.RenderBundle(640, 360, ts)
	if len(bundle.Objects) != 1 {
		t.Fatalf("expected one render object, got %d", len(bundle.Objects))
	}
	gotBounds := bundle.Objects[0].Bounds

	// Compute expected bounds from the OLD Euler-spin path over box segments.
	obj := sceneObject{ID: bundle.Objects[0].ID, Kind: "box", Width: 1.4, Height: 0.8, Depth: 1.1, X: 0.5, Y: -0.25, Z: 0.75, SpinY: spinY}
	first := true
	var want rootengine.RenderBounds
	expand := func(p point3) {
		if first {
			want = rootengine.RenderBounds{MinX: p.X, MinY: p.Y, MinZ: p.Z, MaxX: p.X, MaxY: p.Y, MaxZ: p.Z}
			first = false
			return
		}
		want.MinX = math.Min(want.MinX, p.X)
		want.MinY = math.Min(want.MinY, p.Y)
		want.MinZ = math.Min(want.MinZ, p.Z)
		want.MaxX = math.Max(want.MaxX, p.X)
		want.MaxY = math.Max(want.MaxY, p.Y)
		want.MaxZ = math.Max(want.MaxZ, p.Z)
	}
	for _, seg := range sceneObjectSegments(obj) {
		for _, v := range seg {
			rot := oldRotatePointEulerSpin(v, obj, ts)
			expand(point3{X: rot.X + obj.X, Y: rot.Y + obj.Y, Z: rot.Z + obj.Z})
		}
	}

	if math.Abs(gotBounds.MinX-want.MinX) > 1e-9 || math.Abs(gotBounds.MaxX-want.MaxX) > 1e-9 ||
		math.Abs(gotBounds.MinY-want.MinY) > 1e-9 || math.Abs(gotBounds.MaxY-want.MaxY) > 1e-9 ||
		math.Abs(gotBounds.MinZ-want.MinZ) > 1e-9 || math.Abs(gotBounds.MaxZ-want.MaxZ) > 1e-9 {
		t.Fatalf("single-axis spin bundle bounds diverged from old path:\n got=%+v\nwant=%+v", gotBounds, want)
	}
}

func TestSceneAdapterRenderBundleSyncsDirtyNodes(t *testing.T) {
	prog := &rootengine.Program{
		Name: "RenderBundle",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z":   0,
					"fov": 1,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":     2,
					"color": 3,
					"size":  4,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "75", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: "$scene.x", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: "$scene.color", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "1.4", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$scene.x", Type: islandprogram.TypeFloat, Init: 5},
			{Name: "$scene.color", Type: islandprogram.TypeString, Init: 6},
		},
	}

	rt := NewSceneAdapter(prog, `{"background":"#102030"}`)
	xSig := signal.New(FloatVal(0))
	colorSig := signal.New(StringVal("#8de1ff"))
	rt.SetSharedSignal("$scene.x", xSig)
	rt.SetSharedSignal("$scene.color", colorSig)
	rt.Reconcile()

	xSig.Set(FloatVal(2.25))
	colorSig.Set(StringVal("#ff8f6b"))
	if !rt.dirty[1] {
		t.Fatal("expected mesh node to be dirty before render bundle generation")
	}

	bundle := rt.RenderBundle(640, 360, 1.5)
	if bundle.Background != "#102030" {
		t.Fatalf("expected background from props, got %q", bundle.Background)
	}
	if bundle.Camera.Z != 6 {
		t.Fatalf("expected default camera to flow into bundle, got %#v", bundle.Camera)
	}
	if bundle.Camera.Near != 0.05 || bundle.Camera.Far != 128 {
		t.Fatalf("expected default clip planes in render bundle camera, got %#v", bundle.Camera)
	}
	if bundle.ObjectCount != 1 {
		t.Fatalf("expected 1 object, got %d", bundle.ObjectCount)
	}
	if len(bundle.Materials) != 1 {
		t.Fatalf("expected one resolved material, got %#v", bundle.Materials)
	}
	if len(bundle.Passes) < 2 {
		t.Fatalf("expected prebatched render passes, got %#v", bundle.Passes)
	}
	if len(bundle.Objects) != 1 {
		t.Fatalf("expected one render object, got %#v", bundle.Objects)
	}
	if bundle.Objects[0].MaterialIndex != 0 {
		t.Fatalf("expected render object to reference first material, got %#v", bundle.Objects[0])
	}
	if bundle.Objects[0].RenderPass != "opaque" {
		t.Fatalf("expected render object to carry resolved render pass, got %#v", bundle.Objects[0])
	}
	if bundle.Objects[0].DepthNear <= 0 || bundle.Objects[0].DepthFar <= bundle.Objects[0].DepthNear {
		t.Fatalf("expected render object depth metadata, got %#v", bundle.Objects[0])
	}
	if bundle.Objects[0].Bounds.MaxX <= bundle.Objects[0].Bounds.MinX || bundle.Objects[0].Bounds.MaxZ <= bundle.Objects[0].Bounds.MinZ {
		t.Fatalf("expected render object bounds metadata, got %#v", bundle.Objects[0])
	}
	if bundle.Objects[0].ViewCulled {
		t.Fatalf("expected visible object to stay in-bounds, got %#v", bundle.Objects[0])
	}
	if bundle.VertexCount == 0 {
		t.Fatal("expected projected vertices in render bundle")
	}
	if len(bundle.Positions) != bundle.VertexCount*2 {
		t.Fatalf("expected positions sized to vertex count, got %d for %d vertices", len(bundle.Positions), bundle.VertexCount)
	}
	if len(bundle.Colors) != bundle.VertexCount*4 {
		t.Fatalf("expected colors sized to vertex count, got %d for %d vertices", len(bundle.Colors), bundle.VertexCount)
	}
	if bundle.WorldVertexCount == 0 {
		t.Fatal("expected world vertices in render bundle")
	}
	if len(bundle.WorldPositions) != bundle.WorldVertexCount*3 {
		t.Fatalf("expected world positions sized to world vertex count, got %d for %d vertices", len(bundle.WorldPositions), bundle.WorldVertexCount)
	}
	if len(bundle.WorldColors) != bundle.WorldVertexCount*4 {
		t.Fatalf("expected world colors sized to world vertex count, got %d for %d vertices", len(bundle.WorldColors), bundle.WorldVertexCount)
	}
	if rt.dirty[1] {
		t.Fatal("expected render bundle generation to sync dirty node snapshot")
	}
	if bundle.Passes[0].Name != "staticOpaque" || bundle.Passes[0].CacheKey == "" {
		t.Fatalf("expected static opaque pass with cache key, got %#v", bundle.Passes[0])
	}
}

func TestSceneAdapterRenderBundleProjectsSceneLabels(t *testing.T) {
	prog := &rootengine.Program{
		Name: "SceneLabels",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z":   0,
					"fov": 1,
				},
			},
			{
				Kind: "label",
				Props: map[string]islandprogram.ExprID{
					"text":       2,
					"x":          3,
					"y":          4,
					"z":          5,
					"maxWidth":   6,
					"lineHeight": 7,
					"textAlign":  8,
					"anchorX":    9,
					"anchorY":    10,
					"className":  11,
					"priority":   12,
					"collision":  13,
					"occlude":    14,
					"maxLines":   15,
					"overflow":   16,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "72", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "Scene labels make overlays first-class.", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.4", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "184", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "18", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "center", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "hero-badge", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "4", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "avoid", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitBool, Value: "true", Type: islandprogram.TypeBool},
			{Op: islandprogram.OpLitFloat, Value: "2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "ellipsis", Type: islandprogram.TypeString},
		},
	}

	rt := NewSceneAdapter(prog, `{}`)
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Labels) != 1 {
		t.Fatalf("expected one projected label, got %#v", bundle.Labels)
	}

	label := bundle.Labels[0]
	if label.Text != "Scene labels make overlays first-class." {
		t.Fatalf("unexpected label text: %q", label.Text)
	}
	if label.Position.X < 250 || label.Position.X > 390 {
		t.Fatalf("expected projected X near center, got %#v", label.Position)
	}
	if label.Position.Y >= 180 {
		t.Fatalf("expected label above center point, got %#v", label.Position)
	}
	if label.MaxWidth != 184 {
		t.Fatalf("expected max width to flow into bundle, got %#v", label)
	}
	if label.LineHeight != 18 {
		t.Fatalf("expected line height to flow into bundle, got %#v", label)
	}
	if label.TextAlign != "center" {
		t.Fatalf("expected text alignment to flow into bundle, got %#v", label)
	}
	if label.AnchorX != 0.5 || label.AnchorY != 1 {
		t.Fatalf("expected anchor metadata in bundle, got %#v", label)
	}
	if label.ClassName != "hero-badge" {
		t.Fatalf("expected class metadata in bundle, got %#v", label)
	}
	if label.Priority != 4 || label.Collision != "avoid" || !label.Occlude {
		t.Fatalf("expected placement metadata in bundle, got %#v", label)
	}
	if label.MaxLines != 2 || label.Overflow != "ellipsis" {
		t.Fatalf("expected overflow metadata in bundle, got %#v", label)
	}
}

func TestSceneAdapterRenderBundleProjectsSceneSprites(t *testing.T) {
	prog := &rootengine.Program{
		Name: "SceneSprites",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z":   0,
					"fov": 1,
				},
			},
			{
				Kind: "sprite",
				Props: map[string]islandprogram.ExprID{
					"src":       2,
					"x":         3,
					"y":         4,
					"z":         5,
					"width":     6,
					"height":    7,
					"scale":     8,
					"opacity":   9,
					"className": 10,
					"priority":  11,
					"anchorX":   12,
					"anchorY":   13,
					"fit":       14,
					"occlude":   15,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "72", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "/paper-card.png", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.25", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.55", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.02", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.94", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "hero-card", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "3", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "cover", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitBool, Value: "true", Type: islandprogram.TypeBool},
		},
	}

	rt := NewSceneAdapter(prog, `{}`)
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Sprites) != 1 {
		t.Fatalf("expected one projected sprite, got %#v", bundle.Sprites)
	}
	sprite := bundle.Sprites[0]
	if sprite.Src != "/paper-card.png" {
		t.Fatalf("unexpected sprite src: %#v", sprite)
	}
	if sprite.Position.X < 250 || sprite.Position.X > 410 {
		t.Fatalf("expected sprite projected near center, got %#v", sprite.Position)
	}
	if sprite.Width <= 30 || sprite.Height <= 20 {
		t.Fatalf("expected projected sprite dimensions, got %#v", sprite)
	}
	if sprite.ClassName != "hero-card" || sprite.Fit != "cover" || !sprite.Occlude {
		t.Fatalf("expected sprite metadata in bundle, got %#v", sprite)
	}
	if sprite.Opacity != 0.94 || sprite.AnchorX != 0.5 || sprite.AnchorY != 0.5 {
		t.Fatalf("expected sprite presentation metadata in bundle, got %#v", sprite)
	}
}

func TestSceneAdapterRenderBundleResolvesMaterialPresets(t *testing.T) {
	prog := &rootengine.Program{
		Name: "MaterialProfiles",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z": 0,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "ghost",
				Props: map[string]islandprogram.ExprID{
					"size":  1,
					"color": 2,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "sphere",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"radius":    3,
					"color":     4,
					"opacity":   5,
					"blendMode": 6,
					"emissive":  7,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "pyramid",
				Material: "glow",
				Props: map[string]islandprogram.ExprID{
					"size":  8,
					"color": 9,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.9", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#ffd48f", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "opaque", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.35", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#ff9cff", Type: islandprogram.TypeString},
		},
	}

	rt := NewSceneAdapter(prog, "")
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Materials) != 3 {
		t.Fatalf("expected three materials, got %#v", bundle.Materials)
	}

	ghost := bundle.Materials[0]
	if ghost.Kind != "ghost" || ghost.BlendMode != "alpha" || ghost.RenderPass != "alpha" || ghost.Opacity >= 1 || ghost.Emissive <= 0 || ghost.Key == "" || len(ghost.ShaderData) != 3 || ghost.ShaderData[0] != 1 {
		t.Fatalf("expected ghost preset material, got %#v", ghost)
	}

	flat := bundle.Materials[1]
	if flat.Kind != "flat" || flat.BlendMode != "alpha" || flat.RenderPass != "alpha" || flat.Opacity != 0.6 || flat.Emissive != 0.35 || flat.Key == "" || len(flat.ShaderData) != 3 || flat.ShaderData[0] != 0 {
		t.Fatalf("expected explicit flat material overrides, got %#v", flat)
	}

	glow := bundle.Materials[2]
	if glow.Kind != "glow" || glow.BlendMode != "additive" || glow.RenderPass != "additive" || glow.Opacity <= 0.5 || glow.Emissive <= 0 || glow.Key == "" || len(glow.ShaderData) != 3 || glow.ShaderData[0] != 3 {
		t.Fatalf("expected glow preset material, got %#v", glow)
	}
}

func TestSceneAdapterRenderBundleUsesRegisteredMaterialProfile(t *testing.T) {
	cleanup := RegisterMaterialProfile("cloth", MaterialProfile{
		Opacity:       0.64,
		HasOpacity:    true,
		BlendMode:     "alpha",
		HasBlendMode:  true,
		Emissive:      0.18,
		HasEmissive:   true,
		Clearcoat:     0.22,
		HasClearcoat:  true,
		Anisotropy:    0.4,
		HasAnisotropy: true,
		ShaderData:    []float64{7, 0.18, 0.44},
	})
	defer cleanup()

	prog := &rootengine.Program{
		Name: "CustomMaterialProfile",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z": 0,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "cloth",
				Props: map[string]islandprogram.ExprID{
					"size":  1,
					"color": 2,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#d8b4fe", Type: islandprogram.TypeString},
		},
	}

	rt := NewSceneAdapter(prog, "")
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Materials) != 1 {
		t.Fatalf("expected one material, got %#v", bundle.Materials)
	}
	material := bundle.Materials[0]
	if material.Kind != "cloth" || material.Opacity != 0.64 || material.BlendMode != "alpha" || material.RenderPass != "alpha" || material.Emissive != 0.18 || material.Clearcoat != 0.22 || material.Anisotropy != 0.4 {
		t.Fatalf("expected registered cloth defaults, got %#v", material)
	}
	if len(material.ShaderData) != 3 || material.ShaderData[0] != 7 || material.ShaderData[2] != 0.44 {
		t.Fatalf("expected registered cloth shader data, got %#v", material.ShaderData)
	}
}

func TestSceneAdapterRenderBundlePreservesCustomWGSLMaterial(t *testing.T) {
	prog := &rootengine.Program{
		Name: "CustomWGSLMaterial",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z": 0,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "custom",
				Props: map[string]islandprogram.ExprID{
					"size":               1,
					"color":              2,
					"customVertexWGSL":   3,
					"customFragmentWGSL": 4,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#f5c76b", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "fn gosx_vertex() {}", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "fn gosx_fragment() -> vec4f { return vec4f(1.0); }", Type: islandprogram.TypeString},
		},
	}

	rt := NewSceneAdapter(prog, "")
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Materials) != 1 {
		t.Fatalf("expected one material, got %#v", bundle.Materials)
	}
	material := bundle.Materials[0]
	if material.Kind != "custom" || material.Color != "#f5c76b" {
		t.Fatalf("expected custom material, got %#v", material)
	}
	if material.CustomVertexWGSL != "fn gosx_vertex() {}" {
		t.Fatalf("CustomVertexWGSL = %q", material.CustomVertexWGSL)
	}
	if material.CustomFragmentWGSL != "fn gosx_fragment() -> vec4f { return vec4f(1.0); }" {
		t.Fatalf("CustomFragmentWGSL = %q", material.CustomFragmentWGSL)
	}
	if !strings.Contains(material.Key, "fn gosx_fragment") {
		t.Fatalf("material key should include custom WGSL, got %q", material.Key)
	}
}

func TestResolveRenderMaterialPreservesShaderDescriptor(t *testing.T) {
	material := resolveRenderMaterial(sceneObject{
		Material:           "custom",
		CustomVertexWGSL:   "@vertex fn vertexMain() -> @builtin(position) vec4f { return vec4f(0.0); }",
		CustomFragmentWGSL: "@fragment fn fragmentMain() -> @location(0) vec4f { return vec4f(1.0); }",
		ShaderBackend:      "selena",
		ShaderLayout: map[string]any{
			"schemaVersion": "selena.descriptor.v1",
			"material":      "Defaults",
		},
	})
	if material.ShaderBackend != "selena" {
		t.Fatalf("ShaderBackend = %q, want selena", material.ShaderBackend)
	}
	if material.ShaderLayout["material"] != "Defaults" {
		t.Fatalf("ShaderLayout = %#v", material.ShaderLayout)
	}
	if !strings.Contains(material.Key, "selena.descriptor.v1") {
		t.Fatalf("material key should include shader layout, got %q", material.Key)
	}
}

func TestSceneAdapterRenderBundlePreservesPBRMaterialFields(t *testing.T) {
	prog := &rootengine.Program{
		Name: "PBRMaterial",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z": 0,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "sphere",
				Material: "standard",
				Props: map[string]islandprogram.ExprID{
					"size":         1,
					"color":        2,
					"roughness":    3,
					"metalness":    4,
					"texture":      5,
					"normalMap":    6,
					"roughnessMap": 7,
					"metalnessMap": 8,
					"emissive":     9,
					"emissiveMap":  10,
					"clearcoat":    11,
					"sheen":        12,
					"transmission": 13,
					"iridescence":  14,
					"anisotropy":   15,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#77c6ff", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.32", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.8", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "/albedo.webp", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "/normal.webp", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "/roughness.webp", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "/metalness.webp", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.27", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "/emissive.webp", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.35", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.12", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.18", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "-0.25", Type: islandprogram.TypeFloat},
		},
	}

	rt := NewSceneAdapter(prog, "")
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Materials) != 1 {
		t.Fatalf("expected one material, got %#v", bundle.Materials)
	}
	material := bundle.Materials[0]
	if material.Kind != "standard" || material.Color != "#77c6ff" {
		t.Fatalf("unexpected material identity: %#v", material)
	}
	if material.Roughness != 0.32 || material.Metalness != 0.8 || material.Emissive != 0.27 {
		t.Fatalf("PBR scalar fields were not preserved: %#v", material)
	}
	if material.Clearcoat != 0.35 || material.Sheen != 0.2 || material.Transmission != 0.12 || material.Iridescence != 0.18 || material.Anisotropy != -0.25 {
		t.Fatalf("physical PBR fields were not preserved: %#v", material)
	}
	if material.Texture != "/albedo.webp" || material.NormalMap != "/normal.webp" || material.RoughnessMap != "/roughness.webp" || material.MetalnessMap != "/metalness.webp" || material.EmissiveMap != "/emissive.webp" {
		t.Fatalf("PBR texture maps were not preserved: %#v", material)
	}
	for _, fragment := range []string{"/normal.webp", "/roughness.webp", "/metalness.webp", "/emissive.webp", "0.320", "0.800", "0.350", "0.200", "0.120", "0.180", "-0.250"} {
		if !strings.Contains(material.Key, fragment) {
			t.Fatalf("material key %q does not include %q", material.Key, fragment)
		}
	}
}

func TestSceneAdapterRenderBundlePropagatesNativePostEffects(t *testing.T) {
	props := `{
		"scene": {
			"postEffects": [
				{"kind": "bloom", "threshold": 0.7, "intensity": 0.45, "radius": 6, "scale": 0.5},
				{"kind": "dof", "focusDistance": 7, "aperture": 0.05, "maxBlur": 4},
				{"kind": "vignette", "intensity": 0.2},
				{"kind": "colorGrade", "exposure": 1.1, "contrast": 0.9, "saturation": 0.8},
				{"kind": "toneMapping", "mode": "reinhard", "exposure": 1.2}
			],
			"postFXMaxPixels": 921600
		}
	}`
	rt := NewSceneAdapter(&rootengine.Program{}, props)
	bundle := rt.RenderBundle(640, 360, 0)

	if bundle.PostFXMaxPixels != 921600 {
		t.Fatalf("PostFXMaxPixels = %d, want 921600", bundle.PostFXMaxPixels)
	}
	if len(bundle.PostEffects) != 5 {
		t.Fatalf("PostEffects = %#v, want bloom, native DOF, vignette, colorGrade, and preserved toneMapping", bundle.PostEffects)
	}
	bloom := bundle.PostEffects[0]
	if bloom.Kind != "bloom" || bloom.Threshold != 0.7 || bloom.Intensity != 0.45 || bloom.Radius != 6 || bloom.Scale != 0.5 {
		t.Fatalf("unexpected bloom effect: %#v", bloom)
	}
	if bloom.Params["threshold"] != 0.7 || bloom.Params["intensity"] != 0.45 {
		t.Fatalf("bloom params = %#v", bloom.Params)
	}
	dof := bundle.PostEffects[1]
	if dof.Kind != "dof" || dof.Params["focusDistance"] != 7 || dof.Params["aperture"] != 0.05 || dof.Params["maxBlur"] != 4 {
		t.Fatalf("DOF should be preserved with params, got %#v", dof)
	}
	vignette := bundle.PostEffects[2]
	if vignette.Kind != "vignette" || vignette.Params["intensity"] != 0.2 {
		t.Fatalf("vignette should be preserved with params, got %#v", vignette)
	}
	colorGrade := bundle.PostEffects[3]
	if colorGrade.Kind != "colorGrade" || colorGrade.Params["exposure"] != 1.1 || colorGrade.Params["contrast"] != 0.9 || colorGrade.Params["saturation"] != 0.8 {
		t.Fatalf("color grade should be preserved with params, got %#v", colorGrade)
	}
	toneMapping := bundle.PostEffects[4]
	if toneMapping.Kind != "toneMapping" || toneMapping.Mode != "reinhard" || toneMapping.Params["exposure"] != 1.2 {
		t.Fatalf("toneMapping should be preserved with params, got %#v", toneMapping)
	}
	if len(bundle.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %#v, want all listed post-FX supported by native engine VM", bundle.Diagnostics)
	}
}

func TestSceneAdapterRenderBundlePreservesSceneAnimations(t *testing.T) {
	props := `{
		"scene": {
			"animations": [{
				"name": "pulse",
				"duration": 1.5,
				"channels": [{
					"targetNode": 4,
					"property": "rotationY",
					"times": [0, 1.5],
					"values": [0, 3.14],
					"interpolation": "LINEAR"
				}]
			}]
		}
	}`
	rt := NewSceneAdapter(&rootengine.Program{}, props)
	bundle := rt.RenderBundle(640, 360, 0)

	if len(bundle.Animations) != 1 {
		t.Fatalf("Animations = %#v, want one clip", bundle.Animations)
	}
	clip := bundle.Animations[0]
	if clip.Name != "pulse" || clip.Duration != 1.5 || len(clip.Channels) != 1 {
		t.Fatalf("animation clip = %#v", clip)
	}
	channel := clip.Channels[0]
	if channel.TargetID != "4" || channel.Property != "rotationY" || channel.Interpolation != "LINEAR" {
		t.Fatalf("animation channel = %#v", channel)
	}
	if len(channel.Times) != 2 || channel.Times[1] != 1.5 || len(channel.Values) != 2 || channel.Values[1] != 3.14 {
		t.Fatalf("animation keyframes = times %#v values %#v", channel.Times, channel.Values)
	}
}

func TestSceneAdapterRenderBundleEmitsTexturedPlaneSurfaces(t *testing.T) {
	prog := &rootengine.Program{
		Name: "TexturedPlane",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z":   0,
					"fov": 1,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "plane",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"width":     2,
					"height":    3,
					"texture":   4,
					"wireframe": 5,
					"y":         6,
					"z":         7,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "72", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.55", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.02", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "/paper-card.png", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitBool, Value: "false", Type: islandprogram.TypeBool},
			{Op: islandprogram.OpLitFloat, Value: "0.5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.25", Type: islandprogram.TypeFloat},
		},
	}

	rt := NewSceneAdapter(prog, "")
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Materials) != 1 {
		t.Fatalf("expected one resolved material, got %#v", bundle.Materials)
	}
	if bundle.Materials[0].Texture != "/paper-card.png" {
		t.Fatalf("expected texture to flow into resolved material, got %#v", bundle.Materials[0])
	}
	if len(bundle.Surfaces) != 1 {
		t.Fatalf("expected one textured surface, got %#v", bundle.Surfaces)
	}
	surface := bundle.Surfaces[0]
	if surface.Kind != "plane" {
		t.Fatalf("expected plane surface, got %#v", surface)
	}
	if surface.MaterialIndex != 0 {
		t.Fatalf("expected surface to reference first material, got %#v", surface)
	}
	if surface.RenderPass != "opaque" {
		t.Fatalf("expected opaque textured surface, got %#v", surface)
	}
	if surface.VertexCount != 6 {
		t.Fatalf("expected two surface triangles, got %#v", surface)
	}
	if len(surface.Positions) != 18 || len(surface.UV) != 12 {
		t.Fatalf("expected surface vertex buffers, got %#v", surface)
	}
	if surface.ViewCulled {
		t.Fatalf("expected textured plane to remain visible, got %#v", surface)
	}
}

func TestSceneAdapterRenderBundleCarriesPickabilityMetadata(t *testing.T) {
	prog := &rootengine.Program{
		Name: "Pickability",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z": 0,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":        1,
					"pickable": 2,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":        3,
					"pickable": 4,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "-1.5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitBool, Value: "false", Type: islandprogram.TypeBool},
			{Op: islandprogram.OpLitFloat, Value: "1.5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitBool, Value: "true", Type: islandprogram.TypeBool},
		},
	}

	rt := NewSceneAdapter(prog, "")
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Objects) != 2 {
		t.Fatalf("expected two render objects, got %#v", bundle.Objects)
	}
	if bundle.Objects[0].Pickable == nil || *bundle.Objects[0].Pickable {
		t.Fatalf("expected explicit non-pickable metadata, got %#v", bundle.Objects[0].Pickable)
	}
	if bundle.Objects[1].Pickable == nil || !*bundle.Objects[1].Pickable {
		t.Fatalf("expected explicit pickable metadata, got %#v", bundle.Objects[1].Pickable)
	}
}

func TestSceneAdapterRenderBundleAppliesSceneLightingAndInvalidatesStaticPassCache(t *testing.T) {
	prog := &rootengine.Program{
		Name: "LightingCache",
		EngineNodes: []rootengine.Node{
			{
				Kind: "light",
				Props: map[string]islandprogram.ExprID{
					"kind":       0,
					"color":      1,
					"intensity":  2,
					"directionX": 3,
					"directionY": 4,
					"directionZ": 5,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Static:   true,
				Props: map[string]islandprogram.ExprID{
					"size":  6,
					"color": 7,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitString, Value: "directional", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "#fff1d6", Type: islandprogram.TypeString},
			{Op: islandprogram.OpSignalGet, Value: "$scene.sun", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.35", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "-1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "-0.4", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.9", Type: islandprogram.TypeFloat},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$scene.sun", Type: islandprogram.TypeFloat, Init: 8},
		},
	}

	rt := NewSceneAdapter(prog, `{"scene":{"environment":{"ambientColor":"#f4fbff","ambientIntensity":0.15,"skyColor":"#b9deff","skyIntensity":0.1,"groundColor":"#102030","groundIntensity":0.04,"exposure":1.1}}}`)
	intensitySig := signal.New(FloatVal(0.9))
	rt.SetSharedSignal("$scene.sun", intensitySig)
	rt.Reconcile()

	first := rt.RenderBundle(640, 360, 0)
	if len(first.Lights) != 1 {
		t.Fatalf("expected one render light, got %#v", first.Lights)
	}
	if first.Environment.AmbientIntensity != 0.15 || first.Environment.Exposure != 1.1 {
		t.Fatalf("expected environment from props, got %#v", first.Environment)
	}
	if len(first.Passes) == 0 || first.Passes[0].CacheKey == "" {
		t.Fatalf("expected static pass cache key, got %#v", first.Passes)
	}

	intensitySig.Set(FloatVal(1.8))
	second := rt.RenderBundle(640, 360, 0)
	if len(second.WorldColors) == 0 || len(first.WorldColors) != len(second.WorldColors) {
		t.Fatalf("expected comparable lit world colors, got %#v and %#v", first.WorldColors, second.WorldColors)
	}
	if second.Passes[0].CacheKey == first.Passes[0].CacheKey {
		t.Fatalf("expected static pass cache key to change with lighting, got %q", second.Passes[0].CacheKey)
	}
	if second.WorldColors[0] == first.WorldColors[0] && second.WorldColors[1] == first.WorldColors[1] && second.WorldColors[2] == first.WorldColors[2] {
		t.Fatalf("expected lighting to alter world colors, got %#v and %#v", first.WorldColors[:4], second.WorldColors[:4])
	}
}

func TestSceneAdapterRenderBundleMarksOffscreenObjectsCulled(t *testing.T) {
	prog := &rootengine.Program{
		Name: "FrustumCull",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z":   0,
					"fov": 1,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":    2,
					"size": 3,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "75", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "120", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.2", Type: islandprogram.TypeFloat},
		},
	}

	rt := NewSceneAdapter(prog, "")
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Objects) != 1 {
		t.Fatalf("expected one render object, got %#v", bundle.Objects)
	}
	if !bundle.Objects[0].ViewCulled {
		t.Fatalf("expected far offscreen object to be marked culled, got %#v", bundle.Objects[0])
	}
	if bundle.Objects[0].VertexCount != 0 {
		t.Fatalf("expected culled object to contribute no world vertices, got %#v", bundle.Objects[0])
	}
}

func TestClipWorldSegmentForCameraClipsNearPlane(t *testing.T) {
	camera := sceneCamera{Z: 6, FOV: 72, Near: 0.05, Far: 128}
	from, to, ok := clipWorldSegmentForCamera(
		point3{X: -2, Y: 0, Z: -7},
		point3{X: 2, Y: 0, Z: 1},
		camera,
		640.0/360.0,
	)
	if !ok {
		t.Fatal("expected segment crossing near plane to stay visible")
	}
	if math.Abs(from.X+1.475) > 0.001 || math.Abs(from.Y) > 0.001 || math.Abs(from.Z+5.95) > 0.001 {
		t.Fatalf("expected clipped near-plane point, got %#v", from)
	}
	if to != (point3{X: 2, Y: 0, Z: 1}) {
		t.Fatalf("expected far endpoint to stay intact, got %#v", to)
	}
}

func TestClipWorldSegmentForCameraCullsOffscreenSegment(t *testing.T) {
	camera := sceneCamera{Z: 6, FOV: 72, Near: 0.05, Far: 128}
	_, _, ok := clipWorldSegmentForCamera(
		point3{X: 100, Y: 0, Z: 1},
		point3{X: 120, Y: 0, Z: 1},
		camera,
		640.0/360.0,
	)
	if ok {
		t.Fatal("expected fully offscreen segment to be culled")
	}
}

// TestSpinQuatSingleAxisAllAxesMatchOldEulerPath extends TestSpinQuatSingleAxisMatchesOldEulerPath
// to cover X-only, Y-only, AND Z-only spin (the property holds for all single axes).
func TestSpinQuatSingleAxisAllAxesMatchOldEulerPath(t *testing.T) {
	const ts = 0.37
	const r = 0.9
	p := point3{X: 0.3, Y: -0.4, Z: 0.6} // off-axis so all axes are exercised

	cases := []struct {
		name string
		obj  sceneObject
	}{
		{
			name: "SpinX-only",
			obj: sceneObject{
				ID: "sx", Kind: "box",
				Width: 1.4, Height: 0.8, Depth: 1.1,
				X: 0.5, Y: -0.25, Z: 0.75,
				SpinX: r,
			},
		},
		{
			name: "SpinY-only",
			obj: sceneObject{
				ID: "sy", Kind: "box",
				Width: 1.4, Height: 0.8, Depth: 1.1,
				X: 0.5, Y: -0.25, Z: 0.75,
				SpinY: r,
			},
		},
		{
			name: "SpinZ-only",
			obj: sceneObject{
				ID: "sz", Kind: "box",
				Width: 1.4, Height: 0.8, Depth: 1.1,
				X: 0.5, Y: -0.25, Z: 0.75,
				SpinZ: r,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spinQ := spinQuatForObject(tc.obj, ts)
			got := translatePoint(p, tc.obj, spinQ, clipTRS{}, ts)
			oldRot := oldRotatePointEulerSpin(p, tc.obj, ts)
			want := point3{
				X: oldRot.X + tc.obj.X,
				Y: oldRot.Y + tc.obj.Y,
				Z: oldRot.Z + tc.obj.Z,
			}
			if math.Abs(got.X-want.X) > 1e-9 || math.Abs(got.Y-want.Y) > 1e-9 || math.Abs(got.Z-want.Z) > 1e-9 {
				t.Fatalf("%s: diverged from old Euler path: p=%v got=%v want=%v", tc.name, p, got, want)
			}
		})
	}
}

// TestSpinQuatNormalSingleAxisAllAxesMatchOld extends TestSpinQuatNormalSingleAxisMatchesOld
// to cover X-only, Y-only, AND Z-only spin for world normals.
func TestSpinQuatNormalSingleAxisAllAxesMatchOld(t *testing.T) {
	const ts = 0.37
	const r = 1.1
	p := point3{X: 0.7, Y: 0.1, Z: -0.2} // off-axis so a definite face normal is produced

	cases := []struct {
		name string
		obj  sceneObject
	}{
		{
			name: "SpinX-only",
			obj:  sceneObject{ID: "nx", Kind: "box", Width: 1.4, Height: 0.8, Depth: 1.1, SpinX: r},
		},
		{
			name: "SpinY-only",
			obj:  sceneObject{ID: "ny", Kind: "box", Width: 1.4, Height: 0.8, Depth: 1.1, SpinY: r},
		},
		{
			name: "SpinZ-only",
			obj:  sceneObject{ID: "nz", Kind: "box", Width: 1.4, Height: 0.8, Depth: 1.1, SpinZ: r},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spinQ := spinQuatForObject(tc.obj, ts)
			got := sceneObjectWorldNormal(tc.obj, p, spinQ, clipTRS{})
			local := sceneObjectLocalNormal(tc.obj, p)
			want := normalizePoint3(oldRotatePointEulerSpin(local, tc.obj, ts))
			if math.Abs(got.X-want.X) > 1e-9 || math.Abs(got.Y-want.Y) > 1e-9 || math.Abs(got.Z-want.Z) > 1e-9 {
				t.Fatalf("%s: world normal diverged: got=%v want=%v", tc.name, got, want)
			}
		})
	}
}

// TestSpinQuatZeroAllocForStaticObject verifies the zero-spin early-return path
// allocates nothing (no Eval, no WriteBuf push).
func TestSpinQuatZeroAllocForStaticObject(t *testing.T) {
	staticObj := sceneObject{ID: "static", Kind: "box", Width: 1, Height: 1, Depth: 1}
	allocs := testing.AllocsPerRun(100, func() {
		spinQuatForObject(staticObj, 0.5)
	})
	if allocs != 0 {
		t.Fatalf("expected 0 allocs for zero-spin object, got %v", allocs)
	}
}

// TestSpinQuatZeroAllocWithCachedScratch verifies the spinning path reuses the
// cached scratch (zero per-call alloc once the scratch is initialised).
func TestSpinQuatZeroAllocWithCachedScratch(t *testing.T) {
	sc := newSpinScratch()
	obj := sceneObject{ID: "spinner", Kind: "box", Width: 1, Height: 1, Depth: 1, SpinY: 0.9}
	// Warm up — first call may touch any one-time setup inside WriteBuf.
	spinQuatWithScratch(obj, 0.1, sc)
	allocs := testing.AllocsPerRun(100, func() {
		spinQuatWithScratch(obj, 0.5, sc)
	})
	if allocs != 0 {
		t.Fatalf("expected 0 allocs when reusing cached scratch for spinning object, got %v", allocs)
	}
}

// ---------------------------------------------------------------------------
// Clip TRS tests (P3.1b): production render bundle applies the per-object clip
// transform composed with base rotation + spin. These mirror the spin tests.
// ---------------------------------------------------------------------------

// clipTranslationAnims builds a single-clip animation set whose translation
// channel targets the object ID and moves it 0 -> [6,0,0] over t in [0,1].
func clipTranslationAnims(targetID string, duration float64) []rootengine.RenderAnimation {
	return []rootengine.RenderAnimation{{
		Name:     "clip",
		Duration: duration,
		Channels: []rootengine.RenderAnimationChannel{{
			TargetID:      targetID,
			Property:      "translation",
			Times:         []float64{0, 1},
			Values:        []float64{0, 0, 0, 6, 0, 0},
			Interpolation: "LINEAR",
		}},
	}}
}

// TestClipTranslationMovesObject: a translation clip targeting the object shifts
// its world position by the linearly-interpolated offset (~[3,0,0] at t=0.5).
func TestClipTranslationMovesObject(t *testing.T) {
	obj := sceneObject{ID: "mover", Kind: "box", Width: 1, Height: 1, Depth: 1}
	anims := clipTranslationAnims("mover", 0) // no looping; raw lerp over [0,1]
	sc := newSpinScratch()

	clip0 := objectClipTRS(obj, 0, anims, 0, sc)
	if !clip0.HasT {
		t.Fatalf("expected clip translation present at t=0, got %+v", clip0)
	}
	clipHalf := objectClipTRS(obj, 0, anims, 0.5, sc)
	if math.Abs(clipHalf.T[0]-3) > 1e-9 || math.Abs(clipHalf.T[1]) > 1e-9 || math.Abs(clipHalf.T[2]) > 1e-9 {
		t.Fatalf("expected clip T ~[3,0,0] at t=0.5, got %v", clipHalf.T)
	}

	p := point3{X: 0.2, Y: -0.1, Z: 0.3}
	spinQ := spinQuatForObject(obj, 0.5) // no spin -> identity
	at0 := translatePoint(p, obj, spinQ, clip0, 0)
	atHalf := translatePoint(p, obj, spinQ, clipHalf, 0.5)
	dx := atHalf.X - at0.X
	dy := atHalf.Y - at0.Y
	dz := atHalf.Z - at0.Z
	if math.Abs(dx-3) > 1e-9 || math.Abs(dy) > 1e-9 || math.Abs(dz) > 1e-9 {
		t.Fatalf("expected world position to shift by ~[3,0,0], got [%v,%v,%v]", dx, dy, dz)
	}
}

// TestClipTranslationMovesBoundsViaRenderBundle: end-to-end through RenderBundle
// — a translation clip authored in props shifts the object's Bounds by ~[3,0,0]
// from t=0 to t=0.5.
func TestClipTranslationMovesBoundsViaRenderBundle(t *testing.T) {
	prog := &rootengine.Program{
		Name: "ClipTranslation",
		EngineNodes: []rootengine.Node{{
			Kind:     "mesh",
			Geometry: "box",
			Material: "flat",
			Props: map[string]islandprogram.ExprID{
				"id":     0,
				"width":  1,
				"height": 2,
				"depth":  3,
				"z":      4,
			},
		}},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitString, Value: "mover", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "10", Type: islandprogram.TypeFloat},
		},
	}
	propsJSON := `{
		"animations": [
			{"name":"clip","duration":0,"channels":[
				{"targetID":"mover","property":"translation","times":[0,1],"values":[0,0,0,6,0,0],"interpolation":"LINEAR"}
			]}
		]
	}`
	rt := NewSceneAdapter(prog, propsJSON)
	start := rt.RenderBundle(640, 360, 0)
	later := rt.RenderBundle(640, 360, 0.5)
	if len(start.Objects) != 1 || len(later.Objects) != 1 {
		t.Fatalf("expected one object per bundle, got %#v and %#v", start.Objects, later.Objects)
	}
	startCX := (start.Objects[0].Bounds.MinX + start.Objects[0].Bounds.MaxX) / 2
	laterCX := (later.Objects[0].Bounds.MinX + later.Objects[0].Bounds.MaxX) / 2
	if math.Abs((laterCX-startCX)-3) > 1e-6 {
		t.Fatalf("expected bounds center X to shift by ~3, got start=%f later=%f delta=%f", startCX, laterCX, laterCX-startCX)
	}
}

// TestClipRotationRotatesVertex: a single-axis rotationY clip rotates an off-axis
// vertex by the evaluated angle; cross-checked against QuatFromEuler/RotateVec3.
func TestClipRotationRotatesVertex(t *testing.T) {
	const angle = 1.2 // radians at t=1
	obj := sceneObject{ID: "rotor", Kind: "box", Width: 1, Height: 1, Depth: 1}
	anims := []rootengine.RenderAnimation{{
		Name:     "clip",
		Duration: 0,
		Channels: []rootengine.RenderAnimationChannel{{
			TargetID:      "rotor",
			Property:      "rotationY",
			Times:         []float64{0, 1},
			Values:        []float64{0, angle},
			Interpolation: "LINEAR",
		}},
	}}
	sc := newSpinScratch()
	const ts = 0.5
	clip := objectClipTRS(obj, 0, anims, ts, sc)
	if !clip.HasR {
		t.Fatalf("expected clip rotation present, got %+v", clip)
	}

	p := point3{X: 0.7, Y: 0.0, Z: 0.0} // off the rotation axis
	spinQ := spinQuatForObject(obj, ts) // identity (no spin)
	got := translatePoint(p, obj, spinQ, clip, ts)
	// Expected: base rotation is identity here, so apply QuatFromEuler(0,angle*0.5,0).
	wantQ := motion.QuatFromEuler(0, angle*ts, 0)
	wx, wy, wz := motion.RotateVec3(wantQ, p.X, p.Y, p.Z)
	if math.Abs(got.X-wx) > 1e-9 || math.Abs(got.Y-wy) > 1e-9 || math.Abs(got.Z-wz) > 1e-9 {
		t.Fatalf("clip rotation diverged: got=%v want=(%v,%v,%v)", got, wx, wy, wz)
	}
}

// TestClipScaleScalesExtent: a scale clip scales the local vertex pre-rotation,
// growing the object's extent. Authored uniform-scale 1 -> 2 over [0,1].
func TestClipScaleScalesExtent(t *testing.T) {
	obj := sceneObject{ID: "scaler", Kind: "box", Width: 2, Height: 2, Depth: 2}
	anims := []rootengine.RenderAnimation{{
		Name:     "clip",
		Duration: 0,
		Channels: []rootengine.RenderAnimationChannel{{
			TargetID:      "scaler",
			Property:      "scale",
			Times:         []float64{0, 1},
			Values:        []float64{1, 1, 1, 2, 2, 2},
			Interpolation: "LINEAR",
		}},
	}}
	sc := newSpinScratch()
	// Sample mid-clip (t=0.5 -> scale 1.5). The build sets duration=lastTime=1, so
	// evaluating exactly at t=1 would wrap (mod 1) back to scale 1; a mid value
	// exercises the active scale without hitting the loop boundary.
	const ts = 0.5
	const wantScale = 1.5
	clip := objectClipTRS(obj, 0, anims, ts, sc)
	if !clip.HasS {
		t.Fatalf("expected clip scale present, got %+v", clip)
	}
	if math.Abs(clip.S[0]-wantScale) > 1e-9 || math.Abs(clip.S[1]-wantScale) > 1e-9 || math.Abs(clip.S[2]-wantScale) > 1e-9 {
		t.Fatalf("expected scale %vx at t=0.5, got %v", wantScale, clip.S)
	}

	p := point3{X: 0.5, Y: -0.25, Z: 0.75}
	spinQ := spinQuatForObject(obj, ts) // identity
	// No clip -> base transform; with scale -> local vertex scaled before rotate.
	noClip := translatePoint(p, obj, spinQ, clipTRS{}, ts)
	scaled := translatePoint(p, obj, spinQ, clip, ts)
	// Base rotation is identity, so scaled == wantScale*p and noClip == p.
	if math.Abs(scaled.X-wantScale*noClip.X) > 1e-9 || math.Abs(scaled.Y-wantScale*noClip.Y) > 1e-9 || math.Abs(scaled.Z-wantScale*noClip.Z) > 1e-9 {
		t.Fatalf("expected scaled vertex to be %vx base, got scaled=%v base=%v", wantScale, scaled, noClip)
	}
}

// TestClipRotationComposesWithSpin: an object with BOTH spin and a clip rotation
// composes as base -> clipR -> spin applied to a vertex.
func TestClipRotationComposesWithSpin(t *testing.T) {
	const ts = 0.4
	const spinY = 0.9
	const clipAngle = 1.1
	obj := sceneObject{ID: "both", Kind: "box", Width: 1, Height: 1, Depth: 1, SpinY: spinY, RotationX: 0.3}
	anims := []rootengine.RenderAnimation{{
		Name:     "clip",
		Duration: 0,
		Channels: []rootengine.RenderAnimationChannel{{
			TargetID:      "both",
			Property:      "rotationZ",
			Times:         []float64{0, 1},
			Values:        []float64{0, clipAngle},
			Interpolation: "LINEAR",
		}},
	}}
	sc := newSpinScratch()
	clip := objectClipTRS(obj, 0, anims, ts, sc)
	if !clip.HasR {
		t.Fatalf("expected clip rotation present, got %+v", clip)
	}
	spinQ := spinQuatForObject(obj, ts)

	p := point3{X: 0.5, Y: 0.25, Z: -0.5}
	got := translatePoint(p, obj, spinQ, clip, ts)
	// Manual composition: base rotate -> clip rotate -> spin rotate -> translate.
	base := rotatePoint(p, obj.RotationX, obj.RotationY, obj.RotationZ)
	crx, cry, crz := motion.RotateVec3(clip.R, base.X, base.Y, base.Z)
	srx, sry, srz := motion.RotateVec3(spinQ, crx, cry, crz)
	want := point3{X: srx + obj.X, Y: sry + obj.Y, Z: srz + obj.Z}
	if math.Abs(got.X-want.X) > 1e-9 || math.Abs(got.Y-want.Y) > 1e-9 || math.Abs(got.Z-want.Z) > 1e-9 {
		t.Fatalf("clip+spin composition diverged: got=%v want=%v", got, want)
	}
}

// TestClipNonBreakingForUntargetedObjects: an object with NO clip targeting it
// (whether spinning or static) is byte-identical to the pre-clip output, proving
// the zero-cost / zero-effect path. Animations exist but target a different ID.
func TestClipNonBreakingForUntargetedObjects(t *testing.T) {
	anims := clipTranslationAnims("someone-else", 0)
	sc := newSpinScratch()

	// Spinning object, not targeted by any clip channel.
	const ts = 0.37
	spinner := sceneObject{ID: "spinner", Kind: "box", Width: 1.4, Height: 0.8, Depth: 1.1, X: 0.5, Y: -0.25, Z: 0.75, SpinY: 0.9}
	spinClip := objectClipTRS(spinner, 0, anims, ts, sc)
	if spinClip != (clipTRS{}) {
		t.Fatalf("untargeted spinning object must yield zero clipTRS, got %+v", spinClip)
	}
	spinQ := spinQuatForObject(spinner, ts)
	p := point3{X: 0.3, Y: -0.2, Z: 0.5}
	withZeroClip := translatePoint(p, spinner, spinQ, spinClip, ts)
	withExplicitNoClip := translatePoint(p, spinner, spinQ, clipTRS{}, ts)
	// And the pre-clip spin oracle.
	oldRot := oldRotatePointEulerSpin(p, spinner, ts)
	want := point3{X: oldRot.X + spinner.X, Y: oldRot.Y + spinner.Y, Z: oldRot.Z + spinner.Z}
	if withZeroClip != withExplicitNoClip {
		t.Fatalf("zero clipTRS not byte-identical to explicit no-clip: %v vs %v", withZeroClip, withExplicitNoClip)
	}
	if math.Abs(withZeroClip.X-want.X) > 1e-9 || math.Abs(withZeroClip.Y-want.Y) > 1e-9 || math.Abs(withZeroClip.Z-want.Z) > 1e-9 {
		t.Fatalf("untargeted spinning object diverged from old spin path: got=%v want=%v", withZeroClip, want)
	}

	// Static object, not targeted.
	static := sceneObject{ID: "static", Kind: "box", Width: 1, Height: 1, Depth: 1, X: 1, Y: 2, Z: 3, RotationY: 0.4}
	staticClip := objectClipTRS(static, 1, anims, ts, sc)
	if staticClip != (clipTRS{}) {
		t.Fatalf("untargeted static object must yield zero clipTRS, got %+v", staticClip)
	}
	staticSpin := spinQuatForObject(static, ts)
	gotStatic := translatePoint(p, static, staticSpin, staticClip, ts)
	base := rotatePoint(p, static.RotationX, static.RotationY, static.RotationZ)
	wantStatic := point3{X: base.X + static.X, Y: base.Y + static.Y, Z: base.Z + static.Z}
	if math.Abs(gotStatic.X-wantStatic.X) > 1e-12 || math.Abs(gotStatic.Y-wantStatic.Y) > 1e-12 || math.Abs(gotStatic.Z-wantStatic.Z) > 1e-12 {
		t.Fatalf("untargeted static object diverged from base path: got=%v want=%v", gotStatic, wantStatic)
	}
}

// TestClipLoopsAtDuration: a clip with duration 1 wraps — evaluating at t=1.5
// equals t=0.5 (matches the native render/bundle looping).
func TestClipLoopsAtDuration(t *testing.T) {
	obj := sceneObject{ID: "looper", Kind: "box", Width: 1, Height: 1, Depth: 1}
	anims := clipTranslationAnims("looper", 1) // duration 1 -> loops
	sc := newSpinScratch()

	clipHalf := objectClipTRS(obj, 0, anims, 0.5, sc)
	clipWrapped := objectClipTRS(obj, 0, anims, 1.5, sc)
	if math.Abs(clipHalf.T[0]-clipWrapped.T[0]) > 1e-9 ||
		math.Abs(clipHalf.T[1]-clipWrapped.T[1]) > 1e-9 ||
		math.Abs(clipHalf.T[2]-clipWrapped.T[2]) > 1e-9 {
		t.Fatalf("clip did not loop: t=0.5 T=%v t=1.5 T=%v", clipHalf.T, clipWrapped.T)
	}
	if math.Abs(clipHalf.T[0]-3) > 1e-9 {
		t.Fatalf("expected looped translation T[0]~3 at wrapped t=0.5, got %v", clipHalf.T[0])
	}
}
