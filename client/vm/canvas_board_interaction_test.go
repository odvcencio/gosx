package vm

import (
	"testing"

	rootengine "m31labs.dev/gosx/engine"
	islandprogram "m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/render/bundle"
)

// rectBoardProgram builds a single-rect CanvasBoard program with the given
// world-space bounds (x, y, width, height), color, and node id. It is the
// shared fixture for the interaction tests — a board the pick path can hit.
func rectBoardProgram(id string, x, y, w, h float64) *rootengine.Program {
	return &rootengine.Program{
		Name: "InteractionBoard",
		EngineNodes: []rootengine.Node{
			{
				Kind: "rect",
				Props: map[string]islandprogram.ExprID{
					"x":      0,
					"y":      1,
					"width":  2,
					"height": 3,
					"color":  4,
					"id":     5,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: ftoa(x), Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: ftoa(y), Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: ftoa(w), Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: ftoa(h), Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: id, Type: islandprogram.TypeString},
		},
	}
}

func ftoa(f float64) string {
	// Test fixtures use whole numbers; canvasBoardIntToString handles ints.
	return canvasBoardIntToString(int(f))
}

// TestCanvasBoardAdapterSetCameraOverridesProps is the keystone interaction
// test: SetCamera installs a runtime camera override that the NEXT RenderBundle
// prefers over the props-derived camera. Without it the board renders at a
// fixed camera forever (the pre-interaction state).
func TestCanvasBoardAdapterSetCameraOverridesProps(t *testing.T) {
	// Props say pan=(100,-50), zoom=2.5. The override must win.
	rt := NewCanvasBoardAdapter(&rootengine.Program{Name: "PanZoom"}, `{
		"pan": {"x": 100, "y": -50},
		"zoom": 2.5
	}`)

	// Before any SetCamera, props drive the camera.
	b := rt.RenderBundle(1280, 720, 0)
	if b.Camera.X != 100 || b.Camera.Y != -50 || b.Camera.Z != 2.5 {
		t.Fatalf("pre-override camera = (%v,%v,z=%v), want props (100,-50,2.5)", b.Camera.X, b.Camera.Y, b.Camera.Z)
	}

	rt.SetCamera(7, 9, 3)
	b = rt.RenderBundle(1280, 720, 0)
	if !bundle.IsOrthoCamera2D(b.Camera) {
		t.Fatalf("camera not ortho2d after SetCamera: %#v", b.Camera)
	}
	if b.Camera.X != 7 || b.Camera.Y != 9 {
		t.Errorf("override pan = (%v,%v), want (7,9)", b.Camera.X, b.Camera.Y)
	}
	if b.Camera.Z != 3 {
		t.Errorf("override zoom = %v, want 3", b.Camera.Z)
	}
}

// TestCanvasBoardAdapterSetCameraClampsZoom verifies zoom is clamped to the
// sane [CanvasBoardMinZoom, CanvasBoardMaxZoom] range so a runaway wheel can't
// invert the projection or blow it up.
func TestCanvasBoardAdapterSetCameraClampsZoom(t *testing.T) {
	rt := NewCanvasBoardAdapter(&rootengine.Program{Name: "Clamp"}, `{}`)

	rt.SetCamera(0, 0, 1000) // too big
	if z := rt.RenderBundle(800, 600, 0).Camera.Z; z != CanvasBoardMaxZoom {
		t.Errorf("zoom not clamped to max: got %v, want %v", z, CanvasBoardMaxZoom)
	}

	rt.SetCamera(0, 0, 0.0001) // too small
	if z := rt.RenderBundle(800, 600, 0).Camera.Z; z != CanvasBoardMinZoom {
		t.Errorf("zoom not clamped to min: got %v, want %v", z, CanvasBoardMinZoom)
	}

	rt.SetCamera(0, 0, -5) // non-positive falls back to min, never inverts
	if z := rt.RenderBundle(800, 600, 0).Camera.Z; z <= 0 {
		t.Errorf("zoom went non-positive: got %v", z)
	}
}

// TestCanvasBoardAdapterCameraReadsBack lets the bridge fetch the current
// camera (override if set, else props) so pan/zoom deltas accumulate against
// a known origin and zoom-toward-cursor math has the live values.
func TestCanvasBoardAdapterCameraReadsBack(t *testing.T) {
	rt := NewCanvasBoardAdapter(&rootengine.Program{Name: "ReadBack"}, `{
		"pan": {"x": 12, "y": 34},
		"zoom": 2
	}`)
	// With no override, the camera reads the props values (so a first pan
	// delta accumulates from where the board actually sits).
	panX, panY, zoom := rt.Camera()
	if panX != 12 || panY != 34 || zoom != 2 {
		t.Fatalf("Camera() before override = (%v,%v,%v), want props (12,34,2)", panX, panY, zoom)
	}

	rt.SetCamera(-1, -2, 4)
	panX, panY, zoom = rt.Camera()
	if panX != -1 || panY != -2 || zoom != 4 {
		t.Errorf("Camera() after override = (%v,%v,%v), want (-1,-2,4)", panX, panY, zoom)
	}
}

// TestCanvasBoardAdapterPickWorldHitsRect is the pick keystone: a world point
// inside a rect's bounds returns that rect's id; a point outside returns no hit.
func TestCanvasBoardAdapterPickWorldHitsRect(t *testing.T) {
	// Rect spans world x∈[10,130], y∈[20,100].
	rt := NewCanvasBoardAdapter(rectBoardProgram("node-A", 10, 20, 120, 80), `{}`)

	id, ok := rt.PickWorld(50, 50)
	if !ok {
		t.Fatalf("expected a hit inside the rect, got none")
	}
	if id != "node-A" {
		t.Errorf("pick id = %q, want node-A", id)
	}

	// A point clearly outside misses.
	if _, ok := rt.PickWorld(500, 500); ok {
		t.Errorf("expected miss outside the rect, got a hit")
	}
}

// TestCanvasBoardAdapterPickWorldTopmost verifies the LAST matching pickable
// object wins (painter's order — later objects paint on top, so they pick
// first), matching the JS canvas2d paint order.
func TestCanvasBoardAdapterPickWorldTopmost(t *testing.T) {
	// Two overlapping rects; the second is drawn last (on top).
	prog := &rootengine.Program{
		Name: "Overlap",
		EngineNodes: []rootengine.Node{
			{Kind: "rect", Props: map[string]islandprogram.ExprID{"x": 0, "y": 1, "width": 2, "height": 3, "id": 4}},
			{Kind: "rect", Props: map[string]islandprogram.ExprID{"x": 0, "y": 1, "width": 2, "height": 3, "id": 5}},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "100", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "100", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "under", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "over", Type: islandprogram.TypeString},
		},
	}
	rt := NewCanvasBoardAdapter(prog, `{}`)
	id, ok := rt.PickWorld(50, 50)
	if !ok || id != "over" {
		t.Errorf("topmost pick = (%q, %v), want (over, true)", id, ok)
	}
}

// TestCanvasBoardAdapterPickWorldSkipsNonPickable verifies a rect with
// pickable={false} is transparent to picking and the next-lower pickable rect
// (or a miss) is returned.
func TestCanvasBoardAdapterPickWorldSkipsNonPickable(t *testing.T) {
	prog := &rootengine.Program{
		Name: "NonPickable",
		EngineNodes: []rootengine.Node{
			{Kind: "rect", Props: map[string]islandprogram.ExprID{"x": 0, "y": 1, "width": 2, "height": 3, "id": 4, "pickable": 5}},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "100", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "100", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "ghost", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitBool, Value: "false", Type: islandprogram.TypeBool},
		},
	}
	rt := NewCanvasBoardAdapter(prog, `{}`)
	if id, ok := rt.PickWorld(50, 50); ok {
		t.Errorf("pickable=false rect should not pick, got id=%q", id)
	}
}

// TestCanvasBoardAdapterPickWorldStaticPropsNodes verifies pick works for the
// static (Go-constructed) CanvasBoard path where nodes live in props.nodes and
// the program has no EngineNodes — exactly the Muddy site-map board shape.
func TestCanvasBoardAdapterPickWorldStaticPropsNodes(t *testing.T) {
	rt := NewCanvasBoardAdapter(&rootengine.Program{Name: "StaticPick"}, `{
		"nodes": [
			{"kind": "rect", "id": "page-home", "x": 200, "y": 300, "width": 160, "height": 90, "color": "#8de1ff"}
		]
	}`)
	id, ok := rt.PickWorld(250, 320)
	if !ok || id != "page-home" {
		t.Errorf("static-props pick = (%q, %v), want (page-home, true)", id, ok)
	}
}

func TestCanvasBoardMinMaxZoomSane(t *testing.T) {
	if !(CanvasBoardMinZoom > 0 && CanvasBoardMinZoom < 1) {
		t.Errorf("CanvasBoardMinZoom = %v, want 0<min<1", CanvasBoardMinZoom)
	}
	if !(CanvasBoardMaxZoom > 1) {
		t.Errorf("CanvasBoardMaxZoom = %v, want >1", CanvasBoardMaxZoom)
	}
}
