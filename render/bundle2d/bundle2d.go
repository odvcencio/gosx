// Package bundle2d computes the Canvas2D RenderBundle for a static
// gosx.CanvasBoard entirely in plain server Go — no syscall/js, builds under
// normal GOOS=linux.
//
// It is the WASM-free half of the server-driven Canvas2D board: where the
// browser path constructs a vm.CanvasBoardAdapter and calls RenderBundle, a
// server (or any non-wasm Go) calls ComputeCanvasBundle to produce the IDENTICAL
// bundle, then MarshalCanvasBundle to emit the exact JSON the JS canvas2d
// painter (client/js .../26b1-canvas2d-painter.js) consumes. Emitting that JSON
// inline on the canvas placeholder lets a client paint the board with no WASM.
//
// Byte-identity with the WASM path is the contract (see the parity test): both
// paths funnel the same []gosx.CanvasBoardNode through the same rect/line/label/
// sprite/material helpers in package vm, and both serialize with the same
// json.Marshal via bridge.MarshalEngineRenderBundle.
package bundle2d

import (
	"encoding/json"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/client/bridge"
	"m31labs.dev/gosx/client/vm"
	rootengine "m31labs.dev/gosx/engine"
)

// ComputeCanvasBundle builds the Canvas2D RenderBundle for a slice of board
// nodes at the given framebuffer size and camera, server-side and WASM-free.
//
// The output is byte-identical (after MarshalCanvasBundle / json.Marshal) to
// what a vm.CanvasBoardAdapter built from the equivalent gosx.CanvasBoard(props)
// produces via RenderBundle(width, height, _) for the same nodes and camera —
// it reuses the very same vm helpers. nodes are projected: "rect" → an unlit
// instanced quad whose fill color lives in Materials, "line" → Lines, "label"
// → Labels, "image"/"sprite" → Sprites; unknown kinds are dropped. Lighting and
// post-FX are stripped (ADR 0004's 2D-mode gate).
//
//   - width,height : framebuffer size in CSS pixels (≤0 falls back to 1280x720,
//     matching the WASM path).
//   - zoom         : world→screen scale (1 = 1 world unit per pixel; ≤0 → 1).
//   - panX,panY    : board pan in world units (the point mapped to the
//     viewport center).
func ComputeCanvasBundle(nodes []gosx.CanvasBoardNode, width, height int, zoom, panX, panY float64) rootengine.RenderBundle {
	return vm.ComputeCanvasBoardBundleFromProps(canvasBoardProps(nodes, ""), width, height, zoom, panX, panY)
}

// ComputeCanvasBundleWithBackground is ComputeCanvasBundle plus an explicit
// canvas background color (the CSS color the painter clears with). A blank
// background leaves RenderBundle.Background empty, exactly as the WASM path does
// when gosx.CanvasBoardProps.Background is unset. Provided for callers (e.g.
// gosx-studio's site-map surface) that set a board background; the camera and
// node projection are otherwise identical to ComputeCanvasBundle.
func ComputeCanvasBundleWithBackground(nodes []gosx.CanvasBoardNode, background string, width, height int, zoom, panX, panY float64) rootengine.RenderBundle {
	return vm.ComputeCanvasBoardBundleFromProps(canvasBoardProps(nodes, background), width, height, zoom, panX, panY)
}

// MarshalCanvasBundle serializes a RenderBundle to the JSON shape the JS
// canvas2d painter consumes ({background, camera, materials, objects, lines,
// labels, sprites}). It delegates to bridge.MarshalEngineRenderBundle so the
// server emission and the WASM emission share one marshaler — the JSON cannot
// drift between the two paths.
func MarshalCanvasBundle(bundle rootengine.RenderBundle) (string, error) {
	return bridge.MarshalEngineRenderBundle(bundle)
}

// canvasBoardProps builds the runtime-props map a static gosx.CanvasBoard emits
// from a node slice, then JSON round-trips it so its shape (float64 numerics,
// present keys) matches the WASM path's parseRawProps output byte-for-byte. The
// "nodes" array is the []gosx.CanvasBoardNode shape vm.canvasBoardNodesFromProps
// decodes; "background", when non-empty, is read by the bundle builder's
// canvasBoardBackground. Camera is supplied separately to ComputeCanvasBundle,
// so no "board"/"zoom"/"pan" keys are needed here.
func canvasBoardProps(nodes []gosx.CanvasBoardNode, background string) map[string]any {
	payload := map[string]any{"nodes": nodes}
	if background != "" {
		payload["background"] = background
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return map[string]any{}
	}
	var props map[string]any
	if err := json.Unmarshal(encoded, &props); err != nil {
		return map[string]any{}
	}
	return props
}
