package gosx_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildArtifactsAreExcludedFromPackageDiscovery(t *testing.T) {
	boundary, err := os.ReadFile(filepath.Join("build", "go.mod"))
	if err != nil {
		// Go module archives omit nested modules, including this repository-only
		// boundary. A checkout must contain it; a downloaded module need not.
		if os.IsNotExist(err) {
			if _, gitErr := os.Stat(".git"); os.IsNotExist(gitErr) {
				t.Skip("build artifact boundary is only distributed in the repository")
			}
		}
		t.Fatal(err)
	}
	dir := t.TempDir()
	files := map[string][]byte{
		"go.mod":                          []byte("module example.test/project\n\ngo 1.26\n"),
		"source/source.go":                []byte("package source\n"),
		"build/go.mod":                    boundary,
		"build/validation/a/first.go":     []byte("package first\n"),
		"build/validation/a/second.go":    []byte("package second\n"),
		"build/validation/b/generated.go": []byte("package generated\n"),
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.Command("go", "list", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list ./...: %v\n%s", err, output)
	}
	if got, want := strings.TrimSpace(string(output)), "example.test/project/source"; got != want {
		t.Fatalf("discovered packages = %q, want %q", got, want)
	}
}
