package physics

import (
	"math"
	"testing"
)

// buildFlatMeshFloor returns a square grid mesh in the XZ plane at y = 0. Every
// interior edge is shared by two coplanar triangles, which is exactly the case
// the internal edge filter must cover.
func buildFlatMeshFloor(cells int, size float64) ([]Vec3, []int) {
	step := size / float64(cells)
	start := -size / 2
	vertices := make([]Vec3, 0, (cells+1)*(cells+1))
	for iz := 0; iz <= cells; iz++ {
		for ix := 0; ix <= cells; ix++ {
			vertices = append(vertices, Vec3{X: start + float64(ix)*step, Z: start + float64(iz)*step})
		}
	}
	indices := make([]int, 0, cells*cells*6)
	stride := cells + 1
	for iz := 0; iz < cells; iz++ {
		for ix := 0; ix < cells; ix++ {
			a := iz*stride + ix
			b := a + 1
			c := a + stride
			d := c + 1
			indices = append(indices, a, c, b, b, c, d)
		}
	}
	return vertices, indices
}

// TestFlatMeshMarksEveryInteriorEdgeAsInternal checks the build-time pass. A
// flat floor has no convex ridge anywhere, so every shared edge must carry the
// filter mark and every boundary edge must not.
func TestFlatMeshMarksEveryInteriorEdgeAsInternal(t *testing.T) {
	vertices, indices := buildFlatMeshFloor(4, 8)
	mesh := newTriangleMesh(vertices, indices)
	if mesh == nil {
		t.Fatal("mesh build failed")
	}

	shared := 0
	boundary := 0
	for _, tri := range mesh.tris {
		for edge := 0; edge < 3; edge++ {
			if tri.edgeFiltered&(1<<uint(edge)) != 0 {
				shared++
			} else {
				boundary++
			}
		}
	}
	// A 4x4 grid has 32 triangles, 96 directed edges. 16 of them lie on the
	// outer boundary, so 80 belong to a shared, coplanar join.
	if shared != 80 {
		t.Fatalf("filtered edge count = %d, want 80", shared)
	}
	if boundary != 16 {
		t.Fatalf("unfiltered edge count = %d, want 16", boundary)
	}
}

// TestConvexRidgeKeepsItsEdgeContacts checks the negative case. A tent roof
// bulges outward at its ridge, so that edge is a real surface feature and the
// filter must leave it alone.
func TestConvexRidgeKeepsItsEdgeContacts(t *testing.T) {
	// Two slopes meeting at a ridge along Z at y = 1, both facing upward.
	vertices := []Vec3{
		{X: -1, Y: 0, Z: -1}, {X: -1, Y: 0, Z: 1},
		{X: 0, Y: 1, Z: -1}, {X: 0, Y: 1, Z: 1},
		{X: 1, Y: 0, Z: -1}, {X: 1, Y: 0, Z: 1},
	}
	indices := []int{
		0, 1, 2, 2, 1, 3, // left slope
		2, 3, 4, 4, 3, 5, // right slope
	}
	mesh := newTriangleMesh(vertices, indices)
	if mesh == nil {
		t.Fatal("mesh build failed")
	}

	filtered := 0
	for _, tri := range mesh.tris {
		for edge := 0; edge < 3; edge++ {
			if tri.edgeFiltered&(1<<uint(edge)) != 0 {
				filtered++
			}
		}
	}
	// Each slope has one internal edge of its own two triangles, which is flat
	// and therefore filtered. The ridge join is convex and must stay open, so
	// exactly four directed edges carry the mark.
	if filtered != 4 {
		t.Fatalf("filtered edge count = %d, want 4; a convex ridge must keep its edge contacts", filtered)
	}
}

// TestSphereSlidingAcrossMeshEdgesKeepsItsHeight is the behavioural test. A ball
// sliding along a flat mesh floor must never receive a sideways normal when it
// crosses a triangle boundary.
//
// Without the filter the ball is thrown upward and its forward speed drops each
// time it crosses an edge. The test measures both.
func TestSphereSlidingAcrossMeshEdgesKeepsItsHeight(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 240.0, SolverIterations: 12})
	vertices, indices := buildFlatMeshFloor(16, 32)
	world.AddCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices})

	ball := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: -8, Y: 0.5, Z: 0.3}, Velocity: Vec3{X: 6}})
	ball.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})

	// Let the ball settle onto the floor before measuring.
	for i := 0; i < 60; i++ {
		world.StepFixed()
	}

	minY, maxY := math.Inf(1), math.Inf(-1)
	minSpeed := math.Inf(1)
	for i := 0; i < 500; i++ {
		world.StepFixed()
		minY = math.Min(minY, ball.Position.Y)
		maxY = math.Max(maxY, ball.Position.Y)
		minSpeed = math.Min(minSpeed, ball.Velocity.X)
	}

	if ball.Position.X < 0 {
		t.Fatalf("the ball did not cross the mesh, x = %v", ball.Position.X)
	}
	if maxY-minY > 0.01 {
		t.Fatalf("the ball hopped %v while crossing triangle edges; the internal edge filter is not working",
			maxY-minY)
	}
	if minSpeed < 5.9 {
		t.Fatalf("edge contacts braked the ball to %v m/s, want no loss from a frictionless floor", minSpeed)
	}
	if math.Abs(ball.Velocity.Z) > 0.01 {
		t.Fatalf("edge normals pushed the ball sideways at %v m/s", ball.Velocity.Z)
	}
}

// TestBoxSlidingAcrossMeshEdgesStaysFlat covers the same case for a shape that
// runs through GJK instead of the analytic sphere path.
func TestBoxSlidingAcrossMeshEdgesStaysFlat(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 240.0, SolverIterations: 12})
	vertices, indices := buildFlatMeshFloor(16, 32)
	world.AddCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices})

	box := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: -8, Y: 0.5, Z: 0.3}, Velocity: Vec3{X: 5}})
	box.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})

	for i := 0; i < 60; i++ {
		world.StepFixed()
	}

	maxTilt := 0.0
	minY, maxY := math.Inf(1), math.Inf(-1)
	for i := 0; i < 500; i++ {
		world.StepFixed()
		minY = math.Min(minY, box.Position.Y)
		maxY = math.Max(maxY, box.Position.Y)
		tilt := math.Hypot(math.Hypot(box.Rotation.X, box.Rotation.Y), box.Rotation.Z)
		maxTilt = math.Max(maxTilt, tilt)
	}

	if box.Position.X < 0 {
		t.Fatalf("the box did not cross the mesh, x = %v", box.Position.X)
	}
	if maxY-minY > 0.05 {
		t.Fatalf("the box hopped %v while crossing triangle edges", maxY-minY)
	}
	if maxTilt > 0.05 {
		t.Fatalf("edge normals tipped the box, rotation vector part = %v", maxTilt)
	}
}
