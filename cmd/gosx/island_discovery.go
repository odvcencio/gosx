package main

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/ir"
	islandprogram "m31labs.dev/gosx/island/program"
)

// IslandProgramSource pairs a compiled island program with the absolute path
// to the .gsx file that declared it and a content hash (buildmanifest.
// ContentHash) of that file's raw source bytes as read for this compile.
// RunBuildWithOptions stores SourceHash in the build manifest so a server
// started later can detect that source changed since `gosx build` without
// re-running the compiler pipeline — see buildmanifest.Manifest.StaleIslands.
// PackageDir and PackagePath retain the declaration's package identity while
// the project dependency closure is traversed and validated; they are build
// metadata and do not change the serialized island-program ABI.
type IslandProgramSource struct {
	*islandprogram.Program
	SourceFile     string
	SourceHash     string
	PackageDir     string
	PackagePath    string
	ProjectPackage bool
}

type islandPackageSource struct {
	Dir         string
	ImportPath  string
	PackageName string
	Files       []string
	GoFiles     []string
	GoImports   []string
	Project     bool
}

type islandDiscoveryResult struct {
	Programs  []*IslandProgramSource
	GSXFiles  []string
	WatchDirs []string
	// WatchFiles is the exact canonical physical source allowlist paired with
	// WatchDirs. It contains discovered GSX sources plus the active Go build
	// inputs selected by the Go tool. Most entries are already observed through
	// a project tree or a dependency package root; the explicit list matters
	// when a top-level dependency source is a symlink to a nested file inside
	// that same package.
	WatchFiles []string
	// WatchGoFiles is the exact active-Go subset of WatchFiles. Keeping the
	// subset explicit prevents dev from recreating Go build-selection rules or
	// broadening a package root to inactive GOOS/GOARCH/cgo/build-tag files.
	WatchGoFiles []string
}

func collectProjectIslandPrograms(projectDir string) ([]*IslandProgramSource, []string, error) {
	discovery, err := collectProjectIslandDiscovery(projectDir)
	if err != nil {
		return nil, nil, err
	}
	return discovery.Programs, discovery.GSXFiles, nil
}

// collectProjectIslandWatchDirs returns the canonical package directories
// reached before any source-level validation error. Dev uses this narrower
// result only to keep an invalid newly imported package observable so editing
// that package can recover the quarantined app; build consumers still reject
// the same discovery error and perform no writes.
func collectProjectIslandWatchDirs(projectDir string) ([]string, error) {
	dirs, _, _, err := collectProjectIslandWatchTargets(projectDir)
	return dirs, err
}

// collectProjectIslandWatchTargets returns the canonical package-directory
// closure together with the exact canonical .gsx sources discovered inside
// it. Dev needs both halves: package roots remain direct-file allowlists for
// import changes, while exact files let the watcher observe an accepted
// top-level symlink whose physical target lives in a nested in-package
// directory without recursively trusting that nested tree.
func collectProjectIslandWatchTargets(projectDir string) ([]string, []string, []string, error) {
	discovery, err := collectProjectIslandDiscovery(projectDir)
	if discovery != nil {
		return discovery.WatchDirs, discovery.WatchFiles, discovery.WatchGoFiles, err
	}
	return nil, nil, nil, err
}

func collectProjectIslandDiscovery(projectDir string) (*islandDiscoveryResult, error) {
	canonicalProjectDir, err := canonicalExistingDir(projectDir)
	if err != nil {
		return nil, fmt.Errorf("resolve project dir: %w", err)
	}
	projectDir = canonicalProjectDir

	projectFiles, err := discoverProjectGSXFiles(projectDir)
	if err != nil {
		return nil, err
	}

	projectPackages, err := projectIslandPackages(projectDir, projectFiles)
	if err != nil {
		return nil, err
	}

	pending := append([]islandPackageSource(nil), projectPackages...)
	knownPackages := make(map[string]struct{}, len(pending))
	for _, pkg := range pending {
		knownPackages[islandPackageKey(pkg)] = struct{}{}
	}
	visitedPackages := make(map[string]struct{}, len(pending))
	gsxFileSet := make(map[string]struct{}, len(projectFiles))
	goFileSet := make(map[string]struct{})
	watchDirSet := make(map[string]struct{}, len(projectPackages))
	var programs []*IslandProgramSource
	resolver := newIslandPackageResolver(projectDir)

	for len(pending) > 0 {
		sortIslandPackages(pending)
		pkg := pending[0]
		pending = pending[1:]
		key := islandPackageKey(pkg)
		if _, ok := visitedPackages[key]; ok {
			continue
		}
		visitedPackages[key] = struct{}{}
		watchDirSet[pkg.Dir] = struct{}{}
		// Retain every already-resolved canonical source in partial discovery
		// results as well. If validation below quarantines dev, the exact source
		// that must be edited to recover remains observable.
		for _, file := range pkg.Files {
			gsxFileSet[filepath.Clean(file)] = struct{}{}
		}
		for _, file := range pkg.GoFiles {
			goFileSet[filepath.Clean(file)] = struct{}{}
		}

		packagePrograms, importedPaths, qualifiedAliases, err := collectIslandProgramsFromPackage(pkg)
		if err != nil {
			return finishIslandDiscovery(programs, gsxFileSet, goFileSet, watchDirSet), err
		}
		programs = append(programs, packagePrograms...)

		if len(pkg.Files) == 0 {
			// A Go-only package can bridge a GSX package to another package that
			// owns the actual island source. Traverse its active Go dependency
			// edges; standard-library packages are filtered by go list below.
			importedPaths = mergeStringSets(importedPaths, pkg.GoImports)
		} else {
			qualifiedImportPaths, err := resolveQualifiedComponentImportPaths(resolver, pkg.GoFiles, qualifiedAliases)
			if err != nil {
				return finishIslandDiscovery(programs, gsxFileSet, goFileSet, watchDirSet), err
			}
			importedPaths = mergeStringSets(importedPaths, qualifiedImportPaths)
		}
		importedPackages, resolveErr := resolver.resolve(importedPaths)
		for _, imported := range importedPackages {
			// Resolver failures may occur only after the Go tool has already
			// established a package's canonical physical root (for example,
			// while validating one escaping top-level .gsx symlink). Retain that
			// safe root and every source resolved before the failure so dev can
			// observe the edit that repairs the quarantined closure.
			watchDirSet[imported.Dir] = struct{}{}
			for _, file := range imported.Files {
				gsxFileSet[filepath.Clean(file)] = struct{}{}
			}
			for _, file := range imported.GoFiles {
				goFileSet[filepath.Clean(file)] = struct{}{}
			}
			importedKey := islandPackageKey(imported)
			if _, ok := knownPackages[importedKey]; ok {
				continue
			}
			knownPackages[importedKey] = struct{}{}
			pending = append(pending, imported)
		}
		if resolveErr != nil {
			return finishIslandDiscovery(programs, gsxFileSet, goFileSet, watchDirSet), resolveErr
		}
	}

	discovery := finishIslandDiscovery(programs, gsxFileSet, goFileSet, watchDirSet)
	programs = discovery.Programs
	if err := validateUniqueIslandProgramNames(programs); err != nil {
		return discovery, err
	}
	return discovery, nil
}

func finishIslandDiscovery(programs []*IslandProgramSource, gsxFileSet, goFileSet, watchDirSet map[string]struct{}) *islandDiscoveryResult {
	programs = append([]*IslandProgramSource(nil), programs...)
	sortIslandProgramSources(programs)
	gsxFiles := make([]string, 0, len(gsxFileSet))
	for file := range gsxFileSet {
		gsxFiles = append(gsxFiles, file)
	}
	sort.Strings(gsxFiles)
	goFiles := make([]string, 0, len(goFileSet))
	for file := range goFileSet {
		goFiles = append(goFiles, file)
	}
	sort.Strings(goFiles)
	watchDirs := make([]string, 0, len(watchDirSet))
	for dir := range watchDirSet {
		watchDirs = append(watchDirs, dir)
	}
	sort.Strings(watchDirs)
	return &islandDiscoveryResult{
		Programs:     programs,
		GSXFiles:     gsxFiles,
		WatchDirs:    watchDirs,
		WatchFiles:   mergeStringSets(gsxFiles, goFiles),
		WatchGoFiles: goFiles,
	}
}

func projectIslandPackages(projectDir string, files []string) ([]islandPackageSource, error) {
	filesByDir := make(map[string][]string)
	for _, file := range files {
		dir := filepath.Clean(filepath.Dir(file))
		filesByDir[dir] = append(filesByDir[dir], filepath.Clean(file))
	}

	moduleRoot, modulePath, _ := moduleInfo(projectDir)
	packages := make([]islandPackageSource, 0, len(filesByDir))
	for dir, packageFiles := range filesByDir {
		sort.Strings(packageFiles)
		pkg := islandPackageSource{
			Dir:        dir,
			ImportPath: projectPackageImportPath(moduleRoot, modulePath, dir),
			Files:      packageFiles,
			Project:    true,
		}
		goFiles, packageName, goImports, err := localGoPackageMetadata(dir)
		if err != nil {
			return nil, err
		}
		pkg.GoFiles = goFiles
		pkg.PackageName = packageName
		pkg.GoImports = goImports
		packages = append(packages, pkg)
	}
	sortIslandPackages(packages)
	return packages, nil
}

func projectPackageImportPath(moduleRoot, modulePath, packageDir string) string {
	moduleRoot = filepath.Clean(strings.TrimSpace(moduleRoot))
	modulePath = strings.TrimSpace(modulePath)
	packageDir = filepath.Clean(packageDir)
	if moduleRoot == "." || modulePath == "" || !isPathWithin(packageDir, moduleRoot) {
		return ""
	}
	rel, err := filepath.Rel(moduleRoot, packageDir)
	if err != nil || rel == "." || rel == "" {
		return modulePath
	}
	return pathpkg.Join(modulePath, filepath.ToSlash(rel))
}

func islandPackageKey(pkg islandPackageSource) string {
	// Dir is a canonical physical directory. Runtime names remain deliberately
	// unqualified, so the same package reached through two logical module paths
	// must be visited once rather than compiled twice and falsely diagnosed as
	// two declarations.
	return filepath.Clean(pkg.Dir)
}

func sortIslandPackages(packages []islandPackageSource) {
	sort.SliceStable(packages, func(i, j int) bool {
		if packages[i].Project != packages[j].Project {
			return packages[i].Project
		}
		if packages[i].ImportPath != packages[j].ImportPath {
			return packages[i].ImportPath < packages[j].ImportPath
		}
		return filepath.ToSlash(packages[i].Dir) < filepath.ToSlash(packages[j].Dir)
	})
}

func discoverProjectGSXFiles(projectDir string) ([]string, error) {
	var files []string
	var identities []os.FileInfo
	if err := filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if shouldSkipProjectWalkDir(projectDir, path, info) {
			return filepath.SkipDir
		}
		if strings.HasSuffix(path, ".gsx") {
			canonical, canonicalInfo, err := canonicalSourceFileWithin(path, projectDir)
			if err != nil {
				return err
			}
			if samePhysicalFile(canonicalInfo, identities) {
				return nil
			}
			identities = append(identities, canonicalInfo)
			files = append(files, canonical)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk gsx files: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func canonicalExistingDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", path)
	}
	return filepath.Clean(canonical), nil
}

func canonicalSourceFileWithin(path, root string) (string, os.FileInfo, error) {
	canonicalRoot, err := canonicalExistingDir(root)
	if err != nil {
		return "", nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", nil, err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", nil, fmt.Errorf("resolve source path %s: %w", path, err)
	}
	canonical = filepath.Clean(canonical)
	if !isPathWithin(canonical, canonicalRoot) {
		return "", nil, fmt.Errorf("source path %s resolves outside package root %s", path, canonicalRoot)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("source path %s is not a regular file", path)
	}
	return canonical, info, nil
}

func samePhysicalFile(candidate os.FileInfo, seen []os.FileInfo) bool {
	for _, previous := range seen {
		if os.SameFile(candidate, previous) {
			return true
		}
	}
	return false
}

func collectIslandProgramsFromPackage(pkg islandPackageSource) ([]*IslandProgramSource, []string, []string, error) {
	var programs []*IslandProgramSource
	importSet := map[string]struct{}{}
	qualifiedAliasSet := map[string]struct{}{}
	expectedPackage := strings.TrimSpace(pkg.PackageName)
	expectedSource := ""

	for _, file := range pkg.Files {
		source, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read %s: %w", file, err)
		}

		irProg, err := gosx.Compile(source)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("compile %s: %w", file, err)
		}
		declaredPackage := strings.TrimSpace(irProg.Package)
		if expectedPackage == "" {
			expectedPackage = declaredPackage
			expectedSource = file
		} else if declaredPackage != expectedPackage {
			if expectedSource != "" {
				return nil, nil, nil, fmt.Errorf(
					"mixed island package declarations in %q: %q declares package %q but %q declares package %q",
					filepath.ToSlash(pkg.Dir),
					filepath.ToSlash(expectedSource),
					expectedPackage,
					filepath.ToSlash(file),
					declaredPackage,
				)
			}
			return nil, nil, nil, fmt.Errorf(
				"island package identity mismatch for import %q at %q: %q declares package %q but the resolved Go package is %q",
				pkg.ImportPath,
				filepath.ToSlash(pkg.Dir),
				filepath.ToSlash(file),
				declaredPackage,
				expectedPackage,
			)
		}
		irProg.Dir = pkg.Dir
		irProg.PackagePath = pkg.ImportPath
		for _, imp := range irProg.Imports {
			path := strings.TrimSpace(imp.Path)
			if path != "" {
				importSet[path] = struct{}{}
			}
		}
		for _, alias := range qualifiedComponentAliases(irProg) {
			qualifiedAliasSet[alias] = struct{}{}
		}

		for i, comp := range irProg.Components {
			if !comp.IsIsland {
				continue
			}
			island, err := ir.LowerIsland(irProg, i)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("lower island %s in %s: %w", comp.Name, file, err)
			}
			programs = append(programs, &IslandProgramSource{
				Program:        island,
				SourceFile:     file,
				SourceHash:     buildmanifest.ContentHash(source),
				PackageDir:     pkg.Dir,
				PackagePath:    pkg.ImportPath,
				ProjectPackage: pkg.Project,
			})
		}
	}

	imports := make([]string, 0, len(importSet))
	for path := range importSet {
		imports = append(imports, path)
	}
	sort.Strings(imports)
	aliases := make([]string, 0, len(qualifiedAliasSet))
	for alias := range qualifiedAliasSet {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	return programs, imports, aliases, nil
}

func sortIslandProgramSources(programs []*IslandProgramSource) {
	sort.SliceStable(programs, func(i, j int) bool {
		left, right := programs[i], programs[j]
		if left.ProjectPackage != right.ProjectPackage {
			return left.ProjectPackage
		}
		if left.PackagePath != right.PackagePath {
			return left.PackagePath < right.PackagePath
		}
		if left.SourceFile != right.SourceFile {
			return filepath.ToSlash(left.SourceFile) < filepath.ToSlash(right.SourceFile)
		}
		return left.Name < right.Name
	})
}

func validateUniqueIslandProgramNames(programs []*IslandProgramSource) error {
	seen := make(map[string]*IslandProgramSource, len(programs))
	for i, prog := range programs {
		if prog == nil || prog.Program == nil {
			return fmt.Errorf("invalid island program at index %d: program is nil", i)
		}
		if previous, ok := seen[prog.Name]; ok {
			origins := []string{islandProgramOrigin(previous), islandProgramOrigin(prog)}
			sort.Strings(origins)
			return fmt.Errorf(
				"ambiguous island program %q: %s and %s declare the same unqualified runtime name; rename one island because names must be unique across the project dependency closure",
				prog.Name,
				origins[0],
				origins[1],
			)
		}
		seen[prog.Name] = prog
	}
	return nil
}

func islandProgramOrigin(prog *IslandProgramSource) string {
	if prog == nil {
		return "unknown source"
	}
	packagePath := strings.TrimSpace(prog.PackagePath)
	if packagePath == "" {
		packagePath = filepath.ToSlash(filepath.Clean(prog.PackageDir))
	}
	return fmt.Sprintf("package %q at %q", packagePath, filepath.ToSlash(filepath.Clean(prog.SourceFile)))
}

func qualifiedComponentAliases(prog *ir.Program) []string {
	if prog == nil {
		return nil
	}
	seen := map[string]struct{}{}
	for _, node := range prog.Nodes {
		if node.Kind != ir.NodeComponent {
			continue
		}
		alias, _, ok := strings.Cut(strings.TrimSpace(node.Tag), ".")
		if !ok || alias == "" {
			continue
		}
		seen[alias] = struct{}{}
	}
	aliases := make([]string, 0, len(seen))
	for alias := range seen {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	return aliases
}

func resolveQualifiedComponentImportPaths(resolver *islandPackageResolver, goFiles []string, aliases []string) ([]string, error) {
	if len(aliases) == 0 {
		return nil, nil
	}
	aliasSet := make(map[string]struct{}, len(aliases))
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias != "" {
			aliasSet[alias] = struct{}{}
		}
	}
	if len(aliasSet) == 0 {
		return nil, nil
	}

	importSet := map[string]struct{}{}
	for _, file := range goFiles {
		fileImports, err := matchingGoImportPaths(resolver, file, aliasSet)
		if err != nil {
			return nil, err
		}
		for _, importPath := range fileImports {
			importSet[importPath] = struct{}{}
		}
	}
	imports := make([]string, 0, len(importSet))
	for importPath := range importSet {
		imports = append(imports, importPath)
	}
	sort.Strings(imports)
	return imports, nil
}

func localGoPackageMetadata(packageDir string) ([]string, string, []string, error) {
	hasGoFiles, err := packageHasNonTestGoFiles(packageDir)
	if err != nil {
		return nil, "", nil, err
	}
	if !hasGoFiles {
		// Standalone compiler callers and tests may operate on a GSX-only
		// directory with no module. There is no Go package metadata to select
		// in that case, so avoid requiring go.mod solely to discover .gsx.
		return nil, "", nil, nil
	}
	info, err := goListPackageAtDirReadOnly(packageDir)
	if err != nil {
		return nil, "", nil, fmt.Errorf("resolve active Go package metadata in %s: %w", packageDir, err)
	}
	files, err := canonicalGoListFiles(packageDir, mergeStringSets(info.GoFiles, info.CgoFiles))
	if err != nil {
		return nil, "", nil, err
	}
	return files, strings.TrimSpace(info.Name), mergeStringSets(info.Imports), nil
}

func packageHasNonTestGoFiles(packageDir string) (bool, error) {
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		return false, fmt.Errorf("read package dir %s: %w", packageDir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			return true, nil
		}
	}
	return false, nil
}

func matchingGoImportPaths(resolver *islandPackageResolver, file string, aliases map[string]struct{}) ([]string, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("parse imports %s: %w", file, err)
	}
	var matches []string
	for _, spec := range parsed.Imports {
		importPath := strings.Trim(spec.Path.Value, `"`)
		if importPath == "" {
			continue
		}
		if spec.Name != nil {
			name := spec.Name.Name
			if name == "." || name == "_" {
				continue
			}
			if _, ok := aliases[name]; ok {
				matches = append(matches, importPath)
			}
			continue
		}
		if _, ok := aliases[pathpkg.Base(importPath)]; ok {
			matches = append(matches, importPath)
			continue
		}
		if !shouldResolveGoPackage(importPath) {
			continue
		}
		info, err := resolver.goList(importPath)
		if err != nil {
			return nil, fmt.Errorf("resolve default Go import name %s referenced by %s: %w", importPath, file, err)
		}
		if _, ok := aliases[info.Name]; ok {
			matches = append(matches, importPath)
		}
	}
	return matches, nil
}

func mergeStringSets(values ...[]string) []string {
	set := map[string]struct{}{}
	for _, list := range values {
		for _, value := range list {
			value = strings.TrimSpace(value)
			if value != "" {
				set[value] = struct{}{}
			}
		}
	}
	merged := make([]string, 0, len(set))
	for value := range set {
		merged = append(merged, value)
	}
	sort.Strings(merged)
	return merged
}

func shouldResolveGoPackage(importPath string) bool {
	importPath = strings.TrimSpace(importPath)
	if importPath == "" || importPath == "C" || strings.HasPrefix(importPath, ".") {
		return false
	}
	return true
}

type goListPackageInfo struct {
	Dir            string
	ImportPath     string
	Name           string
	Standard       bool
	GoFiles        []string
	CgoFiles       []string
	IgnoredGoFiles []string
	InvalidGoFiles []string
	Imports        []string
	Error          *goListPackageError
}

type goListPackageError struct {
	Err string
}

type goListCacheEntry struct {
	info goListPackageInfo
	err  error
}

type islandPackageResolver struct {
	projectDir string
	cache      map[string]goListCacheEntry
}

func newIslandPackageResolver(projectDir string) *islandPackageResolver {
	return &islandPackageResolver{projectDir: filepath.Clean(projectDir), cache: map[string]goListCacheEntry{}}
}

func (r *islandPackageResolver) goList(importPath string) (goListPackageInfo, error) {
	importPath = strings.TrimSpace(importPath)
	if cached, ok := r.cache[importPath]; ok {
		return cached.info, cached.err
	}
	info, err := goListPackageReadOnly(r.projectDir, importPath)
	r.cache[importPath] = goListCacheEntry{info: info, err: err}
	return info, err
}

func (r *islandPackageResolver) resolve(importPaths []string) ([]islandPackageSource, error) {
	paths := mergeStringSets(importPaths)
	seen := map[string]struct{}{}
	packages := make([]islandPackageSource, 0, len(paths))
	for _, requestedPath := range paths {
		if !shouldResolveGoPackage(requestedPath) {
			continue
		}
		info, err := r.goList(requestedPath)
		if err != nil {
			return nil, err
		}
		if info.Standard || strings.TrimSpace(info.Dir) == "" {
			continue
		}
		dir, err := canonicalExistingDir(info.Dir)
		if err != nil {
			return nil, fmt.Errorf("resolve imported package directory %s: %w", requestedPath, err)
		}
		logicalImportPath := strings.TrimSpace(info.ImportPath)
		if logicalImportPath == "" {
			logicalImportPath = requestedPath
		}
		pkg := islandPackageSource{
			Dir:         dir,
			ImportPath:  logicalImportPath,
			PackageName: strings.TrimSpace(info.Name),
			GoImports:   mergeStringSets(info.Imports),
			Project:     isPathWithin(dir, r.projectDir),
		}
		goFiles, err := canonicalGoListFiles(dir, mergeStringSets(info.GoFiles, info.CgoFiles))
		pkg.GoFiles = goFiles
		if err != nil {
			packages = append(packages, pkg)
			sortIslandPackages(packages)
			return packages, err
		}
		files, err := packageGSXFiles(dir)
		pkg.Files = files
		if err != nil {
			packages = append(packages, pkg)
			sortIslandPackages(packages)
			return packages, err
		}
		key := islandPackageKey(pkg)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		packages = append(packages, pkg)
	}
	sortIslandPackages(packages)
	return packages, nil
}

func goListPackageReadOnly(projectDir, importPath string) (goListPackageInfo, error) {
	return runGoListPackageReadOnly(projectDir, importPath, false)
}

// goListPackageAtDirReadOnly asks the Go tool for the active package files in
// one physical directory. A directory with no active Go files is valid for
// GoSX because it may contain only .gsx sources (or only files excluded by the
// current GOOS/GOARCH/cgo/build constraints). All other Go package errors stay
// fail-closed.
func goListPackageAtDirReadOnly(packageDir string) (goListPackageInfo, error) {
	info, err := runGoListPackageReadOnly(packageDir, ".", false)
	if err == nil {
		return info, nil
	}
	fallback, fallbackErr := runGoListPackageReadOnly(packageDir, ".", true)
	if fallbackErr == nil && len(fallback.GoFiles) == 0 && len(fallback.CgoFiles) == 0 && len(fallback.InvalidGoFiles) == 0 {
		return fallback, nil
	}
	return goListPackageInfo{}, err
}

func runGoListPackageReadOnly(commandDir, importPath string, tolerateErrors bool) (goListPackageInfo, error) {
	args := []string{"list"}
	if tolerateErrors {
		args = append(args, "-e")
	}
	args = append(args, "-json", importPath)
	cmd := exec.Command("go", args...)
	cmd.Dir = commandDir
	cmd.Env = append(execEnvWithoutGoFlags(), "GOFLAGS=-mod=readonly -buildvcs=false", "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return goListPackageInfo{}, fmt.Errorf("resolve Go package %s from %s without modifying go.mod/go.sum: %s", importPath, commandDir, detail)
	}
	var info goListPackageInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return goListPackageInfo{}, fmt.Errorf("decode go list for %s: %w", importPath, err)
	}
	return info, nil
}

func canonicalGoListFiles(dir string, names []string) ([]string, error) {
	files := make([]string, 0, len(names))
	identities := make([]os.FileInfo, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		canonical, info, err := canonicalSourceFileWithin(path, dir)
		if err != nil {
			return nil, err
		}
		if samePhysicalFile(info, identities) {
			continue
		}
		identities = append(identities, info)
		files = append(files, canonical)
	}
	sort.Strings(files)
	return files, nil
}

func packageGSXFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read package dir %s: %w", dir, err)
	}
	files := make([]string, 0, len(entries))
	identities := make([]os.FileInfo, 0, len(entries))
	var firstErr error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".gsx") {
			continue
		}
		path, info, err := canonicalSourceFileWithin(filepath.Join(dir, entry.Name()), dir)
		if err != nil {
			// Preserve all independently safe direct sources in a partial
			// discovery result, even when an earlier entry is invalid. The first
			// deterministic error still fails the package closed.
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if samePhysicalFile(info, identities) {
			continue
		}
		identities = append(identities, info)
		files = append(files, path)
	}
	sort.Strings(files)
	return files, firstErr
}

func isPathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != "" && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
