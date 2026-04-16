package route_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/route"
)

func TestComposableLoweringLiteralVarRefs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.gsx")
	source := `package docs

func Page() Node {
	return <Scene3D class="shell">
		<Environment fogColor="var(--galaxy-fog-color)" fogDensity="var(--galaxy-fog-density)" />
		<Material name="core" color="var(--galaxy-core-inner)" />
	</Scene3D>
}
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := &route.RouteContext{}
	node, err := route.DefaultFileRenderer(ctx, route.FilePage{FilePath: path, Pattern: "/"})
	if err != nil {
		t.Fatal(err)
	}
	full := gosx.RenderHTML(ctx.Runtime().Head()) + gosx.RenderHTML(node)
	for _, want := range []string{
		`"var(--galaxy-fog-color)"`,
		`"var(--galaxy-fog-density)"`,
		`"var(--galaxy-core-inner)"`,
	} {
		if !strings.Contains(full, want) {
			t.Errorf("LITERAL: missing %s", want)
		}
	}
}

func TestComposableLoweringSpreadDataVarRefs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.gsx")
	source := `package docs

func Page() Node {
	return <Scene3D class="shell">
		<Environment {...data.env} />
		<Material {...data.material} />
	</Scene3D>
}
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := &route.RouteContext{
		Data: map[string]any{
			"env": map[string]any{
				"fogColor":   "var(--galaxy-fog-color)",
				"fogDensity": "var(--galaxy-fog-density)",
			},
			"material": map[string]any{
				"name":  "core",
				"color": "var(--galaxy-core-inner)",
			},
		},
	}
	node, err := route.DefaultFileRenderer(ctx, route.FilePage{FilePath: path, Pattern: "/"})
	if err != nil {
		t.Fatal(err)
	}
	full := gosx.RenderHTML(ctx.Runtime().Head()) + gosx.RenderHTML(node)
	for _, want := range []string{
		`"var(--galaxy-fog-color)"`,
		`"var(--galaxy-fog-density)"`,
		`"var(--galaxy-core-inner)"`,
	} {
		if !strings.Contains(full, want) {
			t.Errorf("SPREAD: missing %s", want)
		}
	}
	// Also dump the scene section for diagnostics
	idx := strings.Index(full, `"scene"`)
	if idx >= 0 {
		tail := full[idx:]
		end := strings.Index(tail, `}},`)
		if end < 0 || end > 600 {
			end = 600
			if end > len(tail) {
				end = len(tail)
			}
		}
		t.Logf("scene excerpt: %s", tail[:end])
	}
}
