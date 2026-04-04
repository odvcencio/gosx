package docs

import (
	docs "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

func init() {
	docs.RegisterDocsPage("Getting Started", "Set up a GoSX project from scratch in under a minute.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Getting Started",
				"description": "Set up a GoSX project from scratch in under a minute.",
				"tags":        []string{"quickstart", "init", "setup"},
				"toc": []map[string]string{
					{"href": "#overview", "label": "Overview"},
					{"href": "#install", "label": "Install"},
					{"href": "#create-a-project", "label": "Create a Project"},
					{"href": "#project-structure", "label": "Project Structure"},
					{"href": "#dev-server", "label": "Dev Server"},
					{"href": "#next-steps", "label": "Next Steps"},
				},
			}, nil
		},
	})
}
