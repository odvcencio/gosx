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
)

func TestIslandMapIndexResolvesImportedTypes(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "app")
	for name, body := range map[string]string{
		"go.mod":         "module example.test/map-import\n\ngo 1.26\n",
		"model/types.go": "package model\ntype Flag bool\ntype Labels map[string]string\n",
		"app/page.gsx":   "package app\nimport model \"example.test/map-import/model\"\ntype Props struct { Flags map[string]model.Flag; Labels model.Labels }\n//gosx:island\nfunc Sample(props Props) Node {\nreturn <div>{props.Flags[\"missing\"]}{props.Labels[\"missing\"]}</div>\n}\n",
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	source, err := os.ReadFile(filepath.Join(app, "page.gsx"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := gosx.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	p.Dir, p.PackagePath = app, "example.test/map-import/app"
	lowered, err := ir.LowerIsland(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, props := range []string{`{"Flags":{},"Labels":{}}`, `{"Flags":null,"Labels":null}`} {
		got := island.RenderResolvedHTML(lowered, vm.ResolveInitialTree(lowered, props))
		if got != "<div>false</div>" {
			t.Fatalf("props %s rendered %q, want false", props, got)
		}
	}
	// A changed dependency must invalidate checked evidence even when the
	// consuming GSX source and directory are unchanged.
	if err := os.WriteFile(filepath.Join(dir, "model", "types.go"), []byte("package model\ntype Flag string\ntype Labels map[string]string\n"), 0644); err != nil {
		t.Fatal(err)
	}
	lowered, err = ir.LowerIsland(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := island.RenderResolvedHTML(lowered, vm.ResolveInitialTree(lowered, `{"Flags":{},"Labels":{}}`)); got != "<div></div>" {
		t.Fatalf("dependency edit rendered %q, want empty string", got)
	}
	custom := "package model\ntype Flag bool\nfunc (Flag) MarshalJSON() ([]byte, error) { return []byte(\"true\"), nil }\ntype Labels map[string]string\n"
	if err := os.WriteFile(filepath.Join(dir, "model", "types.go"), []byte(custom), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ir.LowerIsland(p, 0); err == nil || !strings.Contains(err.Error(), "unsupported map key or element type") {
		t.Fatalf("custom transport must fail closed: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "model", "types.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := ir.LowerIsland(p, 0); err == nil || !strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("missing dependency must fail closed: %v", err)
	}
}

func TestIslandMapIndexRejectsCustomPropsAndKeys(t *testing.T) {
	for _, tc := range []struct {
		name, declarations, want string
	}{
		{"props", "type Props struct { M map[string]bool }\nfunc (Props) MarshalJSON() ([]byte, error) { return nil, nil }", "custom JSON/text encoder"},
		{"key", "type Key string\nfunc (Key) MarshalText() ([]byte, error) { return nil, nil }\ntype Props struct { M map[Key]bool }", "unsupported map key or element type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "package app\n" + tc.declarations + "\n//gosx:island\nfunc Sample(props Props) Node {\nreturn <div>{props.M[\"missing\"]}</div>\n}\n"
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "page.gsx"), []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			p, err := gosx.Compile([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			p.Dir, p.PackagePath = dir, "example.test/app"
			if _, err := ir.LowerIsland(p, 0); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("custom encoder must fail closed with %q: %v", tc.want, err)
			}
		})
	}
}

func TestIslandMapIndexResolvesReplacedModuleFromSibling(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod":       "module example.test/app\n\ngo 1.26\n\nrequire example.test/dep v0.0.0\nreplace example.test/dep => ./dep\n",
		"dep/go.mod":   "module example.test/dep\n\ngo 1.26\n",
		"dep/types.go": "package dep\ntype Flag = bool\ntype Key string\ntype Flags map[Key]Flag\n",
		"app/props.go": "package app\nimport dep \"example.test/dep\"\ntype Props struct { M dep.Flags }\n",
		"app/page.gsx": "package app\n//gosx:island\nfunc Sample(props Props) Node {\nreturn <div>{props.M[\"missing\"]}</div>\n}\n",
	}
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	source := []byte(files["app/page.gsx"])
	p, err := gosx.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	p.Dir, p.PackagePath = filepath.Join(root, "app"), "example.test/app/app"
	lowered, err := ir.LowerIsland(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := island.RenderResolvedHTML(lowered, vm.ResolveInitialTree(lowered, `{"M":{}}`)); got != "<div>false</div>" {
		t.Fatalf("imported alias rendered %q", got)
	}
	if got, err := os.ReadFile(filepath.Join(root, "go.mod")); err != nil || string(got) != files["go.mod"] {
		t.Fatalf("module metadata was changed: %v", err)
	}
}
