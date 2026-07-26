package texture

// AlphaStats records what an image does with its alpha channel.
type AlphaStats struct {
	// Constant is true when every texel shares one alpha value.
	Constant bool `json:"constant"`
	// Value holds that shared value when Constant is true.
	Value float32 `json:"value,omitempty"`
	Min   float32 `json:"min"`
	Max   float32 `json:"max"`
	// Binary is true when every texel is fully opaque or fully clear. A
	// binary channel needs a cutoff, not a blend, so the renderer can keep
	// the opaque pipeline and skip depth sorting.
	Binary bool `json:"binary"`
	// Opaque is true when every texel is fully opaque. An opaque channel
	// carries no information, so the encoder drops it.
	Opaque bool `json:"opaque"`
}

// Mode names the glTF alpha mode the statistics imply.
func (s AlphaStats) Mode() string {
	switch {
	case s.Opaque:
		return "opaque"
	case s.Binary:
		return "mask"
	default:
		return "blend"
	}
}

// AnalyzeAlpha measures the alpha channel of a straight-alpha image.
//
// The result decides two things. An opaque channel drops out of the encoded
// payload, which removes one byte per texel. A non-constant channel forces the
// mip builder to premultiply before it filters.
func AnalyzeAlpha(img *Image) AlphaStats {
	if img == nil || len(img.Pix) == 0 {
		return AlphaStats{Constant: true, Value: 1, Min: 1, Max: 1, Binary: true, Opaque: true}
	}
	minA, maxA := img.Pix[3], img.Pix[3]
	binary := true
	const epsilon = 0.5 / 255
	for i := 3; i < len(img.Pix); i += 4 {
		a := img.Pix[i]
		if a < minA {
			minA = a
		}
		if a > maxA {
			maxA = a
		}
		if a > epsilon && a < 1-epsilon {
			binary = false
		}
	}
	constant := maxA-minA <= epsilon
	stats := AlphaStats{
		Constant: constant,
		Min:      minA,
		Max:      maxA,
		Binary:   binary,
		Opaque:   minA >= 1-epsilon,
	}
	if constant {
		stats.Value = minA
	}
	return stats
}

// IsGrayscale reports whether every texel has equal red, green, and blue.
//
// A grayscale mask, an ambient occlusion map, and a height map all arrive as
// three identical channels from an authoring tool. One channel carries the
// same information at a quarter of the bytes.
func IsGrayscale(img *Image) bool {
	if img == nil {
		return false
	}
	const epsilon = 0.5 / 255
	for i := 0; i+3 < len(img.Pix); i += 4 {
		r, g, b := img.Pix[i], img.Pix[i+1], img.Pix[i+2]
		if absF(r-g) > epsilon || absF(r-b) > epsilon {
			return false
		}
	}
	return true
}

func absF(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// Premultiply multiplies the colour channels by alpha in place and records the
// new mode. Calling it on an already premultiplied image does nothing.
//
// A filter must run on premultiplied values when alpha has structure. Averaging
// straight colour across an alpha edge pulls the colour of invisible texels
// into visible ones, which shows up as a dark or bright halo at every mip
// level below zero.
func Premultiply(img *Image) {
	if img == nil || img.Alpha == AlphaPremultiplied {
		return
	}
	for i := 0; i+3 < len(img.Pix); i += 4 {
		a := img.Pix[i+3]
		img.Pix[i] *= a
		img.Pix[i+1] *= a
		img.Pix[i+2] *= a
	}
	img.Alpha = AlphaPremultiplied
}

// Unpremultiply divides the colour channels by alpha in place and records the
// new mode. A zero-alpha texel keeps its colour, because dividing by zero has
// no answer and zero-alpha colour is invisible either way.
func Unpremultiply(img *Image) {
	if img == nil || img.Alpha == AlphaStraight {
		return
	}
	for i := 0; i+3 < len(img.Pix); i += 4 {
		a := img.Pix[i+3]
		if a <= 0 {
			continue
		}
		inv := 1 / a
		img.Pix[i] *= inv
		img.Pix[i+1] *= inv
		img.Pix[i+2] *= inv
	}
	img.Alpha = AlphaStraight
}
