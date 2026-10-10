package scene

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"m31labs.dev/gosx/engine"

	"m31labs.dev/selena"
	"m31labs.dev/selena/bindings"
	"m31labs.dev/selena/parse"
)

// SelenaMaterialOptions controls how .sel source is compiled into a Scene3D
// CustomMaterial. Material selects a named material from the source; empty uses
// Selena's default of the last material in the program.
type SelenaMaterialOptions struct {
	Material string
	Standard StandardMaterial
	Uniforms map[string]any
	// Targets adds artifacts to transport alongside the browser programs.
	// Use selena.AllTargets() to retain all programs for native adapters.
	Targets []selena.Target
}

// SelenaCompiledMaterial is one Scene3D material produced from a Selena source
// bundle, with its host binding layout kept next to the custom shader payload.
type SelenaCompiledMaterial struct {
	Name     string
	Material CustomMaterial
	Layout   bindings.Layout
}

// SelenaUniforms converts a tagged Go struct or map into Selena host uniform
// overrides. Struct fields use `selena:"name"` when present, then `json:"name"`,
// then a lower-camel version of the exported Go field name. Slices and arrays
// are converted to cloned slices so vector/color uniforms are safe to reuse.
func SelenaUniforms(values any) (map[string]any, error) {
	if values == nil {
		return nil, nil
	}
	v := reflect.ValueOf(values)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, nil
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("selena uniforms map key must be string, got %s", v.Type().Key())
		}
		out := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			name := strings.TrimSpace(iter.Key().String())
			if name == "" {
				continue
			}
			value, err := selenaUniformReflectValue(iter.Value())
			if err != nil {
				return nil, fmt.Errorf("uniform %s: %w", name, err)
			}
			out[name] = value
		}
		return out, nil
	case reflect.Struct:
		out := make(map[string]any, v.NumField())
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			name := selenaUniformFieldName(field)
			if name == "" {
				continue
			}
			value, err := selenaUniformReflectValue(v.Field(i))
			if err != nil {
				return nil, fmt.Errorf("uniform %s: %w", name, err)
			}
			out[name] = value
		}
		return out, nil
	default:
		return nil, fmt.Errorf("selena uniforms must be a struct or map, got %s", v.Kind())
	}
}

// CompileSelenaMaterial compiles Selena .sel source into GoSX's native
// CustomMaterial transport. Selena is Scene3D's default shader authoring
// backend: the returned material carries GLSL ES 3.00 for WebGL2 and WGSL for WebGPU,
// plus the binding layout the runtime uses to wire uniforms and textures.
func CompileSelenaMaterial(source []byte, opts SelenaMaterialOptions) (CustomMaterial, bindings.Layout, error) {
	result, err := selena.Compile(source, selena.CompileOptions{
		Material: opts.Material,
		Targets:  selenaMaterialTargets(opts),
	})
	if err != nil {
		return CustomMaterial{}, bindings.Layout{}, err
	}
	material, err := selenaCustomMaterial(result, opts)
	if err != nil {
		return CustomMaterial{}, bindings.Layout{}, err
	}
	material.VertexWGSL = selenaPointsWGSLRuntimeFrameLayout(material.VertexWGSL)
	material.FragmentWGSL = selenaPointsWGSLRuntimeFrameLayout(material.FragmentWGSL)
	return material, selenaWebGL2Layout(result.Layout), nil
}

// CompileSelenaBundle compiles one .sel source into an ordered set of Scene3D
// custom materials. It parses the source once, then compiles each requested
// material name with its own standard material and host uniform overrides.
// Passing no material options compiles Selena's default material.
func CompileSelenaBundle(source []byte, materials ...SelenaMaterialOptions) ([]SelenaCompiledMaterial, error) {
	program, err := parse.Program(source)
	if err != nil {
		return nil, err
	}
	if len(materials) == 0 {
		materials = []SelenaMaterialOptions{{}}
	}

	out := make([]SelenaCompiledMaterial, 0, len(materials))
	for _, opts := range materials {
		result, err := selena.CompileProgram(program, selena.CompileOptions{
			Material: opts.Material,
			Targets:  selenaMaterialTargets(opts),
		})
		if err != nil {
			label := opts.Material
			if strings.TrimSpace(label) == "" {
				label = "<default>"
			}
			return nil, fmt.Errorf("selena material %s: %w", label, err)
		}
		material, err := selenaCustomMaterial(result, opts)
		if err != nil {
			return nil, err
		}
		out = append(out, SelenaCompiledMaterial{
			Name:     result.Material.Name,
			Material: material,
			Layout:   selenaWebGL2Layout(result.Layout),
		})
	}
	return out, nil
}

// CompileSelenaPoints compiles a Selena .sel source whose target material has
// kind "points". The returned CustomMaterial carries the GLSL vertex/fragment
// programs and the WGSL module, ready to attach to a Points layer via
// Points.Material. An error is returned if the compiled material's surface kind
// is not "points", or if the linked Selena bindings are too old to expose
// surface-kind metadata.
func CompileSelenaPoints(source []byte, opts SelenaMaterialOptions) (CustomMaterial, bindings.Layout, error) {
	result, err := selena.Compile(source, selena.CompileOptions{
		Material: opts.Material,
		Targets:  selenaMaterialTargets(opts),
	})
	if err != nil {
		return CustomMaterial{}, bindings.Layout{}, err
	}
	if err := validateSelenaSurfaceKind(result, "points"); err != nil {
		return CustomMaterial{}, bindings.Layout{}, err
	}
	material, err := selenaCustomMaterial(result, opts)
	if err != nil {
		return CustomMaterial{}, bindings.Layout{}, err
	}
	material.VertexWGSL = selenaPointsWGSLRuntimeFrameLayout(material.VertexWGSL)
	material.FragmentWGSL = selenaPointsWGSLRuntimeFrameLayout(material.FragmentWGSL)
	return material, selenaWebGL2Layout(result.Layout), nil
}

// CompileSelenaParticleRender compiles a Selena .sel source whose target
// material has kind "points" for use as the render-pass override on a
// ComputeParticles system. The returned CustomMaterial carries the authored
// GLSL and WGSL shaders, ready to attach via ComputeParticles.RenderMaterial.
//
// "kind points" covers both Points layers (vertex-buffer path) and
// ComputeParticle render overrides (storage-buffer path); the binding contract
// differs at group(2) only — the gosx browser pipeline routes the shader to
// the correct layout automatically based on the entry type.
func CompileSelenaParticleRender(source []byte, opts SelenaMaterialOptions) (CustomMaterial, bindings.Layout, error) {
	return CompileSelenaPoints(source, opts)
}

// CompileSelenaPost compiles a Selena .sel source whose target material has
// kind "post". The returned CustomMaterial carries WGSL and GLSL shaders
// conforming to the Selena post contract:
//
//	WGSL: fullscreen triangle via @builtin(vertex_index) (3 verts, no vertex
//	  buffers), entries vertexMain/fragmentMain.
//	  @group(0): binding(0) texture_2d<f32> sceneColor, (1) sampler,
//	    (2) texture_depth_2d sceneDepth, (3) sampler,
//	    (4) uniform UserUniforms (present only when params declared).
//
//	GLSL (WebGL2): vertex uses attribute vec2 a_position (fullscreen quad),
//	  v_uv = a_position*0.5+0.5; fragment samples _sceneColor/_sceneDepth
//	  samplers by name.
//
// An error is returned if the compiled material's kind is not "post", or if the
// linked Selena bindings are too old to expose surface-kind metadata.
func CompileSelenaPost(source []byte, opts SelenaMaterialOptions) (CustomMaterial, bindings.Layout, error) {
	result, err := selena.Compile(source, selena.CompileOptions{
		Material: opts.Material,
		Targets:  selenaMaterialTargets(opts),
	})
	if err != nil {
		return CustomMaterial{}, bindings.Layout{}, err
	}
	if err := validateSelenaSurfaceKind(result, "post"); err != nil {
		return CustomMaterial{}, bindings.Layout{}, err
	}
	material, err := selenaCustomMaterial(result, opts)
	if err != nil {
		return CustomMaterial{}, bindings.Layout{}, err
	}
	return material, selenaWebGL2Layout(result.Layout), nil
}

func validateSelenaSurfaceKind(result selena.Result, want string) error {
	got, ok := selenaSurfaceKind(result.Layout)
	if !ok {
		return fmt.Errorf(
			"selena material %q cannot be validated as kind %q because the linked m31labs.dev/selena bindings.Layout does not expose surface kind metadata",
			result.Material.Name, want,
		)
	}
	if got != want {
		return fmt.Errorf("selena material %q has kind %q, expected %q", result.Material.Name, got, want)
	}
	return nil
}

func selenaSurfaceKind(layout any) (string, bool) {
	value := reflect.ValueOf(layout)
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return "", false
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return "", false
	}
	field := value.FieldByName("Kind")
	if !field.IsValid() || field.Kind() != reflect.String {
		return "", false
	}
	kind := strings.TrimSpace(field.String())
	return kind, kind != ""
}

func selenaCustomMaterial(result selena.Result, opts SelenaMaterialOptions) (CustomMaterial, error) {
	wgsl, ok := result.Artifact(selena.TargetWGSL)
	if !ok || strings.TrimSpace(wgsl.Source) == "" {
		return CustomMaterial{}, fmt.Errorf("selena material %q did not emit WGSL", result.Material.Name)
	}
	glsl, ok := result.Artifact(selena.TargetGLES)
	if !ok || strings.TrimSpace(glsl.Vertex) == "" || strings.TrimSpace(glsl.Fragment) == "" {
		return CustomMaterial{}, fmt.Errorf("selena material %q did not emit GLSL vertex/fragment shaders", result.Material.Name)
	}

	material := CustomMaterial{
		StandardMaterial: opts.Standard,
		ShaderBackend:    "selena",
		ShaderLayout:     selenaWebGL2LayoutMap(result.Layout),
		VertexGLSL:       glsl.Vertex,
		FragmentGLSL:     glsl.Fragment,
		VertexWGSL:       wgsl.Source,
		FragmentWGSL:     wgsl.Source,
		Uniforms:         selenaUniforms(result.Layout, opts.Uniforms),
	}
	if kind, ok := selenaSurfaceKind(result.Layout); ok && kind == "post" {
		// GoSX's WebGL post pass binds a_position and draws a four-vertex quad.
		// Selena's GLES wrapper uses a three-vertex triangle and top-left UVs.
		// Keep the GLES fragment, and supply the host's bottom-left quad contract.
		material.VertexGLSL = selenaPostWebGL2Vertex
	}
	if len(opts.Targets) > 0 {
		programs := make(map[string]any, len(result.Artifacts))
		for _, artifact := range result.Artifacts {
			programs[string(artifact.Target)] = engine.ShaderProgram{
				Source: artifact.Source, Vertex: artifact.Vertex, Fragment: artifact.Fragment,
			}
		}
		material.ShaderLayout["programs"] = programs
	}
	if material.Wireframe == nil {
		material.Wireframe = Bool(false)
	}
	return material, nil
}

// MaterialFromSelena adapts precompiled Selena artifacts to Scene3D using the
// same WebGL2, WebGPU, post-processing and native-program contracts as
// CompileSelenaMaterial. Layout and Artifacts are required; the parsed program
// and intermediate representation need not be retained in a shipped bundle.
// This avoids compiling shader source at startup or reimplementing backend
// selection and host binding layout in applications.
func MaterialFromSelena(result selena.Result, opts SelenaMaterialOptions) (CustomMaterial, error) {
	if result.Layout.Material == "" {
		return CustomMaterial{}, fmt.Errorf("Selena artifact layout has no material name")
	}
	if result.Material.Name == "" {
		result.Material.Name = result.Layout.Material
	}
	for _, texture := range result.Layout.Textures {
		ref, _ := opts.Uniforms[texture.Name].(string)
		if !strings.HasPrefix(ref, "gosx:environment:") {
			continue
		}
		want := "cube"
		switch ref {
		case SelenaEnvironmentRadiance, SelenaEnvironmentIrradiance:
		case SelenaEnvironmentBRDFLUT:
			want = "2d"
		default:
			return CustomMaterial{}, fmt.Errorf("unknown Selena environment resource %q", ref)
		}
		if texture.Dimension != want {
			return CustomMaterial{}, fmt.Errorf("Selena texture %s: %s requires %s, got %s", texture.Name, ref, want, texture.Dimension)
		}
	}
	m, err := selenaCustomMaterial(result, opts)
	if err != nil {
		return CustomMaterial{}, err
	}
	// Reserved transforms are supplied per draw and may not have authored
	// defaults. Validate every authored value without publishing placeholders
	// that could shadow the renderer's transforms.
	validation := make(map[string]any, len(m.Uniforms)+3)
	for name, value := range m.Uniforms {
		validation[name] = value
	}
	validation["mvp"] = []float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
	validation["modelMatrix"] = validation["mvp"]
	validation["normalMatrix"] = []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	for _, field := range result.Layout.UniformBlock.Fields {
		if field.Name == "environmentInfo" && field.Class == "context" {
			validation[field.Name] = []float64{0, 0, 0, 0}
		}
	}
	if _, err := bindings.PackUniformsWithDefaults(result.Layout, validation); err != nil {
		return CustomMaterial{}, fmt.Errorf("Selena material %s: %w", result.Layout.Material, err)
	}
	m.VertexWGSL = selenaPointsWGSLRuntimeFrameLayout(m.VertexWGSL)
	m.FragmentWGSL = selenaPointsWGSLRuntimeFrameLayout(m.FragmentWGSL)
	return m, nil
}

func selenaPointsWGSLRuntimeFrameLayout(source string) string {
	const runtimeFrame = `struct FrameUniforms {
  viewMatrix     : mat4x4<f32>,
  projMatrix     : mat4x4<f32>,
  cameraPos      : vec3<f32>,
  lightCount     : u32,
  viewportWidth  : f32,
  viewportHeight : f32,
  toneMap        : u32,
  _pad0          : u32,
};
`
	const prefix = "struct FrameUniforms {"
	start := strings.Index(source, prefix)
	if start < 0 {
		return source
	}
	afterStart := source[start+len(prefix):]
	endRel := strings.Index(afterStart, "};")
	if endRel < 0 {
		return source
	}
	end := start + len(prefix) + endRel + len("};")
	if end < len(source) && source[end] == '\n' {
		end++
	}
	return source[:start] + runtimeFrame + source[end:]
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

func selenaUniformFieldName(field reflect.StructField) string {
	if tag, ok := field.Tag.Lookup("selena"); ok {
		name := strings.TrimSpace(strings.Split(tag, ",")[0])
		if name == "-" {
			return ""
		}
		if name != "" {
			return name
		}
	}
	if tag, ok := field.Tag.Lookup("json"); ok {
		name := strings.TrimSpace(strings.Split(tag, ",")[0])
		if name == "-" {
			return ""
		}
		if name != "" {
			return name
		}
	}
	return lowerFirstASCII(field.Name)
}

func selenaUniformReflectValue(value reflect.Value) (any, error) {
	if !value.IsValid() {
		return nil, nil
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil, nil
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return value.Interface(), nil
	case reflect.Slice, reflect.Array:
		return selenaUniformSequence(value)
	default:
		return nil, fmt.Errorf("unsupported uniform value kind %s", value.Kind())
	}
}

func selenaUniformSequence(value reflect.Value) (any, error) {
	if value.Kind() == reflect.Slice && value.IsNil() {
		return nil, nil
	}
	elem := value.Type().Elem()
	switch elem.Kind() {
	case reflect.Float32:
		out := make([]float32, value.Len())
		for i := range out {
			out[i] = float32(value.Index(i).Convert(elem).Float())
		}
		return out, nil
	case reflect.Float64:
		out := make([]float64, value.Len())
		for i := range out {
			out[i] = value.Index(i).Convert(elem).Float()
		}
		return out, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		out := make([]int, value.Len())
		for i := range out {
			out[i] = int(value.Index(i).Convert(elem).Int())
		}
		return out, nil
	case reflect.String:
		out := make([]string, value.Len())
		for i := range out {
			out[i] = value.Index(i).Convert(elem).String()
		}
		return out, nil
	default:
		out := make([]any, value.Len())
		for i := range out {
			item, err := selenaUniformReflectValue(value.Index(i))
			if err != nil {
				return nil, err
			}
			out[i] = item
		}
		return out, nil
	}
}

func lowerFirstASCII(value string) string {
	if value == "" {
		return ""
	}
	b := []byte(value)
	if b[0] >= 'A' && b[0] <= 'Z' {
		b[0] += 'a' - 'A'
	}
	return string(b)
}

// SelenaTargetRequirements reads a target's host contract from a transported
// shaderLayout descriptor. A descriptor without targetRequires has no extra
// host requirements. It accepts both authored Go values and JSON-decoded maps.
func SelenaTargetRequirements(layout map[string]any, target selena.Target) (bindings.Requirements, error) {
	switch target {
	case selena.TargetWGSL, selena.TargetGLSL, selena.TargetMetal, selena.TargetGLES:
	default:
		return bindings.Requirements{}, fmt.Errorf("unknown Selena target %q", target)
	}
	value, exists := layout["targetRequires"]
	if !exists {
		return bindings.Requirements{}, nil
	}
	targets, ok := value.(map[string]any)
	if !ok {
		return bindings.Requirements{}, fmt.Errorf("invalid Selena target requirements")
	}
	requirements, exists := targets[string(target)]
	if !exists {
		return bindings.Requirements{}, fmt.Errorf("missing Selena target %q requirements", target)
	}
	data, err := json.Marshal(requirements)
	if err != nil {
		return bindings.Requirements{}, err
	}
	var out bindings.Requirements
	if err := json.Unmarshal(data, &out); err != nil {
		return bindings.Requirements{}, err
	}
	return out, nil
}

func selenaTargetRequirements(layout bindings.Layout, target selena.Target) bindings.Requirements {
	requires := layout.Requires
	if target != selena.TargetGLSL {
		requires.GLExtensions = nil
		requires.GLSceneSizeUniform = ""
	} else {
		requires.GLExtensions = append([]string(nil), requires.GLExtensions...)
	}
	return requires
}

func selenaWebGL2Layout(layout bindings.Layout) bindings.Layout {
	layout.Requires = selenaTargetRequirements(layout, selena.TargetGLES)
	return layout
}

func selenaWebGL2LayoutMap(layout bindings.Layout) map[string]any {
	out := selenaLayoutMap(selenaWebGL2Layout(layout))
	if layout.Requires.IsZero() {
		return out
	}
	out["webglTarget"] = string(selena.TargetGLES)
	requires := make(map[string]any)
	for _, target := range selena.AllTargets() {
		requires[string(target)] = selenaTargetRequirements(layout, target)
	}
	out["targetRequires"] = requires
	return out
}

const selenaPostWebGL2Vertex = `#version 300 es
precision highp float;
layout(location = 0) in vec2 a_position;
out vec2 v_uv;
void main() {
  v_uv = a_position * 0.5 + 0.5;
  gl_Position = vec4(a_position, 0.0, 1.0);
}
`

func selenaMaterialTargets(opts SelenaMaterialOptions) []selena.Target {
	targets := []selena.Target{selena.TargetWGSL, selena.TargetGLES}
	for _, target := range opts.Targets {
		found := false
		for _, existing := range targets {
			found = found || existing == target
		}
		if !found {
			targets = append(targets, target)
		}
	}
	return targets
}
