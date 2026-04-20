package bundle

import (
	"testing"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/gpu"
)

// TestFrameInstancedMeshDispatches verifies that RenderInstancedMesh entries
// result in (a) primitive geometry being created on demand, (b) an instance
// transform buffer being uploaded, and (c) an instanced draw call against
// the instanced pipeline.
func TestFrameInstancedMeshDispatches(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	// 3 instances — each needs 16 floats × 3 = 48 floats of transform data.
	transforms := make([]float64, 16*3)
	// Identity matrices: diag = 1.
	for i := 0; i < 3; i++ {
		base := i * 16
		transforms[base+0] = 1
		transforms[base+5] = 1
		transforms[base+10] = 1
		transforms[base+15] = 1
	}

	b := engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			Kind:          "cube",
			VertexCount:   36,
			InstanceCount: 3,
			Transforms:    transforms,
		}},
	}

	buffersBefore := len(d.buffers)
	if err := r.Frame(b, 400, 300, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}

	// Primitive geometry created = position + color = 2 buffers.
	// Instance buffer created = 1 buffer.
	// Total new buffers = 3.
	if got := len(d.buffers) - buffersBefore; got != 3 {
		t.Errorf("expected 3 new buffers (primitive pos + col + instance), got %d", got)
	}

	if len(d.encoders) != 1 {
		t.Fatalf("expected 1 command encoder, got %d", len(d.encoders))
	}
	pass := d.encoders[0].passes[0]
	if pass.desc.DepthStencilAttachment == nil {
		t.Error("pass should have a depth-stencil attachment in R1")
	}
	if len(pass.draws) != 1 {
		t.Fatalf("expected 1 draw call, got %d", len(pass.draws))
	}
	draw := pass.draws[0]
	if draw.vertexCount != 36 {
		t.Errorf("expected cube 36 verts, got %d", draw.vertexCount)
	}
	if draw.instanceCount != 3 {
		t.Errorf("expected 3 instances, got %d", draw.instanceCount)
	}
	// Instanced path sets 3 vertex buffers: pos, col, instance.
	if pass.vbufSets != 3 {
		t.Errorf("expected 3 vertex-buffer sets for instanced draw, got %d", pass.vbufSets)
	}

	// Second frame with the same Kind should not create new primitive buffers.
	buffersBefore = len(d.buffers)
	if err := r.Frame(b, 400, 300, 0.1); err != nil {
		t.Fatalf("second Frame: %v", err)
	}
	if got := len(d.buffers) - buffersBefore; got != 0 {
		t.Errorf("primitive cache should prevent new buffers on second frame, got %d new", got)
	}
}

// TestPrimitiveForKnownKinds verifies that the primitive library returns
// non-empty geometry for each documented Kind and nil for unknown.
func TestPrimitiveForKnownKinds(t *testing.T) {
	for _, kind := range []string{"cube", "box", "plane", "sphere"} {
		geo := primitiveForKind(kind)
		if geo == nil {
			t.Errorf("%s: primitive should be non-nil", kind)
			continue
		}
		if geo.vertexCount == 0 {
			t.Errorf("%s: vertexCount is 0", kind)
		}
		if len(geo.positions) != geo.vertexCount*3 {
			t.Errorf("%s: positions len %d, want %d", kind, len(geo.positions), geo.vertexCount*3)
		}
		if len(geo.colors) != geo.vertexCount*3 {
			t.Errorf("%s: colors len %d, want %d", kind, len(geo.colors), geo.vertexCount*3)
		}
	}
	if got := primitiveForKind("nosuchkind"); got != nil {
		t.Error("unknown kind should return nil")
	}
}

// TestFrameDepthAttachmentResizes verifies that successive frames at
// different canvas sizes produce exactly one depth texture per unique size.
func TestFrameDepthAttachmentResizes(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	empty := engine.RenderBundle{}
	if err := r.Frame(empty, 400, 300, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if got := len(d.textures); got != 1 {
		t.Fatalf("expected 1 depth texture after first frame, got %d", got)
	}
	if t1 := d.textures[0]; t1.desc.Format != gpu.FormatDepth24Plus {
		t.Errorf("expected depth24plus format, got %v", t1.desc.Format)
	}

	// Same size — should not reallocate.
	if err := r.Frame(empty, 400, 300, 0.016); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if got := len(d.textures); got != 1 {
		t.Errorf("same-size reframe should reuse depth texture, got %d", got)
	}

	// Different size — reallocates.
	if err := r.Frame(empty, 800, 600, 0.032); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if got := len(d.textures); got != 2 {
		t.Errorf("resize should trigger new depth texture, got %d total", got)
	}
}
