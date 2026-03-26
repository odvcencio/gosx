package bridge

import (
	"testing"

	rootengine "github.com/odvcencio/gosx/engine"
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

func TestBridgeHydrateTickAndDisposeEngine(t *testing.T) {
	b := New()

	prog := &rootengine.Program{
		Name: "GeometryZoo",
		Nodes: []rootengine.Node{
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]program.ExprID{
					"x":     0,
					"color": 1,
				},
			},
		},
		Exprs: []program.Expr{
			{Op: program.OpSignalGet, Value: "$scene.x", Type: program.TypeFloat},
			{Op: program.OpSignalGet, Value: "$scene.color", Type: program.TypeString},
			{Op: program.OpLitFloat, Value: "0", Type: program.TypeFloat},
			{Op: program.OpLitString, Value: "#8de1ff", Type: program.TypeString},
		},
		Signals: []program.SignalDef{
			{Name: "$scene.x", Type: program.TypeFloat, Init: 2},
			{Name: "$scene.color", Type: program.TypeString, Init: 3},
		},
	}

	data, err := rootengine.EncodeProgramJSON(prog)
	if err != nil {
		t.Fatalf("encode engine program: %v", err)
	}

	initial, err := b.HydrateEngine("engine-0", prog.Name, `{}`, data, "json")
	if err != nil {
		t.Fatalf("hydrate engine: %v", err)
	}
	if len(initial) != 1 || initial[0].Kind != rootengine.CommandCreateObject {
		t.Fatalf("expected initial create command, got %#v", initial)
	}
	if b.EngineCount() != 1 {
		t.Fatalf("expected 1 engine, got %d", b.EngineCount())
	}

	if err := b.SetSharedSignalBatchJSON(`{"$scene.x":2.5,"$scene.color":"#ff8f6b"}`); err != nil {
		t.Fatalf("set shared signal batch: %v", err)
	}

	commands, err := b.TickEngine("engine-0")
	if err != nil {
		t.Fatalf("tick engine: %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("expected transform + material commands, got %#v", commands)
	}
	if commands[0].Kind != rootengine.CommandSetTransform {
		t.Fatalf("expected transform command, got %v", commands[0].Kind)
	}
	if commands[1].Kind != rootengine.CommandSetMaterial {
		t.Fatalf("expected material command, got %v", commands[1].Kind)
	}

	b.DisposeEngine("engine-0")
	if b.EngineCount() != 0 {
		t.Fatalf("expected 0 engines after dispose, got %d", b.EngineCount())
	}
}

func TestBridgeRenderEngineBundle(t *testing.T) {
	b := New()

	prog := &rootengine.Program{
		Name: "RenderBundle",
		Nodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]program.ExprID{
					"z":   0,
					"fov": 1,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "sphere",
				Material: "flat",
				Props: map[string]program.ExprID{
					"x":      2,
					"radius": 3,
					"color":  4,
				},
			},
		},
		Exprs: []program.Expr{
			{Op: program.OpLitFloat, Value: "6", Type: program.TypeFloat},
			{Op: program.OpLitFloat, Value: "75", Type: program.TypeFloat},
			{Op: program.OpSignalGet, Value: "$scene.x", Type: program.TypeFloat},
			{Op: program.OpLitFloat, Value: "0.9", Type: program.TypeFloat},
			{Op: program.OpLitString, Value: "#8de1ff", Type: program.TypeString},
			{Op: program.OpLitFloat, Value: "0", Type: program.TypeFloat},
		},
		Signals: []program.SignalDef{
			{Name: "$scene.x", Type: program.TypeFloat, Init: 5},
		},
	}

	data, err := rootengine.EncodeProgramJSON(prog)
	if err != nil {
		t.Fatalf("encode engine program: %v", err)
	}
	if _, err := b.HydrateEngine("engine-render", prog.Name, `{"background":"#08151f"}`, data, "json"); err != nil {
		t.Fatalf("hydrate engine: %v", err)
	}
	if err := b.SetSharedSignalBatchJSON(`{"$scene.x":1.75}`); err != nil {
		t.Fatalf("set shared signal batch: %v", err)
	}

	bundle, err := b.RenderEngine("engine-render", 640, 360, 1.25)
	if err != nil {
		t.Fatalf("render engine: %v", err)
	}
	if bundle.ObjectCount != 1 {
		t.Fatalf("expected objectCount=1, got %d", bundle.ObjectCount)
	}
	if bundle.VertexCount == 0 {
		t.Fatal("expected non-empty render bundle")
	}
	if bundle.Background != "#08151f" {
		t.Fatalf("expected background passthrough, got %q", bundle.Background)
	}
}
