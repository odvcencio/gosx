//go:build js && wasm

package jsreentry

import (
	"strings"
	"syscall/js"
	"testing"
	"time"
)

// native builds a JavaScript function from source, so no Go runs inside it.
func native(source string) js.Value {
	return js.Global().Get("Function").New("return (" + source + ")").Invoke()
}

func TestProbeCountsAGoExecutorRunInsideNewPromise(t *testing.T) {
	probe := Install(t)
	executor := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	defer executor.Release()
	js.Global().Get("Promise").New(executor)
	if probe.Count() != 1 || probe.MaxDepth() != 2 {
		t.Fatalf("a Go executor must count as one nested entry at depth 2: count=%d depth=%d", probe.Count(), probe.MaxDepth())
	}
	if report := probe.Report(); !strings.Contains(report, "nested entry 1 of 1") {
		t.Fatalf("report lost the entry: %q", report)
	}
}

func TestProbeCountsAGoListenerRunByDispatchEvent(t *testing.T) {
	probe := Install(t)
	target := native(`new EventTarget()`)
	event := native(`new Event("ping")`)
	nativeHits := native(`(function () { const hits = { n: 0 }; return hits; })()`)
	target.Call("addEventListener", "ping", native(`(function (hits) { return function () { hits.n++; }; })`).Invoke(nativeHits))
	target.Call("dispatchEvent", event)
	probe.AssertNone(t, "a JavaScript listener must not count")
	if nativeHits.Get("n").Int() != 1 {
		t.Fatal("the native listener did not run")
	}

	goHits := 0
	listener := js.FuncOf(func(js.Value, []js.Value) any { goHits++; return nil })
	defer listener.Release()
	target.Call("addEventListener", "ping", listener)
	target.Call("dispatchEvent", event)
	if goHits != 1 || probe.Count() != 1 {
		t.Fatalf("a Go listener run by dispatchEvent must count once: hits=%d count=%d", goHits, probe.Count())
	}
}

func TestProbeIgnoresCallbacksThatRunAfterTheCallReturns(t *testing.T) {
	probe := Install(t)
	// Install itself waits for one timer entry; count only the three below.
	probe.Reset()
	done := make(chan struct{}, 3)
	signal := js.FuncOf(func(js.Value, []js.Value) any { done <- struct{}{}; return nil })
	defer signal.Release()
	js.Global().Get("Promise").Call("resolve", 1).Call("then", signal)
	js.Global().Call("queueMicrotask", signal)
	js.Global().Call("setTimeout", signal, 0)
	for i := 0; i < 3; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("a deferred callback never ran")
		}
	}
	probe.AssertNone(t, "microtask and timer callbacks")
	if probe.Entries() < 3 {
		t.Fatalf("the probe missed entries: %d", probe.Entries())
	}
}

func TestProbeReportsNestingDepth(t *testing.T) {
	probe := Install(t)
	var nest func(depth int)
	nest = func(depth int) {
		executor := js.FuncOf(func(js.Value, []js.Value) any {
			if depth > 0 {
				nest(depth - 1)
			}
			return nil
		})
		defer executor.Release()
		js.Global().Get("Promise").New(executor)
	}
	nest(2)
	if probe.Count() != 3 || probe.MaxDepth() != 4 {
		t.Fatalf("three nested Go executors must count three entries at depth 4: count=%d depth=%d", probe.Count(), probe.MaxDepth())
	}
	probe.Reset()
	if probe.Count() != 0 || probe.Entries() != 0 {
		t.Fatal("Reset kept earlier entries")
	}
}

// Install must leave the calling goroutine inside an entry that the probe
// counts, however busy the program is. It once paused with time.Sleep(1ms). A
// goroutine that kept the scheduler busy for longer let that timer expire before
// the program went back to JavaScript, so Go woke the sleeper itself, the sleeper
// stayed inside the entry that began before Install, and the probe recorded a
// Go executor run inside new Promise at depth 1 and counted no nested entry.
func TestInstallCountsNestedEntriesWhileTheSchedulerIsBusy(t *testing.T) {
	go func() {
		for start := time.Now(); time.Since(start) < 5*time.Millisecond; {
		}
	}()
	probe := Install(t)
	if depth := probe.state.Get("depth").Int(); depth < 1 {
		t.Fatalf("Install returned outside every counted entry: depth %d", depth)
	}
	executor := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	defer executor.Release()
	js.Global().Get("Promise").New(executor)
	if probe.Count() != 1 || probe.MaxDepth() != 2 {
		t.Fatalf("a Go executor must count as one nested entry at depth 2: count=%d depth=%d", probe.Count(), probe.MaxDepth())
	}
}

func TestOnlyOneProbeAtATime(t *testing.T) {
	probe := Install(t)
	if active != probe {
		t.Fatal("the installed probe is not the active one")
	}
	probe.uninstall()
	if active != nil {
		t.Fatal("uninstall left the probe active")
	}
	// A second probe after an uninstall must work.
	again := Install(t)
	again.AssertNone(t, "fresh probe")
}
