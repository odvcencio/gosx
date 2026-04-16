package physics

import (
	"math"
	"testing"
)

func TestVec3Operations(t *testing.T) {
	a := Vec3{X: 1, Y: 2, Z: 3}
	b := Vec3{X: -2, Y: 4, Z: 0.5}

	if got := a.Add(b); !got.Near(Vec3{X: -1, Y: 6, Z: 3.5}, 1e-12) {
		t.Fatalf("Add() = %+v", got)
	}
	if got := a.Sub(b); !got.Near(Vec3{X: 3, Y: -2, Z: 2.5}, 1e-12) {
		t.Fatalf("Sub() = %+v", got)
	}
	if got := a.Dot(b); math.Abs(got-7.5) > 1e-12 {
		t.Fatalf("Dot() = %v", got)
	}
	if got := a.Cross(b); !got.Near(Vec3{X: -11, Y: -6.5, Z: 8}, 1e-12) {
		t.Fatalf("Cross() = %+v", got)
	}
	if got := (Vec3{X: 3, Y: 4}).Normalize(); !got.Near(Vec3{X: 0.6, Y: 0.8}, 1e-12) {
		t.Fatalf("Normalize() = %+v", got)
	}
}

func TestQuatRotateAndSlerp(t *testing.T) {
	rotation := QuatFromAxisAngle(Vec3{Y: 1}, math.Pi/2)
	if got := rotation.Rotate(Vec3{X: 1}); !got.Near(Vec3{Z: -1}, 1e-12) {
		t.Fatalf("Rotate() = %+v", got)
	}

	half := IdentityQuat().Slerp(rotation, 0.5)
	got := half.Rotate(Vec3{X: 1})
	want := Vec3{X: math.Sqrt(0.5), Z: -math.Sqrt(0.5)}
	if !got.Near(want, 1e-12) {
		t.Fatalf("Slerp halfway rotated vector = %+v, want %+v", got, want)
	}
}

func TestAABBOperations(t *testing.T) {
	a := AABBFromCenterHalfExtents(Vec3{X: 1}, Vec3{X: 2, Y: 1, Z: 1})
	b := NewAABB(Vec3{X: 2, Y: -0.5, Z: -0.5}, Vec3{X: 5, Y: 0.5, Z: 0.5})
	c := NewAABB(Vec3{X: 4.1}, Vec3{X: 6, Y: 1, Z: 1})

	if !a.Overlaps(b) {
		t.Fatal("expected overlapping AABBs")
	}
	if a.Overlaps(c) {
		t.Fatal("expected separated AABBs")
	}
	if !a.Contains(Vec3{X: -1, Y: 1, Z: 1}) {
		t.Fatal("expected max corner to be contained")
	}
	union := a.Union(c)
	if !union.Min.Near(Vec3{X: -1, Y: -1, Z: -1}, 1e-12) || !union.Max.Near(Vec3{X: 6, Y: 1, Z: 1}, 1e-12) {
		t.Fatalf("Union() = %+v", union)
	}
}
