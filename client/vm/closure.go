// Slice Y.G — OpClosure evaluator and closure-aware OpIndirectCall
// dispatch.
//
// OpClosure materializes a ClosureVal that wraps the synthetic FuncDef
// name (lives in Program.Funcs alongside Y.D's user functions) and a
// snapshot of the caller's frame for capture-by-reference. The frame
// pointer is shared — mutations in the enclosing scope after the
// closure was created remain visible inside the closure when it later
// runs, and mutations inside the closure are visible to the enclosing
// scope. This matches Go's `func() { count++ }` semantics that the
// production c.StartLoop(func(dt float64) { ... }) hook depends on.
//
// Dispatch (closure-aware OpIndirectCall):
//
//   1. The callee name (e.g. "f") is looked up first as a LOCAL in the
//      current frame. If the local holds a ClosureVal, dispatch through
//      the closure path: the captured frame becomes the parent scope,
//      and the synthetic FuncDef's body runs in a new frame that
//      consults the captured frame for any name in the captured set.
//
//   2. Otherwise the existing Y.D path runs: look the name up in
//      vm.funcs (the user-function registry) and dispatch into the
//      regular FuncDef with a fresh frame.
//
//   3. If neither path resolves the name, record an `unknown_callee`
//      diagnostic and return the zero Value, preserving the VM's
//      panic-free contract.
//
// Why local-first wins: in graph_surface.go style code,
// `f := func(){...}; f()` MUST resolve `f` to the closure. A naive
// "registry-first" check would route every bare-identifier call into
// the user-function registry and miss closures stored in locals.

package vm

import (
	"m31labs.dev/gosx/island/program"
)

// evalClosureExpr handles OpClosure: build a ClosureVal carrying the
// synthetic FuncDef name and the captured caller-frame reference.
//
// Operands are OpLitString entries naming each captured local. We
// evaluate them defensively (the lowerer always emits literals, but
// dynamic operands shouldn't crash the VM) and feed the resulting
// names into ClosureVal.
func (vm *VM) evalClosureExpr(e program.Expr) (Value, bool) {
	if e.Op != program.OpClosure {
		return Value{}, false
	}
	captured := make([]string, 0, len(e.Operands))
	for _, op := range e.Operands {
		nameVal := vm.Eval(op)
		if nameVal.Str != "" {
			captured = append(captured, nameVal.Str)
		}
	}
	// Snapshot the caller's frame pointer. nil is OK — closures
	// declared at handler scope with no enclosing locals are valid.
	return ClosureVal(e.Value, captured, vm.frame), true
}

// invokeClosureFromIndirectCall handles the Y.D OpIndirectCall path
// when the callee name resolves to a local-bound ClosureVal. The
// caller (evalIndirectCallExpr) handles registry-miss + arg evaluation;
// this function does the frame push, captured-bridge install, body
// evaluation, and frame pop.
//
// The closure's body is the synthetic FuncDef the lowerer registered;
// its Body / Params live in vm.funcs under the same name carried by
// the ClosureVal. The captured-frame bridge is installed by wrapping
// the fresh callee frame's `parent` slot — see frame.go's parent
// lookup chain.
func (vm *VM) invokeClosureFromIndirectCall(cv Value, args []Value, e program.Expr) Value {
	def, ok := vm.funcs[cv.closure.funcName]
	if !ok {
		vm.recordExprDiagnostic(
			"unknown_closure_func",
			"closure refers to unregistered FuncDef "+cv.closure.funcName,
			e.Op,
			e.Value,
		)
		return ZeroValue(program.TypeAny)
	}

	// Recursion cap — same contract as Y.D's user-function dispatch.
	cap := vm.program.MaxCallDepth
	if cap <= 0 {
		cap = program.DefaultMaxCallDepth
	}
	if vm.callDepth >= cap {
		vm.recordExprDiagnostic(
			"call_depth_exceeded",
			"closure dispatch exceeded MaxCallDepth="+itoa(cap),
			e.Op,
			e.Value,
		)
		return ZeroValue(program.TypeAny)
	}

	prevFrame := vm.frame
	closureFrame := newClosureFrame(cv.closure)
	vm.frame = closureFrame
	vm.callDepth++
	defer func() {
		vm.frame = prevFrame
		vm.callDepth--
	}()

	// Bind params into the fresh closure frame (NOT the captured one
	// — params are the closure's own locals, even if a captured name
	// shadows them, the param wins). Same arity-mismatch policy as Y.D.
	for i, paramName := range def.Params {
		closureFrame.declare(paramName)
		if i < len(args) {
			closureFrame.set(paramName, args[i])
		}
	}

	var result Value
	for _, bodyID := range def.Body {
		result = vm.Eval(bodyID)
		if result.Control == ControlReturn {
			result.Control = ControlNone
			break
		}
	}
	return result
}

// InvokeClosure is the public host-side hook to invoke a ClosureVal
// the host captured (e.g. via c.StartLoop(cb)) at a later time. This
// is the symmetric companion to BindHost — it lets a HostReceiver
// drive the lowered closure body from outside the VM's regular
// OpIndirectCall dispatch path.
//
// The host typically receives a ClosureVal as one of the args to its
// Call method, stores it, and later invokes it (animation tick,
// async response, etc.). Each invocation runs the closure body with
// args bound to the synthetic FuncDef's params and the captured
// frame visible via the by-reference bridge — same semantics as
// inline closure calls.
//
// Returns the zero Value when cv isn't a closure or its synthetic
// FuncDef is missing; otherwise the body's return value. Panic-free
// per the engine-surface contract.
func (vm *VM) InvokeClosure(cv Value, args []Value) Value {
	if !IsClosure(cv) {
		return ZeroValue(program.TypeAny)
	}
	synthetic := program.Expr{Op: program.OpIndirectCall, Value: cv.closure.funcName}
	return vm.invokeClosureFromIndirectCall(cv, args, synthetic)
}

// itoa is a tiny helper so closure.go doesn't pull strconv (keeps the
// file dependency-light alongside its sibling evaluators).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
