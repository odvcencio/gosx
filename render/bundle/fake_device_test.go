package bundle

import (
	"github.com/odvcencio/gosx/render/gpu"
)

// fakeDevice is a gpu.Device test double that records every call against it
// instead of issuing real GPU work. Tests assert on the recorded log to
// verify the renderer's translation logic without needing a GPU backend.
type fakeDevice struct {
	format        gpu.TextureFormat
	formatSupport map[gpu.TextureFormat]bool

	buffers          []*fakeBuffer
	textures         []*fakeTexture
	samplers         []*fakeSampler
	shaders          []*fakeShader
	pipelines        []*fakePipeline
	computePipelines []*fakeComputePipeline
	bindGroups       []*fakeBindGroup
	encoders         []*fakeEncoder
	surfaceViews     int

	queue *fakeQueue
}

func newFakeDevice() *fakeDevice {
	d := &fakeDevice{format: gpu.FormatBGRA8Unorm}
	d.queue = &fakeQueue{}
	return d
}

func (d *fakeDevice) Queue() gpu.Queue                          { return d.queue }
func (d *fakeDevice) PreferredSurfaceFormat() gpu.TextureFormat { return d.format }
func (d *fakeDevice) SupportsTextureFormat(format gpu.TextureFormat) bool {
	if d.formatSupport != nil {
		supported, ok := d.formatSupport[format]
		if ok {
			return supported
		}
	}
	switch format {
	case gpu.FormatRGBA8Unorm, gpu.FormatRGBA8UnormSRGB,
		gpu.FormatBGRA8Unorm, gpu.FormatBGRA8UnormSRGB,
		gpu.FormatRGBA16Float, gpu.FormatRGBA32Float,
		gpu.FormatRGB10A2Unorm,
		gpu.FormatDepth16Unorm, gpu.FormatDepth24Plus,
		gpu.FormatDepth24PlusStencil8, gpu.FormatDepth32Float,
		gpu.FormatR32Uint:
		return true
	default:
		return false
	}
}

func (d *fakeDevice) CreateBuffer(desc gpu.BufferDesc) (gpu.Buffer, error) {
	b := &fakeBuffer{size: desc.Size, usage: desc.Usage, label: desc.Label}
	d.buffers = append(d.buffers, b)
	return b, nil
}

func (d *fakeDevice) CreateTexture(desc gpu.TextureDesc) (gpu.Texture, error) {
	t := &fakeTexture{desc: desc}
	d.textures = append(d.textures, t)
	return t, nil
}

func (d *fakeDevice) CreateSampler(desc gpu.SamplerDesc) (gpu.Sampler, error) {
	s := &fakeSampler{desc: desc}
	d.samplers = append(d.samplers, s)
	return s, nil
}

func (d *fakeDevice) CreateShaderModule(desc gpu.ShaderDesc) (gpu.ShaderModule, error) {
	s := &fakeShader{src: desc.SourceWGSL, label: desc.Label}
	d.shaders = append(d.shaders, s)
	return s, nil
}

func (d *fakeDevice) CreateRenderPipeline(desc gpu.RenderPipelineDesc) (gpu.RenderPipeline, error) {
	p := &fakePipeline{desc: desc, layout: &fakeBindGroupLayout{}}
	d.pipelines = append(d.pipelines, p)
	return p, nil
}

func (d *fakeDevice) CreateBindGroup(desc gpu.BindGroupDesc) (gpu.BindGroup, error) {
	bg := &fakeBindGroup{desc: desc}
	d.bindGroups = append(d.bindGroups, bg)
	return bg, nil
}

func (d *fakeDevice) CreateCommandEncoder() gpu.CommandEncoder {
	e := &fakeEncoder{}
	d.encoders = append(d.encoders, e)
	return e
}

func (d *fakeDevice) CreateComputePipeline(desc gpu.ComputePipelineDesc) (gpu.ComputePipeline, error) {
	p := &fakeComputePipeline{desc: desc, layout: &fakeBindGroupLayout{}}
	d.computePipelines = append(d.computePipelines, p)
	return p, nil
}

func (d *fakeDevice) AcquireSurfaceView(gpu.Surface) (gpu.TextureView, error) {
	d.surfaceViews++
	return &fakeTextureView{}, nil
}

func (d *fakeDevice) OnLost(func(string, string)) {}
func (d *fakeDevice) Destroy()                    {}

// fakeQueue records WriteBuffer + Submit calls.
type fakeQueue struct {
	writes        []queueWrite
	textureWrites []textureWrite
	submits       [][]gpu.CommandBuffer
}

type queueWrite struct {
	buffer gpu.Buffer
	offset int
	bytes  int
	data   []byte
}

func (q *fakeQueue) WriteBuffer(b gpu.Buffer, offset int, data []byte) {
	q.writes = append(q.writes, queueWrite{
		buffer: b,
		offset: offset,
		bytes:  len(data),
		data:   append([]byte(nil), data...),
	})
}

func (q *fakeQueue) WriteTexture(_ gpu.Texture, data []byte, bytesPerRow, width, height int) {
	q.textureWrites = append(q.textureWrites, textureWrite{
		mipLevel:     0,
		bytes:        len(data),
		bytesPerRow:  bytesPerRow,
		rowsPerImage: height,
		width:        width,
		height:       height,
	})
	// Recorded only if tests need it; current tests assert via textures slice.
}

func (q *fakeQueue) WriteTextureLevel(_ gpu.Texture, mipLevel int, data []byte, bytesPerRow, width, height int) {
	q.textureWrites = append(q.textureWrites, textureWrite{
		mipLevel:     mipLevel,
		bytes:        len(data),
		bytesPerRow:  bytesPerRow,
		rowsPerImage: height,
		width:        width,
		height:       height,
	})
}

func (q *fakeQueue) WriteTextureLevelLayer(_ gpu.Texture, mipLevel, layer int, data []byte, bytesPerRow, rowsPerImage, width, height int) {
	q.textureWrites = append(q.textureWrites, textureWrite{
		mipLevel:     mipLevel,
		layer:        layer,
		bytes:        len(data),
		bytesPerRow:  bytesPerRow,
		rowsPerImage: rowsPerImage,
		width:        width,
		height:       height,
	})
}

func (q *fakeQueue) Submit(cmds ...gpu.CommandBuffer) {
	q.submits = append(q.submits, cmds)
}

type textureWrite struct {
	mipLevel     int
	layer        int
	bytes        int
	bytesPerRow  int
	rowsPerImage int
	width        int
	height       int
}

// fakeBuffer is a zero-cost test Buffer.
type fakeBuffer struct {
	size  int
	usage gpu.BufferUsage
	label string
}

func (b *fakeBuffer) Size() int              { return b.size }
func (b *fakeBuffer) Usage() gpu.BufferUsage { return b.usage }
func (b *fakeBuffer) Destroy()               {}
func (b *fakeBuffer) ReadAsync(size int) ([]byte, error) {
	return make([]byte, size), nil
}

// fakeShader holds the source for inspection.
type fakeShader struct {
	src   string
	label string
}

func (s *fakeShader) Destroy() {}

// fakePipeline records its descriptor and vends a stub layout.
type fakePipeline struct {
	desc   gpu.RenderPipelineDesc
	layout *fakeBindGroupLayout
}

func (p *fakePipeline) GetBindGroupLayout(int) gpu.BindGroupLayout { return p.layout }
func (p *fakePipeline) Destroy()                                   {}

type fakeBindGroupLayout struct{}

type fakeBindGroup struct {
	desc gpu.BindGroupDesc
}

func (b *fakeBindGroup) Destroy() {}

// fakeEncoder and fakeRenderPass record calls.
type fakeEncoder struct {
	passes        []*fakeRenderPass
	computePasses []*fakeComputePass
}

func (e *fakeEncoder) BeginRenderPass(desc gpu.RenderPassDesc) gpu.RenderPassEncoder {
	p := &fakeRenderPass{desc: desc}
	e.passes = append(e.passes, p)
	return p
}

func (e *fakeEncoder) BeginComputePass() gpu.ComputePassEncoder {
	p := &fakeComputePass{}
	e.computePasses = append(e.computePasses, p)
	return p
}

func (e *fakeEncoder) CopyTextureToBuffer(gpu.TextureCopyInfo, gpu.BufferCopyInfo, int, int, int) {
}

func (e *fakeEncoder) Finish() gpu.CommandBuffer { return &fakeCommandBuffer{} }

type fakeComputePipeline struct {
	desc   gpu.ComputePipelineDesc
	layout *fakeBindGroupLayout
}

func (p *fakeComputePipeline) GetBindGroupLayout(int) gpu.BindGroupLayout { return p.layout }
func (p *fakeComputePipeline) Destroy()                                   {}

type fakeComputePass struct {
	pipelineSet  bool
	bindGroupSet bool
	dispatches   []fakeDispatch
	ended        bool
}

type fakeDispatch struct{ x, y, z int }

func (p *fakeComputePass) SetPipeline(gpu.ComputePipeline) { p.pipelineSet = true }
func (p *fakeComputePass) SetBindGroup(int, gpu.BindGroup) { p.bindGroupSet = true }
func (p *fakeComputePass) DispatchWorkgroups(x, y, z int) {
	p.dispatches = append(p.dispatches, fakeDispatch{x, y, z})
}
func (p *fakeComputePass) End() { p.ended = true }

type fakeRenderPass struct {
	desc          gpu.RenderPassDesc
	pipelineSet   bool
	bindGroupSet  bool
	vbufSets      int
	draws         []fakeDraw
	indirectDraws int
	ended         bool
}

type fakeDraw struct {
	vertexCount, instanceCount, firstVertex, firstInstance int
}

func (p *fakeRenderPass) SetPipeline(gpu.RenderPipeline)             { p.pipelineSet = true }
func (p *fakeRenderPass) SetBindGroup(int, gpu.BindGroup)            { p.bindGroupSet = true }
func (p *fakeRenderPass) SetVertexBuffer(int, gpu.Buffer)            { p.vbufSets++ }
func (p *fakeRenderPass) SetIndexBuffer(gpu.Buffer, gpu.IndexFormat) {}
func (p *fakeRenderPass) Draw(vc, ic, fv, fi int) {
	p.draws = append(p.draws, fakeDraw{vc, ic, fv, fi})
}
func (p *fakeRenderPass) DrawIndexed(int, int, int, int, int) {}
func (p *fakeRenderPass) DrawIndirect(gpu.Buffer, int) {
	// Represents an indirect draw — we don't know instance count until the
	// GPU reads the buffer, so record a sentinel the tests can spot.
	p.indirectDraws++
}
func (p *fakeRenderPass) End() { p.ended = true }

type fakeCommandBuffer struct{}
type fakeTextureView struct {
	desc gpu.TextureViewDesc
}

type fakeTexture struct {
	desc gpu.TextureDesc
}

func (t *fakeTexture) Width() int                { return t.desc.Width }
func (t *fakeTexture) Height() int               { return t.desc.Height }
func (t *fakeTexture) Format() gpu.TextureFormat { return t.desc.Format }
func (t *fakeTexture) CreateView() gpu.TextureView {
	return &fakeTextureView{}
}
func (t *fakeTexture) CreateViewDesc(desc gpu.TextureViewDesc) gpu.TextureView {
	return &fakeTextureView{desc: desc}
}
func (t *fakeTexture) CreateLayerView(layer int) gpu.TextureView {
	return t.CreateViewDesc(gpu.TextureViewDesc{
		Dimension:       gpu.TextureViewDimension2D,
		BaseArrayLayer:  layer,
		ArrayLayerCount: 1,
	})
}
func (t *fakeTexture) Destroy() {}

type fakeSampler struct {
	desc gpu.SamplerDesc
}

func (*fakeSampler) Destroy() {}

// fakeSurface is a minimal Surface implementation for tests.
type fakeSurface struct{}
