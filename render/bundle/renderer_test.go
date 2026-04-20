package bundle

import (
	"testing"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/gpu"
)

// TestNewBuildsAllPipelines verifies that Renderer construction creates the
// expected GPU resources for R2: unlit + lit + shadow pipelines, scene +
// shadow uniform buffers, shadow map texture, shadow sampler, and
// per-pipeline bind groups.
func TestNewBuildsAllPipelines(t *testing.T) {
	d := newFakeDevice()

	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	if got := len(d.shaders); got != 9 {
		t.Errorf("expected 9 shader modules (unlit+lit+shadow+cull+present+bright+blur+particleUpdate+particleRender), got %d", got)
	}
	if got := len(d.pipelines); got != 7 {
		t.Errorf("expected 7 render pipelines (unlit+lit+shadow+present+bright+blur+particleRender), got %d", got)
	}
	if got := len(d.computePipelines); got != 2 {
		t.Errorf("expected 2 compute pipelines (cull + particleUpdate), got %d", got)
	}
	if got := len(d.buffers); got != 4 {
		t.Errorf("expected 4 uniform buffers (scene + 3 shadow cascades), got %d", got)
	}
	if got := len(d.textures); got != 2 {
		t.Errorf("expected 2 textures at construction (shadow map + 1x1 fallback), got %d", got)
	}
	if got := len(d.samplers); got != 3 {
		t.Errorf("expected 3 samplers (shadow + material + present), got %d", got)
	}
	if got := len(d.bindGroups); got != 5 {
		t.Errorf("expected 5 bind groups (unlit + lit + 3 shadow cascades), got %d", got)
	}

	// Shadow map is a depth32float render-attachment + texture-binding target.
	shadow := d.textures[0]
	if shadow.desc.Format != gpu.FormatDepth32Float {
		t.Errorf("shadow map format: want depth32float, got %v", shadow.desc.Format)
	}
	if !shadow.desc.Usage.Has(gpu.TextureUsageRenderAttachment) {
		t.Error("shadow map must have RenderAttachment usage")
	}
	if !shadow.desc.Usage.Has(gpu.TextureUsageTextureBinding) {
		t.Error("shadow map must have TextureBinding usage so lit pass can sample it")
	}

	// Lit pipeline = 5 vertex buffers (positions + colors + normals + uvs + instance).
	lit := d.pipelines[1]
	if got := len(lit.desc.Vertex.Buffers); got != 5 {
		t.Errorf("lit: expected 5 vertex buffers (pos+col+nrm+uv+instance), got %d", got)
	}
	if got := lit.desc.Vertex.Buffers[4].StepMode; got != gpu.StepInstance {
		t.Errorf("lit: slot 4 (instance) step mode should be Instance, got %v", got)
	}

	// Shadow pipeline = 2 vertex buffers (positions + instance only).
	shadowPipe := d.pipelines[2]
	if got := len(shadowPipe.desc.Vertex.Buffers); got != 2 {
		t.Errorf("shadow: expected 2 vertex buffers (pos+instance), got %d", got)
	}
	// Shadow uses depth32float matching the shadow-map format.
	if shadowPipe.desc.DepthStencil == nil {
		t.Fatal("shadow pipeline must have depth-stencil state")
	}
	if shadowPipe.desc.DepthStencil.Format != gpu.FormatDepth32Float {
		t.Errorf("shadow pipeline depth format: want depth32float, got %v",
			shadowPipe.desc.DepthStencil.Format)
	}

	// Shadow sampler is a comparison sampler.
	if d.samplers[0].desc.Compare == gpu.CompareAlways {
		t.Error("shadow sampler should be a comparison sampler (Compare != Always)")
	}

	// Lit bind group (group 0) has 3 entries: scene uniform + shadow texture + shadow sampler.
	litBG := d.bindGroups[1]
	if got := len(litBG.desc.Entries); got != 3 {
		t.Errorf("lit group-0 bindgroup: expected 3 entries, got %d", got)
	}
}

// TestFrameAlwaysEmitsCSMPlusMainPass confirms every non-trivial frame
// records N shadow passes (one per cascade), a main pass, and a present pass.
func TestFrameAlwaysEmitsCSMPlusMainPass(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	b := engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
	}
	if err := r.Frame(b, 400, 300, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if len(d.encoders) != 1 {
		t.Fatalf("expected 1 command encoder per frame, got %d", len(d.encoders))
	}
	passes := d.encoders[0].passes
	if got := len(passes); got != 5 {
		t.Fatalf("expected 5 passes (3 shadow + main + present), got %d", got)
	}

	// Shadow passes (indices 0..2) have only depth attachments.
	for i := 0; i < 3; i++ {
		if len(passes[i].desc.ColorAttachments) != 0 {
			t.Errorf("shadow cascade %d: expected no color attachments", i)
		}
		if passes[i].desc.DepthStencilAttachment == nil {
			t.Errorf("shadow cascade %d: expected depth attachment", i)
		}
	}
	mainPass := passes[3]
	if len(mainPass.desc.ColorAttachments) != 2 {
		t.Errorf("main pass must have two color attachments (HDR + id), got %d",
			len(mainPass.desc.ColorAttachments))
	}
	if mainPass.desc.DepthStencilAttachment == nil {
		t.Error("main pass must have a depth attachment")
	}
	// Present pass tone-maps to the swap chain; color only, no depth.
	present := passes[4]
	if len(present.desc.ColorAttachments) != 1 {
		t.Error("present pass must have one color attachment")
	}
	if present.desc.DepthStencilAttachment != nil {
		t.Error("present pass must not have a depth attachment")
	}
}

func TestFrameRunsBloomOnlyWhenPostEffectPresent(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	b := engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		PostEffects: []engine.RenderPostEffect{{
			Kind:      "bloom",
			Threshold: 1.25,
			Intensity: 0.75,
			Radius:    10,
			Scale:     0.25,
		}},
	}
	if err := r.Frame(b, 400, 300, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}

	passes := d.encoders[0].passes
	if got := len(passes); got != 8 {
		t.Fatalf("expected 8 passes (3 shadow + main + 3 bloom + present), got %d", got)
	}
	for idx, label := range map[int]string{
		4: "bundle.bloom.bright",
		5: "bundle.bloom.blurH",
		6: "bundle.bloom.blurV",
	} {
		if passes[idx].desc.Label != label {
			t.Fatalf("pass %d label = %q, want %q", idx, passes[idx].desc.Label, label)
		}
	}

	if got := latestWriteBytes(d.queue, "bundle.bloom.params.uniform"); string(got) != string(float32sToBytes([]float32{1.25, 0.75, 0.25, 0})) {
		t.Fatalf("unexpected bloom params uniform bytes: %v", got)
	}
	if got := latestWriteBytes(d.queue, "bundle.bloom.blurH.uniform"); string(got) != string(float32sToBytes([]float32{0.02, 0, 0, 0})) {
		t.Fatalf("unexpected bloom horizontal blur uniform bytes: %v", got)
	}
}

// TestFrameUnlitPassDispatches verifies that a legacy RenderPassBundle entry
// produces an unlit-pipeline draw call on the main pass only (shadow pass
// does not draw pass-data meshes in R2).
func TestFrameUnlitPassDispatches(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	b := engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		Passes: []engine.RenderPassBundle{{
			CacheKey:    "cube",
			Positions:   []float64{0, 0, 0, 1, 0, 0, 0, 1, 0},
			Colors:      []float64{1, 0, 0, 0, 1, 0, 0, 0, 1},
			VertexCount: 3,
		}},
	}
	if err := r.Frame(b, 400, 300, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	passes := d.encoders[0].passes
	if len(passes) != 5 {
		t.Fatalf("expected 5 passes (3 shadow + main + present), got %d", len(passes))
	}
	// None of the shadow cascades should draw pass-data meshes (R3 limitation).
	for i := 0; i < 3; i++ {
		if got := len(passes[i].draws); got != 0 {
			t.Errorf("shadow cascade %d should have no draws, got %d", i, got)
		}
	}
	mainPass := passes[3]
	if len(mainPass.draws) != 1 {
		t.Fatalf("main pass: expected 1 draw, got %d", len(mainPass.draws))
	}
	if mainPass.draws[0].vertexCount != 3 {
		t.Errorf("main draw: expected 3 verts, got %d", mainPass.draws[0].vertexCount)
	}
}

// TestFrameZeroSizedNoOp confirms zero width/height short-circuits before
// any encoder is created.
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
		t.Errorf("zero-sized frame should not create encoder, got %d", len(d.encoders))
	}
}

// TestFrameClearColorFromBackground verifies the main pass clear color is
// parsed from RenderBundle.Background.
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
	// Main pass (HDR + id targets) is at index 3; present is index 4.
	mainPass := d.encoders[0].passes[3]
	if len(mainPass.desc.ColorAttachments) != 2 {
		t.Fatalf("expected 2 color attachments on main pass (HDR + id), got %d",
			len(mainPass.desc.ColorAttachments))
	}
	clear := mainPass.desc.ColorAttachments[0].ClearValue
	const tol = 0.01
	if abs(clear.R-1.0) > tol || abs(clear.G-128.0/255) > tol || abs(clear.B-0) > tol {
		t.Errorf("unexpected clear color: %+v", clear)
	}
}

func latestWriteBytes(q *fakeQueue, label string) []byte {
	for i := len(q.writes) - 1; i >= 0; i-- {
		buffer, ok := q.writes[i].buffer.(*fakeBuffer)
		if !ok || buffer.label != label {
			continue
		}
		return q.writes[i].data
	}
	return nil
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
