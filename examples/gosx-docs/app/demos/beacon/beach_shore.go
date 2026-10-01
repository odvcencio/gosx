package docs

import (
	_ "embed"
	"sync"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

//go:embed swash.sel
var beachSwashSource []byte

var beachSwashOnce sync.Once
var beachSwashMaterial scene.CustomMaterial

// Keep the mesh static on the CPU. Its shader advances and drains one thin
// ground-following surface without invalidating the scene's retained geometry.
func blackglassBeachAddSwash(p *scene.Props) {
	beachSwashOnce.Do(func() {
		var err error
		beachSwashMaterial, _, err = scene.CompileSelenaMaterial(beachSwashSource, scene.SelenaMaterialOptions{
			Standard: scene.StandardMaterial{Color: "#e7edec", Roughness: .65, BlendMode: scene.BlendAlpha},
			Uniforms: map[string]any{"foamTexture": blackglassBeachModelRoot + "shore-foam.png"},
		})
		if err != nil {
			panic(err)
		}
	})
	brightness := 2.2
	if p.Environment.Sky != nil && p.Environment.Sky.SunDirection.Y < 0 {
		brightness = .8
	}
	material := beachSwashMaterial
	material.Uniforms = map[string]any{"foamTexture": blackglassBeachModelRoot + "shore-foam.png", "brightness": brightness}
	p.Graph.Nodes = append(p.Graph.Nodes,
		scene.Mesh{ID: "shore-swash", Geometry: blackglassShaderGeometry(beachgen.SwashGeometry(beachgen.Seed)), Material: material, DepthWrite: scene.Bool(false), Pickable: scene.Bool(false)})
}
