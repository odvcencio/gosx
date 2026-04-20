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

	// Primitive geometry: positions + colors + normals + uvs = 4 buffers.
	// Shared shadow-pass instance buffer = 1.
	// Material uniform = 1.
	// Cull resources (input + output + drawArgs + cullUniform) = 4.
	// Total = 10.
	if got := len(d.buffers) - buffersBefore; got != 10 {
		t.Errorf("expected 10 new buffers (pos+col+nrm+uv+instance+material+4 cull), got %d", got)
	}

	if len(d.encoders) != 1 {
		t.Fatalf("expected 1 command encoder, got %d", len(d.encoders))
	}
	passes := d.encoders[0].passes
	if len(passes) != 4 {
		t.Fatalf("expected 4 passes (3 shadow cascades + main), got %d", len(passes))
	}
	mainPass := passes[3]

	// Each of the 3 cascades draws the instanced mesh once.
	for i := 0; i < 3; i++ {
		if len(passes[i].draws) != 1 {
			t.Fatalf("shadow cascade %d: expected 1 draw, got %d", i, len(passes[i].draws))
		}
		if passes[i].draws[0].instanceCount != 3 {
			t.Errorf("shadow cascade %d: expected 3 instances, got %d",
				i, passes[i].draws[0].instanceCount)
		}
	}
	// Main pass emits one indirect draw (cull compacted output) per mesh.
	if mainPass.indirectDraws != 1 {
		t.Errorf("main pass: expected 1 indirect draw, got %d", mainPass.indirectDraws)
	}
	if len(mainPass.draws) != 0 {
		t.Errorf("main pass: all draws should go via indirect, got %d direct",
			len(mainPass.draws))
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

	// Two textures at construction: shadow map + 1x1 fallback baseColor.
	const baselineTextures = 2
	if got := len(d.textures); got != baselineTextures {
		t.Fatalf("expected %d textures at construction (shadow + fallback), got %d",
			baselineTextures, got)
	}

	empty := engine.RenderBundle{}
	if err := r.Frame(empty, 400, 300, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	// First frame adds the main-pass depth texture.
	if got := len(d.textures); got != baselineTextures+1 {
		t.Fatalf("expected depth texture added on first frame, got %d total", got)
	}
	depth := d.textures[baselineTextures]
	if depth.desc.Format != gpu.FormatDepth24Plus {
		t.Errorf("main depth format: want depth24plus, got %v", depth.desc.Format)
	}

	// Same size — no allocation.
	if err := r.Frame(empty, 400, 300, 0.016); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if got := len(d.textures); got != baselineTextures+1 {
		t.Errorf("same-size reframe should reuse depth texture, got %d", got)
	}

	// Different size — new depth texture allocated.
	if err := r.Frame(empty, 800, 600, 0.032); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if got := len(d.textures); got != baselineTextures+2 {
		t.Errorf("resize should create new depth texture, got %d total", got)
	}
}
