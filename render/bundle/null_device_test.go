package bundle

import (
	"m31labs.dev/gosx/render/gpu"
)

// nullDevice is a gpu.Device that succeeds at everything and records nothing.
// fakeDevice keeps a copy of every buffer write, which makes it useless for
// allocation benchmarks; headless runs a software rasterizer, which buries the
// renderer's own cost. nullDevice isolates the renderer's CPU path so a frame
// benchmark measures the code under test and nothing else.
type nullDevice struct {
	queue nullQueue
	// buffers counts every CreateBuffer call so tests can assert the renderer
	// stopped allocating GPU resources per frame.
	buffers int
}

func newNullDevice() *nullDevice { return &nullDevice{} }

func (d *nullDevice) Queue() gpu.Queue                          { return &d.queue }
func (d *nullDevice) PreferredSurfaceFormat() gpu.TextureFormat { return gpu.FormatBGRA8Unorm }

func (d *nullDevice) SupportsTextureFormat(format gpu.TextureFormat) bool {
	switch format {
	case gpu.FormatRGBA8Unorm, gpu.FormatRGBA8UnormSRGB,
		gpu.FormatBGRA8Unorm, gpu.FormatBGRA8UnormSRGB,
		gpu.FormatRGBA16Float, gpu.FormatRGBA32Float,
		gpu.FormatRGB10A2Unorm,
		gpu.FormatDepth16Unorm, gpu.FormatDepth24Plus,
		gpu.FormatDepth24PlusStencil8, gpu.FormatDepth32Float,
		gpu.FormatR32Uint:
		return true
	}
	return false
}

func (d *nullDevice) CreateBuffer(desc gpu.BufferDesc) (gpu.Buffer, error) {
	d.buffers++
	return nullBuffer{size: desc.Size, usage: desc.Usage}, nil
}

func (d *nullDevice) CreateTexture(desc gpu.TextureDesc) (gpu.Texture, error) {
	return nullTexture{desc: desc}, nil
}

func (d *nullDevice) CreateSampler(gpu.SamplerDesc) (gpu.Sampler, error) {
	return nullSampler{}, nil
}

func (d *nullDevice) CreateShaderModule(gpu.ShaderDesc) (gpu.ShaderModule, error) {
	return nullShader{}, nil
}

func (d *nullDevice) CreateRenderPipeline(gpu.RenderPipelineDesc) (gpu.RenderPipeline, error) {
	return nullRenderPipeline{}, nil
}

func (d *nullDevice) CreateComputePipeline(gpu.ComputePipelineDesc) (gpu.ComputePipeline, error) {
	return nullComputePipeline{}, nil
}

func (d *nullDevice) CreateBindGroup(gpu.BindGroupDesc) (gpu.BindGroup, error) {
	return nullBindGroup{}, nil
}

func (d *nullDevice) CreateCommandEncoder() gpu.CommandEncoder { return nullEncoder{} }

func (d *nullDevice) AcquireSurfaceView(gpu.Surface) (gpu.TextureView, error) {
	return nullTextureView{}, nil
}

func (d *nullDevice) OnLost(func(string, string)) {}
func (d *nullDevice) Destroy()                    {}

type nullQueue struct{}

func (*nullQueue) WriteBuffer(gpu.Buffer, int, []byte)             {}
func (*nullQueue) WriteTexture(gpu.Texture, []byte, int, int, int) {}
func (*nullQueue) WriteTextureLevel(gpu.Texture, int, []byte, int, int, int) {
}
func (*nullQueue) WriteTextureLevelLayer(gpu.Texture, int, int, []byte, int, int, int, int) {
}
func (*nullQueue) Submit(...gpu.CommandBuffer) {}

type nullBuffer struct {
	size  int
	usage gpu.BufferUsage
}

func (b nullBuffer) Size() int              { return b.size }
func (b nullBuffer) Usage() gpu.BufferUsage { return b.usage }
func (nullBuffer) Destroy()                 {}
func (b nullBuffer) ReadAsync(size int) ([]byte, error) {
	return make([]byte, size), nil
}

type nullTexture struct{ desc gpu.TextureDesc }

func (t nullTexture) Width() int                                       { return t.desc.Width }
func (t nullTexture) Height() int                                      { return t.desc.Height }
func (t nullTexture) Format() gpu.TextureFormat                        { return t.desc.Format }
func (nullTexture) CreateView() gpu.TextureView                        { return nullTextureView{} }
func (nullTexture) CreateViewDesc(gpu.TextureViewDesc) gpu.TextureView { return nullTextureView{} }
func (nullTexture) CreateLayerView(int) gpu.TextureView                { return nullTextureView{} }
func (nullTexture) Destroy()                                           {}

type nullTextureView struct{}
type nullSampler struct{}

func (nullSampler) Destroy() {}

type nullShader struct{}

func (nullShader) Destroy() {}

type nullBindGroupLayout struct{}
type nullBindGroup struct{}

func (nullBindGroup) Destroy() {}

type nullRenderPipeline struct{}

func (nullRenderPipeline) GetBindGroupLayout(int) gpu.BindGroupLayout {
	return nullBindGroupLayout{}
}
func (nullRenderPipeline) Destroy() {}

type nullComputePipeline struct{}

func (nullComputePipeline) GetBindGroupLayout(int) gpu.BindGroupLayout {
	return nullBindGroupLayout{}
}
func (nullComputePipeline) Destroy() {}

type nullEncoder struct{}

func (nullEncoder) BeginRenderPass(gpu.RenderPassDesc) gpu.RenderPassEncoder {
	return nullRenderPass{}
}
func (nullEncoder) BeginComputePass() gpu.ComputePassEncoder { return nullComputePass{} }
func (nullEncoder) CopyTextureToBuffer(gpu.TextureCopyInfo, gpu.BufferCopyInfo, int, int, int) {
}
func (nullEncoder) Finish() gpu.CommandBuffer { return nullCommandBuffer{} }

type nullCommandBuffer struct{}

type nullRenderPass struct{}

func (nullRenderPass) SetPipeline(gpu.RenderPipeline)             {}
func (nullRenderPass) SetBindGroup(int, gpu.BindGroup)            {}
func (nullRenderPass) SetVertexBuffer(int, gpu.Buffer)            {}
func (nullRenderPass) SetIndexBuffer(gpu.Buffer, gpu.IndexFormat) {}
func (nullRenderPass) Draw(int, int, int, int)                    {}
func (nullRenderPass) DrawIndexed(int, int, int, int, int)        {}
func (nullRenderPass) DrawIndirect(gpu.Buffer, int)               {}
func (nullRenderPass) End()                                       {}

type nullComputePass struct{}

func (nullComputePass) SetPipeline(gpu.ComputePipeline)            {}
func (nullComputePass) SetBindGroup(int, gpu.BindGroup)            {}
func (nullComputePass) DispatchWorkgroups(int, int, int)           {}
func (nullComputePass) DispatchWorkgroupsIndirect(gpu.Buffer, int) {}
func (nullComputePass) End()                                       {}
