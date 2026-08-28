package app

import (
	"log"
	"time"

	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"serverTime": time.Now().Format(time.RFC3339),
			}, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{Title: server.Title{Default: "GoSX Example - Home"}}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
