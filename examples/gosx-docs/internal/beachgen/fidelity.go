package beachgen

import (
	"image"
	"image/color"
	"math"
)

// Spend the existing terrain vertex budget on the steep headland faces.
func terrainColumn(column int) float64 {
	breaks := []int{0, 12, 42, 94, 116, 128}
	xs := []float64{-60, -38, -24, 30, 44, 60}
	for i := 1; i < len(breaks); i++ {
		if column <= breaks[i] {
			return lerp(xs[i-1], xs[i], float64(column-breaks[i-1])/float64(breaks[i]-breaks[i-1]))
		}
	}
	return 60
}

func cliffLedges(t float64) float64 {
	return .18*smoothstep(.04, .22, t) + .27*smoothstep(.3, .5, t) + .55*smoothstep(.6, .96, t)
}

func shorelineHeight(z float64) float64 {
	return lerp(.12*z, .045*z, smoothstep(-4, 4, z))
}

// Fractures, salt and sparse lichen are baked, including on inexpensive tiers.
func basaltTint(n noiseField, x, y, z, top float64) [3]float64 {
	mottle := n.fbm(x*1.5+17, z*.9+y*.7, 3)
	fracture := math.Pow(1-math.Abs(n.value(x*3.3+y*.08, z*3.1)), 18)
	strata := math.Pow(.5+.5*math.Sin(y*9+n.value(x*.4, z*.4)), 8)
	shade := .88 + .22*mottle - .25*fracture - .12*strata
	wet := 1 - smoothstep(.15, 1.6, y)
	shade *= 1 - .45*wet
	salt := smoothstep(top*.72, top*.97, y) * smoothstep(-.1, .45, n.value(x*2, z*2))
	return [3]float64{(30 + 13*salt) * shade, (36 + 15*salt) * shade, (42 + 6*salt) * shade}
}

const rockAtlasTile = 128

func sandContact(x, z float64, boulders []boulderSpec) float64 {
	contact := 1 - smoothstep(.8, 1.4, math.Hypot(x-MonolithX, z-MonolithZ))
	for _, b := range boulders {
		contact = math.Max(contact, 1-smoothstep(b.radius*.65, b.radius*1.5, math.Hypot(x-b.x, z-b.z)))
	}
	return 1 - .35*contact
}

func rockAtlasUV(g *geometry, first, tile int, bottom, top float64) {
	for i := first; i < len(g.positions)/3; i++ {
		u, v := g.uvs[i*2], g.uvs[i*2+1]
		if top > bottom {
			v = (g.positions[i*3+1] - bottom) / (top - bottom)
		}
		// Inset beyond the byte UV quantization error to avoid atlas bleeding.
		g.uvs[i*2] = (float64(tile%5) + .025 + .95*clamp(u, 0, 1)) / 5
		g.uvs[i*2+1] = (float64(tile/5) + .025 + .95*clamp(v, 0, 1)) / 4
	}
}

func makeRockAtlas(seed int64) ([]byte, []byte, error) {
	bounds := image.Rect(0, 0, 5*rockAtlasTile, 4*rockAtlasTile)
	albedo := image.NewNRGBA(bounds)
	palette := make(color.Palette, 64)
	for i := range palette {
		palette[i] = color.NRGBA{R: 255, G: uint8(math.Round(float64(i) * 255 / 63)), A: 255}
	}
	mr := image.NewPaletted(bounds, palette)
	stacks, boulders := stackSpecs(seed), boulderSpecs(seed)
	n := newNoise(seed)
	for tile := 0; tile < len(stacks)+len(boulders); tile++ {
		for py := 0; py < rockAtlasTile; py++ {
			v := clamp((float64(py)/float64(rockAtlasTile-1)-.025)/.95, 0, 1)
			for px := 0; px < rockAtlasTile; px++ {
				u := clamp((float64(px)/float64(rockAtlasTile-1)-.025)/.95, 0, 1)
				a := 2 * math.Pi * u
				var x, y, z, top float64
				if tile < len(stacks) {
					s := stacks[tile]
					x, y, z, top = s.x+s.radius*math.Cos(a), lerp(-6, s.height, v), s.z+s.radius*math.Sin(a), s.height
				} else {
					b := boulders[tile-len(stacks)]
					ground := terrainHeight(n, b.x, b.z)
					x, y, z = b.x+b.radius*math.Cos(a), ground+b.radius*(.3+.72*math.Cos(v*math.Pi)), b.z+b.radius*math.Sin(a)
					top = ground + b.radius
				}
				c := basaltTint(n, x, y, z, top)
				xp, yp := tile%5*rockAtlasTile+px, tile/5*rockAtlasTile+py
				albedo.SetNRGBA(xp, yp, color.NRGBA{R: uint8(c[0]), G: uint8(c[1]), B: uint8(c[2]), A: 255})
				rough := lerp(.24, .86, smoothstep(.15, 1.6, y)) + .06*n.value(x*4, z*4+y)
				mr.SetColorIndex(xp, yp, uint8(math.Round(clamp(rough, .15, .94)*63)))
			}
		}
	}
	a, err := encodeJPEG(albedo, 85)
	if err != nil {
		return nil, nil, err
	}
	r, err := encodePNG(mr)
	return a, r, err
}
