// Opt-in measurements for the texture stage. Set GOSX_MEASURE=1 to run them.
// They report input bytes, output bytes, the ratio, and the timing of every
// operation at production settings, so the build-time cost stays visible.
package assetpipe

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"m31labs.dev/gosx/assetpipe/texture"
)

// photoLikeImage builds an image whose statistics resemble a photographed
// material: low-frequency structure plus fine grain.
//
// The shape matters for every byte count in this file. A smooth synthetic
// gradient compresses to almost nothing as a PNG, which makes any KTX2 output
// look hundreds of times larger than the source. A real albedo scan does not,
// so the ratios below are the ones a project would actually see.
func photoLikeImage(width, height int, seed int64) *image.NRGBA {
	rng := rand.New(rand.NewSource(seed))
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			fx := float64(x) / float64(width)
			fy := float64(y) / float64(height)
			base := 0.45 + 0.25*math.Sin(fx*9)*math.Cos(fy*7) + 0.1*math.Sin(fx*37+fy*23)
			grain := (rng.Float64() - 0.5) * 0.18
			r := clampByte((base + grain) * 255)
			g := clampByte((base*0.85 + grain) * 255)
			b := clampByte((base*0.6 + grain) * 255)
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return img
}

func clampByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image, quality int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestMeasureTextureStage reports the whole stage on a photographic input.
func TestMeasureTextureStage(t *testing.T) {
	if os.Getenv("GOSX_MEASURE") == "" {
		t.Skip("set GOSX_MEASURE=1")
	}
	source := photoLikeImage(2048, 2048, 7)
	pngBytes := encodePNG(t, source)
	jpegBytes := encodeJPEG(t, source, 85)
	t.Logf("source 2048x2048: png %d bytes, jpeg q85 %d bytes", len(pngBytes), len(jpegBytes))

	start := time.Now()
	decoded, info, err := texture.Decode(pngBytes, texture.SRGB)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("decode png 2048x2048 (%d bit): %v", info.BitDepth, time.Since(start))

	for _, filter := range []texture.Filter{texture.Box, texture.Triangle, texture.Mitchell, texture.Lanczos3} {
		start = time.Now()
		if _, err := texture.Resize(decoded, 1024, 1024, filter); err != nil {
			t.Fatal(err)
		}
		t.Logf("resize 2048 to 1024, %s: %v", filter, time.Since(start))
	}

	start = time.Now()
	chain, err := texture.MipChain(decoded, texture.MipOptions{Filter: texture.Lanczos3})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("mip chain 2048, %d levels, lanczos3: %v", len(chain), time.Since(start))

	start = time.Now()
	plain, _, err := texture.EncodeKTX2(chain, texture.EncodeOptions{ColorSpace: texture.SRGB, Channels: 4})
	if err != nil {
		t.Fatal(err)
	}
	plainTime := time.Since(start)
	start = time.Now()
	packed, _, err := texture.EncodeKTX2(chain, texture.EncodeOptions{ColorSpace: texture.SRGB, Channels: 4, Supercompress: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ktx2 rgba8-srgb plain: %d bytes in %v", len(plain), plainTime)
	t.Logf("ktx2 rgba8-srgb zlib:  %d bytes in %v (%.1f%% of plain, %.2fx the png)",
		len(packed), time.Since(start),
		100*float64(len(packed))/float64(len(plain)),
		float64(len(packed))/float64(len(pngBytes)))
	// The GPU upload size is the plain payload either way. Report it, because
	// it is the number a memory budget cares about.
	t.Logf("gpu upload bytes (mip chain, rgba8): %d", mipUploadBytes(2048, 2048, 4))

	// The whole stage, through the public entry point. The grayscale input goes
	// in under a data-map name, so the report covers both the sRGB colour path
	// and the linear one-channel path.
	grayBytes := encodePNG(t, grayCopy(source))
	cases := []struct {
		name string
		data []byte
	}{
		{"albedo.png", pngBytes},
		{"wall_ao.png", grayBytes},
		{"hero.jpg", jpegBytes},
	}
	for _, tc := range cases {
		start = time.Now()
		result, variants, err := BuildTexture(tc.name, tc.data, DefaultTextureOptions())
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("BuildTexture %s (%s, %s alpha, grayscale %v): %v, source %d bytes, output %d bytes, ratio %.3f",
			tc.name, result.ColorSpace, result.AlphaMode, result.Grayscale, time.Since(start),
			result.SourceBytes, result.OutputBytes,
			float64(result.OutputBytes)/float64(result.SourceBytes))
		for i, plan := range result.Variants {
			gpu := mipUploadBytes(plan.Width, plan.Height, plan.Channels)
			t.Logf("  %-9s %4dx%-4d %-16s %2d levels %9d wire ratio %6.3f  %9d gpu %4dms  %s",
				plan.Tier, plan.Width, plan.Height, plan.Format, plan.Levels, plan.Bytes,
				plan.Ratio, gpu, plan.DurationMS, variants[i].URI)
			if plan.AlphaPruneRejected {
				t.Logf("             rejected the rgb8 form at %d bytes; zlib codes the four-byte stride better", plan.AlphaPruneBytes)
			}
			// What a block encoder would produce for the same tier, computed
			// arithmetically from the format's bytes per block. These are not
			// measured, because this pipeline has no block encoder. They are
			// the target a BC7 or ASTC stage would hit.
			t.Logf("             for comparison, gpu bytes with a block codec: bc7/astc-4x4 %d, astc-6x6 %d, etc2-rgb %d",
				mipBlockBytes(plan.Width, plan.Height, 4, 4, 16),
				mipBlockBytes(plan.Width, plan.Height, 6, 6, 16),
				mipBlockBytes(plan.Width, plan.Height, 4, 4, 8))
		}
	}

	// End to end through Plan and Execute.
	dir := t.TempDir()
	mustWriteBytes(t, filepath.Join(dir, "public", "tex", "albedo.png"), pngBytes)
	mustWriteBytes(t, filepath.Join(dir, "public", "tex", "wall_roughness.png"), encodePNG(t, grayCopy(source)))
	mustWriteBytes(t, filepath.Join(dir, "public", "tex", "hero.jpg"), jpegBytes)

	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	executed, execReport, err := Execute(report, ExecuteOptions{Root: dir, Only: []string{"texture-transcode-ktx2"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Execute textures total: %v, %+v", time.Since(start), execReport.Totals)
	for _, result := range execReport.Results {
		t.Logf("  %s %s %s %dms source %d output %d ratio %s",
			result.Path, result.Action, result.Status, result.DurationMS,
			result.SourceBytes, result.OutputBytes, result.Metrics["outputRatio"])
	}
	manifest := BuildVariantManifest(executed)
	data, err := MarshalVariantManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("variant manifest (%d bytes):\n%s", len(data), data)
}

// grayCopy returns a grayscale copy with three equal channels, which is how an
// authoring tool exports a single-factor map.
func grayCopy(src *image.NRGBA) *image.NRGBA {
	bounds := src.Bounds()
	out := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := src.NRGBAAt(x, y)
			v := uint8((int(c.R)*30 + int(c.G)*59 + int(c.B)*11) / 100)
			out.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return out
}

// mipUploadBytes returns the GPU bytes of one full mip chain.
func mipUploadBytes(width, height, bytesPerPixel int) int {
	total := 0
	for width > 1 || height > 1 {
		total += width * height * bytesPerPixel
		if width > 1 {
			width /= 2
		}
		if height > 1 {
			height /= 2
		}
	}
	return total + bytesPerPixel
}

// mipBlockBytes returns the GPU bytes of one full mip chain in a
// block-compressed format. The pipeline cannot produce these files; the number
// exists so the report states the size of the gap instead of implying it.
func mipBlockBytes(width, height, blockWidth, blockHeight, bytesPerBlock int) int {
	total := 0
	for {
		columns := (width + blockWidth - 1) / blockWidth
		rows := (height + blockHeight - 1) / blockHeight
		total += columns * rows * bytesPerBlock
		if width == 1 && height == 1 {
			return total
		}
		if width > 1 {
			width /= 2
		}
		if height > 1 {
			height /= 2
		}
	}
}

// sineImage builds a horizontal sine of the given period, in linear light and
// with an analytically known value at every texel centre.
func sineImage(width, height int, period float64) *texture.Image {
	img := texture.NewImage(width, height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			v := float32(0.5 + 0.4*math.Sin(2*math.Pi*(float64(x)+0.5)/period))
			img.Set(x, y, v, v, v, 1)
		}
	}
	return img
}

// TestMeasureFilterQuality measures each kernel against an analytic reference,
// not against another resample.
//
// Two measurements decide a minification kernel:
//
//   - passband error. A sine well below the target Nyquist limit must come
//     through unchanged. The reference is the same analytic sine sampled on the
//     target grid, so the error is absolute, not relative to another filter.
//   - stopband leakage, which is aliasing. A sine ABOVE the target Nyquist limit
//     carries no information the target grid can hold, so the correct output is
//     the signal's mean, a flat 0.5. Whatever amplitude survives is aliasing,
//     and it is what makes a minified texture shimmer when the camera moves.
//
// Box is the extreme case: it is an exact area average at a 2:1 ratio, so its
// passband is perfect and its stopband is the worst of the four, because a box
// in the spatial domain is a sinc in the frequency domain and sinc has large
// side lobes.
func TestMeasureFilterQuality(t *testing.T) {
	if os.Getenv("GOSX_MEASURE") == "" {
		t.Skip("set GOSX_MEASURE=1")
	}
	const srcSize = 512
	const dstSize = 256

	for _, filter := range []texture.Filter{texture.Box, texture.Triangle, texture.Mitchell, texture.Lanczos3} {
		// Passband: period 32 source texels is period 16 target texels, well
		// inside what the target grid holds.
		source := sineImage(srcSize, 8, 32)
		got, err := texture.Resize(source, dstSize, 8, filter)
		if err != nil {
			t.Fatal(err)
		}
		reference := sineImage(dstSize, 8, 16)
		var passSum float64
		// Skip six texels at each edge, where clamp-to-edge replication makes
		// the input non-sinusoidal for every kernel.
		const margin = 6
		count := 0
		for x := margin; x < dstSize-margin; x++ {
			g, _, _, _ := got.At(x, 4)
			r, _, _, _ := reference.At(x, 4)
			diff := float64(g - r)
			passSum += diff * diff
			count++
		}
		passRMS := math.Sqrt(passSum / float64(count))

		// Stopband: period 3 source texels is a frequency of one third, above
		// the target Nyquist limit of one quarter. The correct output is flat.
		alias := sineImage(srcSize, 8, 3)
		aliased, err := texture.Resize(alias, dstSize, 8, filter)
		if err != nil {
			t.Fatal(err)
		}
		var leak float64
		for x := margin; x < dstSize-margin; x++ {
			v, _, _, _ := aliased.At(x, 4)
			diff := math.Abs(float64(v) - 0.5)
			if diff > leak {
				leak = diff
			}
		}
		t.Logf("%-9s passband rms %.6f (%.1f dB), stopband leak %.6f of a 0.4 amplitude (%.2f%%)",
			filter, passRMS, 20*math.Log10(1/math.Max(passRMS, 1e-12)), leak, 100*leak/0.4)
	}
}
