//go:build js && wasm

package host

import (
	"syscall/js"
	"testing"
)

// fakeScenePage installs a document with one mount element and a fake
// requestAnimationFrame, and returns the mount and the pending rAF callbacks.
func fakeScenePage(t *testing.T) (mount js.Value, raf map[int]js.Value) {
	t.Helper()
	raf = map[int]js.Value{}
	next := 0
	request := js.FuncOf(func(_ js.Value, args []js.Value) any { next++; raf[next] = args[0]; return next })
	cancel := js.FuncOf(func(_ js.Value, args []js.Value) any { delete(raf, args[0].Int()); return nil })
	t.Cleanup(request.Release)
	t.Cleanup(cancel.Release)
	replaceGlobal(t, "requestAnimationFrame", request)
	replaceGlobal(t, "cancelAnimationFrame", cancel)
	replaceGlobal(t, "__gosx", js.Undefined())
	mount = js.Global().Call("eval", `({})`)
	doc := js.Global().Call("eval", `(function(mount) { return { hidden: false, getElementById(id) { return id === "arena" ? mount : null; } }; })`).Invoke(mount)
	replaceGlobal(t, "document", doc)
	return mount, raf
}

// installHandle gives the mount a renderer handle with setFrameHook, the way
// the Scene3D mount does once its renderer is ready.
func installHandle(mount js.Value) js.Value {
	handle := js.Global().Call("eval", `({ hook: null, registrations: 0, setFrameHook(fn) { this.hook = fn; this.registrations++; const self = this; return function() { if (self.hook === fn) self.hook = null; }; } })`)
	mount.Set("__gosxScene3DHandle", handle)
	return handle
}

func fireRAF(t *testing.T, raf map[int]js.Value, ts float64) {
	t.Helper()
	if len(raf) != 1 {
		t.Fatalf("%d rAF requests outstanding, want 1", len(raf))
	}
	for id, fn := range raf {
		delete(raf, id)
		fn.Invoke(ts)
	}
}

func TestSceneFrameSourceRidesTheRendererFrame(t *testing.T) {
	mount, raf := fakeScenePage(t)
	source := NewSceneFrameSource("arena")
	t.Cleanup(source.Release)
	var times []float64
	var step func(float64)
	step = func(now float64) {
		times = append(times, now)
		source.SetFrameAlpha(float64(len(times)) / 10)
		source.RequestFrame(step)
	}
	// No renderer yet: the source steps on requestAnimationFrame.
	source.RequestFrame(step)
	if source.Attached() {
		t.Fatal("attached before the renderer exists")
	}
	handle := installHandle(mount)
	fireRAF(t, raf, 10)
	// After that frame the renderer is there: the source moves onto it.
	if !source.Attached() || len(raf) != 0 || handle.Get("hook").Type() != js.TypeFunction {
		t.Fatalf("attached %v, %d rAF requests, hook %v", source.Attached(), len(raf), handle.Get("hook").Type())
	}
	hook := handle.Get("hook")
	for i, now := range []float64{26.5, 43} {
		alpha := hook.Invoke(now, 16.5)
		if alpha.Float() != float64(i+2)/10 {
			t.Fatalf("frame %d: the hook returned alpha %v, want %v", i, alpha, float64(i+2)/10)
		}
		if !handle.Get("hook").Equal(hook) || handle.Get("registrations").Int() != 1 {
			t.Fatal("the source re-registered its hook during steady frames")
		}
	}
	if len(times) != 3 || times[1] != 26.5 || times[2] != 43 || len(raf) != 0 {
		t.Fatalf("frames ran at %v with %d rAF requests: the renderer must be the only frame owner", times, len(raf))
	}
	// The mount is disposed: the waiting callback moves to rAF.
	hook.Invoke(50, 7, true)
	if source.Attached() || len(raf) != 1 {
		t.Fatalf("after dispose: attached %v, %d rAF requests", source.Attached(), len(raf))
	}
	mount.Delete("__gosxScene3DHandle")
	fireRAF(t, raf, 60)
	if len(times) != 4 || times[3] != 60 || len(raf) != 1 {
		t.Fatalf("frames %v: the fallback clock must keep the game stepping", times)
	}
}

func TestSceneFrameSourceUnregistersWhenIdle(t *testing.T) {
	mount, raf := fakeScenePage(t)
	handle := installHandle(mount)
	source := NewSceneFrameSource("arena")
	t.Cleanup(source.Release)
	ran := 0
	h := source.RequestFrame(func(float64) { ran++ })
	if !source.Attached() || len(raf) != 0 {
		t.Fatal("a ready renderer must serve the first request")
	}
	source.CancelFrame(h)
	if source.Attached() || !handle.Get("hook").IsNull() {
		t.Fatal("cancelling the last request must unregister, so the renderer can stop drawing")
	}
	source.RequestFrame(func(float64) { ran++ })
	handle.Get("hook").Invoke(5, 5)
	if ran != 1 || source.Attached() || !handle.Get("hook").IsNull() {
		t.Fatalf("ran %d, attached %v: a frame that asks for no next frame must unregister", ran, source.Attached())
	}
}
