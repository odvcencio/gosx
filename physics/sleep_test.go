package physics

import (
	"math"
	"testing"
)

// Sleeping is opt in through BodyConfig.CanSleep. These tests check the three
// things a sleep system must get right: it must stop a settled body, it must
// leave every other body alone, and it must wake a sleeper that is disturbed.

func TestSettledBodySleepsWhenCanSleepIsSet(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIterations: 8})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	box := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 2}, Friction: 0.5, CanSleep: true})
	box.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})

	for i := 0; i < 300; i++ {
		world.StepFixed()
	}

	if !box.IsSleeping() {
		t.Fatalf("a settled box with CanSleep must sleep, velocity = %+v", box.Velocity)
	}
	resting := box.Position
	for i := 0; i < 3000; i++ {
		world.StepFixed()
	}
	if box.Position != resting {
		t.Fatalf("a sleeping body must not move: %+v became %+v", resting, box.Position)
	}
}

func TestBodyWithoutCanSleepKeepsSimulating(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIterations: 8})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	box := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 2}, Friction: 0.5})
	box.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})

	for i := 0; i < 600; i++ {
		world.StepFixed()
	}
	if box.IsSleeping() {
		t.Fatal("a body without CanSleep must never sleep")
	}
}

// TestSleepingStackStopsDriftingCompletely shows what sleeping buys. A stack
// that reaches rest freezes exactly, with no residual creep at all, over twenty
// thousand steps.
//
// The stack sleeps only when every box in it is slow. A box that leans on a
// moving box cannot sleep, because a sleeping body acts as an immovable
// support and one part of a pile freezing would shake the rest.
func TestSleepingStackStopsDriftingCompletely(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIterations: 10})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})

	boxes := make([]*RigidBody, 2)
	for i := range boxes {
		body := world.AddBody(BodyConfig{
			Mass: 1, Position: Vec3{Y: 0.5 + float64(i)}, Friction: 0.6, CanSleep: true,
		})
		body.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})
		boxes[i] = body
	}

	for i := 0; i < 300; i++ {
		world.StepFixed()
	}
	for i, box := range boxes {
		if !box.IsSleeping() {
			t.Fatalf("box[%d] never settled, velocity = %+v", i, box.Velocity)
		}
	}

	settled := make([]Vec3, len(boxes))
	for i, box := range boxes {
		settled[i] = box.Position
	}
	for i := 0; i < 20000; i++ {
		world.StepFixed()
	}
	for i, box := range boxes {
		if box.Position != settled[i] {
			t.Fatalf("box[%d] drifted from %+v to %+v over 20000 steps", i, settled[i], box.Position)
		}
		want := 0.5 + float64(i)
		if math.Abs(box.Position.Y-want) > 0.02 {
			t.Fatalf("box[%d] settled at y=%v, want near %v", i, box.Position.Y, want)
		}
	}
}

func TestSleepingBodyWakesWhenStruck(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIterations: 10})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	target := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 0.5}, Friction: 0.4, CanSleep: true})
	target.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})

	for i := 0; i < 120; i++ {
		world.StepFixed()
	}
	if !target.IsSleeping() {
		t.Fatal("the target must settle before the test means anything")
	}

	striker := world.AddBody(BodyConfig{Mass: 2, Position: Vec3{X: -4, Y: 0.5}, Velocity: Vec3{X: 6}})
	striker.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.4})

	for i := 0; i < 120; i++ {
		world.StepFixed()
		if !target.IsSleeping() {
			break
		}
	}

	if target.IsSleeping() {
		t.Fatal("a struck body must wake")
	}
	if target.Position.X <= 0.01 {
		t.Fatalf("the woken body should have been pushed, x = %v", target.Position.X)
	}
}

func TestWakeAllAndSetGravityWakeSleepers(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIterations: 8})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	box := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 0.5}, CanSleep: true})
	box.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})

	for i := 0; i < 120; i++ {
		world.StepFixed()
	}
	if !box.IsSleeping() {
		t.Fatal("the box must sleep first")
	}
	world.WakeAll()
	if box.IsSleeping() {
		t.Fatal("WakeAll must wake every body")
	}

	for i := 0; i < 120; i++ {
		world.StepFixed()
	}
	if !box.IsSleeping() {
		t.Fatal("the box must settle again")
	}
	world.SetGravity(Vec3{X: 10})
	if box.IsSleeping() {
		t.Fatal("changing gravity must wake every body")
	}
}

func TestRemovingTheSupportWakesTheBodyAbove(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIterations: 10})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	support := world.AddBody(BodyConfig{Mass: 0, Position: Vec3{Y: 1}})
	support.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 4, Height: 2, Depth: 4})
	box := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 2.5}, Friction: 0.5, CanSleep: true})
	box.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})

	for i := 0; i < 240; i++ {
		world.StepFixed()
	}
	if !box.IsSleeping() {
		t.Fatalf("the box must settle on the support first, velocity = %+v", box.Velocity)
	}

	world.RemoveBody(support)
	if box.IsSleeping() {
		t.Fatal("removing the support must wake the body it held")
	}
	for i := 0; i < 240; i++ {
		world.StepFixed()
	}
	if box.Position.Y > 0.6 {
		t.Fatalf("the box should have fallen to the plane, y = %v", box.Position.Y)
	}
}
