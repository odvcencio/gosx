package app

import (
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
)

func init() {
	route.MustRegisterFileModuleHere(route.FileModuleOptions{
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
	})
}
