package scene

import (
	"m31labs.dev/selena"
	"strings"
	"testing"
)

func TestSelenaEnvironmentLibraryCompiles(t *testing.T) {
	for _, expression := range []string{
		"gsxEnvironmentReflection(radiance, environmentInfo, geo.worldNormal, 0.8, ambient)",
		"gsxEnvironmentDiffuse(irradiance, environmentInfo, geo.worldNormal, ambient)",
		"gsxEnvironmentSpecular(radiance, brdf, environmentInfo, geo.worldNormal, 0.6, 0.8, vec3f(0.04,0.04,0.04), 1.0, ambient)",
	} {
		source := SelenaEnvironmentSource + `
material EnvironmentProbe {
    param radiance : textureCube
    param irradiance : textureCube
    param brdf : texture2d
    context { environmentInfo : vec4 ambient : vec3 }
    surface(geo) -> color { return ` + expression + ` }
}`
		m, layout, err := CompileSelenaMaterial([]byte(source), SelenaMaterialOptions{Uniforms: map[string]any{
			"radiance": SelenaEnvironmentRadiance, "irradiance": SelenaEnvironmentIrradiance, "brdf": SelenaEnvironmentBRDFLUT,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(m.FragmentWGSL, "textureSampleLevel") || !strings.Contains(m.FragmentGLSL, "textureLod") {
			t.Fatal("environment prefilter LOD lost")
		}
		found := false
		for _, f := range layout.UniformBlock.Fields {
			if f.Name == "environmentInfo" {
				found = f.Class == "context" && f.Type == "vec4"
			}
		}
		if !found {
			t.Fatal("live environment context contract missing")
		}
		compiled, err := selena.Compile([]byte(source), selena.CompileOptions{})
		if err != nil {
			t.Fatal(err)
		}
		adapted, err := MaterialFromSelena(compiled, SelenaMaterialOptions{Uniforms: map[string]any{
			"radiance": SelenaEnvironmentRadiance, "irradiance": SelenaEnvironmentIrradiance,
			"brdf": SelenaEnvironmentBRDFLUT, "ambient": []float64{.1, .1, .1},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if _, authored := adapted.Uniforms["environmentInfo"]; authored {
			t.Fatal("validation placeholder leaked into authored uniforms")
		}
	}
}
