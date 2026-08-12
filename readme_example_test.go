package gosx_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gosx "m31labs.dev/gosx"
	"m31labs.dev/gosx/strictcheck"
)

func TestREADMEFlagshipGSXExampleCompilesAndTypeChecks(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	const fence = "```gsx\n"
	start := strings.Index(string(readme), fence)
	if start < 0 {
		t.Fatal("README has no GSX example")
	}
	start += len(fence)
	end := strings.Index(string(readme[start:]), "\n```")
	if end < 0 {
		t.Fatal("README GSX example has no closing fence")
	}
	source := readme[start : start+end]
	if _, err := gosx.Compile(source); err != nil {
		t.Fatalf("compile README GSX example: %v", err)
	}

	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goMod := "module example.test/readme\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\nreplace m31labs.dev/gosx => " + filepath.ToSlash(root) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	goSum, err := os.ReadFile("go.sum")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), goSum, 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "example.gsx")
	if err := os.WriteFile(path, source, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := strictcheck.CheckFileWithOptions(context.Background(), path, strictcheck.Options{GOFLAGS: "-mod=mod"}); err != nil {
		t.Fatalf("type-check README GSX example: %v", err)
	}
}
