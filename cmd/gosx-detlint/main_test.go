package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestVetTool builds the tool and runs it through `go vet -vettool` on a
// throwaway module, once with a hazard and once without.
func TestVetTool(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not found")
	}
	dir := t.TempDir()
	tool := filepath.Join(dir, "gosx-detlint")
	build := exec.Command(goBin, "build", "-o", tool, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	mod := filepath.Join(dir, "mod")
	if err := os.MkdirAll(mod, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(mod, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/sim\n\ngo 1.26\n")

	vet := func() (string, error) {
		cmd := exec.Command(goBin, "vet", "-vettool="+tool, "./...")
		cmd.Dir = mod
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	write("sim.go", "//gosx:deterministic\n\npackage sim\n\nfunc F(x, y, z float64) float64 { return x*y + z }\n")
	out, err := vet()
	if err == nil || !strings.Contains(out, "fused-multiply-add") {
		t.Fatalf("want a fused-multiply-add report, got err=%v\n%s", err, out)
	}

	write("sim.go", "//gosx:deterministic\n\npackage sim\n\nfunc F(x, y, z float64) float64 { return float64(x*y) + z }\n")
	if out, err := vet(); err != nil {
		t.Fatalf("clean package reported: %v\n%s", err, out)
	}
}
