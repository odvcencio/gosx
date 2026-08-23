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
// only the missing Load/Metadata/Render wiring gives it away; managed actions
// are registered on the owning route.Router instead of a file module.
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

// unregisteredFileModuleDirThreshold is the most unregistered directories
// unregisteredFileModuleWarnings reports one line each for. Past it, a
// partially regenerated modules.go floods the log with a line per directory
// under test, so the function collapses the list to one summary line
// instead.
const unregisteredFileModuleDirThreshold = 2

// unregisteredFileModuleWarnings finds discovered page directories that ship a
// page.server.go (or index.server.go) file on disk but never registered a
// file module — the signature of a stale modules.go. It is the pure decision
// behind the router-build warning, so a test can assert on it without
// capturing log output.
//
// unregisteredFileModuleDirThreshold or fewer unregistered directories get one
// line each. More than that collapse to a single summary line, so a stale
// modules.go covering a whole sub-tree does not flood the log with a line per
// directory.
func unregisteredFileModuleWarnings(registry *FileModuleRegistry, root string, pages []FilePage) []string {
	var dirs []string
	for _, page := range pages {
		if _, ok := resolveFileModule(registry, root, page); ok {
			continue
		}
		if !isFile(fileRoutePageServerGoPath(page)) {
			continue
		}
		dirs = append(dirs, filepath.Dir(page.FilePath))
	}
	if len(dirs) == 0 {
		return nil
	}
	if len(dirs) > unregisteredFileModuleDirThreshold {
		return []string{fmt.Sprintf(
			"gosx route: %d routed directories have page.server.go files with no registered module (first: %s); regenerate modules.go (gosx build)",
			len(dirs), dirs[0],
		)}
	}
	warnings := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		warnings = append(warnings, fmt.Sprintf(
			"gosx route: %s has a page.server.go but no registered module; regenerate modules.go (gosx build)",
			dir,
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
