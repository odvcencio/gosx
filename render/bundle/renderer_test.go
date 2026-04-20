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

	if got := len(d.shaders); got != 3 {
		t.Errorf("expected 3 shader modules (unlit+lit+shadow), got %d", got)
	}
	if got := len(d.pipelines); got != 3 {
		t.Errorf("expected 3 render pipelines (unlit+lit+shadow), got %d", got)
	}
	if got := len(d.buffers); got != 2 {
		t.Errorf("expected 2 uniform buffers (scene + shadow), got %d", got)
	}
	if got := len(d.textures); got != 2 {
		t.Errorf("expected 2 textures at construction (shadow map + 1x1 fallback), got %d", got)
	}
	if got := len(d.samplers); got != 2 {
		t.Errorf("expected 2 samplers (shadow comparison + material linear), got %d", got)
	}
	if got := len(d.bindGroups); got != 3 {
		t.Errorf("expected 3 bind groups (one per pipeline), got %d", got)
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

// TestFrameAlwaysEmitsTwoPasses confirms that every non-trivial frame records
// both a shadow pass and a main pass — even if InstancedMeshes is empty.
// The shadow pass is still recorded so depth is cleared between frames.
func TestFrameAlwaysEmitsTwoPasses(t *testing.T) {
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
	if got := len(d.encoders[0].passes); got != 2 {
		t.Errorf("expected 2 render passes per frame (shadow + main), got %d", got)
	}

	// Shadow pass has only a depth attachment. Main pass has color + depth.
	if d.encoders[0].passes[0].desc.ColorAttachments != nil &&
		len(d.encoders[0].passes[0].desc.ColorAttachments) > 0 {
		t.Error("shadow pass should have no color attachments")
	}
	if d.encoders[0].passes[0].desc.DepthStencilAttachment == nil {
		t.Error("shadow pass must have a depth attachment")
	}
	if len(d.encoders[0].passes[1].desc.ColorAttachments) != 1 {
		t.Error("main pass must have one color attachment")
	}
	if d.encoders[0].passes[1].desc.DepthStencilAttachment == nil {
		t.Error("main pass must have a depth attachment")
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
	shadowPass := d.encoders[0].passes[0]
	mainPass := d.encoders[0].passes[1]

	if len(shadowPass.draws) != 0 {
		t.Errorf("shadow pass should have no draws when no instanced meshes, got %d",
			len(shadowPass.draws))
	}
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
	mainPass := d.encoders[0].passes[1]
	if len(mainPass.desc.ColorAttachments) != 1 {
		t.Fatalf("expected 1 color attachment on main pass, got %d",
			len(mainPass.desc.ColorAttachments))
	}
	clear := mainPass.desc.ColorAttachments[0].ClearValue
	const tol = 0.01
	if abs(clear.R-1.0) > tol || abs(clear.G-128.0/255) > tol || abs(clear.B-0) > tol {
		t.Errorf("unexpected clear color: %+v", clear)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
