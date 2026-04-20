package bundle

// litWGSL is the R2 physically-based lit + shadowed shader.
//
// Lighting model:
//   - Direct: Cook-Torrance specular (GGX D, Smith V, Schlick F) plus
//     energy-conserving Lambertian diffuse.
//   - Indirect: constant ambient scaled by baseColor until IBL lands in R3.
//   - Shadow: comparison-sampled directional shadow map with a conservative
//     constant bias. Receiver-plane depth bias arrives with CSM in R3.
//
// Material inputs come from a per-mesh-entry MaterialUniforms (group 1):
// baseColor, metalness, roughness, emissive, and texture flags. When no
// material is supplied the renderer defaults to baseColor=vertex-color,
// metal=0, roughness=0.6.
const litWGSL = `
struct Scene {
  viewProj         : mat4x4<f32>,
  lightViewProj0   : mat4x4<f32>,
  lightViewProj1   : mat4x4<f32>,
  lightViewProj2   : mat4x4<f32>,
  cameraPos        : vec4<f32>,
  lightDir         : vec4<f32>,
  lightColor       : vec4<f32>,
  ambientColor     : vec4<f32>,
  skyColor         : vec4<f32>,
  groundColor      : vec4<f32>,
  cascadeSplits    : vec4<f32>, // xyz = view-space far distances for cascades 0/1/2
};

struct Material {
  baseColor     : vec4<f32>, // rgb + a  (a unused for R2, reserved for alpha)
  pbrParams     : vec4<f32>, // x=metalness, y=roughness, z=emissiveStrength, w=useVertexColor
  emissive      : vec4<f32>,
  textureParams : vec4<f32>, // x=hasBaseColor, y=hasNormal, z=hasRoughMap, w=hasMetalMap
  textureParams2: vec4<f32>, // x=hasEmissiveMap
};

@group(0) @binding(0) var<uniform> scene             : Scene;
@group(0) @binding(1) var          shadowMap         : texture_depth_2d_array;
@group(0) @binding(2) var          shadowSampler     : sampler_comparison;
@group(1) @binding(0) var<uniform> material          : Material;
@group(1) @binding(1) var          baseColorTexture  : texture_2d<f32>;
@group(1) @binding(2) var          baseColorSampler  : sampler;
@group(1) @binding(3) var          normalMapTexture  : texture_2d<f32>;
@group(1) @binding(4) var          normalMapSampler  : sampler;
@group(1) @binding(5) var          roughnessMapTex   : texture_2d<f32>;
@group(1) @binding(6) var          metalnessMapTex   : texture_2d<f32>;
@group(1) @binding(7) var          emissiveMapTex    : texture_2d<f32>;

struct VSOut {
  @builtin(position) pos : vec4<f32>,
  @location(0) color    : vec3<f32>,
  @location(1) worldPos : vec3<f32>,
  @location(2) worldNrm : vec3<f32>,
  @location(3) viewZ    : f32,
  @location(4) uv       : vec2<f32>,
  @location(5) @interpolate(flat) pickId : u32,
};

struct FSOut {
  @location(0) color  : vec4<f32>,
  @location(1) pickId : u32,
};

@vertex
fn vs_main(
  @location(0) pos    : vec3<f32>,
  @location(1) color  : vec3<f32>,
  @location(2) normal : vec3<f32>,
  @location(3) uv     : vec2<f32>,
  @location(4) m0     : vec4<f32>,
  @location(5) m1     : vec4<f32>,
  @location(6) m2     : vec4<f32>,
  @location(7) m3     : vec4<f32>,
) -> VSOut {
  let model = mat4x4<f32>(m0, m1, m2, m3);
  let world = model * vec4<f32>(pos, 1.0);
  let worldNormal = normalize((model * vec4<f32>(normal, 0.0)).xyz);

  var out : VSOut;
  out.pos      = scene.viewProj * world;
  out.worldPos = world.xyz;
  out.worldNrm = worldNormal;
  out.color    = color;
  // viewZ is the camera-relative depth used for cascade selection in fs_main.
  // We approximate it as the distance from the camera to the vertex — exact
  // enough for picking the right cascade while the view matrix stays
  // orthographic-approximated in R3.
  let toCam = world.xyz - scene.cameraPos.xyz;
  out.viewZ    = length(toCam);
  out.uv       = uv;
  return out;
}

fn cascadeLightMatrix(idx : i32) -> mat4x4<f32> {
  if (idx == 0) { return scene.lightViewProj0; }
  if (idx == 1) { return scene.lightViewProj1; }
  return scene.lightViewProj2;
}

fn pickCascade(viewZ : f32) -> i32 {
  if (viewZ < scene.cascadeSplits.x) { return 0; }
  if (viewZ < scene.cascadeSplits.y) { return 1; }
  return 2;
}

fn sampleShadow(worldPos : vec3<f32>, viewZ : f32) -> f32 {
  let idx = pickCascade(viewZ);
  let lm  = cascadeLightMatrix(idx);
  let lightUV = lm * vec4<f32>(worldPos, 1.0);
  let proj = lightUV.xyz / lightUV.w;
  let uv   = vec2<f32>(proj.x * 0.5 + 0.5, 0.5 - proj.y * 0.5);
  if (uv.x < 0.0 || uv.x > 1.0 || uv.y < 0.0 || uv.y > 1.0) {
    return 1.0;
  }
  // Tighter cascades need less bias; loosen it for cascade 2 which spans
  // a larger volume per texel.
  let bias = 0.003 + 0.003 * f32(idx);
  let depthRef = proj.z - bias;
  return textureSampleCompareLevel(shadowMap, shadowSampler, uv, idx, depthRef);
}

// GGX / Trowbridge-Reitz normal distribution.
fn distributionGGX(NdotH : f32, roughness : f32) -> f32 {
  let a  = roughness * roughness;
  let a2 = a * a;
  let d  = NdotH * NdotH * (a2 - 1.0) + 1.0;
  return a2 / (3.141592653589793 * d * d + 1e-7);
}

// Smith joint visibility approximation (Hammon 2017): cancels out the 4*NdotL*NdotV.
fn geometrySmith(NdotV : f32, NdotL : f32, roughness : f32) -> f32 {
  let a = roughness * roughness;
  let ggxV = NdotL * (NdotV * (1.0 - a) + a);
  let ggxL = NdotV * (NdotL * (1.0 - a) + a);
  return 0.5 / max(ggxV + ggxL, 1e-5);
}

fn fresnelSchlick(F0 : vec3<f32>, VdotH : f32) -> vec3<f32> {
  let k = pow(clamp(1.0 - VdotH, 0.0, 1.0), 5.0);
  return F0 + (vec3<f32>(1.0) - F0) * k;
}

fn perturbNormal(geomN : vec3<f32>, worldPos : vec3<f32>, uv : vec2<f32>) -> vec3<f32> {
  let q1 = dpdx(worldPos);
  let q2 = dpdy(worldPos);
  let st1 = dpdx(uv);
  let st2 = dpdy(uv);
  let det = st1.x * st2.y - st2.x * st1.y;
  if (abs(det) < 1e-5) {
    return geomN;
  }

  let tangentRaw = (q1 * st2.y - q2 * st1.y) / det;
  let T = normalize(tangentRaw - geomN * dot(geomN, tangentRaw));
  let B = normalize(cross(geomN, T));
  let mapped = textureSample(normalMapTexture, normalMapSampler, uv).xyz * 2.0 - vec3<f32>(1.0);
  return normalize(mat3x3<f32>(T, B, geomN) * mapped);
}

@fragment
fn fs_main(in : VSOut) -> FSOut {
  let geomN = normalize(in.worldNrm);
  let mappedN = perturbNormal(geomN, in.worldPos, in.uv);
  let hasNormalMap = step(0.5, material.textureParams.y);
  let N = normalize(mix(geomN, mappedN, hasNormalMap));
  let V = normalize(scene.cameraPos.xyz - in.worldPos);
  let L = normalize(-scene.lightDir.xyz);
  let H = normalize(V + L);

  let NdotL = max(dot(N, L), 0.0);
  let NdotV = max(dot(N, V), 1e-4);
  let NdotH = max(dot(N, H), 0.0);
  let VdotH = max(dot(V, H), 0.0);

  // Material resolution: vertex color acts as baseColor when the material
  // flags it (useVertexColor = 1). A per-material baseColor texture (white
  // 1×1 fallback when none specified) modulates the resolved baseColor so
  // textures tint rather than replace.
  let useVertex = step(0.5, material.pbrParams.w);
  let solid = mix(material.baseColor.rgb, in.color, useVertex);
  let sampled = textureSample(baseColorTexture, baseColorSampler, in.uv).rgb;
  let baseColor = solid * sampled;

  // Per-texel PBR inputs: each map's .r channel scales the corresponding
  // uniform factor. hasRoughMap / hasMetalMap gates the lookup so materials
  // without maps keep their flat factors.
  let hasRoughMap = step(0.5, material.textureParams.z);
  let hasMetalMap = step(0.5, material.textureParams.w);
  let roughSample = textureSample(roughnessMapTex, baseColorSampler, in.uv).r;
  let metalSample = textureSample(metalnessMapTex, baseColorSampler, in.uv).r;
  let metalness = clamp(material.pbrParams.x * mix(1.0, metalSample, hasMetalMap), 0.0, 1.0);
  let roughness = clamp(material.pbrParams.y * mix(1.0, roughSample, hasRoughMap), 0.04, 1.0);

  // F0: 0.04 for dielectrics, baseColor for metals, linearly interpolated.
  let F0 = mix(vec3<f32>(0.04), baseColor, metalness);

  let D = distributionGGX(NdotH, roughness);
  let G = geometrySmith(NdotV, NdotL, roughness);
  let F = fresnelSchlick(F0, VdotH);

  let specular = D * G * F;

  // Energy-conserving diffuse (kD = (1 - F) * (1 - metalness)).
  let kS = F;
  let kD = (vec3<f32>(1.0) - kS) * (1.0 - metalness);
  let diffuse = kD * baseColor / 3.141592653589793;

  let radiance = scene.lightColor.rgb * scene.lightColor.a;
  let shadow = sampleShadow(in.worldPos, in.viewZ);
  let direct = (diffuse + specular) * radiance * NdotL * shadow;

  // Hemisphere ambient: blend the sky/ground dome colors by the world
  // normal's up-component. Modulated by a tinted ambient (.rgb) and an
  // intensity (.a) so the artist can pull the whole ambient channel up or
  // down with one scalar.
  let hemi = mix(scene.groundColor.rgb, scene.skyColor.rgb, N.y * 0.5 + 0.5);
  let ambient  = baseColor * hemi * scene.ambientColor.rgb * scene.ambientColor.a;
  // Emissive: scalar strength × (emissive tint × optional emissive map sample).
  let hasEmissiveMap = step(0.5, material.textureParams2.x);
  let emissiveSample = textureSample(emissiveMapTex, baseColorSampler, in.uv).rgb;
  let emissiveTint = material.emissive.rgb * mix(vec3<f32>(1.0), emissiveSample, hasEmissiveMap);
  let emissive = emissiveTint * material.pbrParams.z;
  let color = direct + ambient + emissive;
  var out : FSOut;
  out.color  = vec4<f32>(color, 1.0);
  out.pickId = in.pickId;
  return out;
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
