package gosx

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The repository is published from github.com/odvcencio/gosx but the module
// declares the vanity path m31labs.dev/gosx. Both spellings are therefore
// correct — in different places. As a URL that a reader clicks, the GitHub form
// is right. As a Go import or install path, it is not: the toolchain refuses it
// outright.
//
//	go: github.com/odvcencio/gosx/cmd/gosx@latest: version constraints conflict:
//	  module declares its path as: m31labs.dev/gosx
//	          but was required as: github.com/odvcencio/gosx
//
// That error came from the first command on the getting-started page, and the
// wrong path had spread to nine files. Nothing caught it because both forms
// look plausible when read.
var (
	wrongModulePath = regexp.MustCompile(`(?:^|[^/a-zA-Z.])github\.com/odvcencio/gosx`)
	repositoryURL   = regexp.MustCompile(`https?://github\.com/odvcencio/gosx`)
)

// skippedDirs are generated, vendored, or version-control trees. dist/ holds
// rendered output of the docs site and is excluded from the repository; editing
// it would not fix the source anyway.
var skippedDirs = map[string]bool{
	".git":         true,
	".gts":         true,
	".claude":      true,
	"node_modules": true,
	"dist":         true,
}

// allowedWrongPath lists occurrences that are deliberate. Keep it short, and
// give every entry a reason.
var allowedWrongPath = map[string]string{
	// Display text inside a compiled program fixture, not an import. The
	// program prints it and a golden test pins the bytes.
	"island/program/fixtures.go": "rendered contact string in a bytecode fixture",
	// The changelog records what past releases said. Rewriting history would
	// misreport them.
	"CHANGELOG.md": "historical release notes",
	// This file. It must quote the wrong path to explain the rule and to
	// reproduce the toolchain error, so it cannot obey its own check. It
	// imports nothing from GoSX, so nothing here can be a real import.
	"module_path_test.go": "quotes the wrong path to document the rule",
}

// TestNoDocumentationUsesTheRepositoryPathAsAnImportPath walks the tree and
// fails on any Go import or install path spelled with the GitHub URL.
//
// It deliberately ignores https:// occurrences. Those are links to the
// repository and there are dozens of them, including the one in the release
// workflow that checks the vanity redirect resolves to exactly that URL.
func TestNoDocumentationUsesTheRepositoryPathAsAnImportPath(t *testing.T) {
	checked := 0
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skippedDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".gsx", ".md", ".mod", ".yml", ".yaml", ".html":
		default:
			return nil
		}
		if reason, ok := allowedWrongPath[filepath.ToSlash(path)]; ok {
			t.Logf("skipping %s: %s", path, reason)
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checked++
		for number, line := range strings.Split(string(data), "\n") {
			// Strip repository URLs first, so a line carrying both a link
			// and an import is still judged on the import.
			bare := repositoryURL.ReplaceAllString(line, "")
			if wrongModulePath.MatchString(bare) {
				t.Errorf("%s:%d uses github.com/odvcencio/gosx as an import path\n"+
					"  %s\n"+
					"The module declares m31labs.dev/gosx. The toolchain rejects "+
					"the GitHub spelling. Use https://github.com/odvcencio/gosx "+
					"only for links a reader clicks.",
					path, number+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	// A guard that reads nothing passes. The tree holds thousands of matching
	// files; a couple of hundred means the walk broke.
	if checked < 200 {
		t.Fatalf("walked only %d files, expected the whole repository", checked)
	}
	t.Logf("checked %d files", checked)
}

// TestModuleDeclaresTheVanityPath is the fact the test above depends on. If
// GoSX ever moves to the GitHub path, this fails first and explains why the
// other test suddenly looks wrong.
func TestModuleDeclaresTheVanityPath(t *testing.T) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if !strings.HasPrefix(string(data), "module m31labs.dev/gosx\n") {
		t.Errorf("go.mod no longer declares module m31labs.dev/gosx; "+
			"TestNoDocumentationUsesTheRepositoryPathAsAnImportPath encodes that path\nfirst line: %q",
			strings.SplitN(string(data), "\n", 2)[0])
	}
}
