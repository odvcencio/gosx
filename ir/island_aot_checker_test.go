//go:build !tinygo

package ir_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
)

func TestIslandAOTCheckerRoundSix(t *testing.T) {
	for _, tc := range []struct{ name, declaration, body, reason string }{
		{"local binding", "", `Value := "hello"; _ = Value; return <div>{Value}{props.Value}</div>`, "evidence_binding_mismatch"},
		{"package binding", `const Value = "hello"`, `return <div>{Value}{props.Value}</div>`, "evidence_binding_mismatch"},
		{"constructor arity", "", `count := signal.New[int32](0, 7); return <div>{count.Get()}</div>`, "type_error"},
		{"derive argument", "", `count := signal.Derive[int64](func() int32 { return 0 }); return <div>{count.Get()}</div>`, "type_error"},
		{"derive parameters", "", `count := signal.Derive(func(x int) int { return x }); return <div>{count.Get()}</div>`, "type_error"},
		{"shared key type", "", `count := signal.NewShared[int](123, 0); return <div>{count.Get()}</div>`, "type_error"},
		{"shadowed type", `type int32 = int64`, `count := signal.New[int32](0); return <div>{count.Get()}</div>`, "unsupported_type"},
		{"defined type", `type Score int`, `count := signal.New[Score](0); return <div>{count.Get()}</div>`, "unsupported_type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := checkerProgram(t, tc.declaration, tc.body)
			_, err := ir.LowerIslandAOT(p, 0)
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("want %s, got %v", tc.reason, err)
			}
		})
	}
}

func TestIslandAOTCheckerPackageScope(t *testing.T) {
	t.Run("unknown", func(t *testing.T) {
		p := checkerProgram(t, "", `return <div>{props.Value}</div>`)
		p.Dir = ""
		_, err := ir.LowerIslandAOT(p, 0)
		if err == nil || !strings.Contains(err.Error(), "package_scope_unknown") {
			t.Fatalf("unknown package admitted: %v", err)
		}
	})
	t.Run("selected sibling", func(t *testing.T) {
		p := checkerProgram(t, "", `count := signal.New[int32](0); return <div>{count.Get()}</div>`)
		if err := os.WriteFile(filepath.Join(p.Dir, "types.go"), []byte("package example\ntype int32 = int64\n"), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := ir.LowerIslandAOT(p, 0)
		if err == nil || !strings.Contains(err.Error(), "unsupported_type") {
			t.Fatal(err)
		}
	})
	t.Run("unrelated error and excluded sibling", func(t *testing.T) {
		p := checkerProgram(t, "", `count := signal.New[int32](0); return <div>{count.Get()}</div>`)
		for name, source := range map[string]string{
			"unrelated.go":   "package example\nvar invalid = absent\n",
			"excluded.go":    "//go:build ignore\n\npackage example\ntype int32 = int64\n",
			"types_test.go":  "package example\ntype int32 = int64\n",
			"types_test.gsx": "package example\ntype int32 = int64\n",
		} {
			if err := os.WriteFile(filepath.Join(p.Dir, name), []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := ir.LowerIslandAOT(p, 0); err != nil {
			t.Fatal(err)
		}
	})
}

func checkerProgram(t *testing.T, declaration, body string) *ir.Program {
	t.Helper()
	source := []byte("package example\nimport signal \"m31labs.dev/gosx/signal\"\n" + declaration + "\ntype Props struct { Value int }\n//gosx:island\nfunc Counter(props Props) Node {\n" + body + "\n}\n")
	p, err := parse(t, source)
	if err != nil {
		t.Fatal(err)
	}
	p.PackagePath, p.Dir = "example/components", t.TempDir()
	return p
}

func TestIslandAOTCheckerPositiveEvidence(t *testing.T) {
	for _, body := range []string{
		`return <div>{props.Value}</div>`,
		`count := signal.New[int32](0); return <div>{count.Get()}</div>`,
		`doubled := signal.Derive(func() int { return 2*2 }); return <div>{doubled.Get()}</div>`,
	} {
		t.Run(body, func(t *testing.T) {
			p := checkerProgram(t, "", body)
			if _, err := ir.LowerIslandAOT(p, 0); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Only forms outside the declared profile or lacking exact Go evidence stay VM-only.
func TestIslandAOTCheckerVMOnlyFixtures(t *testing.T) {
	for _, tc := range []struct{ name, body, reason string }{
		{"shared state", `count:=signal.NewShared("count",0);return <div>{count.Get()}</div>`, "shared_signal_profile"},
		{"reserved dispatch", `count:=signal.New(0);return <div data-gosx-on-click={count.Get()}/>`, "reserved_attribute"},
		{"uncovered conversion", `count := signal.New('x'); return <div>{count}</div>`, "evidence_shape_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := checkerProgram(t, "", tc.body)
			_, err := ir.LowerIslandAOT(p, 0)
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("want %s, got %v", tc.reason, err)
			}
		})
	}
	t.Run("missing directory", func(t *testing.T) {
		p := checkerProgram(t, "", `return <div>{1}</div>`)
		p.Dir = ""
		_, err := ir.LowerIslandAOT(p, 0)
		if err == nil || !strings.Contains(err.Error(), "package_scope_unknown") {
			t.Fatal(err)
		}
	})
}

func TestIslandAOTCheckerShapeAndImports(t *testing.T) {
	for _, tc := range []struct{ source, replacement string }{{"1+2", "3"}, {`"x"+""`, `"x"`}, {"1==1", "true"}} {
		t.Run("folded shape/"+tc.source, func(t *testing.T) {
			p := checkerProgram(t, "", `return <div>{`+tc.source+`}</div>`)
			for i := range p.Nodes {
				if p.Nodes[i].Kind == ir.NodeExpr {
					p.Nodes[i].Text = tc.replacement
				}
			}
			_, err := ir.LowerIslandAOT(p, 0)
			if err == nil || !strings.Contains(err.Error(), "evidence_shape_mismatch") {
				t.Fatal(err)
			}
		})
	}
	t.Run("changed source shape", func(t *testing.T) {
		p := checkerProgram(t, "", `return <div>{props.Value+1}</div>`)
		for i := range p.Nodes {
			if p.Nodes[i].Kind == ir.NodeExpr {
				p.Nodes[i].Text = "props.Value-1"
			}
		}
		_, err := ir.LowerIslandAOT(p, 0)
		if err == nil || !strings.Contains(err.Error(), "evidence_shape_mismatch") {
			t.Fatal(err)
		}
	})
	t.Run("referenced import", func(t *testing.T) {
		p, err := parseAOT(t, []byte("package example\nimport other \"example.test/other\"\n//gosx:island\nfunc Counter() Node {\n return <div>{other.Value}</div>\n}\n"))
		if err != nil {
			t.Fatal(err)
		}
		p.PackagePath = "example/components"
		_, err = ir.LowerIslandAOT(p, 0)
		if err == nil || !strings.Contains(err.Error(), "import_outside_profile") {
			t.Fatal(err)
		}
	})
	t.Run("unused import", func(t *testing.T) {
		p, err := parseAOT(t, []byte("package example\nimport \"example.test/other\"\n//gosx:island\nfunc Counter() Node {\n return <div>{1}</div>\n}\n"))
		if err != nil {
			t.Fatal(err)
		}
		p.PackagePath = "example/components"
		if _, err = ir.LowerIslandAOT(p, 0); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("invalid referenced object", func(t *testing.T) {
		p := checkerProgram(t, "var Value = absent", `return <div>{Value}{props.Value}</div>`)
		_, err := ir.LowerIslandAOT(p, 0)
		if err == nil || !strings.Contains(err.Error(), "type_error") {
			t.Fatal(err)
		}
	})
}

func parseAOT(t *testing.T, source []byte) (*ir.Program, error) {
	t.Helper()
	p, err := parse(t, source)
	if err == nil {
		p.Dir = t.TempDir()
	}
	return p, err
}
