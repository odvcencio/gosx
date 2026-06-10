package vm

import (
	"encoding/json"
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

// TestCanvasBoardAdapterRenderBundleCarriesRectColorViaMaterials is the
// paint-loop prerequisite: a rect's authored fill color must reach the bundle
// so both the GPU renderer and the JS canvas2d painter can draw it. Color
// flows through bundle.Materials[obj.MaterialIndex].Color (the same contract
// render/bundle/object_mesh.go uses for 3D meshes), and identical colors are
// deduplicated onto one shared material slot.
func TestCanvasBoardAdapterRenderBundleCarriesRectColorViaMaterials(t *testing.T) {
	prog := &rootengine.Program{
		Name: "ColoredBoard",
		EngineNodes: []rootengine.Node{
			{Kind: "rect", Props: map[string]islandprogram.ExprID{"x": 0, "color": 1}},
			{Kind: "rect", Props: map[string]islandprogram.ExprID{"x": 0, "color": 2}},
			{Kind: "rect", Props: map[string]islandprogram.ExprID{"x": 0, "color": 1}}, // dup color
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#ff8866", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "#88ddff", Type: islandprogram.TypeString},
		},
	}

	rt := NewCanvasBoardAdapter(prog, `{}`)
	b := rt.RenderBundle(800, 600, 0)
	if len(b.Objects) != 3 {
		t.Fatalf("expected three rect objects, got %d", len(b.Objects))
	}
	// Two distinct colors → two materials (the duplicate reuses slot 0).
	if len(b.Materials) != 2 {
		t.Fatalf("expected two deduplicated materials, got %d (%#v)", len(b.Materials), b.Materials)
	}
	colorOf := func(obj rootengine.RenderObject) string {
		if obj.MaterialIndex < 0 || obj.MaterialIndex >= len(b.Materials) {
			t.Fatalf("MaterialIndex %d out of range", obj.MaterialIndex)
		}
		return b.Materials[obj.MaterialIndex].Color
	}
	if got := colorOf(b.Objects[0]); got != "#ff8866" {
		t.Errorf("object 0 color = %q, want #ff8866", got)
	}
	if got := colorOf(b.Objects[1]); got != "#88ddff" {
		t.Errorf("object 1 color = %q, want #88ddff", got)
	}
	if b.Objects[0].MaterialIndex != b.Objects[2].MaterialIndex {
		t.Errorf("duplicate-color rects should share a material slot: %d vs %d",
			b.Objects[0].MaterialIndex, b.Objects[2].MaterialIndex)
	}
	// 2D materials must be unlit so Configure2DBundle / the renderer never tries
	// to light a board rect.
	if !b.Materials[0].Unlit {
		t.Errorf("board material should be unlit")
	}
}

// TestCanvasBoardAdapterRenderBundleRendersStaticPropsNodes is the
// static-CanvasBoard regression: a Go-constructed gosx.CanvasBoard(props)
// carries its nodes in the runtime props JSON under props.nodes (the
// CanvasBoardNode shape) and hydrates with an EMPTY {} program (no compiled
// EngineNodes). Before this fix, RenderBundle painted only the background +
// camera because buildCanvasBoardRenderBundle was fed only the resolved
// EngineNodes snapshot (nil here) and never read props.nodes. The adapter
// must now project props.nodes through the same rect/line/label/sprite path,
// emitting Objects (with a color-carrying material), Lines, and Labels.
func TestCanvasBoardAdapterRenderBundleRendersStaticPropsNodes(t *testing.T) {
	// Exactly the wire shape gosx.CanvasBoard marshals: a "board" key with
	// pan/zoom, a top-level "nodes" array of CanvasBoardNode JSON, and a
	// background. The program is the empty {} static-board hydration path.
	propsJSON := `{
		"board": {"pan": {"x": 0, "y": 0}, "zoom": 1},
		"background": "#0f1720",
		"nodes": [
			{"id": "r1", "kind": "rect", "x": 10, "y": 20, "width": 120, "height": 80, "color": "#8de1ff"},
			{"kind": "line", "x1": 0, "y1": 0, "x2": 50, "y2": 60, "color": "#ff5599"},
			{"kind": "label", "x": 5, "y": 7, "text": "hello", "color": "#ffffff"},
			{"kind": "image", "x": 200, "y": 100, "width": 64, "height": 64, "src": "/logo.png"}
		]
	}`

	rt := NewCanvasBoardAdapter(&rootengine.Program{Name: "StaticBoard"}, propsJSON)
	b := rt.RenderBundle(800, 600, 0)

	if b.Background != "#0f1720" {
		t.Errorf("background = %q, want #0f1720", b.Background)
	}
	if len(b.Objects) != 1 {
		t.Fatalf("expected one rect object from props.nodes, got %d (%#v)", len(b.Objects), b.Objects)
	}
	obj := b.Objects[0]
	if obj.Kind != "rect" {
		t.Errorf("object kind = %q, want rect", obj.Kind)
	}
	if obj.ID != "r1" {
		t.Errorf("object ID = %q, want r1", obj.ID)
	}
	if obj.Bounds.MinX != 10 || obj.Bounds.MaxX != 130 || obj.Bounds.MinY != 20 || obj.Bounds.MaxY != 100 {
		t.Errorf("bounds = (%v,%v)-(%v,%v), want (10,20)-(130,100)",
			obj.Bounds.MinX, obj.Bounds.MinY, obj.Bounds.MaxX, obj.Bounds.MaxY)
	}
	// Color must flow through Materials[obj.MaterialIndex].Color so the painter draws it.
	if obj.MaterialIndex < 0 || obj.MaterialIndex >= len(b.Materials) {
		t.Fatalf("MaterialIndex %d out of range for %d materials", obj.MaterialIndex, len(b.Materials))
	}
	if got := b.Materials[obj.MaterialIndex].Color; got != "#8de1ff" {
		t.Errorf("rect material color = %q, want #8de1ff", got)
	}
	if !b.Materials[obj.MaterialIndex].Unlit {
		t.Errorf("board material should be unlit")
	}

	if len(b.Lines) != 1 {
		t.Fatalf("expected one line from props.nodes, got %#v", b.Lines)
	}
	ln := b.Lines[0]
	if ln.From.X != 0 || ln.From.Y != 0 || ln.To.X != 50 || ln.To.Y != 60 {
		t.Errorf("line endpoints = %v→%v, want (0,0)→(50,60)", ln.From, ln.To)
	}
	if ln.Color != "#ff5599" {
		t.Errorf("line color = %q, want #ff5599", ln.Color)
	}

	if len(b.Labels) != 1 {
		t.Fatalf("expected one label from props.nodes, got %#v", b.Labels)
	}
	if b.Labels[0].Text != "hello" {
		t.Errorf("label text = %q, want hello", b.Labels[0].Text)
	}
	if b.Labels[0].Color != "#ffffff" {
		t.Errorf("label color = %q, want #ffffff", b.Labels[0].Color)
	}

	if len(b.Sprites) != 1 {
		t.Fatalf("expected one sprite from props.nodes, got %#v", b.Sprites)
	}
	if b.Sprites[0].Src != "/logo.png" {
		t.Errorf("sprite src = %q, want /logo.png", b.Sprites[0].Src)
	}
}

// TestCanvasBoardAdapterRenderBundleIgnoresPropsNodesWhenEngineNodesPresent
// proves the compiled-.gsx path is unchanged: when prog.EngineNodes is
// populated, the adapter renders those nodes through the resolved-snapshot
// path and IGNORES props.nodes entirely — no double-counting, no precedence
// flip. The props.nodes fallback only fires when EngineNodes is empty.
func TestCanvasBoardAdapterRenderBundleIgnoresPropsNodesWhenEngineNodesPresent(t *testing.T) {
	prog := &rootengine.Program{
		Name: "CompiledWins",
		EngineNodes: []rootengine.Node{
			{Kind: "rect", Props: map[string]islandprogram.ExprID{"x": 0, "y": 1, "width": 2, "height": 3, "color": 4}},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "10", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "20", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "120", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "80", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
		},
	}
	// props.nodes carries DECOY content that must never appear in the bundle
	// while EngineNodes is present: extra rects, lines, labels.
	propsJSON := `{
		"nodes": [
			{"kind": "rect", "x": 999, "y": 999, "width": 1, "height": 1, "color": "#000000"},
			{"kind": "rect", "x": 888, "y": 888, "width": 1, "height": 1, "color": "#111111"},
			{"kind": "line", "x1": 1, "y1": 2, "x2": 3, "y2": 4, "color": "#222222"},
			{"kind": "label", "x": 0, "y": 0, "text": "decoy"}
		]
	}`

	rt := NewCanvasBoardAdapter(prog, propsJSON)
	b := rt.RenderBundle(800, 600, 0)

	// Exactly one object — the single compiled EngineNode — not 1+2 decoys.
	if len(b.Objects) != 1 {
		t.Fatalf("expected one object from EngineNodes (props.nodes must be ignored), got %d (%#v)", len(b.Objects), b.Objects)
	}
	if b.Objects[0].Bounds.MinX != 10 || b.Objects[0].Bounds.MaxX != 130 {
		t.Errorf("rendered the wrong rect: bounds X = (%v,%v), want (10,130) from EngineNodes",
			b.Objects[0].Bounds.MinX, b.Objects[0].Bounds.MaxX)
	}
	if len(b.Lines) != 0 {
		t.Errorf("props.nodes line leaked while EngineNodes present: %#v", b.Lines)
	}
	if len(b.Labels) != 0 {
		t.Errorf("props.nodes label leaked while EngineNodes present: %#v", b.Labels)
	}
	if got := b.Materials[b.Objects[0].MaterialIndex].Color; got != "#8de1ff" {
		t.Errorf("material color = %q, want #8de1ff from EngineNodes", got)
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

// -----------------------------------------------------------------------------
// M1 slice 4: render backend routing (painter default / webgpu opt-in).
//
// SetRenderBackend("webgpu") makes RenderBundle apply
// boardgpu.AttachBoardGPUGeometry so the 16a JS WebGPU renderer can draw the
// board; the default (unset) backend must leave the bundle byte-identical to
// the pre-slice-4 painter bundle. These pin both halves of that contract.
// -----------------------------------------------------------------------------

// backendFixtureProgram builds a board with a rect, a line, and a sprite so the
// GPU attach has all three primitive kinds to expand. Shared by the backend
// routing tests.
func backendFixtureProgram() *rootengine.Program {
	return &rootengine.Program{
		Name: "BackendBoard",
		EngineNodes: []rootengine.Node{
			{Kind: "rect", Props: map[string]islandprogram.ExprID{"x": 0, "y": 1, "width": 2, "height": 3, "color": 4}},
			{Kind: "line", Props: map[string]islandprogram.ExprID{"x1": 0, "y1": 1, "x2": 2, "y2": 3, "color": 5}},
			{Kind: "image", Props: map[string]islandprogram.ExprID{"x": 0, "y": 1, "width": 2, "height": 3, "src": 6}},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "40", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "30", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#3a86ff", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "#8d99ae", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "/logo.png", Type: islandprogram.TypeString},
		},
	}
}

// TestCanvasBoardAdapterDefaultBackendIsPainterBundle pins the regression
// guard: the default (unset) backend emits the painter bundle with NO GPU
// vertex buffers and NO Selena material attach — exactly the wire shape the
// 26b1 painter + the parity/golden tests expect.
func TestCanvasBoardAdapterDefaultBackendIsPainterBundle(t *testing.T) {
	rt := NewCanvasBoardAdapter(backendFixtureProgram(), `{}`)
	if rt.RenderBackend() != "" {
		t.Fatalf("fresh adapter RenderBackend = %q, want empty (painter default)", rt.RenderBackend())
	}
	b := rt.RenderBundle(640, 480, 0)
	if len(b.WorldPositions) != 0 || len(b.WorldNormals) != 0 || len(b.WorldUVs) != 0 {
		t.Errorf("painter bundle must carry no GPU vertex buffers: pos=%d nrm=%d uv=%d",
			len(b.WorldPositions), len(b.WorldNormals), len(b.WorldUVs))
	}
	// Rect lives in Objects; line/sprite stay on the wire arrays (painter draws
	// them from b.Lines/b.Sprites, not Objects).
	if len(b.Objects) != 1 || b.Objects[0].Kind != "rect" {
		t.Errorf("painter bundle Objects = %+v, want the single rect", b.Objects)
	}
	if len(b.Lines) != 1 || len(b.Sprites) != 1 {
		t.Errorf("painter bundle lines/sprites = %d/%d, want 1/1", len(b.Lines), len(b.Sprites))
	}
	for i, m := range b.Materials {
		if m.CustomVertexWGSL != "" || m.CustomFragmentWGSL != "" || m.ShaderBackend != "" {
			t.Errorf("painter material %d carries a Selena attach it should not: %+v", i, m)
		}
	}
}

// TestCanvasBoardAdapterWebGPUBackendCarriesGPUGeometry pins the routed path:
// after SetRenderBackend("webgpu"), RenderBundle expands rects/lines/sprites
// into the World* vertex streams and attaches the BoardFill Selena material to
// the flat fills — the bundle the 16a JS WebGPU renderer draws.
func TestCanvasBoardAdapterWebGPUBackendCarriesGPUGeometry(t *testing.T) {
	rt := NewCanvasBoardAdapter(backendFixtureProgram(), `{}`)
	rt.SetRenderBackend(CanvasBoardBackendWebGPU)
	if rt.RenderBackend() != CanvasBoardBackendWebGPU {
		t.Fatalf("RenderBackend = %q, want webgpu", rt.RenderBackend())
	}
	b := rt.RenderBundle(640, 480, 0)

	// rect + line + sprite → three GPU objects, each a 6-vertex quad.
	if len(b.Objects) != 3 {
		t.Fatalf("webgpu bundle Objects = %d, want 3 (rect+line+sprite)", len(b.Objects))
	}
	if got, want := len(b.WorldPositions), 3*18; got != want {
		t.Errorf("WorldPositions len = %d, want %d", got, want)
	}
	if got, want := len(b.WorldNormals), 3*18; got != want {
		t.Errorf("WorldNormals len = %d, want %d", got, want)
	}
	if got, want := len(b.WorldUVs), 3*12; got != want {
		t.Errorf("WorldUVs len = %d, want %d", got, want)
	}
	if b.ObjectCount != 3 {
		t.Errorf("ObjectCount = %d, want 3 (kept in sync)", b.ObjectCount)
	}
	// The flat rect/line fills carry the BoardFill Selena attach; the sprite
	// material stays bare (default PBR object path).
	var sawSelenaFlat, sawBareSprite bool
	for _, m := range b.Materials {
		switch m.Kind {
		case "flat":
			if m.ShaderBackend == "selena" && m.CustomFragmentWGSL != "" {
				sawSelenaFlat = true
			}
		case "sprite":
			if m.ShaderBackend == "" && m.CustomFragmentWGSL == "" {
				sawBareSprite = true
			}
		}
	}
	if !sawSelenaFlat {
		t.Errorf("webgpu bundle has no flat material with the BoardFill Selena attach: %+v", b.Materials)
	}
	if !sawBareSprite {
		t.Errorf("webgpu bundle sprite material must stay bare (no Selena): %+v", b.Materials)
	}
}

// TestCanvasBoardAdapterUnsetBackendByteIdenticalToBaseline is the parity pin:
// the marshaled JSON of an unset-backend bundle must equal the JSON of a bundle
// built straight through buildCanvasBoardRenderBundleWithCamera (the exact
// pre-slice-4 path), proving the routing flag added zero bytes on the default
// path. (bridge.MarshalEngineRenderBundle is json.Marshal of the same struct;
// the bridge↔vm import cycle keeps that helper out of reach here, so we marshal
// the engine.RenderBundle directly with the same encoder.)
func TestCanvasBoardAdapterUnsetBackendByteIdenticalToBaseline(t *testing.T) {
	prog := backendFixtureProgram()

	rt := NewCanvasBoardAdapter(prog, `{}`)
	// Default backend: no SetRenderBackend call.
	routed := rt.RenderBundle(640, 480, 0)
	routedJSON, err := json.Marshal(routed)
	if err != nil {
		t.Fatalf("marshal routed bundle: %v", err)
	}

	// Baseline: resolve the same nodes and build the bundle with the identical
	// camera, bypassing the adapter's backend branch entirely.
	baseRT := NewCanvasBoardAdapter(prog, `{}`)
	nodes := baseRT.snapshot()
	zoom, panX, panY := baseRT.cameraOrProps()
	baseline := buildCanvasBoardRenderBundleWithCamera(baseRT.props, nodes, zoom, panX, panY, 640, 480, 0)
	baselineJSON, err := json.Marshal(baseline)
	if err != nil {
		t.Fatalf("marshal baseline bundle: %v", err)
	}

	if string(routedJSON) != string(baselineJSON) {
		t.Errorf("unset-backend bundle diverged from baseline:\nrouted:   %s\nbaseline: %s", routedJSON, baselineJSON)
	}
}

// TestCanvasBoardAdapterBackendRoutesStaticPropsBoard guards the static
// gosx.CanvasBoard path (no compiled EngineNodes; content in props.nodes — the
// Muddy site-map shape): SetRenderBackend must route that board to GPU geometry
// too, since RenderBundle's props.nodes fallback feeds the same builder.
func TestCanvasBoardAdapterBackendRoutesStaticPropsBoard(t *testing.T) {
	props := `{"nodes":[{"kind":"rect","id":"card","x":0,"y":0,"width":20,"height":20,"color":"#3a86ff"}]}`
	rt := NewCanvasBoardAdapter(&rootengine.Program{}, props)
	rt.SetRenderBackend(CanvasBoardBackendWebGPU)
	b := rt.RenderBundle(640, 480, 0)
	if len(b.WorldPositions) != 18 {
		t.Fatalf("static-props webgpu board WorldPositions = %d, want 18 (one rect quad)", len(b.WorldPositions))
	}
	if len(b.Materials) != 1 || b.Materials[0].ShaderBackend != "selena" {
		t.Errorf("static-props rect material must carry the BoardFill Selena attach: %+v", b.Materials)
	}
}

// TestCanvasBoardAdapterUnknownBackendStaysPainter pins the precedence rule: any
// backend value other than "webgpu" (a typo, a future name) keeps the painter
// bundle rather than silently routing or erroring.
func TestCanvasBoardAdapterUnknownBackendStaysPainter(t *testing.T) {
	rt := NewCanvasBoardAdapter(backendFixtureProgram(), `{}`)
	rt.SetRenderBackend("vulkan-someday")
	b := rt.RenderBundle(640, 480, 0)
	if len(b.WorldPositions) != 0 {
		t.Errorf("unknown backend must not expand GPU geometry: WorldPositions=%d", len(b.WorldPositions))
	}
}
