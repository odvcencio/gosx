package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage("Scene3D performance", "Automatic scene preloads, deferred scenes, capability gates, parallel WebGL2 compilation, and phone canvas resolution limits.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode": "", "title": "Scene3D performance",
				"description": "Automatic scene preloads, deferred scenes, capability gates, parallel WebGL2 compilation, and phone canvas resolution limits.",
				"tags":        []string{"scene3d", "performance", "preload", "webgl", "mobile", "defer", "capability gates", "renderer"},
				"toc": []map[string]string{
					{"href": "#preloads", "label": "Scene-driven preloads"},
					{"href": "#deferred", "label": "Deferred scenes"},
					{"href": "#gates", "label": "Capability gates"},
					{"href": "#renderer", "label": "The published renderer"},
					{"href": "#shaders", "label": "Parallel shader compilation"},
					{"href": "#resolution", "label": "Phone canvas resolution"},
					{"href": "#measurement", "label": "Measure the first frame"},
				},
				"prev": map[string]string{"href": "/docs/scene3d", "label": "3D engine"},
			}, nil
		},
	})
}
