package bcn

import (
	"encoding/binary"
	"testing"
)

// Why this file exists
//
// A quality gate that no realistic mistake can trip measures nothing. Every test
// below breaks the encoder or the payload on purpose and requires the gate to
// catch it. The mutations copy real mistakes:
//
//   - Skip the least-squares refinement.
//   - Force the per-channel bounding box and drop the principal axis.
//   - Drop the cluster fit.
//   - Drop the three-colour trial.
//   - Drop the endpoint polish.
//   - Swap the two colour endpoints and leave the indices alone, which is the
//     bug an encoder makes when it forgets that the endpoint order carries the
//     mode.
//   - Invert the indices and leave the endpoints alone, which is the other half
//     of the same mistake.
//   - Force the eight-value BC4 mode and drop the six-value mode.
//   - Encode a BC3 colour block with the BC1 mode rule instead of the forced
//     four-colour rule.
//   - Skip the normal renormalization before a BC5 encode.
//   - Encode a colour texture through the unorm transfer instead of the sRGB one.

// TestDegradedBC1EncodersFailTheGate runs the gates against weaker encoders.
//
// Each mutant degrades one quality level, so the gates of that level judge it.
// One image is enough to catch a mutation, because a texture set holds many kinds
// of image and the gate runs on all of them.
func TestDegradedBC1EncodersFailTheGate(t *testing.T) {
	mutants := []struct {
		name   string
		tuning bc1Tuning
		gates  map[string]float64
	}{
		{
			"fast without refinement",
			bc1Tuning{boundingBox: true},
			bc1FastGates,
		},
		{
			"high with the bounding box only",
			bc1Tuning{boundingBox: true, refineIters: 4},
			bc1HighGates,
		},
		{
			"high without the endpoint polish",
			bc1Tuning{boundingBox: true, principalAxis: true, refineIters: 4, clusterFit: true, clusterKeep: 4, threeColor: true},
			bc1HighGates,
		},
	}
	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			caught := 0
			for _, image := range colourImages() {
				payload := encodeBC1Tuned(image.surface, image.transfer, false, mutant.tuning)
				psnr := psnrAgainstSurface(t, image.surface, image.transfer, FormatBC1RGB, payload, rgbChannels...)
				floor := mutant.gates[image.name]
				t.Logf("%-10s psnr %6.2f dB, gate %5.2f dB, caught %v",
					image.name, psnr, floor, psnr < floor)
				if psnr < floor {
					caught++
				}
			}
			if caught == 0 {
				t.Errorf("no image caught the mutation %q, so the gates do not test it", mutant.name)
			}
		})
	}
}

// TestRefinementIsRedundantAtHighQuality records a measured surprise.
//
// The least-squares refinement is worth 3.66 dB on the detail image when it is
// the only step above the bounding box. Add the cluster fit and the endpoint
// polish, and its own contribution falls to about a hundredth of a decibel,
// because those two steps reach the same local optimum on their own.
//
// So no gate can catch the removal of the refinement at QualityHigh, and this test
// says so out loud instead of pretending a gate covers it. QualityFast keeps the
// refinement because it has neither of the other two steps, and the first case
// below measures what it is worth there.
func TestRefinementIsRedundantAtHighQuality(t *testing.T) {
	fastWith := bc1Tuning{boundingBox: true, refineIters: 1}
	fastWithout := bc1Tuning{boundingBox: true}
	highWith := bc1TuningFor(QualityHigh)
	highWithout := highWith
	highWithout.refineIters = 0

	fastGain, highGain := 0.0, 0.0
	for _, image := range colourImages() {
		with := psnrAgainstSurface(t, image.surface, image.transfer, FormatBC1RGB,
			encodeBC1Tuned(image.surface, image.transfer, false, fastWith), rgbChannels...)
		without := psnrAgainstSurface(t, image.surface, image.transfer, FormatBC1RGB,
			encodeBC1Tuned(image.surface, image.transfer, false, fastWithout), rgbChannels...)
		hWith := psnrAgainstSurface(t, image.surface, image.transfer, FormatBC1RGB,
			encodeBC1Tuned(image.surface, image.transfer, false, highWith), rgbChannels...)
		hWithout := psnrAgainstSurface(t, image.surface, image.transfer, FormatBC1RGB,
			encodeBC1Tuned(image.surface, image.transfer, false, highWithout), rgbChannels...)
		t.Logf("%-10s fast gain %+6.3f dB, high gain %+6.3f dB",
			image.name, with-without, hWith-hWithout)
		if with-without > fastGain {
			fastGain = with - without
		}
		if hWith-hWithout > highGain {
			highGain = hWith - hWithout
		}
	}
	if fastGain < 1.0 {
		t.Errorf("refinement bought only %.3f dB at QualityFast, so QualityFast should drop it", fastGain)
	}
	if highGain > 0.5 {
		t.Errorf("refinement bought %.3f dB at QualityHigh, so a gate should cover its removal", highGain)
	}
}

// TestEndpointSwapWithoutIndexRemapBreaksTheImage proves the mode selector and the
// index maps matter.
//
// The mutation swaps color0 and color1 in every block and leaves the indices
// alone. A block that used the four-colour mode then reads as a three-colour
// block, index 3 turns transparent, and the two interpolated entries land on the
// wrong side of the line. A correct encoder must never produce that, and the gate
// must catch it if it did.
func TestEndpointSwapWithoutIndexRemapBreaksTheImage(t *testing.T) {
	for _, image := range colourImages() {
		t.Run(image.name, func(t *testing.T) {
			payload, err := EncodeBC1(image.surface, BC1Options{Transfer: image.transfer, Quality: QualityHigh})
			if err != nil {
				t.Fatalf("EncodeBC1: %v", err)
			}
			good := psnrAgainstSurface(t, image.surface, image.transfer, FormatBC1RGB, payload, rgbChannels...)

			swapped := make([]byte, len(payload))
			copy(swapped, payload)
			for offset := 0; offset < len(swapped); offset += 8 {
				c0 := binary.LittleEndian.Uint16(swapped[offset : offset+2])
				c1 := binary.LittleEndian.Uint16(swapped[offset+2 : offset+4])
				binary.LittleEndian.PutUint16(swapped[offset:offset+2], c1)
				binary.LittleEndian.PutUint16(swapped[offset+2:offset+4], c0)
			}
			bad := psnrAgainstSurface(t, image.surface, image.transfer, FormatBC1RGB, swapped, rgbChannels...)
			t.Logf("correct %6.2f dB, endpoints swapped %6.2f dB", good, bad)
			if bad >= bc1HighGates[image.name] {
				t.Errorf("the swapped payload scored %.2f dB, which the gate at %.2f dB would pass",
					bad, bc1HighGates[image.name])
			}

			inverted := make([]byte, len(payload))
			copy(inverted, payload)
			for offset := 0; offset < len(inverted); offset += 8 {
				bits := binary.LittleEndian.Uint32(inverted[offset+4 : offset+8])
				// Exchange index 0 with 1 and index 2 with 3, which is the
				// remap a swap needs. Applying it without the swap is the
				// mirror mistake.
				binary.LittleEndian.PutUint32(inverted[offset+4:offset+8], bits^0x55555555)
			}
			mirrored := psnrAgainstSurface(t, image.surface, image.transfer, FormatBC1RGB, inverted, rgbChannels...)
			t.Logf("indices remapped without the swap %6.2f dB", mirrored)
			if mirrored >= bc1HighGates[image.name] {
				t.Errorf("the remapped payload scored %.2f dB, which the gate at %.2f dB would pass",
					mirrored, bc1HighGates[image.name])
			}
		})
	}
}

// TestBC4SixValueModeEarnsItsPlace proves the second BC4 mode is worth its code.
//
// The mask image holds plateaus at 0 and 255 with soft slopes between them. The
// six-value mode reaches those two values for free and spends both endpoints on
// the slope. An encoder that only knew the eight-value mode must lose measurable
// decibels there.
//
// The margin of 0.2 dB comes from measurement. Dropping the mode costs about
// 0.4 dB on the mask image, so the assertion has room to hold and still fails if
// the mode stops working.
func TestBC4SixValueModeEarnsItsPlace(t *testing.T) {
	const margin = 0.2
	caught := false
	for _, image := range dataImages() {
		full := encodeBC4Tuned(image.surface, ChannelR, bc4TuningFor(QualityHigh))
		fullPSNR := psnrAgainstSurface(t, image.surface, image.transfer, FormatBC4, full, ChannelR)
		// The mutant keeps every other step and drops the six-value mode with
		// the candidate that only that mode can use.
		mutant := encodeBC4Tuned(image.surface, ChannelR, bc4Tuning{refineIters: 4, jitter: 1, polishSweeps: 4})
		mutantPSNR := psnrAgainstSurface(t, image.surface, image.transfer, FormatBC4, mutant, ChannelR)
		t.Logf("%-15s both modes %6.2f dB, eight-value only %6.2f dB, loss %+5.2f dB",
			image.name, fullPSNR, mutantPSNR, mutantPSNR-fullPSNR)
		if mutantPSNR > fullPSNR+0.001 {
			t.Errorf("%s: dropping a mode raised the score by %.3f dB, which cannot happen",
				image.name, mutantPSNR-fullPSNR)
		}
		if image.name == "cutout mask" {
			caught = fullPSNR-mutantPSNR >= margin
			if !caught {
				t.Errorf("dropping the six-value mode cost only %.3f dB on the mask, want at least %.2f dB",
					fullPSNR-mutantPSNR, margin)
			}
		}
	}
	if !caught {
		t.Error("no image measured the value of the six-value mode")
	}
}

// TestBC3WithoutForcedFourColourBreaksTheEncoderBelief proves the BC3 colour rule
// matters.
//
// The sharp claim is not that the mistake lowers the average. It is that the
// encoder and the decoder stop agreeing. A BC3 colour block always decodes with
// the four-colour mode, so an encoder that scored a three-colour palette scored a
// block that no decoder produces. The error it reports is then fiction, and every
// later decision that reads that error is fiction too.
//
// The average is a weak way to see this. The measurement below reports it and
// shows why: the mistake touches a small share of blocks, and on smooth content
// the wrong palette lands close enough that the mean barely moves. The belief
// check does not depend on content.
func TestBC3WithoutForcedFourColourBreaksTheEncoderBelief(t *testing.T) {
	tuning := bc1TuningFor(QualityHigh)

	// One block built to make the three-colour mode win by a wide margin.
	// Five texels sit at black, six sit at the exact midpoint of the line, and
	// five sit at the far end. The four-colour mode can only put entries at one
	// third and two thirds, so it misses the midpoint by about forty codes for
	// each of the six texels. The three-colour mode holds all three colours.
	var texels [16]vec3
	far := vec3{248, 252, 248} // an RGB565 endpoint that expands exactly
	mid := vec3{124, 126, 124}
	for i := 0; i < 5; i++ {
		texels[i] = vec3{}
	}
	for i := 5; i < 11; i++ {
		texels[i] = mid
	}
	for i := 11; i < 16; i++ {
		texels[i] = far
	}

	var forced, loose [8]byte
	believedForced := encodeColorBlock(&texels, 0, tuning, true, forced[:])
	believedLoose := encodeColorBlock(&texels, 0, tuning, false, loose[:])
	decodedForced := decodedBlockSSE(t, &texels, forced[:], true)
	decodedLoose := decodedBlockSSE(t, &texels, loose[:], true)
	t.Logf("forced four-colour: believed %.1f, a BC3 decoder gives %.1f", believedForced, decodedForced)
	t.Logf("BC1 mode rule:      believed %.1f, a BC3 decoder gives %.1f", believedLoose, decodedLoose)

	if !closeEnough(believedForced, decodedForced) {
		t.Errorf("the correct encoder believed %.4f and a BC3 decoder gives %.4f",
			believedForced, decodedForced)
	}
	if closeEnough(believedLoose, decodedLoose) {
		t.Error("the mutation agreed with the decoder on this block, so the rule is untested")
	}
	if decodedLoose <= decodedForced {
		t.Errorf("the mutation decoded to %.1f and the correct encoder to %.1f; "+
			"the forced rule must win here", decodedLoose, decodedForced)
	}

	// Now count the whole image, which shows why an average is a weak way to see
	// the mistake: it touches a small share of blocks.
	image := colourImages()[3].surface
	across, down := BlocksAcross(image.Width), BlocksAcross(image.Height)
	blocks, differ := 0, 0
	for by := 0; by < down; by++ {
		for bx := 0; bx < across; bx++ {
			block, _ := gatherColor(image, bx, by, TransferSRGB, false, 0)
			var correct, mutant [8]byte
			encodeColorBlock(&block, 0, tuning, true, correct[:])
			encodeColorBlock(&block, 0, tuning, false, mutant[:])
			blocks++
			if correct != mutant {
				differ++
			}
		}
	}
	t.Logf("on a smooth banded image the mutation changes %d of %d blocks", differ, blocks)
}

// TestEncoderBeliefMatchesTheDecoder is the sharpest check on the emit path.
//
// The encoder picks a block by the squared error it computes. The decoder then
// produces some palette from the bytes. If the two numbers differ, the encoder
// chose by a measurement of something it did not write, and every quality number
// in this package would be measuring the wrong object.
//
// A wrong endpoint order, a wrong index map, a wrong swap, or a wrong palette
// weight all break this and nothing else needs to.
func TestEncoderBeliefMatchesTheDecoder(t *testing.T) {
	for _, image := range colourImages() {
		t.Run("bc1 "+image.name, func(t *testing.T) {
			for _, quality := range []Quality{QualityFast, QualityHigh} {
				tuning := bc1TuningFor(quality)
				across, down := BlocksAcross(image.surface.Width), BlocksAcross(image.surface.Height)
				for by := 0; by < down; by++ {
					for bx := 0; bx < across; bx++ {
						texels, mask := gatherColor(image.surface, bx, by, image.transfer, false, 0)
						var payload [8]byte
						believed := encodeColorBlock(&texels, mask, tuning, false, payload[:])
						decoded := decodedBlockSSE(t, &texels, payload[:], false)
						if !closeEnough(believed, decoded) {
							t.Fatalf("%s block %d,%d: the encoder believed %.4f and the decoder gave %.4f",
								quality, bx, by, believed, decoded)
						}
					}
				}
			}
		})
	}
	// Repeat for the cutout variant, whose blocks carry a transparency mask and
	// therefore a different index map.
	cutout := srgbSurface(32, 32, func(x, y int) RGBA8 {
		c := RGBA8{R: uint8(x * 8), G: uint8(y * 8), B: 90, A: 255}
		if (x/3+y/2)%3 == 0 {
			c.A = 0
		}
		return c
	})
	t.Run("bc1 cutout", func(t *testing.T) {
		tuning := bc1TuningFor(QualityHigh)
		for by := 0; by < BlocksAcross(32); by++ {
			for bx := 0; bx < BlocksAcross(32); bx++ {
				texels, mask := gatherColor(cutout, bx, by, TransferSRGB, true, 0.5)
				var payload [8]byte
				believed := encodeColorBlock(&texels, mask, tuning, false, payload[:])
				decoded := decodedBlockSSEMasked(t, &texels, mask, payload[:])
				if !closeEnough(believed, decoded) {
					t.Fatalf("block %d,%d: the encoder believed %.4f and the decoder gave %.4f",
						bx, by, believed, decoded)
				}
			}
		}
	})
	// And for BC4, whose two modes also live in the endpoint order.
	for _, image := range dataImages() {
		t.Run("bc4 "+image.name, func(t *testing.T) {
			for _, quality := range []Quality{QualityFast, QualityHigh} {
				tuning := bc4TuningFor(quality)
				across, down := BlocksAcross(image.surface.Width), BlocksAcross(image.surface.Height)
				for by := 0; by < down; by++ {
					for bx := 0; bx < across; bx++ {
						var values [16]float64
						image.surface.gatherChannel(bx, by, ChannelR, &values)
						var payload [8]byte
						believed := encodeBC4Block(&values, tuning, payload[:])
						codes, err := DecodeBlockBC4Codes(payload[:])
						if err != nil {
							t.Fatalf("DecodeBlockBC4Codes: %v", err)
						}
						decoded := 0.0
						for i, code := range codes {
							d := float64(code) - values[i]
							decoded += d * d
						}
						if !closeEnough(believed, decoded) {
							t.Fatalf("%s block %d,%d: the encoder believed %.4f and the decoder gave %.4f",
								quality, bx, by, believed, decoded)
						}
					}
				}
			}
		})
	}
}

// decodedBlockSSE returns the squared error of one decoded colour block.
func decodedBlockSSE(t *testing.T, texels *[16]vec3, payload []byte, forceFour bool) float64 {
	t.Helper()
	pal := colorPalette(uint16(payload[0])|uint16(payload[1])<<8,
		uint16(payload[2])|uint16(payload[3])<<8, forceFour)
	bits := uint32(payload[4]) | uint32(payload[5])<<8 | uint32(payload[6])<<16 | uint32(payload[7])<<24
	total := 0.0
	for i := range texels {
		entry := pal[(bits>>uint(2*i))&3]
		total += texels[i].squaredDistance(vec3{float64(entry.R), float64(entry.G), float64(entry.B)})
	}
	return total
}

// decodedBlockSSEMasked repeats the measurement and skips the texels the block
// stores as transparent, which is what the encoder scores.
func decodedBlockSSEMasked(t *testing.T, texels *[16]vec3, mask uint16, payload []byte) float64 {
	t.Helper()
	pal := colorPalette(uint16(payload[0])|uint16(payload[1])<<8,
		uint16(payload[2])|uint16(payload[3])<<8, false)
	bits := uint32(payload[4]) | uint32(payload[5])<<8 | uint32(payload[6])<<16 | uint32(payload[7])<<24
	total := 0.0
	for i := range texels {
		index := (bits >> uint(2*i)) & 3
		if mask>>uint(i)&1 != 0 {
			if index != 3 {
				t.Fatalf("texel %d is transparent but holds index %d", i, index)
			}
			continue
		}
		if index == 3 {
			t.Fatalf("texel %d is opaque but holds the transparent index", i)
		}
		entry := pal[index]
		total += texels[i].squaredDistance(vec3{float64(entry.R), float64(entry.G), float64(entry.B)})
	}
	return total
}

// closeEnough compares two squared errors. Both come from the same integer codes,
// so they agree exactly unless the two disagree about the palette.
func closeEnough(a, b float64) bool {
	diff := a - b
	return diff < 1e-6 && diff > -1e-6
}

// TestUnormTransferOnAColourImageIsDetectable proves the transfer choice is a real
// decision and not a matter of taste.
//
// The mutation encodes a colour image through the unorm transfer and then reads it
// back as if the GPU applied the sRGB transfer function, which is what an sRGB
// VkFormat does. The mean linear light moves a long way, which is the same failure
// TestMipChainKeepsLinearMean catches in package texture.
func TestUnormTransferOnAColourImageIsDetectable(t *testing.T) {
	image := colourImages()[3] // the smooth banded image
	mean := func(codes []byte, transfer Transfer) float64 {
		total := 0.0
		count := 0
		for base := 0; base < len(codes); base += 4 {
			for c := 0; c < 3; c++ {
				total += float64(transfer.decode(codes[base+c]))
				count++
			}
		}
		return total / float64(count)
	}

	correct, err := EncodeBC1(image.surface, BC1Options{Transfer: TransferSRGB, Quality: QualityHigh})
	if err != nil {
		t.Fatalf("EncodeBC1: %v", err)
	}
	wrong, err := EncodeBC1(image.surface, BC1Options{Transfer: TransferUnorm, Quality: QualityHigh})
	if err != nil {
		t.Fatalf("EncodeBC1: %v", err)
	}
	correctCodes, err := Decode(FormatBC1RGB, correct, image.surface.Width, image.surface.Height)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	wrongCodes, err := Decode(FormatBC1RGB, wrong, image.surface.Width, image.surface.Height)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	// The source mean in linear light.
	source := 0.0
	for pixel := 0; pixel < image.surface.Width*image.surface.Height; pixel++ {
		i := pixel * 4
		source += float64(image.surface.Pix[i]) + float64(image.surface.Pix[i+1]) + float64(image.surface.Pix[i+2])
	}
	source /= float64(image.surface.Width * image.surface.Height * 3)

	// A GPU reading an sRGB VkFormat always applies the sRGB transfer
	// function, whatever the encoder meant.
	good := mean(correctCodes, TransferSRGB)
	bad := mean(wrongCodes, TransferSRGB)
	t.Logf("source linear mean %.4f, sRGB transfer %.4f, unorm transfer %.4f", source, good, bad)
	if diff := good - source; diff > 0.01 || diff < -0.01 {
		t.Errorf("the correct transfer moved the linear mean by %.4f, want at most 0.01", diff)
	}
	if diff := bad - source; diff > -0.05 && diff < 0.05 {
		t.Errorf("the wrong transfer moved the linear mean by only %.4f, "+
			"so this test cannot catch the mistake", diff)
	}
}

// TestClusterFitAndThreeColourValue measures two steps whose value the endpoint
// polish mostly takes over.
//
// The finding is worth stating plainly. On its own, the cluster fit buys 0.03 to
// 0.10 dB over a refined principal-axis fit. Add the endpoint polish and its own
// contribution falls to 0.03 dB or less, because the polish climbs to the same
// local optimum from a nearer start. The same holds for the three-colour trial,
// which buys 0.45 dB on a smooth banded image without the polish and nothing
// measurable with it.
//
// So neither step can be caught by a quality gate at QualityHigh, and this test
// measures them directly instead. The assertions run on the configuration without
// the polish, where both steps carry their own weight.
func TestClusterFitAndThreeColourValue(t *testing.T) {
	base := bc1Tuning{boundingBox: true, principalAxis: true, refineIters: 4}
	withCluster := base
	withCluster.clusterFit = true
	withCluster.clusterKeep = 4
	withThree := withCluster
	withThree.threeColor = true

	polished := func(tuning bc1Tuning) bc1Tuning {
		tuning.polishSweeps = 4
		return tuning
	}

	bestClusterGain, bestThreeGain := 0.0, 0.0
	for _, image := range colourImages() {
		score := func(tuning bc1Tuning) float64 {
			payload := encodeBC1Tuned(image.surface, image.transfer, false, tuning)
			return psnrAgainstSurface(t, image.surface, image.transfer, FormatBC1RGB, payload, rgbChannels...)
		}
		clusterGain := score(withCluster) - score(base)
		threeGain := score(withThree) - score(withCluster)
		clusterAfterPolish := score(polished(withCluster)) - score(polished(base))
		threeAfterPolish := score(polished(withThree)) - score(polished(withCluster))
		t.Logf("%-10s cluster fit %+6.3f dB alone, %+6.3f dB after the polish; "+
			"three-colour %+6.3f dB alone, %+6.3f dB after the polish",
			image.name, clusterGain, clusterAfterPolish, threeGain, threeAfterPolish)

		if clusterGain > bestClusterGain {
			bestClusterGain = clusterGain
		}
		if threeGain > bestThreeGain {
			bestThreeGain = threeGain
		}
		// Both steps only add candidates, so neither may lose ground.
		if clusterAfterPolish < -0.001 || threeAfterPolish < -0.001 {
			t.Errorf("%s: a step that only adds candidates lost ground", image.name)
		}
	}
	if bestClusterGain < 0.02 {
		t.Errorf("the cluster fit bought only %.3f dB at its best, so the step is untested",
			bestClusterGain)
	}
	if bestThreeGain < 0.2 {
		t.Errorf("the three-colour trial bought only %.3f dB at its best, so the step is untested",
			bestThreeGain)
	}
}
