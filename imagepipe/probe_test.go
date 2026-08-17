package imagepipe

import "testing"

func TestProbeReportsIntrinsicDimensionsAndFormat(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name       string
		path       string
		wantFormat string
	}{
		{"png", writeTestPNG(t, dir, "png-src", 1200, 800), "png"},
		{"jpeg", writeTestJPEG(t, dir, "jpeg-src", 640, 480), "jpeg"},
		{"webp", writeTestWebP(t, dir, "webp-src", 320, 240), "webp"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dims, format, err := Probe(test.path)
			if err != nil {
				t.Fatalf("Probe(%s): %v", test.path, err)
			}
			if format != test.wantFormat {
				t.Errorf("format = %q, want %q", format, test.wantFormat)
			}
			if dims.Width <= 0 || dims.Height <= 0 {
				t.Errorf("dims = %+v, want positive width/height", dims)
			}
		})
	}
}

func TestProbeReportsExactDimensions(t *testing.T) {
	dir := t.TempDir()
	path := writeTestPNG(t, dir, "exact", 1234, 567)

	dims, _, err := Probe(path)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if dims != (Dimensions{Width: 1234, Height: 567}) {
		t.Fatalf("dims = %+v, want {1234 567}", dims)
	}
}

func TestProbeDoesNotDecodePixels(t *testing.T) {
	// A malformed body after a valid header should still probe successfully
	// -- image.DecodeConfig only reads the header -- even though a full
	// Decode of the same bytes would fail.
	dir := t.TempDir()
	full := writeTestPNG(t, dir, "truncatable", 400, 300)
	data := readFileT(t, full)

	// Truncate well past the PNG header/IHDR chunk but before all pixel
	// data; DecodeConfig only needs the header.
	truncated := data[:64]
	truncPath := full + ".trunc"
	writeFileT(t, truncPath, truncated)

	if _, _, err := Probe(truncPath); err != nil {
		t.Fatalf("Probe on truncated body: %v (want header-only success)", err)
	}
	if _, _, err := Decode(truncPath); err == nil {
		t.Fatal("Decode on truncated body unexpectedly succeeded; test fixture no longer proves Probe is header-only")
	}
}

func TestProbeErrorsOnUnreadableFile(t *testing.T) {
	if _, _, err := Probe("/nonexistent/path/does-not-exist.png"); err == nil {
		t.Fatal("expected error for a nonexistent file")
	}
}

func TestProbeErrorsOnUnrecognizedFormat(t *testing.T) {
	dir := t.TempDir()
	path := writeFileT(t, dir+"/not-an-image.png", []byte("this is not image data"))
	if _, _, err := Probe(path); err == nil {
		t.Fatal("expected error for unrecognized image data")
	}
}

func TestDecodeReturnsFullDecodedImage(t *testing.T) {
	dir := t.TempDir()
	path := writeTestPNG(t, dir, "decode-me", 300, 200)

	img, format, err := Decode(path)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if format != "png" {
		t.Fatalf("format = %q, want png", format)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 300 || bounds.Dy() != 200 {
		t.Fatalf("decoded bounds = %v, want 300x200", bounds)
	}
}
