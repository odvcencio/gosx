// Package jsutil provides shared syscall/js helpers used across the client
// renderer, bridge, and engine runtimes.
//
// The build-tagged implementations live in *_js.go (js && wasm) and *_stub.go
// (everything else) so this package can be imported from non-WASM code
// without pulling the syscall/js dependency.
//
// # Keep Go out of calls into JavaScript
//
// Some JavaScript calls run a callback before they return: new Promise(executor)
// runs its executor, dispatchEvent, focus, click and AbortController.abort run
// their listeners, and Array.prototype.forEach runs its function. When that
// callback is a Go function (js.FuncOf), the Go runtime enters Go while the call
// from Go into JavaScript is still on the stack. This is a nested entry.
//
// Go's js/wasm runtime keeps the entries that are in progress on one global
// stack, runtime.events (runtime/lock_js.go, handleEvent). The stack assumes
// that entries from different goroutines never interleave. One goroutine that
// nests is safe. If two goroutines are inside nested entries at once, a push
// can be lost, and the runtime then resumes the wrong goroutine when the nested
// call returns. The program stops with "fatal error: traceback did not unwind
// completely", or runs the end of a function on the wrong stack.
//
// The helpers here follow two rules, and code that calls into JavaScript from
// several goroutines should follow them too:
//
//   - A Go callback only signals a channel, and JavaScript runs it later, as a
//     microtask, after the call that attached it has returned (AwaitPromise,
//     AwaitPromiseContext).
//   - A pending Promise comes from NewPromiseWithResolvers, which hands the
//     constructor a native function, not from new Promise(js.FuncOf(...)).
//
// Calls that are documented to run listeners at once, such as dispatchEvent,
// still nest when Go listeners exist. Keep such calls on one goroutine, or call
// the Go consumers directly and dispatch the DOM event only for page scripts.
package jsutil
