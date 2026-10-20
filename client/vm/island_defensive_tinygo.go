//go:build tinygo

package vm

// tinygoDefensiveReconcile forces the island reconciler onto the
// pre-optimisation path under TinyGo.
//
// The v0.38.0 double-buffer and subtree-reuse optimisation relies on slice
// backing-array aliasing. Native Go handles that aliasing correctly, but
// TinyGo miscompiles it: the retained tree's child list corrupts, and a
// multi-child island with a reactive Each or a signal-driven boolean/empty
// attribute drops its first re-render, then appends a duplicate subtree.
//
// Production GoSX builds compile with TinyGo, so this constant stays true
// there and NewIsland reads it to disable both the spare-buffer recycle and
// the subtree-diff skip. Native/standard-Go builds keep the fast path,
// because the fuzz targets in recycle_fuzz_test.go and reuse_skip_test.go
// still exercise it directly.
const tinygoDefensiveReconcile = true
