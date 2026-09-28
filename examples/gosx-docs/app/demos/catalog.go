package docs

import (
	"fmt"
	"strings"

	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
)

// DemoDefinition is the single product contract behind the demos index, dock,
// and metadata drawer. Keep claims here specific and verifiable.
type DemoDefinition struct {
	Slug         string
	Title        string
	Tag          string
	Summary      string
	Group        string
	PosterPath   string
	Promise      string
	Lesson       string
	Accent       string
	Facets       []string
	SourcePath   string
	SourcePaths  []string
	Backends     []string
	Packages     []string
	Status       string
	RenderMode   string
	Limitations  string
	ShowcaseRank int
}

var demoCatalog = []DemoDefinition{
	{
		Slug: "showreel", Title: "Orbital sculpture", Tag: "interactive scene study",
		Promise: "Orbit a small sculpture built from typed Go scene data.",
		Lesson:  "Scene3D renders a small, asset-free graph of geometry, lighting, materials, and post effects.",
		Accent:  "#f6ba77", Facets: []string{"Scene3D", "PBR", "PostFX"},
		SourcePath: "examples/gosx-docs/app/demos/showreel.go", Packages: []string{"scene", "route"},
		Status: "live", RenderMode: "SSR + Scene3D GPU runtime",
		Limitations: "The scene is still until you orbit it. WebGPU needs browser and hardware support; WebGL2 is the fallback.",
	},
	{
		Slug: "checkers", Title: "Chinese Checkers", Tag: "playable Go strategy table",
		Promise: "Play a two-seat Chinese checkers match on a 121-hole 3D board.",
		Lesson:  "GoSX keeps legality, replay, and CPU search in deterministic Go; a Hub owns turns, Selena owns optics, Arbiter-ready policies bound search posture, and optional Elio hints retain a Go CPU fallback.",
		Accent:  "#e5b84f", Facets: []string{"Scene3D", "Hub", "Simulation", "Selena", "Arbiter policy", "Elio adapter"},
		SourcePath: "examples/gosx-docs/app/demos/checkers/page.gsx", Packages: []string{"scene", "hub", "route", "selena", "arbiter", "elio"},
		Status: "live", RenderMode: "SSR + Scene3D GPU runtime + authoritative GoSX Hub",
		Limitations:  "Playable mode is two-player and in-memory. There is no product network multiplayer or persistence; the active CPU uses bounded Go search with a compiled Arbiter policy fallback, while Elio hints remain optional and inactive.",
		ShowcaseRank: 0,
	},
	{
		Slug: "beacon", Title: "Blackglass Coast", Tag: "Studio-authored water world",
		Promise: "Orbit a volcanic cove with live water, basalt shelves, ruins, and a beacon.",
		Lesson:  "A typed Studio world contract carries water-volume and gameplay anchors into a bounded GoSX Scene3D runtime with truthful backend fallback and frame-quality telemetry.",
		Accent:  "#e7bd6b", Facets: []string{"Scene3D", "Studio", "WaterSystem", "PBR", "PostFX", "Compute particles"},
		SourcePath: "examples/gosx-docs/app/demos/beacon/program.go", Packages: []string{"scene", "route"},
		Status: "live", RenderMode: "SSR + Scene3D GPU runtime",
		Limitations:  "This first vertical slice binds the authored cove and its water semantics, but it is an orbitable world showcase—not yet a third-person game. WebGPU depends on browser and hardware support; GoSX falls back honestly to WebGL2 or its bounded primitive fallback. The scene caps rendering at 60 frames per second (FPS), a device pixel ratio (DPR) of 1.5, and a 720p render surface.",
		ShowcaseRank: 0,
	},
	{
		Slug: "water", Title: "Water", Tag: "flagship real-time optics",
		Promise: "Disturb a responsive water pool with buoyant objects and live caustics.",
		Lesson:  "A typed GoSX WaterSystem drives one Selena-authored optical model through native WebGPU and WebGL2 backends.",
		Accent:  "#69e3c7", Facets: []string{"Scene3D", "Selena", "Physical optics"},
		SourcePath: "examples/gosx-docs/app/demos/water/page.gsx", Packages: []string{"scene", "selena", "route"},
		Status: "featured", RenderMode: "SSR + Scene3D GPU runtime",
		Limitations:  "WebGPU depends on browser and hardware support; GoSX falls back honestly to WebGL2. Hero targets discrete GPUs; the new optical ALU still needs Apple/Metal hardware certification.",
		ShowcaseRank: 1,
	},
	{
		Slug: "playground", Title: "GoSX Playground", Tag: "compile .gsx live",
		Promise: "Edit a GoSX component and see its compiled result beside the source.",
		Lesson:  "GoSX can compile a legacy Go-function .gsx island on demand and hydrate its binary program in the shared browser VM.",
		Accent:  "#9fffa5", Facets: []string{"Compiler", "Island", "Action"},
		SourcePath: "examples/gosx-docs/app/demos/playground/page.gsx", Packages: []string{"gosx", "hydrate", "action"},
		Status: "live", RenderMode: "SSR + hydrated preview island",
		Limitations: "Compilation is rate-limited and intentionally supports a focused demo-safe source subset.",
	},
	{
		Slug: "fluid", Title: "Velocity Field", Tag: "quantized server stream",
		Promise: "Watch particles follow a server-computed 3D velocity field.",
		Lesson:  "A GoSX hub streams compact six-bit field deltas while the client performs lightweight presentation.",
		Accent:  "#7aa2ff", Facets: []string{"Hub", "Simulation", "Quantization"},
		SourcePath: "examples/gosx-docs/app/demos/fluid/page.gsx", Packages: []string{"field", "hub", "route"},
		Status: "live", RenderMode: "Server simulation + Canvas 2D",
		Limitations: "The browser visualizes the middle slice of the full 3D field.",
	},
	{
		Slug: "livesim", Title: "Live Physics", Tag: "server-authoritative multiplayer",
		Promise: "Drop circles into a physics world shared through the server.",
		Lesson:  "GoSX simulation ticks and hub fanout keep every connected browser on one authoritative timeline.",
		Accent:  "#f59e0b", Facets: []string{"Hub", "Simulation", "Multiplayer"},
		SourcePath: "examples/gosx-docs/app/demos/livesim/page.gsx", Packages: []string{"simulation", "hub", "route"},
		Status: "live", RenderMode: "Server simulation + Canvas 2D",
		Limitations: "Open a second tab to see the live viewer count and ghost cursors; there is no persistence or rooms.",
	},
	{
		Slug: "collab", Title: "Collab Editor", Tag: "shared markdown, presence, and cursors over a hub",
		Promise: "Edit one document from two tabs and watch changes and cursors sync live.",
		Lesson:  "A GoSX hub carries versioned last-write-wins document updates, connected-editor presence, and cursor broadcast with a server-seeded first render.",
		Accent:  "#d9f99d", Facets: []string{"Hub", "Realtime", "Presence", "SSR"},
		SourcePath: "examples/gosx-docs/app/demos/collab/page.gsx", Packages: []string{"hub", "route"},
		Status: "live", RenderMode: "SSR + hub-synchronized client",
		Limitations: "This teaching demo uses last-write-wins rather than a production CRDT, and has no rooms or persistence.",
	},
	{
		Slug: "scene3d", Title: "Geometry Zoo", Tag: "declarative PBR scene",
		Promise: "Orbit a PBR scene with typed geometry, lighting, and materials.",
		Lesson:  "Lights, materials, geometry, camera, tonemapping, and bloom lower from Go to GoSX Scene3D.",
		Accent:  "#5fb4ff", Facets: []string{"Scene3D", "PBR", "PostFX"},
		SourcePath: "examples/gosx-docs/app/demos/scene3d/page.gsx", Packages: []string{"scene", "route"},
		Status: "live", RenderMode: "SSR + Scene3D GPU runtime",
		Limitations: "Rendering capability and backend depend on the browser GPU stack.",
	},
	{
		Slug: "scene3d-bench", Title: "Scene3D Bench", Tag: "renderer workload study",
		Promise: "Switch between Scene3D renderer workloads using the URL.",
		Lesson:  "A typed Go scene can select different renderer workloads in the shared browser runtime.",
		Accent:  "#cbd5e1", Facets: []string{"Scene3D", "Performance", "Diagnostics"},
		SourcePath: "examples/gosx-docs/app/demos/scene3d-bench/page.gsx", Packages: []string{"scene", "route"},
		Status: "lab", RenderMode: "SSR + Scene3D runtime",
		Limitations:  "The default route shows the scene only. Published frame-time receipts appear on /performance. Frame times vary by browser and machine; the receipts name the tested system.",
		ShowcaseRank: 0,
	},
	{
		Slug: "html-surface", Title: "Diegetic Panels", Tag: "HTML textured onto 3D geometry",
		Promise: "Read three HTML panels placed on surfaces inside a 3D scene.",
		Lesson:  "A texture-mode scene.HTML is a quad in the scene graph. Page Cascading Style Sheets (CSS), layout, and web fonts render with rotation, occlusion, and depth like a mesh.",
		Accent:  "#35d6ff", Facets: []string{"Scene3D", "HTML surface"},
		SourcePath: "examples/gosx-docs/app/demos/html-surface/program.go", Packages: []string{"scene", "route"},
		Status: "lab", RenderMode: "SSR + Scene3D GPU runtime",
		Limitations: "Pointer hits expose CSS-pixel surface coordinates. The runtime does not synthesize Document Object Model (DOM) events for descendants. Rasterization responds to authored markup, size, device pixel ratio, stylesheet revisions, and explicit invalidation. It does not observe arbitrary DOM mutations. The runtime reports cross-origin stylesheets as blocked.",
	},
	{
		Slug: "cms", Title: "CMS Editor", Tag: "block-editor with live preview",
		Promise: "Add content blocks, preview them live, and publish with a server Action.",
		Lesson:  "Client-side interactivity (dynamic blocks, live preview) and a rate-limited, CSRF-protected server Action compose on the same page — no separate API layer, no client framework.",
		Accent:  "#ec4899", Facets: []string{"SSR", "Action", "CSRF"},
		SourcePath: "examples/gosx-docs/app/demos/cms/page.gsx", Packages: []string{"route", "action"},
		Status: "live", RenderMode: "SSR + client-side draft editor",
		Limitations: "Adding blocks and live preview run entirely in the browser; publish validates and stores the full draft in memory only — there is no persistence across restarts, no reordering, and no block removal.",
	},
	{
		Slug: "orrery", Title: "Lodestar Meridian", Tag: "declarative animation choreography",
		Promise: "Watch a clockwork solar system complete an orbit and transit cycle.",
		Lesson:  "GoSX Scene3D ships graph animation channels and material keyframe tracks as typed data — targets, keys, and timing stay stable, deterministic, and asset-free, with declared node, vertex, and pixel budgets.",
		Accent:  "#c4b5fd", Facets: []string{"Scene3D", "Animation channels", "MaterialAnims", "PostFX", "Points"},
		SourcePath: "examples/gosx-docs/app/demos/orrery/program.go", Packages: []string{"scene", "route"},
		Status: "live", RenderMode: "SSR + Scene3D GPU runtime",
		Limitations:  "The choreography is one declared keyframe cycle played by the shared client runtime; it has no user-authored interaction beyond orbit controls and no server state. WebGPU depends on browser and hardware support; GoSX falls back honestly to WebGL2 or its bounded primitive fallback. The scene caps rendering at 60 frames per second (FPS), device pixel ratio (DPR) 1.5, and a 720p render surface, and under prefers-reduced-motion its animation loop is suppressed to a still.",
		ShowcaseRank: 0,
	},
}

func Demos() []DemoDefinition {
	demos := make([]DemoDefinition, 0, len(demoCatalog))
	for _, definition := range demoCatalog {
		demo := definition
		demo.Summary = demoSummaries[demo.Slug]
		demo.Group = demoGroups[demo.Slug]
		demo.PosterPath = "/demos/posters/" + demo.Slug + ".webp"
		demo.SourcePaths = append([]string(nil), demoSources[demo.Slug]...)
		if len(demo.SourcePaths) == 0 && demo.SourcePath != "" {
			demo.SourcePaths = []string{demo.SourcePath}
		}
		demos = append(demos, demo)
	}
	return demos
}

func FindDemo(slug string) (DemoDefinition, bool) {
	for _, demo := range Demos() {
		if demo.Slug == slug {
			return demo, true
		}
	}
	return DemoDefinition{}, false
}

// ShowcaseDemos returns the featured row in explicit, stable order.
func ShowcaseDemos() []DemoDefinition {
	return galleryShowcaseDemos(Demos())
}

func AdditionalDemos() []DemoDefinition {
	return galleryAdditionalDemos(Demos())
}

func demoValues(values []string) string {
	return strings.Join(values, ", ")
}

func demoBackendSummary(values []string) string {
	switch len(values) {
	case 0:
		return "Not available"
	case 1:
		return values[0]
	case 2:
		return values[0] + " or " + values[1]
	default:
		return strings.Join(values[:len(values)-1], ", ") + " or " + values[len(values)-1]
	}
}

func demoSourceCountLabel(paths []string) string {
	return fmt.Sprintf("All %d source files", len(paths))
}

func demoStatusLabel(status string) string {
	if status == "" {
		return "Status unavailable"
	}
	return strings.ToUpper(status[:1]) + status[1:]
}

func demoSourceURL(path string) string {
	return "https://github.com/odvcencio/gosx/blob/main/" + path
}

// LiveDemoForGuide resolves the catalog demo that proves a documentation
// guide's subject live in the browser. The pairing is owned by the docs
// catalog (DocEntry.Demo); this is the read side used by the docs shell.
func LiveDemoForGuide(docHref string) (DemoDefinition, bool) {
	if docHref == "" {
		return DemoDefinition{}, false
	}
	for _, section := range docsapp.DocsCatalog() {
		for _, entry := range section.Entries {
			if entry.Href == docHref && entry.Demo != "" {
				return FindDemo(entry.Demo)
			}
		}
	}
	return DemoDefinition{}, false
}

// RelatedGuides returns the documentation guides that teach the concepts a
// demo applies, in stable catalog order. It mirrors LiveDemoForGuide so both
// directions of the docs-to-demos cross-navigation stay consistent, and it
// returns nil for demos without a mapped guide.
func RelatedGuides(demoSlug string) []docsapp.DocEntry {
	guides := make([]docsapp.DocEntry, 0, 2)
	for _, section := range docsapp.DocsCatalog() {
		for _, entry := range section.Entries {
			if entry.Demo != "" && entry.Demo == demoSlug {
				guides = append(guides, entry)
			}
		}
	}
	if len(guides) == 0 {
		return nil
	}
	return guides
}
