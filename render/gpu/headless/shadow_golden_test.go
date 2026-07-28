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
	d, surface := New(shadowSceneWidth, shadowSceneHeight)
	r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer r.Destroy()
	if err := r.Frame(shadowScene(headingRadians), shadowSceneWidth, shadowSceneHeight, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	return d.Framebuffer()
}

// shadowedPixels counts pixels that are lit but clearly darker than the fully
// lit ground. The ground is mid grey and rough, so a shadowed sample keeps only
// its ambient term and reads far below a lit one. Fully black pixels are
// background and do not count.
func shadowedPixels(img *image.RGBA) (shadowed, lit int) {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.RGBAAt(x, y)
			v := int(c.R) + int(c.G) + int(c.B)
			switch {
			case v < 24:
				// Background.
			case v < 300:
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
// Reading the fixture: 0 is the background and the unlit side of a cube, 1 is
// ground in shadow, and 9 is sunlit ground. The three shadow patches sit in the
// band below the row of cubes.
const shadowGoldenHeading0 = `0*240,9*47,0,9*64,0,9*13,0,9*26,0*8,9*4,0*5,9*4,0*8,9*20,0*7,9*4,0*5,9*4,0*7,9*21,` +
	`0*7,9*4,0*5,9*4,0*7,9*21,0*7,9*4,0*5,9*4,0*7,9*22,0*6,9*4,0*5,9*4,0*6,9*23,0*5,9*5,` +
	`0*5,9*5,0*5,9*25,1*5,9*3,1*5,9*3,1*5,9*27,1*4,9*4,1*5,9*4,1*4,9*26,1*5,9*4,1*5,9*4,` +
	`1*5,9*733`
