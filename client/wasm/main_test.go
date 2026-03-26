//go:build js && wasm

package main

import (
	"encoding/json"
	"strings"
	"syscall/js"
	"testing"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/client/bridge"
	"github.com/odvcencio/gosx/client/vm"
	rootengine "github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/ir"
	"github.com/odvcencio/gosx/island/program"
)

func compileIslandProgram(t *testing.T, source string) *program.Program {
	t.Helper()

	irProg, err := gosx.Compile([]byte(source))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	for i, comp := range irProg.Components {
		if !comp.IsIsland {
			continue
		}
		prog, err := ir.LowerIsland(irProg, i)
		if err != nil {
			t.Fatalf("lower island: %v", err)
		}
		return prog
	}

	t.Fatal("no island component found")
	return nil
}

func setGlobalFunc(t *testing.T, name string, fn func(this js.Value, args []js.Value) any) {
	t.Helper()

	prev := js.Global().Get(name)
	wrapped := js.FuncOf(fn)
	js.Global().Set(name, wrapped)
	t.Cleanup(func() {
		wrapped.Release()
		js.Global().Set(name, prev)
	})
}

func setGlobalValue(t *testing.T, name string, value any) {
	t.Helper()

	prev := js.Global().Get(name)
	js.Global().Set(name, value)
	t.Cleanup(func() {
		js.Global().Set(name, prev)
	})
}

func uint8ArrayFromBytes(b []byte) js.Value {
	arr := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(arr, b)
	return arr
}

func TestRuntimeHydrateAndDispatchCompiledGSX(t *testing.T) {
	prog := compileIslandProgram(t, `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 1) }
	return <div class="counter">
		<span>{count.Get()}</span>
		<button onClick={increment}>+</button>
	</div>
}`)

	data, err := program.EncodeJSON(prog)
	if err != nil {
		t.Fatalf("encode json: %v", err)
	}

	var patchIslandID string
	var patches []vm.PatchOp
	setGlobalFunc(t, "__gosx_apply_patches", func(this js.Value, args []js.Value) any {
		patchIslandID = args[0].String()
		if err := json.Unmarshal([]byte(args[1].String()), &patches); err != nil {
			t.Fatalf("unmarshal patches: %v", err)
		}
		return nil
	})
	setGlobalValue(t, "__gosx_runtime_ready", js.Undefined())

	registerRuntime(bridge.New())

	hydrateRet := js.Global().Get("__gosx_hydrate").Invoke("counter-0", prog.Name, `{}`, string(data), "json")
	if !hydrateRet.IsNull() {
		t.Fatalf("expected null hydrate result, got %q", hydrateRet.String())
	}

	actionRet := js.Global().Get("__gosx_action").Invoke("counter-0", "increment", `{}`)
	if got := actionRet.Int(); got <= 0 {
		t.Fatalf("expected positive patch count, got %d", got)
	}

	if patchIslandID != "counter-0" {
		t.Fatalf("expected patches for counter-0, got %q", patchIslandID)
	}
	if len(patches) == 0 {
		t.Fatal("expected patches from wasm dispatch")
	}

	foundTextPatch := false
	for _, patch := range patches {
		if patch.Kind == vm.PatchSetText && patch.Text == "1" {
			foundTextPatch = true
		}
	}
	if !foundTextPatch {
		t.Fatalf("expected text patch setting counter to 1, got %#v", patches)
	}
}

func TestRuntimeBinaryHydrateDisposeAndErrors(t *testing.T) {
	prog := compileIslandProgram(t, `package main

//gosx:island
func Toggle() Node {
	visible := signal.New(false)
	toggle := func() { visible.Set(!visible.Get()) }
	return <div>
		<p>{visible.Get()}</p>
		<button onClick={toggle}>Toggle</button>
	</div>
}`)

	data, err := program.EncodeBinary(prog)
	if err != nil {
		t.Fatalf("encode binary: %v", err)
	}

	setGlobalFunc(t, "__gosx_apply_patches", func(this js.Value, args []js.Value) any { return nil })
	setGlobalValue(t, "__gosx_runtime_ready", js.Undefined())

	registerRuntime(bridge.New())

	hydrateRet := js.Global().Get("__gosx_hydrate").Invoke("toggle-0", prog.Name, `{}`, uint8ArrayFromBytes(data), "bin")
	if !hydrateRet.IsNull() {
		t.Fatalf("expected null hydrate result, got %q", hydrateRet.String())
	}

	js.Global().Get("__gosx_dispose").Invoke("toggle-0")

	actionRet := js.Global().Get("__gosx_action").Invoke("toggle-0", "toggle", `{}`)
	if !strings.Contains(actionRet.String(), `error: island "toggle-0" not found`) {
		t.Fatalf("expected disposed island error, got %q", actionRet.String())
	}
}

func TestRuntimeSetSharedSignalExport(t *testing.T) {
	setGlobalValue(t, "__gosx_runtime_ready", js.Undefined())

	b := bridge.New()
	registerRuntime(b)

	ret := js.Global().Get("__gosx_set_shared_signal").Invoke("$presence", `{"count":3}`)
	if !ret.IsNull() {
		t.Fatalf("expected null result, got %q", ret.String())
	}

	val, ok := b.GetStore().Get("$presence")
	if !ok {
		t.Fatal("expected shared signal to be set")
	}
	if val.Fields["count"].Num != 3 {
		t.Fatalf("expected count 3, got %v", val.Fields["count"].Num)
	}
}

func TestRuntimeSetInputBatchExport(t *testing.T) {
	setGlobalValue(t, "__gosx_runtime_ready", js.Undefined())

	b := bridge.New()
	registerRuntime(b)

	payload := js.Global().Get("JSON").Call("parse", `{
		"$input.pointer": {"x": 18, "y": -4.5},
		"$input.key": {"space": true}
	}`)
	ret := js.Global().Get("__gosx_set_input_batch").Invoke(payload)
	if !ret.IsNull() {
		t.Fatalf("expected null result, got %q", ret.String())
	}

	pointer, ok := b.GetStore().Get("$input.pointer")
	if !ok {
		t.Fatal("expected pointer signal to be set")
	}
	if got := pointer.Fields["x"].Num; got != 18 {
		t.Fatalf("expected x 18, got %v", got)
	}
	if got := pointer.Fields["y"].Num; got != -4.5 {
		t.Fatalf("expected y -4.5, got %v", got)
	}

	keyboard, ok := b.GetStore().Get("$input.key")
	if !ok {
		t.Fatal("expected key signal to be set")
	}
	if !keyboard.Fields["space"].Bool {
		t.Fatalf("expected space=true, got %#v", keyboard.Fields["space"])
	}
}

func TestRuntimeHydrateTickAndDisposeEngine(t *testing.T) {
	setGlobalValue(t, "__gosx_runtime_ready", js.Undefined())

	b := bridge.New()
	registerRuntime(b)

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

	hydrateRet := js.Global().Get("__gosx_hydrate_engine").Invoke("engine-0", prog.Name, `{}`, string(data), "json")
	if got := hydrateRet.String(); !strings.Contains(got, `"kind":0`) {
		t.Fatalf("expected create-object command result, got %q", got)
	}
	if b.EngineCount() != 1 {
		t.Fatalf("expected 1 engine after hydrate, got %d", b.EngineCount())
	}

	batchRet := js.Global().Get("__gosx_set_input_batch").Invoke(`{"$scene.x":4.5,"$scene.color":"#ff8f6b"}`)
	if !batchRet.IsNull() {
		t.Fatalf("expected null input batch result, got %q", batchRet.String())
	}

	tickRet := js.Global().Get("__gosx_tick_engine").Invoke("engine-0")
	got := tickRet.String()
	if !strings.Contains(got, `"kind":2`) || !strings.Contains(got, `"kind":3`) {
		t.Fatalf("expected transform and material commands, got %q", got)
	}

	disposeRet := js.Global().Get("__gosx_engine_dispose").Invoke("engine-0")
	if !disposeRet.IsNull() {
		t.Fatalf("expected null dispose result, got %q", disposeRet.String())
	}
	if b.EngineCount() != 0 {
		t.Fatalf("expected 0 engines after dispose, got %d", b.EngineCount())
	}
}
