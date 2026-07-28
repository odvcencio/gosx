package bc7

import (
	"math"
	"math/rand"
	"testing"
)

// This file holds the encoder assertions. The reports in report_test.go print
// the same numbers without asserting, so a number that moves fails here and is
// visible there.

// TestInternalErrorMatchesMeasuredError is the strongest self-check in the
// package.
//
// The encoder compares modes by the error its own fit accounting computed. That
// number only means something if it equals the error the decoder will produce. So
// this test encodes random blocks, packs them, decodes them, and demands the two
// numbers agree exactly.
//
// Exactly, not approximately: every term is a squared integer difference below
// 65026 summed over 64 samples, so every partial sum is an integer that float64
// represents without loss. A mismatch means the fit accounting, the packer or the
// decoder disagree, and any of the three would ship a plausible image.
func TestInternalErrorMatchesMeasuredError(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	cfg, err := resolve(Options{Space: SRGB, Quality: QualityBest})
	if err != nil {
		t.Fatal(err)
	}
	sc := &scratch{}
	for trial := 0; trial < 600; trial++ {
		fillRandomBlock(rng, sc, trial)
		cand := encodeBlock(sc, &cfg)
		if math.IsInf(cand.err, 1) {
			t.Fatalf("trial %d: no mode produced a candidate", trial)
		}
		block := packBlock(&cand.spec)
		got := DecodeBlock(block[:])
		measured := 0.0
		for texel := 0; texel < 16; texel++ {
			for c := 0; c < 4; c++ {
				d := float64(int(got[texel][c]) - int(sc.block[texel][c]))
				measured += d * d
			}
		}
		if measured != cand.err {
			t.Fatalf("trial %d mode %d: the encoder counted %v, the decoder shows %v",
				trial, cand.spec.mode, cand.err, measured)
		}
	}
}

// fillRandomBlock builds one block with a shape that stresses a different part of
// the format on each trial.
func fillRandomBlock(rng *rand.Rand, sc *scratch, trial int) {
	switch trial % 6 {
	case 0: // solid
		var px [4]int32
		for c := 0; c < 4; c++ {
			px[c] = int32(rng.Intn(256))
		}
		for t := 0; t < 16; t++ {
			sc.block[t] = px
		}
	case 1: // two clusters
		var a, b [4]int32
		for c := 0; c < 4; c++ {
			a[c] = int32(rng.Intn(256))
			b[c] = int32(rng.Intn(256))
		}
		for t := 0; t < 16; t++ {
			if rng.Intn(2) == 0 {
				sc.block[t] = a
			} else {
				sc.block[t] = b
			}
		}
	case 2: // three clusters
		var trio [3][4]int32
		for i := range trio {
			for c := 0; c < 4; c++ {
				trio[i][c] = int32(rng.Intn(256))
			}
		}
		for t := 0; t < 16; t++ {
			sc.block[t] = trio[rng.Intn(3)]
		}
	case 3: // gradient
		for t := 0; t < 16; t++ {
			for c := 0; c < 3; c++ {
				sc.block[t][c] = int32(t * 17)
			}
			sc.block[t][3] = 255
		}
	case 4: // alpha that does not track colour
		for t := 0; t < 16; t++ {
			sc.block[t][0] = int32(t % 4 * 85)
			sc.block[t][1] = int32(255 - t%4*85)
			sc.block[t][2] = 128
			sc.block[t][3] = int32(t / 4 * 85)
		}
	default: // noise
		for t := 0; t < 16; t++ {
			for c := 0; c < 4; c++ {
				sc.block[t][c] = int32(rng.Intn(256))
			}
		}
	}
}

// TestAnchorSwapIsRequired is the mutation test for the anchor rule.
//
// skipAnchorFix removes the endpoint swap that keeps an anchor index inside its
// shortened field. The packer then drops the anchor's high bit, so the decoder
// reads a different index for one texel per subset. The image still looks right,
// which is exactly why this needs a test rather than an eyeball.
//
// The test demands two things. First, the broken encoder must lose quality on
// every image where the swap fires. Second, and more precisely, the broken
// encoder's own error accounting must disagree with what the decoder produces,
// because the accounting believes the index it chose.
func TestAnchorSwapIsRequired(t *testing.T) {
	images := buildTestImages()
	broke := 0
	for _, img := range images {
		good := baseConfig(t, img.space, QualityBalanced)
		bad := good
		bad.skipAnchorFix = true

		goodPSNR, _ := measure(img.src, img.space, good)
		badPSNR, _ := measure(img.src, img.space, bad)
		if badPSNR < goodPSNR {
			broke++
		}
		if badPSNR > goodPSNR {
			t.Errorf("%s: skipping the anchor swap improved the result, %v then %v",
				img.name, goodPSNR, badPSNR)
		}
	}
	if broke == 0 {
		t.Fatal("skipping the anchor swap changed nothing, so the test proves nothing")
	}
	t.Logf("skipping the anchor swap lowered quality on %d of %d images", broke, len(images))

	// Now the precise part. Find one block whose accounting the broken encoder
	// gets wrong, and name the texel.
	rng := rand.New(rand.NewSource(3))
	cfg, err := resolve(Options{Space: SRGB, Quality: QualityBalanced})
	if err != nil {
		t.Fatal(err)
	}
	cfg.skipAnchorFix = true
	sc := &scratch{}
	mismatches := 0
	for trial := 0; trial < 400 && mismatches == 0; trial++ {
		fillRandomBlock(rng, sc, trial)
		cand := encodeBlock(sc, &cfg)
		block := packBlock(&cand.spec)
		got := DecodeBlock(block[:])
		measured := 0.0
		for texel := 0; texel < 16; texel++ {
			for c := 0; c < 4; c++ {
				d := float64(int(got[texel][c]) - int(sc.block[texel][c]))
				measured += d * d
			}
		}
		if measured != cand.err {
			mismatches++
		}
	}
	if mismatches == 0 {
		t.Fatal("the broken encoder never mis-counted, so the anchor path is untested")
	}
}

// TestAnchorIndexAlwaysFitsItsField checks the invariant directly on the output.
//
// Every anchor index the encoder writes must sit below half the index range.
// Reading the indices back out of the packed block is the only way to see it, so
// the test decodes the fields and checks each one.
func TestAnchorIndexAlwaysFitsItsField(t *testing.T) {
	rng := rand.New(rand.NewSource(1234))
	cfg, err := resolve(Options{Space: SRGB, Quality: QualityBest})
	if err != nil {
		t.Fatal(err)
	}
	sc := &scratch{}
	for trial := 0; trial < 400; trial++ {
		fillRandomBlock(rng, sc, trial)
		cand := encodeBlock(sc, &cfg)
		m := modes[cand.spec.mode]
		for sub := 0; sub < m.subsets; sub++ {
			anchor := anchorTexel(m.subsets, cand.spec.partition, sub)
			limit := uint8(1) << uint(m.indexBits-1)
			if cand.spec.idx[anchor] >= limit {
				t.Fatalf("trial %d mode %d subset %d: anchor index %d needs %d bits, the field holds %d",
					trial, cand.spec.mode, sub, cand.spec.idx[anchor], m.indexBits, m.indexBits-1)
			}
		}
		if m.indexBits2 > 0 && cand.spec.idx2[0] >= uint8(1)<<uint(m.indexBits2-1) {
			t.Fatalf("trial %d mode %d: second index set anchor %d overflows its field",
				trial, cand.spec.mode, cand.spec.idx2[0])
		}
	}
}

// TestMorePartitionsNeverHurt pins a correctness invariant of the search.
//
// The encoder keeps the lowest measured error, and a larger partition budget only
// adds candidates. So raising the budget can never raise the error. The invariant
// caught a real fault: the seed cache keyed a partition without naming its subset
// count, so a three-subset mode read a two-subset mode's principal axis and a
// larger budget produced a worse image.
//
// The test also covers the bound pruning across modes, which is the other way a
// search can lose a candidate it should have kept.
func TestMorePartitionsNeverHurt(t *testing.T) {
	images := buildTestImages()
	masks := []ModeMask{Mode0, Mode1, Mode2, Mode3, Mode7, ModesAll}
	budgets := []int{1, 2, 3, 4, 8, 16, 64}
	for _, img := range images {
		for _, mask := range masks {
			prev := math.Inf(1)
			for _, budget := range budgets {
				cfg := baseConfig(t, img.space, QualityBest)
				cfg.modes = mask
				cfg.partitions = budget
				_, stats := measure(img.src, img.space, cfg)
				if stats.SquaredError > prev {
					t.Errorf("%s modes %#x: budget %d gives error %.0f, a smaller budget gave %.0f",
						img.name, mask, budget, stats.SquaredError, prev)
				}
				prev = stats.SquaredError
			}
		}
	}
}

// TestPrincipalAxisBeatsBoundingBox measures what the endpoint work bought.
//
// The bounding-box encoder is the reference: per-channel minimum and maximum, no
// refinement, one partition. The test names the images where the difference is
// large, so a regression that quietly reverts to a bounding box fails.
func TestPrincipalAxisBeatsBoundingBox(t *testing.T) {
	// Each entry is the gain the full encoder must reach over the bounding-box
	// reference, in decibels. The floors sit below the measured gains with room
	// for a tie-break change, and above zero, so a silent revert fails.
	//
	// The gradient image is absent on purpose. Its block colours run along two
	// channel axes, and mode 5 with a channel rotation splits those two axes into
	// two index sets. After the split each set holds one channel, and a
	// one-channel bounding box is the principal axis. So the reference already
	// reaches the optimum there and the comparison would prove nothing.
	want := map[string]float64{
		"hardEdge":          20.0,
		"threeRegions":      15.0,
		"alphaUncorrelated": 12.0,
		"opaque":            1.5,
		"normalMap":         3.0,
	}
	for _, img := range buildTestImages() {
		floor, ok := want[img.name]
		if !ok {
			continue
		}
		bbox := baseConfig(t, img.space, QualityBalanced)
		bbox.seed = seedBoundingBox
		bbox.rounds = 0
		bbox.partitions = 1
		base, _ := measure(img.src, img.space, bbox)
		full, _ := measure(img.src, img.space, baseConfig(t, img.space, QualityBest))
		gain := full - base
		if math.IsInf(full, 1) {
			gain = math.Inf(1)
		}
		if gain < floor {
			t.Errorf("%s: the encoder beats a bounding box by %.2f dB, want at least %.2f",
				img.name, gain, floor)
		}
		t.Logf("%-18s bounding box %s, best %s, gain %.2f dB",
			img.name, psnrText(base), psnrText(full), gain)
	}
}

// TestRefinementPays measures the least squares refinement.
//
// Refinement must lower the error on an image with structure inside the block. A
// zero gain would mean the alternating solve is not running.
func TestRefinementPays(t *testing.T) {
	gained := 0
	for _, img := range buildTestImages() {
		none := baseConfig(t, img.space, QualityBest)
		none.rounds = 0
		low, _ := measure(img.src, img.space, none)
		high, _ := measure(img.src, img.space, baseConfig(t, img.space, QualityBest))
		if high < low-1e-9 {
			t.Errorf("%s: refinement lowered quality, %v then %v", img.name, low, high)
		}
		if high > low+0.1 {
			gained++
		}
	}
	if gained == 0 {
		t.Fatal("refinement changed nothing on any image, so it is not running")
	}
}

// TestMultiSubsetModesPayForThreeRegions checks that the partition machinery is
// worth its code.
//
// The three-region image holds three colour clusters with a ramp inside each one,
// so no two clusters fit one line. A single-subset encoder has to lose. The test
// demands a large gain, which fails if the partition search or the partition
// tables break.
func TestMultiSubsetModesPayForThreeRegions(t *testing.T) {
	var img testCase
	for _, c := range buildTestImages() {
		if c.name == "threeRegions" {
			img = c
		}
	}
	if img.src.Pix == nil {
		t.Fatal("the three-region image is missing")
	}
	only6 := baseConfig(t, img.space, QualityBest)
	only6.modes = Mode6
	single, _ := measure(img.src, img.space, only6)
	all, stats := measure(img.src, img.space, baseConfig(t, img.space, QualityBest))
	if all < single+3 {
		t.Errorf("multi-subset modes gained only %.2f dB over mode 6 alone, want at least 3",
			all-single)
	}
	threeSubset := stats.ModeCounts[0] + stats.ModeCounts[2]
	if threeSubset == 0 {
		t.Error("no block picked a three-subset mode on an image built from three clusters")
	}
	t.Logf("mode 6 alone %s, every mode %s, three-subset blocks %d of %d",
		psnrText(single), psnrText(all), threeSubset, stats.Blocks)
}

// TestSplitAlphaModesPayWhenAlphaIsIndependent checks modes 4 and 5.
//
// When alpha runs along one axis and colour along another, a joint RGBA fit has to
// compromise. A mode that gives alpha its own endpoint pair and its own index set
// does not. The test forbids mode 4 and mode 5 and demands a measurable loss.
func TestSplitAlphaModesPayWhenAlphaIsIndependent(t *testing.T) {
	var img testCase
	for _, c := range buildTestImages() {
		if c.name == "alphaUncorrelated" {
			img = c
		}
	}
	if img.src.Pix == nil {
		t.Fatal("the uncorrelated-alpha image is missing")
	}
	without := baseConfig(t, img.space, QualityBest)
	without.modes = ModesAll &^ (Mode4 | Mode5)
	low, _ := measure(img.src, img.space, without)
	high, stats := measure(img.src, img.space, baseConfig(t, img.space, QualityBest))
	if high < low+5 {
		t.Errorf("the split-alpha modes gained only %.2f dB, want at least 5", high-low)
	}
	if stats.ModeCounts[4]+stats.ModeCounts[5] == 0 {
		t.Error("no block picked mode 4 or mode 5 on an image whose alpha ignores colour")
	}
	t.Logf("without modes 4 and 5 %s, with them %s", psnrText(low), psnrText(high))
}

// TestQualityLevelsRank proves the levels are ordered.
//
// Best must never lose to Balanced, and Balanced must never lose to Fast. Nothing
// enforces that automatically, because the levels differ in several dimensions at
// once.
func TestQualityLevelsRank(t *testing.T) {
	for _, img := range buildTestImages() {
		fast, _ := measure(img.src, img.space, baseConfig(t, img.space, QualityFast))
		balanced, _ := measure(img.src, img.space, baseConfig(t, img.space, QualityBalanced))
		best, _ := measure(img.src, img.space, baseConfig(t, img.space, QualityBest))
		if balanced < fast-1e-9 {
			t.Errorf("%s: balanced %v is worse than fast %v", img.name, balanced, fast)
		}
		if best < balanced-1e-9 {
			t.Errorf("%s: best %v is worse than balanced %v", img.name, best, balanced)
		}
	}
}

// TestSolidBlockStaysWithinOneCode covers the easy case the eye notices most.
//
// A flat colour must not drift. It cannot always be exact, and the reason is worth
// recording, because it looks like an encoder fault and is not.
//
// Every mode that stores alpha pins the low bit of an endpoint through one parity
// bit shared by all four channels of that endpoint. Mode 6, the strongest of them,
// stores 7 colour bits plus one such parity bit. So endpoint 0 can only hold codes
// whose low bits all agree. A solid colour like (1, 0, 254, 1) mixes odd and even
// codes, and no single endpoint reaches it. The palette can only bracket it, and
// the best index leaves one code of error on two channels.
//
// The test therefore asserts two things: a colour whose four codes share a parity
// must be exact, and any solid colour must land within one code per channel.
func TestSolidBlockStaysWithinOneCode(t *testing.T) {
	check := func(px [4]uint8, exact bool) {
		t.Helper()
		src := codeImage(8, 8, SRGB, func(x, y int) [4]uint8 { return px })
		data, stats, err := EncodeWithStats(src, Options{Space: SRGB, Quality: QualityFast})
		if err != nil {
			t.Fatal(err)
		}
		got := DecodeBlock(data)
		for texel := 0; texel < 16; texel++ {
			if got[texel] != got[0] {
				t.Errorf("%v: texel %d is %v but texel 0 is %v, so a flat colour did not stay flat",
					px, texel, got[texel], got[0])
			}
			for c := 0; c < 4; c++ {
				d := int(got[texel][c]) - int(px[c])
				if d < -1 || d > 1 {
					t.Errorf("%v: channel %d came back as %d, which is %d codes off",
						px, c, got[texel][c], d)
				}
			}
		}
		if exact && stats.SquaredError != 0 {
			t.Errorf("%v: every code shares a parity, so the block must be exact, lost %v",
				px, stats.SquaredError)
		}
	}
	// Same-parity colours must be exact.
	check([4]uint8{0, 0, 0, 0}, true)
	check([4]uint8{200, 100, 56, 254}, true)
	check([4]uint8{255, 101, 55, 1}, true)
	// Mixed-parity colours only have to stay within one code.
	check([4]uint8{1, 0, 254, 1}, false)
	check([4]uint8{17, 8, 238, 17}, false)
	check([4]uint8{128, 64, 127, 128}, false)
}

// TestFullyTransparentAndFullyOpaqueBlocks covers the two alpha extremes.
//
// Both are common: a cut-out sprite is mostly one or the other. Alpha 0 and alpha
// 255 must come back exactly, because a wrong alpha shows as a hard edge artefact
// while a wrong colour under it shows as nothing.
func TestFullyTransparentAndFullyOpaqueBlocks(t *testing.T) {
	for _, alpha := range []uint8{0, 255} {
		src := codeImage(8, 8, SRGB, func(x, y int) [4]uint8 {
			return [4]uint8{uint8(x * 30), 90, 200, alpha}
		})
		data, err := Encode(src, Options{Space: SRGB, Quality: QualityBalanced})
		if err != nil {
			t.Fatal(err)
		}
		bx, by := BlocksFor(8, 8)
		sc := &scratch{}
		alphaError := 0
		for row := 0; row < by; row++ {
			for col := 0; col < bx; col++ {
				loadBlock(sc, &src, SRGB, col, row)
				got := DecodeBlock(data[(row*bx+col)*BlockBytes:])
				for texel := 0; texel < 16; texel++ {
					d := int(got[texel][3]) - int(sc.block[texel][3])
					alphaError += d * d
				}
			}
		}
		if alphaError != 0 {
			t.Errorf("alpha %d: a constant alpha lost %d squared error", alpha, alphaError)
		}
	}
}

// TestSRGBAndLinearDiffer is the guard against the transfer function mistake.
//
// The same linear pixels must encode to different bytes under the two colour
// spaces, and each must decode back through its own curve. If the two agreed, the
// package would be applying one curve for both, and every colour texture would
// ship dark.
func TestSRGBAndLinearDiffer(t *testing.T) {
	src := Source{Width: 4, Height: 4, Pix: make([]float32, 64)}
	for i := 0; i < 16; i++ {
		// 0.2 linear is far from 0.2 sRGB, so the two paths cannot coincide.
		src.Pix[i*4] = 0.2
		src.Pix[i*4+1] = 0.5
		src.Pix[i*4+2] = 0.8
		src.Pix[i*4+3] = 1
	}
	asSRGB, err := Encode(src, Options{Space: SRGB})
	if err != nil {
		t.Fatal(err)
	}
	asLinear, err := Encode(src, Options{Space: Linear})
	if err != nil {
		t.Fatal(err)
	}
	if string(asSRGB) == string(asLinear) {
		t.Fatal("the sRGB and Linear paths produced the same bytes, so one curve is missing")
	}

	// The sRGB codes must be the brighter ones, because the curve lifts the
	// dark end. Decoding each with its own space must return the input.
	srgbCode := DecodeBlock(asSRGB)[0]
	linearCode := DecodeBlock(asLinear)[0]
	if srgbCode[0] <= linearCode[0] {
		t.Errorf("sRGB code %d is not above the unorm code %d for linear 0.2",
			srgbCode[0], linearCode[0])
	}
	for _, c := range []struct {
		space ColorSpace
		data  []byte
	}{{SRGB, asSRGB}, {Linear, asLinear}} {
		back, err := Decode(c.data, 4, 4, c.space)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			if math.Abs(float64(back.Pix[i]-src.Pix[i])) > 0.01 {
				t.Errorf("%v channel %d: round trip gave %v, want %v",
					c.space, i, back.Pix[i], src.Pix[i])
			}
		}
	}
}

// TestEncodePadsWithTheEdgeTexel covers a size that is not a multiple of four.
func TestEncodePadsWithTheEdgeTexel(t *testing.T) {
	src := codeImage(5, 3, SRGB, func(x, y int) [4]uint8 {
		return [4]uint8{uint8(x * 50), uint8(y * 80), 30, 255}
	})
	data, err := Encode(src, Options{Space: SRGB, Quality: QualityBest})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != EncodedSize(5, 3) {
		t.Fatalf("got %d bytes, want %d", len(data), EncodedSize(5, 3))
	}
	if len(data) != 2*1*BlockBytes {
		t.Fatalf("a 5 by 3 image needs 2 by 1 blocks, got %d bytes", len(data))
	}
	back, err := Decode(data, 5, 3, SRGB)
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 3; y++ {
		for x := 0; x < 5; x++ {
			i := (y*5 + x) * 4
			for c := 0; c < 3; c++ {
				if math.Abs(float64(back.Pix[i+c]-src.Pix[i+c])) > 0.02 {
					t.Errorf("pixel %d,%d channel %d: got %v, want %v",
						x, y, c, back.Pix[i+c], src.Pix[i+c])
				}
			}
		}
	}
}

// TestParallelMatchesSingle proves the goroutine split changes nothing.
func TestParallelMatchesSingle(t *testing.T) {
	src := photoLike(64, 48)
	single, err := Encode(src, Options{Space: SRGB, Quality: QualityBalanced, Parallel: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, workers := range []int{2, 3, 8, 0} {
		many, err := Encode(src, Options{Space: SRGB, Quality: QualityBalanced, Parallel: workers})
		if err != nil {
			t.Fatal(err)
		}
		if string(single) != string(many) {
			t.Fatalf("Parallel %d produced different bytes from Parallel 1", workers)
		}
	}
}

// TestEncodeIsDeterministic proves two runs of the same input agree.
func TestEncodeIsDeterministic(t *testing.T) {
	src := photoLike(32, 32)
	opts := Options{Space: Linear, Quality: QualityBest}
	first, err := Encode(src, opts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Encode(src, opts)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("two encodes of the same image differ")
	}
}

// TestOptionsValidation covers the refusals.
func TestOptionsValidation(t *testing.T) {
	src := codeImage(4, 4, SRGB, func(x, y int) [4]uint8 { return [4]uint8{1, 2, 3, 255} })
	cases := []struct {
		name string
		opts Options
		src  Source
		want error
	}{
		{"missing colour space", Options{}, src, ErrColorSpace},
		{"rate-distortion asked for", Options{Space: SRGB, RDO: RDOOptions{Lambda: 0.5}}, src, ErrRDOUnsupported},
		{"empty mode mask", Options{Space: SRGB, Modes: ModeMask(1) << 12}, src, ErrNoModes},
		{"zero size", Options{Space: SRGB}, Source{Width: 0, Height: 4, Pix: src.Pix}, ErrShape},
		{"short pixel slice", Options{Space: SRGB}, Source{Width: 4, Height: 4, Pix: src.Pix[:8]}, ErrShape},
	}
	for _, c := range cases {
		_, err := Encode(c.src, c.opts)
		if err == nil {
			t.Errorf("%s: Encode returned no error", c.name)
			continue
		}
		if !errorsIs(err, c.want) {
			t.Errorf("%s: Encode returned %v, want %v", c.name, err, c.want)
		}
	}
	if _, err := Decode(nil, 4, 4, spaceUnset); err != ErrColorSpace {
		t.Errorf("Decode with no colour space returned %v", err)
	}
	if _, err := Decode(make([]byte, 4), 4, 4, SRGB); err == nil {
		t.Error("Decode accepted a short payload")
	}
}

func errorsIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

// TestForcedSingleModeStillEncodes proves every mode works alone.
//
// The integration layer may want one mode for a reason of its own, and the
// per-mode hand vectors only cover decoding. This covers encoding.
func TestForcedSingleModeStillEncodes(t *testing.T) {
	src := photoLike(32, 32)
	for mode := 0; mode < 8; mode++ {
		mask := ModeMask(1) << uint(mode)
		data, stats, err := EncodeWithStats(src, Options{Space: SRGB, Modes: mask, Quality: QualityBest})
		if err != nil {
			t.Fatalf("mode %d: %v", mode, err)
		}
		if stats.ModeCounts[mode] != stats.Blocks {
			t.Errorf("mode %d: only %d of %d blocks used it", mode, stats.ModeCounts[mode], stats.Blocks)
		}
		for block := 0; block < stats.Blocks; block++ {
			if got := BlockMode(data[block*BlockBytes:]); got != mode {
				t.Fatalf("mode %d: block %d reads as mode %d", mode, block, got)
			}
		}
		if math.IsInf(stats.PSNR(), -1) {
			t.Errorf("mode %d: the encode produced no signal", mode)
		}
	}
}

// TestChannelWeightsShiftTheError proves the weights reach the fit.
//
// Weighting green far above the rest must lower the green error and raise another
// channel's error. A weight that only sat in the struct would change nothing.
func TestChannelWeightsShiftTheError(t *testing.T) {
	src := photoLike(32, 32)
	perChannel := func(weights [4]float64) [4]float64 {
		data, err := Encode(src, Options{Space: SRGB, Quality: QualityBalanced, ChannelWeights: weights})
		if err != nil {
			t.Fatal(err)
		}
		var sums [4]float64
		bx, by := BlocksFor(src.Width, src.Height)
		sc := &scratch{}
		for row := 0; row < by; row++ {
			for col := 0; col < bx; col++ {
				loadBlock(sc, &src, SRGB, col, row)
				got := DecodeBlock(data[(row*bx+col)*BlockBytes:])
				for t := 0; t < 16; t++ {
					for c := 0; c < 4; c++ {
						d := float64(int(got[t][c]) - int(sc.block[t][c]))
						sums[c] += d * d
					}
				}
			}
		}
		return sums
	}
	flat := perChannel([4]float64{1, 1, 1, 1})
	greenHeavy := perChannel([4]float64{1, 20, 1, 1})
	if greenHeavy[1] >= flat[1] {
		t.Errorf("weighting green by 20 did not lower its error: %v then %v", flat[1], greenHeavy[1])
	}
	if greenHeavy[0] <= flat[0] && greenHeavy[2] <= flat[2] {
		t.Error("weighting green cost neither red nor blue, so the weights are not reaching the fit")
	}
	t.Logf("uniform weights %v, green weighted 20 %v", flat, greenHeavy)
}

// TestEncodedSizeIsOneBytePerTexel pins the size rule the integration layer needs.
func TestEncodedSizeIsOneBytePerTexel(t *testing.T) {
	cases := []struct{ w, h, want int }{
		{4, 4, 16},
		{8, 8, 64},
		{1, 1, 16},
		{5, 5, 64},
		{2048, 2048, 2048 * 2048},
		{2048, 1024, 2048 * 1024},
	}
	for _, c := range cases {
		if got := EncodedSize(c.w, c.h); got != c.want {
			t.Errorf("EncodedSize(%d, %d) = %d, want %d", c.w, c.h, got, c.want)
		}
	}
	if bx, by := BlocksFor(7, 9); bx != 2 || by != 3 {
		t.Errorf("BlocksFor(7, 9) = %d, %d, want 2, 3", bx, by)
	}
}

// TestVkFormatMatchesColorSpace pins the format pairing.
//
// The sampler has to invert exactly the curve the encoder applied, so an sRGB
// encode must name the sRGB block format and nothing else.
func TestVkFormatMatchesColorSpace(t *testing.T) {
	if SRGB.VkFormat() != VkFormatBC7SRGBBlock {
		t.Errorf("SRGB.VkFormat() = %d, want %d", SRGB.VkFormat(), VkFormatBC7SRGBBlock)
	}
	if Linear.VkFormat() != VkFormatBC7UnormBlock {
		t.Errorf("Linear.VkFormat() = %d, want %d", Linear.VkFormat(), VkFormatBC7UnormBlock)
	}
	if VkFormatBC7UnormBlock != 145 || VkFormatBC7SRGBBlock != 146 {
		t.Error("the BC7 VkFormat numbers moved away from 145 and 146")
	}
}

// TestConstantAlphaDoesNotChangeTheRanking proves the shared partition ranking is
// exact, not an approximation.
//
// One ranking per subset count serves every mode of that shape, and it scores all
// four channels. Modes 0 to 3 store no alpha, so an alpha-aware score looks wrong
// for them. It is not: a channel with no spread contributes a zero row and a zero
// column to the covariance, so it changes neither the trace nor the dominant
// eigenvalue. The encoder relies on that to sum nine moments instead of fourteen
// when a block's alpha is constant.
//
// This test checks the claim on the arithmetic directly, which is stronger than
// checking it on an image.
func TestConstantAlphaDoesNotChangeTheRanking(t *testing.T) {
	rng := rand.New(rand.NewSource(555))
	sw := [4]float64{1, 1, 1, 1}
	for trial := 0; trial < 200; trial++ {
		var sums3, sums4 [momentCount]float64
		alpha := float64(rng.Intn(256))
		count := 2 + rng.Intn(15)
		for i := 0; i < count; i++ {
			var v [4]float64
			for c := 0; c < 3; c++ {
				v[c] = float64(rng.Intn(256))
			}
			v[3] = alpha
			for c := 0; c < 4; c++ {
				sums3[sumIdx[c]] += v[c]
				sums4[sumIdx[c]] += v[c]
			}
			for a := 0; a < 4; a++ {
				for b := a; b < 4; b++ {
					sums3[prodIdx[a][b]] += v[a] * v[b]
					sums4[prodIdx[a][b]] += v[a] * v[b]
				}
			}
		}
		three := residual(&sums3, float64(count), 3, &sw)
		four := residual(&sums4, float64(count), 4, &sw)
		if math.Abs(three-four) > 1e-6*(1+math.Abs(three)) {
			t.Fatalf("trial %d: three-channel score %v, four-channel score %v", trial, three, four)
		}
	}
}

// TestRankingWidthFollowsAlpha proves the block preparation picks the narrow path
// only when it is safe.
func TestRankingWidthFollowsAlpha(t *testing.T) {
	sc := &scratch{}
	for t2 := 0; t2 < 16; t2++ {
		sc.block[t2] = [4]int32{int32(t2 * 16), 100, 200, 255}
	}
	sc.prepare()
	if sc.rankChans != 3 {
		t.Errorf("a block with constant alpha ranks %d channels, want 3", sc.rankChans)
	}
	sc.block[7][3] = 128
	sc.prepare()
	if sc.rankChans != 4 {
		t.Errorf("a block with varying alpha ranks %d channels, want 4", sc.rankChans)
	}
}

// TestStatsArithmetic covers the reported measures.
func TestStatsArithmetic(t *testing.T) {
	perfect := Stats{Blocks: 1, Samples: 64, SquaredError: 0}
	if !math.IsInf(perfect.PSNR(), 1) {
		t.Errorf("a lossless encode reported %v dB, want positive infinity", perfect.PSNR())
	}
	// One code of error on every sample gives a mean squared error of 1, so the
	// ratio is 255 squared and the result is 10*log10(65025), about 48.13 dB.
	one := Stats{Blocks: 1, Samples: 64, SquaredError: 64}
	if math.Abs(one.MSE()-1) > 1e-12 {
		t.Errorf("MSE = %v, want 1", one.MSE())
	}
	if math.Abs(one.PSNR()-48.1308) > 0.001 {
		t.Errorf("PSNR = %v, want about 48.1308", one.PSNR())
	}
	if empty := (Stats{}); empty.MSE() != 0 || !math.IsInf(empty.PSNR(), 1) {
		t.Error("an empty Stats must report zero error")
	}
}
