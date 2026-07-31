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

// allBackends lists every backend Verdict considers when the author names none.
//
// The list stops at three on purpose. GoSX ships five render surfaces, and the
// other two carry no row because a row would answer a question nobody asks:
//
//	render/bundle          the caller picks it in Go; no runtime alternative
//	                       exists to fall back to, and it reports its own gaps
//	                       per RECORD through engine.RenderDiagnostic
//	render/gpu/headless    not a surface at all; it implements gpu.Device, so
//	                       render/bundle.Renderer runs ON it rather than beside it
//
// TestFiveRenderSurfacesReduceToThreeBackends records the reasoning and fails if
// either premise stops holding.
var allBackends = []Backend{BackendWebGPU, BackendWebGL, BackendCanvas2D}

// CANVAS2D BLANKET RULE.
//
// Canvas2D draws exactly two things: line segments and screen-space point
// sprites. Read createSceneCanvasRenderer in 18-scene-canvas.js — it calls
// renderSceneCanvasPoints and renderSceneCanvasWorldBundle, and nothing else.
// It rasterizes no triangle, so it shades no material, reads no light, runs no
// pass and samples no texture.
//
// So a missing canvas2d key in a Matrix row is a STATED blanket exclusion, not
// an oversight: supports(canvas2d, f) returns false for every feature that needs
// a mesh. Add a canvas2d key only when Canvas2D genuinely draws the feature.
//
// One feature earns that key today. See the FeatureLineDashed row.
// TestCanvas2DBlanketExclusionIsStated pins both halves.

type Feature string

const (
	FeatureSkinning                  Feature = "skinning"
	FeatureIBL                       Feature = "ibl"
	FeatureEnvironmentMap            Feature = "environment-map"
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
// collectFeatures in scene/scene_ir.go calls this for every LightIR the graph
// lowerer emits, so a rect-area light or a light probe raises its features on
// the wire. The WebGPU renderer also reports the same two gaps to the author
// itself, through the "rect-area-specular" and "light-probe-sh" issue codes in
// 16a-scene-webgpu.js.
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
// feature; the drift guard in drift_test.go ties this to the renderer manifests.
//
// custom-shader has NO row, and that is a decision rather than an omission. A
// flat cell would have to answer "can this backend draw a custom material", and
// no boolean answers it: the answer depends on which shading language THAT
// material ships. Either value lies. True claims WebGPU serves a material with
// GLSL only; false claims WebGPU serves no custom material even when the author
// wrote WGSL. ShaderResolver asks the per-material question instead, and
// Props.SceneIR applies the answer as a post-filter over Capable. See
// TestCustomShaderHasNoFlatCellOnPurpose in customshader_test.go.
var Matrix = map[Feature]map[Backend]bool{
	// Both GPU backends deform a skinned mesh, so both cells are true.
	//
	// WebGL2 skins in the vertex shader: SCENE_PBR_SKINNED_VERTEX_SOURCE builds
	// skinMatrix from a_joints, a_weights and u_jointMatrices[64].
	// WebGPU skins in a compute pass: SCENE_ELIO_SKIN_LBS_SOURCE runs linear
	// blend skinning over a storage buffer of bone matrices, and
	// webGPUBindElioSkinnedBuffers binds the result as vertex slot 0.
	//
	// The two are NOT equally faithful, and the difference has no cell today.
	// The WGSL kernel writes three floats per vertex — position only. Its 28
	// lines contain the strings "normal" and "tangent" zero times. WebGL2 skins
	// all three: "pos = skinMatrix * pos", "norm = mat3(skinMatrix) * norm" and
	// "tang = mat3(skinMatrix) * tang". So a skinned limb on WebGPU lights from
	// rest-pose normals.
	//
	// TestSkinnedNormalGapIsRecordedNotClaimed states the recommended follow-up:
	// a skinned-normals feature, false on WebGPU and true on WebGL2. It needs a
	// key in both renderer manifests, so it is a reported finding here, not a
	// silent row.
	FeatureSkinning: {BackendWebGPU: true, BackendWebGL: true},
	// False everywhere. The WebGL2 cell read true and no code backed it.
	//
	// Image-based lighting (IBL) needs a prefiltered specular cube, an
	// irradiance cube and a split-sum bidirectional reflectance distribution
	// function (BRDF) lookup table. Both backends now consume the assetpipe
	// split-sum products behind a device gate: 16-scene-webgl.js binds
	// u_iblIrradiance, u_iblRadiance and u_iblBRDFLUT as real samplerCube and
	// sampler2D uniforms and reads them with textureLod(u_iblRadiance, ...);
	// 16a-scene-webgpu.js binds the WGSL equivalents and reads them with
	// textureSampleLevel(iblRadiance, ...). This prose used to claim neither
	// backend had these bindings; that claim went stale when the split-sum
	// shaders merged, and TestIBLIsFalseOnBothBackends corroborates the
	// corrected claim against source on every run.
	//
	// The cell stays false anyway, because the path is not yet universal. The
	// WebGL2 split-sum layout needs 18 fragment texture units (material plus
	// cascaded shadows plus IBL); the spec-minimum device exposes 16. Below
	// that gate (maxUnits >= 18 in 16-scene-webgl.js), the renderer compiles a
	// bounded legacy variant and records the diagnostic
	// "fragment-texture-units<18". Claiming the feature at the Matrix level
	// would hide that deterministic degradation from authors who target
	// spec-minimum hardware. Flip this cell when the split-sum path no longer
	// needs a device gate on either backend, and not before.
	//
	// One derivation gap survives, scoped to the legacy fallback only. The
	// WebGL2 u_hasEnvMap branch — taken when no split-sum products are bound —
	// still scales its specular tap by (1.0 - roughness * 0.65), an ad hoc
	// roughness response with no derivation. render/bundle/lit.go:393 carries
	// the identical factor on the identical line shape in the native
	// equirectangular fallback. A split-sum fit reads roughness through a
	// prefiltered mip chain and a two-term BRDF lookup instead; both fallback
	// paths still do not. render/bundle/lit_drift_test.go's environment-map row
	// pins both halves of that residue.
	//
	// See TestIBLIsFalseOnBothBackends in water_shadow_test.go for the
	// corroboration: both the positive split-sum markers and the staging-gate
	// markers, read from renderer source before the cell is asserted.
	FeatureIBL: {BackendWebGPU: false, BackendWebGL: false},
	// environment-map: does the backend READ Environment.EnvMap at all.
	//
	// This row exists because the ibl row above reads as parity and is not. Both
	// ibl cells are false, correctly, because neither browser backend runs a
	// split-sum fit. That hid a much larger gap underneath: one backend samples
	// the authored image and the other never opens it.
	//
	// Count the three authored identifiers, case-insensitive, over
	// client/js/bootstrap-src:
	//
	//	identifier      16a-scene-webgpu.js   16-scene-webgl.js
	//	envMap                            0                  16
	//	envIntensity                      0                   7
	//	envRotation                       0                   6
	//
	// So an author who writes EnvMap gets a reflection on WebGL2, gets one in a
	// poster (render/bundle/environment.go loads a cube and lit.go samples it),
	// and gets NOTHING on WebGPU — which is the preferred backend, so it is the
	// one most viewers see. Nothing told the author that before this row.
	//
	// The cell is a RECORD, not a plan. It is absent from DefaultPolicy, so it
	// excludes no backend; it adds one name to the WebGPU degraded list, which
	// is the honest report. Implementing the WebGPU environment map is renderer
	// work: a cube texture, a sampler, three uniform lanes and the two taps.
	// Flip this cell when 16a-scene-webgpu.js carries them, and not before.
	// TestWebGPUReadsNoEnvironmentMap in environmentmap_test.go fails on the day
	// it does, and names the three edits the flip needs.
	FeatureEnvironmentMap: {BackendWebGPU: false, BackendWebGL: true},
	// gpu-picking is implemented on both GPU backends.
	//
	// The pick CONTRACT — the gosx:scene3d:input events, the pick/drag/event
	// signal namespaces, and every hit field including the world-space ray — is
	// produced by setupScenePickInteractions in 17-scene-input.js. That function
	// takes no renderer argument and has no backend branch, so a page returns
	// identical pick results whichever GPU backend draws it.
	//
	// On top of that shared contract the WebGPU renderer also implements a true
	// GPU pick: createSceneWebGPUPicker in 16a-scene-webgpu.js rasterizes
	// pickable geometry into an r32uint ID attachment and reads the pointer
	// pixel back with copyTextureToBuffer + mapAsync, following the native
	// design in render/bundle/pick.go. It resolves identity on the GPU and
	// derives every geometric field from the same shared CPU raycast helpers
	// WebGL2 uses, so both backends report the same numbers.
	FeatureGPUPicking: {BackendWebGPU: true, BackendWebGL: true},
	// The WebGL2 cell read true and no code backed it. The true cell also sat on
	// the wrong backend entirely.
	//
	// A dash pattern needs a per-fragment arc-length test or a setLineDash call.
	// Count the evidence:
	//
	//	16-scene-webgl.js       "dash" appears 0 times, case-insensitive
	//	16a-scene-webgpu.js     "lineDash" appears 3 times, all of them in
	//	                        webGPUUnsupportedLineStyles, which REFUSES the draw
	//	18-scene-canvas.js      "dash" appears 16 times, and line 31 calls
	//	                        ctx2d.setLineDash([dashSize, gapSize])
	//
	// So WebGL2 draws a dashed line as a SOLID line, WebGPU drops the line data
	// (webGPUUnsupportedLineStyles gates hasWorldLineData), and Canvas2D is the
	// only backend that draws the dashes. The row now says exactly that, and the
	// canvas2d key is the one earned exception to the blanket rule above.
	//
	// The runtime already knew. sceneWebGPUFeatureGap in 20a-scene-mount-backend.js
	// returns "line-styles" for a dashed scene and routes to WebGL2 — but only
	// when the scene carries NO backendCaps, because that function returns early
	// on a present verdict. So the wrong cell overrode the runtime's own guess and
	// sent dashed scenes to the backend that drops them.
	//
	// See linedashed_test.go. This is a degraded image, not a different scene: a
	// solid line still occupies the same pixels, so the feature stays droppable.
	FeatureLineDashed: {BackendWebGPU: false, BackendWebGL: false, BackendCanvas2D: true},
	// compute-particles means the simulation advances in a GPU compute pass.
	//
	// WebGPU: createSceneComputeParticleSystem in 16b-scene-compute.js owns the
	// state buffer, the params uniform and the compute pipeline, and it accepts an
	// authored WGSL kernel through entry.computeWGSL.
	//
	// WebGL2: the renderer has no compute stage. createComputePipeline,
	// beginComputePass and dispatchWorkgroups each appear 0 times in
	// 16-scene-webgl.js. It substitutes createSceneCPUParticleSystem, a CPU mirror
	// of the same 8-float layout and the same hash RNG, so particles still move.
	// Two limits make the mirror a fallback rather than the feature: it clamps to
	// Math.min(entry.count || 0, 10000), and it cannot run an authored WGSL
	// kernel. See computeparticles_test.go.
	FeatureComputeParts: {BackendWebGPU: true, BackendWebGL: false},
	FeatureGPUCull:      {BackendWebGPU: true, BackendWebGL: false},
	// Water features are implemented on WebGL2 by the runtime water renderer
	// (createSceneWaterRendererWebGL/createSceneWaterSimWebGL); WebGPU stays the
	// preferred/primary backend, WebGL2 is the honest fallback.
	//
	// Both cells corroborated against source. WebGL2 ping-pongs two RGBA32F (or
	// RGBA16F) float render targets and compiles the five lowered GLES programs
	// named in passSpecs: simulation, normal, seed, drop and displacement. WebGPU
	// runs the same five stages through dispatchWaterComputeStage inside a
	// compute pass labelled gosx-water-sim-normal-pass. Neither side fakes the
	// height field. See watersim_test.go.
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
	// rect-area-light: the rectangle's shape drives the shading.
	//
	// WebGPU: rectAreaLightRadiance in 16a-scene-webgpu.js evaluates the
	// analytic polygon form factor over the four world corners the JS side
	// builds in sceneWebGPURectAreaBasis. This is the same term three.js gets
	// from LTC_Evaluate with an identity matrix, so the diffuse response is
	// exact and needs no lookup table.
	//
	// WebGL2: 16-scene-webgl.js maps kind "rect-area" to light type 2, the
	// point light. Width and Height reach the IR and then stop. A rect-area
	// light on WebGL2 has no shape, so this cell is false.
	FeatureRectAreaLight: {BackendWebGPU: true, BackendWebGL: false},
	// rect-area-specular: the LTC-fitted specular lobe of a rect-area light.
	//
	// False everywhere, and that is the honest record. three.js reads two
	// fitted 64x64 lookup tables (ltc_1 and ltc_2) to shape the specular
	// highlight of a rectangle. Neither GoSX renderer uploads those tables.
	// WebGPU substitutes a representative-point GGX lobe, which puts the
	// energy in about the right place but gets the highlight's shape wrong on
	// glossy surfaces. WebGL2 has no rect-area path at all.
	FeatureRectAreaSpecular: {BackendWebGPU: false, BackendWebGL: false},
	// light-probe-sh: spherical-harmonic probe coefficients.
	//
	// False everywhere. LightProbe.Coefficients survives lowering into
	// LightIR.Coefficients, and then no renderer reads it. Both GPU backends
	// shade a probe as a flat ambient term built from Color and Intensity.
	// Ambient is the right fold — a probe carries no position, so a point
	// light would invent a distance falloff — but it is not an SH evaluation,
	// so the cell stays false until one exists.
	FeatureLightProbeSH: {BackendWebGPU: false, BackendWebGL: false},
}

func supports(b Backend, f Feature) bool {
	row, ok := Matrix[f]
	if !ok {
		return true
	}
	return row[b]
}

// Supports reports whether one backend implements one feature today.
//
// It answers the same question Verdict asks per backend, for a caller that
// already knows which backend it cares about. scene/preview uses it to warn that
// a poster shows a term the preferred browser backend will not draw.
//
// Read a false answer as "this backend draws nothing for that feature", not as
// "this backend refuses the scene". DefaultPolicy decides which features exclude
// a backend; every other false cell only degrades it.
func Supports(b Backend, f Feature) bool { return supports(b, f) }

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
