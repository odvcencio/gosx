package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutputModuleBoundaryExcludesGeneratedPackages(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":              "module example.test/app\n\ngo 1.26\n",
		"app/page.go":         "package app\n",
		"dist/app/first.go":   "package first\n",
		"dist/app/second.go":  "package second\n",
		"dist/content/raw.go": "package content\n",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list := func() (string, error) {
		cmd := exec.Command("go", "list", "-buildvcs=false", "./...")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOWORK=off")
		output, err := cmd.CombinedOutput()
		return string(output), err
	}
	if output, err := list(); err == nil || !strings.Contains(output, "found packages") {
		t.Fatalf("unisolated output should break discovery: output=%q err=%v", output, err)
	}
	if err := writeOutputModuleBoundary(filepath.Join(root, "dist")); err != nil {
		t.Fatal(err)
	}
	output, err := list()
	if err != nil || strings.TrimSpace(output) != "example.test/app/app" {
		t.Fatalf("isolated discovery output=%q err=%v", output, err)
	}
	if _, err := os.Stat(filepath.Join(root, "dist", "app", "second.go")); err != nil {
		t.Fatalf("boundary changed the generated content: %v", err)
	}
}
