//go:build !tinygo && !js

package ir_test

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/aot"
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/transpile"
)

func TestIslandAOTGoTypesDifferential(t *testing.T) {
	corpus := scalarOracleCorpus()
	sourceDirectory := t.TempDir()
	if len(corpus) < 500 {
		t.Fatal("scalar corpus must contain at least 500 cases")
	}
	imports := &scalarOracleImporter{packages: make(map[string]*types.Package)}
	admitted, disagreements, sourceRejected := 0, 0, 0
	for _, tc := range corpus {
		if !t.Run(tc.name, func(t *testing.T) {
			gsx, goSource := tc.sources()
			p, err := parse(t, []byte(gsx))
			var u aot.Unit
			admission := err
			if err != nil {
				var diagnostic *ir.DiagnosticsError
				if !errors.As(err, &diagnostic) {
					t.Fatalf("invalid corpus syntax: %v\n%s", err, gsx)
				}
				sourceRejected++
			} else {
				p.PackagePath = "example/components"
				p.Dir = sourceDirectory
				u, admission = ir.LowerIslandAOT(p, 0)
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "island.go", goSource, 0)
			if err != nil {
				t.Fatalf("invalid oracle projection: %v\n%s", err, goSource)
			}
			info := &types.Info{Types: make(map[ast.Expr]types.TypeAndValue)}
			cfg := types.Config{Importer: imports, Sizes: &types.StdSizes{WordSize: 4, MaxAlign: 4}}
			_, typeErr := cfg.Check("example/components", fset, []*ast.File{file}, info)
			if typeErr == nil && p != nil && (tc.root != "inline_handler" || tc.expr == tc.goExpr) {
				if hard := ir.AOTCheckingHardErrorsForTest(p); len(hard) > 0 {
					t.Errorf("compiler-valid fixture has scaffold hard errors: %v", hard)
					return
				}
			}
			if admission != nil {
				return
			} // Conservative VM-only exclusions are permitted.
			admitted++
			if typeErr != nil {
				t.Errorf("AOT admitted Go type error: %v", typeErr)
				return
			}
			// Choose has no VM opcode and cannot pass a strict island's syntax
			// gate. Its Go lowering is checked in the independent projection;
			// additionally check real renderer output for native-Go recipes and
			// legacy components, which can render the Go-only helper.
			if tc.expr == tc.goExpr || !strings.Contains(gsx, "component Counter(") {
				compatible := strings.ReplaceAll(gsx, tc.expr, tc.goExpr)
				// Choose is a test-only Go lowering of ternary syntax. Removing the
				// client marker lets the renderer emit it without first asking the
				// restricted VM parser to recognize this oracle helper.
				if !strings.Contains(compatible, "component Counter(") {
					compatible = strings.ReplaceAll(compatible, "//gosx:island\n", "")
				}
				extra := "import gosx \"m31labs.dev/gosx\"\n"
				if tc.expr != tc.goExpr && tc.root != "inline_handler" {
					extra += "import oracle \"example.test/scalaroracle\"\n"
				}
				compatible = strings.Replace(compatible, "package example\n", "package example\n"+extra, 1)
				compatible = strings.Replace(compatible, "type Detail struct", "type Node = gosx.Node\ntype Detail struct", 1)
				lowered, err := transpile.Transpile([]byte(compatible), transpile.Options{})
				if err != nil {
					t.Errorf("AOT admitted source that cannot lower to Go: %v", err)
					return
				}
				loweredFile, err := parser.ParseFile(fset, "lowered.go", lowered, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := cfg.Check("example/components", fset, []*ast.File{loweredFile}, nil); err != nil {
					t.Errorf("AOT admitted lowered Go type error: %v", err)
					return
				}
			}
			root := oracleGoRoot(file, tc.root)
			if root == nil {
				t.Fatal("missing oracle value-consuming root")
			}
			got := info.Types[root]
			wantKind := oracleBasicKind(got.Type)
			if wantKind == "" {
				t.Errorf("AOT admitted non-scalar Go type %v", got.Type)
				return
			}
			if !oracleConstantFits(got, wantKind) {
				t.Errorf("AOT admitted unrepresentable constant %v", got.Value)
				return
			}
			id := oracleProgramRoot(u.Program, tc.root)
			if id < 0 {
				t.Fatal("missing AOT value-consuming root")
			}
			if kind := u.Contract.Expressions[id].Kind; kind != wantKind {
				t.Errorf("AOT kind %s differs from Go kind %s (%v)", kind, wantKind, got.Type)
			}
			t.Run("VM-to-VM", func(t *testing.T) { oracleVMEquality(t, p, u, id, got, tc.typ) })
		}) {
			disagreements++
		}
	}
	t.Logf("corpus=%d admitted=%d source_rejected=%d disagreements=%d", len(corpus), admitted, sourceRejected, disagreements)
	if admitted < 100 {
		t.Errorf("too few positive cases: %d", admitted)
	}
}

func oracleGoRoot(file *ast.File, root string) ast.Expr {
	root = oracleValueRoot(root)
	var result ast.Expr
	ast.Inspect(file, func(n ast.Node) bool {
		if result != nil {
			return false
		}
		if root == "computed" {
			if fn, ok := n.(*ast.FuncLit); ok {
				if r, ok := fn.Body.List[0].(*ast.ReturnStmt); ok {
					result = r.Results[0]
					return false
				}
			}
		}
		if root == "statement" {
			if fn, ok := n.(*ast.FuncLit); ok {
				if e, ok := fn.Body.List[0].(*ast.ExprStmt); ok {
					result = e.X
					return false
				}
			}
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fun := call.Fun
		if indexed, ok := fun.(*ast.IndexExpr); ok {
			fun = indexed.X
		}
		selector, ok := fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := selector.Sel.Name
		matches := root == "text" && name == "Emit" || root == "attribute" && name == "Attr" ||
			root == "prop" && name == "Typed" ||
			(root == "signal" || root == "shared") && (name == "New" || name == "NewShared") ||
			root == "argument" && name == "Set" || root == "predicate" && name == "When" || root == "iteration" && name == "Each"
		if matches {
			result = call.Args[len(call.Args)-1]
			return false
		}
		return true
	})
	return result
}

func oracleBasicKind(typ types.Type) aot.ScalarKind {
	if typ == nil {
		return ""
	}
	basic, ok := types.Default(typ).Underlying().(*types.Basic)
	if !ok {
		return ""
	}
	switch basic.Kind() {
	case types.Int:
		return aot.Int
	case types.Int32:
		return aot.Int32
	case types.Bool:
		return aot.Bool
	case types.String:
		return aot.String
	}
	return ""
}

func oracleConstantFits(value types.TypeAndValue, kind aot.ScalarKind) bool {
	if value.Value == nil {
		return true
	}
	if kind == aot.Int || kind == aot.Int32 {
		n, exact := constant.Int64Val(value.Value)
		return exact && n >= -2147483648 && n <= 2147483647
	}
	return kind == aot.Bool && value.Value.Kind() == constant.Bool || kind == aot.String && value.Value.Kind() == constant.String
}

func oracleProgramRoot(p *program.Program, root string) int {
	attribute := "title"
	if root == "fallback" {
		attribute = "fallback"
	} else if root == "loop_key" {
		attribute = "key"
	}
	root = oracleValueRoot(root)
	if root == "prop" {
		root = "text"
	}
	switch root {
	case "signal", "shared":
		return int(p.Signals[0].Init)
	case "computed":
		return int(p.Computeds[0].Expr)
	case "argument":
		return int(p.Exprs[p.Handlers[0].Body[0]].Operands[0])
	case "statement":
		return int(p.Handlers[0].Body[0])
	}
	for _, n := range p.Nodes {
		if root == "text" && n.Kind == program.NodeExpr || root == "predicate" && n.Kind == program.NodeConditional || root == "iteration" && n.Kind == program.NodeForEach {
			return int(n.Expr)
		}
		for _, attr := range n.Attrs {
			if root == "attribute" && attr.Kind == program.AttrExpr && attr.Name == attribute {
				return int(attr.Expr)
			}
		}
	}
	return -1
}

func oracleValueRoot(root string) string {
	switch root {
	case "inline_handler":
		return "argument"
	case "slot_text", "nested_slot", "default_child", "callee_text", "conditional_body", "loop_body":
		return "text"
	case "slot_attribute", "callee_attribute", "fallback", "loop_key", "spread", "event_attribute":
		return "attribute"
	case "slot_prop", "boolean_prop":
		return "prop"
	}
	return root
}

func oracleVMEquality(t *testing.T, source *ir.Program, unit aot.Unit, root int, typed types.TypeAndValue, typ string) {
	t.Helper()
	fallback, err := ir.LowerIsland(source, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := program.EncodeBinary(fallback)
	if err != nil || !bytes.Equal(raw, unit.ProgramBytes) {
		t.Fatalf("fallback bytes changed: %v", err)
	}
	value := vm.IntVal(7)
	if typ == "bool" {
		value = vm.BoolVal(true)
	}
	if typ == "string" {
		value = vm.StringVal("label")
	}
	props := map[string]vm.Value{
		"Value": value, "Other": vm.IntVal(3), "I32": vm.IntVal(5), "I64": vm.IntVal(9), "Flag": vm.BoolVal(true), "Label": vm.StringVal("label"),
		"Detail": vm.ObjectVal(map[string]vm.Value{"Label": vm.StringVal("nested"), "Number": vm.IntVal(11)}),
	}
	propsObject := make(map[string]vm.Value, len(props))
	for name, value := range props {
		propsObject[name] = value
	}
	props["props"] = vm.ObjectVal(propsObject)
	machines := []*vm.VM{vm.NewVM(fallback, props), vm.NewVM(unit.Program, props)}
	for i, machine := range machines {
		p := fallback
		if i == 1 {
			p = unit.Program
		}
		vm.InitSignals(machine, p)
		t.Cleanup(func() { machine.SwapProgram(&program.Program{}) })
	}
	before, after := machines[0].Eval(program.ExprID(root)), machines[1].Eval(program.ExprID(root))
	if before.Type != after.Type || !before.Eq(after).Truth() {
		t.Errorf("AOT snapshot output %v differs from VM output %v", after, before)
	}
	if typed.Value != nil {
		want := typed.Value.ExactString()
		if typed.Value.Kind() == constant.String {
			want = constant.StringVal(typed.Value)
		}
		if after.String() != want {
			t.Errorf("VM output %s differs from Go constant %s", after.String(), want)
		}
	}
	if !reflect.DeepEqual(machines[0].EvalTree(), machines[1].EvalTree()) {
		t.Error("AOT snapshot and VM initial trees differ")
	}
	for _, handler := range fallback.Handlers {
		for _, id := range handler.Body {
			for _, machine := range machines {
				machine.Eval(id)
			}
		}
		if !reflect.DeepEqual(machines[0].EvalTree(), machines[1].EvalTree()) {
			t.Error("AOT snapshot and VM handler trees differ")
		}
	}
}

// These in-memory packages model only imported APIs. Source type checking,
// generic inference, constant conversions and scope resolution are go/types'
// implementation, not an AOT-derived oracle or an external build tool.
type scalarOracleImporter struct{ packages map[string]*types.Package }

func (i *scalarOracleImporter) Import(path string) (*types.Package, error) {
	if p := i.packages[path]; p != nil {
		return p, nil
	}
	source := ""
	switch path {
	case "example.test/scalaroracle":
		source = `package oracle
func Emit(v any) any { return v }
func Attr(v any) any { return v }
func Typed[T any](v T) any { return v }
func Choose[T any](condition bool, yes, no T) T { if condition { return yes }; return no }
func When(condition bool) any { return condition }
func Each[T any](values []T) any { return values }
`
	case "m31labs.dev/gosx/signal":
		source = `package signal
type Signal[T any] struct { value T }
func New[T any](v T) *Signal[T] { return &Signal[T]{value:v} }
func NewShared[T any](name string, v T) *Signal[T] { return New(v) }
func (s *Signal[T]) Get() T { return s.value }
func (s *Signal[T]) Set(v T) { s.value = v }
type Computed[T any] struct { value T }
func Derive[T any](f func() T) *Computed[T] { return &Computed[T]{value:f()} }
func (s *Computed[T]) Get() T { return s.value }
`
	case "m31labs.dev/gosx":
		source = `package gosx
type Node struct{}
type Attribute struct{}
func Expr(v any) Node { return Node{} }
func Attr(name string, v any) Attribute { return Attribute{} }
func Attrs(v ...Attribute) []Attribute { return v }
func El(tag string, args ...any) Node { return Node{} }
func Text(v string) Node { return Node{} }
func Fragment(v ...Node) Node { return Node{} }
`
	default:
		return nil, fmt.Errorf("unknown oracle import %s", path)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "api.go", source, 0)
	if err != nil {
		return nil, err
	}
	p, err := (&types.Config{Importer: i}).Check(path, fset, []*ast.File{file}, nil)
	if err == nil {
		i.packages[path] = p
	}
	return p, err
}

func TestIslandAOTTypeCheckerHostOnly(t *testing.T) {
	files, err := filepath.Glob("island_aot*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, data, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if path == "go/types" || path == "go/importer" || path == "go/build" {
				line, _, _ := strings.Cut(string(data), "\n")
				boundary, err := constraint.Parse(line)
				if err != nil || boundary.Eval(func(tag string) bool { return tag == "tinygo" }) || boundary.Eval(func(tag string) bool { return tag == "js" }) {
					t.Errorf("%s imports host checker %s without client build boundaries", name, path)
				}
			}
		}
	}
}
