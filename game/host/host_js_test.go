//go:build js && wasm

package host

import (
	"syscall/js"
	"testing"
)

func replaceGlobal(t *testing.T, name string, value any) {
	t.Helper()
	object := js.Global().Get("Object")
	previous := object.Call("getOwnPropertyDescriptor", js.Global(), name)
	object.Call("defineProperty", js.Global(), name, map[string]any{"configurable": true, "writable": true, "value": value})
	t.Cleanup(func() {
		if previous.IsUndefined() {
			js.Global().Get("Reflect").Call("deleteProperty", js.Global(), name)
		} else {
			object.Call("defineProperty", js.Global(), name, previous)
		}
	})
}

func TestBrowserFrameDeliveryAndCancellation(t *testing.T) {
	next := 0
	callbacks := map[int]js.Value{}
	request := js.FuncOf(func(_ js.Value, args []js.Value) any {
		next++
		callbacks[next] = args[0]
		return next
	})
	cancel := js.FuncOf(func(_ js.Value, args []js.Value) any {
		delete(callbacks, args[0].Int())
		return nil
	})
	t.Cleanup(request.Release)
	t.Cleanup(cancel.Release)
	replaceGlobal(t, "requestAnimationFrame", request)
	replaceGlobal(t, "cancelAnimationFrame", cancel)
	replaceGlobal(t, "document", map[string]any{"hidden": false})
	source := NewFrameSourceJS()
	called := 0
	first := source.RequestFrame(func(now float64) {
		called++
		if now != 123.5 || len(source.pending) != 1 {
			t.Error("delivery must pass the host timestamp and release only its own callback")
		}
	})
	second := source.RequestFrame(func(float64) { t.Error("canceled frame ran") })
	fn := callbacks[first]
	delete(callbacks, first)
	fn.Invoke(123.5)
	source.CancelFrame(first) // Already delivered: no second release.
	source.CancelFrame(second)
	source.CancelFrame(second) // Already canceled: no second release.
	if called != 1 || len(callbacks) != 0 || len(source.pending) != 0 {
		t.Fatal("frame callbacks were retained after delivery/cancellation")
	}
	if source.Hidden() {
		t.Fatal("visible document reported hidden")
	}
	js.Global().Get("document").Set("hidden", true)
	if !source.Hidden() {
		t.Fatal("hidden document reported visible")
	}
}

func TestBrowserGamepadSnapshots(t *testing.T) {
	buttons := []any{map[string]any{"value": 1.0}, map[string]any{"value": .25}}
	pad := js.ValueOf(map[string]any{"index": 2, "id": "controller", "mapping": "standard", "buttons": buttons, "axes": []any{.5, -.75}})
	read := js.FuncOf(func(_ js.Value, _ []js.Value) any { return []any{nil, pad} })
	t.Cleanup(read.Release)
	replaceGlobal(t, "navigator", map[string]any{"getGamepads": read})
	source := NavigatorSource{}
	first := source.Gamepads()
	if len(first) != 1 || first[0].Index != 2 || first[0].ID != "controller" || first[0].Mapping != "standard" ||
		first[0].Buttons[0] != 1 || first[0].Buttons[1] != .25 || first[0].Axes[0] != .5 || first[0].Axes[1] != -.75 {
		t.Fatalf("wrong device snapshot: %+v", first)
	}
	pad.Get("buttons").Index(0).Set("value", 0)
	second := source.Gamepads()
	if first[0].Buttons[0] != 1 || second[0].Buttons[0] != 0 {
		t.Fatal("snapshots must not alias mutable browser state or each other")
	}
	js.Global().Set("navigator", map[string]any{})
	if got := source.Gamepads(); len(got) != 0 {
		t.Fatal("missing Gamepad API must yield no devices")
	}
}

func TestWorkerFrameSourceUsesTheWorkerClockAndForwardedVisibility(t *testing.T) {
	next := 100
	callbacks := map[int]js.Value{}
	request := js.FuncOf(func(_ js.Value, args []js.Value) any { next++; callbacks[next] = args[0]; return next })
	cancel := js.FuncOf(func(_ js.Value, args []js.Value) any { delete(callbacks, args[0].Int()); return nil })
	t.Cleanup(request.Release)
	t.Cleanup(cancel.Release)
	// A worker can have requestAnimationFrame; it must not be used.
	rafCalls := 0
	raf := js.FuncOf(func(_ js.Value, _ []js.Value) any { rafCalls++; return 1 })
	t.Cleanup(raf.Release)
	replaceGlobal(t, "requestAnimationFrame", raf)
	replaceGlobal(t, "cancelAnimationFrame", raf)
	worker := map[string]any{"requestFrame": request, "cancelFrame": cancel, "hidden": false}
	replaceGlobal(t, "__gosx", map[string]any{"worker": worker})
	replaceGlobal(t, "document", js.Undefined())

	source := NewFrameSourceJS()
	got := -1.0
	h := source.RequestFrame(func(ts float64) { got = ts })
	if h != 101 || rafCalls != 0 {
		t.Fatalf("handle %d, rAF calls %d: the worker clock must serve the request", h, rafCalls)
	}
	callbacks[h].Invoke(16.5)
	if got != 16.5 {
		t.Fatalf("timestamp %v", got)
	}
	h2 := source.RequestFrame(func(float64) { t.Error("canceled frame ran") })
	source.CancelFrame(h2)
	if _, still := callbacks[h2]; still || rafCalls != 0 {
		t.Fatal("cancel must reach the worker clock")
	}
	if source.Hidden() {
		t.Fatal("a visible page reported hidden")
	}
	js.Global().Get("__gosx").Get("worker").Set("hidden", true)
	if !source.Hidden() {
		t.Fatal("forwarded page visibility must drive Hidden() when there is no document")
	}
}

func TestForwardedInputBindsToTheWorkerBridgeAndReleasesIt(t *testing.T) {
	bridge := js.Global().Call("eval", `({subs: new Set(), onInput(cb) { this.subs.add(cb); return () => this.subs.delete(cb); }, emit(ev) { for (const cb of this.subs) cb(JSON.stringify(ev)); }})`)
	replaceGlobal(t, "__gosx", map[string]any{"worker": bridge})
	input, ok := NewForwardedInput()
	if !ok {
		t.Fatal("a worker bridge with onInput must yield forwarded input")
	}
	var got []InputEvent
	input.Subscribe(func(e InputEvent) { got = append(got, e) })
	bridge.Call("emit", map[string]any{"k": "key", "code": "KeyA", "down": true, "ts": 5380, "at": 380, "recv": 382.5})
	bridge.Call("emit", map[string]any{"k": "pad", "index": 0, "id": "P", "mapping": "standard", "connected": true, "buttons": []any{1, 0}, "axes": []any{-1}, "ts": 1, "at": 1, "recv": 2})
	if !input.KeyDown("KeyA") || len(input.Gamepads()) != 1 || input.Gamepads()[0].Axes[0] != -1 {
		t.Fatal("forwarded events did not update the tracked state")
	}
	if len(got) != 2 || got[0].Delay() != 2.5 || got[0].PageEpochMS != 5380 {
		t.Fatalf("events %+v", got)
	}
	bridge.Call("emit", "not an object") // garbage must be ignored, not crash
	input.Close()
	input.Close()
	if bridge.Get("subs").Get("size").Int() != 0 {
		t.Fatal("Close must unsubscribe from the worker bridge")
	}
	replaceGlobal(t, "__gosx", map[string]any{})
	if _, ok := NewForwardedInput(); ok {
		t.Fatal("no worker bridge: forwarded input must be unavailable")
	}
}
