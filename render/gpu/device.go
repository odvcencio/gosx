package gpu

// Device is the entry-point interface into a GPU backend. Callers obtain a
// Device from a backend package (render/gpu/jsgpu.Open, etc.) and never
// construct one directly.
//
// All resource constructors may return ErrUnsupported if the backend cannot
// provide the feature, ErrInvalidDesc if the descriptor is malformed, or
// backend-specific errors for GPU-side failures.
type Device interface {
	// Queue returns the device's default submission queue. A Device has
	// exactly one default queue. Multi-queue support is deferred.
	Queue() Queue

	// PreferredSurfaceFormat returns the color format recommended by the
	// platform for the primary render surface. On WebGPU this is the result
	// of navigator.gpu.getPreferredCanvasFormat().
	PreferredSurfaceFormat() TextureFormat

	CreateBuffer(BufferDesc) (Buffer, error)
	CreateTexture(TextureDesc) (Texture, error)
	CreateSampler(SamplerDesc) (Sampler, error)
	CreateShaderModule(ShaderDesc) (ShaderModule, error)
	CreateRenderPipeline(RenderPipelineDesc) (RenderPipeline, error)
	CreateBindGroup(BindGroupDesc) (BindGroup, error)
	CreateCommandEncoder() CommandEncoder

	// AcquireSurfaceView returns a texture view for the current swap-chain
	// image, binding the passed Surface if needed. Implementations own
	// configuration and lifecycle of the underlying swap chain.
	AcquireSurfaceView(Surface) (TextureView, error)

	// Destroy releases the device. Resources created through it become invalid.
	Destroy()
}

// Queue is the command submission and resource-upload interface.
type Queue interface {
	// WriteBuffer copies data into buf at offset. The data is copied
	// immediately; buf does not reference data after the call returns.
	WriteBuffer(buf Buffer, offset int, data []byte)

	// Submit runs all command buffers on the GPU. Order is preserved.
	Submit(...CommandBuffer)
}

// Buffer is a GPU-side byte range.
type Buffer interface {
	Size() int
	Usage() BufferUsage
	Destroy()
}

// BufferDesc configures a buffer at creation time.
type BufferDesc struct {
	Size  int
	Usage BufferUsage
	Label string
}

// ShaderModule holds a compiled shader (e.g. a WGSL module containing one or
// more entry points).
type ShaderModule interface {
	Destroy()
}

// ShaderDesc is the source form of a shader module. Only SourceWGSL is
// supported by the WebGPU backend for R1. Future backends may accept other
// source languages or pre-compiled bytecode.
type ShaderDesc struct {
	SourceWGSL string
	Label      string
}

// RenderPipeline is an immutable compiled pipeline state object.
type RenderPipeline interface {
	// GetBindGroupLayout returns the auto-generated layout for a given group
	// slot. Only valid when the pipeline was created with AutoLayout: true.
	GetBindGroupLayout(group int) BindGroupLayout
	Destroy()
}

// BindGroupLayout describes the resource layout expected by a bind group slot.
type BindGroupLayout interface{}

// BindGroup is a bound set of resources (buffers, textures, samplers) for a
// single group slot in a pipeline.
type BindGroup interface {
	Destroy()
}

// BindGroupEntry is one resource binding within a BindGroup. Exactly one of
// Buffer, TextureView, or Sampler should be set. Backends validate.
type BindGroupEntry struct {
	Binding     int
	Buffer      Buffer      // uniform / storage buffer binding
	Offset      int         // buffer byte offset
	Size        int         // 0 = rest of buffer
	TextureView TextureView // sampled / depth / storage texture binding
	Sampler     Sampler     // sampler binding (filtering or comparison)
}

// BindGroupDesc describes a set of bound resources.
type BindGroupDesc struct {
	Layout  BindGroupLayout
	Entries []BindGroupEntry
	Label   string
}

// VertexStageDesc configures the vertex stage of a pipeline.
type VertexStageDesc struct {
	Module     ShaderModule
	EntryPoint string
	Buffers    []VertexBufferLayout
}

// FragmentStageDesc configures the fragment stage of a pipeline.
type FragmentStageDesc struct {
	Module     ShaderModule
	EntryPoint string
	Targets    []ColorTargetState
}

// PrimitiveState controls rasterization.
type PrimitiveState struct {
	Topology  PrimitiveTopology
	CullMode  CullMode
	FrontFace FrontFace
}

// RenderPipelineDesc describes a render pipeline.
type RenderPipelineDesc struct {
	Vertex       VertexStageDesc
	Fragment     FragmentStageDesc
	Primitive    PrimitiveState
	DepthStencil *DepthStencilState
	AutoLayout   bool // true: use "auto" pipeline layout (R1 default)
	Label        string
}

// CommandEncoder accumulates GPU commands into a CommandBuffer for submission.
type CommandEncoder interface {
	BeginRenderPass(RenderPassDesc) RenderPassEncoder
	Finish() CommandBuffer
}

// CommandBuffer is an immutable, submittable batch of GPU commands.
type CommandBuffer interface{}

// RenderPassEncoder records draw commands within a single render pass.
type RenderPassEncoder interface {
	SetPipeline(RenderPipeline)
	SetBindGroup(group int, bg BindGroup)
	SetVertexBuffer(slot int, buf Buffer)
	SetIndexBuffer(buf Buffer, format IndexFormat)
	Draw(vertexCount, instanceCount, firstVertex, firstInstance int)
	DrawIndexed(indexCount, instanceCount, firstIndex, baseVertex, firstInstance int)
	End()
}

// Surface is a presentable render target, typically a canvas. Backends own
// surface configuration; callers pass a Surface to AcquireSurfaceView per
// frame. Opaque from the abstraction's perspective — each backend defines
// its own concrete Surface type and type-asserts on the way in.
type Surface interface{}

// RenderPassDesc configures a render pass.
type RenderPassDesc struct {
	ColorAttachments        []RenderPassColorAttachment
	DepthStencilAttachment  *RenderPassDepthStencilAttachment
	Label                   string
}

// RenderPassColorAttachment configures one color attachment of a render pass.
type RenderPassColorAttachment struct {
	View       TextureView
	LoadOp     LoadOp
	StoreOp    StoreOp
	ClearValue Color
}

// RenderPassDepthStencilAttachment configures the depth/stencil attachment
// of a render pass.
type RenderPassDepthStencilAttachment struct {
	View            TextureView
	DepthLoadOp     LoadOp
	DepthStoreOp    StoreOp
	DepthClearValue float64
}

// TextureView is a view into a texture — the binding unit for render pass
// attachments and shader texture bindings.
type TextureView interface{}

// Sampler is a texture sampling state object. Created once and reused across
// bind groups.
type Sampler interface {
	Destroy()
}

// SamplerDesc configures a sampler at creation time. Zero-valued fields use
// backend defaults (clamp-to-edge, nearest, non-comparison).
type SamplerDesc struct {
	MagFilter    FilterMode
	MinFilter    FilterMode
	MipmapFilter FilterMode
	AddressU     AddressMode
	AddressV     AddressMode
	AddressW     AddressMode
	// Compare, when set to anything other than zero, makes this a comparison
	// sampler (sampler2DShadow in WGSL). Used for shadow map PCF.
	Compare CompareFunc
	Label   string
}

// FilterMode selects sample filtering.
type FilterMode int

const (
	FilterNearest FilterMode = iota
	FilterLinear
)

// AddressMode selects the wrap mode per UV axis.
type AddressMode int

const (
	AddressClampToEdge AddressMode = iota
	AddressRepeat
	AddressMirrorRepeat
)

// Texture is a GPU-side image resource. Call CreateView to obtain a bindable
// TextureView. Destroy frees the underlying memory.
type Texture interface {
	Width() int
	Height() int
	Format() TextureFormat
	CreateView() TextureView
	Destroy()
}

// TextureUsage is a bitset of usages a texture is valid for.
type TextureUsage uint32

const (
	TextureUsageNone             TextureUsage = 0
	TextureUsageCopySrc          TextureUsage = 1 << 0
	TextureUsageCopyDst          TextureUsage = 1 << 1
	TextureUsageTextureBinding   TextureUsage = 1 << 2
	TextureUsageStorageBinding   TextureUsage = 1 << 3
	TextureUsageRenderAttachment TextureUsage = 1 << 4
)

// Has reports whether u contains the requested bit.
func (u TextureUsage) Has(bit TextureUsage) bool { return u&bit == bit }

// TextureDesc configures a texture at creation time.
type TextureDesc struct {
	Width, Height        int
	DepthOrArrayLayers   int // 0 or 1 = 2D single layer
	Format               TextureFormat
	Usage                TextureUsage
	SampleCount          int // 0 or 1 = non-MSAA
	Label                string
}
