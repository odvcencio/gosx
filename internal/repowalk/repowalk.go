// Package repowalk walks the GoSX repository while skipping generated
// output, dependency caches, and nested checkouts. Repo-wide tests use it so
// a worktree or a build directory inside the checkout cannot change results.
package repowalk

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// SkipAnywhere lists directory names skipped at any depth. Callers must not
// modify this slice.
var SkipAnywhere = []string{
	".git", ".worktrees", ".graft", ".canopy", ".gts", ".tiller", ".buckley",
	".claude", "build", "dist", "node_modules",
}

// SkipAtRoot lists directory names skipped only directly under root. Callers
// must not modify this slice.
var SkipAtRoot = []string{"tmp", "data"}

// Skip reports whether the directory at path must be skipped. It is true for
// SkipAnywhere names at any depth, SkipAtRoot names directly under root, and
// any non-root directory that contains a .git entry (a nested repository or
// a worktree). Skip is only meaningful for directories and never skips root.
// path must come from a walk rooted at root, given in the same path form as
// root.
//
// The .git check fails closed: a nil Lstat error means a marker is present
// and the directory is skipped; an fs.ErrNotExist error means no marker is
// present and the directory is not skipped; any other error means the
// marker cannot be ruled out, and Skip reports that error instead of
// guessing.
func Skip(root, path string, entry fs.DirEntry) (bool, error) {
	if !entry.IsDir() {
		return false, nil
	}
	if filepath.Clean(path) == filepath.Clean(root) {
		return false, nil
	}
	name := entry.Name()
	if slices.Contains(SkipAnywhere, name) {
		return true, nil
	}
	if filepath.Dir(filepath.Clean(path)) == filepath.Clean(root) && slices.Contains(SkipAtRoot, name) {
		return true, nil
	}
	_, err := os.Lstat(filepath.Join(path, ".git"))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("repowalk: inspect %s: %w", path, err)
}

// Walk walks root and calls fn for every regular file that is not under a
// skipped directory. fn never receives a directory. An unreadable directory
// or a .git entry that cannot be inspected is reported as an error, never
// silently skipped. root itself must be a directory; a symlink at root,
// even one that resolves to a directory, is reported as an error rather
// than walked.
func Walk(root string, fn func(path string, entry fs.DirEntry) error) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("repowalk: root %s is not a directory", root)
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			skip, err := Skip(root, path, entry)
			if err != nil {
				return err
			}
			if skip {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		return fn(path, entry)
	})
}

// moduleLine is the go.mod prefix that identifies the gosx module.
var moduleLine = []byte("module m31labs.dev/gosx\n")

// Root returns the nearest ancestor of the working directory whose go.mod
// declares module m31labs.dev/gosx.
func Root() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return RootFrom(dir)
}

// RootFrom returns the nearest ancestor of dir, dir included, whose go.mod
// declares module m31labs.dev/gosx.
func RootFrom(dir string) (string, error) {
	start := dir
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && bytes.HasPrefix(data, moduleLine) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("repowalk: m31labs.dev/gosx go.mod not found above " + start)
		}
		dir = parent
	}
}
