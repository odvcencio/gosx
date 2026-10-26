package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func wasmOutputFixture(label string) []byte {
	data := append([]byte(nil), wasmV1Header...)
	return append(data, append([]byte{0, byte(len(label) + 1), byte(len(label))}, label...)...)
}

func init() {
	mode := os.Getenv("GOSX_TEST_WASM_OPTIMIZER")
	if mode == "" {
		return
	}
	if mode != "no-write" {
		data := []byte("fixture optimizer corruption")
		if mode == "empty" {
			data = nil
		}
		if err := os.WriteFile(os.Args[len(os.Args)-1], data, 0600); err != nil {
			os.Exit(90)
		}
	}
	os.Stderr.WriteString("fixture optimizer diagnostics\n")
	os.Exit(0)
}

func TestWASMOutputOptimizerRejectsInvalidSuccessfulAndStaleOutput(t *testing.T) {
	for _, mode := range []string{"empty", "invalid", "no-write"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("GOSX_TEST_WASM_OPTIMIZER", mode)
			path := filepath.Join(t.TempDir(), "runtime.wasm")
			original := wasmOutputFixture("compiled")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path+".opt", wasmOutputFixture("stale"), 0600); err != nil {
				t.Fatal(err)
			}
			var diagnostics bytes.Buffer
			optimizer := &optionalWASMOptimizer{tool: os.Args[0], diagnostics: &diagnostics}
			optimized, err := optimizer.optimize(path)
			if err != nil || optimized || !strings.Contains(diagnostics.String(), "invalid optimizer output") || !strings.Contains(diagnostics.String(), "fixture optimizer diagnostics") {
				t.Fatalf("invalid optimizer output accepted or diagnostics lost: optimized=%v err=%v log=%s", optimized, err, &diagnostics)
			}
			data, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(data, original) {
				t.Fatalf("original replaced: %x err=%v", data, err)
			}
			if _, err := os.Stat(path + ".opt"); !os.IsNotExist(err) {
				t.Fatalf("owned optimizer temporary output remains: %v", err)
			}
		})
	}
}

func TestWASMOutputFraming(t *testing.T) {
	header := append([]byte(nil), wasmV1Header...)
	for _, tc := range []struct {
		name  string
		data  []byte
		valid bool
	}{
		{"v1-header", header, true},
		{"custom-section", wasmOutputFixture("compiled"), true},
		{"padded-LEB", append(append([]byte(nil), header...), 1, 0x81, 0x80, 0x80, 0x80, 0, 0), true},
		{"data-count-before-code", append(append([]byte(nil), header...), 12, 1, 0, 10, 1, 0, 11, 1, 0), true},
		{"tag-after-memory-before-global", append(append([]byte(nil), header...), 1, 4, 1, 0x60, 0, 0, 5, 1, 0, 13, 3, 1, 0, 0, 6, 1, 0), true},
		{"empty", nil, false},
		{"text", []byte("compiled"), false},
		{"truncated-header", header[:7], false},
		{"wrong-version", []byte{0, 'a', 's', 'm', 2, 0, 0, 0}, false},
		{"truncated-length", append(append([]byte(nil), header...), 1, 0x80), false},
		{"overflow-length", append(append([]byte(nil), header...), 1, 0xff, 0xff, 0xff, 0xff, 0x10), false},
		{"truncated-body", append(append([]byte(nil), header...), 1, 4, 0), false},
		{"duplicate-section", append(append([]byte(nil), header...), 1, 1, 0, 1, 1, 0), false},
		{"wrong-order", append(append([]byte(nil), header...), 3, 1, 0, 1, 1, 0), false},
		{"data-count-after-code", append(append([]byte(nil), header...), 10, 1, 0, 12, 1, 0), false},
		{"tag-after-global", append(append([]byte(nil), header...), 6, 1, 0, 13, 1, 0), false},
		{"unsupported-section", append(append([]byte(nil), header...), 14, 0), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateWASMFraming(tc.data); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

func TestWASMOutputCompilerFixtureProcess(t *testing.T) {
	mode := os.Getenv("GOSX_TEST_WASM_COMPILER")
	if mode == "" {
		return
	}
	path := os.Getenv("GOSX_TEST_WASM_OUTPUT")
	var data []byte
	switch mode {
	case "valid":
		data = wasmOutputFixture("compiled")
	case "invalid":
		data = []byte("compiler fixture corruption")
	case "no-write":
		os.Stderr.WriteString("fixture compiler exited zero without writing\n")
		os.Exit(0)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		os.Exit(90)
	}
	os.Stderr.WriteString("fixture compiler diagnostics\n")
	os.Exit(0)
}

func TestWASMOutputCompilerRejectsInvalidSuccessfulAndStaleOutput(t *testing.T) {
	for _, mode := range []string{"empty", "invalid", "valid", "no-write"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.wasm.tmp")
			if err := os.WriteFile(path, wasmOutputFixture("stale"), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestWASMOutputCompilerFixtureProcess$")
			cmd.Env = append(os.Environ(), "GOSX_TEST_WASM_COMPILER="+mode, "GOSX_TEST_WASM_OUTPUT="+path)
			out, err := runValidatedWASMCompiler(cmd, path)
			if mode == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(data, wasmOutputFixture("compiled")) {
					t.Fatalf("valid compiler output=%x err=%v", data, err)
				}
			} else {
				var invalid *invalidCompilerWASMOutput
				if !errors.As(err, &invalid) || !strings.Contains(err.Error(), "fixture compiler") || !strings.Contains(err.Error(), strings.TrimSpace(string(out))) {
					t.Fatalf("invalid output accepted or diagnostics lost: %v", err)
				}
			}
		})
	}
}
