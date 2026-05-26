//go:build !gosx_tiny_islands_only

package bridge

import (
	"encoding/json"
	"testing"

	rootengine "m31labs.dev/gosx/engine"
	islandprogram "m31labs.dev/gosx/island/program"
)

// TestHydrateReconcilerCanvas2DSucceeds is the D1.4 acceptance: the unified
// dispatcher now routes canvas2d to a real CanvasBoardAdapter instead of
// returning the Phase 1d stub error.
func TestHydrateReconcilerCanvas2DSucceeds(t *testing.T) {
	prog := &rootengine.Program{
		Name: "DispatchSmoke",
		EngineNodes: []rootengine.Node{
			{Kind: "rect", Props: map[string]islandprogram.ExprID{"x": 0}},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Type: islandprogram.TypeFloat, Value: "10"},
		},
	}
	data, err := json.Marshal(prog)
	if err != nil {
		t.Fatalf("encode prog: %v", err)
	}

	b := New()
	if err := b.HydrateReconciler(SurfaceKindCanvas2D, "board-1", "CanvasBoard", `{}`, data, "json"); err != nil {
		t.Fatalf("HydrateReconciler canvas2d failed: %v", err)
	}
	if b.CanvasBoardCount() != 1 {
		t.Errorf("CanvasBoardCount = %d, want 1", b.CanvasBoardCount())
	}
	if b.ReconcilerCount() != 1 {
		t.Errorf("ReconcilerCount = %d, want 1", b.ReconcilerCount())
	}

	commands, err := b.TickCanvasBoard("board-1")
	if err != nil {
		t.Fatalf("TickCanvasBoard: %v", err)
	}
	if len(commands) != 1 || commands[0].Kind != rootengine.CommandCreateObject {
		t.Fatalf("expected one CreateObject command, got %#v", commands)
	}

	bundle, err := b.RenderCanvasBoard("board-1", 800, 600, 0)
	if err != nil {
		t.Fatalf("RenderCanvasBoard: %v", err)
	}
	if bundle.Camera.Mode != "ortho2d" {
		t.Errorf("bundle.Camera.Mode = %q, want ortho2d", bundle.Camera.Mode)
	}

	b.DisposeCanvasBoard("board-1")
	if b.CanvasBoardCount() != 0 {
		t.Errorf("DisposeCanvasBoard left board behind: %d", b.CanvasBoardCount())
	}
}

// TestHydrateReconcilerCanvas2DRejectsMalformedProgram verifies the JSON →
// rootengine.Program → CanvasBoardAdapter pipeline that the bootstrap
// drives. A malformed JSON should fail cleanly.
func TestHydrateReconcilerCanvas2DRejectsMalformedProgram(t *testing.T) {
	b := New()
	err := b.HydrateReconciler(SurfaceKindCanvas2D, "board-bad", "CanvasBoard", `{}`, []byte(`{not json`), "json")
	if err == nil {
		t.Fatalf("expected decode error for malformed program")
	}
}
