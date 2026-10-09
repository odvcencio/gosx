//go:build !tinygo

package ir_test

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/aot"
	"m31labs.dev/gosx/island/program"
)

func TestIslandAOTSourceContract(t *testing.T) {
	src := []byte(`package example
type CounterProps struct { Label string; Initial int32 }
//gosx:island
func Counter(props CounterProps) Node {
 count := signal.New(0)
 doubled := signal.Derive(func() int { return 0 }); _ = doubled
 increment := func() {}
 return <div><button type="button" onClick={increment}>Count: {count.Get()}</button><span>{props.Label}{props.Initial}</span></div>
}`)
	p, err := parseAOT(t, src)
	if err != nil {
		t.Fatal(err)
	}
	p.PackagePath = "example/components"
	vmProgram, err := ir.LowerIsland(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	vmBytes, err := program.EncodeBinary(vmProgram)
	if err != nil {
		t.Fatal(err)
	}
	u, err := ir.LowerIslandAOT(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(vmBytes, u.ProgramBytes) {
		t.Fatal("source evidence changed the fallback program")
	}
	if u.Component != "example/components.Counter" || len(u.Contract.Expressions) != len(vmProgram.Exprs) {
		t.Fatalf("incomplete contract: %+v", u.Contract)
	}
	if len(u.Contract.Inputs) != 2 || u.Contract.Inputs[0].Root != "Initial" || u.Contract.Inputs[0].Kind != aot.Int32 || u.Contract.Inputs[1].Root != "Label" {
		t.Fatalf("inputs: %+v", u.Contract.Inputs)
	}
	if len(u.Contract.Signals) != 1 || u.Contract.Signals[0].Kind != aot.Int || len(u.Contract.Computeds) != 1 || u.Contract.Computeds[0].Kind != aot.Int {
		t.Fatalf("state evidence: %+v", u.Contract)
	}
	if len(u.Contract.Bindings) != 5 || len(u.Contract.Bindings[2].Nodes) != 2 || u.Contract.Bindings[2].Kind != program.NodeText {
		t.Fatalf("physical text groups: %+v", u.Contract.Bindings)
	}
	u2, err := ir.LowerIslandAOT(p, 0)
	if err != nil || u.Digest != u2.Digest {
		t.Fatalf("nondeterministic contract: %v", err)
	}
}

func TestIslandAOTRejectsErasedAndUnsupportedSourceTypes(t *testing.T) {
	for _, typ := range []string{"int8", "int16", "int64", "uint", "uint32", "float32", "float64", "CounterInt"} {
		t.Run(typ, func(t *testing.T) {
			src := []byte(fmt.Sprintf(`package example
type CounterInt int
//gosx:island
func Counter() Node {
 derived := signal.Derive(func() %s { return 1 })
 return <div>{derived}</div>
}`, typ))
			p, err := parseAOT(t, src)
			if err != nil {
				t.Fatal(err)
			}
			p.PackagePath = "example/components"
			_, err = ir.LowerIslandAOT(p, 0)
			var diagnostic *ir.DiagnosticsError
			if !errors.As(err, &diagnostic) || diagnostic.Diagnostics[0].Code != "aot_source_type" {
				t.Fatalf("unproved %s: %v", typ, err)
			}
		})
	}
}

func TestIslandAOTRejectsRuneStringErasure(t *testing.T) {
	for _, body := range []string{`label := signal.New('x'); return <div>{label}</div>`, `return <div>{'x'}</div>`, `return <div><Badge /></div>`} {
		src := []byte("package example\n//gosx:island\nfunc Counter() Node { " + body + " }\ncomponent Badge() { return <span>{'x'}</span> }")
		p, err := parseAOT(t, src)
		if err != nil {
			t.Fatal(err)
		}
		p.PackagePath = "example/components"
		if _, err := ir.LowerIslandAOT(p, 0); err == nil {
			t.Fatalf("accepted erased rune: %s", body)
		}
	}
}

func TestIslandAOTMissingTypeEvidenceAndIdentity(t *testing.T) {
	p := &ir.Program{PackagePath: "example/components", Nodes: []ir.Node{{Kind: ir.NodeElement, Tag: "div"}}, Components: []ir.Component{{Name: "Counter", IsIsland: true, Scope: &ir.ComponentScope{Signals: []ir.SignalInfo{{Name: "n", InitExpr: "1", TypeHint: "int"}}}}}}
	if _, err := ir.LowerIslandAOT(p, 0); err == nil {
		t.Fatal("accepted a VM hint as source type proof")
	}
	p.Components[0].Scope = nil
	p.PackagePath = ""
	if _, err := ir.LowerIslandAOT(p, 0); err == nil {
		t.Fatal("accepted an unqualified identity")
	}
	for _, index := range []int{-1, 1} {
		if _, err := ir.LowerIslandAOT(p, index); err == nil {
			t.Fatal("accepted invalid component index")
		}
	}
	if _, err := ir.LowerIslandAOT(nil, 0); err == nil {
		t.Fatal("accepted absent source")
	}
}

func TestIslandAOTNestedInputsAndHandlers(t *testing.T) {
	src := []byte(`package example
type Detail struct { Label string }
type EditorProps struct { Detail Detail }
//gosx:island
func Editor(props EditorProps) Node {
 text := signal.New("")
 edit := func() {}
 return <div><input value={text.Get()} onInput={edit} /><span>{props.Detail.Label}{props.Detail.Label}</span></div>
}`)
	p, err := parseAOT(t, src)
	if err != nil {
		t.Fatal(err)
	}
	p.PackagePath = "example/components"
	u, err := ir.LowerIslandAOT(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Contract.Inputs) != 1 {
		t.Fatalf("inputs: %+v", u.Contract.Inputs)
	}
	input := u.Contract.Inputs[0]
	if input.Root != "Detail" || len(input.Path) != 1 || input.Path[0] != "Label" || len(input.Exprs) != 2 {
		t.Fatalf("selector interning: %+v", input)
	}
}

func TestIslandAOTArithmeticSourceKinds(t *testing.T) {
	for _, typ := range []string{"int", "int32", "int64", "float64"} {
		for _, op := range []string{"+", "-", "*", "/", "%"} {
			for _, literal := range []string{"1", "-2", "(1 + 2)", "-(1 + 2)"} {
				for _, expr := range []string{"props.Initial " + op + " " + literal, literal + " " + op + " props.Initial"} {
					t.Run(typ+"/"+expr, func(t *testing.T) {
						p := parseAOTArithmetic(t, typ, expr)
						vm, err := ir.LowerIsland(p, 0)
						if err != nil {
							t.Fatal(err)
						}
						u, err := ir.LowerIslandAOT(p, 0)
						if typ == "int64" || typ == "float64" || op == "/" || op == "%" {
							if err == nil {
								t.Fatal("accepted arithmetic outside the scalar profile")
							}
							return
						}
						if err != nil {
							t.Fatal(err)
						}
						binding := vm.Nodes[vm.Nodes[vm.Root].Children[0]].Expr
						if got := u.Contract.Expressions[binding].Kind; got != aot.ScalarKind(typ) {
							t.Fatalf("arithmetic result kind = %s, want %s", got, typ)
						}
						before, err := program.EncodeBinary(vm)
						if err != nil || !bytes.Equal(before, u.ProgramBytes) {
							t.Fatalf("changed the fallback program: %v", err)
						}
					})
				}
			}
		}
	}
}

func TestIslandAOTArithmeticRequiresCompatibleEvidence(t *testing.T) {
	for _, op := range []string{"+", "-", "*"} {
		for _, expr := range []string{
			"props.Initial " + op + " props.Other",
			"props.Other " + op + " props.Initial",
			"props.Initial " + op + " (props.Other + 1)",
			"(1 + props.Other) " + op + " props.Initial",
			"props.Initial " + op + " 1.0",
			"1.0 " + op + " props.Initial",
			"props.Initial " + op + " (1 + 2.0)",
			"(2.0 + 1) " + op + " props.Initial",
			"props.Initial " + op + " 2147483648",
			"2147483648 " + op + " props.Initial",
			"props.Initial " + op + " (2147483647 + 1)",
			"(2147483647 + 1) " + op + " props.Initial",
			"props.Initial " + op + " true",
			"true " + op + " props.Initial",
		} {
			t.Run(expr, func(t *testing.T) {
				p := parseAOTArithmetic(t, "int32", expr)
				_, err := ir.LowerIslandAOT(p, 0)
				var diagnostic *ir.DiagnosticsError
				if !errors.As(err, &diagnostic) || diagnostic.Diagnostics[0].Code != "aot_source_type" {
					t.Fatalf("accepted unproved arithmetic: %v", err)
				}
			})
		}
	}
}

func TestIslandAOTArithmeticPreservesTypedExpressions(t *testing.T) {
	for _, typ := range []string{"int", "int32"} {
		for _, expr := range []string{
			"props.Initial + props.Initial",
			"props.Initial + (1 + 2) * (3 - 1)",
			"(1 + 2) * (3 - 1) + props.Initial",
			"-(props.Initial + 1)",
			"(props.Initial + 1) * (2 - props.Initial)",
		} {
			t.Run(typ+"/"+expr, func(t *testing.T) {
				p := parseAOTArithmetic(t, typ, expr)
				u, err := ir.LowerIslandAOT(p, 0)
				if err != nil {
					t.Fatal(err)
				}
				binding := u.Program.Nodes[u.Program.Nodes[u.Program.Root].Children[0]].Expr
				if got := u.Contract.Expressions[binding].Kind; got != aot.ScalarKind(typ) {
					t.Fatalf("arithmetic result kind = %s, want %s", got, typ)
				}
			})
		}
	}
}

func parseAOTArithmetic(t *testing.T, typ, expr string) *ir.Program {
	t.Helper()
	src := []byte(fmt.Sprintf(`package example
type CounterProps struct { Initial %s; Other int }
//gosx:island
func Counter(props CounterProps) Node {
 return <div>{%s}</div>
}`, typ, expr))
	p, err := parseAOT(t, src)
	if err != nil {
		t.Fatal(err)
	}
	p.PackagePath = "example/components"
	return p
}
