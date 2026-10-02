package beachgen

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// wakeFoam paints alpha lace into a tiny shared ribbon texture. U fades both
// edges; V ages the trail, so the runtime needs only one transparent draw.
func wakeFoam() ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, 128, 64))
	noise := newNoise(Seed)
	for y := 0; y < 64; y++ {
		for x := 0; x < 128; x++ {
			u, v := float64(x)/127, float64(y)/63
			edge := math.Pow(math.Sin(u*math.Pi), .7)
			fade := math.Pow(1-v, 1.7)
			lace := .3 + .7*math.Abs(noise.fbm(u*35, v*20, 3))
			vein := math.Pow(math.Max(0, math.Cos(u*80+v*43+noise.value(u*12, v*7)*4)), 4)
			alpha := clamp(edge*fade*(lace*.7+vein*.3)*.9, 0, 1)
			img.SetNRGBA(x, y, color.NRGBA{R: 226, G: 242, B: 236, A: uint8(alpha * 255)})
		}
	}
	var out bytes.Buffer
	err := png.Encode(&out, img)
	return out.Bytes(), err
}
