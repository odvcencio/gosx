package island

import (
	"m31labs.dev/gosx/client/vm"
	"strings"
	"testing"
)

func TestResolvedHTMLFiltersURLsAndNames(t *testing.T) {
	attrs := []vm.ResolvedAttr{{Name: "x y", Value: "value"}, {Name: "OnClick", Value: "example"}, {Name: "style", Value: "color:red"}}
	for _, name := range []string{"href", "src", "action", "formaction", "xlink:href", "poster"} {
		attrs = append(attrs, vm.ResolvedAttr{Name: name, Value: "java\nscript:example"})
	}
	tree := &vm.ResolvedTree{Nodes: []vm.ResolvedNode{{Tag: "div", Attrs: attrs, Events: []vm.ResolvedEvent{{Name: "onClick", Handler: "handle"}}}}}
	got := RenderResolvedHTML(nil, tree)
	if strings.Contains(got, "javascript") || strings.Contains(got, "OnClick") || strings.Contains(got, "x y") || !strings.Contains(got, `style="color:red"`) || !strings.Contains(got, "data-gosx-on-click") {
		t.Fatalf("unsafe rendered attributes: %s", got)
	}
	for _, name := range []string{"href", "src", "action", "formaction", "xlink:href", "poster"} {
		if !strings.Contains(got, name+`="#"`) {
			t.Fatalf("URL not filtered: %s", got)
		}
	}
	tree.Nodes[0].Tag = "img src=x"
	if RenderResolvedHTML(nil, tree) != "" {
		t.Fatal("invalid tag rendered")
	}
}

func TestAuthoredReservedAttrsCannotShadowMarkers(t *testing.T) {
	attrs := []vm.ResolvedAttr{
		{Name: "data-gosx-on-click", Value: "evil"},
		{Name: "Data-Gosx-Handler", Value: "evil"},
		{Name: "data-gosx-path", Value: "9.9"},
		{Name: "data-gosx-on-input", Value: "evil"},
		{Name: "data-id", Value: "7"},
	}
	tree := &vm.ResolvedTree{Nodes: []vm.ResolvedNode{{Tag: "button", Attrs: attrs, Events: []vm.ResolvedEvent{{Name: "onClick", Handler: "real"}}}}}
	got := RenderResolvedHTML(nil, tree)
	if strings.Contains(got, "evil") || strings.Contains(got, "9.9") {
		t.Fatalf("authored marker leaked: %s", got)
	}
	if strings.Count(got, "data-gosx-on-click=") != 1 || !strings.Contains(got, `data-gosx-on-click="real"`) || !strings.Contains(got, `data-gosx-handler="real"`) {
		t.Fatalf("synthesized markers missing: %s", got)
	}
	if !strings.Contains(got, `data-id="7"`) {
		t.Fatalf("ordinary data attribute dropped: %s", got)
	}
}

func TestAuthoredReservedAttrsDroppedWithoutEvents(t *testing.T) {
	attrs := []vm.ResolvedAttr{{Name: "data-gosx-on-click", Value: "evil"}, {Name: "data-gosx-path", Value: "9.9"}, {Name: "data-id", Value: "7"}}
	tree := &vm.ResolvedTree{Nodes: []vm.ResolvedNode{{Tag: "div", Attrs: attrs}}}
	got := RenderResolvedHTML(nil, tree)
	if strings.Contains(got, "evil") || strings.Contains(got, "9.9") || !strings.Contains(got, `data-id="7"`) {
		t.Fatalf("unexpected output: %s", got)
	}
}

func TestRenderResolvedAttrsFastPathNoAllocWhenClean(t *testing.T) {
	node := &vm.ResolvedNode{Tag: "div", Attrs: []vm.ResolvedAttr{{Name: "class", Value: "a"}, {Name: "data-id", Value: "1"}}}
	if n := testing.AllocsPerRun(100, func() { _ = renderResolvedAttrs(node, "0") }); n != 0 {
		t.Fatalf("fast path allocated %v times", n)
	}
}
