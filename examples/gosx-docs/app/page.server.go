package docs

import "github.com/odvcencio/gosx/route"

func init() {
	RegisterStaticDocsPage(
		"Overview",
		"GoSX is a Go-native web framework for sites, docs, forms, and interactive demos that ship from one codebase.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"proofs": []map[string]string{
						{
							"value": "One codebase",
							"label": "Landing page, docs, demos, and gated routes all run together.",
						},
						{
							"value": "One publish button",
							"label": "The CMS commits the whole page at once instead of saving block by block.",
						},
						{
							"value": "Live 3D route",
							"label": "The canvas is interactive without breaking out into a separate app.",
						},
					},
					"routes": []map[string]string{
						{
							"label": "Landing + docs",
							"path":  "/",
							"body":  "The homepage, docs shell, navigation, and page styling all live in the same app tree.",
						},
						{
							"label": "CMS demo",
							"path":  "/demos/cms",
							"body":  "Drag blocks, edit copy, preview instantly, and publish the full document with one action.",
						},
						{
							"label": "Scene3D route",
							"path":  "/demos/scene3d",
							"body":  "A real canvas route with pointer and keyboard input, still wrapped in the normal page shell.",
						},
						{
							"label": "Auth flow",
							"path":  "/docs/auth",
							"body":  "Sessions, passkeys, and protected routes sit beside the rest of the site instead of in a separate demo.",
						},
					},
					"stack": []string{
						"File routes",
						"Nested layouts",
						"Server actions",
						"Sessions + auth",
						"Client navigation",
						"Public assets",
						"Scene3D",
						"Static export",
					},
					"showcases": []map[string]any{
						{
							"kicker":         "Site demo",
							"tone":           "docs",
							"route":          "/docs/getting-started",
							"cues":           []string{"Routes", "Layouts", "Metadata"},
							"title":          "The docs site is the first demo.",
							"body":           "Open the quickstart and you are already inside the framework story. No separate docs engine. No disconnected marketing stack.",
							"href":           "/docs/getting-started",
							"label":          "Read the quickstart",
							"secondaryHref":  "/docs/routing",
							"secondaryLabel": "See routing",
							"points": []string{
								"Nested layouts and file routes shape the whole site.",
								"Page CSS and metadata stay close to the page that owns them.",
								"Client navigation sits on top of server-rendered HTML.",
							},
						},
						{
							"kicker":         "CMS demo",
							"tone":           "cms",
							"route":          "/demos/cms",
							"cues":           []string{"Drag", "Edit", "Publish"},
							"title":          "Edit blocks in place. Publish the page in one move.",
							"body":           "The editor feels immediate in the browser, but the final publish is still one normal form action.",
							"href":           "/demos/cms",
							"label":          "Open the CMS demo",
							"secondaryHref":  "/docs/forms",
							"secondaryLabel": "Read the form model",
							"points": []string{
								"Drag to reorder blocks.",
								"Edit copy inline with a live preview beside it.",
								"Publish once, without a row of per-block save buttons.",
							},
						},
						{
							"kicker":         "Scene3D",
							"tone":           "scene",
							"route":          "/demos/scene3d",
							"cues":           []string{"Pointer", "Keyboard", "Canvas"},
							"title":          "Open a route with a real interactive canvas.",
							"body":           "Scene3D is part of the app, so the route still owns the shell, the copy, and the links around the canvas.",
							"href":           "/demos/scene3d",
							"label":          "Open Geometry Zoo",
							"secondaryHref":  "/docs/runtime",
							"secondaryLabel": "Read runtime docs",
							"points": []string{
								"Pointer and keyboard input.",
								"Canvas lives inside the routed page, not outside it.",
								"Same repo, same deploy, no detached frontend.",
							},
						},
					},
					"principles": []map[string]string{
						{
							"kicker": "Start simple",
							"title":  "Begin with a page, not plumbing.",
							"body":   "Routes, HTML, metadata, and forms should feel boring in the best way. That keeps the base of the app easy to reason about.",
						},
						{
							"kicker": "Add runtime on purpose",
							"title":  "Use browser power where it creates a better experience.",
							"body":   "Bring in navigation, islands, or Scene3D when the page needs them. Leave everything else on the server.",
						},
						{
							"kicker": "Ship the real thing",
							"title":  "The example is not a mockup. It deploys.",
							"body":   "The same app that tells the story can be built, exported, and pushed live without a second implementation hiding behind it.",
						},
					},
				}, nil
			},
		},
	)
}
