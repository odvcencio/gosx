// Package checkpath provides virtual Go package paths for source directories.
package checkpath

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/module"
)

// Sanitize encodes every path element, including its escape characters.
// Hex is reversible and the slash boundaries are preserved.
func Sanitize(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i, part := range parts {
		parts[i] = "p" + hex.EncodeToString([]byte(part))
	}
	return filepath.FromSlash(strings.Join(parts, "/"))
}

// Prepare returns the virtual package directory and the Go command's working
// directory and argument. The overlay preserves the original package files.
func Prepare(dir, tempDir string, overlay map[string]string) (string, string, string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", "", "", err
	}
	root := abs
	for {
		if info, err := os.Stat(filepath.Join(root, "go.mod")); err == nil && !info.IsDir() {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			return abs, abs, ".", nil
		}
		root = parent
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", "", "", err
	}
	if rel == "." || module.CheckImportPath("example.com/"+filepath.ToSlash(rel)) == nil {
		return abs, abs, ".", nil
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	firstInvalid := 0
	for i := range parts {
		if module.CheckImportPath("example.com/"+strings.Join(parts[:i+1], "/")) != nil {
			firstInvalid = i
			break
		}
	}
	// Keep valid ancestors so Go's internal-package visibility is preserved.
	parent := filepath.Join(append([]string{root}, parts[:firstInvalid]...)...)
	prefix := "gosxcheck" + filepath.Base(tempDir)
	for {
		if _, err := os.Stat(filepath.Join(parent, prefix)); os.IsNotExist(err) {
			break
		} else if err != nil {
			return "", "", "", err
		}
		prefix += "x"
	}
	virtual := filepath.Join(parent, prefix, Sanitize(strings.Join(parts[firstInvalid:], "/")))
	err = filepath.WalkDir(abs, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			rel, err := filepath.Rel(abs, path)
			if err != nil {
				return err
			}
			overlay[filepath.Join(virtual, rel)] = path
		}
		return nil
	})
	if err != nil {
		return "", "", "", err
	}
	virtualRel, err := filepath.Rel(root, virtual)
	if err != nil {
		return "", "", "", err
	}
	return virtual, root, "./" + filepath.ToSlash(virtualRel), nil
}
