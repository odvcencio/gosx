//go:build !gosx_tiny_islands_only

package bridge

import (
	"fmt"
	"sync/atomic"

	"m31labs.dev/gosx/client/vm"
)

// CanvasBoardEventKind identifies the interaction a CanvasBoardEvent carries.
// The integer values are pinned to the JS bootstrap protocol (the canvas2d
// branch in client/js/bootstrap-src/26b-feature-engines-prefix.js dispatches
// these via __gosx_canvas_event) so the two sides stay in lockstep without a
// separate enum on the wire.
type CanvasBoardEventKind int

const (
	// CanvasBoardEventPan translates the camera by a screen-space drag delta.
	//   floats = [dxScreen, dyScreen, _, _]
	// The delta is converted to world units using the board's current zoom
	// (panX -= dx/zoom; panY += dy/zoom — Y is flipped, and dragging the
	// canvas moves the content with the pointer, so the camera origin moves
	// opposite the drag).
	CanvasBoardEventPan CanvasBoardEventKind = 1
	// CanvasBoardEventZoom scales the camera toward the cursor.
	//   floats = [factor, cursorXScreen, cursorYScreen, cssWidth, cssHeight]
	// newZoom = clamp(oldZoom * factor); pan is adjusted so the world point
	// under the cursor stays pinned to the same screen pixel.
	CanvasBoardEventZoom CanvasBoardEventKind = 2
	// CanvasBoardEventPick hit-tests a screen point and writes the result into
	// $surface.event.* (ADR 0007).
	//   floats = [screenX, screenY, cssWidth, cssHeight]
	CanvasBoardEventPick CanvasBoardEventKind = 3
)

// CanvasBoardEvent routes a single interaction event into the named board's
// adapter. Pan/zoom mutate the adapter's runtime camera (SetCamera) so the next
// __gosx_render_canvas frame paints the new view; pick hit-tests through the
// camera and writes selection/pointer signals into the shared store under
// $surface.event.* (per ADR 0007, legacy $scene.event.* consumers receive the
// same writes via the read-only alias table).
//
// floats carries the kind-specific numeric payload (see the kind constants);
// payloadStr is reserved for future string args (unused today). Returns an
// error for an unknown board id or a malformed payload.
func (b *Bridge) CanvasBoardEvent(id string, kind CanvasBoardEventKind, floats []float64, payloadStr string) error {
	_ = payloadStr // reserved
	adapter, err := b.canvasBoardAdapter(id)
	if err != nil {
		return err
	}
	switch kind {
	case CanvasBoardEventPan:
		if len(floats) < 2 {
			return fmt.Errorf("canvas pan needs [dx, dy], got %d floats", len(floats))
		}
		canvasBoardApplyPan(adapter, floats[0], floats[1])
		return nil
	case CanvasBoardEventZoom:
		if len(floats) < 5 {
			return fmt.Errorf("canvas zoom needs [factor, cursorX, cursorY, cssW, cssH], got %d floats", len(floats))
		}
		canvasBoardApplyZoom(adapter, floats[0], floats[1], floats[2], floats[3], floats[4])
		return nil
	case CanvasBoardEventPick:
		if len(floats) < 4 {
			return fmt.Errorf("canvas pick needs [screenX, screenY, cssW, cssH], got %d floats", len(floats))
		}
		b.canvasBoardApplyPick(adapter, floats[0], floats[1], floats[2], floats[3])
		return nil
	default:
		return fmt.Errorf("unknown canvas board event kind %d", int(kind))
	}
}

// canvasBoardAdapter resolves a board id to its typed adapter, mirroring the
// cast TickCanvasBoard / RenderCanvasBoard perform.
func (b *Bridge) canvasBoardAdapter(id string) (*vm.CanvasBoardAdapter, error) {
	entry, ok := b.boards[id]
	if !ok {
		return nil, fmt.Errorf("canvas board %q not found", id)
	}
	adapter, ok := entry.(*vm.CanvasBoardAdapter)
	if !ok {
		return nil, fmt.Errorf("board %q is not a canvas board adapter", id)
	}
	return adapter, nil
}

// canvasBoardApplyPan converts a screen-space drag delta into a world-space
// camera translation against the board's live camera. Dragging the canvas
// right (dx>0) scrolls the content right, so the camera origin moves left:
// panX -= dx/zoom. Screen Y grows downward while world Y grows upward, so the
// sign flips back: panY += dy/zoom.
func canvasBoardApplyPan(adapter *vm.CanvasBoardAdapter, dxScreen, dyScreen float64) {
	panX, panY, zoom := adapter.Camera()
	if zoom <= 0 {
		zoom = 1
	}
	panX -= dxScreen / zoom
	panY += dyScreen / zoom
	adapter.SetCamera(panX, panY, zoom)
}

// canvasBoardApplyZoom scales the camera toward the cursor: the world point
// under the cursor stays pinned to the same screen pixel as zoom changes. It
// reproduces the OrthoCamera2D screen↔world mapping exactly (see
// canvasBoardScreenToWorld) so the live painter and this math agree.
func canvasBoardApplyZoom(adapter *vm.CanvasBoardAdapter, factor, cursorX, cursorY, cssW, cssH float64) {
	panX, panY, oldZoom := adapter.Camera()
	if oldZoom <= 0 {
		oldZoom = 1
	}
	if factor <= 0 {
		factor = 1
	}
	// World point under the cursor before the zoom.
	worldX, worldY := canvasBoardScreenToWorld(cursorX, cursorY, panX, panY, oldZoom, cssW, cssH)
	newZoom := vm.ClampCanvasBoardZoom(oldZoom * factor)
	// Re-pin: choose pan so (worldX, worldY) maps back to (cursorX, cursorY)
	// at newZoom. Invert screenX = (worldX - panX)*zoom + cssW/2 for panX, and
	// screenY = cssH/2 - (worldY - panY)*zoom for panY.
	newPanX := worldX - (cursorX-cssW/2)/newZoom
	newPanY := worldY + (cursorY-cssH/2)/newZoom
	adapter.SetCamera(newPanX, newPanY, newZoom)
}

// canvasBoardApplyPick converts a screen point to world, hit-tests the topmost
// pickable node, and writes the outcome into the shared store under
// $surface.event.* (ADR 0007). A miss clears selectedID/targetID to "" so a
// click on empty space deselects.
func (b *Bridge) canvasBoardApplyPick(adapter *vm.CanvasBoardAdapter, screenX, screenY, cssW, cssH float64) {
	panX, panY, zoom := adapter.Camera()
	if zoom <= 0 {
		zoom = 1
	}
	worldX, worldY := canvasBoardScreenToWorld(screenX, screenY, panX, panY, zoom, cssW, cssH)
	id, _ := adapter.PickWorld(worldX, worldY)

	store := b.GetStore()
	if store == nil {
		return
	}
	set := func(name string, value any) {
		store.Set(name, canvasBoardSignalValue(value))
	}
	set("$surface.event.pointerX", screenX)
	set("$surface.event.pointerY", screenY)
	set("$surface.event.type", "click")
	set("$surface.event.worldX", worldX)
	set("$surface.event.worldY", worldY)
	set("$surface.event.worldZ", float64(0))
	set("$surface.event.targetID", id)
	set("$surface.event.selectedID", id)
	set("$surface.event.selected", id != "")
	set("$surface.event.clickCount", float64(nextCanvasBoardClickCount()))
	set("$surface.event.revision", float64(nextCanvasBoardPickRevision()))
}

// canvasBoardScreenToWorld inverts the OrthoCamera2D screen transform the JS
// painter applies (bootstrap-src/26b1-canvas2d-painter.js):
//
//	screenX = (worldX - panX) * zoom + cssW/2
//	screenY = cssH/2 - (worldY - panY) * zoom   (Y flipped)
//
// solved for (worldX, worldY). Keeping this identical to the painter's forward
// transform is what makes a click land on the rect the user actually sees.
func canvasBoardScreenToWorld(screenX, screenY, panX, panY, zoom, cssW, cssH float64) (worldX, worldY float64) {
	if zoom <= 0 {
		zoom = 1
	}
	worldX = (screenX-cssW/2)/zoom + panX
	worldY = panY - (screenY-cssH/2)/zoom
	return worldX, worldY
}

// canvasBoardSignalValue converts a Go scalar to a vm.Value for the shared
// store, mirroring the scene pick path's vmValueOf (client/wasm/render_full.go).
func canvasBoardSignalValue(v any) vm.Value {
	switch t := v.(type) {
	case bool:
		return vm.BoolVal(t)
	case int:
		return vm.IntVal(t)
	case float64:
		return vm.FloatVal(t)
	case string:
		return vm.StringVal(t)
	default:
		return vm.StringVal(fmt.Sprint(v))
	}
}

// canvasBoardClickSeq and canvasBoardPickRevSeq back the clickCount / revision
// signals so .gsx computed signals that watch them re-run on every pick — the
// same monotonic-counter trick the scene pick path uses (nextClickCount /
// nextPickRevision in client/wasm/render_full.go).
var (
	canvasBoardClickSeq   uint64
	canvasBoardPickRevSeq uint64
)

func nextCanvasBoardClickCount() uint64 {
	return atomic.AddUint64(&canvasBoardClickSeq, 1)
}

func nextCanvasBoardPickRevision() uint64 {
	return atomic.AddUint64(&canvasBoardPickRevSeq, 1)
}
