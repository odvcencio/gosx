package physics

import (
	"math"
	"math/rand"
	"testing"
)

// terrainMeshConfig builds a cells x cells triangle mesh of rolling ground,
// span metres across, centred on the origin.
func terrainMeshConfig(cells int, span float64, height func(x, z float64) float64) ColliderConfig {
	vertices := make([]Vec3, 0, (cells+1)*(cells+1))
	for iz := 0; iz <= cells; iz++ {
		for ix := 0; ix <= cells; ix++ {
			x := -span/2 + span*float64(ix)/float64(cells)
			z := -span/2 + span*float64(iz)/float64(cells)
			vertices = append(vertices, Vec3{X: x, Y: height(x, z), Z: z})
		}
	}
	indices := make([]int, 0, cells*cells*6)
	row := cells + 1
	for iz := 0; iz < cells; iz++ {
		for ix := 0; ix < cells; ix++ {
			a := iz*row + ix
			b := a + 1
			c := a + row
			d := c + 1
			indices = append(indices, a, c, d, a, d, b)
		}
	}
	return ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices}
}

// queryBenchWorld is a level-sized scene: mesh ground, 150 static boxes, 40
// dynamic props, 4 kinematic platforms and 8 triggers.
func queryBenchWorld(seed int64) *World {
	rng := rand.New(rand.NewSource(seed))
	w := NewWorld(WorldConfig{Gravity: Vec3{Y: -9.81}, FixedTimestep: 1.0 / 60})
	w.AddCollider(terrainMeshConfig(32, 64, func(x, z float64) float64 {
		return 0.4*math.Sin(0.3*x) + 0.3*math.Cos(0.25*z) - 1
	}))
	for i := 0; i < 150; i++ {
		w.AddCollider(ColliderConfig{
			Shape:  ShapeBox,
			Width:  0.5 + rng.Float64()*2,
			Height: 0.5 + rng.Float64()*2,
			Depth:  0.5 + rng.Float64()*2,
			Offset: Vec3{X: (rng.Float64() - 0.5) * 60, Y: rng.Float64() * 2, Z: (rng.Float64() - 0.5) * 60},
		})
	}
	for i := 0; i < 40; i++ {
		body := w.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: (rng.Float64() - 0.5) * 50, Y: 4 + rng.Float64()*4, Z: (rng.Float64() - 0.5) * 50}})
		if i%2 == 0 {
			body.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.4})
		} else {
			body.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 0.8, Height: 0.8, Depth: 0.8})
		}
	}
	for i := 0; i < 4; i++ {
		platform := w.AddBody(BodyConfig{IsKinematic: true, Position: Vec3{X: float64(i)*8 - 12, Y: 1.5, Z: 6}})
		platform.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 3, Height: 0.3, Depth: 3})
	}
	for i := 0; i < 8; i++ {
		w.AddCollider(ColliderConfig{Shape: ShapeBox, IsTrigger: true, Width: 2, Height: 2, Depth: 2, Offset: Vec3{X: float64(i)*6 - 24, Y: 1, Z: -6}})
	}
	w.StepFixed()
	return w
}

func BenchmarkSphereCast(b *testing.B) {
	w := queryBenchWorld(1)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.SphereCast(Vec3{X: -1, Y: 1, Z: 0.3}, 0.4, Vec3{X: 1, Y: -0.2}, 12, QueryFilter{})
	}
}

func BenchmarkCapsuleCastDown(b *testing.B) {
	w := queryBenchWorld(1)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.CapsuleCast(Vec3{X: 3, Y: 3, Z: 3}, IdentityQuat(), 0.35, 1.1, Vec3{Y: -1}, 6, QueryFilter{})
	}
}

func BenchmarkBoxCast(b *testing.B) {
	w := queryBenchWorld(1)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.BoxCast(Vec3{Y: 1}, QuatFromAxisAngle(Vec3{Y: 1}, 0.4), Vec3{X: 0.5, Y: 0.5, Z: 0.5}, Vec3{X: 1}, 15, QueryFilter{})
	}
}

func BenchmarkOverlapCapsule(b *testing.B) {
	w := queryBenchWorld(1)
	var out []*Collider
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		out = w.OverlapCapsule(Vec3{X: 2, Y: 0.5}, IdentityQuat(), 0.5, 1, QueryFilter{}, out[:0])
	}
}

func BenchmarkRaycastFiltered(b *testing.B) {
	w := queryBenchWorld(1)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.RaycastFiltered(Ray{Origin: Vec3{Y: 5}, Direction: Vec3{X: 0.3, Y: -1}}, 30, QueryFilter{})
	}
}
