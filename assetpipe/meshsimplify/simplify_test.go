package meshsimplify

import (
	"math"
	"testing"
)

// gridMesh builds a flat n by n quad grid on the XZ plane.
func gridMesh(n int) Mesh {
	var mesh Mesh
	for z := 0; z <= n; z++ {
		for x := 0; x <= n; x++ {
			mesh.Positions = append(mesh.Positions, float32(x)/float32(n), 0, float32(z)/float32(n))
		}
	}
	stride := uint32(n + 1)
	for z := 0; z < n; z++ {
		for x := 0; x < n; x++ {
			a := uint32(z)*stride + uint32(x)
			b := a + 1
			c := a + stride
			d := c + 1
			mesh.Indices = append(mesh.Indices, a, c, b, b, c, d)
		}
	}
	return mesh
}

// sphereMesh builds a UV sphere with welded poles.
func sphereMesh(rings, segments int) Mesh {
	var mesh Mesh
	for ring := 0; ring <= rings; ring++ {
		phi := math.Pi * float64(ring) / float64(rings)
		for segment := 0; segment < segments; segment++ {
			theta := 2 * math.Pi * float64(segment) / float64(segments)
			mesh.Positions = append(mesh.Positions,
				float32(math.Sin(phi)*math.Cos(theta)),
				float32(math.Cos(phi)),
				float32(math.Sin(phi)*math.Sin(theta)),
			)
		}
	}
	for ring := 0; ring < rings; ring++ {
		for segment := 0; segment < segments; segment++ {
			a := uint32(ring*segments + segment)
			b := uint32(ring*segments + (segment+1)%segments)
			c := a + uint32(segments)
			d := b + uint32(segments)
			mesh.Indices = append(mesh.Indices, a, c, b, b, c, d)
		}
	}
	return mesh
}

func TestSimplifyFlatGridKeepsThePlane(t *testing.T) {
	source := gridMesh(16)
	result := Simplify(source, Options{TargetRatio: 0.25})
	if result.OutputTriangles > source.TriangleCount()/4+8 {
		t.Fatalf("kept %d triangles, wanted about %d", result.OutputTriangles, source.TriangleCount()/4)
	}
	stats := MeasureError(source, result.Mesh)
	if stats.MaxDistance > 1e-5 {
		t.Fatalf("a flat grid must stay flat: max distance %.8f", stats.MaxDistance)
	}
	for i := 0; i < result.Mesh.VertexCount(); i++ {
		if math.Abs(float64(result.Mesh.Positions[i*3+1])) > 1e-6 {
			t.Fatalf("vertex %d left the plane at height %v", i, result.Mesh.Positions[i*3+1])
		}
	}
}

func TestSimplifySphereReachesTargetWithBoundedError(t *testing.T) {
	source := sphereMesh(24, 32)
	result := Simplify(source, Options{TargetRatio: 0.25})
	if result.OutputTriangles == 0 {
		t.Fatal("simplification removed every triangle")
	}
	ratio := float64(result.OutputTriangles) / float64(source.TriangleCount())
	if ratio > 0.35 {
		t.Fatalf("kept %.2f of the triangles, wanted about 0.25", ratio)
	}
	stats := MeasureError(source, result.Mesh)
	if stats.MaxFraction > 0.02 {
		t.Fatalf("max error %.5f of the diagonal, wanted at most 0.02", stats.MaxFraction)
	}
	// Every output vertex must stay near the unit sphere.
	for i := 0; i < result.Mesh.VertexCount(); i++ {
		x := float64(result.Mesh.Positions[i*3])
		y := float64(result.Mesh.Positions[i*3+1])
		z := float64(result.Mesh.Positions[i*3+2])
		radius := math.Sqrt(x*x + y*y + z*z)
		if radius < 0.9 || radius > 1.1 {
			t.Fatalf("vertex %d sits at radius %.4f", i, radius)
		}
	}
}

func TestSimplifyProducesValidTopology(t *testing.T) {
	source := sphereMesh(16, 24)
	result := Simplify(source, Options{TargetRatio: 0.3})
	vertexCount := uint32(result.Mesh.VertexCount())
	for i := 0; i < result.Mesh.TriangleCount(); i++ {
		a := result.Mesh.Indices[i*3]
		b := result.Mesh.Indices[i*3+1]
		c := result.Mesh.Indices[i*3+2]
		if a >= vertexCount || b >= vertexCount || c >= vertexCount {
			t.Fatalf("triangle %d references a missing vertex", i)
		}
		if a == b || b == c || a == c {
			t.Fatalf("triangle %d is degenerate", i)
		}
	}
	if len(result.Sources) != result.Mesh.VertexCount() {
		t.Fatalf("sources hold %d entries for %d vertices", len(result.Sources), result.Mesh.VertexCount())
	}
	for i, source := range result.Sources {
		if source.A < 0 || int(source.A) >= result.InputVertices {
			t.Fatalf("vertex %d names source A %d", i, source.A)
		}
		if source.B < 0 || int(source.B) >= result.InputVertices {
			t.Fatalf("vertex %d names source B %d", i, source.B)
		}
		if source.T < 0 || source.T > 1 {
			t.Fatalf("vertex %d has blend factor %v", i, source.T)
		}
	}
}

func TestSimplifyRespectsMaxError(t *testing.T) {
	source := sphereMesh(20, 28)
	tight := Simplify(source, Options{TargetRatio: 0.05, MaxErrorFraction: 0.001})
	loose := Simplify(source, Options{TargetRatio: 0.05})
	if tight.OutputTriangles <= loose.OutputTriangles {
		t.Fatalf("the error limit kept %d triangles, the free run kept %d", tight.OutputTriangles, loose.OutputTriangles)
	}
}

func TestSimplifyLocksSplitVertices(t *testing.T) {
	// Two triangles that share a position but not a vertex index model an
	// attribute seam. The shared corner must not move.
	mesh := Mesh{
		Positions: []float32{
			0, 0, 0, 1, 0, 0, 0, 0, 1,
			0, 0, 0, 0, 0, 1, -1, 0, 0,
		},
		Indices: []uint32{0, 1, 2, 3, 4, 5},
	}
	result := Simplify(mesh, Options{TargetRatio: 0.5})
	if result.LockedVertices == 0 {
		t.Fatal("expected the duplicated corner positions to lock")
	}
}

func TestMeasureErrorFindsKnownOffset(t *testing.T) {
	source := gridMesh(4)
	shifted := gridMesh(4)
	for i := 1; i < len(shifted.Positions); i += 3 {
		shifted.Positions[i] += 0.25
	}
	stats := MeasureError(source, shifted)
	if math.Abs(stats.MaxDistance-0.25) > 1e-6 || math.Abs(stats.RMSDistance-0.25) > 1e-6 {
		t.Fatalf("measured max %.6f rms %.6f, want 0.25", stats.MaxDistance, stats.RMSDistance)
	}
}
