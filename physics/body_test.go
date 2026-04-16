package physics

import "testing"

func TestRigidBodyForceAndImpulse(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 0.25, SolverIter: 1})
	body := world.AddBody(BodyConfig{Mass: 2, Position: Vec3{Y: 1}})

	body.ApplyForce(Vec3{X: 8})
	world.Step(0.25)
	if !body.Velocity.Near(Vec3{X: 1}, 1e-12) {
		t.Fatalf("force velocity = %+v", body.Velocity)
	}

	body.ApplyImpulse(Vec3{X: 0, Y: 0, Z: 2}, Vec3{})
	if !body.Velocity.Near(Vec3{X: 1, Z: 1}, 1e-12) {
		t.Fatalf("impulse velocity = %+v", body.Velocity)
	}
	if body.AngularVelocity.X == 0 {
		t.Fatalf("expected off-center impulse to change angular velocity, got %+v", body.AngularVelocity)
	}
}
