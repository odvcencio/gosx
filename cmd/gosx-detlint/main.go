// Command gosx-detlint reports code that can give different bits on js/wasm,
// amd64 and arm64, in packages marked //gosx:deterministic.
//
// Run it standalone:
//
//	gosx-detlint ./physics/...
//
// or as a go vet tool:
//
//	go build -o gosx-detlint ./cmd/gosx-detlint
//	go vet -vettool=$PWD/gosx-detlint ./physics/...
//
// The same analyzer is available as `gosx detlint`. See package
// m31labs.dev/gosx/det/lint for the rules.
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"m31labs.dev/gosx/det/lint"
)

func main() {
	singlechecker.Main(lint.Analyzer)
}
