package vm

import (
	"m31labs.dev/gosx/island/program"
	"testing"
)

func TestIslandDOMUpdatesKeepURLAndNameFiltering(t *testing.T) {
	machine := NewVM(progFromExprs([]program.Expr{{Op: program.OpLitString, Value: "javascript:example", Type: program.TypeString}}), nil)
	attrs := []program.Attr{{Kind: program.AttrStatic, Name: "x y", Value: "value"}, {Kind: program.AttrStatic, Name: "onclick", Value: "example"}, {Kind: program.AttrStatic, Name: "style", Value: "color:red"}, {Kind: program.AttrEvent, Name: "onClick", Event: "handle"}}
	for _, name := range []string{"href", "src", "action", "formaction", "xlink:href", "poster"} {
		attrs = append(attrs, program.Attr{Kind: program.AttrStatic, Name: name, Value: "java\nscript:example"})
	}
	resolved, dom, events, _, _ := machine.resolveElementAttrs(attrs)
	if len(resolved) != 7 || len(events) != 1 || len(dom) != 9 {
		t.Fatalf("resolved=%#v DOM=%#v events=%#v", resolved, dom, events)
	}
	for _, attr := range resolved {
		if attr.Name != "style" && attr.Value != "#" {
			t.Fatalf("unsafe DOM URL: %#v", attr)
		}
	}
	dynamic, _, _, _, _ := machine.resolveElementAttrs([]program.Attr{{Kind: program.AttrExpr, Name: "href", Expr: 0}})
	if len(dynamic) != 1 || dynamic[0].Value != "#" {
		t.Fatalf("unsafe bound URL: %#v", dynamic)
	}
	before := &ResolvedTree{Nodes: []ResolvedNode{{Tag: "a", Attrs: []ResolvedAttr{{Name: "href", Value: "/safe"}}}}}
	after := &ResolvedTree{Nodes: []ResolvedNode{{Tag: "a", Attrs: resolved, DOMAttrs: dom, Events: events}}}
	for _, op := range ReconcileTrees(before, after, nil) {
		if op.Kind == PatchSetAttr && op.AttrName == "href" && op.Text != "#" {
			t.Fatalf("unsafe URL in browser patch: %#v", op)
		}
	}
}
