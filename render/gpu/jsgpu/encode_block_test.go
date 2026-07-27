package jsgpu

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/render/bundle"
	"m31labs.dev/gosx/render/bundle/ktx2"
	"m31labs.dev/gosx/render/gpu"
)

// khronosFixtureFormats lists the VkFormat of every reference container in
// render/bundle/ktx2/testdata.
//
// KTX-Software 4.4.2 wrote those files with "ktx create --raw", so the numbers
// come from the Khronos reference implementation and not from this repository.
// A reader that agrees with them is correct rather than merely self-consistent.
func khronosFixtureFormats() map[string]int {
	return map[string]int{
		"bc1.ktx2":   ktx2.VkFormatBC1RGBUnormBlock,
		"bc1s.ktx2":  ktx2.VkFormatBC1RGBSRGBBlock,
		"bc1a.ktx2":  ktx2.VkFormatBC1RGBAUnormBlock,
		"bc1as.ktx2": ktx2.VkFormatBC1RGBASRGBBlock,
		"bc3.ktx2":   ktx2.VkFormatBC3UnormBlock,
		"bc3s.ktx2":  ktx2.VkFormatBC3SRGBBlock,
		"bc4.ktx2":   ktx2.VkFormatBC4UnormBlock,
		"bc5.ktx2":   ktx2.VkFormatBC5UnormBlock,
		"bc7.ktx2":   ktx2.VkFormatBC7UnormBlock,
		"bc7s.ktx2":  ktx2.VkFormatBC7SRGBBlock,
	}
}

// TestEveryKTX2BlockFormatEncodesAWebGPUName closes the gap that made this test
// necessary.
//
// The KTX2 loader mapped BC1, BC3, BC4 and BC5 onto gpu.TextureFormat values,
// and encodeTextureFormat had a case for BC7 only. CreateTexture therefore built
// a descriptor whose "format" was the empty string, and the browser threw on a
// file the build produced. The two tables lived in two packages with no test
// across them, so nothing caught it.
func TestEveryKTX2BlockFormatEncodesAWebGPUName(t *testing.T) {
	for file, vkFormat := range khronosFixtureFormats() {
		format, ok := bundle.KTX2UploadFormat(vkFormat)
		if !ok {
			t.Errorf("%s: the KTX2 loader refuses vkFormat %d", file, vkFormat)
			continue
		}
		name := encodeTextureFormat(format)
		if name == "" {
			t.Errorf("%s: vkFormat %d encodes to an empty WebGPU format name", file, vkFormat)
			continue
		}
		if !strings.HasPrefix(name, "bc") {
			t.Errorf("%s: vkFormat %d encodes to %q, which does not name a BC format", file, vkFormat, name)
		}
	}
}

// TestBlockFormatSpellingsFollowWebGPU pins each name against the WebGPU
// GPUTextureFormat enumeration. A misspelled name reaches createTexture, and the
// browser rejects it at run time with no compiler complaint.
//
// BC1 without alpha and BC1 with one alpha bit share one name on purpose: WebGPU
// has no opaque BC1 format, and an opaque payload decodes the same under either.
func TestBlockFormatSpellingsFollowWebGPU(t *testing.T) {
	want := map[int]string{
		ktx2.VkFormatBC1RGBUnormBlock:  "bc1-rgba-unorm",
		ktx2.VkFormatBC1RGBAUnormBlock: "bc1-rgba-unorm",
		ktx2.VkFormatBC1RGBSRGBBlock:   "bc1-rgba-unorm-srgb",
		ktx2.VkFormatBC1RGBASRGBBlock:  "bc1-rgba-unorm-srgb",
		ktx2.VkFormatBC3UnormBlock:     "bc3-rgba-unorm",
		ktx2.VkFormatBC3SRGBBlock:      "bc3-rgba-unorm-srgb",
		ktx2.VkFormatBC4UnormBlock:     "bc4-r-unorm",
		ktx2.VkFormatBC5UnormBlock:     "bc5-rg-unorm",
		ktx2.VkFormatBC7UnormBlock:     "bc7-rgba-unorm",
		ktx2.VkFormatBC7SRGBBlock:      "bc7-rgba-unorm-srgb",
	}
	for vkFormat, spelling := range want {
		format, ok := bundle.KTX2UploadFormat(vkFormat)
		if !ok {
			t.Fatalf("the KTX2 loader refuses vkFormat %d", vkFormat)
		}
		if got := encodeTextureFormat(format); got != spelling {
			t.Errorf("vkFormat %d encodes to %q, want %q", vkFormat, got, spelling)
		}
	}
	// BC4 and BC5 have no sRGB form in Vulkan or in WebGPU, so no name may spell
	// one. They store data and an sRGB curve would bend the numbers.
	for _, format := range []gpu.TextureFormat{gpu.FormatBC4RUnorm, gpu.FormatBC5RGUnorm} {
		if name := encodeTextureFormat(format); strings.HasSuffix(name, "-srgb") {
			t.Errorf("%q spells an sRGB form of a format that has none", name)
		}
	}
}

// TestEveryBlockFormatNamesItsDeviceFeature keeps the CreateTexture guard whole.
//
// CreateTexture refuses a compressed format when the device lacks the feature
// that unlocks it. A format the table forgets slips past that guard and reaches
// the browser, which is exactly what happened to BC1, BC3, BC4 and BC5.
func TestEveryBlockFormatNamesItsDeviceFeature(t *testing.T) {
	families := map[gpu.TextureFormat]string{
		gpu.FormatBC1RGBAUnorm:       "texture-compression-bc",
		gpu.FormatBC1RGBAUnormSRGB:   "texture-compression-bc",
		gpu.FormatBC3RGBAUnorm:       "texture-compression-bc",
		gpu.FormatBC3RGBAUnormSRGB:   "texture-compression-bc",
		gpu.FormatBC4RUnorm:          "texture-compression-bc",
		gpu.FormatBC5RGUnorm:         "texture-compression-bc",
		gpu.FormatBC7RGBAUnorm:       "texture-compression-bc",
		gpu.FormatBC7RGBAUnormSRGB:   "texture-compression-bc",
		gpu.FormatASTC4x4Unorm:       "texture-compression-astc",
		gpu.FormatASTC4x4UnormSRGB:   "texture-compression-astc",
		gpu.FormatASTC6x6Unorm:       "texture-compression-astc",
		gpu.FormatASTC6x6UnormSRGB:   "texture-compression-astc",
		gpu.FormatASTC8x8Unorm:       "texture-compression-astc",
		gpu.FormatASTC8x8UnormSRGB:   "texture-compression-astc",
		gpu.FormatETC2RGB8Unorm:      "texture-compression-etc2",
		gpu.FormatETC2RGB8UnormSRGB:  "texture-compression-etc2",
		gpu.FormatETC2RGBA8Unorm:     "texture-compression-etc2",
		gpu.FormatETC2RGBA8UnormSRGB: "texture-compression-etc2",
	}
	for format, feature := range families {
		if got := textureCompressionFeature(format); got != feature {
			t.Errorf("%q maps to feature %q, want %q", encodeTextureFormat(format), got, feature)
		}
	}
	// An uncompressed format needs no optional feature. One named here would
	// make CreateTexture refuse a format every device has.
	for _, format := range []gpu.TextureFormat{
		gpu.FormatRGBA8Unorm, gpu.FormatRGBA8UnormSRGB, gpu.FormatR8Unorm,
		gpu.FormatRG8Unorm, gpu.FormatRGBA16Float, gpu.FormatDepth32Float,
	} {
		if feature := textureCompressionFeature(format); feature != "" {
			t.Errorf("uncompressed format %q claims device feature %q", encodeTextureFormat(format), feature)
		}
	}
}

// TestBlockTextureFeaturesCoverEveryFamily proves the device request names every
// family the format tables can produce.
//
// A family missing from the request list is lost for the life of the device,
// because WebGPU forbids adding a feature after requestDevice.
func TestBlockTextureFeaturesCoverEveryFamily(t *testing.T) {
	requested := map[string]bool{}
	for _, name := range blockTextureFeatures() {
		requested[name] = true
	}
	for _, format := range []gpu.TextureFormat{
		gpu.FormatBC1RGBAUnorm, gpu.FormatBC3RGBAUnorm, gpu.FormatBC4RUnorm,
		gpu.FormatBC5RGUnorm, gpu.FormatBC7RGBAUnorm,
		gpu.FormatASTC4x4Unorm, gpu.FormatASTC8x8Unorm,
		gpu.FormatETC2RGB8Unorm, gpu.FormatETC2RGBA8Unorm,
	} {
		feature := textureCompressionFeature(format)
		if feature == "" {
			t.Errorf("%q names no device feature", encodeTextureFormat(format))
			continue
		}
		if !requested[feature] {
			t.Errorf("%q needs %q, which the device request never asks for", encodeTextureFormat(format), feature)
		}
	}
}
