package vm

import (
	rootengine "m31labs.dev/gosx/engine"
)

// ComputeCanvasBoardBundleFromProps builds the Canvas2D RenderBundle for a
// static board from its runtime-props map — the SAME computation the WASM
// CanvasBoardAdapter.RenderBundle performs for a board that carries no compiled
// EngineNodes. It exists so plain server Go (no syscall/js, normal GOOS=linux)
// can precompute the exact bundle the JS canvas2d painter consumes, without
// constructing an adapter or touching the VM signal machinery.
//
// props is the decoded data-gosx-engine-props object a gosx.CanvasBoard emits:
// a map[string]any with a "nodes" array (the []gosx.CanvasBoardNode shape, JSON
// round-tripped so numerics are float64), an optional "board"/"pan"/"zoom" set,
// and an optional "background". Pass props through json.Unmarshal first so its
// shape matches the WASM path's parseRawProps output byte-for-byte.
//
// zoom/panX/panY are the camera. Callers that want the props-derived camera
// (the WASM static-board default) should read it via CanvasBoardCameraFromProps
// and pass the result, so server and WASM produce an identical OrthoCamera2D.
//
// The returned bundle is byte-identical (after json.Marshal) to
// (*CanvasBoardAdapter).RenderBundle for the same props + camera: it reuses the
// exact rect/line/label/sprite/material helpers (canvasBoardNodesFromProps +
// buildCanvasBoardRenderBundleWithCamera) the adapter's static-board fallback
// uses. timeSeconds is fixed at 0 — the static server bundle is a single frame.
func ComputeCanvasBoardBundleFromProps(props map[string]any, width, height int, zoom, panX, panY float64) rootengine.RenderBundle {
	if props == nil {
		props = map[string]any{}
	}
	nodes := canvasBoardNodesFromProps(props)
	return buildCanvasBoardRenderBundleWithCamera(props, nodes, zoom, panX, panY, width, height, 0)
}

// CanvasBoardCameraFromProps exposes the props→camera derivation the WASM
// static-board path applies (canvasBoardCameraFromProps): it reads zoom/pan
// from the "board" nested object first, then top-level "zoom"/"pan" overrides,
// defaulting to zoom=1, pan=(0,0). Server callers use it to feed
// ComputeCanvasBoardBundleFromProps the SAME camera a CanvasBoardAdapter would
// derive for the identical props, keeping the two bundles byte-identical.
func CanvasBoardCameraFromProps(props map[string]any) (zoom, panX, panY float64) {
	if props == nil {
		return 1, 0, 0
	}
	return canvasBoardCameraFromProps(props)
}
