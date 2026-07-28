package hubrunner

import (
	"testing"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/physics"
	"m31labs.dev/gosx/sim"
)

// TestSimulationSatisfiesSimInterface is the compile-time proof that the
// adapter closes the gap physics can no longer close on its own.
func TestSimulationSatisfiesSimInterface(t *testing.T) {
	var _ sim.Simulation = (*Simulation)(nil)
}

func TestNewRunnerUsesWorldFixedTimestep(t *testing.T) {
	world := physics.NewWorld(physics.WorldConfig{Gravity: physics.Vec3{}, FixedTimestep: 0.25, SolverIter: 1})
	runner := NewRunner(hub.New("physics-runner-test"), world, sim.Options{})
	if runner.TickRate() != 4 {
		t.Fatalf("TickRate() = %d, want 4", runner.TickRate())
	}
}

func TestNewRunnerHonoursExplicitTickRate(t *testing.T) {
	world := physics.NewWorld(physics.WorldConfig{Gravity: physics.Vec3{}, FixedTimestep: 0.25, SolverIter: 1})
	runner := NewRunner(hub.New("physics-runner-explicit"), world, sim.Options{TickRate: 30})
	if runner.TickRate() != 30 {
		t.Fatalf("TickRate() = %d, want 30", runner.TickRate())
	}
}

func TestNewRunnerAcceptsNilWorld(t *testing.T) {
	runner := NewRunner(hub.New("physics-runner-nil"), nil, sim.Options{})
	if runner.TickRate() != 60 {
		t.Fatalf("TickRate() = %d, want 60", runner.TickRate())
	}
}

// TestSimulationTickConvertsInputs proves the adapter really carries the payload
// through to the world. A conversion that dropped the map would leave the body
// at rest, so the velocity check is the load-bearing assertion.
func TestSimulationTickConvertsInputs(t *testing.T) {
	world := physics.NewWorld(physics.WorldConfig{Gravity: physics.Vec3{}, FixedTimestep: 0.5, SolverIter: 1})
	body := world.AddBody(physics.BodyConfig{ID: "ball", Mass: 2})
	simulation := New(world)

	simulation.Tick(map[string]sim.Input{
		"player-1": {Data: []byte(`{"type":"impulse","bodyID":"ball","impulse":{"x":4}}`)},
	})

	if !body.Velocity.Near(physics.Vec3{X: 2}, 1e-12) {
		t.Fatalf("velocity after impulse = %+v", body.Velocity)
	}
	if !body.Position.Near(physics.Vec3{X: 1}, 1e-12) {
		t.Fatalf("position after impulse tick = %+v", body.Position)
	}
}

func TestSimulationSnapshotRestoreRoundTrips(t *testing.T) {
	world := physics.NewWorld(physics.WorldConfig{Gravity: physics.Vec3{}, FixedTimestep: 0.5, SolverIter: 1})
	body := world.AddBody(physics.BodyConfig{ID: "ball", Mass: 1, Velocity: physics.Vec3{X: 2}})
	simulation := New(world)

	simulation.Tick(nil)
	snapshot := simulation.Snapshot()
	if len(snapshot) == 0 {
		t.Fatal("Snapshot() returned no bytes")
	}
	if len(simulation.State()) == 0 {
		t.Fatal("State() returned no bytes")
	}

	body.Position = physics.Vec3{X: 42}
	body.Velocity = physics.Vec3{}
	simulation.Restore(snapshot)

	if !body.Position.Near(physics.Vec3{X: 1}, 1e-12) {
		t.Fatalf("restored position = %+v", body.Position)
	}
	if !body.Velocity.Near(physics.Vec3{X: 2}, 1e-12) {
		t.Fatalf("restored velocity = %+v", body.Velocity)
	}
}

// TestSimulationToleratesNilReceiverAndWorld guards the runner against a
// half-built adapter, because sim.Runner calls every method on every tick.
func TestSimulationToleratesNilReceiverAndWorld(t *testing.T) {
	var nilSim *Simulation
	nilSim.Tick(map[string]sim.Input{"p": {Data: []byte("{}")}})
	if nilSim.Snapshot() != nil || nilSim.State() != nil {
		t.Fatal("nil Simulation should produce nil bytes")
	}
	nilSim.Restore([]byte(`{"bodies":[]}`))

	empty := &Simulation{}
	empty.Tick(nil)
	if empty.Snapshot() != nil || empty.State() != nil {
		t.Fatal("Simulation with nil World should produce nil bytes")
	}
	empty.Restore([]byte(`{"bodies":[]}`))
}

func TestConvertInputsPreservesEveryPlayer(t *testing.T) {
	in := map[string]sim.Input{
		"a": {Data: []byte("1")},
		"b": {Data: []byte("2")},
		"c": {Data: []byte("3")},
	}
	out := convertInputs(in)
	if len(out) != len(in) {
		t.Fatalf("convertInputs kept %d of %d players", len(out), len(in))
	}
	for playerID, input := range in {
		if string(out[playerID].Data) != string(input.Data) {
			t.Fatalf("player %q payload = %q, want %q", playerID, out[playerID].Data, input.Data)
		}
	}
	if convertInputs(nil) != nil {
		t.Fatal("convertInputs(nil) should return nil")
	}
}
