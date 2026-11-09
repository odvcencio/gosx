package serve

import (
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/sim"
)

func TestNewRunnerTicksOncePerFixedStep(t *testing.T) {
	cases := []struct {
		step time.Duration
		want int
	}{
		{time.Second / 60, 60},
		{time.Second / 30, 30},
		{10 * time.Millisecond, 100},
	}
	for _, c := range cases {
		runner := NewRunner(hub.New("rate"), game.New(game.Config{FixedStep: c.step}), sim.Options{})
		if got := runner.TickRate(); got != c.want {
			t.Errorf("step %v: tick rate %d, want %d", c.step, got, c.want)
		}
	}
	if got := NewRunner(hub.New("explicit"), nil, sim.Options{TickRate: 20}).TickRate(); got != 20 {
		t.Errorf("explicit tick rate %d, want 20", got)
	}
}

type recordingMount struct{ cfg engine.Config }

func (m *recordingMount) Engine(cfg engine.Config, fallback gosx.Node) gosx.Node {
	m.cfg = cfg
	return fallback
}

func TestMountPassesTheRuntimeEngineConfig(t *testing.T) {
	rt := game.New(game.Config{Name: "Arena"})
	target := &recordingMount{}
	fallback := gosx.Text("loading")
	if got := gosx.RenderHTML(Mount(rt, target, fallback)); got != "loading" {
		t.Fatalf("Mount returned %q, want the target's node", got)
	}
	if target.cfg.Name != "Arena" || target.cfg.MountAttrs["data-gosx-game"] != true {
		t.Fatalf("engine config = %+v, want the runtime's config", target.cfg)
	}
	var none EngineMount
	if got := gosx.RenderHTML(Mount(rt, none, fallback)); got != "loading" {
		t.Fatal("a nil target must return the fallback")
	}
}
