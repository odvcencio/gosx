// Slice Y.D.1 — failing-first tests for user-defined function call
// lowering.
//
// Pre-Y.D the lowerer rejects every bare-identifier call with
// "calls to user-defined function %q are not supported" (from
// expr.go's lowerCallExpr). Y.D's plan covers:
//
//   1. Same-package zero-arg call:       greet()                 -> string
//   2. Single-arg call w/ composite arg: makeNode(id) -> Node{...}
//   3. Multi-arg call with return:       add(a, b) -> int
//   4. Multi-return user fn:             a, b := splitVec(v)     (Y.B handoff)
//   5. Recursive call:                   fact(n) -> int
//   6. Call returning composite:         node := makeNode(id) -> Node
//   7. Call with composite literal arg:  f(vec2{x, y}) -> int
//
// At Y.D.1 each lowering call still fails: expr.go's lowerCallExpr's
// `default` branch produces the "user-defined function ... not
// supported" diagnostic. Y.D.2 adds OpIndirectCall, Y.D.3 the
// function-registry pass, Y.D.4 the VM eval. Y.D.5 marks PASS.

package golower

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestLowerUserFnCallZeroArg verifies the simplest case: one function
// calls another in the same package with no arguments.
func TestLowerUserFnCallZeroArg(t *testing.T) {
	src := []byte(`package handlers

func greet() string {
	return "hi"
}

func F() string {
	return greet()
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Str != "hi" {
		t.Errorf("F() = %q, want %q", got.Str, "hi")
	}
}

// TestLowerUserFnCallSingleArg verifies a one-arg call with a primitive
// return. Mirrors graph_surface.go's `typeColor(kind)` shape.
func TestLowerUserFnCallSingleArg(t *testing.T) {
	src := []byte(`package handlers

func double(n int) int {
	return n * 2
}

func F() int {
	return double(21)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 42 {
		t.Errorf("F() = %d, want 42", int(got.Num))
	}
}

// TestLowerUserFnCallMultiArg verifies a multi-arg call. Parameter
// order matters — wrong binding would still compute the right value
// for `add` (commutative) but fail for `sub`.
func TestLowerUserFnCallMultiArg(t *testing.T) {
	src := []byte(`package handlers

func sub(a int, b int) int {
	return a - b
}

func F() int {
	return sub(10, 3)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 7 {
		t.Errorf("F() = %d, want 7", int(got.Num))
	}
}

// TestLowerUserFnCallReturnsComposite verifies that the caller can
// receive a struct-shaped Value back from a user function. Mirrors
// graph_surface.go's `node := makeNode(id)` shape.
func TestLowerUserFnCallReturnsComposite(t *testing.T) {
	src := []byte(`package handlers

type vec2 struct {
	X float64
	Y float64
}

func makeVec(x float64, y float64) vec2 {
	return vec2{X: x, Y: y}
}

func F() float64 {
	v := makeVec(3.0, 4.0)
	return v.X + v.Y
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 7.0 {
		t.Errorf("F() = %f, want 7.0", got.Num)
	}
}

// TestLowerUserFnCallCompositeArg verifies that a composite literal
// flows into a parameter correctly. Mirrors graph_surface.go's
// `screenToWorld(vec2{x, y})` shape.
func TestLowerUserFnCallCompositeArg(t *testing.T) {
	src := []byte(`package handlers

type vec2 struct {
	X float64
	Y float64
}

func magSq(v vec2) float64 {
	return v.X*v.X + v.Y*v.Y
}

func F() float64 {
	return magSq(vec2{X: 3.0, Y: 4.0})
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 25.0 {
		t.Errorf("F() = %f, want 25.0", got.Num)
	}
}

// TestLowerUserFnCallRecursion verifies that a user function may call
// itself. Pinned at modest depth (factorial of 6 = 720) so the test
// runs deterministically without depending on the depth-cap value.
func TestLowerUserFnCallRecursion(t *testing.T) {
	src := []byte(`package handlers

func fact(n int) int {
	if n <= 1 {
		return 1
	}
	return n * fact(n-1)
}

func F() int {
	return fact(6)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 720 {
		t.Errorf("F() = %d, want 720", int(got.Num))
	}
}

// TestLowerUserFnCallMultiReturn verifies the multi-return form Y.B
// deferred. Mirrors `wx, wy := screenToWorld(sx, sy)` from
// graph_surface.go's OnMove handler.
func TestLowerUserFnCallMultiReturn(t *testing.T) {
	src := []byte(`package handlers

func split(n int) (int, int) {
	return n / 2, n - n/2
}

func F() int {
	a, b := split(10)
	return a + b
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 10 {
		t.Errorf("F() = %d, want 10", int(got.Num))
	}
}

// TestLowerUserFnCallVoidReturn verifies a side-effecting helper that
// mutates a package-level signal. Mirrors graph_surface.go's helpers
// like `initPositions()` that don't return a value but reshape the
// shared state.
func TestLowerUserFnCallVoidReturn(t *testing.T) {
	src := []byte(`package handlers

var counter = 0

func bump() {
	counter = counter + 1
}

func F() int {
	bump()
	bump()
	bump()
	return counter
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	vm.InitSignals(machine, prog)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 3 {
		t.Errorf("F() = %d, want 3", int(got.Num))
	}
}

// TestLowerUserFnCallFailureDiagnosticGoneAtY_D is a meta-check that
// once Y.D ships the supported subset stops emitting the
// "calls to user-defined function" diagnostic for in-package callees.
// This is the test that flips green when the diagnostic is removed.
func TestLowerUserFnCallNoFailureDiagnosticForInPackageCallee(t *testing.T) {
	src := []byte(`package handlers

func helper() int {
	return 1
}

func F() int {
	return helper()
}`)
	_, err := LowerFile(src)
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "calls to user-defined function \"helper\"") {
		t.Fatalf("Y.D should not emit the in-package callee diagnostic for helper(): %v", err)
	}
}
