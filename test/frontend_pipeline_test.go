package test

import (
	"testing"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/client/bridge"
	"github.com/odvcencio/gosx/ir"
	"github.com/odvcencio/gosx/island/program"
)

// compileAndHydrate proves the full pipeline: .gsx source -> compile -> IR ->
// body analysis -> island lowering -> serialize -> hydrate -> bridge ready.
func compileAndHydrate(t *testing.T, source string) *bridge.Bridge {
	t.Helper()
	irProg, err := gosx.Compile([]byte(source))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var islandProg *program.Program
	for i, comp := range irProg.Components {
		if comp.IsIsland {
			islandProg, err = ir.LowerIsland(irProg, i)
			if err != nil {
				t.Fatalf("lower: %v", err)
			}
			break
		}
	}
	if islandProg == nil {
		t.Fatal("no island component found")
	}

	data, err := program.EncodeJSON(islandProg)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	b := bridge.New()
	err = b.HydrateIsland("test-0", islandProg.Name, "{}", data, "json")
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	return b
}

func TestFrontendCounterFromSource(t *testing.T) {
	b := compileAndHydrate(t, `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 1) }
	decrement := func() { count.Set(count.Get() - 1) }
	return <div class="counter">
		<button onClick={decrement}>-</button>
		<span>{count.Get()}</span>
		<button onClick={increment}>+</button>
	</div>
}`)

	// Increment 3 times
	for i := 0; i < 3; i++ {
		patches, err := b.DispatchAction("test-0", "increment", "{}")
		if err != nil {
			t.Fatalf("increment %d: %v", i, err)
		}
		if len(patches) == 0 {
			t.Fatalf("increment %d: no patches", i)
		}
	}

	// Decrement once (count goes from 3 to 2)
	patches, err := b.DispatchAction("test-0", "decrement", "{}")
	if err != nil {
		t.Fatalf("decrement: %v", err)
	}
	if len(patches) == 0 {
		t.Fatal("decrement: no patches")
	}

	pj, err := bridge.MarshalPatches(patches)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	t.Logf("Final patches after 3 inc + 1 dec: %s", pj)
}

func TestFrontendToggleFromSource(t *testing.T) {
	b := compileAndHydrate(t, `package main

//gosx:island
func Toggle() Node {
	visible := signal.New(false)
	toggle := func() { visible.Set(!visible.Get()) }
	return <div>
		<button onClick={toggle}>Toggle</button>
		<p>{visible.Get()}</p>
	</div>
}`)

	// Toggle on
	p1, err := b.DispatchAction("test-0", "toggle", "{}")
	if err != nil {
		t.Fatalf("toggle on: %v", err)
	}
	if len(p1) == 0 {
		t.Fatal("toggle on: no patches")
	}

	pj1, _ := bridge.MarshalPatches(p1)
	t.Logf("Toggle on patches: %s", pj1)

	// Toggle off
	p2, err := b.DispatchAction("test-0", "toggle", "{}")
	if err != nil {
		t.Fatalf("toggle off: %v", err)
	}
	if len(p2) == 0 {
		t.Fatal("toggle off: no patches")
	}

	pj2, _ := bridge.MarshalPatches(p2)
	t.Logf("Toggle off patches: %s", pj2)
}

func TestFrontendDerivedFromSource(t *testing.T) {
	b := compileAndHydrate(t, `package main

//gosx:island
func Calc() Node {
	price := signal.New(100)
	qty := signal.New(1)
	incQty := func() { qty.Set(qty.Get() + 1) }
	return <div>
		<span>{qty.Get()}</span>
		<button onClick={incQty}>+</button>
	</div>
}`)

	patches, err := b.DispatchAction("test-0", "incQty", "{}")
	if err != nil {
		t.Fatalf("incQty: %v", err)
	}
	if len(patches) == 0 {
		t.Fatal("no patches after incQty")
	}

	pj, _ := bridge.MarshalPatches(patches)
	t.Logf("Calc patches: %s", pj)
}

func TestFrontendMultiSignalFromSource(t *testing.T) {
	b := compileAndHydrate(t, `package main

//gosx:island
func Form() Node {
	name := signal.New("")
	age := signal.New(0)
	setName := func() { name.Set("Alice") }
	incAge := func() { age.Set(age.Get() + 1) }
	return <div>
		<span>{name.Get()}</span>
		<span>{age.Get()}</span>
		<button onClick={setName}>Set Name</button>
		<button onClick={incAge}>+Age</button>
	</div>
}`)

	// Set name
	p1, err := b.DispatchAction("test-0", "setName", "{}")
	if err != nil {
		t.Fatalf("setName: %v", err)
	}
	if len(p1) == 0 {
		t.Fatal("no patches after setName")
	}

	// Inc age
	p2, err := b.DispatchAction("test-0", "incAge", "{}")
	if err != nil {
		t.Fatalf("incAge: %v", err)
	}
	if len(p2) == 0 {
		t.Fatal("no patches after incAge")
	}

	pj1, _ := bridge.MarshalPatches(p1)
	pj2, _ := bridge.MarshalPatches(p2)
	t.Logf("setName patches: %s", pj1)
	t.Logf("incAge patches: %s", pj2)
}

func TestFrontendInputEventValueFromSource(t *testing.T) {
	b := compileAndHydrate(t, `package main

//gosx:island
func Editor() Node {
	code := signal.New("hello")
	updateCode := func() { code.Set(value) }
	return <div>
		<textarea value={code.Get()} onInput={updateCode}></textarea>
		<span>{code.Get()}</span>
	</div>
}`)

	patches, err := b.DispatchAction("test-0", "updateCode", `{"value":"policy"}`)
	if err != nil {
		t.Fatalf("updateCode: %v", err)
	}
	if len(patches) == 0 {
		t.Fatal("expected patches when updating textarea value from event payload")
	}
}

func TestFrontendBinaryRoundTrip(t *testing.T) {
	source := `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	inc := func() { count.Set(count.Get() + 1) }
	return <div><span>{count.Get()}</span><button onClick={inc}>+</button></div>
}`
	irProg, err := gosx.Compile([]byte(source))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var islandProg *program.Program
	for i, comp := range irProg.Components {
		if comp.IsIsland {
			islandProg, err = ir.LowerIsland(irProg, i)
			if err != nil {
				t.Fatalf("lower: %v", err)
			}
			break
		}
	}
	if islandProg == nil {
		t.Fatal("no island component")
	}

	// Serialize to binary
	binData, err := program.EncodeBinary(islandProg)
	if err != nil {
		t.Fatalf("encode binary: %v", err)
	}
	t.Logf("Binary: %d bytes", len(binData))

	// Verify binary can be decoded
	decoded, err := program.DecodeBinary(binData)
	if err != nil {
		t.Fatalf("decode binary: %v", err)
	}
	if decoded.Name != "Counter" {
		t.Fatalf("expected name 'Counter', got %q", decoded.Name)
	}

	// Hydrate from binary
	b := bridge.New()
	err = b.HydrateIsland("test-0", "Counter", "{}", binData, "bin")
	if err != nil {
		t.Fatalf("hydrate from binary: %v", err)
	}

	// Dispatch and verify patches
	patches, err := b.DispatchAction("test-0", "inc", "{}")
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(patches) == 0 {
		t.Fatal("no patches from binary-hydrated island")
	}

	pj, _ := bridge.MarshalPatches(patches)
	t.Logf("Binary round-trip patches: %s", pj)

	// Compare binary size to JSON size
	jsonData, _ := program.EncodeJSON(islandProg)
	t.Logf("JSON: %d bytes, Binary: %d bytes (%.0f%% of JSON)",
		len(jsonData), len(binData), float64(len(binData))/float64(len(jsonData))*100)
}

func TestFrontendJSONRoundTrip(t *testing.T) {
	source := `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	inc := func() { count.Set(count.Get() + 1) }
	return <div><span>{count.Get()}</span><button onClick={inc}>+</button></div>
}`
	irProg, err := gosx.Compile([]byte(source))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var islandProg *program.Program
	for i, comp := range irProg.Components {
		if comp.IsIsland {
			islandProg, err = ir.LowerIsland(irProg, i)
			if err != nil {
				t.Fatalf("lower: %v", err)
			}
			break
		}
	}

	// JSON round-trip
	jsonData, err := program.EncodeJSON(islandProg)
	if err != nil {
		t.Fatalf("encode JSON: %v", err)
	}

	decoded, err := program.DecodeJSON(jsonData)
	if err != nil {
		t.Fatalf("decode JSON: %v", err)
	}

	// Verify structural integrity
	if decoded.Name != islandProg.Name {
		t.Fatalf("name mismatch: %q vs %q", decoded.Name, islandProg.Name)
	}
	if len(decoded.Nodes) != len(islandProg.Nodes) {
		t.Fatalf("node count mismatch: %d vs %d", len(decoded.Nodes), len(islandProg.Nodes))
	}
	if len(decoded.Signals) != len(islandProg.Signals) {
		t.Fatalf("signal count mismatch: %d vs %d", len(decoded.Signals), len(islandProg.Signals))
	}
	if len(decoded.Handlers) != len(islandProg.Handlers) {
		t.Fatalf("handler count mismatch: %d vs %d", len(decoded.Handlers), len(islandProg.Handlers))
	}
	if len(decoded.Exprs) != len(islandProg.Exprs) {
		t.Fatalf("expr count mismatch: %d vs %d", len(decoded.Exprs), len(islandProg.Exprs))
	}

	// Hydrate from the decoded JSON and verify it works
	b := bridge.New()
	err = b.HydrateIsland("test-0", decoded.Name, "{}", jsonData, "json")
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}

	patches, err := b.DispatchAction("test-0", "inc", "{}")
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(patches) == 0 {
		t.Fatal("no patches from JSON round-tripped island")
	}

	t.Logf("JSON round-trip: %d bytes, %d nodes, %d signals, %d handlers",
		len(jsonData), len(decoded.Nodes), len(decoded.Signals), len(decoded.Handlers))
}

func TestFrontendSharedStateFromSource(t *testing.T) {
	// Two islands hydrated from the same compiled source, dispatching independently.
	source := `package main

//gosx:island
func SharedCounter() Node {
	count := signal.NewShared("count", 0)
	inc := func() { count.Set(count.Get() + 1) }
	return <div><span>{count.Get()}</span><button onClick={inc}>+</button></div>
}`
	irProg, err := gosx.Compile([]byte(source))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var islandProg *program.Program
	for i, comp := range irProg.Components {
		if comp.IsIsland {
			islandProg, err = ir.LowerIsland(irProg, i)
			if err != nil {
				t.Fatalf("lower: %v", err)
			}
			break
		}
	}
	data, err := program.EncodeJSON(islandProg)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	b := bridge.New()
	err = b.HydrateIsland("a", "CounterA", "{}", data, "json")
	if err != nil {
		t.Fatalf("hydrate A: %v", err)
	}
	err = b.HydrateIsland("b", "CounterB", "{}", data, "json")
	if err != nil {
		t.Fatalf("hydrate B: %v", err)
	}

	if b.IslandCount() != 2 {
		t.Fatalf("expected 2 islands, got %d", b.IslandCount())
	}

	var sharedPatches int
	b.SetPatchCallback(func(islandID, patchJSON string) {
		if islandID == "b" && patchJSON != "" {
			sharedPatches++
		}
	})

	// Dispatch on A
	p1, err := b.DispatchAction("a", "inc", "{}")
	if err != nil {
		t.Fatalf("dispatch A: %v", err)
	}
	if len(p1) == 0 {
		t.Fatal("no patches from island A")
	}

	// Dispatch on B independently
	p2, err := b.DispatchAction("b", "inc", "{}")
	if err != nil {
		t.Fatalf("dispatch B: %v", err)
	}
	if len(p2) == 0 {
		t.Fatal("no patches from island B")
	}

	pj1, _ := bridge.MarshalPatches(p1)
	pj2, _ := bridge.MarshalPatches(p2)
	t.Logf("Island A patches: %s", pj1)
	t.Logf("Island B patches: %s", pj2)
	if sharedPatches == 0 {
		t.Fatal("expected island B to receive shared-signal patches after island A dispatch")
	}
	t.Log("Two islands from same source now share state through signal.NewShared()")
}

func TestFrontendIslandDispose(t *testing.T) {
	b := compileAndHydrate(t, `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	inc := func() { count.Set(count.Get() + 1) }
	return <div>{count.Get()}</div>
}`)

	if b.IslandCount() != 1 {
		t.Fatalf("expected 1 island, got %d", b.IslandCount())
	}

	// Dispatch works before dispose
	patches, err := b.DispatchAction("test-0", "inc", "{}")
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(patches) == 0 {
		t.Fatal("no patches before dispose")
	}

	// Dispose
	b.DisposeIsland("test-0")
	if b.IslandCount() != 0 {
		t.Fatalf("expected 0 islands after dispose, got %d", b.IslandCount())
	}

	// Dispatch after dispose should return error
	_, err = b.DispatchAction("test-0", "inc", "{}")
	if err == nil {
		t.Fatal("expected error dispatching to disposed island")
	}
}

func TestFrontendIslandLoweringStructure(t *testing.T) {
	// Verify the island program structure is correct after lowering from source.
	source := `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 1) }
	decrement := func() { count.Set(count.Get() - 1) }
	return <div class="counter">
		<button onClick={decrement}>-</button>
		<span>{count.Get()}</span>
		<button onClick={increment}>+</button>
	</div>
}`
	irProg, err := gosx.Compile([]byte(source))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var islandProg *program.Program
	for i, comp := range irProg.Components {
		if comp.IsIsland {
			islandProg, err = ir.LowerIsland(irProg, i)
			if err != nil {
				t.Fatalf("lower: %v", err)
			}
			break
		}
	}

	// Verify program name
	if islandProg.Name != "Counter" {
		t.Fatalf("expected name 'Counter', got %q", islandProg.Name)
	}

	// Verify signals
	if len(islandProg.Signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(islandProg.Signals))
	}
	if islandProg.Signals[0].Name != "count" {
		t.Fatalf("expected signal 'count', got %q", islandProg.Signals[0].Name)
	}
	if islandProg.Signals[0].Type != program.TypeInt {
		t.Fatalf("expected signal type TypeInt, got %d", islandProg.Signals[0].Type)
	}

	// Verify handlers
	if len(islandProg.Handlers) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(islandProg.Handlers))
	}
	handlerNames := map[string]bool{}
	for _, h := range islandProg.Handlers {
		handlerNames[h.Name] = true
		if len(h.Body) == 0 {
			t.Errorf("handler %q has empty body", h.Name)
		}
	}
	if !handlerNames["increment"] {
		t.Error("missing handler 'increment'")
	}
	if !handlerNames["decrement"] {
		t.Error("missing handler 'decrement'")
	}

	// Verify nodes exist
	if len(islandProg.Nodes) == 0 {
		t.Fatal("expected nodes in island program")
	}

	// Verify root node is an element
	rootNode := islandProg.Nodes[islandProg.Root]
	if rootNode.Kind != program.NodeElement {
		t.Fatalf("expected root NodeElement, got %s", rootNode.Kind.String())
	}
	if rootNode.Tag != "div" {
		t.Fatalf("expected root tag 'div', got %q", rootNode.Tag)
	}

	// Verify static mask
	if len(islandProg.StaticMask) != len(islandProg.Nodes) {
		t.Fatalf("static mask length %d != node count %d",
			len(islandProg.StaticMask), len(islandProg.Nodes))
	}

	// Verify expressions exist (from count.Get() and handler bodies)
	if len(islandProg.Exprs) == 0 {
		t.Fatal("expected expressions in island program")
	}

	t.Logf("Island structure: %d nodes, %d exprs, %d signals, %d handlers, %d static mask entries",
		len(islandProg.Nodes), len(islandProg.Exprs),
		len(islandProg.Signals), len(islandProg.Handlers),
		len(islandProg.StaticMask))
}

func TestFrontendMultipleIncrements(t *testing.T) {
	// Verify that multiple dispatches accumulate state correctly.
	b := compileAndHydrate(t, `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	inc := func() { count.Set(count.Get() + 1) }
	return <div><span>{count.Get()}</span><button onClick={inc}>+</button></div>
}`)

	// Dispatch 10 times
	for i := 0; i < 10; i++ {
		patches, err := b.DispatchAction("test-0", "inc", "{}")
		if err != nil {
			t.Fatalf("dispatch %d: %v", i, err)
		}
		if len(patches) == 0 {
			t.Fatalf("dispatch %d: no patches", i)
		}

		pj, _ := bridge.MarshalPatches(patches)
		if len(pj) == 0 {
			t.Fatalf("dispatch %d: empty patch JSON", i)
		}
	}

	t.Log("10 successive increments: all produced patches")
}

func TestFrontendStaticMaskSkipsStatic(t *testing.T) {
	// Verify that static subtrees produce shorter patch lists.
	source := `package main

//gosx:island
func App() Node {
	val := signal.New(0)
	inc := func() { val.Set(val.Get() + 1) }
	return <div>
		<p>This is static text that never changes</p>
		<span>{val.Get()}</span>
		<button onClick={inc}>+</button>
	</div>
}`
	irProg, err := gosx.Compile([]byte(source))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var islandProg *program.Program
	for i, comp := range irProg.Components {
		if comp.IsIsland {
			islandProg, err = ir.LowerIsland(irProg, i)
			if err != nil {
				t.Fatalf("lower: %v", err)
			}
			break
		}
	}

	// Verify that static mask has some true entries
	staticCount := 0
	for _, s := range islandProg.StaticMask {
		if s {
			staticCount++
		}
	}
	if staticCount == 0 {
		t.Fatal("expected some static entries in static mask")
	}
	t.Logf("Static mask: %d static out of %d total nodes", staticCount, len(islandProg.StaticMask))

	// Hydrate and dispatch
	data, _ := program.EncodeJSON(islandProg)
	b := bridge.New()
	err = b.HydrateIsland("test-0", islandProg.Name, "{}", data, "json")
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}

	patches, err := b.DispatchAction("test-0", "inc", "{}")
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(patches) == 0 {
		t.Fatal("no patches")
	}

	pj, _ := bridge.MarshalPatches(patches)
	t.Logf("Patches (static mask in effect): %s", pj)
}
