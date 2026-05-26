package users

import (
	"log"

	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]string{
				"query": ctx.Query("q"),
			}, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{Title: server.Title{Absolute: "Users"}}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
