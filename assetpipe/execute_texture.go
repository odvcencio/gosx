package assetpipe

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"m31labs.dev/gosx/assetpipe/texture"
	"m31labs.dev/gosx/assetpipe/variantsel"
)

// init registers the texture stage with the executor table.
//
// The table lives in execute.go, which another agent owns. Appending from init
// adds the stage without editing that file, and SupportedActions picks it up
// because it reads the same slice.
func init() {
	executors = append(executors,
		executor{action: "texture-transcode-ktx2", kinds: []string{"texture"}, run: runTextureKTX2},
		executor{action: "generate-mips", kinds: []string{"texture"}, run: runGenerateMips},
	)
}

// TextureOptions controls the raster texture stage.
type TextureOptions struct {
	// Filter names the resample kernel: "lanczos3", "mitchell", "triangle",
	// or "box". An empty value selects lanczos3.
	Filter string
	// Tiers overrides the delivery ladder. An empty slice selects
	// texture.DefaultTiers.
	Tiers []texture.Tier
	// NoSupercompress writes plain KTX2 payloads.
	//
	// Leave supercompression on for a texture. A PNG source was already
	// deflate coded, so a plain KTX2 payload runs several times larger on the
	// wire than the file it replaces. The GPU upload size is identical either
	// way, because the driver always receives inflated bytes.
	NoSupercompress bool
	// KeepConstantAlpha writes the alpha channel even when every texel is
	// opaque.
	KeepConstantAlpha bool
	// PortableOnly skips the three-channel rgb8unorm output, which only a
	// WebGL2 consumer can upload.
	PortableOnly bool
	// ColorSpace forces a colour space instead of guessing from the file
	// name. Use "srgb" or "linear".
	ColorSpace string
	// BlockCompression adds one block-compressed variant per tier.
	//
	// The uncompressed variant always ships too, because every block format
	// sits behind an optional device feature. The block variant carries the
	// format token and the device-feature token, so a selector hands it out
	// only when the device itself reported the feature.
	BlockCompression bool
	// Role forces the texture role instead of guessing from the file name.
	// Use one of the texture.Role values, for example "normal" or "mask".
	Role string
	// BlockQuality picks the encoder effort: "fast", "balanced", or "best".
	// An empty value selects balanced.
	BlockQuality string
}

// DefaultTextureOptions returns the settings the Execute path applies.
//
// Execute cannot carry per-stage texture settings yet. ExecuteOptions holds an
// IBL field, a LOD field and an Optimize field, and adding a Texture field
// means editing execute.go, which this change does not own. Until that field
// exists, Execute runs the texture stage on these defaults, and a caller that
// needs other settings calls BuildTexture directly.
//
// BlockCompression is on. A block variant costs encode time once and saves four
// to eight times the GPU memory on every frame a device draws it.
//
// The lacks claim retires this workaround by itself. The moment execute.go
// grows the field, the claim fails and names this comment, so the paragraph
// cannot outlive the limit it describes.
//
//	gosx:claim lacks assetpipe/execute.go `Texture TextureOptions`
//	gosx:claim has assetpipe/execute.go `Optimize OptimizeOptions`
func DefaultTextureOptions() TextureOptions {
	return TextureOptions{Filter: "lanczos3", BlockCompression: true, BlockQuality: "balanced"}
}

// RefusedTextureFormats names the targets the stage will not build, and why.
//
// The list is part of the output record on purpose. A pipeline that quietly
// dropped a target would let a reader assume the KTX2 file carries it.
//
// BC7, BC1, BC3, BC4 and BC5 left this list when the encoders landed. ASTC and
// ETC2 stay: the mobile family needs its own encoders, and the container writer
// still refuses both VkFormats rather than emit a file nobody filled in.
var RefusedTextureFormats = map[string]string{
	"astc": "needs an ASTC block encoder; the container writer refuses the VkFormat",
	"etc2": "needs an ETC2 and EAC block encoder; the container writer refuses the VkFormat",
	"bc6h": "needs a BC6H encoder for high dynamic range; the container writer has no descriptor for it",
}

// runTextureKTX2 builds every KTX2 variant of one raster texture.
//
// The stage resizes to a power of two and to a per-tier ceiling, builds the mip
// chain in linear light, prunes an unused alpha channel, writes the uncompressed
// container, and adds one block-compressed container per tier. ASTC and ETC2
// still have no encoder, and the KTX2 writer refuses those formats rather than
// emit a container whose payload nobody filled in.
//
// Execute replaces the planned variants of this action with the built ones,
// because mergeVariants replaces the variants of an executed action. A planned
// ASTC or ETC2 variant therefore disappears instead of claiming a file. The
// metrics and the sidecar record what was refused and why.
func runTextureKTX2(ctx *executeContext, asset Asset) (actionOutcome, error) {
	data, err := ctx.readSource(asset.Path)
	if err != nil {
		return actionOutcome{}, err
	}
	opts := DefaultTextureOptions()
	result, plans, err := BuildTexture(asset.Path, data, opts)
	if err != nil {
		if errors.Is(err, texture.ErrUnsupportedSource) {
			return actionOutcome{skipReason: fmt.Sprintf("no pure-Go decoder for %s: %v", asset.Path, err)}, nil
		}
		if errors.Is(err, texture.ErrTooManyPixels) {
			return actionOutcome{skipReason: err.Error()}, nil
		}
		return actionOutcome{}, err
	}
	if len(plans) == 0 {
		return actionOutcome{skipReason: "the texture produced no variant"}, nil
	}

	outputs := make([]Variant, 0, len(plans)+1)
	for i, plan := range result.Variants {
		written, err := ctx.writeOutput(plans[i].URI, plan.Data)
		if err != nil {
			return actionOutcome{}, err
		}
		variant := plans[i]
		variant.Bytes = written
		outputs = append(outputs, variant)
	}

	base := strings.TrimSuffix(asset.Path, filepath.Ext(asset.Path))
	sidecarURI := filepath.ToSlash(base + ".textures.json")
	sidecarBytes, err := json.MarshalIndent(textureSidecar{
		SchemaVersion:  1,
		Build:          result,
		RefusedFormats: RefusedTextureFormats,
	}, "", "  ")
	if err != nil {
		return actionOutcome{}, err
	}
	sidecarSize, err := ctx.writeOutput(sidecarURI, append(sidecarBytes, '\n'))
	if err != nil {
		return actionOutcome{}, err
	}
	outputs = append(outputs, Variant{
		URI:          sidecarURI,
		Kind:         "texture-metadata",
		SourceAction: "texture-transcode-ktx2",
		State:        VariantBuilt,
		Bytes:        sidecarSize,
	})

	return actionOutcome{outputs: outputs, metrics: textureMetrics(result)}, nil
}

// BuildTexture builds every KTX2 variant of one source texture and returns the
// build record together with the variant descriptors, in matching order.
//
// The function writes nothing. Execute writes the bytes; a CLI that needs
// non-default settings writes them itself.
func BuildTexture(relPath string, data []byte, opts TextureOptions) (texture.BuildResult, []Variant, error) {
	space := texture.ColorSpaceForName(relPath)
	switch strings.ToLower(strings.TrimSpace(opts.ColorSpace)) {
	case "srgb":
		space = texture.SRGB
	case "linear":
		space = texture.Linear
	}
	result, err := texture.Build(data, texture.BuildOptions{
		ColorSpace:         space,
		Filter:             texture.ParseFilter(strings.ToLower(strings.TrimSpace(opts.Filter))),
		Tiers:              opts.Tiers,
		Supercompress:      !opts.NoSupercompress,
		PruneConstantAlpha: !opts.KeepConstantAlpha,
		EmitPortableOnly:   opts.PortableOnly,
		Source:             relPath,
		BlockCompression:   opts.BlockCompression,
		Role:               texture.Role(strings.ToLower(strings.TrimSpace(opts.Role))),
		BlockQuality:       parseBlockQuality(opts.BlockQuality),
	})
	if err != nil {
		return texture.BuildResult{}, nil, err
	}
	base := strings.TrimSuffix(relPath, filepath.Ext(relPath))
	variants := make([]Variant, 0, len(result.Variants))
	for _, plan := range result.Variants {
		variants = append(variants, Variant{
			URI:                  textureVariantURI(base, plan),
			Kind:                 "texture",
			Quality:              plan.Tier,
			Compression:          textureCompressionLabel(plan),
			SourceAction:         "texture-transcode-ktx2",
			State:                VariantBuilt,
			Bytes:                int64(plan.Bytes),
			RequiredCapabilities: textureVariantCapabilities(plan),
		})
	}
	return result, variants, nil
}

// runGenerateMips reports where the mip chain really comes from.
//
// Plan lists generate-mips as a separate candidate, but a standalone mip file
// has no consumer: a KTX2 container already carries the whole chain, and the
// texture stage writes it there. A deliberate skip keeps the action honest
// without writing a second copy of the same pixels.
func runGenerateMips(ctx *executeContext, asset Asset) (actionOutcome, error) {
	_, _ = ctx, asset
	return actionOutcome{
		skipReason: "texture-transcode-ktx2 writes the full mip chain into the KTX2 container, filtered in linear light",
	}, nil
}

// textureMetrics renders one build record as the flat string map ActionResult
// carries. Every operation reports input bytes, output bytes, the ratio, and
// its own timing.
func textureMetrics(result texture.BuildResult) map[string]string {
	metrics := map[string]string{
		"sourceBytes":  strconv.Itoa(result.SourceBytes),
		"sourcePixels": strconv.Itoa(result.SourceWidth * result.SourceHeight),
		"outputBytes":  strconv.Itoa(result.OutputBytes),
		"colorSpace":   result.ColorSpace,
		"filter":       result.Filter,
		"alphaMode":    result.AlphaMode,
		"grayscale":    strconv.FormatBool(result.Grayscale),
		"variants":     strconv.Itoa(len(result.Variants)),
		"decodeMs":     strconv.FormatInt(result.DecodeMS, 10),
		"mipSpace":     "linear",
		"blockCodecs":  strings.Join(texture.RegisteredBlockCodecs(), ", "),
		"blockRefused": "astc, etc2, and bc6h need their own encoders",
	}
	if result.Role != "" {
		metrics["role"] = result.Role
		metrics["roleGuessed"] = strconv.FormatBool(result.RoleGuessed)
	}
	if result.SourceBytes > 0 {
		metrics["outputRatio"] = texture.FormatMetric(float64(result.OutputBytes) / float64(result.SourceBytes))
	}
	if result.PrunedChannel != "" {
		metrics["prunedChannels"] = result.PrunedChannel
	}
	for _, plan := range result.Variants {
		key := "tier." + plan.Tier + "." + plan.Format
		// Report wire bytes and GPU bytes together. Supercompression moves the
		// first and never the second, so one number alone hides half the story.
		metrics[key] = fmt.Sprintf("%dx%d, %d levels, %d wire bytes, wire ratio %.4f, %d gpu bytes, gpu ratio %.4f, %dms",
			plan.Width, plan.Height, plan.Levels, plan.Bytes, plan.Ratio,
			plan.GPUBytes, plan.GPURatio, plan.DurationMS)
	}
	return metrics
}

// textureVariantURI names one output file.
//
// The name carries the tier and the format, so two variants of one tier never
// collide and a reader can tell what a file holds without opening it.
func textureVariantURI(base string, plan texture.VariantPlan) string {
	return filepath.ToSlash(base + "." + plan.Tier + "." + plan.Format + ".ktx2")
}

// textureCompressionLabel names the container and the pixel format together,
// which is the convention the planned variants already use ("ktx2-bc7").
func textureCompressionLabel(plan texture.VariantPlan) string {
	label := "ktx2-" + plan.Format
	if plan.Supercompress {
		label += "-zlib"
	}
	return label
}

// textureVariantCapabilities lists what a consumer needs to upload one variant.
//
// The tokens come from variantsel, so the strings match the vocabulary the scene
// capability system uses. A selector compares them directly; nothing translates.
//
// A block variant carries two tokens, not one: the format it holds and the
// device feature that format needs. The second token is what stops a block file
// reaching a device that cannot upload it, because no static backend table
// promises an optional feature. Only FromDeviceEvidence supplies it.
func textureVariantCapabilities(plan texture.VariantPlan) []string {
	tokens := []variantsel.Token{variantsel.ContainerKTX2}
	if plan.Supercompress {
		tokens = append(tokens, variantsel.ContainerZlib)
	}
	format := variantsel.Token("texture-format:" + plan.Format)
	switch plan.Format {
	case "rgba8unorm":
		tokens = append(tokens, variantsel.FormatRGBA8Unorm)
	case "rgba8unorm-srgb":
		tokens = append(tokens, variantsel.FormatRGBA8UnormSRGB)
	case "rgb8unorm":
		tokens = append(tokens, variantsel.FormatRGB8Unorm)
	case "rgb8unorm-srgb":
		tokens = append(tokens, variantsel.FormatRGB8UnormSRGB)
	case "rg8unorm":
		tokens = append(tokens, variantsel.FormatRG8Unorm)
	case "r8unorm":
		tokens = append(tokens, variantsel.FormatR8Unorm)
	default:
		if feature, ok := variantsel.BlockFeatureForFormat(format); ok {
			tokens = append(tokens, format, feature)
		}
	}
	tokens = append(tokens, variantsel.TierTokens(plan.Tier)...)
	return variantsel.Strings(tokens...)
}

// parseBlockQuality reads the encoder effort level from an option string.
func parseBlockQuality(name string) texture.BlockQuality {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "fast":
		return texture.BlockQualityFast
	case "best":
		return texture.BlockQualityBest
	}
	return texture.BlockQualityBalanced
}

// textureSidecar is the JSON record a consumer reads for pixel dimensions, mip
// level counts, the measured ratios, and the refused targets.
type textureSidecar struct {
	SchemaVersion  int                 `json:"schemaVersion"`
	Build          texture.BuildResult `json:"build"`
	RefusedFormats map[string]string   `json:"refusedFormats,omitempty"`
}
