package headless

import (
	"encoding/binary"
	"testing"
	"time"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/bundle"
	"m31labs.dev/gosx/render/gpu"
)

// cullHarness wires up the four buffers the bundle.cull bind group carries, so
// a test can drive runCullFrustum through a real compute dispatch.
type cullHarness struct {
	device   *Device
	pipeline gpu.ComputePipeline
	uniform  gpu.Buffer
	input    gpu.Buffer
	output   gpu.Buffer
	drawArgs gpu.Buffer
	bind     gpu.BindGroup
}

const cullTestStride = 80

func newCullHarness(t *testing.T, capacity int) *cullHarness {
	t.Helper()
	d, _ := New(4, 4)
	pipeline, err := d.CreateComputePipeline(gpu.ComputePipelineDesc{Label: "bundle.cull"})
	if err != nil {
		t.Fatalf("CreateComputePipeline: %v", err)
	}
	newBuf := func(size int, usage gpu.BufferUsage) gpu.Buffer {
		buf, err := d.CreateBuffer(gpu.BufferDesc{Size: size, Usage: usage})
		if err != nil {
			t.Fatalf("CreateBuffer: %v", err)
		}
		return buf
	}
	h := &cullHarness{
		device:   d,
		pipeline: pipeline,
		uniform:  newBuf(112, gpu.BufferUsageUniform|gpu.BufferUsageCopyDst),
		input:    newBuf(capacity*cullTestStride, gpu.BufferUsageStorage|gpu.BufferUsageCopyDst),
		output:   newBuf(capacity*cullTestStride, gpu.BufferUsageStorage|gpu.BufferUsageCopyDst),
		drawArgs: newBuf(16, gpu.BufferUsageStorage|gpu.BufferUsageCopyDst),
	}
	bind, err := d.CreateBindGroup(gpu.BindGroupDesc{
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: h.uniform, Size: 112},
			{Binding: 1, Buffer: h.input, Size: capacity * cullTestStride},
			{Binding: 2, Buffer: h.output, Size: capacity * cullTestStride},
			{Binding: 3, Buffer: h.drawArgs, Size: 16},
		},
	})
	if err != nil {
		t.Fatalf("CreateBindGroup: %v", err)
	}
	h.bind = bind
	return h
}

// writeUniform packs the six frustum planes plus the unscaled radius, and
// leaves the live instance count unbounded. The renderer's three-argument call
// writes the same sentinel today.
func (h *cullHarness) writeUniform(planes [6][4]float32, radius float32) {
	h.writeUniformWithCount(planes, radius, cullUnboundedInstanceCount)
}

// writeUniformWithCount packs the whole uniform block, including the live
// instance count the kernel bounds its threads on.
func (h *cullHarness) writeUniformWithCount(planes [6][4]float32, radius float32, instanceCount uint32) {
	data := make([]byte, 112)
	for p := 0; p < 6; p++ {
		for c := 0; c < 4; c++ {
			binary.LittleEndian.PutUint32(data[p*16+c*4:], floatBits(planes[p][c]))
		}
	}
	binary.LittleEndian.PutUint32(data[96:100], 36) // vertexCount
	binary.LittleEndian.PutUint32(data[100:104], floatBits(radius))
	binary.LittleEndian.PutUint32(data[cullUniformInstanceCountOffset:cullUniformInstanceCountOffset+4], instanceCount)
	h.device.Queue().WriteBuffer(h.uniform, 0, data)
}

// writeInstances packs one record per model matrix, matching the renderer's
// 80-byte InstanceRecord layout.
func (h *cullHarness) writeInstances(models [][16]float32) {
	data := make([]byte, len(models)*cullTestStride)
	for i, m := range models {
		for j := 0; j < 16; j++ {
			binary.LittleEndian.PutUint32(data[i*cullTestStride+j*4:], floatBits(m[j]))
		}
		binary.LittleEndian.PutUint32(data[i*cullTestStride+64:], uint32(i+1))
	}
	h.device.Queue().WriteBuffer(h.input, 0, data)
	h.device.Queue().WriteBuffer(h.drawArgs, 0, []byte{36, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
}

// dispatch runs the cull and returns the survivor count plus each survivor's
// pick ID, in compacted order.
func (h *cullHarness) dispatch(t *testing.T, groups int) (int, []uint32) {
	t.Helper()
	enc := h.device.CreateCommandEncoder()
	pass := enc.BeginComputePass()
	pass.SetPipeline(h.pipeline)
	pass.SetBindGroup(0, h.bind)
	pass.DispatchWorkgroups(groups, 1, 1)
	pass.End()
	h.device.Queue().Submit(enc.Finish())

	args, err := h.drawArgs.ReadAsync(16)
	if err != nil {
		t.Fatalf("read draw args: %v", err)
	}
	count := int(binary.LittleEndian.Uint32(args[4:8]))
	out, err := h.output.ReadAsync(count * cullTestStride)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	ids := make([]uint32, count)
	for i := 0; i < count; i++ {
		ids[i] = binary.LittleEndian.Uint32(out[i*cullTestStride+64:])
	}
	return count, ids
}

func floatBits(f float32) uint32 {
	return binary.LittleEndian.Uint32(float32Bytes([]float32{f}))
}

// translatedModel returns a column-major identity matrix scaled by s and moved
// to (x, y, z).
func translatedModel(s, x, y, z float32) [16]float32 {
	return [16]float32{
		s, 0, 0, 0,
		0, s, 0, 0,
		0, 0, s, 0,
		x, y, z, 1,
	}
}

// unitBoxPlanes are six frustum planes forming the axis-aligned box
// [-1, 1] on every axis, with inward normals. n·p + d >= 0 means inside.
func unitBoxPlanes() [6][4]float32 {
	return [6][4]float32{
		{1, 0, 0, 1},  // x >= -1
		{-1, 0, 0, 1}, // x <=  1
		{0, 1, 0, 1},  // y >= -1
		{0, -1, 0, 1}, // y <=  1
		{0, 0, 1, 1},  // z >= -1
		{0, 0, -1, 1}, // z <=  1
	}
}

// TestCullDropsInstancesOutsideTheFrustum proves the headless cull is a real
// frustum test now, not the old pass-through. Without this the backend could not
// witness any GPU-driven culling behaviour.
func TestCullDropsInstancesOutsideTheFrustum(t *testing.T) {
	h := newCullHarness(t, 4)
	h.writeUniform(unitBoxPlanes(), 0.1)
	h.writeInstances([][16]float32{
		translatedModel(1, 0, 0, 0),   // inside
		translatedModel(1, 50, 0, 0),  // far outside +X
		translatedModel(1, 0, -50, 0), // far outside -Y
		translatedModel(1, 0.5, 0, 0), // inside
	})
	count, ids := h.dispatch(t, 1)
	if count != 2 {
		t.Fatalf("survivors = %d, want 2", count)
	}
	if ids[0] != 1 || ids[1] != 4 {
		t.Fatalf("compacted pick IDs = %v, want [1 4]", ids)
	}
}

// TestCullScalesRadiusByInstanceScale is the headless oracle for the per-instance
// scale fix. The instance centre sits outside the box, so a constant radius drops
// it; its 10x bounding sphere still reaches the box, so it must survive.
func TestCullScalesRadiusByInstanceScale(t *testing.T) {
	h := newCullHarness(t, 2)
	h.writeUniform(unitBoxPlanes(), 0.5)
	h.writeInstances([][16]float32{
		translatedModel(1, 4, 0, 0),  // radius 0.5, 3 units clear of the box
		translatedModel(10, 4, 0, 0), // radius 5.0, sphere crosses the box
	})
	count, ids := h.dispatch(t, 1)
	if count != 1 {
		t.Fatalf("survivors = %d, want 1", count)
	}
	if ids[0] != 2 {
		t.Fatalf("survivor pick ID = %d, want 2 (the scaled instance)", ids[0])
	}
}

// TestCullKeepsEverythingWithZeroPlanes keeps the degenerate plane path safe.
// A zero plane gives distance zero, and zero is never below a negative radius,
// so a caller that never wrote the planes still draws every instance.
//
// The live instance count is the one lane that must be written. Zero there means
// zero live instances, and TestCullDropsRecordsPastTheLiveInstanceCount covers
// that half.
func TestCullKeepsEverythingWithZeroPlanes(t *testing.T) {
	h := newCullHarness(t, 3)
	h.writeUniformWithCount([6][4]float32{}, 0, cullUnboundedInstanceCount)
	h.writeInstances([][16]float32{
		translatedModel(1, 0, 0, 0),
		translatedModel(1, 1000, 0, 0),
		translatedModel(1, 0, -1000, 0),
	})
	count, _ := h.dispatch(t, 1)
	if count != 3 {
		t.Fatalf("survivors = %d, want all 3 with a zero plane block", count)
	}
}

// TestCullDropsRecordsPastTheLiveInstanceCount is the oracle for the cull fix of
// 2026-07-26.
//
// The input buffer's capacity runs past the live instance count, and a buffer is
// zero-initialized, so a thread past the live data reads an all-zero matrix. Its
// centre is the origin and its scale is zero, so it keeps the base radius and
// passes the frustum test. The kernel used to compact those records and inflate
// the survivor count. It now bounds on the count the uniform carries.
//
// The case writes four records, declares two of them live, and puts all four
// inside the frustum. A kernel that ignored the count would report four.
func TestCullDropsRecordsPastTheLiveInstanceCount(t *testing.T) {
	h := newCullHarness(t, 4)
	models := [][16]float32{
		translatedModel(1, 0, 0, 0),
		translatedModel(1, 0.5, 0, 0),
		translatedModel(1, 0, 0, 0),
		translatedModel(1, 0, 0, 0),
	}

	h.writeUniformWithCount(unitBoxPlanes(), 0.1, cullUnboundedInstanceCount)
	h.writeInstances(models)
	if count, _ := h.dispatch(t, 1); count != 4 {
		t.Fatalf("unbounded survivors = %d, want 4; the case cannot show a bound that is never crossed", count)
	}

	h.writeUniformWithCount(unitBoxPlanes(), 0.1, 2)
	h.writeInstances(models)
	count, ids := h.dispatch(t, 1)
	if count != 2 {
		t.Fatalf("survivors = %d, want 2; the kernel is compacting records past the live instance count", count)
	}
	if ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("compacted pick IDs = %v, want [1 2]", ids)
	}

	h.writeUniformWithCount(unitBoxPlanes(), 0.1, 0)
	h.writeInstances(models)
	if count, _ := h.dispatch(t, 1); count != 0 {
		t.Fatalf("survivors = %d at a live count of zero, want 0; a mesh with no instances must draw nothing", count)
	}
}

// TestBundleFrameKeepsScaledInstanceOnScreen is the pixel-level guard for the
// per-instance-scale cull fix. The instance's centre sits outside the camera
// frustum, so the old constant-radius cull dropped it and the frame came back
// empty. Scaled by 8, the cube still covers the screen centre and must be drawn.
func TestBundleFrameKeepsScaledInstanceOnScreen(t *testing.T) {
	const size = 48
	d, surface := New(size, size)
	r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer r.Destroy()

	// Cube centre 3 units off-axis with a narrow field of view: outside the
	// frustum as a point, well inside it as an 8-unit-wide box.
	frame := engine.RenderBundle{
		Background: "#000000",
		Camera:     engine.RenderCamera{Z: 6, FOV: 0.5, Near: 0.1, Far: 100},
		Materials:  []engine.RenderMaterial{{Color: "#ffffff"}},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			Kind:          "cube",
			Size:          1,
			MaterialIndex: 0,
			VertexCount:   36,
			InstanceCount: 1,
			Transforms: []float64{
				8, 0, 0, 0,
				0, 8, 0, 0,
				0, 0, 8, 0,
				3, 0, 0, 1,
			},
		}},
	}
	if err := r.Frame(frame, size, size, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	center := d.Framebuffer().RGBAAt(size/2, size/2)
	if int(center.R)+int(center.G)+int(center.B) == 0 {
		t.Fatalf("scaled instance was culled off screen; centre pixel = %+v", center)
	}
}

// TestBundlePickResolvesInstanceIdentityAndGeometry is the end-to-end guard for
// the pick rewrite. The renderer no longer builds a per-instance result map on
// every frame; it snapshots a span table plus the instances the click ray
// crosses, then resolves the one ID the GPU returns. The resolved result must
// still carry the mesh identity, the instance index, and the surface geometry.
func TestBundlePickResolvesInstanceIdentityAndGeometry(t *testing.T) {
	const size = 64
	d, surface := New(size, size)
	r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer r.Destroy()

	// Two instances side by side. The right one sits under the screen centre.
	transforms := []float64{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		-20, 0, 0, 1,

		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
	frame := engine.RenderBundle{
		Background: "#000000",
		Camera:     engine.RenderCamera{Z: 6, FOV: 1, Near: 0.1, Far: 100},
		Materials:  []engine.RenderMaterial{{Color: "#ffffff"}},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID:            "pair",
			Kind:          "cube",
			Size:          2,
			MaterialIndex: 0,
			VertexCount:   36,
			InstanceCount: 2,
			Transforms:    transforms,
		}},
	}

	done := make(chan bundle.PickResult, 1)
	r.QueuePickResult(size/2, size/2, func(result bundle.PickResult) { done <- result })
	if err := r.Frame(frame, size, size, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}

	var got bundle.PickResult
	select {
	case got = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pick callback never fired")
	}
	if got.ObjectID != "pair" {
		t.Fatalf("pick object = %q, want %q; result = %+v", got.ObjectID, "pair", got)
	}
	if got.InstanceIndex != 1 {
		t.Errorf("pick instance = %d, want 1 (the cube under the cursor)", got.InstanceIndex)
	}
	if got.TriangleIndex < 0 {
		t.Errorf("pick lost its triangle metadata: %+v", got)
	}
	if got.Depth <= 0 {
		t.Errorf("pick depth = %v, want a positive distance", got.Depth)
	}
	if got.RayDirection == [3]float32{} {
		t.Errorf("pick lost the click ray: %+v", got)
	}
}

// TestBundlePickOnBackgroundStillReportsTheRay checks a click that hits nothing
// still returns the ray, so an editor can run its own confirmation.
func TestBundlePickOnBackgroundStillReportsTheRay(t *testing.T) {
	const size = 32
	d, surface := New(size, size)
	r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer r.Destroy()

	frame := engine.RenderBundle{
		Background: "#000000",
		Camera:     engine.RenderCamera{Z: 6, FOV: 1, Near: 0.1, Far: 100},
	}
	done := make(chan bundle.PickResult, 1)
	r.QueuePickResult(size/2, size/2, func(result bundle.PickResult) { done <- result })
	if err := r.Frame(frame, size, size, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	var got bundle.PickResult
	select {
	case got = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pick callback never fired")
	}
	if got.ID != 0 || got.ObjectIndex != -1 || got.InstanceIndex != -1 {
		t.Fatalf("background pick = %+v, want an empty hit", got)
	}
	if got.RayDirection == [3]float32{} {
		t.Fatalf("background pick lost the click ray: %+v", got)
	}
}
