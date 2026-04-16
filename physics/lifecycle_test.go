package physics

import "testing"

func TestWorldRemoveColliderDetachesFromBody(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0})
	body := world.AddBody(BodyConfig{Mass: 1})
	collider := body.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 1})

	if got := len(world.Colliders()); got != 1 {
		t.Fatalf("colliders before removal = %d", got)
	}
	if !world.RemoveCollider(collider) {
		t.Fatal("RemoveCollider returned false")
	}
	if got := len(world.Colliders()); got != 0 {
		t.Fatalf("colliders after removal = %d", got)
	}
	if got := len(body.Colliders()); got != 0 {
		t.Fatalf("body colliders after removal = %d", got)
	}
	if collider.Body != nil || collider.index != 0 {
		t.Fatalf("collider not detached: body=%+v index=%d", collider.Body, collider.index)
	}
}

func TestWorldRemoveBodyPrunesCollidersContactsAndConstraints(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIter: 4})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	anchor := world.AddBody(BodyConfig{ID: "anchor", Mass: 0, Position: Vec3{Y: 2}})
	bob := world.AddBody(BodyConfig{ID: "bob", Mass: 1, Position: Vec3{Y: 0.25}})
	bob.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})
	world.AddConstraint(&DistanceConstraint{BodyA: anchor, BodyB: bob, TargetDistance: 1})

	world.Step(1.0 / 60.0)
	if got := len(world.Contacts()); got == 0 {
		t.Fatalf("expected contact before removal, got %d", got)
	}
	if got := len(world.Constraints()); got != 1 {
		t.Fatalf("constraints before removal = %d", got)
	}

	if !world.RemoveBody(bob) {
		t.Fatal("RemoveBody returned false")
	}
	if got := len(world.Bodies()); got != 1 {
		t.Fatalf("bodies after removal = %d", got)
	}
	if got := len(world.Colliders()); got != 1 {
		t.Fatalf("colliders after removal = %d", got)
	}
	if got := len(world.Contacts()); got != 0 {
		t.Fatalf("contacts after removal = %d", got)
	}
	if got := len(world.Constraints()); got != 0 {
		t.Fatalf("constraints after removal = %d", got)
	}
	if bob.world != nil || len(bob.Colliders()) != 0 {
		t.Fatalf("body not detached: world=%+v colliders=%d", bob.world, len(bob.Colliders()))
	}
}
