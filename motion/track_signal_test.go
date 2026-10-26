package motion

import (
	"encoding/json"
	"flag"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateTrackGolden = flag.Bool("update-track-golden", false, "rewrite track native/browser parity fixtures")

func scalarTrack(ease Ease) Track {
	return Track{Ease: ease, Keys: []Key{
		{T: .2, Value: ScalarV(24)}, {T: 1.4, Value: ScalarV(0)},
		{T: 2, Value: ScalarV(0)}, {T: 3, Value: ScalarV(12)},
	}}
}

func TestSampleTrackGolden(t *testing.T) {
	type fixture struct {
		Signal  SignalSpec   `json:"signal"`
		Samples [][2]float64 `json:"samples"`
	}
	var cases []fixture
	tracks := []Track{
		scalarTrack(Ease{}), scalarTrack(Ease{Kind: EaseOutPow, Args: []float64{3}}),
		scalarTrack(Ease{Kind: EaseCubicBezier, Args: []float64{.22, 1, .36, 1}}),
		scalarTrack(Ease{Kind: EaseOutBack}), scalarTrack(Ease{Kind: EaseInOutBack}),
		scalarTrack(Ease{Kind: EaseSteps, Args: []float64{4}}),
	}
	step := scalarTrack(Ease{})
	step.Interp = InterpStep
	tracks = append(tracks, step)
	perKey := scalarTrack(Ease{Kind: EaseOutPow})
	perKey.Keys[0].Ease = &Ease{Kind: EaseInPow, Args: []float64{3}}
	tracks = append(tracks, perKey)
	out, in := ScalarV(4), ScalarV(-2)
	cubic := scalarTrack(Ease{Kind: EaseInPow, Args: []float64{3}})
	cubic.Interp = InterpCubicSpline
	cubic.Keys[0].OutTangent, cubic.Keys[1].InTangent = &out, &in
	cubic.Keys[1].OutTangent, cubic.Keys[2].InTangent = &in, &out
	tracks = append(tracks, cubic)
	tracks = append(tracks, ExpandStagger(tracks[3], []int{0, 1, 2}, StaggerSpec{From: FromCenter, Delay: .17})...)
	for _, track := range tracks {
		p := NewProgram("golden")
		if _, err := p.SampleTrack("pose", p.Time("clock"), track); err != nil {
			t.Fatal(err)
		}
		f := fixture{Signal: p.Signals[1]}
		buf := NewWriteBuf(16)
		times := []float64{-1, 0, .2, 1.4, 2, 3, 5, .75, .1, 2.2, .75}
		for i := 0; i < 100; i++ {
			times = append(times, float64(i)*.037)
		}
		for _, at := range times {
			buf.Reset()
			evalTrack(&track, at, 0, Policy{}, buf)
			f.Samples = append(f.Samples, [2]float64{at, buf.Writes()[3]})
		}
		cases = append(cases, f)
	}
	data, err := json.MarshalIndent(cases, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join("testdata", "track_signal_golden.json")
	if *updateTrackGolden {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil || string(want) != string(data) {
		t.Fatalf("native track fixtures differ; rerun with -update-track-golden: %v", err)
	}
}

func TestSampleTrackCopiesAuthoringAndSurvivesCSSCompilation(t *testing.T) {
	p := NewProgram("story")
	track := scalarTrack(Ease{Kind: EaseOutPow, Args: []float64{3}})
	track.Keys[2].Ease = &Ease{Kind: EaseInPow, Args: []float64{2}}
	ref, err := p.SampleTrack("rise", p.Time("clock"), track)
	if err != nil {
		t.Fatal(err)
	}
	p.BindStyle(ref, ".line", "transform.y", "px")
	track.Keys[0].T = 99
	track.Keys[0].Value = ScalarV(99)
	track.Ease.Args[0] = 99
	track.Keys[2].Ease.Args[0] = 99
	if s := p.Signals[1]; s.Frames[0].At != .2 || s.Frames[0].Value != float64(24) || s.Frames[0].Ease.Args[0] != 3 || s.Frames[2].Ease.Args[0] != 2 {
		t.Fatal("track signal aliases authored keys or easing arguments")
	}
	css, rest, err := CompileCSS(p)
	if err != nil || css != "" || len(rest.Signals) != 2 || len(rest.Bindings) != 1 {
		t.Fatalf("CSS compiler lost scalar track: %v", err)
	}
	if _, err := rest.Marshal(); err != nil {
		t.Fatal(err)
	}
}

func TestSampleTrackRejectsInvalidAuthoringWithoutMutation(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Track)
	}{
		{"few keys", func(tr *Track) { tr.Keys = tr.Keys[:1] }},
		{"too many", func(tr *Track) { tr.Keys = make([]Key, 4097) }},
		{"generator", func(tr *Track) { tr.Gen = &Generator{Kind: GenSpin} }},
		{"unknown interpolation", func(tr *Track) { tr.Interp = Interp(99) }},
		{"mixed arity", func(tr *Track) { tr.Keys[0].Value = Vec2V(1, 2) }},
		{"duplicate time", func(tr *Track) { tr.Keys[1].T = tr.Keys[0].T }},
		{"nan", func(tr *Track) { tr.Keys[0].T = math.NaN() }},
		{"infinite value", func(tr *Track) { tr.Keys[0].Value = ScalarV(math.Inf(1)) }},
		{"bad easing", func(tr *Track) { tr.Ease.Kind = EaseKind(99) }},
		{"nan easing", func(tr *Track) { tr.Ease.Args = []float64{math.NaN()} }},
		{"power", func(tr *Track) { tr.Ease = Ease{Kind: EaseOutPow, Args: []float64{-1}} }},
		{"fractional steps", func(tr *Track) { tr.Ease = Ease{Kind: EaseSteps, Args: []float64{1.5}} }},
		{"bad bezier", func(tr *Track) { tr.Ease = Ease{Kind: EaseCubicBezier, Args: []float64{2, 0, 0, 1}} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := NewProgram("bad")
			clock := p.Time("clock")
			track := scalarTrack(Ease{})
			test.edit(&track)
			if _, err := p.SampleTrack("out", clock, track); err == nil || len(p.Signals) != 1 {
				t.Fatalf("invalid track mutated program or succeeded: %v", err)
			}
		})
	}
	p := NewProgram("bad")
	if _, err := p.SampleTrack("out", "", scalarTrack(Ease{})); err == nil {
		t.Fatal("accepted missing input")
	}
	if _, err := p.SampleTrack(" ", "clock", scalarTrack(Ease{})); err == nil {
		t.Fatal("accepted empty id")
	}
	if _, err := (*Program)(nil).SampleTrack("out", "clock", scalarTrack(Ease{})); err == nil {
		t.Fatal("accepted nil program")
	}
	p.Signals = []SignalSpec{{ID: "out", Kind: SignalTrack, Input: "missing", Frames: []MotionKeyframe{{At: 0, Value: 0.0}, {At: 1, Value: 1.0}}}}
	if _, err := p.Marshal(); err == nil || !strings.Contains(err.Error(), "unknown signal") {
		t.Fatalf("missing input: %v", err)
	}
	p.Signals[0].Frames[0].Value = "oops"
	if _, err := p.Marshal(); err == nil || !strings.Contains(err.Error(), "finite scalar") {
		t.Fatalf("invalid wire track: %v", err)
	}
}
