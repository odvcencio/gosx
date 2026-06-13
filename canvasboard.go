package gosx

import (
	"encoding/json"
	"strconv"
)

// CanvasBoardSurfaceKind is the wire-format surface kind string the WASM
// dispatcher (Phase 1d's __gosx_hydrate) keys on to construct a
// CanvasBoardAdapter. Keep stable — it crosses the Go/JS boundary.
const CanvasBoardSurfaceKind = "canvas2d"

// CanvasBoardBackend selects an optional CanvasBoard render backend marker.
// Empty (the default) keeps the existing painter path. Unknown values are
// intentionally ignored by the DOM emitter.
type CanvasBoardBackend string

// CanvasBoardBackendWebGPU opts a CanvasBoard into the M1 WebGPU renderer
// path by emitting data-gosx-canvas-backend="webgpu" on the canvas.
const CanvasBoardBackendWebGPU CanvasBoardBackend = "webgpu"

// CanvasBoardProps configures a Miro/Figma-style 2D editing board. Author a
// board from a .gsx page with:
//
//	gosx.CanvasBoard(gosx.CanvasBoardProps{
//	    ID:        "board",
//	    Width:     1280,
//	    Height:    720,
//	    Pan:       gosx.CanvasBoardPan{X: 0, Y: 0},
//	    Zoom:      1.0,
//	    Nodes:     boardNodes,
//	    OnPick:    "handlePick",
//	})
//
// The component lowers to a <canvas> placeholder that the WASM runtime
// hydrates into a CanvasBoardAdapter (per Phase 2 plan Section B). Pan,
// zoom, and node positions all flow through reactive signals — the .gsx
// author writes declarative state, and the adapter handles the rest.
type CanvasBoardProps struct {
	// ID is the DOM id attribute. Defaults to "gosx-canvasboard" if blank.
	ID string

	// Width/Height in CSS pixels. Defaults to 1280x720 when both are zero.
	Width  int
	Height int

	// Background is the canvas CSS background color (e.g. "#0f1720").
	Background string

	// Backend optionally marks the canvas for a specific render backend. Empty
	// keeps the default painter path.
	Backend CanvasBoardBackend

	// Pan is the initial board pan offset in world units. May be wired to a
	// shared signal at the .gsx authoring layer; this struct just captures
	// the literal initial value.
	Pan CanvasBoardPan

	// Zoom is the initial zoom factor (1.0 = 1 world unit per pixel).
	// A zero value defaults to 1.0.
	Zoom float64

	// Nodes is the initial board content — rects, lines, labels, images.
	// Author-driven updates flow through signals and the CanvasBoardAdapter's
	// dirty tracker.
	Nodes []CanvasBoardNode

	// OnPick is the name of a Go handler invoked when a board node is
	// clicked. The handler receives the pick payload via $surface.event.*
	// signals (per ADR 0007). Empty string disables pick routing.
	OnPick string

	// ClassName forwards to the canvas's CSS class attribute.
	ClassName string

	// TabIndex makes the canvas focusable when set to a non-default value.
	// Use -1 to mark it programmatically focusable but skip from tab order.
	TabIndex *int
}

// CanvasBoardPan is the board's translation offset in world coordinates.
// (0, 0) means the board's origin maps to the viewport center.
type CanvasBoardPan struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// CanvasBoardNode is a single board element. Kind selects the shape:
// "rect", "line", "label", "image", or "html". Fields not used by the chosen
// kind are ignored.
type CanvasBoardNode struct {
	ID            string  `json:"id,omitempty"`
	Kind          string  `json:"kind"`
	X             float64 `json:"x,omitempty"`
	Y             float64 `json:"y,omitempty"`
	Width         float64 `json:"width,omitempty"`
	Height        float64 `json:"height,omitempty"`
	X1            float64 `json:"x1,omitempty"`
	Y1            float64 `json:"y1,omitempty"`
	X2            float64 `json:"x2,omitempty"`
	Y2            float64 `json:"y2,omitempty"`
	Color         string  `json:"color,omitempty"`
	Text          string  `json:"text,omitempty"`
	Src           string  `json:"src,omitempty"`
	Markup        string  `json:"markup,omitempty"`
	PointerEvents string  `json:"pointerEvents,omitempty"`
}

// CanvasBoard returns a gosx.Node that renders to a <canvas> placeholder the
// WASM runtime hydrates into a CanvasBoardAdapter. Mirrors the
// engine/surface.Renderer.Mount shape so .gsx authors and direct Go callers
// see the same DOM.
//
// The emitted DOM looks like:
//
//	<canvas
//	  id="board"
//	  width="1280" height="720"
//	  data-gosx-surface-kind="canvas2d"
//	  data-gosx-engine-component="CanvasBoard"
//	  data-gosx-engine-props="<base64 json>"
//	  data-gosx-engine-caps="canvas,webgpu"
//	  data-gosx-canvas2d="1"
//	  data-gosx-onpick="handlePick" ...>
//	</canvas>
//
// The WASM bootstrap (engine/surface/runtime/bootstrap.js) sees the
// data-gosx-surface-kind attribute and dispatches to __gosx_hydrate with
// surfaceKind="canvas2d" — exactly the 6-arg form Phase 1d added.
func CanvasBoard(props CanvasBoardProps) Node {
	id := props.ID
	if id == "" {
		id = "gosx-canvasboard"
	}
	width := props.Width
	height := props.Height
	if width == 0 && height == 0 {
		width = 1280
		height = 720
	}
	zoom := props.Zoom
	if zoom <= 0 {
		zoom = 1
	}

	payload := map[string]any{
		"board": map[string]any{
			"pan":  props.Pan,
			"zoom": zoom,
		},
		"nodes": props.Nodes,
	}
	if props.Background != "" {
		payload["background"] = props.Background
	}
	if props.OnPick != "" {
		payload["onPick"] = props.OnPick
	}
	propsJSON, _ := json.Marshal(payload)

	attrs := []any{
		Attr("id", id),
		Attr("data-gosx-surface-kind", CanvasBoardSurfaceKind),
		Attr("data-gosx-engine-component", "CanvasBoard"),
		Attr("data-gosx-engine-props", string(propsJSON)),
		Attr("data-gosx-engine-caps", "canvas,webgpu"),
		Attr("data-gosx-canvas2d", "1"),
	}
	if width > 0 {
		attrs = append(attrs, Attr("width", strconv.Itoa(width)))
	}
	if height > 0 {
		attrs = append(attrs, Attr("height", strconv.Itoa(height)))
	}
	if props.ClassName != "" {
		attrs = append(attrs, Attr("class", props.ClassName))
	}
	if props.TabIndex != nil {
		attrs = append(attrs, Attr("tabindex", strconv.Itoa(*props.TabIndex)))
	}
	if props.Backend == CanvasBoardBackendWebGPU {
		attrs = append(attrs, Attr("data-gosx-canvas-backend", string(CanvasBoardBackendWebGPU)))
	}
	if props.OnPick != "" {
		attrs = append(attrs, Attr("data-gosx-onpick", props.OnPick))
	}

	return El("canvas", Attrs(attrs...))
}
