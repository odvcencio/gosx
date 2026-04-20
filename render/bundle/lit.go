package bundle

// litWGSL is the R2 lit + shadowed shader used for RenderInstancedMesh draws.
//
// It implements a pragmatic PBR approximation:
//   - Lambertian diffuse scaled by per-vertex color (acting as baseColor)
//   - Optional directional-light shadow sampling via a comparison sampler
//   - A fixed metal=0 / roughness=0.7 bias until per-material uniforms land
//
// The shadow bias is conservative (0.005) to avoid shadow acne on the
// primitive geometry. Expect follow-up shadowing work to switch to
// receiver-plane depth bias in R3 when CSM arrives.
const litWGSL = `
struct Scene {
  viewProj       : mat4x4<f32>,
  lightViewProj  : mat4x4<f32>,
  cameraPos      : vec4<f32>,
  lightDir       : vec4<f32>,
  lightColor     : vec4<f32>,
  ambientColor   : vec4<f32>,
};

@group(0) @binding(0) var<uniform> scene   : Scene;
@group(0) @binding(1) var          shadowMap : texture_depth_2d;
@group(0) @binding(2) var          shadowSampler : sampler_comparison;

struct VSOut {
  @builtin(position) pos : vec4<f32>,
  @location(0) color    : vec3<f32>,
  @location(1) worldPos : vec3<f32>,
  @location(2) worldNrm : vec3<f32>,
  @location(3) lightUV  : vec4<f32>,
};

@vertex
fn vs_main(
  @location(0) pos    : vec3<f32>,
  @location(1) color  : vec3<f32>,
  @location(2) normal : vec3<f32>,
  @location(3) m0     : vec4<f32>,
  @location(4) m1     : vec4<f32>,
  @location(5) m2     : vec4<f32>,
  @location(6) m3     : vec4<f32>,
) -> VSOut {
  let model = mat4x4<f32>(m0, m1, m2, m3);
  let world = model * vec4<f32>(pos, 1.0);
  // Assume uniform scaling for R2; generic inverse-transpose on the normal
  // lives in R3 when per-instance scale diverges from 1:1:1 in practice.
  let worldNormal = normalize((model * vec4<f32>(normal, 0.0)).xyz);

  var out : VSOut;
  out.pos      = scene.viewProj * world;
  out.worldPos = world.xyz;
  out.worldNrm = worldNormal;
  out.color    = color;
  out.lightUV  = scene.lightViewProj * world;
  return out;
}

fn sampleShadow(lightUV : vec4<f32>) -> f32 {
  // NDC → [0,1] UV, flipping Y for texture space.
  let proj = lightUV.xyz / lightUV.w;
  let uv   = vec2<f32>(proj.x * 0.5 + 0.5, 0.5 - proj.y * 0.5);
  if (uv.x < 0.0 || uv.x > 1.0 || uv.y < 0.0 || uv.y > 1.0) {
    return 1.0;
  }
  let bias = 0.005;
  let depthRef = proj.z - bias;
  return textureSampleCompare(shadowMap, shadowSampler, uv, depthRef);
}

@fragment
fn fs_main(in : VSOut) -> @location(0) vec4<f32> {
  let N = normalize(in.worldNrm);
  let L = normalize(-scene.lightDir.xyz);
  let lambert = max(dot(N, L), 0.0);

  let shadow = sampleShadow(in.lightUV);

  let direct = in.color * scene.lightColor.rgb * scene.lightColor.a * lambert * shadow;
  let ambient = in.color * scene.ambientColor.rgb * scene.ambientColor.a;
  let color = direct + ambient;
  return vec4<f32>(color, 1.0);
}
`

// shadowWGSL is the depth-only shader that populates the directional-light
// shadow map. It takes only positions and per-instance transforms — no
// colors, no normals — and writes only depth.
const shadowWGSL = `
struct ShadowUniforms {
  lightViewProj : mat4x4<f32>,
};

@group(0) @binding(0) var<uniform> shadowU : ShadowUniforms;

@vertex
fn vs_main(
  @location(0) pos : vec3<f32>,
  @location(1) m0  : vec4<f32>,
  @location(2) m1  : vec4<f32>,
  @location(3) m2  : vec4<f32>,
  @location(4) m3  : vec4<f32>,
) -> @builtin(position) vec4<f32> {
  let model = mat4x4<f32>(m0, m1, m2, m3);
  return shadowU.lightViewProj * model * vec4<f32>(pos, 1.0);
}
`
