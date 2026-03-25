package test

import (
	"testing"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/client/bridge"
	"github.com/odvcencio/gosx/ir"
	"github.com/odvcencio/gosx/island/program"
)

// TestCompilerE2E_CounterFromSource is THE proof: a .gsx component with
// signals and handlers, compiled from source all the way to DOM patches.
// No fixtures. No hand-built programs. Just .gsx source → working island.
func TestCompilerE2E_CounterFromSource(t *testing.T) {
	source := []byte(`package main

//gosx:island
func Counter() Node {
	count := signal.New(0)

	increment := func() {
		count.Set(count.Get() + 1)
	}

	decrement := func() {
		count.Set(count.Get() - 1)
	}

	return <div class="counter">
		<button onClick={decrement}>-</button>
		<span class="count">{count.Get()}</span>
		<button onClick={increment}>+</button>
	</div>
}
`)
	// 1. Compile to IR
	irProg, err := gosx.Compile(source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	comp := irProg.Components[0]
	t.Logf("Component: %s, IsIsland: %v", comp.Name, comp.IsIsland)
	if !comp.IsIsland {
		t.Fatal("expected IsIsland=true")
	}
	if comp.Scope == nil {
		t.Fatal("expected component scope from body analysis")
	}

	t.Logf("Scope: %d signals, %d handlers, locals=%v",
		len(comp.Scope.Signals), len(comp.Scope.Handlers), comp.Scope.Locals)

	// 2. Lower to IslandProgram
	islandProg, err := ir.LowerIsland(irProg, 0)
	if err != nil {
		t.Fatalf("lower island: %v", err)
	}

	t.Logf("IslandProgram: %d nodes, %d exprs, %d signals, %d handlers",
		len(islandProg.Nodes), len(islandProg.Exprs),
		len(islandProg.Signals), len(islandProg.Handlers))

	// Must have signals and handlers from body analysis
	if len(islandProg.Signals) == 0 {
		t.Fatal("expected signals from body analysis")
	}
	if len(islandProg.Handlers) == 0 {
		t.Fatal("expected handlers from body analysis")
	}

	// 3. Serialize and hydrate
	jsonData, err := program.EncodeJSON(islandProg)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	t.Logf("JSON: %d bytes", len(jsonData))

	b := bridge.New()
	err = b.HydrateIsland("counter-0", "Counter", `{}`, jsonData, "json")
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}

	// 4. Dispatch increment and verify patches
	patches, err := b.DispatchAction("counter-0", "increment", "{}")
	if err != nil {
		t.Fatalf("dispatch increment: %v", err)
	}

	if len(patches) == 0 {
		t.Fatal("expected patches after increment — the counter should have changed from 0 to 1")
	}

	patchJSON, _ := bridge.MarshalPatches(patches)
	t.Logf("Patches after increment: %s", patchJSON)

	// 5. Dispatch decrement
	patches2, err := b.DispatchAction("counter-0", "decrement", "{}")
	if err != nil {
		t.Fatalf("dispatch decrement: %v", err)
	}

	if len(patches2) == 0 {
		t.Fatal("expected patches after decrement")
	}

	patchJSON2, _ := bridge.MarshalPatches(patches2)
	t.Logf("Patches after decrement: %s", patchJSON2)

	t.Log("SUCCESS: .gsx source → compile → IR → island → serialize → hydrate → dispatch → patches")
}
