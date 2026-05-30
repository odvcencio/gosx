//go:build js

package gosx

import "m31labs.dev/gosx/ir"

// Compile is unavailable in the wasm runtime. Engine surfaces are compiled to
// bytecode at build time (on the host) and the runtime only hydrates that
// bytecode, so the .gsx compiler — and its gotreesitter/grammargen dependency,
// which is not TinyGo 32-bit-clean — is excluded from the runtime build via the
// //go:build !js tag on compile.go/grammar.go/etc. This stub exists solely so
// host-shared packages that reference gosx.Compile still link under GOOS=js; it
// must never be reached at runtime (surfaces arrive pre-compiled).
func Compile(source []byte) (*ir.Program, error) {
	panic("gosx.Compile is unavailable in the wasm runtime (engine surfaces are pre-compiled to bytecode)")
}
