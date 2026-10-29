//go:build !js || !wasm

package scene

import (
	"fmt"
	"strings"

	"m31labs.dev/selena"
	"m31labs.dev/selena/bindings"
	"m31labs.dev/selena/parse"
)

// The functions in this file compile .sel source, so they link the Selena
// parser, lowerer and emitters. js/wasm builds get the stubs in
// selena_compile_js.go instead; see ErrSelenaCompileUnavailable.

// CompileSelenaMaterial compiles Selena .sel source into GoSX's native
// CustomMaterial transport. Selena is Scene3D's default shader authoring
// backend: the returned material carries GLSL ES 3.00 for WebGL2 and WGSL for WebGPU,
// plus the binding layout the runtime uses to wire uniforms and textures.
func CompileSelenaMaterial(source []byte, opts SelenaMaterialOptions) (CustomMaterial, bindings.Layout, error) {
	result, err := compileSelenaCached(source, selena.CompileOptions{
		Material:    opts.Material,
		SpecularMRT: opts.SpecularMRT,
		Targets:     selenaMaterialTargets(opts),
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
			Material:    opts.Material,
			SpecularMRT: opts.SpecularMRT,
			Targets:     selenaMaterialTargets(opts),
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
		Material:    opts.Material,
		SpecularMRT: opts.SpecularMRT,
		Targets:     selenaMaterialTargets(opts),
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
		Material:    opts.Material,
		SpecularMRT: opts.SpecularMRT,
		Targets:     selenaMaterialTargets(opts),
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
