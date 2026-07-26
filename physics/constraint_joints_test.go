package physics

import (
	"math"
	"testing"
)

// Joint tests check closed-form behaviour and invariants, not screenshots.
// A pendulum has a known period bound, a hinge has one free axis and two locked
// ones, and a weld holds a relative pose whatever the load.

// --- Ball and socket ---------------------------------------------------------

// TestPointConstraintHoldsAPendulumOnItsAnchor checks the three linear rows. The
// bob must stay on a sphere of the rod radius around the anchor, and it must
// swing rather than hang still.
func TestPointConstraintHoldsAPendulumOnItsAnchor(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 240.0, SolverIterations: 20})
	anchor := world.AddBody(BodyConfig{Mass: 0, Position: Vec3{Y: 4}})
	bob := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: 1.5, Y: 4}})
	bob.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.2})

	// The joint holds the bob's own local point (-1.5, 0, 0) on the anchor, so
	// the bob centre traces a sphere of radius 1.5 around it.
	world.AddConstraint(&PointConstraint{
		BodyA:   anchor,
		BodyB:   bob,
		AttachA: Vec3{},
		AttachB: Vec3{X: -1.5},
	})

	lowest := math.Inf(1)
	for i := 0; i < 2400; i++ {
		world.StepFixed()
		offset := bob.Position.Sub(anchor.Position)
		if math.Abs(offset.Len()-1.5) > 0.02 {
			t.Fatalf("step %d: bob left the sphere of radius 1.5, distance = %v", i, offset.Len())
		}
		lowest = math.Min(lowest, bob.Position.Y)
	}
	if lowest > 2.7 {
		t.Fatalf("the pendulum never swung down, lowest y = %v", lowest)
	}
	if !anchor.Position.Near(Vec3{Y: 4}, 1e-12) {
		t.Fatalf("a static anchor must not move, got %+v", anchor.Position)
	}
}

// TestPointConstraintChainHangsStraight checks that a chain of ball joints holds
// its links together and settles under gravity.
func TestPointConstraintChainHangsStraight(t *testing.T) {
	const links = 5
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 240.0, SolverIterations: 24})
	anchor := world.AddBody(BodyConfig{Mass: 0, Position: Vec3{Y: 6}})

	// Each link is 0.5 tall and hangs from the one above by its top face, so
	// link i sits at y = 5.75 - i*0.5 and link zero's top touches the anchor.
	linkHeight := func(i int) float64 { return 5.75 - float64(i)*0.5 }

	previous := anchor
	bodies := make([]*RigidBody, 0, links)
	for i := 0; i < links; i++ {
		link := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: linkHeight(i)}, LinearDamping: 0.4, AngularDamping: 0.4})
		link.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 0.2, Height: 0.5, Depth: 0.2})
		attachA := Vec3{Y: -0.25}
		if i == 0 {
			attachA = Vec3{}
		}
		world.AddConstraint(&PointConstraint{
			BodyA: previous, BodyB: link,
			AttachA: attachA, AttachB: Vec3{Y: 0.25},
		})
		previous = link
		bodies = append(bodies, link)
	}

	for i := 0; i < 4000; i++ {
		world.StepFixed()
	}

	for i, link := range bodies {
		want := linkHeight(i)
		if math.Abs(link.Position.Y-want) > 0.06 {
			t.Fatalf("link %d hangs at y=%v, want near %v", i, link.Position.Y, want)
		}
		if lateral := math.Hypot(link.Position.X, link.Position.Z); lateral > 0.06 {
			t.Fatalf("link %d drifted %v sideways", i, lateral)
		}
	}
}

// --- Hinge -------------------------------------------------------------------

// TestHingeAllowsOneAxisAndLocksTwo is the defining property of a revolute
// joint. A torque about the hinge axis must spin the door; a torque about a
// perpendicular axis must not tilt it.
func TestHingeAllowsOneAxisAndLocksTwo(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 240.0, SolverIterations: 24})
	frame := world.AddBody(BodyConfig{Mass: 0})
	door := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: 0.5}})
	door.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 2, Depth: 0.1})

	world.AddConstraint(&HingeConstraint{
		BodyA:   frame,
		BodyB:   door,
		AttachA: Vec3{},
		AttachB: Vec3{X: -0.5},
		AxisA:   Vec3{Y: 1},
		AxisB:   Vec3{Y: 1},
	})

	for i := 0; i < 240; i++ {
		door.ApplyTorque(Vec3{X: 8, Y: 4, Z: 8})
		world.StepFixed()
	}

	if door.AngularVelocity.Y <= 0.2 {
		t.Fatalf("the hinge must turn about its own axis, angular velocity = %+v", door.AngularVelocity)
	}
	if math.Abs(door.AngularVelocity.X) > 0.02 || math.Abs(door.AngularVelocity.Z) > 0.02 {
		t.Fatalf("the hinge must lock the other two axes, angular velocity = %+v", door.AngularVelocity)
	}
	// The hinge point must not drift away from the frame.
	if math.Abs(door.Position.Len()-0.5) > 0.02 {
		t.Fatalf("the door left its hinge point, position = %+v", door.Position)
	}
}

// TestHingeMotorReachesTargetSpeed checks the motor row. It must reach the
// commanded speed and hold it against gravity.
func TestHingeMotorReachesTargetSpeed(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 240.0, SolverIterations: 20})
	frame := world.AddBody(BodyConfig{Mass: 0})
	arm := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: 0.5}})
	arm.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 0.1, Depth: 0.1})

	hinge := &HingeConstraint{
		BodyA: frame, BodyB: arm,
		AttachA: Vec3{}, AttachB: Vec3{X: -0.5},
		AxisA: Vec3{Z: 1}, AxisB: Vec3{Z: 1},
		MotorEnabled: true, MotorSpeed: 2.5, MaxMotorTorque: 60,
	}
	world.AddConstraint(hinge)

	for i := 0; i < 1200; i++ {
		world.StepFixed()
	}

	if math.Abs(arm.AngularVelocity.Z-2.5) > 0.15 {
		t.Fatalf("motor should hold 2.5 rad/s, got %v", arm.AngularVelocity.Z)
	}
	if math.Abs(arm.AngularVelocity.X) > 0.05 || math.Abs(arm.AngularVelocity.Y) > 0.05 {
		t.Fatalf("motor must not leak into the locked axes, angular velocity = %+v", arm.AngularVelocity)
	}
}

// TestHingeMotorRespectsItsTorqueBudget checks the other half of the motor. A
// weak motor must fail to lift a heavy arm against gravity.
func TestHingeMotorRespectsItsTorqueBudget(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 240.0, SolverIterations: 20})
	frame := world.AddBody(BodyConfig{Mass: 0})
	arm := world.AddBody(BodyConfig{Mass: 20, Position: Vec3{X: 1}})
	arm.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 2, Height: 0.1, Depth: 0.1})

	world.AddConstraint(&HingeConstraint{
		BodyA: frame, BodyB: arm,
		AttachA: Vec3{}, AttachB: Vec3{X: -1},
		AxisA: Vec3{Z: 1}, AxisB: Vec3{Z: 1},
		MotorEnabled: true, MotorSpeed: 4, MaxMotorTorque: 0.05,
	})

	for i := 0; i < 480; i++ {
		world.StepFixed()
	}

	if arm.Position.Y > -0.2 {
		t.Fatalf("a motor capped at 0.05 N m cannot hold a 20 kg arm, y = %v", arm.Position.Y)
	}
}

// TestHingeLimitStopsTheJoint checks the limit rows. The arm must swing down
// under gravity and stop at the lower limit.
func TestHingeLimitStopsTheJoint(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 240.0, SolverIterations: 24})
	frame := world.AddBody(BodyConfig{Mass: 0})
	arm := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: 1}, AngularDamping: 0.4})
	arm.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 2, Height: 0.1, Depth: 0.1})

	hinge := &HingeConstraint{
		BodyA: frame, BodyB: arm,
		AttachA: Vec3{}, AttachB: Vec3{X: -1},
		AxisA: Vec3{Z: 1}, AxisB: Vec3{Z: 1},
		LimitEnabled: true,
		LowerLimit:   -0.6,
		UpperLimit:   0.6,
	}
	world.AddConstraint(hinge)

	worst := 0.0
	for i := 0; i < 3000; i++ {
		world.StepFixed()
		angle := hinge.Angle()
		if angle < -0.6 {
			worst = math.Min(worst, angle+0.6)
		}
	}

	final := hinge.Angle()
	if final > -0.5 {
		t.Fatalf("gravity should push the arm onto the lower limit, angle = %v", final)
	}
	if worst < -0.12 {
		t.Fatalf("the arm broke through the limit by %v radians", -worst)
	}
}

// --- Fixed / weld ------------------------------------------------------------

// TestFixedConstraintWeldsTwoBodiesIntoOne checks that a weld keeps both the
// offset and the relative rotation while the pair tumbles.
func TestFixedConstraintWeldsTwoBodiesIntoOne(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 240.0, SolverIterations: 24, DisableCCD: true})
	a := world.AddBody(BodyConfig{Mass: 1, AngularVelocity: Vec3{X: 1.5, Y: 0.8, Z: -1.1}})
	a.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})
	b := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: 1}})
	b.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})

	world.AddConstraint(&FixedConstraint{
		BodyA: a, BodyB: b,
		AttachA: Vec3{X: 0.5}, AttachB: Vec3{X: -0.5},
	})

	wantRelative := a.Rotation.Inverse().Mul(b.Rotation).Normalize()
	for i := 0; i < 2400; i++ {
		world.StepFixed()

		offset := b.Position.Sub(a.Position)
		want := a.Rotation.Rotate(Vec3{X: 1})
		if offset.Sub(want).Len() > 0.03 {
			t.Fatalf("step %d: the weld slipped, offset = %+v want %+v", i, offset, want)
		}
		relative := a.Rotation.Inverse().Mul(b.Rotation).Normalize()
		if angleBetweenQuats(relative, wantRelative) > 0.05 {
			t.Fatalf("step %d: the weld twisted by %v radians", i, angleBetweenQuats(relative, wantRelative))
		}
	}
}

// TestFixedConstraintCarriesLoadToAStaticAnchor checks that a weld to a static
// body holds a cantilever up against gravity.
func TestFixedConstraintCarriesLoadToAStaticAnchor(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 240.0, SolverIterations: 24})
	wall := world.AddBody(BodyConfig{Mass: 0})
	beam := world.AddBody(BodyConfig{Mass: 2, Position: Vec3{X: 1}})
	beam.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 2, Height: 0.2, Depth: 0.2})

	world.AddConstraint(&FixedConstraint{
		BodyA: wall, BodyB: beam,
		AttachA: Vec3{}, AttachB: Vec3{X: -1},
	})

	for i := 0; i < 2400; i++ {
		world.StepFixed()
	}

	if math.Abs(beam.Position.Y) > 0.05 {
		t.Fatalf("the cantilever sagged to y = %v", beam.Position.Y)
	}
	tilt := math.Hypot(math.Hypot(beam.Rotation.X, beam.Rotation.Y), beam.Rotation.Z)
	if tilt > 0.05 {
		t.Fatalf("the cantilever rotated, rotation vector part = %v", tilt)
	}
}

// --- Lifecycle and spec ------------------------------------------------------

func TestRemoveBodyDropsEveryJointKind(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0, SolverIter: 4})
	a := world.AddBody(BodyConfig{Mass: 1})
	b := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: 1}})

	world.AddConstraint(&DistanceConstraint{BodyA: a, BodyB: b, TargetDistance: 1})
	world.AddConstraint(&PointConstraint{BodyA: a, BodyB: b})
	world.AddConstraint(&HingeConstraint{BodyA: a, BodyB: b, AxisA: Vec3{Y: 1}, AxisB: Vec3{Y: 1}})
	world.AddConstraint(&FixedConstraint{BodyA: a, BodyB: b})
	if got := len(world.Constraints()); got != 4 {
		t.Fatalf("Constraints() = %d, want 4", got)
	}

	world.RemoveBody(b)
	if got := len(world.Constraints()); got != 0 {
		t.Fatalf("after RemoveBody, Constraints() = %d, want 0", got)
	}
}

func TestBuildWorldAddsEveryJointKind(t *testing.T) {
	world := BuildWorld(WorldSpec{
		Config: WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0, SolverIter: 4},
		Bodies: []BodySpec{
			{Body: BodyConfig{ID: "anchor", Mass: 0}},
			{Body: BodyConfig{ID: "bob", Mass: 1, Position: Vec3{X: 2}}},
		},
		Constraints: []ConstraintSpec{
			{Kind: "point", BodyAID: "anchor", BodyBID: "bob"},
			{
				Kind: "hinge", BodyAID: "anchor", BodyBID: "bob",
				AxisA: Vec3{Z: 1}, AxisB: Vec3{Z: 1},
				MotorEnabled: true, MotorSpeed: 1.25, MaxMotorTorque: 9,
				LimitEnabled: true, LowerLimit: -1, UpperLimit: 1,
			},
			{Kind: "fixed", BodyAID: "anchor", BodyBID: "bob"},
		},
	})

	constraints := world.Constraints()
	if len(constraints) != 3 {
		t.Fatalf("BuildWorld constraints = %d, want 3", len(constraints))
	}
	if _, ok := constraints[0].(*PointConstraint); !ok {
		t.Fatalf("constraint 0 type = %T", constraints[0])
	}
	hinge, ok := constraints[1].(*HingeConstraint)
	if !ok {
		t.Fatalf("constraint 1 type = %T", constraints[1])
	}
	if hinge.MotorSpeed != 1.25 || hinge.MaxMotorTorque != 9 {
		t.Fatalf("hinge motor = %v rad/s, %v N m", hinge.MotorSpeed, hinge.MaxMotorTorque)
	}
	if !hinge.LimitEnabled || hinge.LowerLimit != -1 || hinge.UpperLimit != 1 {
		t.Fatalf("hinge limits = %v..%v enabled=%v", hinge.LowerLimit, hinge.UpperLimit, hinge.LimitEnabled)
	}
	if _, ok := constraints[2].(*FixedConstraint); !ok {
		t.Fatalf("constraint 2 type = %T", constraints[2])
	}
}

// angleBetweenQuats returns the smallest rotation angle between two unit
// quaternions, in radians.
func angleBetweenQuats(a, b Quat) float64 {
	dot := math.Abs(a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W)
	if dot > 1 {
		dot = 1
	}
	return 2 * math.Acos(dot)
}
