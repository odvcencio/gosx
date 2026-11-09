package replayui

import (
	_ "embed"
	"fmt"
	"strconv"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/game/replay"
)

//go:embed replay.css
var css string

// CSS returns the stylesheet text. It uses the same --kit-* variables as
// kitui.CSS, so it needs no theme of its own.
func CSS() string { return css }

// Styles returns a <style> element with the replay stylesheet. Put it after
// kitui.Styles().
func Styles() gosx.Node {
	return gosx.El("style", gosx.Attrs(gosx.Attr("id", "gosx-replay-style")), gosx.RawHTML(css))
}

// CameraOption is one choice in the camera picker.
type CameraOption struct {
	ID     string // for Controller.SetCamera and data-kit-value
	Label  string
	Active bool
}

// MarkerItem is one entry of the marker list.
type MarkerItem struct {
	Tick  uint64
	Label string
	Time  string // formatted clip time
}

// View is everything the Bar shows, as plain values.
type View struct {
	Tick, Start, End uint64
	Paused           bool
	Speed            float64
	Time, Length     string // formatted "m:ss.t"
	Cameras          []CameraOption
	Markers          []MarkerItem
}

// FormatTime formats tick as minutes, seconds and tenths since start at hz
// ticks per second.
func FormatTime(tick, start uint64, hz float64) string {
	if hz <= 0 {
		hz = 60
	}
	if tick < start {
		tick = start
	}
	tenths := int(float64(tick-start) / hz * 10)
	return fmt.Sprintf("%d:%02d.%d", tenths/600, tenths/10%60, tenths%10)
}

// View snapshots the controller for the Bar.
func (c *Controller) View() View {
	p := c.cfg.Player
	rec := p.Recording()
	hz := c.tickRate()
	v := View{
		Tick: p.Tick(), Start: p.Start(), End: p.End(), Paused: p.Paused(), Speed: p.Speed(),
		Time: FormatTime(p.Tick(), p.Start(), hz), Length: FormatTime(p.End(), p.Start(), hz),
	}
	active := c.ActiveCamera()
	for _, id := range c.camOrder {
		label := id
		switch {
		case id == CamChase:
			label = "Follow"
		case id == CamOrbit:
			label = "Orbit"
		case id == CamFly:
			label = "Fly"
		case len(id) > 5 && id[:5] == "shot:":
			label = id[5:]
		}
		v.Cameras = append(v.Cameras, CameraOption{ID: id, Label: label, Active: id == active})
	}
	for _, m := range rec.Markers() {
		if m.Tick < p.Start() || m.Tick > p.End() {
			continue
		}
		v.Markers = append(v.Markers, MarkerItem{Tick: m.Tick, Label: m.Label, Time: FormatTime(m.Tick, p.Start(), hz)})
	}
	return v
}

func btn(label, action, value string, extra ...any) gosx.Node {
	a := []any{gosx.Attr("type", "button"), gosx.Attr("class", "kit-button"), gosx.Attr("data-kit-action", action)}
	if value != "" {
		a = append(a, gosx.Attr("data-kit-value", value))
	}
	a = append(a, extra...)
	return gosx.El("button", gosx.Attrs(a...), gosx.Text(label))
}

func group(label, class string, children ...gosx.Node) gosx.Node {
	args := []any{gosx.Attrs(gosx.Attr("class", "kit-replay-group "+class), gosx.Attr("role", "radiogroup"), gosx.Attr("aria-label", label))}
	for _, c := range children {
		args = append(args, c)
	}
	return gosx.El("div", args...)
}

func choice(label, action, value string, on bool) gosx.Node {
	return btn(label, action, value, gosx.Attr("role", "radio"), gosx.Attr("aria-checked", strconv.FormatBool(on)),
		gosx.Attr("data-kit-selected", on))
}

// speedLabel formats 0.25 as "0.25x" and 2 as "2x".
func speedLabel(s float64) string { return strconv.FormatFloat(s, 'f', -1, 64) + "x" }

// Bar renders the replay controls: transport buttons, scrubber, speed picker,
// camera picker and marker list. It is not modal and traps no focus. Replace
// the bar's container HTML with kitui.Render when View changes.
func Bar(v View) gosx.Node {
	play := "Play"
	if !v.Paused {
		play = "Pause"
	}
	transport := gosx.El("div", gosx.Attrs(gosx.Attr("class", "kit-replay-transport")),
		btn("|<", "replay.restart", "", gosx.Attr("aria-label", "Restart")),
		btn("<", "replay.step_back", "", gosx.Attr("aria-label", "Step back one tick")),
		btn(play, "replay.toggle", "", gosx.Attr("data-kit-primary", true), gosx.Attr("aria-label", play)),
		btn(">", "replay.step_forward", "", gosx.Attr("aria-label", "Step forward one tick")),
	)
	scrub := gosx.El("div", gosx.Attrs(gosx.Attr("class", "kit-replay-scrub")),
		gosx.El("input", gosx.Attrs(gosx.Attr("type", "range"), gosx.Attr("id", "kit-replay-scrub"), gosx.Attr("min", v.Start),
			gosx.Attr("max", v.End), gosx.Attr("step", 1), gosx.Attr("value", v.Tick),
			gosx.Attr("aria-label", "Replay position"),
			gosx.Attr("aria-valuetext", v.Time+" of "+v.Length),
			gosx.Attr("data-kit-setting", "replay.scrub"))),
		gosx.El("span", gosx.Attrs(gosx.Attr("class", "kit-replay-time"), gosx.Attr("aria-hidden", "true")), gosx.Text(v.Time+" / "+v.Length)),
	)
	var speeds []gosx.Node
	for _, s := range replay.Speeds {
		speeds = append(speeds, choice(speedLabel(s), "replay.speed", strconv.FormatFloat(s, 'f', -1, 64), s == v.Speed))
	}
	var cams []gosx.Node
	for _, c := range v.Cameras {
		cams = append(cams, choice(c.Label, "replay.camera", c.ID, c.Active))
	}
	root := []any{gosx.Attrs(gosx.Attr("class", "kit-replay"), gosx.Attr("id", "kit-replay"), gosx.Attr("role", "group"),
		gosx.Attr("aria-label", "Replay controls")), transport, scrub, group("Playback speed", "kit-replay-speed", speeds...)}
	if len(cams) > 0 {
		root = append(root, group("Camera", "kit-replay-cams", cams...))
	}
	if len(v.Markers) > 0 {
		items := []any{gosx.Attrs(gosx.Attr("class", "kit-replay-markers"), gosx.Attr("aria-label", "Highlights"))}
		for _, m := range v.Markers {
			items = append(items, gosx.El("li", btn(m.Time+"  "+m.Label, "replay.marker", strconv.FormatUint(m.Tick, 10))))
		}
		root = append(root, gosx.El("ol", items...))
	}
	return gosx.El("section", root...)
}
