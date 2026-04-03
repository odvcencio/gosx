package docs

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/scene"
	"github.com/odvcencio/gosx/server"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Geometry Zoo",
		"Interactive native 3D primitives rendered inside the routed GoSX application shell.",
		route.FileModuleOptions{
			Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
				return server.Metadata{
					Title: server.Title{Absolute: "Geometry Zoo | GoSX"},
				}, nil
			},
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"scene": scene.Props{
						Width:               920,
						Height:              560,
						Label:               "GoSX Geometry Zoo",
						Background:          "#08151f",
						ProgramRef:          "/api/demos/scene-program",
						Capabilities:        []string{"pointer", "keyboard"},
						CapabilityTier:      "balanced",
						MaxDevicePixelRatio: 1.85,
						DragToRotate:        scene.Bool(true),
						Camera: scene.PerspectiveCamera{
							Position: scene.Vec3(0, 0.18, 6.5),
							FOV:      72,
							Near:     0.15,
							Far:      84,
						},
						Graph: scene.NewGraph(
							scene.Mesh{
								ID:       "zoo-box",
								Geometry: scene.BoxGeometry{Width: 1.5, Height: 1.5, Depth: 1.5},
								Material: scene.MatteMaterial{Color: "#6ab6ff"},
								Position: scene.Vec3(-2.1, 0.48, 0.15),
								Spin:     scene.Rotate(0.12, 0.62, 0),
							},
							scene.Mesh{
								ID:       "zoo-orb",
								Geometry: scene.SphereGeometry{Radius: 0.86, Segments: 20},
								Material: scene.GlassMaterial{
									Color:      "#ffd48f",
									Opacity:    scene.Float(0.42),
									Emissive:   scene.Float(0.08),
									BlendMode:  scene.BlendAlpha,
									RenderPass: scene.RenderAlpha,
								},
								Position:   scene.Vec3(0, -0.16, 1.52),
								Spin:       scene.Rotate(-0.24, 0.84, 0.18),
								Drift:      scene.Vec3(0, 0.16, 0),
								DriftSpeed: 1.08,
								DriftPhase: 0.3,
							},
							scene.Mesh{
								ID:       "zoo-pyramid",
								Geometry: scene.PyramidGeometry{Width: 1.72, Height: 1.72, Depth: 1.72},
								Material: scene.GlowMaterial{
									Color:      "#d6ee82",
									Opacity:    scene.Float(0.78),
									Emissive:   scene.Float(0.38),
									BlendMode:  scene.BlendAdditive,
									RenderPass: scene.RenderAdditive,
								},
								Position:   scene.Vec3(2.16, 0.12, 0.18),
								Spin:       scene.Rotate(0.16, -0.44, 0),
								DriftSpeed: 0.72,
								DriftPhase: 0.7,
							},
							scene.Mesh{
								ID:       "zoo-floor",
								Geometry: scene.PlaneGeometry{Width: 7.4, Height: 7.4},
								Material: scene.MatteMaterial{Color: "#173044"},
								Position: scene.Vec3(0, -1.76, 0.15),
								Rotation: scene.Rotate(-1.16, 0, 0),
							},
						),
					},
					"traits": []map[string]string{
						{
							"kicker": "Pointer",
							"title":  "Pull the camera across the surface.",
							"body":   "The canvas follows live pointer position so the scene stays reactive on desktop and touch hardware.",
						},
						{
							"kicker": "Arrow keys",
							"title":  "Push the palette and motion system.",
							"body":   "Left and right rebalance the zoo while up tightens the camera and warms the materials.",
						},
						{
							"kicker": "Shared runtime",
							"title":  "The engine is part of the route, not a separate app.",
							"body":   "The same server-driven page owns the copy, metadata, links, and native 3D program endpoint.",
						},
					},
				}, nil
			},
		},
	)
}
