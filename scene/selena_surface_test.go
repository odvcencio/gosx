//go:build !js || !wasm

package scene

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/selena"
	"m31labs.dev/selena/artifact/builder"
	"m31labs.dev/selena/bindings"
)

func surfaceRockSource(t *testing.T) []byte {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("testdata", "selena", "surface-rock.sel"))
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestSelenaSurfaceMaterialCarriesHooks(t *testing.T) {
	material, layout, err := CompileSelenaMaterial(surfaceRockSource(t), SelenaMaterialOptions{
		Standard: StandardMaterial{BlendMode: BlendOpaque},
		Uniforms: map[string]any{"albedo": SelenaTexture{URL: " /rock.png ", ColorSpace: TextureColorSpaceSRGB}, "roughness": 0.6},
	})
	if err != nil {
		t.Fatal(err)
	}
	if layout.Kind != bindings.SurfaceKindSurface || layout.Surface == nil || layout.Surface.ABI != bindings.SurfaceABI {
		t.Fatalf("layout kind %q hook %+v", layout.Kind, layout.Surface)
	}
	if material.VertexGLSL != "" || material.VertexWGSL != "" {
		t.Fatal("a surface hook must not carry a vertex stage")
	}
	if !strings.Contains(material.FragmentGLSL, "SelenaSurface selenaSurface(SelenaSurfaceGeo selenaGeo)") ||
		!strings.Contains(material.FragmentWGSL, "fn selenaSurface(selenaGeo : SelenaSurfaceGeo) -> SelenaSurface") {
		t.Fatalf("hooks missing:\n%s\n%s", material.FragmentGLSL, material.FragmentWGSL)
	}
	if material.ShaderBackend != "selena" || material.ShaderLayout["kind"] != "surface" {
		t.Fatalf("backend %q layout kind %v", material.ShaderBackend, material.ShaderLayout["kind"])
	}
	textures, _ := material.ShaderLayout["surfaceTextures"].([]map[string]any)
	if len(textures) != 1 || textures[0]["name"] != "albedo" || textures[0]["uri"] != "/rock.png" || textures[0]["colorSpace"] != "srgb" {
		t.Fatalf("surface textures = %#v", material.ShaderLayout["surfaceTextures"])
	}
	if material.Uniforms["albedo"] != "/rock.png" || material.Uniforms["roughness"] != 0.6 {
		t.Fatalf("uniforms = %#v", material.Uniforms)
	}
	if _, ok := material.Uniforms["tint"]; !ok {
		t.Fatal("param default tint missing from uniforms")
	}
}

func TestSelenaSurfaceMaterialTextureBindings(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  string
	}{
		{"/a.png", "linear"},
		{SelenaTexture{URL: "/a.png"}, "linear"},
		{&SelenaTexture{URL: "/a.png", ColorSpace: TextureColorSpaceSRGB}, "srgb"},
	} {
		material, _, err := CompileSelenaMaterial(surfaceRockSource(t), SelenaMaterialOptions{Uniforms: map[string]any{"albedo": tc.value}})
		if err != nil {
			t.Fatal(err)
		}
		textures := material.ShaderLayout["surfaceTextures"].([]map[string]any)
		if textures[0]["colorSpace"] != tc.want {
			t.Fatalf("%#v: color space %v, want %s", tc.value, textures[0]["colorSpace"], tc.want)
		}
	}
	for _, tc := range []struct {
		opts SelenaMaterialOptions
		want string
	}{
		{SelenaMaterialOptions{Uniforms: map[string]any{"albedo": SelenaTexture{URL: "/a.png", ColorSpace: "rec2020"}}}, "unknown color space"},
		{SelenaMaterialOptions{Uniforms: map[string]any{"albedo": 3}}, "want a URL string"},
		{SelenaMaterialOptions{Standard: StandardMaterial{NormalMap: "/n.png"}}, "Standard.NormalMap is not sampled"},
	} {
		if _, _, err := CompileSelenaMaterial(surfaceRockSource(t), tc.opts); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("err = %v, want %q", err, tc.want)
		}
	}
}

// The artifact path gives the same surface material as run-time compilation.
func TestSelenaSurfaceArtifactMatchesCompiler(t *testing.T) {
	source := surfaceRockSource(t)
	opts := SelenaMaterialOptions{Uniforms: map[string]any{"albedo": SelenaTexture{URL: "/rock.png", ColorSpace: TextureColorSpaceSRGB}}}
	compiled, compiledLayout, err := CompileSelenaMaterial(source, opts)
	if err != nil {
		t.Fatal(err)
	}
	file, err := builder.Build(source, builder.Options{Path: "surface-rock.sel", Targets: []selena.Target{selena.TargetWGSL, selena.TargetGLES}})
	if err != nil {
		t.Fatal(err)
	}
	loaded, loadedLayout, err := MaterialFromSelenaArtifact(file, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, compiled) || !reflect.DeepEqual(loadedLayout, compiledLayout) {
		t.Fatal("the surface artifact differs from run-time compilation")
	}
}

// A surface material lowers to the scene IR as a custom Selena material whose
// only shaders are the two fragment hooks.
func TestSelenaSurfaceMaterialLowersToIR(t *testing.T) {
	material, _, err := CompileSelenaMaterial(surfaceRockSource(t), SelenaMaterialOptions{Uniforms: map[string]any{"albedo": "/rock.png"}})
	if err != nil {
		t.Fatal(err)
	}
	ir := Props{Graph: NewGraph(Mesh{ID: "rock", Geometry: SphereGeometry{Segments: 8}, Material: material})}.SceneIR()
	if len(ir.Objects) != 1 {
		t.Fatalf("objects = %d", len(ir.Objects))
	}
	obj := ir.Objects[0]
	if obj.ShaderBackend != "selena" || obj.CustomVertex != "" || obj.CustomVertexWGSL != "" ||
		!strings.Contains(obj.CustomFragment, "selenaSurface(") || !strings.Contains(obj.CustomFragmentWGSL, "selenaSurface(") {
		t.Fatalf("object material = backend %q vertex %q/%q", obj.ShaderBackend, obj.CustomVertex, obj.CustomVertexWGSL)
	}
	if obj.ShaderLayout["kind"] != "surface" {
		t.Fatalf("layout kind = %v", obj.ShaderLayout["kind"])
	}
}

// The renderer tests in client/js/scene3d-selena-surface.test.js splice this
// fixture into both PBR fragments; it must match the linked compiler.
// Regenerate with GOSX_UPDATE_SELENA_SURFACE_FIXTURE=1 go test -run TestSelenaSurfaceRendererFixture ./scene/
func TestSelenaSurfaceRendererFixture(t *testing.T) {
	material, _, err := CompileSelenaMaterial(surfaceRockSource(t), SelenaMaterialOptions{Uniforms: map[string]any{
		"albedo": SelenaTexture{URL: "/rock.png", ColorSpace: TextureColorSpaceSRGB},
	}})
	if err != nil {
		t.Fatal(err)
	}
	layout, err := json.MarshalIndent(material.ShaderLayout, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join("..", "client", "js", "testdata", "selena-surface")
	want := map[string][]byte{
		"rock.wgsl":        []byte(material.FragmentWGSL),
		"rock.glsl":        []byte(material.FragmentGLSL),
		"rock.layout.json": append(layout, '\n'),
	}
	if os.Getenv("GOSX_UPDATE_SELENA_SURFACE_FIXTURE") == "1" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range want {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, data := range want {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, data) {
			t.Fatalf("%s is stale; regenerate it with GOSX_UPDATE_SELENA_SURFACE_FIXTURE=1", name)
		}
	}
}

// Every surface example in docs/scene3d-selena.md compiles with the surface
// module linked, and the sculpted rock fits the six texture slots.
func TestSelenaSurfaceDocExamplesCompile(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "docs", "scene3d-selena.md"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := strings.Split(string(doc), "```selena\n")
	compiled := 0
	for _, block := range blocks[1:] {
		source := block[:strings.Index(block, "```")]
		if !strings.Contains(source, "-> Surface") {
			continue
		}
		_, layout, err := CompileSelenaMaterial([]byte(source), SelenaMaterialOptions{Modules: []string{"surface"}})
		if err != nil {
			t.Fatalf("doc example does not compile: %v\n%s", err, source)
		}
		if strings.Contains(source, "SculptedRock") && len(layout.Textures) != bindings.SurfaceMaxTextures {
			t.Fatalf("sculpted rock binds %d textures, want %d", len(layout.Textures), bindings.SurfaceMaxTextures)
		}
		compiled++
	}
	if compiled < 2 {
		t.Fatalf("found %d surface examples in the doc, want at least 2", compiled)
	}
}
