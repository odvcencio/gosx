// Slice Y.D mutual-recursion regression test — verifies that two
// user functions calling each other resolve through the registry's
// forward-reference contract.
//
// Mutual recursion is the cleanest stress test for the pre-pass
// design: even-handed `isEven`/`isOdd` only works if scanUserFuncs
// has registered BOTH functions before lowering either body. The
// equivalent code with `go vet` style sequential resolution would
// silently fall through to the legacy "calls to user-defined
// function" diagnostic for the forward call.

package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestY_D_MutualRecursion verifies isEven/isOdd cross-dispatch works
// at modest depth.
func TestY_D_MutualRecursion(t *testing.T) {
	src := []byte(`package handlers

func isEven(n int) bool {
	if n == 0 {
		return true
	}
	return isOdd(n - 1)
}

func isOdd(n int) bool {
	if n == 0 {
		return false
	}
	return isEven(n - 1)
}

func F() bool {
	return isEven(10)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if !got.Bool {
		t.Errorf("isEven(10) = %v, want true (mutual recursion must resolve via registry pre-pass)", got.Bool)
	}
}

// TestY_D_MutualRecursionOddCase rounds out the table — verifies the
// false branch of the cross-dispatch chain produces the right answer.
func TestY_D_MutualRecursionOddCase(t *testing.T) {
	src := []byte(`package handlers

func isEven(n int) bool {
	if n == 0 {
		return true
	}
	return isOdd(n - 1)
}

func isOdd(n int) bool {
	if n == 0 {
		return false
	}
	return isEven(n - 1)
}

func F() bool {
	return isEven(7)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Bool {
		t.Errorf("isEven(7) = %v, want false", got.Bool)
	}
}
