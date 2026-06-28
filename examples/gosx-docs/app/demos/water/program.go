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

var waterObjectMaterialLayout = map[string]any{
	"schemaVersion": "selena.descriptor.v1",
	"material":      "WaterObject",
	"uniformBlock": map[string]any{
		"name":    "WaterObjectUniforms",
		"size":    224,
		"binding": 0,
		"fields": []map[string]any{
			{"name": "mvp", "type": "mat4", "offset": 0},
			{"name": "modelMatrix", "type": "mat4", "offset": 64},
			{"name": "lightDir", "type": "vec4", "offset": 128},
			{"name": "poolSize", "type": "vec4", "offset": 144},
			{"name": "baseColor", "type": "vec4", "offset": 160},
			{"name": "params", "type": "vec4", "offset": 176},
			{"name": "texturePassMode", "type": "vec4", "offset": 192},
			{"name": "isTexturePass", "type": "vec4", "offset": 208},
		},
		"defaults": []map[string]any{
			{"name": "lightDir", "values": []float64{2, 3, -1, 0}},
			{"name": "poolSize", "values": []float64{1, 1, 1, 0}},
			{"name": "baseColor", "values": []float64{0.5, 0.5, 0.5, 1}},
			{"name": "params", "values": []float64{256, 0.25, 0, 0}},
			{"name": "texturePassMode", "values": []float64{0, 0, 0, 0}},
			{"name": "isTexturePass", "values": []float64{0, 0, 0, 0}},
		},
	},
	"wgsl": map[string]any{"binding": 0},
	"attributes": []map[string]any{
		{"name": "position", "type": "vec3", "location": 0},
		{"name": "normal", "type": "vec3", "location": 1},
		{"name": "uv", "type": "vec2", "location": 2},
	},
	"textures": []map[string]any{
		{"name": "causticTexture", "wgsl": map[string]any{"textureBinding": 1, "samplerBinding": 2}},
		{"name": "objectShadowTexture", "wgsl": map[string]any{"textureBinding": 3, "samplerBinding": 4}},
	},
	"storageBuffers": []map[string]any{
		{"name": "waterState", "kind": "read-only-storage", "wgsl": map[string]any{"binding": 5}},
	},
}

var waterObjectMaterialUniforms = map[string]any{
	"lightDir":            []float64{2, 3, -1, 0},
	"poolSize":            []float64{1, 1, 1, 0},
	"baseColor":           []float64{0.52, 0.54, 0.56, 1},
	"params":              []float64{256, 0.25, 0, 0},
	"texturePassMode":     []float64{0, 0, 0, 0},
	"isTexturePass":       []float64{0, 0, 0, 0},
	"causticTexture":      "gosx:water:water-main:caustics",
	"objectShadowTexture": "gosx:water:water-main:shadow",
	"waterState":          "gosx:water:water-main:state",
}

var waterDuckMaterialLayout = map[string]any{
	"schemaVersion": "selena.descriptor.v1",
	"material":      "WaterDuck",
	"uniformBlock":  waterObjectMaterialLayout["uniformBlock"],
	"wgsl":          waterObjectMaterialLayout["wgsl"],
	"attributes":    waterObjectMaterialLayout["attributes"],
	"textures": []map[string]any{
		{"name": "modelTexture", "wgsl": map[string]any{"textureBinding": 1, "samplerBinding": 2}},
		{"name": "causticTexture", "wgsl": map[string]any{"textureBinding": 3, "samplerBinding": 4}},
		{"name": "objectShadowTexture", "wgsl": map[string]any{"textureBinding": 5, "samplerBinding": 6}},
	},
	"storageBuffers": []map[string]any{
		{"name": "waterState", "kind": "read-only-storage", "wgsl": map[string]any{"binding": 7}},
	},
}

var waterDuckMaterialUniforms = map[string]any{
	"lightDir":            []float64{2, 3, -1, 0},
	"poolSize":            []float64{1, 1, 1, 0},
	"baseColor":           []float64{1, 1, 1, 1},
	"params":              []float64{256, 1, 0, 0},
	"texturePassMode":     []float64{0, 0, 0, 0},
	"isTexturePass":       []float64{0, 0, 0, 0},
	"modelTexture":        "/water/models/duck/DuckCM.png",
	"causticTexture":      "gosx:water:water-main:caustics",
	"objectShadowTexture": "gosx:water:water-main:shadow",
	"waterState":          "gosx:water:water-main:state",
}

func WaterDemoData() (map[string]any, error) {
	waterDemoOnce.Do(func() {
		controlData, err := waterControlDataJSON()
		if err != nil {
			waterDemoErr = err
			return
		}
		shaderSources, err := waterShaderSources()
		if err != nil {
			waterDemoErr = err
			return
		}

		waterDemoData = map[string]any{
			"waterControlData":                  controlData,
			"waterComputeSource":                waterComputeSourceID,
			"waterMaterialSource":               waterMaterialSourceID,
			"waterComputeSourceFiles":           cloneWaterSourceFiles(waterComputeSourceFiles),
			"waterMaterialSourceFiles":          cloneWaterSourceFiles(waterMaterialSourceFiles),
			"waterObjectMaterialSource":         waterObjectMaterialSourceID,
			"waterObjectMaterialSourceFiles":    cloneWaterSourceFiles(waterObjectMaterialSourceFiles),
			"waterDuckMaterialSource":           waterDuckMaterialSourceID,
			"waterDuckMaterialSourceFiles":      cloneWaterSourceFiles(waterDuckMaterialSourceFiles),
			"waterSeedWGSL":                     shaderSources["waterSeedWGSL"],
			"waterDropWGSL":                     shaderSources["waterDropWGSL"],
			"waterDisplacementWGSL":             shaderSources["waterDisplacementWGSL"],
			"waterSimulationWGSL":               shaderSources["waterSimulationWGSL"],
			"waterNormalWGSL":                   shaderSources["waterNormalWGSL"],
			"waterCausticsWGSL":                 shaderSources["waterCausticsWGSL"],
			"waterPoolVertexWGSL":               shaderSources["waterPoolVertexWGSL"],
			"waterPoolFragmentWGSL":             shaderSources["waterPoolFragmentWGSL"],
			"waterSurfaceVertexWGSL":            shaderSources["waterSurfaceVertexWGSL"],
			"waterSurfaceFragmentWGSL":          shaderSources["waterSurfaceFragmentWGSL"],
			"waterSurfaceBelowFragmentWGSL":     shaderSources["waterSurfaceBelowFragmentWGSL"],
			"waterObjectShadowWGSL":             shaderSources["waterObjectShadowWGSL"],
			"waterObjectMeshShadowVertexWGSL":   shaderSources["waterObjectMeshShadowVertexWGSL"],
			"waterObjectMeshShadowFragmentWGSL": shaderSources["waterObjectMeshShadowFragmentWGSL"],
			"waterObjectMaterialWGSL":           shaderSources["waterObjectMaterialWGSL"],
			"waterObjectMaterialLayout":         waterObjectMaterialLayout,
			"waterObjectMaterialUniforms":       waterObjectMaterialUniforms,
			"waterDuckMaterialWGSL":             shaderSources["waterDuckMaterialWGSL"],
			"waterDuckMaterialLayout":           waterDuckMaterialLayout,
			"waterDuckMaterialUniforms":         waterDuckMaterialUniforms,
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
				"buoyancyRadius":          0.25,
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
				"buoyancyRadius":          0.25,
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
				"buoyancyRadius":            0.25,
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
