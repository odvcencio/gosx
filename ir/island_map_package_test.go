//go:build !tinygo && !js

package ir_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/island/program"
)

func TestIslandMapIndexResolvesPackageTypes(t *testing.T) {
	for _, tc := range []struct {
		name, declaration, sibling string
		wantType                   program.ExprType
		marked                     bool
	}{
		{"builtin", "type Props struct { M map[string]bool }", "", program.TypeBool, true},
		{"shadowed bool", "type Props struct { M map[string]bool }", "type bool string", program.TypeString, true},
		{"sibling gsx", "type Props struct { M map[string]bool }", "type bool string", program.TypeString, true},
		{"sibling props and element", "", "type Flag = bool\ntype Props struct { M map[string]Flag }", program.TypeBool, true},
		{"sibling named map", "", "type Flags map[string]string\ntype Props struct { M Flags }", program.TypeString, true},
		{"shadowed integer", "type Props struct { M map[string]bool }", "type bool int", program.TypeAny, false},
		{"excluded sibling", "type Props struct { M map[string]bool }", "//go:build gosx_never_enabled\n\npackage app\ntype bool string", program.TypeBool, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			source := "package app\n" + tc.declaration + "\n//gosx:island\nfunc Sample(props Props) Node {\n return <div>{props.M[\"missing\"]}</div>\n}\n"
			if err := os.WriteFile(filepath.Join(dir, "page.gsx"), []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			if tc.sibling != "" {
				sibling := "package app\n" + tc.sibling + "\n"
				if tc.name == "excluded sibling" {
					sibling = tc.sibling + "\n"
				}
				name := "types.go"
				if tc.name == "sibling gsx" {
					name = "types.gsx"
				}
				if err := os.WriteFile(filepath.Join(dir, name), []byte(sibling), 0600); err != nil {
					t.Fatal(err)
				}
			}
			p, err := gosx.Compile([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			p.Dir, p.PackagePath = dir, "example.test/app"
			got, err := ir.LowerIsland(p, 0)
			if err != nil {
				t.Fatal(err)
			}
			var found bool
			for _, e := range got.Exprs {
				if e.Op == program.OpIndex && e.Value == program.IndexTypedMap {
					found = true
					if e.Type != tc.wantType {
						t.Fatalf("map type = %v, want %v", e.Type, tc.wantType)
					}
				}
			}
			if found != tc.marked {
				t.Fatalf("marked=%v, want %v", found, tc.marked)
			}
			want := "<div>0</div>"
			if tc.marked && tc.wantType == program.TypeBool {
				want = "<div>false</div>"
			} else if tc.marked && tc.wantType == program.TypeString {
				want = "<div></div>"
			}
			for _, props := range []string{`{"M":{}}`, `{"M":null}`} {
				gotHTML := island.RenderResolvedHTML(got, vm.ResolveInitialTree(got, props))
				if gotHTML != want {
					t.Fatalf("props %s: rendered %q, want %q", props, gotHTML, want)
				}
			}
		})
	}
}

func TestIslandMapPackageEvidenceTracksSiblingEditsAndFailsClosed(t *testing.T) {
	dir := t.TempDir()
	source := []byte("package app\ntype Props struct { M map[string]bool }\n//gosx:island\nfunc Sample(props Props) Node {\nreturn <div>{props.M[\"missing\"]}</div>\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "page.gsx"), source, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := gosx.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	p.Dir, p.PackagePath = dir, "example.test/app"
	sibling := filepath.Join(dir, "types.go")
	for _, tc := range []struct {
		body string
		want program.ExprType
	}{{"package app\n", program.TypeBool}, {"package app\ntype bool string\n", program.TypeString}} {
		if err := os.WriteFile(sibling, []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := ir.LowerIsland(p, 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, expr := range got.Exprs {
			if expr.Value == program.IndexTypedMap {
				found = true
				if expr.Type != tc.want {
					t.Fatalf("after sibling edit type=%v, want %v", expr.Type, tc.want)
				}
			}
		}
		if !found {
			t.Fatal("missing typed index")
		}
	}
	if err := os.WriteFile(sibling, []byte("package app\ntype broken {\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ir.LowerIsland(p, 0); err == nil || !strings.Contains(err.Error(), "resolve island Sample map prop types") {
		t.Fatalf("unreadable package type evidence error = %v", err)
	}
}

func TestIslandMapPackageRejectsUnresolvedImportedElement(t *testing.T) {
	dir := t.TempDir()
	source := []byte("package app\nimport other \"example.invalid/other\"\ntype Props struct { M map[string]other.Flag }\n//gosx:island\nfunc Sample(props Props) Node {\nreturn <div>{props.M[\"missing\"]}</div>\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "page.gsx"), source, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := gosx.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	p.Dir, p.PackagePath = dir, "example.test/app"
	if _, err := ir.LowerIsland(p, 0); err == nil || !strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("unresolved map element error = %v", err)
	}
}

func TestIslandMapPackageRejectsUnsupportedResolvedElement(t *testing.T) {
	dir := t.TempDir()
	source := []byte("package app\ntype Props struct { M map[string]bool }\n//gosx:island\nfunc Sample(props Props) Node {\nreturn <div>{props.M[\"missing\"]}</div>\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "page.gsx"), source, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "types.go"), []byte("package app\ntype bool struct { X int }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := gosx.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	p.Dir, p.PackagePath = dir, "example.test/app"
	if _, err := ir.LowerIsland(p, 0); err == nil || !strings.Contains(err.Error(), "unsupported map key or element type") {
		t.Fatalf("unsupported map element error = %v", err)
	}
}
