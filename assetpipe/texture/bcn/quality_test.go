package bcn

import (
	"math"
	"testing"
)

// bc1Ladder lists the search steps in the order the package builds them, so each
// row adds exactly one step to the row above it. A difference between two rows is
// therefore the gain of one step and of nothing else.
var bc1Ladder = []struct {
	name   string
	tuning bc1Tuning
}{
	{"1 bounding box", bc1Tuning{boundingBox: true}},
	{"2 plus least squares", bc1Tuning{boundingBox: true, refineIters: 4}},
	{"3 plus principal axis", bc1Tuning{boundingBox: true, principalAxis: true, refineIters: 4}},
	{"4 plus cluster fit", bc1Tuning{boundingBox: true, principalAxis: true, refineIters: 4, clusterFit: true, clusterKeep: 4}},
	{"5 plus three-colour trial", bc1Tuning{boundingBox: true, principalAxis: true, refineIters: 4, clusterFit: true, clusterKeep: 4, threeColor: true}},
	{"6 plus endpoint polish", bc1Tuning{boundingBox: true, principalAxis: true, refineIters: 4, clusterFit: true, clusterKeep: 4, threeColor: true, polishSweeps: 4}},
}

// TestBC1QualityLadder measures what every search step buys, in decibels.
//
// The measurement runs in stored-code space after the sRGB transfer function,
// because that is the space the GPU samples and the space the encoder minimizes.
// Alpha stays out of the measurement: the opaque variant stores no alpha, so
// including it would add a perfect channel and lift every number.
//
// Every row only adds candidates to the search, and the encoder keeps the best
// candidate by measured error. So no row may score below the row above it, and the
// test asserts that.
//
// The property depends on the polish running on every candidate rather than on the
// winner alone. The polish is a hill climb, and a hill climb that starts from a
// better candidate can still stop at a worse local optimum. An earlier version
// polished the winner alone, and dropping the cluster fit then raised the score of
// one image by 0.4 dB.
func TestBC1QualityLadder(t *testing.T) {
	for _, image := range colourImages() {
		t.Run(image.name, func(t *testing.T) {
			previous := math.Inf(-1)
			for step, entry := range bc1Ladder {
				payload := encodeBC1Tuned(image.surface, image.transfer, false, entry.tuning)
				psnr := psnrAgainstSurface(t, image.surface, image.transfer, FormatBC1RGB, payload, rgbChannels...)
				gain := psnr - previous
				if step == 0 {
					t.Logf("%-26s psnr %6.2f dB", entry.name, psnr)
				} else {
					t.Logf("%-26s psnr %6.2f dB  gain %+5.2f dB", entry.name, psnr, gain)
				}
				if step >= 1 && gain < -0.001 {
					t.Errorf("step %q lost %.3f dB, but it only adds candidates", entry.name, -gain)
				}
				previous = psnr
			}
		})
	}
}

// TestBC1HighNeverLosesToFastPerBlock asserts the strong form of the quality
// claim.
//
// QualityHigh evaluates every candidate QualityFast evaluates and then some, and
// it keeps the candidate with the smallest measured error. So no single block may
// come out worse. A total would hide one block that got worse while another got
// better; a per-block check does not.
func TestBC1HighNeverLosesToFastPerBlock(t *testing.T) {
	for _, image := range colourImages() {
		t.Run(image.name, func(t *testing.T) {
			fast, err := EncodeBC1(image.surface, BC1Options{Transfer: image.transfer, Quality: QualityFast})
			if err != nil {
				t.Fatalf("EncodeBC1 fast: %v", err)
			}
			high, err := EncodeBC1(image.surface, BC1Options{Transfer: image.transfer, Quality: QualityHigh})
			if err != nil {
				t.Fatalf("EncodeBC1 high: %v", err)
			}
			fastSSE := blockSSE(t, image.surface, image.transfer, FormatBC1RGB, fast, rgbChannels...)
			highSSE := blockSSE(t, image.surface, image.transfer, FormatBC1RGB, high, rgbChannels...)
			worse := 0
			for i := range fastSSE {
				if highSSE[i] > fastSSE[i] {
					worse++
					if worse == 1 {
						t.Errorf("block %d: high scored %.1f, fast scored %.1f", i, highSSE[i], fastSSE[i])
					}
				}
			}
			if worse > 0 {
				t.Errorf("%d of %d blocks got worse at QualityHigh", worse, len(fastSSE))
			}
		})
	}
}

// TestBC1RefinementIterations measures where the least-squares refinement stops
// paying, which is how the iteration cap of four was chosen.
func TestBC1RefinementIterations(t *testing.T) {
	for _, image := range colourImages() {
		t.Run(image.name, func(t *testing.T) {
			previous := math.Inf(-1)
			for _, rounds := range []int{0, 1, 2, 3, 4, 6, 8} {
				tuning := bc1Tuning{boundingBox: true, principalAxis: true, refineIters: rounds}
				payload := encodeBC1Tuned(image.surface, image.transfer, false, tuning)
				psnr := psnrAgainstSurface(t, image.surface, image.transfer, FormatBC1RGB, payload, rgbChannels...)
				if math.IsInf(previous, -1) {
					t.Logf("rounds %d  psnr %6.2f dB", rounds, psnr)
				} else {
					t.Logf("rounds %d  psnr %6.2f dB  gain %+6.3f dB", rounds, psnr, psnr-previous)
				}
				if psnr < previous-0.001 {
					t.Errorf("rounds %d lost %.3f dB, but refinement only accepts an improvement",
						rounds, previous-psnr)
				}
				previous = psnr
			}
		})
	}
}

// TestBC1ClusterKeepCount measures what the exact scoring pass of the cluster
// search buys.
//
// The closed form ranks candidates on real endpoints. Quantizing to RGB565 can
// reorder two close candidates, so the search scores the best few exactly. This
// test reports what each extra candidate is worth.
func TestBC1ClusterKeepCount(t *testing.T) {
	for _, image := range colourImages() {
		t.Run(image.name, func(t *testing.T) {
			previous := math.Inf(-1)
			for _, keep := range []int{1, 2, 4, 8} {
				tuning := bc1Tuning{
					boundingBox: true, principalAxis: true, refineIters: 4,
					clusterFit: true, clusterKeep: keep, threeColor: true,
				}
				payload := encodeBC1Tuned(image.surface, image.transfer, false, tuning)
				psnr := psnrAgainstSurface(t, image.surface, image.transfer, FormatBC1RGB, payload, rgbChannels...)
				if math.IsInf(previous, -1) {
					t.Logf("keep %d  psnr %6.2f dB", keep, psnr)
				} else {
					t.Logf("keep %d  psnr %6.2f dB  gain %+6.3f dB", keep, psnr, psnr-previous)
				}
				if psnr < previous-0.001 {
					t.Errorf("keep %d lost %.3f dB, but a longer list only adds candidates", keep, previous-psnr)
				}
				previous = psnr
			}
		})
	}
}

// bc4Ladder lists the BC4 search steps in nesting order.
var bc4Ladder = []struct {
	name   string
	tuning bc4Tuning
}{
	{"1 bounding box, eight-value only", bc4Tuning{}},
	{"2 plus six-value mode", bc4Tuning{sixValue: true}},
	{"3 plus least squares", bc4Tuning{sixValue: true, refineIters: 4}},
	{"4 plus interior start", bc4Tuning{sixValue: true, refineIters: 4, interiorStart: true}},
	{"5 plus endpoint jitter", bc4Tuning{sixValue: true, refineIters: 4, interiorStart: true, jitter: 1, polishSweeps: 1}},
	{"6 plus repeated jitter", bc4Tuning{sixValue: true, refineIters: 4, interiorStart: true, jitter: 1, polishSweeps: 4}},
}

// TestBC4QualityLadder measures what every BC4 search step buys.
//
// The measurement stays in linear code units, because BC4 stores a number a
// shader reads as a number. Applying the sRGB transfer function here would report
// the error of a value nobody displays.
func TestBC4QualityLadder(t *testing.T) {
	for _, image := range dataImages() {
		t.Run(image.name, func(t *testing.T) {
			previous := math.Inf(-1)
			for step, entry := range bc4Ladder {
				payload := encodeBC4Tuned(image.surface, ChannelR, entry.tuning)
				psnr := psnrAgainstSurface(t, image.surface, image.transfer, FormatBC4, payload, ChannelR)
				if step == 0 {
					t.Logf("%-34s psnr %6.2f dB", entry.name, psnr)
				} else {
					t.Logf("%-34s psnr %6.2f dB  gain %+6.3f dB", entry.name, psnr, psnr-previous)
				}
				if psnr < previous-0.001 {
					t.Errorf("step %q lost %.3f dB, but it only adds candidates", entry.name, previous-psnr)
				}
				previous = psnr
			}
		})
	}
}

// TestBC4HighNeverLosesToFastPerBlock repeats the per-block invariant for BC4.
func TestBC4HighNeverLosesToFastPerBlock(t *testing.T) {
	for _, image := range dataImages() {
		t.Run(image.name, func(t *testing.T) {
			fast, err := EncodeBC4(image.surface, BC4Options{Transfer: TransferUnorm, Quality: QualityFast})
			if err != nil {
				t.Fatalf("EncodeBC4 fast: %v", err)
			}
			high, err := EncodeBC4(image.surface, BC4Options{Transfer: TransferUnorm, Quality: QualityHigh})
			if err != nil {
				t.Fatalf("EncodeBC4 high: %v", err)
			}
			fastSSE := blockSSE(t, image.surface, image.transfer, FormatBC4, fast, ChannelR)
			highSSE := blockSSE(t, image.surface, image.transfer, FormatBC4, high, ChannelR)
			for i := range fastSSE {
				if highSSE[i] > fastSSE[i] {
					t.Fatalf("block %d: high scored %.1f, fast scored %.1f", i, highSSE[i], fastSSE[i])
				}
			}
		})
	}
}

// bc1FastGates and bc1HighGates hold the floor every colour image must clear.
//
// The floors sit about 0.3 dB below the measured values of TestBC1QualityLadder.
// The margin covers the small differences a fused multiply-add instruction can
// cause on another processor. It stays far tighter than the loss of the mutations
// in mutation_test.go, which is what makes the gates worth having.
var (
	bc1FastGates = map[string]float64{
		"gradient": 38.4,
		"detail":   24.0,
		"noise":    12.4,
		"bands":    44.2,
	}
	bc1HighGates = map[string]float64{
		"gradient": 38.5,
		"detail":   25.9,
		"noise":    13.4,
		"bands":    45.4,
	}
)

// TestBC4RefinementIterations measures where the BC4 refinement stops paying,
// which is how its iteration cap of four was chosen.
func TestBC4RefinementIterations(t *testing.T) {
	for _, image := range dataImages() {
		t.Run(image.name, func(t *testing.T) {
			previous := math.Inf(-1)
			for _, rounds := range []int{0, 1, 2, 3, 4, 6, 8} {
				tuning := bc4Tuning{sixValue: true, interiorStart: true, refineIters: rounds}
				payload := encodeBC4Tuned(image.surface, ChannelR, tuning)
				psnr := psnrAgainstSurface(t, image.surface, image.transfer, FormatBC4, payload, ChannelR)
				if math.IsInf(previous, -1) {
					t.Logf("rounds %d  psnr %6.2f dB", rounds, psnr)
				} else {
					t.Logf("rounds %d  psnr %6.2f dB  gain %+6.3f dB", rounds, psnr, psnr-previous)
				}
				if psnr < previous-0.001 {
					t.Errorf("rounds %d lost %.3f dB, but refinement only accepts an improvement",
						rounds, previous-psnr)
				}
				previous = psnr
			}
		})
	}
}

// TestQualityGates holds the floor every image must clear.
//
// The numbers come from the measurements above, rounded down by about a third of
// a decibel. They exist so a later change that quietly breaks the search fails
// the build instead of shipping. TestMutationsFailTheGates proves the gates can
// fail.
func TestQualityGates(t *testing.T) {
	gates := map[string]struct{ fast, high float64 }{}
	for name, floor := range bc1FastGates {
		gates[name] = struct{ fast, high float64 }{floor, bc1HighGates[name]}
	}
	for _, image := range colourImages() {
		gate, ok := gates[image.name]
		if !ok {
			t.Fatalf("no gate for image %q", image.name)
		}
		t.Run(image.name, func(t *testing.T) {
			for _, level := range []struct {
				quality Quality
				floor   float64
			}{{QualityFast, gate.fast}, {QualityHigh, gate.high}} {
				payload, err := EncodeBC1(image.surface, BC1Options{
					Transfer: image.transfer, Quality: level.quality,
				})
				if err != nil {
					t.Fatalf("EncodeBC1: %v", err)
				}
				psnr := psnrAgainstSurface(t, image.surface, image.transfer, FormatBC1RGB, payload, rgbChannels...)
				t.Logf("bc1 %-4s psnr %6.2f dB, floor %5.2f dB", level.quality, psnr, level.floor)
				if psnr < level.floor {
					t.Errorf("bc1 %s psnr %.2f dB is below the floor %.2f dB", level.quality, psnr, level.floor)
				}
			}
		})
	}
}

// TestBC4Gates holds the floor for the single-channel encoder.
func TestBC4Gates(t *testing.T) {
	// The floors sit about 0.3 dB below the measured values of
	// TestBC4QualityLadder.
	gates := map[string]struct{ fast, high float64 }{
		"roughness ramp": {51.1, 51.7},
		"cutout mask":    {34.3, 35.3},
		"occlusion":      {36.4, 37.0},
	}
	for _, image := range dataImages() {
		gate, ok := gates[image.name]
		if !ok {
			t.Fatalf("no gate for image %q", image.name)
		}
		t.Run(image.name, func(t *testing.T) {
			for _, level := range []struct {
				quality Quality
				floor   float64
			}{{QualityFast, gate.fast}, {QualityHigh, gate.high}} {
				payload, err := EncodeBC4(image.surface, BC4Options{
					Transfer: TransferUnorm, Quality: level.quality,
				})
				if err != nil {
					t.Fatalf("EncodeBC4: %v", err)
				}
				psnr := psnrAgainstSurface(t, image.surface, image.transfer, FormatBC4, payload, ChannelR)
				t.Logf("bc4 %-4s psnr %6.2f dB, floor %5.2f dB", level.quality, psnr, level.floor)
				if psnr < level.floor {
					t.Errorf("bc4 %s psnr %.2f dB is below the floor %.2f dB", level.quality, psnr, level.floor)
				}
			}
		})
	}
}

// TestBC3Gates measures colour and alpha apart, because the two halves of a BC3
// block are independent and one can hide the other.
func TestBC3Gates(t *testing.T) {
	image := alphaImage(64)
	for _, quality := range []Quality{QualityFast, QualityHigh} {
		payload, err := EncodeBC3(image, BC3Options{Transfer: TransferSRGB, Quality: quality})
		if err != nil {
			t.Fatalf("EncodeBC3: %v", err)
		}
		colour := psnrAgainstSurface(t, image, TransferSRGB, FormatBC3, payload, rgbChannels...)
		alpha := psnrAgainstSurface(t, image, TransferSRGB, FormatBC3, payload, ChannelA)
		t.Logf("bc3 %-4s colour %6.2f dB, alpha %6.2f dB", quality, colour, alpha)
		// The floors sit about 0.3 dB below the measured values. The alpha
		// half scores much higher than the colour half because BC4 spends
		// eight endpoint bits and eight palette levels on one channel, while
		// BC1 spends five or six bits on each of three.
		if colour < 38.0 {
			t.Errorf("bc3 %s colour psnr %.2f dB is below 38.0 dB", quality, colour)
		}
		if alpha < 51.1 {
			t.Errorf("bc3 %s alpha psnr %.2f dB is below 51.1 dB", quality, alpha)
		}
	}
}
