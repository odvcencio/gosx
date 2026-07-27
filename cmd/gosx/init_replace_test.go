package main

import (
	"os"
	"path/filepath"
	"testing"
)

// plantGoSXLookalike builds the smallest directory that passes the original
// "does this look like GoSX" test: the module line, plus env/ and session/.
// Every case below starts from one of these, so a case that is refused is
// refused by the discriminating check it targets and not by an accident of
// setup.
func plantGoSXLookalike(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	goMod := "module m31labs.dev/gosx\n\ngo 1.26\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	for _, sub := range []string{"env", "session"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	return dir
}

func markAsCheckout(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	return dir
}

// TestCheckoutDetectionRefusesTheModuleCache is the test that matters.
//
// An installed CLI builds from the module cache, so runtime.Caller points
// there. The cache carries a real GoSX go.mod and real env/ and session/
// directories, so it passes every non-discriminating check. Accepting it writes
// the packager's absolute home directory into a stranger's go.mod.
func TestCheckoutDetectionRefusesTheModuleCache(t *testing.T) {
	root := t.TempDir()

	cases := []struct {
		name string
		dir  string
	}{
		{
			// How `go install` actually stores it.
			name: "version suffix in the leaf",
			dir:  filepath.Join(root, "cache", "m31labs.dev", "gosx@v0.36.0"),
		},
		{
			// The suffix can sit above the module root too.
			name: "version suffix in a parent",
			dir:  filepath.Join(root, "pkg", "mod", "example.com@v1.2.3", "gosx"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Mark it as a checkout as well, so only the cache check can
			// reject it.
			dir := markAsCheckout(t, plantGoSXLookalike(t, tc.dir))
			if isGoSXCheckout(dir) {
				t.Errorf("isGoSXCheckout(%q) = true, want false\n"+
					"A module cache path was taken for a working checkout. "+
					"An installed CLI would write this absolute path into a "+
					"scaffolded go.mod.", dir)
			}
		})
	}
}

// TestCheckoutDetectionRefusesGOMODCACHE covers the case where the cache lives
// somewhere without an @ in the path, which GOMODCACHE still names.
func TestCheckoutDetectionRefusesGOMODCACHE(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, "modcache")
	dir := markAsCheckout(t, plantGoSXLookalike(t, filepath.Join(cache, "gosx")))

	t.Setenv("GOMODCACHE", cache)
	if isGoSXCheckout(dir) {
		t.Errorf("isGoSXCheckout(%q) = true with GOMODCACHE=%q, want false", dir, cache)
	}

	// Same directory, no GOMODCACHE: now nothing marks it as the cache and it
	// is accepted. This is what keeps the case above honest — it proves the
	// refusal came from GOMODCACHE and not from the fixture.
	t.Setenv("GOMODCACHE", "")
	if !isGoSXCheckout(dir) {
		t.Errorf("isGoSXCheckout(%q) = false without GOMODCACHE, want true", dir)
	}
}

// TestCheckoutDetectionRefusesAnUnpackedModule covers a plain extracted zip:
// real GoSX contents, no version control.
func TestCheckoutDetectionRefusesAnUnpackedModule(t *testing.T) {
	dir := plantGoSXLookalike(t, filepath.Join(t.TempDir(), "gosx"))
	if isGoSXCheckout(dir) {
		t.Errorf("isGoSXCheckout(%q) = true for a tree with no .git, want false", dir)
	}
}

// TestCheckoutDetectionAcceptsAWorkingTree is the other half. A guard that
// refuses everything would pass all three tests above and silently remove the
// dogfooding path.
func TestCheckoutDetectionAcceptsAWorkingTree(t *testing.T) {
	dir := markAsCheckout(t, plantGoSXLookalike(t, filepath.Join(t.TempDir(), "gosx")))
	if !isGoSXCheckout(dir) {
		t.Errorf("isGoSXCheckout(%q) = false for a working checkout, want true", dir)
	}
}

// TestCheckoutDetectionAcceptsALinkedWorktree pins the .git-as-a-file case. Git
// worktrees are how several agents work in this repository, and a directory
// check instead of an existence check would break them.
func TestCheckoutDetectionAcceptsALinkedWorktree(t *testing.T) {
	dir := plantGoSXLookalike(t, filepath.Join(t.TempDir(), "gosx"))
	gitFile := filepath.Join(dir, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: /elsewhere/.git/worktrees/x\n"), 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}
	if !isGoSXCheckout(dir) {
		t.Errorf("isGoSXCheckout(%q) = false for a linked worktree, want true", dir)
	}
}

// TestCheckoutDetectionRefusesAnUnrelatedModule confirms the module-line check
// still carries its weight.
func TestCheckoutDetectionRefusesAnUnrelatedModule(t *testing.T) {
	dir := markAsCheckout(t, plantGoSXLookalike(t, filepath.Join(t.TempDir(), "other")))
	goMod := "module example.com/other\n\ngo 1.26\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if isGoSXCheckout(dir) {
		t.Errorf("isGoSXCheckout(%q) = true for a different module, want false", dir)
	}
}
