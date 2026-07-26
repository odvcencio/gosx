package bundle

import (
	"fmt"
	"strings"
	"testing"
	"unsafe"

	"m31labs.dev/gosx/engine"
)

// staticInstanceBundle builds a bundle with one instanced mesh whose transforms
// never change between frames.
func staticInstanceBundle(instances int) engine.RenderBundle {
	transforms := make([]float64, instances*16)
	for i := 0; i < instances; i++ {
		base := i * 16
		transforms[base+0] = 1
		transforms[base+5] = 1
		transforms[base+10] = 1
		transforms[base+15] = 1
		transforms[base+12] = float64(i%16) * 2
		transforms[base+14] = float64(i/16) * -2
	}
	return engine.RenderBundle{
		Background: "#101018",
		Camera:     engine.RenderCamera{Z: 30, FOV: 1, Near: 0.1, Far: 200},
		Materials:  []engine.RenderMaterial{{Kind: "standard", Color: "#cccccc"}},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID:            "grid",
			Kind:          "cube",
			MaterialIndex: 0,
			VertexCount:   36,
			InstanceCount: instances,
			Transforms:    transforms,
			CastShadow:    true,
		}},
	}
}

// countWritesTo returns the number of queue writes whose target buffer label
// carries the given prefix.
func countWritesTo(d *fakeDevice, prefix string) int {
	labels := make(map[interface{}]string, len(d.buffers))
	for _, buf := range d.buffers {
		labels[buf] = buf.label
	}
	n := 0
	for _, w := range d.queue.writes {
		if strings.HasPrefix(labels[w.buffer], prefix) {
			n++
		}
	}
	return n
}

// TestFrameSkipsInstanceUploadForStaticTransforms pins defect B: a mesh whose
// transforms did not change must not re-encode and re-upload them. The recording
// fake device makes the missing WriteBuffer call visible, not merely the missing
// allocation.
func TestFrameSkipsInstanceUploadForStaticTransforms(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	b := staticInstanceBundle(64)
	if err := r.Frame(b, 320, 240, 0); err != nil {
		t.Fatalf("first Frame: %v", err)
	}
	if got := countWritesTo(d, "bundle.cull.input:"); got != 1 {
		t.Fatalf("first frame instance uploads = %d, want 1", got)
	}

	d.queue.writes = nil
	for i := 1; i <= 4; i++ {
		if err := r.Frame(b, 320, 240, float64(i)*0.016); err != nil {
			t.Fatalf("Frame %d: %v", i, err)
		}
	}
	if got := countWritesTo(d, "bundle.cull.input:"); got != 0 {
		t.Fatalf("static transforms re-uploaded %d times across 4 frames, want 0", got)
	}

	// A moved instance must upload again on the very next frame.
	moved := staticInstanceBundle(64)
	moved.InstancedMeshes[0].Transforms[12] = 99
	d.queue.writes = nil
	if err := r.Frame(moved, 320, 240, 0.1); err != nil {
		t.Fatalf("moved Frame: %v", err)
	}
	if got := countWritesTo(d, "bundle.cull.input:"); got != 1 {
		t.Fatalf("changed transforms uploads = %d, want 1", got)
	}
}

// TestFrameSkipsRepeatPostFXUniformWrites pins the post-FX half of defect D and
// E: bloom, tone-map, and blur uniforms hold still across frames, so the
// renderer must stop rewriting them.
func TestFrameSkipsRepeatPostFXUniformWrites(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	b := staticInstanceBundle(4)
	for i := 0; i < 3; i++ {
		if err := r.Frame(b, 320, 240, float64(i)*0.016); err != nil {
			t.Fatalf("warm-up Frame: %v", err)
		}
	}
	// Confirm the labels the assertion below relies on really exist, so a
	// renamed buffer cannot make this test pass by matching nothing.
	uniforms := []string{
		"bundle.bloom.params.uniform",
		"bundle.present.tonemap.uniform",
		"bundle.bloom.blurH.uniform",
		"bundle.bloom.blurV.uniform",
	}
	for _, label := range uniforms {
		found := false
		for _, buf := range d.buffers {
			if buf.label == label {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("post-FX uniform %q does not exist; update this test", label)
		}
	}

	d.queue.writes = nil
	if err := r.Frame(b, 320, 240, 0.1); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	for _, label := range uniforms {
		if got := countWritesTo(d, label); got != 0 {
			t.Errorf("%s rewritten %d times on a steady frame, want 0", label, got)
		}
	}
}

// TestFrameSteadyStateAllocatesNothing pins defects A through E together. The
// null device does no GPU work, so every remaining allocation belongs to the
// renderer's own per-frame path.
func TestFrameSteadyStateAllocatesNothing(t *testing.T) {
	device := newNullDevice()
	r, err := New(Config{Device: device, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	b := staticInstanceBundle(1000)
	for i := 0; i < 4; i++ {
		if err := r.Frame(b, 256, 256, float64(i)*0.016); err != nil {
			t.Fatalf("warm-up Frame: %v", err)
		}
	}
	buffersBefore := device.buffers
	frame := 4
	allocs := testing.AllocsPerRun(20, func() {
		frame++
		if err := r.Frame(b, 256, 256, float64(frame)*0.016); err != nil {
			t.Fatalf("Frame: %v", err)
		}
	})
	if allocs != 0 {
		t.Errorf("steady-state Frame allocs = %v, want 0", allocs)
	}
	if device.buffers != buffersBefore {
		t.Errorf("steady-state Frame created %d GPU buffers, want 0",
			device.buffers-buffersBefore)
	}
}

// TestFrameKeyBuildingIsOncePerGeometryChange pins defect C: the cull/skin cache
// key must be built when a slot's geometry parameters change, not four or five
// times per mesh per frame.
func TestFrameKeyBuildingIsOncePerGeometryChange(t *testing.T) {
	device := newNullDevice()
	r, err := New(Config{Device: device, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	b := staticInstanceBundle(8)
	if err := r.Frame(b, 128, 128, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	want := instancedMeshKey(0, b.InstancedMeshes[0])
	if got := r.meshStates[0].key; got != want {
		t.Fatalf("cached key = %q, want %q", got, want)
	}
	keyPtr := unsafeStringData(r.meshStates[0].key)

	if err := r.Frame(b, 128, 128, 0.016); err != nil {
		t.Fatalf("second Frame: %v", err)
	}
	if unsafeStringData(r.meshStates[0].key) != keyPtr {
		t.Fatal("a steady frame rebuilt the mesh cache key")
	}

	// Changing the geometry parameters must produce a new key.
	resized := staticInstanceBundle(8)
	resized.InstancedMeshes[0].Size = 4
	if err := r.Frame(resized, 128, 128, 0.032); err != nil {
		t.Fatalf("resized Frame: %v", err)
	}
	if r.meshStates[0].key == want {
		t.Fatalf("changed primitive size kept the old key %q", want)
	}
}

// TestFrameCasterCullBuffersExistPerCascade pins defect G's resource shape: each
// shadow-casting mesh gets one compacted caster buffer plus indirect draw args
// per cascade.
func TestFrameCasterCullBuffersExistPerCascade(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	if err := r.Frame(staticInstanceBundle(8), 320, 240, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	for cascade := 0; cascade < cascadeCount; cascade++ {
		suffix := fmt.Sprintf("#cascade%d", cascade)
		outputs, drawArgs := 0, 0
		for _, buf := range d.buffers {
			if !strings.HasSuffix(buf.label, suffix) {
				continue
			}
			switch {
			case strings.HasPrefix(buf.label, "bundle.cull.casterOutput:"):
				outputs++
			case strings.HasPrefix(buf.label, "bundle.cull.casterDrawArgs:"):
				drawArgs++
			}
		}
		if outputs != 1 || drawArgs != 1 {
			t.Errorf("cascade %d: outputs=%d drawArgs=%d, want 1 and 1", cascade, outputs, drawArgs)
		}
	}

	// A mesh that casts no shadow must not pay for caster buffers.
	d2 := newFakeDevice()
	r2, err := New(Config{Device: d2, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r2.Destroy()
	quiet := staticInstanceBundle(8)
	quiet.InstancedMeshes[0].CastShadow = false
	if err := r2.Frame(quiet, 320, 240, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	for _, buf := range d2.buffers {
		if strings.HasPrefix(buf.label, "bundle.cull.caster") {
			t.Fatalf("non-casting mesh allocated caster buffer %q", buf.label)
		}
	}
}

// unsafeStringData returns the address of a string's backing bytes. Tests use it
// to tell a reused cache key from a freshly formatted one.
func unsafeStringData(s string) uintptr {
	if s == "" {
		return 0
	}
	return uintptr(unsafe.Pointer(unsafe.StringData(s)))
}

// TestFrameSkipsCasterCullWhenShadowsTurnOff checks the caster dispatches and
// their uniform writes stop when a mesh stops casting. The buffers stay
// allocated, so only the per-frame work must disappear.
func TestFrameSkipsCasterCullWhenShadowsTurnOff(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	casting := staticInstanceBundle(8)
	if err := r.Frame(casting, 320, 240, 0); err != nil {
		t.Fatalf("casting Frame: %v", err)
	}
	if got := len(d.encoders[0].computePasses[0].dispatches); got != 1+cascadeCount {
		t.Fatalf("casting dispatches = %d, want %d", got, 1+cascadeCount)
	}

	quiet := staticInstanceBundle(8)
	quiet.InstancedMeshes[0].CastShadow = false
	d.queue.writes = nil
	if err := r.Frame(quiet, 320, 240, 0.016); err != nil {
		t.Fatalf("quiet Frame: %v", err)
	}
	if got := len(d.encoders[1].computePasses[0].dispatches); got != 1 {
		t.Errorf("dispatches after shadows turned off = %d, want 1", got)
	}
	if got := countWritesTo(d, "bundle.cull.caster"); got != 0 {
		t.Errorf("caster uniform writes after shadows turned off = %d, want 0", got)
	}
	for cascade := 0; cascade < cascadeCount; cascade++ {
		if got := d.encoders[1].passes[cascade].indirectDraws; got != 0 {
			t.Errorf("cascade %d drew %d casters for a non-casting mesh", cascade, got)
		}
	}
}
