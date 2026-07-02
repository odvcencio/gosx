package scene

import "testing"

func TestPolygonGeometrySquare(t *testing.T) {
	geom := PolygonGeometry([]float64{0, 0, 4, 0, 4, 4, 0, 4}, nil, 2.5)

	if len(geom.Indices) != 6 {
		t.Fatalf("expected 2 triangles (6 indices), got %d indices: %v", len(geom.Indices), geom.Indices)
	}
	if len(geom.Positions) != 4*3 {
		t.Fatalf("expected 4 vertices * 3 floats, got %d", len(geom.Positions))
	}
	if len(geom.Normals) != len(geom.Positions) {
		t.Fatalf("expected one normal triple per position triple, got %d normals vs %d positions",
			len(geom.Normals), len(geom.Positions))
	}

	// Every vertex must sit at the requested elevation, with X/Z carrying the
	// original 2D polygon coordinates (the engine's ground-plane convention).
	for i := 0; i < len(geom.Positions); i += 3 {
		y := geom.Positions[i+1]
		if y != 2.5 {
			t.Fatalf("vertex %d: y=%v, want 2.5", i/3, y)
		}
	}
	// Normals must point straight up.
	for i := 0; i < len(geom.Normals); i += 3 {
		nx, ny, nz := geom.Normals[i], geom.Normals[i+1], geom.Normals[i+2]
		if nx != 0 || ny != 1 || nz != 0 {
			t.Fatalf("normal %d = (%v,%v,%v), want (0,1,0)", i/3, nx, ny, nz)
		}
	}

	// Indices must reference valid vertices and describe the square's area:
	// sum of triangle areas (in XZ) should equal the square's area (16).
	area := 0.0
	for i := 0; i < len(geom.Indices); i += 3 {
		a, b, c := geom.Indices[i], geom.Indices[i+1], geom.Indices[i+2]
		ax, az := geom.Positions[a*3], geom.Positions[a*3+2]
		bx, bz := geom.Positions[b*3], geom.Positions[b*3+2]
		cx, cz := geom.Positions[c*3], geom.Positions[c*3+2]
		area += abs(((bx-ax)*(cz-az) - (cx-ax)*(bz-az)) / 2)
	}
	if area != 16 {
		t.Fatalf("triangulated area = %v, want 16", area)
	}
}

func TestPolygonGeometryWithHole(t *testing.T) {
	outer := []float64{0, 0, 10, 0, 10, 10, 0, 10}
	holes := [][]float64{{2, 2, 8, 2, 8, 8, 2, 8}}

	geom := PolygonGeometry(outer, holes, 0)
	if len(geom.Indices) == 0 {
		t.Fatal("expected a non-empty triangulation with a hole")
	}
	// outer square (100) minus hole square (36) == 64
	area := 0.0
	for i := 0; i < len(geom.Indices); i += 3 {
		a, b, c := geom.Indices[i], geom.Indices[i+1], geom.Indices[i+2]
		ax, az := geom.Positions[a*3], geom.Positions[a*3+2]
		bx, bz := geom.Positions[b*3], geom.Positions[b*3+2]
		cx, cz := geom.Positions[c*3], geom.Positions[c*3+2]
		area += abs(((bx-ax)*(cz-az) - (cx-ax)*(bz-az)) / 2)
	}
	if area != 64 {
		t.Fatalf("triangulated area with hole = %v, want 64", area)
	}
}

func TestPolygonGeometryDegenerateReturnsEmpty(t *testing.T) {
	// A single point (or empty polygon) can't be triangulated.
	geom := PolygonGeometry(nil, nil, 0)
	if len(geom.Positions) != 0 || len(geom.Indices) != 0 {
		t.Fatalf("expected empty BufferGeometry for degenerate input, got %+v", geom)
	}
}

// TestPolygonGeometryLowersLikeBufferGeometry confirms the bridge's output
// plugs straight into Mesh{Geometry: ...} without any further conversion —
// the whole point of keeping the integration surface to a single helper.
func TestPolygonGeometryLowersLikeBufferGeometry(t *testing.T) {
	geom := PolygonGeometry([]float64{0, 0, 4, 0, 4, 4, 0, 4}, nil, 0)
	props := Props{Graph: NewGraph(Mesh{
		ID:       "polygon-floor",
		Geometry: geom,
		Material: StandardMaterial{Color: "#ffffff"},
	})}
	ir := props.SceneIR()
	if len(ir.Objects) != 1 {
		t.Fatalf("expected 1 object, got %d", len(ir.Objects))
	}
	obj := ir.Objects[0]
	if obj.Kind != "gltf-mesh" {
		t.Fatalf("expected kind gltf-mesh, got %q", obj.Kind)
	}
	if obj.Vertices == nil || obj.Vertices.Count != 6 {
		t.Fatalf("expected 6 expanded (non-indexed) vertices, got %+v", obj.Vertices)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
