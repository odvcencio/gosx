package docs

import (
	docs "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

func init() {
	docs.RegisterDocsPage("Deployment", "Build, export, and deploy GoSX applications as single binaries, static sites, or edge bundles.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Deployment",
				"description": "Build, export, and deploy GoSX applications as single binaries, static sites, or edge bundles.",
				"tags":        []string{"build", "deploy", "static", "ssr", "isr", "edge"},
				"toc": []map[string]string{
					{"href": "#build-modes", "label": "Build Modes"},
					{"href": "#static-export", "label": "Static Export"},
					{"href": "#server-deployment", "label": "Server Deployment"},
					{"href": "#isr", "label": "ISR"},
					{"href": "#edge-bundles", "label": "Edge Bundles"},
					{"href": "#docker", "label": "Docker"},
				},
			}, nil
		},
	})
}
