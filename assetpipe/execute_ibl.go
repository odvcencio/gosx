package assetpipe

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"m31labs.dev/gosx/assetpipe/hdrimage"
	"m31labs.dev/gosx/assetpipe/ibl"
	"m31labs.dev/gosx/assetpipe/variantsel"
	"m31labs.dev/gosx/render/bundle/ktx2"
)

// IBLOptions controls the image-based lighting stage.
type IBLOptions struct {
	// CubeSize is the edge length of the prefiltered specular cubemap.
	// Zero selects DefaultIBLCubeSize.
	CubeSize int
	// Samples is the number of GGX directions per prefiltered texel. Zero
	// selects ibl.DefaultPrefilterSamples.
	Samples int
	// ProjectionSamples is the supersampling factor per cube texel while the
	// equirectangular source projects onto the cube. Zero selects 2.
	ProjectionSamples int
	// IrradianceSize is the edge length of the diffuse cubemap. Zero selects
	// DefaultIrradianceSize.
	IrradianceSize int
	// BRDFLUTSize is the edge length of the split-sum table. Zero selects
	// ibl.DefaultBRDFLUTSize.
	BRDFLUTSize int
	// BRDFSamples is the sample count per lookup table texel. Zero selects
	// ibl.DefaultBRDFSamples.
	BRDFSamples int
	// BRDFLUTPath overrides the output path of the split-sum table. The
	// table is scene independent, so one file can serve a whole project.
	BRDFLUTPath string
	// Supercompress writes the cubemap payloads with KTX2 zlib
	// supercompression. A 256 pixel prefiltered sky drops from 4.19 MB to
	// 365 KB, at the cost of an inflate step before upload. Leave it off
	// when the HTTP layer already compresses the response, because the
	// upload bytes do not change either way.
	Supercompress bool
}

// Defaults for the IBL stage.
const (
	DefaultIBLCubeSize    = 256
	DefaultIrradianceSize = 32
)

// runPrefilterIBL builds the GGX-prefiltered specular cubemap, the diffuse
// irradiance cubemap, and the spherical-harmonic sidecar for one environment
// asset.
func runPrefilterIBL(ctx *executeContext, asset Asset) (actionOutcome, error) {
	data, err := ctx.readSource(asset.Path)
	if err != nil {
		return actionOutcome{}, err
	}
	image, err := hdrimage.Decode(data)
	if err != nil {
		return actionOutcome{}, fmt.Errorf("decode %s: %w", asset.Path, err)
	}

	opts := ctx.opts.IBL
	cubeSize := opts.CubeSize
	if cubeSize <= 0 {
		cubeSize = DefaultIBLCubeSize
	}
	samples := opts.Samples
	if samples <= 0 {
		samples = ibl.DefaultPrefilterSamples
	}
	projection := opts.ProjectionSamples
	if projection <= 0 {
		projection = 2
	}
	irradianceSize := opts.IrradianceSize
	if irradianceSize <= 0 {
		irradianceSize = DefaultIrradianceSize
	}

	source, err := ibl.EquirectToCube(equirectSource{image}, cubeSize, projection)
	if err != nil {
		return actionOutcome{}, err
	}
	chain := ibl.Prefilter(source, ibl.PrefilterOptions{Samples: samples, MipSelect: true})

	// Project the harmonics from a small level. The projection weights every
	// texel by its solid angle, so a small level carries the same low bands
	// at a fraction of the cost.
	shSource := source
	for _, level := range ibl.BuildChain(source) {
		if level.Size <= 32 {
			shSource = level
			break
		}
	}
	harmonics := ibl.ProjectSH(shSource)
	irradiance := ibl.IrradianceCube(harmonics, irradianceSize)

	roughness := make([]string, len(chain))
	for level := range chain {
		roughness[level] = strconv.FormatFloat(ibl.RoughnessForLevel(level, len(chain)), 'f', 4, 64)
	}
	keyValues := map[string]string{
		"GoSXiblModel":     ibl.BRDFModel,
		"GoSXiblRoughness": strings.Join(roughness, ","),
		"GoSXiblSamples":   strconv.Itoa(samples),
		"GoSXiblSource":    asset.Path,
	}

	scheme := ktx2.SupercompressionNone
	if opts.Supercompress {
		scheme = ktx2.SupercompressionZlib
	}
	specularBytes, err := ibl.EncodeCubeKTX2Options(chain, keyValues, scheme)
	if err != nil {
		return actionOutcome{}, err
	}
	specularURI := plannedVariantURI(asset, "prefilter-ibl-ggx", ".ibl", ".ktx2")
	specularSize, err := ctx.writeOutput(specularURI, specularBytes)
	if err != nil {
		return actionOutcome{}, err
	}

	irradianceBytes, err := ibl.EncodeCubeKTX2Options(ibl.Chain{irradiance}, map[string]string{
		"GoSXiblModel":  "lambert-sh9",
		"GoSXiblSource": asset.Path,
	}, scheme)
	if err != nil {
		return actionOutcome{}, err
	}
	irradianceURI := siblingVariant(asset.Path, ".irradiance", ".ktx2")
	irradianceFileSize, err := ctx.writeOutput(irradianceURI, irradianceBytes)
	if err != nil {
		return actionOutcome{}, err
	}

	sidecar := iblSidecar{
		SchemaVersion:      1,
		Source:             asset.Path,
		SourceFormat:       image.Source,
		SourceWidth:        image.Width,
		SourceHeight:       image.Height,
		BRDFModel:          ibl.BRDFModel,
		SpecularURI:        specularURI,
		SpecularSize:       cubeSize,
		SpecularMipLevels:  len(chain),
		SpecularSamples:    samples,
		IrradianceURI:      irradianceURI,
		IrradianceSize:     irradianceSize,
		Format:             "rgba16f",
		RoughnessPerLevel:  make([]float64, len(chain)),
		SphericalHarmonics: make([][3]float64, 9),
		Consumer:           iblConsumerNote(),
	}
	for level := range chain {
		sidecar.RoughnessPerLevel[level] = ibl.RoughnessForLevel(level, len(chain))
	}
	for i, coefficient := range harmonics {
		sidecar.SphericalHarmonics[i] = [3]float64{coefficient.X, coefficient.Y, coefficient.Z}
	}
	sidecarBytes, err := json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		return actionOutcome{}, err
	}
	sidecarURI := siblingVariant(asset.Path, ".ibl", ".json")
	sidecarSize, err := ctx.writeOutput(sidecarURI, append(sidecarBytes, '\n'))
	if err != nil {
		return actionOutcome{}, err
	}

	// The capability tokens come from variantsel, so the environment variants
	// and the texture variants share one vocabulary with the scene capability
	// system. A selector compares the strings directly; nothing translates.
	cubeTokens := []variantsel.Token{
		variantsel.ContainerKTX2,
		variantsel.FormatRGBA16Float,
		variantsel.ViewCube,
	}
	if opts.Supercompress {
		cubeTokens = append(cubeTokens, variantsel.ContainerZlib)
	}
	return actionOutcome{
		outputs: []Variant{
			{
				URI:                  specularURI,
				Kind:                 "environment",
				Compression:          "ktx2",
				SourceAction:         "prefilter-ibl-ggx",
				State:                VariantBuilt,
				Bytes:                specularSize,
				RequiredCapabilities: variantsel.Strings(cubeTokens...),
			},
			{
				URI:                  irradianceURI,
				Kind:                 "environment-irradiance",
				Compression:          "ktx2",
				SourceAction:         "prefilter-ibl-ggx",
				State:                VariantBuilt,
				Bytes:                irradianceFileSize,
				RequiredCapabilities: variantsel.Strings(cubeTokens...),
			},
			{
				URI:          sidecarURI,
				Kind:         "environment-metadata",
				SourceAction: "prefilter-ibl-ggx",
				State:        VariantBuilt,
				Bytes:        sidecarSize,
			},
		},
		metrics: map[string]string{
			"sourcePixels":  strconv.Itoa(image.Width * image.Height),
			"cubeSize":      strconv.Itoa(cubeSize),
			"mipLevels":     strconv.Itoa(len(chain)),
			"samples":       strconv.Itoa(samples),
			"specularBytes": strconv.FormatInt(specularSize, 10),
		},
	}, nil
}

// runSplitSumLUT bakes the scene-independent split-sum table once per run and
// writes it wherever the plan reserved a URI.
func runSplitSumLUT(ctx *executeContext, asset Asset) (actionOutcome, error) {
	opts := ctx.opts.IBL
	size := opts.BRDFLUTSize
	if size <= 0 {
		size = ibl.DefaultBRDFLUTSize
	}
	samples := opts.BRDFSamples
	if samples <= 0 {
		samples = ibl.DefaultBRDFSamples
	}
	if ctx.brdfLUT == nil {
		lut := ibl.GenerateBRDFLUT(size, samples)
		encoded, err := ibl.EncodeBRDFLUTKTX2(lut, map[string]string{
			"GoSXiblModel":   ibl.BRDFModel,
			"GoSXlutAxes":    "x=NdotV,y=roughness,texel-centre",
			"GoSXlutSamples": strconv.Itoa(samples),
		})
		if err != nil {
			return actionOutcome{}, err
		}
		ctx.brdfLUT = encoded
	}
	uri := strings.TrimSpace(opts.BRDFLUTPath)
	reused := false
	switch {
	case uri != "":
		if ctx.brdfLUTPath == uri {
			reused = true
		}
	default:
		uri = plannedVariantURI(asset, "generate-split-sum-lut", ".brdf-lut", ".ktx2")
	}
	var size64 int64
	if reused {
		size64 = int64(len(ctx.brdfLUT))
	} else {
		written, err := ctx.writeOutput(uri, ctx.brdfLUT)
		if err != nil {
			return actionOutcome{}, err
		}
		size64 = written
		ctx.brdfLUTPath = uri
	}

	return actionOutcome{
		outputs: []Variant{{
			URI:                  uri,
			Kind:                 "environment-lut",
			Compression:          "ktx2",
			SourceAction:         "generate-split-sum-lut",
			State:                VariantBuilt,
			Bytes:                size64,
			RequiredCapabilities: variantsel.Strings(variantsel.ContainerKTX2, variantsel.FormatRG16Float),
		}},
		metrics: map[string]string{
			"lutSize":          strconv.Itoa(size),
			"lutSamples":       strconv.Itoa(samples),
			"sceneIndependent": "true",
		},
	}, nil
}

// iblSidecar is the JSON contract a renderer reads to bind the generated
// environment products.
type iblSidecar struct {
	SchemaVersion      int          `json:"schemaVersion"`
	Source             string       `json:"source"`
	SourceFormat       string       `json:"sourceFormat,omitempty"`
	SourceWidth        int          `json:"sourceWidth,omitempty"`
	SourceHeight       int          `json:"sourceHeight,omitempty"`
	BRDFModel          string       `json:"brdfModel"`
	SpecularURI        string       `json:"specularUri"`
	SpecularSize       int          `json:"specularSize"`
	SpecularMipLevels  int          `json:"specularMipLevels"`
	SpecularSamples    int          `json:"specularSamples"`
	IrradianceURI      string       `json:"irradianceUri"`
	IrradianceSize     int          `json:"irradianceSize"`
	Format             string       `json:"format"`
	RoughnessPerLevel  []float64    `json:"roughnessPerLevel"`
	SphericalHarmonics [][3]float64 `json:"sphericalHarmonics"`
	Consumer           IBLConsumer  `json:"consumer"`
}

// IBLConsumer reports whether a shipped renderer can read these products.
//
// The block is in the sidecar because the gap is a fact about this build, not a
// wish list. A build that writes a prefiltered cubemap nobody samples should say
// so in the file, next to the cubemap.
type IBLConsumer struct {
	// Ready is true only when every requirement in ibl.ConsumerRequirements
	// reports Present.
	Ready bool `json:"ready"`
	// Missing lists the requirement identifiers no renderer implements.
	Missing []string `json:"missing,omitempty"`
	// Note states what the runtime does instead.
	Note string `json:"note,omitempty"`
	// BRDFModelAuthority names the record a shader author must follow.
	BRDFModelAuthority string `json:"brdfModelAuthority"`
}

// iblConsumerNote builds the sidecar's consumer block from the requirement list.
func iblConsumerNote() IBLConsumer {
	note := IBLConsumer{
		Ready:              true,
		BRDFModelAuthority: "assetpipe/ibl.BRDFModel; the sidecar field and the GoSXiblModel container key are copies",
	}
	for _, requirement := range ibl.ConsumerRequirements() {
		if requirement.Present {
			continue
		}
		note.Ready = false
		note.Missing = append(note.Missing, requirement.ID)
	}
	if !note.Ready {
		note.Note = "no shipped renderer samples these products. The WebGL2 path tone maps the source " +
			"HDR to 8-bit and samples one equirectangular 2D texture twice, with no prefiltering and no " +
			"split-sum term. See assetpipe/ibl.ConsumerRequirements for what each missing piece needs."
	}
	return note
}

// equirectSource adapts a decoded HDR image to the projector interface.
type equirectSource struct{ image *hdrimage.Image }

func (s equirectSource) Size() (int, int) { return s.image.Width, s.image.Height }

func (s equirectSource) RGB(x, y int) (float32, float32, float32) { return s.image.At(x, y) }
