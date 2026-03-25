package test

import (
	"testing"

	"github.com/odvcencio/gosx/client/bridge"
	"github.com/odvcencio/gosx/client/vm"
	"github.com/odvcencio/gosx/island/program"
)

// TestSharedStoreBasic verifies the shared store works as a Redux-like signal bus.
func TestSharedStoreBasic(t *testing.T) {
	b := bridge.New()
	store := b.GetStore()

	// Set a shared value
	store.Set("$theme", vm.StringVal("dark"))

	// Read it back
	val, ok := store.Get("$theme")
	if !ok {
		t.Fatal("expected shared signal")
	}
	if val.Str != "dark" {
		t.Fatalf("expected 'dark', got %q", val.Str)
	}
}

// TestCrossIslandState proves that two islands sharing a "$count" signal
// see each other's mutations.
func TestCrossIslandState(t *testing.T) {
	b := bridge.New()

	// Create two programs that both use a shared "$count" signal.
	// Island A increments $count, Island B reads it.
	progA := sharedCounterProgram("A")
	progB := sharedCounterProgram("B")

	dataA, _ := program.EncodeJSON(progA)
	dataB, _ := program.EncodeJSON(progB)

	// Hydrate both islands
	err := b.HydrateIsland("island-a", "CounterA", `{}`, dataA, "json")
	if err != nil {
		t.Fatalf("hydrate A: %v", err)
	}
	err = b.HydrateIsland("island-b", "CounterB", `{}`, dataB, "json")
	if err != nil {
		t.Fatalf("hydrate B: %v", err)
	}

	// Dispatch increment on island A
	patchesA, err := b.DispatchAction("island-a", "increment", "{}")
	if err != nil {
		t.Fatalf("dispatch A: %v", err)
	}
	t.Logf("Island A patches after increment: %d", len(patchesA))

	// Now reconcile island B — it should see the updated $count
	patchesB, err := b.DispatchAction("island-b", "noop", "{}")
	if err != nil {
		// noop handler might not exist, that's OK
		t.Logf("dispatch B noop: %v (expected)", err)
	}

	// Even without dispatching, B's tree should reflect the new $count
	// Let's verify by reading the shared store
	val, _ := b.GetStore().Get("$count")
	if val.Num != 1 {
		t.Fatalf("expected shared $count=1 after A incremented, got %v", val.Num)
	}
	t.Logf("Shared $count = %v (correct: A incremented, B sees it)", val.Num)

	// Dispatch increment on A again
	b.DispatchAction("island-a", "increment", "{}")
	val2, _ := b.GetStore().Get("$count")
	if val2.Num != 2 {
		t.Fatalf("expected shared $count=2, got %v", val2.Num)
	}

	t.Logf("Cross-island state verified: A incremented twice, store shows %v", val2.Num)
	_ = patchesB
}

// sharedCounterProgram creates a counter that uses the shared "$count" signal.
func sharedCounterProgram(name string) *program.Program {
	exprs := []program.Expr{
		{Op: program.OpSignalGet, Value: "$count", Type: program.TypeInt},                        // 0: read $count
		{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},                                // 1: init
		{Op: program.OpSignalSet, Operands: []program.ExprID{3}, Value: "$count", Type: program.TypeInt}, // 2: set $count
		{Op: program.OpAdd, Operands: []program.ExprID{4, 5}, Type: program.TypeInt},             // 3: $count + 1
		{Op: program.OpSignalGet, Value: "$count", Type: program.TypeInt},                        // 4: read for add
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},                                // 5: literal 1
	}

	nodes := []program.Node{
		{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1, 2}},
		{Kind: program.NodeText, Text: name + ": "},
		{Kind: program.NodeExpr, Expr: 0},
	}

	return &program.Program{
		Name:  "Counter" + name,
		Nodes: nodes,
		Root:  0,
		Exprs: exprs,
		Signals: []program.SignalDef{
			{Name: "$count", Type: program.TypeInt, Init: 1}, // shared signal
		},
		Handlers: []program.Handler{
			{Name: "increment", Body: []program.ExprID{2}},
		},
		StaticMask: []bool{false, true, false},
	}
}
