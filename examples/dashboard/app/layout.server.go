package app

import (
	"fmt"
	"log"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		// footer is composed as one string, not joined from separate text and
		// {dash.version}/{dash.renderedAt} holes in layout.gsx: gosx fmt wraps
		// a long mix of literal text and expression holes onto separate
		// lines, and each hole then renders with the line break and indent
		// around it as literal text. A single hole has nothing to wrap.
		Bindings: func(ctx *route.RouteContext, page route.FilePage, data any) route.FileTemplateBindings {
			return route.FileTemplateBindings{Values: map[string]any{
				"dash": map[string]string{
					"footer": fmt.Sprintf("GoSX v%s — Server rendered at %s", gosx.Version, time.Now().Format("15:04:05")),
				},
			}}
		},
	}); err != nil {
		log.Fatal(err)
	}
}
