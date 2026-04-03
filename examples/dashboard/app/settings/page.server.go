package settings

import (
	"github.com/odvcencio/gosx/action"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
)

func init() {
	route.MustRegisterFileModuleHere(route.FileModuleOptions{
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{Title: server.Title{Absolute: "Settings"}}, nil
		},
		Actions: route.FileActions{
			"saveSettings": func(ctx *action.Context) error {
				return ctx.Success("Settings saved.", nil)
			},
		},
	})
}
