package texture

// Throughput benchmarks for the texture pipeline.
//
// Every benchmark reports megapixels per second, so a number is comparable across
// image sizes and across formats with different bitrates. b.SetBytes is not used:
// bytes per second would rank a format by its bitrate instead of by its speed, and
// the question here is how long a build takes.
//
// The wall time a caller cares about is the whole ladder, not one level, so
// BenchmarkTextureLadder measures that too.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"
)

// benchImage builds a linear image with content a block encoder cannot encode for
// free: gradients, a high-frequency term, and a sharp edge.
//
// A flat or a purely smooth image would measure the early-exit paths of each
// encoder and report a throughput no real asset reaches.
func benchImage(width, height int) *Image {
	img := NewImage(width, height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			fx := float64(x) / float64(width)
			fy := float64(y) / float64(height)
			r := 0.15 + 0.7*fx
			g := 0.15 + 0.7*fy
			b := 0.5 + 0.4*math.Sin(float64(x)*0.7)*math.Cos(float64(y)*0.9)
			a := 1.0
			if (x/17+y/13)%5 == 0 {
				// A sharp edge every few blocks, which is where a block
				// encoder spends its search.
				r, g, b = 1-r, 1-g, 1-b
			}
			if x > width*3/4 {
				a = 0.35
			}
			img.Set(x, y, float32(r), float32(g), float32(clamp(b)), float32(a))
		}
	}
	return img
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// benchNormalImage builds a tangent-space normal map for the BC5 benchmarks.
func benchNormalImage(width, height int) *Image {
	img := NewImage(width, height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			dx := 0.35 * math.Sin(float64(x)*0.31)
			dy := 0.35 * math.Cos(float64(y)*0.27)
			n := math.Sqrt(dx*dx + dy*dy + 1)
			img.Set(x, y,
				float32(dx/n*0.5+0.5), float32(dy/n*0.5+0.5), float32(1/n*0.5+0.5), 1)
		}
	}
	return img
}

// reportMegapixelsPerSecond adds the throughput metric to a benchmark result.
//
// The number is level-0 pixels divided by wall time. For a mip chain the pixel
// count is the whole chain, so a chain benchmark and a level benchmark stay
// comparable.
func reportMegapixelsPerSecond(b *testing.B, pixels int) {
	b.Helper()
	seconds := b.Elapsed().Seconds()
	if seconds <= 0 {
		return
	}
	total := float64(pixels) * float64(b.N)
	b.ReportMetric(total/seconds/1e6, "Mpixel/s")
	b.ReportMetric(seconds/float64(b.N)*1000, "ms/op")
}

// benchSizes are the level-0 sizes the benchmarks run. 2048 is the high tier of
// the default ladder, so it is the number a build actually pays.
var benchSizes = []int{256, 512, 2048}

// BenchmarkBlockEncode measures one level of each codec at each quality level.
func BenchmarkBlockEncode(b *testing.B) {
	qualities := []BlockQuality{BlockQualityFast, BlockQualityBalanced, BlockQualityBest}
	for _, id := range RegisteredBlockCodecs() {
		codec, _ := BlockCodecFor(id)
		for _, size := range benchSizes {
			source := benchImage(size, size)
			if id == "bc5-rg-unorm-normal" {
				source = benchNormalImage(size, size)
			}
			for _, quality := range qualities {
				name := fmt.Sprintf("%s/%dx%d/%s", id, size, size, quality)
				b.Run(name, func(b *testing.B) {
					// Encode once outside the loop, so a failure reports as a
					// failure and not as a wrong throughput.
					if _, err := codec.Encode(source, quality); err != nil {
						b.Fatal(err)
					}
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if _, err := codec.Encode(source, quality); err != nil {
							b.Fatal(err)
						}
					}
					b.StopTimer()
					reportMegapixelsPerSecond(b, size*size)
				})
			}
		}
	}
}

// BenchmarkMipChain measures mip generation on its own.
//
// The chain resamples in linear light with the Lanczos3 kernel, which is the
// pipeline default. The pixel count is the whole chain, because that is the work
// the function does.
func BenchmarkMipChain(b *testing.B) {
	for _, size := range benchSizes {
		for _, alphaAware := range []bool{false, true} {
			source := benchImage(size, size)
			name := fmt.Sprintf("%dx%d/alphaAware=%v", size, size, alphaAware)
			b.Run(name, func(b *testing.B) {
				chain, err := MipChain(source, MipOptions{AlphaAware: alphaAware})
				if err != nil {
					b.Fatal(err)
				}
				pixels := 0
				for _, level := range chain {
					pixels += level.Width * level.Height
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := MipChain(source, MipOptions{AlphaAware: alphaAware}); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				reportMegapixelsPerSecond(b, pixels)
				b.ReportMetric(float64(len(chain)), "levels")
			})
		}
	}
}

// BenchmarkBlockMipChainEncode measures a whole block-compressed mip chain.
//
// The small levels cost almost nothing, so the number is close to the level-0
// number times four thirds. Measuring it anyway states the real cost per variant
// instead of implying it.
func BenchmarkBlockMipChainEncode(b *testing.B) {
	for _, id := range []string{"bc1-rgba-unorm-srgb", "bc4-r-unorm", "bc5-rg-unorm-normal", "bc7-rgba-unorm-srgb"} {
		codec, ok := BlockCodecFor(id)
		if !ok {
			b.Fatalf("%s is not registered", id)
		}
		for _, size := range benchSizes {
			source := benchImage(size, size)
			if id == "bc5-rg-unorm-normal" {
				source = benchNormalImage(size, size)
			}
			chain, err := MipChain(source, MipOptions{})
			if err != nil {
				b.Fatal(err)
			}
			pixels := 0
			for _, level := range chain {
				pixels += level.Width * level.Height
			}
			b.Run(fmt.Sprintf("%s/%dx%d", id, size, size), func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					for _, level := range chain {
						if _, err := codec.Encode(level, BlockQualityBalanced); err != nil {
							b.Fatal(err)
						}
					}
				}
				b.StopTimer()
				reportMegapixelsPerSecond(b, pixels)
			})
		}
	}
}

// BenchmarkTextureLadder measures the whole build: decode, resize, mip, encode,
// and container write, for every tier.
//
// This is the wall time a caller waits. The uncompressed run and the block run are
// separate, so the report can state what block compression adds.
func BenchmarkTextureLadder(b *testing.B) {
	for _, size := range benchSizes {
		source := benchPNG(b, size, size)
		for _, block := range []bool{false, true} {
			for _, quality := range []BlockQuality{BlockQualityFast, BlockQualityBalanced} {
				if !block && quality != BlockQualityFast {
					// Quality means nothing without block compression, so run
					// the uncompressed ladder once.
					continue
				}
				name := fmt.Sprintf("%dx%d/block=%v", size, size, block)
				if block {
					name = fmt.Sprintf("%dx%d/block=%v/%s", size, size, block, quality)
				}
				b.Run(name, func(b *testing.B) {
					opts := BuildOptions{
						ColorSpace:         SRGB,
						Supercompress:      true,
						PruneConstantAlpha: true,
						BlockCompression:   block,
						BlockQuality:       quality,
						Source:             "bench_albedo.png",
					}
					result, err := Build(source, opts)
					if err != nil {
						b.Fatal(err)
					}
					// Report the output the ladder produced, so a reader can see
					// what the time bought.
					b.ReportMetric(float64(len(result.Variants)), "variants")
					b.ReportMetric(float64(result.OutputBytes), "wire_bytes")
					gpu := 0
					for _, plan := range result.Variants {
						gpu += plan.GPUBytes
					}
					b.ReportMetric(float64(gpu), "gpu_bytes")
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if _, err := Build(source, opts); err != nil {
							b.Fatal(err)
						}
					}
					b.StopTimer()
					reportMegapixelsPerSecond(b, size*size)
				})
			}
		}
	}
}

// BenchmarkContainerWrite measures the KTX2 container write on its own, with and
// without zlib supercompression.
//
// Supercompression changes wire bytes only. Measuring it apart from the encode
// shows how much of a build's time is entropy coding rather than block search.
func BenchmarkContainerWrite(b *testing.B) {
	codec, _ := BlockCodecFor("bc7-rgba-unorm-srgb")
	for _, size := range []int{512, 2048} {
		source := benchImage(size, size)
		chain, err := MipChain(source, MipOptions{})
		if err != nil {
			b.Fatal(err)
		}
		payloads := make([][]byte, len(chain))
		for i, level := range chain {
			payload, err := codec.Encode(level, BlockQualityFast)
			if err != nil {
				b.Fatal(err)
			}
			payloads[i] = payload
		}
		pixels := 0
		for _, level := range chain {
			pixels += level.Width * level.Height
		}
		for _, supercompress := range []bool{false, true} {
			b.Run(fmt.Sprintf("%dx%d/zlib=%v", size, size, supercompress), func(b *testing.B) {
				opts := BlockEncodeOptions{Supercompress: supercompress}
				data, err := EncodeBlockKTX2(codec, chain, payloads, opts)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(data)), "wire_bytes")
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := EncodeBlockKTX2(codec, chain, payloads, opts); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				reportMegapixelsPerSecond(b, pixels)
			})
		}
	}
}

// benchPNG encodes a source PNG for the whole-ladder benchmark, because Build
// takes encoded bytes.
func benchPNG(b *testing.B, width, height int) []byte {
	b.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	linear := benchImage(width, height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, bl, a := linear.At(x, y)
			img.SetNRGBA(x, y, color.NRGBA{
				R: LinearToSRGB8(r), G: LinearToSRGB8(g), B: LinearToSRGB8(bl), A: LinearToUnorm8(a),
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		b.Fatal(err)
	}
	return buf.Bytes()
}
