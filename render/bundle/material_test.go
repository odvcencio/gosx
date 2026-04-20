package bundle

import (
	"testing"

	"github.com/odvcencio/gosx/engine"
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
