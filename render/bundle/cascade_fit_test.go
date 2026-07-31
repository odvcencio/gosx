package bundle

import (
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
)

// cascadeBoxRadius recovers the fitted orthographic half-extent of one cascade
// from its view-projection matrix. The light view is a rotation plus a
// translation, so the first row of the combined 3x3 keeps the projection's
// 2/size scale and nothing else. Tests read the value to compare shadow-map
// texel density between fits.
func cascadeBoxRadius(vp mat4) float32 {
	scale := math.Sqrt(float64(vp[0]*vp[0] + vp[4]*vp[4] + vp[8]*vp[8]))
	if scale == 0 {
		return 0
	}
	return float32(1 / scale)
}

// insideCascade reports whether a world point falls inside a cascade's clip
// volume. The cascade projection puts clip space between -1 and 1 on every
// axis, and a point outside that volume gets no shadow-map coverage.
func insideCascade(vp mat4, p [3]float32) bool {
	x := vp[0]*p[0] + vp[4]*p[1] + vp[8]*p[2] + vp[12]
	y := vp[1]*p[0] + vp[5]*p[1] + vp[9]*p[2] + vp[13]
	z := vp[2]*p[0] + vp[6]*p[1] + vp[10]*p[2] + vp[14]
	w := vp[3]*p[0] + vp[7]*p[1] + vp[11]*p[2] + vp[15]
	if w == 0 {
		return false
	}
	x, y, z = x/w, y/w, z/w
	return x >= -1 && x <= 1 && y >= -1 && y <= 1 && z >= -1 && z <= 1
}

// cascadeUV projects a world point into one cascade's shadow-map texture
// coordinates, matching the mapping sampleShadow uses in litWGSL.
func cascadeUV(vp mat4, p [3]float32) (u, v float32, ok bool) {
	x := vp[0]*p[0] + vp[4]*p[1] + vp[8]*p[2] + vp[12]
	y := vp[1]*p[0] + vp[5]*p[1] + vp[9]*p[2] + vp[13]
	w := vp[3]*p[0] + vp[7]*p[1] + vp[11]*p[2] + vp[15]
	if w == 0 {
		return 0, 0, false
	}
	return (x/w)*0.5 + 0.5, 0.5 - (y/w)*0.5, true
}

var straightDownLight = [3]float32{0, -1, 0}

// TestCascadeFitFollowsCameraRotation pins the rotated-camera defect. The fit
// used to place every cascade as though the camera looked down world -Z, so a
// camera turned a quarter turn fitted its cascades to empty space and the whole
// scene lost its shadows.
func TestCascadeFitFollowsCameraRotation(t *testing.T) {
	// A narrow field of view and a short far plane keep cascade 0 small, so the
	// check separates a fit that follows the heading from one that does not.
	// RotationY = pi/2 turns the camera to look down world +X.
	cam := engine.RenderCamera{
		X: 0, Y: 4, Z: 0,
		RotationY: math.Pi / 2,
		FOV:       math.Pi / 6, Near: 0.1, Far: 20,
	}
	cascades := computeCascades(cam, straightDownLight, defaultCascadeLambda, 16.0/9.0)

	// A point on the camera's own view axis, inside cascade 0's range.
	ahead := [3]float32{2.5, 4, 0}
	if !insideCascade(cascades.viewProjs[0], ahead) {
		t.Fatalf("cascade 0 does not cover %v, the point the rotated camera looks at", ahead)
	}

	// Down world -Z, where the old rotation-blind fit centred cascade 0. That
	// fit reached this point; a fit that follows the heading must not.
	stale := [3]float32{0, 4, -4.5}
	if insideCascade(cascades.viewProjs[0], stale) {
		t.Errorf("cascade 0 covers %v, which the rotated camera never sees", stale)
	}
}

// TestCascadeFitCoversWideViewportEdges pins the aspect defect. The fit assumed
// a square framebuffer, so on a wide viewport the real frustum reached past the
// fitted box and casters near the left and right screen edges lost their
// shadows.
func TestCascadeFitCoversWideViewportEdges(t *testing.T) {
	const aspect = 21.0 / 9.0
	cam := engine.RenderCamera{Y: 4, Z: 0, FOV: math.Pi / 3, Near: 0.1, Far: 100}
	cascades := computeCascades(cam, straightDownLight, defaultCascadeLambda, aspect)

	splits := cascadeSplitDistances(0.1, 100, cascadeCount, defaultCascadeLambda)
	tanHalf := float32(math.Tan(math.Pi / 6))
	for i := 0; i < cascadeCount; i++ {
		// A point at the far edge of this cascade's slice, at the extreme right
		// of the frustum. The camera looks down -Z from (0, 4, 0).
		depth := splits[i+1] * 0.98
		edge := [3]float32{tanHalf * depth * aspect * 0.98, 4, -depth}
		if !insideCascade(cascades.viewProjs[i], edge) {
			t.Errorf("cascade %d does not cover the wide-viewport edge point %v", i, edge)
		}
	}
}

// TestCascadeFitIsTighterThanThePaddedFit measures the resolution win. The old
// fit widened the bounding sphere by a fifth. Removing the padding shrinks each
// cascade box by the same fifth and raises shadow-map texel density by its
// square.
func TestCascadeFitIsTighterThanThePaddedFit(t *testing.T) {
	cam := engine.RenderCamera{Z: 10, FOV: math.Pi / 3, Near: 0.1, Far: 100}
	cascades := computeCascades(cam, straightDownLight, defaultCascadeLambda, 1)

	splits := cascadeSplitDistances(0.1, 100, cascadeCount, defaultCascadeLambda)
	ratio := radialRatio(math.Pi/3, 1)
	for i := 0; i < cascadeCount; i++ {
		sliceNear := splits[i] / ratio
		if sliceNear < 0.1 {
			sliceNear = 0.1
		}
		exact := exactSliceSphereRadius(cam, 1, sliceNear, splits[i+1])
		got := cascadeBoxRadius(cascades.viewProjs[i])
		if math.Abs(float64(got-exact)) > float64(exact)*0.001 {
			t.Errorf("cascade %d half-extent = %v, want the exact slice sphere radius %v",
				i, got, exact)
		}
		// The padded fit was 1.2x wider. Prove the new box is smaller than it.
		if got >= exact*1.2 {
			t.Errorf("cascade %d half-extent %v is not tighter than the padded %v",
				i, got, exact*1.2)
		}
	}
}

// exactSliceSphereRadius returns the bounding-sphere radius of one view-frustum
// slice in world space, computed directly from the corner geometry. It is the
// independent oracle for what buildCascadeMatrix should fit.
func exactSliceSphereRadius(cam engine.RenderCamera, aspect, viewNear, viewFar float32) float32 {
	tanHalf := float32(math.Tan(float64(cam.FOV) / 2))
	var corners [8][3]float32
	idx := 0
	for _, d := range [2]float32{viewNear, viewFar} {
		for _, sx := range [2]float32{-1, 1} {
			for _, sy := range [2]float32{-1, 1} {
				corners[idx] = [3]float32{sx * tanHalf * d * aspect, sy * tanHalf * d, -d}
				idx++
			}
		}
	}
	toWorld := cameraViewToWorldRotation(cam)
	var cx, cy, cz float32
	for i := range corners {
		w := rotateVec3(toWorld, corners[i])
		corners[i] = [3]float32{
			w[0] + float32(cam.X), w[1] + float32(cam.Y), w[2] + float32(cam.Z),
		}
		cx += corners[i][0]
		cy += corners[i][1]
		cz += corners[i][2]
	}
	cx /= 8
	cy /= 8
	cz /= 8
	var r float32
	for _, c := range corners {
		dx, dy, dz := c[0]-cx, c[1]-cy, c[2]-cz
		if d := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz))); d > r {
			r = d
		}
	}
	return r
}

// TestCascadeFitCoversTheRadialSelectionBand pins the coupling between the fit
// and the shader. litWGSL picks a cascade from the straight-line distance to the
// camera, so a corner pixel just inside one split lands in the next cascade. A
// fit that started the next cascade at the split itself would leave that pixel
// outside every box and drop its shadow.
func TestCascadeFitCoversTheRadialSelectionBand(t *testing.T) {
	const aspect = 16.0 / 9.0
	const fov = math.Pi / 3
	cam := engine.RenderCamera{Z: 0, FOV: fov, Near: 0.1, Far: 100}
	cascades := computeCascades(cam, straightDownLight, defaultCascadeLambda, aspect)
	splits := cascadeSplitDistances(0.1, 100, cascadeCount, defaultCascadeLambda)

	tanHalf := float32(math.Tan(fov / 2))
	for i := 0; i+1 < cascadeCount; i++ {
		// A corner pixel whose view-space depth sits just under split i, so the
		// main pass draws it, but whose straight-line distance exceeds split i,
		// so the shader routes it to cascade i+1.
		depth := splits[i+1] * 0.99
		p := [3]float32{tanHalf * depth * aspect, tanHalf * depth, -depth}
		radial := float32(math.Sqrt(float64(p[0]*p[0] + p[1]*p[1] + p[2]*p[2])))
		if radial <= splits[i+1] {
			t.Fatalf("test point is not in the selection band: radial %v, split %v",
				radial, splits[i+1])
		}
		if !insideCascade(cascades.viewProjs[i+1], p) {
			t.Errorf("cascade %d does not cover %v, which the shader routes to it", i+1, p)
		}
	}
}

// TestCascadeCentreSnapsToShadowTexels pins the texel snap. A tight fit follows
// the camera exactly, so without the snap a sub-texel camera step slides the
// whole shadow map and the shadow edges crawl. After the snap a fixed world
// point keeps the same position inside its texel.
func TestCascadeCentreSnapsToShadowTexels(t *testing.T) {
	base := engine.RenderCamera{Y: 6, Z: 0, FOV: math.Pi / 3, Near: 0.1, Far: 60}
	fixed := [3]float32{1.5, 0, -12}

	var fractions []float64
	for step := 0; step < 8; step++ {
		cam := base
		// Steps far smaller than one cascade-0 texel.
		cam.X = float64(step) * 0.0011
		cascades := computeCascades(cam, straightDownLight, defaultCascadeLambda, 1)
		u, _, ok := cascadeUV(cascades.viewProjs[0], fixed)
		if !ok {
			t.Fatalf("step %d: point does not project", step)
		}
		frac := float64(u) * shadowMapSize
		fractions = append(fractions, frac-math.Floor(frac))
	}
	for i := 1; i < len(fractions); i++ {
		d := math.Abs(fractions[i] - fractions[0])
		// Allow a wrap across the texel boundary.
		if d > 0.5 {
			d = 1 - d
		}
		if d > 0.02 {
			t.Errorf("step %d moved the sample %.4f of a texel inside its cell; the snap failed",
				i, d)
		}
	}
}

// TestCascadeFitStaysStableUnderRotation checks the other half of the shimmer
// guard: a bounding sphere has one radius whatever the heading, so turning the
// camera must not resize any cascade box.
func TestCascadeFitStaysStableUnderRotation(t *testing.T) {
	base := engine.RenderCamera{Y: 4, FOV: math.Pi / 3, Near: 0.1, Far: 80}
	reference := computeCascades(base, straightDownLight, defaultCascadeLambda, 1.5)
	for _, angle := range []float64{0.1, 0.7, 1.9, 3.0, -2.2} {
		cam := base
		cam.RotationY = angle
		cascades := computeCascades(cam, straightDownLight, defaultCascadeLambda, 1.5)
		for i := 0; i < cascadeCount; i++ {
			want := cascadeBoxRadius(reference.viewProjs[i])
			got := cascadeBoxRadius(cascades.viewProjs[i])
			if math.Abs(float64(got-want)) > math.Abs(float64(want))*1e-4 {
				t.Errorf("rotation %v resized cascade %d from %v to %v", angle, i, want, got)
			}
		}
	}
}

// TestCameraViewMatrixMatchesComputeMVP keeps the cascade fit and the main pass
// in step. buildCascadeMatrix inverts cameraViewMatrix, so a change to one that
// misses the other would silently fit cascades to the wrong volume.
func TestCameraViewMatrixMatchesComputeMVP(t *testing.T) {
	cam := engine.RenderCamera{
		X: 3, Y: -2, Z: 7,
		RotationX: 0.4, RotationY: -1.1,
		FOV: math.Pi / 3, Near: 0.1, Far: 50,
	}
	want := mat4Mul(mat4Perspective(math.Pi/3, 2, 0.1, 50), cameraViewMatrix(cam))
	got := computeMVP(cam, 200, 100)
	for i := range got {
		if math.Abs(float64(got[i]-want[i])) > 1e-5 {
			t.Fatalf("computeMVP no longer equals proj * cameraViewMatrix:\n got %v\nwant %v", got, want)
		}
	}
}

// TestCameraViewToWorldRotationInvertsTheView proves the transpose really is the
// inverse rotation: carrying a view-space direction to world and back must
// return it unchanged.
func TestCameraViewToWorldRotationInvertsTheView(t *testing.T) {
	cam := engine.RenderCamera{RotationX: 0.6, RotationY: 2.3}
	view := cameraViewMatrix(cam)
	toWorld := cameraViewToWorldRotation(cam)
	for _, v := range [][3]float32{{1, 0, 0}, {0, 1, 0}, {0, 0, -1}, {0.3, -0.8, 0.5}} {
		world := rotateVec3(toWorld, v)
		back := rotateVec3(view, world)
		for i := range back {
			if math.Abs(float64(back[i]-v[i])) > 1e-5 {
				t.Fatalf("round trip of %v returned %v", v, back)
			}
		}
	}
}

// shadowTexelSize returns the world size of one shadow-map texel for cascade i
// of a schedule with the given cascade count. The cascade box is square and
// fitted to the slice bounding sphere, so the texel size is the box width
// divided by the shadow-map resolution.
func shadowTexelSize(cam engine.RenderCamera, aspect float32, count, cascade int) float32 {
	near, far := float32(cam.Near), float32(cam.Far)
	splits := cascadeSplitDistances(near, far, count, defaultCascadeLambda)
	ratio := radialRatio(float32(cam.FOV), aspect)
	sliceNear := splits[cascade] / ratio
	if sliceNear < near {
		sliceNear = near
	}
	r := exactSliceSphereRadius(cam, aspect, sliceNear, splits[cascade+1])
	return 2 * r / float32(shadowMapSize)
}

// TestThreeCascadesBeatTwoOnNearFieldResolution is the measurement behind the
// fixed cascadeCount. Making the count variable costs runtime-sized bind-group
// arrays, a variable shadow texture layer count, and a WGSL constant. The
// question is whether two cascades would serve a typical scene, because two
// would save a whole shadow pass every frame.
//
// They would not. Two cascades stretch the near cascade over the first third of
// the view range instead of the first fifth, which coarsens every shadow near
// the camera by about half again. Shadows are inspected close up, so that is
// the wrong place to spend the saving.
func TestThreeCascadesBeatTwoOnNearFieldResolution(t *testing.T) {
	const aspect = 16.0 / 9.0
	cases := []struct {
		name string
		cam  engine.RenderCamera
		// wantNearGain is the smallest acceptable ratio of the two-cascade near
		// texel to the three-cascade near texel.
		wantNearGain float32
	}{
		{"100 unit scene", engine.RenderCamera{FOV: math.Pi / 3, Near: 0.1, Far: 120}, 1.4},
		{"300 unit scene", engine.RenderCamera{FOV: math.Pi / 3, Near: 0.5, Far: 300}, 1.4},
		{"narrow lens", engine.RenderCamera{FOV: math.Pi / 4, Near: 0.1, Far: 60}, 1.4},
	}
	for _, tc := range cases {
		two := shadowTexelSize(tc.cam, aspect, 2, 0)
		three := shadowTexelSize(tc.cam, aspect, 3, 0)
		if three <= 0 {
			t.Fatalf("%s: three-cascade near texel is %v", tc.name, three)
		}
		gain := two / three
		if gain < tc.wantNearGain {
			t.Errorf("%s: dropping to two cascades coarsens the near texel by only %.2fx "+
				"(%.4f to %.4f); the third cascade may no longer earn its pass",
				tc.name, gain, three, two)
		}
		t.Logf("%s: near texel %.4f with three cascades, %.4f with two (%.2fx coarser)",
			tc.name, three, two, gain)
	}
}

// TestCascadeCountMatchesTheShaderAndTheUniformBlock keeps the fixed count
// honest. cascadeCount drives the shadow texture layer count, the per-cascade
// bind groups, the scene uniform layout, and the branch list inside litWGSL.
// Changing the constant alone would leave the shader reading a cascade the
// renderer never wrote.
func TestCascadeCountMatchesTheShaderAndTheUniformBlock(t *testing.T) {
	if cascadeCount != 3 {
		t.Fatalf("cascadeCount is %d; litWGSL still declares three lightViewProj "+
			"members and pickCascade still branches on three splits", cascadeCount)
	}
	// 4 mat4 (view-projection plus three cascades) + 10 vec4. The ninth vec4 is
	// lightParams, appended when litWGSL gained the scene light array; the
	// tenth is fogParams, appended when litWGSL gained exponential fog.
	if want := 4*64 + 10*16; sceneUniformSize != want {
		t.Fatalf("sceneUniformSize is %d, want %d for %d cascades",
			sceneUniformSize, want, cascadeCount)
	}
	for _, member := range []string{"lightViewProj0", "lightViewProj1", "lightViewProj2"} {
		if !strings.Contains(litWGSL, member) {
			t.Errorf("litWGSL no longer declares %s", member)
		}
	}
	if strings.Contains(litWGSL, "lightViewProj3") {
		t.Error("litWGSL declares a fourth cascade that the renderer never writes")
	}
}
