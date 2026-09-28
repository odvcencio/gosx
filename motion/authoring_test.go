package motion

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestProgramSerializesSignalsBindingsAndPinTo(t *testing.T) {
	p := NewProgram("motion-demo")
	scroll := p.ScrollProgress("scroll", "#stage", AxisY)
	cameraZ := p.Map("camera-z", scroll, 0, 1, 8, 5)
	hover := p.Hover("hover", "#button")
	lift := p.Spring("lift", 0, SpringOptions{
		To: 1, Input: hover, Physics: Spring{Stiffness: 260, Damping: 24},
	})
	p.BindCSSVariable(scroll, "#card", "--progress", "")
	p.BindCamera(cameraZ, "#scene", "position.z")
	p.BindSceneNode(lift, "#scene", "hover-node", "scale.x")
	p.BindMaterialUniform(lift, "#scene", "hover-node", "glow")
	p.PinTo("#scene", "pinned-node", "#pin")
	p.Tween("reveal", 0, 1, 180*time.Millisecond, Ease{Kind: EaseOutPow}, ReducedMotionFade)

	data, err := p.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var got Program
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.ID != "motion-demo" || len(got.Signals) != 5 {
		t.Fatalf("program header/signals = v%d %q %d", got.Version, got.ID, len(got.Signals))
	}
	if got.Signals[0].Source == nil || got.Signals[0].Source.Kind != SignalScroll {
		t.Fatalf("scroll source = %#v", got.Signals[0].Source)
	}
	if got.Signals[3].ReducedMotion != ReducedMotionSkip {
		t.Fatalf("spring reduced-motion default = %q", got.Signals[3].ReducedMotion)
	}
	if got.Signals[4].ReducedMotion != ReducedMotionFade {
		t.Fatalf("tween reduced-motion policy = %q", got.Signals[4].ReducedMotion)
	}
	if len(got.Bindings) != 4 || got.Bindings[1].Property != "position.z" || len(got.Pins) != 1 {
		t.Fatalf("bindings/pins = %#v / %#v", got.Bindings, got.Pins)
	}
	if !strings.Contains(string(data), `"kind":"scroll"`) || !strings.Contains(string(data), `"target":"materialUniform"`) {
		t.Fatalf("serialized program missing expected wire keys: %s", data)
	}
}

func TestProgramMarshalRejectsInvalidReferences(t *testing.T) {
	p := NewProgram("invalid")
	p.BindStyle("missing", "#target", "opacity", "")
	if _, err := p.Marshal(); err == nil || !strings.Contains(err.Error(), "unknown signal") {
		t.Fatalf("Marshal error = %v, want unknown signal", err)
	}
}

func TestProgramMarshalDoesNotMutateVersion(t *testing.T) {
	p := NewProgram("version-default")
	p.Version = 0
	data, err := p.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != 0 {
		t.Fatalf("Marshal mutated Version to %d", p.Version)
	}
	var got Program
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 {
		t.Fatalf("serialized version = %d, want 1", got.Version)
	}
}

func TestProgramMarshalRejectsUnsafeBindingValues(t *testing.T) {
	tests := []struct {
		name    string
		binding Binding
	}{
		{name: "unknown target", binding: Binding{Target: "script", Selector: "#target", Property: "opacity"}},
		{name: "cssText", binding: Binding{Target: BindingStyle, Selector: "#target", Property: "cssText"}},
		{name: "trimmed property", binding: Binding{Target: BindingStyle, Selector: "#target", Property: " opacity "}},
		{name: "unsafe unit", binding: Binding{Target: BindingCSSVariable, Selector: "#target", Property: "--offset", Unit: ";display:none"}},
		{name: "trimmed unit", binding: Binding{Target: BindingCSSVariable, Selector: "#target", Property: "--offset", Unit: " px "}},
		{name: "malformed css variable", binding: Binding{Target: BindingCSSVariable, Selector: "#target", Property: "--x; color"}},
		{name: "unknown scene property", binding: Binding{Target: BindingSceneNode, Selector: "#scene", Node: "node", Property: "__proto__"}},
		{name: "uniform injection", binding: Binding{Target: BindingMaterialUniform, Selector: "#scene", Node: "mesh", Property: "glow;opacity"}},
		{name: "camera unit", binding: Binding{Target: BindingCamera, Selector: "#scene", Property: "position.z", Unit: "px"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := NewProgram("unsafe-binding")
			signal := p.Time("time")
			test.binding.Signal = signal
			p.bind(test.binding)
			if _, err := p.Marshal(); err == nil {
				t.Fatal("Marshal accepted unsafe binding")
			}
		})
	}
}

func TestProgramMarshalRejectsUnknownSignalInputs(t *testing.T) {
	tests := []struct {
		name string
		set  func(*SignalSpec)
	}{
		{name: "input", set: func(s *SignalSpec) { s.Input = "missing" }},
		{name: "a", set: func(s *SignalSpec) { s.A = "missing" }},
		{name: "b", set: func(s *SignalSpec) { s.B = "missing" }},
		{name: "weight", set: func(s *SignalSpec) { s.Weight = "missing" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := NewProgram("unknown-input")
			spec := SignalSpec{ID: "derived", Kind: SignalMix}
			test.set(&spec)
			p.Signals = append(p.Signals, spec)
			if _, err := p.Marshal(); err == nil {
				t.Fatal("Marshal accepted unknown signal input")
			}
		})
	}
}
