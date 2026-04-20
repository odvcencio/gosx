package bundle

import (
	"fmt"
	"math"

	"github.com/odvcencio/gosx/render/gpu"
)

// cullWGSL is the R3 frustum-culling compute shader. One thread per instance
// tests the instance's bounding sphere (center from the transform's
// translation column; radius from a conservative constant) against the six
// camera frustum planes. Visible instances are appended to a compacted
// output buffer; the indirect-draw args buffer's instanceCount field is
// atomically bumped so the main pass picks up only the visible count.
//
// This is intentionally per-instance O(N) — a compute shader running 10k
// threads on any WebGPU-capable GPU completes in well under a millisecond.
// BVH / per-cluster culling is R4.
const cullWGSL = `
struct CullUniforms {
  planes    : array<vec4<f32>, 6>,
  vertexCount : u32,
  radius    : f32,
  _pad0     : vec2<f32>,
};

@group(0) @binding(0) var<uniform> cull      : CullUniforms;
@group(0) @binding(1) var<storage, read>         input : array<mat4x4<f32>>;
@group(0) @binding(2) var<storage, read_write>   output : array<mat4x4<f32>>;
@group(0) @binding(3) var<storage, read_write>   drawArgs : array<atomic<u32>, 4>;

@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  if (i >= arrayLength(&input)) { return; }
  let m = input[i];
  // Translation column of a column-major mat4 lives at m[3].xyz.
  let center = m[3].xyz;
  var inside = true;
  for (var p : i32 = 0; p < 6; p = p + 1) {
    let plane = cull.planes[p];
    let d = dot(plane.xyz, center) + plane.w;
    if (d < -cull.radius) {
      inside = false;
      break;
    }
  }
  if (inside) {
    let slot = atomicAdd(&drawArgs[1], 1u);
    output[slot] = m;
  }
}
`

// cullResources are the per-InstancedMesh GPU resources for culling: input
// storage, output storage (bound as vertex for the main pass), and the
// indirect-draw args buffer that the main pass reads via DrawIndirect.
type cullResources struct {
	capacity     int
	cullUniform  gpu.Buffer
	inputBuf     gpu.Buffer
	outputBuf    gpu.Buffer
	drawArgsBuf  gpu.Buffer
	bindGroup    gpu.BindGroup
}

// ensureCullResources grows or lazily creates the cull buffers for a given
// (cache key, max instance count) pair. Resources never shrink.
func (r *Renderer) ensureCullResources(key string, instanceCount int) (*cullResources, error) {
	res, ok := r.cullCache[key]
	if ok && res.capacity >= instanceCount {
		return res, nil
	}
	// Grow geometrically so we don't reallocate on every frame when instance
	// counts drift upward.
	newCap := max(32, instanceCount+instanceCount/4)
	if res != nil {
		destroyCullResources(res)
	}

	bufBytes := newCap * 64 // mat4 per instance

	inputBuf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  bufBytes,
		Usage: gpu.BufferUsageStorage | gpu.BufferUsageCopyDst,
		Label: "bundle.cull.input:" + key,
	})
	if err != nil {
		return nil, fmt.Errorf("bundle.ensureCullResources: %w", err)
	}
	outputBuf, err := r.device.CreateBuffer(gpu.BufferDesc{
		// Storage + vertex: the compute shader writes, the main pass reads.
		Size:  bufBytes,
		Usage: gpu.BufferUsageStorage | gpu.BufferUsageVertex | gpu.BufferUsageCopyDst,
		Label: "bundle.cull.output:" + key,
	})
	if err != nil {
		inputBuf.Destroy()
		return nil, fmt.Errorf("bundle.ensureCullResources: %w", err)
	}
	drawArgsBuf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  16, // 4×u32
		Usage: gpu.BufferUsageStorage | gpu.BufferUsageIndirect | gpu.BufferUsageCopyDst,
		Label: "bundle.cull.drawArgs:" + key,
	})
	if err != nil {
		inputBuf.Destroy()
		outputBuf.Destroy()
		return nil, fmt.Errorf("bundle.ensureCullResources: %w", err)
	}
	cullUniform, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  cullUniformSize,
		Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
		Label: "bundle.cull.uniforms:" + key,
	})
	if err != nil {
		inputBuf.Destroy()
		outputBuf.Destroy()
		drawArgsBuf.Destroy()
		return nil, fmt.Errorf("bundle.ensureCullResources: %w", err)
	}

	bg, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: r.cullBGLayout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: cullUniform, Size: cullUniformSize},
			{Binding: 1, Buffer: inputBuf, Size: bufBytes},
			{Binding: 2, Buffer: outputBuf, Size: bufBytes},
			{Binding: 3, Buffer: drawArgsBuf, Size: 16},
		},
		Label: "bundle.cull.bindgroup:" + key,
	})
	if err != nil {
		inputBuf.Destroy()
		outputBuf.Destroy()
		drawArgsBuf.Destroy()
		cullUniform.Destroy()
		return nil, fmt.Errorf("bundle.ensureCullResources: %w", err)
	}

	new := &cullResources{
		capacity:    newCap,
		cullUniform: cullUniform,
		inputBuf:    inputBuf,
		outputBuf:   outputBuf,
		drawArgsBuf: drawArgsBuf,
		bindGroup:   bg,
	}
	r.cullCache[key] = new
	return new, nil
}

func destroyCullResources(c *cullResources) {
	if c == nil {
		return
	}
	if c.inputBuf != nil {
		c.inputBuf.Destroy()
	}
	if c.outputBuf != nil {
		c.outputBuf.Destroy()
	}
	if c.drawArgsBuf != nil {
		c.drawArgsBuf.Destroy()
	}
	if c.cullUniform != nil {
		c.cullUniform.Destroy()
	}
}

// buildCullPipeline constructs the frustum-culling compute pipeline.
func (r *Renderer) buildCullPipeline() error {
	shader, err := r.device.CreateShaderModule(gpu.ShaderDesc{
		SourceWGSL: cullWGSL,
		Label:      "bundle.cull",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildCullPipeline: %w", err)
	}
	pipeline, err := r.device.CreateComputePipeline(gpu.ComputePipelineDesc{
		Module:     shader,
		EntryPoint: "main",
		AutoLayout: true,
		Label:      "bundle.cull",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildCullPipeline: %w", err)
	}
	r.cullPipeline = pipeline
	r.cullBGLayout = pipeline.GetBindGroupLayout(0)
	return nil
}

// cullUniformSize is the layout size of the CullUniforms struct in WGSL.
// 6 vec4 planes (96) + vertexCount + radius + 2 padding floats = 112 bytes.
const cullUniformSize = 112

// cullUniformBytes packs cull-shader inputs into std140-friendly bytes.
func cullUniformBytes(planes [6][4]float32, vertexCount uint32, radius float32) []byte {
	out := make([]byte, cullUniformSize)
	for i := 0; i < 6; i++ {
		copy(out[i*16:(i+1)*16], float32sToBytes(planes[i][:]))
	}
	// vertexCount (u32) + radius (f32) + 2 padding floats.
	put32 := func(off int, v uint32) {
		out[off+0] = byte(v)
		out[off+1] = byte(v >> 8)
		out[off+2] = byte(v >> 16)
		out[off+3] = byte(v >> 24)
	}
	put32(96, vertexCount)
	copy(out[100:104], float32sToBytes([]float32{radius}))
	// Trailing 8 bytes of pad are already zero.
	return out
}

// extractFrustumPlanes returns the 6 world-space frustum planes derived from
// a view-projection matrix via the Gribb-Hartmann method. Planes are
// normalized and stored as (nx, ny, nz, d) where the half-space n·p + d ≥ 0
// means "inside the frustum".
func extractFrustumPlanes(vp mat4) [6][4]float32 {
	// Column-major mat4: vp[col*4+row]. Row i is (vp[0*4+i], vp[1*4+i],
	// vp[2*4+i], vp[3*4+i]).
	row := func(r int) [4]float32 {
		return [4]float32{vp[0*4+r], vp[1*4+r], vp[2*4+r], vp[3*4+r]}
	}
	r0, r1, r2, r3 := row(0), row(1), row(2), row(3)

	planes := [6][4]float32{
		addRow(r3, r0),  // left:   r3 + r0
		subRow(r3, r0),  // right:  r3 - r0
		addRow(r3, r1),  // bottom: r3 + r1
		subRow(r3, r1),  // top:    r3 - r1
		r2,              // near:   r2
		subRow(r3, r2),  // far:    r3 - r2
	}
	for i := range planes {
		planes[i] = normalizePlane(planes[i])
	}
	return planes
}

func addRow(a, b [4]float32) [4]float32 {
	return [4]float32{a[0] + b[0], a[1] + b[1], a[2] + b[2], a[3] + b[3]}
}

func subRow(a, b [4]float32) [4]float32 {
	return [4]float32{a[0] - b[0], a[1] - b[1], a[2] - b[2], a[3] - b[3]}
}

func normalizePlane(p [4]float32) [4]float32 {
	l := float32(math.Sqrt(float64(p[0]*p[0] + p[1]*p[1] + p[2]*p[2])))
	if l == 0 {
		return p
	}
	return [4]float32{p[0] / l, p[1] / l, p[2] / l, p[3] / l}
}

// drawArgsResetBytes builds the 16-byte reset pattern for the indirect-draw
// args buffer: [vertexCount, 0, 0, 0] as 4×u32 little-endian.
func drawArgsResetBytes(vertexCount uint32) []byte {
	out := make([]byte, 16)
	put := func(off int, v uint32) {
		out[off+0] = byte(v)
		out[off+1] = byte(v >> 8)
		out[off+2] = byte(v >> 16)
		out[off+3] = byte(v >> 24)
	}
	put(0, vertexCount)
	// Other three u32 stay zero (instanceCount, firstVertex, firstInstance).
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
