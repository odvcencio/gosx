package bundle

import (
	"testing"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/gpu"
)

// TestNewBuildsPipeline verifies that Renderer construction creates exactly
// the expected GPU resources for the unlit pipeline: one shader, one
// pipeline, one uniform buffer, one bind group.
func TestNewBuildsPipeline(t *testing.T) {
	d := newFakeDevice()

	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	if got := len(d.shaders); got != 1 {
		t.Errorf("expected 1 shader module, got %d", got)
	}
	if got := len(d.pipelines); got != 1 {
		t.Errorf("expected 1 render pipeline, got %d", got)
	}
	if got := len(d.buffers); got != 1 {
		t.Errorf("expected 1 buffer (uniform), got %d", got)
	}
	if got := len(d.bindGroups); got != 1 {
		t.Errorf("expected 1 bind group, got %d", got)
	}

	if d.pipelines[0].desc.Vertex.EntryPoint != "vs_main" {
		t.Errorf("expected vertex entry vs_main, got %q", d.pipelines[0].desc.Vertex.EntryPoint)
	}
	if d.pipelines[0].desc.Fragment.EntryPoint != "fs_main" {
		t.Errorf("expected fragment entry fs_main, got %q", d.pipelines[0].desc.Fragment.EntryPoint)
	}
	if got := len(d.pipelines[0].desc.Vertex.Buffers); got != 2 {
		t.Errorf("expected 2 vertex buffers (positions + colors), got %d", got)
	}
	if d.pipelines[0].desc.Primitive.CullMode != gpu.CullBack {
		t.Errorf("expected back-face culling, got %v", d.pipelines[0].desc.Primitive.CullMode)
	}
}

// TestFrameSubmitsOnePassPerBundlePass verifies that each RenderPassBundle in
// the input produces a draw call. Uses cache keys to confirm the buffer
// cache hit path on the second frame.
func TestFrameSubmitsOnePassPerBundlePass(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	b := engine.RenderBundle{
		Background: "#112233",
		Camera:     engine.RenderCamera{Z: 5, FOV: 1.0, Near: 0.1, Far: 100},
		Passes: []engine.RenderPassBundle{
			{
				CacheKey:    "cube",
				Positions:   []float64{0, 0, 0, 1, 0, 0, 0, 1, 0},
				Colors:      []float64{1, 0, 0, 0, 1, 0, 0, 0, 1},
				VertexCount: 3,
			},
		},
	}

	if err := r.Frame(b, 800, 600, 0.0); err != nil {
		t.Fatalf("Frame: %v", err)
	}

	if len(d.encoders) != 1 {
		t.Fatalf("expected 1 encoder, got %d", len(d.encoders))
	}
	if len(d.encoders[0].passes) != 1 {
		t.Fatalf("expected 1 render pass, got %d", len(d.encoders[0].passes))
	}
	pass := d.encoders[0].passes[0]
	if !pass.pipelineSet {
		t.Error("pipeline was not set on the pass")
	}
	if !pass.bindGroupSet {
		t.Error("bind group was not set on the pass")
	}
	if pass.vbufSets != 2 {
		t.Errorf("expected 2 vertex buffer sets, got %d", pass.vbufSets)
	}
	if len(pass.draws) != 1 {
		t.Fatalf("expected 1 draw call, got %d", len(pass.draws))
	}
	if pass.draws[0].vertexCount != 3 {
		t.Errorf("expected vertexCount=3, got %d", pass.draws[0].vertexCount)
	}
	if !pass.ended {
		t.Error("pass.End was not called")
	}

	// Frame again. Cached pass should not allocate new buffers.
	buffersBefore := len(d.buffers)
	if err := r.Frame(b, 800, 600, 0.1); err != nil {
		t.Fatalf("second Frame: %v", err)
	}
	buffersAfter := len(d.buffers)
	if buffersAfter != buffersBefore {
		t.Errorf("cached pass should not allocate new buffers: before=%d after=%d",
			buffersBefore, buffersAfter)
	}
}

// TestFrameClearColorFromBackground verifies that a valid #rrggbb background
// string is parsed and drives the render pass clear value.
func TestFrameClearColorFromBackground(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	if err := r.Frame(engine.RenderBundle{Background: "#ff8000"}, 100, 100, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}

	pass := d.encoders[0].passes[0]
	if len(pass.desc.ColorAttachments) != 1 {
		t.Fatalf("expected 1 color attachment, got %d", len(pass.desc.ColorAttachments))
	}
	clear := pass.desc.ColorAttachments[0].ClearValue
	const tol = 0.01
	if abs(clear.R-1.0) > tol || abs(clear.G-128.0/255) > tol || abs(clear.B-0) > tol {
		t.Errorf("unexpected clear color: %+v", clear)
	}
}

// TestFrameZeroSizedNoOp confirms zero width/height is treated as a no-op
// rather than spawning a pass.
func TestFrameZeroSizedNoOp(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	if err := r.Frame(engine.RenderBundle{}, 0, 0, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if len(d.encoders) != 0 {
		t.Errorf("zero-sized frame should not create a command encoder, got %d", len(d.encoders))
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
