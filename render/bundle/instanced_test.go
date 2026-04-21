package bundle

import (
	"testing"

	"github.com/odvcencio/gosx/engine"
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
	// Post-FX resources: bloom params + blurH + blurV uniforms = 3.
	// Total = 13.
	if got := len(d.buffers) - buffersBefore; got != 13 {
		t.Errorf("expected 13 new buffers (geometry + instance + material + cull + post-fx), got %d", got)
	}

	if len(d.encoders) != 1 {
		t.Fatalf("expected 1 command encoder, got %d", len(d.encoders))
	}
	passes := d.encoders[0].passes
	if len(passes) != 6 {
		t.Fatalf("expected 6 passes (3 shadow + main + compose + fxaa), got %d", len(passes))
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

func TestFrameSkinnedInstancedMeshUsesSkinnedPipelineInputs(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	transforms := make([]float64, 16)
	transforms[0] = 1
	transforms[5] = 1
	transforms[10] = 1
	transforms[15] = 1

	b := engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID:            "skinned-cube",
			Kind:          "cube",
			VertexCount:   36,
			InstanceCount: 1,
			Transforms:    transforms,
			SkinID:        "default",
		}},
	}

	buffersBefore := len(d.buffers)
	if err := r.Frame(b, 400, 300, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if got := len(d.buffers) - buffersBefore; got != 16 {
		t.Fatalf("expected 16 new buffers (geometry + instance + material + cull + skin + post-fx), got %d", got)
	}
	mainPass := d.encoders[0].passes[3]
	if mainPass.indirectDraws != 1 {
		t.Fatalf("main pass indirect draws = %d, want 1", mainPass.indirectDraws)
	}
	if mainPass.vbufSets < 8 {
		t.Fatalf("main pass vertex buffer sets = %d, want at least 8 for skinned inputs", mainPass.vbufSets)
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

	// Three textures at construction: shadow map + 1x1 fallback + fallback env cube.
	const baselineTextures = 3
	if got := len(d.textures); got != baselineTextures {
		t.Fatalf("expected %d textures at construction (shadow + fallback + env cube), got %d",
			baselineTextures, got)
	}

	empty := engine.RenderBundle{}
	if err := r.Frame(empty, 400, 300, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	// First frame adds: depth + HDR + idBuffer + bloomA + bloomB + postFX = +6.
	if got := len(d.textures); got != baselineTextures+6 {
		t.Fatalf("expected depth + HDR + id + 2 bloom + postFX on first frame, got %d total", got)
	}

	// Same size — no allocation.
	if err := r.Frame(empty, 400, 300, 0.016); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if got := len(d.textures); got != baselineTextures+6 {
		t.Errorf("same-size reframe should reuse textures, got %d", got)
	}

	// Different size — depth + HDR + id + 2 bloom + postFX reallocated = +6.
	if err := r.Frame(empty, 800, 600, 0.032); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if got := len(d.textures); got != baselineTextures+12 {
		t.Errorf("resize should add new depth + HDR + id + 2 bloom + postFX, got %d total", got)
	}
}
