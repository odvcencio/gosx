package replayui_test

import (
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/camera"
	"m31labs.dev/gosx/game/replay"
	"m31labs.dev/gosx/game/replay/replayui"
	"m31labs.dev/gosx/room/roomtest"
	"m31labs.dev/gosx/sim/simcore"
)

const frame = time.Second / 60

func newGame() *roomtest.Game { return roomtest.NewGame(2, 4, 3, 60) }

type rig struct {
	g    *roomtest.Game
	ctl  *replayui.Controller
	in   *actions.Map
	pl   *replay.Player
	hash []uint64
}

func setup(t *testing.T) *rig {
	t.Helper()
	live := newGame()
	rec := replay.NewRecorder(live, replay.Config{Interval: 30, TickRate: 60})
	hashes := []uint64{replay.Hash(live)}
	for i := 0; i < 600; i++ {
		rec.Tick(map[string]simcore.Input{"p0": {Data: []byte{byte(i % 16)}}})
		hashes = append(hashes, replay.Hash(live))
		if i == 199 || i == 399 {
			rec.Mark(map[bool]string{true: "first", false: "second"}[i == 199])
		}
	}
	g := newGame()
	pl, err := replay.NewPlayer(g, rec.Recording())
	if err != nil {
		t.Fatal(err)
	}
	cam := replay.NewRig(replay.RigConfig{
		Subject: func(float64) (camera.Vec3, bool) { return camera.Vec3{X: float64(g.World.Bodies[0].X) / 1000}, true },
		Shots:   map[string]camera.Pose{"high": {Position: camera.Vec3{Y: 30, Z: 1}}},
	})
	in := actions.NewMap()
	in.Define(replayui.Context())
	in.Push(replayui.ContextName)
	ctl := replayui.New(replayui.Config{Player: pl, Rig: cam, Input: in})
	return &rig{g: g, ctl: ctl, in: in, pl: pl, hash: hashes}
}

// tap presses, runs one frame, and releases.
func (r *rig) tap(src actions.Source) {
	r.in.Feed(actions.Event{Source: src, Value: 1})
	r.in.Update(frame)
	r.ctl.Update(frame)
	r.in.Feed(actions.Event{Source: src, Value: 0})
	r.in.Update(frame)
	r.ctl.Update(frame)
}

func TestKeyboardPlayPauseStepSpeed(t *testing.T) {
	r := setup(t)
	if !r.pl.Paused() {
		t.Fatal("starts paused")
	}
	r.tap(actions.Key("Space"))
	if r.pl.Paused() {
		t.Fatal("Space should play")
	}
	for i := 0; i < 30; i++ {
		r.in.Update(frame)
		r.ctl.Update(frame)
	}
	if r.pl.Tick() < 25 {
		t.Fatalf("playing advanced to %d", r.pl.Tick())
	}
	r.tap(actions.Key("Space"))
	if !r.pl.Paused() {
		t.Fatal("Space should pause")
	}
	at := r.pl.Tick()
	r.tap(actions.Key("Period"))
	r.tap(actions.Key("Period"))
	if r.pl.Tick() != at+2 || replay.Hash(r.g) != r.hash[at+2] {
		t.Fatalf("step forward: tick %d", r.pl.Tick())
	}
	r.tap(actions.Key("Comma"))
	if r.pl.Tick() != at+1 || replay.Hash(r.g) != r.hash[at+1] {
		t.Fatalf("step back: tick %d", r.pl.Tick())
	}
	r.tap(actions.Key("Equal"))
	if r.pl.Speed() != 2 {
		t.Fatalf("speed up: %v", r.pl.Speed())
	}
	r.tap(actions.Key("Minus"))
	r.tap(actions.Key("Minus"))
	r.tap(actions.Key("Minus"))
	r.tap(actions.Key("Minus"))
	if r.pl.Speed() != 0.25 {
		t.Fatalf("speed floor: %v", r.pl.Speed())
	}
	r.tap(actions.Key("Home"))
	if r.pl.Tick() != 0 {
		t.Fatal("Home should restart")
	}
}

func TestGamepadScrubAndMarkers(t *testing.T) {
	r := setup(t)
	// Hold the right trigger: scrubbing forward at 6x real time.
	r.in.Feed(actions.Event{Source: actions.PadButton(7), Value: 1})
	for i := 0; i < 30; i++ { // 0.5 s -> about 3 s of replay = 180 ticks
		r.in.Update(frame)
		r.ctl.Update(frame)
	}
	r.in.Feed(actions.Event{Source: actions.PadButton(7), Value: 0})
	r.in.Update(frame)
	got := r.pl.Tick()
	if got < 150 || got > 200 {
		t.Fatalf("trigger scrub reached %d", got)
	}
	if replay.Hash(r.g) != r.hash[got] {
		t.Fatal("scrubbed state differs from live")
	}
	r.ctl.SeekTick(0)
	// Left shoulder / right shoulder jump between markers (lead 2 s = 120 ticks).
	r.tap(actions.PadButton(5))
	if r.pl.Tick() != 80 {
		t.Fatalf("next marker landed on %d, want 80", r.pl.Tick())
	}
	r.tap(actions.PadButton(5))
	if r.pl.Tick() != 280 {
		t.Fatalf("second marker landed on %d, want 280", r.pl.Tick())
	}
	r.tap(actions.PadButton(4))
	if r.pl.Tick() != 80 {
		t.Fatalf("previous marker landed on %d, want 80", r.pl.Tick())
	}
	if replay.Hash(r.g) != r.hash[80] {
		t.Fatal("marker jump state differs from live")
	}
	// A pad button toggles play too.
	r.tap(actions.PadButton(actions.PadA))
	if r.pl.Paused() {
		t.Fatal("pad A should play")
	}
}

func TestCameraSwitching(t *testing.T) {
	r := setup(t)
	if r.ctl.ActiveCamera() != replayui.CamChase {
		t.Fatalf("camera %q", r.ctl.ActiveCamera())
	}
	r.tap(actions.Key("KeyC"))
	if r.ctl.ActiveCamera() != "shot:high" {
		t.Fatalf("after C: %q", r.ctl.ActiveCamera())
	}
	r.tap(actions.Key("KeyC"))
	if r.ctl.ActiveCamera() != replayui.CamOrbit {
		t.Fatalf("after C C: %q", r.ctl.ActiveCamera())
	}
	r.tap(actions.Key("KeyV"))
	if r.ctl.ActiveCamera() != "shot:high" {
		t.Fatalf("after V: %q", r.ctl.ActiveCamera())
	}
	r.ctl.SetCamera(replayui.CamChase)
	r.tap(actions.Key("KeyF"))
	if r.ctl.ActiveCamera() != replayui.CamOrbit {
		t.Fatal("F should enter orbit")
	}
	r.tap(actions.Key("KeyF"))
	if r.ctl.ActiveCamera() != replayui.CamFly {
		t.Fatal("F should enter fly")
	}
	// Hold W: the fly camera moves.
	f0 := r.ctl.Update(frame)
	for i := 0; i < 60; i++ {
		r.in.Update(frame)
		r.ctl.Update(frame)
	}
	r.in.Feed(actions.Event{Source: actions.Key("KeyW"), Value: 1})
	var f1 replayui.Frame
	for i := 0; i < 60; i++ {
		r.in.Update(frame)
		f1 = r.ctl.Update(frame)
	}
	if f1.Pose.Position.Sub(f0.Pose.Position).Len() < 3 {
		t.Fatalf("W did not move the fly camera: %+v -> %+v", f0.Pose.Position, f1.Pose.Position)
	}
	if f1.Camera.FOV <= 0 {
		t.Fatal("scene camera not filled")
	}
}

func TestSeekedFrameFlag(t *testing.T) {
	r := setup(t)
	r.ctl.SeekTick(300)
	if f := r.ctl.Update(frame); !f.Seeked {
		t.Fatal("seek not reported")
	}
	if f := r.ctl.Update(frame); f.Seeked {
		t.Fatal("seek reported twice")
	}
}

func TestBarRendersAndHandlesEvents(t *testing.T) {
	r := setup(t)
	html := gosx.RenderHTML(replayui.Bar(r.ctl.View()))
	for _, want := range []string{
		`role="group"`, `aria-label="Replay controls"`,
		`data-kit-action="replay.toggle"`, `data-kit-action="replay.step_back"`, `data-kit-action="replay.step_forward"`,
		`type="range"`, `data-kit-setting="replay.scrub"`, `max="600"`, `aria-valuetext="0:00.0 of 0:10.0"`,
		`data-kit-action="replay.speed"`, `data-kit-value="0.25"`, `>4x<`,
		`data-kit-action="replay.camera"`, `data-kit-value="shot:high"`, `data-kit-value="orbit"`, `role="radio"`,
		`data-kit-action="replay.marker"`, `data-kit-value="200"`, `0:03.3  first`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in\n%s", want, html)
		}
	}
	if !strings.Contains(html, `>Play<`) {
		t.Error("paused bar should offer Play")
	}

	// Events from the bar drive the same controller.
	for _, c := range []struct{ name, value string }{
		{"replay.toggle", ""}, {"replay.speed", "4"}, {"replay.camera", "orbit"}, {"replay.marker", "400"},
	} {
		if !r.ctl.Handle(c.name, c.value) {
			t.Fatalf("%s not handled", c.name)
		}
	}
	if r.pl.Paused() || r.pl.Speed() != 4 || r.ctl.ActiveCamera() != replayui.CamOrbit || r.pl.Tick() != 400 {
		t.Fatalf("state after events: paused=%v speed=%v cam=%q tick=%d", r.pl.Paused(), r.pl.Speed(), r.ctl.ActiveCamera(), r.pl.Tick())
	}
	if !r.ctl.HandleSetting("replay.scrub", "123") || r.pl.Tick() != 123 || replay.Hash(r.g) != r.hash[123] {
		t.Fatalf("scrub to 123 ended at %d", r.pl.Tick())
	}
	if r.ctl.HandleSetting("volume.music", "3") || r.ctl.Handle("resume", "") {
		t.Error("foreign events should not be handled")
	}
	html = gosx.RenderHTML(replayui.Bar(r.ctl.View()))
	for _, want := range []string{`>Pause<`, `value="123"`} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q after events", want)
		}
	}
	if !strings.Contains(html, `aria-checked="true" class="kit-button" data-kit-action="replay.camera" data-kit-selected="true" data-kit-value="orbit"`) &&
		!strings.Contains(html, `data-kit-selected="true"`) {
		t.Error("active camera not marked")
	}
}

func TestContextRebindable(t *testing.T) {
	m := actions.NewMap()
	m.Define(replayui.Context())
	if bs := m.Bindings(replayui.ContextName, replayui.ActToggle); len(bs) != 3 {
		t.Fatalf("toggle has %d bindings", len(bs))
	}
}
