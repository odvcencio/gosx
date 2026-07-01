package docs

import (
	"encoding/json"
	"math"
	"sync"
)

var (
	waterDemoOnce sync.Once
	waterDemoData map[string]any
	waterDemoErr  error
)

const (
	waterDemoHiddenY = 10.0
)

// waterObjectMaterialSelenaUniforms/waterDuckMaterialSelenaUniforms are the
// customUniforms maps for the Selena-compiled object-material.sel/
// duck-material.sel materials (shaders/jeantimex-water.selena/) -- the sole
// materials the water-object/water-duck <Material> blocks in page.gsx bind
// to; the hand-written WaterObject/WaterDuck layout+uniform+WGSL trio they
// used to pair with has been retired now that Selena is the sole primary
// material source for these passes. The Selena port's uniform-block field
// set is a strict simplification of that retired hand-written contract (see
// waterShaderDescriptors["objectMaterial"/"duckMaterial"],
// selena_wgsl_binding_test.go): "poolHeight"/"baseColor"/"isTexturePass"/
// "texturePassMode" are author `param`s (not host-injected context), so a
// literal default here — matching the WaterSystem's static page.gsx config
// (poolHeight=1.0) — is the correct, honest value for BOTH consumers of this
// Material: the main-scene draw (drawPBRObjects, which supplies no
// renderContext) and the reflection/refraction/shadow RTT draws
// (drawWaterObjectMeshObjects, which layers a live renderContext.uniforms map
// with "lightDir" -- the material's one declared `context` field -- on top of
// these customUniforms; see sceneWaterObjectTextureSelenaUniforms in
// 16a-scene-webgpu.js). "grid" is NOT a descriptor uniform field: it feeds the
// generic Selena state-grid uniform (StateGrid{gridWidth,gridHeight}) via
// sceneSelenaGridUniformData's customUniforms.grid fallback (16a-scene-webgpu.js),
// needed because the main-scene draw call site has no per-draw renderContext
// to carry a "grid" value (unlike the RTT draw, which does supply one). "water"
// is the live water heightfield resource ref, keyed to the descriptor's
// declared `state water` name (see waterShaderDescriptors["objectMaterial"].states[0].name).
var waterObjectMaterialSelenaUniforms = map[string]any{
	"poolHeight":      1.0,
	"baseColor":       []float64{0.52, 0.54, 0.56, 1},
	"isTexturePass":   0.0,
	"texturePassMode": 0.0,
	"lightDir":        []float64{2, 3, -1},
	"grid":            256.0,
	"water":           "gosx:water:water-main:state",
}

var waterDuckMaterialSelenaUniforms = map[string]any{
	"poolHeight":      1.0,
	"baseColor":       []float64{1, 1, 1, 1},
	"isTexturePass":   0.0,
	"texturePassMode": 0.0,
	"modelTexture":    "/water/models/duck/DuckCM.png",
	"lightDir":        []float64{2, 3, -1},
	"grid":            256.0,
	"water":           "gosx:water:water-main:state",
}

func WaterDemoData() (map[string]any, error) {
	waterDemoOnce.Do(func() {
		controlData, err := waterControlDataJSON()
		if err != nil {
			waterDemoErr = err
			return
		}
		selenaGLSL, err := waterSelenaGLSLData()
		if err != nil {
			waterDemoErr = err
			return
		}
		selenaRenderWGSL, err := waterSelenaRenderWGSLData()
		if err != nil {
			waterDemoErr = err
			return
		}
		selenaComputeWGSL, err := waterSelenaComputeWGSLData()
		if err != nil {
			waterDemoErr = err
			return
		}

		waterDemoData = map[string]any{
			"waterControlData":                  controlData,
			"waterObjectMaterialSelenaUniforms": waterObjectMaterialSelenaUniforms,
			"waterDuckMaterialSelenaUniforms":   waterDuckMaterialSelenaUniforms,
		}
		// Merge the Selena-compiled GLSL/GLES + descriptor slots. These feed the
		// WebGL/WebGL2 water fallback.
		for key, value := range selenaGLSL {
			waterDemoData[key] = value
		}
		// Merge the Selena-compiled RENDER-pass WGSL slots (one
		// "<dataPrefix>SelenaWGSL" key per pass: seed, drop, displacement,
		// simulation, normal are compute kernels handled by selenaComputeWGSL
		// below; pool, surface, surfaceBelow, caustics, objectMaterial
		// ("waterObjectPass"), duckMaterial ("waterDuckPass"), objectShadow,
		// compoundShadow, objectMeshShadow are RENDER passes handled here). These
		// feed the generic descriptor-driven Selena WebGPU render path -- the
		// sole primary WGSL source for every water render pass now that the
		// hand-written *WGSL shader trees have been retired (the JS runtime's
		// builtin SCENE_WATER_*_SOURCE constants remain as the last-resort
		// runtime safety-net fallback; see 16a-scene-webgpu.js).
		for key, value := range selenaRenderWGSL {
			waterDemoData[key] = value
		}
		// Merge the Selena-compiled feedback-COMPUTE kernel WGSL slots (one
		// "<dataPrefix>SelenaWGSL" key per kernel: seed, drop, displacement,
		// simulation, normal). These feed the generic descriptor-driven Selena
		// feedback-compute WebGPU path -- the sole primary WGSL source for every
		// water compute kernel (builtin SCENE_WATER_COMPUTE_SOURCE remains the
		// last-resort runtime safety-net fallback; see 16a-scene-webgpu.js).
		for key, value := range selenaComputeWGSL {
			waterDemoData[key] = value
		}
		// waterShaderDescriptors (merged from selenaGLSL above) already carries
		// the objectMaterial/duckMaterial host binding descriptors keyed by
		// descKey; expose them as flat top-level keys too so page.gsx's plain
		// <Material> components (which read a literal shaderLayout object, not a
		// keyed sub-lookup -- unlike <WaterSystem>'s single shaderDescriptors
		// prop) can reference them directly.
		if descriptors, ok := waterDemoData["waterShaderDescriptors"].(map[string]json.RawMessage); ok {
			if layout, ok := descriptors["objectMaterial"]; ok {
				waterDemoData["waterObjectMaterialSelenaLayout"] = layout
			}
			if layout, ok := descriptors["duckMaterial"]; ok {
				waterDemoData["waterDuckMaterialSelenaLayout"] = layout
			}
		}
	})
	return waterDemoData, waterDemoErr
}

func waterControlDataJSON() (string, error) {
	payload := map[string]any{
		"hiddenY":     waterDemoHiddenY,
		"inactiveY":   waterDemoHiddenY,
		"physics":     map[string]any{"gravityY": -4, "bounce": 0.7, "defaultBuoyancyScale": 1.1},
		"interaction": map[string]any{"profile": "water-object-drop-orbit", "pointerDrops": true, "keyboard": true, "dropRadius": 0.03, "dropStrength": 0.01},
		"objects": map[string]any{
			"Sphere": map[string]any{
				"id":                      "float-sphere",
				"label":                   "Sphere",
				"objectKind":              "sphere",
				"objectHitTest":           "sphere",
				"objectX":                 -0.4,
				"objectY":                 -0.75,
				"objectZ":                 0.2,
				"objectRadius":            0.25,
				"objectHalfSizeX":         0,
				"objectHalfSizeY":         0,
				"objectHalfSizeZ":         0,
				"buoyancyRadius":          0.31,
				"floorClearance":          0.25,
				"xLimitRadius":            0.25,
				"zLimitRadius":            0.25,
				"meshYOffset":             0,
				"objectDriftX":            0,
				"objectDriftY":            0,
				"objectDriftZ":            0,
				"objectBobAmplitude":      0,
				"objectBobSpeed":          0,
				"objectDisplacementScale": 1,
				"mesh": map[string]any{
					"x": -0.4, "y": -0.75, "z": 0.2,
					"visible": true,
					"spinX":   0, "spinY": 0, "spinZ": 0,
					"driftX": 0, "driftY": 0, "driftZ": 0,
					"bobAmplitude": 0, "bobSpeed": 0,
				},
			},
			"Cube": map[string]any{
				"id":                      "float-cube",
				"label":                   "Cube",
				"objectKind":              "cube",
				"objectHitTest":           "box",
				"objectX":                 -0.4,
				"objectY":                 -0.75,
				"objectZ":                 0.2,
				"objectRadius":            0.25,
				"objectHalfSizeX":         0.25,
				"objectHalfSizeY":         0.25,
				"objectHalfSizeZ":         0.25,
				"buoyancyRadius":          0.31,
				"floorClearance":          0.25,
				"xLimitRadius":            0.25,
				"zLimitRadius":            0.25,
				"meshYOffset":             0,
				"objectDriftX":            0,
				"objectDriftY":            0,
				"objectDriftZ":            0,
				"objectBobAmplitude":      0,
				"objectBobSpeed":          0,
				"objectDisplacementScale": 1,
				"mesh": map[string]any{
					"x": -0.4, "y": -0.75, "z": 0.2,
					"visible": true,
					"width":   0.5, "height": 0.5, "depth": 0.5,
					"rotationX": 0, "rotationY": 0,
					"spinX": 0, "spinY": 0, "spinZ": 0,
					"driftX": 0, "driftY": 0, "driftZ": 0,
					"bobAmplitude": 0, "bobSpeed": 0,
				},
			},
			"TorusKnot": map[string]any{
				"id":                        "float-torus",
				"label":                     "TorusKnot",
				"objectKind":                "compound",
				"objectHitTest":             "mesh",
				"objectSubtype":             "torusKnot",
				"objectX":                   -0.4,
				"objectY":                   -0.87,
				"objectZ":                   0.2,
				"objectRadius":              0.31,
				"objectHalfSizeX":           0,
				"objectHalfSizeY":           0,
				"objectHalfSizeZ":           0,
				"buoyancyRadius":            0.31,
				"floorClearance":            0.13,
				"xLimitRadius":              0.31,
				"zLimitRadius":              0.31,
				"meshYOffset":               0,
				"objectDriftX":              0,
				"objectDriftY":              0,
				"objectDriftZ":              0,
				"objectBobAmplitude":        0,
				"objectBobSpeed":            0,
				"objectDisplacementScale":   0.15,
				"objectDisplacementSpheres": torusKnotDisplacementSpheres(),
				"mesh": map[string]any{
					"x": -0.4, "y": -0.87, "z": 0.2,
					"visible": true,
					"radius":  0.17, "tube": 0.045,
					"rotationX": math.Pi / 2,
					"spinX":     0, "spinY": 0, "spinZ": 0,
					"driftX": 0, "driftY": 0, "driftZ": 0,
					"bobAmplitude": 0, "bobSpeed": 0,
				},
			},
			"Rubber Duck": map[string]any{
				"id":                        "float-duck",
				"label":                     "Rubber Duck",
				"model":                     true,
				"src":                       "/water/models/duck/Duck.gltf",
				"objectKind":                "compound",
				"objectHitTest":             "sphere",
				"objectSubtype":             "duck",
				"objectX":                   0.4,
				"objectY":                   -0.735,
				"objectZ":                   -0.2,
				"objectRadius":              0.25,
				"objectHalfSizeX":           0,
				"objectHalfSizeY":           0,
				"objectHalfSizeZ":           0,
				"buoyancyRadius":            0.31,
				"floorClearance":            0.265,
				"xLimitRadius":              0.25,
				"zLimitRadius":              0.25,
				"meshYOffset":               0,
				"objectDriftX":              0,
				"objectDriftY":              0,
				"objectDriftZ":              0,
				"objectBobAmplitude":        0,
				"objectBobSpeed":            0,
				"objectDisplacementScale":   0.15,
				"objectDisplacementSpheres": duckDisplacementSpheres(),
				"mesh": map[string]any{
					"x": 0.4, "y": -0.735, "z": -0.2,
					"visible":   true,
					"rotationY": 0,
					"scaleX":    1, "scaleY": 1, "scaleZ": 1,
					"bounds": 0.5, "fit": "contain", "fitAlign": "center-min-y",
					"material": "water-duck-material", "castShadow": true, "receiveShadow": true,
					"driftX": 0, "driftY": 0, "driftZ": 0,
					"bobAmplitude": 0, "bobSpeed": 0,
				},
			},
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func torusKnotDisplacementSpheres() []map[string]float64 {
	const (
		segments = 24
		radius   = 0.17
		tube     = 0.045
		p        = 2
		q        = 3
	)
	spheres := make([]map[string]float64, 0, segments)
	for i := 0; i < segments; i++ {
		theta := float64(i) / segments * math.Pi * 2
		radialRadius := radius * (2 + math.Cos(q*theta)) * 0.5
		spheres = append(spheres, map[string]float64{
			"offsetX": radialRadius * math.Cos(p*theta),
			"offsetY": -radius * math.Sin(q*theta) * 0.5,
			"offsetZ": radialRadius * math.Sin(p*theta),
			"radius":  tube * 2,
		})
	}
	return spheres
}

func duckDisplacementSpheres() []map[string]float64 {
	return []map[string]float64{
		{"offsetX": 0, "offsetY": 0, "offsetZ": 0, "radius": 0.15},
		{"offsetX": 0, "offsetY": 0.1, "offsetZ": 0.1, "radius": 0.08},
		{"offsetX": 0, "offsetY": -0.08, "offsetZ": -0.05, "radius": 0.1},
	}
}

// cloneWaterSourceFiles shallow-copies a data-key -> source-file-path map.
// Kept alive here (relocated from the now-deleted shader_sources.go, which
// used to embed the hand-written Elio/Selena shader trees) solely for
// selena_glsl.go's waterSelenaSourceFiles, which advertises the .selena
// source-file provenance of each additive Selena GLSL slot.
func cloneWaterSourceFiles(files map[string]string) map[string]string {
	if len(files) == 0 {
		return nil
	}
	out := make(map[string]string, len(files))
	for key, value := range files {
		out[key] = value
	}
	return out
}
