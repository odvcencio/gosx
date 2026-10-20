package scene

import _ "embed"

// Renderer-owned, linear-light textures accepted as Selena texture uniform
// values. They share the scene Environment.IBL validation, loading and lifetime;
// they are never fetched as URLs. Cube declarations are required for radiance
// and irradiance, texture2d for the BRDF LUT. An unavailable environment binds
// a dimension-correct placeholder and reports environmentInfo.x == 0.
const (
	SelenaEnvironmentRadiance   = "gosx:environment:radiance"
	SelenaEnvironmentIrradiance = "gosx:environment:irradiance"
	SelenaEnvironmentBRDFLUT    = "gosx:environment:brdf-lut"
)

// SelenaEnvironmentSource provides opt-in reflection, irradiance and split-sum
// helpers. Prepend it before parsing/linking application materials. Declare
// context { environmentInfo : vec4 } for the live renderer contract:
// (available, intensity, rotationRadians, radianceMaxLOD). The context cannot be
// shadowed by a material parameter/default. Helpers preserve an explicit linear
// ambient fallback until all IBL products are validated and ready.
//
//go:embed selena_environment.sel
var SelenaEnvironmentSource string
