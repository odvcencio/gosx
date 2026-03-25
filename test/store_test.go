package test

import (
	"sync"
	"testing"

	"github.com/odvcencio/gosx/client/bridge"
	"github.com/odvcencio/gosx/client/vm"
	"github.com/odvcencio/gosx/island/program"
)

// TestSharedStoreBasic verifies typed signal creation and read-back.
func TestSharedStoreBasic(t *testing.T) {
	b := bridge.New()
	store := b.GetStore()

	store.Set("$theme", vm.StringVal("dark"))

	val, ok := store.Get("$theme")
	if !ok {
		t.Fatal("expected shared signal")
	}
	if val.Str != "dark" {
		t.Fatalf("expected 'dark', got %q", val.Str)
	}
}

// TestSharedStoreTypedInit verifies that auto-created signals honor the
// declared type, not default to StringVal("").
func TestSharedStoreTypedInit(t *testing.T) {
	b := bridge.New()

	prog := sharedCounterProgram("X")
	data, _ := program.EncodeJSON(prog)

	// Hydrate — this should create $count with init value 0 (IntVal),
	// NOT StringVal("").
	err := b.HydrateIsland("test-0", "CounterX", `{}`, data, "json")
	if err != nil {
		t.Fatal(err)
	}

	val, ok := b.GetStore().Get("$count")
	if !ok {
		t.Fatal("$count not created")
	}
	if val.Type != program.TypeInt {
		t.Fatalf("expected TypeInt, got %d", val.Type)
	}
	if val.Num != 0 {
		t.Fatalf("expected init value 0, got %v", val.Num)
	}
}

// TestCrossIslandReRender proves that when island A mutates a shared signal,
// island B automatically re-renders and produces patches — without any
// explicit dispatch on B.
func TestCrossIslandReRender(t *testing.T) {
	b := bridge.New()

	// Collect patches pushed via the callback
	var mu sync.Mutex
	patchLog := make(map[string][]string) // islandID -> patch JSONs

	b.SetPatchCallback(func(islandID, patchJSON string) {
		mu.Lock()
		patchLog[islandID] = append(patchLog[islandID], patchJSON)
		mu.Unlock()
	})

	progA := sharedCounterProgram("A")
	progB := sharedCounterProgram("B")
	dataA, _ := program.EncodeJSON(progA)
	dataB, _ := program.EncodeJSON(progB)

	b.HydrateIsland("island-a", "CounterA", `{}`, dataA, "json")
	b.HydrateIsland("island-b", "CounterB", `{}`, dataB, "json")

	// Dispatch increment on A only
	patchesA, err := b.DispatchAction("island-a", "increment", "{}")
	if err != nil {
		t.Fatal(err)
	}

	// A should produce patches (it dispatched the handler)
	if len(patchesA) == 0 {
		t.Fatal("expected patches from island A dispatch")
	}
	t.Logf("A dispatch patches: %d", len(patchesA))

	// B should have received patches via the callback — the shared $count
	// changed, B's subscription fired, B reconciled and pushed patches.
	mu.Lock()
	bPatches := patchLog["island-b"]
	mu.Unlock()

	if len(bPatches) == 0 {
		t.Fatal("CRITICAL: island B did NOT re-render when shared $count changed. " +
			"Cross-island reactivity is not wired.")
	}
	t.Logf("B auto-re-render patches: %s", bPatches[0])

	// Verify the store value
	val, _ := b.GetStore().Get("$count")
	if val.Num != 1 {
		t.Fatalf("expected $count=1, got %v", val.Num)
	}

	// Increment again — B should get another patch
	b.DispatchAction("island-a", "increment", "{}")

	mu.Lock()
	bPatches2 := patchLog["island-b"]
	mu.Unlock()

	if len(bPatches2) < 2 {
		t.Fatalf("expected 2 patch deliveries to B, got %d", len(bPatches2))
	}

	val2, _ := b.GetStore().Get("$count")
	t.Logf("After 2 increments: $count=%v, B received %d patch sets", val2.Num, len(bPatches2))
}

// sharedCounterProgram creates a counter that uses the shared "$count" signal.
func sharedCounterProgram(name string) *program.Program {
	exprs := []program.Expr{
		{Op: program.OpSignalGet, Value: "$count", Type: program.TypeInt},                                // 0: read $count
		{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},                                        // 1: init value
		{Op: program.OpSignalSet, Operands: []program.ExprID{3}, Value: "$count", Type: program.TypeInt}, // 2: set $count
		{Op: program.OpAdd, Operands: []program.ExprID{4, 5}, Type: program.TypeInt},                     // 3: $count + 1
		{Op: program.OpSignalGet, Value: "$count", Type: program.TypeInt},                                // 4: read for add
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},                                        // 5: literal 1
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
			{Name: "$count", Type: program.TypeInt, Init: 1},
		},
		Handlers: []program.Handler{
			{Name: "increment", Body: []program.ExprID{2}},
		},
		StaticMask: []bool{false, true, false},
	}
}
