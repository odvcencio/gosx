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
	// Mode is "gradient" or "environment". Empty behaves as "gradient" when
	// any gradient color is set, else the sky is ignored.
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
	if out.Mode == "" && out.TopColor == "" && out.HorizonColor == "" &&
		out.BottomColor == "" && out.Blur == 0 && out.Intensity == 0 {
		return nil
	}
	return &out
}

// skyRaisesEnvironmentFeature reports an authored environment sky.
func skyRaisesEnvironmentFeature(s *Sky) bool {
	return s != nil && s.Mode == "environment"
}

func skyRaisesGradientFeature(s *Sky) bool {
	return s != nil && (s.Mode == "gradient" || (s.Mode == "" &&
		(s.TopColor != "" || s.HorizonColor != "" || s.BottomColor != "")))
}
