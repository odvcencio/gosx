package version

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoGoMod returns the contents of the repository go.mod. The version package
// sits two directories below the module root.
func repoGoMod(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "go.mod")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), "module m31labs.dev/gosx") {
		t.Fatalf("%s is not the GoSX module go.mod", path)
	}
	return string(data)
}

// TestMinGoMatchesGoModDirective pins MinGo to the module's own floor.
//
// Anything generated for a user — the go.mod that `gosx init` writes above all
// — must declare at least what GoSX itself declares. Bump the go directive
// without bumping MinGo and every newly scaffolded project fails to run.
func TestMinGoMatchesGoModDirective(t *testing.T) {
	directive := regexp.MustCompile(`(?m)^go (\d+\.\d+(?:\.\d+)?)$`)
	match := directive.FindStringSubmatch(repoGoMod(t))
	if match == nil {
		t.Fatal("go.mod declares no go directive")
	}
	if match[1] != MinGo {
		t.Errorf("MinGo = %q, go.mod declares go %q\n"+
			"Update MinGo in internal/version/version.go. Everything GoSX\n"+
			"generates for a user inherits this floor.", MinGo, match[1])
	}
}

// TestNumberMatchesCurrent enforces the sync the Number doc comment asks for.
//
// The comment said to keep them together and nothing checked it, so a release
// could ship a tag and a bare semver that disagree.
func TestNumberMatchesCurrent(t *testing.T) {
	if want := strings.TrimPrefix(Current, "v"); Number != want {
		t.Errorf("Number = %q, Current = %q (want Number %q)", Number, Current, want)
	}
	if !strings.HasPrefix(Current, "v") {
		t.Errorf("Current = %q, want a leading %q", Current, "v")
	}
}
