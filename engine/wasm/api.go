// Package wasm lets a standard Go WebAssembly module register a GoSX engine
// component with the browser bootstrap.
//
// The package is intentionally separate from GoSX's shared island runtime.
// Each module is booted once per exact URL, while its registered factories may
// create any number of independent engine instances. Register every component
// during synchronous startup and keep main alive while its instances are in use.
package wasm

import "errors"

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
