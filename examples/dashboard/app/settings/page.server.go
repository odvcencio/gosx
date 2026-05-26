package settings

import (
	"log"

	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{Title: server.Title{Absolute: "Settings"}}, nil
		},
		Actions: route.FileActions{
			"saveSettings": func(ctx *action.Context) error {
				return ctx.Success("Settings saved.", nil)
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
