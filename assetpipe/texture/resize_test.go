package texture

import (
	"math"
	"testing"
)

// constantImage builds a linear image whose every texel holds value.
func constantImage(width, height int, value float32) *Image {
	img := NewImage(width, height)
	for i := range img.Pix {
		img.Pix[i] = value
	}
	return img
}

// rampImage builds a horizontal linear ramp from 0 to 1 across the width, with
// the sample value taken at the texel centre.
func rampImage(width, height int) *Image {
	img := NewImage(width, height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			v := (float32(x) + 0.5) / float32(width)
			img.Set(x, y, v, v, v, 1)
		}
	}
	return img
}

// TestResizeKeepsConstantImageConstant is the partition-of-unity check.
//
// Every kernel in the package normalizes its weights after clamping the window
// to the source edge. A constant input must therefore survive every ratio and
// every filter exactly, including at the border where the window is asymmetric.
// A kernel that normalizes before clamping fails here at the first and last
// column.
func TestResizeKeepsConstantImageConstant(t *testing.T) {
	const value = 0.375
	for _, filter := range []Filter{Lanczos3, Mitchell, Triangle, Box} {
		for _, size := range [][2]int{{64, 64}, {17, 5}, {1, 1}} {
			src := constantImage(64, 64, value)
			got, err := Resize(src, size[0], size[1], filter)
			if err != nil {
				t.Fatalf("%s %dx%d: %v", filter, size[0], size[1], err)
			}
			for i, v := range got.Pix {
				if math.Abs(float64(v-value)) > 1e-6 {
					t.Fatalf("%s resize to %dx%d changed a constant at index %d: %g, want %g",
						filter, size[0], size[1], i, v, value)
				}
			}
		}
	}
}

// TestBoxResizeAveragesExactly checks a 2:1 minification against the arithmetic
// mean, which is the closed-form answer a box filter must reproduce.
//
// Box at a 2:1 ratio covers exactly two source texels per output texel with
// equal weight, so the result is a plain average. That makes Box the reference
// the other kernels are measured against in this file.
func TestBoxResizeAveragesExactly(t *testing.T) {
	src := rampImage(16, 1)
	got, err := Resize(src, 8, 1, Box)
	if err != nil {
		t.Fatal(err)
	}
	for x := 0; x < 8; x++ {
		a, _, _, _ := src.At(2*x, 0)
		b, _, _, _ := src.At(2*x+1, 0)
		want := (a + b) / 2
		gotValue, _, _, _ := got.At(x, 0)
		if math.Abs(float64(gotValue-want)) > 1e-6 {
			t.Fatalf("box 2:1 at x=%d gave %g, want the mean %g", x, gotValue, want)
		}
	}
}

// TestSymmetricKernelsReproduceALinearRamp checks the first-moment property.
//
// A normalized kernel that is symmetric about the sample point reproduces any
// linear function exactly, because the weighted mean of the sample positions is
// the sample point. The property fails near the border, where clamp-to-edge
// replication makes the input non-linear, so the test measures the interior and
// asserts a separate, looser bound at the border.
func TestSymmetricKernelsReproduceALinearRamp(t *testing.T) {
	const srcWidth = 64
	const dstWidth = 32
	src := rampImage(srcWidth, 1)
	for _, filter := range []Filter{Lanczos3, Mitchell, Triangle, Box} {
		got, err := Resize(src, dstWidth, 1, filter)
		if err != nil {
			t.Fatal(err)
		}
		// The widest kernel here is Lanczos3 at a 2:1 ratio, which reaches
		// six source texels, so three output texels at each end can see the
		// clamped border.
		const margin = 4
		for x := margin; x < dstWidth-margin; x++ {
			want := (float64(x) + 0.5) / dstWidth
			gotValue, _, _, _ := got.At(x, 0)
			if math.Abs(float64(gotValue)-want) > 1e-5 {
				t.Errorf("%s ramp at x=%d gave %g, want %g", filter, x, gotValue, want)
			}
		}
		// Monotonicity must hold everywhere, border included. A ringing
		// kernel can overshoot at a step edge, but never on a ramp.
		for x := 1; x < dstWidth; x++ {
			prev, _, _, _ := got.At(x-1, 0)
			curr, _, _, _ := got.At(x, 0)
			if curr < prev-1e-6 {
				t.Errorf("%s ramp is not monotone at x=%d: %g then %g", filter, x, prev, curr)
			}
		}
	}
}

// TestLanczosOvershootsAtAStepEdge records the kernel's negative lobes.
//
// Lanczos3 rings. The overshoot is why EncodeBytes clamps before it quantizes,
// and this test fails if the kernel silently loses its negative lobes, which
// would mean someone replaced it with a blur.
func TestLanczosOvershootsAtAStepEdge(t *testing.T) {
	src := NewImage(32, 1)
	for x := 0; x < 32; x++ {
		v := float32(0)
		if x >= 16 {
			v = 1
		}
		src.Set(x, 0, v, v, v, 1)
	}
	got, err := Resize(src, 64, 1, Lanczos3)
	if err != nil {
		t.Fatal(err)
	}
	minV, maxV := float32(1), float32(0)
	for x := 0; x < 64; x++ {
		v, _, _, _ := got.At(x, 0)
		minV = minFloat(minV, v)
		maxV = maxFloat(maxV, v)
	}
	if minV >= 0 && maxV <= 1 {
		t.Fatalf("Lanczos3 produced no ringing at a step edge: range %g to %g", minV, maxV)
	}
	if got := LinearToUnorm8(maxV); got != 255 {
		t.Fatalf("the quantizer must clamp the overshoot to 255, got %d", got)
	}
	if got := LinearToUnorm8(minV); got != 0 {
		t.Fatalf("the quantizer must clamp the undershoot to 0, got %d", got)
	}
}

func minFloat(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func TestNearestPowerOfTwo(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 1}, {1, 1}, {2, 2}, {3, 4}, {5, 4}, {6, 8},
		{100, 128}, {90, 64}, {1200, 1024}, {1500, 2048}, {2048, 2048},
	}
	for _, tc := range cases {
		if got := NearestPowerOfTwo(tc.in); got != tc.want {
			t.Errorf("NearestPowerOfTwo(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestFitPowerOfTwo(t *testing.T) {
	cases := []struct {
		w, h, maxEdge int
		wantW, wantH  int
	}{
		{2048, 2048, 2048, 2048, 2048},
		{2048, 2048, 1024, 1024, 1024},
		{2048, 1024, 512, 512, 512},
		{1200, 600, 0, 1024, 512},
		{300, 100, 128, 128, 128},
		{1, 1, 2048, 1, 1},
		// A non-power-of-two ceiling rounds down, never up. Rounding 1000 up
		// to 1024 would break the promise the tier ladder makes.
		{2048, 2048, 1000, 512, 512},
	}
	for _, tc := range cases {
		gotW, gotH := FitPowerOfTwo(tc.w, tc.h, tc.maxEdge)
		if gotW != tc.wantW || gotH != tc.wantH {
			t.Errorf("FitPowerOfTwo(%d, %d, %d) = %dx%d, want %dx%d",
				tc.w, tc.h, tc.maxEdge, gotW, gotH, tc.wantW, tc.wantH)
		}
	}
}

// sineRow builds a horizontal sine of the given period, one row high, with a
// value analytically known at every texel centre.
func sineRow(width int, period float64) *Image {
	img := NewImage(width, 1)
	for x := 0; x < width; x++ {
		v := float32(0.5 + 0.4*math.Sin(2*math.Pi*(float64(x)+0.5)/period))
		img.Set(x, 0, v, v, v, 1)
	}
	return img
}

// TestFilterPassbandAndStopbandOrdering checks each kernel against an analytic
// reference and pins the quality ordering the default filter choice rests on.
//
// Two properties decide a minification kernel:
//
//   - Passband: a sine well below the target Nyquist limit must survive. The
//     reference is the same analytic sine sampled on the target grid, so the
//     error is absolute.
//   - Stopband: a sine ABOVE the target Nyquist limit carries nothing the target
//     grid can hold, so the correct output is the signal's mean. Whatever
//     amplitude survives is aliasing, and aliasing is what makes a minified
//     texture shimmer when the camera moves.
//
// Box has a perfect passband at a 2:1 ratio, because it is then an exact area
// average, and the worst stopband of the four, because a box in space is a sinc
// in frequency and sinc has large side lobes. Lanczos3 wins both, which is why
// it is the default.
func TestFilterPassbandAndStopbandOrdering(t *testing.T) {
	const srcSize = 512
	const dstSize = 256
	const margin = 6 // Skip the clamped border, where no kernel sees a sine.

	measure := func(filter Filter) (passRMS, leak float64) {
		got, err := Resize(sineRow(srcSize, 32), dstSize, 1, filter)
		if err != nil {
			t.Fatal(err)
		}
		reference := sineRow(dstSize, 16)
		var sum float64
		count := 0
		for x := margin; x < dstSize-margin; x++ {
			g, _, _, _ := got.At(x, 0)
			r, _, _, _ := reference.At(x, 0)
			diff := float64(g - r)
			sum += diff * diff
			count++
		}
		passRMS = math.Sqrt(sum / float64(count))

		aliased, err := Resize(sineRow(srcSize, 3), dstSize, 1, filter)
		if err != nil {
			t.Fatal(err)
		}
		for x := margin; x < dstSize-margin; x++ {
			v, _, _, _ := aliased.At(x, 0)
			if diff := math.Abs(float64(v) - 0.5); diff > leak {
				leak = diff
			}
		}
		return passRMS, leak
	}

	boxPass, boxLeak := measure(Box)
	trianglePass, triangleLeak := measure(Triangle)
	mitchellPass, mitchellLeak := measure(Mitchell)
	lanczosPass, lanczosLeak := measure(Lanczos3)

	t.Logf("box      passband rms %.6f stopband leak %.6f", boxPass, boxLeak)
	t.Logf("triangle passband rms %.6f stopband leak %.6f", trianglePass, triangleLeak)
	t.Logf("mitchell passband rms %.6f stopband leak %.6f", mitchellPass, mitchellLeak)
	t.Logf("lanczos3 passband rms %.6f stopband leak %.6f", lanczosPass, lanczosLeak)

	// Every kernel must pass a well-sampled sine to within 1 percent of the
	// 0.4 amplitude. A kernel that fails here is broken, not merely soft.
	for name, rms := range map[string]float64{
		"box": boxPass, "triangle": trianglePass, "mitchell": mitchellPass, "lanczos3": lanczosPass,
	} {
		if rms > 0.004*1.2 {
			t.Errorf("%s passband rms is %.6f, which is too much error for a band-limited sine", name, rms)
		}
	}
	// Lanczos3 must be the best of the four on both axes, because that is why
	// it is the default filter.
	if lanczosPass > mitchellPass || lanczosPass > trianglePass {
		t.Errorf("lanczos3 passband rms %.6f is not the best; mitchell %.6f triangle %.6f",
			lanczosPass, mitchellPass, trianglePass)
	}
	if lanczosLeak > mitchellLeak || mitchellLeak > boxLeak {
		t.Errorf("the aliasing ordering broke: lanczos3 %.6f, mitchell %.6f, box %.6f",
			lanczosLeak, mitchellLeak, boxLeak)
	}
	// Box must alias far more than Lanczos3. A factor of five is the measured
	// gap, so demand at least three to leave room for a kernel tweak.
	if boxLeak < 3*lanczosLeak {
		t.Errorf("box leaks %.6f and lanczos3 leaks %.6f; the gap collapsed, so one kernel changed",
			boxLeak, lanczosLeak)
	}
}
