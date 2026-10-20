//go:build !tinygo

package vm

// tinygoDefensiveReconcile stays false outside TinyGo, so NewIsland leaves
// the double-buffer and subtree-reuse optimisation on. See
// island_defensive_tinygo.go for the TinyGo miscompile this guards against.
const tinygoDefensiveReconcile = false
