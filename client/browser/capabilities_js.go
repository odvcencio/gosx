//go:build js && wasm

package browser

import (
	"context"
	"fmt"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
	"m31labs.dev/gosx/internal/browserdom"
)

// WriteClipboard starts the permission-sensitive write synchronously, so it
// retains the caller's user gesture. Completion runs after the promise settles.
func WriteClipboard(text string, done func(error)) {
	WriteClipboardContext(context.Background(), text, done)
}

// WriteClipboardContext releases pending Go handlers when ctx is canceled.
func WriteClipboardContext(ctx context.Context, text string, done func(error)) {
	platformOperation(ctx, func() js.Value { return js.Global().Get("navigator").Get("clipboard").Call("writeText", text) }, done)
}

// FullscreenSupported reports the standard document fullscreen capability.
func FullscreenSupported() (supported bool) {
	defer func() {
		if recover() != nil {
			supported = false
		}
	}()
	return js.Global().Get("document").Get("documentElement").Get("requestFullscreen").Type() == js.TypeFunction
}
func InFullscreen() bool                 { return js.Global().Get("document").Get("fullscreenElement").Truthy() }
func RequestFullscreen(done func(error)) { RequestFullscreenContext(context.Background(), done) }
func RequestFullscreenContext(ctx context.Context, done func(error)) {
	platformOperation(ctx, func() js.Value { return js.Global().Get("document").Get("documentElement").Call("requestFullscreen") }, done)
}
func ExitFullscreen(done func(error)) { ExitFullscreenContext(context.Background(), done) }
func ExitFullscreenContext(ctx context.Context, done func(error)) {
	platformOperation(ctx, func() js.Value { return js.Global().Get("document").Call("exitFullscreen") }, done)
}

func platformOperation(ctx context.Context, invoke func() js.Value, done func(error)) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		if done != nil {
			done(err)
		}
		return
	}
	defer func() {
		if value := recover(); value != nil && done != nil {
			done(fmt.Errorf("browser operation: %v", value))
		}
	}()
	promise := invoke()
	if !promise.Truthy() || promise.Get("then").Type() != js.TypeFunction {
		if done != nil {
			done(nil)
		}
		return
	}
	go func() {
		_, err := jsutil.AwaitPromiseContext(ctx, promise)
		if done != nil {
			done(err)
		}
	}()
}

// OnNativeMessage listens to the optional native WebView transport. Missing
// native hosts return an inert listener; browser-only pages need no shim.
func OnNativeMessage(fn func(string)) *Listener {
	chrome := js.Global().Get("chrome")
	if !chrome.Truthy() || fn == nil {
		return nil
	}
	view := chrome.Get("webview")
	if !view.Truthy() || view.Get("addEventListener").Type() != js.TypeFunction {
		return nil
	}
	return browserdom.Listen(view, "message", func(event Event) { fn(event.DataString()) })
}

// NativePostJSON sends already encoded data to the optional native host.
func NativePostJSON(data []byte) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("browser native message: %v", value)
		}
	}()
	chrome := js.Global().Get("chrome")
	if !chrome.Truthy() || !chrome.Get("webview").Truthy() {
		return nil
	}
	chrome.Get("webview").Call("postMessage", string(data))
	return nil
}

// StartupString reads a host-provided string configuration value. Missing or
// non-string values return empty; no methods on the host object are invoked.
func StartupString(name string) string {
	value := js.Global().Get(name)
	if value.Type() != js.TypeString {
		return ""
	}
	return value.String()
}

// Vibrate requests a duration in milliseconds. Unsupported devices return false.
func Vibrate(milliseconds int) (accepted bool) {
	defer func() {
		if recover() != nil {
			accepted = false
		}
	}()
	nav := js.Global().Get("navigator")
	return nav.Truthy() && nav.Get("vibrate").Type() == js.TypeFunction && nav.Call("vibrate", max(0, milliseconds)).Truthy()
}

// DispatchError reports a synthetic browser error through the normal window
// error event path. It is intended for integration diagnostics.
func DispatchError(message, filename string) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("browser error event: %v", value)
		}
	}()
	event := js.Global().Get("ErrorEvent").New("error", map[string]any{"message": message, "filename": filename})
	js.Global().Call("dispatchEvent", event)
	return nil
}

// HardwareConcurrency reports the browser's advertised logical core count.
// Zero means it did not publish a useful count.
func HardwareConcurrency() int {
	nav := js.Global().Get("navigator")
	if !nav.Truthy() {
		return 0
	}
	value := nav.Get("hardwareConcurrency")
	if value.Type() != js.TypeNumber {
		return 0
	}
	return max(0, value.Int())
}
