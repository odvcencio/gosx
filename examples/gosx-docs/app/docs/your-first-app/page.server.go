package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Your first GoSX app", "Build one GoSX app in four steps with server data, an island, a hub, and a typed Scene3D.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"sample001":   docsapp.DocSample("tutorial/step-01-page-server.go.sample"),
				"sample002":   docsapp.DocSample("tutorial/step-01-page.gsx.sample"),
				"sample003":   docsapp.DocSample("tutorial/step-02-counter-props.go.sample"),
				"sample004":   docsapp.DocSample("tutorial/step-02-page-server.go.sample"),
				"sample005":   docsapp.DocSample("tutorial/step-02-page.gsx.sample"),
				"sample006":   docsapp.DocSample("tutorial/step-03-tab-hub.go.sample"),
				"sample007":   docsapp.DocSample("tutorial/step-03-main.go.sample"),
				"sample008":   docsapp.DocSample("tutorial/step-03-page-server.go.sample"),
				"sample009":   docsapp.DocSample("tutorial/step-03-page.gsx.sample"),
				"sample010":   docsapp.DocSample("tutorial/step-04-page-server.go.sample"),
				"sample011":   docsapp.DocSample("tutorial/step-04-page.gsx.sample"),
				"mode":        "light",
				"title":       "Your first GoSX app",
				"description": "Build one GoSX app in four steps with server data, an island, a hub, and a typed Scene3D.",
				"tags":        []string{"tutorial", "four steps", "about 5 minutes"},
				"toc": []map[string]string{
					{"href": "#step-server-data", "label": "Server data"},
					{"href": "#step-island", "label": "Island"},
					{"href": "#step-hub", "label": "Hub"},
					{"href": "#step-scene3d", "label": "Scene3D"},
				},
			}, nil
		},
	})
}
