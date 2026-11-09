// Package kitui renders the screens every game needs (pause menu, settings,
// key rebinding, loading, error) as GoSX nodes.
//
// The output is plain HTML with data attributes and ARIA roles, so it works
// in a static export: render it at build time, or call gosx.RenderHTML in the
// browser. It has no hidden state. Colors, sizes and fonts come from CSS
// variables (--kit-*); see Styles.
//
// Interaction uses one DOM event. The small script from Script() turns clicks
// and input changes on kit elements into a bubbling "gosx:kit" CustomEvent
// with detail {kind, name, value}. In a js/wasm build, Bind forwards those to
// Go handlers. ApplySetting maps a settings control to a settings.Manager.
package kitui

import (
	_ "embed"
	"fmt"
	"strconv"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/boot"
	"m31labs.dev/gosx/game/settings"
)

//go:embed kit.css
var css string

//go:embed events.js
var script string

// CSS returns the stylesheet text.
func CSS() string { return css }

// ScriptSource returns the event script text.
func ScriptSource() string { return script }

// Styles returns a <style> element with the kit stylesheet.
func Styles() gosx.Node {
	return gosx.El("style", gosx.Attrs(gosx.Attr("id", "gosx-kit-style")), gosx.RawHTML(css))
}

// Script returns the <script> element with the event script.
func Script() gosx.Node {
	return gosx.El("script", gosx.Attrs(gosx.Attr("id", "gosx-kit-events")), gosx.RawHTML(script))
}

func button(label, action string, attrs ...any) gosx.Node {
	args := []any{gosx.Attrs(append([]any{gosx.Attr("type", "button"), gosx.Attr("class", "kit-button"), gosx.Attr("data-kit-action", action)}, attrs...)...), gosx.Text(label)}
	return gosx.El("button", args...)
}

func overlay(id, titleID string, hidden bool, children ...gosx.Node) gosx.Node {
	panel := []any{gosx.Attrs(gosx.Attr("class", "kit-panel"), gosx.Attr("role", "dialog"), gosx.Attr("aria-modal", "true"),
		gosx.Attr("aria-labelledby", titleID), gosx.Attr("data-kit-modal", "true"))}
	for _, c := range children {
		panel = append(panel, c)
	}
	attrs := []any{gosx.Attr("class", "kit-overlay"), gosx.Attr("id", id)}
	if hidden {
		attrs = append(attrs, gosx.Attr("hidden", true))
	}
	return gosx.El("div", gosx.Attrs(attrs...), gosx.El("div", panel...))
}

func title(id, text string) gosx.Node {
	return gosx.El("h2", gosx.Attrs(gosx.Attr("class", "kit-title"), gosx.Attr("id", id)), gosx.Text(text))
}

// MenuItem is one button of a menu. Selecting it sends action ID.
type MenuItem struct {
	ID       string
	Label    string
	Disabled bool
	Primary  bool
	Danger   bool
}

// PauseProps configures PauseMenu.
type PauseProps struct {
	// Title defaults to "Paused".
	Title string
	// Items default to Resume, Settings and Quit (actions "resume",
	// "settings", "quit").
	Items []MenuItem
	// Hidden renders the overlay with the hidden attribute, so a script can
	// show it by removing the attribute.
	Hidden bool
}

// PauseMenu renders a modal pause menu.
func PauseMenu(p PauseProps) gosx.Node {
	if p.Title == "" {
		p.Title = "Paused"
	}
	if len(p.Items) == 0 {
		p.Items = []MenuItem{{ID: "resume", Label: "Resume", Primary: true}, {ID: "settings", Label: "Settings"}, {ID: "quit", Label: "Quit", Danger: true}}
	}
	lis := make([]gosx.Node, 0, len(p.Items))
	for i, it := range p.Items {
		var attrs []any
		if it.Disabled {
			attrs = append(attrs, gosx.Attr("disabled", true))
		}
		if it.Primary {
			attrs = append(attrs, gosx.Attr("data-kit-primary", true))
		}
		if it.Danger {
			attrs = append(attrs, gosx.Attr("data-kit-danger", true))
		}
		if i == 0 {
			attrs = append(attrs, gosx.Attr("autofocus", true))
		}
		lis = append(lis, gosx.El("li", button(it.Label, it.ID, attrs...)))
	}
	menu := []any{gosx.Attrs(gosx.Attr("class", "kit-menu"))}
	for _, l := range lis {
		menu = append(menu, l)
	}
	return overlay("kit-pause", "kit-pause-title", p.Hidden, title("kit-pause-title", p.Title), gosx.El("ul", menu...))
}

// Option is a choice in a select.
type Option struct{ Value, Label string }

// SettingsProps configures SettingsPanel.
type SettingsProps struct {
	Settings  settings.Settings
	Languages []Option // default: the current language only
	// Sections hides parts: set a field to true to leave that part out.
	NoAudio, NoVideo, NoAccessibility, NoLanguage bool
	// Labels override the English text, keyed by control name such as
	// "volume.music" or by "title", "close", "reset".
	Labels map[string]string
}

var defaultLabels = map[string]string{
	"title": "Settings", "close": "Back", "reset": "Reset to defaults",
	"audio": "Audio", "video": "Video", "access": "Accessibility", "language": "Language",
	"volume.master": "Master volume", "volume.music": "Music", "volume.sfx": "Effects",
	"volume.voice": "Voice", "volume.ui": "Interface",
	"cameraShake": "Camera shake", "renderScale": "Render scale", "frameCap": "Frame cap", "quality": "Quality",
	"motion": "Reduce motion",
}

func label(labels map[string]string, key string) string {
	if v, ok := labels[key]; ok {
		return v
	}
	return defaultLabels[key]
}

func rangeField(l map[string]string, key string, min, max, step, value int, unit string) gosx.Node {
	id := "kit-" + key
	return gosx.El("div", gosx.Attrs(gosx.Attr("class", "kit-field")),
		gosx.El("label", gosx.Attrs(gosx.Attr("for", id)), gosx.Text(label(l, key))),
		gosx.El("input", gosx.Attrs(gosx.Attr("type", "range"), gosx.Attr("id", id), gosx.Attr("min", min), gosx.Attr("max", max),
			gosx.Attr("step", step), gosx.Attr("value", value), gosx.Attr("data-kit-setting", key), gosx.Attr("data-kit-unit", unit))),
		gosx.El("span", gosx.Attrs(gosx.Attr("class", "kit-value"), gosx.Attr("aria-hidden", "true")), gosx.Text(strconv.Itoa(value)+unit)),
	)
}

func selectField(l map[string]string, key string, opts []Option, current string) gosx.Node {
	id := "kit-" + key
	sel := []any{gosx.Attrs(gosx.Attr("id", id), gosx.Attr("data-kit-setting", key))}
	for _, o := range opts {
		a := []any{gosx.Attrs(gosx.Attr("value", o.Value))}
		if o.Value == current {
			a = []any{gosx.Attrs(gosx.Attr("value", o.Value), gosx.Attr("selected", true))}
		}
		sel = append(sel, gosx.El("option", append(a, gosx.Text(o.Label))...))
	}
	return gosx.El("div", gosx.Attrs(gosx.Attr("class", "kit-field")),
		gosx.El("label", gosx.Attrs(gosx.Attr("for", id)), gosx.Text(label(l, key))),
		gosx.El("span"), // keeps the grid columns aligned
		gosx.El("select", sel...),
	)
}

func section(l map[string]string, key string) gosx.Node {
	return gosx.El("h3", gosx.Attrs(gosx.Attr("class", "kit-section")), gosx.Text(label(l, key)))
}

func pct(v float64) int { return int(v*100 + 0.5) }

// FrameCaps are the choices in the frame cap select.
var FrameCaps = []Option{{"0", "Uncapped"}, {"30", "30 fps"}, {"60", "60 fps"}, {"90", "90 fps"}, {"120", "120 fps"}, {"144", "144 fps"}}

// Qualities are the choices in the quality select.
var Qualities = []Option{{"auto", "Auto"}, {"low", "Low"}, {"medium", "Medium"}, {"high", "High"}, {"ultra", "Ultra"}}

// MotionChoices are the choices in the reduce-motion select.
var MotionChoices = []Option{{"system", "Follow system"}, {"reduce", "On"}, {"full", "Off"}}

// SettingsPanel renders sliders and selects for the settings. Each control
// carries data-kit-setting="<key>"; pass the key and value to ApplySetting.
func SettingsPanel(p SettingsProps) gosx.Node {
	s, l := p.Settings, p.Labels
	body := []gosx.Node{title("kit-settings-title", label(l, "title"))}
	if !p.NoAudio {
		body = append(body, section(l, "audio"))
		for _, bus := range settings.Buses {
			body = append(body, rangeField(l, "volume."+bus, 0, 100, 1, pct(s.Volume.Get(bus)), "%"))
		}
	}
	if !p.NoVideo {
		body = append(body, section(l, "video"),
			rangeField(l, "renderScale", 25, 200, 5, pct(s.RenderScale), "%"),
			selectField(l, "frameCap", FrameCaps, strconv.Itoa(s.FrameCap)),
			selectField(l, "quality", Qualities, string(s.Quality)))
	}
	if !p.NoAccessibility {
		body = append(body, section(l, "access"),
			selectField(l, "motion", MotionChoices, string(s.Motion)),
			rangeField(l, "cameraShake", 0, 100, 5, pct(s.CameraShake), "%"))
	}
	if !p.NoLanguage {
		langs := p.Languages
		if len(langs) == 0 {
			langs = []Option{{s.Language, s.Language}}
		}
		body = append(body, section(l, "language"), selectField(l, "language", langs, s.Language))
	}
	body = append(body,
		button(label(l, "reset"), "settings-reset", gosx.Attr("data-kit-danger", true)),
		button(label(l, "close"), "settings-close", gosx.Attr("data-kit-primary", true)))
	return overlay("kit-settings", "kit-settings-title", false, body...)
}

// ApplySetting applies one control change from SettingsPanel to m. key is the
// data-kit-setting value and value is the control's value as a string. It
// returns the changed fields and any save error. An unknown key or a value
// that does not parse returns an error and changes nothing.
func ApplySetting(m *settings.Manager, key, value string) (settings.Field, error) {
	num := func() (float64, error) { return strconv.ParseFloat(value, 64) }
	switch {
	case len(key) > 7 && key[:7] == "volume.":
		v, err := num()
		if err != nil {
			return 0, err
		}
		bus := key[7:]
		known := false
		for _, b := range settings.Buses {
			known = known || b == bus
		}
		if !known {
			return 0, fmt.Errorf("kitui: unknown bus %q", bus)
		}
		return m.SetVolume(bus, v/100)
	case key == "renderScale":
		v, err := num()
		if err != nil {
			return 0, err
		}
		return m.Update(func(s *settings.Settings) { s.RenderScale = v / 100 })
	case key == "cameraShake":
		v, err := num()
		if err != nil {
			return 0, err
		}
		return m.Update(func(s *settings.Settings) { s.CameraShake = v / 100 })
	case key == "frameCap":
		v, err := strconv.Atoi(value)
		if err != nil {
			return 0, err
		}
		return m.Update(func(s *settings.Settings) { s.FrameCap = v })
	case key == "quality":
		return m.Update(func(s *settings.Settings) { s.Quality = settings.Quality(value) })
	case key == "motion":
		return m.Update(func(s *settings.Settings) { s.Motion = settings.Motion(value) })
	case key == "language":
		return m.Update(func(s *settings.Settings) { s.Language = value })
	}
	return 0, fmt.Errorf("kitui: unknown setting %q", key)
}

// RebindProps configures RebindPanel.
type RebindProps struct {
	Map     *actions.Map
	Context string
	// Names maps an action name to its display text. Missing names show as is.
	Names map[string]string
	Style actions.PadStyle
	// Listening marks the slot waiting for a key: "action/slot".
	ListeningAction string
	ListeningSlot   int
	Listening       bool
	// Message is shown under the list, for example a conflict warning.
	Message string
	// Class selects which bindings to show. ClassKeyboardMouse by default.
	Class actions.Class
}

// RebindID builds the data-kit-rebind value for a slot: "context/action/slot".
func RebindID(ctx, action string, slot int) string {
	return ctx + "/" + action + "/" + strconv.Itoa(slot)
}

// RebindPanel lists each action of a context with a button per binding slot.
// Selecting a slot sends a "rebind" event named RebindID; the game then calls
// Map.Capture and, with the captured source, Map.Rebind. The last button of a
// row ("+") has slot number len(bindings) and adds a binding.
func RebindPanel(p RebindProps) gosx.Node {
	if p.Class == 0 {
		p.Class = actions.ClassKeyboardMouse
	}
	rows := []any{gosx.Attrs(gosx.Attr("class", "kit-binds"))}
	for _, action := range p.Map.Actions(p.Context) {
		name := action
		if n, ok := p.Names[action]; ok {
			name = n
		}
		row := []any{gosx.Attrs(gosx.Attr("class", "kit-bind-row")), gosx.El("span", gosx.Text(name))}
		bindings := p.Map.Bindings(p.Context, action)
		for slot, b := range bindings {
			if !b.Source.Zero() && b.Source.Class() != p.Class {
				continue
			}
			text := actions.BindingLabel(b, p.Style)
			listening := p.Listening && p.ListeningAction == action && p.ListeningSlot == slot
			if listening {
				text = "Press a key..."
			}
			row = append(row, gosx.El("button", gosx.Attrs(gosx.Attr("type", "button"), gosx.Attr("class", "kit-button"),
				gosx.Attr("data-kit-rebind", RebindID(p.Context, action, slot)),
				gosx.Attr("aria-pressed", strconv.FormatBool(listening)),
				gosx.Attr("aria-label", name+": "+text)), gosx.Text(text)))
		}
		if p.Map.CanAddSlot(p.Context, action) {
			slot := len(bindings)
			listening := p.Listening && p.ListeningAction == action && p.ListeningSlot == slot
			text := "+"
			if listening {
				text = "Press a key..."
			}
			row = append(row, gosx.El("button", gosx.Attrs(gosx.Attr("type", "button"), gosx.Attr("class", "kit-button"),
				gosx.Attr("data-kit-rebind", RebindID(p.Context, action, slot)),
				gosx.Attr("aria-pressed", strconv.FormatBool(listening)),
				gosx.Attr("aria-label", "Add binding for "+name)), gosx.Text(text)))
		}
		rows = append(rows, gosx.El("div", row...))
	}
	return overlay("kit-rebind", "kit-rebind-title", false,
		title("kit-rebind-title", "Controls"),
		gosx.El("div", rows...),
		gosx.El("p", gosx.Attrs(gosx.Attr("class", "kit-message"), gosx.Attr("role", "status"), gosx.Attr("aria-live", "polite")), gosx.Text(p.Message)),
		button("Reset controls", "rebind-reset", gosx.Attr("data-kit-danger", true)),
		button("Back", "rebind-close", gosx.Attr("data-kit-primary", true)),
	)
}

// ParseRebindID reverses RebindID.
func ParseRebindID(id string) (ctx, action string, slot int, ok bool) {
	var parts [3]string
	n := 0
	start := 0
	for i := 0; i < len(id) && n < 2; i++ {
		if id[i] == '/' {
			parts[n] = id[start:i]
			n++
			start = i + 1
		}
	}
	if n != 2 {
		return "", "", 0, false
	}
	parts[2] = id[start:]
	slot, err := strconv.Atoi(parts[2])
	if err != nil || slot < 0 || parts[0] == "" || parts[1] == "" {
		return "", "", 0, false
	}
	return parts[0], parts[1], slot, true
}

// LoadingProps configures LoadingScreen.
type LoadingProps struct {
	Title string // default "Loading"
	// Snapshot is the gate state. The zero value renders 0 percent.
	Snapshot boot.Snapshot
	// Recovering, Failed and Retry text.
	RecoveringText, FailedText, RetryLabel string
}

// BootRoot renders the loading screen inside the element the page watchdog
// and the Go gate both mark: data-boot-root with data-boot and --boot-progress.
// Put your game canvas and this node in the same page; the gate hides the
// screen when it reaches "ready". The element works without any script: its
// initial state is "loading".
func BootRoot(p LoadingProps, game ...gosx.Node) gosx.Node {
	if p.Title == "" {
		p.Title = "Loading"
	}
	if p.RecoveringText == "" {
		p.RecoveringText = "Loading is taking longer than usual. Trying once more..."
	}
	if p.FailedText == "" {
		p.FailedText = "The game did not finish loading."
	}
	if p.RetryLabel == "" {
		p.RetryLabel = "Try again"
	}
	phase := p.Snapshot.Phase.String()
	copyText := ""
	switch p.Snapshot.Phase {
	case boot.PhaseRecovering:
		copyText = p.RecoveringText
	case boot.PhaseFailed:
		copyText = p.FailedText
	}
	progress := strconv.FormatFloat(p.Snapshot.Progress, 'f', 3, 64)
	screen := gosx.El("div", gosx.Attrs(gosx.Attr("class", "kit-overlay kit-overlay--boot"), gosx.Attr("role", "status"), gosx.Attr("aria-live", "polite")),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "kit-panel")),
			gosx.El("h2", gosx.Attrs(gosx.Attr("class", "kit-title"), gosx.Attr("data-boot-title", true)), gosx.Text(p.Title)),
			gosx.El("div", gosx.Attrs(gosx.Attr("class", "kit-progress"), gosx.Attr("role", "progressbar"), gosx.Attr("aria-valuemin", 0),
				gosx.Attr("aria-valuemax", 100), gosx.Attr("aria-valuenow", pct(p.Snapshot.Progress))), gosx.El("span")),
			gosx.El("p", gosx.Attrs(gosx.Attr("class", "kit-copy"), gosx.Attr("data-boot-copy", true)), gosx.Text(copyText)),
			gosx.El("button", gosx.Attrs(gosx.Attr("type", "button"), gosx.Attr("class", "kit-button"), gosx.Attr("data-boot-retry", true),
				gosx.Attr("data-kit-action", "boot-retry"), gosx.Attr("data-kit-primary", true)), gosx.Text(p.RetryLabel)),
		))
	args := []any{gosx.Attrs(gosx.Attr("data-boot-root", true), gosx.Attr("data-boot", phase), gosx.Attr("aria-busy", strconv.FormatBool(p.Snapshot.Phase != boot.PhaseReady)),
		gosx.Attr("style", "--boot-progress:"+progress),
		gosx.Attr("data-boot-recovering-title", "Reconnecting"), gosx.Attr("data-boot-recovering-copy", p.RecoveringText),
		gosx.Attr("data-boot-failed-title", "Could not start"), gosx.Attr("data-boot-failed-copy", p.FailedText)), screen}
	for _, g := range game {
		args = append(args, g)
	}
	return gosx.El("div", args...)
}
