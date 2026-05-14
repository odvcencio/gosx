package bundle

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"
)

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

func TestEnsureMaterialTextureDecodesDataURL(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	img.SetRGBA(1, 0, color.RGBA{G: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	key := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	if _, err := r.ensureMaterialTexture(key); err != nil {
		t.Fatalf("ensureMaterialTexture: %v", err)
	}
	if _, ok := r.textureCache[key]; !ok {
		t.Fatal("decoded data URL was not stored in the texture cache")
	}
	tex := d.textures[len(d.textures)-1]
	if tex.desc.Width != 2 || tex.desc.Height != 1 {
		t.Fatalf("decoded texture size = %dx%d, want 2x1", tex.desc.Width, tex.desc.Height)
	}
	if len(d.queue.textureWrites) == 0 || d.queue.textureWrites[len(d.queue.textureWrites)-1].width != 1 {
		t.Fatalf("expected mip upload writes, got %#v", d.queue.textureWrites)
	}
}

func TestRegisterRGBATextureCachesDecodedPixels(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	if err := r.RegisterRGBATexture("hud", []byte{
		255, 0, 0, 255,
		0, 255, 0, 255,
		0, 0, 255, 255,
		255, 255, 255, 255,
	}, 2, 2); err != nil {
		t.Fatalf("RegisterRGBATexture: %v", err)
	}
	tex, err := r.ensureMaterialTexture("hud")
	if err != nil {
		t.Fatalf("ensureMaterialTexture: %v", err)
	}
	if tex != r.textureCache["hud"] {
		t.Fatal("registered texture was not reused for material lookup")
	}
}
