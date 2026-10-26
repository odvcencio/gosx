//go:build !js || !wasm

package host

import "testing"

func TestNewForwardedInputIsUnavailableOffTheBrowser(t *testing.T) {
	if input, ok := NewForwardedInput(); ok || input != nil {
		t.Fatal("forwarded input must be unavailable outside a browser engine worker")
	}
	var none *ForwardedInput
	none.Close() // a nil value is safe to close
}
