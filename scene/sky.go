package scene

import "strings"

// Sky selects the scene background source. A nil Sky keeps the flat
// Props.Background clear color, which is the current behavior.
//
// One shared type serves both the authoring surface (Environment.Sky) and
// the wire IR (EnvironmentIR.Sky, IREnvironment.Sky), mirroring how
// EnvironmentIBL (texture_contract.go) already serves both roles. A pointer
// field keeps the wire byte-identical for every existing scene: normalizeSky
// returns nil for an author who never sets Sky, so no "sky" key appears.
type Sky struct {
	// Mode is "gradient", "environment" or "physical". Empty behaves as
	// "gradient" when any gradient color is set, else the sky is ignored.
	//
	// "physical" draws an analytic daylight sky: Rayleigh and Mie single
	// scattering after Preetham, Shirley and Smits (1999) in the real-time
	// form of Hoffman and Preetham (2002), with a sun disk. The same model is
	// evaluated in Go by PhysicalRadiance, so build-time IBL bakes and
	// fallback colors match what the browser draws.
	Mode string `json:"mode,omitempty"`
	// Gradient stops. HorizonColor also serves as the degrade target for
	// backends that cannot draw the environment mode.
	TopColor     string `json:"topColor,omitempty"`
	HorizonColor string `json:"horizonColor,omitempty"`
	BottomColor  string `json:"bottomColor,omitempty"`
	// Blur selects the radiance mip for environment mode, in [0,1]. 0 samples
	// mip 0. It maps to lod = Blur * (mipLevels - 1).
	Blur float64 `json:"blur,omitempty"`
	// Intensity scales the sky radiance in linear light. Zero means 1, the
	// same "unset means default" convention Environment.EnvIntensity uses.
	Intensity float64 `json:"intensity,omitempty"`

	// Physical mode parameters. Zero means the documented default.
	//
	// SunDirection points from the scene toward the sun. Author the key
	// DirectionalLight with the opposite direction so shadows agree with
	// the drawn sun. Default: SunDirectionFromAngles(6, 0), a low sun
	// over -Z.
	SunDirection Vector3 `json:"sunDirection,omitzero"`
	// Turbidity is atmospheric haze, 1 (clear) to 20 (hazy). Default 10.
	Turbidity float64 `json:"turbidity,omitempty"`
	// Rayleigh scales molecular scattering (the blue of the sky), 0 to 8.
	// Default 2.
	Rayleigh float64 `json:"rayleigh,omitempty"`
	// MieCoefficient scales aerosol scattering (the glow around the sun),
	// 0 to 0.1. Default 0.005.
	MieCoefficient float64 `json:"mieCoefficient,omitempty"`
	// MieDirectionalG is the Henyey-Greenstein anisotropy of the aerosol
	// glow, 0 to 0.999. Default 0.8.
	MieDirectionalG float64 `json:"mieDirectionalG,omitempty"`
	// SunDiskRadius is the drawn sun's angular radius in degrees, 0 to 5.
	// Default 0.53. A negative value hides the disk.
	SunDiskRadius float64 `json:"sunDiskRadius,omitempty"`
}

// normalizeSky trims string fields and reports a nil Sky as nil rather than a
// struct of empty strings and zero numbers, so the wire carries no "sky" key
// for a scene that never authored one.
func normalizeSky(s *Sky) *Sky {
	if s == nil {
		return nil
	}
	out := Sky{
		Mode:         strings.ToLower(strings.TrimSpace(s.Mode)),
		TopColor:     strings.TrimSpace(s.TopColor),
		HorizonColor: strings.TrimSpace(s.HorizonColor),
		BottomColor:  strings.TrimSpace(s.BottomColor),
		Blur:         s.Blur,
		Intensity:    s.Intensity,
	}
	if out.Mode == "physical" {
		out.SunDirection = normalizeSunDirection(s.SunDirection)
		out.Turbidity = clampSkyParam(s.Turbidity, 1, 20)
		out.Rayleigh = clampSkyParam(s.Rayleigh, 0, 8)
		out.MieCoefficient = clampSkyParam(s.MieCoefficient, 0, 0.1)
		out.MieDirectionalG = clampSkyParam(s.MieDirectionalG, 0, 0.999)
		out.SunDiskRadius = s.SunDiskRadius
		if out.SunDiskRadius > 5 {
			out.SunDiskRadius = 5
		}
		// Canvas2D and any backend that cannot draw the physical pass fall
		// back to the gradient stops, so fill unset stops from the model.
		fillPhysicalSkyGradient(&out)
		return &out
	}
	if out.Mode == "" && out.TopColor == "" && out.HorizonColor == "" &&
		out.BottomColor == "" && out.Blur == 0 && out.Intensity == 0 {
		return nil
	}
	return &out
}

// clampSkyParam keeps zero (the "use the default" marker) and clamps any
// authored value into its documented range.
func clampSkyParam(v, lo, hi float64) float64 {
	if v == 0 {
		return 0
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func skyRaisesPhysicalFeature(s *Sky) bool {
	return s != nil && s.Mode == "physical"
}

// skyRaisesEnvironmentFeature reports an authored environment sky.
func skyRaisesEnvironmentFeature(s *Sky) bool {
	return s != nil && s.Mode == "environment"
}

func skyRaisesGradientFeature(s *Sky) bool {
	return s != nil && (s.Mode == "gradient" || (s.Mode == "" &&
		(s.TopColor != "" || s.HorizonColor != "" || s.BottomColor != "")))
}
