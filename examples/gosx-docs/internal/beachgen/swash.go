package beachgen

import (
	"image"
	"image/color"
	"math"

	"m31labs.dev/gosx/scene"
)

// SwashGeometry keeps one small shader surface grounded in the beach.
// Millimetre coordinates stay compact in the scene transport. The authored
// shader uses the aggregate vertex stream until direct-stream binding is supported.
func SwashGeometry(seed int64) scene.BufferGeometry {
	n := newNoise(seed)
	g := &geometry{}
	const columns, rows = 33, 3
	for row := 0; row < rows; row++ {
		v := float64(row) / (rows - 1)
		for col := 0; col < columns; col++ {
			x := -28 + 60*float64(col)/(columns-1)
			z := -1.3 + 3.4*v + .25*n.value(x*.28, 31)
			appendVertex(g, vec3{x, terrainHeight(n, x, z) + .018, z}, float64(col)/(columns-1), v)
			k := len(g.normals) - 3
			g.normals[k+1] = 1
		}
	}
	for row := 0; row < rows-1; row++ {
		for col := 0; col < columns-1; col++ {
			a := uint16(row*columns + col)
			b := a + columns
			g.indices = append(g.indices, a, b, a+1, a+1, b, b+1)
		}
	}
	out := scene.BufferGeometry{Positions: g.positions, Normals: g.normals, UVs: g.uvs, Revision: 1}
	for i, value := range out.Positions {
		out.Positions[i] = math.Round(value*1000) / 1000
	}
	for _, index := range g.indices {
		out.Indices = append(out.Indices, int(index))
	}
	return out
}

func swashTexture(n noiseField) ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, 256, 64))
	for y := 0; y < 64; y++ {
		v := float64(y) / 63
		for x := 0; x < 256; x++ {
			u := float64(x) / 255
			front := .63 + .09*n.value(u*36, 13) + .035*math.Sin(u*123)
			d := v - front
			line := math.Exp(-math.Pow(d/.055, 2))
			lace := math.Pow(1-math.Abs(n.value(u*170, v*37)), 8)
			wash := (1 - smoothstep(front-.2, front, v)) * smoothstep(0, .3, v)
			grain := .35 + .65*(.5+.5*n.value(u*410, v*81))
			a := (.7*line*grain + .38*wash*lace) * smoothstep(0, .035, u) * (1 - smoothstep(.965, 1, u))
			img.SetNRGBA(x, y, color.NRGBA{R: 224, G: 231, B: 228, A: uint8(clamp(a, 0, 1) * 255)})
		}
	}
	return encodePNG(img)
}
