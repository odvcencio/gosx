package physics

import (
	"math"
	"testing"

	"github.com/odvcencio/gosx/hub"
	"github.com/odvcencio/gosx/sim"
)

func TestWorldFixedTimestepAccumulator(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 0.1, SolverIter: 1})
	body := world.AddBody(BodyConfig{Mass: 1, Velocity: Vec3{X: 1}})

	if steps := world.Step(0.05); steps != 0 {
		t.Fatalf("Step(0.05) = %d", steps)
	}
	if !body.Position.Near(Vec3{}, 1e-12) {
		t.Fatalf("body moved before fixed step: %+v", body.Position)
	}

	if steps := world.Step(0.05); steps != 1 {
		t.Fatalf("second Step(0.05) = %d", steps)
	}
	if !body.Position.Near(Vec3{X: 0.1}, 1e-12) {
		t.Fatalf("body position = %+v", body.Position)
	}
}

func TestWorldIntegratesGravitySemiImplicit(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 0.5, SolverIter: 1})
	body := world.AddBody(BodyConfig{Mass: 2})

	if steps := world.Step(0.5); steps != 1 {
		t.Fatalf("Step() = %d", steps)
	}
	if !body.Velocity.Near(Vec3{Y: -5}, 1e-12) {
		t.Fatalf("velocity = %+v", body.Velocity)
	}
	if !body.Position.Near(Vec3{Y: -2.5}, 1e-12) {
		t.Fatalf("position = %+v", body.Position)
	}
}

func TestWorldSphereRestsOnPlane(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIter: 8})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	sphere := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 2}, Restitution: 0})
	sphere.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})

	for i := 0; i < 180; i++ {
		world.Step(1.0 / 60.0)
	}

	if sphere.Position.Y < 0.499 {
		t.Fatalf("sphere penetrated plane: y=%v", sphere.Position.Y)
	}
	if math.Abs(sphere.Velocity.Y) > 0.25 {
		t.Fatalf("sphere should be near rest, velocity=%+v", sphere.Velocity)
	}
}

func TestWorldSeparatesOverlappingSpheres(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0, SolverIter: 4})
	a := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: -0.45}})
	a.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})
	b := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: 0.45}})
	b.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})

	for i := 0; i < 8; i++ {
		world.Step(1.0 / 60.0)
	}

	distance := a.Position.Distance(b.Position)
	if distance < 0.995 {
		t.Fatalf("spheres still overlap too much: distance=%v a=%+v b=%+v", distance, a.Position, b.Position)
	}
}

func TestWorldSeparatesSphereFromBox(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0, SolverIter: 4})
	box := world.AddBody(BodyConfig{Mass: 0})
	box.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 2, Height: 2, Depth: 2})
	sphere := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 1.4}})
	sphere.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})

	for i := 0; i < 8; i++ {
		world.Step(1.0 / 60.0)
	}

	if sphere.Position.Y < 1.495 {
		t.Fatalf("sphere was not pushed out of box: y=%v", sphere.Position.Y)
	}
}

func TestWorldBoxStacksOnPlane(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIter: 8})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	box := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 3}, Restitution: 0})
	box.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})

	for i := 0; i < 240; i++ {
		world.Step(1.0 / 60.0)
	}

	if box.Position.Y < 0.45 || box.Position.Y > 0.55 {
		t.Fatalf("box should rest with center near y=0.5, got y=%v", box.Position.Y)
	}
	if math.Abs(box.Velocity.Y) > 0.2 {
		t.Fatalf("box should be near rest, velocity=%+v", box.Velocity)
	}
}

func TestWorldThreeBoxStackWarmStarts(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIter: 10})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})

	heights := []float64{3, 5, 7}
	boxes := make([]*RigidBody, 3)
	for i, y := range heights {
		body := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: y}, Restitution: 0, Friction: 0.5})
		body.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})
		boxes[i] = body
	}

	for i := 0; i < 600; i++ {
		world.Step(1.0 / 60.0)
	}

	// Stack settles with 1-unit spacing; some residual compression is accepted.
	want := []float64{0.5, 1.5, 2.5}
	for i, box := range boxes {
		if box.Position.Y < want[i]-0.15 || box.Position.Y > want[i]+0.15 {
			t.Fatalf("box[%d] Y = %v, want near %v", i, box.Position.Y, want[i])
		}
		if math.Abs(box.Velocity.Y) > 0.4 {
			t.Fatalf("box[%d] should be near rest, velocity=%+v", i, box.Velocity)
		}
	}

	// Warm-start cache should have at least one entry per stacked contact.
	if len(world.contactCache) < 2 {
		t.Fatalf("expected warm-start cache to retain stack contacts, got %d entries", len(world.contactCache))
	}
	var nonZero int
	for _, cm := range world.contactCache {
		for i := 0; i < cm.Count; i++ {
			if cm.Points[i].NormalImpulse > 0 {
				nonZero++
			}
		}
	}
	if nonZero == 0 {
		t.Fatal("expected warm-start cache to carry nonzero normal impulses")
	}
}

func TestWorldWarmStartDisabledSkipsCache(t *testing.T) {
	world := NewWorld(WorldConfig{
		Gravity:          Vec3{Y: -10},
		FixedTimestep:    1.0 / 60.0,
		SolverIter:       8,
		DisableWarmStart: true,
	})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	box := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 2}})
	box.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})

	for i := 0; i < 180; i++ {
		world.Step(1.0 / 60.0)
	}

	if world.contactCache != nil && len(world.contactCache) != 0 {
		t.Fatalf("expected empty cache when warm-start disabled, got %d entries", len(world.contactCache))
	}
}

func TestWorldTwoBoxesStackOnPlane(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIter: 10})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})

	bottom := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 3}, Restitution: 0, Friction: 0.5})
	bottom.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})

	top := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 5}, Restitution: 0, Friction: 0.5})
	top.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})

	for i := 0; i < 480; i++ {
		world.Step(1.0 / 60.0)
	}

	if bottom.Position.Y < 0.45 || bottom.Position.Y > 0.6 {
		t.Fatalf("bottom box should rest near y=0.5, got y=%v", bottom.Position.Y)
	}
	if top.Position.Y < 1.4 || top.Position.Y > 1.65 {
		t.Fatalf("top box should stack near y=1.5, got y=%v", top.Position.Y)
	}
	if math.Abs(top.Velocity.Y) > 0.3 {
		t.Fatalf("top box should approach rest, got velocity=%+v", top.Velocity)
	}
}

func TestWorldImplementsSimSimulation(t *testing.T) {
	var _ sim.Simulation = (*World)(nil)
}

func TestNewRunnerUsesWorldFixedTimestep(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 0.25, SolverIter: 1})
	runner := NewRunner(hub.New("physics-runner-test"), world, sim.Options{})
	if runner.TickRate() != 4 {
		t.Fatalf("TickRate() = %d, want 4", runner.TickRate())
	}
}

func TestWorldTickAndSnapshotRestore(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 0.5, SolverIter: 1})
	body := world.AddBody(BodyConfig{ID: "ball", Mass: 1, Velocity: Vec3{X: 2}})

	world.Tick(nil)
	if !body.Position.Near(Vec3{X: 1}, 1e-12) {
		t.Fatalf("position after Tick = %+v", body.Position)
	}

	snapshot := world.Snapshot()
	body.Position = Vec3{X: 42}
	body.Velocity = Vec3{}
	world.Restore(snapshot)

	if !body.Position.Near(Vec3{X: 1}, 1e-12) {
		t.Fatalf("restored position = %+v", body.Position)
	}
	if !body.Velocity.Near(Vec3{X: 2}, 1e-12) {
		t.Fatalf("restored velocity = %+v", body.Velocity)
	}
}

func TestWorldTickAppliesRunnerInputCommands(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 0.5, SolverIter: 1})
	body := world.AddBody(BodyConfig{ID: "ball", Mass: 2})

	world.Tick(map[string]sim.Input{
		"player-1": {Data: []byte(`{"type":"impulse","bodyID":"ball","impulse":{"x":4}}`)},
	})

	if !body.Velocity.Near(Vec3{X: 2}, 1e-12) {
		t.Fatalf("velocity after impulse = %+v", body.Velocity)
	}
	if !body.Position.Near(Vec3{X: 1}, 1e-12) {
		t.Fatalf("position after impulse tick = %+v", body.Position)
	}

	world.Tick(map[string]sim.Input{
		"player-1": {Data: []byte(`[{"type":"force","bodyID":"ball","force":[4,0,0]},{"type":"torque","bodyID":"ball","torque":{"z":2}}]`)},
	})

	if !body.Velocity.Near(Vec3{X: 3}, 1e-12) {
		t.Fatalf("velocity after force = %+v", body.Velocity)
	}
	if body.AngularVelocity.Z <= 0 {
		t.Fatalf("expected torque input to affect angular velocity, got %+v", body.AngularVelocity)
	}
}
