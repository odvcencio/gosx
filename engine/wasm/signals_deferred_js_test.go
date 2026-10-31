//go:build js && wasm

package wasm

import (
	"encoding/json"
	"strings"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/internal/browserdom"
	"m31labs.dev/gosx/internal/jsreentry"
)

// signalHost returns a JavaScript engine context whose signal store behaves as
// the page's does: setSignal and setSignalsJSON notify every subscriber before
// they return, a batch is stored completely before the first notification, and
// subscribeSignalJSON with {immediate: true} delivers the current value (JSON
// null when there is none) before it returns. host.watch adds a page-script
// subscriber, host.log collects what the page scripts and the Go subscribers
// saw, and host.listeners is the EventTarget that emit dispatches on. It is
// plain JavaScript, because a fixture written as a Go function would itself be
// a call from JavaScript into Go.
func signalHost() js.Value {
	return js.Global().Get("Function").New(`return (function () {
		const store = new Map();
		const subscribers = new Map();
		const host = {
			log: [],
			listeners: new EventTarget(),
			getSignal(name) { return store.get(name); },
			setSignal(name, value) {
				store.set(name, value);
				notify(name, JSON.stringify(value));
				return "";
			},
			setSignalsJSON(json, changedOnly) {
				const values = JSON.parse(json);
				const names = Object.keys(values);
				for (const name of names) store.set(name, values[name]);
				for (const name of names) notify(name, JSON.stringify(values[name]));
				return "";
			},
			subscribeSignalJSON(name, callback, options) {
				const entry = { callback, active: true };
				if (!subscribers.has(name)) subscribers.set(name, []);
				subscribers.get(name).push(entry);
				if (options && options.immediate) callback(JSON.stringify(store.has(name) ? store.get(name) : null), name);
				return function () { entry.active = false; };
			},
			watch(name, label) {
				host.subscribeSignalJSON(name, function (json) { host.log.push(label + ":" + json); });
			},
			listen(name, label) {
				host.listeners.addEventListener("gosx:engine:" + name, function () { host.log.push(label); });
			},
			emit(name, detail) {
				host.listeners.dispatchEvent(new CustomEvent("gosx:engine:" + name, { detail }));
			},
		};
		function notify(name, json) {
			for (const entry of subscribers.get(name) || []) if (entry.active) entry.callback(json, name);
		}
		return host;
	})()`).Invoke()
}

func hostLog(host js.Value) string {
	log := host.Get("log")
	words := make([]string, log.Length())
	for i := range words {
		words[i] = log.Index(i).String()
	}
	return strings.Join(words, " ")
}

// A write to the shared signals makes the page notify its subscribers. A Go
// subscriber must not run inside that call: Go's js/wasm runtime can stop the
// program when JavaScript enters Go inside a call that Go made (see package
// m31labs.dev/gosx/client/jsutil). It runs once the page has notified its other
// subscribers, before the write returns, and it sees the whole batch.
func TestSharedSignalWritesNeverRunGoSubscribersInsideTheCall(t *testing.T) {
	probe := jsreentry.Install(t)
	host := signalHost()
	ctx := Context{value: host}
	host.Call("watch", "$a", "js-before")
	var seenB string
	dispose, err := ctx.SubscribeSignal("$a", func(raw json.RawMessage) {
		b, _ := ctx.SignalJSON("$b")
		seenB = string(b)
		host.Get("log").Call("push", "go:"+string(raw))
	})
	if err != nil {
		t.Fatal(err)
	}
	defer dispose.Dispose()
	host.Call("watch", "$a", "js-after")
	probe.Reset()

	writes := []struct {
		name  string
		write func() error
		want  string // what the page's subscribers and the Go subscriber logged
		wantB string // the value of $b that the Go subscriber read
	}{
		{"SetSignal", func() error { return ctx.SetSignal("$a", 1) }, "js-before:1 js-after:1 go:1", ""},
		{"SetSignals", func() error { return ctx.SetSignals(map[string]any{"$a": 2, "$b": "batch"}) }, "js-before:2 js-after:2 go:2", `"batch"`},
		{"SetChangedSignals", func() error { return ctx.SetChangedSignals(map[string]any{"$a": 3, "$b": "snapshot"}) }, "js-before:3 js-after:3 go:3", `"snapshot"`},
	}
	for _, w := range writes {
		host.Get("log").Set("length", 0)
		seenB = ""
		if err := w.write(); err != nil {
			t.Fatalf("%s: %v", w.name, err)
		}
		probe.AssertNone(t, "%s", w.name)
		if got := hostLog(host); got != w.want {
			t.Fatalf("%s: the Go subscriber must have run when the write returns, after the page's subscribers: got %q, want %q", w.name, got, w.want)
		}
		// The subscriber saw the whole batch: $b already held its new value.
		if seenB != w.wantB {
			t.Fatalf("%s: the Go subscriber read $b = %q, want %q (a batch is complete before anyone is notified)", w.name, seenB, w.wantB)
		}
	}
}

// The replay that ServeCalls asks for delivers the current value before the
// subscription call returns, so a call written before the engine subscribed is
// answered. The page makes that delivery inside the call; Go runs it after the
// call, and before subscribeSignalReplay returns.
func TestSubscribeSignalReplayDeliversBeforeItReturnsWithoutNesting(t *testing.T) {
	probe := jsreentry.Install(t)
	host := signalHost()
	ctx := Context{value: host}
	if err := ctx.SetSignal("$present", map[string]int{"n": 7}); err != nil {
		t.Fatal(err)
	}
	probe.Reset()

	var got []string
	dispose, err := ctx.subscribeSignalReplay("$present", func(raw json.RawMessage) { got = append(got, string(raw)) })
	if err != nil {
		t.Fatal(err)
	}
	defer dispose.Dispose()
	if strings.Join(got, " ") != `{"n":7}` {
		t.Fatalf("the current value was not delivered before subscribeSignalReplay returned: %q", got)
	}
	absent, err := ctx.subscribeSignalReplay("$absent", func(raw json.RawMessage) { got = append(got, string(raw)) })
	if err != nil {
		t.Fatal(err)
	}
	defer absent.Dispose()
	if strings.Join(got, " ") != `{"n":7} null` {
		t.Fatalf("a signal with no value must replay JSON null: %q", got)
	}
	// A write after the replay reaches the subscriber, and only once.
	if err := ctx.SetSignal("$present", map[string]int{"n": 8}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != `{"n":7} null {"n":8}` {
		t.Fatalf("later writes: %q", got)
	}
	probe.AssertNone(t, "the replay and later writes")
}

// A Go subscriber that writes another signal while it runs sees that write's
// subscribers finish before its own write returns, as it did when subscribers
// ran inside the call.
func TestAGoSubscriberThatWritesASignalSeesItsSubscribersFinishFirst(t *testing.T) {
	probe := jsreentry.Install(t)
	host := signalHost()
	ctx := Context{value: host}
	host.Call("watch", "$out", "page")
	var order []string
	downstream, err := ctx.SubscribeSignal("$out", func(raw json.RawMessage) { order = append(order, "out="+string(raw)) })
	if err != nil {
		t.Fatal(err)
	}
	defer downstream.Dispose()
	relay, err := ctx.SubscribeSignal("$in", func(raw json.RawMessage) {
		order = append(order, "in="+string(raw)+":start")
		if err := ctx.SetSignal("$out", 2); err != nil {
			t.Error(err)
		}
		order = append(order, "in:end")
	})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Dispose()
	probe.Reset()
	if err := ctx.SetSignal("$in", 1); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(order, " "), "in=1:start out=2 in:end"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	probe.AssertNone(t, "a subscriber that writes")
}

// Context.Emit dispatches a CustomEvent on the page's document. A Go listener on
// that event must not run inside the call either.
func TestEmitNeverRunsGoListenersInsideTheCall(t *testing.T) {
	probe := jsreentry.Install(t)
	host := signalHost()
	ctx := Context{value: host}
	host.Call("listen", "ready", "js-before")
	var revision uint64
	goListener := browserdom.Listen(host.Get("listeners"), "gosx:engine:ready", func(e browserdom.Event) {
		host.Get("log").Call("push", "go")
		revision = e.DetailRevision()
	})
	defer goListener.Dispose()
	host.Call("listen", "ready", "js-after")
	probe.Reset()
	if err := ctx.Emit("ready", map[string]any{"revision": 2}); err != nil {
		t.Fatal(err)
	}
	probe.AssertNone(t, "Emit")
	if got := hostLog(host); got != "js-before js-after go" {
		t.Fatalf("the Go listener must have run when Emit returns, after the page's listeners: %q", got)
	}
	if revision != 2 {
		t.Fatalf("the Go listener saw detail revision %d, want 2", revision)
	}
}

// On the main thread a view applies its batch with setSignalsJSON, which
// notifies subscribers like any other write.
func TestMainThreadViewBatchesNeverRunGoSubscribersInsideTheCall(t *testing.T) {
	probe := jsreentry.Install(t)
	host := signalHost()
	ctx := Context{value: host}
	ran := 0
	dispose, err := ctx.SubscribeSignal("$view", func(json.RawMessage) { ran++ })
	if err != nil {
		t.Fatal(err)
	}
	defer dispose.Dispose()
	probe.Reset()
	sink := ctx.viewSink()
	defer sink.release()
	applied := false
	if err := sink.send([]byte(`{"$view":{"turn":1}}`), func(ok bool) { applied = ok && ran == 1 }); err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatalf("the view reported the batch applied before its Go subscriber ran (ran=%d)", ran)
	}
	probe.AssertNone(t, "a view batch")
}
