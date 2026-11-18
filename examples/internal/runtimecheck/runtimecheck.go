// Package runtimecheck validates assets used by standalone examples.
package runtimecheck

import (
	"fmt"
	"os"
	"path/filepath"
)

// Check verifies that the example source directory and runtime files exist.
func Check(sourceDir string) error {
	info, err := os.Stat(sourceDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("this example must be run from a checkout of the repository (source directory %s is unavailable)", sourceDir)
	}
	root, err := filepath.Abs(filepath.Join(sourceDir, "../.."))
	if err != nil {
		return err
	}
	for _, name := range []string{"gosx-runtime.wasm", "wasm_exec.js"} {
		file := filepath.Join(root, "build", name)
		if _, err := os.Stat(file); err != nil {
			return fmt.Errorf("runtime file %s: %w; run `go run ./cmd/gosx build-runtime build` from the repository root", file, err)
		}
	}
	return nil
}
