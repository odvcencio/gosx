package galaxy

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Galaxy",
		"A native particle galaxy rendered without three.js.",
		route.FileModuleOptions{
			Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
				return server.Metadata{
					Title: server.Title{Absolute: "Galaxy | GoSX"},
				}, nil
			},
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"scene": GalaxyScene().GoSXSpreadProps(),
				}, nil
			},
		},
	)
}
