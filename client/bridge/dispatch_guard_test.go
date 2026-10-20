package bridge

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
)

type bridgeDispatchHost struct {
	call func() error
}

func (host *bridgeDispatchHost) Call(string, []vm.Value) (vm.Value, error) {
	return vm.ZeroValue(program.TypeAny), host.call()
}

func encodeDispatchProgram(t *testing.T, prog *program.Program) []byte {
	t.Helper()
	data, err := program.EncodeJSON(prog)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBridgeRejectsSameIslandReentrantDispatch(t *testing.T) {
	b := New()
	var nestedErr error
	b.RegisterIslandHostFactory("loop", func(islandID string) vm.HostReceiver {
		return &bridgeDispatchHost{call: func() error {
			_, nestedErr = b.DispatchAction(islandID, "inner", `{}`)
			return nil
		}}
	})
	prog := &program.Program{
		Name: "BridgeReentry",
		Exprs: []program.Expr{
			{Op: program.OpHostCall, Value: "loop.Dispatch", Type: program.TypeAny},
		},
		Handlers: []program.Handler{
			{Name: "outer", Body: []program.ExprID{0}},
			{Name: "inner"},
		},
		Nodes: []program.Node{{Kind: program.NodeElement, Tag: "div"}},
	}
	if err := b.HydrateIsland("island-a", prog.Name, `{}`, encodeDispatchProgram(t, prog), "json"); err != nil {
		t.Fatal(err)
	}

	if _, err := b.DispatchAction("island-a", "outer", `{}`); err != nil {
		t.Fatalf("outer dispatch: %v", err)
	}
	if nestedErr == nil || !strings.Contains(nestedErr.Error(), "already dispatching") {
		t.Fatalf("nested dispatch error = %v, want explicit already-dispatching error", nestedErr)
	}
	if len(b.dispatching) != 0 {
		t.Fatalf("active dispatches after outer return = %+v, want empty", b.dispatching)
	}
}

func sharedDispatchProgram(name, handler string, body []program.ExprID, exprs []program.Expr) *program.Program {
	return &program.Program{
		Name: name,
		Exprs: append([]program.Expr{
			{Op: program.OpSignalGet, Value: "$shared.value", Type: program.TypeInt},
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},
		}, exprs...),
		Signals:  []program.SignalDef{{Name: "$shared.value", Type: program.TypeInt, Init: 1}},
		Handlers: []program.Handler{{Name: handler, Body: body}},
		Nodes:    []program.Node{{Kind: program.NodeExpr, Expr: 0}},
		Root:     0,
	}
}

func TestBridgeCrossIslandNestedDispatchPreservesAllActiveIslands(t *testing.T) {
	b := New()
	var nestedPatches []vm.PatchOp
	var nestedErr error
	b.RegisterIslandHostFactory("cross", func(string) vm.HostReceiver {
		return &bridgeDispatchHost{call: func() error {
			nestedPatches, nestedErr = b.DispatchAction("island-b", "inner", `{}`)
			if b.dispatching["island-a"] != 1 {
				t.Fatalf("outer island activity was not restored after nested dispatch: %+v", b.dispatching)
			}
			return nestedErr
		}}
	})

	// Expr IDs 2..4 are appended after the shared read/init expressions.
	progA := sharedDispatchProgram("Outer", "outer", []program.ExprID{2, 4}, []program.Expr{
		{Op: program.OpHostCall, Value: "cross.DispatchB", Type: program.TypeAny},                               // 2
		{Op: program.OpLitInt, Value: "2", Type: program.TypeInt},                                               // 3
		{Op: program.OpSignalSet, Value: "$shared.value", Operands: []program.ExprID{3}, Type: program.TypeInt}, // 4
	})
	progB := sharedDispatchProgram("Inner", "inner", []program.ExprID{3}, []program.Expr{
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},                                               // 2
		{Op: program.OpSignalSet, Value: "$shared.value", Operands: []program.ExprID{2}, Type: program.TypeInt}, // 3
	})
	if err := b.HydrateIsland("island-a", progA.Name, `{}`, encodeDispatchProgram(t, progA), "json"); err != nil {
		t.Fatal(err)
	}
	if err := b.HydrateIsland("island-b", progB.Name, `{}`, encodeDispatchProgram(t, progB), "json"); err != nil {
		t.Fatal(err)
	}

	var pushed []string
	b.SetPatchCallback(func(islandID, _ string) { pushed = append(pushed, islandID) })
	outerPatches, err := b.DispatchAction("island-a", "outer", `{}`)
	if err != nil {
		t.Fatalf("outer dispatch: %v", err)
	}
	if nestedErr != nil {
		t.Fatalf("cross-island nested dispatch: %v", nestedErr)
	}
	if len(nestedPatches) != 1 || nestedPatches[0].Text != "1" {
		t.Fatalf("nested island patches = %#v, want text 1", nestedPatches)
	}
	if len(outerPatches) != 1 || outerPatches[0].Text != "2" {
		t.Fatalf("outer island patches = %#v, want text 2", outerPatches)
	}
	// The first shared write occurs while A and B are active, so neither is
	// pushed. The second occurs after B returns but while A remains active, so
	// exactly B is reconciled by the shared-signal callback.
	if len(pushed) != 1 || pushed[0] != "island-b" {
		t.Fatalf("shared callback patch islands = %v, want [island-b]", pushed)
	}
	if len(b.dispatching) != 0 {
		t.Fatalf("active dispatches after nested return = %+v, want empty", b.dispatching)
	}
}
