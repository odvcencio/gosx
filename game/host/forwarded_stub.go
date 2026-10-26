//go:build !js || !wasm

package host

// NewForwardedInput returns ok false outside a browser engine worker.
func NewForwardedInput() (input *ForwardedInput, ok bool) { return nil, false }

// Close stops listening. It does nothing outside a browser build.
func (f *ForwardedInput) Close() {}
