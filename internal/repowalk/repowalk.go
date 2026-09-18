// Package repowalk walks the GoSX repository while skipping generated
// output, dependency caches, and nested checkouts. Repo-wide tests use it so
// a worktree or a build directory inside the checkout cannot change results.
package repowalk

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// SkipAnywhere lists directory names skipped at any depth.
var SkipAnywhere = []string{
	".git", ".worktrees", ".graft", ".canopy", ".gts", ".tiller", ".buckley",
	".claude", "build", "dist", "node_modules",
}

// SkipAtRoot lists directory names skipped only directly under root.
var SkipAtRoot = []string{"tmp", "data"}

// Skip reports whether the directory at path must be skipped. It is true for
// SkipAnywhere names at any depth, SkipAtRoot names directly under root, and
// any non-root directory that contains a .git entry (a nested repository or a
// worktree). Skip is only meaningful for directories and never skips root.
// The .git check fails closed: an Lstat error other than "not exist" is
// treated as a marker it cannot rule out, so the directory is skipped.
func Skip(root, path string, entry fs.DirEntry) bool {
	if !entry.IsDir() {
		return false
	}
	if filepath.Clean(path) == filepath.Clean(root) {
		return false
	}
	name := entry.Name()
	if slices.Contains(SkipAnywhere, name) {
		return true
	}
	if filepath.Dir(filepath.Clean(path)) == filepath.Clean(root) && slices.Contains(SkipAtRoot, name) {
		return true
	}
	_, err := os.Lstat(filepath.Join(path, ".git"))
	if err == nil {
		return true
	}
	return !errors.Is(err, fs.ErrNotExist)
}

// Walk walks root and calls fn for every regular file that is not under a
// skipped directory. fn never receives a directory.
func Walk(root string, fn func(path string, entry fs.DirEntry) error) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if Skip(root, path, entry) {
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

// Root returns the nearest ancestor of the working directory whose go.mod
// declares module m31labs.dev/gosx.
func Root() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.HasPrefix(string(data), "module m31labs.dev/gosx\n") {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("repowalk: m31labs.dev/gosx go.mod not found above " + dir)
		}
		dir = parent
	}
}
