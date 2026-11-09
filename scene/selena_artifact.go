package scene

import (
	"fmt"
	"strings"

	"m31labs.dev/selena/artifact"
	"m31labs.dev/selena/bindings"
	"m31labs.dev/selena/result"
)

// SelenaArtifact is a decoded Selena artifact file: the binding layout and the
// emitted shaders for every material in one .sel source, plus the source hash
// and the Selena version that wrote it. `gosx build` and `selena artifact`
// write these files; DecodeSelenaArtifact reads one.
//
// Loading an artifact links no Selena compiler, so it is the way browser
// (js/wasm) code gets Selena materials. Native code may use the artifact path
// too, or compile at run time with CompileSelenaMaterial.
type SelenaArtifact = artifact.File

// DecodeSelenaArtifact parses and validates a Selena artifact file, typically
// one embedded with //go:embed next to its .sel source.
func DecodeSelenaArtifact(data []byte) (SelenaArtifact, error) {
	return artifact.Decode(data)
}

// MaterialFromSelenaArtifact builds the Scene3D material for one material in a
// decoded artifact file. opts.Material selects it by name; empty selects the
// file's default, the last material in the source, as CompileSelenaMaterial
// does. The result equals what CompileSelenaMaterial returns for the same
// source and options.
//
// The file must carry the WGSL and GLES targets the browser renderers use.
// opts.SpecularMRT requires a file built with the specular variant. opts.Targets
// is not needed: the material transports every target the file carries when
// opts.Targets is non-empty, as with CompileSelenaMaterial.
func MaterialFromSelenaArtifact(file SelenaArtifact, opts SelenaMaterialOptions) (CustomMaterial, bindings.Layout, error) {
	res, err := file.Result(opts.Material)
	if err != nil {
		return CustomMaterial{}, bindings.Layout{}, err
	}
	if err := checkSelenaArtifactOptions(file, res.Layout, opts); err != nil {
		return CustomMaterial{}, bindings.Layout{}, err
	}
	if !opts.SpecularMRT {
		res = withoutSelenaSpecularMRT(res)
	}
	material, err := MaterialFromSelena(res, opts)
	if err != nil {
		return CustomMaterial{}, bindings.Layout{}, err
	}
	return material, selenaWebGL2Layout(res.Layout), nil
}

// SelenaBundleFromArtifact is CompileSelenaBundle for a decoded artifact file:
// it returns one material per option set, in order. Passing no options returns
// the file's default material.
func SelenaBundleFromArtifact(file SelenaArtifact, materials ...SelenaMaterialOptions) ([]SelenaCompiledMaterial, error) {
	if len(materials) == 0 {
		materials = []SelenaMaterialOptions{{}}
	}
	out := make([]SelenaCompiledMaterial, 0, len(materials))
	for _, opts := range materials {
		material, layout, err := MaterialFromSelenaArtifact(file, opts)
		if err != nil {
			label := opts.Material
			if strings.TrimSpace(label) == "" {
				label = "<default>"
			}
			return nil, fmt.Errorf("selena material %s: %w", label, err)
		}
		out = append(out, SelenaCompiledMaterial{Name: layout.Material, Material: material, Layout: layout})
	}
	return out, nil
}

func checkSelenaArtifactOptions(file SelenaArtifact, layout bindings.Layout, opts SelenaMaterialOptions) error {
	if opts.SpecularMRT && !file.Options.SpecularMRT && (layout.Kind == "" || layout.Kind == bindings.SurfaceKindMesh) {
		return fmt.Errorf("selena artifact %s: material %s was built without the specular variant; rebuild with SpecularMRT", file.Source.Path, layout.Material)
	}
	have := make(map[string]bool, len(file.Options.Targets))
	for _, t := range file.Options.Targets {
		have[string(t)] = true
	}
	for _, t := range selenaMaterialTargets(opts) {
		if !have[string(t)] {
			return fmt.Errorf("selena artifact %s: material %s has no %s shader (file targets %v)", file.Source.Path, layout.Material, t, file.Options.Targets)
		}
	}
	return nil
}

// withoutSelenaSpecularMRT removes what SpecularMRT adds to a compile: the
// specular entry point and output in the layout and the variant shaders. A file
// built with the variant then loads exactly as a compile without it.
func withoutSelenaSpecularMRT(res result.Result) result.Result {
	res.Layout.EntryPoints.FragmentSpecular = ""
	res.Layout.SpecularOutput = nil
	artifacts := make([]result.Artifact, len(res.Artifacts))
	for i, a := range res.Artifacts {
		a.SpecularMRT = nil
		artifacts[i] = a
	}
	res.Artifacts = artifacts
	return res
}
