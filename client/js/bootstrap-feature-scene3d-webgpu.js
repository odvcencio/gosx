(function() {
  "use strict";

  if (!window.__gosx_scene3d_api) {
    console.warn("[gosx] scene3d-webgpu chunk loaded without main scene3d bundle");
    return;
  }

  var sceneApi = window.__gosx_scene3d_api;
  var runtimeApi = window.__gosx_runtime_api || {};

  var sceneBool = sceneApi.sceneBool || function(v, d) { return v == null ? d : !!v; };
  var sceneNumber = sceneApi.sceneNumber || function(v, d) { var n = Number(v); return Number.isFinite(n) ? n : d; };
  var clamp01 = sceneApi.clamp01 || function(v) { return Math.max(0, Math.min(1, Number(v) || 0)); };
  var SCENE_POST_TONE_MAPPING = sceneApi.SCENE_POST_TONE_MAPPING || "toneMapping";
  var SCENE_POST_BLOOM = sceneApi.SCENE_POST_BLOOM || "bloom";
  var SCENE_POST_VIGNETTE = sceneApi.SCENE_POST_VIGNETTE || "vignette";
  var SCENE_POST_COLOR_GRADE = sceneApi.SCENE_POST_COLOR_GRADE || "colorGrade";
  var sceneColorRGBA = sceneApi.sceneColorRGBA || function() { return [0, 0, 0, 1]; };
  var scenePointStyleCode = sceneApi.scenePointStyleCode || function() { return 0; };
  var sceneRenderCamera = sceneApi.sceneRenderCamera || function(c) { return c; };
  var scenePBRDepthSort = sceneApi.scenePBRDepthSort;
  var scenePBRObjectRenderPass = sceneApi.scenePBRObjectRenderPass;
  var prepareScene = sceneApi.prepareScene || function(ir) { return { ir: ir, pbrPasses: null }; };
  var scenePreparedCommandSequence = sceneApi.scenePreparedCommandSequence || function() { return []; };
  var sceneCachedBuffer = sceneApi.sceneCachedBuffer;
  var scenePBRProjectionMatrix = sceneApi.scenePBRProjectionMatrix;
  var scenePBRViewMatrix = sceneApi.scenePBRViewMatrix;
  var sceneShadowLightSpaceMatrix = sceneApi.sceneShadowLightSpaceMatrix;
  var sceneShadowComputeBounds = sceneApi.sceneShadowComputeBounds;
  var resolvePostFXFactor = sceneApi.resolvePostFXFactor || function() { return 1; };
  var resolveShadowSize = sceneApi.resolveShadowSize || function(s) { return s; };

  function _externalProbe() {
    if (typeof window.__gosx_scene3d_webgpu_probe === "function") {
      return window.__gosx_scene3d_webgpu_probe();
    }
    return { adapter: null, device: null, ready: false };
  }

  var WGSL_COMMON_CONSTANTS = [
    "const PI: f32 = 3.14159265359;",
    "const MAX_LIGHTS: u32 = 8u;",
  ].join("\n");

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

  var WGSL_MATERIAL_STRUCT = [
    "struct MaterialUniforms {",
    "    albedo: vec3f,",
    "    roughness: f32,",
    "    metalness: f32,",
    "    emissive: f32,",
    "    opacity: f32,",
    "    unlit: u32,",
    "    hasAlbedoMap: u32,",
    "    hasNormalMap: u32,",
    "    hasRoughnessMap: u32,",
    "    hasMetalnessMap: u32,",
    "    hasEmissiveMap: u32,",
    "    receiveShadow: u32,",
    "    _pad0: u32,",
    "    _pad1: u32,",
    "};",
  ].join("\n");

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
    "    out.clipPos = frame.projMatrix * frame.viewMatrix * vec4f(in.position, 1.0);",
    "    return out;",
    "}",
  ].join("\n");

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
    "// 4-tap Poisson disk PCF shadow sampling.",
    "fn shadowFactor(lightSpaceMatrix: mat4x4f, bias: f32, useShadow0: bool) -> f32 {",
    "    let lightSpacePos = lightSpaceMatrix * vec4f(fragWorldPos, 1.0);",
    "    let projCoords3 = lightSpacePos.xyz / lightSpacePos.w;",
    "    let projCoords = projCoords3 * 0.5 + 0.5;",
    "",
    "    if (projCoords.z > 1.0) { return 1.0; }",
    "",
    "    let poissonDisk = array<vec2f, 4>(",
    "        vec2f(-0.94201624, -0.39906216),",
    "        vec2f(0.94558609, -0.76890725),",
    "        vec2f(-0.094184101, -0.92938870),",
    "        vec2f(0.34495938, 0.29387760),",
    "    );",
    "",
    "    var shadowVal: f32 = 0.0;",
    "    var texDim: vec2u;",
    "    if (useShadow0) {",
    "        texDim = textureDimensions(shadowMap0);",
    "    } else {",
    "        texDim = textureDimensions(shadowMap1);",
    "    }",
    "    let texelSize = 1.0 / f32(texDim.x);",
    "",
    "    for (var i = 0u; i < 4u; i = i + 1u) {",
    "        let sampleUV = projCoords.xy + poissonDisk[i] * texelSize;",
    "        let refDepth = projCoords.z - bias;",
    "        var tapShadow: f32;",
    "        if (useShadow0) {",
    "            tapShadow = textureSampleCompare(shadowMap0, shadowSampler0, sampleUV, refDepth);",
    "        } else {",
    "            tapShadow = textureSampleCompare(shadowMap1, shadowSampler1, sampleUV, refDepth);",
    "        }",
    "        shadowVal = shadowVal + tapShadow;",
    "    }",
    "    return shadowVal / 4.0;",
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
    "// Module-scope variable for world position (used by shadowFactor).",
    "var<private> fragWorldPos: vec3f;",
    "",
    "@fragment fn fragmentMain(in: VertexOutput) -> @location(0) vec4f {",
    "    fragWorldPos = in.worldPos;",
    "",
    "    // Resolve material properties, sampling textures when available.",
    "    var albedo = material.albedo;",
    "    if (material.hasAlbedoMap != 0u) {",
    "        let texAlbedo = textureSample(albedoTex, albedoSamp, in.uv);",
    "        albedo = albedo * texAlbedo.rgb;",
    "    }",
    "",
    "    var roughness = material.roughness;",
    "    if (material.hasRoughnessMap != 0u) {",
    "        roughness = roughness * textureSample(roughnessTex, roughnessSamp, in.uv).g;",
    "    }",
    "    roughness = clamp(roughness, 0.04, 1.0);",
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
    "        return vec4f(color, material.opacity);",
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
    "        let denominator = 4.0 * max(dot(N, V), 0.0) * NdotL + 0.0001;",
    "        let specular = numerator / denominator;",
    "",
    "        // Energy conservation: diffuse complement of specular.",
    "        let kD = (vec3f(1.0) - F) * (1.0 - metalness);",
    "",
    "        // Shadow attenuation for directional lights.",
    "        var shadowAtten: f32 = 1.0;",
    "        if (material.receiveShadow != 0u && lightType == 1u) {",
    "            if (shadow.hasShadow0 != 0u && i32(i) == shadow.shadowLightIndex0) {",
    "                shadowAtten = shadowFactor(shadow.lightSpaceMatrix0, shadow.shadowBias0, true);",
    "            } else if (shadow.hasShadow1 != 0u && i32(i) == shadow.shadowLightIndex1) {",
    "                shadowAtten = shadowFactor(shadow.lightSpaceMatrix1, shadow.shadowBias1, false);",
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
    "    return vec4f(color, material.opacity);",
    "}",
  ].join("\n");

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

  var WGSL_SHADOW_FRAGMENT = [
    "@fragment fn fragmentMain() {}",
  ].join("\n");

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
    "    defaultSize: f32,",
    "    defaultColor: vec3f,",
    "    hasPerVertexColor: u32,",
    "    hasPerVertexSize: u32,",
    "    sizeAttenuation: u32,",
    "    pointStyle: u32,",
    "    opacity: f32,",
    "    hasFog: u32,",
    "    fogDensity: f32,",
    "    fogColor: vec3f,",
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
    "    if (points.hasPerVertexSize == 0u) { rawSize = points.defaultSize; }",
    "",
    "    var pixelSize: f32;",
    "    if (points.sizeAttenuation != 0u) {",
    "        pixelSize = max(rawSize * (frame.viewportHeight * 0.5) / max(-viewPos.z, 0.001), 1.0);",
    "    } else {",
    "        pixelSize = max(rawSize, 1.0);",
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
    "    if (points.hasPerVertexColor != 0u) {",
    "        out.color = p.color.rgb;",
    "    } else {",
    "        out.color = points.defaultColor;",
    "    }",
    "    out.alpha = p.color.a * points.opacity;",
    "    out.pointCoord = quad + vec2f(0.5, 0.5);",
    "    out.pointSize = pixelSize;",
    "",
    "    // Fog.",
    "    if (points.hasFog != 0u) {",
    "        let dist = length(viewPos.xyz);",
    "        out.fogFactor = clamp(exp(-points.fogDensity * points.fogDensity * dist * dist), 0.0, 1.0);",
    "    } else {",
    "        out.fogFactor = 1.0;",
    "    }",
    "",
    "    return out;",
    "}",
  ].join("\n");

  var WGSL_POINTS_FRAGMENT = [
    "struct PointsUniforms {",
    "    modelMatrix: mat4x4f,",
    "    defaultSize: f32,",
    "    defaultColor: vec3f,",
    "    hasPerVertexColor: u32,",
    "    hasPerVertexSize: u32,",
    "    sizeAttenuation: u32,",
    "    pointStyle: u32,",
    "    opacity: f32,",
    "    hasFog: u32,",
    "    fogDensity: f32,",
    "    fogColor: vec3f,",
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
    "    if (points.pointStyle == 1u) {",
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
    "    }",
    "    if (points.hasFog != 0u) {",
    "        color = mix(points.fogColor, color, in.fogFactor);",
    "    }",
    "    return vec4f(color, alpha);",
    "}",
  ].join("\n");

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
    "    _pad0: f32,",
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
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    var color = textureSample(inputTex, inputSamp, uv).rgb;",
    "    color = color * params.exposure;",
    "    color = aces(color);",
    "    return vec4f(color, 1.0);",
    "}",
  ].join("\n");

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
    "",
    "    for (var i = 0u; i < 4u; i = i + 1u) {",
    "        let offset = params.direction * texelSize * offsets[i] * params.radius;",
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

  function wgpuAlignUp(size, alignment) {
    return Math.ceil(size / alignment) * alignment;
  }

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

  function wgpuEnsureBufferData(device, existingBuffer, usage, data) {
    var needed = wgpuAlignUp(Math.max(data.byteLength, 4), 4);
    if (existingBuffer && existingBuffer.size >= needed) {
      device.queue.writeBuffer(existingBuffer, 0, data);
      return existingBuffer;
    }
    if (existingBuffer) existingBuffer.destroy();
    return wgpuCreateBuffer(device, usage, data);
  }

  function wgpuPipelineKey(shaderVariant, blendMode, depthWrite, targetFormat, depthFormat) {
    return shaderVariant + "|" + blendMode + "|" + (depthWrite ? "1" : "0") + "|" + targetFormat + "|" + (depthFormat || "");
  }

  function wgpuLoadTexture(device, url, cache) {
    if (!cache) return null;
    var key = typeof url === "string" ? url.trim() : "";
    if (!key) return null;
    if (cache.has(key)) return cache.get(key);

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

  var WGPU_PBR_VERTEX_LAYOUT = [
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 0 }] },
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 1 }] },
    { arrayStride: 8,  stepMode: "vertex", attributes: [{ format: "float32x2", offset: 0, shaderLocation: 2 }] },
    { arrayStride: 16, stepMode: "vertex", attributes: [{ format: "float32x4", offset: 0, shaderLocation: 3 }] },
  ];

  var WGPU_SHADOW_VERTEX_LAYOUT = [
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 0 }] },
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

  function wgpuCreatePBRPipeline(device, pipelineLayout, vertexModule, fragmentModule, blendMode, depthWrite, targetFormat) {
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
      primitive: { topology: "triangle-list", cullMode: "back" },
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

  function wgpuCreatePointsPipeline(device, pipelineLayout, vertexModule, fragmentModule, blendMode, depthWrite, targetFormat) {
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

  function wgpuCreateShadowMap(device, size) {
    var texture = device.createTexture({
      size: [size, size, 1],
      format: "depth24plus",
      usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
    });
    return { texture: texture, view: texture.createView(), size: size };
  }

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

    var pipelines = {};
    var postParamsLayout = null;
    var bloomCompositeLayout = null;
    var postBlitLayout = null;
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

    function ensureFBOs(width, height) {
      if (width === currentWidth && height === currentHeight && sceneTex) return;
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
        usage: GPUTextureUsage.RENDER_ATTACHMENT,
      });
      depthTexView = depthTex.createView();

      currentWidth = width;
      currentHeight = height;
    }

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

      apply: function(encoder, effects, scaledW, scaledH, canvasW, canvasH, finalView) {
        ensureFBOs(scaledW, scaledH);

        var currentTexView = sceneTexView;
        var blitPipeline = getPipeline("blit", WGSL_POST_BLIT_FRAGMENT, getPostBlitLayout());

        for (var i = 0; i < effects.length; i++) {
          var effect = effects[i];
          var isLast = (i === effects.length - 1);
          var outputView = isLast ? finalView : (currentTexView === sceneTexView ? auxTexView : sceneTexView);

          switch (effect.kind) {
            case SCENE_POST_TONE_MAPPING: {
              var pipeline = getPipeline("toneMapping", WGSL_POST_TONEMAPPING_FRAGMENT, getPostParamsLayout());
              var buf = getParamBuffer("toneMapping", 16);
              device.queue.writeBuffer(buf, 0, new Float32Array([sceneNumber(effect.exposure, 1.0), 0, 0, 0]));
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
              var bloomScale = (effect.scale > 0 && effect.scale <= 1) ? effect.scale : 0.5;
              var halfW = Math.max(1, Math.floor(scaledW * bloomScale));
              var halfH = Math.max(1, Math.floor(scaledH * bloomScale));
              ensureBloomPingPong(halfW, halfH);
              var threshold = sceneNumber(effect.threshold, 0.8);
              var radius = sceneNumber(effect.radius, 5.0);
              var intensity = sceneNumber(effect.intensity, 0.5);

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

  function createSceneWebGPURenderer(canvas) {
    if (typeof navigator === "undefined" || !navigator.gpu) return null;
    if (typeof canvas.getContext !== "function") return null;

    var probe = _externalProbe();
    if (!probe || !probe.ready || !probe.adapter || !probe.device) return null;
    var adapter = probe.adapter;
    var device = probe.device;

    var gpuCtx = canvas.getContext("webgpu");
    if (!gpuCtx) return null;

    var initFailed = false;
    var initStarted = true;
    var targetFormat = navigator.gpu.getPreferredCanvasFormat();

    var frameBindGroupLayout = null;
    var materialBindGroupLayout = null;
    var pointsBindGroupLayout = null;
    var shadowBindGroupLayout = null;
    var pbrPipelineLayout = null;
    var pointsPipelineLayout = null;

    var pbrVertexModule = null;
    var pbrFragmentModule = null;
    var shadowVertexModule = null;
    var shadowFragmentModule = null;
    var pointsVertexModule = null;
    var pointsFragmentModule = null;

    var pipelineCache = {};

    var shadowSlots = [null, null];

    var frameUniformBuffer = null;
    var lightStorageBuffer = null;
    var fogUniformBuffer = null;
    var envUniformBuffer = null;
    var shadowUniformBuffer = null;
    var materialUniformBuffer = null;
    var positionBuffer = null;
    var normalBuffer = null;
    var uvBuffer = null;
    var tangentBuffer = null;

    var pointsEntryGPUBuffers = new Set(); // all allocated GPUBuffers for dispose()
    var pointsUniformScratch = new ArrayBuffer(128);
    var pointsUniformScratchF = new Float32Array(pointsUniformScratch);
    var pointsUniformScratchU = new Uint32Array(pointsUniformScratch);
    var computeParticleSystems = new Map();
    var lastComputeParticleTimeSeconds = null;
    var lastPreparedScene = null;

    var shadowPositionBuffer = null;
    var shadowFrameBuffer = null;

    var mainDepthTexture = null;
    var mainDepthView = null;
    var mainDepthWidth = 0;
    var mainDepthHeight = 0;

    var dummyShadowTex = null;
    var dummyShadowView = null;

    var linearSampler = null;
    var comparisonSampler = null;

    var textureCache = new Map();

    var placeholderTex = null;
    var placeholderView = null;

    var postProcessor = null;

    var scratchViewMatrix = new Float32Array(16);
    var scratchProjMatrix = new Float32Array(16);
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

    function startInit() { /* no-op: device already initialized */ }

    (function initGPUResources() {
      try {
        device.lost.then(function(info) {
          console.warn("[gosx] WebGPU device lost:", info && info.message);
          device = null;
          initFailed = true;
        }).catch(function() {});

        gpuCtx.configure({
          device: device,
          format: targetFormat,
          alphaMode: "premultiplied",
        });

        frameBindGroupLayout = wgpuCreateFrameBindGroupLayout(device);
        materialBindGroupLayout = wgpuCreateMaterialBindGroupLayout(device);
        pointsBindGroupLayout = wgpuCreatePointsBindGroupLayout(device);
        shadowBindGroupLayout = wgpuCreateShadowBindGroupLayout(device);

        pbrPipelineLayout = device.createPipelineLayout({
          bindGroupLayouts: [frameBindGroupLayout, materialBindGroupLayout],
        });
        pointsPipelineLayout = device.createPipelineLayout({
          bindGroupLayouts: [frameBindGroupLayout, materialBindGroupLayout, pointsBindGroupLayout],
        });

        pbrVertexModule = device.createShaderModule({ label: "pbr-vert", code: WGSL_PBR_VERTEX });
        pbrFragmentModule = device.createShaderModule({ label: "pbr-frag", code: WGSL_PBR_FRAGMENT });
        shadowVertexModule = device.createShaderModule({ label: "shadow-vert", code: WGSL_SHADOW_VERTEX });
        shadowFragmentModule = device.createShaderModule({ label: "shadow-frag", code: WGSL_SHADOW_FRAGMENT });
        pointsVertexModule = device.createShaderModule({ label: "points-vert", code: WGSL_POINTS_VERTEX });
        pointsFragmentModule = device.createShaderModule({ label: "points-frag", code: WGSL_POINTS_FRAGMENT });

        frameUniformBuffer = device.createBuffer({ size: 256, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
        lightStorageBuffer = device.createBuffer({ size: 512, usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST });
        fogUniformBuffer = device.createBuffer({ size: 32, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
        envUniformBuffer = device.createBuffer({ size: 48, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
        shadowUniformBuffer = device.createBuffer({ size: 256, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
        materialUniformBuffer = device.createBuffer({ size: 64, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
        shadowFrameBuffer = device.createBuffer({ size: 64, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });

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

        dummyShadowTex = device.createTexture({
          size: [1, 1, 1],
          format: "depth24plus",
          usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
        });
        dummyShadowView = dummyShadowTex.createView();

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
        console.warn("[gosx] WebGPU synchronous init failed:", err);
        initFailed = true;
      }
    })();

    function ensureMainDepth(width, height) {
      if (mainDepthTexture && mainDepthWidth === width && mainDepthHeight === height) return;
      if (mainDepthTexture) mainDepthTexture.destroy();
      mainDepthTexture = device.createTexture({
        size: [width, height, 1],
        format: "depth24plus",
        usage: GPUTextureUsage.RENDER_ATTACHMENT,
      });
      mainDepthView = mainDepthTexture.createView();
      mainDepthWidth = width;
      mainDepthHeight = height;
    }

    function getPBRPipeline(blendMode, depthWrite) {
      var key = wgpuPipelineKey("pbr", blendMode, depthWrite, targetFormat, "depth24plus");
      if (pipelineCache[key]) return pipelineCache[key];
      var pipeline = wgpuCreatePBRPipeline(device, pbrPipelineLayout, pbrVertexModule, pbrFragmentModule, blendMode, depthWrite, targetFormat);
      pipelineCache[key] = pipeline;
      return pipeline;
    }

    var shadowPipeline = null;
    function getShadowPipeline() {
      if (shadowPipeline) return shadowPipeline;
      shadowPipeline = wgpuCreateShadowPipeline(device, shadowBindGroupLayout, shadowVertexModule);
      return shadowPipeline;
    }

    function getPointsPipeline(blendMode, depthWrite) {
      var key = wgpuPipelineKey("points", blendMode, depthWrite, targetFormat, "depth24plus");
      if (pipelineCache[key]) return pipelineCache[key];
      var pipeline = wgpuCreatePointsPipeline(device, pointsPipelineLayout, pointsVertexModule, pointsFragmentModule, blendMode, depthWrite, targetFormat);
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

    function uploadFrameUniforms(camera, width, height, toneMap) {
      var cam = sceneRenderCamera(camera);
      var aspect = Math.max(0.0001, width / Math.max(1, height));
      scenePBRViewMatrix(cam, scratchViewMatrix);
      scenePBRProjectionMatrix(cam.fov, aspect, cam.near, cam.far, scratchProjMatrix);

      var data = new ArrayBuffer(160);
      var f = new Float32Array(data);
      var u = new Uint32Array(data);
      f.set(scratchViewMatrix, 0);          // offset 0
      f.set(scratchProjMatrix, 16);         // offset 64
      f[32] = cam.x;                         // cameraPos.x
      f[33] = cam.y;                         // cameraPos.y
      f[34] = -cam.z;                        // cameraPos.z (negated per convention)
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

      var countBuf = new Uint32Array([count]);
      device.queue.writeBuffer(frameUniformBuffer, 140, countBuf);

      var lightData = new Float32Array(8 * 16);
      var colorCache = {};

      for (var i = 0; i < count; i++) {
        var light = lightArray[i];
        var kind = typeof light.kind === "string" ? light.kind.toLowerCase() : "";
        var lightType = 2; // point
        if (kind === "ambient") lightType = 0;
        else if (kind === "directional") lightType = 1;

        var base = i * 16;
        lightData[base + 0] = sceneNumber(light.x, 0);
        lightData[base + 1] = sceneNumber(light.y, 0);
        lightData[base + 2] = sceneNumber(light.z, 0);
        lightData[base + 3] = lightType;

        lightData[base + 4] = sceneNumber(light.directionX, 0);
        lightData[base + 5] = sceneNumber(light.directionY, -1);
        lightData[base + 6] = sceneNumber(light.directionZ, 0);
        lightData[base + 7] = sceneNumber(light.intensity, 1);

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

    function uploadMaterialUniforms(material) {
      var mat = material || {};
      var albedoRGBA = sceneColorRGBA(mat.color, [0.8, 0.8, 0.8, 1]);

      var data = new ArrayBuffer(64);
      var f = new Float32Array(data);
      var u = new Uint32Array(data);

      f[0] = albedoRGBA[0]; f[1] = albedoRGBA[1]; f[2] = albedoRGBA[2];
      f[3] = sceneNumber(mat.roughness, 0.5);
      f[4] = sceneNumber(mat.metalness, 0);
      f[5] = sceneNumber(mat.emissive, 0);
      f[6] = clamp01(sceneNumber(mat.opacity, 1));
      u[7] = mat.unlit ? 1 : 0;

      device.queue.writeBuffer(materialUniformBuffer, 0, new Float32Array(data));
    }

    function createMaterialBindGroup(material, receiveShadow) {
      var mat = material || {};
      var albedoRGBA = sceneColorRGBA(mat.color, [0.8, 0.8, 0.8, 1]);

      var data = new ArrayBuffer(64);
      var f = new Float32Array(data);
      var u = new Uint32Array(data);

      f[0] = albedoRGBA[0]; f[1] = albedoRGBA[1]; f[2] = albedoRGBA[2];
      f[3] = sceneNumber(mat.roughness, 0.5);
      f[4] = sceneNumber(mat.metalness, 0);
      f[5] = sceneNumber(mat.emissive, 0);
      f[6] = clamp01(sceneNumber(mat.opacity, 1));
      u[7] = mat.unlit ? 1 : 0;

      var textureMaps = [
        { prop: "texture",      index: 8 },
        { prop: "normalMap",    index: 9 },
        { prop: "roughnessMap", index: 10 },
        { prop: "metalnessMap", index: 11 },
        { prop: "emissiveMap",  index: 12 },
      ];

      var texViews = [];
      for (var ti = 0; ti < textureMaps.length; ti++) {
        var tm = textureMaps[ti];
        var record = mat[tm.prop] ? wgpuLoadTexture(device, mat[tm.prop], textureCache) : null;
        var loaded = Boolean(record && record.loaded);
        u[tm.index] = loaded ? 1 : 0;
        texViews.push(loaded ? record.view : placeholderView);
      }

      u[13] = receiveShadow ? 1 : 0;
      u[14] = 0;
      u[15] = 0;

      device.queue.writeBuffer(materialUniformBuffer, 0, new Float32Array(data));

      return device.createBindGroup({
        layout: materialBindGroupLayout,
        entries: [
          { binding: 0, resource: { buffer: materialUniformBuffer } },
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

    function renderShadowPass(encoder, lightMatrix, bundle, shadowResource) {
      var sp = getShadowPipeline();
      if (!sp) return;

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

      pass.setPipeline(sp);
      pass.setBindGroup(0, shadowBG);

      var objects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];
      for (var i = 0; i < objects.length; i++) {
        var obj = objects[i];
        if (!obj || obj.viewCulled) continue;
        if (!obj.castShadow) continue;
        if (!Number.isFinite(obj.vertexOffset) || !Number.isFinite(obj.vertexCount) || obj.vertexCount <= 0) continue;

        var positions = sliceToFloat32(bundle.worldMeshPositions, obj.vertexOffset, obj.vertexCount, 3, "positions");
        shadowPositionBuffer = wgpuEnsureBufferData(
          device, shadowPositionBuffer,
          GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST,
          positions
        );

        pass.setVertexBuffer(0, shadowPositionBuffer);
        pass.draw(obj.vertexCount);
      }

      pass.end();
    }

    function drawPBRObjects(pass, objectList, bundle, materials, frameBindGroup) {
      var lastMaterialIndex = -1;
      var lastReceiveShadow = null;

      for (var i = 0; i < objectList.length; i++) {
        var obj = objectList[i];
        var matIndex = sceneNumber(obj.materialIndex, 0);
        var mat = materials[matIndex] || null;
        var receiveShadow = !!obj.receiveShadow;

        if (matIndex !== lastMaterialIndex || receiveShadow !== lastReceiveShadow) {
          var matBG = createMaterialBindGroup(mat, receiveShadow);
          pass.setBindGroup(1, matBG);
          lastMaterialIndex = matIndex;
          lastReceiveShadow = receiveShadow;
        }

        var offset = obj.vertexOffset;
        var count = obj.vertexCount;

        var positions = sliceToFloat32(bundle.worldMeshPositions, offset, count, 3, "positions");
        positionBuffer = wgpuEnsureBufferData(device, positionBuffer, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, positions);
        pass.setVertexBuffer(0, positionBuffer);

        var normals = sliceToFloat32(bundle.worldMeshNormals, offset, count, 3, "normals");
        normalBuffer = wgpuEnsureBufferData(device, normalBuffer, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, normals);
        pass.setVertexBuffer(1, normalBuffer);

        if (bundle.worldMeshUVs) {
          var uvs = sliceToFloat32(bundle.worldMeshUVs, offset, count, 2, "uvs");
          uvBuffer = wgpuEnsureBufferData(device, uvBuffer, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, uvs);
        } else {
          var zeroUVs = ensureScratch("uvs", count * 2);
          for (var ui = 0; ui < count * 2; ui++) zeroUVs[ui] = 0;
          uvBuffer = wgpuEnsureBufferData(device, uvBuffer, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, zeroUVs.subarray(0, count * 2));
        }
        pass.setVertexBuffer(2, uvBuffer);

        if (bundle.worldMeshTangents) {
          var tangents = sliceToFloat32(bundle.worldMeshTangents, offset, count, 4, "tangents");
          tangentBuffer = wgpuEnsureBufferData(device, tangentBuffer, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, tangents);
        } else {
          var defTangents = ensureScratch("tangents", count * 4);
          for (var ti = 0; ti < count; ti++) {
            defTangents[ti * 4] = 1;
            defTangents[ti * 4 + 1] = 0;
            defTangents[ti * 4 + 2] = 0;
            defTangents[ti * 4 + 3] = 1;
          }
          tangentBuffer = wgpuEnsureBufferData(device, tangentBuffer, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, defTangents.subarray(0, count * 4));
        }
        pass.setVertexBuffer(3, tangentBuffer);

        pass.draw(count);
      }
    }

    function drawPointsEntries(pass, bundle, cam, timeSeconds) {
      var pointsArray = Array.isArray(bundle.points) ? bundle.points : [];
      if (pointsArray.length === 0) return;

      var env = bundle.environment || {};
      var fogDensity = sceneNumber(env.fogDensity, 0);
      var fogColorRGBA = sceneColorRGBA(env.fogColor, [0.5, 0.5, 0.5, 1]);

      var _pointsModelMat = new Float32Array(16);
      var _pointsTilt = new Float32Array(16);
      var _pointsSpin = new Float32Array(16);

      for (var i = 0; i < pointsArray.length; i++) {
        var entry = pointsArray[i];
        var count = sceneNumber(entry.count, 0);
        if (count <= 0) continue;

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

        pointsUniformScratchF.fill(0);
        var puF = pointsUniformScratchF;
        var puU = pointsUniformScratchU;

        puF.set(_pointsModelMat, 0);   // modelMatrix @ 0
        puF[16] = sceneNumber(entry.size, 1); // defaultSize @ 64
        var defaultColorRGBA = sceneColorRGBA(entry.color, [1, 1, 1, 1]);
        puF[17] = defaultColorRGBA[0]; // defaultColor @ 68
        puF[18] = defaultColorRGBA[1];
        puF[19] = defaultColorRGBA[2];
        puU[20] = 0; // hasPerVertexColor
        puU[21] = 0; // hasPerVertexSize
        puU[22] = entry.attenuation ? 1 : 0; // sizeAttenuation
        puU[23] = scenePointStyleCode(entry.style); // pointStyle
        puF[24] = clamp01(sceneNumber(entry.opacity, 1)); // opacity
        puU[25] = fogDensity > 0 ? 1 : 0; // hasFog
        puF[26] = fogDensity; // fogDensity
        puF[27] = fogColorRGBA[0]; // fogColor
        puF[28] = fogColorRGBA[1];
        puF[29] = fogColorRGBA[2];

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

        if (!entry._cachedPos) continue;

        var hasSizes = !!entry._cachedSizes;
        var hasColors = !!entry._cachedColors;
        puU[20] = hasColors ? 1 : 0;
        puU[21] = hasSizes ? 1 : 0;

        var pointsUniformBuffer = ensurePointsUniformGPUBuffer(entry, puF);

        var particleData = ensurePointsParticleData(entry, count, hasSizes, hasColors, defaultColorRGBA);
        var pointsParticleBuffer = ensurePointsParticleGPUBuffer(entry, particleData);

        var pointsBG = device.createBindGroup({
          layout: pointsBindGroupLayout,
          entries: [
            { binding: 0, resource: { buffer: pointsUniformBuffer } },
            { binding: 1, resource: { buffer: pointsParticleBuffer } },
          ],
        });

        var blendMode = typeof entry.blendMode === "string" ? entry.blendMode.toLowerCase() : "";
        var depthWrite = entry.depthWrite !== false;
        var validBlend = blendMode === "additive" || blendMode === "alpha" ? blendMode : "opaque";
        var pipeline = getPointsPipeline(validBlend, depthWrite);

        pass.setPipeline(pipeline);
        pass.setBindGroup(2, pointsBG);
        pass.draw(6, count); // 6 vertices per quad, count instances
      }
    }

    function drawComputeParticleEntries(pass, records, environment) {
      if (!Array.isArray(records) || records.length === 0) return;

      var env = environment || {};
      var fogDensity = sceneNumber(env.fogDensity, 0);
      var fogColorRGBA = sceneColorRGBA(env.fogColor, [0.5, 0.5, 0.5, 1]);

      for (var i = 0; i < records.length; i++) {
        var record = records[i];
        var system = record && record.system;
        if (!system || !system.renderBuffer || system.count <= 0) continue;
        if (typeof system.isReady === "function" && !system.isReady()) continue;

        var entry = system.entry && typeof system.entry === "object" ? system.entry : {};
        var material = entry.material && typeof entry.material === "object" ? entry.material : {};

        pointsUniformScratchF.fill(0);
        var puF = pointsUniformScratchF;
        var puU = pointsUniformScratchU;
        puF.set(pointsIdentityMatrix, 0);

        var defaultColorRGBA = sceneColorRGBA(material.color, [1, 1, 1, 1]);
        puF[16] = sceneNumber(material.size, 1);
        puF[17] = defaultColorRGBA[0];
        puF[18] = defaultColorRGBA[1];
        puF[19] = defaultColorRGBA[2];
        puU[20] = 1;
        puU[21] = 1;
        puU[22] = material.attenuation ? 1 : 0;
        puU[23] = scenePointStyleCode(material.style);
        puF[24] = 1;
        puU[25] = fogDensity > 0 ? 1 : 0;
        puF[26] = fogDensity;
        puF[27] = fogColorRGBA[0];
        puF[28] = fogColorRGBA[1];
        puF[29] = fogColorRGBA[2];

        var pointsUniformBuffer = ensurePointsUniformGPUBuffer(system, puF);

        var pointsBG = device.createBindGroup({
          layout: pointsBindGroupLayout,
          entries: [
            { binding: 0, resource: { buffer: pointsUniformBuffer } },
            { binding: 1, resource: { buffer: system.renderBuffer } },
          ],
        });

        var blendMode = typeof material.blendMode === "string" ? material.blendMode.toLowerCase() : "";
        var depthWrite = entry.depthWrite !== false;
        var validBlend = blendMode === "additive" || blendMode === "alpha" ? blendMode : "opaque";
        var pipeline = getPointsPipeline(validBlend, depthWrite);

        pass.setPipeline(pipeline);
        pass.setBindGroup(2, pointsBG);
        pass.draw(6, system.count);
      }
    }

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
      if (!hasPBRData && !hasPointsData) return;
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
      }

      var width = canvas.width;
      var height = canvas.height;
      if (width <= 0 || height <= 0) return;

      var perfEnabled = typeof window !== "undefined" && window.__gosx_scene3d_perf === true;
      if (perfEnabled) {
        performance.mark("scene3d-render-start");
      }

      gpuCtx.configure({
        device: device,
        format: targetFormat,
        alphaMode: "premultiplied",
      });

      var postEffects = Array.isArray(bundle.postEffects) ? bundle.postEffects : [];
      var usePostProcessing = postEffects.length > 0;

      var postFXMaxPixels = (typeof bundle.postFXMaxPixels === "number") ? bundle.postFXMaxPixels : 0;
      var postfxFactor = usePostProcessing
        ? resolvePostFXFactor(postFXMaxPixels, width * height)
        : 1;
      var scaledW = Math.max(1, Math.floor(width * postfxFactor));
      var scaledH = Math.max(1, Math.floor(height * postfxFactor));

      var cam = uploadFrameUniforms(bundle.camera, scaledW, scaledH, !usePostProcessing);
      uploadLights(bundle.lights);
      uploadFogUniforms(bundle.environment);
      uploadEnvUniforms(bundle.environment);

      var shadowLightMatrices = [null, null];
      var shadowLightIndices = [-1, -1];
      var activeShadowCount = 0;

      var encoder = device.createCommandEncoder({ label: "gosx-frame" });
      var frameTimeSeconds = performance.now() / 1000;
      var computeParticleRecords = updateComputeParticleSystems(bundle.computeParticles, encoder, frameTimeSeconds);

      var lightArray = Array.isArray(bundle.lights) ? bundle.lights : [];
      var sceneBounds = null;
      var shadowMaxPixels = (typeof bundle.shadowMaxPixels === "number") ? bundle.shadowMaxPixels : 0;

      for (var li = 0; li < lightArray.length && activeShadowCount < 2; li++) {
        var light = lightArray[li];
        if (!light || !light.castShadow) continue;
        var kind = typeof light.kind === "string" ? light.kind.toLowerCase() : "";
        if (kind !== "directional") continue;

        if (!sceneBounds) sceneBounds = sceneShadowComputeBounds(bundle);

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

        renderShadowPass(encoder, lightMatrix, bundle, shadowSlots[slot]);
        activeShadowCount++;
      }

      uploadShadowUniforms(shadowLightMatrices, shadowLightIndices, bundle.lights);

      var shadowView0 = shadowSlots[0] ? shadowSlots[0].view : null;
      var shadowView1 = shadowSlots[1] ? shadowSlots[1].view : null;
      var frameBindGroup = createFrameBindGroup(shadowView0, shadowView1);

      var mainColorView;
      var mainDepthTargetView;
      var postTarget = null;

      if (usePostProcessing) {
        if (!postProcessor) postProcessor = wgpuCreatePostProcessor(device, targetFormat);
        postTarget = postProcessor.getSceneTarget(scaledW, scaledH);
        mainColorView = postTarget.colorView;
        mainDepthTargetView = postTarget.depthView;
      } else {
        var currentTexture = gpuCtx.getCurrentTexture();
        mainColorView = currentTexture.createView();
        ensureMainDepth(width, height);
        mainDepthTargetView = mainDepthView;
      }

      var bgStr = typeof bundle.background === "string" ? bundle.background.trim().toLowerCase() : "";
      var bg = bgStr === "transparent" ? [0, 0, 0, 0] : sceneColorRGBA(bundle.background, [0.03, 0.08, 0.12, 1]);

      var mainPass = encoder.beginRenderPass({
        colorAttachments: [{
          view: mainColorView,
          loadOp: "clear",
          storeOp: "store",
          clearValue: { r: bg[0], g: bg[1], b: bg[2], a: bg[3] },
        }],
        depthStencilAttachment: {
          view: mainDepthTargetView,
          depthLoadOp: "clear",
          depthClearValue: 1.0,
          depthStoreOp: "store",
        },
      });

      if (hasPBRData) {
        var drawList = preparedScene && preparedScene.pbrPasses
          ? preparedScene.pbrPasses
          : buildDrawList(bundle);
        var materials = Array.isArray(bundle.materials) ? bundle.materials : [];

        if (drawList.opaque.length > 0) {
          var opaquePipeline = getPBRPipeline("opaque", true);
          mainPass.setPipeline(opaquePipeline);
          mainPass.setBindGroup(0, frameBindGroup);
          drawPBRObjects(mainPass, drawList.opaque, bundle, materials, frameBindGroup);
        }

        if (drawList.alpha.length > 0) {
          var alphaPipeline = getPBRPipeline("alpha", false);
          mainPass.setPipeline(alphaPipeline);
          mainPass.setBindGroup(0, frameBindGroup);
          drawPBRObjects(mainPass, drawList.alpha, bundle, materials, frameBindGroup);
        }

        if (drawList.additive.length > 0) {
          var additivePipeline = getPBRPipeline("additive", false);
          mainPass.setPipeline(additivePipeline);
          mainPass.setBindGroup(0, frameBindGroup);
          drawPBRObjects(mainPass, drawList.additive, bundle, materials, frameBindGroup);
        }
      }

      if (hasPointsData) {
        mainPass.setBindGroup(0, frameBindGroup);
        var dummyMatBG = createMaterialBindGroup(null, false);
        mainPass.setBindGroup(1, dummyMatBG);
        drawPointsEntries(mainPass, bundle, cam, frameTimeSeconds);
        drawComputeParticleEntries(mainPass, computeParticleRecords, bundle.environment);
      }

      mainPass.end();

      if (usePostProcessing && postProcessor) {
        var screenView = gpuCtx.getCurrentTexture().createView();
        postProcessor.apply(encoder, postEffects, scaledW, scaledH, width, height, screenView);
      }

      device.queue.submit([encoder.finish()]);

      if (perfEnabled) {
        performance.mark("scene3d-render-end");
        performance.measure("scene3d-render", "scene3d-render-start", "scene3d-render-end");
        performance.clearMarks("scene3d-render-start");
        performance.clearMarks("scene3d-render-end");
      }
    }

    function dispose() {
      if (!device) return;

      if (frameUniformBuffer) frameUniformBuffer.destroy();
      if (lightStorageBuffer) lightStorageBuffer.destroy();
      if (fogUniformBuffer) fogUniformBuffer.destroy();
      if (envUniformBuffer) envUniformBuffer.destroy();
      if (shadowUniformBuffer) shadowUniformBuffer.destroy();
      if (materialUniformBuffer) materialUniformBuffer.destroy();
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
      if (dummyShadowTex) dummyShadowTex.destroy();
      if (placeholderTex) placeholderTex.destroy();

      for (var si = 0; si < shadowSlots.length; si++) {
        if (shadowSlots[si]) shadowSlots[si].texture.destroy();
      }

      for (var record of textureCache.values()) {
        if (record && record.texture) record.texture.destroy();
      }
      textureCache.clear();

      if (postProcessor) {
        postProcessor.dispose();
        postProcessor = null;
      }

      device = null;
    }

    if (initFailed) return null;

    return {
      kind: "webgpu",
      type: "webgpu",
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

  function sceneWebGPUAvailable() {
    var probe = _externalProbe();
    return probe.ready && probe.adapter !== false && probe.adapter !== null;
  }

  var SCENE_COMPUTE_PARTICLE_SOURCE = [
    "struct Particle {",
    "    posX: f32, posY: f32, posZ: f32,",
    "    velX: f32, velY: f32, velZ: f32,",
    "    age: f32,",
    "    lifetime: f32,",
    "};",
    "",
    "struct RenderVertex {",
    "    posX: f32, posY: f32, posZ: f32,",
    "    size: f32,",
    "    r: f32, g: f32, b: f32, a: f32,",
    "};",
    "",
    "struct SimParams {",
    "    deltaTime: f32,",
    "    totalTime: f32,",
    "    count: u32,",
    "    _pad0: u32,",
    "    emitterKind: u32,",
    "    emitterX: f32, emitterY: f32, emitterZ: f32,",
    "    emitterRadius: f32,",
    "    emitterRate: f32,",
    "    emitterLifetime: f32,",
    "    _pad1: u32,",
    "    emitterArms: u32,",
    "    emitterWind: f32,",
    "    emitterScatter: f32,",
    "    emitterRotX: f32, emitterRotY: f32, emitterRotZ: f32,",
    "    _pad2: u32,",
    "    sizeStart: f32, sizeEnd: f32,",
    "    colorStartR: f32, colorStartG: f32, colorStartB: f32,",
    "    colorEndR: f32, colorEndG: f32, colorEndB: f32,",
    "    opacityStart: f32, opacityEnd: f32,",
    "    forceCount: u32,",
    "    _pad3: u32,",
    "};",
    "",
    "struct Force {",
    "    kind: u32,",
    "    strength: f32,",
    "    dirX: f32, dirY: f32, dirZ: f32,",
    "    frequency: f32,",
    "    _pad0: f32, _pad1: f32,",
    "};",
    "",
    "@group(0) @binding(0) var<storage, read_write> particles: array<Particle>;",
    "@group(0) @binding(1) var<storage, read_write> renderData: array<RenderVertex>;",
    "@group(0) @binding(2) var<uniform> params: SimParams;",
    "@group(0) @binding(3) var<storage, read> forces: array<Force>;",
    "",
    "fn hash(seed: u32) -> f32 {",
    "    var s = seed;",
    "    s = s ^ (s >> 16u);",
    "    s = s * 0x45d9f3bu;",
    "    s = s ^ (s >> 16u);",
    "    s = s * 0x45d9f3bu;",
    "    s = s ^ (s >> 16u);",
    "    return f32(s) / f32(0xffffffffu);",
    "}",
    "",
    "fn hash2(a: u32, b: u32) -> f32 {",
    "    return hash(a * 1597334677u + b * 3812015801u);",
    "}",
    "",
    "fn emitPoint(index: u32, p: SimParams) -> Particle {",
    "    var out: Particle;",
    "    out.posX = p.emitterX;",
    "    out.posY = p.emitterY;",
    "    out.posZ = p.emitterZ;",
    "    out.velX = (hash2(index, 0u) - 0.5) * 0.1;",
    "    out.velY = (hash2(index, 1u) - 0.5) * 0.1;",
    "    out.velZ = (hash2(index, 2u) - 0.5) * 0.1;",
    "    out.age = 0.0;",
    "    out.lifetime = p.emitterLifetime;",
    "    return out;",
    "}",
    "",
    "fn emitSphere(index: u32, p: SimParams) -> Particle {",
    "    var out: Particle;",
    "    let theta = hash2(index, 10u) * 6.283185;",
    "    let phi = acos(2.0 * hash2(index, 11u) - 1.0);",
    "    let r = p.emitterRadius * pow(hash2(index, 12u), 0.333);",
    "    out.posX = p.emitterX + r * sin(phi) * cos(theta);",
    "    out.posY = p.emitterY + r * cos(phi);",
    "    out.posZ = p.emitterZ + r * sin(phi) * sin(theta);",
    "    out.velX = 0.0; out.velY = 0.0; out.velZ = 0.0;",
    "    out.age = 0.0;",
    "    out.lifetime = p.emitterLifetime;",
    "    return out;",
    "}",
    "",
    "fn emitDisc(index: u32, p: SimParams) -> Particle {",
    "    var out: Particle;",
    "    let angle = hash2(index, 20u) * 6.283185;",
    "    let r = p.emitterRadius * sqrt(hash2(index, 21u));",
    "    out.posX = p.emitterX + r * cos(angle);",
    "    out.posY = p.emitterY;",
    "    out.posZ = p.emitterZ + r * sin(angle);",
    "    out.velX = 0.0; out.velY = 0.0; out.velZ = 0.0;",
    "    out.age = 0.0;",
    "    out.lifetime = p.emitterLifetime;",
    "    return out;",
    "}",
    "",
    "fn rotateEulerZYX(lx: f32, ly: f32, lz: f32, rx: f32, ry: f32, rz: f32) -> vec3<f32> {",
    "    let cx = cos(rx); let sx = sin(rx);",
    "    let cy = cos(ry); let sy = sin(ry);",
    "    let cz = cos(rz); let sz = sin(rz);",
    "    return vec3<f32>(",
    "        lx*(cy*cz) + ly*(sx*sy*cz - cx*sz) + lz*(cx*sy*cz + sx*sz),",
    "        lx*(cy*sz) + ly*(sx*sy*sz + cx*cz) + lz*(cx*sy*sz - sx*cz),",
    "        lx*(-sy)   + ly*(sx*cy)             + lz*(cx*cy)",
    "    );",
    "}",
    "",
    "fn emitSpiral(index: u32, p: SimParams) -> Particle {",
    "    var out: Particle;",
    "    let radius = hash2(index, 30u) * p.emitterRadius;",
    "    let arm = index % p.emitterArms;",
    "    let armAngle = f32(arm) * 3.14159265 / f32(max(p.emitterArms / 2u, 1u));",
    "    let spiralAngle = armAngle + (radius / p.emitterRadius) * p.emitterWind;",
    "    let scatter = (hash2(index, 31u) - 0.5) * radius * p.emitterScatter;",
    "    let lx = cos(spiralAngle) * radius + scatter;",
    "    let ly = (hash2(index, 32u) - 0.5) * p.emitterRadius * 0.05;",
    "    let lz = sin(spiralAngle) * radius + (hash2(index, 33u) - 0.5) * radius * p.emitterScatter;",
    "    let rotated = rotateEulerZYX(lx, ly, lz, p.emitterRotX, p.emitterRotY, p.emitterRotZ);",
    "    out.posX = p.emitterX + rotated.x;",
    "    out.posY = p.emitterY + rotated.y;",
    "    out.posZ = p.emitterZ + rotated.z;",
    "    out.velX = 0.0; out.velY = 0.0; out.velZ = 0.0;",
    "    out.age = 0.0;",
    "    out.lifetime = p.emitterLifetime;",
    "    return out;",
    "}",
    "",
    "fn emitParticle(index: u32, p: SimParams) -> Particle {",
    "    switch (p.emitterKind) {",
    "        case 1u: { return emitSphere(index, p); }",
    "        case 2u: { return emitDisc(index, p); }",
    "        case 3u: { return emitSpiral(index, p); }",
    "        default: { return emitPoint(index, p); }",
    "    }",
    "}",
    "",
    "fn applyGravity(part: Particle, f: Force, dt: f32) -> vec3f {",
    "    return vec3f(f.dirX, f.dirY, f.dirZ) * f.strength * dt;",
    "}",
    "",
    "fn applyWind(part: Particle, f: Force, dt: f32) -> vec3f {",
    "    return vec3f(f.dirX, f.dirY, f.dirZ) * f.strength * dt;",
    "}",
    "",
    "fn applyTurbulence(part: Particle, f: Force, dt: f32, time: f32) -> vec3f {",
    "    let freq = f.frequency;",
    "    let nx = sin(part.posX * freq + time * 1.3) * cos(part.posZ * freq + time * 0.7);",
    "    let ny = sin(part.posY * freq + time * 0.9) * cos(part.posX * freq + time * 1.1);",
    "    let nz = sin(part.posZ * freq + time * 1.7) * cos(part.posY * freq + time * 0.5);",
    "    return vec3f(nx, ny, nz) * f.strength * dt;",
    "}",
    "",
    "fn applyOrbit(part: Particle, f: Force, dt: f32) -> vec3f {",
    "    let dx = part.posX;",
    "    let dz = part.posZ;",
    "    let dist = max(sqrt(dx * dx + dz * dz), 0.001);",
    "    return vec3f(-dz / dist, 0.0, dx / dist) * f.strength * dt;",
    "}",
    "",
    "fn applyDrag(part: Particle, f: Force, dt: f32) -> vec3f {",
    "    return vec3f(-part.velX, -part.velY, -part.velZ) * f.strength * dt;",
    "}",
    "",
    "@compute @workgroup_size(64)",
    "fn simulate(@builtin(global_invocation_id) id: vec3u) {",
    "    let i = id.x;",
    "    if (i >= params.count) { return; }",
    "",
    "    var p = particles[i];",
    "",
    "    if (p.age < 0.0) {",
    "        p = emitParticle(i, params);",
    "    }",
    "",
    "    p.age += params.deltaTime;",
    "",
    "    if (p.lifetime > 0.0 && p.age >= p.lifetime) {",
    "        p = emitParticle(i, params);",
    "    }",
    "",
    "    for (var fi = 0u; fi < params.forceCount; fi++) {",
    "        let f = forces[fi];",
    "        switch (f.kind) {",
    "            case 0u: { let dv = applyGravity(p, f, params.deltaTime); p.velX += dv.x; p.velY += dv.y; p.velZ += dv.z; }",
    "            case 1u: { let dv = applyWind(p, f, params.deltaTime); p.velX += dv.x; p.velY += dv.y; p.velZ += dv.z; }",
    "            case 2u: { let dv = applyTurbulence(p, f, params.deltaTime, params.totalTime); p.velX += dv.x; p.velY += dv.y; p.velZ += dv.z; }",
    "            case 3u: { let dv = applyOrbit(p, f, params.deltaTime); p.velX += dv.x; p.velY += dv.y; p.velZ += dv.z; }",
    "            case 4u: { let dv = applyDrag(p, f, params.deltaTime); p.velX += dv.x; p.velY += dv.y; p.velZ += dv.z; }",
    "            default: {}",
    "        }",
    "    }",
    "",
    "    p.posX += p.velX * params.deltaTime;",
    "    p.posY += p.velY * params.deltaTime;",
    "    p.posZ += p.velZ * params.deltaTime;",
    "",
    "    particles[i] = p;",
    "",
    "    let t = select(p.age / p.lifetime, 0.0, p.lifetime <= 0.0);",
    "    var rv: RenderVertex;",
    "    rv.posX = p.posX;",
    "    rv.posY = p.posY;",
    "    rv.posZ = p.posZ;",
    "    rv.size = mix(params.sizeStart, params.sizeEnd, t);",
    "    rv.r = mix(params.colorStartR, params.colorEndR, t);",
    "    rv.g = mix(params.colorStartG, params.colorEndG, t);",
    "    rv.b = mix(params.colorStartB, params.colorEndB, t);",
    "    rv.a = mix(params.opacityStart, params.opacityEnd, t);",
    "    renderData[i] = rv;",
    "}",
  ].join("\n");

  function sceneComputeUploadSimParams(device, buffer, entry, deltaTime, totalTime) {
    var emitter = entry.emitter || {};
    var material = entry.material || {};
    var forces = entry.forces || [];

    var kindMap = { point: 0, sphere: 1, disc: 2, spiral: 3 };
    var emitterKind = kindMap[emitter.kind] || 0;

    var colorStart = sceneColorRGBA(material.color || "#ffffff", [1, 1, 1, 1]);
    var colorEnd = sceneColorRGBA(material.colorEnd || material.color || "#ffffff", [1, 1, 1, 1]);

    var buf = new ArrayBuffer(256);
    var view = new DataView(buf);
    var offset = 0;

    view.setFloat32(offset, deltaTime, true); offset += 4;            // deltaTime
    view.setFloat32(offset, totalTime, true); offset += 4;            // totalTime
    view.setUint32(offset, entry.count, true); offset += 4;           // count
    view.setUint32(offset, 0, true); offset += 4;                     // _pad0

    view.setUint32(offset, emitterKind, true); offset += 4;           // emitterKind
    view.setFloat32(offset, sceneNumber(emitter.x, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.y, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.z, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.radius, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.rate, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.lifetime, 0), true); offset += 4;
    view.setUint32(offset, 0, true); offset += 4;                     // _pad1

    view.setUint32(offset, sceneNumber(emitter.arms, 2), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.wind, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.scatter, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.rotationX, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.rotationY, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.rotationZ, 0), true); offset += 4;
    view.setUint32(offset, 0, true); offset += 4;                     // _pad2

    view.setFloat32(offset, sceneNumber(material.size, 1), true); offset += 4;
    view.setFloat32(offset, sceneNumber(material.sizeEnd, material.size || 1), true); offset += 4;
    view.setFloat32(offset, colorStart[0], true); offset += 4;
    view.setFloat32(offset, colorStart[1], true); offset += 4;
    view.setFloat32(offset, colorStart[2], true); offset += 4;
    view.setFloat32(offset, colorEnd[0], true); offset += 4;
    view.setFloat32(offset, colorEnd[1], true); offset += 4;
    view.setFloat32(offset, colorEnd[2], true); offset += 4;
    view.setFloat32(offset, sceneNumber(material.opacity, 1), true); offset += 4;
    view.setFloat32(offset, sceneNumber(material.opacityEnd, material.opacity || 1), true); offset += 4;

    view.setUint32(offset, forces.length, true); offset += 4;
    view.setUint32(offset, 0, true); offset += 4;                     // _pad3

    device.queue.writeBuffer(buffer, 0, buf, 0, 256);
  }

  function sceneComputeUploadForces(device, buffer, forces) {
    var kindMap = { gravity: 0, wind: 1, turbulence: 2, orbit: 3, drag: 4 };
    var byteLen = Math.max(32, forces.length * 32);
    var buf = new ArrayBuffer(byteLen);
    var view = new DataView(buf);

    for (var i = 0; i < forces.length; i++) {
      var f = forces[i];
      var off = i * 32;
      view.setUint32(off, kindMap[f.kind] || 0, true);
      view.setFloat32(off + 4, sceneNumber(f.strength, 0), true);
      view.setFloat32(off + 8, sceneNumber(f.x, 0), true);
      view.setFloat32(off + 12, sceneNumber(f.y, 0), true);
      view.setFloat32(off + 16, sceneNumber(f.z, 0), true);
      view.setFloat32(off + 20, sceneNumber(f.frequency, 1), true);
    }

    device.queue.writeBuffer(buffer, 0, buf);
  }

  function createSceneComputeParticleSystem(device, entry) {
    var count = entry.count || 0;
    if (count <= 0) return null;

    var particleBuffer = device.createBuffer({
      size: count * 32,
      usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST,
    });

    var renderBuffer = device.createBuffer({
      size: count * 32,
      usage: GPUBufferUsage.STORAGE | GPUBufferUsage.VERTEX,
    });

    var paramsBuffer = device.createBuffer({
      size: 256,
      usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
    });

    var forceCount = (entry.forces || []).length;
    var forceBuffer = device.createBuffer({
      size: Math.max(32, forceCount * 32),
      usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST,
    });

    var initData = new Float32Array(count * 8);
    for (var i = 0; i < count; i++) {
      initData[i * 8 + 6] = -1.0;
    }
    device.queue.writeBuffer(particleBuffer, 0, initData);

    sceneComputeUploadForces(device, forceBuffer, entry.forces || []);

    var computePipeline = null;
    var bindGroup = null;
    var ready = false;

    var shaderModule = device.createShaderModule({
      code: SCENE_COMPUTE_PARTICLE_SOURCE,
    });

    var bindGroupLayout = device.createBindGroupLayout({
      entries: [
        { binding: 0, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } },
        { binding: 1, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } },
        { binding: 2, visibility: GPUShaderStage.COMPUTE, buffer: { type: "uniform" } },
        { binding: 3, visibility: GPUShaderStage.COMPUTE, buffer: { type: "read-only-storage" } },
      ],
    });

    var pipelineLayout = device.createPipelineLayout({
      bindGroupLayouts: [bindGroupLayout],
    });

    device.createComputePipelineAsync({
      layout: pipelineLayout,
      compute: {
        module: shaderModule,
        entryPoint: "simulate",
      },
    }).then(function(pipeline) {
      computePipeline = pipeline;
      bindGroup = device.createBindGroup({
        layout: bindGroupLayout,
        entries: [
          { binding: 0, resource: { buffer: particleBuffer } },
          { binding: 1, resource: { buffer: renderBuffer } },
          { binding: 2, resource: { buffer: paramsBuffer } },
          { binding: 3, resource: { buffer: forceBuffer } },
        ],
      });
      ready = true;
    }).catch(function(err) {
      console.warn("[gosx] Compute particle pipeline creation failed:", err);
    });

    return {
      count: count,
      renderBuffer: renderBuffer,
      entry: entry,
      isReady: function() {
        return ready;
      },

      update: function(device, encoder, deltaTime, totalTime) {
        if (!ready) return;

        sceneComputeUploadSimParams(device, paramsBuffer, entry, deltaTime, totalTime);

        var pass = encoder.beginComputePass();
        pass.setPipeline(computePipeline);
        pass.setBindGroup(0, bindGroup);
        pass.dispatchWorkgroups(Math.ceil(count / 64));
        pass.end();
      },

      dispose: function() {
        particleBuffer.destroy();
        renderBuffer.destroy();
        paramsBuffer.destroy();
        forceBuffer.destroy();
        computePipeline = null;
        bindGroup = null;
        ready = false;
      },
    };
  }

  function createSceneCPUParticleSystem(entry) {
    var count = Math.min(entry.count || 0, 10000);
    if (count <= 0) return null;

    var particles = new Float32Array(count * 8);

    var positions = new Float32Array(count * 3);
    var sizes = new Float32Array(count);
    var colors = new Float32Array(count * 3);
    var opacities = new Float32Array(count);

    for (var i = 0; i < count; i++) {
      particles[i * 8 + 6] = -1.0;
    }

    function hash(seed) {
      var s = seed >>> 0;
      s = s ^ (s >>> 16);
      s = Math.imul(s, 0x45d9f3b) >>> 0;
      s = s ^ (s >>> 16);
      s = Math.imul(s, 0x45d9f3b) >>> 0;
      s = s ^ (s >>> 16);
      return (s >>> 0) / 0xffffffff;
    }

    function hash2(a, b) {
      return hash(Math.imul(a >>> 0, 1597334677) + Math.imul(b >>> 0, 3812015801));
    }

    var kindMap = { point: 0, sphere: 1, disc: 2, spiral: 3 };

    var forceKindMap = { gravity: 0, wind: 1, turbulence: 2, orbit: 3, drag: 4 };

    function currentEmitterConfig() {
      var emitter = entry && entry.emitter && typeof entry.emitter === "object" ? entry.emitter : {};
      return {
        kind: kindMap[emitter.kind] || 0,
        x: 0,
        y: 0,
        z: 0,
        radius: sceneNumber(emitter.radius, 0),
        lifetime: sceneNumber(emitter.lifetime, 0),
        arms: Math.max(1, Math.floor(sceneNumber(emitter.arms, 2))),
        wind: sceneNumber(emitter.wind, 0),
        scatter: sceneNumber(emitter.scatter, 0),
      };
    }

    function currentMaterialConfig() {
      var material = entry && entry.material && typeof entry.material === "object" ? entry.material : {};
      return {
        colorStart: sceneColorRGBA(material.color || "#ffffff", [1, 1, 1, 1]),
        colorEnd: sceneColorRGBA(material.colorEnd || material.color || "#ffffff", [1, 1, 1, 1]),
        sizeStart: sceneNumber(material.size, 1),
        sizeEnd: sceneNumber(material.sizeEnd, material.size || 1),
        opacityStart: sceneNumber(material.opacity, 1),
        opacityEnd: sceneNumber(material.opacityEnd, material.opacity || 1),
      };
    }

    function currentForces() {
      return Array.isArray(entry && entry.forces) ? entry.forces : [];
    }

    function emitParticle(index, base, emitterConfig) {
      switch (emitterConfig.kind) {
        case 1: { // sphere
          var theta = hash2(index, 10) * 6.283185;
          var phi = Math.acos(2.0 * hash2(index, 11) - 1.0);
          var r = emitterConfig.radius * Math.pow(hash2(index, 12), 0.333);
          base[0] = r * Math.sin(phi) * Math.cos(theta);
          base[1] = r * Math.cos(phi);
          base[2] = r * Math.sin(phi) * Math.sin(theta);
          base[3] = 0; base[4] = 0; base[5] = 0;
          break;
        }
        case 2: { // disc
          var angle = hash2(index, 20) * 6.283185;
          var dr = emitterConfig.radius * Math.sqrt(hash2(index, 21));
          base[0] = dr * Math.cos(angle);
          base[1] = 0;
          base[2] = dr * Math.sin(angle);
          base[3] = 0; base[4] = 0; base[5] = 0;
          break;
        }
        case 3: { // spiral (local space)
          var radius = hash2(index, 30) * emitterConfig.radius;
          var arm = index % emitterConfig.arms;
          var armAngle = arm * 3.14159265 / Math.max(emitterConfig.arms / 2, 1);
          var spiralAngle = armAngle + (radius / Math.max(emitterConfig.radius, 0.001)) * emitterConfig.wind;
          var scatter = (hash2(index, 31) - 0.5) * radius * emitterConfig.scatter;
          base[0] = Math.cos(spiralAngle) * radius + scatter;
          base[1] = (hash2(index, 32) - 0.5) * emitterConfig.radius * 0.05;
          base[2] = Math.sin(spiralAngle) * radius + (hash2(index, 33) - 0.5) * radius * emitterConfig.scatter;
          base[3] = 0; base[4] = 0; base[5] = 0;
          break;
        }
        default: { // point
          base[0] = 0;
          base[1] = 0;
          base[2] = 0;
          base[3] = (hash2(index, 0) - 0.5) * 0.1;
          base[4] = (hash2(index, 1) - 0.5) * 0.1;
          base[5] = (hash2(index, 2) - 0.5) * 0.1;
          break;
        }
      }
      base[6] = 0.0;              // age
      base[7] = emitterConfig.lifetime;   // lifetime
    }

    return {
      count: count,
      positions: positions,
      sizes: sizes,
      colors: colors,
      opacities: opacities,
      entry: entry,

      update: function(deltaTime, totalTime) {
        var emitterConfig = currentEmitterConfig();
        var materialConfig = currentMaterialConfig();
        var forces = currentForces();
        for (var i = 0; i < count; i++) {
          var base = i * 8;

          var posX = particles[base];
          var posY = particles[base + 1];
          var posZ = particles[base + 2];
          var velX = particles[base + 3];
          var velY = particles[base + 4];
          var velZ = particles[base + 5];
          var age = particles[base + 6];
          var lifetime = particles[base + 7];

          if (age < 0) {
            emitParticle(i, particles.subarray(base, base + 8), emitterConfig);
            posX = particles[base];
            posY = particles[base + 1];
            posZ = particles[base + 2];
            velX = particles[base + 3];
            velY = particles[base + 4];
            velZ = particles[base + 5];
            age = particles[base + 6];
            lifetime = particles[base + 7];
          }

          age += deltaTime;

          if (lifetime > 0 && age >= lifetime) {
            emitParticle(i, particles.subarray(base, base + 8), emitterConfig);
            posX = particles[base];
            posY = particles[base + 1];
            posZ = particles[base + 2];
            velX = particles[base + 3];
            velY = particles[base + 4];
            velZ = particles[base + 5];
            age = particles[base + 6];
            lifetime = particles[base + 7];
          }

          for (var fi = 0; fi < forces.length; fi++) {
            var f = forces[fi];
            var fKind = forceKindMap[f.kind] || 0;
            var str = sceneNumber(f.strength, 0);
            var fx = sceneNumber(f.x, 0);
            var fy = sceneNumber(f.y, 0);
            var fz = sceneNumber(f.z, 0);
            var freq = sceneNumber(f.frequency, 1);

            switch (fKind) {
              case 0: { // gravity
                velX += fx * str * deltaTime;
                velY += fy * str * deltaTime;
                velZ += fz * str * deltaTime;
                break;
              }
              case 1: { // wind
                velX += fx * str * deltaTime;
                velY += fy * str * deltaTime;
                velZ += fz * str * deltaTime;
                break;
              }
              case 2: { // turbulence
                var nx = Math.sin(posX * freq + totalTime * 1.3) * Math.cos(posZ * freq + totalTime * 0.7);
                var ny = Math.sin(posY * freq + totalTime * 0.9) * Math.cos(posX * freq + totalTime * 1.1);
                var nz = Math.sin(posZ * freq + totalTime * 1.7) * Math.cos(posY * freq + totalTime * 0.5);
                velX += nx * str * deltaTime;
                velY += ny * str * deltaTime;
                velZ += nz * str * deltaTime;
                break;
              }
              case 3: { // orbit
                var dx = posX;
                var dz = posZ;
                var dist = Math.max(Math.sqrt(dx * dx + dz * dz), 0.001);
                velX += (-dz / dist) * str * deltaTime;
                velZ += (dx / dist) * str * deltaTime;
                break;
              }
              case 4: { // drag
                velX += -velX * str * deltaTime;
                velY += -velY * str * deltaTime;
                velZ += -velZ * str * deltaTime;
                break;
              }
            }
          }

          posX += velX * deltaTime;
          posY += velY * deltaTime;
          posZ += velZ * deltaTime;

          particles[base] = posX;
          particles[base + 1] = posY;
          particles[base + 2] = posZ;
          particles[base + 3] = velX;
          particles[base + 4] = velY;
          particles[base + 5] = velZ;
          particles[base + 6] = age;
          particles[base + 7] = lifetime;

          var t = lifetime > 0 ? age / lifetime : 0;

          positions[i * 3] = posX;
          positions[i * 3 + 1] = posY;
          positions[i * 3 + 2] = posZ;
          sizes[i] = materialConfig.sizeStart + (materialConfig.sizeEnd - materialConfig.sizeStart) * t;
          colors[i * 3] = materialConfig.colorStart[0] + (materialConfig.colorEnd[0] - materialConfig.colorStart[0]) * t;
          colors[i * 3 + 1] = materialConfig.colorStart[1] + (materialConfig.colorEnd[1] - materialConfig.colorStart[1]) * t;
          colors[i * 3 + 2] = materialConfig.colorStart[2] + (materialConfig.colorEnd[2] - materialConfig.colorStart[2]) * t;
          opacities[i] = materialConfig.opacityStart + (materialConfig.opacityEnd - materialConfig.opacityStart) * t;
        }
      },

      dispose: function() {
        particles = null;
        positions = null;
        sizes = null;
        colors = null;
        opacities = null;
      },
    };
  }

  function createSceneParticleSystem(device, entry) {
    if (device) {
      return createSceneComputeParticleSystem(device, entry);
    }
    return createSceneCPUParticleSystem(entry);
  }

  function sceneComputeSystemSignature(entry) {
    return JSON.stringify({
      count: Math.max(0, Math.floor(sceneNumber(entry && entry.count, 0))),
      forces: Array.isArray(entry && entry.forces) ? entry.forces : [],
    });
  }

  window.__gosx_scene3d_webgpu_api = {
    createRenderer: createSceneWebGPURenderer,
    available: sceneWebGPUAvailable,
    commandSequence: sceneWebGPUCommandSequence,
  };
  window.__gosx_scene3d_api.sceneWebGPUCommandSequence = sceneWebGPUCommandSequence;

  window.__gosx_scene3d_webgpu_loaded = true;

})();
//# sourceMappingURL=bootstrap-feature-scene3d-webgpu.js.map
