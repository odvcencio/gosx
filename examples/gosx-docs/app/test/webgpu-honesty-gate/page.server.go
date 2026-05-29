// Package docs is the top-level page package for the gosx-docs example app.
// This file registers a minimal e2e test fixture for the WebGPU honesty gate.
// The fixture serves a Scene3D with preferWebGPU=true and a skinned model so
// the honesty gate must downgrade to WebGL. It should not appear in navigation.
package docs

import (
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/scene"
)

// fixtureSkinLookup is a static SkinLookup that marks the fixture asset as skinned.
type fixtureSkinLookup struct{}

func (fixtureSkinLookup) Skinned(src string) bool {
	return src == "/_test-fixture/skinned.glb"
}

func init() {
	// Register the build-time skin probe so collectFeatures can detect skinning
	// on the fixture model without loading the GLB binary.
	scene.SetSkinLookup(fixtureSkinLookup{})

	route.RegisterFileModuleCaller(0, route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			t := true
			props := scene.Props{
				PreferWebGPU: &t,
				Responsive:   scene.Bool(true),
				Graph: scene.NewGraph(
					scene.Model{
						ID:  "skinned-fixture",
						Src: "/_test-fixture/skinned.glb",
					},
				),
			}
			return map[string]any{"scene": props}, nil
		},
	})
}
