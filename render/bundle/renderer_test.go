package bundle

import (
	"encoding/binary"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/gpu"
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

	if got := len(d.shaders); got != 17 {
		t.Errorf("expected 17 shader modules (unlit+lit+skinnedLit+surface+worldLine+shadow+cull+present+fxaa+bright+blur+ssao+dof+vignette+colorGrade+particleUpdate+particleRender), got %d", got)
	}
	if got := len(d.pipelines); got != 17 {
		t.Errorf("expected 17 render pipelines (unlit+lit+skinnedLit+3 surface modes+worldLine+shadow+present+fxaa+bright+blur+ssao+dof+vignette+colorGrade+particleRender), got %d", got)
	}
	if got := len(d.computePipelines); got != 2 {
		t.Errorf("expected 2 compute pipelines (cull + particleUpdate), got %d", got)
	}
	if got := len(d.buffers); got != 5 {
		t.Errorf("expected 5 startup buffers (scene + 3 shadow cascades + default bone palette), got %d", got)
	}
	if got := len(d.textures); got != 3 {
		t.Errorf("expected 3 textures at construction (shadow map + 1x1 fallback + fallback env cube), got %d", got)
	}
	if got := len(d.samplers); got != 3 {
		t.Errorf("expected 3 samplers (shadow + material + present), got %d", got)
	}
	if got := len(d.bindGroups); got != 11 {
		t.Errorf("expected 11 bind groups (default bone + unlit + 3 surface modes + worldLine + lit + skinned lit + 3 shadow cascades), got %d", got)
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

	// Lit pipeline = 5 vertex buffers (positions + colors + normals + uvs + instance record).
	lit := findPipeline(t, d, "bundle.lit")
	if got := len(lit.desc.Vertex.Buffers); got != 5 {
		t.Errorf("lit: expected 5 vertex buffers (pos+col+nrm+uv+instance record), got %d", got)
	}
	if got := lit.desc.Vertex.Buffers[4].StepMode; got != gpu.StepInstance {
		t.Errorf("lit: slot 4 (instance) step mode should be Instance, got %v", got)
	}
	if got := lit.desc.Vertex.Buffers[4].ArrayStride; got != instanceRecordStride {
		t.Errorf("lit: instance record stride = %d, want %d", got, instanceRecordStride)
	}
	attrs := lit.desc.Vertex.Buffers[4].Attributes
	if len(attrs) != 5 {
		t.Fatalf("lit: instance attrs = %d, want matrix columns + pick data", len(attrs))
	}
	if got := attrs[4]; got.ShaderLocation != 8 || got.Offset != 64 || got.Format != gpu.VertexFormatUint32x4 {
		t.Errorf("lit: pick attribute = %#v, want location 8 offset 64 uint32x4", got)
	}

	// Skinned lit pipeline = rigid lit buffers + joints + weights + bind-pose mat4.
	skinnedLit := findPipeline(t, d, "bundle.lit.skinned")
	if got := len(skinnedLit.desc.Vertex.Buffers); got != 8 {
		t.Errorf("skinned lit: expected 8 vertex buffers, got %d", got)
	}
	if got := skinnedLit.desc.Vertex.Buffers[5].Attributes[0].Format; got != gpu.VertexFormatUint32x4 {
		t.Errorf("skinned lit joints format = %v, want %v", got, gpu.VertexFormatUint32x4)
	}
	if got := skinnedLit.desc.Vertex.Buffers[5].Attributes[0].ShaderLocation; got != 9 {
		t.Errorf("skinned lit joints location = %d, want 9 after pick data", got)
	}

	surfaceOpaque := findPipeline(t, d, "bundle.surface.opaque")
	if got := len(surfaceOpaque.desc.Vertex.Buffers); got != 3 {
		t.Errorf("surface opaque: expected positions + uv + pick-id vertex buffers, got %d", got)
	}
	if got := surfaceOpaque.desc.Vertex.Buffers[2].Attributes[0].Format; got != gpu.VertexFormatUint32 {
		t.Errorf("surface pick-id format = %v, want uint32", got)
	}
	if surfaceOpaque.desc.Fragment.Targets[0].Blend != nil {
		t.Error("surface opaque pipeline should not blend")
	}
	if surfaceOpaque.desc.DepthStencil == nil || !surfaceOpaque.desc.DepthStencil.DepthWriteEnabled {
		t.Fatal("surface opaque pipeline should depth-test and depth-write")
	}
	surfaceAlpha := findPipeline(t, d, "bundle.surface.alpha")
	if surfaceAlpha.desc.Fragment.Targets[0].Blend == nil {
		t.Error("surface alpha pipeline should enable alpha blending")
	}
	if surfaceAlpha.desc.DepthStencil == nil || surfaceAlpha.desc.DepthStencil.DepthWriteEnabled {
		t.Fatal("surface alpha pipeline should depth-test without depth writes")
	}
	surfaceAdditive := findPipeline(t, d, "bundle.surface.additive")
	if surfaceAdditive.desc.Fragment.Targets[0].Blend == nil || surfaceAdditive.desc.Fragment.Targets[0].Blend.Color.DstFactor != gpu.BlendOne {
		t.Error("surface additive pipeline should use additive color blending")
	}
	worldLine := findPipeline(t, d, "bundle.worldLine")
	if worldLine.desc.Primitive.Topology != gpu.TopologyLineList {
		t.Fatalf("world line topology = %v, want line-list", worldLine.desc.Primitive.Topology)
	}
	if got := len(worldLine.desc.Vertex.Buffers); got != 2 {
		t.Fatalf("world line pipeline should use positions + rgba colors, got %d buffers", got)
	}
	if worldLine.desc.DepthStencil == nil || worldLine.desc.DepthStencil.DepthWriteEnabled {
		t.Fatal("world line pipeline should depth-test without depth writes")
	}

	// Shadow pipeline = 2 vertex buffers (positions + instance only).
	shadowPipe := findPipeline(t, d, "bundle.shadow")
	if got := len(shadowPipe.desc.Vertex.Buffers); got != 2 {
		t.Errorf("shadow: expected 2 vertex buffers (pos+instance), got %d", got)
	}
	if got := shadowPipe.desc.Vertex.Buffers[1].ArrayStride; got != instanceRecordStride {
		t.Errorf("shadow: instance record stride = %d, want %d", got, instanceRecordStride)
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

	// Lit bind group (group 0) has scene, shadow, and environment entries.
	litBG := findBindGroup(t, d, "bundle.lit.bindgroup")
	if got := len(litBG.desc.Entries); got != 5 {
		t.Errorf("lit group-0 bindgroup: expected 5 entries, got %d", got)
	}
	if litBG.desc.Entries[3].TextureView == nil {
		t.Error("lit group-0 bindgroup must include environment cubemap")
	}
}

func TestNewSelectsCompactHDRFormatWhenMemoryBudgetIsTight(t *testing.T) {
	d := newFakeDevice()
	d.formatSupport = map[gpu.TextureFormat]bool{
		gpu.FormatRGBA8Unorm:          true,
		gpu.FormatBGRA8Unorm:          true,
		gpu.FormatDepth24Plus:         true,
		gpu.FormatDepth32Float:        true,
		gpu.FormatR32Uint:             true,
		gpu.FormatRGBA16Float:         true,
		gpu.FormatRGB9E5Ufloat:        true,
		gpu.FormatRGB10A2Unorm:        true,
		gpu.FormatRGBA8UnormSRGB:      true,
		gpu.FormatBGRA8UnormSRGB:      true,
		gpu.FormatDepth16Unorm:        true,
		gpu.FormatDepth24PlusStencil8: true,
	}

	r, err := New(Config{
		Device:               d,
		Surface:              fakeSurface{},
		HDRMemoryBudgetBytes: 1024,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	if r.hdrFormat != gpu.FormatRGB9E5Ufloat {
		t.Fatalf("tight HDR budget selected %v, want RGB9E5", r.hdrFormat)
	}
}

func TestNewBuildsHDR10FXAAShaderForTenBitSurface(t *testing.T) {
	d := newFakeDevice()
	d.format = gpu.FormatRGB10A2Unorm

	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	var found bool
	for _, shader := range d.shaders {
		if shader.label == "bundle.postfx.fxaa311" {
			found = true
			if !strings.Contains(shader.src, "const useHDR10 : bool = true;") {
				t.Fatalf("FXAA shader did not enable HDR10 PQ encode:\n%s", shader.src)
			}
		}
	}
	if !found {
		t.Fatal("FXAA shader was not built")
	}
}

// TestFrameAlwaysEmitsCSMPlusMainPass confirms every non-trivial frame
// records N shadow passes (one per cascade), a main pass, compose pass, and FXAA pass.
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
	if got := len(passes); got != 6 {
		t.Fatalf("expected 6 passes (3 shadow + main + compose + fxaa), got %d", got)
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
	// Present compose tone-maps to an LDR intermediate; color only, no depth.
	present := passes[4]
	if len(present.desc.ColorAttachments) != 1 {
		t.Error("present compose pass must have one color attachment")
	}
	if present.desc.DepthStencilAttachment != nil {
		t.Error("present compose pass must not have a depth attachment")
	}
	fxaa := passes[5]
	if fxaa.desc.Label != "bundle.fxaa311" {
		t.Fatalf("final pass label = %q, want bundle.fxaa311", fxaa.desc.Label)
	}
	if len(fxaa.desc.ColorAttachments) != 1 {
		t.Error("fxaa pass must have one color attachment")
	}
	if fxaa.desc.DepthStencilAttachment != nil {
		t.Error("fxaa pass must not have a depth attachment")
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
		}, {
			Kind: "toneMapping",
			Mode: "reinhard",
			Params: map[string]float64{
				"exposure": 1.2,
			},
		}},
	}
	if err := r.Frame(b, 400, 300, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}

	passes := d.encoders[0].passes
	if got := len(passes); got != 9 {
		t.Fatalf("expected 9 passes (3 shadow + main + 3 bloom + compose + fxaa), got %d", got)
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
	if got := latestWriteBytes(d.queue, "bundle.present.tonemap.uniform"); string(got) != string(float32sToBytes([]float32{1, 1.2, 0, 0})) {
		t.Fatalf("unexpected tone-map uniform bytes: %v", got)
	}
	if got := latestWriteBytes(d.queue, "bundle.bloom.blurH.uniform"); string(got) != string(float32sToBytes([]float32{0.02, 0, 0, 0})) {
		t.Fatalf("unexpected bloom horizontal blur uniform bytes: %v", got)
	}
}

func TestFrameRunsNativeDepthBackedPostEffects(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	b := engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		PostEffects: []engine.RenderPostEffect{
			{Kind: "ssao", Intensity: 0.8, Radius: 9, Params: map[string]float64{"bias": 0.002}},
			{Kind: "vignette", Intensity: 1.2},
			{Kind: "colorGrade", Params: map[string]float64{"exposure": 0.9, "contrast": 1.1, "saturation": 0.75}},
			{Kind: "dof", Params: map[string]float64{"focusDistance": 7, "aperture": 0.06, "maxBlur": 4}},
		},
	}
	if err := r.Frame(b, 400, 300, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}

	passes := d.encoders[0].passes
	if got := len(passes); got != 10 {
		t.Fatalf("expected 10 passes (3 shadow + main + ssao + vignette + colorGrade + dof + compose + fxaa), got %d", got)
	}
	if passes[4].desc.Label != "bundle.postfx.ssao" {
		t.Fatalf("pass 4 label = %q, want bundle.postfx.ssao", passes[4].desc.Label)
	}
	if passes[5].desc.Label != "bundle.postfx.vignette" {
		t.Fatalf("pass 5 label = %q, want bundle.postfx.vignette", passes[5].desc.Label)
	}
	if passes[6].desc.Label != "bundle.postfx.colorGrade" {
		t.Fatalf("pass 6 label = %q, want bundle.postfx.colorGrade", passes[6].desc.Label)
	}
	if passes[7].desc.Label != "bundle.postfx.dof" {
		t.Fatalf("pass 7 label = %q, want bundle.postfx.dof", passes[7].desc.Label)
	}
	if passes[4].desc.DepthStencilAttachment != nil || passes[5].desc.DepthStencilAttachment != nil || passes[6].desc.DepthStencilAttachment != nil || passes[7].desc.DepthStencilAttachment != nil {
		t.Fatal("native post-FX passes must sample depth instead of owning a depth attachment")
	}
	if got := latestWriteBytes(d.queue, "bundle.postfx.ssao.uniform"); string(got) != string(float32sToBytes([]float32{9, 0.8, 0.002, 0})) {
		t.Fatalf("unexpected SSAO uniform bytes: %v", got)
	}
	focusDepth := float32(7.0 / 8.0)
	if got := latestWriteBytes(d.queue, "bundle.postfx.dof.uniform"); string(got) != string(float32sToBytes([]float32{focusDepth, 0.06, 4, 0})) {
		t.Fatalf("unexpected DOF uniform bytes: %v", got)
	}
	if got := latestWriteBytes(d.queue, "bundle.postfx.vignette.uniform"); string(got) != string(float32sToBytes([]float32{1.2, 0, 0, 0})) {
		t.Fatalf("unexpected vignette uniform bytes: %v", got)
	}
	if got := latestWriteBytes(d.queue, "bundle.postfx.colorGrade.uniform"); string(got) != string(float32sToBytes([]float32{0.9, 1.1, 0.75, 0})) {
		t.Fatalf("unexpected color grade uniform bytes: %v", got)
	}
	depth := newestTexture(d, "bundle.depth", 400, 300)
	if depth == nil || !depth.desc.Usage.Has(gpu.TextureUsageTextureBinding) {
		t.Fatalf("depth texture = %#v, want texture-binding depth target", depth)
	}
	if scratch := newestTexture(d, "bundle.nativePostFX.hdr", 400, 300); scratch == nil {
		t.Fatal("expected native post-FX HDR scratch texture")
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
	if len(passes) != 6 {
		t.Fatalf("expected 6 passes (3 shadow + main + compose + fxaa), got %d", len(passes))
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

func TestFrameDrawsNativeTexturedSurfaces(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	b := engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		Materials: []engine.RenderMaterial{
			{Key: "opaque", Color: "#ffffff", Texture: "/surface-opaque.png", Opacity: 1, RenderPass: "opaque"},
			{Key: "far", Color: "#ffffff", Texture: "/surface-far.png", Opacity: 0.5, BlendMode: "alpha", RenderPass: "alpha"},
			{Key: "near", Color: "#ffffff", Texture: "/surface-near.png", Opacity: 0.5, BlendMode: "alpha", RenderPass: "alpha"},
			{Key: "glow", Color: "#ffffff", Texture: "/surface-glow.png", Opacity: 0.75, BlendMode: "additive", RenderPass: "additive"},
			{Key: "pending-html", Color: "#ffffff", Texture: "/pending.png", Opacity: 1, RenderPass: "alpha"},
		},
		Surfaces: []engine.RenderSurface{
			testSurface("opaque", "opaque", 0, 0),
			testSurface("far", "alpha", 1, 20),
			testSurface("near", "alpha", 2, 2),
			testSurface("glow", "additive", 3, 5),
			{
				ID:            "pending-html",
				Kind:          "html",
				SourceKind:    "html",
				TextureReady:  false,
				MaterialIndex: 4,
				RenderPass:    "alpha",
				Positions:     testSurfacePositions(),
				UV:            testSurfaceUV(),
				VertexCount:   6,
			},
		},
	}
	if err := r.Frame(b, 400, 300, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	passes := d.encoders[0].passes
	if len(passes) != 6 {
		t.Fatalf("expected 6 passes (3 shadow + main + compose + fxaa), got %d", len(passes))
	}
	mainPass := passes[3]
	if got := len(mainPass.draws); got != 4 {
		t.Fatalf("main pass surface draws = %d, want 4; pipelines=%v vbufs=%v", got, mainPass.pipelineLabels, mainPass.vbufLabels)
	}
	for _, label := range []string{"bundle.surface.opaque", "bundle.surface.alpha", "bundle.surface.additive"} {
		if !stringSliceContains(mainPass.pipelineLabels, label) {
			t.Fatalf("main pass pipelines %v did not include %s", mainPass.pipelineLabels, label)
		}
	}
	wantPositionOrder := []string{"opaque", "far", "near", "glow"}
	var gotPositionOrder []string
	for _, label := range mainPass.vbufLabels {
		if !strings.HasPrefix(label, "bundle.surface.positions:") {
			continue
		}
		for _, id := range wantPositionOrder {
			if strings.Contains(label, ":"+id+":") {
				gotPositionOrder = append(gotPositionOrder, id)
				break
			}
		}
	}
	if strings.Join(gotPositionOrder, ",") != strings.Join(wantPositionOrder, ",") {
		t.Fatalf("surface draw order = %v, want %v from opaque then far-to-near alpha then additive", gotPositionOrder, wantPositionOrder)
	}
	for _, url := range []string{"/surface-opaque.png", "/surface-far.png", "/surface-near.png", "/surface-glow.png"} {
		if _, ok := r.textureCache[url]; !ok {
			t.Fatalf("surface texture %q was not resolved into the native texture cache", url)
		}
	}
	pickBytes := latestWriteBytesPrefix(d.queue, "bundle.surface.pickIDs:0:opaque")
	if len(pickBytes) != 6*4 {
		t.Fatalf("surface pick-id bytes len = %d, want %d", len(pickBytes), 6*4)
	}
	if pickID := binary.LittleEndian.Uint32(pickBytes[:4]); pickID == 0 {
		t.Fatal("surface pick-id should be nonzero for native picking")
	}
	if _, ok := r.textureCache["/pending.png"]; ok {
		t.Fatal("pending HTML texture surface should not resolve its texture")
	}
}

func TestFrameDrawsNativeWorldLines(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	b := engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		WorldPositions: []float64{
			-1, 0, 0,
			1, 0, 0,
			0, -1, 0,
			0, 1, 0,
		},
		WorldColors: []float64{
			1, 0, 0, 1,
			0, 1, 0, 1,
			0, 0, 1, 0.5,
			1, 1, 0, 0.5,
		},
		WorldVertexCount: 4,
	}
	if err := r.Frame(b, 400, 300, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	mainPass := d.encoders[0].passes[3]
	if !stringSliceContains(mainPass.pipelineLabels, "bundle.worldLine") {
		t.Fatalf("main pass pipelines %v did not include native world line pipeline", mainPass.pipelineLabels)
	}
	if got := mainPass.draws[len(mainPass.draws)-1].vertexCount; got != 4 {
		t.Fatalf("world line draw vertex count = %d, want 4", got)
	}
	if r.worldLineCache == nil || r.worldLineCache.vertexCount != 4 {
		t.Fatalf("world line cache = %#v, want 4 vertices", r.worldLineCache)
	}
	if got := latestWriteBytes(d.queue, "bundle.worldLine.colors"); string(got) != string(float64sToFloat32Bytes(b.WorldColors)) {
		t.Fatalf("world line color upload mismatch: %v", got)
	}
}

func TestFrameDrawsNativeWorldObjectMeshes(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	pickable := true
	b := engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		Materials: []engine.RenderMaterial{{
			Kind:       "standard",
			Color:      "#77c6ff",
			Roughness:  0.35,
			Metalness:  0.2,
			RenderPass: "opaque",
		}},
		Objects: []engine.RenderObject{
			{
				ID:            "mesh-object",
				Kind:          "model-mesh",
				Pickable:      &pickable,
				CastShadow:    true,
				MaterialIndex: 0,
				RenderPass:    "opaque",
				VertexOffset:  0,
				VertexCount:   3,
			},
			{
				ID:           "line-object",
				Kind:         "helper-line",
				VertexOffset: 3,
				VertexCount:  2,
			},
		},
		WorldPositions: []float64{
			-1, -1, 0,
			1, -1, 0,
			0, 1, 0,
			-1, 0, 0,
			1, 0, 0,
		},
		WorldNormals: []float64{
			0, 0, 1,
			0, 0, 1,
			0, 0, 1,
		},
		WorldUVs: []float64{
			0, 1,
			1, 1,
			0.5, 0,
		},
		WorldColors: []float64{
			1, 0, 0, 1,
			0, 1, 0, 1,
			0, 0, 1, 1,
			1, 1, 0, 1,
			0, 1, 1, 1,
		},
		WorldVertexCount: 5,
	}
	if err := r.Frame(b, 400, 300, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	passes := d.encoders[0].passes
	for i := 0; i < 3; i++ {
		if got := len(passes[i].draws); got != 1 {
			t.Fatalf("shadow cascade %d object draws = %d, want 1", i, got)
		}
	}
	mainPass := passes[3]
	if !stringSliceContains(mainPass.pipelineLabels, "bundle.lit") {
		t.Fatalf("main pass pipelines %v did not include lit object pipeline", mainPass.pipelineLabels)
	}
	if !stringSliceContains(mainPass.pipelineLabels, "bundle.worldLine") {
		t.Fatalf("main pass pipelines %v did not include world-line pipeline for non-mesh object", mainPass.pipelineLabels)
	}
	if !stringSliceContains(mainPass.vbufLabels, "bundle.object.positions:0:mesh-object:0:0:3") {
		t.Fatalf("main pass vertex buffers %v did not include native object positions", mainPass.vbufLabels)
	}
	if r.worldLineCache == nil || r.worldLineCache.vertexCount != 2 {
		t.Fatalf("world line cache = %#v, want only the two non-mesh vertices", r.worldLineCache)
	}
	instanceBytes := latestWriteBytesPrefix(d.queue, "bundle.object.instance:0:mesh-object")
	if len(instanceBytes) != instanceRecordStride {
		t.Fatalf("object instance bytes len = %d, want %d", len(instanceBytes), instanceRecordStride)
	}
	if pickID := binary.LittleEndian.Uint32(instanceBytes[64:68]); pickID == 0 {
		t.Fatalf("object pick id = %d, want nonzero", pickID)
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
	// Main pass (HDR + id targets) is at index 3; post-FX follows it.
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

func testSurface(id, renderPass string, materialIndex int, depthCenter float64) engine.RenderSurface {
	return engine.RenderSurface{
		ID:            id,
		Kind:          "plane",
		MaterialIndex: materialIndex,
		RenderPass:    renderPass,
		Positions:     testSurfacePositions(),
		UV:            testSurfaceUV(),
		VertexCount:   6,
		DepthCenter:   depthCenter,
	}
}

func testSurfacePositions() []float64 {
	return []float64{
		-1, -1, 0,
		1, -1, 0,
		1, 1, 0,
		-1, -1, 0,
		1, 1, 0,
		-1, 1, 0,
	}
}

func testSurfaceUV() []float64 {
	return []float64{
		0, 1,
		1, 1,
		1, 0,
		0, 1,
		1, 0,
		0, 0,
	}
}

func stringSliceContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func findPipeline(t *testing.T, d *fakeDevice, label string) *fakePipeline {
	t.Helper()
	for _, pipeline := range d.pipelines {
		if pipeline.desc.Label == label {
			return pipeline
		}
	}
	t.Fatalf("pipeline %q was not built", label)
	return nil
}

func findBindGroup(t *testing.T, d *fakeDevice, label string) *fakeBindGroup {
	t.Helper()
	for _, bg := range d.bindGroups {
		if bg.desc.Label == label {
			return bg
		}
	}
	t.Fatalf("bind group %q was not built", label)
	return nil
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
