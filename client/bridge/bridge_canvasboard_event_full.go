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
	// CanvasBoardEventMarquee hit-tests a screen-space rectangle (shift-drag on
	// empty canvas) and writes the comma-joined ids of every pickable node it
	// covers into $surface.event.selectedIDs, plus the first/primary into
	// selectedID (ADR 0007). A zero-area rect clears the selection (the Escape
	// gesture). Mirrors the DOM board's startMarquee → selectedNodes.
	//   floats = [x0, y0, x1, y1, cssWidth, cssHeight]
	CanvasBoardEventMarquee CanvasBoardEventKind = 4
	// CanvasBoardEventNav walks the selection to the spatially-nearest node in a
	// direction (arrow keys) via the adapter's NavFrom and writes the result into
	// $surface.event.selectedID. The current selection is read from selectedID;
	// an empty current selection lands on the topmost-leftmost node. Mirrors the
	// DOM board's navigateNodes → nearestNodeKey.
	//   floats = [dirCode]  (see the CanvasNav* direction codes)
	CanvasBoardEventNav CanvasBoardEventKind = 5
)

// CanvasNavDirection codes pin the arrow-key direction the JS bootstrap sends in
// a CanvasBoardEventNav payload (floats[0]) to the string directions NavFrom
// understands. The integers cross the JS↔WASM boundary, so the bootstrap's
// keydown handler maps ArrowUp/Down/Left/Right to these exact values.
type CanvasNavDirection int

const (
	CanvasNavUp    CanvasNavDirection = 0
	CanvasNavDown  CanvasNavDirection = 1
	CanvasNavLeft  CanvasNavDirection = 2
	CanvasNavRight CanvasNavDirection = 3
)

// canvasNavDirString maps a wire direction code to the string NavFrom expects.
// An unknown code yields "" so NavFrom returns "" (selection unchanged) rather
// than guessing a direction.
func canvasNavDirString(code CanvasNavDirection) string {
	switch code {
	case CanvasNavUp:
		return "up"
	case CanvasNavDown:
		return "down"
	case CanvasNavLeft:
		return "left"
	case CanvasNavRight:
		return "right"
	default:
		return ""
	}
}

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
	case CanvasBoardEventMarquee:
		if len(floats) < 6 {
			return fmt.Errorf("canvas marquee needs [x0, y0, x1, y1, cssW, cssH], got %d floats", len(floats))
		}
		b.canvasBoardApplyMarquee(adapter, floats[0], floats[1], floats[2], floats[3], floats[4], floats[5])
		return nil
	case CanvasBoardEventNav:
		if len(floats) < 1 {
			return fmt.Errorf("canvas nav needs [dirCode], got %d floats", len(floats))
		}
		b.canvasBoardApplyNav(adapter, CanvasNavDirection(int(floats[0])))
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

// canvasBoardApplyMarquee converts a screen-space rectangle to world (via the
// live camera, the same inverse-transform a single pick uses) and writes the
// ids of every pickable node it covers into $surface.event.selectedIDs
// (comma-joined, back-to-front) plus the first as the primary selectedID (ADR
// 0007). A zero-area rect (the Escape / clear gesture) writes empty strings so
// the muddy bridge clears the board's multi-selection. The two screen corners
// are each inverted to world; PickWorldRect re-normalizes, so corner order does
// not matter.
func (b *Bridge) canvasBoardApplyMarquee(adapter *vm.CanvasBoardAdapter, x0, y0, x1, y1, cssW, cssH float64) {
	store := b.GetStore()
	if store == nil {
		return
	}
	set := func(name string, value any) {
		store.Set(name, canvasBoardSignalValue(value))
	}

	var ids []string
	// A zero-area rect is the clear gesture — skip the hit test, write empties.
	if x0 != x1 || y0 != y1 {
		panX, panY, zoom := adapter.Camera()
		if zoom <= 0 {
			zoom = 1
		}
		wx0, wy0 := canvasBoardScreenToWorld(x0, y0, panX, panY, zoom, cssW, cssH)
		wx1, wy1 := canvasBoardScreenToWorld(x1, y1, panX, panY, zoom, cssW, cssH)
		ids = adapter.PickWorldRect(wx0, wy0, wx1, wy1)
	}

	primary := ""
	if len(ids) > 0 {
		primary = ids[0]
	}
	set("$surface.event.selectedIDs", canvasBoardJoinIDs(ids))
	set("$surface.event.selectedID", primary)
	set("$surface.event.targetID", primary)
	set("$surface.event.selected", primary != "")
	set("$surface.event.type", "marquee")
	set("$surface.event.revision", float64(nextCanvasBoardPickRevision()))
}

// canvasBoardApplyNav resolves the spatially-nearest node in the pressed
// direction from the current selectedID (read from the shared store) via the
// adapter's NavFrom and writes the result back into $surface.event.selectedID.
// NavFrom returns "" when there is no node in that direction, in which case the
// current selection is left untouched (a felt-better no-op than clearing). An
// empty current selection lands on the topmost-leftmost node.
func (b *Bridge) canvasBoardApplyNav(adapter *vm.CanvasBoardAdapter, dir CanvasNavDirection) {
	store := b.GetStore()
	if store == nil {
		return
	}
	current := ""
	if cur, ok := store.Get("$surface.event.selectedID"); ok {
		current = cur.Str
	}
	next := adapter.NavFrom(current, canvasNavDirString(dir))
	if next == "" {
		// No neighbor in that direction — keep the current selection.
		return
	}
	set := func(name string, value any) {
		store.Set(name, canvasBoardSignalValue(value))
	}
	set("$surface.event.selectedID", next)
	set("$surface.event.targetID", next)
	set("$surface.event.selected", true)
	set("$surface.event.type", "nav")
	set("$surface.event.revision", float64(nextCanvasBoardPickRevision()))
}

// canvasBoardJoinIDs comma-joins ids without pulling in strings.Join's import
// churn at this seam (the file is otherwise import-light). Empty input → "".
func canvasBoardJoinIDs(ids []string) string {
	switch len(ids) {
	case 0:
		return ""
	case 1:
		return ids[0]
	}
	out := ids[0]
	for _, id := range ids[1:] {
		out += "," + id
	}
	return out
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
