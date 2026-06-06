  // WebGPU rendering backend for GoSX Scene3D.
  //
  // Parallel implementation of the PBR WebGL2 renderer (16-scene-webgl.js)
  // using the WebGPU API. Provides Cook-Torrance BRDF with per-pixel
  // lighting, shadow maps, fog, and post-processing. Points are rendered
  // as instanced camera-facing quads since WebGPU has no gl_PointSize.

  // -----------------------------------------------------------------------
  // WGSL Shader Sources
  // -----------------------------------------------------------------------

  // -- Shared constants embedded in multiple shaders --
  var WGSL_COMMON_CONSTANTS = [
    "const PI: f32 = 3.14159265359;",
    "const MAX_LIGHTS: u32 = 8u;",
  ].join("\n");

  // -- Frame-level uniform structures --
  var WGSL_FRAME_STRUCTS = [
    "struct Light {",
    "    position: vec4f,",       // xyz = position, w = type (0=ambient,1=dir,2=point)
    "    direction: vec4f,",      // xyz = direction, w = intensity
    "    color: vec4f,",          // rgb = color, a = range
    "    params: vec4f,",         // x = decay, y = shadowBias, z = castShadow, w = unused
    "};",
    "",
    "struct FrameUniforms {",
    "    viewMatrix: mat4x4f,",
    "    projMatrix: mat4x4f,",
    "    cameraPos: vec3f,",
    "    lightCount: u32,",
    "    viewportWidth: f32,",
    "    viewportHeight: f32,",
    "    toneMap: u32,",
    "    _pad0: u32,",
    "};",
    "",
    "struct FogUniforms {",
    "    fogColor: vec3f,",
    "    fogDensity: f32,",
    "    hasFog: u32,",
    "    _pad0: u32,",
    "    _pad1: u32,",
    "    _pad2: u32,",
    "};",
    "",
    "struct EnvUniforms {",
    "    ambientColor: vec3f,",
    "    ambientIntensity: f32,",
    "    skyColor: vec3f,",
    "    skyIntensity: f32,",
    "    groundColor: vec3f,",
    "    groundIntensity: f32,",
    "};",
    "",
    "struct ShadowUniforms {",
    "    lightSpaceMatrix0: mat4x4f,",
    "    lightSpaceMatrix1: mat4x4f,",
    "    hasShadow0: u32,",
    "    hasShadow1: u32,",
    "    shadowBias0: f32,",
    "    shadowBias1: f32,",
    "    shadowLightIndex0: i32,",
    "    shadowLightIndex1: i32,",
    "    _pad0: u32,",
    "    _pad1: u32,",
    "};",
  ].join("\n");

  // -- PBR material uniform structure --
  var WGSL_MATERIAL_STRUCT = [
    "struct MaterialUniforms {",
    "    albedo: vec3f,",
    "    roughness: f32,",
    "    metalness: f32,",
    "    emissive: f32,",
    "    opacity: f32,",
    "    clearcoat: f32,",
    "    sheen: f32,",
    "    transmission: f32,",
    "    iridescence: f32,",
    "    anisotropy: f32,",
    "    unlit: u32,",
    "    hasAlbedoMap: u32,",
    "    hasNormalMap: u32,",
    "    hasRoughnessMap: u32,",
    "    hasMetalnessMap: u32,",
    "    hasEmissiveMap: u32,",
    "    receiveShadow: u32,",
    "    _pad0: u32,",
    "};",
  ].join("\n");

  // -----------------------------------------------------------------------
  // PBR Vertex Shader (WGSL)
  // -----------------------------------------------------------------------

  var WGSL_PBR_VERTEX = [
    WGSL_FRAME_STRUCTS,
    "",
    "struct VertexInput {",
    "    @location(0) position: vec3f,",
    "    @location(1) normal: vec3f,",
    "    @location(2) uv: vec2f,",
    "    @location(3) tangent: vec4f,",
    "};",
    "",
    "struct VertexOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) worldPos: vec3f,",
    "    @location(1) normal: vec3f,",
    "    @location(2) uv: vec2f,",
    "    @location(3) tangent: vec3f,",
    "    @location(4) bitangent: vec3f,",
    "    @location(5) instanceColor: vec4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "",
    "@vertex fn vertexMain(in: VertexInput) -> VertexOutput {",
    "    var out: VertexOutput;",
    "    out.worldPos = in.position;",
    "    out.normal = normalize(in.normal);",
    "    out.uv = in.uv;",
    "    let T = normalize(in.tangent.xyz);",
    "    let N = out.normal;",
    "    out.tangent = T;",
    "    out.bitangent = cross(N, T) * in.tangent.w;",
    "    out.instanceColor = vec4f(1.0, 1.0, 1.0, 1.0);",
    "    out.clipPos = frame.projMatrix * frame.viewMatrix * vec4f(in.position, 1.0);",
    "    return out;",
    "}",
  ].join("\n");

  // Emitted by m31labs.dev/elio/emit/wgsl from stdlib.Skin().
  // The runtime pads dispatch buffers to the 64-wide workgroup size, so the
  // generated kernel can stay byte-for-byte aligned with Elio's current output.
  var SCENE_ELIO_SKIN_LBS_SOURCE = [
    "struct SkinVertex {",
    "  px : f32,",
    "  py : f32,",
    "  pz : f32,",
    "  w0 : f32,",
    "  w1 : f32,",
    "  w2 : f32,",
    "  w3 : f32,",
    "  b0 : u32,",
    "  b1 : u32,",
    "  b2 : u32,",
    "  b3 : u32,",
    "};",
    "",
    "@group(0) @binding(0) var<storage, read> bones : array<mat4x4<f32>>;",
    "@group(0) @binding(1) var<storage, read> verts : array<SkinVertex>;",
    "@group(0) @binding(2) var<storage, read_write> out : array<f32>;",
    "",
    "@compute @workgroup_size(64)",
    "fn skin(@builtin(global_invocation_id) gid : vec3<u32>) {",
    "  let i = gid.x;",
    "  let v = verts[i];",
    "  let skinned = ((((((((bones[v.b0][0u] * v.px) + (bones[v.b0][1u] * v.py)) + (bones[v.b0][2u] * v.pz)) + bones[v.b0][3u]) * v.w0) + (((((bones[v.b1][0u] * v.px) + (bones[v.b1][1u] * v.py)) + (bones[v.b1][2u] * v.pz)) + bones[v.b1][3u]) * v.w1)) + (((((bones[v.b2][0u] * v.px) + (bones[v.b2][1u] * v.py)) + (bones[v.b2][2u] * v.pz)) + bones[v.b2][3u]) * v.w2)) + (((((bones[v.b3][0u] * v.px) + (bones[v.b3][1u] * v.py)) + (bones[v.b3][2u] * v.pz)) + bones[v.b3][3u]) * v.w3));",
    "  out[((i * 3u) + 0u)] = skinned.x;",
    "  out[((i * 3u) + 1u)] = skinned.y;",
    "  out[((i * 3u) + 2u)] = skinned.z;",
    "}",
  ].join("\n");

  var SCENE_COMPUTED_MORPH_SOURCE = [
    "struct MorphUniforms {",
    "  model : mat4x4<f32>,",
    "  alpha : f32,",
    "  count : f32,",
    "  _pad0 : f32,",
    "  _pad1 : f32,",
    "};",
    "",
    "@group(0) @binding(0) var<storage, read> sourcePacked : array<f32>;",
    "@group(0) @binding(1) var<storage, read> targetPacked : array<f32>;",
    "@group(0) @binding(2) var<storage, read_write> outPositions : array<f32>;",
    "@group(0) @binding(3) var<storage, read_write> outNormals : array<f32>;",
    "@group(0) @binding(4) var<storage, read_write> outTangents : array<f32>;",
    "@group(0) @binding(5) var<uniform> morph : MorphUniforms;",
    "",
    "fn safeNormalize(v : vec3<f32>, fallback : vec3<f32>) -> vec3<f32> {",
    "  let len = length(v);",
    "  if (len > 0.000001) {",
    "    return v / len;",
    "  }",
    "  return fallback;",
    "}",
    "",
    "@compute @workgroup_size(64)",
    "fn morphPose(@builtin(global_invocation_id) gid : vec3<u32>) {",
    "  let i = gid.x;",
    "  if (f32(i) >= morph.count) {",
    "    return;",
    "  }",
    "  let p = i * 3u;",
    "  let t = i * 4u;",
    "  let packed = i * 10u;",
    "  let a = clamp(morph.alpha, 0.0, 1.0);",
    "  let localPosition = mix(",
    "    vec3<f32>(sourcePacked[packed], sourcePacked[packed + 1u], sourcePacked[packed + 2u]),",
    "    vec3<f32>(targetPacked[packed], targetPacked[packed + 1u], targetPacked[packed + 2u]),",
    "    a",
    "  );",
    "  let worldPosition = morph.model * vec4<f32>(localPosition, 1.0);",
    "  outPositions[p] = worldPosition.x;",
    "  outPositions[p + 1u] = worldPosition.y;",
    "  outPositions[p + 2u] = worldPosition.z;",
    "",
    "  let localNormal = mix(",
    "    vec3<f32>(sourcePacked[packed + 3u], sourcePacked[packed + 4u], sourcePacked[packed + 5u]),",
    "    vec3<f32>(targetPacked[packed + 3u], targetPacked[packed + 4u], targetPacked[packed + 5u]),",
    "    a",
    "  );",
    "  let worldNormal = safeNormalize((morph.model * vec4<f32>(localNormal, 0.0)).xyz, vec3<f32>(0.0, 0.0, 1.0));",
    "  outNormals[p] = worldNormal.x;",
    "  outNormals[p + 1u] = worldNormal.y;",
    "  outNormals[p + 2u] = worldNormal.z;",
    "",
    "  let localTangent = mix(",
    "    vec3<f32>(sourcePacked[packed + 6u], sourcePacked[packed + 7u], sourcePacked[packed + 8u]),",
    "    vec3<f32>(targetPacked[packed + 6u], targetPacked[packed + 7u], targetPacked[packed + 8u]),",
    "    a",
    "  );",
    "  let worldTangent = safeNormalize((morph.model * vec4<f32>(localTangent, 0.0)).xyz, vec3<f32>(1.0, 0.0, 0.0));",
    "  outTangents[t] = worldTangent.x;",
    "  outTangents[t + 1u] = worldTangent.y;",
    "  outTangents[t + 2u] = worldTangent.z;",
    "  outTangents[t + 3u] = select(sourcePacked[packed + 9u], targetPacked[packed + 9u], a >= 0.5);",
    "}",
  ].join("\n");

  var WGSL_PBR_INSTANCED_VERTEX = [
    WGSL_FRAME_STRUCTS,
    "",
    "struct VertexInput {",
    "    @location(0) position: vec3f,",
    "    @location(1) normal: vec3f,",
    "    @location(2) uv: vec2f,",
    "    @location(3) tangent: vec4f,",
    "    @location(4) instanceMatrix0: vec4f,",
    "    @location(5) instanceMatrix1: vec4f,",
    "    @location(6) instanceMatrix2: vec4f,",
    "    @location(7) instanceMatrix3: vec4f,",
    "    @location(8) instanceColor: vec4f,",
    "};",
    "",
    "struct VertexOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) worldPos: vec3f,",
    "    @location(1) normal: vec3f,",
    "    @location(2) uv: vec2f,",
    "    @location(3) tangent: vec3f,",
    "    @location(4) bitangent: vec3f,",
    "    @location(5) instanceColor: vec4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "",
    "@vertex fn vertexMain(in: VertexInput) -> VertexOutput {",
    "    var out: VertexOutput;",
    "    let model = mat4x4f(in.instanceMatrix0, in.instanceMatrix1, in.instanceMatrix2, in.instanceMatrix3);",
    "    let world = model * vec4f(in.position, 1.0);",
    "    out.worldPos = world.xyz;",
    "    out.normal = normalize((model * vec4f(in.normal, 0.0)).xyz);",
    "    out.uv = in.uv;",
    "    let T = normalize((model * vec4f(in.tangent.xyz, 0.0)).xyz);",
    "    let N = out.normal;",
    "    out.tangent = T;",
    "    out.bitangent = cross(N, T) * in.tangent.w;",
    "    out.instanceColor = in.instanceColor;",
    "    out.clipPos = frame.projMatrix * frame.viewMatrix * world;",
    "    return out;",
    "}",
  ].join("\n");

  // -----------------------------------------------------------------------
  // PBR Fragment Shader (WGSL)
  // -----------------------------------------------------------------------

  var WGSL_PBR_FRAGMENT = [
    WGSL_COMMON_CONSTANTS,
    "",
    WGSL_FRAME_STRUCTS,
    "",
    WGSL_MATERIAL_STRUCT,
    "",
    "struct VertexOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) worldPos: vec3f,",
    "    @location(1) normal: vec3f,",
    "    @location(2) uv: vec2f,",
    "    @location(3) tangent: vec3f,",
    "    @location(4) bitangent: vec3f,",
    "    @location(5) instanceColor: vec4f,",
    "};",
    "",
    "// Group 0: per-frame",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "@group(0) @binding(1) var<storage, read> lights: array<Light>;",
    "@group(0) @binding(2) var<uniform> fog: FogUniforms;",
    "@group(0) @binding(3) var<uniform> env: EnvUniforms;",
    "@group(0) @binding(4) var shadowMap0: texture_depth_2d;",
    "@group(0) @binding(5) var shadowSampler0: sampler_comparison;",
    "@group(0) @binding(6) var shadowMap1: texture_depth_2d;",
    "@group(0) @binding(7) var shadowSampler1: sampler_comparison;",
    "@group(0) @binding(8) var<uniform> shadow: ShadowUniforms;",
    "",
    "// Group 1: per-material",
    "@group(1) @binding(0) var<uniform> material: MaterialUniforms;",
    "@group(1) @binding(1) var albedoTex: texture_2d<f32>;",
    "@group(1) @binding(2) var albedoSamp: sampler;",
    "@group(1) @binding(3) var normalTex: texture_2d<f32>;",
    "@group(1) @binding(4) var normalSamp: sampler;",
    "@group(1) @binding(5) var roughnessTex: texture_2d<f32>;",
    "@group(1) @binding(6) var roughnessSamp: sampler;",
    "@group(1) @binding(7) var metalnessTex: texture_2d<f32>;",
    "@group(1) @binding(8) var metalnessSamp: sampler;",
    "@group(1) @binding(9) var emissiveTex: texture_2d<f32>;",
    "@group(1) @binding(10) var emissiveSamp: sampler;",
    "",
    "fn shadowProjectedCoords(worldPos: vec3f, lightSpaceMatrix: mat4x4f) -> vec3f {",
    "    let lightSpacePos = lightSpaceMatrix * vec4f(worldPos, 1.0);",
    "    let projCoords3 = lightSpacePos.xyz / lightSpacePos.w;",
    "    return projCoords3 * 0.5 + 0.5;",
    "}",
    "",
    "// 4-tap Poisson disk PCF shadow sampling for shadow slot 0.",
    "fn shadowFactor0(worldPos: vec3f, lightSpaceMatrix: mat4x4f, bias: f32) -> f32 {",
    "    let projCoords = shadowProjectedCoords(worldPos, lightSpaceMatrix);",
    "    let inside = projCoords.x >= 0.0 && projCoords.x <= 1.0 && projCoords.y >= 0.0 && projCoords.y <= 1.0 && projCoords.z >= 0.0 && projCoords.z <= 1.0;",
    "    let poissonDisk = array<vec2f, 4>(",
    "        vec2f(-0.94201624, -0.39906216),",
    "        vec2f(0.94558609, -0.76890725),",
    "        vec2f(-0.094184101, -0.92938870),",
    "        vec2f(0.34495938, 0.29387760),",
    "    );",
    "",
    "    var shadowVal: f32 = 0.0;",
    "    let texDim = textureDimensions(shadowMap0);",
    "    let texelSize = 1.0 / f32(texDim.x);",
    "",
    "    for (var i = 0u; i < 4u; i = i + 1u) {",
    "        let sampleUV = clamp(projCoords.xy + poissonDisk[i] * texelSize, vec2f(0.0), vec2f(1.0));",
    "        let refDepth = clamp(projCoords.z - bias, 0.0, 1.0);",
    "        shadowVal = shadowVal + textureSampleCompareLevel(shadowMap0, shadowSampler0, sampleUV, refDepth);",
    "    }",
    "    return select(1.0, shadowVal / 4.0, inside);",
    "}",
    "",
    "// 4-tap Poisson disk PCF shadow sampling for shadow slot 1.",
    "fn shadowFactor1(worldPos: vec3f, lightSpaceMatrix: mat4x4f, bias: f32) -> f32 {",
    "    let projCoords = shadowProjectedCoords(worldPos, lightSpaceMatrix);",
    "    let inside = projCoords.x >= 0.0 && projCoords.x <= 1.0 && projCoords.y >= 0.0 && projCoords.y <= 1.0 && projCoords.z >= 0.0 && projCoords.z <= 1.0;",
    "    let poissonDisk = array<vec2f, 4>(",
    "        vec2f(-0.94201624, -0.39906216),",
    "        vec2f(0.94558609, -0.76890725),",
    "        vec2f(-0.094184101, -0.92938870),",
    "        vec2f(0.34495938, 0.29387760),",
    "    );",
    "",
    "    var shadowVal: f32 = 0.0;",
    "    let texDim = textureDimensions(shadowMap1);",
    "    let texelSize = 1.0 / f32(texDim.x);",
    "",
    "    for (var i = 0u; i < 4u; i = i + 1u) {",
    "        let sampleUV = clamp(projCoords.xy + poissonDisk[i] * texelSize, vec2f(0.0), vec2f(1.0));",
    "        let refDepth = clamp(projCoords.z - bias, 0.0, 1.0);",
    "        shadowVal = shadowVal + textureSampleCompareLevel(shadowMap1, shadowSampler1, sampleUV, refDepth);",
    "    }",
    "    return select(1.0, shadowVal / 4.0, inside);",
    "}",
    "",
    "// GGX/Trowbridge-Reitz normal distribution function.",
    "fn distributionGGX(N: vec3f, H: vec3f, roughness: f32) -> f32 {",
    "    let a = roughness * roughness;",
    "    let a2 = a * a;",
    "    let NdotH = max(dot(N, H), 0.0);",
    "    let NdotH2 = NdotH * NdotH;",
    "    let denom = NdotH2 * (a2 - 1.0) + 1.0;",
    "    return a2 / max(PI * denom * denom, 0.0000001);",
    "}",
    "",
    "// Smith geometry function (GGX variant) -- single direction.",
    "fn geometrySchlickGGX(NdotV: f32, roughness: f32) -> f32 {",
    "    let r = roughness + 1.0;",
    "    let k = (r * r) / 8.0;",
    "    return NdotV / (NdotV * (1.0 - k) + k);",
    "}",
    "",
    "// Smith geometry function -- combined for view and light directions.",
    "fn geometrySmith(N: vec3f, V: vec3f, L: vec3f, roughness: f32) -> f32 {",
    "    let NdotV = max(dot(N, V), 0.0);",
    "    let NdotL = max(dot(N, L), 0.0);",
    "    return geometrySchlickGGX(NdotV, roughness) * geometrySchlickGGX(NdotL, roughness);",
    "}",
    "",
    "// Schlick fresnel approximation.",
    "fn fresnelSchlick(cosTheta: f32, F0: vec3f) -> vec3f {",
    "    return F0 + (1.0 - F0) * pow(clamp(1.0 - cosTheta, 0.0, 1.0), 5.0);",
    "}",
    "",
    "// Point light distance attenuation.",
    "fn pointLightAttenuation(distance: f32, range: f32, decay: f32) -> f32 {",
    "    if (range > 0.0) {",
    "        let ratio = clamp(1.0 - pow(distance / range, 4.0), 0.0, 1.0);",
    "        return ratio * ratio / max(distance * distance, 0.0001);",
    "    }",
    "    return 1.0 / max(pow(distance, decay), 0.0001);",
    "}",
    "",
    "@fragment fn fragmentMain(in: VertexOutput) -> @location(0) vec4f {",
    "    // Resolve material properties, sampling textures when available.",
    "    var albedo = material.albedo;",
    "    if (material.hasAlbedoMap != 0u) {",
    "        let texAlbedo = textureSample(albedoTex, albedoSamp, in.uv);",
    "        albedo = albedo * texAlbedo.rgb;",
    "    }",
    "    albedo = albedo * in.instanceColor.rgb;",
    "    let finalOpacity = material.opacity * clamp(in.instanceColor.a, 0.0, 1.0);",
    "",
    "    var roughness = material.roughness;",
    "    if (material.hasRoughnessMap != 0u) {",
    "        roughness = roughness * textureSample(roughnessTex, roughnessSamp, in.uv).g;",
    "    }",
    "    roughness = clamp(roughness, 0.04, 1.0);",
    "    roughness = clamp(roughness * (1.0 - abs(material.anisotropy) * 0.28), 0.04, 1.0);",
    "",
    "    var metalness = material.metalness;",
    "    if (material.hasMetalnessMap != 0u) {",
    "        metalness = metalness * textureSample(metalnessTex, metalnessSamp, in.uv).b;",
    "    }",
    "    metalness = clamp(metalness, 0.0, 1.0);",
    "",
    "    var emissiveStrength = material.emissive;",
    "    var emissiveColor = albedo;",
    "    if (material.hasEmissiveMap != 0u) {",
    "        emissiveColor = textureSample(emissiveTex, emissiveSamp, in.uv).rgb;",
    "    }",
    "",
    "    // Unlit path: output albedo directly.",
    "    if (material.unlit != 0u) {",
    "        let color = albedo + emissiveColor * emissiveStrength;",
    "        return vec4f(color, finalOpacity);",
    "    }",
    "",
    "    // Resolve per-pixel normal via TBN matrix.",
    "    var N = normalize(in.normal);",
    "    if (material.hasNormalMap != 0u) {",
    "        let T = normalize(in.tangent);",
    "        let B = normalize(in.bitangent);",
    "        let TBN = mat3x3f(T, B, N);",
    "        let mapNormal = textureSample(normalTex, normalSamp, in.uv).rgb * 2.0 - 1.0;",
    "        N = normalize(TBN * mapNormal);",
    "    }",
    "",
    "    let V = normalize(frame.cameraPos - in.worldPos);",
    "    let NoV = max(dot(N, V), 0.0);",
    "",
    "    // Fresnel reflectance at normal incidence.",
    "    let F0 = mix(vec3f(0.04), albedo, metalness);",
    "",
    "    // Accumulate direct lighting.",
    "    var Lo = vec3f(0.0);",
    "",
    "    let lightCount = min(frame.lightCount, MAX_LIGHTS);",
    "    for (var i = 0u; i < lightCount; i = i + 1u) {",
    "        let light = lights[i];",
    "        let lightType = u32(light.position.w);",
    "        let lightColor = light.color.rgb;",
    "        let intensity = light.direction.w;",
    "        let range = light.color.a;",
    "        let decay = light.params.x;",
    "",
    "        // Ambient light (type 0): add flat contribution, no BRDF.",
    "        if (lightType == 0u) {",
    "            Lo = Lo + albedo * lightColor * intensity;",
    "            continue;",
    "        }",
    "",
    "        var L: vec3f;",
    "        var attenuation: f32 = 1.0;",
    "",
    "        if (lightType == 1u) {",
    "            // Directional light.",
    "            L = normalize(-light.direction.xyz);",
    "        } else {",
    "            // Point light.",
    "            let toLight = light.position.xyz - in.worldPos;",
    "            let dist = length(toLight);",
    "            L = toLight / max(dist, 0.0001);",
    "            attenuation = pointLightAttenuation(dist, range, decay);",
    "        }",
    "",
    "        let H = normalize(V + L);",
    "        let NdotL = max(dot(N, L), 0.0);",
    "",
    "        // Cook-Torrance specular BRDF.",
    "        let D = distributionGGX(N, H, roughness);",
    "        let G = geometrySmith(N, V, L, roughness);",
    "        let F = fresnelSchlick(max(dot(H, V), 0.0), F0);",
    "",
    "        let numerator = D * G * F;",
    "        let denominator = 4.0 * NoV * NdotL + 0.0001;",
    "        let specular = numerator / denominator;",
    "",
    "        // Energy conservation: diffuse complement of specular.",
    "        let kD = (vec3f(1.0) - F) * (1.0 - metalness);",
    "",
    "        // Shadow attenuation for directional lights.",
    "        var shadowAtten: f32 = 1.0;",
    "        if (material.receiveShadow != 0u && lightType == 1u) {",
    "            if (shadow.hasShadow0 != 0u && i32(i) == shadow.shadowLightIndex0) {",
    "                shadowAtten = shadowFactor0(in.worldPos, shadow.lightSpaceMatrix0, shadow.shadowBias0);",
    "            } else if (shadow.hasShadow1 != 0u && i32(i) == shadow.shadowLightIndex1) {",
    "                shadowAtten = shadowFactor1(in.worldPos, shadow.lightSpaceMatrix1, shadow.shadowBias1);",
    "            }",
    "        }",
    "",
    "        let radiance = lightColor * intensity * attenuation;",
    "        Lo = Lo + (kD * albedo / PI + specular) * radiance * NdotL * shadowAtten;",
    "    }",
    "",
    "    // Environment hemisphere lighting.",
    "    let hemi = N.y * 0.5 + 0.5;",
    "    let envDiffuse = env.ambientColor * env.ambientIntensity",
    "                   + env.skyColor * env.skyIntensity * hemi",
    "                   + env.groundColor * env.groundIntensity * (1.0 - hemi);",
    "    let ambient = envDiffuse * albedo;",
    "",
    "    // Emissive contribution.",
    "    let emission = emissiveColor * emissiveStrength;",
    "",
    "    var color = ambient + Lo + emission;",
    "",
    "    let clearcoat = clamp(material.clearcoat, 0.0, 1.0);",
    "    if (clearcoat > 0.0001) {",
    "        let cc = pow(NoV, mix(12.0, 96.0, 1.0 - roughness)) * clearcoat;",
    "        color = color + vec3f(cc * 0.28);",
    "    }",
    "",
    "    let sheen = clamp(material.sheen, 0.0, 1.0);",
    "    if (sheen > 0.0001) {",
    "        let velvet = pow(1.0 - NoV, 3.0) * sheen;",
    "        color = color + albedo * velvet * 0.55;",
    "    }",
    "",
    "    let iridescence = clamp(material.iridescence, 0.0, 1.0);",
    "    if (iridescence > 0.0001) {",
    "        let iri = vec3f(0.5) + vec3f(0.5) * cos(vec3f(0.0, 2.1, 4.2) + vec3f(NoV * 8.0));",
    "        color = mix(color, color * (vec3f(0.65) + iri * 0.7), iridescence * pow(1.0 - NoV, 2.0));",
    "    }",
    "",
    "    let transmission = clamp(material.transmission, 0.0, 1.0) * (1.0 - metalness);",
    "    if (transmission > 0.0001) {",
    "        color = mix(color, ambient + albedo * 0.1, transmission * 0.55);",
    "    }",
    "",
    "    // Exponential fog.",
    "    if (fog.hasFog != 0u) {",
    "        let fogDist = length(in.worldPos - frame.cameraPos);",
    "        let fogFactor = exp(-fog.fogDensity * fog.fogDensity * fogDist * fogDist);",
    "        color = mix(fog.fogColor, color, clamp(fogFactor, 0.0, 1.0));",
    "    }",
    "",
    "    // Tone mapping (Reinhard) and gamma correction.",
    "    if (frame.toneMap != 0u) {",
    "        color = color / (color + vec3f(1.0));",
    "        color = pow(color, vec3f(1.0 / 2.2));",
    "    }",
    "",
    "    return vec4f(color, finalOpacity);",
    "}",
  ].join("\n");

  // -----------------------------------------------------------------------
  // Shadow Depth Shader (WGSL)
  // -----------------------------------------------------------------------

  var WGSL_SHADOW_VERTEX = [
    "struct ShadowFrameUniforms {",
    "    lightViewProjection: mat4x4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> shadowFrame: ShadowFrameUniforms;",
    "",
    "@vertex fn vertexMain(@location(0) position: vec3f) -> @builtin(position) vec4f {",
    "    return shadowFrame.lightViewProjection * vec4f(position, 1.0);",
    "}",
  ].join("\n");

  var WGSL_SHADOW_INSTANCED_VERTEX = [
    "struct ShadowFrameUniforms {",
    "    lightViewProjection: mat4x4f,",
    "};",
    "",
    "struct VertexInput {",
    "    @location(0) position: vec3f,",
    "    @location(4) instanceMatrix0: vec4f,",
    "    @location(5) instanceMatrix1: vec4f,",
    "    @location(6) instanceMatrix2: vec4f,",
    "    @location(7) instanceMatrix3: vec4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> shadowFrame: ShadowFrameUniforms;",
    "",
    "@vertex fn vertexMain(in: VertexInput) -> @builtin(position) vec4f {",
    "    let model = mat4x4f(in.instanceMatrix0, in.instanceMatrix1, in.instanceMatrix2, in.instanceMatrix3);",
    "    return shadowFrame.lightViewProjection * model * vec4f(in.position, 1.0);",
    "}",
  ].join("\n");

  // Shadow fragment shader is empty -- depth-only pass.
  var WGSL_SHADOW_FRAGMENT = [
    "@fragment fn fragmentMain() {}",
  ].join("\n");

  var WGSL_SCENE_COLOR_FRAGMENT = [
    "struct ColorOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) color: vec4f,",
    "    @location(1) material: vec3f,",
    "};",
    "",
    "@fragment fn fragmentMain(in: ColorOutput) -> @location(0) vec4f {",
    "    var color = in.color;",
    "    var rgb = color.rgb;",
    "    let kind = floor(in.material.x + 0.5);",
    "    let emissive = max(in.material.y, 0.0);",
    "    let tone = clamp(in.material.z, 0.0, 1.0);",
    "    if (kind > 3.5) {",
    "        rgb = rgb * mix(0.78, 1.0, tone);",
    "    } else if (kind > 2.5) {",
    "        rgb = rgb * (1.0 + emissive * 0.75);",
    "    } else if (kind > 1.5) {",
    "        rgb = mix(rgb, vec3f(0.92, 0.98, 1.0), 0.28 + tone * 0.16);",
    "        color.a = color.a * 0.84;",
    "    } else if (kind > 0.5) {",
    "        rgb = mix(rgb, vec3f(0.84, 0.94, 1.0), 0.18 + tone * 0.12);",
    "        color.a = color.a * 0.9;",
    "    } else {",
    "        rgb = rgb * mix(0.9, 1.0, tone);",
    "    }",
    "    return vec4f(clamp(rgb, vec3f(0.0), vec3f(1.0)), clamp(color.a, 0.0, 1.0));",
    "}",
  ].join("\n");

  var WGSL_SCENE_WORLD_COLOR_VERTEX = [
    WGSL_FRAME_STRUCTS,
    "",
    "struct ColorInput {",
    "    @location(0) position: vec3f,",
    "    @location(1) color: vec4f,",
    "    @location(2) material: vec3f,",
    "};",
    "",
    "struct ColorOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) color: vec4f,",
    "    @location(1) material: vec3f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "",
    "@vertex fn vertexMain(in: ColorInput) -> ColorOutput {",
    "    var out: ColorOutput;",
    "    out.clipPos = frame.projMatrix * frame.viewMatrix * vec4f(in.position, 1.0);",
    "    out.color = in.color;",
    "    out.material = in.material;",
    "    return out;",
    "}",
  ].join("\n");

  var WGSL_SCENE_CLIP_COLOR_VERTEX = [
    "struct ColorInput {",
    "    @location(0) position: vec3f,",
    "    @location(1) color: vec4f,",
    "    @location(2) material: vec3f,",
    "};",
    "",
    "struct ColorOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) color: vec4f,",
    "    @location(1) material: vec3f,",
    "};",
    "",
    "@vertex fn vertexMain(in: ColorInput) -> ColorOutput {",
    "    var out: ColorOutput;",
    "    out.clipPos = vec4f(in.position.xy, in.position.z, 1.0);",
    "    out.color = in.color;",
    "    out.material = in.material;",
    "    return out;",
    "}",
  ].join("\n");

  var WGSL_SURFACE_VERTEX = [
    WGSL_FRAME_STRUCTS,
    "",
    "struct SurfaceInput {",
    "    @location(0) position: vec3f,",
    "    @location(1) uv: vec2f,",
    "};",
    "",
    "struct SurfaceOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) uv: vec2f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "",
    "@vertex fn vertexMain(in: SurfaceInput) -> SurfaceOutput {",
    "    var out: SurfaceOutput;",
    "    out.clipPos = frame.projMatrix * frame.viewMatrix * vec4f(in.position, 1.0);",
    "    out.uv = in.uv;",
    "    return out;",
    "}",
  ].join("\n");

  var WGSL_SURFACE_FRAGMENT = [
    WGSL_MATERIAL_STRUCT,
    "",
    "struct SurfaceOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) uv: vec2f,",
    "};",
    "",
    "@group(1) @binding(0) var<uniform> material: MaterialUniforms;",
    "@group(1) @binding(1) var albedoTex: texture_2d<f32>;",
    "@group(1) @binding(2) var albedoSamp: sampler;",
    "",
    "@fragment fn fragmentMain(in: SurfaceOutput) -> @location(0) vec4f {",
    "    let sampleColor = textureSample(albedoTex, albedoSamp, in.uv);",
    "    var rgb = sampleColor.rgb * material.albedo;",
    "    rgb = rgb * (1.0 + max(material.emissive, 0.0) * 0.5);",
    "    return vec4f(clamp(rgb, vec3f(0.0), vec3f(1.0)), clamp(sampleColor.a * material.opacity, 0.0, 1.0));",
    "}",
  ].join("\n");

  var WGSL_THICK_LINE_VERTEX = [
    WGSL_FRAME_STRUCTS,
    "",
    "struct ThickLineInput {",
    "    @location(0) positionA: vec3f,",
    "    @location(1) positionB: vec3f,",
    "    @location(2) colorA: vec4f,",
    "    @location(3) colorB: vec4f,",
    "    @location(4) side: f32,",
    "    @location(5) endpoint: f32,",
    "    @location(6) width: f32,",
    "};",
    "",
    "struct ThickLineOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) color: vec4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "",
    "fn safeNDC(clip: vec4f) -> vec2f {",
    "    return clip.xy / max(clip.w, 0.0001);",
    "}",
    "",
    "@vertex fn vertexMain(in: ThickLineInput) -> ThickLineOutput {",
    "    var out: ThickLineOutput;",
    "    let clipA = frame.projMatrix * frame.viewMatrix * vec4f(in.positionA, 1.0);",
    "    let clipB = frame.projMatrix * frame.viewMatrix * vec4f(in.positionB, 1.0);",
    "    let base = mix(clipA, clipB, clamp(in.endpoint, 0.0, 1.0));",
    "    let viewport = max(vec2f(frame.viewportWidth, frame.viewportHeight), vec2f(1.0));",
    "    let screenA = safeNDC(clipA) * (viewport * 0.5);",
    "    let screenB = safeNDC(clipB) * (viewport * 0.5);",
    "    var dir = screenB - screenA;",
    "    let len = length(dir);",
    "    if (len < 0.0001) {",
    "        dir = vec2f(1.0, 0.0);",
    "    } else {",
    "        dir = dir / len;",
    "    }",
    "    let normal = vec2f(-dir.y, dir.x);",
    "    let pixelOffset = normal * (in.side * max(in.width, 1.0) * 0.5);",
    "    let ndcOffset = pixelOffset / max(viewport * 0.5, vec2f(0.0001));",
    "    out.clipPos = base + vec4f(ndcOffset * base.w, 0.0, 0.0);",
    "    out.color = mix(in.colorA, in.colorB, clamp(in.endpoint, 0.0, 1.0));",
    "    return out;",
    "}",
  ].join("\n");

  var WGSL_THICK_LINE_FRAGMENT = [
    "struct ThickLineOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) color: vec4f,",
    "};",
    "",
    "@fragment fn fragmentMain(in: ThickLineOutput) -> @location(0) vec4f {",
    "    return vec4f(clamp(in.color.rgb, vec3f(0.0), vec3f(1.0)), clamp(in.color.a, 0.0, 1.0));",
    "}",
  ].join("\n");

  // -----------------------------------------------------------------------
  // Points Vertex Shader (WGSL) -- instanced billboard quads
  // -----------------------------------------------------------------------

  var WGSL_POINTS_VERTEX = [
    WGSL_FRAME_STRUCTS,
    "",
    "struct ParticleInstance {",
    "    position: vec3f,",
    "    size: f32,",
    "    color: vec4f,",
    "};",
    "",
    "struct PointsUniforms {",
    "    modelMatrix: mat4x4f,",
    "    defaultColorAndSize: vec4f,",
    "    flags: vec4u,",
    "    params: vec4f,",
    "    fogColor: vec4f,",
    "};",
    "",
    "struct PointsOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) color: vec3f,",
    "    @location(1) fogFactor: f32,",
    "    @location(2) alpha: f32,",
    "    @location(3) pointCoord: vec2f,",
    "    @location(4) pointSize: f32,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "@group(2) @binding(0) var<uniform> points: PointsUniforms;",
    "@group(2) @binding(1) var<storage, read> particles: array<ParticleInstance>;",
    "",
    "// Unit quad: 6 vertices for 2 triangles.",
    "const quadPos = array<vec2f, 6>(",
    "    vec2f(-0.5, -0.5), vec2f(0.5, -0.5), vec2f(-0.5, 0.5),",
    "    vec2f(0.5, -0.5), vec2f(0.5, 0.5), vec2f(-0.5, 0.5),",
    ");",
    "",
    "@vertex fn vertexMain(",
    "    @builtin(vertex_index) vertexIndex: u32,",
    "    @builtin(instance_index) instanceIndex: u32,",
    ") -> PointsOutput {",
    "    let quad = quadPos[vertexIndex];",
    "    let p = particles[instanceIndex];",
    "",
    "    let worldPos = (points.modelMatrix * vec4f(p.position, 1.0)).xyz;",
    "    let viewPos = frame.viewMatrix * vec4f(worldPos, 1.0);",
    "",
    "    // Compute point size with optional attenuation.",
    "    var rawSize = p.size;",
    "    if (points.flags.y == 0u) { rawSize = points.defaultColorAndSize.w; }",
    "",
    "    var pixelSize: f32;",
    "    if (points.flags.z != 0u) {",
    "        pixelSize = max(rawSize * (frame.viewportHeight * 0.5) / max(-viewPos.z, 0.001), 1.0);",
    "    } else {",
    "        pixelSize = max(rawSize, 1.0);",
    "    }",
    "    let minPixelSize = max(points.fogColor.a, 0.0);",
    "    if (minPixelSize > 0.0) {",
    "        pixelSize = max(pixelSize, minPixelSize);",
    "    }",
    "    if (points.params.w > 0.0) {",
    "        pixelSize = min(pixelSize, points.params.w);",
    "    }",
    "",
    "    // Billboard: offset in clip space by quad * pixelSize.",
    "    let clipPos = frame.projMatrix * viewPos;",
    "    let ndcOffsetX = quad.x * pixelSize / frame.viewportWidth * clipPos.w * 2.0;",
    "    let ndcOffsetY = quad.y * pixelSize / frame.viewportHeight * clipPos.w * 2.0;",
    "",
    "    var out: PointsOutput;",
    "    out.clipPos = vec4f(clipPos.x + ndcOffsetX, clipPos.y + ndcOffsetY, clipPos.z, clipPos.w);",
    "",
    "    // Color.",
    "    if (points.flags.x != 0u) {",
    "        out.color = p.color.rgb;",
    "    } else {",
    "        out.color = points.defaultColorAndSize.rgb;",
    "    }",
    "    out.alpha = p.color.a * points.params.x;",
    "    out.pointCoord = quad + vec2f(0.5, 0.5);",
    "    out.pointSize = pixelSize;",
    "",
    "    // Fog.",
    "    if (points.params.y != 0.0) {",
    "        let dist = length(viewPos.xyz);",
    "        out.fogFactor = clamp(exp(-points.params.z * points.params.z * dist * dist), 0.0, 1.0);",
    "    } else {",
    "        out.fogFactor = 1.0;",
    "    }",
    "",
    "    return out;",
    "}",
  ].join("\n");

  var WGSL_POINTS_INSTANCED_VERTEX = [
    WGSL_FRAME_STRUCTS,
    "",
    "struct PointsUniforms {",
    "    modelMatrix: mat4x4f,",
    "    defaultColorAndSize: vec4f,",
    "    flags: vec4u,",
    "    params: vec4f,",
    "    fogColor: vec4f,",
    "};",
    "",
    "struct PointsInput {",
    "    @location(0) position: vec3f,",
    "    @location(1) size: f32,",
    "    @location(2) color: vec4f,",
    "};",
    "",
    "struct PointsOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) color: vec3f,",
    "    @location(1) fogFactor: f32,",
    "    @location(2) alpha: f32,",
    "    @location(3) pointCoord: vec2f,",
    "    @location(4) pointSize: f32,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "@group(2) @binding(0) var<uniform> points: PointsUniforms;",
    "",
    "// Unit quad: 6 vertices for 2 triangles.",
    "const quadPos = array<vec2f, 6>(",
    "    vec2f(-0.5, -0.5), vec2f(0.5, -0.5), vec2f(-0.5, 0.5),",
    "    vec2f(0.5, -0.5), vec2f(0.5, 0.5), vec2f(-0.5, 0.5),",
    ");",
    "",
    "@vertex fn vertexMain(",
    "    @builtin(vertex_index) vertexIndex: u32,",
    "    in: PointsInput,",
    ") -> PointsOutput {",
    "    let quad = quadPos[vertexIndex];",
    "",
    "    let worldPos = (points.modelMatrix * vec4f(in.position, 1.0)).xyz;",
    "    let viewPos = frame.viewMatrix * vec4f(worldPos, 1.0);",
    "",
    "    // Compute point size with optional attenuation.",
    "    var rawSize = in.size;",
    "    if (points.flags.y == 0u) { rawSize = points.defaultColorAndSize.w; }",
    "",
    "    var pixelSize: f32;",
    "    if (points.flags.z != 0u) {",
    "        pixelSize = max(rawSize * (frame.viewportHeight * 0.5) / max(-viewPos.z, 0.001), 1.0);",
    "    } else {",
    "        pixelSize = max(rawSize, 1.0);",
    "    }",
    "    let minPixelSize = max(points.fogColor.a, 0.0);",
    "    if (minPixelSize > 0.0) {",
    "        pixelSize = max(pixelSize, minPixelSize);",
    "    }",
    "    if (points.params.w > 0.0) {",
    "        pixelSize = min(pixelSize, points.params.w);",
    "    }",
    "",
    "    // Billboard: offset in clip space by quad * pixelSize.",
    "    let clipPos = frame.projMatrix * viewPos;",
    "    let ndcOffsetX = quad.x * pixelSize / frame.viewportWidth * clipPos.w * 2.0;",
    "    let ndcOffsetY = quad.y * pixelSize / frame.viewportHeight * clipPos.w * 2.0;",
    "",
    "    var out: PointsOutput;",
    "    out.clipPos = vec4f(clipPos.x + ndcOffsetX, clipPos.y + ndcOffsetY, clipPos.z, clipPos.w);",
    "",
    "    // Color.",
    "    if (points.flags.x != 0u) {",
    "        out.color = in.color.rgb;",
    "    } else {",
    "        out.color = points.defaultColorAndSize.rgb;",
    "    }",
    "    out.alpha = in.color.a * points.params.x;",
    "    out.pointCoord = quad + vec2f(0.5, 0.5);",
    "    out.pointSize = pixelSize;",
    "",
    "    // Fog.",
    "    if (points.params.y != 0.0) {",
    "        let dist = length(viewPos.xyz);",
    "        out.fogFactor = clamp(exp(-points.params.z * points.params.z * dist * dist), 0.0, 1.0);",
    "    } else {",
    "        out.fogFactor = 1.0;",
    "    }",
    "",
    "    return out;",
    "}",
  ].join("\n");

  // -----------------------------------------------------------------------
  // Points Fragment Shader (WGSL)
  // -----------------------------------------------------------------------

  var WGSL_POINTS_FRAGMENT = [
    "struct PointsUniforms {",
    "    modelMatrix: mat4x4f,",
    "    defaultColorAndSize: vec4f,",
    "    flags: vec4u,",
    "    params: vec4f,",
    "    fogColor: vec4f,",
    "};",
    "",
    "@group(2) @binding(0) var<uniform> points: PointsUniforms;",
    "",
    "struct PointsInput {",
    "    @location(0) color: vec3f,",
    "    @location(1) fogFactor: f32,",
    "    @location(2) alpha: f32,",
    "    @location(3) pointCoord: vec2f,",
    "    @location(4) pointSize: f32,",
    "};",
    "",
    "@fragment fn fragmentMain(in: PointsInput) -> @location(0) vec4f {",
    "    var color = in.color;",
    "    var alpha = in.alpha;",
    "    if (points.flags.w == 1u) {",
    "        let centered = in.pointCoord - vec2f(0.5, 0.5);",
    "        let radial = length(centered);",
    "        let square = max(abs(centered.x), abs(centered.y));",
    "        let focus = clamp((in.pointSize - 1.0) / 10.0, 0.0, 1.0);",
    "        let coreRadius = mix(0.49, 0.18, focus);",
    "        let core = 1.0 - smoothstep(coreRadius, coreRadius + 0.05, square);",
    "        let halo = (1.0 - smoothstep(0.12, 0.72, radial)) * focus;",
    "        let streakX = 1.0 - smoothstep(0.02, 0.16, abs(centered.x));",
    "        let streakY = 1.0 - smoothstep(0.02, 0.16, abs(centered.y));",
    "        let streak = max(streakX, streakY) * focus;",
    "        alpha = clamp(core + halo * 0.5 + streak * 0.2, 0.0, 1.0) * in.alpha;",
    "        color = mix(color, vec3f(1.0, 1.0, 1.0), clamp(focus * 0.22 + core * focus * 0.28, 0.0, 0.4));",
    "    } else if (points.flags.w == 2u) {",
    "        let centered = in.pointCoord - vec2f(0.5, 0.5);",
    "        let radial = length(centered) * 2.0;",
    "        if (radial > 1.0) {",
    "            discard;",
    "        }",
    "        let sizeFocus = clamp((in.pointSize - 4.0) / 48.0, 0.0, 1.0);",
    "        let falloff = mix(4.2, 3.2, sizeFocus);",
    "        let core = exp(-radial * radial * falloff);",
    "        let edgeFeather = 1.0 - smoothstep(0.78, 1.0, radial);",
    "        alpha = core * edgeFeather * in.alpha;",
    "    }",
    "    if (alpha <= 0.003) {",
    "        discard;",
    "    }",
    "    if (points.params.y != 0.0) {",
    "        color = mix(points.fogColor.rgb, color, in.fogFactor);",
    "    }",
    "    return vec4f(color, alpha);",
    "}",
  ].join("\n");

  // -----------------------------------------------------------------------
  // Post-processing shaders (WGSL)
  // -----------------------------------------------------------------------

  var WGSL_POST_VERTEX = [
    "struct VertexOutput {",
    "    @builtin(position) position: vec4f,",
    "    @location(0) uv: vec2f,",
    "};",
    "",
    "const positions = array<vec2f, 4>(",
    "    vec2f(-1.0, -1.0),",
    "    vec2f( 1.0, -1.0),",
    "    vec2f(-1.0,  1.0),",
    "    vec2f( 1.0,  1.0),",
    ");",
    "const uvs = array<vec2f, 4>(",
    "    vec2f(0.0, 1.0),",
    "    vec2f(1.0, 1.0),",
    "    vec2f(0.0, 0.0),",
    "    vec2f(1.0, 0.0),",
    ");",
    "",
    "@vertex fn vertexMain(@builtin(vertex_index) vi: u32) -> VertexOutput {",
    "    var out: VertexOutput;",
    "    out.position = vec4f(positions[vi], 0.0, 1.0);",
    "    out.uv = uvs[vi];",
    "    return out;",
    "}",
  ].join("\n");

  var WGSL_POST_BLIT_FRAGMENT = [
    "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
    "@group(0) @binding(1) var inputSamp: sampler;",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    return textureSample(inputTex, inputSamp, uv);",
    "}",
  ].join("\n");

  var WGSL_POST_TONEMAPPING_FRAGMENT = [
    "struct ToneMappingParams {",
    "    exposure: f32,",
    "    toneMapMode: f32,",
    "    _pad1: f32,",
    "    _pad2: f32,",
    "};",
    "",
    "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
    "@group(0) @binding(1) var inputSamp: sampler;",
    "@group(0) @binding(2) var<uniform> params: ToneMappingParams;",
    "",
    "fn aces(x: vec3f) -> vec3f {",
    "    let a = 2.51;",
    "    let b = 0.03;",
    "    let c = 2.43;",
    "    let d = 0.59;",
    "    let e = 0.14;",
    "    return clamp((x * (a * x + b)) / (x * (c * x + d) + e), vec3f(0.0), vec3f(1.0));",
    "}",
    "",
    "fn reinhard(x: vec3f) -> vec3f {",
    "    return x / (x + vec3f(1.0));",
    "}",
    "",
    "fn filmic(x: vec3f) -> vec3f {",
    "    let y = max(vec3f(0.0), x - vec3f(0.004));",
    "    return clamp((y * (6.2 * y + vec3f(0.5))) / (y * (6.2 * y + vec3f(1.7)) + vec3f(0.06)), vec3f(0.0), vec3f(1.0));",
    "}",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    var color = textureSample(inputTex, inputSamp, uv).rgb;",
    "    color = color * params.exposure;",
    "    let mode = i32(params.toneMapMode);",
    "    if (mode == 0) {",
    "        color = clamp(color, vec3f(0.0), vec3f(1.0));",
    "    } else if (mode == 2) {",
    "        color = reinhard(color);",
    "    } else if (mode == 3) {",
    "        color = filmic(color);",
    "    } else {",
    "        color = aces(color);",
    "    }",
    "    return vec4f(color, 1.0);",
    "}",
  ].join("\n");

  function sceneWebGPUToneMapMode(mode) {
    if (typeof mode === "string") {
      var normalized = mode.trim().toLowerCase();
      if (normalized === "linear" || normalized === "none") return 0;
      if (normalized === "reinhard") return 2;
      if (normalized === "filmic") return 3;
    }
    return 1;
  }

  var WGSL_POST_BLOOM_BRIGHT_FRAGMENT = [
    "struct BloomBrightParams {",
    "    threshold: f32,",
    "    _pad0: f32,",
    "    _pad1: f32,",
    "    _pad2: f32,",
    "};",
    "",
    "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
    "@group(0) @binding(1) var inputSamp: sampler;",
    "@group(0) @binding(2) var<uniform> params: BloomBrightParams;",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    let color = textureSample(inputTex, inputSamp, uv).rgb;",
    "    let brightness = dot(color, vec3f(0.2126, 0.7152, 0.0722));",
    "    if (brightness > params.threshold) {",
    "        return vec4f(color, 1.0);",
    "    }",
    "    return vec4f(0.0, 0.0, 0.0, 1.0);",
    "}",
  ].join("\n");

  var WGSL_POST_BLUR_FRAGMENT = [
    "struct BlurParams {",
    "    direction: vec2f,",
    "    radius: f32,",
    "    _pad0: f32,",
    "};",
    "",
    "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
    "@group(0) @binding(1) var inputSamp: sampler;",
    "@group(0) @binding(2) var<uniform> params: BlurParams;",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    let texDim = vec2f(textureDimensions(inputTex));",
    "    let texelSize = 1.0 / texDim;",
    "    var result = textureSample(inputTex, inputSamp, uv).rgb * 0.227027;",
    "",
    "    let offsets = array<f32, 4>(1.0, 2.0, 3.0, 4.0);",
    "    let weights = array<f32, 4>(0.1945946, 0.1216216, 0.054054, 0.016216);",
    "    let radiusStep = clamp(params.radius * 0.35, 1.0, 4.0);",
    "",
    "    for (var i = 0u; i < 4u; i = i + 1u) {",
    "        let offset = params.direction * texelSize * offsets[i] * radiusStep;",
    "        result = result + textureSample(inputTex, inputSamp, uv + offset).rgb * weights[i];",
    "        result = result + textureSample(inputTex, inputSamp, uv - offset).rgb * weights[i];",
    "    }",
    "    return vec4f(result, 1.0);",
    "}",
  ].join("\n");

  var WGSL_POST_BLOOM_COMPOSITE_FRAGMENT = [
    "struct BloomCompositeParams {",
    "    intensity: f32,",
    "    _pad0: f32,",
    "    _pad1: f32,",
    "    _pad2: f32,",
    "};",
    "",
    "@group(0) @binding(0) var sceneTex: texture_2d<f32>;",
    "@group(0) @binding(1) var sceneSamp: sampler;",
    "@group(0) @binding(2) var bloomTex: texture_2d<f32>;",
    "@group(0) @binding(3) var bloomSamp: sampler;",
    "@group(0) @binding(4) var<uniform> params: BloomCompositeParams;",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    let scene = textureSample(sceneTex, sceneSamp, uv).rgb;",
    "    let bloom = textureSample(bloomTex, bloomSamp, uv).rgb;",
    "    return vec4f(scene + bloom * params.intensity, 1.0);",
    "}",
  ].join("\n");

  var WGSL_POST_VIGNETTE_FRAGMENT = [
    "struct VignetteParams {",
    "    intensity: f32,",
    "    _pad0: f32,",
    "    _pad1: f32,",
    "    _pad2: f32,",
    "};",
    "",
    "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
    "@group(0) @binding(1) var inputSamp: sampler;",
    "@group(0) @binding(2) var<uniform> params: VignetteParams;",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    let color = textureSample(inputTex, inputSamp, uv).rgb;",
    "    let center = uv - 0.5;",
    "    let dist = length(center);",
    "    let vignette = 1.0 - smoothstep(0.3, 0.7, dist * params.intensity);",
    "    return vec4f(color * vignette, 1.0);",
    "}",
  ].join("\n");

  var WGSL_POST_COLORGRADE_FRAGMENT = [
    "struct ColorGradeParams {",
    "    exposure: f32,",
    "    contrast: f32,",
    "    saturation: f32,",
    "    _pad0: f32,",
    "};",
    "",
    "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
    "@group(0) @binding(1) var inputSamp: sampler;",
    "@group(0) @binding(2) var<uniform> params: ColorGradeParams;",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    var color = textureSample(inputTex, inputSamp, uv).rgb;",
    "    color = color * params.exposure;",
    "    color = mix(vec3f(0.5), color, params.contrast);",
    "    let gray = dot(color, vec3f(0.2126, 0.7152, 0.0722));",
    "    color = mix(vec3f(gray), color, params.saturation);",
    "    return vec4f(clamp(color, vec3f(0.0), vec3f(1.0)), 1.0);",
    "}",
  ].join("\n");

  var WGSL_POST_SSAO_FRAGMENT = [
    "struct SSAOParams {",
    "    radius: f32,",
    "    intensity: f32,",
    "    bias: f32,",
    "    _pad0: f32,",
    "    texelSize: vec2f,",
    "    _pad1: vec2f,",
    "};",
    "",
    "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
    "@group(0) @binding(1) var inputSamp: sampler;",
    "@group(0) @binding(2) var depthTex: texture_depth_2d;",
    "@group(0) @binding(3) var<uniform> params: SSAOParams;",
    "",
    "fn depthAt(uv: vec2f) -> f32 {",
    "    let dims = vec2f(textureDimensions(depthTex));",
    "    let p = vec2i(clamp(uv * dims, vec2f(0.0), dims - vec2f(1.0)));",
    "    return textureLoad(depthTex, p, 0);",
    "}",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    let color = textureSample(inputTex, inputSamp, uv).rgb;",
    "    let centerDepth = depthAt(uv);",
    "    if (centerDepth >= 0.9999) {",
    "        return vec4f(color, 1.0);",
    "    }",
    "    let offsets = array<vec2f, 8>(",
    "        vec2f(1.0, 0.0), vec2f(-1.0, 0.0), vec2f(0.0, 1.0), vec2f(0.0, -1.0),",
    "        vec2f(0.707, 0.707), vec2f(-0.707, 0.707), vec2f(0.707, -0.707), vec2f(-0.707, -0.707)",
    "    );",
    "    let radius = clamp(params.radius, 1.0, 64.0);",
    "    var occlusion = 0.0;",
    "    for (var i = 0u; i < 8u; i = i + 1u) {",
    "        let sampleDepth = depthAt(uv + offsets[i] * params.texelSize * radius);",
    "        let delta = centerDepth - sampleDepth;",
    "        let range = 1.0 - smoothstep(0.0, 0.035 * radius, abs(delta));",
    "        if (delta > max(params.bias, 0.0001)) {",
    "            occlusion = occlusion + range;",
    "        }",
    "    }",
    "    let ao = 1.0 - clamp((occlusion / 8.0) * clamp(params.intensity, 0.0, 2.0), 0.0, 0.92);",
    "    return vec4f(color * ao, 1.0);",
    "}",
  ].join("\n");

  var WGSL_POST_DOF_FRAGMENT = [
    "struct DOFParams {",
    "    focusDepth: f32,",
    "    aperture: f32,",
    "    maxBlur: f32,",
    "    _pad0: f32,",
    "    texelSize: vec2f,",
    "    _pad1: vec2f,",
    "};",
    "",
    "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
    "@group(0) @binding(1) var inputSamp: sampler;",
    "@group(0) @binding(2) var depthTex: texture_depth_2d;",
    "@group(0) @binding(3) var<uniform> params: DOFParams;",
    "",
    "fn depthAt(uv: vec2f) -> f32 {",
    "    let dims = vec2f(textureDimensions(depthTex));",
    "    let p = vec2i(clamp(uv * dims, vec2f(0.0), dims - vec2f(1.0)));",
    "    return textureLoad(depthTex, p, 0);",
    "}",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    let center = textureSample(inputTex, inputSamp, uv).rgb;",
    "    let depth = depthAt(uv);",
    "    let coc = clamp(abs(depth - params.focusDepth) * max(params.aperture, 0.0) * 80.0, 0.0, 1.0);",
    "    let radius = clamp(params.maxBlur, 0.0, 48.0) * coc;",
    "    let offsets = array<vec2f, 8>(",
    "        vec2f(1.0, 0.0), vec2f(-1.0, 0.0), vec2f(0.0, 1.0), vec2f(0.0, -1.0),",
    "        vec2f(0.707, 0.707), vec2f(-0.707, 0.707), vec2f(0.707, -0.707), vec2f(-0.707, -0.707)",
    "    );",
    "    var blur = center * 0.28;",
    "    for (var i = 0u; i < 8u; i = i + 1u) {",
    "        blur = blur + textureSample(inputTex, inputSamp, uv + offsets[i] * params.texelSize * radius).rgb * 0.09;",
    "    }",
    "    return vec4f(mix(center, blur, coc), 1.0);",
    "}",
  ].join("\n");

  // -----------------------------------------------------------------------
  // Buffer / Uniform Helpers
  // -----------------------------------------------------------------------

  // Align a byte count up to the specified alignment (typically 256 for uniform buffers).
  function wgpuAlignUp(size, alignment) {
    return Math.ceil(size / alignment) * alignment;
  }

  // Create a GPU buffer with the given usage flags and initial data (or size).
  function wgpuCreateBuffer(device, usage, dataOrSize) {
    var size;
    var mappedAtCreation = false;
    if (typeof dataOrSize === "number") {
      size = wgpuAlignUp(Math.max(dataOrSize, 4), 4);
    } else {
      size = wgpuAlignUp(Math.max(dataOrSize.byteLength, 4), 4);
      mappedAtCreation = true;
    }
    var buffer = device.createBuffer({
      size: size,
      usage: usage,
      mappedAtCreation: mappedAtCreation,
    });
    if (mappedAtCreation) {
      new dataOrSize.constructor(buffer.getMappedRange()).set(dataOrSize);
      buffer.unmap();
    }
    return buffer;
  }

  // Write data into an existing buffer. If the buffer is too small, recreate it.
  function wgpuEnsureBufferData(device, existingBuffer, usage, data) {
    var needed = wgpuAlignUp(Math.max(data.byteLength, 4), 4);
    if (existingBuffer && existingBuffer.size >= needed) {
      device.queue.writeBuffer(existingBuffer, 0, data);
      return existingBuffer;
    }
    if (existingBuffer) existingBuffer.destroy();
    return wgpuCreateBuffer(device, usage, data);
  }

  // -----------------------------------------------------------------------
  // Pipeline Cache
  // -----------------------------------------------------------------------

  // Build a cache key from pipeline configuration parameters.
  function wgpuPipelineKey(shaderVariant, blendMode, depthWrite, targetFormat, depthFormat, sampleCount) {
    return shaderVariant + "|" + blendMode + "|" + (depthWrite ? "1" : "0") + "|" + targetFormat + "|" + (depthFormat || "") + "|" + Math.max(1, Math.floor(sampleCount || 1));
  }

  // -----------------------------------------------------------------------
  // Texture Management
  // -----------------------------------------------------------------------

  function wgpuLoadTexture(device, url, cache) {
    if (!cache) return null;
    var key = typeof url === "string" ? url.trim() : "";
    if (!key) return null;
    if (cache.has(key)) return cache.get(key);

    // Placeholder: 1x1 white pixel.
    var placeholderTex = device.createTexture({
      size: [1, 1, 1],
      format: "rgba8unorm",
      usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST | GPUTextureUsage.RENDER_ATTACHMENT,
    });
    device.queue.writeTexture(
      { texture: placeholderTex },
      new Uint8Array([255, 255, 255, 255]),
      { bytesPerRow: 4 },
      [1, 1, 1]
    );

    var record = { texture: placeholderTex, view: placeholderTex.createView(), src: key, loaded: false, failed: false };
    cache.set(key, record);

    if (typeof Image === "function") {
      var image = new Image();
      image.onload = function() {
        var w = image.width;
        var h = image.height;
        var tex = device.createTexture({
          size: [w, h, 1],
          format: "rgba8unorm",
          usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST | GPUTextureUsage.RENDER_ATTACHMENT,
        });
        // Use createImageBitmap for copyExternalImageToTexture.
        if (typeof createImageBitmap === "function") {
          createImageBitmap(image).then(function(bitmap) {
            device.queue.copyExternalImageToTexture(
              { source: bitmap },
              { texture: tex },
              [w, h]
            );
            record.texture.destroy();
            record.texture = tex;
            record.view = tex.createView();
            record.loaded = true;
          }).catch(function() {
            record.failed = true;
          });
        } else {
          record.failed = true;
        }
      };
      image.onerror = function() {
        record.failed = true;
      };
      image.crossOrigin = "anonymous";
      image.src = key;
    }

    return record;
  }

  // -----------------------------------------------------------------------
  // Bind Group Layout Definitions
  // -----------------------------------------------------------------------

  function wgpuCreateFrameBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-frame",
      entries: [
        { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
        { binding: 1, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "read-only-storage" } },
        { binding: 2, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
        { binding: 3, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
        { binding: 4, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "depth" } },
        { binding: 5, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "comparison" } },
        { binding: 6, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "depth" } },
        { binding: 7, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "comparison" } },
        { binding: 8, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
      ],
    });
  }

  function wgpuCreateMaterialBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-material",
      entries: [
        { binding: 0, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
        { binding: 1, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
        { binding: 2, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
        { binding: 3, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
        { binding: 4, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
        { binding: 5, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
        { binding: 6, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
        { binding: 7, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
        { binding: 8, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
        { binding: 9, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
        { binding: 10, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
      ],
    });
  }

  function wgpuCreatePointsBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-points",
      entries: [
        { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
        { binding: 1, visibility: GPUShaderStage.VERTEX, buffer: { type: "read-only-storage" } },
      ],
    });
  }

  function wgpuCreatePointsUniformBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-points-uniform",
      entries: [
        { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
      ],
    });
  }

  function wgpuCreateShadowBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-shadow-frame",
      entries: [
        { binding: 0, visibility: GPUShaderStage.VERTEX, buffer: { type: "uniform" } },
      ],
    });
  }

  function wgpuCreatePostBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-post",
      entries: [
        { binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
        { binding: 1, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
      ],
    });
  }

  function wgpuCreatePostWithParamsBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-post-params",
      entries: [
        { binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
        { binding: 1, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
        { binding: 2, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
      ],
    });
  }

  function wgpuCreateBloomCompositeBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-bloom-composite",
      entries: [
        { binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
        { binding: 1, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
        { binding: 2, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
        { binding: 3, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
        { binding: 4, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
      ],
    });
  }

  function wgpuCreateSSAOBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-ssao",
      entries: [
        { binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
        { binding: 1, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
        { binding: 2, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "depth" } },
        { binding: 3, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
      ],
    });
  }

  // -----------------------------------------------------------------------
  // Pipeline Creation
  // -----------------------------------------------------------------------

  // PBR vertex buffer layout (position, normal, uv, tangent).
  var WGPU_PBR_VERTEX_LAYOUT = [
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 0 }] },
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 1 }] },
    { arrayStride: 8,  stepMode: "vertex", attributes: [{ format: "float32x2", offset: 0, shaderLocation: 2 }] },
    { arrayStride: 16, stepMode: "vertex", attributes: [{ format: "float32x4", offset: 0, shaderLocation: 3 }] },
  ];

  var WGPU_PBR_INSTANCED_VERTEX_LAYOUT = WGPU_PBR_VERTEX_LAYOUT.concat([
    {
      arrayStride: 64,
      stepMode: "instance",
      attributes: [
        { format: "float32x4", offset: 0,  shaderLocation: 4 },
        { format: "float32x4", offset: 16, shaderLocation: 5 },
        { format: "float32x4", offset: 32, shaderLocation: 6 },
        { format: "float32x4", offset: 48, shaderLocation: 7 },
      ],
    },
    { arrayStride: 16, stepMode: "instance", attributes: [{ format: "float32x4", offset: 0, shaderLocation: 8 }] },
  ]);

  // Shadow vertex buffer layout (position only).
  var WGPU_SHADOW_VERTEX_LAYOUT = [
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 0 }] },
  ];

  var WGPU_SHADOW_INSTANCED_VERTEX_LAYOUT = WGPU_SHADOW_VERTEX_LAYOUT.concat([
    {
      arrayStride: 64,
      stepMode: "instance",
      attributes: [
        { format: "float32x4", offset: 0,  shaderLocation: 4 },
        { format: "float32x4", offset: 16, shaderLocation: 5 },
        { format: "float32x4", offset: 32, shaderLocation: 6 },
        { format: "float32x4", offset: 48, shaderLocation: 7 },
      ],
    },
  ]);

  var WGPU_SCENE_COLOR_VERTEX_LAYOUT = [
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 0 }] },
    { arrayStride: 16, stepMode: "vertex", attributes: [{ format: "float32x4", offset: 0, shaderLocation: 1 }] },
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 2 }] },
  ];

  var WGPU_SURFACE_VERTEX_LAYOUT = [
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 0 }] },
    { arrayStride: 8, stepMode: "vertex", attributes: [{ format: "float32x2", offset: 0, shaderLocation: 1 }] },
  ];

  var WGPU_THICK_LINE_VERTEX_LAYOUT = [
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 0 }] },
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 1 }] },
    { arrayStride: 16, stepMode: "vertex", attributes: [{ format: "float32x4", offset: 0, shaderLocation: 2 }] },
    { arrayStride: 16, stepMode: "vertex", attributes: [{ format: "float32x4", offset: 0, shaderLocation: 3 }] },
    { arrayStride: 4, stepMode: "vertex", attributes: [{ format: "float32", offset: 0, shaderLocation: 4 }] },
    { arrayStride: 4, stepMode: "vertex", attributes: [{ format: "float32", offset: 0, shaderLocation: 5 }] },
    { arrayStride: 4, stepMode: "vertex", attributes: [{ format: "float32", offset: 0, shaderLocation: 6 }] },
  ];

  var WGPU_POINTS_INSTANCE_VERTEX_LAYOUT = [
    {
      arrayStride: 32,
      stepMode: "instance",
      attributes: [
        { format: "float32x3", offset: 0, shaderLocation: 0 },
        { format: "float32", offset: 12, shaderLocation: 1 },
        { format: "float32x4", offset: 16, shaderLocation: 2 },
      ],
    },
  ];

  function wgpuBlendState(mode) {
    if (mode === "alpha") {
      return {
        color: { srcFactor: "src-alpha", dstFactor: "one-minus-src-alpha", operation: "add" },
        alpha: { srcFactor: "one", dstFactor: "one-minus-src-alpha", operation: "add" },
      };
    }
    if (mode === "additive") {
      return {
        color: { srcFactor: "src-alpha", dstFactor: "one", operation: "add" },
        alpha: { srcFactor: "one", dstFactor: "one", operation: "add" },
      };
    }
    return undefined; // opaque -- no blending
  }

  function wgpuCreatePBRPipeline(device, pipelineLayout, vertexModule, fragmentModule, blendMode, depthWrite, targetFormat, sampleCount) {
    return device.createRenderPipeline({
      label: "gosx-pbr-" + blendMode,
      layout: pipelineLayout,
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: WGPU_PBR_VERTEX_LAYOUT,
      },
      fragment: {
        module: fragmentModule,
        entryPoint: "fragmentMain",
        targets: [{
          format: targetFormat,
          blend: wgpuBlendState(blendMode),
        }],
      },
      primitive: { topology: "triangle-list", cullMode: "none" },
      multisample: { count: Math.max(1, Math.floor(sampleCount || 1)) },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: depthWrite,
        depthCompare: "less-equal",
      },
    });
  }

  function wgpuCreatePBRInstancedPipeline(device, pipelineLayout, vertexModule, fragmentModule, blendMode, depthWrite, targetFormat, sampleCount) {
    return device.createRenderPipeline({
      label: "gosx-pbr-instanced-" + blendMode,
      layout: pipelineLayout,
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: WGPU_PBR_INSTANCED_VERTEX_LAYOUT,
      },
      fragment: {
        module: fragmentModule,
        entryPoint: "fragmentMain",
        targets: [{
          format: targetFormat,
          blend: wgpuBlendState(blendMode),
        }],
      },
      primitive: { topology: "triangle-list", cullMode: "none" },
      multisample: { count: Math.max(1, Math.floor(sampleCount || 1)) },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: depthWrite,
        depthCompare: "less-equal",
      },
    });
  }

  function wgpuCreateShadowPipeline(device, shadowLayout, vertexModule) {
    return device.createRenderPipeline({
      label: "gosx-shadow",
      layout: device.createPipelineLayout({ bindGroupLayouts: [shadowLayout] }),
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: WGPU_SHADOW_VERTEX_LAYOUT,
      },
      primitive: { topology: "triangle-list", cullMode: "front" },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: true,
        depthCompare: "less-equal",
      },
    });
  }

  function wgpuCreateShadowInstancedPipeline(device, shadowLayout, vertexModule) {
    return device.createRenderPipeline({
      label: "gosx-shadow-instanced",
      layout: device.createPipelineLayout({ bindGroupLayouts: [shadowLayout] }),
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: WGPU_SHADOW_INSTANCED_VERTEX_LAYOUT,
      },
      primitive: { topology: "triangle-list", cullMode: "front" },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: true,
        depthCompare: "less-equal",
      },
    });
  }

  function wgpuCreateSceneColorPipeline(device, pipelineLayout, vertexModule, fragmentModule, topology, blendMode, depthWrite, targetFormat, sampleCount) {
    return device.createRenderPipeline({
      label: "gosx-scene-color-" + topology + "-" + blendMode,
      layout: pipelineLayout,
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: WGPU_SCENE_COLOR_VERTEX_LAYOUT,
      },
      fragment: {
        module: fragmentModule,
        entryPoint: "fragmentMain",
        targets: [{
          format: targetFormat,
          blend: wgpuBlendState(blendMode),
        }],
      },
      primitive: { topology: topology },
      multisample: { count: Math.max(1, Math.floor(sampleCount || 1)) },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: depthWrite,
        depthCompare: "less-equal",
      },
    });
  }

  function wgpuCreateSurfacePipeline(device, pipelineLayout, vertexModule, fragmentModule, blendMode, depthWrite, targetFormat, sampleCount) {
    return device.createRenderPipeline({
      label: "gosx-surface-" + blendMode,
      layout: pipelineLayout,
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: WGPU_SURFACE_VERTEX_LAYOUT,
      },
      fragment: {
        module: fragmentModule,
        entryPoint: "fragmentMain",
        targets: [{
          format: targetFormat,
          blend: wgpuBlendState(blendMode),
        }],
      },
      primitive: { topology: "triangle-list", cullMode: "none" },
      multisample: { count: Math.max(1, Math.floor(sampleCount || 1)) },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: depthWrite,
        depthCompare: "less-equal",
      },
    });
  }

  function wgpuCreateThickLinePipeline(device, pipelineLayout, vertexModule, fragmentModule, blendMode, depthWrite, targetFormat, sampleCount) {
    return device.createRenderPipeline({
      label: "gosx-thick-line-" + blendMode,
      layout: pipelineLayout,
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: WGPU_THICK_LINE_VERTEX_LAYOUT,
      },
      fragment: {
        module: fragmentModule,
        entryPoint: "fragmentMain",
        targets: [{
          format: targetFormat,
          blend: wgpuBlendState(blendMode),
        }],
      },
      primitive: { topology: "triangle-list", cullMode: "none" },
      multisample: { count: Math.max(1, Math.floor(sampleCount || 1)) },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: depthWrite,
        depthCompare: "less-equal",
      },
    });
  }

  function wgpuCreatePointsPipeline(device, pipelineLayout, vertexModule, fragmentModule, blendMode, depthWrite, targetFormat, sampleCount) {
    return device.createRenderPipeline({
      label: "gosx-points-" + blendMode,
      layout: pipelineLayout,
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: [],
      },
      fragment: {
        module: fragmentModule,
        entryPoint: "fragmentMain",
        targets: [{
          format: targetFormat,
          blend: wgpuBlendState(blendMode),
        }],
      },
      primitive: { topology: "triangle-list" },
      multisample: { count: Math.max(1, Math.floor(sampleCount || 1)) },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: depthWrite,
        depthCompare: "less-equal",
      },
    });
  }

  function wgpuCreatePointsVertexPipeline(device, pipelineLayout, vertexModule, fragmentModule, blendMode, depthWrite, targetFormat, sampleCount) {
    return device.createRenderPipeline({
      label: "gosx-points-vertex-" + blendMode,
      layout: pipelineLayout,
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: WGPU_POINTS_INSTANCE_VERTEX_LAYOUT,
      },
      fragment: {
        module: fragmentModule,
        entryPoint: "fragmentMain",
        targets: [{
          format: targetFormat,
          blend: wgpuBlendState(blendMode),
        }],
      },
      primitive: { topology: "triangle-list" },
      multisample: { count: Math.max(1, Math.floor(sampleCount || 1)) },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: depthWrite,
        depthCompare: "less-equal",
      },
    });
  }

  function wgpuCreatePostPipeline(device, layout, fragmentModule, targetFormat) {
    var vertModule = device.createShaderModule({ label: "post-vert", code: WGSL_POST_VERTEX });
    return device.createRenderPipeline({
      label: "gosx-post",
      layout: layout,
      vertex: {
        module: vertModule,
        entryPoint: "vertexMain",
        buffers: [],
      },
      fragment: {
        module: fragmentModule,
        entryPoint: "fragmentMain",
        targets: [{ format: targetFormat }],
      },
      primitive: { topology: "triangle-strip", stripIndexFormat: "uint32" },
    });
  }

  // -----------------------------------------------------------------------
  // Shadow Resources
  // -----------------------------------------------------------------------

  function wgpuCreateShadowMap(device, size) {
    var texture = device.createTexture({
      size: [size, size, 1],
      format: "depth24plus",
      usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
    });
    return { texture: texture, view: texture.createView(), size: size };
  }

  // -----------------------------------------------------------------------
  // Post-Processing Manager (WebGPU)
  // -----------------------------------------------------------------------

  function wgpuCreatePostProcessor(device, targetFormat) {
    var sceneTex = null;
    var sceneTexView = null;
    var auxTex = null;
    var auxTexView = null;
    var pingPongA = null;
    var pingPongAView = null;
    var pingPongB = null;
    var pingPongBView = null;
    var pingPongWidth = 0;
    var pingPongHeight = 0;
    var depthTex = null;
    var depthTexView = null;
    var currentWidth = 0;
    var currentHeight = 0;

    var linearSampler = device.createSampler({ magFilter: "linear", minFilter: "linear" });

    // Lazily compiled pipelines and layouts.
    var pipelines = {};
    var postParamsLayout = null;
    var bloomCompositeLayout = null;
    var postBlitLayout = null;
    var ssaoLayout = null;
    // Uniform buffers for post params (reused each frame).
    var postParamBuffers = {};

    function getPostParamsLayout() {
      if (!postParamsLayout) postParamsLayout = wgpuCreatePostWithParamsBindGroupLayout(device);
      return postParamsLayout;
    }
    function getBloomCompositeLayout() {
      if (!bloomCompositeLayout) bloomCompositeLayout = wgpuCreateBloomCompositeBindGroupLayout(device);
      return bloomCompositeLayout;
    }
    function getPostBlitLayout() {
      if (!postBlitLayout) postBlitLayout = wgpuCreatePostBindGroupLayout(device);
      return postBlitLayout;
    }
    function getSSAOLayout() {
      if (!ssaoLayout) ssaoLayout = wgpuCreateSSAOBindGroupLayout(device);
      return ssaoLayout;
    }

    function getPipeline(name, fragmentSource, layout) {
      if (pipelines[name]) return pipelines[name];
      var fragModule = device.createShaderModule({ label: "post-" + name, code: fragmentSource });
      var pipelineLayout = device.createPipelineLayout({ bindGroupLayouts: [layout] });
      var pipeline = wgpuCreatePostPipeline(device, pipelineLayout, fragModule, targetFormat);
      pipelines[name] = pipeline;
      return pipeline;
    }

    function getParamBuffer(name, byteSize) {
      if (postParamBuffers[name] && postParamBuffers[name].size >= byteSize) {
        return postParamBuffers[name];
      }
      if (postParamBuffers[name]) postParamBuffers[name].destroy();
      postParamBuffers[name] = device.createBuffer({
        size: wgpuAlignUp(byteSize, 16),
        usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
      });
      return postParamBuffers[name];
    }

    function focusDepthForEffect(effect, camera) {
      var focus = Math.max(0, sceneNumber(effect && effect.focusDistance, 8));
      var near = Math.max(0.0001, sceneNumber(camera && camera.near, 0.1));
      var far = Math.max(near + 0.0001, sceneNumber(camera && camera.far, 1000));
      return clamp01((focus - near) / (far - near));
    }

    function ensureFBOs(width, height) {
      if (width === currentWidth && height === currentHeight && sceneTex) return;
      // Destroy old.
      if (sceneTex) sceneTex.destroy();
      if (auxTex) auxTex.destroy();
      if (depthTex) depthTex.destroy();

      var texUsage = GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING;
      sceneTex = device.createTexture({ size: [width, height, 1], format: targetFormat, usage: texUsage });
      sceneTexView = sceneTex.createView();
      auxTex = device.createTexture({ size: [width, height, 1], format: targetFormat, usage: texUsage });
      auxTexView = auxTex.createView();
      depthTex = device.createTexture({
        size: [width, height, 1],
        format: "depth24plus",
        usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
      });
      depthTexView = depthTex.createView();

      currentWidth = width;
      currentHeight = height;
    }

    // Lazily (re)allocate the bloom ping-pong pair at a specific resolution.
    // Called from inside the bloom effect case with dims derived from
    // effect.scale, so Bloom.Scale reaches the WebGPU backend at parity with
    // the WebGL backend. Keeps the textures cached across frames and only
    // tears them down when the target resolution changes.
    function ensureBloomPingPong(w, h) {
      if (w === pingPongWidth && h === pingPongHeight && pingPongA) return;
      if (pingPongA) pingPongA.destroy();
      if (pingPongB) pingPongB.destroy();
      var texUsage = GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING;
      pingPongA = device.createTexture({ size: [w, h, 1], format: targetFormat, usage: texUsage });
      pingPongAView = pingPongA.createView();
      pingPongB = device.createTexture({ size: [w, h, 1], format: targetFormat, usage: texUsage });
      pingPongBView = pingPongB.createView();
      pingPongWidth = w;
      pingPongHeight = h;
    }

    function fullscreenPass(encoder, pipeline, bindGroup, targetView) {
      var pass = encoder.beginRenderPass({
        colorAttachments: [{
          view: targetView,
          loadOp: "clear",
          storeOp: "store",
          clearValue: { r: 0, g: 0, b: 0, a: 1 },
        }],
      });
      pass.setPipeline(pipeline);
      pass.setBindGroup(0, bindGroup);
      pass.draw(4);
      pass.end();
    }

    return {
      getSceneTarget: function(width, height) {
        ensureFBOs(width, height);
        return { colorView: sceneTexView, depthView: depthTexView };
      },

      apply: function(encoder, effects, scaledW, scaledH, canvasW, canvasH, finalView, camera) {
        ensureFBOs(scaledW, scaledH);

        var currentTexView = sceneTexView;
        var blitPipeline = getPipeline("blit", WGSL_POST_BLIT_FRAGMENT, getPostBlitLayout());
        var stats = { postEffects: effects.length, postSSAOPasses: 0, postDOFPasses: 0 };

        for (var i = 0; i < effects.length; i++) {
          var effect = effects[i];
          var isLast = (i === effects.length - 1);
          var outputView = isLast ? finalView : (currentTexView === sceneTexView ? auxTexView : sceneTexView);

          switch (effect.kind) {
            case SCENE_POST_TONE_MAPPING: {
              var pipeline = getPipeline("toneMapping", WGSL_POST_TONEMAPPING_FRAGMENT, getPostParamsLayout());
              var buf = getParamBuffer("toneMapping", 16);
              device.queue.writeBuffer(buf, 0, new Float32Array([sceneNumber(effect.exposure, 1.0), sceneWebGPUToneMapMode(effect.mode), 0, 0]));
              var bg = device.createBindGroup({
                layout: getPostParamsLayout(),
                entries: [
                  { binding: 0, resource: currentTexView },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: { buffer: buf } },
                ],
              });
              fullscreenPass(encoder, pipeline, bg, outputView);
              currentTexView = outputView;
              break;
            }
            case SCENE_POST_BLOOM: {
              // Bloom ping-pong resolution is scaledW/H * Bloom.Scale.
              // Zero / out-of-range scale falls back to 0.5 (v0.14.0 default),
              // matching the WebGL helper in applyBloom.
              var bloomScale = (effect.scale > 0 && effect.scale <= 1) ? effect.scale : 0.5;
              var halfW = Math.max(1, Math.floor(scaledW * bloomScale));
              var halfH = Math.max(1, Math.floor(scaledH * bloomScale));
              ensureBloomPingPong(halfW, halfH);
              var threshold = sceneNumber(effect.threshold, 0.8);
              var radius = sceneNumber(effect.radius, 5.0);
              var intensity = sceneNumber(effect.intensity, 0.5);

              // 1. Bright pass -> pingPongA.
              var brightPipeline = getPipeline("bloomBright", WGSL_POST_BLOOM_BRIGHT_FRAGMENT, getPostParamsLayout());
              var brightBuf = getParamBuffer("bloomBright", 16);
              device.queue.writeBuffer(brightBuf, 0, new Float32Array([threshold, 0, 0, 0]));
              var brightBG = device.createBindGroup({
                layout: getPostParamsLayout(),
                entries: [
                  { binding: 0, resource: currentTexView },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: { buffer: brightBuf } },
                ],
              });
              fullscreenPass(encoder, brightPipeline, brightBG, pingPongAView);

              // 2. Horizontal blur: pingPongA -> pingPongB.
              var blurPipeline = getPipeline("blur", WGSL_POST_BLUR_FRAGMENT, getPostParamsLayout());
              var blurBuf = getParamBuffer("bloomBlurH", 16);
              device.queue.writeBuffer(blurBuf, 0, new Float32Array([1.0, 0.0, radius, 0]));
              var blurBGH = device.createBindGroup({
                layout: getPostParamsLayout(),
                entries: [
                  { binding: 0, resource: pingPongAView },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: { buffer: blurBuf } },
                ],
              });
              fullscreenPass(encoder, blurPipeline, blurBGH, pingPongBView);

              // 3. Vertical blur: pingPongB -> pingPongA.
              var blurBufV = getParamBuffer("bloomBlurV", 16);
              device.queue.writeBuffer(blurBufV, 0, new Float32Array([0.0, 1.0, radius, 0]));
              var blurBGV = device.createBindGroup({
                layout: getPostParamsLayout(),
                entries: [
                  { binding: 0, resource: pingPongBView },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: { buffer: blurBufV } },
                ],
              });
              fullscreenPass(encoder, blurPipeline, blurBGV, pingPongAView);

              // 4. Composite: scene + bloom -> output.
              var compPipeline = getPipeline("bloomComposite", WGSL_POST_BLOOM_COMPOSITE_FRAGMENT, getBloomCompositeLayout());
              var compBuf = getParamBuffer("bloomComposite", 16);
              device.queue.writeBuffer(compBuf, 0, new Float32Array([intensity, 0, 0, 0]));
              var compBG = device.createBindGroup({
                layout: getBloomCompositeLayout(),
                entries: [
                  { binding: 0, resource: currentTexView },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: pingPongAView },
                  { binding: 3, resource: linearSampler },
                  { binding: 4, resource: { buffer: compBuf } },
                ],
              });
              fullscreenPass(encoder, compPipeline, compBG, outputView);
              currentTexView = outputView;
              break;
            }
            case SCENE_POST_SSAO: {
              var ssaoPipeline = getPipeline("ssao", WGSL_POST_SSAO_FRAGMENT, getSSAOLayout());
              var ssaoBuf = getParamBuffer("ssao", 32);
              var radius = sceneNumber(effect.radius, 4.0);
              var intensity = sceneNumber(effect.intensity, 0.55);
              var bias = sceneNumber(effect.bias, 0.01);
              device.queue.writeBuffer(ssaoBuf, 0, new Float32Array([
                radius,
                intensity,
                bias,
                0,
                1 / Math.max(1, scaledW),
                1 / Math.max(1, scaledH),
                0,
                0,
              ]));
              var ssaoBG = device.createBindGroup({
                layout: getSSAOLayout(),
                entries: [
                  { binding: 0, resource: currentTexView },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: depthTexView },
                  { binding: 3, resource: { buffer: ssaoBuf } },
                ],
              });
              fullscreenPass(encoder, ssaoPipeline, ssaoBG, outputView);
              stats.postSSAOPasses += 1;
              currentTexView = outputView;
              break;
            }
            case SCENE_POST_DOF: {
              var dofPipeline = getPipeline("dof", WGSL_POST_DOF_FRAGMENT, getSSAOLayout());
              var dofBuf = getParamBuffer("dof", 32);
              device.queue.writeBuffer(dofBuf, 0, new Float32Array([
                focusDepthForEffect(effect, camera),
                sceneNumber(effect.aperture, 0.04),
                sceneNumber(effect.maxBlur, 8.0),
                0,
                1 / Math.max(1, scaledW),
                1 / Math.max(1, scaledH),
                0,
                0,
              ]));
              var dofBG = device.createBindGroup({
                layout: getSSAOLayout(),
                entries: [
                  { binding: 0, resource: currentTexView },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: depthTexView },
                  { binding: 3, resource: { buffer: dofBuf } },
                ],
              });
              fullscreenPass(encoder, dofPipeline, dofBG, outputView);
              stats.postDOFPasses += 1;
              currentTexView = outputView;
              break;
            }
            case SCENE_POST_VIGNETTE: {
              var vigPipeline = getPipeline("vignette", WGSL_POST_VIGNETTE_FRAGMENT, getPostParamsLayout());
              var vigBuf = getParamBuffer("vignette", 16);
              device.queue.writeBuffer(vigBuf, 0, new Float32Array([sceneNumber(effect.intensity, 1.0), 0, 0, 0]));
              var vigBG = device.createBindGroup({
                layout: getPostParamsLayout(),
                entries: [
                  { binding: 0, resource: currentTexView },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: { buffer: vigBuf } },
                ],
              });
              fullscreenPass(encoder, vigPipeline, vigBG, outputView);
              currentTexView = outputView;
              break;
            }
            case SCENE_POST_COLOR_GRADE: {
              var cgPipeline = getPipeline("colorGrade", WGSL_POST_COLORGRADE_FRAGMENT, getPostParamsLayout());
              var cgBuf = getParamBuffer("colorGrade", 16);
              device.queue.writeBuffer(cgBuf, 0, new Float32Array([
                sceneNumber(effect.exposure, 1.0),
                sceneNumber(effect.contrast, 1.0),
                sceneNumber(effect.saturation, 1.0),
                0,
              ]));
              var cgBG = device.createBindGroup({
                layout: getPostParamsLayout(),
                entries: [
                  { binding: 0, resource: currentTexView },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: { buffer: cgBuf } },
                ],
              });
              fullscreenPass(encoder, cgPipeline, cgBG, outputView);
              currentTexView = outputView;
              break;
            }
            default:
              break;
          }
        }

        // If no effects matched or we need a final blit.
        if (currentTexView !== finalView) {
          var blitBG = device.createBindGroup({
            layout: getPostBlitLayout(),
            entries: [
              { binding: 0, resource: currentTexView },
              { binding: 1, resource: linearSampler },
            ],
          });
          fullscreenPass(encoder, blitPipeline, blitBG, finalView);
        }
        return stats;
      },

      dispose: function() {
        if (sceneTex) sceneTex.destroy();
        if (auxTex) auxTex.destroy();
        if (depthTex) depthTex.destroy();
        if (pingPongA) pingPongA.destroy();
        if (pingPongB) pingPongB.destroy();
        for (var key in postParamBuffers) {
          if (postParamBuffers[key]) postParamBuffers[key].destroy();
        }
        sceneTex = auxTex = depthTex = pingPongA = pingPongB = null;
        currentWidth = 0;
        currentHeight = 0;
        pingPongWidth = 0;
        pingPongHeight = 0;
      },
    };
  }

  // -----------------------------------------------------------------------
  // WebGPU Renderer
  // -----------------------------------------------------------------------

  function createSceneWebGPURenderer(canvas, options) {
    function sceneWebGPUFactoryFailure(reason) {
      var text = String(reason || "unknown");
      try {
        if (typeof window !== "undefined") {
          var rect = canvas && typeof canvas.getBoundingClientRect === "function" ? canvas.getBoundingClientRect() : null;
          window.__gosx_scene3d_webgpu_factory_error = text;
          window.__gosx_scene3d_webgpu_factory_context = {
            reason: text,
            canvasChildren: canvas && canvas.childNodes ? canvas.childNodes.length : -1,
            canvasParent: canvas && canvas.parentNode && canvas.parentNode.tagName ? canvas.parentNode.tagName : "",
            canvasWidth: canvas && Number(canvas.width) || 0,
            canvasHeight: canvas && Number(canvas.height) || 0,
            canvasConnected: !!(canvas && canvas.isConnected),
            canvasRectWidth: rect ? Number(rect.width) || 0 : 0,
            canvasRectHeight: rect ? Number(rect.height) || 0 : 0,
          };
        }
      } catch (_err) {}
      console.warn("[gosx] WebGPU factory unavailable:", text);
      return null;
    }

    if (typeof navigator === "undefined" || !navigator.gpu) return sceneWebGPUFactoryFailure("navigator-gpu-unavailable");
    if (!canvas || typeof canvas.getContext !== "function") return sceneWebGPUFactoryFailure("canvas-context-unavailable");

    // Device + adapter come from the main-bundle probe (16z). The
    // probe has already verified BOTH requestAdapter AND requestDevice
    // succeed — if we're here, WebGPU is genuinely usable. Reusing the
    // probed device (instead of requesting another) sidesteps a subtle
    // failure mode where requestAdapter works twice but requestDevice
    // fails on the second call (seen on some mobile GPUs).
    var probe = _externalProbe();
    if (!probe || !probe.ready || !probe.adapter || !probe.device) {
      var probeError = probe && probe.error ? ": " + probe.error : "";
      return sceneWebGPUFactoryFailure("probe-not-ready" + probeError);
    }
    var adapter = probe.adapter;
    var device = probe.device;
    var rendererOptions = options && typeof options === "object" ? options : {};

    // Only NOW taint the canvas with a WebGPU context. If any of the
    // checks above failed we never reached this line, so the canvas
    // stays clean and the mount code can fall through to WebGL.
    var gpuCtx = canvas.getContext("webgpu");
    if (!gpuCtx) return sceneWebGPUFactoryFailure("canvas-webgpu-context-unavailable");

    // initFailed remains for runtime device-loss recovery; startInit is
    // effectively a no-op now that we have the device up front, but we
    // keep the name for backwards compatibility with the existing render
    // loop structure.
    var initFailed = false;
    var initError = "";
    var initStarted = true;
    var targetFormat = navigator.gpu.getPreferredCanvasFormat();
    var presentationOptions = rendererOptions.presentation && typeof rendererOptions.presentation === "object" ? rendererOptions.presentation : {};
    var probeOptions = probe.probeOptions && typeof probe.probeOptions === "object" ? probe.probeOptions : {};
    var activePowerPreference = sceneWebGPUCanvasPowerPreference(probeOptions.powerPreference);
    var activePresentation = {
      alphaMode: sceneWebGPUCanvasAlphaMode(presentationOptions.alphaMode),
      colorSpace: sceneWebGPUCanvasColorSpace(presentationOptions.colorSpace),
      toneMappingMode: sceneWebGPUCanvasToneMappingMode(presentationOptions.toneMappingMode),
    };

    function sceneWebGPUCanvasAlphaMode(value) {
      var normalized = String(value || "").trim().toLowerCase();
      if (normalized === "opaque" || normalized === "premultiplied") {
        return normalized;
      }
      return "premultiplied";
    }

    function sceneWebGPUCanvasColorSpace(value) {
      var normalized = String(value || "").trim().toLowerCase();
      if (normalized === "display-p3" || normalized === "srgb") {
        return normalized;
      }
      return "srgb";
    }

    function sceneWebGPUCanvasToneMappingMode(value) {
      var normalized = String(value || "").trim().toLowerCase();
      if (normalized === "extended" || normalized === "standard") {
        return normalized;
      }
      return "";
    }

    function sceneWebGPUCanvasPowerPreference(value) {
      var normalized = String(value || "").trim().toLowerCase();
      if (normalized === "high-performance" || normalized === "low-power") {
        return normalized;
      }
      return "";
    }

    function sceneWebGPUCanvasConfiguration() {
      var config = {
        device: device,
        format: targetFormat,
        alphaMode: activePresentation.alphaMode,
        colorSpace: activePresentation.colorSpace,
      };
      if (activePresentation.toneMappingMode) {
        config.toneMapping = { mode: activePresentation.toneMappingMode };
      }
      return config;
    }

    function configureWebGPUCanvas() {
      gpuCtx.configure(sceneWebGPUCanvasConfiguration());
    }

    // GPU resources (initialized after device is ready).
    var frameBindGroupLayout = null;
    var materialBindGroupLayout = null;
    var elioSkinBindGroupLayout = null;
    var computedMorphBindGroupLayout = null;
    var pointsBindGroupLayout = null;
    var pointsUniformBindGroupLayout = null;
    var shadowBindGroupLayout = null;
    var pbrPipelineLayout = null;
    var elioSkinPipelineLayout = null;
    var computedMorphPipelineLayout = null;
    var pointsPipelineLayout = null;
    var pointsVertexPipelineLayout = null;
    var selenaPipelineCache = new Map();

    var pbrVertexModule = null;
    var pbrInstancedVertexModule = null;
    var pbrFragmentModule = null;
    var elioSkinShaderModule = null;
    var elioSkinPipeline = null;
    var computedMorphShaderModule = null;
    var computedMorphPipeline = null;
    var shadowVertexModule = null;
    var shadowInstancedVertexModule = null;
    var shadowFragmentModule = null;
    var sceneWorldColorVertexModule = null;
    var sceneClipColorVertexModule = null;
    var sceneColorFragmentModule = null;
    var surfaceVertexModule = null;
    var surfaceFragmentModule = null;
    var thickLineVertexModule = null;
    var thickLineFragmentModule = null;
    var pointsVertexModule = null;
    var pointsInstancedVertexModule = null;
    var pointsFragmentModule = null;

    // Pipeline cache.
    var pipelineCache = {};
    var activeSampleCount = 1;

    // Shadow resources.
    var shadowSlots = [null, null];

    // Persistent GPU buffers.
    var frameUniformBuffer = null;
    var lightStorageBuffer = null;
    var fogUniformBuffer = null;
    var envUniformBuffer = null;
    var shadowUniformBuffer = null;
    var positionBuffer = null;
    var normalBuffer = null;
    var uvBuffer = null;
    var tangentBuffer = null;
    var defaultMaterialOwner = {};
    var instancedGeometryCache = {};
    var worldDrawScratch = typeof createSceneWorldDrawScratch === "function" ? createSceneWorldDrawScratch() : null;
    var thickLineScratch = typeof createSceneThickLineScratch === "function" ? createSceneThickLineScratch() : null;
    var thickLineOwner = {};
    var screenLineOwner = {};

    // Points buffers.
    //
    // Each points entry owns its uniform/storage buffers. Uniform data can
    // move every frame (spin/fog/opacity), so it reuses per-entry GPUBuffer
    // storage with writeBuffer. Particle storage is keyed by the stable
    // typed-array payload and uploads only when source/count/color inputs
    // change.
    var pointsEntryGPUBuffers = new Set(); // all allocated GPUBuffers for dispose()
    // Hoisted scratches so uniform uploads don't allocate fresh 128-byte
    // ArrayBuffers per entry per frame. The WGSL PointsUniforms layout is
    // vec4-aligned: mat4 + vec4 color/size + vec4 flags + vec4 params +
    // vec4 fog color. Wrapped Float32Array / Uint32Array views are created
    // once for the same underlying storage.
    var pointsUniformScratch = new ArrayBuffer(128);
    var pointsUniformScratchF = new Float32Array(pointsUniformScratch);
    var pointsUniformScratchU = new Uint32Array(pointsUniformScratch);
    var computeParticleSystems = new Map();
    var lastComputeParticleTimeSeconds = null;
    var lastPreparedScene = null;
    var lastWebGPUFrameStats = null;
    var webGPUFrameSeq = 0;
    var pendingWebGPUErrorScope = false;
    var webGPUErrorReportCount = 0;

    // Shadow pass buffer.
    var shadowPositionBuffer = null;
    var shadowFrameBuffer = null;

    // Depth texture for main render pass.
    var mainDepthTexture = null;
    var mainDepthView = null;
    var mainDepthWidth = 0;
    var mainDepthHeight = 0;
    var mainDepthSampleCount = 1;
    var mainMSAATexture = null;
    var mainMSAAView = null;
    var mainMSAAWidth = 0;
    var mainMSAAHeight = 0;
    var mainMSAASampleCount = 1;

    // 1x1 dummy depth texture for shadow map bind group when no shadows.
    var dummyShadowTex = null;
    var dummyShadowView = null;

    // Default sampler for materials.
    var linearSampler = null;
    var comparisonSampler = null;

    // Texture cache.
    var textureCache = new Map();

    // 1x1 white placeholder texture (for unbound material maps).
    var placeholderTex = null;
    var placeholderView = null;

    // Post-processor.
    var postProcessor = null;

    // Scratch Float32Arrays.
    var scratchViewMatrix = new Float32Array(16);
    var scratchProjMatrix = new Float32Array(16);
    var scratchSelenaViewProjection = new Float32Array(16);
    var pointsIdentityMatrix = new Float32Array([
      1, 0, 0, 0,
      0, 1, 0, 0,
      0, 0, 1, 0,
      0, 0, 0, 1,
    ]);

    var scratchPositions = null;
    var scratchNormals = null;
    var scratchUVs = null;
    var scratchTangents = null;

    function ensureScratch(name, length) {
      if (name === "positions") {
        if (!scratchPositions || scratchPositions.length < length) scratchPositions = new Float32Array(length);
        return scratchPositions;
      }
      if (name === "normals") {
        if (!scratchNormals || scratchNormals.length < length) scratchNormals = new Float32Array(length);
        return scratchNormals;
      }
      if (name === "uvs") {
        if (!scratchUVs || scratchUVs.length < length) scratchUVs = new Float32Array(length);
        return scratchUVs;
      }
      if (name === "tangents") {
        if (!scratchTangents || scratchTangents.length < length) scratchTangents = new Float32Array(length);
        return scratchTangents;
      }
      return new Float32Array(length);
    }

    function sliceToFloat32(source, offset, count, stride, scratchName) {
      var length = count * stride;
      var start = offset * stride;
      var buf = ensureScratch(scratchName, length);
      for (var i = 0; i < length; i++) {
        buf[i] = source && source[start + i] !== undefined ? +source[start + i] : 0;
      }
      return buf.subarray(0, length);
    }

    function wgpuCreateTrackedBuffer(usage, dataOrSize) {
      var size = typeof dataOrSize === "number"
        ? wgpuAlignUp(Math.max(dataOrSize, 4), 4)
        : wgpuAlignUp(Math.max(dataOrSize.byteLength, 4), 4);
      var buffer = wgpuCreateBuffer(device, usage, dataOrSize);
      try { buffer._gosxByteLength = size; } catch (_err) {}
      pointsEntryGPUBuffers.add(buffer);
      return buffer;
    }

    function wgpuTrackedBufferSize(buffer) {
      if (!buffer) return 0;
      if (typeof buffer.size === "number") return buffer.size;
      if (typeof buffer._gosxByteLength === "number") return buffer._gosxByteLength;
      return 0;
    }

    function wgpuUploadTrackedBuffer(usage, buffer, data, state) {
      var needed = wgpuAlignUp(Math.max(data && data.byteLength || 0, 4), 4);
      if (state && state.bytesChanged && wgpuTrackedBufferSize(buffer) < needed) {
        if (buffer && typeof buffer.destroy === "function") {
          pointsEntryGPUBuffers.delete(buffer);
          buffer.destroy();
        }
        buffer = wgpuCreateTrackedBuffer(usage, needed);
      }
      device.queue.writeBuffer(buffer, 0, data);
      return buffer;
    }

    function wgpuCachedTrackedBuffer(owner, slot, typedArray, usage, dynamic) {
      if (!owner || !typedArray) return null;
      if (typeof sceneCachedBuffer === "function") {
        return sceneCachedBuffer(owner, typedArray, function(data) {
          return wgpuCreateTrackedBuffer(usage, data && data.byteLength || 4);
        }, function(buffer, data, state) {
          return wgpuUploadTrackedBuffer(usage, buffer, data, state);
        }, { slot: slot, dynamic: !!dynamic });
      }
      var existing = owner[slot];
      if (!existing || wgpuTrackedBufferSize(existing) < typedArray.byteLength) {
        if (existing && typeof existing.destroy === "function") {
          pointsEntryGPUBuffers.delete(existing);
          existing.destroy();
        }
        existing = wgpuCreateTrackedBuffer(usage, typedArray && typedArray.byteLength || 4);
        owner[slot] = existing;
        device.queue.writeBuffer(existing, 0, typedArray);
        owner[slot + "Source"] = typedArray;
        return existing;
      }
      if (dynamic || owner[slot + "Source"] !== typedArray) {
        device.queue.writeBuffer(existing, 0, typedArray);
        owner[slot + "Source"] = typedArray;
      }
      return existing;
    }

    function ensurePointsUniformGPUBuffer(owner, uniformData) {
      return wgpuCachedTrackedBuffer(
        owner,
        "_gosxWGPUPointsUniformBuffer",
        uniformData,
        GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
        true
      );
    }

    function ensurePointsParticleGPUBuffer(entry, particleData) {
      return wgpuCachedTrackedBuffer(
        entry,
        "_gosxWGPUPointsParticleBuffer",
        particleData,
        GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST,
        false
      );
    }

    function ensurePointsParticleVertexGPUBuffer(entry, particleData) {
      return wgpuCachedTrackedBuffer(
        entry,
        "_gosxWGPUPointsParticleVertexBuffer",
        particleData,
        GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST,
        false
      );
    }

    function pointsDefaultColorChanged(entry, rgba) {
      var cached = entry._cachedParticleDefaultColor;
      return !cached ||
        cached[0] !== rgba[0] ||
        cached[1] !== rgba[1] ||
        cached[2] !== rgba[2] ||
        cached[3] !== rgba[3];
    }

    function ensurePointsParticleData(entry, count, hasSizes, hasColors, defaultColorRGBA) {
      var pos = entry._cachedPos;
      var sizes = hasSizes ? entry._cachedSizes : null;
      var colors = hasColors ? entry._cachedColors : null;
      if (
        entry._cachedParticleData &&
        entry._cachedParticleCount === count &&
        entry._cachedParticlePositions === pos &&
        entry._cachedParticleSizes === sizes &&
        entry._cachedParticleColors === colors &&
        !pointsDefaultColorChanged(entry, defaultColorRGBA)
      ) {
        return entry._cachedParticleData;
      }

      var particleData = new Float32Array(count * 8);
      for (var pi = 0; pi < count; pi++) {
        var base = pi * 8;
        particleData[base + 0] = pos[pi * 3];
        particleData[base + 1] = pos[pi * 3 + 1];
        particleData[base + 2] = pos[pi * 3 + 2];
        particleData[base + 3] = hasSizes ? sizes[pi] : sceneNumber(entry.size, 1);
        if (hasColors) {
          particleData[base + 4] = colors[pi * 4];
          particleData[base + 5] = colors[pi * 4 + 1];
          particleData[base + 6] = colors[pi * 4 + 2];
          particleData[base + 7] = colors[pi * 4 + 3];
        } else {
          particleData[base + 4] = defaultColorRGBA[0];
          particleData[base + 5] = defaultColorRGBA[1];
          particleData[base + 6] = defaultColorRGBA[2];
          particleData[base + 7] = 1.0;
        }
      }

      entry._cachedParticleData = particleData;
      entry._cachedParticleCount = count;
      entry._cachedParticlePositions = pos;
      entry._cachedParticleSizes = sizes;
      entry._cachedParticleColors = colors;
      entry._cachedParticleDefaultColor = defaultColorRGBA.slice ? defaultColorRGBA.slice(0, 4) : [
        defaultColorRGBA[0], defaultColorRGBA[1], defaultColorRGBA[2], defaultColorRGBA[3],
      ];
      return particleData;
    }

    // Synchronous device initialization — device was already obtained
    // by the main-bundle probe (16z). Previously this was a two-stage
    // async sequence (requestAdapter → requestDevice → set up GPU
    // resources), but the probe now owns the adapter+device lifecycle
    // so we can do all the GPU-resource setup synchronously at factory
    // construction time, ensuring the renderer is never returned in a
    // half-initialized state.
    //
    // startInit is retained as a no-op for the existing call site in
    // render() ("if (!device) startInit()") to keep the diff tight;
    // the first render call falls straight through since device is
    // already set.
    function startInit() { /* no-op: device already initialized */ }

    // Everything below used to be inside the .then() chain after
    // requestDevice resolved. It's now run synchronously so the
    // returned renderer is fully ready before the factory call returns.
    (function initGPUResources() {
      try {
        // Handle device loss post-factory. Re-uses the stub's probe
        // invalidation path via window.__gosx_scene3d_webgpu_api.
        device.lost.then(function(info) {
          console.warn("[gosx] WebGPU device lost:", info && info.message);
          device = null;
          initFailed = true;
        }).catch(function() {});

        configureWebGPUCanvas();

        // Create bind group layouts.
        frameBindGroupLayout = wgpuCreateFrameBindGroupLayout(device);
        materialBindGroupLayout = wgpuCreateMaterialBindGroupLayout(device);
        elioSkinBindGroupLayout = device.createBindGroupLayout({
          label: "gosx-elio-skin-lbs",
          entries: [
            { binding: 0, visibility: GPUShaderStage.COMPUTE, buffer: { type: "read-only-storage" } },
            { binding: 1, visibility: GPUShaderStage.COMPUTE, buffer: { type: "read-only-storage" } },
            { binding: 2, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } },
          ],
        });
        computedMorphBindGroupLayout = device.createBindGroupLayout({
          label: "gosx-computed-morph",
          entries: [
            { binding: 0, visibility: GPUShaderStage.COMPUTE, buffer: { type: "read-only-storage" } },
            { binding: 1, visibility: GPUShaderStage.COMPUTE, buffer: { type: "read-only-storage" } },
            { binding: 2, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } },
            { binding: 3, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } },
            { binding: 4, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } },
            { binding: 5, visibility: GPUShaderStage.COMPUTE, buffer: { type: "uniform" } },
          ],
        });
        pointsBindGroupLayout = wgpuCreatePointsBindGroupLayout(device);
        pointsUniformBindGroupLayout = wgpuCreatePointsUniformBindGroupLayout(device);
        shadowBindGroupLayout = wgpuCreateShadowBindGroupLayout(device);

        // Pipeline layouts.
        pbrPipelineLayout = device.createPipelineLayout({
          bindGroupLayouts: [frameBindGroupLayout, materialBindGroupLayout],
        });
        elioSkinPipelineLayout = device.createPipelineLayout({
          bindGroupLayouts: [elioSkinBindGroupLayout],
        });
        computedMorphPipelineLayout = device.createPipelineLayout({
          bindGroupLayouts: [computedMorphBindGroupLayout],
        });
        pointsPipelineLayout = device.createPipelineLayout({
          bindGroupLayouts: [frameBindGroupLayout, materialBindGroupLayout, pointsBindGroupLayout],
        });
        pointsVertexPipelineLayout = device.createPipelineLayout({
          bindGroupLayouts: [frameBindGroupLayout, materialBindGroupLayout, pointsUniformBindGroupLayout],
        });

        // Compile shader modules.
        pbrVertexModule = device.createShaderModule({ label: "pbr-vert", code: WGSL_PBR_VERTEX });
        pbrInstancedVertexModule = device.createShaderModule({ label: "pbr-instanced-vert", code: WGSL_PBR_INSTANCED_VERTEX });
        pbrFragmentModule = device.createShaderModule({ label: "pbr-frag", code: WGSL_PBR_FRAGMENT });
        elioSkinShaderModule = device.createShaderModule({ label: "elio-skin-lbs", code: SCENE_ELIO_SKIN_LBS_SOURCE });
        elioSkinPipeline = device.createComputePipeline({
          label: "gosx-elio-skin-lbs",
          layout: elioSkinPipelineLayout,
          compute: { module: elioSkinShaderModule, entryPoint: "skin" },
        });
        computedMorphShaderModule = device.createShaderModule({ label: "computed-morph", code: SCENE_COMPUTED_MORPH_SOURCE });
        computedMorphPipeline = device.createComputePipeline({
          label: "gosx-computed-morph",
          layout: computedMorphPipelineLayout,
          compute: { module: computedMorphShaderModule, entryPoint: "morphPose" },
        });
        shadowVertexModule = device.createShaderModule({ label: "shadow-vert", code: WGSL_SHADOW_VERTEX });
        shadowInstancedVertexModule = device.createShaderModule({ label: "shadow-instanced-vert", code: WGSL_SHADOW_INSTANCED_VERTEX });
        shadowFragmentModule = device.createShaderModule({ label: "shadow-frag", code: WGSL_SHADOW_FRAGMENT });
        sceneWorldColorVertexModule = device.createShaderModule({ label: "scene-world-color-vert", code: WGSL_SCENE_WORLD_COLOR_VERTEX });
        sceneClipColorVertexModule = device.createShaderModule({ label: "scene-clip-color-vert", code: WGSL_SCENE_CLIP_COLOR_VERTEX });
        sceneColorFragmentModule = device.createShaderModule({ label: "scene-color-frag", code: WGSL_SCENE_COLOR_FRAGMENT });
        surfaceVertexModule = device.createShaderModule({ label: "surface-vert", code: WGSL_SURFACE_VERTEX });
        surfaceFragmentModule = device.createShaderModule({ label: "surface-frag", code: WGSL_SURFACE_FRAGMENT });
        thickLineVertexModule = device.createShaderModule({ label: "thick-line-vert", code: WGSL_THICK_LINE_VERTEX });
        thickLineFragmentModule = device.createShaderModule({ label: "thick-line-frag", code: WGSL_THICK_LINE_FRAGMENT });
        pointsVertexModule = device.createShaderModule({ label: "points-vert", code: WGSL_POINTS_VERTEX });
        pointsInstancedVertexModule = device.createShaderModule({ label: "points-instanced-vert", code: WGSL_POINTS_INSTANCED_VERTEX });
        pointsFragmentModule = device.createShaderModule({ label: "points-frag", code: WGSL_POINTS_FRAGMENT });

        // Create persistent uniform buffers.
        // FrameUniforms: 2*mat4 + vec3 + u32 + 2*f32 + 2*u32 = 128+16+16 = ~160 bytes.
        frameUniformBuffer = device.createBuffer({ size: 256, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
        // 8 lights * 64 bytes = 512 bytes.
        lightStorageBuffer = device.createBuffer({ size: 512, usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST });
        fogUniformBuffer = device.createBuffer({ size: 32, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
        envUniformBuffer = device.createBuffer({ size: 48, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
        shadowUniformBuffer = device.createBuffer({ size: 256, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
        shadowFrameBuffer = device.createBuffer({ size: 64, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });

        // Create samplers.
        linearSampler = device.createSampler({
          magFilter: "linear",
          minFilter: "linear",
          mipmapFilter: "linear",
          addressModeU: "clamp-to-edge",
          addressModeV: "clamp-to-edge",
        });
        comparisonSampler = device.createSampler({
          compare: "less",
          magFilter: "linear",
          minFilter: "linear",
        });

        // Create 1x1 dummy shadow depth texture.
        dummyShadowTex = device.createTexture({
          size: [1, 1, 1],
          format: "depth24plus",
          usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
        });
        dummyShadowView = dummyShadowTex.createView();

        // Clear the dummy shadow texture to depth 1.0.
        var initEncoder = device.createCommandEncoder();
        initEncoder.beginRenderPass({
          colorAttachments: [],
          depthStencilAttachment: {
            view: dummyShadowView,
            depthLoadOp: "clear",
            depthClearValue: 1.0,
            depthStoreOp: "store",
          },
        }).end();
        device.queue.submit([initEncoder.finish()]);

        // Placeholder texture.
        placeholderTex = device.createTexture({
          size: [1, 1, 1],
          format: "rgba8unorm",
          usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST,
        });
        device.queue.writeTexture(
          { texture: placeholderTex },
          new Uint8Array([255, 255, 255, 255]),
          { bytesPerRow: 4 },
          [1, 1, 1]
        );
        placeholderView = placeholderTex.createView();
      } catch (err) {
        // Synchronous GPU resource setup failed — the probe said the
        // device was good, but something in the texture/buffer/shader
        // creation path failed anyway. Mark the renderer broken so
        // render() no-ops. The canvas is tainted at this point (we
        // already called getContext("webgpu") above), so the mount
        // code can't fall back to WebGL — but at least we log loudly
        // and stop doing broken work.
        initError = String(err && (err.message || err) || "unknown error");
        console.warn("[gosx] WebGPU synchronous init failed:", err);
        initFailed = true;
      }
    })();

    // Ensure main depth texture matches canvas size.
    function ensureMainDepth(width, height, sampleCount) {
      sampleCount = Math.max(1, Math.floor(sampleCount || 1));
      if (mainDepthTexture && mainDepthWidth === width && mainDepthHeight === height && mainDepthSampleCount === sampleCount) return;
      if (mainDepthTexture) mainDepthTexture.destroy();
      mainDepthTexture = device.createTexture({
        size: [width, height, 1],
        format: "depth24plus",
        sampleCount: sampleCount,
        usage: GPUTextureUsage.RENDER_ATTACHMENT,
      });
      mainDepthView = mainDepthTexture.createView();
      mainDepthWidth = width;
      mainDepthHeight = height;
      mainDepthSampleCount = sampleCount;
    }

    function ensureMSAAColor(width, height, sampleCount) {
      sampleCount = Math.max(1, Math.floor(sampleCount || 1));
      if (sampleCount <= 1) return null;
      if (
        mainMSAATexture &&
        mainMSAAWidth === width &&
        mainMSAAHeight === height &&
        mainMSAASampleCount === sampleCount
      ) {
        return mainMSAAView;
      }
      if (mainMSAATexture) mainMSAATexture.destroy();
      mainMSAATexture = device.createTexture({
        size: [width, height, 1],
        format: targetFormat,
        sampleCount: sampleCount,
        usage: GPUTextureUsage.RENDER_ATTACHMENT,
      });
      mainMSAAView = mainMSAATexture.createView();
      mainMSAAWidth = width;
      mainMSAAHeight = height;
      mainMSAASampleCount = sampleCount;
      return mainMSAAView;
    }

    // Get or create a PBR pipeline for the given blend mode.
    function getPBRPipeline(blendMode, depthWrite) {
      var key = wgpuPipelineKey("pbr", blendMode, depthWrite, targetFormat, "depth24plus", activeSampleCount);
      if (pipelineCache[key]) return pipelineCache[key];
      var pipeline = wgpuCreatePBRPipeline(device, pbrPipelineLayout, pbrVertexModule, pbrFragmentModule, blendMode, depthWrite, targetFormat, activeSampleCount);
      pipelineCache[key] = pipeline;
      return pipeline;
    }

    function getPBRInstancedPipeline(blendMode, depthWrite) {
      var key = wgpuPipelineKey("pbr-instanced", blendMode, depthWrite, targetFormat, "depth24plus", activeSampleCount);
      if (pipelineCache[key]) return pipelineCache[key];
      var pipeline = wgpuCreatePBRInstancedPipeline(device, pbrPipelineLayout, pbrInstancedVertexModule, pbrFragmentModule, blendMode, depthWrite, targetFormat, activeSampleCount);
      pipelineCache[key] = pipeline;
      return pipeline;
    }

    function getSceneColorPipeline(space, topology, blendMode, depthWrite) {
      var normalizedSpace = space === "clip" ? "clip" : "world";
      var normalizedTopology = topology === "triangle-list" ? "triangle-list" : "line-list";
      var key = wgpuPipelineKey("scene-color-" + normalizedSpace + "-" + normalizedTopology, blendMode, depthWrite, targetFormat, "depth24plus", activeSampleCount);
      if (pipelineCache[key]) return pipelineCache[key];
      var vertexModule = normalizedSpace === "clip" ? sceneClipColorVertexModule : sceneWorldColorVertexModule;
      var pipeline = wgpuCreateSceneColorPipeline(device, device.createPipelineLayout({ bindGroupLayouts: [frameBindGroupLayout] }), vertexModule, sceneColorFragmentModule, normalizedTopology, blendMode, depthWrite, targetFormat, activeSampleCount);
      pipelineCache[key] = pipeline;
      return pipeline;
    }

    function getSurfacePipeline(blendMode, depthWrite) {
      var key = wgpuPipelineKey("surface", blendMode, depthWrite, targetFormat, "depth24plus", activeSampleCount);
      if (pipelineCache[key]) return pipelineCache[key];
      var pipeline = wgpuCreateSurfacePipeline(device, pbrPipelineLayout, surfaceVertexModule, surfaceFragmentModule, blendMode, depthWrite, targetFormat, activeSampleCount);
      pipelineCache[key] = pipeline;
      return pipeline;
    }

    function sceneSelenaMaterialLayout(material) {
      var layout = material && material.shaderLayout;
      if (!layout || typeof layout !== "object") return null;
      if (!layout.uniformBlock || typeof layout.uniformBlock !== "object") return null;
      if (!Array.isArray(layout.uniformBlock.fields)) return null;
      return layout;
    }

    function sceneSelenaIsMaterial(material) {
      return Boolean(
        material &&
        material.shaderBackend === "selena" &&
        sceneSelenaMaterialLayout(material) &&
        (
          (typeof material.customVertexWGSL === "string" && material.customVertexWGSL.trim()) ||
          (typeof material.customFragmentWGSL === "string" && material.customFragmentWGSL.trim())
        )
      );
    }

    function sceneSelenaWGSLSource(material) {
      var src = typeof material.customVertexWGSL === "string" && material.customVertexWGSL.trim()
        ? material.customVertexWGSL
        : material.customFragmentWGSL;
      return String(src || "").trim();
    }

    function sceneSelenaFloatCount(type) {
      switch (String(type || "")) {
      case "float": return 1;
      case "vec2": return 2;
      case "vec3": return 3;
      case "vec4": return 4;
      case "mat3": return 9;
      case "mat4": return 16;
      default: return 1;
      }
    }

    function sceneSelenaAttributeComponents(type) {
      switch (String(type || "")) {
      case "vec2": return 2;
      case "vec4": return 4;
      case "vec3":
      default:
        return 3;
      }
    }

    function sceneSelenaWGPUFormat(type) {
      switch (sceneSelenaAttributeComponents(type)) {
      case 2: return "float32x2";
      case 4: return "float32x4";
      default: return "float32x3";
      }
    }

    function sceneSelenaUniformDefault(layout, name) {
      var defaults = layout && layout.uniformBlock && Array.isArray(layout.uniformBlock.defaults)
        ? layout.uniformBlock.defaults
        : [];
      for (var i = 0; i < defaults.length; i++) {
        if (defaults[i] && defaults[i].name === name) {
          return defaults[i].values;
        }
      }
      return undefined;
    }

    function sceneSelenaUniformValue(material, layout, field) {
      var name = field && field.name;
      if (name === "mvp") return scratchSelenaViewProjection;
      if (name === "normalMatrix") return [1, 0, 0, 0, 1, 0, 0, 0, 1];
      var values = material && material.customUniforms;
      if (values && typeof values === "object" && Object.prototype.hasOwnProperty.call(values, name)) {
        return values[name];
      }
      var def = sceneSelenaUniformDefault(layout, name);
      if (def !== undefined) return def;
      var count = sceneSelenaFloatCount(field && field.type);
      if (count === 16) return pointsIdentityMatrix;
      if (count === 9) return [1, 0, 0, 0, 1, 0, 0, 0, 1];
      return 0;
    }

    function sceneSelenaScalar(value) {
      if (Array.isArray(value) || (value && typeof value.length === "number")) {
        return sceneNumber(value[0], 0);
      }
      return sceneNumber(value, 0);
    }

    function sceneSelenaWriteUniformField(f32, base, type, value) {
      var count = sceneSelenaFloatCount(type);
      if (type === "float") {
        f32[base] = sceneSelenaScalar(value);
        return;
      }
      if (type === "mat3") {
        for (var c = 0; c < 3; c++) {
          f32[base + c * 4] = sceneNumber(value && value[c * 3], c === 0 ? 1 : 0);
          f32[base + c * 4 + 1] = sceneNumber(value && value[c * 3 + 1], c === 1 ? 1 : 0);
          f32[base + c * 4 + 2] = sceneNumber(value && value[c * 3 + 2], c === 2 ? 1 : 0);
        }
        return;
      }
      for (var i = 0; i < count; i++) {
        f32[base + i] = sceneNumber(value && value[i], 0);
      }
    }

    function sceneSelenaUniformData(material) {
      var layout = sceneSelenaMaterialLayout(material);
      if (!layout) return null;
      var size = Math.max(16, Math.floor(sceneNumber(layout.uniformBlock.size, 16)));
      var f32 = new Float32Array(Math.ceil(size / 4));
      var fields = layout.uniformBlock.fields;
      for (var i = 0; i < fields.length; i++) {
        var field = fields[i];
        if (!field || typeof field.name !== "string") continue;
        sceneSelenaWriteUniformField(
          f32,
          Math.floor(sceneNumber(field.offset, 0) / 4),
          String(field.type || "float"),
          sceneSelenaUniformValue(material, layout, field)
        );
      }
      return f32;
    }

    function sceneSelenaTextureURL(material, texture, index) {
      var name = texture && texture.name;
      var values = material && material.customUniforms;
      if (values && typeof values === "object" && name && typeof values[name] === "string" && values[name].trim()) {
        return values[name].trim();
      }
      if (material && name && typeof material[name] === "string" && material[name].trim()) {
        return material[name].trim();
      }
      if (index === 0 && material && typeof material.texture === "string" && material.texture.trim()) {
        return material.texture.trim();
      }
      return "";
    }

    function sceneSelenaTextureDescriptors(layout) {
      return layout && Array.isArray(layout.textures) ? layout.textures : [];
    }

    function sceneSelenaBindGroupLayout(device, layout) {
      var visibility = typeof GPUShaderStage !== "undefined"
        ? (GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT)
        : 3;
      var entries = [{
        binding: sceneNumber(layout && layout.wgsl && layout.wgsl.binding, 0),
        visibility: visibility,
        buffer: { type: "uniform", minBindingSize: Math.max(16, Math.floor(sceneNumber(layout && layout.uniformBlock && layout.uniformBlock.size, 16))) },
      }];
      var textures = sceneSelenaTextureDescriptors(layout);
      for (var i = 0; i < textures.length; i++) {
        var wgsl = textures[i] && textures[i].wgsl || {};
        entries.push({
          binding: sceneNumber(wgsl.textureBinding, 1 + i * 2),
          visibility: typeof GPUShaderStage !== "undefined" ? GPUShaderStage.FRAGMENT : 2,
          texture: { sampleType: "float", viewDimension: "2d" },
        });
        entries.push({
          binding: sceneNumber(wgsl.samplerBinding, 2 + i * 2),
          visibility: typeof GPUShaderStage !== "undefined" ? GPUShaderStage.FRAGMENT : 2,
          sampler: { type: "filtering" },
        });
      }
      return device.createBindGroupLayout({ label: "gosx-selena-material", entries: entries });
    }

    function sceneSelenaAttributeSource(name) {
      switch (name) {
      case "position": return "positions";
      case "normal": return "normals";
      case "uv": return "uvs";
      default: return "";
      }
    }

    function sceneSelenaPipelineAttributes(layout) {
      var attrs = Array.isArray(layout && layout.attributes) ? layout.attributes : [];
      var out = [];
      for (var i = 0; i < attrs.length; i++) {
        var attr = attrs[i] || {};
        var source = sceneSelenaAttributeSource(attr.name);
        if (!source) continue;
        out.push({
          name: attr.name,
          source: source,
          slot: out.length,
          components: sceneSelenaAttributeComponents(attr.type),
          shaderLocation: Math.max(0, Math.floor(sceneNumber(attr.location, out.length))),
          format: sceneSelenaWGPUFormat(attr.type),
        });
      }
      return out;
    }

    function getSelenaPipeline(material, blendMode, depthWrite) {
      if (!sceneSelenaIsMaterial(material)) return null;
      var layout = sceneSelenaMaterialLayout(material);
      var shader = sceneSelenaWGSLSource(material);
      var key = [
        "selena",
        material.key || sceneMaterialProfileKey(material),
        blendMode,
        depthWrite ? "1" : "0",
        targetFormat,
        activeSampleCount,
      ].join("|");
      var cached = selenaPipelineCache.get(key);
      if (cached) return cached.failed ? null : cached;
      try {
        var bindGroupLayout = sceneSelenaBindGroupLayout(device, layout);
        var pipelineLayout = device.createPipelineLayout({ bindGroupLayouts: [bindGroupLayout] });
        var module = device.createShaderModule({ label: "selena-material", code: shader });
        var attrs = sceneSelenaPipelineAttributes(layout);
        var buffers = attrs.map(function(attr) {
          return {
            arrayStride: attr.components * 4,
            stepMode: "vertex",
            attributes: [{ format: attr.format, offset: 0, shaderLocation: attr.shaderLocation }],
          };
        });
        var pipeline = device.createRenderPipeline({
          label: "gosx-selena-" + (layout.material || "material") + "-" + blendMode,
          layout: pipelineLayout,
          vertex: { module: module, entryPoint: "vertexMain", buffers: buffers },
          fragment: { module: module, entryPoint: "fragmentMain", targets: [{ format: targetFormat, blend: wgpuBlendState(blendMode) }] },
          primitive: { topology: "triangle-list", cullMode: "back" },
          multisample: { count: Math.max(1, Math.floor(activeSampleCount || 1)) },
          depthStencil: { format: "depth24plus", depthWriteEnabled: depthWrite, depthCompare: "less-equal" },
        });
        cached = { pipeline: pipeline, bindGroupLayout: bindGroupLayout, layout: layout, attrs: attrs };
        selenaPipelineCache.set(key, cached);
        return cached;
      } catch (err) {
        console.warn("[gosx] Selena WebGPU shader pipeline failed; falling back to PBR material.", err);
        selenaPipelineCache.set(key, { failed: true });
        return null;
      }
    }

    function createSelenaBindGroup(material, resource, cacheOwner) {
      var uniformData = sceneSelenaUniformData(material);
      if (!uniformData || !resource) return null;
      var owner = (cacheOwner && typeof cacheOwner === "object") ? cacheOwner : material;
      var uniformBuffer = wgpuCachedTrackedBuffer(
        owner,
        "_gosxWGPUSelenaUniform",
        uniformData,
        GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
        true
      );
      var entries = [{
        binding: sceneNumber(resource.layout && resource.layout.wgsl && resource.layout.wgsl.binding, 0),
        resource: { buffer: uniformBuffer },
      }];
      var textures = sceneSelenaTextureDescriptors(resource.layout);
      for (var i = 0; i < textures.length; i++) {
        var tex = textures[i] || {};
        var url = sceneSelenaTextureURL(material, tex, i);
        var record = url ? wgpuLoadTexture(device, url, textureCache) : null;
        var view = record && record.view ? record.view : placeholderView;
        var wgsl = tex.wgsl || {};
        entries.push({ binding: sceneNumber(wgsl.textureBinding, 1 + i * 2), resource: view });
        entries.push({ binding: sceneNumber(wgsl.samplerBinding, 2 + i * 2), resource: linearSampler });
      }
      return device.createBindGroup({ layout: resource.bindGroupLayout, entries: entries });
    }

    function getThickLinePipeline(blendMode, depthWrite) {
      var key = wgpuPipelineKey("thick-line", blendMode, depthWrite, targetFormat, "depth24plus", activeSampleCount);
      if (pipelineCache[key]) return pipelineCache[key];
      var pipeline = wgpuCreateThickLinePipeline(device, device.createPipelineLayout({ bindGroupLayouts: [frameBindGroupLayout] }), thickLineVertexModule, thickLineFragmentModule, blendMode, depthWrite, targetFormat, activeSampleCount);
      pipelineCache[key] = pipeline;
      return pipeline;
    }

    // Get or create a shadow pipeline.
    var shadowPipeline = null;
    function getShadowPipeline() {
      if (shadowPipeline) return shadowPipeline;
      shadowPipeline = wgpuCreateShadowPipeline(device, shadowBindGroupLayout, shadowVertexModule);
      return shadowPipeline;
    }

    var shadowInstancedPipeline = null;
    function getShadowInstancedPipeline() {
      if (shadowInstancedPipeline) return shadowInstancedPipeline;
      shadowInstancedPipeline = wgpuCreateShadowInstancedPipeline(device, shadowBindGroupLayout, shadowInstancedVertexModule);
      return shadowInstancedPipeline;
    }

    // Get or create a points pipeline for the given blend mode.
    function getPointsPipeline(blendMode, depthWrite) {
      var key = wgpuPipelineKey("points", blendMode, depthWrite, targetFormat, "depth24plus", activeSampleCount);
      if (pipelineCache[key]) return pipelineCache[key];
      var pipeline = wgpuCreatePointsPipeline(device, pointsPipelineLayout, pointsVertexModule, pointsFragmentModule, blendMode, depthWrite, targetFormat, activeSampleCount);
      pipelineCache[key] = pipeline;
      return pipeline;
    }

    function getPointsVertexPipeline(blendMode, depthWrite) {
      var key = wgpuPipelineKey("points-vertex", blendMode, depthWrite, targetFormat, "depth24plus", activeSampleCount);
      if (pipelineCache[key]) return pipelineCache[key];
      var pipeline = wgpuCreatePointsVertexPipeline(device, pointsVertexPipelineLayout, pointsInstancedVertexModule, pointsFragmentModule, blendMode, depthWrite, targetFormat, activeSampleCount);
      pipelineCache[key] = pipeline;
      return pipeline;
    }

    function disposeComputeParticleSystems() {
      for (const record of computeParticleSystems.values()) {
        if (record && record.system && typeof record.system.dispose === "function") {
          record.system.dispose();
        }
      }
      computeParticleSystems.clear();
      lastComputeParticleTimeSeconds = null;
    }

    function syncComputeParticleSystems(entries) {
      var activeIds = new Set();
      var records = [];
      var sourceEntries = Array.isArray(entries) ? entries : [];
      for (var i = 0; i < sourceEntries.length; i++) {
        var entry = sourceEntries[i];
        if (!entry || typeof entry !== "object") continue;
        var id = typeof entry.id === "string" && entry.id ? entry.id : ("scene-particles-" + i);
        var signature = sceneComputeSystemSignature(entry);
        activeIds.add(id);
        var record = computeParticleSystems.get(id);
        if (!record || record.signature !== signature) {
          if (record && record.system && typeof record.system.dispose === "function") {
            record.system.dispose();
          }
          record = {
            signature: signature,
            system: createSceneParticleSystem(device, entry),
          };
          computeParticleSystems.set(id, record);
        } else if (record.system) {
          record.system.entry = entry;
        }
        if (record && record.system) {
          records.push(record);
        }
      }
      for (const [id, record] of computeParticleSystems.entries()) {
        if (!activeIds.has(id)) {
          if (record && record.system && typeof record.system.dispose === "function") {
            record.system.dispose();
          }
          computeParticleSystems.delete(id);
        }
      }
      return records;
    }

    function updateComputeParticleSystems(entries, encoder, timeSeconds) {
      var currentTime = Number.isFinite(timeSeconds) ? timeSeconds : 0;
      var deltaTime = lastComputeParticleTimeSeconds == null
        ? 0
        : Math.max(0, Math.min(0.1, currentTime - lastComputeParticleTimeSeconds));
      lastComputeParticleTimeSeconds = currentTime;
      var records = syncComputeParticleSystems(entries);
      for (var i = 0; i < records.length; i++) {
        if (records[i].system && typeof records[i].system.update === "function") {
          records[i].system.update(device, encoder, deltaTime, currentTime);
        }
      }
      return records;
    }

    // -----------------------------------------------------------------------
    // Uniform upload helpers
    // -----------------------------------------------------------------------

    function uploadFrameUniforms(camera, width, height, toneMap) {
      var cam = sceneRenderCamera(camera);
      var aspect = Math.max(0.0001, width / Math.max(1, height));
      scenePBRViewMatrix(cam, scratchViewMatrix);
      if (typeof scenePBRProjectionMatrixForCamera === "function") {
        scenePBRProjectionMatrixForCamera(cam, aspect, scratchProjMatrix);
      } else {
        scenePBRProjectionMatrix(cam.fov, aspect, cam.near, cam.far, scratchProjMatrix);
      }

      // scenePBRProjectionMatrix produces a WebGL-convention matrix whose
      // clip-z range is [-w, w]. WebGPU's clip volume keeps z in [0, w],
      // so without this remap every primitive in the front half of the
      // frustum is silently clipped. Pre-multiplying by the depth-remap
      // matrix R (row 2 = 0.5 * (row 2 + row 3)) converts to WebGPU clip
      // space. Affects every WebGPU pipeline that consumes frame.projMatrix
      // (PBR meshes, world lines, surfaces, points, compute particles).
      scratchProjMatrix[2]  = 0.5 * (scratchProjMatrix[2]  + scratchProjMatrix[3]);
      scratchProjMatrix[6]  = 0.5 * (scratchProjMatrix[6]  + scratchProjMatrix[7]);
      scratchProjMatrix[10] = 0.5 * (scratchProjMatrix[10] + scratchProjMatrix[11]);
      scratchProjMatrix[14] = 0.5 * (scratchProjMatrix[14] + scratchProjMatrix[15]);
      sceneMat4MultiplyInto(scratchSelenaViewProjection, scratchProjMatrix, scratchViewMatrix);

      // FrameUniforms layout (std140):
      // mat4x4f viewMatrix:  0  (64 bytes)
      // mat4x4f projMatrix:  64 (64 bytes)
      // vec3f cameraPos:     128 (12 bytes)
      // u32 lightCount:      140 (4 bytes)
      // f32 viewportWidth:   144 (4 bytes)
      // f32 viewportHeight:  148 (4 bytes)
      // u32 toneMap:         152 (4 bytes)
      // u32 _pad:            156 (4 bytes)
      var data = new ArrayBuffer(160);
      var f = new Float32Array(data);
      var u = new Uint32Array(data);
      f.set(scratchViewMatrix, 0);          // offset 0
      f.set(scratchProjMatrix, 16);         // offset 64
      f[32] = cam.x;                         // cameraPos.x
      f[33] = cam.y;                         // cameraPos.y
      f[34] = -cam.z;                        // cameraPos.z (negated per convention)
      // lightCount set below in uploadLights
      f[36] = width;                          // viewportWidth
      f[37] = height;                         // viewportHeight
      u[38] = toneMap ? 1 : 0;               // toneMap
      u[39] = 0;                              // pad

      device.queue.writeBuffer(frameUniformBuffer, 0, new Float32Array(data));
      return cam;
    }

    function uploadLights(lights) {
      var lightArray = Array.isArray(lights) ? lights : [];
      var count = Math.min(lightArray.length, 8);

      // Write lightCount into frame uniform buffer at byte offset 140.
      var countBuf = new Uint32Array([count]);
      device.queue.writeBuffer(frameUniformBuffer, 140, countBuf);

      // Each light: 4 * vec4f = 64 bytes.
      var lightData = new Float32Array(8 * 16);
      var colorCache = {};

      for (var i = 0; i < count; i++) {
        var light = lightArray[i];
        var kind = typeof light.kind === "string" ? light.kind.toLowerCase() : "";
        var lightType = 2; // point
        if (kind === "ambient") lightType = 0;
        else if (kind === "directional") lightType = 1;

        var base = i * 16;
        // position (xyz) + type (w, stored as float, cast to u32 in WGSL)
        lightData[base + 0] = sceneNumber(light.x, 0);
        lightData[base + 1] = sceneNumber(light.y, 0);
        lightData[base + 2] = sceneNumber(light.z, 0);
        lightData[base + 3] = lightType;

        // direction (xyz) + intensity (w)
        lightData[base + 4] = sceneNumber(light.directionX, 0);
        lightData[base + 5] = sceneNumber(light.directionY, -1);
        lightData[base + 6] = sceneNumber(light.directionZ, 0);
        lightData[base + 7] = sceneNumber(light.intensity, 1);

        // color (rgb) + range (a)
        var colorKey = light.color;
        var lc = typeof colorKey === "string" && colorCache[colorKey];
        if (!lc) {
          lc = sceneColorRGBA(light.color, [1, 1, 1, 1]);
          if (typeof colorKey === "string") colorCache[colorKey] = lc;
        }
        lightData[base + 8] = lc[0];
        lightData[base + 9] = lc[1];
        lightData[base + 10] = lc[2];
        lightData[base + 11] = sceneNumber(light.range, 0);

        // params: decay, shadowBias, castShadow, unused
        lightData[base + 12] = sceneNumber(light.decay, 2);
        lightData[base + 13] = sceneNumber(light.shadowBias, 0.005);
        lightData[base + 14] = light.castShadow ? 1.0 : 0.0;
        lightData[base + 15] = 0;
      }

      device.queue.writeBuffer(lightStorageBuffer, 0, lightData);
    }

    function uploadFogUniforms(environment) {
      var env = environment || {};
      var fogDensity = sceneNumber(env.fogDensity, 0);
      var fogColorRGBA = sceneColorRGBA(env.fogColor, [0.5, 0.5, 0.5, 1]);

      // FogUniforms: vec3f fogColor(12) + f32 density(4) + u32 hasFog(4) + pad(12) = 32 bytes.
      var data = new ArrayBuffer(32);
      var f = new Float32Array(data);
      var u = new Uint32Array(data);
      f[0] = fogColorRGBA[0];
      f[1] = fogColorRGBA[1];
      f[2] = fogColorRGBA[2];
      f[3] = fogDensity;
      u[4] = fogDensity > 0 ? 1 : 0;
      u[5] = 0;
      u[6] = 0;
      u[7] = 0;
      device.queue.writeBuffer(fogUniformBuffer, 0, new Float32Array(data));
    }

    function uploadEnvUniforms(environment) {
      var env = environment || {};
      var ambientColorRGBA = sceneColorRGBA(env.ambientColor, [1, 1, 1, 1]);
      var skyColorRGBA = sceneColorRGBA(env.skyColor, [0.88, 0.94, 1, 1]);
      var groundColorRGBA = sceneColorRGBA(env.groundColor, [0.12, 0.16, 0.22, 1]);

      // EnvUniforms: vec3f + f32 + vec3f + f32 + vec3f + f32 = 48 bytes.
      var data = new Float32Array(12);
      data[0] = ambientColorRGBA[0]; data[1] = ambientColorRGBA[1]; data[2] = ambientColorRGBA[2];
      data[3] = sceneNumber(env.ambientIntensity, 0);
      data[4] = skyColorRGBA[0]; data[5] = skyColorRGBA[1]; data[6] = skyColorRGBA[2];
      data[7] = sceneNumber(env.skyIntensity, 0);
      data[8] = groundColorRGBA[0]; data[9] = groundColorRGBA[1]; data[10] = groundColorRGBA[2];
      data[11] = sceneNumber(env.groundIntensity, 0);
      device.queue.writeBuffer(envUniformBuffer, 0, data);
    }

    function uploadShadowUniforms(shadowLightMatrices, shadowLightIndices, lights) {
      var lightArray = Array.isArray(lights) ? lights : [];
      // ShadowUniforms: mat4(64) + mat4(64) + 6*u32(24) + pad(8) = 160. Round up to 256.
      var data = new ArrayBuffer(160);
      var f = new Float32Array(data);
      var u = new Uint32Array(data);
      var i = new Int32Array(data);

      if (shadowLightMatrices[0]) {
        f.set(shadowLightMatrices[0], 0);   // lightSpaceMatrix0 @ offset 0
      }
      if (shadowLightMatrices[1]) {
        f.set(shadowLightMatrices[1], 16);  // lightSpaceMatrix1 @ offset 64
      }

      u[32] = shadowLightMatrices[0] ? 1 : 0;  // hasShadow0
      u[33] = shadowLightMatrices[1] ? 1 : 0;  // hasShadow1

      var bias0 = 0.005;
      if (shadowLightIndices[0] >= 0 && lightArray[shadowLightIndices[0]]) {
        bias0 = sceneNumber(lightArray[shadowLightIndices[0]].shadowBias, 0.005);
      }
      f[34] = bias0;  // shadowBias0

      var bias1 = 0.005;
      if (shadowLightIndices[1] >= 0 && lightArray[shadowLightIndices[1]]) {
        bias1 = sceneNumber(lightArray[shadowLightIndices[1]].shadowBias, 0.005);
      }
      f[35] = bias1;  // shadowBias1

      i[36] = shadowLightIndices[0];  // shadowLightIndex0
      i[37] = shadowLightIndices[1];  // shadowLightIndex1
      u[38] = 0; // pad
      u[39] = 0; // pad

      device.queue.writeBuffer(shadowUniformBuffer, 0, new Float32Array(data));
    }

    function materialUniformData(material, receiveShadow) {
      var mat = material || {};
      var albedoRGBA = sceneColorRGBA(mat.color, [0.8, 0.8, 0.8, 1]);

      // MaterialUniforms: vec3f + 9*f32 + 8*u32 = 80 bytes.
      var data = new ArrayBuffer(80);
      var f = new Float32Array(data);
      var u = new Uint32Array(data);

      f[0] = albedoRGBA[0]; f[1] = albedoRGBA[1]; f[2] = albedoRGBA[2];
      f[3] = sceneNumber(mat.roughness, 0.5);
      f[4] = sceneNumber(mat.metalness, 0);
      f[5] = sceneNumber(mat.emissive, 0);
      f[6] = clamp01(sceneNumber(mat.opacity, 1));
      f[7] = clamp01(sceneNumber(mat.clearcoat, 0));
      f[8] = clamp01(sceneNumber(mat.sheen, 0));
      f[9] = clamp01(sceneNumber(mat.transmission, 0));
      f[10] = clamp01(sceneNumber(mat.iridescence, 0));
      f[11] = Math.max(-1, Math.min(1, sceneNumber(mat.anisotropy, 0)));
      u[12] = mat.unlit ? 1 : 0;

      u[18] = receiveShadow ? 1 : 0;
      u[19] = 0;
      return { data: new Float32Array(data), u: u };
    }

    function createMaterialBindGroup(material, receiveShadow, cacheOwner) {
      var mat = material || {};
      var uniform = materialUniformData(mat, receiveShadow);
      var u = uniform.u;
      // Texture records.
      var textureMaps = [
        { prop: "texture",      index: 13 },
        { prop: "normalMap",    index: 14 },
        { prop: "roughnessMap", index: 15 },
        { prop: "metalnessMap", index: 16 },
        { prop: "emissiveMap",  index: 17 },
      ];

      var texViews = [];
      for (var ti = 0; ti < textureMaps.length; ti++) {
        var tm = textureMaps[ti];
        var record = mat[tm.prop] ? wgpuLoadTexture(device, mat[tm.prop], textureCache) : null;
        var loaded = Boolean(record && record.loaded);
        u[tm.index] = loaded ? 1 : 0;
        texViews.push(loaded ? record.view : placeholderView);
      }

      var owner = (cacheOwner && typeof cacheOwner === "object")
        ? cacheOwner
        : ((material && typeof material === "object") ? material : defaultMaterialOwner);
      var slot = receiveShadow ? "_gosxWGPUMaterialShadowUniform" : "_gosxWGPUMaterialUniform";
      var materialBuffer = wgpuCachedTrackedBuffer(
        owner,
        slot,
        uniform.data,
        GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
        true
      );

      // Create bind group with texture views and sampler.
      return device.createBindGroup({
        layout: materialBindGroupLayout,
        entries: [
          { binding: 0, resource: { buffer: materialBuffer } },
          { binding: 1, resource: texViews[0] },
          { binding: 2, resource: linearSampler },
          { binding: 3, resource: texViews[1] },
          { binding: 4, resource: linearSampler },
          { binding: 5, resource: texViews[2] },
          { binding: 6, resource: linearSampler },
          { binding: 7, resource: texViews[3] },
          { binding: 8, resource: linearSampler },
          { binding: 9, resource: texViews[4] },
          { binding: 10, resource: linearSampler },
        ],
      });
    }

    function createFrameBindGroup(shadowView0, shadowView1) {
      return device.createBindGroup({
        layout: frameBindGroupLayout,
        entries: [
          { binding: 0, resource: { buffer: frameUniformBuffer } },
          { binding: 1, resource: { buffer: lightStorageBuffer } },
          { binding: 2, resource: { buffer: fogUniformBuffer } },
          { binding: 3, resource: { buffer: envUniformBuffer } },
          { binding: 4, resource: shadowView0 || dummyShadowView },
          { binding: 5, resource: comparisonSampler },
          { binding: 6, resource: shadowView1 || dummyShadowView },
          { binding: 7, resource: comparisonSampler },
          { binding: 8, resource: { buffer: shadowUniformBuffer } },
        ],
      });
    }

    function webGPUObjectVertexCount(obj) {
      return Math.max(0, Math.floor(sceneNumber(obj && obj.vertexCount, obj && obj.vertices && obj.vertices.count || 0)));
    }

    function webGPUDirectAttribute(obj, key, count, tupleSize) {
      var vertices = obj && obj.vertices;
      var data = vertices && vertices[key];
      var required = Math.max(0, Math.floor(sceneNumber(count, 0))) * Math.max(1, tupleSize);
      if (!vertices || required <= 0 || !data || typeof data.length !== "number" || data.length < required) {
        return null;
      }
      if (!(data instanceof Float32Array)) {
        data = new Float32Array(data);
        vertices[key] = data;
      }
      if (data.length === required) {
        return data;
      }
      var views = vertices._wgpuAttributeViews;
      if (!views) {
        views = Object.create(null);
        vertices._wgpuAttributeViews = views;
      }
      var viewKey = key + ":" + required;
      var record = views[viewKey];
      if (!record || record.source !== data) {
        record = {
          source: data,
          view: data.subarray(0, required),
        };
        views[viewKey] = record;
      }
      return record.view;
    }

    function webGPUDefaultAttributeData(obj, key, count, tupleSize, defaults) {
      var direct = webGPUDirectAttribute(obj, key, count, tupleSize);
      if (direct) return direct;
      var stride = Math.max(1, tupleSize);
      var data = ensureScratch(key, Math.max(0, count * stride));
      for (var i = 0; i < count; i++) {
        for (var c = 0; c < stride; c++) {
          data[i * stride + c] = sceneNumber(defaults && defaults[c], 0);
        }
      }
      return data.subarray(0, count * stride);
    }

    function webGPUObjectIsSkinned(obj) {
      var count = webGPUObjectVertexCount(obj);
      var vertices = obj && obj.vertices;
      var skin = obj && obj.skin;
      var jointMatrices = skin && skin.jointMatrices;
      return Boolean(
        count > 0 &&
        vertices &&
        skin &&
        jointMatrices &&
        typeof jointMatrices.length === "number" &&
        jointMatrices.length >= 16 &&
        webGPUDirectAttribute(obj, "positions", count, 3) &&
        webGPUDirectAttribute(obj, "joints", count, 4) &&
        webGPUDirectAttribute(obj, "weights", count, 4)
      );
    }

    function webGPUObjectModelMatrix(obj) {
      var matrix = obj && obj.modelMatrix;
      return matrix && typeof matrix.length === "number" && matrix.length >= 16
        ? matrix
        : pointsIdentityMatrix;
    }

    function webGPUMat4MultiplyInto(out, outOffset, a, b, bOffset) {
      for (var col = 0; col < 4; col++) {
        var bi = bOffset + col * 4;
        var b0 = sceneNumber(b[bi], col === 0 ? 1 : 0);
        var b1 = sceneNumber(b[bi + 1], col === 1 ? 1 : 0);
        var b2 = sceneNumber(b[bi + 2], col === 2 ? 1 : 0);
        var b3 = sceneNumber(b[bi + 3], col === 3 ? 1 : 0);
        out[outOffset + col * 4] = sceneNumber(a[0], 1) * b0 + sceneNumber(a[4], 0) * b1 + sceneNumber(a[8], 0) * b2 + sceneNumber(a[12], 0) * b3;
        out[outOffset + col * 4 + 1] = sceneNumber(a[1], 0) * b0 + sceneNumber(a[5], 1) * b1 + sceneNumber(a[9], 0) * b2 + sceneNumber(a[13], 0) * b3;
        out[outOffset + col * 4 + 2] = sceneNumber(a[2], 0) * b0 + sceneNumber(a[6], 0) * b1 + sceneNumber(a[10], 1) * b2 + sceneNumber(a[14], 0) * b3;
        out[outOffset + col * 4 + 3] = sceneNumber(a[3], 0) * b0 + sceneNumber(a[7], 0) * b1 + sceneNumber(a[11], 0) * b2 + sceneNumber(a[15], 1) * b3;
      }
    }

    function webGPUElioBoneData(obj, jointCount) {
      var skin = obj && obj.skin;
      var jointMatrices = skin && skin.jointMatrices;
      if (!skin || !jointMatrices || typeof jointMatrices.length !== "number" || jointCount <= 0) return null;
      var data = skin._gosxWGPUElioBoneData;
      if (!data || data.length !== jointCount * 16) {
        data = new Float32Array(jointCount * 16);
        skin._gosxWGPUElioBoneData = data;
      }
      var model = webGPUObjectModelMatrix(obj);
      for (var i = 0; i < jointCount; i++) {
        webGPUMat4MultiplyInto(data, i * 16, model, jointMatrices, i * 16);
      }
      return data;
    }

    function webGPUElioSkinVertexData(obj, count, paddedCount, jointCount) {
      var vertices = obj && obj.vertices;
      var positions = webGPUDirectAttribute(obj, "positions", count, 3);
      var joints = webGPUDirectAttribute(obj, "joints", count, 4);
      var weights = webGPUDirectAttribute(obj, "weights", count, 4);
      if (!vertices || !positions || !joints || !weights || count <= 0 || paddedCount <= 0) return null;
      var cache = vertices._gosxWGPUElioSkinVertexData;
      if (
        cache &&
        cache.positions === positions &&
        cache.joints === joints &&
        cache.weights === weights &&
        cache.count === count &&
        cache.paddedCount === paddedCount &&
        cache.jointCount === jointCount
      ) {
        return cache.data;
      }

      var stride = 44;
      var bytes = new Uint8Array(paddedCount * stride);
      var view = new DataView(bytes.buffer);
      var maxJoint = Math.max(0, jointCount - 1);
      for (var i = 0; i < paddedCount; i++) {
        var off = i * stride;
        if (i < count) {
          var p = i * 3;
          var q = i * 4;
          view.setFloat32(off, sceneNumber(positions[p], 0), true);
          view.setFloat32(off + 4, sceneNumber(positions[p + 1], 0), true);
          view.setFloat32(off + 8, sceneNumber(positions[p + 2], 0), true);
          var w0 = Math.max(0, sceneNumber(weights[q], 0));
          var w1 = Math.max(0, sceneNumber(weights[q + 1], 0));
          var w2 = Math.max(0, sceneNumber(weights[q + 2], 0));
          var w3 = Math.max(0, sceneNumber(weights[q + 3], 0));
          var sum = w0 + w1 + w2 + w3;
          if (sum <= 0.000001) {
            w0 = 1; w1 = 0; w2 = 0; w3 = 0;
          } else {
            w0 /= sum; w1 /= sum; w2 /= sum; w3 /= sum;
          }
          view.setFloat32(off + 12, w0, true);
          view.setFloat32(off + 16, w1, true);
          view.setFloat32(off + 20, w2, true);
          view.setFloat32(off + 24, w3, true);
          view.setUint32(off + 28, Math.min(maxJoint, Math.max(0, Math.floor(sceneNumber(joints[q], 0)))), true);
          view.setUint32(off + 32, Math.min(maxJoint, Math.max(0, Math.floor(sceneNumber(joints[q + 1], 0)))), true);
          view.setUint32(off + 36, Math.min(maxJoint, Math.max(0, Math.floor(sceneNumber(joints[q + 2], 0)))), true);
          view.setUint32(off + 40, Math.min(maxJoint, Math.max(0, Math.floor(sceneNumber(joints[q + 3], 0)))), true);
        } else {
          view.setFloat32(off + 12, 1, true);
        }
      }

      cache = {
        positions: positions,
        joints: joints,
        weights: weights,
        count: count,
        paddedCount: paddedCount,
        jointCount: jointCount,
        data: bytes,
      };
      vertices._gosxWGPUElioSkinVertexData = cache;
      return bytes;
    }

    function webGPUElioEnsureOutputBuffer(record, paddedCount) {
      var bytes = Math.max(4, paddedCount * 3 * 4);
      if (record.outputBuffer && wgpuTrackedBufferSize(record.outputBuffer) >= bytes) return record.outputBuffer;
      if (record.outputBuffer && typeof record.outputBuffer.destroy === "function") {
        pointsEntryGPUBuffers.delete(record.outputBuffer);
        record.outputBuffer.destroy();
      }
      record.outputBuffer = wgpuCreateTrackedBuffer(GPUBufferUsage.STORAGE | GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, bytes);
      return record.outputBuffer;
    }

    function webGPUElioSkinRecord(obj) {
      if (!webGPUObjectIsSkinned(obj) || !elioSkinPipeline || !elioSkinBindGroupLayout) return null;
      var count = webGPUObjectVertexCount(obj);
      var skin = obj.skin;
      var jointCount = Math.floor(sceneNumber(skin && skin.jointMatrices && skin.jointMatrices.length, 0) / 16);
      if (count <= 0 || jointCount <= 0) return null;
      var paddedCount = Math.max(64, Math.ceil(count / 64) * 64);
      var vertexData = webGPUElioSkinVertexData(obj, count, paddedCount, jointCount);
      var boneData = webGPUElioBoneData(obj, jointCount);
      if (!vertexData || !boneData) return null;

      var record = obj._gosxWGPUElioSkinRecord;
      if (!record) {
        record = {};
        obj._gosxWGPUElioSkinRecord = record;
      }

      var boneBuffer = wgpuCachedTrackedBuffer(
        skin,
        "_gosxWGPUElioSkinBoneBuffer",
        boneData,
        GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST,
        true
      );
      var vertexBuffer = wgpuCachedTrackedBuffer(
        obj.vertices,
        "_gosxWGPUElioSkinVertexBuffer",
        vertexData,
        GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST,
        false
      );
      var outputBuffer = webGPUElioEnsureOutputBuffer(record, paddedCount);
      if (!boneBuffer || !vertexBuffer || !outputBuffer) return null;

      if (
        !record.bindGroup ||
        record.boneBuffer !== boneBuffer ||
        record.vertexBuffer !== vertexBuffer ||
        record.outputBuffer !== outputBuffer
      ) {
        record.bindGroup = device.createBindGroup({
          layout: elioSkinBindGroupLayout,
          entries: [
            { binding: 0, resource: { buffer: boneBuffer } },
            { binding: 1, resource: { buffer: vertexBuffer } },
            { binding: 2, resource: { buffer: outputBuffer } },
          ],
        });
        record.boneBuffer = boneBuffer;
        record.vertexBuffer = vertexBuffer;
        record.outputBuffer = outputBuffer;
      }
      record.count = count;
      record.paddedCount = paddedCount;
      record.workgroups = Math.ceil(paddedCount / 64);
      obj._gosxWGPUElioSkinOutputBuffer = outputBuffer;
      return record;
    }

    function updateElioSkinnedMeshes(bundle, encoder) {
      var stats = {
        elioSkinningDispatches: 0,
        elioSkinningVertices: 0,
        elioSkinningKernel: "m31labs.dev/elio/stdlib.Skin",
      };
      var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
      var pass = null;
      for (var i = 0; i < objects.length; i++) {
        var obj = objects[i];
        if (!obj || obj.viewCulled || !webGPUObjectIsSkinned(obj)) continue;
        var record = webGPUElioSkinRecord(obj);
        if (!record) continue;
        if (!pass) {
          pass = encoder.beginComputePass({ label: "gosx-elio-skin-lbs" });
          pass.setPipeline(elioSkinPipeline);
        }
        pass.setBindGroup(0, record.bindGroup);
        pass.dispatchWorkgroups(record.workgroups);
        stats.elioSkinningDispatches += 1;
        stats.elioSkinningVertices += record.count;
      }
      if (pass) pass.end();
      return stats;
    }

    function webGPUComputedMorphArray(morph, key, count, components) {
      var required = Math.max(0, Math.floor(sceneNumber(count, 0))) * Math.max(1, Math.floor(sceneNumber(components, 1)));
      var source = morph && morph[key];
      if (!source || required <= 0 || typeof source.length !== "number" || source.length < required) return null;
      var typed = source instanceof Float32Array ? source : toSceneFloat32Array(source);
      if (typed !== source && morph) morph[key] = typed;
      return typed.length === required ? typed : typed.subarray(0, required);
    }

    function webGPUComputedMorphDefaultArray(morph, key, count, components, defaults) {
      var required = Math.max(0, Math.floor(sceneNumber(count, 0))) * Math.max(1, Math.floor(sceneNumber(components, 1)));
      if (required <= 0) return null;
      var data = morph && morph[key];
      if (!data || data.length !== required) {
        data = new Float32Array(required);
        var width = Math.max(1, Math.floor(sceneNumber(components, 1)));
        for (var i = 0; i < count; i++) {
          for (var c = 0; c < width; c++) {
            data[i * width + c] = sceneNumber(defaults && defaults[c], 0);
          }
        }
        if (morph) morph[key] = data;
      }
      return data;
    }

    function webGPUComputedMorphData(obj) {
      var morph = obj && obj.computedMorph;
      if (!morph || !computedMorphPipeline || !computedMorphBindGroupLayout) return null;
      var requested = Math.max(0, Math.floor(sceneNumber(morph.count, sceneNumber(obj && obj.vertexCount, 0))));
      var objCount = Math.max(0, Math.floor(sceneNumber(obj && obj.vertexCount, requested)));
      var count = Math.min(requested, objCount);
      var sourcePositions = webGPUComputedMorphArray(morph, "sourcePositions", count, 3);
      var targetPositions = webGPUComputedMorphArray(morph, "targetPositions", count, 3);
      if (!sourcePositions || !targetPositions || count <= 0) return null;
      var sourceNormals = webGPUComputedMorphArray(morph, "sourceNormals", count, 3) ||
        webGPUComputedMorphDefaultArray(morph, "_defaultSourceNormals", count, 3, [0, 0, 1]);
      var targetNormals = webGPUComputedMorphArray(morph, "targetNormals", count, 3) || sourceNormals;
      var sourceTangents = webGPUComputedMorphArray(morph, "sourceTangents", count, 4) ||
        webGPUComputedMorphDefaultArray(morph, "_defaultSourceTangents", count, 4, [1, 0, 0, 1]);
      var targetTangents = webGPUComputedMorphArray(morph, "targetTangents", count, 4) || sourceTangents;
      if (!sourceNormals || !targetNormals || !sourceTangents || !targetTangents) return null;
      return {
        morph: morph,
        count: count,
        sourcePositions: sourcePositions,
        targetPositions: targetPositions,
        sourceNormals: sourceNormals,
        targetNormals: targetNormals,
        sourceTangents: sourceTangents,
        targetTangents: targetTangents,
      };
    }

    function webGPUComputedMorphUniformData(obj, morph, count) {
      var data = morph && morph._gosxWGPUComputedMorphUniformData;
      if (!data || data.length !== 20) {
        data = new Float32Array(20);
        if (morph) morph._gosxWGPUComputedMorphUniformData = data;
      }
      var matrix = morph && morph.modelMatrix || webGPUObjectModelMatrix(obj);
      for (var i = 0; i < 16; i++) {
        data[i] = sceneNumber(matrix && matrix[i], i % 5 === 0 ? 1 : 0);
      }
      data[16] = Math.max(0, Math.min(1, sceneNumber(morph && morph.alpha, 0.45)));
      data[17] = Math.max(0, Math.floor(sceneNumber(count, 0)));
      data[18] = 0;
      data[19] = 0;
      return data;
    }

    function webGPUComputedMorphPackedData(morph, slot, count, positions, normals, tangents) {
      if (!morph || !positions || !normals || !tangents) return null;
      var cache = morph[slot];
      if (
        cache &&
        cache.count === count &&
        cache.positions === positions &&
        cache.normals === normals &&
        cache.tangents === tangents
      ) {
        return cache.data;
      }
      var data = new Float32Array(Math.max(0, count) * 10);
      for (var i = 0; i < count; i++) {
        var p = i * 3;
        var t = i * 4;
        var out = i * 10;
        data[out] = sceneNumber(positions[p], 0);
        data[out + 1] = sceneNumber(positions[p + 1], 0);
        data[out + 2] = sceneNumber(positions[p + 2], 0);
        data[out + 3] = sceneNumber(normals[p], 0);
        data[out + 4] = sceneNumber(normals[p + 1], 0);
        data[out + 5] = sceneNumber(normals[p + 2], 1);
        data[out + 6] = sceneNumber(tangents[t], 1);
        data[out + 7] = sceneNumber(tangents[t + 1], 0);
        data[out + 8] = sceneNumber(tangents[t + 2], 0);
        data[out + 9] = sceneNumber(tangents[t + 3], 1);
      }
      morph[slot] = {
        count: count,
        positions: positions,
        normals: normals,
        tangents: tangents,
        data: data,
      };
      return data;
    }

    function webGPUComputedMorphEnsureOutputBuffer(record, slot, count, components) {
      var bytes = Math.max(4, Math.max(0, Math.floor(sceneNumber(count, 0))) * Math.max(1, components) * 4);
      var buffer = record && record[slot];
      if (buffer && wgpuTrackedBufferSize(buffer) >= bytes) return buffer;
      if (buffer && typeof buffer.destroy === "function") {
        pointsEntryGPUBuffers.delete(buffer);
        buffer.destroy();
      }
      buffer = wgpuCreateTrackedBuffer(GPUBufferUsage.STORAGE | GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, bytes);
      record[slot] = buffer;
      return buffer;
    }

    function webGPUComputedMorphRecord(obj) {
      var data = webGPUComputedMorphData(obj);
      if (!data) return null;
      var morph = data.morph;
      var record = morph._gosxWGPUComputedMorphRecord;
      if (!record) {
        record = {};
        morph._gosxWGPUComputedMorphRecord = record;
      }
      var count = data.count;
      var sourcePacked = webGPUComputedMorphPackedData(morph, "_gosxWGPUComputedMorphSourcePackedData", count, data.sourcePositions, data.sourceNormals, data.sourceTangents);
      var targetPacked = webGPUComputedMorphPackedData(morph, "_gosxWGPUComputedMorphTargetPackedData", count, data.targetPositions, data.targetNormals, data.targetTangents);
      var sourcePackedBuffer = wgpuCachedTrackedBuffer(morph, "_gosxWGPUComputedMorphSourcePacked", sourcePacked, GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST, false);
      var targetPackedBuffer = wgpuCachedTrackedBuffer(morph, "_gosxWGPUComputedMorphTargetPacked", targetPacked, GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST, false);
      var uniformData = webGPUComputedMorphUniformData(obj, morph, count);
      var uniformBuffer = wgpuCachedTrackedBuffer(morph, "_gosxWGPUComputedMorphUniform", uniformData, GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST, true);
      var positionBuffer = webGPUComputedMorphEnsureOutputBuffer(record, "positionBuffer", count, 3);
      var normalBuffer = webGPUComputedMorphEnsureOutputBuffer(record, "normalBuffer", count, 3);
      var tangentBuffer = webGPUComputedMorphEnsureOutputBuffer(record, "tangentBuffer", count, 4);
      if (
        !sourcePackedBuffer || !targetPackedBuffer ||
        !uniformBuffer || !positionBuffer || !normalBuffer || !tangentBuffer
      ) {
        return null;
      }
      if (
        !record.bindGroup ||
        record.sourcePackedBuffer !== sourcePackedBuffer ||
        record.targetPackedBuffer !== targetPackedBuffer ||
        record.positionBuffer !== positionBuffer ||
        record.normalBuffer !== normalBuffer ||
        record.tangentBuffer !== tangentBuffer ||
        record.uniformBuffer !== uniformBuffer
      ) {
        record.bindGroup = device.createBindGroup({
          layout: computedMorphBindGroupLayout,
          entries: [
            { binding: 0, resource: { buffer: sourcePackedBuffer } },
            { binding: 1, resource: { buffer: targetPackedBuffer } },
            { binding: 2, resource: { buffer: positionBuffer } },
            { binding: 3, resource: { buffer: normalBuffer } },
            { binding: 4, resource: { buffer: tangentBuffer } },
            { binding: 5, resource: { buffer: uniformBuffer } },
          ],
        });
        record.sourcePackedBuffer = sourcePackedBuffer;
        record.targetPackedBuffer = targetPackedBuffer;
        record.positionBuffer = positionBuffer;
        record.normalBuffer = normalBuffer;
        record.tangentBuffer = tangentBuffer;
        record.uniformBuffer = uniformBuffer;
      }
      record.count = count;
      record.workgroups = Math.ceil(Math.max(64, count) / 64);
      obj._gosxWGPUComputedMorphRecord = record;
      return record;
    }

    function updateComputedMorphMeshes(bundle, encoder) {
      var stats = {
        computedMorphDispatches: 0,
        computedMorphVertices: 0,
        computedMorphKernel: "m31labs.dev/gosx/scene3d.ComputedMorph",
      };
      var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
      var pass = null;
      for (var i = 0; i < objects.length; i++) {
        var obj = objects[i];
        if (!obj || obj.viewCulled || webGPUObjectIsSkinned(obj)) continue;
        var record = webGPUComputedMorphRecord(obj);
        if (!record) continue;
        if (!pass) {
          pass = encoder.beginComputePass({ label: "gosx-computed-morph" });
          pass.setPipeline(computedMorphPipeline);
        }
        pass.setBindGroup(0, record.bindGroup);
        pass.dispatchWorkgroups(record.workgroups);
        stats.computedMorphDispatches += 1;
        stats.computedMorphVertices += record.count;
      }
      if (pass) pass.end();
      return stats;
    }

    function webGPUObjectComputedMorphDrawRecord(obj) {
      var record = obj && obj._gosxWGPUComputedMorphRecord;
      return record && record.positionBuffer && record.normalBuffer && record.tangentBuffer ? record : null;
    }

    function webGPUBindComputedMorphBuffer(pass, slot, buffer, count, components) {
      if (!buffer) return false;
      var byteSize = Math.max(4, Math.max(0, Math.floor(sceneNumber(count, 0))) * Math.max(1, components) * 4);
      pass.setVertexBuffer(slot, buffer, 0, byteSize);
      return true;
    }

    function webGPUTransformVec3Attribute(obj, key, count, defaults, scratchName) {
      var source = webGPUDefaultAttributeData(obj, key, count, 3, defaults);
      var out = ensureScratch(scratchName, count * 3);
      var m = webGPUObjectModelMatrix(obj);
      for (var i = 0; i < count; i++) {
        var off = i * 3;
        var x = sceneNumber(source[off], defaults && defaults[0] || 0);
        var y = sceneNumber(source[off + 1], defaults && defaults[1] || 0);
        var z = sceneNumber(source[off + 2], defaults && defaults[2] || 0);
        var tx = sceneNumber(m[0], 1) * x + sceneNumber(m[4], 0) * y + sceneNumber(m[8], 0) * z;
        var ty = sceneNumber(m[1], 0) * x + sceneNumber(m[5], 1) * y + sceneNumber(m[9], 0) * z;
        var tz = sceneNumber(m[2], 0) * x + sceneNumber(m[6], 0) * y + sceneNumber(m[10], 1) * z;
        var len = Math.hypot(tx, ty, tz);
        if (len > 0.000001) {
          tx /= len; ty /= len; tz /= len;
        }
        out[off] = tx;
        out[off + 1] = ty;
        out[off + 2] = tz;
      }
      return out.subarray(0, count * 3);
    }

    function webGPUTransformTangentAttribute(obj, count) {
      var source = webGPUDefaultAttributeData(obj, "tangents", count, 4, [1, 0, 0, 1]);
      var out = ensureScratch("tangents", count * 4);
      var m = webGPUObjectModelMatrix(obj);
      for (var i = 0; i < count; i++) {
        var off = i * 4;
        var x = sceneNumber(source[off], 1);
        var y = sceneNumber(source[off + 1], 0);
        var z = sceneNumber(source[off + 2], 0);
        var tx = sceneNumber(m[0], 1) * x + sceneNumber(m[4], 0) * y + sceneNumber(m[8], 0) * z;
        var ty = sceneNumber(m[1], 0) * x + sceneNumber(m[5], 1) * y + sceneNumber(m[9], 0) * z;
        var tz = sceneNumber(m[2], 0) * x + sceneNumber(m[6], 0) * y + sceneNumber(m[10], 1) * z;
        var len = Math.hypot(tx, ty, tz);
        if (len > 0.000001) {
          tx /= len; ty /= len; tz /= len;
        }
        out[off] = tx;
        out[off + 1] = ty;
        out[off + 2] = tz;
        out[off + 3] = sceneNumber(source[off + 3], 1);
      }
      return out.subarray(0, count * 4);
    }

    function webGPUBindElioSkinnedBuffers(pass, obj, count) {
      var outputBuffer = obj && obj._gosxWGPUElioSkinOutputBuffer;
      if (!outputBuffer) return false;
      var normals = webGPUTransformVec3Attribute(obj, "normals", count, [0, 0, 1], "normals");
      var uvs = webGPUDefaultAttributeData(obj, "uvs", count, 2, [0, 0]);
      var tangents = webGPUTransformTangentAttribute(obj, count);
      pass.setVertexBuffer(0, outputBuffer);
      pass.setVertexBuffer(1, wgpuCachedTrackedBuffer(obj, "_gosxWGPUSkinnedNormals", normals, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
      pass.setVertexBuffer(2, wgpuCachedTrackedBuffer(obj, "_gosxWGPUSkinnedUVs", uvs, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
      pass.setVertexBuffer(3, wgpuCachedTrackedBuffer(obj, "_gosxWGPUSkinnedTangents", tangents, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
      return true;
    }

    function webGPUCountSkinnedMeshes(bundle) {
      var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
      var count = 0;
      for (var i = 0; i < objects.length; i++) {
        if (webGPUObjectIsSkinned(objects[i])) count++;
      }
      return count;
    }

    function webGPUSceneMeshVertexCount(bundle) {
      var count = Math.max(0, Math.floor(sceneNumber(bundle && bundle.worldMeshVertexCount, 0)));
      var positions = bundle && bundle.worldMeshPositions;
      if (positions && typeof positions.length === "number") {
        count = Math.max(count, Math.floor(positions.length / 3));
      }
      var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
      for (var i = 0; i < objects.length; i++) {
        var obj = objects[i];
        if (!obj) continue;
        count = Math.max(count, Math.floor(sceneNumber(obj.vertexOffset, 0)) + Math.floor(sceneNumber(obj.vertexCount, 0)));
      }
      return count;
    }

    function webGPUSceneMeshAttributeData(bundle, key, components, defaults, vertexCount) {
      var required = Math.max(0, Math.floor(vertexCount || 0)) * Math.max(1, components);
      var source = bundle && bundle[key];
      if (source && typeof source.length === "number" && source.length >= required) {
        var typed = toSceneFloat32Array(source);
        if (typed !== source && bundle) bundle[key] = typed;
        return typed.length === required ? typed : typed.subarray(0, required);
      }

      var cacheKey = "_gosxWGPUDefault" + key;
      var cacheCountKey = cacheKey + "VertexCount";
      var data = bundle && bundle[cacheKey];
      if (!data || data.length !== required || bundle[cacheCountKey] !== vertexCount) {
        data = new Float32Array(required);
        var stride = Math.max(1, components);
        for (var i = 0; i < vertexCount; i++) {
          for (var c = 0; c < stride; c++) {
            data[i * stride + c] = sceneNumber(defaults && defaults[c], 0);
          }
        }
        if (bundle) {
          bundle[cacheKey] = data;
          bundle[cacheCountKey] = vertexCount;
        }
      }
      return data;
    }

    function ensurePBRSceneAttributeBuffers(bundle) {
      if (!bundle) return null;
      var vertexCount = webGPUSceneMeshVertexCount(bundle);
      if (vertexCount <= 0) return null;
      var usage = GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST;
      var positions = webGPUSceneMeshAttributeData(bundle, "worldMeshPositions", 3, [0, 0, 0], vertexCount);
      var normals = webGPUSceneMeshAttributeData(bundle, "worldMeshNormals", 3, [0, 0, 1], vertexCount);
      var uvs = webGPUSceneMeshAttributeData(bundle, "worldMeshUVs", 2, [0, 0], vertexCount);
      var tangents = webGPUSceneMeshAttributeData(bundle, "worldMeshTangents", 4, [1, 0, 0, 1], vertexCount);
      return {
        positions: { buffer: wgpuCachedTrackedBuffer(bundle, "_gosxWGPUScenePBRPositions", positions, usage, true), components: 3 },
        normals: { buffer: wgpuCachedTrackedBuffer(bundle, "_gosxWGPUScenePBRNormals", normals, usage, true), components: 3 },
        uvs: { buffer: wgpuCachedTrackedBuffer(bundle, "_gosxWGPUScenePBRUVs", uvs, usage, true), components: 2 },
        tangents: { buffer: wgpuCachedTrackedBuffer(bundle, "_gosxWGPUScenePBRTangents", tangents, usage, true), components: 4 },
        vertexCount: vertexCount,
      };
    }

    function webGPUBindSceneMeshVertexBuffer(pass, slot, record, vertexOffset, vertexCount) {
      if (!record || !record.buffer) return false;
      var components = Math.max(1, Math.floor(sceneNumber(record.components, 1)));
      var offset = Math.max(0, Math.floor(sceneNumber(vertexOffset, 0)));
      var count = Math.max(0, Math.floor(sceneNumber(vertexCount, 0)));
      var byteOffset = offset * components * 4;
      var byteSize = Math.max(4, count * components * 4);
      pass.setVertexBuffer(slot, record.buffer, byteOffset, byteSize);
      return true;
    }

    // -----------------------------------------------------------------------
    // Draw list construction
    // -----------------------------------------------------------------------

    function buildDrawList(bundle) {
      var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
      var materials = Array.isArray(bundle.materials) ? bundle.materials : [];
      var opaque = [];
      var alpha = [];
      var additive = [];

      for (var i = 0; i < objects.length; i++) {
        var obj = objects[i];
        if (!obj || obj.viewCulled) continue;
        if (!Number.isFinite(obj.vertexOffset) || !Number.isFinite(obj.vertexCount) || obj.vertexCount <= 0) continue;
        var mat = materials[obj.materialIndex] || null;
        var pass = scenePBRObjectRenderPass(obj, mat);
        if (pass === "alpha") alpha.push(obj);
        else if (pass === "additive") additive.push(obj);
        else opaque.push(obj);
      }

      alpha.sort(scenePBRDepthSort);
      additive.sort(scenePBRDepthSort);

      return { opaque: opaque, alpha: alpha, additive: additive };
    }

    // -----------------------------------------------------------------------
    // Shadow pass
    // -----------------------------------------------------------------------

    function renderShadowPass(encoder, lightMatrix, bundle, shadowResource, pbrBuffers) {
      var sp = getShadowPipeline();
      if (!sp) return;

      // Upload light space matrix.
      device.queue.writeBuffer(shadowFrameBuffer, 0, lightMatrix);

      var shadowBG = device.createBindGroup({
        layout: shadowBindGroupLayout,
        entries: [
          { binding: 0, resource: { buffer: shadowFrameBuffer } },
        ],
      });

      var pass = encoder.beginRenderPass({
        colorAttachments: [],
        depthStencilAttachment: {
          view: shadowResource.view,
          depthLoadOp: "clear",
          depthClearValue: 1.0,
          depthStoreOp: "store",
        },
      });

      pass.setBindGroup(0, shadowBG);
      var currentShadowPipeline = "";

      var objects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];
      for (var i = 0; i < objects.length; i++) {
        var obj = objects[i];
        if (!obj || obj.viewCulled) continue;
        if (!obj.castShadow) continue;
        if (!Number.isFinite(obj.vertexOffset) || !Number.isFinite(obj.vertexCount) || obj.vertexCount <= 0) continue;

        if (webGPUObjectIsSkinned(obj)) {
          var skinnedPositionBuffer = obj._gosxWGPUElioSkinOutputBuffer;
          if (!skinnedPositionBuffer) continue;
          if (currentShadowPipeline !== "static") {
            pass.setPipeline(sp);
            pass.setBindGroup(0, shadowBG);
            currentShadowPipeline = "static";
          }
          pass.setVertexBuffer(0, skinnedPositionBuffer);
          pass.draw(obj.vertexCount);
          continue;
        }

        var computedMorphRecord = webGPUObjectComputedMorphDrawRecord(obj);
        if (computedMorphRecord) {
          if (currentShadowPipeline !== "static") {
            pass.setPipeline(sp);
            pass.setBindGroup(0, shadowBG);
            currentShadowPipeline = "static";
          }
          if (!webGPUBindComputedMorphBuffer(pass, 0, computedMorphRecord.positionBuffer, obj.vertexCount, 3)) continue;
          pass.draw(obj.vertexCount);
          continue;
        }

        if (currentShadowPipeline !== "static") {
          pass.setPipeline(sp);
          pass.setBindGroup(0, shadowBG);
          currentShadowPipeline = "static";
        }

        if (!webGPUBindSceneMeshVertexBuffer(pass, 0, pbrBuffers && pbrBuffers.positions, obj.vertexOffset, obj.vertexCount)) continue;
        pass.draw(obj.vertexCount);
      }

      drawInstancedShadowMeshes(pass, bundle);
      pass.end();
    }

    // -----------------------------------------------------------------------
    // PBR object drawing
    // -----------------------------------------------------------------------

    function drawPBRObjects(pass, objectList, bundle, materials, frameBindGroup, blendMode, depthWrite, pbrBuffers) {
      var lastMaterialIndex = -1;
      var lastReceiveShadow = null;
      var currentPipelineKind = "";

      function bindMeshAttribute(attr, obj, offset, count) {
        var computedRecord = webGPUObjectComputedMorphDrawRecord(obj);
        if (attr.source === "positions") {
          if (computedRecord && webGPUBindComputedMorphBuffer(pass, attr.slot, computedRecord.positionBuffer, count, 3)) return;
          webGPUBindSceneMeshVertexBuffer(pass, attr.slot, pbrBuffers && pbrBuffers.positions, offset, count);
          return;
        }
        if (attr.source === "normals") {
          if (computedRecord && webGPUBindComputedMorphBuffer(pass, attr.slot, computedRecord.normalBuffer, count, 3)) return;
          webGPUBindSceneMeshVertexBuffer(pass, attr.slot, pbrBuffers && pbrBuffers.normals, offset, count);
          return;
        }
        if (attr.source === "uvs") {
          webGPUBindSceneMeshVertexBuffer(pass, attr.slot, pbrBuffers && pbrBuffers.uvs, offset, count);
          return;
        }
        if (attr.source === "tangents") {
          if (computedRecord && webGPUBindComputedMorphBuffer(pass, attr.slot, computedRecord.tangentBuffer, count, 4)) return;
          webGPUBindSceneMeshVertexBuffer(pass, attr.slot, pbrBuffers && pbrBuffers.tangents, offset, count);
        }
      }

      function bindPBRPipeline() {
        if (currentPipelineKind === "pbr") return;
        pass.setPipeline(getPBRPipeline(blendMode, depthWrite));
        pass.setBindGroup(0, frameBindGroup);
        currentPipelineKind = "pbr";
        lastMaterialIndex = -1;
        lastReceiveShadow = null;
      }

      for (var i = 0; i < objectList.length; i++) {
        var obj = objectList[i];
        var matIndex = sceneNumber(obj.materialIndex, 0);
        var mat = materials[matIndex] || null;
        var receiveShadow = !!obj.receiveShadow;
        var offset = obj.vertexOffset;
        var count = obj.vertexCount;
        var isSkinned = webGPUObjectIsSkinned(obj);
        var computedMorphRecord = !isSkinned ? webGPUObjectComputedMorphDrawRecord(obj) : null;
        var selenaResource = !isSkinned ? getSelenaPipeline(mat, blendMode, depthWrite) : null;
        if (selenaResource) {
          var selenaKey = "selena:" + (mat && mat.key || matIndex);
          if (currentPipelineKind !== selenaKey) {
            pass.setPipeline(selenaResource.pipeline);
            currentPipelineKind = selenaKey;
          }
          var selenaBG = createSelenaBindGroup(mat, selenaResource, obj);
          if (selenaBG) {
            pass.setBindGroup(0, selenaBG);
            for (var ai = 0; ai < selenaResource.attrs.length; ai++) {
              bindMeshAttribute(selenaResource.attrs[ai], obj, offset, count);
            }
            pass.draw(count);
            continue;
          }
        }

        if (isSkinned) {
          bindPBRPipeline();
          if (matIndex !== lastMaterialIndex || receiveShadow !== lastReceiveShadow) {
            var skinnedMatBG = createMaterialBindGroup(mat, receiveShadow, mat || obj);
            pass.setBindGroup(1, skinnedMatBG);
            lastMaterialIndex = matIndex;
            lastReceiveShadow = receiveShadow;
          }
          if (webGPUBindElioSkinnedBuffers(pass, obj, count)) {
            pass.draw(count);
          }
          continue;
        }

        bindPBRPipeline();

        // Recreate material bind group when material or receiveShadow changes.
        if (matIndex !== lastMaterialIndex || receiveShadow !== lastReceiveShadow) {
          var matBG = createMaterialBindGroup(mat, receiveShadow, mat || obj);
          pass.setBindGroup(1, matBG);
          lastMaterialIndex = matIndex;
          lastReceiveShadow = receiveShadow;
        }

        if (computedMorphRecord) {
          if (!webGPUBindComputedMorphBuffer(pass, 0, computedMorphRecord.positionBuffer, count, 3)) continue;
          if (!webGPUBindComputedMorphBuffer(pass, 1, computedMorphRecord.normalBuffer, count, 3)) continue;
          if (!webGPUBindSceneMeshVertexBuffer(pass, 2, pbrBuffers && pbrBuffers.uvs, offset, count)) continue;
          if (!webGPUBindComputedMorphBuffer(pass, 3, computedMorphRecord.tangentBuffer, count, 4)) continue;
          pass.draw(count);
          continue;
        }

        if (!webGPUBindSceneMeshVertexBuffer(pass, 0, pbrBuffers && pbrBuffers.positions, offset, count)) continue;
        if (!webGPUBindSceneMeshVertexBuffer(pass, 1, pbrBuffers && pbrBuffers.normals, offset, count)) continue;
        if (!webGPUBindSceneMeshVertexBuffer(pass, 2, pbrBuffers && pbrBuffers.uvs, offset, count)) continue;
        if (!webGPUBindSceneMeshVertexBuffer(pass, 3, pbrBuffers && pbrBuffers.tangents, offset, count)) continue;

        pass.draw(count);
      }
    }

    function instancedMeshCount(mesh) {
      if (!mesh) return 0;
      return Math.max(0, Math.floor(sceneNumber(mesh.instanceCount, sceneNumber(mesh.count, 0))));
    }

    function instancedMeshMaterial(mesh, materials) {
      var mat = materials[sceneNumber(mesh && mesh.materialIndex, 0)] || null;
      if (mat) return mat;
      return {
        color: mesh && mesh.color || "#8de1ff",
        roughness: sceneNumber(mesh && mesh.roughness, 0.5),
        metalness: sceneNumber(mesh && mesh.metalness, 0),
        emissive: sceneNumber(mesh && mesh.emissive, 0),
        opacity: clamp01(sceneNumber(mesh && mesh.opacity, 1)),
        unlit: mesh && mesh.materialKind === "flat",
        renderPass: mesh && mesh.renderPass,
      };
    }

    function instancedMeshTransformData(mesh, count) {
      if (!mesh || count <= 0 || !mesh.transforms) return null;
      if (!mesh._cachedTransforms) {
        if (mesh.transforms instanceof Float32Array) {
          mesh._cachedTransforms = mesh.transforms;
        } else if (Array.isArray(mesh.transforms)) {
          mesh._cachedTransforms = new Float32Array(mesh.transforms);
        }
      }
      var data = mesh._cachedTransforms;
      return data && data.length >= count * 16 ? data : null;
    }

    function instancedMeshColorData(mesh, count) {
      if (!mesh || count <= 0) return null;
      var rawColors = mesh.colors;
      var source = rawColors || null;
      if (
        mesh._cachedWGPUInstanceColors &&
        mesh._cachedWGPUInstanceColorCount === count &&
        mesh._cachedWGPUInstanceColorSource === source
      ) {
        return mesh._cachedWGPUInstanceColors;
      }

      var data = null;
      if (rawColors && typeof rawColors.length === "number" && rawColors.length > 0) {
        if (Array.isArray(rawColors) && typeof rawColors[0] === "string") {
          data = new Float32Array(count * 4);
          for (var ci = 0; ci < count; ci++) {
            var rgba = sceneColorRGBA(rawColors[ci] || rawColors[rawColors.length - 1], [1, 1, 1, 1]);
            data[ci * 4] = rgba[0];
            data[ci * 4 + 1] = rgba[1];
            data[ci * 4 + 2] = rgba[2];
            data[ci * 4 + 3] = rgba[3];
          }
        } else if (rawColors.length >= count * 4) {
          data = rawColors instanceof Float32Array ? rawColors : new Float32Array(rawColors);
        } else if (rawColors.length >= count * 3) {
          data = new Float32Array(count * 4);
          for (var ni = 0; ni < count; ni++) {
            data[ni * 4] = rawColors[ni * 3];
            data[ni * 4 + 1] = rawColors[ni * 3 + 1];
            data[ni * 4 + 2] = rawColors[ni * 3 + 2];
            data[ni * 4 + 3] = 1;
          }
        }
      }

      if (!data) {
        data = new Float32Array(count * 4);
        for (var di = 0; di < count; di++) {
          data[di * 4] = 1;
          data[di * 4 + 1] = 1;
          data[di * 4 + 2] = 1;
          data[di * 4 + 3] = 1;
        }
      }

      mesh._cachedWGPUInstanceColors = data;
      mesh._cachedWGPUInstanceColorCount = count;
      mesh._cachedWGPUInstanceColorSource = source;
      return data;
    }

    function getInstancedGeometry(mesh) {
      if (typeof generateInstancedGeometry !== "function") return null;
      var kind = typeof normalizeInstancedGeometryKind === "function"
        ? normalizeInstancedGeometryKind(mesh && mesh.kind)
        : (typeof mesh.kind === "string" ? mesh.kind.toLowerCase() : "box");
      var size = sceneNumber(mesh && mesh.size, 0);
      var w = sceneNumber(mesh.width, 1);
      var h = sceneNumber(mesh.height, 1);
      var d = sceneNumber(mesh.depth, 1);
      var r = sceneNumber(mesh.radius, 0.5);
      var rt = sceneNumber(mesh.radiusTop, r);
      var rb = sceneNumber(mesh.radiusBottom, r);
      var tube = sceneNumber(mesh.tube, 0.3);
      var s = sceneNumber(mesh.segments, 32);
      var radial = sceneNumber(mesh.radialSegments, 32);
      var tubular = sceneNumber(mesh.tubularSegments, 16);
      var key = kind + ":" + size + ":" + w + ":" + h + ":" + d + ":" + r + ":" + rt + ":" + rb + ":" + tube + ":" + s + ":" + radial + ":" + tubular;
      if (instancedGeometryCache[key]) return instancedGeometryCache[key];
      var geom = generateInstancedGeometry(kind, {
        size: size,
        width: w,
        height: h,
        depth: d,
        radius: r,
        radiusTop: rt,
        radiusBottom: rb,
        tube: tube,
        segments: s,
        radialSegments: radial,
        tubularSegments: tubular,
      });
      instancedGeometryCache[key] = geom;
      return geom;
    }

    function ensureInstancedGeometryGPUBuffer(geom, slot, data) {
      return wgpuCachedTrackedBuffer(geom, slot, data, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, false);
    }

    function ensureInstancedTransformGPUBuffer(mesh, data) {
      return wgpuCachedTrackedBuffer(mesh, "_gosxWGPUInstanceTransformBuffer", data, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true);
    }

    function ensureInstancedColorGPUBuffer(mesh, data) {
      return wgpuCachedTrackedBuffer(mesh, "_gosxWGPUInstanceColorBuffer", data, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true);
    }

    function buildInstancedDrawList(bundle, materials) {
      var meshes = Array.isArray(bundle && bundle.instancedMeshes) ? bundle.instancedMeshes : [];
      var opaque = [];
      var alpha = [];
      var additive = [];
      for (var i = 0; i < meshes.length; i++) {
        var mesh = meshes[i];
        if (!mesh || mesh.viewCulled) continue;
        if (instancedMeshCount(mesh) <= 0) continue;
        if (!instancedMeshTransformData(mesh, instancedMeshCount(mesh))) continue;
        var mat = instancedMeshMaterial(mesh, materials);
        var pass = scenePBRObjectRenderPass(mesh, mat);
        if (pass === "alpha") alpha.push(mesh);
        else if (pass === "additive") additive.push(mesh);
        else opaque.push(mesh);
      }
      alpha.sort(scenePBRDepthSort);
      additive.sort(scenePBRDepthSort);
      return { opaque: opaque, alpha: alpha, additive: additive };
    }

    function drawInstancedMeshes(pass, meshList, materials) {
      for (var i = 0; i < meshList.length; i++) {
        var mesh = meshList[i];
        var instanceCount = instancedMeshCount(mesh);
        var transformData = instancedMeshTransformData(mesh, instanceCount);
        if (!transformData) continue;

        var geom = getInstancedGeometry(mesh);
        if (!geom || geom.vertexCount <= 0) continue;

        var mat = instancedMeshMaterial(mesh, materials);
        pass.setBindGroup(1, createMaterialBindGroup(mat, !!mesh.receiveShadow, mesh));

        pass.setVertexBuffer(0, ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedPositionBuffer", geom.positions));
        pass.setVertexBuffer(1, ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedNormalBuffer", geom.normals));
        pass.setVertexBuffer(2, ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedUVBuffer", geom.uvs));
        pass.setVertexBuffer(3, ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedTangentBuffer", geom.tangents));
        pass.setVertexBuffer(4, ensureInstancedTransformGPUBuffer(mesh, transformData));
        pass.setVertexBuffer(5, ensureInstancedColorGPUBuffer(mesh, instancedMeshColorData(mesh, instanceCount)));
        pass.draw(geom.vertexCount, instanceCount);
      }
    }

    function instancedLocalBounds(mesh) {
      var kind = typeof normalizeInstancedGeometryKind === "function"
        ? normalizeInstancedGeometryKind(mesh && mesh.kind)
        : (typeof mesh.kind === "string" ? mesh.kind.toLowerCase() : "box");
      if (kind === "sphere") {
        var radius = Math.max(0.0001, sceneNumber(mesh.radius, 0.5));
        return { minX: -radius, minY: -radius, minZ: -radius, maxX: radius, maxY: radius, maxZ: radius };
      }
      if (kind === "cylinder" || kind === "cone") {
        var top = kind === "cone" ? 0 : Math.max(0, sceneNumber(mesh.radiusTop, sceneNumber(mesh.radius, 0.5)));
        var bottom = Math.max(0, sceneNumber(mesh.radiusBottom, sceneNumber(mesh.radius, 0.5)));
        var cylinderRadius = Math.max(top, bottom);
        var cylinderHeight = Math.max(0.0001, sceneNumber(mesh.height, 1));
        return { minX: -cylinderRadius, minY: -cylinderHeight * 0.5, minZ: -cylinderRadius, maxX: cylinderRadius, maxY: cylinderHeight * 0.5, maxZ: cylinderRadius };
      }
      if (kind === "torus") {
        var major = Math.max(0.0001, sceneNumber(mesh.radius, 0.7));
        var tube = Math.max(0.0001, sceneNumber(mesh.tube, 0.3));
        var torusRadius = major + tube;
        return { minX: -torusRadius, minY: -tube, minZ: -torusRadius, maxX: torusRadius, maxY: tube, maxZ: torusRadius };
      }
      var w = Math.max(0.0001, sceneNumber(mesh.width, 1));
      var h = kind === "plane" ? 0 : Math.max(0.0001, sceneNumber(mesh.height, 1));
      var d = Math.max(0.0001, sceneNumber(mesh.depth, 1));
      return { minX: -w * 0.5, minY: -h * 0.5, minZ: -d * 0.5, maxX: w * 0.5, maxY: h * 0.5, maxZ: d * 0.5 };
    }

    function expandBoundsPoint(bounds, x, y, z) {
      if (!bounds) return { minX: x, minY: y, minZ: z, maxX: x, maxY: y, maxZ: z };
      if (x < bounds.minX) bounds.minX = x;
      if (y < bounds.minY) bounds.minY = y;
      if (z < bounds.minZ) bounds.minZ = z;
      if (x > bounds.maxX) bounds.maxX = x;
      if (y > bounds.maxY) bounds.maxY = y;
      if (z > bounds.maxZ) bounds.maxZ = z;
      return bounds;
    }

    function expandInstancedBounds(bounds, mesh, transformData, count) {
      var b = instancedLocalBounds(mesh);
      var xs = [b.minX, b.maxX];
      var ys = [b.minY, b.maxY];
      var zs = [b.minZ, b.maxZ];
      for (var ii = 0; ii < count; ii++) {
        var base = ii * 16;
        for (var xi = 0; xi < 2; xi++) {
          for (var yi = 0; yi < 2; yi++) {
            for (var zi = 0; zi < 2; zi++) {
              var x = xs[xi], y = ys[yi], z = zs[zi];
              bounds = expandBoundsPoint(bounds,
                transformData[base + 0] * x + transformData[base + 4] * y + transformData[base + 8] * z + transformData[base + 12],
                transformData[base + 1] * x + transformData[base + 5] * y + transformData[base + 9] * z + transformData[base + 13],
                transformData[base + 2] * x + transformData[base + 6] * y + transformData[base + 10] * z + transformData[base + 14]
              );
            }
          }
        }
      }
      return bounds;
    }

    function webGPUShadowComputeBounds(bundle) {
      var bounds = typeof sceneShadowComputeBounds === "function" ? sceneShadowComputeBounds(bundle) : null;
      var meshes = Array.isArray(bundle && bundle.instancedMeshes) ? bundle.instancedMeshes : [];
      for (var i = 0; i < meshes.length; i++) {
        var mesh = meshes[i];
        if (!mesh || mesh.viewCulled) continue;
        var count = instancedMeshCount(mesh);
        var transforms = instancedMeshTransformData(mesh, count);
        if (!transforms) continue;
        bounds = expandInstancedBounds(bounds, mesh, transforms, count);
      }
      return bounds || { minX: -10, minY: -10, minZ: -10, maxX: 10, maxY: 10, maxZ: 10 };
    }

    function drawInstancedShadowMeshes(pass, bundle) {
      var meshes = Array.isArray(bundle && bundle.instancedMeshes) ? bundle.instancedMeshes : [];
      var drew = false;
      for (var i = 0; i < meshes.length; i++) {
        var mesh = meshes[i];
        if (!mesh || mesh.viewCulled || !mesh.castShadow) continue;
        var instanceCount = instancedMeshCount(mesh);
        var transformData = instancedMeshTransformData(mesh, instanceCount);
        if (!transformData) continue;
        var geom = getInstancedGeometry(mesh);
        if (!geom || geom.vertexCount <= 0) continue;
        if (!drew) {
          pass.setPipeline(getShadowInstancedPipeline());
          drew = true;
        }
        pass.setVertexBuffer(0, ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedShadowPositionBuffer", geom.positions));
        pass.setVertexBuffer(1, ensureInstancedTransformGPUBuffer(mesh, transformData));
        pass.draw(geom.vertexCount, instanceCount);
      }
    }

    function toSceneFloat32Array(values) {
      if (values instanceof Float32Array) return values;
      if (!values || typeof values.length !== "number") return new Float32Array(0);
      return new Float32Array(values);
    }

    function webGPUUnsupportedLineStyles(bundle) {
      var dashes = bundle && bundle.worldLineDashes;
      if (dashes && typeof dashes.length === "number") {
        for (var di = 0; di < dashes.length; di++) {
          if (dashes[di]) return true;
        }
      }
      var lines = Array.isArray(bundle && bundle.lines) ? bundle.lines : [];
      for (var li = 0; li < lines.length; li++) {
        var line = lines[li];
        if (!line) continue;
        if (line.lineDash) return true;
        var material = line.material && typeof line.material === "object" ? line.material : null;
        var materialKind = String(line.materialKind || line.kind || material && material.kind || "").toLowerCase();
        if (material && material.lineDash) return true;
        if (materialKind === "line-dashed" || materialKind === "dashed") return true;
      }
      return false;
    }

    function webGPUWorldLineSegmentCount(bundle) {
      return Math.max(0, Math.floor(sceneNumber(bundle && bundle.worldVertexCount, 0) / 2));
    }

    function webGPUHasThickWorldLines(bundle) {
      var widths = bundle && bundle.worldLineWidths;
      if (widths && typeof widths.length === "number") {
        for (var i = 0; i < widths.length; i++) {
          if (sceneNumber(widths[i], 0) > 1) return true;
        }
      }
      var lines = Array.isArray(bundle && bundle.lines) ? bundle.lines : [];
      for (var li = 0; li < lines.length; li++) {
        if (sceneNumber(lines[li] && lines[li].lineWidth, 0) > 1) return true;
      }
      return false;
    }

    function webGPUCanUseThickWorldLines(bundle) {
      if (!webGPUHasThickWorldLines(bundle)) return true;
      if (typeof createSceneThickLineScratch !== "function" || typeof expandSceneThickLineIntoScratch !== "function") return false;
      var segmentCount = webGPUWorldLineSegmentCount(bundle);
      return segmentCount > 0 && segmentCount <= 16384;
    }

    function hasWorldLineData(bundle) {
      return Boolean(
        bundle &&
        !webGPUUnsupportedLineStyles(bundle) &&
        webGPUCanUseThickWorldLines(bundle) &&
        bundle.worldPositions &&
        bundle.worldColors &&
        Number(bundle.worldVertexCount || 0) > 0
      );
    }

    function hasScreenLineData(bundle) {
      return Boolean(
        bundle &&
        !hasWorldLineData(bundle) &&
        !webGPUUnsupportedLineStyles(bundle) &&
        bundle.positions &&
        bundle.colors &&
        Number(bundle.vertexCount || 0) > 0
      );
    }

    function hasSurfaceData(bundle) {
      var surfaces = Array.isArray(bundle && bundle.surfaces) ? bundle.surfaces : [];
      for (var i = 0; i < surfaces.length; i++) {
        var surface = surfaces[i];
        if (surface && !surface.viewCulled && sceneNumber(surface.vertexCount, 0) > 0) return true;
      }
      return false;
    }

    function fallbackMaterialData(owner, vertexCount) {
      var count = Math.max(0, Math.floor(sceneNumber(vertexCount, 0)));
      if (
        owner &&
        owner._gosxWGPUFallbackMaterialData &&
        owner._gosxWGPUFallbackMaterialCount === count
      ) {
        return owner._gosxWGPUFallbackMaterialData;
      }
      var data = new Float32Array(count * 3);
      for (var i = 0; i < count; i++) {
        data[i * 3] = 0;
        data[i * 3 + 1] = 0;
        data[i * 3 + 2] = 1;
      }
      if (owner) {
        owner._gosxWGPUFallbackMaterialData = data;
        owner._gosxWGPUFallbackMaterialCount = count;
      }
      return data;
    }

    function screenLinePositionData(bundle) {
      var source = bundle && bundle.positions;
      var count = Math.max(0, Math.floor(sceneNumber(bundle && bundle.vertexCount, 0)));
      if (
        bundle &&
        bundle._gosxWGPUScreenLineSource === source &&
        bundle._gosxWGPUScreenLineCount === count &&
        bundle._gosxWGPUScreenLinePositions
      ) {
        return bundle._gosxWGPUScreenLinePositions;
      }
      var src = toSceneFloat32Array(source);
      var data = new Float32Array(count * 3);
      for (var i = 0; i < count; i++) {
        data[i * 3] = src[i * 2] || 0;
        data[i * 3 + 1] = src[i * 2 + 1] || 0;
        data[i * 3 + 2] = 0;
      }
      if (bundle) {
        bundle._gosxWGPUScreenLineSource = source;
        bundle._gosxWGPUScreenLineCount = count;
        bundle._gosxWGPUScreenLinePositions = data;
      }
      return data;
    }

    function primitiveVertexCount(positions, colors, materials, requested) {
      var positionCount = Math.floor((positions && positions.length || 0) / 3);
      var colorCount = Math.floor((colors && colors.length || 0) / 4);
      var materialCount = Math.floor((materials && materials.length || 0) / 3);
      var maxCount = Math.max(0, Math.min(positionCount, colorCount, materialCount));
      var count = Math.max(0, Math.floor(sceneNumber(requested, maxCount)));
      return Math.min(count, maxCount);
    }

    function linePassDepthWrite(blendMode) {
      return blendMode !== "alpha" && blendMode !== "additive";
    }

    function colorPrimitiveOwner(name) {
      if (!screenLineOwner[name]) screenLineOwner[name] = {};
      return screenLineOwner[name];
    }

    function drawColorPrimitive(renderPass, entry, frameBindGroup) {
      if (!entry || !entry.vertexCount) return false;
      var owner = entry.owner || colorPrimitiveOwner(entry.name || "primitive");
      var positions = toSceneFloat32Array(entry.positions);
      var colors = toSceneFloat32Array(entry.colors);
      var materials = entry.materials ? toSceneFloat32Array(entry.materials) : fallbackMaterialData(owner, entry.vertexCount);
      var vertexCount = primitiveVertexCount(positions, colors, materials, entry.vertexCount);
      if (vertexCount <= 0) return false;

      var blend = entry.blend === "alpha" || entry.blend === "additive" ? entry.blend : "opaque";
      var topology = entry.topology === "triangle-list" ? "triangle-list" : "line-list";
      var depthWrite = typeof entry.depthWrite === "boolean" ? entry.depthWrite : linePassDepthWrite(blend);
      renderPass.setPipeline(getSceneColorPipeline(entry.space === "clip" ? "clip" : "world", topology, blend, depthWrite));
      renderPass.setBindGroup(0, frameBindGroup);
      renderPass.setVertexBuffer(0, wgpuCachedTrackedBuffer(owner, "_gosxWGPUPrimitivePositions", positions, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
      renderPass.setVertexBuffer(1, wgpuCachedTrackedBuffer(owner, "_gosxWGPUPrimitiveColors", colors, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
      renderPass.setVertexBuffer(2, wgpuCachedTrackedBuffer(owner, "_gosxWGPUPrimitiveMaterials", materials, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
      renderPass.draw(vertexCount);
      return true;
    }

    function webGPUWorldLinePasses(bundle) {
      if (!hasWorldLineData(bundle)) return [];
      if (typeof buildSceneWorldDrawPlan === "function") {
        if (!worldDrawScratch && typeof createSceneWorldDrawScratch === "function") {
          worldDrawScratch = createSceneWorldDrawScratch();
        }
        var drawPlan = buildSceneWorldDrawPlan(bundle, worldDrawScratch);
        if (drawPlan) {
          return [
            { name: "world-static-opaque", owner: drawPlan, positions: drawPlan.staticOpaquePositions, colors: drawPlan.staticOpaqueColors, materials: drawPlan.staticOpaqueMaterials, vertexCount: drawPlan.staticOpaqueVertexCount, blend: "opaque", space: "world", topology: "line-list" },
            { name: "world-dynamic-opaque", owner: drawPlan, positions: drawPlan.dynamicOpaquePositions, colors: drawPlan.dynamicOpaqueColors, materials: drawPlan.dynamicOpaqueMaterials, vertexCount: drawPlan.dynamicOpaqueVertexCount, blend: "opaque", space: "world", topology: "line-list" },
            { name: "world-alpha", owner: drawPlan.alphaPositions ? drawPlan : null, positions: drawPlan.alphaPositions, colors: drawPlan.alphaColors, materials: drawPlan.alphaMaterials, vertexCount: drawPlan.alphaVertexCount, blend: "alpha", space: "world", topology: "line-list", depthWrite: false },
            { name: "world-additive", owner: drawPlan.additivePositions ? drawPlan : null, positions: drawPlan.additivePositions, colors: drawPlan.additiveColors, materials: drawPlan.additiveMaterials, vertexCount: drawPlan.additiveVertexCount, blend: "additive", space: "world", topology: "line-list", depthWrite: false },
          ];
        }
      }
      var vertexCount = Math.max(0, Math.floor(sceneNumber(bundle.worldVertexCount, 0)));
      return [{
        name: "world-fallback",
        owner: bundle,
        positions: bundle.worldPositions,
        colors: bundle.worldColors,
        materials: fallbackMaterialData(bundle, vertexCount),
        vertexCount: vertexCount,
        blend: "alpha",
        space: "world",
        topology: "line-list",
        depthWrite: false,
      }];
    }

    function drawWorldLineEntries(renderPass, entries, passName, frameBindGroup) {
      var drew = false;
      for (var i = 0; i < entries.length; i++) {
        var entry = entries[i];
        if (!entry || entry.blend !== passName) continue;
        drew = drawColorPrimitive(renderPass, entry, frameBindGroup) || drew;
      }
      return drew;
    }

    function webGPUThickLineRecord(bundle) {
      if (!webGPUHasThickWorldLines(bundle) || !webGPUCanUseThickWorldLines(bundle)) return null;
      if (!bundle.worldPositions || !bundle.worldColors) return null;
      if (!thickLineScratch && typeof createSceneThickLineScratch === "function") {
        thickLineScratch = createSceneThickLineScratch();
      }
      if (!thickLineScratch || typeof expandSceneThickLineIntoScratch !== "function") return null;
      var segmentCount = webGPUWorldLineSegmentCount(bundle);
      if (segmentCount <= 0 || segmentCount > 16384) return null;
      var usedSegments = expandSceneThickLineIntoScratch(
        thickLineScratch,
        bundle.worldPositions,
        bundle.worldColors,
        bundle.worldLineWidths,
        bundle.worldLinePasses,
        segmentCount
      );
      if (usedSegments <= 0) return null;
      return {
        scratch: thickLineScratch,
        usedVerts: usedSegments * 4,
        owner: thickLineOwner,
      };
    }

    function thickLinePassIndexData(record, passName) {
      var scratch = record && record.scratch;
      if (!scratch) return null;
      if (passName === "additive") {
        return { slot: "_gosxWGPUThickLineAdditiveIndex", data: scratch.additiveIndices.subarray(0, scratch.additiveIndexCount), count: scratch.additiveIndexCount };
      }
      if (passName === "alpha") {
        return { slot: "_gosxWGPUThickLineAlphaIndex", data: scratch.alphaIndices.subarray(0, scratch.alphaIndexCount), count: scratch.alphaIndexCount };
      }
      return { slot: "_gosxWGPUThickLineOpaqueIndex", data: scratch.opaqueIndices.subarray(0, scratch.opaqueIndexCount), count: scratch.opaqueIndexCount };
    }

    function drawThickWorldLineEntries(renderPass, record, passName, frameBindGroup) {
      var scratch = record && record.scratch;
      var usedVerts = record && record.usedVerts || 0;
      if (!scratch || usedVerts <= 0) return false;
      var indexData = thickLinePassIndexData(record, passName);
      if (!indexData || indexData.count <= 0) return false;
      var owner = record.owner || thickLineOwner;
      var blend = passName === "alpha" || passName === "additive" ? passName : "opaque";
      renderPass.setPipeline(getThickLinePipeline(blend, linePassDepthWrite(blend)));
      renderPass.setBindGroup(0, frameBindGroup);
      renderPass.setVertexBuffer(0, wgpuCachedTrackedBuffer(owner, "_gosxWGPUThickLinePositionA", scratch.positionsA.subarray(0, usedVerts * 3), GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
      renderPass.setVertexBuffer(1, wgpuCachedTrackedBuffer(owner, "_gosxWGPUThickLinePositionB", scratch.positionsB.subarray(0, usedVerts * 3), GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
      renderPass.setVertexBuffer(2, wgpuCachedTrackedBuffer(owner, "_gosxWGPUThickLineColorA", scratch.colorsA.subarray(0, usedVerts * 4), GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
      renderPass.setVertexBuffer(3, wgpuCachedTrackedBuffer(owner, "_gosxWGPUThickLineColorB", scratch.colorsB.subarray(0, usedVerts * 4), GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
      renderPass.setVertexBuffer(4, wgpuCachedTrackedBuffer(owner, "_gosxWGPUThickLineSide", scratch.sides.subarray(0, usedVerts), GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
      renderPass.setVertexBuffer(5, wgpuCachedTrackedBuffer(owner, "_gosxWGPUThickLineEndpoint", scratch.endpoints.subarray(0, usedVerts), GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
      renderPass.setVertexBuffer(6, wgpuCachedTrackedBuffer(owner, "_gosxWGPUThickLineWidth", scratch.widths.subarray(0, usedVerts), GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
      var indexBuffer = wgpuCachedTrackedBuffer(owner, indexData.slot, indexData.data, GPUBufferUsage.INDEX | GPUBufferUsage.COPY_DST, true);
      renderPass.setIndexBuffer(indexBuffer, "uint16");
      renderPass.drawIndexed(indexData.count);
      return true;
    }

    function drawScreenLines(renderPass, bundle, frameBindGroup) {
      if (!hasScreenLineData(bundle)) return false;
      var vertexCount = Math.max(0, Math.floor(sceneNumber(bundle.vertexCount, 0)));
      return drawColorPrimitive(renderPass, {
        name: "screen-lines",
        owner: bundle,
        positions: screenLinePositionData(bundle),
        colors: bundle.colors,
        materials: fallbackMaterialData(bundle, vertexCount),
        vertexCount: vertexCount,
        blend: "alpha",
        space: "clip",
        topology: "line-list",
        depthWrite: false,
      }, frameBindGroup);
    }

    function surfaceEntries(bundle, renderPass) {
      var surfaces = Array.isArray(bundle && bundle.surfaces) ? bundle.surfaces.slice() : [];
      var filtered = [];
      for (var i = 0; i < surfaces.length; i++) {
        var surface = surfaces[i];
        if (!surface || surface.viewCulled) continue;
        if (Math.max(0, Math.floor(sceneNumber(surface.vertexCount, 0))) <= 0) continue;
        if (String(surface.renderPass || "opaque") !== renderPass) continue;
        filtered.push(surface);
      }
      if (renderPass !== "opaque") {
        filtered.sort(function(left, right) {
          var leftDepth = sceneNumber(left && left.depthCenter, 0);
          var rightDepth = sceneNumber(right && right.depthCenter, 0);
          if (leftDepth !== rightDepth) return rightDepth - leftDepth;
          return String(left && left.id || "").localeCompare(String(right && right.id || ""));
        });
      }
      return filtered;
    }

    function drawSurfaceEntries(renderPass, bundle, materials, passName, frameBindGroup) {
      var entries = surfaceEntries(bundle, passName);
      if (!entries.length) return false;
      var blend = passName === "alpha" || passName === "additive" ? passName : "opaque";
      renderPass.setPipeline(getSurfacePipeline(blend, blend === "opaque"));
      renderPass.setBindGroup(0, frameBindGroup);
      var drew = false;
      for (var i = 0; i < entries.length; i++) {
        var surface = entries[i];
        var mat = materials[sceneNumber(surface.materialIndex, 0)] || null;
        var positions = toSceneFloat32Array(surface.positions);
        var uvs = toSceneFloat32Array(surface.uv);
        var vertexCount = Math.min(Math.floor(positions.length / 3), Math.floor(uvs.length / 2), Math.max(0, Math.floor(sceneNumber(surface.vertexCount, 0))));
        if (vertexCount <= 0) continue;
        renderPass.setBindGroup(1, createMaterialBindGroup(mat, false, surface));
        renderPass.setVertexBuffer(0, wgpuCachedTrackedBuffer(surface, "_gosxWGPUSurfacePositions", positions, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
        renderPass.setVertexBuffer(1, wgpuCachedTrackedBuffer(surface, "_gosxWGPUSurfaceUVs", uvs, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
        renderPass.draw(vertexCount);
        drew = true;
      }
      return drew;
    }

    function resolveWebGPUSampleCount(bundle) {
      var requested = sceneNumber(bundle && (bundle.msaaSamples != null ? bundle.msaaSamples : bundle.sampleCount), sceneNumber(rendererOptions.msaaSamples, 0));
      if (requested <= 1 && (rendererOptions.antialias === true || sceneBool(bundle && bundle.antialias, false) || sceneBool(bundle && bundle.msaa, false))) {
        requested = 4;
      }
      if (requested >= 4) return 4;
      return 1;
    }

    // -----------------------------------------------------------------------
    // Points drawing (instanced billboard quads)
    // -----------------------------------------------------------------------

    function webGPUEmptyPointDrawStats() {
      return {
        pointDrawEntries: 0,
        pointDrawInstances: 0,
        pointDrawCalls: 0,
        pointSkippedEmpty: 0,
        pointSkippedNoPositions: 0,
      };
    }

    function drawPointsEntries(pass, bundle, cam, timeSeconds) {
      var stats = webGPUEmptyPointDrawStats();
      var pointsArray = Array.isArray(bundle.points) ? bundle.points : [];
      if (pointsArray.length === 0) return stats;

      var env = bundle.environment || {};
      var fogDensity = sceneNumber(env.fogDensity, 0);
      var fogColorRGBA = sceneColorRGBA(env.fogColor, [0.5, 0.5, 0.5, 1]);

      var _pointsModelMat = new Float32Array(16);
      var _pointsTilt = new Float32Array(16);
      var _pointsSpin = new Float32Array(16);

      for (var i = 0; i < pointsArray.length; i++) {
        var entry = pointsArray[i];
        var count = sceneNumber(entry.count, 0);
        if (count <= 0) {
          stats.pointSkippedEmpty += 1;
          continue;
        }

        // Compute model matrix (same logic as WebGL2 backend).
        var px = sceneNumber(entry.x, 0);
        var py = sceneNumber(entry.y, 0);
        var pz = sceneNumber(entry.z, 0);
        var hasSpin = sceneNumber(entry.spinX, 0) !== 0 || sceneNumber(entry.spinY, 0) !== 0 || sceneNumber(entry.spinZ, 0) !== 0;

        if (hasSpin) {
          var spx = sceneNumber(entry.spinX, 0) * timeSeconds;
          var spy = sceneNumber(entry.spinY, 0) * timeSeconds;
          var spz = sceneNumber(entry.spinZ, 0) * timeSeconds;
          var csx = Math.cos(spx), ssx = Math.sin(spx);
          var csy = Math.cos(spy), ssy = Math.sin(spy);
          var csz = Math.cos(spz), ssz = Math.sin(spz);
          _pointsSpin[0] = csy*csz; _pointsSpin[4] = ssx*ssy*csz-csx*ssz; _pointsSpin[8]  = csx*ssy*csz+ssx*ssz; _pointsSpin[12] = 0;
          _pointsSpin[1] = csy*ssz; _pointsSpin[5] = ssx*ssy*ssz+csx*csz; _pointsSpin[9]  = csx*ssy*ssz-ssx*csz; _pointsSpin[13] = 0;
          _pointsSpin[2] = -ssy;    _pointsSpin[6] = ssx*csy;             _pointsSpin[10] = csx*csy;             _pointsSpin[14] = 0;
          _pointsSpin[3] = 0;       _pointsSpin[7] = 0;                   _pointsSpin[11] = 0;                   _pointsSpin[15] = 1;

          var rx = sceneNumber(entry.rotationX, 0);
          var ry = sceneNumber(entry.rotationY, 0);
          var rz = sceneNumber(entry.rotationZ, 0);
          var cxr = Math.cos(rx), sxr = Math.sin(rx);
          var cyr = Math.cos(ry), syr = Math.sin(ry);
          var czr = Math.cos(rz), szr = Math.sin(rz);
          _pointsTilt[0] = cyr*czr; _pointsTilt[4] = sxr*syr*czr-cxr*szr; _pointsTilt[8]  = cxr*syr*czr+sxr*szr; _pointsTilt[12] = px;
          _pointsTilt[1] = cyr*szr; _pointsTilt[5] = sxr*syr*szr+cxr*czr; _pointsTilt[9]  = cxr*syr*szr-sxr*czr; _pointsTilt[13] = py;
          _pointsTilt[2] = -syr;    _pointsTilt[6] = sxr*cyr;             _pointsTilt[10] = cxr*cyr;             _pointsTilt[14] = pz;
          _pointsTilt[3] = 0;       _pointsTilt[7] = 0;                   _pointsTilt[11] = 0;                   _pointsTilt[15] = 1;

          sceneMat4MultiplyInto(_pointsModelMat, _pointsTilt, _pointsSpin);
        } else {
          var rx2 = sceneNumber(entry.rotationX, 0);
          var ry2 = sceneNumber(entry.rotationY, 0);
          var rz2 = sceneNumber(entry.rotationZ, 0);
          var cxr2 = Math.cos(rx2), sxr2 = Math.sin(rx2);
          var cyr2 = Math.cos(ry2), syr2 = Math.sin(ry2);
          var czr2 = Math.cos(rz2), szr2 = Math.sin(rz2);
          _pointsModelMat[0] = cyr2*czr2; _pointsModelMat[4] = sxr2*syr2*czr2-cxr2*szr2; _pointsModelMat[8]  = cxr2*syr2*czr2+sxr2*szr2; _pointsModelMat[12] = px;
          _pointsModelMat[1] = cyr2*szr2; _pointsModelMat[5] = sxr2*syr2*szr2+cxr2*czr2; _pointsModelMat[9]  = cxr2*syr2*szr2-sxr2*czr2; _pointsModelMat[13] = py;
          _pointsModelMat[2] = -syr2;     _pointsModelMat[6] = sxr2*cyr2;                _pointsModelMat[10] = cxr2*cyr2;                _pointsModelMat[14] = pz;
          _pointsModelMat[3] = 0;         _pointsModelMat[7] = 0;                        _pointsModelMat[11] = 0;                        _pointsModelMat[15] = 1;
        }

        // Build PointsUniforms buffer.
        // Layout: mat4x4f(64) + vec4 defaultColorAndSize(16) +
        // vec4u flags(16) + vec4 params(16) + vec4 fogColor(16) = 128.
        pointsUniformScratchF.fill(0);
        var puF = pointsUniformScratchF;
        var puU = pointsUniformScratchU;

        puF.set(_pointsModelMat, 0);   // modelMatrix @ 0
        var defaultColorRGBA = sceneColorRGBA(entry.color, [1, 1, 1, 1]);
        puF[16] = defaultColorRGBA[0]; // defaultColorAndSize.r @ 64
        puF[17] = defaultColorRGBA[1];
        puF[18] = defaultColorRGBA[2];
        puF[19] = sceneNumber(entry.size, 1); // defaultColorAndSize.w/defaultSize
        puU[20] = 0; // flags.x: hasPerVertexColor
        puU[21] = 0; // flags.y: hasPerVertexSize
        puU[22] = entry.attenuation ? 1 : 0; // flags.z: sizeAttenuation
        puU[23] = scenePointStyleCode(entry.style); // flags.w: pointStyle
        puF[24] = clamp01(sceneNumber(entry.opacity, 1)); // params.x: opacity
        puF[25] = fogDensity > 0 ? 1 : 0; // params.y: hasFog
        puF[26] = fogDensity; // params.z: fogDensity
        puF[27] = sceneNumber(entry.maxPixelSize, 0); // params.w: maxPixelSize
        puF[28] = fogColorRGBA[0]; // fogColor.r @ 112
        puF[29] = fogColorRGBA[1];
        puF[30] = fogColorRGBA[2];
        puF[31] = sceneNumber(entry.minPixelSize, 0); // fogColor.a carries minPixelSize for points.

        // Cache particle typed arrays on entry.
        var rawPositions = entry.positions;
        if (!entry._cachedPos && rawPositions && (Array.isArray(rawPositions) || sceneIsNumericTypedArray(rawPositions)) && rawPositions.length >= count * 3) {
          entry._cachedPos = rawPositions instanceof Float32Array ? rawPositions : new Float32Array(rawPositions);
        }
        var rawSizes = entry.sizes;
        if (!entry._cachedSizes && rawSizes && (Array.isArray(rawSizes) || sceneIsNumericTypedArray(rawSizes)) && rawSizes.length >= count) {
          entry._cachedSizes = rawSizes instanceof Float32Array ? rawSizes : new Float32Array(rawSizes);
        }
        var rawColors = entry.colors;
        if (!entry._cachedColors && rawColors && (Array.isArray(rawColors) || sceneIsNumericTypedArray(rawColors)) && rawColors.length >= count) {
          if (Array.isArray(rawColors) && typeof rawColors[0] === "string") {
            entry._cachedColors = new Float32Array(count * 4);
            for (var ci = 0; ci < count; ci++) {
              var crgba = sceneColorRGBA(rawColors[ci], [1, 1, 1, 1]);
              entry._cachedColors[ci * 4] = crgba[0];
              entry._cachedColors[ci * 4 + 1] = crgba[1];
              entry._cachedColors[ci * 4 + 2] = crgba[2];
              entry._cachedColors[ci * 4 + 3] = crgba[3];
            }
          } else if (rawColors.length >= count * 4) {
            entry._cachedColors = new Float32Array(rawColors);
          } else if (rawColors.length >= count * 3) {
            entry._cachedColors = new Float32Array(count * 4);
            for (var ci2 = 0; ci2 < count; ci2++) {
              entry._cachedColors[ci2 * 4] = rawColors[ci2 * 3];
              entry._cachedColors[ci2 * 4 + 1] = rawColors[ci2 * 3 + 1];
              entry._cachedColors[ci2 * 4 + 2] = rawColors[ci2 * 3 + 2];
              entry._cachedColors[ci2 * 4 + 3] = 1;
            }
          }
        }

        if (!entry._cachedPos) {
          stats.pointSkippedNoPositions += 1;
          continue;
        }

        var hasSizes = !!entry._cachedSizes;
        var hasColors = !!entry._cachedColors;
        puU[20] = hasColors ? 1 : 0;
        puU[21] = hasSizes ? 1 : 0;

        var pointsUniformBuffer = ensurePointsUniformGPUBuffer(entry, puF);

        // Build particle instance vertex buffer.
        // Each particle: vec3f position(12) + f32 size(4) + vec4f color(16) = 32 bytes.
        var particleData = ensurePointsParticleData(entry, count, hasSizes, hasColors, defaultColorRGBA);
        var pointsParticleBuffer = ensurePointsParticleVertexGPUBuffer(entry, particleData);

        var pointsBG = device.createBindGroup({
          layout: pointsUniformBindGroupLayout,
          entries: [
            { binding: 0, resource: { buffer: pointsUniformBuffer } },
          ],
        });

        // Select pipeline based on blend mode.
        var blendMode = typeof entry.blendMode === "string" ? entry.blendMode.toLowerCase() : "";
        var depthWrite = entry.depthWrite !== false;
        var validBlend = blendMode === "additive" || blendMode === "alpha" ? blendMode : "opaque";
        var pipeline = getPointsVertexPipeline(validBlend, depthWrite);

        pass.setPipeline(pipeline);
        pass.setVertexBuffer(0, pointsParticleBuffer);
        pass.setBindGroup(2, pointsBG);
        pass.draw(6, count); // 6 vertices per quad, count instances
        stats.pointDrawEntries += 1;
        stats.pointDrawInstances += count;
        stats.pointDrawCalls += 1;
      }
      return stats;
    }

    function drawComputeParticleEntries(pass, records, environment, timeSeconds) {
      var stats = {
        computeParticleDrawEntries: 0,
        computeParticleDrawInstances: 0,
        computeParticleDrawCalls: 0,
        computeParticleSkippedNotReady: 0,
        computeParticleSkippedEmpty: 0,
      };
      if (!Array.isArray(records) || records.length === 0) return stats;

      var env = environment || {};
      var fogDensity = sceneNumber(env.fogDensity, 0);
      var fogColorRGBA = sceneColorRGBA(env.fogColor, [0.5, 0.5, 0.5, 1]);
      var _computeModelMat = new Float32Array(16);
      var _computeTilt = new Float32Array(16);
      var _computeSpin = new Float32Array(16);

      for (var i = 0; i < records.length; i++) {
        var record = records[i];
        var system = record && record.system;
        if (!system || !system.renderBuffer || system.count <= 0) {
          stats.computeParticleSkippedEmpty += 1;
          continue;
        }
        if (typeof system.isReady === "function" && !system.isReady()) {
          stats.computeParticleSkippedNotReady += 1;
          continue;
        }

        var entry = system.entry && typeof system.entry === "object" ? system.entry : {};
        var material = entry.material && typeof entry.material === "object" ? entry.material : {};
        var emitter = entry.emitter && typeof entry.emitter === "object" ? entry.emitter : {};

        var px = sceneNumber(emitter.x, 0);
        var py = sceneNumber(emitter.y, 0);
        var pz = sceneNumber(emitter.z, 0);
        var hasSpin = sceneNumber(emitter.spinX, 0) !== 0 || sceneNumber(emitter.spinY, 0) !== 0 || sceneNumber(emitter.spinZ, 0) !== 0;

        if (hasSpin) {
          var spx = sceneNumber(emitter.spinX, 0) * timeSeconds;
          var spy = sceneNumber(emitter.spinY, 0) * timeSeconds;
          var spz = sceneNumber(emitter.spinZ, 0) * timeSeconds;
          var csx = Math.cos(spx), ssx = Math.sin(spx);
          var csy = Math.cos(spy), ssy = Math.sin(spy);
          var csz = Math.cos(spz), ssz = Math.sin(spz);
          _computeSpin[0] = csy*csz; _computeSpin[4] = ssx*ssy*csz-csx*ssz; _computeSpin[8]  = csx*ssy*csz+ssx*ssz; _computeSpin[12] = 0;
          _computeSpin[1] = csy*ssz; _computeSpin[5] = ssx*ssy*ssz+csx*csz; _computeSpin[9]  = csx*ssy*ssz-ssx*csz; _computeSpin[13] = 0;
          _computeSpin[2] = -ssy;    _computeSpin[6] = ssx*csy;             _computeSpin[10] = csx*csy;             _computeSpin[14] = 0;
          _computeSpin[3] = 0;       _computeSpin[7] = 0;                   _computeSpin[11] = 0;                   _computeSpin[15] = 1;

          var rx = sceneNumber(emitter.rotationX, 0);
          var ry = sceneNumber(emitter.rotationY, 0);
          var rz = sceneNumber(emitter.rotationZ, 0);
          var cxr = Math.cos(rx), sxr = Math.sin(rx);
          var cyr = Math.cos(ry), syr = Math.sin(ry);
          var czr = Math.cos(rz), szr = Math.sin(rz);
          _computeTilt[0] = cyr*czr; _computeTilt[4] = sxr*syr*czr-cxr*szr; _computeTilt[8]  = cxr*syr*czr+sxr*szr; _computeTilt[12] = px;
          _computeTilt[1] = cyr*szr; _computeTilt[5] = sxr*syr*szr+cxr*czr; _computeTilt[9]  = cxr*syr*szr-sxr*czr; _computeTilt[13] = py;
          _computeTilt[2] = -syr;    _computeTilt[6] = sxr*cyr;             _computeTilt[10] = cxr*cyr;             _computeTilt[14] = pz;
          _computeTilt[3] = 0;       _computeTilt[7] = 0;                   _computeTilt[11] = 0;                   _computeTilt[15] = 1;

          sceneMat4MultiplyInto(_computeModelMat, _computeTilt, _computeSpin);
        } else {
          var rx2 = sceneNumber(emitter.rotationX, 0);
          var ry2 = sceneNumber(emitter.rotationY, 0);
          var rz2 = sceneNumber(emitter.rotationZ, 0);
          var cxr2 = Math.cos(rx2), sxr2 = Math.sin(rx2);
          var cyr2 = Math.cos(ry2), syr2 = Math.sin(ry2);
          var czr2 = Math.cos(rz2), szr2 = Math.sin(rz2);
          _computeModelMat[0] = cyr2*czr2; _computeModelMat[4] = sxr2*syr2*czr2-cxr2*szr2; _computeModelMat[8]  = cxr2*syr2*czr2+sxr2*szr2; _computeModelMat[12] = px;
          _computeModelMat[1] = cyr2*szr2; _computeModelMat[5] = sxr2*syr2*szr2+cxr2*czr2; _computeModelMat[9]  = cxr2*syr2*szr2-sxr2*czr2; _computeModelMat[13] = py;
          _computeModelMat[2] = -syr2;     _computeModelMat[6] = sxr2*cyr2;                _computeModelMat[10] = cxr2*cyr2;                _computeModelMat[14] = pz;
          _computeModelMat[3] = 0;         _computeModelMat[7] = 0;                        _computeModelMat[11] = 0;                        _computeModelMat[15] = 1;
        }

        pointsUniformScratchF.fill(0);
        var puF = pointsUniformScratchF;
        var puU = pointsUniformScratchU;
        puF.set(_computeModelMat, 0);

        var defaultColorRGBA = sceneColorRGBA(material.color, [1, 1, 1, 1]);
        puF[16] = defaultColorRGBA[0];
        puF[17] = defaultColorRGBA[1];
        puF[18] = defaultColorRGBA[2];
        puF[19] = sceneNumber(material.size, 1);
        puU[20] = 1;
        puU[21] = 1;
        puU[22] = material.attenuation ? 1 : 0;
        puU[23] = scenePointStyleCode(material.style);
        puF[24] = 1;
        puF[25] = fogDensity > 0 ? 1 : 0;
        puF[26] = fogDensity;
        puF[27] = sceneNumber(material.maxPixelSize, 0);
        puF[28] = fogColorRGBA[0];
        puF[29] = fogColorRGBA[1];
        puF[30] = fogColorRGBA[2];
        puF[31] = sceneNumber(material.minPixelSize, 0);

        var pointsUniformBuffer = ensurePointsUniformGPUBuffer(system, puF);

        var pointsBG = device.createBindGroup({
          layout: pointsBindGroupLayout,
          entries: [
            { binding: 0, resource: { buffer: pointsUniformBuffer } },
            { binding: 1, resource: { buffer: system.renderBuffer } },
          ],
        });

        var blendMode = typeof material.blendMode === "string" ? material.blendMode.toLowerCase() : "";
        var validBlend = blendMode === "additive" || blendMode === "alpha" ? blendMode : "opaque";
        var depthWrite = entry.depthWrite === true || (validBlend === "opaque" && entry.depthWrite !== false);
        var pipeline = getPointsPipeline(validBlend, depthWrite);

        pass.setPipeline(pipeline);
        pass.setBindGroup(2, pointsBG);
        pass.draw(6, system.count);
        stats.computeParticleDrawEntries += 1;
        stats.computeParticleDrawInstances += system.count;
        stats.computeParticleDrawCalls += 1;
      }
      return stats;
    }

    function webGPUPlannedPointStats(bundle, computeParticleRecords) {
      var pointsArray = Array.isArray(bundle && bundle.points) ? bundle.points : [];
      var pointInstances = 0;
      for (var i = 0; i < pointsArray.length; i++) {
        pointInstances += Math.max(0, Math.floor(sceneNumber(pointsArray[i] && pointsArray[i].count, 0)));
      }
      var computeRecords = Array.isArray(computeParticleRecords) ? computeParticleRecords : [];
      var computeInstances = 0;
      for (var c = 0; c < computeRecords.length; c++) {
        var system = computeRecords[c] && computeRecords[c].system;
        computeInstances += Math.max(0, Math.floor(sceneNumber(system && system.count, 0)));
      }
      return {
        pointEntries: pointsArray.length,
        pointInstances: pointInstances,
        computeParticleEntries: computeRecords.length,
        computeParticleInstances: computeInstances,
      };
    }

    function webGPUPlannedInstanceCount(list) {
      var total = 0;
      var source = Array.isArray(list) ? list : [];
      for (var i = 0; i < source.length; i++) {
        total += Math.max(0, Math.floor(sceneNumber(source[i] && source[i].count, 0)));
      }
      return total;
    }

    function webGPUCustomMaterialStats(materials) {
      var stats = { customMaterialFallbacks: 0, customWGSLFallbacks: 0, customUniformFallbacks: 0 };
      var source = Array.isArray(materials) ? materials : [];
      for (var i = 0; i < source.length; i++) {
        var material = source[i] || {};
        if (sceneSelenaIsMaterial(material)) {
          continue;
        }
        var hasWGSL = (typeof material.customVertexWGSL === "string" && material.customVertexWGSL.trim()) ||
          (typeof material.customFragmentWGSL === "string" && material.customFragmentWGSL.trim());
        var hasCustomUniforms = material.customUniforms && typeof material.customUniforms === "object" && Object.keys(material.customUniforms).length > 0;
        if (!hasWGSL && !hasCustomUniforms) {
          continue;
        }
        stats.customMaterialFallbacks += 1;
        if (hasWGSL) {
          stats.customWGSLFallbacks += 1;
        }
        if (hasCustomUniforms) {
          stats.customUniformFallbacks += 1;
        }
      }
      return stats;
    }

    function publishWebGPUFrameStats(stats) {
      var mount = canvas && canvas.parentNode;
      if (!mount) return;
      webGPUFrameSeq += 1;
      var published = Object.assign({}, stats || {}, {
        frameSeq: webGPUFrameSeq,
        frameAt: (typeof performance !== "undefined" && typeof performance.now === "function") ? performance.now() : Date.now(),
      });
      lastWebGPUFrameStats = published;
      mount.__gosxScene3DWebGPUStats = published;
      if (typeof mount.setAttribute !== "function") return;
      mount.setAttribute("data-gosx-scene3d-webgpu-frame-seq", String(published.frameSeq || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-frame-at", String(published.frameAt || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-point-entries", String(published.pointEntries || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-point-instances", String(published.pointInstances || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-point-draw-entries", String(published.pointDrawEntries || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-point-draw-instances", String(published.pointDrawInstances || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-point-draw-calls", String(published.pointDrawCalls || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-point-skipped-empty", String(published.pointSkippedEmpty || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-point-skipped-no-positions", String(published.pointSkippedNoPositions || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-entries", String(published.computeParticleEntries || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-instances", String(published.computeParticleInstances || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-draw-entries", String(published.computeParticleDrawEntries || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-draw-instances", String(published.computeParticleDrawInstances || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-draw-calls", String(published.computeParticleDrawCalls || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-skipped-empty", String(published.computeParticleSkippedEmpty || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-skipped-not-ready", String(published.computeParticleSkippedNotReady || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-mesh-objects", String(published.meshObjects || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-skinned-mesh-objects", String(published.skinnedMeshObjects || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-computed-morph-dispatches", String(published.computedMorphDispatches || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-computed-morph-vertices", String(published.computedMorphVertices || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-computed-morph-kernel", String(published.computedMorphKernel || ""));
      mount.setAttribute("data-gosx-scene3d-webgpu-elio-skinning-dispatches", String(published.elioSkinningDispatches || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-elio-skinning-vertices", String(published.elioSkinningVertices || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-elio-skinning-kernel", String(published.elioSkinningKernel || ""));
      mount.setAttribute("data-gosx-scene3d-webgpu-instanced-meshes", String(published.instancedMeshes || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-instanced-instances", String(published.instancedInstances || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-line-entries", String(published.lineEntries || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-surface-entries", String(published.surfaceEntries || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-post-effects", String(published.postEffects || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-post-ssao-passes", String(published.postSSAOPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-post-dof-passes", String(published.postDOFPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-custom-material-fallbacks", String(published.customMaterialFallbacks || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-custom-wgsl-fallbacks", String(published.customWGSLFallbacks || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-custom-uniform-fallbacks", String(published.customUniformFallbacks || 0));
      if (published.customMaterialFallbacks > 0) {
        mount.setAttribute("data-gosx-scene3d-webgpu-custom-material-fallback-reason", "custom-wgsl-hooks-unsupported");
      } else {
        mount.removeAttribute("data-gosx-scene3d-webgpu-custom-material-fallback-reason");
      }
      if (published.lastError) {
        mount.setAttribute("data-gosx-scene3d-webgpu-last-error", String(published.lastError));
      } else {
        mount.removeAttribute("data-gosx-scene3d-webgpu-last-error");
      }
    }

    function isWebGPUErrorScopeLifecycleMessage(message) {
      var text = String(message || "").toLowerCase();
      return text.indexOf("instance dropped") >= 0 && text.indexOf("poperrorscope") >= 0;
    }

    function reportWebGPUFrameError(message) {
      var text = String(message || "").slice(0, 500);
      if (!text) return;
      var stats = Object.assign({}, lastWebGPUFrameStats || {}, { renderer: "webgpu", lastError: text });
      publishWebGPUFrameStats(stats);
      if (webGPUErrorReportCount >= 3) return;
      webGPUErrorReportCount += 1;
      try {
        if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
          window.__gosx_emit("error", "scene3d-webgpu", "render-error", {
            error: text,
            pointEntries: stats.pointEntries || 0,
            pointInstances: stats.pointInstances || 0,
            pointDrawEntries: stats.pointDrawEntries || 0,
            pointDrawInstances: stats.pointDrawInstances || 0,
            computeParticleDrawInstances: stats.computeParticleDrawInstances || 0,
          });
        }
      } catch (_err) {}
    }

    function beginWebGPUErrorScope() {
      if (!device || pendingWebGPUErrorScope || typeof device.pushErrorScope !== "function") return false;
      try {
        device.pushErrorScope("validation");
        pendingWebGPUErrorScope = true;
        return true;
      } catch (_err) {
        pendingWebGPUErrorScope = false;
        return false;
      }
    }

    function endWebGPUErrorScope() {
      if (!device || !pendingWebGPUErrorScope || typeof device.popErrorScope !== "function") return;
      pendingWebGPUErrorScope = false;
      try {
        device.popErrorScope().then(function(error) {
          if (error) {
            reportWebGPUFrameError(error.message || String(error));
          } else if (lastWebGPUFrameStats && lastWebGPUFrameStats.lastError) {
            var clean = Object.assign({}, lastWebGPUFrameStats);
            delete clean.lastError;
            publishWebGPUFrameStats(clean);
          }
        }).catch(function(error) {
          var message = error && error.message ? error.message : String(error);
          if (isWebGPUErrorScopeLifecycleMessage(message)) return;
          reportWebGPUFrameError(message);
        });
      } catch (error) {
        var message = error && error.message ? error.message : String(error);
        if (isWebGPUErrorScopeLifecycleMessage(message)) return;
        reportWebGPUFrameError(message);
      }
    }

    // -----------------------------------------------------------------------
    // Main render function
    // -----------------------------------------------------------------------

    function render(bundle, viewport) {
      if (!device) {
        startInit();
        return;
      }
      if (initFailed || !bundle) return;

      var hasPBRData = Boolean(
        bundle.worldMeshPositions &&
        bundle.worldMeshNormals &&
        Array.isArray(bundle.meshObjects) &&
        bundle.meshObjects.length > 0
      );
      var hasPointsData = (Array.isArray(bundle.points) && bundle.points.length > 0) ||
        (Array.isArray(bundle.computeParticles) && bundle.computeParticles.length > 0);
      var hasInstancedData = Array.isArray(bundle.instancedMeshes) && bundle.instancedMeshes.length > 0;
      var hasWorldLines = hasWorldLineData(bundle);
      var hasScreenLines = hasScreenLineData(bundle);
      var hasSurfaces = hasSurfaceData(bundle);
      if (!hasPBRData && !hasPointsData && !hasInstancedData && !hasWorldLines && !hasScreenLines && !hasSurfaces) return;
      var preparedScene = typeof prepareScene === "function"
        ? prepareScene(bundle, bundle.camera, viewport, lastPreparedScene, {
          mount: canvas && canvas.parentNode || null,
          sentinels: canvas && canvas.parentNode && canvas.parentNode.__gosxScene3DSentinels || null,
        })
        : null;
      if (preparedScene) {
        lastPreparedScene = preparedScene;
        bundle = preparedScene.ir || bundle;
        if (canvas && canvas.parentNode) {
          canvas.parentNode.__gosxScene3DCSSDynamic = Boolean(preparedScene.cssDynamic);
        }
        hasPBRData = Boolean(
          bundle.worldMeshPositions &&
          bundle.worldMeshNormals &&
          Array.isArray(bundle.meshObjects) &&
          bundle.meshObjects.length > 0
        );
        hasPointsData = (Array.isArray(bundle.points) && bundle.points.length > 0) ||
          (Array.isArray(bundle.computeParticles) && bundle.computeParticles.length > 0);
        hasInstancedData = Array.isArray(bundle.instancedMeshes) && bundle.instancedMeshes.length > 0;
        hasWorldLines = hasWorldLineData(bundle);
        hasScreenLines = hasScreenLineData(bundle);
        hasSurfaces = hasSurfaceData(bundle);
      }

      var width = canvas.width;
      var height = canvas.height;
      if (width <= 0 || height <= 0) return;

      // Opt-in perf instrumentation. Mirrors the WebGL renderer: when
      // window.__gosx_scene3d_perf is truthy, emit performance.mark +
      // measure pairs so a PerformanceObserver (installed by gosx perf's
      // instrument.js) can collect per-frame wall-clock durations.
      var perfEnabled = typeof window !== "undefined" && window.__gosx_scene3d_perf === true;
      if (perfEnabled) {
        performance.mark("scene3d-render-start");
      }

      // Reconfigure context if canvas resized.
      configureWebGPUCanvas();

      // Determine post-processing.
      var postEffects = Array.isArray(bundle.postEffects) ? bundle.postEffects : [];
      var usePostProcessing = postEffects.length > 0;

      // Compute scaled render-target dimensions (PostFX memory cap).
      var postFXMaxPixels = (typeof bundle.postFXMaxPixels === "number") ? bundle.postFXMaxPixels : 0;
      var postfxFactor = usePostProcessing
        ? resolvePostFXFactor(postFXMaxPixels, width * height)
        : 1;
      var scaledW = Math.max(1, Math.floor(width * postfxFactor));
      var scaledH = Math.max(1, Math.floor(height * postfxFactor));
      var sampleCount = resolveWebGPUSampleCount(bundle);
      activeSampleCount = sampleCount;

      // Upload per-frame uniforms (use scaled dims so point sprites and
      // projection aspect match the actual render target, not the canvas).
      var cam = uploadFrameUniforms(bundle.camera, scaledW, scaledH, !usePostProcessing);
      uploadLights(bundle.lights);
      uploadFogUniforms(bundle.environment);
      uploadEnvUniforms(bundle.environment);

      // --- Shadow Pass ---
      var shadowLightMatrices = [null, null];
      var shadowLightIndices = [-1, -1];
      var activeShadowCount = 0;

      var encoder = device.createCommandEncoder({ label: "gosx-frame" });
      var scopedFrameErrors = beginWebGPUErrorScope();
      var frameTimeSeconds = performance.now() / 1000;
      var computeParticleRecords = updateComputeParticleSystems(bundle.computeParticles, encoder, frameTimeSeconds);
      var computedMorphStats = updateComputedMorphMeshes(bundle, encoder);
      var elioSkinStats = updateElioSkinnedMeshes(bundle, encoder);
      var pbrSceneBuffers = hasPBRData ? ensurePBRSceneAttributeBuffers(bundle) : null;

      var lightArray = Array.isArray(bundle.lights) ? bundle.lights : [];
      var sceneBounds = null;
      var shadowMaxPixels = (typeof bundle.shadowMaxPixels === "number") ? bundle.shadowMaxPixels : 0;

      for (var li = 0; li < lightArray.length && activeShadowCount < 2; li++) {
        var light = lightArray[li];
        if (!light || !light.castShadow) continue;
        var kind = typeof light.kind === "string" ? light.kind.toLowerCase() : "";
        if (kind !== "directional") continue;

        if (!sceneBounds) sceneBounds = webGPUShadowComputeBounds(bundle);

        var slot = activeShadowCount;
        var shadowSize = sceneNumber(light.shadowSize, 1024);
        shadowSize = Math.max(256, Math.min(4096, shadowSize));
        shadowSize = resolveShadowSize(shadowSize, shadowMaxPixels);

        if (!shadowSlots[slot] || shadowSlots[slot].size !== shadowSize) {
          if (shadowSlots[slot]) shadowSlots[slot].texture.destroy();
          shadowSlots[slot] = wgpuCreateShadowMap(device, shadowSize);
        }

        var lightMatrix = sceneShadowLightSpaceMatrix(light, sceneBounds);
        shadowLightMatrices[slot] = lightMatrix;
        shadowLightIndices[slot] = li;

        renderShadowPass(encoder, lightMatrix, bundle, shadowSlots[slot], pbrSceneBuffers);
        activeShadowCount++;
      }

      uploadShadowUniforms(shadowLightMatrices, shadowLightIndices, bundle.lights);

      // Create frame bind group.
      var shadowView0 = shadowSlots[0] ? shadowSlots[0].view : null;
      var shadowView1 = shadowSlots[1] ? shadowSlots[1].view : null;
      var frameBindGroup = createFrameBindGroup(shadowView0, shadowView1);

      // --- Main Render Target ---
      var mainColorView;
      var mainResolveView = null;
      var mainDepthTargetView;
      var postTarget = null;

      if (usePostProcessing) {
        if (!postProcessor) postProcessor = wgpuCreatePostProcessor(device, targetFormat);
        postTarget = postProcessor.getSceneTarget(scaledW, scaledH);
        if (sampleCount > 1) {
          mainColorView = ensureMSAAColor(scaledW, scaledH, sampleCount);
          mainResolveView = postTarget.colorView;
          ensureMainDepth(scaledW, scaledH, sampleCount);
          mainDepthTargetView = mainDepthView;
        } else {
          mainColorView = postTarget.colorView;
          mainDepthTargetView = postTarget.depthView;
        }
      } else {
        var currentTexture = gpuCtx.getCurrentTexture();
        var currentView = currentTexture.createView();
        if (sampleCount > 1) {
          mainColorView = ensureMSAAColor(width, height, sampleCount);
          mainResolveView = currentView;
        } else {
          mainColorView = currentView;
        }
        ensureMainDepth(width, height, sampleCount);
        mainDepthTargetView = mainDepthView;
      }

      // Clear color.
      var bgStr = typeof bundle.background === "string" ? bundle.background.trim().toLowerCase() : "";
      var bg = bgStr === "transparent" ? [0, 0, 0, 0] : sceneColorRGBA(bundle.background, [0.03, 0.08, 0.12, 1]);

      var mainColorAttachment = {
        view: mainColorView,
        loadOp: "clear",
        storeOp: "store",
        clearValue: { r: bg[0], g: bg[1], b: bg[2], a: bg[3] },
      };
      if (mainResolveView) {
        mainColorAttachment.resolveTarget = mainResolveView;
      }

      var mainPass = encoder.beginRenderPass({
        colorAttachments: [mainColorAttachment],
        depthStencilAttachment: {
          view: mainDepthTargetView,
          depthLoadOp: "clear",
          depthClearValue: 1.0,
          depthStoreOp: "store",
        },
      });

      var materials = Array.isArray(bundle.materials) ? bundle.materials : [];
      var instancedDrawList = hasInstancedData
        ? buildInstancedDrawList(bundle, materials)
        : { opaque: [], alpha: [], additive: [] };
      var drawList = hasPBRData
        ? (preparedScene && preparedScene.pbrPasses ? preparedScene.pbrPasses : buildDrawList(bundle))
        : { opaque: [], alpha: [], additive: [] };
      var thickLineRecord = hasWorldLines ? webGPUThickLineRecord(bundle) : null;
      var worldLineEntries = hasWorldLines && !thickLineRecord ? webGPUWorldLinePasses(bundle) : [];
      var pointStats = webGPUPlannedPointStats(bundle, computeParticleRecords);
      var customMaterialStats = webGPUCustomMaterialStats(materials);
      var frameStats = {
        renderer: "webgpu",
        pointEntries: pointStats.pointEntries,
        pointInstances: pointStats.pointInstances,
        computeParticleEntries: pointStats.computeParticleEntries,
        computeParticleInstances: pointStats.computeParticleInstances,
        meshObjects: Array.isArray(bundle.meshObjects) ? bundle.meshObjects.length : 0,
        skinnedMeshObjects: webGPUCountSkinnedMeshes(bundle),
        computedMorphDispatches: computedMorphStats.computedMorphDispatches,
        computedMorphVertices: computedMorphStats.computedMorphVertices,
        computedMorphKernel: computedMorphStats.computedMorphKernel,
        elioSkinningDispatches: elioSkinStats.elioSkinningDispatches,
        elioSkinningVertices: elioSkinStats.elioSkinningVertices,
        elioSkinningKernel: elioSkinStats.elioSkinningKernel,
        instancedMeshes: Array.isArray(bundle.instancedMeshes) ? bundle.instancedMeshes.length : 0,
        instancedInstances: webGPUPlannedInstanceCount(bundle.instancedMeshes),
        lineEntries: thickLineRecord ? 1 : worldLineEntries.length,
        surfaceEntries: Array.isArray(bundle.surfaces) ? bundle.surfaces.length : 0,
        customMaterialFallbacks: customMaterialStats.customMaterialFallbacks,
        customWGSLFallbacks: customMaterialStats.customWGSLFallbacks,
        customUniformFallbacks: customMaterialStats.customUniformFallbacks,
      };

      // Draw PBR meshes, WebGPU-native instanced meshes, world lines, and textured surfaces.
      if (hasPBRData || hasInstancedData || hasWorldLines || hasSurfaces) {
        // Opaque pass.
        if (drawList.opaque.length > 0) {
          var opaquePipeline = getPBRPipeline("opaque", true);
          mainPass.setPipeline(opaquePipeline);
          mainPass.setBindGroup(0, frameBindGroup);
          drawPBRObjects(mainPass, drawList.opaque, bundle, materials, frameBindGroup, "opaque", true, pbrSceneBuffers);
        }
        if (instancedDrawList.opaque.length > 0) {
          var opaqueInstancedPipeline = getPBRInstancedPipeline("opaque", true);
          mainPass.setPipeline(opaqueInstancedPipeline);
          mainPass.setBindGroup(0, frameBindGroup);
          drawInstancedMeshes(mainPass, instancedDrawList.opaque, materials);
        }
        if (hasSurfaces) {
          drawSurfaceEntries(mainPass, bundle, materials, "opaque", frameBindGroup);
        }
        if (thickLineRecord) {
          drawThickWorldLineEntries(mainPass, thickLineRecord, "opaque", frameBindGroup);
        } else if (worldLineEntries.length > 0) {
          drawWorldLineEntries(mainPass, worldLineEntries, "opaque", frameBindGroup);
        }

        // Alpha pass.
        if (drawList.alpha.length > 0) {
          var alphaPipeline = getPBRPipeline("alpha", false);
          mainPass.setPipeline(alphaPipeline);
          mainPass.setBindGroup(0, frameBindGroup);
          drawPBRObjects(mainPass, drawList.alpha, bundle, materials, frameBindGroup, "alpha", false, pbrSceneBuffers);
        }
        if (instancedDrawList.alpha.length > 0) {
          var alphaInstancedPipeline = getPBRInstancedPipeline("alpha", false);
          mainPass.setPipeline(alphaInstancedPipeline);
          mainPass.setBindGroup(0, frameBindGroup);
          drawInstancedMeshes(mainPass, instancedDrawList.alpha, materials);
        }
        if (hasSurfaces) {
          drawSurfaceEntries(mainPass, bundle, materials, "alpha", frameBindGroup);
        }
        if (thickLineRecord) {
          drawThickWorldLineEntries(mainPass, thickLineRecord, "alpha", frameBindGroup);
        } else if (worldLineEntries.length > 0) {
          drawWorldLineEntries(mainPass, worldLineEntries, "alpha", frameBindGroup);
        }

        // Additive pass.
        if (drawList.additive.length > 0) {
          var additivePipeline = getPBRPipeline("additive", false);
          mainPass.setPipeline(additivePipeline);
          mainPass.setBindGroup(0, frameBindGroup);
          drawPBRObjects(mainPass, drawList.additive, bundle, materials, frameBindGroup, "additive", false, pbrSceneBuffers);
        }
        if (instancedDrawList.additive.length > 0) {
          var additiveInstancedPipeline = getPBRInstancedPipeline("additive", false);
          mainPass.setPipeline(additiveInstancedPipeline);
          mainPass.setBindGroup(0, frameBindGroup);
          drawInstancedMeshes(mainPass, instancedDrawList.additive, materials);
        }
        if (hasSurfaces) {
          drawSurfaceEntries(mainPass, bundle, materials, "additive", frameBindGroup);
        }
        if (thickLineRecord) {
          drawThickWorldLineEntries(mainPass, thickLineRecord, "additive", frameBindGroup);
        } else if (worldLineEntries.length > 0) {
          drawWorldLineEntries(mainPass, worldLineEntries, "additive", frameBindGroup);
        }
      }

      if (hasScreenLines) {
        drawScreenLines(mainPass, bundle, frameBindGroup);
      }

      // Draw points.
      if (hasPointsData) {
        mainPass.setBindGroup(0, frameBindGroup);
        // Create a dummy material bind group for group 1 (points pipeline layout requires it).
        var dummyMatBG = createMaterialBindGroup(null, false, defaultMaterialOwner);
        mainPass.setBindGroup(1, dummyMatBG);
        Object.assign(frameStats, drawPointsEntries(mainPass, bundle, cam, frameTimeSeconds));
        Object.assign(frameStats, drawComputeParticleEntries(mainPass, computeParticleRecords, bundle.environment, frameTimeSeconds));
      }

      mainPass.end();

      // Post-processing.
      if (usePostProcessing && postProcessor) {
        var screenView = gpuCtx.getCurrentTexture().createView();
        Object.assign(frameStats, postProcessor.apply(encoder, postEffects, scaledW, scaledH, width, height, screenView, bundle.camera));
      }

      device.queue.submit([encoder.finish()]);
      publishWebGPUFrameStats(frameStats);
      if (scopedFrameErrors) endWebGPUErrorScope();

      if (perfEnabled) {
        performance.mark("scene3d-render-end");
        performance.measure("scene3d-render", "scene3d-render-start", "scene3d-render-end");
        performance.clearMarks("scene3d-render-start");
        performance.clearMarks("scene3d-render-end");
      }
    }

    // -----------------------------------------------------------------------
    // Dispose
    // -----------------------------------------------------------------------

    function dispose() {
      if (!device) return;

      if (frameUniformBuffer) frameUniformBuffer.destroy();
      if (lightStorageBuffer) lightStorageBuffer.destroy();
      if (fogUniformBuffer) fogUniformBuffer.destroy();
      if (envUniformBuffer) envUniformBuffer.destroy();
      if (shadowUniformBuffer) shadowUniformBuffer.destroy();
      if (positionBuffer) positionBuffer.destroy();
      if (normalBuffer) normalBuffer.destroy();
      if (uvBuffer) uvBuffer.destroy();
      if (tangentBuffer) tangentBuffer.destroy();
      if (shadowPositionBuffer) shadowPositionBuffer.destroy();
      if (shadowFrameBuffer) shadowFrameBuffer.destroy();
      pointsEntryGPUBuffers.forEach(function(buffer) {
        if (buffer && typeof buffer.destroy === "function") buffer.destroy();
      });
      pointsEntryGPUBuffers.clear();
      disposeComputeParticleSystems();

      if (mainDepthTexture) mainDepthTexture.destroy();
      if (mainMSAATexture) mainMSAATexture.destroy();
      if (dummyShadowTex) dummyShadowTex.destroy();
      if (placeholderTex) placeholderTex.destroy();

      for (var si = 0; si < shadowSlots.length; si++) {
        if (shadowSlots[si]) shadowSlots[si].texture.destroy();
      }

      for (var record of textureCache.values()) {
        if (record && record.texture) record.texture.destroy();
      }
      textureCache.clear();
      selenaPipelineCache.clear();

      if (postProcessor) {
        postProcessor.dispose();
        postProcessor = null;
      }

      device = null;
    }

    // Device + GPU resources were already initialized synchronously
    // above (using the pre-probed device from 16z). If that setup
    // failed, initFailed is true and render() will no-op; return null
    // so the mount code can try to fall back — though note the canvas
    // is already tainted at this point (getContext("webgpu") ran
    // before initGPUResources), so the fallback will itself fail.
    // The probe in 16z is what prevents us from ever reaching this
    // state on broken backends — it verifies device creation works
    // before we're allowed to construct a renderer at all.
    if (initFailed) return sceneWebGPUFactoryFailure("init-failed: " + initError);

    function supportsBundle(bundle) {
      if (webGPUUnsupportedLineStyles(bundle)) {
        return false;
      }
      if (!webGPUCanUseThickWorldLines(bundle)) {
        return false;
      }
      return true;
    }

    function diagnostics() {
      var base = typeof sceneWebGPUDiagnostics === "function"
        ? sceneWebGPUDiagnostics()
        : {};
      var out = {};
      for (var key in base) {
        if (Object.prototype.hasOwnProperty.call(base, key)) {
          out[key] = base[key];
        }
      }
      out.renderer = "webgpu";
      out.targetFormat = targetFormat;
      out.activeSampleCount = activeSampleCount;
      out.presentationAlphaMode = activePresentation.alphaMode;
      out.presentationColorSpace = activePresentation.colorSpace;
      out.presentationToneMappingMode = activePresentation.toneMappingMode;
      out.powerPreference = activePowerPreference;
      out.postProcessing = !!postProcessor;
      out.customMaterialFallbacks = lastWebGPUFrameStats && lastWebGPUFrameStats.customMaterialFallbacks || 0;
      out.customMaterialFallbackReason = out.customMaterialFallbacks > 0 ? "custom-wgsl-hooks-unsupported" : "";
      out.skinnedMeshObjects = lastWebGPUFrameStats && lastWebGPUFrameStats.skinnedMeshObjects || 0;
      out.computedMorphDispatches = lastWebGPUFrameStats && lastWebGPUFrameStats.computedMorphDispatches || 0;
      out.computedMorphVertices = lastWebGPUFrameStats && lastWebGPUFrameStats.computedMorphVertices || 0;
      out.computedMorphKernel = lastWebGPUFrameStats && lastWebGPUFrameStats.computedMorphKernel || "";
      out.elioSkinningDispatches = lastWebGPUFrameStats && lastWebGPUFrameStats.elioSkinningDispatches || 0;
      out.elioSkinningVertices = lastWebGPUFrameStats && lastWebGPUFrameStats.elioSkinningVertices || 0;
      out.elioSkinningKernel = lastWebGPUFrameStats && lastWebGPUFrameStats.elioSkinningKernel || "";
      return out;
    }

    return {
      kind: "webgpu",
      type: "webgpu",
      supportsBundle: supportsBundle,
      diagnostics: diagnostics,
      render: render,
      dispose: dispose,
    };
  }

  function sceneWebGPUCommandSequence(bundle, viewport, previousPrepared) {
    var prepared = prepareScene(
      bundle || {},
      bundle && bundle.camera || {},
      viewport || {},
      previousPrepared || null
    );
    return scenePreparedCommandSequence(prepared);
  }

  // -----------------------------------------------------------------------
  // Integration
  // -----------------------------------------------------------------------

  // --- Early WebGPU adapter probe ---
  // Adapter probe + sceneWebGPUAvailable + createSceneWebGPURendererOrFallback
  // used to live here. They've been moved to:
  //   - 16z-scene-webgpu-probe.js (main scene3d bundle) — owns the
  //     probe, the stub sceneWebGPUAvailable, and the fallback factory
  //     that reads from window.__gosx_scene3d_webgpu_api.
  //   - This file is now loaded only via bootstrap-feature-scene3d-webgpu.js
  //     (the sub-feature chunk), whose suffix publishes
  //     createSceneWebGPURenderer to the api so the stub can dispatch.
  //
  // createSceneWebGPURenderer itself (the real factory, ~1300 lines
  // above) is still defined in this file and is exported by the suffix.

  // Local sceneWebGPUAvailable for use by createSceneWebGPURenderer's
  // own startup paths — checks the probe shared by the main bundle.
  // _externalProbe is a function (not a snapshot) so each call sees
  // the current probe state — the main bundle's probe is async and
  // may still be pending when this chunk first loads.
  function sceneWebGPUAvailable() {
    var probe = _externalProbe();
    return probe.ready && probe.adapter !== false && probe.adapter !== null;
  }
