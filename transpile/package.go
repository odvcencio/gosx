package transpile

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
)

// PackageFile is one .gsx source participating in package validation.
type PackageFile struct {
	Path    string
	Source  []byte
	Program *ir.Program
}

// LoadPackage reads and compiles every .gsx file in the same directory and
// package as path. The target file selects the package; an alphabetically
// earlier route helper with a different package can never redirect the load.
func LoadPackage(path string) ([]PackageFile, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	targetSource, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", abs, err)
	}
	targetProgram, err := gosx.Compile(targetSource)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", abs, err)
	}

	entries, err := os.ReadDir(filepath.Dir(abs))
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".gsx") {
			continue
		}
		paths = append(paths, filepath.Join(filepath.Dir(abs), entry.Name()))
	}
	sort.Strings(paths)

	files := make([]PackageFile, 0, len(paths))
	for _, file := range paths {
		source := targetSource
		if !samePackageFile(file, abs) {
			source, err = os.ReadFile(file)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", file, err)
			}
		}
		packageName, ok := sourcePackageName(source)
		if !ok || packageName != targetProgram.Package {
			continue
		}
		program := targetProgram
		if !samePackageFile(file, abs) {
			program, err = gosx.Compile(source)
			if err != nil {
				return nil, fmt.Errorf("compile %s: %w", file, err)
			}
		}
		files = append(files, PackageFile{Path: file, Source: source, Program: program})
	}
	return files, nil
}

func sourcePackageName(source []byte) (string, bool) {
	tree, lang, err := gosx.Parse(source)
	if err != nil {
		return "", false
	}
	root := tree.RootNode()
	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		if child.Type(lang) != "package_clause" {
			continue
		}
		for j := 0; j < int(child.NamedChildCount()); j++ {
			name := child.NamedChild(j)
			if name.Type(lang) == "package_identifier" {
				return string(source[name.StartByte():name.EndByte()]), true
			}
		}
	}
	return "", false
}

// StrictProjection emits one coherent strict-only Go file. The full original
// file is collected before emission so same-file prop schemas, import aliases,
// and strict calls remain visible to one another. Legacy funcs and top-level
// route/data/request DSL declarations never enter the projection. Top-level
// constants are retained because Go types may depend on array lengths and
// other compile-time values; dynamic variables remain excluded.
func StrictProjection(file PackageFile) (string, bool, error) {
	return StrictProjectionWithImportNames(file, nil)
}

// StrictProjectionWithImportNames projects a file using Go-resolved default
// package identifiers. Import paths do not always encode the package name
// (for example github.com/redis/go-redis/v9 declares package redis).
func StrictProjectionWithImportNames(file PackageFile, importNames map[string]string) (string, bool, error) {
	hasStrict := fileHasStrict(file)
	generated, err := Transpile(file.Source, Options{
		SourceFile:       file.Path,
		strictProjection: true,
		importNames:      importNames,
	})
	if err != nil {
		return "", false, err
	}
	return generated, hasStrict, nil
}

// UnaliasedImportPaths returns stable-sorted imports whose package identifier
// must be resolved by Go rather than supplied explicitly in source.
func UnaliasedImportPaths(files []PackageFile) []string {
	paths := make(map[string]struct{})
	for _, packageFile := range files {
		tree, lang, err := gosx.Parse(packageFile.Source)
		if err != nil {
			continue
		}
		root := tree.RootNode()
		for i := 0; i < int(root.NamedChildCount()); i++ {
			child := root.NamedChild(i)
			if child.Type(lang) != "import_declaration" {
				continue
			}
			text := string(packageFile.Source[child.StartByte():child.EndByte()])
			parsed, err := parser.ParseFile(token.NewFileSet(), packageFile.Path, "package gosximports\n"+text, parser.ImportsOnly)
			if err != nil {
				continue
			}
			for _, spec := range parsed.Imports {
				if spec.Name != nil {
					continue
				}
				importPath, err := strconv.Unquote(spec.Path.Value)
				if err == nil {
					paths[importPath] = struct{}{}
				}
			}
		}
	}
	out := make([]string, 0, len(paths))
	for importPath := range paths {
		out = append(out, importPath)
	}
	sort.Strings(out)
	return out
}

func fileHasStrict(file PackageFile) bool {
	if file.Program == nil {
		return false
	}
	for _, component := range file.Program.Components {
		if component.Syntax == ir.ComponentSyntaxStrict {
			return true
		}
	}
	return false
}

// ValidateStrictPackageBoundaries rejects strict component calls across .gsx
// files. The Go projection can type-check such calls, but the current file
// renderer compiles one .gsx file at a time and therefore cannot execute them.
func ValidateStrictPackageBoundaries(files []PackageFile) error {
	strictOwners := make(map[string][]string)
	localNames := make(map[string]map[string]struct{}, len(files))
	for _, file := range files {
		if file.Program == nil {
			continue
		}
		local := make(map[string]struct{}, len(file.Program.Components))
		for _, comp := range file.Program.Components {
			local[comp.Name] = struct{}{}
			if comp.Syntax == ir.ComponentSyntaxStrict {
				strictOwners[comp.Name] = append(strictOwners[comp.Name], file.Path)
			}
		}
		localNames[file.Path] = local
	}
	for _, file := range files {
		if file.Program == nil {
			continue
		}
		for _, node := range file.Program.Nodes {
			if node.Kind != ir.NodeComponent {
				continue
			}
			// The file renderer resolves a local definition first. A same-file
			// legacy component therefore legitimately shadows an identically named
			// strict peer in another file.
			if _, local := localNames[file.Path][node.Tag]; local {
				continue
			}
			for _, strictOwner := range strictOwners[node.Tag] {
				if samePackageFile(strictOwner, file.Path) {
					continue
				}
				return fmt.Errorf("%s: cross-file strict component call <%s> is not supported by the file renderer; keep caller and callee in one .gsx file", file.Path, node.Tag)
			}
		}
	}
	return nil
}

func samePackageFile(a, b string) bool {
	aAbs, _ := filepath.Abs(a)
	bAbs, _ := filepath.Abs(b)
	return filepath.Clean(aAbs) == filepath.Clean(bAbs)
}

// TranspilePackage projects a package after enforcing the current renderer
// boundary. Legacy-only packages are a true no-op. Once a package contains a
// strict component, every same-package file contributes its retained type
// declarations so prop types can live beside legacy templates.
func TranspilePackage(files []PackageFile) (map[string]string, error) {
	return TranspilePackageWithImportNames(files, nil)
}

// TranspilePackageWithImportNames projects a package using package identifiers
// resolved by the Go tool in the target module/workspace.
func TranspilePackageWithImportNames(files []PackageFile, importNames map[string]string) (map[string]string, error) {
	if err := ValidateStrictPackageBoundaries(files); err != nil {
		return nil, err
	}
	hasStrict := false
	for _, file := range files {
		hasStrict = hasStrict || fileHasStrict(file)
	}
	if !hasStrict {
		return map[string]string{}, nil
	}

	out := make(map[string]string, len(files))
	for _, file := range files {
		generated, _, err := StrictProjectionWithImportNames(file, importNames)
		if err != nil {
			return nil, fmt.Errorf("transpile %s: %w", file.Path, err)
		}
		out[file.Path] = generated
	}
	return out, nil
}
