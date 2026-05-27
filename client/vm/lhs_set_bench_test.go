// Slice Y.C — benchmarks for OpFieldSet / OpIndexSet. The Y.C handlers
// run once per LHS assignment in lowered Go, so their cost is scaled by
// handler invocation count and the number of writes in tight loops (the
// stepLayout repulsion loop in graph_surface.go does N×N OpIndexSet
// writes per frame). Target: a single write should be a few hundred
// nanoseconds at most so 60 FPS with O(N²) writes on a 100-node graph
// (10k writes/frame) stays well under the 16 ms frame budget.

package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

// BenchmarkFieldSet measures the steady-state cost of OpFieldSet
// against a fixed-size struct (Fields-backed ObjectVal). Setup builds
// the program once; the bench loop measures Eval + the map write.
func BenchmarkFieldSet(b *testing.B) {
	// Program: OpFieldSet v.X = 42
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLocalGet, Value: "v"},                                            // 0
			{Op: program.OpLitFloat, Value: "42", Type: program.TypeFloat},                  // 1
			{Op: program.OpFieldSet, Value: "X", Operands: []program.ExprID{0, 1}},          // 2
		},
	}
	machine := NewVM(prog, nil)
	machine.frame = newFrame()
	machine.frame.declare("v")
	machine.frame.set("v", ObjectVal(map[string]Value{
		"X": FloatVal(0), "Y": FloatVal(0), "Z": FloatVal(0),
	}))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = machine.Eval(2)
	}
}

// BenchmarkIndexSetSlice measures the slice-branch cost of OpIndexSet.
// The 100-element slice mimics the order-of-magnitude `fx[]` array
// stepLayout writes into per frame.
func BenchmarkIndexSetSlice(b *testing.B) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLocalGet, Value: "s"},                                            // 0
			{Op: program.OpLitInt, Value: "50", Type: program.TypeInt},                      // 1
			{Op: program.OpLitFloat, Value: "3.14", Type: program.TypeFloat},                // 2
			{Op: program.OpIndexSet, Operands: []program.ExprID{0, 1, 2}},                   // 3
		},
	}
	items := make([]Value, 100)
	for i := range items {
		items[i] = FloatVal(0)
	}
	machine := NewVM(prog, nil)
	machine.frame = newFrame()
	machine.frame.declare("s")
	machine.frame.set("s", ArrayVal(items))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = machine.Eval(3)
	}
}

// BenchmarkIndexSetMap measures the map-branch cost of OpIndexSet.
// The 100-entry map mimics gPos (one entry per graph node) and uses
// a key that always overwrites the same slot so the bench measures
// the write itself, not key creation.
func BenchmarkIndexSetMap(b *testing.B) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLocalGet, Value: "m"},                                            // 0
			{Op: program.OpLitString, Value: "key_42", Type: program.TypeString},            // 1
			{Op: program.OpLitFloat, Value: "1.5", Type: program.TypeFloat},                 // 2
			{Op: program.OpIndexSet, Operands: []program.ExprID{0, 1, 2}},                   // 3
		},
	}
	fields := make(map[string]Value, 100)
	for i := 0; i < 100; i++ {
		fields[mapKey(i)] = FloatVal(0)
	}
	machine := NewVM(prog, nil)
	machine.frame = newFrame()
	machine.frame.declare("m")
	machine.frame.set("m", ObjectVal(fields))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = machine.Eval(3)
	}
}

func mapKey(i int) string {
	const digits = "0123456789"
	return "key_" + digits[i/10:i/10+1] + digits[i%10:i%10+1]
}
