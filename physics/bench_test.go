package physics

import (
	"fmt"
	"math"
	"testing"
)

// Benchmark scene sizes. The 50/400/1600/3200 ladder shows the scaling of the
// broadphase and of the swept collision pass.
var benchBodyCounts = []int{50, 400, 1600, 3200}

// benchLateralSpeed keeps every dynamic body in motion. A body that comes to a
// complete stop skips the swept pass, which would hide the cost under test.
const benchLateralSpeed = 0.5

// buildSpheresOnPlane returns a world with one ground plane and count dynamic
// spheres that slide on a grid. Each body keeps a lateral velocity, so the
// swept pass runs on every body every step.
func buildSpheresOnPlane(count int, disableCCD bool) *World {
	world := NewWorld(WorldConfig{
		Gravity:          Vec3{Y: -9.81},
		FixedTimestep:    1.0 / 60.0,
		SolverIterations: 8,
		BroadPhaseCell:   2,
		DisableCCD:       disableCCD,
	})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})

	side := int(math.Ceil(math.Sqrt(float64(count))))
	for i := 0; i < count; i++ {
		x := float64(i%side) * 1.5
		z := float64(i/side) * 1.5
		body := world.AddBody(BodyConfig{
			Mass:     1,
			Position: Vec3{X: x, Y: 0.5, Z: z},
			Velocity: Vec3{X: benchLateralSpeed},
		})
		body.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})
	}
	return world
}

// buildSpheresOnStaticFloor returns a world whose ground is a grid of static
// box colliders instead of one plane. This is the shape of scene geometry that
// makes a full static scan expensive.
func buildSpheresOnStaticFloor(count int, disableCCD bool) *World {
	world := NewWorld(WorldConfig{
		Gravity:          Vec3{Y: -9.81},
		FixedTimestep:    1.0 / 60.0,
		SolverIterations: 8,
		BroadPhaseCell:   2,
		DisableCCD:       disableCCD,
	})

	side := int(math.Ceil(math.Sqrt(float64(count))))
	for x := 0; x < side; x++ {
		for z := 0; z < side; z++ {
			world.AddCollider(ColliderConfig{
				Shape:  ShapeBox,
				Offset: Vec3{X: float64(x) * 1.5, Y: -0.5, Z: float64(z) * 1.5},
				Width:  1.5, Height: 1, Depth: 1.5,
			})
		}
	}
	for i := 0; i < count; i++ {
		x := float64(i%side) * 1.5
		z := float64(i/side) * 1.5
		body := world.AddBody(BodyConfig{
			Mass:     1,
			Position: Vec3{X: x, Y: 0.6, Z: z},
			Velocity: Vec3{X: benchLateralSpeed},
		})
		body.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})
	}
	return world
}

func benchmarkStepFixed(b *testing.B, build func(int, bool) *World, count int, disableCCD bool) {
	world := build(count, disableCCD)
	// Warm the contact cache and settle the stack before measuring.
	for i := 0; i < 30; i++ {
		world.StepFixed()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		world.StepFixed()
	}
}

func BenchmarkStepFixedSpheresOnPlaneCCDOn(b *testing.B) {
	for _, count := range benchBodyCounts {
		b.Run(fmt.Sprintf("n=%d", count), func(b *testing.B) {
			benchmarkStepFixed(b, buildSpheresOnPlane, count, false)
		})
	}
}

func BenchmarkStepFixedSpheresOnPlaneCCDOff(b *testing.B) {
	for _, count := range benchBodyCounts {
		b.Run(fmt.Sprintf("n=%d", count), func(b *testing.B) {
			benchmarkStepFixed(b, buildSpheresOnPlane, count, true)
		})
	}
}

func BenchmarkStepFixedStaticFloorCCDOn(b *testing.B) {
	for _, count := range benchBodyCounts {
		b.Run(fmt.Sprintf("n=%d", count), func(b *testing.B) {
			benchmarkStepFixed(b, buildSpheresOnStaticFloor, count, false)
		})
	}
}

func BenchmarkStepFixedStaticFloorCCDOff(b *testing.B) {
	for _, count := range benchBodyCounts {
		b.Run(fmt.Sprintf("n=%d", count), func(b *testing.B) {
			benchmarkStepFixed(b, buildSpheresOnStaticFloor, count, true)
		})
	}
}

func BenchmarkCandidatePairs(b *testing.B) {
	for _, count := range benchBodyCounts {
		b.Run(fmt.Sprintf("n=%d", count), func(b *testing.B) {
			world := buildSpheresOnPlane(count, true)
			for i := 0; i < 30; i++ {
				world.StepFixed()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = world.broadphase.CandidatePairs(world.colliders)
			}
		})
	}
}

// benchmarkCollide measures one narrowphase pair in isolation. The colliders
// must overlap so the full contact-generation path runs.
func benchmarkCollide(b *testing.B, a, other *Collider) {
	if _, ok := Collide(a, other); !ok {
		b.Fatalf("benchmark pair does not collide: %v vs %v", a.Shape, other.Shape)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Collide(a, other)
	}
}

func BenchmarkNarrowphaseSphereSphere(b *testing.B) {
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeSphere, Radius: 1}),
		NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{X: 1.5}, Radius: 1}))
}

func BenchmarkNarrowphaseSpherePlane(b *testing.B) {
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{Y: 0.75}, Radius: 1}),
		NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}}))
}

func BenchmarkNarrowphaseSphereBox(b *testing.B) {
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{Y: 1.4}, Radius: 0.5}),
		NewCollider(ColliderConfig{Shape: ShapeBox, Width: 2, Height: 2, Depth: 2}))
}

func BenchmarkNarrowphaseBoxPlane(b *testing.B) {
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeBox, Offset: Vec3{Y: 0.4}, Width: 1, Height: 1, Depth: 1}),
		NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}}))
}

func BenchmarkNarrowphaseBoxBoxFace(b *testing.B) {
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeBox, Width: 2, Height: 2, Depth: 2}),
		NewCollider(ColliderConfig{Shape: ShapeBox, Offset: Vec3{Y: 1.8}, Width: 2, Height: 2, Depth: 2}))
}

func BenchmarkNarrowphaseBoxBoxEdge(b *testing.B) {
	rotY := QuatFromAxisAngle(Vec3{Y: 1}, math.Pi/4)
	rotX := QuatFromAxisAngle(Vec3{X: 1}, math.Pi/4)
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1}),
		NewCollider(ColliderConfig{
			Shape: ShapeBox, Offset: Vec3{Y: 1.1}, Rotation: rotX.Mul(rotY),
			Width: 1, Height: 1, Depth: 1,
		}))
}

func BenchmarkNarrowphaseSphereCapsule(b *testing.B) {
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{X: 0.8}, Radius: 0.4}),
		NewCollider(ColliderConfig{Shape: ShapeCapsule, Height: 2, Radius: 0.5}))
}

func BenchmarkNarrowphaseCapsulePlane(b *testing.B) {
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeCapsule, Offset: Vec3{Y: 0.45}, Height: 2, Radius: 0.5}),
		NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}}))
}

func BenchmarkNarrowphaseCapsuleCapsule(b *testing.B) {
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeCapsule, Height: 1, Radius: 0.5}),
		NewCollider(ColliderConfig{Shape: ShapeCapsule, Offset: Vec3{X: 0.8}, Height: 1, Radius: 0.5}))
}

func BenchmarkNarrowphaseCylinderPlane(b *testing.B) {
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeCylinder, Offset: Vec3{Y: 0.9}, Radius: 0.5, Height: 2}),
		NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}}))
}

func BenchmarkNarrowphaseCylinderSphere(b *testing.B) {
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 4}),
		NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{X: 1.2}, Radius: 0.5}))
}

func BenchmarkNarrowphaseCylinderBox(b *testing.B) {
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeBox, Width: 4, Height: 2, Depth: 4}),
		NewCollider(ColliderConfig{Shape: ShapeCylinder, Offset: Vec3{Y: 1.9}, Radius: 0.5, Height: 2}))
}

func BenchmarkNarrowphaseCylinderCylinder(b *testing.B) {
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 4}),
		NewCollider(ColliderConfig{Shape: ShapeCylinder, Offset: Vec3{X: 1.7}, Radius: 1, Height: 4}))
}

func BenchmarkNarrowphaseCylinderCapsule(b *testing.B) {
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 4}),
		NewCollider(ColliderConfig{Shape: ShapeCapsule, Offset: Vec3{X: 1.4}, Radius: 0.5, Height: 2}))
}

func BenchmarkNarrowphaseConePlane(b *testing.B) {
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeCone, Offset: Vec3{Y: 0.9}, Radius: 1, Height: 2}),
		NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}}))
}

func BenchmarkNarrowphaseConeBox(b *testing.B) {
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeBox, Width: 4, Height: 2, Depth: 4}),
		NewCollider(ColliderConfig{
			Shape: ShapeCone, Offset: Vec3{Y: 1.95}, Radius: 1, Height: 2,
			Rotation: QuatFromAxisAngle(Vec3{X: 1}, math.Pi),
		}))
}

func BenchmarkNarrowphaseHullPlane(b *testing.B) {
	benchmarkCollide(b,
		NewCollider(ColliderConfig{
			Shape: ShapeConvexHull, Offset: Vec3{Y: 0.4},
			Vertices: benchBoxVertices(Vec3{X: 0.5, Y: 0.5, Z: 0.5}),
		}),
		NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}}))
}

func BenchmarkNarrowphaseHullHull(b *testing.B) {
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeConvexHull, Vertices: benchBoxVertices(Vec3{X: 0.5, Y: 0.5, Z: 0.5})}),
		NewCollider(ColliderConfig{
			Shape: ShapeConvexHull, Offset: Vec3{Y: 0.9},
			Vertices: benchBoxVertices(Vec3{X: 0.5, Y: 0.5, Z: 0.5}),
		}))
}

// benchmarkCollideAll measures a pair that can produce several manifolds.
func benchmarkCollideAll(b *testing.B, a, other *Collider) {
	if len(CollideAll(a, other, nil)) == 0 {
		b.Fatalf("benchmark pair does not collide: %v vs %v", a.Shape, other.Shape)
	}
	out := make([]ContactManifold, 0, meshMaxManifolds)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out = CollideAll(a, other, out[:0])
	}
}

func BenchmarkNarrowphaseMeshSphere(b *testing.B) {
	vertices, indices := benchGridMesh(16, 32)
	benchmarkCollideAll(b,
		NewCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices}),
		NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{Y: 0.4}, Radius: 0.5}))
}

func BenchmarkNarrowphaseMeshBox(b *testing.B) {
	vertices, indices := benchGridMesh(16, 32)
	benchmarkCollideAll(b,
		NewCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices}),
		NewCollider(ColliderConfig{Shape: ShapeBox, Offset: Vec3{Y: 0.4}, Width: 1, Height: 1, Depth: 1}))
}

func BenchmarkNarrowphaseMeshPlane(b *testing.B) {
	vertices, indices := benchGridMesh(16, 32)
	benchmarkCollide(b,
		NewCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices, Offset: Vec3{Y: -0.1}}),
		NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}}))
}

func BenchmarkTriangleMeshBuild(b *testing.B) {
	vertices, indices := benchGridMesh(64, 128)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if newTriangleMesh(vertices, indices) == nil {
			b.Fatal("mesh build failed")
		}
	}
}

func BenchmarkRaycastMesh8kTriangles(b *testing.B) {
	vertices, indices := benchGridMesh(64, 128)
	mesh := NewCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices})
	if len(mesh.mesh.tris) < 8000 {
		b.Fatalf("mesh has %d triangles, want at least 8000", len(mesh.mesh.tris))
	}
	ray := Ray{Origin: Vec3{X: 3, Y: 10, Z: -7}, Direction: Vec3{Y: -1}}
	if _, ok := mesh.Raycast(ray, 0); !ok {
		b.Fatal("benchmark ray misses the mesh")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mesh.Raycast(ray, 0)
	}
}

func benchBoxVertices(half Vec3) []Vec3 {
	return []Vec3{
		{X: -half.X, Y: -half.Y, Z: -half.Z},
		{X: half.X, Y: -half.Y, Z: -half.Z},
		{X: -half.X, Y: half.Y, Z: -half.Z},
		{X: half.X, Y: half.Y, Z: -half.Z},
		{X: -half.X, Y: -half.Y, Z: half.Z},
		{X: half.X, Y: -half.Y, Z: half.Z},
		{X: -half.X, Y: half.Y, Z: half.Z},
		{X: half.X, Y: half.Y, Z: half.Z},
	}
}

func benchGridMesh(cells int, size float64) ([]Vec3, []int) {
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

func BenchmarkRaycast10kColliders(b *testing.B) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0})
	for i := 0; i < 10000; i++ {
		world.AddCollider(ColliderConfig{
			Shape:  ShapeBox,
			Offset: Vec3{X: 10 + float64(i%100), Y: float64(i / 100), Z: 0},
			Width:  0.5, Height: 0.5, Depth: 0.5,
		})
	}
	world.AddCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{X: 2}, Radius: 0.25})

	ray := Ray{Origin: Vec3{}, Direction: Vec3{X: 1}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = world.Raycast(ray, 0)
	}
}
