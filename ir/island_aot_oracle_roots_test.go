//go:build !tinygo && !js

package ir

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"m31labs.dev/gosx/island/aot"
	"m31labs.dev/gosx/island/program"
)

// Function bodies are absent from source-lowered scalar islands and are
// rejected by NewUnit's envelope. Exercise their consumption boundary directly
// alongside every other root, using independently type-checked Go values.
func TestIslandAOTGoTypesRootDifferential(t *testing.T) {
	roots := []struct {
		name string
		p    program.Program
	}{
		{"text", program.Program{Nodes: []program.Node{{Kind: program.NodeExpr}}}},
		{"attribute", program.Program{Nodes: []program.Node{{Attrs: []program.Attr{{Kind: program.AttrExpr}}}}}},
		{"predicate", program.Program{Nodes: []program.Node{{Kind: program.NodeConditional}}}},
		{"iteration", program.Program{Nodes: []program.Node{{Kind: program.NodeForEach}}}},
		{"signal", program.Program{Signals: []program.SignalDef{{Name: "value"}}}},
		{"computed", program.Program{Computeds: []program.ComputedDef{{Name: "value"}}}},
		{"handler", program.Program{Handlers: []program.Handler{{Body: []program.ExprID{0}}}}},
		{"function", program.Program{Funcs: []program.FuncDef{{Body: []program.ExprID{0}}}}},
	}
	values := []struct {
		source string
		kind   aot.ScalarKind
	}{
		{"0", aot.Int}, {"int32(1)", aot.Int32}, {"true", aot.Bool}, {`"x"`, aot.String},
		{"struct{ Value int32 }{1}", aot.SelectorPath}, {"[]int32{1}", aot.SelectorPath},
		{`map[string]int32{"x":1}`, aot.SelectorPath}, {"new(int32)", aot.SelectorPath},
	}
	for _, root := range roots {
		for _, value := range values {
			t.Run(root.name+"/"+value.source, func(t *testing.T) {
				fset := token.NewFileSet()
				file, err := parser.ParseFile(fset, "root.go", "package example\nfunc value() any { return "+value.source+" }", 0)
				if err != nil {
					t.Fatal(err)
				}
				info := &types.Info{Types: make(map[ast.Expr]types.TypeAndValue)}
				_, err = (&types.Config{Sizes: &types.StdSizes{WordSize: 4, MaxAlign: 4}}).Check("example", fset, []*ast.File{file}, info)
				if err != nil {
					t.Fatal(err)
				}
				expr := file.Decls[0].(*ast.FuncDecl).Body.List[0].(*ast.ReturnStmt).Results[0]
				_, scalar := info.Types[expr].Type.Underlying().(*types.Basic)
				kinds := []aot.ScalarKind{value.kind}
				constants := []constant.Value{info.Types[expr].Value}
				p := root.p
				p.Exprs = []program.Expr{{Op: program.OpLitInt, Value: "0"}}
				err = aotScalarRoots(&p, kinds, constants, map[string]aot.ScalarKind{"value": value.kind})
				if !scalar && err == nil {
					t.Fatal("aggregate Go root admitted")
				}
				// A handler expression statement is an effect, never a pure
				// scalar value. Predicate and iteration nodes have extra shape
				// rules at the fixed-DOM envelope boundary.
				if scalar && root.name != "handler" && err != nil {
					t.Fatal(err)
				}
				if root.name == "function" {
					p := root.p
					p.Nodes = []program.Node{{Kind: program.NodeElement, Tag: "div"}}
					p.Exprs = []program.Expr{{Op: program.OpLitInt, Value: "0"}}
					if _, err := aot.NewUnit("example.Counter", &p, aot.ScalarContract{Version: 1, Component: "example.Counter"}); err == nil {
						t.Fatal("function envelope admitted")
					}
				}
			})
		}
	}
	t.Log(fmt.Sprintf("root_corpus=%d", len(roots)*len(values)))
}
