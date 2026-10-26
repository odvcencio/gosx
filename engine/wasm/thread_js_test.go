//go:build js && wasm

package wasm

import (
	"context"
	"testing"

	"syscall/js"

	"m31labs.dev/gosx/engine"
)

func installWorkerBridge(t *testing.T) js.Value {
	t.Helper()
	global := js.Global()
	previous := global.Get("__gosx")
	bridge := global.Call("eval", `({
	  hidden: false, posted: [], bytes: [], rate: 60, handlers: new Set(), visibility: new Set(),
	  post(name, json) { this.posted.push([name, json]); },
	  postBytes(name, u8) { this.bytes.push([name, Array.from(u8)]); },
	  onMessage(cb) { this.handlers.add(cb); return () => this.handlers.delete(cb); },
	  onVisibility(cb) { this.visibility.add(cb); return () => this.visibility.delete(cb); },
	  setFrameRate(hz) { if (hz < 1 || hz > 1000) return false; this.rate = hz; return true; },
	  frameStats() { return { hz: this.rate, frames: 12, lateMaxMs: 3.5, gapMaxMs: 20, gapAvgMs: 16.7, skippedSlots: 1 }; },
	  now() { return 42.5; },
	})`)
	root := global.Get("Object").New()
	root.Set("worker", bridge)
	global.Set("__gosx", root)
	t.Cleanup(func() {
		if previous.IsUndefined() {
			global.Delete("__gosx")
		} else {
			global.Set("__gosx", previous)
		}
	})
	return bridge
}

func TestThreadModeAndFallbackReason(t *testing.T) {
	worker := Context{value: js.Global().Call("eval", `({threadMode: "worker"})`)}
	if worker.Thread() != engine.ThreadWorker {
		t.Fatal("worker context must report ThreadWorker")
	}
	fellBack := Context{value: js.Global().Call("eval", `({threadMode: "main", threadFallbackReason: "worker-create-failed"})`)}
	if fellBack.Thread() != engine.ThreadMain || fellBack.ThreadFallbackReason() != "worker-create-failed" {
		t.Fatalf("fallback context reported %q / %q", fellBack.Thread(), fellBack.ThreadFallbackReason())
	}
	plain := Context{value: js.Global().Call("eval", `({})`)}
	if plain.Thread() != engine.ThreadMain || plain.ThreadFallbackReason() != "" {
		t.Fatal("a context with no thread fields is a main-thread engine")
	}
}

func TestPortExistsOnlyInAWorkerEngine(t *testing.T) {
	installWorkerBridge(t)
	mainCtx := Context{value: js.Global().Call("eval", `({threadMode: "main"})`)}
	if _, ok := mainCtx.Port(); ok {
		t.Fatal("a main-thread engine must not get a port, even when a bridge exists")
	}
	workerCtx := Context{value: js.Global().Call("eval", `({threadMode: "worker"})`)}
	if _, ok := workerCtx.Port(); !ok {
		t.Fatal("a worker engine must get a port")
	}
}

func TestPortPostsJSONAndTransfersBytes(t *testing.T) {
	bridge := installWorkerBridge(t)
	port := Port{value: bridge}
	if err := port.Post("hud", map[string]int{"hp": 9}); err != nil {
		t.Fatal(err)
	}
	if got := bridge.Get("posted").Index(0); got.Index(0).String() != "hud" || got.Index(1).String() != `{"hp":9}` {
		t.Fatalf("posted %v", got)
	}
	if err := port.Post("bad", make(chan int)); err == nil {
		t.Fatal("a value that is not JSON must fail before it reaches the page")
	}
	data := []byte{1, 2, 3}
	if err := port.PostBytes("blob", data); err != nil {
		t.Fatal(err)
	}
	data[0] = 99 // the page must have its own copy
	sent := bridge.Get("bytes").Index(0)
	if sent.Index(0).String() != "blob" || sent.Index(1).Length() != 3 || sent.Index(1).Index(0).Int() != 1 {
		t.Fatalf("bytes %v", sent)
	}
}

func TestPortDeliversMessagesAndStopsAfterDispose(t *testing.T) {
	bridge := installWorkerBridge(t)
	port := Port{value: bridge}
	var got []Message
	off := port.OnMessage(func(m Message) { got = append(got, m) })
	deliver := func(args ...any) {
		bridge.Get("handlers").Call("forEach", js.FuncOf(func(_ js.Value, a []js.Value) any { a[0].Invoke(args...); return nil }))
	}
	deliver("pause", `{"on":true}`)
	bytes := js.Global().Get("Uint8Array").New(2)
	bytes.SetIndex(0, 7)
	bytes.SetIndex(1, 8)
	deliver("blob", js.Undefined(), bytes)
	if len(got) != 2 {
		t.Fatalf("got %d messages", len(got))
	}
	if got[0].Name != "pause" || string(got[0].JSON) != `{"on":true}` || got[0].Bytes != nil {
		t.Fatalf("first message %+v", got[0])
	}
	if got[1].Name != "blob" || got[1].JSON != nil || len(got[1].Bytes) != 2 || got[1].Bytes[1] != 8 {
		t.Fatalf("second message %+v", got[1])
	}
	off.Dispose()
	off.Dispose()
	if bridge.Get("handlers").Get("size").Int() != 0 {
		t.Fatal("dispose must unsubscribe")
	}
}

func TestPortVisibilityRateAndStats(t *testing.T) {
	bridge := installWorkerBridge(t)
	port := Port{value: bridge}
	if port.Hidden() {
		t.Fatal("visible page reported hidden")
	}
	bridge.Set("hidden", true)
	if !port.Hidden() {
		t.Fatal("hidden page reported visible")
	}
	var seen []bool
	off := port.OnVisibility(func(h bool) { seen = append(seen, h) })
	bridge.Get("visibility").Call("forEach", js.FuncOf(func(_ js.Value, a []js.Value) any { a[0].Invoke(false); return nil }))
	if len(seen) != 1 || seen[0] {
		t.Fatalf("visibility callbacks %v", seen)
	}
	off.Dispose()
	if err := port.SetFrameRate(120); err != nil || bridge.Get("rate").Int() != 120 {
		t.Fatalf("SetFrameRate: %v rate=%v", err, bridge.Get("rate"))
	}
	if err := port.SetFrameRate(0); err == nil {
		t.Fatal("a zero rate must fail")
	}
	if err := port.SetFrameRate(5000); err == nil {
		t.Fatal("a rate above 1000 Hz must fail")
	}
	stats := port.FrameStats()
	if stats.Frames != 12 || stats.LateMaxMS != 3.5 || stats.SkippedSlots != 1 {
		t.Fatalf("stats %+v", stats)
	}
	if port.Now() != 42.5 {
		t.Fatal("Now")
	}
}

func TestScene3DByIDPassesTheIDAsTheMountTarget(t *testing.T) {
	host := js.Global().Call("eval", `({current:true, isCurrent(){return true}, scene3D(method, target, ...args){ this.method = method; this.target = target; return Promise.resolve(); }})`)
	ctx := Context{value: host}
	if err := ctx.Scene3DByID("arena").Dispatch(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if host.Get("method").String() != "dispatchCommands" || host.Get("target").Type() != js.TypeString || host.Get("target").String() != "arena" {
		t.Fatalf("method %v target %v", host.Get("method"), host.Get("target"))
	}
}
