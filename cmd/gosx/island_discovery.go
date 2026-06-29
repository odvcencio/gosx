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
	"m31labs.dev/gosx/ir"
	islandprogram "m31labs.dev/gosx/island/program"
)

func collectProjectIslandPrograms(projectDir string) ([]*islandprogram.Program, []string, error) {
	absProjectDir, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve project dir: %w", err)
	}
	projectDir = absProjectDir

	projectFiles, err := discoverProjectGSXFiles(projectDir)
	if err != nil {
		return nil, nil, err
	}

	programs, importedPaths, qualifiedAliases, err := collectIslandProgramsFromFiles(projectFiles)
	if err != nil {
		return nil, nil, err
	}
	qualifiedImportPaths, err := resolveQualifiedComponentImportPaths(projectDir, qualifiedAliases)
	if err != nil {
		return nil, nil, err
	}
	importedPaths = mergeStringSets(importedPaths, qualifiedImportPaths)

	importedDirs, err := resolveImportedGSXPackageDirs(projectDir, importedPaths)
	if err != nil {
		return nil, nil, err
	}

	var importedFiles []string
	for _, dir := range importedDirs {
		files, err := packageGSXFiles(dir)
		if err != nil {
			return nil, nil, err
		}
		importedFiles = append(importedFiles, files...)
	}
	if len(importedFiles) == 0 {
		return programs, projectFiles, nil
	}

	importedPrograms, _, _, err := collectIslandProgramsFromFiles(importedFiles)
	if err != nil {
		return nil, nil, err
	}
	programs = append(programs, importedPrograms...)

	allFiles := append([]string(nil), projectFiles...)
	allFiles = append(allFiles, importedFiles...)
	sort.Strings(allFiles)
	return programs, allFiles, nil
}

func discoverProjectGSXFiles(projectDir string) ([]string, error) {
	var files []string
	if err := filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && shouldSkipProjectDir(info.Name()) {
			return filepath.SkipDir
		}
		if strings.HasSuffix(path, ".gsx") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk gsx files: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func collectIslandProgramsFromFiles(files []string) ([]*islandprogram.Program, []string, []string, error) {
	var programs []*islandprogram.Program
	importSet := map[string]struct{}{}
	qualifiedAliasSet := map[string]struct{}{}

	for _, file := range files {
		source, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read %s: %w", file, err)
		}

		irProg, err := gosx.Compile(source)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("compile %s: %w", file, err)
		}
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
			programs = append(programs, island)
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

func resolveQualifiedComponentImportPaths(projectDir string, aliases []string) ([]string, error) {
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

	goFiles, err := discoverProjectGoFiles(projectDir)
	if err != nil {
		return nil, err
	}
	importSet := map[string]struct{}{}
	for _, file := range goFiles {
		fileImports, err := matchingGoImportPaths(projectDir, file, aliasSet)
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

func discoverProjectGoFiles(projectDir string) ([]string, error) {
	var files []string
	if err := filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && shouldSkipProjectDir(info.Name()) {
			return filepath.SkipDir
		}
		if !info.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk go files: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func matchingGoImportPaths(projectDir, file string, aliases map[string]struct{}) ([]string, error) {
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
		if !shouldResolveImportedPackage(importPath) {
			continue
		}
		info, err := goListPackage(projectDir, importPath)
		if err != nil {
			continue
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

func resolveImportedGSXPackageDirs(projectDir string, importPaths []string) ([]string, error) {
	projectDir = filepath.Clean(projectDir)
	seen := map[string]struct{}{}
	var dirs []string

	for _, importPath := range importPaths {
		if !shouldResolveImportedPackage(importPath) {
			continue
		}
		info, err := goListPackage(projectDir, importPath)
		if err != nil {
			return nil, err
		}
		if info.Standard || info.Dir == "" {
			continue
		}

		dir := filepath.Clean(info.Dir)
		if isPathWithin(dir, projectDir) {
			continue
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		files, err := packageGSXFiles(dir)
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			continue
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}

	sort.Strings(dirs)
	return dirs, nil
}

func shouldResolveImportedPackage(importPath string) bool {
	importPath = strings.TrimSpace(importPath)
	if importPath == "" || strings.HasPrefix(importPath, ".") {
		return false
	}
	first := importPath
	if slash := strings.IndexByte(importPath, '/'); slash >= 0 {
		first = importPath[:slash]
	}
	return strings.Contains(first, ".")
}

type goListPackageInfo struct {
	Dir        string
	ImportPath string
	Name       string
	Standard   bool
}

func goListPackage(projectDir, importPath string) (goListPackageInfo, error) {
	cmd := exec.Command("go", "list", "-json", importPath)
	cmd.Dir = projectDir
	cmd.Env = append(execEnvWithoutGoFlags(), "GOFLAGS="+goModuleCommandFlags, "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		return goListPackageInfo{}, fmt.Errorf("resolve imported package %s: %w", importPath, err)
	}
	var info goListPackageInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return goListPackageInfo{}, fmt.Errorf("decode go list for %s: %w", importPath, err)
	}
	return info, nil
}

func packageGSXFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read package dir %s: %w", dir, err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".gsx") {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	return files, nil
}

func isPathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != "" && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
