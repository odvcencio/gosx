// Package capability is the single source of truth for which Scene3D
// rendering backend can faithfully render a given feature set. Go computes a
// verdict; the JS runtime obeys it. See spec.m31labs-gosx.webgpu-honesty-gate.v0.1.
package capability

import "strings"

type Backend string

const (
	BackendWebGPU   Backend = "webgpu"
	BackendWebGL    Backend = "webgl"
	BackendCanvas2D Backend = "canvas2d"
)

var allBackends = []Backend{BackendWebGPU, BackendWebGL, BackendCanvas2D}

type Feature string

const (
	FeatureSkinning                  Feature = "skinning"
	FeatureIBL                       Feature = "ibl"
	FeatureGPUPicking                Feature = "gpu-picking"
	FeatureLineDashed                Feature = "line-dashed"
	FeatureCustomShader              Feature = "custom-shader"
	FeatureComputeParts              Feature = "compute-particles"
	FeatureGPUCull                   Feature = "gpu-cull"
	FeatureWaterSim                  Feature = "water-simulation"
	FeatureWaterObjectTexturePass    Feature = "water-object-texture-pass"
	FeatureWaterObjectMeshShadowPass Feature = "water-object-mesh-shadow-pass"
	FeatureRectAreaLight             Feature = "rect-area-light"
	FeatureRectAreaSpecular          Feature = "rect-area-specular"
	FeatureLightProbeSH              Feature = "light-probe-sh"
)

// LightKindFeatures returns the features a light of the given LightIR.Kind
// needs for a faithful image. An empty result means every backend that
// declares the kind already shades it right, so no Matrix cell exists to
// disagree with.
//
// Five of the seven GoSX light kinds return nothing:
//
//	ambient      flat term on both GPU backends
//	directional  same BRDF and the same two shadow slots on both
//	point        same distance falloff on both
//	spot         same cone: cos(angle), cos(angle*(1-penumbra)), clamp
//	hemisphere   same sky/ground blend by normal Y
//
// The two that remain carry real gaps. See the Matrix rows below.
//
// Nothing calls this yet. The wire-side collector lives in
// scene/scene_ir.go (collectFeatures), which this change does not own, so the
// runtime reports these two gaps itself through reportIssue in
// 16a-scene-webgpu.js. Wire the call here when collectFeatures next changes.
func LightKindFeatures(kind string) []Feature {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "rect-area":
		return []Feature{FeatureRectAreaLight, FeatureRectAreaSpecular}
	case "light-probe":
		return []Feature{FeatureLightProbeSH}
	}
	return nil
}

// Matrix records which backends implement each feature TODAY. A feature absent
// from the map is supported everywhere. Flip a cell when a renderer gains the
// feature; the drift guard (later task) ties this to renderer manifests.
// custom-shader is per-material (resolved via ShaderResolver), not a flat cell.
var Matrix = map[Feature]map[Backend]bool{
	FeatureSkinning: {BackendWebGPU: true, BackendWebGL: true},
	// False everywhere. The WebGL2 cell read true and no code backed it.
	//
	// Image-based lighting (IBL) needs a prefiltered specular cube, an
	// irradiance cube and a split-sum bidirectional reflectance distribution
	// function (BRDF) lookup table. The WebGL2 path has none of the three. It
	// tone maps the source environment to an 8-bit low-dynamic-range texture
	// through scenePBRTonemapHDRPixels, taps that one equirectangular texture
	// twice, and scales the result by (1.0 - roughness * 0.65). That factor has
	// no derivation. The renderer holds no samplerCube, no textureCubeLod and
	// no u_brdfLUT.
	//
	// sceneAllocateTextureUnits in 15a-scene-postfx-shared.js already reserves
	// three units named irradiance, radiance and brdfLUT, and negotiates them
	// against the cascaded shadow allocator. Only irradiance is ever bound, and
	// what binds into it is the tone-mapped 2D texture, not an irradiance cube.
	// So the unit budget a real consumer needs is solved; the content is not.
	//
	// assetpipe/ibl produces the correct products and pins the convention. See
	// ibl.ConsumerRequirements for the five pieces a consumer must add. Flip
	// this cell when one exists, and not before.
	FeatureIBL: {BackendWebGPU: false, BackendWebGL: false},
	// WebGL picking is implemented by the backend-neutral raycast contract in
	// 17-scene-input.js. The WebGPU ID-buffer implementation is not part of
	// this capability slice, so its cell stays false until those renderer
	// symbols ship in the runtime slice.
	FeatureGPUPicking:   {BackendWebGPU: false, BackendWebGL: true},
	FeatureLineDashed:   {BackendWebGPU: false, BackendWebGL: true},
	FeatureComputeParts: {BackendWebGPU: true, BackendWebGL: false},
	// No browser renderer in this slice owns the compute-cull pipeline yet.
	// Keep both cells false until the renderer supplies indirect draw,
	// survivor compaction, and scale-aware bounds.
	FeatureGPUCull: {BackendWebGPU: false, BackendWebGL: false},
	// Water features are implemented on WebGL2 by the runtime water renderer
	// (createSceneWaterRendererWebGL/createSceneWaterSimWebGL); WebGPU stays the
	// preferred/primary backend, WebGL2 is the honest fallback.
	FeatureWaterSim:               {BackendWebGPU: true, BackendWebGL: true},
	FeatureWaterObjectTexturePass: {BackendWebGPU: true, BackendWebGL: true},
	// The WebGL2 cell read true and no code backed it.
	//
	// The name says mesh shadow, and a mesh shadow rasterizes the caster's own
	// geometry. The WebGL2 water renderer compiles two shadow programs,
	// objectShadow and compoundShadow. Both bind an empty vertex array object
	// and draw one full-screen triangle, so both shade an analytic primitive and
	// neither reads a vertex buffer. The identifier objectMeshShadow appears
	// zero times in 16-scene-webgl.js and sixteen times in
	// 16a-scene-webgpu.js, where renderWaterObjectMeshShadowPass rasterizes real
	// geometry through WGPU_PBR_VERTEX_LAYOUT.
	//
	// A mesh shadow and a primitive shadow are different images, so the cell is
	// false. See the corroboration test in water_shadow_test.go.
	FeatureWaterObjectMeshShadowPass: {BackendWebGPU: true, BackendWebGL: false},
	// rect-area-light: neither browser renderer in this slice shades the
	// authored rectangle. WebGL folds it to a point light; WebGPU has no
	// rect-area branch yet. The runtime slice flips the WebGPU cell only when
	// the polygon-form-factor implementation is present.
	FeatureRectAreaLight: {BackendWebGPU: false, BackendWebGL: false},
	// rect-area-specular: the LTC-fitted specular lobe of a rect-area light.
	//
	// False everywhere, and that is the honest record. A faithful renderer reads
	// fitted 64x64 lookup tables (ltc_1 and ltc_2) to shape the specular
	// highlight of a rectangle. Neither GoSX renderer uploads those tables.
	FeatureRectAreaSpecular: {BackendWebGPU: false, BackendWebGL: false},
	// light-probe-sh: spherical-harmonic probe coefficients.
	//
	// False everywhere. LightProbe.Coefficients survives lowering into
	// LightIR.Coefficients, and then no renderer evaluates the spherical-
	// harmonic basis, so the cell stays false until one does.
	FeatureLightProbeSH: {BackendWebGPU: false, BackendWebGL: false},
}

func supports(b Backend, f Feature) bool {
	row, ok := Matrix[f]
	if !ok {
		return true
	}
	return row[b]
}

type Policy struct{ Required map[Feature]bool }

// DefaultPolicy names the features that exclude a backend rather than degrade
// it. Every other feature keeps the backend and lists the gap under Degraded.
//
// water-object-mesh-shadow-pass is deliberately NOT here, and it used to be.
// It became droppable when its WebGL2 cell went false, because Verdict returns
// an empty Capable list when a required feature excludes every backend. A
// browser without WebGPU would then have no backend for a scene that WebGL2
// draws. WebGL2 runs the simulation, the caustics and the object texture pass,
// and it shades the object shadow from an analytic primitive. Only the shadow
// SHAPE is wrong. A differently shaped shadow beats a blank canvas, so the
// author sees the gap under Degraded and the scene still renders.
//
// gpucull_test.go records the same reasoning for gpu-cull.
func DefaultPolicy() Policy {
	return Policy{Required: map[Feature]bool{
		FeatureSkinning:               true,
		FeatureGPUPicking:             true,
		FeatureWaterSim:               true,
		FeatureWaterObjectTexturePass: true,
	}}
}

type BackendCaps struct {
	Capable  []Backend             `json:"capable"`
	Degraded map[Backend][]Feature `json:"degraded,omitempty"`
	Reasons  []CapReason           `json:"reasons,omitempty"`
}

type CapReason struct {
	Feature  Feature `json:"feature"`
	Excludes Backend `json:"excludes,omitempty"`
	Degrades Backend `json:"degrades,omitempty"`
}

// Verdict computes capable backends + per-backend degradations. required lists
// author-gated backends (empty = no gate).
func Verdict(features []Feature, required []Backend, pol Policy) BackendCaps {
	caps := BackendCaps{Degraded: map[Backend][]Feature{}}
	candidate := allBackends
	if len(required) > 0 {
		candidate = required
	}
	for _, b := range candidate {
		excluded := false
		degraded := []Feature{}
		degradedReasons := []CapReason{}
		for _, f := range features {
			if supports(b, f) {
				continue
			}
			if pol.Required[f] {
				excluded = true
				caps.Reasons = append(caps.Reasons, CapReason{Feature: f, Excludes: b})
				continue
			}
			degraded = append(degraded, f)
			degradedReasons = append(degradedReasons, CapReason{Feature: f, Degrades: b})
		}
		if excluded {
			continue
		}
		if len(degraded) > 0 {
			caps.Degraded[b] = append(caps.Degraded[b], degraded...)
			caps.Reasons = append(caps.Reasons, degradedReasons...)
		}
		caps.Capable = append(caps.Capable, b)
	}
	if len(caps.Degraded) == 0 {
		caps.Degraded = nil
	}
	return caps
}
