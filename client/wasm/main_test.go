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
