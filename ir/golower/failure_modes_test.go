// Failure-mode tests for the lowerer (Slice X.C.9). Each test feeds
// a construct that's outside the supported subset and asserts the
// resulting Issue carries the escape-hatch suggestion + a line number
// so authors can locate the problem.

package golower

import (
	"strings"
	"testing"
)

func TestFailureMode_Goroutine(t *testing.T) {
	src := []byte(`package handlers

func F() { go doStuff() }`)
	_, err := LowerFile(src)
	requireLowerError(t, err, "ADR 0006", 3)
}

func TestFailureMode_Channel(t *testing.T) {
	src := []byte(`package handlers

func F() {
	ch := make(chan int)
	ch <- 1
}`)
	_, err := LowerFile(src)
	requireLowerError(t, err, "ADR 0006", 0)
}

// TestFailureMode_Interface_NowHostDispatched documents the Y.E-era
// behavior shift: pre-Y.E the lowerer rejected every method call on
// a non-package receiver with "method calls on non-package receivers
// are not supported." Y.E generalizes that path into OpHostCall (so
// `c.MoveTo()` and `ctx.PropsInto()` can lower against runtime-bound
// HostReceivers). The trade is that an interface call now lowers
// cleanly and only fails at *evaluation* time, recording a
// `host_unbound` diagnostic when no receiver is bound.
//
// The test pins the new contract: lowering succeeds, and the surface
// bootstrap (or test setup) is responsible for binding the host
// receiver. Interfaces remain semantically unsupported — there's no
// way to ship a generic interface-method dispatch through the VM
// without a separate vtable mechanism — but the lowerer is no longer
// the gatekeeper.
func TestFailureMode_Interface_NowHostDispatched(t *testing.T) {
	src := []byte(`package handlers

type Greeter interface {
	Hello() string
}

func F(g Greeter) string { return g.Hello() }`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("Y.E lowers interface method calls as OpHostCall; got error: %v", err)
	}
	if prog == nil {
		t.Fatal("expected a non-nil Program from successful lowering")
	}
}

func TestFailureMode_UnknownIntrinsic(t *testing.T) {
	src := []byte(`package handlers

import "fmt"

func F() { fmt.Println("hi") }`)
	_, err := LowerFile(src)
	requireLowerError(t, err, "fmt.Println", 0)
	requireLowerError(t, err, "ADR 0006", 0)
}

// TestFailureMode_MultiReturnUserFunctionCall_NowSupported is the
// Y.D-era successor to Y.B's failure-mode test for `a, b := f()`.
// Y.B explicitly deferred the user-function multi-return form to
// Slice Y.D; Y.D now lowers it through OpIndirectCall + the
// `__ret_<i>` ObjectVal carrier so the call resolves cleanly. We
// keep the test in failure_modes_test.go (rather than relocating to
// user_fn_calls_test.go) as a permanent regression guard: if the
// LowerFile call ever starts emitting a Y.D-deferral diagnostic
// again, this test catches the regression before any handler-level
// test does.
func TestFailureMode_MultiReturnUserFunctionCall_NowSupported(t *testing.T) {
	src := []byte(`package handlers

func screenToWorld(x int, y int) (int, int) { return x, y }

func F() int {
	wx, wy := screenToWorld(1, 2)
	return wx + wy
}`)
	_, err := LowerFile(src)
	if err != nil {
		t.Fatalf("Y.D should lower multi-return user function calls cleanly, got: %v", err)
	}
}

func TestFailureMode_LabeledBreak(t *testing.T) {
	// Labeled statements themselves are rejected before the lowerer
	// even sees the inner break. Either error message is acceptable
	// — both point at the escape hatch.
	src := []byte(`package handlers

func F() {
outer:
	for i := 0; i < 10; i++ {
		for j := 0; j < 10; j++ {
			if i == j {
				break outer
			}
		}
	}
}`)
	_, err := LowerFile(src)
	requireLowerError(t, err, "ADR 0006", 0)
}

// requireLowerError asserts the LowerError contains substr. line=0
// means "don't check the line number" (e.g., a multi-line construct).
func requireLowerError(t *testing.T, err error, substr string, line int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected LowerError containing %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("expected error to contain %q, got: %v", substr, err)
	}
	if line > 0 {
		marker := ":" + itoa(line) + ":"
		if !strings.Contains(err.Error(), marker) {
			t.Errorf("expected line marker %q in error: %v", marker, err)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = '0' + byte(n%10)
		n /= 10
	}
	return string(b[i:])
}
