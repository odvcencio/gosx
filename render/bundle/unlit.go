package bundle

// unlitWGSL is the R1 unlit shader. It takes a position and a per-vertex
// color, transforms position by the MVP matrix, and emits the color as-is.
// No lighting, no textures, no shadows. This is deliberately the simplest
// thing that's a valid pipeline so R1 can prove end-to-end draw dispatch
// before R2 layers on PBR.
const unlitWGSL = `
struct Uniforms {
  mvp : mat4x4<f32>,
};

@group(0) @binding(0) var<uniform> u : Uniforms;

struct VSOut {
  @builtin(position) pos : vec4<f32>,
  @location(0) color : vec3<f32>,
};

@vertex
fn vs_main(
  @location(0) pos : vec3<f32>,
  @location(1) color : vec3<f32>,
) -> VSOut {
  var out : VSOut;
  out.pos = u.mvp * vec4<f32>(pos, 1.0);
  out.color = color;
  return out;
}

@fragment
fn fs_main(in : VSOut) -> @location(0) vec4<f32> {
  return vec4<f32>(in.color, 1.0);
}
`
