//go:build tinygo && wasm

package signal

// TinyGo's browser runtime uses the documented shared reactive scope.
// runtime.Stack returns no goroutine header on this target, so skip its
// scratch-buffer pool and the identity lookup that would always return zero.
func goroutineID() uint64 { return 0 }
