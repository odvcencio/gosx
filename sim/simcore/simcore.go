// Package simcore holds the parts of package sim that need no hub: the
// Simulation contract, the per-player Input a tick receives, and deterministic
// input ordering. Package sim re-exports every name here, so server code keeps
// using sim.Input and sim.Simulation.
//
// Browser (js/wasm) builds import simcore instead of sim, because sim's Runner
// links the hub server and with it net/http and crypto/tls. game.Runtime
// implements Simulation through this package, which keeps package game out of
// the hub. A client that predicts ticks locally (netplay, replays) calls
// Runtime.Tick with simcore.Input values.
package simcore

import "sort"

// Input holds raw per-client input data for a single tick.
type Input struct {
	Data []byte
}

// Simulation is the interface a game must implement to be driven by a
// sim.Runner.
type Simulation interface {
	// Tick advances the simulation one step with the collected inputs. The
	// inputs map has random iteration order: use SortedPlayerIDs or EachInput
	// when order can affect state. The Runner never calls Tick, Snapshot,
	// Restore or State concurrently with one another.
	Tick(inputs map[string]Input)
	// Snapshot returns an opaque checkpoint for rollback or replay.
	Snapshot() []byte
	// Restore resets the simulation to a previous snapshot.
	Restore(snapshot []byte)
	// State returns the current authoritative state for broadcast.
	State() []byte
}

// CheckedRestorer is the optional part of a Simulation that reports a snapshot
// it cannot apply and, when it does, leaves the simulation unchanged. Restore
// has no result, so a Runner that is told to load a save cannot see a refusal
// unless the simulation implements this. Runner.RestoreSnapshot calls it in place
// of Restore. game.Runtime implements it.
type CheckedRestorer interface {
	RestoreSnapshot(snapshot []byte) error
}

// SortedPlayerIDs returns the keys of inputs in ascending order.
//
// Go randomizes map iteration. A Simulation that ranges over the inputs map in
// Tick and lets the order affect state (event order, last-writer-wins fields,
// RNG draws) diverges between runs and breaks replay. Iterate this slice
// instead.
func SortedPlayerIDs(inputs map[string]Input) []string {
	ids := make([]string, 0, len(inputs))
	for id := range inputs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// EachInput calls fn for every input in ascending player ID order.
func EachInput(inputs map[string]Input, fn func(playerID string, input Input)) {
	for _, id := range SortedPlayerIDs(inputs) {
		fn(id, inputs[id])
	}
}
