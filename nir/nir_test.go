package nir

import (
	"encoding/json"
	"testing"
)

func TestModuleRoundTrip(t *testing.T) {
	in := &Module{
		SourceLanguage: "swift",
		Components: []*Component{{
			Name: "Counter",
			Body: &Element{
				Tag: "vstack",
				Children: []View{
					&Element{Tag: "text", Children: []View{&Text{Value: "hi"}}},
				},
			},
		}},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Module
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Components) != 1 || out.Components[0].Name != "Counter" {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if _, ok := out.Components[0].Body.(*Element); !ok {
		t.Fatalf("body did not decode as element: %T", out.Components[0].Body)
	}
}

func TestComponentSignals(t *testing.T) {
	c := &Component{
		Name: "Counter",
		Signals: []*SignalDecl{{
			Name: "count",
			Type: "Int",
			Init: &RxExpr{Kind: "ref", Ref: "props.start"},
		}},
	}
	if c.Signals[0].Name != "count" {
		t.Fatalf("signal field broken: %+v", c.Signals[0])
	}
}

func TestComponentComputeds(t *testing.T) {
	in := &Module{
		Components: []*Component{{
			Name: "Derived",
			Signals: []*SignalDecl{{
				Name: "count",
				Type: "Int",
				Init: &RxExpr{Kind: "literal", Literal: &Literal{Type: "int", Value: "1"}},
			}},
			Computeds: []*ComputedDecl{{
				Name: "doubled",
				Type: "Int",
				Body: &RxExpr{
					Kind: "binop",
					BinOp: &BinOp{
						Op:    "*",
						Left:  RxExpr{Kind: "ref", Ref: "count"},
						Right: RxExpr{Kind: "literal", Literal: &Literal{Type: "int", Value: "2"}},
					},
				},
			}},
			Body: &Element{Tag: "text", Children: []View{&ExprHole{Expr: RxExpr{Kind: "ref", Ref: "doubled"}}}},
		}},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Module
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Components) != 1 || len(out.Components[0].Computeds) != 1 {
		t.Fatalf("computed round-trip mismatch: %+v", out.Components)
	}
	if out.Components[0].Computeds[0].Name != "doubled" || out.Components[0].Computeds[0].Body.Kind != "binop" {
		t.Fatalf("computed field broken: %+v", out.Components[0].Computeds[0])
	}
}

func TestConditionalExpressionRoundTrip(t *testing.T) {
	in := RxExpr{
		Kind: "cond",
		Cond: &Cond{
			Condition: RxExpr{Kind: "ref", Ref: "visible"},
			Then:      RxExpr{Kind: "literal", Literal: &Literal{Type: "string", Value: "shown"}},
			Else:      RxExpr{Kind: "literal", Literal: &Literal{Type: "string", Value: "hidden"}},
		},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out RxExpr
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Kind != "cond" || out.Cond == nil || out.Cond.Condition.Ref != "visible" || out.Cond.Then.Literal.Value != "shown" || out.Cond.Else.Literal.Value != "hidden" {
		t.Fatalf("conditional expression round-trip mismatch: %+v", out)
	}
}

func TestConditionalViewRoundTrip(t *testing.T) {
	in := &Module{
		Components: []*Component{{
			Name: "Toggle",
			Body: &Conditional{
				Condition: RxExpr{Kind: "ref", Ref: "visible"},
				Then: []View{
					&Element{Tag: "text", Children: []View{&Text{Value: "visible"}}},
				},
				Else: []View{
					&Element{Tag: "text", Children: []View{&Text{Value: "hidden"}}},
				},
			},
		}},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Module
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	conditional, ok := out.Components[0].Body.(*Conditional)
	if !ok {
		t.Fatalf("body decoded as %T, want *Conditional", out.Components[0].Body)
	}
	if conditional.Condition.Ref != "visible" || len(conditional.Then) != 1 || len(conditional.Else) != 1 {
		t.Fatalf("conditional round-trip mismatch: %+v", conditional)
	}
}

func TestComponentRefViewRoundTrip(t *testing.T) {
	in := &Module{
		Components: []*Component{{
			Name: "Profile",
			Body: &ComponentRef{
				Name: "Badge",
				Props: []Attr{{
					Name:  "label",
					Value: RxExpr{Kind: "ref", Ref: "props.name"},
				}},
			},
		}},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Module
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ref, ok := out.Components[0].Body.(*ComponentRef)
	if !ok {
		t.Fatalf("body decoded as %T, want *ComponentRef", out.Components[0].Body)
	}
	if ref.Name != "Badge" || len(ref.Props) != 1 || ref.Props[0].Value.Ref != "props.name" {
		t.Fatalf("component ref round-trip mismatch: %+v", ref)
	}
}

func TestLoopViewRoundTrip(t *testing.T) {
	in := &Module{
		Components: []*Component{{
			Name: "Roster",
			Body: &Loop{
				Items:    RxExpr{Kind: "ref", Ref: "props.items"},
				ItemName: "item",
				Body: []View{
					&Element{Tag: "text", Children: []View{&ExprHole{Expr: RxExpr{Kind: "ref", Ref: "item"}}}},
				},
			},
		}},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Module
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	loop, ok := out.Components[0].Body.(*Loop)
	if !ok {
		t.Fatalf("body decoded as %T, want *Loop", out.Components[0].Body)
	}
	if loop.Items.Ref != "props.items" || loop.ItemName != "item" || len(loop.Body) != 1 {
		t.Fatalf("loop round-trip mismatch: %+v", loop)
	}
}
