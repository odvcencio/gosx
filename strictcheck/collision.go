package strictcheck

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/transpile"
)

// packageDecl is one package-level Go declaration: where it is written, and
// which keyword wrote it.
type packageDecl struct {
	kind string
	span ir.Span
}

// validatePackageDeclCollisions reports every package-level name a strict
// .gsx file projects into Go that a sibling .go file of the same package
// already declares (gosx#230, ask 3).
//
// A strict `component MatchupCard` compiles to `func MatchupCard(...)
// gosx.Node`, and a .gsx `type MatchupCardProps struct` compiles to itself.
// Both land in the package the sibling .go files share. Before this check,
// a name written on both sides reached the Go compiler as
// "MatchupCard redeclared in this block", pointing at a temporary projection
// file the author never wrote and cannot open. This runs first and names
// both declarations with their own source positions instead.
//
// The check reads the projection rather than the .gsx source, so it sees
// exactly the declarations the Go compiler is about to see, and it reads
// their positions through the projection's //line directives, so every
// diagnostic points into the .gsx file the author wrote.
//
// It reports only what it can prove. A sibling .go file that does not
// parse, does not belong to this package, or does not build for the current
// platform is skipped: such a file contributes no declaration to the
// package the projection joins, so a diagnostic about it would be a false
// positive. A missed collision keeps the Go compiler's own message, which
// is where this rule started.
func validatePackageDeclCollisions(files []transpile.PackageFile, generated map[string]string) error {
	if len(files) == 0 || len(generated) == 0 {
		return nil
	}
	packageName := ""
	for _, file := range files {
		if file.Program != nil && file.Program.Package != "" {
			packageName = file.Program.Package
			break
		}
	}
	if packageName == "" {
		return nil
	}
	dir := filepath.Dir(files[0].Path)
	siblings := siblingGoDecls(dir, packageName)
	if len(siblings) == 0 {
		return nil
	}

	sourcePaths := make([]string, 0, len(generated))
	for sourcePath := range generated {
		sourcePaths = append(sourcePaths, sourcePath)
	}
	sort.Strings(sourcePaths)

	var diagnostics []ir.Diagnostic
	for _, sourcePath := range sourcePaths {
		projected := projectedPackageDecls(sourcePath, generated[sourcePath])
		names := make([]string, 0, len(projected))
		for name := range projected {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			sibling, clashes := siblings[name]
			if !clashes {
				continue
			}
			mine := projected[name]
			diagnostics = append(diagnostics, ir.Diagnostic{
				Span: mine.span,
				Message: fmt.Sprintf(
					"%s %s collides with %s %s declared at %s; a strict .gsx file projects this declaration into the package as ordinary Go, and a Go package declares each name once",
					mine.kind, name, sibling.kind, name, formatDeclPosition(sibling.span),
				),
				Hint: "rename one side; a converter type beside a component is commonly named " + name + "Data",
			})
		}
	}
	return ir.NewDiagnosticsError("strict package", diagnostics)
}

// formatDeclPosition renders a sibling declaration's position. Every file in
// a package shares one directory, so the base name identifies it without the
// path noise a full absolute path adds to every diagnostic.
func formatDeclPosition(span ir.Span) string {
	file := filepath.Base(span.File)
	if file == "" || file == "." {
		file = span.File
	}
	return file + ":" + strconv.Itoa(span.StartLine) + ":" + strconv.Itoa(span.StartCol)
}

// projectedPackageDecls parses one generated projection and returns every
// package-level name it declares, positioned through the projection's
// //line directives so each span points into the .gsx source. sourcePath
// names the .gsx file the projection came from; it is used only as the
// parser's nominal file name, which the //line directives then override.
func projectedPackageDecls(sourcePath, projection string) map[string]packageDecl {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, sourcePath, projection, parser.SkipObjectResolution)
	if err != nil {
		// The projection is gosx's own output. A parse failure here is a
		// transpiler defect, not an author error, and the Go compiler stage
		// that follows reports it with its own message.
		return nil
	}
	return collectPackageDecls(fset, file, "strict component")
}

// siblingGoDecls returns every package-level declaration in the hand-written
// .go files of dir that belong to package packageName and build for the
// current platform. Test files are excluded: `go list -export .` compiles
// the projection against the non-test package only.
func siblingGoDecls(dir, packageName string) map[string]packageDecl {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	context := build.Default
	context.UseAllFiles = false
	decls := make(map[string]packageDecl)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		matched, err := context.MatchFile(dir, name)
		if err != nil || !matched {
			continue
		}
		path := filepath.Join(dir, name)
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil || file.Name == nil || file.Name.Name != packageName {
			continue
		}
		for declName, decl := range collectPackageDecls(fset, file, "func") {
			if _, seen := decls[declName]; seen {
				continue
			}
			decls[declName] = decl
		}
	}
	return decls
}

// collectPackageDecls records every package-level name file declares, with
// the keyword that declared it. funcKind names what a func declaration is
// called in a diagnostic: a projection's funcs are strict components in the
// author's source, while a sibling .go file's funcs are plain funcs. Methods
// (a func with a receiver) declare no package-level name and are skipped,
// and so is the blank identifier.
func collectPackageDecls(fset *token.FileSet, file *ast.File, funcKind string) map[string]packageDecl {
	decls := make(map[string]packageDecl)
	record := func(ident *ast.Ident, kind string) {
		if ident == nil || ident.Name == "" || ident.Name == "_" {
			return
		}
		if _, seen := decls[ident.Name]; seen {
			return
		}
		decls[ident.Name] = packageDecl{kind: kind, span: declSpan(fset, ident)}
	}
	for _, decl := range file.Decls {
		switch typed := decl.(type) {
		case *ast.FuncDecl:
			if typed.Recv != nil {
				continue
			}
			record(typed.Name, funcKind)
		case *ast.GenDecl:
			for _, spec := range typed.Specs {
				switch typedSpec := spec.(type) {
				case *ast.TypeSpec:
					record(typedSpec.Name, "type")
				case *ast.ValueSpec:
					kind := "var"
					if typed.Tok == token.CONST {
						kind = "const"
					}
					for _, ident := range typedSpec.Names {
						record(ident, kind)
					}
				}
			}
		}
	}
	return decls
}

func declSpan(fset *token.FileSet, ident *ast.Ident) ir.Span {
	position := fset.Position(ident.Pos())
	column := position.Column
	if column <= 0 {
		// A projection position resolved through a `//line file:line`
		// directive carries no column (go/token reports 0 for unknown).
		// Report the start of the line rather than a column zero no editor
		// can place a caret on.
		column = 1
	}
	return ir.Span{
		File:      position.Filename,
		StartLine: position.Line,
		StartCol:  column,
		EndLine:   position.Line,
		EndCol:    column + len(ident.Name),
	}
}
