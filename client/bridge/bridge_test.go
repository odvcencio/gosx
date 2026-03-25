package bridge

import (
	"testing"

	"github.com/odvcencio/gosx/island/program"
)

func TestBridgeHydrateAndDispatch(t *testing.T) {
	b := New()

	// Encode the counter program as JSON
	prog := program.CounterProgram()
	data, err := program.EncodeJSON(prog)
	if err != nil {
		t.Fatal(err)
	}

	// Hydrate
	err = b.HydrateIsland("island-0", "Counter", `{}`, data, "json")
	if err != nil {
		t.Fatal(err)
	}

	if b.IslandCount() != 1 {
		t.Fatalf("expected 1 island, got %d", b.IslandCount())
	}

	// Dispatch increment
	patches, err := b.DispatchAction("island-0", "increment", "{}")
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) == 0 {
		t.Fatal("expected patches")
	}

	// Verify patches can be marshaled
	json, err := MarshalPatches(patches)
	if err != nil {
		t.Fatal(err)
	}
	if len(json) == 0 {
		t.Fatal("empty JSON")
	}
}

func TestBridgeHydrateBinary(t *testing.T) {
	b := New()
	prog := program.CounterProgram()
	data, err := program.EncodeBinary(prog)
	if err != nil {
		t.Fatal(err)
	}

	err = b.HydrateIsland("island-0", "Counter", `{}`, data, "bin")
	if err != nil {
		t.Fatal(err)
	}
	if b.IslandCount() != 1 {
		t.Fatal("expected 1 island")
	}
}

func TestBridgeDispose(t *testing.T) {
	b := New()
	prog := program.CounterProgram()
	data, _ := program.EncodeJSON(prog)
	b.HydrateIsland("island-0", "Counter", `{}`, data, "json")

	b.DisposeIsland("island-0")
	if b.IslandCount() != 0 {
		t.Fatal("expected 0 islands after dispose")
	}
}

func TestBridgeUnknownIsland(t *testing.T) {
	b := New()
	_, err := b.DispatchAction("nonexistent", "handler", "{}")
	if err == nil {
		t.Fatal("expected error for unknown island")
	}
}

func TestBridgeUnknownFormat(t *testing.T) {
	b := New()
	err := b.HydrateIsland("island-0", "Counter", `{}`, []byte("data"), "xml")
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestDecodeProgram(t *testing.T) {
	prog := program.CounterProgram()

	// JSON
	jsonData, _ := program.EncodeJSON(prog)
	decoded, err := DecodeProgram(jsonData, "json")
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Name != "Counter" {
		t.Fatal("wrong name")
	}

	// Binary
	binData, _ := program.EncodeBinary(prog)
	decoded, err = DecodeProgram(binData, "bin")
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Name != "Counter" {
		t.Fatal("wrong name")
	}
}
