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
