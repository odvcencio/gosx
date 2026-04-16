package physics

import (
	"math"
	"testing"
)

func TestCollideSphereSphere(t *testing.T) {
	a := NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{X: 0}, Radius: 1})
	b := NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{X: 1.5}, Radius: 1})

	contact, ok := Collide(a, b)
	if !ok {
		t.Fatal("expected sphere-sphere contact")
	}
	if !contact.Normal.Near(Vec3{X: 1}, 1e-12) {
		t.Fatalf("normal = %+v", contact.Normal)
	}
	if contact.PointCount != 1 {
		t.Fatalf("PointCount = %d", contact.PointCount)
	}
	if got := contact.Points[0].Penetration; got < 0.499 || got > 0.501 {
		t.Fatalf("penetration = %v", got)
	}
}

func TestCollideSpherePlane(t *testing.T) {
	sphere := NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{Y: 0.75}, Radius: 1})
	plane := NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})

	contact, ok := Collide(sphere, plane)
	if !ok {
		t.Fatal("expected sphere-plane contact")
	}
	if !contact.Normal.Near(Vec3{Y: -1}, 1e-12) {
		t.Fatalf("normal = %+v", contact.Normal)
	}
	if got := contact.Points[0].Penetration; got < 0.249 || got > 0.251 {
		t.Fatalf("penetration = %v", got)
	}
}

func TestCollideSphereBox(t *testing.T) {
	sphere := NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{Y: 1.4}, Radius: 0.5})
	box := NewCollider(ColliderConfig{Shape: ShapeBox, Width: 2, Height: 2, Depth: 2})

	contact, ok := Collide(sphere, box)
	if !ok {
		t.Fatal("expected sphere-box contact")
	}
	if !contact.Normal.Near(Vec3{Y: -1}, 1e-12) {
		t.Fatalf("normal = %+v", contact.Normal)
	}
	if got := contact.Points[0].Penetration; got < 0.099 || got > 0.101 {
		t.Fatalf("penetration = %v", got)
	}
}

func TestCollideBoxBoxAxisAlignedFaceFace(t *testing.T) {
	// Two unit cubes. B sits 1.8 above A, so they overlap by 0.2 on Y.
	a := NewCollider(ColliderConfig{Shape: ShapeBox, Width: 2, Height: 2, Depth: 2})
	b := NewCollider(ColliderConfig{Shape: ShapeBox, Offset: Vec3{Y: 1.8}, Width: 2, Height: 2, Depth: 2})

	contact, ok := Collide(a, b)
	if !ok {
		t.Fatal("expected box-box contact")
	}
	if !contact.Normal.Near(Vec3{Y: 1}, 1e-9) {
		t.Fatalf("normal = %+v (expected +Y)", contact.Normal)
	}
	if contact.PointCount != 4 {
		t.Fatalf("PointCount = %d (expected 4 face-face contacts)", contact.PointCount)
	}
	for i := 0; i < contact.PointCount; i++ {
		if got := contact.Points[i].Penetration; got < 0.199 || got > 0.201 {
			t.Fatalf("point[%d] penetration = %v (expected 0.2)", i, got)
		}
	}
}

func TestCollideBoxBoxSeparatedReturnsFalse(t *testing.T) {
	a := NewCollider(ColliderConfig{Shape: ShapeBox, Width: 2, Height: 2, Depth: 2})
	b := NewCollider(ColliderConfig{Shape: ShapeBox, Offset: Vec3{Y: 2.1}, Width: 2, Height: 2, Depth: 2})

	if _, ok := Collide(a, b); ok {
		t.Fatal("expected no contact for separated boxes")
	}
}

func TestCollideBoxBoxRotatedBoxOnBox(t *testing.T) {
	// Ground box 4x4x4 at origin. Small box rotated 45° around Y, dropped so the
	// corner penetrates the top face.
	ground := NewCollider(ColliderConfig{Shape: ShapeBox, Width: 4, Height: 4, Depth: 4})
	rotation := QuatFromAxisAngle(Vec3{Y: 1}, math.Pi/4)
	top := NewCollider(ColliderConfig{
		Shape:    ShapeBox,
		Offset:   Vec3{Y: 2 + 0.5 - 0.1}, // ground top at y=2, box half-height 0.5, overlap 0.1
		Rotation: rotation,
		Width:    1, Height: 1, Depth: 1,
	})

	contact, ok := Collide(ground, top)
	if !ok {
		t.Fatal("expected contact with rotated top box")
	}
	if !contact.Normal.Near(Vec3{Y: 1}, 1e-6) {
		t.Fatalf("normal = %+v (expected +Y)", contact.Normal)
	}
	if contact.PointCount < 1 {
		t.Fatalf("expected at least one contact point")
	}
	for i := 0; i < contact.PointCount; i++ {
		pt := contact.Points[i].Point
		if math.Abs(pt.Y-2.0) > 0.15 {
			t.Fatalf("point[%d].Y = %v (expected near ground top y=2)", i, pt.Y)
		}
		if got := contact.Points[i].Penetration; got < 0 || got > 0.11 {
			t.Fatalf("point[%d].penetration = %v", i, got)
		}
	}
}

func TestCollideSphereCapsule(t *testing.T) {
	// Capsule along Y: axis from y=-1 to y=1, radius 0.5. Sphere at (0.8, 0, 0)
	// with radius 0.4 just brushes the capsule's lateral surface.
	capsule := NewCollider(ColliderConfig{Shape: ShapeCapsule, Height: 2, Radius: 0.5})
	sphere := NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{X: 0.8}, Radius: 0.4})

	contact, ok := Collide(sphere, capsule)
	if !ok {
		t.Fatal("expected sphere-capsule contact")
	}
	if contact.Normal.X >= 0 {
		t.Fatalf("normal should point sphere -> capsule (negative X), got %+v", contact.Normal)
	}
	if got := contact.Points[0].Penetration; got < 0.09 || got > 0.11 {
		t.Fatalf("penetration = %v (expected ~0.1)", got)
	}
}

func TestCollideSphereCapsuleEndcap(t *testing.T) {
	// Sphere above the capsule's top hemisphere.
	capsule := NewCollider(ColliderConfig{Shape: ShapeCapsule, Height: 2, Radius: 0.5})
	sphere := NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{Y: 1.8}, Radius: 0.4})

	contact, ok := Collide(sphere, capsule)
	if !ok {
		t.Fatal("expected sphere-capsule end-cap contact")
	}
	// Closest segment point is (0,1,0); sphere center at (0,1.8,0); overlap 0.1.
	if got := contact.Points[0].Penetration; got < 0.09 || got > 0.11 {
		t.Fatalf("endcap penetration = %v", got)
	}
}

func TestCollideCapsulePlaneBothEnds(t *testing.T) {
	// Horizontal capsule (rotated around Z by 90°) resting just above a +Y plane.
	rot := QuatFromAxisAngle(Vec3{Z: 1}, 3.14159265358979/2)
	capsule := NewCollider(ColliderConfig{
		Shape: ShapeCapsule, Height: 2, Radius: 0.5,
		Offset: Vec3{Y: 0.45}, Rotation: rot,
	})
	plane := NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})

	contact, ok := Collide(capsule, plane)
	if !ok {
		t.Fatal("expected capsule-plane contact")
	}
	if contact.PointCount != 2 {
		t.Fatalf("expected 2 contact points (both caps), got %d", contact.PointCount)
	}
	for i := 0; i < contact.PointCount; i++ {
		if got := contact.Points[i].Penetration; got < 0.04 || got > 0.06 {
			t.Fatalf("cap %d penetration = %v (expected ~0.05)", i, got)
		}
	}
}

func TestCollideCapsuleCapsuleParallel(t *testing.T) {
	// Two vertical capsules 0.8 units apart; radii sum 1.0 → 0.2 overlap.
	a := NewCollider(ColliderConfig{Shape: ShapeCapsule, Height: 1, Radius: 0.5})
	b := NewCollider(ColliderConfig{
		Shape: ShapeCapsule, Height: 1, Radius: 0.5,
		Offset: Vec3{X: 0.8},
	})

	contact, ok := Collide(a, b)
	if !ok {
		t.Fatal("expected capsule-capsule contact")
	}
	if got := contact.Points[0].Penetration; got < 0.19 || got > 0.21 {
		t.Fatalf("parallel penetration = %v", got)
	}
}

func TestCollideBoxBoxEdgeEdge(t *testing.T) {
	// Two unit cubes rotated so their edges meet obliquely. A is axis-aligned;
	// B is rotated 45° around Y then 45° around X and positioned so a near-edge
	// on B's corner overlaps A's top face diagonally.
	a := NewCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})
	rotY := QuatFromAxisAngle(Vec3{Y: 1}, math.Pi/4)
	rotX := QuatFromAxisAngle(Vec3{X: 1}, math.Pi/4)
	b := NewCollider(ColliderConfig{
		Shape:    ShapeBox,
		Offset:   Vec3{Y: 1.1},
		Rotation: rotX.Mul(rotY),
		Width:    1, Height: 1, Depth: 1,
	})
	// Without penetration the separation along Y is 0.5+√3/2 ≈ 1.366, so
	// placing at Y=1.1 gives a small overlap.
	contact, ok := Collide(a, b)
	if !ok {
		t.Fatal("expected oblique box-box contact")
	}
	if contact.PointCount < 1 {
		t.Fatalf("expected at least one contact point")
	}
	// Normal should have a positive Y component (B is above A).
	if contact.Normal.Y <= 0 {
		t.Fatalf("normal.Y = %v (expected > 0)", contact.Normal.Y)
	}
}
