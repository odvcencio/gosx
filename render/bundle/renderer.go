package bundle

import (
	"errors"
	"fmt"
	"math"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/gpu"
)

// Renderer consumes engine.RenderBundle values and issues draw calls against
// a gpu.Device. The Renderer is not safe for concurrent use; one instance
// serves one canvas / one engine runtime.
type Renderer struct {
	device        gpu.Device
	surface       gpu.Surface
	surfaceFormat gpu.TextureFormat
	depthFormat   gpu.TextureFormat

	// Pipelines created once and reused.
	unlitPipeline      gpu.RenderPipeline
	unlitBGLayout      gpu.BindGroupLayout
	instancedPipeline  gpu.RenderPipeline
	instancedBGLayout  gpu.BindGroupLayout

	// Shared uniform buffer holding the MVP; bound by both pipelines.
	uniformBuf             gpu.Buffer
	unlitUniformBindGrp    gpu.BindGroup
	instancedUniformBindGrp gpu.BindGroup

	// Depth attachment, resized lazily to the surface dimensions.
	depthTex      gpu.Texture
	depthView     gpu.TextureView
	depthWidth    int
	depthHeight   int

	// Caches keyed by identity strings. Entries created on first use and
	// reused across frames; sizes never shrink.
	passCache      map[string]*passResources
	primitiveCache map[string]*primitiveResources

	// A reusable per-instance transform buffer. Grows to fit the largest
	// instance count seen so far; never shrinks. One buffer per renderer is
	// fine for R1 since we draw instanced meshes sequentially.
	instanceBuf      gpu.Buffer
	instanceBufBytes int
}

// passResources holds the per-pass GPU buffers for a cached RenderPassBundle.
type passResources struct {
	positions   gpu.Buffer
	colors      gpu.Buffer
	vertexCount int
}

// primitiveResources holds the GPU vertex/color buffers for one instanced-mesh
// Kind (e.g., "cube"). Uploaded once and reused across frames.
type primitiveResources struct {
	positions   gpu.Buffer
	colors      gpu.Buffer
	vertexCount int
}

// Config configures a Renderer.
type Config struct {
	// Device is the GPU device to draw on. Required.
	Device gpu.Device
	// Surface is the render surface (typically a canvas). Required.
	Surface gpu.Surface
}

// New constructs a Renderer. It immediately creates both the unlit and
// instanced pipelines plus the shared uniform bind groups.
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
	}
	if err := r.buildUniformBuf(); err != nil {
		return nil, err
	}
	if err := r.buildUnlitPipeline(); err != nil {
		return nil, err
	}
	if err := r.buildInstancedPipeline(); err != nil {
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
		if p.positions != nil {
			p.positions.Destroy()
		}
		if p.colors != nil {
			p.colors.Destroy()
		}
	}
	r.passCache = nil
	for _, p := range r.primitiveCache {
		if p.positions != nil {
			p.positions.Destroy()
		}
		if p.colors != nil {
			p.colors.Destroy()
		}
	}
	r.primitiveCache = nil
	if r.instanceBuf != nil {
		r.instanceBuf.Destroy()
		r.instanceBuf = nil
	}
	if r.depthTex != nil {
		r.depthTex.Destroy()
		r.depthTex = nil
	}
	if r.uniformBuf != nil {
		r.uniformBuf.Destroy()
	}
	if r.unlitPipeline != nil {
		r.unlitPipeline.Destroy()
	}
	if r.instancedPipeline != nil {
		r.instancedPipeline.Destroy()
	}
}

// Frame renders a bundle to the current surface image. Width and height are
// the canvas framebuffer dimensions in physical pixels; timeSeconds is
// available for animated effects (e.g., shader-time uniforms) but R1 only
// uses it indirectly through the bundle.
func (r *Renderer) Frame(b engine.RenderBundle, width, height int, timeSeconds float64) error {
	_ = timeSeconds // R1 does not consume time directly; future phases will.

	if width <= 0 || height <= 0 {
		return nil
	}
	view, err := r.device.AcquireSurfaceView(r.surface)
	if err != nil {
		return fmt.Errorf("bundle.Frame: acquire surface view: %w", err)
	}
	depthView, err := r.ensureDepth(width, height)
	if err != nil {
		return err
	}

	mvp := computeMVP(b.Camera, width, height)
	r.device.Queue().WriteBuffer(r.uniformBuf, 0, float32sToBytes(mvp[:]))

	clear := parseBackground(b.Background)

	enc := r.device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.RenderPassColorAttachment{{
			View:       view,
			LoadOp:     gpu.LoadOpClear,
			StoreOp:    gpu.StoreOpStore,
			ClearValue: clear,
		}},
		DepthStencilAttachment: &gpu.RenderPassDepthStencilAttachment{
			View:            depthView,
			DepthLoadOp:     gpu.LoadOpClear,
			DepthStoreOp:    gpu.StoreOpStore,
			DepthClearValue: 1.0,
		},
		Label: "bundle.frame",
	})

	// Unlit pre-batched passes (existing scene Passes from the engine runtime).
	if len(b.Passes) > 0 {
		pass.SetPipeline(r.unlitPipeline)
		pass.SetBindGroup(0, r.unlitUniformBindGrp)
		for _, pb := range b.Passes {
			res, err := r.ensurePassBuffers(pb)
			if err != nil {
				pass.End()
				return err
			}
			if res == nil || res.vertexCount == 0 {
				continue
			}
			pass.SetVertexBuffer(0, res.positions)
			pass.SetVertexBuffer(1, res.colors)
			pass.Draw(res.vertexCount, 1, 0, 0)
		}
	}

	// Instanced meshes from the RenderInstancedMesh bundle entries.
	if len(b.InstancedMeshes) > 0 {
		pass.SetPipeline(r.instancedPipeline)
		pass.SetBindGroup(0, r.instancedUniformBindGrp)
		for _, im := range b.InstancedMeshes {
			if im.InstanceCount <= 0 || len(im.Transforms) == 0 {
				continue
			}
			prim, err := r.ensurePrimitive(im.Kind)
			if err != nil {
				pass.End()
				return err
			}
			if prim == nil || prim.vertexCount == 0 {
				continue
			}
			transformBytes := float64sToFloat32Bytes(im.Transforms)
			if err := r.ensureInstanceBuffer(len(transformBytes)); err != nil {
				pass.End()
				return err
			}
			r.device.Queue().WriteBuffer(r.instanceBuf, 0, transformBytes)

			pass.SetVertexBuffer(0, prim.positions)
			pass.SetVertexBuffer(1, prim.colors)
			pass.SetVertexBuffer(2, r.instanceBuf)
			pass.Draw(prim.vertexCount, im.InstanceCount, 0, 0)
		}
	}

	pass.End()
	r.device.Queue().Submit(enc.Finish())
	return nil
}

// ensureDepth allocates or resizes the depth texture to match the surface.
// The old texture is destroyed on size change; callers can call every frame
// without worrying about leaks.
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

// ensurePrimitive uploads the geometry for an instanced-mesh Kind the first
// time it's requested; subsequent calls reuse the cached GPU buffers.
func (r *Renderer) ensurePrimitive(kind string) (*primitiveResources, error) {
	if res, ok := r.primitiveCache[kind]; ok {
		return res, nil
	}
	geo := primitiveForKind(kind)
	if geo == nil {
		// Unknown kind — skip silently. Future: log / diagnostic channel.
		return nil, nil
	}
	posBytes := float32sToBytes(geo.positions)
	colBytes := float32sToBytes(geo.colors)

	posBuf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  len(posBytes),
		Usage: gpu.BufferUsageVertex | gpu.BufferUsageCopyDst,
		Label: "bundle.primitive.positions:" + kind,
	})
	if err != nil {
		return nil, fmt.Errorf("bundle: create primitive position buffer: %w", err)
	}
	r.device.Queue().WriteBuffer(posBuf, 0, posBytes)

	colBuf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  len(colBytes),
		Usage: gpu.BufferUsageVertex | gpu.BufferUsageCopyDst,
		Label: "bundle.primitive.colors:" + kind,
	})
	if err != nil {
		posBuf.Destroy()
		return nil, fmt.Errorf("bundle: create primitive color buffer: %w", err)
	}
	r.device.Queue().WriteBuffer(colBuf, 0, colBytes)

	res := &primitiveResources{
		positions:   posBuf,
		colors:      colBuf,
		vertexCount: geo.vertexCount,
	}
	r.primitiveCache[kind] = res
	return res, nil
}

// ensureInstanceBuffer grows the shared per-instance buffer to at least size
// bytes. Growth is one-way — shrink is not worth the allocation churn.
func (r *Renderer) ensureInstanceBuffer(size int) error {
	if size <= r.instanceBufBytes {
		return nil
	}
	if r.instanceBuf != nil {
		r.instanceBuf.Destroy()
		r.instanceBuf = nil
	}
	// Over-allocate 25% to absorb small growths without reallocating. The
	// cost of the extra bytes is negligible; the cost of reallocating every
	// frame when instance count oscillates is high.
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
				{
					ArrayStride: 12,
					StepMode:    gpu.StepVertex,
					Attributes: []gpu.VertexAttribute{
						{ShaderLocation: 0, Offset: 0, Format: gpu.VertexFormatFloat32x3},
					},
				},
				{
					ArrayStride: 12,
					StepMode:    gpu.StepVertex,
					Attributes: []gpu.VertexAttribute{
						{ShaderLocation: 1, Offset: 0, Format: gpu.VertexFormatFloat32x3},
					},
				},
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

// buildInstancedPipeline creates the per-instance-transform pipeline used
// for RenderInstancedMesh entries. Slot 2 is a per-instance buffer carrying
// a 4x4 matrix split across four vec4 attributes (locations 2..5).
func (r *Renderer) buildInstancedPipeline() error {
	shader, err := r.device.CreateShaderModule(gpu.ShaderDesc{
		SourceWGSL: instancedWGSL,
		Label:      "bundle.instanced",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildInstancedPipeline: %w", err)
	}
	pipeline, err := r.device.CreateRenderPipeline(gpu.RenderPipelineDesc{
		Vertex: gpu.VertexStageDesc{
			Module:     shader,
			EntryPoint: "vs_main",
			Buffers: []gpu.VertexBufferLayout{
				{
					ArrayStride: 12,
					StepMode:    gpu.StepVertex,
					Attributes: []gpu.VertexAttribute{
						{ShaderLocation: 0, Offset: 0, Format: gpu.VertexFormatFloat32x3},
					},
				},
				{
					ArrayStride: 12,
					StepMode:    gpu.StepVertex,
					Attributes: []gpu.VertexAttribute{
						{ShaderLocation: 1, Offset: 0, Format: gpu.VertexFormatFloat32x3},
					},
				},
				{
					ArrayStride: 64, // 4 vec4 = mat4
					StepMode:    gpu.StepInstance,
					Attributes: []gpu.VertexAttribute{
						{ShaderLocation: 2, Offset: 0, Format: gpu.VertexFormatFloat32x4},
						{ShaderLocation: 3, Offset: 16, Format: gpu.VertexFormatFloat32x4},
						{ShaderLocation: 4, Offset: 32, Format: gpu.VertexFormatFloat32x4},
						{ShaderLocation: 5, Offset: 48, Format: gpu.VertexFormatFloat32x4},
					},
				},
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
		Label:      "bundle.instanced",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildInstancedPipeline: %w", err)
	}
	r.instancedPipeline = pipeline
	r.instancedBGLayout = pipeline.GetBindGroupLayout(0)
	return nil
}

func (r *Renderer) buildUniformBuf() error {
	buf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  64, // single mat4
		Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
		Label: "bundle.mvp",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildUniformBuf: %w", err)
	}
	r.uniformBuf = buf
	return nil
}

// buildBindGroups builds one bind group per pipeline, both pointing at the
// shared MVP uniform buffer. WebGPU requires a bind group per pipeline
// layout even when the underlying resource is identical.
func (r *Renderer) buildBindGroups() error {
	unlit, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout:  r.unlitBGLayout,
		Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: r.uniformBuf, Size: 64}},
		Label:   "bundle.unlit.bindgroup",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildBindGroups (unlit): %w", err)
	}
	inst, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout:  r.instancedBGLayout,
		Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: r.uniformBuf, Size: 64}},
		Label:   "bundle.instanced.bindgroup",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildBindGroups (instanced): %w", err)
	}
	r.unlitUniformBindGrp = unlit
	r.instancedUniformBindGrp = inst
	return nil
}

// float64sToFloat32Bytes reinterprets a slice of float64 as little-endian
// float32 bytes. The bundle type uses float64 for readability on the server
// side; GPU buffers want float32 to save bandwidth.
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

// parseBackground parses a simple #rrggbb string into a Color. Anything else
// returns an opaque near-black default so frames render even with malformed
// input — a visible wrong color is better than a silent crash.
func parseBackground(s string) gpu.Color {
	if len(s) == 7 && s[0] == '#' {
		var r, g, b byte
		if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err == nil {
			return gpu.Color{
				R: float64(r) / 255,
				G: float64(g) / 255,
				B: float64(b) / 255,
				A: 1,
			}
		}
	}
	return gpu.Color{R: 0.05, G: 0.06, B: 0.08, A: 1}
}
