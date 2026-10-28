//go:build js && wasm

package jsutil

import (
	"context"
	"errors"
	"syscall/js"
	"testing"
	"time"

	"m31labs.dev/gosx/internal/jsreentry"
)

// documentLifecycle installs a fake __gosx.host.lifecycle that is plain
// JavaScript, so that calling it never runs Go. Its documentSignal returns the
// signal of state.controller, and state.live counts the abort listeners that
// are added and not yet removed. state.renew starts a new document scope.
func documentLifecycle(t *testing.T) js.Value {
	t.Helper()
	previous := js.Global().Get("__gosx")
	t.Cleanup(func() { js.Global().Set("__gosx", previous) })
	state := native(`(function () {
		const state = { controller: null, live: new Set() };
		state.renew = function () {
			const controller = new AbortController();
			const signal = controller.signal;
			const add = signal.addEventListener.bind(signal);
			const remove = signal.removeEventListener.bind(signal);
			signal.addEventListener = function (type, listener, options) {
				if (type === "abort") state.live.add(listener);
				return add(type, listener, options);
			};
			signal.removeEventListener = function (type, listener, options) {
				if (type === "abort") state.live.delete(listener);
				return remove(type, listener, options);
			};
			state.controller = controller;
		};
		state.renew();
		globalThis.__gosx = { host: { lifecycle: { documentSignal() { return state.controller.signal; } } } };
		return state;
	})()`)
	return state
}

// waitUntil polls until condition is true or five seconds pass.
func waitUntil(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// awaitCanceled waits for a context that the document scope cancels from a
// goroutine, a few microtasks after the abort.
func awaitCanceled(t *testing.T, ctx context.Context, what string) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatalf("%s", what)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("%s: err = %v", what, ctx.Err())
	}
}

func TestDocumentContextCancelsCurrentAndLateRequests(t *testing.T) {
	previous := js.Global().Get("__gosx")
	defer js.Global().Set("__gosx", previous)
	controller := js.Global().Get("AbortController").New()
	get := js.FuncOf(func(js.Value, []js.Value) any { return controller.Get("signal") })
	defer get.Release()
	js.Global().Set("__gosx", map[string]any{"host": map[string]any{"lifecycle": map[string]any{"documentSignal": get}}})

	ctx, cancel, err := DocumentContext(context.Background())
	if err != nil || ctx.Err() != nil {
		t.Fatalf("live scope: %v", err)
	}
	controller.Call("abort")
	awaitCanceled(t, ctx, "native navigation did not cancel request")
	cancel()
	late, release, err := DocumentContext(context.Background())
	if err != nil || !errors.Is(late.Err(), context.Canceled) {
		t.Fatalf("late request survived: %v", err)
	}
	release()
	controller = js.Global().Get("AbortController").New()
	restored, release, err := DocumentContext(context.Background())
	if err != nil || restored.Err() != nil {
		t.Fatalf("restored document unavailable: %v", err)
	}
	release()
	release()                // exact-once listener disposal
	controller.Call("abort") // the released scope has no listener left to run

	parent, stop := context.WithCancel(context.Background())
	controller = js.Global().Get("AbortController").New()
	child, release, err := DocumentContext(parent)
	if err != nil {
		t.Fatal(err)
	}
	stop()
	if !errors.Is(child.Err(), context.Canceled) {
		t.Fatal("parent cancellation lost")
	}
	release()
}

func TestDocumentContextRequiresRuntime(t *testing.T) {
	previous := js.Global().Get("__gosx")
	defer js.Global().Set("__gosx", previous)
	js.Global().Delete("__gosx")
	if _, _, err := DocumentContext(context.Background()); err == nil {
		t.Fatal("missing bootstrap accepted")
	}
	if _, _, err := DocumentContext(nil); err == nil {
		t.Fatal("nil parent accepted")
	}
}

// The browser aborts the document signal inside the call that starts a
// navigation (location.assign, a form submission, a link click). Go must not run
// there: Go's js/wasm runtime can fail when two goroutines are inside nested
// entries. controller.abort() stands for that call.
func TestDocumentContextNeverRunsGoInsideTheAbortCall(t *testing.T) {
	probe := jsreentry.Install(t)
	state := documentLifecycle(t)
	ctx, cancel, err := DocumentContext(context.Background())
	if err != nil || ctx.Err() != nil {
		t.Fatalf("live scope: %v", err)
	}
	defer cancel()
	if got := state.Get("live").Get("size").Int(); got != 1 {
		t.Fatalf("a live scope must hold one abort listener, holds %d", got)
	}
	probe.Reset()
	state.Get("controller").Call("abort")
	probe.AssertNone(t, "the abort listener of DocumentContext")
	awaitCanceled(t, ctx, "the abort did not cancel the context")
	waitUntil(t, "the abort listener to be removed", func() bool { return state.Get("live").Get("size").Int() == 0 })
	probe.AssertNone(t, "canceling the document context")
}

func TestDocumentContextRemovesItsListenerOnEveryExit(t *testing.T) {
	state := documentLifecycle(t)
	live := func() int { return state.Get("live").Get("size").Int() }

	// The caller finishes its work.
	_, release, err := DocumentContext(context.Background())
	if err != nil || live() != 1 {
		t.Fatalf("scope: %v, %d listeners", err, live())
	}
	release()
	if live() != 0 {
		t.Fatalf("release left %d listeners", live())
	}
	release()

	// The parent ends first.
	parent, stop := context.WithCancel(context.Background())
	child, release, err := DocumentContext(parent)
	if err != nil {
		t.Fatal(err)
	}
	stop()
	awaitCanceled(t, child, "parent cancellation lost")
	waitUntil(t, "the parent's cancellation to remove the listener", func() bool { return live() == 0 })
	release()

	// The document starts navigating first.
	nav, release, err := DocumentContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state.Get("controller").Call("abort")
	awaitCanceled(t, nav, "navigation lost")
	waitUntil(t, "navigation to remove the listener", func() bool { return live() == 0 })
	release()

	// A scope that starts after the abort is already canceled and holds nothing.
	late, release, err := DocumentContext(context.Background())
	if err != nil || !errors.Is(late.Err(), context.Canceled) || live() != 0 {
		t.Fatalf("late scope: err=%v ctx=%v listeners=%d", err, late.Err(), live())
	}
	release()

	// A restored document gets a live scope again.
	state.Call("renew")
	restored, release, err := DocumentContext(context.Background())
	if err != nil || restored.Err() != nil || live() != 1 {
		t.Fatalf("restored scope: err=%v ctx=%v listeners=%d", err, restored.Err(), live())
	}
	release()
	if live() != 0 {
		t.Fatalf("release left %d listeners", live())
	}
}
