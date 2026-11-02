package evalparity

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/transpile"
)

// Exercise a real sibling declaration through native generated Go, the route
// interpreter, and the browser VM's evaluator on the same authored input.
func TestSiblingMapTypeParity(t *testing.T) {
	shadow := Case{
		ID:           "sibling_shadow",
		Expr:         `props.M["missing"]`,
		PropsFields:  "M map[string]bool",
		PropsLiteral: `M: map[string]bool{"k": "present"}`,
		PropsValue:   map[string]any{"M": map[string]string{"k": "present"}},
	}
	sibling := Case{
		ID:           "sibling_props",
		Expr:         `props.M["missing"]`,
		PropsLiteral: `M: map[string]Flag{"k": true}`,
		PropsValue:   map[string]any{"M": map[string]bool{"k": true}},
	}
	siblingSource := []byte("package app\nimport gosx \"m31labs.dev/gosx\"\n//gosx:island\nfunc " + componentName(sibling) + "(props " + propsTypeName(sibling) + ") gosx.Node {\nreturn <div>{props.M[\"missing\"]}</div>\n}\n")
	imported := Case{ID: "imported_map", Expr: `props.M["missing"]`, PropsLiteral: "M: nil", PropsValue: map[string]any{"M": map[string]bool(nil)}}
	importedSource := []byte("package app\nimport (gosx \"m31labs.dev/gosx\"; model \"example.test/sibling-map/model\")\ntype " + propsTypeName(imported) + " struct { M map[string]model.Flag }\n//gosx:island\nfunc " + componentName(imported) + "(props " + propsTypeName(imported) + ") gosx.Node {\nreturn <div>{props.M[\"missing\"]}</div>\n}\n")
	for _, tc := range []struct {
		name, declarations, want string
		c                        Case
		source                   []byte
		extra                    map[string]string
	}{
		{"shadow", "type bool string", "", shadow, gsxSource(shadow), nil},
		{"sibling props", "type Flag = bool\ntype " + propsTypeName(sibling) + " struct { M map[string]Flag }", "false", sibling, siblingSource, nil},
		{"imported element", "", "false", imported, importedSource, map[string]string{"model/types.go": "package model\ntype Flag bool\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertSiblingMapParity(t, tc.c, tc.source, tc.declarations, tc.want, tc.extra)
		})
	}
}

func assertSiblingMapParity(t *testing.T, c Case, source []byte, declarations, want string, extra map[string]string) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":       fmt.Sprintf("module example.test/sibling-map\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\nreplace m31labs.dev/gosx => %s\n", filepath.ToSlash(gosxModuleRoot())),
		"page.gsx":     string(source),
		"types.go":     "package app\n" + declarations + "\n",
		"page_test.go": "package app\nimport (\"testing\"; gosx \"m31labs.dev/gosx\")\nfunc TestNative(t *testing.T) { if got := gosx.RenderHTML(" + componentName(c) + "(" + propsTypeName(c) + "{" + c.PropsLiteral + "})); got != " + fmt.Sprintf("%q", "<div>"+want+"</div>") + " { t.Fatalf(\"native: %q\", got) } }\n",
	}
	for name, body := range extra {
		files[name] = body
	}
	generated, err := transpile.Transpile(source, transpile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	compiled, err := gosx.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	index := -1
	for i, component := range compiled.Components {
		if component.Name == componentName(c) {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("compiled component missing")
	}
	compiled.Dir, compiled.PackagePath = dir, "example.test/sibling-map"
	for name, run := range map[string]func() (string, error){
		"route": func() (string, error) { return runRoute(compiled, c) },
		"vm":    func() (string, error) { return runVM(compiled, index, c) },
	} {
		value, err := run()
		if err != nil || value != want {
			t.Fatalf("%s: value %q, error %v; want %q", name, value, err, want)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "page.go"), []byte(generated), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-mod=mod", "-run", "^TestNative$", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Go: %v\n%s", err, strings.TrimSpace(string(output)))
	}
}
