//go:build !js || !wasm

package scene

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"m31labs.dev/selena"
	"m31labs.dev/selena/artifact/builder"
	"m31labs.dev/selena/bindings"
	"m31labs.dev/selena/materiallib"
)

func TestMaterialFromSelenaMatchesCompiler(t *testing.T) {
	for _, source := range []string{selenaDefaultsSource, `material ColorPass kind post { surface(pixel) -> color { return rgb(0.2, 0.4, 0.6) } }`} {
		opts := SelenaMaterialOptions{Standard: StandardMaterial{Color: "#abcabc"}, Uniforms: map[string]any{"gain": float32(2)}, Targets: selena.AllTargets()}
		result, err := selena.Compile([]byte(source), selena.CompileOptions{Targets: selena.AllTargets()})
		if err != nil {
			t.Fatal(err)
		}
		compiled, _, err := CompileSelenaMaterial([]byte(source), opts)
		if err != nil {
			t.Fatal(err)
		}
		// A shipped bundle contains no AST or intermediate representation.
		artifacts := selena.Result{Layout: result.Layout, Artifacts: result.Artifacts}
		material, err := MaterialFromSelena(artifacts, opts)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(material, compiled) {
			t.Fatal("precompiled artifact changed host contract")
		}
		if _, err := MaterialFromSelena(selena.Result{Layout: result.Layout}, opts); err == nil {
			t.Fatal("missing browser artifacts accepted")
		}
	}
	if _, err := MaterialFromSelena(selena.Result{}, SelenaMaterialOptions{}); err == nil {
		t.Fatal("missing layout accepted")
	}
}

// An artifact file must give the same Scene3D material as compiling its
// source at run time, for every material and option the browser uses.
func TestMaterialFromSelenaArtifactMatchesCompiler(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "selena", "glow.sel"))
	if err != nil {
		t.Fatal(err)
	}
	file := glowArtifact(t)
	for _, opts := range []SelenaMaterialOptions{
		{},
		{Material: "Glow", SpecularMRT: true},
		{Material: "GlowTinted", Standard: StandardMaterial{Color: "#123456"}, Uniforms: map[string]any{"gain": float32(3)}},
	} {
		compiled, compiledLayout, err := CompileSelenaMaterial(source, opts)
		if err != nil {
			t.Fatal(err)
		}
		loaded, loadedLayout, err := MaterialFromSelenaArtifact(file, opts)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(loaded, compiled) {
			t.Fatalf("%+v: artifact material differs from CompileSelenaMaterial", opts)
		}
		if !reflect.DeepEqual(loadedLayout, compiledLayout) {
			t.Fatalf("%+v: artifact layout differs from CompileSelenaMaterial", opts)
		}
	}
}

// The shipped test artifact must match its source and the linked compiler.
// Regenerate it with:
//
//	selena artifact -targets wgsl,gles -specular-mrt -path glow.sel testdata/selena/glow.sel
func TestSelenaTestArtifactMatchesSource(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "selena", "glow.sel"))
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.Check(glowArtifact(t), source); err != nil {
		t.Fatal(err)
	}
}

// Scene lighting through the Selena lighting module: run-time compilation with
// Modules and the build-time artifact give the same material, and the layout
// asks the renderer for every light, both shadow slots and fog.
func TestSelenaLightingModuleCompilesAndLoads(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "selena", "scene-lit.sel"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := CompileSelenaMaterial(source, SelenaMaterialOptions{}); err == nil {
		t.Fatal("a source calling lighting helpers compiled without the lighting module")
	}
	opts := SelenaMaterialOptions{Modules: []string{"lighting"}}
	compiled, layout, err := CompileSelenaMaterial(source, opts)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]bindings.Field{}
	for _, f := range layout.UniformBlock.Fields {
		fields[f.Name] = f
	}
	if fields["lights"].Count != 32 || fields["lights"].Source != "engine" || fields["fog"].Source != "engine" {
		t.Fatalf("lighting fields = %+v / %+v", fields["lights"], fields["fog"])
	}
	if len(layout.Textures) != 2 || layout.Textures[0].Engine != "shadowMap0" || layout.Textures[1].Dimension != "depth2d-array" {
		t.Fatalf("textures = %+v", layout.Textures)
	}
	file, err := builder.Build(source, builder.Options{Path: "scene-lit.sel", Targets: []selena.Target{selena.TargetWGSL, selena.TargetGLES}, Modules: []materiallib.Module{materiallib.Lighting}})
	if err != nil {
		t.Fatal(err)
	}
	loaded, loadedLayout, err := MaterialFromSelenaArtifact(file, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, compiled) || !reflect.DeepEqual(loadedLayout, layout) {
		t.Fatal("the lighting artifact differs from run-time compilation")
	}
	bundle, err := CompileSelenaBundle(source, opts)
	if err != nil || len(bundle) != 1 || !reflect.DeepEqual(bundle[0].Material, compiled) {
		t.Fatalf("CompileSelenaBundle with modules: %v", err)
	}
}
