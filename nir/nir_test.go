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
