package sim

import "sort"

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
