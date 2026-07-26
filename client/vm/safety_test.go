// Regression tests for the panic-free-contract crash bugs fixed
// alongside this file: OpSlice / OpSubstring bounds clamping (value.go
// SliceVal / SubstringVal), and Eval's own recursion depth guard.
// These build the exact opcode graphs the bug report reproduced
// against. value_test.go and json_test.go cover the same fixes at
// the Value method level directly.

package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

// TestVMOpSliceEmptyArrayNegativeEndDoesNotPanic reproduces
// `items[0:len(items)-1]` where items is an empty array. end
// evaluates to -1 through ordinary OpLen/OpSub bytecode, not a
// hand-constructed negative literal. Before the fix this panicked
// inside SliceVal with "slice bounds out of range [:-1]".
func TestVMOpSliceEmptyArrayNegativeEndDoesNotPanic(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpComposite, Value: "slice"},                  // 0: items := []T{} (empty)
		{Op: program.OpLen, Operands: []program.ExprID{0}},         // 1: len(items) == 0
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},  // 2
		{Op: program.OpSub, Operands: []program.ExprID{1, 2}},      // 3: len(items)-1 == -1
		{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},  // 4: start
		{Op: program.OpSlice, Operands: []program.ExprID{0, 4, 3}}, // 5: items[0:len(items)-1]
	})
	vm := NewVM(prog, nil)
	got := vm.Eval(5)
	if len(got.List()) != 0 {
		t.Fatalf("items[0:len(items)-1] on empty items = %+v, want 0 items", got)
	}
}

// TestVMOpSubstringEmptyStringNegativeEndDoesNotPanic reproduces
// `name[0:len(name)-1]` where name is "". Before the fix this panicked
// inside SubstringVal the same way SliceVal did.
func TestVMOpSubstringEmptyStringNegativeEndDoesNotPanic(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitString, Value: "", Type: program.TypeString}, // 0: name := ""
		{Op: program.OpLen, Operands: []program.ExprID{0}},             // 1: len(name) == 0
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},      // 2
		{Op: program.OpSub, Operands: []program.ExprID{1, 2}},          // 3: len(name)-1 == -1
		{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},      // 4: start
		{Op: program.OpSubstring, Operands: []program.ExprID{0, 4, 3}}, // 5: name[0:len(name)-1]
	})
	vm := NewVM(prog, nil)
	got := vm.Eval(5)
	if got.Text() != "" {
		t.Fatalf("name[0:len(name)-1] on empty name = %q, want \"\"", got.Text())
	}
}

// TestVMOpSliceFloatOverflowEndDoesNotPanic reproduces the "end
// becomes +Inf" path. It uses a genuine runtime float overflow
// (OpMul on two large literals), not a hand-built literal.
// 1e300*1e300 exceeds float64's range. IEEE 754 rounds that to
// +Inf, matching how a surface expression like `1e300 * scale`
// could overflow at runtime. int(+Inf) is MinInt64 on amd64. Before
// the fix, this reached v.Items[start:end] as a negative index and
// panicked with "slice bounds out of range
// [:-9223372036854775808]".
func TestVMOpSliceFloatOverflowEndDoesNotPanic(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitFloat, Value: "1e300", Type: program.TypeFloat},                 // 0
		{Op: program.OpLitFloat, Value: "1e300", Type: program.TypeFloat},                 // 1
		{Op: program.OpMul, Operands: []program.ExprID{0, 1}},                             // 2: +Inf
		{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},                         // 3: composite index 0
		{Op: program.OpLitInt, Value: "10", Type: program.TypeInt},                        // 4: composite value 0
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},                         // 5: composite index 1
		{Op: program.OpLitInt, Value: "20", Type: program.TypeInt},                        // 6: composite value 1
		{Op: program.OpComposite, Value: "slice", Operands: []program.ExprID{3, 4, 5, 6}}, // 7: [10, 20]
		{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},                         // 8: start
		{Op: program.OpSlice, Operands: []program.ExprID{7, 8, 2}},                        // 9: arr[0:+Inf]
	})
	vm := NewVM(prog, nil)
	got := vm.Eval(9)
	if len(got.List()) != 2 || int(got.List()[0].num) != 10 || int(got.List()[1].num) != 20 {
		t.Fatalf("arr[0:+Inf] = %+v, want the full [10, 20] array (end clamped to len)", got)
	}
}

// TestEvalSelfReferentialExprDoesNotOverflow reproduces an
// expression graph whose first operand refers to itself. It has no
// OpIndirectCall or closure frame anywhere in it. So
// Program.MaxCallDepth (Y.D) cannot bound this recursion, because
// it never calls a function. Before Eval grew its own depth guard,
// this ran until the goroutine stack overflowed with a fatal
// (unrecoverable) error.
//
// Only the first operand self-refers. The second is a plain
// literal. So the recursion is linear, not exponential, and the
// test completes quickly even while it exercises the full depth
// cap.
//
// The result's exact value isn't meaningful here. Once the guard
// fires at maxEvalDepth, the zero it returns for the innermost call
// flows back up through maxEvalDepth Add() calls against the
// literal operand. It lands on some large but finite float. What
// matters is that Eval returns at all, instead of crashing, and
// records the eval_depth_exceeded diagnostic exactly once.
func TestEvalSelfReferentialExprDoesNotOverflow(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpAdd, Operands: []program.ExprID{0, 1}}, // 0: refers to itself + a literal
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},
	})
	vm := NewVM(prog, nil)
	vm.Eval(0) // must return, not crash
	requireDiag(t, vm, "eval_depth_exceeded")
}

// TestEvalDeepButFiniteExprStillEvaluates guards against an
// overcorrection. A legitimately deep, but finite and non-cyclic,
// chain of nested OpAdd expressions sits well under maxEvalDepth.
// It must still evaluate to the correct value, rather than getting
// cut off by the new depth guard.
func TestEvalDeepButFiniteExprStillEvaluates(t *testing.T) {
	const depth = 500
	exprs := make([]program.Expr, 0, depth+1)
	exprs = append(exprs, program.Expr{Op: program.OpLitInt, Value: "1", Type: program.TypeInt}) // 0
	for i := 1; i <= depth; i++ {
		exprs = append(exprs, program.Expr{
			Op:       program.OpAdd,
			Operands: []program.ExprID{program.ExprID(i - 1), 0}, // running += 1, depth deep
		})
	}
	vm := NewVM(progFromExprs(exprs), nil)
	got := vm.Eval(program.ExprID(depth))
	if int(got.num) != depth+1 {
		t.Fatalf("deep-but-finite chain = %d, want %d", int(got.num), depth+1)
	}
	if len(vm.Diagnostics()) != 0 {
		t.Fatalf("deep-but-finite chain should not trip the depth guard, got diagnostics: %+v", vm.Diagnostics())
	}
}
