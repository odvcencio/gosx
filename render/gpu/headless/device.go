package headless

import (
	"encoding/binary"
	"image"
	"image/color"
	"math"

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

func (d *Device) Queue() gpu.Queue                          { return d.queue }
func (d *Device) PreferredSurfaceFormat() gpu.TextureFormat { return gpu.FormatRGBA8UnormSRGB }

func (d *Device) CreateBuffer(desc gpu.BufferDesc) (gpu.Buffer, error) {
	return &Buffer{size: desc.Size, usage: desc.Usage, label: desc.Label,
		data: make([]byte, desc.Size)}, nil
}

func (d *Device) CreateTexture(desc gpu.TextureDesc) (gpu.Texture, error) {
	w := desc.Width
	if w <= 0 {
		w = 1
	}
	h := desc.Height
	if h <= 0 {
		h = 1
	}
	layers := desc.DepthOrArrayLayers
	if layers <= 0 {
		layers = 1
	}
	mips := desc.MipLevelCount
	if mips <= 0 {
		mips = 1
	}
	t := &Texture{width: w, height: h, layers: layers, format: desc.Format, mipLevels: mips}
	if bpp := bytesPerPixel(desc.Format); bpp > 0 {
		t.mipData = make([][]byte, mips)
		for level := 0; level < mips; level++ {
			lw, lh := mipSize(w, h, level)
			t.mipData[level] = make([]byte, lw*lh*layers*bpp)
		}
		t.data = t.mipData[0]
	}
	if !desc.Format.HasDepth() {
		t.mipRGBA = make([][]byte, mips)
		for level := 0; level < mips; level++ {
			lw, lh := mipSize(w, h, level)
			t.mipRGBA[level] = make([]byte, lw*lh*4)
		}
		t.rgba = t.mipRGBA[0]
	} else {
		t.depth = make([]float32, w*h*layers)
	}
	return t, nil
}

func (d *Device) CreateSampler(gpu.SamplerDesc) (gpu.Sampler, error) {
	return &Sampler{}, nil
}

func (d *Device) CreateShaderModule(gpu.ShaderDesc) (gpu.ShaderModule, error) {
	return &ShaderModule{}, nil
}

func (d *Device) CreateRenderPipeline(desc gpu.RenderPipelineDesc) (gpu.RenderPipeline, error) {
	return &RenderPipeline{desc: desc}, nil
}

func (d *Device) CreateComputePipeline(desc gpu.ComputePipelineDesc) (gpu.ComputePipeline, error) {
	return &ComputePipeline{desc: desc}, nil
}

func (d *Device) CreateBindGroup(desc gpu.BindGroupDesc) (gpu.BindGroup, error) {
	return &BindGroup{desc: desc}, nil
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
	if offset < 0 || offset+len(data) > len(buf.data) {
		return
	}
	copy(buf.data[offset:], data)
	buf.lastWriteOffset = offset
	buf.lastWriteSize = len(data)
}

// WriteTexture blits raw bytes into a Texture. The headless backend keeps
// both exact texture bytes and a display-oriented RGBA cache so render pass
// clears, texture readbacks, and the bundle present pass can be exercised
// without a real rasterizer.
func (q *Queue) WriteTexture(t gpu.Texture, data []byte, bytesPerRow, width, height int) {
	q.WriteTextureLevel(t, 0, data, bytesPerRow, width, height)
}

func (q *Queue) WriteTextureLevel(t gpu.Texture, mipLevel int, data []byte, bytesPerRow, width, height int) {
	tex, ok := t.(*Texture)
	if !ok || tex == nil {
		return
	}
	if bytesPerRow <= 0 || width <= 0 || height <= 0 || mipLevel < 0 || mipLevel >= tex.mipLevels {
		return
	}
	tex.lastWriteSize = bytesPerRow * height
	tex.lastWriteMipLevel = mipLevel
	bpp := bytesPerPixel(tex.format)
	if bpp == 0 {
		return
	}
	lw, lh := mipSize(tex.width, tex.height, mipLevel)
	dst := tex.levelData(mipLevel)
	copyW := min(width, lw)
	copyH := min(height, lh)
	for y := 0; y < copyH; y++ {
		srcOff := y * bytesPerRow
		dstOff := y * lw * bpp
		if srcOff >= len(data) || dstOff >= len(dst) {
			continue
		}
		rowBytes := min(copyW*bpp, len(data)-srcOff, len(dst)-dstOff)
		copy(dst[dstOff:min(dstOff+rowBytes, len(dst))], data[srcOff:srcOff+rowBytes])
	}
	tex.refreshRGBALevel(mipLevel)
}

func (q *Queue) Submit(...gpu.CommandBuffer) {
	// Command execution happens eagerly in the encoder so Submit is a
	// no-op here. When the sibling rasterizer lands, real draw calls get
	// deferred until Submit batches them.
}

// Buffer, Texture, Sampler, etc. -----------------------------------------

type Buffer struct {
	size            int
	usage           gpu.BufferUsage
	label           string
	data            []byte
	lastWriteOffset int
	lastWriteSize   int
}

func (b *Buffer) Size() int              { return b.size }
func (b *Buffer) Usage() gpu.BufferUsage { return b.usage }
func (b *Buffer) Destroy()               {}
func (b *Buffer) ReadAsync(size int) ([]byte, error) {
	out := make([]byte, size)
	copy(out, b.data)
	return out, nil
}

type Texture struct {
	width, height     int
	layers            int
	mipLevels         int
	format            gpu.TextureFormat
	lastWriteSize     int
	lastWriteMipLevel int
	data              []byte
	mipData           [][]byte
	rgba              []byte
	mipRGBA           [][]byte
	depth             []float32
}

func (t *Texture) Width() int                  { return t.width }
func (t *Texture) Height() int                 { return t.height }
func (t *Texture) Format() gpu.TextureFormat   { return t.format }
func (t *Texture) CreateView() gpu.TextureView { return &TextureView{owner: t, layer: -1} }
func (t *Texture) CreateLayerView(layer int) gpu.TextureView {
	return &TextureView{owner: t, layer: layer}
}
func (t *Texture) Destroy() {}

func (t *Texture) levelData(level int) []byte {
	if t == nil || level < 0 {
		return nil
	}
	if level == 0 && len(t.data) > 0 {
		return t.data
	}
	if level < len(t.mipData) {
		return t.mipData[level]
	}
	return nil
}

func (t *Texture) levelRGBA(level int) []byte {
	if t == nil || level < 0 {
		return nil
	}
	if level == 0 && len(t.rgba) > 0 {
		return t.rgba
	}
	if level < len(t.mipRGBA) {
		return t.mipRGBA[level]
	}
	return nil
}

type TextureView struct {
	owner *Texture
	layer int
}
type SurfaceView struct{ device *Device }

type Sampler struct{}

func (s *Sampler) Destroy() {}

type ShaderModule struct{}

func (s *ShaderModule) Destroy() {}

type RenderPipeline struct {
	desc gpu.RenderPipelineDesc
}

func (p *RenderPipeline) GetBindGroupLayout(int) gpu.BindGroupLayout { return &BindGroupLayout{} }
func (p *RenderPipeline) Destroy()                                   {}

type ComputePipeline struct {
	desc gpu.ComputePipelineDesc
}

func (p *ComputePipeline) GetBindGroupLayout(int) gpu.BindGroupLayout { return &BindGroupLayout{} }
func (p *ComputePipeline) Destroy()                                   {}

type BindGroup struct {
	desc gpu.BindGroupDesc
}

func (b *BindGroup) Destroy() {}

type BindGroupLayout struct{}

// CommandEncoder ---------------------------------------------------------

type CommandEncoder struct {
	device *Device
}

func (e *CommandEncoder) BeginRenderPass(desc gpu.RenderPassDesc) gpu.RenderPassEncoder {
	// For any color attachment with LoadOpClear targeting the surface,
	// fill the framebuffer with the clear color. Offscreen color textures
	// also retain their clear color so later readbacks or present passes
	// can observe the render target contents.
	for _, att := range desc.ColorAttachments {
		if att.LoadOp != gpu.LoadOpClear {
			continue
		}
		switch view := att.View.(type) {
		case *SurfaceView:
			fill(e.device.framebuffer, att.ClearValue)
		case *TextureView:
			clearTexture(view.owner, att.ClearValue)
		}
	}
	if att := desc.DepthStencilAttachment; att != nil && att.DepthLoadOp == gpu.LoadOpClear {
		if view, ok := att.View.(*TextureView); ok {
			clearDepthView(view.owner, view.layer, att.DepthClearValue)
		}
	}
	return &RenderPassEncoder{device: e.device, desc: desc}
}

func (e *CommandEncoder) BeginComputePass() gpu.ComputePassEncoder {
	return &ComputePassEncoder{}
}

func (e *CommandEncoder) CopyTextureToBuffer(
	src gpu.TextureCopyInfo, dst gpu.BufferCopyInfo, w, h, d int) {
	_ = d
	tex, ok := src.Texture.(*Texture)
	buf, bufOK := dst.Buffer.(*Buffer)
	if !ok || !bufOK || tex == nil || buf == nil {
		return
	}
	bpp := bytesPerPixel(tex.format)
	if bpp == 0 || dst.BytesPerRow <= 0 || w <= 0 || h <= 0 {
		return
	}
	level := src.MipLevel
	if level < 0 || level >= tex.mipLevels {
		return
	}
	levelW, levelH := mipSize(tex.width, tex.height, level)
	data := tex.levelData(level)
	layer := textureLayer(tex, src.Origin[2])
	for y := 0; y < h; y++ {
		sy := src.Origin[1] + y
		if sy < 0 || sy >= levelH {
			continue
		}
		for x := 0; x < w; x++ {
			sx := src.Origin[0] + x
			if sx < 0 || sx >= levelW {
				continue
			}
			srcOff := ((layer*levelH+sy)*levelW + sx) * bpp
			dstOff := dst.Offset + y*dst.BytesPerRow + x*bpp
			if srcOff < 0 || srcOff+bpp > len(data) ||
				dstOff < 0 || dstOff+bpp > len(buf.data) {
				continue
			}
			copy(buf.data[dstOff:dstOff+bpp], data[srcOff:srcOff+bpp])
		}
	}
}

func (e *CommandEncoder) Finish() gpu.CommandBuffer { return &CommandBuffer{} }

type CommandBuffer struct{}

type RenderPassEncoder struct {
	device        *Device
	desc          gpu.RenderPassDesc
	pipeline      *RenderPipeline
	bindGroup     *BindGroup
	bindGroups    map[int]*BindGroup
	vertexBuffers map[int]*Buffer
}

func (r *RenderPassEncoder) SetPipeline(p gpu.RenderPipeline) {
	if pipeline, ok := p.(*RenderPipeline); ok {
		r.pipeline = pipeline
	}
}
func (r *RenderPassEncoder) SetBindGroup(slot int, bg gpu.BindGroup) {
	if group, ok := bg.(*BindGroup); ok {
		if r.bindGroups == nil {
			r.bindGroups = make(map[int]*BindGroup)
		}
		r.bindGroups[slot] = group
		if slot == 0 {
			r.bindGroup = group
		}
	}
}
func (r *RenderPassEncoder) SetVertexBuffer(slot int, b gpu.Buffer) {
	if buf, ok := b.(*Buffer); ok {
		if r.vertexBuffers == nil {
			r.vertexBuffers = make(map[int]*Buffer)
		}
		r.vertexBuffers[slot] = buf
	}
}
func (r *RenderPassEncoder) SetIndexBuffer(gpu.Buffer, gpu.IndexFormat) {}
func (r *RenderPassEncoder) Draw(vertexCount, instanceCount, firstVertex, firstInstance int) {
	if r.desc.Label != "bundle.present" || r.bindGroup == nil {
		r.rasterizeDraw(vertexCount, instanceCount, firstVertex, firstInstance)
		return
	}
	var src *Texture
	for _, entry := range r.bindGroup.desc.Entries {
		if entry.Binding != 0 {
			continue
		}
		if view, ok := entry.TextureView.(*TextureView); ok {
			src = view.owner
		}
		break
	}
	if src == nil {
		return
	}
	for _, att := range r.desc.ColorAttachments {
		if _, ok := att.View.(*SurfaceView); ok && att.StoreOp != gpu.StoreOpDiscard {
			copyTextureToFramebuffer(src, r.device.framebuffer)
			return
		}
	}
}
func (r *RenderPassEncoder) DrawIndexed(int, int, int, int, int) {}
func (r *RenderPassEncoder) DrawIndirect(b gpu.Buffer, offset int) {
	buf, ok := b.(*Buffer)
	if !ok || buf == nil || offset < 0 || offset+16 > len(buf.data) {
		return
	}
	vertexCount := int(binary.LittleEndian.Uint32(buf.data[offset : offset+4]))
	instanceCount := int(binary.LittleEndian.Uint32(buf.data[offset+4 : offset+8]))
	firstVertex := int(binary.LittleEndian.Uint32(buf.data[offset+8 : offset+12]))
	firstInstance := int(binary.LittleEndian.Uint32(buf.data[offset+12 : offset+16]))
	r.Draw(vertexCount, instanceCount, firstVertex, firstInstance)
}
func (r *RenderPassEncoder) End() {}

type ComputePassEncoder struct {
	pipeline   *ComputePipeline
	bindGroups map[int]*BindGroup
}

func (c *ComputePassEncoder) SetPipeline(p gpu.ComputePipeline) {
	if pipeline, ok := p.(*ComputePipeline); ok {
		c.pipeline = pipeline
	}
}
func (c *ComputePassEncoder) SetBindGroup(slot int, bg gpu.BindGroup) {
	if group, ok := bg.(*BindGroup); ok {
		if c.bindGroups == nil {
			c.bindGroups = make(map[int]*BindGroup)
		}
		c.bindGroups[slot] = group
	}
}
func (c *ComputePassEncoder) DispatchWorkgroups(x, _, _ int) {
	if c.pipeline == nil {
		return
	}
	bg := c.bindGroups[0]
	if bg == nil {
		return
	}
	switch c.pipeline.desc.Label {
	case "bundle.cull":
		runCullPassThrough(bg)
	case "bundle.particles.update":
		// The particle WGSL uses workgroup_size(64). Limit the CPU update
		// to the same logical invocation count the recorded dispatch asked
		// for; oversize buffers keep their untouched tail deterministic.
		runParticleUpdate(bg, max(0, x)*64)
	}
}
func (c *ComputePassEncoder) End() {}

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

func clearTexture(t *Texture, c gpu.Color) {
	if t == nil {
		return
	}
	r := uint8(clamp01(c.R) * 255)
	g := uint8(clamp01(c.G) * 255)
	b := uint8(clamp01(c.B) * 255)
	a := uint8(clamp01(c.A) * 255)
	for i := 0; i+3 < len(t.rgba); i += 4 {
		t.rgba[i+0] = r
		t.rgba[i+1] = g
		t.rgba[i+2] = b
		t.rgba[i+3] = a
	}

	bpp := bytesPerPixel(t.format)
	if bpp == 0 || len(t.data) == 0 {
		return
	}
	switch t.format {
	case gpu.FormatRGBA8Unorm, gpu.FormatRGBA8UnormSRGB:
		for i := 0; i+3 < len(t.data); i += bpp {
			t.data[i+0] = r
			t.data[i+1] = g
			t.data[i+2] = b
			t.data[i+3] = a
		}
	case gpu.FormatBGRA8Unorm, gpu.FormatBGRA8UnormSRGB:
		for i := 0; i+3 < len(t.data); i += bpp {
			t.data[i+0] = b
			t.data[i+1] = g
			t.data[i+2] = r
			t.data[i+3] = a
		}
	case gpu.FormatR32Uint:
		v := uint32(math.Round(clampNonNegative(c.R)))
		for i := 0; i+3 < len(t.data); i += bpp {
			binary.LittleEndian.PutUint32(t.data[i:i+4], v)
		}
	case gpu.FormatRGBA16Float:
		half := [4]uint16{
			float32ToHalf(float32(clamp01(c.R))),
			float32ToHalf(float32(clamp01(c.G))),
			float32ToHalf(float32(clamp01(c.B))),
			float32ToHalf(float32(clamp01(c.A))),
		}
		for i := 0; i+7 < len(t.data); i += bpp {
			binary.LittleEndian.PutUint16(t.data[i+0:i+2], half[0])
			binary.LittleEndian.PutUint16(t.data[i+2:i+4], half[1])
			binary.LittleEndian.PutUint16(t.data[i+4:i+6], half[2])
			binary.LittleEndian.PutUint16(t.data[i+6:i+8], half[3])
		}
	case gpu.FormatRGBA32Float:
		vals := [4]float32{
			float32(clamp01(c.R)),
			float32(clamp01(c.G)),
			float32(clamp01(c.B)),
			float32(clamp01(c.A)),
		}
		for i := 0; i+15 < len(t.data); i += bpp {
			for j, v := range vals {
				binary.LittleEndian.PutUint32(t.data[i+j*4:i+j*4+4], math.Float32bits(v))
			}
		}
	}
}

func clearDepthTexture(t *Texture, depth float64) {
	clearDepthView(t, -1, depth)
}

func clearDepthView(t *Texture, layer int, depth float64) {
	if t == nil || !t.format.HasDepth() {
		return
	}
	v := float32(clamp01(depth))
	start, end := textureLayerRange(t, layer)
	for i := start; i < end && i < len(t.depth); i++ {
		t.depth[i] = v
	}
	bpp := bytesPerPixel(t.format)
	if bpp == 0 || len(t.data) == 0 {
		return
	}
	dataStart := start * bpp
	dataEnd := end * bpp
	if layer < 0 {
		dataStart = 0
		dataEnd = len(t.data)
	}
	switch t.format {
	case gpu.FormatDepth16Unorm:
		encoded := uint16(math.Round(float64(v * 0xffff)))
		for i := dataStart; i+1 < dataEnd && i+1 < len(t.data); i += bpp {
			binary.LittleEndian.PutUint16(t.data[i:i+2], encoded)
		}
	case gpu.FormatDepth24Plus, gpu.FormatDepth24PlusStencil8:
		encoded := uint32(math.Round(float64(v * 0x00ffffff)))
		for i := dataStart; i+3 < dataEnd && i+3 < len(t.data); i += bpp {
			binary.LittleEndian.PutUint32(t.data[i:i+4], encoded)
		}
	case gpu.FormatDepth32Float:
		encoded := math.Float32bits(v)
		for i := dataStart; i+3 < dataEnd && i+3 < len(t.data); i += bpp {
			binary.LittleEndian.PutUint32(t.data[i:i+4], encoded)
		}
	}
}

func copyTextureToFramebuffer(src *Texture, dst *image.RGBA) {
	if src == nil || dst == nil || len(src.rgba) == 0 {
		return
	}
	bounds := dst.Bounds()
	w := min(src.width, bounds.Dx())
	h := min(src.height, bounds.Dy())
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			srcOff := (y*src.width + x) * 4
			dst.SetRGBA(bounds.Min.X+x, bounds.Min.Y+y, color.RGBA{
				R: src.rgba[srcOff+0],
				G: src.rgba[srcOff+1],
				B: src.rgba[srcOff+2],
				A: src.rgba[srcOff+3],
			})
		}
	}
}

func textureLayer(t *Texture, layer int) int {
	if t == nil || t.layers <= 1 || layer < 0 {
		return 0
	}
	if layer >= t.layers {
		return t.layers - 1
	}
	return layer
}

func texturePixelIndex(t *Texture, layer, x, y int) int {
	if t == nil {
		return -1
	}
	return (textureLayer(t, layer)*t.height+y)*t.width + x
}

func textureLayerRange(t *Texture, layer int) (int, int) {
	if t == nil || t.width <= 0 || t.height <= 0 {
		return 0, 0
	}
	plane := t.width * t.height
	if layer < 0 {
		return 0, plane * max(1, t.layers)
	}
	start := textureLayer(t, layer) * plane
	return start, start + plane
}

type rasterTarget struct {
	img          *image.RGBA
	tex          *Texture
	texLayer     int
	id           *Texture
	idLayer      int
	pickID       uint32
	blend        *gpu.BlendState
	writeMask    gpu.ColorWriteMask
	depth        *Texture
	depthLayer   int
	depthCompare gpu.CompareFunc
	depthWrite   bool
	width        int
	height       int
}

func runCullPassThrough(bg *BindGroup) {
	var input, output, drawArgs *Buffer
	for _, entry := range bg.desc.Entries {
		switch entry.Binding {
		case 1:
			input, _ = entry.Buffer.(*Buffer)
		case 2:
			output, _ = entry.Buffer.(*Buffer)
		case 3:
			drawArgs, _ = entry.Buffer.(*Buffer)
		}
	}
	if input == nil || output == nil || drawArgs == nil || len(drawArgs.data) < 8 {
		return
	}
	instanceBytes := input.lastWriteSize
	if input.lastWriteOffset != 0 || instanceBytes < 0 {
		return
	}
	instanceBytes = min(instanceBytes, len(input.data), len(output.data))
	instanceBytes -= instanceBytes % 64
	instanceCount := instanceBytes / 64
	copy(output.data[:instanceBytes], input.data[:instanceBytes])
	output.lastWriteOffset = 0
	output.lastWriteSize = instanceBytes
	binary.LittleEndian.PutUint32(drawArgs.data[4:8], uint32(instanceCount))
}

func runParticleUpdate(bg *BindGroup, maxInvocations int) {
	if maxInvocations <= 0 {
		return
	}
	var uniforms, particles *Buffer
	for _, entry := range bg.desc.Entries {
		switch entry.Binding {
		case 0:
			uniforms, _ = entry.Buffer.(*Buffer)
		case 1:
			particles, _ = entry.Buffer.(*Buffer)
		}
	}
	if uniforms == nil || particles == nil || len(uniforms.data) < 64 {
		return
	}
	dt := readFloat32(uniforms.data, 0)
	tSeconds := readFloat32(uniforms.data, 4)
	lifetime := readFloat32(uniforms.data, 8)
	drag := readFloat32(uniforms.data, 12)
	emitter := [4]float32{
		readFloat32(uniforms.data, 16),
		readFloat32(uniforms.data, 20),
		readFloat32(uniforms.data, 24),
		readFloat32(uniforms.data, 28),
	}
	gravity := [3]float32{
		readFloat32(uniforms.data, 32),
		readFloat32(uniforms.data, 36),
		readFloat32(uniforms.data, 40),
	}
	initialSpeed := readFloat32(uniforms.data, 48)
	count := min(len(particles.data)/32, maxInvocations)
	for i := 0; i < count; i++ {
		off := i * 32
		pos := [4]float32{
			readFloat32(particles.data, off+0),
			readFloat32(particles.data, off+4),
			readFloat32(particles.data, off+8),
			readFloat32(particles.data, off+12),
		}
		vel := [4]float32{
			readFloat32(particles.data, off+16),
			readFloat32(particles.data, off+20),
			readFloat32(particles.data, off+24),
			readFloat32(particles.data, off+28),
		}
		newAge := pos[3] + dt
		if newAge >= vel[3] || vel[3] <= 0 {
			pos, vel = respawnParticle(i, tSeconds, emitter, lifetime, initialSpeed)
		} else {
			dragFactor := clamp01f(1 - drag*dt)
			vel[0] = vel[0]*dragFactor + gravity[0]*dt
			vel[1] = vel[1]*dragFactor + gravity[1]*dt
			vel[2] = vel[2]*dragFactor + gravity[2]*dt
			pos[0] += vel[0] * dt
			pos[1] += vel[1] * dt
			pos[2] += vel[2] * dt
			pos[3] = newAge
		}
		writeFloat32(particles.data, off+0, pos[0])
		writeFloat32(particles.data, off+4, pos[1])
		writeFloat32(particles.data, off+8, pos[2])
		writeFloat32(particles.data, off+12, pos[3])
		writeFloat32(particles.data, off+16, vel[0])
		writeFloat32(particles.data, off+20, vel[1])
		writeFloat32(particles.data, off+24, vel[2])
		writeFloat32(particles.data, off+28, vel[3])
	}
	particles.lastWriteOffset = 0
	particles.lastWriteSize = count * 32
}

func respawnParticle(i int, tSeconds float32, emitter [4]float32, lifetime, initialSpeed float32) ([4]float32, [4]float32) {
	seed := [3]float32{float32(i), tSeconds, tSeconds * 1.37}
	rx := hash13(seed)*2.0 - 1.0
	ry := hash13(add3(seed, [3]float32{1.7, 2.3, 3.1}))
	rz := hash13(add3(seed, [3]float32{4.1, 5.3, 6.7}))*2.0 - 1.0
	dir := normalize3([3]float32{rx, ry*0.4 + 0.3, rz})
	offsetY := hash13(add3(seed, [3]float32{9.1, 3.3, 7.7}))*2.0 - 1.0
	radius := emitter[3]
	pos := [4]float32{
		emitter[0] + rx*radius,
		emitter[1] + offsetY*radius,
		emitter[2] + rz*radius,
		0,
	}
	vel := [4]float32{
		dir[0] * initialSpeed,
		dir[1] * initialSpeed,
		dir[2] * initialSpeed,
		lifetime,
	}
	return pos, vel
}

func (r *RenderPassEncoder) rasterizeDraw(vertexCount, instanceCount, firstVertex, firstInstance int) {
	if r.pipeline == nil || vertexCount < 3 || instanceCount <= 0 {
		return
	}
	label := r.pipeline.desc.Label
	if label == "bundle.particles.render" {
		r.rasterizeParticles(instanceCount, firstInstance)
		return
	}
	if label != "bundle.unlit" && label != "bundle.lit" && label != "bundle.shadow" {
		return
	}
	posBuf := r.vertexBuffers[0]
	if posBuf == nil {
		return
	}
	target, ok := r.colorRasterTarget()
	if label == "bundle.shadow" {
		target, ok = r.depthRasterTarget()
	}
	if !ok {
		return
	}
	mvp := r.activeMVP()
	colorBuf := r.vertexBuffers[1]
	instanceBuf := r.vertexBuffers[4]
	normalBuf := r.vertexBuffers[2]
	uvBuf := r.vertexBuffers[3]
	if label == "bundle.unlit" {
		instanceCount = 1
		firstInstance = 0
		instanceBuf = nil
	} else if label == "bundle.shadow" {
		instanceBuf = r.vertexBuffers[1]
		colorBuf = nil
		normalBuf = nil
		uvBuf = nil
	}
	material := r.activeMaterial()
	lighting := r.activeLighting()
	for inst := firstInstance; inst < firstInstance+instanceCount; inst++ {
		model, ok := readMat4(instanceBuf, inst)
		if !ok {
			model = identityMat4()
		}
		for base := 0; base+2 < vertexCount; base += 3 {
			var pts [3][2]float32
			var depths [3]float32
			var cols [3][3]float32
			valid := true
			for i := 0; i < 3; i++ {
				vertex := firstVertex + base + i
				pos, ok := readVec3(posBuf, vertex)
				if !ok {
					valid = false
					break
				}
				worldPos := pos
				if instanceBuf != nil {
					worldPos = transformPoint(model, pos)
				}
				x, y, depth, ok := transformToScreen(mvp, worldPos, target.width, target.height)
				if !ok {
					valid = false
					break
				}
				pts[i] = [2]float32{x, y}
				depths[i] = depth
				if label == "bundle.lit" {
					baseColor := material.resolve(readColor(colorBuf, vertex), readUV(uvBuf, vertex))
					normal := readNormal(normalBuf, vertex)
					if instanceBuf != nil {
						normal = transformDirection(model, normal)
					}
					cols[i] = lighting.shade(baseColor, normal, worldPos)
				} else {
					cols[i] = readColor(colorBuf, vertex)
				}
			}
			if valid {
				if !triangleOutsideClip(depths) {
					rasterizeTriangle(target, pts, depths, cols)
				}
			}
		}
	}
}

func (r *RenderPassEncoder) rasterizeParticles(instanceCount, firstInstance int) {
	if r.pipeline == nil || instanceCount <= 0 {
		return
	}
	target, ok := r.colorRasterTarget()
	if !ok {
		return
	}
	bg := r.bindGroups[0]
	if bg == nil {
		bg = r.bindGroup
	}
	if bg == nil {
		return
	}
	scene, particles := particleBindings(bg)
	if scene == nil || particles == nil || len(scene.data) < 128 {
		return
	}
	viewProj := readMat4At(scene, 0)
	colorStart := [4]float32{
		readFloat32(scene.data, 80),
		readFloat32(scene.data, 84),
		readFloat32(scene.data, 88),
		readFloat32(scene.data, 92),
	}
	colorEnd := [4]float32{
		readFloat32(scene.data, 96),
		readFloat32(scene.data, 100),
		readFloat32(scene.data, 104),
		readFloat32(scene.data, 108),
	}
	sizeStart := readFloat32(scene.data, 112)
	sizeEnd := readFloat32(scene.data, 116)
	if firstInstance < 0 || firstInstance >= len(particles.data)/32 {
		return
	}
	count := min(instanceCount, len(particles.data)/32-firstInstance)
	if count <= 0 {
		return
	}
	for inst := firstInstance; inst < firstInstance+count; inst++ {
		off := inst * 32
		pos := [3]float32{
			readFloat32(particles.data, off+0),
			readFloat32(particles.data, off+4),
			readFloat32(particles.data, off+8),
		}
		age := readFloat32(particles.data, off+12)
		lifetime := readFloat32(particles.data, off+28)
		if lifetime <= 0 {
			continue
		}
		t := clamp01f(age / lifetime)
		alpha := mix(colorStart[3], colorEnd[3], t) *
			smoothstep(0, 0.15, t) *
			(1 - smoothstep(0.85, 1, t))
		if alpha <= 0 {
			continue
		}
		x, y, depth, ok := transformToScreen(viewProj, pos, target.width, target.height)
		if !ok || pointOutsideClip(depth) || !depthPasses(target, int(x+0.5), int(y+0.5), depth) {
			continue
		}
		size := mix(sizeStart, sizeEnd, t)
		radius := max(1, int(math.Ceil(float64(size)*float64(min(target.width, target.height))*0.06)))
		col := color.RGBA{
			R: clampByte(mix(colorStart[0], colorEnd[0], t) * alpha),
			G: clampByte(mix(colorStart[1], colorEnd[1], t) * alpha),
			B: clampByte(mix(colorStart[2], colorEnd[2], t) * alpha),
			A: clampByte(alpha),
		}
		cx := int(math.Round(float64(x)))
		cy := int(math.Round(float64(y)))
		for py := cy - radius; py <= cy+radius; py++ {
			for px := cx - radius; px <= cx+radius; px++ {
				if px < 0 || px >= target.width || py < 0 || py >= target.height {
					continue
				}
				dx := float64(px - cx)
				dy := float64(py - cy)
				if dx*dx+dy*dy > float64(radius*radius) {
					continue
				}
				if depthPasses(target, px, py, depth) {
					writeRasterColor(target, px, py, col)
				}
			}
		}
	}
}

func particleBindings(bg *BindGroup) (*Buffer, *Buffer) {
	var scene, particles *Buffer
	for _, entry := range bg.desc.Entries {
		switch entry.Binding {
		case 0:
			scene, _ = entry.Buffer.(*Buffer)
		case 1:
			particles, _ = entry.Buffer.(*Buffer)
		}
	}
	return scene, particles
}

func (r *RenderPassEncoder) colorRasterTarget() (rasterTarget, bool) {
	for _, att := range r.desc.ColorAttachments {
		if att.StoreOp == gpu.StoreOpDiscard {
			continue
		}
		switch view := att.View.(type) {
		case *SurfaceView:
			if view == nil || view.device == nil || view.device.framebuffer == nil {
				return rasterTarget{}, false
			}
			bounds := view.device.framebuffer.Bounds()
			target := rasterTarget{
				img:       view.device.framebuffer,
				texLayer:  -1,
				idLayer:   -1,
				blend:     firstColorBlend(r.pipeline),
				writeMask: firstColorWriteMask(r.pipeline),
				width:     bounds.Dx(),
				height:    bounds.Dy(),
			}
			r.attachDepth(&target)
			r.attachID(&target)
			return target, true
		case *TextureView:
			if view == nil || view.owner == nil || view.owner.format.HasDepth() {
				return rasterTarget{}, false
			}
			target := rasterTarget{
				tex:       view.owner,
				texLayer:  view.layer,
				idLayer:   -1,
				blend:     firstColorBlend(r.pipeline),
				writeMask: firstColorWriteMask(r.pipeline),
				width:     view.owner.width,
				height:    view.owner.height,
			}
			r.attachDepth(&target)
			r.attachID(&target)
			return target, true
		}
	}
	return rasterTarget{}, false
}

func (r *RenderPassEncoder) depthRasterTarget() (rasterTarget, bool) {
	if r.desc.DepthStencilAttachment == nil || r.pipeline == nil || r.pipeline.desc.DepthStencil == nil {
		return rasterTarget{}, false
	}
	view, ok := r.desc.DepthStencilAttachment.View.(*TextureView)
	if !ok || view == nil || view.owner == nil || !view.owner.format.HasDepth() {
		return rasterTarget{}, false
	}
	target := rasterTarget{
		depth:        view.owner,
		depthLayer:   view.layer,
		depthCompare: r.pipeline.desc.DepthStencil.DepthCompare,
		depthWrite:   r.pipeline.desc.DepthStencil.DepthWriteEnabled,
		texLayer:     -1,
		idLayer:      -1,
		width:        view.owner.width,
		height:       view.owner.height,
	}
	return target, true
}

func (r *RenderPassEncoder) attachDepth(target *rasterTarget) {
	if target == nil || r.pipeline == nil || r.pipeline.desc.DepthStencil == nil ||
		r.desc.DepthStencilAttachment == nil {
		return
	}
	view, ok := r.desc.DepthStencilAttachment.View.(*TextureView)
	if !ok || view == nil || view.owner == nil || !view.owner.format.HasDepth() {
		return
	}
	target.depth = view.owner
	target.depthLayer = view.layer
	target.depthCompare = r.pipeline.desc.DepthStencil.DepthCompare
	target.depthWrite = r.pipeline.desc.DepthStencil.DepthWriteEnabled
}

func (r *RenderPassEncoder) attachID(target *rasterTarget) {
	if target == nil || len(r.desc.ColorAttachments) < 2 {
		return
	}
	att := r.desc.ColorAttachments[1]
	if att.StoreOp == gpu.StoreOpDiscard {
		return
	}
	view, ok := att.View.(*TextureView)
	if !ok || view == nil || view.owner == nil || view.owner.format != gpu.FormatR32Uint {
		return
	}
	target.id = view.owner
	target.idLayer = view.layer
}

func firstColorBlend(p *RenderPipeline) *gpu.BlendState {
	if p == nil || len(p.desc.Fragment.Targets) == 0 {
		return nil
	}
	return p.desc.Fragment.Targets[0].Blend
}

func firstColorWriteMask(p *RenderPipeline) gpu.ColorWriteMask {
	if p == nil || len(p.desc.Fragment.Targets) == 0 {
		return gpu.ColorWriteAll
	}
	mask := p.desc.Fragment.Targets[0].WriteMask
	if mask == 0 {
		return gpu.ColorWriteAll
	}
	return mask
}

func (r *RenderPassEncoder) activeMVP() [16]float32 {
	m := identityMat4()
	bg := r.bindGroups[0]
	if bg == nil {
		bg = r.bindGroup
	}
	if bg == nil {
		return m
	}
	for _, entry := range bg.desc.Entries {
		if entry.Binding != 0 {
			continue
		}
		buf, ok := entry.Buffer.(*Buffer)
		if !ok || buf == nil {
			continue
		}
		offset := entry.Offset
		if offset < 0 || offset+64 > len(buf.data) {
			continue
		}
		for i := range m {
			m[i] = readFloat32(buf.data, offset+i*4)
		}
		return m
	}
	return m
}

func identityMat4() [16]float32 {
	var m [16]float32
	m[0], m[5], m[10], m[15] = 1, 1, 1, 1
	return m
}

func transformPoint(m [16]float32, p [3]float32) [3]float32 {
	x, y, z := p[0], p[1], p[2]
	w := m[3]*x + m[7]*y + m[11]*z + m[15]
	if w == 0 {
		w = 1
	}
	return [3]float32{
		(m[0]*x + m[4]*y + m[8]*z + m[12]) / w,
		(m[1]*x + m[5]*y + m[9]*z + m[13]) / w,
		(m[2]*x + m[6]*y + m[10]*z + m[14]) / w,
	}
}

func transformDirection(m [16]float32, p [3]float32) [3]float32 {
	x, y, z := p[0], p[1], p[2]
	return normalize3([3]float32{
		m[0]*x + m[4]*y + m[8]*z,
		m[1]*x + m[5]*y + m[9]*z,
		m[2]*x + m[6]*y + m[10]*z,
	})
}

func transformToNDC(m [16]float32, p [3]float32) ([3]float32, bool) {
	x, y, z := p[0], p[1], p[2]
	clipX := m[0]*x + m[4]*y + m[8]*z + m[12]
	clipY := m[1]*x + m[5]*y + m[9]*z + m[13]
	clipZ := m[2]*x + m[6]*y + m[10]*z + m[14]
	clipW := m[3]*x + m[7]*y + m[11]*z + m[15]
	if clipW == 0 || math.IsNaN(float64(clipW)) || math.IsInf(float64(clipW), 0) {
		return [3]float32{}, false
	}
	ndc := [3]float32{clipX / clipW, clipY / clipW, clipZ / clipW}
	if math.IsNaN(float64(ndc[0])) || math.IsNaN(float64(ndc[1])) ||
		math.IsNaN(float64(ndc[2])) || math.IsInf(float64(ndc[0]), 0) ||
		math.IsInf(float64(ndc[1]), 0) || math.IsInf(float64(ndc[2]), 0) {
		return [3]float32{}, false
	}
	return ndc, true
}

func transformToScreen(m [16]float32, p [3]float32, width, height int) (float32, float32, float32, bool) {
	if width <= 0 || height <= 0 {
		return 0, 0, 0, false
	}
	x, y, z := p[0], p[1], p[2]
	clipX := m[0]*x + m[4]*y + m[8]*z + m[12]
	clipY := m[1]*x + m[5]*y + m[9]*z + m[13]
	clipZ := m[2]*x + m[6]*y + m[10]*z + m[14]
	clipW := m[3]*x + m[7]*y + m[11]*z + m[15]
	if clipW <= 0 || math.IsNaN(float64(clipW)) || math.IsInf(float64(clipW), 0) {
		return 0, 0, 0, false
	}
	ndcX := clipX / clipW
	ndcY := clipY / clipW
	ndcZ := clipZ / clipW
	if math.IsNaN(float64(ndcX)) || math.IsNaN(float64(ndcY)) ||
		math.IsNaN(float64(ndcZ)) || math.IsInf(float64(ndcX), 0) ||
		math.IsInf(float64(ndcY), 0) || math.IsInf(float64(ndcZ), 0) {
		return 0, 0, 0, false
	}
	sx := (ndcX*0.5 + 0.5) * float32(width-1)
	sy := (1 - (ndcY*0.5 + 0.5)) * float32(height-1)
	return sx, sy, ndcZ, true
}

func readVec3(buf *Buffer, vertex int) ([3]float32, bool) {
	var out [3]float32
	if buf == nil || vertex < 0 {
		return out, false
	}
	offset := vertex * 12
	if offset < 0 || offset+12 > len(buf.data) {
		return out, false
	}
	out[0] = readFloat32(buf.data, offset)
	out[1] = readFloat32(buf.data, offset+4)
	out[2] = readFloat32(buf.data, offset+8)
	return out, true
}

func readColor(buf *Buffer, vertex int) [3]float32 {
	if col, ok := readVec3(buf, vertex); ok {
		return col
	}
	return [3]float32{1, 1, 1}
}

func readNormal(buf *Buffer, vertex int) [3]float32 {
	if n, ok := readVec3(buf, vertex); ok {
		return normalize3(n)
	}
	return [3]float32{0, 0, 1}
}

func readUV(buf *Buffer, vertex int) [2]float32 {
	if buf == nil || vertex < 0 {
		return [2]float32{}
	}
	offset := vertex * 8
	if offset < 0 || offset+8 > len(buf.data) {
		return [2]float32{}
	}
	return [2]float32{
		readFloat32(buf.data, offset),
		readFloat32(buf.data, offset+4),
	}
}

type materialState struct {
	baseColor        [3]float32
	emissive         [3]float32
	emissiveScale    float32
	useVertexColor   bool
	baseColorTexture *Texture
	baseColorLayer   int
}

func defaultMaterialState() materialState {
	return materialState{
		baseColor:      [3]float32{1, 1, 1},
		useVertexColor: true,
		baseColorLayer: -1,
	}
}

func (r *RenderPassEncoder) activeMaterial() materialState {
	state := defaultMaterialState()
	bg := r.bindGroups[1]
	if bg == nil {
		return state
	}
	for _, entry := range bg.desc.Entries {
		switch entry.Binding {
		case 0:
			buf, ok := entry.Buffer.(*Buffer)
			if !ok || buf == nil {
				continue
			}
			offset := entry.Offset
			if offset < 0 || offset+48 > len(buf.data) {
				continue
			}
			state.baseColor = [3]float32{
				readFloat32(buf.data, offset+0),
				readFloat32(buf.data, offset+4),
				readFloat32(buf.data, offset+8),
			}
			state.emissiveScale = readFloat32(buf.data, offset+24)
			state.useVertexColor = readFloat32(buf.data, offset+28) >= 0.5
			state.emissive = [3]float32{
				readFloat32(buf.data, offset+32),
				readFloat32(buf.data, offset+36),
				readFloat32(buf.data, offset+40),
			}
		case 1:
			view, ok := entry.TextureView.(*TextureView)
			if !ok || view == nil {
				continue
			}
			state.baseColorTexture = view.owner
			state.baseColorLayer = view.layer
		}
	}
	return state
}

func (m materialState) resolve(vertex [3]float32, uv [2]float32) [3]float32 {
	base := [3]float32{
		clamp01f(m.baseColor[0] + m.emissive[0]*m.emissiveScale),
		clamp01f(m.baseColor[1] + m.emissive[1]*m.emissiveScale),
		clamp01f(m.baseColor[2] + m.emissive[2]*m.emissiveScale),
	}
	if m.useVertexColor {
		base = vertex
	}
	sample := sampleTextureRGB(m.baseColorTexture, m.baseColorLayer, uv)
	return [3]float32{
		clamp01f(base[0] * sample[0]),
		clamp01f(base[1] * sample[1]),
		clamp01f(base[2] * sample[2]),
	}
}

type sceneLighting struct {
	lightViewProjs [3][16]float32
	cameraPos      [3]float32
	lightDir       [3]float32
	lightColor     [4]float32
	ambientColor   [4]float32
	skyColor       [4]float32
	groundColor    [4]float32
	cascadeSplits  [3]float32
	shadow         *Texture
	shadowLayer    int
}

func defaultSceneLighting() sceneLighting {
	state := sceneLighting{
		lightDir:     [3]float32{-0.4, -1.0, -0.3},
		lightColor:   [4]float32{1, 0.96, 0.9, 1},
		ambientColor: [4]float32{0.35, 0.38, 0.45, 0.35},
		skyColor:     [4]float32{0.8, 0.88, 1, 1},
		groundColor:  [4]float32{0.28, 0.24, 0.22, 1},
		shadowLayer:  -1,
	}
	for i := range state.lightViewProjs {
		state.lightViewProjs[i] = identityMat4()
	}
	return state
}

func (r *RenderPassEncoder) activeLighting() sceneLighting {
	state := defaultSceneLighting()
	bg := r.bindGroups[0]
	if bg == nil {
		bg = r.bindGroup
	}
	if bg == nil {
		return state
	}
	for _, entry := range bg.desc.Entries {
		switch entry.Binding {
		case 0:
			buf, ok := entry.Buffer.(*Buffer)
			if !ok || buf == nil {
				continue
			}
			offset := entry.Offset
			if offset < 0 || offset+368 > len(buf.data) {
				continue
			}
			for i := range state.lightViewProjs {
				state.lightViewProjs[i] = readMat4At(buf, offset+64+i*64)
			}
			state.cameraPos = readVec3At(buf.data, offset+256)
			state.lightDir = readVec3At(buf.data, offset+272)
			state.lightColor = readVec4At(buf.data, offset+288)
			state.ambientColor = readVec4At(buf.data, offset+304)
			state.skyColor = readVec4At(buf.data, offset+320)
			state.groundColor = readVec4At(buf.data, offset+336)
			splits := readVec4At(buf.data, offset+352)
			state.cascadeSplits = [3]float32{splits[0], splits[1], splits[2]}
		case 1:
			view, ok := entry.TextureView.(*TextureView)
			if !ok || view == nil {
				continue
			}
			state.shadow = view.owner
			state.shadowLayer = view.layer
		}
	}
	return state
}

func (l sceneLighting) shade(base, normal, worldPos [3]float32) [3]float32 {
	n := normalize3(normal)
	light := normalize3([3]float32{-l.lightDir[0], -l.lightDir[1], -l.lightDir[2]})
	ndotl := max(0, float32(n[0]*light[0]+n[1]*light[1]+n[2]*light[2]))
	hemiT := clamp01f(n[1]*0.5 + 0.5)
	hemi := [3]float32{
		mix(l.groundColor[0], l.skyColor[0], hemiT),
		mix(l.groundColor[1], l.skyColor[1], hemiT),
		mix(l.groundColor[2], l.skyColor[2], hemiT),
	}
	shadow := l.sampleShadow(worldPos)
	return [3]float32{
		clamp01f(base[0] * (l.ambientColor[0]*l.ambientColor[3]*hemi[0] + l.lightColor[0]*l.lightColor[3]*ndotl*shadow)),
		clamp01f(base[1] * (l.ambientColor[1]*l.ambientColor[3]*hemi[1] + l.lightColor[1]*l.lightColor[3]*ndotl*shadow)),
		clamp01f(base[2] * (l.ambientColor[2]*l.ambientColor[3]*hemi[2] + l.lightColor[2]*l.lightColor[3]*ndotl*shadow)),
	}
}

func (l sceneLighting) sampleShadow(worldPos [3]float32) float32 {
	if l.shadow == nil || l.shadow.width <= 0 || l.shadow.height <= 0 {
		return 1
	}
	cascade := l.pickCascade(worldPos)
	proj, ok := transformToNDC(l.lightViewProjs[cascade], worldPos)
	if !ok || pointOutsideClip(proj[2]) {
		return 1
	}
	u := proj[0]*0.5 + 0.5
	v := 0.5 - proj[1]*0.5
	if u < 0 || u > 1 || v < 0 || v > 1 {
		return 1
	}
	layer := cascade
	if l.shadowLayer >= 0 {
		layer = l.shadowLayer
	}
	bias := float32(0.003 + 0.003*float32(cascade))
	return sampleShadowCompare(l.shadow, layer, u, v, proj[2]-bias)
}

func (l sceneLighting) pickCascade(worldPos [3]float32) int {
	viewZ := length3([3]float32{
		worldPos[0] - l.cameraPos[0],
		worldPos[1] - l.cameraPos[1],
		worldPos[2] - l.cameraPos[2],
	})
	if l.cascadeSplits[0] > 0 && viewZ < l.cascadeSplits[0] {
		return 0
	}
	if l.cascadeSplits[1] > 0 && viewZ < l.cascadeSplits[1] {
		return 1
	}
	if l.cascadeSplits[2] > 0 {
		return 2
	}
	return 0
}

func readMat4(buf *Buffer, index int) ([16]float32, bool) {
	var out [16]float32
	if buf == nil || index < 0 {
		return out, false
	}
	offset := index * 64
	if offset < 0 || offset+64 > len(buf.data) {
		return out, false
	}
	for i := range out {
		out[i] = readFloat32(buf.data, offset+i*4)
	}
	return out, true
}

func readMat4At(buf *Buffer, offset int) [16]float32 {
	m := identityMat4()
	if buf == nil || offset < 0 || offset+64 > len(buf.data) {
		return m
	}
	for i := range m {
		m[i] = readFloat32(buf.data, offset+i*4)
	}
	return m
}

func readVec3At(data []byte, offset int) [3]float32 {
	if offset < 0 || offset+12 > len(data) {
		return [3]float32{}
	}
	return [3]float32{
		readFloat32(data, offset+0),
		readFloat32(data, offset+4),
		readFloat32(data, offset+8),
	}
}

func readVec4At(data []byte, offset int) [4]float32 {
	if offset < 0 || offset+16 > len(data) {
		return [4]float32{}
	}
	return [4]float32{
		readFloat32(data, offset+0),
		readFloat32(data, offset+4),
		readFloat32(data, offset+8),
		readFloat32(data, offset+12),
	}
}

func readFloat32(data []byte, offset int) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(data[offset : offset+4]))
}

func writeFloat32(data []byte, offset int, v float32) {
	if offset < 0 || offset+4 > len(data) {
		return
	}
	binary.LittleEndian.PutUint32(data[offset:offset+4], math.Float32bits(v))
}

func rasterizeTriangle(target rasterTarget, pts [3][2]float32, depths [3]float32, cols [3][3]float32) {
	area := edge(pts[0], pts[1], pts[2])
	if math.Abs(float64(area)) < 1e-6 {
		return
	}
	minX := max(0, int(math.Floor(float64(min3(pts[0][0], pts[1][0], pts[2][0])))))
	minY := max(0, int(math.Floor(float64(min3(pts[0][1], pts[1][1], pts[2][1])))))
	maxX := min(target.width-1, int(math.Ceil(float64(max3(pts[0][0], pts[1][0], pts[2][0])))))
	maxY := min(target.height-1, int(math.Ceil(float64(max3(pts[0][1], pts[1][1], pts[2][1])))))
	if maxX < minX || maxY < minY {
		return
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			p := [2]float32{float32(x) + 0.5, float32(y) + 0.5}
			w0 := edge(pts[1], pts[2], p)
			w1 := edge(pts[2], pts[0], p)
			w2 := edge(pts[0], pts[1], p)
			if !sameSign(w0, area) || !sameSign(w1, area) || !sameSign(w2, area) {
				continue
			}
			w0 /= area
			w1 /= area
			w2 /= area
			depth := depths[0]*w0 + depths[1]*w1 + depths[2]*w2
			if !depthPasses(target, x, y, depth) {
				continue
			}
			writeRasterColor(target, x, y, color.RGBA{
				R: clampByte(cols[0][0]*w0 + cols[1][0]*w1 + cols[2][0]*w2),
				G: clampByte(cols[0][1]*w0 + cols[1][1]*w1 + cols[2][1]*w2),
				B: clampByte(cols[0][2]*w0 + cols[1][2]*w1 + cols[2][2]*w2),
				A: 255,
			})
			if target.id != nil {
				writeTextureUint32(target.id, target.idLayer, x, y, target.pickID)
			}
			if target.depthWrite {
				writeDepth(target.depth, target.depthLayer, x, y, depth)
			}
		}
	}
}

func depthPasses(target rasterTarget, x, y int, depth float32) bool {
	if target.depth == nil {
		return true
	}
	if pointOutsideClip(depth) {
		return false
	}
	stored := readDepth(target.depth, target.depthLayer, x, y)
	switch target.depthCompare {
	case gpu.CompareAlways:
		return true
	case gpu.CompareNever:
		return false
	case gpu.CompareLess:
		return depth < stored
	case gpu.CompareLessEqual:
		return depth <= stored
	case gpu.CompareEqual:
		return math.Abs(float64(depth-stored)) <= 1e-6
	case gpu.CompareGreater:
		return depth > stored
	case gpu.CompareGreaterEqual:
		return depth >= stored
	case gpu.CompareNotEqual:
		return math.Abs(float64(depth-stored)) > 1e-6
	default:
		return true
	}
}

func readDepth(t *Texture, layer, x, y int) float32 {
	if t == nil || x < 0 || x >= t.width || y < 0 || y >= t.height {
		return 1
	}
	idx := texturePixelIndex(t, layer, x, y)
	if idx >= 0 && idx < len(t.depth) {
		return t.depth[idx]
	}
	bpp := bytesPerPixel(t.format)
	off := idx * bpp
	if bpp == 0 || off < 0 || off+bpp > len(t.data) {
		return 1
	}
	switch t.format {
	case gpu.FormatDepth16Unorm:
		return float32(binary.LittleEndian.Uint16(t.data[off:off+2])) / 0xffff
	case gpu.FormatDepth24Plus, gpu.FormatDepth24PlusStencil8:
		return float32(binary.LittleEndian.Uint32(t.data[off:off+4])&0x00ffffff) / 0x00ffffff
	case gpu.FormatDepth32Float:
		return math.Float32frombits(binary.LittleEndian.Uint32(t.data[off : off+4]))
	default:
		return 1
	}
}

func sampleShadowCompare(t *Texture, layer int, u, v, depthRef float32) float32 {
	if t == nil || t.width <= 0 || t.height <= 0 {
		return 1
	}
	x := u*float32(t.width) - 0.5
	y := v*float32(t.height) - 0.5
	x0 := int(math.Floor(float64(x)))
	y0 := int(math.Floor(float64(y)))
	tx := x - float32(x0)
	ty := y - float32(y0)
	c00 := shadowComparePixel(t, layer, x0, y0, depthRef)
	c10 := shadowComparePixel(t, layer, x0+1, y0, depthRef)
	c01 := shadowComparePixel(t, layer, x0, y0+1, depthRef)
	c11 := shadowComparePixel(t, layer, x0+1, y0+1, depthRef)
	return mix(mix(c00, c10, tx), mix(c01, c11, tx), ty)
}

func shadowComparePixel(t *Texture, layer, x, y int, depthRef float32) float32 {
	x = clampTextureIndex(x, t.width)
	y = clampTextureIndex(y, t.height)
	if depthRef <= readDepth(t, layer, x, y)+1e-6 {
		return 1
	}
	return 0
}

func clampTextureIndex(i, size int) int {
	if size <= 1 {
		return 0
	}
	if i < 0 {
		return 0
	}
	if i >= size {
		return size - 1
	}
	return i
}

func writeDepth(t *Texture, layer, x, y int, depth float32) {
	if t == nil || x < 0 || x >= t.width || y < 0 || y >= t.height {
		return
	}
	v := clamp01f(depth)
	idx := texturePixelIndex(t, layer, x, y)
	if idx >= 0 && idx < len(t.depth) {
		t.depth[idx] = v
	}
	bpp := bytesPerPixel(t.format)
	off := idx * bpp
	if bpp == 0 || off < 0 || off+bpp > len(t.data) {
		return
	}
	switch t.format {
	case gpu.FormatDepth16Unorm:
		binary.LittleEndian.PutUint16(t.data[off:off+2], uint16(math.Round(float64(v*0xffff))))
	case gpu.FormatDepth24Plus, gpu.FormatDepth24PlusStencil8:
		binary.LittleEndian.PutUint32(t.data[off:off+4], uint32(math.Round(float64(v*0x00ffffff))))
	case gpu.FormatDepth32Float:
		binary.LittleEndian.PutUint32(t.data[off:off+4], math.Float32bits(v))
	}
}

func writeRasterColor(target rasterTarget, x, y int, col color.RGBA) {
	if target.blend != nil {
		col = blendRasterColor(readRasterColor(target, x, y), col, *target.blend)
	}
	if target.writeMask == 0 {
		target.writeMask = gpu.ColorWriteAll
	}
	if target.writeMask != gpu.ColorWriteAll {
		col = applyColorWriteMask(readRasterColor(target, x, y), col, target.writeMask)
	}
	if target.img != nil {
		bounds := target.img.Bounds()
		target.img.SetRGBA(bounds.Min.X+x, bounds.Min.Y+y, col)
		return
	}
	writeTextureRGBA(target.tex, target.texLayer, x, y, col)
}

func blendRasterColor(dst, src color.RGBA, blend gpu.BlendState) color.RGBA {
	sr, sg, sb, sa := float32(src.R)/255, float32(src.G)/255, float32(src.B)/255, float32(src.A)/255
	dr, dg, db, da := float32(dst.R)/255, float32(dst.G)/255, float32(dst.B)/255, float32(dst.A)/255
	r := blendComponent(sr, dr, sa, da, blend.Color)
	g := blendComponent(sg, dg, sa, da, blend.Color)
	b := blendComponent(sb, db, sa, da, blend.Color)
	a := blendComponent(sa, da, sa, da, blend.Alpha)
	return color.RGBA{
		R: clampByte(r),
		G: clampByte(g),
		B: clampByte(b),
		A: clampByte(a),
	}
}

func blendComponent(src, dst, srcA, dstA float32, c gpu.BlendComponent) float32 {
	s := src * blendFactor(c.SrcFactor, srcA, dstA)
	d := dst * blendFactor(c.DstFactor, srcA, dstA)
	switch c.Operation {
	case gpu.BlendOpSubtract:
		return s - d
	case gpu.BlendOpReverseSubtract:
		return d - s
	case gpu.BlendOpMin:
		return float32(math.Min(float64(s), float64(d)))
	case gpu.BlendOpMax:
		return float32(math.Max(float64(s), float64(d)))
	default:
		return s + d
	}
}

func blendFactor(f gpu.BlendFactor, srcA, dstA float32) float32 {
	switch f {
	case gpu.BlendZero:
		return 0
	case gpu.BlendSrcAlpha:
		return srcA
	case gpu.BlendOneMinusSrcAlpha:
		return 1 - srcA
	case gpu.BlendDstAlpha:
		return dstA
	case gpu.BlendOneMinusDstAlpha:
		return 1 - dstA
	default:
		return 1
	}
}

func applyColorWriteMask(dst, src color.RGBA, mask gpu.ColorWriteMask) color.RGBA {
	if mask&gpu.ColorWriteRed == 0 {
		src.R = dst.R
	}
	if mask&gpu.ColorWriteGreen == 0 {
		src.G = dst.G
	}
	if mask&gpu.ColorWriteBlue == 0 {
		src.B = dst.B
	}
	if mask&gpu.ColorWriteAlpha == 0 {
		src.A = dst.A
	}
	return src
}

func addRasterColor(target rasterTarget, x, y int, col color.RGBA) {
	base := readRasterColor(target, x, y)
	writeRasterColor(target, x, y, color.RGBA{
		R: saturatingAdd(base.R, col.R),
		G: saturatingAdd(base.G, col.G),
		B: saturatingAdd(base.B, col.B),
		A: saturatingAdd(base.A, col.A),
	})
}

func readRasterColor(target rasterTarget, x, y int) color.RGBA {
	if target.img != nil {
		bounds := target.img.Bounds()
		return target.img.RGBAAt(bounds.Min.X+x, bounds.Min.Y+y)
	}
	return readTextureRGBA(target.tex, target.texLayer, x, y)
}

func sampleTextureRGB(t *Texture, layer int, uv [2]float32) [3]float32 {
	if t == nil || t.width <= 0 || t.height <= 0 {
		return [3]float32{1, 1, 1}
	}
	u := fract32(uv[0])
	v := fract32(uv[1])
	x := u*float32(t.width) - 0.5
	y := v*float32(t.height) - 0.5
	x0 := int(math.Floor(float64(x)))
	y0 := int(math.Floor(float64(y)))
	tx := x - float32(x0)
	ty := y - float32(y0)
	c00 := sampleTexturePixelRGB(t, layer, x0, y0)
	c10 := sampleTexturePixelRGB(t, layer, x0+1, y0)
	c01 := sampleTexturePixelRGB(t, layer, x0, y0+1)
	c11 := sampleTexturePixelRGB(t, layer, x0+1, y0+1)
	return [3]float32{
		mix(mix(c00[0], c10[0], tx), mix(c01[0], c11[0], tx), ty),
		mix(mix(c00[1], c10[1], tx), mix(c01[1], c11[1], tx), ty),
		mix(mix(c00[2], c10[2], tx), mix(c01[2], c11[2], tx), ty),
	}
}

func sampleTexturePixelRGB(t *Texture, layer, x, y int) [3]float32 {
	x = wrapTextureIndex(x, t.width)
	y = wrapTextureIndex(y, t.height)
	col := readTextureRGBA(t, layer, x, y)
	return [3]float32{
		float32(col.R) / 255,
		float32(col.G) / 255,
		float32(col.B) / 255,
	}
}

func wrapTextureIndex(i, size int) int {
	if size <= 0 {
		return 0
	}
	i %= size
	if i < 0 {
		i += size
	}
	return i
}

func mipSize(width, height, level int) (int, int) {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	for i := 0; i < level; i++ {
		if width > 1 {
			width /= 2
		}
		if height > 1 {
			height /= 2
		}
	}
	return width, height
}

func saturatingAdd(a, b uint8) uint8 {
	sum := int(a) + int(b)
	if sum > 255 {
		return 255
	}
	return uint8(sum)
}

func readTextureRGBA(t *Texture, layer, x, y int) color.RGBA {
	if t == nil || x < 0 || x >= t.width || y < 0 || y >= t.height {
		return color.RGBA{}
	}
	if layer <= 0 {
		rgbaOff := (y*t.width + x) * 4
		if rgbaOff+3 < len(t.rgba) {
			return color.RGBA{
				R: t.rgba[rgbaOff+0],
				G: t.rgba[rgbaOff+1],
				B: t.rgba[rgbaOff+2],
				A: t.rgba[rgbaOff+3],
			}
		}
	}
	bpp := bytesPerPixel(t.format)
	dataOff := texturePixelIndex(t, layer, x, y) * bpp
	if bpp == 0 || dataOff < 0 || dataOff+bpp > len(t.data) {
		return color.RGBA{}
	}
	switch t.format {
	case gpu.FormatRGBA8Unorm, gpu.FormatRGBA8UnormSRGB:
		return color.RGBA{R: t.data[dataOff+0], G: t.data[dataOff+1], B: t.data[dataOff+2], A: t.data[dataOff+3]}
	case gpu.FormatBGRA8Unorm, gpu.FormatBGRA8UnormSRGB:
		return color.RGBA{R: t.data[dataOff+2], G: t.data[dataOff+1], B: t.data[dataOff+0], A: t.data[dataOff+3]}
	}
	return color.RGBA{}
}

func writeTextureRGBA(t *Texture, layer, x, y int, col color.RGBA) {
	if t == nil || x < 0 || x >= t.width || y < 0 || y >= t.height {
		return
	}
	rgbaOff := (y*t.width + x) * 4
	if rgbaOff+3 < len(t.rgba) {
		t.rgba[rgbaOff+0] = col.R
		t.rgba[rgbaOff+1] = col.G
		t.rgba[rgbaOff+2] = col.B
		t.rgba[rgbaOff+3] = col.A
	}

	bpp := bytesPerPixel(t.format)
	dataOff := texturePixelIndex(t, layer, x, y) * bpp
	if bpp == 0 || dataOff < 0 || dataOff+bpp > len(t.data) {
		return
	}
	switch t.format {
	case gpu.FormatRGBA8Unorm, gpu.FormatRGBA8UnormSRGB:
		t.data[dataOff+0] = col.R
		t.data[dataOff+1] = col.G
		t.data[dataOff+2] = col.B
		t.data[dataOff+3] = col.A
	case gpu.FormatBGRA8Unorm, gpu.FormatBGRA8UnormSRGB:
		t.data[dataOff+0] = col.B
		t.data[dataOff+1] = col.G
		t.data[dataOff+2] = col.R
		t.data[dataOff+3] = col.A
	case gpu.FormatRGBA16Float:
		vals := [4]uint16{
			float32ToHalf(float32(col.R) / 255),
			float32ToHalf(float32(col.G) / 255),
			float32ToHalf(float32(col.B) / 255),
			float32ToHalf(float32(col.A) / 255),
		}
		for i, v := range vals {
			binary.LittleEndian.PutUint16(t.data[dataOff+i*2:dataOff+i*2+2], v)
		}
	case gpu.FormatRGBA32Float:
		vals := [4]float32{
			float32(col.R) / 255,
			float32(col.G) / 255,
			float32(col.B) / 255,
			float32(col.A) / 255,
		}
		for i, v := range vals {
			binary.LittleEndian.PutUint32(t.data[dataOff+i*4:dataOff+i*4+4], math.Float32bits(v))
		}
	}
}

func writeTextureUint32(t *Texture, layer, x, y int, v uint32) {
	if t == nil || t.format != gpu.FormatR32Uint || x < 0 || x >= t.width || y < 0 || y >= t.height {
		return
	}
	idx := texturePixelIndex(t, layer, x, y)
	off := idx * 4
	if off < 0 || off+4 > len(t.data) {
		return
	}
	binary.LittleEndian.PutUint32(t.data[off:off+4], v)
	if layer <= 0 {
		rgbaOff := (y*t.width + x) * 4
		if rgbaOff+3 < len(t.rgba) {
			b := uint8(min(int(v), 255))
			t.rgba[rgbaOff+0] = b
			t.rgba[rgbaOff+1] = b
			t.rgba[rgbaOff+2] = b
			t.rgba[rgbaOff+3] = 255
		}
	}
}

func edge(a, b, c [2]float32) float32 {
	return (c[0]-a[0])*(b[1]-a[1]) - (c[1]-a[1])*(b[0]-a[0])
}

func sameSign(v, ref float32) bool {
	if ref < 0 {
		return v <= 0
	}
	return v >= 0
}

func pointOutsideClip(depth float32) bool {
	return math.IsNaN(float64(depth)) || math.IsInf(float64(depth), 0) || depth < -1 || depth > 1
}

func triangleOutsideClip(depths [3]float32) bool {
	return (depths[0] < -1 && depths[1] < -1 && depths[2] < -1) ||
		(depths[0] > 1 && depths[1] > 1 && depths[2] > 1)
}

func mix(a, b, t float32) float32 {
	return a + (b-a)*t
}

func smoothstep(edge0, edge1, x float32) float32 {
	if edge0 == edge1 {
		if x < edge0 {
			return 0
		}
		return 1
	}
	t := clamp01f((x - edge0) / (edge1 - edge0))
	return t * t * (3 - 2*t)
}

func add3(a, b [3]float32) [3]float32 {
	return [3]float32{a[0] + b[0], a[1] + b[1], a[2] + b[2]}
}

func length3(v [3]float32) float32 {
	return float32(math.Sqrt(float64(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])))
}

func normalize3(v [3]float32) [3]float32 {
	l := length3(v)
	if l <= 1e-6 || math.IsNaN(float64(l)) || math.IsInf(float64(l), 0) {
		return [3]float32{0, 1, 0}
	}
	return [3]float32{v[0] / l, v[1] / l, v[2] / l}
}

func hash13(p [3]float32) float32 {
	p0 := fract32(p[0] * 0.1031)
	p1 := fract32(p[1] * 0.1031)
	p2 := fract32(p[2] * 0.1031)
	d := p0*(p1+33.33) + p1*(p2+33.33) + p2*(p0+33.33)
	p0 += d
	p1 += d
	p2 += d
	return fract32((p0 + p1) * p2)
}

func fract32(v float32) float32 {
	return v - float32(math.Floor(float64(v)))
}

func min3(a, b, c float32) float32 {
	return float32(math.Min(float64(a), math.Min(float64(b), float64(c))))
}

func max3(a, b, c float32) float32 {
	return float32(math.Max(float64(a), math.Max(float64(b), float64(c))))
}

func clampByte(v float32) uint8 {
	if math.IsNaN(float64(v)) || v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(v*255 + 0.5)
}

func (t *Texture) refreshRGBA() {
	t.refreshRGBALevel(0)
}

func (t *Texture) refreshRGBALevel(level int) {
	rgba := t.levelRGBA(level)
	data := t.levelData(level)
	if t == nil || len(rgba) == 0 {
		return
	}
	switch t.format {
	case gpu.FormatRGBA8Unorm, gpu.FormatRGBA8UnormSRGB:
		for i := 0; i+3 < len(data) && i+3 < len(rgba); i += 4 {
			copy(rgba[i:i+4], data[i:i+4])
		}
	case gpu.FormatBGRA8Unorm, gpu.FormatBGRA8UnormSRGB:
		for i := 0; i+3 < len(data) && i+3 < len(rgba); i += 4 {
			rgba[i+0] = data[i+2]
			rgba[i+1] = data[i+1]
			rgba[i+2] = data[i+0]
			rgba[i+3] = data[i+3]
		}
	case gpu.FormatR32Uint:
		for i := 0; i+3 < len(data) && i+3 < len(rgba); i += 4 {
			v := data[i]
			rgba[i+0] = v
			rgba[i+1] = v
			rgba[i+2] = v
			rgba[i+3] = 255
		}
	}
}

func bytesPerPixel(f gpu.TextureFormat) int {
	switch f {
	case gpu.FormatRGBA8Unorm, gpu.FormatRGBA8UnormSRGB,
		gpu.FormatBGRA8Unorm, gpu.FormatBGRA8UnormSRGB,
		gpu.FormatR32Uint, gpu.FormatDepth24Plus, gpu.FormatDepth32Float:
		return 4
	case gpu.FormatDepth16Unorm:
		return 2
	case gpu.FormatDepth24PlusStencil8:
		return 4
	case gpu.FormatRGBA16Float:
		return 8
	case gpu.FormatRGBA32Float:
		return 16
	default:
		return 0
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

func clamp01f(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampNonNegative(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

func float32ToHalf(f float32) uint16 {
	bits := math.Float32bits(f)
	sign := uint16((bits >> 16) & 0x8000)
	exp := int((bits>>23)&0xff) - 127 + 15
	mant := bits & 0x7fffff
	switch {
	case exp <= 0:
		if exp < -10 {
			return sign
		}
		mant = (mant | 0x800000) >> uint(1-exp)
		return sign | uint16((mant+0x1000)>>13)
	case exp >= 31:
		return sign | 0x7c00
	default:
		return sign | uint16(exp<<10) | uint16((mant+0x1000)>>13)
	}
}
