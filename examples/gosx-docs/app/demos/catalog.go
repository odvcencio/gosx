package docs

import "strings"

// DemoDefinition is the single product contract behind the demos index, dock,
// and metadata drawer. Keep claims here specific and verifiable.
type DemoDefinition struct {
	Slug        string
	Title       string
	Tag         string
	Promise     string
	Lesson      string
	Accent      string
	Facets      []string
	SourcePath  string
	Packages    []string
	Status      string
	RenderMode  string
	Limitations string
}

var demoCatalog = []DemoDefinition{
	{
		Slug: "checkers", Title: "Chinese Checkers", Tag: "playable Go strategy table",
		Promise: "Play a revisioned two-seat match on a complete 121-hole Scene3D table with animated hop paths, four CPU difficulty tiers, and three selectable Selena material families.",
		Lesson:  "GoSX keeps legality, replay, and CPU search in deterministic Go; a Hub owns turns, Selena owns optics, Arbiter-ready policies bound search posture, and optional Elio hints retain a Go CPU fallback.",
		Accent:  "#e5b84f", Facets: []string{"Scene3D", "Hub", "Simulation", "Selena", "Arbiter policy", "Elio adapter"},
		SourcePath: "examples/gosx-docs/app/demos/checkers/page.gsx", Packages: []string{"scene", "hub", "route", "selena", "arbiter", "elio"},
		Status: "live", RenderMode: "SSR + Scene3D GPU runtime + authoritative GoSX Hub",
		Limitations: "Playable mode is two-player and in-memory. There is no product network multiplayer or persistence; the active CPU uses bounded Go search with a compiled Arbiter policy fallback, while Elio hints remain optional and inactive.",
	},
	{
		Slug: "water", Title: "Water Lab", Tag: "adaptive GPU simulation",
		Promise: "Disturb a physically responsive pool with caustics, buoyant objects, and adaptive quality.",
		Lesson:  "A typed GoSX WaterSystem drives Selena-authored shaders through native WebGPU and WebGL2 backends.",
		Accent:  "#69e3c7", Facets: []string{"Scene3D", "Selena", "WebGPU", "WebGL2"},
		SourcePath: "examples/gosx-docs/app/demos/water/page.gsx", Packages: []string{"scene", "selena", "route"},
		Status: "featured", RenderMode: "SSR + Scene3D GPU runtime",
		Limitations: "WebGPU depends on browser and hardware support; GoSX falls back honestly to WebGL2.",
	},
	{
		Slug: "playground", Title: "GoSX Playground", Tag: "compile .gsx live",
		Promise: "Edit a GoSX component and see the compiled result hydrate beside the source.",
		Lesson:  "GoSX can compile a typed .gsx component on demand and hydrate its binary program as an island.",
		Accent:  "#9fffa5", Facets: []string{"Compiler", "Island", "Action"},
		SourcePath: "examples/gosx-docs/app/demos/playground/page.gsx", Packages: []string{"gosx", "hydrate", "action"},
		Status: "live", RenderMode: "SSR + hydrated preview island",
		Limitations: "Compilation is rate-limited and intentionally supports a focused demo-safe source subset.",
	},
	{
		Slug: "fluid", Title: "Velocity Field", Tag: "quantized server stream",
		Promise: "Watch particles ride a 3D velocity field computed on the server and decoded in the browser.",
		Lesson:  "A GoSX hub streams compact six-bit field deltas while the client performs lightweight presentation.",
		Accent:  "#7aa2ff", Facets: []string{"Hub", "Simulation", "Quantization"},
		SourcePath: "examples/gosx-docs/app/demos/fluid/page.gsx", Packages: []string{"field", "hub", "route"},
		Status: "live", RenderMode: "Server simulation + Canvas 2D",
		Limitations: "The browser visualizes the middle slice of the full 3D field.",
	},
	{
		Slug: "livesim", Title: "Live Physics", Tag: "server-authoritative multiplayer",
		Promise: "Drop circles into a shared physics world whose state is owned by the server.",
		Lesson:  "GoSX simulation ticks and hub fanout keep every connected browser on one authoritative timeline.",
		Accent:  "#f59e0b", Facets: []string{"Hub", "Simulation", "Multiplayer"},
		SourcePath: "examples/gosx-docs/app/demos/livesim/page.gsx", Packages: []string{"simulation", "hub", "route"},
		Status: "live", RenderMode: "Server simulation + Canvas 2D",
		Limitations: "Open a second tab to see the live viewer count and ghost cursors; there is no persistence or rooms.",
	},
	{
		Slug: "collab", Title: "Collab Editor", Tag: "shared markdown, presence, and cursors over a hub",
		Promise: "Edit one document from two tabs and watch both copies converge in real time, with live presence and colored remote cursors.",
		Lesson:  "A GoSX hub carries versioned last-write-wins document updates, connected-editor presence, and cursor broadcast with a server-seeded first render.",
		Accent:  "#d9f99d", Facets: []string{"Hub", "Realtime", "Presence", "SSR"},
		SourcePath: "examples/gosx-docs/app/demos/collab/page.gsx", Packages: []string{"hub", "route"},
		Status: "live", RenderMode: "SSR + hub-synchronized client",
		Limitations: "This teaching demo uses last-write-wins rather than a production CRDT, and has no rooms or persistence.",
	},
	{
		Slug: "scene3d", Title: "Geometry Zoo", Tag: "declarative PBR scene",
		Promise: "Orbit a slowly turning PBR composition on a glossy clearcoat floor, declared as a typed GoSX scene.",
		Lesson:  "Lights, materials, geometry, camera, tonemapping, and bloom lower from Go to GoSX Scene3D.",
		Accent:  "#5fb4ff", Facets: []string{"Scene3D", "PBR", "PostFX"},
		SourcePath: "examples/gosx-docs/app/demos/scene3d/page.gsx", Packages: []string{"scene", "route"},
		Status: "live", RenderMode: "SSR + Scene3D GPU runtime",
		Limitations: "Rendering capability and backend depend on the browser GPU stack.",
	},
	{
		Slug: "scene3d-bench", Title: "Scene3D Bench", Tag: "renderer diagnostics",
		Promise: "Compare seven workloads with live frame-time percentiles, a histogram, an fps sparkline, and detected GPU facts.",
		Lesson:  "GoSX exposes opt-in renderer measurements without changing the declared Scene3D program.",
		Accent:  "#cbd5e1", Facets: []string{"Scene3D", "Performance", "Diagnostics"},
		SourcePath: "examples/gosx-docs/app/demos/scene3d-bench/page.gsx", Packages: []string{"scene", "route"},
		Status: "lab", RenderMode: "SSR + instrumented Scene3D runtime",
		Limitations: "Measurements reflect the current machine and browser; they are not cross-device benchmarks.",
	},
	{
		Slug: "cms", Title: "CMS Editor", Tag: "block-editor with live preview",
		Promise: "Add blocks, watch the preview update instantly, and publish a validated document through a protected GoSX server Action.",
		Lesson:  "Client-side interactivity (dynamic blocks, live preview) and a rate-limited, CSRF-protected server Action compose on the same page — no separate API layer, no client framework.",
		Accent:  "#ec4899", Facets: []string{"SSR", "Action", "CSRF"},
		SourcePath: "examples/gosx-docs/app/demos/cms/page.gsx", Packages: []string{"route", "action"},
		Status: "live", RenderMode: "SSR + client-side draft editor",
		Limitations: "Adding blocks and live preview run entirely in the browser; publish validates and stores the full draft in memory only — there is no persistence across restarts, no reordering, and no block removal.",
	},
}

func Demos() []DemoDefinition {
	return demoCatalog
}

func FindDemo(slug string) (DemoDefinition, bool) {
	for _, demo := range demoCatalog {
		if demo.Slug == slug {
			return demo, true
		}
	}
	return DemoDefinition{}, false
}

func demoValues(values []string) string {
	return strings.Join(values, ", ")
}

func demoSourceURL(path string) string {
	return "https://github.com/odvcencio/gosx/blob/main/" + path
}
