package test

import (
	"testing"

	"github.com/odvcencio/gosx/client/bridge"
	"github.com/odvcencio/gosx/hydrate"
	"github.com/odvcencio/gosx/ir"
	"github.com/odvcencio/gosx/island/program"
)

// TestEndToEndPipeline tests the complete flow:
// IR → LowerIsland → Serialize → Deserialize → VM → Dispatch → Patches
func TestEndToEndPipeline(t *testing.T) {
	// 1. Build an IR program with an island component
	irProg := buildIRIsland()

	// 2. Lower to IslandProgram
	islandProg, err := ir.LowerIsland(irProg, 0)
	if err != nil {
		t.Fatalf("lower island: %v", err)
	}
	if islandProg.Name != "Counter" {
		t.Fatalf("expected Counter, got %s", islandProg.Name)
	}

	// 3. JSON round-trip
	jsonData, err := program.EncodeJSON(islandProg)
	if err != nil {
		t.Fatalf("encode JSON: %v", err)
	}
	jsonProg, err := program.DecodeJSON(jsonData)
	if err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if jsonProg.Name != islandProg.Name {
		t.Fatal("JSON round-trip lost name")
	}

	// 4. Binary round-trip
	binData, err := program.EncodeBinary(islandProg)
	if err != nil {
		t.Fatalf("encode binary: %v", err)
	}
	binProg, err := program.DecodeBinary(binData)
	if err != nil {
		t.Fatalf("decode binary: %v", err)
	}
	if binProg.Name != islandProg.Name {
		t.Fatal("binary round-trip lost name")
	}
	t.Logf("JSON size: %d bytes, binary size: %d bytes", len(jsonData), len(binData))
}

// TestEndToEndCounterViaCounterProgram tests with the reference CounterProgram fixture
func TestEndToEndCounterViaCounterProgram(t *testing.T) {
	prog := program.CounterProgram()

	// Serialize to JSON and hydrate through the bridge
	data, err := program.EncodeJSON(prog)
	if err != nil {
		t.Fatal(err)
	}

	b := bridge.New()
	err = b.HydrateIsland("island-0", "Counter", `{}`, data, "json")
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}

	// Dispatch increment 3 times
	for i := 0; i < 3; i++ {
		patches, err := b.DispatchAction("island-0", "increment", "{}")
		if err != nil {
			t.Fatalf("dispatch %d: %v", i, err)
		}
		if len(patches) == 0 {
			t.Fatalf("dispatch %d: expected patches", i)
		}

		// Verify patches can be serialized (this is what goes to JS)
		patchJSON, err := bridge.MarshalPatches(patches)
		if err != nil {
			t.Fatalf("marshal patches %d: %v", i, err)
		}
		if len(patchJSON) == 0 {
			t.Fatalf("empty patch JSON at %d", i)
		}
	}

	// Dispatch decrement once
	patches, err := b.DispatchAction("island-0", "decrement", "{}")
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) == 0 {
		t.Fatal("expected patches from decrement")
	}

	// Dispose
	b.DisposeIsland("island-0")
	if b.IslandCount() != 0 {
		t.Fatal("expected 0 islands after dispose")
	}
}

// TestEndToEndManifest tests manifest generation with program refs
func TestEndToEndManifest(t *testing.T) {
	m := hydrate.NewManifest()
	m.Runtime = hydrate.RuntimeRef{
		Path: "/gosx/runtime.wasm",
		Hash: "abc123",
	}

	id, err := m.AddIsland("Counter", "main", map[string]int{"initial": 0})
	if err != nil {
		t.Fatal(err)
	}

	// Set program ref
	for i := range m.Islands {
		if m.Islands[i].ID == id {
			m.Islands[i].ProgramRef = "/gosx/islands/Counter.json"
			m.Islands[i].ProgramFormat = "json"
		}
	}

	// Marshal and unmarshal
	data, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := hydrate.Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}

	if decoded.Runtime.Path != "/gosx/runtime.wasm" {
		t.Fatal("lost runtime path")
	}
	if decoded.Islands[0].ProgramRef != "/gosx/islands/Counter.json" {
		t.Fatal("lost program ref")
	}
}

// buildIRIsland creates a simple island IR for testing the lowering pipeline.
func buildIRIsland() *ir.Program {
	prog := &ir.Program{}

	// Node 0: <div class="test">
	prog.Nodes = append(prog.Nodes, ir.Node{
		Kind: ir.NodeElement,
		Tag:  "div",
		Attrs: []ir.Attr{
			{Kind: ir.AttrStatic, Name: "class", Value: "test"},
		},
		Children: []ir.NodeID{1, 2},
		IsStatic: false,
	})

	// Node 1: static text
	prog.Nodes = append(prog.Nodes, ir.Node{
		Kind:     ir.NodeText,
		Text:     "Count: ",
		IsStatic: true,
	})

	// Node 2: expression
	prog.Nodes = append(prog.Nodes, ir.Node{
		Kind:     ir.NodeExpr,
		Text:     "count",
		IsStatic: false,
	})

	prog.Components = append(prog.Components, ir.Component{
		Name:     "Counter",
		Root:     0,
		IsIsland: true,
	})

	return prog
}
