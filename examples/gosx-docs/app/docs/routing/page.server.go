package docs

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Routing",
		"File pages, server modules, redirects, rewrites, and action endpoints now live in the same routing model.",
		route.FileModuleOptions{},
	)
}
