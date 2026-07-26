package texture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"testing"
)

// srgbPNG encodes a straight-alpha PNG from a pixel function.
func srgbPNG(t *testing.T, width, height int, at func(x, y int) color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, at(x, y))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestDecodeAppliesTheSRGBTransfer checks that an sRGB source arrives linear.
func TestDecodeAppliesTheSRGBTransfer(t *testing.T) {
	data := srgbPNG(t, 2, 1, func(x, y int) color.NRGBA {
		if x == 0 {
			return color.NRGBA{R: 128, G: 128, B: 128, A: 255}
		}
		return color.NRGBA{R: 255, G: 255, B: 255, A: 128}
	})
	img, info, err := Decode(data, SRGB)
	if err != nil {
		t.Fatal(err)
	}
	if info.Format != "png" || info.Width != 2 || info.Height != 1 || info.Bytes != len(data) {
		t.Fatalf("unexpected source info: %+v", info)
	}
	r, _, _, a := img.At(0, 0)
	if math.Abs(float64(r)-float64(SRGBToLinear(128))) > 1e-6 {
		t.Fatalf("code 128 decoded to %g, want %g", r, SRGBToLinear(128))
	}
	if a != 1 {
		t.Fatalf("opaque alpha decoded to %g", a)
	}
	// Alpha must NOT pass through the transfer function.
	_, _, _, a2 := img.At(1, 0)
	if math.Abs(float64(a2)-128.0/255) > 1e-6 {
		t.Fatalf("alpha 128 decoded to %g, want %g", a2, 128.0/255)
	}
}

// TestDecodeLinearSkipsTheTransfer checks a data texture stays byte-for-byte.
func TestDecodeLinearSkipsTheTransfer(t *testing.T) {
	data := srgbPNG(t, 1, 1, func(int, int) color.NRGBA {
		return color.NRGBA{R: 128, G: 64, B: 32, A: 255}
	})
	img, _, err := Decode(data, Linear)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(0, 0)
	for i, pair := range [][2]float64{{float64(r), 128.0 / 255}, {float64(g), 64.0 / 255}, {float64(b), 32.0 / 255}} {
		if math.Abs(pair[0]-pair[1]) > 1e-6 {
			t.Fatalf("channel %d decoded to %g, want %g", i, pair[0], pair[1])
		}
	}
}

// TestDecodeRefusesAnUnsupportedSource pins the WebP and AVIF refusal.
//
// The standard library decodes neither, and the pipeline takes no third-party
// image decoder. The refusal must be a named error the executor can turn into a
// skip, not a panic and not a silent black texture.
func TestDecodeRefusesAnUnsupportedSource(t *testing.T) {
	// A real WebP header: RIFF, size, WEBP, VP8 chunk.
	webp := []byte("RIFF\x24\x00\x00\x00WEBPVP8 \x18\x00\x00\x00")
	if _, _, err := Decode(webp, SRGB); !errors.Is(err, ErrUnsupportedSource) {
		t.Fatalf("WebP gave %v, want ErrUnsupportedSource", err)
	}
	if _, _, err := Decode(nil, SRGB); !errors.Is(err, ErrUnsupportedSource) {
		t.Fatalf("empty data gave %v, want ErrUnsupportedSource", err)
	}
}

// TestDecodeRefusesTooManyPixelsFromTheHeader checks that the bound applies
// before any allocation.
//
// The input is a PNG signature plus one IHDR chunk declaring 100000 by 100000
// pixels, and nothing else. DecodeConfig reads only the header, so the bound
// rejects the file without ever asking for ten billion pixels of memory.
func TestDecodeRefusesTooManyPixelsFromTheHeader(t *testing.T) {
	data := pngHeaderOnly(100000, 100000)
	_, _, err := Decode(data, SRGB)
	if !errors.Is(err, ErrTooManyPixels) {
		t.Fatalf("oversize header gave %v, want ErrTooManyPixels", err)
	}
}

// pngHeaderOnly builds a PNG signature and a valid IHDR chunk, and stops.
func pngHeaderOnly(width, height uint32) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	var ihdr bytes.Buffer
	ihdr.WriteString("IHDR")
	binary.Write(&ihdr, binary.BigEndian, width)
	binary.Write(&ihdr, binary.BigEndian, height)
	ihdr.Write([]byte{8, 6, 0, 0, 0}) // 8-bit, truecolour with alpha.
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(ihdr.Len()-4))
	buf.Write(length[:])
	buf.Write(ihdr.Bytes())
	var sum [4]byte
	binary.BigEndian.PutUint32(sum[:], crc32.ChecksumIEEE(ihdr.Bytes()))
	buf.Write(sum[:])
	return buf.Bytes()
}

// TestDecodeReads16BitSources checks the wide path keeps its precision.
func TestDecodeReads16BitSources(t *testing.T) {
	img := image.NewNRGBA64(image.Rect(0, 0, 1, 1))
	img.SetNRGBA64(0, 0, color.NRGBA64{R: 0x0101, G: 0x8080, B: 0xFFFF, A: 0xFFFF})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	decoded, info, err := Decode(buf.Bytes(), Linear)
	if err != nil {
		t.Fatal(err)
	}
	if info.BitDepth != 16 {
		t.Fatalf("bit depth %d, want 16", info.BitDepth)
	}
	r, g, b, _ := decoded.At(0, 0)
	if math.Abs(float64(r)-float64(0x0101)/65535) > 1e-6 {
		t.Errorf("red %g, want %g", r, float64(0x0101)/65535)
	}
	if math.Abs(float64(g)-float64(0x8080)/65535) > 1e-6 {
		t.Errorf("green %g, want %g", g, float64(0x8080)/65535)
	}
	if math.Abs(float64(b)-1) > 1e-6 {
		t.Errorf("blue %g, want 1", b)
	}
}

// TestDecodeReadsJPEG checks the YCbCr path lands through image/draw.
func TestDecodeReadsJPEG(t *testing.T) {
	// image/jpeg is registered by the package. Build a JPEG through the
	// encoder so the test does not embed a binary blob.
	src := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	data := jpegBytes(t, src)
	img, info, err := Decode(data, SRGB)
	if err != nil {
		t.Fatal(err)
	}
	if info.Format != "jpeg" {
		t.Fatalf("format %q, want jpeg", info.Format)
	}
	r, _, _, a := img.At(4, 4)
	if a != 1 {
		t.Fatalf("JPEG alpha is %g, want 1", a)
	}
	// JPEG is lossy, so allow a few codes of drift.
	want := float64(SRGBToLinear(200))
	if math.Abs(float64(r)-want) > 0.02 {
		t.Fatalf("red %g, want about %g", r, want)
	}
}

// TestEncodeBytesChannelCounts checks the packing stride of each channel count.
func TestEncodeBytesChannelCounts(t *testing.T) {
	img := NewImage(2, 1)
	img.Set(0, 0, 1, 0.5, 0.25, 0.75)
	img.Set(1, 0, 0, 0, 0, 1)
	for channels := 1; channels <= 4; channels++ {
		out, err := EncodeBytes(img, Linear, channels)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != 2*channels {
			t.Fatalf("%d channels gave %d bytes, want %d", channels, len(out), 2*channels)
		}
		if out[0] != 255 {
			t.Fatalf("%d channels lost the red channel", channels)
		}
		if channels >= 4 && out[3] != 191 {
			t.Fatalf("alpha encoded to %d, want 191", out[3])
		}
	}
	if _, err := EncodeBytes(img, Linear, 5); !errors.Is(err, ErrShape) {
		t.Fatalf("five channels gave %v, want ErrShape", err)
	}
}

// jpegBytes encodes one image as JPEG for the decoder test.
func jpegBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
