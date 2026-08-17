package imagepipe

import (
	"bytes"
	"testing"

	xwebp "golang.org/x/image/webp"
)

func TestEncodeWebPProducesDecodableOutput(t *testing.T) {
	src := newTestImage(320, 200)
	data, err := Encode(src, FormatWebP, EncodeOptions{})
	if err != nil {
		t.Fatalf("Encode webp: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Encode webp returned no bytes")
	}
	if !bytes.HasPrefix(data, []byte("RIFF")) {
		t.Fatalf("webp output missing RIFF header: % x", data[:min(16, len(data))])
	}
	decoded, err := xwebp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode our own webp output: %v", err)
	}
	if decoded.Bounds().Dx() != 320 || decoded.Bounds().Dy() != 200 {
		t.Fatalf("decoded bounds = %v, want 320x200", decoded.Bounds())
	}
}

func TestEncodeJPEGProducesDecodableOutput(t *testing.T) {
	src := newTestImage(200, 150)
	data, err := Encode(src, FormatJPEG, EncodeOptions{})
	if err != nil {
		t.Fatalf("Encode jpeg: %v", err)
	}
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		t.Fatalf("jpeg output missing SOI marker: % x", data[:min(4, len(data))])
	}
}

func TestEncodePNGProducesDecodableOutput(t *testing.T) {
	src := newTestImage(200, 150)
	data, err := Encode(src, FormatPNG, EncodeOptions{})
	if err != nil {
		t.Fatalf("Encode png: %v", err)
	}
	pngMagic := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if !bytes.HasPrefix(data, pngMagic) {
		t.Fatalf("png output missing PNG signature: % x", data[:min(8, len(data))])
	}
}

func TestEncodeRejectsUnknownFormat(t *testing.T) {
	src := newTestImage(50, 50)
	if _, err := Encode(src, Format("avif"), EncodeOptions{}); err == nil {
		t.Fatal("expected an error for an unsupported format")
	}
}

func TestEncodeWebPHigherQualityProducesNoSmallerOutput(t *testing.T) {
	// A meaningful, non-tautological quality check: raising quality on a
	// lossy codec should never require *fewer* bytes than a lower quality
	// pass over the very same source pixels.
	src := newTestImage(640, 480)
	low, err := Encode(src, FormatWebP, EncodeOptions{Quality: 10})
	if err != nil {
		t.Fatalf("Encode low quality: %v", err)
	}
	high, err := Encode(src, FormatWebP, EncodeOptions{Quality: 95})
	if err != nil {
		t.Fatalf("Encode high quality: %v", err)
	}
	if len(high) < len(low) {
		t.Fatalf("high quality output (%d bytes) is smaller than low quality output (%d bytes)", len(high), len(low))
	}
}

func TestFormatExt(t *testing.T) {
	tests := map[Format]string{
		FormatWebP:      ".webp",
		FormatJPEG:      ".jpg",
		FormatPNG:       ".png",
		Format("bogus"): "",
	}
	for format, want := range tests {
		if got := format.Ext(); got != want {
			t.Errorf("%q.Ext() = %q, want %q", format, got, want)
		}
	}
}
