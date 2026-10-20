package motion

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func valueTracks() []Track {
	keys := func(values ...Value) []Key {
		out := make([]Key, len(values))
		for i, value := range values {
			out[i] = Key{T: .2 + float64(i)*1.2, Value: value}
		}
		return out
	}
	q := QuatFromEuler(.2, .3, -.4)
	other := QuatFromEuler(-.4, 2.8, .6)
	tracks := []Track{
		{Keys: keys(Vec2V(-10, 4), Vec2V(12, -2), Vec2V(2, 8)), Ease: Ease{Kind: EaseOutBack}},
		{Keys: keys(Vec3V(-.6, -.3, .1), Vec3V(.4, .8, 0), Vec3V(0, .1, -.6)), Ease: Ease{Kind: EaseCubicBezier, Args: []float64{.22, 1, .36, 1}}},
		{Keys: keys(Vec4V(-1, 3, 0, 6), Vec4V(3, 5, -2, 8)), Ease: Ease{Kind: EaseSteps, Args: []float64{4}}},
		{Keys: keys(ColorV(.18, .7, .6, .3), ColorV(1.2, .2, .08, 1)), Ease: Ease{Kind: EaseOutBack}},
		{Keys: keys(QuatV(q), QuatV(other)), Interp: InterpSlerp, Ease: Ease{Kind: EaseInOutPow}},
		{Keys: keys(QuatV(q), QuatV(Quat{-other.X, -other.Y, -other.Z, -other.W}))},
		{Keys: keys(QuatV(q), QuatV(Quat{-q.X, -q.Y, -q.Z, -q.W}))},
		{Keys: keys(QuatV(Quat{W: 1}), QuatV(QuatFromEuler(0, .00001, 0)))},
	}
	step := tracks[1]
	step.Interp = InterpStep
	tracks = append(tracks, step)
	step = tracks[4]
	step.Interp = InterpStep
	tracks = append(tracks, step)
	return tracks
}

func TestSampleValueTrackGolden(t *testing.T) {
	type sample struct {
		Time  float64   `json:"time"`
		Value []float64 `json:"value"`
	}
	type fixture struct {
		Signal  SignalSpec `json:"signal"`
		Samples []sample   `json:"samples"`
	}
	var fixtures []fixture
	for _, track := range valueTracks() {
		p := NewProgram("typed")
		if _, err := p.SampleTrack("value", p.Time("clock"), track); err != nil {
			t.Fatal(err)
		}
		f := fixture{Signal: p.Signals[1]}
		buf := NewWriteBuf(16)
		times := []float64{-1, 0, .2, .7, 1.4 - 1e-7, 1.4, 1.4 + 1e-7, 2, 2.6, 3, 5, .7, .2}
		for i := 0; i < 24; i++ {
			times = append(times, float64(i)*.127)
		}
		for _, at := range times {
			buf.Reset()
			evalTrack(&track, at, 0, Policy{}, buf)
			f.Samples = append(f.Samples, sample{at, append([]float64(nil), buf.Writes()[3:]...)})
		}
		fixtures = append(fixtures, f)
	}
	data, err := json.MarshalIndent(fixtures, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join("testdata", "track_value_golden.json")
	if *updateTrackGolden {
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil || string(want) != string(data) {
		t.Fatalf("native value fixtures differ; regenerate with -update-track-golden: %v", err)
	}
}

func TestSampleValueTracksRoundTripAndCopy(t *testing.T) {
	for _, track := range valueTracks() {
		p := NewProgram("typed")
		ref, err := p.SampleTrack("value", p.Time("clock"), track)
		if err != nil {
			t.Fatal(err)
		}
		p.BindMaterialUniform(ref, "#scene", "form", "value")
		original := p.Signals[1].Frames[0].Value.([]float64)[0]
		track.Keys[0].Value.F[0] = 99
		if p.Signals[1].Frames[0].Value.([]float64)[0] != original {
			t.Fatal("track aliases original values")
		}
		track.Keys[0].Value.F[0] = original
		wire, err := p.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		var decoded Program
		if err := json.Unmarshal(wire, &decoded); err != nil {
			t.Fatal(err)
		}
		if _, err := decoded.Marshal(); err != nil {
			t.Fatalf("JSON vector round trip: %v", err)
		}
	}
}

func TestSampleValueTracksRejectInvalidShapes(t *testing.T) {
	invalid := []Track{
		{Keys: []Key{{Value: Vec2V(1, 2)}, {T: 1, Value: Vec3V(1, 2, 3)}}},
		{Keys: []Key{{Value: Vec2V(1, 2)}, {T: 1, Value: Vec2V(1, math.NaN())}}},
		{Keys: []Key{{Value: QuatV(Quat{})}, {T: 1, Value: QuatV(Quat{W: 1})}}},
		{Keys: []Key{{Value: Vec2V(1, 2)}, {T: 1, Value: Vec2V(3, 4)}}, Interp: InterpSlerp},
		{Keys: []Key{{Value: Value{Arity: 99}}, {T: 1, Value: Value{Arity: 99}}}},
	}
	for _, track := range invalid {
		p := NewProgram("bad")
		clock := p.Time("clock")
		if _, err := p.SampleTrack("bad", clock, track); err == nil || len(p.Signals) != 1 {
			t.Fatalf("invalid track changed program: %v", err)
		}
	}
}

func TestSampleValueTrackBindingsAndComposition(t *testing.T) {
	p := NewProgram("typed")
	clock := p.Time("clock")
	a, _ := p.SampleTrack("a", clock, valueTracks()[1])
	b, _ := p.SampleTrack("b", clock, valueTracks()[1])
	weight := p.Hover("hover", "#form")
	mixed := p.Mix("mixed", a, b, weight)
	p.BindSceneNode(mixed, "#scene", "form", "position")
	p.BindCamera(mixed, "#scene", "position")
	if _, err := p.Marshal(); err != nil {
		t.Fatal(err)
	}
	p.BindStyle(mixed, "#title", "opacity", "")
	if _, err := p.Marshal(); err == nil {
		t.Fatal("accepted vector opacity")
	}
	p.Bindings = p.Bindings[:len(p.Bindings)-1]
	p.Map("bad-map", mixed, 0, 1, 0, 1)
	if _, err := p.Marshal(); err == nil {
		t.Fatal("accepted vector through scalar map")
	}
	p = NewProgram("cycle")
	p.Signals = []SignalSpec{{ID: "a", Kind: SignalMap, Input: "b"}, {ID: "b", Kind: SignalMap, Input: "a"}}
	if _, err := p.Marshal(); err == nil || !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("cycle: %v", err)
	}
}

func TestMotionValueGraphDepthLimitIsDeterministic(t *testing.T) {
	for _, edges := range []int{1024, 1025} {
		p := NewProgram("depth")
		previous := p.Time("clock")
		for i := 0; i < edges; i++ {
			previous = p.Map(fmt.Sprintf("level-%d", i), previous, 0, 1, 0, 1)
		}
		for attempt := 0; attempt < 20; attempt++ {
			_, err := p.Marshal()
			if (err != nil) != (edges > 1024) {
				t.Fatalf("%d dependency edges, attempt %d: %v", edges, attempt, err)
			}
		}
	}
}
