package hdrimage

import (
	"bytes"
	"fmt"
	"math"
	"testing"
)

// floatToRGBE encodes one linear colour as a Radiance RGBE quad.
func floatToRGBE(r, g, b float64) [4]byte {
	peak := math.Max(r, math.Max(g, b))
	if peak < 1e-32 {
		return [4]byte{0, 0, 0, 0}
	}
	mantissa, exponent := math.Frexp(peak)
	scale := mantissa * 256 / peak
	return [4]byte{
		byte(r * scale),
		byte(g * scale),
		byte(b * scale),
		byte(exponent + 128),
	}
}

// writeRadianceRLE builds a run-length encoded Radiance file.
func writeRadianceRLE(width, height int, pixel func(x, y int) (float64, float64, float64)) []byte {
	var buf bytes.Buffer
	buf.WriteString("#?RADIANCE\n")
	buf.WriteString("FORMAT=32-bit_rle_rgbe\n")
	buf.WriteString("\n")
	fmt.Fprintf(&buf, "-Y %d +X %d\n", height, width)
	for y := 0; y < height; y++ {
		buf.Write([]byte{2, 2, byte(width >> 8), byte(width & 0xFF)})
		components := make([][]byte, 4)
		for i := range components {
			components[i] = make([]byte, width)
		}
		for x := 0; x < width; x++ {
			r, g, b := pixel(x, y)
			quad := floatToRGBE(r, g, b)
			for i := 0; i < 4; i++ {
				components[i][x] = quad[i]
			}
		}
		for _, component := range components {
			// Write each component as literal runs of at most 128 bytes.
			for x := 0; x < width; {
				run := width - x
				if run > 128 {
					run = 128
				}
				buf.WriteByte(byte(run))
				buf.Write(component[x : x+run])
				x += run
			}
		}
	}
	return buf.Bytes()
}

// writeRadianceFlat builds the old uncompressed Radiance layout.
func writeRadianceFlat(width, height int, pixel func(x, y int) (float64, float64, float64)) []byte {
	var buf bytes.Buffer
	buf.WriteString("#?RADIANCE\n\n")
	fmt.Fprintf(&buf, "-Y %d +X %d\n", height, width)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b := pixel(x, y)
			quad := floatToRGBE(r, g, b)
			buf.Write(quad[:])
		}
	}
	return buf.Bytes()
}

func rampPixel(x, y int) (float64, float64, float64) {
	return float64(x+1) * 0.5, float64(y+1) * 0.25, 1
}

func TestDecodeRadianceRLE(t *testing.T) {
	const width, height = 16, 4
	data := writeRadianceRLE(width, height, rampPixel)
	img, err := Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if img.Width != width || img.Height != height || img.Source != "radiance" {
		t.Fatalf("unexpected image: %dx%d source=%q", img.Width, img.Height, img.Source)
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			wantR, wantG, wantB := rampPixel(x, y)
			r, g, b := img.At(x, y)
			// RGBE keeps eight bits of mantissa, so allow one percent.
			if relative(float64(r), wantR) > 0.01 || relative(float64(g), wantG) > 0.01 || relative(float64(b), wantB) > 0.01 {
				t.Fatalf("pixel (%d,%d) = (%v,%v,%v), want (%v,%v,%v)", x, y, r, g, b, wantR, wantG, wantB)
			}
		}
	}
}

func TestDecodeRadianceFlat(t *testing.T) {
	const width, height = 6, 3
	data := writeRadianceFlat(width, height, rampPixel)
	img, err := Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if img.Width != width || img.Height != height {
		t.Fatalf("unexpected size %dx%d", img.Width, img.Height)
	}
	r, _, _ := img.At(5, 2)
	if relative(float64(r), 3.0) > 0.01 {
		t.Fatalf("last pixel red = %v, want about 3", r)
	}
}

func TestDecodeRadianceRepeatMarker(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("#?RADIANCE\n\n-Y 1 +X 4\n")
	quad := floatToRGBE(2, 2, 2)
	buf.Write(quad[:])
	buf.Write([]byte{1, 1, 1, 3}) // Repeat the previous pixel three times.
	img, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	for x := 0; x < 4; x++ {
		r, _, _ := img.At(x, 0)
		if relative(float64(r), 2) > 0.01 {
			t.Fatalf("pixel %d red = %v, want 2", x, r)
		}
	}
}

func TestDecodeRadianceRejectsUnknownOrientation(t *testing.T) {
	data := []byte("#?RADIANCE\n\n+Y 2 +X 2\n")
	if _, err := Decode(data); err == nil {
		t.Fatal("expected an orientation error")
	}
}

func TestDecodeRejectsUnknownMagic(t *testing.T) {
	if _, err := Decode([]byte("not an image")); err == nil {
		t.Fatal("expected a format error")
	}
}

func TestHalfConversionRoundTrip(t *testing.T) {
	cases := []float32{0, 1, -1, 0.5, 2048, 65504, 6e-8, 1e-5, 3.5}
	for _, want := range cases {
		got := HalfToFloat32(Float32ToHalf(want))
		if math.Abs(float64(got-want)) > 1e-3*math.Max(1, math.Abs(float64(want))) {
			t.Fatalf("round trip of %v gave %v", want, got)
		}
	}
	if got := Float32ToHalf(1e9); got != 0x7C00 {
		t.Fatalf("overflow gave %#x, want 0x7C00", got)
	}
	if got := HalfToFloat32(0x3C00); got != 1 {
		t.Fatalf("half 1.0 decoded as %v", got)
	}
}

func relative(got, want float64) float64 {
	if want == 0 {
		return math.Abs(got)
	}
	return math.Abs(got-want) / math.Abs(want)
}
