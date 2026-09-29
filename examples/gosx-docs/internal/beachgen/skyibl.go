package beachgen

import (
	"fmt"
	"math"

	"m31labs.dev/gosx/assetpipe/ibl"
	"m31labs.dev/gosx/render/bundle/ktx2"
	"m31labs.dev/gosx/scene"
)

// Sky periods of Blackglass Beach. The scene program draws these skies and
// the generator bakes the IBL from the same values, so reflections and
// ambient light always match the sky on screen.
const (
	PeriodGolden = "golden-hour"
	PeriodBlue   = "blue-hour"
	PeriodNoon   = "noon"
)

// Periods lists the baked periods in display order.
var Periods = []string{PeriodGolden, PeriodBlue, PeriodNoon}

// PeriodSky returns the physical sky of one period.
func PeriodSky(period string) scene.Sky {
	switch period {
	case PeriodBlue:
		return scene.Sky{Mode: "physical", SunDirection: scene.SunDirectionFromAngles(-1.5, -18), Turbidity: 3, Rayleigh: 3.2, MieCoefficient: 0.004, MieDirectionalG: 0.78}
	case PeriodNoon:
		return scene.Sky{Mode: "physical", SunDirection: scene.SunDirectionFromAngles(52, -35), Turbidity: 3.5, Rayleigh: 1.2, MieCoefficient: 0.004, MieDirectionalG: 0.8}
	default:
		return scene.Sky{Mode: "physical", SunDirection: scene.SunDirectionFromAngles(4.5, -14), Turbidity: 6, Rayleigh: 1.7, MieCoefficient: 0.006, MieDirectionalG: 0.82}
	}
}

// Sky IBL sizes: a 64 px radiance cube is sharp enough for the scene's
// glossiest surfaces (wet sand at roughness 0.1) because the sky itself is
// smooth; the sun is carried by the key light, not the cube.
const (
	skyRadianceSize   = 64
	skyIrradianceSize = 8
	skyBRDFLUTSize    = 64
	skyPrefilterRuns  = 256
)

// groundRadiance fills the hemisphere below the horizon. Toward the open sea
// (-Z) it is dark water that mirrors the sky with Fresnel weighting; toward
// land (+Z) it is black volcanic sand lit by the sky. Glossy surfaces (the
// monolith, wet sand) then reflect a sea horizon instead of more sky.
func groundRadiance(sky scene.Sky, d ibl.Vec3) (float64, float64, float64) {
	r, g, b := sky.PhysicalRadiance(scene.Vector3{X: d.X, Y: d.Y, Z: d.Z}, false)
	if d.Y >= 0 {
		return r, g, b
	}
	ur, ug, ub := sky.PhysicalRadiance(scene.Vector3{Y: 1}, false)
	// Sea: reflect the direction about the surface and weight by Fresnel.
	cosI := -d.Y
	fresnel := 0.02 + 0.98*math.Pow(1-cosI, 5)
	rr, rg, rb := sky.PhysicalRadiance(scene.Vector3{X: d.X, Y: -d.Y, Z: d.Z}, false)
	deep := [3]float64{0.012, 0.045, 0.06} // linear deep-water body colour times sky light
	sea := [3]float64{
		fresnel*rr + (1-fresnel)*deep[0]*ur*3,
		fresnel*rg + (1-fresnel)*deep[1]*ug*3,
		fresnel*rb + (1-fresnel)*deep[2]*ub*3,
	}
	const sandAlbedo = 0.045
	sand := [3]float64{sandAlbedo * ur * 2, sandAlbedo * ug * 2, sandAlbedo * ub * 2}
	// Sea toward -Z, sand toward +Z, with a soft seam; the horizon itself
	// stays continuous with the sky for the first 7 degrees below it.
	seaWeight := 1 - smoothstepGo(-0.35, 0.35, d.Z)
	horizon := math.Min(1, -d.Y/0.12)
	out := [3]float64{}
	sky3 := [3]float64{r, g, b}
	for i := range out {
		ground := seaWeight*sea[i] + (1-seaWeight)*sand[i]
		out[i] = sky3[i] + (ground-sky3[i])*horizon
	}
	return out[0], out[1], out[2]
}

func smoothstepGo(e0, e1, x float64) float64 {
	t := math.Max(0, math.Min(1, (x-e0)/(e1-e0)))
	return t * t * (3 - 2*t)
}

// BakeSkyIBL renders the period's sky into radiance, irradiance and BRDF
// products under uriPrefix (for example "/env/blackglass-beach/golden-hour").
// It returns the encoded files keyed by base name and the descriptor the
// scene program embeds as Environment.IBL.
func BakeSkyIBL(period, uriPrefix string) (map[string][]byte, scene.EnvironmentIBL, error) {
	sky := PeriodSky(period)
	cube := ibl.CubeFromRadiance(skyRadianceSize, func(d ibl.Vec3) (float64, float64, float64) { return groundRadiance(sky, d) })
	chain := ibl.Prefilter(cube, ibl.PrefilterOptions{Samples: skyPrefilterRuns, MipSelect: true})
	shSource := cube
	for _, level := range ibl.BuildChain(cube) {
		if level.Size <= 32 {
			shSource = level
			break
		}
	}
	sh := ibl.ProjectSH(shSource)
	irradiance := ibl.IrradianceCube(sh, skyIrradianceSize)
	roughness := make([]float64, len(chain))
	for level := range chain {
		roughness[level] = ibl.RoughnessForLevel(level, len(chain))
	}
	source := "physical-sky:" + period
	radianceBytes, err := ibl.EncodeCubeKTX2Options(chain, map[string]string{
		"GoSXiblModel": ibl.BRDFModel, "GoSXiblRole": "environment-radiance", "GoSXColorSpace": "linear", "GoSXiblSource": source,
	}, ktx2.SupercompressionZlib)
	if err != nil {
		return nil, scene.EnvironmentIBL{}, fmt.Errorf("encode radiance: %w", err)
	}
	irradianceBytes, err := ibl.EncodeCubeKTX2Options(ibl.Chain{irradiance}, map[string]string{
		"GoSXiblModel": "lambert-sh9", "GoSXiblRole": "environment-irradiance", "GoSXColorSpace": "linear", "GoSXiblSource": source,
	}, ktx2.SupercompressionZlib)
	if err != nil {
		return nil, scene.EnvironmentIBL{}, fmt.Errorf("encode irradiance: %w", err)
	}
	lutBytes, err := ibl.EncodeBRDFLUTKTX2(ibl.GenerateBRDFLUT(skyBRDFLUTSize, ibl.DefaultBRDFSamples), map[string]string{
		"GoSXiblModel": ibl.BRDFModel, "GoSXiblRole": "brdf-lut", "GoSXColorSpace": "linear", "GoSXlutAxes": "x=NdotV,y=roughness,texel-centre",
	})
	if err != nil {
		return nil, scene.EnvironmentIBL{}, fmt.Errorf("encode BRDF LUT: %w", err)
	}
	harmonics := make([][3]float64, len(sh))
	for i, c := range sh {
		harmonics[i] = [3]float64{c.X, c.Y, c.Z}
	}
	env := scene.EnvironmentIBL{
		SchemaVersion: 1, Source: source, BRDFModel: ibl.BRDFModel, RoughnessPerLevel: roughness, SphericalHarmonics: harmonics,
		Radiance:   scene.TextureDescriptor{URI: uriPrefix + ".ibl.ktx2", Role: "environment-radiance", ColorSpace: "linear", Channels: "rgba", View: "cube", Format: "rgba16f", MipLevels: len(chain), Width: skyRadianceSize, Height: skyRadianceSize, Faces: 6},
		Irradiance: scene.TextureDescriptor{URI: uriPrefix + ".irradiance.ktx2", Role: "environment-irradiance", ColorSpace: "linear", Channels: "rgba", View: "cube", Format: "rgba16f", MipLevels: 1, Width: skyIrradianceSize, Height: skyIrradianceSize, Faces: 6},
		BRDFLUT:    scene.TextureDescriptor{URI: uriPrefix + ".brdf-lut.ktx2", Role: "brdf-lut", ColorSpace: "linear", Channels: "rg", View: "2d", Format: "rg16f", MipLevels: 1, Width: skyBRDFLUTSize, Height: skyBRDFLUTSize, Faces: 1},
	}
	return map[string][]byte{"ibl.ktx2": radianceBytes, "irradiance.ktx2": irradianceBytes, "brdf-lut.ktx2": lutBytes}, env, nil
}
