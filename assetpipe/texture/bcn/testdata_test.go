package bcn

import (
	"math"
	"testing"
)

// The test images
//
// Every image below is built from the 8-bit codes it should store, not from
// linear floats. So ReferenceCodes returns those codes back exactly, and any
// error a test reports comes from the block encoder and from nothing else.
// TestSurfaceHelpersRoundTrip proves the property.

// srgbSurface builds a surface whose stored sRGB codes equal the codes at
// returns. The colour channels pass through the inverse sRGB transfer function
// and alpha does not, which is how a decoded PNG reaches this package.
func srgbSurface(width, height int, at func(x, y int) RGBA8) *Surface {
	s := NewSurface(width, height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := at(x, y)
			s.Set(x, y,
				srgbDecodeLUT[c.R], srgbDecodeLUT[c.G], srgbDecodeLUT[c.B],
				float32(c.A)/255)
		}
	}
	return s
}

// unormSurface builds a surface whose stored linear codes equal the codes at
// returns. A data texture reaches this package that way.
func unormSurface(width, height int, at func(x, y int) RGBA8) *Surface {
	s := NewSurface(width, height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := at(x, y)
			s.Set(x, y,
				float32(c.R)/255, float32(c.G)/255, float32(c.B)/255, float32(c.A)/255)
		}
	}
	return s
}

// namedSurface pairs a test image with its name and its transfer function.
type namedSurface struct {
	name     string
	surface  *Surface
	transfer Transfer
}

// blockCases holds the six 4x4 blocks a naive encoder fails.
func blockCases() []namedSurface {
	solid := srgbSurface(4, 4, func(x, y int) RGBA8 {
		return RGBA8{R: 120, G: 90, B: 40, A: 255}
	})
	gradient := srgbSurface(4, 4, func(x, y int) RGBA8 {
		return RGBA8{R: uint8(20 + x*24), G: uint8(60 + y*20), B: 128, A: 255}
	})
	edge := srgbSurface(4, 4, func(x, y int) RGBA8 {
		if x < 2 {
			return RGBA8{R: 250, G: 20, B: 20, A: 255}
		}
		return RGBA8{R: 10, G: 10, B: 240, A: 255}
	})
	outlier := srgbSurface(4, 4, func(x, y int) RGBA8 {
		if x == 1 && y == 1 {
			return RGBA8{R: 250, G: 250, B: 250, A: 255}
		}
		return RGBA8{R: 40, G: 44, B: 48, A: 255}
	})
	transparent := srgbSurface(4, 4, func(x, y int) RGBA8 {
		// The colour stays non-zero on purpose. A correct cutout encoder
		// throws it away and writes the canonical transparent block.
		return RGBA8{R: 200, G: 30, B: 90, A: 0}
	})
	oneCut := srgbSurface(4, 4, func(x, y int) RGBA8 {
		c := RGBA8{R: 60, G: 180, B: 200, A: 255}
		if x == 2 && y == 1 {
			c.A = 0
		}
		return c
	})
	return []namedSurface{
		{"solid colour", solid, TransferSRGB},
		{"smooth gradient", gradient, TransferSRGB},
		{"hard two-colour edge", edge, TransferSRGB},
		{"single outlier texel", outlier, TransferSRGB},
		{"fully transparent", transparent, TransferSRGB},
		{"opaque except one texel", oneCut, TransferSRGB},
	}
}

// colourImages holds the larger colour images the quality measurements use.
func colourImages() []namedSurface {
	const size = 64
	gradient := srgbSurface(size, size, func(x, y int) RGBA8 {
		// A smooth two-way ramp. Banding on a gradient is the classic
		// symptom of a bounding-box encoder.
		return RGBA8{
			R: uint8(x * 255 / (size - 1)),
			G: uint8(y * 255 / (size - 1)),
			B: uint8((x + y) * 255 / (2 * (size - 1))),
			A: 255,
		}
	})
	detail := srgbSurface(size, size, func(x, y int) RGBA8 {
		fx := float64(x) / size
		fy := float64(y) / size
		r := 0.5 + 0.5*math.Sin(fx*18)*math.Cos(fy*7)
		g := 0.5 + 0.5*math.Sin((fx+fy)*11)
		b := 0.5 + 0.4*math.Cos(math.Hypot(fx-0.5, fy-0.5)*24)
		return RGBA8{
			R: encodeUnorm8(float32(r)),
			G: encodeUnorm8(float32(g)),
			B: encodeUnorm8(float32(b)),
			A: 255,
		}
	})
	// A deterministic pseudo random image. It is the worst case for any block
	// encoder, because a 4x4 block of unrelated colours has no dominant axis.
	state := uint32(0x1234567)
	next := func() uint8 {
		state = state*1664525 + 1013904223
		return uint8(state >> 24)
	}
	noise := srgbSurface(size, size, func(x, y int) RGBA8 {
		return RGBA8{R: next(), G: next(), B: next(), A: 255}
	})
	// Skin-like and sky-like bands, which is where banding shows in practice.
	bands := srgbSurface(size, size, func(x, y int) RGBA8 {
		t := float64(y) / (size - 1)
		return RGBA8{
			R: encodeUnorm8(float32(0.85 - 0.35*t)),
			G: encodeUnorm8(float32(0.70 - 0.20*t)),
			B: encodeUnorm8(float32(0.55 + 0.35*t)),
			A: 255,
		}
	})
	return []namedSurface{
		{"gradient", gradient, TransferSRGB},
		{"detail", detail, TransferSRGB},
		{"noise", noise, TransferSRGB},
		{"bands", bands, TransferSRGB},
	}
}

// dataImages holds the single-channel images the BC4 measurements use.
func dataImages() []namedSurface {
	const size = 64
	roughness := unormSurface(size, size, func(x, y int) RGBA8 {
		v := encodeUnorm8(float32(float64(x) / (size - 1)))
		return RGBA8{R: v, G: v, B: v, A: 255}
	})
	// A mask whose plateaus saturate at 0 and 1 while the slopes between them
	// stay soft. Many blocks therefore hold both a saturated value and a
	// midtone, which is exactly where the six-value BC4 mode pays: it reaches
	// 0 and 1 for free and spends both endpoints on the slope.
	mask := unormSurface(size, size, func(x, y int) RGBA8 {
		wave := 0.5 + 0.5*math.Sin(float64(x)*0.32)*math.Cos(float64(y)*0.26)
		v := math.Min(1, math.Max(0, wave*2.2-0.6))
		c := encodeUnorm8(float32(v))
		return RGBA8{R: c, G: c, B: c, A: c}
	})
	occlusion := unormSurface(size, size, func(x, y int) RGBA8 {
		v := 0.5 + 0.5*math.Sin(float64(x)*0.4)*math.Sin(float64(y)*0.3)
		c := encodeUnorm8(float32(v))
		return RGBA8{R: c, G: c, B: c, A: 255}
	})
	return []namedSurface{
		{"roughness ramp", roughness, TransferUnorm},
		{"cutout mask", mask, TransferUnorm},
		{"occlusion", occlusion, TransferUnorm},
	}
}

// normalImage builds a tangent-space normal map from a bumpy height field.
//
// The map holds strong slopes on purpose, so many texels sit near the silhouette
// where the rebuilt z part is small. Those texels are where an angular error
// grows while a per-channel error stays small.
func normalImage(size int) *Surface {
	height := func(x, y float64) float64 {
		return 0.35*math.Sin(x*0.5)*math.Cos(y*0.4) + 0.2*math.Sin((x+y)*0.9)
	}
	s := NewSurface(size, size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			fx, fy := float64(x), float64(y)
			dx := height(fx+1, fy) - height(fx-1, fy)
			dy := height(fx, fy+1) - height(fx, fy-1)
			nx, ny, nz := -dx, -dy, 1.0
			length := math.Sqrt(nx*nx + ny*ny + nz*nz)
			nx, ny, nz = nx/length, ny/length, nz/length
			s.Set(x, y,
				float32(nx*0.5+0.5), float32(ny*0.5+0.5), float32(nz*0.5+0.5), 1)
		}
	}
	return s
}

// alphaImage builds a colour image with a soft alpha ramp, which is what BC3
// exists for.
func alphaImage(size int) *Surface {
	return srgbSurface(size, size, func(x, y int) RGBA8 {
		return RGBA8{
			R: uint8(x * 255 / (size - 1)),
			G: 128,
			B: uint8(255 - y*255/(size-1)),
			A: uint8(y * 255 / (size - 1)),
		}
	})
}

// TestSurfaceHelpersRoundTrip proves the test images hold the codes they claim.
//
// If this fails, every quality number in the package measures the helper instead
// of the encoder.
func TestSurfaceHelpersRoundTrip(t *testing.T) {
	want := func(x, y int) RGBA8 {
		return RGBA8{R: uint8(x * 7), G: uint8(y * 11), B: uint8(x*3 + y*5), A: uint8(x * 9)}
	}
	for _, tc := range []struct {
		name     string
		surface  *Surface
		transfer Transfer
	}{
		{"srgb", srgbSurface(16, 16, want), TransferSRGB},
		{"unorm", unormSurface(16, 16, want), TransferUnorm},
	} {
		t.Run(tc.name, func(t *testing.T) {
			codes, err := ReferenceCodes(tc.surface, tc.transfer)
			if err != nil {
				t.Fatalf("ReferenceCodes: %v", err)
			}
			for y := 0; y < 16; y++ {
				for x := 0; x < 16; x++ {
					i := (y*16 + x) * 4
					got := RGBA8{codes[i], codes[i+1], codes[i+2], codes[i+3]}
					if got != want(x, y) {
						t.Fatalf("at %d,%d got %+v, want %+v", x, y, got, want(x, y))
					}
				}
			}
		})
	}
}

// encodeBC1Tuned runs the colour encoder with an explicit tuning. The ablation
// and the mutation tests need parts of the search switched off, and the public
// API on purpose exposes only whole quality levels.
func encodeBC1Tuned(s *Surface, t Transfer, cutout bool, tuning bc1Tuning) []byte {
	return encodeBlocks(s, 8, 1, func(bx, by int, dst []byte) {
		texels, mask := gatherColor(s, bx, by, t, cutout, 0.5)
		encodeColorBlock(&texels, mask, tuning, false, dst)
	})
}

// encodeBC4Tuned runs the single-channel encoder with an explicit tuning.
func encodeBC4Tuned(s *Surface, ch Channel, tuning bc4Tuning) []byte {
	return encodeBlocks(s, 8, 1, func(bx, by int, dst []byte) {
		var values [16]float64
		s.gatherChannel(bx, by, ch, &values)
		encodeBC4Block(&values, tuning, dst)
	})
}

// psnrAgainstSurface measures a payload against the codes an uncompressed upload
// of the same surface would hold.
func psnrAgainstSurface(t *testing.T, s *Surface, transfer Transfer, f Format, payload []byte, channels ...Channel) float64 {
	t.Helper()
	reference, err := ReferenceCodes(s, transfer)
	if err != nil {
		t.Fatalf("ReferenceCodes: %v", err)
	}
	got, err := Decode(f, payload, s.Width, s.Height)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	value, err := PSNR8(reference, got, channels...)
	if err != nil {
		t.Fatalf("PSNR8: %v", err)
	}
	return value
}

// blockSSE returns the squared error of every block, in block order. The
// monotone tests need a per-block number, because a total can hide one block
// that got worse while another got better.
func blockSSE(t *testing.T, s *Surface, transfer Transfer, f Format, payload []byte, channels ...Channel) []float64 {
	t.Helper()
	reference, err := ReferenceCodes(s, transfer)
	if err != nil {
		t.Fatalf("ReferenceCodes: %v", err)
	}
	got, err := Decode(f, payload, s.Width, s.Height)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	across, down := BlocksAcross(s.Width), BlocksAcross(s.Height)
	out := make([]float64, across*down)
	for by := 0; by < down; by++ {
		for bx := 0; bx < across; bx++ {
			total := 0.0
			for row := 0; row < 4; row++ {
				y := by*4 + row
				if y >= s.Height {
					break
				}
				for col := 0; col < 4; col++ {
					x := bx*4 + col
					if x >= s.Width {
						break
					}
					base := (y*s.Width + x) * 4
					for _, ch := range channels {
						d := float64(reference[base+int(ch)]) - float64(got[base+int(ch)])
						total += d * d
					}
				}
			}
			out[by*across+bx] = total
		}
	}
	return out
}

// rgbChannels names the three colour channels, which is what a colour
// measurement must use. Including alpha would add a channel the opaque formats
// never store and would lift every number for nothing.
var rgbChannels = []Channel{ChannelR, ChannelG, ChannelB}
