package docs

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage("Galaxy", "GPU particle galaxy with 2,800 particles.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"scene": GalaxyScene(),
			}, nil
		},
	})
}
