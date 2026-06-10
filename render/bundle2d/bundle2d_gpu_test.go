package bundle2d

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	rootengine "m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/bundle"
)

func TestAttachBoardGPUGeometry(t *testing.T) {
	nodes := []gosx.CanvasBoardNode{
		{ID: "a", Kind: "rect", X: -100, Y: 0, Width: 70, Height: 70, Color: "#ff0000"},
		{ID: "b", Kind: "rect", X: 50, Y: 10, Width: 40, Height: 20, Color: "#00ff00"},
	}
	b := ComputeCanvasGPUBundle(nodes, 360, 180, 1, 0, 0)

	if len(b.Objects) != 2 {
		t.Fatalf("want 2 objects, got %d", len(b.Objects))
	}
	// 6 verts/object: WorldPositions 18 floats, WorldNormals 18, WorldUVs 12.
	if got, want := len(b.WorldPositions), 2*18; got != want {
		t.Errorf("WorldPositions len=%d want %d", got, want)
	}
	if got, want := len(b.WorldNormals), 2*18; got != want {
		t.Errorf("WorldNormals len=%d want %d", got, want)
	}
	if got, want := len(b.WorldUVs), 2*12; got != want {
		t.Errorf("WorldUVs len=%d want %d", got, want)
	}
	// Objects point at their own 6-vertex slices, in order.
	if b.Objects[0].VertexOffset != 0 || b.Objects[0].VertexCount != 6 {
		t.Errorf("obj0 offset/count = %d/%d, want 0/6", b.Objects[0].VertexOffset, b.Objects[0].VertexCount)
	}
	if b.Objects[1].VertexOffset != 6 || b.Objects[1].VertexCount != 6 {
		t.Errorf("obj1 offset/count = %d/%d, want 6/6", b.Objects[1].VertexOffset, b.Objects[1].VertexCount)
	}
	// VertexCount must be triangle-list valid for the renderer's object path.
	for i, o := range b.Objects {
		if o.VertexCount%3 != 0 {
			t.Errorf("obj%d VertexCount=%d not a multiple of 3", i, o.VertexCount)
		}
	}
	// The first quad's vertices must equal object 0's rect bounds (z=0). First
	// vertex = (MinX, MinY, 0).
	bb := b.Objects[0].Bounds
	if b.WorldPositions[0] != bb.MinX || b.WorldPositions[1] != bb.MinY || b.WorldPositions[2] != 0 {
		t.Errorf("obj0 first vertex = (%v,%v,%v), want bounds min (%v,%v,0)",
			b.WorldPositions[0], b.WorldPositions[1], b.WorldPositions[2], bb.MinX, bb.MinY)
	}
	// All Z coordinates are 0 (the board sits on the z=0 plane).
	for i := 2; i < len(b.WorldPositions); i += 3 {
		if b.WorldPositions[i] != 0 {
			t.Fatalf("non-zero Z at vertex %d: %v", i/3, b.WorldPositions[i])
		}
	}
}

// TestAttachBoardGPUGeometry_NoObjectsIsNoop guards the empty-board path.
func TestAttachBoardGPUGeometry_NoObjectsIsNoop(t *testing.T) {
	b := ComputeCanvasGPUBundle(nil, 360, 180, 1, 0, 0)
	if len(b.WorldPositions) != 0 {
		t.Errorf("empty board should emit no WorldPositions, got %d", len(b.WorldPositions))
	}
}

// TestComputeCanvasGPUBundle_AttachesBoardFillSelena proves the GPU bundle's
// rect materials carry the compiled BoardFill Selena material in exactly the
// fields the 16a WebGPU renderer reads (16a-scene-webgpu.js
// sceneSelenaIsMaterial / sceneSelenaUniformValue): shaderBackend "selena",
// the binding layout with a baseColor uniform field, the WGSL in
// CustomVertexWGSL/CustomFragmentWGSL, and the theme color as the
// customUniforms.baseColor value.
func TestComputeCanvasGPUBundle_AttachesBoardFillSelena(t *testing.T) {
	nodes := []gosx.CanvasBoardNode{
		{ID: "a", Kind: "rect", X: 0, Y: 0, Width: 10, Height: 10, Color: "#3a86ff"},
		{ID: "b", Kind: "rect", X: 20, Y: 0, Width: 10, Height: 10, Color: "#ffbe0b"},
		{ID: "c", Kind: "rect", X: 40, Y: 0, Width: 10, Height: 10}, // colorless → layout default
	}
	b := ComputeCanvasGPUBundle(nodes, 360, 180, 1, 0, 0)
	if len(b.Materials) != 3 {
		t.Fatalf("want 3 materials (two colors + default slot), got %d", len(b.Materials))
	}

	wantBaseColor := map[string][]float32{
		"#3a86ff": {58.0 / 255, 134.0 / 255, 255.0 / 255},
		"#ffbe0b": {255.0 / 255, 190.0 / 255, 11.0 / 255},
	}
	for i, m := range b.Materials {
		if m.ShaderBackend != "selena" {
			t.Errorf("material %d ShaderBackend = %q, want selena", i, m.ShaderBackend)
		}
		if strings.TrimSpace(m.CustomVertexWGSL) == "" || strings.TrimSpace(m.CustomFragmentWGSL) == "" {
			t.Fatalf("material %d missing custom WGSL (vertex %d bytes, fragment %d bytes)",
				i, len(m.CustomVertexWGSL), len(m.CustomFragmentWGSL))
		}
		if !strings.Contains(m.CustomFragmentWGSL, "baseColor") {
			t.Errorf("material %d fragment WGSL does not reference baseColor", i)
		}
		if m.ShaderLayout == nil {
			t.Fatalf("material %d ShaderLayout is nil — 16a sceneSelenaMaterialLayout requires uniformBlock.fields", i)
		}
		if _, ok := m.ShaderLayout["uniformBlock"]; !ok {
			t.Errorf("material %d ShaderLayout missing uniformBlock", i)
		}
		// The painter/native color stays alongside the shader fields.
		if m.Kind != "flat" || !m.Unlit {
			t.Errorf("material %d kind/unlit = %q/%v, want flat/true", i, m.Kind, m.Unlit)
		}
		if want, ok := wantBaseColor[m.Color]; ok {
			got, _ := m.CustomUniforms["baseColor"].([]float32)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("material %d (%s) customUniforms.baseColor = %v, want %v", i, m.Color, got, want)
			}
		} else if m.CustomUniforms != nil {
			// Colorless slot: no override — the JS side falls back to the
			// layout default (board_fill.sel's themed rgb(0.13,0.14,0.18)).
			t.Errorf("material %d (color %q) should carry no customUniforms, got %v", i, m.Color, m.CustomUniforms)
		}
	}

	// Idempotent: re-attaching (hosts may chain AttachBoardGPUGeometry on an
	// already-GPU bundle) must not change the materials. Snapshot the whole
	// slice before re-attach (attach mutates the backing array in place) so a
	// re-attach clobbering CustomUniforms or ShaderLayout — not just the
	// fragment WGSL — fails the comparison.
	before := slices.Clone(b.Materials)
	b2 := AttachBoardGPUGeometry(b)
	if len(b2.Diagnostics) != 0 {
		t.Errorf("re-attach must not add diagnostics, got %v", b2.Diagnostics)
	}
	if !reflect.DeepEqual(b2.Materials, before) {
		t.Errorf("materials changed on second attach:\nbefore: %+v\nafter:  %+v", before, b2.Materials)
	}
}

// TestBoardFillBaseColor (the #rrggbb→vec3 conversion + fallback contract) now
// lives in render/boardgpu where boardFillBaseColor is defined (the helper moved
// to the leaf package so client/vm can route to AttachBoardGPUGeometry without an
// import cycle). The bundle2d-level material attach is still covered by
// TestComputeCanvasGPUBundle_AttachesBoardFillSelena above and the golden test.

// ---------------------------------------------------------------------------
// M1 slice 2A: board LINE and SPRITE quads on the GPU bundle.
// ---------------------------------------------------------------------------

// lineBundle builds a minimal ortho-2D bundle carrying only the given lines —
// the direct AttachBoardGPUGeometry unit-test input (the typed CanvasBoardNode
// wire cannot express LineWidth ≠ 1, so width/zoom math is pinned here).
func lineBundle(zoom float64, lines ...rootengine.RenderLine) rootengine.RenderBundle {
	return rootengine.RenderBundle{
		Camera: bundle.OrthoCamera2D(zoom, 0, 0, 640, 480),
		Lines:  lines,
	}
}

// assertVec asserts a float64 slice matches want within 1e-9 (perpendicular
// math goes through Hypot/division, so exact equality is too strict for
// diagonals).
func assertVec(t *testing.T, got, want []float64, label string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: len=%d want %d (%v vs %v)", label, len(got), len(want), got, want)
	}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Fatalf("%s[%d] = %v, want %v (full: %v vs %v)", label, i, got[i], want[i], got, want)
		}
	}
}

// TestAttachBoardGPULineQuads pins the perpendicular expansion math: each line
// becomes a z=0 quad of corners From±p, To±p with p = unit-perpendicular ×
// half-width, emitted as triangles (A,B,C)(A,C,D) for A=From+p B=From-p
// C=To-p D=To+p — butt-capped like 26b1's default ctx.lineCap.
func TestAttachBoardGPULineQuads(t *testing.T) {
	cases := []struct {
		name string
		line rootengine.RenderLine
		zoom float64
		want []float64 // 18 floats: 6 verts × xyz
	}{
		{
			// width 4 → half-width 2, perp is +Y for a +X segment.
			name: "horizontal",
			line: rootengine.RenderLine{From: rootengine.RenderPoint{X: 10, Y: 20}, To: rootengine.RenderPoint{X: 30, Y: 20}, LineWidth: 4},
			zoom: 1,
			want: []float64{10, 22, 0, 10, 18, 0, 30, 18, 0, 10, 22, 0, 30, 18, 0, 30, 22, 0},
		},
		{
			// width 2 → half-width 1, perp is -X for a +Y segment.
			name: "vertical",
			line: rootengine.RenderLine{From: rootengine.RenderPoint{X: 5, Y: 0}, To: rootengine.RenderPoint{X: 5, Y: 10}, LineWidth: 2},
			zoom: 1,
			want: []float64{4, 0, 0, 6, 0, 0, 6, 10, 0, 4, 0, 0, 6, 10, 0, 4, 10, 0},
		},
		{
			// 45° segment, width 2√2 → half-width √2 → p = (-1, 1).
			name: "diagonal",
			line: rootengine.RenderLine{From: rootengine.RenderPoint{}, To: rootengine.RenderPoint{X: 10, Y: 10}, LineWidth: 2 * math.Sqrt2},
			zoom: 1,
			want: []float64{-1, 1, 0, 1, -1, 0, 11, 9, 0, -1, 1, 0, 11, 9, 0, 9, 11, 0},
		},
		{
			// Zoom compensation: lineWidth is SCREEN px (26b1 never scales
			// ctx.lineWidth by zoom), so world width = lineWidth/zoom.
			// zoom 2 → world width 0.5 → half-width 0.25.
			name: "zoom 2 halves world width",
			line: rootengine.RenderLine{To: rootengine.RenderPoint{X: 10}, LineWidth: 1},
			zoom: 2,
			want: []float64{0, 0.25, 0, 0, -0.25, 0, 10, -0.25, 0, 0, 0.25, 0, 10, -0.25, 0, 10, 0.25, 0},
		},
		{
			// zoom 0.5 → world width 2 (the shared fixtures' zoom).
			name: "zoom 0.5 doubles world width",
			line: rootengine.RenderLine{To: rootengine.RenderPoint{X: 10}, LineWidth: 1},
			zoom: 0.5,
			want: []float64{0, 1, 0, 0, -1, 0, 10, -1, 0, 0, 1, 0, 10, -1, 0, 10, 1, 0},
		},
		{
			// Width floor: LineWidth 0 (and sub-1) → 1 screen px.
			name: "zero width floors to 1px",
			line: rootengine.RenderLine{To: rootengine.RenderPoint{X: 10}},
			zoom: 1,
			want: []float64{0, 0.5, 0, 0, -0.5, 0, 10, -0.5, 0, 0, 0.5, 0, 10, -0.5, 0, 10, 0.5, 0},
		},
		{
			name: "sub-1 width floors to 1px",
			line: rootengine.RenderLine{To: rootengine.RenderPoint{X: 10}, LineWidth: 0.25},
			zoom: 1,
			want: []float64{0, 0.5, 0, 0, -0.5, 0, 10, -0.5, 0, 0, 0.5, 0, 10, -0.5, 0, 10, 0.5, 0},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := AttachBoardGPUGeometry(lineBundle(tc.zoom, tc.line))
			if len(b.Objects) != 1 {
				t.Fatalf("want 1 line object, got %d", len(b.Objects))
			}
			obj := b.Objects[0]
			if obj.Kind != "line" || obj.VertexOffset != 0 || obj.VertexCount != 6 {
				t.Errorf("object = kind %q offset %d count %d, want line/0/6", obj.Kind, obj.VertexOffset, obj.VertexCount)
			}
			assertVec(t, b.WorldPositions, tc.want, "WorldPositions")
			if len(b.WorldNormals) != 18 || len(b.WorldUVs) != 12 {
				t.Errorf("normals/uvs len = %d/%d, want 18/12", len(b.WorldNormals), len(b.WorldUVs))
			}
			for i := 2; i < len(b.WorldNormals); i += 3 {
				if b.WorldNormals[i] != 1 {
					t.Fatalf("normal %d not +Z", i/3)
				}
			}
			// The object's bounds cover the expanded quad (used for view culling).
			minX, maxX := math.Inf(1), math.Inf(-1)
			minY, maxY := math.Inf(1), math.Inf(-1)
			for i := 0; i < len(tc.want); i += 3 {
				minX, maxX = math.Min(minX, tc.want[i]), math.Max(maxX, tc.want[i])
				minY, maxY = math.Min(minY, tc.want[i+1]), math.Max(maxY, tc.want[i+1])
			}
			bb := obj.Bounds
			for _, p := range [][2]float64{{bb.MinX, minX}, {bb.MaxX, maxX}, {bb.MinY, minY}, {bb.MaxY, maxY}} {
				if math.Abs(p[0]-p[1]) > 1e-9 {
					t.Errorf("bounds %+v do not cover quad AABB [%v %v %v %v]", bb, minX, minY, maxX, maxY)
					break
				}
			}
		})
	}
}

// TestAttachBoardGPULineDegenerateSkipped: a zero-length segment paints
// nothing in 26b1 (canvas2d strokes a single-point subpath only with round/
// square caps; the painter leaves the default butt cap), so the GPU bundle
// emits no quad for it.
func TestAttachBoardGPULineDegenerateSkipped(t *testing.T) {
	b := AttachBoardGPUGeometry(lineBundle(1,
		rootengine.RenderLine{From: rootengine.RenderPoint{X: 7, Y: 7}, To: rootengine.RenderPoint{X: 7, Y: 7}, Color: "#ff0000", LineWidth: 3},
		rootengine.RenderLine{To: rootengine.RenderPoint{X: 10}, Color: "#00ff00", LineWidth: 1},
	))
	if len(b.Objects) != 1 {
		t.Fatalf("degenerate line must be skipped: want 1 object, got %d", len(b.Objects))
	}
	if len(b.WorldPositions) != 18 {
		t.Errorf("WorldPositions len = %d, want 18 (one quad)", len(b.WorldPositions))
	}
	if got := len(b.Materials); got != 1 {
		t.Errorf("skipped line must not allocate a material: got %d", got)
	}
	if b.Materials[0].Color != "#00ff00" {
		t.Errorf("surviving material color = %q, want #00ff00", b.Materials[0].Color)
	}
}

// TestAttachBoardGPULineMaterials pins the dedupe contract: one flat material
// per distinct line color, shared with rect materials of the same color
// (mirrors vm's ensureCanvasBoardMaterial), all carrying the BoardFill Selena
// attach exactly like rect fills.
func TestAttachBoardGPULineMaterials(t *testing.T) {
	nodes := []gosx.CanvasBoardNode{
		{ID: "card", Kind: "rect", X: 0, Y: 0, Width: 10, Height: 10, Color: "#3a86ff"},
		{ID: "e1", Kind: "line", X1: 0, Y1: 0, X2: 10, Y2: 0, Color: "#8d99ae"},
		{ID: "e2", Kind: "line", X1: 0, Y1: 5, X2: 10, Y2: 5, Color: "#8d99ae"}, // same color → same material
		{ID: "e3", Kind: "line", X1: 0, Y1: 9, X2: 10, Y2: 9, Color: "#3a86ff"}, // rect's color → rect's material
	}
	b := ComputeCanvasGPUBundle(nodes, 640, 480, 1, 0, 0)
	if len(b.Materials) != 2 {
		t.Fatalf("want 2 materials (#3a86ff shared, #8d99ae deduped), got %d: %+v", len(b.Materials), b.Materials)
	}
	if len(b.Objects) != 4 {
		t.Fatalf("want 4 objects (1 rect + 3 lines), got %d", len(b.Objects))
	}
	if b.Objects[1].MaterialIndex != 1 || b.Objects[2].MaterialIndex != 1 {
		t.Errorf("same-color lines must share material 1, got %d/%d", b.Objects[1].MaterialIndex, b.Objects[2].MaterialIndex)
	}
	if b.Objects[3].MaterialIndex != 0 {
		t.Errorf("rect-colored line must reuse material 0, got %d", b.Objects[3].MaterialIndex)
	}
	for i, m := range b.Materials {
		if m.Kind != "flat" || !m.Unlit {
			t.Errorf("material %d kind/unlit = %q/%v, want flat/true", i, m.Kind, m.Unlit)
		}
		if m.ShaderBackend != "selena" || m.CustomFragmentWGSL == "" {
			t.Errorf("material %d missing the BoardFill Selena attach", i)
		}
	}
}

// TestAttachBoardGPUSpriteQuad pins the sprite world rect and UV orientation:
// RenderSprite.Position is the world BOTTOM-LEFT corner (26b1 paints
// fillRect(spx, spy - h·zoom, w·zoom, h·zoom), whose top edge is world
// y+Height after the screen Y-flip), and V=0 maps to the world-rect TOP so a
// top-left-origin image upload (copyExternalImageToTexture) draws upright.
func TestAttachBoardGPUSpriteQuad(t *testing.T) {
	nodes := []gosx.CanvasBoardNode{
		{ID: "logo", Kind: "image", X: 30, Y: 40, Width: 32, Height: 32, Src: "/logo.png"},
	}
	b := ComputeCanvasGPUBundle(nodes, 640, 480, 1, 0, 0)
	if len(b.Objects) != 1 {
		t.Fatalf("want 1 sprite object, got %d", len(b.Objects))
	}
	obj := b.Objects[0]
	if obj.Kind != "sprite" || obj.ID != "logo" || obj.VertexOffset != 0 || obj.VertexCount != 6 {
		t.Errorf("object = %+v, want kind sprite id logo offset 0 count 6", obj)
	}
	// World rect spans x..x+W, y..y+H — same corner convention as rect bounds.
	assertVec(t, b.WorldPositions, []float64{
		30, 40, 0, 62, 40, 0, 62, 72, 0,
		30, 40, 0, 62, 72, 0, 30, 72, 0,
	}, "WorldPositions")
	// V flipped relative to rect UVs: world bottom (y=40) carries V=1, world
	// top (y=72) carries V=0.
	assertVec(t, b.WorldUVs, []float64{0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 0, 0}, "WorldUVs")
	wantBounds := rootengine.RenderBounds{MinX: 30, MinY: 40, MaxX: 62, MaxY: 72}
	if obj.Bounds != wantBounds {
		t.Errorf("bounds = %+v, want %+v", obj.Bounds, wantBounds)
	}
}

// TestAttachBoardGPUSpriteMaterialContract pins the sprite material contract:
// each sprite appends its OWN material carrying exactly {Kind:"sprite",
// Texture:Src, Color:"#ffffff", Unlit:true} and NO Selena fields. Sprites
// render through 16a's default PBR object path (unlit albedo-texture
// passthrough), not a custom Selena shader, so the material stays bare; the
// WHITE Color makes the PBR `albedo = material.albedo * texAlbedo.rgb` multiply
// the identity (an absent Color would dim the texture to 16a's 0.8 grey
// fallback). Per-sprite (not deduped): two sprites sharing a Src still get two
// materials.
func TestAttachBoardGPUSpriteMaterialContract(t *testing.T) {
	nodes := []gosx.CanvasBoardNode{
		{ID: "a", Kind: "image", X: 0, Y: 0, Width: 8, Height: 8, Src: "/logo.png"},
		{ID: "b", Kind: "image", X: 20, Y: 0, Width: 8, Height: 8, Src: "/logo.png"},
	}
	b := ComputeCanvasGPUBundle(nodes, 640, 480, 1, 0, 0)
	if len(b.Materials) != 2 {
		t.Fatalf("sprite materials are per-sprite: want 2, got %d", len(b.Materials))
	}
	for i, m := range b.Materials {
		if m.Kind != "sprite" || m.Texture != "/logo.png" || !m.Unlit {
			t.Errorf("material %d = %+v, want {Kind:sprite Texture:/logo.png Unlit:true}", i, m)
		}
		if m.Color != "#ffffff" {
			t.Errorf("material %d color = %q, want #ffffff (white albedo so the PBR texture multiply is identity)", i, m.Color)
		}
		if m.ShaderBackend != "" || m.CustomVertexWGSL != "" || m.CustomFragmentWGSL != "" || m.ShaderLayout != nil || m.CustomUniforms != nil {
			t.Errorf("material %d must carry NO Selena fields (sprites use the default PBR path): %+v", i, m)
		}
	}
	if b.Objects[0].MaterialIndex == b.Objects[1].MaterialIndex {
		t.Errorf("sprites must not share materials: both index %d", b.Objects[0].MaterialIndex)
	}
}

// TestAttachBoardGPUSpriteZeroSizeSkipped mirrors the 26b1 guard (`sw > 0 &&
// sh > 0`): zero-/negative-size sprites paint nothing, so no quad/material.
func TestAttachBoardGPUSpriteZeroSizeSkipped(t *testing.T) {
	b := AttachBoardGPUGeometry(rootengine.RenderBundle{
		Camera: bundle.OrthoCamera2D(1, 0, 0, 640, 480),
		Sprites: []rootengine.RenderSprite{
			{ID: "flat", Position: rootengine.RenderPoint{X: 1, Y: 1}, Width: 10, Height: 0, Src: "/a.png"},
			{ID: "neg", Position: rootengine.RenderPoint{X: 1, Y: 1}, Width: -5, Height: 10, Src: "/b.png"},
		},
	})
	if len(b.Objects) != 0 || len(b.Materials) != 0 || len(b.WorldPositions) != 0 {
		t.Errorf("zero-size sprites must be skipped: objects=%d materials=%d positions=%d",
			len(b.Objects), len(b.Materials), len(b.WorldPositions))
	}
}

// TestAttachBoardGPUZOrder pins painter parity for draw order: 26b1 paints
// rects → lines → sprites (labels are DOM-side), and the GPU path draws
// meshObjects in array order — so Objects must be appended rects, then lines,
// then sprites, with contiguous vertex slices in that order.
func TestAttachBoardGPUZOrder(t *testing.T) {
	nodes := []gosx.CanvasBoardNode{
		{ID: "logo", Kind: "image", X: 30, Y: 40, Width: 32, Height: 32, Src: "/logo.png"},
		{ID: "edge", Kind: "line", X1: 0, Y1: 0, X2: 50, Y2: 50, Color: "#ef233c"},
		{ID: "card", Kind: "rect", X: 16, Y: 24, Width: 200, Height: 120, Color: "#3a86ff"},
		{ID: "title", Kind: "label", X: 20, Y: 20, Text: "Board", Color: "#edf2f4"},
	}
	b := ComputeCanvasGPUBundle(nodes, 640, 480, 1, 0, 0)
	kinds := make([]string, len(b.Objects))
	for i, o := range b.Objects {
		kinds[i] = o.Kind
	}
	if !reflect.DeepEqual(kinds, []string{"rect", "line", "sprite"}) {
		t.Fatalf("object order = %v, want [rect line sprite] (painter z-order)", kinds)
	}
	for i, o := range b.Objects {
		if o.VertexOffset != i*6 || o.VertexCount != 6 {
			t.Errorf("object %d offset/count = %d/%d, want %d/6", i, o.VertexOffset, o.VertexCount, i*6)
		}
	}
	if len(b.WorldPositions) != 3*18 || len(b.WorldNormals) != 3*18 || len(b.WorldUVs) != 3*12 {
		t.Errorf("buffer lens = %d/%d/%d, want 54/54/36",
			len(b.WorldPositions), len(b.WorldNormals), len(b.WorldUVs))
	}
	if b.ObjectCount != 3 {
		t.Errorf("ObjectCount = %d, want 3 (kept in sync with appended Objects)", b.ObjectCount)
	}
	// Labels stay wire-only (M1 slice 2C renders them as a DOM overlay).
	if len(b.Labels) != 1 {
		t.Errorf("labels must stay on the wire: got %d", len(b.Labels))
	}
}

// TestAttachBoardGPUIdempotent: re-attaching a GPU bundle (hosts may chain
// the attach) must not duplicate line/sprite quads, objects, or materials.
func TestAttachBoardGPUIdempotent(t *testing.T) {
	nodes := []gosx.CanvasBoardNode{
		{ID: "card", Kind: "rect", X: 0, Y: 0, Width: 10, Height: 10, Color: "#3a86ff"},
		{ID: "edge", Kind: "line", X1: 0, Y1: 0, X2: 50, Y2: 50, Color: "#ef233c"},
		{ID: "logo", Kind: "image", X: 30, Y: 40, Width: 32, Height: 32, Src: "/logo.png"},
	}
	b := ComputeCanvasGPUBundle(nodes, 640, 480, 1, 0, 0)
	objects := slices.Clone(b.Objects)
	materials := slices.Clone(b.Materials)
	positions := slices.Clone(b.WorldPositions)

	b2 := AttachBoardGPUGeometry(b)
	if len(b2.Diagnostics) != 0 {
		t.Errorf("re-attach must not add diagnostics: %v", b2.Diagnostics)
	}
	if !reflect.DeepEqual(b2.Objects, objects) {
		t.Errorf("objects changed on re-attach:\nbefore %+v\nafter  %+v", objects, b2.Objects)
	}
	if !reflect.DeepEqual(b2.Materials, materials) {
		t.Errorf("materials changed on re-attach")
	}
	if !reflect.DeepEqual(b2.WorldPositions, positions) {
		t.Errorf("WorldPositions changed on re-attach: len %d → %d", len(positions), len(b2.WorldPositions))
	}
}

// TestAttachBoardGPULinesOnlyBoard: a board with no rects still gets GPU
// geometry for its lines (the attach must not early-return on empty Objects).
func TestAttachBoardGPULinesOnlyBoard(t *testing.T) {
	nodes := []gosx.CanvasBoardNode{
		{ID: "edge", Kind: "line", X1: 0, Y1: 0, X2: 50, Y2: 0, Color: "#ef233c"},
	}
	b := ComputeCanvasGPUBundle(nodes, 640, 480, 1, 0, 0)
	if len(b.Objects) != 1 || b.Objects[0].Kind != "line" {
		t.Fatalf("lines-only board must emit a line object, got %+v", b.Objects)
	}
	if len(b.WorldPositions) != 18 {
		t.Errorf("WorldPositions len = %d, want 18", len(b.WorldPositions))
	}
	if len(b.Materials) != 1 || b.Materials[0].ShaderBackend != "selena" {
		t.Errorf("line material must carry the BoardFill Selena attach: %+v", b.Materials)
	}
}

// TestPainterBundleUnchangedByGPUPath guards the 26b1 isolation contract:
// ComputeCanvasBundle (the painter constructor, what gosx-studio's site map
// inlines) must stay exactly as before — no World* buffers, Objects = rects
// only, materials untouched. Lines/sprites render via bundle.Lines/.Sprites
// in the painter; only ComputeCanvasGPUBundle appends them to Objects.
func TestPainterBundleUnchangedByGPUPath(t *testing.T) {
	nodes := []gosx.CanvasBoardNode{
		{ID: "card", Kind: "rect", X: 0, Y: 0, Width: 10, Height: 10, Color: "#3a86ff"},
		{ID: "edge", Kind: "line", X1: 0, Y1: 0, X2: 50, Y2: 50, Color: "#ef233c"},
		{ID: "logo", Kind: "image", X: 30, Y: 40, Width: 32, Height: 32, Src: "/logo.png"},
	}
	b := ComputeCanvasBundle(nodes, 640, 480, 1, 0, 0)
	if len(b.WorldPositions) != 0 || len(b.WorldNormals) != 0 || len(b.WorldUVs) != 0 {
		t.Errorf("painter bundle must carry no GPU vertex buffers")
	}
	if len(b.Objects) != 1 || b.Objects[0].Kind != "rect" {
		t.Errorf("painter bundle objects = %+v, want the single rect", b.Objects)
	}
	if len(b.Materials) != 1 || b.Materials[0].CustomFragmentWGSL != "" {
		t.Errorf("painter bundle materials must stay bare flat fills: %+v", b.Materials)
	}
	if len(b.Lines) != 1 || len(b.Sprites) != 1 {
		t.Errorf("painter bundle lines/sprites = %d/%d, want 1/1", len(b.Lines), len(b.Sprites))
	}
}
