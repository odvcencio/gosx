package boardgpu

import _ "embed"

// BoardTextSelenaSource is the Selena (.sel) source for the canvas board's label
// glyph material. Like BoardFill it stays embedded so host-side tests can compile
// it and prove the static browser-runtime shader payload below has not drifted.
//
//go:embed board_text.sel
var BoardTextSelenaSource string

type boardTextMaterial struct {
	VertexWGSL    string
	FragmentWGSL  string
	ShaderBackend string
	ShaderLayout  map[string]any
}

func boardTextCompiled() (boardTextMaterial, error) {
	return boardTextStaticMaterial, nil
}

var boardTextStaticMaterial = boardTextMaterial{
	VertexWGSL:    boardTextWGSL,
	FragmentWGSL:  boardTextWGSL,
	ShaderBackend: "selena",
	ShaderLayout:  boardTextShaderLayout,
}

// boardTextWGSL is the Selena-emitted WGSL for the BoardText material: a textured
// quad whose fragment samples the glyph-atlas coverage (alpha) and returns the
// per-label textColor with that coverage as alpha for standard alpha blending.
// Vertex inputs are position (location 0) + uv (location 1); the atlas texture and
// its sampler bind at group(0) binding 1/2, matching the Selena texture layout.
const boardTextWGSL = `struct Uniforms {
  mvp : mat4x4<f32>,
  normalMatrix : mat3x3<f32>,
  textColor : vec3<f32>,
};
@group(0) @binding(0) var<uniform> u : Uniforms;

@group(0) @binding(1) var atlas : texture_2d<f32>;
@group(0) @binding(2) var atlasSampler : sampler;

struct VertexInput {
  @location(0) position : vec3<f32>,
  @location(1) uv : vec2<f32>,
};

struct VertexOutput {
  @builtin(position) position : vec4<f32>,
  @location(0) vUv : vec2<f32>,
};

@vertex
fn vertexMain(in : VertexInput) -> VertexOutput {
  var out : VertexOutput;
  out.vUv = in.uv;
  out.position = (u.mvp * vec4<f32>(in.position, 1.0));
  return out;
}

@fragment
fn fragmentMain(in : VertexOutput) -> @location(0) vec4<f32> {
  let coverage = textureSample(atlas, atlasSampler, in.vUv).a;
  return vec4<f32>(u.textColor.r, u.textColor.g, u.textColor.b, coverage);
}
`

var boardTextShaderLayout = map[string]any{
	"schemaVersion":   "selena.descriptor.v1",
	"languageVersion": "selena.lang.v1",
	"material":        "BoardText",
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
		map[string]any{
			"name":     "uv",
			"type":     "vec2",
			"location": float64(1),
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
				"name":   "textColor",
				"type":   "vec3",
				"offset": float64(112),
				"size":   float64(12),
			},
		},
		"defaults": []any{
			map[string]any{
				"name":   "textColor",
				"type":   "vec3",
				"values": []any{float64(0.902), float64(0.929), float64(0.953)},
			},
		},
	},
	"textures": []any{
		map[string]any{
			"name":      "atlas",
			"dimension": "2d",
			"wgsl": map[string]any{
				"group":          float64(0),
				"textureBinding": float64(1),
				"samplerBinding": float64(2),
			},
			"gl": map[string]any{
				"uniform": "atlas",
				"unit":    float64(0),
			},
			"metal": map[string]any{
				"texture": float64(0),
				"sampler": float64(0),
			},
		},
	},
	"wgsl": map[string]any{
		"group":   float64(0),
		"binding": float64(0),
	},
	"metal": map[string]any{
		"buffer": float64(0),
	},
}
