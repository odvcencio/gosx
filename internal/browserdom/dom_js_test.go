//go:build js && wasm

package browserdom

import (
	"syscall/js"
	"testing"
)

func domObject() js.Value { return js.Global().Get("Object").New() }
func domMethod(t *testing.T, target js.Value, name string, fn func([]js.Value) any) {
	t.Helper()
	callback := js.FuncOf(func(_ js.Value, args []js.Value) any { return fn(args) })
	target.Set(name, callback)
	t.Cleanup(callback.Release)
}
func domGetter(t *testing.T, target js.Value, name string, fn func() any) {
	t.Helper()
	callback := js.FuncOf(func(js.Value, []js.Value) any { return fn() })
	js.Global().Get("Object").Call("defineProperty", target, name, map[string]any{"get": callback, "configurable": true})
	t.Cleanup(callback.Release)
}

func TestListenerReadsOnlyUsedEventFieldsAndDisposes(t *testing.T) {
	target, event := domObject(), domObject()
	keyReads, pointerReads, targetReads := 0, 0, 0
	domGetter(t, event, "key", func() any { keyReads++; return "Escape" })
	domGetter(t, event, "clientX", func() any { pointerReads++; return 8 })
	domGetter(t, event, "target", func() any { targetReads++; return target })
	var callback js.Value
	removes, calls := 0, 0
	domMethod(t, target, "addEventListener", func(args []js.Value) any { callback = args[1]; return nil })
	domMethod(t, target, "removeEventListener", func(args []js.Value) any {
		removes++
		if args[0].String() != "keydown" || !args[1].Equal(callback) || !args[2].Bool() {
			t.Error("listener removed with different identity/options")
		}
		return nil
	})
	listener := FromJS(target).On("keydown", func(e Event) {
		calls++
		if e.Key() != "Escape" {
			t.Error("wrong key")
		}
	}, ListenerOptions{Capture: true})
	if keyReads+pointerReads+targetReads != 0 {
		t.Fatal("registration eagerly read event fields")
	}
	callback.Invoke(event)
	if calls != 1 || keyReads != 1 || pointerReads != 0 || targetReads != 0 {
		t.Fatal("event dispatch eagerly read unrelated fields")
	}
	listener.Dispose()
	listener.Dispose()
	if removes != 1 {
		t.Fatalf("removals=%d, want one", removes)
	}
	// A late native invocation is prevented by removeEventListener before release.
	// The held native callback itself must not be invoked after release.
	var missing Element
	missing.On("click", func(Event) { t.Error("missing element dispatched") }).Dispose()
}

func TestListenerDisposeWithinHandlerAndOnce(t *testing.T) {
	target, event := domObject(), domObject()
	var callback js.Value
	removes := 0
	domMethod(t, target, "addEventListener", func(args []js.Value) any { callback = args[1]; return nil })
	domMethod(t, target, "removeEventListener", func([]js.Value) any { removes++; return nil })
	var listener *Listener
	listener = FromJS(target).On("pointerup", func(Event) { listener.Dispose() }, ListenerOptions{Once: true})
	callback.Invoke(event)
	listener.Dispose()
	if removes != 1 {
		t.Fatalf("handler/once double-release: removes=%d", removes)
	}
}

func TestTypedElementAndDatasetHandleSemantics(t *testing.T) {
	node, style, classes, dataset := domObject(), domObject(), domObject(), domObject()
	styleReads := 0
	domGetter(t, node, "style", func() any { styleReads++; return style })
	node.Set("classList", classes)
	node.Set("dataset", dataset)
	node.Set("textContent", "")
	first, second := FromJS(node), FromJS(node)
	if !first.Equal(second) || !first.Valid() || (Element{}).Valid() {
		t.Fatal("opaque element identity mismatch")
	}
	first.SetText("changed")
	if second.Text() != "changed" {
		t.Fatal("handle copy lost live node identity")
	}
	retained := first.Style()
	for i := 0; i < 10; i++ {
		retained.Set("opacity", "1")
	}
	if styleReads != 1 {
		t.Fatalf("cached style performed %d native lookups", styleReads)
	}
	first.Dataset().Set("frameCpuMs", "1.25")
	if second.Dataset().Get("frameCpuMs") != "1.25" || second.Dataset().Get("missing") != "" {
		t.Fatal("dataset access changed string/missing contract")
	}
	retainedClasses := first.Classes()
	toggles := 0
	domMethod(t, classes, "toggle", func(args []js.Value) any { toggles++; return args[1] })
	retainedClasses.Toggle("active", true)
	if toggles != 1 {
		t.Fatal("class toggle did not reach native classList")
	}
}

func TestElementHandlesAndLazyTargetsAllocateNoWrappers(t *testing.T) {
	node, event := domObject(), domObject()
	event.Set("target", node)
	e := Event{event}
	if got := testing.AllocsPerRun(1000, func() {
		handle := FromJS(node)
		if !handle.Valid() {
			panic("missing")
		}
	}); got != 0 {
		t.Fatalf("element wrapper allocations/run=%g", got)
	}
	// Native js.Value.Get itself allocates in Go's bridge. Compare the typed
	// accessor with that baseline, so a wrapper cannot add hot-path allocation.
	raw := testing.AllocsPerRun(1000, func() { _ = event.Get("target") })
	typed := testing.AllocsPerRun(1000, func() { _ = e.Target() })
	if typed != raw {
		t.Fatalf("target allocations typed=%g raw=%g", typed, raw)
	}
}

func BenchmarkTypedEventTarget(b *testing.B) {
	node, event := domObject(), domObject()
	event.Set("target", node)
	e := Event{event}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.Target()
	}
}

func BenchmarkRawEventTarget(b *testing.B) {
	node, event := domObject(), domObject()
	event.Set("target", node)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = event.Get("target")
	}
}

func TestTypedRectReadsBoundedLayoutFields(t *testing.T) {
	node, rect := domObject(), domObject()
	reads, calls := 0, 0
	for name, value := range map[string]float64{"left": 10, "top": 20, "width": 300, "height": 200} {
		domGetter(t, rect, name, func() any { reads++; return value })
	}
	// Right/bottom are derived from the rectangle's size; no extra native
	// bridge reads are needed on the pointer projection hot path.
	domGetter(t, rect, "right", func() any { t.Error("redundant right layout read"); return 310 })
	domGetter(t, rect, "bottom", func() any { t.Error("redundant bottom layout read"); return 220 })
	domMethod(t, node, "getBoundingClientRect", func([]js.Value) any { calls++; return rect })
	r := FromJS(node).Rect()
	if calls != 1 || reads != 4 || r.Left != 10 || r.Top != 20 || r.Width != 300 || r.Height != 200 || r.Right != 310 || r.Bottom != 220 {
		t.Fatalf("unexpected rect %+v calls=%d reads=%d", r, calls, reads)
	}
}

func TestMessageDataAcceptsOnlyStrings(t *testing.T) {
	event := domObject()
	e := Event{event}
	for _, value := range []any{nil, 42, map[string]any{"text": "wrong"}} {
		event.Set("data", value)
		if e.DataString() != "" {
			t.Fatal("non-string message escaped typed bridge")
		}
	}
	event.Set("data", "native focus lost")
	if e.DataString() != "native focus lost" {
		t.Fatal("string message lost")
	}
}
