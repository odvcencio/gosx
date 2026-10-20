package typeoracle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/transpile"
)

// Options controls the Go command environment Session shells out to when it
// resolves import names and dependency export data. It mirrors
// strictcheck.Options' identically named fields deliberately: both packages
// drive the same "go list" idiom for the same reason (module-aware
// resolution with no new dependency), and a caller already holding a
// strictcheck.Options value can copy these three fields across unchanged.
type Options struct {
	// Env, when non-nil, replaces the process environment for every "go"
	// invocation Session makes. A nil Env uses os.Environ().
	Env []string

	// GOWORK, when non-empty, is set on every "go" invocation's
	// environment. Set it to "off" when the caller's own working tree
	// sits under an ancestor directory Go's workspace auto-detection
	// would otherwise pick up (see the package doc's Importer choice
	// section neighbor, strictcheck/check.go's identical field, for the
	// same rationale).
	GOWORK string

	// GOFLAGS, when non-empty, is set on every "go" invocation's
	// environment.
	GOFLAGS string
}

// Session is one package directory's go/types result: every diagnostic
// go/types reported, mapped back to .gsx source positions, and a query API
// over the resulting *types.Package for a caller that needs a specific type
// fact rather than a full diagnostic sweep.
//
// A Session with a nil Package is a legitimate, non-error result: the
// directory had no strict .gsx component to project (see
// transpile.TranspilePackageWithSharedImports), so there is nothing for
// go/types to check. Every query method reports ok=false against a nil
// Package rather than panicking.
type Session struct {
	// Dir is the absolute directory this session was loaded from.
	Dir string

	// Fset is the file set every position in Diagnostics, Info, and
	// Package was recorded against.
	Fset *token.FileSet

	// Package is the type-checked package, or nil if the directory
	// projected no strict component.
	Package *types.Package

	// Info carries the full go/types side tables (Types, Defs, Uses,
	// Selections, Scopes) for the checked files, or a zero Info if
	// Package is nil.
	Info *types.Info

	// Diagnostics is every go/types error, mapped to its .gsx source
	// position through the projection's "//line" directives. Empty means
	// the package type-checked cleanly (or there was nothing to check).
	Diagnostics []ir.Diagnostic

	// Files is the .gsx package this session loaded, exactly as
	// transpile.LoadPackage(Dir) or LoadPackageDir returned it. A query
	// that needs a component's declared props type name reads it from
	// here (Files[i].Program.Components[j].PropsType) rather than
	// re-deriving it.
	Files []transpile.PackageFile

	// componentProps maps a strict component's name to its declared
	// props type name, gathered from Files at Load time so PropsType
	// does not re-walk Files on every call.
	componentProps map[string]string
}

// Load type-checks the package directory dir: every .gsx file in dir
// belonging to one package (transpile.LoadPackageDir's selection rule) plus
// dir's sibling .go files. See LoadWithOptions for the Go command
// environment this uses.
func Load(ctx context.Context, dir string) (*Session, error) {
	return LoadWithOptions(ctx, dir, Options{})
}

// LoadWithOptions is Load with an explicit Go command environment.
func LoadWithOptions(ctx context.Context, dir string, opts Options) (*Session, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	files, err := transpile.LoadPackageDir(abs)
	if err != nil {
		return nil, err
	}
	return loadFiles(ctx, abs, files, opts)
}

// LoadFile type-checks the .gsx package containing path — every .gsx file
// in path's directory that shares path's package (transpile.LoadPackage's
// selection rule) plus that directory's sibling .go files.
func LoadFile(ctx context.Context, path string) (*Session, error) {
	return LoadFileWithOptions(ctx, path, Options{})
}

// LoadFileWithOptions is LoadFile with an explicit Go command environment.
func LoadFileWithOptions(ctx context.Context, path string, opts Options) (*Session, error) {
	files, err := transpile.LoadPackage(path)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	return loadFiles(ctx, abs, files, opts)
}

func loadFiles(ctx context.Context, dir string, files []transpile.PackageFile, opts Options) (*Session, error) {
	session := &Session{
		Dir:            dir,
		Fset:           token.NewFileSet(),
		Files:          files,
		componentProps: componentPropsByName(files),
	}
	if len(files) == 0 {
		return session, nil
	}

	importNames, err := resolveImportNames(ctx, files, opts)
	if err != nil {
		return nil, fmt.Errorf("typeoracle: resolve import names: %w", err)
	}
	generated, err := transpile.TranspilePackageWithSharedImports(files, importNames, nil)
	if err != nil {
		return nil, fmt.Errorf("typeoracle: project package: %w", err)
	}
	if len(generated) == 0 {
		// No strict component: nothing go/types can usefully check (see
		// the package doc's Scope of this slice section). Not an error.
		return session, nil
	}

	packageName := files[0].Program.Package

	var astFiles []*ast.File
	sourcePaths := make([]string, 0, len(generated))
	for sourcePath := range generated {
		sourcePaths = append(sourcePaths, sourcePath)
	}
	sort.Strings(sourcePaths)
	for _, sourcePath := range sourcePaths {
		file, err := parser.ParseFile(session.Fset, sourcePath, generated[sourcePath], parser.SkipObjectResolution)
		if err != nil {
			// The projection is transpile's own output; a parse failure
			// here is a transpiler defect, not an author error. Report it
			// as a diagnostic rather than failing the whole session, so a
			// sibling file's real findings still surface.
			session.Diagnostics = append(session.Diagnostics, ir.Diagnostic{
				Span:    ir.Span{File: sourcePath},
				Message: fmt.Sprintf("typeoracle: projected Go for %s does not parse: %v", sourcePath, err),
			})
			continue
		}
		astFiles = append(astFiles, file)
	}

	siblings, err := siblingGoFiles(session.Fset, dir, packageName)
	if err != nil {
		return nil, fmt.Errorf("typeoracle: read sibling .go files: %w", err)
	}
	astFiles = append(astFiles, siblings...)
	if len(astFiles) == 0 {
		return session, nil
	}

	lookup, err := newExportLookup(ctx, dir, generated, opts)
	if err != nil {
		return nil, fmt.Errorf("typeoracle: resolve dependency export data: %w", err)
	}

	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Instances:  make(map[*ast.Ident]types.Instance),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Implicits:  make(map[ast.Node]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Scopes:     make(map[ast.Node]*types.Scope),
	}
	conf := &types.Config{
		Importer: importerForCompiler(session.Fset, lookup),
		Error: func(err error) {
			session.Diagnostics = append(session.Diagnostics, diagnosticFromTypesError(err))
		},
	}
	pkg, _ := conf.Check(packageName, session.Fset, astFiles, info)
	sortDiagnostics(session.Diagnostics)
	session.Package = pkg
	session.Info = info
	return session, nil
}

// componentPropsByName gathers every strict component's declared props type
// name across files, keyed by component name. A component with no declared
// props type is omitted (PropsType reports ok=false for it, the same as a
// name it has never heard of).
func componentPropsByName(files []transpile.PackageFile) map[string]string {
	props := make(map[string]string)
	for _, file := range files {
		if file.Program == nil {
			continue
		}
		for _, component := range file.Program.Components {
			if strings.TrimSpace(component.PropsType) == "" {
				continue
			}
			props[component.Name] = component.PropsType
		}
	}
	return props
}

// sortDiagnostics orders diagnostics by file, then line, then column, so a
// caller printing them gets a stable, source-order report rather than
// go/types' own (non-deterministic across Config.Error callback timing when
// several files are checked) order.
func sortDiagnostics(diags []ir.Diagnostic) {
	sort.SliceStable(diags, func(i, j int) bool {
		a, b := diags[i].Span, diags[j].Span
		if a.File != b.File {
			return a.File < b.File
		}
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		return a.StartCol < b.StartCol
	})
}

// resolveImportNames asks the Go tool for every unaliased import's real
// package identifier (an import path does not always match its declared
// package name, e.g. github.com/redis/go-redis/v9 declares package redis),
// the same idiom strictcheck.resolveImportNames uses for the same reason:
// transpile needs the real name to emit a resolvable selector for a call
// through that import.
func resolveImportNames(ctx context.Context, files []transpile.PackageFile, opts Options) (map[string]string, error) {
	imports := transpile.UnaliasedImportPaths(files)
	if len(imports) == 0 || len(files) == 0 {
		return nil, nil
	}
	args := []string{"list", "-e", "-find", "-buildvcs=false", "-f={{if not .Error}}{{.ImportPath}}{{\"\\t\"}}{{.Name}}{{end}}"}
	if !hasModuleModeFlag(opts.GOFLAGS) {
		args = append(args, "-mod=readonly")
	}
	args = append(args, imports...)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = filepath.Dir(files[0].Path)
	cmd.Env = commandEnv(opts)
	out, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("resolve import names: %w", err)
	}
	names := make(map[string]string)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(fields) == 2 && fields[0] != "" && fields[1] != "" {
			names[fields[0]] = fields[1]
		}
	}
	return names, nil
}

func hasModuleModeFlag(goFlags string) bool {
	for _, field := range strings.Fields(goFlags) {
		if strings.HasPrefix(field, "-mod=") {
			return true
		}
	}
	return false
}

func commandEnv(opts Options) []string {
	base := opts.Env
	if base == nil {
		base = os.Environ()
	}
	env := append([]string(nil), base...)
	if opts.GOWORK != "" {
		env = setEnvVar(env, "GOWORK", opts.GOWORK)
	}
	if opts.GOFLAGS != "" {
		env = setEnvVar(env, "GOFLAGS", opts.GOFLAGS)
	}
	return env
}

func setEnvVar(env []string, key, value string) []string {
	prefix := key + "="
	out := env[:0]
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return append(out, prefix+value)
}

// siblingGoFiles parses every hand-written .go file of dir that belongs to
// package packageName and builds for the current platform — the file set
// go/types must join with the .gsx projection for a same-package sibling
// type (a props struct declared beside its component, gosx#230's own
// scenario) to resolve at all. Test files are excluded: a strict
// component's own package build never includes them. Platform matching
// (a "_linux.go" suffix, a "//go:build" constraint) uses go/build.Default,
// the identical rule strictcheck/collision.go's siblingGoDecls applies for
// the identical reason: a file this platform's real build would skip must
// not contribute a phantom declaration or a phantom conflict here either.
func siblingGoFiles(fset *token.FileSet, dir, packageName string) ([]*ast.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	buildContext := build.Default
	buildContext.UseAllFiles = false
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		matched, err := buildContext.MatchFile(dir, name)
		if err != nil || !matched {
			continue
		}
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil || file.Name == nil || file.Name.Name != packageName {
			continue
		}
		files = append(files, file)
	}
	return files, nil
}

// exportEntry is one line of `go list -export -deps -json`'s NDJSON stream:
// enough of the go/packages.list schema to resolve an import path to its
// compiled export data file.
type exportEntry struct {
	ImportPath string
	Export     string
}

// newExportLookup runs `go list -export -deps -json .` once for dir and
// returns a go/importer.Lookup backed by the resulting import-path ->
// export-data-file map (see the package doc's Importer choice section for
// why this, and not go/importer's GOPATH-only fallback or an x/tools
// dependency, is the resolver).
//
// `go list` resolves "." by reading real files from dir, and a directory
// holding only .gsx source (no hand-written .go file at all) is not a
// buildable package as far as `go list` is concerned — a legitimate shape
// for a small strict-only page directory. generated (the same projection
// Session already parsed in memory) is written to a -overlay so `go list`
// sees a real, buildable package there without this package ever writing a
// permanent file into the author's own tree, the identical technique
// strictcheck's own goCheck uses (strictcheck/check.go) to feed the same
// projection to the real Go compiler. The overlay's temp directory is
// removed before this function returns: every Export path `go list` reports
// back points into the Go build cache, never into the overlay itself, so
// nothing under it needs to survive past this one command.
func newExportLookup(ctx context.Context, dir string, generated map[string]string, opts Options) (func(path string) (io.ReadCloser, error), error) {
	overlayPath, cleanup, err := writeOverlay(dir, generated)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// -e: report a build error per package instead of aborting the whole
	// command. Without it, a type error in "." itself (exactly the finding
	// this package exists to surface) would make `go list` fail closed and
	// this function would report no dependency export data at all, even
	// for a healthy dependency the checked package's own error has nothing
	// to do with.
	args := []string{"list", "-e", "-export", "-deps", "-json", "-buildvcs=false", "-overlay", overlayPath}
	if !hasModuleModeFlag(opts.GOFLAGS) {
		args = append(args, "-mod=readonly")
	}
	args = append(args, ".")
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Env = commandEnv(opts)
	out, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("go list -export: %w\n%s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("go list -export: %w", err)
	}
	exports := make(map[string]string)
	// `go list -json` with several packages in scope (-deps here) prints
	// one JSON object per package back to back, not a JSON array — decode
	// it as a stream. exportEntry reads only ImportPath and Export, so
	// every other field in a record is simply ignored by encoding/json.
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var entry exportEntry
		if err := dec.Decode(&entry); err != nil {
			return nil, fmt.Errorf("decode go list -export output: %w", err)
		}
		if entry.ImportPath == "" || entry.Export == "" {
			// A package with no compiled export data (e.g. the target
			// package itself before compilation, or a package with build
			// errors) is skipped; an import that actually needs it fails
			// at Import(path) with a clear "package not found", not here.
			continue
		}
		exports[entry.ImportPath] = entry.Export
	}
	return func(path string) (io.ReadCloser, error) {
		file, ok := exports[path]
		if !ok {
			return nil, fmt.Errorf("no export data for %q (not a dependency of %s, or it failed to build)", path, dir)
		}
		return os.Open(file)
	}, nil
}

// writeOverlay writes every generated projection to a real temp file and
// returns the path to a `go list -overlay`-shaped JSON file mapping a
// virtual .go path inside dir to it, plus a cleanup func that removes the
// temp directory. It never touches dir itself. See newExportLookup's doc
// comment for why this overlay exists.
func writeOverlay(dir string, generated map[string]string) (string, func(), error) {
	tempDir, err := os.MkdirTemp("", "gosx-typeoracle-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(tempDir) }

	sourcePaths := make([]string, 0, len(generated))
	for sourcePath := range generated {
		sourcePaths = append(sourcePaths, sourcePath)
	}
	sort.Strings(sourcePaths)

	overlay := make(map[string]string, len(sourcePaths))
	used := make(map[string]struct{}, len(sourcePaths))
	for i, sourcePath := range sourcePaths {
		tempPath := filepath.Join(tempDir, "projection_"+strconv.Itoa(i)+".go")
		if err := os.WriteFile(tempPath, []byte(generated[sourcePath]), 0o600); err != nil {
			cleanup()
			return "", nil, err
		}
		virtual := uniqueVirtualGoPath(dir, i, used)
		used[virtual] = struct{}{}
		overlay[virtual] = tempPath
	}
	overlayPath := filepath.Join(tempDir, "overlay.json")
	data, err := json.Marshal(struct {
		Replace map[string]string `json:"Replace"`
	}{Replace: overlay})
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if err := os.WriteFile(overlayPath, data, 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	return overlayPath, cleanup, nil
}

// uniqueVirtualGoPath names a virtual .go file inside dir that collides
// with neither a real file already there nor a name already claimed by an
// earlier entry in this same overlay — strictcheck's own goCheck
// (strictcheck/check.go) uses the identical naming rule for the identical
// reason.
func uniqueVirtualGoPath(dir string, index int, used map[string]struct{}) string {
	for suffix := index; ; suffix++ {
		candidate := filepath.Join(dir, "zz_gosx_typeoracle_"+strconv.Itoa(suffix)+".go")
		_, alreadyUsed := used[candidate]
		if _, err := os.Stat(candidate); !alreadyUsed && os.IsNotExist(err) {
			return candidate
		}
	}
}
