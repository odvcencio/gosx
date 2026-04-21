// Package jsutil provides shared syscall/js helpers used across the client
// renderer, bridge, and engine runtimes.
//
// The build-tagged implementations live in *_js.go (js && wasm) and *_stub.go
// (everything else) so this package can be imported from non-WASM code
// without pulling the syscall/js dependency.
package jsutil
