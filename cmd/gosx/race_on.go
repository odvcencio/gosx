//go:build race

package main

// raceDetectorEnabled reports whether the binary was built with the Go
// race detector (-race). Tests that shell out to a long-running
// `go build` / `go test ./...` / TinyGo subprocess use this to skip
// under -race: the heavy work happens in the child process, which the
// parent's race instrumentation cannot observe, so running them under
// -race only blows the package timeout for zero added coverage. The
// same tests still run fully in the non-race `make test` gate.
const raceDetectorEnabled = true
