package main

import (
	"context"
	"fmt"
	"io"

	"m31labs.dev/gosx/internal/typeoracle"
)

// runCheckTypes runs the go/types oracle (internal/typeoracle) over the
// .gsx package containing file and prints every diagnostic it found, one
// per line, positioned in the .gsx source the author wrote. It is the
// implementation behind `gosx check --types` (cmdCheck).
//
// runCheck's own strictcheck pass already feeds the same strict projection
// through a real Go compiler (strictcheck.goCheck), so this rarely proves
// a healthy package unhealthy that runCheck alone would have passed; the
// two mostly agree on pass/fail. What this adds is structured, individual
// diagnostics (ir.Diagnostic values, not one joined compiler-output
// string) and, for a caller that imports internal/typeoracle directly
// rather than shelling out to this CLI, a live *types.Package to query --
// see that package's doc comment's "Role" section for the full case.
//
// GOWORK is forced off: a working tree nested under an ancestor directory
// that itself carries a go.work would otherwise have every `go list` call
// typeoracle makes silently resolved against the wrong module (the same
// footgun strictcheck's own checkStrictProject already guards against in
// build.go). A real gosx project checked from its own root is unaffected
// either way.
func runCheckTypes(file string, stderr io.Writer) error {
	session, err := typeoracle.LoadFileWithOptions(context.Background(), file, typeoracle.Options{GOWORK: "off"})
	if err != nil {
		return fmt.Errorf("types: %w", err)
	}
	if len(session.Diagnostics) == 0 {
		fmt.Fprintln(stderr, "types: ok")
		return nil
	}
	fmt.Fprintf(stderr, "types: %d diagnostic(s):\n", len(session.Diagnostics))
	for _, diag := range session.Diagnostics {
		fmt.Fprintf(stderr, "  %s\n", diag.String())
	}
	return fmt.Errorf("types: %d diagnostic(s) found", len(session.Diagnostics))
}
