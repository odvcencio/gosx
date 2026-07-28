package bcn

import (
	"math"
	"testing"
)

// referenceLinearToSRGB8 is a literal transcription of the sRGB transfer function
// of IEC 61966-2-1, followed by the round-half-up rule that texture.LinearToSRGB8
// uses. The package cannot import package texture, because package texture calls
// this package, so the reference lives here.
func referenceLinearToSRGB8(l float64) uint8 {
	if l < 0 {
		l = 0
	}
	if l > 1 {
		l = 1
	}
	var encoded float64
	if l <= 0.0031308 {
		encoded = l * 12.92
	} else {
		encoded = 1.055*math.Pow(l, 1.0/2.4) - 0.055
	}
	scaled := encoded*255 + 0.5
	if scaled <= 0 {
		return 0
	}
	if scaled >= 255 {
		return 255
	}
	return uint8(scaled)
}

// TestTransferMatchesReference checks the binary search in transfer.go returns the
// same code as the transfer function it replaces.
//
// The search exists for speed: a 4096 by 4096 colour texture holds 50 million
// colour samples, and one call to math.Pow for each of them would cost more than
// the whole block search. Speed is worth nothing if the answer moves, so the test
// walks a dense sample of the range and both ends of it.
func TestTransferMatchesReference(t *testing.T) {
	// The reference takes the float32 value the encoder receives, not the
	// float64 the loop computes. A sample can otherwise land on the far side of
	// a code boundary from its own float32 rounding, and the test would report
	// a difference the encoder cannot see.
	const steps = 200000
	for i := 0; i <= steps; i++ {
		value := float32(float64(i) / steps)
		want := referenceLinearToSRGB8(float64(value))
		if got := encodeSRGB8(value); got != want {
			t.Fatalf("linear %.9f: got code %d, want %d", value, got, want)
		}
	}
	for _, value := range []float32{-1, -1e-9, 0, 1, 1 + 1e-6, 2, float32(math.Inf(1))} {
		want := referenceLinearToSRGB8(float64(value))
		if got := encodeSRGB8(value); got != want {
			t.Fatalf("linear %v: got code %d, want %d", value, got, want)
		}
	}
}

// TestTransferRoundTripsEveryCode checks a code survives the trip to linear light
// and back.
//
// The property matters because every test image in this package is built from
// codes. If the round trip lost a code, the measurements would report the loss of
// the helper instead of the loss of the encoder.
func TestTransferRoundTripsEveryCode(t *testing.T) {
	for code := 0; code < 256; code++ {
		linear := srgbDecodeLUT[code]
		if got := encodeSRGB8(linear); got != uint8(code) {
			t.Fatalf("sRGB code %d became %d", code, got)
		}
		if got := encodeUnorm8(float32(code) / 255); got != uint8(code) {
			t.Fatalf("unorm code %d became %d", code, got)
		}
	}
}

// TestSRGBDecodeTableIsExact checks the decode table against the inverse transfer
// function of IEC 61966-2-1.
func TestSRGBDecodeTableIsExact(t *testing.T) {
	for code := 0; code < 256; code++ {
		want := srgbToLinear(float64(code) / 255)
		got := float64(srgbDecodeLUT[code])
		if diff := got - want; diff > 1e-7 || diff < -1e-7 {
			t.Fatalf("code %d decodes to %.9f, want %.9f", code, got, want)
		}
	}
}

// TestTransferChoiceChangesTheCodes proves the two transfer functions are not
// interchangeable.
//
// A mid grey shows the gap best: linear 0.2158 is sRGB code 128, and unorm code
// 55. A caller who names the wrong one moves every colour by that much.
func TestTransferChoiceChangesTheCodes(t *testing.T) {
	linear := srgbDecodeLUT[128]
	if got := TransferSRGB.encode(linear); got != 128 {
		t.Fatalf("the sRGB transfer gave code %d, want 128", got)
	}
	unorm := TransferUnorm.encode(linear)
	if unorm >= 128 {
		t.Fatalf("the unorm transfer gave code %d, which must be far below 128", unorm)
	}
	t.Logf("linear %.4f stores as sRGB code 128 and as unorm code %d", linear, unorm)
	if !TransferSRGB.valid() || !TransferUnorm.valid() {
		t.Fatal("both named transfer functions must be valid")
	}
	if TransferUnspecified.valid() {
		t.Fatal("the zero value must not be valid")
	}
}
