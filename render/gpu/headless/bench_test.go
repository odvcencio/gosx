package headless

import (
	"encoding/binary"
	"fmt"
	"image/color"
	"math"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/bundle"
	"m31labs.dev/gosx/render/gpu"
)

// This file measures the CPU rasterizer. It had no benchmark at all, and it sits
// on the path of every headless test and the whole authoring preview loop.
//
// Read the numbers as a profile of one machine, not as a property of the code.
// What travels between machines is the shape: which axis dominates, and how the
// cost grows along it.
//
// Every benchmark reports bytes and allocations too. A per-frame allocation that
// grows with the pixel count is a separate problem from raw arithmetic, and one
// number cannot show both.

// benchScene builds a lit scene with a controlled triangle count, light count and
// shadow setting.
//
// meshes is how many spheres to draw. segments sets the tessellation of each, so
// the triangle count is meshes * segments * (segments/2) * 2. lights is how many
// directional lights the bundle carries; the rasterizer reads the first and the
// rest measure the cost of carrying them. shadows turns the shadow-map pass on.
func benchScene(meshes, segments, lights int, shadows bool) engine.RenderBundle {
	frame := engine.RenderBundle{
		Background: "#101014",
		Camera:     engine.RenderCamera{Y: 2, Z: 9, FOV: 1, Near: 0.1, Far: 100},
		Materials:  []engine.RenderMaterial{{Kind: "standard", Color: "#8a8a8a", Roughness: 0.6}},
		// Every environment term is authored, including the two hemisphere
		// intensities. resolveHemisphereAmbient substitutes an intensity of one
		// when either is unset, and that substitution is under dispute, so a
		// fixture must not depend on it.
		Environment: engine.RenderEnvironment{
			AmbientColor: "#404858", AmbientIntensity: 0.35,
			SkyColor: "#ccddff", SkyIntensity: 0.6,
			GroundColor: "#483c38", GroundIntensity: 0.4,
		},
	}
	for index := 0; index < lights; index++ {
		frame.Lights = append(frame.Lights, engine.RenderLight{
			Kind: "directional", Color: "#ffffff", Intensity: 1,
			DirectionX: -0.4, DirectionY: -1, DirectionZ: -0.3,
			CastShadow: shadows,
		})
	}
	// A ground plane so a shadow has somewhere to land. Without it the shadow pass
	// rasterizes a depth map that nothing ever samples.
	frame.InstancedMeshes = append(frame.InstancedMeshes, engine.RenderInstancedMesh{
		ID: "ground", Kind: "plane", Width: 40, Height: 40,
		MaterialIndex: 0, InstanceCount: 1, ReceiveShadow: shadows,
		Transforms: []float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, -1.5, -4, 1},
	})
	// One instanced batch holding every sphere. Instancing is the path the
	// renderer really uses, and a separate batch per mesh would measure bind-group
	// churn instead of rasterization.
	transforms := make([]float64, 0, meshes*16)
	columns := 1
	for columns*columns < meshes {
		columns++
	}
	for index := 0; index < meshes; index++ {
		x := float64(index%columns) - float64(columns-1)*0.5
		z := float64(index/columns) - float64(columns-1)*0.5
		transforms = append(transforms,
			1, 0, 0, 0,
			0, 1, 0, 0,
			0, 0, 1, 0,
			x*1.6, 0, z*1.6, 1)
	}
	frame.InstancedMeshes = append(frame.InstancedMeshes, engine.RenderInstancedMesh{
		ID: "spheres", Kind: "sphere", Radius: 0.6, Segments: segments,
		MaterialIndex: 0, InstanceCount: meshes, Transforms: transforms,
		CastShadow: shadows,
	})
	return frame
}

// benchTriangleCount reports how many triangles one benchScene draws, so a
// benchmark can print cost per triangle instead of cost per arbitrary label.
func benchTriangleCount(meshes, segments int) int {
	rings := segments / 2
	if rings < 2 {
		rings = 2
	}
	// The plane adds two triangles. Each sphere is segments * rings quads.
	return 2 + meshes*segments*rings*2
}

// runFrames drives one renderer for b.N frames. The renderer is built once,
// outside the timed region, because building it compiles no shader on this
// backend and would otherwise hide the raster cost behind setup.
func runFrames(b *testing.B, frame engine.RenderBundle, width, height int) {
	b.Helper()
	device, surface := New(width, height)
	renderer, err := bundle.New(bundle.Config{Device: device, Surface: surface})
	if err != nil {
		b.Fatalf("bundle.New: %v", err)
	}
	defer renderer.Destroy()
	// One warm frame fills every cache the renderer keeps: the primitive
	// tessellation, the material bind group, the depth and colour targets.
	if err := renderer.Frame(frame, width, height, 0); err != nil {
		b.Fatalf("warm frame: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if err := renderer.Frame(frame, width, height, float64(index)/60.0); err != nil {
			b.Fatalf("frame %d: %v", index, err)
		}
	}
	b.StopTimer()
	// Guard the measurement. A renderer that stopped drawing would look fast.
	colors, variance := frameEvidence(device.Framebuffer())
	if colors < 4 || variance < 0.001 {
		b.Fatalf("the benchmark scene drew nothing: %d colours, variance %.6f", colors, variance)
	}
}

// BenchmarkFrameResolution measures cost against pixel count at a fixed scene.
// The rasterizer fills per pixel, so this axis should grow close to linearly in
// the pixel count once the per-frame setup is amortized.
func BenchmarkFrameResolution(b *testing.B) {
	frame := benchScene(9, 16, 1, false)
	for _, size := range []struct {
		name          string
		width, height int
	}{
		{"64x48", 64, 48},
		{"128x96", 128, 96},
		{"256x192", 256, 192},
		{"512x384", 512, 384},
		{"1280x720", 1280, 720},
	} {
		b.Run(size.name, func(b *testing.B) {
			pixels := size.width * size.height
			b.ReportMetric(float64(pixels), "pixels")
			runFrames(b, frame, size.width, size.height)
		})
	}
}

// BenchmarkFrameTriangles measures cost against triangle count at a fixed
// resolution. Two axes drive it: vertex transform per triangle, and fill per
// covered pixel. The scene keeps its screen area roughly constant, so the growth
// here is mostly transform and setup, not fill.
func BenchmarkFrameTriangles(b *testing.B) {
	for _, tc := range []struct {
		name             string
		meshes, segments int
	}{
		{"1-sphere-8seg", 1, 8},
		{"1-sphere-32seg", 1, 32},
		{"9-spheres-16seg", 9, 16},
		{"36-spheres-16seg", 36, 16},
		{"36-spheres-32seg", 36, 32},
	} {
		b.Run(tc.name, func(b *testing.B) {
			triangles := benchTriangleCount(tc.meshes, tc.segments)
			b.ReportMetric(float64(triangles), "triangles")
			runFrames(b, benchScene(tc.meshes, tc.segments, 1, false), 256, 192)
		})
	}
}

// BenchmarkFrameLights measures the cost of carrying more lights. The rasterizer
// reads the first directional light and ignores the rest, so a flat line here is
// the expected result and is itself the finding: adding lights buys nothing and
// costs almost nothing.
func BenchmarkFrameLights(b *testing.B) {
	for _, lights := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("%d-lights", lights), func(b *testing.B) {
			b.ReportMetric(float64(lights), "lights")
			runFrames(b, benchScene(9, 16, lights, false), 256, 192)
		})
	}
}

// BenchmarkFrameShadows measures the shadow pass. Shadows on rasterizes the
// casters again into a depth map per cascade, and every lit pixel then runs a
// cascade selection and a depth comparison.
func BenchmarkFrameShadows(b *testing.B) {
	for _, tc := range []struct {
		name    string
		shadows bool
	}{{"off", false}, {"on", true}} {
		b.Run(tc.name, func(b *testing.B) {
			runFrames(b, benchScene(9, 16, 1, tc.shadows), 256, 192)
		})
	}
}

// BenchmarkFramePostFX measures the post-effect chain on the CPU path. Each pass
// is a texture copy here, so the cost is one full-frame copy per effect and
// nothing else. It bounds what the chain can cost a preview today.
func BenchmarkFramePostFX(b *testing.B) {
	base := benchScene(9, 16, 1, false)
	chains := []struct {
		name    string
		effects []engine.RenderPostEffect
	}{
		{"none", nil},
		{"vignette", []engine.RenderPostEffect{{Kind: "vignette", Intensity: 1}}},
		{"full-chain", []engine.RenderPostEffect{
			{Kind: "bloom", Intensity: 1}, {Kind: "ssao"}, {Kind: "dof"},
			{Kind: "vignette", Intensity: 1}, {Kind: "colorgrade"}, {Kind: "tonemap"},
		}},
	}
	for _, chain := range chains {
		b.Run(chain.name, func(b *testing.B) {
			frame := base
			frame.PostEffects = chain.effects
			runFrames(b, frame, 256, 192)
		})
	}
}

// BenchmarkRasterPhases splits the frame into the phases a caller can turn off,
// so a reader can see where the time goes without a profiler.
//
//   - background is a clear and a present copy with no geometry at all.
//   - geometry adds the lit raster of the scene.
//   - shadows adds the shadow-map pass and the per-pixel comparison.
//
// Subtract one from the next to attribute the cost.
func BenchmarkRasterPhases(b *testing.B) {
	const width, height = 256, 192
	empty := benchScene(0, 16, 1, false)
	empty.InstancedMeshes = nil
	b.Run("background-only", func(b *testing.B) {
		device, surface := New(width, height)
		renderer, err := bundle.New(bundle.Config{Device: device, Surface: surface})
		if err != nil {
			b.Fatal(err)
		}
		defer renderer.Destroy()
		if err := renderer.Frame(empty, width, height, 0); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			if err := renderer.Frame(empty, width, height, float64(index)/60.0); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("geometry", func(b *testing.B) {
		runFrames(b, benchScene(9, 16, 1, false), width, height)
	})
	b.Run("geometry-and-shadows", func(b *testing.B) {
		runFrames(b, benchScene(9, 16, 1, true), width, height)
	})
}

// BenchmarkRasterizeTriangleFill isolates the inner fill loop from every layer
// above it. It builds one raster target and one screen-filling triangle, then
// calls rasterizeTriangle directly.
//
// Compare it against BenchmarkFrameResolution to separate the fill cost from the
// renderer's own per-frame work.
func BenchmarkRasterizeTriangleFill(b *testing.B) {
	for _, tc := range []struct {
		name    string
		shading bool
	}{{"unlit", false}, {"lit", true}} {
		b.Run(tc.name, func(b *testing.B) {
			const size = 256
			device, _ := New(size, size)
			target := rasterTarget{img: device.framebuffer, width: size, height: size}
			verts := [3]rasterVertex{
				{x: 0, y: float32(size), depth: 0.5, invW: 1, color: [4]float32{1, 0, 0, 1},
					normal: [3]float32{0, 0, 1}, world: [3]float32{-1, -1, 0}},
				{x: float32(size), y: float32(size), depth: 0.5, invW: 1, color: [4]float32{0, 1, 0, 1},
					normal: [3]float32{0, 0, 1}, world: [3]float32{1, -1, 0}},
				{x: float32(size) / 2, y: 0, depth: 0.5, invW: 1, color: [4]float32{0, 0, 1, 1},
					normal: [3]float32{0, 0, 1}, world: [3]float32{0, 1, 0}},
			}
			var shading *litProgram
			if tc.shading {
				program := newLitProgram(defaultSceneLighting(), defaultMaterialState())
				shading = &program
			}
			// Half of a size-by-size square, so report the covered pixel count.
			b.ReportMetric(float64(size*size)/2, "filled-pixels")
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				rasterizeTriangle(target, verts, shading)
			}
			b.StopTimer()
			if colors, _ := frameEvidence(device.framebuffer); colors < 2 {
				b.Fatalf("the fill drew nothing: %d colours", colors)
			}
		})
	}
}

// BenchmarkPostPass measures one fullscreen fragment stage per output pixel.
//
// Whole-frame timing cannot attribute this cost. A frame at 256x192 spends about
// two milliseconds in the present pass, and the run-to-run spread of the same
// frame on the same machine is larger than that. This benchmark drives one stage
// directly over a fixed target, so the per-pixel number is stable and a reader
// can multiply it by any frame size.
//
// The blur is the expensive one by a wide margin: nine bilinear samples per
// output texel, and a bilinear sample is four texel reads. It only runs when the
// author enables bloom, and it runs at half resolution.
func BenchmarkPostPass(b *testing.B) {
	const (
		width  = 320
		height = 240
	)
	device, _ := New(width, height)
	source := newBenchPostTexture(b, device, width, height)
	half := newBenchPostTexture(b, device, width/2, height/2)
	destination := newBenchPostTexture(b, device, width, height)
	dst := postTarget{tex: destination, w: width, h: height}

	bloomParams := newBenchPostUniform(b, device, 0.4, 1, 0.5, 0)
	presentParams := newBenchPostUniform(b, device, 1, 1.2, 0, 0)
	gradeParams := newBenchPostUniform(b, device, 1.2, 1.3, 0.7, 0)
	blurParams := newBenchPostUniform(b, device, 1.0/float32(width), 0, 0, 0)

	cases := []struct {
		name string
		bind *BindGroup
		make func(*BindGroup, postTarget) postFragment
	}{
		// The bloom-free case is the one every frame pays: one exact fetch of a
		// same-size source plus the curve. The bloom case adds one filtered
		// fetch of the half-resolution glow target, which is four texel reads.
		{"compose-present", benchBindGroup(
			benchTexture(0, source),
			benchBuffer(4, newBenchPostUniform(b, device, 0.4, 0, 0.5, 0)), benchBuffer(5, presentParams),
		), newComposePresentFragment},
		{"compose-present-with-bloom", benchBindGroup(
			benchTexture(0, source), benchTexture(2, half),
			benchBuffer(4, bloomParams), benchBuffer(5, presentParams),
		), newComposePresentFragment},
		{"vignette", benchBindGroup(benchTexture(0, source), benchBuffer(3, gradeParams)), newVignetteFragment},
		{"color-grade", benchBindGroup(benchTexture(0, source), benchBuffer(3, gradeParams)), newColorGradeFragment},
		{"bloom-bright", benchBindGroup(benchTexture(0, source), benchBuffer(2, bloomParams)), newBrightPassFragment},
		{"bloom-blur", benchBindGroup(benchTexture(0, source), benchBuffer(2, blurParams)), newBlurFragment},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			shade := tc.make(tc.bind, dst)
			if shade == nil {
				b.Fatalf("%s built no fragment stage; the bind group is wrong", tc.name)
			}
			b.ReportMetric(float64(width*height), "pixels")
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				runPostPass(dst, shade)
			}
			b.StopTimer()
			if colors, _ := frameEvidence(device.framebuffer); colors < 1 {
				b.Fatal("the pass wrote nothing")
			}
		})
	}
}

// newBenchPostTexture builds one colour target and fills it with a gradient, so
// no pass reads a constant and folds its arithmetic to one branch.
func newBenchPostTexture(b *testing.B, device *Device, width, height int) *Texture {
	b.Helper()
	created, err := device.CreateTexture(gpu.TextureDesc{Width: width, Height: height, Format: gpu.FormatRGBA8Unorm})
	if err != nil {
		b.Fatalf("CreateTexture: %v", err)
	}
	tex, ok := created.(*Texture)
	if !ok {
		b.Fatalf("CreateTexture returned %T, want *Texture", created)
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			writeTextureRGBA(tex, 0, x, y, color.RGBA{
				R: uint8((x * 255) / max(1, width-1)),
				G: uint8((y * 255) / max(1, height-1)),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}
	return tex
}

func newBenchPostUniform(b *testing.B, device *Device, x, y, z, w float32) *Buffer {
	b.Helper()
	created, err := device.CreateBuffer(gpu.BufferDesc{Size: 16, Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst})
	if err != nil {
		b.Fatalf("CreateBuffer: %v", err)
	}
	data := make([]byte, 16)
	for i, v := range [4]float32{x, y, z, w} {
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(v))
	}
	device.Queue().WriteBuffer(created, 0, data)
	buf, ok := created.(*Buffer)
	if !ok {
		b.Fatalf("CreateBuffer returned %T, want *Buffer", created)
	}
	return buf
}

func benchTexture(binding int, tex *Texture) gpu.BindGroupEntry {
	return gpu.BindGroupEntry{Binding: binding, TextureView: tex.CreateView()}
}

func benchBuffer(binding int, buf *Buffer) gpu.BindGroupEntry {
	return gpu.BindGroupEntry{Binding: binding, Buffer: buf, Size: 16}
}

func benchBindGroup(entries ...gpu.BindGroupEntry) *BindGroup {
	return &BindGroup{desc: gpu.BindGroupDesc{Entries: entries}}
}
