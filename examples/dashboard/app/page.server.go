package app

import (
	"log"

	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]string{
				"users":   "1,247",
				"active":  "892",
				"revenue": "$48,290",
				"growth":  "+12.5%",
			}, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{Title: server.Title{Absolute: "Dashboard"}}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
