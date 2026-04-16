package scene

import (
	"math"
	"testing"

	"github.com/odvcencio/gosx/physics"
)

func TestPropsPhysicsIRIsOmittedWhenUnused(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Mesh{ID: "inert", Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1}},
		),
	}
	ir := props.CanonicalIR()
	if ir.Physics != nil {
		t.Fatalf("expected no physics IR, got %+v", ir.Physics)
	}
}

func TestPropsPhysicsIRCapturesConfigAndBody(t *testing.T) {
	props := Props{
		Physics: PhysicsWorld{
			Gravity:          Vector3{Y: -9.81},
			FixedTimestep:    1.0 / 120.0,
			SolverIterations: 10,
			BroadphaseCell:   3,
			Colliders: []Collider3D{
				{Shape: "plane", Normal: Vector3{Y: 1}},
			},
		},
		Graph: NewGraph(
			Mesh{
				ID:       "crate",
				Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
				Position: Vector3{Y: 5},
				RigidBody: &RigidBody3D{
					Mass:        1,
					Restitution: 0.2,
					Friction:    0.6,
					Colliders: []Collider3D{
						{Shape: "box", Width: 1, Height: 1, Depth: 1},
					},
				},
			},
		),
	}

	ir := props.CanonicalIR()
	if ir.Physics == nil {
		t.Fatal("expected physics IR")
	}
	p := ir.Physics
	if p.FixedTimestep != 1.0/120.0 {
		t.Fatalf("fixedTimestep = %v", p.FixedTimestep)
	}
	if p.SolverIterations != 10 {
		t.Fatalf("solverIterations = %d", p.SolverIterations)
	}
	if p.Gravity.Y != -9.81 {
		t.Fatalf("gravity.Y = %v", p.Gravity.Y)
	}
	if len(p.Bodies) != 1 {
		t.Fatalf("expected 1 body, got %d", len(p.Bodies))
	}
	body := p.Bodies[0]
	if body.ID != "crate" || body.Mass != 1 || body.Friction != 0.6 {
		t.Fatalf("body fields wrong: %+v", body)
	}
	if body.Position.Y != 5 {
		t.Fatalf("body.Position.Y = %v", body.Position.Y)
	}
	if len(body.Colliders) != 1 || body.Colliders[0].Shape != "box" {
		t.Fatalf("body colliders wrong: %+v", body.Colliders)
	}
	if len(p.Static) != 1 || p.Static[0].Shape != "plane" {
		t.Fatalf("static colliders wrong: %+v", p.Static)
	}
}

func TestIRPhysicsSpecBuildsRunnableWorld(t *testing.T) {
	props := Props{
		Physics: PhysicsWorld{
			Gravity:          Vector3{Y: -10},
			FixedTimestep:    1.0 / 60.0,
			SolverIterations: 8,
			Colliders: []Collider3D{
				{Shape: "plane", Normal: Vector3{Y: 1}},
			},
		},
		Graph: NewGraph(
			Mesh{
				ID:       "cube",
				Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
				Position: Vector3{Y: 3},
				RigidBody: &RigidBody3D{
					Mass: 1,
					Colliders: []Collider3D{
						{Shape: "box", Width: 1, Height: 1, Depth: 1},
					},
				},
			},
		),
	}

	ir := props.CanonicalIR()
	spec := ir.PhysicsSpec()
	if len(spec.Bodies) != 1 {
		t.Fatalf("spec bodies = %d", len(spec.Bodies))
	}
	if len(spec.Static) != 1 {
		t.Fatalf("spec static = %d", len(spec.Static))
	}
	if spec.Config.Gravity.Y != -10 {
		t.Fatalf("spec gravity.Y = %v", spec.Config.Gravity.Y)
	}

	world := physics.BuildWorld(spec)
	if world == nil {
		t.Fatal("BuildWorld returned nil")
	}
	if len(world.Bodies()) != 1 {
		t.Fatalf("world has %d bodies", len(world.Bodies()))
	}
	if len(world.Colliders()) < 2 {
		t.Fatalf("world should have body+static colliders, got %d", len(world.Colliders()))
	}

	// Step the world for 2 seconds; the cube should settle near y=0.5.
	for i := 0; i < 240; i++ {
		world.Step(1.0 / 60.0)
	}
	body := world.Bodies()[0]
	if body.ID != "cube" {
		t.Fatalf("expected body ID 'cube', got %q", body.ID)
	}
	if body.Position.Y < 0.4 || body.Position.Y > 0.65 {
		t.Fatalf("body did not settle near y=0.5 (got %v)", body.Position.Y)
	}
	if math.Abs(body.Velocity.Y) > 0.3 {
		t.Fatalf("body still moving: velocity=%+v", body.Velocity)
	}
}

func TestIRPhysicsSpecCarriesDistanceConstraint(t *testing.T) {
	props := Props{
		Physics: PhysicsWorld{
			Constraints: []Constraint3D{
				{
					Kind:     "distance",
					BodyA:    "anchor",
					BodyB:    "bob",
					AttachA:  Vector3{Y: 0.25},
					AttachB:  Vector3{Y: -0.25},
					Distance: 2,
					Softness: 0.1,
				},
			},
		},
		Graph: NewGraph(
			Mesh{
				ID:       "anchor",
				Geometry: SphereGeometry{Radius: 0.25},
				RigidBody: &RigidBody3D{
					Static: true,
				},
			},
			Mesh{
				ID:       "bob",
				Geometry: SphereGeometry{Radius: 0.25},
				Position: Vector3{X: 2},
				RigidBody: &RigidBody3D{
					Mass: 1,
				},
			},
		),
	}

	ir := props.CanonicalIR()
	if ir.Physics == nil {
		t.Fatal("expected physics IR")
	}
	if got := len(ir.Physics.Constraints); got != 1 {
		t.Fatalf("IR constraints = %d", got)
	}
	if ir.Physics.Constraints[0].BodyA != "anchor" || ir.Physics.Constraints[0].BodyB != "bob" {
		t.Fatalf("IR constraint bodies wrong: %+v", ir.Physics.Constraints[0])
	}

	spec := ir.PhysicsSpec()
	if got := len(spec.Constraints); got != 1 {
		t.Fatalf("spec constraints = %d", got)
	}
	constraint := spec.Constraints[0]
	if constraint.BodyAID != "anchor" || constraint.BodyBID != "bob" {
		t.Fatalf("spec constraint bodies wrong: %+v", constraint)
	}
	if constraint.AttachA.Y != 0.25 || constraint.AttachB.Y != -0.25 {
		t.Fatalf("spec attach points wrong: %+v", constraint)
	}
	if constraint.Distance != 2 || constraint.Softness != 0.1 {
		t.Fatalf("spec constraint values wrong: %+v", constraint)
	}

	world := physics.BuildWorld(spec)
	constraints := world.Constraints()
	if got := len(constraints); got != 1 {
		t.Fatalf("world constraints = %d", got)
	}
	rod, ok := constraints[0].(*physics.DistanceConstraint)
	if !ok {
		t.Fatalf("world constraint type = %T", constraints[0])
	}
	if rod.BodyA.ID != "anchor" || rod.BodyB.ID != "bob" {
		t.Fatalf("world constraint bodies wrong: A=%q B=%q", rod.BodyA.ID, rod.BodyB.ID)
	}
}

func TestIRPhysicsTopicFallsBackToDefault(t *testing.T) {
	ir := IR{Physics: &IRPhysics{}}
	if got := ir.PhysicsTopic(""); got != "scene3d:physics" {
		t.Fatalf("default topic wrong: %q", got)
	}
	if got := ir.PhysicsTopic("room-42"); got != "scene3d:physics:room-42" {
		t.Fatalf("scoped topic wrong: %q", got)
	}
	ir.Physics.Topic = "custom.feed"
	if got := ir.PhysicsTopic("ignored"); got != "custom.feed" {
		t.Fatalf("custom topic wrong: %q", got)
	}
	var empty IR
	if got := empty.PhysicsTopic("room-42"); got != "" {
		t.Fatalf("no-physics topic should be empty, got %q", got)
	}
}
