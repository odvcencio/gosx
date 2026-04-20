package bundle

import (
	"testing"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/gpu"
)

// TestFrameInstancedMeshDispatches verifies that a RenderInstancedMesh
// produces draw calls on BOTH the shadow pass and the lit main pass.
func TestFrameInstancedMeshDispatches(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	transforms := make([]float64, 16*3)
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

	// Primitive geometry: positions + colors + normals = 3 buffers.
	// Instance buffer = 1 buffer.
	// Material uniform (resolved for the cube's default material) = 1 buffer.
	if got := len(d.buffers) - buffersBefore; got != 5 {
		t.Errorf("expected 5 new buffers (pos+col+nrm+instance+material), got %d", got)
	}

	if len(d.encoders) != 1 {
		t.Fatalf("expected 1 command encoder, got %d", len(d.encoders))
	}
	shadowPass := d.encoders[0].passes[0]
	mainPass := d.encoders[0].passes[1]

	if len(shadowPass.draws) != 1 {
		t.Fatalf("shadow pass: expected 1 instanced draw, got %d", len(shadowPass.draws))
	}
	if shadowPass.draws[0].instanceCount != 3 {
		t.Errorf("shadow pass: expected 3 instances, got %d", shadowPass.draws[0].instanceCount)
	}
	if len(mainPass.draws) != 1 {
		t.Fatalf("main pass: expected 1 instanced draw, got %d", len(mainPass.draws))
	}
	if mainPass.draws[0].instanceCount != 3 {
		t.Errorf("main pass: expected 3 instances, got %d", mainPass.draws[0].instanceCount)
	}
	if mainPass.draws[0].vertexCount != 36 {
		t.Errorf("main pass: expected 36 verts (cube), got %d", mainPass.draws[0].vertexCount)
	}

	// Second frame — caches hit; no new buffers.
	buffersBefore = len(d.buffers)
	if err := r.Frame(b, 400, 300, 0.1); err != nil {
		t.Fatalf("second Frame: %v", err)
	}
	if got := len(d.buffers) - buffersBefore; got != 0 {
		t.Errorf("cached frame should not allocate new buffers, got %d new", got)
	}
}

// TestPrimitiveForKnownKinds verifies all documented primitive kinds yield
// non-empty geometry with positions, colors, AND normals.
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
		if len(geo.normals) != geo.vertexCount*3 {
			t.Errorf("%s: normals len %d, want %d", kind, len(geo.normals), geo.vertexCount*3)
		}
	}
	if got := primitiveForKind("nosuchkind"); got != nil {
		t.Error("unknown kind should return nil")
	}
}

// TestFrameDepthAttachmentResizes verifies the main-pass depth texture
// resizes on canvas-size changes, while the shadow map (created at New)
// stays at its fixed resolution.
func TestFrameDepthAttachmentResizes(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	// Shadow map (1 texture) created at New.
	shadowMapCount := 1
	if got := len(d.textures); got != shadowMapCount {
		t.Fatalf("expected %d textures at construction (shadow map), got %d",
			shadowMapCount, got)
	}

	empty := engine.RenderBundle{}
	if err := r.Frame(empty, 400, 300, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	// First frame adds the main-pass depth texture.
	if got := len(d.textures); got != shadowMapCount+1 {
		t.Fatalf("expected depth texture added on first frame, got %d total", got)
	}
	depth := d.textures[shadowMapCount]
	if depth.desc.Format != gpu.FormatDepth24Plus {
		t.Errorf("main depth format: want depth24plus, got %v", depth.desc.Format)
	}

	// Same size — no allocation.
	if err := r.Frame(empty, 400, 300, 0.016); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if got := len(d.textures); got != shadowMapCount+1 {
		t.Errorf("same-size reframe should reuse depth texture, got %d", got)
	}

	// Different size — new depth texture allocated.
	if err := r.Frame(empty, 800, 600, 0.032); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if got := len(d.textures); got != shadowMapCount+2 {
		t.Errorf("resize should create new depth texture, got %d total", got)
	}
}
