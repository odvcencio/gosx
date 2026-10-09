//go:build !tinygo

package ir_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/aot"
	"m31labs.dev/gosx/island/program"
)

func TestIslandAOTRejectsShadowedScalarNames(t *testing.T) {
	for _, typ := range []string{"int", "int32", "bool", "string"} {
		for _, declaration := range []string{"type " + typ + " = int64", "type " + typ + " int64"} {
			for _, body := range []string{
				fmt.Sprintf("count := signal.New[%s](0)\nreturn <div>{count}</div>", typ),
				fmt.Sprintf("count := signal.NewShared[%s](\"count\", 0)\nreturn <div>{count}</div>", typ),
				fmt.Sprintf("count := signal.Shared[%s](\"count\", 0)\nreturn <div>{count}</div>", typ),
				fmt.Sprintf("count := signal.Derive(func() %s { return 0 })\nreturn <div>{count}</div>", typ),
				"return <div>{props.Initial}</div>",
				"return <div>{props.Detail.Value}</div>",
			} {
				t.Run(declaration+"/"+body, func(t *testing.T) {
					p := parseAOTAdmission(t, declaration, "", typ, body)
					assertAOTAdmissionRejected(t, p)
				})
			}
		}
	}
	for _, local := range []string{
		"type int32 = int64", "type int32 int64", "var int32 = 0", "const int32 = 0",
		"int32 := 0", "{ type int32 = int64 }",
	} {
		t.Run("local/"+local, func(t *testing.T) {
			p := parseAOTAdmission(t, "", "", "int32", local+"\ncount := signal.New[int32](0)\nreturn <div>{count}</div>")
			assertAOTAdmissionRejected(t, p)
		})
	}
	for _, parameter := range []string{", int32 int", ", int32 ...int"} {
		t.Run("parameter/"+parameter, func(t *testing.T) {
			p := parseAOTAdmission(t, "", parameter, "int32", "count := signal.New[int32](0)\nreturn <div>{count}</div>")
			assertAOTAdmissionRejected(t, p)
		})
	}
	for _, declaration := range []string{
		"var unused, int32 = 0, 0", "const (\nunused = 0\nint32 = 0\n)",
		"for int32 := range props.Label {}", "if true { type int32 = int64 }",
	} {
		t.Run("nested or grouped/"+declaration, func(t *testing.T) {
			p := parseAOTAdmission(t, "", "", "int32", declaration+"\nreturn <div>{props.Initial}</div>")
			assertAOTAdmissionRejected(t, p)
		})
	}
	t.Run("type parameter", func(t *testing.T) {
		p := parseAOTAdmission(t, "func helper[int32 any](s string) int { return 0 }", "", "int32", "return <div>{props.Initial}</div>")
		assertAOTAdmissionRejected(t, p)
	})
	// Package declarations bind names throughout the file, even below a use.
	src := []byte(`package example
//gosx:island
func Counter() Node {
 count := signal.New[int32](0)
 return <div>{count}</div>
}
type int32 = int64
`)
	p, err := parse(t, src)
	if err != nil {
		t.Fatal(err)
	}
	p.PackagePath = "example/components"
	assertAOTAdmissionRejected(t, p)
}

func TestIslandAOTRejectsPackageSiblingShadows(t *testing.T) {
	for _, extension := range []string{".go", ".gsx"} {
		for _, declaration := range []string{"type int32 = int64", "func len(s string) int64 { return 99 }", "var signal = 0"} {
			t.Run(extension+"/"+declaration, func(t *testing.T) {
				p := parseAOTAdmission(t, "", "", "int32", "count := signal.New[int32](0)\nreturn <div>{count}{props.Initial}{len(props.Label)}</div>")
				p.Dir = t.TempDir()
				if err := os.WriteFile(filepath.Join(p.Dir, "types"+extension), []byte("package example\n"+declaration+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				assertAOTAdmissionRejected(t, p)
			})
		}
	}
}

func TestIslandAOTRejectsUnresolvedCallableNames(t *testing.T) {
	for _, tc := range []struct{ name, prelude, parameter, body string }{
		{"package len", `func len(s string) int64 { return 99 }`, "", `return <div>{len(props.Label)}</div>`},
		{"local len", "", "", "len := 99\nreturn <div>{len(props.Label)}</div>"},
		{"parameter len", "", ", len func(string) int64", `return <div>{len(props.Label)}</div>`},
		{"import len", `import len "example/custom"`, "", `return <div>{len(props.Label)}</div>`},
		{"case folded len", `func LEN(s string) int { return 99 }`, "", `return <div>{LEN(props.Label)}</div>`},
		{"case folded signal method", "", "", "count := signal.New(0)\nreturn <div>{count.get()}</div>"},
		{"field length", "", "", `return <div>{props.Label.length}</div>`},
		{"method length", "", "", `return <div>{props.Label.Len()}</div>`},
		{"method conversion", "", "", `return <div>{props.Initial.ToString()}</div>`},
		{"function conversion", "", "", "count := signal.New[string](string(0))\nreturn <div>{count}</div>"},
		{"generic call initializer", `func label[T any](n int) string { return "ready" }`, "", "count := signal.New[string](label[string](0))\nreturn <div>{count}</div>"},
		{"unproved call initializer", "", "", "count := signal.New[string](props.Label.Get())\nreturn <div>{count}</div>"},
		{"shadowed true", `const true = false`, "", `return <div>{true}</div>`},
		{"shadowed false", `const false = true`, "", `return <div>{false}</div>`},
		{"shadowed constructor", `var signal = 0`, "", "count := signal.New(0)\nreturn <div>{count}</div>"},
		{"shadowed computed constructor", `var signal = 0`, "", "count := signal.Derive(func() int { return 0 })\nreturn <div>{count}</div>"},
		{"unresolved import", `import "example/custom"`, "", `return <div>{len(props.Label)}</div>`},
		{"type import", `import int32 "example/custom"`, "", `return <div>{props.Initial}</div>`},
		{"constructor alias shadow", `import s "m31labs.dev/gosx/signal"; var s = 0`, "", "count := s.New(0)\nreturn <div>{count}</div>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := parseAOTAdmission(t, tc.prelude, tc.parameter, "int32", tc.body)
			assertAOTAdmissionRejected(t, p)
		})
	}
}

func TestIslandAOTRejectsAggregateValueConsumers(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"DOM text", `return <div>{props.Detail}{props.Detail.Value}</div>`},
		{"aggregate and string leaf", `return <div>{props.Detail}{props.Detail.Label}</div>`},
		{"whole props", `return <div>{props}{props.Detail.Value}</div>`},
		{"attribute", `return <div title={props.Detail}>{props.Detail.Value}</div>`},
		{"conditional predicate", `return <div>{props.Detail ? 1 : 2}{props.Detail.Value}</div>`},
		{"conditional arms", `return <div>{true ? props.Detail : props.Detail}{props.Detail.Value}</div>`},
		{"arithmetic", `return <div>{props.Detail + 1}{props.Detail.Value}</div>`},
		{"negation", `return <div>{-props.Detail}{props.Detail.Value}</div>`},
		{"comparison", `return <div>{props.Detail == props.Detail}{props.Detail.Value}</div>`},
		{"ordered comparison", `return <div>{props.Detail < props.Detail}{props.Detail.Value}</div>`},
		{"logical operand", `return <div>{props.Detail && true}{props.Detail.Value}</div>`},
		{"logical negation", `return <div>{!props.Detail}{props.Detail.Value}</div>`},
		{"length argument", `return <div>{len(props.Detail)}{props.Detail.Value}</div>`},
		{"conversion receiver", `return <div>{props.Detail.ToString()}{props.Detail.Value}</div>`},
		{"signal initializer", "count := signal.New[string](props.Detail)\nreturn <div>{count}{props.Detail.Value}</div>"},
		{"computed result", "count := signal.Derive(func() string { return props.Detail })\nreturn <div>{count}{props.Detail.Value}</div>"},
		{"handler argument", "count := signal.New(0)\nchange := func() { count.Set(props.Detail) }\nreturn <button onClick={change}>{count}{props.Detail.Value}</button>"},
		{"handler result", "change := func() { props.Detail }\nreturn <button onClick={change}>{props.Detail.Value}</button>"},
		{"conditional node", `return <div><If when={props.Detail}><span>yes</span></If>{props.Detail.Value}</div>`},
		{"conditional fallback", `return <div><If when={true} fallback={props.Detail}><span>yes</span></If>{props.Detail.Value}</div>`},
		{"iteration source", `return <div><Each of={props.Detail} as="item"><span>item</span></Each>{props.Detail.Value}</div>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := parseAOTAdmission(t, "", "", "int32", tc.body)
			assertAOTAdmissionRejected(t, p)
		})
	}
}

func TestIslandAOTAdmitsProvedNamesAndSelectors(t *testing.T) {
	for _, body := range []string{
		`return <div>{props.Detail.Value}{len(props.Label)}</div>`,
		`return <div>{props.Initial + 1}{true ? props.Detail.Value : props.Initial}</div>`,
		"count := signal.New[int32](0)\nchange := func() { count.Set(props.Initial) }\nreturn <button onClick={change}>{count.Get()}{props.Detail.Value}</button>",
		"count := signal.Derive(func() int32 { return props.Detail.Value })\nreturn <div>{count.Get()}</div>",
	} {
		t.Run(body, func(t *testing.T) {
			p := parseAOTAdmission(t, "", "", "int32", body)
			u, err := ir.LowerIslandAOT(p, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, input := range u.Contract.Inputs {
				if input.Kind == aot.SelectorPath {
					t.Fatal("aggregate was recorded as a scalar input")
				}
			}
			fallback, err := ir.LowerIsland(p, 0)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := program.EncodeBinary(fallback)
			if err != nil || !bytes.Equal(raw, u.ProgramBytes) {
				t.Fatalf("changed VM bytes: %v", err)
			}
		})
	}
}

func TestIslandAOTProvedImportBindings(t *testing.T) {
	for _, tc := range []struct{ prelude, body string }{
		{`import "m31labs.dev/gosx/signal"`, "count := signal.New[int32](0)\nreturn <div>{count.Get()}</div>"},
		{`import s "m31labs.dev/gosx/signal"`, "count := s.New[int32](0)\nreturn <div>{count.Get()}</div>"},
		{`import . "m31labs.dev/gosx/signal"`, "count := New(0)\nreturn <div>{count.Get()}</div>"},
	} {
		t.Run(tc.prelude, func(t *testing.T) {
			p := parseAOTAdmission(t, tc.prelude, "", "int32", tc.body)
			p.Dir = t.TempDir()
			// Field names do not shadow lexical types, constants or functions.
			if err := os.WriteFile(filepath.Join(p.Dir, "fields.go"), []byte("package example\ntype Names struct { int32 int; len int; signal int; true bool }\n"), 0600); err != nil {
				t.Fatal(err)
			}
			before, err := ir.LowerIsland(p, 0)
			if err != nil {
				t.Fatal(err)
			}
			u, err := ir.LowerIslandAOT(p, 0)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := program.EncodeBinary(before)
			if err != nil || !bytes.Equal(raw, u.ProgramBytes) {
				t.Fatalf("changed VM bytes: %v", err)
			}
		})
	}
}

func parseAOTAdmission(t *testing.T, prelude, parameter, typ, body string) *ir.Program {
	t.Helper()
	source := fmt.Sprintf(`package example
%s
type Detail struct { Value %s; Label string }
type CounterProps struct { Initial %s; Label string; Detail Detail }
//gosx:island
func Counter(props CounterProps%s) Node {
%s
}
`, prelude, typ, typ, parameter, body)
	p, err := parse(t, []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	p.PackagePath = "example/components"
	return p
}

func assertAOTAdmissionRejected(t *testing.T, p *ir.Program) {
	t.Helper()
	if _, err := ir.LowerIsland(p, 0); err != nil {
		t.Fatalf("VM lowering failed: %v", err)
	}
	_, err := ir.LowerIslandAOT(p, 0)
	var diagnostic *ir.DiagnosticsError
	if !errors.As(err, &diagnostic) || !strings.HasPrefix(diagnostic.Diagnostics[0].Code, "aot_") {
		t.Fatalf("admitted an unproved source name or aggregate value: %v", err)
	}
}
