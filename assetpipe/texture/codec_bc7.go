package texture

// This file is the only place in package texture that imports
// assetpipe/texture/bc7. Every signature change in that package therefore costs
// exactly this file. Read the BlockCodec comment in build.go for the seam.

import (
	"m31labs.dev/gosx/assetpipe/texture/bc7"
	"m31labs.dev/gosx/render/bundle/ktx2"
)

// init registers BC7.
//
// BC7 stores one 4x4 block in 16 bytes, so it costs one byte per texel. It is
// the highest quality desktop block format for 8-bit colour, and it is the
// default rung for every colour role.
//
// The sRGB entry pairs with VK_FORMAT_BC7_SRGB_BLOCK and the linear entry with
// VK_FORMAT_BC7_UNORM_BLOCK. bc7.ColorSpace.VkFormat returns the same numbers
// from the encoder package, and TestBlockCodecVkFormatMatchesEncoderPackage
// compares the two, so the pairing has a witness outside this file.
func init() {
	RegisterBlockCodec(BlockCodec{
		ID:          "bc7-rgba-unorm-srgb",
		Format:      "bc7-rgba-unorm-srgb",
		VkFormat:    ktx2.VkFormatBC7SRGBBlock,
		ColorSpace:  SRGB,
		BlockWidth:  4,
		BlockHeight: 4,
		BlockBytes:  16,
		Roles:       []Role{RoleBaseColor, RoleEmissive},
		// BC7 keeps eight alpha bits, so it serves an opaque and a
		// transparent colour map alike.
		NeedsAlpha: true,
		Encode: func(level *Image, quality BlockQuality) ([]byte, error) {
			return bc7.Encode(bc7Source(level), bc7.Options{
				Space:   bc7.SRGB,
				Quality: bc7Quality(quality),
			})
		},
	})
	RegisterBlockCodec(BlockCodec{
		ID:          "bc7-rgba-unorm",
		Format:      "bc7-rgba-unorm",
		VkFormat:    ktx2.VkFormatBC7UnormBlock,
		ColorSpace:  Linear,
		BlockWidth:  4,
		BlockHeight: 4,
		BlockBytes:  16,
		// A packed data map keeps every channel, so BC7 with a linear
		// transfer is the answer that needs no renderer change. A BC5 repack
		// would halve the bitrate and needs a channel swizzle first.
		Roles:      []Role{RolePacked},
		NeedsAlpha: true,
		Encode: func(level *Image, quality BlockQuality) ([]byte, error) {
			return bc7.Encode(bc7Source(level), bc7.Options{
				Space:   bc7.Linear,
				Quality: bc7Quality(quality),
			})
		},
	})
}

// bc7Source wraps one Image as a bc7.Source without copying pixels.
//
// bc7.Source repeats the memory layout of Image on purpose, because package bc7
// cannot import this package. Sharing the slice keeps the wrapper free.
func bc7Source(level *Image) bc7.Source {
	if level == nil {
		return bc7.Source{}
	}
	return bc7.Source{Width: level.Width, Height: level.Height, Pix: level.Pix}
}

// bc7Quality maps the pipeline level onto the bc7 level.
func bc7Quality(q BlockQuality) bc7.Quality {
	switch q {
	case BlockQualityFast:
		return bc7.QualityFast
	case BlockQualityBest:
		return bc7.QualityBest
	}
	return bc7.QualityBalanced
}

// BC7VkFormatFor returns the BC7 VkFormat the encoder package pairs with one
// colour space.
//
// The function exists as an oracle for the codec table above. The encoder
// package owns the pairing, so a test can compare our registered VkFormat with
// the encoder's own answer instead of with a second copy of the same constant.
func BC7VkFormatFor(space ColorSpace) int {
	if space == SRGB {
		return bc7.SRGB.VkFormat()
	}
	return bc7.Linear.VkFormat()
}

// BC7EncodedSize returns the payload size one BC7 level costs, through the
// encoder package's own arithmetic. It is the oracle for BlockChainLevelBytes.
func BC7EncodedSize(width, height int) int {
	return bc7.EncodedSize(width, height)
}
