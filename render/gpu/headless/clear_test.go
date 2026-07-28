package headless

import (
	"encoding/binary"
	"math"
	"testing"

	"m31labs.dev/gosx/render/gpu"
)

// This file guards and measures the depth clear, which is the single hottest
// function in this backend.
//
// render/bundle clears three 2048-square shadow cascades on every frame, whether
// or not the scene holds a shadow caster or even a light. That is 12.6 million
// texels, and this backend keeps each one twice: once as a float32 for the raster
// depth test, and once encoded in the texture bytes for a readback. So one frame
// writes about 100 megabytes before it draws anything.
//
// A CPU profile of an empty sixteen-pixel frame put 98.8 percent of all samples
// in clearDepthView. The clear now fills through memmove instead of one write per
// texel, which is as fast as a full clear can be. The remaining cost is memory
// bandwidth and cannot be reduced from inside this file. Removing it needs
// render/bundle to skip the shadow pass when nothing casts a shadow.

// shadowLikeTexture builds a texture with the shape render/bundle allocates for
// its cascaded shadow map, so the benchmark measures the real work.
func shadowLikeTexture() *Texture {
	const size = 2048
	const layers = 3
	texels := size * size * layers
	return &Texture{
		width: size, height: size, layers: layers,
		format: gpu.FormatDepth32Float,
		depth:  make([]float32, texels), data: make([]byte, texels*4),
	}
}

// clearDepthViewPerTexel is the straightforward per-texel clear. Keep it: it is
// the reference the fast clear is proved against, and a fast path with no slow
// reference beside it drifts.
func clearDepthViewPerTexel(t *Texture, layer int, depth float64) {
	if t == nil || !t.format.HasDepth() {
		return
	}
	v := float32(clamp01(depth))
	start, end := textureLayerRange(t, layer)
	for i := start; i < end && i < len(t.depth); i++ {
		t.depth[i] = v
	}
	bpp := bytesPerPixel(t.format)
	if bpp == 0 || len(t.data) == 0 {
		return
	}
	dataStart, dataEnd := start*bpp, end*bpp
	if layer < 0 {
		dataStart, dataEnd = 0, len(t.data)
	}
	switch t.format {
	case gpu.FormatDepth16Unorm:
		encoded := uint16(math.Round(float64(v * 0xffff)))
		for i := dataStart; i+1 < dataEnd && i+1 < len(t.data); i += bpp {
			binary.LittleEndian.PutUint16(t.data[i:i+2], encoded)
		}
	case gpu.FormatDepth24Plus, gpu.FormatDepth24PlusStencil8:
		encoded := uint32(math.Round(float64(v * 0x00ffffff)))
		for i := dataStart; i+3 < dataEnd && i+3 < len(t.data); i += bpp {
			binary.LittleEndian.PutUint32(t.data[i:i+4], encoded)
		}
	case gpu.FormatDepth32Float:
		encoded := math.Float32bits(v)
		for i := dataStart; i+3 < dataEnd && i+3 < len(t.data); i += bpp {
			binary.LittleEndian.PutUint32(t.data[i:i+4], encoded)
		}
	}
}

// smallDepthTexture builds a texture of the given format for the equivalence
// check. It stays small so the check covers every format quickly.
func smallDepthTexture(format gpu.TextureFormat, layers int) *Texture {
	const size = 7 // Not a power of two, so a partial trailing span is exercised.
	texels := size * size * layers
	return &Texture{
		width: size, height: size, layers: layers, format: format,
		depth: make([]float32, texels), data: make([]byte, texels*bytesPerPixel(format)),
	}
}

// TestFastDepthClearMatchesThePerTexelClear proves the memmove fill writes
// exactly the bytes the per-texel loop wrote. Every depth format and both the
// whole-texture and single-layer paths are covered, because the layer path
// computes its own byte range.
func TestFastDepthClearMatchesThePerTexelClear(t *testing.T) {
	formats := []gpu.TextureFormat{
		gpu.FormatDepth16Unorm,
		gpu.FormatDepth24Plus,
		gpu.FormatDepth24PlusStencil8,
		gpu.FormatDepth32Float,
	}
	for _, format := range formats {
		for _, layers := range []int{1, 3} {
			for _, layer := range []int{-1, 0, 1, 2, 5} {
				for _, depth := range []float64{0, 0.375, 1, -0.5, 1.5} {
					reference := smallDepthTexture(format, layers)
					fast := smallDepthTexture(format, layers)
					clearDepthViewPerTexel(reference, layer, depth)
					clearDepthView(fast, layer, depth)
					for index := range reference.depth {
						if reference.depth[index] != fast.depth[index] {
							t.Fatalf("format %v, %d layers, layer %d, depth %v: float mismatch at %d: %v against %v",
								format, layers, layer, depth, index, fast.depth[index], reference.depth[index])
						}
					}
					for index := range reference.data {
						if reference.data[index] != fast.data[index] {
							t.Fatalf("format %v, %d layers, layer %d, depth %v: byte mismatch at %d: %#x against %#x",
								format, layers, layer, depth, index, fast.data[index], reference.data[index])
						}
					}
				}
			}
		}
	}
}

// TestRepeatPatternFillsWholeSpans covers the doubling copy on its own, including
// the spans a texture never produces.
func TestRepeatPatternFillsWholeSpans(t *testing.T) {
	for _, length := range []int{0, 1, 2, 3, 4, 5, 8, 9, 33, 1024, 1025} {
		for _, stride := range []int{0, 1, 2, 4} {
			buffer := make([]byte, length)
			for index := 0; index < stride && index < length; index++ {
				buffer[index] = byte(0xa0 + index)
			}
			repeatPattern(buffer, stride)
			if stride <= 0 {
				continue
			}
			for index := stride; index < length; index++ {
				if want := byte(0xa0 + index%stride); buffer[index] != want {
					t.Fatalf("length %d, stride %d: byte %d is %#x, want %#x",
						length, stride, index, buffer[index], want)
				}
			}
		}
	}
}

// TestRepeatFloat32FillsWholeSpans covers the float variant of the doubling copy.
func TestRepeatFloat32FillsWholeSpans(t *testing.T) {
	for _, length := range []int{0, 1, 2, 3, 7, 64, 65} {
		buffer := make([]float32, length)
		repeatFloat32(buffer, 0.25)
		for index := range buffer {
			if buffer[index] != 0.25 {
				t.Fatalf("length %d: element %d is %v, want 0.25", length, index, buffer[index])
			}
		}
	}
}

// BenchmarkClearShadowAtlas measures one full clear of the cascaded shadow map at
// the size render/bundle allocates. Compare the two variants to see the cost of a
// per-texel clear, and compare either against BenchmarkRasterPhases/background-only
// to see how much of an empty frame this one call is.
func BenchmarkClearShadowAtlas(b *testing.B) {
	const bytesPerClear = 2048 * 2048 * 3 * 8 // float32 depth plus encoded bytes.
	b.Run("memmove", func(b *testing.B) {
		texture := shadowLikeTexture()
		b.SetBytes(bytesPerClear)
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			clearDepthView(texture, -1, 1)
		}
	})
	b.Run("per-texel", func(b *testing.B) {
		texture := shadowLikeTexture()
		b.SetBytes(bytesPerClear)
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			clearDepthViewPerTexel(texture, -1, 1)
		}
	})
	b.Run("memmove-one-layer", func(b *testing.B) {
		texture := shadowLikeTexture()
		b.SetBytes(bytesPerClear / 3)
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			clearDepthView(texture, 1, 1)
		}
	})
}
