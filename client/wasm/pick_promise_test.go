//go:build js && wasm && !gosx_tiny_islands_only && !gosx_runtime_core && !gosx_runtime_collab

package main

import (
	"syscall/js"
	"testing"
	"time"

	"m31labs.dev/gosx/internal/jsreentry"
	"m31labs.dev/gosx/render/bundle"
)

// __gosx_pick_engine returns a Promise. Before, it built that Promise with a Go
// executor, which JavaScript ran inside the constructor, nested in the call
// from Go into JavaScript, and which was never released.
func TestPickEngineBuildsItsPromiseWithoutRunningGoInsideTheConstructor(t *testing.T) {
	const id = "pick-promise-test"
	engineRendererMu.Lock()
	engineRendererRegistry[id] = &engineRenderer{renderer: &bundle.Renderer{}}
	engineRendererMu.Unlock()
	t.Cleanup(func() {
		engineRendererMu.Lock()
		delete(engineRendererRegistry, id)
		engineRendererMu.Unlock()
	})

	probe := jsreentry.Install(t)
	pick := pickEngineRuntimeFunc(nil) // the bridge is read only when the call names an event type
	defer pick.Release()
	// Call it as the page does, from JavaScript on a timer turn, so that this
	// call is the outermost entry into Go.
	js.Global().Get("Function").New("pick", "id", `
		globalThis.__gosxPicked = undefined;
		setTimeout(function () { globalThis.__gosxPicked = pick(id, 3, 4); }, 0);
	`).Invoke(pick, id)
	deadline := time.Now().Add(5 * time.Second)
	for js.Global().Get("__gosxPicked").IsUndefined() {
		if time.Now().After(deadline) {
			t.Fatal("__gosx_pick_engine never returned")
		}
		time.Sleep(time.Millisecond)
	}
	picked := js.Global().Get("__gosxPicked")
	js.Global().Delete("__gosxPicked")
	if !picked.InstanceOf(js.Global().Get("Promise")) {
		t.Fatalf("__gosx_pick_engine returned %v, not a Promise", picked)
	}
	probe.AssertNone(t, "__gosx_pick_engine")
	if probe.Entries() == 0 {
		t.Fatal("the probe never saw the call")
	}
}
