package strictcheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/route"
)

// validateRouteMountContract is strictcheck's whole-tree page-mount
// contract (gosx#249, check 3): a "page.gsx" or "index.gsx" no
// router.AddDir call reaches is dead code a reader mistakes for live code
// -- exactly the examples/dashboard defect gosx#249's premise table
// names (five file-routed pages main.go never mounted). Unlike checks 1,
// 2, and 4, this one cannot be scoped to one package directory: knowing
// whether a page is reached requires seeing every AddDir call in the
// whole project, so CheckTreeWithOptions calls this once per run, not
// once per package the way runBuiltinChecks' other checks do.
//
// Warning severity: a deliberate work-in-progress page (or a scaffold
// template no main.go anywhere is meant to mount yet, such as
// cmd/gosx/templates/) is legitimate, so this must never fail a build.
func validateRouteMountContract(root string, sources []string, opts Options) {
	if opts.Warnings == nil {
		return
	}
	mountRoots, ok := resolveProjectAddDirRoots(root)
	if !ok || len(mountRoots) == 0 {
		// Either no AddDir call was found anywhere (a project that does not
		// use file routing, or one this scan could not observe using it),
		// or at least one call's target directory could not be resolved
		// with confidence -- and an unresolvable call could mount any
		// directory in the project, including one holding a page this scan
		// would otherwise call unmounted. Stay silent rather than risk that
		// false positive (gosx#249's "only report with confidence" rule).
		return
	}
	mounted := mountedPageSet(mountRoots)
	for _, src := range sources {
		if !isRoutablePageFileName(src) {
			continue
		}
		clean := filepath.Clean(src)
		if mounted[clean] {
			continue
		}
		if hasUnrenderedMainTemplate(filepath.Dir(clean), root) {
			// gosx#249: a "main.gotmpl" between this page and root is a
			// scaffold's own unrendered Go source for what becomes the
			// main.go that calls router.AddDir once rendered -- never
			// before. This scan cannot read a router.AddDir call out of
			// template syntax, so this page is not confidently unmounted;
			// see hasUnrenderedMainTemplate's own doc comment.
			continue
		}
		addWarnings(opts, []ir.Diagnostic{{
			Span:     ir.Span{File: src, StartLine: 1, StartCol: 1},
			Severity: ir.SeverityWarning,
			Message:  fmt.Sprintf("gosx: %s is not reached by any router.AddDir mount found in this project", filepath.Base(src)),
			Hint:     "mount its directory tree with router.AddDir, or remove/rename it if it is intentionally unrouted (a work-in-progress page, a scaffold template, and so on)",
		}})
	}
}

// hasUnrenderedMainTemplate walks from dir up to (and including) root
// looking for a "main.gotmpl" file: a scaffold's own unrendered Go source
// for what becomes the "main.go" that calls router.AddDir only once
// rendered (by `gosx init`, or a project's own generator) -- gosx#249
// confirmed this exact shape in cmd/gosx/templates/docs/main.gotmpl,
// which sits two directories above the seven page.gsx files it would
// mount once rendered. This scan cannot read a router.AddDir call out of
// template syntax, so a page below a directory holding one of these is
// not confidently unmounted -- the same "the Go side is templated here,
// stay silent" rule hasUnrenderedServerGoTemplate applies to checks 1 and
// 4 (servergo.go), applied to check 3's own missing fact (a mount, not a
// registration).
func hasUnrenderedMainTemplate(dir, root string) bool {
	root = filepath.Clean(root)
	dir = filepath.Clean(dir)
	for {
		if info, err := os.Stat(filepath.Join(dir, "main.gotmpl")); err == nil && !info.IsDir() {
			return true
		}
		if dir == root {
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// isRoutablePageFileName reports whether path's base name is one the file
// router treats as a standalone page ("page.gsx"/"index.gsx" and their
// ".html" sibling extension -- see route/filesystem.go's routeFileKind).
// "layout.gsx", "not-found.gsx", and "error.gsx" are structural, not
// routes of their own, so they are out of scope for "is this reachable".
func isRoutablePageFileName(path string) bool {
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	return name == "page" || name == "index"
}

// mountedPageSet resolves every mount root through route.ScanDir -- the
// framework's own file-route discovery, so this can never drift from what
// AddDir would actually mount -- and returns the set of every page source
// path (including special pages) it discovers, clean-path-normalized. A
// root route.ScanDir itself fails to read is skipped, not fatal to the
// whole set.
func mountedPageSet(mountRoots []string) map[string]bool {
	mounted := make(map[string]bool)
	add := func(path string) {
		if path != "" {
			mounted[filepath.Clean(path)] = true
		}
	}
	for _, mountRoot := range mountRoots {
		routes, err := route.ScanDir(mountRoot)
		if err != nil {
			continue
		}
		for _, page := range routes.Pages {
			add(page.FilePath)
		}
		if routes.NotFound != nil {
			add(routes.NotFound.FilePath)
		}
		if routes.Error != nil {
			add(routes.Error.FilePath)
		}
		for _, scope := range routes.NotFoundScopes {
			add(scope.Page.FilePath)
		}
	}
	return mounted
}

// resolveProjectAddDirRoots walks every non-test ".go" file under root
// (using the same directory-skip rules as CheckTreeWithOptions's own .gsx
// walk, so the two stay consistent about what is "in the project") and
// resolves every router.AddDir call it finds to an absolute directory. ok
// is false the moment any call's argument cannot be resolved with
// confidence -- see resolveAddDirRootsInFile's doc comment for exactly
// which shapes are resolvable.
func resolveProjectAddDirRoots(root string) ([]string, bool) {
	var roots []string
	ok := true
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if shouldSkipDir(root, path, entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			// A file this package's own go/parser cannot read is a real Go
			// syntax error the compiler will also reject elsewhere; it is
			// not evidence one way or the other about AddDir mounts, so it
			// is skipped rather than treated as an unresolved call.
			return nil
		}
		fileRoots, unresolved := resolveAddDirRootsInFile(file, path)
		if unresolved {
			ok = false
			return filepath.SkipAll
		}
		roots = append(roots, fileRoots...)
		return nil
	})
	return roots, ok
}

// resolveAddDirRootsInFile finds every "<x>.AddDir(<dirExpr>, ...)" call in
// file and resolves each dirExpr to an absolute directory. unresolved is
// true the moment one call's argument is not one of the two shapes this
// scan can resolve with confidence:
//
//  1. A string literal, resolved relative to file's own directory -- the
//     conventional "AddDir("app", ...)" shape run from the source tree.
//  2. filepath.Join(<X>, "seg", ...) (or a bare <X>) where X is, or
//     resolves through filepath.Dir(...) to, the second result of a
//     "runtime.Caller(0)" call earlier in the SAME file -- the
//     "_, thisFile, _, _ := runtime.Caller(0); root :=
//     filepath.Dir(thisFile)" idiom every real AddDir call in this
//     repository actually uses (gosx#249 confirmed this against
//     examples/basic, examples/dashboard, examples/goetrope-watch,
//     examples/gosx-docs, and cmd/gosx/init.go's own scaffold template
//     before relying on it).
//
// Any other shape -- a variable built some other way, a function
// parameter, string concatenation, os.Getenv, and so on -- is unresolved:
// this scan cannot rule out that call mounting a directory holding a page
// it would otherwise call unmounted, so it must not proceed with a
// possibly-incomplete mounted-page set.
func resolveAddDirRootsInFile(file *ast.File, filePath string) (roots []string, unresolved bool) {
	fileDir := filepath.Dir(filePath)
	vars := resolvePathVarsInFile(file, filePath)
	ast.Inspect(file, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel || sel.Sel == nil || sel.Sel.Name != "AddDir" || len(call.Args) == 0 {
			return true
		}
		dir, ok := resolvePathExpr(call.Args[0], fileDir, vars)
		if !ok {
			unresolved = true
			return false
		}
		roots = append(roots, dir)
		return true
	})
	return roots, unresolved
}

// resolvePathVarsInFile computes, for every local or package-level
// variable in file this scan can trace, the absolute path it resolves to
// at build time. This is the single shared foundation both router.AddDir
// target resolution (resolveAddDirRootsInFile above) and
// route.FileModuleFor source resolution (servergo.go's
// fileModuleRegistrationsInFile) build on: both idioms are ultimately
// "start from runtime.Caller(0)'s own file, walk filepath.Dir/
// filepath.Join calls, optionally through a named intermediate variable,
// to a final directory or file path" -- see resolvePathExpr for exactly
// which expression shapes resolve.
func resolvePathVarsInFile(file *ast.File, filePath string) map[string]string {
	vars := make(map[string]string)

	// Pass 1: "_, thisFile, _, _ := runtime.Caller(0)" binds thisFile to
	// this very file's own build-time path. Only skip level 0 counts --
	// "runtime.Caller(1)" and above name a DIFFERENT file (the caller of
	// the function this scan is reading), which this scan has no way to
	// identify statically, so a non-zero or non-literal skip argument is
	// simply not added.
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 4 || len(assign.Rhs) != 1 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "Caller" {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "runtime" {
			return true
		}
		skip, ok := call.Args[0].(*ast.BasicLit)
		if !ok || skip.Kind != token.INT || skip.Value != "0" {
			return true
		}
		if ident, ok := assign.Lhs[1].(*ast.Ident); ok && ident.Name != "_" {
			vars[ident.Name] = filePath
		}
		return true
	})

	// Pass 2+: fixed point over every plain single-value assignment
	// (":=" or "=") whose right side resolvePathExpr can resolve given
	// the variables resolved so far -- a chain like "root :=
	// filepath.Dir(thisFile); source := filepath.Join(root, "[code]",
	// "page.gsx")" resolves regardless of declaration order in the file.
	fileDir := filepath.Dir(filePath)
	for pass := 0; pass < 8; pass++ {
		before := len(vars)
		ast.Inspect(file, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
				return true
			}
			name, ok := assign.Lhs[0].(*ast.Ident)
			if !ok || name.Name == "_" {
				return true
			}
			if _, already := vars[name.Name]; already {
				return true
			}
			if resolved, ok := resolvePathExpr(assign.Rhs[0], fileDir, vars); ok {
				vars[name.Name] = resolved
			}
			return true
		})
		if len(vars) == before {
			break
		}
	}
	return vars
}

// resolvePathExpr resolves one path-shaped expression to an absolute
// path, given fileDir (the directory holding the file being scanned) and
// vars (every already-resolved variable in that file; see
// resolvePathVarsInFile). Four shapes resolve:
//
//  1. A string literal, resolved relative to fileDir -- the conventional
//     "AddDir("app", ...)" shape run from the source tree.
//  2. An identifier already present in vars.
//  3. filepath.Dir(<expr>), where <expr> itself resolves.
//  4. filepath.Join(<expr>, "seg", ...), where <expr> resolves and every
//     later argument is a string literal -- covers both a directory
//     target ("app") and a full file target ("[code]", "page.gsx"), since
//     this function does not distinguish a directory from a file: it is
//     shared by router.AddDir's directory argument and
//     route.FileModuleFor's file argument alike.
//
// Anything else -- a function call other than filepath.Dir/Join, a
// concatenation, os.Getenv, a struct field, a function parameter, and so
// on -- is unresolved.
func resolvePathExpr(expr ast.Expr, fileDir string, vars map[string]string) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return filepath.Clean(filepath.Join(fileDir, value)), true
	case *ast.Ident:
		resolved, ok := vars[e.Name]
		return resolved, ok
	case *ast.CallExpr:
		sel, ok := e.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil {
			return "", false
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		// server.ResolveAppRoot(callerFile) (server/approot.go) is a second
		// real "find my own source directory" idiom this repository's own
		// examples use beside the bare filepath.Dir(thisFile) one --
		// examples/goetrope-watch and examples/gosx-docs both call it with
		// runtime.Caller(0)'s own file. Its real resolution order also
		// checks GOSX_APP_ROOT and the running executable's own directory
		// first, so treating it as filepath.Dir(callerFile) is a documented
		// assumption, not a certainty: it holds whenever neither override is
		// in play, true for every ordinary `go run`/`gosx build`/`gosx dev`
		// invocation this scan's own callers (gosx build's build gate, `gosx
		// check`) run under. Accepting that bounded risk here roughly
		// doubles this check's real coverage in this repository alone (2 of
		// 4 example apps use it) — a worthwhile trade given check 3 is
		// warning-severity, not error.
		if pkg.Name == "server" && sel.Sel.Name == "ResolveAppRoot" && len(e.Args) == 1 {
			base, ok := resolvePathExpr(e.Args[0], fileDir, vars)
			if !ok {
				return "", false
			}
			return filepath.Dir(base), true
		}
		if pkg.Name != "filepath" {
			return "", false
		}
		switch sel.Sel.Name {
		case "Dir":
			if len(e.Args) != 1 {
				return "", false
			}
			base, ok := resolvePathExpr(e.Args[0], fileDir, vars)
			if !ok {
				return "", false
			}
			return filepath.Dir(base), true
		case "Join":
			if len(e.Args) == 0 {
				return "", false
			}
			base, ok := resolvePathExpr(e.Args[0], fileDir, vars)
			if !ok {
				return "", false
			}
			segments := []string{base}
			for _, arg := range e.Args[1:] {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return "", false
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					return "", false
				}
				segments = append(segments, value)
			}
			return filepath.Clean(filepath.Join(segments...)), true
		default:
			return "", false
		}
	default:
		return "", false
	}
}
