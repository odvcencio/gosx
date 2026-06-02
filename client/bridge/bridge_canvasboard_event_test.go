//go:build !gosx_tiny_islands_only

package bridge

import (
	"math"
	"testing"

	rootengine "m31labs.dev/gosx/engine"
	islandprogram "m31labs.dev/gosx/island/program"
)

// rectBoardProg builds a single-rect board program (world x∈[x,x+w], y∈[y,y+h])
// with the given node id — the fixture the interaction-event tests pick against.
func rectBoardProg(id string, x, y, w, h float64) *rootengine.Program {
	itoa := func(f float64) string {
		// whole-number fixtures only
		neg := f < 0
		n := int(math.Abs(f))
		if n == 0 {
			return "0"
		}
		var buf []byte
		for n > 0 {
			buf = append([]byte{byte('0' + n%10)}, buf...)
			n /= 10
		}
		if neg {
			buf = append([]byte{'-'}, buf...)
		}
		return string(buf)
	}
	return &rootengine.Program{
		Name: "EventBoard",
		EngineNodes: []rootengine.Node{
			{Kind: "rect", Props: map[string]islandprogram.ExprID{"x": 0, "y": 1, "width": 2, "height": 3, "id": 4}},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: itoa(x), Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: itoa(y), Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: itoa(w), Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: itoa(h), Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: id, Type: islandprogram.TypeString},
		},
	}
}

// mountTypedBoard (re)hydrates a typed program onto an existing canvas2d board
// id by round-tripping it through the wire JSON encoder — the only public mount
// path. HydrateReconciler disposes any prior adapter under the same id first.
func mountTypedBoard(t *testing.T, b *Bridge, id string, prog *rootengine.Program) {
	t.Helper()
	data, err := rootengine.EncodeProgramJSON(prog)
	if err != nil {
		t.Fatalf("encode program: %v", err)
	}
	if err := b.HydrateReconciler("canvas2d", id, "Board", `{}`, data, "json"); err != nil {
		t.Fatalf("mount typed board: %v", err)
	}
}

// TestCanvasBoardEventPanShiftsCamera verifies a "pan" event translates the
// board's camera by the screen-space delta converted to world units, and that
// the next rendered bundle reflects the new pan. Dragging the canvas right
// (dx>0) moves the content right, i.e. the camera origin moves LEFT in world.
func TestCanvasBoardEventPanShiftsCamera(t *testing.T) {
	b := New()
	if err := b.HydrateReconciler("canvas2d", "board-pan", "Board", `{"zoom": 2}`, []byte("{}"), "json"); err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	// Pan by (dx=20, dy=10) screen px at zoom=2 → world delta (10, 5).
	// panX -= dx/zoom = -10 ; panY += dy/zoom = +5.
	if err := b.CanvasBoardEvent("board-pan", CanvasBoardEventPan, []float64{20, 10, 0, 0}, ""); err != nil {
		t.Fatalf("pan event: %v", err)
	}
	bundle, err := b.RenderCanvasBoard("board-pan", 800, 600, 0)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if bundle.Camera.X != -10 {
		t.Errorf("panX = %v, want -10", bundle.Camera.X)
	}
	if bundle.Camera.Y != 5 {
		t.Errorf("panY = %v, want 5", bundle.Camera.Y)
	}
	if bundle.Camera.Z != 2 {
		t.Errorf("zoom = %v, want unchanged 2", bundle.Camera.Z)
	}
}

// TestCanvasBoardEventPanAccumulates verifies successive pan deltas accumulate
// against the live camera (not the props origin each time).
func TestCanvasBoardEventPanAccumulates(t *testing.T) {
	b := New()
	if err := b.HydrateReconciler("canvas2d", "board-acc", "Board", `{"zoom": 1}`, []byte("{}"), "json"); err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	_ = b.CanvasBoardEvent("board-acc", CanvasBoardEventPan, []float64{5, 0, 0, 0}, "")
	_ = b.CanvasBoardEvent("board-acc", CanvasBoardEventPan, []float64{5, 0, 0, 0}, "")
	bundle, _ := b.RenderCanvasBoard("board-acc", 800, 600, 0)
	if bundle.Camera.X != -10 {
		t.Errorf("accumulated panX = %v, want -10 (two -5 deltas)", bundle.Camera.X)
	}
}

// TestCanvasBoardEventZoomTowardCursor verifies a "zoom" event scales the
// camera by the factor AND keeps the world point under the cursor fixed on
// screen (the defining property of zoom-to-cursor). We zoom in by 2x with the
// cursor at the top-left corner (screen 0,0) and assert the world point that
// was under the cursor before maps back to the same screen pixel after.
func TestCanvasBoardEventZoomTowardCursor(t *testing.T) {
	b := New()
	if err := b.HydrateReconciler("canvas2d", "board-zoom", "Board", `{"zoom": 1}`, []byte("{}"), "json"); err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	const cssW, cssH = 800.0, 600.0
	const cursorX, cursorY = 0.0, 0.0 // top-left corner

	// World point under the cursor BEFORE the zoom (pan starts at 0, zoom 1).
	// worldX = (sx - cssW/2)/zoom + panX ; worldY = panY - (sy - cssH/2)/zoom.
	beforeWorldX := (cursorX-cssW/2)/1 + 0
	beforeWorldY := 0 - (cursorY-cssH/2)/1

	if err := b.CanvasBoardEvent("board-zoom", CanvasBoardEventZoom, []float64{2, cursorX, cursorY, cssW, cssH}, ""); err != nil {
		t.Fatalf("zoom event: %v", err)
	}
	bundle, _ := b.RenderCanvasBoard("board-zoom", int(cssW), int(cssH), 0)
	if bundle.Camera.Z != 2 {
		t.Fatalf("zoom = %v, want 2", bundle.Camera.Z)
	}
	// The same world point must now project back to the cursor's screen pos.
	newZoom := bundle.Camera.Z
	newPanX := bundle.Camera.X
	newPanY := bundle.Camera.Y
	gotScreenX := (beforeWorldX-newPanX)*newZoom + cssW/2
	gotScreenY := cssH/2 - (beforeWorldY-newPanY)*newZoom
	if math.Abs(gotScreenX-cursorX) > 1e-6 {
		t.Errorf("world-under-cursor screenX drifted: got %v, want %v", gotScreenX, cursorX)
	}
	if math.Abs(gotScreenY-cursorY) > 1e-6 {
		t.Errorf("world-under-cursor screenY drifted: got %v, want %v", gotScreenY, cursorY)
	}
}

// TestCanvasBoardEventZoomClamps verifies repeated zoom-in saturates at the
// adapter's max zoom rather than running away.
func TestCanvasBoardEventZoomClamps(t *testing.T) {
	b := New()
	_ = b.HydrateReconciler("canvas2d", "board-zc", "Board", `{"zoom": 1}`, []byte("{}"), "json")
	for i := 0; i < 50; i++ {
		_ = b.CanvasBoardEvent("board-zc", CanvasBoardEventZoom, []float64{2, 400, 300, 800, 600}, "")
	}
	bundle, _ := b.RenderCanvasBoard("board-zc", 800, 600, 0)
	if bundle.Camera.Z > 10.0+1e-9 {
		t.Errorf("zoom did not clamp: %v", bundle.Camera.Z)
	}
}

// TestCanvasBoardEventPickWritesSelectedID is the pick keystone for the bridge:
// a "pick" event at a screen position over a node converts to world, hit-tests,
// and writes the node's id into $surface.event.selectedID / targetID per ADR
// 0007. A pick over empty space clears selection.
func TestCanvasBoardEventPickWritesSelectedID(t *testing.T) {
	b := New()
	// Rect at world (0,0,100,100). At pan=(0,0), zoom=1, viewport 200x200, the
	// rect's world origin (0,0) sits at screen center (100,100). World (50,50)
	// — the rect center — sits at screen (150, 50): screenX=(50-100)*1+100=50?
	// Recompute carefully below; the test derives the screen point from the
	// transform rather than hand-waving.
	if err := b.HydrateReconciler("canvas2d", "board-pick", "Board", `{}`, []byte("{}"), "json"); err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	// Replace the empty program with a real rect by re-hydrating a built prog.
	// HydrateReconciler decodes wire JSON; instead mount the typed program via
	// the same path RenderCanvasBoard uses. We rebuild through hydrate of a
	// JSON-encoded program below to keep within the public API.
	prog := rectBoardProg("node-A", 0, 0, 100, 100)
	mountTypedBoard(t, b, "board-pick", prog)

	const cssW, cssH = 200.0, 200.0
	// World (50,50) → screen. screenX=(50-0)*1+100=150 ; screenY=100-(50-0)*1=50.
	screenX, screenY := 150.0, 50.0
	if err := b.CanvasBoardEvent("board-pick", CanvasBoardEventPick, []float64{screenX, screenY, cssW, cssH}, ""); err != nil {
		t.Fatalf("pick event: %v", err)
	}
	store := b.GetStore()
	got, ok := store.Get("$surface.event.selectedID")
	if !ok {
		t.Fatalf("$surface.event.selectedID not written")
	}
	if got.Str != "node-A" {
		t.Errorf("selectedID = %q, want node-A", got.Str)
	}
	// Legacy alias must forward (ADR 0007).
	if legacy, ok := store.Get("$scene.event.selectedID"); !ok || legacy.Str != "node-A" {
		t.Errorf("legacy $scene.event.selectedID = (%q,%v), want node-A", legacy.Str, ok)
	}
	target, _ := store.Get("$surface.event.targetID")
	if target.Str != "node-A" {
		t.Errorf("targetID = %q, want node-A", target.Str)
	}

	// A pick over empty space clears the selection.
	if err := b.CanvasBoardEvent("board-pick", CanvasBoardEventPick, []float64{5, 5, cssW, cssH}, ""); err != nil {
		t.Fatalf("pick miss event: %v", err)
	}
	cleared, _ := store.Get("$surface.event.selectedID")
	if cleared.Str != "" {
		t.Errorf("selectedID after miss = %q, want empty", cleared.Str)
	}
}

// TestCanvasBoardEventPickPointerSignals verifies the pointer + revision fields
// land on every pick (so computed signals watching revision re-run).
func TestCanvasBoardEventPickPointerSignals(t *testing.T) {
	b := New()
	_ = b.HydrateReconciler("canvas2d", "board-ps", "Board", `{}`, []byte("{}"), "json")
	mountTypedBoard(t, b, "board-ps", rectBoardProg("n", 0, 0, 100, 100))
	if err := b.CanvasBoardEvent("board-ps", CanvasBoardEventPick, []float64{150, 50, 200, 200}, ""); err != nil {
		t.Fatalf("pick: %v", err)
	}
	store := b.GetStore()
	px, ok := store.Get("$surface.event.pointerX")
	if !ok || px.Num != 150 {
		t.Errorf("pointerX = (%v,%v), want 150", px.Num, ok)
	}
	if _, ok := store.Get("$surface.event.revision"); !ok {
		t.Errorf("revision not written on pick")
	}
}

// TestCanvasBoardEventUnknownBoard returns an error for an unregistered id.
func TestCanvasBoardEventUnknownBoard(t *testing.T) {
	b := New()
	if err := b.CanvasBoardEvent("nope", CanvasBoardEventPan, []float64{1, 1, 0, 0}, ""); err == nil {
		t.Error("expected error for unknown board id")
	}
}
