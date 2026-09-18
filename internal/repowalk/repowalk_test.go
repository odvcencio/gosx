package repowalk_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/repowalk"
)

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWalkSkipsGeneratedAndNestedTrees(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"pkg/ok.go",
		".worktrees/w/x.go",
		"nested/.git", // a file marks a worktree
		"nested/y.go",
		"build/z.go",
		"dist/d.go",
		"node_modules/a.js",
		"tmp/t.go",
		"data/d.txt",
		"sub/tmp/keep.go",
		"sub/data/keep.txt",
	} {
		writeFile(t, filepath.Join(root, filepath.FromSlash(rel)))
	}
	var got []string
	err := repowalk.Walk(root, func(path string, entry os.DirEntry) error {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		got = append(got, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{"pkg/ok.go", "sub/data/keep.txt", "sub/tmp/keep.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("visited %v, want %v", got, want)
	}
}

func TestRootFindsModuleRoot(t *testing.T) {
	root, err := repowalk.Root()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "module m31labs.dev/gosx\n") {
		t.Fatalf("go.mod at %s is not the gosx module", root)
	}
}

func TestSkipRootOnlyNames(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"tmp", "sub/tmp"} {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var rootTmp os.DirEntry
	for _, entry := range entries {
		if entry.Name() == "tmp" {
			rootTmp = entry
		}
	}
	if rootTmp == nil {
		t.Fatal("tmp entry not found under root")
	}
	if !repowalk.Skip(root, filepath.Join(root, "tmp"), rootTmp) {
		t.Fatal("tmp directly under root must be skipped")
	}

	subEntries, err := os.ReadDir(filepath.Join(root, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	var subTmp os.DirEntry
	for _, entry := range subEntries {
		if entry.Name() == "tmp" {
			subTmp = entry
		}
	}
	if subTmp == nil {
		t.Fatal("tmp entry not found under sub")
	}
	if repowalk.Skip(root, filepath.Join(root, "sub", "tmp"), subTmp) {
		t.Fatal("sub/tmp must not be skipped by the root-only rule")
	}
}

func TestSkipNestedGitDirectory(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(nested, ".git"))

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var nestedEntry os.DirEntry
	for _, entry := range entries {
		if entry.Name() == "nested" {
			nestedEntry = entry
		}
	}
	if nestedEntry == nil {
		t.Fatal("nested entry not found under root")
	}
	if !repowalk.Skip(root, nested, nestedEntry) {
		t.Fatal("a directory containing .git must be skipped")
	}

	rootEntry := &rootDirEntry{name: filepath.Base(root)}
	if repowalk.Skip(root, root, rootEntry) {
		t.Fatal("root itself must never be skipped")
	}
}

// rootDirEntry is a minimal os.DirEntry used to exercise Skip against the
// root path itself, since filepath.WalkDir never calls fn for the root's own
// parent listing.
type rootDirEntry struct{ name string }

func (r *rootDirEntry) Name() string               { return r.name }
func (r *rootDirEntry) IsDir() bool                { return true }
func (r *rootDirEntry) Type() os.FileMode          { return os.ModeDir }
func (r *rootDirEntry) Info() (os.FileInfo, error) { return os.Lstat(r.name) }
