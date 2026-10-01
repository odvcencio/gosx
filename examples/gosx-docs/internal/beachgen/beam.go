package beachgen

import (
	"image"
	"image/color"
	"math"
)

// BeamTexture gives the two inexpensive light cones a soft transverse edge
// and an exponential falloff along their length, shared by their water glint.
func BeamTexture() ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, 128, 64))
	for y := 0; y < 64; y++ {
		v := float64(y) / 63
		for x := 0; x < 128; x++ {
			u := float64(x) / 127
			// Integrated path length through a round cone, with a soft rim.
			cross := math.Sqrt(math.Max(0, 1-math.Pow(2*v-1, 2)))
			cross *= smoothstep(0, .06, v) * (1 - smoothstep(.94, 1, v))
			a := cross * math.Exp(-2.8*u) * (1 - smoothstep(.65, 1, u))
			// The renderer decodes color maps from sRGB; encode the desired
			// linear radiance so it does not collapse into a bright thin core.
			img.SetNRGBA(x, y, color.NRGBA{R: beamChannel(a), G: beamChannel(a * .82), B: beamChannel(a * .55), A: uint8(a * 255)})
		}
	}
	return encodePNG(img)
}

func beamChannel(value float64) uint8 {
	if value <= .0031308 {
		value *= 12.92
	} else {
		value = 1.055*math.Pow(value, 1/2.4) - .055
	}
	return uint8(math.Round(clamp(value, 0, 1) * 255))
}
