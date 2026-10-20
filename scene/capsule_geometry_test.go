package scene

import (
	"math"
	"slices"
	"testing"

	"m31labs.dev/gosx/scene/geom"
)

func TestCapsuleGeometryIndexedBufferGeometry(t *testing.T) {
	g := CapsuleGeometry(CapsuleGeometryOptions{})
	vertices := len(g.Positions) / 3
	if vertices == 0 {
		t.Fatal("CapsuleGeometry returned empty geometry")
	}
	if vertices != geom.CapsuleVertexCount(4, 8) {
		t.Fatalf("vertex count = %d, want %d", vertices, geom.CapsuleVertexCount(4, 8))
	}
	wantIndices := 12 * 8 * 4
	if len(g.Indices) != wantIndices {
		t.Fatalf("index count = %d, want %d", len(g.Indices), wantIndices)
	}
	for _, idx := range g.Indices {
		if idx < 0 || idx >= vertices {
			t.Fatalf("index out of range: %d", idx)
		}
	}
	if len(g.Normals) != vertices*3 {
		t.Error("expected normals on public capsule geometry")
	}
	if len(g.UVs) != vertices*2 {
		t.Error("expected uvs on public capsule geometry")
	}
}

func TestCapsuleGeometryMatchesCanonicalGenerator(t *testing.T) {
	opts := CapsuleGeometryOptions{Radius: 0.6, Length: 1.25, CapSegments: 5, RadialSegments: 9}
	geometry := CapsuleGeometry(opts)
	want := geom.Capsule(opts.Radius, opts.Length, opts.CapSegments, opts.RadialSegments, geom.AttrNormals|geom.AttrUVs)
	if want == nil {
		t.Fatal("canonical geom.Capsule returned nil")
	}
	if !slices.Equal(geometry.Positions, want.Positions) ||
		!slices.Equal(geometry.Normals, want.Normals) ||
		!slices.Equal(geometry.UVs, want.UVs) ||
		!slices.Equal(geometry.Indices, want.Indices) {
		t.Fatal("CapsuleGeometry diverges from the canonical geom.Capsule build")
	}
}

func TestCapsuleGeometryLoweringExpansionContract(t *testing.T) {
	g := CapsuleGeometry(CapsuleGeometryOptions{})
	vertices := bufferGeometryVertices(g)
	if vertices == nil {
		t.Fatal("BufferGeometry lowering returned nil")
	}
	// Defaults draw 4*R*S = 128 triangles through the authored index list.
	wantDrawn := 4 * 8 * 4 * 3
	if len(vertices.Indices) != wantDrawn {
		t.Fatalf("drawn index count = %d, want %d", len(vertices.Indices), wantDrawn)
	}
	if vertices.Count != len(g.Positions)/3 {
		t.Fatalf("unique vertex count = %d, want %d", vertices.Count, len(g.Positions)/3)
	}
	if len(vertices.Positions) != vertices.Count*3 {
		t.Fatalf("unique positions = %d, want %d", len(vertices.Positions), vertices.Count*3)
	}
	// Every drawn vertex still sits exactly on the unit-radius surface.
	for _, raw := range vertices.Indices {
		i := int(raw)
		x, y, z := vertices.Positions[i*3], vertices.Positions[i*3+1], vertices.Positions[i*3+2]
		r := math.Hypot(x, z)
		limit := 1.0
		if math.Abs(y) > 0.5 {
			limit = math.Sqrt(max(1e-12, 1-(math.Abs(y)-0.5)*(math.Abs(y)-0.5)))
		}
		if r > limit+1e-9 || r < limit-1e-6 {
			t.Errorf("drawn vertex %d radius = %g at y=%g, want about %g", i, r, y, limit)
		}
	}
}

func TestCapsuleGeometryExactRaycast(t *testing.T) {
	g := CapsuleGeometry(CapsuleGeometryOptions{})
	if len(g.Positions) == 0 {
		t.Fatal("CapsuleGeometry returned empty geometry")
	}

	tests := []struct {
		name       string
		ray        Ray
		wantHit    bool
		wantMethod string
		checkPoint func(*testing.T, Vector3)
	}{
		{
			name:       "body hit",
			ray:        Ray{Origin: Vector3{X: 5}, Direction: Vector3{X: -1}},
			wantHit:    true,
			wantMethod: "mesh-triangle",
			checkPoint: func(t *testing.T, p Vector3) {
				if math.Abs(p.X-1) > 1e-9 {
					t.Errorf("body hit x = %g, want 1", p.X)
				}
				if math.Abs(p.Y) > 0.5+1e-9 {
					t.Errorf("body hit y = %g outside the cylindrical body", p.Y)
				}
			},
		},
		{
			name:       "cap hit",
			ray:        Ray{Origin: Vector3{Y: -5}, Direction: Vector3{Y: 1}},
			wantHit:    true,
			wantMethod: "mesh-triangle",
			checkPoint: func(t *testing.T, p Vector3) {
				if math.Abs(p.Y+1.5) > 1e-9 || math.Hypot(p.X, p.Z) > 1e-9 {
					t.Errorf("cap hit = (%g, %g, %g), want the bottom pole (0, -1.5, 0)", p.X, p.Y, p.Z)
				}
			},
		},
		{
			name:    "miss above the capsule",
			ray:     Ray{Origin: Vector3{X: 5, Y: 2}, Direction: Vector3{X: -1}},
			wantHit: false,
		},
		{
			name:    "miss beside the capsule",
			ray:     Ray{Origin: Vector3{X: 5, Z: 3}, Direction: Vector3{X: -1}},
			wantHit: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hit, kind, ok := raycastGeometry(g, tc.ray, DefaultPointThreshold)
			if ok != tc.wantHit {
				t.Fatalf("raycastGeometry hit = %v, want %v (hit %+v)", ok, tc.wantHit, hit)
			}
			if !ok {
				return
			}
			if kind != "gltf-mesh" {
				t.Errorf("kind = %q, want gltf-mesh", kind)
			}
			if hit.Method != tc.wantMethod {
				t.Errorf("method = %q, want %q", hit.Method, tc.wantMethod)
			}
			tc.checkPoint(t, hit.Point)
		})
	}
}

// TestCapsuleGeometrySceneGraphRaycast exercises the full lowering path: a Mesh
// carrying CapsuleGeometry lowers to inline vertices and picks through the
// mesh-triangle raycaster.
func TestCapsuleGeometrySceneGraphRaycast(t *testing.T) {
	graph := NewGraph(Mesh{ID: "capsule", Geometry: CapsuleGeometry(CapsuleGeometryOptions{})})

	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(5, 0, 0), Direction: Vec3(-1, 0, 0)})
	if !ok {
		t.Fatal("scene graph raycast missed the capsule body")
	}
	if math.Abs(hit.Point.X-1) > 1e-9 {
		t.Errorf("hit point x = %g, want 1", hit.Point.X)
	}
	if _, ok := RaycastGraph(graph, Ray{Origin: Vec3(5, 2, 0), Direction: Vec3(-1, 0, 0)}); ok {
		t.Error("ray passing above the capsule reported a hit")
	}
}
