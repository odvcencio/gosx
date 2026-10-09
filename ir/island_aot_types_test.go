//go:build !tinygo

package ir_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/aot"
	"m31labs.dev/gosx/island/program"
)

func TestIslandAOTExplicitSignalTypes(t *testing.T) {
	for _, constructor := range []string{"New", "NewShared", "Shared"} {
		for _, tc := range []struct {
			typ, init string
		}{
			{"int", "0"}, {"int32", "0"}, {"bool", "true"}, {"string", `"ready"`},
			{"int64", "0"}, {"uint", "0"}, {"rune", "0"}, {"float64", "0"},
			{"CounterInt", "0"}, {"CounterAlias", "0"}, {"MissingInt", "0"},
		} {
			t.Run(constructor+"/"+tc.typ, func(t *testing.T) {
				p := parseAOTSignalConstructor(t, constructor, tc.typ, tc.init)
				if p.Components[0].Scope == nil {
					t.Fatal("constructor did not produce signal metadata")
				}
				signals := p.Components[0].Scope.Signals
				if len(signals) != 1 {
					t.Fatalf("signals = %+v", signals)
				}
				if signals[0].SourceType != tc.typ {
					t.Errorf("source type = %q, want %q", signals[0].SourceType, tc.typ)
				}
				u, err := ir.LowerIslandAOT(p, 0)
				if constructor != "New" {
					if err == nil {
						t.Fatal("shared constructor admitted")
					}
					return
				}
				switch tc.typ {
				case "int", "int32", "bool", "string", "rune", "CounterAlias":
					want := aot.ScalarKind(tc.typ)
					if tc.typ == "rune" || tc.typ == "CounterAlias" {
						want = aot.Int32
					}
					if err != nil {
						t.Fatal(err)
					}
					if len(u.Contract.Signals) != 1 || u.Contract.Signals[0].Kind != want {
						t.Fatalf("signal contract = %+v", u.Contract.Signals)
					}
					binding := u.Program.Nodes[u.Program.Nodes[u.Program.Root].Children[0]].Expr
					if got := u.Contract.Expressions[binding].Kind; got != want {
						t.Fatalf("signal read kind = %s, want %s", got, tc.typ)
					}
				default:
					var diagnostic *ir.DiagnosticsError
					if !errors.As(err, &diagnostic) || diagnostic.Diagnostics[0].Code != "aot_source_type" {
						t.Fatalf("accepted unproved constructor type: %v", err)
					}
				}
				// Constructor source evidence must not change VM lowering, even
				// when the AOT path rejects the explicit type.
				implicit := parseAOTSignalConstructor(t, constructor, "", tc.init)
				before, err := ir.LowerIsland(implicit, 0)
				if err != nil {
					t.Fatal(err)
				}
				after, err := ir.LowerIsland(p, 0)
				if err != nil {
					t.Fatal(err)
				}
				beforeBytes, err := program.EncodeBinary(before)
				if err != nil {
					t.Fatal(err)
				}
				afterBytes, err := program.EncodeBinary(after)
				if err != nil || !bytes.Equal(beforeBytes, afterBytes) {
					t.Fatalf("explicit type changed the VM program: %v", err)
				}
				if tc.typ == "int32" {
					inferred, err := ir.LowerIslandAOT(implicit, 0)
					if err != nil {
						t.Fatal(err)
					}
					if u.Digest == inferred.Digest {
						t.Fatal("explicit int32 has the inferred int digest")
					}
					if !bytes.Equal(u.ProgramBytes, inferred.ProgramBytes) {
						t.Fatal("source types changed fallback bytes")
					}
				}
			})
		}
	}
}

func parseAOTSignalConstructor(t *testing.T, constructor, typ, init string) *ir.Program {
	t.Helper()
	args := init
	if constructor != "New" {
		args = `"count", ` + init
	}
	if typ != "" {
		constructor += "[" + typ + "]"
	}
	src := []byte(fmt.Sprintf(`package example
import sig "m31labs.dev/gosx/signal"
type CounterInt int
type CounterAlias = int32
//gosx:island
func Counter() Node {
 count := sig.%s(%s)
 return <div>{count.Get()}</div>
}`, constructor, args))
	p, err := parseAOT(t, src)
	if err != nil {
		t.Fatal(err)
	}
	p.PackagePath = "example/components"
	return p
}

func TestIslandAOTConditionalSourceKinds(t *testing.T) {
	for _, condition := range []string{"true", "false"} {
		for _, tc := range []struct {
			typ, yes, no string
			prop         vm.Value
		}{
			{"int", "1", "2", vm.IntVal(7)},
			{"int32", "props.Initial", "1", vm.IntVal(7)},
			{"int32", "1", "props.Initial", vm.IntVal(7)},
			{"int32", "props.Initial", "-(1 + 2)", vm.IntVal(7)},
			{"int32", "(1 + 2) * 3", "props.Initial", vm.IntVal(7)},
			{"int32", "props.Initial + 1", "props.Initial - 1", vm.IntVal(7)},
			{"int32", "(true ? props.Initial : 1)", "2", vm.IntVal(7)},
			{"string", "props.Initial", `"ready"`, vm.StringVal("value")},
			{"string", `"ready"`, "props.Initial", vm.StringVal("value")},
			{"bool", "props.Initial", "false", vm.BoolVal(true)},
			{"bool", "false", "props.Initial", vm.BoolVal(true)},
		} {
			expr := condition + " ? " + tc.yes + " : " + tc.no
			t.Run(tc.typ+"/"+expr, func(t *testing.T) {
				p := parseAOTArithmetic(t, tc.typ, expr)
				_, err := ir.LowerIslandAOT(p, 0)
				if err == nil || !strings.Contains(err.Error(), "evidence_shape_mismatch") {
					t.Fatalf("ternary requires VM: %v", err)
				}
				fallback, err := ir.LowerIsland(p, 0)
				if err != nil {
					t.Fatal(err)
				}
				binding := fallback.Nodes[fallback.Nodes[fallback.Root].Children[0]].Expr
				machine := vm.NewVM(fallback, map[string]vm.Value{"Initial": tc.prop})
				branch := fallback.Exprs[binding].Operands[2]
				if condition == "true" {
					branch = fallback.Exprs[binding].Operands[1]
				}
				got, want := machine.Eval(binding), machine.Eval(branch)
				if got.Type != want.Type || !got.Eq(want).Truth() {
					t.Fatalf("VM branch changed: %v vs %v", got, want)
				}

			})
		}
	}
}

func TestIslandAOTConditionalRequiresCompatibleEvidence(t *testing.T) {
	for _, condition := range []string{"true", "false"} {
		for _, branches := range []string{
			`1 : "x"`, `"x" : 1`, "true : 1", "1 : false",
			"props.Initial : props.Other", "props.Other : props.Initial",
			"props.Initial : 2147483648", "2147483648 : props.Initial",
			"props.Initial : (2147483647 + 1)", "(2147483647 + 1) : props.Initial",
		} {
			expr := condition + " ? " + branches
			t.Run(expr, func(t *testing.T) {
				p := parseAOTArithmetic(t, "int32", expr)
				_, err := ir.LowerIslandAOT(p, 0)
				var diagnostic *ir.DiagnosticsError
				if !errors.As(err, &diagnostic) || diagnostic.Diagnostics[0].Code != "aot_source_type" {
					t.Fatalf("accepted incompatible conditional: %v", err)
				}
			})
		}
	}
	for _, condition := range []string{"1", `"x"`, "props.Initial"} {
		t.Run("condition/"+condition, func(t *testing.T) {
			p := parseAOTArithmetic(t, "int32", condition+" ? 1 : 2")
			_, err := ir.LowerIslandAOT(p, 0)
			var diagnostic *ir.DiagnosticsError
			if !errors.As(err, &diagnostic) || diagnostic.Diagnostics[0].Code != "aot_source_type" {
				t.Fatalf("accepted non-boolean condition: %v", err)
			}
		})
	}
}

func TestIslandAOTConstantDomainBoundaries(t *testing.T) {
	for _, boundary := range []struct {
		name, literal, arithmetic string
		allowed                   bool
	}{
		{"maximum", "2147483647", "2147483646 + 1", true},
		{"maximum_plus_one", "2147483648", "2147483647 + 1", false},
		{"minimum", "-2147483648", "-2147483647 - 1", true},
		{"minimum_minus_one", "-2147483649", "-2147483648 - 1", false},
	} {
		for _, shape := range []struct{ name, expr string }{
			{"literal", boundary.literal},
			{"arithmetic", boundary.arithmetic},
			{"conditional_true", "true ? " + boundary.literal + " : 0"},
			{"conditional_false", "false ? 0 : " + boundary.literal},
			{"conditional_unselected_true", "true ? 0 : " + boundary.arithmetic},
			{"conditional_unselected_false", "false ? " + boundary.arithmetic + " : 0"},
		} {
			t.Run(shape.name+"/"+boundary.name, func(t *testing.T) {
				p := parseAOTArithmetic(t, "int", shape.expr)
				u, err := ir.LowerIslandAOT(p, 0)
				if !boundary.allowed || strings.HasPrefix(shape.name, "conditional") {
					var diagnostic *ir.DiagnosticsError
					if !errors.As(err, &diagnostic) || diagnostic.Diagnostics[0].Code != "aot_source_type" {
						t.Fatalf("accepted out-of-domain constant: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				binding := u.Program.Nodes[u.Program.Nodes[u.Program.Root].Children[0]].Expr
				if got := u.Contract.Expressions[binding].Kind; got != aot.Int {
					t.Fatalf("boundary kind = %s, want int", got)
				}
				want := boundary.literal
				if shape.name == "conditional_unselected_true" || shape.name == "conditional_unselected_false" {
					want = "0"
				}
				if got := vm.NewVM(u.Program, nil).Eval(binding).String(); got != want {
					t.Fatalf("boundary value = %s, want %s", got, want)
				}
				fallback, err := ir.LowerIsland(p, 0)
				if err != nil {
					t.Fatal(err)
				}
				before, err := program.EncodeBinary(fallback)
				if err != nil || !bytes.Equal(before, u.ProgramBytes) {
					t.Fatalf("changed boundary fallback bytes: %v", err)
				}
			})
		}
	}
}

func TestIslandAOTRejectsOutOfDomainStateAndOperands(t *testing.T) {
	for _, body := range []string{
		`return <div title={2147483648} />`,
		`count := signal.New(2147483648); return <div>{count}</div>`,
		`count := signal.NewShared("count", -2147483649); return <div>{count}</div>`,
		`count := signal.Derive(func() int { return 2147483647 + 1 }); return <div>{count}</div>`,
		`count := signal.New(0); change := func() { count.Set(-2147483648 - 1) }; return <button onClick={change}>{count}</button>`,
		`return <div>{2147483648 == 0}</div>`,
		`return <div>{-(-2147483648)}</div>`,
		`return <div>{(2147483647 + 1) - 1}</div>`,
	} {
		t.Run(body, func(t *testing.T) {
			p, err := parseAOT(t, []byte("package example\n//gosx:island\nfunc Counter() Node {\n"+body+"\n}\n"))
			if err != nil {
				t.Fatal(err)
			}
			p.PackagePath = "example/components"
			if _, err := ir.LowerIsland(p, 0); err != nil {
				t.Fatalf("VM lowering rejected the constant: %v", err)
			}
			_, err = ir.LowerIslandAOT(p, 0)
			var diagnostic *ir.DiagnosticsError
			if !errors.As(err, &diagnostic) || diagnostic.Diagnostics[0].Code != "aot_source_type" {
				t.Fatalf("accepted out-of-domain state or operand: %v", err)
			}
		})
	}
}
