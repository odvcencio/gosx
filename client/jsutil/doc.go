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
// A nested entry can stop the program, however many goroutines make them. Go's
// js/wasm runtime keeps the calls from JavaScript into Go that are in progress
// on one global stack, runtime.events (runtime/lock_js.go, handleEvent). Each
// entry allocates its stack record before it pushes it. If that allocation
// pauses the entry, as it does when it starts the first garbage collection of
// the process, another goroutine can run first. A nested entry that goroutine
// makes goes onto the stack first, and the paused entry then goes on top of it,
// against the order of the calls on the JavaScript stack. The runtime resumes
// the wrong goroutine and the program stops, usually with "fatal error:
// traceback did not unwind completely" or "RuntimeError: memory access out of
// bounds".
//
// The paused entry can be any call from JavaScript into Go: a timer, an event
// or a promise callback. The goroutine that nests can be the only one that
// calls JavaScript. In a test program whose only
// nesting goroutine looped over new Promise(js.FuncOf(...)) with 20 µs sleeps,
// 19 of 256 runs stopped on go1.26.0 and 14 of 256 on go1.26.10. The same loop
// over dispatchEvent with a Go listener stopped 46 of 256 runs on go1.26.0 and
// 31 of 256 on go1.26.10. The same programs with a native executor or listener
// stopped none in 256 runs. Keeping nested calls on one goroutine therefore
// does not make them safe. The failure needs the first collection to start
// inside the push, so it depends on the build and on timing: a browser
// application that nested often stopped in about 6 of 400 page loads, far less
// than the stress programs. A program can pass its tests and still fail for a
// user.
//
// The helpers in this package never run Go inside a call into JavaScript:
//
//   - A Go callback that waits only signals a channel, and JavaScript runs it
//     later, as a microtask, after the call that attached it has returned
//     (AwaitPromise, AwaitPromiseContext).
//   - A pending Promise comes from NewPromiseWithResolvers, which hands the
//     constructor a native function, not from new Promise(js.FuncOf(...)).
//   - A listener that the browser can run inside a call that Go makes, such as
//     the abort listener of the document signal, is the native resolve function
//     of such a promise, and a goroutine waits on the promise (DocumentContext).
//   - A Go function that JavaScript may call while Go is inside a call, such as
//     an event listener or a signal subscriber, is a Callback, and Go makes the
//     call with Call. The Callback only records the JavaScript call, and Call
//     runs it after the JavaScript call has returned.
//
// A function made with js.FuncOf is not deferred. Code that hands one to a
// JavaScript call that runs it before the call returns, or that calls
// dispatchEvent, focus or click with js.Value.Call while a Go listener is
// attached, still nests and still has the problem above. Use Callback and Call
// there, or call the Go consumers directly and dispatch the DOM event only for
// page scripts.
//
// Go's standard library nests in a browser too. A write to os.Stdout or
// os.Stderr, which includes fmt.Println and the log package, goes through
// syscall.fsCall. That gives the fs.write function of the browser's
// wasm_exec.js a Go callback, and fs.write calls it before it returns. In
// Chrome for Testing 156 with the wasm_exec.js of go1.26.0, the probe in
// internal/jsreentry counts one nested entry for each write; the Node host
// (wasm_exec_node.js) calls the callback later and shows none. Package
// m31labs.dev/gosx/client/browser has LogError, which calls console.error
// without a Go callback.
package jsutil
