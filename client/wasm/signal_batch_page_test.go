//go:build js && wasm && !gosx_tiny_islands_only && !gosx_runtime_core && !gosx_runtime_collab

package main

import (
	"os"
	"strings"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/client/bridge"
)

// These tests run the page's real bootstrap code (the shared signal store and
// the engine context) against this package's real runtime, in one JavaScript
// realm. The page's tests use a stand-in for the runtime, and the runtime's tests
// use stand-ins for the page; only this file catches the two drifting apart.

func readBootstrapSource(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile("../js/bootstrap-src/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func between(t *testing.T, source, from, to string) string {
	t.Helper()

	start := strings.Index(source, from)
	end := strings.Index(source, to)
	if start < 0 || end < start {
		t.Fatalf("bootstrap source no longer has %q before %q", from, to)
	}
	return source[start:end]
}

// pageWithRuntime registers the runtime and returns an engine context made by
// the page's own code. Every JSON.parse the page makes counts toward the
// number the returned function reports.
func pageWithRuntime(t *testing.T) (ctx js.Value, parses func() int) {
	t.Helper()

	setGlobalValue(t, "__gosx_runtime_ready", js.Undefined())
	for _, name := range []string{"__gosx", "__gosx_notify_shared_signal", "__gosx_subscribe_shared_signal", "__gosx_get_shared_signal", "__gosx_set_shared_signal"} {
		name, prev := name, js.Global().Get(name)
		t.Cleanup(func() { js.Global().Set(name, prev) })
	}
	layout := readBootstrapSource(t, "00-textlayout.ts")
	mounting := readBootstrapSource(t, "30b-tail-engine-mounting.ts")
	store := between(t, layout, "  // The page's shared signal values.", "  function gosxIssueStore()")
	engine := between(t, mounting, "function createEngineContext(", "async function mountEngine(")

	// A fresh namespace, so a store left by another test does not leak in.
	js.Global().Set("__gosx", js.Undefined())
	registerRuntime(bridge.New())

	count := js.Global().Get("Object").New()
	count.Set("parses", 0)
	realJSON := js.Global().Get("JSON")
	countingJSON := js.Global().Get("Object").New()
	countingJSON.Set("stringify", realJSON.Get("stringify"))
	parse := js.FuncOf(func(this js.Value, args []js.Value) any {
		count.Set("parses", count.Get("parses").Int()+1)
		return realJSON.Call("parse", args[0])
	})
	t.Cleanup(parse.Release)
	countingJSON.Set("parse", parse)

	factory := js.Global().Get("Function").New("window", "document", "console", "JSON", "pendingEngineOwned", "goWASMEnginePageGeneration",
		"const GOSX_VERSION = 'test';\n"+store+"\n"+engine+"\nreturn createEngineContext;")
	owned := js.FuncOf(func(js.Value, []js.Value) any { return true })
	t.Cleanup(owned.Release)
	create := factory.Invoke(js.Global(), js.Global().Get("Object").New(), js.Global().Get("console"), countingJSON, owned, 1)

	pending := js.Global().Get("Object").New()
	pending.Set("generation", 1)
	pending.Set("closed", false)
	options := js.Global().Get("Object").New()
	options.Set("required", js.Global().Get("Array").New())
	ctx = create.Invoke(js.Global().Get("Object").New(), js.Global().Get("Object").New(), js.Global().Get("Object").New(), options, pending)
	return ctx, func() int { return count.Get("parses").Int() }
}

func rawSubscriber(t *testing.T, ctx js.Value, name string, got map[string]string) {
	t.Helper()

	handler := js.FuncOf(func(this js.Value, args []js.Value) any {
		got[args[1].String()] = args[0].String()
		return nil
	})
	t.Cleanup(handler.Release)
	options := js.Global().Get("Object").New()
	options.Set("immediate", false)
	ctx.Call("subscribeSignalJSON", name, handler, options)
}

func TestPageAndRuntimeAgreeOnSignalBatches(t *testing.T) {
	ctx, parses := pageWithRuntime(t)

	got := map[string]string{}
	for _, name := range []string{"$hud", "$rail", "$bar"} {
		rawSubscriber(t, ctx, name, got)
	}

	// A raw subscriber gets the runtime's own text for each signal, and the page decodes nothing:
	// the runtime decodes once, and nothing in the page asks for a value.
	batch := ` { "$hud" : { "tick" : 4 , "cash": {"v":1.50} } , "$rail":[ 1, 2 ], "$bar" : "text" } `
	if result := ctx.Call("setSignalsJSON", batch, false); !result.IsNull() {
		t.Fatalf("setSignalsJSON = %v", result)
	}
	want := map[string]string{"$hud": `{"cash":{"v":1.5},"tick":4}`, "$rail": `[1,2]`, "$bar": `"text"`}
	for name, text := range want {
		if got[name] != text {
			t.Errorf("%s: subscriber got %q, want %q", name, got[name], text)
		}
	}
	if n := parses(); n != 0 {
		t.Fatalf("the page parsed JSON %d times for a batch that only raw subscribers read", n)
	}

	// A reader decodes the one signal it asks for, once.
	hud := ctx.Call("getSignal", "$hud")
	if hud.Get("tick").Int() != 4 || hud.Get("cash").Get("v").Float() != 1.5 {
		t.Fatalf("$hud = %v", js.Global().Get("JSON").Call("stringify", hud))
	}
	if ctx.Call("getSignal", "$hud").Equal(hud) != true {
		t.Error("a second read returned another object")
	}
	if n := parses(); n != 1 {
		t.Fatalf("reading one signal twice parsed %d times, want 1", n)
	}
}

func TestPageSeesTheRuntimeChangedOnlyAndRefusals(t *testing.T) {
	ctx, parses := pageWithRuntime(t)

	got := map[string]string{}
	for _, name := range []string{"$a", "$b"} {
		rawSubscriber(t, ctx, name, got)
	}
	ctx.Call("setSignalsJSON", `{"$a":1,"$b":{"x":[1,2]}}`, true)
	delete(got, "$a")
	delete(got, "$b")

	// Only what changed reaches the subscribers; the unchanged $b stays quiet even in other spelling.
	ctx.Call("setSignalsJSON", `{"$a":2, "$b" : {"x" : [1, 2]}}`, true)
	if len(got) != 1 || got["$a"] != "2" {
		t.Fatalf("subscribers heard %v, want only $a=2", got)
	}

	// A refused batch throws, and the page keeps its values.
	for _, bad := range []string{`{"$a":`, `{" ":1,"$a":9}`, `[1]`, `null`, ``} {
		if !throws(ctx, "setSignalsJSON", bad, true) {
			t.Errorf("%q was accepted", bad)
		}
	}
	if a := ctx.Call("getSignal", "$a"); a.Int() != 2 {
		t.Fatalf("$a = %v after refused batches, want 2", a)
	}
	if n := parses(); n != 1 {
		t.Fatalf("page parsed %d times, want only the one read of $a", n)
	}
}

func TestPageSingleWritesStillReachTheRuntimeAndBack(t *testing.T) {
	ctx, _ := pageWithRuntime(t)

	got := map[string]string{}
	rawSubscriber(t, ctx, "$mode", got)
	ctx.Call("setSignal", "$mode", map[string]any{"name": "dock"})
	if got["$mode"] != `{"name":"dock"}` {
		t.Fatalf("subscriber got %q", got["$mode"])
	}
	if mode := ctx.Call("getSignal", "$mode"); mode.Get("name").String() != "dock" {
		t.Fatalf("$mode = %v", mode)
	}
	// A batch then reads the runtime's own store: the same value is no change.
	delete(got, "$mode")
	ctx.Call("setSignalsJSON", `{"$mode": {"name": "dock"}}`, true)
	if len(got) != 0 {
		t.Fatalf("an unchanged value was announced: %v", got)
	}
}

// throws reports whether calling the method raised a JavaScript error.
func throws(target js.Value, method string, args ...any) (raised bool) {
	defer func() {
		if recover() != nil {
			raised = true
		}
	}()
	target.Call(method, args...)
	return false
}
