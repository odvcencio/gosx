// Package texture builds GPU-ready raster texture variants in pure Go.
//
// # What the package does
//
// The package decodes a PNG or JPEG source, resizes it to a power of two and
// to a per-tier ceiling, builds a mip chain, packs channels, prunes an unused
// alpha channel, and writes a KTX2 container with the whole mip chain.
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
// The package writes uncompressed KTX2 payloads only. BC7, ASTC, and ETC2 all
// need a rate-distortion block encoder, which this repository does not have.
// The KTX2 writer refuses those formats with ErrEncodeBlockCompressed rather
// than emitting a container whose payload nobody filled in.
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
