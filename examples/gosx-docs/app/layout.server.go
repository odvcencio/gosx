package docs

import (
	"log"

	"m31labs.dev/gosx/route"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Bindings: func(ctx *route.RouteContext, page route.FilePage, data any) route.FileTemplateBindings {
			return route.FileTemplateBindings{Values: map[string]any{
				"site": SiteBuildInfo(),
			}}
		},
	}); err != nil {
		log.Fatal(err)
	}
}
