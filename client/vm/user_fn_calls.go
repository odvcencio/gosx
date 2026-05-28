// Slice Y.D — OpIndirectCall evaluator: user-defined function dispatch.
//
// OpIndirectCall is emitted by ir/golower for `helper(args...)` calls
// that resolve to a sibling function in the same surface (a registered
// program.FuncDef). The VM evaluator here:
//
//  1. Looks up the callee FuncDef in vm.funcs by name.
//  2. Evaluates each argument expression left-to-right in the CALLER's
//     frame (so unevaluated argument expressions see the caller's
//     locals, not the callee's parameter slots).
//  3. Enforces the program-level call-depth cap so a runaway recursion
//     surfaces as a structured `call_depth_exceeded` diagnostic instead
//     of panicking the host.
//  4. Pushes a fresh frame, binds each evaluated argument to the
//     callee's parameter name in that frame, evaluates the callee's
//     body, then pops the frame.
//  5. For single-return callees, returns the body's value (with the
//     ControlReturn signal consumed at the call boundary — matching
//     EvalWithFrame's handler-boundary contract).
//  6. For multi-return callees (FuncDef.Results > 1), wraps the
//     return-tuple in an ObjectVal carrier with the "__ret_<i>" key
//     scheme so lowerMultiAssign's bindFromTmp helper can extract each
//     return slot through OpIndex reads — same shape as Slice Y.B's
//     OpMapLookup return carrier.
//
// Call-stack mechanism. The implementation deliberately extends the
// existing flat single-frame mechanism (vm.frame pointer + defer
// restore) rather than introducing a new explicit stack data structure.
// Each OpIndirectCall save-and-restores vm.frame via the Go host's
// goroutine stack — recursive calls nest the saves naturally and the
// cap enforced via vm.callDepth keeps the host stack growth bounded.
// This decision is documented at length in the Y.D retrospective
// (lessons/2026-05-26-y-d-user-fn-calls-retrospective.md).
//
// Parameter semantics. Argument Values are bound by value-copy in the
// callee's frame — but because Value.Fields (map) and Value.Items
// (slice) are reference types in Go, composite arguments share their
// underlying storage with the caller. This preserves Slice Y.C's
// retrospective guarantee: "OpFieldSet on a parameter-typed receiver
// already propagates because Value.Fields is reference-typed." Scalar
// args (int/float/bool/string) pass by value — Go's normal semantics.

package vm

import (
	"fmt"

	"m31labs.dev/gosx/island/program"
)

// evalIndirectCallExpr dispatches an OpIndirectCall through the VM's
// user-function registry. Returns (zero, false) for non-IndirectCall
// opcodes so the dispatcher in evalExpr keeps cascading; returns
// (result, true) once the call resolves (success OR diagnostic).
//
// Slice Y.G — closure-aware dispatch: before consulting vm.funcs, the
// callee name is looked up as a LOCAL in the current frame. If the
// local holds a ClosureVal (IsClosure(v) reports true), dispatch goes
// through invokeClosureFromIndirectCall — the closure's body runs in
// a fresh frame that bridges captured names through to the enclosing
// frame for Go's capture-by-reference semantics. Otherwise the
// existing Y.D path runs.
func (vm *VM) evalIndirectCallExpr(e program.Expr) (Value, bool) {
	if e.Op != program.OpIndirectCall {
		return Value{}, false
	}

	// Slice Y.G — closure-aware path. If a local with this name holds a
	// ClosureVal, dispatch through it instead of looking up the FuncDef
	// registry. Evaluate args in the caller's frame BEFORE pushing the
	// closure frame, matching Y.D's argument-evaluation contract.
	if vm.frame != nil {
		if local, ok := vm.frame.get(e.Value); ok && IsClosure(local) {
			args := make([]Value, len(e.Operands))
			for i, opID := range e.Operands {
				args[i] = vm.Eval(opID)
			}
			return vm.invokeClosureFromIndirectCall(local, args, e), true
		}
	}

	def, ok := vm.funcs[e.Value]
	if !ok {
		vm.recordExprDiagnostic(
			"unknown_user_function",
			fmt.Sprintf("OpIndirectCall callee %q is not in the program's Funcs registry", e.Value),
			e.Op,
			e.Value,
		)
		return ZeroValue(program.TypeAny), true
	}

	// Cap recursion depth so a runaway call sequence is panic-free.
	// Default 256 if MaxCallDepth wasn't set on the Program. The cap
	// includes the current call — exceeding triggers the diagnostic.
	cap := vm.program.MaxCallDepth
	if cap <= 0 {
		cap = program.DefaultMaxCallDepth
	}
	if vm.callDepth >= cap {
		vm.recordExprDiagnostic(
			"call_depth_exceeded",
			fmt.Sprintf("OpIndirectCall %q exceeded MaxCallDepth=%d", e.Value, cap),
			e.Op,
			e.Value,
		)
		return ZeroValue(program.TypeAny), true
	}

	// Step 1: evaluate arguments in the CALLER's frame.
	// Arity mismatch is recorded but doesn't abort — extra args are
	// dropped, missing args bind to the zero Value so a stale call
	// site still produces a deterministic (if wrong) result instead of
	// panicking the host.
	args := make([]Value, len(e.Operands))
	for i, opID := range e.Operands {
		args[i] = vm.Eval(opID)
	}
	if len(args) != len(def.Params) {
		vm.recordExprDiagnostic(
			"call_arity_mismatch",
			fmt.Sprintf("OpIndirectCall %q expects %d arg(s), got %d", e.Value, len(def.Params), len(args)),
			e.Op,
			e.Value,
		)
	}

	// Step 2: push a fresh frame and bind parameters.
	prevFrame := vm.frame
	vm.frame = newFrame()
	vm.callDepth++
	defer func() {
		vm.frame = prevFrame
		vm.callDepth--
	}()

	for i, paramName := range def.Params {
		vm.frame.declare(paramName)
		if i < len(args) {
			vm.frame.set(paramName, args[i])
		}
	}

	// Step 3: evaluate the body. The callee's OpReturn unwinds back to
	// here via the ControlReturn sentinel; consuming it at the call
	// boundary keeps the return semantics symmetric with
	// EvalWithFrame's handler-boundary contract.
	var result Value
	for _, bodyID := range def.Body {
		result = vm.Eval(bodyID)
		if result.Control == ControlReturn {
			result.Control = ControlNone
			break
		}
	}

	// Step 4: multi-return shaping. When the callee declares 2+
	// results, the body's OpReturn payload is already an ObjectVal
	// carrier (lowered by lowerMultiReturn — see ir/golower for the
	// `__ret_<i>` keying contract). Pass it through unchanged so the
	// caller's lowerMultiAssign reads it via OpIndex.
	return result, true
}
