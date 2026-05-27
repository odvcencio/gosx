// Slice Y.C.3 — VM-level tests for OpFieldSet / OpIndexSet evaluators.
//
// These tests exercise the VM directly (no lowering) so a bug in the
// evaluator can be bisected without going through the AST → opcode
// stage. The lowerer tests (lhs_selectors_test.go in ir/golower) cover
// the full path; this file pins the in-place mutation contract.

package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

// TestOpFieldSetWritesIntoExistingObject pins the in-place mutation
// contract: OpFieldSet on a target whose Fields map is non-nil writes
// into the map and the change is visible through any aliasing Value.
func TestOpFieldSetWritesIntoExistingObject(t *testing.T) {
	// Build a program that:
	//   1. Declares local "v" and assigns it an ObjectVal{X:1, Y:2}.
	//   2. OpFieldSet v.X = 99.
	//   3. Reads back v.X via OpIndex.
	prog := &program.Program{
		Exprs: []program.Expr{
			// 0: literal 99
			{Op: program.OpLitFloat, Value: "99", Type: program.TypeFloat},
			// 1: OpLocalGet v
			{Op: program.OpLocalGet, Value: "v"},
			// 2: OpFieldSet v.X = 99
			{Op: program.OpFieldSet, Value: "X", Operands: []program.ExprID{1, 0}},
			// 3: lit string "X"
			{Op: program.OpLitString, Value: "X", Type: program.TypeString},
			// 4: OpLocalGet v (after mutation)
			{Op: program.OpLocalGet, Value: "v"},
			// 5: OpIndex v["X"]
			{Op: program.OpIndex, Operands: []program.ExprID{4, 3}},
			// 6: OpSeq(2, 5)
			{Op: program.OpSeq, Operands: []program.ExprID{2, 5}},
		},
	}
	machine := NewVM(prog, nil)
	machine.frame = newFrame()
	machine.frame.declare("v")
	machine.frame.set("v", ObjectVal(map[string]Value{
		"X": FloatVal(1),
		"Y": FloatVal(2),
	}))
	got := machine.Eval(6)
	if got.Num != 99 {
		t.Errorf("v.X after FieldSet = %f, want 99", got.Num)
	}
	// Confirm the original map was mutated, not a copy.
	v, _ := machine.frame.get("v")
	if v.Fields["X"].Num != 99 {
		t.Errorf("frame.v.Fields[X] = %f, want 99 (in-place mutation broken)", v.Fields["X"].Num)
	}
}

// TestOpFieldSetOnNonStructDiagnoses pins the diagnostic path: a target
// without a Fields map records `field_set_non_struct` and stays
// panic-free.
func TestOpFieldSetOnNonStructDiagnoses(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			// 0: int target (non-struct)
			{Op: program.OpLitInt, Value: "7", Type: program.TypeInt},
			// 1: lit value
			{Op: program.OpLitInt, Value: "42", Type: program.TypeInt},
			// 2: OpFieldSet on the int — should diagnose
			{Op: program.OpFieldSet, Value: "X", Operands: []program.ExprID{0, 1}},
		},
	}
	machine := NewVM(prog, nil)
	machine.frame = newFrame()
	got := machine.Eval(2)
	if int(got.Num) != 42 {
		t.Errorf("OpFieldSet on non-struct returned %f, want 42 (assigned value)", got.Num)
	}
	diags := machine.Diagnostics()
	found := false
	for _, d := range diags {
		if d.Code == "field_set_non_struct" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected field_set_non_struct diagnostic; got %v", diags)
	}
}

// TestOpIndexSetWritesIntoSliceInPlace exercises the slice branch
// (Items non-nil). Sets s[1] = 9, reads back s[1].
func TestOpIndexSetWritesIntoSliceInPlace(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			// 0: target s
			{Op: program.OpLocalGet, Value: "s"},
			// 1: key 1
			{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},
			// 2: value 9
			{Op: program.OpLitInt, Value: "9", Type: program.TypeInt},
			// 3: OpIndexSet s[1] = 9
			{Op: program.OpIndexSet, Operands: []program.ExprID{0, 1, 2}},
			// 4-6: read back s[1]
			{Op: program.OpLocalGet, Value: "s"},
			{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},
			{Op: program.OpIndex, Operands: []program.ExprID{4, 5}},
			// 7: seq
			{Op: program.OpSeq, Operands: []program.ExprID{3, 6}},
		},
	}
	machine := NewVM(prog, nil)
	machine.frame = newFrame()
	machine.frame.declare("s")
	machine.frame.set("s", ArrayVal([]Value{IntVal(1), IntVal(2), IntVal(3)}))
	got := machine.Eval(7)
	if int(got.Num) != 9 {
		t.Errorf("s[1] after IndexSet = %d, want 9", int(got.Num))
	}
	v, _ := machine.frame.get("s")
	if int(v.Items[1].Num) != 9 {
		t.Errorf("frame.s.Items[1] = %d, want 9 (in-place mutation broken)", int(v.Items[1].Num))
	}
}

// TestOpIndexSetWritesIntoMapInPlace exercises the map branch (Fields
// non-nil). Sets m["k"] = 5, reads back m["k"]; new key creation works.
func TestOpIndexSetWritesIntoMapInPlace(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			// 0: target m
			{Op: program.OpLocalGet, Value: "m"},
			// 1: key "k"
			{Op: program.OpLitString, Value: "k", Type: program.TypeString},
			// 2: value 5
			{Op: program.OpLitInt, Value: "5", Type: program.TypeInt},
			// 3: OpIndexSet m["k"] = 5
			{Op: program.OpIndexSet, Operands: []program.ExprID{0, 1, 2}},
			// 4-6: read m["k"]
			{Op: program.OpLocalGet, Value: "m"},
			{Op: program.OpLitString, Value: "k", Type: program.TypeString},
			{Op: program.OpIndex, Operands: []program.ExprID{4, 5}},
			// 7: seq
			{Op: program.OpSeq, Operands: []program.ExprID{3, 6}},
		},
	}
	machine := NewVM(prog, nil)
	machine.frame = newFrame()
	machine.frame.declare("m")
	// Start with an empty map; IndexSet should create the key.
	machine.frame.set("m", ObjectVal(map[string]Value{}))
	got := machine.Eval(7)
	if int(got.Num) != 5 {
		t.Errorf("m[\"k\"] after IndexSet = %d, want 5", int(got.Num))
	}
}

// TestOpIndexSetOutOfRangeDiagnoses pins the diagnostic path for slice
// bounds violations.
func TestOpIndexSetOutOfRangeDiagnoses(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLocalGet, Value: "s"},
			{Op: program.OpLitInt, Value: "10", Type: program.TypeInt},
			{Op: program.OpLitInt, Value: "99", Type: program.TypeInt},
			{Op: program.OpIndexSet, Operands: []program.ExprID{0, 1, 2}},
		},
	}
	machine := NewVM(prog, nil)
	machine.frame = newFrame()
	machine.frame.declare("s")
	machine.frame.set("s", ArrayVal([]Value{IntVal(1), IntVal(2)}))
	_ = machine.Eval(3)
	diags := machine.Diagnostics()
	found := false
	for _, d := range diags {
		if d.Code == "index_set_out_of_range" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected index_set_out_of_range diagnostic; got %v", diags)
	}
}

// TestOpIndexSetOnNonCollectionDiagnoses pins the third diagnostic
// branch: target has neither Items nor Fields.
func TestOpIndexSetOnNonCollectionDiagnoses(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: "7", Type: program.TypeInt},
			{Op: program.OpLitString, Value: "k", Type: program.TypeString},
			{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},
			{Op: program.OpIndexSet, Operands: []program.ExprID{0, 1, 2}},
		},
	}
	machine := NewVM(prog, nil)
	machine.frame = newFrame()
	_ = machine.Eval(3)
	diags := machine.Diagnostics()
	found := false
	for _, d := range diags {
		if d.Code == "index_set_non_collection" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected index_set_non_collection diagnostic; got %v", diags)
	}
}
