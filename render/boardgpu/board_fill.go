package boardgpu

import _ "embed"

// BoardFillSelenaSource is the Selena (.sel) source for the canvas board's rect
// fill material. It stays embedded so host-side tests can compile it and prove
// the static browser-runtime shader payload below has not drifted.
//
//go:embed board_fill.sel
var BoardFillSelenaSource string

type boardFillMaterial struct {
	VertexWGSL    string
	FragmentWGSL  string
	ShaderBackend string
	ShaderLayout  map[string]any
}

func boardFillCompiled() (boardFillMaterial, error) {
	return boardFillStaticMaterial, nil
}

var boardFillStaticMaterial = boardFillMaterial{
	VertexWGSL:    boardFillWGSL,
	FragmentWGSL:  boardFillWGSL,
	ShaderBackend: "selena",
	ShaderLayout:  boardFillShaderLayout,
}

const boardFillWGSL = `struct Uniforms {
  mvp : mat4x4<f32>,
  normalMatrix : mat3x3<f32>,
  baseColor : vec3<f32>,
};
@group(0) @binding(0) var<uniform> u : Uniforms;

struct VertexInput {
  @location(0) position : vec3<f32>,
};

struct VertexOutput {
  @builtin(position) position : vec4<f32>,
};

@vertex
fn vertexMain(in : VertexInput) -> VertexOutput {
  var out : VertexOutput;
  out.position = (u.mvp * vec4<f32>(in.position, 1.0));
  return out;
}

@fragment
fn fragmentMain(in : VertexOutput) -> @location(0) vec4<f32> {
  return vec4<f32>(u.baseColor, 1.0);
}
`

var boardFillShaderLayout = map[string]any{
	"schemaVersion":   "selena.descriptor.v1",
	"languageVersion": "selena.lang.v1",
	"material":        "BoardFill",
	"kind":            "mesh",
	"entryPoints": map[string]any{
		"vertex":   "vertexMain",
		"fragment": "fragmentMain",
	},
	"attributes": []any{
		map[string]any{
			"name":     "position",
			"type":     "vec3",
			"location": float64(0),
		},
	},
	"uniformBlock": map[string]any{
		"size": float64(128),
		"fields": []any{
			map[string]any{
				"name":   "mvp",
				"type":   "mat4",
				"offset": float64(0),
				"size":   float64(64),
			},
			map[string]any{
				"name":   "normalMatrix",
				"type":   "mat3",
				"offset": float64(64),
				"size":   float64(48),
			},
			map[string]any{
				"name":   "baseColor",
				"type":   "vec3",
				"offset": float64(112),
				"size":   float64(12),
			},
		},
		"defaults": []any{
			map[string]any{
				"name":   "baseColor",
				"type":   "vec3",
				"values": []any{float64(0.13), float64(0.14), float64(0.18)},
			},
		},
	},
	"textures": []any{},
	"wgsl": map[string]any{
		"group":   float64(0),
		"binding": float64(0),
	},
	"metal": map[string]any{
		"buffer": float64(0),
	},
}
