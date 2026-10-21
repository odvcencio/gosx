package scene

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"reflect"
	"testing"
	"time"

	"m31labs.dev/gosx/motion"
)

var updateTimelineMotion = flag.Bool("update-timeline-motion", false, "rewrite native scene timeline / motion program parity fixtures")

type timelineMotionSample struct {
	Seconds  float64   `json:"seconds"`
	Commands []Command `json:"commands"`
}

type timelineMotionFixture struct {
	Name    string                 `json:"name"`
	Offset  float64                `json:"offset,omitempty"`
	Plan    *Timeline              `json:"plan"`
	Program *motion.Program        `json:"program"`
	Samples []timelineMotionSample `json:"samples"`
	Final   []Command              `json:"final"`
}

func TestTimelineMotionGolden(t *testing.T) {
	placement := placementTimeline()
	// Deliberately unsorted authoring order; Then still follows the latest end.
	placement.Tweens[0], placement.Tweens[3] = placement.Tweens[3], placement.Tweens[0]
	plans := []*Timeline{
		placement,
		NewTimeline("cuts").At(time.Second, Tween{Node: "piece", Property: "x", From: 1e20, To: 1, Duration: time.Second}).
			At(3*time.Second, Tween{Node: "piece", Property: "x", From: 7, To: 9}).
			At(4*time.Second, Tween{Node: "piece", Property: "x", From: 2, To: 4, Duration: time.Second}).
			Then(Tween{Node: "piece", Property: "x", From: -3, To: -1, Duration: time.Second}),
		NewTimeline("instant").At(0, Tween{Node: "piece", Property: "y", From: 9, To: -2}),
		NewTimeline("late-nanosecond").At(23*time.Hour, Tween{Camera: true, Property: "x", From: 0, To: 1, Duration: time.Nanosecond}),
	}
	properties := NewTimeline("properties")
	for _, camera := range []bool{false, true} {
		for i, property := range []string{"x", "y", "z", "rotationX", "rotationY", "rotationZ", "scaleX", "scaleY", "scaleZ", "opacity", "fov"} {
			if camera && i >= 6 && property != "fov" || !camera && property == "fov" {
				continue
			}
			node := "piece"
			if camera {
				node = ""
			}
			properties.At(time.Second, Tween{Node: node, Camera: camera, Property: property, From: float64(i), To: float64(i + 2), Duration: time.Second})
		}
	}
	plans = append(plans, properties)
	linear := NewTimeline("long-linear")
	windowed := NewTimeline("windowed")
	for i := 0; i < 64; i++ {
		linear.At(time.Duration(i)*time.Second, Tween{Node: "piece", Property: "x", From: float64(i), To: float64(i + 1), Duration: time.Second})
		if i >= 32 {
			continue
		}
		duration := time.Duration(1+i%3) * 250 * time.Millisecond
		if i%5 == 0 {
			duration = 0
		}
		windowed.At(time.Duration(i)*2*time.Second,
			Tween{Node: "piece", Property: "x", From: float64(-i - 3), To: float64(i + 2), Duration: duration, Ease: motion.Ease{Kind: motion.EaseOutPow, Args: []float64{2}}},
			Tween{Camera: true, Property: "z", From: float64(i + 4), To: float64(i + 1), Duration: duration, Ease: motion.Ease{Kind: motion.EaseInOutBack}},
		)
		if i%2 == 0 {
			windowed.At(time.Duration(i)*2*time.Second, Tween{Node: "other", Property: "rotationY", From: float64(i), To: float64(i) + .5, Duration: time.Second})
		}
	}
	// Interleave targets and author in reverse order to exercise grouping/sorting.
	for i, j := 0, len(windowed.Tweens)-1; i < j; i, j = i+1, j-1 {
		windowed.Tweens[i], windowed.Tweens[j] = windowed.Tweens[j], windowed.Tweens[i]
	}
	plans = append(plans, linear, windowed)
	for i, ease := range []motion.Ease{
		{}, {Kind: motion.EaseInPow, Args: []float64{3}}, {Kind: motion.EaseOutPow, Args: []float64{3}},
		{Kind: motion.EaseInOutPow, Args: []float64{2}}, {Kind: motion.EaseCubicBezier, Args: []float64{.22, 1, .36, 1}},
		{Kind: motion.EaseSteps, Args: []float64{4}}, {Kind: motion.EaseOutBack}, {Kind: motion.EaseInOutBack},
	} {
		plans = append(plans, NewTimeline(fmt.Sprintf("ease-%d", i)).At(time.Second, Tween{Node: "piece", Property: "z", From: 2, To: -4, Duration: time.Second, Ease: ease}))
	}
	var fixtures []timelineMotionFixture
	for _, plan := range plans {
		p := motion.NewProgram(plan.ID).WithTransport(motion.TransportOptions{Duration: 24 * time.Hour})
		clock := p.Playhead("clock")
		offset := 0.0
		if plan.ID == "windowed" {
			offset = -2
			clock = p.Map("timeline-clock", clock, 0, 1, offset, offset+1)
		}
		if err := plan.BindMotion(p, clock, "#scene"); err != nil {
			t.Fatal(err)
		}
		f := timelineMotionFixture{Name: plan.ID, Offset: offset, Plan: plan, Program: p}
		times := []float64{0, .05, .1, .2, .4, .5, .55, .6, .65, .7, 1, 1.1, 1.5, 1.999999, 2, 2.5, 2.999999, 3, 3.5, 4, 4.5, 5, 5.5, 6, 7, 3, 1.25, 0}
		if plan.ID == "late-nanosecond" {
			start := (23 * time.Hour).Seconds()
			times = []float64{0, start - 1e-9, start, math.Nextafter(start, math.Inf(1)), start + 1e-9, start + 1, start}
		}
		if plan.ID == "long-linear" || plan.ID == "windowed" {
			times = []float64{offset, 0, 1e-9}
			for _, tween := range plan.Tweens {
				start, end := tween.At.Seconds(), (tween.At + tween.Duration).Seconds()
				times = append(times, start-1e-6, start, start+1e-6, (start+end)/2, end, end+.125)
			}
			// Arbitrary backward seeks cross multiple inactive clock windows.
			for i := len(times) - 1; i >= 0; i-- {
				times = append(times, times[i])
			}
		}
		player, err := NewTimelinePlayer(plan)
		if err != nil {
			t.Fatal(err)
		}
		for _, at := range times {
			commands, err := player.Sample(at)
			if err != nil {
				t.Fatal(err)
			}
			f.Samples = append(f.Samples, timelineMotionSample{Seconds: at, Commands: commands})
		}
		f.Final, err = player.Sample(86400)
		if err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, f)
	}
	var ordinary, windows []timelineMotionFixture
	for _, fixture := range fixtures {
		if fixture.Name == "long-linear" || fixture.Name == "windowed" {
			windows = append(windows, fixture)
		} else {
			ordinary = append(ordinary, fixture)
		}
	}
	for _, corpus := range []struct {
		path     string
		fixtures []timelineMotionFixture
		compact  bool
	}{{"testdata/timeline_motion.json", ordinary, false}, {"testdata/timeline_motion_windows.json", windows, true}} {
		data, err := json.Marshal(corpus.fixtures)
		if !corpus.compact {
			data, err = json.MarshalIndent(corpus.fixtures, "", "  ")
		}
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, '\n')
		if *updateTimelineMotion {
			if err := os.WriteFile(corpus.path, data, 0644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(corpus.path)
		if err != nil || string(want) != string(data) {
			t.Fatalf("native scene motion fixtures differ; rerun with -update-timeline-motion: %s: %v", corpus.path, err)
		}
	}
}

func TestTimelineBindMotionOwnsInputAndIsDeterministic(t *testing.T) {
	plan := placementTimeline()
	plan.Tweens[0].Ease.Args = []float64{2}
	before, _ := json.Marshal(plan)
	compile := func() []byte {
		p := motion.NewProgram("story").WithTransport(motion.TransportOptions{Duration: time.Second})
		if err := plan.BindMotion(p, p.Playhead("clock"), " #scene "); err != nil {
			t.Fatal(err)
		}
		for _, binding := range p.Bindings {
			if binding.Selector != "#scene" {
				t.Fatal(binding)
			}
		}
		data, err := p.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if a, b := compile(), compile(); string(a) != string(b) {
		t.Fatal("compilation is not deterministic")
	}
	after, _ := json.Marshal(plan)
	if string(before) != string(after) {
		t.Fatal("compilation mutated the plan")
	}
	p := motion.NewProgram("copy")
	if err := plan.BindMotion(p, p.Time("clock"), "#scene"); err != nil {
		t.Fatal(err)
	}
	compiled, _ := p.Marshal()
	plan.Tweens[0].Ease.Args[0], plan.Tweens[0].To = 99, 99
	current, _ := p.Marshal()
	if string(compiled) != string(current) {
		t.Fatal("compiled program aliases plan values or easing arguments")
	}
}

func TestTimelineBindMotionRejectsAtomically(t *testing.T) {
	for _, name := range []string{"nil plan", "empty plan", "bad plan", "blank id", "empty input", "unknown input", "vector input", "empty selector", "collision", "bad program"} {
		t.Run(name, func(t *testing.T) {
			p := motion.NewProgram("story")
			p.Signals = make([]motion.SignalSpec, 0, 32)
			p.Bindings = make([]motion.Binding, 0, 16)
			input, selector, plan := p.Time("clock"), "#scene", placementTimeline()
			switch name {
			case "nil plan":
				plan = nil
			case "empty plan":
				plan.Tweens = nil
			case "bad plan":
				plan.Tweens[0].Duration = -1
			case "blank id":
				plan.ID = " "
			case "empty input":
				input = " "
			case "unknown input":
				input = "missing"
			case "vector input":
				var err error
				input, err = p.SampleTrack("vector", input, motion.Track{Keys: []motion.Key{{T: 0, Value: motion.Vec2V(0, 0)}, {T: 1, Value: motion.Vec2V(1, 1)}}})
				if err != nil {
					t.Fatal(err)
				}
			case "empty selector":
				selector = " "
			case "collision":
				p.Time("place.motion.0.time")
			case "bad program":
				p.BindStyle(input, ".title", "__proto__", "")
			}
			before, _ := json.Marshal(p)
			allSignals := append([]motion.SignalSpec(nil), p.Signals[:cap(p.Signals)]...)
			allBindings := append([]motion.Binding(nil), p.Bindings[:cap(p.Bindings)]...)
			if err := plan.BindMotion(p, input, selector); err == nil {
				t.Fatal("invalid compilation succeeded")
			}
			after, _ := json.Marshal(p)
			if string(before) != string(after) || !reflect.DeepEqual(allSignals, p.Signals[:cap(p.Signals)]) || !reflect.DeepEqual(allBindings, p.Bindings[:cap(p.Bindings)]) {
				t.Fatal("failed compilation mutated program or backing slices")
			}
		})
	}
	if err := placementTimeline().BindMotion(nil, "clock", "#scene"); err == nil {
		t.Fatal("nil program accepted")
	}
}

func TestTimelineBindMotionSupportsMaximumSequence(t *testing.T) {
	plan := NewTimeline("long")
	for i := 0; i < 1024; i++ {
		plan.At(time.Duration(i)*time.Millisecond, Tween{Node: "piece", Property: "x", From: float64(i), To: float64(i + 1), Duration: time.Millisecond})
	}
	p := motion.NewProgram("long").WithTransport(motion.TransportOptions{Duration: plan.Duration()})
	if err := plan.BindMotion(p, p.Playhead("clock"), "#scene"); err != nil {
		t.Fatal(err)
	}
	if len(p.Bindings) != 1 {
		t.Fatal("expected one binding per target property")
	}
	if _, err := p.Marshal(); err != nil {
		t.Fatalf("maximum legal plan exceeds graph depth: %v", err)
	}
}

func TestTimelineBindMotionWindowCollisionIsAtomic(t *testing.T) {
	plan := NewTimeline("windows")
	for i := 0; i < 16; i++ {
		plan.At(time.Duration(i)*time.Second, Tween{Node: "piece", Property: "x", From: float64(i), To: float64(i + 1), Duration: time.Second})
	}
	compiled := motion.NewProgram("inspect")
	if err := plan.BindMotion(compiled, compiled.Time("clock"), "#scene"); err != nil {
		t.Fatal(err)
	}
	var windowID string
	for _, signal := range compiled.Signals {
		if signal.Kind == motion.SignalClamp {
			windowID = signal.ID
			break
		}
	}
	if windowID == "" {
		t.Fatal("long plan has no window clock")
	}
	p := motion.NewProgram("collision")
	p.Time(windowID)
	clock := p.Time("clock")
	before, _ := p.Marshal()
	if err := plan.BindMotion(p, clock, "#scene"); err == nil {
		t.Fatal("window clock collision accepted")
	}
	after, _ := p.Marshal()
	if string(before) != string(after) {
		t.Fatal("failed compilation changed the caller's program")
	}
}
