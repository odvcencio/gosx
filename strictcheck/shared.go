package strictcheck

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/transpile"
)

// resolveSharedImports discovers every shared (./ or ../ prefixed) import
// files declares, loads each target directory, and returns the map
// TranspilePackageWithSharedImports needs to project a shared call
// (transpile.SharedImport, keyed by the raw import path text — see
// transpile/package.go's SharedImport doc comment, which names this
// function as "the intended producer"), alongside every shared target's own
// generated Go text, keyed by a virtual .go path already resolved to sit
// inside that target's own real directory (see goCheck).
//
// A package with no shared import returns (nil, nil, nil): every existing
// call path is unaffected byte-for-byte.
//
// gosx build copies only app/, content/, and public/ into the deployment
// bundle (cmd/gosx's stageDeploymentBundle). A shared import resolving
// outside root's app/ directory would type-check here and then not ship —
// this rejects that case with a diagnostic naming the import, the file that
// declared it, and the resolved (and expected) directories, rather than
// leaving it to fail once the built binary tries to render it in
// production.
func resolveSharedImports(ctx context.Context, files []transpile.PackageFile, opts Options, root string) (map[string]transpile.SharedImport, map[string]string, error) {
	if len(files) == 0 {
		return nil, nil, nil
	}
	rawPaths := collectSharedImportPaths(files)
	if len(rawPaths) == 0 {
		return nil, nil, nil
	}
	dir := filepath.Dir(files[0].Path)

	var appRoot, modulePath string
	if root != "" {
		appRoot = filepath.Join(root, "app")
		// A "" modulePath (go.mod missing, unreadable, or carrying no module
		// directive) is not fatal by itself here — the per-import loop below
		// reports it only once it actually needs a Go import path, so a
		// package with shared imports that all happen to fail the app/ check
		// first reports THAT diagnostic instead.
		modulePath, _ = readModulePath(root)
	}

	sharedImports := make(map[string]transpile.SharedImport, len(rawPaths))
	overlay := make(map[string]string)
	usedVirtual := make(map[string]struct{})
	virtualIndex := 0

	for _, rawPath := range rawPaths {
		targetDir := filepath.Clean(filepath.Join(dir, rawPath))
		if appRoot != "" && !withinDir(appRoot, targetDir) {
			return nil, nil, fmt.Errorf("shared import %q in %s resolves to %s, outside %s; gosx build stages only app/, content/, and public/ into the deployment bundle, so a shared component directory must live under app/", rawPath, dir, targetDir, appRoot)
		}
		targetFiles, err := transpile.LoadPackageDir(targetDir)
		if err != nil {
			return nil, nil, fmt.Errorf("shared import %q in %s: %w", rawPath, dir, err)
		}
		targetImportNames, err := resolveImportNames(ctx, targetFiles, opts)
		if err != nil {
			return nil, nil, fmt.Errorf("shared import %q in %s: %w", rawPath, dir, err)
		}
		// No recursion: a shared target's OWN shared imports (a nested "./"
		// import inside app/ui, for example) are not resolved here. Nothing
		// in the shared components design v1 scope requires it, and every
		// real caller today is one directory deep (design section 1.2's own
		// headline example). A shared directory that itself imports another
		// shared directory fails the same way an unresolved shared import
		// always has: errUnresolvedSharedCall's message naming gosx check.
		targetGenerated, err := transpile.TranspilePackageWithSharedImports(targetFiles, targetImportNames, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("shared import %q in %s: %w", rawPath, dir, err)
		}
		if modulePath == "" {
			return nil, nil, fmt.Errorf("shared import %q in %s: cannot resolve a Go import path for %s without a readable go.mod module directive at %s", rawPath, dir, targetDir, root)
		}
		rel, err := filepath.Rel(root, targetDir)
		if err != nil {
			return nil, nil, fmt.Errorf("shared import %q in %s: %w", rawPath, dir, err)
		}
		goImportPath := path.Join(modulePath, filepath.ToSlash(rel))

		for _, text := range targetGenerated {
			virtual := uniqueVirtualGoPath(targetDir, virtualIndex, usedVirtual)
			usedVirtual[virtual] = struct{}{}
			overlay[virtual] = text
			virtualIndex++
		}

		sharedImports[rawPath] = transpile.SharedImport{
			GoImportPath: goImportPath,
			Components:   transpile.CollectSharedComponents(targetFiles),
		}
	}

	return sharedImports, overlay, nil
}

// collectSharedImportPaths returns every distinct shared (./ or ../
// prefixed) import path files declares, sorted for a deterministic
// resolution order (and therefore a deterministic first diagnostic when
// more than one shared import fails).
func collectSharedImportPaths(files []transpile.PackageFile) []string {
	seen := make(map[string]struct{})
	for _, file := range files {
		if file.Program == nil {
			continue
		}
		for _, imp := range file.Program.Imports {
			if ir.IsSharedImportPath(imp.Path) {
				seen[imp.Path] = struct{}{}
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// withinDir reports whether target is rootDir itself or a descendant of it.
func withinDir(rootDir, target string) bool {
	rootDir = filepath.Clean(rootDir)
	target = filepath.Clean(target)
	if rootDir == target {
		return true
	}
	rel, err := filepath.Rel(rootDir, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// readModulePath reads the module directive from root/go.mod. It is a
// deliberately minimal parse — the first "module " line, trimmed — rather
// than a dependency on golang.org/x/mod/modfile: within one's own module, a
// directory's Go import path is always modulePath + its path relative to
// the module root, regardless of any replace or vendor directive (those
// affect how OTHER modules resolve, not paths inside this one), so nothing
// past the module line is needed here.
func readModulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", fmt.Errorf("no module directive in %s", filepath.Join(root, "go.mod"))
}
