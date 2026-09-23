//go:build !js || !wasm

package input

// PollGamepads returns an empty snapshot outside a browser WebAssembly runtime.
func PollGamepads() Gamepads { return Gamepads{} }
