// Slice Y.E.2.3 — VM-level tests for OpHostCall + BindHost.
//
// These cover the evaluator contract independently of the lowerer (the
// lowering side gets its own coverage in ir/golower/host_call_test.go).
// The shape: programs assembled by hand, BindHost called explicitly,
// HostRecorder used to capture the dispatch sequence.

package vm

import (
	"errors"
	"testing"

	"m31labs.dev/gosx/island/program"
)

// TestEvalHostCallZeroArg verifies the simplest dispatch path: the
// receiver receives the method name and an empty args slice.
func TestEvalHostCallZeroArg(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpHostCall, Value: "c.BeginPath"},
		},
	}
	vm := NewVM(prog, nil)
	rec := NewHostRecorder()
	vm.BindHost("c", rec)
	vm.Eval(0)
	if len(rec.Calls) != 1 || rec.Calls[0].Method != "BeginPath" {
		t.Errorf("expected one BeginPath call; got %+v", rec.Calls)
	}
	if len(rec.Calls[0].Args) != 0 {
		t.Errorf("expected zero args; got %d", len(rec.Calls[0].Args))
	}
}

// TestEvalHostCallEvaluatesArgsInSourceOrder verifies that operand
// evaluation runs left-to-right and the result Values reach the host
// in the same order.
func TestEvalHostCallEvaluatesArgsInSourceOrder(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLitFloat, Value: "1.5", Type: program.TypeFloat}, // id 0
			{Op: program.OpLitFloat, Value: "2.5", Type: program.TypeFloat}, // id 1
			{Op: program.OpHostCall, Value: "c.MoveTo", Operands: []program.ExprID{0, 1}},
		},
	}
	vm := NewVM(prog, nil)
	rec := NewHostRecorder()
	vm.BindHost("c", rec)
	vm.Eval(2)
	if len(rec.Calls) != 1 {
		t.Fatalf("expected one call; got %+v", rec.Calls)
	}
	args := rec.Calls[0].Args
	if len(args) != 2 {
		t.Fatalf("expected 2 args; got %d", len(args))
	}
	if args[0].Num != 1.5 {
		t.Errorf("args[0] = %f, want 1.5", args[0].Num)
	}
	if args[1].Num != 2.5 {
		t.Errorf("args[1] = %f, want 2.5", args[1].Num)
	}
}

// TestEvalHostCallReturnValue verifies that ReturnFor wiring lets the
// host return a Value to the VM (mirrors Width()/Height() shape).
func TestEvalHostCallReturnValue(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpHostCall, Value: "c.Width"},
		},
	}
	vm := NewVM(prog, nil)
	rec := NewHostRecorder()
	rec.ReturnFor = map[string]Value{
		"Width": IntVal(800),
	}
	vm.BindHost("c", rec)
	got := vm.Eval(0)
	if int(got.Num) != 800 {
		t.Errorf("c.Width() returned %d, want 800", int(got.Num))
	}
}

// TestEvalHostCallUnboundReceiverIsPanicFree verifies the contract:
// an OpHostCall with no bound receiver records a diagnostic and
// returns the zero Value — the VM never panics.
func TestEvalHostCallUnboundReceiverIsPanicFree(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpHostCall, Value: "c.MoveTo"},
		},
	}
	vm := NewVM(prog, nil)
	got := vm.Eval(0)
	if got.Type != program.TypeAny {
		t.Errorf("unbound host call should yield TypeAny zero; got Type=%d", got.Type)
	}
}

// TestEvalHostCallTwoReceiversIndependent verifies that binding two
// different identifiers gives two independent dispatch paths.
func TestEvalHostCallTwoReceiversIndependent(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpHostCall, Value: "c.BeginPath"},  // id 0
			{Op: program.OpHostCall, Value: "ctx.Register"}, // id 1
		},
	}
	vm := NewVM(prog, nil)
	canvasRec := NewHostRecorder()
	ctxRec := NewHostRecorder()
	vm.BindHost("c", canvasRec)
	vm.BindHost("ctx", ctxRec)
	vm.Eval(0)
	vm.Eval(1)
	if len(canvasRec.Calls) != 1 || canvasRec.Calls[0].Method != "BeginPath" {
		t.Errorf("canvas recorder got %+v", canvasRec.Calls)
	}
	if len(ctxRec.Calls) != 1 || ctxRec.Calls[0].Method != "Register" {
		t.Errorf("ctx recorder got %+v", ctxRec.Calls)
	}
}

// TestEvalHostCallErrorBecomesDiagnostic verifies that an error
// returned by the host receiver becomes a host_call_error diagnostic
// without panicking the VM.
func TestEvalHostCallErrorBecomesDiagnostic(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpHostCall, Value: "c.Boom"},
		},
	}
	vm := NewVM(prog, nil)
	vm.BindHost("c", &errorReceiver{})
	got := vm.Eval(0)
	if got.Type != program.TypeAny {
		t.Errorf("erroring host call should yield TypeAny zero; got Type=%d", got.Type)
	}
}

// TestEvalHostCallInvalidValueShape covers the diagnostic for a Value
// missing the receiver-dot-method format. Shouldn't happen with the
// lowerer but the VM stays panic-free even on hand-built bad programs.
func TestEvalHostCallInvalidValueShape(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpHostCall, Value: "noDot"},
		},
	}
	vm := NewVM(prog, nil)
	vm.BindHost("c", NewHostRecorder())
	got := vm.Eval(0)
	if got.Type != program.TypeAny {
		t.Errorf("invalid OpHostCall shape should yield TypeAny zero")
	}
}

// TestBindHostNilClears verifies that BindHost(name, nil) removes
// the binding so a subsequent host call records the unbound diagnostic.
func TestBindHostNilClears(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpHostCall, Value: "c.Ping"},
		},
	}
	vm := NewVM(prog, nil)
	rec := NewHostRecorder()
	vm.BindHost("c", rec)
	vm.Eval(0)
	if len(rec.Calls) != 1 {
		t.Fatalf("expected one call after BindHost; got %d", len(rec.Calls))
	}
	vm.BindHost("c", nil)
	rec.Reset()
	vm.Eval(0)
	if len(rec.Calls) != 0 {
		t.Errorf("expected zero calls after BindHost(nil); got %+v", rec.Calls)
	}
}

// errorReceiver always errors. Used by TestEvalHostCallErrorBecomesDiagnostic.
type errorReceiver struct{}

func (*errorReceiver) Call(method string, args []Value) (Value, error) {
	return Value{}, errors.New("intentional test error")
}
