package docs

import (
	_ "embed"
	"sync"

	"m31labs.dev/gosx/scene"
)

//go:embed beam.sel
var beachBeamSource []byte

var beachBeamOnce sync.Once
var beachBeamMaterial scene.CustomMaterial

// Analytic scattering avoids PBR highlights on the air itself. Both renderer
// backends compile the same small Selena surface, cached across page requests.
func blackglassBeamMaterial(strength float64, origin scene.Vector3) scene.CustomMaterial {
	beachBeamOnce.Do(func() {
		var err error
		beachBeamMaterial, _, err = scene.CompileSelenaMaterial(beachBeamSource, scene.SelenaMaterialOptions{
			Standard: scene.StandardMaterial{Color: "#000000", Roughness: 1, BlendMode: scene.BlendAdditive},
		})
		if err != nil {
			panic(err)
		}
	})
	m := beachBeamMaterial
	m.Uniforms = map[string]any{"strength": strength, "origin": []float64{origin.X, origin.Y, origin.Z}}
	return m
}

// The current WebGPU authored-material path binds the aggregate triangle
// stream. Expand these tiny surfaces so direct indices cannot address it as
// though it still held the original shared vertices.
func blackglassShaderGeometry(g scene.BufferGeometry) scene.BufferGeometry {
	out := scene.BufferGeometry{Revision: 1}
	for _, index := range g.Indices {
		out.Positions = append(out.Positions, g.Positions[index*3:index*3+3]...)
		out.Normals = append(out.Normals, g.Normals[index*3:index*3+3]...)
		out.UVs = append(out.UVs, g.UVs[index*2:index*2+2]...)
	}
	return out
}
