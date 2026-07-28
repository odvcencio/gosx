// Package hubrunner drives a physics world from a gosx hub through the
// server-authoritative sim runner.
//
// The package exists to keep the physics engine free of transport code. The
// physics package imports only the Go standard library, so it can ship on its
// own or run in a client build that has no hub and no sim. This package holds
// the one edge that joins the two.
//
// Why an adapter and not an interface inside physics: sim.Simulation demands
// the method Tick(map[string]sim.Input). Go compares interface method sets by
// exact type, and map[string]physics.Input is a different type from
// map[string]sim.Input even though the element structs match. No interface that
// physics declares can therefore make *physics.World satisfy sim.Simulation. A
// value that owns both types must exist, and Simulation below is that value.
package hubrunner

import (
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/physics"
	"m31labs.dev/gosx/sim"
)

// Simulation adapts a physics world to the sim.Simulation interface.
//
// Every method forwards to the world. The only work the adapter does is convert
// the input map, because that conversion is the reason the adapter exists.
type Simulation struct {
	World *physics.World
}

// New returns a Simulation for world. A nil world gets a default world, which
// matches what the runner did before physics and sim were separated.
func New(world *physics.World) *Simulation {
	if world == nil {
		world = physics.NewWorld(physics.WorldConfig{})
	}
	return &Simulation{World: world}
}

// Tick applies the collected inputs and advances the world one fixed step.
func (s *Simulation) Tick(inputs map[string]sim.Input) {
	if s == nil || s.World == nil {
		return
	}
	s.World.Tick(convertInputs(inputs))
}

// Snapshot returns an opaque physics checkpoint for rollback or replay.
func (s *Simulation) Snapshot() []byte {
	if s == nil || s.World == nil {
		return nil
	}
	return s.World.Snapshot()
}

// Restore resets the world to a previous snapshot.
func (s *Simulation) Restore(snapshot []byte) {
	if s == nil || s.World == nil {
		return
	}
	s.World.Restore(snapshot)
}

// State returns the current authoritative world state for broadcast.
func (s *Simulation) State() []byte {
	if s == nil || s.World == nil {
		return nil
	}
	return s.World.State()
}

// NewRunner wires a physics world into the server-authoritative sim runner.
// Leave opts.TickRate zero to take the rate from the world's fixed timestep.
func NewRunner(h *hub.Hub, world *physics.World, opts sim.Options) *sim.Runner {
	simulation := New(world)
	if opts.TickRate <= 0 {
		opts.TickRate = simulation.World.TickRate()
	}
	return sim.New(h, simulation, opts)
}

// convertInputs copies a sim input map into the physics input type. The byte
// slices are shared, because sim.Runner already hands out private copies.
func convertInputs(inputs map[string]sim.Input) map[string]physics.Input {
	if len(inputs) == 0 {
		return nil
	}
	out := make(map[string]physics.Input, len(inputs))
	for playerID, input := range inputs {
		out[playerID] = physics.Input{Data: input.Data}
	}
	return out
}
