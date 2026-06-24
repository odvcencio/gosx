// Package docs is the top-level page package for the gosx-docs example app.
// This file registers a hidden e2e test fixture for Scene3D material-uniform
// motion. The fixture serves a Scene3D with a single mesh whose CustomMaterial
// carries an explicit "emissive" customUniform AND a MaterialAnims Oscillator
// on that same uniform. The non-empty MaterialAnims auto-emits a SEPARATE wire
// program (SceneIR.MaterialMotionProgram, base64-serialized as the JSON
// "materialMotionProgram" key) into the scene IR — independent of any transform
// motion. When the shared Go WASM runtime is loaded and window.__gosx_motion_wasm
// is set, the JS seam applyWasmMaterialMotionFrame (client/js/bootstrap-src/
// 20-scene-mount.js) ticks motion.Eval each frame and writes the evaluated value
// into the mesh's customUniforms["emissive"] bag (read live via
// ctx.mount.__gosxScene3DState). selena re-packs that uniform per frame so the
// emissive pulses black<->white at ~0.5Hz. It should not appear in navigation.
//
// Headless note: a stand-alone declarative Scene3D does not load the Go WASM
// runtime (no island / shared engine on the page), so __gosx_motion_tick is
// undefined and the uniform does not animate under headless Chrome; the visual
// pulse requires a GPU host with a shared-runtime Scene3D fixture. The fixture
// still proves the lowering shipped (materialMotionProgram present, customUniforms
// present), which the e2e hard-asserts.
package docs

import (
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/scene"
)

func init() {
	route.RegisterFileModuleCaller(0, route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
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
						// CustomMaterial carries an explicit "emissive" customUniform
						// (vec4) that the renderer re-packs each frame. The
						// MaterialAnims Oscillator below mutates exactly this uniform.
						Material: scene.CustomMaterial{
							StandardMaterial: scene.StandardMaterial{
								Color:     "#222633",
								Roughness: 0.4,
								Metalness: 0.1,
							},
							Uniforms: map[string]any{
								"emissive": []float64{0, 0, 0, 1},
							},
						},
						// Oscillator pulse on the emissive (vec4) uniform:
						//   value[i] = Base[i] + Amplitude[i]*sin(t*Freq[i]*2π).
						// emissive RGB pulses 0<->1 at 0.5Hz, alpha pinned at 1.
						MaterialAnims: []scene.MaterialUniformAnim{
							{
								Uniform: "emissive",
								Arity:   4,
								Oscillator: &scene.MaterialOscillatorAnim{
									Base:      []float64{0, 0, 0, 1},
									Amplitude: []float64{1, 1, 1, 0},
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
