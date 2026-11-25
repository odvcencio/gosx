//go:build js && wasm

package host

import (
	"syscall/js"
	"testing"
)

// The real navigator path: a pad that reports timestamp 0 (some browsers never
// fill it in) is read on every poll, and a nonzero timestamp that did not
// change is not. Delegation must leave both as they were.
func TestGamepadReaderRealPathZeroTimestampIsAlwaysRead(t *testing.T) {
	pad := js.Global().Call("eval", `({index: 0, id: "controller", mapping: "standard", connected: true, timestamp: 0, buttons: [{value: 0}], axes: [0, 0]})`)
	pads := js.Global().Get("Array").New(pad)
	read := js.FuncOf(func(_ js.Value, _ []js.Value) any { return pads })
	t.Cleanup(read.Release)
	replaceGlobal(t, "navigator", map[string]any{"getGamepads": read})
	reader := NewGamepadReader()
	first := reader.Gamepads()
	buttons := &first[0].Buttons[0]
	pad.Get("buttons").Index(0).Set("value", 1)
	second := reader.Gamepads()
	if second[0].Buttons[0] != 1 || &second[0].Buttons[0] != buttons {
		t.Fatalf("zero timestamp: pad not read again into the same buffer: %+v", second)
	}
	pad.Set("timestamp", 9)
	reader.Gamepads()
	pad.Get("buttons").Index(0).Set("value", 0)
	if got := reader.Gamepads(); got[0].Buttons[0] != 1 {
		t.Fatal("a nonzero timestamp that did not change was read again")
	}
}

// A pad leaves and another takes its slot between two polls, with an
// unchanged timestamp: after the connection event the reader reads the new
// id, so the new pad is never reported as the old one.
func TestGamepadReaderRereadsTheIDAfterAConnectionEvent(t *testing.T) {
	pad := js.Global().Call("eval", `({index: 0, id: "Xbox", mapping: "standard", connected: true, timestamp: 5, buttons: [{value: 0}], axes: [0, 0]})`)
	pads := js.Global().Get("Array").New(pad)
	read := js.FuncOf(func(_ js.Value, _ []js.Value) any { return pads })
	t.Cleanup(read.Release)
	replaceGlobal(t, "navigator", map[string]any{"getGamepads": read})
	reader := NewGamepadReader()
	if got := reader.Gamepads(); got[0].ID != "Xbox" {
		t.Fatalf("first poll: %+v", got)
	}
	pad.Set("id", "DualSense")
	if got := reader.Gamepads(); got[0].ID != "Xbox" {
		t.Fatal("without an event the slot keeps its id (the cheap path)")
	}
	ev := js.Global().Call("eval", `({gamepad: {index: 0}})`)
	reader.listen.Invoke(ev)
	if got := reader.Gamepads(); got[0].ID != "DualSense" {
		t.Fatalf("after gamepadconnected: %+v", got)
	}
}
