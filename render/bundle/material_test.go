package bundle

import (
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/gpu"
)

func TestMaterialBindGroupIncludesNormalMap(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	fp := materialFromRender(engine.RenderMaterial{
		Color:     "#ffffff",
		Texture:   "/albedo.png",
		NormalMap: "/normal.png",
	})
	res, err := r.ensureMaterial(fp)
	if err != nil {
		t.Fatalf("ensureMaterial: %v", err)
	}
	bg, ok := res.bindGroup.(*fakeBindGroup)
	if !ok {
		t.Fatalf("material bind group type = %T", res.bindGroup)
	}
	// uniform + baseColor tex + sampler + normal + sampler + rough + metal + emissive + occlusion = 9.
	if got := len(bg.desc.Entries); got != 9 {
		t.Fatalf("material bind group entries = %d, want 9", got)
	}
	if bg.desc.Entries[3].TextureView == nil {
		t.Fatal("normal-map texture view was not bound")
	}
	if bg.desc.Entries[4].Sampler == nil {
		t.Fatal("normal-map sampler was not bound")
	}
	if bg.desc.Entries[5].TextureView == nil {
		t.Fatal("roughness-map texture view was not bound")
	}
	if bg.desc.Entries[6].TextureView == nil {
		t.Fatal("metalness-map texture view was not bound")
	}
	if bg.desc.Entries[7].TextureView == nil {
		t.Fatal("emissive-map texture view was not bound")
	}
	if bg.desc.Entries[8].TextureView == nil {
		t.Fatal("occlusion-map texture view was not bound")
	}
	if _, ok := r.textureCache["/normal.png"]; !ok {
		t.Fatal("normal-map texture was not loaded into the texture cache")
	}
}

func TestMaterialUniformFlagsNormalMap(t *testing.T) {
	fp := materialFromRender(engine.RenderMaterial{
		Texture:   "/albedo.png",
		NormalMap: "/normal.png",
	})
	got := materialUniformBytes(fp)[48:64]
	want := float32sToBytes([]float32{1, 1, 0, 0})
	if string(got) != string(want) {
		t.Fatalf("textureParams bytes = %v, want %v", got, want)
	}
}

func TestMaterialUniformFlagsOcclusionMap(t *testing.T) {
	fp := materialFromRender(engine.RenderMaterial{
		Texture:      "/albedo.png",
		OcclusionMap: "/occlusion.png",
	})
	got := materialUniformBytes(fp)[64:80]
	want := float32sToBytes([]float32{0, 1, 0, 0})
	if string(got) != string(want) {
		t.Fatalf("textureParams2 bytes = %v, want %v", got, want)
	}
}

// TestMaterialFingerprintDiffersOnOcclusionMap keeps the occlusion map a
// distinguishing part of the material cache key. Two materials that differ
// only by OcclusionMap must not share a cached uniform buffer and bind group,
// or one mesh would render with the other's AO texture.
func TestMaterialFingerprintDiffersOnOcclusionMap(t *testing.T) {
	base := materialFromRender(engine.RenderMaterial{Color: "#ffffff"})
	withMap := materialFromRender(engine.RenderMaterial{Color: "#ffffff", OcclusionMap: "/occlusion.png"})
	if base == withMap {
		t.Fatal("materialFingerprint must differ when only OcclusionMap differs")
	}
}

func TestMaterialUniformFlagsWireframe(t *testing.T) {
	fp := materialFromRender(engine.RenderMaterial{
		Texture:   "/albedo.png",
		Wireframe: true,
	})
	got := materialUniformBytes(fp)[64:80]
	want := float32sToBytes([]float32{0, 0, 1, 0})
	if string(got) != string(want) {
		t.Fatalf("textureParams2 bytes = %v, want %v", got, want)
	}
}

// TestMaterialFingerprintDiffersOnWireframe is the lane
// render/gpu/headless/material_gap_test.go's pinning test named as missing
// (material_gap_test.go:369, before this PR). Two materials that differ only
// by Wireframe must not share a cached uniform buffer, or one mesh would draw
// with the other's wireframe state.
func TestMaterialFingerprintDiffersOnWireframe(t *testing.T) {
	base := materialFromRender(engine.RenderMaterial{Color: "#ffffff"})
	wireframe := materialFromRender(engine.RenderMaterial{Color: "#ffffff", Wireframe: true})
	if base == wireframe {
		t.Fatal("materialFingerprint must differ when only Wireframe differs")
	}
}

func TestMaterialUniformEncodesOpacity(t *testing.T) {
	fp := materialFromRender(engine.RenderMaterial{
		Color:   "#ffffff",
		Opacity: 0.42,
	})
	got := materialUniformBytes(fp)[0:16]
	want := float32sToBytes([]float32{1, 1, 1, dequantize(quantize(0.42))})
	if string(got) != string(want) {
		t.Fatalf("baseColor bytes = %v, want %v", got, want)
	}

	defaultFP := materialFromRender(engine.RenderMaterial{Color: "#ffffff"})
	if got := materialUniformBytes(defaultFP)[0:16]; string(got) != string(float32sToBytes([]float32{1, 1, 1, 1})) {
		t.Fatalf("default opacity bytes = %v, want alpha 1", got)
	}

	invisibleFP := materialFromRender(engine.RenderMaterial{
		Color:     "#ffffff",
		BlendMode: "alpha",
		Opacity:   0,
	})
	if got := materialUniformBytes(invisibleFP)[0:16]; string(got) != string(float32sToBytes([]float32{1, 1, 1, 0})) {
		t.Fatalf("explicit zero opacity bytes = %v, want alpha 0", got)
	}
}

func TestMaterialUniformEncodesPhysicalMaterialFields(t *testing.T) {
	fp := materialFromRender(engine.RenderMaterial{
		Color:        "#ffffff",
		Clearcoat:    0.35,
		Sheen:        0.2,
		Transmission: 0.12,
		Iridescence:  0.18,
		Anisotropy:   -0.25,
	})
	gotPhysical := materialUniformBytes(fp)[80:96]
	wantPhysical := float32sToBytes([]float32{
		dequantize(quantize(0.35)),
		dequantize(quantize(0.2)),
		dequantize(quantize(0.12)),
		dequantize(quantize(0.18)),
	})
	if string(gotPhysical) != string(wantPhysical) {
		t.Fatalf("physicalParams bytes = %v, want %v", gotPhysical, wantPhysical)
	}
	gotPhysical2 := materialUniformBytes(fp)[96:112]
	wantPhysical2 := float32sToBytes([]float32{dequantizeSignedUnit(quantizeSignedUnit(-0.25)), 0, 0, 0})
	if string(gotPhysical2) != string(wantPhysical2) {
		t.Fatalf("physicalParams2 bytes = %v, want %v", gotPhysical2, wantPhysical2)
	}

	plain := materialFromRender(engine.RenderMaterial{Color: "#ffffff"})
	if fp == plain {
		t.Fatal("physical material fields should participate in the material fingerprint")
	}
}

func TestLitPipelinesEnableSourceAlphaBlend(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	for _, label := range []string{"bundle.lit", "bundle.lit.skinned"} {
		pipeline := findRenderPipeline(t, d, label)
		if len(pipeline.desc.Fragment.Targets) < 2 {
			t.Fatalf("%s targets = %d, want at least 2", label, len(pipeline.desc.Fragment.Targets))
		}
		blend := pipeline.desc.Fragment.Targets[0].Blend
		if blend == nil {
			t.Fatalf("%s color target blend = nil", label)
		}
		if blend.Color.SrcFactor != gpu.BlendSrcAlpha || blend.Color.DstFactor != gpu.BlendOneMinusSrcAlpha || blend.Color.Operation != gpu.BlendOpAdd {
			t.Fatalf("%s color blend = %#v", label, blend.Color)
		}
		if blend.Alpha.SrcFactor != gpu.BlendOne || blend.Alpha.DstFactor != gpu.BlendOneMinusSrcAlpha || blend.Alpha.Operation != gpu.BlendOpAdd {
			t.Fatalf("%s alpha blend = %#v", label, blend.Alpha)
		}
		if pipeline.desc.Fragment.Targets[1].Blend != nil {
			t.Fatalf("%s pick target unexpectedly blends", label)
		}
	}
}

func findRenderPipeline(t *testing.T, d *fakeDevice, label string) *fakePipeline {
	t.Helper()
	for _, pipeline := range d.pipelines {
		if pipeline.desc.Label == label {
			return pipeline
		}
	}
	t.Fatalf("pipeline %q not found", label)
	return nil
}

func TestEnvironmentMapRebuildsLitBindGroupWithCubeView(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	if err := r.ensureEnvironmentBindGroups(engine.RenderEnvironment{
		EnvMap:       "/env/studio.ktx2",
		EnvIntensity: 1.25,
		EnvRotation:  0.5,
	}); err != nil {
		t.Fatalf("ensureEnvironmentBindGroups: %v", err)
	}

	bg := r.litBindGrp.(*fakeBindGroup)
	// Six entries: scene uniform, shadow array, shadow sampler, environment
	// cube, material sampler, scene light storage buffer.
	if len(bg.desc.Entries) != 6 {
		t.Fatalf("lit bind group entries = %d, want 6", len(bg.desc.Entries))
	}
	view := bg.desc.Entries[3].TextureView.(*fakeTextureView)
	if view.desc.Dimension != gpu.TextureViewDimensionCube {
		t.Fatalf("environment view dimension = %v, want cube", view.desc.Dimension)
	}
}

func TestEnvironmentParamsEncodeCubemapControls(t *testing.T) {
	got := environmentParams(engine.RenderEnvironment{
		EnvMap:       "studio.ktx2",
		EnvIntensity: 1.25,
		EnvRotation:  0.5,
	})
	want := [4]float32{1.25, 0.5, 1, 0}
	if got != want {
		t.Fatalf("environment params = %#v, want %#v", got, want)
	}
	if empty := environmentParams(engine.RenderEnvironment{}); empty != ([4]float32{}) {
		t.Fatalf("empty environment params = %#v, want zero", empty)
	}
}
