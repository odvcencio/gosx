//go:build !js || !wasm

package main

// main is a no-op stub so `go build ./...` succeeds on native (non
// js/wasm) toolchains. cross_frame_parse.go is deliberately left without a
// js/wasm build tag so its pure-Go query parser can be unit-tested with
// `go test ./client/wasm` on a native toolchain; that leaves this directory,
// on native builds, containing only cross_frame_parse.go — a package main
// with no func main. The real js/wasm entry point lives in main.go.
func main() {}
