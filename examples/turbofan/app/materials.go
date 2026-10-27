package app

import (
	"m31labs.dev/gosx/examples/turbofan/cycle"
	"m31labs.dev/gosx/scene"
)

// These are illustrative finish choices, not measured JT9D optical properties.
// Versioned, shared maps keep all 88 resident meshes on the same small asset set.
func brushedMetal(color string, roughness, normalScale, anisotropy float64) scene.StandardMaterial {
	return scene.StandardMaterial{Color: color, Metalness: 1, Roughness: roughness,
		NormalMap: "/materials/brushed-normal-v1.png", NormalScale: &normalScale,
		RoughnessMap: "/materials/brushed-roughness-v1.png", Anisotropy: anisotropy}
}

var (
	titanium     = brushedMetal("#b8bcbd", .40, .24, .35)
	polished     = brushedMetal("#c6c8c6", .22, .12, .55)
	steel        = brushedMetal("#767d7b", .46, .20, .25)
	hotAlloy     = brushedMetal(cycle.SurfaceLinerColor, .56, .18, .15)
	shell        = scene.StandardMaterial{Color: "#454e4b", Metalness: 0, Roughness: .43, Clearcoat: .12}
	ceramic      = scene.StandardMaterial{Color: "#c8c0ac", Metalness: 0, Roughness: .72}
	sectionPaint = scene.StandardMaterial{Color: "#bd8155", Metalness: 0, Roughness: .55}
)

func studioEnvironment() scene.Environment {
	return scene.Environment{AmbientColor: "#deded7", AmbientIntensity: .10,
		EnvironmentMap: "/materials/studio-v1.png", EnvIntensity: .85, EnvRotation: .35,
		ToneMapping: "aces", Exposure: 1.05}
}
