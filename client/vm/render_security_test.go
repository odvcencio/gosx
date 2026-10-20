package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
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

func TestIslandDOMUpdatesRejectReservedDispatchAttrs(t *testing.T) {
	machine := NewVM(progFromExprs([]program.Expr{{Op: program.OpLitString, Value: "evil", Type: program.TypeString}}), nil)
	var attrs []program.Attr
	for _, name := range []string{"data-gosx-handler", "data-gosx-path", "data-gosx-on-click", "DATA-GOSX-ON-INPUT", "data-gosx-on-"} {
		for _, kind := range []program.AttrKind{program.AttrStatic, program.AttrExpr, program.AttrBool} {
			attrs = append(attrs, program.Attr{Kind: kind, Name: name, Value: "evil", Expr: 0})
		}
	}
	attrs = append(attrs, program.Attr{Kind: program.AttrStatic, Name: "data-gosx-link", Value: "true"}, program.Attr{Kind: program.AttrEvent, Name: "onClick", Event: "real"})
	resolved, dom, events, _, _ := machine.resolveElementAttrs(attrs)
	if len(resolved) != 1 || len(dom) != 3 || len(events) != 1 {
		t.Fatalf("authored markers survived: resolved=%+v DOM=%+v events=%+v", resolved, dom, events)
	}
	for _, attr := range dom {
		if attr.Value == "evil" {
			t.Fatalf("authored marker leaked: %+v", attr)
		}
	}
}

func TestReconcileReservedAttrsUsesOnlyGeneratedDispatch(t *testing.T) {
	before := &ResolvedTree{Nodes: []ResolvedNode{{Tag: "button"}}}
	after := &ResolvedTree{Nodes: []ResolvedNode{{Tag: "button", Attrs: []ResolvedAttr{
		{Name: "data-gosx-handler", Value: "evil"}, {Name: "data-gosx-path", Value: "9"}, {Name: "DATA-GOSX-ON-INPUT", Value: "evil"},
	}, Events: []ResolvedEvent{{Name: "onClick", Handler: "real"}}}}}
	ops := ReconcileTrees(before, after, nil)
	if len(ops) != 2 {
		t.Fatalf("expected only generated click markers: %+v", ops)
	}
	for _, op := range ops {
		if op.Kind != PatchSetDispatch || op.Text != "real" {
			t.Fatalf("unsafe dispatch patch: %+v", op)
		}
	}
	for _, op := range ReconcileTrees(after, before, nil) {
		if op.Kind != PatchRemoveDispatch {
			t.Fatalf("expected generated dispatch removal: %+v", op)
		}
	}
	after.Nodes[0].DOMAttrs = nil
	after.Nodes[0].Events = nil
	if ops := ReconcileTrees(before, after, nil); len(ops) != 0 {
		t.Fatalf("eventless authored markers leaked: %+v", ops)
	}
}

func TestReconcileCallerDOMAttrsCannotGenerateDispatchOps(t *testing.T) {
	before := &ResolvedTree{Nodes: []ResolvedNode{{Tag: "button", DOMAttrs: []ResolvedAttr{{Name: "data-gosx-handler", Value: "edit"}}}}}
	after := &ResolvedTree{Nodes: []ResolvedNode{{Tag: "button", DOMAttrs: []ResolvedAttr{{Name: "data-gosx-handler", Value: "save"}}}}}
	for _, pair := range [][2]*ResolvedTree{{before, after}, {before, {Nodes: []ResolvedNode{{Tag: "button"}}}}} {
		for _, op := range ReconcileTrees(pair[0], pair[1], nil) {
			if op.Kind == PatchSetDispatch || op.Kind == PatchRemoveDispatch {
				t.Fatalf("caller attrs acquired generated privilege: %+v", op)
			}
		}
	}
}

func TestIslandDispatchCreatesChangesAndRemovesEventMarkers(t *testing.T) {
	// Each phase resolves a different program branch to the same button. This
	// exercises the VM's cached DOMAttrs, rather than hand-built resolved nodes.
	prog := &program.Program{
		Name: "DispatchMarkers",
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},
			{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},
			{Op: program.OpLitInt, Value: "2", Type: program.TypeInt},
			{Op: program.OpLitInt, Value: "3", Type: program.TypeInt},
			{Op: program.OpSignalGet, Value: "phase", Type: program.TypeInt},
			{Op: program.OpEq, Operands: []program.ExprID{4, 3}, Type: program.TypeBool},
			{Op: program.OpEq, Operands: []program.ExprID{4, 1}, Type: program.TypeBool},
			{Op: program.OpEq, Operands: []program.ExprID{4, 2}, Type: program.TypeBool},
			{Op: program.OpSignalSet, Value: "phase", Operands: []program.ExprID{1}, Type: program.TypeInt},
			{Op: program.OpSignalSet, Value: "phase", Operands: []program.ExprID{2}, Type: program.TypeInt},
			{Op: program.OpSignalSet, Value: "phase", Operands: []program.ExprID{3}, Type: program.TypeInt},
		},
		Signals: []program.SignalDef{{Name: "phase", Type: program.TypeInt, Init: 0}},
		Handlers: []program.Handler{
			{Name: "reveal", Body: []program.ExprID{8}},
			{Name: "edit", Body: []program.ExprID{9}},
			{Name: "save", Body: []program.ExprID{10}},
		},
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1, 3, 5}},
			{Kind: program.NodeConditional, Expr: 6, Children: []program.NodeID{2}},
			{Kind: program.NodeElement, Tag: "button", Attrs: []program.Attr{
				{Kind: program.AttrStatic, Name: "key", Value: "action"},
				{Kind: program.AttrEvent, Name: "onClick", Event: "edit"},
			}},
			{Kind: program.NodeConditional, Expr: 7, Children: []program.NodeID{4}},
			{Kind: program.NodeElement, Tag: "button", Attrs: []program.Attr{
				{Kind: program.AttrStatic, Name: "key", Value: "action"},
				{Kind: program.AttrEvent, Name: "onClick", Event: "save"},
			}},
			{Kind: program.NodeConditional, Expr: 5, Children: []program.NodeID{6}},
			{Kind: program.NodeElement, Tag: "button", Attrs: []program.Attr{{Kind: program.AttrStatic, Name: "key", Value: "action"}}},
		},
		Root: 0,
	}
	island := NewIsland(prog, `{}`)
	defer island.Dispose()
	for _, step := range []struct {
		handler string
		kind    PatchKind
		value   string
	}{
		{"reveal", PatchSetDispatch, "edit"},
		{"edit", PatchSetDispatch, "save"},
		{"save", PatchRemoveDispatch, ""},
	} {
		t.Run(step.handler, func(t *testing.T) {
			ops := island.Dispatch(step.handler, `{}`)
			markers := map[string]bool{}
			created := false
			for _, op := range ops {
				if op.Kind == PatchCreateElement && op.Tag == "button" {
					created = true
				}
				if op.AttrName != "data-gosx-on-click" && op.AttrName != "data-gosx-handler" {
					continue
				}
				if op.Kind != step.kind || op.Text != step.value || op.Path != "0" {
					t.Fatalf("%s: incorrect dispatch operation: %+v", step.handler, op)
				}
				markers[op.AttrName] = true
			}
			if len(markers) != 2 || created != (step.handler == "reveal") {
				t.Fatalf("%s: missing button or dispatch markers: %+v", step.handler, ops)
			}
		})
	}
}
