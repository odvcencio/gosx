package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOptionalWASMOptimizerMissingWarnsAndPreservesInput(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	input := filepath.Join(t.TempDir(), "runtime.wasm")
	if err := os.WriteFile(input, []byte("compiled"), 0600); err != nil {
		t.Fatal(err)
	}
	var diagnostics bytes.Buffer
	optimized, err := optimizeWASMWithWasmOptDiagnostics(input, &diagnostics)
	if err != nil || optimized {
		t.Fatalf("missing optional optimizer: optimized=%v err=%v", optimized, err)
	}
	for _, want := range []string{"runtime.wasm", "not available on PATH", "keeping compiled WASM", "Install Binaryen"} {
		if !strings.Contains(diagnostics.String(), want) {
			t.Fatalf("missing %q in diagnostic: %s", want, &diagnostics)
		}
	}
	if data, err := os.ReadFile(input); err != nil || string(data) != "compiled" {
		t.Fatalf("input changed: %q, %v", data, err)
	}
}

func TestOptionalWASMOptimizerSubprocess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake wasm-opt command requires a POSIX shell")
	}
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			bin := t.TempDir()
			tool := filepath.Join(bin, "wasm-opt")
			script := `#!/bin/sh
if [ "$1 $2 $3 $4 $5" != "-Oz --enable-bulk-memory --enable-nontrapping-float-to-int --strip-debug --strip-producers" ] || [ "$7" != "-o" ]; then
  printf '%s\n' 'unexpected optimizer flags' >&2
  exit 91
fi
`
			if fail {
				script += "printf '%s\\n' 'fixture optimizer failure' >&2\nexit 7\n"
			} else {
				script += "printf '%s' 'optimized' > \"$8\"\n"
			}
			if err := os.WriteFile(tool, []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			input := filepath.Join(t.TempDir(), "runtime.wasm")
			if err := os.WriteFile(input, []byte("compiled"), 0600); err != nil {
				t.Fatal(err)
			}
			var diagnostics bytes.Buffer
			optimized, err := optimizeWASMWithWasmOptDiagnostics(input, &diagnostics)
			if err != nil || optimized == fail {
				t.Fatalf("optimized=%v err=%v diagnostics=%s", optimized, err, &diagnostics)
			}
			wantInput := "optimized"
			if fail {
				wantInput = "compiled"
				for _, want := range []string{tool, "exit status 7", "fixture optimizer failure", "keeping compiled WASM"} {
					if !strings.Contains(diagnostics.String(), want) {
						t.Fatalf("missing %q in diagnostic: %s", want, &diagnostics)
					}
				}
			} else if diagnostics.Len() != 0 {
				t.Fatalf("successful optimization warned: %s", &diagnostics)
			}
			if data, err := os.ReadFile(input); err != nil || string(data) != wantInput {
				t.Fatalf("input=%q err=%v, want %q", data, err, wantInput)
			}
		})
	}
}
