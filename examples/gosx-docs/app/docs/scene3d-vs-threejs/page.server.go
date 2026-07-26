package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

const scene3dVsThreeDescription = "A measured comparison: about 90% of Scene3D maps onto three.js, about 35% of three.js maps onto Scene3D, and Scene3D still costs more bytes."

func init() {
	docsapp.RegisterStaticDocsPage(
		"Scene3D vs three.js",
		scene3dVsThreeDescription,
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"mode":        "",
					"title":       "Scene3D vs three.js",
					"description": scene3dVsThreeDescription,
					"tags": []string{
						"3d", "three.js", "comparison", "bundle-size", "webgpu",
					},
					"toc": []map[string]string{
						{"href": "#summary", "label": "Summary"},
						{"href": "#overlap", "label": "Asymmetric Overlap"},
						{"href": "#coverage-table", "label": "Coverage by Area"},
						{"href": "#bytes", "label": "The Byte Comparison"},
						{"href": "#gosx-leads", "label": "Where Scene3D Leads"},
						{"href": "#threejs-leads", "label": "Where three.js Leads"},
						{"href": "#choosing", "label": "Choosing Between Them"},
					},
					"prev": map[string]string{
						"href":  "/docs/scene3d",
						"label": "3D Engine",
					},
				}, nil
			},
		},
	)
}
