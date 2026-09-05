//go:build !tinygo

// Package modelschema owns host-side Go model discovery, never pure lowering.
package modelschema

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"m31labs.dev/gosx/ir"
)

// Refs is syntax-derived input, not a partial semantic compilation.
type Refs struct {
	Package  string
	Roots    []string
	Declared map[string]bool
}

func SourceRefs(root *gotreesitter.Node, lang *gotreesitter.Language, source []byte) Refs {
	r := Refs{Declared: map[string]bool{}}
	deps := map[string][]string{}
	text := func(n *gotreesitter.Node) string {
		if n == nil {
			return ""
		}
		return string(source[n.StartByte():n.EndByte()])
	}
	var walk func(*gotreesitter.Node)
	walk = func(n *gotreesitter.Node) {
		switch n.Type(lang) {
		case "package_clause":
			for i := 0; i < int(n.NamedChildCount()); i++ {
				child := n.NamedChild(i)
				if child.Type(lang) == "package_identifier" {
					r.Package = text(child)
				}
			}
			return
		case "type_spec", "type_alias":
			name := text(n.ChildByFieldName("name", lang))
			r.Declared[name] = true
			f, e := parser.ParseFile(token.NewFileSet(), "", "package model\ntype "+text(n), 0)
			if e == nil {
				for _, decl := range f.Decls {
					g, ok := decl.(*ast.GenDecl)
					if !ok {
						continue
					}
					for _, sp := range g.Specs {
						ts, ok := sp.(*ast.TypeSpec)
						if !ok {
							continue
						}
						st, ok := ts.Type.(*ast.StructType)
						if !ok {
							continue
						}
						for _, field := range st.Fields.List {
							visible := false
							for _, name := range field.Names {
								visible = visible || ast.IsExported(name.Name)
							}
							if !visible {
								continue
							}
							switch v := field.Type.(type) {
							case *ast.Ident:
								deps[ts.Name.Name] = append(deps[ts.Name.Name], v.Name)
							case *ast.ArrayType:
								if v.Len == nil {
									if el, ok := v.Elt.(*ast.Ident); ok {
										deps[ts.Name.Name] = append(deps[ts.Name.Name], el.Name)
									}
								}
							}
						}
					}
				}
			}
			return
		case "gosx_component_declaration":
			params := n.ChildByFieldName("parameters", lang)
			if params != nil {
				for i := 0; i < int(params.NamedChildCount()); i++ {
					p := params.NamedChild(i)
					if p.Type(lang) == "gosx_component_parameter" {
						typ := text(p.ChildByFieldName("type", lang))
						if typ != "" {
							r.Roots = append(r.Roots, typ)
						}
					}
				}
			}
			return
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(root)
	seen := map[string]bool{}
	var expand func(string)
	expand = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		for _, dep := range deps[name] {
			r.Roots = append(r.Roots, dep)
			if r.Declared[dep] {
				expand(dep)
			}
		}
	}
	for _, name := range append([]string(nil), r.Roots...) {
		expand(name)
	}
	sort.Strings(r.Roots)
	return r
}

// Selection is caller-owned. GoTool selects with Env; otherwise only required
// unconditional models are admitted. Neither mode guesses running binary tags.
type Selection struct {
	GoTool bool
	Env    []string
}

// Overlay reserves coherent editor input; disk entrypoints do not yet use it.
// Extraction and Go-tool selection must both honor an overlay before admission.
type Overlay struct {
	Source  []byte
	Deleted bool
}

type Snapshot struct {
	schemas   *ir.SchemaSet
	digest    string
	directory string
	sources   map[string][]byte
}

// Verify detects net filesystem edits since capture; it is not an atomic or ABA
// transaction guarantee. Captured source bytes are never exposed to callers.
func (s Snapshot) Verify() error {
	if s.directory == "" {
		return nil
	}
	return VerifyDirectory(s.directory, ".go", s.sources)
}

func (s Snapshot) Schemas() *ir.SchemaSet { return s.schemas }
func (s Snapshot) Digest() string         { return s.digest }

type declaration struct {
	spec        *ast.TypeSpec
	file        string
	conditional bool
	pos         token.Position
}

// Resolve captures all candidate bytes before extracting only required models.
// The caller includes its exact GSX bytes in its program/cache identity.
func Resolve(ctx context.Context, dir string, refs Refs, selection Selection) (Snapshot, error) {
	snapshots, err := ResolvePackage(ctx, dir, []Refs{refs}, selection)
	if err != nil {
		return Snapshot{}, err
	}
	return snapshots[0], nil
}

// ResolvePackage captures and selects once for all package files, then projects
// a separate required closure for each file's source-owned declaration boundary.
func ResolvePackage(ctx context.Context, dir string, allRefs []Refs, selection Selection) ([]Snapshot, error) {
	if len(allRefs) == 0 {
		return nil, nil
	}
	refs := allRefs[0]

	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	fs := token.NewFileSet()
	index := map[string][]declaration{}
	data := map[string][]byte{}
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s/%s/%s\x00%t\x00", dir, refs.Package, runtime.GOOS, runtime.GOARCH, runtime.Compiler, selection.GoTool)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !goCandidate(name) {
			continue
		}
		path := filepath.Join(dir, name)
		src, e := os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		data[name] = src
		fmt.Fprintf(h, "%s\x00%x\x00", name, sha256.Sum256(src))
		f, e := parser.ParseFile(fs, path, src, parser.ParseComments)
		if e != nil {
			return nil, fmt.Errorf("model index %s: %w", path, e)
		}
		if f.Name.Name != refs.Package {
			continue
		}
		conditional := conditionalFile(name, f)
		for _, imp := range f.Imports {
			if imp.Path.Value == `"C"` {
				conditional = true
			}
		}
		for _, decl := range f.Decls {
			g, ok := decl.(*ast.GenDecl)
			if !ok || g.Tok != token.TYPE {
				continue
			}
			for _, sp := range g.Specs {
				t := sp.(*ast.TypeSpec)
				index[t.Name.Name] = append(index[t.Name.Name], declaration{t, path, conditional, fs.Position(t.Pos())})
			}
		}
	}
	selected := map[string]bool{}
	selectedCgo := map[string]bool{}
	needs := false
	for _, r := range allRefs {
		for _, root := range r.Roots {
			if len(index[root]) > 0 {
				needs = true
			}
		}
	}
	if selection.GoTool && needs {
		env := append([]string(nil), selection.Env...)
		if env == nil {
			env = os.Environ()
		}
		for _, kv := range []string{"GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local"} {
			key := strings.SplitN(kv, "=", 2)[0] + "="
			out := env[:0]
			for _, v := range env {
				if !strings.HasPrefix(v, key) {
					out = append(out, v)
				}
			}
			env = append(out, kv)
		}
		// Match os/exec's effective last-wins environment, then canonicalize.
		values := map[string]string{}
		for _, v := range env {
			key, value, ok := strings.Cut(v, "=")
			if ok {
				values[key] = value
			}
		}
		env = nil
		for key, value := range values {
			env = append(env, key+"="+value)
		}
		sort.Strings(env)
		for _, v := range env {
			fmt.Fprintf(h, "%s\x00", v)
		}
		child, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		// Like strictcheck.goCheck, executable lookup uses the process PATH.
		binary, e := exec.LookPath("go")
		if e != nil {
			return nil, e
		}
		identity := exec.CommandContext(child, binary, "env", "GOVERSION", "GOROOT", "GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOEXPERIMENT")
		identity.Dir = dir
		identity.Env = env
		identityBytes, e := identity.Output()
		if e != nil {
			return nil, fmt.Errorf("model tool identity: %w", e)
		}
		fmt.Fprintf(h, "tool:%s\x00%s\x00", binary, identityBytes)
		cmd := exec.CommandContext(child, "go", "list", "-e", "-json", ".")
		cmd.Dir = dir
		cmd.Env = env
		out, e := cmd.Output()
		if e != nil {
			return nil, fmt.Errorf("model build selection in %s: %w", dir, e)
		}
		var pkg struct {
			Name                              string
			GoFiles, CgoFiles, InvalidGoFiles []string
			Error                             *struct{ Err string }
		}
		if e = json.Unmarshal(out, &pkg); e != nil {
			return nil, fmt.Errorf("model build selection: %w", e)
		}
		if pkg.Name != refs.Package || len(pkg.InvalidGoFiles) > 0 {
			return nil, fmt.Errorf("model build selection in %s has incompatible package %q or invalid Go files", dir, pkg.Name)
		}
		if pkg.Error != nil {
			return nil, fmt.Errorf("model build selection in %s: %s", dir, pkg.Error.Err)
		}
		for _, name := range pkg.GoFiles {
			selected[name] = true
		}
		for _, name := range pkg.CgoFiles {
			selected[name] = true
			selectedCgo[name] = true
		}
		names := make([]string, 0, len(selected))
		for name := range selected {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(h, "selected:%s:%t\x00", name, selectedCgo[name])
		}
	}
	var snapshots []Snapshot
	baseDigest := fmt.Sprintf("%x", h.Sum(nil))
	for _, refs := range allRefs {
		if refs.Package != allRefs[0].Package {
			return nil, fmt.Errorf("mixed package schema snapshot")
		}
		var defs []ir.StructSchema
		seen := map[string]bool{}
		var collect func(string) error
		collect = func(name string) error {
			if seen[name] {
				return nil
			}
			seen[name] = true
			all := index[name]
			if refs.Declared[name] {
				return fmt.Errorf("model %s crosses into a GSX-owned declaration; cross-source model closure is not supported", name)
			}
			var candidates []declaration
			for _, d := range all {
				if !selection.GoTool && d.conditional {
					return fmt.Errorf("model %s at %s requires conditional build selection unsupported by file rendering", name, d.pos)
				}
				if !selection.GoTool || selected[filepath.Base(d.file)] {
					candidates = append(candidates, d)
				}
			}
			if len(candidates) == 0 {
				return fmt.Errorf("required model %s has no selected Go declaration", name)
			}
			if len(candidates) > 1 {
				return fmt.Errorf("duplicate model %s: %s and %s", name, candidates[0].pos, candidates[1].pos)
			}
			d := candidates[0]
			if selectedCgo[filepath.Base(d.file)] {
				return fmt.Errorf("model %s at %s depends on unsupported cgo", name, d.pos)
			}
			st, ok := d.spec.Type.(*ast.StructType)
			if !ok || d.spec.Assign.IsValid() || d.spec.TypeParams != nil {
				return fmt.Errorf("model %s at %s requires a non-generic named struct, not an alias/imported type", name, d.pos)
			}
			origin := func(p token.Pos) ir.SchemaOrigin {
				v := fs.Position(p)
				return ir.SchemaOrigin{File: v.Filename, Line: v.Line, Column: v.Column}
			}
			def := ir.StructSchema{Name: name, Origin: origin(d.spec.Pos())}
			for _, field := range st.Fields.List {
				if len(field.Names) == 0 {
					return fmt.Errorf("model %s at %s has unsupported embedded field", name, fs.Position(field.Pos()))
				}
				visible := false
				for _, n := range field.Names {
					visible = visible || ast.IsExported(n.Name)
				}
				if !visible {
					continue
				}
				typ := ""
				nested := ""
				switch t := field.Type.(type) {
				case *ast.Ident:
					typ = t.Name
					if len(index[typ]) > 0 || refs.Declared[typ] {
						nested = typ
					}
				case *ast.ArrayType:
					if t.Len == nil {
						if elem, ok := t.Elt.(*ast.Ident); ok {
							typ = "[]" + elem.Name
							nested = elem.Name
						}
					}
				}
				if typ == "" {
					return fmt.Errorf("model %s at %s has unsupported field type", name, fs.Position(field.Pos()))
				}
				for _, n := range field.Names {
					if ast.IsExported(n.Name) {
						def.Fields = append(def.Fields, ir.SchemaField{Name: n.Name, Type: typ, Origin: origin(n.Pos())})
					}
				}
				if nested != "" {
					if e := collect(nested); e != nil {
						return e
					}
				}
			}
			defs = append(defs, def)
			return nil
		}
		for _, root := range refs.Roots {
			if refs.Declared[root] {
				for _, d := range index[root] {
					if !selection.GoTool && d.conditional {
						return nil, fmt.Errorf("model %s at %s requires conditional build selection unsupported by file rendering", root, d.pos)
					}
					if !selection.GoTool || selected[filepath.Base(d.file)] {
						return nil, fmt.Errorf("duplicate model %s: type %s collides with type %s declared at %s:%d:%d (GSX-owned declaration and %s)", root, root, root, filepath.Base(d.file), d.pos.Line, d.pos.Column, d.pos)
					}
				}
				continue
			}
			if len(index[root]) == 0 {
				continue
			}
			if err := collect(root); err != nil {
				return nil, err
			}
		}
		schemas, err := ir.NewSchemaSet(refs.Package, defs)
		if err != nil {
			return nil, err
		}
		for _, root := range refs.Roots {
			fmt.Fprintf(h, "root:%s\x00", root)
		}
		key := sha256.Sum256([]byte(baseDigest + strings.Join(refs.Roots, "\x00")))
		snapshots = append(snapshots, Snapshot{schemas: schemas, digest: fmt.Sprintf("%x", key), directory: dir, sources: data})
	}
	if err := VerifyDirectory(dir, ".go", data); err != nil {
		return nil, err
	}
	return snapshots, nil
}

// VerifyDirectory rejects additions, deletions and content changes since capture.
// Callers compile only their captured bytes; this is bounded validation, not retry.
func VerifyDirectory(dir, extension string, data map[string][]byte) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	count := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}
		if extension == ".go" {
			if !goCandidate(name) {
				continue
			}
		} else if !strings.EqualFold(filepath.Ext(name), extension) {
			continue
		}
		count++
		previous, ok := data[name]
		now, err := os.ReadFile(filepath.Join(dir, name))
		if !ok || err != nil || string(now) != string(previous) {
			return fmt.Errorf("schema source snapshot changed during capture: %s", filepath.Join(dir, name))
		}
	}
	if count != len(data) {
		return fmt.Errorf("schema source snapshot changed during capture in %s", dir)
	}
	return nil
}

func goCandidate(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") && !strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "_")
}

func conditionalFile(name string, file *ast.File) bool {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if comment.Pos() > file.Package {
				continue
			}
			if constraint.IsGoBuild(comment.Text) || constraint.IsPlusBuild(comment.Text) {
				return true // malformed constraints are not unconditional either
			}
		}
	}
	base := strings.TrimSuffix(name, ".go")
	parts := strings.Split(base, "_")
	if len(parts) > 1 {
		p := parts[len(parts)-1]
		if strings.Contains(" aix android darwin dragonfly freebsd hurd illumos ios js linux nacl netbsd openbsd plan9 solaris wasip1 windows zos 386 amd64 amd64p32 arm armbe arm64 arm64be loong64 mips mipsle mips64 mips64le mips64p32 mips64p32le ppc ppc64 ppc64le riscv riscv64 s390 s390x sparc sparc64 wasm ", " "+p+" ") {
			return true
		}
	}
	return false
}
