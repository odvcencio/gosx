// Package ktx2 implements a KTX2 container parser and writer for texture
// assets in GoSX's render/bundle.Renderer.
//
// KTX2 (Khronos Texture 2.0) is an open, block-compressed texture format
// that ships a VkFormat field + supercompressed mip data + a full
// descriptor block. See https://registry.khronos.org/KTX/specs/2.0/ktxspec.v2.html
// for the canonical spec.
//
// # Scope today
//
// Supported by Parse:
//   - Uncompressed RGBA8 / BGRA8, including sRGB variants.
//   - Narrow uncompressed formats: R8, RG8, and RGB8, with the sRGB variants
//     of R8, RG8, and RGB8. The texture build step writes R8 for a grayscale
//     map whose alpha channel is unused, and RGB8 for an opaque colour map on
//     a WebGL2 consumer. WebGPU has no three-channel 8-bit format, so an RGB8
//     variant must carry a capability gate.
//   - Half-float and float formats: R16F, RG16F, RGBA16F, R32F, RG32F,
//     and RGBA32F. The asset pipeline writes prefiltered environment maps
//     as RGBA16F and the split-sum lookup table as RG16F.
//   - Block-compressed pass-through formats: BC1 (with and without alpha),
//     BC3, BC4, BC5, BC7, ASTC 4x4 / 6x6 / 8x8, and ETC2 RGB/RGBA8, including
//     the sRGB variants each format has. BC4 and BC5 have no sRGB variant,
//     because they store data and not colour.
//   - Zlib (DEFLATE) supercompression (scheme 3), via compress/zlib.
//   - Arbitrary mip level counts.
//   - 2D, 2D array, cubemap, cubemap-array, and 3D texture metadata.
//
// Explicitly unsupported (raised as ErrUnsupportedFormat):
//   - Basis Universal supercompression (scheme 1) — a transcoder can be
//     supplied via RegisterBasisTranscoder when/if a Basis Go port lands;
//     the parser hands raw BasisLZ payloads to it unchanged.
//   - Zstandard (scheme 2) — pure-Go zstd isn't in stdlib; bring-your-own.
//
// # Writing
//
// Encode writes a spec-shaped container: the 12-byte identifier, the
// 68-byte header, the level index, a Basic Data Format Descriptor, a
// key/value block, and the level payloads ordered from the smallest mip to
// the largest. It supports the uncompressed formats above, with either
// plain payloads or zlib supercompression.
//
// Encode also writes the BC block family: BC1, BC3, BC4, BC5, and BC7. The
// descriptor of each one was checked byte for byte against a file KTX-Software
// 4.4.2 wrote for the same VkFormat; testdata holds those files and
// TestBlockDescriptorMatchesKhronosGolden compares them.
//
// Encode refuses a block format it has no descriptor for, with
// ErrEncodeBlockCompressed, which wraps ErrEncodeFormat. ASTC and ETC2 sit there
// today: no GoSX encoder writes those payloads, and a container that named ASTC
// over a payload nobody compressed would upload as noise. Adding an encoder, not
// the container, is the work that unlocks them.
//
// Encode refuses a level-0 size that is not a whole number of texel blocks, with
// ErrEncodeBlockAlignment. WebGPU createTexture validates the same rule, so a
// file that broke it would fail in the browser instead of at build time. Mip
// levels below the block size stay legal: the GPU derives them and pads the last
// block.
//
// A three-byte texel makes rows of the last two mip levels unaligned to four
// bytes. A WebGL2 consumer of an RGB8 container must set UNPACK_ALIGNMENT to 1
// before texImage2D. The texture build step records that in the container's
// GoSXtextureUnpackAlignment key.
//
// # Breakout contract
//
// Everything inside this package is self-contained. If KTX2 growth
// justifies it, the package can lift to a sibling module with zero
// impact on render/bundle: render/bundle/texture.go only depends on
// Parse + Image + Level.
package ktx2
