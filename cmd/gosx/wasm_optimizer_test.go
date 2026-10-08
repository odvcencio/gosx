package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
				script += "printf '%s' 'partial' > \"$8\"\nprintf '%s\\n' 'fixture optimizer failure' >&2\nexit 7\n"
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
			var diagnostics optimizerWarningWriter
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
			if _, err := os.Stat(input + ".opt"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("optimizer left temporary output: %v", err)
			}
			if fail && diagnostics.writes != 1 {
				t.Fatalf("warning used %d writes, want one", diagnostics.writes)
			}
			if data, err := os.ReadFile(input); err != nil || string(data) != wantInput {
				t.Fatalf("input=%q err=%v, want %q", data, err, wantInput)
			}
		})
	}
}

type optimizerWarningWriter struct {
	bytes.Buffer
	writes int
}

func (w *optimizerWarningWriter) Write(data []byte) (int, error) {
	w.writes++
	return w.Buffer.Write(data)
}

func (w *optimizerWarningWriter) WriteString(data string) (int, error) {
	w.writes++
	return w.Buffer.WriteString(data)
}

func TestOptionalWASMOptimizerMissingWarnsOncePerBuild(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for build := 0; build < 2; build++ {
		var diagnostics optimizerWarningWriter
		optimizer := newOptionalWASMOptimizer(&diagnostics)
		var wg sync.WaitGroup
		for variant := 0; variant < 5; variant++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if optimized, err := optimizer.optimize(fmt.Sprintf("variant-%d.wasm", variant)); err != nil || optimized {
					t.Errorf("optimized=%v err=%v", optimized, err)
				}
			}()
		}
		wg.Wait()
		if diagnostics.writes != 1 || strings.Count(diagnostics.String(), "warning:") != 1 {
			t.Fatalf("build %d: writes=%d warning=%q", build, diagnostics.writes, diagnostics.String())
		}
	}
}
