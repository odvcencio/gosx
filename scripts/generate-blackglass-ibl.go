//go:build ignore

// Generate the three small HDR skies and bake their Scene3D IBL products.
// Run from the repository root with GOWORK=off go run scripts/generate-blackglass-ibl.go.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/assetpipe"
)

type sky struct {
	name         string
	top, horizon [3]float64
	sun          [3]float64
	sunX, sunY   float64
	sunStrength  float64
}

var skies = []sky{
	{"daybreak", [3]float64{0.18, 0.31, 0.47}, [3]float64{0.72, 0.42, 0.24}, [3]float64{1, 0.75, 0.49}, 0.72, 0.38, 2.4},
	{"high-sun", [3]float64{0.20, 0.43, 0.58}, [3]float64{0.51, 0.69, 0.67}, [3]float64{1, 0.91, 0.72}, 0.70, 0.31, 3.0},
	{"ember-hour", [3]float64{0.11, 0.14, 0.25}, [3]float64{0.59, 0.29, 0.22}, [3]float64{1, 0.48, 0.25}, 0.73, 0.45, 1.9},
}

func rgbe(r, g, b float64) [4]byte {
	peak := math.Max(r, math.Max(g, b))
	if peak < 1e-32 {
		return [4]byte{}
	}
	mantissa, exponent := math.Frexp(peak)
	scale := mantissa * 256 / peak
	return [4]byte{byte(math.Min(255, r*scale)), byte(math.Min(255, g*scale)), byte(math.Min(255, b*scale)), byte(exponent + 128)}
}

func hdr(s sky) []byte {
	const width, height = 128, 64
	var out bytes.Buffer
	fmt.Fprintf(&out, "#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y %d +X %d\n", height, width)
	for y := 0; y < height; y++ {
		v := float64(y) / float64(height-1)
		t := math.Pow(math.Max(0, 1-v*1.8), 1.3)
		for x := 0; x < width; x++ {
			u := float64(x) / float64(width-1)
			dx := math.Min(math.Abs(u-s.sunX), 1-math.Abs(u-s.sunX))
			dy := v - s.sunY
			glow := s.sunStrength * math.Exp(-((dx*dx)/(0.055*0.055) + (dy*dy)/(0.07*0.07)))
			var rgb [3]float64
			for c := range rgb {
				rgb[c] = s.horizon[c]*(1-t) + s.top[c]*t + glow*s.sun[c]
			}
			value := rgbe(rgb[0], rgb[1], rgb[2])
			out.Write(value[:])
		}
	}
	return out.Bytes()
}

func main() {
	public := filepath.Join("examples", "gosx-docs", "public", "env", "blackglass")
	embed := filepath.Join("examples", "gosx-docs", "app", "demos", "beacon", "ibl")
	for _, dir := range []string{public, embed} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			panic(err)
		}
	}
	for _, s := range skies {
		if err := os.WriteFile(filepath.Join(public, s.name+".hdr"), hdr(s), 0644); err != nil {
			panic(err)
		}
		for _, suffix := range []string{".ibl.ktx2", ".irradiance.ktx2", ".brdf-lut.ktx2", ".ibl.json"} {
			if err := os.Remove(filepath.Join(public, s.name+suffix)); err != nil && !os.IsNotExist(err) {
				panic(err)
			}
		}
	}
	plan, err := assetpipe.Plan([]string{public}, assetpipe.Options{})
	if err != nil {
		panic(err)
	}
	_, report, err := assetpipe.Execute(plan, assetpipe.ExecuteOptions{
		Root: public,
		Only: []string{"prefilter-ibl-ggx", "generate-split-sum-lut"},
		IBL:  assetpipe.IBLOptions{CubeSize: 32, Samples: 32, ProjectionSamples: 2, IrradianceSize: 8, BRDFLUTSize: 64, BRDFSamples: 128},
	})
	if err != nil {
		panic(err)
	}
	if report.Totals.Failed != 0 {
		panic(fmt.Sprintf("IBL build failures: %+v", report.Results))
	}
	for _, s := range skies {
		data, err := os.ReadFile(filepath.Join(public, s.name+".ibl.json"))
		if err != nil {
			panic(err)
		}
		var sidecar map[string]any
		if err := json.Unmarshal(data, &sidecar); err != nil {
			panic(err)
		}
		ibl, ok := sidecar["ibl"].(map[string]any)
		if !ok {
			panic("IBL descriptor missing")
		}
		for _, key := range []string{"radiance", "irradiance", "brdfLUT"} {
			product, ok := ibl[key].(map[string]any)
			if !ok {
				panic("IBL product missing: " + key)
			}
			uri, ok := product["uri"].(string)
			if !ok || !strings.HasSuffix(uri, ".ktx2") {
				panic("IBL URI missing: " + key)
			}
			product["uri"] = "/env/blackglass/" + filepath.Base(uri)
		}
		encoded, err := json.Marshal(ibl)
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(embed, s.name+".json"), append(encoded, '\n'), 0644); err != nil {
			panic(err)
		}
	}
	fmt.Printf("Blackglass IBL: %d actions, %d files, %d bytes\n", report.Totals.Executed, report.Totals.OutputFiles, report.Totals.OutputBytes)
}
