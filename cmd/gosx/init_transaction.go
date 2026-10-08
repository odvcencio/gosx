package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// createScaffold prepares generated modules and dependency files away from the
// destination. Only a complete plan is published. A tidy failure remains a
// warning, and publishes the original go.mod rather than a partially tidied one.
func createScaffold(dir, module string, files []scaffoldFile, tidy func(string) error) (tidyErr, err error) {
	if runtime.GOOS == "js" || runtime.GOOS == "plan9" {
		return nil, fmt.Errorf("init requires descriptor-backed os.Root")
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	before, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("scaffold destination must be a real directory: %s", dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		return nil, fmt.Errorf("scaffold destination changed while opening: %s", dir)
	}
	// go.sum belongs to dependency resolution even when an offline tidy cannot
	// produce it. Refuse an existing file or dangling symlink before any writes.
	planned := append(append([]scaffoldFile(nil), files...), scaffoldFile{Path: "go.sum"})
	if err = preflightScaffold(root, planned); err != nil {
		return nil, err
	}
	existingImports, err := discoverModuleImports(dir, dir, module)
	if err != nil {
		return nil, err
	}
	existingGo := false
	if err = fs.WalkDir(root.FS(), ".", func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			existingGo = true
			return fs.SkipAll
		}
		return nil
	}); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp("", "gosx-init-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	for _, file := range files {
		name := filepath.Join(stage, filepath.FromSlash(file.Path))
		if err = os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			return nil, err
		}
		if err = os.WriteFile(name, []byte(file.Contents), 0644); err != nil {
			return nil, err
		}
	}
	imports, err := discoverModuleImports(stage, stage, module)
	if err != nil {
		return nil, err
	}
	importSet := make(map[string]bool, len(imports))
	for _, name := range imports {
		importSet[name] = true
	}
	for _, name := range existingImports {
		if !importSet[name] {
			imports = append(imports, name)
			importSet[name] = true
		}
	}
	sort.Strings(imports)
	for i := range files {
		if files[i].Path == "modules/modules.go" {
			files[i].Contents = renderModulesPackage(imports)
			if err = os.WriteFile(filepath.Join(stage, "modules", "modules.go"), []byte(files[i].Contents), 0644); err != nil {
				return nil, err
			}
		}
	}
	if existingGo {
		// Existing packages, including those outside app/, are absent from staging.
		// Resolve all their dependencies in the completed project instead.
		tidyErr = fmt.Errorf("existing Go files require dependency resolution in the completed project")
	} else {
		tidyErr = tidy(stage)
	}
	if tidyErr == nil {
		// go mod tidy owns only these two outputs. Do not copy any unexpected
		// command output into the caller's project.
		for _, name := range []string{"go.mod", "go.sum"} {
			contents, readErr := os.ReadFile(filepath.Join(stage, name))
			if errors.Is(readErr, os.ErrNotExist) && name == "go.sum" {
				continue
			}
			if readErr != nil {
				return nil, readErr
			}
			if name == "go.sum" {
				files = append(files, scaffoldFile{Path: name, Contents: string(contents)})
			} else {
				for i := range files {
					if files[i].Path == name {
						files[i].Contents = string(contents)
					}
				}
			}
		}
	}
	// Recheck the reserved go.sum as well, including when tidy failed.
	if err = preflightScaffold(root, planned); err != nil {
		return tidyErr, err
	}
	return tidyErr, publishScaffold(root, files, nil)
}

func preflightScaffold(root *os.Root, files []scaffoldFile) error {
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		if !fs.ValidPath(file.Path) || strings.ContainsAny(file.Path, "\\:") || seen[file.Path] {
			return fmt.Errorf("invalid or duplicate scaffold path %q", file.Path)
		}
		seen[file.Path] = true
	}
	for _, file := range files {
		parts := strings.Split(file.Path, "/")
		for i := range parts {
			name := strings.Join(parts[:i+1], "/")
			if i < len(parts)-1 && seen[name] {
				return fmt.Errorf("scaffold file %s is also a parent directory", name)
			}
			info, err := root.Lstat(name)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return fmt.Errorf("inspect %s: %w", name, err)
			}
			if i == len(parts)-1 {
				return fmt.Errorf("%s already exists", name)
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("parent %s must be a real directory", name)
			}
		}
	}
	return nil
}

type scaffoldPublication struct {
	file      scaffoldFile
	parent    *os.Root
	handle    *os.File
	info      os.FileInfo
	installed bool
	written   int64
}

// publishScaffold pins each parent and creates destination files with O_EXCL.
// A late conflict rolls back only the files this invocation still owns. Files
// can be visible while being written; this is not crash-atomic publication.
// Empty directories may remain after a failed init.
func publishScaffold(root *os.Root, files []scaffoldFile, beforePublish func(int) error) error {
	return publishScaffoldWithFS(root, files, beforePublish, scaffoldFS{})
}

// scaffoldFS tests publication using only no-clobber creation, without requiring
// hard-link support or changing process-wide functions.
type scaffoldFS struct {
	openFile func(*os.Root, string, int, fs.FileMode) (*os.File, error)
	stat     func(*os.File) (os.FileInfo, error)
	close    func(*os.File) error
}

func publishScaffoldWithFS(root *os.Root, files []scaffoldFile, beforePublish func(int) error, filesystem scaffoldFS) (err error) {
	if err = preflightScaffold(root, files); err != nil {
		return err
	}
	openFile := filesystem.openFile
	if openFile == nil {
		openFile = (*os.Root).OpenFile
	}
	stat, closeFile := filesystem.stat, filesystem.close
	if stat == nil {
		stat = (*os.File).Stat
	}
	if closeFile == nil {
		closeFile = (*os.File).Close
	}
	var entries []*scaffoldPublication
	defer func() {
		failed := err != nil
		for i := len(entries) - 1; i >= 0; i-- {
			e := entries[i]
			if failed && e.installed {
				err = errors.Join(err, rollbackScaffoldFile(e))
			}
			if e.handle != nil {
				err = errors.Join(err, closeFile(e.handle))
			}
			err = errors.Join(err, e.parent.Close())
		}
	}()
	// Pin all parents before callbacks or writes so swaps are detected later.
	for _, file := range files {
		parent, openErr := openScaffoldParent(root, file.Path, true)
		if openErr != nil {
			return openErr
		}
		entries = append(entries, &scaffoldPublication{file: file, parent: parent})
	}
	for i, e := range entries {
		if beforePublish != nil {
			if err = beforePublish(i); err != nil {
				return err
			}
		}
		current, openErr := openScaffoldParent(root, e.file.Path, false)
		if openErr != nil {
			return openErr
		}
		a, aErr := current.Stat(".")
		b, bErr := e.parent.Stat(".")
		current.Close()
		if aErr != nil || bErr != nil || !os.SameFile(a, b) {
			return fmt.Errorf("parent of %s changed during init", e.file.Path)
		}
		f, createErr := openFile(e.parent, path.Base(e.file.Path), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0644)
		if createErr != nil {
			return fmt.Errorf("publish %s: %w", e.file.Path, createErr)
		}
		e.handle = f
		e.info, err = stat(f)
		if err != nil {
			err = errors.Join(err, closeFile(f), e.parent.Remove(path.Base(e.file.Path)))
			e.handle = nil
			return err
		}
		e.installed = true
		n, writeErr := io.WriteString(f, e.file.Contents)
		e.written = int64(n)
		if writeErr == nil {
			writeErr = f.Sync()
		}
		if writeErr != nil {
			return fmt.Errorf("write %s: %w", e.file.Path, writeErr)
		}
	}
	return nil
}

func openScaffoldParent(root *os.Root, target string, create bool) (*os.Root, error) {
	dir := path.Dir(target)
	if dir != "." {
		current := ""
		for _, part := range strings.Split(dir, "/") {
			current = path.Join(current, part)
			info, err := root.Lstat(current)
			if errors.Is(err, os.ErrNotExist) && create {
				if err = root.Mkdir(current, 0755); err != nil && !errors.Is(err, os.ErrExist) {
					return nil, err
				}
				info, err = root.Lstat(current)
			}
			if err != nil {
				return nil, err
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("parent %s must be a real directory", current)
			}
		}
	}
	before, err := root.Lstat(dir)
	if err != nil {
		return nil, err
	}
	parent, err := root.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	after, err := parent.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		parent.Close()
		return nil, fmt.Errorf("parent of %s changed while opening", target)
	}
	return parent, nil
}

func rollbackScaffoldFile(e *scaffoldPublication) error {
	// Ownership checks and removal are best effort: Go has no unlink-by-handle,
	// so a concurrent replacement between the last check and Remove can race.
	name := path.Base(e.file.Path)
	info, err := e.parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, e.info) {
		return fmt.Errorf("preserving %s: file changed after init publication", e.file.Path)
	}
	// Read the descriptor retained from creation. Reopening by path here could
	// block on a concurrently substituted FIFO or follow a substituted symlink.
	data, readErr := io.ReadAll(io.NewSectionReader(e.handle, 0, e.written+1))
	if readErr != nil || !bytes.Equal(data, []byte(e.file.Contents[:e.written])) {
		return fmt.Errorf("preserving %s: file changed after init publication", e.file.Path)
	}
	return e.parent.Remove(name)
}
