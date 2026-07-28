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
}

// DefaultTextureOptions returns the settings the Execute path applies.
//
// Execute cannot carry per-stage texture settings yet. ExecuteOptions holds an
// IBL field and a LOD field, and adding a Texture field means editing
// execute.go, which this change does not own. Until that field exists, Execute
// runs the texture stage on these defaults, and a caller that needs other
// settings calls BuildTexture directly.
func DefaultTextureOptions() TextureOptions {
	return TextureOptions{Filter: "lanczos3"}
}

// RefusedTextureFormats names the targets the stage will not build, and why.
//
// The list is part of the output record on purpose. A pipeline that quietly
// dropped bc7 would let a reader assume the KTX2 file is block compressed.
var RefusedTextureFormats = map[string]string{
	"bc7":  "needs a rate-distortion BC7 block encoder; the container writer refuses the VkFormat",
	"astc": "needs an ASTC block encoder; the container writer refuses the VkFormat",
	"etc2": "needs an ETC2 block encoder; the container writer refuses the VkFormat",
}

// runTextureKTX2 builds every KTX2 variant of one raster texture.
//
// The stage resizes to a power of two and to a per-tier ceiling, builds the mip
// chain in linear light, prunes an unused alpha channel, and writes the
// container. It does no block compression: BC7, ASTC, and ETC2 each need a
// rate-distortion block encoder, and the KTX2 writer refuses those formats
// rather than emit a container whose payload nobody filled in.
//
// Execute drops the planned block-compressed variants of this action when the
// stage runs, because mergeVariants replaces the variants of an executed action.
// That is the honest result: those files were never going to exist. The metrics
// and the sidecar both record the refusal.
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
		"blockCodecs":  "refused: bc7, astc, and etc2 need a block encoder",
	}
	if result.SourceBytes > 0 {
		metrics["outputRatio"] = texture.FormatMetric(float64(result.OutputBytes) / float64(result.SourceBytes))
	}
	if result.PrunedChannel != "" {
		metrics["prunedChannels"] = result.PrunedChannel
	}
	for _, plan := range result.Variants {
		key := "tier." + plan.Tier + "." + plan.Format
		metrics[key] = fmt.Sprintf("%dx%d, %d levels, %d bytes, ratio %.4f, %dms",
			plan.Width, plan.Height, plan.Levels, plan.Bytes, plan.Ratio, plan.DurationMS)
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
func textureVariantCapabilities(plan texture.VariantPlan) []string {
	tokens := []variantsel.Token{variantsel.ContainerKTX2}
	if plan.Supercompress {
		tokens = append(tokens, variantsel.ContainerZlib)
	}
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
	}
	tokens = append(tokens, variantsel.TierTokens(plan.Tier)...)
	return variantsel.Strings(tokens...)
}

// textureSidecar is the JSON record a consumer reads for pixel dimensions, mip
// level counts, the measured ratios, and the refused targets.
type textureSidecar struct {
	SchemaVersion  int                 `json:"schemaVersion"`
	Build          texture.BuildResult `json:"build"`
	RefusedFormats map[string]string   `json:"refusedFormats,omitempty"`
}
