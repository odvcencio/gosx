package docs

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Runtime",
		"Hydration bootstrap, page disposal, and streamed regions cooperate during client-side transitions.",
		route.FileModuleOptions{},
	)
}
