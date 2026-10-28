// Package jsreentry finds Go code that runs inside a call from Go into
// JavaScript. It is a test helper for js/wasm packages.
//
// Go's js/wasm runtime keeps every call from JavaScript into Go that is in
// progress on one shared stack (runtime/lock_js.go, runtime.events). A nested
// call, one that arrives while a call from Go into JavaScript is on the stack,
// can put an entry on that stack out of order when another call from JavaScript
// into Go is paused in its allocation, and then the program stops with "fatal
// error: traceback did not unwind completely". One goroutine that nests is
// enough: the paused call can be a timer, an event or a promise callback. See
// package m31labs.dev/gosx/client/jsutil for the mechanism and the measured
// failure rates. The calls that nest are the ones whose JavaScript side runs a
// Go function before it returns: new Promise(executor), dispatchEvent, focus,
// click, abort, or Array.prototype.forEach, for example. A Go function that
// JavaScript runs later, from a microtask, a timer or an animation frame, does
// not nest.
//
// Install patches the wasm_exec.js Go class so that each call from JavaScript
// into Go that starts while another is in progress is counted, with the
// JavaScript stack that led to it. A test then asserts that the count is zero
// for the operation it exercises. The probe works under Node with
// go_js_wasm_exec and in a browser page that loads wasm_exec.js.
//
// In a browser host, a write to os.Stdout is itself a nested entry (see package
// m31labs.dev/gosx/client/jsutil), so a test that runs in a browser should not
// print, or call t.Log with -test.v, between the operation and its assertion.
//
// The JavaScript side of such a test must be native. A Go function that stands
// in for fetch, a thenable or a listener is itself a call from JavaScript into
// Go, so build mocks with eval or Function and keep Go out of them.
package jsreentry
