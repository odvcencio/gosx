//go:build !js || !wasm

package jsutil

// This file exists so non-WASM Go builds can import client/jsutil without
// compiling the syscall/js helpers. The exported helpers in promise_js.go
// require a JS runtime and have no meaningful native equivalent. Import this
// package from WASM entry points only.
