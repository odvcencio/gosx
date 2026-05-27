// Slice Y.D — benchmarks for OpIndirectCall. User function calls are
// the heaviest opcode in the Y series: each call pushes a fresh frame
// (one map alloc per dispatch) and binds parameters, so even a
// 100ns/call cost compounds in tight graph-surface helper loops. Y.D
// targets the same "fits in 60fps budget" envelope as Y.C: thousands
// of calls per frame should still leave headroom for the lowerer's
// other opcodes.

package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

// BenchmarkIndirectCallNoArg measures the bare dispatch cost: no
// arguments to evaluate, no return-value carrier shaping, just the
// frame push/pop + body eval.
func BenchmarkIndirectCallNoArg(b *testing.B) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: "42", Type: program.TypeInt},     // 0
			{Op: program.OpReturn, Operands: []program.ExprID{0}},          // 1
			{Op: program.OpSeq, Operands: []program.ExprID{1}},             // 2 body
			{Op: program.OpIndirectCall, Value: "f"},                       // 3
		},
		Funcs: []program.FuncDef{
			{Name: "f", Params: nil, Body: []program.ExprID{2}, Results: 1},
		},
	}
	machine := NewVM(prog, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = machine.Eval(3)
	}
}

// BenchmarkIndirectCallTwoArgs measures the typical
// graph_surface.go helper shape: two-arg call with a scalar return.
// Tracks the marginal cost of argument evaluation + parameter
// binding on top of BenchmarkIndirectCallNoArg.
func BenchmarkIndirectCallTwoArgs(b *testing.B) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLocalGet, Value: "a"},                            // 0
			{Op: program.OpLocalGet, Value: "b"},                            // 1
			{Op: program.OpAdd, Operands: []program.ExprID{0, 1}},           // 2
			{Op: program.OpReturn, Operands: []program.ExprID{2}},           // 3
			{Op: program.OpSeq, Operands: []program.ExprID{3}},              // 4 body
			{Op: program.OpLitInt, Value: "3", Type: program.TypeInt},       // 5
			{Op: program.OpLitInt, Value: "4", Type: program.TypeInt},       // 6
			{Op: program.OpIndirectCall, Value: "add", Operands: []program.ExprID{5, 6}}, // 7
		},
		Funcs: []program.FuncDef{
			{Name: "add", Params: []string{"a", "b"}, Body: []program.ExprID{4}, Results: 1},
		},
	}
	machine := NewVM(prog, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = machine.Eval(7)
	}
}

// BenchmarkIndirectCallRecursive measures the dispatch cost in a
// recursive call chain. n=8 keeps allocations modest while still
// exercising the call-depth tracker. The arithmetic is intentionally
// trivial so the bench measures dispatch overhead, not work.
func BenchmarkIndirectCallRecursive(b *testing.B) {
	// fact-like recursion with depth 8.
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLocalGet, Value: "n"},                            // 0
			{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},       // 1
			{Op: program.OpLte, Operands: []program.ExprID{0, 1}},           // 2 n<=1
			{Op: program.OpReturn, Operands: []program.ExprID{1}},           // 3 return 1
			{Op: program.OpSub, Operands: []program.ExprID{0, 1}},           // 4 n-1
			{Op: program.OpIndirectCall, Value: "fact", Operands: []program.ExprID{4}}, // 5 fact(n-1)
			{Op: program.OpMul, Operands: []program.ExprID{0, 5}},           // 6 n*fact(n-1)
			{Op: program.OpReturn, Operands: []program.ExprID{6}},           // 7
			{Op: program.OpCond, Operands: []program.ExprID{2, 3, 7}},       // 8 if
			{Op: program.OpSeq, Operands: []program.ExprID{8}},              // 9 body
			{Op: program.OpLitInt, Value: "8", Type: program.TypeInt},       // 10
			{Op: program.OpIndirectCall, Value: "fact", Operands: []program.ExprID{10}}, // 11 fact(8)
		},
		Funcs: []program.FuncDef{
			{Name: "fact", Params: []string{"n"}, Body: []program.ExprID{9}, Results: 1},
		},
	}
	machine := NewVM(prog, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = machine.Eval(11)
	}
}
