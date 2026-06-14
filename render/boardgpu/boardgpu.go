// Package boardgpu turns a Canvas2D board RenderBundle into GPU-drawable form:
// rect/line/sprite quads in the bundle's World* vertex streams (+ per-object
// RenderObjects in painter z-order) and the BoardFill Selena material on the
// flat fills. It is the composable half of the WebGPU canvas re-platform — the
// piece both the server-side constructors (render/bundle2d.ComputeCanvasGPUBundle*)
// and the live WASM adapter (client/vm.CanvasBoardAdapter, when routed to the
// WebGPU backend) apply to make a board renderable by the 16a JS WebGPU
// renderer / the native render/bundle GPU path.
//
// LEAF PACKAGE (acyclic seam): this code lives here — not in render/bundle2d —
// specifically so client/vm and client/bridge can call AttachBoardGPUGeometry
// directly. render/bundle2d imports BOTH client/vm and client/bridge (it
// re-marshals the WASM bundle byte-for-byte), so neither client/vm nor
// client/bridge can import bundle2d without a cycle. AttachBoardGPUGeometry and
// its helpers depend only on engine + the embedded/static BoardFill shader
// payload (verified: no scene, client/vm, client/bridge, or gosx runtime
// reference), so hoisting them into this dependency-free leaf lets the live
// adapter route to the GPU bundle while bundle2d keeps delegating to the same
// implementation (its ComputeCanvasGPUBundle* compose ComputeCanvasBundle +
// AttachBoardGPUGeometry, and its goldens stay green).
package boardgpu

import (
	"fmt"
	"math"
	"strings"

	rootengine "m31labs.dev/gosx/engine"
)

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
// every FLAT board material so the 16a WebGPU renderer draws rect and line
// fills unlit at full color through its custom-WGSL path
// (sceneSelenaIsMaterial reads shaderBackend/shaderLayout/customVertexWGSL/
// customFragmentWGSL; the per-fill color rides customUniforms.baseColor).
// Flat materials are the rect fills built by vm.ensureCanvasBoardMaterial and
// the line fills appended by AttachBoardGPUGeometry — both Kind "flat".
// Sprite materials (Kind "sprite", Texture set) are deliberately skipped:
// BoardFill samples no textures, so sprites render through 16a's default PBR
// object path instead (unlit albedo-texture passthrough — see
// appendBoardSpriteQuads). Materials that already carry custom WGSL are left
// untouched, which makes the attach idempotent.
//
// The native Go renderer ignores these fields (fixed pipeline, oracle-only
// dim-lit board), and the 26b1 canvas2d painter reads only Color — the attach
// stays additive and safe for any board bundle.
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
		if m.Kind != "flat" || m.Texture != "" {
			continue
		}
		if m.CustomVertexWGSL != "" || m.CustomFragmentWGSL != "" {
			continue
		}
		m.CustomVertexWGSL = fill.VertexWGSL
		m.CustomFragmentWGSL = fill.FragmentWGSL
		m.ShaderBackend = fill.ShaderBackend
		// 2D board fills have no back face to cull; "none" also keeps them
		// visible under the ortho-2D FlipY winding inversion.
		m.CullMode = "none"
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

// boardGeometry accumulates the bundle's packed triangle-list vertex streams
// while AttachBoardGPUGeometry appends rect, line, and sprite quads.
type boardGeometry struct {
	pos, nrm, uv []float64
	offset       int // next free vertex slot
}

// appendQuad appends one z=0 quad — counter-clockwise corners a→b→c→d, the
// same winding the rect path has always emitted — as the triangles (a,b,c)
// (a,c,d) plus the constant +Z board normals, and returns the quad's vertex
// offset. UVs differ per primitive, so callers append those themselves.
func (g *boardGeometry) appendQuad(ax, ay, bx, by, cx, cy, dx, dy float64) int {
	g.pos = append(g.pos,
		ax, ay, 0, bx, by, 0, cx, cy, 0,
		ax, ay, 0, cx, cy, 0, dx, dy, 0,
	)
	g.nrm = append(g.nrm, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1)
	offset := g.offset
	g.offset += 6
	return offset
}

// ensureBoardLineMaterial returns the index of the flat material carrying the
// given line color, appending one if absent. Mirrors vm's
// ensureCanvasBoardMaterial dedupe (rects with that color already produced an
// identical record, so same-color lines and rects share a material; empty
// colors collapse onto the shared default slot and render the BoardFill
// layout default). The Kind/Texture guards keep line fills from ever matching
// a sprite material.
func ensureBoardLineMaterial(b *rootengine.RenderBundle, color string) int {
	color = strings.TrimSpace(color)
	for i, existing := range b.Materials {
		if existing.Kind == "flat" && existing.Texture == "" && existing.Color == color {
			return i
		}
	}
	b.Materials = append(b.Materials, rootengine.RenderMaterial{
		Kind:  "flat",
		Color: color,
		Unlit: true,
	})
	return len(b.Materials) - 1
}

// appendBoardLineQuads expands every b.Lines segment into a z=0 quad: the
// segment From→To extruded by half the line width along the unit
// perpendicular, butt-capped exactly like 26b1's default ctx.lineCap (no
// extension past the endpoints).
//
// Width: RenderLine.LineWidth is SCREEN pixels — 26b1 camera-transforms the
// endpoints but never zoom-scales ctx.lineWidth — so the world-unit width is
// max(LineWidth,1)/zoom, with zoom read from the bundle's OrthoCamera2D
// (Camera.Z). The board pipeline recomputes the bundle every frame, so the
// on-screen thickness matches the painter exactly; if a host ever reuses one
// bundle across zoom changes the thickness becomes approximate (scales with
// the quad instead of staying constant) — accepted for M1. The floor also
// clamps sub-1px widths (the painter would stroke those fractionally; GPU
// quads thinner than a pixel shimmer) — accepted divergence.
//
// Degenerate segments (From==To) are skipped: a zero-length canvas2d stroke
// with the butt cap paints nothing, so 26b1 shows nothing for them either.
func appendBoardLineQuads(b *rootengine.RenderBundle, g *boardGeometry) {
	zoom := b.Camera.Z
	if zoom <= 0 {
		zoom = 1
	}
	for _, line := range b.Lines {
		dx, dy := line.To.X-line.From.X, line.To.Y-line.From.Y
		length := math.Hypot(dx, dy)
		if length == 0 {
			continue
		}
		width := line.LineWidth
		if width < 1 {
			width = 1
		}
		hw := width / zoom / 2
		// Unit perpendicular × half-width; quad corners From±p, To±p.
		px, py := -dy/length*hw, dx/length*hw
		offset := g.appendQuad(
			line.From.X+px, line.From.Y+py,
			line.From.X-px, line.From.Y-py,
			line.To.X-px, line.To.Y-py,
			line.To.X+px, line.To.Y+py,
		)
		// U runs From→To, V crosses the width (0 on the -p side).
		g.uv = append(g.uv, 0, 1, 0, 0, 1, 0, 0, 1, 1, 0, 1, 1)
		b.Objects = append(b.Objects, rootengine.RenderObject{
			Kind:          "line",
			MaterialIndex: ensureBoardLineMaterial(b, line.Color),
			VertexOffset:  offset,
			VertexCount:   6,
			Bounds: rootengine.RenderBounds{
				MinX: math.Min(line.From.X, line.To.X) - math.Abs(px),
				MinY: math.Min(line.From.Y, line.To.Y) - math.Abs(py),
				MaxX: math.Max(line.From.X, line.To.X) + math.Abs(px),
				MaxY: math.Max(line.From.Y, line.To.Y) + math.Abs(py),
			},
		})
	}
}

// appendBoardSpriteQuads expands every b.Sprites record into a z=0 quad over
// the sprite's world rect. RenderSprite.Position is the world BOTTOM-LEFT
// corner — vm's canvasBoardSprite passes node x/y/width/height straight
// through, and 26b1 paints fillRect(spx, spy - h·zoom, w·zoom, h·zoom), whose
// top edge sits at world y+Height after the screen Y-flip — so the rect spans
// x..x+Width, y..y+Height, the same corner convention as rect bounds.
// Zero-/negative-size sprites are skipped (26b1's `sw > 0 && sh > 0` guard
// paints nothing for them).
//
// UVs flip V relative to rect quads: V=0 maps to the world-rect TOP (maxY).
// The JS texture upload (copyExternalImageToTexture) keeps the image origin
// at the top-left, so V=0-at-top draws the image upright under the +Y-up
// ortho camera.
//
// Each sprite appends its OWN material: {Kind:"sprite", Texture:Src,
// Color:"#ffffff", Unlit:true}. Sprites render through 16a's default PBR
// object path (no Selena attach) — that path loads material.texture into the
// albedo texture slot and, because Unlit is set, its fragment WGSL returns the
// sampled albedo directly (the unlit branch bypasses the BRDF, so a board with
// no lights still shows the image instead of black). Color is WHITE so the
// shader's `albedo = material.albedo * texAlbedo.rgb` multiply is the identity
// 1.0 — an absent Color would default 16a's albedo to its 0.8 grey fallback
// and dim every sprite to 80%. While the image loads, 16a binds a 1×1 white
// placeholder (wgpuLoadTexture), so a sprite flashes a white box before the
// texture resolves — equal to or better than 26b1's grey placeholder.
func appendBoardSpriteQuads(b *rootengine.RenderBundle, g *boardGeometry) {
	for _, sprite := range b.Sprites {
		if sprite.Width <= 0 || sprite.Height <= 0 {
			continue
		}
		x0, y0 := sprite.Position.X, sprite.Position.Y
		x1, y1 := x0+sprite.Width, y0+sprite.Height
		offset := g.appendQuad(x0, y0, x1, y0, x1, y1, x0, y1)
		g.uv = append(g.uv, 0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 0, 0)
		b.Materials = append(b.Materials, rootengine.RenderMaterial{
			Kind:    "sprite",
			Texture: sprite.Src,
			Color:   "#ffffff",
			Unlit:   true,
		})
		b.Objects = append(b.Objects, rootengine.RenderObject{
			ID:            sprite.ID,
			Kind:          "sprite",
			MaterialIndex: len(b.Materials) - 1,
			VertexOffset:  offset,
			VertexCount:   6,
			Bounds: rootengine.RenderBounds{
				MinX: x0, MinY: y0,
				MaxX: x1, MaxY: y1,
			},
		})
	}
}

// AttachBoardGPUGeometry makes a Canvas2D board bundle GPU-renderable without a
// new pipeline. The board's rects live in bundle.Objects as a 2D-painter display
// list (kind/bounds/materialIndex) consumed by 26b1-canvas2d-painter.js, but the
// GPU scene renderers (native render/bundle's drawObjectMeshes and the JS 16a
// renderer) draw RenderObjects from the bundle's World* vertex buffers via
// VertexOffset/VertexCount — which the painter bundle leaves empty. This expands
// each object's rect bounds into a z=0 quad (2 triangles) in WorldPositions, with
// matching WorldNormals/WorldUVs, and points each object at its vertices — then
// appends one quad per b.Lines segment (appendBoardLineQuads) and one per
// b.Sprites record (appendBoardSpriteQuads), each with its own RenderObject.
// Objects land in painter z-order — rects, then lines, then sprites — which is
// the order the GPU object paths draw them in (labels stay wire-only; M1 slice
// 2C renders them as a DOM overlay). ObjectCount is kept in sync.
//
// DRY: the rect geometry has ONE source — the object's existing Bounds (already
// computed by package vm). Lighting/depth stay off via the bundle's
// OrthoCamera2D camera (ADR 0004); rect/line color comes from the Selena
// BoardFill shader this also attaches to the flat materials
// (attachBoardFillMaterials) — custom WGSL the 16a WebGPU renderer draws unlit
// at full brightness, with the flat/Unlit Color kept alongside for the native
// oracle. Line materials are deduped by color into the same flat pool the
// rects use; sprite materials are per-sprite (Texture=Src, white albedo,
// Unlit) and render unlit-textured through 16a's default PBR object path —
// they get no Selena attach.
//
// NOT 26b1-compatible: once line/sprite quads are appended to Objects, the
// bundle is GPU-only. The 26b1 painter would skip the appended objects (its
// loop draws kind "rect" only) and then stroke b.Lines/paint b.Sprites
// placeholders again from the untouched wire arrays — feed the painter
// ComputeCanvasBundle output instead (the constructors are separate, and the
// only production painter caller, gosx-studio's site map, uses the painter
// one).
//
// Idempotent: a bundle that already carries WorldPositions is returned with
// only the (itself idempotent) material attach re-applied — re-expanding
// would duplicate the appended line/sprite quads.
//
// Mutates and returns b for chained use.
func AttachBoardGPUGeometry(b rootengine.RenderBundle) rootengine.RenderBundle {
	if len(b.WorldPositions) > 0 {
		return attachBoardFillMaterials(b)
	}
	if len(b.Objects) == 0 && len(b.Lines) == 0 && len(b.Sprites) == 0 {
		return b
	}
	// 6 vertices (2 triangles) per rect/line/sprite quad.
	quads := len(b.Objects) + len(b.Lines) + len(b.Sprites)
	g := &boardGeometry{
		pos: make([]float64, 0, quads*6*3),
		nrm: make([]float64, 0, quads*6*3),
		uv:  make([]float64, 0, quads*6*2),
	}
	for i := range b.Objects {
		bb := b.Objects[i].Bounds
		x0, y0, x1, y1 := bb.MinX, bb.MinY, bb.MaxX, bb.MaxY
		// Two triangles: (x0,y0)(x1,y0)(x1,y1) and (x0,y0)(x1,y1)(x0,y1), z=0.
		b.Objects[i].VertexOffset = g.appendQuad(x0, y0, x1, y0, x1, y1, x0, y1)
		b.Objects[i].VertexCount = 6
		g.uv = append(g.uv, 0, 0, 1, 0, 1, 1, 0, 0, 1, 1, 0, 1)
	}
	appendBoardLineQuads(&b, g)
	appendBoardSpriteQuads(&b, g)
	b.WorldPositions = g.pos
	b.WorldNormals = g.nrm
	b.WorldUVs = g.uv
	b.ObjectCount = len(b.Objects)
	return attachBoardFillMaterials(b)
}
