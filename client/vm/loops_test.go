// Tests for OpFor / OpForRange (loops.go). Before this file existed,
// no test in the repository exercised either opcode directly against
// the VM. The per-loop cap and the per-dispatch step budget were both
// unverified.

package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// buildCountingForProgram returns a program computing:
//
//	for i := 0; i < limit; i++ { n = n + 1 }
//
// as OpFor bytecode, plus the ExprID of the OpFor node itself.
func buildCountingForProgram(limit int) (*program.Program, program.ExprID) {
	exprs := []program.Expr{
		{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},             // 0: init value
		{Op: program.OpAssign, Value: "i", Operands: []program.ExprID{0}},     // 1: init: i = 0
		{Op: program.OpLocalGet, Value: "i"},                                  // 2
		{Op: program.OpLitInt, Value: itoaTest(limit), Type: program.TypeInt}, // 3
		{Op: program.OpLt, Operands: []program.ExprID{2, 3}},                  // 4: cond: i < limit
		{Op: program.OpLocalGet, Value: "i"},                                  // 5
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},             // 6
		{Op: program.OpAdd, Operands: []program.ExprID{5, 6}},                 // 7: i+1
		{Op: program.OpAssign, Value: "i", Operands: []program.ExprID{7}},     // 8: post: i = i+1
		{Op: program.OpSignalGet, Value: "n", Type: program.TypeInt},          // 9
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},             // 10
		{Op: program.OpAdd, Operands: []program.ExprID{9, 10}},                // 11: n+1
		{Op: program.OpSignalSet, Value: "n", Operands: []program.ExprID{11}}, // 12: body: n = n+1
		{Op: program.OpFor, Operands: []program.ExprID{1, 4, 8, 12}},          // 13
	}
	return progFromExprs(exprs), 13
}

// itoaTest is a tiny decimal formatter so this file has no extra
// import beyond what's already here.
func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// TestForValueRunsExpectedIterations is the basic sanity test OpFor
// never had: a bounded `for i := 0; i < 3; i++` must run exactly 3
// times.
func TestForValueRunsExpectedIterations(t *testing.T) {
	prog, forID := buildCountingForProgram(3)
	vm := NewVM(prog, nil)
	sig := signal.New(IntVal(0))
	vm.SetSignal("n", sig)

	vm.EvalWithFrame(forID)

	if int(sig.Get().num) != 3 {
		t.Fatalf("n after for i:=0;i<3;i++ = %v, want 3", sig.Get().num)
	}
	if len(vm.Diagnostics()) != 0 {
		t.Fatalf("bounded loop should not emit diagnostics, got %+v", vm.Diagnostics())
	}
}

// TestForValuePerLoopCapExceeded pins the existing (pre-existing,
// previously untested) per-loop cap: an unconditionally-true loop
// stops at SetForCap's value and records loop_cap_exceeded.
func TestForValuePerLoopCapExceeded(t *testing.T) {
	exprs := []program.Expr{
		{Op: program.OpSeq},                                                  // 0: noop init/post
		{Op: program.OpLitBool, Value: "true"},                               // 1: cond, always true
		{Op: program.OpSignalGet, Value: "n", Type: program.TypeInt},         // 2
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},            // 3
		{Op: program.OpAdd, Operands: []program.ExprID{2, 3}},                // 4
		{Op: program.OpSignalSet, Value: "n", Operands: []program.ExprID{4}}, // 5: body
		{Op: program.OpFor, Operands: []program.ExprID{0, 1, 0, 5}},          // 6
	}
	vm := NewVM(progFromExprs(exprs), nil)
	sig := signal.New(IntVal(0))
	vm.SetSignal("n", sig)
	vm.SetForCap(4)

	vm.Eval(6)

	if int(sig.Get().num) != 4 {
		t.Fatalf("n after infinite loop capped at 4 = %v, want 4", sig.Get().num)
	}
	requireDiag(t, vm, "loop_cap_exceeded")
}

// TestNestedForLoopsShareOneStepBudget is the regression test for
// the bug this file exists to fix. Two nested, unconditionally-true
// OpFor loops each stay within their own per-loop cap. But together
// they used to multiply to cap*cap total body evaluations, with
// nothing bounding the total.
//
// With SetForCap(5), the pre-fix VM ran the inner loop to completion
// (5 iterations) on every one of the outer loop's 5 iterations. That
// is 25 total increments of n, none of it caught by any diagnostic.
//
// The fix charges every iteration of every loop, at any nesting
// depth, against one shared per-dispatch budget. So the total
// across both loops must stay at or under the cap, and a
// loop_step_budget_exceeded diagnostic must fire.
func TestNestedForLoopsShareOneStepBudget(t *testing.T) {
	const cap = 5
	exprs := []program.Expr{
		{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},         // 0: shared init value
		{Op: program.OpAssign, Value: "i", Operands: []program.ExprID{0}}, // 1: outer init: i = 0
		{Op: program.OpLitBool, Value: "true"},                            // 2: outer cond
		{Op: program.OpLocalGet, Value: "i"},                              // 3
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},         // 4
		{Op: program.OpAdd, Operands: []program.ExprID{3, 4}},             // 5: i+1
		{Op: program.OpAssign, Value: "i", Operands: []program.ExprID{5}}, // 6: outer post

		{Op: program.OpAssign, Value: "j", Operands: []program.ExprID{0}},  // 7: inner init: j = 0
		{Op: program.OpLitBool, Value: "true"},                             // 8: inner cond
		{Op: program.OpLocalGet, Value: "j"},                               // 9
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},          // 10
		{Op: program.OpAdd, Operands: []program.ExprID{9, 10}},             // 11: j+1
		{Op: program.OpAssign, Value: "j", Operands: []program.ExprID{11}}, // 12: inner post

		{Op: program.OpSignalGet, Value: "n", Type: program.TypeInt},          // 13
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},             // 14
		{Op: program.OpAdd, Operands: []program.ExprID{13, 14}},               // 15: n+1
		{Op: program.OpSignalSet, Value: "n", Operands: []program.ExprID{15}}, // 16: innermost body

		{Op: program.OpFor, Operands: []program.ExprID{7, 8, 12, 16}}, // 17: inner for
		{Op: program.OpFor, Operands: []program.ExprID{1, 2, 6, 17}},  // 18: outer for, body = inner for
	}
	vm := NewVM(progFromExprs(exprs), nil)
	sig := signal.New(IntVal(0))
	vm.SetSignal("n", sig)
	vm.SetForCap(cap)

	vm.EvalWithFrame(18)

	total := int(sig.Get().num)
	if total > cap {
		t.Fatalf("nested loops incremented n %d times with a per-dispatch budget of %d; the aggregate step budget did not bound the total", total, cap)
	}
	if total >= cap*cap {
		t.Fatalf("nested loops incremented n %d times, which is the unbounded cap*cap=%d blowup the fix must prevent", total, cap*cap)
	}
	requireDiag(t, vm, "loop_step_budget_exceeded")
}

// TestSetForCapAppliesToFreshDispatch verifies SetForCap has a real,
// exercised effect: lowering the cap to 1 must stop even a single
// unconditionally-true loop after exactly 1 iteration.
func TestSetForCapAppliesToFreshDispatch(t *testing.T) {
	exprs := []program.Expr{
		{Op: program.OpSeq},                                                  // 0: noop
		{Op: program.OpLitBool, Value: "true"},                               // 1: cond
		{Op: program.OpSignalGet, Value: "n", Type: program.TypeInt},         // 2
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},            // 3
		{Op: program.OpAdd, Operands: []program.ExprID{2, 3}},                // 4
		{Op: program.OpSignalSet, Value: "n", Operands: []program.ExprID{4}}, // 5
		{Op: program.OpFor, Operands: []program.ExprID{0, 1, 0, 5}},          // 6
	}
	vm := NewVM(progFromExprs(exprs), nil)
	sig := signal.New(IntVal(0))
	vm.SetSignal("n", sig)
	vm.SetForCap(1)

	vm.Eval(6)

	if int(sig.Get().num) != 1 {
		t.Fatalf("n with SetForCap(1) = %v, want exactly 1", sig.Get().num)
	}
}

// TestForRangeSharesStepBudgetWithOpFor verifies OpForRange charges
// against the same per-dispatch budget as OpFor. This test nests the
// loops the other way around: an OpFor loop whose body is an
// OpForRange over a slice.
func TestForRangeSharesStepBudgetWithOpFor(t *testing.T) {
	const cap = 6
	// items := []int{0,0,0,0,0,0,0,0,0,0} (10 items, an ordinary slice
	// literal — no loop cap applies to building it, only to iterating
	// it).
	pairs := make([]program.ExprID, 0, 20)
	exprs := []program.Expr{
		{Op: program.OpLitInt, Value: "0", Type: program.TypeInt}, // 0: shared init/value
	}
	for i := 0; i < 10; i++ {
		pairs = append(pairs, 0, 0)
	}
	exprs = append(exprs, program.Expr{Op: program.OpComposite, Value: "slice", Operands: pairs}) // 1: items
	itemsID := program.ExprID(1)

	exprs = append(exprs,
		program.Expr{Op: program.OpAssign, Value: "i", Operands: []program.ExprID{0}}, // 2: outer init
		program.Expr{Op: program.OpLitBool, Value: "true"},                            // 3: outer cond
		program.Expr{Op: program.OpLocalGet, Value: "i"},                              // 4
		program.Expr{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},         // 5
		program.Expr{Op: program.OpAdd, Operands: []program.ExprID{4, 5}},             // 6
		program.Expr{Op: program.OpAssign, Value: "i", Operands: []program.ExprID{6}}, // 7: outer post

		program.Expr{Op: program.OpSignalGet, Value: "n", Type: program.TypeInt},          // 8
		program.Expr{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},             // 9
		program.Expr{Op: program.OpAdd, Operands: []program.ExprID{8, 9}},                 // 10
		program.Expr{Op: program.OpSignalSet, Value: "n", Operands: []program.ExprID{10}}, // 11: range body

		program.Expr{Op: program.OpForRange, Operands: []program.ExprID{itemsID, 11}}, // 12: for range items
	)
	forRangeID := program.ExprID(12)
	exprs = append(exprs, program.Expr{Op: program.OpFor, Operands: []program.ExprID{2, 3, 7, forRangeID}}) // 13: outer for

	vm := NewVM(progFromExprs(exprs), nil)
	sig := signal.New(IntVal(0))
	vm.SetSignal("n", sig)
	vm.SetForCap(cap)

	vm.EvalWithFrame(program.ExprID(13))

	total := int(sig.Get().num)
	if total > cap {
		t.Fatalf("OpFor-over-OpForRange incremented n %d times with a per-dispatch budget of %d", total, cap)
	}
	requireDiag(t, vm, "loop_step_budget_exceeded")
}
