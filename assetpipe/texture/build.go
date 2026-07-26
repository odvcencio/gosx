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
		Data:          data,
	})

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
				Data:          rgbData,
			})
		} else {
			out[len(out)-1].AlphaPruneRejected = true
			out[len(out)-1].AlphaPruneBytes = len(rgbData)
		}
	}
	return out, nil
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
