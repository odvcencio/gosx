package physics

import "testing"

func TestWorldRaycastReturnsClosestHit(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0})
	boxBody := world.AddBody(BodyConfig{ID: "box", Mass: 0, Position: Vec3{Z: -3}})
	boxCollider := boxBody.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})
	sphereBody := world.AddBody(BodyConfig{ID: "sphere", Mass: 0, Position: Vec3{Z: -6}})
	sphereBody.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 1})

	hit, ok := world.Raycast(Ray{Origin: Vec3{}, Direction: Vec3{Z: -2}}, 0)
	if !ok {
		t.Fatal("expected ray hit")
	}
	if hit.Collider != boxCollider || hit.Body != boxBody {
		t.Fatalf("closest hit = collider %+v body %+v", hit.Collider, hit.Body)
	}
	if !hit.Point.Near(Vec3{Z: -2.5}, 1e-9) {
		t.Fatalf("hit point = %+v", hit.Point)
	}
	if !hit.Normal.Near(Vec3{Z: 1}, 1e-9) {
		t.Fatalf("hit normal = %+v", hit.Normal)
	}
	if hit.Distance < 2.49 || hit.Distance > 2.51 {
		t.Fatalf("hit distance = %v", hit.Distance)
	}
}

func TestColliderRaycastSphereAndPlane(t *testing.T) {
	sphere := NewCollider(ColliderConfig{Shape: ShapeSphere, Radius: 2, Offset: Vec3{X: 5}})
	hit, ok := sphere.Raycast(Ray{Origin: Vec3{}, Direction: Vec3{X: 1}}, 10)
	if !ok {
		t.Fatal("expected sphere hit")
	}
	if hit.Distance < 2.99 || hit.Distance > 3.01 {
		t.Fatalf("sphere distance = %v", hit.Distance)
	}
	if !hit.Normal.Near(Vec3{X: -1}, 1e-9) {
		t.Fatalf("sphere normal = %+v", hit.Normal)
	}

	plane := NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	hit, ok = plane.Raycast(Ray{Origin: Vec3{Y: 3}, Direction: Vec3{Y: -1}}, 10)
	if !ok {
		t.Fatal("expected plane hit")
	}
	if hit.Distance != 3 {
		t.Fatalf("plane distance = %v", hit.Distance)
	}
	if !hit.Normal.Near(Vec3{Y: 1}, 1e-9) {
		t.Fatalf("plane normal = %+v", hit.Normal)
	}
}

func TestWorldRaycastHonorsMaxDistance(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0})
	body := world.AddBody(BodyConfig{Mass: 0, Position: Vec3{Z: -5}})
	body.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 1})

	if _, ok := world.Raycast(Ray{Origin: Vec3{}, Direction: Vec3{Z: -1}}, 3); ok {
		t.Fatal("raycast should miss beyond max distance")
	}
	if _, ok := world.Raycast(Ray{Origin: Vec3{}, Direction: Vec3{Z: -1}}, 5); !ok {
		t.Fatal("raycast should hit inside max distance")
	}
}
