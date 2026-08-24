package scene

import (
	"math"
	"testing"

	"m31labs.dev/gosx/scene/geom"
)

func TestLatheGeometryIndexedBufferGeometry(t *testing.T) {
	profile := []LathePoint{{Radius: 1, Y: 0}, {Radius: 1, Y: 2}}
	g := LatheGeometry(profile, 12, 0, 2*math.Pi)
	if len(g.Positions) == 0 {
		t.Fatal("LatheGeometry returned empty geometry")
	}
	indices := g.Indices
	if len(indices) != 12*2*3 {
		t.Fatalf("index count = %d, want %d", len(indices), 12*2*3)
	}
	positions := g.Positions
	if len(positions) != 26*3 {
		t.Fatalf("position count = %d, want %d", len(positions), 26*3)
	}
	if len(g.Normals) != 26*3 {
		t.Error("expected normals on public lathe geometry")
	}
	if len(g.UVs) != 26*2 {
		t.Error("expected uvs on public lathe geometry")
	}
	for _, idx := range indices {
		if idx < 0 || idx >= 26 {
			t.Fatalf("index out of range: %d", idx)
		}
	}
}

func TestLatheGeometryLoweringExpansionContract(t *testing.T) {
	profile := []LathePoint{{Radius: 1, Y: 0}, {Radius: 1, Y: 2}}
	g := LatheGeometry(profile, 6, 0, 2*math.Pi)
	vertices := bufferGeometryVertices(g)
	if vertices == nil {
		t.Fatal("BufferGeometry lowering returned nil")
	}
	drawn := vertices.Count
	if drawn != 6*2*3 {
		t.Fatalf("drawn vertex count = %d, want %d", drawn, 6*2*3)
	}
	if len(vertices.Positions) != drawn*3 {
		t.Fatalf("expanded positions = %d, want %d", len(vertices.Positions), drawn*3)
	}
	for i := 0; i < drawn; i++ {
		r := math.Hypot(vertices.Positions[i*3], vertices.Positions[i*3+2])
		if math.Abs(r-1) > 1e-9 {
			t.Errorf("expanded vertex radius = %g, want 1", r)
		}
	}
}

func TestLatheGeometryMatchesCanonicalGenerator(t *testing.T) {
	profile := []LathePoint{{Radius: 0.5, Y: -1}, {Radius: 1, Y: 0}, {Radius: 0.5, Y: 1}}
	geometry := LatheGeometry(profile, 9, 0, math.Pi)
	want := geom.Lathe([]float64{0.5, -1, 1, 0, 0.5, 1}, 9, 0, math.Pi, geom.AttrNormals|geom.AttrUVs)
	if want == nil {
		t.Fatal("canonical geom.Lathe returned nil")
	}
	if len(geometry.Positions) != len(want.Positions) {
		t.Fatalf("position length = %d, want %d", len(geometry.Positions), len(want.Positions))
	}
	for i := range want.Positions {
		if geometry.Positions[i] != want.Positions[i] {
			t.Fatalf("position %d = %g, want %g", i, geometry.Positions[i], want.Positions[i])
		}
	}
}
