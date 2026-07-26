// Package hdrimage decodes high dynamic range images into linear float32
// pixels. It reads Radiance RGBE (.hdr) files and the common scanline subset
// of OpenEXR (.exr) files. The package uses the standard library only.
package hdrimage

import "math"

// HalfToFloat32 converts an IEEE 754 binary16 value to float32.
func HalfToFloat32(h uint16) float32 {
	sign := uint32(h&0x8000) << 16
	exponent := uint32(h>>10) & 0x1F
	mantissa := uint32(h & 0x03FF)
	switch {
	case exponent == 0:
		if mantissa == 0 {
			return math.Float32frombits(sign)
		}
		// Subnormal binary16 values normalize into the float32 range.
		exponent = 1
		for mantissa&0x0400 == 0 {
			mantissa <<= 1
			exponent--
		}
		mantissa &= 0x03FF
		return math.Float32frombits(sign | (exponent+112)<<23 | mantissa<<13)
	case exponent == 31:
		return math.Float32frombits(sign | 0x7F800000 | mantissa<<13)
	default:
		return math.Float32frombits(sign | (exponent+112)<<23 | mantissa<<13)
	}
}

// Float32ToHalf converts a float32 value to IEEE 754 binary16. The function
// rounds to nearest even and clamps overflow to infinity.
func Float32ToHalf(f float32) uint16 {
	bits := math.Float32bits(f)
	sign := uint16(bits >> 16 & 0x8000)
	exponent := int32(bits>>23&0xFF) - 127
	mantissa := bits & 0x007FFFFF

	if exponent == 128 { // Infinity or NaN.
		if mantissa != 0 {
			return sign | 0x7E00 // Quiet NaN.
		}
		return sign | 0x7C00
	}
	if exponent > 15 { // Overflow becomes infinity.
		return sign | 0x7C00
	}
	if exponent < -24 { // Underflow becomes zero.
		return sign
	}
	if exponent < -14 { // Subnormal binary16 range.
		shift := uint32(-14 - exponent)
		mantissa |= 0x00800000
		half := mantissa >> (shift + 13)
		round := mantissa >> (shift + 12) & 1
		lower := mantissa & ((1 << (shift + 12)) - 1)
		if round == 1 && (lower != 0 || half&1 == 1) {
			half++
		}
		return sign | uint16(half)
	}
	half := uint32(exponent+15)<<10 | mantissa>>13
	round := mantissa >> 12 & 1
	lower := mantissa & 0x0FFF
	if round == 1 && (lower != 0 || half&1 == 1) {
		half++
	}
	return sign | uint16(half)
}
