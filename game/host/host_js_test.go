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
	var functions []js.Value
	request := js.FuncOf(func(_ js.Value, args []js.Value) any {
		next++
		callbacks[next] = args[0]
		functions = append(functions, args[0])
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
	t.Cleanup(source.Release)
	fire := func(ts float64) {
		t.Helper()
		if len(callbacks) != 1 {
			t.Fatalf("%d browser requests outstanding, want 1", len(callbacks))
		}
		for id, fn := range callbacks {
			delete(callbacks, id)
			fn.Invoke(ts)
		}
	}
	called := 0
	first := source.RequestFrame(func(now float64) {
		called++
		if now != 123.5 {
			t.Errorf("timestamp %v, want the host timestamp", now)
		}
	})
	second := source.RequestFrame(func(float64) { t.Error("canceled frame ran") })
	if first == second || len(callbacks) != 1 {
		t.Fatalf("handles %d %d with %d browser requests: two requests share one frame", first, second, len(callbacks))
	}
	source.CancelFrame(second)
	fire(123.5)
	source.CancelFrame(first)  // Already delivered: a no-op.
	source.CancelFrame(second) // Already canceled: a no-op.
	if called != 1 || len(callbacks) != 0 || !source.queue.empty() {
		t.Fatal("frame callbacks were retained after delivery/cancellation")
	}
	// A frame callback that asks for the next frame gets one browser request,
	// with the same JS function as before: no js.FuncOf per frame.
	frames := 0
	var loop func(float64)
	loop = func(float64) {
		frames++
		if frames < 5 {
			source.RequestFrame(loop)
		}
	}
	source.RequestFrame(loop)
	for i := 0; i < 5; i++ {
		fire(float64(i))
	}
	if frames != 5 || len(callbacks) != 0 {
		t.Fatalf("ran %d frames with %d requests left", frames, len(callbacks))
	}
	for i, fn := range functions {
		if !fn.Equal(functions[0]) {
			t.Fatalf("browser request %d used another JS function", i)
		}
	}
	// Cancelling the only request cancels the browser request.
	h := source.RequestFrame(func(float64) { t.Error("canceled frame ran") })
	source.CancelFrame(h)
	if len(callbacks) != 0 {
		t.Fatal("cancelling the last request must cancel the browser request")
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
	t.Cleanup(source.Release)
	got := -1.0
	source.RequestFrame(func(ts float64) { got = ts })
	if _, ok := callbacks[101]; !ok || rafCalls != 0 {
		t.Fatalf("worker requests %v, rAF calls %d: the worker clock must serve the request", callbacks, rafCalls)
	}
	fn := callbacks[101]
	delete(callbacks, 101)
	fn.Invoke(16.5)
	if got != 16.5 {
		t.Fatalf("timestamp %v", got)
	}
	h2 := source.RequestFrame(func(float64) { t.Error("canceled frame ran") })
	source.CancelFrame(h2)
	if len(callbacks) != 0 || rafCalls != 0 {
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

func TestGamepadReaderReusesBuffersAndSkipsUnchangedPads(t *testing.T) {
	pad := js.Global().Call("eval", `({index: 1, id: "controller", mapping: "standard", connected: true, timestamp: 5, buttons: [{value: 1}, {value: 0}], axes: [0.5, -0.25]})`)
	pads := js.Global().Get("Array").New(js.Null(), pad)
	reads := 0
	read := js.FuncOf(func(_ js.Value, _ []js.Value) any { reads++; return pads })
	t.Cleanup(read.Release)
	replaceGlobal(t, "navigator", map[string]any{"getGamepads": read})
	reader := NewGamepadReader()
	first := reader.Gamepads()
	if len(first) != 1 || first[0].Index != 1 || first[0].ID != "controller" || first[0].Mapping != "standard" ||
		first[0].Buttons[0] != 1 || first[0].Axes[1] != -0.25 {
		t.Fatalf("snapshot %+v", first)
	}
	buttons := &first[0].Buttons[0]
	// Unchanged timestamp: the reader keeps its snapshot.
	pad.Get("buttons").Index(0).Set("value", 0)
	if got := reader.Gamepads(); got[0].Buttons[0] != 1 {
		t.Fatal("a pad whose timestamp did not change was read again")
	}
	// A new timestamp: the reader reads the pad into the same buffers.
	pad.Set("timestamp", 6)
	second := reader.Gamepads()
	if second[0].Buttons[0] != 0 || &second[0].Buttons[0] != buttons {
		t.Fatal("a changed pad must be read again into the reused buffers")
	}
	// The pad goes and another arrives in the same slot: its id is read again.
	pads.SetIndex(1, js.Null())
	if got := reader.Gamepads(); len(got) != 0 {
		t.Fatalf("a missing pad is still reported: %+v", got)
	}
	pad.Set("id", "other")
	pads.SetIndex(1, pad)
	if got := reader.Gamepads(); len(got) != 1 || got[0].ID != "other" {
		t.Fatalf("a reconnected slot kept the old id: %+v", got)
	}
	if reads != 5 {
		t.Fatalf("navigator.getGamepads ran %d times, want once per poll", reads)
	}
}

func TestForwardedInputPrefersTheBinaryBridge(t *testing.T) {
	bridge := js.Global().Call("eval", `({json: 0, subs: new Set(), onInput(cb) { this.json++; return () => {}; }, onInputBytes(cb) { this.subs.add(cb); return () => this.subs.delete(cb); }, emit(bytes) { for (const cb of this.subs) cb(bytes); }})`)
	replaceGlobal(t, "__gosx", map[string]any{"worker": bridge})
	input, ok := NewForwardedInput()
	if !ok || bridge.Get("json").Int() != 0 {
		t.Fatal("a bridge with onInputBytes must be used in binary form")
	}
	var got []InputEvent
	input.Subscribe(func(e InputEvent) { got = append(got, e) })
	emit := func(e InputEvent) {
		record := encodeInputRecord(e)
		bytes := js.Global().Get("Uint8Array").New(len(record))
		js.CopyBytesToJS(bytes, record)
		bridge.Call("emit", bytes)
	}
	emit(InputEvent{Kind: "key", Code: "KeyA", Down: true, AtMS: 380, ReceivedMS: 382.5})
	emit(InputEvent{Kind: "pad", PadIndex: 0, PadID: "P", Mapping: "standard", Connected: true, Buttons: []float64{1, 0}, Axes: []float64{-1}})
	if !input.KeyDown("KeyA") || len(input.Gamepads()) != 1 || input.Gamepads()[0].Axes[0] != -1 {
		t.Fatal("binary events did not update the tracked state")
	}
	if len(got) != 2 || got[0].Delay() != 2.5 {
		t.Fatalf("events %+v", got)
	}
	bridge.Call("emit", js.Global().Get("Uint8Array").New(3)) // garbage is ignored
	bridge.Call("emit", "not bytes")
	if len(got) != 2 {
		t.Fatal("a malformed record reached subscribers")
	}
	input.Close()
	if bridge.Get("subs").Get("size").Int() != 0 {
		t.Fatal("Close must unsubscribe from the binary bridge")
	}
}
