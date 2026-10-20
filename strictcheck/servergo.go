package strictcheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// serverGoDir holds every *.server.go file parsed from one file-routed page
// directory. Both the form-action-registry check (gosx#249 check 1) and the
// data-loader-keys check (gosx#249 check 4) read the same
// route.FileModuleOptions / route.FileModule composite literal -- one
// registered field (Actions) for check 1, two more (Load, Bindings) for
// check 4 -- so this is one shared parse rather than two.
//
// Both checks fail closed on anything this type cannot represent
// confidently: parseErr covers a *.server.go file this package's own
// go/parser cannot read (a real syntax error the Go compiler will also
// reject, so no check needs to diagnose it a second time).
type serverGoDir struct {
	dir      string
	fset     *token.FileSet
	files    []*ast.File
	paths    []string
	parseErr error
}

// loadServerGoDir parses every "*.server.go" file directly inside dir (no
// recursion -- a file-routed page directory's own server file always sits
// beside its .gsx page, never in a subdirectory). Files named "*_test.go"
// are excluded; a "*.server.go" name never matches that suffix in practice,
// but the exclusion costs nothing and keeps the scan test-safe.
func loadServerGoDir(dir string) serverGoDir {
	result := serverGoDir{dir: dir, fset: token.NewFileSet()}
	entries, err := os.ReadDir(dir)
	if err != nil {
		result.parseErr = err
		return result
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".server.go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(result.fset, path, nil, 0)
		if err != nil {
			result.parseErr = err
			return result
		}
		result.files = append(result.files, file)
		result.paths = append(result.paths, path)
	}
	return result
}

// compositeLitTypeName returns the bare type name a composite literal
// names, unqualified by any package selector ("route.FileModuleOptions"
// and a dot-imported "FileModuleOptions" both return "FileModuleOptions").
// An elided-type literal (legal only when nested inside another literal of
// known element type, which is not a shape route.FileModuleOptions ever
// appears in as a value) returns "".
func compositeLitTypeName(lit *ast.CompositeLit) string {
	switch t := lit.Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	default:
		return ""
	}
}

// fileModuleField returns the value expression assigned to name within
// lit's fields, and whether that field was written at all. A field never
// written is the same as an explicit Go zero value (nil) to every caller
// here -- FileModuleOptions has no non-nil zero value for Actions, Load,
// or Bindings, so "absent" and "present as nil" are one case, not two.
func fileModuleField(lit *ast.CompositeLit, name string) (ast.Expr, bool) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != name {
			continue
		}
		return kv.Value, true
	}
	return nil, false
}

// isNilExpr reports whether expr is the bare identifier "nil".
func isNilExpr(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "nil"
}

// literalStringMapKeys resolves a composite literal to its complete
// literal string key set. ok is false when any element is not a
// string-keyed key:value pair -- a spread-like bare-value element, a
// non-literal (computed) key, or a non-string key -- since any of those
// means the true key set is not what this function can read off the
// syntax alone.
func literalStringMapKeys(lit *ast.CompositeLit) (map[string]bool, bool) {
	keys := make(map[string]bool, len(lit.Elts))
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return nil, false
		}
		basic, ok := kv.Key.(*ast.BasicLit)
		if !ok || basic.Kind != token.STRING {
			return nil, false
		}
		name, err := strconv.Unquote(basic.Value)
		if err != nil {
			return nil, false
		}
		keys[name] = true
	}
	return keys, true
}

// fileTopLevelCompositeVars maps every package-level "var name = <composite
// literal>" (and "var name = TypeName{...}") declaration in file to that
// literal, by name. This resolves the one indirection level real code
// actually uses -- a FileActions or a data map built as its own named
// variable next to the registration call -- without attempting to trace
// assignment through arbitrary control flow, which is exactly the
// unbounded-analysis shape gosx#249 says to abstain from instead of
// guessing at.
func fileTopLevelCompositeVars(file *ast.File) map[string]*ast.CompositeLit {
	vars := make(map[string]*ast.CompositeLit)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != len(value.Values) {
				continue
			}
			for i, name := range value.Names {
				if lit, ok := value.Values[i].(*ast.CompositeLit); ok {
					vars[name.Name] = lit
				}
			}
		}
	}
	return vars
}

// funcDeclsByName maps every top-level function name declared across
// files to its declaration -- used to resolve a Load field written as a
// bare identifier (Load: loadDashboardData) to the function it names, which
// may live in a *.server.go file other than the one registering it.
func funcDeclsByName(files []*ast.File) map[string]*ast.FuncDecl {
	funcs := make(map[string]*ast.FuncDecl)
	for _, file := range files {
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name != nil {
				funcs[fn.Name.Name] = fn
			}
		}
	}
	return funcs
}

// funcDeclsAcrossDirs unions funcDeclsByName over every "*.server.go" file
// in each of dirs -- a Load field written as a bare identifier can name a
// function declared in a sibling "*.server.go" file, not only the one
// holding the registration call itself. A directory whose files fail to
// parse simply contributes nothing; it does not abort the scan, since an
// unrelated directory's parse failure should not hide a resolvable
// function declared in another.
func funcDeclsAcrossDirs(dirs []string) map[string]*ast.FuncDecl {
	funcs := make(map[string]*ast.FuncDecl)
	for _, dir := range dirs {
		parsed := loadServerGoDir(dir)
		if parsed.parseErr != nil {
			continue
		}
		for name, fn := range funcDeclsByName(parsed.files) {
			funcs[name] = fn
		}
	}
	return funcs
}

// fileModuleRegistration is one route.FileModuleOptions/FileModule
// composite literal this scan found while reading "*.server.go" files,
// resolved to the exact .gsx (or .html) source path it registers for.
type fileModuleRegistration struct {
	// target is the clean absolute path this literal registers for.
	target string
	lit    *ast.CompositeLit
	// vars is the declaring file's own package-level composite-literal
	// variables, needed to resolve an Actions/Load field written as a
	// local identifier rather than an inline literal.
	vars map[string]*ast.CompositeLit
}

// candidateServerGoDirs returns gsxDir and its immediate parent directory
// (deduplicated, and only the parent when it differs from gsxDir) -- the
// two places a "*.server.go" file registering gsxDir's page can live.
// Beside gsxDir is the ordinary RegisterFileModuleHere convention; one
// level up is the explicit route.FileModuleFor(source, opts) pattern a
// shared parent server file uses to register for a dynamic child route
// segment (gosx#249 confirmed this against
// examples/goetrope-watch/app/watch/page.server.go, which registers for
// app/watch/[code]/page.gsx this exact way). This scan does not search
// beyond one level up: each additional level raises the chance of
// wrongly attributing a distant, unrelated page's registration to this
// one, which is exactly the false-positive risk gosx#249 asks every
// check here to avoid.
func candidateServerGoDirs(gsxDir string) []string {
	parent := filepath.Dir(gsxDir)
	if parent == gsxDir {
		return []string{gsxDir}
	}
	return []string{gsxDir, parent}
}

// hasUnrenderedServerGoTemplate reports whether any of dirs contains a
// "*.server.gotmpl" file: a scaffold's own unrendered Go source for what
// becomes a "*.server.go" file only once rendered (by `gosx init`, or a
// project's own generator) -- never before. Its real Actions/Load content
// is template syntax, not Go, so this scan cannot read it, and "found no
// *.server.go here" must not be read as "confidently registers nothing"
// when one of these sits right beside where that file would be.
//
// gosx#249 confirmed this exact shape in
// cmd/gosx/templates/docs/app/docs/forms/page.server.gotmpl and its six
// siblings: every one of them registers real Actions and a real Load once
// `gosx init` renders it, and check 1 and check 4 were reporting all
// fourteen of those pages as broken before this existed — a scaffold gosx
// itself ships, found broken by the very tool meant to keep a real
// application from shipping broken, is exactly the "first impression is a
// wall of noise" outcome that gets a check turned off.
func hasUnrenderedServerGoTemplate(dirs []string) bool {
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".server.gotmpl") {
				return true
			}
		}
	}
	return false
}

// collectFileModuleRegistrations scans every "*.server.go" file in each of
// dirs and resolves every route.FileModuleOptions/FileModule registration
// it finds to its exact target .gsx path: RegisterFileModuleHere,
// MustRegisterFileModuleHere, and FileModuleHere infer the sibling source
// path from the calling file (see impliedFileModuleTarget); FileModuleFor
// takes its target as an explicit first argument, resolved the same way
// resolvePathExpr resolves a router.AddDir target (see that function's own
// doc comment in routemount.go for exactly which source-expression shapes
// resolve).
//
// ok is false if any "*.server.go" file in dirs failed to parse, OR if any
// of them builds a route.FileModuleOptions/FileModule literal through a
// call shape this scan does not recognize by name (a project's own helper
// wrapping FileModuleOptions, for example) -- gosx#249 confirmed this
// shape is real too, against
// examples/gosx-docs/app/demos/water/page.server.go's
// docsapp.RegisterStaticDocsPage helper. Such a literal is real evidence
// of a registration this scan simply cannot target precisely, so treating
// "found no recognized registration" as "confidently registers nothing"
// would be a false positive waiting to happen for every directory holding
// one -- ok=false here makes every caller abstain instead.
func collectFileModuleRegistrations(dirs []string) ([]fileModuleRegistration, bool) {
	var out []fileModuleRegistration
	seen := make(map[string]bool, len(dirs))
	for _, dir := range dirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		parsed := loadServerGoDir(dir)
		if parsed.parseErr != nil {
			return nil, false
		}
		for i, file := range parsed.files {
			regs, orphan := fileModuleRegistrationsInFile(file, parsed.paths[i])
			if orphan {
				return nil, false
			}
			out = append(out, regs...)
		}
	}
	return out, true
}

// fileModuleRegistrationsInFile scans one file for every recognized
// FileModuleOptions/FileModule registration call, and separately for any
// FileModuleOptions/FileModule composite literal this scan can find but
// could not attribute to one of those recognized calls (orphan=true) --
// see collectFileModuleRegistrations' own doc comment for why an orphan
// forces every caller to abstain.
func fileModuleRegistrationsInFile(file *ast.File, path string) (regs []fileModuleRegistration, orphan bool) {
	vars := fileTopLevelCompositeVars(file)
	pathVars := resolvePathVarsInFile(file, path)
	fileDir := filepath.Dir(path)
	consumed := make(map[*ast.CompositeLit]bool)

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch calleeName(call.Fun) {
		case "RegisterFileModuleHere", "MustRegisterFileModuleHere", "FileModuleHere":
			if len(call.Args) != 1 {
				return true
			}
			lit, ok := asFileModuleOptionsLit(call.Args[0], vars)
			if !ok {
				return true
			}
			consumed[lit] = true
			regs = append(regs, fileModuleRegistration{
				target: impliedFileModuleTarget(path),
				lit:    lit,
				vars:   vars,
			})
		case "FileModuleFor":
			if len(call.Args) != 2 {
				return true
			}
			lit, ok := asFileModuleOptionsLit(call.Args[1], vars)
			if !ok {
				return true
			}
			consumed[lit] = true
			target, ok := resolvePathExpr(call.Args[0], fileDir, pathVars)
			if !ok {
				return true
			}
			regs = append(regs, fileModuleRegistration{target: target, lit: lit, vars: vars})
		}
		return true
	})

	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		switch compositeLitTypeName(lit) {
		case "FileModuleOptions", "FileModule":
			if !consumed[lit] {
				orphan = true
			}
		}
		return true
	})
	return regs, orphan
}

func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		if f.Sel != nil {
			return f.Sel.Name
		}
	}
	return ""
}

// asFileModuleOptionsLit resolves expr -- an inline composite literal or
// an identifier naming one of the declaring file's own package-level
// composite-literal variables -- to a route.FileModuleOptions or
// route.FileModule literal.
func asFileModuleOptionsLit(expr ast.Expr, vars map[string]*ast.CompositeLit) (*ast.CompositeLit, bool) {
	var lit *ast.CompositeLit
	switch e := expr.(type) {
	case *ast.CompositeLit:
		lit = e
	case *ast.Ident:
		found, ok := vars[e.Name]
		if !ok {
			return nil, false
		}
		lit = found
	default:
		return nil, false
	}
	switch compositeLitTypeName(lit) {
	case "FileModuleOptions", "FileModule":
		return lit, true
	default:
		return nil, false
	}
}

// impliedFileModuleTarget mirrors route/filemodule.go's own
// fileModuleSourceFromFile: RegisterFileModuleHere and its siblings swap
// a calling "X.server.go" file's own suffix for ".gsx" (preferring
// ".html" when only that sibling exists on disk), registering for
// whichever one is actually present beside it.
func impliedFileModuleTarget(serverGoPath string) string {
	base := strings.TrimSuffix(serverGoPath, ".server.go")
	for _, ext := range []string{".gsx", ".html"} {
		candidate := base + ext
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return filepath.Clean(candidate)
		}
	}
	return filepath.Clean(base + ".gsx")
}
