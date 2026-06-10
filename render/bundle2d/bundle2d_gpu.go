package bundle2d

import (
	"m31labs.dev/gosx"
	rootengine "m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/boardgpu"
)

// The board→GPU-geometry attach (rect/line/sprite quad expansion + the BoardFill
// Selena material) lives in the leaf package render/boardgpu, NOT here. bundle2d
// imports client/vm and client/bridge to re-marshal the WASM bundle byte-for-byte,
// so the live WASM adapter (client/vm.CanvasBoardAdapter) cannot import bundle2d
// without a cycle — but it CAN import boardgpu (deps: engine + scene only). These
// thin wrappers keep bundle2d's existing surface (AttachBoardGPUGeometry,
// ComputeCanvasGPUBundle*, BoardFillSelenaSource) and goldens unchanged while the
// single implementation is shared with the adapter.

// BoardFillSelenaSource re-exports boardgpu.BoardFillSelenaSource so existing
// bundle2d callers/tests keep referencing it through this package.
var BoardFillSelenaSource = boardgpu.BoardFillSelenaSource

// AttachBoardGPUGeometry delegates to boardgpu.AttachBoardGPUGeometry. See that
// package for the full contract (idempotent; turns a painter bundle into a
// GPU-only bundle; not 26b1-compatible afterward).
func AttachBoardGPUGeometry(b rootengine.RenderBundle) rootengine.RenderBundle {
	return boardgpu.AttachBoardGPUGeometry(b)
}

// ComputeCanvasGPUBundle is ComputeCanvasBundle plus AttachBoardGPUGeometry: a
// board bundle the GPU scene renderer (16a / native render/bundle) can draw, used
// by the WebGPU canvas re-platform. The non-GPU 26b1 painter path keeps using
// ComputeCanvasBundle.
func ComputeCanvasGPUBundle(nodes []gosx.CanvasBoardNode, width, height int, zoom, panX, panY float64) rootengine.RenderBundle {
	return boardgpu.AttachBoardGPUGeometry(ComputeCanvasBundle(nodes, width, height, zoom, panX, panY))
}

// ComputeCanvasGPUBundleWithBackground is ComputeCanvasGPUBundle with an explicit
// board background color.
func ComputeCanvasGPUBundleWithBackground(nodes []gosx.CanvasBoardNode, background string, width, height int, zoom, panX, panY float64) rootengine.RenderBundle {
	return boardgpu.AttachBoardGPUGeometry(ComputeCanvasBundleWithBackground(nodes, background, width, height, zoom, panX, panY))
}
