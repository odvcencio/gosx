package bundle

import (
	"testing"

	"github.com/odvcencio/gosx/render/gpu"
)

func TestEnsureHDRPreservesOldResourcesOnIDBufferFailure(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	if _, err := r.ensureHDR(64, 64); err != nil {
		t.Fatalf("ensureHDR initial: %v", err)
	}
	oldHDR := r.hdrTex.(*fakeTexture)
	oldID := r.idBufferTex.(*fakeTexture)

	d.failTextureLabel = "bundle.pickIdBuffer"
	if _, err := r.ensureHDR(128, 128); err == nil {
		t.Fatal("ensureHDR resize succeeded despite id-buffer allocation failure")
	}

	if r.hdrTex != oldHDR || r.idBufferTex != oldID {
		t.Fatal("ensureHDR swapped renderer state after failed replacement")
	}
	if oldHDR.destroyed || oldID.destroyed {
		t.Fatal("ensureHDR destroyed old resources after failed replacement")
	}
	if replacement := newestTexture(d, "bundle.hdr", 128, 128); replacement == nil || !replacement.destroyed {
		t.Fatalf("replacement HDR texture = %#v, want destroyed", replacement)
	}
}

func TestEnsurePostFXPreservesOldResourcesOnBindGroupFailure(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	if err := r.ensurePostFX(64, 64); err != nil {
		t.Fatalf("ensurePostFX initial: %v", err)
	}
	oldPostFX := r.postFXTex.(*fakeTexture)
	oldBindGroup := r.fxaaBindGrp.(*fakeBindGroup)

	d.failBindLabel = "bundle.fxaa311.bg"
	if err := r.ensurePostFX(128, 128); err == nil {
		t.Fatal("ensurePostFX resize succeeded despite bind-group failure")
	}

	if r.postFXTex != oldPostFX || r.fxaaBindGrp != oldBindGroup {
		t.Fatal("ensurePostFX swapped renderer state after failed replacement")
	}
	if oldPostFX.destroyed || oldBindGroup.destroyed {
		t.Fatal("ensurePostFX destroyed old resources after failed replacement")
	}
	if replacement := newestTexture(d, "bundle.postfx.ldr", 128, 128); replacement == nil || !replacement.destroyed {
		t.Fatalf("replacement post-FX texture = %#v, want destroyed", replacement)
	}
}

func TestEnsureBloomPreservesOldResourcesOnComposeFailure(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	if _, err := r.ensureHDR(64, 64); err != nil {
		t.Fatalf("ensureHDR initial: %v", err)
	}
	cfg := bloomConfig{enabled: true, threshold: defaultBloomThreshold, intensity: defaultBloomIntensity, radius: defaultBloomRadius, scale: defaultBloomScale}
	if err := r.ensureBloom(64, 64, cfg); err != nil {
		t.Fatalf("ensureBloom initial: %v", err)
	}
	oldBloom := r.bloom
	oldPresent := r.presentBindGrp.(*fakeBindGroup)

	d.failBindLabel = "bundle.present.compose.bg"
	if err := r.ensureBloom(128, 128, cfg); err == nil {
		t.Fatal("ensureBloom resize succeeded despite compose bind-group failure")
	}

	if r.bloom != oldBloom || r.presentBindGrp != oldPresent {
		t.Fatal("ensureBloom swapped renderer state after failed replacement")
	}
	if oldBloom.texA.(*fakeTexture).destroyed || oldBloom.texB.(*fakeTexture).destroyed || oldPresent.destroyed {
		t.Fatal("ensureBloom destroyed old resources after failed replacement")
	}
	if replacement := newestTexture(d, "bundle.bloom.A", 64, 64); replacement == nil || !replacement.destroyed {
		t.Fatalf("replacement bloom A = %#v, want destroyed", replacement)
	}
	for _, label := range []string{"bundle.bloom.bright.bg", "bundle.bloom.blurH.bg", "bundle.bloom.blurV.bg"} {
		bg := newestBindGroup(d, label)
		if bg == nil || !bg.destroyed {
			t.Fatalf("%s replacement bind group = %#v, want destroyed", label, bg)
		}
	}
}

func newestTexture(d *fakeDevice, label string, width, height int) *fakeTexture {
	for i := len(d.textures) - 1; i >= 0; i-- {
		tex := d.textures[i]
		if tex.desc.Label == label && tex.desc.Width == width && tex.desc.Height == height {
			return tex
		}
	}
	return nil
}

func newestBindGroup(d *fakeDevice, label string) *fakeBindGroup {
	for i := len(d.bindGroups) - 1; i >= 0; i-- {
		bg := d.bindGroups[i]
		if bg.desc.Label == label {
			return bg
		}
	}
	return nil
}

func TestDestroyReleasesPresentBindGroups(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := r.ensureHDR(64, 64); err != nil {
		t.Fatalf("ensureHDR: %v", err)
	}
	if err := r.ensurePostFX(64, 64); err != nil {
		t.Fatalf("ensurePostFX: %v", err)
	}
	if err := r.ensureBloom(64, 64, bloomConfig{scale: defaultBloomScale}); err != nil {
		t.Fatalf("ensureBloom: %v", err)
	}
	present := r.presentBindGrp.(*fakeBindGroup)
	fxaa := r.fxaaBindGrp.(*fakeBindGroup)

	r.Destroy()

	if !present.destroyed || !fxaa.destroyed {
		t.Fatalf("present destroyed=%v fxaa destroyed=%v, want both destroyed", present.destroyed, fxaa.destroyed)
	}
}

var _ gpu.BindGroup = (*fakeBindGroup)(nil)
