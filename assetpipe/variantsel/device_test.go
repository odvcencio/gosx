package variantsel

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

// allBlockFormats lists every block-compressed format token the vocabulary spells.
func allBlockFormats() []Token {
	return []Token{
		FormatBC1, FormatBC1SRGB, FormatBC3, FormatBC3SRGB,
		FormatBC4, FormatBC5, FormatBC7, FormatBC7Linear,
		FormatETC2, FormatETC2RGB, FormatEACR11, FormatEACRG11,
		FormatASTC4x4, FormatASTC4x4Linear, FormatASTC8x8,
	}
}

// TestBackendTableHoldsNoBlockFormat is the rule that keeps FromBackendCaps
// honest without any code in FromBackendCaps.
//
// backendFormats holds guarantees. Every block format is an optional WebGPU
// feature and an optional WebGL2 extension, so none of them is a guarantee. One
// block token in that table would let a static server verdict promise a format
// the device may not have, and the failure would land in the browser.
func TestBackendTableHoldsNoBlockFormat(t *testing.T) {
	blocks := map[Token]bool{}
	for _, format := range allBlockFormats() {
		blocks[format] = true
	}
	for backend, tokens := range backendFormats {
		for _, token := range tokens {
			if blocks[token] {
				t.Errorf("backend %s promises block format %s, which is optional on every device", backend, token)
			}
			// Catch a new block token nobody added to allBlockFormats.
			for _, marker := range []string{"bc1-", "bc3-", "bc4-", "bc5-", "bc6", "bc7-", "etc2", "eac-", "astc"} {
				if strings.Contains(string(token), marker) {
					t.Errorf("backend %s promises %s, which names a block format", backend, token)
				}
			}
		}
	}

	// The same statement from the other side: a verdict naming every backend
	// still carries no block token.
	for _, caps := range []capability.BackendCaps{
		{Capable: []capability.Backend{capability.BackendWebGPU}},
		{Capable: []capability.Backend{capability.BackendWebGL}},
		{Capable: []capability.Backend{capability.BackendWebGPU, capability.BackendWebGL, capability.BackendCanvas2D}},
	} {
		set := FromBackendCaps(caps)
		for _, format := range allBlockFormats() {
			if set.Has(format) {
				t.Errorf("a verdict for %v carries %s", caps.Capable, format)
			}
		}
		for _, feature := range []Token{
			FeatureTextureCompressionBC, FeatureTextureCompressionETC2, FeatureTextureCompressionASTC,
		} {
			if set.Has(feature) {
				t.Errorf("a verdict for %v carries %s", caps.Capable, feature)
			}
		}
	}
}

// TestFromDeviceEvidenceUnlocksOnlyWhatTheDeviceReported checks the WebGPU path.
//
// An adapter reports its optional features by name. Each name unlocks exactly its
// own family and nothing else, because a device with ASTC and no BC is the normal
// case on a phone.
func TestFromDeviceEvidenceUnlocksOnlyWhatTheDeviceReported(t *testing.T) {
	cases := []struct {
		names   []string
		present []Token
		absent  []Token
	}{
		{
			names:   []string{"texture-compression-bc"},
			present: []Token{FeatureTextureCompressionBC, FormatBC1, FormatBC4, FormatBC5, FormatBC7, FormatBC7Linear},
			absent:  []Token{FeatureTextureCompressionASTC, FeatureTextureCompressionETC2, FormatETC2, FormatASTC4x4, FormatEACR11},
		},
		{
			names:   []string{"texture-compression-etc2"},
			present: []Token{FeatureTextureCompressionETC2, FormatETC2, FormatETC2RGB, FormatEACR11, FormatEACRG11},
			absent:  []Token{FeatureTextureCompressionBC, FormatBC7, FormatBC4, FormatASTC4x4},
		},
		{
			names:   []string{"texture-compression-astc", "texture-compression-etc2"},
			present: []Token{FeatureTextureCompressionASTC, FeatureTextureCompressionETC2, FormatASTC4x4, FormatASTC8x8, FormatETC2},
			absent:  []Token{FeatureTextureCompressionBC, FormatBC1, FormatBC7},
		},
		{
			// An empty report is the honest zero: an adapter with no optional
			// texture feature keeps the uncompressed formats and nothing else.
			names:   nil,
			present: []Token{ContainerKTX2, ContainerZlib, FormatRGBA8UnormSRGB, FormatR8Unorm},
			absent:  allBlockFormats(),
		},
		{
			// An unknown name must be ignored, not guessed at.
			names:   []string{"texture-compression-xyz", "some-future-feature"},
			present: []Token{ContainerKTX2},
			absent:  allBlockFormats(),
		},
	}
	for _, tc := range cases {
		set := FromDeviceEvidence(capability.BackendWebGPU, tc.names)
		for _, token := range tc.present {
			if !set.Has(token) {
				t.Errorf("%v: %s is missing", tc.names, token)
			}
		}
		for _, token := range tc.absent {
			if set.Has(token) {
				t.Errorf("%v: %s must not be present", tc.names, token)
			}
		}
		// Every set must name its backend and must carry the uncompressed
		// fallback, whatever the device reported.
		if !set.Has(BackendToken(capability.BackendWebGPU)) {
			t.Errorf("%v: the set does not name its backend", tc.names)
		}
		if !set.Has(FormatRGBA8UnormSRGB) {
			t.Errorf("%v: the uncompressed fallback is missing", tc.names)
		}
		// WebGPU has no three-channel 8-bit format, so no evidence may unlock
		// the WebGL2-only rgb8unorm variant.
		if set.Has(FormatRGB8Unorm) || set.Has(FormatRGB8UnormSRGB) {
			t.Errorf("%v: a WebGPU set carries rgb8unorm, which WebGPU has no format for", tc.names)
		}
	}
}

// TestFromDeviceEvidenceSplitsTheWebGL2Extensions checks the WebGL2 path.
//
// WebGL2 splits the BC family across three extensions. A context can have S3TC and
// not RGTC, so the whole-family feature token appears only when every format of
// the family is present. A variant carries both tokens, so a partial family stays
// unselectable rather than half selected.
func TestFromDeviceEvidenceSplitsTheWebGL2Extensions(t *testing.T) {
	s3tcOnly := FromDeviceEvidence(capability.BackendWebGL, []string{
		"WEBGL_compressed_texture_s3tc", "WEBGL_compressed_texture_s3tc_srgb",
	})
	for _, token := range []Token{FormatBC1, FormatBC1SRGB, FormatBC3, FormatBC3SRGB} {
		if !s3tcOnly.Has(token) {
			t.Errorf("S3TC did not unlock %s", token)
		}
	}
	for _, token := range []Token{FormatBC4, FormatBC5, FormatBC7, FormatBC7Linear} {
		if s3tcOnly.Has(token) {
			t.Errorf("S3TC unlocked %s, which needs a different extension", token)
		}
	}
	if s3tcOnly.Has(FeatureTextureCompressionBC) {
		t.Error("a partial BC family claimed the whole-family feature token")
	}

	rgtc := FromDeviceEvidence(capability.BackendWebGL, []string{"EXT_texture_compression_rgtc"})
	if !rgtc.Has(FormatBC4) || !rgtc.Has(FormatBC5) {
		t.Error("RGTC did not unlock BC4 and BC5")
	}
	if rgtc.Has(FormatBC7) {
		t.Error("RGTC unlocked BC7, which needs BPTC")
	}

	bptc := FromDeviceEvidence(capability.BackendWebGL, []string{"EXT_texture_compression_bptc"})
	if !bptc.Has(FormatBC7) || !bptc.Has(FormatBC7Linear) {
		t.Error("BPTC did not unlock BC7")
	}
	if bptc.Has(FormatBC1) {
		t.Error("BPTC unlocked BC1, which needs S3TC")
	}

	// All four extensions together complete the family.
	full := FromDeviceEvidence(capability.BackendWebGL, []string{
		"WEBGL_compressed_texture_s3tc", "WEBGL_compressed_texture_s3tc_srgb",
		"EXT_texture_compression_rgtc", "EXT_texture_compression_bptc",
	})
	if !full.Has(FeatureTextureCompressionBC) {
		t.Error("a complete BC family did not claim the whole-family feature token")
	}
	// WebGL2 does have three-channel formats, so the set keeps rgb8unorm.
	if !full.Has(FormatRGB8Unorm) {
		t.Error("a WebGL2 set lost rgb8unorm, which WebGL2 uploads")
	}
}

// TestFromDeviceEvidenceGivesCanvas2DNothing states the honest zero. Canvas2D
// uploads no GPU texture at all, so no feature name may unlock a format for it.
func TestFromDeviceEvidenceGivesCanvas2DNothing(t *testing.T) {
	set := FromDeviceEvidence(capability.BackendCanvas2D, []string{
		"texture-compression-bc", "texture-compression-astc", "texture-compression-etc2",
	})
	for _, format := range allBlockFormats() {
		if set.Has(format) {
			t.Errorf("a Canvas2D set carries %s", format)
		}
	}
	if set.Has(FormatRGBA8Unorm) {
		t.Error("a Canvas2D set carries a GPU texture format")
	}
	if !set.Has(BackendToken(capability.BackendCanvas2D)) {
		t.Error("a Canvas2D set must still name its backend")
	}
}

// TestBlockFeatureForFormatCoversEveryBlockToken keeps the two tables in step.
//
// Every block format token must map to exactly one device feature, and every
// uncompressed token to none. A block token with no feature would ship without a
// gate, and a static verdict could then select it.
func TestBlockFeatureForFormatCoversEveryBlockToken(t *testing.T) {
	for _, format := range allBlockFormats() {
		feature, ok := BlockFeatureForFormat(format)
		if !ok {
			t.Errorf("%s maps to no device feature, so nothing gates it", format)
			continue
		}
		found := false
		for _, candidate := range FormatsForFeature(feature) {
			if candidate == format {
				found = true
			}
		}
		if !found {
			t.Errorf("%s maps to %s, which does not list it back", format, feature)
		}
	}
	for _, format := range []Token{
		FormatRGBA8Unorm, FormatRGBA8UnormSRGB, FormatRGB8Unorm, FormatRGB8UnormSRGB,
		FormatRG8Unorm, FormatR8Unorm, FormatRGBA16Float, FormatRG16Float,
	} {
		if feature, ok := BlockFeatureForFormat(format); ok {
			t.Errorf("uncompressed format %s maps to device feature %s", format, feature)
		}
	}
	// The lookup must normalize case, as Set.Has does.
	if _, ok := BlockFeatureForFormat(Token("TEXTURE-FORMAT:BC7-RGBA-UNORM-SRGB")); !ok {
		t.Error("BlockFeatureForFormat did not normalize case")
	}
}

// TestBlockFormatSpellingsFollowWebGPU pins the vocabulary.
//
// The spelling follows the WebGPU GPUTextureFormat enumeration, because that is
// the narrower and more precisely specified of the two backend vocabularies. A
// misspelled token matches nothing and the variant silently never ships.
func TestBlockFormatSpellingsFollowWebGPU(t *testing.T) {
	want := map[Token]string{
		FormatBC1:           "bc1-rgba-unorm",
		FormatBC1SRGB:       "bc1-rgba-unorm-srgb",
		FormatBC3:           "bc3-rgba-unorm",
		FormatBC3SRGB:       "bc3-rgba-unorm-srgb",
		FormatBC4:           "bc4-r-unorm",
		FormatBC5:           "bc5-rg-unorm",
		FormatBC7:           "bc7-rgba-unorm-srgb",
		FormatBC7Linear:     "bc7-rgba-unorm",
		FormatETC2:          "etc2-rgba8unorm-srgb",
		FormatETC2RGB:       "etc2-rgb8unorm-srgb",
		FormatEACR11:        "eac-r11unorm",
		FormatEACRG11:       "eac-rg11unorm",
		FormatASTC4x4:       "astc-4x4-unorm-srgb",
		FormatASTC4x4Linear: "astc-4x4-unorm",
		FormatASTC8x8:       "astc-8x8-unorm-srgb",
	}
	for token, spelling := range want {
		if string(token) != "texture-format:"+spelling {
			t.Errorf("token %q, want texture-format:%s", token, spelling)
		}
	}
	// BC4 and BC5 have no sRGB form in Vulkan or WebGPU, so no token may spell
	// one. They store data, and an sRGB curve would bend the numbers.
	for _, token := range allBlockFormats() {
		name := string(token)
		if (strings.Contains(name, "bc4-") || strings.Contains(name, "bc5-")) && strings.HasSuffix(name, "-srgb") {
			t.Errorf("%s spells an sRGB form of a format that has none", token)
		}
	}
	// Every token must be unique, or two variants collide on one gate.
	seen := map[Token]bool{}
	for _, token := range allBlockFormats() {
		if seen[token] {
			t.Errorf("token %s appears twice", token)
		}
		seen[token] = true
	}
}
