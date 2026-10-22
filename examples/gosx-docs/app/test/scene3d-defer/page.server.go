// Package docs is the top-level page package for the gosx-docs example app.
// This file registers an e2e fixture for deferred Scene3D and capability gates.
// It should not appear in navigation. Query: ?gates=a,b sets
// RequiredCapabilities; ?scene=b changes the mount id for the navigation test.
package docs

import (
	"strings"

	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/scene"
)

func init() {
	route.RegisterFileModuleCaller(0, route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			query := ctx.Request.URL.Query()
			var gates []string
			for _, g := range strings.Split(query.Get("gates"), ",") {
				if g = strings.TrimSpace(g); g != "" {
					gates = append(gates, g)
				}
			}
			mountID := "defer-scene-a"
			if query.Get("scene") == "b" {
				mountID = "defer-scene-b"
			}
			props := scene.Props{
				Label:                "Deferred Scene3D fixture",
				AriaLabel:            "Deferred Scene3D fixture",
				Background:           "#10131a",
				Defer:                &scene.Defer{TimeoutMS: 800},
				RequiredCapabilities: gates,
				RequireWebGL:         scene.Bool(true), // headless Chrome has no WebGPU adapter
				Responsive:           scene.Bool(true),
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
						ID:       "defer-box",
						Geometry: scene.BoxGeometry{Width: 1.6, Height: 1.6, Depth: 1.6},
						Material: scene.StandardMaterial{Color: "#4f9dff", Roughness: 0.4, Metalness: 0.1},
					},
				),
			}
			return map[string]any{"scene": props, "mountID": mountID, "next": "/test/scene3d-defer?scene=b"}, nil
		},
	})
}
