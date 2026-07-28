package bcn

import "math"

// srgbToLinear applies the inverse sRGB transfer function to one channel in the
// range 0 to 1. The constants come from IEC 61966-2-1.
//
// The function repeats texture.srgbToLinear because package texture calls this
// package and Go forbids the cycle. TestTransferMatchesReference checks the two
// agree on every 8-bit code.
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

// srgbDecodeLUT holds the exact linear value of every 8-bit sRGB code.
var srgbDecodeLUT = func() [256]float32 {
	var lut [256]float32
	for i := range lut {
		lut[i] = float32(srgbToLinear(float64(i) / 255))
	}
	return lut
}()

// srgbCodeEdge holds the linear value at which the nearest 8-bit sRGB code
// steps from k to k+1. A binary search over the table returns the same code as
// a call to linearToSRGB followed by rounding, and it costs eight comparisons
// instead of one call to math.Pow.
//
// The speed matters. A 4096 by 4096 colour texture holds 50 million colour
// samples, and one math.Pow for each of them costs more than the whole block
// search.
var srgbCodeEdge = func() [255]float64 {
	var edge [255]float64
	for k := range edge {
		edge[k] = srgbToLinear((float64(k) + 0.5) / 255)
	}
	return edge
}()

// encodeSRGB8 converts one linear value to the nearest 8-bit sRGB code. It
// clamps out-of-range input, which a Lanczos resample can produce through its
// negative lobes.
func encodeSRGB8(v float32) uint8 {
	l := float64(v)
	if l <= srgbCodeEdge[0] {
		return 0
	}
	if l >= srgbCodeEdge[254] {
		return 255
	}
	// Find the count of edges at or below l. A tie takes the higher code,
	// which matches the round-half-up rule of texture.LinearToSRGB8.
	lo, hi := 0, 255
	for lo < hi {
		mid := (lo + hi) / 2
		if l < srgbCodeEdge[mid] {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return uint8(lo)
}

// encodeUnorm8 converts one linear value to an 8-bit unorm code with no
// transfer function. Data channels take this path.
func encodeUnorm8(v float32) uint8 {
	scaled := float64(v)*255 + 0.5
	if scaled <= 0 {
		return 0
	}
	if scaled >= 255 {
		return 255
	}
	return uint8(scaled)
}

// encode applies the transfer function of t to one colour channel.
func (t Transfer) encode(v float32) uint8 {
	if t == TransferSRGB {
		return encodeSRGB8(v)
	}
	return encodeUnorm8(v)
}

// decode returns the linear value of one stored code under t.
func (t Transfer) decode(code uint8) float32 {
	if t == TransferSRGB {
		return srgbDecodeLUT[code]
	}
	return float32(code) / 255
}

// valid reports whether the caller named a legal transfer function.
func (t Transfer) valid() bool {
	return t == TransferSRGB || t == TransferUnorm
}
