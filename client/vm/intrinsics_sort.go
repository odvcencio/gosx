// sort.Slice intrinsic — Slice X.B. Unlike the other intrinsics in this
// family, sort.Slice cannot use the standard Intrinsic signature because
// the comparator must be re-evaluated for each (i, j) pair during the
// sort, not pre-evaluated once like a normal call argument.
//
// The X.C lowerer encodes a sort.Slice call as an OpCall with:
//   Value      = "sort.Slice"
//   Operands[0] = ExprID of the slice expression (evaluated once)
//   Operands[1] = ExprID of the comparator BODY (an expression that
//                 reads "_i" and "_j" props injected by the VM during
//                 each comparison)
//
// The OpCall evaluator detects the "sort.Slice" name early and routes
// to sortSliceValue below, which never pre-evaluates Operands[1].
// This mirrors how OpFilter/OpMap treat their predicate operand as a
// body template rather than a value.
//
// We also register a stub Intrinsic so IntrinsicNames() reports
// sort.Slice; the lowerer uses that for the "is this a known stdlib
// call?" check. The stub returns an error if invoked directly, which
// would only happen if the OpCall fast-path was bypassed.

package vm

import (
	"errors"
	"sort"

	"m31labs.dev/gosx/island/program"
)

func init() {
	RegisterIntrinsic("sort.Slice", func(args []Value) (Value, error) {
		return Value{}, errors.New("sort.Slice must be dispatched through the OpCall fast-path, not LookupIntrinsic")
	})
}

// sortSliceValue executes a sort.Slice OpCall. The slice operand is
// evaluated once, then the items are sorted in place using
// sort.SliceStable with a Go-side adapter that re-evaluates the
// comparator-body expression for each comparison.
//
// The comparator reads through three synthetic props (lowercase,
// underscore-prefixed to avoid colliding with Go identifiers):
//   _i, _j   — current indices being compared.
//   _items   — the live working copy of the slice. The comparator must
//              index into _items (not the original collection prop) so
//              that comparisons see swaps performed during the sort.
//
// The X.C lowerer rewrites `s[i]` inside the comparator body to read
// from _items when the surrounding call is sort.Slice. This matches
// how Go's sort.Slice closure is reinvoked over the in-place mutated
// slice. Returns the sorted array as a new Value.
func (vm *VM) sortSliceValue(e program.Expr) Value {
	if !vm.requireOperands(e, 2) {
		return ArrayVal(nil)
	}
	coll := vm.Eval(e.Operands[0])
	if coll.Items == nil {
		return coll
	}
	items := make([]Value, len(coll.Items))
	copy(items, coll.Items)

	bodyID := e.Operands[1]
	restore := vm.captureProps([]string{"_i", "_j", "_items"})
	defer vm.restoreProps(restore)

	working := items // closure captures the slice header
	sort.SliceStable(items, func(i, j int) bool {
		vm.props["_i"] = IntVal(i)
		vm.props["_j"] = IntVal(j)
		vm.props["_items"] = ArrayVal(working)
		return vm.Eval(bodyID).Bool
	})
	return ArrayVal(items)
}
