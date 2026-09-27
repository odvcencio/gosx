package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Getting Started", "GoSX is a Go framework for server-rendered web apps.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"sample001":   docsapp.DocSample("getting-started/quickstart-install.bash.sample"),
				"sample002":   docsapp.DocSample("getting-started/quickstart-init.bash.sample"),
				"sample003":   docsapp.DocSample("getting-started/quickstart-run.bash.sample"),
				"sample004":   docsapp.DocSample("getting-started/code-003.text.sample"),
				"sample005":   docsapp.DocSample("getting-started/code-007.bash.sample"),
				"mode":        "light",
				"title":       "Getting Started",
				"description": "GoSX is a Go framework for server-rendered web apps.",
				"toc": []map[string]string{
					{"href": "#quickstart-heading", "label": "Start"},
					{"href": "#prerequisites", "label": "Prerequisites"},
					{"href": "#timing", "label": "Measured time"},
					{"href": "#troubleshooting", "label": "Troubleshooting"},
					{"href": "#project-files", "label": "Project files"},
				},
			}, nil
		},
	})
}
