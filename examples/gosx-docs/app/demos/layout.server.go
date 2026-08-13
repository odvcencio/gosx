package docs

import (
	"log"

	"m31labs.dev/gosx/route"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Bindings: demoLayoutBindings,
	}); err != nil {
		log.Fatal(err)
	}
}

func demoLayoutBindings(*route.RouteContext, route.FilePage, any) route.FileTemplateBindings {
	return route.FileTemplateBindings{Funcs: map[string]any{
		"Demos":         Demos,
		"demoValues":    demoValues,
		"demoSourceURL": demoSourceURL,
	}}
}
