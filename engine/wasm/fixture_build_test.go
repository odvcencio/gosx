//go:build !js || !wasm

package wasm

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStandardGoWASMFixtureBuilds(t *testing.T) {
	out := filepath.Join(t.TempDir(), "fixture.wasm")
	cmd := exec.Command("go", "build", "-o", out, "./testdata/fixture")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build standard-Go WASM fixture: %v\n%s", err, output)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("fixture.wasm is empty")
	}
}
