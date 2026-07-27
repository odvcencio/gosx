package texture

import (
	"fmt"

	"m31labs.dev/gosx/render/bundle/ktx2"
)

// EncodeOptions controls the KTX2 container the writer produces.
type EncodeOptions struct {
	// ColorSpace decides the transfer function of the colour channels and
	// therefore the sRGB or unorm VkFormat.
	ColorSpace ColorSpace
	// Channels selects how many samples each texel stores, from 1 to 4.
	Channels int
	// Supercompress writes zlib payloads, KTX2 scheme 3.
	//
	// Turn this on for a texture. The PNG or JPEG source was already
	// entropy coded, so a plain KTX2 payload is larger on the wire than the
	// file it replaces. Zlib brings the wire size back and does not change
	// the GPU upload size either way.
	Supercompress bool
	// KeyValues adds container metadata. The writer always adds KTXwriter.
	KeyValues map[string]string
}

// VkFormatFor returns the VkFormat the writer uses for a channel count and a
// colour space.
//
// The sRGB transfer function only applies to a colour texture. A one or two
// channel texture holds data, so it always takes an unorm format. That rule
// matters: an sRGB one-channel format exists in Vulkan, but no WebGPU or WebGL2
// texture format matches it, so emitting it would produce a file no browser can
// upload.
func VkFormatFor(channels int, space ColorSpace) (int, string, error) {
	switch channels {
	case 1:
		return ktx2.VkFormatR8Unorm, "r8unorm", nil
	case 2:
		return ktx2.VkFormatR8G8Unorm, "rg8unorm", nil
	case 3:
		if space == SRGB {
			return ktx2.VkFormatR8G8B8SRGB, "rgb8unorm-srgb", nil
		}
		return ktx2.VkFormatR8G8B8Unorm, "rgb8unorm", nil
	case 4:
		if space == SRGB {
			return ktx2.VkFormatR8G8B8A8SRGB, "rgba8unorm-srgb", nil
		}
		return ktx2.VkFormatR8G8B8A8Unorm, "rgba8unorm", nil
	}
	return 0, "", fmt.Errorf("%w: %d channels, want 1 to 4", ErrShape, channels)
}

// EncodeKTX2 writes a mip chain as one KTX2 container.
//
// levels must start at the base level and halve each edge, which MipChain
// guarantees. The writer stores the smallest mip first inside the file, as the
// KTX2 specification requires, and keeps level zero first in the level index.
func EncodeKTX2(levels []*Image, opts EncodeOptions) ([]byte, string, error) {
	if len(levels) == 0 || levels[0] == nil {
		return nil, "", fmt.Errorf("%w: no levels", ErrShape)
	}
	channels := opts.Channels
	if channels == 0 {
		channels = 4
	}
	format, formatName, err := VkFormatFor(channels, opts.ColorSpace)
	if err != nil {
		return nil, "", err
	}
	img := &ktx2.Image{
		Format: format,
		Width:  levels[0].Width,
		Height: levels[0].Height,
		Faces:  1,
		Levels: make([]ktx2.Level, len(levels)),
	}
	for i, level := range levels {
		payload, err := EncodeBytes(level, opts.ColorSpace, channels)
		if err != nil {
			return nil, "", fmt.Errorf("level %d: %w", i, err)
		}
		img.Levels[i] = ktx2.Level{
			Width:  level.Width,
			Height: level.Height,
			Depth:  1,
			Layers: 1,
			Faces:  1,
			Bytes:  payload,
		}
	}
	scheme := ktx2.SupercompressionNone
	if opts.Supercompress {
		scheme = ktx2.SupercompressionZlib
	}
	data, err := ktx2.Encode(img, ktx2.EncodeOptions{
		Writer:           "GoSX assetpipe texture",
		KeyValues:        opts.KeyValues,
		Supercompression: scheme,
		ZlibLevel:        6,
	})
	if err != nil {
		return nil, "", err
	}
	return data, formatName, nil
}

// BlockEncodeOptions controls the container of a block-compressed variant.
type BlockEncodeOptions struct {
	// Supercompress writes zlib payloads, KTX2 scheme 3.
	//
	// Turn it on. Supercompression changes wire bytes only: the driver always
	// receives inflated blocks, so the GPU cost is the same either way.
	Supercompress bool
	// KeyValues adds container metadata. The writer always adds KTXwriter.
	KeyValues map[string]string
}

// EncodeBlockKTX2 writes a block-compressed mip chain as one KTX2 container.
//
// levels carries the geometry of each mip level and payloads carries the block
// bytes of the same level, in the same order. The two must have the same length,
// and each payload must be a whole number of blocks for its level, which the
// writer checks before it builds the container.
//
// The container writer refuses a level-0 size that is not a whole number of
// texel blocks, because WebGPU createTexture refuses the same thing.
func EncodeBlockKTX2(codec BlockCodec, levels []*Image, payloads [][]byte, opts BlockEncodeOptions) ([]byte, error) {
	if len(levels) == 0 || levels[0] == nil {
		return nil, fmt.Errorf("%w: no levels", ErrShape)
	}
	if len(payloads) != len(levels) {
		return nil, fmt.Errorf("%w: %d payloads for %d levels", ErrShape, len(payloads), len(levels))
	}
	if codec.VkFormat == 0 {
		return nil, fmt.Errorf("%w: block codec %q has no VkFormat", ErrShape, codec.ID)
	}
	// The codec's colour space and its VkFormat must name the same transfer
	// function. A mismatch makes the sampler invert a curve the encoder never
	// applied, which looks plausible and is wrong on every texel.
	if err := checkBlockTransferPairing(codec); err != nil {
		return nil, err
	}

	img := &ktx2.Image{
		Format: codec.VkFormat,
		Width:  levels[0].Width,
		Height: levels[0].Height,
		Faces:  1,
		Levels: make([]ktx2.Level, len(levels)),
	}
	for i, level := range levels {
		want := BlockChainLevelBytes(codec, level.Width, level.Height)
		if len(payloads[i]) != want {
			return nil, fmt.Errorf("%w: level %d is %dx%d and holds %d bytes, want %d",
				ErrShape, i, level.Width, level.Height, len(payloads[i]), want)
		}
		img.Levels[i] = ktx2.Level{
			Width:  level.Width,
			Height: level.Height,
			Depth:  1,
			Layers: 1,
			Faces:  1,
			Bytes:  payloads[i],
		}
	}
	scheme := ktx2.SupercompressionNone
	if opts.Supercompress {
		scheme = ktx2.SupercompressionZlib
	}
	return ktx2.Encode(img, ktx2.EncodeOptions{
		Writer:           "GoSX assetpipe texture",
		KeyValues:        opts.KeyValues,
		Supercompression: scheme,
		ZlibLevel:        6,
	})
}

// blockSRGBFormats lists every block VkFormat whose sampler applies the sRGB
// transfer function. Anything else is linear.
//
// The list is short on purpose, so a reader can check it against the Vulkan
// enumeration by eye. BC4 and BC5 have no sRGB form at all.
var blockSRGBFormats = map[int]bool{
	ktx2.VkFormatBC1RGBSRGBBlock:  true,
	ktx2.VkFormatBC1RGBASRGBBlock: true,
	ktx2.VkFormatBC3SRGBBlock:     true,
	ktx2.VkFormatBC7SRGBBlock:     true,
}

// checkBlockTransferPairing refuses a codec whose colour space and VkFormat
// disagree about the transfer function.
func checkBlockTransferPairing(codec BlockCodec) error {
	wantSRGB := codec.ColorSpace == SRGB
	gotSRGB := blockSRGBFormats[codec.VkFormat]
	if wantSRGB != gotSRGB {
		return fmt.Errorf("%w: codec %q encodes %s but vkFormat %d samples %s",
			ErrShape, codec.ID, codec.ColorSpace, codec.VkFormat, transferName(gotSRGB))
	}
	return nil
}

func transferName(srgb bool) string {
	if srgb {
		return "srgb"
	}
	return "linear"
}

// DecodeKTX2 reads an uncompressed KTX2 texture back into linear images. The
// texture tests use it to check what the writer produced.
func DecodeKTX2(data []byte) ([]*Image, ColorSpace, int, error) {
	img, err := ktx2.Parse(data)
	if err != nil {
		return nil, Linear, 0, err
	}
	channels := ktx2.BytesPerPixel(img.Format)
	if channels < 1 || channels > 4 {
		return nil, Linear, 0, fmt.Errorf("texture: vkFormat %d is not an 8-bit unorm format", img.Format)
	}
	space := Linear
	switch img.Format {
	case ktx2.VkFormatR8G8B8SRGB, ktx2.VkFormatR8G8B8A8SRGB:
		space = SRGB
	}
	out := make([]*Image, len(img.Levels))
	for i, level := range img.Levels {
		decoded := NewImage(level.Width, level.Height)
		want := level.Width * level.Height * channels
		if len(level.Bytes) < want {
			return nil, space, channels, fmt.Errorf("texture: level %d holds %d bytes, want %d", i, len(level.Bytes), want)
		}
		for pixel := 0; pixel < level.Width*level.Height; pixel++ {
			src := pixel * channels
			dst := pixel * 4
			decoded.Pix[dst+3] = 1
			for c := 0; c < channels; c++ {
				code := level.Bytes[src+c]
				if space == SRGB && c < 3 {
					decoded.Pix[dst+c] = srgbDecodeLUT[code]
					continue
				}
				decoded.Pix[dst+c] = float32(code) / 255
			}
		}
		out[i] = decoded
	}
	return out, space, channels, nil
}
