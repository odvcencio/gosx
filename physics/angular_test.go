package physics

import (
	"math"
	"math/rand"
	"testing"
)

// Angular contact response is easy to get wrong in ways that still look
// plausible on screen. Every test in this file checks a physical invariant or a
// closed-form answer, and none of them shares code with the solver.

// systemState is an independent bookkeeping of the conserved quantities of a
// closed system. It recomputes the inertia tensor from the collider geometry by
// hand rather than reading the body's cached tensor.
type systemState struct {
	Momentum        Vec3
	AngularMomentum Vec3
	Kinetic         float64
}

// measureSystem sums linear momentum, angular momentum about the world origin
// and kinetic energy over the given bodies.
//
// Angular momentum about the origin is r x p for the translation plus I * w for
// the spin. Kinetic energy is the translational half m v^2 plus the rotational
// half w . (I w).
func measureSystem(t *testing.T, bodies []*RigidBody) systemState {
	t.Helper()
	var state systemState
	for _, body := range bodies {
		inertia := oracleWorldInertia(t, body)
		state.Momentum = state.Momentum.Add(body.Velocity.Mul(body.Mass))
		state.AngularMomentum = state.AngularMomentum.
			Add(body.Position.Cross(body.Velocity.Mul(body.Mass))).
			Add(inertia.mul(body.AngularVelocity))
		state.Kinetic += 0.5 * body.Mass * body.Velocity.Len2()
		state.Kinetic += 0.5 * body.AngularVelocity.Dot(inertia.mul(body.AngularVelocity))
	}
	return state
}

// oracleWorldInertia rebuilds the world inertia tensor of a body from its shape
// and rotation, using textbook formulas written out here on purpose. It shares
// no code with inertia.go.
func oracleWorldInertia(t *testing.T, body *RigidBody) mat3 {
	t.Helper()
	colliders := body.Colliders()
	if len(colliders) != 1 {
		t.Fatalf("oracle handles one collider per body, body %q has %d", body.ID, len(colliders))
	}
	c := colliders[0]
	var diagonal Vec3
	switch c.Shape {
	case ShapeSphere:
		i := 2.0 / 5.0 * body.Mass * c.Radius * c.Radius
		diagonal = Vec3{X: i, Y: i, Z: i}
	case ShapeBox:
		w, h, d := c.Width, c.Height, c.Depth
		diagonal = Vec3{
			X: body.Mass * (h*h + d*d) / 12,
			Y: body.Mass * (w*w + d*d) / 12,
			Z: body.Mass * (w*w + h*h) / 12,
		}
	default:
		t.Fatalf("oracle does not model shape %v", c.Shape)
	}
	rotation := mat3FromQuat(body.Rotation)
	return rotateTensor(rotation, mat3Diagonal(diagonal))
}

func assertVecNear(t *testing.T, label string, got, want Vec3, tolerance float64) {
	t.Helper()
	if math.Abs(got.X-want.X) > tolerance ||
		math.Abs(got.Y-want.Y) > tolerance ||
		math.Abs(got.Z-want.Z) > tolerance {
		t.Fatalf("%s = %+v, want %+v within %v", label, got, want, tolerance)
	}
}

// --- Conservation ------------------------------------------------------------

// TestContactImpulseConservesMomentumExactly isolates the solver primitive.
// Two free bodies receive equal and opposite impulses at one shared world
// point, which is what every contact does. Linear and angular momentum about
// the world origin must both survive to the last bit of precision.
//
// No position correction runs here, so the check needs no tolerance for body
// movement. It is the strictest angular statement in the file.
func TestContactImpulseConservesMomentumExactly(t *testing.T) {
	rng := rand.New(rand.NewSource(31337))
	for trial := 0; trial < 200; trial++ {
		a := NewRigidBody(BodyConfig{
			Mass:            0.5 + rng.Float64()*3,
			Position:        Vec3{X: rng.Float64()*4 - 2, Y: rng.Float64()*4 - 2, Z: rng.Float64()*4 - 2},
			Rotation:        randomUnitQuat(rng),
			Velocity:        Vec3{X: rng.Float64() - 0.5, Y: rng.Float64() - 0.5, Z: rng.Float64() - 0.5},
			AngularVelocity: Vec3{X: rng.Float64() - 0.5, Y: rng.Float64() - 0.5, Z: rng.Float64() - 0.5},
		})
		a.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 0.6, Height: 1.1, Depth: 0.9})
		b := NewRigidBody(BodyConfig{
			Mass:            0.5 + rng.Float64()*3,
			Position:        Vec3{X: rng.Float64()*4 - 2, Y: rng.Float64()*4 - 2, Z: rng.Float64()*4 - 2},
			Rotation:        randomUnitQuat(rng),
			Velocity:        Vec3{X: rng.Float64() - 0.5, Y: rng.Float64() - 0.5, Z: rng.Float64() - 0.5},
			AngularVelocity: Vec3{X: rng.Float64() - 0.5, Y: rng.Float64() - 0.5, Z: rng.Float64() - 0.5},
		})
		b.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.55})

		bodies := []*RigidBody{a, b}
		before := measureSystem(t, bodies)

		point := Vec3{X: rng.Float64()*4 - 2, Y: rng.Float64()*4 - 2, Z: rng.Float64()*4 - 2}
		impulse := Vec3{X: rng.Float64()*6 - 3, Y: rng.Float64()*6 - 3, Z: rng.Float64()*6 - 3}
		applyImpulseAtOffset(a, impulse.Neg(), point.Sub(a.Position))
		applyImpulseAtOffset(b, impulse, point.Sub(b.Position))

		after := measureSystem(t, bodies)
		assertVecNear(t, "linear momentum", after.Momentum, before.Momentum, 1e-12)
		assertVecNear(t, "angular momentum", after.AngularMomentum, before.AngularMomentum, 1e-11)
	}
}

// TestClosedSystemConservesMomentumAndEnergy runs collisions with no gravity, no
// damping and no friction. Linear momentum, angular momentum and kinetic energy
// must all survive the collision.
//
// Linear momentum is exact. Angular momentum carries a small tolerance because
// the position pass removes the overlap by moving bodies, and moving a body
// with velocity v by dx changes r x p by dx x mv. That shift is bounded by the
// overlap the discrete step allows, and it is unrelated to the impulse itself,
// which TestContactImpulseConservesMomentumExactly checks with no tolerance.
func TestClosedSystemConservesMomentumAndEnergy(t *testing.T) {
	cases := []struct {
		name    string
		build   func(*World) []*RigidBody
		steps   int
		angular float64
		energy  float64
	}{
		{
			name: "head on spheres",
			build: func(w *World) []*RigidBody {
				a := w.AddBody(BodyConfig{ID: "a", Mass: 1, Position: Vec3{X: -2}, Velocity: Vec3{X: 4}, Restitution: 1})
				a.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})
				b := w.AddBody(BodyConfig{ID: "b", Mass: 1, Position: Vec3{X: 2}, Velocity: Vec3{X: -4}, Restitution: 1})
				b.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})
				return []*RigidBody{a, b}
			},
			steps: 240, angular: 1e-9, energy: 1e-6,
		},
		{
			name: "spinning sphere strikes a resting sphere",
			build: func(w *World) []*RigidBody {
				a := w.AddBody(BodyConfig{
					ID: "a", Mass: 2, Position: Vec3{X: -2, Y: 0.3},
					Velocity: Vec3{X: 5}, AngularVelocity: Vec3{Z: 3}, Restitution: 1,
				})
				a.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})
				b := w.AddBody(BodyConfig{ID: "b", Mass: 1, Restitution: 1})
				b.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})
				return []*RigidBody{a, b}
			},
			steps: 240, angular: 0.02, energy: 1e-4,
		},
		{
			name: "box struck off centre by a sphere",
			build: func(w *World) []*RigidBody {
				a := w.AddBody(BodyConfig{ID: "a", Mass: 1, Position: Vec3{X: -2, Y: 0.4}, Velocity: Vec3{X: 6}, Restitution: 1})
				a.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.3})
				b := w.AddBody(BodyConfig{ID: "b", Mass: 3, Restitution: 1})
				b.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})
				return []*RigidBody{a, b}
			},
			steps: 240, angular: 0.05, energy: 1e-3,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			world := NewWorld(WorldConfig{
				Gravity:       Vec3{},
				FixedTimestep: 1.0 / 240.0,
				SolverIter:    30,
				DisableCCD:    true,
			})
			bodies := testCase.build(world)
			before := measureSystem(t, bodies)

			touched := false
			for i := 0; i < testCase.steps; i++ {
				world.StepFixed()
				if len(world.contacts) > 0 {
					touched = true
				}
			}
			if !touched {
				t.Fatal("the bodies never touched; the test proves nothing")
			}

			after := measureSystem(t, bodies)
			assertVecNear(t, "linear momentum", after.Momentum, before.Momentum, 1e-9)
			assertVecNear(t, "angular momentum", after.AngularMomentum, before.AngularMomentum, testCase.angular)
			if math.Abs(after.Kinetic-before.Kinetic) > testCase.energy {
				t.Fatalf("kinetic energy = %v, want %v within %v", after.Kinetic, before.Kinetic, testCase.energy)
			}
		})
	}
}

// TestInelasticCollisionNeverGainsEnergy checks the other half of the energy
// invariant. Restitution below one must lose energy, never gain it, and both
// momenta must still survive.
func TestInelasticCollisionNeverGainsEnergy(t *testing.T) {
	rng := rand.New(rand.NewSource(4242))
	for trial := 0; trial < 40; trial++ {
		world := NewWorld(WorldConfig{
			Gravity: Vec3{}, FixedTimestep: 1.0 / 240.0, SolverIter: 20, DisableCCD: true,
		})
		restitution := rng.Float64() * 0.9
		a := world.AddBody(BodyConfig{
			Mass:            0.5 + rng.Float64()*2,
			Position:        Vec3{X: -1.5, Y: (rng.Float64() - 0.5) * 0.6, Z: (rng.Float64() - 0.5) * 0.6},
			Velocity:        Vec3{X: 2 + rng.Float64()*4},
			AngularVelocity: Vec3{X: rng.Float64() - 0.5, Y: rng.Float64() - 0.5, Z: rng.Float64() - 0.5},
			Restitution:     restitution,
		})
		a.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})
		b := world.AddBody(BodyConfig{
			Mass:        0.5 + rng.Float64()*2,
			Position:    Vec3{X: 1.5},
			Velocity:    Vec3{X: -(1 + rng.Float64()*3)},
			Restitution: restitution,
		})
		b.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})

		bodies := []*RigidBody{a, b}
		before := measureSystem(t, bodies)
		for i := 0; i < 300; i++ {
			world.StepFixed()
		}
		after := measureSystem(t, bodies)

		assertVecNear(t, "linear momentum", after.Momentum, before.Momentum, 1e-9)
		// Angular momentum keeps a relative tolerance for the same reason as
		// the test above: the position pass moves overlapping bodies, and that
		// shifts r x p without any impulse.
		angularTolerance := 0.02*before.AngularMomentum.Len() + 0.01
		assertVecNear(t, "angular momentum", after.AngularMomentum, before.AngularMomentum, angularTolerance)
		if after.Kinetic > before.Kinetic+1e-9 {
			t.Fatalf("trial %d: kinetic energy grew from %v to %v", trial, before.Kinetic, after.Kinetic)
		}
	}
}

// --- Analytic cases ----------------------------------------------------------

// TestEqualSpheresExchangeVelocityHeadOn is the textbook elastic collision of
// two equal masses: they swap velocities exactly.
func TestEqualSpheresExchangeVelocityHeadOn(t *testing.T) {
	world := NewWorld(WorldConfig{
		Gravity: Vec3{}, FixedTimestep: 1.0 / 240.0, SolverIter: 20, DisableCCD: true,
	})
	a := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: -1.5}, Velocity: Vec3{X: 3}, Restitution: 1})
	a.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})
	b := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: 1.5}, Restitution: 1})
	b.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})

	for i := 0; i < 400; i++ {
		world.StepFixed()
	}

	assertVecNear(t, "struck sphere velocity", b.Velocity, Vec3{X: 3}, 1e-6)
	assertVecNear(t, "striker velocity", a.Velocity, Vec3{}, 1e-6)
	if a.AngularVelocity.Len() > 1e-9 || b.AngularVelocity.Len() > 1e-9 {
		t.Fatalf("a frictionless central hit must not spin either sphere: a=%+v b=%+v",
			a.AngularVelocity, b.AngularVelocity)
	}
}

// TestOffCentreHitMatchesClosedFormImpulse checks the angular half of the
// contact response against the closed form of a single elastic impulse.
//
// A sphere of mass m1 strikes a resting sphere of mass m2. The contact normal
// runs between the centres, so the impulse magnitude is
//
//	j = (1 + e) * v . n / (1/m1 + 1/m2)
//
// The struck sphere leaves along n at j/m2, and the striker keeps v - j/m1 * n.
// A frictionless normal impulse passes through both centres, so neither sphere
// gains spin however far off centre the approach is.
func TestOffCentreHitMatchesClosedFormImpulse(t *testing.T) {
	const offset = 0.4 // vertical miss distance between the two centres
	const speed = 5.0
	const massA = 2.0
	const massB = 1.0
	const radius = 0.5

	world := NewWorld(WorldConfig{
		Gravity: Vec3{}, FixedTimestep: 1.0 / 2000.0, SolverIter: 30, DisableCCD: true,
	})
	a := world.AddBody(BodyConfig{Mass: massA, Position: Vec3{X: -3, Y: offset}, Velocity: Vec3{X: speed}, Restitution: 1})
	a.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: radius})
	b := world.AddBody(BodyConfig{Mass: massB, Restitution: 1})
	b.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: radius})

	for i := 0; i < 1600; i++ {
		world.StepFixed()
	}

	// At the moment of contact the two centres are 2*radius apart, with a
	// vertical separation of offset. That fixes the contact normal.
	horizontal := math.Sqrt(4*radius*radius - offset*offset)
	normal := Vec3{X: horizontal, Y: -offset}.Normalize()
	approach := Vec3{X: speed}.Dot(normal)
	impulse := 2 * approach / (1/massA + 1/massB)

	wantB := normal.Mul(impulse / massB)
	wantA := Vec3{X: speed}.Sub(normal.Mul(impulse / massA))

	// The tolerance covers the overlap that one discrete step allows: the
	// contact normal is measured when the centres are slightly closer than two
	// radii, which tilts the normal by about one step of approach.
	assertVecNear(t, "struck sphere velocity", b.Velocity, wantB, 0.03)
	assertVecNear(t, "striker velocity", a.Velocity, wantA, 0.03)
	if b.AngularVelocity.Len() > 1e-9 {
		t.Fatalf("a frictionless normal impulse cannot spin a sphere, got %+v", b.AngularVelocity)
	}
}

// TestRodPivotingAboutOneEndMatchesAngularAcceleration checks the inertia
// tensor and the torque integrator against the closed form of a rod on a pivot.
//
// A uniform rod of length L pinned at one end has inertia m L^2 / 3 about the
// pivot. Held horizontal, gravity acts at the centre with moment arm L/2, so
// the angular acceleration at release is
//
//	alpha = m g (L/2) / (m L^2 / 3) = 3 g / (2 L)
func TestRodPivotingAboutOneEndMatchesAngularAcceleration(t *testing.T) {
	const length = 2.0
	const mass = 1.0
	const gravity = -10.0

	world := NewWorld(WorldConfig{
		Gravity: Vec3{Y: gravity}, FixedTimestep: 1.0 / 2000.0, SolverIter: 40, DisableCCD: true,
	})
	pivot := world.AddBody(BodyConfig{Mass: 0, Position: Vec3{}})
	rod := world.AddBody(BodyConfig{Mass: mass, Position: Vec3{X: length / 2}})
	// A thin box along X carries the rod inertia m L^2 / 12 about its centre.
	rod.AddCollider(ColliderConfig{Shape: ShapeBox, Width: length, Height: 1e-3, Depth: 1e-3})
	world.AddConstraint(&PointConstraint{
		BodyA:   pivot,
		BodyB:   rod,
		AttachA: Vec3{},
		AttachB: Vec3{X: -length / 2},
	})

	for i := 0; i < 20; i++ {
		world.StepFixed()
	}

	elapsed := 20.0 / 2000.0
	want := 3 * gravity / (2 * length)
	got := rod.AngularVelocity.Z / elapsed
	if math.Abs(got-want) > math.Abs(want)*0.05 {
		t.Fatalf("angular acceleration = %v, want %v within 5%%", got, want)
	}
}

// TestBoxDroppedFlatDoesNotRotate pins the negative case. A box that lands
// square on a plane must stay square: four balanced contact impulses produce no
// net torque.
func TestBoxDroppedFlatDoesNotRotate(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIter: 10})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	box := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 2}, Friction: 0.5})
	box.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})

	for i := 0; i < 300; i++ {
		world.StepFixed()
	}

	tilt := math.Hypot(math.Hypot(box.Rotation.X, box.Rotation.Y), box.Rotation.Z)
	if tilt > 1e-3 {
		t.Fatalf("a flat landing must not rotate the box, tilt component = %v", tilt)
	}
	if box.AngularVelocity.Len() > 1e-3 {
		t.Fatalf("box should be still, angular velocity = %+v", box.AngularVelocity)
	}
	if box.Position.Y < 0.49 || box.Position.Y > 0.51 {
		t.Fatalf("box should rest near y=0.5, got %v", box.Position.Y)
	}
}

// TestBoxDroppedOnCornerRotatesOntoItsFace pins the positive case. A box
// balanced on one corner must tip over, and it must come to rest flat.
func TestBoxDroppedOnCornerRotatesOntoItsFace(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 120.0, SolverIter: 12})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})

	// Tip the box about two axes so no corner, edge or face is level.
	tilt := QuatFromAxisAngle(Vec3{Z: 1}, 0.6).Mul(QuatFromAxisAngle(Vec3{X: 1}, 0.5))
	box := world.AddBody(BodyConfig{
		Mass: 1, Position: Vec3{Y: 1.4}, Rotation: tilt, Friction: 0.6, Restitution: 0,
	})
	box.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})

	spun := false
	for i := 0; i < 1200; i++ {
		world.StepFixed()
		if box.AngularVelocity.Len() > 0.5 {
			spun = true
		}
	}

	if !spun {
		t.Fatal("a corner landing must produce angular velocity; the contact carried no torque")
	}
	// A cube at rest on a plane has one axis of its rotation matrix aligned
	// with the world up axis.
	up := box.Rotation.Rotate(Vec3{Y: 1})
	best := math.Max(math.Abs(up.X), math.Max(math.Abs(up.Y), math.Abs(up.Z)))
	if best < 0.995 {
		t.Fatalf("box did not settle on a face, local up in world = %+v", up)
	}
	if box.Position.Y < 0.49 || box.Position.Y > 0.51 {
		t.Fatalf("box should rest with a face down near y=0.5, got %v", box.Position.Y)
	}
	if box.AngularVelocity.Len() > 0.05 {
		t.Fatalf("box should have stopped rotating, angular velocity = %+v", box.AngularVelocity)
	}
}

// TestFrictionAtContactPointSpinsARollingSphere checks that friction acts at
// the contact point and therefore produces torque. A sphere thrown along a
// rough floor with no spin must start to roll, and it must roll forward.
func TestFrictionAtContactPointSpinsARollingSphere(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 240.0, SolverIter: 12})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	ball := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 0.5}, Velocity: Vec3{X: 4}, Friction: 0.4})
	ball.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})

	for i := 0; i < 1200; i++ {
		world.StepFixed()
	}

	// Rolling forward along +X about the -Z axis.
	if ball.AngularVelocity.Z >= -0.1 {
		t.Fatalf("friction must spin the ball forward, angular velocity = %+v", ball.AngularVelocity)
	}
	// Rolling without slipping gives v = omega * r.
	surface := -ball.AngularVelocity.Z * 0.5
	if math.Abs(surface-ball.Velocity.X) > 0.05 {
		t.Fatalf("ball should roll without slipping: v=%v, omega*r=%v", ball.Velocity.X, surface)
	}
	if ball.Velocity.X <= 0 {
		t.Fatalf("ball must keep moving forward, velocity = %+v", ball.Velocity)
	}
}

// --- Rest stability ----------------------------------------------------------

// TestThreeBoxStackStaysStackedOverThousandsOfSteps extends the warm-start stack
// test to rotation. The stack must keep its heights, keep its alignment, and
// stop moving.
func TestThreeBoxStackStaysStackedOverThousandsOfSteps(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIterations: 10})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})

	boxes := make([]*RigidBody, 3)
	for i := range boxes {
		body := world.AddBody(BodyConfig{
			Mass: 1, Position: Vec3{Y: 0.5 + float64(i)}, Friction: 0.6, Restitution: 0,
		})
		body.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})
		boxes[i] = body
	}

	for i := 0; i < 5000; i++ {
		world.StepFixed()
	}

	for i, box := range boxes {
		want := 0.5 + float64(i)
		if math.Abs(box.Position.Y-want) > 0.01 {
			t.Fatalf("box[%d] sank or rose: y = %v, want %v", i, box.Position.Y, want)
		}
		if lateral := math.Hypot(box.Position.X, box.Position.Z); lateral > 0.05 {
			t.Fatalf("box[%d] slid %v sideways", i, lateral)
		}
		tilt := math.Hypot(math.Hypot(box.Rotation.X, box.Rotation.Y), box.Rotation.Z)
		if tilt > 0.01 {
			t.Fatalf("box[%d] tilted, rotation vector part = %v", i, tilt)
		}
		if box.AngularVelocity.Len() > 0.01 {
			t.Fatalf("box[%d] still spinning at %+v", i, box.AngularVelocity)
		}
		if box.Velocity.Len() > 0.01 {
			t.Fatalf("box[%d] still moving at %+v", i, box.Velocity)
		}
	}
}

// TestRestingContactsNeverGainEnergy is the strongest rest invariant available
// without a rest state: the total mechanical energy of a settling stack must
// never rise. A solver that injects energy through its angular terms fails here
// long before the stack visibly jitters.
func TestRestingContactsNeverGainEnergy(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIterations: 10})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})

	boxes := make([]*RigidBody, 3)
	for i := range boxes {
		body := world.AddBody(BodyConfig{
			Mass: 1, Position: Vec3{Y: 0.5 + float64(i)}, Friction: 0.6,
		})
		body.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})
		boxes[i] = body
	}

	energy := func() float64 {
		total := 0.0
		for _, body := range boxes {
			inertia := oracleWorldInertia(t, body)
			total += 0.5 * body.Mass * body.Velocity.Len2()
			total += 0.5 * body.AngularVelocity.Dot(inertia.mul(body.AngularVelocity))
			total += body.Mass * 10 * body.Position.Y
		}
		return total
	}

	// Let the stack settle before measuring, so the initial drop is not counted.
	for i := 0; i < 120; i++ {
		world.StepFixed()
	}
	peak := energy()
	for i := 0; i < 3000; i++ {
		world.StepFixed()
		if now := energy(); now > peak+1e-6 {
			t.Fatalf("step %d: mechanical energy rose from %.9f to %.9f", i, peak, now)
		}
	}
}

// --- Determinism -------------------------------------------------------------

// TestAngularSolverIsDeterministic replays a tumbling scene twice and demands
// bit-identical results. Replay is one of this engine's promises, and angular
// state is new surface where a map iteration or a pointer compare could break
// it.
func TestAngularSolverIsDeterministic(t *testing.T) {
	build := func() ([]*RigidBody, *World) {
		world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIterations: 10})
		world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
		rng := rand.New(rand.NewSource(99))
		bodies := make([]*RigidBody, 0, 12)
		for i := 0; i < 12; i++ {
			body := world.AddBody(BodyConfig{
				Mass: 1 + rng.Float64(),
				Position: Vec3{
					X: (rng.Float64() - 0.5) * 3,
					Y: 1 + float64(i)*0.8,
					Z: (rng.Float64() - 0.5) * 3,
				},
				Rotation:        randomUnitQuat(rng),
				Velocity:        Vec3{X: rng.Float64() - 0.5, Z: rng.Float64() - 0.5},
				AngularVelocity: Vec3{X: rng.Float64() - 0.5, Y: rng.Float64() - 0.5, Z: rng.Float64() - 0.5},
				Friction:        0.4,
				Restitution:     0.2,
			})
			switch i % 3 {
			case 0:
				body.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 0.8, Height: 0.8, Depth: 0.8})
			case 1:
				body.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.4})
			default:
				body.AddCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 0.35, Height: 0.7})
			}
			bodies = append(bodies, body)
		}
		return bodies, world
	}

	run := func() []BodyState {
		bodies, world := build()
		for i := 0; i < 600; i++ {
			world.StepFixed()
		}
		out := make([]BodyState, len(bodies))
		for i, body := range bodies {
			out[i] = BodyState{
				Position:        body.Position,
				Rotation:        body.Rotation,
				Velocity:        body.Velocity,
				AngularVelocity: body.AngularVelocity,
			}
		}
		return out
	}

	first := run()
	second := run()
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("body %d diverged between runs:\n first: %+v\nsecond: %+v", i, first[i], second[i])
		}
	}
}

// --- Inertia -----------------------------------------------------------------

// TestInertiaTensorMatchesTextbookValues checks the derived tensor against the
// closed form for every primitive shape.
func TestInertiaTensorMatchesTextbookValues(t *testing.T) {
	const mass = 3.0
	cases := []struct {
		name   string
		config ColliderConfig
		want   Vec3
		rel    float64
	}{
		{
			name:   "sphere",
			config: ColliderConfig{Shape: ShapeSphere, Radius: 0.7},
			want:   Vec3{X: 0.4 * mass * 0.49, Y: 0.4 * mass * 0.49, Z: 0.4 * mass * 0.49},
			rel:    1e-12,
		},
		{
			name:   "box",
			config: ColliderConfig{Shape: ShapeBox, Width: 1, Height: 2, Depth: 3},
			want: Vec3{
				X: mass * (4 + 9) / 12,
				Y: mass * (1 + 9) / 12,
				Z: mass * (1 + 4) / 12,
			},
			rel: 1e-12,
		},
		{
			name:   "cylinder",
			config: ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 2},
			want: Vec3{
				X: mass * (3*0.25 + 4) / 12,
				Y: 0.5 * mass * 0.25,
				Z: mass * (3*0.25 + 4) / 12,
			},
			rel: 1e-12,
		},
		{
			name:   "cone",
			config: ColliderConfig{Shape: ShapeCone, Radius: 0.5, Height: 2},
			want: Vec3{
				X: 0.15*mass*0.25 + mass*4/10,
				Y: 0.3 * mass * 0.25,
				Z: 0.15*mass*0.25 + mass*4/10,
			},
			rel: 1e-12,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			body := NewRigidBody(BodyConfig{Mass: mass})
			body.AddCollider(testCase.config)
			got := body.Inertia()
			for _, pair := range [][3]float64{
				{got.X, testCase.want.X, 0},
				{got.Y, testCase.want.Y, 1},
				{got.Z, testCase.want.Z, 2},
			} {
				tolerance := math.Abs(pair[1])*testCase.rel + 1e-12
				if math.Abs(pair[0]-pair[1]) > tolerance {
					t.Fatalf("axis %v: inertia %v, want %v", pair[2], pair[0], pair[1])
				}
			}
		})
	}
}

// TestCapsuleInertiaSitsBetweenCylinderAndSphere checks the composite capsule
// formula against bounds that need no algebra: a capsule is heavier to spin than
// its cylinder and lighter than the cylinder that encloses it.
func TestCapsuleInertiaSitsBetweenCylinderAndSphere(t *testing.T) {
	const mass = 2.0
	const radius = 0.4
	const height = 1.2

	capsule := NewRigidBody(BodyConfig{Mass: mass})
	capsule.AddCollider(ColliderConfig{Shape: ShapeCapsule, Radius: radius, Height: height})
	got := capsule.Inertia()

	innerAxial := 0.5 * mass * radius * radius
	outerAxial := 0.5 * mass * radius * radius * 1.0001
	if got.Y < innerAxial*0.5 || got.Y > outerAxial {
		t.Fatalf("capsule axial inertia %v is outside the cylinder and sphere bounds", got.Y)
	}
	shortCylinder := mass * (3*radius*radius + height*height) / 12
	longCylinder := mass * (3*radius*radius + (height+2*radius)*(height+2*radius)) / 12
	if got.X < shortCylinder || got.X > longCylinder {
		t.Fatalf("capsule radial inertia %v is not between %v and %v", got.X, shortCylinder, longCylinder)
	}
	if math.Abs(got.X-got.Z) > 1e-12 {
		t.Fatalf("a capsule is symmetric about its axis, got X=%v Z=%v", got.X, got.Z)
	}
}

// TestOffsetColliderShiftsInertiaByParallelAxis checks the parallel axis term.
// A sphere held away from the body origin must be harder to spin by exactly
// m * d^2 about the axes perpendicular to the offset.
func TestOffsetColliderShiftsInertiaByParallelAxis(t *testing.T) {
	const mass = 1.5
	const radius = 0.3
	const offset = 2.0

	centred := NewRigidBody(BodyConfig{Mass: mass})
	centred.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: radius})
	shifted := NewRigidBody(BodyConfig{Mass: mass})
	shifted.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: radius, Offset: Vec3{X: offset}})

	base := centred.Inertia()
	moved := shifted.Inertia()
	want := mass * offset * offset

	if math.Abs(moved.X-base.X) > 1e-9 {
		t.Fatalf("the offset axis must not change: %v vs %v", moved.X, base.X)
	}
	if math.Abs((moved.Y-base.Y)-want) > 1e-9 {
		t.Fatalf("Y inertia grew by %v, want %v", moved.Y-base.Y, want)
	}
	if math.Abs((moved.Z-base.Z)-want) > 1e-9 {
		t.Fatalf("Z inertia grew by %v, want %v", moved.Z-base.Z, want)
	}
}

// TestLockRotationRefusesEveryAngularImpulse checks the escape hatch a
// character controller needs.
func TestLockRotationRefusesEveryAngularImpulse(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIter: 8})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	box := world.AddBody(BodyConfig{
		Mass: 1, Position: Vec3{Y: 1.4}, Friction: 0.5, LockRotation: true,
		Rotation: QuatFromAxisAngle(Vec3{Z: 1}, 0.6),
	})
	box.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})

	start := box.Rotation
	box.ApplyTorque(Vec3{Z: 50})
	box.ApplyImpulse(Vec3{Y: 20}, Vec3{X: 3, Y: 1.4})
	for i := 0; i < 300; i++ {
		world.StepFixed()
	}

	if box.AngularVelocity.Len() > 1e-12 {
		t.Fatalf("a rotation-locked body must never spin, got %+v", box.AngularVelocity)
	}
	if box.Rotation != start {
		t.Fatalf("rotation changed from %+v to %+v", start, box.Rotation)
	}
}

// TestInertiaOverrideReplacesTheDerivedTensor checks that a caller can model a
// mass distribution the shape does not describe.
func TestInertiaOverrideReplacesTheDerivedTensor(t *testing.T) {
	body := NewRigidBody(BodyConfig{Mass: 1, Inertia: Vec3{X: 5, Y: 6, Z: 7}})
	body.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 1})
	got := body.Inertia()
	assertVecNear(t, "override inertia", got, Vec3{X: 5, Y: 6, Z: 7}, 1e-12)

	body.SetInertia(Vec3{})
	derived := body.Inertia()
	assertVecNear(t, "derived inertia", derived, Vec3{X: 0.4, Y: 0.4, Z: 0.4}, 1e-12)
}
