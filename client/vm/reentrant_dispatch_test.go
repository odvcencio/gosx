package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

type reentrantIslandHost struct {
	island  *Island
	nested  []PatchOp
	invoked int
}

func (host *reentrantIslandHost) Call(string, []Value) (Value, error) {
	host.invoked++
	host.nested = host.island.Dispatch("inner", `{"value":"nested"}`)
	return ZeroValue(program.TypeAny), nil
}

func reentrantDispatchProgram() *program.Program {
	return &program.Program{
		Name: "ReentrantDispatch",
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},
			{Op: program.OpSignalGet, Value: "count", Type: program.TypeInt},
			{Op: program.OpHostCall, Value: "test.Reenter", Type: program.TypeAny},
			{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},
			{Op: program.OpSignalSet, Value: "count", Operands: []program.ExprID{3}, Type: program.TypeInt},
			{Op: program.OpLitInt, Value: "99", Type: program.TypeInt},
			{Op: program.OpSignalSet, Value: "count", Operands: []program.ExprID{5}, Type: program.TypeInt},
		},
		Signals: []program.SignalDef{{Name: "count", Type: program.TypeInt, Init: 0}},
		Handlers: []program.Handler{
			{Name: "outer", Body: []program.ExprID{2, 4}},
			{Name: "inner", Body: []program.ExprID{6}},
		},
		Nodes: []program.Node{{Kind: program.NodeExpr, Expr: 1}},
		Root:  0,
	}
}

func TestIslandRejectsSameIslandHostReentrantDispatch(t *testing.T) {
	island := NewIsland(reentrantDispatchProgram(), `{}`)
	host := &reentrantIslandHost{island: island}
	island.BindHost("test", host)

	patches := island.Dispatch("outer", `{"value":"outer"}`)
	if host.invoked != 1 {
		t.Fatalf("host calls = %d, want 1", host.invoked)
	}
	if host.nested != nil {
		t.Fatalf("nested dispatch patches = %#v, want nil", host.nested)
	}
	if len(patches) != 1 || patches[0].Kind != PatchSetText || patches[0].Text != "1" {
		t.Fatalf("outer dispatch patches = %#v, want one SetText(1)", patches)
	}
	if island.vm.eventData != nil || island.dispatching {
		t.Fatal("outer dispatch did not clear its event/dispatch state")
	}
	for _, diagnostic := range island.vm.Diagnostics() {
		if diagnostic.Code == "reentrant_dispatch" {
			return
		}
	}
	t.Fatalf("missing reentrant_dispatch diagnostic: %+v", island.vm.Diagnostics())
}
