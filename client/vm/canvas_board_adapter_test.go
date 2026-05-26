package vm

import (
	"strings"
	"testing"

	rootengine "m31labs.dev/gosx/engine"
	islandprogram "m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/render/bundle"
	"m31labs.dev/gosx/signal"
)

// TestNewCanvasBoardAdapterReturnsReconciler is the B1.1 acceptance: the
// constructor returns a Reconciler-conforming adapter that reports the
// SurfaceCanvas2D surface kind. This is the keystone test — without it the
// bridge can't dispatch correctly.
func TestNewCanvasBoardAdapterReturnsReconciler(t *testing.T) {
	prog := &rootengine.Program{
		Name: "BoardSmoke",
	}
	rt := NewCanvasBoardAdapter(prog, `{}`)
	if rt == nil {
		t.Fatal("NewCanvasBoardAdapter returned nil")
	}
	var _ Reconciler = rt // compile-time check (also enforced by B1.6 var _ assertion)
	if rt.Surface() != islandprogram.SurfaceCanvas2D {
		t.Errorf("Surface = %v, want SurfaceCanvas2D", rt.Surface())
	}
}

// TestCanvasBoardAdapterInitialReconcileCreatesObjects exercises B1.3 — the
// adapter resolves prog.EngineNodes through the shared VM and emits
// CommandCreateObject commands for each board node.
func TestCanvasBoardAdapterInitialReconcileCreatesObjects(t *testing.T) {
	prog := &rootengine.Program{
		Name: "BoardCreate",
		EngineNodes: []rootengine.Node{
			{
				Kind: "rect",
				Props: map[string]islandprogram.ExprID{
					"x":     0,
					"y":     1,
					"width": 2,
					"color": 3,
				},
			},
			{
				Kind: "rect",
				Props: map[string]islandprogram.ExprID{
					"x":      4,
					"y":      5,
					"height": 6,
					"color":  3,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "10", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "20", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "60", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#ff5599", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "100", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "120", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "40", Type: islandprogram.TypeFloat},
		},
	}

	rt := NewCanvasBoardAdapter(prog, `{}`)
	commands := rt.Reconcile()
	if len(commands) != 2 {
		t.Fatalf("expected 2 create commands, got %d", len(commands))
	}
	if commands[0].Kind != rootengine.CommandCreateObject || commands[1].Kind != rootengine.CommandCreateObject {
		t.Fatalf("expected create commands, got %#v", commands)
	}
}

// TestCanvasBoardAdapterReconcileEmitsIncrementalTransformOnDirtySignal proves
// the dirty-tracking pipeline carries through to incremental commands — the
// adapter does not re-emit a full create command on a single prop change.
func TestCanvasBoardAdapterReconcileEmitsIncrementalTransformOnDirtySignal(t *testing.T) {
	prog := &rootengine.Program{
		Name: "BoardDirty",
		EngineNodes: []rootengine.Node{
			{
				Kind: "rect",
				Props: map[string]islandprogram.ExprID{
					"x":     0,
					"color": 1,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpSignalGet, Value: "$board.x", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#ff5599", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$board.x", Type: islandprogram.TypeFloat, Init: 2},
		},
	}

	rt := NewCanvasBoardAdapter(prog, `{}`)
	xSig := signal.New(FloatVal(0))
	rt.SetSharedSignal("$board.x", xSig)

	if commands := rt.Reconcile(); len(commands) != 1 || commands[0].Kind != rootengine.CommandCreateObject {
		t.Fatalf("expected initial create command, got %#v", commands)
	}

	xSig.Set(FloatVal(42))
	commands := rt.Reconcile()
	if len(commands) != 1 {
		t.Fatalf("expected one transform command after signal change, got %#v", commands)
	}
	if commands[0].Kind != rootengine.CommandSetTransform {
		t.Fatalf("expected SetTransform command, got %v", commands[0].Kind)
	}
}

// TestCanvasBoardAdapterRenderBundleUsesOrthoCamera2D is the B1.4 acceptance:
// the bundle returned by RenderBundle carries an OrthoCamera2D-tagged camera
// so the renderer dispatches into the 2D pipeline.
func TestCanvasBoardAdapterRenderBundleUsesOrthoCamera2D(t *testing.T) {
	rt := NewCanvasBoardAdapter(&rootengine.Program{Name: "EmptyBoard"}, `{}`)
	b := rt.RenderBundle(1280, 720, 0)
	if !bundle.IsOrthoCamera2D(b.Camera) {
		t.Fatalf("RenderBundle.Camera not in OrthoCamera2D mode: %#v", b.Camera)
	}
}

// TestCanvasBoardAdapterRenderBundleReadsPanZoomFromProps verifies the
// signal-driven pan/zoom prop wiring. The .gsx author writes
// <CanvasBoard pan={panSig} zoom={zoomSig}>; the SSR pipeline passes these
// as JSON props that the adapter reads here.
func TestCanvasBoardAdapterRenderBundleReadsPanZoomFromProps(t *testing.T) {
	rt := NewCanvasBoardAdapter(&rootengine.Program{Name: "PanZoom"}, `{
		"pan": {"x": 100, "y": -50},
		"zoom": 2.5
	}`)
	b := rt.RenderBundle(1280, 720, 0)
	if !bundle.IsOrthoCamera2D(b.Camera) {
		t.Fatalf("expected ortho camera, got %#v", b.Camera)
	}
	if b.Camera.X != 100 {
		t.Errorf("panX = %v, want 100", b.Camera.X)
	}
	if b.Camera.Y != -50 {
		t.Errorf("panY = %v, want -50", b.Camera.Y)
	}
	if b.Camera.Z != 2.5 {
		t.Errorf("zoom = %v, want 2.5", b.Camera.Z)
	}
}

// TestCanvasBoardAdapterRenderBundleEmitsRectObjects ensures rect nodes
// flow into bundle.Objects with sensible bounds metadata that the renderer
// can use for hit-testing and view culling.
func TestCanvasBoardAdapterRenderBundleEmitsRectObjects(t *testing.T) {
	prog := &rootengine.Program{
		Name: "RectBoard",
		EngineNodes: []rootengine.Node{
			{
				Kind: "rect",
				Props: map[string]islandprogram.ExprID{
					"x":      0,
					"y":      1,
					"width":  2,
					"height": 3,
					"color":  4,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "10", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "20", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "120", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "80", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
		},
	}

	rt := NewCanvasBoardAdapter(prog, `{}`)
	b := rt.RenderBundle(800, 600, 0)
	if len(b.Objects) != 1 {
		t.Fatalf("expected one rect object, got %#v", b.Objects)
	}
	obj := b.Objects[0]
	if obj.Kind != "rect" {
		t.Errorf("kind = %q, want rect", obj.Kind)
	}
	if obj.Bounds.MinX != 10 || obj.Bounds.MaxX != 130 {
		t.Errorf("bounds X = (%v, %v), want (10, 130)", obj.Bounds.MinX, obj.Bounds.MaxX)
	}
	if obj.Bounds.MinY != 20 || obj.Bounds.MaxY != 100 {
		t.Errorf("bounds Y = (%v, %v), want (20, 100)", obj.Bounds.MinY, obj.Bounds.MaxY)
	}
}

// TestCanvasBoardAdapterRenderBundleStripsLightingAndPostFX is the
// integration check that Configure2DBundle runs on the adapter output. A 2D
// bundle should NEVER carry lights, post-FX, or environment data — even if
// a confused caller wired props that would normally trigger those paths.
func TestCanvasBoardAdapterRenderBundleStripsLightingAndPostFX(t *testing.T) {
	rt := NewCanvasBoardAdapter(&rootengine.Program{Name: "EmptyBoard"}, `{}`)
	b := rt.RenderBundle(800, 600, 0)
	if len(b.Lights) != 0 {
		t.Errorf("lights leaked through: %#v", b.Lights)
	}
	if len(b.PostEffects) != 0 {
		t.Errorf("post-FX leaked through: %#v", b.PostEffects)
	}
	if b.Environment != (rootengine.RenderEnvironment{}) {
		t.Errorf("environment leaked through: %#v", b.Environment)
	}
}

// TestCanvasBoardAdapterRectObjectsArePickableByDefault is the B1.5
// adapter-side acceptance: board rects flow into the renderer with
// Pickable=true so the WASM pick path (which writes $surface.event.*) can
// route hover/click events at them. The actual signal write lives in the
// renderer + Section C alias plumbing — this test guards the metadata that
// makes the route possible.
func TestCanvasBoardAdapterRectObjectsArePickableByDefault(t *testing.T) {
	prog := &rootengine.Program{
		Name: "Pickable",
		EngineNodes: []rootengine.Node{
			{Kind: "rect", Props: map[string]islandprogram.ExprID{"x": 0}},
			{Kind: "rect", Props: map[string]islandprogram.ExprID{"x": 0, "pickable": 1}},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitBool, Value: "false", Type: islandprogram.TypeBool},
		},
	}
	rt := NewCanvasBoardAdapter(prog, `{}`)
	b := rt.RenderBundle(800, 600, 0)
	if len(b.Objects) != 2 {
		t.Fatalf("expected two rect objects, got %#v", b.Objects)
	}
	if b.Objects[0].Pickable == nil || !*b.Objects[0].Pickable {
		t.Errorf("default rect not pickable: %#v", b.Objects[0].Pickable)
	}
	if b.Objects[1].Pickable == nil || *b.Objects[1].Pickable {
		t.Errorf("explicit pickable={false} not honored: %#v", b.Objects[1].Pickable)
	}
}

// TestCanvasBoardAdapterDisposeClearsState satisfies the lifecycle contract
// (B1.7 cross-test) — after Dispose, the adapter holds no signal subs and
// Reconcile returns nil.
func TestCanvasBoardAdapterDisposeClearsState(t *testing.T) {
	prog := &rootengine.Program{
		Name: "BoardDispose",
		EngineNodes: []rootengine.Node{
			{
				Kind: "rect",
				Props: map[string]islandprogram.ExprID{
					"x": 0,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpSignalGet, Value: "$board.x", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$board.x", Type: islandprogram.TypeFloat, Init: 1},
		},
	}

	rt := NewCanvasBoardAdapter(prog, `{}`)
	xSig := signal.New(FloatVal(0))
	rt.SetSharedSignal("$board.x", xSig)
	rt.Reconcile()

	rt.Dispose()
	if len(rt.unsubs) != 0 {
		t.Errorf("unsubs not cleared: %#v", rt.unsubs)
	}
	// After dispose, Reconcile returns nil (no retained snapshot).
	if rt.prev != nil {
		t.Errorf("prev snapshot not cleared: %#v", rt.prev)
	}
}

// TestCanvasBoardAdapterLifecycleMirrorsSceneAdapter is the B1.7 cross-test:
// the same lifecycle calls (NewX, EvalExpr, SetSharedSignal, Dispose) on
// both adapters produce equivalent observable behavior. Subtle drift here
// would suggest a future refactor candidate (extract a shared base).
func TestCanvasBoardAdapterLifecycleMirrorsSceneAdapter(t *testing.T) {
	makeProg := func() *rootengine.Program {
		return &rootengine.Program{
			Name: "Mirror",
			Exprs: []islandprogram.Expr{
				{Op: islandprogram.OpLitFloat, Value: "3.14", Type: islandprogram.TypeFloat},
			},
		}
	}

	scene := NewSceneAdapter(makeProg(), `{}`)
	board := NewCanvasBoardAdapter(makeProg(), `{}`)

	sceneVal := scene.EvalExpr(islandprogram.ExprID(0))
	boardVal := board.EvalExpr(islandprogram.ExprID(0))
	if sceneVal.Num != boardVal.Num {
		t.Errorf("EvalExpr divergence: scene=%v board=%v", sceneVal, boardVal)
	}

	xSig := signal.New(FloatVal(1.5))
	scene.SetSharedSignal("$shared.test", xSig)
	board.SetSharedSignal("$shared.test", xSig)

	scene.Dispose()
	board.Dispose()
	// Both should be inert; calling Dispose again must not panic.
	scene.Dispose()
	board.Dispose()
}

// TestCanvasBoardAdapterCommandsAreJSONSerializable is a smoke check that the
// rootengine.Command payload survives JSON marshaling — the WASM bridge
// serializes commands across the syscall/js boundary, so a non-JSON-friendly
// value here would silently fail at the renderer.
func TestCanvasBoardAdapterCommandsAreJSONSerializable(t *testing.T) {
	prog := &rootengine.Program{
		Name: "BoardJSON",
		EngineNodes: []rootengine.Node{
			{
				Kind: "rect",
				Props: map[string]islandprogram.ExprID{
					"x":     0,
					"color": 1,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#ffffff", Type: islandprogram.TypeString},
		},
	}

	rt := NewCanvasBoardAdapter(prog, `{}`)
	commands := rt.Reconcile()
	if len(commands) != 1 {
		t.Fatalf("expected one command, got %d", len(commands))
	}
	if commands[0].Data == nil || !strings.Contains(string(commands[0].Data), `"color"`) {
		t.Fatalf("expected color in serialized command data, got %s", commands[0].Data)
	}
}
