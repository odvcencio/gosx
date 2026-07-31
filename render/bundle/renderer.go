package bundle

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/compute"
	"m31labs.dev/gosx/render/gpu"
)

// shadowMapSize is the square resolution of each cascaded-shadow-map layer.
// 2048² per cascade × 3 cascades = ~48 MB of depth memory on depth32float.
const shadowMapSize = 2048

// cascadeCount is the number of shadow cascades, fixed at three.
//
// Making it variable needs runtime-sized bind-group arrays, a variable shadow
// texture layer count, a variable scene uniform layout, and a WGSL constant.
// Two cascades would save a whole shadow pass every frame, so the question is
// whether two serve a typical scene.
//
// They do not. TestThreeCascadesBeatTwoOnNearFieldResolution measures it: two
// cascades stretch the near cascade over the first third of the view range
// instead of the first fifth, which coarsens every shadow texel within about 20
// units of the camera by half again. Shadows are looked at close up, so that is
// the wrong place to spend the saving. Three stays.
const cascadeCount = 3

// Renderer consumes engine.RenderBundle values and issues draw calls against
// a gpu.Device. One Renderer instance serves one canvas / one engine runtime.
// Not safe for concurrent use.
type Renderer struct {
	device        gpu.Device
	surface       gpu.Surface
	surfaceFormat gpu.TextureFormat
	depthFormat   gpu.TextureFormat

	// Render-coupled compute extension (M0): external passes plus the
	// per-frame bus of resources they publish for later passes to consume.
	externalPasses []compute.ExternalComputePass
	published      map[string]compute.GPUResource

	// Pipelines created once and reused across frames.
	unlitPipeline            gpu.RenderPipeline
	unlitBGLayout            gpu.BindGroupLayout
	litPipeline              gpu.RenderPipeline
	litBGLayout              gpu.BindGroupLayout
	litMaterialLayout        gpu.BindGroupLayout
	skinnedLitPipeline       gpu.RenderPipeline
	skinnedLitBGLayout       gpu.BindGroupLayout
	skinnedLitMaterialLayout gpu.BindGroupLayout
	skinnedPaletteLayout     gpu.BindGroupLayout
	surfacePipelines         map[string]gpu.RenderPipeline
	surfaceBGLayouts         map[string]gpu.BindGroupLayout
	surfaceMaterialLayouts   map[string]gpu.BindGroupLayout
	surfaceBindGrps          map[string]gpu.BindGroup
	worldLinePipeline        gpu.RenderPipeline
	worldLineBGLayout        gpu.BindGroupLayout
	worldLineBindGrp         gpu.BindGroup
	shadowPipeline           gpu.RenderPipeline
	shadowBGLayout           gpu.BindGroupLayout

	// Scene uniforms (viewProj + 3 lightViewProjs + camera + light + env).
	sceneUniformBuf gpu.Buffer
	// Scene light array. litWGSL reads it as a runtime-sized storage array, so
	// no compile-time light cap exists. The buffer grows by doubling and never
	// shrinks, because a scene that once held many lights normally holds them
	// again on the next frame.
	lightStorageBuf gpu.Buffer
	lightStorageCap int
	// Shadow-pass uniforms: one buffer per cascade, each holding the
	// cascade's lightViewProj (64 bytes). Separate bind groups per buffer.
	shadowUniformBufs [cascadeCount]gpu.Buffer
	shadowBindGrps    [cascadeCount]gpu.BindGroup

	// Per-pipeline bind groups.
	unlitBindGrp      gpu.BindGroup
	litBindGrp        gpu.BindGroup
	skinnedLitBindGrp gpu.BindGroup

	// Main-pass depth attachment, resized lazily to surface.
	depthTex    gpu.Texture
	depthView   gpu.TextureView
	depthWidth  int
	depthHeight int

	// Cascaded shadow map: one 3-layer depth texture array. Per-cascade
	// layer views are used as depth render targets in the shadow passes; the
	// full-array view is bound in the lit main pass for sampling.
	shadowTex        gpu.Texture
	shadowArrayView  gpu.TextureView
	shadowLayerViews [cascadeCount]gpu.TextureView
	shadowSampler    gpu.Sampler
	// shadowCascadesClear is true when the last recorded shadow pass drew
	// nothing, so every cascade still holds the far-plane clear value. Only then
	// may recordShadowPass skip a caster-free frame. It starts false, so the
	// first such frame still runs the pass once and clears.
	shadowCascadesClear bool

	// Shared material texture sampler (separate from the comparison sampler
	// used for shadows; this one does anisotropic color lookup).
	materialSampler gpu.Sampler
	// 1x1 white fallback texture bound when a material has no Texture URL.
	fallbackTexture     *textureResources
	fallbackCubeTexture *textureResources
	envBindGroupKey     string

	// defaultTangentBuf is the single [1,0,0,1] tangent record the lit and
	// skinned-lit pipelines bind at the tangent vertex-attribute slot
	// (location 9 rigid, location 15 skinned; see litWGSL). It is declared
	// with ArrayStride 0, so every vertex of every draw reads the same 16
	// bytes: WebGPU defines a stride-0 vertex buffer layout as "do not
	// advance between vertices," which is exactly the constant-attribute
	// fallback the browser copy gets from filling a real per-vertex array
	// with the same four floats. No lobe reads this attribute yet — see
	// Scene3D parity cluster C PR2.
	defaultTangentBuf gpu.Buffer

	// GPU-driven culling pipeline + layout. Per-mesh resources live in
	// cullCache.
	cullPipeline gpu.ComputePipeline
	cullBGLayout gpu.BindGroupLayout

	// Post-FX present pipeline + HDR intermediate. The main pass writes
	// to hdrTex; the present pass tone-maps that into the swap chain.
	presentPipeline gpu.RenderPipeline
	presentBGLayout gpu.BindGroupLayout
	presentBindGrp  gpu.BindGroup
	presentSampler  gpu.Sampler
	fxaaPipeline    gpu.RenderPipeline
	fxaaBGLayout    gpu.BindGroupLayout
	fxaaBindGrp     gpu.BindGroup
	hdrFormat       gpu.TextureFormat
	hdrTex          gpu.Texture
	hdrView         gpu.TextureView
	hdrWidth        int
	hdrHeight       int
	postFXTex       gpu.Texture
	postFXView      gpu.TextureView
	postFXWidth     int
	postFXHeight    int

	// R4 GPU picking: per-pixel object ID as a second color attachment on
	// the main pass + the async readback state that ties QueuePick to the
	// copy-to-buffer + map-async sequence.
	idBufferTex  gpu.Texture
	idBufferView gpu.TextureView
	pickMu       sync.Mutex
	pendingPick  *pickRequest
	retiredPicks []*pickRequest
	// pickSpans maps runs of pick IDs back to bundle entries. Frame refreshes
	// it with no allocation; a queued pick snapshots it. The renderer no longer
	// builds a per-instance result map every frame.
	pickSpans        []pickSpan
	pickBases        []uint32
	objectPickBases  []uint32
	surfacePickBases []uint32

	// Bloom chain (bright-pass + 2 blur passes → composited into present).
	brightPipeline gpu.RenderPipeline
	brightBGLayout gpu.BindGroupLayout
	blurPipeline   gpu.RenderPipeline
	blurBGLayout   gpu.BindGroupLayout
	bloom          *bloomResources
	bloomSourceKey string

	// Native depth-backed post-FX run in HDR before bloom/present.
	ssaoPipeline       gpu.RenderPipeline
	ssaoBGLayout       gpu.BindGroupLayout
	dofPipeline        gpu.RenderPipeline
	dofBGLayout        gpu.BindGroupLayout
	vignettePipeline   gpu.RenderPipeline
	vignetteBGLayout   gpu.BindGroupLayout
	colorGradePipeline gpu.RenderPipeline
	colorGradeBGLayout gpu.BindGroupLayout
	nativePostFX       *nativePostFXResources
	// Custom post pipelines keyed by "name:wgsl_prefix" (first 128 bytes of
	// source) for dedup. Built lazily when a customPost entry appears in the
	// bundle.  On failure the entry is skipped (identity passthrough for that
	// pass) and an error is printed once.
	customPostCache map[string]*nativeCustomPostResources

	// Compute-particle pipelines. Per-system resources live in particleCache.
	particleUpdatePipeline gpu.ComputePipeline
	particleUpdateBGLayout gpu.BindGroupLayout
	particleRenderPipeline gpu.RenderPipeline
	particleRenderBGLayout gpu.BindGroupLayout

	// Override kernel config stored from New so buildParticlePipelines can use
	// it without plumbing cfg through every call site.
	particleOverrideWGSL       string
	particleOverrideEntryPoint string

	// Per-system authored render pipelines keyed by content hash. Built lazily
	// when a bundle entry carries RenderVertexWGSL/RenderFragmentWGSL.
	particleRenderOverrideCache map[string]*particleRenderOverride
	// Failed authored render keys — avoid re-attempting broken shaders.
	particleRenderOverrideFailed map[string]bool

	// cascadeLambda blends the logarithmic and uniform cascade split schedules.
	// Set from Config.ShadowCascadeLambda at construction.
	cascadeLambda float32

	// Tracks the previous frame's time for particle dt integration.
	lastFrameTime float64

	// Frame stats + device-lost state. Populated on every Frame call.
	stats frameStatsRecorder

	// Caches keyed by identity strings, reused across frames.
	passCache          map[string]*passResources
	primitiveCache     map[string]*primitiveResources
	objectMeshCache    map[string]*objectMeshResources
	surfaceCache       map[string]*surfaceResources
	worldLineCache     *worldLineResources
	materialCache      map[materialFingerprint]*materialResources
	textureCache       map[string]*textureResources
	cullCache          map[string]*cullResources
	particleCache      map[string]*particleResources
	skinCache          map[string]*skinResources
	bonePalettes       map[string]*BonePalette
	defaultBonePalette *BonePalette

	// Per-slot cache for the bundle's InstancedMeshes. prepareMeshStates fills
	// it once per frame; the cull pass, the shadow passes, and the main pass all
	// read from it instead of recomputing cache keys and materials.
	meshStates []meshFrameState

	// materialFPs memoizes one material fingerprint per bundle material index
	// for the frame in progress. Many meshes normally share few materials.
	materialFPs materialFingerprintMemo

	// Reusable encode buffers. Every queue write copies its input, so one
	// buffer per payload shape is enough and a steady-state frame allocates
	// nothing for uniform or instance packing.
	instanceEncodeScratch []byte
	// lightRecords and lightEncodeScratch hold the packed scene lights for the
	// frame in progress. Both only grow, so a steady-state frame allocates
	// nothing for the light array.
	lightRecords         []packedLight
	lightEncodeScratch   []byte
	sceneUniformScratch  [sceneUniformSize]byte
	shadowUniformScratch [64]byte
	cullUniformScratch   [cullUniformSize]byte
	drawArgsScratch      [16]byte

	// Reusable render-pass attachment records. WebGPU consumes a pass
	// descriptor when the pass begins, so one set per pass shape avoids a slice
	// allocation on every frame.
	mainColorAttachments  [2]gpu.RenderPassColorAttachment
	mainDepthAttachment   gpu.RenderPassDepthStencilAttachment
	shadowDepthAttachment [cascadeCount]gpu.RenderPassDepthStencilAttachment
	// postAttachments holds the single color attachment of every fullscreen
	// post pass. Each pass owns its own slot, so a descriptor stays valid for
	// the whole life of its pass.
	postAttachments [postPassSlotCount]gpu.RenderPassColorAttachment
	// vec4Scratch backs vec4Bytes. Every post-FX uniform block is one vec4.
	vec4Scratch [16]byte
	// submitScratch carries the frame's single command buffer into Submit
	// without building a variadic slice.
	submitScratch [1]gpu.CommandBuffer
}

// postPassSlot names one fullscreen post pass. Each slot owns a reusable color
// attachment record in Renderer.postAttachments.
type postPassSlot int

const (
	postSlotBloomBright postPassSlot = iota
	postSlotBloomBlurH
	postSlotBloomBlurV
	postSlotPresentCompose
	postSlotFXAA
	postSlotNativeEffect
	postPassSlotCount
)

// postColorAttachments fills one slot with a clear-then-store color attachment
// and returns it as a one-element slice. The slice header points at Renderer
// memory, so no frame allocates for it.
func (r *Renderer) postColorAttachments(slot postPassSlot, view gpu.TextureView) []gpu.RenderPassColorAttachment {
	r.postAttachments[slot] = gpu.RenderPassColorAttachment{
		View:       view,
		LoadOp:     gpu.LoadOpClear,
		StoreOp:    gpu.StoreOpStore,
		ClearValue: gpu.Color{R: 0, G: 0, B: 0, A: 1},
	}
	return r.postAttachments[slot : slot+1]
}

// vec4Bytes packs four floats into a Renderer-owned 16-byte buffer and returns
// it. WriteBuffer copies its input, so one buffer serves every post-FX uniform
// write in a frame.
func (r *Renderer) vec4Bytes(x, y, z, w float32) []byte {
	out := r.vec4Scratch[:]
	binary.LittleEndian.PutUint32(out[0:4], math.Float32bits(x))
	binary.LittleEndian.PutUint32(out[4:8], math.Float32bits(y))
	binary.LittleEndian.PutUint32(out[8:12], math.Float32bits(z))
	binary.LittleEndian.PutUint32(out[12:16], math.Float32bits(w))
	return out
}

// vec4Cache remembers the last vec4 written to one uniform buffer.
// writeVec4IfChanged uses it to skip repeat uploads: post-FX settings hold
// still across frames, so the repeat write is pure overhead.
type vec4Cache struct {
	value [4]float32
	valid bool
}

func (r *Renderer) writeVec4IfChanged(cache *vec4Cache, buf gpu.Buffer, x, y, z, w float32) {
	if buf == nil {
		return
	}
	next := [4]float32{x, y, z, w}
	if cache.valid && cache.value == next {
		return
	}
	cache.value = next
	cache.valid = true
	r.device.Queue().WriteBuffer(buf, 0, r.vec4Bytes(x, y, z, w))
}

// passResources holds the per-pass GPU buffers for a cached RenderPassBundle.
type passResources struct {
	positions   gpu.Buffer
	colors      gpu.Buffer
	vertexCount int
}

// primitiveResources holds GPU vertex buffers for one instanced-mesh Kind.
// Uploaded once and reused across frames.
type primitiveResources struct {
	positions   gpu.Buffer
	colors      gpu.Buffer
	normals     gpu.Buffer
	uvs         gpu.Buffer
	vertexCount int
}

type objectMeshResources struct {
	positions   gpu.Buffer
	colors      gpu.Buffer
	normals     gpu.Buffer
	uvs         gpu.Buffer
	instance    gpu.Buffer
	positionLen int
	colorLen    int
	normalLen   int
	uvLen       int
	instanceLen int
	vertexCount int
}

type surfaceResources struct {
	positions   gpu.Buffer
	uvs         gpu.Buffer
	pickIDs     gpu.Buffer
	positionLen int
	uvLen       int
	pickIDLen   int
	vertexCount int
}

type worldLineResources struct {
	positions   gpu.Buffer
	colors      gpu.Buffer
	positionLen int
	colorLen    int
	vertexCount int
}

type skinResources struct {
	joints   gpu.Buffer
	weights  gpu.Buffer
	bindPose gpu.Buffer
}

// Config configures a Renderer.
type Config struct {
	// Device is the GPU device to draw on. Required.
	Device gpu.Device
	// Surface is the render surface (typically a canvas). Required.
	Surface gpu.Surface
	// HDRFormat overrides automatic HDR intermediate selection when set.
	HDRFormat gpu.TextureFormat
	// HDRMemoryBudgetBytes controls automatic HDR format selection. Zero uses
	// the renderer default budget.
	HDRMemoryBudgetBytes int
	// ExternalComputePasses are render-coupled compute passes contributed from
	// outside the renderer (e.g. Elio-generated kernels). They run at their
	// declared PassPhase within Frame() and may publish bus resources for the
	// draw to consume.
	ExternalComputePasses []compute.ExternalComputePass

	// ShadowCascadeLambda blends the two cascade split schedules. 0 gives
	// uniform splits, which spend shadow resolution far from the camera. 1 gives
	// logarithmic splits, which spend it close to the camera and leave the far
	// cascade coarse. nil selects 0.5, the same default the JavaScript WebGL
	// backend uses, so both renderers place their cascade edges alike.
	//
	// Reach for this when a scene's near shadows read soft: a camera with a very
	// small near plane pushes the uniform term high, and a larger lambda pulls
	// the first cascade back in.
	ShadowCascadeLambda *float64

	// ParticleUpdateWGSL is an optional replacement for the built-in particle
	// integrator kernel. When non-empty the renderer compiles and uses it for
	// all particle-update dispatches. The kernel must expose the same buffer
	// and uniform binding contract as the built-in (binding 0: uniforms,
	// binding 1: particle storage). If empty the built-in kernel is used.
	ParticleUpdateWGSL string
	// ParticleUpdateEntryPoint is the entry-point name for ParticleUpdateWGSL.
	// Ignored when ParticleUpdateWGSL is empty. Defaults to "main" when
	// ParticleUpdateWGSL is set but this field is left empty.
	ParticleUpdateEntryPoint string
}

// New constructs a Renderer, building all pipelines, uniform buffers, and the
// shadow map up-front so the first Frame call just issues draw commands.
func New(cfg Config) (*Renderer, error) {
	if cfg.Device == nil {
		return nil, errors.New("bundle.New: device is required")
	}
	if cfg.Surface == nil {
		return nil, errors.New("bundle.New: surface is required")
	}
	hdrFormat := cfg.HDRFormat
	if hdrFormat == gpu.FormatUndefined {
		hdrFormat = selectHDRFormat(cfg.Device, cfg.HDRMemoryBudgetBytes)
	} else if !gpu.TextureFormatSupported(cfg.Device, hdrFormat) {
		return nil, fmt.Errorf("bundle.New: HDR format %v is not supported by device", hdrFormat)
	}
	r := &Renderer{
		device:                       cfg.Device,
		surface:                      cfg.Surface,
		surfaceFormat:                cfg.Device.PreferredSurfaceFormat(),
		depthFormat:                  gpu.FormatDepth24Plus,
		hdrFormat:                    hdrFormat,
		surfacePipelines:             make(map[string]gpu.RenderPipeline),
		surfaceBGLayouts:             make(map[string]gpu.BindGroupLayout),
		surfaceMaterialLayouts:       make(map[string]gpu.BindGroupLayout),
		surfaceBindGrps:              make(map[string]gpu.BindGroup),
		passCache:                    make(map[string]*passResources),
		primitiveCache:               make(map[string]*primitiveResources),
		objectMeshCache:              make(map[string]*objectMeshResources),
		surfaceCache:                 make(map[string]*surfaceResources),
		materialCache:                make(map[materialFingerprint]*materialResources),
		textureCache:                 make(map[string]*textureResources),
		cullCache:                    make(map[string]*cullResources),
		externalPasses:               cfg.ExternalComputePasses,
		published:                    make(map[string]compute.GPUResource),
		particleOverrideWGSL:         cfg.ParticleUpdateWGSL,
		particleOverrideEntryPoint:   cfg.ParticleUpdateEntryPoint,
		particleCache:                make(map[string]*particleResources),
		particleRenderOverrideCache:  make(map[string]*particleRenderOverride),
		particleRenderOverrideFailed: make(map[string]bool),
		skinCache:                    make(map[string]*skinResources),
		bonePalettes:                 make(map[string]*BonePalette),
		cascadeLambda:                resolveCascadeLambda(cfg.ShadowCascadeLambda),
	}
	if err := r.buildUniformBuffers(); err != nil {
		return nil, err
	}
	if err := r.buildShadowResources(); err != nil {
		return nil, err
	}
	if err := r.buildMaterialSampler(); err != nil {
		return nil, err
	}
	if _, err := r.ensureFallbackTexture(); err != nil {
		return nil, err
	}
	if err := r.buildDefaultTangentBuffer(); err != nil {
		return nil, err
	}
	if err := r.buildUnlitPipeline(); err != nil {
		return nil, err
	}
	if err := r.buildLitPipeline(); err != nil {
		return nil, err
	}
	if err := r.buildSkinnedLitPipeline(); err != nil {
		return nil, err
	}
	if err := r.buildSurfacePipelines(); err != nil {
		return nil, err
	}
	if err := r.buildWorldLinePipeline(); err != nil {
		return nil, err
	}
	if err := r.buildDefaultBonePalette(); err != nil {
		return nil, err
	}
	if err := r.buildShadowPipeline(); err != nil {
		return nil, err
	}
	if err := r.buildCullPipeline(); err != nil {
		return nil, err
	}
	if err := r.buildPresentSampler(); err != nil {
		return nil, err
	}
	if err := r.buildPresentPipeline(); err != nil {
		return nil, err
	}
	if err := r.buildFXAAPipeline(); err != nil {
		return nil, err
	}
	if err := r.buildBloomPipelines(); err != nil {
		return nil, err
	}
	if err := r.buildNativePostFXPipelines(); err != nil {
		return nil, err
	}
	if err := r.buildParticlePipelines(); err != nil {
		return nil, err
	}
	if err := r.buildBindGroups(); err != nil {
		return nil, err
	}
	// Subscribe to device-loss events so Frame can short-circuit once the
	// backend reports that the GPU context is gone.
	cfg.Device.OnLost(func(reason, message string) {
		r.stats.markLost(reason, message)
	})
	return r, nil
}

// Stats returns a snapshot of the renderer's frame timing + device health.
// Host apps typically call this every 10–30 frames to drive a perf panel.
func (r *Renderer) Stats() FrameStats {
	return r.stats.snapshot()
}

// Destroy releases all GPU resources owned by the Renderer. The device is
// not destroyed — callers retain ownership.
func (r *Renderer) Destroy() {
	for _, p := range r.passCache {
		destroyPassResources(p)
	}
	r.passCache = nil
	for _, p := range r.primitiveCache {
		destroyPrimitiveResources(p)
	}
	r.primitiveCache = nil
	for _, p := range r.objectMeshCache {
		destroyObjectMeshResources(p)
	}
	r.objectMeshCache = nil
	for _, s := range r.surfaceCache {
		destroySurfaceResources(s)
	}
	r.surfaceCache = nil
	destroyWorldLineResources(r.worldLineCache)
	r.worldLineCache = nil
	for _, m := range r.materialCache {
		if m != nil && m.buf != nil {
			m.buf.Destroy()
		}
		if m != nil && m.bindGroup != nil {
			m.bindGroup.Destroy()
		}
		if m != nil && m.skinnedBindGroup != nil {
			m.skinnedBindGroup.Destroy()
		}
		for _, bg := range m.surfaceBindGroups {
			if bg != nil {
				bg.Destroy()
			}
		}
	}
	r.materialCache = nil
	for _, tx := range r.textureCache {
		if tx != nil && tx.tex != nil {
			tx.tex.Destroy()
		}
	}
	r.textureCache = nil
	for _, c := range r.cullCache {
		destroyCullResources(c)
	}
	r.cullCache = nil
	// meshStates hold pointers into the caches just released.
	r.meshStates = nil
	if r.cullPipeline != nil {
		r.cullPipeline.Destroy()
	}
	if r.hdrTex != nil {
		r.hdrTex.Destroy()
		r.hdrTex = nil
	}
	if r.postFXTex != nil {
		r.postFXTex.Destroy()
		r.postFXTex = nil
	}
	if r.presentBindGrp != nil {
		r.presentBindGrp.Destroy()
		r.presentBindGrp = nil
	}
	if r.fxaaBindGrp != nil {
		r.fxaaBindGrp.Destroy()
		r.fxaaBindGrp = nil
	}
	if r.idBufferTex != nil {
		r.idBufferTex.Destroy()
		r.idBufferTex = nil
	}
	if r.bloom != nil {
		destroyBloomResources(r.bloom)
		r.bloom = nil
	}
	if r.nativePostFX != nil {
		destroyNativePostFXResources(r.nativePostFX)
		r.nativePostFX = nil
	}
	if r.brightPipeline != nil {
		r.brightPipeline.Destroy()
	}
	if r.blurPipeline != nil {
		r.blurPipeline.Destroy()
	}
	if r.ssaoPipeline != nil {
		r.ssaoPipeline.Destroy()
	}
	if r.dofPipeline != nil {
		r.dofPipeline.Destroy()
	}
	if r.vignettePipeline != nil {
		r.vignettePipeline.Destroy()
	}
	if r.colorGradePipeline != nil {
		r.colorGradePipeline.Destroy()
	}
	if r.presentPipeline != nil {
		r.presentPipeline.Destroy()
	}
	if r.fxaaPipeline != nil {
		r.fxaaPipeline.Destroy()
	}
	for _, p := range r.particleCache {
		destroyParticleResources(p)
	}
	r.particleCache = nil
	for _, s := range r.skinCache {
		destroySkinResources(s)
	}
	r.skinCache = nil
	if r.defaultBonePalette != nil {
		r.DestroyBonePalette(r.defaultBonePalette)
		r.defaultBonePalette = nil
	}
	if r.particleUpdatePipeline != nil {
		r.particleUpdatePipeline.Destroy()
	}
	if r.particleRenderPipeline != nil {
		r.particleRenderPipeline.Destroy()
	}
	if r.fallbackTexture != nil && r.fallbackTexture.tex != nil {
		r.fallbackTexture.tex.Destroy()
		r.fallbackTexture = nil
	}
	if r.fallbackCubeTexture != nil && r.fallbackCubeTexture.tex != nil {
		r.fallbackCubeTexture.tex.Destroy()
		r.fallbackCubeTexture = nil
	}
	if r.defaultTangentBuf != nil {
		r.defaultTangentBuf.Destroy()
		r.defaultTangentBuf = nil
	}
	if r.depthTex != nil {
		r.depthTex.Destroy()
		r.depthTex = nil
	}
	if r.shadowTex != nil {
		r.shadowTex.Destroy()
		r.shadowTex = nil
	}
	if r.sceneUniformBuf != nil {
		r.sceneUniformBuf.Destroy()
	}
	if r.lightStorageBuf != nil {
		r.lightStorageBuf.Destroy()
		r.lightStorageBuf = nil
		r.lightStorageCap = 0
	}
	for i := range r.shadowUniformBufs {
		if r.shadowUniformBufs[i] != nil {
			r.shadowUniformBufs[i].Destroy()
		}
	}
	if r.unlitPipeline != nil {
		r.unlitPipeline.Destroy()
	}
	if r.litPipeline != nil {
		r.litPipeline.Destroy()
	}
	if r.skinnedLitPipeline != nil {
		r.skinnedLitPipeline.Destroy()
	}
	for _, pipeline := range r.surfacePipelines {
		if pipeline != nil {
			pipeline.Destroy()
		}
	}
	r.surfacePipelines = nil
	for _, bg := range r.surfaceBindGrps {
		if bg != nil {
			bg.Destroy()
		}
	}
	r.surfaceBindGrps = nil
	if r.worldLineBindGrp != nil {
		r.worldLineBindGrp.Destroy()
		r.worldLineBindGrp = nil
	}
	if r.worldLinePipeline != nil {
		r.worldLinePipeline.Destroy()
	}
	if r.shadowPipeline != nil {
		r.shadowPipeline.Destroy()
	}
}

// Frame renders a bundle to the current surface image. The frame runs:
//
//  1. Cull pass — one compute dispatch per instanced mesh against the camera
//     frustum, plus one per shadow cascade against that cascade's light volume.
//     Each dispatch compacts the survivors and writes indirect draw args.
//  2. Shadow passes — one depth-only pass per cascade, drawing that cascade's
//     compacted casters through DrawIndirect.
//  3. Main pass — color + depth into the HDR target, drawing the camera-frustum
//     survivors through DrawIndirect, with the pick ID buffer as a second
//     colour attachment.
//  4. Post chain — native post-FX, bloom, tone map, then FXAA to the surface.
//
// A steady-state frame allocates nothing on the Go heap. prepareMeshStates
// caches every per-mesh key, resource, and material across frames, and
// recordCullPass fingerprints the instance transforms so unchanged geometry
// costs no upload.
//
// Pre-batched Passes data (legacy) still goes through the unlit pipeline and
// does not cast shadows — R3 revisits this when the pass data grows normals.
func (r *Renderer) Frame(b engine.RenderBundle, width, height int, timeSeconds float64) error {
	// Fast-path out of the frame loop when the device has been lost — the
	// host is responsible for tearing down + rebuilding the Renderer on
	// the next resize or lifecycle event.
	if r.stats.isLost() {
		return gpu.ErrDeviceLost
	}

	if width <= 0 || height <= 0 {
		return nil
	}
	b = applyNativeAnimations(b, timeSeconds)
	r.updatePickSpans(b)
	if err := r.prepareMeshStates(b); err != nil {
		return err
	}
	depthView, err := r.ensureDepth(width, height)
	if err != nil {
		return err
	}

	viewProj := computeMVP(b.Camera, width, height)
	lightDir, lightColor, ambientColor := resolveDirectionalLight(b)
	skyColor, groundColor := resolveHemisphereAmbient(b)
	// The cascade fit needs the framebuffer aspect. computeMVP applies it to the
	// projection, so a cascade fitted without it covers the wrong volume.
	cascades := computeCascades(b.Camera, lightDir, r.cascadeLambda, float32(width)/float32(height))

	// The light array has to reach the GPU before the scene uniform, because the
	// uniform carries the count the shader loops to.
	var shadowLightIndex int
	r.lightRecords, shadowLightIndex = resolveSceneLights(b, r.lightRecords)
	if err := r.ensureLightStorage(len(r.lightRecords)); err != nil {
		return err
	}
	lightBytes, lightCount := r.lightStorageBytes(r.lightRecords)
	if lightCount > 0 {
		r.device.Queue().WriteBuffer(r.lightStorageBuf, 0, lightBytes)
	}
	if shadowLightIndex >= lightCount {
		shadowLightIndex = -1
	}

	r.device.Queue().WriteBuffer(r.sceneUniformBuf, 0, r.sceneUniformBytes(sceneUniformBlock{
		viewProj:       viewProj,
		lightViewProjs: cascades.viewProjs,
		cameraPos:      [4]float32{float32(b.Camera.X), float32(b.Camera.Y), float32(b.Camera.Z), 1},
		lightDir:       [4]float32{lightDir[0], lightDir[1], lightDir[2], 0},
		lightColor:     lightColor,
		ambientColor:   ambientColor,
		skyColor:       skyColor,
		groundColor:    groundColor,
		cascadeSplits:  cascades.farSplits,
		envParams:      environmentParams(b.Environment),
		lightParams:    [4]float32{float32(lightCount), float32(shadowLightIndex), 0, 0},
	}))
	for i := 0; i < cascadeCount; i++ {
		putFloat32s(r.shadowUniformScratch[:], cascades.viewProjs[i][:])
		r.device.Queue().WriteBuffer(r.shadowUniformBufs[i], 0, r.shadowUniformScratch[:])
	}

	// Extract frustum planes once per frame for GPU-driven culling.
	frustum := extractFrustumPlanes(viewProj)

	enc := r.device.CreateCommandEncoder()

	// Render-coupled compute: reset the per-frame bus of published resources.
	for k := range r.published {
		delete(r.published, k)
	}

	// 1) GPU-driven culling: compute pass writes a compacted visible-
	// transforms buffer + indirect draw args per InstancedMesh. It also
	// uploads per-mesh source transforms that the shadow pass binds directly.
	if err := r.recordCullPass(enc, b, frustum, cascades); err != nil {
		return err
	}
	// External render-coupled compute that feeds culling/instancing.
	if err := r.runExternalPasses(enc, compute.PhaseAfterCull); err != nil {
		return err
	}
	if err := r.prepareObjectMeshResources(b); err != nil {
		return err
	}

	// 2) One shadow pass per cascade. Shadow passes intentionally bind the
	// unculled per-mesh transform buffers; a shadow caster outside the main
	// frustum can still cast into it. CSM cascades bound the shadow draw volume
	// on their own.
	//
	// A frame with no caster skips all three passes, but only once the cascades
	// are known clear. Read shadowCascadesClear for why the state is needed.
	casters := r.frameCastsShadow(b)
	for i := 0; i < cascadeCount; i++ {
		r.recordShadowPass(enc, b, i, casters)
	}
	r.shadowCascadesClear = !casters

	// 2b) Advance particle state (compute pass). Runs before the main pass
	// so the state storage buffer is ready to be read as vertex data.
	dt := timeSeconds - r.lastFrameTime
	if dt <= 0 || dt > 0.25 {
		// First frame or a stall — clamp to a sensible default step.
		dt = 1.0 / 60.0
	}
	r.lastFrameTime = timeSeconds
	cameraPos := [4]float32{
		float32(b.Camera.X), float32(b.Camera.Y), float32(b.Camera.Z), 1,
	}
	if err := r.recordParticleUpdates(enc, b, dt, timeSeconds, viewProj, cameraPos); err != nil {
		return err
	}
	bloom := resolveBloomConfig(b)
	nativePostFX := resolveNativePostFXEffects(b)

	// The main pass now writes into the HDR intermediate instead of the
	// swap chain. Bloom chain + present pass then tone-map HDR → swap chain.
	hdrView, err := r.ensureHDR(width, height)
	if err != nil {
		return err
	}
	_ = hdrView // main pass picks it up via r.hdrView below
	if err := r.ensureBloom(width, height, bloom); err != nil {
		return err
	}
	r.configureBloom(bloom)
	r.configureToneMap(resolveToneMapConfig(b))
	if err := r.ensurePostFX(width, height); err != nil {
		return err
	}
	if len(nativePostFX) > 0 {
		if _, err := r.ensureNativePostFX(width, height, depthView); err != nil {
			return err
		}
		r.configureNativePostFX(nativePostFX)
	}
	if err := r.ensureEnvironmentBindGroups(b.Environment); err != nil {
		return err
	}

	// External render-coupled compute that produces geometry/instance data
	// (skinning, procedural meshing) consumed by the main pass.
	if err := r.runExternalPasses(enc, compute.PhaseBeforeMain); err != nil {
		return err
	}

	// 3) Main pass — lit scene rendered to the HDR intermediate with depth,
	// plus the GPU picking id buffer as a second color attachment.
	r.mainColorAttachments[0] = gpu.RenderPassColorAttachment{
		View:       r.hdrView,
		LoadOp:     gpu.LoadOpClear,
		StoreOp:    gpu.StoreOpStore,
		ClearValue: parseBackground(b.Background),
	}
	// pick ID = 0 means "background / not a pickable surface".
	r.mainColorAttachments[1] = gpu.RenderPassColorAttachment{
		View:       r.idBufferView,
		LoadOp:     gpu.LoadOpClear,
		StoreOp:    gpu.StoreOpStore,
		ClearValue: gpu.Color{R: 0, G: 0, B: 0, A: 0},
	}
	r.mainDepthAttachment = gpu.RenderPassDepthStencilAttachment{
		View:            depthView,
		DepthLoadOp:     gpu.LoadOpClear,
		DepthStoreOp:    gpu.StoreOpStore,
		DepthClearValue: 1.0,
	}
	mainPass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments:       r.mainColorAttachments[:],
		DepthStencilAttachment: &r.mainDepthAttachment,
		Label:                  "bundle.main",
	})

	// Unlit pre-batched passes (legacy RenderPassBundle).
	if len(b.Passes) > 0 {
		mainPass.SetPipeline(r.unlitPipeline)
		mainPass.SetBindGroup(0, r.unlitBindGrp)
		for _, pb := range b.Passes {
			res, err := r.ensurePassBuffers(pb)
			if err != nil {
				mainPass.End()
				return err
			}
			if res == nil || res.vertexCount == 0 {
				continue
			}
			mainPass.SetVertexBuffer(0, res.positions)
			mainPass.SetVertexBuffer(1, res.colors)
			mainPass.Draw(res.vertexCount, 1, 0, 0)
		}
	}

	// Lit instanced meshes. prepareMeshStates already resolved each slot's
	// primitive buffers, material bind group, and cull resources before the
	// first pass opened, because a material bind group may write its backing
	// uniform buffer and a queue write inside an open pass is illegal.
	if len(r.meshStates) > 0 {
		mainPass.SetPipeline(r.litPipeline)
		mainPass.SetBindGroup(0, r.litBindGrp)
		for i := range r.meshStates {
			st := &r.meshStates[i]
			if !st.drawable || st.skinned {
				continue
			}
			inst, args, ok := r.instanceDrawSource(st)
			if !ok {
				// Unreachable: prepareMeshStates gives every drawable slot cull
				// resources. Skip rather than bind a nil buffer, which would
				// fault the device.
				continue
			}
			mainPass.SetBindGroup(1, st.mat.bindGroup)
			mainPass.SetVertexBuffer(0, st.prim.positions)
			mainPass.SetVertexBuffer(1, st.prim.colors)
			mainPass.SetVertexBuffer(2, st.prim.normals)
			mainPass.SetVertexBuffer(3, st.prim.uvs)
			mainPass.SetVertexBuffer(4, inst)
			mainPass.SetVertexBuffer(5, r.defaultTangentBuf)
			mainPass.DrawIndirect(args, 0)
		}

		mainPass.SetPipeline(r.skinnedLitPipeline)
		mainPass.SetBindGroup(0, r.skinnedLitBindGrp)
		for i := range r.meshStates {
			st := &r.meshStates[i]
			if !st.drawable || !st.skinned {
				continue
			}
			inst, args, ok := r.instanceDrawSource(st)
			if !ok {
				continue
			}
			im := b.InstancedMeshes[i]
			skin, err := r.ensureSkinBuffers(st.key, st.vertexCount, im)
			if err != nil {
				mainPass.End()
				return err
			}
			palette := r.bonePaletteForMesh(im)
			if palette == nil || palette.bindGroup == nil {
				mainPass.End()
				return fmt.Errorf("bundle.Frame: skinned mesh %q has no bone palette", im.ID)
			}
			mainPass.SetBindGroup(1, st.mat.skinnedBindGroup)
			mainPass.SetBindGroup(2, palette.bindGroup)
			mainPass.SetVertexBuffer(0, st.prim.positions)
			mainPass.SetVertexBuffer(1, st.prim.colors)
			mainPass.SetVertexBuffer(2, st.prim.normals)
			mainPass.SetVertexBuffer(3, st.prim.uvs)
			mainPass.SetVertexBuffer(5, skin.joints)
			mainPass.SetVertexBuffer(6, skin.weights)
			mainPass.SetVertexBuffer(7, skin.bindPose)
			mainPass.SetVertexBuffer(4, inst)
			mainPass.SetVertexBuffer(8, r.defaultTangentBuf)
			mainPass.DrawIndirect(args, 0)
		}
	}

	if err := r.drawObjectMeshes(mainPass, b); err != nil {
		mainPass.End()
		return err
	}
	if err := r.drawSurfaces(mainPass, b); err != nil {
		mainPass.End()
		return err
	}
	if err := r.drawWorldLines(mainPass, b); err != nil {
		mainPass.End()
		return err
	}

	// Particles last in the main pass so they composite additively over the
	// opaque lit geometry, with depth test but no depth write.
	r.drawParticles(mainPass, b)

	mainPass.End()

	// 3b) If a pick is queued, copy the requested pixel from the id buffer
	// into a staging buffer for async readback after submission. Must run
	// between the main pass (which writes the id buffer) and any later
	// passes that might clobber it.
	r.recordPickCopy(enc, b, width, height)

	// External render-coupled compute that runs after the main pass resolves
	// into the HDR target, before the post chain consumes it (e.g. screen-space
	// effects driven by Elio kernels — lens distortion, custom grading).
	if err := r.runExternalPasses(enc, compute.PhaseBeforePostFX); err != nil {
		return err
	}

	hdrSourceView, hdrSourceScratch := r.recordNativePostFXPasses(enc, nativePostFX)
	hdrSourceKey := "hdr"
	if hdrSourceScratch {
		hdrSourceKey = "native"
	}
	if err := r.ensureBloomSourceBindGroups(hdrSourceView, hdrSourceKey); err != nil {
		return err
	}

	// 4) Optional bloom chain (bright-pass + horizontal + vertical blurs).
	if bloom.enabled {
		r.recordBloomPasses(enc)
	}

	// 5) Present compose — HDR + optional bloom → ACES tone map → LDR post-FX.
	r.recordPresentPass(enc)

	// 6) Dedicated FXAA 3.11 pass — final LDR image → swap chain.
	surfaceView, err := r.device.AcquireSurfaceView(r.surface)
	if err != nil {
		return fmt.Errorf("bundle.Frame: acquire surface view: %w", err)
	}
	r.recordFXAAPass(enc, surfaceView)

	// Pass the command buffer through a reusable one-element array. A variadic
	// call site would build a fresh slice, and Submit takes an interface, so
	// escape analysis cannot keep that slice on the stack.
	r.submitScratch[0] = enc.Finish()
	r.device.Queue().Submit(r.submitScratch[:]...)
	r.submitScratch[0] = nil

	// After submission, kick off the async pick readback if one was queued.
	// Runs in a goroutine — the frame completes immediately.
	r.finishPickReadback()

	// Record frame timing for Stats(). dt was already computed above for
	// particle integration; reuse it here so the numbers match.
	r.stats.record(dt)
	return nil
}

// instanceDrawSource selects the instance buffer + indirect-draw args for one
// InstancedMesh draw, preferring an external compute pass's published output
// (RoleInstanceAttr + RoleIndirectArgs under "<key>.instances"/"<key>.drawArgs")
// over the renderer's built-in GPU cull. This is the consumption side of the
// render-coupled compute bus: an Elio-generated culling/instancing pass drives
// the draw in place of the engine cull. Returns ok=false when neither source is
// available, so the caller falls back to an unculled draw.
func (r *Renderer) instanceDrawSource(st *meshFrameState) (instances, drawArgs gpu.Buffer, ok bool) {
	if len(r.published) > 0 {
		if inst, iok := r.published[st.key+".instances"]; iok {
			if args, aok := r.published[st.key+".drawArgs"]; aok &&
				inst.Role == compute.RoleInstanceAttr && args.Role == compute.RoleIndirectArgs &&
				inst.Buffer != nil && args.Buffer != nil {
				return inst.Buffer, args.Buffer, true
			}
		}
	}
	if st.cull != nil {
		return st.cull.outputBuf, st.cull.drawArgsBuf, true
	}
	return nil, nil, false
}

// InstancedMeshKey is the bus key under which the instanced mesh im — at draw
// slot idx in the bundle — resolves its draw-source resources. An external
// compute pass (e.g. an Elio-generated cull) that publishes "<key>.instances"
// and "<key>.drawArgs" drives that mesh's draw in place of the built-in cull
// (see instanceDrawSource). Exposed so external pass authors can target a
// specific mesh's draw without replicating the key construction.
func InstancedMeshKey(idx int, im engine.RenderInstancedMesh) string {
	return instancedMeshKey(idx, im)
}

// instancedMeshKey returns the cull/skin-cache key for one InstancedMesh slot.
// Combines the bundle index with the full primitive key so entries with the
// same Kind but different authored geometry parameters do not share stale
// vertex-count-dependent resources.
//
// Frame does not call this per draw. prepareMeshStates computes the key once
// per slot and reuses it until the slot's geometry parameters change.
func instancedMeshKey(idx int, im engine.RenderInstancedMesh) string {
	return instancedMeshKeyForParams(idx, primitiveParamsForInstancedMesh(im))
}

func primitiveParamsForInstancedMesh(im engine.RenderInstancedMesh) primitiveParams {
	return primitiveParams{
		Kind:            im.Kind,
		Size:            im.Size,
		Width:           im.Width,
		Height:          im.Height,
		Depth:           im.Depth,
		Radius:          im.Radius,
		RadiusTop:       im.RadiusTop,
		RadiusBottom:    im.RadiusBottom,
		Tube:            im.Tube,
		Segments:        im.Segments,
		RadialSegments:  im.RadialSegments,
		TubularSegments: im.TubularSegments,
	}
}

// runExternalPasses records every registered ExternalComputePass whose phase
// matches, in registration order, onto enc. Each pass dispatches and may
// publish bus resources (instance/indirect buffers) into r.published for later
// passes to consume. WebGPU auto-synchronizes the compute writes against the
// render passes that follow within this encoder.
func (r *Renderer) runExternalPasses(enc gpu.CommandEncoder, phase compute.PassPhase) error {
	if len(r.externalPasses) == 0 {
		return nil
	}
	if r.published == nil {
		r.published = make(map[string]compute.GPUResource)
	}
	ctx := compute.PassContext{
		Device:  r.device,
		Encoder: enc,
		Publish: func(res compute.GPUResource) { r.published[res.Name] = res },
	}
	for _, p := range r.externalPasses {
		if p.Phase() != phase {
			continue
		}
		if err := p.Record(ctx); err != nil {
			return fmt.Errorf("bundle: external compute pass %q: %w", p.ID(), err)
		}
	}
	return nil
}

// recordCullPass uploads per-mesh instance data, resets indirect-draw args,
// and dispatches the culling compute shader for every InstancedMesh in the
// bundle. The compacted output + draw args land in GPU buffers that the
// main pass reads later via DrawIndirect.
// The instance upload is fingerprinted: a mesh whose transforms did not change
// since the last frame keeps the bytes already resident in its input buffer.
// Static geometry therefore costs one hash pass over the source transforms and
// no upload at all.
func (r *Renderer) recordCullPass(enc gpu.CommandEncoder, b engine.RenderBundle, frustum [6][4]float32, cascades cascadeData) error {
	if len(r.meshStates) == 0 {
		return nil
	}
	// Per-cascade shadow-caster frustum planes, taken from each cascade's light
	// view-projection. The cascade volume already extends toward the light, so
	// culling against its own planes keeps every caster that can reach it.
	var casterPlanes [cascadeCount][6][4]float32
	for cascade := 0; cascade < cascadeCount; cascade++ {
		casterPlanes[cascade] = extractFrustumPlanes(cascades.viewProjs[cascade])
	}
	// Upload instance transforms + reset draw args BEFORE beginning the
	// compute pass — writeBuffer operations within an open pass are not
	// allowed.
	drawable := 0
	for i := range r.meshStates {
		st := &r.meshStates[i]
		if !st.drawable {
			continue
		}
		drawable++
		cull := st.cull
		transforms := b.InstancedMeshes[i].Transforms
		hash := instanceTransformHash(transforms, st.instanceCount, st.pickBase)
		if !cull.haveUpload || cull.uploadedHash != hash || cull.uploadedCount != st.instanceCount {
			records := r.instanceScratch(st.instanceCount * instanceRecordStride)
			instanceRecordInto(records, transforms, st.instanceCount, st.pickBase)
			r.device.Queue().WriteBuffer(cull.inputBuf, 0, records)
			cull.uploadedHash = hash
			cull.uploadedCount = st.instanceCount
			cull.haveUpload = true
		}
		r.device.Queue().WriteBuffer(cull.drawArgsBuf, 0, r.drawArgsResetBytes(uint32(st.vertexCount)))
		// Pass the live instance count so the kernel bounds its threads on
		// min(instanceCount, arrayLength(&input)), matching the browser kernel.
		// Omitting it writes 0xffffffff, which makes min() select arrayLength
		// and lets the kernel compact zero-matrix records past the live count.
		r.device.Queue().WriteBuffer(cull.cullUniform, 0,
			r.cullUniformBytes(frustum, uint32(st.vertexCount), st.radius, uint32(st.instanceCount)))
		if !st.castShadow {
			// A mesh that stopped casting shadows keeps its caster buffers, but
			// nothing reads them. Skip the writes and the dispatch.
			continue
		}
		for cascade := 0; cascade < cascadeCount; cascade++ {
			caster := cull.casters[cascade]
			if caster == nil {
				continue
			}
			r.device.Queue().WriteBuffer(caster.drawArgsBuf, 0, r.drawArgsResetBytes(uint32(st.vertexCount)))
			r.device.Queue().WriteBuffer(caster.cullUniform, 0,
				r.cullUniformBytes(casterPlanes[cascade], uint32(st.vertexCount), st.radius, uint32(st.instanceCount)))
		}
	}
	if drawable == 0 {
		return nil
	}

	pass := enc.BeginComputePass()
	pass.SetPipeline(r.cullPipeline)
	for i := range r.meshStates {
		st := &r.meshStates[i]
		if !st.drawable {
			continue
		}
		// workgroup_size is 64 in the shader; dispatch (N+63)/64 groups.
		groups := (st.instanceCount + 63) / 64
		pass.SetBindGroup(0, st.cull.bindGroup)
		pass.DispatchWorkgroups(groups, 1, 1)
		if !st.castShadow {
			continue
		}
		for cascade := 0; cascade < cascadeCount; cascade++ {
			caster := st.cull.casters[cascade]
			if caster == nil {
				continue
			}
			pass.SetBindGroup(0, caster.bindGroup)
			pass.DispatchWorkgroups(groups, 1, 1)
		}
	}
	pass.End()
	return nil
}

// frameCastsShadow reports whether any draw would reach a shadow pass.
//
// It tests the same two conditions recordShadowPass tests, so it cannot report
// "no caster" for a frame that would draw one. It can report a caster for a frame
// whose object mesh cache turns out empty, which costs one pass and no
// correctness.
func (r *Renderer) frameCastsShadow(b engine.RenderBundle) bool {
	for i := range r.meshStates {
		if st := &r.meshStates[i]; st.drawable && st.castShadow {
			return true
		}
	}
	for _, object := range b.Objects {
		if object.CastShadow && nativeObjectDrawable(b, object) {
			return true
		}
	}
	return false
}

// recordShadowPass renders cascade-specific depth-only draws into the
// cascade's layer of the shadow texture array. Called once per cascade index.
//
// hasCasters comes from frameCastsShadow. When it is false the pass has nothing
// to draw and only clears, so the whole pass is skipped once the cascades are
// already clear. That clear is not free: the cascades are 2048 squares, and a
// profile of an empty sixteen-pixel frame put most of its samples in it.
//
// The state matters. A frame that skips the pass leaves whatever the previous
// frame drew, so a caster that goes away would keep shadowing until something
// else cleared the map. shadowCascadesClear records that the last recorded pass
// drew nothing, which is the only condition under which the stale contents are
// the same as a fresh clear.
func (r *Renderer) recordShadowPass(enc gpu.CommandEncoder, b engine.RenderBundle, cascade int, hasCasters bool) {
	if !hasCasters && r.shadowCascadesClear {
		return
	}
	r.shadowDepthAttachment[cascade] = gpu.RenderPassDepthStencilAttachment{
		View:            r.shadowLayerViews[cascade],
		DepthLoadOp:     gpu.LoadOpClear,
		DepthStoreOp:    gpu.StoreOpStore,
		DepthClearValue: 1.0,
	}
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		DepthStencilAttachment: &r.shadowDepthAttachment[cascade],
		Label:                  "bundle.shadow.cascade",
	})
	if len(r.meshStates) > 0 {
		pass.SetPipeline(r.shadowPipeline)
		pass.SetBindGroup(0, r.shadowBindGrps[cascade])
		for i := range r.meshStates {
			st := &r.meshStates[i]
			if !st.drawable || !st.castShadow {
				continue
			}
			if caster := st.cull.casters[cascade]; caster != nil {
				// Per-cascade caster cull: draw only the instances whose
				// bounding spheres reach this cascade's light volume.
				pass.SetVertexBuffer(0, st.prim.positions)
				pass.SetVertexBuffer(1, caster.outputBuf)
				pass.DrawIndirect(caster.drawArgsBuf, 0)
				continue
			}
			pass.SetVertexBuffer(0, st.prim.positions)
			pass.SetVertexBuffer(1, st.cull.inputBuf)
			pass.Draw(st.vertexCount, st.instanceCount, 0, 0)
		}
	}
	if len(b.Objects) > 0 {
		pass.SetPipeline(r.shadowPipeline)
		pass.SetBindGroup(0, r.shadowBindGrps[cascade])
		for i, object := range b.Objects {
			if !object.CastShadow || !nativeObjectDrawable(b, object) {
				continue
			}
			res := r.objectMeshCache[objectMeshKey(i, object)]
			if res == nil || res.vertexCount == 0 || res.instance == nil {
				continue
			}
			pass.SetVertexBuffer(0, res.positions)
			pass.SetVertexBuffer(1, res.instance)
			pass.Draw(res.vertexCount, 1, 0, 0)
		}
	}
	pass.End()
}

// ensureDepth allocates or resizes the main-pass depth texture to match the
// surface dimensions.
func (r *Renderer) ensureDepth(width, height int) (gpu.TextureView, error) {
	if r.depthTex != nil && r.depthWidth == width && r.depthHeight == height {
		return r.depthView, nil
	}
	if r.depthTex != nil {
		r.depthTex.Destroy()
		r.depthTex = nil
	}
	tex, err := r.device.CreateTexture(gpu.TextureDesc{
		Width:  width,
		Height: height,
		Format: r.depthFormat,
		Usage:  gpu.TextureUsageRenderAttachment | gpu.TextureUsageTextureBinding,
		Label:  "bundle.depth",
	})
	if err != nil {
		return nil, fmt.Errorf("bundle: create depth texture: %w", err)
	}
	r.depthTex = tex
	r.depthView = tex.CreateView()
	r.depthWidth = width
	r.depthHeight = height
	return r.depthView, nil
}

// ensurePrimitiveForMesh uploads the geometry for an instanced mesh on first
// request. The cache key includes primitive parameters, so two torus batches
// with different segment counts do not accidentally share buffers.
func (r *Renderer) ensurePrimitiveForMesh(im engine.RenderInstancedMesh) (*primitiveResources, error) {
	return r.ensurePrimitive(primitiveParamsForInstancedMesh(im))
}

func (r *Renderer) ensurePrimitive(params primitiveParams) (*primitiveResources, error) {
	key := primitiveCacheKey(params)
	if key == "" {
		return nil, nil
	}
	if res, ok := r.primitiveCache[key]; ok {
		return res, nil
	}
	geo := primitiveForParams(params)
	if geo == nil {
		return nil, nil
	}
	posBuf, err := r.uploadVertexBuffer(geo.positions, "bundle.primitive.positions:"+key)
	if err != nil {
		return nil, err
	}
	colBuf, err := r.uploadVertexBuffer(geo.colors, "bundle.primitive.colors:"+key)
	if err != nil {
		posBuf.Destroy()
		return nil, err
	}
	nrmBuf, err := r.uploadVertexBuffer(geo.normals, "bundle.primitive.normals:"+key)
	if err != nil {
		posBuf.Destroy()
		colBuf.Destroy()
		return nil, err
	}
	uvBuf, err := r.uploadVertexBuffer(geo.uvs, "bundle.primitive.uvs:"+key)
	if err != nil {
		posBuf.Destroy()
		colBuf.Destroy()
		nrmBuf.Destroy()
		return nil, err
	}
	res := &primitiveResources{
		positions:   posBuf,
		colors:      colBuf,
		normals:     nrmBuf,
		uvs:         uvBuf,
		vertexCount: geo.vertexCount,
	}
	r.primitiveCache[key] = res
	return res, nil
}

func (r *Renderer) uploadVertexBuffer(data []float32, label string) (gpu.Buffer, error) {
	bytes := float32sToBytes(data)
	buf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  len(bytes),
		Usage: gpu.BufferUsageVertex | gpu.BufferUsageCopyDst,
		Label: label,
	})
	if err != nil {
		return nil, fmt.Errorf("bundle: create %s: %w", label, err)
	}
	r.device.Queue().WriteBuffer(buf, 0, bytes)
	return buf, nil
}

func (r *Renderer) uploadVertexBytes(data []byte, label string) (gpu.Buffer, error) {
	buf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  len(data),
		Usage: gpu.BufferUsageVertex | gpu.BufferUsageCopyDst,
		Label: label,
	})
	if err != nil {
		return nil, fmt.Errorf("bundle: create %s: %w", label, err)
	}
	r.device.Queue().WriteBuffer(buf, 0, data)
	return buf, nil
}

func isSkinnedMesh(im engine.RenderInstancedMesh) bool {
	return im.SkinID != "" || len(im.JointIndices) > 0 || len(im.Weights) > 0 || len(im.BindPose) > 0
}

func (r *Renderer) ensureSkinBuffers(key string, vertexCount int, im engine.RenderInstancedMesh) (*skinResources, error) {
	if res, ok := r.skinCache[key]; ok {
		return res, nil
	}
	joints, err := r.uploadVertexBytes(skinJointBytes(im.JointIndices, vertexCount), "bundle.skin.joints:"+key)
	if err != nil {
		return nil, err
	}
	weights, err := r.uploadVertexBytes(skinWeightBytes(im.Weights, vertexCount), "bundle.skin.weights:"+key)
	if err != nil {
		joints.Destroy()
		return nil, err
	}
	bindPose, err := r.uploadVertexBytes(skinBindPoseBytes(im.BindPose, vertexCount), "bundle.skin.bindpose:"+key)
	if err != nil {
		joints.Destroy()
		weights.Destroy()
		return nil, err
	}
	res := &skinResources{joints: joints, weights: weights, bindPose: bindPose}
	r.skinCache[key] = res
	return res, nil
}

func skinJointBytes(src []uint32, vertexCount int) []byte {
	out := make([]byte, vertexCount*16)
	needed := min(len(src), vertexCount*4)
	for i := 0; i < needed; i++ {
		putUint32LE(out[i*4:i*4+4], src[i])
	}
	return out
}

func skinWeightBytes(src []float64, vertexCount int) []byte {
	values := make([]float32, vertexCount*4)
	if len(src) == 0 {
		for i := 0; i < vertexCount; i++ {
			values[i*4] = 1
		}
	} else {
		needed := min(len(src), vertexCount*4)
		for i := 0; i < needed; i++ {
			values[i] = float32(src[i])
		}
	}
	return float32sToBytes(values)
}

func skinBindPoseBytes(src []float64, vertexCount int) []byte {
	if len(src) > 0 {
		values := make([]float32, vertexCount*16)
		needed := min(len(src), vertexCount*16)
		for i := 0; i < needed; i++ {
			values[i] = float32(src[i])
		}
		return float32sToBytes(values)
	}
	values := make([]float32, vertexCount*16)
	for i := 0; i < vertexCount; i++ {
		base := i * 16
		values[base+0] = 1
		values[base+5] = 1
		values[base+10] = 1
		values[base+15] = 1
	}
	return float32sToBytes(values)
}

func (r *Renderer) bonePaletteForMesh(im engine.RenderInstancedMesh) *BonePalette {
	if im.SkinID != "" {
		if palette := r.bonePalettes[im.SkinID]; palette != nil {
			return palette
		}
	}
	return r.defaultBonePalette
}

func (r *Renderer) ensurePassBuffers(pb engine.RenderPassBundle) (*passResources, error) {
	cacheKey := pb.CacheKey
	if cacheKey != "" {
		if cached, ok := r.passCache[cacheKey]; ok {
			return cached, nil
		}
	}
	posBytes := float64sToFloat32Bytes(pb.Positions)
	if len(posBytes) == 0 {
		return nil, nil
	}
	vertexCount := pb.VertexCount
	if vertexCount == 0 {
		vertexCount = len(pb.Positions) / 3
	}

	posBuf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  len(posBytes),
		Usage: gpu.BufferUsageVertex | gpu.BufferUsageCopyDst,
		Label: "bundle.pass.positions:" + cacheKey,
	})
	if err != nil {
		return nil, fmt.Errorf("bundle: create position buffer: %w", err)
	}
	r.device.Queue().WriteBuffer(posBuf, 0, posBytes)

	colBytes := float64sToFloat32Bytes(pb.Colors)
	if len(colBytes) == 0 {
		colBytes = whiteColorsFor(vertexCount)
	}
	colBuf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  len(colBytes),
		Usage: gpu.BufferUsageVertex | gpu.BufferUsageCopyDst,
		Label: "bundle.pass.colors:" + cacheKey,
	})
	if err != nil {
		posBuf.Destroy()
		return nil, fmt.Errorf("bundle: create color buffer: %w", err)
	}
	r.device.Queue().WriteBuffer(colBuf, 0, colBytes)

	res := &passResources{
		positions:   posBuf,
		colors:      colBuf,
		vertexCount: vertexCount,
	}
	if cacheKey != "" {
		r.passCache[cacheKey] = res
	}
	return res, nil
}

// buildUniformBuffers allocates the scene uniform buffer and one shadow
// uniform buffer per cascade.
func (r *Renderer) buildUniformBuffers() error {
	scene, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  sceneUniformSize,
		Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
		Label: "bundle.scene.uniforms",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildUniformBuffers (scene): %w", err)
	}
	r.sceneUniformBuf = scene
	// The lit bind groups are built once, at startup, and they need a light
	// buffer to point at from the first frame.
	if err := r.ensureLightStorage(0); err != nil {
		return fmt.Errorf("bundle.buildUniformBuffers (lights): %w", err)
	}
	for i := 0; i < cascadeCount; i++ {
		buf, err := r.device.CreateBuffer(gpu.BufferDesc{
			Size:  64,
			Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
			Label: fmt.Sprintf("bundle.shadow.uniforms.cascade%d", i),
		})
		if err != nil {
			return fmt.Errorf("bundle.buildUniformBuffers (shadow %d): %w", i, err)
		}
		r.shadowUniformBufs[i] = buf
	}
	return nil
}

// buildShadowResources creates the cascaded shadow map (a depth texture
// array), per-cascade layer views, and the comparison sampler used by the
// lit pass to sample it.
func (r *Renderer) buildShadowResources() error {
	tex, err := r.device.CreateTexture(gpu.TextureDesc{
		Width:              shadowMapSize,
		Height:             shadowMapSize,
		DepthOrArrayLayers: cascadeCount,
		Format:             gpu.FormatDepth32Float,
		Usage:              gpu.TextureUsageRenderAttachment | gpu.TextureUsageTextureBinding,
		Label:              "bundle.shadow.cascades",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildShadowResources (texture): %w", err)
	}
	samp, err := r.device.CreateSampler(gpu.SamplerDesc{
		MagFilter:    gpu.FilterLinear,
		MinFilter:    gpu.FilterLinear,
		MipmapFilter: gpu.FilterNearest,
		AddressU:     gpu.AddressClampToEdge,
		AddressV:     gpu.AddressClampToEdge,
		AddressW:     gpu.AddressClampToEdge,
		Compare:      gpu.CompareLessEqual,
		Label:        "bundle.shadow.sampler",
	})
	if err != nil {
		tex.Destroy()
		return fmt.Errorf("bundle.buildShadowResources (sampler): %w", err)
	}
	r.shadowTex = tex
	r.shadowArrayView = tex.CreateView()
	for i := 0; i < cascadeCount; i++ {
		r.shadowLayerViews[i] = tex.CreateLayerView(i)
	}
	r.shadowSampler = samp
	return nil
}

func (r *Renderer) buildUnlitPipeline() error {
	shader, err := r.device.CreateShaderModule(gpu.ShaderDesc{
		SourceWGSL: unlitWGSL,
		Label:      "bundle.unlit",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildUnlitPipeline: %w", err)
	}
	pipeline, err := r.device.CreateRenderPipeline(gpu.RenderPipelineDesc{
		Vertex: gpu.VertexStageDesc{
			Module:     shader,
			EntryPoint: "vs_main",
			Buffers: []gpu.VertexBufferLayout{
				{ArrayStride: 12, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 0, Offset: 0, Format: gpu.VertexFormatFloat32x3},
				}},
				{ArrayStride: 12, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 1, Offset: 0, Format: gpu.VertexFormatFloat32x3},
				}},
			},
		},
		Fragment: gpu.FragmentStageDesc{
			Module:     shader,
			EntryPoint: "fs_main",
			Targets: []gpu.ColorTargetState{
				{Format: r.hdrFormat, WriteMask: gpu.ColorWriteAll},
				{Format: gpu.FormatR32Uint, WriteMask: gpu.ColorWriteAll},
			},
		},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			CullMode:  gpu.CullBack,
			FrontFace: gpu.FrontFaceCCW,
		},
		DepthStencil: &gpu.DepthStencilState{
			Format:            r.depthFormat,
			DepthWriteEnabled: true,
			DepthCompare:      gpu.CompareLess,
		},
		AutoLayout: true,
		Label:      "bundle.unlit",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildUnlitPipeline: %w", err)
	}
	r.unlitPipeline = pipeline
	r.unlitBGLayout = pipeline.GetBindGroupLayout(0)
	return nil
}

// buildMaterialSampler creates the shared linear-filtering sampler used for
// baseColor texture reads on the material bind group.
func (r *Renderer) buildMaterialSampler() error {
	s, err := r.device.CreateSampler(gpu.SamplerDesc{
		MagFilter:    gpu.FilterLinear,
		MinFilter:    gpu.FilterLinear,
		MipmapFilter: gpu.FilterLinear,
		AddressU:     gpu.AddressRepeat,
		AddressV:     gpu.AddressRepeat,
		AddressW:     gpu.AddressRepeat,
		Label:        "bundle.material.sampler",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildMaterialSampler: %w", err)
	}
	r.materialSampler = s
	return nil
}

// buildDefaultTangentBuffer creates the shared [1,0,0,1] tangent record every
// lit and skinned-lit draw binds at the new tangent vertex-attribute slot
// until a mesh carries authored tangents. See defaultTangentBuf.
func (r *Renderer) buildDefaultTangentBuffer() error {
	buf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  16,
		Usage: gpu.BufferUsageVertex | gpu.BufferUsageCopyDst,
		Label: "bundle.tangent.default",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildDefaultTangentBuffer: %w", err)
	}
	r.device.Queue().WriteBuffer(buf, 0, float32sToBytes([]float32{1, 0, 0, 1}))
	r.defaultTangentBuf = buf
	return nil
}

func alphaBlendState() *gpu.BlendState {
	return &gpu.BlendState{
		Color: gpu.BlendComponent{SrcFactor: gpu.BlendSrcAlpha, DstFactor: gpu.BlendOneMinusSrcAlpha, Operation: gpu.BlendOpAdd},
		Alpha: gpu.BlendComponent{SrcFactor: gpu.BlendOne, DstFactor: gpu.BlendOneMinusSrcAlpha, Operation: gpu.BlendOpAdd},
	}
}

// buildLitPipeline is the directional-lit + shadowed pipeline used for
// RenderInstancedMesh entries. 5 vertex buffers: positions, colors, normals,
// uvs, per-instance mat4 (as 4 vec4 attributes).
func (r *Renderer) buildLitPipeline() error {
	shader, err := r.device.CreateShaderModule(gpu.ShaderDesc{
		SourceWGSL: litWGSL,
		Label:      "bundle.lit",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildLitPipeline: %w", err)
	}
	pipeline, err := r.device.CreateRenderPipeline(gpu.RenderPipelineDesc{
		Vertex: gpu.VertexStageDesc{
			Module:     shader,
			EntryPoint: "vs_main",
			Buffers: []gpu.VertexBufferLayout{
				{ArrayStride: 12, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 0, Offset: 0, Format: gpu.VertexFormatFloat32x3},
				}},
				{ArrayStride: 12, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 1, Offset: 0, Format: gpu.VertexFormatFloat32x3},
				}},
				{ArrayStride: 12, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 2, Offset: 0, Format: gpu.VertexFormatFloat32x3},
				}},
				{ArrayStride: 8, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 3, Offset: 0, Format: gpu.VertexFormatFloat32x2},
				}},
				{ArrayStride: instanceRecordStride, StepMode: gpu.StepInstance, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 4, Offset: 0, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 5, Offset: 16, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 6, Offset: 32, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 7, Offset: 48, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 8, Offset: 64, Format: gpu.VertexFormatUint32x4},
				}},
				// Tangent, slot 5, location 9. ArrayStride 0 makes every vertex of
				// every draw read the same defaultTangentBuf record, so no lobe
				// consumes it, no golden frame moves, and no per-mesh tangent
				// buffer needs to exist yet. See Scene3D parity cluster C PR2.
				{ArrayStride: 0, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 9, Offset: 0, Format: gpu.VertexFormatFloat32x4},
				}},
			},
		},
		Fragment: gpu.FragmentStageDesc{
			Module:     shader,
			EntryPoint: "fs_main",
			Targets: []gpu.ColorTargetState{
				{Format: r.hdrFormat, Blend: alphaBlendState(), WriteMask: gpu.ColorWriteAll},
				{Format: gpu.FormatR32Uint, WriteMask: gpu.ColorWriteAll},
			},
		},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			CullMode:  gpu.CullBack,
			FrontFace: gpu.FrontFaceCCW,
		},
		DepthStencil: &gpu.DepthStencilState{
			Format:            r.depthFormat,
			DepthWriteEnabled: true,
			DepthCompare:      gpu.CompareLess,
		},
		AutoLayout: true,
		Label:      "bundle.lit",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildLitPipeline: %w", err)
	}
	r.litPipeline = pipeline
	r.litBGLayout = pipeline.GetBindGroupLayout(0)
	r.litMaterialLayout = pipeline.GetBindGroupLayout(1)
	return nil
}

// buildSkinnedLitPipeline is the skeletal-animation variant of the lit
// pipeline. It keeps group 0/1 compatible with the rigid lit path and adds
// group 2 for the bone palette plus three vertex streams: joints, weights,
// and per-vertex bind-pose transforms.
func (r *Renderer) buildSkinnedLitPipeline() error {
	shader, err := r.device.CreateShaderModule(gpu.ShaderDesc{
		SourceWGSL: skinnedLitWGSL(),
		Label:      "bundle.lit.skinned",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildSkinnedLitPipeline: %w", err)
	}
	pipeline, err := r.device.CreateRenderPipeline(gpu.RenderPipelineDesc{
		Vertex: gpu.VertexStageDesc{
			Module:     shader,
			EntryPoint: "vs_main",
			Buffers: []gpu.VertexBufferLayout{
				{ArrayStride: 12, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 0, Offset: 0, Format: gpu.VertexFormatFloat32x3},
				}},
				{ArrayStride: 12, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 1, Offset: 0, Format: gpu.VertexFormatFloat32x3},
				}},
				{ArrayStride: 12, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 2, Offset: 0, Format: gpu.VertexFormatFloat32x3},
				}},
				{ArrayStride: 8, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 3, Offset: 0, Format: gpu.VertexFormatFloat32x2},
				}},
				{ArrayStride: instanceRecordStride, StepMode: gpu.StepInstance, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 4, Offset: 0, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 5, Offset: 16, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 6, Offset: 32, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 7, Offset: 48, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 8, Offset: 64, Format: gpu.VertexFormatUint32x4},
				}},
				{ArrayStride: 16, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 9, Offset: 0, Format: gpu.VertexFormatUint32x4},
				}},
				{ArrayStride: 16, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 10, Offset: 0, Format: gpu.VertexFormatFloat32x4},
				}},
				{ArrayStride: 64, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 11, Offset: 0, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 12, Offset: 16, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 13, Offset: 32, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 14, Offset: 48, Format: gpu.VertexFormatFloat32x4},
				}},
				// Tangent, slot 8, location 15 (locations 9-14 are the skinning
				// attributes above). Same ArrayStride-0 default-record trick as the
				// rigid pipeline. See Scene3D parity cluster C PR2.
				{ArrayStride: 0, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 15, Offset: 0, Format: gpu.VertexFormatFloat32x4},
				}},
			},
		},
		Fragment: gpu.FragmentStageDesc{
			Module:     shader,
			EntryPoint: "fs_main",
			Targets: []gpu.ColorTargetState{
				{Format: r.hdrFormat, Blend: alphaBlendState(), WriteMask: gpu.ColorWriteAll},
				{Format: gpu.FormatR32Uint, WriteMask: gpu.ColorWriteAll},
			},
		},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			CullMode:  gpu.CullBack,
			FrontFace: gpu.FrontFaceCCW,
		},
		DepthStencil: &gpu.DepthStencilState{
			Format:            r.depthFormat,
			DepthWriteEnabled: true,
			DepthCompare:      gpu.CompareLess,
		},
		AutoLayout: true,
		Label:      "bundle.lit.skinned",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildSkinnedLitPipeline: %w", err)
	}
	r.skinnedLitPipeline = pipeline
	r.skinnedLitBGLayout = pipeline.GetBindGroupLayout(0)
	r.skinnedLitMaterialLayout = pipeline.GetBindGroupLayout(1)
	r.skinnedPaletteLayout = pipeline.GetBindGroupLayout(2)
	return nil
}

// buildShadowPipeline is the depth-only pipeline used during the shadow pass.
// Positions + per-instance mat4. No color, no normal, no fragment output.
func (r *Renderer) buildShadowPipeline() error {
	shader, err := r.device.CreateShaderModule(gpu.ShaderDesc{
		SourceWGSL: shadowWGSL,
		Label:      "bundle.shadow",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildShadowPipeline: %w", err)
	}
	pipeline, err := r.device.CreateRenderPipeline(gpu.RenderPipelineDesc{
		Vertex: gpu.VertexStageDesc{
			Module:     shader,
			EntryPoint: "vs_main",
			Buffers: []gpu.VertexBufferLayout{
				{ArrayStride: 12, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 0, Offset: 0, Format: gpu.VertexFormatFloat32x3},
				}},
				{ArrayStride: instanceRecordStride, StepMode: gpu.StepInstance, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 1, Offset: 0, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 2, Offset: 16, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 3, Offset: 32, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 4, Offset: 48, Format: gpu.VertexFormatFloat32x4},
				}},
			},
		},
		// No fragment stage — depth-only.
		Fragment: gpu.FragmentStageDesc{},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			CullMode:  gpu.CullBack,
			FrontFace: gpu.FrontFaceCCW,
		},
		DepthStencil: &gpu.DepthStencilState{
			Format:            gpu.FormatDepth32Float,
			DepthWriteEnabled: true,
			DepthCompare:      gpu.CompareLess,
		},
		AutoLayout: true,
		Label:      "bundle.shadow",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildShadowPipeline: %w", err)
	}
	r.shadowPipeline = pipeline
	r.shadowBGLayout = pipeline.GetBindGroupLayout(0)
	return nil
}

// buildBindGroups builds the per-pipeline bind groups. The lit bind group
// holds scene uniforms, shadow resources, and the environment cubemap.
func (r *Renderer) buildBindGroups() error {
	unlit, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout:  r.unlitBGLayout,
		Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: r.sceneUniformBuf, Size: sceneUniformSize}},
		Label:   "bundle.unlit.bindgroup",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildBindGroups (unlit): %w", err)
	}
	for _, mode := range []string{surfacePassOpaque, surfacePassAlpha, surfacePassAdditive} {
		bg, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
			Layout:  r.surfaceBGLayouts[mode],
			Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: r.sceneUniformBuf, Size: sceneUniformSize}},
			Label:   "bundle.surface.bindgroup." + mode,
		})
		if err != nil {
			return fmt.Errorf("bundle.buildBindGroups (surface %s): %w", mode, err)
		}
		r.surfaceBindGrps[mode] = bg
	}
	worldLine, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout:  r.worldLineBGLayout,
		Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: r.sceneUniformBuf, Size: sceneUniformSize}},
		Label:   "bundle.worldLine.bindgroup",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildBindGroups (worldLine): %w", err)
	}
	envTex, err := r.ensureFallbackCubeTexture()
	if err != nil {
		return fmt.Errorf("bundle.buildBindGroups (environment): %w", err)
	}
	lit, err := r.createLitSceneBindGroup(r.litBGLayout, envTex, "bundle.lit.bindgroup")
	if err != nil {
		return fmt.Errorf("bundle.buildBindGroups (lit): %w", err)
	}
	skinnedLit, err := r.createLitSceneBindGroup(r.skinnedLitBGLayout, envTex, "bundle.lit.skinned.bindgroup")
	if err != nil {
		return fmt.Errorf("bundle.buildBindGroups (skinned lit): %w", err)
	}
	for i := 0; i < cascadeCount; i++ {
		bg, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
			Layout: r.shadowBGLayout,
			Entries: []gpu.BindGroupEntry{
				{Binding: 0, Buffer: r.shadowUniformBufs[i], Size: 64},
			},
			Label: fmt.Sprintf("bundle.shadow.bindgroup.cascade%d", i),
		})
		if err != nil {
			return fmt.Errorf("bundle.buildBindGroups (shadow %d): %w", i, err)
		}
		r.shadowBindGrps[i] = bg
	}
	r.unlitBindGrp = unlit
	r.worldLineBindGrp = worldLine
	r.litBindGrp = lit
	r.skinnedLitBindGrp = skinnedLit
	r.envBindGroupKey = fallbackEnvironmentKey
	return nil
}

func (r *Renderer) createLitSceneBindGroup(layout gpu.BindGroupLayout, envTex *textureResources, label string) (gpu.BindGroup, error) {
	return r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: r.sceneUniformBuf, Size: sceneUniformSize},
			{Binding: 1, TextureView: r.shadowArrayView},
			{Binding: 2, Sampler: r.shadowSampler},
			{Binding: 3, TextureView: envTex.view},
			{Binding: 4, Sampler: r.materialSampler},
			{Binding: 5, Buffer: r.lightStorageBuf, Size: r.lightStorageCap * lightRecordSize},
		},
		Label: label,
	})
}

// sceneUniformSize is the layout size of the Scene struct in WGSL. 4 mat4
// (viewProj + 3 cascade lightViewProjs) = 256, plus 9 vec4 = 144 -> 400 bytes.
//
// lightParams was appended at 384. Every earlier offset stayed, so the CPU
// decode in render/gpu/headless/device.go activeLighting needs one added read
// and no edit to an existing one.
const sceneUniformSize = 400

type sceneUniformBlock struct {
	viewProj       mat4
	lightViewProjs [cascadeCount]mat4
	cameraPos      [4]float32
	lightDir       [4]float32
	lightColor     [4]float32
	ambientColor   [4]float32
	skyColor       [4]float32
	groundColor    [4]float32
	// cascadeSplits.xyz are the view-space far distances for cascades 0/1/2.
	// Cascade i covers the frustum slice [split_{i-1}, split_i] (split_{-1} =
	// camera near). Cascade 2 extends to the camera far plane regardless.
	cascadeSplits [4]float32
	// envParams.x = cubemap intensity, y = Y rotation radians, z = has env.
	envParams [4]float32
	// lightParams.x is how many records the light storage buffer holds this
	// frame. lightParams.y is the index of the light the cascaded shadow map is
	// fitted to, or -1 when no light reads it.
	lightParams [4]float32
}

// sceneUniformBytes packs the scene uniform block into a Renderer-owned buffer
// and returns it. The writes go straight into the buffer, so packing costs no
// allocation at all. The previous version allocated the output plus eleven
// throwaway float32 conversion slices on every frame.
func (r *Renderer) sceneUniformBytes(s sceneUniformBlock) []byte {
	out := r.sceneUniformScratch[:]
	putFloat32s(out[0:64], s.viewProj[:])
	for i := 0; i < cascadeCount; i++ {
		putFloat32s(out[64+i*64:64+(i+1)*64], s.lightViewProjs[i][:])
	}
	base := 64 + cascadeCount*64
	putFloat32s(out[base+0:base+16], s.cameraPos[:])
	putFloat32s(out[base+16:base+32], s.lightDir[:])
	putFloat32s(out[base+32:base+48], s.lightColor[:])
	putFloat32s(out[base+48:base+64], s.ambientColor[:])
	putFloat32s(out[base+64:base+80], s.skyColor[:])
	putFloat32s(out[base+80:base+96], s.groundColor[:])
	putFloat32s(out[base+96:base+112], s.cascadeSplits[:])
	putFloat32s(out[base+112:base+128], s.envParams[:])
	putFloat32s(out[base+128:base+144], s.lightParams[:])
	return out
}

// primaryDirectionalIndex returns the index of the light the cascaded shadow map
// is fitted to, or -1 when no authored light qualifies.
//
// The rule: the first light of kind "directional" wins, and a direction of
// exactly zero disqualifies it, because a zero vector names no direction. A
// later directional light never replaces a disqualified earlier one.
//
// resolveDirectionalLight and resolveSceneLights both call this, so the light
// that steers the cascade fit is always the same light that reads the shadow
// map in litWGSL. Two separate rules would drift and put the shadow on a light
// the cascades were never fitted to.
func primaryDirectionalIndex(lights []engine.RenderLight) int {
	for i, l := range lights {
		if l.Kind != "directional" {
			continue
		}
		if l.DirectionX == 0 && l.DirectionY == 0 && l.DirectionZ == 0 {
			return -1
		}
		return i
	}
	return -1
}

// resolveDirectionalLight picks a primary directional light from the bundle's
// Lights + Environment. If none exist it falls back to a tasteful default —
// unlit demos should still render usefully.
func resolveDirectionalLight(b engine.RenderBundle) (dir [3]float32, color [4]float32, ambient [4]float32) {
	dir = [3]float32{-0.4, -1.0, -0.3}
	color = [4]float32{1, 0.96, 0.9, 1.0} // w = intensity
	ambient = [4]float32{0.35, 0.38, 0.45, 0.35}

	if idx := primaryDirectionalIndex(b.Lights); idx >= 0 {
		l := b.Lights[idx]
		dx, dy, dz := float32(l.DirectionX), float32(l.DirectionY), float32(l.DirectionZ)
		length := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
		if length > 0 {
			dir = [3]float32{dx / length, dy / length, dz / length}
		}
		lc := parseCSSColor(l.Color, [3]float32{1, 1, 1})
		intensity := float32(l.Intensity)
		if intensity == 0 {
			intensity = 1.0
		}
		color = [4]float32{lc[0], lc[1], lc[2], intensity}
	}

	env := b.Environment
	if env.AmbientColor != "" || env.AmbientIntensity != 0 {
		ac := parseCSSColor(env.AmbientColor, [3]float32{0.5, 0.5, 0.5})
		intensity := float32(env.AmbientIntensity)
		if intensity == 0 {
			intensity = 0.3
		}
		ambient = [4]float32{ac[0], ac[1], ac[2], intensity}
	}
	return dir, color, ambient
}

// resolveHemisphereAmbient pulls sky + ground colors from the bundle's
// Environment for the hemisphere-ambient IBL approximation. When unset,
// defaults to a soft overcast (warm sky, cool ground) tuned to read well
// with primitive geometry.
func resolveHemisphereAmbient(b engine.RenderBundle) (sky [4]float32, ground [4]float32) {
	env := b.Environment
	skyRGB := parseCSSColor(env.SkyColor, [3]float32{0.80, 0.88, 1.00})
	groundRGB := parseCSSColor(env.GroundColor, [3]float32{0.28, 0.24, 0.22})
	skyI := float32(env.SkyIntensity)
	if skyI == 0 {
		skyI = 1.0
	}
	groundI := float32(env.GroundIntensity)
	if groundI == 0 {
		groundI = 1.0
	}
	return [4]float32{skyRGB[0] * skyI, skyRGB[1] * skyI, skyRGB[2] * skyI, 1},
		[4]float32{groundRGB[0] * groundI, groundRGB[1] * groundI, groundRGB[2] * groundI, 1}
}

// The scene light array.
//
// litWGSL reads a storage array of Light records. The five vec4 below carry the
// same fields, in the same order, as the first five vec4 of the browser Light
// struct in client/js/bootstrap-src/16a-scene-webgpu.js. The browser record
// holds two more vec4 for the edge vectors of a rect-area light;
// engine.RenderLight has no width and no height, so those cannot exist here.
const (
	// lightFloats is the float count of one packed light: five vec4.
	lightFloats = 20
	// lightRecordSize is the byte size of one packed light.
	lightRecordSize = lightFloats * 4
	// lightCapacityMin and lightCapacityMax bound the storage buffer. They
	// match SCENE_WEBGPU_LIGHT_CAPACITY_MIN and _MAX in the browser renderer,
	// so neither backend accepts a scene the other rejects.
	lightCapacityMin = 8
	lightCapacityMax = 256
)

// Light kind codes. Codes 0 to 5 are the numbers both browser renderers write,
// so a scene reads the same kind on every backend.
const (
	lightKindAmbient     = 0
	lightKindDirectional = 1
	lightKindPoint       = 2
	lightKindSpot        = 3
	lightKindHemisphere  = 4
	lightKindRectArea    = 5
)

// packedLight is one scene light in the layout litWGSL reads.
type packedLight [lightFloats]float32

// lightKindCode maps an engine.RenderLight kind to its shader kind code.
//
// A light probe maps to ambient, not to point. A probe carries no position, so
// a point code would invent a distance falloff the author never asked for. Both
// browser renderers make the same choice. Neither backend reads
// LightProbe.Coefficients; see the light-probe-sh cell in
// scene/capability/capability.go.
//
// An unknown kind maps to point, which is what both browser renderers do.
func lightKindCode(kind string) float32 {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "ambient":
		return lightKindAmbient
	case "light-probe":
		return lightKindAmbient
	case "directional":
		return lightKindDirectional
	case "point":
		return lightKindPoint
	case "spot":
		return lightKindSpot
	case "hemisphere":
		return lightKindHemisphere
	case "rect-area":
		return lightKindRectArea
	default:
		return lightKindPoint
	}
}

// resolveSceneLights packs every authored light into out and returns the packed
// slice together with the index of the light that reads the shadow map.
//
// Defaults follow sceneWebGPUPackLights in the browser renderer field for
// field, because engine.RenderBundle marshals with omitempty: a zero on this
// side is an absent field on that side, and an absent field takes the browser
// default. Intensity 0 becomes 1, decay 0 becomes 2, shadow bias 0 becomes
// 0.005, and a direction of exactly zero becomes straight down.
//
// A bundle with no lights at all takes one synthetic key light, which is the
// same fallback resolveDirectionalLight applies. An unlit demo still renders.
// A bundle that authors any light takes only the lights it authored, which is
// what both browser renderers do.
//
// out is reused across frames. Pass the previous slice to allocate nothing.
func resolveSceneLights(b engine.RenderBundle, out []packedLight) ([]packedLight, int) {
	out = out[:0]
	for _, l := range b.Lights {
		var rec packedLight
		// position.xyz + kind code
		rec[0] = float32(l.X)
		rec[1] = float32(l.Y)
		rec[2] = float32(l.Z)
		rec[3] = lightKindCode(l.Kind)

		// direction.xyz + intensity
		dx, dy, dz := float32(l.DirectionX), float32(l.DirectionY), float32(l.DirectionZ)
		if dx == 0 && dy == 0 && dz == 0 {
			dy = -1
		}
		intensity := float32(l.Intensity)
		if intensity == 0 {
			intensity = 1
		}
		rec[4], rec[5], rec[6], rec[7] = dx, dy, dz, intensity

		// color.rgb + range
		lc := parseCSSColor(l.Color, [3]float32{1, 1, 1})
		rec[8], rec[9], rec[10] = lc[0], lc[1], lc[2]
		rec[11] = float32(l.Range)

		// params: decay, shadow bias, cast shadow, cone angle
		decay := float32(l.Decay)
		if decay == 0 {
			decay = 2
		}
		bias := float32(l.ShadowBias)
		if bias == 0 {
			bias = 0.005
		}
		castShadow := float32(0)
		if l.CastShadow {
			castShadow = 1
		}
		rec[12], rec[13], rec[14], rec[15] = decay, bias, castShadow, float32(l.Angle)

		// groundPenumbra: hemisphere ground colour + spot penumbra
		gc := parseCSSColor(l.GroundColor, [3]float32{0, 0, 0})
		rec[16], rec[17], rec[18] = gc[0], gc[1], gc[2]
		rec[19] = clamp01f(float32(l.Penumbra))

		out = append(out, rec)
	}
	if len(out) == 0 {
		out = append(out, fallbackKeyLight())
		// The cascades are fitted to the same fallback direction, so the
		// synthetic light is the one that reads the shadow map.
		return out, 0
	}
	return out, primaryDirectionalIndex(b.Lights)
}

// fallbackKeyLight is the built-in directional light a bundle with no lights
// takes. Its direction, colour and intensity are the defaults
// resolveDirectionalLight substitutes, so a scene with no authored light shades
// exactly as it did before the light array arrived.
//
// The direction stays unnormalized here. litWGSL normalizes it, exactly as it
// normalizes scene.lightDir.
func fallbackKeyLight() packedLight {
	var rec packedLight
	rec[3] = lightKindDirectional
	rec[4], rec[5], rec[6] = -0.4, -1.0, -0.3
	rec[7] = 1
	rec[8], rec[9], rec[10] = 1, 0.96, 0.9
	rec[12] = 2     // decay, unread by a directional light
	rec[13] = 0.005 // shadow bias, unread by litWGSL
	return rec
}

// lightStorageCapacityFor returns the record capacity that holds count lights: a
// power of two, at least the minimum, never past the maximum. It mirrors
// sceneWebGPULightCapacityFor in the browser renderer.
func lightStorageCapacityFor(count int) int {
	capacity := lightCapacityMin
	for capacity < count && capacity < lightCapacityMax {
		capacity *= 2
	}
	if capacity > lightCapacityMax {
		return lightCapacityMax
	}
	return capacity
}

// ensureLightStorage grows the light storage buffer to hold count lights.
//
// Growing replaces the buffer, and the lit bind groups hold the old one, so this
// clears the environment bind group key. Frame calls
// ensureEnvironmentBindGroups later in the same frame, which rebuilds both lit
// bind groups against the new buffer.
func (r *Renderer) ensureLightStorage(count int) error {
	capacity := lightStorageCapacityFor(count)
	if r.lightStorageBuf != nil && r.lightStorageCap >= capacity {
		return nil
	}
	buf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  capacity * lightRecordSize,
		Usage: gpu.BufferUsageStorage | gpu.BufferUsageCopyDst,
		Label: "bundle.scene.lights",
	})
	if err != nil {
		return fmt.Errorf("bundle: create light storage buffer: %w", err)
	}
	if r.lightStorageBuf != nil {
		r.lightStorageBuf.Destroy()
		// Force ensureEnvironmentBindGroups to rebuild the lit bind groups.
		r.envBindGroupKey = ""
	}
	r.lightStorageBuf = buf
	r.lightStorageCap = capacity
	return nil
}

// lightStorageBytes packs the light records into a Renderer-owned buffer and
// returns it. Records past the buffer capacity are dropped; the count written
// into the scene uniform is clamped to match, so the shader never reads past
// the array.
func (r *Renderer) lightStorageBytes(records []packedLight) ([]byte, int) {
	count := len(records)
	if count > r.lightStorageCap {
		count = r.lightStorageCap
	}
	need := count * lightRecordSize
	if cap(r.lightEncodeScratch) < need {
		r.lightEncodeScratch = make([]byte, need)
	}
	out := r.lightEncodeScratch[:need]
	for i := 0; i < count; i++ {
		putFloat32s(out[i*lightRecordSize:(i+1)*lightRecordSize], records[i][:])
	}
	return out, count
}

func destroyPassResources(p *passResources) {
	if p == nil {
		return
	}
	if p.positions != nil {
		p.positions.Destroy()
	}
	if p.colors != nil {
		p.colors.Destroy()
	}
}

func destroyPrimitiveResources(p *primitiveResources) {
	if p == nil {
		return
	}
	if p.positions != nil {
		p.positions.Destroy()
	}
	if p.colors != nil {
		p.colors.Destroy()
	}
	if p.normals != nil {
		p.normals.Destroy()
	}
}

func destroySkinResources(s *skinResources) {
	if s == nil {
		return
	}
	if s.joints != nil {
		s.joints.Destroy()
	}
	if s.weights != nil {
		s.weights.Destroy()
	}
	if s.bindPose != nil {
		s.bindPose.Destroy()
	}
}

// float64sToFloat32Bytes reinterprets a slice of float64 as little-endian
// float32 bytes. The bundle uses float64 for server-side readability; GPU
// buffers want float32.
func float64sToFloat32Bytes(src []float64) []byte {
	if len(src) == 0 {
		return nil
	}
	out := make([]byte, len(src)*4)
	for i, f := range src {
		bits := math.Float32bits(float32(f))
		out[i*4+0] = byte(bits)
		out[i*4+1] = byte(bits >> 8)
		out[i*4+2] = byte(bits >> 16)
		out[i*4+3] = byte(bits >> 24)
	}
	return out
}

// instanceRecordBytes allocates and packs one instance record per transform.
// Frame does not use it — recordCullPass packs into a reusable buffer through
// instanceRecordInto. Single-shot callers such as the object-mesh upload path
// use this wrapper.
func instanceRecordBytes(transforms []float64, instanceCount int, pickBase uint32) []byte {
	if instanceCount <= 0 {
		return nil
	}
	out := make([]byte, instanceCount*instanceRecordStride)
	instanceRecordInto(out, transforms, instanceCount, pickBase)
	return out
}

// float32sToBytes encodes a float32 slice as little-endian bytes.
func float32sToBytes(src []float32) []byte {
	if len(src) == 0 {
		return nil
	}
	out := make([]byte, len(src)*4)
	for i, f := range src {
		bits := math.Float32bits(f)
		out[i*4+0] = byte(bits)
		out[i*4+1] = byte(bits >> 8)
		out[i*4+2] = byte(bits >> 16)
		out[i*4+3] = byte(bits >> 24)
	}
	return out
}

func putUint32LE(dst []byte, v uint32) {
	dst[0] = byte(v)
	dst[1] = byte(v >> 8)
	dst[2] = byte(v >> 16)
	dst[3] = byte(v >> 24)
}

func whiteColorsFor(vertexCount int) []byte {
	out := make([]byte, vertexCount*12)
	one := math.Float32bits(1.0)
	for i := 0; i < vertexCount*3; i++ {
		out[i*4+0] = byte(one)
		out[i*4+1] = byte(one >> 8)
		out[i*4+2] = byte(one >> 16)
		out[i*4+3] = byte(one >> 24)
	}
	return out
}

// parseBackground parses a #rrggbb clear-color string; malformed input falls
// back to a visible near-black so bad data stays debuggable.
func parseBackground(s string) gpu.Color {
	if rgb, ok := tryParseCSSColor(s); ok {
		return gpu.Color{R: float64(rgb[0]), G: float64(rgb[1]), B: float64(rgb[2]), A: 1}
	}
	return gpu.Color{R: 0.05, G: 0.06, B: 0.08, A: 1}
}

// parseCSSColor parses a #rrggbb string to a normalized RGB triplet; on
// failure returns the provided fallback so call sites don't need to check.
func parseCSSColor(s string, fallback [3]float32) [3]float32 {
	if rgb, ok := tryParseCSSColor(s); ok {
		return rgb
	}
	return fallback
}

// tryParseCSSColor decodes "#rrggbb". It reads the hex digits directly instead
// of going through fmt.Sscanf, which allocated a scan state on every call.
// Frame parses the background plus several material and light colours, so this
// used to be the largest remaining per-frame allocation source.
func tryParseCSSColor(s string) ([3]float32, bool) {
	if len(s) != 7 || s[0] != '#' {
		return [3]float32{}, false
	}
	var channels [3]float32
	for i := 0; i < 3; i++ {
		hi, ok := hexNibble(s[1+i*2])
		if !ok {
			return [3]float32{}, false
		}
		lo, ok := hexNibble(s[2+i*2])
		if !ok {
			return [3]float32{}, false
		}
		channels[i] = float32(hi<<4|lo) / 255
	}
	return channels, true
}

func hexNibble(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
