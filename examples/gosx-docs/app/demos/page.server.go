package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Demos",
		"A tour of GoSX capabilities — servers, islands, real-time, simulation, and 3D.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"showreel":   DemoShowreelProgram(),
					"showcase":   ShowcaseDemos(),
					"additional": AdditionalDemos(),
				}, nil
			},
			Bindings: func(_ *route.RouteContext, _ route.FilePage, _ any) route.FileTemplateBindings {
				return route.FileTemplateBindings{Funcs: map[string]any{
					// Resolves the documentation guides that teach the
					// concepts behind a demo, straight from the shared
					// catalogs; unmapped demos render no guide links.
					"demoGuides": RelatedGuides,
				}}
			},
		},
	)
}
