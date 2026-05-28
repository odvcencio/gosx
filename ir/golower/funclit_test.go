// Slice Y.G.1 — failing-first tests for FuncLit closure lowering.
//
// Pre-Y.G the lowerer rejects every *ast.FuncLit with
// "unsupported expression *ast.FuncLit" from expr.go's default branch.
// Y.G's plan covers:
//
//   1. Simple no-capture FuncLit assigned and called:
//        f := func(x int) int { return x * 2 }
//        f(5) -> 10
//   2. FuncLit captures a local by reference (Go semantics):
//        y := 10
//        f := func() int { return y }
//        y = 20
//        f() -> 20
//   3. FuncLit captures multiple locals and uses captured arg:
//        a, b := 3, 4
//        f := func(c int) int { return a + b + c }
//        f(5) -> 12
//   4. FuncLit passed to host method (Mount-style c.StartLoop hook):
//        c.StartLoop(func(dt float64) { ... })
//      The host receives a ClosureVal and is expected to invoke it
//      later via vm.InvokeClosure.
//   5. FuncLit calling user-function (mutual surface dispatch):
//        f := func() int { return helper() }
//        f() resolves helper() through the user-fn registry.
//
// At Y.G.1 each lowering call still fails. Subsequent steps add:
//   Y.G.2 — ClosureVal value kind + OpClosure opcode
//   Y.G.3 — lowerer FuncLit handler + scanFuncLits anonymous registration
//   Y.G.4 — VM evalClosureExpr + closure-aware OpIndirectCall dispatch
//   Y.G.5 — host-side InvokeClosure entry point
//
// The capture-by-reference semantics matter: Go closes over the
// VARIABLE not the value, and the VM must match.

package golower

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestLowerFuncLitSimple verifies a trivial FuncLit assigned to a local
// and immediately invoked returns the correct value.
func TestLowerFuncLitSimple(t *testing.T) {
	src := []byte(`package handlers

func F() int {
	f := func(x int) int {
		return x * 2
	}
	return f(5)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 10 {
		t.Errorf("F() = %v, want 10", got.Num)
	}
}

// TestLowerFuncLitCapturesByReference verifies Go's closure-over-
// variable semantics: mutating the captured local AFTER the closure is
// created is visible through the closure when it later runs.
func TestLowerFuncLitCapturesByReference(t *testing.T) {
	src := []byte(`package handlers

func F() int {
	y := 10
	f := func() int {
		return y
	}
	y = 20
	return f()
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 20 {
		t.Errorf("F() = %v, want 20 (capture-by-reference)", got.Num)
	}
}

// TestLowerFuncLitCapturesMultipleAndUsesArg captures two locals and
// also takes an argument; the closure body sums all three.
func TestLowerFuncLitCapturesMultipleAndUsesArg(t *testing.T) {
	src := []byte(`package handlers

func F() int {
	a := 3
	b := 4
	f := func(c int) int {
		return a + b + c
	}
	return f(5)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 12 {
		t.Errorf("F() = %v, want 12", got.Num)
	}
}

// TestLowerFuncLitPassedToHostCall pins that a FuncLit passed to a
// host method (the c.StartLoop pattern) lowers to a host call carrying
// a ClosureVal argument. The host can then invoke the closure via
// vm.InvokeClosure with concrete arg values.
func TestLowerFuncLitPassedToHostCall(t *testing.T) {
	src := []byte(`package handlers

import "m31labs.dev/gosx/engine/surface"

func Mount(c *surface.Canvas) {
	c.StartLoop(func(dt float64) {
		return
	})
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "Mount")
	machine := vm.NewVM(prog, nil)
	rec := vm.NewHostRecorder()
	machine.BindHost("c", rec)
	machine.EvalWithFrame(handler.Body[0])
	if len(rec.Calls) != 1 {
		t.Fatalf("expected 1 host call, got %d: %v", len(rec.Calls), rec.Calls)
	}
	if rec.Calls[0].Method != "StartLoop" {
		t.Errorf("method = %q, want StartLoop", rec.Calls[0].Method)
	}
	if len(rec.Calls[0].Args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(rec.Calls[0].Args))
	}
	cv := rec.Calls[0].Args[0]
	// Y.G.2 will add vm.IsClosure; until then assert the arg is at least
	// non-zero (the placeholder ZeroValue from the legacy diagnostic
	// would have an empty Str AND no fields, so this still fails).
	if cv.Fields == nil {
		t.Errorf("StartLoop arg should carry closure Fields; got Value{Type=%v Str=%q Fields=nil}", cv.Type, cv.Str)
	}
}

// TestLowerFuncLitCallsUserFn verifies a FuncLit body can dispatch to
// a sibling user function via the Y.D registry; the closure inherits
// the same lowering context.
func TestLowerFuncLitCallsUserFn(t *testing.T) {
	src := []byte(`package handlers

func helper() int {
	return 7
}

func F() int {
	f := func() int {
		return helper()
	}
	return f()
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 7 {
		t.Errorf("F() = %v, want 7", got.Num)
	}
}

// TestLowerFuncLitMutateCapturedLocal verifies the closure can WRITE
// back to its captured local and the caller sees the mutation. This
// is the strongest capture-by-reference test — Go's semantics require
// it and the production c.StartLoop hook depends on it.
func TestLowerFuncLitMutateCapturedLocal(t *testing.T) {
	src := []byte(`package handlers

func F() int {
	count := 0
	f := func() {
		count = count + 1
	}
	f()
	f()
	f()
	return count
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 3 {
		t.Errorf("F() = %v, want 3 (closure-mutates-captured-local)", got.Num)
	}
}

// TestLowerFuncLitPreY_GDiagnosticGone pins that the legacy
// "unsupported expression *ast.FuncLit" diagnostic no longer fires
// for the simplest possible FuncLit.
func TestLowerFuncLitPreY_GDiagnosticGone(t *testing.T) {
	src := []byte(`package handlers

func F() {
	_ = func() {}
}`)
	_, err := LowerFile(src)
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "FuncLit") {
		t.Errorf("FuncLit lowering still rejected: %v", err)
	}
}
