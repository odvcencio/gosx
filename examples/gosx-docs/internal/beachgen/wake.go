package beachgen

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// WakeFoam packs eight ages of the same porous foam puff vertically. Each
// cell fades at all four edges, so overlapping wake patches have no hard
// boundaries. Older cells retain the pattern while their coverage dissolves.
func WakeFoam() ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	noise := newNoise(Seed)
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			u, v := float64(x)/127, float64(y%16)/15
			warp := u + .06*math.Sin(v*8) + .025*noise.fbm(u*12, v*10, 3)
			body := math.Exp(-math.Pow((warp-.5)/(.21+.035*math.Sin(v*math.Pi)), 2))
			lace := clamp(.35+2.2*noise.fbm(u*52, v*20, 3), 0, 1)
			ridge := math.Pow(math.Max(0, math.Cos(v*32+noise.value(u*18, v*14)*5)), 3)
			alpha := body * math.Pow(math.Sin(u*math.Pi), .8) * math.Pow(math.Sin(v*math.Pi), .8)
			age := float64(y / 16)
			coverage := clamp(.18+.8*lace+.18*ridge-age*.095, 0, 1)
			alpha *= coverage * math.Pow(1-age/8, 1.15)
			img.SetNRGBA(x, y, color.NRGBA{R: 239, G: 245, B: 238, A: uint8(clamp(alpha, 0, 1) * 255)})
		}
	}
	var out bytes.Buffer
	err := png.Encode(&out, img)
	return out.Bytes(), err
}
