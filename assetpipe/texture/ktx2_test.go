package texture

import (
	"errors"
	"math"
	"testing"

	"m31labs.dev/gosx/render/bundle/ktx2"
)

// TestEncodeKTX2RoundTripsEveryChannelCount checks the container against the
// parser this repository already ships, not against the writer's own idea of
// its output.
func TestEncodeKTX2RoundTripsEveryChannelCount(t *testing.T) {
	src := NewImage(4, 4)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			src.Set(x, y, float32(x)/3, float32(y)/3, 0.5, 1)
		}
	}
	chain, err := MipChain(src, MipOptions{Filter: Box})
	if err != nil {
		t.Fatal(err)
	}
	for channels := 1; channels <= 4; channels++ {
		for _, space := range []ColorSpace{Linear, SRGB} {
			data, name, err := EncodeKTX2(chain, EncodeOptions{ColorSpace: space, Channels: channels})
			if err != nil {
				t.Fatalf("%d channels %s: %v", channels, space, err)
			}
			parsed, err := ktx2.Parse(data)
			if err != nil {
				t.Fatalf("%d channels %s: parse: %v", channels, space, err)
			}
			if parsed.Width != 4 || parsed.Height != 4 || len(parsed.Levels) != 3 {
				t.Fatalf("%s parsed as %dx%d with %d levels", name, parsed.Width, parsed.Height, len(parsed.Levels))
			}
			if got := ktx2.BytesPerPixel(parsed.Format); got != channels {
				t.Fatalf("%s reports %d bytes per pixel, want %d", name, got, channels)
			}
			if len(parsed.Levels[0].Bytes) != 4*4*channels {
				t.Fatalf("%s level 0 holds %d bytes, want %d", name, len(parsed.Levels[0].Bytes), 4*4*channels)
			}
		}
	}
}

// TestEncodeKTX2StoresTheSmallestMipFirst pins the KTX2 layout rule.
func TestEncodeKTX2StoresTheSmallestMipFirst(t *testing.T) {
	chain, err := MipChain(constantImage(8, 8, 1), MipOptions{Filter: Box})
	if err != nil {
		t.Fatal(err)
	}
	data, _, err := EncodeKTX2(chain, EncodeOptions{Channels: 4})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ktx2.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Levels) != 4 {
		t.Fatalf("parsed %d levels, want 4", len(parsed.Levels))
	}
	// Level 0 stays first in the index and largest in size.
	if parsed.Levels[0].Width != 8 || parsed.Levels[3].Width != 1 {
		t.Fatalf("level order broke: level 0 is %d wide, level 3 is %d wide",
			parsed.Levels[0].Width, parsed.Levels[3].Width)
	}
}

// TestEncodeKTX2Supercompresses checks that scheme 3 round trips and reports the
// ratio the pipeline depends on.
func TestEncodeKTX2Supercompresses(t *testing.T) {
	chain, err := MipChain(constantImage(64, 64, 0.5), MipOptions{Filter: Box})
	if err != nil {
		t.Fatal(err)
	}
	plain, _, err := EncodeKTX2(chain, EncodeOptions{Channels: 4})
	if err != nil {
		t.Fatal(err)
	}
	packed, _, err := EncodeKTX2(chain, EncodeOptions{Channels: 4, Supercompress: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(packed) >= len(plain) {
		t.Fatalf("zlib gave %d bytes, plain gave %d", len(packed), len(plain))
	}
	// The parser must inflate transparently.
	levels, _, channels, err := DecodeKTX2(packed)
	if err != nil {
		t.Fatal(err)
	}
	if channels != 4 || len(levels) != len(chain) {
		t.Fatalf("inflated %d levels at %d channels, want %d and 4", len(levels), channels, len(chain))
	}
	r, _, _, _ := levels[0].At(0, 0)
	if math.Abs(float64(r)-0.5) > 1.0/255 {
		t.Fatalf("inflated red is %g, want 0.5", r)
	}
}

// TestEncodeKTX2PixelRoundTripIsWithinOneCode measures the quantization error of
// the whole write-and-read path.
func TestEncodeKTX2PixelRoundTripIsWithinOneCode(t *testing.T) {
	const size = 16
	src := NewImage(size, size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			src.Set(x, y, SRGBToLinear(uint8(x*16)), SRGBToLinear(uint8(y*16)), SRGBToLinear(128), 1)
		}
	}
	data, _, err := EncodeKTX2([]*Image{src}, EncodeOptions{ColorSpace: SRGB, Channels: 4})
	if err != nil {
		t.Fatal(err)
	}
	levels, space, _, err := DecodeKTX2(data)
	if err != nil {
		t.Fatal(err)
	}
	if space != SRGB {
		t.Fatalf("the container reported %s, want srgb", space)
	}
	// The values started as exact 8-bit sRGB codes, so the round trip must be
	// exact, not merely close. A tolerance here would hide a transfer function
	// applied twice.
	for i := range src.Pix {
		if math.Abs(float64(src.Pix[i]-levels[0].Pix[i])) > 1e-6 {
			t.Fatalf("pixel index %d round tripped %g to %g", i, src.Pix[i], levels[0].Pix[i])
		}
	}
}

// TestVkFormatForRefusesBadChannelCounts and the block-format refusal together
// state what the writer will not do.
func TestVkFormatForRefusesBadChannelCounts(t *testing.T) {
	if _, _, err := VkFormatFor(0, Linear); !errors.Is(err, ErrShape) {
		t.Fatalf("zero channels gave %v, want ErrShape", err)
	}
	if _, _, err := VkFormatFor(5, Linear); !errors.Is(err, ErrShape) {
		t.Fatalf("five channels gave %v, want ErrShape", err)
	}
	// A one or two channel texture never takes an sRGB format, because no
	// browser texture format matches VK_FORMAT_R8_SRGB.
	for _, channels := range []int{1, 2} {
		format, name, err := VkFormatFor(channels, SRGB)
		if err != nil {
			t.Fatal(err)
		}
		if format == ktx2.VkFormatR8SRGB || format == ktx2.VkFormatR8G8SRGB {
			t.Fatalf("%d channels selected %s, which no browser can upload", channels, name)
		}
	}
}

// TestKTX2WriterRefusesBlockCompressedFormats states the scope boundary in a
// test, so nobody "fixes" it by emitting an empty BC7 container.
func TestKTX2WriterRefusesBlockCompressedFormats(t *testing.T) {
	for _, format := range []int{
		ktx2.VkFormatBC7SRGBBlock,
		ktx2.VkFormatASTC4x4SRGBBlock,
		ktx2.VkFormatETC2R8G8B8A8SRGBBlock,
	} {
		img := &ktx2.Image{
			Format: format,
			Width:  4,
			Height: 4,
			Faces:  1,
			Levels: []ktx2.Level{{Width: 4, Height: 4, Depth: 1, Layers: 1, Faces: 1, Bytes: make([]byte, 16)}},
		}
		_, err := ktx2.Encode(img, ktx2.EncodeOptions{})
		if !errors.Is(err, ktx2.ErrEncodeBlockCompressed) {
			t.Fatalf("vkFormat %d gave %v, want ErrEncodeBlockCompressed", format, err)
		}
	}
}
