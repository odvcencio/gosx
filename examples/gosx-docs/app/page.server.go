package docs

import "github.com/odvcencio/gosx/route"

func init() {
	RegisterStaticDocsPage(
		"Overview",
		"GoSX is a Go-native web framework with routing, layouts, forms, auth, APIs, and selective runtime in one product.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"features": []map[string]string{
						{
							"kicker": "Platform",
							"title":  "One framework for routes, pages, APIs, forms, sessions, auth, and assets.",
							"body":   "Start from a coherent app shape instead of wiring together a custom stack before product work can even begin.",
							"href":   "/docs/getting-started",
						},
						{
							"kicker": "Routing",
							"title":  "Routing stays obvious as the app grows.",
							"body":   "Pages, nested layouts, not-found boundaries, redirects, and rewrites all live in one route model that stays easy to inspect.",
							"href":   "/docs/routing",
						},
						{
							"kicker": "Actions",
							"title":  "Writes and identity stay inside the framework.",
							"body":   "Forms, validation, flashed state, sessions, CSRF, and auth work together without getting split across extra subsystems.",
							"href":   "/docs/forms",
						},
						{
							"kicker": "Runtime",
							"title":  "Add runtime only where it earns its keep.",
							"body":   "Client navigation, islands, streaming, and richer surfaces extend the app without forcing the whole product into a client-first architecture.",
							"href":   "/docs/runtime",
						},
						{
							"kicker": "Delivery",
							"title":  "Builds are meant to ship, not sit in a demo folder.",
							"body":   "GoSX produces real deployable output, so the same product model can move from local work to production without a rewrite.",
							"href":   "/docs/getting-started",
						},
					},
				}, nil
			},
		},
	)
}
