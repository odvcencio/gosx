// Package docs is the top-level page package for the gosx-docs example app.
// This file registers a hidden fixture for motion milestone 2: a perspective
// DOM overlay (real, focusable HTML on a rotated 3D plane) and an interactive
// scene node with an accessible name. It is used for real-GPU browser evidence
// and does not appear in navigation.
package docs

import (
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/scene"
)

func init() {
	route.RegisterFileModuleCaller(0, route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			props := scene.Props{
				Label:      "Perspective HTML and interactive node fixture",
				AriaLabel:  "Perspective HTML and interactive node fixture",
				Background: "#10131a",
				Responsive: scene.Bool(true),
				Camera: scene.PerspectiveCamera{
					Position: scene.Vector3{X: 0, Y: 0, Z: 6},
					FOV:      55,
					Near:     0.1,
					Far:      100,
				},
				Graph: scene.NewGraph(
					scene.DirectionalLight{ID: "key", Color: "#ffffff", Intensity: 1.2, Direction: scene.Vector3{X: -0.4, Y: -1, Z: -0.6}},
					scene.Mesh{
						ID:          "action-cube",
						Geometry:    scene.BoxGeometry{Width: 1.2, Height: 1.2, Depth: 1.2},
						Material:    scene.StandardMaterial{Color: "#3a6ea5"},
						Position:    scene.Vector3{X: -2, Y: 0, Z: 0},
						Interactive: true,
						Label:       "Toggle the cube",
					},
					scene.HTML{
						ID:            "plane",
						Mode:          scene.HTMLDOM,
						Perspective:   true,
						Markup:        `<button id="plane-button" type="button">Press on the plane</button>`,
						SurfaceWidth:  3,
						SurfaceHeight: 1,
						Position:      scene.Vector3{X: 1.6, Y: 0, Z: 0},
						Rotation:      scene.Euler{Y: -0.5},
						PointerEvents: "auto",
					},
				),
			}
			return map[string]any{"scene": props}, nil
		},
	})
}
