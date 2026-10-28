//go:build js && wasm && !gosx_tiny_islands_only && !gosx_runtime_core && !gosx_runtime_collab

package main

import (
	"fmt"
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

// runtimeText is what the runtime itself holds for a signal, as JSON text.
func runtimeText(name string) string {
	return js.Global().Call("__gosx_get_shared_signal", name).String()
}

// heardLog subscribes a raw subscriber that keeps every text it is given.
func heardLog(t *testing.T, ctx js.Value, name string, log map[string][]string) {
	t.Helper()

	handler := js.FuncOf(func(this js.Value, args []js.Value) any {
		log[name] = append(log[name], args[0].String())
		return nil
	})
	t.Cleanup(handler.Release)
	options := js.Global().Get("Object").New()
	options.Set("immediate", false)
	ctx.Call("subscribeSignalJSON", name, handler, options)
}

func TestSubscribersEndOnTheStoredValueWhenASubscriberWritesASignalOfTheBatch(t *testing.T) {
	// The first signal of the batch to be announced has a subscriber that writes the
	// other one. The runtime tells the page about that write at once, so the batch's
	// own text for the other signal is out of date when its turn comes. Which signal
	// is announced first depends on the runtime's map order, so the trials repeat.
	for trial := 0; trial < 30; trial++ {
		t.Run(fmt.Sprint(trial), func(t *testing.T) {
			ctx, _ := pageWithRuntime(t)
			heard := map[string][]string{}
			for _, name := range []string{"$a", "$b"} {
				heardLog(t, ctx, name, heard)
			}
			wrote := false
			for name, other := range map[string]string{"$a": "$b", "$b": "$a"} {
				name, other := name, other
				writer := js.FuncOf(func(this js.Value, args []js.Value) any {
					if !wrote {
						wrote = true
						ctx.Call("setSignal", other, "written from "+name)
					}
					return nil
				})
				t.Cleanup(writer.Release)
				options := js.Global().Get("Object").New()
				options.Set("immediate", false)
				ctx.Call("subscribeSignal", name, writer, options)
			}

			if result := ctx.Call("setSignalsJSON", `{"$a":1,"$b":"batch"}`, false); !result.IsNull() {
				t.Fatalf("setSignalsJSON = %v", result)
			}
			if !wrote {
				t.Fatal("no subscriber ran")
			}
			for _, name := range []string{"$a", "$b"} {
				last := heard[name][len(heard[name])-1]
				stored := js.Global().Get("JSON").Call("stringify", ctx.Call("getSignal", name)).String()
				if last != stored || last != runtimeText(name) {
					t.Errorf("%s: the subscriber ended on %s, the page holds %s, the runtime holds %s (heard %v)",
						name, last, stored, runtimeText(name), heard[name])
				}
			}
		})
	}
}

func TestSubscriberBatchSupersedesTheOlderBatchThatWokeIt(t *testing.T) {
	ctx, _ := pageWithRuntime(t)
	heard := map[string][]string{}
	heardLog(t, ctx, "$b", heard)
	var once bool
	sender := js.FuncOf(func(this js.Value, args []js.Value) any {
		if !once {
			once = true
			ctx.Call("setSignalsJSON", `{"$b":"inner"}`, false)
		}
		return nil
	})
	t.Cleanup(sender.Release)
	options := js.Global().Get("Object").New()
	options.Set("immediate", false)
	ctx.Call("subscribeSignal", "$a", sender, options)

	// $a is sent last as well as first, so the order of the runtime's map does not matter:
	// whichever of them is announced first, $b ends on the inner batch.
	ctx.Call("setSignalsJSON", `{"$a":1,"$b":"outer"}`, false)
	if last := heard["$b"][len(heard["$b"])-1]; last != `"inner"` || runtimeText("$b") != `"inner"` {
		t.Fatalf("$b: subscriber ended on %s, runtime holds %s (heard %v)", last, runtimeText("$b"), heard["$b"])
	}
}

func TestPageKeepsALegacyNameCurrentWhenTheSenderSpellsIt(t *testing.T) {
	ctx, _ := pageWithRuntime(t)

	// Reading the legacy name caches it in the page. The runtime announces changes
	// of $scene.event.selectedID as $surface.event.selectedID, so the page must be
	// told the legacy spelling as well or its copy goes stale.
	if first := ctx.Call("getSignal", "$scene.event.selectedID"); !first.IsNull() && !first.IsUndefined() {
		t.Fatalf("before any batch: %v", first)
	}
	for _, value := range []string{"n1", "n2", "n3"} {
		ctx.Call("setSignalsJSON", `{"$scene.event.selectedID":"`+value+`"}`, true)
		if got := ctx.Call("getSignal", "$scene.event.selectedID"); got.String() != value {
			t.Errorf("legacy spelling after %s: %v", value, got)
		}
		if got := ctx.Call("getSignal", "$surface.event.selectedID"); got.String() != value {
			t.Errorf("canonical spelling after %s: %v", value, got)
		}
	}
	// And the canonical spelling moves the cached legacy copy only through the sender's own words.
	ctx.Call("setSignalsJSON", `{"$surface.event.selectedID":"n4"}`, true)
	if got := ctx.Call("getSignal", "$surface.event.selectedID"); got.String() != "n4" {
		t.Errorf("canonical spelling: %v", got)
	}
}

func TestChangedOnlyBatchDoesNotRepairAValueOnlyThePageHolds(t *testing.T) {
	ctx, _ := pageWithRuntime(t)

	// A signal the page was told about by hand, and the runtime never heard of
	// that value. The page used to parse every batch and store what the sender
	// said, so any snapshot repaired such a value. The runtime now decides what
	// changed, and a snapshot that equals what the runtime holds changes nothing,
	// so the page keeps its own value until the runtime announces something else.
	ctx.Call("setSignalsJSON", `{"$x":0}`, true)
	js.Global().Call("__gosx_notify_shared_signal", "$x", "5")
	if got := ctx.Call("getSignal", "$x"); got.Int() != 5 {
		t.Fatalf("$x = %v after the manual notification", got)
	}
	ctx.Call("setSignalsJSON", `{"$x":0}`, true)
	if got := ctx.Call("getSignal", "$x"); got.Int() != 5 || runtimeText("$x") != "0" {
		t.Fatalf("changed-only snapshot: page %v, runtime %s; the documented behavior is that the page keeps 5", got, runtimeText("$x"))
	}
	// A snapshot with a different value announces it and settles both.
	ctx.Call("setSignalsJSON", `{"$x":1}`, true)
	if got := ctx.Call("getSignal", "$x"); got.Int() != 1 || runtimeText("$x") != "1" {
		t.Fatalf("after a real change: page %v, runtime %s", got, runtimeText("$x"))
	}
}
