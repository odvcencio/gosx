package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

type scaffoldUnreadableFS struct {
	fs.FS
	err error
}

func (s scaffoldUnreadableFS) Open(name string) (fs.File, error) {
	if name == "private" {
		return nil, &fs.PathError{Op: "open", Path: name, Err: s.err}
	}
	return s.FS.Open(name)
}

func TestScaffoldGoFileScanDirectoryErrors(t *testing.T) {
	for _, denied := range []error{fs.ErrPermission, fs.ErrInvalid} {
		t.Run(denied.Error(), func(t *testing.T) {
			filesystem := scaffoldUnreadableFS{
				FS:  fstest.MapFS{"private/package.go": &fstest.MapFile{Data: []byte("package private\n")}},
				err: denied,
			}
			existingGo, err := scaffoldHasGoFiles(filesystem)
			if errors.Is(denied, fs.ErrPermission) {
				if err != nil || !existingGo {
					t.Fatalf("permission error must request final tidy: existing=%v err=%v", existingGo, err)
				}
			} else if !errors.Is(err, denied) {
				t.Fatalf("unexpected filesystem error was suppressed: %v", err)
			}
		})
	}
}

func TestInitSkipsUnreadableDirectory(t *testing.T) {
	dir := t.TempDir()
	private := filepath.Join(dir, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(private, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(private, 0700) })
	if _, err := os.ReadDir(private); !errors.Is(err, fs.ErrPermission) {
		t.Skip("filesystem does not enforce directory permission bits")
	}
	output, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	oldStderr := os.Stderr
	os.Stderr = output
	defer func() { os.Stderr = oldStderr }()
	if err := RunInit(dir, "example.com/app", "app"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, output.Name()); !strings.Contains(got, "go mod tidy") {
		t.Fatalf("missing final tidy reminder: %s", got)
	}
	if got := readFile(t, filepath.Join(dir, "go.mod")); !strings.Contains(got, "module example.com/app") {
		t.Fatalf("scaffold was not published: %s", got)
	}
}

func TestInitPreflightsEveryOutput(t *testing.T) {
	for _, name := range []string{"public/styles.css", "modules/modules.go", "go.sum"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte("caller content"), 0644); err != nil {
				t.Fatal(err)
			}
			files, err := scaffoldFilesForTemplate("example.com/app", "app")
			if err != nil {
				t.Fatal(err)
			}
			_, err = createScaffold(dir, "example.com/app", files, func(string) error {
				t.Fatal("tidy ran despite a pre-existing conflict")
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("wanted conflict for %s, got %v", name, err)
			}
			if got := readFile(t, target); got != "caller content" {
				t.Fatalf("caller file changed: %q", got)
			}
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("preflight wrote go.mod: %v", err)
			}
		})
	}
}

func TestInitRejectsSymlinkParentsAndDanglingTargets(t *testing.T) {
	for _, target := range []string{"app", "modules", "public/styles.css", "go.sum"} {
		t.Run(target, func(t *testing.T) {
			dir := t.TempDir()
			outside := t.TempDir()
			name := filepath.Join(dir, target)
			if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
				t.Fatal(err)
			}
			linkTarget := outside
			if strings.Contains(target, ".") {
				linkTarget = filepath.Join(outside, "absent")
			}
			if err := os.Symlink(linkTarget, name); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if err := RunInit(dir, "example.com/app", "app"); err == nil {
				t.Fatal("init accepted a symlink")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("init wrote outside destination: %v, %v", entries, err)
			}
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("init wrote go.mod before rejecting symlink: %v", err)
			}
		})
	}
}

func TestInitAcceptsSymlinkRoot(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	files := []scaffoldFile{{"go.mod", "module example.com/app\n"}, {"app/page.gsx", "package app\n"}}
	if tidyErr, err := createScaffold(link, "example.com/app", files, func(string) error { return nil }); err != nil || tidyErr != nil {
		t.Fatalf("init through symlink: tidy=%v err=%v", tidyErr, err)
	}
	for _, file := range files {
		if got := readFile(t, filepath.Join(target, filepath.FromSlash(file.Path))); got != file.Contents {
			t.Fatalf("%s = %q", file.Path, got)
		}
	}
}

func TestInitTidyRunsOnlyInStaging(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			dir := t.TempDir()
			files, err := scaffoldFilesForTemplate("example.com/app", "app")
			if err != nil {
				t.Fatal(err)
			}
			originalMod := files[0].Contents
			var stageDir string
			tidyErr, err := createScaffold(dir, "example.com/app", files, func(stage string) error {
				stageDir = stage
				if stage == dir {
					t.Fatal("tidy ran in the destination")
				}
				if _, err := os.Stat(filepath.Join(dir, "go.mod")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("files published before tidy completed: %v", err)
				}
				modules := readFile(t, filepath.Join(stage, "modules/modules.go"))
				if !strings.Contains(modules, `// Code generated by gosx.`) || !strings.Contains(modules, `"example.com/app/app/stack"`) {
					t.Fatalf("modules not generated before tidy: %s", modules)
				}
				for _, name := range []string{"go.mod", "go.sum", "unexpected.txt"} {
					if err := os.WriteFile(filepath.Join(stage, name), []byte("tidy output"), 0644); err != nil {
						t.Fatal(err)
					}
				}
				if fail {
					return errors.New("offline")
				}
				return nil
			})
			if err != nil || (tidyErr != nil) != fail {
				t.Fatalf("createScaffold: tidy=%v, err=%v", tidyErr, err)
			}
			want := "tidy output"
			if fail {
				want = originalMod
			}
			if got := readFile(t, filepath.Join(dir, "go.mod")); got != want {
				t.Fatalf("wrong go.mod after tidy: %q", got)
			}
			_, sumErr := os.Stat(filepath.Join(dir, "go.sum"))
			if fail && !errors.Is(sumErr, os.ErrNotExist) || !fail && sumErr != nil {
				t.Fatalf("wrong go.sum after tidy: %v", sumErr)
			}
			if _, err := os.Stat(filepath.Join(dir, "unexpected.txt")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("copied unexpected tidy output: %v", err)
			}
			if _, err := os.Stat(stageDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("staging directory was not cleaned: %v", err)
			}
		})
	}
}

func TestInitPreservesExistingRouteRegistration(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "app", "custom", "page.server.go")
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		t.Fatal(err)
	}
	const content = "package custom\n// caller-owned route module\n"
	if err := os.WriteFile(name, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	files, err := scaffoldFilesForTemplate("example.com/app", "app")
	if err != nil {
		t.Fatal(err)
	}
	tidyErr, err := createScaffold(dir, "example.com/app", files, func(string) error {
		t.Fatal("tidy must not try to download an existing local route absent from staging")
		return nil
	})
	if err != nil || tidyErr == nil {
		t.Fatalf("expected complete scaffold with a tidy reminder: tidy=%v err=%v", tidyErr, err)
	}
	if got := readFile(t, name); got != content {
		t.Fatalf("existing route changed: %q", got)
	}
	modules := readFile(t, filepath.Join(dir, "modules", "modules.go"))
	if !strings.Contains(modules, `"example.com/app/app/custom"`) || !strings.Contains(modules, `"example.com/app/app/stack"`) {
		t.Fatalf("missing existing or scaffold route registration: %s", modules)
	}
}

func TestScaffoldPublicationPreservesConcurrentFiles(t *testing.T) {
	for _, kind := range []string{"file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			root := openScaffoldTestRoot(t, dir)
			err := publishScaffold(root, []scaffoldFile{{"first", "framework"}, {"last", "framework"}}, func(i int) error {
				if i != 1 {
					return nil
				}
				if kind == "symlink" {
					if err := os.Symlink("caller-target", filepath.Join(dir, "last")); err != nil {
						t.Skipf("symlinks unavailable: %v", err)
					}
					return nil
				}
				return os.WriteFile(filepath.Join(dir, "last"), []byte("caller"), 0644)
			})
			if err == nil {
				t.Fatal("publication overwrote a concurrently created destination")
			}
			if _, err := os.Stat(filepath.Join(dir, "first")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("did not roll back earlier publication: %v", err)
			}
			if kind == "file" {
				if got := readFile(t, filepath.Join(dir, "last")); got != "caller" {
					t.Fatalf("overwrote concurrent file: %q", got)
				}
			} else if link, err := os.Readlink(filepath.Join(dir, "last")); err != nil || link != "caller-target" {
				t.Fatalf("overwrote concurrent symlink: %s, %v", link, err)
			}
		})
	}
}

func TestScaffoldRollbackPreservesCallerEdits(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "in-place edit", true: "replacement with same bytes"}[replacement], func(t *testing.T) {
			dir := t.TempDir()
			root := openScaffoldTestRoot(t, dir)
			content := "edited"
			if replacement {
				content = "framework"
			}
			err := publishScaffold(root, []scaffoldFile{{"first", "framework"}, {"last", "framework"}}, func(i int) error {
				if i != 1 {
					return nil
				}
				name := filepath.Join(dir, "first")
				if replacement {
					if err := os.Remove(name); err != nil {
						return err
					}
				}
				if err := os.WriteFile(name, []byte(content), 0644); err != nil {
					return err
				}
				return errors.New("injected publication failure")
			})
			if err == nil || !strings.Contains(err.Error(), "preserving first") {
				t.Fatalf("missing retained-file diagnostic: %v", err)
			}
			if got := readFile(t, filepath.Join(dir, "first")); got != content {
				t.Fatalf("rollback lost caller edit: %q", got)
			}
		})
	}
}

func TestScaffoldRejectsParentSwapDuringPublication(t *testing.T) {
	dir := t.TempDir()
	root := openScaffoldTestRoot(t, dir)
	err := publishScaffold(root, []scaffoldFile{{"app/page.gsx", "framework"}}, func(int) error {
		if err := os.Rename(filepath.Join(dir, "app"), filepath.Join(dir, "original")); err != nil {
			return err
		}
		return os.Mkdir(filepath.Join(dir, "app"), 0755)
	})
	if err == nil || !strings.Contains(err.Error(), "changed during init") {
		t.Fatalf("parent swap not rejected: %v", err)
	}
	for _, parent := range []string{"app", "original"} {
		if _, err := os.Stat(filepath.Join(dir, parent, "page.gsx")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("published into swapped parent %s: %v", parent, err)
		}
	}
}

func TestScaffoldRejectsInvalidPlanBeforeWriting(t *testing.T) {
	for _, paths := range [][]string{{"../outside"}, {"."}, {"/absolute"}, {"app\\escape"}, {"a", "a"}, {"a/b", "a"}} {
		t.Run(strings.Join(paths, ","), func(t *testing.T) {
			dir := t.TempDir()
			root := openScaffoldTestRoot(t, dir)
			var files []scaffoldFile
			for _, name := range paths {
				files = append(files, scaffoldFile{name, "content"})
			}
			if err := publishScaffold(root, files, nil); err == nil {
				t.Fatal("invalid plan accepted")
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("invalid plan wrote files: %v, %v", entries, err)
			}
		})
	}
}

func openScaffoldTestRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root
}

func TestScaffoldPublicationWithoutHardLinks(t *testing.T) {
	dir := t.TempDir()
	root := openScaffoldTestRoot(t, dir)
	opens := 0
	filesystem := scaffoldFS{
		openFile: func(parent *os.Root, name string, flags int, mode fs.FileMode) (*os.File, error) {
			opens++
			if flags&os.O_EXCL == 0 || flags&os.O_CREATE == 0 || flags&os.O_TRUNC != 0 {
				t.Fatalf("not a no-clobber write: %d", flags)
			}
			if strings.HasPrefix(name, ".gosx-init-") {
				t.Fatal("publication requires a temporary destination file")
			}
			return parent.OpenFile(name, flags, mode)
		},
	}
	files := []scaffoldFile{{"go.mod", "module example.com/app\n"}, {"app/page.gsx", "package app\n"}}
	if err := publishScaffoldWithFS(root, files, nil, filesystem); err != nil {
		t.Fatal(err)
	}
	if opens != len(files) {
		t.Fatalf("no-clobber opens=%d, want %d", opens, len(files))
	}
	for _, file := range files {
		if got := readFile(t, filepath.Join(dir, filepath.FromSlash(file.Path))); got != file.Contents {
			t.Fatalf("%s = %q", file.Path, got)
		}
	}
}

func TestScaffoldOpenFailureRollsBackEarlierWrites(t *testing.T) {
	dir := t.TempDir()
	root := openScaffoldTestRoot(t, dir)
	failure := errors.New("filesystem refuses late creation")
	filesystem := scaffoldFS{openFile: func(parent *os.Root, name string, flags int, mode fs.FileMode) (*os.File, error) {
		if name == "last" {
			return nil, failure
		}
		return parent.OpenFile(name, flags, mode)
	}}
	if err := publishScaffoldWithFS(root, []scaffoldFile{{"first", "complete"}, {"last", "unused"}}, nil, filesystem); !errors.Is(err, failure) {
		t.Fatalf("expected filesystem failure, got %v", err)
	}
	for _, name := range []string{"first", "last"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s remains after rollback: %v", name, err)
		}
	}
}

func TestInitExistingGoFilesRequireFinalTidy(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "services", "service.go")
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		t.Fatal(err)
	}
	const existing = "package services\nimport _ \"example.com/existing-dependency\"\n"
	if err := os.WriteFile(name, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	output, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	oldStderr := os.Stderr
	os.Stderr = output
	defer func() { os.Stderr = oldStderr }()
	if err := RunInit(dir, "example.com/app", "app"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, output.Name()); !strings.Contains(got, "existing Go files") || !strings.Contains(got, "go mod tidy") {
		t.Fatalf("missing final tidy reminder: %s", got)
	}
	if got := readFile(t, name); got != existing {
		t.Fatalf("existing package changed: %q", got)
	}
}

func TestScaffoldStatAndCloseFailures(t *testing.T) {
	for _, operation := range []string{"stat", "close"} {
		t.Run(operation, func(t *testing.T) {
			dir := t.TempDir()
			root := openScaffoldTestRoot(t, dir)
			failure := errors.New("injected " + operation + " failure")
			filesystem := scaffoldFS{
				stat: func(f *os.File) (os.FileInfo, error) {
					if operation == "stat" && filepath.Base(f.Name()) == "last" {
						return nil, failure
					}
					return f.Stat()
				},
				close: func(f *os.File) error {
					err := f.Close()
					if operation == "close" && filepath.Base(f.Name()) == "last" {
						return errors.Join(err, failure)
					}
					return err
				},
			}
			files := []scaffoldFile{{"first", "complete"}, {"last", "complete"}}
			if err := publishScaffoldWithFS(root, files, nil, filesystem); !errors.Is(err, failure) {
				t.Fatalf("missing failure: %v", err)
			}
			for _, file := range files {
				if operation == "stat" {
					if _, err := os.Stat(filepath.Join(dir, file.Path)); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("%s remains after stat failure: %v", file.Path, err)
					}
				} else if got := readFile(t, filepath.Join(dir, file.Path)); got != file.Contents {
					t.Fatalf("close error removed %s: %q", file.Path, got)
				}
			}
		})
	}
}
