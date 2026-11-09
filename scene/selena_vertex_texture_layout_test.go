package scene

import (
	"encoding/json"
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
