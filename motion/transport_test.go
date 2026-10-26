package motion

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestTransportAuthoring(t *testing.T) {
	labels := map[string]time.Duration{"arrival": 1200 * time.Millisecond}
	scenes := []string{"#shader"}
	p := NewProgram("story").WithTransport(TransportOptions{Duration: 3 * time.Second, Labels: labels, Scenes: scenes})
	clock := p.Playhead("clock")
	value := p.Curve("entrance", clock, []CurveStop{{At: 0, Value: 0}, {At: 1.2, Value: 1}}, false)
	p.BindStyle(value, ".title", "opacity", "")
	p.BindCamera(value, "#shader", "position.x")
	p.BindMaterialUniform(value, "#shader", "shader-shape", "gain")
	labels["arrival"], scenes[0] = 0, "#changed"
	data, err := p.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var wire Program
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Transport.DurationMS != 3000 || wire.Transport.Labels["arrival"] != 1200 || wire.Transport.Scenes[0] != "#shader" || wire.Signals[0].Kind != SignalPlayhead {
		t.Fatalf("transport lost in wire program: %s", data)
	}
	_, rest, err := CompileCSS(p)
	if err != nil || rest.Transport == nil || len(rest.Bindings) != 3 {
		t.Fatalf("CSS compilation must preserve transport bindings: %v", err)
	}
}

func TestTransportValidation(t *testing.T) {
	for _, spec := range []*TransportSpec{
		{DurationMS: -1}, {DurationMS: math.NaN()}, {DurationMS: math.Inf(1)}, {DurationMS: 86400001},
		{DurationMS: 1000, Labels: map[string]float64{"past": 1001}},
		{DurationMS: 1000, Labels: map[string]float64{" ": 1}},
		{Scenes: []string{" "}},
	} {
		p := NewProgram("bad")
		p.Transport = spec
		if _, err := p.Marshal(); err == nil {
			t.Fatalf("accepted invalid transport: %+v", spec)
		}
	}
	p := NewProgram("missing")
	p.Playhead("clock")
	if _, err := p.Marshal(); err == nil || !strings.Contains(err.Error(), "requires a transport") {
		t.Fatalf("playhead without transport: %v", err)
	}
}
