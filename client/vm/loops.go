// Loop opcodes for the shared VM. Slice X.C's lowerer emits OpFor for
// `for init; cond; post { body }` and OpForRange for `for i, v := range
// collection { body }`. Both are bounded by a safety cap so a runaway
// loop in lowered Go can't lock up the shared client WASM.
//
// The cap is per-VM (defaults to 1<<20 = 1M iterations); call
// VM.SetForCap to raise or lower it. Hitting the cap produces a
// structured diagnostic (loop_cap_exceeded) and the loop terminates
// at the current iteration. The convention is "fail visibly under
// stress" — we never silently truncate output.

package vm

import (
	"fmt"

	"m31labs.dev/gosx/island/program"
)

// defaultForCap bounds OpFor / OpForRange iterations per call. Chosen
// so trivial bounded loops never trip it and pathological infinite
// loops surface quickly (~ms wall clock for a tight body).
const defaultForCap = 1 << 20

// SetForCap sets the per-loop iteration cap for this VM. A value of 0
// or less resets to the default.
func (vm *VM) SetForCap(n int) {
	if n <= 0 {
		vm.forCap = defaultForCap
		return
	}
	vm.forCap = n
}

// effectiveForCap returns the active cap, lazily initializing to the
// default if SetForCap was never called.
func (vm *VM) effectiveForCap() int {
	if vm.forCap <= 0 {
		return defaultForCap
	}
	return vm.forCap
}

// forValue evaluates an OpFor expression. Operands order:
//   [0] init — evaluated once before the loop.
//   [1] cond — evaluated before each iteration; loop ends when false.
//   [2] post — evaluated after each body.
//   [3] body — evaluated each iteration.
//
// Any missing operand is treated as a noop. This lets the lowerer omit
// init/post when the Go source omits them.
func (vm *VM) forValue(e program.Expr) Value {
	if len(e.Operands) < 4 {
		vm.recordExprDiagnostic(
			"missing_operands",
			fmt.Sprintf("OpFor requires 4 operands (init, cond, post, body), got %d", len(e.Operands)),
			e.Op,
			e.Value,
		)
		return ZeroValue(program.TypeAny)
	}
	vm.Eval(e.Operands[0])

	cap := vm.effectiveForCap()
	var last Value
	for i := 0; i < cap; i++ {
		if !vm.Eval(e.Operands[1]).Bool {
			return last
		}
		last = vm.Eval(e.Operands[3])
		switch last.Control {
		case ControlBreak:
			last.Control = ControlNone
			return last
		case ControlReturn:
			return last // propagate to enclosing EvalWithFrame
		case ControlContinue:
			last.Control = ControlNone
		}
		vm.Eval(e.Operands[2])
	}
	vm.recordExprDiagnostic(
		"loop_cap_exceeded",
		fmt.Sprintf("OpFor exceeded iteration cap of %d; aborting", cap),
		e.Op,
		e.Value,
	)
	return last
}

// forRangeValue evaluates an OpForRange expression. Operands order:
//   [0] collection — evaluated once.
//   [1] body — evaluated each iteration with "_item", "_index", and
//              (for maps) "_key" injected into the props table.
//
// Returns the last body value, or the zero value of TypeAny when the
// collection is empty. Honors the same iteration cap as OpFor for
// pathological inputs (a million-element array is allowed; a billion
// surfaces the diagnostic).
func (vm *VM) forRangeValue(e program.Expr) Value {
	if !vm.requireOperands(e, 2) {
		return ZeroValue(program.TypeAny)
	}
	coll := vm.Eval(e.Operands[0])
	body := e.Operands[1]
	cap := vm.effectiveForCap()

	restore := vm.captureProps([]string{"_item", "_index", "_key"})
	defer vm.restoreProps(restore)

	var last Value
	switch {
	case coll.Items != nil:
		n := len(coll.Items)
		if n > cap {
			vm.recordExprDiagnostic(
				"loop_cap_exceeded",
				fmt.Sprintf("OpForRange over %d items exceeded cap %d; iterating first %d only", n, cap, cap),
				e.Op,
				e.Value,
			)
			n = cap
		}
		for i := 0; i < n; i++ {
			vm.props["_index"] = IntVal(i)
			vm.props["_item"] = coll.Items[i]
			vm.props["_key"] = IntVal(i)
			last = vm.Eval(body)
			switch last.Control {
			case ControlBreak:
				last.Control = ControlNone
				return last
			case ControlReturn:
				return last
			case ControlContinue:
				last.Control = ControlNone
			}
		}
	case coll.Fields != nil:
		// Iterate map fields in sorted-key order so the lowered
		// program behaves deterministically (Go's range-over-map is
		// randomized; the bytecode runtime is the gentler default).
		keys := sortedEachFieldKeys(coll.Fields)
		n := len(keys)
		if n > cap {
			vm.recordExprDiagnostic(
				"loop_cap_exceeded",
				fmt.Sprintf("OpForRange over %d map entries exceeded cap %d; iterating first %d only", n, cap, cap),
				e.Op,
				e.Value,
			)
			n = cap
		}
		for i := 0; i < n; i++ {
			k := keys[i]
			vm.props["_key"] = StringVal(k)
			vm.props["_item"] = coll.Fields[k]
			vm.props["_index"] = IntVal(i)
			last = vm.Eval(body)
			switch last.Control {
			case ControlBreak:
				last.Control = ControlNone
				return last
			case ControlReturn:
				return last
			case ControlContinue:
				last.Control = ControlNone
			}
		}
	default:
		// Empty / scalar collection: no iterations.
	}
	return last
}
