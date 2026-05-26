package vm

import (
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// Reconciler is the lifecycle interface shared by every surface adapter
// (DOM islands, scene engines, future Canvas2D boards). Surface-specific
// output channels (PatchOp lists for DOM, Command lists for scenes) stay
// on the concrete adapter types and are accessed via type assertion at
// the WASM bridge boundary — this interface intentionally does not unify
// outputs. That unification is Phase 1c/1d work.
//
// Implementations:
//   - *Island in this package (DOM patches)
//   - *enginevm.Runtime in client/enginevm (engine commands)
type Reconciler interface {
	// Dispose releases any retained reconciliation state.
	Dispose()

	// EvalExpr evaluates an expression in the reconciler's VM. Used by the
	// bridge for shared-signal initialization and ad-hoc evaluation.
	EvalExpr(id program.ExprID) Value

	// SetSharedSignal replaces a runtime-local signal with a shared signal
	// store entry. Used by the bridge to wire cross-island/cross-engine
	// reactive state.
	SetSharedSignal(name string, sig *signal.Signal[Value])
}
