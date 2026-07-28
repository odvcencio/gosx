// Package texture builds GPU-ready raster texture variants in pure Go.
//
// # What the package does
//
// The package decodes a PNG or JPEG source, resizes it to a power of two and
// to a per-tier ceiling, builds a mip chain, packs channels, prunes an unused
// alpha channel, and writes a KTX2 container with the whole mip chain.
//
// # Block compression
//
// BuildOptions.BlockCompression adds one block-compressed variant per tier. The
// role decides the format, because the role is what the pixels mean:
//
//   - A tangent-space normal map takes BC5 through the normalizing encoder. A
//     shader must rebuild z, which the container records in
//     GoSXtextureShaderWork.
//   - A single-channel mask takes BC4, at half the bitrate of BC7 and higher
//     quality on one channel.
//   - Base colour takes BC7, or BC3 on the low tier with alpha, or BC1 on the
//     low tier without.
//   - A packed data map takes BC7 with a linear transfer, which needs no
//     renderer change.
//
// The uncompressed variant always ships as well. Every block format sits behind
// an optional WebGPU feature and an optional WebGL2 extension, so a device may
// have none of them. A block variant therefore carries both its format token and
// the device-feature token that format needs, and only device evidence can
// satisfy the second one.
//
// The colour space is the trap. An sRGB codec must pair with an sRGB VkFormat and
// a linear codec with a unorm one, or the sampler inverts a curve the encoder
// never applied. EncodeBlockKTX2 refuses a mismatched pair, and
// TestBlockCodecAppliesTheTransferFunctionItDeclares reads the stored codes back
// to check what the encoder really did.
//
// # Linear light, always
//
// Every resample and every mip level runs on linear-light float32 pixels. An
// sRGB source decodes through the IEC 61966-2-1 transfer function on the way
// in and encodes back on the way out. A filter that averages sRGB code values
// darkens each mip level, because the transfer function is convex. The
// package never does that. TestMipChainKeepsLinearMean asserts it.
//
// # What the package refuses
//
// ASTC and ETC2 need their own block encoders, which this repository does not
// have. The KTX2 writer refuses those formats with ErrEncodeBlockCompressed
// rather than emitting a container whose payload nobody filled in. BC6H, for
// high dynamic range, is refused for the same reason.
//
// No rate-distortion optimization pass exists. Rate-distortion trades a little
// quality for smaller supercompressed bytes; it never changes GPU bytes. The
// encoder packages reject a non-zero setting rather than ignore it.
//
// WebP and AVIF sources also fail, with ErrUnsupportedSource. The standard
// library decodes neither, and the package takes no third-party decoder.
//
// # Bounds
//
// Decode refuses an image above MaxPixels. The bound exists because the
// package holds pixels as linear float32 RGBA, at 16 bytes per pixel. See the
// MaxPixels comment for the memory arithmetic.
package texture
