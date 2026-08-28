package main

import (
	"fmt"
	"io"

	"m31labs.dev/gosx/ir"
)

// printCheckWarnings prints every warning-severity diagnostic (gosx#249)
// to w, one per line, and never affects a caller's exit code -- a warning
// informs; it never fails a build. Both cmdCheck's single-file path and
// checkStrictProject's whole-project build-gate path share this so a
// warning always prints the same way regardless of which one produced it.
func printCheckWarnings(w io.Writer, warnings []ir.Diagnostic) {
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintf(w, "warning: %d check warning(s):\n", len(warnings))
	for _, diag := range warnings {
		fmt.Fprintf(w, "  %s\n", diag.String())
	}
}
