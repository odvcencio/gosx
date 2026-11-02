package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimewasm "m31labs.dev/gosx/client/runtime/wasm"
)

func TestRuntimeSourceCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
	}{
		{"matching", `package wasm; const wasmManifestIdentity = "` + runtimewasm.ManifestIdentity() + `"`, ""},
		{"stale", `package wasm; const wasmManifestIdentity = "old"`, "incompatible ABI"},
		{"missing", `package wasm`, "no readable ABI identity"},
		{"invalid", `package`, "read GoSX runtime contract"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "client", "runtime", "wasm", "abi.go")
			if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte(tc.source), 0644); err != nil {
				t.Fatal(err)
			}
			err := checkRuntimeSourceCompatibility(root)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("compatibility error = %v, want %q", err, tc.want)
			}
		})
	}
	if err := checkRuntimeSourceCompatibility(filepath.Join("..", "..")); err != nil {
		t.Fatalf("current checkout: %v", err)
	}
}
