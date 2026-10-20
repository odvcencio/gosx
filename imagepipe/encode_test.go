package imagepipe

import (
	"bytes"
	"errors"
	"image"
	"strings"
	"testing"
)

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

// TestEncodeWebPWithNoRegisteredEncoderNamesTheExtensionPoint covers gosx's
// excision of its built-in WebP encoder (no wazero wasm runtime, no purego
// FFI shim): Encode must not silently produce a different format, or link
// a WebP encoder by default -- it must fail with a message that names
// RegisterEncoder as the way to add one back.
func TestEncodeWebPWithNoRegisteredEncoderNamesTheExtensionPoint(t *testing.T) {
	src := newTestImage(50, 50)
	_, err := Encode(src, FormatWebP, EncodeOptions{})
	if err == nil {
		t.Fatal("expected an error encoding webp with no registered encoder")
	}
	if !strings.Contains(err.Error(), "RegisterEncoder") {
		t.Fatalf("expected the error to name the RegisterEncoder extension point, got %v", err)
	}
	if !strings.Contains(err.Error(), "webp") {
		t.Fatalf("expected the error to name the rejected format, got %v", err)
	}
}

// TestRegisterEncoderRoutesEncodeToTheRegisteredImplementation proves the
// registry is real: a format Encode does not build in reaches exactly the
// Encoder RegisterEncoder registered for it, with the same img and opts
// Encode itself was called with.
func TestRegisterEncoderRoutesEncodeToTheRegisteredImplementation(t *testing.T) {
	t.Cleanup(func() { UnregisterEncoder(FormatWebP) })

	var gotImg image.Image
	var gotOpts EncodeOptions
	stub := EncoderFunc(func(img image.Image, opts EncodeOptions) ([]byte, error) {
		gotImg = img
		gotOpts = opts
		return []byte("stub-encoded-bytes"), nil
	})
	if err := RegisterEncoder(FormatWebP, stub); err != nil {
		t.Fatalf("RegisterEncoder: %v", err)
	}
	if !EncoderRegistered(FormatWebP) {
		t.Fatal("EncoderRegistered(FormatWebP) = false after RegisterEncoder")
	}

	src := newTestImage(64, 48)
	data, err := Encode(src, FormatWebP, EncodeOptions{Quality: 42})
	if err != nil {
		t.Fatalf("Encode with a registered webp encoder: %v", err)
	}
	if string(data) != "stub-encoded-bytes" {
		t.Fatalf("Encode returned %q, want the registered encoder's own bytes", data)
	}
	if gotImg != src {
		t.Fatal("the registered encoder did not receive Encode's own img")
	}
	if gotOpts.Quality != 42 {
		t.Fatalf("the registered encoder received Quality=%d, want 42", gotOpts.Quality)
	}
}

// TestRegisterEncoderRejectsBuiltInFormats proves a caller cannot shadow
// Encode's own JPEG/PNG paths through the registry -- both are, and stay,
// built in.
func TestRegisterEncoderRejectsBuiltInFormats(t *testing.T) {
	for _, format := range []Format{FormatJPEG, FormatPNG} {
		if err := RegisterEncoder(format, EncoderFunc(func(image.Image, EncodeOptions) ([]byte, error) {
			return nil, errors.New("should never run")
		})); err == nil {
			t.Errorf("RegisterEncoder(%q, ...) = nil error, want a rejection", format)
		}
	}
}

// TestRegisterEncoderRejectsNilEncoder proves a nil registration cannot
// silently produce a panic deep inside a later Encode call.
func TestRegisterEncoderRejectsNilEncoder(t *testing.T) {
	if err := RegisterEncoder(FormatWebP, nil); err == nil {
		t.Fatal("RegisterEncoder with a nil Encoder = nil error, want a rejection")
	}
}

// TestUnregisterEncoderRemovesARegistration proves UnregisterEncoder is a
// real, working inverse of RegisterEncoder: Encode fails again afterward,
// and a second call (nothing registered) is a harmless no-op.
func TestUnregisterEncoderRemovesARegistration(t *testing.T) {
	if err := RegisterEncoder(FormatWebP, EncoderFunc(func(image.Image, EncodeOptions) ([]byte, error) {
		return []byte("stub"), nil
	})); err != nil {
		t.Fatalf("RegisterEncoder: %v", err)
	}
	if !EncoderRegistered(FormatWebP) {
		t.Fatal("EncoderRegistered(FormatWebP) = false right after RegisterEncoder")
	}

	UnregisterEncoder(FormatWebP)
	if EncoderRegistered(FormatWebP) {
		t.Fatal("EncoderRegistered(FormatWebP) = true after UnregisterEncoder")
	}
	if _, err := Encode(newTestImage(10, 10), FormatWebP, EncodeOptions{}); err == nil {
		t.Fatal("expected Encode to fail again after UnregisterEncoder")
	}

	UnregisterEncoder(FormatWebP) // a second call with nothing registered must not panic
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
