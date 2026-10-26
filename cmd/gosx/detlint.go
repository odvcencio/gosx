package main

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/tools/go/analysis/singlechecker"

	"m31labs.dev/gosx/det/lint"
)

func detlintUsage(w io.Writer) {
	fmt.Fprintf(w, `gosx detlint - Check deterministic packages for cross-platform hazards

Usage:
  gosx detlint [flags] <package pattern>...

A package opts in with a //gosx:deterministic comment before its package
clause. gosx detlint then reports unrounded float products (fused multiply-add),
math functions with platform-specific results, map ranges, wall-clock reads,
the global random source, go statements and select statements.

Flags:
  -fix  Apply suggested fixes (wraps float products in an explicit conversion)

Silence one rule on one line with a comment, with a reason:
  //gosx:allow map-range summing integers is order independent

The same analyzer runs under go vet:
  go build -o gosx-detlint ./cmd/gosx-detlint
  go vet -vettool=$PWD/gosx-detlint ./physics/...

`)
}

// cmdDetlint hands the rest of the command line to the analysis driver. The
// driver parses flags, loads and type-checks the packages, prints reports,
// and exits with a nonzero status if it found any.
func cmdDetlint() {
	if len(os.Args) < 3 {
		detlintUsage(os.Stderr)
		os.Exit(1)
	}
	os.Args = append([]string{"gosx detlint"}, os.Args[2:]...)
	singlechecker.Main(lint.Analyzer)
}
