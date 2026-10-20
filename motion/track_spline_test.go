package motion

import (
	"encoding/json"
	"math"
	"testing"
)

func TestSampleSplineTangentsRoundTripAndCopy(t *testing.T) {
	in, out := Vec3V(1, -2, 3), Vec3V(2, 3, -4)
	track := Track{Interp: InterpCubicSpline, Keys: []Key{
		{Value: Vec3V(0, 1, 2), OutTangent: &out},
		{T: 2, Value: Vec3V(2, 3, 4), InTangent: &in},
	}}
	p := NewProgram("spline")
	ref, err := p.SampleTrack("path", p.Time("clock"), track)
	if err != nil {
		t.Fatal(err)
	}
	p.BindCamera(ref, "#scene", "position")
	in.F[0], out.F[0] = 99, 99
	spec := p.Signals[1]
	if spec.Interp != InterpCubicSpline || spec.Frames[0].OutTangent.([]float64)[0] != 2 || spec.Frames[1].InTangent.([]float64)[0] != 1 {
		t.Fatal("spline aliases authored tangents or loses interpolation")
	}
	wire, err := p.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Program
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := decoded.Marshal()
	if err != nil || string(roundTrip) != string(wire) {
		t.Fatalf("spline wire round trip: %v", err)
	}
}

func TestSampleSplineRejectsMalformedTangentsWithoutMutation(t *testing.T) {
	for _, tangent := range []Value{ScalarV(1), Vec3V(1, math.NaN(), 2), Vec3V(1, 2, math.Inf(1))} {
		for _, incoming := range []bool{false, true} {
			p := NewProgram("bad")
			clock := p.Time("clock")
			track := Track{Interp: InterpCubicSpline, Keys: []Key{{Value: Vec3V(0, 0, 0)}, {T: 1, Value: Vec3V(1, 1, 1)}}}
			if incoming {
				track.Keys[1].InTangent = &tangent
			} else {
				track.Keys[0].OutTangent = &tangent
			}
			if _, err := p.SampleTrack("bad", clock, track); err == nil || len(p.Signals) != 1 {
				t.Fatalf("invalid tangent changed program: %v", err)
			}
		}
	}
	for _, edit := range []func(*SignalSpec){
		func(s *SignalSpec) { s.Interp = Interp(99) },
		func(s *SignalSpec) { s.Interp = InterpSlerp },
		func(s *SignalSpec) { s.Interp = InterpLinear; s.Frames[0].OutTangent = []float64{1, 2, 3} },
		func(s *SignalSpec) { s.Frames[0].OutTangent = []any{1.0, "2", 3.0} },
		func(s *SignalSpec) { s.Frames[1].InTangent = []float64{1, 2} },
	} {
		p := NewProgram("bad-wire")
		if _, err := p.SampleTrack("path", p.Time("clock"), Track{Interp: InterpCubicSpline, Keys: []Key{
			{Value: Vec3V(0, 0, 0)}, {T: 1, Value: Vec3V(1, 1, 1)},
		}}); err != nil {
			t.Fatal(err)
		}
		edit(&p.Signals[1])
		if _, err := p.Marshal(); err == nil {
			t.Fatal("accepted malformed spline wire data")
		}
	}
}
