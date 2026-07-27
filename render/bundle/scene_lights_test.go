package bundle

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
)

// The scene light array.
//
// litWGSL read one directional light until 2026-07-26. A scene lit by a point
// light, a spot light or a hemisphere light therefore took a canned key light on
// every native surface: the desktop renderer, the server-side-rendering oracle
// and the build-time poster. The same scene took its authored lights in a
// browser. The tests below pin the packing that closed the gap.
//
// lit_drift_test.go pins the shader terms against the browser copy. This file
// pins the bytes that reach the shader.

// lightsBundle builds a one-sphere scene lit by the given lights.
func lightsBundle(lights ...engine.RenderLight) engine.RenderBundle {
	return engine.RenderBundle{
		Camera:    engine.RenderCamera{Z: 6, FOV: 1, Near: 0.1, Far: 100},
		Materials: []engine.RenderMaterial{{Kind: "standard", Color: "#ffffff"}},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID: "probe", Kind: "sphere", MaterialIndex: 0,
			InstanceCount: 1, Transforms: identityTransform(),
		}},
		Lights: lights,
	}
}

// lastWriteToLabel returns the bytes of the last queue write aimed at the buffer
// with the given label. ok is false when no write reached that buffer.
func lastWriteToLabel(d *fakeDevice, label string) (data []byte, ok bool) {
	for i := len(d.queue.writes) - 1; i >= 0; i-- {
		buf, isFake := d.queue.writes[i].buffer.(*fakeBuffer)
		if !isFake || buf.label != label {
			continue
		}
		return d.queue.writes[i].data, true
	}
	return nil, false
}

// readFloat decodes one little-endian float32 from a packed buffer.
func readFloat(data []byte, index int) float32 {
	offset := index * 4
	if offset+4 > len(data) {
		return float32(math.NaN())
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(data[offset : offset+4]))
}

// renderLightFrame renders one frame and returns the light buffer bytes and the
// scene uniform bytes.
func renderLightFrame(t *testing.T, b engine.RenderBundle) (lights, scene []byte) {
	t.Helper()
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()
	if err := r.Frame(b, 64, 64, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	lights, ok := lastWriteToLabel(d, "bundle.scene.lights")
	if !ok {
		t.Fatal("no frame write reached bundle.scene.lights; the light array never leaves the CPU")
	}
	scene, ok = lastWriteToLabel(d, "bundle.scene.uniforms")
	if !ok {
		t.Fatal("no frame write reached bundle.scene.uniforms")
	}
	return lights, scene
}

// lightParamsOffset is the byte offset of the lightParams lane in the Scene
// uniform. It is the last lane, appended after envParams, so every earlier
// offset stayed where it was.
const lightParamsOffset = 384

// TestFrameUploadsEveryAuthoredLight pins the bytes one frame sends for a scene
// that authors five light kinds at once.
//
// A PASS PROVES: resolveSceneLights writes one 80-byte record per authored
// light, in bundle order, with the kind code, position, direction, intensity,
// colour, range, decay, cone angle, ground colour and penumbra each in the lane
// litWGSL reads; and the scene uniform carries the same count.
//
// A PASS DOES NOT PROVE: that the shader shades those lanes correctly. No GPU
// runs in this test. lit_drift_test.go pins the shading terms against the
// browser copy, which does run on a GPU.
func TestFrameUploadsEveryAuthoredLight(t *testing.T) {
	lights, scene := renderLightFrame(t, lightsBundle(
		engine.RenderLight{Kind: "ambient", Color: "#804020", Intensity: 0.5},
		engine.RenderLight{Kind: "directional", Color: "#ffffff", Intensity: 1.2,
			DirectionX: 0, DirectionY: -1, DirectionZ: 0},
		engine.RenderLight{Kind: "point", Color: "#00ff00", Intensity: 2,
			X: 1, Y: 2, Z: 3, Range: 12, Decay: 1.5},
		engine.RenderLight{Kind: "spot", Color: "#0000ff", Intensity: 3,
			X: -1, Y: 5, Z: 0, DirectionX: 0, DirectionY: -1, DirectionZ: 0,
			Angle: 0.5, Penumbra: 0.25, Range: 20},
		engine.RenderLight{Kind: "hemisphere", Color: "#ff0000", GroundColor: "#0000ff", Intensity: 0.8},
	))

	if want := 5 * lightRecordSize; len(lights) != want {
		t.Fatalf("light upload is %d bytes, want %d for five records of %d bytes",
			len(lights), want, lightRecordSize)
	}
	if got := readFloat(scene, lightParamsOffset/4); got != 5 {
		t.Errorf("scene uniform light count = %g, want 5. The shader loops to this number, so a wrong count drops or repeats lights.", got)
	}
	if got := readFloat(scene, lightParamsOffset/4+1); got != 1 {
		t.Errorf("scene uniform shadow light index = %g, want 1, the index of the only directional light.", got)
	}

	// lane names the float index inside one 20-float record.
	for _, row := range []struct {
		name   string
		record int
		lane   int
		want   float32
	}{
		{"ambient kind code", 0, 3, lightKindAmbient},
		{"ambient intensity", 0, 7, 0.5},
		{"directional kind code", 1, 3, lightKindDirectional},
		{"directional direction y", 1, 5, -1},
		{"directional intensity", 1, 7, 1.2},
		{"point kind code", 2, 3, lightKindPoint},
		{"point position x", 2, 0, 1},
		{"point position y", 2, 1, 2},
		{"point position z", 2, 2, 3},
		{"point range", 2, 11, 12},
		{"point decay", 2, 12, 1.5},
		{"spot kind code", 3, 3, lightKindSpot},
		{"spot cone angle", 3, 15, 0.5},
		{"spot penumbra", 3, 19, 0.25},
		{"spot range", 3, 11, 20},
		{"hemisphere kind code", 4, 3, lightKindHemisphere},
		{"hemisphere intensity", 4, 7, 0.8},
	} {
		got := readFloat(lights, row.record*lightFloats+row.lane)
		if got != row.want {
			t.Errorf("%s: record %d lane %d is %g, want %g",
				row.name, row.record, row.lane, got, row.want)
		}
	}

	// The hemisphere sky colour rides in color.rgb and the ground colour in
	// groundPenumbra.rgb. Swapping them lights the top of a sphere with the
	// ground colour, which is the defect the shader rows guard against.
	if red := readFloat(lights, 4*lightFloats+8); red != 1 {
		t.Errorf("hemisphere sky red = %g, want 1 for #ff0000", red)
	}
	if blue := readFloat(lights, 4*lightFloats+18); blue != 1 {
		t.Errorf("hemisphere ground blue = %g, want 1 for #0000ff", blue)
	}
}

// TestMovingAPointLightMovesTheUpload is the perturbation proof for the light
// upload.
//
// A test that only reads a fixed scene proves nothing about whether the data is
// live. This one renders the same scene twice with one number changed and
// requires the bytes to follow.
//
// A PASS PROVES: a point light's world position reaches the GPU, and only the
// three position lanes of that light move when the light moves.
func TestMovingAPointLightMovesTheUpload(t *testing.T) {
	before, _ := renderLightFrame(t, lightsBundle(
		engine.RenderLight{Kind: "point", Color: "#ffffff", Intensity: 1, X: 1, Y: 2, Z: 3},
	))
	after, _ := renderLightFrame(t, lightsBundle(
		engine.RenderLight{Kind: "point", Color: "#ffffff", Intensity: 1, X: 4, Y: 2, Z: 3},
	))
	if len(before) != len(after) {
		t.Fatalf("moving a light changed the upload size from %d to %d bytes", len(before), len(after))
	}
	var moved []int
	for lane := 0; lane < lightFloats; lane++ {
		if readFloat(before, lane) != readFloat(after, lane) {
			moved = append(moved, lane)
		}
	}
	if len(moved) != 1 || moved[0] != 0 {
		t.Fatalf("moving a point light along X changed lanes %v, want lane 0 alone. "+
			"No change at all means the position never reaches the shader.", moved)
	}
	if got := readFloat(after, 0); got != 4 {
		t.Errorf("moved point light X = %g, want 4", got)
	}
}

// TestOneDirectionalLightPacksWhatTheOldSingleLightPathUploaded proves the light
// array changed no existing picture.
//
// The scene uniform still carries scene.lightDir and scene.lightColor, because
// the cascade fit and the image-based terms read them. The light array has to
// agree with them for a scene with one directional light, or every such scene
// would shade twice or shade differently.
//
// A PASS PROVES: for a scene with one directional light, the packed record
// carries the same unit direction, the same colour and the same intensity that
// resolveDirectionalLight puts in the scene uniform, and the shadow index names
// that light.
func TestOneDirectionalLightPacksWhatTheOldSingleLightPathUploaded(t *testing.T) {
	b := lightsBundle(engine.RenderLight{
		Kind: "directional", Color: "#ffeecc", Intensity: 1.2,
		DirectionX: 1, DirectionY: -0.2, DirectionZ: 0.5,
	})
	lights, scene := renderLightFrame(t, b)

	dir, color, _ := resolveDirectionalLight(b)
	packedDir := unitVector([3]float32{readFloat(lights, 4), readFloat(lights, 5), readFloat(lights, 6)})
	for i := range dir {
		if absDiff(packedDir[i], dir[i]) > 1e-6 {
			t.Errorf("packed direction component %d is %g and the scene uniform carries %g. "+
				"The cascades are fitted to the uniform, so a mismatch shadows the wrong light.",
				i, packedDir[i], dir[i])
		}
	}
	for i := 0; i < 3; i++ {
		if got := readFloat(lights, 8+i); got != color[i] {
			t.Errorf("packed colour component %d is %g, want %g", i, got, color[i])
		}
	}
	if got := readFloat(lights, 7); got != color[3] {
		t.Errorf("packed intensity is %g, want %g", got, color[3])
	}
	if got := readFloat(scene, lightParamsOffset/4+1); got != 0 {
		t.Errorf("shadow light index = %g, want 0; the only directional light must read the shadow map", got)
	}
}

// TestSceneUniformKeepsEveryOffsetBeforeLightParams pins the append-only shape of
// the Scene uniform.
//
// render/gpu/headless/device.go decodes this buffer independently, at fixed byte
// offsets. lightParams was appended at the end for that reason: the CPU decode
// needs one added read and no edit to an existing one.
//
// A PASS PROVES: the camera, light direction, light colour, ambient, sky, ground,
// cascade split and environment lanes all still sit where they sat, and
// lightParams sits after all of them.
func TestSceneUniformKeepsEveryOffsetBeforeLightParams(t *testing.T) {
	var r Renderer
	packed := r.sceneUniformBytes(sceneUniformBlock{
		cameraPos:     [4]float32{1, 2, 3, 1},
		lightDir:      [4]float32{4, 5, 6, 0},
		lightColor:    [4]float32{7, 8, 9, 10},
		ambientColor:  [4]float32{11, 12, 13, 14},
		skyColor:      [4]float32{15, 16, 17, 18},
		groundColor:   [4]float32{19, 20, 21, 22},
		cascadeSplits: [4]float32{23, 24, 25, 26},
		envParams:     [4]float32{27, 28, 29, 30},
		lightParams:   [4]float32{31, 32, 0, 0},
	})
	if len(packed) != sceneUniformSize {
		t.Fatalf("scene uniform is %d bytes, want %d", len(packed), sceneUniformSize)
	}
	for _, row := range []struct {
		name   string
		offset int
		want   float32
	}{
		{"cameraPos.x", 256, 1},
		{"lightDir.x", 272, 4},
		{"lightColor.x", 288, 7},
		{"ambientColor.x", 304, 11},
		{"skyColor.x", 320, 15},
		{"groundColor.x", 336, 19},
		{"cascadeSplits.x", 352, 23},
		{"envParams.x", 368, 27},
		{"lightParams.x", lightParamsOffset, 31},
		{"lightParams.y", lightParamsOffset + 4, 32},
	} {
		if got := readFloat(packed, row.offset/4); got != row.want {
			t.Errorf("%s sits at byte %d and reads %g, want %g. "+
				"render/gpu/headless/device.go activeLighting decodes these offsets by hand.",
				row.name, row.offset, got, row.want)
		}
	}
}

// TestLightStorageGrowsPastTheInitialCapacity pins the growth path.
//
// The storage buffer starts at eight records. A scene with more lights has to
// grow it, and growing replaces the buffer that both lit bind groups point at.
//
// A PASS PROVES: a scene with twenty lights uploads twenty records, the buffer
// grew to the next power of two, and the lit bind group points at the new buffer
// rather than the destroyed one.
func TestLightStorageGrowsPastTheInitialCapacity(t *testing.T) {
	many := make([]engine.RenderLight, 20)
	for i := range many {
		many[i] = engine.RenderLight{Kind: "point", Color: "#ffffff", Intensity: 1, X: float64(i)}
	}
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()
	if r.lightStorageCap != lightCapacityMin {
		t.Fatalf("startup light capacity = %d, want %d", r.lightStorageCap, lightCapacityMin)
	}
	if err := r.Frame(lightsBundle(many...), 64, 64, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if r.lightStorageCap != 32 {
		t.Errorf("light capacity after twenty lights = %d, want 32", r.lightStorageCap)
	}
	lights, ok := lastWriteToLabel(d, "bundle.scene.lights")
	if !ok {
		t.Fatal("no write reached bundle.scene.lights")
	}
	if want := 20 * lightRecordSize; len(lights) != want {
		t.Errorf("twenty lights uploaded %d bytes, want %d", len(lights), want)
	}
	if got := readFloat(lights, 19*lightFloats); got != 19 {
		t.Errorf("the twentieth light packed X = %g, want 19; a light past the initial capacity was dropped", got)
	}

	// The bind group must point at the live buffer. A destroyed buffer here
	// would fault a real device on the next draw.
	bg, isFake := r.litBindGrp.(*fakeBindGroup)
	if !isFake {
		t.Fatal("lit bind group is not a fake bind group")
	}
	bound, isBuf := bg.desc.Entries[5].Buffer.(*fakeBuffer)
	if !isBuf {
		t.Fatal("lit bind group entry 5 holds no buffer")
	}
	if bound.destroyed {
		t.Error("the lit bind group still points at the destroyed light buffer; growing must rebuild it")
	}
	if bound != r.lightStorageBuf {
		t.Error("the lit bind group points at a light buffer the renderer no longer writes")
	}
}

// TestLightStorageCapacityMatchesTheBrowserGrowth pins the capacity rule against
// the browser renderer.
//
// A backend that grows differently accepts a scene the other rejects. The
// browser reports the overflow through its issue channel; the native path drops
// the extra records and lowers the count it writes into the scene uniform, so
// the shader never reads past the array.
func TestLightStorageCapacityMatchesTheBrowserGrowth(t *testing.T) {
	for _, row := range []struct {
		count int
		want  int
	}{
		{0, 8}, {1, 8}, {8, 8}, {9, 16}, {17, 32}, {256, 256}, {1000, 256},
	} {
		if got := lightStorageCapacityFor(row.count); got != row.want {
			t.Errorf("lightStorageCapacityFor(%d) = %d, want %d", row.count, got, row.want)
		}
	}
	js := readJSWebGPURenderer(t)
	for _, needle := range []string{
		"var SCENE_WEBGPU_LIGHT_CAPACITY_MIN = 8;",
		"var SCENE_WEBGPU_LIGHT_CAPACITY_MAX = 256;",
	} {
		if !strings.Contains(js, needle) {
			t.Errorf("%s no longer declares:\n  %s\nThe two backends must accept the same scene sizes.",
				jsWebGPURendererFile, needle)
		}
	}
}

// TestLightCountNeverExceedsTheBufferCapacity pins the overflow behaviour.
//
// The shader loops to the count in the scene uniform, bounded by arrayLength.
// A count larger than the buffer would make the loop read past the array on a
// backend that does not bound it, so the count is clamped on this side too.
func TestLightCountNeverExceedsTheBufferCapacity(t *testing.T) {
	many := make([]engine.RenderLight, lightCapacityMax+40)
	for i := range many {
		many[i] = engine.RenderLight{Kind: "point", Color: "#ffffff", Intensity: 1}
	}
	lights, scene := renderLightFrame(t, lightsBundle(many...))
	if want := lightCapacityMax * lightRecordSize; len(lights) != want {
		t.Errorf("an overflowing scene uploaded %d bytes, want the capped %d", len(lights), want)
	}
	if got := readFloat(scene, lightParamsOffset/4); got != float32(lightCapacityMax) {
		t.Errorf("scene uniform light count = %g, want the capped %d. A larger count reads past the array.",
			got, lightCapacityMax)
	}
}

// TestShadowIndexNamesTheLightTheCascadesFollow pins which light reads the one
// shadow map.
//
// litWGSL samples the cascaded shadow map for exactly one light, and the
// cascades are fitted to scene.lightDir. Naming any other light would darken a
// surface with a shadow computed for a different direction.
func TestShadowIndexNamesTheLightTheCascadesFollow(t *testing.T) {
	for _, row := range []struct {
		name   string
		lights []engine.RenderLight
		want   float32
	}{
		{
			name: "the first directional light wins",
			lights: []engine.RenderLight{
				{Kind: "point", Color: "#ffffff", Intensity: 1},
				{Kind: "directional", Color: "#ffffff", Intensity: 1, DirectionY: -1},
				{Kind: "directional", Color: "#ffffff", Intensity: 1, DirectionX: 1},
			},
			want: 1,
		},
		{
			name: "no directional light means no shadowed light",
			lights: []engine.RenderLight{
				{Kind: "point", Color: "#ffffff", Intensity: 1},
				{Kind: "spot", Color: "#ffffff", Intensity: 1, Angle: 0.4},
			},
			want: -1,
		},
		{
			// resolveDirectionalLight stops at the first directional light and
			// keeps the fallback direction when that light names none. No
			// authored light matches the cascade fit, so none reads the map.
			name: "a directional light with no direction disqualifies itself",
			lights: []engine.RenderLight{
				{Kind: "directional", Color: "#ffffff", Intensity: 1},
			},
			want: -1,
		},
		{
			name:   "a scene with no lights shadows the fallback key light",
			lights: nil,
			want:   0,
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			_, scene := renderLightFrame(t, lightsBundle(row.lights...))
			if got := readFloat(scene, lightParamsOffset/4+1); got != row.want {
				t.Errorf("shadow light index = %g, want %g", got, row.want)
			}
		})
	}
}

// benchLightCount measures one full frame against a growing light count.
//
// The shader loop runs per pixel, and no GPU runs in a benchmark, so this
// measures only the CPU half: resolving, packing and uploading the array. Read
// it as the floor on what a light costs, not as the frame cost. The per-pixel
// half scales with the light count on a real device, and the CPU half does not
// move at all past the first few lights.
func benchLightCount(b *testing.B, count int) {
	b.Helper()
	lights := make([]engine.RenderLight, count)
	for i := range lights {
		lights[i] = engine.RenderLight{
			Kind: "point", Color: "#ffffff", Intensity: 1,
			X: float64(i), Y: 2, Z: 3, Range: 10, Decay: 2,
		}
	}
	bundle := lightsBundle(lights...)
	d := newNullDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	defer r.Destroy()
	if err := r.Frame(bundle, 256, 256, 0); err != nil {
		b.Fatalf("Frame: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.Frame(bundle, 256, 256, float64(i)*0.016); err != nil {
			b.Fatalf("Frame: %v", err)
		}
	}
}

// benchLightPack measures only the work the light array added to a frame:
// resolving the bundle lights and packing them into upload bytes.
func benchLightPack(b *testing.B, count int) {
	b.Helper()
	lights := make([]engine.RenderLight, count)
	for i := range lights {
		lights[i] = engine.RenderLight{
			Kind: "spot", Color: "#ffffff", Intensity: 1,
			X: float64(i), DirectionY: -1, Angle: 0.5, Penumbra: 0.2, Range: 10,
		}
	}
	bundle := lightsBundle(lights...)
	var r Renderer
	r.lightStorageCap = lightCapacityMax
	records, _ := resolveSceneLights(bundle, nil)
	r.lightStorageBytes(records)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		records, _ = resolveSceneLights(bundle, records)
		r.lightStorageBytes(records)
	}
}

func BenchmarkLightPack1(b *testing.B)  { benchLightPack(b, 1) }
func BenchmarkLightPack4(b *testing.B)  { benchLightPack(b, 4) }
func BenchmarkLightPack16(b *testing.B) { benchLightPack(b, 16) }
func BenchmarkLightPack64(b *testing.B) { benchLightPack(b, 64) }

func BenchmarkFrameLights1(b *testing.B)  { benchLightCount(b, 1) }
func BenchmarkFrameLights4(b *testing.B)  { benchLightCount(b, 4) }
func BenchmarkFrameLights16(b *testing.B) { benchLightCount(b, 16) }
func BenchmarkFrameLights64(b *testing.B) { benchLightCount(b, 64) }

// TestPackingLightsAllocatesNothingOnASteadyFrame pins the allocation budget.
//
// The renderer documents that a steady-state frame allocates nothing on the Go
// heap. The light array is packed every frame, so it has to reuse its buffers.
func TestPackingLightsAllocatesNothingOnASteadyFrame(t *testing.T) {
	b := lightsBundle(
		engine.RenderLight{Kind: "point", Color: "#ffffff", Intensity: 1, X: 1},
		engine.RenderLight{Kind: "spot", Color: "#ffffff", Intensity: 1, Angle: 0.5},
		engine.RenderLight{Kind: "directional", Color: "#ffffff", Intensity: 1, DirectionY: -1},
	)
	var records []packedLight
	records, _ = resolveSceneLights(b, records)
	var r Renderer
	r.lightStorageCap = lightCapacityMin
	r.lightStorageBytes(records)

	allocs := testing.AllocsPerRun(50, func() {
		records, _ = resolveSceneLights(b, records)
		r.lightStorageBytes(records)
	})
	if allocs != 0 {
		t.Errorf("packing three lights allocates %g times per frame, want 0", allocs)
	}
}
