package texture

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Tier names one delivery quality step and the edge ceiling it applies.
type Tier struct {
	// Name is the tier label. "high", "standard", and "low" are the three
	// the defaults use.
	Name string
	// MaxEdge caps both edges after the power-of-two rounding.
	MaxEdge int
}

// DefaultTiers is the ceiling ladder the build uses when the caller names none.
//
// The numbers are GPU-memory decisions, not guesses. One RGBA8 texture with a
// full mip chain costs about 1.33 * w * h * 4 bytes of GPU memory:
//
//	2048  22.4 MB   a hero material on a discrete GPU
//	1024   5.6 MB   the usual ceiling for a phone
//	 512   1.4 MB   a data-saver or a very low memory device
//
// A material set with five maps multiplies each of those by five, which is why
// the low tier exists at all.
func DefaultTiers() []Tier {
	return []Tier{
		{Name: "high", MaxEdge: 2048},
		{Name: "standard", MaxEdge: 1024},
		{Name: "low", MaxEdge: 512},
	}
}

// BuildOptions controls one source texture's whole variant set.
type BuildOptions struct {
	// ColorSpace tells the builder how the source encodes colour.
	ColorSpace ColorSpace
	// Filter selects the resample kernel. Zero means Lanczos3.
	Filter Filter
	// Tiers lists the quality steps. An empty slice selects DefaultTiers.
	Tiers []Tier
	// Supercompress writes zlib KTX2 payloads. Leave it on for a texture.
	Supercompress bool
	// PruneConstantAlpha drops an all-opaque alpha channel.
	PruneConstantAlpha bool
	// EmitPortableOnly skips the three-channel RGB8 output. WebGPU has no
	// rgb8unorm format, so the three-channel form only serves WebGL2.
	EmitPortableOnly bool
	// Source names the input for the container metadata.
	Source string

	// BlockCompression adds one block-compressed variant per tier, chosen by
	// Role. The uncompressed variant always ships too, because every block
	// format sits behind an optional device feature.
	//
	// The field is off by default. A caller that turns it on pays the encode
	// time and gains four to eight times less GPU memory on a device that has
	// the feature.
	BlockCompression bool
	// Role says what the texture means to a renderer, which decides the block
	// format. The zero value asks RoleForName to guess from Source.
	Role Role
	// BlockCodecs overrides the role ladder with explicit codec identifiers.
	// A tier takes the first identifier the registry knows.
	BlockCodecs []string
	// BlockQuality trades encode time for image quality. Zero means
	// BlockQualityBalanced.
	BlockQuality BlockQuality
}

// BlockQuality picks the speed and quality trade of a block encoder.
//
// The value maps onto the quality level of each encoder package. The names stay
// in this package so a caller never imports bc7 or bcn directly.
type BlockQuality int

const (
	// BlockQualityBalanced is the default. It runs the full mode or endpoint
	// search at a bounded candidate count.
	BlockQualityBalanced BlockQuality = iota
	// BlockQualityFast cuts the candidate count. Use it for a preview build.
	BlockQualityFast
	// BlockQualityBest runs the widest search. Use it for a final bake.
	BlockQualityBest
)

// String names the level for a manifest or a metric.
func (q BlockQuality) String() string {
	switch q {
	case BlockQualityFast:
		return "fast"
	case BlockQualityBest:
		return "best"
	}
	return "balanced"
}

// Role names what a texture means to a renderer.
//
// The role, not the file name, decides the block format and the colour space.
// A tangent-space normal map wants BC5, and a base colour map wants BC7; the
// same pixels under the wrong role look plausible and are wrong.
type Role string

// The roles the ladder knows.
const (
	// RoleUnknown asks the builder to fall back to the colour space.
	RoleUnknown Role = ""
	// RoleBaseColor is a colour map the sampler reads through sRGB.
	RoleBaseColor Role = "base-color"
	// RoleEmissive is a colour map that adds light. It takes the same ladder
	// as base colour.
	RoleEmissive Role = "emissive"
	// RoleNormal is a tangent-space normal map. BC5 stores x and y and a
	// shader rebuilds z, which beats BC7 at the same bitrate.
	RoleNormal Role = "normal"
	// RoleMask is a single-channel map: roughness, metalness, occlusion,
	// height, or opacity. BC4 spends all of its bits on the one channel.
	RoleMask Role = "mask"
	// RolePacked is a multi-channel data map such as a glTF
	// metallicRoughnessTexture or a packed occlusion-roughness-metalness map.
	// It needs every channel, so it takes BC7 with a linear transfer.
	RolePacked Role = "packed"
)

// maskMarkers name the single-channel maps. The order matters only for
// readability; RoleForName returns on the first match.
var maskMarkers = []string{
	"roughness", "_rough", "-rough", "metal", "occlusion", "_ao", "-ao",
	"ambientocclusion", "height", "displacement", "bump", "mask", "gloss",
	"opacity", "specular", "curvature",
}

// packedMarkers name the maps that pack several factors into one image.
var packedMarkers = []string{"_orm", "-orm", "orm_", "metallicroughness", "metallic_roughness", "_arm", "-arm"}

// normalMarkers name a tangent-space normal map.
var normalMarkers = []string{"normal", "_nrm", "-nrm", "_n.", "-n.", "_norm", "-norm"}

// emissiveMarkers name an emissive map.
var emissiveMarkers = []string{"emissive", "emission", "_emit", "-emit", "glow"}

// RoleForName guesses the role of a texture from its file name.
//
// The guess is a heuristic and the build records it, so a wrong guess is visible
// in the sidecar instead of silent. glTF carries the real answer in its material
// bindings; a caller that has it must pass BuildOptions.Role instead.
//
// The order of the tests is deliberate. A packed marker wins over a mask marker,
// because "metallicRoughness" contains "metal" and holds two factors, not one.
// A normal marker wins over everything, because a normal map under any other
// role is the most damaging mistake in the table.
func RoleForName(name string) Role {
	lower := strings.ToLower(name)
	if containsAny(lower, normalMarkers) {
		return RoleNormal
	}
	if containsAny(lower, packedMarkers) {
		return RolePacked
	}
	if containsAny(lower, emissiveMarkers) {
		return RoleEmissive
	}
	if containsAny(lower, maskMarkers) {
		return RoleMask
	}
	if containsAny(lower, []string{"albedo", "basecolor", "base_color", "diffuse", "color", "colour"}) {
		return RoleBaseColor
	}
	return RoleUnknown
}

func containsAny(s string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// ColorSpaceForRole returns the transfer function a role needs.
//
// A colour role goes through the sRGB curve; every data role stays linear. The
// pairing is not a preference: an sRGB VkFormat makes the sampler invert a curve
// the encoder must therefore have applied.
func ColorSpaceForRole(role Role) (ColorSpace, bool) {
	switch role {
	case RoleBaseColor, RoleEmissive:
		return SRGB, true
	case RoleNormal, RoleMask, RolePacked:
		return Linear, true
	}
	return Linear, false
}

// BlockCodec binds one block format to the pipeline.
//
// The two adapter files codec_bcn.go and codec_bc7.go are the only files that
// import an encoder package, so a signature change there costs one file.
type BlockCodec struct {
	// ID names the codec in the registry and in BuildOptions.BlockCodecs. It
	// is not always the format name: BC5 has one WebGPU format and two
	// encoders, one for a plain channel pair and one for normals.
	ID string
	// Format is the WebGPU GPUTextureFormat spelling. It names the output file
	// and the capability token.
	Format string
	// VkFormat is the container format. It must pair with ColorSpace: an sRGB
	// space needs an sRGB VkFormat, and a linear space needs a unorm one.
	VkFormat int
	// ColorSpace is the transfer function the encoder applies and the sampler
	// inverts. The two must be the same curve.
	ColorSpace ColorSpace
	// BlockWidth, BlockHeight and BlockBytes describe one texel block.
	BlockWidth  int
	BlockHeight int
	BlockBytes  int
	// Roles lists the roles this codec may serve. A codec outside the role's
	// list never encodes that role's pixels.
	Roles []Role
	// NeedsAlpha is true when the codec stores an alpha channel. A source with
	// real alpha may not take a codec that drops it.
	NeedsAlpha bool
	// ShaderWork records renderer work the format needs, empty when none. BC5
	// normals need a z rebuild in every shader that samples them.
	ShaderWork string
	// Encode compresses one mip level. The image holds linear light; the codec
	// applies its own transfer function.
	Encode func(level *Image, quality BlockQuality) ([]byte, error)
}

// blockCodecs holds the registered codecs. The adapter files fill it from init,
// so a build that does not link an adapter simply has no block ladder.
var blockCodecs = map[string]BlockCodec{}

// RegisterBlockCodec adds one codec. It panics on a duplicate identifier or on
// a codec whose colour space and VkFormat disagree, because both are programmer
// errors that no runtime check downstream could catch.
func RegisterBlockCodec(codec BlockCodec) {
	if codec.ID == "" || codec.Encode == nil {
		panic("texture: a block codec needs an ID and an Encode function")
	}
	if _, ok := blockCodecs[codec.ID]; ok {
		panic("texture: duplicate block codec " + codec.ID)
	}
	if codec.BlockWidth < 1 || codec.BlockHeight < 1 || codec.BlockBytes < 1 {
		panic("texture: block codec " + codec.ID + " has an empty block")
	}
	blockCodecs[codec.ID] = codec
}

// BlockCodecFor returns one registered codec.
func BlockCodecFor(id string) (BlockCodec, bool) {
	codec, ok := blockCodecs[id]
	return codec, ok
}

// RegisteredBlockCodecs lists every codec identifier in a stable order.
func RegisteredBlockCodecs() []string {
	out := make([]string, 0, len(blockCodecs))
	for id := range blockCodecs {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// CodecsForRole returns the codec identifiers a role may use, best first.
//
// The mapping is the quality decision of the whole feature:
//
//   - A tangent-space normal map takes BC5. BC7 spends bits on a third channel
//     and on alpha that carry nothing, so BC5 wins at the same bitrate.
//   - A single-channel mask takes BC4 at half the bitrate of BC7 and higher
//     quality, because every bit serves the one channel.
//   - Base colour with alpha takes BC7, or BC3 where BC7 is absent.
//   - Base colour without alpha takes BC7, or BC1 as the cheap tier.
//
// cheap asks for the low-bitrate rung, which the low tier uses. A role with one
// rung ignores it.
func CodecsForRole(role Role, hasAlpha, cheap bool) []string {
	switch role {
	case RoleNormal:
		return []string{"bc5-rg-unorm-normal"}
	case RoleMask:
		return []string{"bc4-r-unorm"}
	case RolePacked:
		return []string{"bc7-rgba-unorm"}
	case RoleBaseColor, RoleEmissive:
		if hasAlpha {
			if cheap {
				return []string{"bc3-rgba-unorm-srgb", "bc7-rgba-unorm-srgb"}
			}
			return []string{"bc7-rgba-unorm-srgb", "bc3-rgba-unorm-srgb"}
		}
		if cheap {
			return []string{"bc1-rgba-unorm-srgb", "bc7-rgba-unorm-srgb"}
		}
		return []string{"bc7-rgba-unorm-srgb", "bc1-rgba-unorm-srgb"}
	}
	return nil
}

// VariantPlan is one KTX2 output the builder produced.
type VariantPlan struct {
	Tier          string  `json:"tier"`
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	Levels        int     `json:"levels"`
	Channels      int     `json:"channels"`
	Format        string  `json:"format"`
	VkFormat      int     `json:"vkFormat"`
	Bytes         int     `json:"bytes"`
	Supercompress bool    `json:"supercompressed,omitempty"`
	Portable      bool    `json:"portable"`
	Ratio         float64 `json:"ratioOfSource"`
	DurationMS    int64   `json:"durationMs"`
	// AlphaPruneRejected is true when the builder tried the three-channel
	// form of this variant and measured it larger, so it kept only this one.
	AlphaPruneRejected bool `json:"alphaPruneRejected,omitempty"`
	// AlphaPruneBytes records the size the rejected three-channel form would
	// have had, so the decision is auditable and not merely asserted.
	AlphaPruneBytes int `json:"alphaPruneBytes,omitempty"`

	// Block is true for a block-compressed variant.
	Block bool `json:"block,omitempty"`
	// Codec names the block codec, empty for an uncompressed variant. It can
	// differ from Format: BC5 has one WebGPU format and two encoders.
	Codec string `json:"codec,omitempty"`
	// Role records the role the builder used, guessed or given. A wrong guess
	// is then visible in the sidecar instead of silent.
	Role string `json:"role,omitempty"`
	// RoleGuessed marks a role the builder read from the file name rather than
	// from a glTF material binding.
	RoleGuessed bool `json:"roleGuessed,omitempty"`
	// BlockQuality names the encoder effort level.
	BlockQuality string `json:"blockQuality,omitempty"`
	// GPUBytes is the memory the whole mip chain occupies on the GPU. It is
	// the number block compression exists to cut, and it is not the file size:
	// supercompression changes wire bytes only.
	GPUBytes int `json:"gpuBytes"`
	// GPUBytesRGBA8 is what the same chain would cost as rgba8unorm. It is the
	// denominator of GPURatio.
	GPUBytesRGBA8 int `json:"gpuBytesRgba8"`
	// GPURatio is GPUBytes over GPUBytesRGBA8.
	GPURatio float64 `json:"gpuRatio"`
	// EncodeMS is the block encode time, excluding the container write.
	EncodeMS int64 `json:"encodeMs,omitempty"`
	// Data holds the encoded container. The caller writes it.
	Data []byte `json:"-"`
}

// BuildResult reports every measurement of one source texture's build.
type BuildResult struct {
	Source        string        `json:"source"`
	SourceBytes   int           `json:"sourceBytes"`
	SourceFormat  string        `json:"sourceFormat"`
	SourceWidth   int           `json:"sourceWidth"`
	SourceHeight  int           `json:"sourceHeight"`
	BitDepth      int           `json:"bitDepth"`
	ColorSpace    string        `json:"colorSpace"`
	Filter        string        `json:"filter"`
	Alpha         AlphaStats    `json:"alpha"`
	AlphaMode     string        `json:"alphaMode"`
	Grayscale     bool          `json:"grayscale"`
	Variants      []VariantPlan `json:"variants"`
	OutputBytes   int           `json:"outputBytes"`
	DecodeMS      int64         `json:"decodeMs"`
	DurationMS    int64         `json:"durationMs"`
	PrunedChannel string        `json:"prunedChannel,omitempty"`
	// Role records the role every block variant used.
	Role string `json:"role,omitempty"`
	// RoleGuessed marks a role read from the file name. A caller with a glTF
	// material binding sets BuildOptions.Role and this stays false.
	RoleGuessed bool `json:"roleGuessed,omitempty"`
	// BlockSkipped records every tier that shipped no block variant, and why.
	// A silent skip would let a reader assume block compression ran.
	BlockSkipped []string `json:"blockSkipped,omitempty"`
}

// Build decodes one source texture and produces every KTX2 variant.
//
// Build writes nothing. It returns the encoded containers so the executor stays
// the only part of the pipeline that touches the file system.
func Build(data []byte, opts BuildOptions) (BuildResult, error) {
	start := time.Now()
	decodeStart := time.Now()
	img, info, err := Decode(data, opts.ColorSpace)
	if err != nil {
		return BuildResult{}, err
	}
	decodeMS := time.Since(decodeStart).Milliseconds()

	alpha := AnalyzeAlpha(img)
	gray := IsGrayscale(img)
	role, guessed := resolveRole(opts)
	result := BuildResult{
		Source:       opts.Source,
		SourceBytes:  len(data),
		SourceFormat: info.Format,
		SourceWidth:  info.Width,
		SourceHeight: info.Height,
		BitDepth:     info.BitDepth,
		ColorSpace:   opts.ColorSpace.String(),
		Filter:       opts.Filter.String(),
		Alpha:        alpha,
		AlphaMode:    alpha.Mode(),
		Grayscale:    gray,
		DecodeMS:     decodeMS,
		Role:         string(role),
		RoleGuessed:  guessed,
	}

	// Decide the channel count once, from the pixels.
	//
	// One case prunes to a format both GPU backends sample: a grayscale map
	// with an unused alpha channel becomes r8unorm. That is real GPU
	// savings, not only container savings, because r8unorm costs one quarter
	// of the GPU bytes of rgba8unorm.
	//
	// The builder does not guess a two-channel form. rg8unorm would suit a
	// tangent-space normal map, whose Z the shader can reconstruct, and it
	// would suit nothing else. No GoSX renderer reconstructs Z today, so
	// emitting rg8unorm would ship a file the runtime reads wrong.
	portableChannels := 4
	if opts.PruneConstantAlpha && alpha.Opaque && gray {
		portableChannels = 1
		result.PrunedChannel = "green,blue,alpha"
	}

	tiers := opts.Tiers
	if len(tiers) == 0 {
		tiers = DefaultTiers()
	}

	seen := map[string]bool{}
	for _, tier := range tiers {
		width, height := FitPowerOfTwo(info.Width, info.Height, tier.MaxEdge)
		key := strconv.Itoa(width) + "x" + strconv.Itoa(height)
		if seen[key] {
			// A smaller tier resolved to a size a higher tier already
			// covers. Writing the same pixels twice under two names would
			// inflate the manifest and the build output for nothing.
			continue
		}
		seen[key] = true

		tierStart := time.Now()
		base := img
		if width != img.Width || height != img.Height {
			base, err = Resize(img, width, height, opts.Filter)
			if err != nil {
				return BuildResult{}, err
			}
		}
		chain, err := MipChain(base, MipOptions{Filter: opts.Filter, AlphaAware: !alpha.Constant})
		if err != nil {
			return BuildResult{}, err
		}

		plans, err := encodeTier(chain, tier, portableChannels, opts, alpha, time.Since(tierStart))
		if err != nil {
			return BuildResult{}, err
		}
		if opts.BlockCompression {
			block, skipped, err := encodeBlockTier(chain, tier, role, guessed, opts, alpha)
			if err != nil {
				return BuildResult{}, err
			}
			plans = append(plans, block...)
			if skipped != "" {
				result.BlockSkipped = append(result.BlockSkipped, skipped)
			}
		}
		result.Variants = append(result.Variants, plans...)
	}

	for _, variant := range result.Variants {
		result.OutputBytes += variant.Bytes
	}
	for i := range result.Variants {
		if result.SourceBytes > 0 {
			result.Variants[i].Ratio = float64(result.Variants[i].Bytes) / float64(result.SourceBytes)
		}
	}
	result.DurationMS = time.Since(start).Milliseconds()
	sort.SliceStable(result.Variants, func(i, j int) bool {
		return result.Variants[i].Width*result.Variants[i].Height > result.Variants[j].Width*result.Variants[j].Height
	})
	return result, nil
}

// encodeTier writes the portable container and, when the alpha prune applies to
// a colour texture, the three-channel WebGL2-only container as well.
func encodeTier(chain []*Image, tier Tier, portableChannels int, opts BuildOptions, alpha AlphaStats, elapsed time.Duration) ([]VariantPlan, error) {
	base := chain[0]
	keyValues := map[string]string{
		"GoSXtextureSource":     opts.Source,
		"GoSXtextureTier":       tier.Name,
		"GoSXtextureFilter":     opts.Filter.String(),
		"GoSXtextureColorSpace": opts.ColorSpace.String(),
		"GoSXtextureAlphaMode":  alpha.Mode(),
		"GoSXtextureMipSpace":   "linear",
	}

	var out []VariantPlan
	data, name, err := EncodeKTX2(chain, EncodeOptions{
		ColorSpace:    opts.ColorSpace,
		Channels:      portableChannels,
		Supercompress: opts.Supercompress,
		KeyValues:     keyValues,
	})
	if err != nil {
		return nil, err
	}
	format, _, _ := VkFormatFor(portableChannels, opts.ColorSpace)
	out = append(out, VariantPlan{
		Tier:          tier.Name,
		Width:         base.Width,
		Height:        base.Height,
		Levels:        len(chain),
		Channels:      portableChannels,
		Format:        name,
		VkFormat:      format,
		Bytes:         len(data),
		Supercompress: opts.Supercompress,
		Portable:      true,
		DurationMS:    elapsed.Milliseconds(),
		GPUBytes:      PixelMipChainBytes(base.Width, base.Height, portableChannels),
		GPUBytesRGBA8: PixelMipChainBytes(base.Width, base.Height, 4),
		Data:          data,
	})
	setGPURatio(&out[len(out)-1])

	// The three-channel form only helps a WebGL2 consumer. WebGPU has no
	// rgb8unorm texture format at all, so a selector must gate this variant on
	// texture-format:rgb8unorm and never hand it to a WebGPU page.
	//
	// The builder keeps the variant only when it measures smaller. Dropping a
	// constant alpha channel removes a quarter of the plain payload, but zlib
	// codes a byte that repeats every four bytes almost for free, and it codes
	// a three-byte stride worse than a four-byte one. On some images the
	// three-channel container therefore comes out LARGER. Emitting it anyway
	// would ship a second file that costs bytes and buys nothing.
	if !opts.EmitPortableOnly && opts.PruneConstantAlpha && alpha.Opaque && portableChannels == 4 {
		rgbKeys := map[string]string{}
		for key, value := range keyValues {
			rgbKeys[key] = value
		}
		// A three-byte texel makes a row of an odd-width mip level unaligned
		// to four bytes. A WebGL2 consumer must set UNPACK_ALIGNMENT to 1
		// before texImage2D, or the last two mip levels shear.
		rgbKeys["GoSXtextureUnpackAlignment"] = "1"
		rgbData, rgbName, err := EncodeKTX2(chain, EncodeOptions{
			ColorSpace:    opts.ColorSpace,
			Channels:      3,
			Supercompress: opts.Supercompress,
			KeyValues:     rgbKeys,
		})
		if err != nil {
			return nil, err
		}
		rgbFormat, _, _ := VkFormatFor(3, opts.ColorSpace)
		if len(rgbData) < len(data) {
			out = append(out, VariantPlan{
				Tier:          tier.Name,
				Width:         base.Width,
				Height:        base.Height,
				Levels:        len(chain),
				Channels:      3,
				Format:        rgbName,
				VkFormat:      rgbFormat,
				Bytes:         len(rgbData),
				Supercompress: opts.Supercompress,
				Portable:      false,
				DurationMS:    elapsed.Milliseconds(),
				GPUBytes:      PixelMipChainBytes(base.Width, base.Height, 3),
				GPUBytesRGBA8: PixelMipChainBytes(base.Width, base.Height, 4),
				Data:          rgbData,
			})
			setGPURatio(&out[len(out)-1])
		} else {
			out[len(out)-1].AlphaPruneRejected = true
			out[len(out)-1].AlphaPruneBytes = len(rgbData)
		}
	}
	return out, nil
}

// resolveRole picks the role of one build and reports whether it was guessed.
//
// An explicit BuildOptions.Role wins, because a caller that has a glTF material
// binding knows more than any file name. Otherwise RoleForName reads the source
// name, and a name that says nothing falls back to the colour space: sRGB means
// colour, and linear means packed data.
func resolveRole(opts BuildOptions) (Role, bool) {
	if opts.Role != RoleUnknown {
		return opts.Role, false
	}
	if role := RoleForName(opts.Source); role != RoleUnknown {
		return role, true
	}
	if opts.ColorSpace == SRGB {
		return RoleBaseColor, true
	}
	return RolePacked, true
}

// encodeBlockTier writes the block-compressed variants of one tier.
//
// The function encodes every mip level through one codec, then hands the block
// payloads to the container writer. It returns no variant when the role has no
// codec or when no registered codec suits the source, which is the honest
// answer: the uncompressed variant already shipped.
func encodeBlockTier(chain []*Image, tier Tier, role Role, guessed bool, opts BuildOptions, alpha AlphaStats) ([]VariantPlan, string, error) {
	base := chain[0]
	codec, ok := selectBlockCodec(role, opts, tier, alpha)
	if !ok {
		return nil, fmt.Sprintf("tier %s: no registered block codec serves role %q", tier.Name, role), nil
	}
	// Skip a tier whose level 0 is not a whole number of texel blocks.
	//
	// WebGPU createTexture refuses a compressed texture whose width or height
	// is not a multiple of the block size. The power-of-two ladder divides by
	// four at every size of four and above, so only a tiny source reaches this
	// branch. The uncompressed variant already shipped, so skipping costs
	// nothing but the saving.
	if base.Width%codec.BlockWidth != 0 || base.Height%codec.BlockHeight != 0 {
		return nil, fmt.Sprintf("tier %s: %dx%d is not a whole number of %dx%d blocks, which WebGPU createTexture refuses",
			tier.Name, base.Width, base.Height, codec.BlockWidth, codec.BlockHeight), nil
	}

	keyValues := map[string]string{
		"GoSXtextureSource":       opts.Source,
		"GoSXtextureTier":         tier.Name,
		"GoSXtextureFilter":       opts.Filter.String(),
		"GoSXtextureColorSpace":   codec.ColorSpace.String(),
		"GoSXtextureAlphaMode":    alpha.Mode(),
		"GoSXtextureMipSpace":     "linear",
		"GoSXtextureRole":         string(role),
		"GoSXtextureBlockCodec":   codec.ID,
		"GoSXtextureBlockQuality": opts.BlockQuality.String(),
	}
	if guessed {
		keyValues["GoSXtextureRoleGuessed"] = "true"
	}
	if codec.ShaderWork != "" {
		keyValues["GoSXtextureShaderWork"] = codec.ShaderWork
	}

	encodeStart := time.Now()
	payloads := make([][]byte, len(chain))
	for i, level := range chain {
		payload, err := codec.Encode(level, opts.BlockQuality)
		if err != nil {
			return nil, "", fmt.Errorf("%s level %d: %w", codec.ID, i, err)
		}
		want := BlockChainLevelBytes(codec, level.Width, level.Height)
		if len(payload) != want {
			return nil, "", fmt.Errorf("%w: codec %s wrote %d bytes for a %dx%d level, want %d",
				ErrShape, codec.ID, len(payload), level.Width, level.Height, want)
		}
		payloads[i] = payload
	}
	encodeMS := time.Since(encodeStart).Milliseconds()

	data, err := EncodeBlockKTX2(codec, chain, payloads, BlockEncodeOptions{
		Supercompress: opts.Supercompress,
		KeyValues:     keyValues,
	})
	if err != nil {
		return nil, "", err
	}

	plan := VariantPlan{
		Tier:          tier.Name,
		Width:         base.Width,
		Height:        base.Height,
		Levels:        len(chain),
		Channels:      4,
		Format:        codec.Format,
		VkFormat:      codec.VkFormat,
		Bytes:         len(data),
		Supercompress: opts.Supercompress,
		Portable:      false,
		Block:         true,
		Codec:         codec.ID,
		Role:          string(role),
		RoleGuessed:   guessed,
		BlockQuality:  opts.BlockQuality.String(),
		GPUBytes:      BlockMipChainBytes(codec, base.Width, base.Height),
		GPUBytesRGBA8: PixelMipChainBytes(base.Width, base.Height, 4),
		EncodeMS:      encodeMS,
		DurationMS:    encodeMS,
		Data:          data,
	}
	setGPURatio(&plan)
	return []VariantPlan{plan}, "", nil
}

// selectBlockCodec resolves the codec of one tier.
//
// An explicit BuildOptions.BlockCodecs list wins. Otherwise the role ladder
// decides, and the low tier asks for the cheap rung, which halves the bitrate
// again on a device that already chose the smallest tier.
//
// A source with real alpha never takes a codec that drops alpha. That check is
// the reason the ladder is a list rather than one name: BC1 is the cheap rung
// for an opaque colour map and the wrong answer for a cutout one.
func selectBlockCodec(role Role, opts BuildOptions, tier Tier, alpha AlphaStats) (BlockCodec, bool) {
	candidates := opts.BlockCodecs
	if len(candidates) == 0 {
		candidates = CodecsForRole(role, !alpha.Opaque, tier.Name == "low")
	}
	for _, id := range candidates {
		codec, ok := BlockCodecFor(id)
		if !ok {
			continue
		}
		if !codecServesRole(codec, role) {
			continue
		}
		if !alpha.Opaque && !codec.NeedsAlpha {
			continue
		}
		return codec, true
	}
	return BlockCodec{}, false
}

// codecServesRole reports whether a codec may encode one role. A codec with no
// role list serves any role, which lets a caller name an explicit codec.
func codecServesRole(codec BlockCodec, role Role) bool {
	if len(codec.Roles) == 0 {
		return true
	}
	for _, allowed := range codec.Roles {
		if allowed == role {
			return true
		}
	}
	return false
}

// BlockChainLevelBytes returns the payload size one mip level costs in a block
// format. A level smaller than one block still costs a whole block.
func BlockChainLevelBytes(codec BlockCodec, width, height int) int {
	if codec.BlockWidth < 1 || codec.BlockHeight < 1 || width < 1 || height < 1 {
		return 0
	}
	columns := (width + codec.BlockWidth - 1) / codec.BlockWidth
	rows := (height + codec.BlockHeight - 1) / codec.BlockHeight
	return columns * rows * codec.BlockBytes
}

// BlockMipChainBytes returns the GPU bytes a full block-compressed mip chain
// costs, from the base size down to one texel.
//
// The count includes the padding the small levels carry: the GPU allocates a
// whole block for a 2x2 or a 1x1 level. That padding is why a small texture
// saves less than the block ratio suggests.
func BlockMipChainBytes(codec BlockCodec, width, height int) int {
	if codec.BlockBytes < 1 || width < 1 || height < 1 {
		return 0
	}
	total := 0
	for {
		total += BlockChainLevelBytes(codec, width, height)
		if width <= 1 && height <= 1 {
			return total
		}
		width, height = halveEdge(width), halveEdge(height)
	}
}

// PixelMipChainBytes returns the GPU bytes an uncompressed mip chain costs at a
// given byte stride per texel. It is the denominator of every ratio the build
// reports.
func PixelMipChainBytes(width, height, bytesPerPixel int) int {
	if width < 1 || height < 1 || bytesPerPixel < 1 {
		return 0
	}
	total := 0
	for {
		total += width * height * bytesPerPixel
		if width <= 1 && height <= 1 {
			return total
		}
		width, height = halveEdge(width), halveEdge(height)
	}
}

func halveEdge(v int) int {
	if v <= 1 {
		return 1
	}
	return v / 2
}

// setGPURatio fills GPURatio from the two byte counts. A zero denominator leaves
// the ratio at zero rather than producing a not-a-number in JSON.
func setGPURatio(plan *VariantPlan) {
	if plan.GPUBytesRGBA8 > 0 {
		plan.GPURatio = float64(plan.GPUBytes) / float64(plan.GPUBytesRGBA8)
	}
}

// ColorSpaceForName guesses the colour space of a texture from its file name.
//
// The guess is a heuristic and the build records it, so a wrong guess is
// visible in the manifest instead of silent. glTF carries the right answer in
// its material bindings, and a future glTF-aware pass should override this.
//
// The rule is one-way safe in the common case: a data map that leaks through as
// sRGB is a visible material error, so the list errs toward Linear.
func ColorSpaceForName(name string) ColorSpace {
	lower := strings.ToLower(name)
	for _, marker := range []string{
		"normal", "_nrm", "-nrm", "_n.", "roughness", "_rough", "metal", "_orm", "-orm",
		"occlusion", "_ao", "-ao", "ambientocclusion", "height", "displacement",
		"bump", "mask", "specular", "gloss", "opacity", "_data", "-data", "curvature",
	} {
		if strings.Contains(lower, marker) {
			return Linear
		}
	}
	return SRGB
}

// TierForName finds a tier by label.
func TierForName(tiers []Tier, name string) (Tier, bool) {
	for _, tier := range tiers {
		if tier.Name == name {
			return tier, true
		}
	}
	return Tier{}, false
}

// FormatMetric renders a ratio for a metric map.
func FormatMetric(value float64) string { return fmt.Sprintf("%.4f", value) }
