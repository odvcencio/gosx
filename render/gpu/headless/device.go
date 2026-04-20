package headless

import (
	"image"
	"image/color"

	"github.com/odvcencio/gosx/render/gpu"
)

// Device is a pure-Go gpu.Device whose "swap chain" is a CPU-backed RGBA
// image. Use New to construct one; the returned Surface represents the
// framebuffer and should be passed into render/bundle.Config.Surface.
type Device struct {
	framebuffer *image.RGBA
	width       int
	height      int
	queue       *Queue
	surface     *Surface
}

// New creates a headless device targeting a width×height RGBA framebuffer.
// Initial framebuffer contents are transparent black.
func New(width, height int) (*Device, *Surface) {
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	fb := image.NewRGBA(image.Rect(0, 0, width, height))
	d := &Device{
		framebuffer: fb,
		width:       width,
		height:      height,
	}
	d.queue = &Queue{device: d}
	d.surface = &Surface{device: d}
	return d, d.surface
}

// Framebuffer returns the backing RGBA image. Callers should copy bytes
// before mutating; the returned image aliases the live framebuffer.
func (d *Device) Framebuffer() *image.RGBA { return d.framebuffer }

// Encode as PNG / JPG / etc. lives at the call site; this package stays
// format-agnostic to keep its dependency surface minimal.

// Queue implements gpu.Queue.
type Queue struct {
	device *Device
}

// Surface is the headless "swap chain" — a handle pointing back at the
// Device's framebuffer.
type Surface struct {
	device *Device
}

// Satisfaction of gpu.Device ------------------------------------------------

func (d *Device) Queue() gpu.Queue                        { return d.queue }
func (d *Device) PreferredSurfaceFormat() gpu.TextureFormat { return gpu.FormatRGBA8UnormSRGB }

func (d *Device) CreateBuffer(desc gpu.BufferDesc) (gpu.Buffer, error) {
	return &Buffer{size: desc.Size, usage: desc.Usage, label: desc.Label,
		data: make([]byte, desc.Size)}, nil
}

func (d *Device) CreateTexture(desc gpu.TextureDesc) (gpu.Texture, error) {
	return &Texture{width: desc.Width, height: desc.Height, format: desc.Format}, nil
}

func (d *Device) CreateSampler(gpu.SamplerDesc) (gpu.Sampler, error) {
	return &Sampler{}, nil
}

func (d *Device) CreateShaderModule(gpu.ShaderDesc) (gpu.ShaderModule, error) {
	return &ShaderModule{}, nil
}

func (d *Device) CreateRenderPipeline(gpu.RenderPipelineDesc) (gpu.RenderPipeline, error) {
	return &RenderPipeline{}, nil
}

func (d *Device) CreateComputePipeline(gpu.ComputePipelineDesc) (gpu.ComputePipeline, error) {
	return &ComputePipeline{}, nil
}

func (d *Device) CreateBindGroup(gpu.BindGroupDesc) (gpu.BindGroup, error) {
	return &BindGroup{}, nil
}

func (d *Device) CreateCommandEncoder() gpu.CommandEncoder {
	return &CommandEncoder{device: d}
}

// AcquireSurfaceView returns a view that, when used as a color attachment,
// writes clears into the backing framebuffer.
func (d *Device) AcquireSurfaceView(s gpu.Surface) (gpu.TextureView, error) {
	_ = s
	return &SurfaceView{device: d}, nil
}

func (d *Device) OnLost(func(string, string)) {
	// Headless is never lost — it's a Go slice.
}

func (d *Device) Destroy() {}

// Queue -------------------------------------------------------------------

func (q *Queue) WriteBuffer(b gpu.Buffer, offset int, data []byte) {
	buf, ok := b.(*Buffer)
	if !ok || buf == nil {
		return
	}
	if offset+len(data) > len(buf.data) {
		return
	}
	copy(buf.data[offset:], data)
}

// WriteTexture blits raw bytes into a Texture; if the texture happens to
// be used as a color attachment later, the contents end up in the
// framebuffer on clear. For the R5 stub this is the main path by which
// non-clear pixels reach the output — a real rasterizer replaces this.
func (q *Queue) WriteTexture(t gpu.Texture, data []byte, bytesPerRow, width, height int) {
	tex, ok := t.(*Texture)
	if !ok || tex == nil {
		return
	}
	// Keep the last write so a caller can introspect; no per-pixel store
	// yet. R5 raster fills this in.
	tex.lastWriteSize = bytesPerRow * height
	_ = data
	_ = width
}

func (q *Queue) Submit(...gpu.CommandBuffer) {
	// Command execution happens eagerly in the encoder so Submit is a
	// no-op here. When the sibling rasterizer lands, real draw calls get
	// deferred until Submit batches them.
}

// Buffer, Texture, Sampler, etc. -----------------------------------------

type Buffer struct {
	size  int
	usage gpu.BufferUsage
	label string
	data  []byte
}

func (b *Buffer) Size() int                     { return b.size }
func (b *Buffer) Usage() gpu.BufferUsage        { return b.usage }
func (b *Buffer) Destroy()                      {}
func (b *Buffer) ReadAsync(size int) ([]byte, error) {
	out := make([]byte, size)
	copy(out, b.data)
	return out, nil
}

type Texture struct {
	width, height int
	format        gpu.TextureFormat
	lastWriteSize int
}

func (t *Texture) Width() int                             { return t.width }
func (t *Texture) Height() int                            { return t.height }
func (t *Texture) Format() gpu.TextureFormat              { return t.format }
func (t *Texture) CreateView() gpu.TextureView            { return &TextureView{owner: t} }
func (t *Texture) CreateLayerView(int) gpu.TextureView    { return &TextureView{owner: t} }
func (t *Texture) Destroy()                               {}

type TextureView struct{ owner *Texture }
type SurfaceView struct{ device *Device }

type Sampler struct{}

func (s *Sampler) Destroy() {}

type ShaderModule struct{}

func (s *ShaderModule) Destroy() {}

type RenderPipeline struct{}

func (p *RenderPipeline) GetBindGroupLayout(int) gpu.BindGroupLayout { return &BindGroupLayout{} }
func (p *RenderPipeline) Destroy()                                   {}

type ComputePipeline struct{}

func (p *ComputePipeline) GetBindGroupLayout(int) gpu.BindGroupLayout { return &BindGroupLayout{} }
func (p *ComputePipeline) Destroy()                                   {}

type BindGroup struct{}

func (b *BindGroup) Destroy() {}

type BindGroupLayout struct{}

// CommandEncoder ---------------------------------------------------------

type CommandEncoder struct {
	device *Device
}

func (e *CommandEncoder) BeginRenderPass(desc gpu.RenderPassDesc) gpu.RenderPassEncoder {
	// For any color attachment with LoadOpClear targeting the surface,
	// fill the framebuffer with the clear color. Clearing offscreen
	// targets is a no-op in the stub.
	for _, att := range desc.ColorAttachments {
		if att.LoadOp != gpu.LoadOpClear {
			continue
		}
		if _, ok := att.View.(*SurfaceView); !ok {
			continue
		}
		fill(e.device.framebuffer, att.ClearValue)
	}
	return &RenderPassEncoder{}
}

func (e *CommandEncoder) BeginComputePass() gpu.ComputePassEncoder {
	return &ComputePassEncoder{}
}

func (e *CommandEncoder) CopyTextureToBuffer(
	src gpu.TextureCopyInfo, dst gpu.BufferCopyInfo, w, h, d int) {
	// Real copy semantics would blit src → dst per bytesPerRow. Stub
	// leaves dst's data unchanged so pick readbacks return zero (=
	// background) on headless; fine for thumbnail use cases.
}

func (e *CommandEncoder) Finish() gpu.CommandBuffer { return &CommandBuffer{} }

type CommandBuffer struct{}

type RenderPassEncoder struct{}

func (r *RenderPassEncoder) SetPipeline(gpu.RenderPipeline)                 {}
func (r *RenderPassEncoder) SetBindGroup(int, gpu.BindGroup)                {}
func (r *RenderPassEncoder) SetVertexBuffer(int, gpu.Buffer)                {}
func (r *RenderPassEncoder) SetIndexBuffer(gpu.Buffer, gpu.IndexFormat)     {}
func (r *RenderPassEncoder) Draw(int, int, int, int)                        {}
func (r *RenderPassEncoder) DrawIndexed(int, int, int, int, int)            {}
func (r *RenderPassEncoder) DrawIndirect(gpu.Buffer, int)                   {}
func (r *RenderPassEncoder) End()                                           {}

type ComputePassEncoder struct{}

func (c *ComputePassEncoder) SetPipeline(gpu.ComputePipeline) {}
func (c *ComputePassEncoder) SetBindGroup(int, gpu.BindGroup) {}
func (c *ComputePassEncoder) DispatchWorkgroups(int, int, int) {}
func (c *ComputePassEncoder) End()                             {}

// fill paints the framebuffer with a clear color in sRGB-encoded bytes.
// Input color channels are already normalized 0..1.
func fill(img *image.RGBA, c gpu.Color) {
	r := uint8(clamp01(c.R) * 255)
	g := uint8(clamp01(c.G) * 255)
	b := uint8(clamp01(c.B) * 255)
	a := uint8(clamp01(c.A) * 255)
	col := color.RGBA{R: r, G: g, B: b, A: a}
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			img.SetRGBA(x, y, col)
		}
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
