package bundle

import (
	"testing"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/gpu"
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
	// uniform + baseColor tex + sampler + normal + sampler + rough + metal + emissive = 8.
	if got := len(bg.desc.Entries); got != 8 {
		t.Fatalf("material bind group entries = %d, want 8", got)
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
	if len(bg.desc.Entries) != 5 {
		t.Fatalf("lit bind group entries = %d, want 5", len(bg.desc.Entries))
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
