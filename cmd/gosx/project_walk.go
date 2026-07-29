package main

import (
	"os"
	"path/filepath"
	"strings"
)

func shouldSkipProjectWalkDir(projectRoot, path string, info os.FileInfo) bool {
	if info == nil || !info.IsDir() {
		return false
	}
	if sameCleanPath(projectRoot, path) {
		return false
	}
	name := info.Name()
	if shouldSkipProjectDir(name) || strings.HasPrefix(name, ".") {
		return true
	}
	return isNestedGitCheckout(path)
}

func shouldSkipProjectDir(name string) bool {
	switch name {
	case ".git", ".tiller", "build", "dist", "node_modules":
		return true
	default:
		return strings.HasPrefix(name, ".tmp")
	}
}

func isNestedGitCheckout(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

func sameCleanPath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}
