//go:build js && wasm

package wasm

import (
	"encoding/json"
	"testing"
	"time"

	"syscall/js"
)

// callCtx is a JavaScript engine context with an in-memory signal store, the
// subscribe options the real contexts honor, and optional worker-only methods.
func callCtx(t *testing.T, worker bool) js.Value {
	t.Helper()
	extra := ""
	if worker {
		extra = `
  declareCalls(channel, timeout, recent, zero) { this.declared = { channel, timeout, recent, zero }; },
  sendView(json, changedOnly, token, cb) { this.views.push({ json, changedOnly, token, cb }); },`
	}
	return js.Global().Call("eval", `({
  store: new Map(), subs: new Map(), views: [], batches: [], declared: null,
  subscribeSignalJSON(name, cb, options) {
    this.subOptions = options;
    this.subs.set(name, cb);
    if (options && options.immediate) cb(JSON.stringify(this.store.has(name) ? this.store.get(name) : null), name);
    return () => { this.subs.delete(name); this.unsubscribed = true; };
  },
  setSignalsJSON(json, changedOnly) {
    const values = JSON.parse(json);
    this.batches.push({ values, changedOnly });
    for (const [k, v] of Object.entries(values)) this.store.set(k, v);
    return "";
  },
  deliver(name, value) { this.store.set(name, value); const cb = this.subs.get(name); if (cb) cb(JSON.stringify(value), name); },`+extra+`
})`)
}

func TestServeCallsOnAContextDeclaresReplaysAndReplies(t *testing.T) {
	v := callCtx(t, true)
	v.Get("store").Call("set", "$bv.cmd", js.Global().Get("JSON").Call("parse", `{"ID":2,"Name":"add","Args":{"A":1,"B":2}}`))
	ctx := Context{value: v}
	off, err := ServeCalls(ctx, "bv.cmd", CallOptions{Timeout: 2 * time.Second, Recent: 4}, adder)
	if err != nil {
		t.Fatal(err)
	}
	d := v.Get("declared")
	if d.Get("channel").String() != "$bv.cmd" || d.Get("timeout").Int() != 2000 || d.Get("recent").Int() != 4 {
		t.Fatalf("declared = %s", js.Global().Get("JSON").Call("stringify", d).String())
	}
	if !v.Get("subOptions").Get("immediate").Bool() {
		t.Fatal("ServeCalls must replay the current value")
	}
	if v.Get("batches").Length() != 1 {
		t.Fatalf("the call written before the subscription was not answered: %d batches", v.Get("batches").Length())
	}
	raw := js.Global().Get("JSON").Call("stringify", v.Get("batches").Index(0).Get("values").Get("$bv.cmd.result")).String()
	var result struct {
		ID    uint64
		Error string
		Value sum
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil || result.ID != 2 || result.Value.Total != 3 {
		t.Fatalf("result = %s (%v)", raw, err)
	}
	v.Call("deliver", "$bv.cmd", js.Global().Get("JSON").Call("parse", `{"ID":3,"Name":"add","Args":{"A":5,"B":5}}`))
	if v.Get("batches").Length() != 2 {
		t.Fatal("a later call was not answered")
	}
	off.Dispose()
	if !v.Get("unsubscribed").Bool() {
		t.Fatal("dispose must unsubscribe")
	}
}

func TestServeCallsWorksOnAMainThreadContextWithoutDeclareCalls(t *testing.T) {
	v := callCtx(t, false)
	if _, err := ServeCalls(Context{value: v}, "$c", CallOptions{}, adder); err != nil {
		t.Fatal(err)
	}
	v.Call("deliver", "$c", js.Global().Get("JSON").Call("parse", `{"ID":1,"Name":"add","Args":{"A":1,"B":1}}`))
	if v.Get("batches").Length() != 1 {
		t.Fatal("a main-thread engine must answer calls too")
	}
}

func TestViewOnAWorkerContextWaitsForTheAckThenSendsNewest(t *testing.T) {
	v := callCtx(t, true)
	view := NewView(Context{value: v}, ViewOptions{MinInterval: time.Millisecond})
	defer view.Dispose()
	_ = view.Set(map[string]any{"$bv.view": map[string]int{"N": 1}})
	if v.Get("views").Length() != 1 {
		t.Fatal("the first batch goes at once")
	}
	_ = view.Set(map[string]any{"$bv.view": map[string]int{"N": 2}})
	_ = view.Set(map[string]any{"$bv.view": map[string]int{"N": 3}})
	time.Sleep(20 * time.Millisecond)
	if v.Get("views").Length() != 1 {
		t.Fatal("a second batch went out before the page acknowledged the first")
	}
	first := v.Get("views").Index(0)
	if !first.Get("changedOnly").Bool() {
		t.Fatal("view batches must be changed-only")
	}
	first.Get("cb").Invoke(first.Get("token"))
	deadline := time.Now().Add(2 * time.Second)
	for v.Get("views").Length() < 2 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if v.Get("views").Length() != 2 {
		t.Fatal("the acknowledgement did not release the next batch")
	}
	if got := v.Get("views").Index(1).Get("json").String(); got != `{"$bv.view":{"N":3}}` {
		t.Fatalf("second batch = %s, want the newest value", got)
	}
}

func TestViewOnAMainThreadContextAppliesAtOnce(t *testing.T) {
	v := callCtx(t, false)
	view := NewView(Context{value: v}, ViewOptions{MinInterval: time.Millisecond})
	defer view.Dispose()
	_ = view.Set(map[string]any{"$bv.view": 1})
	if v.Get("batches").Length() != 1 || !v.Get("batches").Index(0).Get("changedOnly").Bool() {
		t.Fatal("main-thread view batches apply through setSignalsJSON, changed-only")
	}
	_ = view.Set(map[string]any{"$bv.view": 2})
	deadline := time.Now().Add(2 * time.Second)
	for v.Get("batches").Length() < 2 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if v.Get("batches").Length() != 2 {
		t.Fatal("a main-thread view must not wait for an acknowledgement")
	}
}
