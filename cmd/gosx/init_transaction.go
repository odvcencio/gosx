package main

import (
	"bytes"
	"crypto/rand"
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
	if len(existingImports) > 0 {
		// Preserve registration of existing routes without exposing their import
		// paths to a staging tidy that cannot see those local packages.
		tidyErr = fmt.Errorf("existing route modules require dependency resolution in the completed project")
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
	stage     string
	info      os.FileInfo
	installed bool
}

// publishScaffold uses the same pinned-parent, staged hard-link publication and
// identity/content checked rollback as internal/uirecipe. Init only creates
// files; it does not need the recipe updater's ownership manifest or backups.
// Publication is no-clobber per file, not an atomic whole-directory swap or a
// crash-recovery protocol. Empty directories may remain after a failed init.
func publishScaffold(root *os.Root, files []scaffoldFile, beforePublish func(int) error) (err error) {
	if err = preflightScaffold(root, files); err != nil {
		return err
	}
	var entries []*scaffoldPublication
	defer func() {
		rollback := err != nil
		for i := len(entries) - 1; i >= 0; i-- {
			e := entries[i]
			if rollback && e.installed {
				err = errors.Join(err, rollbackScaffoldFile(e))
			}
			if e.stage != "" {
				if cleanupErr := e.parent.Remove(e.stage); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
					err = errors.Join(err, fmt.Errorf("remove scaffold staging file %s: %w", e.stage, cleanupErr))
				}
			}
			if e.handle != nil {
				err = errors.Join(err, e.handle.Close())
			}
			e.parent.Close()
		}
	}()
	for _, file := range files {
		parent, openErr := openScaffoldParent(root, file.Path, true)
		if openErr != nil {
			return openErr
		}
		e := &scaffoldPublication{file: file, parent: parent, stage: ".gosx-init-" + rand.Text()}
		entries = append(entries, e)
		f, createErr := parent.OpenFile(e.stage, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0644)
		if createErr != nil {
			e.stage = "" // An existing name is owned by its creator.
			return createErr
		}
		e.handle = f
		_, writeErr := io.WriteString(f, file.Contents)
		if writeErr == nil {
			writeErr = f.Sync()
		}
		e.info, err = f.Stat()
		if err = errors.Join(writeErr, err); err != nil {
			return err
		}
	}
	for i, e := range entries {
		if beforePublish != nil {
			if err = beforePublish(i); err != nil {
				return err
			}
		}
		// Detect directory changes after staging; os.Root remains the traversal
		// boundary even if an ancestor is replaced concurrently with this check.
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
		staged, statErr := e.parent.Lstat(e.stage)
		if statErr != nil || !staged.Mode().IsRegular() || !os.SameFile(staged, e.info) {
			return fmt.Errorf("staged file for %s changed during init", e.file.Path)
		}
		// Link never replaces an existing file, directory, or dangling symlink.
		// Filesystems without hard links fail closed.
		if err = e.parent.Link(e.stage, path.Base(e.file.Path)); err != nil {
			return fmt.Errorf("publish %s: %w", e.file.Path, err)
		}
		e.installed = true
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
	data, readErr := io.ReadAll(io.NewSectionReader(e.handle, 0, int64(len(e.file.Contents))+1))
	if readErr != nil || !bytes.Equal(data, []byte(e.file.Contents)) {
		return fmt.Errorf("preserving %s: file changed after init publication", e.file.Path)
	}
	return e.parent.Remove(name)
}
