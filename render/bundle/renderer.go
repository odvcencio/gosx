package bundle

import (
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/gpu"
)

// shadowMapSize is the square resolution of each cascaded-shadow-map layer.
// 2048² per cascade × 3 cascades = ~48 MB of depth memory on depth32float.
const shadowMapSize = 2048

// cascadeCount is the number of shadow cascades. Three covers near/mid/far
// sensibly for common 100-unit scenes. Increasing costs linear memory + draw
// time; decreasing leaves mid-range shadows banded. R4 can make this tunable.
const cascadeCount = 3

// Renderer consumes engine.RenderBundle values and issues draw calls against
// a gpu.Device. One Renderer instance serves one canvas / one engine runtime.
// Not safe for concurrent use.
type Renderer struct {
	device        gpu.Device
	surface       gpu.Surface
	surfaceFormat gpu.TextureFormat
	depthFormat   gpu.TextureFormat

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
	shadowPipeline           gpu.RenderPipeline
	shadowBGLayout           gpu.BindGroupLayout

	// Scene uniforms (viewProj + 3 lightViewProjs + camera + light + env).
	sceneUniformBuf gpu.Buffer
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

	// Shared material texture sampler (separate from the comparison sampler
	// used for shadows; this one does anisotropic color lookup).
	materialSampler gpu.Sampler
	// 1x1 white fallback texture bound when a material has no Texture URL.
	fallbackTexture     *textureResources
	fallbackCubeTexture *textureResources
	envBindGroupKey     string

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

	// Bloom chain (bright-pass + 2 blur passes → composited into present).
	brightPipeline gpu.RenderPipeline
	brightBGLayout gpu.BindGroupLayout
	blurPipeline   gpu.RenderPipeline
	blurBGLayout   gpu.BindGroupLayout
	bloom          *bloomResources

	// Compute-particle pipelines. Per-system resources live in particleCache.
	particleUpdatePipeline gpu.ComputePipeline
	particleUpdateBGLayout gpu.BindGroupLayout
	particleRenderPipeline gpu.RenderPipeline
	particleRenderBGLayout gpu.BindGroupLayout

	// Tracks the previous frame's time for particle dt integration.
	lastFrameTime float64

	// Frame stats + device-lost state. Populated on every Frame call.
	stats frameStatsRecorder

	// Caches keyed by identity strings, reused across frames.
	passCache          map[string]*passResources
	primitiveCache     map[string]*primitiveResources
	materialCache      map[materialFingerprint]*materialResources
	textureCache       map[string]*textureResources
	cullCache          map[string]*cullResources
	particleCache      map[string]*particleResources
	skinCache          map[string]*skinResources
	bonePalettes       map[string]*BonePalette
	defaultBonePalette *BonePalette

	// Reusable per-instance transform buffer. Grows one-way to fit the
	// largest instance count seen. R2 uses a single buffer since instanced
	// meshes draw sequentially within a frame.
	instanceBuf      gpu.Buffer
	instanceBufBytes int
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
		device:         cfg.Device,
		surface:        cfg.Surface,
		surfaceFormat:  cfg.Device.PreferredSurfaceFormat(),
		depthFormat:    gpu.FormatDepth24Plus,
		hdrFormat:      hdrFormat,
		passCache:      make(map[string]*passResources),
		primitiveCache: make(map[string]*primitiveResources),
		materialCache:  make(map[materialFingerprint]*materialResources),
		textureCache:   make(map[string]*textureResources),
		cullCache:      make(map[string]*cullResources),
		particleCache:  make(map[string]*particleResources),
		skinCache:      make(map[string]*skinResources),
		bonePalettes:   make(map[string]*BonePalette),
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
	if err := r.buildUnlitPipeline(); err != nil {
		return nil, err
	}
	if err := r.buildLitPipeline(); err != nil {
		return nil, err
	}
	if err := r.buildSkinnedLitPipeline(); err != nil {
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
	if r.idBufferTex != nil {
		r.idBufferTex.Destroy()
		r.idBufferTex = nil
	}
	if r.bloom != nil {
		destroyBloomResources(r.bloom)
		r.bloom = nil
	}
	if r.brightPipeline != nil {
		r.brightPipeline.Destroy()
	}
	if r.blurPipeline != nil {
		r.blurPipeline.Destroy()
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
	if r.instanceBuf != nil {
		r.instanceBuf.Destroy()
		r.instanceBuf = nil
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
	if r.shadowPipeline != nil {
		r.shadowPipeline.Destroy()
	}
}

// Frame renders a bundle to the current surface image. Performs two render
// passes per frame:
//
//  1. Shadow pass — depth-only draw of all instanced meshes from the primary
//     directional light's POV into the shadow map texture.
//  2. Main pass — color + depth render to the surface. The lit pipeline
//     samples the shadow map from step 1 via a comparison sampler.
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
	depthView, err := r.ensureDepth(width, height)
	if err != nil {
		return err
	}

	viewProj := computeMVP(b.Camera, width, height)
	lightDir, lightColor, ambientColor := resolveDirectionalLight(b)
	skyColor, groundColor := resolveHemisphereAmbient(b)
	cascades := computeCascades(b.Camera, lightDir)

	r.device.Queue().WriteBuffer(r.sceneUniformBuf, 0, buildSceneUniformBytes(sceneUniformBlock{
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
	}))
	for i := 0; i < cascadeCount; i++ {
		r.device.Queue().WriteBuffer(r.shadowUniformBufs[i], 0, float32sToBytes(cascades.viewProjs[i][:]))
	}

	// Extract frustum planes once per frame for GPU-driven culling.
	frustum := extractFrustumPlanes(viewProj)

	enc := r.device.CreateCommandEncoder()

	// 1) One shadow pass per cascade. Shadow passes intentionally don't run
	// through culling — a shadow caster outside the main frustum can still
	// cast into it. CSM cascades bound the shadow draw volume on their own.
	for i := 0; i < cascadeCount; i++ {
		r.recordShadowPass(enc, b, i)
	}

	// 2) GPU-driven culling: compute pass writes a compacted visible-
	// transforms buffer + indirect draw args per InstancedMesh. Must run
	// BEFORE the main render pass since compute + render can't interleave
	// within the same pass encoder.
	if err := r.recordCullPass(enc, b, frustum); err != nil {
		return err
	}

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
	if err := r.ensurePostFX(width, height); err != nil {
		return err
	}
	if err := r.ensureEnvironmentBindGroups(b.Environment); err != nil {
		return err
	}

	// 3) Main pass — lit scene rendered to the HDR intermediate with depth,
	// plus the GPU picking id buffer as a second color attachment.
	mainPass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.RenderPassColorAttachment{
			{
				View:       r.hdrView,
				LoadOp:     gpu.LoadOpClear,
				StoreOp:    gpu.StoreOpStore,
				ClearValue: parseBackground(b.Background),
			},
			{
				// pick ID = 0 means "background / not a pickable surface".
				View:       r.idBufferView,
				LoadOp:     gpu.LoadOpClear,
				StoreOp:    gpu.StoreOpStore,
				ClearValue: gpu.Color{R: 0, G: 0, B: 0, A: 0},
			},
		},
		DepthStencilAttachment: &gpu.RenderPassDepthStencilAttachment{
			View:            depthView,
			DepthLoadOp:     gpu.LoadOpClear,
			DepthStoreOp:    gpu.StoreOpStore,
			DepthClearValue: 1.0,
		},
		Label: "bundle.main",
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

	// Lit instanced meshes. Resolve each entry's material before binding
	// because material bind groups are created lazily and may write their
	// backing uniform buffer — writeBuffer is disallowed inside a pass, so
	// this materialization happens between the two passes (shadow pass
	// already ended) rather than mid-draw.
	if len(b.InstancedMeshes) > 0 {
		materials := make([]*materialResources, len(b.InstancedMeshes))
		for i, im := range b.InstancedMeshes {
			fp := resolveMaterialFingerprint(b, im)
			mat, err := r.ensureMaterial(fp)
			if err != nil {
				mainPass.End()
				return err
			}
			materials[i] = mat
		}
		mainPass.SetPipeline(r.litPipeline)
		mainPass.SetBindGroup(0, r.litBindGrp)
		for i, im := range b.InstancedMeshes {
			if isSkinnedMesh(im) || im.InstanceCount <= 0 || len(im.Transforms) == 0 {
				continue
			}
			prim, err := r.ensurePrimitive(im.Kind)
			if err != nil {
				mainPass.End()
				return err
			}
			if prim == nil || prim.vertexCount == 0 {
				continue
			}
			mainPass.SetBindGroup(1, materials[i].bindGroup)
			mainPass.SetVertexBuffer(0, prim.positions)
			mainPass.SetVertexBuffer(1, prim.colors)
			mainPass.SetVertexBuffer(2, prim.normals)
			mainPass.SetVertexBuffer(3, prim.uvs)
			cull, _ := r.cullCache[instancedMeshKey(i, im)]
			if cull != nil {
				mainPass.SetVertexBuffer(4, cull.outputBuf)
				mainPass.DrawIndirect(cull.drawArgsBuf, 0)
			} else {
				mainPass.SetVertexBuffer(4, r.instanceBuf)
				mainPass.Draw(prim.vertexCount, im.InstanceCount, 0, 0)
			}
		}

		mainPass.SetPipeline(r.skinnedLitPipeline)
		mainPass.SetBindGroup(0, r.skinnedLitBindGrp)
		for i, im := range b.InstancedMeshes {
			if !isSkinnedMesh(im) || im.InstanceCount <= 0 || len(im.Transforms) == 0 {
				continue
			}
			prim, err := r.ensurePrimitive(im.Kind)
			if err != nil {
				mainPass.End()
				return err
			}
			if prim == nil || prim.vertexCount == 0 {
				continue
			}
			skin, err := r.ensureSkinBuffers(instancedMeshKey(i, im), prim.vertexCount, im)
			if err != nil {
				mainPass.End()
				return err
			}
			palette := r.bonePaletteForMesh(im)
			if palette == nil || palette.bindGroup == nil {
				mainPass.End()
				return fmt.Errorf("bundle.Frame: skinned mesh %q has no bone palette", im.ID)
			}
			mainPass.SetBindGroup(1, materials[i].skinnedBindGroup)
			mainPass.SetBindGroup(2, palette.bindGroup)
			mainPass.SetVertexBuffer(0, prim.positions)
			mainPass.SetVertexBuffer(1, prim.colors)
			mainPass.SetVertexBuffer(2, prim.normals)
			mainPass.SetVertexBuffer(3, prim.uvs)
			mainPass.SetVertexBuffer(5, skin.joints)
			mainPass.SetVertexBuffer(6, skin.weights)
			mainPass.SetVertexBuffer(7, skin.bindPose)
			cull, _ := r.cullCache[instancedMeshKey(i, im)]
			if cull != nil {
				mainPass.SetVertexBuffer(4, cull.outputBuf)
				mainPass.DrawIndirect(cull.drawArgsBuf, 0)
			} else {
				// Should never happen — recordCullPass populates the cache
				// for every mesh before the main pass runs. Falling back to
				// non-culled draw prevents a missing frame if it does.
				mainPass.SetVertexBuffer(4, r.instanceBuf)
				mainPass.Draw(prim.vertexCount, im.InstanceCount, 0, 0)
			}
		}
	}

	// Particles last in the main pass so they composite additively over the
	// opaque lit geometry, with depth test but no depth write.
	r.drawParticles(mainPass, b)

	mainPass.End()

	// 3b) If a pick is queued, copy the requested pixel from the id buffer
	// into a staging buffer for async readback after submission. Must run
	// between the main pass (which writes the id buffer) and any later
	// passes that might clobber it.
	r.recordPickCopy(enc, width, height)

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

	r.device.Queue().Submit(enc.Finish())

	// After submission, kick off the async pick readback if one was queued.
	// Runs in a goroutine — the frame completes immediately.
	r.finishPickReadback()

	// Record frame timing for Stats(). dt was already computed above for
	// particle integration; reuse it here so the numbers match.
	r.stats.record(dt)
	return nil
}

// recordShadowPass renders cascade-specific depth-only draws into the
// cascade's layer of the shadow texture array. Called once per cascade index.
// The instance-transform buffer is shared across cascades — it's written
// once per Frame before any pass begins (outside this function).
// instancedMeshKey returns the cull-cache key for one InstancedMesh slot.
// Combines the bundle index with the Kind so different mesh entries using
// the same Kind (e.g., two cube-only layers) don't share cull output.
func instancedMeshKey(idx int, im engine.RenderInstancedMesh) string {
	return fmt.Sprintf("%d:%s", idx, im.Kind)
}

// recordCullPass uploads per-mesh instance data, resets indirect-draw args,
// and dispatches the culling compute shader for every InstancedMesh in the
// bundle. The compacted output + draw args land in GPU buffers that the
// main pass reads later via DrawIndirect.
func (r *Renderer) recordCullPass(enc gpu.CommandEncoder, b engine.RenderBundle, frustum [6][4]float32) error {
	if len(b.InstancedMeshes) == 0 {
		return nil
	}
	// Upload instance transforms + reset draw args BEFORE beginning the
	// compute pass — writeBuffer operations within an open pass are not
	// allowed.
	for i, im := range b.InstancedMeshes {
		if im.InstanceCount <= 0 || len(im.Transforms) == 0 {
			continue
		}
		prim, err := r.ensurePrimitive(im.Kind)
		if err != nil {
			return err
		}
		if prim == nil || prim.vertexCount == 0 {
			continue
		}
		key := instancedMeshKey(i, im)
		cull, err := r.ensureCullResources(key, im.InstanceCount)
		if err != nil {
			return err
		}
		transformBytes := float64sToFloat32Bytes(im.Transforms)
		r.device.Queue().WriteBuffer(cull.inputBuf, 0, transformBytes)
		r.device.Queue().WriteBuffer(cull.drawArgsBuf, 0, drawArgsResetBytes(uint32(prim.vertexCount)))
		r.device.Queue().WriteBuffer(cull.cullUniform, 0,
			cullUniformBytes(frustum, uint32(prim.vertexCount), defaultCullRadius(im.Kind)))
	}

	pass := enc.BeginComputePass()
	pass.SetPipeline(r.cullPipeline)
	for i, im := range b.InstancedMeshes {
		if im.InstanceCount <= 0 || len(im.Transforms) == 0 {
			continue
		}
		key := instancedMeshKey(i, im)
		cull, ok := r.cullCache[key]
		if !ok {
			continue
		}
		pass.SetBindGroup(0, cull.bindGroup)
		// workgroup_size is 64 in the shader; dispatch (N+63)/64 groups.
		groups := (im.InstanceCount + 63) / 64
		pass.DispatchWorkgroups(groups, 1, 1)
	}
	pass.End()
	return nil
}

// defaultCullRadius returns a conservative bounding-sphere radius for a
// primitive kind, accounting for a unit primitive scaled by the instance
// transform. Per-instance uniform scale factors out into the diagonal of
// the transform — a real implementation would read that in the shader —
// but for R3's MVP we use a fixed padded radius per primitive kind.
func defaultCullRadius(kind string) float32 {
	switch kind {
	case "plane", "planeGeometry":
		return 8.0 // planes are often very large in world units
	case "sphere", "sphereGeometry":
		return 1.1
	case "cube", "box", "boxGeometry":
		return 1.8 // sqrt(3) padded
	}
	return 2.0
}

func (r *Renderer) recordShadowPass(enc gpu.CommandEncoder, b engine.RenderBundle, cascade int) {
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		DepthStencilAttachment: &gpu.RenderPassDepthStencilAttachment{
			View:            r.shadowLayerViews[cascade],
			DepthLoadOp:     gpu.LoadOpClear,
			DepthStoreOp:    gpu.StoreOpStore,
			DepthClearValue: 1.0,
		},
		Label: "bundle.shadow.cascade",
	})
	if len(b.InstancedMeshes) > 0 {
		pass.SetPipeline(r.shadowPipeline)
		pass.SetBindGroup(0, r.shadowBindGrps[cascade])
		for _, im := range b.InstancedMeshes {
			if im.InstanceCount <= 0 || len(im.Transforms) == 0 {
				continue
			}
			prim, err := r.ensurePrimitive(im.Kind)
			if err != nil || prim == nil || prim.vertexCount == 0 {
				continue
			}
			// Instance buffer is populated once per frame on cascade 0 (or
			// first pass to see instances). Writing on cascade 0 ensures it's
			// ready before cascades 1 and 2 record their draws.
			if cascade == 0 {
				transformBytes := float64sToFloat32Bytes(im.Transforms)
				if err := r.ensureInstanceBuffer(len(transformBytes)); err != nil {
					continue
				}
				r.device.Queue().WriteBuffer(r.instanceBuf, 0, transformBytes)
			}
			pass.SetVertexBuffer(0, prim.positions)
			pass.SetVertexBuffer(1, r.instanceBuf)
			pass.Draw(prim.vertexCount, im.InstanceCount, 0, 0)
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
		Usage:  gpu.TextureUsageRenderAttachment,
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

// ensurePrimitive uploads the geometry for a Kind on first request.
func (r *Renderer) ensurePrimitive(kind string) (*primitiveResources, error) {
	if res, ok := r.primitiveCache[kind]; ok {
		return res, nil
	}
	geo := primitiveForKind(kind)
	if geo == nil {
		return nil, nil
	}
	posBuf, err := r.uploadVertexBuffer(geo.positions, "bundle.primitive.positions:"+kind)
	if err != nil {
		return nil, err
	}
	colBuf, err := r.uploadVertexBuffer(geo.colors, "bundle.primitive.colors:"+kind)
	if err != nil {
		posBuf.Destroy()
		return nil, err
	}
	nrmBuf, err := r.uploadVertexBuffer(geo.normals, "bundle.primitive.normals:"+kind)
	if err != nil {
		posBuf.Destroy()
		colBuf.Destroy()
		return nil, err
	}
	uvBuf, err := r.uploadVertexBuffer(geo.uvs, "bundle.primitive.uvs:"+kind)
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
	r.primitiveCache[kind] = res
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

// ensureInstanceBuffer grows the shared per-instance buffer to at least size
// bytes. Growth is one-way.
func (r *Renderer) ensureInstanceBuffer(size int) error {
	if size <= r.instanceBufBytes {
		return nil
	}
	if r.instanceBuf != nil {
		r.instanceBuf.Destroy()
		r.instanceBuf = nil
	}
	grown := size + size/4
	buf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  grown,
		Usage: gpu.BufferUsageVertex | gpu.BufferUsageCopyDst,
		Label: "bundle.instance.transforms",
	})
	if err != nil {
		return fmt.Errorf("bundle: create instance buffer: %w", err)
	}
	r.instanceBuf = buf
	r.instanceBufBytes = grown
	return nil
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
				{ArrayStride: 64, StepMode: gpu.StepInstance, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 4, Offset: 0, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 5, Offset: 16, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 6, Offset: 32, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 7, Offset: 48, Format: gpu.VertexFormatFloat32x4},
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
				{ArrayStride: 64, StepMode: gpu.StepInstance, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 4, Offset: 0, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 5, Offset: 16, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 6, Offset: 32, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 7, Offset: 48, Format: gpu.VertexFormatFloat32x4},
				}},
				{ArrayStride: 16, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 8, Offset: 0, Format: gpu.VertexFormatUint32x4},
				}},
				{ArrayStride: 16, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 9, Offset: 0, Format: gpu.VertexFormatFloat32x4},
				}},
				{ArrayStride: 64, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 10, Offset: 0, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 11, Offset: 16, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 12, Offset: 32, Format: gpu.VertexFormatFloat32x4},
					{ShaderLocation: 13, Offset: 48, Format: gpu.VertexFormatFloat32x4},
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
				{ArrayStride: 64, StepMode: gpu.StepInstance, Attributes: []gpu.VertexAttribute{
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
		},
		Label: label,
	})
}

// sceneUniformSize is the layout size of the Scene struct in WGSL. 4 mat4
// (viewProj + 3 cascade lightViewProjs) = 256, plus 8 vec4 = 128 -> 384 bytes.
const sceneUniformSize = 384

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
}

func buildSceneUniformBytes(s sceneUniformBlock) []byte {
	out := make([]byte, sceneUniformSize)
	copy(out[0:64], float32sToBytes(s.viewProj[:]))
	for i := 0; i < cascadeCount; i++ {
		copy(out[64+i*64:64+(i+1)*64], float32sToBytes(s.lightViewProjs[i][:]))
	}
	base := 64 + cascadeCount*64
	copy(out[base+0:base+16], float32sToBytes(s.cameraPos[:]))
	copy(out[base+16:base+32], float32sToBytes(s.lightDir[:]))
	copy(out[base+32:base+48], float32sToBytes(s.lightColor[:]))
	copy(out[base+48:base+64], float32sToBytes(s.ambientColor[:]))
	copy(out[base+64:base+80], float32sToBytes(s.skyColor[:]))
	copy(out[base+80:base+96], float32sToBytes(s.groundColor[:]))
	copy(out[base+96:base+112], float32sToBytes(s.cascadeSplits[:]))
	copy(out[base+112:base+128], float32sToBytes(s.envParams[:]))
	return out
}

// resolveDirectionalLight picks a primary directional light from the bundle's
// Lights + Environment. If none exist it falls back to a tasteful default —
// unlit demos should still render usefully.
func resolveDirectionalLight(b engine.RenderBundle) (dir [3]float32, color [4]float32, ambient [4]float32) {
	dir = [3]float32{-0.4, -1.0, -0.3}
	color = [4]float32{1, 0.96, 0.9, 1.0} // w = intensity
	ambient = [4]float32{0.35, 0.38, 0.45, 0.35}

	for _, l := range b.Lights {
		if l.Kind == "directional" {
			dx, dy, dz := float32(l.DirectionX), float32(l.DirectionY), float32(l.DirectionZ)
			if dx == 0 && dy == 0 && dz == 0 {
				break
			}
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
			break
		}
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

func tryParseCSSColor(s string) ([3]float32, bool) {
	if len(s) == 7 && s[0] == '#' {
		var r, g, b byte
		if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err == nil {
			return [3]float32{float32(r) / 255, float32(g) / 255, float32(b) / 255}, true
		}
	}
	return [3]float32{}, false
}
