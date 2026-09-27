package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"GoSX demos",
		"Browse interactive GoSX demos with source files, rendered posters, and verified renderer backends.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				demos, err := GalleryDemos()
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"featured": FeaturedGalleryDemos(demos),
					"groups":   GroupedGalleryDemos(demos),
				}, nil
			},
			Bindings: func(_ *route.RouteContext, _ route.FilePage, _ any) route.FileTemplateBindings {
				return route.FileTemplateBindings{Funcs: map[string]any{
					"demoBackendSummary": demoBackendSummary,
					"demoSourceURL":      demoSourceURL,
					"demoStatusLabel":    demoStatusLabel,
					// Resolves the documentation guides that teach the
					// concepts behind a demo, straight from the shared
					// catalogs; unmapped demos render no guide links.
					"demoGuides": RelatedGuides,
				}}
			},
		},
	)
}
