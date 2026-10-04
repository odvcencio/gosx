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
