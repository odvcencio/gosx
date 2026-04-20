package bundle

import "testing"

func TestCreateTextureFromRGBAGeneratesMipChain(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	pixels := checkerBytes(4, [3]byte{255, 0, 0}, [3]byte{0, 0, 255})
	if _, err := r.createTextureFromRGBA(pixels, 4, 4, "bundle.texture.test"); err != nil {
		t.Fatalf("createTextureFromRGBA: %v", err)
	}

	tex := d.textures[len(d.textures)-1]
	if got := tex.desc.MipLevelCount; got != 3 {
		t.Fatalf("MipLevelCount = %d, want 3", got)
	}
	var levels []int
	for _, write := range d.queue.textureWrites {
		levels = append(levels, write.mipLevel)
	}
	if len(levels) < 3 || levels[len(levels)-3] != 0 || levels[len(levels)-2] != 1 || levels[len(levels)-1] != 2 {
		t.Fatalf("texture writes levels = %v, want trailing [0 1 2]", levels)
	}
}

func TestGenerateRGBAMipChainDownsamplesToOnePixel(t *testing.T) {
	base := []byte{
		0, 0, 0, 255, 100, 0, 0, 255,
		0, 100, 0, 255, 100, 100, 0, 255,
	}
	mips := generateRGBAMipChain(base, 2, 2)
	if len(mips) != 2 {
		t.Fatalf("mip count = %d, want 2", len(mips))
	}
	got := mips[1].Pixels
	want := []byte{50, 50, 0, 255}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mip pixel byte %d = %d, want %d", i, got[i], want[i])
		}
	}
}
