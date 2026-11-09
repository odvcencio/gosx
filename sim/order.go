package sim

import "m31labs.dev/gosx/sim/simcore"

// SortedPlayerIDs returns the keys of inputs in ascending order.
//
// Go randomizes map iteration. A Simulation that ranges over the inputs map in
// Tick and lets the order affect state (event order, last-writer-wins fields,
// RNG draws) diverges between runs and breaks replay. Iterate this slice
// instead.
func SortedPlayerIDs(inputs map[string]Input) []string {
	return simcore.SortedPlayerIDs(inputs)
}

// EachInput calls fn for every input in ascending player ID order.
func EachInput(inputs map[string]Input, fn func(playerID string, input Input)) {
	simcore.EachInput(inputs, fn)
}
