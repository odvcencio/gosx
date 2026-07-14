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
						{"num": "01", "name": "Server", "purpose": "Pages, layouts, loaders, metadata, and streamed HTML.", "cost": "Browser cost: none"},
						{"num": "02", "name": "Action", "purpose": "Typed mutations, validation, CSRF, redirects, and form state.", "cost": "Browser cost: native HTML"},
						{"num": "03", "name": "Island", "purpose": "Focused reactive DOM behavior compiled from constrained Go.", "cost": "Browser cost: shared Go VM"},
						{"num": "04", "name": "Engine", "purpose": "Canvas, Scene3D, simulation, media, workers, and GPU work.", "cost": "Browser cost: managed runtime"},
						{"num": "05", "name": "Hub", "purpose": "Presence, fanout, CRDT documents, and shared realtime state.", "cost": "Browser cost: WebSocket"},
					},
					"paths": []map[string]string{
						{"num": "01", "title": "Web applications", "body": "Ship fast HTML first, then add typed mutations, sessions, auth, streaming, and caching where needed.", "tools": "Server · Action", "href": "/docs/getting-started"},
						{"num": "02", "title": "Interactive interfaces", "body": "Hydrate only the reactive regions. Signals and islands keep ordinary content out of the client runtime.", "tools": "Island · Signal", "href": "/docs/islands"},
						{"num": "03", "title": "Realtime workspaces", "body": "Coordinate users and agents through hubs, presence, shared state, and conflict-free documents.", "tools": "Hub · CRDT", "href": "/docs/hubs"},
						{"num": "04", "title": "Visual computing", "body": "Author 3D scenes, materials, simulations, and GPU workloads in Go with managed backend fallback.", "tools": "Engine · Scene3D · Selena", "href": "/docs/scene3d"},
					},
					"proofPoints": []map[string]string{
						{"value": "5", "label": "Execution surfaces"},
						{"value": "2", "label": "GPU backends"},
						{"value": "1", "label": "Deployable binary"},
						{"value": "0", "label": "JS app toolchains"},
					},
				}, nil
			},
		},
	)
}
