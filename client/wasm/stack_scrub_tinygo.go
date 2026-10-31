//go:build js && wasm && tinygo

package main

import (
	"syscall/js"
	"unsafe"
)

// funcOf is js.FuncOf for every function this runtime hands to JavaScript. The
// function scrubs the stack of its goroutine before it returns.
//
// TinyGo starts a goroutine for each call from JavaScript, and gives it a stack
// that is one block of the garbage-collected heap (256 KiB in the runtime GoSX
// builds). The collector scans memory conservatively, and some of the words in
// the program's static data are small integers or packed tables that read as
// addresses inside the heap. Whichever block such a word points into is kept,
// so a dead stack is often kept for good. The frames the call left behind
// still hold pointers to everything the call touched, so each kept stack also
// keeps its whole working set: the batch text, the decoded values and the JSON
// for each signal. Less than a third of the heap was then free after a
// collection, so the heap doubled each time the collector ran, to 1 GiB and
// more, and a collection of such a heap took from 0.1 second to several
// seconds.
//
// Zeroing the part of the stack the call used leaves a kept stack holding only
// itself. In a soak of 2,000 batches of 32.8 KB through __gosx_set_input_batch,
// the scrub took the worst stall from 9 to 13 seconds to 0.2 second, and the
// memory from 2.3 or 3 GiB to 144 MiB, in the full and engine runtimes. Stacks
// stay 256 KiB, so a kept stack still costs its 256 KiB; the direct batch entry
// (batch_direct.go) is what keeps the 20 Hz batches from allocating one.
func funcOf(fn func(this js.Value, args []js.Value) any) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		result := fn(this, args)
		scrubStack()
		return result
	})
}

// scrubStackBytes is how far below the caller scrubStack zeroes. It must stay
// well under the smallest goroutine stack the runtime is built with: TinyGo's
// default is 64 KiB, and cmd/gosx builds with 256 KiB. The frames of a signal
// batch reach about 6 KiB below the callback, so most of this is margin.
const scrubStackBytes = 32 << 10

// currentStackPointer is the function TinyGo's own collector calls to find the
// WebAssembly stack pointer. The stack grows down: everything below the pointer
// belongs to calls that have returned.
//
//go:linkname currentStackPointer tinygo_getCurrentStackPointer
func currentStackPointer() unsafe.Pointer

// scrubStack zeroes the scrubStackBytes below its caller's frame. It has no
// frame of its own that matters, because it calls nothing that keeps data on
// the stack.
//
//go:noinline
func scrubStack() {
	clear(unsafe.Slice((*byte)(unsafe.Add(currentStackPointer(), -scrubStackBytes)), scrubStackBytes))
}
