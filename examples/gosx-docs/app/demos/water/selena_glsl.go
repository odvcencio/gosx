package docs

import (
	"embed"
	"encoding/json"
	"fmt"

	"m31labs.dev/selena"
)

// waterSelenaFS embeds the selena-authored (`.sel`) water shaders — the sole
// shader source for the jeantimex water port. The demo compiles only the GLES
// (WebGL2) and WGSL (WebGPU) artifacts it serves, plus backend-agnostic host
// binding descriptors. Framework-wide WaterSystem desktop GLSL fields remain
// available, but this page does not pay to compile or transmit unused WebGL1
// artifacts. The former hand-written raw-WGSL trees
// (jeantimex-water.sel/, jeantimex-water.elio/) were retired; the JS client
// keeps builtin SCENE_WATER_* sources only as a last-resort runtime fallback.
//
//go:embed shaders/jeantimex-water.selena/*.sel
var waterSelenaFS embed.FS

// waterSelenaShader maps one logical WaterSystem shader slot to its authored
// Selena source. dataPrefix is the WaterDemoData key prefix the page.gsx reads
// (e.g. "waterSurface" → data.waterSurfaceVertexGLES); descKey is the key under
// the per-shader descriptor map (data.waterShaderDescriptors).
type waterSelenaShader struct {
	descKey    string
	dataPrefix string
	file       string
}

// waterSelenaShaders is the WaterSystemIR-relevant shader set. Each compiles to
// a vertex+fragment pair per backend. The five feedback simulation kernels
// (seed/drop/displacement/simulation/normal) emit a fullscreen vertex + a sim
// fragment for the WebGL ping-pong pass; the render shaders emit their authored
// vertex+fragment.
var waterSelenaShaders = []waterSelenaShader{
	{descKey: "seed", dataPrefix: "waterSeed", file: "seed.sel"},
	{descKey: "drop", dataPrefix: "waterDrop", file: "drop.sel"},
	{descKey: "displacement", dataPrefix: "waterDisplacement", file: "displacement.sel"},
	{descKey: "simulation", dataPrefix: "waterSimulation", file: "simulation.sel"},
	{descKey: "normal", dataPrefix: "waterNormal", file: "normal.sel"},
	{descKey: "caustics", dataPrefix: "waterCaustics", file: "caustics.sel"},
	{descKey: "pool", dataPrefix: "waterPool", file: "pool.sel"},
	{descKey: "objectMaterial", dataPrefix: "waterObjectPass", file: "object-material.sel"},
	{descKey: "duckMaterial", dataPrefix: "waterDuckPass", file: "duck-material.sel"},
	{descKey: "surface", dataPrefix: "waterSurface", file: "surface.sel"},
	{descKey: "surfaceBelow", dataPrefix: "waterSurfaceBelow", file: "surface-below.sel"},
	{descKey: "objectShadow", dataPrefix: "waterObjectShadow", file: "object-shadow.sel"},
	{descKey: "compoundShadow", dataPrefix: "waterCompoundShadow", file: "compound-shadow.sel"},
	{descKey: "objectMeshShadow", dataPrefix: "waterObjectMeshShadow", file: "object-mesh-shadow.sel"},
}

// waterSelenaGLESData compiles every page-relevant Selena water shader to the
// WebGL2 dialect plus a host descriptor. It deliberately emits no desktop GLSL
// keys, keeping unused WebGL1 source out of WaterDemoData and rendered HTML.
func waterSelenaGLESData() (map[string]any, error) {
	out := make(map[string]any, len(waterSelenaShaders)*2+1)
	descriptors := make(map[string]json.RawMessage, len(waterSelenaShaders))
	for _, s := range waterSelenaShaders {
		src, err := waterSelenaFS.ReadFile("shaders/jeantimex-water.selena/" + s.file)
		if err != nil {
			return nil, fmt.Errorf("read selena water shader %s: %w", s.file, err)
		}
		result, err := selena.Compile(src, selena.CompileOptions{
			Targets: []selena.Target{selena.TargetGLES},
		})
		if err != nil {
			return nil, fmt.Errorf("compile selena water shader %s: %w", s.file, err)
		}
		gles, ok := result.Artifact(selena.TargetGLES)
		if !ok || gles.Vertex == "" || gles.Fragment == "" {
			return nil, fmt.Errorf("selena water shader %s did not emit GLES vertex/fragment", s.file)
		}
		layout, err := json.Marshal(result.Layout)
		if err != nil {
			return nil, fmt.Errorf("marshal selena descriptor for %s: %w", s.file, err)
		}
		out[s.dataPrefix+"VertexGLES"] = gles.Vertex
		out[s.dataPrefix+"FragmentGLES"] = gles.Fragment
		descriptors[s.descKey] = json.RawMessage(layout)
	}
	out["waterShaderDescriptors"] = descriptors
	return out, nil
}

// waterSelenaComputeDescKeys are the five feedback simulation kernels. They
// are excluded from waterSelenaRenderWGSLData (which only ever emits
// "<dataPrefix>SelenaWGSL" for the RENDER-kind entries) because they route
// through the generic descriptor-driven Selena feedback-compute host path
// instead (getSelenaComputePipeline/createSelenaComputeBindGroup in
// 16a-scene-webgpu.js) -- see waterSelenaComputeWGSLData below, the compute
// analogue of waterSelenaRenderWGSLData.
var waterSelenaComputeDescKeys = map[string]bool{
	"seed":         true,
	"drop":         true,
	"displacement": true,
	"simulation":   true,
	"normal":       true,
}

// waterSelenaRenderWGSLData compiles every RENDER-kind shader in
// waterSelenaShaders (i.e. every entry except the five feedback compute
// kernels) to WGSL and returns its single combined vertex+fragment module
// source as one new, strictly additive WaterDemoData slot per pass:
// "<dataPrefix>SelenaWGSL" (e.g. "waterPoolSelenaWGSL", "waterSurfaceSelenaWGSL",
// "waterObjectPassSelenaWGSL"). This feeds the generic descriptor-driven Selena
// render path in the WebGPU renderer -- the sole primary WGSL source for
// every water render pass now that the hand-written waterPool*WGSL /
// waterSurface*WGSL / ... shader trees (and the shader_sources.go that used
// to embed them) have been retired. The JS runtime's builtin
// SCENE_WATER_*_SOURCE constants remain as the last-resort runtime
// safety-net fallback (see 16a-scene-webgpu.js).
//
// The host binding descriptor for each WGSL slot is the SAME descriptor
// already exposed at waterShaderDescriptors[descKey] by waterSelenaGLESData
// (compiled from the same .sel source for the GLES target): Selena's
// bindings.Layout is backend-agnostic -- the "wgsl" sub-object (group/binding
// numbers), uniform block field order/offsets and Class tags do not depend on
// which target(s) were requested at compile time. TestWaterSelenaWGSLDescriptorMatchesBindings
// (selena_wgsl_binding_test.go) asserts this equivalence directly, so a single
// compile-to-WGSL call per shader is sufficient; no second descriptor is
// threaded.
//
// This function is the generalization of the pool task's one-off
// waterPoolSelenaWGSLData: it produces the SAME "waterPoolSelenaWGSL" key
// (dataPrefix "waterPool" + "SelenaWGSL") plus one new key per additional
// migrated render pass, so page.gsx/program.go wiring is uniform across every
// pass instead of one bespoke function per shader.
func waterSelenaRenderWGSLData() (map[string]any, error) {
	out := make(map[string]any, len(waterSelenaShaders))
	for _, s := range waterSelenaShaders {
		if waterSelenaComputeDescKeys[s.descKey] {
			continue
		}
		src, err := waterSelenaFS.ReadFile("shaders/jeantimex-water.selena/" + s.file)
		if err != nil {
			return nil, fmt.Errorf("read selena water shader %s: %w", s.file, err)
		}
		result, err := selena.Compile(src, selena.CompileOptions{
			Targets: []selena.Target{selena.TargetWGSL},
		})
		if err != nil {
			return nil, fmt.Errorf("compile selena water shader %s to WGSL: %w", s.file, err)
		}
		wgsl, ok := result.Artifact(selena.TargetWGSL)
		if !ok || wgsl.Source == "" {
			return nil, fmt.Errorf("selena water shader %s did not emit a WGSL artifact", s.file)
		}
		out[s.dataPrefix+"SelenaWGSL"] = wgsl.Source
	}
	return out, nil
}

// waterSelenaComputeWGSLData compiles the five feedback-compute simulation
// kernels (seed/drop/displacement/simulation/normal -- exactly the entries
// waterSelenaComputeDescKeys marks true, i.e. the complement of
// waterSelenaRenderWGSLData's selection) to WGSL and returns each kernel's
// single @compute module as one new, strictly additive WaterDemoData slot:
// "<dataPrefix>SelenaWGSL" (e.g. "waterSeedSelenaWGSL", "waterDisplacementSelenaWGSL").
// This feeds the generic descriptor-driven Selena feedback-compute WebGPU
// path (getSelenaComputePipeline/createSelenaComputeBindGroup,
// 16a-scene-webgpu.js) -- any WaterSystem entry that doesn't carry these slots
// (or whose Selena compute pipeline/bind group fails to build) keeps
// dispatching through the resolved authored-or-hardcoded compute pipeline
// (sceneWaterAuthoredComputePipeline) unchanged.
//
// The host binding descriptor for each WGSL slot is the SAME descriptor
// already exposed at waterShaderDescriptors[descKey] by waterSelenaGLESData,
// exactly like waterSelenaRenderWGSLData's descriptor reuse above (Selena's
// bindings.Layout is backend-agnostic: the "wgsl"/"grid"/"states" sub-objects,
// uniform block field order/offsets, and Class tags don't depend on which
// target(s) were requested at compile time).
func waterSelenaComputeWGSLData() (map[string]any, error) {
	out := make(map[string]any, len(waterSelenaComputeDescKeys))
	for _, s := range waterSelenaShaders {
		if !waterSelenaComputeDescKeys[s.descKey] {
			continue
		}
		src, err := waterSelenaFS.ReadFile("shaders/jeantimex-water.selena/" + s.file)
		if err != nil {
			return nil, fmt.Errorf("read selena water shader %s: %w", s.file, err)
		}
		result, err := selena.Compile(src, selena.CompileOptions{
			Targets: []selena.Target{selena.TargetWGSL},
		})
		if err != nil {
			return nil, fmt.Errorf("compile selena water shader %s to WGSL: %w", s.file, err)
		}
		wgsl, ok := result.Artifact(selena.TargetWGSL)
		if !ok || wgsl.Source == "" {
			return nil, fmt.Errorf("selena water shader %s did not emit a WGSL artifact", s.file)
		}
		out[s.dataPrefix+"SelenaWGSL"] = wgsl.Source
	}
	return out, nil
}
