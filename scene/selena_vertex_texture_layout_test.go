package scene

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// A Selena descriptor marks vertex-sampled textures ("vertexStage") and the
// WebGL 1 vertex texture unit need ("requires.glVertexTextureUnits"). The
// runtime reads both from the shaderLayout map, so the map must reach the wire
// IR unchanged, including nested slices and booleans.
func TestShaderLayoutKeepsVertexTextureDescriptorKeys(t *testing.T) {
	var layout map[string]any
	if err := json.Unmarshal([]byte(`{
		"material": "HeightDisplace",
		"textures": [
			{"name": "heightMap", "dimension": "2d", "vertexStage": true, "wgsl": {"group": 0, "textureBinding": 1, "samplerBinding": 2}},
			{"name": "tint", "dimension": "2d", "wgsl": {"group": 0, "textureBinding": 3, "samplerBinding": 4}}
		],
		"requires": {"glVertexTextureUnits": 1}
	}`), &layout); err != nil {
		t.Fatal(err)
	}
	material := CustomMaterial{
		ShaderBackend: "selena",
		ShaderLayout:  layout,
		VertexWGSL:    "@vertex fn vertexMain() {}",
		FragmentWGSL:  "@fragment fn fragmentMain() {}",
		VertexGLSL:    "void main() {}",
		FragmentGLSL:  "void main() {}",
	}
	ir := (Props{Graph: NewGraph(
		Mesh{ID: "mesh", Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1}, Material: material},
	)}).CanonicalIR()
	if len(ir.Materials) != 1 {
		t.Fatalf("materials = %d, want 1", len(ir.Materials))
	}
	encoded, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Materials []struct {
			ShaderLayout map[string]any `json:"shaderLayout"`
		} `json:"materials"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Materials) != 1 || !reflect.DeepEqual(wire.Materials[0].ShaderLayout, layout) {
		t.Fatalf("shaderLayout changed on the wire:\n got %#v\nwant %#v", wire.Materials, layout)
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
