// Slice Y.E.1 — VM evaluator for the make() builtin opcode (OpMake).
//
// OpMake allocates an empty collection. The kind tag in Expr.Value
// selects which:
//
//   - "map"   — fresh ObjectVal with an empty Fields map. The lowerer
//     drops Go's optional capacity hint at lower time; the VM has no
//     concept of map capacity (Go's runtime ignores it for iteration
//     semantics anyway).
//
//   - "slice" — fresh ArrayVal whose Items slice has length n
//     (Operands[0] evaluates to the length). Pre-filled with the
//     ZeroValue(TypeAny) entries so subsequent OpIndex reads on
//     out-of-range slots return safely; OpIndexSet writes through the
//     same slice in place per Slice Y.C's mutation contract.
//
// Per Y.D's retrospective handoff, the lowerer detects `make` BEFORE
// reaching the user-function registry probe, so this evaluator is the
// authoritative landing site for every `make(...)` call expression in
// a lowered Go source file. The Y.A composite-literal path remains the
// way to allocate a *populated* collection in one expression; OpMake
// only covers the explicit allocation form.

package vm

import (
	"fmt"

	"m31labs.dev/gosx/island/program"
)

// evalMakeExpr dispatches OpMake by inspecting the Value kind tag and
// either allocating an empty map (ObjectVal) or a length-n slice
// (ArrayVal). Unknown tags record an "invalid_make" diagnostic and
// fall back to the zero Any value so the VM's panic-free contract
// holds.
func (vm *VM) evalMakeExpr(e program.Expr) (Value, bool) {
	if e.Op != program.OpMake {
		return Value{}, false
	}
	switch e.Value {
	case "map":
		return ObjectVal(map[string]Value{}), true
	case "slice":
		return vm.makeSlice(e), true
	default:
		vm.recordExprDiagnostic(
			"invalid_make",
			fmt.Sprintf("OpMake has unknown kind tag %q (want \"map\" or \"slice\")", e.Value),
			e.Op,
			e.Value,
		)
		return ZeroValue(program.TypeAny), true
	}
}

// makeSlice evaluates the length operand and returns an ArrayVal with n
// zero-Value entries. Negative or non-numeric lengths record a
// diagnostic and fall back to a zero-length slice — the engine-surface
// authoring contract is permissive about runtime type errors so the VM
// stays panic-free (matches the Y.A / Y.C convention).
func (vm *VM) makeSlice(e program.Expr) Value {
	if len(e.Operands) == 0 {
		// `make([]T)` is not valid Go syntactically, but `make([]T, 0)`
		// produces an empty slice — preserve that even if the lowerer
		// ever emits the no-length form.
		return ArrayVal([]Value{})
	}
	lenVal := vm.Eval(e.Operands[0])
	n := int(lenVal.Num)
	if n < 0 {
		vm.recordExprDiagnostic(
			"invalid_make",
			fmt.Sprintf("OpMake(slice) length must be non-negative, got %d", n),
			e.Op,
			e.Value,
		)
		n = 0
	}
	items := make([]Value, n)
	zero := ZeroValue(program.TypeAny)
	for i := range items {
		items[i] = zero
	}
	return ArrayVal(items)
}
