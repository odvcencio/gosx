package gosx

import (
	"fmt"
	"strings"
	"testing"
)

type securityURL string

func (v securityURL) String() string { return string(v) }

func TestNodeRenderFiltersURLValuesAndNames(t *testing.T) {
	for _, name := range []string{"href", "src", "action", "formaction", "xlink:href", "poster"} {
		for _, value := range []any{"javascript:example", securityURL("javascript:example")} {
			got := RenderHTML(El("a", Attrs(Attr(name, value))))
			if !strings.Contains(got, fmt.Sprintf(`%s="#"`, name)) {
				t.Fatalf("unsafe URL: %s", got)
			}
		}
	}
	got := RenderHTML(El("div", Attrs(Attr("x y=z", "value")), Spread(map[string]any{"onload": "example", "OnClick": "example", "style": "color:red", "data-ok": "ok"})))
	if got != `<div data-ok="ok"></div>` {
		t.Fatalf("unsafe names: %s", got)
	}
	if RenderHTML(El("img src=x", Text("child"))) != "" {
		t.Fatal("invalid tag rendered")
	}
	if RenderAttrs(Attrs(BoolAttr("x y"))) != "" {
		t.Fatal("invalid boolean name rendered")
	}
	if !strings.Contains(RenderHTML(El("div", Attrs(Attr("style", "color:red")))), `style="color:red"`) {
		t.Fatal("authored style dropped")
	}
}

func TestNodeRenderDropsReservedDispatchAttrs(t *testing.T) {
	for _, name := range []string{"data-gosx-handler", "data-gosx-path", "data-gosx-on-click", "DATA-GOSX-ON-INPUT", "data-gosx-on-"} {
		for _, node := range []Node{
			El("button", Attrs(Attr(name, "evil"), Attr("data-app-id", "true"))),
			El("button", Spread(map[string]any{name: "evil", "data-app-id": "true"})),
		} {
			if got := RenderHTML(node); got != `<button data-app-id="true"></button>` {
				t.Fatalf("%s: %s", name, got)
			}
		}
	}
}

func TestNodeSpreadCannotInstallFrameworkBindings(t *testing.T) {
	attrs := map[string]any{"data-gosx-bind-source": "#source", "DATA-GOSX-BIND-ATTR": "onclick:data-code", "data-gosx-action": "POST /delete", "data-gosx-action-target": "#target", "data-app-id": "safe"}
	got := RenderHTML(El("div", Spread(attrs)))
	if got != `<div data-app-id="safe"></div>` {
		t.Fatalf("spread installed framework behavior: %s", got)
	}
	// Explicit framework attributes remain available to trusted authors.
	if got := RenderHTML(El("div", Attrs(Attr("data-gosx-bind-source", "#source")))); !strings.Contains(got, `data-gosx-bind-source="#source"`) {
		t.Fatalf("authored binding dropped: %s", got)
	}
}
