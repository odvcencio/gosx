//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/client/vm"
)

func browserTestGlobal(t *testing.T, name string, value any) {
	t.Helper()
	previous := js.Global().Get(name)
	js.Global().Set(name, value)
	t.Cleanup(func() { js.Global().Set(name, previous) })
}

func browserTestFunc(t *testing.T, fn func(this js.Value, args []js.Value) any) js.Func {
	t.Helper()
	wrapped := js.FuncOf(fn)
	t.Cleanup(wrapped.Release)
	return wrapped
}

func TestBrowserHostCurrentEventEffectsAndGuard(t *testing.T) {
	prevented := 0
	stopped := 0
	event := js.Global().Get("Object").New()
	event.Set("preventDefault", browserTestFunc(t, func(js.Value, []js.Value) any {
		prevented++
		return nil
	}))
	event.Set("stopPropagation", browserTestFunc(t, func(js.Value, []js.Value) any {
		stopped++
		return nil
	}))
	browserTestGlobal(t, "__gosx_current_event", event)

	receiver := newBrowserHostReceiver("island-0")
	if result, err := receiver.Call("PreventDefault", nil); err != nil || !result.Truth() {
		t.Fatalf("PreventDefault result/error = %v/%v", result, err)
	}
	if result, err := receiver.Call("StopPropagation", []vm.Value{vm.BoolVal(false)}); err != nil || result.Truth() {
		t.Fatalf("guarded StopPropagation result/error = %v/%v", result, err)
	}
	if prevented != 1 || stopped != 0 {
		t.Fatalf("prevented/stopped = %d/%d, want 1/0", prevented, stopped)
	}
	if result, err := receiver.Call("StopPropagation", nil); err != nil || !result.Truth() {
		t.Fatalf("StopPropagation result/error = %v/%v", result, err)
	}
	if stopped != 1 || !event.Get("__gosx_stop_island_fanout").Bool() {
		t.Fatalf("StopPropagation did not mark global island fanout stop")
	}
}

func TestBrowserHostRefreshUsesNetworkRevalidateNotStateRefresh(t *testing.T) {
	revalidated := 0
	legacyRefresh := 0
	navigation := js.Global().Get("Object").New()
	navigation.Set("revalidate", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		revalidated++
		if len(args) != 1 || !args[0].Get("preserveScroll").Bool() {
			t.Errorf("revalidate args = %v, want preserveScroll", args)
		}
		return nil
	}))
	navigation.Set("refresh", browserTestFunc(t, func(js.Value, []js.Value) any {
		legacyRefresh++
		return nil
	}))
	gosx := js.Global().Get("Object").New()
	gosx.Set("navigation", navigation)
	browserTestGlobal(t, "__gosx", gosx)

	result, err := newBrowserHostReceiver("island-0").Call("Refresh", nil)
	if err != nil || !result.Truth() {
		t.Fatalf("Refresh result/error = %v/%v", result, err)
	}
	if revalidated != 1 || legacyRefresh != 0 {
		t.Fatalf("revalidate/state-refresh calls = %d/%d, want 1/0", revalidated, legacyRefresh)
	}
}

func TestBrowserHostNavigationFallsBackToLegacySoftNavigationGlobal(t *testing.T) {
	navigated := 0
	revalidated := 0
	navigation := js.Global().Get("Object").New()
	navigation.Set("navigate", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		navigated++
		if len(args) != 2 || args[0].String() != "/next" || !args[1].Get("replace").Bool() || !args[1].Get("preserveScroll").Bool() {
			t.Errorf("navigate args = %v", args)
		}
		return nil
	}))
	navigation.Set("revalidate", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		revalidated++
		if len(args) != 1 || !args[0].Get("preserveScroll").Bool() {
			t.Errorf("revalidate args = %v", args)
		}
		return nil
	}))
	browserTestGlobal(t, "__gosx", js.Global().Get("Object").New())
	browserTestGlobal(t, "__gosx_page_nav", navigation)
	location := js.Global().Get("Object").New()
	location.Set("href", "http://localhost/current")
	browserTestGlobal(t, "location", location)

	receiver := newBrowserHostReceiver("island-0")
	if result, err := receiver.Call("Navigate", []vm.Value{vm.StringVal("/next"), vm.BoolVal(true), vm.BoolVal(true)}); err != nil || !result.Truth() {
		t.Fatalf("Navigate result/error = %v/%v", result, err)
	}
	if result, err := receiver.Call("Refresh", nil); err != nil || !result.Truth() {
		t.Fatalf("Refresh result/error = %v/%v", result, err)
	}
	if navigated != 1 || revalidated != 1 {
		t.Fatalf("fallback soft navigate/revalidate calls = %d/%d, want 1/1", navigated, revalidated)
	}
}

func TestBrowserHostNavigateHardensFallbackURLs(t *testing.T) {
	assigned := []string{}
	replaced := []string{}
	location := js.Global().Get("Object").New()
	location.Set("href", "https://app.example/current")
	location.Set("assign", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		assigned = append(assigned, args[0].String())
		return nil
	}))
	location.Set("replace", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		replaced = append(replaced, args[0].String())
		return nil
	}))
	browserTestGlobal(t, "location", location)
	browserTestGlobal(t, "__gosx", js.Global().Get("Object").New())
	browserTestGlobal(t, "__gosx_page_nav", js.Undefined())

	receiver := newBrowserHostReceiver("island-0")
	if result, err := receiver.Call("Navigate", []vm.Value{vm.StringVal("https://other.example/path")}); err != nil || !result.Truth() {
		t.Fatalf("cross-origin Navigate result/error = %v/%v", result, err)
	}
	if result, err := receiver.Call("Navigate", []vm.Value{vm.StringVal("https://other.example/replaced"), vm.BoolVal(true)}); err != nil || !result.Truth() {
		t.Fatalf("cross-origin replace Navigate result/error = %v/%v", result, err)
	}
	if len(assigned) != 1 || assigned[0] != "https://other.example/path" || len(replaced) != 1 || replaced[0] != "https://other.example/replaced" {
		t.Fatalf("assigned/replaced = %v/%v", assigned, replaced)
	}

	for _, unsafe := range []string{
		"javascript:alert(1)",
		"data:text/html,attacker",
		"vbscript:msgbox(1)",
		"blob:https://app.example/attacker",
		"http://[",
	} {
		result, err := receiver.Call("Navigate", []vm.Value{vm.StringVal(unsafe)})
		if err != nil || result.Truth() {
			t.Fatalf("unsafe Navigate %q result/error = %v/%v, want false/nil", unsafe, result, err)
		}
	}
	if len(assigned) != 1 || len(replaced) != 1 || location.Get("href").String() != "https://app.example/current" {
		t.Fatalf("unsafe navigation mutated location: assigned/replaced/href = %v/%v/%q", assigned, replaced, location.Get("href").String())
	}
}

func TestBrowserHostManagedNavigationRejectionFallsBackAndReleasesCallbacks(t *testing.T) {
	var navigateFulfilled, navigateRejected js.Value
	var refreshFulfilled, refreshRejected js.Value
	navigatePromise := js.Global().Get("Object").New()
	navigatePromise.Set("then", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		navigateFulfilled, navigateRejected = args[0], args[1]
		return navigatePromise
	}))
	refreshPromise := js.Global().Get("Object").New()
	refreshPromise.Set("then", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		refreshFulfilled, refreshRejected = args[0], args[1]
		return refreshPromise
	}))
	navigation := js.Global().Get("Object").New()
	navigation.Set("navigate", browserTestFunc(t, func(js.Value, []js.Value) any { return navigatePromise }))
	navigation.Set("revalidate", browserTestFunc(t, func(js.Value, []js.Value) any { return refreshPromise }))
	gosx := js.Global().Get("Object").New()
	gosx.Set("navigation", navigation)
	browserTestGlobal(t, "__gosx", gosx)

	assigned := []string{}
	reloaded := 0
	location := js.Global().Get("Object").New()
	location.Set("href", "https://app.example/current")
	location.Set("origin", "https://app.example")
	location.Set("assign", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		assigned = append(assigned, args[0].String())
		return nil
	}))
	location.Set("reload", browserTestFunc(t, func(js.Value, []js.Value) any {
		reloaded++
		return nil
	}))
	browserTestGlobal(t, "location", location)

	receiver := newBrowserHostReceiver("island-0")
	pendingCallbacks := browserPendingPromiseCallbacks
	if result, err := receiver.Call("Navigate", []vm.Value{vm.StringVal("/next")}); err != nil || !result.Truth() {
		t.Fatalf("Navigate result/error = %v/%v", result, err)
	}
	if navigateRejected.IsUndefined() || navigateRejected.IsNull() || len(assigned) != 0 || browserPendingPromiseCallbacks != pendingCallbacks+1 {
		t.Fatalf("Navigate rejection was not retained safely: rejected/assigned/pending = %v/%v/%d", navigateRejected, assigned, browserPendingPromiseCallbacks)
	}
	navigateRejected.Invoke(js.Global().Get("Error").New("soft navigation failed"))
	if len(assigned) != 1 || assigned[0] != "https://app.example/next" || browserPendingPromiseCallbacks != pendingCallbacks {
		t.Fatalf("Navigate rejection fallback/pending = %v/%d", assigned, browserPendingPromiseCallbacks)
	}

	if result, err := receiver.Call("Refresh", nil); err != nil || !result.Truth() {
		t.Fatalf("Refresh result/error = %v/%v", result, err)
	}
	if refreshRejected.IsUndefined() || refreshRejected.IsNull() || reloaded != 0 || browserPendingPromiseCallbacks != pendingCallbacks+1 {
		t.Fatalf("Refresh rejection was not retained safely: rejected/reloaded/pending = %v/%d/%d", refreshRejected, reloaded, browserPendingPromiseCallbacks)
	}
	refreshRejected.Invoke(js.Global().Get("Error").New("revalidate failed"))
	if reloaded != 1 || browserPendingPromiseCallbacks != pendingCallbacks {
		t.Fatalf("Refresh rejection reloads/pending = %d/%d, want 1/%d", reloaded, browserPendingPromiseCallbacks, pendingCallbacks)
	}
	if navigateFulfilled.IsUndefined() || refreshFulfilled.IsUndefined() {
		t.Fatal("managed promises did not receive paired settlement callbacks")
	}
	if result, err := receiver.Call("Navigate", []vm.Value{vm.StringVal("/settled")}); err != nil || !result.Truth() {
		t.Fatalf("settled Navigate result/error = %v/%v", result, err)
	}
	if browserPendingPromiseCallbacks != pendingCallbacks+1 {
		t.Fatalf("settled Navigate pending callbacks = %d", browserPendingPromiseCallbacks)
	}
	navigateFulfilled.Invoke()
	if browserPendingPromiseCallbacks != pendingCallbacks || len(assigned) != 1 {
		t.Fatalf("fulfilled Navigate pending/fallback = %d/%v", browserPendingPromiseCallbacks, assigned)
	}
}

func TestBrowserHostFocusDoesNotReenterOuterDispatch(t *testing.T) {
	outerDispatching := true
	reentered := false
	focused := 0
	element := js.Global().Get("Object").New()
	element.Set("focus", browserTestFunc(t, func(js.Value, []js.Value) any {
		focused++
		if outerDispatching {
			reentered = true
		}
		return nil
	}))
	root := js.Global().Get("Object").New()
	root.Set("matches", browserTestFunc(t, func(js.Value, []js.Value) any { return false }))
	root.Set("querySelector", browserTestFunc(t, func(js.Value, []js.Value) any { return element }))
	document := js.Global().Get("Object").New()
	document.Set("getElementById", browserTestFunc(t, func(js.Value, []js.Value) any { return root }))
	browserTestGlobal(t, "document", document)

	var queued js.Value
	browserTestGlobal(t, "queueMicrotask", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		queued = args[0]
		return nil
	}))
	receiver := newBrowserHostReceiver("island-0")
	result, err := receiver.Call("Focus", []vm.Value{vm.StringVal("#search")})
	if err != nil || !result.Truth() {
		t.Fatalf("Focus result/error = %v/%v", result, err)
	}
	if focused != 0 || reentered {
		t.Fatalf("Focus synchronously re-entered outer dispatch: focused=%d reentered=%v", focused, reentered)
	}
	if queued.IsUndefined() || queued.IsNull() {
		t.Fatal("Focus did not queue its DOM effect")
	}
	outerDispatching = false
	queued.Invoke()
	if focused != 1 || reentered {
		t.Fatalf("deferred Focus order = focused=%d reentered=%v, want 1/false", focused, reentered)
	}
}

func TestBrowserHostSelectorSkipsNestedIslandMatches(t *testing.T) {
	nestedFocused := 0
	ownedFocused := 0
	root := js.Global().Get("Object").New()
	root.Set("matches", browserTestFunc(t, func(js.Value, []js.Value) any { return false }))
	root.Set("hasAttribute", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		return args[0].String() == "data-gosx-island"
	}))
	nestedRoot := js.Global().Get("Object").New()
	nestedRoot.Set("parentNode", root)
	nestedRoot.Set("hasAttribute", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		return args[0].String() == "data-gosx-island"
	}))
	nested := js.Global().Get("Object").New()
	nested.Set("parentNode", nestedRoot)
	nested.Set("focus", browserTestFunc(t, func(js.Value, []js.Value) any {
		nestedFocused++
		return nil
	}))
	owned := js.Global().Get("Object").New()
	owned.Set("parentNode", root)
	owned.Set("focus", browserTestFunc(t, func(js.Value, []js.Value) any {
		ownedFocused++
		return nil
	}))
	list := js.Global().Get("Array").New(2)
	list.SetIndex(0, nested)
	list.SetIndex(1, owned)
	root.Set("querySelector", browserTestFunc(t, func(js.Value, []js.Value) any { return nested }))
	root.Set("querySelectorAll", browserTestFunc(t, func(js.Value, []js.Value) any { return list }))
	document := js.Global().Get("Object").New()
	document.Set("getElementById", browserTestFunc(t, func(js.Value, []js.Value) any { return root }))
	browserTestGlobal(t, "document", document)
	var queued js.Value
	browserTestGlobal(t, "queueMicrotask", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		queued = args[0]
		return nil
	}))

	receiver := newBrowserHostReceiver("outer")
	result, err := receiver.Call("Focus", []vm.Value{vm.StringVal("[data-focus]")})
	if err != nil || !result.Truth() {
		t.Fatalf("Focus result/error = %v/%v", result, err)
	}
	queued.Invoke()
	if nestedFocused != 0 || ownedFocused != 1 {
		t.Fatalf("nested/owned focus calls = %d/%d, want 0/1", nestedFocused, ownedFocused)
	}
}

func TestBrowserHostDialogEffectsRunAfterOuterHandler(t *testing.T) {
	opened := 0
	closed := 0
	element := js.Global().Get("Object").New()
	element.Set("showModal", browserTestFunc(t, func(js.Value, []js.Value) any {
		opened++
		return nil
	}))
	element.Set("close", browserTestFunc(t, func(js.Value, []js.Value) any {
		closed++
		return nil
	}))
	root := js.Global().Get("Object").New()
	root.Set("matches", browserTestFunc(t, func(js.Value, []js.Value) any { return false }))
	root.Set("querySelector", browserTestFunc(t, func(js.Value, []js.Value) any { return element }))
	document := js.Global().Get("Object").New()
	document.Set("getElementById", browserTestFunc(t, func(js.Value, []js.Value) any { return root }))
	browserTestGlobal(t, "document", document)
	var queued js.Value
	browserTestGlobal(t, "queueMicrotask", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		queued = args[0]
		return nil
	}))

	receiver := newBrowserHostReceiver("island-0")
	for _, method := range []string{"Open", "Close"} {
		result, err := receiver.Call(method, []vm.Value{vm.StringVal("#dialog")})
		if err != nil || !result.Truth() {
			t.Fatalf("%s result/error = %v/%v", method, result, err)
		}
		if opened != 0 || closed != 0 || queued.IsUndefined() || queued.IsNull() {
			t.Fatalf("%s ran synchronously: opened/closed/queued = %d/%d/%v", method, opened, closed, queued)
		}
		queued.Invoke()
		if method == "Open" {
			if opened != 1 {
				t.Fatalf("Open did not run in microtask: opened=%d", opened)
			}
			opened = 0
		} else {
			if closed != 1 {
				t.Fatalf("Close did not run in microtask: closed=%d", closed)
			}
			closed = 0
		}
	}
}

func TestBrowserHostCloseDetailsKeepsSummaryVisible(t *testing.T) {
	details := js.Global().Get("Object").New()
	details.Set("tagName", "DETAILS")
	details.Set("open", true)
	details.Set("hidden", false)
	details.Set("setAttribute", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		t.Errorf("Close DETAILS unexpectedly set attribute %q", args[0].String())
		return nil
	}))
	details.Set("removeAttribute", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		if len(args) != 1 || args[0].String() != "open" {
			t.Errorf("removeAttribute args = %v, want open", args)
		}
		return nil
	}))
	root := js.Global().Get("Object").New()
	root.Set("matches", browserTestFunc(t, func(js.Value, []js.Value) any { return false }))
	root.Set("querySelector", browserTestFunc(t, func(js.Value, []js.Value) any { return details }))
	document := js.Global().Get("Object").New()
	document.Set("getElementById", browserTestFunc(t, func(js.Value, []js.Value) any { return root }))
	browserTestGlobal(t, "document", document)
	var queued js.Value
	browserTestGlobal(t, "queueMicrotask", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		queued = args[0]
		return nil
	}))

	result, err := newBrowserHostReceiver("island-0").Call("Close", []vm.Value{vm.StringVal("#disclosure")})
	if err != nil || !result.Truth() {
		t.Fatalf("Close result/error = %v/%v", result, err)
	}
	queued.Invoke()
	if details.Get("open").Bool() || details.Get("hidden").Bool() {
		t.Fatalf("closed details open/hidden = %v/%v, want false/false", details.Get("open").Bool(), details.Get("hidden").Bool())
	}
}

func TestBrowserHostSubmitDefersSelectorLookupUntilAfterReconcile(t *testing.T) {
	patched := false
	submitted := 0
	form := js.Global().Get("Object").New()
	form.Set("requestSubmit", browserTestFunc(t, func(js.Value, []js.Value) any {
		if !patched {
			t.Error("requestSubmit ran before the caller marked reconciliation complete")
		}
		submitted++
		return nil
	}))
	root := js.Global().Get("Object").New()
	root.Set("matches", browserTestFunc(t, func(js.Value, []js.Value) any { return false }))
	root.Set("querySelector", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		if len(args) != 1 || args[0].String() != "#move-form" {
			t.Errorf("querySelector args = %v", args)
			return js.Null()
		}
		return form
	}))
	document := js.Global().Get("Object").New()
	document.Set("getElementById", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		if len(args) != 1 || args[0].String() != "island-0" {
			t.Errorf("getElementById args = %v", args)
			return js.Null()
		}
		return root
	}))
	browserTestGlobal(t, "document", document)

	var queued js.Value
	queue := browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		queued = args[0]
		return nil
	})
	browserTestGlobal(t, "queueMicrotask", queue)

	receiver := newBrowserHostReceiver("island-0")
	result, err := receiver.Call("Submit", []vm.Value{vm.StringVal("#move-form")})
	if err != nil || !result.Truth() {
		t.Fatalf("Submit result/error = %v/%v", result, err)
	}
	if submitted != 0 || queued.IsUndefined() || queued.IsNull() {
		t.Fatalf("before microtask submitted/queued = %d/%v", submitted, queued)
	}
	patched = true
	queued.Invoke()
	if submitted != 1 {
		t.Fatalf("after microtask submitted = %d, want 1", submitted)
	}
}

func TestBrowserHostScrollIntoViewDefersSelectorLookupUntilAfterReconcile(t *testing.T) {
	reconciled := false
	oldScrolled := 0
	newScrolled := 0
	oldElement := js.Global().Get("Object").New()
	oldElement.Set("scrollIntoView", browserTestFunc(t, func(js.Value, []js.Value) any {
		oldScrolled++
		return nil
	}))
	newElement := js.Global().Get("Object").New()
	newElement.Set("scrollIntoView", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		newScrolled++
		if len(args) != 1 || args[0].Get("behavior").String() != "smooth" || args[0].Get("block").String() != "nearest" {
			t.Errorf("scrollIntoView args = %v", args)
		}
		return nil
	}))
	root := js.Global().Get("Object").New()
	root.Set("matches", browserTestFunc(t, func(js.Value, []js.Value) any { return false }))
	root.Set("querySelector", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		if len(args) != 1 || args[0].String() != "#revealed" {
			t.Errorf("querySelector args = %v", args)
		}
		if reconciled {
			return newElement
		}
		return oldElement
	}))
	document := js.Global().Get("Object").New()
	document.Set("getElementById", browserTestFunc(t, func(js.Value, []js.Value) any { return root }))
	browserTestGlobal(t, "document", document)
	var queued js.Value
	browserTestGlobal(t, "queueMicrotask", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		queued = args[0]
		return nil
	}))

	result, err := newBrowserHostReceiver("island-0").Call("ScrollIntoView", []vm.Value{
		vm.StringVal("#revealed"),
		vm.StringVal("smooth"),
	})
	if err != nil || !result.Truth() {
		t.Fatalf("ScrollIntoView result/error = %v/%v", result, err)
	}
	if oldScrolled != 0 || newScrolled != 0 || queued.IsUndefined() || queued.IsNull() {
		t.Fatalf("before reconcile old/new/queued = %d/%d/%v", oldScrolled, newScrolled, queued)
	}
	reconciled = true
	queued.Invoke()
	if oldScrolled != 0 || newScrolled != 1 {
		t.Fatalf("after reconcile old/new = %d/%d, want 0/1", oldScrolled, newScrolled)
	}
}

func TestBrowserHostFocusMoveWrapsVisibleRootScopedCandidates(t *testing.T) {
	focuses := []int{}
	newCandidate := func(index int, visible bool) js.Value {
		element := js.Global().Get("Object").New()
		element.Set("focus", browserTestFunc(t, func(js.Value, []js.Value) any {
			focuses = append(focuses, index)
			return nil
		}))
		element.Set("getAttribute", browserTestFunc(t, func(js.Value, []js.Value) any { return js.Null() }))
		element.Set("getClientRects", browserTestFunc(t, func(js.Value, []js.Value) any {
			rects := js.Global().Get("Object").New()
			if visible {
				rects.Set("length", 1)
			} else {
				rects.Set("length", 0)
			}
			return rects
		}))
		return element
	}
	first := newCandidate(0, true)
	hidden := newCandidate(1, false)
	last := newCandidate(2, true)
	list := js.Global().Get("Array").New(3)
	list.SetIndex(0, first)
	list.SetIndex(1, hidden)
	list.SetIndex(2, last)
	root := js.Global().Get("Object").New()
	root.Set("matches", browserTestFunc(t, func(js.Value, []js.Value) any { return false }))
	root.Set("querySelectorAll", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		if len(args) != 1 || args[0].String() != "[role=option]" {
			t.Errorf("querySelectorAll args = %v", args)
		}
		return list
	}))
	document := js.Global().Get("Object").New()
	document.Set("getElementById", browserTestFunc(t, func(js.Value, []js.Value) any { return root }))
	document.Set("activeElement", last)
	browserTestGlobal(t, "document", document)
	var queued js.Value
	browserTestGlobal(t, "queueMicrotask", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		queued = args[0]
		return nil
	}))

	receiver := newBrowserHostReceiver("island-0")
	result, err := receiver.Call("FocusMove", []vm.Value{vm.StringVal("[role=option]"), vm.IntVal(1)})
	if err != nil || !result.Truth() {
		t.Fatalf("FocusMove forward result/error = %v/%v", result, err)
	}
	if len(focuses) != 0 || queued.IsUndefined() || queued.IsNull() {
		t.Fatalf("FocusMove ran before microtask: focuses/queued = %v/%v", focuses, queued)
	}
	queued.Invoke()
	document.Set("activeElement", first)
	result, err = receiver.Call("FocusMove", []vm.Value{vm.StringVal("[role=option]"), vm.IntVal(-1)})
	if err != nil || !result.Truth() {
		t.Fatalf("FocusMove backward result/error = %v/%v", result, err)
	}
	if len(focuses) != 1 {
		t.Fatalf("second FocusMove ran before microtask: %v", focuses)
	}
	queued.Invoke()
	if len(focuses) != 2 || focuses[0] != 0 || focuses[1] != 2 {
		t.Fatalf("focus order = %v, want wrapped [0 2] with hidden candidate skipped", focuses)
	}
}

func TestBrowserHostActivateDefersFirstVisibleMatch(t *testing.T) {
	clicked := []int{}
	newCandidate := func(index int, visible bool) js.Value {
		element := js.Global().Get("Object").New()
		element.Set("click", browserTestFunc(t, func(js.Value, []js.Value) any {
			clicked = append(clicked, index)
			return nil
		}))
		element.Set("getClientRects", browserTestFunc(t, func(js.Value, []js.Value) any {
			rects := js.Global().Get("Object").New()
			if visible {
				rects.Set("length", 1)
			} else {
				rects.Set("length", 0)
			}
			return rects
		}))
		return element
	}
	hidden := newCandidate(0, false)
	visible := newCandidate(1, true)
	list := js.Global().Get("Array").New(2)
	list.SetIndex(0, hidden)
	list.SetIndex(1, visible)
	root := js.Global().Get("Object").New()
	root.Set("matches", browserTestFunc(t, func(js.Value, []js.Value) any { return false }))
	root.Set("querySelectorAll", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		if len(args) != 1 || args[0].String() != "[data-command-result]" {
			t.Errorf("querySelectorAll args = %v", args)
		}
		return list
	}))
	document := js.Global().Get("Object").New()
	document.Set("getElementById", browserTestFunc(t, func(js.Value, []js.Value) any { return root }))
	browserTestGlobal(t, "document", document)

	var queued js.Value
	browserTestGlobal(t, "queueMicrotask", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		queued = args[0]
		return nil
	}))
	receiver := newBrowserHostReceiver("island-0")
	result, err := receiver.Call("Activate", []vm.Value{vm.StringVal("[data-command-result]")})
	if err != nil || !result.Truth() {
		t.Fatalf("Activate result/error = %v/%v", result, err)
	}
	if len(clicked) != 0 || queued.IsUndefined() || queued.IsNull() {
		t.Fatalf("before microtask clicked/queued = %v/%v", clicked, queued)
	}
	queued.Invoke()
	if len(clicked) != 1 || clicked[0] != 1 {
		t.Fatalf("clicked = %v, want first visible candidate [1]", clicked)
	}
}

func TestBrowserHostClipboardWriteUsesRetentionFreeFallback(t *testing.T) {
	selected := 0
	appended := 0
	removed := 0
	restoredFocus := 0
	textarea := js.Global().Get("Object").New()
	textarea.Set("style", js.Global().Get("Object").New())
	textarea.Set("setAttribute", browserTestFunc(t, func(js.Value, []js.Value) any { return nil }))
	textarea.Set("select", browserTestFunc(t, func(js.Value, []js.Value) any {
		selected++
		return nil
	}))
	body := js.Global().Get("Object").New()
	body.Set("appendChild", browserTestFunc(t, func(js.Value, []js.Value) any {
		appended++
		return textarea
	}))
	body.Set("removeChild", browserTestFunc(t, func(js.Value, []js.Value) any {
		removed++
		return textarea
	}))
	previousFocus := js.Global().Get("Object").New()
	previousFocus.Set("focus", browserTestFunc(t, func(js.Value, []js.Value) any {
		restoredFocus++
		return nil
	}))
	document := js.Global().Get("Object").New()
	document.Set("body", body)
	document.Set("activeElement", previousFocus)
	document.Set("createElement", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		if len(args) != 1 || args[0].String() != "textarea" {
			t.Errorf("createElement args = %v", args)
		}
		return textarea
	}))
	document.Set("execCommand", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		return len(args) == 1 && args[0].String() == "copy"
	}))
	browserTestGlobal(t, "document", document)
	browserTestGlobal(t, "navigator", js.Global().Get("Object").New())
	var queued js.Value
	browserTestGlobal(t, "queueMicrotask", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		queued = args[0]
		return nil
	}))

	receiver := newBrowserHostReceiver("island-0")
	result, err := receiver.Call("ClipboardWrite", []vm.Value{vm.StringVal("copy me")})
	if err != nil || !result.Truth() {
		t.Fatalf("ClipboardWrite result/error = %v/%v", result, err)
	}
	if selected != 0 || appended != 0 || removed != 0 || restoredFocus != 0 || queued.IsUndefined() || queued.IsNull() {
		t.Fatalf("clipboard fallback ran inside outer dispatch: selected/appended/removed/restored/queued = %d/%d/%d/%d/%v",
			selected, appended, removed, restoredFocus, queued)
	}
	queued.Invoke()
	if textarea.Get("value").String() != "copy me" || selected != 1 || appended != 1 || removed != 1 || restoredFocus != 1 {
		t.Fatalf("fallback value/selected/appended/removed/restored = %q/%d/%d/%d/%d",
			textarea.Get("value").String(), selected, appended, removed, restoredFocus)
	}
}

func TestBrowserHostClipboardPromiseRejectionUsesFallback(t *testing.T) {
	removed := 0
	copied := 0
	textarea := js.Global().Get("Object").New()
	textarea.Set("style", js.Global().Get("Object").New())
	textarea.Set("select", browserTestFunc(t, func(js.Value, []js.Value) any { return nil }))
	body := js.Global().Get("Object").New()
	body.Set("appendChild", browserTestFunc(t, func(js.Value, []js.Value) any { return textarea }))
	body.Set("removeChild", browserTestFunc(t, func(js.Value, []js.Value) any {
		removed++
		return textarea
	}))
	document := js.Global().Get("Object").New()
	document.Set("body", body)
	document.Set("createElement", browserTestFunc(t, func(js.Value, []js.Value) any { return textarea }))
	document.Set("execCommand", browserTestFunc(t, func(js.Value, []js.Value) any {
		copied++
		return true
	}))
	browserTestGlobal(t, "document", document)

	var rejected js.Value
	promise := js.Global().Get("Object").New()
	promise.Set("then", browserTestFunc(t, func(_ js.Value, args []js.Value) any {
		if len(args) != 2 {
			t.Fatalf("Promise.then args = %d, want 2", len(args))
		}
		rejected = args[1]
		return promise
	}))
	clipboard := js.Global().Get("Object").New()
	clipboard.Set("writeText", browserTestFunc(t, func(js.Value, []js.Value) any { return promise }))
	navigator := js.Global().Get("navigator")
	if navigator.IsUndefined() || navigator.IsNull() {
		navigator = js.Global().Get("Object").New()
		browserTestGlobal(t, "navigator", navigator)
	}
	previousClipboard := navigator.Get("clipboard")
	navigator.Set("clipboard", clipboard)
	t.Cleanup(func() { navigator.Set("clipboard", previousClipboard) })

	receiver := newBrowserHostReceiver("island-0")
	result, err := receiver.Call("ClipboardWrite", []vm.Value{vm.StringVal("retry me")})
	if err != nil || !result.Truth() {
		t.Fatalf("ClipboardWrite result/error = %v/%v", result, err)
	}
	if copied != 0 || rejected.IsUndefined() || rejected.IsNull() {
		t.Fatalf("before rejection copied/rejected = %d/%v", copied, rejected)
	}
	rejected.Invoke(js.Global().Get("Error").New("denied"))
	if copied != 1 || removed != 1 || textarea.Get("value").String() != "retry me" {
		t.Fatalf("fallback copied/removed/value = %d/%d/%q", copied, removed, textarea.Get("value").String())
	}
}

func TestBrowserHostRejectsInvalidTypesWithoutPanicking(t *testing.T) {
	receiver := newBrowserHostReceiver("island-0")
	if _, err := receiver.Call("Focus", []vm.Value{vm.IntVal(1)}); err == nil {
		t.Fatal("Focus accepted a non-string selector")
	}
	if _, err := receiver.Call("Eval", nil); err == nil {
		t.Fatal("unknown browser method was accepted")
	}
}
