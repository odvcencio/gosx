package bundle

import (
	"errors"
	"fmt"
	"math"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/gpu"
)

// shadowMapSize is the square resolution of the directional-light shadow map.
// 2048 is a reasonable default for a single non-cascaded shadow; R3 replaces
// this with cascaded shadow maps sized per-cascade.
const shadowMapSize = 2048

// Renderer consumes engine.RenderBundle values and issues draw calls against
// a gpu.Device. One Renderer instance serves one canvas / one engine runtime.
// Not safe for concurrent use.
type Renderer struct {
	device        gpu.Device
	surface       gpu.Surface
	surfaceFormat gpu.TextureFormat
	depthFormat   gpu.TextureFormat

	// Pipelines created once and reused across frames.
	unlitPipeline      gpu.RenderPipeline
	unlitBGLayout      gpu.BindGroupLayout
	litPipeline        gpu.RenderPipeline
	litBGLayout        gpu.BindGroupLayout
	litMaterialLayout  gpu.BindGroupLayout
	shadowPipeline     gpu.RenderPipeline
	shadowBGLayout     gpu.BindGroupLayout

	// Scene uniforms (viewProj + lightViewProj + camera + light). 192 bytes.
	sceneUniformBuf gpu.Buffer
	// Shadow-pass uniforms: just lightViewProj. 64 bytes.
	shadowUniformBuf gpu.Buffer

	// Per-pipeline bind groups. The lit group also holds shadow map + sampler.
	unlitBindGrp  gpu.BindGroup
	litBindGrp    gpu.BindGroup
	shadowBindGrp gpu.BindGroup

	// Main-pass depth attachment, resized lazily to surface.
	depthTex    gpu.Texture
	depthView   gpu.TextureView
	depthWidth  int
	depthHeight int

	// Shadow-map depth texture + view. Fixed size; content refreshed per frame.
	shadowTex     gpu.Texture
	shadowView    gpu.TextureView
	shadowSampler gpu.Sampler

	// Shared material texture sampler (separate from the comparison sampler
	// used for shadows; this one does anisotropic color lookup).
	materialSampler gpu.Sampler
	// 1x1 white fallback texture bound when a material has no Texture URL.
	fallbackTexture *textureResources

	// Caches keyed by identity strings, reused across frames.
	passCache      map[string]*passResources
	primitiveCache map[string]*primitiveResources
	materialCache  map[materialFingerprint]*materialResources
	textureCache   map[string]*textureResources

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

// Config configures a Renderer.
type Config struct {
	// Device is the GPU device to draw on. Required.
	Device gpu.Device
	// Surface is the render surface (typically a canvas). Required.
	Surface gpu.Surface
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
	r := &Renderer{
		device:         cfg.Device,
		surface:        cfg.Surface,
		surfaceFormat:  cfg.Device.PreferredSurfaceFormat(),
		depthFormat:    gpu.FormatDepth24Plus,
		passCache:      make(map[string]*passResources),
		primitiveCache: make(map[string]*primitiveResources),
		materialCache:  make(map[materialFingerprint]*materialResources),
		textureCache:   make(map[string]*textureResources),
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
	if err := r.buildShadowPipeline(); err != nil {
		return nil, err
	}
	if err := r.buildBindGroups(); err != nil {
		return nil, err
	}
	return r, nil
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
	}
	r.materialCache = nil
	for _, tx := range r.textureCache {
		if tx != nil && tx.tex != nil {
			tx.tex.Destroy()
		}
	}
	r.textureCache = nil
	if r.fallbackTexture != nil && r.fallbackTexture.tex != nil {
		r.fallbackTexture.tex.Destroy()
		r.fallbackTexture = nil
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
	if r.shadowUniformBuf != nil {
		r.shadowUniformBuf.Destroy()
	}
	if r.unlitPipeline != nil {
		r.unlitPipeline.Destroy()
	}
	if r.litPipeline != nil {
		r.litPipeline.Destroy()
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
	_ = timeSeconds // R2 does not consume time directly; future phases will.

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
	lightViewProj := computeLightViewProj(lightDir)

	r.device.Queue().WriteBuffer(r.sceneUniformBuf, 0, buildSceneUniformBytes(sceneUniformBlock{
		viewProj:      viewProj,
		lightViewProj: lightViewProj,
		cameraPos:     [4]float32{float32(b.Camera.X), float32(b.Camera.Y), float32(b.Camera.Z), 1},
		lightDir:      [4]float32{lightDir[0], lightDir[1], lightDir[2], 0},
		lightColor:    lightColor,
		ambientColor:  ambientColor,
		skyColor:      skyColor,
		groundColor:   groundColor,
	}))
	r.device.Queue().WriteBuffer(r.shadowUniformBuf, 0, float32sToBytes(lightViewProj[:]))

	enc := r.device.CreateCommandEncoder()

	// 1) Shadow pass — depth-only from the light's perspective.
	r.recordShadowPass(enc, b)

	// 2) Main pass — color + depth to the surface, lit + shadowed.
	view, err := r.device.AcquireSurfaceView(r.surface)
	if err != nil {
		return fmt.Errorf("bundle.Frame: acquire surface view: %w", err)
	}
	mainPass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.RenderPassColorAttachment{{
			View:       view,
			LoadOp:     gpu.LoadOpClear,
			StoreOp:    gpu.StoreOpStore,
			ClearValue: parseBackground(b.Background),
		}},
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
			if im.InstanceCount <= 0 || len(im.Transforms) == 0 {
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
			mainPass.SetVertexBuffer(4, r.instanceBuf)
			mainPass.Draw(prim.vertexCount, im.InstanceCount, 0, 0)
		}
	}

	mainPass.End()
	r.device.Queue().Submit(enc.Finish())
	return nil
}

// recordShadowPass issues the depth-only instanced-mesh draws into the shadow
// map. Runs before any uniform uploads the main pass depends on except for
// the instance-transform buffer, which is shared.
func (r *Renderer) recordShadowPass(enc gpu.CommandEncoder, b engine.RenderBundle) {
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		DepthStencilAttachment: &gpu.RenderPassDepthStencilAttachment{
			View:            r.shadowView,
			DepthLoadOp:     gpu.LoadOpClear,
			DepthStoreOp:    gpu.StoreOpStore,
			DepthClearValue: 1.0,
		},
		Label: "bundle.shadow",
	})
	if len(b.InstancedMeshes) > 0 {
		pass.SetPipeline(r.shadowPipeline)
		pass.SetBindGroup(0, r.shadowBindGrp)
		for _, im := range b.InstancedMeshes {
			if im.InstanceCount <= 0 || len(im.Transforms) == 0 {
				continue
			}
			if im.CastShadow == false {
				// The engine runtime defaults CastShadow=false in places;
				// drawing anyway keeps the R2 demo visible. R3 honors the
				// flag strictly.
			}
			prim, err := r.ensurePrimitive(im.Kind)
			if err != nil || prim == nil || prim.vertexCount == 0 {
				continue
			}
			transformBytes := float64sToFloat32Bytes(im.Transforms)
			if err := r.ensureInstanceBuffer(len(transformBytes)); err != nil {
				continue
			}
			r.device.Queue().WriteBuffer(r.instanceBuf, 0, transformBytes)
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

// buildUniformBuffers allocates the scene (192 bytes) and shadow (64 bytes)
// uniform buffers. Shared by multiple pipelines via their own bind groups.
func (r *Renderer) buildUniformBuffers() error {
	scene, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  sceneUniformSize,
		Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
		Label: "bundle.scene.uniforms",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildUniformBuffers (scene): %w", err)
	}
	shadow, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  64,
		Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
		Label: "bundle.shadow.uniforms",
	})
	if err != nil {
		scene.Destroy()
		return fmt.Errorf("bundle.buildUniformBuffers (shadow): %w", err)
	}
	r.sceneUniformBuf = scene
	r.shadowUniformBuf = shadow
	return nil
}

// buildShadowResources creates the shadow map texture + view + comparison
// sampler used for directional-light shadows.
func (r *Renderer) buildShadowResources() error {
	tex, err := r.device.CreateTexture(gpu.TextureDesc{
		Width:  shadowMapSize,
		Height: shadowMapSize,
		Format: gpu.FormatDepth32Float,
		Usage:  gpu.TextureUsageRenderAttachment | gpu.TextureUsageTextureBinding,
		Label:  "bundle.shadow.map",
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
	r.shadowView = tex.CreateView()
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
				{Format: r.surfaceFormat, WriteMask: gpu.ColorWriteAll},
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
				{Format: r.surfaceFormat, WriteMask: gpu.ColorWriteAll},
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
// holds three resources: scene uniforms, shadow map, shadow sampler.
func (r *Renderer) buildBindGroups() error {
	unlit, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout:  r.unlitBGLayout,
		Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: r.sceneUniformBuf, Size: sceneUniformSize}},
		Label:   "bundle.unlit.bindgroup",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildBindGroups (unlit): %w", err)
	}
	lit, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: r.litBGLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: r.sceneUniformBuf, Size: sceneUniformSize},
			{Binding: 1, TextureView: r.shadowView},
			{Binding: 2, Sampler: r.shadowSampler},
		},
		Label: "bundle.lit.bindgroup",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildBindGroups (lit): %w", err)
	}
	shadow, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout:  r.shadowBGLayout,
		Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: r.shadowUniformBuf, Size: 64}},
		Label:   "bundle.shadow.bindgroup",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildBindGroups (shadow): %w", err)
	}
	r.unlitBindGrp = unlit
	r.litBindGrp = lit
	r.shadowBindGrp = shadow
	return nil
}

// sceneUniformSize is the layout size of the Scene struct in WGSL. 224
// bytes: two mat4 (128) + six vec4 (96).
const sceneUniformSize = 224

type sceneUniformBlock struct {
	viewProj, lightViewProj mat4
	cameraPos               [4]float32
	lightDir                [4]float32
	lightColor              [4]float32
	ambientColor            [4]float32
	skyColor                [4]float32
	groundColor             [4]float32
}

func buildSceneUniformBytes(s sceneUniformBlock) []byte {
	out := make([]byte, sceneUniformSize)
	copy(out[0:64], float32sToBytes(s.viewProj[:]))
	copy(out[64:128], float32sToBytes(s.lightViewProj[:]))
	copy(out[128:144], float32sToBytes(s.cameraPos[:]))
	copy(out[144:160], float32sToBytes(s.lightDir[:]))
	copy(out[160:176], float32sToBytes(s.lightColor[:]))
	copy(out[176:192], float32sToBytes(s.ambientColor[:]))
	copy(out[192:208], float32sToBytes(s.skyColor[:]))
	copy(out[208:224], float32sToBytes(s.groundColor[:]))
	return out
}

// resolveDirectionalLight picks a primary directional light from the bundle's
// Lights + Environment. If none exist it falls back to a tasteful default —
// unlit demos should still render usefully.
func resolveDirectionalLight(b engine.RenderBundle) (dir [3]float32, color [4]float32, ambient [4]float32) {
	dir = [3]float32{-0.4, -1.0, -0.3}
	color = [4]float32{1, 0.96, 0.9, 1.0}    // w = intensity
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
