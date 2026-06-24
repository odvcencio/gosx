// Package docs is the top-level page package for the gosx-docs example app.
// This file registers a hidden e2e test fixture for Scene3D motion.
// The fixture serves a Scene3D with a single spinning box, a fixed perspective
// camera, and a directional light. The non-zero Spin auto-emits a motionProgram
// (a motion.Timeline) into the scene IR. When the shared Go WASM runtime is
// loaded, the JS runtime routes that motionProgram through the WASM motion
// exports (__gosx_motion_load / __gosx_motion_tick) once window.__gosx_motion_wasm
// is set; for a stand-alone declarative Scene3D the seam falls through to
// JS-computed spin. Either way the rotation visibly changes pixels frame to
// frame, which the e2e pixel-diff asserts. It should not appear in navigation.
package docs

import (
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/scene"
)

func init() {
	route.RegisterFileModuleCaller(0, route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			props := scene.Props{
				Label:      "Spinning box motion fixture",
				AriaLabel:  "Spinning box motion fixture",
				Background: "#10131a",
				Responsive: scene.Bool(true),
				Camera: scene.PerspectiveCamera{
					Position: scene.Vector3{X: 0, Y: 1.5, Z: 4},
					FOV:      55,
					Near:     0.1,
					Far:      100,
				},
				Graph: scene.NewGraph(
					scene.DirectionalLight{
						ID:        "key-light",
						Color:     "#ffffff",
						Intensity: 1.2,
						Direction: scene.Vector3{X: -0.4, Y: -1, Z: -0.6},
					},
					scene.Mesh{
						ID:       "spinning-box",
						Geometry: scene.BoxGeometry{Width: 1.6, Height: 1.6, Depth: 1.6},
						Material: scene.StandardMaterial{
							Color:     "#4f9dff",
							Roughness: 0.4,
							Metalness: 0.1,
						},
						// Non-zero Spin (radians/sec) about the Y axis. The scene IR
						// lowers this into a GenSpin motion track / motionProgram so the
						// WASM motion seam computes the rotation each frame.
						Spin: scene.Euler{X: 0, Y: 0.8, Z: 0},
					},
				),
			}
			return map[string]any{"scene": props}, nil
		},
	})
}
