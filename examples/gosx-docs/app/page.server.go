package docs

import (
	"m31labs.dev/gosx/route"
)

func init() {
	RegisterStaticDocsPage(
		"GoSX",
		"Build server-rendered apps, interactive tools, realtime systems, and GPU scenes in Go without a JavaScript app toolchain.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"heroScene": HeroScene(),
					"runtimeSurfaces": []map[string]string{
						{"num": "01", "name": "Server", "purpose": "Pages, layouts, loaders, metadata, and streamed HTML.", "cost": "Component runtime: none"},
						{"num": "02", "name": "Action", "purpose": "Typed mutations, validation, CSRF, redirects, and form state.", "cost": "Browser cost: native HTML"},
						{"num": "03", "name": "Island", "purpose": "Focused reactive DOM behavior compiled from constrained Go.", "cost": "Browser cost: shared Go VM"},
						{"num": "04", "name": "Engine", "purpose": "Canvas, Scene3D, simulation, media, workers, and GPU work.", "cost": "Browser cost: managed runtime"},
						{"num": "05", "name": "Hub", "purpose": "Presence, fanout, CRDT documents, and shared realtime state.", "cost": "Browser cost: WebSocket"},
					},
					"paths": []map[string]string{
						{"num": "01", "title": "Web applications", "body": "Ship server HTML with strict typed components or flexible legacy routes, then add actions, sessions, auth, streaming, and caching where needed.", "tools": "Component · Server · Action", "href": "/docs/components"},
						{"num": "02", "title": "Interactive interfaces", "body": "Hydrate only the reactive regions. Signals and islands keep ordinary content out of the client runtime.", "tools": "Island · Signal", "href": "/docs/islands"},
						{"num": "03", "title": "Realtime workspaces", "body": "Coordinate users and agents through hubs, presence, shared state, and conflict-free documents.", "tools": "Hub · CRDT", "href": "/docs/hubs"},
						{"num": "04", "title": "Visual computing", "body": "Author 3D scenes, materials, simulations, and GPU workloads in Go with managed backend fallback.", "tools": "Engine · Scene3D · Selena", "href": "/docs/scene3d"},
					},
					"siteProofs": []map[string]string{
						{"surface": "Server", "title": "Strict typed route", "body": "This production route is authored with component syntax and checked against real Go props.", "href": "/docs/typed-live", "cta": "Inspect the rendered proof"},
						{"surface": "Action + Island", "title": "Compiler playground", "body": "A protected server action compiles a focused legacy island program and the shared browser VM hydrates its preview.", "href": "/demos/playground", "cta": "Compile a component"},
						{"surface": "Hub", "title": "Realtime collaboration", "body": "Open two tabs to watch one server-owned document, presence, and remote cursors converge over a GoSX hub.", "href": "/demos/collab", "cta": "Open the shared editor"},
						{"surface": "Engine", "title": "Scene3D world", "body": "The server declares the scene; the managed runtime selects WebGPU, WebGL2, or an honest bounded fallback.", "href": "/demos/beacon", "cta": "Enter Blackglass Coast"},
					},
					"proofPoints": []map[string]string{
						{"value": "5", "label": "Execution surfaces"},
						{"value": "2", "label": "GPU backends"},
						{"value": "1", "label": "Deployable dist"},
						{"value": "0", "label": "JS app toolchains"},
					},
				}, nil
			},
		},
	)
}
