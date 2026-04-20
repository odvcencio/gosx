// Package ktx2 implements a KTX2 container parser for loading texture
// assets into GoSX's render/bundle.Renderer.
//
// KTX2 (Khronos Texture 2.0) is an open, block-compressed texture format
// that ships a VkFormat field + supercompressed mip data + a full
// descriptor block. See https://registry.khronos.org/KTX/specs/2.0/ktxspec.v2.html
// for the canonical spec.
//
// # Scope today
//
// Supported in R3:
//   - Uncompressed RGBA8 / sRGB formats (VkFormat 37, 43).
//   - Zlib (DEFLATE) supercompression (scheme 3), via compress/zlib.
//   - Arbitrary mip level counts.
//   - Single-layer 2D textures.
//
// Explicitly unsupported (raised as ErrUnsupportedFormat):
//   - Basis Universal supercompression (scheme 1) — a transcoder can be
//     supplied via RegisterBasisTranscoder when/if a Basis Go port lands;
//     the parser hands raw BasisLZ payloads to it unchanged.
//   - Zstandard (scheme 2) — pure-Go zstd isn't in stdlib; bring-your-own.
//   - Block-compressed formats (BC7, ASTC, ETC2) — R4/R5 work.
//   - 3D, cubemap, array textures — R5 work.
//
// # Breakout contract
//
// Everything inside this package is self-contained. If KTX2 growth
// justifies it, the package can lift to a sibling module with zero
// impact on render/bundle: render/bundle/texture.go only depends on
// Parse + Image + Level.
package ktx2
