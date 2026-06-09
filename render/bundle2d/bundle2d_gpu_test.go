package bundle2d

import (
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx"
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
	// already-GPU bundle) must not change the materials.
	before := make([]string, len(b.Materials))
	for i, m := range b.Materials {
		before[i] = m.CustomFragmentWGSL
	}
	b2 := AttachBoardGPUGeometry(b)
	for i, m := range b2.Materials {
		if m.CustomFragmentWGSL != before[i] {
			t.Errorf("material %d changed on second attach", i)
		}
		if len(b2.Diagnostics) != 0 {
			t.Errorf("re-attach must not add diagnostics, got %v", b2.Diagnostics)
		}
	}
}

// TestBoardFillBaseColor pins the #rrggbb→vec3 conversion and the fallback
// contract (anything else → no override, layout default rides).
func TestBoardFillBaseColor(t *testing.T) {
	if rgb, ok := boardFillBaseColor("#ff8800"); !ok || !reflect.DeepEqual(rgb, []float32{1, 136.0 / 255, 0}) {
		t.Errorf("#ff8800 = %v/%v, want [1 0.53333336 0]/true", rgb, ok)
	}
	for _, bad := range []string{"", "red", "#fff", "#12345", "#gggggg", "rgb(1,2,3)"} {
		if _, ok := boardFillBaseColor(bad); ok {
			t.Errorf("boardFillBaseColor(%q) ok=true, want false", bad)
		}
	}
}
