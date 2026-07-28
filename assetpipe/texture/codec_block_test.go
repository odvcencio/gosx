package texture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image/color"
	"math"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"m31labs.dev/gosx/assetpipe/texture/bc7"
	"m31labs.dev/gosx/assetpipe/texture/bcn"
	"m31labs.dev/gosx/render/bundle"
	"m31labs.dev/gosx/render/bundle/ktx2"
)

// dfdTransfer reads the transfer function byte out of a KTX2 container's Basic
// Data Format Descriptor.
//
// The descriptor is the byte the GPU sampler acts on, and
// TestBlockDescriptorMatchesKhronosGolden in package ktx2 pins those bytes
// against files KTX-Software wrote. So reading the descriptor here checks the
// codec table against something outside this package, not against itself.
func dfdTransfer(t *testing.T, data []byte) int {
	t.Helper()
	if len(data) < 80 {
		t.Fatalf("container holds %d bytes, want at least 80", len(data))
	}
	dfdOffset := int(binary.LittleEndian.Uint32(data[12+36:]))
	if dfdOffset+16 > len(data) {
		t.Fatalf("descriptor at %d runs past %d bytes", dfdOffset, len(data))
	}
	word := binary.LittleEndian.Uint32(data[dfdOffset+12:])
	return int((word >> 16) & 0xFF)
}

const (
	// The two transfer function IDs the Khronos Data Format Specification
	// gives. They are duplicated here because package ktx2 keeps them private.
	// TestBlockDescriptorTransferIDsAreRight pins both against a container the
	// writer produced for a format whose transfer is known.
	kdfTransferLinear = 1
	kdfTransferSRGB   = 2
)

// blockTestImage returns a 4x4 linear image with distinct texels, which every
// registered codec accepts.
func blockTestImage(width, height int) *Image {
	img := NewImage(width, height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Keep the values inside the unit range and away from the ends, so
			// no codec clamps and hides a transfer-function mistake.
			r := 0.1 + 0.8*float32(x)/float32(max1(width-1))
			g := 0.1 + 0.8*float32(y)/float32(max1(height-1))
			img.Set(x, y, r, g, 0.5, 1)
		}
	}
	return img
}

func max1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

// TestBlockDescriptorTransferIDsAreRight anchors the two constants above.
//
// rgba8unorm-srgb must write the sRGB ID and rgba8unorm the linear one. If the
// constants were swapped, every pairing test below would pass while asserting
// the opposite of the truth.
func TestBlockDescriptorTransferIDsAreRight(t *testing.T) {
	chain := []*Image{blockTestImage(4, 4)}
	srgb, _, err := EncodeKTX2(chain, EncodeOptions{ColorSpace: SRGB, Channels: 4})
	if err != nil {
		t.Fatal(err)
	}
	linear, _, err := EncodeKTX2(chain, EncodeOptions{ColorSpace: Linear, Channels: 4})
	if err != nil {
		t.Fatal(err)
	}
	if got := dfdTransfer(t, srgb); got != kdfTransferSRGB {
		t.Errorf("rgba8unorm-srgb wrote transfer %d, want %d", got, kdfTransferSRGB)
	}
	if got := dfdTransfer(t, linear); got != kdfTransferLinear {
		t.Errorf("rgba8unorm wrote transfer %d, want %d", got, kdfTransferLinear)
	}
}

// TestBlockCodecPairsColorSpaceWithVkFormat checks the trap the whole feature
// turns on.
//
// A codec that applies the sRGB curve must ship an sRGB VkFormat, and a codec
// that stores linear numbers must ship a unorm one. Get it wrong and the sampler
// inverts a curve the encoder never applied: a normal map bends, a colour map
// goes dark, and every texel looks plausible.
//
// The test reads the descriptor of a real container, so it measures what the file
// says rather than what the table says.
func TestBlockCodecPairsColorSpaceWithVkFormat(t *testing.T) {
	if len(RegisteredBlockCodecs()) == 0 {
		t.Fatal("no block codec is registered; the adapters are not linked")
	}
	for _, id := range RegisteredBlockCodecs() {
		codec, ok := BlockCodecFor(id)
		if !ok {
			t.Fatalf("%s vanished from the registry", id)
		}
		level := blockTestImage(4, 4)
		payload, err := codec.Encode(level, BlockQualityFast)
		if err != nil {
			t.Fatalf("%s: encode: %v", id, err)
		}
		data, err := EncodeBlockKTX2(codec, []*Image{level}, [][]byte{payload}, BlockEncodeOptions{})
		if err != nil {
			t.Fatalf("%s: container: %v", id, err)
		}
		want := kdfTransferLinear
		if codec.ColorSpace == SRGB {
			want = kdfTransferSRGB
		}
		if got := dfdTransfer(t, data); got != want {
			t.Errorf("%s encodes %s but the container says transfer %d, want %d",
				id, codec.ColorSpace, got, want)
		}
		// A data role must never carry the sRGB curve. State it separately, so
		// a future codec cannot pass by pairing sRGB with sRGB on a normal map.
		for _, role := range codec.Roles {
			space, known := ColorSpaceForRole(role)
			if !known {
				t.Errorf("%s serves unknown role %q", id, role)
				continue
			}
			if space != codec.ColorSpace {
				t.Errorf("%s serves role %q, which needs %s, but the codec encodes %s",
					id, role, space, codec.ColorSpace)
			}
		}
	}
}

// TestBlockCodecGeometryMatchesContainer checks the codec table against the
// container's own block table.
//
// The two must agree. A codec that claims 8 bytes per block against a VkFormat
// the container sizes at 16 writes half a texture and the writer's shape check is
// the only thing between that and a shipped file.
func TestBlockCodecGeometryMatchesContainer(t *testing.T) {
	for _, id := range RegisteredBlockCodecs() {
		codec, _ := BlockCodecFor(id)
		info, ok := ktx2.FormatBlockInfo(codec.VkFormat)
		if !ok {
			t.Errorf("%s names vkFormat %d, which the container cannot describe", id, codec.VkFormat)
			continue
		}
		if !info.Compressed {
			t.Errorf("%s names vkFormat %d, which is not a block format", id, codec.VkFormat)
		}
		if codec.BlockWidth != info.Width || codec.BlockHeight != info.Height {
			t.Errorf("%s says %dx%d blocks, the container says %dx%d",
				id, codec.BlockWidth, codec.BlockHeight, info.Width, info.Height)
		}
		if codec.BlockBytes != info.BytesPerBlock {
			t.Errorf("%s says %d bytes per block, the container says %d",
				id, codec.BlockBytes, info.BytesPerBlock)
		}
	}
}

// TestBlockCodecPayloadLengthMatchesGeometry encodes at several sizes and checks
// the payload length against the block arithmetic.
//
// The sizes include one that is not a multiple of four, so the padding rule is
// exercised: a 6x6 level costs four blocks, not two and a bit.
func TestBlockCodecPayloadLengthMatchesGeometry(t *testing.T) {
	sizes := [][2]int{{4, 4}, {8, 4}, {16, 16}, {6, 6}, {12, 20}}
	for _, id := range RegisteredBlockCodecs() {
		codec, _ := BlockCodecFor(id)
		for _, size := range sizes {
			level := blockTestImage(size[0], size[1])
			payload, err := codec.Encode(level, BlockQualityFast)
			if err != nil {
				t.Fatalf("%s at %dx%d: %v", id, size[0], size[1], err)
			}
			want := BlockChainLevelBytes(codec, size[0], size[1])
			if len(payload) != want {
				t.Errorf("%s at %dx%d wrote %d bytes, want %d",
					id, size[0], size[1], len(payload), want)
			}
		}
	}
}

// TestBlockCodecVkFormatMatchesEncoderPackage compares our BC7 VkFormat with the
// number the encoder package returns.
//
// The encoder package owns the pairing and states it in its own type. Comparing
// against that is stronger than comparing against a second copy of the constant.
func TestBlockCodecVkFormatMatchesEncoderPackage(t *testing.T) {
	cases := map[string]ColorSpace{
		"bc7-rgba-unorm-srgb": SRGB,
		"bc7-rgba-unorm":      Linear,
	}
	for id, space := range cases {
		codec, ok := BlockCodecFor(id)
		if !ok {
			t.Fatalf("%s is not registered", id)
		}
		if codec.ColorSpace != space {
			t.Fatalf("%s encodes %s, want %s", id, codec.ColorSpace, space)
		}
		if want := BC7VkFormatFor(space); codec.VkFormat != want {
			t.Errorf("%s names vkFormat %d, the bc7 package pairs %s with %d",
				id, codec.VkFormat, space, want)
		}
	}
	// The bc7 package's own constants must match the container's.
	if bc7.VkFormatBC7UnormBlock != ktx2.VkFormatBC7UnormBlock {
		t.Errorf("bc7 says unorm is %d, the container says %d",
			bc7.VkFormatBC7UnormBlock, ktx2.VkFormatBC7UnormBlock)
	}
	if bc7.VkFormatBC7SRGBBlock != ktx2.VkFormatBC7SRGBBlock {
		t.Errorf("bc7 says srgb is %d, the container says %d",
			bc7.VkFormatBC7SRGBBlock, ktx2.VkFormatBC7SRGBBlock)
	}
}

// TestBlockMipChainBytesMatchesEncoderArithmetic checks the GPU-byte arithmetic
// against a second, independent implementation.
//
// BlockMipChainBytes walks the codec table. bcn.MipChainBytes walks the encoder
// package's own format table. Two implementations that agree are evidence; one
// implementation measuring itself is not.
func TestBlockMipChainBytesMatchesEncoderArithmetic(t *testing.T) {
	sizes := [][2]int{{4, 4}, {64, 64}, {2048, 2048}, {1024, 256}, {512, 8}}
	for _, id := range RegisteredBlockCodecs() {
		codec, _ := BlockCodecFor(id)
		for _, size := range sizes {
			got := BlockMipChainBytes(codec, size[0], size[1])
			if got <= 0 {
				t.Errorf("%s at %dx%d reported %d GPU bytes", id, size[0], size[1], got)
				continue
			}
			if strings.HasPrefix(id, "bc7") {
				// Sum the encoder package's own per-level arithmetic.
				want := 0
				w, h := size[0], size[1]
				for {
					want += BC7EncodedSize(w, h)
					if w <= 1 && h <= 1 {
						break
					}
					w, h = halveEdge(w), halveEdge(h)
				}
				if got != want {
					t.Errorf("%s at %dx%d: %d GPU bytes, the bc7 package sums %d",
						id, size[0], size[1], got, want)
				}
				continue
			}
			want, err := BCNMipChainBytes(id, size[0], size[1])
			if err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			if got != want {
				t.Errorf("%s at %dx%d: %d GPU bytes, the bcn package says %d",
					id, size[0], size[1], got, want)
			}
		}
	}
	// The denominator must agree too, or every ratio is wrong by the same
	// factor and nothing catches it.
	for _, size := range sizes {
		got := PixelMipChainBytes(size[0], size[1], 4)
		if want := BCNRGBA8MipChainBytes(size[0], size[1]); got != want {
			t.Errorf("rgba8 chain at %dx%d: %d bytes, the bcn package says %d",
				size[0], size[1], got, want)
		}
	}
}

// TestRoleForNameReadsTheUsualNames pins the file-name heuristic.
//
// The guess is recorded in the sidecar, so a wrong guess is visible. It still
// must be right on the names an authoring tool actually writes, because the role
// decides the format and the transfer function together.
func TestRoleForNameReadsTheUsualNames(t *testing.T) {
	cases := map[string]Role{
		"tex/hero_normal.png":            RoleNormal,
		"tex/hero_nrm.png":               RoleNormal,
		"tex/wall-Normal-DX.png":         RoleNormal,
		"tex/hero_roughness.png":         RoleMask,
		"tex/wall_ao.png":                RoleMask,
		"tex/floor_height.png":           RoleMask,
		"tex/hero_metallicRoughness.png": RolePacked,
		"tex/crate_orm.png":              RolePacked,
		"tex/lamp_emissive.png":          RoleEmissive,
		"tex/hero_albedo.png":            RoleBaseColor,
		"tex/hero_baseColor.png":         RoleBaseColor,
		"tex/hero_diffuse.jpg":           RoleBaseColor,
		"tex/plate.png":                  RoleUnknown,
	}
	for name, want := range cases {
		if got := RoleForName(name); got != want {
			t.Errorf("RoleForName(%q) = %q, want %q", name, got, want)
		}
	}
	// A packed name contains a mask marker. The packed test must win, because
	// a metallicRoughness map holds two factors and BC4 would drop one.
	if got := RoleForName("hero_metallic_roughness.png"); got != RolePacked {
		t.Errorf("a metallic-roughness map resolved to %q, want %q", got, RolePacked)
	}
}

// TestCodecsForRoleKeepsDataMapsLinear checks the role ladder never hands a data
// map an sRGB codec, and never hands a source with alpha a codec that drops it.
func TestCodecsForRoleKeepsDataMapsLinear(t *testing.T) {
	dataRoles := []Role{RoleNormal, RoleMask, RolePacked}
	for _, role := range dataRoles {
		for _, cheap := range []bool{false, true} {
			ids := CodecsForRole(role, false, cheap)
			if len(ids) == 0 {
				t.Errorf("role %q has no codec", role)
			}
			for _, id := range ids {
				codec, ok := BlockCodecFor(id)
				if !ok {
					t.Errorf("role %q names unregistered codec %q", role, id)
					continue
				}
				if codec.ColorSpace != Linear {
					t.Errorf("role %q may take %q, which encodes %s", role, id, codec.ColorSpace)
				}
				if strings.Contains(codec.Format, "srgb") {
					t.Errorf("role %q may take %q, whose format name says srgb", role, id)
				}
			}
		}
	}
	// A colour role must take an sRGB codec, and the cheap rung must cost fewer
	// bytes per block than the default rung.
	for _, role := range []Role{RoleBaseColor, RoleEmissive} {
		best, _ := BlockCodecFor(CodecsForRole(role, false, false)[0])
		cheap, _ := BlockCodecFor(CodecsForRole(role, false, true)[0])
		if best.ColorSpace != SRGB || cheap.ColorSpace != SRGB {
			t.Errorf("role %q took %s and %s, want srgb for both", role, best.ColorSpace, cheap.ColorSpace)
		}
		if cheap.BlockBytes >= best.BlockBytes {
			t.Errorf("role %q: the cheap rung %q costs %d bytes per block and the default rung %q costs %d",
				role, cheap.ID, cheap.BlockBytes, best.ID, best.BlockBytes)
		}
	}
	// A source with alpha may only take a codec that stores alpha.
	for _, role := range []Role{RoleBaseColor, RoleEmissive} {
		for _, cheap := range []bool{false, true} {
			for _, id := range CodecsForRole(role, true, cheap) {
				codec, _ := BlockCodecFor(id)
				if !codec.NeedsAlpha {
					t.Errorf("role %q with alpha may take %q, which drops alpha", role, id)
				}
			}
		}
	}
	// The normal role must take BC5 through the normalizing encoder. BC7 at the
	// same bitrate is measurably worse on normals; TestBC5NormalBeatsBC7OnNormals
	// reports the numbers.
	if ids := CodecsForRole(RoleNormal, false, false); len(ids) != 1 || ids[0] != "bc5-rg-unorm-normal" {
		t.Errorf("the normal role resolved to %v, want only bc5-rg-unorm-normal", ids)
	}
	if ids := CodecsForRole(RoleMask, false, false); len(ids) != 1 || ids[0] != "bc4-r-unorm" {
		t.Errorf("the mask role resolved to %v, want only bc4-r-unorm", ids)
	}
}

// TestTierLadderAlwaysDividesByTheBlockSize pins the hard WebGPU constraint.
//
// WebGPU createTexture refuses a compressed texture whose level-0 width or height
// is not a multiple of the texel block size. Every format here is 4x4 and the
// ladder rounds to a power of two, so four always divides once the edge reaches
// four. The rule is asserted rather than assumed, because the ladder is code that
// somebody can change.
func TestTierLadderAlwaysDividesByTheBlockSize(t *testing.T) {
	sources := []int{4, 5, 7, 8, 100, 255, 256, 513, 1000, 2048, 4096, 8192}
	for _, tier := range DefaultTiers() {
		for _, width := range sources {
			for _, height := range sources {
				w, h := FitPowerOfTwo(width, height, tier.MaxEdge)
				if w < 4 || h < 4 {
					// A tiny source cannot fill one block. encodeBlockTier
					// skips those tiers and records the reason.
					continue
				}
				if w%4 != 0 || h%4 != 0 {
					t.Errorf("tier %s turned %dx%d into %dx%d, which is not a whole number of 4x4 blocks",
						tier.Name, width, height, w, h)
				}
			}
		}
	}
	// The other half of the rule: the container must refuse an unaligned level
	// 0 rather than write a file the browser rejects.
	codec, _ := BlockCodecFor("bc7-rgba-unorm-srgb")
	level := blockTestImage(6, 8)
	payload, err := codec.Encode(level, BlockQualityFast)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeBlockKTX2(codec, []*Image{level}, [][]byte{payload}, BlockEncodeOptions{}); !errors.Is(err, ktx2.ErrEncodeBlockAlignment) {
		t.Errorf("a 6x8 BC7 level gave %v, want ErrEncodeBlockAlignment", err)
	}
}

// TestBuildSkipsBlockVariantsForTinySources checks the skip is recorded, not
// silent.
func TestBuildSkipsBlockVariantsForTinySources(t *testing.T) {
	src := srgbPNG(t, 2, 2, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x * 100), G: uint8(y * 100), B: 40, A: 255}
	})
	result, err := Build(src, BuildOptions{
		ColorSpace:       SRGB,
		Supercompress:    true,
		BlockCompression: true,
		Source:           "tiny_albedo.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range result.Variants {
		if variant.Block {
			t.Errorf("a 2x2 source produced block variant %q; WebGPU refuses it", variant.Format)
		}
	}
	if len(result.BlockSkipped) == 0 {
		t.Error("the build skipped every block variant and recorded no reason")
	}
	for _, reason := range result.BlockSkipped {
		if !strings.Contains(reason, "block") {
			t.Errorf("the skip reason %q does not say what was skipped", reason)
		}
	}
}

// TestEncodeBlockKTX2RefusesAMismatchedPairing proves the container writer, not
// only the codec table, blocks the sRGB trap.
func TestEncodeBlockKTX2RefusesAMismatchedPairing(t *testing.T) {
	good, _ := BlockCodecFor("bc7-rgba-unorm-srgb")
	level := blockTestImage(4, 4)
	payload, err := good.Encode(level, BlockQualityFast)
	if err != nil {
		t.Fatal(err)
	}
	// Claim the linear VkFormat while keeping the sRGB colour space.
	bad := good
	bad.VkFormat = ktx2.VkFormatBC7UnormBlock
	if _, err := EncodeBlockKTX2(bad, []*Image{level}, [][]byte{payload}, BlockEncodeOptions{}); !errors.Is(err, ErrShape) {
		t.Errorf("an sRGB codec on a unorm VkFormat gave %v, want ErrShape", err)
	}
	// And the other direction.
	bad = good
	bad.ColorSpace = Linear
	if _, err := EncodeBlockKTX2(bad, []*Image{level}, [][]byte{payload}, BlockEncodeOptions{}); !errors.Is(err, ErrShape) {
		t.Errorf("a linear codec on an sRGB VkFormat gave %v, want ErrShape", err)
	}
	// BC4 and BC5 have no sRGB VkFormat at all, so no codec may claim one.
	for _, id := range []string{"bc4-r-unorm", "bc5-rg-unorm", "bc5-rg-unorm-normal"} {
		codec, ok := BlockCodecFor(id)
		if !ok {
			t.Fatalf("%s is not registered", id)
		}
		if codec.ColorSpace != Linear {
			t.Errorf("%s encodes %s; BC4 and BC5 have no sRGB VkFormat", id, codec.ColorSpace)
		}
	}
}

// TestEveryEmittedFormatUploads is the real fix for the latent upload bug.
//
// The pipeline once emitted r8unorm containers the native loader could not
// upload, and nothing noticed, because the emit list and the upload list lived in
// two packages with no test across them. This test walks every format the
// pipeline can emit and asks the loader whether it can upload it. A new codec
// with no loader case fails here, at build time, instead of in a renderer.
func TestEveryEmittedFormatUploads(t *testing.T) {
	type emitted struct {
		vkFormat int
		name     string
		source   string
	}
	var all []emitted

	// The uncompressed side: every channel count and colour space the writer
	// resolves.
	for channels := 1; channels <= 4; channels++ {
		for _, space := range []ColorSpace{Linear, SRGB} {
			format, name, err := VkFormatFor(channels, space)
			if err != nil {
				t.Fatalf("VkFormatFor(%d, %s): %v", channels, space, err)
			}
			all = append(all, emitted{format, name, "VkFormatFor"})
		}
	}
	// The block side: every registered codec.
	for _, id := range RegisteredBlockCodecs() {
		codec, _ := BlockCodecFor(id)
		all = append(all, emitted{codec.VkFormat, codec.Format, "codec " + id})
	}

	if len(all) < 10 {
		t.Fatalf("the emit list holds %d formats, which is too few to be the whole set", len(all))
	}
	for _, item := range all {
		// rgb8unorm is deliberately WebGL2-only: WebGPU has no three-channel
		// 8-bit format, so the native loader rejects it on purpose.
		if strings.HasPrefix(item.name, "rgb8unorm") {
			if _, ok := bundle.KTX2UploadFormat(item.vkFormat); ok {
				t.Errorf("%s (vkFormat %d) uploads, but WebGPU has no three-channel 8-bit format",
					item.name, item.vkFormat)
			}
			continue
		}
		if _, ok := bundle.KTX2UploadFormat(item.vkFormat); !ok {
			t.Errorf("%s emits %s (vkFormat %d) and the native loader cannot upload it",
				item.source, item.name, item.vkFormat)
		}
		// The container must also know the upload geometry, or the loader
		// computes a zero row pitch and refuses at run time.
		if _, ok := ktx2.FormatBlockInfo(item.vkFormat); !ok {
			t.Errorf("%s emits vkFormat %d and the container has no block geometry for it",
				item.source, item.vkFormat)
		}
	}
}

// TestBuildBlockVariantsParseAndUpload runs the whole ladder and checks each
// block file end to end.
func TestBuildBlockVariantsParseAndUpload(t *testing.T) {
	src := srgbPNG(t, 256, 128, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x), G: uint8(y * 2), B: uint8((x + y) / 2), A: 255}
	})
	result, err := Build(src, BuildOptions{
		ColorSpace:       SRGB,
		Supercompress:    true,
		BlockCompression: true,
		BlockQuality:     BlockQualityFast,
		Source:           "hero_albedo.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Role != string(RoleBaseColor) {
		t.Errorf("hero_albedo.png resolved to role %q, want %q", result.Role, RoleBaseColor)
	}
	if !result.RoleGuessed {
		t.Error("the role came from the file name, so RoleGuessed must be true")
	}

	blocks := 0
	for _, plan := range result.Variants {
		if !plan.Block {
			continue
		}
		blocks++
		parsed, err := ktx2.Parse(plan.Data)
		if err != nil {
			t.Fatalf("%s %s: parse: %v", plan.Tier, plan.Format, err)
		}
		if parsed.Format != plan.VkFormat {
			t.Errorf("%s %s: the container says vkFormat %d, the plan says %d",
				plan.Tier, plan.Format, parsed.Format, plan.VkFormat)
		}
		if parsed.Width != plan.Width || parsed.Height != plan.Height {
			t.Errorf("%s %s: container %dx%d, plan %dx%d",
				plan.Tier, plan.Format, parsed.Width, parsed.Height, plan.Width, plan.Height)
		}
		if len(parsed.Levels) != plan.Levels {
			t.Errorf("%s %s: %d levels in the container, %d in the plan",
				plan.Tier, plan.Format, len(parsed.Levels), plan.Levels)
		}
		if _, ok := bundle.KTX2UploadFormat(parsed.Format); !ok {
			t.Errorf("%s %s: the native loader cannot upload vkFormat %d",
				plan.Tier, plan.Format, parsed.Format)
		}
		codec, ok := BlockCodecFor(plan.Codec)
		if !ok {
			t.Fatalf("%s names codec %q, which is not registered", plan.Format, plan.Codec)
		}
		// Every level must hold exactly its blocks. The sum is the GPU cost.
		total := 0
		for i, level := range parsed.Levels {
			want := BlockChainLevelBytes(codec, level.Width, level.Height)
			if len(level.Bytes) != want {
				t.Errorf("%s %s level %d is %dx%d and holds %d bytes, want %d",
					plan.Tier, plan.Format, i, level.Width, level.Height, len(level.Bytes), want)
			}
			total += len(level.Bytes)
		}
		if total != plan.GPUBytes {
			t.Errorf("%s %s: the levels sum to %d bytes, the plan reports %d GPU bytes",
				plan.Tier, plan.Format, total, plan.GPUBytes)
		}
		if plan.GPURatio <= 0 || plan.GPURatio >= 1 {
			t.Errorf("%s %s: GPU ratio %.4f, want a real saving below 1",
				plan.Tier, plan.Format, plan.GPURatio)
		}
	}
	if blocks == 0 {
		t.Fatal("the ladder produced no block variant")
	}
	// The uncompressed variant must survive at every tier. It is the fallback
	// for a device that reported no block feature at all.
	tiers := map[string]bool{}
	for _, plan := range result.Variants {
		if plan.Portable {
			tiers[plan.Tier] = true
		}
	}
	for _, plan := range result.Variants {
		if plan.Block && !tiers[plan.Tier] {
			t.Errorf("tier %s ships a block variant and no portable one", plan.Tier)
		}
	}
}

// TestBlockVariantsCutGPUBytesByTheExpectedFactor reports the measured saving per
// format and checks it against the bitrate of the format.
//
// The expected factor is arithmetic, not a guess: rgba8unorm costs 4 bytes per
// texel, BC1 and BC4 cost half a byte, and BC3, BC5 and BC7 cost one. The mip
// padding of the small levels makes the measured factor slightly worse than the
// ideal, which is why the test allows the measured number to fall short but never
// to exceed.
func TestBlockVariantsCutGPUBytesByTheExpectedFactor(t *testing.T) {
	const size = 1024
	for _, id := range RegisteredBlockCodecs() {
		codec, _ := BlockCodecFor(id)
		got := BlockMipChainBytes(codec, size, size)
		rgba8 := PixelMipChainBytes(size, size, 4)
		ideal := 4.0 * 16.0 / float64(codec.BlockBytes)
		measured := float64(rgba8) / float64(got)
		t.Logf("%-20s %d bytes per block: %9d gpu bytes vs %9d rgba8, %.2fx (ideal %.0fx)",
			id, codec.BlockBytes, got, rgba8, measured, ideal)
		if measured > ideal+0.001 {
			t.Errorf("%s claims %.3fx, which beats the format's own bitrate of %.0fx",
				id, measured, ideal)
		}
		if measured < ideal*0.98 {
			t.Errorf("%s reached only %.3fx of an ideal %.0fx; the mip padding cannot cost that much",
				id, measured, ideal)
		}
	}
}

// normalMapKinds returns the normal maps the format comparison runs on.
//
// One image proves nothing about a format. The set spans what a real material
// library holds: fine noise, stacked sine bumps, gentle surface detail, a sharp
// tiling crease, and a full hemisphere that reaches the silhouette. Each function
// returns a unit normal in world convention.
func normalMapKinds(size int) map[string]func(x, y int) (float64, float64, float64) {
	slope := func(dx, dy float64) (float64, float64, float64) {
		n := math.Sqrt(dx*dx + dy*dy + 1)
		return dx / n, dy / n, 1 / n
	}
	return map[string]func(x, y int) (float64, float64, float64){
		"noise": func(x, y int) (float64, float64, float64) {
			r := rand.New(rand.NewSource(int64(y*size + x)))
			return slope(r.Float64()*1.6-0.8, r.Float64()*1.6-0.8)
		},
		"bumps": func(x, y int) (float64, float64, float64) {
			dx, dy := 0.0, 0.0
			for _, f := range []float64{0.4, 0.13, 0.05} {
				dx += f * math.Cos(float64(x)*f*3) * 2
				dy += f * math.Cos(float64(y)*f*3.7) * 2
			}
			return slope(-dx, -dy)
		},
		"surface-detail": func(x, y int) (float64, float64, float64) {
			return slope(0.15*math.Sin(float64(x)*0.7), 0.15*math.Sin(float64(y)*0.9))
		},
		"crease": func(x, y int) (float64, float64, float64) {
			edge := func(v int) float64 {
				switch m := v % 32; {
				case m < 2:
					return -0.9
				case m > 29:
					return 0.9
				}
				return 0
			}
			return slope(edge(x), edge(y))
		},
		"hemisphere": func(x, y int) (float64, float64, float64) {
			u := (float64(x)+0.5)/float64(size)*2 - 1
			v := (float64(y)+0.5)/float64(size)*2 - 1
			if r2 := u*u + v*v; r2 < 1 {
				return u, v, math.Sqrt(1 - r2)
			}
			return 0, 0, 1
		},
	}
}

// detailNormalMaps names the images on which the role table's BC5 choice must
// win. They are the ones a material library is full of.
//
// The two omitted images are stated rather than dropped. On "crease" the two
// formats tie, because a flat face with a hard edge is exactly the content BC7's
// two-subset modes fit best. On "hemisphere" BC5 wins the mean and loses the
// worst texel, because the rim is where z reconstruction is least stable.
var detailNormalMaps = map[string]bool{"noise": true, "bumps": true, "surface-detail": true}

// TestBC5BeatsBC7OnDetailNormalMaps measures the claim behind the role table.
//
// The role table sends a tangent-space normal map to BC5. The reason is that BC7
// spends bits on a third channel and on alpha that carry nothing, while BC5 gives
// eight whole bytes each to x and y. The cost of BC5 is that a shader must rebuild
// z, and that the rebuild is least stable near the silhouette.
//
// The oracles come from the encoder packages: bcn.AngularErrorBC5 measures BC5 and
// bc7.Decode feeds the same angle measurement for BC7. Neither is this package
// measuring itself.
//
// The unit is degrees of turned normal, which is the unit that matters. A
// per-channel error hides the silhouette texels, where one code of channel error
// turns the normal by several degrees.
func TestBC5BeatsBC7OnDetailNormalMaps(t *testing.T) {
	const size = 128
	bc5Codec, ok := BlockCodecFor("bc5-rg-unorm-normal")
	if !ok {
		t.Fatal("bc5-rg-unorm-normal is not registered")
	}

	for name, shape := range normalMapKinds(size) {
		img := NewImage(size, size)
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				nx, ny, nz := shape(x, y)
				img.Set(x, y, float32(nx*0.5+0.5), float32(ny*0.5+0.5), float32(nz*0.5+0.5), 1)
			}
		}
		// Normalize once, before either encode. Both formats must see the same
		// pixels, or the comparison measures the source drift instead of the
		// formats.
		surface := &bcn.Surface{Width: size, Height: size, Pix: img.Pix}
		bcn.NormalizeNormals(surface)

		bc5Payload, err := bc5Codec.Encode(img, BlockQualityBest)
		if err != nil {
			t.Fatalf("%s: BC5: %v", name, err)
		}
		bc5Err, err := bcn.AngularErrorBC5(surface, bc5Payload)
		if err != nil {
			t.Fatalf("%s: BC5 measurement: %v", name, err)
		}

		bc7Payload, err := bc7.Encode(bc7.Source{Width: size, Height: size, Pix: img.Pix},
			bc7.Options{Space: bc7.Linear, Quality: bc7.QualityBest})
		if err != nil {
			t.Fatalf("%s: BC7: %v", name, err)
		}
		decoded, err := bc7.Decode(bc7Payload, size, size, bc7.Linear)
		if err != nil {
			t.Fatalf("%s: BC7 decode: %v", name, err)
		}
		bc7Mean, bc7P95, bc7Max := angularStats(img.Pix, decoded.Pix)

		// Report every figure. A test that stopped at the mean would hide the
		// silhouette cost of z reconstruction, which is real.
		t.Logf("%-15s BC5 mean %.4f p95 %.4f max %.4f deg | BC7 mean %.4f p95 %.4f max %.4f deg | both %d bytes",
			name, bc5Err.MeanDegrees, bc5Err.P95Degrees, bc5Err.MaxDegrees,
			bc7Mean, bc7P95, bc7Max, len(bc5Payload))

		if len(bc5Payload) != len(bc7Payload) {
			t.Fatalf("%s: payloads are %d and %d bytes; the comparison needs one bitrate",
				name, len(bc5Payload), len(bc7Payload))
		}
		if detailNormalMaps[name] && bc5Err.MeanDegrees >= bc7Mean {
			t.Errorf("%s: BC5 turned normals by %.4f deg and BC7 by %.4f; the role table picks BC5 for detail maps",
				name, bc5Err.MeanDegrees, bc7Mean)
		}
		// BC5 must never be much worse anywhere, or the role choice is wrong
		// for some content the ladder cannot tell apart.
		if bc5Err.MeanDegrees > bc7Mean*1.15 {
			t.Errorf("%s: BC5 mean %.4f deg is more than 15 percent worse than BC7's %.4f",
				name, bc5Err.MeanDegrees, bc7Mean)
		}
		if bc5Err.MaxDegrees > bc7Max {
			t.Logf("%-15s note: BC5 loses on the worst texel, %.4f deg against %.4f. "+
				"That is the price of rebuilding z; it lands on the silhouette.",
				name, bc5Err.MaxDegrees, bc7Max)
		}
	}
}

// angularStats returns the mean, the 95th percentile, and the worst angle between
// two normal maps stored in the usual n*0.5+0.5 encoding.
func angularStats(ref, got []float32) (float64, float64, float64) {
	unit := func(pix []float32, i int) (float64, float64, float64) {
		x := float64(pix[i])*2 - 1
		y := float64(pix[i+1])*2 - 1
		z := float64(pix[i+2])*2 - 1
		length := math.Sqrt(x*x + y*y + z*z)
		if length < 1e-9 {
			return 0, 0, 1
		}
		return x / length, y / length, z / length
	}
	angles := make([]float64, 0, len(ref)/4)
	total, worst := 0.0, 0.0
	for i := 0; i+3 < len(ref) && i+3 < len(got); i += 4 {
		rx, ry, rz := unit(ref, i)
		gx, gy, gz := unit(got, i)
		cosine := math.Min(1, math.Max(-1, rx*gx+ry*gy+rz*gz))
		angle := math.Acos(cosine) * 180 / math.Pi
		angles = append(angles, angle)
		total += angle
		if angle > worst {
			worst = angle
		}
	}
	if len(angles) == 0 {
		return 0, 0, 0
	}
	sort.Float64s(angles)
	return total / float64(len(angles)), angles[int(0.95*float64(len(angles)-1))], worst
}

// TestBC4BeatsBC1AtTheSameBitrateOnAMask measures the mask row of the role table.
//
// The comparison that decides a format is the one at equal bitrate. BC4 and BC1
// both cost 8 bytes per 4x4 block, so they are the two candidates at 4 bits per
// texel. BC1 spends its bits on RGB565 endpoints and 2-bit indices; BC4 spends all
// of them on one channel with 3-bit indices. So BC4 must win on a single-factor
// map, and by how much is a measurement.
//
// BC7 is measured too, and it wins, because it costs twice the GPU bytes. The
// test reports that trade rather than hiding it: the role picks BC4 to halve GPU
// memory, and the decibel gap is the price.
//
// The oracles are bcn.PSNR8 and bcn.MaxAbsError against bcn.ReferenceCodes, which
// is what an uncompressed 8-bit upload would hold. Comparing against that isolates
// the block error from the quantization every path pays.
func TestBC4BeatsBC1AtTheSameBitrateOnAMask(t *testing.T) {
	const size = 128
	cases := []struct {
		name string
		// maxCodeError bounds the worst BC4 error in 8-bit code units. The
		// bound is per image, because a hard step inside one block costs more
		// than a smooth gradient and one number for both would be meaningless.
		maxCodeError int
		value        func(x, y int) float32
	}{
		{
			// A roughness field: a smooth sine plus a hard step. The step is
			// where a block encoder loses the most, so the bound is loose.
			name:         "sine-with-step",
			maxCodeError: 10,
			value: func(x, y int) float32 {
				v := float32(0.2 + 0.6*math.Abs(math.Sin(float64(x)*0.05)))
				if y > size/2 {
					v = 1 - v
				}
				return v
			},
		},
		{
			// Smooth content, which is what most of a roughness map is. Here
			// BC4 must stay within one code of the reference.
			name:         "smooth",
			maxCodeError: 2,
			value: func(x, y int) float32 {
				return float32(0.3 + 0.4*math.Sin(float64(x)*0.03)*math.Cos(float64(y)*0.02))
			},
		},
		{
			// Two values on an 8-texel grid. Every block holds at most two
			// distinct values, so both formats must be exact.
			name:         "two-value",
			maxCodeError: 0,
			value: func(x, y int) float32 {
				if (x/8+y/8)%2 == 0 {
					return 0.05
				}
				return 0.95
			},
		},
	}

	bc4Codec, ok := BlockCodecFor("bc4-r-unorm")
	if !ok {
		t.Fatal("bc4-r-unorm is not registered")
	}
	bc1Codec, ok := BlockCodecFor("bc1-rgba-unorm")
	if !ok {
		t.Fatal("bc1-rgba-unorm is not registered")
	}

	for _, tc := range cases {
		img := NewImage(size, size)
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				v := tc.value(x, y)
				img.Set(x, y, v, v, v, 1)
			}
		}
		surface := &bcn.Surface{Width: size, Height: size, Pix: img.Pix}
		reference, err := bcn.ReferenceCodes(surface, bcn.TransferUnorm)
		if err != nil {
			t.Fatal(err)
		}

		bc4Payload, err := bc4Codec.Encode(img, BlockQualityBest)
		if err != nil {
			t.Fatalf("%s: BC4: %v", tc.name, err)
		}
		bc4Decoded, err := bcn.Decode(bcn.FormatBC4, bc4Payload, size, size)
		if err != nil {
			t.Fatal(err)
		}
		bc4PSNR, err := bcn.PSNR8(reference, bc4Decoded, bcn.ChannelR)
		if err != nil {
			t.Fatal(err)
		}
		bc4Worst, err := bcn.MaxAbsError(reference, bc4Decoded, bcn.ChannelR)
		if err != nil {
			t.Fatal(err)
		}

		bc1Payload, err := bc1Codec.Encode(img, BlockQualityBest)
		if err != nil {
			t.Fatalf("%s: BC1: %v", tc.name, err)
		}
		bc1Decoded, err := bcn.Decode(bcn.FormatBC1RGB, bc1Payload, size, size)
		if err != nil {
			t.Fatal(err)
		}
		bc1PSNR, err := bcn.PSNR8(reference, bc1Decoded, bcn.ChannelR)
		if err != nil {
			t.Fatal(err)
		}
		bc1Worst, err := bcn.MaxAbsError(reference, bc1Decoded, bcn.ChannelR)
		if err != nil {
			t.Fatal(err)
		}

		bc7Payload, err := bc7.Encode(bc7.Source{Width: size, Height: size, Pix: img.Pix},
			bc7.Options{Space: bc7.Linear, Quality: bc7.QualityBest})
		if err != nil {
			t.Fatal(err)
		}
		bc7Codes := decodeBC7Codes(t, bc7Payload, size, size)
		bc7PSNR, err := bcn.PSNR8(reference, bc7Codes, bcn.ChannelR)
		if err != nil {
			t.Fatal(err)
		}
		bc7Worst, err := bcn.MaxAbsError(reference, bc7Codes, bcn.ChannelR)
		if err != nil {
			t.Fatal(err)
		}

		// Report every figure of every format. A test that reported one number
		// would let a win on the mean hide a loss on the worst texel.
		t.Logf("%-15s BC4 %5d B %7.2f dB worst %2d | BC1 %5d B %7.2f dB worst %2d | BC7 %5d B %7.2f dB worst %2d",
			tc.name, len(bc4Payload), bc4PSNR, bc4Worst,
			len(bc1Payload), bc1PSNR, bc1Worst, len(bc7Payload), bc7PSNR, bc7Worst)

		if len(bc4Payload) != len(bc1Payload) {
			t.Fatalf("%s: BC4 wrote %d bytes and BC1 %d; the comparison needs one bitrate",
				tc.name, len(bc4Payload), len(bc1Payload))
		}
		if len(bc4Payload)*2 != size*size {
			t.Fatalf("%s: BC4 wrote %d bytes for %d texels, want half a byte each",
				tc.name, len(bc4Payload), size*size)
		}
		if len(bc7Payload) != 2*len(bc4Payload) {
			t.Fatalf("%s: BC7 wrote %d bytes against BC4's %d, want exactly twice",
				tc.name, len(bc7Payload), len(bc4Payload))
		}
		// The load-bearing claim: at one bitrate, BC4 wins.
		if bc4PSNR < bc1PSNR {
			t.Errorf("%s: BC4 reached %.2f dB and BC1 %.2f dB at the same %d bytes; the mask role picks BC4",
				tc.name, bc4PSNR, bc1PSNR, len(bc4Payload))
		}
		if bc4Worst > bc1Worst {
			t.Errorf("%s: BC4 worst code error %d against BC1's %d at the same bitrate",
				tc.name, bc4Worst, bc1Worst)
		}
		if bc4Worst > tc.maxCodeError {
			t.Errorf("%s: BC4 worst code error is %d of 255, want at most %d",
				tc.name, bc4Worst, tc.maxCodeError)
		}
		// State the other half of the trade. BC7 costs twice the GPU bytes and
		// must earn them; if it ever stopped winning, the ladder would be
		// spending memory for nothing.
		if bc7PSNR < bc4PSNR {
			t.Errorf("%s: BC7 reached only %.2f dB against BC4's %.2f while costing twice the GPU bytes",
				tc.name, bc7PSNR, bc4PSNR)
		}
	}
}

// decodeBC7Codes decodes a BC7 payload back to the 8-bit codes an uncompressed
// upload would hold, so the bcn metrics can measure it on the same footing.
func decodeBC7Codes(t *testing.T, payload []byte, width, height int) []byte {
	t.Helper()
	decoded, err := bc7.Decode(payload, width, height, bc7.Linear)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, width*height*4)
	for i := range out {
		v := float64(decoded.Pix[i])*255 + 0.5
		if v <= 0 {
			continue
		}
		if v >= 255 {
			out[i] = 255
			continue
		}
		out[i] = byte(v)
	}
	return out
}

// storedCodeDecoders maps a codec identifier onto a decoder that returns the
// 8-bit codes the payload really holds, before any transfer function is undone.
//
// bc5-rg-unorm-normal has no entry on purpose. It rewrites each texel into a unit
// normal before it quantizes, so a stored code is not a function of one input
// value. Its transfer function needs no test here either: bcn.EncodeBC5Normal
// accepts TransferUnorm only and returns ErrTransfer for anything else, so the
// encoder package itself makes the mistake unrepresentable.
func storedCodeDecoders() map[string]func(payload []byte, width, height int) ([]byte, error) {
	return map[string]func([]byte, int, int) ([]byte, error){
		"bc1-rgba-unorm-srgb": func(p []byte, w, h int) ([]byte, error) {
			return bcn.Decode(bcn.FormatBC1RGB, p, w, h)
		},
		"bc1-rgba-unorm": func(p []byte, w, h int) ([]byte, error) {
			return bcn.Decode(bcn.FormatBC1RGB, p, w, h)
		},
		"bc3-rgba-unorm-srgb": func(p []byte, w, h int) ([]byte, error) {
			return bcn.Decode(bcn.FormatBC3, p, w, h)
		},
		"bc3-rgba-unorm": func(p []byte, w, h int) ([]byte, error) {
			return bcn.Decode(bcn.FormatBC3, p, w, h)
		},
		"bc4-r-unorm": func(p []byte, w, h int) ([]byte, error) {
			return bcn.Decode(bcn.FormatBC4, p, w, h)
		},
		"bc5-rg-unorm": func(p []byte, w, h int) ([]byte, error) {
			return bcn.Decode(bcn.FormatBC5, p, w, h)
		},
		"bc7-rgba-unorm-srgb": decodeBC7Stored,
		"bc7-rgba-unorm":      decodeBC7Stored,
	}
}

// decodeBC7Stored returns the stored codes of a BC7 payload with no transfer
// function applied.
func decodeBC7Stored(payload []byte, width, height int) ([]byte, error) {
	out := make([]byte, width*height*4)
	columns := (width + 3) / 4
	rows := (height + 3) / 4
	for row := 0; row < rows; row++ {
		for col := 0; col < columns; col++ {
			offset := (row*columns + col) * 16
			texels := bc7.DecodeBlock(payload[offset : offset+16])
			for t := 0; t < 16; t++ {
				x := col*4 + t%4
				y := row*4 + t/4
				if x >= width || y >= height {
					continue
				}
				i := (y*width + x) * 4
				copy(out[i:i+4], texels[t][:])
			}
		}
	}
	return out, nil
}

// TestBlockCodecAppliesTheTransferFunctionItDeclares measures the stored codes.
//
// The declared ColorSpace field and the transfer function the encoder really
// applies are two separate things, and only the second one reaches a texel. A
// codec whose field says sRGB while its call says linear stores codes that are too
// small; the sampler then applies the sRGB decode a second time and every texture
// ships dark. Nothing downstream of the encode can find that.
//
// So the test encodes one known linear value and reads back the stored code. The
// expected code comes from the package's own transfer function, which
// TestSRGBRoundTrip pins to IEC 61966-2-1.
func TestBlockCodecAppliesTheTransferFunctionItDeclares(t *testing.T) {
	// Values chosen away from 0 and 1, where the two curves are far apart. At
	// linear 0.2 the unorm code is 51 and the sRGB code is 124.
	values := []float32{0.05, 0.2, 0.5, 0.8}
	decoders := storedCodeDecoders()
	const size = 8

	for _, id := range RegisteredBlockCodecs() {
		decode, ok := decoders[id]
		if !ok {
			// Only bc5-rg-unorm-normal may be absent. Read the
			// storedCodeDecoders comment for the reason.
			if id != "bc5-rg-unorm-normal" {
				t.Errorf("%s has no stored-code decoder, so its transfer function is unchecked", id)
			}
			continue
		}
		codec, _ := BlockCodecFor(id)
		for _, value := range values {
			img := NewImage(size, size)
			for y := 0; y < size; y++ {
				for x := 0; x < size; x++ {
					img.Set(x, y, value, value, value, 1)
				}
			}
			payload, err := codec.Encode(img, BlockQualityBest)
			if err != nil {
				t.Fatalf("%s at %.2f: %v", id, value, err)
			}
			codes, err := decode(payload, size, size)
			if err != nil {
				t.Fatalf("%s at %.2f: decode: %v", id, value, err)
			}
			want := LinearToUnorm8(value)
			if codec.ColorSpace == SRGB {
				want = LinearToSRGB8(value)
			}
			// A constant block encodes exactly in every BC format, so the
			// tolerance covers endpoint quantization only. BC1 quantizes its
			// endpoints to five and six bits, which is why the bound is not one.
			tolerance := 1
			if codec.BlockBytes == 8 && codec.BlockWidth == 4 {
				tolerance = 4
			}
			got := int(codes[0])
			if diff := got - int(want); diff > tolerance || diff < -tolerance {
				t.Errorf("%s declares %s: linear %.2f stored as code %d, want %d within %d",
					id, codec.ColorSpace, value, got, want, tolerance)
			}
			// The two curves must be far enough apart at this value that the
			// test could tell them apart. Otherwise it proves nothing.
			other := LinearToSRGB8(value)
			if codec.ColorSpace == SRGB {
				other = LinearToUnorm8(value)
			}
			if diff := int(other) - int(want); diff <= tolerance && diff >= -tolerance {
				t.Fatalf("%s at %.2f: the sRGB code %d and the unorm code %d are within the tolerance %d, so the test cannot distinguish them",
					id, value, LinearToSRGB8(value), LinearToUnorm8(value), tolerance)
			}
		}
	}
}

// TestBC5NormalCodecNormalizesBeforeQuantizing pins the reason the normal role
// uses EncodeBC5Normal and not plain EncodeBC5.
//
// Any resample wider than a box filter pulls texels off the unit sphere, and every
// mip level below zero comes from such a filter. A shorter vector shrinks x and y
// toward the middle code, and a shader that rebuilds z from those then produces a
// z that is too large, which flattens the surface.
//
// Plain EncodeBC5 stores the drifted channels as they are. EncodeBC5Normal
// normalizes each texel first. On a drifted source the difference is measurable,
// so the test measures it. On an already normalized source the two agree exactly,
// which is why a test that only used clean normals would pass either way.
func TestBC5NormalCodecNormalizesBeforeQuantizing(t *testing.T) {
	const size = 64
	img := NewImage(size, size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			u := (float64(x)+0.5)/size*2 - 1
			v := (float64(y)+0.5)/size*2 - 1
			nx, ny, nz := 0.0, 0.0, 1.0
			if r2 := u*u + v*v; r2 < 1 {
				nx, ny, nz = u, v, math.Sqrt(1-r2)
			}
			// Shrink each vector by up to 30 percent, which is the drift a
			// filtered mip level of a detailed normal map really carries.
			scale := 0.7 + 0.3*math.Abs(math.Sin(float64(x+y)*0.3))
			img.Set(x, y,
				float32(nx*scale*0.5+0.5), float32(ny*scale*0.5+0.5), float32(nz*scale*0.5+0.5), 1)
		}
	}

	// The reference is the normalized source, because a shader always consumes
	// unit normals. Copy first: NormalizeNormals writes in place.
	referenceImg := img.Clone()
	reference := &bcn.Surface{Width: size, Height: size, Pix: referenceImg.Pix}
	bcn.NormalizeNormals(reference)

	codec, ok := BlockCodecFor("bc5-rg-unorm-normal")
	if !ok {
		t.Fatal("bc5-rg-unorm-normal is not registered")
	}
	normalized, err := codec.Encode(img, BlockQualityBest)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := bcn.EncodeBC5(&bcn.Surface{Width: size, Height: size, Pix: img.Pix},
		bcn.BC5Options{Transfer: bcn.TransferUnorm, Quality: bcn.QualityHigh,
			First: bcn.ChannelR, Second: bcn.ChannelG})
	if err != nil {
		t.Fatal(err)
	}

	normalizedErr, err := bcn.AngularErrorBC5(reference, normalized)
	if err != nil {
		t.Fatal(err)
	}
	plainErr, err := bcn.AngularErrorBC5(reference, plain)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("drifted source: EncodeBC5Normal mean %.4f p95 %.4f max %.4f deg",
		normalizedErr.MeanDegrees, normalizedErr.P95Degrees, normalizedErr.MaxDegrees)
	t.Logf("drifted source: plain EncodeBC5  mean %.4f p95 %.4f max %.4f deg",
		plainErr.MeanDegrees, plainErr.P95Degrees, plainErr.MaxDegrees)

	if len(normalized) != len(plain) {
		t.Fatalf("payloads are %d and %d bytes; the comparison needs one bitrate", len(normalized), len(plain))
	}
	// Report all three statistics, so a win on the mean cannot hide a loss in
	// the tail.
	if normalizedErr.MeanDegrees >= plainErr.MeanDegrees {
		t.Errorf("the normal codec turned normals by %.4f deg and the plain one by %.4f; the codec must normalize first",
			normalizedErr.MeanDegrees, plainErr.MeanDegrees)
	}
	if normalizedErr.P95Degrees >= plainErr.P95Degrees {
		t.Errorf("the normal codec p95 is %.4f deg and the plain one %.4f",
			normalizedErr.P95Degrees, plainErr.P95Degrees)
	}
}

// TestBlockCodecEncodeIsDeterministic checks that a parallel encode returns one
// answer.
//
// Both adapters ask their encoder for one goroutine per processor, so a build on a
// twenty-core machine and a build on a two-core machine must produce the same
// bytes. A race in either encoder would show up here as a differing payload, and a
// build that depended on core count would break every content hash downstream.
func TestBlockCodecEncodeIsDeterministic(t *testing.T) {
	// The size must span several block rows, or a worker split never happens.
	const size = 64
	for _, id := range RegisteredBlockCodecs() {
		codec, _ := BlockCodecFor(id)
		source := blockTestImage(size, size)
		first, err := codec.Encode(source, BlockQualityBalanced)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		for run := 0; run < 4; run++ {
			again, err := codec.Encode(source, BlockQualityBalanced)
			if err != nil {
				t.Fatalf("%s run %d: %v", id, run, err)
			}
			if !bytes.Equal(first, again) {
				t.Fatalf("%s run %d produced different bytes; the encode is not deterministic", id, run)
			}
		}
	}
}

// TestReportBytesPerRoleAndFormat is the measurement record of the feature.
//
// It builds one texture per role at the default ladder and reports, per variant,
// the wire bytes, the GPU bytes, and both ratios. Every number is measured from a
// real container the pipeline wrote, and the GPU number is checked against the
// encoder packages' own arithmetic by
// TestBlockMipChainBytesMatchesEncoderArithmetic.
//
// The test asserts the three properties a reader must be able to trust:
//
//  1. GPU bytes fall by the format's bitrate, never further.
//  2. Wire bytes and GPU bytes are separate numbers. Supercompression moves the
//     first and never the second.
//  3. The uncompressed variant survives at every tier.
func TestReportBytesPerRoleAndFormat(t *testing.T) {
	sources := []struct {
		name string
		role Role
		make func(x, y int) color.NRGBA
	}{
		{
			name: "hero_albedo.png",
			role: RoleBaseColor,
			make: func(x, y int) color.NRGBA {
				return color.NRGBA{R: uint8(x), G: uint8(y), B: uint8((x * y) / 7), A: 255}
			},
		},
		{
			name: "hero_albedo_cutout.png",
			role: RoleBaseColor,
			make: func(x, y int) color.NRGBA {
				a := uint8(255)
				if (x/16+y/16)%3 == 0 {
					a = 40
				}
				return color.NRGBA{R: uint8(x * 2), G: uint8(y), B: 90, A: a}
			},
		},
		{
			name: "hero_normal.png",
			role: RoleNormal,
			make: func(x, y int) color.NRGBA {
				dx := 0.4 * math.Sin(float64(x)*0.2)
				dy := 0.4 * math.Cos(float64(y)*0.17)
				n := math.Sqrt(dx*dx + dy*dy + 1)
				return color.NRGBA{
					R: uint8((dx/n*0.5 + 0.5) * 255),
					G: uint8((dy/n*0.5 + 0.5) * 255),
					B: uint8((1/n*0.5 + 0.5) * 255),
					A: 255,
				}
			},
		},
		{
			name: "hero_roughness.png",
			role: RoleMask,
			make: func(x, y int) color.NRGBA {
				v := uint8(60 + 150*math.Abs(math.Sin(float64(x)*0.04)))
				return color.NRGBA{R: v, G: v, B: v, A: 255}
			},
		},
		{
			name: "hero_metallicRoughness.png",
			role: RolePacked,
			make: func(x, y int) color.NRGBA {
				return color.NRGBA{R: 0, G: uint8(x), B: uint8(y / 2), A: 255}
			},
		},
	}

	t.Logf("%-14s %-24s %-6s %9s %9s %11s %11s %8s",
		"role", "format", "tier", "wire", "gpu", "wire/source", "gpu/rgba8", "encodeMs")
	for _, source := range sources {
		data := srgbPNG(t, 512, 512, source.make)
		result, err := Build(data, BuildOptions{
			ColorSpace:         ColorSpaceForName(source.name),
			Supercompress:      true,
			PruneConstantAlpha: true,
			BlockCompression:   true,
			BlockQuality:       BlockQualityFast,
			Source:             source.name,
		})
		if err != nil {
			t.Fatalf("%s: %v", source.name, err)
		}
		if Role(result.Role) != source.role {
			t.Errorf("%s resolved to role %q, want %q", source.name, result.Role, source.role)
		}

		portableTiers := map[string]bool{}
		for _, plan := range result.Variants {
			if plan.Portable {
				portableTiers[plan.Tier] = true
			}
		}
		for _, plan := range result.Variants {
			t.Logf("%-14s %-24s %-6s %9d %9d %11.4f %11.4f %8d",
				result.Role, plan.Format, plan.Tier, plan.Bytes, plan.GPUBytes,
				plan.Ratio, plan.GPURatio, plan.EncodeMS)

			if plan.GPUBytes <= 0 || plan.GPUBytesRGBA8 <= 0 {
				t.Errorf("%s %s reports %d GPU bytes against %d rgba8 bytes",
					source.name, plan.Format, plan.GPUBytes, plan.GPUBytesRGBA8)
			}
			if !plan.Block {
				continue
			}
			if !portableTiers[plan.Tier] {
				t.Errorf("%s tier %s ships a block variant and no uncompressed fallback",
					source.name, plan.Tier)
			}
			codec, ok := BlockCodecFor(plan.Codec)
			if !ok {
				t.Fatalf("%s names unregistered codec %q", plan.Format, plan.Codec)
			}
			// The GPU saving is the format's bitrate, and no supercompression
			// can improve it. A ratio better than the bitrate would mean the
			// two numbers had been confused.
			bestRatio := float64(codec.BlockBytes) / float64(codec.BlockWidth*codec.BlockHeight*4)
			if plan.GPURatio < bestRatio-0.001 {
				t.Errorf("%s %s claims GPU ratio %.4f, better than the format's own %.4f",
					source.name, plan.Format, plan.GPURatio, bestRatio)
			}
			// Wire bytes must be at most the GPU bytes: zlib either shrinks the
			// blocks or leaves them. It can never inflate past the payload plus
			// the small container header, so a wire size far above the GPU size
			// means the writer wrote the wrong thing.
			if plan.Bytes > plan.GPUBytes+4096 {
				t.Errorf("%s %s wire %d bytes exceeds GPU %d bytes by more than a header",
					source.name, plan.Format, plan.Bytes, plan.GPUBytes)
			}
		}
	}
}

// TestLowTierTakesTheCheapRung checks the ladder end to end.
//
// The low tier serves a device that already chose the smallest pixels, usually
// because of memory. Halving the bitrate again is exactly what that device wants,
// so the low tier takes BC1 for an opaque colour map and BC3 for one with alpha,
// while the higher tiers take BC7.
func TestLowTierTakesTheCheapRung(t *testing.T) {
	cases := []struct {
		name      string
		alpha     bool
		wantHigh  string
		wantLow   string
		makePixel func(x, y int) color.NRGBA
	}{
		{
			name:     "opaque",
			wantHigh: "bc7-rgba-unorm-srgb",
			wantLow:  "bc1-rgba-unorm-srgb",
			makePixel: func(x, y int) color.NRGBA {
				return color.NRGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255}
			},
		},
		{
			name:     "with-alpha",
			alpha:    true,
			wantHigh: "bc7-rgba-unorm-srgb",
			wantLow:  "bc3-rgba-unorm-srgb",
			makePixel: func(x, y int) color.NRGBA {
				a := uint8(255)
				if (x/32+y/32)%2 == 0 {
					a = 30
				}
				return color.NRGBA{R: uint8(x), G: uint8(y), B: 128, A: a}
			},
		},
	}

	for _, tc := range cases {
		// A 2048 source resolves to three distinct tier sizes, so the ladder
		// does not collapse them and the low rung is reachable.
		data := srgbPNG(t, 2048, 64, tc.makePixel)
		result, err := Build(data, BuildOptions{
			ColorSpace:         SRGB,
			Supercompress:      true,
			PruneConstantAlpha: true,
			BlockCompression:   true,
			BlockQuality:       BlockQualityFast,
			Source:             tc.name + "_albedo.png",
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		byTier := map[string]string{}
		for _, plan := range result.Variants {
			if !plan.Block {
				continue
			}
			if old, ok := byTier[plan.Tier]; ok {
				t.Errorf("%s tier %s ships two block variants, %s and %s", tc.name, plan.Tier, old, plan.Codec)
			}
			byTier[plan.Tier] = plan.Codec
		}
		for tier, want := range map[string]string{
			"high":     tc.wantHigh,
			"standard": tc.wantHigh,
			"low":      tc.wantLow,
		} {
			if got := byTier[tier]; got != want {
				t.Errorf("%s tier %s took codec %q, want %q", tc.name, tier, got, want)
			}
		}
		// Report the measured saving of the cheap rung against the default one,
		// so the ladder decision carries a number.
		var highBytes, lowBytes int
		for _, plan := range result.Variants {
			if !plan.Block {
				continue
			}
			switch plan.Tier {
			case "high":
				highBytes = plan.GPUBytes
			case "low":
				lowBytes = plan.GPUBytes
			}
		}
		t.Logf("%-10s high %s %d gpu bytes, low %s %d gpu bytes",
			tc.name, byTier["high"], highBytes, byTier["low"], lowBytes)
	}
}

// TestExplicitRoleWinsOverTheFileNameGuess pins the override.
//
// A caller that read a glTF material binding knows the real role. The file name
// heuristic must never overrule it, and the record must say the role was given and
// not guessed, so a reader can tell a fact from a guess.
func TestExplicitRoleWinsOverTheFileNameGuess(t *testing.T) {
	// The name says colour. The caller says normal map, which is what a glTF
	// normalTexture binding would say.
	data := srgbPNG(t, 64, 64, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(128 + x), G: uint8(128 + y), B: 250, A: 255}
	})
	if guess := RoleForName("plate_albedo.png"); guess != RoleBaseColor {
		t.Fatalf("the file name guesses %q; the test needs it to guess base colour", guess)
	}

	result, err := Build(data, BuildOptions{
		ColorSpace:       Linear,
		Supercompress:    true,
		BlockCompression: true,
		BlockQuality:     BlockQualityFast,
		Role:             RoleNormal,
		Source:           "plate_albedo.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if Role(result.Role) != RoleNormal {
		t.Errorf("the build used role %q, want %q", result.Role, RoleNormal)
	}
	if result.RoleGuessed {
		t.Error("the caller gave the role, so RoleGuessed must be false")
	}
	blocks := 0
	for _, plan := range result.Variants {
		if !plan.Block {
			continue
		}
		blocks++
		if plan.Codec != "bc5-rg-unorm-normal" {
			t.Errorf("role normal took codec %q, want bc5-rg-unorm-normal", plan.Codec)
		}
		if plan.RoleGuessed {
			t.Errorf("variant %s records a guessed role", plan.Format)
		}
	}
	if blocks == 0 {
		t.Fatal("the explicit role produced no block variant")
	}
}

// TestExplicitBlockCodecsStillObeyAlphaAndRole checks the two gates that stand
// between an explicit codec list and a wrong file.
//
// BuildOptions.BlockCodecs lets a caller name a codec directly. It must not let a
// caller drop an alpha channel the source uses, and it must not let a caller put a
// mask codec on a colour map. Both would ship a plausible file with a channel
// missing.
func TestExplicitBlockCodecsStillObeyAlphaAndRole(t *testing.T) {
	withAlpha := srgbPNG(t, 64, 64, func(x, y int) color.NRGBA {
		a := uint8(255)
		if x < 32 {
			a = 20
		}
		return color.NRGBA{R: uint8(x * 4), G: uint8(y * 4), B: 70, A: a}
	})

	// BC1 stores no eight-bit alpha, so it must be refused for this source even
	// though the caller named it.
	result, err := Build(withAlpha, BuildOptions{
		ColorSpace:       SRGB,
		Supercompress:    true,
		BlockCompression: true,
		BlockQuality:     BlockQualityFast,
		BlockCodecs:      []string{"bc1-rgba-unorm-srgb"},
		Source:           "sprite_albedo.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, plan := range result.Variants {
		if plan.Block {
			t.Errorf("BC1 was used on a source with alpha, as codec %q", plan.Codec)
		}
	}
	if len(result.BlockSkipped) == 0 {
		t.Error("the build refused BC1 and recorded no reason")
	}

	// A mask codec on a colour role must be refused too.
	opaque := srgbPNG(t, 64, 64, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x * 4), G: uint8(y * 4), B: 70, A: 255}
	})
	result, err = Build(opaque, BuildOptions{
		ColorSpace:       SRGB,
		Supercompress:    true,
		BlockCompression: true,
		BlockQuality:     BlockQualityFast,
		Role:             RoleBaseColor,
		BlockCodecs:      []string{"bc4-r-unorm"},
		Source:           "sprite_albedo.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, plan := range result.Variants {
		if plan.Block {
			t.Errorf("the mask codec %q was used on a base colour map", plan.Codec)
		}
	}

	// The same list on the role it serves must work, so the two refusals above
	// cannot pass by refusing every explicit codec.
	mask := srgbPNG(t, 64, 64, func(x, y int) color.NRGBA {
		v := uint8(x * 4)
		return color.NRGBA{R: v, G: v, B: v, A: 255}
	})
	result, err = Build(mask, BuildOptions{
		ColorSpace:       Linear,
		Supercompress:    true,
		BlockCompression: true,
		BlockQuality:     BlockQualityFast,
		Role:             RoleMask,
		BlockCodecs:      []string{"bc4-r-unorm"},
		Source:           "sprite_ao.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	used := ""
	for _, plan := range result.Variants {
		if plan.Block {
			used = plan.Codec
		}
	}
	if used != "bc4-r-unorm" {
		t.Errorf("the mask role with an explicit BC4 list used codec %q", used)
	}
}
