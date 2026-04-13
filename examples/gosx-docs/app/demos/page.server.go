package docs

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

type demoCard struct {
	Slug   string
	Title  string
	Tag    string
	Body   string
	Accent string
	Live   bool
}

var demosIndexCards = []demoCard{
	{
		Slug:   "playground",
		Title:  "Playground",
		Tag:    "write .gsx live",
		Body:   "A live .gsx editor. Type a component on the left, watch it hydrate on the right in real time.",
		Accent: "#9fffa5",
		Live:   true,
	},
	{
		Slug:   "fluid",
		Title:  "Fluid",
		Tag:    "server-streamed velocity field",
		Body:   "A 3D velocity field advected on the server and streamed to your browser as quantized deltas. Particles ride the field on the GPU.",
		Accent: "#a8b8ff",
		Live:   false,
	},
	{
		Slug:   "livesim",
		Title:  "Live Sim",
		Tag:    "authoritative multiplayer",
		Body:   "A tick-rate server runs the full game state. Drop circles, watch them collide in real time across all connected clients.",
		Accent: "#f59e0b",
		Live:   true,
	},
	{
		Slug:   "cms",
		Title:  "CMS",
		Tag:    "block editor",
		Body:   "Drag, reorder, and publish blocks in an editorial-grade writing surface. Nothing persists — it's a demo.",
		Accent: "#ec4899",
		Live:   true,
	},
	{
		Slug:   "scene3d",
		Title:  "Scene3D",
		Tag:    "PBR showroom",
		Body:   "A curated turntable of PBR models with material sliders, lighting presets, and postfx toggles.",
		Accent: "#5fb4ff",
		Live:   true,
	},
	{
		Slug:   "scene3d-bench",
		Title:  "Scene3D Bench",
		Tag:    "frame-time instrumentation",
		Body:   "100K-particle stress field with a live frame-time histogram, P50/P95/P99, GPU info, and tuning knobs.",
		Accent: "#cbd5e1",
		Live:   true,
	},
	{
		Slug:   "collab",
		Title:  "Collab Editor",
		Tag:    "LWW markdown sync",
		Body:   "Two tabs, one document. Real-time preview, instant sync via a hub. Open two tabs — start typing.",
		Accent: "#d9f99d",
		Live:   true,
	},
}

func init() {
	docsapp.RegisterStaticDocsPage(
		"Demos",
		"A tour of GoSX capabilities — servers, islands, real-time, simulation, and 3D.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"demos": demosIndexCards,
				}, nil
			},
		},
	)
}
