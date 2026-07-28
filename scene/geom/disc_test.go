package geom

import (
	"math"
	"testing"
)

// TestCircleVertexAndTriangleCounts pins the counts a memory report reads.
func TestCircleVertexAndTriangleCounts(t *testing.T) {
	for _, segments := range []int{0, 3, 8, 32, 64, 9999} {
		mesh := Circle(1, segments, 0, 0, AllAttributes)
		if mesh == nil {
			t.Fatalf("segments %d: nil mesh", segments)
		}
		want := CircleVertexCount(segments)
		if got := mesh.VertexCount(); got != want {
			t.Fatalf("segments %d: %d vertices, want %d", segments, got, want)
		}
		if got, expect := mesh.TriangleCount(), want-2; got != expect {
			t.Fatalf("segments %d: %d triangles, want %d", segments, got, expect)
		}
		assertFiniteUnitNormals(t, "circle", mesh)
		assertWindingMatchesNormals(t, "circle", mesh, 0)
	}
}

// TestCircleLiesFlatAndFacesUp proves the disc sits in the XZ plane with a +Y
// normal, which is what PlaneGeometry and PolygonGeometry already do. A disc
// built in the XY plane would need a rotation nobody expects.
func TestCircleLiesFlatAndFacesUp(t *testing.T) {
	const radius = 1.75
	mesh := Circle(radius, 24, 0, 0, AllAttributes)
	for i := 0; i+3 <= len(mesh.Positions); i += 3 {
		if y := mesh.Positions[i+1]; y != 0 {
			t.Fatalf("vertex %d sits at y=%v, want the XZ plane", i/3, y)
		}
		if mesh.Normals[i] != 0 || mesh.Normals[i+1] != 1 || mesh.Normals[i+2] != 0 {
			t.Fatalf("vertex %d normal is not +Y", i/3)
		}
	}
	for triangle := 0; triangle < mesh.TriangleCount(); triangle++ {
		p0, p1, p2 := meshTriangle(mesh, triangle)
		if got := triangleNormal(p0, p1, p2); math.Abs(got.Y-1) > 1e-9 {
			t.Fatalf("triangle %d has the geometric normal %v, want +Y", triangle, got)
		}
	}
}

// TestCircleBoundsMatchTheRadius proves the disc fills its radius and never
// passes it.
func TestCircleBoundsMatchTheRadius(t *testing.T) {
	const radius = 3.0
	mesh := Circle(radius, 64, 0, 0, AllAttributes)
	widest := 0.0
	for i := 0; i+3 <= len(mesh.Positions); i += 3 {
		x, z := mesh.Positions[i], mesh.Positions[i+2]
		distance := math.Hypot(x, z)
		if distance > radius+1e-9 {
			t.Fatalf("vertex %d reaches %v, past the radius %v", i/3, distance, radius)
		}
		widest = math.Max(widest, distance)
	}
	if math.Abs(widest-radius) > 1e-9 {
		t.Fatalf("the widest vertex reaches %v, want the radius %v", widest, radius)
	}
	lo, hi := meshBounds(mesh)
	if math.Abs(lo.X+radius) > 1e-9 || math.Abs(hi.X-radius) > 1e-9 {
		t.Fatalf("x bounds [%v, %v], want [%v, %v]", lo.X, hi.X, -radius, radius)
	}
	if lo.Y != 0 || hi.Y != 0 {
		t.Fatalf("y bounds [%v, %v], want a flat disc", lo.Y, hi.Y)
	}
}

// TestCircleSliceSweepsTheAuthoredAngle proves thetaStart and thetaLength do
// something. A generator that ignored them would still pass a count test.
func TestCircleSliceSweepsTheAuthoredAngle(t *testing.T) {
	quarter := Circle(1, 16, 0, math.Pi/2, AllAttributes)
	for i := 3; i+3 <= len(quarter.Positions); i += 3 {
		x, z := quarter.Positions[i], quarter.Positions[i+2]
		if x < -1e-9 || z < -1e-9 {
			t.Fatalf("a quarter slice from angle 0 reached (%v, %v), outside the first quadrant", x, z)
		}
	}
	// A sweep at or below zero means a whole disc.
	whole := Circle(1, 16, 0, 0, PositionsOnly)
	full := Circle(1, 16, 0, 2*math.Pi, PositionsOnly)
	if len(whole.Positions) != len(full.Positions) {
		t.Fatal("a zero sweep did not fall back to a full turn")
	}
	for i := range whole.Positions {
		if math.Abs(whole.Positions[i]-full.Positions[i]) > 1e-12 {
			t.Fatalf("position %d differs between a zero sweep and a full turn", i)
		}
	}
}

// TestRingVertexAndTriangleCounts pins the counts a memory report reads.
func TestRingVertexAndTriangleCounts(t *testing.T) {
	for _, theta := range []int{0, 3, 32} {
		for _, phi := range []int{0, 1, 4} {
			mesh := Ring(0.5, 1, theta, phi, 0, 0, AllAttributes)
			if mesh == nil {
				t.Fatalf("theta %d phi %d: nil mesh", theta, phi)
			}
			if got, want := mesh.VertexCount(), RingVertexCount(theta, phi); got != want {
				t.Fatalf("theta %d phi %d: %d vertices, want %d", theta, phi, got, want)
			}
			expect := ClampInt(theta, 32, 3, 512) * ClampInt(phi, 1, 1, 128) * 2
			if got := mesh.TriangleCount(); got != expect {
				t.Fatalf("theta %d phi %d: %d triangles, want %d", theta, phi, got, expect)
			}
			assertFiniteUnitNormals(t, "ring", mesh)
			assertWindingMatchesNormals(t, "ring", mesh, 0)
		}
	}
}

// TestRingBoundsHoldTheBand proves the band starts at the inner radius and stops
// at the outer one. A ring that filled its hole would still pass a count test.
func TestRingBoundsHoldTheBand(t *testing.T) {
	const (
		inner = 0.75
		outer = 2.0
	)
	mesh := Ring(inner, outer, 48, 3, 0, 0, AllAttributes)
	closest, widest := math.Inf(1), 0.0
	for i := 0; i+3 <= len(mesh.Positions); i += 3 {
		if y := mesh.Positions[i+1]; y != 0 {
			t.Fatalf("vertex %d sits at y=%v, want the XZ plane", i/3, y)
		}
		distance := math.Hypot(mesh.Positions[i], mesh.Positions[i+2])
		if distance < inner-1e-9 || distance > outer+1e-9 {
			t.Fatalf("vertex %d sits %v from the axis, outside [%v, %v]", i/3, distance, inner, outer)
		}
		closest = math.Min(closest, distance)
		widest = math.Max(widest, distance)
	}
	if math.Abs(closest-inner) > 1e-9 || math.Abs(widest-outer) > 1e-9 {
		t.Fatalf("the band runs [%v, %v], want [%v, %v]", closest, widest, inner, outer)
	}
}

// TestRingRejectsAnEmptyBand proves a ring with no area refuses instead of
// returning an empty mesh that reads like a successful build.
func TestRingRejectsAnEmptyBand(t *testing.T) {
	for _, pair := range [][2]float64{{1, 1}, {2, 1}, {1, 0}} {
		if got := Ring(pair[0], pair[1], 32, 1, 0, 0, AllAttributes); got != nil {
			t.Fatalf("Ring(%v, %v) produced a mesh, want nil", pair[0], pair[1])
		}
	}
}

// TestRingFacesUp proves the whole band faces +Y, not just the first triangle.
func TestRingFacesUp(t *testing.T) {
	mesh := Ring(0.4, 1, 16, 2, 0.3, 1.2, AllAttributes)
	for triangle := 0; triangle < mesh.TriangleCount(); triangle++ {
		p0, p1, p2 := meshTriangle(mesh, triangle)
		if got := triangleNormal(p0, p1, p2); math.Abs(got.Y-1) > 1e-9 {
			t.Fatalf("triangle %d has the geometric normal %v, want +Y", triangle, got)
		}
	}
}
