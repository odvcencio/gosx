// Slice Y.G — OpClosure + InvokeClosure benchmarks.
//
// Closures sit on the hot path of any engine surface that drives a
// per-frame animation loop (e.g. graph_surface.go's
// `c.StartLoop(func(dt) { stepLayout(); draw() })`). Pin the costs
// for OpClosure allocation and InvokeClosure dispatch so a future
// refactor that regresses either is visible immediately.

package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

// BenchmarkOpClosureNoCapture measures the cost of allocating a
// ClosureVal with no captured locals. This is the lower bound on
// closure construction: just the synth-name string + an empty
// closureRef.
func BenchmarkOpClosureNoCapture(b *testing.B) {
	prog := &program.Program{
		Funcs: []program.FuncDef{
			{Name: "__y_g_noop", Params: nil, Body: []program.ExprID{0}, Results: 0},
		},
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},
			{Op: program.OpClosure, Value: "__y_g_noop"},
		},
	}
	vm := NewVM(prog, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = vm.Eval(1)
	}
}

// BenchmarkInvokeClosureZeroArg measures the steady-state cost of
// dispatching into a pre-built closure with no args and a trivial
// body. Comparison point: Y.D's OpIndirectCall zero-arg is ~286 ns;
// closures add the captured-frame bridge but should stay in the same
// order of magnitude.
func BenchmarkInvokeClosureZeroArg(b *testing.B) {
	prog := &program.Program{
		Funcs: []program.FuncDef{
			{Name: "__y_g_noop", Params: nil, Body: []program.ExprID{0}, Results: 0},
		},
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},
		},
	}
	vm := NewVM(prog, nil)
	cv := ClosureVal("__y_g_noop", nil, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = vm.InvokeClosure(cv, nil)
	}
}

// BenchmarkInvokeClosureCapturedWrite measures the canonical
// graph_surface case: a closure that captures one local and increments
// it. Goes through the captured-frame bridge once per invocation.
func BenchmarkInvokeClosureCapturedWrite(b *testing.B) {
	prog := &program.Program{
		Funcs: []program.FuncDef{
			{Name: "__y_g_incCount", Params: nil, Body: []program.ExprID{3}, Results: 0},
		},
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},             // 0
			{Op: program.OpLocalGet, Value: "count"},                              // 1
			{Op: program.OpAdd, Operands: []program.ExprID{1, 0}},                 // 2
			{Op: program.OpAssign, Value: "count", Operands: []program.ExprID{2}}, // 3
		},
	}
	vm := NewVM(prog, nil)
	parent := newFrame()
	parent.declare("count")
	parent.set("count", IntVal(0))
	cv := ClosureVal("__y_g_incCount", []string{"count"}, parent)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = vm.InvokeClosure(cv, nil)
	}
}
