package components

import (
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
)

func TestRegistryRendersRegisteredComponent(t *testing.T) {
	registry := NewRegistry()
	props := Props{"tone": "primary"}
	if err := registry.RegisterFunc("Button", func(props Props, children ...gosx.Node) gosx.Node {
		props["tone"] = "mutated"
		return gosx.El("button",
			gosx.Attrs(gosx.Attr("data-tone", props["tone"])),
			gosx.Fragment(children...),
		)
	}, Metadata{Package: "core", Styles: []string{"/ui/button.css"}}); err != nil {
		t.Fatal(err)
	}

	node, ok := registry.Render("Button", props, gosx.Text("Save"))
	if !ok {
		t.Fatal("expected registered component to render")
	}
	if props["tone"] != "primary" {
		t.Fatalf("expected render props to be cloned, got %#v", props)
	}
	html := gosx.RenderHTML(node)
	if !strings.Contains(html, `<button data-tone="mutated">Save</button>`) {
		t.Fatalf("unexpected component HTML %q", html)
	}
	def, ok := registry.Lookup("Button")
	if !ok || len(def.Styles) != 1 || def.Styles[0] != "/ui/button.css" {
		t.Fatalf("unexpected component metadata %#v", def)
	}
}

func TestRegistryImportsLibraryUnderNamespace(t *testing.T) {
	base := NewRegistry()
	if err := base.RegisterFunc("Card", func(Props, ...gosx.Node) gosx.Node {
		return gosx.El("section")
	}, Metadata{}); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.RegisterLibrary("admin", base); err != nil {
		t.Fatal(err)
	}

	if _, ok := registry.Lookup("admin.Card"); !ok {
		t.Fatal("expected namespaced component")
	}
	if err := registry.RegisterLibrary("admin", base); err == nil {
		t.Fatal("expected duplicate namespaced component registration to fail")
	}
}

func TestRegistryBindingsAdaptToFileRouteComponents(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterFunc("Panel", func(props Props, children ...gosx.Node) gosx.Node {
		return gosx.El("section",
			gosx.Attrs(gosx.Attr("data-title", props["title"])),
			gosx.Fragment(children...),
		)
	}, Metadata{}); err != nil {
		t.Fatal(err)
	}

	bindings := registry.Bindings()
	component, ok := bindings["Panel"].(func(map[string]any) gosx.Node)
	if !ok {
		t.Fatalf("unexpected binding type %#v", bindings["Panel"])
	}
	html := gosx.RenderHTML(component(map[string]any{
		"title":    "Docs",
		"children": gosx.Text("Body"),
	}))
	if !strings.Contains(html, `<section data-title="Docs">Body</section>`) {
		t.Fatalf("unexpected bound component HTML %q", html)
	}
}
