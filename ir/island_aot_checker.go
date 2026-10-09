//go:build !tinygo && !js

package ir

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

//go:embed island_aot_signal.stub
var aotSignalStub string

const aotStubRevision = "signal-api-v1:7efe0270ca59f827910d9b5dcf9e22159092e299d37564cb62612e1a148c83d3"

type aotCheckedSource struct {
	fset             *token.FileSet
	file             *ast.File
	info             *types.Info
	projection       aotCheckingFile
	errors           []types.Error
	functions        map[string]*ast.FuncDecl
	syntheticObjects map[types.Object]bool
}

var aotCheckCache = struct {
	sync.Mutex
	entries map[[32]byte]map[[32]byte]*aotCheckedSource
	order   [][32]byte
}{entries: map[[32]byte]map[[32]byte]*aotCheckedSource{}}

// Read and hash authored bytes before projecting or parsing. A package check
// shares object identities across candidates, but each file retains its own
// source maps. Even edits erased by projection invalidate both kinds of data.
func aotCheckSource(p *Program) (*aotCheckedSource, error) {
	if p.Dir == "" {
		return nil, fmt.Errorf("package_scope_unknown: source directory is required")
	}
	if p.aotBindings == nil || p.aotBindings.project == nil {
		return nil, fmt.Errorf("evidence_shape_mismatch: exact source is missing")
	}
	type member struct {
		name      string
		data      []byte
		candidate bool
	}
	var members []member
	candidateKey := sha256.Sum256(p.aotBindings.source)
	digest := sha256.New()
	add := func(data []byte) { fmt.Fprintf(digest, "%d:", len(data)); digest.Write(data) }
	add([]byte(p.PackagePath))
	add([]byte(p.Dir))
	add([]byte(aotStubRevision))
	add([]byte(fmt.Sprintf("%s/%s/%t/%q/%q", build.Default.GOOS, build.Default.GOARCH, build.Default.CgoEnabled, build.Default.BuildTags, build.Default.ReleaseTags)))
	entries, err := os.ReadDir(p.Dir)
	if err != nil {
		return nil, fmt.Errorf("package_scope_unknown: %w", err)
	}
	found := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_test.gsx") || (filepath.Ext(name) != ".go" && filepath.Ext(name) != ".gsx") {
			continue
		}
		selected, err := aotMatchFile(p.Dir, name)
		if err != nil {
			return nil, fmt.Errorf("package_scope_unknown: %w", err)
		}
		if !selected {
			continue
		}
		data, err := os.ReadFile(filepath.Join(p.Dir, name))
		if err != nil {
			return nil, err
		}
		isCandidate := !found && bytes.Equal(data, p.aotBindings.source)
		found = found || isCandidate
		members = append(members, member{name, data, isCandidate})
		add([]byte(name))
		add(data)
	}
	if !found {
		members = append(members, member{"candidate.gsx", p.aotBindings.source, true})
		add([]byte("candidate.gsx"))
		add(p.aotBindings.source)
	}
	var key [32]byte
	copy(key[:], digest.Sum(nil))
	aotCheckCache.Lock()
	defer aotCheckCache.Unlock()
	if cached := aotCheckCache.entries[key]; cached != nil {
		return cached[candidateKey], nil
	}
	fset := token.NewFileSet()
	var files []*ast.File
	views := map[[32]byte]*aotCheckedSource{}
	functions := map[string]*ast.FuncDecl{}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}, Instances: map[*ast.Ident]types.Instance{}}
	for _, m := range members {
		data := m.data
		var projection aotCheckingFile
		if filepath.Ext(m.name) == ".gsx" {
			memberProgram := p
			if !m.candidate {
				memberProgram, err = p.aotBindings.lower(data)
				if err != nil {
					return nil, fmt.Errorf("package_scope_unknown: %w", err)
				}
			}
			projection, err = memberProgram.aotBindings.project(memberProgram)
			if err != nil {
				return nil, err
			}
			data = projection.bytes
		}
		file, err := parser.ParseFile(fset, m.name, data, parser.AllErrors)
		if err != nil {
			return nil, fmt.Errorf("evidence_shape_mismatch: %w", err)
		}
		if file.Name.Name != p.Package {
			return nil, fmt.Errorf("package_scope_unknown: sibling package differs")
		}
		files = append(files, file)
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				functions[fn.Name.Name] = fn
			}
		}
		if filepath.Ext(m.name) == ".gsx" {
			views[sha256.Sum256(m.data)] = &aotCheckedSource{fset: fset, file: file, info: info, projection: projection, functions: functions}
		}
	}
	// Compiler-owned declarations are emitted once per package. Authored
	// declarations always keep precedence and their original scope.
	authored := map[string]bool{}
	synthesized := map[ast.Decl]string{}
	for _, view := range views {
		for _, decl := range view.file.Decls {
			for name, region := range view.projection.synthetic {
				offset := fset.Position(decl.Pos()).Offset
				if offset >= region.start && offset < region.end {
					synthesized[decl] = name
				}
			}
		}
	}
	declarationName := func(decl ast.Decl) string {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			return fn.Name.Name
		}
		if gen, ok := decl.(*ast.GenDecl); ok && len(gen.Specs) == 1 {
			if typ, ok := gen.Specs[0].(*ast.TypeSpec); ok {
				return typ.Name.Name
			}
		}
		return ""
	}
	for _, file := range files {
		for _, decl := range file.Decls {
			if synthesized[decl] == "" {
				authored[declarationName(decl)] = true
			}
		}
	}
	seen := map[string]bool{}
	clear(functions)
	for _, file := range files {
		var declarations []ast.Decl
		for _, decl := range file.Decls {
			if name := synthesized[decl]; name != "" {
				if seen[name] || authored[name] {
					continue
				}
				seen[name] = true
			}
			declarations = append(declarations, decl)
			if fn, ok := decl.(*ast.FuncDecl); ok {
				functions[fn.Name.Name] = fn
			}
		}
		file.Decls = declarations
	}
	var hardErrors []types.Error
	cfg := types.Config{Importer: &aotStubImporter{packages: map[string]*types.Package{}}, Sizes: &types.StdSizes{WordSize: 4, MaxAlign: 4}, DisableUnusedImportCheck: true, Error: func(err error) {
		if e, ok := err.(types.Error); ok && !e.Soft {
			hardErrors = append(hardErrors, e)
		}
	}}
	_, _ = cfg.Check(p.PackagePath, fset, files, info)
	trusted := map[types.Object]bool{}
	for declaration, name := range synthesized {
		if fn, ok := declaration.(*ast.FuncDecl); ok && functions[name] == fn {
			if object := info.Defs[fn.Name]; object != nil {
				trusted[object] = true
			}
		}
	}
	for _, view := range views {
		view.errors = hardErrors
		view.syntheticObjects = trusted
	}
	if len(aotCheckCache.order) == 64 {
		delete(aotCheckCache.entries, aotCheckCache.order[0])
		aotCheckCache.order = aotCheckCache.order[1:]
	}
	aotCheckCache.entries[key] = views
	aotCheckCache.order = append(aotCheckCache.order, key)
	return views[candidateKey], nil
}

type aotStubImporter struct{ packages map[string]*types.Package }

func (i *aotStubImporter) Import(importPath string) (*types.Package, error) {
	if p := i.packages[importPath]; p != nil {
		return p, nil
	}
	if importPath != signalImportPath {
		// Unknown imports have no invented API. Unused imports do not affect
		// a candidate; a referenced package is rejected using Info.Uses.
		p := types.NewPackage(importPath, path.Base(importPath))
		p.MarkComplete()
		i.packages[importPath] = p
		return p, nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "signal.stub", aotSignalStub, 0)
	if err != nil {
		return nil, err
	}
	p, err := (&types.Config{}).Check(importPath, fset, []*ast.File{file}, nil)
	if err == nil {
		i.packages[importPath] = p
	}
	return p, err
}

func (c *aotCheckedSource) candidateError(names map[string]bool) error {
	inCandidate := func(pos token.Pos) bool {
		for name := range names {
			if fn := c.functions[name]; fn != nil && pos >= fn.Pos() && pos <= fn.End() {
				return true
			}
		}
		return false
	}
	type failure struct {
		pos    token.Pos
		reason string
	}
	var failures []failure
	for ident, obj := range c.info.Uses {
		if !inCandidate(ident.Pos()) {
			continue
		}
		if imported, ok := obj.(*types.PkgName); ok {
			if imported.Imported().Path() != signalImportPath {
				failures = append(failures, failure{ident.Pos(), "import_outside_profile: " + imported.Imported().Path()})
			}
			continue
		}
		if _, builtin := obj.(*types.Builtin); builtin {
			continue
		}
		if aotInvalidType(obj.Type(), map[types.Type]bool{}) {
			failures = append(failures, failure{ident.Pos(), "type_error: referenced object " + obj.Name() + " has an invalid type"})
		}
	}
	for expr, tv := range c.info.Types {
		if inCandidate(expr.Pos()) && aotInvalidType(tv.Type, map[types.Type]bool{}) {
			failures = append(failures, failure{expr.Pos(), "type_error: expression has an invalid type"})
		}
	}
	// Binding errors at a declaration can invalidate an otherwise well-typed
	// use. Follow used objects to their declarations, including conflicting
	// package declarations of a referenced import alias.
	dependencies := map[token.Pos]bool{}
	usedNames := map[string]bool{}
	for ident, obj := range c.info.Uses {
		if inCandidate(ident.Pos()) && obj != nil {
			dependencies[obj.Pos()] = true
			usedNames[obj.Name()] = true
		}
	}
	for ident, obj := range c.info.Defs {
		if obj == nil || !usedNames[obj.Name()] {
			continue
		}
		_, imported := obj.(*types.PkgName)
		if imported || obj.Pkg() != nil && obj.Parent() == obj.Pkg().Scope() {
			dependencies[ident.Pos()] = true
		}
	}
	var relevant []types.Error
	for _, err := range c.errors {
		if strings.Contains(err.Msg, aotConditionalHelper) && inCandidate(err.Pos) {
			return fmt.Errorf("conditional_type_mismatch: %s", err.Msg)
		}
		if inCandidate(err.Pos) || dependencies[err.Pos] {
			relevant = append(relevant, err)
		}
	}
	slices.SortStableFunc(relevant, func(a, b types.Error) int {
		if a.Pos < b.Pos {
			return -1
		}
		if a.Pos > b.Pos {
			return 1
		}
		return 0
	})
	slices.SortStableFunc(failures, func(a, b failure) int {
		if a.pos < b.pos {
			return -1
		}
		if a.pos > b.pos {
			return 1
		}
		return strings.Compare(a.reason, b.reason)
	})
	for _, failure := range failures {
		if strings.HasPrefix(failure.reason, "import_outside_profile:") {
			return fmt.Errorf("%s", failure.reason)
		}
	}
	if len(relevant) > 0 {
		return fmt.Errorf("type_error: %s", relevant[0].Msg)
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", failures[0].reason)
	}
	return nil
}

func aotInvalidType(typ types.Type, seen map[types.Type]bool) bool {
	if typ == nil {
		return true
	}
	if seen[typ] {
		return false
	}
	seen[typ] = true
	if b, ok := typ.(*types.Basic); ok {
		return b.Kind() == types.Invalid
	}
	switch t := types.Unalias(typ).(type) {
	case *types.Basic:
		return t.Kind() == types.Invalid
	case *types.Named:
		for i := 0; i < t.TypeArgs().Len(); i++ {
			if aotInvalidType(t.TypeArgs().At(i), seen) {
				return true
			}
		}
		return aotInvalidType(t.Underlying(), seen)
	case *types.TypeParam:
		return aotInvalidType(t.Constraint(), seen)
	case *types.Union:
		for i := 0; i < t.Len(); i++ {
			if aotInvalidType(t.Term(i).Type(), seen) {
				return true
			}
		}
	case *types.Interface:
		t.Complete()
		for i := 0; i < t.NumMethods(); i++ {
			if aotInvalidType(t.Method(i).Type(), seen) {
				return true
			}
		}
		for i := 0; i < t.NumEmbeddeds(); i++ {
			if aotInvalidType(t.EmbeddedType(i), seen) {
				return true
			}
		}
	case *types.Pointer:
		return aotInvalidType(t.Elem(), seen)
	case *types.Array:
		return aotInvalidType(t.Elem(), seen)
	case *types.Slice:
		return aotInvalidType(t.Elem(), seen)
	case *types.Map:
		return aotInvalidType(t.Key(), seen) || aotInvalidType(t.Elem(), seen)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if aotInvalidType(t.Field(i).Type(), seen) {
				return true
			}
		}
	case *types.Signature:
		return aotInvalidType(t.Params(), seen) || aotInvalidType(t.Results(), seen)
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			if aotInvalidType(t.At(i).Type(), seen) {
				return true
			}
		}
	}
	return false
}

// Match GoSX filenames as their Go projection, including target suffixes and
// build constraints. OpenFile serves the actual authored bytes to go/build.
func aotMatchFile(dir, name string) (bool, error) {
	ctx := build.Default
	if filepath.Ext(name) == ".gsx" {
		original := filepath.Join(dir, name)
		name = strings.TrimSuffix(name, ".gsx") + ".go"
		ctx.OpenFile = func(_ string) (io.ReadCloser, error) { return os.Open(original) }
	}
	return ctx.MatchFile(dir, name)
}
