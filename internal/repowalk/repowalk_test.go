package repowalk_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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

// walkRel walks root with repowalk.Walk and returns the sorted, slash
// separated paths of every regular file it visits, relative to root.
func walkRel(t *testing.T, root string) []string {
	t.Helper()
	var got []string
	err := repowalk.Walk(root, func(path string, entry fs.DirEntry) error {
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
	slices.Sort(got)
	return got
}

// dirEntry returns the fs.DirEntry for parent/name, mirroring what
// filepath.WalkDir would report for that entry.
func dirEntry(t *testing.T, parent, name string) fs.DirEntry {
	t.Helper()
	info, err := os.Lstat(filepath.Join(parent, name))
	if err != nil {
		t.Fatal(err)
	}
	return fs.FileInfoToDirEntry(info)
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
	got := walkRel(t, root)
	want := []string{"pkg/ok.go", "sub/data/keep.txt", "sub/tmp/keep.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("visited %v, want %v", got, want)
	}
}

func TestWalkSkipsEverySkipAnywhereName(t *testing.T) {
	root := t.TempDir()
	for _, name := range repowalk.SkipAnywhere {
		writeFile(t, filepath.Join(root, name, "f.go"))
	}
	writeFile(t, filepath.Join(root, "keep", "ok.go"))

	got := walkRel(t, root)
	want := []string{"keep/ok.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("visited %v, want %v", got, want)
	}
}

func TestSkipNeverSkipsRootEvenWithGitMarker(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git"))

	entry := dirEntry(t, filepath.Dir(root), filepath.Base(root))
	skip, err := repowalk.Skip(root, root, entry)
	if err != nil {
		t.Fatal(err)
	}
	if skip {
		t.Fatal("root must never be skipped, even when it contains a .git marker")
	}
}

func TestWalkVisitsRootNamedLikeASkippedDirectory(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "build")
	writeFile(t, filepath.Join(root, "f.go"))

	got := walkRel(t, root)
	want := []string{"f.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("visited %v, want %v", got, want)
	}
}

func TestWalkReportsUnreadableGitProbe(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply when running as root")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(locked, ".git"))
	// 0o444 is readable but not traversable: filepath.WalkDir can still list
	// locked's own entries, so only Skip's Lstat of locked/.git is denied.
	// That isolates the error to Skip's probe instead of a directory-open
	// failure that would satisfy a weaker assertion for the wrong reason.
	if err := os.Chmod(locked, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(locked, 0o755); err != nil {
			t.Errorf("restore mode on %s: %v", locked, err)
		}
	})

	err := repowalk.Walk(root, func(path string, entry fs.DirEntry) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected Walk to report an error for an unreadable .git probe")
	}
	if !strings.Contains(err.Error(), "repowalk: inspect") {
		t.Fatalf("error %q does not come from Skip's .git probe", err)
	}
}

func TestSkipListsArePinned(t *testing.T) {
	wantAnywhere := []string{
		".git", ".worktrees", ".graft", ".canopy", ".gts", ".tiller", ".buckley",
		".claude", "build", "dist", "node_modules",
	}
	if !slices.Equal(repowalk.SkipAnywhere, wantAnywhere) {
		t.Fatalf("SkipAnywhere = %v, want %v", repowalk.SkipAnywhere, wantAnywhere)
	}
	wantAtRoot := []string{"tmp", "data"}
	if !slices.Equal(repowalk.SkipAtRoot, wantAtRoot) {
		t.Fatalf("SkipAtRoot = %v, want %v", repowalk.SkipAtRoot, wantAtRoot)
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

func TestRootFromFindsModuleRootAboveNestedDir(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module m31labs.dev/gosx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(tmp, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := repowalk.RootFrom(nested)
	if err != nil {
		t.Fatal(err)
	}
	if got != tmp {
		t.Fatalf("RootFrom(%s) = %s, want %s", nested, got, tmp)
	}
}

func TestRootFromAcceptsRelativeInput(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module m31labs.dev/gosx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(tmp, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	got, err := repowalk.RootFrom(filepath.FromSlash("../.."))
	if err != nil {
		t.Fatal(err)
	}
	if got != tmp {
		t.Fatalf("RootFrom(%q) = %s, want %s", "../..", got, tmp)
	}
}

func TestRootFromReturnsErrorNamingStartWhenNoModule(t *testing.T) {
	start := t.TempDir()
	_, err := repowalk.RootFrom(start)
	if err == nil {
		t.Fatal("expected an error when no ancestor has the gosx go.mod")
	}
	if !strings.Contains(err.Error(), start) {
		t.Fatalf("error %q does not name the starting directory %s", err, start)
	}
}

func TestRootResolvesSymlinkedWorkingDirectory(t *testing.T) {
	tmp := t.TempDir()
	real := filepath.Join(tmp, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "go.mod"), []byte("module m31labs.dev/gosx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}
	wantReal, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}

	t.Chdir(link)
	t.Setenv("PWD", link)

	got, err := repowalk.Root()
	if err != nil {
		t.Fatal(err)
	}
	if got != wantReal {
		t.Fatalf("Root() = %s, want %s", got, wantReal)
	}
	if err := repowalk.Walk(got, func(path string, entry fs.DirEntry) error {
		return nil
	}); err != nil {
		t.Fatalf("Walk(Root()) failed: %v", err)
	}
}

func TestSkipRootOnlyNames(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"tmp", "data", "sub/tmp", "sub/data"} {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"tmp", "data"} {
		entry := dirEntry(t, root, name)
		skip, err := repowalk.Skip(root, filepath.Join(root, name), entry)
		if err != nil {
			t.Fatal(err)
		}
		if !skip {
			t.Fatalf("%s directly under root must be skipped", name)
		}
	}
	for _, name := range []string{"tmp", "data"} {
		entry := dirEntry(t, filepath.Join(root, "sub"), name)
		skip, err := repowalk.Skip(root, filepath.Join(root, "sub", name), entry)
		if err != nil {
			t.Fatal(err)
		}
		if skip {
			t.Fatalf("sub/%s must not be skipped by the root-only rule", name)
		}
	}
}

func TestSkipNestedGitDirectory(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(nested, ".git"))

	entry := dirEntry(t, root, "nested")
	skip, err := repowalk.Skip(root, nested, entry)
	if err != nil {
		t.Fatal(err)
	}
	if !skip {
		t.Fatal("a directory containing .git must be skipped")
	}
}

func TestWalkRejectsSymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	err := repowalk.Walk(link, func(path string, entry fs.DirEntry) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected Walk to reject a symlinked root")
	}
}

func TestWalkSkipsSymlinkedFilesAndDirectories(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "real.go"))
	realDir := filepath.Join(root, "realdir")
	writeFile(t, filepath.Join(realDir, "inside.go"))

	if err := os.Symlink(filepath.Join(root, "real.go"), filepath.Join(root, "link.go")); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}
	if err := os.Symlink(realDir, filepath.Join(root, "linkdir")); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	got := walkRel(t, root)
	want := []string{"real.go", "realdir/inside.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("visited %v, want %v", got, want)
	}
}

func TestWalkPropagatesFnError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "real.go"))

	wantErr := errors.New("boom")
	err := repowalk.Walk(root, func(path string, entry fs.DirEntry) error {
		if filepath.Base(path) == "real.go" {
			return wantErr
		}
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Walk returned %v, want %v", err, wantErr)
	}
}
