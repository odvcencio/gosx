package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

const scene3dDescription = "Go-declared 3D: 28 node types, 11 geometries, 9 materials, 7 lights, 8 post effects, and a server-computed backend verdict."

func init() {
	docsapp.RegisterStaticDocsPage(
		"3D Engine",
		scene3dDescription,
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"mode":        "",
					"title":       "3D Engine",
					"description": scene3dDescription,
					"tags": []string{
						"3d", "webgpu", "webgl", "pbr", "scene-graph",
						"raycasting", "post-processing", "selena",
					},
					"toc": []map[string]string{
						{"href": "#scene-graph", "label": "Scene Graph"},
						{"href": "#no-hierarchical-scale", "label": "No Hierarchical Scale"},
						{"href": "#backends", "label": "Backends & Verdicts"},
						{"href": "#camera-controls", "label": "Camera & Controls"},
						{"href": "#geometry", "label": "Geometry"},
						{"href": "#materials", "label": "Materials"},
						{"href": "#selena", "label": "Selena Shaders"},
						{"href": "#lights", "label": "Lights"},
						{"href": "#shadows", "label": "Shadows"},
						{"href": "#post-processing", "label": "Post-processing"},
						{"href": "#animation", "label": "Animation"},
						{"href": "#transitions", "label": "Transitions"},
						{"href": "#css-scene-state", "label": "CSS-driven Scene State"},
						{"href": "#instancing", "label": "Instancing & LOD"},
						{"href": "#particles", "label": "Points & Particles"},
						{"href": "#water", "label": "Water Simulation"},
						{"href": "#overlays", "label": "Labels, Sprites & HTML"},
						{"href": "#gltf", "label": "glTF Loading"},
						{"href": "#compression", "label": "Compression"},
						{"href": "#raycasting", "label": "Raycasting & Picking"},
						{"href": "#helpers", "label": "Helpers & Editors"},
						{"href": "#physics-audio", "label": "Physics & Audio"},
						{"href": "#quality", "label": "Adaptive Quality"},
						{"href": "#streaming", "label": "Streaming Mutations"},
						{"href": "#native-preview", "label": "Native Preview"},
						{"href": "#assets", "label": "Asset Planning"},
						{"href": "#full-stack-3d", "label": "Full-Stack 3D"},
						{"href": "#node-index", "label": "Node Type Index"},
					},
					"demoScene": DemoScene(),
					"next": map[string]string{
						"href":  "/docs/scene3d-vs-threejs",
						"label": "Scene3D vs three.js",
					},
				}, nil
			},
		},
	)
}
