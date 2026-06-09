package bundle2d

import (
	"m31labs.dev/gosx"
	rootengine "m31labs.dev/gosx/engine"
)

// AttachBoardGPUGeometry makes a Canvas2D board bundle GPU-renderable without a
// new pipeline. The board's rects live in bundle.Objects as a 2D-painter display
// list (kind/bounds/materialIndex) consumed by 26b1-canvas2d-painter.js, but the
// GPU scene renderers (native render/bundle's drawObjectMeshes and the JS 16a
// renderer) draw RenderObjects from the bundle's World* vertex buffers via
// VertexOffset/VertexCount — which the painter bundle leaves empty. This expands
// each object's rect bounds into a z=0 quad (2 triangles) in WorldPositions, with
// matching WorldNormals/WorldUVs, and points each object at its vertices.
//
// DRY: the rect geometry has ONE source — the object's existing Bounds (already
// computed by package vm). The 26b1 2D-context painter ignores World*, so this is
// additive and safe to apply to any board bundle. Lighting/depth stay off via the
// bundle's OrthoCamera2D camera (ADR 0004); a flat/Unlit fill material (and the
// Selena fill shader on RenderMaterial.CustomFragmentWGSL) controls color.
//
// Mutates and returns b for chained use.
func AttachBoardGPUGeometry(b rootengine.RenderBundle) rootengine.RenderBundle {
	if len(b.Objects) == 0 {
		return b
	}
	// 6 vertices (2 triangles) per rect object.
	pos := make([]float64, 0, len(b.Objects)*6*3)
	nrm := make([]float64, 0, len(b.Objects)*6*3)
	uv := make([]float64, 0, len(b.Objects)*6*2)
	offset := 0
	for i := range b.Objects {
		bb := b.Objects[i].Bounds
		x0, y0, x1, y1 := bb.MinX, bb.MinY, bb.MaxX, bb.MaxY
		// Two triangles: (x0,y0)(x1,y0)(x1,y1) and (x0,y0)(x1,y1)(x0,y1), z=0.
		pos = append(pos,
			x0, y0, 0, x1, y0, 0, x1, y1, 0,
			x0, y0, 0, x1, y1, 0, x0, y1, 0,
		)
		nrm = append(nrm, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1)
		uv = append(uv, 0, 0, 1, 0, 1, 1, 0, 0, 1, 1, 0, 1)
		b.Objects[i].VertexOffset = offset
		b.Objects[i].VertexCount = 6
		offset += 6
	}
	b.WorldPositions = pos
	b.WorldNormals = nrm
	b.WorldUVs = uv
	return b
}

// ComputeCanvasGPUBundle is ComputeCanvasBundle plus AttachBoardGPUGeometry: a
// board bundle the GPU scene renderer (16a / native render/bundle) can draw, used
// by the WebGPU canvas re-platform. The non-GPU 26b1 painter path keeps using
// ComputeCanvasBundle.
func ComputeCanvasGPUBundle(nodes []gosx.CanvasBoardNode, width, height int, zoom, panX, panY float64) rootengine.RenderBundle {
	return AttachBoardGPUGeometry(ComputeCanvasBundle(nodes, width, height, zoom, panX, panY))
}

// ComputeCanvasGPUBundleWithBackground is ComputeCanvasGPUBundle with an explicit
// board background color.
func ComputeCanvasGPUBundleWithBackground(nodes []gosx.CanvasBoardNode, background string, width, height int, zoom, panX, panY float64) rootengine.RenderBundle {
	return AttachBoardGPUGeometry(ComputeCanvasBundleWithBackground(nodes, background, width, height, zoom, panX, panY))
}
