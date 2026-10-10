//go:build !tinygo && !js

package ir_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/program"
)

type scaffoldOracleCase struct {
	name, declaration, params, body, goBody, reason string
}

func scaffoldOracleCorpus() []scaffoldOracleCase {
	var cases []scaffoldOracleCase
	for _, group := range []struct{ names, typ, zero, literal string }{
		{"value key code targetID currentTargetID pointerType eventData", "string", `""`, `"authored"`},
		{"checked ctrlKey metaKey altKey shiftKey repeat editable isPrimary", "bool", "false", "true"},
		{"selectedIndex pointerID button buttons deltaMode", "int", "0", "42"},
		{"timeStamp clientX clientY pressure width height offsetX offsetY elementWidth elementHeight deltaX deltaY", "float64", "0.0", "1.5"},
		{"data", "interface{}", "nil", "nil"},
	} {
		for _, name := range strings.Fields(group.names) {
			body := fmt.Sprintf("state:=signal.New[%s](%s);change:=func(){state.Set(%s)};", group.typ, group.zero, name)
			cases = append(cases, scaffoldOracleCase{
				name: name, declaration: "var " + name + " " + group.typ + " = " + group.literal,
				body:   body + "return <button onClick={change}>ready</button>",
				goBody: body + "_ = change; return Node{}", reason: "implicit_identifier_shadowed: " + name,
			})
		}
	}
	cases = append(cases,
		scaffoldOracleCase{name: "incompatible value", declaration: "const value = 42", body: `state:=signal.New("");change:=func(){state.Set(value)};return <input onInput={change}/>`, goBody: `state:=signal.New("");change:=func(){state.Set(value)};_ = change;return Node{}`, reason: "implicit_identifier_shadowed: value"},
		scaffoldOracleCase{name: "signal", declaration: "var signal struct{New func(int) int}", body: "count:=signal.New(0);return <div>{count.Get()}</div>", goBody: "count:=signal.New(0);_ = count.Get();return Node{}", reason: "type_error"},
		scaffoldOracleCase{name: "Node", declaration: "type Node = struct{}", body: "return <div>{1}</div>", goBody: "_ = 1;return Node{}"},
		scaffoldOracleCase{name: "__gosx_aot_choose", declaration: "func __gosx_aot_choose(c bool,a,b int) int{return b}", body: "return <div>{true ? 1 : 2}</div>", goBody: "_ = __gosx_aot_choose(true,1,2);return Node{}", reason: "evidence_shape_mismatch"},
		scaffoldOracleCase{name: "props", declaration: "const props = 42", params: "props Props", body: "return <div>{props.Value}</div>", goBody: "_ = props.Value;return Node{}"},
		scaffoldOracleCase{name: "count", declaration: "const count = 42", body: "count:=signal.New(0);return <div>{count.Get()}</div>", goBody: "count:=signal.New(0);_ = count.Get();return Node{}"},
		scaffoldOracleCase{name: "_", declaration: "const _ = 42", body: "count:=signal.New(0);return <div>{count.Get()}</div>", goBody: "count:=signal.New(0);_ = count.Get();return Node{}"},
		scaffoldOracleCase{name: "children", declaration: "const children = 42", body: "return <div><Layout>ready</Layout></div>", goBody: "return Node{}", reason: "implicit_identifier_shadowed: children"},
		scaffoldOracleCase{name: "slotTitle", declaration: "const slotTitle = 42", body: `return <div><Layout><span slot="Title">ready</span></Layout></div>`, goBody: "return Node{}", reason: "implicit_identifier_shadowed: slotTitle"},
	)
	for _, name := range []string{"T", "c", "a", "b"} {
		cases = append(cases, scaffoldOracleCase{name: name, declaration: "const " + name + " = 42", body: "return <div>{true ? 1 : 2}</div>", goBody: "_ = 1;return Node{}"})
	}
	// Stub API declarations belong to the imported package. Application
	// declarations cannot change a qualified signal.New/Get/Set binding.
	for _, name := range []string{"Subscriber", "Subscribable", "Signal", "Computed", "Effect", "New", "NewShared", "Shared", "NewWithEqual", "Derive", "Get", "Set", "Revision", "Update", "Subscribe", "subscribe", "Stop", "Watch", "Dispose", "Batch", "ResolveAlias", "IsAlias", "AliasesOf", "LegacySceneEventNames", "SurfaceEventNames", "IsSurfaceEventName", "IsSceneEventName"} {
		cases = append(cases, scaffoldOracleCase{name: "stub/" + name, declaration: "const " + name + " = 42", body: "count:=signal.New(0);change:=func(){count.Set(count.Get()+1)};return <button onClick={change}>{count}</button>", goBody: "count:=signal.New(0);change:=func(){count.Set(count.Get()+1)};_ = change;_ = count.Get();return Node{}"})
	}
	return cases
}

func TestIslandAOTSynthesizedIdentifierOracle(t *testing.T) {
	cases := scaffoldOracleCorpus()
	covered := map[string]bool{}
	for _, tc := range cases {
		covered[tc.name] = true
	}
	for _, name := range ir.AOTScaffoldNamesForTest() {
		if !covered[name] {
			t.Errorf("missing sibling-collision oracle for synthesized identifier %s", name)
		}
	}
	admitted, disagreements := 0, 0
	for _, tc := range cases {
		for _, extension := range []string{"go", "gsx"} {
			if !t.Run(tc.name+"/"+extension, func(t *testing.T) {
				candidate := "package example\ntype Props struct{Value int}\n//gosx:island\nfunc Counter(" + tc.params + ") Node {\n" + tc.body + "\n}\n"
				switch tc.name {
				case "children":
					candidate = strings.Replace(candidate, "func Counter() Node", "component Counter()", 1)
					candidate += "component Layout(){return <article>{children}</article>}\n"
				case "slotTitle":
					candidate = strings.Replace(candidate, "func Counter() Node", "component Counter()", 1)
					candidate += "component Layout(){return <article>{slotTitle}</article>}\n"
				}
				p := pairingSource(t, candidate)
				sibling := "package example\n" + tc.declaration + "\n"
				writeScaffoldSource(t, p.Dir, "scope."+extension, sibling)
				// This Go file preserves the authored sibling binding. It never
				// declares an implicit event local to make the fixture compile.
				imports := "import signal \"m31labs.dev/gosx/signal\"\n"
				if tc.name == "signal" {
					imports = ""
				}
				goSource := "package example\n" + imports + "type Props struct{Value int}\n"
				if tc.name != "Node" {
					goSource += "type Node struct{}\n"
				}
				goSource += "func Counter(" + tc.params + ") Node {\n" + tc.goBody + "\n}\n"
				fset := token.NewFileSet()
				var files []*ast.File
				for _, source := range []string{goSource, sibling} {
					file, err := parser.ParseFile(fset, "oracle.go", source, 0)
					if err != nil {
						t.Fatal(err)
					}
					files = append(files, file)
				}
				info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
				cfg := types.Config{Importer: &scalarOracleImporter{packages: map[string]*types.Package{}}, Sizes: &types.StdSizes{WordSize: 4, MaxAlign: 4}, DisableUnusedImportCheck: true}
				_, typeErr := cfg.Check("example/components", fset, files, info)
				if tc.name != "signal" && tc.name != "incompatible value" && typeErr != nil {
					t.Fatalf("invalid authored Go oracle: %v", typeErr)
				}
				if tc.name == "incompatible value" && typeErr == nil {
					t.Fatal("authored numeric value must fail the string Set argument")
				}
				u, err := ir.LowerIslandAOT(p, 0)
				if tc.reason != "" {
					if err == nil || !strings.Contains(err.Error(), tc.reason) {
						t.Fatalf("want %s, got %v; authored Go error: %v", tc.reason, err, typeErr)
					}
					return
				}
				if typeErr != nil || err != nil {
					t.Fatalf("authored scopes must agree: Go=%v AOT=%v", typeErr, err)
				}
				admitted++
				var scalar types.TypeAndValue
				ast.Inspect(files[0], func(node ast.Node) bool {
					assign, ok := node.(*ast.AssignStmt)
					if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
						return true
					}
					name, ok := assign.Lhs[0].(*ast.Ident)
					value := info.Types[assign.Rhs[0]]
					if ok && name.Name == "_" && oracleBasicKind(value.Type) != "" {
						scalar = value
					}
					return true
				})
				id := oracleProgramRoot(u.Program, "text")
				kind := oracleBasicKind(scalar.Type)
				if id < 0 || kind == "" || !oracleConstantFits(scalar, kind) {
					t.Fatal("missing representable scalar oracle root")
				}
				if got := u.Contract.Expressions[id].Kind; got != kind {
					t.Fatalf("AOT kind=%s Go kind=%s", got, kind)
				}
				fallback, err := ir.LowerIsland(p, 0)
				if err != nil {
					t.Fatal(err)
				}
				t.Run("VM-to-VM", func(t *testing.T) {
					left, right := vm.NewVM(u.Program, nil), vm.NewVM(fallback, nil)
					vm.InitSignals(left, u.Program)
					vm.InitSignals(right, fallback)
					t.Cleanup(func() { left.SwapProgram(&program.Program{}); right.SwapProgram(&program.Program{}) })
					a, _ := json.Marshal(left.EvalTree())
					b, _ := json.Marshal(right.EvalTree())
					if !bytes.Equal(a, b) {
						t.Fatal("VM output changed")
					}
				})
			}) {
				disagreements++
			}
		}
	}
	t.Logf("corpus=%d admitted=%d disagreements=%d skips=0", len(cases)*2, admitted, disagreements)
}
