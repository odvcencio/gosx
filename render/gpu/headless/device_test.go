package headless

import (
	"encoding/binary"
	"image/color"
	"math"
	"testing"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/bundle"
	"github.com/odvcencio/gosx/render/gpu"
)

// TestFramebufferReflectsClearValue verifies that a render pass with a
// color-clear on the surface attachment writes the clear color into the
// headless framebuffer.
func TestFramebufferReflectsClearValue(t *testing.T) {
	d, surface := New(4, 4)

	view, err := d.AcquireSurfaceView(surface)
	if err != nil {
		t.Fatalf("AcquireSurfaceView: %v", err)
	}
	enc := d.CreateCommandEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.RenderPassColorAttachment{{
			View:       view,
			LoadOp:     gpu.LoadOpClear,
			StoreOp:    gpu.StoreOpStore,
			ClearValue: gpu.Color{R: 1.0, G: 0.5, B: 0.0, A: 1.0},
		}},
	})
	pass.End()
	d.Queue().Submit(enc.Finish())

	fb := d.Framebuffer()
	got := fb.RGBAAt(2, 2)
	want := color.RGBA{R: 255, G: 127, B: 0, A: 255}
	if got.R != want.R || got.G != want.G || got.B != want.B || got.A != want.A {
		t.Errorf("framebuffer pixel: want %+v, got %+v", want, got)
	}
}

// TestBufferWriteReadRoundTrip verifies that WriteBuffer + ReadAsync on a
// headless buffer round-trip bytes correctly. Confirms the path used by
// pick readback tests on the headless backend.
func TestBufferWriteReadRoundTrip(t *testing.T) {
	d, _ := New(2, 2)
	buf, err := d.CreateBuffer(gpu.BufferDesc{
		Size:  16,
		Usage: gpu.BufferUsageMapRead | gpu.BufferUsageCopyDst,
	})
	if err != nil {
		t.Fatalf("CreateBuffer: %v", err)
	}
	input := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	d.Queue().WriteBuffer(buf, 0, input)

	got, err := buf.ReadAsync(len(input))
	if err != nil {
		t.Fatalf("ReadAsync: %v", err)
	}
	for i, v := range input {
		if got[i] != v {
			t.Errorf("byte %d: want %d, got %d", i, v, got[i])
		}
	}
}

// TestTextureWriteCopyRoundTrip verifies that the headless backend keeps CPU
// texture bytes precise enough for copyTextureToBuffer readbacks.
func TestTextureWriteCopyRoundTrip(t *testing.T) {
	d, _ := New(1, 1)
	tex, err := d.CreateTexture(gpu.TextureDesc{
		Width:  2,
		Height: 2,
		Format: gpu.FormatRGBA8Unorm,
		Usage:  gpu.TextureUsageCopyDst | gpu.TextureUsageCopySrc,
	})
	if err != nil {
		t.Fatalf("CreateTexture: %v", err)
	}
	pixels := []byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
	}
	d.Queue().WriteTexture(tex, pixels, 8, 2, 2)

	buf, err := d.CreateBuffer(gpu.BufferDesc{
		Size:  256,
		Usage: gpu.BufferUsageMapRead | gpu.BufferUsageCopyDst,
	})
	if err != nil {
		t.Fatalf("CreateBuffer: %v", err)
	}
	enc := d.CreateCommandEncoder()
	enc.CopyTextureToBuffer(
		gpu.TextureCopyInfo{Texture: tex, Origin: [3]int{1, 1, 0}},
		gpu.BufferCopyInfo{Buffer: buf, BytesPerRow: 256, RowsPerImage: 1},
		1, 1, 1,
	)
	d.Queue().Submit(enc.Finish())

	got, err := buf.ReadAsync(4)
	if err != nil {
		t.Fatalf("ReadAsync: %v", err)
	}
	want := []byte{13, 14, 15, 16}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("copied byte %d: want %d, got %d", i, want[i], got[i])
		}
	}
}

func TestTextureWriteCopyRoundTripFromMipLevel(t *testing.T) {
	d, _ := New(1, 1)
	tex, err := d.CreateTexture(gpu.TextureDesc{
		Width:         4,
		Height:        4,
		Format:        gpu.FormatRGBA8Unorm,
		Usage:         gpu.TextureUsageCopyDst | gpu.TextureUsageCopySrc,
		MipLevelCount: 3,
	})
	if err != nil {
		t.Fatalf("CreateTexture: %v", err)
	}
	level1 := []byte{
		21, 22, 23, 24, 25, 26, 27, 28,
		29, 30, 31, 32, 33, 34, 35, 36,
	}
	d.Queue().WriteTextureLevel(tex, 1, level1, 8, 2, 2)
	if got := tex.(*Texture).lastWriteMipLevel; got != 1 {
		t.Fatalf("lastWriteMipLevel = %d, want 1", got)
	}

	buf, err := d.CreateBuffer(gpu.BufferDesc{
		Size:  256,
		Usage: gpu.BufferUsageMapRead | gpu.BufferUsageCopyDst,
	})
	if err != nil {
		t.Fatalf("CreateBuffer: %v", err)
	}
	enc := d.CreateCommandEncoder()
	enc.CopyTextureToBuffer(
		gpu.TextureCopyInfo{Texture: tex, Origin: [3]int{1, 1, 0}, MipLevel: 1},
		gpu.BufferCopyInfo{Buffer: buf, BytesPerRow: 256, RowsPerImage: 1},
		1, 1, 1,
	)
	d.Queue().Submit(enc.Finish())

	got, err := buf.ReadAsync(4)
	if err != nil {
		t.Fatalf("ReadAsync: %v", err)
	}
	want := []byte{33, 34, 35, 36}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("byte %d: want %d, got %d", i, want[i], got[i])
		}
	}
}

func TestTextureWriteLayerRoundTrip(t *testing.T) {
	d, _ := New(1, 1)
	tex, err := d.CreateTexture(gpu.TextureDesc{
		Width:              2,
		Height:             2,
		DepthOrArrayLayers: 3,
		Format:             gpu.FormatRGBA8Unorm,
		Usage:              gpu.TextureUsageCopyDst | gpu.TextureUsageCopySrc,
	})
	if err != nil {
		t.Fatalf("CreateTexture: %v", err)
	}
	layer2 := []byte{
		41, 42, 43, 44, 45, 46, 47, 48,
		49, 50, 51, 52, 53, 54, 55, 56,
	}
	d.Queue().WriteTextureLevelLayer(tex, 0, 2, layer2, 8, 2, 2, 2)

	buf, err := d.CreateBuffer(gpu.BufferDesc{
		Size:  256,
		Usage: gpu.BufferUsageMapRead | gpu.BufferUsageCopyDst,
	})
	if err != nil {
		t.Fatalf("CreateBuffer: %v", err)
	}
	enc := d.CreateCommandEncoder()
	enc.CopyTextureToBuffer(
		gpu.TextureCopyInfo{Texture: tex, Origin: [3]int{0, 1, 2}},
		gpu.BufferCopyInfo{Buffer: buf, BytesPerRow: 256, RowsPerImage: 1},
		1, 1, 1,
	)
	d.Queue().Submit(enc.Finish())

	got, err := buf.ReadAsync(4)
	if err != nil {
		t.Fatalf("ReadAsync: %v", err)
	}
	want := []byte{49, 50, 51, 52}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("byte %d: want %d, got %d", i, want[i], got[i])
		}
	}
}

func TestTextureViewDescriptors(t *testing.T) {
	d, _ := New(1, 1)
	tex, err := d.CreateTexture(gpu.TextureDesc{
		Width:              8,
		Height:             8,
		DepthOrArrayLayers: 6,
		Format:             gpu.FormatRGBA8Unorm,
		Usage:              gpu.TextureUsageTextureBinding,
	})
	if err != nil {
		t.Fatalf("CreateTexture: %v", err)
	}

	cube := tex.CreateViewDesc(gpu.TextureViewDesc{
		Dimension:       gpu.TextureViewDimensionCube,
		BaseArrayLayer:  0,
		ArrayLayerCount: 6,
		MipLevelCount:   1,
	}).(*TextureView)
	if cube.desc.Dimension != gpu.TextureViewDimensionCube {
		t.Fatalf("cube view dimension = %v, want %v", cube.desc.Dimension, gpu.TextureViewDimensionCube)
	}
	if cube.layer != -1 {
		t.Fatalf("cube view layer = %d, want full texture", cube.layer)
	}

	layer := tex.CreateLayerView(3).(*TextureView)
	if layer.layer != 3 {
		t.Fatalf("layer view layer = %d, want 3", layer.layer)
	}
	if layer.desc.Dimension != gpu.TextureViewDimension2D || layer.desc.ArrayLayerCount != 1 {
		t.Fatalf("layer view desc = %+v, want 2D single-layer view", layer.desc)
	}
}

// TestOffscreenIntegerClearReadback verifies color clears on offscreen
// R32Uint attachments are observable through texture readback. This is the
// same format the bundle renderer uses for GPU picking IDs.
func TestOffscreenIntegerClearReadback(t *testing.T) {
	d, _ := New(1, 1)
	tex, err := d.CreateTexture(gpu.TextureDesc{
		Width:  2,
		Height: 2,
		Format: gpu.FormatR32Uint,
		Usage:  gpu.TextureUsageRenderAttachment | gpu.TextureUsageCopySrc,
	})
	if err != nil {
		t.Fatalf("CreateTexture: %v", err)
	}
	buf, err := d.CreateBuffer(gpu.BufferDesc{
		Size:  256,
		Usage: gpu.BufferUsageMapRead | gpu.BufferUsageCopyDst,
	})
	if err != nil {
		t.Fatalf("CreateBuffer: %v", err)
	}
	enc := d.CreateCommandEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.RenderPassColorAttachment{{
			View:       tex.CreateView(),
			LoadOp:     gpu.LoadOpClear,
			StoreOp:    gpu.StoreOpStore,
			ClearValue: gpu.Color{R: 42},
		}},
	})
	pass.End()
	enc.CopyTextureToBuffer(
		gpu.TextureCopyInfo{Texture: tex, Origin: [3]int{0, 0, 0}},
		gpu.BufferCopyInfo{Buffer: buf, BytesPerRow: 256, RowsPerImage: 1},
		1, 1, 1,
	)
	d.Queue().Submit(enc.Finish())

	got, err := buf.ReadAsync(4)
	if err != nil {
		t.Fatalf("ReadAsync: %v", err)
	}
	if id := binary.LittleEndian.Uint32(got); id != 42 {
		t.Fatalf("readback ID: want 42, got %d", id)
	}
}

// TestBundleFramePresentsBackground verifies the headless backend follows the
// bundle renderer's HDR -> present path instead of leaving the CPU framebuffer
// at the present pass clear color.
func TestBundleFramePresentsBackground(t *testing.T) {
	d, surface := New(4, 4)
	r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer r.Destroy()

	err = r.Frame(engine.RenderBundle{
		Background: "#336699",
		Camera:     engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
	}, 4, 4, 0)
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}

	got := d.Framebuffer().RGBAAt(2, 2)
	want := color.RGBA{R: 0x33, G: 0x66, B: 0x99, A: 0xff}
	if got != want {
		t.Fatalf("framebuffer pixel: want %+v, got %+v", want, got)
	}
}

// TestBundleFrameRasterizesUnlitTriangle verifies the R1 legacy pass path
// produces CPU pixels in the headless backend, not only the background clear.
func TestBundleFrameRasterizesUnlitTriangle(t *testing.T) {
	d, surface := New(32, 32)
	r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer r.Destroy()

	err = r.Frame(engine.RenderBundle{
		Background: "#000000",
		Camera:     engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		Passes: []engine.RenderPassBundle{{
			Positions: []float64{
				-2, -2, 0,
				2, -2, 0,
				0, 2, 0,
			},
			Colors: []float64{
				1, 0, 0,
				0, 1, 0,
				0, 0, 1,
			},
			VertexCount: 3,
		}},
	}, 32, 32, 0)
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}

	center := d.Framebuffer().RGBAAt(16, 16)
	if center.A != 255 || int(center.R)+int(center.G)+int(center.B) == 0 {
		t.Fatalf("center pixel should be covered by unlit triangle, got %+v", center)
	}
}

// TestBundleFrameDepthTestsUnlitPasses verifies later geometry does not
// overwrite nearer pixels when the R1 unlit pipeline has depth enabled.
func TestBundleFrameDepthTestsUnlitPasses(t *testing.T) {
	d, surface := New(32, 32)
	r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer r.Destroy()

	triangle := func(z float64, color []float64) engine.RenderPassBundle {
		return engine.RenderPassBundle{
			Positions: []float64{
				-2, -2, z,
				2, -2, z,
				0, 2, z,
			},
			Colors: []float64{
				color[0], color[1], color[2],
				color[0], color[1], color[2],
				color[0], color[1], color[2],
			},
			VertexCount: 3,
		}
	}
	err = r.Frame(engine.RenderBundle{
		Background: "#000000",
		Camera:     engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		Passes: []engine.RenderPassBundle{
			triangle(1, []float64{1, 0, 0}), // nearer, drawn first
			triangle(0, []float64{0, 1, 0}), // farther, drawn second
		},
	}, 32, 32, 0)
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}

	center := d.Framebuffer().RGBAAt(16, 16)
	if center.R <= center.G {
		t.Fatalf("near red triangle should win depth test over far green, got %+v", center)
	}
}

// TestBundleFrameClipsNearPlaneTriangle verifies geometry fully outside the
// clip depth range does not leak into the software raster target.
func TestBundleFrameClipsNearPlaneTriangle(t *testing.T) {
	d, surface := New(32, 32)
	r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer r.Destroy()

	err = r.Frame(engine.RenderBundle{
		Background: "#000000",
		Camera:     engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		Passes: []engine.RenderPassBundle{{
			Positions: []float64{
				-2, -2, 4.95,
				2, -2, 4.95,
				0, 2, 4.95,
			},
			Colors: []float64{
				1, 0, 0,
				1, 0, 0,
				1, 0, 0,
			},
			VertexCount: 3,
		}},
	}, 32, 32, 0)
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}

	if center := d.Framebuffer().RGBAAt(16, 16); center != (color.RGBA{A: 255}) {
		t.Fatalf("near-clipped triangle should leave background, got %+v", center)
	}
}

// TestBundleFrameRasterizesInstancedMesh verifies the headless backend can
// follow the R1/R2 instanced path through cull compute + DrawIndirect and
// still produce deterministic CPU pixels.
func TestBundleFrameRasterizesInstancedMesh(t *testing.T) {
	d, surface := New(48, 48)
	r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer r.Destroy()

	err = r.Frame(engine.RenderBundle{
		Background: "#000000",
		Camera:     engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			Kind:          "cube",
			VertexCount:   36,
			InstanceCount: 1,
			Transforms: []float64{
				1, 0, 0, 0,
				0, 1, 0, 0,
				0, 0, 1, 0,
				0, 0, 0, 1,
			},
		}},
	}, 48, 48, 0)
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}

	center := d.Framebuffer().RGBAAt(24, 24)
	if center.A != 255 || int(center.R)+int(center.G)+int(center.B) == 0 {
		t.Fatalf("center pixel should be covered by instanced cube, got %+v", center)
	}
}

// TestBundleFrameAppliesLitMaterialColor tightens the R2 validation path:
// explicit RenderMaterial colors should flow through the lit bind group even
// though headless uses a deterministic shading approximation.
func TestBundleFrameAppliesLitMaterialColor(t *testing.T) {
	d, surface := New(48, 48)
	r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer r.Destroy()

	err = r.Frame(engine.RenderBundle{
		Background: "#000000",
		Camera:     engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		Materials: []engine.RenderMaterial{{
			Color: "#2244ff",
		}},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			Kind:          "cube",
			MaterialIndex: 0,
			VertexCount:   36,
			InstanceCount: 1,
			Transforms: []float64{
				1, 0, 0, 0,
				0, 1, 0, 0,
				0, 0, 1, 0,
				0, 0, 0, 1,
			},
		}},
	}, 48, 48, 0)
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}

	center := d.Framebuffer().RGBAAt(24, 24)
	if center.B <= center.R || center.B <= center.G {
		t.Fatalf("explicit blue material should dominate center pixel, got %+v", center)
	}
}

// TestActiveMaterialSamplesBaseColorTexture verifies the D-series material
// approximation follows the lit shader's baseColor texture binding and UV
// repeat path instead of using only the material uniform color.
func TestActiveMaterialSamplesBaseColorTexture(t *testing.T) {
	d, _ := New(1, 1)
	tex, err := d.CreateTexture(gpu.TextureDesc{
		Width:  2,
		Height: 1,
		Format: gpu.FormatRGBA8UnormSRGB,
		Usage:  gpu.TextureUsageTextureBinding | gpu.TextureUsageCopyDst,
	})
	if err != nil {
		t.Fatalf("CreateTexture: %v", err)
	}
	d.Queue().WriteTexture(tex, []byte{
		255, 0, 0, 255,
		0, 128, 255, 255,
	}, 8, 2, 1)

	uniforms := &Buffer{data: make([]byte, 64)}
	writeFloat32(uniforms.data, 0, 1)
	writeFloat32(uniforms.data, 4, 1)
	writeFloat32(uniforms.data, 8, 1)

	pass := &RenderPassEncoder{bindGroups: map[int]*BindGroup{1: &BindGroup{
		desc: gpu.BindGroupDesc{Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: uniforms},
			{Binding: 1, TextureView: tex.CreateView()},
		}},
	}}}
	material := pass.activeMaterial()
	got := material.resolve([3]float32{1, 1, 1}, [2]float32{1.75, 0})
	if got[1] < 0.49 || got[2] < 0.99 || got[0] != 0 {
		t.Fatalf("material texture sample should repeat to blue-green texel, got %v", got)
	}
}

// TestBundleFrameLitRespondsToDirectionalLight verifies the D-series
// approximation still follows scene lighting uniforms instead of rendering
// all lit geometry at material color.
func TestBundleFrameLitRespondsToDirectionalLight(t *testing.T) {
	renderCenter := func(dirZ float64) color.RGBA {
		d, surface := New(48, 48)
		r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
		if err != nil {
			t.Fatalf("bundle.New: %v", err)
		}
		defer r.Destroy()
		err = r.Frame(engine.RenderBundle{
			Background: "#000000",
			Camera:     engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
			Lights: []engine.RenderLight{{
				Kind:       "directional",
				Color:      "#ffffff",
				Intensity:  1,
				DirectionZ: dirZ,
			}},
			Environment: engine.RenderEnvironment{
				AmbientColor:     "#ffffff",
				AmbientIntensity: 0.02,
				SkyColor:         "#ffffff",
				SkyIntensity:     1,
				GroundColor:      "#ffffff",
				GroundIntensity:  1,
			},
			Materials: []engine.RenderMaterial{{
				Color: "#ffffff",
			}},
			InstancedMeshes: []engine.RenderInstancedMesh{{
				Kind:          "cube",
				MaterialIndex: 0,
				VertexCount:   36,
				InstanceCount: 1,
				Transforms: []float64{
					1, 0, 0, 0,
					0, 1, 0, 0,
					0, 0, 1, 0,
					0, 0, 0, 1,
				},
			}},
		}, 48, 48, 0)
		if err != nil {
			t.Fatalf("Frame: %v", err)
		}
		return d.Framebuffer().RGBAAt(24, 24)
	}

	frontLit := renderCenter(-1)
	backLit := renderCenter(1)
	frontSum := int(frontLit.R) + int(frontLit.G) + int(frontLit.B)
	backSum := int(backLit.R) + int(backLit.G) + int(backLit.B)
	if frontSum <= backSum {
		t.Fatalf("front-facing light should be brighter than back-facing light, front=%+v back=%+v", frontLit, backLit)
	}
}

// TestActiveLightingSamplesShadowMap verifies the D-series lit approximation
// consumes the same scene cascade matrices and shadow texture binding as the
// WebGPU shader, reducing direct light when the sampled depth is occluded.
func TestActiveLightingSamplesShadowMap(t *testing.T) {
	scene := &Buffer{data: make([]byte, 368)}
	identity := identityMat4()
	for i := 0; i < 3; i++ {
		copy(scene.data[64+i*64:64+(i+1)*64], float32Bytes(identity[:]))
	}
	writeFloat32(scene.data, 272+8, -1) // lightDir.z, so L points toward +Z.
	writeFloat32(scene.data, 288, 1)
	writeFloat32(scene.data, 292, 1)
	writeFloat32(scene.data, 296, 1)
	writeFloat32(scene.data, 300, 1)
	writeFloat32(scene.data, 352, 10)
	writeFloat32(scene.data, 356, 20)
	writeFloat32(scene.data, 360, 30)

	shadow := &Texture{
		width:  4,
		height: 4,
		layers: 3,
		format: gpu.FormatDepth32Float,
		depth:  make([]float32, 4*4*3),
	}
	for i := range shadow.depth {
		shadow.depth[i] = 1
	}
	writeDepth(shadow, 0, 2, 2, 0.2)

	pass := &RenderPassEncoder{bindGroups: map[int]*BindGroup{0: &BindGroup{
		desc: gpu.BindGroupDesc{Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: scene},
			{Binding: 1, TextureView: shadow.CreateView()},
		}},
	}}}
	lighting := pass.activeLighting()
	base := [3]float32{1, 1, 1}
	normal := [3]float32{0, 0, 1}
	lit := lighting.shade(base, normal, [3]float32{0, 0, 0.1})
	occluded := lighting.shade(base, normal, [3]float32{0, 0, 0.8})
	shadowFactor := lighting.sampleShadow([3]float32{0, 0, 0.8})
	if shadowFactor <= 0 || shadowFactor >= 1 {
		t.Fatalf("linear comparison shadow sample should blend neighboring depths, got %f", shadowFactor)
	}
	sum := func(c [3]float32) float32 { return c[0] + c[1] + c[2] }
	if sum(occluded) >= sum(lit) {
		t.Fatalf("shadowed sample should reduce direct light, lit=%v occluded=%v", lit, occluded)
	}
}

// TestShadowPassWritesDepthOnlyAttachment verifies the R3 shadow pipeline is
// no longer a no-op in headless: a depth-only draw updates the depth target
// without requiring a color attachment.
func TestShadowPassWritesDepthOnlyAttachment(t *testing.T) {
	d, _ := New(16, 16)
	depth, err := d.CreateTexture(gpu.TextureDesc{
		Width:  16,
		Height: 16,
		Format: gpu.FormatDepth32Float,
		Usage:  gpu.TextureUsageRenderAttachment | gpu.TextureUsageCopySrc,
	})
	if err != nil {
		t.Fatalf("CreateTexture: %v", err)
	}
	depthTex := depth.(*Texture)
	pipeline, err := d.CreateRenderPipeline(gpu.RenderPipelineDesc{
		DepthStencil: &gpu.DepthStencilState{
			Format:            gpu.FormatDepth32Float,
			DepthWriteEnabled: true,
			DepthCompare:      gpu.CompareLess,
		},
		Label: "bundle.shadow",
	})
	if err != nil {
		t.Fatalf("CreateRenderPipeline: %v", err)
	}
	uniforms, err := d.CreateBuffer(gpu.BufferDesc{
		Size:  64,
		Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
	})
	if err != nil {
		t.Fatalf("CreateBuffer(uniforms): %v", err)
	}
	d.Queue().WriteBuffer(uniforms, 0, float32Bytes([]float32{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}))
	bindGroup, err := d.CreateBindGroup(gpu.BindGroupDesc{
		Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: uniforms, Size: 64}},
	})
	if err != nil {
		t.Fatalf("CreateBindGroup: %v", err)
	}
	positions, err := d.CreateBuffer(gpu.BufferDesc{
		Size:  36,
		Usage: gpu.BufferUsageVertex | gpu.BufferUsageCopyDst,
	})
	if err != nil {
		t.Fatalf("CreateBuffer(positions): %v", err)
	}
	d.Queue().WriteBuffer(positions, 0, float32Bytes([]float32{
		-0.9, -0.9, 0.25,
		0.9, -0.9, 0.25,
		0, 0.9, 0.25,
	}))

	enc := d.CreateCommandEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		DepthStencilAttachment: &gpu.RenderPassDepthStencilAttachment{
			View:            depth.CreateView(),
			DepthLoadOp:     gpu.LoadOpClear,
			DepthStoreOp:    gpu.StoreOpStore,
			DepthClearValue: 1,
		},
	})
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, bindGroup)
	pass.SetVertexBuffer(0, positions)
	pass.Draw(3, 1, 0, 0)
	pass.End()
	d.Queue().Submit(enc.Finish())

	if got := readDepth(depthTex, -1, 8, 8); got >= 1 {
		t.Fatalf("shadow depth should be written below clear depth, got %f", got)
	}
}

// TestBundleFrameRendersComputeParticles verifies the D-series backend now
// follows the compute-particle update and render path far enough for golden
// validation to see particle pixels.
func TestBundleFrameRendersComputeParticles(t *testing.T) {
	d, surface := New(32, 32)
	r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer r.Destroy()

	b := engine.RenderBundle{
		Background: "#000020",
		Camera:     engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		ComputeParticles: []engine.RenderComputeParticles{{
			ID:    "spark",
			Count: 1,
			Emitter: engine.RenderParticleEmitter{
				Kind:     "point",
				Radius:   0.001,
				Lifetime: 0.1,
				Scatter:  0.01,
			},
			Forces: []engine.RenderParticleForce{{
				Kind:     "gravity",
				Strength: 0,
			}},
			Material: engine.RenderParticleMaterial{
				Color:      "#00ff88",
				ColorEnd:   "#00ff88",
				Size:       3,
				SizeEnd:    3,
				Opacity:    1,
				OpacityEnd: 1,
			},
		}},
	}
	if err := r.Frame(b, 32, 32, 0); err != nil {
		t.Fatalf("Frame first: %v", err)
	}
	if err := r.Frame(b, 32, 32, 1.0/60.0); err != nil {
		t.Fatalf("Frame second: %v", err)
	}

	center := d.Framebuffer().RGBAAt(16, 16)
	if center.G == 0 || center.B <= 0x20 {
		t.Fatalf("center pixel should include additive teal particle energy over background, got %+v", center)
	}
}

func TestParticleForceGraphWindMovesCPUState(t *testing.T) {
	uniforms := &Buffer{data: make([]byte, headlessParticleUniformSize)}
	particles := &Buffer{data: make([]byte, 32)}

	writeFloat32(uniforms.data, 0, 1)  // dt
	writeFloat32(uniforms.data, 4, 0)  // time
	writeFloat32(uniforms.data, 8, 10) // lifetime
	writeFloat32(uniforms.data, 12, 3) // gravity + drag + wind
	writeFloat32(uniforms.data, 32, 0) // initial speed, unused for alive particle

	forceOff := headlessParticleUniformForceOffset
	writeFloat32(uniforms.data, forceOff+0, headlessParticleForceGravity)
	writeFloat32(uniforms.data, forceOff+4, 0)
	forceOff += headlessParticleForceStride
	writeFloat32(uniforms.data, forceOff+0, headlessParticleForceDrag)
	writeFloat32(uniforms.data, forceOff+4, 0)
	forceOff += headlessParticleForceStride
	writeFloat32(uniforms.data, forceOff+0, headlessParticleForceWind)
	writeFloat32(uniforms.data, forceOff+4, 2)
	writeFloat32(uniforms.data, forceOff+16, 1)

	writeFloat32(particles.data, 12, 1)  // current age: alive, not respawned
	writeFloat32(particles.data, 28, 10) // lifetime

	bg := &BindGroup{desc: gpu.BindGroupDesc{Entries: []gpu.BindGroupEntry{
		{Binding: 0, Buffer: uniforms},
		{Binding: 1, Buffer: particles},
	}}}
	runParticleUpdate(bg, 1)

	if got := readFloat32(particles.data, 16); got != 2 {
		t.Fatalf("velocity x = %v, want 2", got)
	}
	if got := readFloat32(particles.data, 0); got != 2 {
		t.Fatalf("position x = %v, want 2", got)
	}
}

// TestDeviceLostOnHeadlessIsNoOp confirms the device-lost path is a
// no-op on headless — the backend has nothing to lose.
func TestDeviceLostOnHeadlessIsNoOp(t *testing.T) {
	d, _ := New(1, 1)
	called := false
	d.OnLost(func(reason, message string) {
		called = true
	})
	if called {
		t.Error("OnLost callback fired unexpectedly on headless")
	}
}

func float32Bytes(values []float32) []byte {
	out := make([]byte, len(values)*4)
	for i, v := range values {
		binary.LittleEndian.PutUint32(out[i*4:i*4+4], math.Float32bits(v))
	}
	return out
}
