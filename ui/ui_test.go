package ui

import (
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/components"
)

func TestButtonRendersLinkVariant(t *testing.T) {
	node := Button(ButtonProps{Href: "/docs", Variant: "secondary", Size: "sm"}, gosx.Text("Docs"))
	html := gosx.RenderHTML(node)
	for _, want := range []string{
		`<a class="gosx-ui gosx-ui-button gosx-ui-button-secondary gosx-ui-button-sm"`,
		`href="/docs"`,
		`>Docs</a>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in %q", want, html)
		}
	}
}

func TestFieldComposesLabelControlAndMessages(t *testing.T) {
	node := Field(FieldProps{
		ID:       "email",
		Label:    "Email",
		Help:     "Used for billing.",
		Error:    "Required.",
		Required: true,
	}, Input(InputProps{ID: "email", Name: "email", Type: "email"}))
	html := gosx.RenderHTML(node)
	for _, want := range []string{
		`<label class="gosx-ui-field-label" for="email">Email<span aria-hidden="true"> *</span></label>`,
		`<input class="gosx-ui gosx-ui-input" id="email" name="email" type="email" />`,
		`<p class="gosx-ui-field-help">Used for billing.</p>`,
		`<p class="gosx-ui-field-error" role="alert">Required.</p>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in %q", want, html)
		}
	}
}

func TestStylesReturnsDefaultStylesheet(t *testing.T) {
	html := gosx.RenderHTML(Styles())
	if !strings.Contains(html, `<style data-gosx-ui>`) || !strings.Contains(html, `.gosx-ui-button`) {
		t.Fatalf("unexpected stylesheet %q", html)
	}
}

func TestRegistrySurfacesCorePrimitives(t *testing.T) {
	registry := Registry()
	for _, name := range []string{"Box", "Stack", "Heading", "Button", "Card", "Field", "Select", "Tabs", "Table", "Styles"} {
		if _, ok := registry.Lookup(name); !ok {
			t.Fatalf("expected %s in registry; got %#v", name, sortedDefinitionNames(registry))
		}
	}

	node, ok := registry.Render("Select", components.Props{
		"name": "status",
		"options": []any{
			map[string]any{"value": "draft", "label": "Draft"},
			"published",
		},
	})
	if !ok {
		t.Fatal("expected Select to render")
	}
	html := gosx.RenderHTML(node)
	for _, want := range []string{`name="status"`, `<option value="draft">Draft</option>`, `<option value="published">published</option>`} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in %q", want, html)
		}
	}
}

func TestRegistryBindingsAdaptToFileRouteComponents(t *testing.T) {
	bindings := Registry().Bindings()
	component, ok := bindings["Button"].(func(map[string]any) gosx.Node)
	if !ok {
		t.Fatalf("unexpected Button binding %#v", bindings["Button"])
	}
	html := gosx.RenderHTML(component(map[string]any{
		"variant":  "ghost",
		"children": gosx.Text("Open"),
	}))
	if !strings.Contains(html, `<button class="gosx-ui gosx-ui-button gosx-ui-button-ghost gosx-ui-button-md" type="button">Open</button>`) {
		t.Fatalf("unexpected bound button HTML %q", html)
	}
}
