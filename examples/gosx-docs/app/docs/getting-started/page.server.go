package docs

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Getting Started",
		"The first-use path: init a project, import the app package for file modules, and get sessions, forms, and metadata without hand wiring.",
		route.FileModuleOptions{},
	)
}
