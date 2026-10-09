//go:build js && wasm

package wasm

import (
	"context"
	"errors"
	"syscall/js"
	"testing"
	"time"
)

func playbackFrames(t *testing.T) js.Value {
	t.Helper()
	host := js.Global().Call("eval", `(()=>{
 const old=[globalThis.document,globalThis.requestAnimationFrame,globalThis.cancelAnimationFrame];
 let next=0;const frames=new Map(),listeners=new Set();
 globalThis.document={hidden:false,addEventListener(n,f){listeners.add(f)},removeEventListener(n,f){listeners.delete(f)}};
 globalThis.requestAnimationFrame=f=>{frames.set(++next,f);return next};
 globalThis.cancelAnimationFrame=id=>frames.delete(id);
 return {pending:()=>frames.size,listeners:()=>listeners.size,
 step(at){const queue=[...frames.values()];frames.clear();for(const f of queue)f(at)},
 hidden(value){document.hidden=value;for(const f of [...listeners])f()},
 restore(){[globalThis.document,globalThis.requestAnimationFrame,globalThis.cancelAnimationFrame]=old}};
 })()`)
	t.Cleanup(func() { host.Call("restore") })
	return host
}
func awaitPlayback(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("playback did not reach expected state")
		}
		time.Sleep(time.Millisecond)
	}
}
func TestFinitePlaybackCompletionHiddenAndCancel(t *testing.T) {
	host := playbackFrames(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var samples []float64
	finals := 0
	done := make(chan error, 1)
	go func() {
		done <- playFrames(ctx, FramePlaybackOptions{Duration: .1}, func(at float64, final bool) error {
			samples = append(samples, at)
			if final {
				finals++
			}
			return nil
		})
	}()
	step := func(at float64) {
		awaitPlayback(t, func() bool { return host.Call("pending").Int() == 1 })
		host.Call("step", at)
	}
	step(0)
	step(50)
	awaitPlayback(t, func() bool { return len(samples) == 2 && host.Call("pending").Int() == 1 })
	host.Call("hidden", true)
	awaitPlayback(t, func() bool { return host.Call("pending").Int() == 0 })
	host.Call("step", 1000)
	if len(samples) != 2 {
		t.Fatal("hidden playback advanced")
	}
	host.Call("hidden", false)
	step(1000)
	step(1050)
	awaitPlayback(t, func() bool { return finals == 1 })
	select {
	case <-done:
		t.Fatal("completion preceded final visible frame")
	default:
	}
	step(1066)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if samples[len(samples)-1] != .1 || host.Call("pending").Int() != 0 || host.Call("listeners").Int() != 0 {
		t.Fatal("playback did not settle and release", samples)
	}
	samples = nil
	finals = 0
	canceled, stop := context.WithCancel(context.Background())
	go func() {
		done <- playFrames(canceled, FramePlaybackOptions{Duration: 1}, func(at float64, final bool) error { samples = append(samples, at); return nil })
	}()
	awaitPlayback(t, func() bool { return host.Call("pending").Int() == 1 })
	stop()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation not returned", err)
	}
	host.Call("step", 5000)
	if len(samples) != 0 || host.Call("pending").Int() != 0 || host.Call("listeners").Int() != 0 {
		t.Fatal("canceled playback retained callbacks")
	}
}
