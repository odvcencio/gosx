package motion

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSVGMorphBindingWireAndValidation(t *testing.T) {
	p := NewProgram("diagram")
	progress := p.Hover("progress", "#diagram")
	p.BindSVGMorph(progress, " #before ", " #after ")
	data, err := p.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Program
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := decoded.SVGMorphs[0]; got.Signal != progress || got.From != "#before" || got.To != "#after" {
		t.Fatalf("unexpected binding: %+v", got)
	}
	for _, binding := range []SVGMorphBinding{
		{Signal: "missing", From: "#a", To: "#b"},
		{Signal: progress, From: " ", To: "#b"},
		{Signal: progress, From: "#a", To: "#a"},
		{Signal: progress, From: "#a", To: strings.Repeat("x", 1025)},
	} {
		p.SVGMorphs = []SVGMorphBinding{binding}
		if _, err := p.Marshal(); err == nil {
			t.Fatalf("accepted invalid binding: %+v", binding)
		}
	}
	p.SVGMorphs = make([]SVGMorphBinding, 129)
	if _, err := p.Marshal(); err == nil {
		t.Fatal("accepted unbounded SVG morph bindings")
	}
}
