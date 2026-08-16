package route

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// fileRouteDirSource remembers what AddDir discovered so Build/BuildChecked
// can re-check the file module registry at build time, when a stale
// modules.go is otherwise silent: the route serves, the template renders, and
// only the missing Load/Actions wiring gives it away.
type fileRouteDirSource struct {
	root     string
	registry *FileModuleRegistry
	pages    []FilePage
}

// logUnregisteredFileModuleWarnings logs one line per page directory whose
// *.server.go registrant never made it into the file module registry.
func (r *Router) logUnregisteredFileModuleWarnings() {
	if r == nil {
		return
	}
	for _, source := range r.fileRouteDirs {
		for _, warning := range unregisteredFileModuleWarnings(source.registry, source.root, source.pages) {
			log.Print(warning)
		}
	}
}

// unregisteredFileModuleWarnings finds discovered page directories that ship a
// page.server.go (or index.server.go) file on disk but never registered a
// file module — the signature of a stale modules.go. It is the pure decision
// behind the router-build warning, so a test can assert on it without
// capturing log output.
func unregisteredFileModuleWarnings(registry *FileModuleRegistry, root string, pages []FilePage) []string {
	var warnings []string
	for _, page := range pages {
		if _, ok := resolveFileModule(registry, root, page); ok {
			continue
		}
		if !isFile(fileRoutePageServerGoPath(page)) {
			continue
		}
		warnings = append(warnings, fmt.Sprintf(
			"gosx route: %s has a page.server.go but no registered module; regenerate modules.go (gosx build)",
			filepath.Dir(page.FilePath),
		))
	}
	return warnings
}

// fileRoutePageServerGoPath is the *.server.go sibling a page source expects,
// following the same base-name convention FileModuleHere infers from the
// calling file (page.gsx <-> page.server.go, index.html <-> index.server.go).
func fileRoutePageServerGoPath(page FilePage) string {
	ext := filepath.Ext(page.FilePath)
	base := strings.TrimSuffix(page.FilePath, ext)
	return base + ".server.go"
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
