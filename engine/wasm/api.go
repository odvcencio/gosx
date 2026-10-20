// Package wasm lets a standard Go WebAssembly module register a GoSX engine
// component with the browser bootstrap.
//
// The package is intentionally separate from GoSX's shared island runtime.
// Each module is booted once per exact URL, while its registered factories may
// create any number of independent engine instances. Register every component
// during synchronous startup and keep main alive while its instances are in use.
//
// Context exposes shared signals, navigation, and mounted Scene3D commands.
// Reads and writes are scoped to the exact live engine instance. Cancel the
// context supplied to asynchronous scene/navigation operations and dispose
// subscriptions from the engine Handle; PlayFrames and TransitionCamera then
// release their frame callbacks and pause naturally while the page is hidden.
package wasm

import (
	"encoding/json"
	"errors"
)

var (
	// ErrUnsupported reports use outside a js/wasm build.
	ErrUnsupported = errors.New("gosx engine/wasm is available only on js/wasm")
	// ErrRegistrationClosed reports a registration that was not requested by
	// the currently booting Go-WASM engine module.
	ErrRegistrationClosed = errors.New("Go-WASM engine registration is closed")
)

// Handle owns one mounted engine instance. GoSX calls Dispose exactly once
// when the instance is replaced or the page is disposed.
type Handle interface {
	Dispose()
}

// HandleFunc adapts a function to Handle.
type HandleFunc func()

// Dispose calls f when it is non-nil.
func (f HandleFunc) Dispose() {
	if f != nil {
		f()
	}
}

// Factory creates one independent engine instance for a browser context.
type Factory func(Context) (Handle, error)

// SubscribeSignal decodes a browser shared signal into T before calling handler.
// Malformed values are ignored. Return the subscription as the engine handle,
// or dispose it from your Handle, to release the browser and Go callbacks.
// The current value is not replayed; subscriptions receive subsequent writes.
func SubscribeSignal[T any](ctx Context, name string, handler func(T)) (HandleFunc, error) {
	if handler == nil {
		return nil, errors.New("shared signal handler is required")
	}
	return ctx.SubscribeSignal(name, func(data json.RawMessage) {
		var value T
		if json.Unmarshal(data, &value) == nil {
			handler(value)
		}
	})
}

// ReadSignal decodes the current shared signal. A missing signal returns
// ErrSignalNotFound; malformed values are reported rather than silently coerced.
func ReadSignal[T any](ctx Context, name string) (T, error) {
	var value T
	data, err := ctx.SignalJSON(name)
	if err == nil {
		err = json.Unmarshal(data, &value)
	}
	return value, err
}

// NavigationOptions selects enhanced navigation behavior. The framework uses
// a full document navigation when enhancement is unavailable.
type NavigationOptions struct {
	Replace    bool `json:"replace,omitempty"`
	Force      bool `json:"force,omitempty"`
	Revalidate bool `json:"revalidate,omitempty"`
}

// ErrSignalNotFound reports an unavailable shared signal.
var ErrSignalNotFound = errors.New("shared signal is unavailable")
