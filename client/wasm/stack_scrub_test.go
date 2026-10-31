//go:build js && wasm

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every function the runtime hands to JavaScript must come from funcOf, which
// clears the goroutine stack when the call is over (stack_scrub_tinygo.go says
// why). A js.FuncOf call that slips in keeps its dead stacks and everything their
// frames pointed to for as long as the page lives.
func TestRuntimeFunctionsComeFromFuncOf(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v (%d files)", err, len(files))
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || file == "stack_scrub_other.go" || file == "stack_scrub_tinygo.go" {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if strings.Contains(code, "js.FuncOf(") {
				t.Errorf("%s:%d calls js.FuncOf; use funcOf so the callback clears its stack", file, i+1)
			}
		}
	}
}
