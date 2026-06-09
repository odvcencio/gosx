package bundle2d

import (
	"fmt"
	"sync"

	"m31labs.dev/gosx"
	rootengine "m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene"
)

// boardFill* memoize the one-time compile of BoardFillSelenaSource. The source
// is embedded and immutable, so the WGSL is identical for every bundle; a
// compile failure must never panic a render path — boardFillCompiled returns
// the error and attachBoardFillMaterials degrades to no-attach, surfacing the
// failure on the bundle's Diagnostics (the renderer then falls back to its
// default material path, i.e. the pre-Selena dim-lit fill).
var (
	boardFillOnce sync.Once
	boardFillMat  scene.CustomMaterial
	boardFillErr  error
)

func boardFillCompiled() (scene.CustomMaterial, error) {
	boardFillOnce.Do(func() {
		boardFillMat, _, boardFillErr = scene.CompileSelenaMaterial(
			[]byte(BoardFillSelenaSource),
			scene.SelenaMaterialOptions{Material: "BoardFill"},
		)
	})
	return boardFillMat, boardFillErr
}

// boardFillBaseColor parses a #rrggbb fill color into the normalized RGB
// triplet the BoardFill baseColor uniform consumes (float32 components,
// matching the Selena layout's own defaults). Empty or non-#rrggbb colors
// return ok=false: the material then carries no customUniforms override and
// the renderer falls back to the layout default — the authored themed fill
// rgb(0.13, 0.14, 0.18) in board_fill.sel. Mirrors render/bundle's
// tryParseCSSColor; kept local so this package does not grow a native-renderer
// dependency for an 8-line parse.
func boardFillBaseColor(color string) ([]float32, bool) {
	if len(color) != 7 || color[0] != '#' {
		return nil, false
	}
	var r, g, b byte
	if _, err := fmt.Sscanf(color, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return nil, false
	}
	return []float32{float32(r) / 255, float32(g) / 255, float32(b) / 255}, true
}

// attachBoardFillMaterials attaches the compiled BoardFill Selena material to
// every board material so the 16a WebGPU renderer draws rect fills unlit at
// full color through its custom-WGSL path (sceneSelenaIsMaterial reads
// shaderBackend/shaderLayout/customVertexWGSL/customFragmentWGSL; the per-rect
// color rides customUniforms.baseColor). Board bundles only ever carry rect
// fill materials — lines/labels/sprites hold their colors inline — so the
// attach applies to all of b.Materials. Materials that already carry custom
// WGSL are left untouched, which makes the attach idempotent.
//
// The native Go renderer ignores these fields (fixed pipeline, oracle-only
// dim-lit board), and the 26b1 canvas2d painter reads only Color — the attach
// stays additive and safe for any board bundle, like the geometry attach.
func attachBoardFillMaterials(b rootengine.RenderBundle) rootengine.RenderBundle {
	if len(b.Materials) == 0 {
		return b
	}
	fill, err := boardFillCompiled()
	if err != nil {
		for _, d := range b.Diagnostics {
			if d.Code == "board-fill-selena-compile-failed" {
				return b
			}
		}
		b.Diagnostics = append(b.Diagnostics, rootengine.RenderDiagnostic{
			Severity: "warning",
			Code:     "board-fill-selena-compile-failed",
			Backend:  "webgpu",
			Target:   "BoardFill",
			Message:  fmt.Sprintf("board fill Selena material failed to compile; rect fills fall back to the renderer's default material path: %v", err),
		})
		return b
	}
	for i := range b.Materials {
		m := &b.Materials[i]
		if m.CustomVertexWGSL != "" || m.CustomFragmentWGSL != "" {
			continue
		}
		m.CustomVertexWGSL = fill.VertexWGSL
		m.CustomFragmentWGSL = fill.FragmentWGSL
		m.ShaderBackend = fill.ShaderBackend
		// One immutable layout map shared by every board material (and every
		// bundle) — it describes the shader, not the instance, and nothing
		// downstream mutates it: the marshal path only reads, and the JS scene
		// path clones on ingest (13-scene-material.js sceneCloneData).
		m.ShaderLayout = fill.ShaderLayout
		if rgb, ok := boardFillBaseColor(m.Color); ok {
			m.CustomUniforms = map[string]any{"baseColor": rgb}
		}
	}
	return b
}

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
// bundle's OrthoCamera2D camera (ADR 0004); color comes from the Selena BoardFill
// shader this also attaches to the rect materials (attachBoardFillMaterials) —
// custom WGSL the 16a WebGPU renderer draws unlit at full brightness, with the
// flat/Unlit Color kept alongside for the painter and the native oracle.
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
	return attachBoardFillMaterials(b)
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
