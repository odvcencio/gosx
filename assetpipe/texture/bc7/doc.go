// Package bc7 encodes and decodes BC7 texture blocks in pure Go.
//
// # What BC7 is
//
// BC7 stores one 4 by 4 texel block in 16 bytes, so it costs exactly one byte
// per texel. It is the highest quality desktop block format for 8-bit colour.
// Every block picks one of eight modes. A mode fixes the subset count, the
// partition table, the endpoint precision, the parity bit layout, and the
// index width. See tables.go for the mode table and the field order.
//
// # Where the transfer function goes
//
// The encoder takes linear-light float32 pixels, the same layout the
// assetpipe/texture package holds. BC7 stores integer endpoints, and the GPU
// reads them through the sRGB transfer function when the sRGB VkFormat is
// bound. So the encoder must convert linear light to 8-bit codes with the same
// transfer function the GPU will invert.
//
// Options.Space names that choice, and Encode refuses a zero Space. A guess is
// not acceptable here. Encoding an sRGB colour map with the unorm path stores
// codes that are too small, the GPU then applies the sRGB decode a second
// time, and every texture comes out dark. Encoding a normal map with the sRGB
// path bends the vectors instead.
//
// # Error space
//
// The encoder measures error on the 8-bit codes it will store, after the
// transfer function. That is the right space for a colour map, because equal
// linear error is not equal perceived error: the sRGB curve spends more codes
// on the dark end, where the eye is more sensitive. For a data map the codes
// are already linear and the same rule holds trivially.
//
// # This package does not import texture
//
// The assetpipe/texture package calls this one, so this one cannot call back.
// Source mirrors texture.Image field for field, and the sRGB helpers are
// duplicated on purpose. srgb_test.go pins this copy to absolute values from
// IEC 61966-2-1 rather than to the other copy, so the two cannot drift apart
// silently.
//
// # Rate-distortion optimization
//
// The package does not implement rate-distortion optimization, and Encode
// rejects a non-zero RDO.Lambda. See the RDOOptions comment for the hook.
package bc7
