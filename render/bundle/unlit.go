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

struct UnlitFSOut {
  @location(0) color  : vec4<f32>,
  @location(1) pickId : u32,
};

@fragment
fn fs_main(in : VSOut) -> UnlitFSOut {
  // Pre-batched pass data doesn't carry per-primitive pick IDs in R4;
  // writing 0 means "background" so the pick lookup skips these surfaces.
  var out : UnlitFSOut;
  out.color  = vec4<f32>(in.color, 1.0);
  out.pickId = 0u;
  return out;
}
`

// instancedWGSL is the R1 instanced-mesh shader. Identical to unlitWGSL
// except that the position is pre-multiplied by a per-instance mat4
// assembled from four vec4 attributes (locations 2..5). Split across four
// attributes because WebGPU doesn't allow mat4 as a vertex attribute
// directly — the layout emits four consecutive float32x4 slots.
const instancedWGSL = `
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
  @location(2) m0 : vec4<f32>,
  @location(3) m1 : vec4<f32>,
  @location(4) m2 : vec4<f32>,
  @location(5) m3 : vec4<f32>,
) -> VSOut {
  let instance : mat4x4<f32> = mat4x4<f32>(m0, m1, m2, m3);
  var out : VSOut;
  out.pos = u.mvp * instance * vec4<f32>(pos, 1.0);
  out.color = color;
  return out;
}

@fragment
fn fs_main(in : VSOut) -> @location(0) vec4<f32> {
  return vec4<f32>(in.color, 1.0);
}
`
