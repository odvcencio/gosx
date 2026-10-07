package scene

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/motion"
)

func placementTimeline() *Timeline {
	return NewTimeline("place").At(0,
		Tween{Node: "piece", Property: "y", From: 1, To: 0, Duration: 200 * time.Millisecond, Ease: motion.Ease{Kind: motion.EaseOutPow}},
		Tween{Camera: true, Property: "y", From: 4, To: 2, Duration: 400 * time.Millisecond},
	).Then(Tween{Node: "piece", Property: "scaleX", From: 1, To: 1.1, Duration: 100 * time.Millisecond}).
		At(600*time.Millisecond, Tween{Node: "piece", Property: "scaleX", From: 1.1, To: 1, Duration: 100 * time.Millisecond})
}

func TestTimelineWireAndSequencing(t *testing.T) {
	plan := placementTimeline()
	if plan.Duration() != 700*time.Millisecond {
		t.Fatal(plan.Duration())
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("testdata/timeline.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != strings.TrimSpace(string(fixture)) {
		t.Fatalf("browser fixture differs: %s", data)
	}
	var restored Timeline
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan, &restored) {
		t.Fatalf("round trip differs: %#v", restored)
	}
}

func TestTimelineSampleEasingHoldAndSeek(t *testing.T) {
	player, err := NewTimelinePlayer(placementTimeline())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ seconds, y, camera, scale float64 }{
		{-1, 1, 4, 1}, {0.1, 0.25, 3.5, 1}, {0.55, 0, 2, 1.1}, {0.65, 0, 2, 1.05}, {2, 0, 2, 1}, {0.05, 0.5625, 3.75, 1},
	} {
		commands, err := player.Sample(tc.seconds)
		if err != nil || len(commands) != 2 {
			t.Fatalf("sample %v: %v %v", tc.seconds, commands, err)
		}
		piece, camera := commands[0].Data.(map[string]float64), commands[1].Data.(map[string]float64)
		if commands[0].Kind != CommandSetTransform || commands[0].ObjectID != "piece" || commands[1].Kind != CommandSetCamera ||
			math.Abs(piece["y"]-tc.y) > 1e-12 || math.Abs(piece["scaleX"]-tc.scale) > 1e-12 || math.Abs(camera["y"]-tc.camera) > 1e-12 {
			t.Fatalf("sample %v: %v", tc.seconds, commands)
		}
		if len(piece) != 2 || len(camera) != 1 {
			t.Fatal("sampler must patch only authored properties")
		}
	}
}

func TestTimelineValidationAndAtomicDecode(t *testing.T) {
	for name, change := range map[string]func(*Timeline){
		"empty":        func(p *Timeline) { p.ID = "" },
		"property":     func(p *Timeline) { p.Tweens[0].Property = "__proto__" },
		"camera scale": func(p *Timeline) { p.Tweens[1].Property = "scaleX" },
		"target":       func(p *Timeline) { p.Tweens[0].Node = "" },
		"nonfinite":    func(p *Timeline) { p.Tweens[0].From = math.NaN() },
		"negative":     func(p *Timeline) { p.Tweens[0].Duration = -1 },
		"overflow":     func(p *Timeline) { p.Tweens[0].At = time.Duration(math.MaxInt64) },
		"overlap":      func(p *Timeline) { p.Tweens[3].At = 450 * time.Millisecond },
		"same start":   func(p *Timeline) { p.Tweens[3].At = p.Tweens[2].At },
		"unknown ease": func(p *Timeline) { p.Tweens[0].Ease.Kind = 99 },
		"ease args":    func(p *Timeline) { p.Tweens[0].Ease.Args = []float64{-1} },
		"bezier": func(p *Timeline) {
			p.Tweens[0].Ease = motion.Ease{Kind: motion.EaseCubicBezier, Args: []float64{2, 0, 1, 1}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			plan := placementTimeline()
			change(plan)
			if _, err := json.Marshal(plan); err == nil {
				t.Fatal("invalid plan accepted")
			}
		})
	}
	plan := placementTimeline()
	for _, data := range []string{`{"version":2}`, `{"version":1,"id":"bad","tweens":[{"duration":1e99}]}`} {
		if err := json.Unmarshal([]byte(data), plan); err == nil || plan.ID != "place" {
			t.Fatal("invalid decode changed the receiver")
		}
	}
	if _, err := plan.Sample(math.Inf(1)); err == nil {
		t.Fatal("non-finite sample accepted")
	}
}

func TestTimelinePlayerOwnsPreparedTracks(t *testing.T) {
	plan := placementTimeline()
	plan.Tweens[0].Ease.Args = []float64{2}
	player, err := NewTimelinePlayer(plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.Tweens[0].Ease.Args[0] = 1
	plan.Tweens[0].To = 99
	commands, _ := player.Sample(0.1)
	if commands[0].Data.(map[string]float64)["y"] != 0.25 {
		t.Fatal("prepared tracks alias the source")
	}
}
