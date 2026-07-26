// Loop opcodes for the shared VM. Slice X.C's lowerer emits OpFor for
// `for init; cond; post { body }` and OpForRange for `for i, v := range
// collection { body }`. Both are bounded by a safety cap so a runaway
// loop in lowered Go can't lock up the shared client WASM.
//
// The cap (default 1<<20 = 1,048,576) applies at two levels:
//
//  1. Per loop: no single OpFor / OpForRange runs more than the
//     cap's worth of iterations. Hitting this alone produces a
//     loop_cap_exceeded diagnostic.
//  2. Per dispatch, across every nested loop: forValue and
//     forRangeValue charge each iteration against one VM-wide
//     counter, vm.loopSteps. This applies at any nesting depth. Eval
//     resets the counter at the start of each top-level dispatch.
//     Two nested loops can each stay under the per-loop cap, yet
//     still multiply past any single loop's cap. Two loops capped at
//     1<<20 each permit up to 1<<40 total body evaluations with no
//     aggregate check. That is far too much work for one dispatch.
//     Once the shared counter exceeds the cap, a
//     loop_step_budget_exceeded diagnostic fires once. Every loop,
//     at every nesting depth, then stops on its next iteration
//     check.
//
// SetForCap raises or lowers the single knob both levels share. The
// convention throughout is "fail visibly under stress". A loop that
// hits either cap stops and records a diagnostic. It never silently
// truncates its output or runs forever.

package vm

import (
	"fmt"

	"m31labs.dev/gosx/island/program"
)

// defaultForCap bounds OpFor / OpForRange iterations, both per loop
// and in aggregate across every loop nested inside one dispatch (see
// the file header). It is not a wall-clock guarantee. A tight body
// can still take a perceptible amount of time to run the full cap.
// But it does bound the total number of body evaluations a
// dispatch's loops can ever perform. This closes the per-loop-only
// cap's nested-multiplication gap.
const defaultForCap = 1 << 20

// SetForCap sets the iteration cap for this VM. Both the per-loop
// bound (effectiveForCap) and the per-dispatch aggregate bound
// (charged by chargeLoopStep) share this one value. A value of 0 or
// less resets to the default.
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

// chargeLoopStep charges one iteration against the VM-wide loop
// step budget. Every OpFor / OpForRange loop active in the current
// top-level dispatch shares this budget, including loops nested
// inside other loops.
//
// Eval resets the budget at the start of each fresh dispatch, when
// evalDepth transitions from 0. So the bound is "total loop work
// per dispatch", not "total loop work for the VM's lifetime".
//
// Returns true once the budget is exhausted. The caller (forValue
// or forRangeValue) must then stop iterating immediately, without
// running the iteration that triggered this. Every enclosing loop
// must also stop on its own next check. This is why loopBudgetHit
// is sticky, rather than reset per loop.
func (vm *VM) chargeLoopStep(e *program.Expr) bool {
	if vm.loopBudgetHit {
		return true
	}
	vm.loopSteps++
	if vm.loopSteps <= vm.effectiveForCap() {
		return false
	}
	vm.loopBudgetHit = true
	vm.recordExprDiagnostic(
		"loop_step_budget_exceeded",
		fmt.Sprintf("total loop steps across nested loops exceeded the per-dispatch budget of %d; aborting", vm.effectiveForCap()),
		e.Op,
		e.Value,
	)
	return true
}

// forValue evaluates an OpFor expression. Operands order:
//
//	[0] init — evaluated once before the loop.
//	[1] cond — evaluated before each iteration; loop ends when false.
//	[2] post — evaluated after each body.
//	[3] body — evaluated each iteration.
//
// Any missing operand is treated as a noop. This lets the lowerer omit
// init/post when the Go source omits them.
func (vm *VM) forValue(e *program.Expr) Value {
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
		if vm.loopBudgetHit {
			return last
		}
		if !vm.Eval(e.Operands[1]).Truth() {
			return last
		}
		if vm.chargeLoopStep(e) {
			return last
		}
		last = vm.Eval(e.Operands[3])
		switch last.Control() {
		case ControlBreak:
			return last.WithControl(ControlNone)
		case ControlReturn:
			return last // propagate to enclosing EvalWithFrame
		case ControlContinue:
			last = last.WithControl(ControlNone)
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
//
//	[0] collection — evaluated once.
//	[1] body — evaluated each iteration with "_item", "_index", and
//	           (for maps) "_key" injected into the props table.
//
// Returns the last body value, or the zero value of TypeAny when the
// collection is empty. Honors the same iteration cap as OpFor for
// pathological inputs (a million-element array is allowed; a billion
// surfaces the diagnostic).
func (vm *VM) forRangeValue(e *program.Expr) Value {
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
	case coll.isList():
		items := coll.list()
		n := len(items)
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
			if vm.loopBudgetHit {
				return last
			}
			if vm.chargeLoopStep(e) {
				return last
			}
			vm.props["_index"] = IntVal(i)
			vm.props["_item"] = items[i]
			vm.props["_key"] = IntVal(i)
			last = vm.Eval(body)
			switch last.Control() {
			case ControlBreak:
				return last.WithControl(ControlNone)
			case ControlReturn:
				return last
			case ControlContinue:
				last = last.WithControl(ControlNone)
			}
		}
	case coll.isMap():
		// Iterate map fields in sorted-key order so the lowered
		// program behaves deterministically (Go's range-over-map is
		// randomized; the bytecode runtime is the gentler default).
		fields := coll.dict()
		keys := sortedEachFieldKeys(fields)
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
			if vm.loopBudgetHit {
				return last
			}
			if vm.chargeLoopStep(e) {
				return last
			}
			k := keys[i]
			vm.props["_key"] = StringVal(k)
			vm.props["_item"] = fields[k]
			vm.props["_index"] = IntVal(i)
			last = vm.Eval(body)
			switch last.Control() {
			case ControlBreak:
				return last.WithControl(ControlNone)
			case ControlReturn:
				return last
			case ControlContinue:
				last = last.WithControl(ControlNone)
			}
		}
	default:
		// Empty / scalar collection: no iterations.
	}
	return last
}
