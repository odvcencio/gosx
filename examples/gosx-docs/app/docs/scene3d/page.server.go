package docs

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"3D Engine",
		"PBR renderer with WebGPU and WebGL2 backends. Declare scenes in Go, render in the browser.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"mode":        "",
					"title":       "3D Engine",
					"description": "PBR renderer with WebGPU and WebGL2 backends. Declare scenes in Go, render in the browser.",
					"tags":        []string{"3d", "webgl", "webgpu", "pbr", "scene-graph"},
					"toc": []map[string]string{
						{"href": "#scene-graph", "label": "Scene Graph"},
						{"href": "#full-stack-3d", "label": "Full-Stack 3D"},
						{"href": "#camera-controls", "label": "Camera & Controls"},
						{"href": "#geometries", "label": "Geometries"},
						{"href": "#materials", "label": "Materials"},
						{"href": "#lights-shadows", "label": "Lights & Shadows"},
						{"href": "#animation", "label": "Animation"},
						{"href": "#particles", "label": "Particles"},
						{"href": "#gltf-loading", "label": "glTF Loading"},
						{"href": "#instancing", "label": "Instancing"},
					},
					"demoScene": DemoScene(),
				}, nil
			},
		},
	)
}
