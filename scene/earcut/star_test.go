package earcut

import (
	"math"
	"math/rand"
	"testing"
)

// TestRandomStarPolygons is a fuzz-ish sanity check: triangulate a batch of
// random star-shaped polygons (simple, non-self-intersecting by
// construction, since radius varies but angle strictly increases) and assert
// Deviation stays near zero and Triangulate produces the expected triangle
// count for a simple n-gon (n-2 triangles).
func TestRandomStarPolygons(t *testing.T) {
	const epsilon = 1e-9
	rng := rand.New(rand.NewSource(20260701))

	for trial := 0; trial < 200; trial++ {
		n := 3 + rng.Intn(30) // 3..32 points
		vertices := starPolygon(rng, n)

		indices := Triangulate(vertices, nil, 2)

		wantTriangles := n - 2
		if gotTriangles := len(indices) / 3; gotTriangles != wantTriangles {
			t.Fatalf("trial %d (n=%d): got %d triangles, want %d\nvertices=%v",
				trial, n, gotTriangles, wantTriangles, vertices)
		}

		dev := Deviation(vertices, nil, 2, indices)
		if dev > epsilon {
			t.Fatalf("trial %d (n=%d): deviation %v exceeds epsilon %v\nvertices=%v",
				trial, n, dev, epsilon, vertices)
		}
	}
}

// starPolygon generates a simple (non-self-intersecting) star-shaped polygon
// with n vertices: angles are strictly increasing around the circle and
// radii vary randomly between an inner and outer bound, so consecutive
// points never cross — the polygon is "star-shaped" with respect to the
// origin, which is enough to guarantee simplicity.
func starPolygon(rng *rand.Rand, n int) []float64 {
	const innerR, outerR = 10.0, 100.0

	vertices := make([]float64, 0, n*2)
	for i := 0; i < n; i++ {
		angle := 2 * math.Pi * float64(i) / float64(n)
		// jitter the angle within its slice so points stay strictly ordered
		angle += (rng.Float64() - 0.5) * (math.Pi / float64(n)) * 0.8
		r := innerR + rng.Float64()*(outerR-innerR)
		vertices = append(vertices, r*math.Cos(angle), r*math.Sin(angle))
	}
	return vertices
}
