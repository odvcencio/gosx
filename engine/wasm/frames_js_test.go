//go:build js && wasm

package wasm

import (
	"context"
	"errors"
	"syscall/js"
	"testing"
	"time"

	"m31labs.dev/gosx/scene"
)

// pageHost fakes the browser pieces a page-side loop meets: a macrotask-based
// requestAnimationFrame (as in a browser, a callback runs in its own task) and a
// probe that counts the tasks the browser got to run. scene3D resolves at once,
// as the command bridge does for a mounted scene.
func pageHost(t *testing.T) js.Value {
	t.Helper()
	host := js.Global().Call("eval", `(()=>{
 const old=[globalThis.document,globalThis.requestAnimationFrame,globalThis.cancelAnimationFrame];
 let ticks=0,frames=0,stopped=false,next=0;const timers=new Map(),listeners=new Set();
 const tick=()=>{if(!stopped){ticks++;setTimeout(tick,0)}};setTimeout(tick,0);
 globalThis.document={hidden:false,addEventListener(n,f){listeners.add(f)},removeEventListener(n,f){listeners.delete(f)}};
 globalThis.requestAnimationFrame=f=>{const id=++next;timers.set(id,setTimeout(()=>{timers.delete(id);frames++;f(performance.now())},4));return id};
 globalThis.cancelAnimationFrame=id=>{clearTimeout(timers.get(id));timers.delete(id)};
 return {current:true,isCurrent(){return true},dispatched:0,
  scene3D(method,mount,...args){this.dispatched++;return Promise.resolve()},
  ticks:()=>ticks,frames:()=>frames,pending:()=>timers.size,
  hidden(v){document.hidden=v;for(const f of [...listeners])f()},
  restore(){stopped=true;[globalThis.document,globalThis.requestAnimationFrame,globalThis.cancelAnimationFrame]=old}};
 })()`)
	t.Cleanup(func() { host.Call("restore") })
	return host
}

func pageCamera() scene.PerspectiveCamera {
	return scene.PerspectiveCamera{Position: scene.Vec3(0, 5, 20), FOV: 50}
}

// A loop that waits only on Scene3D results never returns to the browser:
// every promise is already resolved, so Go resumes inside the browser's
// microtask queue. This is why the page froze, and it is the reason the loop
// below needs EachFrame.
func TestSceneCallLoopWithoutAFrameWaitStarvesThePage(t *testing.T) {
	host := pageHost(t)
	target := Context{value: host}.Scene3DByID("scene")
	time.Sleep(10 * time.Millisecond) // let the probe start
	ticks, frames := host.Call("ticks").Int(), host.Call("frames").Int()
	started := time.Now()
	for i := 0; i < 3000 || time.Since(started) < 30*time.Millisecond; i++ {
		if err := target.SetCamera(context.Background(), pageCamera()); err != nil {
			t.Fatal(err)
		}
	}
	if got := host.Call("ticks").Int() - ticks; got != 0 {
		t.Fatalf("the browser ran %d tasks during a loop that never waits for a frame; the loop is not starving it", got)
	}
	if host.Call("frames").Int() != frames {
		t.Fatal("an animation frame ran during the loop")
	}
}

func TestEachFrameLoopKeepsThePageResponsive(t *testing.T) {
	host := pageHost(t)
	target := Context{value: host}.Scene3DByID("scene")
	time.Sleep(10 * time.Millisecond)
	ticks0 := host.Call("ticks").Int()
	var frames []Frame
	var ticksAtFrame []int
	err := EachFrame(context.Background(), func(f Frame) error {
		frames = append(frames, f)
		ticksAtFrame = append(ticksAtFrame, host.Call("ticks").Int())
		if err := target.SetCamera(context.Background(), pageCamera()); err != nil {
			return err
		}
		if len(frames) == 20 {
			return StopFrames
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 20 || host.Get("dispatched").Int() != 20 {
		t.Fatalf("frames=%d dispatched=%d", len(frames), host.Get("dispatched").Int())
	}
	if frames[0].Delta != 0 {
		t.Fatalf("first delta = %v", frames[0].Delta)
	}
	for i := 1; i < len(frames); i++ {
		if frames[i].Time <= frames[i-1].Time || frames[i].Delta <= 0 || ticksAtFrame[i] <= ticksAtFrame[i-1] {
			t.Fatalf("frame %d: time %v delta %v; the browser ran no task between frames (%d -> %d)", i, frames[i].Time, frames[i].Delta, ticksAtFrame[i-1], ticksAtFrame[i])
		}
	}
	if host.Call("ticks").Int()-ticks0 < 20 {
		t.Fatal("the page's own tasks did not keep running during the loop")
	}
	if host.Call("pending").Int() != 0 {
		t.Fatal("a frame request outlived the loop")
	}
}

func TestEachFrameStopsOnCancelErrorAndHiddenPage(t *testing.T) {
	host := pageHost(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boom := errors.New("boom")
	if err := EachFrame(context.Background(), func(Frame) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("step error = %v", err)
	}
	if err := EachFrame(context.Background(), nil); err == nil {
		t.Fatal("nil step accepted")
	}
	steps := 0
	done := make(chan error, 1)
	go func() {
		done <- EachFrame(ctx, func(Frame) error { steps++; return nil })
	}()
	deadline := time.Now().Add(time.Second)
	for steps < 3 {
		if time.Now().After(deadline) {
			t.Fatal("loop did not run")
		}
		time.Sleep(time.Millisecond)
	}
	host.Call("hidden", true)
	time.Sleep(20 * time.Millisecond)
	atHide := steps
	time.Sleep(40 * time.Millisecond)
	if steps > atHide+1 || host.Call("pending").Int() != 0 {
		t.Fatalf("a hidden page kept stepping or keeps a frame request: steps %d -> %d pending %d", atHide, steps, host.Call("pending").Int())
	}
	host.Call("hidden", false)
	time.Sleep(40 * time.Millisecond)
	if steps <= atHide {
		t.Fatal("loop did not resume when the page was shown")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	if host.Call("pending").Int() != 0 {
		t.Fatal("cancel left a frame request")
	}
}
