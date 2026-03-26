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

func TestBridgeRejectsInvalidPropsJSON(t *testing.T) {
	b := New()
	prog := program.CounterProgram()
	data, err := program.EncodeJSON(prog)
	if err != nil {
		t.Fatal(err)
	}

	err = b.HydrateIsland("island-0", "Counter", `{`, data, "json")
	if err == nil {
		t.Fatal("expected invalid props JSON error")
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

func TestSetSharedSignalJSON(t *testing.T) {
	b := New()

	if err := b.SetSharedSignalJSON("$presence", `{"count":2,"members":["a","b"]}`); err != nil {
		t.Fatalf("set shared signal json: %v", err)
	}

	val, ok := b.GetStore().Get("$presence")
	if !ok {
		t.Fatal("expected shared signal value")
	}
	if val.Type != program.TypeAny {
		t.Fatalf("expected any type, got %d", val.Type)
	}
	if got := val.Fields["count"].Num; got != 2 {
		t.Fatalf("expected count=2, got %v", got)
	}
	if got := val.Fields["members"].Items[1].Str; got != "b" {
		t.Fatalf("expected member b, got %q", got)
	}
}

func TestSetSharedSignalBatchJSON(t *testing.T) {
	b := New()

	err := b.SetSharedSignalBatchJSON(`{
		"$input.pointer": {"x": 12.5, "y": -3},
		"$input.key": {"space": true}
	}`)
	if err != nil {
		t.Fatalf("set shared signal batch json: %v", err)
	}

	pointer, ok := b.GetStore().Get("$input.pointer")
	if !ok {
		t.Fatal("expected pointer signal value")
	}
	if got := pointer.Fields["x"].Num; got != 12.5 {
		t.Fatalf("expected x=12.5, got %v", got)
	}
	if got := pointer.Fields["y"].Num; got != -3 {
		t.Fatalf("expected y=-3, got %v", got)
	}

	keyboard, ok := b.GetStore().Get("$input.key")
	if !ok {
		t.Fatal("expected key signal value")
	}
	if !keyboard.Fields["space"].Bool {
		t.Fatalf("expected space=true, got %#v", keyboard.Fields["space"])
	}
}

func TestSetSharedSignalBatchJSONRejectsInvalidPayloadWithoutApplying(t *testing.T) {
	b := New()

	if err := b.SetSharedSignalBatchJSON(`{"$input.pointer":{"x":1.5},"$input.key":`); err == nil {
		t.Fatal("expected invalid batch payload error")
	}
	if _, ok := b.GetStore().Get("$input.pointer"); ok {
		t.Fatal("expected no values applied on invalid batch payload")
	}
}
