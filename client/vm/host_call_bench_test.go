// Slice Y.E benchmarks for OpMake + OpHostCall.
//
// These document the per-operation cost so the WASM-size + frame-budget
// gates (Phase 1d) have concrete data to assess against. The
// expectation is the cost stays well within 60 fps frame budget
// (16ms / frame) even at canvas dispatch rates of hundreds of calls
// per frame (graph_surface.go's draw issues ~5 calls per node * up
// to 200 nodes = ~1000 calls per frame).

package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

func BenchmarkOpMakeMap(b *testing.B) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpMake, Value: "map"},
		},
	}
	vm := NewVM(prog, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = vm.Eval(0)
	}
}

func BenchmarkOpMakeSliceSized(b *testing.B) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: "16", Type: program.TypeInt}, // id 0
			{Op: program.OpMake, Value: "slice", Operands: []program.ExprID{0}},
		},
	}
	vm := NewVM(prog, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = vm.Eval(1)
	}
}

func BenchmarkOpHostCallZeroArg(b *testing.B) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpHostCall, Value: "c.BeginPath"},
		},
	}
	vm := NewVM(prog, nil)
	vm.BindHost("c", &noopHost{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = vm.Eval(0)
	}
}

func BenchmarkOpHostCallTwoArgs(b *testing.B) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLitFloat, Value: "1.5", Type: program.TypeFloat}, // id 0
			{Op: program.OpLitFloat, Value: "2.5", Type: program.TypeFloat}, // id 1
			{Op: program.OpHostCall, Value: "c.MoveTo", Operands: []program.ExprID{0, 1}},
		},
	}
	vm := NewVM(prog, nil)
	vm.BindHost("c", &noopHost{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = vm.Eval(2)
	}
}

// BenchmarkOpHostCallDrawShape mirrors the inner loop of
// graph_surface.go's draw handler: 4 canvas calls per edge.
func BenchmarkOpHostCallDrawShape(b *testing.B) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLitFloat, Value: "1.0", Type: program.TypeFloat},                 // id 0
			{Op: program.OpLitFloat, Value: "2.0", Type: program.TypeFloat},                 // id 1
			{Op: program.OpLitFloat, Value: "3.0", Type: program.TypeFloat},                 // id 2
			{Op: program.OpLitFloat, Value: "4.0", Type: program.TypeFloat},                 // id 3
			{Op: program.OpHostCall, Value: "c.BeginPath"},                                  // id 4
			{Op: program.OpHostCall, Value: "c.MoveTo", Operands: []program.ExprID{0, 1}},   // id 5
			{Op: program.OpHostCall, Value: "c.LineTo", Operands: []program.ExprID{2, 3}},   // id 6
			{Op: program.OpHostCall, Value: "c.Stroke"},                                     // id 7
			{Op: program.OpSeq, Operands: []program.ExprID{4, 5, 6, 7}},                     // id 8
		},
	}
	vm := NewVM(prog, nil)
	vm.BindHost("c", &noopHost{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = vm.Eval(8)
	}
}

// noopHost is a HostReceiver that does nothing. Used by the
// benchmarks so the dispatch cost is measured in isolation from the
// host work.
type noopHost struct{}

func (*noopHost) Call(method string, args []Value) (Value, error) {
	return Value{}, nil
}
