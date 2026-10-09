//go:build !tinygo && !js

package ir

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/gotreesitter/grammars"
	"m31labs.dev/gosx/internal/gsxparse"
)

func checkerInternalProgram(t *testing.T, source string) *Program {
	t.Helper()
	lang := grammars.GoLanguage()
	tree, err := gsxparse.Parse(lang, []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Release()
	p, err := Lower(tree.RootNode(), []byte(source), lang)
	if err != nil {
		t.Fatal(err)
	}
	p.Dir, p.PackagePath = t.TempDir(), "example/components"
	return p
}

func TestIslandAOTCheckingSpansAndCache(t *testing.T) {
	source := "package example\n//gosx:island\nfunc Counter() Node {\n // preserved comment\n _ = (1 /* keep */ + 2); return Node{}\n}\ntype Node struct{}\n"
	p := checkerInternalProgram(t, source)
	a, err := aotCheckSource(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, span := range a.projection.copies {
		if !bytes.Equal([]byte(source)[span.sourceStart:span.sourceEnd], a.projection.bytes[span.checkStart:span.checkStart+span.sourceEnd-span.sourceStart]) {
			t.Fatal("checking file changed copied source")
		}
	}
	if !bytes.Contains(a.projection.bytes, []byte("1 /* keep */ + 2")) || !bytes.Contains(a.projection.bytes, []byte("// preserved comment")) {
		t.Fatal("checking file lost authored bytes")
	}
	b, err := aotCheckSource(p)
	if err != nil || a != b {
		t.Fatal("identical source was checked twice", err)
	}
	if err := os.WriteFile(filepath.Join(p.Dir, "types.go"), []byte("package example\ntype Added int64\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := aotCheckSource(p)
	if err != nil || c == a {
		t.Fatal("sibling change reused evidence", err)
	}
}

func TestIslandAOTReferencedPackageBindingErrors(t *testing.T) {
	p := checkerInternalProgram(t, "package example\nimport s \"m31labs.dev/gosx/signal\"\nvar s=0\nfunc Counter() Node { count:=s.New(0);_ = count;return Node{} }\ntype Node struct{}\n")
	c, err := aotCheckSource(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.candidateError(map[string]bool{"Counter": true}); err == nil {
		t.Fatalf("conflicting imported object admitted; checker errors: %+v", c.errors)
	}
}

func TestIslandAOTClosedDiscriminants(t *testing.T) {
	for _, mutation := range []func(*Program){func(p *Program) { p.Nodes[p.Components[0].Root].Kind = 255 }, func(p *Program) {
		p.Nodes[p.Components[0].Root].Attrs = append(p.Nodes[p.Components[0].Root].Attrs, Attr{Kind: 255})
	}} {
		p := &Program{PackagePath: "example/components", Nodes: []Node{{Kind: NodeElement, Tag: "div"}}, Components: []Component{{Name: "Counter", IsIsland: true}}}
		mutation(p)
		if _, err := LowerIsland(p, 0); err == nil {
			t.Fatal("VM lowerer ignored unknown discriminant")
		}
		if _, err := LowerIslandAOT(p, 0); err == nil {
			t.Fatal("admission ignored unknown discriminant")
		}
	}
	for _, filename := range []string{"island.go", "island_aot_walk.go", "island_aot_values.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			s, ok := n.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			tag, ok := s.Tag.(*ast.SelectorExpr)
			if !ok || tag.Sel.Name != "Kind" {
				return true
			}
			for _, branch := range s.Body.List {
				if branch.(*ast.CaseClause).List == nil {
					return true
				}
			}
			t.Errorf("%s has an open discriminant switch", filename)
			return true
		})
	}
}

func TestIslandAOTStubSignatures(t *testing.T) {
	if want := fmt.Sprintf("signal-api-v1:%x", sha256.Sum256([]byte(aotSignalStub))); aotStubRevision != want {
		t.Fatalf("stub revision does not identify embedded bytes: %s", want)
	}
	command := exec.Command("go", "list", "-export", "-deps", "-json", "./signal")
	command.Dir = ".."
	command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	exports := map[string]string{}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	for {
		var pkg struct{ ImportPath, Export string }
		err := decoder.Decode(&pkg)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		exports[pkg.ImportPath] = pkg.Export
	}
	realImporter := importer.ForCompiler(token.NewFileSet(), "gc", func(name string) (io.ReadCloser, error) { return os.Open(exports[name]) })
	real, err := realImporter.Import(signalImportPath)
	if err != nil {
		t.Fatal(err)
	}
	stub, err := (&aotStubImporter{packages: map[string]*types.Package{}}).Import(signalImportPath)
	if err != nil {
		t.Fatal(err)
	}
	qualify := func(p *types.Package) string { return p.Path() }
	signatures := func(p *types.Package) map[string]string {
		result := map[string]string{}
		for _, name := range p.Scope().Names() {
			obj := p.Scope().Lookup(name)
			if !obj.Exported() {
				continue
			}
			result[name] = types.TypeString(obj.Type(), qualify)
			if named, ok := obj.Type().(*types.Named); ok {
				for i := 0; i < named.TypeParams().Len(); i++ {
					result[fmt.Sprintf("%s/typeparameter/%d", name, i)] = types.TypeString(named.TypeParams().At(i).Constraint(), qualify)
				}
				if _, opaque := named.Underlying().(*types.Struct); !opaque {
					result[name+"/underlying"] = types.TypeString(named.Underlying(), qualify)
				}
				methods := types.NewMethodSet(types.NewPointer(named))
				for i := 0; i < methods.Len(); i++ {
					method := methods.At(i)
					if method.Obj().Exported() {
						result[name+"."+method.Obj().Name()] = types.TypeString(method.Type(), qualify)
					}
				}
			}
		}
		return result
	}
	want, got := signatures(real), signatures(stub)
	for name, sig := range want {
		if got[name] != sig {
			t.Errorf("signature drift %s: real=%s stub=%s", name, sig, got[name])
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("stub exports absent real API %s", name)
		}
	}
}

func TestIslandAOTTinyGoDependencyBoundary(t *testing.T) {
	command := exec.Command("go", "list", "-deps", "-tags=tinygo", "./client/wasm")
	command.Dir = ".."
	command.Env = append(os.Environ(), "GOWORK=off", "GOOS=js", "GOARCH=wasm", "GOPROXY=off")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("TinyGo dependency graph: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		switch dependency {
		case "go/types", "go/importer", "go/build":
			t.Errorf("host checker in TinyGo client graph: %s", dependency)
		}
	}
}

func TestIslandAOTGoWASMDependencyBoundary(t *testing.T) {
	command := exec.Command("go", "list", "-deps", "./client/wasm")
	command.Dir = ".."
	command.Env = append(os.Environ(), "GOWORK=off", "GOOS=js", "GOARCH=wasm", "GOPROXY=off")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Go WASM graph: %v %s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		switch dependency {
		case "go/types", "go/importer", "go/build":
			t.Errorf("host checker in Go WASM client graph: %s", dependency)
		}
	}
}

// AOTCheckingHardErrorsForTest exposes scaffold diagnostics to the external corpus.
func AOTCheckingHardErrorsForTest(p *Program) []string {
	c, err := aotCheckSource(p)
	if err != nil {
		return []string{err.Error()}
	}
	var errors []string
	for _, e := range c.errors {
		errors = append(errors, e.Msg)
	}
	return errors
}

func AOTPackageEvidenceSharedForTest(a, b *Program) bool {
	x, err := aotCheckSource(a)
	if err != nil {
		return false
	}
	y, err := aotCheckSource(b)
	return err == nil && x.info == y.info
}

func AOTResetCheckCacheForTest() {
	aotCheckCache.Lock()
	defer aotCheckCache.Unlock()
	clear(aotCheckCache.entries)
	aotCheckCache.order = nil
}

// Names from the actual event contract and embedded stub keep the collision
// corpus complete when either surface grows. "type" is a Go keyword and is
// never inserted as an implicit local.
func AOTScaffoldNamesForTest() []string {
	names := []string{"signal", "Node", aotConditionalHelper, "props", "count", "_", "children", "slotTitle", "T", "c", "a", "b"}
	for _, name := range islandEventFields {
		if !token.Lookup(name).IsKeyword() {
			names = append(names, name)
		}
	}
	file, err := parser.ParseFile(token.NewFileSet(), "signal.stub", aotSignalStub, 0)
	if err != nil {
		panic(err)
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			names = append(names, "stub/"+d.Name.Name)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if typ, ok := spec.(*ast.TypeSpec); ok {
					names = append(names, "stub/"+typ.Name.Name)
				}
			}
		}
	}
	return names
}
