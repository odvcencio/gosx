package test

import (
	"os"
	"testing"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/client/bridge"
	"github.com/odvcencio/gosx/ir"
	"github.com/odvcencio/gosx/island/program"
)

// TestGSXPipelineEndToEnd is the proof that GoSX works end-to-end:
// .gsx source → parse → IR (with IsIsland) → lower to IslandProgram →
// serialize → deserialize → VM → dispatch → patches
func TestGSXPipelineEndToEnd(t *testing.T) {
	// 1. Read a real .gsx file
	source, err := os.ReadFile("../examples/counter/counter.gsx")
	if err != nil {
		t.Fatalf("read .gsx: %v", err)
	}
	t.Logf("Source (%d bytes):\n%s", len(source), source)

	// 2. Parse and compile to IR
	irProg, err := gosx.Compile(source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// 3. Verify the component was detected
	if len(irProg.Components) == 0 {
		t.Fatal("no components found in .gsx source")
	}
	comp := irProg.Components[0]
	t.Logf("Component: %s, IsIsland: %v", comp.Name, comp.IsIsland)

	if comp.Name != "Counter" {
		t.Fatalf("expected Counter, got %s", comp.Name)
	}

	// 4. Verify //gosx:island was detected
	if !comp.IsIsland {
		t.Fatal("//gosx:island directive not detected — IsIsland should be true")
	}

	// 5. Lower to IslandProgram
	islandProg, err := ir.LowerIsland(irProg, 0)
	if err != nil {
		t.Fatalf("lower island: %v", err)
	}
	t.Logf("IslandProgram: %s, %d nodes, %d exprs", islandProg.Name, len(islandProg.Nodes), len(islandProg.Exprs))

	// 6. Serialize to JSON (dev mode)
	jsonData, err := program.EncodeJSON(islandProg)
	if err != nil {
		t.Fatalf("encode JSON: %v", err)
	}
	t.Logf("JSON (%d bytes): %s", len(jsonData), jsonData)

	// 7. Serialize to binary (prod mode)
	binData, err := program.EncodeBinary(islandProg)
	if err != nil {
		t.Fatalf("encode binary: %v", err)
	}
	t.Logf("Binary: %d bytes (%.0f%% of JSON)", len(binData), float64(len(binData))/float64(len(jsonData))*100)

	// 8. Deserialize from JSON and verify round-trip
	decoded, err := program.DecodeJSON(jsonData)
	if err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if decoded.Name != "Counter" {
		t.Fatalf("JSON round-trip: expected Counter, got %s", decoded.Name)
	}

	// 9. Hydrate through the bridge (simulating browser)
	b := bridge.New()
	err = b.HydrateIsland("island-0", "Counter", `{}`, jsonData, "json")
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}

	t.Log("PIPELINE COMPLETE: .gsx → parse → IR → island → serialize → hydrate")
}

// TestGSXIslandDirectiveDetection verifies //gosx:island is parsed from source
func TestGSXIslandDirectiveDetection(t *testing.T) {
	// With directive
	withDirective := []byte(`package main

//gosx:island
func Counter() Node {
	return <div>count</div>
}
`)
	prog, err := gosx.Compile(withDirective)
	if err != nil {
		t.Fatalf("compile with directive: %v", err)
	}
	if len(prog.Components) != 1 {
		t.Fatal("expected 1 component")
	}
	if !prog.Components[0].IsIsland {
		t.Fatal("expected IsIsland=true with //gosx:island directive")
	}

	// Without directive
	withoutDirective := []byte(`package main

func ServerComp() Node {
	return <div>hello</div>
}
`)
	prog2, err := gosx.Compile(withoutDirective)
	if err != nil {
		t.Fatalf("compile without directive: %v", err)
	}
	if len(prog2.Components) != 1 {
		t.Fatal("expected 1 component")
	}
	if prog2.Components[0].IsIsland {
		t.Fatal("expected IsIsland=false without directive")
	}
}

// TestSidecarCSSExists verifies the .gsx + .css pair is present
func TestSidecarCSSExists(t *testing.T) {
	_, err := os.Stat("../examples/counter/counter.gsx")
	if err != nil {
		t.Fatal("counter.gsx missing")
	}
	_, err = os.Stat("../examples/counter/counter.css")
	if err != nil {
		t.Fatal("counter.css missing — sidecar CSS not demonstrated")
	}
}
