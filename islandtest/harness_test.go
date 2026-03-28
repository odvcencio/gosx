package islandtest

import (
	"strings"
	"testing"

	"github.com/odvcencio/gosx/client/vm"
	"github.com/odvcencio/gosx/island/program"
)

func listProgram() *program.Program {
	return &program.Program{
		Name: "List",
		Props: []program.PropDef{
			{Name: "items", Type: program.TypeAny},
		},
		Exprs: []program.Expr{
			{Op: program.OpPropGet, Value: "items", Type: program.TypeAny},                                  // 0
			{Op: program.OpPropGet, Value: "item", Type: program.TypeAny},                                   // 1
			{Op: program.OpSignalGet, Value: "items", Type: program.TypeAny},                                // 2
			{Op: program.OpLitString, Value: "gamma", Type: program.TypeString},                             // 3
			{Op: program.OpAppend, Operands: []program.ExprID{2, 3}, Type: program.TypeAny},                 // 4
			{Op: program.OpSignalSet, Operands: []program.ExprID{4}, Value: "items", Type: program.TypeAny}, // 5
		},
		Signals: []program.SignalDef{
			{Name: "items", Type: program.TypeAny, Init: 0},
		},
		Handlers: []program.Handler{
			{Name: "add", Body: []program.ExprID{5}},
		},
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1, 2}},
			{Kind: program.NodeElement, Tag: "button", Attrs: []program.Attr{{Kind: program.AttrEvent, Name: "click", Event: "add"}}, Children: []program.NodeID{3}},
			{Kind: program.NodeForEach, Expr: 2, Attrs: []program.Attr{{Kind: program.AttrStatic, Name: "as", Value: "item"}}, Children: []program.NodeID{4}},
			{Kind: program.NodeText, Text: "add"},
			{Kind: program.NodeElement, Tag: "li", Children: []program.NodeID{5}},
			{Kind: program.NodeExpr, Expr: 1},
		},
		Root: 0,
		StaticMask: []bool{
			false, false, false, true, false, false,
		},
	}
}

func TestHarnessClickUpdatesRenderedHTML(t *testing.T) {
	h, err := New(program.CounterProgram(), nil)
	if err != nil {
		t.Fatalf("new harness: %v", err)
	}

	if html := h.HTML(); !strings.Contains(html, ">0<") {
		t.Fatalf("expected initial count render, got %s", html)
	}

	patches, err := h.Click("increment")
	if err != nil {
		t.Fatalf("click increment: %v", err)
	}
	if len(patches) == 0 {
		t.Fatal("expected patches after increment")
	}

	if html := h.HTML(); !strings.Contains(html, ">1<") {
		t.Fatalf("expected updated count render, got %s", html)
	}
}

func TestHarnessInputCarriesEventValue(t *testing.T) {
	h, err := New(program.EditorProgram(), nil)
	if err != nil {
		t.Fatalf("new harness: %v", err)
	}

	patches, err := h.Input("onInput", "abc")
	if err != nil {
		t.Fatalf("input onInput: %v", err)
	}
	if len(patches) == 0 {
		t.Fatal("expected patches after input")
	}

	html := h.HTML()
	if !strings.Contains(html, "abc") {
		t.Fatalf("expected editor contents in html, got %s", html)
	}
	if !strings.Contains(html, "3 chars") {
		t.Fatalf("expected updated character count, got %s", html)
	}
}

func TestHarnessDispatchErrorsForUnknownHandler(t *testing.T) {
	h, err := New(program.CounterProgram(), nil)
	if err != nil {
		t.Fatalf("new harness: %v", err)
	}

	if _, err := h.Click("missing"); err == nil {
		t.Fatal("expected missing-handler error")
	}
}

func TestHarnessEachRendersAndAppends(t *testing.T) {
	h, err := New(listProgram(), map[string]any{
		"items": []string{"alpha", "beta"},
	})
	if err != nil {
		t.Fatalf("new harness: %v", err)
	}

	initial := h.HTML()
	for _, snippet := range []string{"<li>alpha</li>", "<li>beta</li>"} {
		if !strings.Contains(initial, snippet) {
			t.Fatalf("expected %q in %s", snippet, initial)
		}
	}

	patches, err := h.Click("add")
	if err != nil {
		t.Fatalf("click add: %v", err)
	}
	if len(patches) == 0 {
		t.Fatal("expected patches after append")
	}

	foundCreate := false
	for _, patch := range patches {
		if patch.Kind == vm.PatchCreateElement || patch.Kind == vm.PatchCreateText {
			foundCreate = true
			break
		}
	}
	if !foundCreate {
		t.Fatalf("expected create patch for appended list item, got %+v", patches)
	}

	html := h.HTML()
	for _, snippet := range []string{"<li>alpha</li>", "<li>beta</li>", "<li>gamma</li>"} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %s", snippet, html)
		}
	}
}
