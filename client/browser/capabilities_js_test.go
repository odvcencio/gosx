//go:build js && wasm

package browser

import (
	"context"
	"errors"
	"syscall/js"
	"testing"
	"time"
)

func awaitBrowserCompletion(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("browser completion did not arrive")
		return nil
	}
}

func TestClipboardStartsSynchronouslyAndCompletesAfterPromise(t *testing.T) {
	g := js.Global()
	navigator := g.Get("Object").New()
	clipboard := g.Get("Object").New()
	navigator.Set("clipboard", clipboard)
	replaceBrowserGlobal(t, "navigator", navigator)
	calls := 0
	browserMethod(t, clipboard, "writeText", func(receiver js.Value, args []js.Value) any {
		calls++
		if !receiver.Equal(clipboard) || len(args) != 1 || args[0].String() != "room ABC123" {
			t.Error("clipboard call changed")
		}
		return g.Get("Promise").Call("resolve")
	})
	done := make(chan error, 1)
	WriteClipboard("room ABC123", func(err error) { done <- err })
	if calls != 1 {
		t.Fatal("permission-sensitive operation lost synchronous user gesture")
	}
	if err := awaitBrowserCompletion(t, done); err != nil {
		t.Fatal(err)
	}
	clipboard.Set("writeText", g.Get("Function").New("throw new Error('clipboard denied')"))
	WriteClipboard("room", func(err error) { done <- err })
	if err := awaitBrowserCompletion(t, done); err == nil {
		t.Fatal("clipboard denial ignored")
	}
}

func TestCapabilityCancellationAllowsSafeLatePromiseRejection(t *testing.T) {
	g := js.Global()
	navigator := g.Get("Object").New()
	clipboard := g.Get("Object").New()
	navigator.Set("clipboard", clipboard)
	replaceBrowserGlobal(t, "navigator", navigator)
	var reject js.Value
	executor := js.FuncOf(func(_ js.Value, args []js.Value) any { reject = args[1]; return nil })
	defer executor.Release()
	promise := g.Get("Promise").New(executor)
	calls := 0
	browserMethod(t, clipboard, "writeText", func(js.Value, []js.Value) any { calls++; return promise })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	WriteClipboardContext(ctx, "room", func(err error) { done <- err })
	if calls != 1 {
		t.Fatal("clipboard start deferred")
	}
	cancel()
	if err := awaitBrowserCompletion(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	reject.Invoke("late native failure")
	WriteClipboardContext(ctx, "room", func(err error) { done <- err })
	if err := awaitBrowserCompletion(t, done); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("canceled operation invoked native host", err, calls)
	}
}

func TestFullscreenPreservesGestureAndNativeMessageLifecycle(t *testing.T) {
	g := js.Global()
	document := g.Get("Object").New()
	root := g.Get("Object").New()
	document.Set("documentElement", root)
	replaceBrowserGlobal(t, "document", document)
	requests, exits := 0, 0
	browserMethod(t, root, "requestFullscreen", func(js.Value, []js.Value) any {
		requests++
		document.Set("fullscreenElement", root)
		return g.Get("Promise").Call("resolve")
	})
	browserMethod(t, document, "exitFullscreen", func(js.Value, []js.Value) any {
		exits++
		document.Set("fullscreenElement", js.Null())
		return js.Undefined()
	})
	if !FullscreenSupported() || InFullscreen() {
		t.Fatal("fullscreen capabilities")
	}
	done := make(chan error, 1)
	RequestFullscreen(func(err error) { done <- err })
	if requests != 1 || !InFullscreen() {
		t.Fatal("fullscreen gesture deferred")
	}
	if err := awaitBrowserCompletion(t, done); err != nil {
		t.Fatal(err)
	}
	ExitFullscreen(func(err error) { done <- err })
	if err := awaitBrowserCompletion(t, done); err != nil || exits != 1 || InFullscreen() {
		t.Fatal("fullscreen exit", err)
	}
	view := g.Get("Object").New()
	chrome := g.Get("Object").New()
	chrome.Set("webview", view)
	replaceBrowserGlobal(t, "chrome", chrome)
	var listener js.Value
	removes := 0
	browserMethod(t, view, "addEventListener", func(_ js.Value, args []js.Value) any {
		if args[0].String() != "message" {
			t.Error("native listener event")
		}
		listener = args[1]
		return nil
	})
	browserMethod(t, view, "removeEventListener", func(_ js.Value, args []js.Value) any {
		removes++
		if !args[1].Equal(listener) {
			t.Error("native listener identity")
		}
		return nil
	})
	messages := []string{}
	subscription := OnNativeMessage(func(data string) { messages = append(messages, data) })
	listener.Invoke(map[string]any{"data": `{"reload":true}`})
	listener.Invoke(map[string]any{"data": 42})
	if len(messages) != 2 || messages[0] != `{"reload":true}` || messages[1] != "" {
		t.Fatal("native payload coercion", messages)
	}
	subscription.Dispose()
	subscription.Dispose()
	if removes != 1 {
		t.Fatal("native listener leaked")
	}
	post := ""
	browserMethod(t, view, "postMessage", func(_ js.Value, args []js.Value) any { post = args[0].String(); return nil })
	if err := NativePostJSON([]byte(`{"fault":"gpu"}`)); err != nil || post != `{"fault":"gpu"}` {
		t.Fatal("native post", err, post)
	}
}
