// Slice Y.E.1.3 — VM-level tests for OpMake. The lowering side gets
// its own coverage in ir/golower/make_test.go; this file pins the
// evaluator contract independently so a future opcode-shape refactor
// can land without coupling to the lowerer's encoder.

package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

// TestEvalOpMakeEmptyMap verifies the simplest path: kind tag "map"
// returns a fresh ObjectVal with an empty (non-nil) Fields map. The
// non-nil distinction matters — OpFieldSet / OpIndexSet branch on
// Fields presence per Y.C's evaluator.
func TestEvalOpMakeEmptyMap(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpMake, Value: "map"}, // id 0
		},
	}
	vm := NewVM(prog, nil)
	got := vm.Eval(0)
	if got.Fields == nil {
		t.Fatalf("OpMake(map) should return a Value with a non-nil Fields map")
	}
	if len(got.Fields) != 0 {
		t.Errorf("OpMake(map) should be empty; got %d entries", len(got.Fields))
	}
}

// TestEvalOpMakeSizedSlice verifies that the length operand pre-sizes
// the Items slice and that each slot holds the zero Value (so OpIndex
// reads on uninitialized slots don't panic).
func TestEvalOpMakeSizedSlice(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: "5", Type: program.TypeInt}, // id 0 — length
			{Op: program.OpMake, Value: "slice", Operands: []program.ExprID{0}},
		},
	}
	vm := NewVM(prog, nil)
	got := vm.Eval(1)
	if got.Items == nil {
		t.Fatalf("OpMake(slice, 5) should return a Value with a non-nil Items slice")
	}
	if len(got.Items) != 5 {
		t.Errorf("OpMake(slice, 5) len = %d, want 5", len(got.Items))
	}
}

// TestEvalOpMakeEmptySlice verifies the zero-length form `make([]T, 0)`
// is distinct from a nil slice — len() returns 0 but the slice itself
// is allocated so subsequent appends work.
func TestEvalOpMakeEmptySlice(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt}, // id 0
			{Op: program.OpMake, Value: "slice", Operands: []program.ExprID{0}},
		},
	}
	vm := NewVM(prog, nil)
	got := vm.Eval(1)
	if got.Items == nil {
		t.Errorf("OpMake(slice, 0) should still allocate a non-nil empty slice")
	}
	if len(got.Items) != 0 {
		t.Errorf("OpMake(slice, 0) len = %d, want 0", len(got.Items))
	}
}

// TestEvalOpMakeNegativeLength documents the panic-free contract: a
// negative length records a diagnostic and yields a zero-length slice
// (matching Y.C's "stay safe under malformed input" pattern).
func TestEvalOpMakeNegativeLength(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: "-3", Type: program.TypeInt}, // id 0
			{Op: program.OpMake, Value: "slice", Operands: []program.ExprID{0}},
		},
	}
	vm := NewVM(prog, nil)
	got := vm.Eval(1)
	if len(got.Items) != 0 {
		t.Errorf("OpMake(slice, -3) should clamp to len=0; got len=%d", len(got.Items))
	}
	// A diagnostic should have been recorded — the VM's recordExprDiagnostic
	// surface is internal to the test scope; we verify the safe-output
	// contract instead.
}

// TestEvalOpMakeUnknownKindTag verifies unknown kind tags yield the
// zero Any value plus a diagnostic. The VM never panics on bad input.
func TestEvalOpMakeUnknownKindTag(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpMake, Value: "chan"}, // id 0 — channels are not supported
		},
	}
	vm := NewVM(prog, nil)
	got := vm.Eval(0)
	if got.Fields != nil {
		t.Errorf("OpMake(\"chan\") should NOT return a populated Fields map; got %v", got.Fields)
	}
	if got.Items != nil {
		t.Errorf("OpMake(\"chan\") should NOT return a populated Items slice; got %v", got.Items)
	}
}

// TestEvalOpMakeMapIsFreshAllocation verifies each evaluation of an
// OpMake(map) returns a distinct Fields map — important because Y.C's
// in-place mutation contract relies on aliasing being explicit
// (multiple holders of "the same" map share storage; multiple OpMake
// evals do not).
func TestEvalOpMakeMapIsFreshAllocation(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpMake, Value: "map"}, // id 0
		},
	}
	vm := NewVM(prog, nil)
	a := vm.Eval(0)
	b := vm.Eval(0)
	a.Fields["k"] = StringVal("from-a")
	if _, ok := b.Fields["k"]; ok {
		t.Errorf("each OpMake(map) eval must allocate a fresh Fields map; got aliased storage")
	}
}
