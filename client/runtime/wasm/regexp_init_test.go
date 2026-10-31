//go:build !js || !wasm

package wasm

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every published browser runtime is a TinyGo build of m31labs.dev/gosx/client/wasm
// with these tags (cmd/gosx runtimeVariantBuildTags and tinyGoWASMTags).
var browserRuntimeTags = map[string]string{
	"core":   "tinygo gosx_tiny_runtime gosx_runtime_core",
	"engine": "tinygo gosx_tiny_runtime gosx_runtime_engine",
	"collab": "tinygo gosx_runtime_collab",
	"full":   "tinygo gosx_runtime_full",
}

// A package that compiles a regular expression when the program starts keeps
// regexp and regexp/syntax in the browser runtime, even when nothing calls the
// pattern: the call runs at start-up, so the linker cannot drop it. Package ir
// and the server's text estimator did this, and regexp, regexp/syntax and the
// Unicode tables they reach were part of every runtime, although no code in
// the browser matches those patterns. A pattern in the runtime's packages is
// built with lazyre.New, which compiles on first use.
//
// This test reads the source of every package of this module that a runtime
// links, and fails on a regexp compiled in a package-level variable or in an
// init function. It cannot see through a helper that compiles at start-up;
// the WASM size budgets (scripts/check-wasm-size.sh) are the check for that.
func TestBrowserRuntimeCompilesNoRegexpAtStartup(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for variant, tags := range browserRuntimeTags {
		cmd := exec.Command("go", "list", "-deps", "-tags="+tags,
			"-f", "{{if not .Standard}}{{.ImportPath}}\t{{.Dir}}\t{{join .GoFiles \",\"}}{{end}}",
			"m31labs.dev/gosx/client/wasm")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off", "GOPROXY=off", "GOFLAGS=-mod=mod -buildvcs=false")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go list for the %s runtime: %v\n%s", variant, err, out)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) != 3 || !strings.HasPrefix(fields[0], "m31labs.dev/gosx") {
				continue
			}
			for _, name := range strings.Split(fields[2], ",") {
				path := filepath.Join(fields[1], name)
				if name == "" || seen[path] {
					continue
				}
				seen[path] = true
				for _, finding := range regexpCompiledAtStartup(t, path) {
					t.Errorf("%s (linked by the %s runtime): %s", path, variant, finding)
				}
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("the dependency list named no file of this module")
	}
}

// regexpCompiledAtStartup reports each call of a regexp compile function in a
// package-level variable's initializer or in an init function of the file.
func regexpCompiledAtStartup(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	regexpName := ""
	for _, spec := range file.Imports {
		if spec.Path.Value == `"regexp"` {
			regexpName = "regexp"
			if spec.Name != nil {
				regexpName = spec.Name.Name
			}
		}
	}
	if regexpName == "" {
		return nil
	}
	var findings []string
	look := func(node ast.Node, where string) {
		ast.Inspect(node, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == regexpName && strings.Contains(sel.Sel.Name, "Compile") {
				findings = append(findings, where+" calls regexp."+sel.Sel.Name+" at "+fset.Position(call.Pos()).String()+"; use lazyre.New")
			}
			return true
		})
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, spec := range d.Specs {
				for _, value := range spec.(*ast.ValueSpec).Values {
					look(value, "a package-level variable")
				}
			}
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == "init" && d.Body != nil {
				look(d.Body, "init")
			}
		}
	}
	return findings
}
