package vm

import (
	"math"
	"testing"

	"m31labs.dev/gosx/motion"
)

func triangleArea(a, b, c point3) float64 {
	ux, uy, uz := b.X-a.X, b.Y-a.Y, b.Z-a.Z
	vx, vy, vz := c.X-a.X, c.Y-a.Y, c.Z-a.Z
	cx := uy*vz - uz*vy
	cy := uz*vx - ux*vz
	cz := ux*vy - uy*vx
	return math.Sqrt(cx*cx+cy*cy+cz*cz) / 2
}

func distinctPoints(points []point3) int {
	seen := map[[3]float64]struct{}{}
	for _, point := range points {
		seen[[3]float64{point.X + 0, point.Y + 0, point.Z + 0}] = struct{}{}
	}
	return len(seen)
}

func TestBoxVertexSliceIsDegenerate(t *testing.T) {
	slice := boxVertices(2, 0, 2)[:4]
	if got := distinctPoints(slice); got != 2 {
		t.Fatalf("expected the height-zero slice to collapse to 2 points, got %d", got)
	}
}

func TestPlaneQuadVerticesWalksTheRing(t *testing.T) {
	quad := planeQuadVertices(4, 2)
	if len(quad) != 4 {
		t.Fatalf("expected 4 corners, got %d", len(quad))
	}
	want := [][3]float64{{-2, 0, -1}, {2, 0, -1}, {2, 0, 1}, {-2, 0, 1}}
	for index, expected := range want {
		if math.Abs(quad[index].X-expected[0]) > 1e-9 ||
			math.Abs(quad[index].Y-expected[1]) > 1e-9 ||
			math.Abs(quad[index].Z-expected[2]) > 1e-9 {
			t.Fatalf("corner %d = %+v, want %v", index, quad[index], expected)
		}
	}
}

func TestScenePlaneSurfaceCornersArea(t *testing.T) {
	cases := []struct{ width, depth float64 }{
		{1, 1}, {4, 2}, {0.25, 3.5}, {1.8, 0.72},
	}
	for _, tc := range cases {
		object := sceneObject{
			Kind:   "plane",
			Width:  tc.width,
			Depth:  tc.depth,
			ScaleX: 1,
			ScaleY: 1,
			ScaleZ: 1,
		}
		corners := scenePlaneSurfaceCorners(object, motion.Quat{W: 1}, clipTRS{}, 0)
		if len(corners) != 4 {
			t.Fatalf("%vx%v: expected 4 corners, got %d", tc.width, tc.depth, len(corners))
		}
		if got := distinctPoints(corners); got != 4 {
			t.Fatalf("%vx%v: corners collapsed to %d distinct points", tc.width, tc.depth, got)
		}
		area := triangleArea(corners[0], corners[1], corners[2]) +
			triangleArea(corners[0], corners[2], corners[3])
		if math.Abs(area-tc.width*tc.depth) > 1e-9 {
			t.Fatalf("%vx%v: surface area %v, want %v", tc.width, tc.depth, area, tc.width*tc.depth)
		}
	}
}

func TestPlaneSegmentsDrawASquare(t *testing.T) {
	segments := planeSegments(sceneObject{Kind: "plane", Width: 4, Depth: 2})
	if len(segments) != 4 {
		t.Fatalf("expected 4 edges, got %d", len(segments))
	}
	perimeter := 0.0
	for index, edge := range segments {
		dx, dy, dz := edge[1].X-edge[0].X, edge[1].Y-edge[0].Y, edge[1].Z-edge[0].Z
		length := math.Sqrt(dx*dx + dy*dy + dz*dz)
		if length == 0 {
			t.Fatalf("edge %d has zero length", index)
		}
		perimeter += length
	}
	if math.Abs(perimeter-12) > 1e-9 {
		t.Fatalf("perimeter %v, want 12", perimeter)
	}
}
