package docs

import (
	"embed"
	"fmt"
)

const (
	waterComputeSourceID        = "water/jeantimex-water.elio"
	waterMaterialSourceID       = "water/jeantimex-water.sel"
	waterObjectMaterialSourceID = "water/jeantimex-water.sel#object-material"
	waterDuckMaterialSourceID   = "water/jeantimex-water.sel#duck-material"
)

//go:embed shaders/jeantimex-water.elio/*.elio shaders/jeantimex-water.sel/*.sel
var waterShaderFS embed.FS

// waterShaderSourceFiles maps the public .gsx data keys to colocated authored
// Elio/Selena source modules. The module bodies are WGSL-compatible today; the
// source identity is kept separate so a future Selena/Elio compiler can consume
// the same files without changing the WaterSystem payload contract.
var waterComputeSourceFiles = map[string]string{
	"seedWGSL":         "shaders/jeantimex-water.elio/seed.elio",
	"dropWGSL":         "shaders/jeantimex-water.elio/drop.elio",
	"displacementWGSL": "shaders/jeantimex-water.elio/displacement.elio",
	"simulationWGSL":   "shaders/jeantimex-water.elio/simulation.elio",
	"normalWGSL":       "shaders/jeantimex-water.elio/normal.elio",
}

var waterMaterialSourceFiles = map[string]string{
	"causticsWGSL":                 "shaders/jeantimex-water.sel/caustics.sel",
	"poolVertexWGSL":               "shaders/jeantimex-water.sel/pool.vertex.sel",
	"poolFragmentWGSL":             "shaders/jeantimex-water.sel/pool.fragment.sel",
	"surfaceVertexWGSL":            "shaders/jeantimex-water.sel/surface.vertex.sel",
	"surfaceFragmentWGSL":          "shaders/jeantimex-water.sel/surface.fragment.sel",
	"surfaceBelowFragmentWGSL":     "shaders/jeantimex-water.sel/surface-below.fragment.sel",
	"objectShadowWGSL":             "shaders/jeantimex-water.sel/object-shadow.fragment.sel",
	"objectMeshShadowVertexWGSL":   "shaders/jeantimex-water.sel/object-mesh-shadow.vertex.sel",
	"objectMeshShadowFragmentWGSL": "shaders/jeantimex-water.sel/object-mesh-shadow.fragment.sel",
	"waterObjectMaterialWGSL":      "shaders/jeantimex-water.sel/object-material.sel",
	"waterDuckMaterialWGSL":        "shaders/jeantimex-water.sel/duck-material.sel",
}

var waterObjectMaterialSourceFiles = map[string]string{
	"customVertexWGSL":   "shaders/jeantimex-water.sel/object-material.sel",
	"customFragmentWGSL": "shaders/jeantimex-water.sel/object-material.sel",
}

var waterDuckMaterialSourceFiles = map[string]string{
	"customVertexWGSL":   "shaders/jeantimex-water.sel/duck-material.sel",
	"customFragmentWGSL": "shaders/jeantimex-water.sel/duck-material.sel",
}

var waterShaderSourceFiles = map[string]string{
	"waterSeedWGSL":                     "shaders/jeantimex-water.elio/seed.elio",
	"waterDropWGSL":                     "shaders/jeantimex-water.elio/drop.elio",
	"waterDisplacementWGSL":             "shaders/jeantimex-water.elio/displacement.elio",
	"waterSimulationWGSL":               "shaders/jeantimex-water.elio/simulation.elio",
	"waterNormalWGSL":                   "shaders/jeantimex-water.elio/normal.elio",
	"waterCausticsWGSL":                 "shaders/jeantimex-water.sel/caustics.sel",
	"waterPoolVertexWGSL":               "shaders/jeantimex-water.sel/pool.vertex.sel",
	"waterPoolFragmentWGSL":             "shaders/jeantimex-water.sel/pool.fragment.sel",
	"waterSurfaceVertexWGSL":            "shaders/jeantimex-water.sel/surface.vertex.sel",
	"waterSurfaceFragmentWGSL":          "shaders/jeantimex-water.sel/surface.fragment.sel",
	"waterSurfaceBelowFragmentWGSL":     "shaders/jeantimex-water.sel/surface-below.fragment.sel",
	"waterObjectShadowWGSL":             "shaders/jeantimex-water.sel/object-shadow.fragment.sel",
	"waterObjectMeshShadowVertexWGSL":   "shaders/jeantimex-water.sel/object-mesh-shadow.vertex.sel",
	"waterObjectMeshShadowFragmentWGSL": "shaders/jeantimex-water.sel/object-mesh-shadow.fragment.sel",
	"waterObjectMaterialWGSL":           "shaders/jeantimex-water.sel/object-material.sel",
	"waterDuckMaterialWGSL":             "shaders/jeantimex-water.sel/duck-material.sel",
}

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

func waterShaderSources() (map[string]string, error) {
	sources := make(map[string]string, len(waterShaderSourceFiles))
	for key, filename := range waterShaderSourceFiles {
		body, err := waterShaderFS.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("read water shader source %s: %w", filename, err)
		}
		if len(body) == 0 {
			return nil, fmt.Errorf("water shader source %s is empty", filename)
		}
		sources[key] = string(body)
	}
	return sources, nil
}
