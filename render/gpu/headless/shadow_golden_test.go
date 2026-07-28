package headless

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/bundle"
)

// shadowSceneWidth and shadowSceneHeight keep the rasterized shadow fixtures
// small. The CPU rasterizer fills every shadow-map texel it touches, so a large
// framebuffer makes these tests slow without telling us anything more.
const (
	shadowSceneWidth  = 48
	shadowSceneHeight = 32
)

// shadowScene builds a lit, shadowed, instanced scene: one wide ground plane
// plus a row of cubes floating above it, lit straight down. The camera looks
// along +X from above, which is the case the old cascade fit could not serve:
// it placed every cascade as though the camera looked down world -Z.
//
// headingRadians turns the camera about Y. The scene geometry turns with it, so
// the rendered image should be the same whatever heading the caller picks. A
// fit that ignores camera rotation only produces shadows at heading zero.
func shadowScene(headingRadians float64) engine.RenderBundle {
	// Camera 5 units up, tilted down so the ground fills the frame.
	const camHeight = 5.0
	const tilt = 0.5586 // atan(5/8): centres the frame on the ground 8 units out.

	sin, cos := math.Sin(headingRadians), math.Cos(headingRadians)
	// forward is the world direction the camera looks along on the ground plane.
	// RotationY = h turns the view axis from -Z toward +X.
	forward := [2]float64{sin, -cos}
	right := [2]float64{cos, sin}

	// Ground centred 8 units ahead.
	groundX := forward[0] * 8
	groundZ := forward[1] * 8
	ground := []float64{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		groundX, 0, groundZ, 1,
	}

	// Three cubes in a row across the view, 2 units above the ground.
	var cubes []float64
	for _, offset := range []float64{-3, 0, 3} {
		x := groundX + right[0]*offset
		z := groundZ + right[1]*offset
		cubes = append(cubes,
			1, 0, 0, 0,
			0, 1, 0, 0,
			0, 0, 1, 0,
			x, 2, z, 1,
		)
	}

	return engine.RenderBundle{
		Background: "#000000",
		Camera: engine.RenderCamera{
			Y: camHeight, RotationX: tilt, RotationY: headingRadians,
			FOV: math.Pi / 3, Near: 0.1, Far: 60,
		},
		Materials: []engine.RenderMaterial{
			// Mid grey, not white. A white surface saturates under a full-strength
			// key light, and a saturated surface hides the shadow it receives.
			{Kind: "standard", Color: "#8a8a8a", Roughness: 0.9},
		},
		// Author every environment term, and keep the dome dim.
		//
		// Two reasons. resolveHemisphereAmbient substitutes a sky and ground
		// intensity of one when either is unset, and that default is under dispute,
		// so a fixture must not depend on it. And sceneLighting.shade now sums the
		// three ambient terms independently, matching litWGSL, so a bright dome
		// plus a full-strength key light saturates the mid-grey ground and the
		// fixture loses the range it needs to show a shadow.
		Environment: engine.RenderEnvironment{
			AmbientColor: "#404858", AmbientIntensity: 0.35,
			SkyColor: "#ccddff", SkyIntensity: 0.15,
			GroundColor: "#483c38", GroundIntensity: 0.10,
		},
		Lights: []engine.RenderLight{{
			Kind: "directional", Color: "#ffffff", Intensity: 1,
			DirectionX: 0, DirectionY: -1, DirectionZ: 0,
		}},
		InstancedMeshes: []engine.RenderInstancedMesh{
			{
				ID: "ground", Kind: "plane", Width: 40, Height: 40,
				MaterialIndex: 0, InstanceCount: 1, Transforms: ground,
			},
			{
				ID: "casters", Kind: "cube", Size: 1.6,
				MaterialIndex: 0, InstanceCount: 3, Transforms: cubes,
				CastShadow: true,
			},
		},
	}
}

// renderShadowScene draws one heading and returns the framebuffer.
func renderShadowScene(t *testing.T, headingRadians float64) *image.RGBA {
	t.Helper()
	return renderShadowSceneWithToneMap(t, headingRadians, "")
}

// renderShadowSceneWithToneMap draws one heading under one tone-map name. An
// empty name takes the default, which resolveToneMapConfig turns into ACES.
func renderShadowSceneWithToneMap(t *testing.T, headingRadians float64, toneMapping string) *image.RGBA {
	t.Helper()
	d, surface := New(shadowSceneWidth, shadowSceneHeight)
	r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer r.Destroy()
	frame := shadowScene(headingRadians)
	frame.Environment.ToneMapping = toneMapping
	if err := r.Frame(frame, shadowSceneWidth, shadowSceneHeight, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	return d.Framebuffer()
}

// shadowedPixels counts pixels that are lit but clearly darker than the fully
// lit ground. The ground is mid grey and rough, so a shadowed sample keeps only
// its ambient term and reads far below a lit one. Fully black pixels are
// background and do not count.
//
// The lit threshold moved from 300 to 160 when litProgram.shade adopted the
// energy-conserving diffuse lobe of litWGSL. The old model wrote base*NdotL, and
// the new one writes kD*base/pi, which is the form litWGSL, both browser
// renderers and three.js all use. That is about three times darker.
//
// The present pass then started running the ACES curve, which lifted all four
// levels: 0 background, 93 an unlit cube face, 135 shadowed ground and 333
// sunlit ground. Any split between 135 and 333 separates the two ground states,
// and 160 still sits inside that gap, so the threshold did not move again.
func shadowedPixels(img *image.RGBA) (shadowed, lit int) {
	const litChannelSum = 160
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.RGBAAt(x, y)
			v := int(c.R) + int(c.G) + int(c.B)
			switch {
			case v < 24:
				// Background.
			case v < litChannelSum:
				shadowed++
			default:
				lit++
			}
		}
	}
	return shadowed, lit
}

// TestShadowedSceneRendersShadowsAtEveryHeading is the rasterized guard for the
// cascade fit. The scene turns with the camera, so every heading must produce
// the same amount of shadow. The rotation-blind fit produced shadows only near
// heading zero and left the ground flat-lit everywhere else.
func TestShadowedSceneRendersShadowsAtEveryHeading(t *testing.T) {
	base := renderShadowScene(t, 0)
	baseShadow, baseLit := shadowedPixels(base)
	if baseShadow == 0 {
		t.Fatalf("heading 0 rendered no shadow at all: %d lit pixels", baseLit)
	}
	if baseLit == 0 {
		t.Fatal("heading 0 rendered no lit ground; the fixture is wrong")
	}

	for _, heading := range []float64{math.Pi / 2, math.Pi, -math.Pi / 2, 2.3} {
		img := renderShadowScene(t, heading)
		shadow, lit := shadowedPixels(img)
		if shadow == 0 {
			t.Errorf("heading %.2f rendered no shadow; the cascade fit ignores the camera heading", heading)
			continue
		}
		// The scene is rotationally symmetric, so the shadow area must match the
		// reference. The slack covers rasterization only: an off-axis heading
		// moves the shadow edge by a few pixels. The rotation-blind fit lost 43
		// of 155 shadow pixels at heading pi and 18 at heading 2.3, so this
		// bound rejects it while the pixel-exact headings pass with room.
		if diff := math.Abs(float64(shadow - baseShadow)); diff > float64(baseShadow)*0.03+8 {
			t.Errorf("heading %.2f shadowed %d pixels, heading 0 shadowed %d; the fits disagree",
				heading, shadow, baseShadow)
		}
		if lit == 0 {
			t.Errorf("heading %.2f rendered no lit ground", heading)
		}
	}
}

// TestShadowedSceneMatchesGoldenAtHeadingZero pins the exact rendered image for
// one lit, shadowed, instanced scene. The only other golden test in this
// package compares a flat red quad against an image built in the same file,
// which cannot catch a lighting, shadow, or instancing regression.
//
// The golden is stored as a run-length encoded luminance ramp so the fixture
// stays readable and small.
func TestShadowedSceneMatchesGoldenAtHeadingZero(t *testing.T) {
	got := renderShadowScene(t, 0)
	want := decodeLuminanceGolden(t, shadowSceneWidth, shadowSceneHeight, shadowGoldenHeading0)
	assertGoldenMatch(t, quantizeLuminance(got), want, 0)
}

// TestPresentPassMovesLevelsNotShadowGeometry is the evidence behind the last
// regeneration of shadowGoldenHeading0.
//
// The present pass now runs the authored tone map instead of copying the frame,
// so every level in the fixture moved. That is the whole change. The check below
// renders the same scene twice, once with tone mapping "none" so the pass only
// clamps, and once with the default ACES curve, then proves three things:
//
//  1. the two frames differ, so the curve reaches the image;
//  2. the run-length structure of the fixture is identical, so no pixel changed
//     which patch it belongs to;
//  3. shadowedPixels counts the same shadowed and lit totals, so the shadow
//     boundary sits where it did.
//
// Together those reject the failure a regenerated golden hides: a curve that
// also moved, softened or erased a shadow edge.
func TestPresentPassMovesLevelsNotShadowGeometry(t *testing.T) {
	clamped := renderShadowSceneWithToneMap(t, 0, "none")
	curved := renderShadowSceneWithToneMap(t, 0, "")

	if imagesEqual(clamped, curved) {
		t.Fatal("the ACES curve changed no pixel; the present pass is still a copy")
	}

	clampedRuns := goldenRunLengths(encodeLuminanceGolden(quantizeLuminance(clamped)))
	curvedRuns := goldenRunLengths(encodeLuminanceGolden(quantizeLuminance(curved)))
	if len(clampedRuns) != len(curvedRuns) {
		t.Fatalf("the curve changed the run count from %d to %d, so it moved a patch boundary, not only a level",
			len(clampedRuns), len(curvedRuns))
	}
	for i := range clampedRuns {
		if clampedRuns[i] != curvedRuns[i] {
			t.Fatalf("run %d is %d pixels long clamped and %d curved; the curve moved a patch boundary",
				i, clampedRuns[i], curvedRuns[i])
		}
	}

	clampedShadow, clampedLit := shadowedPixels(clamped)
	curvedShadow, curvedLit := shadowedPixels(curved)
	if clampedShadow != curvedShadow || clampedLit != curvedLit {
		t.Fatalf("the curve changed the shadow area from %d/%d to %d/%d shadowed over lit pixels",
			clampedShadow, clampedLit, curvedShadow, curvedLit)
	}
	if clampedShadow == 0 || clampedLit == 0 {
		t.Fatalf("the fixture scene shows %d shadowed and %d lit pixels, so it cannot prove anything",
			clampedShadow, clampedLit)
	}
}

// goldenRunLengths returns the length of every run in an encoded fixture and
// discards the ramp step. Two fixtures with the same lengths hold the same
// patches at the same places, whatever value each patch carries.
func goldenRunLengths(encoded string) []int {
	runs := strings.Split(encoded, ",")
	out := make([]int, 0, len(runs))
	for _, run := range runs {
		if star := strings.IndexByte(run, '*'); star >= 0 {
			count, err := strconv.Atoi(run[star+1:])
			if err != nil {
				count = 0
			}
			out = append(out, count)
			continue
		}
		out = append(out, 1)
	}
	return out
}

// luminanceRampStep is the width of one step of the golden fixture's luminance
// ramp. Sixteen steps separate a shadowed sample from an ambient-lit cube face
// and from the sunlit ground; eight steps put all three on the same value and
// the fixture stops saying anything about the lighting.
const luminanceRampStep = 16

// quantizeLuminance reduces an image to the luminance ramp the golden fixture
// stores. Exact colour reproduction across tone mapping and FXAA is not the
// point; the shape and the depth of the shadow are.
func quantizeLuminance(img *image.RGBA) *image.RGBA {
	bounds := img.Bounds()
	out := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.RGBAAt(x, y)
			lum := (int(c.R)*299 + int(c.G)*587 + int(c.B)*114) / 1000
			v := uint8(lum / luminanceRampStep * luminanceRampStep)
			out.SetRGBA(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return out
}

// decodeLuminanceGolden expands a run-length string of ramp steps into an
// image. Runs are separated by commas. Each run reads "step" for a single pixel
// or "step*count" for a repeat, with the step as one hexadecimal digit. The
// comma matters: without it a decimal count runs straight into the next step
// digit and the fixture decodes to something else entirely.
func decodeLuminanceGolden(t *testing.T, width, height int, encoded string) *image.RGBA {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	idx := 0
	for _, run := range strings.Split(encoded, ",") {
		step, count := run, "1"
		if star := strings.IndexByte(run, '*'); star >= 0 {
			step, count = run[:star], run[star+1:]
		}
		parsed, err := strconv.ParseInt(step, 16, 32)
		stepValue := int(parsed)
		if err != nil || stepValue < 0 || stepValue > 15 {
			t.Fatalf("golden run %q has a bad ramp step", run)
		}
		repeat, err := strconv.Atoi(count)
		if err != nil || repeat < 1 {
			t.Fatalf("golden run %q has a bad count", run)
		}
		v := uint8(stepValue * luminanceRampStep)
		for i := 0; i < repeat; i++ {
			if idx >= width*height {
				t.Fatalf("golden holds more pixels than the %dx%d image", width, height)
			}
			img.SetRGBA(idx%width, idx/width, color.RGBA{R: v, G: v, B: v, A: 255})
			idx++
		}
	}
	if idx != width*height {
		t.Fatalf("golden holds %d pixels, want %d", idx, width*height)
	}
	return img
}

// encodeLuminanceGolden is the inverse of decodeLuminanceGolden. Keep it: it is
// how the fixture below gets regenerated after a deliberate rendering change,
// and a decoder with no encoder beside it drifts.
func encodeLuminanceGolden(img *image.RGBA) string {
	var runs []string
	prev, count := -1, 0
	flush := func() {
		if count < 1 {
			return
		}
		run := strconv.FormatInt(int64(prev), 16)
		if count > 1 {
			run += "*" + strconv.Itoa(count)
		}
		runs = append(runs, run)
	}
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			step := int(img.RGBAAt(x, y).R) / luminanceRampStep
			if step == prev {
				count++
				continue
			}
			flush()
			prev, count = step, 1
		}
	}
	flush()
	return strings.Join(runs, ",")
}

// TestShadowSkipDoesNotLeaveAStaleOccluder is the pixel proof for the shadow-pass
// skip in render/bundle/renderer.go.
//
// recordShadowPass returns early on a frame with no caster, so the cascades keep
// whatever the previous frame drew. That is safe only while the cascades hold the
// clear value. This test drives the dangerous order on one reused renderer: draw
// the casters, then take them away, then read the ground.
//
// A PASS PROVES: the ground carries no shadow after the casters go, and the
// picture matches a renderer that never saw a caster at all.
//
// A PASS DOES NOT PROVE: that the skip fires. TestShadowPassSkipsWhenNothingCasts
// in render/bundle counts the recorded passes.
func TestShadowSkipDoesNotLeaveAStaleOccluder(t *testing.T) {
	withCasters := shadowScene(0)
	// Same scene, casters removed. The ground and the light stay, so every pixel
	// that changes is a shadow and nothing else.
	noCasters := withCasters
	noCasters.InstancedMeshes = append([]engine.RenderInstancedMesh(nil),
		withCasters.InstancedMeshes[:1]...)

	// One renderer, three frames, in the order that exposes a stale map.
	device, surface := New(shadowSceneWidth, shadowSceneHeight)
	renderer, err := bundle.New(bundle.Config{Device: device, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer renderer.Destroy()
	frame := func(scene engine.RenderBundle, index int) *image.RGBA {
		if err := renderer.Frame(scene, shadowSceneWidth, shadowSceneHeight, float64(index)/60); err != nil {
			t.Fatalf("Frame %d: %v", index, err)
		}
		return cloneTestRGBA(device.Framebuffer())
	}
	shadowed := frame(withCasters, 0)
	if got, _ := shadowedPixels(shadowed); got == 0 {
		t.Fatal("the caster frame drew no shadow, so this test cannot see a stale one")
	}
	// The frame straight after the casters go, then one more. The skip only fires
	// on the second, so both have to be clean.
	firstClear := frame(noCasters, 1)
	secondClear := frame(noCasters, 2)

	// A renderer that never saw a caster is the reference.
	reference := func() *image.RGBA {
		freshDevice, freshSurface := New(shadowSceneWidth, shadowSceneHeight)
		freshRenderer, err := bundle.New(bundle.Config{Device: freshDevice, Surface: freshSurface})
		if err != nil {
			t.Fatalf("bundle.New: %v", err)
		}
		defer freshRenderer.Destroy()
		if err := freshRenderer.Frame(noCasters, shadowSceneWidth, shadowSceneHeight, 0); err != nil {
			t.Fatalf("reference Frame: %v", err)
		}
		return cloneTestRGBA(freshDevice.Framebuffer())
	}()
	if got, _ := shadowedPixels(reference); got != 0 {
		t.Fatalf("the caster-free reference already holds %d shadowed pixels; the fixture is wrong", got)
	}

	for _, tc := range []struct {
		name string
		img  *image.RGBA
	}{
		{"the frame after the casters go", firstClear},
		{"the frame after that, where the skip fires", secondClear},
	} {
		if got, _ := shadowedPixels(tc.img); got != 0 {
			t.Errorf("%s still holds %d shadowed pixels; "+
				"recordShadowPass skipped a frame whose cascades were not clear", tc.name, got)
		}
		if diff := imagePixelDiff(tc.img, reference); diff != 0 {
			t.Errorf("%s differs from a renderer that never saw a caster in %d pixels", tc.name, diff)
		}
	}
}

// imagePixelDiff counts pixels that differ between two images of the same size.
func imagePixelDiff(a, b *image.RGBA) int {
	count := 0
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				count++
			}
		}
	}
	return count
}

// TestLuminanceGoldenCodecRoundTrips guards the fixture format itself. An
// encoding that cannot survive a round trip makes the golden test above compare
// the render against noise and pass or fail for the wrong reason.
func TestLuminanceGoldenCodecRoundTrips(t *testing.T) {
	source := quantizeLuminance(renderShadowScene(t, 0))
	decoded := decodeLuminanceGolden(t, shadowSceneWidth, shadowSceneHeight,
		encodeLuminanceGolden(source))
	for y := 0; y < shadowSceneHeight; y++ {
		for x := 0; x < shadowSceneWidth; x++ {
			if source.RGBAAt(x, y) != decoded.RGBAAt(x, y) {
				t.Fatalf("round trip changed pixel (%d, %d): %v then %v",
					x, y, source.RGBAAt(x, y), decoded.RGBAAt(x, y))
			}
		}
	}
}

// shadowGoldenHeading0 is the run-length encoded luminance ramp of the lit,
// shadowed, instanced scene at heading zero. Regenerate it only with a
// deliberate rendering change, and say which change in the commit message.
//
// Reading the fixture: 0 is the background, 1 is the unlit side of a cube, 2 is
// ground in shadow, and 6 is sunlit ground. The three shadow patches sit in the
// band below the row of cubes.
//
// Regenerated four times, each for a deliberate change:
//
//   - The shading model sums the ambient, sky and ground terms independently,
//     matching litWGSL and both browser renderers. The earlier form multiplied
//     the dome by the ambient intensity and made every frame about three times
//     too dark.
//   - The scene now authors its environment instead of taking the defaults, so
//     the fixture no longer depends on the disputed sky and ground intensity
//     substitution in resolveHemisphereAmbient.
//   - litProgram.shade replaced the Lambert term with the whole fragment stage
//     of litWGSL: an energy-conserving diffuse lobe of kD*base/pi plus a
//     Cook-Torrance specular lobe. Sunlit ground moved from ramp step a to ramp
//     step 4 for that reason alone. Every run of the fixture kept its position
//     and its length, so the shape of the three shadow patches did not move.
//     The old value was wrong: base*NdotL carries about pi times the energy the
//     surface reflects, and no headless frame could see it.
//   - The headless device runs the present pass instead of copying it, so the
//     authored tone map reaches the frame. This scene takes the default, which
//     is ACES. The curve lifts the low end and rolls the high end off, so all
//     four levels moved: the background stayed at 0, an unlit cube face went
//     from a channel sum of 75 to 93, shadowed ground from 98 to 135, and
//     sunlit ground from 228 to 333. Nothing else moved. Every one of the 62
//     runs kept its length, and shadowedPixels still counts 155 shadowed and
//     1140 lit pixels, the same two numbers as before. The old value was wrong
//     because the browser tone maps the same frame and this device did not, so
//     a headless capture and a poster read brighter in the shadows and clipped
//     in the highlights. TestPresentPassMovesLevelsNotShadowGeometry holds that
//     evidence as a running test.
//
// The curve separated two levels that used to share a ramp step. A shadowed
// ground sample now reads 2 and an unlit cube face reads 1. Read the shape of
// the three patches first; the gap between those two values is small.
const shadowGoldenHeading0 = `0*240,6*47,0,6*64,1,6*13,1,6*26,1*8,6*4,1*5,6*4,1*8,6*20,1*7,6*4,1*5,6*4,1*7,6*21,` +
	`1*7,6*4,1*5,6*4,1*7,6*21,1*7,6*4,1*5,6*4,1*7,6*22,1*6,6*4,1*5,6*4,1*6,6*23,1*5,6*5,` +
	`1*5,6*5,1*5,6*25,2*5,6*3,2*5,6*3,2*5,6*27,2*4,6*4,2*5,6*4,2*4,6*26,2*5,6*4,2*5,6*4,` +
	`2*5,6*733`
