package playground

// Preset is a starter .gsx snippet shown in the playground editor.
type Preset struct {
	Slug        string // url-safe key (e.g., "counter")
	Title       string // display title (e.g., "Counter")
	Description string // short tagline
	Source      string // full .gsx source, compile+lower ready
}

// presets is the canonical ordered slice. Not exported — callers use Presets().
var presets = []Preset{
	{
		Slug:        "counter",
		Title:       "Counter",
		Description: "One signal, plus/minus buttons",
		Source: `package playground

//gosx:island
func Counter() Node {
	count := signal.New(0)
	return <div class="counter">
		<button data-on-click="count.Set(count.Get() - 1)">-1</button>
		<span>{count.Get()}</span>
		<button data-on-click="count.Set(count.Get() + 1)">+1</button>
	</div>
}
`,
	},
	{
		Slug:        "two-counters",
		Title:       "Two Counters",
		Description: "Two independent signals, no shared state",
		Source: `package playground

//gosx:island
func TwoCounters() Node {
	a := signal.New(0)
	b := signal.New(0)
	return <div class="two-counters">
		<button data-on-click="a.Set(a.Get() + 1)">a++</button>
		<span>{a.Get()}</span>
		<button data-on-click="b.Set(b.Get() + 1)">b++</button>
		<span>{b.Get()}</span>
	</div>
}
`,
	},
	{
		Slug:        "toggle",
		Title:       "Toggle",
		Description: "Boolean signal toggled by a button",
		Source: `package playground

//gosx:island
func Toggle() Node {
	on := signal.New(false)
	return <div class="toggle">
		<button data-on-click="on.Set(!on.Get())">Toggle</button>
		<span>{on.Get()}</span>
	</div>
}
`,
	},
	{
		Slug:        "greeter",
		Title:       "Greeter",
		Description: "Text input bound to a signal",
		Source: `package playground

//gosx:island
func Greeter() Node {
	name := signal.New("world")
	return <div class="greeter">
		<input type="text" data-on-input="name.Set(value)" />
		<h1>Hello, {name.Get()}</h1>
	</div>
}
`,
	},
	{
		Slug:        "shared-theme",
		Title:       "Shared Theme",
		Description: "Shared signal with string value",
		Source: `package playground

//gosx:island
func SharedTheme() Node {
	theme := signal.NewShared("$theme", "dark")
	return <div class="shared-theme">
		<button data-on-click="theme.Set(\"light\")">light</button>
		<button data-on-click="theme.Set(\"dark\")">dark</button>
		<span>{theme.Get()}</span>
	</div>
}
`,
	},
}

// Presets returns the curated list in display order. The first entry is the
// default loaded into the editor on first visit. Returns a fresh slice each
// call to prevent callers from mutating the internal state.
func Presets() []Preset {
	out := make([]Preset, len(presets))
	copy(out, presets)
	return out
}

// PresetBySlug returns the preset with the given slug, or (zero, false).
func PresetBySlug(slug string) (Preset, bool) {
	for _, p := range presets {
		if p.Slug == slug {
			return p, true
		}
	}
	return Preset{}, false
}

// DefaultPreset is a convenience for the first entry.
func DefaultPreset() Preset {
	return presets[0]
}
