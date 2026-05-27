// Slice Y.D multi-return edge-case tests — pin the behavior of
// blank-identifier discards, mixed-type multi-returns, and 3+ return
// values so future refactors can't silently regress them.
//
// graph_surface.go uses `_, ok := gPos[id]` style discards heavily;
// these tests ensure Y.D's user-function multi-return follows the
// same blank-identifier convention Y.B established for the comma-ok
// map index pattern.

package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestY_D_MultiReturnBlankFirst verifies discarding the first return
// (`_, b := f()`) skips the bind for that slot but still evaluates
// the call once.
func TestY_D_MultiReturnBlankFirst(t *testing.T) {
	src := []byte(`package handlers

func split(n int) (int, int) {
	return n / 3, n - n/3
}

func F() int {
	_, b := split(10)
	return b
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	// 10/3 = 3 (int division); 10 - 3 = 7
	if int(got.Num) != 7 {
		t.Errorf("F() = %d, want 7", int(got.Num))
	}
}

// TestY_D_MultiReturnBlankSecond verifies the symmetric case where
// the second return is discarded.
func TestY_D_MultiReturnBlankSecond(t *testing.T) {
	src := []byte(`package handlers

func split(n int) (int, int) {
	return n / 3, n - n/3
}

func F() int {
	a, _ := split(10)
	return a
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 3 {
		t.Errorf("F() = %d, want 3", int(got.Num))
	}
}

// TestY_D_MultiReturnThreeValues stretches the carrier scheme past
// the 2-value case Y.B's failure mode covered. Three return values
// exercise the `__ret_<i>` key scheme at scale.
func TestY_D_MultiReturnThreeValues(t *testing.T) {
	src := []byte(`package handlers

func triple(n int) (int, int, int) {
	return n, n * 2, n * 3
}

func F() int {
	a, b, c := triple(5)
	return a + b + c
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	// 5 + 10 + 15 = 30
	if int(got.Num) != 30 {
		t.Errorf("F() = %d, want 30", int(got.Num))
	}
}

// TestY_D_MultiReturnMixedTypes verifies that returns of different
// Value kinds (string + bool + float) round-trip through the
// ObjectVal carrier correctly. The lowerer doesn't know the per-slot
// types — Value's runtime kind preserves the distinction.
func TestY_D_MultiReturnMixedTypes(t *testing.T) {
	src := []byte(`package handlers

func describe(n int) (string, bool, float64) {
	return "n", n > 0, float64(n) + 0.5
}

func F() float64 {
	_, ok, f := describe(7)
	if ok {
		return f
	}
	return -1.0
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 7.5 {
		t.Errorf("F() = %f, want 7.5", got.Num)
	}
}

// TestY_D_MultiReturnAssignWithoutDefine verifies the `=` (re-assign)
// form, not just `:=`. Authors sometimes pre-declare the LHS locals.
func TestY_D_MultiReturnAssignWithoutDefine(t *testing.T) {
	src := []byte(`package handlers

func split(n int) (int, int) {
	return n / 2, n - n/2
}

func F() int {
	var a int
	var b int
	a, b = split(10)
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
