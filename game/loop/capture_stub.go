//go:build !js || !wasm || !gosx_capture

package loop

// Without the gosx_capture tag there is no capture clock.
func newCaptureClock() *captureClock { return nil }
