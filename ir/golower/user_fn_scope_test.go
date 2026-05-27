// Slice Y.D scope-isolation tests — verify that the per-call frame
// correctly shadows package signals when a parameter shares a name
// with one, and that the caller's frame is restored cleanly when the
// callee returns.
//
// These behaviors emerge from the EvalWithFrame save/restore +
// OpLocalGet's frame→signals→props fallback chain; the tests pin
// them so a future refactor can't quietly break the scoping rules
// engine-surface authors rely on.

package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestY_D_ParamShadowsSignal verifies a parameter named the same as
// a package-level signal binds to the call argument inside the
// callee's frame, NOT to the signal. The caller's signal is
// untouched after the call returns.
func TestY_D_ParamShadowsSignal(t *testing.T) {
	src := []byte(`package handlers

var counter = 100

func inspect(counter int) int {
	return counter
}

func F() int {
	return inspect(7)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	vm.InitSignals(machine, prog)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 7 {
		t.Errorf("F() = %d, want 7 (param must shadow signal in callee frame)", int(got.Num))
	}
}

// TestY_D_CallerFrameRestoredAfterCall verifies that locals declared
// in the caller's frame remain bound after the callee returns. A
// frame-restore bug would surface as the caller's local being zero
// after the call.
func TestY_D_CallerFrameRestoredAfterCall(t *testing.T) {
	src := []byte(`package handlers

func noop(n int) int {
	return n
}

func F() int {
	x := 42
	noop(99)
	return x
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 42 {
		t.Errorf("F() = %d, want 42 (caller's local must survive the call)", int(got.Num))
	}
}

// TestY_D_NestedCallsRestoreFramesInOrder verifies that two levels of
// nested calls properly stack and unstack frames — A calls B calls
// C, then we're back in A's frame.
func TestY_D_NestedCallsRestoreFramesInOrder(t *testing.T) {
	src := []byte(`package handlers

func c(n int) int {
	x := n + 100
	return x
}

func b(n int) int {
	x := c(n) + 10
	return x
}

func a(n int) int {
	x := b(n) + 1
	return x
}

func F() int {
	return a(5)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	// a(5) = b(5)+1 = (c(5)+10)+1 = (5+100+10)+1 = 116
	if int(got.Num) != 116 {
		t.Errorf("F() = %d, want 116 (3-deep nested call stack must compose)", int(got.Num))
	}
}

// TestY_D_CalleeLocalDoesNotLeak verifies that a local declared in a
// callee's frame is invisible to the caller after the callee returns.
// (A regression here would mean OpLocalDecl is leaking into the
// caller's frame.)
func TestY_D_CalleeLocalDoesNotLeak(t *testing.T) {
	src := []byte(`package handlers

func helper() int {
	hiddenVar := 42
	return hiddenVar
}

func F() int {
	helper()
	// hiddenVar is invisible here; OpLocalGet falls through to props
	// then to a missing_local diagnostic. The runtime returns zero.
	return hiddenVar
}`)
	prog, err := LowerFile(src)
	if err != nil {
		// In this fixture the lowerer happily emits OpLocalGet for any
		// bare identifier (X.A's design); the VM is the one that
		// diagnoses the missing local. So LowerFile should still
		// succeed and the value should be zero.
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 0 {
		t.Errorf("F() = %d, want 0 (callee's hiddenVar must not leak into caller frame)", int(got.Num))
	}
}
