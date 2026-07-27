package texture

// This file is the only place in package texture that imports
// assetpipe/texture/bcn. Every signature change in that package therefore costs
// exactly this file. Read the BlockCodec comment in build.go for the seam.

import (
	"fmt"

	"m31labs.dev/gosx/assetpipe/texture/bcn"
	"m31labs.dev/gosx/render/bundle/ktx2"
)

// init registers BC1, BC3, BC4 and BC5.
//
// Every entry pairs a transfer function with the VkFormat whose sampler inverts
// that same curve. TestBlockCodecPairsColorSpaceWithVkFormat checks the pairing
// for every registered codec, because an sRGB normal map looks plausible and is
// wrong on every texel.
func init() {
	// BC1 stores three colour channels in 8 bytes, so it costs 4 bits per
	// texel. The encoder never emits the transparent index, so the payload
	// decodes the same under the RGB and the RGBA VkFormat. The pipeline writes
	// the RGBA pair because WebGPU has no BC1-RGB format at all.
	RegisterBlockCodec(BlockCodec{
		ID:          "bc1-rgba-unorm-srgb",
		Format:      "bc1-rgba-unorm-srgb",
		VkFormat:    ktx2.VkFormatBC1RGBASRGBBlock,
		ColorSpace:  SRGB,
		BlockWidth:  4,
		BlockHeight: 4,
		BlockBytes:  8,
		Roles:       []Role{RoleBaseColor, RoleEmissive},
		NeedsAlpha:  false,
		Encode: func(level *Image, quality BlockQuality) ([]byte, error) {
			return bcn.EncodeBC1(bcnSurface(level), bcn.BC1Options{
				Transfer: bcn.TransferSRGB,
				Quality:  bcnQuality(quality),
				Alpha:    bcn.BC1Opaque,
				Workers:  bcnWorkers,
			})
		},
	})
	RegisterBlockCodec(BlockCodec{
		ID:          "bc1-rgba-unorm",
		Format:      "bc1-rgba-unorm",
		VkFormat:    ktx2.VkFormatBC1RGBAUnormBlock,
		ColorSpace:  Linear,
		BlockWidth:  4,
		BlockHeight: 4,
		BlockBytes:  8,
		Roles:       []Role{RolePacked},
		NeedsAlpha:  false,
		Encode: func(level *Image, quality BlockQuality) ([]byte, error) {
			return bcn.EncodeBC1(bcnSurface(level), bcn.BC1Options{
				Transfer: bcn.TransferUnorm,
				Quality:  bcnQuality(quality),
				Alpha:    bcn.BC1Opaque,
				Workers:  bcnWorkers,
			})
		},
	})

	// BC3 stores colour plus eight alpha bits in 16 bytes. It is the fallback
	// for a colour map with alpha on a device with no BC7.
	RegisterBlockCodec(BlockCodec{
		ID:          "bc3-rgba-unorm-srgb",
		Format:      "bc3-rgba-unorm-srgb",
		VkFormat:    ktx2.VkFormatBC3SRGBBlock,
		ColorSpace:  SRGB,
		BlockWidth:  4,
		BlockHeight: 4,
		BlockBytes:  16,
		Roles:       []Role{RoleBaseColor, RoleEmissive},
		NeedsAlpha:  true,
		Encode: func(level *Image, quality BlockQuality) ([]byte, error) {
			return bcn.EncodeBC3(bcnSurface(level), bcn.BC3Options{
				Transfer: bcn.TransferSRGB,
				Quality:  bcnQuality(quality),
				Workers:  bcnWorkers,
			})
		},
	})
	RegisterBlockCodec(BlockCodec{
		ID:          "bc3-rgba-unorm",
		Format:      "bc3-rgba-unorm",
		VkFormat:    ktx2.VkFormatBC3UnormBlock,
		ColorSpace:  Linear,
		BlockWidth:  4,
		BlockHeight: 4,
		BlockBytes:  16,
		Roles:       []Role{RolePacked},
		NeedsAlpha:  true,
		Encode: func(level *Image, quality BlockQuality) ([]byte, error) {
			return bcn.EncodeBC3(bcnSurface(level), bcn.BC3Options{
				Transfer: bcn.TransferUnorm,
				Quality:  bcnQuality(quality),
				Workers:  bcnWorkers,
			})
		},
	})

	// BC4 stores one channel in 8 bytes. It beats BC7 on a single-factor map at
	// half the bitrate, because every bit serves the one channel.
	//
	// The codec reads the red channel. A grayscale roughness or occlusion map
	// holds the same value in every channel, so red is right. A glTF
	// metallicRoughnessTexture packs roughness in green and metalness in blue;
	// that map is RolePacked and takes BC7 instead, because splitting it into
	// two BC4 images needs a renderer channel map that does not exist yet.
	RegisterBlockCodec(BlockCodec{
		ID:          "bc4-r-unorm",
		Format:      "bc4-r-unorm",
		VkFormat:    ktx2.VkFormatBC4UnormBlock,
		ColorSpace:  Linear,
		BlockWidth:  4,
		BlockHeight: 4,
		BlockBytes:  8,
		Roles:       []Role{RoleMask},
		NeedsAlpha:  false,
		ShaderWork:  "a BC4 sample yields (r, 0, 0, 1); the material must read the red channel",
		Encode: func(level *Image, quality BlockQuality) ([]byte, error) {
			return bcn.EncodeBC4(bcnSurface(level), bcn.BC4Options{
				Transfer: bcn.TransferUnorm,
				Quality:  bcnQuality(quality),
				Channel:  bcn.ChannelR,
				Workers:  bcnWorkers,
			})
		},
	})

	// BC5 stores two channels in 16 bytes. The normal codec reads the three
	// colour channels as a normal, normalizes each texel, and stores x and y.
	RegisterBlockCodec(BlockCodec{
		ID:          "bc5-rg-unorm-normal",
		Format:      "bc5-rg-unorm",
		VkFormat:    ktx2.VkFormatBC5UnormBlock,
		ColorSpace:  Linear,
		BlockWidth:  4,
		BlockHeight: 4,
		BlockBytes:  16,
		Roles:       []Role{RoleNormal},
		NeedsAlpha:  false,
		ShaderWork:  "rebuild z as sqrt(max(0, 1 - x*x - y*y)) after decoding x and y from (2*r-1, 2*g-1)",
		Encode: func(level *Image, quality BlockQuality) ([]byte, error) {
			return bcn.EncodeBC5Normal(bcnSurface(level), bcn.BC5Options{
				Transfer: bcn.TransferUnorm,
				Quality:  bcnQuality(quality),
				Workers:  bcnWorkers,
			})
		},
	})

	// The plain BC5 codec keeps two unrelated data channels. No role picks it
	// today; a caller names it through BuildOptions.BlockCodecs.
	RegisterBlockCodec(BlockCodec{
		ID:          "bc5-rg-unorm",
		Format:      "bc5-rg-unorm",
		VkFormat:    ktx2.VkFormatBC5UnormBlock,
		ColorSpace:  Linear,
		BlockWidth:  4,
		BlockHeight: 4,
		BlockBytes:  16,
		Roles:       []Role{RolePacked},
		NeedsAlpha:  false,
		ShaderWork:  "a BC5 sample yields (r, g, 0, 1); the material must name which factor each channel holds",
		Encode: func(level *Image, quality BlockQuality) ([]byte, error) {
			return bcn.EncodeBC5(bcnSurface(level), bcn.BC5Options{
				Transfer: bcn.TransferUnorm,
				Quality:  bcnQuality(quality),
				First:    bcn.ChannelR,
				Second:   bcn.ChannelG,
				Workers:  bcnWorkers,
			})
		},
	})
}

// bcnSurface wraps one Image as a bcn.Surface without copying pixels.
//
// bcn.Surface repeats the memory layout of Image on purpose, because package bcn
// cannot import this package. Sharing the slice keeps the wrapper free.
func bcnSurface(level *Image) *bcn.Surface {
	if level == nil {
		return &bcn.Surface{}
	}
	return &bcn.Surface{Width: level.Width, Height: level.Height, Pix: level.Pix}
}

// bcnWorkers asks the bcn encoders for one goroutine per processor.
//
// Package bcn defaults to a single goroutine, because it cannot know whether its
// caller already runs many textures at once. This caller does know: the asset
// executor builds one asset at a time, so a single-threaded encode leaves every
// other core idle. A 2048 square BC1 level costs about 17 seconds on one core.
//
// The output never depends on the worker count. Each block writes only its own
// bytes and reads only the source, which TestWorkersDoNotChangeOutput in package
// bcn pins, and TestBlockCodecEncodeIsDeterministic pins again through this seam.
const bcnWorkers = -1

// bcnQuality maps the pipeline level onto the bcn level.
func bcnQuality(q BlockQuality) bcn.Quality {
	if q == BlockQualityFast {
		return bcn.QualityFast
	}
	// bcn has two levels, so balanced and best both take the high search. The
	// difference between them lives in the BC7 encoder, which has three.
	return bcn.QualityHigh
}

// BCNMipChainBytes reports the GPU bytes of one mip chain through the encoder
// package's own arithmetic.
//
// The function exists as an oracle. BlockMipChainBytes in build.go computes the
// same number from the codec table, and TestBlockMipChainBytesMatchesEncoder
// compares the two. Two independent implementations that agree are evidence; one
// implementation measuring itself is not.
func BCNMipChainBytes(id string, width, height int) (int, error) {
	format, ok := map[string]bcn.Format{
		"bc1-rgba-unorm-srgb": bcn.FormatBC1RGB,
		"bc1-rgba-unorm":      bcn.FormatBC1RGB,
		"bc3-rgba-unorm-srgb": bcn.FormatBC3,
		"bc3-rgba-unorm":      bcn.FormatBC3,
		"bc4-r-unorm":         bcn.FormatBC4,
		"bc5-rg-unorm":        bcn.FormatBC5,
		"bc5-rg-unorm-normal": bcn.FormatBC5,
	}[id]
	if !ok {
		return 0, fmt.Errorf("%w: %q is not a bcn codec", ErrShape, id)
	}
	return bcn.MipChainBytes(format, width, height), nil
}

// BCNRGBA8MipChainBytes reports the rgba8unorm cost of one mip chain through the
// encoder package. It is the independent denominator of every GPU ratio.
func BCNRGBA8MipChainBytes(width, height int) int {
	return bcn.RGBA8MipChainBytes(width, height)
}
