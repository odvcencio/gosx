//go:build tinygo

package gosx

import "m31labs.dev/gosx/ir"

// Compile is unavailable in the TinyGo production runtime. Engine surfaces are
// compiled to bytecode at build time (on the host) and the runtime only
// hydrates that bytecode, so the .gsx compiler — and its
// gotreesitter/grammargen dependency, which is not TinyGo 32-bit-clean — is
// excluded from the TinyGo build via the //go:build !tinygo tag on
// compile.go/grammar.go/etc. (The standard-go wasm test build keeps the real
// compiler: it has 64-bit int, so grammargen compiles, and client/wasm tests
// genuinely compile .gsx.) This stub exists solely so any host-shared code that
// references gosx.Compile still links under TinyGo; it must never be reached at
// runtime (surfaces arrive pre-compiled).
func Compile(source []byte) (*ir.Program, error) {
	panic("gosx.Compile is unavailable in the wasm runtime (engine surfaces are pre-compiled to bytecode)")
}
