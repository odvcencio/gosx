package bundle

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/bundle/ktx2"
	"github.com/odvcencio/gosx/render/gpu"
)

func TestLoadKTX2TextureUploadsCompressedFormat(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	payload := make([]byte, 32)
	data := buildBundleKTX2(ktx2.VkFormatBC7SRGBBlock, 8, 4, 1, 1, 1, [][]byte{payload})
	writesBefore := len(d.queue.textureWrites)
	res, err := r.LoadKTX2Texture(data)
	if err != nil {
		t.Fatalf("LoadKTX2Texture: %v", err)
	}

	tex := d.textures[len(d.textures)-1]
	if tex.desc.Format != gpu.FormatBC7RGBAUnormSRGB {
		t.Fatalf("texture format = %v, want %v", tex.desc.Format, gpu.FormatBC7RGBAUnormSRGB)
	}
	if tex.desc.DepthOrArrayLayers != 1 || tex.desc.Dimension != gpu.TextureDimension2D {
		t.Fatalf("texture layout: depthOrLayers=%d dimension=%v", tex.desc.DepthOrArrayLayers, tex.desc.Dimension)
	}
	if _, ok := res.view.(*fakeTextureView); !ok {
		t.Fatalf("view type = %T, want *fakeTextureView", res.view)
	}

	writes := d.queue.textureWrites[writesBefore:]
	if len(writes) != 1 {
		t.Fatalf("writes = %d, want 1", len(writes))
	}
	if got := writes[0]; got.mipLevel != 0 || got.layer != 0 || got.bytes != 32 ||
		got.bytesPerRow != 32 || got.rowsPerImage != 1 || got.width != 8 || got.height != 4 {
		t.Fatalf("write = %+v, want BC7 8x4 one-row block upload", got)
	}
}

func TestLoadKTX2TextureCreatesCubeArrayViewAndUploadsLayers(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	payload := make([]byte, 12*4)
	data := buildBundleKTX2(ktx2.VkFormatR8G8B8A8Unorm, 1, 1, 1, 2, 6, [][]byte{payload})
	writesBefore := len(d.queue.textureWrites)
	res, err := r.LoadKTX2Texture(data)
	if err != nil {
		t.Fatalf("LoadKTX2Texture: %v", err)
	}

	tex := d.textures[len(d.textures)-1]
	if tex.desc.DepthOrArrayLayers != 12 {
		t.Fatalf("DepthOrArrayLayers = %d, want 12", tex.desc.DepthOrArrayLayers)
	}
	view := res.view.(*fakeTextureView)
	if view.desc.Dimension != gpu.TextureViewDimensionCubeArray || view.desc.ArrayLayerCount != 12 {
		t.Fatalf("view desc = %+v, want cube-array over 12 layers", view.desc)
	}
	writes := d.queue.textureWrites[writesBefore:]
	if len(writes) != 12 {
		t.Fatalf("writes = %d, want 12", len(writes))
	}
	for i, write := range writes {
		if write.layer != i || write.bytes != 4 || write.bytesPerRow != 4 || write.rowsPerImage != 1 {
			t.Fatalf("write %d = %+v", i, write)
		}
	}
}

func TestRegisterKTX2TextureFeedsEnvironmentCubemap(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	payload := make([]byte, 6*4)
	data := buildBundleKTX2(ktx2.VkFormatR8G8B8A8Unorm, 1, 1, 1, 1, 6, [][]byte{payload})
	if err := r.RegisterKTX2Texture("studio.ktx2", data); err != nil {
		t.Fatalf("RegisterKTX2Texture: %v", err)
	}
	if err := r.ensureEnvironmentBindGroups(engine.RenderEnvironment{EnvMap: "studio.ktx2"}); err != nil {
		t.Fatalf("ensureEnvironmentBindGroups: %v", err)
	}

	bg := r.litBindGrp.(*fakeBindGroup)
	view := bg.desc.Entries[3].TextureView.(*fakeTextureView)
	if view.desc.Dimension != gpu.TextureViewDimensionCube || view.desc.ArrayLayerCount != 6 {
		t.Fatalf("environment view desc = %+v, want registered cube", view.desc)
	}
}

func TestLoadKTX2TextureCreates3DTextureViewAndUploadsSlices(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	payload := make([]byte, 2*2*3*4)
	data := buildBundleKTX2(ktx2.VkFormatR8G8B8A8Unorm, 2, 2, 3, 1, 1, [][]byte{payload})
	writesBefore := len(d.queue.textureWrites)
	res, err := r.LoadKTX2Texture(data)
	if err != nil {
		t.Fatalf("LoadKTX2Texture: %v", err)
	}

	tex := d.textures[len(d.textures)-1]
	if tex.desc.Dimension != gpu.TextureDimension3D || tex.desc.DepthOrArrayLayers != 3 {
		t.Fatalf("texture desc = %+v, want 3D depth 3", tex.desc)
	}
	view := res.view.(*fakeTextureView)
	if view.desc.Dimension != gpu.TextureViewDimension3D || view.desc.ArrayLayerCount != 0 {
		t.Fatalf("view desc = %+v, want 3D view without array layer count", view.desc)
	}
	writes := d.queue.textureWrites[writesBefore:]
	if len(writes) != 3 {
		t.Fatalf("writes = %d, want 3", len(writes))
	}
	for i, write := range writes {
		if write.layer != i || write.bytes != 16 || write.bytesPerRow != 8 || write.rowsPerImage != 2 {
			t.Fatalf("write %d = %+v", i, write)
		}
	}
}

func buildBundleKTX2(vkFormat int, width, height, depth, layers, faces uint32, levelData [][]byte) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xAB, 'K', 'T', 'X', ' ', '2', '0', 0xBB, 0x0D, 0x0A, 0x1A, 0x0A})

	header := make([]byte, 68)
	put32 := func(off int, v uint32) { binary.LittleEndian.PutUint32(header[off:], v) }
	put64 := func(off int, v uint64) { binary.LittleEndian.PutUint64(header[off:], v) }
	put32(0, uint32(vkFormat))
	put32(4, 1)
	put32(8, width)
	put32(12, height)
	put32(16, depth)
	put32(20, layers)
	put32(24, faces)
	put32(28, uint32(len(levelData)))
	put32(32, 0)
	put32(36, 0)
	put32(40, 0)
	put32(44, 0)
	put32(48, 0)
	put64(52, 0)
	put64(60, 0)
	buf.Write(header)

	indexStart := buf.Len()
	dataStart := indexStart + len(levelData)*24
	index := make([]byte, len(levelData)*24)
	running := uint64(dataStart)
	for i, level := range levelData {
		binary.LittleEndian.PutUint64(index[i*24+0:], running)
		binary.LittleEndian.PutUint64(index[i*24+8:], uint64(len(level)))
		binary.LittleEndian.PutUint64(index[i*24+16:], uint64(len(level)))
		running += uint64(len(level))
	}
	buf.Write(index)
	for _, level := range levelData {
		buf.Write(level)
	}
	return buf.Bytes()
}
