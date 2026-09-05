package route

import (
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedLinkFileContractParity(t *testing.T) {
	for _, target := range []string{"/docs?tab=one", "/docs?tab=two", "/docs/child", "/else"} {
		for _, extra := range [][]ir.Attr{nil, {{Kind: ir.AttrStatic, Name: "prefetch", Value: "intent"}}, {{Kind: ir.AttrStatic, Name: "aria-current", Value: "step"}, {Kind: ir.AttrStatic, Name: "current", Value: "page"}}} {
			env := fileRenderEnv{values: map[string]any{"request": map[string]any{"target": target}}}
			node := ir.Node{Tag: "Link", Attrs: append([]ir.Attr{{Kind: ir.AttrStatic, Name: "class", Value: "nav"}, {Kind: ir.AttrStatic, Name: "href", Value: "/docs?tab=one"}}, extra...)}
			renderer := fileProgramRenderer{}
			var old, now strings.Builder
			old.WriteString("<a")
			renderer.renderLinkAttrs(&old, node.Attrs, env)
			renderer.writeManagedLinkContract(&old, node.Attrs, env, fileManagedLinkContractForAttrs(node.Attrs, env))
			old.WriteString("></a>")
			renderer.writeLink(&now, &node, env)
			if now.String() != old.String() {
				t.Fatalf("%s: got %s want %s", target, now.String(), old.String())
			}
		}
	}
}

func TestManagedLinkRouteNodeBoundaries(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "layout.gsx", "package app\ncomponent Layout() {\n return <main>{children}</main>\n}\n")
	node := gosx.ManagedLink("/docs?tab=one", nil, gosx.Text("<child>"))
	for _, target := range []string{"/docs?tab=one", "/docs?tab=two"} {
		ctx := &RouteContext{Request: httptest.NewRequest("GET", target, nil)}
		path := filepath.Join(root, "layout.gsx")
		got, err := renderFileLayout(path, ctx, node, layoutFilePage("", path), FileModule{}, FileLayoutOptions{})
		if err != nil {
			t.Fatal(err)
		}
		want := gosx.RenderHTMLWithContext(node, gosx.RenderContext{RequestTarget: target})
		if gosx.RenderHTML(got) != "<main>"+want+"</main>" {
			t.Fatal(gosx.RenderHTML(got))
		}
		env := fileRenderEnv{values: map[string]any{"request": map[string]any{"target": target}}}
		for _, value := range []any{node, &node, []gosx.Node{node}} {
			if html := renderFileEvaluatedExprWithEnv(value, env); html != want {
				t.Fatal(html)
			}
		}
		if html, ok := renderBoundComponentValueWithEnv(func() gosx.Node { return node }, [][]any{{}}, env); !ok || html != want {
			t.Fatal(html, ok)
		}
	}
}

func TestManagedLinkFileDirectSpreadAndNilParity(t *testing.T) {
	env := fileRenderEnv{values: map[string]any{"request": map[string]any{"target": "/docs/child"}, "spread": map[string]any{"href": "/wrong", "current": "none", "prefetch": "off", "title": nil}}}
	attrs := []ir.Attr{{Kind: ir.AttrSpread, Expr: "spread"}, {Kind: ir.AttrStatic, Name: "href", Value: "/docs"}, {Kind: ir.AttrStatic, Name: "current", Value: "auto"}, {Kind: ir.AttrStatic, Name: "prefetch", Value: "intent"}, {Kind: ir.AttrExpr, Name: "id", Expr: "missing"}}
	renderer := fileProgramRenderer{}
	node := ir.Node{Tag: "Link", Attrs: attrs}
	var old, now strings.Builder
	old.WriteString("<a")
	renderer.renderLinkAttrs(&old, attrs, env)
	renderer.writeManagedLinkContract(&old, attrs, env, fileManagedLinkContractForAttrs(attrs, env))
	old.WriteString("></a>")
	renderer.writeLink(&now, &node, env)
	if now.String() != old.String() {
		t.Fatalf("got %s want %s", now.String(), old.String())
	}
	for _, want := range []string{`data-gosx-link-current="ancestor"`, `data-gosx-prefetch="intent"`} {
		if !strings.Contains(now.String(), want) {
			t.Fatal(now.String())
		}
	}
	if strings.Contains(now.String(), "&lt;nil&gt;") || strings.Contains(now.String(), " title=") || strings.Contains(now.String(), " id=") {
		t.Fatal(now.String())
	}
}
