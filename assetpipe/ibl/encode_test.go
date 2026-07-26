package ibl

import (
	"math"
	"testing"

	"m31labs.dev/gosx/assetpipe/hdrimage"
	"m31labs.dev/gosx/render/bundle/ktx2"
)

func TestEncodeCubeKTX2RoundTrip(t *testing.T) {
	source := patternCube(8)
	chain := Prefilter(source, PrefilterOptions{Samples: 16, MipSelect: true})
	data, err := EncodeCubeKTX2(chain, map[string]string{"GoSXiblModel": BRDFModel})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	img, err := ktx2.Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if img.Faces != 6 || img.Width != 8 || len(img.Levels) != len(chain) {
		t.Fatalf("unexpected container: faces=%d width=%d levels=%d", img.Faces, img.Width, len(img.Levels))
	}
	decoded, err := DecodeCubeKTX2(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	for level := range chain {
		if decoded[level].Size != chain[level].Size {
			t.Fatalf("level %d size %d, want %d", level, decoded[level].Size, chain[level].Size)
		}
		for face := 0; face < 6; face++ {
			for i := range chain[level].Faces[face] {
				want := float64(chain[level].Faces[face][i])
				got := float64(decoded[level].Faces[face][i])
				// Half float carries about 11 bits of mantissa.
				if math.Abs(got-want) > 1e-3*math.Max(1, math.Abs(want)) {
					t.Fatalf("level %d face %d component %d: %v, want %v", level, face, i, got, want)
				}
			}
		}
	}
	kv, err := ktx2.KeyValues(data)
	if err != nil {
		t.Fatalf("key values: %v", err)
	}
	if kv["GoSXiblModel"] != BRDFModel {
		t.Fatalf("model key = %q, want %q", kv["GoSXiblModel"], BRDFModel)
	}
}

func TestEncodeBRDFLUTKTX2RoundTrip(t *testing.T) {
	lut := GenerateBRDFLUT(16, 64)
	data, err := EncodeBRDFLUTKTX2(lut, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	img, err := ktx2.Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if img.Format != ktx2.VkFormatR16G16Sfloat || img.Width != 16 || img.Height != 16 {
		t.Fatalf("unexpected container: %+v", img)
	}
	if len(img.Levels[0].Bytes) != 16*16*4 {
		t.Fatalf("payload has %d bytes, want %d", len(img.Levels[0].Bytes), 16*16*4)
	}
}

func TestHalfRoundTrip(t *testing.T) {
	// Handled inside hdrimage, checked here because the IBL writer depends on
	// exact behaviour at the values a prefiltered map produces.
	values := []float32{0, 1, 0.5, -1, 65504, 1e-5, 3.14159}
	for _, want := range values {
		got := halfRoundTrip(want)
		if math.Abs(float64(got-want)) > 1e-3*math.Max(1, math.Abs(float64(want))) {
			t.Fatalf("half round trip of %v gave %v", want, got)
		}
	}
}

func halfRoundTrip(v float32) float32 {
	return hdrimage.HalfToFloat32(hdrimage.Float32ToHalf(v))
}
