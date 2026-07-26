package bcn

import (
	"encoding/binary"
	"testing"
)

// TestModeSelectorFollowsTheEndpointOrder checks the rule that both BC1 modes
// hang from.
//
// The same index field and the same two endpoint values decode two ways,
// depending only on which endpoint comes first. The test builds both orders from
// one pair and asserts the palettes differ in the documented way.
func TestModeSelectorFollowsTheEndpointOrder(t *testing.T) {
	const high = uint16(0xF800) // red
	const low = uint16(0x001F)  // blue

	fourColour := make([]byte, 8)
	binary.LittleEndian.PutUint16(fourColour[0:2], high)
	binary.LittleEndian.PutUint16(fourColour[2:4], low)
	binary.LittleEndian.PutUint32(fourColour[4:8], 0xE4E4E4E4)

	threeColour := make([]byte, 8)
	binary.LittleEndian.PutUint16(threeColour[0:2], low)
	binary.LittleEndian.PutUint16(threeColour[2:4], high)
	binary.LittleEndian.PutUint32(threeColour[4:8], 0xE4E4E4E4)

	four, err := DecodeBlockBC1(fourColour)
	if err != nil {
		t.Fatalf("DecodeBlockBC1: %v", err)
	}
	three, err := DecodeBlockBC1(threeColour)
	if err != nil {
		t.Fatalf("DecodeBlockBC1: %v", err)
	}

	if four[3].A != 255 {
		t.Error("index 3 of the four-colour mode must be opaque")
	}
	if three[3].A != 0 {
		t.Error("index 3 of the three-colour mode must be transparent")
	}
	// The four-colour mode puts its interpolants at one third and two thirds,
	// and the three-colour mode puts its one interpolant at the midpoint.
	if four[2] == three[2] {
		t.Error("the one third entry and the midpoint entry must differ")
	}
	if got, want := three[2].R, uint8(128); got != want {
		// (0 + 255)/2 rounds to 128 under the round-half-up rule this
		// decoder uses. The published rules leave the rounding open, so the
		// test states which rule the package follows.
		t.Errorf("the midpoint red is %d, want %d", got, want)
	}
}

// TestBC4ModeSelectorFollowsTheEndpointOrder repeats the check for BC4.
func TestBC4ModeSelectorFollowsTheEndpointOrder(t *testing.T) {
	eight := bc4Palette(200, 40)
	six := bc4Palette(40, 200)
	// The eight-value mode spreads all eight entries across the range.
	if six[6] != 0 || six[7] != 255 {
		t.Errorf("the six-value mode must end with 0 and 255, got %v and %v", six[6], six[7])
	}
	if eight[6] == 0 || eight[7] == 255 {
		t.Error("the eight-value mode must not hold the constants 0 and 255")
	}
	// Both modes span the same two endpoints, so the six-value mode takes
	// larger steps between them.
	eightStep := (eight[0] - eight[1]) / 7
	sixStep := (six[1] - six[0]) / 5
	if sixStep <= eightStep {
		t.Errorf("the six-value step %.3f must exceed the eight-value step %.3f", sixStep, eightStep)
	}
}

// TestDecodeAgreesWithTheBlockDecoders checks the whole-level decoder against the
// single-block decoders. A size that is not a multiple of four exercises the crop.
func TestDecodeAgreesWithTheBlockDecoders(t *testing.T) {
	const width, height = 7, 6
	image := srgbSurface(width, height, func(x, y int) RGBA8 {
		return RGBA8{R: uint8(x * 30), G: uint8(y * 40), B: uint8(x*10 + y*20), A: uint8(x * 36)}
	})
	payload, err := EncodeBC3(image, BC3Options{Transfer: TransferSRGB, Quality: QualityHigh})
	if err != nil {
		t.Fatalf("EncodeBC3: %v", err)
	}
	level, err := Decode(FormatBC3, payload, width, height)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	across := BlocksAcross(width)
	for by := 0; by < BlocksAcross(height); by++ {
		for bx := 0; bx < across; bx++ {
			block, err := DecodeBlockBC3(payload[(by*across+bx)*16:])
			if err != nil {
				t.Fatalf("DecodeBlockBC3: %v", err)
			}
			for row := 0; row < 4; row++ {
				y := by*4 + row
				if y >= height {
					continue
				}
				for col := 0; col < 4; col++ {
					x := bx*4 + col
					if x >= width {
						continue
					}
					i := (y*width + x) * 4
					got := RGBA8{level[i], level[i+1], level[i+2], level[i+3]}
					if want := block[row*4+col]; got != want {
						t.Fatalf("at %d,%d got %+v, want %+v", x, y, got, want)
					}
				}
			}
		}
	}
}

// TestDecodeBC1RGBIgnoresTheAlphaBit checks the promise of the RGB VkFormat.
func TestDecodeBC1RGBIgnoresTheAlphaBit(t *testing.T) {
	// A three-colour block whose index 3 would be transparent under the RGBA
	// VkFormat.
	block := []byte{0x00, 0x00, 0x00, 0x04, 0xFF, 0xFF, 0xFF, 0xFF}
	rgb, err := Decode(FormatBC1RGB, block, 4, 4)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	rgba, err := Decode(FormatBC1RGBA, block, 4, 4)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	for texel := 0; texel < 16; texel++ {
		if rgb[texel*4+3] != 255 {
			t.Fatalf("texel %d of the RGB variant has alpha %d, want 255", texel, rgb[texel*4+3])
		}
		if rgba[texel*4+3] != 0 {
			t.Fatalf("texel %d of the RGBA variant has alpha %d, want 0", texel, rgba[texel*4+3])
		}
	}
}

// TestRoundTripIsSelfConsistent runs every format through the encoder and the
// decoder and checks the shapes line up.
//
// The test proves the pair agrees with itself, which is a weaker claim than the
// vector tests make. It sits here to catch a size or ordering mistake that the
// single-block vectors cannot see.
func TestRoundTripIsSelfConsistent(t *testing.T) {
	image := srgbSurface(20, 12, func(x, y int) RGBA8 {
		return RGBA8{R: uint8(x * 12), G: uint8(y * 20), B: uint8(x + y), A: 255}
	})
	cases := []struct {
		format  Format
		encode  func() ([]byte, error)
		channel []Channel
		floor   float64
	}{
		{FormatBC1RGB, func() ([]byte, error) {
			return EncodeBC1(image, BC1Options{Transfer: TransferSRGB, Quality: QualityHigh})
		}, rgbChannels, 29.5},
		{FormatBC3, func() ([]byte, error) {
			return EncodeBC3(image, BC3Options{Transfer: TransferSRGB, Quality: QualityHigh})
		}, rgbChannels, 29.5},
		{FormatBC4, func() ([]byte, error) {
			return EncodeBC4(image, BC4Options{Transfer: TransferUnorm, Quality: QualityHigh})
		}, []Channel{ChannelR}, 40},
		// The floors come from measurement on this small image. The colour
		// floors are lower than the single-channel ones because the image ramps
		// red and green in different directions, so many blocks hold colours
		// that no line through the cube can cover.
		{FormatBC5, func() ([]byte, error) {
			return EncodeBC5(image, BC5Options{Transfer: TransferUnorm, Quality: QualityHigh, Second: ChannelG})
		}, []Channel{ChannelR, ChannelG}, 40},
	}
	for _, tc := range cases {
		t.Run(tc.format.String(), func(t *testing.T) {
			payload, err := tc.encode()
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			transfer := TransferSRGB
			if tc.format == FormatBC4 || tc.format == FormatBC5 {
				transfer = TransferUnorm
			}
			psnr := psnrAgainstSurface(t, image, transfer, tc.format, payload, tc.channel...)
			t.Logf("psnr %.2f dB", psnr)
			if psnr < tc.floor {
				t.Errorf("psnr %.2f dB is below %.2f dB", psnr, tc.floor)
			}
		})
	}
}
