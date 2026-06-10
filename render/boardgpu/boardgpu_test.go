package boardgpu

import (
	"math"
	"reflect"
	"strings"
	"testing"

	rootengine "m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/bundle"
)

// TestBoardFillBaseColor pins the #rrggbb→vec3 conversion and the fallback
// contract (anything else → no override, layout default rides). Moved here from
// render/bundle2d when AttachBoardGPUGeometry + boardFillBaseColor were hoisted
// into this leaf package so client/vm can route to the GPU bundle without an
// import cycle.
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

// TestBoardFillSelenaSourceEmbedded guards the embed: an empty source means the
// //go:embed board_fill.sel directive failed to bundle the file with the package.
func TestBoardFillSelenaSourceEmbedded(t *testing.T) {
	if strings.TrimSpace(BoardFillSelenaSource) == "" {
		t.Fatal("BoardFillSelenaSource is empty (embed failed)")
	}
	if !strings.Contains(BoardFillSelenaSource, "material BoardFill") {
		t.Errorf("BoardFillSelenaSource missing the BoardFill material declaration")
	}
}

// rectBundle builds a minimal painter-shape bundle (Objects with Bounds, a flat
// material) the way package vm emits one, so the leaf package can unit-test the
// pure attach without depending on bundle2d's gosx-typed constructors.
func rectBundle(zoom float64) rootengine.RenderBundle {
	return rootengine.RenderBundle{
		Camera: bundle.OrthoCamera2D(zoom, 0, 0, 360, 180),
		Objects: []rootengine.RenderObject{
			{Kind: "rect", MaterialIndex: 0, Bounds: rootengine.RenderBounds{MinX: -100, MinY: 0, MaxX: -30, MaxY: 70}},
		},
		Materials: []rootengine.RenderMaterial{{Kind: "flat", Color: "#ff0000", Unlit: true}},
	}
}

// TestAttachBoardGPUGeometryExpandsRectQuad pins the rect → z=0 quad expansion
// and the BoardFill Selena attach directly on the leaf package's entry point.
func TestAttachBoardGPUGeometryExpandsRectQuad(t *testing.T) {
	b := AttachBoardGPUGeometry(rectBundle(1))
	if len(b.WorldPositions) != 18 || len(b.WorldNormals) != 18 || len(b.WorldUVs) != 12 {
		t.Fatalf("buffer lens = %d/%d/%d, want 18/18/12", len(b.WorldPositions), len(b.WorldNormals), len(b.WorldUVs))
	}
	if b.Objects[0].VertexOffset != 0 || b.Objects[0].VertexCount != 6 {
		t.Errorf("rect object offset/count = %d/%d, want 0/6", b.Objects[0].VertexOffset, b.Objects[0].VertexCount)
	}
	// First vertex = (MinX, MinY, 0).
	if b.WorldPositions[0] != -100 || b.WorldPositions[1] != 0 || b.WorldPositions[2] != 0 {
		t.Errorf("first vertex = (%v,%v,%v), want (-100,0,0)", b.WorldPositions[0], b.WorldPositions[1], b.WorldPositions[2])
	}
	// The flat rect material carries the BoardFill Selena attach with the parsed
	// baseColor override.
	m := b.Materials[0]
	if m.ShaderBackend != "selena" || strings.TrimSpace(m.CustomFragmentWGSL) == "" {
		t.Fatalf("rect material missing BoardFill Selena attach: %+v", m)
	}
	if got, _ := m.CustomUniforms["baseColor"].([]float32); !reflect.DeepEqual(got, []float32{1, 0, 0}) {
		t.Errorf("baseColor = %v, want [1 0 0] (#ff0000)", got)
	}
}

// TestAttachBoardGPUGeometryNoopOnEmpty guards the empty-board early return.
func TestAttachBoardGPUGeometryNoopOnEmpty(t *testing.T) {
	b := AttachBoardGPUGeometry(rootengine.RenderBundle{Camera: bundle.OrthoCamera2D(1, 0, 0, 360, 180)})
	if len(b.WorldPositions) != 0 || len(b.Objects) != 0 {
		t.Errorf("empty board should stay empty: positions=%d objects=%d", len(b.WorldPositions), len(b.Objects))
	}
}

// TestAttachBoardGPUGeometryIdempotent: a bundle that already carries
// WorldPositions re-runs only the (idempotent) material attach — re-expanding
// would duplicate the line/sprite quads.
func TestAttachBoardGPUGeometryIdempotent(t *testing.T) {
	b := AttachBoardGPUGeometry(rectBundle(1))
	posLen := len(b.WorldPositions)
	objLen := len(b.Objects)
	b2 := AttachBoardGPUGeometry(b)
	if len(b2.WorldPositions) != posLen || len(b2.Objects) != objLen {
		t.Errorf("re-attach changed geometry: positions %d→%d objects %d→%d", posLen, len(b2.WorldPositions), objLen, len(b2.Objects))
	}
	if len(b2.Diagnostics) != 0 {
		t.Errorf("re-attach added diagnostics: %v", b2.Diagnostics)
	}
}

// TestAttachBoardGPULineZoomCompensation pins the SCREEN-px line width / zoom
// math on the leaf entry point (26b1 never scales ctx.lineWidth by zoom, so the
// world-unit half-width is max(width,1)/zoom/2). A +X segment extrudes along +Y.
func TestAttachBoardGPULineZoomCompensation(t *testing.T) {
	b := AttachBoardGPUGeometry(rootengine.RenderBundle{
		Camera: bundle.OrthoCamera2D(2, 0, 0, 640, 480),
		Lines:  []rootengine.RenderLine{{To: rootengine.RenderPoint{X: 10}, LineWidth: 1, Color: "#abcdef"}},
	})
	if len(b.Objects) != 1 || b.Objects[0].Kind != "line" {
		t.Fatalf("want 1 line object, got %+v", b.Objects)
	}
	// width 1 / zoom 2 = world width 0.5 → half-width 0.25.
	want := []float64{0, 0.25, 0, 0, -0.25, 0, 10, -0.25, 0, 0, 0.25, 0, 10, -0.25, 0, 10, 0.25, 0}
	for i := range want {
		if math.Abs(b.WorldPositions[i]-want[i]) > 1e-9 {
			t.Fatalf("WorldPositions[%d] = %v, want %v", i, b.WorldPositions[i], want[i])
		}
	}
}
