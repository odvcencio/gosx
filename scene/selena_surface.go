package scene

import (
	"fmt"
	"strings"

	"m31labs.dev/selena/bindings"
	"m31labs.dev/selena/result"
)

// SelenaTexture binds one texture param of a surface-kind Selena material.
// Put it in SelenaMaterialOptions.Uniforms under the param name. A plain URL
// string also works and samples as linear data.
type SelenaTexture struct {
	URL string
	// ColorSpace is TextureColorSpaceSRGB for color images (base color,
	// emissive) and TextureColorSpaceLinear (the default) for data: normal,
	// roughness, occlusion, height and mask maps.
	ColorSpace TextureColorSpace
}

// selenaSurfaceMaterial adapts a surface-kind Selena material. The material
// carries only fragment hooks (FragmentGLSL and FragmentWGSL); both Scene3D
// renderers splice the hook into their standard PBR fragment, so the
// returned surface is shaded by the same lights, shadows, image-based
// lighting, fog and exposure as a StandardMaterial. opts.Standard supplies the
// non-surface material state: blend mode, alpha cutoff, IOR and specular,
// rim light and wireframe.
func selenaSurfaceMaterial(res result.Result, opts SelenaMaterialOptions) (CustomMaterial, error) {
	hook := res.Layout.Surface
	if hook == nil {
		return CustomMaterial{}, fmt.Errorf("selena surface material %q has no surface hook descriptor", res.Layout.Material)
	}
	if hook.ABI != bindings.SurfaceABI {
		return CustomMaterial{}, fmt.Errorf("selena surface material %q uses surface ABI %q; this GoSX shades %q", res.Layout.Material, hook.ABI, bindings.SurfaceABI)
	}
	wgsl, ok := res.Artifact(result.TargetWGSL)
	if !ok || strings.TrimSpace(wgsl.Source) == "" {
		return CustomMaterial{}, fmt.Errorf("selena surface material %q did not emit a WGSL hook", res.Layout.Material)
	}
	gles, ok := res.Artifact(result.TargetGLES)
	if !ok || strings.TrimSpace(gles.Fragment) == "" {
		return CustomMaterial{}, fmt.Errorf("selena surface material %q did not emit a GLES hook", res.Layout.Material)
	}
	if len(res.Layout.Textures) > bindings.SurfaceMaxTextures || len(hook.TextureNames) != len(res.Layout.Textures) {
		return CustomMaterial{}, fmt.Errorf("selena surface material %q declares %d textures for %d hook names (at most %d)",
			res.Layout.Material, len(res.Layout.Textures), len(hook.TextureNames), bindings.SurfaceMaxTextures)
	}
	uniforms := selenaUniforms(res.Layout, opts.Uniforms)
	textures := make([]map[string]any, len(res.Layout.Textures))
	for i, texture := range res.Layout.Textures {
		binding, err := selenaSurfaceTexture(texture.Name, uniforms[texture.Name])
		if err != nil {
			return CustomMaterial{}, fmt.Errorf("selena surface material %q: %w", res.Layout.Material, err)
		}
		textures[i] = map[string]any{"name": texture.Name, "uri": binding.URL, "colorSpace": string(binding.ColorSpace)}
		if binding.URL == "" {
			delete(uniforms, texture.Name)
		} else {
			uniforms[texture.Name] = binding.URL
		}
	}
	layout := selenaWebGL2LayoutMap(res.Layout)
	layout["surfaceTextures"] = textures
	material := CustomMaterial{
		StandardMaterial: opts.Standard,
		ShaderBackend:    "selena",
		ShaderLayout:     layout,
		FragmentGLSL:     gles.Fragment,
		FragmentWGSL:     wgsl.Source,
		Uniforms:         uniforms,
	}
	if material.Wireframe == nil {
		material.Wireframe = Bool(false)
	}
	// A surface material samples only its own textures; standard maps on the
	// options would be ignored by the hook, so reject them instead.
	for _, slot := range [...]struct{ name, url string }{
		{"Texture", opts.Standard.Texture}, {"NormalMap", opts.Standard.NormalMap}, {"RoughnessMap", opts.Standard.RoughnessMap},
		{"MetalnessMap", opts.Standard.MetalnessMap}, {"OcclusionMap", opts.Standard.OcclusionMap}, {"EmissiveMap", opts.Standard.EmissiveMap},
	} {
		if strings.TrimSpace(slot.url) != "" {
			return CustomMaterial{}, fmt.Errorf("selena surface material %q: Standard.%s is not sampled by a surface hook; declare a texture2d param and sample it", res.Layout.Material, slot.name)
		}
	}
	if _, err := bindings.PackUniformsWithDefaults(res.Layout, selenaSurfaceValidationValues(res.Layout, uniforms)); err != nil {
		return CustomMaterial{}, fmt.Errorf("Selena material %s: %w", res.Layout.Material, err)
	}
	return material, nil
}

// selenaSurfaceTexture reads one texture binding from a uniform value.
func selenaSurfaceTexture(name string, value any) (SelenaTexture, error) {
	switch v := value.(type) {
	case nil:
		return SelenaTexture{ColorSpace: TextureColorSpaceLinear}, nil
	case string:
		return SelenaTexture{URL: strings.TrimSpace(v), ColorSpace: TextureColorSpaceLinear}, nil
	case SelenaTexture:
		return normalizeSelenaTexture(name, v)
	case *SelenaTexture:
		if v == nil {
			return SelenaTexture{ColorSpace: TextureColorSpaceLinear}, nil
		}
		return normalizeSelenaTexture(name, *v)
	default:
		return SelenaTexture{}, fmt.Errorf("texture %s: want a URL string or scene.SelenaTexture, got %T", name, value)
	}
}

func normalizeSelenaTexture(name string, t SelenaTexture) (SelenaTexture, error) {
	t.URL = strings.TrimSpace(t.URL)
	switch t.ColorSpace {
	case "":
		t.ColorSpace = TextureColorSpaceLinear
	case TextureColorSpaceLinear, TextureColorSpaceSRGB:
	default:
		return SelenaTexture{}, fmt.Errorf("texture %s: unknown color space %q (want srgb or linear)", name, t.ColorSpace)
	}
	return t, nil
}

// selenaSurfaceValidationValues adds zero placeholders for context fields so
// authored values validate without the per-frame engine inputs.
func selenaSurfaceValidationValues(layout bindings.Layout, uniforms map[string]any) map[string]any {
	out := make(map[string]any, len(uniforms)+len(layout.Context))
	//gosx:allow map-range copies each entry independently; order does not matter
	for name, value := range uniforms {
		out[name] = value
	}
	for _, field := range layout.UniformBlock.Fields {
		if _, ok := out[field.Name]; !ok && field.Class == "context" {
			out[field.Name] = selenaZeroValue(field)
		}
	}
	return out
}
