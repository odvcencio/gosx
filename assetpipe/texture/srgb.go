package texture

import "math"

// ColorSpace records how a source image encodes its RGB channels.
//
// Choose Linear for data textures. A normal map, a roughness map, a metalness
// map, an ambient occlusion map, and a height map all store numbers, not
// colours, so their bytes are already linear and must not pass through the
// sRGB transfer function.
//
// Choose SRGB for colour textures. A base colour map and an emissive map hold
// display-referred values.
//
// The alpha channel is linear in both cases. The sRGB specification applies
// the transfer function to the three colour channels only.
type ColorSpace int

const (
	// Linear means the stored bytes are already linear light.
	Linear ColorSpace = iota
	// SRGB means the stored bytes carry the sRGB transfer function.
	SRGB
)

// String names the colour space for a manifest or a metric.
func (c ColorSpace) String() string {
	if c == SRGB {
		return "srgb"
	}
	return "linear"
}

// srgbDecodeLUT holds the exact linear value of every 8-bit sRGB code. A table
// removes 256 possible math.Pow calls per pixel and is exact, because the
// input domain has 256 members.
var srgbDecodeLUT = func() [256]float32 {
	var lut [256]float32
	for i := range lut {
		lut[i] = float32(srgbToLinear(float64(i) / 255))
	}
	return lut
}()

// srgbToLinear applies the inverse sRGB transfer function to one channel in
// the range 0 to 1. The constants come from IEC 61966-2-1.
func srgbToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// linearToSRGB applies the sRGB transfer function to one linear channel.
func linearToSRGB(l float64) float64 {
	if l <= 0.0031308 {
		return l * 12.92
	}
	return 1.055*math.Pow(l, 1.0/2.4) - 0.055
}

// SRGBToLinear converts one 8-bit sRGB code to linear light.
func SRGBToLinear(code uint8) float32 { return srgbDecodeLUT[code] }

// LinearToSRGB8 converts one linear value to an 8-bit sRGB code. The function
// clamps out-of-range input, which a Lanczos resample can produce through its
// negative lobes.
func LinearToSRGB8(l float32) uint8 {
	return quantize8(linearToSRGB(clamp01(float64(l))))
}

// LinearToUnorm8 converts one linear value to an 8-bit unorm code without a
// transfer function. Data channels take this path.
func LinearToUnorm8(l float32) uint8 { return quantize8(clamp01(float64(l))) }

// quantize8 rounds a 0-to-1 value to the nearest 8-bit code.
func quantize8(v float64) uint8 {
	scaled := v*255 + 0.5
	if scaled <= 0 {
		return 0
	}
	if scaled >= 255 {
		return 255
	}
	return uint8(scaled)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
