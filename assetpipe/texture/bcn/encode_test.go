package bcn

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// TestBlockCasesBC1 runs the six blocks a naive encoder fails.
//
// Each case states the property that matters for that block, not a single
// threshold for all of them. A solid block must come back within the RGB565
// endpoint step. A two-colour edge must come back within the same step, because
// two endpoints can hold two colours exactly. A block with one outlier must not
// let the outlier drag the other fifteen texels.
func TestBlockCasesBC1(t *testing.T) {
	for _, tc := range blockCases() {
		t.Run(tc.name, func(t *testing.T) {
			cutout := BC1Opaque
			if tc.name == "fully transparent" || tc.name == "opaque except one texel" {
				cutout = BC1Cutout
			}
			payload, err := EncodeBC1(tc.surface, BC1Options{
				Transfer: tc.transfer, Quality: QualityHigh, Alpha: cutout,
			})
			if err != nil {
				t.Fatalf("EncodeBC1: %v", err)
			}
			if len(payload) != 8 {
				t.Fatalf("one block must produce 8 bytes, got %d", len(payload))
			}
			reference, err := ReferenceCodes(tc.surface, tc.transfer)
			if err != nil {
				t.Fatalf("ReferenceCodes: %v", err)
			}
			decoded, err := DecodeBlockBC1(payload)
			if err != nil {
				t.Fatalf("DecodeBlockBC1: %v", err)
			}

			switch tc.name {
			case "solid colour", "hard two-colour edge":
				// One RGB565 code step is 9 codes in red and blue, so the
				// nearest endpoint sits within 5 codes of any target. Both
				// blocks need at most two distinct colours, so nothing else
				// contributes.
				worst := 0
				for i, got := range decoded {
					for c, value := range [3]uint8{got.R, got.G, got.B} {
						d := int(value) - int(reference[i*4+c])
						if d < 0 {
							d = -d
						}
						if d > worst {
							worst = d
						}
					}
				}
				t.Logf("worst channel error %d codes", worst)
				if worst > 5 {
					t.Errorf("worst channel error %d codes, want at most 5", worst)
				}
			case "single outlier texel":
				// The fifteen quiet texels must stay quiet. A bounding-box
				// encoder stretches its endpoints to the outlier and spreads
				// its error over the whole block.
				worst := 0
				for i, got := range decoded {
					if i == 1*4+1 {
						continue
					}
					for c, value := range [3]uint8{got.R, got.G, got.B} {
						d := int(value) - int(reference[i*4+c])
						if d < 0 {
							d = -d
						}
						if d > worst {
							worst = d
						}
					}
				}
				t.Logf("worst quiet-texel error %d codes", worst)
				if worst > 8 {
					t.Errorf("worst quiet-texel error %d codes, want at most 8", worst)
				}
			case "smooth gradient":
				// This block ramps red along x and green along y, so its
				// sixteen colours fill a plane. Two endpoints describe a
				// line, so no BC1 block can hold a plane and the error here
				// measures where the encoder puts its line. The floor of 25
				// dB comes from measurement; a bounding-box encoder reaches
				// about 22 dB on the same block.
				psnr := psnrAgainstSurface(t, tc.surface, tc.transfer, FormatBC1RGB, payload, rgbChannels...)
				boxOnly := encodeBC1Tuned(tc.surface, tc.transfer, false, bc1Tuning{boundingBox: true})
				boxPSNR := psnrAgainstSurface(t, tc.surface, tc.transfer, FormatBC1RGB, boxOnly, rgbChannels...)
				t.Logf("psnr %.2f dB, bounding box alone %.2f dB", psnr, boxPSNR)
				if psnr < 25 {
					t.Errorf("psnr %.2f dB is below 25 dB", psnr)
				}
				if psnr <= boxPSNR {
					t.Errorf("psnr %.2f dB must beat the bounding box at %.2f dB", psnr, boxPSNR)
				}
			case "fully transparent":
				// The canonical transparent block: two zero endpoints select
				// the three-colour mode and index 3 is transparent black.
				want := []byte{0, 0, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF}
				if !bytes.Equal(payload, want) {
					t.Fatalf("got % x, want % x", payload, want)
				}
				for i, got := range decoded {
					if got.A != 0 {
						t.Fatalf("texel %d has alpha %d, want 0", i, got.A)
					}
				}
			case "opaque except one texel":
				cut := 1*4 + 2
				for i, got := range decoded {
					want := uint8(255)
					if i == cut {
						want = 0
					}
					if got.A != want {
						t.Fatalf("texel %d has alpha %d, want %d", i, got.A, want)
					}
				}
				c0 := binary.LittleEndian.Uint16(payload[0:2])
				c1 := binary.LittleEndian.Uint16(payload[2:4])
				if c0 > c1 {
					t.Fatalf("a cutout block needs color0 <= color1, got %#04x and %#04x", c0, c1)
				}
			}
		})
	}
}

// TestBlockCasesBC3 repeats the six blocks for BC3, whose eight alpha bits change
// what the block must do.
func TestBlockCasesBC3(t *testing.T) {
	for _, tc := range blockCases() {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := EncodeBC3(tc.surface, BC3Options{Transfer: tc.transfer, Quality: QualityHigh})
			if err != nil {
				t.Fatalf("EncodeBC3: %v", err)
			}
			if len(payload) != 16 {
				t.Fatalf("one block must produce 16 bytes, got %d", len(payload))
			}
			reference, err := ReferenceCodes(tc.surface, tc.transfer)
			if err != nil {
				t.Fatalf("ReferenceCodes: %v", err)
			}
			decoded, err := DecodeBlockBC3(payload)
			if err != nil {
				t.Fatalf("DecodeBlockBC3: %v", err)
			}
			// BC3 stores alpha with eight bits and two endpoints, so a block
			// with at most two alpha values stores them exactly.
			for i, got := range decoded {
				if want := reference[i*4+3]; got.A != want {
					t.Fatalf("texel %d alpha %d, want %d", i, got.A, want)
				}
			}
			// BC3 keeps all four colour entries, so it must never lose colour
			// to transparency the way a BC1 cutout block does.
			if tc.name == "opaque except one texel" {
				worst := 0
				for i, got := range decoded {
					for c, value := range [3]uint8{got.R, got.G, got.B} {
						d := int(value) - int(reference[i*4+c])
						if d < 0 {
							d = -d
						}
						if d > worst {
							worst = d
						}
					}
				}
				t.Logf("worst colour error %d codes", worst)
				if worst > 5 {
					t.Errorf("worst colour error %d codes, want at most 5", worst)
				}
			}
		})
	}
}

// TestBlockCasesBC4 runs the same blocks through the single-channel encoder on
// the alpha channel, which is the channel those cases exercise.
func TestBlockCasesBC4(t *testing.T) {
	for _, tc := range blockCases() {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := EncodeBC4(tc.surface, BC4Options{
				Transfer: TransferUnorm, Quality: QualityHigh, Channel: ChannelA,
			})
			if err != nil {
				t.Fatalf("EncodeBC4: %v", err)
			}
			reference, err := ReferenceCodes(tc.surface, TransferUnorm)
			if err != nil {
				t.Fatalf("ReferenceCodes: %v", err)
			}
			codes, err := DecodeBlockBC4Codes(payload)
			if err != nil {
				t.Fatalf("DecodeBlockBC4Codes: %v", err)
			}
			// Every case holds at most two distinct alpha values, and two
			// endpoints store two values exactly.
			for i, got := range codes {
				if want := reference[i*4+3]; got != want {
					t.Fatalf("texel %d got %d, want %d", i, got, want)
				}
			}
		})
	}
}

// TestBC1OpaqueNeverUsesTransparentIndex checks the promise of FormatBC1RGB.
//
// The opaque variant may still pick the three-colour mode, because that mode is
// sometimes the better fit. Index 3 of that mode decodes to black, and the RGBA
// VkFormat also makes it transparent. So the encoder must never assign index 3 on
// an opaque block, or the payload would decode two ways.
func TestBC1OpaqueNeverUsesTransparentIndex(t *testing.T) {
	images := append(colourImages(), namedSurface{"alpha ramp", alphaImage(64), TransferSRGB})
	for _, image := range images {
		t.Run(image.name, func(t *testing.T) {
			for _, quality := range []Quality{QualityFast, QualityHigh} {
				payload, err := EncodeBC1(image.surface, BC1Options{
					Transfer: image.transfer, Quality: quality, Alpha: BC1Opaque,
				})
				if err != nil {
					t.Fatalf("EncodeBC1: %v", err)
				}
				for offset := 0; offset < len(payload); offset += 8 {
					block := payload[offset : offset+8]
					c0 := binary.LittleEndian.Uint16(block[0:2])
					c1 := binary.LittleEndian.Uint16(block[2:4])
					if c0 > c1 {
						continue // four-colour mode, index 3 is a colour
					}
					bits := binary.LittleEndian.Uint32(block[4:8])
					for texel := 0; texel < 16; texel++ {
						if (bits>>(2*texel))&3 == 3 {
							t.Fatalf("%s block %d texel %d uses the transparent index",
								quality, offset/8, texel)
						}
					}
				}
			}
		})
	}
}

// TestEdgeClampLeavesNoSeam checks the padding rule on a size that is not a
// multiple of four.
//
// A 5x5 solid image needs four blocks, and three of them are mostly padding. The
// padding repeats the edge, so every decoded texel must still hold the solid
// colour. Padding with black would drag the endpoints of those blocks toward
// black and would show as a dark seam.
func TestEdgeClampLeavesNoSeam(t *testing.T) {
	const code = 200
	surface := srgbSurface(5, 5, func(x, y int) RGBA8 {
		return RGBA8{R: code, G: code, B: code, A: 255}
	})
	payload, err := EncodeBC1(surface, BC1Options{Transfer: TransferSRGB, Quality: QualityHigh})
	if err != nil {
		t.Fatalf("EncodeBC1: %v", err)
	}
	if want := PayloadSize(FormatBC1RGB, 5, 5); len(payload) != want {
		t.Fatalf("payload holds %d bytes, want %d", len(payload), want)
	}
	got, err := Decode(FormatBC1RGB, payload, 5, 5)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	for pixel := 0; pixel < 25; pixel++ {
		for c := 0; c < 3; c++ {
			d := int(got[pixel*4+c]) - code
			if d < 0 {
				d = -d
			}
			if d > 5 {
				t.Fatalf("texel %d channel %d is %d, want within 5 of %d",
					pixel, c, got[pixel*4+c], code)
			}
		}
	}
}

// TestWorkersDoNotChangeOutput checks the payload is the same at every worker
// count. A build that produced different bytes on a different machine would break
// every content hash downstream.
func TestWorkersDoNotChangeOutput(t *testing.T) {
	colour := colourImages()[1]
	normals := normalImage(37)
	for _, workers := range []int{1, 2, 5, -1} {
		t.Run(colour.name, func(t *testing.T) {
			one, err := EncodeBC1(colour.surface, BC1Options{Transfer: colour.transfer, Workers: 1})
			if err != nil {
				t.Fatalf("EncodeBC1: %v", err)
			}
			many, err := EncodeBC1(colour.surface, BC1Options{Transfer: colour.transfer, Workers: workers})
			if err != nil {
				t.Fatalf("EncodeBC1: %v", err)
			}
			if !bytes.Equal(one, many) {
				t.Fatalf("BC1 payload changed with %d workers", workers)
			}

			oneBC5, err := EncodeBC5Normal(normals, BC5Options{Transfer: TransferUnorm, Workers: 1})
			if err != nil {
				t.Fatalf("EncodeBC5Normal: %v", err)
			}
			manyBC5, err := EncodeBC5Normal(normals, BC5Options{Transfer: TransferUnorm, Workers: workers})
			if err != nil {
				t.Fatalf("EncodeBC5Normal: %v", err)
			}
			if !bytes.Equal(oneBC5, manyBC5) {
				t.Fatalf("BC5 payload changed with %d workers", workers)
			}
		})
	}
}

// TestEncodersRejectABadTransfer checks the guard that stops the darkening bug.
//
// The zero value of Transfer is illegal, so a caller who forgets the field gets an
// error instead of a texture that is wrong in a way no later stage can see.
func TestEncodersRejectABadTransfer(t *testing.T) {
	surface := srgbSurface(4, 4, func(x, y int) RGBA8 { return RGBA8{A: 255} })
	if _, err := EncodeBC1(surface, BC1Options{}); !errors.Is(err, ErrTransfer) {
		t.Errorf("EncodeBC1 with no transfer returned %v, want ErrTransfer", err)
	}
	if _, err := EncodeBC3(surface, BC3Options{}); !errors.Is(err, ErrTransfer) {
		t.Errorf("EncodeBC3 with no transfer returned %v, want ErrTransfer", err)
	}
	if _, err := EncodeBC4(surface, BC4Options{}); !errors.Is(err, ErrTransfer) {
		t.Errorf("EncodeBC4 with no transfer returned %v, want ErrTransfer", err)
	}
	// BC4 and BC5 have no sRGB VkFormat, so they must refuse the sRGB
	// transfer instead of quietly writing codes no format can read back.
	if _, err := EncodeBC4(surface, BC4Options{Transfer: TransferSRGB}); !errors.Is(err, ErrTransfer) {
		t.Errorf("EncodeBC4 with the sRGB transfer returned %v, want ErrTransfer", err)
	}
	if _, err := EncodeBC5(surface, BC5Options{Transfer: TransferSRGB}); !errors.Is(err, ErrTransfer) {
		t.Errorf("EncodeBC5 with the sRGB transfer returned %v, want ErrTransfer", err)
	}
	if _, err := EncodeBC5Normal(surface, BC5Options{Transfer: TransferSRGB}); !errors.Is(err, ErrTransfer) {
		t.Errorf("EncodeBC5Normal with the sRGB transfer returned %v, want ErrTransfer", err)
	}
}

// TestEncodersRejectABadSurface checks the shape and channel guards.
func TestEncodersRejectABadSurface(t *testing.T) {
	if _, err := EncodeBC1(nil, BC1Options{Transfer: TransferSRGB}); !errors.Is(err, ErrShape) {
		t.Errorf("EncodeBC1 with no surface returned %v, want ErrShape", err)
	}
	short := &Surface{Width: 4, Height: 4, Pix: make([]float32, 8)}
	if _, err := EncodeBC1(short, BC1Options{Transfer: TransferSRGB}); !errors.Is(err, ErrShape) {
		t.Errorf("EncodeBC1 with a short pixel slice returned %v, want ErrShape", err)
	}
	surface := NewSurface(4, 4)
	if _, err := EncodeBC4(surface, BC4Options{Transfer: TransferUnorm, Channel: 9}); !errors.Is(err, ErrChannel) {
		t.Errorf("EncodeBC4 with channel 9 returned %v, want ErrChannel", err)
	}
	if _, err := EncodeBC5(surface, BC5Options{Transfer: TransferUnorm, Second: -1}); !errors.Is(err, ErrChannel) {
		t.Errorf("EncodeBC5 with channel -1 returned %v, want ErrChannel", err)
	}
}

// TestDecodeRejectsABadPayload checks the decoder guards, which the integration
// layer relies on when it reads a payload back.
func TestDecodeRejectsABadPayload(t *testing.T) {
	if _, err := Decode(FormatBC1RGB, make([]byte, 7), 4, 4); !errors.Is(err, ErrPayload) {
		t.Errorf("Decode with 7 bytes returned %v, want ErrPayload", err)
	}
	if _, err := Decode(FormatUnknown, make([]byte, 8), 4, 4); !errors.Is(err, ErrFormat) {
		t.Errorf("Decode with an unknown format returned %v, want ErrFormat", err)
	}
	if _, err := Decode(FormatBC1RGB, make([]byte, 8), 0, 4); !errors.Is(err, ErrShape) {
		t.Errorf("Decode with a zero width returned %v, want ErrShape", err)
	}
	if _, err := DecodeBlockBC1(make([]byte, 4)); !errors.Is(err, ErrPayload) {
		t.Errorf("DecodeBlockBC1 with 4 bytes returned %v, want ErrPayload", err)
	}
	if _, err := DecodeBlockBC3(make([]byte, 8)); !errors.Is(err, ErrPayload) {
		t.Errorf("DecodeBlockBC3 with 8 bytes returned %v, want ErrPayload", err)
	}
	if _, err := DecodeBlockBC5(make([]byte, 8)); !errors.Is(err, ErrPayload) {
		t.Errorf("DecodeBlockBC5 with 8 bytes returned %v, want ErrPayload", err)
	}
}
