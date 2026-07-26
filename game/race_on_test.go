//go:build race

package game

// raceEnabled reports that the binary runs with the race detector. Wall clock
// budgets do not hold under the detector, so timing probes skip.
const raceEnabled = true
