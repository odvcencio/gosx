// Package kitui renders the screens every game needs (pause menu, settings,
// key rebinding, loading, error) as GoSX nodes.
//
// The output is plain HTML with data attributes and ARIA roles, so it works
// in a static export: render it at build time, or call markup.RenderHTML (or
// Render) in the browser. The nodes come from package markup, not package
// gosx, so a js/wasm build does not link the .gsx compiler; gosx.Node is the
// same type. It has no hidden state. Colors, sizes and fonts come from CSS
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

	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/boot"
	"m31labs.dev/gosx/game/settings"
	"m31labs.dev/gosx/markup"
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
func Styles() markup.Node {
	return markup.El("style", markup.Attrs(markup.Attr("id", "gosx-kit-style")), markup.RawHTML(css))
}

// Script returns the <script> element with the event script.
func Script() markup.Node {
	return markup.El("script", markup.Attrs(markup.Attr("id", "gosx-kit-events")), markup.RawHTML(script))
}

func button(label, action string, attrs ...any) markup.Node {
	args := []any{markup.Attrs(append([]any{markup.Attr("type", "button"), markup.Attr("class", "kit-button"), markup.Attr("data-kit-action", action)}, attrs...)...), markup.Text(label)}
	return markup.El("button", args...)
}

func overlay(id, titleID string, hidden bool, children ...markup.Node) markup.Node {
	panel := []any{markup.Attrs(markup.Attr("class", "kit-panel"), markup.Attr("role", "dialog"), markup.Attr("aria-modal", "true"),
		markup.Attr("aria-labelledby", titleID), markup.Attr("data-kit-modal", "true"))}
	for _, c := range children {
		panel = append(panel, c)
	}
	attrs := []any{markup.Attr("class", "kit-overlay"), markup.Attr("id", id)}
	if hidden {
		attrs = append(attrs, markup.Attr("hidden", true))
	}
	return markup.El("div", markup.Attrs(attrs...), markup.El("div", panel...))
}

func title(id, text string) markup.Node {
	return markup.El("h2", markup.Attrs(markup.Attr("class", "kit-title"), markup.Attr("id", id)), markup.Text(text))
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
func PauseMenu(p PauseProps) markup.Node {
	if p.Title == "" {
		p.Title = "Paused"
	}
	if len(p.Items) == 0 {
		p.Items = []MenuItem{{ID: "resume", Label: "Resume", Primary: true}, {ID: "settings", Label: "Settings"}, {ID: "quit", Label: "Quit", Danger: true}}
	}
	lis := make([]markup.Node, 0, len(p.Items))
	for i, it := range p.Items {
		var attrs []any
		if it.Disabled {
			attrs = append(attrs, markup.Attr("disabled", true))
		}
		if it.Primary {
			attrs = append(attrs, markup.Attr("data-kit-primary", true))
		}
		if it.Danger {
			attrs = append(attrs, markup.Attr("data-kit-danger", true))
		}
		if i == 0 {
			attrs = append(attrs, markup.Attr("autofocus", true))
		}
		lis = append(lis, markup.El("li", button(it.Label, it.ID, attrs...)))
	}
	menu := []any{markup.Attrs(markup.Attr("class", "kit-menu"))}
	for _, l := range lis {
		menu = append(menu, l)
	}
	return overlay("kit-pause", "kit-pause-title", p.Hidden, title("kit-pause-title", p.Title), markup.El("ul", menu...))
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

func rangeField(l map[string]string, key string, min, max, step, value int, unit string) markup.Node {
	id := "kit-" + key
	return markup.El("div", markup.Attrs(markup.Attr("class", "kit-field")),
		markup.El("label", markup.Attrs(markup.Attr("for", id)), markup.Text(label(l, key))),
		markup.El("input", markup.Attrs(markup.Attr("type", "range"), markup.Attr("id", id), markup.Attr("min", min), markup.Attr("max", max),
			markup.Attr("step", step), markup.Attr("value", value), markup.Attr("data-kit-setting", key), markup.Attr("data-kit-unit", unit))),
		markup.El("span", markup.Attrs(markup.Attr("class", "kit-value"), markup.Attr("aria-hidden", "true")), markup.Text(strconv.Itoa(value)+unit)),
	)
}

func selectField(l map[string]string, key string, opts []Option, current string) markup.Node {
	id := "kit-" + key
	sel := []any{markup.Attrs(markup.Attr("id", id), markup.Attr("data-kit-setting", key))}
	for _, o := range opts {
		a := []any{markup.Attrs(markup.Attr("value", o.Value))}
		if o.Value == current {
			a = []any{markup.Attrs(markup.Attr("value", o.Value), markup.Attr("selected", true))}
		}
		sel = append(sel, markup.El("option", append(a, markup.Text(o.Label))...))
	}
	return markup.El("div", markup.Attrs(markup.Attr("class", "kit-field")),
		markup.El("label", markup.Attrs(markup.Attr("for", id)), markup.Text(label(l, key))),
		markup.El("span"), // keeps the grid columns aligned
		markup.El("select", sel...),
	)
}

func section(l map[string]string, key string) markup.Node {
	return markup.El("h3", markup.Attrs(markup.Attr("class", "kit-section")), markup.Text(label(l, key)))
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
func SettingsPanel(p SettingsProps) markup.Node {
	s, l := p.Settings, p.Labels
	body := []markup.Node{title("kit-settings-title", label(l, "title"))}
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
		button(label(l, "reset"), "settings-reset", markup.Attr("data-kit-danger", true)),
		button(label(l, "close"), "settings-close", markup.Attr("data-kit-primary", true)))
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
func RebindPanel(p RebindProps) markup.Node {
	if p.Class == 0 {
		p.Class = actions.ClassKeyboardMouse
	}
	rows := []any{markup.Attrs(markup.Attr("class", "kit-binds"))}
	for _, action := range p.Map.Actions(p.Context) {
		name := action
		if n, ok := p.Names[action]; ok {
			name = n
		}
		row := []any{markup.Attrs(markup.Attr("class", "kit-bind-row")), markup.El("span", markup.Text(name))}
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
			row = append(row, markup.El("button", markup.Attrs(markup.Attr("type", "button"), markup.Attr("class", "kit-button"),
				markup.Attr("data-kit-rebind", RebindID(p.Context, action, slot)),
				markup.Attr("aria-pressed", strconv.FormatBool(listening)),
				markup.Attr("aria-label", name+": "+text)), markup.Text(text)))
		}
		if p.Map.CanAddSlot(p.Context, action) {
			slot := len(bindings)
			listening := p.Listening && p.ListeningAction == action && p.ListeningSlot == slot
			text := "+"
			if listening {
				text = "Press a key..."
			}
			row = append(row, markup.El("button", markup.Attrs(markup.Attr("type", "button"), markup.Attr("class", "kit-button"),
				markup.Attr("data-kit-rebind", RebindID(p.Context, action, slot)),
				markup.Attr("aria-pressed", strconv.FormatBool(listening)),
				markup.Attr("aria-label", "Add binding for "+name)), markup.Text(text)))
		}
		rows = append(rows, markup.El("div", row...))
	}
	return overlay("kit-rebind", "kit-rebind-title", false,
		title("kit-rebind-title", "Controls"),
		markup.El("div", rows...),
		markup.El("p", markup.Attrs(markup.Attr("class", "kit-message"), markup.Attr("role", "status"), markup.Attr("aria-live", "polite")), markup.Text(p.Message)),
		button("Reset controls", "rebind-reset", markup.Attr("data-kit-danger", true)),
		button("Back", "rebind-close", markup.Attr("data-kit-primary", true)),
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
	// BudgetMS is the stall budget the page watchdog script uses, in
	// milliseconds. Zero leaves the script default (12000).
	BudgetMS int
}

// BootRoot renders the loading screen inside the element the page watchdog
// and the Go gate both mark: data-boot-root with data-boot and --boot-progress.
// Put your game canvas and this node in the same page; the gate hides the
// screen when it reaches "ready". The element works without any script: its
// initial state is "loading".
func BootRoot(p LoadingProps, game ...markup.Node) markup.Node {
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
	screen := markup.El("div", markup.Attrs(markup.Attr("class", "kit-overlay kit-overlay--boot"), markup.Attr("role", "status"), markup.Attr("aria-live", "polite")),
		markup.El("div", markup.Attrs(markup.Attr("class", "kit-panel")),
			markup.El("h2", markup.Attrs(markup.Attr("class", "kit-title"), markup.Attr("data-boot-title", true)), markup.Text(p.Title)),
			markup.El("div", markup.Attrs(markup.Attr("class", "kit-progress"), markup.Attr("role", "progressbar"), markup.Attr("aria-valuemin", 0),
				markup.Attr("aria-valuemax", 100), markup.Attr("aria-valuenow", pct(p.Snapshot.Progress))), markup.El("span")),
			markup.El("p", markup.Attrs(markup.Attr("class", "kit-copy"), markup.Attr("data-boot-copy", true)), markup.Text(copyText)),
			markup.El("button", markup.Attrs(markup.Attr("type", "button"), markup.Attr("class", "kit-button"), markup.Attr("data-boot-retry", true),
				markup.Attr("data-kit-action", "boot-retry"), markup.Attr("data-kit-primary", true)), markup.Text(p.RetryLabel)),
		))
	budget := []any{}
	if p.BudgetMS > 0 {
		budget = append(budget, markup.Attr("data-boot-budget-ms", p.BudgetMS))
	}
	args := []any{markup.Attrs(append(budget, markup.Attr("data-boot-root", true), markup.Attr("data-boot", phase), markup.Attr("aria-busy", strconv.FormatBool(p.Snapshot.Phase != boot.PhaseReady)),
		markup.Attr("style", "--boot-progress:"+progress),
		markup.Attr("data-boot-recovering-title", "Reconnecting"), markup.Attr("data-boot-recovering-copy", p.RecoveringText),
		markup.Attr("data-boot-failed-title", "Could not start"), markup.Attr("data-boot-failed-copy", p.FailedText))...), screen}
	for _, g := range game {
		args = append(args, g)
	}
	return markup.El("div", args...)
}
