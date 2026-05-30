// Slice Y.G — VM-level closure dispatch tests.
//
// These tests exercise the VM eval path directly (no lowerer round-
// trip) to pin behavior under known programs. They sit alongside the
// golower-level funclit_test.go which proves the lowerer + VM compose
// correctly end-to-end.

package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

// TestVM_OpClosureBuildsClosureVal pins the OpClosure evaluator's
// shape: given a program with a registered synthetic FuncDef and an
// OpClosure expression naming it, evaluating OpClosure under a frame
// with the captured locals produces a ClosureVal whose captured frame
// is the current frame.
func TestVM_OpClosureBuildsClosureVal(t *testing.T) {
	prog := &program.Program{
		Funcs: []program.FuncDef{
			{Name: "__y_g_funclit_1", Params: nil, Body: []program.ExprID{0}, Results: 1},
		},
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: "42", Type: program.TypeInt},                       // 0: body
			{Op: program.OpLitString, Value: "y", Type: program.TypeString},                  // 1: captured-name literal
			{Op: program.OpClosure, Value: "__y_g_funclit_1", Operands: []program.ExprID{1}}, // 2
		},
	}
	vm := NewVM(prog, nil)
	result := vm.EvalWithFrame(2)
	if !IsClosure(result) {
		t.Fatalf("OpClosure result is not a ClosureVal: %+v", result)
	}
	if ClosureFuncName(result) != "__y_g_funclit_1" {
		t.Errorf("ClosureFuncName = %q, want __y_g_funclit_1", ClosureFuncName(result))
	}
}

// TestVM_OpClosureInvokeRunsBody pins the dispatch path: after
// OpClosure builds a ClosureVal, calling InvokeClosure with concrete
// args runs the synthetic FuncDef's body and returns the result.
func TestVM_OpClosureInvokeRunsBody(t *testing.T) {
	prog := &program.Program{
		Funcs: []program.FuncDef{
			{Name: "__y_g_double", Params: []string{"x"}, Body: []program.ExprID{4}, Results: 1},
		},
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: "2", Type: program.TypeInt}, // 0: literal 2
			{Op: program.OpLocalGet, Value: "x"},                      // 1: load x
			{Op: program.OpMul, Operands: []program.ExprID{1, 0}},     // 2: x * 2
			{Op: program.OpReturn, Operands: []program.ExprID{2}},     // 3: return x*2
			{Op: program.OpSeq, Operands: []program.ExprID{3}},        // 4: body
			{Op: program.OpClosure, Value: "__y_g_double"},            // 5: closure ref
		},
	}
	vm := NewVM(prog, nil)
	cv := vm.EvalWithFrame(5)
	if !IsClosure(cv) {
		t.Fatalf("OpClosure did not yield a ClosureVal")
	}
	got := vm.InvokeClosure(cv, []Value{IntVal(7)})
	if int(got.Num) != 14 {
		t.Errorf("InvokeClosure(7) = %v, want 14", got.Num)
	}
}

// TestVM_ClosureCaptureByReferenceWriteback pins the strongest
// capture-semantics contract: a closure can WRITE to its captured
// local and the enclosing frame observes the mutation. This is what
// makes graph_surface's per-frame "loopFired++" pattern work.
func TestVM_ClosureCaptureByReferenceWriteback(t *testing.T) {
	// Manually construct a parent frame holding "n" = 0, then a
	// closure that captures "n" and increments it.
	parent := newFrame()
	parent.declare("n")
	parent.set("n", IntVal(0))

	prog := &program.Program{
		Funcs: []program.FuncDef{
			{
				Name:    "__y_g_incN",
				Params:  nil,
				Body:    []program.ExprID{3},
				Results: 0,
			},
		},
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},         // 0
			{Op: program.OpLocalGet, Value: "n"},                              // 1
			{Op: program.OpAdd, Operands: []program.ExprID{1, 0}},             // 2: n + 1
			{Op: program.OpAssign, Value: "n", Operands: []program.ExprID{2}}, // 3: n = n + 1
		},
	}
	vm := NewVM(prog, nil)
	cv := ClosureVal("__y_g_incN", []string{"n"}, parent)

	vm.InvokeClosure(cv, nil)
	vm.InvokeClosure(cv, nil)
	vm.InvokeClosure(cv, nil)

	got, ok := parent.get("n")
	if !ok {
		t.Fatalf("parent frame lost n after closure invocations")
	}
	if int(got.Num) != 3 {
		t.Errorf("parent n = %v, want 3 (closure writes did not propagate)", got.Num)
	}
}

// TestVM_ClosureShadowingParamWinsOverCapture verifies Go's lexical
// scoping rule: when a closure parameter has the same name as a
// captured local, references inside the body see the param, not the
// capture, and the capture stays unchanged after the call.
func TestVM_ClosureShadowingParamWinsOverCapture(t *testing.T) {
	parent := newFrame()
	parent.declare("x")
	parent.set("x", IntVal(100))

	prog := &program.Program{
		Funcs: []program.FuncDef{
			{
				Name:    "__y_g_shadow",
				Params:  []string{"x"},       // param shadows capture
				Body:    []program.ExprID{1}, // body: return x
				Results: 1,
			},
		},
		Exprs: []program.Expr{
			{Op: program.OpLocalGet, Value: "x"},                  // 0
			{Op: program.OpReturn, Operands: []program.ExprID{0}}, // 1
		},
	}
	vm := NewVM(prog, nil)
	cv := ClosureVal("__y_g_shadow", []string{"x"}, parent)
	got := vm.InvokeClosure(cv, []Value{IntVal(5)})
	if int(got.Num) != 5 {
		t.Errorf("shadowing: got %v, want 5 (the parameter, not the captured 100)", got.Num)
	}
	if v, _ := parent.get("x"); int(v.Num) != 100 {
		t.Errorf("parent x mutated despite param shadow: %v, want 100", v.Num)
	}
}

// TestVM_ClosureRecursionCap verifies the call-depth cap applies to
// closure dispatch too (not just Y.D user functions). A closure that
// invokes itself unboundedly via the registry path is caught at the
// program's MaxCallDepth and diagnoses, not panics.
func TestVM_ClosureRecursionCap(t *testing.T) {
	prog := &program.Program{
		MaxCallDepth: 5,
		Funcs: []program.FuncDef{
			{Name: "__y_g_recur", Params: []string{"n"}, Body: []program.ExprID{2}, Results: 0},
		},
		Exprs: []program.Expr{
			{Op: program.OpLocalGet, Value: "n"},                                              // 0
			{Op: program.OpIndirectCall, Value: "__y_g_recur", Operands: []program.ExprID{0}}, // 1
			{Op: program.OpSeq, Operands: []program.ExprID{1}},                                // 2 body
		},
	}
	vm := NewVM(prog, nil)
	cv := ClosureVal("__y_g_recur", nil, nil)
	// Should NOT panic; should record a call_depth_exceeded diagnostic.
	_ = vm.InvokeClosure(cv, []Value{IntVal(0)})
	// At least one diagnostic of the recursion-cap variety must be
	// present.
	found := false
	for _, d := range vm.Diagnostics() {
		if d.Code == "call_depth_exceeded" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected call_depth_exceeded diagnostic for runaway closure recursion; got: %+v", vm.Diagnostics())
	}
}
