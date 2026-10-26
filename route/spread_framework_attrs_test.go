package route

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestScene3DSpreadMountAttrsRejectFrameworkAndStyle(t *testing.T) {
	attrs := map[string]any{
		"data-gosx-action": "POST /forged", "data-gosx-bind-source": "#forged",
		"data-GOSX-bind-attr": "forged:data-code", "data-gosx-pixel-injected": "forged",
		"style": "color:red", "className": "safe-spread", "role": "img",
		"title": "safe-title", "aria-label": "safe-label", "data-app-id": "safe-id",
	}
	for _, tc := range []struct {
		name, markup string
		authored     bool
	}{
		{"spread", `<Scene3D {...data.attrs} />`, false},
		{"authored", `<Scene3D {...data.attrs} style="color:green" data-gosx-action="GET /trusted" />`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "page.gsx")
			source := "package main\nfunc Page() Node {\nreturn " + tc.markup + "\n}\n"
			if err := os.WriteFile(path, []byte(source), 0644); err != nil {
				t.Fatal(err)
			}
			node, err := DefaultFileRenderer(&RouteContext{Data: map[string]any{"attrs": attrs}}, FilePage{FilePath: path, Pattern: "/"})
			if err != nil {
				t.Fatal(err)
			}
			got := gosx.RenderHTML(node)
			for _, bad := range []string{"forged", "color:red"} {
				if strings.Contains(got, bad) {
					t.Fatalf("spread installed unsafe mount attributes: %s", got)
				}
			}
			for _, want := range []string{"safe-spread", `role="img"`, `title="safe-title"`, `aria-label="safe-label"`, `data-app-id="safe-id"`} {
				if !strings.Contains(got, want) {
					t.Fatalf("safe mount attribute %q lost: %s", want, got)
				}
			}
			if tc.authored && (!strings.Contains(got, "color:green") || !strings.Contains(got, `data-gosx-action="GET /trusted"`)) {
				t.Fatalf("authored mount attributes lost: %s", got)
			}
		})
	}
}

func TestFallbackPreservesAuthoredFrameworkAttrs(t *testing.T) {
	for _, markup := range []string{
		`<Unknown {...attrs} data-gosx-action="POST /trusted" data-gosx-bind-source={source} data-gosx-bind-attr="title:data-label" data-gosx-link style="color:green" data-gosx-on-click="forged-dispatch" />`,
		`<Unknown data-gosx-action="POST /trusted" data-gosx-bind-source={source} data-gosx-bind-attr="title:data-label" data-gosx-link style="color:green" data-gosx-on-click="forged-dispatch" {...attrs} />`,
	} {
		prog, err := gosx.Compile([]byte("package main\nfunc Page() Node {\nreturn " + markup + "\n}\n"))
		if err != nil {
			t.Fatal(err)
		}
		got, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Values: map[string]any{
			"source": "#trusted",
			"attrs":  map[string]any{"data-gosx-action": "POST /forged", "data-gosx-bind-source": "#forged", "data-gosx-bind-attr": "forged:data-code", "data-gosx-link": false, "style": "color:red", "data-app-id": "safe"},
		}})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`data-gosx-action="POST /trusted"`, `data-gosx-bind-source="#trusted"`, `data-gosx-bind-attr="title:data-label"`, ` data-gosx-link`, `style="color:green"`, `data-app-id="safe"`} {
			if !strings.Contains(got, want) {
				t.Fatalf("authored attribute %q lost: %s", want, got)
			}
		}
		if strings.Contains(got, "forged") || strings.Contains(got, "color:red") {
			t.Fatalf("unsafe spread or dispatch attribute rendered: %s", got)
		}
	}
}
