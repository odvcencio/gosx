package bundle

import (
	"encoding/binary"
	"fmt"
	"math"

	"m31labs.dev/gosx/render/gpu"
)

// cullWGSL is the frustum-culling compute shader. One thread per instance
// tests the instance's bounding sphere against the six frustum planes. The
// sphere centre comes from the transform's translation column; the radius is
// the primitive's unscaled radius multiplied by the largest axis scale the
// transform carries. Visible instances are appended to a compacted output
// buffer; the indirect-draw args buffer's instanceCount field is atomically
// bumped, so the draw pass picks up only the visible count.
//
// Scaling the radius per thread costs three lengths and two comparisons and
// fixes a correctness bug: with a constant radius an instance scaled up 10x
// vanished while still on screen. instanceCullRadius in primitive.go is the
// CPU oracle for the same calculation — keep the two in step.
//
// This is intentionally per-instance O(N). A compute shader running 100k
// threads on any WebGPU-capable GPU finishes in well under a millisecond.
//
// A cluster hierarchy is not the next scaling step, and BenchmarkFrameNull in
// bench_frame_test.go says why. Hold the total instance count at 20000 and move
// it between meshes: one mesh costs about 86 microseconds of renderer CPU and a
// thousand meshes cost about 287. One extra mesh costs as much as fifty extra
// instances. The renderer spends nothing per instance on culling — the GPU does
// that — and about 4 nanoseconds per instance on the change-detection hash that
// lets a static mesh skip its upload.
//
// A cluster hierarchy would add a CPU-side build, a second dispatch, and a
// rebuild whenever an instance moves. It would attack the cheap axis and make
// the expensive one worse. Per-mesh work is the axis to attack. The first cut
// was the material fingerprint memo in material.go, worth about 19 percent of a
// thousand-mesh frame. Re-run BenchmarkFrameNull1000Mesh1InstanceEach with a
// profile before adding anything here.
//
// The thread guard bounds on the live instance count, not on the buffer length.
// The input buffer's capacity runs 25 percent past the instance count and the
// dispatch rounds the thread count up to a multiple of 64, so threads run past
// the live records. WebGPU zero-initializes a buffer, so such a thread reads an
// all-zero matrix. Its centre is the origin and its scale is zero, so it keeps
// the base radius, passes the frustum test, and compacts a degenerate record
// into the output. That record rasterizes nothing, but it inflates the survivor
// count the telemetry reports and it costs vertex shading.
//
// The live count rides in the uniform lane this struct used to leave as
// padding. SCENE_INSTANCED_CULL_BUILTIN_WGSL in
// client/js/bootstrap-src/16b-scene-compute.js declared the field first and
// recorded the difference in prose. TestCullWGSLMatchesJSCompute now
// pins the agreement instead.
const cullWGSL = `
struct CullUniforms {
  planes    : array<vec4<f32>, 6>,
  vertexCount : u32,
  radius    : f32,
  instanceCount : u32,
  _pad1     : u32,
};

struct InstanceRecord {
  model    : mat4x4<f32>,
  pickData : vec4<u32>,
};

@group(0) @binding(0) var<uniform> cull      : CullUniforms;
@group(0) @binding(1) var<storage, read>         input : array<InstanceRecord>;
@group(0) @binding(2) var<storage, read_write>   output : array<InstanceRecord>;
@group(0) @binding(3) var<storage, read_write>   drawArgs : array<atomic<u32>, 4>;

@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  if (i >= min(cull.instanceCount, arrayLength(&input))) { return; }
  let record = input[i];
  let m = record.model;
  // Translation column of a column-major mat4 lives at m[3].xyz.
  let center = m[3].xyz;
  // Largest axis scale of the upper-left 3x3. The maximum never
  // under-estimates the bounding sphere, so no visible instance is dropped.
  let scale = max(length(m[0].xyz), max(length(m[1].xyz), length(m[2].xyz)));
  var radius = cull.radius;
  if (scale > 0.0) {
    radius = radius * scale;
  }
  var inside = true;
  for (var p : i32 = 0; p < 6; p = p + 1) {
    let plane = cull.planes[p];
    let d = dot(plane.xyz, center) + plane.w;
    if (d < -radius) {
      inside = false;
      break;
    }
  }
  if (inside) {
    let slot = atomicAdd(&drawArgs[1], 1u);
    output[slot] = record;
  }
}
`

const instanceRecordStride = 80

// cullResources are the per-InstancedMesh GPU resources for culling: input
// storage, output storage (bound as vertex for the main pass), and the
// indirect-draw args buffer that the main pass reads via DrawIndirect.
type cullResources struct {
	capacity    int
	cullUniform gpu.Buffer
	inputBuf    gpu.Buffer
	outputBuf   gpu.Buffer
	drawArgsBuf gpu.Buffer
	bindGroup   gpu.BindGroup

	// uploadedHash fingerprints the instance records last written into
	// inputBuf. recordCullPass skips the re-encode and the upload when the
	// incoming transforms hash to the same value. haveUpload guards the first
	// frame, and a grown resource set starts with haveUpload false because
	// ensureCullResources builds a fresh struct.
	uploadedHash  uint64
	uploadedCount int
	haveUpload    bool

	// casters hold the per-cascade shadow-caster cull outputs. They are built
	// only for meshes that cast shadows. Each shares inputBuf and owns its own
	// compacted output plus indirect draw args.
	casters [cascadeCount]*casterCullResources
}

// casterCullResources is one cascade's shadow-caster cull output for one
// InstancedMesh. The shadow pass used to bind the unculled input buffer and
// draw every instance into all three cascades. With these, each cascade draws
// only the casters whose bounding spheres reach its light volume.
type casterCullResources struct {
	cullUniform gpu.Buffer
	outputBuf   gpu.Buffer
	drawArgsBuf gpu.Buffer
	bindGroup   gpu.BindGroup
}

func destroyCasterCullResources(c *casterCullResources) {
	if c == nil {
		return
	}
	if c.cullUniform != nil {
		c.cullUniform.Destroy()
	}
	if c.outputBuf != nil {
		c.outputBuf.Destroy()
	}
	if c.drawArgsBuf != nil {
		c.drawArgsBuf.Destroy()
	}
}

// ensureCullResources grows or lazily creates the cull buffers for a given
// (cache key, max instance count) pair. Resources never shrink. When
// needCasters is true the per-cascade shadow-caster outputs are built too.
func (r *Renderer) ensureCullResources(key string, instanceCount int, needCasters bool) (*cullResources, error) {
	res, ok := r.cullCache[key]
	if ok && res.capacity >= instanceCount {
		if needCasters && res.casters[0] == nil {
			if err := r.buildCasterCullResources(res, key); err != nil {
				return nil, err
			}
		}
		return res, nil
	}
	// Grow geometrically so we don't reallocate on every frame when instance
	// counts drift upward.
	newCap := max(32, instanceCount+instanceCount/4)
	if res != nil {
		destroyCullResources(res)
	}

	bufBytes := newCap * instanceRecordStride // mat4 + pick metadata per instance

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

	fresh := &cullResources{
		capacity:    newCap,
		cullUniform: cullUniform,
		inputBuf:    inputBuf,
		outputBuf:   outputBuf,
		drawArgsBuf: drawArgsBuf,
		bindGroup:   bg,
	}
	r.cullCache[key] = fresh
	if needCasters {
		if err := r.buildCasterCullResources(fresh, key); err != nil {
			return nil, err
		}
	}
	return fresh, nil
}

// buildCasterCullResources creates one cull output set per shadow cascade for
// the mesh described by res. Every set reads the mesh's shared instance input
// buffer and writes its own compacted output plus indirect draw args.
func (r *Renderer) buildCasterCullResources(res *cullResources, key string) error {
	bufBytes := res.capacity * instanceRecordStride
	for cascade := 0; cascade < cascadeCount; cascade++ {
		label := fmt.Sprintf("%s#cascade%d", key, cascade)
		outputBuf, err := r.device.CreateBuffer(gpu.BufferDesc{
			Size:  bufBytes,
			Usage: gpu.BufferUsageStorage | gpu.BufferUsageVertex | gpu.BufferUsageCopyDst,
			Label: "bundle.cull.casterOutput:" + label,
		})
		if err != nil {
			return fmt.Errorf("bundle.buildCasterCullResources: %w", err)
		}
		drawArgsBuf, err := r.device.CreateBuffer(gpu.BufferDesc{
			Size:  16, // 4×u32
			Usage: gpu.BufferUsageStorage | gpu.BufferUsageIndirect | gpu.BufferUsageCopyDst,
			Label: "bundle.cull.casterDrawArgs:" + label,
		})
		if err != nil {
			outputBuf.Destroy()
			return fmt.Errorf("bundle.buildCasterCullResources: %w", err)
		}
		cullUniform, err := r.device.CreateBuffer(gpu.BufferDesc{
			Size:  cullUniformSize,
			Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
			Label: "bundle.cull.casterUniforms:" + label,
		})
		if err != nil {
			outputBuf.Destroy()
			drawArgsBuf.Destroy()
			return fmt.Errorf("bundle.buildCasterCullResources: %w", err)
		}
		bg, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
			Layout: r.cullBGLayout,
			Entries: []gpu.BindGroupEntry{
				{Binding: 0, Buffer: cullUniform, Size: cullUniformSize},
				{Binding: 1, Buffer: res.inputBuf, Size: bufBytes},
				{Binding: 2, Buffer: outputBuf, Size: bufBytes},
				{Binding: 3, Buffer: drawArgsBuf, Size: 16},
			},
			Label: "bundle.cull.casterBindgroup:" + label,
		})
		if err != nil {
			outputBuf.Destroy()
			drawArgsBuf.Destroy()
			cullUniform.Destroy()
			return fmt.Errorf("bundle.buildCasterCullResources: %w", err)
		}
		res.casters[cascade] = &casterCullResources{
			cullUniform: cullUniform,
			outputBuf:   outputBuf,
			drawArgsBuf: drawArgsBuf,
			bindGroup:   bg,
		}
	}
	return nil
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
	for _, caster := range c.casters {
		destroyCasterCullResources(caster)
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
// 6 vec4 planes (96) + vertexCount + radius + instanceCount + 1 pad = 112 bytes.
const cullUniformSize = 112

// cullUniformInstanceCountOffset is the byte offset of the instanceCount lane
// inside CullUniforms. The CPU oracle in render/gpu/headless reads the same
// offset, so keep the two in step.
const cullUniformInstanceCountOffset = 104

// cullUnboundedInstanceCount is the value that means "no live count supplied".
// The shader takes min(instanceCount, arrayLength(&input)), so the all-ones
// value selects arrayLength and reproduces the behaviour of the shader before
// the instanceCount lane existed.
const cullUnboundedInstanceCount = uint32(0xffffffff)

// cullUniformBytes packs cull-shader inputs into a Renderer-owned buffer and
// returns it. WriteBuffer copies the bytes, so reusing one buffer per frame is
// safe and keeps the per-frame allocation count flat.
//
// instanceCount is the live instance count of the mesh. Supply it. The shader
// bounds its threads on min(instanceCount, arrayLength(&input)), so a supplied
// count stops the dispatch from compacting the zero-matrix records that sit
// past the live data in an over-allocated input buffer.
//
// The parameter is optional only because recordCullPass in renderer.go still
// calls the three-argument form, and that file belongs to another change in
// flight. A call that omits the count writes cullUnboundedInstanceCount, which
// reproduces the old behaviour exactly. The one-line caller change is:
//
//	r.cullUniformBytes(frustum, uint32(st.vertexCount), st.radius, uint32(st.instanceCount))
//
// TestCullUniformCarriesLiveInstanceCount pins both forms.
func (r *Renderer) cullUniformBytes(planes [6][4]float32, vertexCount uint32, radius float32, instanceCount ...uint32) []byte {
	out := r.cullUniformScratch[:]
	for i := 0; i < 6; i++ {
		putFloat32s(out[i*16:(i+1)*16], planes[i][:])
	}
	binary.LittleEndian.PutUint32(out[96:100], vertexCount)
	binary.LittleEndian.PutUint32(out[100:104], math.Float32bits(radius))
	live := cullUnboundedInstanceCount
	if len(instanceCount) > 0 {
		live = instanceCount[0]
	}
	binary.LittleEndian.PutUint32(out[cullUniformInstanceCountOffset:108], live)
	// The last four bytes are pad and stay zero.
	binary.LittleEndian.PutUint32(out[108:112], 0)
	return out
}

// extractFrustumPlanes returns the 6 world-space frustum planes derived from
// a view-projection matrix via the Gribb-Hartmann method. Planes are
// normalized and stored as (nx, ny, nz, d) where the half-space n·p + d ≥ 0
// means "inside the frustum".
//
// The near plane is r3 + r2, which is the form for a projection whose clip-space
// z runs from -1 to 1. Both mat4Perspective and mat4Orthographic in this package
// produce that range. The plain r2 form belongs to a 0-to-1 depth range; using it
// here put the near plane at the midpoint of an orthographic depth range and
// discarded half the volume. That made per-cascade shadow-caster culling reject
// every caster, because a cascade projection is orthographic.
func extractFrustumPlanes(vp mat4) [6][4]float32 {
	// Column-major mat4: vp[col*4+row]. Row i is (vp[0*4+i], vp[1*4+i],
	// vp[2*4+i], vp[3*4+i]).
	row := func(r int) [4]float32 {
		return [4]float32{vp[0*4+r], vp[1*4+r], vp[2*4+r], vp[3*4+r]}
	}
	r0, r1, r2, r3 := row(0), row(1), row(2), row(3)

	planes := [6][4]float32{
		addRow(r3, r0), // left:   r3 + r0
		subRow(r3, r0), // right:  r3 - r0
		addRow(r3, r1), // bottom: r3 + r1
		subRow(r3, r1), // top:    r3 - r1
		addRow(r3, r2), // near:   r3 + r2
		subRow(r3, r2), // far:    r3 - r2
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
// args buffer: [vertexCount, 0, 0, 0] as 4 little-endian u32. The bytes land in
// a Renderer-owned buffer that WriteBuffer copies, so no allocation happens.
func (r *Renderer) drawArgsResetBytes(vertexCount uint32) []byte {
	out := r.drawArgsScratch[:]
	binary.LittleEndian.PutUint32(out[0:4], vertexCount)
	// The other three u32 stay zero: instanceCount, firstVertex, firstInstance.
	binary.LittleEndian.PutUint32(out[4:8], 0)
	binary.LittleEndian.PutUint32(out[8:12], 0)
	binary.LittleEndian.PutUint32(out[12:16], 0)
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
