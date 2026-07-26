// Package bcn encodes 4x4 block-compressed textures: BC1, BC3, BC4 and BC5.
//
// # What the package does
//
// The package takes linear-light float32 RGBA pixels and returns the block
// payload bytes a GPU uploads. It also carries a spec-exact decoder for every
// format it writes, so a test can check the payload without a GPU.
//
// # Why the package holds its own surface type
//
// Surface repeats the memory layout of texture.Image on purpose. Package
// texture calls this package, so this package must not call package texture.
// A Surface shares the pixel slice, so the wrapper costs no copy:
//
//	payload, err := bcn.EncodeBC1(&bcn.Surface{
//		Width:  img.Width,
//		Height: img.Height,
//		Pix:    img.Pix,
//	}, opts)
//
// The sRGB math in transfer.go repeats the math in texture/srgb.go for the same
// reason. TestTransferMatchesReference checks both against one reference
// implementation of IEC 61966-2-1.
//
// # Name the transfer function
//
// Surface holds linear light. BC1 and BC3 store 8-bit colour codes, and the GPU
// applies the sRGB transfer function to those codes when the caller picks an
// sRGB VkFormat. So the encoder must know which function to apply on the way in:
//
//   - Pick TransferSRGB for a colour target. Base colour and emissive land here.
//     The stored codes then carry the sRGB transfer function, which is what
//     VK_FORMAT_BC1_RGB_SRGB_BLOCK and VK_FORMAT_BC3_SRGB_BLOCK expect.
//   - Pick TransferUnorm for a data target. Normal, roughness, metalness and
//     occlusion land here. Their numbers are already linear.
//
// The zero value of Transfer is invalid and every encoder rejects it. A wrong
// guess darkens or brightens every texel, and no later stage can find the
// mistake. Compare the failure with TestMipChainKeepsLinearMean in package
// texture, which exists to catch the same class of error.
//
// BC4 and BC5 have no sRGB VkFormat, so they accept TransferUnorm only.
//
// The alpha channel never passes through a transfer function. The sRGB
// specification applies the function to the three colour channels only.
//
// # Error space
//
// The encoder minimizes squared error over the stored 8-bit codes, which is the
// same measurement the tests report. So a reported gain in decibels is a real
// gain, not a metric mismatch.
//
// For a colour target the stored codes are sRGB codes, so squared error over
// them is already perceptually weighted. For a data target the stored codes are
// linear, which is the right space for a number. For BC5 normals, measure
// degrees with AngularErrorBC5, because a small channel error near the
// silhouette is a large angular error.
//
// # Reference for a signal-to-noise ratio
//
// Every peak signal-to-noise ratio (PSNR) in this package compares the decoded
// codes against ReferenceCodes, which is what an uncompressed 8-bit upload of
// the same surface would hold. The reference isolates block error from the
// 8-bit quantization both paths share.
package bcn
