package docs

import (
	"math"

	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/scene"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Runtime",
		"Hydration bootstrap, page disposal, and streamed regions cooperate during client-side transitions.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				ctx.LifecycleScript(docsapp.PublicAssetURL("runtime/watch-transport.js"))
				return map[string]any{
					"sceneDemo": scene.Props{
						Width:               720,
						Height:              420,
						Label:               "GoSX Scene3D runtime demo",
						Background:          "#08151f",
						ProgramRef:          "/api/runtime/scene-program",
						Capabilities:        []string{"pointer", "keyboard"},
						CapabilityTier:      "balanced",
						MaxDevicePixelRatio: 1.75,
						DragToRotate:        scene.Bool(true),
						Camera: scene.PerspectiveCamera{
							Position: scene.Vec3(0, 1.1, 6.2),
							FOV:      58,
							Near:     0.15,
							Far:      72,
						},
						Graph: scene.NewGraph(
							scene.Group{
								ID:       "runtime-shell",
								Position: scene.Vec3(0, 0.15, 0),
								Rotation: scene.Rotate(-0.1, 0.24, 0),
								Children: []scene.Node{
									scene.Mesh{
										ID:       "runtime-core",
										Geometry: scene.BoxGeometry{Width: 1.8, Height: 1.2, Depth: 1.2},
										Material: scene.GlassMaterial{
											Color:      "#8de1ff",
											Opacity:    scene.Float(0.38),
											Emissive:   scene.Float(0.06),
											BlendMode:  scene.BlendAlpha,
											RenderPass: scene.RenderAlpha,
										},
										Position:   scene.Vec3(-1.35, 0.55, 0),
										Rotation:   scene.Rotate(0.28, 0.42, 0.08),
										Spin:       scene.Rotate(0.08, 0.28, 0),
										Drift:      scene.Vec3(0, 0.12, 0),
										DriftSpeed: 0.9,
									},
									scene.Label{
										Target:     "runtime-core",
										Text:       "Shared runtime",
										Position:   scene.Vec3(0, 1.05, 0),
										Priority:   5,
										Shift:      scene.Vec3(0.12, 0.16, 0),
										DriftSpeed: 0.8,
										DriftPhase: 0.2,
										Occlude:    true,
									},
									scene.Mesh{
										ID:       "runtime-orb",
										Geometry: scene.SphereGeometry{Radius: 0.8, Segments: 20},
										Material: scene.GlowMaterial{
											Color:      "#ffd36e",
											Opacity:    scene.Float(0.84),
											Emissive:   scene.Float(0.42),
											BlendMode:  scene.BlendAdditive,
											RenderPass: scene.RenderAdditive,
										},
										Position:   scene.Vec3(1.2, 0.95, -0.25),
										Spin:       scene.Rotate(0.14, 0.24, 0.08),
										Drift:      scene.Vec3(0, 0.2, 0),
										DriftSpeed: 1.15,
										DriftPhase: 1.3,
									},
									scene.Label{
										Target:     "runtime-orb",
										Text:       "Request-aware scene state",
										Position:   scene.Vec3(0, 1.2, 0),
										Priority:   4,
										MaxWidth:   220,
										Shift:      scene.Vec3(0.16, 0.1, 0),
										DriftSpeed: 0.95,
										DriftPhase: 1.1,
										Occlude:    true,
									},
									scene.Mesh{
										ID:       "runtime-floor",
										Geometry: scene.PlaneGeometry{Width: 6.6, Height: 4.6},
										Material: scene.MatteMaterial{Color: "#112738"},
										Position: scene.Vec3(0, -1.05, 0),
										Rotation: scene.Rotate(-math.Pi/2, 0, 0),
									},
								},
							},
						),
					},
				}, nil
			},
		},
	)
}
