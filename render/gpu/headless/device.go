package headless

import (
	"encoding/binary"
	"image"
	"image/color"
	"math"
	"strings"

	"m31labs.dev/gosx/render/gpu"
)

// ComputeExecutor is the interface for a CPU-side implementation of a named
// compute pipeline. RegisterComputeExecutor associates a ComputeExecutor with a
// pipeline label so that DispatchWorkgroups/DispatchWorkgroupsIndirect can
// invoke real compute logic for that label in headless tests.
//
// Exec receives the full bind-group map (slot → BindGroup) and the workgroup
// dispatch dimensions. For DispatchWorkgroupsIndirect the dimensions are (0,0,0)
// — the executor is expected to derive its own count from the bound buffers
// (e.g. from the input buffer's lastWriteSize).
type ComputeExecutor interface {
	Exec(bindGroups map[int]*BindGroup, x, y, z int)
}

// Device is a pure-Go gpu.Device whose "swap chain" is a CPU-backed RGBA
// image. Use New to construct one; the returned Surface represents the
// framebuffer and should be passed into render/bundle.Config.Surface.
type Device struct {
	framebuffer      *image.RGBA
	width            int
	height           int
	queue            *Queue
	surface          *Surface
	computeExecutors map[string]ComputeExecutor
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

// RegisterComputeExecutor registers exec as the CPU implementation for pipelines
// whose Label matches label. Additive: unregistered labels keep today's
// no-op/built-in behaviour; existing headless tests are unaffected.
func (d *Device) RegisterComputeExecutor(label string, exec ComputeExecutor) {
	if d.computeExecutors == nil {
		d.computeExecutors = make(map[string]ComputeExecutor)
	}
	d.computeExecutors[label] = exec
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
func (d *Device) SupportsTextureFormat(format gpu.TextureFormat) bool {
	switch format {
	case gpu.FormatRGBA8Unorm, gpu.FormatRGBA8UnormSRGB,
		gpu.FormatBGRA8Unorm, gpu.FormatBGRA8UnormSRGB,
		gpu.FormatRGBA16Float, gpu.FormatRGBA32Float,
		gpu.FormatRGB9E5Ufloat, gpu.FormatRGB10A2Unorm,
		gpu.FormatDepth16Unorm, gpu.FormatDepth24Plus,
		gpu.FormatDepth24PlusStencil8, gpu.FormatDepth32Float,
		gpu.FormatR32Uint:
		return true
	default:
		return false
	}
}

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
	t.dimension = desc.Dimension
	if t.dimension == gpu.TextureDimensionUndefined {
		t.dimension = gpu.TextureDimension2D
	}
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
	buf.writeGeneration++
}

// WriteTexture blits raw bytes into a Texture. The headless backend keeps
// both exact texture bytes and a display-oriented RGBA cache so render pass
// clears, texture readbacks, and the bundle present pass can be exercised
// without a real rasterizer.
func (q *Queue) WriteTexture(t gpu.Texture, data []byte, bytesPerRow, width, height int) {
	q.WriteTextureLevel(t, 0, data, bytesPerRow, width, height)
}

func (q *Queue) WriteTextureLevel(t gpu.Texture, mipLevel int, data []byte, bytesPerRow, width, height int) {
	q.WriteTextureLevelLayer(t, mipLevel, 0, data, bytesPerRow, height, width, height)
}

func (q *Queue) WriteTextureLevelLayer(t gpu.Texture, mipLevel, layer int, data []byte, bytesPerRow, rowsPerImage, width, height int) {
	tex, ok := t.(*Texture)
	if !ok || tex == nil {
		return
	}
	if bytesPerRow <= 0 || rowsPerImage <= 0 || width <= 0 || height <= 0 || mipLevel < 0 || mipLevel >= tex.mipLevels || layer < 0 {
		return
	}
	tex.lastWriteSize = bytesPerRow * rowsPerImage
	tex.lastWriteMipLevel = mipLevel
	bpp := bytesPerPixel(tex.format)
	if bpp == 0 {
		return
	}
	lw, lh := mipSize(tex.width, tex.height, mipLevel)
	dst := tex.levelData(mipLevel)
	copyW := min(width, lw)
	copyH := min(height, lh)
	layer = textureLayer(tex, layer)
	for y := 0; y < copyH; y++ {
		srcOff := y * bytesPerRow
		dstOff := ((layer*lh + y) * lw) * bpp
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
	// writeGeneration counts the Queue.WriteBuffer calls this buffer has taken.
	// It is the only invalidation key a decode cache needs, and lightCache below
	// is the one such cache. See decodeSceneLightsCached.
	writeGeneration uint64
	lightCache      []sceneLight
	lightCacheGen   uint64
	lightCacheAt    int
}

func (b *Buffer) Size() int              { return b.size }
func (b *Buffer) Usage() gpu.BufferUsage { return b.usage }
func (b *Buffer) Destroy()               {}
func (b *Buffer) ReadAsync(size int) ([]byte, error) {
	out := make([]byte, size)
	copy(out, b.data)
	return out, nil
}

// Data returns a direct (mutable) view of the buffer's backing bytes.
// Executors registered via Device.RegisterComputeExecutor use this to
// read input data and write output data without a copy.
func (b *Buffer) Data() []byte { return b.data }

// LastWriteSize returns the byte count of the last Queue.WriteBuffer call.
// Executors use this to determine how many instances were actually written
// when the buffer was allocated larger than needed.
func (b *Buffer) LastWriteSize() int { return b.lastWriteSize }

type Texture struct {
	width, height     int
	layers            int
	mipLevels         int
	dimension         gpu.TextureDimension
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
func (t *Texture) CreateViewDesc(desc gpu.TextureViewDesc) gpu.TextureView {
	return &TextureView{owner: t, layer: textureViewLayer(desc), desc: desc}
}
func (t *Texture) CreateLayerView(layer int) gpu.TextureView {
	return t.CreateViewDesc(gpu.TextureViewDesc{
		Dimension:       gpu.TextureViewDimension2D,
		BaseArrayLayer:  layer,
		ArrayLayerCount: 1,
	})
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
	desc  gpu.TextureViewDesc
}

func textureViewLayer(desc gpu.TextureViewDesc) int {
	if desc.ArrayLayerCount == 1 {
		return desc.BaseArrayLayer
	}
	return -1
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

// Desc returns the BindGroupDesc used to create this bind group. Executors
// registered via RegisterComputeExecutor use this to resolve bound buffers.
func (b *BindGroup) Desc() gpu.BindGroupDesc { return b.desc }

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
	return &ComputePassEncoder{device: e.device}
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

// SetIndexBuffer and DrawIndexed are not implemented. The bundle renderer
// draws every mesh non-indexed, so nothing exercises them today. They stay as
// no-ops rather than panics because a host may bind an index buffer it never
// draws from. Implement both before adding an indexed path: an indexed draw
// through this backend renders nothing and reports no error.
func (r *RenderPassEncoder) SetIndexBuffer(gpu.Buffer, gpu.IndexFormat) {}
func (r *RenderPassEncoder) Draw(vertexCount, instanceCount, firstVertex, firstInstance int) {
	// A fullscreen pass this backend implements runs its fragment stage. The
	// tone-map, bloom, vignette and colour-grade passes are in that set; read
	// postFragmentFor in postfx.go for the list and the reason.
	if r.bindGroup != nil {
		if dst, ok := r.postDestination(); ok {
			if shade := postFragmentFor(r.desc.Label, r.bindGroup, dst); shade != nil {
				runPostPass(dst, shade)
				return
			}
		}
	}
	if !isFullscreenCopyPass(r.desc.Label) || r.bindGroup == nil {
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
		if view, ok := att.View.(*TextureView); ok && view.owner != nil && att.StoreOp != gpu.StoreOpDiscard {
			copyTextureToTexture(src, view.owner)
			return
		}
	}
}

// DrawIndexed is not implemented. See the note on SetIndexBuffer.
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

// isFullscreenCopyPass reports whether a render pass draws one screen-filling
// triangle that reads its whole input from the texture at binding zero and that
// this backend does not shade. Such a pass copies its source through unchanged.
//
// Draw asks postFragmentFor first, so a label this backend does shade never
// reaches the copy. The set that still copies is ambient occlusion, depth of
// field, anti-aliasing, and every Selena custom pass. The first two read the
// depth attachment and the last runs author-supplied WGSL, none of which this
// backend interprets.
//
// Read the chain to see why. Each effect clears a scratch target, draws into it,
// and hands the scratch view to the next stage. The last stage's target becomes
// the presented image. A dropped draw therefore presents a target that holds
// only its clear colour, so one authored vignette turned every headless frame
// into flat black and the backend reported success. A copy makes the effect
// visibly absent instead, which is honest and keeps the scene.
func isFullscreenCopyPass(label string) bool {
	switch label {
	case "bundle.present", "bundle.present.compose", "bundle.fxaa311":
		return true
	}
	return strings.HasPrefix(label, "bundle.postfx.")
}

type ComputePassEncoder struct {
	device     *Device
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
func (c *ComputePassEncoder) DispatchWorkgroups(x, y, z int) {
	if c.pipeline == nil {
		return
	}
	bg := c.bindGroups[0]
	if bg == nil {
		return
	}
	switch c.pipeline.desc.Label {
	case "bundle.cull":
		runCullFrustum(bg)
	case "bundle.particles.update":
		// The particle WGSL uses workgroup_size(64). Limit the CPU update
		// to the same logical invocation count the recorded dispatch asked
		// for; oversize buffers keep their untouched tail deterministic.
		runParticleUpdate(bg, max(0, x)*64)
	default:
		// Invoke a registered ComputeExecutor for this pipeline label, if any.
		if c.device != nil {
			if exec, ok := c.device.computeExecutors[c.pipeline.desc.Label]; ok {
				exec.Exec(c.bindGroups, x, y, z)
			}
		}
	}
}

// DispatchWorkgroupsIndirect approximates indirect dispatch in the CPU
// backend. The workgroup count lives in a GPU buffer this backend does not
// introspect, so only kernels that derive their own invocation count from the
// bound buffers run. runCullFrustum is one: it reads the instance count from
// the input buffer's last write size. Registered ComputeExecutors receive
// (0, 0, 0) for the workgroup dimensions and must do the same.
func (c *ComputePassEncoder) DispatchWorkgroupsIndirect(_ gpu.Buffer, _ int) {
	if c.pipeline == nil {
		return
	}
	bg := c.bindGroups[0]
	if bg == nil {
		return
	}
	switch c.pipeline.desc.Label {
	case "bundle.cull":
		runCullFrustum(bg)
	default:
		// Invoke a registered ComputeExecutor for this pipeline label, if any.
		// Pass (0,0,0) dims; the executor must use buffer state for its count.
		if c.device != nil {
			if exec, ok := c.device.computeExecutors[c.pipeline.desc.Label]; ok {
				exec.Exec(c.bindGroups, 0, 0, 0)
			}
		}
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
	case gpu.FormatRGB9E5Ufloat, gpu.FormatRGB10A2Unorm:
		packed := packRGB10A2(r, g, b, a)
		for i := 0; i+3 < len(t.data); i += bpp {
			binary.LittleEndian.PutUint32(t.data[i:i+4], packed)
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

// clearDepthView fills one depth layer, or the whole texture when layer is
// negative, with a single depth value.
//
// The fill runs through repeatPattern instead of a per-texel loop because this is
// the hottest function in the whole backend. render/bundle clears three
// 2048-square shadow cascades on every frame, which is twelve million texels, and
// it does so whether or not the scene has a shadow caster. A CPU profile of an
// empty sixteen-pixel frame put 98.8 percent of all samples in this function.
func clearDepthView(t *Texture, layer int, depth float64) {
	if t == nil || !t.format.HasDepth() {
		return
	}
	v := float32(clamp01(depth))
	start, end := textureLayerRange(t, layer)
	if depthEnd := min(end, len(t.depth)); start < depthEnd {
		repeatFloat32(t.depth[start:depthEnd], v)
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
	limit := min(dataEnd, len(t.data))
	if dataStart < 0 || dataStart+bpp > limit {
		return
	}
	// Round the span down to whole texels. The per-texel loop this replaced
	// stopped before a partial trailing texel, so keep that boundary exactly.
	region := t.data[dataStart : dataStart+((limit-dataStart)/bpp)*bpp]
	switch t.format {
	case gpu.FormatDepth16Unorm:
		binary.LittleEndian.PutUint16(region[:2], uint16(math.Round(float64(v*0xffff))))
	case gpu.FormatDepth24Plus, gpu.FormatDepth24PlusStencil8:
		binary.LittleEndian.PutUint32(region[:4], uint32(math.Round(float64(v*0x00ffffff))))
	case gpu.FormatDepth32Float:
		binary.LittleEndian.PutUint32(region[:4], math.Float32bits(v))
	default:
		return
	}
	repeatPattern(region, bpp)
}

// repeatPattern copies the first stride bytes of dst across the rest of dst. It
// doubles the written span each round, so the Go runtime serves the fill with
// memmove and it runs at memory bandwidth rather than one write per texel.
func repeatPattern(dst []byte, stride int) {
	if stride <= 0 || len(dst) <= stride {
		return
	}
	for filled := stride; filled < len(dst); {
		filled += copy(dst[filled:], dst[:filled])
	}
}

// repeatFloat32 fills dst with one value using the same doubling copy.
func repeatFloat32(dst []float32, value float32) {
	if len(dst) == 0 {
		return
	}
	dst[0] = value
	for filled := 1; filled < len(dst); {
		filled += copy(dst[filled:], dst[:filled])
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

func copyTextureToTexture(src, dst *Texture) {
	if src == nil || dst == nil || len(src.rgba) == 0 {
		return
	}
	w := min(src.width, dst.width)
	h := min(src.height, dst.height)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			srcOff := (y*src.width + x) * 4
			writeTextureRGBA(dst, -1, x, y, color.RGBA{
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

// instanceRecordStride is the byte size of one InstanceRecord in the cull
// shader: a column-major mat4 of float32 (64 B) plus a vec4 of u32 (16 B).
const instanceRecordStride = 80

// cullPlaneCount is the number of frustum planes the cull uniform carries.
const cullPlaneCount = 6

// cullUniformInstanceCountOffset is the byte offset of the live instance count
// inside the cull uniform block, and cullUnboundedInstanceCount is the value a
// caller writes when it supplies no count. Both mirror render/bundle/cull.go;
// keep the two in step. TestCullUniformCarriesLiveInstanceCount over there pins
// the writer, and TestCullDropsRecordsPastTheLiveInstanceCount here pins this
// reader.
const (
	cullUniformInstanceCountOffset = 104
	cullUnboundedInstanceCount     = uint32(0xffffffff)
)

// runCullFrustum executes the render/bundle cull shader on the CPU. It is a
// counterpart of cullWGSL: one bounding-sphere test per instance against six
// frustum planes, with the primitive radius scaled by the instance's largest
// axis scale, then compaction of the survivors into the output buffer.
//
// Headless used to pass every instance through untouched. Running the real test
// turns this device into an oracle for GPU-driven culling: a test can pin that a
// scaled instance survives, or that an off-screen instance is dropped, with no
// GPU present.
//
// A zero uniform block degrades to "everything visible", because a zero plane
// gives distance 0 and 0 is never below a negative radius. The instance-count
// lane is the exception: zero there means zero live instances, exactly as it
// does on a real device.
func runCullFrustum(bg *BindGroup) {
	var uniforms, input, output, drawArgs *Buffer
	for _, entry := range bg.desc.Entries {
		switch entry.Binding {
		case 0:
			uniforms, _ = entry.Buffer.(*Buffer)
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
	instanceBytes -= instanceBytes % instanceRecordStride
	instanceCount := instanceBytes / instanceRecordStride

	var planes [cullPlaneCount][4]float32
	baseRadius := float32(0)
	if uniforms != nil && len(uniforms.data) >= 104 {
		for p := 0; p < cullPlaneCount; p++ {
			for c := 0; c < 4; c++ {
				planes[p][c] = readFloat32(uniforms.data, p*16+c*4)
			}
		}
		baseRadius = readFloat32(uniforms.data, 100)
	}
	// The kernel bounds its threads on min(cull.instanceCount, buffer length).
	// Apply the same bound here, or this oracle keeps records a real device
	// drops. cullUnboundedInstanceCount means the caller supplied no count.
	if uniforms != nil && len(uniforms.data) >= cullUniformInstanceCountOffset+4 {
		live := binary.LittleEndian.Uint32(uniforms.data[cullUniformInstanceCountOffset : cullUniformInstanceCountOffset+4])
		if live != cullUnboundedInstanceCount && int(live) < instanceCount {
			instanceCount = int(live)
		}
	}

	visible := 0
	for i := 0; i < instanceCount; i++ {
		record := input.data[i*instanceRecordStride : (i+1)*instanceRecordStride]
		if !cullInstanceVisible(record, planes, baseRadius) {
			continue
		}
		dst := output.data[visible*instanceRecordStride : (visible+1)*instanceRecordStride]
		copy(dst, record)
		visible++
	}
	output.lastWriteOffset = 0
	output.lastWriteSize = visible * instanceRecordStride
	binary.LittleEndian.PutUint32(drawArgs.data[4:8], uint32(visible))
}

// cullInstanceVisible mirrors the per-thread body of cullWGSL for one packed
// instance record.
func cullInstanceVisible(record []byte, planes [cullPlaneCount][4]float32, baseRadius float32) bool {
	// Column-major mat4: column c starts at byte c*16.
	scale := columnScale(record, 0)
	if s := columnScale(record, 1); s > scale {
		scale = s
	}
	if s := columnScale(record, 2); s > scale {
		scale = s
	}
	radius := baseRadius
	if scale > 0 {
		radius = baseRadius * scale
	}
	cx := readFloat32(record, 48)
	cy := readFloat32(record, 52)
	cz := readFloat32(record, 56)
	for _, plane := range planes {
		d := plane[0]*cx + plane[1]*cy + plane[2]*cz + plane[3]
		if d < -radius {
			return false
		}
	}
	return true
}

func columnScale(record []byte, column int) float32 {
	x := readFloat32(record, column*16+0)
	y := readFloat32(record, column*16+4)
	z := readFloat32(record, column*16+8)
	return float32(math.Sqrt(float64(x*x + y*y + z*z)))
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
	if uniforms == nil || particles == nil || len(uniforms.data) < headlessParticleUniformSize {
		return
	}
	dt := readFloat32(uniforms.data, 0)
	tSeconds := readFloat32(uniforms.data, 4)
	lifetime := readFloat32(uniforms.data, 8)
	forceCount := int(readFloat32(uniforms.data, 12) + 0.5)
	if forceCount < 0 {
		forceCount = 0
	}
	if forceCount > headlessParticleMaxForces {
		forceCount = headlessParticleMaxForces
	}
	emitter := [4]float32{
		readFloat32(uniforms.data, 16),
		readFloat32(uniforms.data, 20),
		readFloat32(uniforms.data, 24),
		readFloat32(uniforms.data, 28),
	}
	initialSpeed := readFloat32(uniforms.data, 32)
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
			acceleration := [3]float32{}
			drag := float32(0)
			for fi := 0; fi < forceCount; fi++ {
				forceOff := headlessParticleUniformForceOffset + fi*headlessParticleForceStride
				kind := int(readFloat32(uniforms.data, forceOff+0) + 0.5)
				strength := readFloat32(uniforms.data, forceOff+4)
				frequency := readFloat32(uniforms.data, forceOff+8)
				vector := [3]float32{
					readFloat32(uniforms.data, forceOff+16),
					readFloat32(uniforms.data, forceOff+20),
					readFloat32(uniforms.data, forceOff+24),
				}
				if kind == headlessParticleForceDrag {
					drag += strength
					continue
				}
				acceleration = addVec3(acceleration, headlessParticleForceAcceleration(kind, strength, frequency, vector, [3]float32{pos[0], pos[1], pos[2]}, tSeconds))
			}
			dragFactor := clamp01f(1 - drag*dt)
			vel[0] = vel[0]*dragFactor + acceleration[0]*dt
			vel[1] = vel[1]*dragFactor + acceleration[1]*dt
			vel[2] = vel[2]*dragFactor + acceleration[2]*dt
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

const (
	headlessParticleMaxForces          = 8
	headlessParticleForceStride        = 32
	headlessParticleUniformForceOffset = 48
	headlessParticleUniformSize        = headlessParticleUniformForceOffset + headlessParticleMaxForces*headlessParticleForceStride

	headlessParticleForceGravity = 1
	headlessParticleForceDrag    = 2
	headlessParticleForceWind    = 3
	headlessParticleForceAttract = 4
	headlessParticleForceVortex  = 5
	headlessParticleForceNoise   = 6
)

func headlessParticleForceAcceleration(kind int, strength, frequency float32, vector, pos [3]float32, tSeconds float32) [3]float32 {
	switch kind {
	case headlessParticleForceGravity:
		return scaleVec3(vec3OrDefault(vector, [3]float32{0, -1, 0}), strength)
	case headlessParticleForceWind:
		return scaleVec3(vec3OrDefault(vector, [3]float32{1, 0, 0}), strength)
	case headlessParticleForceAttract:
		delta := subVec3(vector, pos)
		if lengthVec3(delta) <= 0.0001 {
			return [3]float32{}
		}
		return scaleVec3(normalizeVec3(delta), strength)
	case headlessParticleForceVortex:
		axis := normalizeVec3(vec3OrDefault(vector, [3]float32{0, 1, 0}))
		radial := subVec3(pos, scaleVec3(axis, dotVec3(pos, axis)))
		if lengthVec3(radial) <= 0.0001 {
			return [3]float32{}
		}
		return scaleVec3(normalizeVec3(crossVec3(radial, axis)), strength)
	case headlessParticleForceNoise:
		if frequency == 0 {
			frequency = 1
		}
		nx := float32(math.Sin(float64(pos[0]*frequency+tSeconds*1.3)) * math.Cos(float64(pos[2]*frequency+tSeconds*0.7)))
		ny := float32(math.Sin(float64(pos[1]*frequency+tSeconds*0.9)) * math.Cos(float64(pos[0]*frequency+tSeconds*1.1)))
		nz := float32(math.Sin(float64(pos[2]*frequency+tSeconds*1.7)) * math.Cos(float64(pos[1]*frequency+tSeconds*0.5)))
		return scaleVec3([3]float32{nx, ny, nz}, strength)
	}
	return [3]float32{}
}

func vec3OrDefault(v, fallback [3]float32) [3]float32 {
	if dotVec3(v, v) <= 0.00000001 {
		return fallback
	}
	return v
}

func addVec3(a, b [3]float32) [3]float32 {
	return [3]float32{a[0] + b[0], a[1] + b[1], a[2] + b[2]}
}

func subVec3(a, b [3]float32) [3]float32 {
	return [3]float32{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
}

func scaleVec3(v [3]float32, s float32) [3]float32 {
	return [3]float32{v[0] * s, v[1] * s, v[2] * s}
}

func dotVec3(a, b [3]float32) float32 {
	return a[0]*b[0] + a[1]*b[1] + a[2]*b[2]
}

func crossVec3(a, b [3]float32) [3]float32 {
	return [3]float32{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

func lengthVec3(v [3]float32) float32 {
	return float32(math.Sqrt(float64(dotVec3(v, v))))
}

func normalizeVec3(v [3]float32) [3]float32 {
	l := lengthVec3(v)
	if l <= 0.000001 {
		return [3]float32{}
	}
	return scaleVec3(v, 1/l)
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
	instanceStride := 64
	instanceSlot := 4
	if label == "bundle.shadow" {
		instanceSlot = 1
	}
	if instanceBuf != nil && instanceSlot < len(r.pipeline.desc.Vertex.Buffers) {
		if stride := r.pipeline.desc.Vertex.Buffers[instanceSlot].ArrayStride; stride >= 64 {
			instanceStride = stride
		}
	}
	// The lit pipeline shades per pixel. Every other pipeline hands the fill a
	// finished colour to interpolate.
	var shading *litProgram
	if label == "bundle.lit" {
		program := newLitProgram(lighting, material)
		shading = &program
	}
	var tri [3]clipVertex
	var clipScratch [4]clipVertex
	for inst := firstInstance; inst < firstInstance+instanceCount; inst++ {
		model, ok := readMat4Stride(instanceBuf, inst, instanceStride)
		if !ok {
			model = identityMat4()
		}
		// The instance record carries its stable pick ID in the first u32 after
		// the model matrix. Writing it into the id attachment lets a test drive
		// the renderer's whole pick path — queue, copy, read back, resolve —
		// with no GPU present. The field stayed zero before, so every headless
		// pick reported background.
		target.pickID = readInstancePickID(instanceBuf, inst, instanceStride)
		for base := 0; base+2 < vertexCount; base += 3 {
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
				tri[i].clip = transformToClip(mvp, worldPos)
				tri[i].world = worldPos
				if label == "bundle.lit" {
					// The solid colour, the normal and the texture coordinate
					// travel to the fill, which runs the whole material model per
					// pixel as litWGSL does. Every map is sampled there, not here:
					// sampling at the corners quantized a texture to the triangle
					// and left a normal map with nothing to perturb.
					tri[i].color = material.solidColor(readColor(colorBuf, vertex))
					tri[i].uv = readUV(uvBuf, vertex)
					normal := readNormal(normalBuf, vertex)
					if instanceBuf != nil {
						normal = transformDirection(model, normal)
					}
					tri[i].normal = normal
				} else {
					color := readColor(colorBuf, vertex)
					tri[i].color = [4]float32{color[0], color[1], color[2], 1}
				}
			}
			if !valid {
				continue
			}
			r.rasterizeClippedTriangle(target, tri, clipScratch[:0], shading)
		}
	}
}

// clipVertex is one triangle corner in homogeneous clip space plus every
// attribute the rasterizer interpolates across the face.
type clipVertex struct {
	clip   [4]float32
	color  [4]float32
	world  [3]float32
	normal [3]float32
	uv     [2]float32
}

// rasterizeClippedTriangle clips one triangle against the near plane and draws
// the surviving polygon as a fan.
//
// The rasterizer used to drop any triangle with a vertex behind the camera,
// because the projection of such a vertex has a non-positive w. That silently
// removed every large ground plane from every headless image: a plane wide
// enough to receive a shadow always has one corner behind the camera. No test
// in this package could witness a shadow landing on a ground plane, and the
// backend reported a black frame instead of failing.
func (r *RenderPassEncoder) rasterizeClippedTriangle(
	target rasterTarget, tri [3]clipVertex, scratch []clipVertex, shading *litProgram,
) {
	poly := clipTriangleNearPlane(tri, scratch)
	if len(poly) < 3 {
		return
	}
	for i := 1; i+1 < len(poly); i++ {
		var verts [3]rasterVertex
		corners := [3]clipVertex{poly[0], poly[i], poly[i+1]}
		ok := true
		for j, v := range corners {
			x, y, depth, good := projectClip(v.clip, target.width, target.height)
			if !good {
				ok = false
				break
			}
			verts[j] = rasterVertex{
				x: x, y: y, depth: depth, invW: 1 / v.clip[3],
				color: v.color, world: v.world, normal: v.normal, uv: v.uv,
			}
		}
		if ok && !triangleOutsideClip([3]float32{verts[0].depth, verts[1].depth, verts[2].depth}) {
			rasterizeTriangle(target, verts, shading)
		}
	}
}

// clipTriangleNearPlane clips a triangle against the near plane in homogeneous
// clip space and appends the result to out.
//
// The near plane is z + w >= 0, the form that matches a projection whose clip
// depth runs from -1 to 1. Both mat4Perspective and mat4Orthographic in the
// bundle package produce that range. A point behind a perspective camera fails
// the test, so one plane covers both the near clip and the negative-w case.
//
// Clipping a triangle against one plane yields at most four vertices, so a
// four-element scratch array serves every call and the rasterizer allocates
// nothing per triangle.
func clipTriangleNearPlane(tri [3]clipVertex, out []clipVertex) []clipVertex {
	distance := func(v clipVertex) float32 { return v.clip[2] + v.clip[3] }
	inside := [3]bool{
		distance(tri[0]) >= 0,
		distance(tri[1]) >= 0,
		distance(tri[2]) >= 0,
	}
	if inside[0] && inside[1] && inside[2] {
		return append(out, tri[0], tri[1], tri[2])
	}
	if !inside[0] && !inside[1] && !inside[2] {
		return out
	}
	for i := 0; i < 3; i++ {
		current := tri[i]
		next := tri[(i+1)%3]
		if inside[i] {
			out = append(out, current)
		}
		if inside[i] != inside[(i+1)%3] {
			d0 := distance(current)
			d1 := distance(next)
			denom := d0 - d1
			if denom == 0 {
				continue
			}
			out = append(out, lerpClipVertex(current, next, d0/denom))
		}
	}
	return out
}

// lerpClipVertex interpolates a clip-space vertex. Clip space is linear in the
// homogeneous coordinates, so a straight interpolation of both the position and
// the colour is correct here; the perspective divide happens afterwards.
func lerpClipVertex(a, b clipVertex, t float32) clipVertex {
	var out clipVertex
	for i := 0; i < 4; i++ {
		out.clip[i] = a.clip[i] + (b.clip[i]-a.clip[i])*t
		out.color[i] = a.color[i] + (b.color[i]-a.color[i])*t
	}
	for i := 0; i < 3; i++ {
		out.world[i] = a.world[i] + (b.world[i]-a.world[i])*t
		out.normal[i] = a.normal[i] + (b.normal[i]-a.normal[i])*t
	}
	for i := 0; i < 2; i++ {
		out.uv[i] = a.uv[i] + (b.uv[i]-a.uv[i])*t
	}
	return out
}

// transformToClip projects a world point into homogeneous clip space without
// dividing. Callers clip first and divide afterwards.
func transformToClip(m [16]float32, p [3]float32) [4]float32 {
	x, y, z := p[0], p[1], p[2]
	return [4]float32{
		m[0]*x + m[4]*y + m[8]*z + m[12],
		m[1]*x + m[5]*y + m[9]*z + m[13],
		m[2]*x + m[6]*y + m[10]*z + m[14],
		m[3]*x + m[7]*y + m[11]*z + m[15],
	}
}

// projectClip divides a clip-space position through and maps it to pixel
// coordinates plus a normalized depth.
func projectClip(clip [4]float32, width, height int) (float32, float32, float32, bool) {
	if width <= 0 || height <= 0 {
		return 0, 0, 0, false
	}
	w := clip[3]
	if w <= 0 || math.IsNaN(float64(w)) || math.IsInf(float64(w), 0) {
		return 0, 0, 0, false
	}
	ndcX := clip[0] / w
	ndcY := clip[1] / w
	ndcZ := clip[2] / w
	if math.IsNaN(float64(ndcX)) || math.IsNaN(float64(ndcY)) ||
		math.IsNaN(float64(ndcZ)) || math.IsInf(float64(ndcX), 0) ||
		math.IsInf(float64(ndcY), 0) || math.IsInf(float64(ndcZ), 0) {
		return 0, 0, 0, false
	}
	sx := (ndcX*0.5 + 0.5) * float32(width-1)
	sy := (1 - (ndcY*0.5 + 0.5)) * float32(height-1)
	return sx, sy, ndcZ, true
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

// textureBinding is one bound texture view: the texture plus the array layer the
// view selects, or -1 when the view covers the whole texture.
type textureBinding struct {
	tex   *Texture
	layer int
}

func (b textureBinding) bound() bool { return b.tex != nil }

// sampleRGB reads the texture at one UV and returns white when nothing is bound.
// White is the identity for every place litWGSL multiplies a sample in, so an
// absent map leaves the flat material factor alone.
func (b textureBinding) sampleRGB(uv [2]float32) [3]float32 {
	return sampleTextureRGB(b.tex, b.layer, uv)
}

// sampleCube reads a cube map along dir.
//
// WebGPU orders the six cube faces +X, -X, +Y, -Y, +Z, -Z, and stores each one in
// its own array layer. Pick the layer from the major axis, then project the other
// two axes onto that face. render/bundle builds one texel per face today, so the
// projection changes nothing yet; write it out so a larger cube map still reads
// correctly.
func (b textureBinding) sampleCube(dir [3]float32) [3]float32 {
	if b.tex == nil || b.tex.layers < 6 {
		return [3]float32{1, 1, 1}
	}
	ax, ay, az := absf(dir[0]), absf(dir[1]), absf(dir[2])
	var face int
	var major, sc, tc float32
	switch {
	case ax >= ay && ax >= az:
		if dir[0] > 0 {
			face, major, sc, tc = 0, ax, -dir[2], -dir[1]
		} else {
			face, major, sc, tc = 1, ax, dir[2], -dir[1]
		}
	case ay >= az:
		if dir[1] > 0 {
			face, major, sc, tc = 2, ay, dir[0], dir[2]
		} else {
			face, major, sc, tc = 3, ay, dir[0], -dir[2]
		}
	default:
		if dir[2] > 0 {
			face, major, sc, tc = 4, az, dir[0], -dir[1]
		} else {
			face, major, sc, tc = 5, az, -dir[0], -dir[1]
		}
	}
	if major <= 1e-8 {
		return [3]float32{1, 1, 1}
	}
	u := 0.5 * (sc/major + 1)
	v := 0.5 * (tc/major + 1)
	return sampleTexturePixelRGB(b.tex, face,
		clampIndex(int(u*float32(b.tex.width)), b.tex.width),
		clampIndex(int(v*float32(b.tex.height)), b.tex.height))
}

func clampIndex(i, size int) int {
	if i < 0 {
		return 0
	}
	if i >= size {
		return size - 1
	}
	return i
}

// materialState is the CPU copy of the Material uniform litWGSL reads at group 1
// binding 0, plus the five texture views bound beside it.
//
// The byte offsets follow materialUniformBytes in render/bundle/material.go. Add
// a lane there and add a field here, or the rasterizer shades a material the
// shader would shade differently and no golden frame can see the difference.
type materialState struct {
	baseColor      [3]float32
	opacity        float32
	metalness      float32
	roughness      float32
	emissiveScale  float32
	useVertexColor bool
	// emissive holds the Material.emissive vec4. litWGSL no longer reads it:
	// the emissive colour comes from the base colour, or from the emissive map
	// when one is bound. Keep the field so a dedicated emissive colour has a
	// place to arrive.
	emissive [3]float32

	clearcoat    float32
	sheen        float32
	transmission float32
	iridescence  float32
	anisotropy   float32

	hasNormalMap    bool
	hasRoughMap     bool
	hasMetalMap     bool
	hasEmissiveMap  bool
	hasOcclusionMap bool
	// hasWireframe mirrors litWGSL's textureParams2.z gate. It is read by
	// rasterizeTriangle, not by litProgram.shade: wireframe is a rasterizer
	// discard, not a shading term, and the WGSL copy discards before it
	// shades too.
	hasWireframe bool

	baseColorMap textureBinding
	normalMap    textureBinding
	roughMap     textureBinding
	metalMap     textureBinding
	emissiveMap  textureBinding
	occlusionMap textureBinding
}

// defaultMaterialState matches defaultVertexColorMaterial in render/bundle: a
// vertex-coloured dielectric at roughness 0.6. A draw with no material bind group
// takes it.
func defaultMaterialState() materialState {
	return materialState{
		baseColor:      [3]float32{1, 1, 1},
		opacity:        1,
		roughness:      0.6,
		useVertexColor: true,
		baseColorMap:   textureBinding{layer: -1},
		normalMap:      textureBinding{layer: -1},
		roughMap:       textureBinding{layer: -1},
		metalMap:       textureBinding{layer: -1},
		emissiveMap:    textureBinding{layer: -1},
		occlusionMap:   textureBinding{layer: -1},
	}
}

func (r *RenderPassEncoder) activeMaterial() materialState {
	state := defaultMaterialState()
	bg := r.bindGroups[1]
	if bg == nil {
		return state
	}
	bind := func(entry gpu.BindGroupEntry) textureBinding {
		view, ok := entry.TextureView.(*TextureView)
		if !ok || view == nil {
			return textureBinding{layer: -1}
		}
		return textureBinding{tex: view.owner, layer: view.layer}
	}
	for _, entry := range bg.desc.Entries {
		switch entry.Binding {
		case 0:
			buf, ok := entry.Buffer.(*Buffer)
			if !ok || buf == nil {
				continue
			}
			offset := entry.Offset
			if offset < 0 || offset+16 > len(buf.data) {
				continue
			}
			// Every lane past the base colour reads through a bounds-checked
			// helper. A unit test binds a short buffer on purpose, and a short
			// buffer must yield zeros rather than panic.
			data := buf.data
			state.baseColor = readVec3At(data, offset+0)
			state.opacity = clamp01f(readFloat32At(data, offset+12))
			state.metalness = readFloat32At(data, offset+16)
			state.roughness = readFloat32At(data, offset+20)
			state.emissiveScale = readFloat32At(data, offset+24)
			state.useVertexColor = readFloat32At(data, offset+28) >= 0.5
			state.emissive = readVec3At(data, offset+32)
			state.hasNormalMap = readFloat32At(data, offset+52) >= 0.5
			state.hasRoughMap = readFloat32At(data, offset+56) >= 0.5
			state.hasMetalMap = readFloat32At(data, offset+60) >= 0.5
			state.hasEmissiveMap = readFloat32At(data, offset+64) >= 0.5
			state.hasOcclusionMap = readFloat32At(data, offset+68) >= 0.5
			state.hasWireframe = readFloat32At(data, offset+72) >= 0.5
			state.clearcoat = readFloat32At(data, offset+80)
			state.sheen = readFloat32At(data, offset+84)
			state.transmission = readFloat32At(data, offset+88)
			state.iridescence = readFloat32At(data, offset+92)
			state.anisotropy = readFloat32At(data, offset+96)
		case 1:
			state.baseColorMap = bind(entry)
		case 3:
			state.normalMap = bind(entry)
		case 5:
			state.roughMap = bind(entry)
		case 6:
			state.metalMap = bind(entry)
		case 7:
			state.emissiveMap = bind(entry)
		case 8:
			state.occlusionMap = bind(entry)
		}
	}
	return state
}

// solidColor returns the untextured base colour and the opacity, which is what
// litWGSL resolves before it samples the base colour map. The rasterizer carries
// this value on the vertex and applies the map per pixel, so a texture no longer
// gets quantized to the triangle corners.
func (m *materialState) solidColor(vertex [3]float32) [4]float32 {
	base := m.baseColor
	if m.useVertexColor {
		base = vertex
	}
	return [4]float32{base[0], base[1], base[2], m.opacity}
}

// resolve composes the solid colour with the base colour map at one UV.
func (m materialState) resolve(vertex [3]float32, uv [2]float32) [4]float32 {
	solid := m.solidColor(vertex)
	sample := m.baseColorMap.sampleRGB(uv)
	return [4]float32{
		solid[0] * sample[0],
		solid[1] * sample[1],
		solid[2] * sample[2],
		solid[3],
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
	// envParams mirrors the Scene uniform lane litWGSL reads: x is the cubemap
	// intensity, y is the rotation about Y in radians, z is one when a cubemap
	// is authored. z stays zero for a scene with no environment map, and then
	// the whole image-based term drops out.
	envParams   [4]float32
	envCube     textureBinding
	shadow      *Texture
	shadowLayer int
	// fogParams mirrors the Scene uniform lane litWGSL reads: xyz is the fog
	// colour in linear rgb, w is the exponential-squared density. w <= 0 means
	// no fog, and litProgram.shade skips the term entirely.
	fogParams [4]float32

	// lights holds every authored scene light, in bundle order. litWGSL reads
	// the same records from a storage buffer at group 0 binding 5 and shades one
	// Cook-Torrance lobe per light.
	//
	// lightParams.x is the live record count and lightParams.y is the index of
	// the light the cascaded shadow map is fitted to. The storage buffer's
	// capacity is a power of two, so it runs past the count; bound on the count,
	// never on the buffer length, or a zero record shades as a black ambient
	// light at the origin.
	lights      []sceneLight
	lightParams [4]float32
}

// sceneLight is one decoded record of the light storage buffer. Every field
// names the lane it comes from in the Light struct of litWGSL.
type sceneLight struct {
	position    [3]float32 // world position; a directional light leaves it zero
	kind        int        // 0 ambient, 1 directional, 2 point, 3 spot, 4 hemisphere, 5 rect-area
	direction   [3]float32 // the direction the light shines
	intensity   float32
	color       [3]float32 // colour, or the sky colour of a hemisphere light
	rangeLimit  float32    // zero means no windowed falloff
	decay       float32
	coneAngle   float32
	groundColor [3]float32 // hemisphere ground colour
	penumbra    float32    // spot penumbra, zero to one
}

// lightKind codes. These are the codes lightKindCode in render/bundle writes and
// both browser renderers read. A light probe folds onto ambient, because a probe
// carries no position.
const (
	lightKindAmbient     = 0
	lightKindDirectional = 1
	lightKindPoint       = 2
	lightKindSpot        = 3
	lightKindHemisphere  = 4
	lightKindRectArea    = 5
)

// lightRecordSize is the byte size of one packed light: five vec4 of float32.
// render/bundle/renderer.go writes this layout; keep the two in step.
const lightRecordSize = 80

// sceneUniformSize is the byte size of the Scene struct litWGSL reads: four mat4
// and ten vec4. It mirrors sceneUniformSize in render/bundle/renderer.go, and
// activeLighting reads every lane of it.
const sceneUniformSize = 416

func defaultSceneLighting() sceneLighting {
	state := sceneLighting{
		lightDir:     [3]float32{-0.4, -1.0, -0.3},
		lightColor:   [4]float32{1, 0.96, 0.9, 1},
		ambientColor: [4]float32{0.35, 0.38, 0.45, 0.35},
		skyColor:     [4]float32{0.8, 0.88, 1, 1},
		groundColor:  [4]float32{0.28, 0.24, 0.22, 1},
		envCube:      textureBinding{layer: -1},
		shadowLayer:  -1,
	}
	for i := range state.lightViewProjs {
		state.lightViewProjs[i] = identityMat4()
	}
	state.lights = []sceneLight{keyLightFrom(state)}
	state.lightParams = [4]float32{1, 0, 0, 0}
	return state
}

// keyLightFrom builds one directional light out of the primary light lanes of
// the scene uniform.
//
// It exists because a caller may bind a scene uniform and no light storage
// buffer. Every such caller predates the light array, and the old single-light
// path shaded exactly one directional light from scene.lightDir and
// scene.lightColor, with the intensity in the alpha lane. Rebuilding that light
// keeps those callers rendering the image they rendered before, so no golden
// frame moves.
//
// resolveSceneLights in render/bundle always uploads at least one record, so the
// real path never reaches this.
func keyLightFrom(state sceneLighting) sceneLight {
	return sceneLight{
		kind:      lightKindDirectional,
		direction: state.lightDir,
		intensity: state.lightColor[3],
		color:     [3]float32{state.lightColor[0], state.lightColor[1], state.lightColor[2]},
		decay:     2,
	}
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
			// The guard covers every lane this switch reads. It used to stop at
			// 368, which is the offset of envParams and not its end, so the
			// envParams read below could run past a buffer of exactly 384 bytes.
			// lightParams ends at 400 and fogParams ends at 416.
			if offset < 0 || offset+sceneUniformSize > len(buf.data) {
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
			state.envParams = readVec4At(buf.data, offset+368)
			state.lightParams = readVec4At(buf.data, offset+384)
			state.fogParams = readVec4At(buf.data, offset+400)
		case 5:
			buf, ok := entry.Buffer.(*Buffer)
			if !ok || buf == nil {
				continue
			}
			state.lights = decodeSceneLightsCached(buf, entry.Offset)
		case 1:
			view, ok := entry.TextureView.(*TextureView)
			if !ok || view == nil {
				continue
			}
			state.shadow = view.owner
			state.shadowLayer = view.layer
		case 3:
			view, ok := entry.TextureView.(*TextureView)
			if !ok || view == nil {
				continue
			}
			state.envCube = textureBinding{tex: view.owner, layer: view.layer}
		}
	}
	// A caller that bound a scene uniform and no light storage buffer gets the
	// primary light rebuilt as one directional record. Read keyLightFrom for why.
	if len(state.lights) == 0 {
		state.lights = []sceneLight{keyLightFrom(state)}
		state.lightParams[0] = 1
		state.lightParams[1] = 0
	}
	return state
}

// decodeSceneLightsCached decodes the light storage buffer once per upload.
//
// activeLighting runs once per draw call, so a scene with a thousand meshes
// decoded the same records a thousand times and allocated a slice each time. The
// buffer's write generation is the whole invalidation rule: a frame writes the
// lights once, before the first pass, so every draw in that frame reads the same
// bytes. A caller that never writes the buffer keeps the first decode, which is
// correct because the bytes never change either.
func decodeSceneLightsCached(buf *Buffer, offset int) []sceneLight {
	if buf.lightCache != nil && buf.lightCacheGen == buf.writeGeneration && buf.lightCacheAt == offset {
		return buf.lightCache
	}
	decoded := decodeSceneLights(buf.data, offset)
	buf.lightCache = decoded
	buf.lightCacheGen = buf.writeGeneration
	buf.lightCacheAt = offset
	return decoded
}

// decodeSceneLights reads every 80-byte record the light storage buffer holds.
//
// It decodes the whole buffer, not the live count. The count lives in the scene
// uniform, which this switch may not have read yet, and shade bounds its loop on
// that count exactly as litWGSL does. Decoding the tail costs a few reads on a
// buffer whose capacity is at most 256 records.
func decodeSceneLights(data []byte, offset int) []sceneLight {
	if offset < 0 || offset > len(data) {
		return nil
	}
	data = data[offset:]
	count := len(data) / lightRecordSize
	if count == 0 {
		return nil
	}
	out := make([]sceneLight, count)
	for i := range out {
		base := i * lightRecordSize
		position := readVec4At(data, base+0)
		direction := readVec4At(data, base+16)
		color := readVec4At(data, base+32)
		params := readVec4At(data, base+48)
		ground := readVec4At(data, base+64)
		out[i] = sceneLight{
			position:    [3]float32{position[0], position[1], position[2]},
			kind:        int(position[3]),
			direction:   [3]float32{direction[0], direction[1], direction[2]},
			intensity:   direction[3],
			color:       [3]float32{color[0], color[1], color[2]},
			rangeLimit:  color[3],
			decay:       params[0],
			coneAngle:   params[3],
			groundColor: [3]float32{ground[0], ground[1], ground[2]},
			penumbra:    ground[3],
		}
	}
	return out
}

// litProgram is the CPU copy of the fragment stage of litWGSL in
// render/bundle/lit.go. rasterizeTriangle runs it at every covered pixel, so a
// headless frame answers the same material question as the WebGPU backend.
//
// The model was Lambert diffuse plus an ambient dome until 2026-07-26. It carried
// no specular lobe, so roughness and metalness reached the uniform and left no
// mark on a pixel, and every material feature added for three.js parity was
// unverifiable by the only GPU-free oracle in the repository.
//
// Read litWGSL as the specification, not this file. Each term below names the
// shader line it copies. render/bundle/lit_drift_test.go records where litWGSL
// and the browser copies still disagree and which one this repository judges
// correct.
type litProgram struct {
	lighting sceneLighting
	material materialState

	// light is the direction toward the primary light, which is the light the
	// cascade fit uses. litWGSL keeps it as `let L = normalize(-scene.lightDir.xyz)`
	// outside its light loop and takes the Fresnel term of the image-based
	// lighting block from it, so a cubemap keeps the response it had before the
	// light array arrived. It is constant for a draw, so resolve it once.
	light [3]float32

	// lights is the slice shade loops over, already cut to the live count the
	// scene uniform carries. shadowLight is the index inside it that reads the
	// cascaded shadow map, or -1 when no light does.
	lights      []sceneLight
	shadowLight int
}

// newLitProgram binds one lighting state to one material and precomputes the
// per-draw constants.
func newLitProgram(lighting sceneLighting, material materialState) litProgram {
	// Cut the light slice to the live count. The storage buffer's capacity is a
	// power of two, so the tail holds zero records, and a zero record decodes to
	// a black ambient light. litWGSL takes the same minimum.
	count := int(maxF(lighting.lightParams[0], 0))
	if count > len(lighting.lights) {
		count = len(lighting.lights)
	}
	return litProgram{
		lighting: lighting,
		material: material,
		light: normalize3([3]float32{
			-lighting.lightDir[0], -lighting.lightDir[1], -lighting.lightDir[2],
		}),
		lights:      lighting.lights[:count],
		shadowLight: int(lighting.lightParams[1]),
	}
}

// pointLightAttenuation is the distance falloff of a point or spot light.
//
// A light with a range takes the windowed inverse square law three.js uses; a
// light with no range takes the plain inverse power of the decay. Both browser
// renderers and litWGSL carry the same two expressions.
func pointLightAttenuation(dist, rangeLimit, decay float32) float32 {
	if rangeLimit > 0 {
		ratio := clamp01f(1 - float32(math.Pow(float64(dist/rangeLimit), float64(rangeWindowExponent))))
		return ratio * ratio / max32(dist*dist, attenuationFloor)
	}
	return 1 / max32(float32(math.Pow(float64(dist), float64(decay))), attenuationFloor)
}

// spotConeAttenuation is the cone falloff of a spot light. L points from the
// surface toward the light and spotDir is the direction the light shines. A
// penumbra of zero gives a hard edge; a penumbra of one fades from the axis.
func spotConeAttenuation(L, spotDir [3]float32, angle, penumbra float32) float32 {
	axis := normalize3(spotDir)
	cosAngle := dotVec3(L, [3]float32{-axis[0], -axis[1], -axis[2]})
	outerCos := float32(math.Cos(float64(angle)))
	innerCos := float32(math.Cos(float64(angle * (1 - penumbra))))
	return clamp01f((cosAngle - outerCos) / max32(innerCos-outerCos, spotConeFloor))
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// fragment is one interpolated surface point: the CPU equivalent of the VSOut
// litWGSL receives.
//
// base is the solid material colour before the base colour map. frame is the
// texture-space basis of the triangle, which a normal map needs and every other
// material reads past.
type fragment struct {
	base   [3]float32
	world  [3]float32
	normal [3]float32
	uv     [2]float32
	frame  tangentFrame
}

// tangentFrame is the texture-space tangent of one triangle.
//
// litWGSL derives the same basis from screen-space derivatives, because a
// fragment shader cannot see the other two corners of its triangle. A CPU
// rasterizer can, so it solves the same equation from the triangle itself. The
// two agree on a planar triangle, and this form carries no quad quantization.
// lit_drift_test.go records the tangent basis as a known divergence from the
// browser, which uses an authored vertex tangent.
type tangentFrame struct {
	tangent [3]float32
	valid   bool
}

// triangleTangent solves the same linear system litWGSL solves with dpdx and
// dpdy. Substituting the two triangle edges for the two screen derivatives gives
// the same tangent, because both pairs span the same surface.
func triangleTangent(verts [3]rasterVertex) tangentFrame {
	e1 := subVec3(verts[1].world, verts[0].world)
	e2 := subVec3(verts[2].world, verts[0].world)
	du1 := verts[1].uv[0] - verts[0].uv[0]
	dv1 := verts[1].uv[1] - verts[0].uv[1]
	du2 := verts[2].uv[0] - verts[0].uv[0]
	dv2 := verts[2].uv[1] - verts[0].uv[1]
	det := du1*dv2 - du2*dv1
	if absf(det) < 1e-8 {
		return tangentFrame{}
	}
	inv := 1 / det
	tangent := [3]float32{
		(e1[0]*dv2 - e2[0]*dv1) * inv,
		(e1[1]*dv2 - e2[1]*dv1) * inv,
		(e1[2]*dv2 - e2[2]*dv1) * inv,
	}
	if length3(tangent) < 1e-8 {
		return tangentFrame{}
	}
	return tangentFrame{tangent: tangent, valid: true}
}

// perturbNormal rotates the geometric normal by a tangent-space normal map
// sample. It copies perturbNormal in litWGSL, including the Gram-Schmidt step
// that squares the tangent against the normal.
func perturbNormal(geomN [3]float32, frame tangentFrame, sample [3]float32) [3]float32 {
	if !frame.valid {
		return geomN
	}
	raw := frame.tangent
	along := dotVec3(geomN, raw)
	tangent := normalize3([3]float32{
		raw[0] - geomN[0]*along,
		raw[1] - geomN[1]*along,
		raw[2] - geomN[2]*along,
	})
	bitangent := normalize3(crossVec3(geomN, tangent))
	mapped := [3]float32{sample[0]*2 - 1, sample[1]*2 - 1, sample[2]*2 - 1}
	return normalize3([3]float32{
		tangent[0]*mapped[0] + bitangent[0]*mapped[1] + geomN[0]*mapped[2],
		tangent[1]*mapped[0] + bitangent[1]*mapped[1] + geomN[1]*mapped[2],
		tangent[2]*mapped[0] + bitangent[2]*mapped[1] + geomN[2]*mapped[2],
	})
}

// distributionGGX is the Trowbridge-Reitz normal distribution, copied from
// litWGSL. It sets the width of the specular highlight.
func distributionGGX(NdotH, roughness float32) float32 {
	a := roughness * roughness
	a2 := a * a
	d := NdotH*NdotH*(a2-1) + 1
	return a2 / (math.Pi*d*d + 1e-7)
}

// geometrySmith is the Hammon height-correlated Smith visibility term, copied
// from litWGSL. It already contains the 1/(4 NdotL NdotV) factor, so the caller
// multiplies D, G and F and divides by nothing.
//
// This is the one term where the Go shader and the browser shaders disagree on
// purpose. lit_drift_test.go row "specular-geometry-term" holds the measurement
// and the verdict: the correlated form approaches no masking as the surface
// approaches a mirror, and the browser Schlick form never does.
func geometrySmith(NdotV, NdotL, roughness float32) float32 {
	a := roughness * roughness
	ggxV := NdotL * (NdotV*(1-a) + a)
	ggxL := NdotV * (NdotL*(1-a) + a)
	return 0.5 / max(ggxV+ggxL, 1e-5)
}

// fresnelSchlick is the Schlick approximation, copied from litWGSL. The fifth
// power runs as four multiplications, because math.Pow on a constant integer
// exponent costs about twenty times as much and this call sits in the pixel loop.
func fresnelSchlick(f0 [3]float32, VdotH float32) [3]float32 {
	t := clamp01f(1 - VdotH)
	t2 := t * t
	k := t2 * t2 * t
	return [3]float32{
		f0[0] + (1-f0[0])*k,
		f0[1] + (1-f0[1])*k,
		f0[2] + (1-f0[2])*k,
	}
}

// rotateEnvY turns a direction about the Y axis, copied from litWGSL. It applies
// the authored environment rotation to a cubemap lookup.
func rotateEnvY(v [3]float32, radians float32) [3]float32 {
	if radians == 0 {
		return v
	}
	c := float32(math.Cos(float64(radians)))
	s := float32(math.Sin(float64(radians)))
	return [3]float32{v[0]*c - v[2]*s, v[1], v[0]*s + v[2]*c}
}

// The constants below are the numbers litProgram.shade shares with litWGSL in
// render/bundle/lit.go. Naming them buys one thing: lit_parity_test.go compares
// each value against the shader source, so a change to one copy fails a test
// instead of producing a picture only one backend draws.
//
// The name of each constant is the identifier used by the pinned row that guards
// it. Do not change a value here without changing litWGSL and the browser copies
// in the same commit.
const (
	// dielectricF0 is the normal-incidence reflectance of a non-metal.
	dielectricF0 = float32(0.04)
	// roughnessFloor stops a polished material collapsing to a pinpoint mirror.
	roughnessFloor = float32(0.04)
	// anisotropyRoughnessGain is how far anisotropy narrows the lobe.
	anisotropyRoughnessGain = float32(0.28)
	// clearcoatPowerLow and clearcoatPowerHigh bound the coat exponent.
	clearcoatPowerLow  = float32(12)
	clearcoatPowerHigh = float32(96)
	// clearcoatGain scales the coat lobe.
	clearcoatGain = float32(0.28)
	// sheenGain scales the velvet term.
	sheenGain = float32(0.55)
	// iridescencePhaseGreen, iridescencePhaseBlue and iridescenceFrequency set
	// the hue sweep. iridescenceTintBase and iridescenceTintGain set its depth.
	iridescencePhaseGreen = float32(2.1)
	iridescencePhaseBlue  = float32(4.2)
	iridescenceFrequency  = float32(8.0)
	iridescenceTintBase   = float32(0.65)
	iridescenceTintGain   = float32(0.7)
	// transmissionBaseGain and transmissionMixGain set how far a transmissive
	// surface fades toward the ambient term.
	transmissionBaseGain = float32(0.1)
	transmissionMixGain  = float32(0.55)
	// envSpecularRoughFade is how fast a rough surface loses its cubemap
	// reflection.
	envSpecularRoughFade = float32(0.65)
	// rangeWindowExponent shapes the window a ranged point or spot light fades
	// through. three.js uses the fourth power and both browser renderers copy it.
	rangeWindowExponent = float32(4)
	// attenuationFloor stops a light at zero distance dividing by zero.
	attenuationFloor = float32(0.0001)
	// spotConeFloor stops a spot light with a zero-width penumbra band dividing
	// by zero.
	spotConeFloor = float32(0.001)
)

// shade evaluates the material at one surface point.
//
// directLight sums one Cook-Torrance lobe per scene light. It is the CPU copy of
// the light loop in litWGSL, term for term and branch for branch.
//
// The kinds are the codes both browser renderers use: ambient, directional,
// point, spot and hemisphere. Ambient and hemisphere carry no bidirectional
// reflectance distribution function and no cosine term, so each adds a flat
// product and skips the lobe. A rect-area light contributes nothing, because
// engine.RenderLight carries no width and no height, so the rectangle the form
// factor integrates over does not exist on this path.
//
// One cascaded shadow map exists and it is fitted to the primary directional
// light. Only that light samples it; every other light is unshadowed. litWGSL
// makes the same restriction.
func (p *litProgram) directLight(
	f fragment,
	N, V, baseColor, f0 [3]float32,
	metalness, roughness, NdotV float32,
) [3]float32 {
	const invPi = float32(1 / math.Pi)
	var sum [3]float32
	for index := range p.lights {
		light := &p.lights[index]
		switch light.kind {
		case lightKindAmbient:
			for i := 0; i < 3; i++ {
				sum[i] += baseColor[i] * light.color[i] * light.intensity
			}
			continue
		case lightKindHemisphere:
			hemiBlend := N[1]*0.5 + 0.5
			for i := 0; i < 3; i++ {
				hemiColor := mix(light.groundColor[i], light.color[i], hemiBlend)
				sum[i] += baseColor[i] * hemiColor * light.intensity
			}
			continue
		case lightKindRectArea:
			continue
		}

		var L [3]float32
		attenuation := float32(1)
		switch light.kind {
		case lightKindDirectional:
			L = normalize3([3]float32{-light.direction[0], -light.direction[1], -light.direction[2]})
		case lightKindSpot:
			toLight := subVec3(light.position, f.world)
			dist := length3(toLight)
			L = scaleVec3(toLight, 1/max32(dist, 0.0001))
			cone := spotConeAttenuation(L, light.direction, light.coneAngle, light.penumbra)
			attenuation = pointLightAttenuation(dist, light.rangeLimit, light.decay) * cone
		default:
			toLight := subVec3(light.position, f.world)
			dist := length3(toLight)
			L = scaleVec3(toLight, 1/max32(dist, 0.0001))
			attenuation = pointLightAttenuation(dist, light.rangeLimit, light.decay)
		}

		H := normalize3(add3(V, L))
		NdotL := max(0, dotVec3(N, L))
		NdotH := max(0, dotVec3(N, H))
		VdotH := max(0, dotVec3(V, H))

		d := distributionGGX(NdotH, roughness)
		g := geometrySmith(NdotV, NdotL, roughness)
		fresnel := fresnelSchlick(f0, VdotH)
		dg := d * g

		shadow := float32(1)
		if light.kind == lightKindDirectional && index == p.shadowLight {
			shadow = p.lighting.sampleShadow(f.world)
		}
		for i := 0; i < 3; i++ {
			// Energy conservation: what the specular lobe reflects, the diffuse
			// lobe cannot, and a metal has no diffuse lobe at all.
			kD := (1 - fresnel[i]) * (1 - metalness)
			diffuse := kD * baseColor[i] * invPi
			specular := dg * fresnel[i]
			// The falloff rides in the radiance, exactly as litWGSL puts it
			// there, so the direct term is the same product in both copies.
			radiance := light.color[i] * light.intensity * attenuation
			sum[i] += (diffuse + specular) * radiance * NdotL * shadow
		}
	}
	return sum
}

// Every term below carries the name it has in litWGSL, in the same order, so the
// two copies can be read side by side. A term whose parameter is zero contributes
// exactly zero, so the guards that skip those terms change no pixel; they only
// keep a plain dielectric off the cost of five physical lobes.
func (p *litProgram) shade(f fragment) [3]float32 {
	m := &p.material
	l := &p.lighting

	geomN := normalize3(f.normal)
	N := geomN
	if m.hasNormalMap {
		N = perturbNormal(geomN, f.frame, m.normalMap.sampleRGB(f.uv))
	}
	V := normalize3(subVec3(l.cameraPos, f.world))
	// The primary light decides the Fresnel term the image-based lighting block
	// reuses. litWGSL keeps the same two lines outside its light loop.
	H := normalize3(add3(V, p.light))

	NdotV := max(1e-4, dotVec3(N, V))
	VdotH := max(0, dotVec3(V, H))

	// Base colour: the solid colour the vertex carried, modulated by the base
	// colour map. A texture tints, it does not replace.
	sample := m.baseColorMap.sampleRGB(f.uv)
	baseColor := [3]float32{
		f.base[0] * sample[0],
		f.base[1] * sample[1],
		f.base[2] * sample[2],
	}

	// glTF 2.0 packs roughness in green and metalness in blue. Read the same two
	// channels litWGSL reads, or a packed texture drives both factors from the
	// occlusion channel.
	metalness := m.metalness
	if m.hasMetalMap {
		metalness *= m.metalMap.sampleRGB(f.uv)[2]
	}
	metalness = clamp01f(metalness)
	roughness := m.roughness
	if m.hasRoughMap {
		roughness *= m.roughMap.sampleRGB(f.uv)[1]
	}
	roughness = clampRange(roughness, roughnessFloor, 1)
	anisotropy := clampSignedUnit(m.anisotropy)
	roughness = clampRange(roughness*(1-absf(anisotropy)*anisotropyRoughnessGain), roughnessFloor, 1)

	// F0 is 0.04 for a dielectric and the base colour for a metal.
	f0 := [3]float32{
		mix(dielectricF0, baseColor[0], metalness),
		mix(dielectricF0, baseColor[1], metalness),
		mix(dielectricF0, baseColor[2], metalness),
	}
	// Fresnel and kD at the primary light. The image-based terms at the end reuse
	// both, so a cubemap keeps the exact response it had before the light array
	// arrived. Each light in the loop below computes its own pair.
	fresnel := fresnelSchlick(f0, VdotH)
	kD := [3]float32{
		(1 - fresnel[0]) * (1 - metalness),
		(1 - fresnel[1]) * (1 - metalness),
		(1 - fresnel[2]) * (1 - metalness),
	}

	// Direct light: one Cook-Torrance lobe per scene light, summed.
	color := p.directLight(f, N, V, baseColor, f0, metalness, roughness, NdotV)

	// Ambient occlusion: sample the red channel (glTF 2.0 convention) and scale
	// the indirect terms only, matching litWGSL. Direct light and emissive stay
	// untouched.
	ao := float32(1)
	if m.hasOcclusionMap {
		ao = clamp01f(m.occlusionMap.sampleRGB(f.uv)[0])
	}

	// Environment ambient: three independent terms, each gated by its own
	// intensity only. The sky and ground intensities arrive premultiplied into
	// the colour from resolveHemisphereAmbient in render/bundle/renderer.go.
	hemi := clamp01f(N[1]*0.5 + 0.5)
	var ambient [3]float32
	for i := 0; i < 3; i++ {
		envDiffuse := l.ambientColor[i]*l.ambientColor[3] +
			l.skyColor[i]*hemi + l.groundColor[i]*(1-hemi)
		ambient[i] = envDiffuse * baseColor[i]
		color[i] += ambient[i] * ao
	}

	// Image-based lighting. envParams.z is zero for a scene with no environment
	// map, and then this whole block contributes nothing.
	if l.envParams[2] != 0 && l.envCube.bound() {
		rotation := l.envParams[1]
		diffuseEnv := l.envCube.sampleCube(rotateEnvY(N, rotation))
		reflected := reflect3(V, N)
		specularEnv := l.envCube.sampleCube(rotateEnvY(reflected, rotation))
		gain := l.envParams[0] * l.envParams[2]
		roughFade := 1 - roughness*envSpecularRoughFade
		for i := 0; i < 3; i++ {
			cubeDiffuse := diffuseEnv[i] * baseColor[i] * kD[i]
			cubeSpecular := specularEnv[i] * fresnel[i] * roughFade
			color[i] += (cubeDiffuse + cubeSpecular) * gain * ao
		}
	}

	// Emissive. The map replaces the emissive colour; it does not tint it. The
	// term is added after the light, so an unlit face still glows.
	if m.emissiveScale != 0 {
		emissiveColor := baseColor
		if m.hasEmissiveMap {
			emissiveColor = m.emissiveMap.sampleRGB(f.uv)
		}
		for i := 0; i < 3; i++ {
			color[i] += emissiveColor[i] * m.emissiveScale
		}
	}

	// Clear coat: a second, tighter lobe over the whole surface.
	if clearcoat := clamp01f(m.clearcoat); clearcoat > 0 {
		power := mix(clearcoatPowerLow, clearcoatPowerHigh, 1-roughness)
		lobe := float32(math.Pow(float64(NdotV), float64(power))) * clearcoat * clearcoatGain
		color[0] += lobe
		color[1] += lobe
		color[2] += lobe
	}

	// Sheen: the velvet edge of a fabric, added rather than blended.
	if sheen := clamp01f(m.sheen); sheen > 0 {
		facing := 1 - NdotV
		velvet := facing * facing * facing * sheen
		for i := 0; i < 3; i++ {
			color[i] += baseColor[i] * velvet * sheenGain
		}
	}

	// Iridescence: a thin-film hue sweep that grows toward the silhouette.
	if iridescence := clamp01f(m.iridescence); iridescence > 0 {
		facing := 1 - NdotV
		weight := iridescence * facing * facing
		phases := [3]float32{0, iridescencePhaseGreen, iridescencePhaseBlue}
		for i := 0; i < 3; i++ {
			iri := 0.5 + 0.5*float32(math.Cos(float64(phases[i]+NdotV*iridescenceFrequency)))
			color[i] = mix(color[i], color[i]*(iridescenceTintBase+iri*iridescenceTintGain), weight)
		}
	}

	// Transmission. This is the same single-pixel approximation litWGSL and both
	// browser renderers use: fade the shaded colour toward the ambient term plus
	// a tenth of the base colour. None of the four renderers refracts, because
	// refraction needs a second pass over the whole scene.
	if transmission := clamp01f(m.transmission) * (1 - metalness); transmission > 0 {
		weight := transmission * transmissionMixGain
		for i := 0; i < 3; i++ {
			color[i] = mix(color[i], ambient[i]+baseColor[i]*transmissionBaseGain, weight)
		}
	}

	// Exponential-squared fog, applied last, mirroring litWGSL: both browser
	// renderers fog before exposure/tone-mapping, and this shader has none, so
	// the last step before output is the equivalent point.
	if l.fogParams[3] > 0 {
		dist := length3(subVec3(f.world, l.cameraPos))
		fogFactor := float32(math.Exp(float64(-l.fogParams[3] * l.fogParams[3] * dist * dist)))
		fogFactor = clamp01f(fogFactor)
		for i := 0; i < 3; i++ {
			color[i] = mix(l.fogParams[i], color[i], fogFactor)
		}
	}
	return color
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
	// The shadow map holds depth in the zero-to-one range, so the reference
	// depth has to travel through the same mapping the raster write uses.
	bias := float32(0.003 + 0.003*float32(cascade))
	return sampleShadowCompare(l.shadow, layer, u, v, ndcToDepth(proj[2])-bias)
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

// readInstancePickID reads the stable pick ID an instance record carries in the
// first u32 after its model matrix. Returns 0 when the buffer is absent or the
// record has no room for the pick tail, which means "background" downstream.
func readInstancePickID(buf *Buffer, index, stride int) uint32 {
	if buf == nil || index < 0 || stride < instanceRecordStride {
		return 0
	}
	offset := index*stride + 64
	if offset < 0 || offset+4 > len(buf.data) {
		return 0
	}
	return binary.LittleEndian.Uint32(buf.data[offset : offset+4])
}

func readMat4Stride(buf *Buffer, index, stride int) ([16]float32, bool) {
	var out [16]float32
	if buf == nil || index < 0 || stride < 64 {
		return out, false
	}
	offset := index * stride
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

// rasterVertex is one screen-space triangle corner: pixel position, normalized
// depth, the reciprocal of the clip w, and the attributes the rasterizer
// interpolates. invW drives perspective-correct interpolation.
type rasterVertex struct {
	x, y   float32
	depth  float32
	invW   float32
	color  [4]float32
	world  [3]float32
	normal [3]float32
	uv     [2]float32
}

// rasterizeTriangle fills one screen-space triangle.
//
// When shading is non-nil the fill evaluates the lighting model at every pixel,
// exactly as litWGSL evaluates it in its fragment stage. The rasterizer used to
// shade at the three corners and interpolate the result, which meant a shadow
// only appeared where it happened to land on a vertex. A ground plane has four
// corners, so no shadow could ever be seen on one, and the headless backend
// reported an evenly lit plane whatever the shadow map held.
//
// Interpolation is perspective correct: attributes are carried as attr/w, the
// reciprocal 1/w is carried alongside, and the divide happens per pixel. Screen
// linear interpolation is close enough on a small triangle but wildly wrong on
// a ground plane running to the horizon, which is precisely the surface a
// shadow lands on.
func rasterizeTriangle(target rasterTarget, verts [3]rasterVertex, shading *litProgram) {
	// The texture-space basis is a property of the triangle, so solve it once
	// outside the pixel loop, and only when a normal map is bound.
	var frame tangentFrame
	if shading != nil && shading.material.hasNormalMap {
		frame = triangleTangent(verts)
	}
	pts := [3][2]float32{
		{verts[0].x, verts[0].y},
		{verts[1].x, verts[1].y},
		{verts[2].x, verts[2].y},
	}
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
			depth := verts[0].depth*w0 + verts[1].depth*w1 + verts[2].depth*w2
			if !depthPasses(target, x, y, depth) {
				continue
			}
			// Perspective-correct weights. invSum is the interpolated 1/w.
			invSum := verts[0].invW*w0 + verts[1].invW*w1 + verts[2].invW*w2
			p0, p1, p2 := w0, w1, w2
			if invSum > 0 {
				p0 = verts[0].invW * w0 / invSum
				p1 = verts[1].invW * w1 / invSum
				p2 = verts[2].invW * w2 / invSum
			}
			// Wireframe: p0/p1/p2 ARE the perspective-correct barycentric
			// weights litWGSL's in.bary carries (vs_main writes the same
			// (1,0,0)/(0,1,0)/(0,0,1) corners; default WGSL interpolation is
			// perspective-correct). A software rasterizer walks one pixel at a
			// time and has no 2x2 quad to take fwidth(d) from, so this oracle
			// uses a fixed barycentric-space band instead of a
			// screen-space-constant antialiased one. That is a recorded
			// approximation, not a bug: the measured floor in
			// TestPhysicallyBasedMaterialFieldsReachThePixels proves the term
			// reaches a pixel; it does not claim the CPU and GPU wireframes are
			// antialiased identically.
			if shading != nil && shading.material.hasWireframe {
				const wireframeEdgeBand = 0.06
				if min3(p0, p1, p2) > wireframeEdgeBand {
					continue
				}
			}
			var out [4]float32
			for i := 0; i < 4; i++ {
				out[i] = verts[0].color[i]*p0 + verts[1].color[i]*p1 + verts[2].color[i]*p2
			}
			if shading != nil {
				frag := fragment{
					base:  [3]float32{out[0], out[1], out[2]},
					frame: frame,
				}
				for i := 0; i < 3; i++ {
					frag.world[i] = verts[0].world[i]*p0 + verts[1].world[i]*p1 + verts[2].world[i]*p2
					frag.normal[i] = verts[0].normal[i]*p0 + verts[1].normal[i]*p1 + verts[2].normal[i]*p2
				}
				for i := 0; i < 2; i++ {
					frag.uv[i] = verts[0].uv[i]*p0 + verts[1].uv[i]*p1 + verts[2].uv[i]*p2
				}
				lit := shading.shade(frag)
				out[0], out[1], out[2] = lit[0], lit[1], lit[2]
			}
			writeRasterColor(target, x, y, color.RGBA{
				R: clampByte(out[0]),
				G: clampByte(out[1]),
				B: clampByte(out[2]),
				A: clampByte(out[3]),
			})
			if target.id != nil {
				writeTextureUint32(target.id, target.idLayer, x, y, target.pickID)
			}
			if target.depthWrite {
				writeDepth(target.depth, target.depthLayer, x, y, ndcToDepth(depth))
			}
		}
	}
}

// ndcToDepth maps a clip-space depth into the zero-to-one range a depth texture
// holds. mat4Perspective and mat4Orthographic in the bundle package both put
// clip depth between -1 and 1, and a depth texture stores no negative value.
//
// The rasterizer used to store the clip depth directly. writeDepth clamps to
// zero and one, so every sample in the near half of a light volume landed on
// zero, and a shadow comparison against zero always reported "lit". No shadow
// could ever darken a headless image, whatever the shadow map held.
func ndcToDepth(ndc float32) float32 {
	return clamp01f(ndc*0.5 + 0.5)
}

func depthPasses(target rasterTarget, x, y int, depth float32) bool {
	if target.depth == nil {
		return true
	}
	if pointOutsideClip(depth) {
		return false
	}
	value := ndcToDepth(depth)
	stored := readDepth(target.depth, target.depthLayer, x, y)
	switch target.depthCompare {
	case gpu.CompareAlways:
		return true
	case gpu.CompareNever:
		return false
	case gpu.CompareLess:
		return value < stored
	case gpu.CompareLessEqual:
		return value <= stored
	case gpu.CompareEqual:
		return math.Abs(float64(value-stored)) <= 1e-6
	case gpu.CompareGreater:
		return value > stored
	case gpu.CompareGreaterEqual:
		return value >= stored
	case gpu.CompareNotEqual:
		return math.Abs(float64(value-stored)) > 1e-6
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
	// render/bundle binds a one-texel fallback in every unused map slot, so this
	// case is the common one. Bilinear filtering of one texel costs four reads
	// and three blends and returns that same texel.
	if t.width == 1 && t.height == 1 {
		return sampleTexturePixelRGB(t, layer, 0, 0)
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
	case gpu.FormatRGB9E5Ufloat, gpu.FormatRGB10A2Unorm:
		return unpackRGB10A2(binary.LittleEndian.Uint32(t.data[dataOff : dataOff+4]))
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
	case gpu.FormatRGB9E5Ufloat, gpu.FormatRGB10A2Unorm:
		binary.LittleEndian.PutUint32(t.data[dataOff:dataOff+4], packRGB10A2(col.R, col.G, col.B, col.A))
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

// reflect3 mirrors the view vector about the normal and returns the direction a
// cubemap lookup needs. litWGSL writes reflect(-V, N); this is the same vector.
func reflect3(v, n [3]float32) [3]float32 {
	d := 2 * dotVec3(n, v)
	return [3]float32{d*n[0] - v[0], d*n[1] - v[1], d*n[2] - v[2]}
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func clampRange(v, low, high float32) float32 {
	if math.IsNaN(float64(v)) || v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

func clampSignedUnit(v float32) float32 { return clampRange(v, -1, 1) }

// readFloat32At reads one float and returns zero when the offset falls outside
// the buffer. A unit test binds a uniform buffer shorter than the shader struct
// on purpose, and a short buffer must read as zero rather than panic.
func readFloat32At(data []byte, offset int) float32 {
	if offset < 0 || offset+4 > len(data) {
		return 0
	}
	return readFloat32(data, offset)
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

func packRGB10A2(r, g, b, a uint8) uint32 {
	rr := uint32(r) * 1023 / 255
	gg := uint32(g) * 1023 / 255
	bb := uint32(b) * 1023 / 255
	aa := uint32(a) * 3 / 255
	return rr | (gg << 10) | (bb << 20) | (aa << 30)
}

func unpackRGB10A2(v uint32) color.RGBA {
	return color.RGBA{
		R: uint8((v & 0x3ff) * 255 / 1023),
		G: uint8(((v >> 10) & 0x3ff) * 255 / 1023),
		B: uint8(((v >> 20) & 0x3ff) * 255 / 1023),
		A: uint8(((v >> 30) & 0x3) * 255 / 3),
	}
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
	case gpu.FormatRGB9E5Ufloat, gpu.FormatRGB10A2Unorm:
		for i := 0; i+3 < len(data) && i+3 < len(rgba); i += 4 {
			col := unpackRGB10A2(binary.LittleEndian.Uint32(data[i : i+4]))
			rgba[i+0] = col.R
			rgba[i+1] = col.G
			rgba[i+2] = col.B
			rgba[i+3] = col.A
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
		gpu.FormatRGB9E5Ufloat, gpu.FormatRGB10A2Unorm,
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
