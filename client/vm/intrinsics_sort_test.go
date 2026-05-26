package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

// TestSortSliceAscending dispatches a sort.Slice OpCall with an
// ascending integer comparator and asserts the result is sorted. The
// comparator indexes into _items (the live working slice the VM
// publishes during the sort), matching the contract documented in
// sortSliceValue.
func TestSortSliceAscending(t *testing.T) {
	// items prop = [3, 1, 2]
	props := map[string]Value{
		"items": ArrayVal([]Value{IntVal(3), IntVal(1), IntVal(2)}),
	}

	// Comparator body: _items[_i] < _items[_j]
	// Exprs:
	//   0: OpPropGet items     (the original collection, passed to sort.Slice)
	//   1: OpPropGet _items    (the live working slice during the sort)
	//   2: OpPropGet _i
	//   3: OpIndex (_items, _i)
	//   4: OpPropGet _j
	//   5: OpIndex (_items, _j)
	//   6: OpLt (_items[_i], _items[_j])
	//   7: OpCall sort.Slice (items, body=6)
	prog := progFromExprs([]program.Expr{
		{Op: program.OpPropGet, Value: "items"},
		{Op: program.OpPropGet, Value: "_items"},
		{Op: program.OpPropGet, Value: "_i", Type: program.TypeInt},
		{Op: program.OpIndex, Operands: []program.ExprID{1, 2}},
		{Op: program.OpPropGet, Value: "_j", Type: program.TypeInt},
		{Op: program.OpIndex, Operands: []program.ExprID{1, 4}},
		{Op: program.OpLt, Operands: []program.ExprID{3, 5}, Type: program.TypeBool},
		{Op: program.OpCall, Value: "sort.Slice", Operands: []program.ExprID{0, 6}},
	})
	vm := NewVM(prog, props)
	got := vm.Eval(7)
	if len(got.Items) != 3 {
		t.Fatalf("sort.Slice result len = %d, want 3", len(got.Items))
	}
	if int(got.Items[0].Num) != 1 || int(got.Items[1].Num) != 2 || int(got.Items[2].Num) != 3 {
		t.Errorf("sorted = [%f %f %f], want [1 2 3]",
			got.Items[0].Num, got.Items[1].Num, got.Items[2].Num)
	}
}

// TestSortSliceStability verifies sort.SliceStable semantics — equal
// keys preserve original order.
func TestSortSliceStability(t *testing.T) {
	// Items are objects with a "k" field that's the same value, plus a
	// "tag" field that proves order preservation.
	items := ArrayVal([]Value{
		ObjectVal(map[string]Value{"k": IntVal(1), "tag": StringVal("a")}),
		ObjectVal(map[string]Value{"k": IntVal(1), "tag": StringVal("b")}),
		ObjectVal(map[string]Value{"k": IntVal(1), "tag": StringVal("c")}),
	})
	props := map[string]Value{"items": items}

	// Comparator that always returns false (no swaps): assert order preserved.
	prog := progFromExprs([]program.Expr{
		{Op: program.OpPropGet, Value: "items"},
		{Op: program.OpLitBool, Value: "false", Type: program.TypeBool},
		{Op: program.OpCall, Value: "sort.Slice", Operands: []program.ExprID{0, 1}},
	})
	vm := NewVM(prog, props)
	got := vm.Eval(2)
	if len(got.Items) != 3 {
		t.Fatalf("len = %d", len(got.Items))
	}
	if got.Items[0].Fields["tag"].Str != "a" ||
		got.Items[1].Fields["tag"].Str != "b" ||
		got.Items[2].Fields["tag"].Str != "c" {
		t.Errorf("stability broken; got tags %q %q %q",
			got.Items[0].Fields["tag"].Str,
			got.Items[1].Fields["tag"].Str,
			got.Items[2].Fields["tag"].Str)
	}
}
