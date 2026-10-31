//go:build js && wasm && !tinygo

package main

import "syscall/js"

// funcOf is js.FuncOf. Only the TinyGo runtime needs to clear its goroutine
// stacks; see stack_scrub_tinygo.go.
func funcOf(fn func(this js.Value, args []js.Value) any) js.Func {
	return js.FuncOf(fn)
}
