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
// sprites. Read createSceneCanvasRenderer in 18-scene-canvas.ts — it calls
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
	// FeatureSkyEnvironment tracks only the environment-cube sky mode. Gradient
	// sky draws on every backend, including Canvas2D (a genuine ctx2d gradient
	// fill, not a degrade), so it earns no row: an absent feature is supported
	// everywhere, per the Matrix contract. See sky_test.go.
	FeatureSkyEnvironment Feature = "sky-environment"
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
	// Both GPU backends skin a skinned mesh fully — positions, normals AND
	// tangents — so both cells are true.
	//
	// WebGL2 skins in the vertex shader: SCENE_PBR_SKINNED_VERTEX_SOURCE builds
	// skinMatrix from a_joints, a_weights and u_jointMatrices[64], and applies
	// mat3(skinMatrix) to both the normal and the tangent.
	// WebGPU skins in a compute pass: SCENE_ELIO_SKIN_LBS_SOURCE runs linear
	// blend skinning over a storage buffer of bone matrices and writes three
	// packed regions per vertex — positions at byte 0, joint-matrix-skinned,
	// renormalized normals at paddedCount*12, and tangents (with preserved w)
	// at paddedCount*24. webGPUBindElioSkinnedBuffers binds those regions to
	// vertex slots 0, 1 and 3 of the same output buffer.
	//
	// The two backends differ in mechanism; what the evidence establishes is
	// full attribute coverage — both deform all three vectors with the blended
	// joint matrices, so every position, normal and tangent reaching the draw
	// has been joint-skinned on either backend. Pixel-level lighting parity
	// between the two renderers is a separate question this row does not claim.
	FeatureSkinning: {BackendWebGPU: true, BackendWebGL: true},
	// WebGPU true, WebGL2 false. Both cells are corroborated against renderer
	// source; see ibl_test.go.
	//
	// Image-based lighting (IBL) needs a prefiltered specular cube, an
	// irradiance cube and a split-sum bidirectional reflectance distribution
	// function (BRDF) lookup table. The WebGPU renderer holds all three: group(0)
	// bindings 9-12 bind iblIrradiance/iblRadiance/iblBRDFLUT/iblSampler
	// (16a-scene-webgpu.js), and syncEnvironmentIBL loads the KTX2 products,
	// validates the BRDF model and the roughnessPerLevel mapping, and binds them
	// through the frame bind group every frame with no texture-unit budget to
	// negotiate — RGBA16F cube sampling is unconditional in core WebGPU. A
	// validation failure degrades per frame with a recorded render-truth reason
	// rather than silently shading wrong, so the cell is unconditionally true.
	//
	// WebGL2 holds the same three samplers (u_iblIrradiance, u_iblRadiance,
	// u_iblBRDFLUT, behind #if GOSX_HDR_IBL) and the same runtime asset path
	// (scenePBRUploadEnvironmentMap), but scenePBRHDRIBLAvailable gates the
	// whole branch on MAX_TEXTURE_IMAGE_UNITS >= 20. A Matrix cell answers an
	// unconditional question — "does this backend shade IBL for every
	// authoring scene" — and a device below the gate does not, regardless of
	// what the shader compiles. See assetpipe/ibl/contract.go:38-46 for the
	// gate's rationale and PR-8 (ibl_test.go) for the SH9 irradiance fallback
	// that keeps sub-20-unit devices from losing ambient light entirely.
	//
	// The ad hoc (1.0 - roughness * 0.65) legacy equirect-tap response —
	// unrelated to this row — lives under FeatureEnvironmentMap below.
	FeatureIBL: {BackendWebGPU: true, BackendWebGL: false},
	// environment-map: does the backend READ Environment.EnvMap at all.
	//
	// This row exists because the ibl row above reads as parity and is not.
	// Neither browser backend runs a split-sum fit for this legacy path — WebGL2
	// tone maps and taps one equirectangular texture twice; WebGPU now does the
	// same. Both are the ad hoc (1.0 - roughness * 0.65) legacy response, not
	// split-sum IBL, which is why that critique lives here and not on the ibl
	// row above.
	//
	// WebGPU: group(0) bindings 13/14 bind envMapTex/envMapSampler (dedicated
	// repeat/clamp-to-edge sampler, distinct from iblSampler, for the equirect
	// wrap seam). syncEnvironmentMap loads the authored image through the same
	// wgpuLoadTexture path material albedo maps use, and the fragment shader
	// taps envEquirectUV(N) and envEquirectUV(R) exactly where WebGL2 orders
	// them: after the IBL branch, before the hemisphere fallback. IBL wins —
	// syncEnvironmentMap suppresses the equirect map once ibl.active is true,
	// mirroring WebGL2's iblStatus.active gate — so an author who authors both
	// products on the same scene gets one term, not two stacked ambient terms.
	//
	// See environmentmap_test.go for the corroboration and the identifier
	// counts before this row flipped.
	FeatureEnvironmentMap: {BackendWebGPU: true, BackendWebGL: true},
	// gpu-picking is implemented on both GPU backends.
	//
	// The pick CONTRACT — the gosx:scene3d:input events, the pick/drag/event
	// signal namespaces, and every hit field including the world-space ray — is
	// produced by setupScenePickInteractions in 17-scene-input.ts. That function
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
	//	18-scene-canvas.ts      "dash" appears 16 times, and line 31 calls
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
	// sky-environment: does the backend draw the environment-cube/equirect sky
	// mode. False everywhere at this row's introduction — no backend draws any
	// sky yet. Gradient sky (the other Sky.Mode) draws on every backend
	// including Canvas2D and earns no row of its own; see the const doc.
	// Flip each cell as its draw lands. See sky_test.go.
	FeatureSkyEnvironment: {BackendWebGPU: false, BackendWebGL: false},
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
