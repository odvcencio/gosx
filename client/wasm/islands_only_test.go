//go:build js && wasm && gosx_tiny_islands_only

package main

import (
	"encoding/json"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/client/bridge"
	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/program"
)

func compileSlimIslandProgram(t *testing.T, source string) *program.Program {
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

func setSlimGlobalFunc(t *testing.T, name string, fn func(this js.Value, args []js.Value) any) {
	t.Helper()

	prev := js.Global().Get(name)
	wrapped := js.FuncOf(fn)
	js.Global().Set(name, wrapped)
	t.Cleanup(func() {
		wrapped.Release()
		js.Global().Set(name, prev)
	})
}

func setSlimGlobalValue(t *testing.T, name string, value any) {
	t.Helper()

	prev := js.Global().Get(name)
	js.Global().Set(name, value)
	t.Cleanup(func() {
		js.Global().Set(name, prev)
	})
}

func TestIslandsOnlyRuntimeHydrateAndDispatchCompiledGSX(t *testing.T) {
	prog := compileSlimIslandProgram(t, `package main

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
	setSlimGlobalFunc(t, "__gosx_apply_patches", func(this js.Value, args []js.Value) any {
		patchIslandID = args[0].String()
		if err := json.Unmarshal([]byte(args[1].String()), &patches); err != nil {
			t.Fatalf("unmarshal patches: %v", err)
		}
		return nil
	})
	setSlimGlobalValue(t, "__gosx_runtime_ready", js.Undefined())

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

func TestIslandsOnlyRuntimeHydrateComputeIsland(t *testing.T) {
	prog := &program.Program{
		Name: "ComputeReader",
		Nodes: []program.Node{
			{Kind: program.NodeExpr, Expr: 0},
		},
		Root: 0,
		Exprs: []program.Expr{
			{Op: program.OpSignalGet, Value: "$shared.count", Type: program.TypeInt},
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},
		},
		Signals: []program.SignalDef{
			{Name: "$shared.count", Type: program.TypeInt, Init: 1},
		},
	}
	data, err := program.EncodeJSON(prog)
	if err != nil {
		t.Fatalf("encode json: %v", err)
	}

	var patches int
	setSlimGlobalFunc(t, "__gosx_apply_patches", func(this js.Value, args []js.Value) any {
		patches++
		return nil
	})
	setSlimGlobalValue(t, "__gosx_runtime_ready", js.Undefined())

	b := bridge.New()
	registerRuntime(b)

	ret := js.Global().Get("__gosx_hydrate_compute").Invoke("compute-0", prog.Name, `{}`, string(data), "json")
	if !ret.IsNull() {
		t.Fatalf("expected null hydrate result, got %q", ret.String())
	}
	if b.ComputeIslandCount() != 1 {
		t.Fatalf("expected one compute island, got %d", b.ComputeIslandCount())
	}
	if ret := js.Global().Get("__gosx_set_shared_signal").Invoke("$shared.count", `7`); !ret.IsNull() {
		t.Fatalf("expected null shared signal result, got %q", ret.String())
	}
	if patches != 0 {
		t.Fatalf("compute island should suppress DOM patches, got %d callback(s)", patches)
	}
}

func TestIslandsOnlyRuntimeOmitsFullRuntimeExports(t *testing.T) {
	setSlimGlobalValue(t, "__gosx_runtime_ready", js.Undefined())
	registerRuntime(bridge.New())

	for _, name := range []string{
		"__gosx_hydrate_engine",
		"__gosx_tick_engine",
		"__gosx_engine_dispose",
		"__gosx_text_layout",
		"__gosx_crdt_apply",
		"__gosx_render_canvas",
		"__gosx_tick_canvas",
		"__gosx_canvas_event",
		"__gosx_dispose_canvas",
	} {
		if got := js.Global().Get(name); got.Type() != js.TypeUndefined {
			t.Fatalf("expected %s to be omitted from islands-only runtime, got %v", name, got.Type())
		}
	}
}
