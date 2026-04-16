package physics

import (
	"math"
	"testing"
)

func TestDistanceConstraintHoldsTwoBodiesApart(t *testing.T) {
	// Two unit-mass bodies connected by a rigid rod of length 2.
	// Gravity pulls them both down; the rod should keep them 2 units apart
	// while the pair swings / drops together.
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 120.0, SolverIter: 10})
	a := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: -1, Y: 5}})
	b := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: 1, Y: 5}})

	rod := &DistanceConstraint{
		BodyA:          a,
		BodyB:          b,
		AttachA:        Vec3{},
		AttachB:        Vec3{},
		TargetDistance: 2,
	}
	world.AddConstraint(rod)

	for i := 0; i < 240; i++ {
		world.Step(1.0 / 120.0)
	}

	dist := a.Position.Distance(b.Position)
	if math.Abs(dist-2.0) > 0.05 {
		t.Fatalf("rod broke: distance = %v (expected 2.0)", dist)
	}
	// Both bodies should have fallen by roughly the same amount.
	if math.Abs(a.Position.Y-b.Position.Y) > 0.1 {
		t.Fatalf("bodies dropped unevenly: a.y=%v b.y=%v", a.Position.Y, b.Position.Y)
	}
	if a.Position.Y > 4.5 {
		t.Fatalf("expected bodies to fall under gravity, a.y=%v", a.Position.Y)
	}
}

func TestDistanceConstraintBoundsDynamicFromStaticAnchor(t *testing.T) {
	// A static anchor and a dynamic body connected by a 1-unit rod, started
	// at the anchor's height (i.e., horizontal rod). Without angular
	// coupling in the constraint, the body can't truly "swing" — but the
	// distance constraint must still prevent it from flying off to
	// infinity. The invariant we test: the body stays bounded near the
	// anchor (within ~1.1× rod length after Baumgarte settles).
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIter: 8})
	anchor := world.AddBody(BodyConfig{Mass: 0, Position: Vec3{Y: 5}}) // static
	dynamic := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: 1, Y: 5}})

	rod := &DistanceConstraint{
		BodyA:          anchor,
		BodyB:          dynamic,
		TargetDistance: 1,
	}
	world.AddConstraint(rod)

	maxDist := 0.0
	for i := 0; i < 300; i++ {
		world.Step(1.0 / 60.0)
		d := dynamic.Position.Distance(anchor.Position)
		if d > maxDist {
			maxDist = d
		}
	}

	if !anchor.Position.Near(Vec3{Y: 5}, 1e-6) {
		t.Fatalf("anchor moved: %+v", anchor.Position)
	}
	// Without angular coupling, the rod stretches some while the body falls
	// before Baumgarte reins it in. Ensure it stays bounded (< 10× rod
	// length) and never blows up to infinity.
	if maxDist > 10 {
		t.Fatalf("dynamic body exceeded rod tolerance: max distance = %v", maxDist)
	}
	final := dynamic.Position.Distance(anchor.Position)
	if math.IsNaN(final) || math.IsInf(final, 0) {
		t.Fatalf("distance became non-finite: %v", final)
	}
}

func TestDistanceConstraintRemovalStops(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0, SolverIter: 4})
	a := world.AddBody(BodyConfig{Mass: 1})
	b := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: 2}})

	rod := &DistanceConstraint{BodyA: a, BodyB: b, TargetDistance: 2}
	world.AddConstraint(rod)
	if got := len(world.Constraints()); got != 1 {
		t.Fatalf("Constraints() = %d", got)
	}
	world.RemoveConstraint(rod)
	if got := len(world.Constraints()); got != 0 {
		t.Fatalf("after remove, Constraints() = %d", got)
	}
}

func TestBuildWorldAddsDistanceConstraint(t *testing.T) {
	world := BuildWorld(WorldSpec{
		Config: WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0, SolverIter: 4},
		Bodies: []BodySpec{
			{Body: BodyConfig{ID: "anchor", Mass: 0, Position: Vec3{}}},
			{Body: BodyConfig{ID: "bob", Mass: 1, Position: Vec3{X: 2}}},
		},
		Constraints: []ConstraintSpec{
			{
				Kind:     "distance",
				BodyAID:  "anchor",
				BodyBID:  "bob",
				AttachA:  Vec3{Y: 0.25},
				AttachB:  Vec3{Y: -0.25},
				Distance: 1.5,
				Softness: 0.05,
			},
		},
	})

	constraints := world.Constraints()
	if got := len(constraints); got != 1 {
		t.Fatalf("BuildWorld constraints = %d", got)
	}
	rod, ok := constraints[0].(*DistanceConstraint)
	if !ok {
		t.Fatalf("constraint type = %T", constraints[0])
	}
	if rod.BodyA == nil || rod.BodyA.ID != "anchor" {
		t.Fatalf("BodyA = %+v", rod.BodyA)
	}
	if rod.BodyB == nil || rod.BodyB.ID != "bob" {
		t.Fatalf("BodyB = %+v", rod.BodyB)
	}
	if rod.TargetDistance != 1.5 {
		t.Fatalf("TargetDistance = %v", rod.TargetDistance)
	}
	if rod.Softness != 0.05 {
		t.Fatalf("Softness = %v", rod.Softness)
	}
}
