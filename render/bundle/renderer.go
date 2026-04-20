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
	device         gpu.Device
	surface        gpu.Surface
	surfaceFormat  gpu.TextureFormat

	unlitPipeline  gpu.RenderPipeline
	unlitBGLayout  gpu.BindGroupLayout

	// Per-frame resources. Created lazily on first Frame and reused.
	uniformBuf     gpu.Buffer
	uniformBindGrp gpu.BindGroup

	// Vertex buffer cache, keyed by RenderPassBundle.CacheKey. Entries with
	// empty cache keys are not cached; they get a transient buffer each frame.
	passCache map[string]*passResources
}

// passResources holds the per-pass GPU buffers for a cached RenderPassBundle.
type passResources struct {
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

// New constructs a Renderer. It immediately creates the unlit pipeline and
// associated per-frame uniform resources.
func New(cfg Config) (*Renderer, error) {
	if cfg.Device == nil {
		return nil, errors.New("bundle.New: device is required")
	}
	if cfg.Surface == nil {
		return nil, errors.New("bundle.New: surface is required")
	}
	r := &Renderer{
		device:        cfg.Device,
		surface:       cfg.Surface,
		surfaceFormat: cfg.Device.PreferredSurfaceFormat(),
		passCache:     make(map[string]*passResources),
	}
	if err := r.buildUnlitPipeline(); err != nil {
		return nil, err
	}
	if err := r.buildUniforms(); err != nil {
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
	if r.uniformBuf != nil {
		r.uniformBuf.Destroy()
	}
	if r.unlitPipeline != nil {
		r.unlitPipeline.Destroy()
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
		Label: "bundle.unlit",
	})
	pass.SetPipeline(r.unlitPipeline)
	pass.SetBindGroup(0, r.uniformBindGrp)

	// R1: iterate RenderPassBundle entries and draw with the unlit pipeline.
	// Instanced meshes and finer-grained material dispatch land after this.
	for _, pb := range b.Passes {
		res, err := r.ensurePassBuffers(pb)
		if err != nil {
			return err
		}
		if res == nil || res.vertexCount == 0 {
			continue
		}
		pass.SetVertexBuffer(0, res.positions)
		pass.SetVertexBuffer(1, res.colors)
		pass.Draw(res.vertexCount, 1, 0, 0)
	}

	pass.End()
	r.device.Queue().Submit(enc.Finish())
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

func (r *Renderer) buildUniforms() error {
	buf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  64, // single mat4
		Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
		Label: "bundle.unlit.uniforms",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildUniforms: %w", err)
	}
	bg, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: r.unlitBGLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: buf, Offset: 0, Size: 64},
		},
		Label: "bundle.unlit.uniforms",
	})
	if err != nil {
		buf.Destroy()
		return fmt.Errorf("bundle.buildUniforms: %w", err)
	}
	r.uniformBuf = buf
	r.uniformBindGrp = bg
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
