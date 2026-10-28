//go:build js && wasm

package wasm

import (
	"encoding/json"
	"reflect"
	"sync"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/client/jsutil"
)

// pageStore is a JavaScript engine context whose shared signals behave like
// the page's: a write notifies every subscriber before it returns, and a
// subscriber may write again. thread is the context's threadMode.
func pageStore(thread string) js.Value {
	return js.Global().Call("eval", `({
  threadMode: "`+thread+`", store: new Map(), subs: new Map(), batches: 0,
  getSignal(name) { return this.store.get(name); },
  notify(name) {
    for (const cb of [...(this.subs.get(name) || [])]) cb(JSON.stringify(this.store.get(name)), name);
  },
  setSignal(name, value) { this.store.set(name, value); this.notify(name); return ""; },
  setSignalsJSON(json) {
    this.batches++;
    const values = JSON.parse(json);
    for (const [name, value] of Object.entries(values)) { this.store.set(name, value); this.notify(name); }
    return "";
  },
  // The bootstrap's context drops the answered calls of a mailbox in one step.
  trimCalls(name, through) {
    const list = this.store.get(name);
    if (!Array.isArray(list)) return false;
    const kept = list.filter(call => call && (Number(call.ID) || 0) > through);
    if (kept.length === list.length) return false;
    this.setSignal(name, kept);
    return true;
  },
  subscribeSignalJSON(name, cb, options) {
    const list = this.subs.get(name) || [];
    this.subs.set(name, list);
    list.push(cb);
    if (options && options.immediate) cb(JSON.stringify(this.store.has(name) ? this.store.get(name) : null), name);
    return () => { const i = list.indexOf(cb); if (i >= 0) list.splice(i, 1); };
  },
})`)
}

func parseJS(text string) js.Value { return js.Global().Get("JSON").Call("parse", text) }

func stringifyJS(v js.Value) string { return js.Global().Get("JSON").Call("stringify", v).String() }

// delay returns a promise that only the event loop can settle: the browser
// resolves it after ms milliseconds. It uses Promise.withResolvers and
// setTimeout, both native, so no Go function runs inside a call into
// JavaScript (new Promise with a Go executor would).
func delay(ms int) js.Value {
	pr := js.Global().Get("Promise").Call("withResolvers")
	js.Global().Call("setTimeout", pr.Get("resolve"), ms)
	return pr.Get("promise")
}

// A handler that waits for a browser promise deadlocks ServeCalls, which runs
// it inside the callback the event loop is waiting on. ServeCallsAsync runs it
// on a goroutine, so the promise settles and the reply goes out.
func TestServeCallsAsyncHandlerAwaitsABrowserPromise(t *testing.T) {
	v := pageStore("worker")
	off, err := ServeCallsAsync(Context{value: v}, "bv.cmd", CallOptions{}, func(name string, a addArgs) (sum, error) {
		if _, err := jsutil.AwaitPromise(delay(15)); err != nil {
			return sum{}, err
		}
		return sum{Total: a.A + a.B}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer off.Dispose()
	v.Call("setSignal", "$bv.cmd", parseJS(`{"ID":1,"Name":"add","Args":{"A":20,"B":22}}`))
	waitFor(t, "the reply", func() bool { return v.Get("batches").Int() == 1 })
	var r struct {
		ID    uint64
		Error string
		Value sum
	}
	if err := json.Unmarshal([]byte(stringifyJS(v.Get("store").Call("get", "$bv.cmd.result"))), &r); err != nil || r.ID != 1 || r.Value.Total != 42 || r.Error != "" {
		t.Fatalf("result = %+v (%v)", r, err)
	}
}

// Calls of one mailbox run in ID order, and the page sees the replies in that
// order, even when the first handler waits longer than the second would.
func TestServeCallsAsyncRepliesToAMailboxInOrder(t *testing.T) {
	v := pageStore("worker")
	delays := map[string]int{"slow": 40, "fast": 1}
	off, _ := ServeCallsAsync(Context{value: v}, "bv.cmd", CallOptions{}, func(name string, _ addArgs) (sum, error) {
		if _, err := jsutil.AwaitPromise(delay(delays[name])); err != nil {
			return sum{}, err
		}
		return sum{Total: delays[name]}, nil
	})
	defer off.Dispose()
	var mu sync.Mutex
	var seen []uint64
	unsub, err := Context{value: v}.SubscribeSignal("$bv.cmd.result", func(raw json.RawMessage) {
		var r struct{ ID uint64 }
		if json.Unmarshal(raw, &r) == nil {
			mu.Lock()
			seen = append(seen, r.ID)
			mu.Unlock()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer unsub.Dispose()
	v.Call("setSignal", "$bv.cmd", parseJS(`[{"ID":2,"Name":"fast"},{"ID":1,"Name":"slow"}]`))
	waitFor(t, "both replies", func() bool { mu.Lock(); defer mu.Unlock(); return len(seen) == 2 })
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(seen, []uint64{1, 2}) {
		t.Fatalf("replies reached the page in order %v, want [1 2]", seen)
	}
}

// The client and the engine on one page talk through the page's store. The
// store notifies the engine inside the client's write, and the client inside
// the engine's write, so both directions re-enter. The sync server answers
// inside the write; the async server answers from its own goroutine.
func TestCallerAndServeCallsRoundTripThroughThePageStore(t *testing.T) {
	serve := map[string]func(Context, string, CallOptions, func(string, addArgs) (sum, error)) (HandleFunc, error){
		"sync":  ServeCalls[addArgs, sum],
		"async": ServeCallsAsync[addArgs, sum],
	}
	for mode, serveCalls := range serve {
		for _, window := range []int{1, 4} {
			v := pageStore("")
			ctx := Context{value: v}
			off, err := serveCalls(ctx, "bv.cmd", CallOptions{}, adder)
			if err != nil {
				t.Fatal(err)
			}
			caller := NewCallerWith("bv.cmd", ctx.SetSignal, CallerOptions{Window: window})
			var mu sync.Mutex
			var replies []Reply
			collect := func(more []Reply) { mu.Lock(); replies = append(replies, more...); mu.Unlock() }
			sub, err := ctx.SubscribeSignal(caller.ResultSignal(), func(raw json.RawMessage) { collect(caller.Result(raw)) })
			if err != nil {
				t.Fatal(err)
			}
			for i, tag := range []string{"a", "b", "c", "boom"} {
				name := "add"
				if tag == "boom" {
					name = "boom"
				}
				more, err := caller.Call(name, addArgs{A: i, B: 10}, tag)
				if err != nil {
					t.Fatal(err)
				}
				collect(more)
			}
			waitFor(t, mode+" replies", func() bool { mu.Lock(); defer mu.Unlock(); return len(replies) == 4 })
			mu.Lock()
			if caller.Pending() != 0 {
				t.Fatalf("%s window %d: replies = %+v pending %d", mode, window, replies, caller.Pending())
			}
			for i, tag := range []string{"a", "b", "c"} {
				got, err := DecodeReply[sum](replies[i])
				if replies[i].Tag != tag || err != nil || got.Total != i+10 {
					t.Errorf("%s window %d: reply %d = %+v (%v)", mode, window, i, replies[i], err)
				}
			}
			if replies[3].Tag != "boom" || replies[3].Err != "no such thing" {
				t.Errorf("%s window %d: the error reply = %+v", mode, window, replies[3])
			}
			mu.Unlock()
			// The engine, on the main thread, dropped the answered calls from the mailbox.
			// The async server does it from the event loop right after the result, so it
			// may be a few microtasks behind the reply the client has just read.
			if window > 1 {
				waitFor(t, mode+" mailbox to empty", func() bool { return stringifyJS(v.Get("store").Call("get", "$bv.cmd")) == "[]" })
			}
			sub.Dispose()
			off.Dispose()
		}
	}
}

// A channel whose writer sends one call at a time never holds a mailbox, so the
// engine on the main thread does not read or rewrite the call signal.
func TestServeCallsLeavesASingleCallSignalAlone(t *testing.T) {
	v := pageStore("")
	reads := js.Global().Call("eval", `(function(v) { v.reads = 0; const get = v.getSignal.bind(v); v.getSignal = (name) => { v.reads++; return get(name); }; return v; })`).Invoke(v)
	off, err := ServeCalls(Context{value: reads}, "bv.cmd", CallOptions{}, adder)
	if err != nil {
		t.Fatal(err)
	}
	defer off.Dispose()
	v.Call("setSignal", "$bv.cmd", parseJS(`{"ID":1,"Name":"add","Args":{"A":1,"B":2}}`))
	if n := v.Get("reads").Int(); n != 0 {
		t.Fatalf("the call signal was read %d times", n)
	}
	if got := stringifyJS(v.Get("store").Call("get", "$bv.cmd")); got != `{"ID":1,"Name":"add","Args":{"A":1,"B":2}}` {
		t.Fatalf("the call signal was rewritten: %s", got)
	}
}

// A worker's view of the page lags it, so the engine in a worker leaves the
// mailbox to the page host; on the main thread it trims at once, and keeps the
// calls that no reply covers.
func TestServeCallsTrimsAMailboxOnTheMainThreadOnly(t *testing.T) {
	for _, thread := range []string{"", "worker"} {
		v := pageStore(thread)
		store := v.Get("store")
		store.Call("set", "$bv.cmd", parseJS(`[{"ID":1,"Name":"add"},{"ID":2,"Name":"add"},{"ID":3,"Name":"add"}]`))
		tr := contextTransport{ctx: Context{value: v}}
		if err := tr.trimCalls("$bv.cmd", 2); err != nil {
			t.Fatal(err)
		}
		want := `[{"ID":1,"Name":"add"},{"ID":2,"Name":"add"},{"ID":3,"Name":"add"}]`
		if thread == "" {
			want = `[{"ID":3,"Name":"add"}]`
		}
		if got := stringifyJS(store.Call("get", "$bv.cmd")); got != want {
			t.Errorf("thread %q: mailbox = %s, want %s", thread, got, want)
		}
		// An entry with no usable ID can never be answered, so it leaves too.
		store.Call("set", "$bv.cmd", parseJS(`[null,{"ID":"x"},7,{"ID":4,"Name":"add"}]`))
		if err := tr.trimCalls("$bv.cmd", 2); err != nil {
			t.Fatal(err)
		}
		want = `[null,{"ID":"x"},7,{"ID":4,"Name":"add"}]`
		if thread == "" {
			want = `[{"ID":4,"Name":"add"}]`
		}
		if got := stringifyJS(store.Call("get", "$bv.cmd")); got != want {
			t.Errorf("thread %q: mailbox with unusable entries = %s, want %s", thread, got, want)
		}
		// A single call is not a mailbox.
		store.Call("set", "$bv.cmd", parseJS(`{"ID":1,"Name":"add"}`))
		if err := tr.trimCalls("$bv.cmd", 5); err != nil || stringifyJS(store.Call("get", "$bv.cmd")) != `{"ID":1,"Name":"add"}` {
			t.Errorf("thread %q: a single call was changed", thread)
		}
	}
}

// installEntryProbe counts the entries from JavaScript into Go that start while
// another entry is still on the stack (a nested entry), the way the
// nested-callbacks work counts them: it wraps the Go runtime's _resume. Go keeps
// the entries in progress on one stack and assumes that entries from different
// goroutines never interleave, so a goroutine that makes a call into JavaScript
// which runs a Go callback before it returns (a write that notifies this
// engine's own subscriptions) must not be one of several.
func installEntryProbe() {
	js.Global().Call("eval", `(() => {
  const Go = globalThis.Go;
  if (Go.prototype.__entryProbe) return;
  Go.prototype.__entryProbe = true;
  const original = Go.prototype._resume;
  globalThis.__entries = { depth: 0, entries: 0, nested: 0 };
  Go.prototype._resume = function () {
    const p = globalThis.__entries;
    p.depth++; p.entries++; if (p.depth > 1) p.nested++;
    try { return original.apply(this, arguments); } finally { p.depth--; }
  };
})()`)
}

func nestedEntries() int { return js.Global().Get("__entries").Get("nested").Int() }

// An island writes its calls from the event loop, one every few milliseconds.
const driveCalls = `(function(ctx, signal, calls, mailbox) {
  let id = 0;
  const tick = () => {
    id++;
    const call = { ID: id, Name: "add", Args: { A: id, B: 1 } };
    if (mailbox) {
      const list = Array.isArray(ctx.getSignal(signal)) ? ctx.getSignal(signal).slice() : [];
      list.push(call);
      ctx.setSignal(signal, list);
    } else {
      ctx.setSignal(signal, call);
    }
    if (id < calls) setTimeout(tick, 3);
  };
  setTimeout(tick, 0);
})`

// The page store tells the engine's own subscriptions inside every write, so a
// goroutine that writes a result enters Go again before the write returns. The
// handler of ServeCallsAsync answers from a goroutine, and with several channels
// several goroutines do, so none of them may start a nested entry: the engine
// has the browser write from the event loop, where no call from Go is pending.
func TestServeCallsAsyncMakesNoNestedEntriesIntoGo(t *testing.T) {
	installEntryProbe()
	for _, mailbox := range []bool{false, true} {
		v := pageStore("")
		ctx := Context{value: v}
		off, err := ServeCallsAsync(ctx, "bv.cmd", CallOptions{}, adder)
		if err != nil {
			t.Fatal(err)
		}
		before := nestedEntries()
		js.Global().Call("eval", driveCalls).Invoke(v, "$bv.cmd", 10, mailbox)
		waitFor(t, "ten results", func() bool { return v.Get("batches").Int() >= 10 })
		if got := nestedEntries() - before; got != 0 {
			t.Errorf("mailbox=%v: %d nested entries into Go while the async server answered 10 calls, want 0", mailbox, got)
		}
		var r struct {
			ID    uint64
			Value sum
		}
		if err := json.Unmarshal([]byte(stringifyJS(v.Get("store").Call("get", "$bv.cmd.result"))), &r); err != nil || r.ID != 10 || r.Value.Total != 11 {
			t.Errorf("mailbox=%v: last result = %+v (%v)", mailbox, r, err)
		}
		if mailbox {
			waitFor(t, "an empty mailbox", func() bool { return stringifyJS(v.Get("store").Call("get", "$bv.cmd")) == "[]" })
		}
		off.Dispose()
	}
}
