//go:build !js || !wasm

package scene

// Tests that compile Selena source at run time. The compiler is linked only in
// native builds (and js/wasm builds with -tags gosx_selena_compiler), so these
// tests run natively; the wasm test run covers the artifact path instead.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSelenaSpecularAALibraryCompiles(t *testing.T) {
	source := SelenaSpecularAASource + `
material RoughnessProbe {
 surface(geo) -> color {
  let r = gsxSpecularRoughness(normalize(geo.worldNormal), 0.22, 0.15, 0.2)
  return vec3f(r,r,r)
 }
}`
	material, _, err := CompileSelenaMaterial([]byte(source), SelenaMaterialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct{ body, derivative string }{{material.FragmentWGSL, "dpdx("}, {material.FragmentGLSL, "dFdx("}} {
		if !strings.Contains(pair.body, pair.derivative) || !strings.Contains(pair.body, "sqrt(sqrt(") {
			t.Fatal("authored AA lost geometric derivatives or perceptual roughness mapping")
		}
	}
	// Selena supplies a shared WGSL module to both entry points; helper functions
	// are present in the module but invoked only from fragmentMain.
}

func TestShaderSurfaceBothBackendsAndShapes(t *testing.T) {
	material, _, err := CompileSelenaMaterial([]byte(selenaDefaultsSource), SelenaMaterialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, shape := range []ShaderShape{ShaderPlane, ShaderSphere, ShaderBox, ShaderTorus} {
		props, err := (ShaderSurface{Material: material, Shape: shape, Interactive: true}).Scene()
		if err != nil {
			t.Fatal(err)
		}
		cfg := props.EngineConfig()
		var wire map[string]any
		if err := json.Unmarshal(cfg.Props, &wire); err != nil {
			t.Fatal(err)
		}
		if props.MaxPixels != 2000000 || props.MaxFrameRate != 30 || props.MaxDevicePixelRatio != 1.5 {
			t.Fatalf("unbounded surface: %+v", props)
		}
		if (props.OrthographicCamera != nil) != (shape == ShaderPlane) || *props.DragToRotate != (shape != ShaderPlane) {
			t.Fatalf("camera/control policy for %s", shape)
		}
		mesh := props.Graph.Nodes[0].(Mesh)
		custom := mesh.Material.(CustomMaterial)
		if custom.VertexWGSL == "" || custom.FragmentWGSL == "" || custom.VertexGLSL == "" || custom.FragmentGLSL == "" {
			t.Fatalf("surface lost compiled browser programs for %s", shape)
		}
	}
}

func TestSelenaSpecularVariantsPreserveCoverageAndHoist(t *testing.T) {
	m, _, err := CompileSelenaMaterial([]byte(`material Plain { surface(g)->vec4 { return vec4f(9.0,8.0,7.0,0.2) } }`), SelenaMaterialOptions{SpecularMRT: true})
	if err != nil {
		t.Fatal(err)
	}
	if m.SpecularFragmentGLSL == "" || m.SpecularFragmentWGSL == "" {
		t.Fatal("missing alternate fragment artifacts")
	}
	if !strings.Contains(m.SpecularFragmentWGSL, "fragmentMainSpecular") {
		t.Fatal("missing MRT entrypoint")
	}
	if strings.Contains(m.FragmentGLSL, "layout(location = 1)") {
		t.Fatal("ordinary source changed")
	}
	props := m.legacyMaterial()
	if props["specularFragmentGLSL"] != strings.TrimSpace(m.SpecularFragmentGLSL) || props["specularFragmentWGSL"] != strings.TrimSpace(m.SpecularFragmentWGSL) {
		t.Fatal("legacy transport lost variants")
	}
	source := strings.Repeat(m.SpecularFragmentWGSL, 10)
	ir := SceneIR{Objects: []ObjectIR{{ID: "a", SpecularFragmentWGSL: source}, {ID: "b", SpecularFragmentWGSL: source}}}
	raw, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "fragmentMainSpecular") != 10 {
		t.Fatal("repeated specular shader was not hoisted")
	}
	var restored SceneIR
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Objects[0].SpecularFragmentWGSL != source || restored.Objects[1].SpecularFragmentWGSL != source {
		t.Fatal("hoisted variants did not inflate")
	}
}

func TestSelenaPrimitiveRemainsSolidAfterFallbackReplacement(t *testing.T) {
	compiled, _, err := CompileSelenaMaterial([]byte(`material Paint { surface(g)->color { return rgb(0.5,0.6,0.7) } }`), SelenaMaterialOptions{SpecularMRT: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		wire *bool
		want bool
	}{{"default", nil, false}, {"solid", Bool(false), false}, {"wireframe", Bool(true), true}} {
		t.Run(tc.name, func(t *testing.T) {
			material := compiled
			// Replacing fallback properties must not turn an untextured
			// compiled surface into the browser's legacy wire primitive.
			material.StandardMaterial = StandardMaterial{Color: "#abcdef", Wireframe: tc.wire}
			props := Props{Graph: NewGraph(Mesh{ID: "painted-edge", Geometry: BoxGeometry{Width: 2, Height: .1, Depth: .1}, Material: material})}
			ir := props.SceneIR()
			if len(ir.Objects) != 1 {
				t.Fatalf("objects = %d", len(ir.Objects))
			}
			object := ir.Objects[0]
			if object.Kind != "box" || object.Wireframe == nil || *object.Wireframe != tc.want {
				t.Fatalf("primitive kind=%s wireframe=%v, want box/%v", object.Kind, object.Wireframe, tc.want)
			}
			if object.SpecularFragmentGLSL == "" {
				t.Fatal("compiled coverage-preserving MRT fragment lost")
			}
			legacy := props.LegacyProps()["scene"].(map[string]any)["objects"].([]map[string]any)[0]
			if legacy["wireframe"] != tc.want || material.legacyMaterial()["wireframe"] != tc.want {
				t.Fatalf("legacy solid/explicit-wire contract diverged: %#v", legacy["wireframe"])
			}
		})
	}
}

// Exercise the authored material compiler, its cache and canonical wire path.
// A hand-built layout cannot prove that the pinned compiler emits the contract.
func TestCompiledSelenaVertexTextureLayoutReachesSceneIR(t *testing.T) {
	source, err := os.ReadFile("testdata/selena_vertex_texture.sel")
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"compile", "cache-hit"} {
		t.Run(phase, func(t *testing.T) {
			material, _, err := CompileSelenaMaterial(source, SelenaMaterialOptions{
				Uniforms: map[string]any{"heightMap": "/height.png", "tint": "/tint.png"},
			})
			if err != nil {
				t.Fatalf("authored vertex-texture material: %v", err)
			}
			lowered := (Props{Graph: NewGraph(Mesh{
				ID: "displaced", Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1}, Material: material,
			})}).CanonicalIR()
			encoded, err := json.Marshal(lowered)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Materials []struct {
					ShaderLayout struct {
						Textures []struct {
							Name        string `json:"name"`
							VertexStage bool   `json:"vertexStage"`
						} `json:"textures"`
						Requires struct {
							Units int `json:"glVertexTextureUnits"`
						} `json:"requires"`
					} `json:"shaderLayout"`
				} `json:"materials"`
			}
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			if len(wire.Materials) != 1 {
				t.Fatalf("materials=%d, want1", len(wire.Materials))
			}
			layout := wire.Materials[0].ShaderLayout
			if len(layout.Textures) != 2 || layout.Textures[0].Name != "heightMap" || !layout.Textures[0].VertexStage || layout.Textures[1].Name != "tint" || layout.Textures[1].VertexStage || layout.Requires.Units != 1 {
				t.Fatalf("compiled descriptor lost vertex-stage contract: %+v", layout)
			}
		})
	}
}
