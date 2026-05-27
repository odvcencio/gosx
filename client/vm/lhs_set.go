// Slice Y.C — LHS selector / indexed-set evaluators.
//
// OpFieldSet and OpIndexSet mutate the target collection in place.
// Because Value.Fields is a `map[string]Value` and Value.Items is a
// `[]Value`, the underlying storage is reference-shared between any
// Values that aliased the same materialized collection — a write
// through one alias is visible through every other alias.
//
// **Design decision (deferred from Y.A's exit, resolved here):** the
// VM uses **in-place mutation** for ObjectVal struct-field-set and
// ArrayVal / ObjectVal index-set rather than copy-on-write. Rationale:
//
//   1. graph_surface.go's canonical hard cases (`gPos[id] = vec2{...}`,
//      `fx[a.ID] += force`) target package-level maps that the author
//      *intends* to mutate — wrapping a CoW shell around every write
//      would silently break those semantics.
//   2. Y.B's range loop already mutates `_item` in place; the symmetry
//      keeps the VM evaluator's mental model coherent across slices.
//   3. Go's actual value-semantics for struct assignment surface as the
//      author's explicit `gVel[n.ID] = v` writeback in stepLayout. Y.C
//      lowers that writeback to an OpIndexSet — the by-value copy
//      already happened on the OpLocalGet read, so the OpIndexSet is
//      restoring the (mutated) local Value into the shared collection.
//      No CoW machinery is needed for that pattern to behave the same
//      as Go.
//
// The trade-off is that if a future surface author writes
// `node := gPos[id]; node.X = 5` *without* a writeback, the Go-level
// semantics (the assignment doesn't propagate to gPos) and the VM-level
// semantics (the assignment DOES propagate because node aliases the
// same Fields map) diverge. The supported subset's documented
// limitation is that surface handlers must treat composite values as
// reference-shared. If that bites a real surface we revisit (see the
// Y.C retrospective for the open question).

package vm

import (
	"fmt"

	"m31labs.dev/gosx/island/program"
)

// evalLHSSetExpr dispatches the Slice Y.C in-place mutation opcodes.
// Returns (value, true) for any LHS-set opcode (value is the assigned
// RHS, mirroring OpAssign's return so callers can chain through an
// OpSeq). Returns (Value{}, false) for any other opcode.
func (vm *VM) evalLHSSetExpr(e program.Expr) (Value, bool) {
	switch e.Op {
	case program.OpFieldSet:
		return vm.fieldSetValue(e), true
	case program.OpIndexSet:
		return vm.indexSetValue(e), true
	default:
		return Value{}, false
	}
}

// fieldSetValue evaluates `target.<Value> = Operands[1]` and writes
// the assigned value into Operands[0]'s Fields map in place. The field
// name lives in Value because it's always a compile-time identifier in
// the Go source.
//
// If Operands[0] evaluates to a Value whose Fields is nil, the
// evaluator lazily initializes the map and writes anyway. This matters
// for two scenarios:
//
//   - A composite literal that the lowerer typed as struct but ended up
//     with zero declared fields (e.g., `vec2{}` followed by `v.X = 1`)
//     — the OpComposite handler still emits an empty Fields map, so
//     this branch is rare but consistent.
//   - A future-proofing hedge: a missing Fields shouldn't drop the
//     write silently because that would mask real lowering bugs. The
//     diagnostic still fires so the issue surfaces in test logs.
//
// In-place mutation propagates through every aliasing Value because
// Go maps are reference types. If the caller obtained `target` via
// OpLocalGet (a Value-by-value copy), the Fields map inside that copy
// still points at the same underlying storage as the original.
func (vm *VM) fieldSetValue(e program.Expr) Value {
	if !vm.requireOperands(e, 2) {
		return ZeroValue(program.TypeAny)
	}
	target := vm.Eval(e.Operands[0])
	value := vm.Eval(e.Operands[1])
	if target.Fields == nil {
		vm.recordExprDiagnostic(
			"field_set_non_struct",
			fmt.Sprintf("OpFieldSet target field %q evaluates to a Value with no Fields map (Value type %d)", e.Value, target.Type),
			e.Op,
			e.Value,
		)
		// Best-effort: we cannot reach back to the local slot from here
		// (the lowerer evaluated Operands[0] as an arbitrary expression),
		// so writing into a fresh Fields map would be invisible to the
		// caller. Drop the write but keep the panic-free contract.
		return value
	}
	target.Fields[e.Value] = value
	return value
}

// indexSetValue evaluates `target[Operands[1]] = Operands[2]` and
// writes through the collection in place. The runtime path branches on
// the collection's storage:
//
//   - Items non-nil → slice/array write: the key is coerced to an int
//     and the write lands in Items[idx]. Out-of-range indices are
//     diagnosed; the VM stays panic-free (no slice growth).
//   - Fields non-nil → map write: the key is stringified through
//     Value.String() (matching OpIndex's read-side coercion) and the
//     write lands in Fields[key]. New keys are created on demand.
//   - Both nil → the collection wasn't a materialized array/object;
//     diagnose and drop the write.
//
// The branch order matters for Y.A's encoding where struct literals
// land as ObjectVal — those go through Fields. Slice literals land as
// ArrayVal (Items non-nil, Fields nil) — those go through Items. A
// Value can't legally have both populated in the supported subset, so
// "Items first if non-nil" is unambiguous.
func (vm *VM) indexSetValue(e program.Expr) Value {
	if !vm.requireOperands(e, 3) {
		return ZeroValue(program.TypeAny)
	}
	target := vm.Eval(e.Operands[0])
	key := vm.Eval(e.Operands[1])
	value := vm.Eval(e.Operands[2])

	switch {
	case target.Items != nil:
		idx := int(key.Num)
		if idx < 0 || idx >= len(target.Items) {
			vm.recordExprDiagnostic(
				"index_set_out_of_range",
				fmt.Sprintf("OpIndexSet index %d out of range [0,%d)", idx, len(target.Items)),
				e.Op,
				e.Value,
			)
			return value
		}
		target.Items[idx] = value
		return value
	case target.Fields != nil:
		target.Fields[key.String()] = value
		return value
	default:
		vm.recordExprDiagnostic(
			"index_set_non_collection",
			fmt.Sprintf("OpIndexSet target has neither Items nor Fields (Value type %d)", target.Type),
			e.Op,
			e.Value,
		)
		return value
	}
}
