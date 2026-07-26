package physics

import (
	"math"
	"testing"
)

// gridMesh returns a flat square mesh on the y=0 plane, made of cells*cells
// quads split into two triangles each. The mesh spans [-size/2, size/2] on X
// and Z.
func gridMesh(cells int, size float64) ([]Vec3, []int) {
	step := size / float64(cells)
	start := -size / 2
	vertices := make([]Vec3, 0, (cells+1)*(cells+1))
	for iz := 0; iz <= cells; iz++ {
		for ix := 0; ix <= cells; ix++ {
			vertices = append(vertices, Vec3{
				X: start + float64(ix)*step,
				Z: start + float64(iz)*step,
			})
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

func TestTriangleMeshBuildsBVHOverAllTriangles(t *testing.T) {
	vertices, indices := gridMesh(8, 16)
	mesh := newTriangleMesh(vertices, indices)
	if mesh == nil {
		t.Fatal("newTriangleMesh returned nil for a valid grid")
	}
	if want := len(indices) / 3; len(mesh.tris) != want {
		t.Fatalf("triangle count = %d, want %d", len(mesh.tris), want)
	}
	if len(mesh.nodes) == 0 {
		t.Fatal("BVH has no nodes")
	}

	// Every triangle must sit in exactly one leaf, and the leaf ranges must
	// cover the whole triangle list without overlap.
	covered := make([]int, len(mesh.tris))
	for _, node := range mesh.nodes {
		if !node.leaf() {
			continue
		}
		for i := node.start; i < node.start+node.count; i++ {
			covered[i]++
		}
	}
	for i, count := range covered {
		if count != 1 {
			t.Fatalf("triangle %d appears in %d leaves, want 1", i, count)
		}
	}

	// Every node's bounds must contain the bounds of its triangles.
	for index, node := range mesh.nodes {
		if !node.leaf() {
			continue
		}
		for i := node.start; i < node.start+node.count; i++ {
			bounds := mesh.tris[i].bounds()
			if !node.bounds.Contains(bounds.Min) || !node.bounds.Contains(bounds.Max) {
				t.Fatalf("node %d bounds %+v do not contain triangle %d bounds %+v",
					index, node.bounds, i, bounds)
			}
		}
	}
}

func TestTriangleMeshQueryAABBMatchesBruteForce(t *testing.T) {
	vertices, indices := gridMesh(10, 20)
	mesh := newTriangleMesh(vertices, indices)

	boxes := []AABB{
		NewAABB(Vec3{X: -1, Y: -1, Z: -1}, Vec3{X: 1, Y: 1, Z: 1}),
		NewAABB(Vec3{X: 7, Y: -0.5, Z: 7}, Vec3{X: 12, Y: 0.5, Z: 12}),
		NewAABB(Vec3{X: -30, Y: -30, Z: -30}, Vec3{X: 30, Y: 30, Z: 30}),
		NewAABB(Vec3{X: 100, Y: 0, Z: 100}, Vec3{X: 101, Y: 1, Z: 101}),
	}
	for _, box := range boxes {
		got := mesh.queryAABB(box, nil)
		want := make([]int, 0, len(mesh.tris))
		for i := range mesh.tris {
			if mesh.tris[i].bounds().Overlaps(box) {
				want = append(want, i)
			}
		}
		if len(got) != len(want) {
			t.Fatalf("box %+v: query returned %d triangles, brute force found %d", box, len(got), len(want))
		}
		found := make(map[int]bool, len(got))
		for _, index := range got {
			found[index] = true
		}
		for _, index := range want {
			if !found[index] {
				t.Fatalf("box %+v: query missed triangle %d", box, index)
			}
		}
	}
}

func TestCollideMeshSphereOnFloor(t *testing.T) {
	vertices, indices := gridMesh(4, 8)
	mesh := NewCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices})
	if err := mesh.Err(); err != nil {
		t.Fatalf("mesh collider is invalid: %v", err)
	}
	sphere := NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{Y: 0.4}, Radius: 0.5})

	manifolds := CollideAll(mesh, sphere, nil)
	if len(manifolds) == 0 {
		t.Fatal("expected a mesh-sphere contact")
	}
	best := manifolds[0]
	for _, m := range manifolds {
		if m.Points[0].Penetration > best.Points[0].Penetration {
			best = m
		}
	}
	if best.Normal.Dot(Vec3{Y: 1}) < 0.999 {
		t.Fatalf("normal = %+v, want +Y (mesh toward sphere)", best.Normal)
	}
	if got := best.Points[0].Penetration; math.Abs(got-0.1) > 1e-12 {
		t.Fatalf("penetration = %v, want 0.1", got)
	}
	// The floor is at y=0 and the sphere surface reaches y=-0.1, so the contact
	// midpoint is y=-0.05.
	if got := best.Points[0].Point.Y; math.Abs(got+0.05) > 1e-12 {
		t.Fatalf("contact Y = %v, want -0.05", got)
	}
}

func TestCollideMeshFlipsManifoldWhenMeshIsSecond(t *testing.T) {
	vertices, indices := gridMesh(2, 4)
	mesh := NewCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices})
	sphere := NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{Y: 0.4}, Radius: 0.5})

	forward := CollideAll(mesh, sphere, nil)
	reverse := CollideAll(sphere, mesh, nil)
	if len(forward) == 0 || len(reverse) == 0 {
		t.Fatalf("expected contacts both ways, got %d and %d", len(forward), len(reverse))
	}
	if reverse[0].ColliderA != sphere || reverse[0].ColliderB != mesh {
		t.Fatal("reversed call must report the sphere as A and the mesh as B")
	}
	if !reverse[0].Normal.Near(forward[0].Normal.Neg(), 1e-12) {
		t.Fatalf("reversed normal = %+v, want %+v", reverse[0].Normal, forward[0].Normal.Neg())
	}
}

func TestCollideMeshPlaneKeepsDeepestVertices(t *testing.T) {
	// A mesh tilted so its corners sit at different depths below the plane.
	vertices := []Vec3{
		{X: -1, Y: -0.3, Z: -1},
		{X: 1, Y: -0.2, Z: -1},
		{X: -1, Y: 0.5, Z: 1},
		{X: 1, Y: 0.6, Z: 1},
	}
	indices := []int{0, 2, 1, 1, 2, 3}
	mesh := NewCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices})
	plane := NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})

	contact, ok := Collide(mesh, plane)
	if !ok {
		t.Fatal("expected a mesh-plane contact")
	}
	if !contact.Normal.Near(Vec3{Y: -1}, 1e-12) {
		t.Fatalf("normal = %+v, want -Y", contact.Normal)
	}
	deepest := 0.0
	for i := 0; i < contact.PointCount; i++ {
		deepest = maxFloat(deepest, contact.Points[i].Penetration)
	}
	if math.Abs(deepest-0.3) > 1e-12 {
		t.Fatalf("deepest penetration = %v, want 0.3", deepest)
	}
}

func TestWorldSphereRestsOnMeshFloor(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIter: 8})
	vertices, indices := gridMesh(4, 10)
	world.AddCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices})
	if err := world.Err(); err != nil {
		t.Fatalf("world reports diagnostics: %v", err)
	}

	body := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 2}, Restitution: 0, Friction: 0.5})
	body.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})

	for i := 0; i < 300; i++ {
		world.Step(1.0 / 60.0)
	}

	if body.Position.Y < 0.45 || body.Position.Y > 0.56 {
		t.Fatalf("sphere should rest near y=0.5 on the mesh floor, got y=%v", body.Position.Y)
	}
	if math.Abs(body.Velocity.Y) > 0.25 {
		t.Fatalf("sphere should be near rest, velocity=%+v", body.Velocity)
	}
}

func TestWorldBoxRestsOnMeshFloor(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIter: 10})
	vertices, indices := gridMesh(4, 10)
	world.AddCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices})

	body := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 2}, Restitution: 0, Friction: 0.5})
	body.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})

	for i := 0; i < 400; i++ {
		world.Step(1.0 / 60.0)
	}

	if body.Position.Y < 0.45 || body.Position.Y > 0.58 {
		t.Fatalf("box should rest near y=0.5 on the mesh floor, got y=%v", body.Position.Y)
	}
}

func TestRaycastMeshHitsClosestTriangle(t *testing.T) {
	vertices, indices := gridMesh(8, 16)
	mesh := NewCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices})

	hit, ok := mesh.Raycast(Ray{Origin: Vec3{X: 1, Y: 5, Z: -2}, Direction: Vec3{Y: -1}}, 0)
	if !ok {
		t.Fatal("expected a mesh raycast hit")
	}
	if math.Abs(hit.Distance-5) > 1e-9 {
		t.Fatalf("distance = %v, want 5", hit.Distance)
	}
	if !hit.Point.Near(Vec3{X: 1, Y: 0, Z: -2}, 1e-9) {
		t.Fatalf("point = %+v, want (1,0,-2)", hit.Point)
	}
	if hit.Normal.Dot(Vec3{Y: 1}) < 0.999 {
		t.Fatalf("normal = %+v, want +Y", hit.Normal)
	}

	if _, ok := mesh.Raycast(Ray{Origin: Vec3{X: 100, Y: 5}, Direction: Vec3{Y: -1}}, 0); ok {
		t.Fatal("a ray outside the mesh footprint must miss")
	}
}

func TestRaycastMeshHonoursRotationAndOffset(t *testing.T) {
	vertices, indices := gridMesh(2, 4)
	rotation := QuatFromAxisAngle(Vec3{X: 1}, math.Pi/2)
	mesh := NewCollider(ColliderConfig{
		Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices,
		Offset: Vec3{X: 3}, Rotation: rotation,
	})
	// After rotating 90 degrees about X the floor becomes a wall at x=3 facing
	// -Z, so a ray along +Z from the origin must hit it.
	hit, ok := mesh.Raycast(Ray{Origin: Vec3{X: 3, Y: 0, Z: -4}, Direction: Vec3{Z: 1}}, 0)
	if !ok {
		t.Fatal("expected a hit on the rotated mesh")
	}
	if math.Abs(hit.Distance-4) > 1e-9 {
		t.Fatalf("distance = %v, want 4", hit.Distance)
	}
	if hit.Normal.Dot(Vec3{Z: -1}) < 0.999 {
		t.Fatalf("normal = %+v, want -Z", hit.Normal)
	}
}

func TestConvexHullRaycastNeedsIndices(t *testing.T) {
	withoutIndices := NewCollider(ColliderConfig{
		Shape: ShapeConvexHull, Vertices: boxVertices(Vec3{X: 1, Y: 1, Z: 1}),
	})
	if withoutIndices.RaycastSupported() {
		t.Fatal("a hull with no Indices must report that raycast cannot hit it")
	}
	if _, ok := withoutIndices.Raycast(Ray{Origin: Vec3{X: -5}, Direction: Vec3{X: 1}}, 0); ok {
		t.Fatal("a hull with no triangulated surface must not report a hit")
	}

	withIndices := NewCollider(ColliderConfig{
		Shape:    ShapeConvexHull,
		Vertices: boxVertices(Vec3{X: 1, Y: 1, Z: 1}),
		Indices:  boxTriangleIndices(),
	})
	if !withIndices.RaycastSupported() {
		t.Fatal("a hull with Indices must support raycast")
	}
	hit, ok := withIndices.Raycast(Ray{Origin: Vec3{X: -5}, Direction: Vec3{X: 1}}, 0)
	if !ok {
		t.Fatal("expected a hit on the triangulated hull")
	}
	if math.Abs(hit.Distance-4) > 1e-9 {
		t.Fatalf("distance = %v, want 4", hit.Distance)
	}
	if hit.Normal.Dot(Vec3{X: -1}) < 0.999 {
		t.Fatalf("normal = %+v, want -X", hit.Normal)
	}
}

// boxTriangleIndices triangulates the eight corners that boxVertices returns.
// The corner order is (x, y, z) with x fastest.
func boxTriangleIndices() []int {
	const (
		nnn = 0 // -x -y -z
		pnn = 1 // +x -y -z
		npn = 2 // -x +y -z
		ppn = 3 // +x +y -z
		nnp = 4 // -x -y +z
		pnp = 5 // +x -y +z
		npp = 6 // -x +y +z
		ppp = 7 // +x +y +z
	)
	return []int{
		nnn, npn, pnn, pnn, npn, ppn, // -Z face
		nnp, pnp, npp, npp, pnp, ppp, // +Z face
		nnn, nnp, npn, npn, nnp, npp, // -X face
		pnn, ppn, pnp, pnp, ppn, ppp, // +X face
		nnn, pnn, nnp, nnp, pnn, pnp, // -Y face
		npn, npp, ppn, ppn, npp, ppp, // +Y face
	}
}

func TestClosestPointOnTriangleCoversEveryVoronoiRegion(t *testing.T) {
	a := Vec3{}
	b := Vec3{X: 4}
	c := Vec3{Z: 3}

	cases := []struct {
		name  string
		point Vec3
		want  Vec3
	}{
		{name: "vertex a", point: Vec3{X: -2, Z: -2}, want: a},
		{name: "vertex b", point: Vec3{X: 7, Z: -2}, want: b},
		{name: "vertex c", point: Vec3{X: -2, Z: 6}, want: c},
		{name: "edge ab", point: Vec3{X: 2, Y: 5, Z: -3}, want: Vec3{X: 2}},
		{name: "edge ac", point: Vec3{X: -3, Z: 1.5}, want: Vec3{Z: 1.5}},
		{name: "face interior", point: Vec3{X: 1, Y: 9, Z: 0.5}, want: Vec3{X: 1, Z: 0.5}},
		{name: "face interior below", point: Vec3{X: 1, Y: -9, Z: 0.5}, want: Vec3{X: 1, Z: 0.5}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := closestPointOnTriangle(testCase.point, a, b, c)
			if !got.Near(testCase.want, 1e-9) {
				t.Fatalf("closestPointOnTriangle = %+v, want %+v", got, testCase.want)
			}
		})
	}

	// Edge bc: the point sits beyond the hypotenuse midpoint.
	got := closestPointOnTriangle(Vec3{X: 5, Z: 4}, a, b, c)
	if got.X <= 0 || got.Z <= 0 || got.X >= 4 || got.Z >= 3 {
		t.Fatalf("edge bc closest point = %+v, want a point strictly inside edge bc", got)
	}
	// A point on edge bc must satisfy x/4 + z/3 = 1.
	if math.Abs(got.X/4+got.Z/3-1) > 1e-9 {
		t.Fatalf("edge bc closest point %+v is not on the hypotenuse", got)
	}
}

func TestCollideSphereTriangleMatchesGJK(t *testing.T) {
	// The analytic path replaced GJK for sphere-triangle pairs. Check that the
	// two still agree wherever GJK reports a contact.
	a := Vec3{X: -1, Z: -1}
	b := Vec3{X: 1, Z: -1}
	c := Vec3{Z: 1}
	tri := triangleShape(a, b, c)

	centers := []Vec3{
		{Y: 0.4},
		{X: 0.3, Y: 0.35, Z: 0.1},
		{X: 1.1, Y: 0.2, Z: -1.1},
		{X: -0.9, Y: 0.1, Z: -0.95},
		{Y: -0.45},
		{X: 0.2, Y: 0.49, Z: 0.2},
	}
	const radius = 0.5
	for _, center := range centers {
		sphere := NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: center, Radius: radius})
		shape, _ := newConvexShape(sphere)
		simplex, overlap := gjkOverlap(tri, shape)
		analytic, analyticOK := collideSphereTriangle(center, radius, a, b, c)
		if !overlap {
			if analyticOK && analytic.Depth > 1e-6 {
				t.Fatalf("center %+v: analytic depth %v but GJK found no overlap", center, analytic.Depth)
			}
			continue
		}
		if !analyticOK {
			t.Fatalf("center %+v: GJK found overlap but the analytic path did not", center)
		}
		epa, ok := epaPenetration(tri, shape, simplex)
		if !ok {
			continue
		}
		if math.Abs(epa.Depth-analytic.Depth) > 2e-3 {
			t.Fatalf("center %+v: analytic depth %v, EPA depth %v", center, analytic.Depth, epa.Depth)
		}
		if epa.Normal.Dot(analytic.Normal) < 0.99 {
			t.Fatalf("center %+v: analytic normal %+v, EPA normal %+v", center, analytic.Normal, epa.Normal)
		}
	}
}
