package ui

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/components"
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

func TestButtonDisabledLinkFallback(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		direct := Button(ButtonProps{Href: `/docs?a=1&b="two"`, Disabled: disabled, Variant: "secondary", Size: "sm"}, gosx.Text("Docs & more"))
		registered, ok := Registry().Render("Button", components.Props{"href": `/docs?a=1&b="two"`, "disabled": disabled, "variant": "secondary", "size": "sm"}, gosx.Text("Docs & more"))
		if !ok {
			t.Fatal("registry missing")
		}
		for _, node := range []gosx.Node{direct, registered} {
			h := gosx.RenderHTML(node)
			if !strings.Contains(h, `class="gosx-ui gosx-ui-button gosx-ui-button-secondary gosx-ui-button-sm"`) || !strings.Contains(h, "Docs &amp; more") {
				t.Fatal(h)
			}
			if disabled {
				if !strings.HasPrefix(h, "<span ") || !strings.Contains(h, `aria-disabled="true"`) || strings.Contains(h, "href=") || strings.Contains(h, "tabindex=") {
					t.Fatalf("disabled live link: %s", h)
				}
			} else if !strings.HasPrefix(h, "<a ") || !strings.Contains(h, `href="/docs?a=1&amp;b=&#34;two&#34;"`) {
				t.Fatal(h)
			}
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
		`<p class="gosx-ui-field-help" id="email-help">Used for billing.</p>`,
		`<p class="gosx-ui-field-error" role="alert" id="email-error">Required.</p>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in %q", want, html)
		}
	}
}

func TestFieldInputWiresAccessibleDescriptions(t *testing.T) {
	node := FieldInput(FieldProps{
		ID:       "email",
		Label:    "Email",
		Help:     "Used for billing.",
		Error:    "Required.",
		Required: true,
	}, InputProps{Type: "email", DescribedBy: "external-note"})
	html := gosx.RenderHTML(node)
	for _, want := range []string{
		`<input class="gosx-ui gosx-ui-input" id="email" name="email" aria-describedby="external-note email-help email-error" aria-invalid="true" required type="email" />`,
		`<p class="gosx-ui-field-help" id="email-help">Used for billing.</p>`,
		`<p class="gosx-ui-field-error" role="alert" id="email-error">Required.</p>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in %q", want, html)
		}
	}
}

func TestTabsExposeStaticNativeSemantics(t *testing.T) {
	node := Tabs(TabsProps{
		Active: "settings",
		Items: []TabItem{
			{ID: "overview", Label: "Overview", Href: "/overview"},
			{ID: "settings", Label: "Settings"},
			{ID: "disabled", Label: "Disabled", Disabled: true},
		},
	})
	html := gosx.RenderHTML(node)
	for _, want := range []string{
		`href="/overview">Overview</a>`,
		`class="gosx-ui-tab is-active" type="button">Settings</button>`,
		`aria-disabled="true" type="button" disabled`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in %q", want, html)
		}
	}
	for _, forbidden := range []string{`role="tab`, `aria-selected`, `aria-current`, `tabindex`} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("unexpected %s in %s", forbidden, html)
		}
	}
}

func TestTabsCurrentSelectionAndRegistryParity(t *testing.T) {
	for _, tc := range []struct {
		name, active string
		items        []TabItem
		current      string
	}{
		{"first-active", "", []TabItem{{ID: "a", Label: "A", Href: "/a", Active: true}, {ID: "b", Label: "B", Href: "/b", Active: true}}, "/a"},
		{"explicit", "b", []TabItem{{ID: "a", Href: "/a", Active: true}, {ID: "b", Href: "/b"}}, "/b"},
		{"missing-no-fallback", "missing", []TabItem{{ID: "a", Href: "/a", Active: true}}, ""},
		{"disabled-no-fallback", "a", []TabItem{{ID: "a", Href: "/a", Disabled: true}, {ID: "a", Href: "/duplicate", Active: true}}, ""},
		{"button-no-fallback", "a", []TabItem{{ID: "a"}, {ID: "a", Href: "/duplicate", Active: true}}, ""},
		{"duplicate-first", "a", []TabItem{{ID: "a", Href: "/first"}, {ID: "a", Href: "/second"}}, "/first"},
		{"implicit-skip-ineligible", "", []TabItem{{Active: true}, {Href: "/disabled", Active: true, Disabled: true}, {Href: "/ok", Active: true}}, "/ok"},
		{"empty", "", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			props := TabsProps{Active: tc.active, Items: tc.items}
			html := gosx.RenderHTML(Tabs(props))
			count := strings.Count(html, `aria-current="page"`)
			if tc.current == "" && count != 0 || tc.current != "" && (count != 1 || !strings.Contains(html, `aria-current="page" href="`+tc.current+`"`)) {
				t.Fatalf("current %q: %s", tc.current, html)
			}
			node, ok := Registry().Render("Tabs", components.Props{"active": tc.active, "items": tc.items})
			if !ok || gosx.RenderHTML(node) != html {
				t.Fatal("registry differs")
			}
			for _, item := range tc.items {
				if item.ID != "" && strings.Contains(html, ` id="`+item.ID+`"`) {
					t.Fatal("logical ID leaked into DOM")
				}
			}
		})
	}
}

func TestTabsMapPropsEscapingAndDisabledLink(t *testing.T) {
	node, ok := Registry().Render("Tabs", components.Props{"items": []map[string]any{{"id": "logical", "label": "<unsafe>", "href": "/a?x=1&y=2", "active": true}, {"label": "Disabled", "href": "/blocked", "disabled": true}}})
	if !ok {
		t.Fatal("registry missing")
	}
	html := gosx.RenderHTML(node)
	for _, want := range []string{`href="/a?x=1&amp;y=2"`, `&lt;unsafe&gt;`, `<span class="gosx-ui-tab" aria-disabled="true">Disabled</span>`} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %s in %s", want, html)
		}
	}
	if strings.Contains(html, "/blocked") {
		t.Fatal("disabled target retained")
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
	for _, name := range []string{"Box", "Stack", "Heading", "Button", "Card", "Field", "FieldInput", "Select", "Tabs", "Table", "Styles"} {
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
