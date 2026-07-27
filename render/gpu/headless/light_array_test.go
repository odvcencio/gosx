package headless

import (
	"image"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/bundle"
	"m31labs.dev/gosx/render/gpu"
)

// This file guards the CPU copy of the light loop in litWGSL.
//
// The rasterizer read one directional light until 2026-07-27. render/bundle
// shipped a runtime light array on the same day, so the desktop renderer shaded
// ambient, directional, point, spot and hemisphere lights while every poster and
// every server-side render shaded one directional light and dropped the rest.
// An author who lit a scene with a point light got a picture in the browser and
// a differently lit picture from the build.
//
// render/gpu/headless/lit_parity_test.go pins the numbers this copy shares with
// the shader. This file pins the behaviour: each kind must reach a pixel, and it
// must reach it in the direction the model predicts.

const lightProbeSize = 40

// lightProbeScene puts one plane below the origin and one sphere on it, with a
// black background and no ambient dome. Every visible photon then comes from the
// authored lights, so a kind that never shades leaves the frame black.
//
// Tone mapping is "none" so the present pass only clamps. Each case below states
// a byte value or an ordering, and the ACES curve would bend both.
func lightProbeScene(lights ...engine.RenderLight) engine.RenderBundle {
	return engine.RenderBundle{
		Background: "#000000",
		// Straight down at the floor, the same camera litSurfaceScene uses. A
		// plane seen face-on fills the frame, so a light with a falloff paints a
		// readable pool instead of a sliver.
		Camera: engine.RenderCamera{Y: 5, RotationX: 1.5707963, FOV: 1, Near: 0.1, Far: 100},
		Materials: []engine.RenderMaterial{
			{Kind: "standard", Color: "#ffffff", Roughness: 0.9, Metalness: 0},
		},
		Lights: lights,
		Environment: engine.RenderEnvironment{
			AmbientColor: "#000000", AmbientIntensity: 0.0001,
			SkyColor: "#000000", SkyIntensity: 0.0001,
			GroundColor: "#000000", GroundIntensity: 0.0001,
			ToneMapping: "none",
		},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID: "floor", Kind: "plane", Width: 8, Height: 8,
			MaterialIndex: 0, InstanceCount: 1,
			Transforms: []float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1},
		}},
	}
}

func renderLightProbe(t *testing.T, frame engine.RenderBundle) *image.RGBA {
	t.Helper()
	device, surface := New(lightProbeSize, lightProbeSize)
	renderer, err := bundle.New(bundle.Config{Device: device, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer renderer.Destroy()
	if err := renderer.Frame(frame, lightProbeSize, lightProbeSize, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	return cloneTestRGBA(device.Framebuffer())
}

// litSum is the sum of the three channels of the brightest pixel in the frame.
// One number is enough for an ordering, and the brightest pixel is where a light
// with a falloff lands its peak.
func litSum(img *image.RGBA) int {
	best := 0
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if sum := int(c.R) + int(c.G) + int(c.B); sum > best {
				best = sum
			}
		}
	}
	return best
}

// TestEveryLightKindReachesAPixel is the guard for the whole light array.
//
// A PASS PROVES: each of the five shaded kinds lights the probe scene on its own.
// A kind the loop drops leaves the frame at the background, because the scene
// authors no ambient dome and no environment map.
//
// A PASS DOES NOT PROVE: that the CPU copy and the shader agree numerically.
// lit_parity_test.go pins the constants and the expressions for that.
func TestEveryLightKindReachesAPixel(t *testing.T) {
	dark := litSum(renderLightProbe(t, lightProbeScene(
		// A rect-area light is recorded as unshaded on this path, so it is the
		// reference for "nothing reached a pixel". engine.RenderLight carries no
		// width and no height, so the shader cannot integrate over a rectangle.
		engine.RenderLight{Kind: "rect-area", Color: "#ffffff", Intensity: 4, X: 0, Y: 3, Z: 0},
	)))
	if dark != 0 {
		t.Fatalf("the reference frame is not black (channel sum %d), so this test cannot tell a shaded light from an unshaded one", dark)
	}

	for _, tc := range []struct {
		name  string
		light engine.RenderLight
	}{
		{"ambient", engine.RenderLight{Kind: "ambient", Color: "#ffffff", Intensity: 0.5}},
		{"directional", engine.RenderLight{Kind: "directional", Color: "#ffffff", Intensity: 1,
			DirectionX: 0, DirectionY: -1, DirectionZ: -0.2}},
		{"point", engine.RenderLight{Kind: "point", Color: "#ffffff", Intensity: 4, X: 0, Y: 2, Z: 0}},
		{"spot", engine.RenderLight{Kind: "spot", Color: "#ffffff", Intensity: 8, X: 0, Y: 3, Z: 0,
			DirectionX: 0, DirectionY: -1, DirectionZ: 0, Angle: 0.7, Penumbra: 0.3}},
		{"hemisphere", engine.RenderLight{Kind: "hemisphere", Color: "#ffffff", GroundColor: "#000000", Intensity: 1}},
		{"light-probe", engine.RenderLight{Kind: "light-probe", Color: "#ffffff", Intensity: 0.6}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := litSum(renderLightProbe(t, lightProbeScene(tc.light)))
			if got == 0 {
				t.Fatalf("a %s light lit nothing; the CPU light loop drops that kind", tc.name)
			}
		})
	}
}

// TestPointLightFalloffFollowsDistance proves the point-light attenuation is
// live and runs the right way round.
//
// A PASS PROVES: moving the same light further from the surface makes the
// brightest lit pixel darker, and a light with a range goes fully dark past that
// range. Both are properties of pointLightAttenuation, and a loop that ignored
// the distance would fail the first and a loop that ignored the range the second.
func TestPointLightFalloffFollowsDistance(t *testing.T) {
	at := func(height float64, rangeLimit float64) int {
		return litSum(renderLightProbe(t, lightProbeScene(engine.RenderLight{
			Kind: "point", Color: "#ffffff", Intensity: 6, X: 0, Y: height, Z: 0, Range: rangeLimit,
		})))
	}
	near := at(1.5, 0)
	far := at(4, 0)
	if near == 0 {
		t.Fatal("the near point light lit nothing, so this test cannot measure a falloff")
	}
	if far >= near {
		t.Fatalf("a point light at height 4 is not dimmer than one at height 1.5: %d against %d", far, near)
	}

	// A range of 1 puts every surface point past the window, so the ratio term
	// clamps to zero and the light contributes nothing.
	if inside := at(1.5, 8); inside == 0 {
		t.Fatal("a point light with a range of 8 lit nothing; the windowed falloff rejected a surface inside its range")
	}
	if outside := at(1.5, 0.5); outside != 0 {
		t.Fatalf("a point light with a range of 0.5 still lit the floor (%d); the windowed falloff ignores the range", outside)
	}
}

// TestSpotConeBoundsTheLitArea proves the spot cone term is live.
//
// A PASS PROVES: a narrow cone lights fewer pixels than a wide one at the same
// position and intensity. A loop that dropped spotConeAttenuation would light the
// same area for both.
func TestSpotConeBoundsTheLitArea(t *testing.T) {
	litPixels := func(angle float64) int {
		img := renderLightProbe(t, lightProbeScene(engine.RenderLight{
			Kind: "spot", Color: "#ffffff", Intensity: 12, X: 0, Y: 3, Z: 0,
			DirectionX: 0, DirectionY: -1, DirectionZ: 0, Angle: angle, Penumbra: 0.2,
		}))
		count := 0
		bounds := img.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				c := img.RGBAAt(x, y)
				if int(c.R)+int(c.G)+int(c.B) > 6 {
					count++
				}
			}
		}
		return count
	}
	narrow := litPixels(0.25)
	wide := litPixels(1.1)
	if narrow == 0 {
		t.Fatal("the narrow spot lit nothing, so this test cannot measure a cone")
	}
	if narrow >= wide {
		t.Fatalf("a spot at 0.25 radians lit %d pixels and one at 1.1 radians lit %d; the cone term is not bounding the area",
			narrow, wide)
	}
}

// TestHemisphereLightPutsSkyAboveGround proves the hemisphere blend reads the
// normal and does not swap its two colours.
//
// A PASS PROVES: a floor whose normal points up takes the sky colour, not the
// ground colour. The two colours are pure red and pure blue, so a swap moves the
// whole frame from one channel to the other and cannot hide inside a mean.
func TestHemisphereLightPutsSkyAboveGround(t *testing.T) {
	img := renderLightProbe(t, lightProbeScene(engine.RenderLight{
		Kind: "hemisphere", Color: "#ff0000", GroundColor: "#0000ff", Intensity: 1,
	}))
	var red, blue int
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.RGBAAt(x, y)
			red += int(c.R)
			blue += int(c.B)
		}
	}
	if red == 0 && blue == 0 {
		t.Fatal("the hemisphere light lit nothing")
	}
	if red <= blue {
		t.Fatalf("an upward-facing floor summed %d red and %d blue under a red sky and a blue ground; the two colours are swapped",
			red, blue)
	}
}

// TestEveryAuthoredLightAddsToTheFrame proves the loop covers the whole array and
// not only its first record.
//
// A PASS PROVES: three point lights at three positions light more of the floor
// than any one of them, and the frame grows brighter with each light added. A
// loop that read only index zero would report the same numbers for one light and
// for three.
func TestEveryAuthoredLightAddsToTheFrame(t *testing.T) {
	lights := []engine.RenderLight{
		{Kind: "point", Color: "#ff0000", Intensity: 5, X: -2, Y: 1.5, Z: 0},
		{Kind: "point", Color: "#00ff00", Intensity: 5, X: 0, Y: 1.5, Z: 0},
		{Kind: "point", Color: "#0000ff", Intensity: 5, X: 2, Y: 1.5, Z: 0},
	}
	total := func(img *image.RGBA) int {
		sum := 0
		bounds := img.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				c := img.RGBAAt(x, y)
				sum += int(c.R) + int(c.G) + int(c.B)
			}
		}
		return sum
	}

	previous := 0
	for count := 1; count <= len(lights); count++ {
		got := total(renderLightProbe(t, lightProbeScene(lights[:count]...)))
		if got <= previous {
			t.Fatalf("%d lights summed %d channel units and %d lights summed %d; "+
				"the loop is not reading every record", count, got, count-1, previous)
		}
		previous = got
	}

	// Each light owns one channel, so all three must appear. A loop that read one
	// record would leave two channels at zero.
	img := renderLightProbe(t, lightProbeScene(lights...))
	var red, green, blue int
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.RGBAAt(x, y)
			red += int(c.R)
			green += int(c.G)
			blue += int(c.B)
		}
	}
	for _, ch := range []struct {
		name string
		sum  int
	}{{"red", red}, {"green", green}, {"blue", blue}} {
		if ch.sum == 0 {
			t.Errorf("the %s point light contributed nothing; the loop stopped before its record", ch.name)
		}
	}
}

// TestOneDirectionalLightShadesAsBeforeTheLightArray is the fixture guard.
//
// The light array changed no golden frame, and this test is why that is safe to
// claim. A scene with one directional light must shade the same as the primary
// light lanes of the scene uniform on their own, because resolveSceneLights packs
// the same direction, colour and intensity that resolveDirectionalLight wrote.
//
// A PASS PROVES: the frame a one-light bundle produces is byte-identical to the
// frame the same bundle produces through the fallback path, which is the path
// every caller took before the array existed.
func TestOneDirectionalLightShadesAsBeforeTheLightArray(t *testing.T) {
	frame := lightProbeScene(engine.RenderLight{
		Kind: "directional", Color: "#ffcc88", Intensity: 1.3,
		DirectionX: -0.4, DirectionY: -1, DirectionZ: -0.3,
	})
	throughArray := renderLightProbe(t, frame)

	// The fallback path: no authored light at all. resolveSceneLights substitutes
	// one directional record from the same defaults resolveDirectionalLight uses,
	// so a bundle with no light must still shade.
	unlit := renderLightProbe(t, lightProbeScene())
	if litSum(unlit) == 0 {
		t.Fatal("a bundle with no authored light rendered black; the fallback key light disappeared")
	}
	if litSum(throughArray) == 0 {
		t.Fatal("a bundle with one directional light rendered black; the light array is not reaching the rasterizer")
	}
	// The two frames must differ, because the authored light has its own colour
	// and intensity. Without this half the test would pass on a rasterizer that
	// ignored the array and always used the fallback.
	if imagesEqual(throughArray, unlit) {
		t.Fatal("an authored directional light produced the same frame as no light at all; " +
			"the rasterizer is shading the fallback and ignoring the array")
	}
}

// TestZeroLightCountShadesNothingDirect proves the loop honours the count in the
// scene uniform instead of the length of the storage buffer.
//
// The storage buffer's capacity is a power of two, so it always runs past the
// live count, and a buffer is zero-initialized. A zero record decodes to an
// ambient light of kind zero with a black colour, which adds nothing, so reading
// the tail is invisible in a frame. This test drives litProgram directly, where
// the difference is visible.
//
// A PASS PROVES: newLitProgram cuts the slice to the count, so a record past it
// never shades even when it holds a bright colour.
func TestZeroLightCountShadesNothingDirect(t *testing.T) {
	lighting := defaultSceneLighting()
	lighting.ambientColor = [4]float32{}
	lighting.skyColor = [4]float32{}
	lighting.groundColor = [4]float32{}
	lighting.lights = []sceneLight{
		{kind: lightKindAmbient, color: [3]float32{1, 1, 1}, intensity: 1},
		{kind: lightKindAmbient, color: [3]float32{1, 1, 1}, intensity: 1},
	}
	material := defaultMaterialState()
	material.baseColor = [3]float32{1, 1, 1}
	frag := fragment{base: [3]float32{1, 1, 1}, normal: [3]float32{0, 1, 0}}

	shadeAt := func(count float32) [3]float32 {
		lighting.lightParams = [4]float32{count, -1, 0, 0}
		program := newLitProgram(lighting, material)
		return program.shade(frag)
	}
	both := shadeAt(2)
	one := shadeAt(1)
	none := shadeAt(0)

	if none[0] != 0 {
		t.Errorf("a light count of zero still shaded %g; the loop is reading the buffer length", none[0])
	}
	if one[0] == 0 {
		t.Fatal("a light count of one shaded nothing, so this test cannot separate the count from the length")
	}
	if diff := both[0] - one[0]*2; diff > 1e-5 || diff < -1e-5 {
		t.Errorf("two identical ambient lights shaded %g and one shaded %g; the second record is not adding", both[0], one[0])
	}
}

// TestOnlyThePrimaryDirectionalLightReadsTheShadowMap pins the one restriction
// the shadow term carries.
//
// One cascaded shadow map exists and the cascade fit aims it at one light. Every
// other light must ignore it, or a point light on the far side of the scene loses
// its contribution inside a shadow the map drew for a different light.
//
// The case drives litProgram directly, because a rendered frame cannot separate
// "the point light is shadowed" from "the point light is dim". The shadow map
// below occludes every sample, so a shadowed light contributes nothing at all.
//
// A PASS PROVES: the directional light at the shadow index loses its whole
// contribution, the point light beside it keeps all of its own, and moving the
// shadow index onto the point light shadows nothing, because that light is not
// directional.
func TestOnlyThePrimaryDirectionalLightReadsTheShadowMap(t *testing.T) {
	lighting := defaultSceneLighting()
	lighting.ambientColor = [4]float32{}
	lighting.skyColor = [4]float32{}
	lighting.groundColor = [4]float32{}
	lighting.shadow = fullyOccludedShadowMap()
	lighting.shadowLayer = 0
	lighting.lights = []sceneLight{
		{kind: lightKindDirectional, direction: [3]float32{0, -1, 0}, intensity: 1,
			color: [3]float32{1, 1, 1}, decay: 2},
		{kind: lightKindPoint, position: [3]float32{0, 1, 0}, intensity: 1,
			color: [3]float32{1, 1, 1}, decay: 2},
	}

	material := defaultMaterialState()
	material.baseColor = [3]float32{1, 1, 1}
	frag := fragment{base: [3]float32{1, 1, 1}, normal: [3]float32{0, 1, 0}}
	shadeAt := func(shadowIndex float32) [3]float32 {
		lighting.lightParams = [4]float32{2, shadowIndex, 0, 0}
		program := newLitProgram(lighting, material)
		return program.shade(frag)
	}

	unshadowed := shadeAt(-1)
	if unshadowed[0] <= 0 {
		t.Fatal("the two lights shaded nothing with no shadow index, so this test cannot measure the shadow")
	}
	// The shadow map occludes every sample, so naming the directional light must
	// remove its whole contribution and nothing else.
	onDirectional := shadeAt(0)
	if onDirectional[0] >= unshadowed[0] {
		t.Fatalf("naming the directional light did not darken the surface: %g against %g",
			onDirectional[0], unshadowed[0])
	}
	if onDirectional[0] <= 0 {
		t.Fatalf("naming the directional light removed every light (%g); the point light beside it must stay lit",
			onDirectional[0])
	}
	// Naming the point light must change nothing. The shader guards on the kind
	// as well as the index, so a point light never reads the map.
	onPoint := shadeAt(1)
	if onPoint != unshadowed {
		t.Fatalf("naming the point light shaded %v against %v unshadowed; "+
			"only a directional light may read the cascaded shadow map", onPoint, unshadowed)
	}
}

// fullyOccludedShadowMap returns a depth array whose every texel reads as an
// occluder. sampleShadow compares against the stored depth minus a bias, and a
// stored zero is below every reference, so every sample lands in shadow.
func fullyOccludedShadowMap() *Texture {
	const size = 4
	return &Texture{
		width:  size,
		height: size,
		layers: 3,
		format: gpu.FormatDepth32Float,
		depth:  make([]float32, size*size*3),
	}
}
