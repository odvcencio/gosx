package main

import (
	"fmt"
	"os"
	"path/filepath"

	"m31labs.dev/gosx"
)

// writeOutputModuleBoundary keeps copied runtime source out of the app's Go
// package pattern. The deployable output is not a Go application module.
func writeOutputModuleBoundary(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory %s: %w", dir, err)
	}
	const contents = "// Generated output boundary; this is not an application module.\nmodule gosx.invalid/build-output\n\ngo " + gosx.MinGoVersion + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(contents), 0o644); err != nil {
		return fmt.Errorf("write output module boundary: %w", err)
	}
	return nil
}
