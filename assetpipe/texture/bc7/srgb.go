package bc7

import "math"

// This file duplicates the transfer function of assetpipe/texture. The
// duplication is deliberate: texture calls this package, so this package cannot
// call texture without a cycle. srgb_test.go pins both copies to the same
// IEC 61966-2-1 constants and to the same rounding rule, so a drift fails a
// test rather than darkening every texture.

// ColorSpace records how the stored 8-bit endpoint codes encode colour.
//
// The zero value is not a valid choice, and Encode rejects it. A default here
// would be a guess, and the wrong guess is invisible until a texture ships
// looking dark.
type ColorSpace int

const (
	// spaceUnset is the zero value. Encode refuses it.
	spaceUnset ColorSpace = iota
	// Linear stores the linear value directly as an 8-bit unorm code. Choose
	// it for a data map: a normal map, a roughness map, a metalness map, an
	// ambient occlusion map, or a height map. Pair it with
	// VK_FORMAT_BC7_UNORM_BLOCK.
	Linear
	// SRGB stores the value through the sRGB transfer function. Choose it for
	// a colour map: a base colour map or an emissive map. Pair it with
	// VK_FORMAT_BC7_SRGB_BLOCK, which makes the GPU invert the same curve on
	// the sampler side.
	SRGB
)

// String names the colour space for a manifest or a metric.
func (c ColorSpace) String() string {
	switch c {
	case SRGB:
		return "srgb"
	case Linear:
		return "linear"
	}
	return "unset"
}

// Vulkan block format numbers for BC7, from the VkFormat enumeration.
const (
	// VkFormatBC7UnormBlock is VK_FORMAT_BC7_UNORM_BLOCK.
	VkFormatBC7UnormBlock = 145
	// VkFormatBC7SRGBBlock is VK_FORMAT_BC7_SRGB_BLOCK.
	VkFormatBC7SRGBBlock = 146
)

// VkFormat returns the BC7 VkFormat that matches the colour space. The sampler
// must invert exactly the curve the encoder applied, so the two must agree.
func (c ColorSpace) VkFormat() int {
	if c == SRGB {
		return VkFormatBC7SRGBBlock
	}
	return VkFormatBC7UnormBlock
}

// srgbDecodeLUT holds the exact linear value of every 8-bit sRGB code. The
// input domain has 256 members, so a table is exact and removes 256 possible
// math.Pow calls per texel.
var srgbDecodeLUT = func() [256]float32 {
	var lut [256]float32
	for i := range lut {
		lut[i] = float32(srgbToLinear(float64(i) / 255))
	}
	return lut
}()

// srgbToLinear applies the inverse sRGB transfer function to one channel in the
// range 0 to 1. The constants come from IEC 61966-2-1.
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

// linearToSRGB8 converts one linear value to an 8-bit sRGB code. It clamps
// out-of-range input, which a Lanczos resample produces through its negative
// lobes.
func linearToSRGB8(l float32) uint8 {
	return quantize8(linearToSRGB(clamp01(float64(l))))
}

// linearToUnorm8 converts one linear value to an 8-bit unorm code, with no
// transfer function. Data channels and alpha take this path.
func linearToUnorm8(l float32) uint8 { return quantize8(clamp01(float64(l))) }

// quantize8 rounds a 0 to 1 value to the nearest 8-bit code.
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

// encodeTexel converts one linear RGBA pixel to the 8-bit codes BC7 stores.
//
// Only the three colour channels take the transfer function. The sRGB
// specification leaves alpha linear, and so does the Basic Data Format
// Descriptor that a KTX2 writer emits for an sRGB block format.
func encodeTexel(space ColorSpace, r, g, b, a float32) [4]uint8 {
	if space == SRGB {
		return [4]uint8{linearToSRGB8(r), linearToSRGB8(g), linearToSRGB8(b), linearToUnorm8(a)}
	}
	return [4]uint8{linearToUnorm8(r), linearToUnorm8(g), linearToUnorm8(b), linearToUnorm8(a)}
}

// decodeTexel converts stored 8-bit codes back to linear light. Decode uses it,
// and so does every quality measurement that reports in linear space.
func decodeTexel(space ColorSpace, px [4]uint8) [4]float32 {
	if space == SRGB {
		return [4]float32{
			srgbDecodeLUT[px[0]],
			srgbDecodeLUT[px[1]],
			srgbDecodeLUT[px[2]],
			float32(px[3]) / 255,
		}
	}
	return [4]float32{
		float32(px[0]) / 255,
		float32(px[1]) / 255,
		float32(px[2]) / 255,
		float32(px[3]) / 255,
	}
}
