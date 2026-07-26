package physics

import (
	"math"
	"testing"
)

func TestRaycastCapsuleTubeAndCaps(t *testing.T) {
	// Capsule along Y: segment from y=-1 to y=1, radius 0.5.
	capsule := NewCollider(ColliderConfig{Shape: ShapeCapsule, Radius: 0.5, Height: 2})

	hit, ok := capsule.Raycast(Ray{Origin: Vec3{X: -4}, Direction: Vec3{X: 1}}, 0)
	if !ok {
		t.Fatal("expected a hit on the capsule tube")
	}
	if math.Abs(hit.Distance-3.5) > 1e-9 {
		t.Fatalf("tube distance = %v, want 3.5", hit.Distance)
	}
	if hit.Normal.Dot(Vec3{X: -1}) < 0.999 {
		t.Fatalf("tube normal = %+v, want -X", hit.Normal)
	}

	// Straight down the axis hits the top cap at y=1.5.
	hit, ok = capsule.Raycast(Ray{Origin: Vec3{Y: 5}, Direction: Vec3{Y: -1}}, 0)
	if !ok {
		t.Fatal("expected a hit on the top cap")
	}
	if math.Abs(hit.Distance-3.5) > 1e-9 {
		t.Fatalf("cap distance = %v, want 3.5", hit.Distance)
	}
	if hit.Normal.Dot(Vec3{Y: 1}) < 0.999 {
		t.Fatalf("cap normal = %+v, want +Y", hit.Normal)
	}

	// A ray level with the cap centre but outside the radius must miss.
	if _, ok := capsule.Raycast(Ray{Origin: Vec3{X: -4, Y: 0.9}, Direction: Vec3{X: 1}}, 0); !ok {
		t.Fatal("a ray inside the tube radius must hit")
	}
	if _, ok := capsule.Raycast(Ray{Origin: Vec3{X: -4, Y: 1.6}, Direction: Vec3{X: 1}}, 0); ok {
		t.Fatal("a ray above the top cap must miss")
	}
}

func TestRaycastCapsuleFromInsideReportsZeroDistance(t *testing.T) {
	capsule := NewCollider(ColliderConfig{Shape: ShapeCapsule, Radius: 0.5, Height: 2})
	hit, ok := capsule.Raycast(Ray{Origin: Vec3{}, Direction: Vec3{X: 1}}, 0)
	if !ok {
		t.Fatal("a ray starting inside must report a hit")
	}
	if hit.Distance != 0 {
		t.Fatalf("distance = %v, want 0", hit.Distance)
	}
}

func TestRaycastCylinderSideAndCaps(t *testing.T) {
	cylinder := NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 2})

	hit, ok := cylinder.Raycast(Ray{Origin: Vec3{X: -5}, Direction: Vec3{X: 1}}, 0)
	if !ok {
		t.Fatal("expected a hit on the cylinder wall")
	}
	if math.Abs(hit.Distance-4) > 1e-9 {
		t.Fatalf("wall distance = %v, want 4", hit.Distance)
	}
	if hit.Normal.Dot(Vec3{X: -1}) < 0.999 {
		t.Fatalf("wall normal = %+v, want -X", hit.Normal)
	}

	hit, ok = cylinder.Raycast(Ray{Origin: Vec3{X: 0.5, Y: 4}, Direction: Vec3{Y: -1}}, 0)
	if !ok {
		t.Fatal("expected a hit on the top cap")
	}
	if math.Abs(hit.Distance-3) > 1e-9 {
		t.Fatalf("cap distance = %v, want 3", hit.Distance)
	}
	if !hit.Normal.Near(Vec3{Y: 1}, 1e-9) {
		t.Fatalf("cap normal = %+v, want +Y", hit.Normal)
	}

	// Above the cap plane but outside the wall: must miss.
	if _, ok := cylinder.Raycast(Ray{Origin: Vec3{X: -5, Y: 1.2}, Direction: Vec3{X: 1}}, 0); ok {
		t.Fatal("a ray above the cylinder must miss")
	}
	// Rotated cylinder: lay it along X and shoot down the new axis.
	rotated := NewCollider(ColliderConfig{
		Shape: ShapeCylinder, Radius: 1, Height: 2,
		Rotation: QuatFromAxisAngle(Vec3{Z: 1}, math.Pi/2),
	})
	hit, ok = rotated.Raycast(Ray{Origin: Vec3{X: -5}, Direction: Vec3{X: 1}}, 0)
	if !ok {
		t.Fatal("expected a hit on the rotated cylinder cap")
	}
	if math.Abs(hit.Distance-4) > 1e-9 {
		t.Fatalf("rotated cap distance = %v, want 4", hit.Distance)
	}
	if hit.Normal.Dot(Vec3{X: -1}) < 0.999 {
		t.Fatalf("rotated cap normal = %+v, want -X", hit.Normal)
	}
}

func TestRaycastConeApexBaseAndSide(t *testing.T) {
	// Radius 1, height 2: apex at y=1, base disc radius 1 at y=-1.
	cone := NewCollider(ColliderConfig{Shape: ShapeCone, Radius: 1, Height: 2})

	// Straight down the axis hits the apex.
	hit, ok := cone.Raycast(Ray{Origin: Vec3{Y: 5}, Direction: Vec3{Y: -1}}, 0)
	if !ok {
		t.Fatal("expected a hit at the cone apex")
	}
	if math.Abs(hit.Distance-4) > 1e-6 {
		t.Fatalf("apex distance = %v, want 4", hit.Distance)
	}

	// Straight up the axis hits the base disc.
	hit, ok = cone.Raycast(Ray{Origin: Vec3{Y: -5}, Direction: Vec3{Y: 1}}, 0)
	if !ok {
		t.Fatal("expected a hit on the cone base")
	}
	if math.Abs(hit.Distance-4) > 1e-9 {
		t.Fatalf("base distance = %v, want 4", hit.Distance)
	}
	if !hit.Normal.Near(Vec3{Y: -1}, 1e-9) {
		t.Fatalf("base normal = %+v, want -Y", hit.Normal)
	}

	// Level with the base rim: the wall is at radius 1 there.
	hit, ok = cone.Raycast(Ray{Origin: Vec3{X: -5, Y: -1 + 1e-6}, Direction: Vec3{X: 1}}, 0)
	if !ok {
		t.Fatal("expected a hit on the cone wall near the base rim")
	}
	if math.Abs(hit.Distance-4) > 1e-3 {
		t.Fatalf("rim distance = %v, want 4", hit.Distance)
	}

	// Half way up, the cone radius is 0.5, so the wall sits at x=-0.5.
	hit, ok = cone.Raycast(Ray{Origin: Vec3{X: -5, Y: 0}, Direction: Vec3{X: 1}}, 0)
	if !ok {
		t.Fatal("expected a hit half way up the cone wall")
	}
	if math.Abs(hit.Distance-4.5) > 1e-9 {
		t.Fatalf("mid-wall distance = %v, want 4.5", hit.Distance)
	}
	if hit.Normal.X >= 0 || hit.Normal.Y <= 0 {
		t.Fatalf("wall normal = %+v, want it pointing out and up", hit.Normal)
	}

	// Above the apex: must miss.
	if _, ok := cone.Raycast(Ray{Origin: Vec3{X: -5, Y: 1.1}, Direction: Vec3{X: 1}}, 0); ok {
		t.Fatal("a ray above the apex must miss")
	}
	// Below the base: must miss.
	if _, ok := cone.Raycast(Ray{Origin: Vec3{X: -5, Y: -1.1}, Direction: Vec3{X: 1}}, 0); ok {
		t.Fatal("a ray below the base must miss")
	}
}

func TestRaycastHonoursMaxDistanceForNewShapes(t *testing.T) {
	shapes := map[string]*Collider{
		"capsule":  NewCollider(ColliderConfig{Shape: ShapeCapsule, Radius: 0.5, Height: 2, Offset: Vec3{X: 10}}),
		"cylinder": NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 2, Offset: Vec3{X: 10}}),
		"cone":     NewCollider(ColliderConfig{Shape: ShapeCone, Radius: 0.5, Height: 2, Offset: Vec3{X: 10}}),
	}
	for name, collider := range shapes {
		ray := Ray{Origin: Vec3{}, Direction: Vec3{X: 1}}
		if _, ok := collider.Raycast(ray, 5); ok {
			t.Fatalf("%s: a 5 unit ray must not reach a shape 10 units away", name)
		}
		if _, ok := collider.Raycast(ray, 20); !ok {
			t.Fatalf("%s: a 20 unit ray must reach the shape", name)
		}
	}
}

func TestWorldRaycastFindsCylinderAmongMixedShapes(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0})
	world.AddCollider(ColliderConfig{Shape: ShapeBox, Offset: Vec3{X: 8}, Width: 1, Height: 1, Depth: 1})
	cylinder := world.AddCollider(ColliderConfig{Shape: ShapeCylinder, Offset: Vec3{X: 4}, Radius: 0.5, Height: 2})
	world.AddCollider(ColliderConfig{Shape: ShapeCone, Offset: Vec3{X: 12}, Radius: 0.5, Height: 2})

	hit, ok := world.Raycast(Ray{Origin: Vec3{}, Direction: Vec3{X: 1}}, 0)
	if !ok {
		t.Fatal("expected a hit")
	}
	if hit.Collider != cylinder {
		t.Fatalf("hit collider index %d, want the cylinder at index %d",
			colliderIndex(hit.Collider), colliderIndex(cylinder))
	}
	if math.Abs(hit.Distance-3.5) > 1e-9 {
		t.Fatalf("distance = %v, want 3.5", hit.Distance)
	}
}
