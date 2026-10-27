// Materials generates the demo's small, seamless surface maps and studio
// reflection environment. Run from the repository root with go run
// ./examples/turbofan/materials. No downloaded textures are required.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

func main() {
	dir := "examples/turbofan/public/materials"
	if err := os.MkdirAll(dir, 0755); err != nil {
		panic(err)
	}
	write := func(name string, img image.Image) {
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			panic(err)
		}
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(file, img)
		closeErr := file.Close()
		if err != nil {
			panic(err)
		}
		if closeErr != nil {
			panic(closeErr)
		}
		info, _ := os.Stat(file.Name())
		fmt.Printf("%s: %d bytes\n", name, info.Size())
	}
	// A grayscale LDR studio, not a measured or prefiltered HDR light probe.
	// Soft rectangular cards produce broad shape-defining metal reflections.
	palette := color.Palette{}
	for i := 0; i < 32; i++ {
		v := uint8(i * 255 / 31)
		palette = append(palette, color.NRGBA{v, v, v, 255})
	}
	studio := image.NewPaletted(image.Rect(0, 0, 256, 128), palette)
	wrap := func(a, b float64) float64 { d := math.Abs(a - b); return math.Min(d, 1-d) }
	for y := 0; y < 128; y++ {
		for x := 0; x < 256; x++ {
			u, v := float64(x)/256, float64(y)/127
			light := .055 + .10*math.Exp(-math.Pow((v-.38)/.30, 2))
			for _, card := range []struct{ u, v, w, h, p float64 }{{.18, .32, .13, .14, .80}, {.67, .38, .045, .25, .70}, {.90, .67, .12, .10, .25}} {
				light += card.p * math.Exp(-math.Pow(wrap(u, card.u)/card.w, 6)-math.Pow((v-card.v)/card.h, 6))
			}
			studio.SetColorIndex(x, y, uint8(math.Round(math.Min(1, light)*31)))
		}
	}
	write("studio-v1.png", studio)
	normal := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	rough := image.NewGray(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			u, v := float64(x)/64, float64(y)/64
			grain := math.Sin(2 * math.Pi * (19*v + .035*math.Sin(2*math.Pi*3*u)))
			fine := math.Sin(2 * math.Pi * (29*v + .025*math.Sin(2*math.Pi*5*u)))
			nx := .012 * math.Sin(2*math.Pi*3*u)
			ny := .10*grain + .025*fine
			nz := math.Sqrt(1 - nx*nx - ny*ny)
			encode := func(n float64) uint8 { return uint8(math.Round((n*.5 + .5) * 255)) }
			normal.SetNRGBA(x, y, color.NRGBA{encode(nx), encode(ny), encode(nz), 255})
			rough.SetGray(x, y, color.Gray{uint8(math.Round(234 + 12*grain + 5*fine))})
		}
	}
	write("brushed-normal-v1.png", normal)
	write("brushed-roughness-v1.png", rough)
}
