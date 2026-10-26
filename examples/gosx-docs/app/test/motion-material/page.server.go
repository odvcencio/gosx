// Standalone Go/Selena fixture: MaterialAnims automatically loads the framework kernel.
package docs

import (
	"sync"

	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/scene"
)

var motionMaterialFixture = sync.OnceValues(func() (scene.CustomMaterial, error) {
	material, _, err := scene.CompileSelenaMaterial([]byte(`material Pulse {
    param emissive : color = rgb(0.4, 0.55, 0.65)
    surface(geo) -> color {
        let stripe = sin(geo.uv.x * 18.0) * 0.12 + 0.88
        return mix(rgb(0.025, 0.04, 0.07), emissive, stripe)
    }
}`), scene.SelenaMaterialOptions{})
	return material, err
})

func init() {
	route.RegisterFileModuleCaller(0, route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			material, err := motionMaterialFixture()
			if err != nil {
				return nil, err
			}
			props := scene.Props{
				Label:      "Animated material-uniform motion fixture",
				AriaLabel:  "Animated material-uniform motion fixture",
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
						ID:       "glow-cube",
						Geometry: scene.BoxGeometry{Width: 1.6, Height: 1.6, Depth: 1.6},
						Material: material,
						// Oscillator pulse on the emissive (vec4) uniform:
						//   value[i] = Base[i] + Amplitude[i]*sin(t*Freq[i]*2π).
						// Emissive RGB pulses at 0.5Hz; the shader has no time input.
						MaterialAnims: []scene.MaterialUniformAnim{
							{
								Uniform: "emissive",
								Arity:   4,
								Oscillator: &scene.MaterialOscillatorAnim{
									Base:      []float64{0.4, 0.55, 0.65, 1},
									Amplitude: []float64{0.25, 0.35, 0.35, 0},
									Freq:      []float64{0.5, 0.5, 0.5, 0},
								},
							},
						},
					},
				),
			}
			return map[string]any{"scene": props}, nil
		},
	})
}
