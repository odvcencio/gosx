// Package samples embeds the source files rendered by the GoSX documentation.
package samples

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed */*.sample
var files embed.FS

// Read returns a documentation sample by its slash-separated path.
func Read(path string) (string, error) {
	if !fs.ValidPath(path) {
		return "", fmt.Errorf("invalid documentation sample path %q", path)
	}
	body, err := files.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read documentation sample %q: %w", path, err)
	}
	return string(body), nil
}

// MustRead returns a sample embedded in the site. It panics only when a page
// refers to a file that was not included in the build.
func MustRead(path string) string {
	body, err := Read(path)
	if err != nil {
		panic(err)
	}
	return body
}

// Files returns every embedded sample path in stable order.
func Files() []string {
	var paths []string
	if err := fs.WalkDir(files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".sample") {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		panic(fmt.Errorf("walk embedded documentation samples: %w", err))
	}
	sort.Strings(paths)
	return paths
}
