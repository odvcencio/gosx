//go:build js && wasm

package jsutil

import (
	"context"
	"errors"
	"syscall/js"
	"testing"
)

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
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("native navigation did not cancel request")
	}
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
	release()                // exact-once callback disposal
	controller.Call("abort") // no callback into a released Go function

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
