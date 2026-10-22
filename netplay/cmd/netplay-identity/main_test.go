package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for n, c := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func gen(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out := filepath.Join(dir, "identity_gen.go")
	var log bytes.Buffer
	oldwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldwd)
	if err := run(append([]string{"-pkg", "sim", "-o", out}, args...), &log); err != nil {
		t.Fatalf("run: %v\n%s", err, log.String())
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGeneratesCompilableDeterministicFile(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"step.go": "package sim\n", "moves.json": "{}"})
	a := gen(t, dir, "-src", "step.go", "-data", "moves.json", "-det", "v1.2.0", "-go", "go1.26.0")
	if _, err := parser.ParseFile(token.NewFileSet(), "identity_gen.go", a, 0); err != nil {
		t.Fatalf("generated file does not parse: %v\n%s", err, a)
	}
	for _, want := range []string{"Code generated", "package sim", "var SimulationIdentity = [32]byte{", `SimulationGoVersion = "go1.26.0"`, `SimulationDetVersion = "v1.2.0"`} {
		if !strings.Contains(a, want) {
			t.Errorf("missing %q in\n%s", want, a)
		}
	}
	if b := gen(t, dir, "-src", "step.go", "-data", "moves.json", "-det", "v1.2.0", "-go", "go1.26.0"); a != b {
		t.Fatal("two runs differ")
	}
	writeFiles(t, dir, map[string]string{"moves.json": `{"jab":3}`})
	if c := gen(t, dir, "-src", "step.go", "-data", "moves.json", "-det", "v1.2.0", "-go", "go1.26.0"); c == a {
		t.Fatal("editing a data file did not change the identity")
	}
}

func TestRejectsMissingInputs(t *testing.T) {
	dir := t.TempDir()
	var log bytes.Buffer
	if err := run([]string{"-pkg", "sim", "-o", filepath.Join(dir, "x.go")}, &log); err == nil {
		t.Fatal("accepted no -src")
	}
	if err := run([]string{"-pkg", "sim", "-src", filepath.Join(dir, "missing.go"), "-o", filepath.Join(dir, "x.go")}, &log); err == nil {
		t.Fatal("accepted a missing file")
	}
}
