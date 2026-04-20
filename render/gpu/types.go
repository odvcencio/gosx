package gpu

// TextureFormat names the pixel layout of a texture or render target.
// Names are canonical (not backend-specific strings). Backends translate on
// resource creation.
type TextureFormat int

const (
	FormatUndefined TextureFormat = iota

	// Color, 8-bit unorm variants.
	FormatRGBA8Unorm
	FormatRGBA8UnormSRGB
	FormatBGRA8Unorm
	FormatBGRA8UnormSRGB

	// Color, higher precision.
	FormatRGBA16Float
	FormatRGBA32Float

	// Depth / stencil.
	FormatDepth16Unorm
	FormatDepth24Plus
	FormatDepth24PlusStencil8
	FormatDepth32Float
)

// HasDepth reports whether f has a depth aspect.
func (f TextureFormat) HasDepth() bool {
	switch f {
	case FormatDepth16Unorm, FormatDepth24Plus, FormatDepth24PlusStencil8, FormatDepth32Float:
		return true
	}
	return false
}

// HasStencil reports whether f has a stencil aspect.
func (f TextureFormat) HasStencil() bool {
	return f == FormatDepth24PlusStencil8
}

// BufferUsage is a bitset of usages a buffer is valid for. Usages OR together;
// a vertex+copy_dst buffer is BufferUsageVertex|BufferUsageCopyDst.
type BufferUsage uint32

const (
	BufferUsageNone    BufferUsage = 0
	BufferUsageVertex  BufferUsage = 1 << 0
	BufferUsageIndex   BufferUsage = 1 << 1
	BufferUsageUniform BufferUsage = 1 << 2
	BufferUsageStorage BufferUsage = 1 << 3
	BufferUsageCopyDst BufferUsage = 1 << 4
	BufferUsageCopySrc BufferUsage = 1 << 5
	BufferUsageIndirect BufferUsage = 1 << 6
)

// Has reports whether u contains the requested bit.
func (u BufferUsage) Has(bit BufferUsage) bool { return u&bit == bit }

// PrimitiveTopology is the input-assembly topology for a render pipeline.
type PrimitiveTopology int

const (
	TopologyTriangleList PrimitiveTopology = iota
	TopologyTriangleStrip
	TopologyLineList
	TopologyLineStrip
	TopologyPointList
)

// CullMode selects which winding is discarded during rasterization.
type CullMode int

const (
	CullNone CullMode = iota
	CullFront
	CullBack
)

// FrontFace defines which winding is considered front-facing.
type FrontFace int

const (
	FrontFaceCCW FrontFace = iota
	FrontFaceCW
)

// IndexFormat names the index buffer element size.
type IndexFormat int

const (
	IndexFormatUint16 IndexFormat = iota
	IndexFormatUint32
)

// VertexFormat names the in-memory format of a single vertex attribute.
type VertexFormat int

const (
	VertexFormatFloat32 VertexFormat = iota
	VertexFormatFloat32x2
	VertexFormatFloat32x3
	VertexFormatFloat32x4
	VertexFormatUint32
	VertexFormatUint32x2
	VertexFormatUint32x3
	VertexFormatUint32x4
	VertexFormatUint8x4Unorm
)

// Stride returns the byte stride of a single attribute in the given format.
// Returns 0 for unknown formats — caller should validate before sizing a layout.
func (f VertexFormat) Stride() int {
	switch f {
	case VertexFormatFloat32, VertexFormatUint32:
		return 4
	case VertexFormatFloat32x2, VertexFormatUint32x2:
		return 8
	case VertexFormatFloat32x3, VertexFormatUint32x3:
		return 12
	case VertexFormatFloat32x4, VertexFormatUint32x4, VertexFormatUint8x4Unorm:
		return 16
	}
	return 0
}

// VertexStepMode selects per-vertex vs per-instance advancement for a buffer.
type VertexStepMode int

const (
	StepVertex VertexStepMode = iota
	StepInstance
)

// VertexAttribute is a single attribute inside a VertexBufferLayout.
type VertexAttribute struct {
	ShaderLocation int
	Offset         int
	Format         VertexFormat
}

// VertexBufferLayout describes one vertex or instance buffer slot.
type VertexBufferLayout struct {
	ArrayStride int
	StepMode    VertexStepMode
	Attributes  []VertexAttribute
}

// BlendFactor names a blend equation factor.
type BlendFactor int

const (
	BlendZero BlendFactor = iota
	BlendOne
	BlendSrcAlpha
	BlendOneMinusSrcAlpha
	BlendDstAlpha
	BlendOneMinusDstAlpha
)

// BlendOp names a blend equation operator.
type BlendOp int

const (
	BlendOpAdd BlendOp = iota
	BlendOpSubtract
	BlendOpReverseSubtract
	BlendOpMin
	BlendOpMax
)

// BlendComponent is a single color or alpha blend equation.
type BlendComponent struct {
	SrcFactor BlendFactor
	DstFactor BlendFactor
	Operation BlendOp
}

// BlendState is the blend state for a color target.
type BlendState struct {
	Color BlendComponent
	Alpha BlendComponent
}

// ColorTargetState is one color attachment on a pipeline.
type ColorTargetState struct {
	Format    TextureFormat
	Blend     *BlendState // nil = disabled
	WriteMask ColorWriteMask
}

// ColorWriteMask selects which channels are written.
type ColorWriteMask uint8

const (
	ColorWriteRed   ColorWriteMask = 1 << 0
	ColorWriteGreen ColorWriteMask = 1 << 1
	ColorWriteBlue  ColorWriteMask = 1 << 2
	ColorWriteAlpha ColorWriteMask = 1 << 3
	ColorWriteAll   ColorWriteMask = ColorWriteRed | ColorWriteGreen | ColorWriteBlue | ColorWriteAlpha
)

// CompareFunc names a depth-compare predicate.
type CompareFunc int

const (
	CompareAlways CompareFunc = iota
	CompareNever
	CompareLess
	CompareLessEqual
	CompareEqual
	CompareGreater
	CompareGreaterEqual
	CompareNotEqual
)

// DepthStencilState is the depth/stencil state for a pipeline. Nil means
// depth test disabled.
type DepthStencilState struct {
	Format            TextureFormat
	DepthWriteEnabled bool
	DepthCompare      CompareFunc
}

// LoadOp selects what happens to an attachment at pass start.
type LoadOp int

const (
	LoadOpLoad LoadOp = iota
	LoadOpClear
)

// StoreOp selects what happens to an attachment at pass end.
type StoreOp int

const (
	StoreOpStore StoreOp = iota
	StoreOpDiscard
)

// Color is an RGBA tuple used for clear values. Components are in 0..1.
type Color struct {
	R, G, B, A float64
}
