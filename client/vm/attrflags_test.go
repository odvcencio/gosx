package vm

import (
	"reflect"
	"testing"

	"m31labs.dev/gosx/island/program"
)

func attrFlagsTestAttrs() []program.Attr {
	names := []string{
		"class", "id", "href", "HREF", "src", "xlink:href", "poster", "action", "formaction", "style",
		"onclick", "OnClick", "on", "x y", "a\"b", "", "data-gosx-handler", "DATA-GOSX-PATH",
		"data-gosx-on-click", "data-gosx-link", "aria-label", "d", "key", "disabled", "checked",
	}
	var attrs []program.Attr
	for _, name := range names {
		for _, kind := range []program.AttrKind{program.AttrStatic, program.AttrExpr, program.AttrBool, program.AttrEvent} {
			attrs = append(attrs, program.Attr{Kind: kind, Name: name, Value: "java\nscript:x", Expr: 0, Event: "h"})
		}
	}
	return attrs
}

// The load-time verdicts must give the same output as checking on render.
func TestAttrFlagsMatchRenderTimeChecks(t *testing.T) {
	attrs := attrFlagsTestAttrs()
	prog := progFromExprs([]program.Expr{{Op: program.OpLitString, Value: "javascript:x", Type: program.TypeString}})
	prog.Nodes = []program.Node{{Kind: program.NodeElement, Tag: "a", Attrs: attrs}}
	machine := NewVM(prog, nil)
	flags := machine.attrFlagsFor(0, len(attrs))
	if flags == nil {
		t.Fatal("NewVM left no verdicts for a node with attributes")
	}
	wantR, wantD, wantE, wantK, wantEK := machine.resolveElementAttrs(attrs, nil)
	gotR, gotD, gotE, gotK, gotEK := machine.resolveElementAttrs(attrs, flags)
	if !reflect.DeepEqual(wantR, gotR) || !reflect.DeepEqual(wantD, gotD) || !reflect.DeepEqual(wantE, gotE) || wantK != gotK || wantEK != gotEK {
		t.Fatalf("flags changed output:\nwant %#v %#v %#v\ngot  %#v %#v %#v", wantR, wantD, wantE, gotR, gotD, gotE)
	}
}

func TestAttrFlagsFollowSwapProgramAndIgnoreStaleShape(t *testing.T) {
	a := progFromExprs(nil)
	a.Nodes = []program.Node{{Kind: program.NodeElement, Tag: "a", Attrs: []program.Attr{{Kind: program.AttrStatic, Name: "href", Value: "x"}}}}
	machine := NewVM(a, nil)
	if f := machine.attrFlagsFor(0, 1); len(f) != 1 || f[0]&attrURL == 0 || f[0]&attrSkip != 0 {
		t.Fatalf("flags %v", f)
	}
	b := progFromExprs(nil)
	b.Nodes = []program.Node{{Kind: program.NodeElement, Tag: "a", Attrs: []program.Attr{{Kind: program.AttrStatic, Name: "onclick", Value: "x"}, {Kind: program.AttrStatic, Name: "id", Value: "y"}}}}
	machine.SwapProgram(b)
	if f := machine.attrFlagsFor(0, 2); len(f) != 2 || f[0]&attrSkip == 0 || f[1] != 0 {
		t.Fatalf("flags after swap %v", f)
	}
	if machine.attrFlagsFor(0, 3) != nil || machine.attrFlagsFor(-1, 2) != nil || machine.attrFlagsFor(5, 2) != nil {
		t.Fatal("mismatched shape must fall back to checking on render")
	}
}

func benchAttrs() []program.Attr {
	return []program.Attr{
		{Kind: program.AttrStatic, Name: "class", Value: "mg-session"},
		{Kind: program.AttrExpr, Name: "data-state", Expr: 0},
		{Kind: program.AttrStatic, Name: "aria-label", Value: "Room service"},
		{Kind: program.AttrExpr, Name: "d", Expr: 0},
		{Kind: program.AttrStatic, Name: "data-testid", Value: "session"},
		{Kind: program.AttrEvent, Name: "onClick", Event: "pick"},
	}
}

func benchResolveAttrs(b *testing.B, useFlags bool) {
	attrs := benchAttrs()
	prog := progFromExprs([]program.Expr{{Op: program.OpLitString, Value: "M0 0L10 10", Type: program.TypeString}})
	prog.Nodes = []program.Node{{Kind: program.NodeElement, Tag: "path", Attrs: attrs}}
	machine := NewVM(prog, nil)
	var flags []uint8
	if useFlags {
		flags = machine.attrFlagsFor(0, len(attrs))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		machine.resolveElementAttrs(attrs, flags)
	}
}

func BenchmarkResolveElementAttrsChecked(b *testing.B) { benchResolveAttrs(b, false) }
func BenchmarkResolveElementAttrsLoaded(b *testing.B)  { benchResolveAttrs(b, true) }
