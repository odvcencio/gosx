package physics

import (
	"math"
	"testing"
)

func TestColliderAABBForSphereAndRotatedBox(t *testing.T) {
	sphere := NewCollider(ColliderConfig{
		Shape:  ShapeSphere,
		Offset: Vec3{X: 2, Y: 3, Z: 4},
		Radius: 1.5,
	})
	if got := sphere.AABB(); !got.Min.Near(Vec3{X: 0.5, Y: 1.5, Z: 2.5}, 1e-12) || !got.Max.Near(Vec3{X: 3.5, Y: 4.5, Z: 5.5}, 1e-12) {
		t.Fatalf("sphere AABB = %+v", got)
	}

	box := NewCollider(ColliderConfig{
		Shape:    ShapeBox,
		Width:    2,
		Height:   4,
		Depth:    2,
		Rotation: QuatFromAxisAngle(Vec3{Z: 1}, math.Pi/2),
	})
	got := box.AABB()
	if !got.Min.Near(Vec3{X: -2, Y: -1, Z: -1}, 1e-12) || !got.Max.Near(Vec3{X: 2, Y: 1, Z: 1}, 1e-12) {
		t.Fatalf("rotated box AABB = %+v", got)
	}
}

func TestBroadPhaseReturnsDeterministicCandidates(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0, BroadPhaseCell: 1})
	plane := world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	a := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: 0}})
	ac := a.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 1})
	b := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: 1.5}})
	bc := b.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 1})
	c := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: 10}})
	cc := c.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 1})

	pairs := world.CandidatePairs()
	if !hasPair(pairs, ac, bc) {
		t.Fatal("expected overlapping spheres to be candidates")
	}
	if hasPair(pairs, ac, cc) {
		t.Fatal("did not expect separated spheres to be candidates")
	}
	if !hasPair(pairs, plane, ac) || !hasPair(pairs, plane, bc) || !hasPair(pairs, plane, cc) {
		t.Fatal("expected infinite plane to pair with dynamic colliders")
	}

	again := world.CandidatePairs()
	if len(pairs) != len(again) {
		t.Fatalf("candidate count changed: %d vs %d", len(pairs), len(again))
	}
	for i := range pairs {
		if pairs[i] != again[i] {
			t.Fatalf("candidate order changed at %d: %+v vs %+v", i, pairs[i], again[i])
		}
	}
}

func hasPair(pairs []ColliderPair, a, b *Collider) bool {
	for _, pair := range pairs {
		if (pair.A == a && pair.B == b) || (pair.A == b && pair.B == a) {
			return true
		}
	}
	return false
}
