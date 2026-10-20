package ir

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/island/program"
)

func callbackCompositionProgram() *Program {
	p := basicComposedIslandProgram()
	p.Components[0].Scope = &ComponentScope{
		Signals:  []SignalInfo{{Name: "count", InitExpr: "0", TypeHint: "int"}},
		Handlers: []HandlerInfo{{Name: "increment", Statements: []string{"count.Set(count.Get() + 1)"}}},
	}
	p.Components[1].PropsFields["OnActivate"] = "func()"
	p.Nodes[1].Attrs = []Attr{
		{Kind: AttrStatic, Name: "Label", Value: "Increment"},
		{Kind: AttrExpr, Name: "OnActivate", Expr: "increment"},
	}
	p.Nodes[2].Tag = "button"
	p.Nodes[2].Attrs = []Attr{{Kind: AttrExpr, Name: "onClick", Expr: "props.OnActivate", IsEvent: true}}
	return p
}

func TestIslandComposedCallbackBindsParentHandler(t *testing.T) {
	p := callbackCompositionProgram()
	lowered, err := LowerIsland(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lowered.Handlers) != 1 || lowered.Handlers[0].Name != "increment" {
		t.Fatalf("composition must reuse the parent handler: %+v", lowered.Handlers)
	}
	button := lowered.Nodes[lowered.Nodes[lowered.Root].Children[0]]
	if len(button.Attrs) != 1 || button.Attrs[0].Kind != program.AttrEvent || button.Attrs[0].Event != "increment" {
		t.Fatalf("wrong static callback binding: %+v", button.Attrs)
	}
	for _, expr := range lowered.Exprs {
		if expr.Op == program.OpPropGet && expr.Value == "increment" {
			t.Fatal("callback escaped into the value graph")
		}
	}
}

func TestIslandComposedCallbackRejectsDynamicOrDataUse(t *testing.T) {
	for _, source := range []string{"missing", "props.OnActivate", "increment()", "true ? increment : increment"} {
		t.Run(source, func(t *testing.T) {
			p := callbackCompositionProgram()
			p.Nodes[1].Attrs[1].Expr = source
			if _, err := LowerIsland(p, 0); err == nil || !strings.Contains(err.Error(), "callback") {
				t.Fatalf("callback source %q accepted: %v", source, err)
			}
		})
	}
	p := callbackCompositionProgram()
	p.Nodes[3].Text = "props.OnActivate"
	if _, err := LowerIsland(p, 0); err == nil || !strings.Contains(err.Error(), "callback") {
		t.Fatalf("callback rendered as data: %v", err)
	}
}

func TestIslandCallbackRequiresADeclaredHandlerAndSupportedEvent(t *testing.T) {
	p := callbackCompositionProgram()
	p.Nodes[0].Attrs = []Attr{{Kind: AttrExpr, Name: "onClick", Expr: "forged", IsEvent: true}}
	p.Nodes[1].Attrs[1].Expr = "forged"
	if _, err := LowerIsland(p, 0); err == nil || !strings.Contains(err.Error(), "callback") {
		t.Fatalf("an event reference was mistaken for a declared handler: %v", err)
	}
	p = callbackCompositionProgram()
	p.Nodes[2].Attrs[0].Name = "onUnknownEvent"
	if _, err := LowerIsland(p, 0); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("unhandled DOM event accepted: %v", err)
	}
}
