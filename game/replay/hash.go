package replay

import "m31labs.dev/gosx/sim/simcore"

// Hash returns the FNV-1a 64 hash of the simulation's snapshot. Tests compare
// it between live play and replay at the same tick.
func Hash(sim simcore.Simulation) uint64 { return HashBytes(sim.Snapshot()) }

// HashBytes returns the FNV-1a 64 hash of b.
func HashBytes(b []byte) uint64 {
	h := uint64(14695981039346656037)
	for _, c := range b {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return h
}
