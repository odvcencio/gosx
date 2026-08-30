package bundle

import "strings"

// litWGSL is the R2 physically-based lit + shadowed shader.
//
// Lighting model:
//   - Direct: one Cook-Torrance lobe per scene light. The loop reads the light
//     array in group 0 binding 5 and shades five kinds: ambient, directional,
//     point, spot and hemisphere.
//   - Indirect: three independent RenderEnvironment terms, ambient plus sky
//     plus ground, summed and then scaled by baseColor.
//   - Shadow: comparison-sampled directional shadow map with a conservative
//     constant bias. Receiver-plane depth bias arrives with CSM in R3.
//
// Material inputs come from a per-mesh-entry MaterialUniforms (group 1):
// baseColor, metalness, roughness, emissive, and texture flags. When no
// material is supplied the renderer defaults to baseColor=vertex-color,
// metal=0, roughness=0.6.
//
// The shading terms here must match the browser copy in
// client/js/bootstrap-src/16a-scene-webgpu.js WGSL_PBR_FRAGMENT. Nothing
// regenerates one copy from the other, so lit_drift_test.go pins the terms both
// copies share and records the terms they still compute differently. Change a
// term here and update that ledger in the same commit.
const litWGSL = `
// Light is one packed scene light. resolveSceneLights in renderer.go writes it,
// and the first five fields carry the same meaning as the browser Light struct
// in 16a-scene-webgpu.js, field for field.
//
// The browser record is seven vec4. The two it carries beyond this one hold the
// world-space edge vectors of a rect-area light. engine.RenderLight has no
// width and no height, so those two vectors cannot exist on this path, and this
// record stops at five vec4. See the rect-area-light row in lit_drift_test.go.
struct Light {
  position       : vec4<f32>, // xyz = world position, w = kind code
  direction      : vec4<f32>, // xyz = direction the light shines, w = intensity
  color          : vec4<f32>, // rgb = colour, or the sky colour, a = range
  params         : vec4<f32>, // x = decay, y = shadow bias, z = cast shadow, w = cone angle
  groundPenumbra : vec4<f32>, // rgb = hemisphere ground colour, a = spot penumbra
};

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
  envParams        : vec4<f32>, // x = cubemap intensity, y = Y rotation, z = has cubemap
  lightParams      : vec4<f32>, // x = light count, y = shadowed light index, zw reserved
};

struct Material {
  baseColor     : vec4<f32>, // rgba
  pbrParams     : vec4<f32>, // x=metalness, y=roughness, z=emissiveStrength, w=useVertexColor
  emissive      : vec4<f32>,
  textureParams : vec4<f32>, // x=hasBaseColor, y=hasNormal, z=hasRoughMap, w=hasMetalMap
  textureParams2: vec4<f32>, // x=hasEmissiveMap
  physicalParams : vec4<f32>, // x=clearcoat, y=sheen, z=transmission, w=iridescence
  physicalParams2: vec4<f32>, // x=anisotropy, y=dielectricF0
  specularParams : vec4<f32>, // xyz = effective dielectric F0, w = F90
};

@group(0) @binding(0) var<uniform> scene             : Scene;
@group(0) @binding(1) var          shadowMap         : texture_depth_2d_array;
@group(0) @binding(2) var          shadowSampler     : sampler_comparison;
@group(0) @binding(3) var          envCubeTexture    : texture_cube<f32>;
@group(0) @binding(4) var          envCubeSampler    : sampler;
// The light array is a storage buffer, not a fixed uniform array, so no
// compile-time light cap exists. The fragment loop bounds itself with
// arrayLength, exactly as the browser copy does.
@group(0) @binding(5) var<storage, read> lights       : array<Light>;
@group(1) @binding(0) var<uniform> material          : Material;
@group(1) @binding(1) var          baseColorTexture  : texture_2d<f32>;
@group(1) @binding(2) var          baseColorSampler  : sampler;
@group(1) @binding(3) var          normalMapTexture  : texture_2d<f32>;
@group(1) @binding(4) var          normalMapSampler  : sampler;
@group(1) @binding(5) var          roughnessMapTex   : texture_2d<f32>;
@group(1) @binding(6) var          metalnessMapTex   : texture_2d<f32>;
@group(1) @binding(7) var          emissiveMapTex    : texture_2d<f32>;

struct AffineNormalResult {
  normal : vec3<f32>,
  orientation : f32,
};

fn affineNormal(model : mat4x4<f32>, localNormal : vec3<f32>) -> AffineNormalResult {
  let linearScale = max(max(max(abs(model[0].x), abs(model[0].y)), max(abs(model[0].z), abs(model[1].x))), max(max(abs(model[1].y), abs(model[1].z)), max(max(abs(model[2].x), abs(model[2].y)), abs(model[2].z))));
  if (linearScale == 0.0) {
    return AffineNormalResult(normalize(localNormal), 1.0);
  }
  let c0 = model[0].xyz / linearScale;
  let c1 = model[1].xyz / linearScale;
  let c2 = model[2].xyz / linearScale;
  let co0 = cross(c1, c2);
  let co1 = cross(c2, c0);
  let co2 = cross(c0, c1);
  let determinant = dot(c0, co0);
  if (abs(determinant) <= 1e-12) {
    return AffineNormalResult(normalize(localNormal), 1.0);
  }
  let orientation = select(-1.0, 1.0, determinant > 0.0);
  return AffineNormalResult(normalize(mat3x3<f32>(co0, co1, co2) * localNormal * orientation), orientation);
}

struct VSOut {
  @builtin(position) pos : vec4<f32>,
  @location(0) color    : vec3<f32>,
  @location(1) worldPos : vec3<f32>,
  @location(2) worldNrm : vec3<f32>,
  @location(3) viewZ    : f32,
  @location(4) uv       : vec2<f32>,
  @location(5) @interpolate(flat) pickId : u32,
  @location(6) @interpolate(flat) orientation : f32,
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
  @location(8) pickData : vec4<u32>,
) -> VSOut {
  let model = mat4x4<f32>(m0, m1, m2, m3);
  let world = model * vec4<f32>(pos, 1.0);
  let affine = affineNormal(model, normal);
  let worldNormal = affine.normal;

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
  out.pickId   = pickData.x;
  out.orientation = affine.orientation;
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

fn fresnelSchlick(F0 : vec3<f32>, F90 : f32, VdotH : f32) -> vec3<f32> {
  let k = pow(clamp(1.0 - VdotH, 0.0, 1.0), 5.0);
  return F0 + (vec3<f32>(F90) - F0) * k;
}

// Point light distance falloff. A light with a range uses the windowed inverse
// square law that three.js uses; a light with no range uses the plain inverse
// power of the decay. Both browser renderers carry this expression unchanged.
fn pointLightAttenuation(dist : f32, range : f32, decay : f32) -> f32 {
  if (range > 0.0) {
    let ratio = clamp(1.0 - pow(dist / range, 4.0), 0.0, 1.0);
    return ratio * ratio / max(dist * dist, 0.0001);
  }
  return 1.0 / max(pow(dist, decay), 0.0001);
}

// Spot cone falloff. L points from the surface toward the light, and spotDir is
// the direction the light shines. The penumbra narrows the inner cone, so a
// penumbra of zero gives a hard edge and a penumbra of one fades from the axis.
fn spotConeAttenuation(L : vec3<f32>, spotDir : vec3<f32>, angle : f32, penumbra : f32) -> f32 {
  let cosAngle = dot(L, -normalize(spotDir));
  let outerCos = cos(angle);
  let innerCos = cos(angle * (1.0 - penumbra));
  return clamp((cosAngle - outerCos) / max(innerCos - outerCos, 0.001), 0.0, 1.0);
}

fn rotateEnvY(v : vec3<f32>, radians : f32) -> vec3<f32> {
  let c = cos(radians);
  let s = sin(radians);
  return vec3<f32>(
    v.x * c - v.z * s,
    v.y,
    v.x * s + v.z * c,
  );
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
fn fs_main(in : VSOut, @builtin(front_facing) frontFacing : bool) -> FSOut {
  if (frontFacing != (in.orientation > 0.0)) { discard; }
  let geomN = normalize(in.worldNrm);
  let mappedN = perturbNormal(geomN, in.worldPos, in.uv);
  let hasNormalMap = step(0.5, material.textureParams.y);
  let N = normalize(mix(geomN, mappedN, hasNormalMap));
  let V = normalize(scene.cameraPos.xyz - in.worldPos);
  // scene.lightDir still carries the primary directional light. The cascade fit
  // uses it, and the image-based terms at the end of this function take their
  // Fresnel from it. The light loop below shades every light from the light
  // array instead, and declares its own copy of each term.
  let L = normalize(-scene.lightDir.xyz);
  let H = normalize(V + L);

  let NdotV = max(dot(N, V), 1e-4);
  let VdotH = max(dot(V, H), 0.0);

  // Material resolution: vertex color acts as baseColor when the material
  // flags it (useVertexColor = 1). A per-material baseColor texture (white
  // 1×1 fallback when none specified) modulates the resolved baseColor so
  // textures tint rather than replace.
  let useVertex = step(0.5, material.pbrParams.w);
  let solid = mix(material.baseColor.rgb, in.color, useVertex);
  let sampled = textureSample(baseColorTexture, baseColorSampler, in.uv).rgb;
  let baseColor = solid * sampled;

  // Per-texel material inputs. glTF 2.0 packs roughness in green and metalness
  // in blue, and leaves red for the occlusion map that shares the texture. Read
  // green and blue, or a packed texture drives both factors from occlusion.
  // Both browser renderers read the same two channels, and
  // assetpipe/texture PackMetallicRoughness writes that layout. A grey
  // single-factor map holds one value in all three channels, so the channel
  // choice changes the image only for a packed texture. hasRoughMap and
  // hasMetalMap gate the lookup, so a material with no map keeps its flat
  // factor.
  let hasRoughMap = step(0.5, material.textureParams.z);
  let hasMetalMap = step(0.5, material.textureParams.w);
  let roughSample = textureSample(roughnessMapTex, baseColorSampler, in.uv).g;
  let metalSample = textureSample(metalnessMapTex, baseColorSampler, in.uv).b;
  let metalness = clamp(material.pbrParams.x * mix(1.0, metalSample, hasMetalMap), 0.0, 1.0);
  var roughness = clamp(material.pbrParams.y * mix(1.0, roughSample, hasRoughMap), 0.04, 1.0);
  let anisotropy = clamp(material.physicalParams2.x, -1.0, 1.0);
  roughness = clamp(roughness * (1.0 - abs(anisotropy) * 0.28), 0.04, 1.0);

  // Specular: the prepared material carries the effective dielectric F0 in
  // linear light in specularParams.xyz (min(IOR F0 * colour, 1) * intensity)
  // and its grazing reflectance F90 in specularParams.w (= the intensity).
  // Byte 100 still carries the legacy IOR F0 for readers without the vec4;
  // this shader reads the prepared one.
  let specF0 = material.specularParams.xyz;
  let specF90 = material.specularParams.w;

  // F0: the dielectric F0, the base colour for metals. A fully metallic
  // surface takes the base colour exactly, so no dielectric lane can leak in
  // through a rounding of the lerp.
  var F0 = mix(material.specularParams.xyz, baseColor, metalness);
  var F90 = mix(material.specularParams.w, 1.0, metalness);
  if (metalness >= 1.0) {
    F0 = baseColor;
    F90 = 1.0;
  }

  // Fresnel at the primary light. The image-based terms below reuse it, so a
  // cubemap keeps the exact response it had before the light array arrived.
  let F = fresnelSchlick(F0, F90, VdotH);

  // Energy-conserving diffuse. The weight is a scalar built from the
  // dielectric Fresnel alone: (1 - maxRGB(Fdiel)) * (1 - metalness). The
  // earlier componentwise mixed form tinted the diffuse by the metallic
  // Fresnel, double-counting the base colour. The browser copy still uses
  // that form; see the divergence ledger.
  let Fdiel = fresnelSchlick(specF0, specF90, VdotH);
  let kD = (1.0 - max(Fdiel.x, max(Fdiel.y, Fdiel.z))) * (1.0 - metalness);

  // Direct light: one Cook-Torrance lobe per scene light.
  //
  // The array holds every authored light, in bundle order, so a scene lit by a
  // point light shades here instead of falling back to a canned key light. The
  // kind codes match the two browser renderers: 0 ambient, 1 directional,
  // 2 point, 3 spot, 4 hemisphere, 5 rect-area. A LightProbe arrives as code 0,
  // because a probe is a flat term with no position; the browser folds it the
  // same way.
  //
  // Code 5 falls through with no contribution. engine.RenderLight carries no
  // width and no height, so the rectangle the form factor integrates over does
  // not exist on this path. See the rect-area-light row in lit_drift_test.go.
  //
  // Every texture read stays outside this loop on purpose. textureSample needs
  // uniform control flow, and a loop over a per-pixel count is not uniform.
  var directSum = vec3<f32>(0.0);
  let lightCount = min(u32(max(scene.lightParams.x, 0.0)), arrayLength(&lights));
  let shadowLightIndex = i32(scene.lightParams.y);
  for (var i = 0u; i < lightCount; i = i + 1u) {
    let light = lights[i];
    let kind = u32(light.position.w);
    let lightColor = light.color.rgb;
    let intensity = light.direction.w;
    let range = light.color.a;
    let decay = light.params.x;

    // Ambient (code 0): a flat term with no BRDF and no falloff.
    if (kind == 0u) {
      directSum = directSum + baseColor * lightColor * intensity;
      continue;
    }
    // Hemisphere (code 4): sky above, ground below, blended by the normal Y.
    if (kind == 4u) {
      let hemiBlend = N.y * 0.5 + 0.5;
      let hemiColor = mix(light.groundPenumbra.rgb, lightColor, hemiBlend);
      directSum = directSum + baseColor * hemiColor * intensity;
      continue;
    }
    // Rect-area (code 5): recorded as unshaded, not approximated.
    if (kind == 5u) {
      continue;
    }

    var L : vec3<f32>;
    var attenuation = 1.0;
    if (kind == 1u) {
      L = normalize(-light.direction.xyz);
    } else if (kind == 3u) {
      let toLight = light.position.xyz - in.worldPos;
      let dist = length(toLight);
      L = toLight / max(dist, 0.0001);
      let cone = spotConeAttenuation(L, light.direction.xyz, light.params.w, light.groundPenumbra.a);
      attenuation = pointLightAttenuation(dist, range, decay) * cone;
    } else {
      let toLight = light.position.xyz - in.worldPos;
      let dist = length(toLight);
      L = toLight / max(dist, 0.0001);
      attenuation = pointLightAttenuation(dist, range, decay);
    }

    let H = normalize(V + L);
    let NdotL = max(dot(N, L), 0.0);
    let NdotH = max(dot(N, H), 0.0);
    let VdotH = max(dot(V, H), 0.0);

    let D = distributionGGX(NdotH, roughness);
    let G = geometrySmith(NdotV, NdotL, roughness);
    let F = fresnelSchlick(F0, F90, VdotH);

    let FdielL = fresnelSchlick(specF0, specF90, VdotH);
    let kD = (1.0 - max(FdielL.x, max(FdielL.y, FdielL.z))) * (1.0 - metalness);

    let specular = D * G * F;

    let diffuse = kD * baseColor / 3.141592653589793;

    let radiance = lightColor * intensity * attenuation;
    // One cascaded shadow map exists, and it is fitted to the light in
    // scene.lightDir. Only that light reads it; every other light is unshadowed.
    var shadow = 1.0;
    if (kind == 1u && i32(i) == shadowLightIndex) {
      shadow = sampleShadow(in.worldPos, in.viewZ);
    }
    let direct = (diffuse + specular) * radiance * NdotL * shadow;
    directSum = directSum + direct;
  }
  // direct names the summed direct light. The per-light line above keeps the
  // exact expression the browser copy and the CPU oracle in
  // render/gpu/headless both pin, so all three copies still name one term.
  let direct = directSum;

  // Environment ambient: sum three independent terms, so each intensity gates
  // only its own colour. Both browser renderers sum the same three terms. The
  // earlier form also multiplied the sky and ground blend by the ambient
  // intensity. The default ambient intensity is 0.3, so that form cut the whole
  // dome to about a third and made every native image too dark.
  //
  // The sky and ground intensities arrive premultiplied into .rgb from
  // resolveHemisphereAmbient in renderer.go, so this line carries no explicit
  // sky or ground factor. This line applies the ambient intensity from .a, and
  // applies it once.
  let hemi = N.y * 0.5 + 0.5;
  let envDiffuse = scene.ambientColor.rgb * scene.ambientColor.a
                 + scene.skyColor.rgb * hemi
                 + scene.groundColor.rgb * (1.0 - hemi);
  let ambient = envDiffuse * baseColor;
  let cubeDiffuse = textureSample(envCubeTexture, envCubeSampler, rotateEnvY(N, scene.envParams.y)).rgb * baseColor * kD;
  let envReflect = rotateEnvY(reflect(-V, N), scene.envParams.y);
  let envSpecular = textureSample(envCubeTexture, envCubeSampler, envReflect).rgb * F * (1.0 - roughness * 0.65);
  let cubeIBL = (cubeDiffuse + envSpecular) * scene.envParams.x * scene.envParams.z;
  // Emissive colour: start from the shaded base colour, and let the emissive
  // map replace it. Both browser renderers do the same. The earlier form
  // multiplied the map by material.emissive.rgb. materialFromRender fills those
  // lanes from the base colour, so the native path applied the base colour
  // twice and emitted the wrong hue.
  let hasEmissiveMap = step(0.5, material.textureParams2.x);
  let emissiveSample = textureSample(emissiveMapTex, baseColorSampler, in.uv).rgb;
  let emissiveColor = mix(baseColor, emissiveSample, hasEmissiveMap);
  let emissive = emissiveColor * material.pbrParams.z;
  var color = direct + ambient + cubeIBL + emissive;
  // Clear coat: scale the lobe by 0.28, as both browser renderers do. The
  // earlier gain of 1.0 made a coated highlight about 3.6 times too bright.
  let clearcoat = clamp(material.physicalParams.x, 0.0, 1.0);
  let clearcoatPower = mix(12.0, 96.0, 1.0 - roughness);
  color = color + vec3<f32>(pow(NdotV, clearcoatPower) * clearcoat * 0.28);
  // Sheen: add the velvet term instead of blending toward it. The earlier
  // blend pulled the lit colour toward the velvet colour, so a fabric lost
  // brightness and saturation as sheen rose.
  let sheen = clamp(material.physicalParams.y, 0.0, 1.0);
  let velvet = pow(1.0 - NdotV, 3.0) * sheen;
  color = color + baseColor * velvet * 0.55;
  // Iridescence: take the phases and the frequency both browser renderers use.
  // The earlier constants swept one full turn (6.2831853 radians) across the
  // facing range, so the hue bands sat about 1.27 times wider apart.
  let iridescence = clamp(material.physicalParams.w, 0.0, 1.0);
  let iri = 0.5 + 0.5 * cos(vec3<f32>(0.0, 2.1, 4.2) + NdotV * 8.0);
  color = mix(color, color * (vec3<f32>(0.65) + iri * 0.7), iridescence * pow(1.0 - NdotV, 2.0));
  let transmission = clamp(material.physicalParams.z, 0.0, 1.0) * (1.0 - metalness);
  color = mix(color, ambient + baseColor * 0.1, transmission * 0.55);
  var out : FSOut;
  out.color  = vec4<f32>(color, clamp(material.baseColor.a, 0.0, 1.0));
  out.pickId = in.pickId;
  return out;
}
`

func skinnedLitWGSL() string {
	const signature = `fn vs_main(
  @location(0) pos    : vec3<f32>,
  @location(1) color  : vec3<f32>,
  @location(2) normal : vec3<f32>,
  @location(3) uv     : vec2<f32>,
  @location(4) m0     : vec4<f32>,
  @location(5) m1     : vec4<f32>,
  @location(6) m2     : vec4<f32>,
  @location(7) m3     : vec4<f32>,
  @location(8) pickData : vec4<u32>,
) -> VSOut {`
	const skinnedSignature = `fn vs_main(
  @location(0) pos     : vec3<f32>,
  @location(1) color   : vec3<f32>,
  @location(2) normal  : vec3<f32>,
  @location(3) uv      : vec2<f32>,
  @location(4) m0      : vec4<f32>,
  @location(5) m1      : vec4<f32>,
  @location(6) m2      : vec4<f32>,
  @location(7) m3      : vec4<f32>,
  @location(8) pickData : vec4<u32>,
  @location(9) joints  : vec4<u32>,
  @location(10) weights : vec4<f32>,
  @location(11) b0     : vec4<f32>,
  @location(12) b1     : vec4<f32>,
  @location(13) b2     : vec4<f32>,
  @location(14) b3     : vec4<f32>,
) -> VSOut {`
	const rigidTransform = `  let model = mat4x4<f32>(m0, m1, m2, m3);
  let world = model * vec4<f32>(pos, 1.0);
  let affine = affineNormal(model, normal);
  let worldNormal = affine.normal;`
	const skinnedTransform = `  let model = mat4x4<f32>(m0, m1, m2, m3);
  let bindPose = mat4x4<f32>(b0, b1, b2, b3);
  let skinnedLocal = bindPose * applySkinning(pos, joints, weights);
  let skinnedNormal = affineNormal(bindPose, applySkinningNormal(normal, joints, weights)).normal;
  let world = model * skinnedLocal;
  let affine = affineNormal(model, skinnedNormal);
  let worldNormal = affine.normal;`
	src := strings.Replace(litWGSL, signature, skinnedSignature, 1)
	src = strings.Replace(src, rigidTransform, skinnedTransform, 1)
	return skinningWGSL + "\n" + src
}

// shadowWGSL is the depth-only shader that populates the directional-light
// shadow map. It takes only positions and per-instance transforms — no
// colors, no normals — and writes only depth.
const shadowWGSL = `
struct ShadowUniforms {
  lightViewProj : mat4x4<f32>,
};

@group(0) @binding(0) var<uniform> shadowU : ShadowUniforms;

struct ShadowOut {
  @builtin(position) position : vec4<f32>,
  @location(0) @interpolate(flat) orientation : f32,
};

@vertex
fn vs_main(
  @location(0) pos : vec3<f32>,
  @location(1) m0  : vec4<f32>,
  @location(2) m1  : vec4<f32>,
  @location(3) m2  : vec4<f32>,
  @location(4) m3  : vec4<f32>,
) -> ShadowOut {
  let model = mat4x4<f32>(m0, m1, m2, m3);
  let linearScale = max(max(max(abs(model[0].x), abs(model[0].y)), max(abs(model[0].z), abs(model[1].x))), max(max(abs(model[1].y), abs(model[1].z)), max(max(abs(model[2].x), abs(model[2].y)), abs(model[2].z))));
  var orientation = 1.0;
  if (linearScale > 0.0) {
    let c0 = model[0].xyz / linearScale;
    let c1 = model[1].xyz / linearScale;
    let c2 = model[2].xyz / linearScale;
    let determinant = dot(c0, cross(c1, c2));
    if (abs(determinant) > 1e-12) { orientation = select(-1.0, 1.0, determinant > 0.0); }
  }
  var out : ShadowOut;
  out.position = shadowU.lightViewProj * model * vec4<f32>(pos, 1.0);
  out.orientation = orientation;
  return out;
}

@fragment
fn fs_main(in : ShadowOut, @builtin(front_facing) frontFacing : bool) {
  if (frontFacing != (in.orientation > 0.0)) { discard; }
}
`
