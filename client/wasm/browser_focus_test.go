//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/client/vm"
)

func TestBrowserHostFocusFollowsNativeEventDefault(t *testing.T) {
	defaultFinished := false
	focused := false
	done := make(chan struct{}, 1)
	element := js.Global().Get("Object").New()
	element.Set("focus", browserTestFunc(t, func(js.Value, []js.Value) any {
		if !defaultFinished {
			t.Error("focus ran before the event default could move it")
		}
		focused = true
		done <- struct{}{}
		return nil
	}))
	root := js.Global().Get("Object").New()
	root.Set("querySelector", browserTestFunc(t, func(js.Value, []js.Value) any { return element }))
	document := js.Global().Get("Object").New()
	document.Set("getElementById", browserTestFunc(t, func(js.Value, []js.Value) any { return root }))
	browserTestGlobal(t, "document", document)
	result, err := newBrowserHostReceiver("island-0").Call("Focus", []vm.Value{vm.StringVal("#draft")})
	if err != nil || !result.Truth() {
		t.Fatalf("Focus result/error = %v/%v", result, err)
	}
	// Earlier microtasks represent the remaining event-default work. Focus
	// queued in that phase would run first and be overwritten by the default.
	finishDefault := browserTestFunc(t, func(js.Value, []js.Value) any { defaultFinished = true; return nil })
	js.Global().Get("Promise").Call("resolve").Call("then", finishDefault)
	browserTestAwait(t, done)
	if !focused {
		t.Fatal("focus did not run after the event default")
	}
}

func TestBrowserHostFocusDoesNotTargetReplacementIsland(t *testing.T) {
	focused := 0
	element := js.Global().Get("Object").New()
	element.Set("focus", browserTestFunc(t, func(js.Value, []js.Value) any { focused++; return nil }))
	makeRoot := func() js.Value {
		root := js.Global().Get("Object").New()
		root.Set("querySelector", browserTestFunc(t, func(js.Value, []js.Value) any { return element }))
		return root
	}
	root := makeRoot()
	document := js.Global().Get("Object").New()
	document.Set("getElementById", browserTestFunc(t, func(js.Value, []js.Value) any { return root }))
	browserTestGlobal(t, "document", document)
	result, err := newBrowserHostReceiver("island-0").Call("Focus", []vm.Value{vm.StringVal("#draft")})
	if err != nil || !result.Truth() {
		t.Fatalf("Focus result/error = %v/%v", result, err)
	}
	root = makeRoot()
	done := make(chan struct{}, 1)
	barrier := browserTestFunc(t, func(js.Value, []js.Value) any { done <- struct{}{}; return nil })
	js.Global().Call("setTimeout", barrier, 0)
	browserTestAwait(t, done)
	if focused != 0 {
		t.Fatal("a stale focus effect reached the next island with the same ID")
	}
}
