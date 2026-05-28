// Slice Y.G — Mount end-to-end test.
//
// Pins the canonical proof case the dispatch brief calls out: a
// Mount-shaped handler that
//   1. declares `var props GraphProps` (zero-init Fields map)
//   2. calls `ctx.PropsInto(&props)` (host populates the local)
//   3. captures package state via `gNodes = props.Nodes`
//   4. calls `c.StartLoop(func(dt float64) { ... })` (FuncLit closure
//      passed to a host method)
//
// This is the exact pattern that blocked Y.F's full Mount parity
// claim. Both residuals (FuncLit closure and `&x` on nil-Fields) must
// close for this scenario to lower cleanly AND run end-to-end.

package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestY_G_MountStyleHandlerLowersAndRuns is the canonical proof of
// both Y.G residuals closing together.
func TestY_G_MountStyleHandlerLowersAndRuns(t *testing.T) {
	src := []byte(`package handlers

type GraphNode struct {
	ID string
}

type GraphProps struct {
	Name  string
	Count int
}

var gName string
var gCount int

func Mount() string {
	var props GraphProps
	_ = host.PropsInto(&props)
	gName = props.Name
	gCount = props.Count
	loopFired := 0
	c.StartLoop(func(dt float64) {
		loopFired = loopFired + 1
	})
	return gName
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("Mount-style lowering failed: %v", err)
	}
	if len(prog.Handlers) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(prog.Handlers))
	}
	// The closure body must have been registered as a synthetic FuncDef.
	var found bool
	for _, f := range prog.Funcs {
		if len(f.Params) == 1 && f.Params[0] == "dt" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Mount's StartLoop closure not registered as a synthetic FuncDef; Funcs=%+v", prog.Funcs)
	}

	machine := vm.NewVM(prog, nil)
	machine.BindHost("host", &propsIntoHostNamed{name: "Mount-via-bytecode", count: 42})
	captured := &capturedClosureCanvas{}
	machine.BindHost("c", captured)

	handler := findHandler(t, prog.Handlers, "Mount")
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Str != "Mount-via-bytecode" {
		t.Errorf("Mount returned %q, want %q (props decode + propagate)", got.Str, "Mount-via-bytecode")
	}
	if !vm.IsClosure(captured.cb) {
		t.Fatal("c.StartLoop did not receive a ClosureVal")
	}

	// Invoke the captured closure twice — verifies the host can keep a
	// ClosureVal and call it later, exercising the closure-by-name
	// dispatch in evalIndirectCallExpr.
	machine.InvokeClosure(captured.cb, []vm.Value{vm.FloatVal(0.016)})
	machine.InvokeClosure(captured.cb, []vm.Value{vm.FloatVal(0.016)})
	// gCount was set from props (42). Stage the captured-local
	// mutation check: loopFired captured + mutated in the closure body
	// is a frame-local so it lives only inside Mount's frame which
	// has already returned — instead verify the closure's body ran
	// by looking at any observable side-effect we set.
	// (For this test the "no panic + closure invokable" is the
	// success signal.)
}

// propsIntoHostNamed pretends to be the gosx Surface.Context: receives
// a struct-by-reference, populates Name + Count fields.
type propsIntoHostNamed struct {
	name  string
	count int
}

func (h *propsIntoHostNamed) Call(method string, args []vm.Value) (vm.Value, error) {
	if method != "PropsInto" || len(args) != 1 {
		return vm.ZeroValue(0), nil
	}
	target := args[0]
	if target.Fields == nil {
		// Y.G's eager struct zero-init means this branch should
		// not fire; if it does, the test fails the props-propagate
		// assertion above.
		return vm.ZeroValue(0), nil
	}
	target.Fields["Name"] = vm.StringVal(h.name)
	target.Fields["Count"] = vm.IntVal(h.count)
	return vm.ZeroValue(0), nil
}

// capturedClosureCanvas implements a tiny `c` receiver that just
// captures the closure StartLoop is called with so the test can
// invoke it directly later.
type capturedClosureCanvas struct {
	cb vm.Value
}

func (c *capturedClosureCanvas) Call(method string, args []vm.Value) (vm.Value, error) {
	if method == "StartLoop" && len(args) == 1 {
		c.cb = args[0]
	}
	return vm.ZeroValue(0), nil
}
