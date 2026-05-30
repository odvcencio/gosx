// Sequencing-opcode tests for Slice X.A. These exercise OpSeq, OpAssign,
// OpLocalDecl, OpLocalGet, OpLocalSet and the EvalWithFrame entry point
// that the X.C lowerer will eventually call.

package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// TestOpSeqReturnsLastValue is the spec test referenced by X.A.2:
// OpSeq{[a, b, c]} returns c's value and a/b's side effects are visible
// to c. Here we sequence three signal writes and a final read, then
// assert the read sees the cumulative effect.
func TestOpSeqReturnsLastValue(t *testing.T) {
	// Exprs:
	//   0: int literal 1
	//   1: int literal 2
	//   2: int literal 3
	//   3: OpAssign x := 1       (Operands=[0], Value="x")
	//   4: OpAssign x := 2       (Operands=[1], Value="x")
	//   5: OpAssign x := 3       (Operands=[2], Value="x")
	//   6: OpLocalGet x          (Value="x")
	//   7: OpSeq[3,4,5,6]
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},
		{Op: program.OpLitInt, Value: "2", Type: program.TypeInt},
		{Op: program.OpLitInt, Value: "3", Type: program.TypeInt},
		{Op: program.OpAssign, Value: "x", Operands: []program.ExprID{0}},
		{Op: program.OpAssign, Value: "x", Operands: []program.ExprID{1}},
		{Op: program.OpAssign, Value: "x", Operands: []program.ExprID{2}},
		{Op: program.OpLocalGet, Value: "x"},
		{Op: program.OpSeq, Operands: []program.ExprID{3, 4, 5, 6}},
	})
	vm := NewVM(prog, nil)
	got := vm.EvalWithFrame(7)
	if got.Type != program.TypeInt || int(got.Num) != 3 {
		t.Fatalf("OpSeq result = %+v, want IntVal(3)", got)
	}
}

// TestOpSeqWritesSignal verifies that OpSeq side effects propagate to
// real signals when the target is registered. This is the contract the
// X.C lowerer relies on for `pkg.x = ...` assignments.
func TestOpSeqWritesSignal(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "7", Type: program.TypeInt},
		{Op: program.OpAssign, Value: "count", Operands: []program.ExprID{0}},
		{Op: program.OpSignalGet, Value: "count", Type: program.TypeInt},
		{Op: program.OpSeq, Operands: []program.ExprID{1, 2}},
	})
	vm := NewVM(prog, nil)
	sig := signal.New(IntVal(0))
	vm.SetSignal("count", sig)
	got := vm.EvalWithFrame(3)
	if got.Type != program.TypeInt || int(got.Num) != 7 {
		t.Fatalf("OpSeq with signal target = %+v, want IntVal(7)", got)
	}
	if int(sig.Get().Num) != 7 {
		t.Fatalf("signal.Get = %f, want 7", sig.Get().Num)
	}
}

// TestOpLocalDeclThenAssignThenGet checks the canonical lower of
// `x := 5; return x`. The declaration is explicit, the assignment uses
// OpLocalSet, and the get returns the stored value.
func TestOpLocalDeclThenAssignThenGet(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "5", Type: program.TypeInt},
		{Op: program.OpLocalDecl, Value: "x"},
		{Op: program.OpLocalSet, Value: "x", Operands: []program.ExprID{0}},
		{Op: program.OpLocalGet, Value: "x"},
		{Op: program.OpSeq, Operands: []program.ExprID{1, 2, 3}},
	})
	vm := NewVM(prog, nil)
	got := vm.EvalWithFrame(4)
	if got.Type != program.TypeInt || int(got.Num) != 5 {
		t.Fatalf("local x = %+v, want IntVal(5)", got)
	}
}

// TestOpLocalGetMissingProducesDiagnostic ensures reading an undeclared
// local follows the panic-free contract: zero value plus diagnostic.
func TestOpLocalGetMissingProducesDiagnostic(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLocalGet, Value: "nope"},
	})
	vm := NewVM(prog, nil)
	got, diags := vm.EvalWithDiagnostics(0)
	if got.Type != program.TypeAny || got.Num != 0 || got.Str != "" || got.Bool {
		t.Fatalf("missing local should return zero value, got %+v", got)
	}
	if len(diags) != 1 || diags[0].Code != "missing_local" {
		t.Fatalf("expected missing_local diagnostic, got %+v", diags)
	}
}

// TestOpAssignWithoutFrameDiagnostic ensures OpAssign with neither a
// registered signal nor an active frame produces a structured error
// rather than silently corrupting state.
func TestOpAssignWithoutFrameDiagnostic(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},
		{Op: program.OpAssign, Value: "x", Operands: []program.ExprID{0}},
	})
	vm := NewVM(prog, nil)
	// Direct Eval (no EvalWithFrame) — frame is nil; "x" isn't a signal.
	_, diags := vm.EvalWithDiagnostics(1)
	if len(diags) != 1 || diags[0].Code != "missing_frame" {
		t.Fatalf("expected missing_frame diagnostic, got %+v", diags)
	}
}

// TestEvalWithFrameIsIsolated verifies that a nested EvalWithFrame call
// doesn't leak locals into the outer frame and that the outer frame's
// locals are restored when the inner evaluation returns.
func TestEvalWithFrameIsIsolated(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},         // 0
		{Op: program.OpLitInt, Value: "99", Type: program.TypeInt},        // 1
		{Op: program.OpAssign, Value: "x", Operands: []program.ExprID{0}}, // 2: outer x = 1
		{Op: program.OpAssign, Value: "x", Operands: []program.ExprID{1}}, // 3: inner x = 99
		{Op: program.OpLocalGet, Value: "x"},                              // 4: read x
	})
	vm := NewVM(prog, nil)

	// Outer frame writes x = 1.
	vm.frame = newFrame()
	vm.Eval(2)
	if v, ok := vm.frame.get("x"); !ok || int(v.Num) != 1 {
		t.Fatalf("outer frame x = %+v ok=%v, want IntVal(1)", v, ok)
	}

	// Inner EvalWithFrame writes x = 99. Outer x must remain 1.
	inner := vm.EvalWithFrame(3)
	if int(inner.Num) != 99 {
		t.Fatalf("inner assign returned %+v, want IntVal(99)", inner)
	}

	if v, ok := vm.frame.get("x"); !ok || int(v.Num) != 1 {
		t.Fatalf("outer frame x after inner EvalWithFrame = %+v ok=%v, want IntVal(1)", v, ok)
	}
}

// TestOpSeqEmptyIsHarmless guards against the surprise corner case
// where the lowerer emits an empty OpSeq (e.g. for an empty block).
func TestOpSeqEmptyIsHarmless(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpSeq, Operands: nil},
	})
	vm := NewVM(prog, nil)
	got, diags := vm.EvalWithDiagnostics(0)
	if got.Type != program.TypeAny {
		t.Errorf("empty OpSeq should return zero-value Any, got %+v", got)
	}
	if len(diags) != 0 {
		t.Errorf("empty OpSeq should not emit diagnostics, got %+v", diags)
	}
}

// TestOpAssignFallsThroughToSignal documents the OpAssign target
// resolution order: signal first, frame second. The lowerer relies on
// this to compile package-level `var` writes (signals) and function
// locals through the same opcode.
func TestOpAssignFallsThroughToSignal(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "42", Type: program.TypeInt},
		{Op: program.OpAssign, Value: "shared", Operands: []program.ExprID{0}},
	})
	vm := NewVM(prog, nil)
	sig := signal.New(IntVal(0))
	vm.SetSignal("shared", sig)

	// Even with an active frame, signal takes precedence.
	got := vm.EvalWithFrame(1)
	if int(got.Num) != 42 {
		t.Fatalf("assign returned %+v, want IntVal(42)", got)
	}
	if int(sig.Get().Num) != 42 {
		t.Fatalf("signal not updated: got %f, want 42", sig.Get().Num)
	}
}
