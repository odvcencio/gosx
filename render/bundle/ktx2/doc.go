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
// Supported:
//   - Uncompressed RGBA8 / BGRA8, including sRGB variants.
//   - Block-compressed pass-through formats: BC7, ASTC 4x4 / 6x6 / 8x8,
//     and ETC2 RGB/RGBA8, including sRGB variants.
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
// # Breakout contract
//
// Everything inside this package is self-contained. If KTX2 growth
// justifies it, the package can lift to a sibling module with zero
// impact on render/bundle: render/bundle/texture.go only depends on
// Parse + Image + Level.
package ktx2
