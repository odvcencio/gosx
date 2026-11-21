//go:build js && wasm && gosx_padtest

package host

import (
	"strings"
	"syscall/js"
	"testing"
)

func installTestHooks(t *testing.T, h *padHooks) {
	t.Helper()
	padHook.Store(h)
	t.Cleanup(func() { padHook.Store(nil) })
}

func TestGamepadReaderUsesTheOverrideAndNeverTheNavigator(t *testing.T) {
	calls := 0
	read := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		calls++
		return js.Global().Call("eval", `([{index: 0, id: "real", mapping: "standard", connected: true, timestamp: 1, buttons: [{value: 1}], axes: []}])`)
	})
	t.Cleanup(read.Release)
	replaceGlobal(t, "navigator", map[string]any{"getGamepads": read})
	reader := NewGamepadReader()
	if got := reader.Gamepads(); len(got) != 1 || got[0].ID != "real" {
		t.Fatalf("real path: %+v", got)
	}
	calls = 0
	src := NewPadTestSource()
	installTestHooks(t, &padHooks{source: src})
	if got := reader.Gamepads(); len(got) != 0 {
		t.Fatalf("with an empty override the real pad leaked: %+v", got)
	}
	if err := src.Enqueue(PadSet{Seq: 1, Next: true, Pads: []GamepadState{NeutralStandardPad(1, "synthetic", "standard")}}); err != nil {
		t.Fatal(err)
	}
	src.BeginFrame()
	got := reader.Gamepads()
	if len(got) != 1 || got[0].Index != 1 || got[0].ID != "synthetic" {
		t.Fatalf("override pads: %+v", got)
	}
	src.Lose()
	src.BeginFrame()
	if got := reader.Gamepads(); len(got) != 0 {
		t.Fatalf("a lost source must expose no pads: %+v", got)
	}
	if calls != 0 {
		t.Fatalf("navigator.getGamepads ran %d times while the override was active", calls)
	}
}

func resetDesignated(t *testing.T) {
	t.Helper()
	padDesignated, padInFrame = nil, false
	t.Cleanup(func() { padDesignated, padInFrame = nil, false })
}

// The bracket wraps the game's callback: begin before, end after, once per
// frame, through a SceneFrameSource on its fallback clock (which runs inside a
// FrameSourceJS), and not at all for a batch with no callback.
func TestFrameBracketWrapsTheGameCallbackOnce(t *testing.T) {
	resetDesignated(t)
	var log []string
	installTestHooks(t, &padHooks{
		begin: func() { log = append(log, "begin") },
		end:   func() { log = append(log, "end") },
	})
	_, raf := fakeScenePage(t)
	source := NewSceneFrameSource("arena")
	t.Cleanup(source.Release)
	frames := 0
	var step func(float64)
	step = func(float64) {
		frames++
		log = append(log, "game")
		if frames < 2 {
			source.RequestFrame(step)
		}
	}
	source.RequestFrame(step)
	fireRAF(t, raf, 10)
	fireRAF(t, raf, 20)
	want := "begin game end begin game end"
	if got := strings.Join(log, " "); got != want {
		t.Fatalf("log %q, want %q", got, want)
	}
	if padInFrame {
		t.Fatal("frame left open")
	}
	plain := NewFrameSourceJS()
	t.Cleanup(plain.Release)
	log = nil
	plain.onFrame(js.Undefined(), []js.Value{js.ValueOf(30.0)})
	if len(log) != 0 {
		t.Fatalf("an empty batch ran the hooks: %v", log)
	}
}

// Only the first source that runs a callback is the designated one; a second
// source in the same tick, or later, makes no frames.
func TestFrameBracketCountsOnlyTheDesignatedSource(t *testing.T) {
	resetDesignated(t)
	begins := 0
	installTestHooks(t, &padHooks{begin: func() { begins++ }})
	_, raf := fakeScenePage(t)
	a, b := NewFrameSourceJS(), NewFrameSourceJS()
	t.Cleanup(a.Release)
	t.Cleanup(b.Release)
	ran := 0
	a.RequestFrame(func(float64) { ran++ })
	b.RequestFrame(func(float64) { ran++ })
	if len(raf) != 2 {
		t.Fatalf("%d rAF requests, want one per source", len(raf))
	}
	for id, fn := range raf {
		delete(raf, id)
		fn.Invoke(1.0)
	}
	if ran != 2 || begins != 1 {
		t.Fatalf("ran %d callbacks, %d pad frames; want 2 and 1", ran, begins)
	}
	a.RequestFrame(func(float64) {})
	b.RequestFrame(func(float64) {})
	for id, fn := range raf {
		delete(raf, id)
		fn.Invoke(2.0)
	}
	if begins != 2 {
		t.Fatalf("%d pad frames after a second tick, want 2 (one per tick, from the designated source)", begins)
	}
}

// A callback panic still ends the frame, and a cancelled request makes none.
func TestFrameBracketPanicAndCancel(t *testing.T) {
	resetDesignated(t)
	var log []string
	installTestHooks(t, &padHooks{begin: func() { log = append(log, "begin") }, end: func() { log = append(log, "end") }})
	_, raf := fakeScenePage(t)
	source := NewSceneFrameSource("arena")
	t.Cleanup(source.Release)
	h := source.RequestFrame(func(float64) { log = append(log, "never") })
	source.CancelFrame(h)
	for id, fn := range raf {
		delete(raf, id)
		fn.Invoke(1.0)
	}
	if len(log) != 0 {
		t.Fatalf("a cancelled fallback made a frame: %v", log)
	}
	source.RequestFrame(func(float64) { panic("boom") })
	func() {
		defer func() { _ = recover() }()
		source.run(2)
	}()
	if got := strings.Join(log, " "); got != "begin end" || padInFrame {
		t.Fatalf("after a panic: log %q, in frame %v", got, padInFrame)
	}
}
