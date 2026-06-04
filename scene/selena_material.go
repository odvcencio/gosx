package scene

import (
	"encoding/json"
	"fmt"
	"strings"

	"m31labs.dev/selena"
	"m31labs.dev/selena/bindings"
)

// SelenaMaterialOptions controls how .sel source is compiled into a Scene3D
// CustomMaterial. Material selects a named material from the source; empty uses
// Selena's default of the last material in the program.
type SelenaMaterialOptions struct {
	Material string
	Standard StandardMaterial
	Uniforms map[string]any
}

// CompileSelenaMaterial compiles Selena .sel source into GoSX's native
// CustomMaterial transport. Selena is Scene3D's default shader authoring
// backend: the returned material carries GLSL for WebGL and WGSL for WebGPU,
// plus the binding layout the runtime uses to wire uniforms and textures.
func CompileSelenaMaterial(source []byte, opts SelenaMaterialOptions) (CustomMaterial, bindings.Layout, error) {
	result, err := selena.Compile(source, selena.CompileOptions{
		Material: opts.Material,
		Targets:  []selena.Target{selena.TargetWGSL, selena.TargetGLSL},
	})
	if err != nil {
		return CustomMaterial{}, bindings.Layout{}, err
	}

	wgsl, ok := result.Artifact(selena.TargetWGSL)
	if !ok || strings.TrimSpace(wgsl.Source) == "" {
		return CustomMaterial{}, bindings.Layout{}, fmt.Errorf("selena material %q did not emit WGSL", result.Material.Name)
	}
	glsl, ok := result.Artifact(selena.TargetGLSL)
	if !ok || strings.TrimSpace(glsl.Vertex) == "" || strings.TrimSpace(glsl.Fragment) == "" {
		return CustomMaterial{}, bindings.Layout{}, fmt.Errorf("selena material %q did not emit GLSL vertex/fragment shaders", result.Material.Name)
	}

	material := CustomMaterial{
		StandardMaterial: opts.Standard,
		ShaderBackend:    "selena",
		ShaderLayout:     selenaLayoutMap(result.Layout),
		VertexGLSL:       glsl.Vertex,
		FragmentGLSL:     glsl.Fragment,
		VertexWGSL:       wgsl.Source,
		FragmentWGSL:     wgsl.Source,
		Uniforms:         selenaUniforms(result.Layout, opts.Uniforms),
	}
	return material, result.Layout, nil
}

func selenaLayoutMap(layout bindings.Layout) map[string]any {
	encoded, err := json.Marshal(layout)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil
	}
	return out
}

func selenaUniforms(layout bindings.Layout, overrides map[string]any) map[string]any {
	out := selenaDefaultUniforms(layout)
	if len(overrides) == 0 {
		return out
	}
	if out == nil {
		out = make(map[string]any, len(overrides))
	}
	for name, value := range overrides {
		if strings.TrimSpace(name) == "" {
			continue
		}
		out[name] = cloneSelenaUniformValue(value)
	}
	return out
}

func selenaDefaultUniforms(layout bindings.Layout) map[string]any {
	if len(layout.UniformBlock.Defaults) == 0 {
		return nil
	}
	out := make(map[string]any, len(layout.UniformBlock.Defaults))
	for _, d := range layout.UniformBlock.Defaults {
		if len(d.Values) == 1 {
			out[d.Name] = d.Values[0]
			continue
		}
		out[d.Name] = append([]float32(nil), d.Values...)
	}
	return out
}

func cloneSelenaUniformValue(value any) any {
	switch v := value.(type) {
	case []float32:
		return append([]float32(nil), v...)
	case []float64:
		return append([]float64(nil), v...)
	case []int:
		return append([]int(nil), v...)
	case []any:
		return append([]any(nil), v...)
	default:
		return value
	}
}
