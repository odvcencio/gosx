package physics

import (
	"math"
	"testing"
)

// The swept pass used to report a zero distance hit whenever a body already
// touched a static collider. integrateVelocities then clamped the whole step,
// so a body resting on a static box could not move sideways at all. These tests
// pin the fixed behaviour and guard the tunnelling case the sweep exists for.

// TestBodyRestingOnStaticBoxSlidesFreely is the headline of the fix. A ball put
// down on a static box with a lateral push must travel.
func TestBodyRestingOnStaticBoxSlidesFreely(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIterations: 10})
	ground := world.AddBody(BodyConfig{Mass: 0})
	ground.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 40, Height: 2, Depth: 40})

	ball := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 1.6}, Velocity: Vec3{X: 3}})
	ball.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})

	for i := 0; i < 120; i++ {
		world.StepFixed()
	}

	if ball.Position.X < 5.5 {
		t.Fatalf("the ball froze on the static box: x = %v after 2 seconds at 3 m/s", ball.Position.X)
	}
	if math.Abs(ball.Velocity.X-3) > 0.05 {
		t.Fatalf("the ball lost speed with no friction: %v m/s", ball.Velocity.X)
	}
	if ball.Position.Y < 1.49 || ball.Position.Y > 1.51 {
		t.Fatalf("the ball left the box surface: y = %v, want near 1.5", ball.Position.Y)
	}
}

// TestBodyRestingOnPlaneSlidesFreely covers the same fix for a plane, which is
// the other collider the sweep reports a zero distance hit against.
func TestBodyRestingOnPlaneSlidesFreely(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIterations: 10})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	ball := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 0.6}, Velocity: Vec3{Z: 2}})
	ball.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})

	for i := 0; i < 120; i++ {
		world.StepFixed()
	}

	if ball.Position.Z < 3.7 {
		t.Fatalf("the ball froze on the plane: z = %v after 2 seconds at 2 m/s", ball.Position.Z)
	}
	if ball.Position.Y < 0.498 {
		t.Fatalf("the ball sank into the plane: y = %v", ball.Position.Y)
	}
}

// TestDiscreteResolutionAloneHoldsABodyUp removes the swept pass entirely. The
// discrete contacts plus the position pass must still stop a falling ball on
// the surface, which is what makes the sweep free to ignore resting bodies.
func TestDiscreteResolutionAloneHoldsABodyUp(t *testing.T) {
	world := NewWorld(WorldConfig{
		Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIterations: 8, DisableCCD: true,
	})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	ball := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 4}})
	ball.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})

	for i := 0; i < 300; i++ {
		world.StepFixed()
	}

	if ball.Position.Y < 0.499 {
		t.Fatalf("discrete resolution let the ball sink to y = %v", ball.Position.Y)
	}
	if ball.Position.Y > 0.5 {
		t.Fatalf("the ball floats above the plane at y = %v", ball.Position.Y)
	}
	if math.Abs(ball.Velocity.Y) > 1e-6 {
		t.Fatalf("the ball is not at rest, velocity = %+v", ball.Velocity)
	}
}

// TestSweepSkipsSlowBodiesAndKeepsFastOnes pins the motion gate directly. A body
// that moves less than half its own radius in a step cannot tunnel, so it must
// skip the sweep; a bullet must not.
func TestSweepSkipsSlowBodiesAndKeepsFastOnes(t *testing.T) {
	body := NewRigidBody(BodyConfig{Mass: 1})
	body.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})

	cases := []struct {
		name         string
		displacement Vec3
		want         bool
	}{
		{"still", Vec3{}, false},
		{"creeping", Vec3{X: 0.001}, false},
		{"walking", Vec3{X: 0.2}, false},
		{"just under the gate", Vec3{X: 0.24}, false},
		{"just over the gate", Vec3{X: 0.26}, true},
		{"bullet", Vec3{Y: -4}, true},
	}
	for _, testCase := range cases {
		if got := bodyNeedsSweep(body, testCase.displacement); got != testCase.want {
			t.Fatalf("%s: bodyNeedsSweep(%+v) = %v, want %v",
				testCase.name, testCase.displacement, got, testCase.want)
		}
	}
}

// TestSweepReportsNoHitWhenAlreadyTouching pins the other half of the fix. Every
// swept shape test must decline a target it already overlaps, so the caller
// never clamps a whole step to zero.
func TestSweepReportsNoHitWhenAlreadyTouching(t *testing.T) {
	plane := NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	if _, ok := sweepSpherePlane(Vec3{Y: 0.4}, 0.5, Vec3{X: 1}, 1, plane); ok {
		t.Fatal("a sphere already through the plane must report no swept hit")
	}
	if _, ok := sweepSpherePlane(Vec3{Y: 2}, 0.5, Vec3{Y: -1}, 3, plane); !ok {
		t.Fatal("a sphere falling toward the plane must still report a swept hit")
	}

	sphere := NewCollider(ColliderConfig{Shape: ShapeSphere, Radius: 1})
	if _, ok := sweepSphereSphere(Vec3{X: 0.5}, 1, Vec3{X: 1}, 2, sphere); ok {
		t.Fatal("two overlapping spheres must report no swept hit")
	}
	if _, ok := sweepSphereSphere(Vec3{X: -5}, 1, Vec3{X: 1}, 10, sphere); !ok {
		t.Fatal("a sphere approaching from outside must still report a swept hit")
	}

	box := NewCollider(ColliderConfig{Shape: ShapeBox, Width: 2, Height: 2, Depth: 2})
	if _, ok := sweepSphereBox(Vec3{Y: 1.2}, 0.5, Vec3{X: 1}, 2, box); ok {
		t.Fatal("a sphere resting on the box must report no swept hit")
	}
	if _, ok := sweepSphereBox(Vec3{Y: 5}, 0.5, Vec3{Y: -1}, 10, box); !ok {
		t.Fatal("a sphere falling toward the box must still report a swept hit")
	}
}

// TestFastBodiesStillCannotTunnel is the regression guard for the gate. Every
// speed above the gate must still be caught by the sweep.
func TestFastBodiesStillCannotTunnel(t *testing.T) {
	for _, speed := range []float64{-40, -120, -240, -600, -2000} {
		world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0, SolverIterations: 8})
		world.AddCollider(ColliderConfig{Shape: ShapeBox, Offset: Vec3{Y: -1}, Width: 100, Height: 2, Depth: 100})
		bullet := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 3}, Velocity: Vec3{Y: speed}})
		bullet.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.05})

		for i := 0; i < 30; i++ {
			world.StepFixed()
		}
		if bullet.Position.Y < 0 {
			t.Fatalf("a bullet at %v m/s tunneled to y = %v", speed, bullet.Position.Y)
		}
	}
}
