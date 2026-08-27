  // webgpu.ts — WebGPU rendering backend for GoSX Scene3D.
  // @ts-check
  //
  // Parallel implementation of the PBR WebGL2 renderer (16-scene-webgl.js)
  // using the WebGPU API. Provides Cook-Torrance BRDF with per-pixel
  // lighting, shadow maps, fog, and post-processing. Points are rendered
  // as instanced camera-facing quads since WebGPU has no gl_PointSize.

  /**
   * @typedef {object} GoSXSceneWebGPURenderer
   * @property {(bundle: object, frame?: number) => void} render
   * @property {() => void} dispose
   */

  // renderTruth resolves the shared render-truth helpers published by
  // 15a-scene-postfx-shared.ts. This chunk (bootstrap-feature-scene3d-webgpu.js)
  // is a SEPARATE <script> whose IIFE does not concatenate 15a, so the only
  // link is the global. Resolved per call rather than cached at chunk-load
  // time because the main scene3d bundle and this chunk race on slow networks.
  // The no-op fallback keeps the renderer working when 15a is absent (an old
  // cached main bundle) instead of throwing mid-frame.
  var WEBGPU_RENDER_TRUTH_NOOP = {
    enabled: function() { return false; },
    chain: function() { return []; },
    mark: function() {},
    publish: function() {},
    record: function() {},
    latch: function() {},
    captureShaderInfo: function() {},
    implementation: function() { return "unknown"; },
    PIPELINE_MISSING: "missing",
    PIPELINE_PENDING: "pending",
    PIPELINE_FAILED: "failed",
    PIPELINE_OK: "ok",
  };

  function renderTruth() {
    if (typeof window !== "undefined" && window && window.__gosx_scene3d_render_truth_api) {
      return window.__gosx_scene3d_render_truth_api;
    }
    return WEBGPU_RENDER_TRUTH_NOOP;
  }

  function sceneWebGPUSRGBChannelToLinear(value) {
    var channel = Math.max(0, Math.min(1, sceneNumber(value, 0)));
    return channel <= 0.04045
      ? channel / 12.92
      : Math.pow((channel + 0.055) / 1.055, 2.4);
  }

  // -----------------------------------------------------------------------
  // WGSL Shader Sources
  // -----------------------------------------------------------------------

  // -- Shared constants embedded in multiple shaders --
  //
  // The light loop no longer carries a compile-time cap. lights is a
  // runtime-sized storage array, so the fragment shader bounds itself with
  // arrayLength(&lights). See sceneWebGPULightCapacityFor for the JS side.
  var WGSL_COMMON_CONSTANTS = [
    "const PI: f32 = 3.14159265359;",
  ].join("\n");
  var SCENE_WEBGPU_IBL_BRDF_MODEL = "ggx-split-sum/smith-schlick-k=alpha-over-2/schlick-fresnel";

  // -- Frame-level uniform structures --
  //
  // Light packs all seven GoSX light kinds. The type codes 0..4 match the
  // WebGL2 renderer (0=ambient, 1=directional, 2=point, 3=spot,
  // 4=hemisphere), so both backends read the same numbers. Code 5 is
  // rect-area, which only this renderer shades as a rectangle. A LightProbe
  // arrives as code 0, because a probe is a first-order ambient term with no
  // position and no distance falloff.
  //
  // Keep this layout in step with sceneWebGPUPackLights and with
  // SCENE_WEBGPU_LIGHT_FLOATS. The struct is 7 * vec4f = 112 bytes.
  var WGSL_FRAME_STRUCTS = [
    "struct Light {",
    "    position: vec4f,",       // xyz = position, w = type code
    "    direction: vec4f,",      // xyz = direction, w = intensity
    "    color: vec4f,",          // rgb = color (sky color for hemisphere), a = range
    "    params: vec4f,",         // x = decay, y = shadowBias, z = castShadow, w = spot cone angle
    "    groundPenumbra: vec4f,", // rgb = hemisphere ground color, a = spot penumbra
    "    areaHalfWidth: vec4f,",  // xyz = rect-area half-width vector, w = unused
    "    areaHalfHeight: vec4f,", // xyz = rect-area half-height vector, w = unused
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
    "    envIntensity: f32,",
    "    envRotation: f32,",
    "    hasIBL: u32,",
    "    radianceMipLevels: u32,",
    "    hasEnvMap: u32,",
    "    _pad1: u32,",
    "    _pad2: u32,",
    "    _pad3: u32,",
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
    "    hasOcclusionMap: u32,",
    "    modelMatrix: mat4x4f,",
    "    modelScaleSigns: vec4f,",
    // Trailing dedicated material scalars: the authored dielectric F0 and the
    // effective specular factors (min(IOR F0 * colour, 1) * intensity as an
    // aligned vec3f, then the intensity as F90). Every texture flag slot and
    // the model transform/sign floats keep their existing offsets; the vec3f
    // alignment pads the struct to 192 bytes total.
    "    dielectricF0: f32,",
    "    specularF0: vec3f,",
    "    specularF90: f32,",
    "};",
  ].join("\n");

  // -----------------------------------------------------------------------
  // PBR Vertex Shader (WGSL)
  // -----------------------------------------------------------------------

  var WGSL_PBR_VERTEX = [
    WGSL_FRAME_STRUCTS,
    WGSL_MATERIAL_STRUCT,
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
    "@group(1) @binding(0) var<uniform> material: MaterialUniforms;",
    "",
    "@vertex fn vertexMain(in: VertexInput) -> VertexOutput {",
    "    var out: VertexOutput;",
    "    let worldPosition = material.modelMatrix * vec4f(in.position, 1.0);",
    "    let modelBasis = mat3x3f(",
    "        normalize(material.modelMatrix[0].xyz) * material.modelScaleSigns.x,",
    "        normalize(material.modelMatrix[1].xyz) * material.modelScaleSigns.y,",
    "        normalize(material.modelMatrix[2].xyz) * material.modelScaleSigns.z",
    "    );",
    "    out.worldPos = worldPosition.xyz;",
    "    out.normal = normalize(modelBasis * in.normal);",
    "    out.uv = in.uv;",
    "    let T = normalize(modelBasis * in.tangent.xyz);",
    "    let N = out.normal;",
    "    out.tangent = T;",
    "    out.bitangent = cross(N, T) * in.tangent.w;",
    "    out.instanceColor = vec4f(1.0, 1.0, 1.0, 1.0);",
    "    out.clipPos = frame.projMatrix * frame.viewMatrix * worldPosition;",
    "    return out;",
    "}",
  ].join("\n");

  // Emitted by m31labs.dev/elio/emit/wgsl from stdlib.Skin().
  // The runtime pads dispatch buffers to the 64-wide workgroup size, so the
  // generated kernel can stay byte-for-byte aligned with Elio's current output.
  //
  // One dispatch writes three contiguous regions of a single tracked output
  // buffer, each paddedCount elements wide: positions (vec3) first, then
  // normals (vec3), then tangents (vec4). Each invocation blends the four
  // bone influences into ONE weighted mat4x4 blend: its translation
  // column skins the position while the linear xyz columns skin the packed
  // source normal and tangent, safe-normalized so degenerate vectors pass
  // through as zero instead of NaN. Tangent w is carried through untouched so
  // bitangent handedness survives skinning. paddedCount comes from
  // arrayLength(&verts) to place the normal/tangent region bases.
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
    "  nx : f32,",
    "  ny : f32,",
    "  nz : f32,",
    "  tx : f32,",
    "  ty : f32,",
    "  tz : f32,",
    "  tw : f32,",
    "};",
    "",
    "@group(0) @binding(0) var<storage, read> bones : array<mat4x4<f32>>;",
    "@group(0) @binding(1) var<storage, read> verts : array<SkinVertex>;",
    "@group(0) @binding(2) var<storage, read_write> out : array<f32>;",
    "",
    "@compute @workgroup_size(64)",
    "fn skin(@builtin(global_invocation_id) gid : vec3<u32>) {",
    "  let i = gid.x;",
    "  let paddedCount = arrayLength(&verts);",
    "  let v = verts[i];",
    "  let m = (bones[v.b0] * v.w0 + bones[v.b1] * v.w1) + (bones[v.b2] * v.w2 + bones[v.b3] * v.w3);",
    "  let skinned = (m * vec4f(v.px, v.py, v.pz, 1.0)).xyz;",
    "  let rawNormal = (m * vec4f(v.nx, v.ny, v.nz, 0.0)).xyz;",
    "  let rawTangent = (m * vec4f(v.tx, v.ty, v.tz, 0.0)).xyz;",
    "  let nLen = length(rawNormal);",
    "  let sn = select(rawNormal, rawNormal / nLen, nLen > 0.000001);",
    "  let tLen = length(rawTangent);",
    "  let st = select(rawTangent, rawTangent / tLen, tLen > 0.000001);",
    "  let posBase = i * 3u;",
    "  out[posBase] = skinned.x;",
    "  out[posBase + 1u] = skinned.y;",
    "  out[posBase + 2u] = skinned.z;",
    "  let normBase = (paddedCount * 3u) + posBase;",
    "  out[normBase] = sn.x;",
    "  out[normBase + 1u] = sn.y;",
    "  out[normBase + 2u] = sn.z;",
    "  let tanBase = (paddedCount * 6u) + (i * 4u);",
    "  out[tanBase] = st.x;",
    "  out[tanBase + 1u] = st.y;",
    "  out[tanBase + 2u] = st.z;",
    "  out[tanBase + 3u] = v.tw;",
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

  var SCENE_WATER_COMPUTE_SOURCE = [
    WGSL_COMMON_CONSTANTS,
    "",
    "struct WaterUniforms {",
    "  resolution: u32,",
    "  cellCount: u32,",
    "  seedDrops: u32,",
    "  frameIndex: u32,",
    "  deltaTime: f32,",
    "  time: f32,",
    "  waveSpeed: f32,",
    "  damping: f32,",
    "  dropRadius: f32,",
    "  dropStrength: f32,",
    "  normalScale: f32,",
    "  poolWidth: f32,",
    "  poolHeight: f32,",
    "  poolLength: f32,",
    "  cornerRadius: f32,",
    "  poolShape: f32,",
    "  lightDir: vec4f,",
    "  shallowColor: vec4f,",
    "  deepColor: vec4f,",
    "  objectCenter: vec4f,",
    "  objectPreviousCenter: vec4f,",
    "  objectHalfSizeRadius: vec4f,",
    "  objectParams: vec4f,",
    "  opticsFlags: vec4f,",
    "  interactiveDrop: vec4f,",
    "  seedSalt: f32,",
    "};",
    "",
    "struct WaterDisplacementSphere {",
    "  offsetRadius: vec4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> params: WaterUniforms;",
    "@group(0) @binding(1) var<storage, read> inState: array<vec4f>;",
    "@group(0) @binding(2) var<storage, read_write> outState: array<vec4f>;",
    "@group(0) @binding(3) var<storage, read> objectSpheres: array<WaterDisplacementSphere>;",
    "",
    "fn waterIndex(x: u32, y: u32) -> u32 {",
    "  return y * params.resolution + x;",
    "}",
    "",
    "fn hash01(n: f32) -> f32 {",
    "  return fract(sin(n) * 43758.5453123);",
    "}",
    "",
    "fn waterCoord(i: u32) -> vec2f {",
    "  let res = params.resolution;",
    "  let x = i % res;",
    "  let y = i / res;",
    "  return (vec2f(f32(x), f32(y)) + vec2f(0.5)) / max(vec2f(f32(res)), vec2f(1.0));",
    "}",
    "",
    "fn volumeInSphere(coord: vec2f, center: vec3f, radius: f32, displacementScale: f32) -> f32 {",
    "  let safeRadius = max(radius, 0.0001);",
    "  let toCenter = vec3f(coord.x * 2.0 - 1.0, 0.0, coord.y * 2.0 - 1.0) - center;",
    "  let t = length(toCenter) / safeRadius;",
    "  let dy = exp(-pow(t * 1.5, 6.0));",
    "  let ymin = min(0.0, center.y - dy);",
    "  let ymax = min(max(0.0, center.y + dy), ymin + 2.0 * dy);",
    "  return (ymax - ymin) * 0.1 * displacementScale;",
    "}",
    "",
    "fn volumeInCube(coord: vec2f, center: vec3f, halfSize: vec3f, displacementScale: f32) -> f32 {",
    "  let safeHalfSize = max(halfSize, vec3f(0.0001));",
    "  let point = vec3f(coord.x * 2.0 - 1.0, 0.0, coord.y * 2.0 - 1.0);",
    "  let distanceToBox = abs(point - center) - safeHalfSize;",
    "  let signedDistance = length(max(distanceToBox, vec3f(0.0))) + min(max(distanceToBox.x, max(distanceToBox.y, distanceToBox.z)), 0.0);",
    "  let scale = max(max(safeHalfSize.x, safeHalfSize.y), safeHalfSize.z);",
    "  let t = max(signedDistance, 0.0) / scale;",
    "  let dy = exp(-pow(t * 1.5, 6.0));",
    "  let ymin = min(0.0, center.y - dy);",
    "  let ymax = min(max(0.0, center.y + dy), ymin + 2.0 * dy);",
    "  return (ymax - ymin) * 0.1 * displacementScale;",
    "}",
    "",
    "@compute @workgroup_size(64)",
    "fn seedDrops(@builtin(global_invocation_id) gid: vec3u) {",
    "  let i = gid.x;",
    "  if (i >= params.cellCount) { return; }",
    "  let res = params.resolution;",
    "  let x = i % res;",
    "  let y = i / res;",
    "  let uv = (vec2f(f32(x), f32(y)) + vec2f(0.5)) / max(vec2f(f32(res)), vec2f(1.0));",
    "  var info = inState[i];",
    "  let count = min(params.seedDrops, 64u);",
    "  let seedSalt = params.seedSalt;",
    "  for (var j = 0u; j < count; j = j + 1u) {",
    "    let jf = f32(j + 1u);",
    "    let center = vec2f(hash01(jf * 12.9898 + seedSalt + 0.173), hash01(jf * 78.233 + seedSalt * 1.371 + 0.719));",
    "    let radius = max(params.dropRadius, 0.0001);",
    "    var drop = max(0.0, 1.0 - length(center - uv) / radius);",
    "    drop = 0.5 - cos(drop * PI) * 0.5;",
    "    let polarity = select(1.0, -1.0, (j & 1u) == 0u);",
    "    info.x = info.x + drop * params.dropStrength * polarity;",
    "  }",
    "  outState[i] = info;",
    "}",
    "",
    "@compute @workgroup_size(64)",
    "fn addDrop(@builtin(global_invocation_id) gid: vec3u) {",
    "  let i = gid.x;",
    "  if (i >= params.cellCount) { return; }",
    "  let uv = waterCoord(i);",
    "  var info = inState[i];",
    "  let center = params.interactiveDrop.xy * 0.5 + vec2f(0.5);",
    "  let radius = max(params.interactiveDrop.z, 0.0001);",
    "  var drop = max(0.0, 1.0 - length(center - uv) / radius);",
    "  drop = 0.5 - cos(drop * PI) * 0.5;",
    "  info.x = info.x + drop * params.interactiveDrop.w;",
    "  outState[i] = info;",
    "}",
    "",
    "@compute @workgroup_size(64)",
    "fn displaceObject(@builtin(global_invocation_id) gid: vec3u) {",
    "  let i = gid.x;",
    "  if (i >= params.cellCount) { return; }",
    "  var info = inState[i];",
    "  let kind = params.objectParams.x;",
    "  let displacementScale = max(params.objectParams.y, 0.0);",
    "  if (kind < 0.5 || displacementScale <= 0.0) {",
    "    outState[i] = info;",
    "    return;",
    "  }",
    "  let coord = waterCoord(i);",
    "  let previous = params.objectPreviousCenter.xyz;",
    "  let current = params.objectCenter.xyz;",
    "  if (kind < 1.5) {",
    "    let radius = params.objectHalfSizeRadius.w;",
    "    info.x = info.x + volumeInSphere(coord, previous, radius, displacementScale);",
    "    info.x = info.x - volumeInSphere(coord, current, radius, displacementScale);",
    "  } else if (kind < 2.5) {",
    "    let halfSize = params.objectHalfSizeRadius.xyz;",
    "    info.x = info.x + volumeInCube(coord, previous, halfSize, displacementScale);",
    "    info.x = info.x - volumeInCube(coord, current, halfSize, displacementScale);",
    "  } else {",
    "    let sphereCount = min(u32(params.objectParams.z), 32u);",
    "    for (var sphereIndex = 0u; sphereIndex < sphereCount; sphereIndex = sphereIndex + 1u) {",
    "      let sphere = objectSpheres[sphereIndex].offsetRadius;",
    "      let offset = sphere.xyz;",
    "      let radius = max(sphere.w, 0.0001);",
    "      info.x = info.x + volumeInSphere(coord, previous + offset, radius, displacementScale);",
    "      info.x = info.x - volumeInSphere(coord, current + offset, radius, displacementScale);",
    "    }",
    "  }",
    "  outState[i] = info;",
    "}",
    "",
    "@compute @workgroup_size(64)",
    "fn stepSimulation(@builtin(global_invocation_id) gid: vec3u) {",
    "  let i = gid.x;",
    "  if (i >= params.cellCount) { return; }",
    "  let res = params.resolution;",
    "  let x = i % res;",
    "  let y = i / res;",
    "  var westX = 0u;",
    "  if (x > 0u) { westX = x - 1u; }",
    "  let eastX = min(x + 1u, res - 1u);",
    "  var southY = 0u;",
    "  if (y > 0u) { southY = y - 1u; }",
    "  let northY = min(y + 1u, res - 1u);",
    "  var info = inState[i];",
    "  let average = (",
    "    inState[waterIndex(westX, y)].x +",
    "    inState[waterIndex(eastX, y)].x +",
    "    inState[waterIndex(x, southY)].x +",
    "    inState[waterIndex(x, northY)].x",
    "  ) * 0.25;",
    "  info.y = (info.y + (average - info.x) * 2.0 * params.waveSpeed) * params.damping;",
    "  info.x = info.x + info.y;",
    "  outState[i] = info;",
    "}",
    "",
    "@compute @workgroup_size(64)",
    "fn updateNormals(@builtin(global_invocation_id) gid: vec3u) {",
    "  let i = gid.x;",
    "  if (i >= params.cellCount) { return; }",
    "  let res = params.resolution;",
    "  let x = i % res;",
    "  let y = i / res;",
    "  let eastX = min(x + 1u, res - 1u);",
    "  let northY = min(y + 1u, res - 1u);",
    "  var info = inState[i];",
    "  let deltaX = 2.0 * max(params.poolWidth, 0.0001) / max(f32(res), 1.0);",
    "  let deltaZ = 2.0 * max(params.poolLength, 0.0001) / max(f32(res), 1.0);",
    "  let dx = vec3f(deltaX, inState[waterIndex(eastX, y)].x - info.x, 0.0);",
    "  let dz = vec3f(0.0, inState[waterIndex(x, northY)].x - info.x, deltaZ);",
    "  let normal = normalize(cross(dz, dx));",
    "  info.z = normal.x;",
    "  info.w = normal.z;",
    "  outState[i] = info;",
    "}",
  ].join("\n");

  var SCENE_WATER_RENDER_VERTEX_SOURCE = [
    WGSL_FRAME_STRUCTS,
    "",
    "struct WaterUniforms {",
    "  resolution: u32,",
    "  cellCount: u32,",
    "  seedDrops: u32,",
    "  frameIndex: u32,",
    "  deltaTime: f32,",
    "  time: f32,",
    "  waveSpeed: f32,",
    "  damping: f32,",
    "  dropRadius: f32,",
    "  dropStrength: f32,",
    "  normalScale: f32,",
    "  poolWidth: f32,",
    "  poolHeight: f32,",
    "  poolLength: f32,",
    "  cornerRadius: f32,",
    "  poolShape: f32,",
    "  lightDir: vec4f,",
    "  shallowColor: vec4f,",
    "  deepColor: vec4f,",
    "  objectCenter: vec4f,",
    "  objectPreviousCenter: vec4f,",
    "  objectHalfSizeRadius: vec4f,",
    "  objectParams: vec4f,",
    "  opticsFlags: vec4f,",
    "  interactiveDrop: vec4f,",
    "  seedSalt: f32,",
    "};",
    "",
    "struct VertexOutput {",
    "  @builtin(position) clipPos: vec4f,",
    "  @location(0) worldPos: vec3f,",
    "  @location(1) normal: vec3f,",
    "  @location(2) uv: vec2f,",
    "  @location(3) height: f32,",
    "};",
    "",
    "struct WaterObjectTextureMatrices {",
    "  viewProjectionMatrix: mat4x4f,",
    "  reflectionViewProjectionMatrix: mat4x4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "@group(1) @binding(0) var<uniform> params: WaterUniforms;",
    "@group(1) @binding(1) var<storage, read> state: array<vec4f>;",
    "",
    "fn waterIndex(x: u32, y: u32) -> u32 {",
    "  return y * params.resolution + x;",
    "}",
    "",
    // The surface grid is drawn INDEXED. It used to be a non-indexed triangle list:
    // six vertices per quad, so every interior grid point was transformed ~6 times and
    // re-read `state[]` from the storage buffer ~6 times. At the shipped 192x192 grid
    // that is 218,886 vertex invocations per pass, twice per frame (above + below
    // surface) — and on Apple/Metal it dominated the frame: measured 200ms of GPU work
    // for a scene occupying 0.1 megapixels, while the same scene minus the water cost
    // 1.3ms. Immediate-mode desktop GPUs brute-force the duplication and show nothing.
    //
    // With an index buffer, vertex_index IS the grid vertex id (resolution^2 of them,
    // 36,864 at the default), the post-transform cache does its job, and the geometry
    // produced is identical.
    "@vertex fn vertexMain(@builtin(vertex_index) vertexIndex: u32) -> VertexOutput {",
    "  let cellsPerSide = max(params.resolution - 1u, 1u);",
    "  let quad = vertexIndex / 6u;",
    "  let corner = vertexIndex % 6u;",
    "  let cellX = quad % cellsPerSide;",
    "  let cellY = quad / cellsPerSide;",
    "  var ox = 0u;",
    "  var oy = 0u;",
    "  if (corner == 1u || corner == 2u || corner == 4u) { ox = 1u; }",
    "  if (corner == 2u || corner == 4u || corner == 5u) { oy = 1u; }",
    "  let gx = min(cellX + ox, params.resolution - 1u);",
    "  let gy = min(cellY + oy, params.resolution - 1u);",
    "  let uv = vec2f(f32(gx), f32(gy)) / max(vec2f(f32(params.resolution - 1u)), vec2f(1.0));",
    "  let info = state[waterIndex(gx, gy)];",
    "  let nx = info.z * params.normalScale;",
    "  let nz = info.w * params.normalScale;",
    "  let ny = sqrt(max(0.0, 1.0 - info.z * info.z - info.w * info.w));",
    "  var out: VertexOutput;",
    "  out.height = info.x;",
    "  out.uv = uv;",
    "  out.worldPos = vec3f((uv.x - 0.5) * params.poolWidth * 2.0, info.x * params.poolHeight, (uv.y - 0.5) * params.poolLength * 2.0);",
    "  out.normal = normalize(vec3f(nx, ny, nz));",
    "  out.clipPos = frame.projMatrix * frame.viewMatrix * vec4f(out.worldPos, 1.0);",
    "  return out;",
    "}",
  ].join("\n");

  var SCENE_WATER_RENDER_FRAGMENT_SOURCE = [
    WGSL_FRAME_STRUCTS,
    "",
    "const WATER_SURFACE_VIEW_BELOW: bool = false;",
    "",
    "struct WaterUniforms {",
    "  resolution: u32,",
    "  cellCount: u32,",
    "  seedDrops: u32,",
    "  frameIndex: u32,",
    "  deltaTime: f32,",
    "  time: f32,",
    "  waveSpeed: f32,",
    "  damping: f32,",
    "  dropRadius: f32,",
    "  dropStrength: f32,",
    "  normalScale: f32,",
    "  poolWidth: f32,",
    "  poolHeight: f32,",
    "  poolLength: f32,",
    "  cornerRadius: f32,",
    "  poolShape: f32,",
    "  lightDir: vec4f,",
    "  shallowColor: vec4f,",
    "  deepColor: vec4f,",
    "  objectCenter: vec4f,",
    "  objectPreviousCenter: vec4f,",
    "  objectHalfSizeRadius: vec4f,",
    "  objectParams: vec4f,",
    "  opticsFlags: vec4f,",
    "  interactiveDrop: vec4f,",
    "  seedSalt: f32,",
    "};",
    "",
    "struct VertexOutput {",
    "  @builtin(position) clipPos: vec4f,",
    "  @location(0) worldPos: vec3f,",
    "  @location(1) normal: vec3f,",
    "  @location(2) uv: vec2f,",
    "  @location(3) height: f32,",
    "};",
    "",
    "struct WaterObjectTextureMatrices {",
    "  viewProjectionMatrix: mat4x4f,",
    "  reflectionViewProjectionMatrix: mat4x4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "@group(1) @binding(0) var<uniform> params: WaterUniforms;",
    "@group(1) @binding(2) var causticSampler: sampler;",
    "@group(1) @binding(3) var causticTexture: texture_2d<f32>;",
    "@group(1) @binding(4) var objectReflectionTexture: texture_2d<f32>;",
    "@group(1) @binding(5) var objectClippedReflectionTexture: texture_2d<f32>;",
    "@group(1) @binding(6) var objectRefractionTexture: texture_2d<f32>;",
    "@group(1) @binding(7) var waterSkyTexture: texture_cube<f32>;",
    "@group(1) @binding(8) var<uniform> objectTextureMatrices: WaterObjectTextureMatrices;",
    "",
    "fn roundedPoolSDF(point: vec2f, halfSize: vec2f, radius: f32) -> f32 {",
    "  let r = clamp(radius, 0.0, max(0.0, min(halfSize.x, halfSize.y) - 0.001));",
    "  let q = abs(point) - max(halfSize - vec2f(r), vec2f(0.001));",
    "  return length(max(q, vec2f(0.0))) + min(max(q.x, q.y), 0.0) - r;",
    "}",
    "fn roundedWaterSDF(point: vec2f, halfSize: vec2f, radius: f32) -> f32 {",
    "  let inset = max(min(halfSize.x, halfSize.y) * 0.018, 0.012);",
    "  return roundedPoolSDF(point, max(halfSize - vec2f(inset), vec2f(0.001)), max(radius - inset, 0.0));",
    "}",
    "",
    "fn sampleWaterSky(direction: vec3f) -> vec3f {",
    "  let sky = textureSample(waterSkyTexture, causticSampler, normalize(direction)).rgb;",
    "  let horizon = clamp(direction.y * 0.5 + 0.5, 0.0, 1.0);",
    "  let fallback = mix(params.deepColor.rgb * 0.55, params.shallowColor.rgb * 1.12, horizon);",
    "  return mix(fallback, sky, 0.82);",
    "}",
    "",
    "fn sampleProjectedTexture(tex: texture_2d<f32>, matrix: mat4x4f, worldPos: vec3f) -> vec4f {",
    "  let clip = matrix * vec4f(worldPos, 1.0);",
    "  let safeW = select(0.0001, clip.w, abs(clip.w) > 0.0001);",
    "  let ndc = clip.xyz / safeW;",
    "  let uv = clamp(ndc.xy * vec2f(0.5, -0.5) + vec2f(0.5), vec2f(0.0), vec2f(1.0));",
    "  let inBounds = step(0.0, uv.x) * step(0.0, uv.y) * step(uv.x, 1.0) * step(uv.y, 1.0) * step(0.0, clip.w);",
    "  return textureSampleLevel(tex, causticSampler, uv, 0.0) * inBounds;",
    "}",
    "",
    "fn intersectSurfaceSphereBounds(origin: vec3f, ray: vec3f, center: vec3f, radius: f32) -> f32 {",
    "  let toSphere = origin - center;",
    "  let a = dot(ray, ray);",
    "  let b = 2.0 * dot(toSphere, ray);",
    "  let c = dot(toSphere, toSphere) - radius * radius;",
    "  let discriminant = b * b - 4.0 * a * c;",
    "  if (discriminant > 0.0 && a > 0.0000001) {",
    "    let root = sqrt(discriminant);",
    "    let near = (-b - root) / (2.0 * a);",
    "    let far = (-b + root) / (2.0 * a);",
    "    if (near > 0.0) { return near; }",
    "    if (far > 0.0) { return 0.0; }",
    "  }",
    "  return 1000000.0;",
    "}",
    "",
    "fn surfaceObjectCenterWorld() -> vec3f {",
    "  return vec3f(params.objectCenter.x * params.poolWidth, params.objectCenter.y, params.objectCenter.z * params.poolLength);",
    "}",
    "",
    "fn surfaceObjectHalfSizeWorld() -> vec3f {",
    "  return vec3f(params.objectHalfSizeRadius.x * params.poolWidth, params.objectHalfSizeRadius.y, params.objectHalfSizeRadius.z * params.poolLength);",
    "}",
    "",
    "fn surfaceObjectRadiusWorld() -> f32 {",
    "  return max(params.objectHalfSizeRadius.w * params.poolLength, 0.001);",
    "}",
    "",
    "fn objectTextureRadiusWorld() -> f32 {",
    "  if (params.objectParams.x < 2.5) {",
    "    let halfSize = surfaceObjectHalfSizeWorld();",
    "    return max(max(max(halfSize.x, halfSize.y), halfSize.z), surfaceObjectRadiusWorld());",
    "  }",
    "  return max(surfaceObjectRadiusWorld(), 0.31);",
    "}",
    "",
    "fn sampleObjectRefraction(origin: vec3f, ray: vec3f) -> vec4f {",
    "  if (params.objectParams.x < 0.5 || params.opticsFlags.w <= 0.0) { return vec4f(0.0); }",
    "  let hit = intersectSurfaceSphereBounds(origin, ray, surfaceObjectCenterWorld(), objectTextureRadiusWorld());",
    "  if (hit >= 1000000.0) { return vec4f(0.0); }",
    "  return sampleProjectedTexture(objectRefractionTexture, objectTextureMatrices.viewProjectionMatrix, origin + ray * hit);",
    "}",
    "",
    "fn sampleObjectReflection(origin: vec3f, ray: vec3f) -> vec4f {",
    "  if (params.objectParams.x < 0.5 || params.opticsFlags.w <= 0.0) { return vec4f(0.0); }",
    "  let hit = intersectSurfaceSphereBounds(origin, ray, surfaceObjectCenterWorld(), objectTextureRadiusWorld());",
    "  if (hit >= 1000000.0) { return vec4f(0.0); }",
    "  return sampleProjectedTexture(objectReflectionTexture, objectTextureMatrices.reflectionViewProjectionMatrix, origin + ray * hit);",
    "}",
    "",
    "@fragment fn fragmentMain(in: VertexOutput) -> @location(0) vec4f {",
    "  var shapeAlpha = 1.0;",
    "  if (params.poolShape > 0.5) {",
    "    let halfSize = vec2f(max(params.poolWidth, 0.001), max(params.poolLength, 0.001));",
    "    let sdf = roundedWaterSDF(in.worldPos.xz, halfSize, params.cornerRadius);",
    "    let edge = max(0.008, min(params.poolWidth, params.poolLength) / max(f32(params.resolution), 1.0));",
    "    shapeAlpha = smoothstep(edge, -edge, sdf);",
    "    if (shapeAlpha <= 0.001) { discard; }",
    "  }",
    "  var n = normalize(in.normal);",
    "  if (WATER_SURFACE_VIEW_BELOW) { n = -n; }",
    "  let viewDir = normalize(frame.cameraPos - in.worldPos);",
    "  let reflectDir = reflect(-viewDir, n);",
    "  let refractEta = select(1.0 / 1.333, 1.333 / 1.0, WATER_SURFACE_VIEW_BELOW);",
    "  let refractDir = refract(-viewDir, n, refractEta);",
    "  let fresnelBase = select(0.25, 0.50, WATER_SURFACE_VIEW_BELOW);",
    "  let fresnel = mix(fresnelBase, 1.0, pow(1.0 - clamp(dot(n, viewDir), 0.0, 1.0), 3.0));",
    "  let causticsEnabled = clamp(params.opticsFlags.x, 0.0, 1.0);",
    "  let reflectionEnabled = clamp(params.opticsFlags.y, 0.0, 1.0);",
    "  let refractionEnabled = clamp(params.opticsFlags.z, 0.0, 1.0);",
    "  var causticTexel = vec3f(0.0);",
    "  if (causticsEnabled > 0.0) {",
    "    causticTexel = textureSample(causticTexture, causticSampler, clamp(in.uv, vec2f(0.0), vec2f(1.0))).rgb;",
    "  }",
    "  var reflectionTexel = vec4f(0.0);",
    "  var clippedReflectionTexel = vec4f(0.0);",
    "  var refractionTexel = vec4f(0.0);",
    "  if (reflectionEnabled > 0.0) {",
    "    reflectionTexel = sampleObjectReflection(in.worldPos, reflectDir);",
    "    clippedReflectionTexel = sampleProjectedTexture(objectClippedReflectionTexture, objectTextureMatrices.reflectionViewProjectionMatrix, in.worldPos);",
    "  }",
    "  if (refractionEnabled > 0.0) {",
    "    refractionTexel = sampleObjectRefraction(in.worldPos, refractDir);",
    "  }",
    "  let causticHint = causticTexel.r * causticsEnabled;",
    "  let depthMix = clamp(0.38 + in.height * 8.0 + in.uv.y * 0.18, 0.0, 1.0);",
    "  var reflectedColor = sampleWaterSky(reflectDir);",
    "  var refractedColor = mix(params.deepColor.rgb, params.shallowColor.rgb, depthMix);",
    "  if (WATER_SURFACE_VIEW_BELOW) {",
    "    reflectedColor = reflectedColor * vec3f(0.4, 0.9, 1.0);",
    "    refractedColor = refractedColor * vec3f(0.8, 1.0, 1.1) + vec3f(0.10, 0.38, 0.46) * causticHint * 0.10;",
    "  } else {",
    "    refractedColor = refractedColor * vec3f(0.25, 1.0, 1.25) + vec3f(0.18, 0.28, 0.22) * causticHint * 0.08;",
    "  }",
    "  if (params.objectParams.x >= 2.5 && params.opticsFlags.w > 0.0) {",
    "    if (WATER_SURFACE_VIEW_BELOW) {",
    "      if (params.objectParams.w > 0.5 && params.objectParams.w < 1.5) {",
    "        let refractedObject = max(refractionTexel, refractionTexel * vec4f(0.78, 1.0, 1.08, 1.0));",
    "        refractedColor = mix(refractedColor, refractedObject.rgb, refractedObject.a * refractionEnabled);",
    "        reflectedColor = mix(reflectedColor, reflectionTexel.rgb, reflectionTexel.a * reflectionEnabled);",
    "      } else {",
    "        refractedColor = mix(refractedColor, refractionTexel.rgb * vec3f(0.78, 1.0, 1.08), refractionTexel.a * refractionEnabled);",
    "        reflectedColor = mix(reflectedColor, reflectionTexel.rgb, reflectionTexel.a * reflectionEnabled);",
    "      }",
    "    } else if (params.objectParams.w > 0.5 && params.objectParams.w < 1.5) {",
    "      refractedColor = mix(refractedColor, refractionTexel.rgb, refractionTexel.a * refractionEnabled);",
    "      reflectedColor = mix(reflectedColor, reflectionTexel.rgb, reflectionTexel.a * reflectionEnabled);",
    "    } else {",
    "      refractedColor = mix(refractedColor, refractionTexel.rgb, refractionTexel.a * refractionEnabled);",
    "      reflectedColor = mix(reflectedColor, clippedReflectionTexel.rgb, clippedReflectionTexel.a * reflectionEnabled);",
    "    }",
    "  }",
    "  if (WATER_SURFACE_VIEW_BELOW) {",
    "    return vec4f(mix(reflectedColor, refractedColor, (1.0 - fresnel) * length(refractDir)), shapeAlpha);",
    "  }",
    "  return vec4f(mix(refractedColor, reflectedColor, fresnel), shapeAlpha);",
    "}",
  ].join("\n");

  var SCENE_WATER_RENDER_BELOW_FRAGMENT_SOURCE = SCENE_WATER_RENDER_FRAGMENT_SOURCE.replace(
    "const WATER_SURFACE_VIEW_BELOW: bool = false;",
    "const WATER_SURFACE_VIEW_BELOW: bool = true;"
  );

  var SCENE_WATER_POOL_VERTEX_SOURCE = [
    WGSL_FRAME_STRUCTS,
    "",
    "struct WaterUniforms {",
    "  resolution: u32,",
    "  cellCount: u32,",
    "  seedDrops: u32,",
    "  frameIndex: u32,",
    "  deltaTime: f32,",
    "  time: f32,",
    "  waveSpeed: f32,",
    "  damping: f32,",
    "  dropRadius: f32,",
    "  dropStrength: f32,",
    "  normalScale: f32,",
    "  poolWidth: f32,",
    "  poolHeight: f32,",
    "  poolLength: f32,",
    "  cornerRadius: f32,",
    "  poolShape: f32,",
    "  lightDir: vec4f,",
    "  shallowColor: vec4f,",
    "  deepColor: vec4f,",
    "  objectCenter: vec4f,",
    "  objectPreviousCenter: vec4f,",
    "  objectHalfSizeRadius: vec4f,",
    "  objectParams: vec4f,",
    "  opticsFlags: vec4f,",
    "  interactiveDrop: vec4f,",
    "  seedSalt: f32,",
    "};",
    "",
    "struct VertexOutput {",
    "  @builtin(position) clipPos: vec4f,",
    "  @location(0) worldPos: vec3f,",
    "  @location(1) normal: vec3f,",
    "  @location(2) tileUV: vec2f,",
    "  @location(3) waterUV: vec2f,",
    "  @location(4) face: f32,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "@group(1) @binding(0) var<uniform> params: WaterUniforms;",
    "",
    "const WATER_POOL_ROUNDED_SEGMENTS: u32 = 44u;",
    "const WATER_POOL_ROUNDED_CORNER_SAMPLES: u32 = 11u;",
    "const WATER_POOL_ROUNDED_CORNER_STEPS: f32 = 10.0;",
    "const WATER_POOL_ROUNDED_FLOOR_VERTICES: u32 = WATER_POOL_ROUNDED_SEGMENTS * 3u;",
    "const WATER_POOL_HALF_PI: f32 = 1.57079632679;",
    "",
    "fn waterPoolCornerSign(corner: u32) -> vec2f {",
    "  var signValue = vec2f(1.0, 1.0);",
    "  if (corner == 1u || corner == 2u) { signValue.x = -1.0; }",
    "  if (corner >= 2u) { signValue.y = -1.0; }",
    "  return signValue;",
    "}",
    "",
    "fn waterPoolRoundedBoundaryPoint(index: u32, halfWidth: f32, halfLength: f32, radius: f32) -> vec2f {",
    "  let wrapped = index % WATER_POOL_ROUNDED_SEGMENTS;",
    "  let corner = min(wrapped / WATER_POOL_ROUNDED_CORNER_SAMPLES, 3u);",
    "  let local = wrapped % WATER_POOL_ROUNDED_CORNER_SAMPLES;",
    "  let signValue = waterPoolCornerSign(corner);",
    "  let inset = max(vec2f(halfWidth, halfLength) - vec2f(radius), vec2f(0.001));",
    "  let theta = f32(corner) * WATER_POOL_HALF_PI + f32(local) / WATER_POOL_ROUNDED_CORNER_STEPS * WATER_POOL_HALF_PI;",
    "  return signValue * inset + vec2f(cos(theta), sin(theta)) * radius;",
    "}",
    "",
    "fn waterPoolRoundedBoundaryNormal(point: vec2f, halfWidth: f32, halfLength: f32, radius: f32) -> vec2f {",
    "  let inset = max(vec2f(halfWidth, halfLength) - vec2f(radius), vec2f(0.001));",
    "  let absPoint = abs(point);",
    "  var outward = vec2f(0.0, 1.0);",
    "  if (absPoint.x > inset.x && absPoint.y > inset.y && radius > 0.0001) {",
    "    outward = normalize(point - sign(point) * inset);",
    "  } else if (absPoint.x / max(halfWidth, 0.001) > absPoint.y / max(halfLength, 0.001)) {",
    "    outward = vec2f(sign(point.x), 0.0);",
    "  } else {",
    "    outward = vec2f(0.0, sign(point.y));",
    "  }",
    "  return -outward;",
    "}",
    "",
    "fn waterPoolQuadUV(corner: u32) -> vec2f {",
    "  var uv = vec2f(0.0);",
    "  if (corner == 1u || corner == 2u || corner == 4u) { uv.x = 1.0; }",
    "  if (corner == 2u || corner == 4u || corner == 5u) { uv.y = 1.0; }",
    "  return uv;",
    "}",
    "",
    "fn waterPoolRoundedVertex(vertexIndex: u32, halfWidth: f32, halfLength: f32, floorY: f32, rimY: f32, radius: f32) -> VertexOutput {",
    "  var worldPos = vec3f(0.0);",
    "  var normal = vec3f(0.0, 1.0, 0.0);",
    "  var tileUV = vec2f(0.0);",
    "  var face = 0.0;",
    "  if (vertexIndex < WATER_POOL_ROUNDED_FLOOR_VERTICES) {",
    "    let tri = vertexIndex / 3u;",
    "    let corner = vertexIndex % 3u;",
    "    var point = vec2f(0.0);",
    "    if (corner == 1u) {",
    "      point = waterPoolRoundedBoundaryPoint((tri + 1u) % WATER_POOL_ROUNDED_SEGMENTS, halfWidth, halfLength, radius);",
    "    } else if (corner == 2u) {",
    "      point = waterPoolRoundedBoundaryPoint(tri, halfWidth, halfLength, radius);",
    "    }",
    "    worldPos = vec3f(point.x, floorY, point.y);",
    "    tileUV = point * 0.42;",
    "  } else {",
    "    let localIndex = vertexIndex - WATER_POOL_ROUNDED_FLOOR_VERTICES;",
    "    let segment = (localIndex / 6u) % WATER_POOL_ROUNDED_SEGMENTS;",
    "    let corner = localIndex % 6u;",
    "    let quadUV = waterPoolQuadUV(corner);",
    "    let pointA = waterPoolRoundedBoundaryPoint(segment, halfWidth, halfLength, radius);",
    "    let pointB = waterPoolRoundedBoundaryPoint((segment + 1u) % WATER_POOL_ROUNDED_SEGMENTS, halfWidth, halfLength, radius);",
    "    let point = mix(pointA, pointB, quadUV.x);",
    "    let inward = waterPoolRoundedBoundaryNormal(point, halfWidth, halfLength, radius);",
    "    worldPos = vec3f(point.x, mix(floorY, rimY, quadUV.y), point.y);",
    "    normal = vec3f(inward.x, 0.0, inward.y);",
    "    tileUV = vec2f((f32(segment) + quadUV.x) * 0.18, worldPos.y * 0.72);",
    "    face = 5.0;",
    "  }",
    "  var out: VertexOutput;",
    "  out.worldPos = worldPos;",
    "  out.normal = normal;",
    "  out.tileUV = tileUV;",
    "  out.waterUV = worldPos.xz / max(vec2f(params.poolWidth * 2.0, params.poolLength * 2.0), vec2f(0.001)) + vec2f(0.5);",
    "  out.face = face;",
    "  out.clipPos = frame.projMatrix * frame.viewMatrix * vec4f(worldPos, 1.0);",
    "  return out;",
    "}",
    "",
    "@vertex fn vertexMain(@builtin(vertex_index) vertexIndex: u32) -> VertexOutput {",
    "  let halfWidth = max(params.poolWidth, 0.001);",
    "  let halfLength = max(params.poolLength, 0.001);",
    "  let floorY = -max(params.poolHeight, 0.001);",
    "  let rimY = max(params.poolHeight * (2.0 / 12.0), 0.025);",
    "  let maxCornerRadius = max(0.0, min(halfWidth, halfLength) - 0.001);",
    "  let cornerRadius = clamp(params.cornerRadius, 0.0, maxCornerRadius);",
    "  if (params.poolShape > 0.5 && cornerRadius > 0.0001) {",
    "    return waterPoolRoundedVertex(vertexIndex, halfWidth, halfLength, floorY, rimY, cornerRadius);",
    "  }",
    "  let face = min(vertexIndex / 6u, 4u);",
    "  let corner = vertexIndex % 6u;",
    "  var u = 0.0;",
    "  var v = 0.0;",
    // The pool is an open vessel, not a solid box: floor faces upward and
    // walls face inward. This matches the rounded path and lets ordinary back
    // culling hide the large exterior shell without making the water
    // double-sided. The water surface itself keeps depth writes enabled, so
    // the overlap fix is independent from this presentation winding.
    "  if (corner == 1u || corner == 2u || corner == 5u) { u = 1.0; }",
    "  if (corner == 1u || corner == 4u || corner == 5u) { v = 1.0; }",
    "  var worldPos = vec3f(0.0);",
    "  var normal = vec3f(0.0, 1.0, 0.0);",
    "  var tileUV = vec2f(0.0);",
    "  if (face == 0u) {",
    "    worldPos = vec3f(mix(-halfWidth, halfWidth, u), floorY, mix(-halfLength, halfLength, v));",
    "    normal = vec3f(0.0, 1.0, 0.0);",
    "    tileUV = worldPos.xz * 0.42;",
    "  } else if (face == 1u) {",
    "    worldPos = vec3f(mix(-halfWidth, halfWidth, u), mix(floorY, rimY, v), halfLength);",
    "    normal = vec3f(0.0, 0.0, -1.0);",
    "    tileUV = vec2f(worldPos.x * 0.42, worldPos.y * 0.72);",
    "  } else if (face == 2u) {",
    "    worldPos = vec3f(mix(halfWidth, -halfWidth, u), mix(floorY, rimY, v), -halfLength);",
    "    normal = vec3f(0.0, 0.0, 1.0);",
    "    tileUV = vec2f(worldPos.x * 0.42, worldPos.y * 0.72);",
    "  } else if (face == 3u) {",
    "    worldPos = vec3f(halfWidth, mix(floorY, rimY, v), mix(halfLength, -halfLength, u));",
    "    normal = vec3f(-1.0, 0.0, 0.0);",
    "    tileUV = vec2f(worldPos.z * 0.42, worldPos.y * 0.72);",
    "  } else {",
    "    worldPos = vec3f(-halfWidth, mix(floorY, rimY, v), mix(-halfLength, halfLength, u));",
    "    normal = vec3f(1.0, 0.0, 0.0);",
    "    tileUV = vec2f(worldPos.z * 0.42, worldPos.y * 0.72);",
    "  }",
    "  var out: VertexOutput;",
    "  out.worldPos = worldPos;",
    "  out.normal = normal;",
    "  out.tileUV = tileUV;",
    "  out.waterUV = worldPos.xz / max(vec2f(params.poolWidth * 2.0, params.poolLength * 2.0), vec2f(0.001)) + vec2f(0.5);",
    "  out.face = f32(face);",
    "  out.clipPos = frame.projMatrix * frame.viewMatrix * vec4f(worldPos, 1.0);",
    "  return out;",
    "}",
  ].join("\n");

  var SCENE_WATER_POOL_FRAGMENT_SOURCE = [
    "",
    "struct WaterUniforms {",
    "  resolution: u32,",
    "  cellCount: u32,",
    "  seedDrops: u32,",
    "  frameIndex: u32,",
    "  deltaTime: f32,",
    "  time: f32,",
    "  waveSpeed: f32,",
    "  damping: f32,",
    "  dropRadius: f32,",
    "  dropStrength: f32,",
    "  normalScale: f32,",
    "  poolWidth: f32,",
    "  poolHeight: f32,",
    "  poolLength: f32,",
    "  cornerRadius: f32,",
    "  poolShape: f32,",
    "  lightDir: vec4f,",
    "  shallowColor: vec4f,",
    "  deepColor: vec4f,",
    "  objectCenter: vec4f,",
    "  objectPreviousCenter: vec4f,",
    "  objectHalfSizeRadius: vec4f,",
    "  objectParams: vec4f,",
    "  opticsFlags: vec4f,",
    "  interactiveDrop: vec4f,",
    "  seedSalt: f32,",
    "};",
    "",
    "struct WaterDisplacementSphere {",
    "  offsetRadius: vec4f,",
    "};",
    "",
    "struct VertexOutput {",
    "  @builtin(position) clipPos: vec4f,",
    "  @location(0) worldPos: vec3f,",
    "  @location(1) normal: vec3f,",
    "  @location(2) tileUV: vec2f,",
    "  @location(3) waterUV: vec2f,",
    "  @location(4) face: f32,",
    "};",
    "",
    "@group(1) @binding(0) var<uniform> params: WaterUniforms;",
    "@group(1) @binding(1) var<storage, read> state: array<vec4f>;",
    "@group(1) @binding(2) var poolSampler: sampler;",
    "@group(1) @binding(3) var causticTexture: texture_2d<f32>;",
    "@group(1) @binding(4) var objectShadowTexture: texture_2d<f32>;",
    "@group(1) @binding(5) var tileTexture: texture_2d<f32>;",
    "",
    "fn waterIndex(x: u32, y: u32) -> u32 {",
    "  return y * params.resolution + x;",
    "}",
    "",
    "fn sampleWaterInfo(uv: vec2f) -> vec4f {",
    "  let safeUV = clamp(uv, vec2f(0.0), vec2f(1.0));",
    "  let maxCoord = max(params.resolution - 1u, 1u);",
    "  let x = min(u32(round(safeUV.x * f32(maxCoord))), params.resolution - 1u);",
    "  let y = min(u32(round(safeUV.y * f32(maxCoord))), params.resolution - 1u);",
    "  return state[waterIndex(x, y)];",
    "}",
    "",
    "fn objectPoolShadow(uv: vec2f, point: vec3f) -> f32 {",
    "  if (params.objectParams.x < 0.5 || params.opticsFlags.w <= 0.0) { return 0.0; }",
    "  let centerUV = params.objectCenter.xz * 0.5 + vec2f(0.5);",
    "  let aspect = vec2f(max(params.poolWidth / max(params.poolLength, 0.001), 0.001), 1.0);",
    "  let sphereRadius = max(params.objectHalfSizeRadius.w * 0.55, 0.018);",
    "  let cubeRadius = max(max(params.objectHalfSizeRadius.x, params.objectHalfSizeRadius.z) * 0.62, sphereRadius);",
    "  let radius = select(sphereRadius, cubeRadius, params.objectParams.x > 1.5);",
    "  let d = length((uv - centerUV) * aspect);",
    "  let footprint = 1.0 - smoothstep(radius, radius + max(radius * 1.25, 0.022), d);",
    "  let proximityRadius = select(params.objectHalfSizeRadius.w, max(max(params.objectHalfSizeRadius.x, params.objectHalfSizeRadius.y), params.objectHalfSizeRadius.z), params.objectParams.x > 1.5);",
    "  let proximity = 1.0 - smoothstep(proximityRadius, proximityRadius + max(proximityRadius * 2.0, 0.08), length(point - params.objectCenter.xyz));",
    "  return max(footprint * 0.68, proximity * 0.38);",
    "}",
    "",
    "@fragment fn fragmentMain(in: VertexOutput) -> @location(0) vec4f {",
    "  let waterUV = clamp(in.waterUV, vec2f(0.0), vec2f(1.0));",
    "  let info = sampleWaterInfo(waterUV);",
    "  let waterHeight = info.x * params.poolHeight;",
    "  let lightDir = normalize(params.lightDir.xyz);",
    "  let refracted = refract(-lightDir, vec3f(0.0, 1.0, 0.0), 1.0 / 1.333);",
    "  let refractedY = select(0.05, refracted.y, abs(refracted.y) > 0.05);",
    "  let projected = (in.worldPos.xz - in.worldPos.y * refracted.xz / refractedY) / max(vec2f(params.poolWidth * 2.0, params.poolLength * 2.0), vec2f(0.001));",
    "  let causticUV = clamp(projected * 0.75 + vec2f(0.5), vec2f(0.0), vec2f(1.0));",
    "  let tileColor = textureSample(tileTexture, poolSampler, in.tileUV).rgb;",
    "  let caustic = textureSample(causticTexture, poolSampler, causticUV).rgb;",
    "  let shadowMap = textureSample(objectShadowTexture, poolSampler, waterUV).r;",
    "  let objectShadow = max(shadowMap, objectPoolShadow(waterUV, in.worldPos));",
    "  let diffuse = max(dot(normalize(in.normal), normalize(-refracted)), 0.0);",
    "  let below = select(0.0, 1.0, in.worldPos.y < waterHeight);",
    "  let distanceFade = 1.0 / max(length(in.worldPos) * 0.52, 1.0);",
    "  let underwaterTint = vec3f(0.42, 0.92, 1.0);",
    "  let dryLight = 0.46 + diffuse * 0.34;",
    "  let causticEnergy = dot(caustic, vec3f(0.34, 0.44, 0.22)) * params.opticsFlags.x;",
    "  var color = tileColor * dryLight * distanceFade;",
    "  color = mix(color, color * underwaterTint * (0.72 + diffuse * 0.22) + caustic * (1.55 + causticEnergy * 0.6), below);",
    "  color = color * (1.0 - clamp(objectShadow, 0.0, 1.0) * 0.62);",
    "  let rim = smoothstep(0.0, 0.12, in.worldPos.y);",
    "  color = mix(color, color + vec3f(0.05, 0.035, 0.018), rim * (1.0 - below));",
    "  return vec4f(color, 1.0);",
    "}",
  ].join("\n");

  var SCENE_WATER_CAUSTICS_VERTEX_SOURCE = [
    "struct VertexOutput {",
    "  @builtin(position) clipPos: vec4f,",
    "  @location(0) uv: vec2f,",
    "};",
    "",
    "@vertex fn vertexMain(@builtin(vertex_index) vertexIndex: u32) -> VertexOutput {",
    "  let x = f32((vertexIndex << 1u) & 2u);",
    "  let y = f32(vertexIndex & 2u);",
    "  var out: VertexOutput;",
    "  out.uv = vec2f(x, y);",
    "  out.clipPos = vec4f(x * 2.0 - 1.0, 1.0 - y * 2.0, 0.0, 1.0);",
    "  return out;",
    "}",
  ].join("\n");

  var SCENE_WATER_CAUSTICS_FRAGMENT_SOURCE = [
    WGSL_COMMON_CONSTANTS,
    "",
    "struct WaterUniforms {",
    "  resolution: u32,",
    "  cellCount: u32,",
    "  seedDrops: u32,",
    "  frameIndex: u32,",
    "  deltaTime: f32,",
    "  time: f32,",
    "  waveSpeed: f32,",
    "  damping: f32,",
    "  dropRadius: f32,",
    "  dropStrength: f32,",
    "  normalScale: f32,",
    "  poolWidth: f32,",
    "  poolHeight: f32,",
    "  poolLength: f32,",
    "  cornerRadius: f32,",
    "  poolShape: f32,",
    "  lightDir: vec4f,",
    "  shallowColor: vec4f,",
    "  deepColor: vec4f,",
    "  objectCenter: vec4f,",
    "  objectPreviousCenter: vec4f,",
    "  objectHalfSizeRadius: vec4f,",
    "  objectParams: vec4f,",
    "  opticsFlags: vec4f,",
    "  interactiveDrop: vec4f,",
    "  seedSalt: f32,",
    "};",
    "",
    "struct WaterDisplacementSphere {",
    "  offsetRadius: vec4f,",
    "};",
    "",
    "struct VertexOutput {",
    "  @builtin(position) clipPos: vec4f,",
    "  @location(0) uv: vec2f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> params: WaterUniforms;",
    "@group(0) @binding(1) var<storage, read> state: array<vec4f>;",
    "@group(0) @binding(2) var<storage, read> objectSpheres: array<WaterDisplacementSphere>;",
    "@group(0) @binding(3) var objectShadowSampler: sampler;",
    "@group(0) @binding(4) var objectShadowTexture: texture_2d<f32>;",
    "",
    "fn waterIndex(x: u32, y: u32) -> u32 {",
    "  return y * params.resolution + x;",
    "}",
    "",
    "fn sampleWaterInfo(uv: vec2f) -> vec4f {",
    "  let safeUV = clamp(uv, vec2f(0.0), vec2f(1.0));",
    "  let maxCoord = max(params.resolution - 1u, 1u);",
    "  let x = min(u32(round(safeUV.x * f32(maxCoord))), params.resolution - 1u);",
    "  let y = min(u32(round(safeUV.y * f32(maxCoord))), params.resolution - 1u);",
    "  return state[waterIndex(x, y)];",
    "}",
    "",
    "fn objectShadowMask(uv: vec2f) -> f32 {",
    "  if (params.objectParams.x < 0.5 || params.opticsFlags.w <= 0.0) { return 0.0; }",
    "  let centerUV = params.objectCenter.xz * 0.5 + vec2f(0.5);",
    "  let aspect = vec2f(max(params.poolWidth / max(params.poolLength, 0.001), 0.001), 1.0);",
    "  if (params.objectParams.x >= 2.5) {",
    "    let count = min(u32(params.objectParams.z), 32u);",
    "    var mask = 0.0;",
    "    for (var i = 0u; i < count; i = i + 1u) {",
    "      let sphere = objectSpheres[i].offsetRadius;",
    "      let sphereUV = centerUV + sphere.xz * 0.5;",
    "      let radius = max(sphere.w * 0.72, 0.012);",
    "      let d = length((uv - sphereUV) * aspect);",
    "      mask = max(mask, 1.0 - smoothstep(radius, radius + max(radius * 1.25, 0.018), d));",
    "    }",
    "    return mask;",
    "  }",
    "  let sphereRadius = max(params.objectHalfSizeRadius.w * 0.55, 0.018);",
    "  let cubeRadius = max(max(params.objectHalfSizeRadius.x, params.objectHalfSizeRadius.z) * 0.6, sphereRadius);",
    "  let radius = select(sphereRadius, cubeRadius, params.objectParams.x > 1.5);",
    "  let d = length((uv - centerUV) * aspect);",
    "  return 1.0 - smoothstep(radius, radius + max(radius * 1.2, 0.02), d);",
    "}",
    "",
    "@fragment fn fragmentMain(in: VertexOutput) -> @location(0) vec4f {",
    "  let uv = clamp(in.uv, vec2f(0.0), vec2f(1.0));",
    "  let texel = 1.0 / max(f32(params.resolution), 1.0);",
    "  let c = sampleWaterInfo(uv);",
    "  let e = sampleWaterInfo(uv + vec2f(texel, 0.0));",
    "  let w = sampleWaterInfo(uv - vec2f(texel, 0.0));",
    "  let n = sampleWaterInfo(uv + vec2f(0.0, texel));",
    "  let s = sampleWaterInfo(uv - vec2f(0.0, texel));",
    "  let lightDir = normalize(params.lightDir.xyz);",
    "  let waterNormal = normalize(vec3f(c.z * params.normalScale, 1.0, c.w * params.normalScale));",
    "  let refracted = refract(-lightDir, waterNormal, 1.0 / 1.333);",
    "  let convergence = abs((e.x + w.x + n.x + s.x) - c.x * 4.0);",
    "  let slopeFocus = max(0.0, dot(normalize(vec3f(-refracted.x, max(refracted.y, 0.05), -refracted.z)), waterNormal));",
    "  let shimmer = 0.5 + 0.5 * sin((uv.x * 41.0 + uv.y * 37.0) + params.time * 2.4 + c.x * 180.0);",
    "  var intensity = smoothstep(0.001, 0.028, convergence * 0.72 + length(c.zw) * 0.035);",
    "  intensity = intensity * (0.52 + 0.48 * shimmer) * (0.58 + 0.42 * slopeFocus);",
    "  let shadow = max(objectShadowMask(uv), textureSample(objectShadowTexture, objectShadowSampler, uv).r);",
    "  intensity = intensity * (1.0 - shadow * 0.82);",
    "  let warm = vec3f(1.0, 0.78, 0.42);",
    "  let cool = vec3f(0.44, 0.95, 1.0);",
    "  return vec4f(mix(cool, warm, clamp(intensity * 1.8, 0.0, 1.0)) * intensity, 1.0);",
    "}",
  ].join("\n");

  var SCENE_WATER_OBJECT_TEXTURE_VERTEX_SOURCE = SCENE_WATER_CAUSTICS_VERTEX_SOURCE;

  var SCENE_WATER_OBJECT_TEXTURE_FRAGMENT_SOURCE = [
    WGSL_COMMON_CONSTANTS,
    "",
    "struct WaterUniforms {",
    "  resolution: u32,",
    "  cellCount: u32,",
    "  seedDrops: u32,",
    "  frameIndex: u32,",
    "  deltaTime: f32,",
    "  time: f32,",
    "  waveSpeed: f32,",
    "  damping: f32,",
    "  dropRadius: f32,",
    "  dropStrength: f32,",
    "  normalScale: f32,",
    "  poolWidth: f32,",
    "  poolHeight: f32,",
    "  poolLength: f32,",
    "  cornerRadius: f32,",
    "  poolShape: f32,",
    "  lightDir: vec4f,",
    "  shallowColor: vec4f,",
    "  deepColor: vec4f,",
    "  objectCenter: vec4f,",
    "  objectPreviousCenter: vec4f,",
    "  objectHalfSizeRadius: vec4f,",
    "  objectParams: vec4f,",
    "  opticsFlags: vec4f,",
    "  interactiveDrop: vec4f,",
    "  seedSalt: f32,",
    "};",
    "",
    "struct WaterDisplacementSphere {",
    "  offsetRadius: vec4f,",
    "};",
    "",
    "struct VertexOutput {",
    "  @builtin(position) clipPos: vec4f,",
    "  @location(0) uv: vec2f,",
    "};",
    "",
    "struct ObjectTextureOutput {",
    "  @location(0) reflection: vec4f,",
    "  @location(1) clippedReflection: vec4f,",
    "  @location(2) refraction: vec4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> params: WaterUniforms;",
    "@group(0) @binding(1) var<storage, read> objectSpheres: array<WaterDisplacementSphere>;",
    "",
    "fn objectMaskInfo(uv: vec2f) -> vec4f {",
    "  if (params.objectParams.x < 0.5 || params.opticsFlags.w <= 0.0) { return vec4f(0.0); }",
    "  let centerUV = params.objectCenter.xz * 0.5 + vec2f(0.5);",
    "  let aspect = vec2f(max(params.poolWidth / max(params.poolLength, 0.001), 0.001), 1.0);",
    "  var mask = 0.0;",
    "  var core = 0.0;",
    "  if (params.objectParams.x >= 2.5) {",
    "    let count = min(u32(params.objectParams.z), 32u);",
    "    for (var i = 0u; i < count; i = i + 1u) {",
    "      let sphere = objectSpheres[i].offsetRadius;",
    "      let sphereUV = centerUV + sphere.xz * 0.5;",
    "      let radius = max(sphere.w * 0.72, 0.012);",
    "      let d = length((uv - sphereUV) * aspect);",
    "      let localMask = 1.0 - smoothstep(radius, radius + max(radius * 1.18, 0.018), d);",
    "      mask = max(mask, localMask);",
    "      core = max(core, 1.0 - smoothstep(radius * 0.42, radius, d));",
    "    }",
    "  } else {",
    "    let sphereRadius = max(params.objectHalfSizeRadius.w * 0.55, 0.018);",
    "    let cubeRadius = max(max(params.objectHalfSizeRadius.x, params.objectHalfSizeRadius.z) * 0.6, sphereRadius);",
    "    let radius = select(sphereRadius, cubeRadius, params.objectParams.x > 1.5);",
    "    let d = length((uv - centerUV) * aspect);",
    "    mask = 1.0 - smoothstep(radius, radius + max(radius * 1.2, 0.02), d);",
    "    core = 1.0 - smoothstep(radius * 0.38, radius, d);",
    "  }",
    "  let objectTop = params.objectCenter.y + max(params.objectHalfSizeRadius.y, params.objectHalfSizeRadius.w);",
    "  let clipped = smoothstep(-0.08, 0.16, objectTop);",
    "  return vec4f(clamp(mask, 0.0, 1.0), clamp(core, 0.0, 1.0), clipped, 0.0);",
    "}",
    "",
    "@fragment fn fragmentMain(in: VertexOutput) -> ObjectTextureOutput {",
    "  let uv = clamp(in.uv, vec2f(0.0), vec2f(1.0));",
    "  let lightOffset = normalize(params.lightDir.xyz).xz * vec2f(0.025, -0.025);",
    "  let info = objectMaskInfo(clamp(uv - lightOffset, vec2f(0.0), vec2f(1.0)));",
    "  let mask = info.x;",
    "  let core = info.y;",
    "  let clippedMask = mask * info.z;",
    "  let rim = clamp(mask - core * 0.35, 0.0, 1.0);",
    "  let reflectionColor = mix(vec3f(0.12, 0.24, 0.42), vec3f(0.82, 0.92, 1.0), rim);",
    "  let clippedColor = mix(vec3f(0.10, 0.18, 0.28), vec3f(0.72, 0.84, 0.96), core);",
    "  let refractionColor = mix(vec3f(0.06, 0.22, 0.28), vec3f(0.88, 0.66, 0.36), core);",
    "  var out: ObjectTextureOutput;",
    "  out.reflection = vec4f(reflectionColor, mask * params.opticsFlags.y);",
    "  out.clippedReflection = vec4f(clippedColor, clippedMask * params.opticsFlags.y);",
    "  out.refraction = vec4f(refractionColor, mask * params.opticsFlags.z);",
    "  return out;",
    "}",
    "",
    "@fragment fn shadowMain(in: VertexOutput) -> @location(0) vec4f {",
    "  let info = objectMaskInfo(clamp(in.uv, vec2f(0.0), vec2f(1.0)));",
    "  let shadow = info.x * (0.42 + 0.58 * info.y);",
    "  return vec4f(vec3f(shadow), 1.0);",
    "}",
  ].join("\n");

  var SCENE_WATER_OBJECT_SHADOW_FRAGMENT_SOURCE = SCENE_WATER_OBJECT_TEXTURE_FRAGMENT_SOURCE;

  var SCENE_WATER_OBJECT_MESH_SHADOW_VERTEX_SOURCE = [
    "struct ObjectMeshShadowUniforms {",
    "  light: vec4f,",
    "  pool: vec4f,",
    "};",
    "",
    "struct VertexInput {",
    "    @location(0) position: vec3f,",
    "    @location(1) normal: vec3f,",
    "    @location(2) uv: vec2f,",
    "    @location(3) tangent: vec4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> shadow: ObjectMeshShadowUniforms;",
    "",
    "@vertex fn vertexMain(in: VertexInput) -> @builtin(position) vec4f {",
    "    let worldPosition = in.position;",
    "    let refractedLight = refract(-normalize(shadow.light.xyz), vec3f(0.0, 1.0, 0.0), 1.0 / 1.333);",
    "    let fallbackY = select(-0.0001, 0.0001, refractedLight.y >= 0.0);",
    "    let refractedY = select(fallbackY, refractedLight.y, abs(refractedLight.y) > 0.0001);",
    "    let projected = 0.75 * (worldPosition.xz - worldPosition.y * refractedLight.xz / refractedY);",
    "    return vec4f(",
    "      projected.x / max(shadow.pool.x, 0.0001),",
    "      projected.y / max(shadow.pool.y, 0.0001),",
    "      0.0,",
    "      1.0",
    "    );",
    "}",
  ].join("\n");

  var SCENE_WATER_OBJECT_MESH_SHADOW_FRAGMENT_SOURCE = [
    "@fragment fn fragmentMain() -> @location(0) vec4f {",
    "  return vec4f(1.0);",
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

  // Cull-path instanced vertex shader: location 8 = pickData (vec4u) instead
  // of instanceColor (vec4f). The output struct drops instanceColor — material
  // color is read from the per-material uniform in the fragment shader, so no
  // per-instance color interpolation is needed on the cull path. VertexOutput
  // is identical to the non-cull variant (same locations 0-4) so it is
  // compatible with WGSL_PBR_FRAGMENT without modification. pickData is read
  // in vertex but not forwarded to fragment (gpu picking consumes it natively).
  var WGSL_PBR_INSTANCED_CULL_VERTEX = [
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
    "    @location(8) pickData: vec4u,",
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
    "    out.instanceColor = vec4f(1.0, 1.0, 1.0, 1.0);",
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
    // Group 0: per-frame
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "@group(0) @binding(1) var<storage, read> lights: array<Light>;",
    "@group(0) @binding(2) var<uniform> fog: FogUniforms;",
    "@group(0) @binding(3) var<uniform> env: EnvUniforms;",
    "@group(0) @binding(4) var shadowMap0: texture_depth_2d;",
    "@group(0) @binding(5) var shadowSampler0: sampler_comparison;",
    "@group(0) @binding(6) var shadowMap1: texture_depth_2d;",
    "@group(0) @binding(7) var shadowSampler1: sampler_comparison;",
    "@group(0) @binding(8) var<uniform> shadow: ShadowUniforms;",
    "@group(0) @binding(9) var iblIrradiance: texture_cube<f32>;",
    "@group(0) @binding(10) var iblRadiance: texture_cube<f32>;",
    "@group(0) @binding(11) var iblBRDFLUT: texture_2d<f32>;",
    "@group(0) @binding(12) var iblSampler: sampler;",
    "@group(0) @binding(13) var envMapTex: texture_2d<f32>;",
    "@group(0) @binding(14) var envMapSampler: sampler;",
    "",
    // Group 1: per-material
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
    "@group(1) @binding(11) var occlusionTex: texture_2d<f32>;",
    "@group(1) @binding(12) var occlusionSamp: sampler;",
    "",
    "fn shadowProjectedCoords(worldPos: vec3f, lightSpaceMatrix: mat4x4f) -> vec3f {",
    "    let lightSpacePos = lightSpaceMatrix * vec4f(worldPos, 1.0);",
    "    let projCoords3 = lightSpacePos.xyz / lightSpacePos.w;",
    "    return projCoords3 * 0.5 + 0.5;",
    "}",
    "",
    // 4-tap Poisson disk PCF shadow sampling for shadow slot 0.
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
    // 4-tap Poisson disk PCF shadow sampling for shadow slot 1.
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
    // GGX/Trowbridge-Reitz normal distribution function.
    "fn distributionGGX(N: vec3f, H: vec3f, roughness: f32) -> f32 {",
    "    let a = roughness * roughness;",
    "    let a2 = a * a;",
    "    let NdotH = max(dot(N, H), 0.0);",
    "    let NdotH2 = NdotH * NdotH;",
    "    let denom = NdotH2 * (a2 - 1.0) + 1.0;",
    "    return a2 / max(PI * denom * denom, 0.0000001);",
    "}",
    "",
    // Smith geometry function (GGX variant) -- single direction.
    "fn geometrySchlickGGX(NdotV: f32, roughness: f32) -> f32 {",
    "    let r = roughness + 1.0;",
    "    let k = (r * r) / 8.0;",
    "    return NdotV / (NdotV * (1.0 - k) + k);",
    "}",
    "",
    // Smith geometry function -- combined for view and light directions.
    "fn geometrySmith(N: vec3f, V: vec3f, L: vec3f, roughness: f32) -> f32 {",
    "    let NdotV = max(dot(N, V), 0.0);",
    "    let NdotL = max(dot(N, L), 0.0);",
    "    return geometrySchlickGGX(NdotV, roughness) * geometrySchlickGGX(NdotL, roughness);",
    "}",
    "",
    // Schlick fresnel approximation. F90 is the authored specular intensity:
    // the grazing reflectance the KHR specular extension scales, so an
    // intensity below 1 dims the whole lobe, not just the F0 floor.
    "fn fresnelSchlick(cosTheta: f32, F0: vec3f, F90: f32) -> vec3f {",
    "    return F0 + (vec3f(F90) - F0) * pow(clamp(1.0 - cosTheta, 0.0, 1.0), 5.0);",
    "}",
    "",
    "fn fresnelSchlickRoughness(cosTheta: f32, F0: vec3f, F90: f32, roughness: f32) -> vec3f {",
    "    return F0 + (max(vec3f(1.0 - roughness) * F90, F0) - F0) * pow(clamp(1.0 - cosTheta, 0.0, 1.0), 5.0);",
    "}",
    "",
    "fn rotateEnvY(dir: vec3f, radians: f32) -> vec3f {",
    "    let c = cos(radians);",
    "    let s = sin(radians);",
    "    return vec3f(dir.x * c + dir.z * s, dir.y, -dir.x * s + dir.z * c);",
    "}",
    "",
    // Equirectangular direction-to-UV mapping. Ported verbatim from the GLSL
    // source (16-scene-webgl.js envEquirectUV) so both backends sample the
    // same texel for the same direction.
    "fn envEquirectUV(dir: vec3f) -> vec2f {",
    "    let d = normalize(dir);",
    "    return vec2f(atan2(d.z, d.x) / (2.0 * PI) + 0.5, asin(clamp(d.y, -1.0, 1.0)) / PI + 0.5);",
    "}",
    "",
    // Point light distance attenuation.
    "fn pointLightAttenuation(distance: f32, range: f32, decay: f32) -> f32 {",
    "    if (range > 0.0) {",
    "        let ratio = clamp(1.0 - pow(distance / range, 4.0), 0.0, 1.0);",
    "        return ratio * ratio / max(distance * distance, 0.0001);",
    "    }",
    "    return 1.0 / max(pow(distance, decay), 0.0001);",
    "}",
    "",
    // Spot cone falloff. Ported from the WebGL2 renderer so a spot light shades
    // the same on both GPU backends. L points from the surface toward the light;
    // spotDir is the direction the light shines. These comments stay on the JS
    // side on purpose: text inside a WGSL string ships in the bundle, a JS
    // comment does not.
    "fn spotConeAttenuation(L: vec3f, spotDir: vec3f, angle: f32, penumbra: f32) -> f32 {",
    "    let cosAngle = dot(L, -normalize(spotDir));",
    "    let outerCos = cos(angle);",
    "    let innerCos = cos(angle * (1.0 - penumbra));",
    "    return clamp((cosAngle - outerCos) / max(innerCos - outerCos, 0.001), 0.0, 1.0);",
    "}",
    "",
    // -- Rect-area light: polygon form factor --------------------------------
    //
    // The next three functions reproduce the diffuse half of three.js
    // RE_Direct_RectArea_Physical. three.js evaluates the diffuse term with
    // LTC_Evaluate and an IDENTITY matrix, which reduces to the analytic polygon
    // form factor and needs no lookup table. The diffuse response of a
    // RectAreaLight is therefore exact here, not an approximation.
    //
    // The specular half of three.js reads two fitted 64x64 LTC tables. This
    // renderer uploads no such tables, so rectAreaLightRadiance approximates
    // specular with a representative point. See the rect-area-specular cell in
    // scene/capability/capability.go for the recorded gap.

    // Edge integral of the polygon form factor.
    "fn ltcEdgeVectorFormFactor(v1: vec3f, v2: vec3f) -> vec3f {",
    "    let x = dot(v1, v2);",
    "    let y = abs(x);",
    "    let a = 0.8543985 + (0.4965155 + 0.0145206 * y) * y;",
    "    let b = 3.4175940 + (4.1616724 + y) * y;",
    "    let v = a / b;",
    "    var thetaSinTheta = 0.5 * inverseSqrt(max(1.0 - x * x, 0.0000001)) - v;",
    "    if (x > 0.0) {",
    "        thetaSinTheta = v;",
    "    }",
    "    return cross(v1, v2) * thetaSinTheta;",
    "}",
    "",
    // Clipped sphere form factor of the accumulated edge vector.
    "fn ltcClippedSphereFormFactor(f: vec3f) -> f32 {",
    "    let l = length(f);",
    "    return max((l * l + f.z) / (l + 1.0), 0.0);",
    "}",
    "",
    // Diffuse form factor of a rectangle at a shaded point. Returns 0 when the
    // point sits behind the emitter, so a rect-area light only lights the side
    // its direction points at.
    //
    // The tangent basis: with the identity matrix the form factor depends only
    // on N, so any basis around N gives the same answer. Pick a stable one.
    "fn rectAreaFormFactor(N: vec3f, P: vec3f, halfWidth: vec3f, halfHeight: vec3f, center: vec3f) -> f32 {",
    "    let rect0 = center + halfWidth - halfHeight;",
    "    let rect1 = center - halfWidth - halfHeight;",
    "    let rect2 = center - halfWidth + halfHeight;",
    "    let rect3 = center + halfWidth + halfHeight;",
    "    let lightNormal = cross(rect1 - rect0, rect3 - rect0);",
    "    if (dot(lightNormal, P - rect0) < 0.0) {",
    "        return 0.0;",
    "    }",
    "    var helper = vec3f(0.0, 0.0, 1.0);",
    "    if (abs(N.z) > 0.999) {",
    "        helper = vec3f(1.0, 0.0, 0.0);",
    "    }",
    "    let T1 = normalize(cross(helper, N));",
    "    let T2 = cross(N, T1);",
    "    let basis = transpose(mat3x3f(T1, T2, N));",
    "    let c0 = normalize(basis * (rect0 - P));",
    "    let c1 = normalize(basis * (rect1 - P));",
    "    let c2 = normalize(basis * (rect2 - P));",
    "    let c3 = normalize(basis * (rect3 - P));",
    "    var edges = ltcEdgeVectorFormFactor(c0, c1);",
    "    edges = edges + ltcEdgeVectorFormFactor(c1, c2);",
    "    edges = edges + ltcEdgeVectorFormFactor(c2, c3);",
    "    edges = edges + ltcEdgeVectorFormFactor(c3, c0);",
    "    return ltcClippedSphereFormFactor(edges);",
    "}",
    "",
    // Closest point on the rectangle to the mirror direction. This is the
    // representative-point stand-in for the LTC specular table.
    "fn rectAreaRepresentativePoint(P: vec3f, N: vec3f, V: vec3f, center: vec3f, halfWidth: vec3f, halfHeight: vec3f, planeNormal: vec3f) -> vec3f {",
    "    let R = reflect(-V, N);",
    "    var hit = center;",
    "    let denom = dot(planeNormal, R);",
    "    if (abs(denom) > 0.00001) {",
    "        let t = dot(planeNormal, center - P) / denom;",
    "        if (t > 0.0) {",
    "            hit = P + R * t;",
    "        }",
    "    }",
    "    let wLen = max(length(halfWidth), 0.00001);",
    "    let hLen = max(length(halfHeight), 0.00001);",
    "    let wDir = halfWidth / wLen;",
    "    let hDir = halfHeight / hLen;",
    "    let offset = hit - center;",
    "    let u = clamp(dot(offset, wDir), -wLen, wLen);",
    "    let v = clamp(dot(offset, hDir), -hLen, hLen);",
    "    return center + wDir * u + hDir * v;",
    "}",
    "",
    // Full rect-area contribution: exact diffuse form factor plus a
    // representative-point specular lobe.
    //
    // three.js multiplies the diffuse term by diffuseColor, which already folds
    // in metalness. The form factor carries the cosine and the solid angle, so
    // there is no separate NdotL or 1/PI in the diffuse line.
    "fn rectAreaLightRadiance(light: Light, P: vec3f, N: vec3f, V: vec3f, albedo: vec3f, roughness: f32, metalness: f32, F0: vec3f, F90: f32, NoV: f32) -> vec3f {",
    "    let center = light.position.xyz;",
    "    let halfWidth = light.areaHalfWidth.xyz;",
    "    let halfHeight = light.areaHalfHeight.xyz;",
    "    let formFactor = rectAreaFormFactor(N, P, halfWidth, halfHeight, center);",
    "    if (formFactor <= 0.0) {",
    "        return vec3f(0.0);",
    "    }",
    "    let radiance = light.color.rgb * light.direction.w;",
    "    var out = albedo * (1.0 - metalness) * radiance * formFactor;",
    "    let repPoint = rectAreaRepresentativePoint(P, N, V, center, halfWidth, halfHeight, light.direction.xyz);",
    "    let toRep = repPoint - P;",
    "    let repDist = length(toRep);",
    "    let L = toRep / max(repDist, 0.0001);",
    "    let NdotL = max(dot(N, L), 0.0);",
    "    if (NdotL > 0.0) {",
    "        let H = normalize(V + L);",
    "        let D = distributionGGX(N, H, roughness);",
    "        let G = geometrySmith(N, V, L, roughness);",
    "        let F = fresnelSchlick(max(dot(H, V), 0.0), F0, F90);",
    "        let brdf = (D * G * F) / (4.0 * NoV * NdotL + 0.0001);",
    "        out = out + radiance * brdf * formFactor;",
    "    }",
    "    return out;",
    "}",
    "",
    "@fragment fn fragmentMain(in: VertexOutput) -> @location(0) vec4f {",
    // Resolve material properties, sampling textures when available.
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
    "    var ambientOcclusion = 1.0;",
    "    if (material.hasOcclusionMap != 0u) {",
    "        ambientOcclusion = clamp(textureSample(occlusionTex, occlusionSamp, in.uv).r, 0.0, 1.0);",
    "    }",
    "",
    "    var emissiveStrength = material.emissive;",
    "    var emissiveColor = albedo;",
    "    if (material.hasEmissiveMap != 0u) {",
    "        emissiveColor = textureSample(emissiveTex, emissiveSamp, in.uv).rgb;",
    "    }",
    "",
    // Unlit path: output albedo directly.
    "    if (material.unlit != 0u) {",
    "        let color = albedo + emissiveColor * emissiveStrength;",
    "        return vec4f(color, finalOpacity);",
    "    }",
    "",
    // Resolve per-pixel normal via TBN matrix.
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
      // Fresnel reflectance at normal incidence — the material's authored
      // dielectric F0 = ((ior-1)/(ior+1))^2 blended with metallic albedo.
      // Defaults to 0.04 (ior 1.5); 1.0 in the glTF ior=0 compatibility mode.
      // Direct and environment consumers share this single F0.
      // The authored KHR specular factors refine the dielectric lane:
      // material.specularF0 is min(IOR F0 * linear colour, 1) * intensity and
      // material.specularF90 is the intensity itself, both prepared CPU-side
      // so the packed buffer is always finite and bounded to [0, 1]. The
      // metallic mix keeps its exact fully-metal branch so a metal never
      // reads the dielectric lane.
      "    let specF0 = material.specularF0;",
      "    let specF90 = material.specularF90;",
      "    var F0 = mix(specF0, albedo, metalness);",
      "    var F90 = mix(specF90, 1.0, metalness);",
      "    if (metalness >= 1.0) {",
      "        F0 = albedo;",
      "        F90 = 1.0;",
      "    }",
    "",
    // Accumulate direct lighting.
    "    var Lo = vec3f(0.0);",
    "",
    // arrayLength bounds the loop against the storage buffer the JS side sized
    // this frame. No compile-time light cap remains.
    "    let lightCount = min(frame.lightCount, arrayLength(&lights));",
    "    for (var i = 0u; i < lightCount; i = i + 1u) {",
    "        let light = lights[i];",
    "        let lightType = u32(light.position.w);",
    "        let lightColor = light.color.rgb;",
    "        let intensity = light.direction.w;",
    "        let range = light.color.a;",
    "        let decay = light.params.x;",
    "",
    // Ambient light (type 0): flat contribution, no BRDF. A light probe
    // arrives here too; see sceneWebGPULightTypeCode.
    "        if (lightType == 0u) {",
    "            Lo = Lo + albedo * lightColor * intensity;",
    "            continue;",
    "        }",
    "",
    // Hemisphere light (type 4): sky/ground blend driven by normal Y. Matches
    // the WebGL2 renderer.
    "        if (lightType == 4u) {",
    "            let hBlend = N.y * 0.5 + 0.5;",
    "            let hemiColor = mix(light.groundPenumbra.rgb, lightColor, hBlend);",
    "            Lo = Lo + albedo * hemiColor * intensity;",
    "            continue;",
    "        }",
    "",
    // Rect-area light (type 5): the rectangle's shape drives both terms. The
    // form factor already carries the cosine and the falloff, so the shared
    // BRDF block below does not apply.
    "        if (lightType == 5u) {",
    "            Lo = Lo + rectAreaLightRadiance(light, in.worldPos, N, V, albedo, roughness, metalness, F0, F90, NoV);",
    "            continue;",
    "        }",
    "",
    "        var L: vec3f;",
    "        var attenuation: f32 = 1.0;",
    "",
    "        if (lightType == 1u) {",
    // Directional light.
    "            L = normalize(-light.direction.xyz);",
    // Spot light (type 3): cone falloff times point-light distance falloff.
    "        } else if (lightType == 3u) {",
    "            let toLight = light.position.xyz - in.worldPos;",
    "            let dist = length(toLight);",
    "            L = toLight / max(dist, 0.0001);",
    "            let cone = spotConeAttenuation(L, light.direction.xyz, light.params.w, light.groundPenumbra.a);",
    "            attenuation = pointLightAttenuation(dist, range, decay) * cone;",
    "        } else {",
    // Point light (type 2).
    "            let toLight = light.position.xyz - in.worldPos;",
    "            let dist = length(toLight);",
    "            L = toLight / max(dist, 0.0001);",
    "            attenuation = pointLightAttenuation(dist, range, decay);",
    "        }",
    "",
    "        let H = normalize(V + L);",
    "        let NdotL = max(dot(N, L), 0.0);",
    "",
    // Cook-Torrance specular BRDF.
    "        let D = distributionGGX(N, H, roughness);",
    "        let G = geometrySmith(N, V, L, roughness);",
    "        let F = fresnelSchlick(max(dot(H, V), 0.0), F0, F90);",
    "",
    "        let numerator = D * G * F;",
    "        let denominator = 4.0 * NoV * NdotL + 0.0001;",
    "        let specular = numerator / denominator;",
    "",
    // Energy conservation: diffuse complement of the dielectric specular.
    // The weight is the scalar (1 - maxRGB(dielectric Fresnel)) *
    // (1 - metalness), so the diffuse lobe is never tinted by the inverse of
    // the Fresnel colour and never borrows the metallic Fresnel.
    "        let Fdiel = fresnelSchlick(max(dot(H, V), 0.0), specF0, specF90);",
    "        let kD = (1.0 - max(Fdiel.x, max(Fdiel.y, Fdiel.z))) * (1.0 - metalness);",
    "",
    // Shadow attenuation for directional lights.
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
    "    // Assetpipe split-sum IBL, with hemisphere fallback while products load.",
    "    var ambient: vec3f;",
    "    if (env.hasIBL != 0u) {",
    "        let Nr = rotateEnvY(N, env.envRotation);",
    "        let Rr = rotateEnvY(reflect(-V, N), env.envRotation);",
    "        let FdielEnv = fresnelSchlickRoughness(NoV, specF0, specF90, roughness);",
    "        let kDenv = (1.0 - max(FdielEnv.x, max(FdielEnv.y, FdielEnv.z))) * (1.0 - metalness);",
    "        let irradiance = textureSample(iblIrradiance, iblSampler, Nr).rgb;",
    "        let maxLod = f32(max(env.radianceMipLevels, 1u) - 1u);",
    "        let prefiltered = textureSampleLevel(iblRadiance, iblSampler, Rr, roughness * maxLod).rgb;",
    "        let brdf = textureSample(iblBRDFLUT, iblSampler, vec2f(NoV, roughness)).rg;",
    "        let diffuseIBL = irradiance * albedo * kDenv;",
    "        let specularIBL = prefiltered * (F0 * brdf.x + vec3f(F90) * brdf.y);",
    "        ambient = (diffuseIBL + specularIBL) * env.envIntensity;",
    "    } else if (env.hasEnvMap != 0u) {",
    "        let Nr = rotateEnvY(N, env.envRotation);",
    "        let Rr = rotateEnvY(reflect(-V, N), env.envRotation);",
    "        let envDiffuse = textureSample(envMapTex, envMapSampler, envEquirectUV(Nr)).rgb * albedo;",
    "        let envSpecular = textureSample(envMapTex, envMapSampler, envEquirectUV(Rr)).rgb;",
    "        let Fenv = fresnelSchlickRoughness(NoV, F0, F90, roughness);",
    "        let FdielEnv = fresnelSchlickRoughness(NoV, specF0, specF90, roughness);",
    "        let kDenv = (1.0 - max(FdielEnv.x, max(FdielEnv.y, FdielEnv.z))) * (1.0 - metalness);",
    "        ambient = (kDenv * envDiffuse + envSpecular * Fenv * (1.0 - roughness * 0.65)) * env.envIntensity;",
    "    } else {",
    "        let hemi = N.y * 0.5 + 0.5;",
    "        let envDiffuse = env.ambientColor * env.ambientIntensity",
    "                       + env.skyColor * env.skyIntensity * hemi",
    "                       + env.groundColor * env.groundIntensity * (1.0 - hemi);",
    "        ambient = envDiffuse * albedo;",
    "    }",
    "    ambient = ambient * ambientOcclusion;",
    "",
    // Emissive contribution.
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
    // Exponential fog.
    "    if (fog.hasFog != 0u) {",
    "        let fogDist = length(in.worldPos - frame.cameraPos);",
    "        let fogFactor = exp(-fog.fogDensity * fog.fogDensity * fogDist * fogDist);",
    "        color = mix(fog.fogColor, color, clamp(fogFactor, 0.0, 1.0));",
    "    }",
    "",
    // Mode 4 hands linear scene-referred values to the post chain. Every
    // direct-to-display mode applies its authored curve followed by the
    // display transfer exactly once; filmic already returns display values.
    "    if (frame.toneMap != 4u) {",
    "        if (frame.toneMap == 1u) {",
    "            color = (color * (2.51 * color + vec3f(0.03))) / (color * (2.43 * color + vec3f(0.59)) + vec3f(0.14));",
    "        } else if (frame.toneMap == 2u) {",
    "            color = color / (color + vec3f(1.0));",
    "        } else if (frame.toneMap == 3u) {",
    "            let hejl = max(vec3f(0.0), color - vec3f(0.004));",
    "            color = clamp((hejl * (6.2 * hejl + vec3f(0.5))) / (hejl * (6.2 * hejl + vec3f(1.7)) + vec3f(0.06)), vec3f(0.0), vec3f(1.0));",
    "        }",
    "        if (frame.toneMap != 3u) {",
    "            color = pow(max(color, vec3f(0.0)), vec3f(1.0 / 2.2));",
    "        }",
    "    }",
    "",
    "    return vec4f(color, finalOpacity);",
    "}",
  ].join("\n");

  function sceneWaterObjectMeshFragmentSource(texturePassMode) {
    var mode = Math.max(1, Math.min(2, Math.floor(sceneNumber(texturePassMode, 1))));
    return [
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
      "@group(1) @binding(0) var<uniform> material: MaterialUniforms;",
      "@group(1) @binding(1) var albedoTex: texture_2d<f32>;",
      "@group(1) @binding(2) var albedoSamp: sampler;",
      "@group(1) @binding(9) var emissiveTex: texture_2d<f32>;",
      "@group(1) @binding(10) var emissiveSamp: sampler;",
      "",
      "@fragment fn fragmentMain(in: VertexOutput) -> @location(0) vec4f {",
      "    let texturePassMode = " + mode + "u;",
      "    if (texturePassMode == 2u && in.worldPos.y < 0.0) { discard; }",
      "    var albedo = material.albedo;",
      "    if (material.hasAlbedoMap != 0u) {",
      "        albedo = albedo * textureSample(albedoTex, albedoSamp, in.uv).rgb;",
      "    }",
      "    albedo = albedo * in.instanceColor.rgb;",
      "    var emissiveColor = albedo;",
      "    if (material.hasEmissiveMap != 0u) {",
      "        emissiveColor = textureSample(emissiveTex, emissiveSamp, in.uv).rgb;",
      "    }",
      "    let normal = normalize(in.normal);",
      "    let upLight = clamp(normal.y * 0.5 + 0.5, 0.0, 1.0);",
      "    let rim = pow(1.0 - upLight, 2.0);",
      "    var color = albedo * (0.58 + upLight * 0.34) + emissiveColor * material.emissive;",
      "    if (texturePassMode == 2u) {",
      "        color = mix(color, vec3f(0.62, 0.82, 0.96), 0.18 + rim * 0.24);",
      "    } else {",
      "        color = mix(color, vec3f(0.08, 0.18, 0.26), 0.08);",
      "    }",
      "    return vec4f(color, material.opacity * clamp(in.instanceColor.a, 0.0, 1.0));",
      "}",
    ].join("\n");
  }

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
    // Unit quad: 6 vertices for 2 triangles.
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
    // Compute point size with optional attenuation.
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
    // Billboard: offset in clip space by quad * pixelSize.
    "    let clipPos = frame.projMatrix * viewPos;",
    "    let viewport = max(vec2f(frame.viewportWidth, frame.viewportHeight), vec2f(1.0));",
    "    let ndcOffsetX = quad.x * pixelSize / viewport.x * clipPos.w * 2.0;",
    "    let ndcOffsetY = quad.y * pixelSize / viewport.y * clipPos.w * 2.0;",
    "",
    "    var out: PointsOutput;",
    "    out.clipPos = vec4f(clipPos.x + ndcOffsetX, clipPos.y + ndcOffsetY, clipPos.z, clipPos.w);",
    "",
    // Color.
    "    if (points.flags.x != 0u) {",
    "        out.color = p.color.rgb;",
    "    } else {",
    "        out.color = points.defaultColorAndSize.rgb;",
    "    }",
    "    out.alpha = p.color.a * points.params.x;",
    "    out.pointCoord = quad + vec2f(0.5, 0.5);",
    "    out.pointSize = pixelSize;",
    "",
    // Fog.
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
    // Unit quad: 6 vertices for 2 triangles.
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
    // Compute point size with optional attenuation.
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
    // Billboard: offset in clip space by quad * pixelSize.
    "    let clipPos = frame.projMatrix * viewPos;",
    "    let viewport = max(vec2f(frame.viewportWidth, frame.viewportHeight), vec2f(1.0));",
    "    let ndcOffsetX = quad.x * pixelSize / viewport.x * clipPos.w * 2.0;",
    "    let ndcOffsetY = quad.y * pixelSize / viewport.y * clipPos.w * 2.0;",
    "",
    "    var out: PointsOutput;",
    "    out.clipPos = vec4f(clipPos.x + ndcOffsetX, clipPos.y + ndcOffsetY, clipPos.z, clipPos.w);",
    "",
    // Color.
    "    if (points.flags.x != 0u) {",
    "        out.color = in.color.rgb;",
    "    } else {",
    "        out.color = points.defaultColorAndSize.rgb;",
    "    }",
    "    out.alpha = in.color.a * points.params.x;",
    "    out.pointCoord = quad + vec2f(0.5, 0.5);",
    "    out.pointSize = pixelSize;",
    "",
    // Fog.
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
    // Soft knee, not a hard cut. A hard cut is discontinuous at the authored
    // threshold: a pixel one part in a thousand below it contributes nothing
    // and the same pixel one part above contributes its whole colour, so a slow
    // camera move snaps a highlight on. The knee crosses zero smoothly.
    //
    // This matches brightPassWGSL in render/bundle/bloom.go, which already used
    // the knee. render/bundle/postfx_drift_test.go records the divergence and
    // its brightDivergentTerms row retires when this lands.
    "    let excess = max(brightness - params.threshold, 0.0);",
    "    return vec4f(color * (excess / (excess + 1.0)), 1.0);",
    "}",
  ].join("\n");

  // -----------------------------------------------------------------------
  // shader-f16 in the post chain
  // -----------------------------------------------------------------------
  //
  // Half precision cuts register pressure and lets a GPU issue two lanes of
  // arithmetic per cycle. The win lands on the integrated and mobile parts where
  // GoSX is weakest, and it lands hardest on a shader that is a weighted average
  // of many texture taps.
  //
  // Where it is SAFE here, and why. Every post target this renderer allocates
  // uses targetFormat — the preferred canvas format, an 8-bit UNORM. See
  // ensureFBOs and ensureBloomPingPong. So every value a post shader samples is
  // already quantized to 8 bits in [0, 1]. An f16 carries an 11-bit significand,
  // which strictly exceeds that, and the blur weights sum to 1, so the
  // accumulator never leaves [0, 1] either. Half precision cannot lose a bit the
  // target could have stored.
  //
  // Where it is NOT safe, and why these shaders stay f32:
  //
  //   tone mapping   multiplies by exposure BEFORE clamping, so it is the one
  //                  post stage that handles values above 1 by design.
  //   SSAO and DOF   reconstruct view-space position from depth. A depth value
  //                  near the far plane loses metres of precision in f16.
  //   PBR lighting   sums an unbounded number of lights into an HDR colour, and
  //                  the GGX distribution divides by alpha squared. At roughness
  //                  0.02 that term is 1.6e-7, below the smallest normal f16
  //                  (6.1e-5), so it underflows to zero and the highlight
  //                  vanishes. This is the precision hazard f16 is famous for.
  //
  // vignette, colour grading, the bloom bright pass and the bloom composite are
  // one or two taps each. They are safe, but a single tap is not bandwidth bound,
  // so converting them would add bytes and buy nothing measurable.
  //
  // The variant is a two-line preamble. The body is written once against the
  // aliases, so there is no second copy of the shader to keep in step.
  function sceneWebGPUPostPrecisionPreamble(useF16) {
    if (!useF16) {
      return ["alias pf = f32;", "alias pf3 = vec3f;", ""];
    }
    return ["enable f16;", "alias pf = f16;", "alias pf3 = vec3h;", ""];
  }

  function sceneWebGPUPostShaderSource(bodyLines, useF16) {
    return sceneWebGPUPostPrecisionPreamble(useF16).concat(bodyLines).join("\n");
  }

  var WGSL_POST_BLUR_BODY = [
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
    "    var result = pf3(textureSample(inputTex, inputSamp, uv).rgb) * pf(0.227027);",
    "",
    "    let offsets = array<f32, 4>(1.0, 2.0, 3.0, 4.0);",
    "    let weights = array<pf, 4>(pf(0.1945946), pf(0.1216216), pf(0.054054), pf(0.016216));",
    "    let radiusStep = clamp(params.radius * 0.35, 1.0, 4.0);",
    "",
    "    for (var i = 0u; i < 4u; i = i + 1u) {",
    "        let offset = params.direction * texelSize * offsets[i] * radiusStep;",
    "        result = result + pf3(textureSample(inputTex, inputSamp, uv + offset).rgb) * weights[i];",
    "        result = result + pf3(textureSample(inputTex, inputSamp, uv - offset).rgb) * weights[i];",
    "    }",
    "    return vec4f(vec3f(result), 1.0);",
    "}",
  ];

  var WGSL_POST_BLUR_FRAGMENT = sceneWebGPUPostShaderSource(WGSL_POST_BLUR_BODY, false);
  var WGSL_POST_BLUR_FRAGMENT_F16 = sceneWebGPUPostShaderSource(WGSL_POST_BLUR_BODY, true);

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

  // FXAA 3.11 quality-preset edge anti-aliasing — the chain-end pass. Uses
  // the same 2-binding (texture + sampler) layout as WGSL_POST_BLIT_FRAGMENT
  // (getPostBlitLayout) since FXAA has no tunable uniforms. Algorithm
  // mirrors the native render/bundle FXAA pass (render/bundle/postfx.go,
  // fxaa311WGSLTemplate) for cross-backend parity: green-channel luma edge
  // detection, local contrast search, 2-tap + 2-tap subpixel blend.
  // FXAA reads nine taps and blends four more. Every tap is an 8-bit UNORM in
  // [0, 1] and every blend weight is at most 1, so half precision is exact
  // against the storage format. The edge-direction vector stays f32: it is
  // divided by a reduction term as small as 1/128, and its result is multiplied
  // by the texel size, so a half-precision reciprocal would quantize the sample
  // offsets and shift where FXAA looks.
  var WGSL_POST_FXAA_BODY = [
    "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
    "@group(0) @binding(1) var inputSamp: sampler;",
    "",
    "fn greenLuma(c: pf3) -> pf {",
    "    return c.g;",
    "}",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    let texDim = vec2f(textureDimensions(inputTex));",
    "    let texelSize = 1.0 / texDim;",
    "",
    "    let rgbNW = pf3(textureSample(inputTex, inputSamp, uv + vec2f(-1.0, -1.0) * texelSize).rgb);",
    "    let rgbNE = pf3(textureSample(inputTex, inputSamp, uv + vec2f( 1.0, -1.0) * texelSize).rgb);",
    "    let rgbSW = pf3(textureSample(inputTex, inputSamp, uv + vec2f(-1.0,  1.0) * texelSize).rgb);",
    "    let rgbSE = pf3(textureSample(inputTex, inputSamp, uv + vec2f( 1.0,  1.0) * texelSize).rgb);",
    "    let rgbM  = pf3(textureSample(inputTex, inputSamp, uv).rgb);",
    "",
    "    let lumaNW = greenLuma(rgbNW);",
    "    let lumaNE = greenLuma(rgbNE);",
    "    let lumaSW = greenLuma(rgbSW);",
    "    let lumaSE = greenLuma(rgbSE);",
    "    let lumaM  = greenLuma(rgbM);",
    "",
    "    let lumaMin = min(lumaM, min(min(lumaNW, lumaNE), min(lumaSW, lumaSE)));",
    "    let lumaMax = max(lumaM, max(max(lumaNW, lumaNE), max(lumaSW, lumaSE)));",
    "",
    "    var dir = vec2f(",
    "        -f32((lumaNW + lumaNE) - (lumaSW + lumaSE)),",
    "         f32((lumaNW + lumaSW) - (lumaNE + lumaSE)),",
    "    );",
    "",
    "    let reduceMul = 1.0 / 8.0;",
    "    let reduceMin = 1.0 / 128.0;",
    "    let spanMax = 8.0;",
    "    let dirReduce = max(f32(lumaNW + lumaNE + lumaSW + lumaSE) * (0.25 * reduceMul), reduceMin);",
    "    let rcpDirMin = 1.0 / (min(abs(dir.x), abs(dir.y)) + dirReduce);",
    "    dir = clamp(dir * rcpDirMin, vec2f(-spanMax), vec2f(spanMax)) * texelSize;",
    "",
    "    let rgbA = pf(0.5) * (",
    "        pf3(textureSample(inputTex, inputSamp, uv + dir * (1.0 / 3.0 - 0.5)).rgb) +",
    "        pf3(textureSample(inputTex, inputSamp, uv + dir * (2.0 / 3.0 - 0.5)).rgb));",
    "    let rgbB = rgbA * pf(0.5) + pf(0.25) * (",
    "        pf3(textureSample(inputTex, inputSamp, uv + dir * -0.5).rgb) +",
    "        pf3(textureSample(inputTex, inputSamp, uv + dir *  0.5).rgb));",
    "",
    "    let lumaB = greenLuma(rgbB);",
    "    let color = select(rgbB, rgbA, lumaB < lumaMin || lumaB > lumaMax);",
    "    return vec4f(vec3f(color), 1.0);",
    "}",
  ];

  var WGSL_POST_FXAA_FRAGMENT = sceneWebGPUPostShaderSource(WGSL_POST_FXAA_BODY, false);
  var WGSL_POST_FXAA_FRAGMENT_F16 = sceneWebGPUPostShaderSource(WGSL_POST_FXAA_BODY, true);

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
  // Frustum Plane Extraction (browser-side parity with native cull.go)
  // -----------------------------------------------------------------------
  // extractFrustumPlanesJS + instancePassesCullTest are defined in
  // 11-scene-math.ts (shared by both this WebGPU renderer and 16-scene-webgl.js).
  //
  // This renderer passes scratchSelenaViewProjection (post-depth-remap, WebGPU
  // [0,1] clip convention) so the near=R2 half-depth formula is correct for
  // what the GPU actually clips.

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

  // KTX2 block-texture path.
  //
  // 19a-scene-ktx2.ts publishes window.__gosx_scene3d_ktx2 and ships in the
  // lazily fetched glTF chunk, because only a model asset carries a .ktx2
  // texture. This renderer therefore resolves the reader at call time and
  // falls back to the image path when the chunk is absent.
  //
  // Registering wgpuUploadKTX2Texture on
  // window.__gosx_scene3d_ktx2_texture_loader is what opens the variant swap in
  // 19-scene-gltf.js. Until a renderer registers a loader, the swap keeps
  // serving the PNG or JPEG URI, because a .ktx2 URI in an image element is a
  // broken texture.
  function wgpuIsKTX2URL(url) {
    return typeof url === "string" && /\.ktx2(\?|#|$)/.test(url);
  }

  function wgpuKTX2API() {
    return typeof window !== "undefined" && window.__gosx_scene3d_ktx2
      ? window.__gosx_scene3d_ktx2
      : null;
  }

  function wgpuNotifyTextureSettled(record) {
    if (record && typeof notifySceneTextureLoaded === "function") {
      notifySceneTextureLoaded(record.src, Boolean(record.loaded && !record.failed));
    }
    var generation = record && record.generation;
    if (
      !record ||
      record.disposed ||
      !generation ||
      generation.disposed ||
      typeof generation.onResourceReady !== "function"
    ) {
      return;
    }
    generation.onResourceReady();
  }

  // wgpuUploadKTX2Texture fetches, decodes and uploads one KTX2 container into
  // an existing cache record.
  //
  // Every failure marks the record failed and warns. It never leaves the record
  // in the loaded state with the 1x1 placeholder still bound, because a blank
  // texture that reports success is worse than a visible failure: the three
  // failure paths the reader raises are a fetch that does not return 200, a
  // format this device cannot sample, and a level whose byte length disagrees
  // with its block arithmetic.
  // The signature is (context, url, record), the same shape the WebGL2
  // uploader uses, because both are published on the one gate global.
  function wgpuUploadKTX2Texture(device, url, record) {
    var ktx2 = wgpuKTX2API();
    if (!ktx2 || typeof ktx2.load !== "function" || typeof ktx2.uploadWebGPU !== "function") {
      record.failed = true;
      record.pending = false;
      return Promise.resolve(record);
    }
    return ktx2.load(url).then(function(image) {
      var texture = ktx2.uploadWebGPU(device, image, { label: "gosx.ktx2:" + url });
      if (record.disposed || record.generation && record.generation.disposed) {
        if (texture && typeof texture.destroy === "function") texture.destroy();
        return record;
      }
      // Swap only after the upload returns. A throw inside uploadWebGPU lands
      // in the catch below with the placeholder still bound and failed set.
      if (record.texture && typeof record.texture.destroy === "function") {
        record.texture.destroy();
      }
      record.texture = texture;
      record.view = texture.createView({ dimension: image.faces === 6 ? "cube" : "2d" });
      record.width = image.width;
      record.height = image.height;
      record.faces = image.faces;
      record.levels = image.levels.length;
      record.vkFormat = image.vkFormat;
      record.keyValues = image.keyValues || {};
      record.ktx2 = true;
      record.loaded = true;
      record.pending = false;
      wgpuNotifyTextureSettled(record);
      return record;
    }).catch(function(error) {
      if (record.disposed || record.generation && record.generation.disposed) return record;
      record.failed = true;
      record.pending = false;
      record.error = error && error.message ? error.message : String(error);
      try {
        console.warn("[gosx] KTX2 texture " + url + " failed: " + record.error);
      } catch (_e) {}
      wgpuNotifyTextureSettled(record);
      return record;
    });
  }

  function wgpuTextureDescriptor(raw, url, fallbackRole, fallbackColorSpace) {
    var descriptor = raw && typeof raw === "object" ? raw : {};
    return {
      uri: typeof descriptor.uri === "string" && descriptor.uri.trim() ? descriptor.uri.trim() : String(url || "").trim(),
      role: typeof descriptor.role === "string" ? descriptor.role.trim().toLowerCase() : String(fallbackRole || ""),
      colorSpace: typeof descriptor.colorSpace === "string" && descriptor.colorSpace.trim()
        ? descriptor.colorSpace.trim().toLowerCase()
        : String(fallbackColorSpace || "linear"),
      view: typeof descriptor.view === "string" && descriptor.view.trim() ? descriptor.view.trim().toLowerCase() : "2d",
      format: typeof descriptor.format === "string" ? descriptor.format.trim().toLowerCase() : "",
      mipLevels: Math.max(0, Math.floor(sceneNumber(descriptor.mipLevels, 0))),
      width: Math.max(0, Math.floor(sceneNumber(descriptor.width, 0))),
      height: Math.max(0, Math.floor(sceneNumber(descriptor.height, 0))),
      faces: Math.max(0, Math.floor(sceneNumber(descriptor.faces, 0))),
    };
  }

  function wgpuTextureCacheKey(descriptor) {
    return [
      descriptor.uri,
      descriptor.role,
      descriptor.colorSpace,
      descriptor.view,
      descriptor.format,
      descriptor.width,
      descriptor.height,
      descriptor.faces,
      descriptor.mipLevels,
    ].join("\u0000");
  }

  function wgpuLoadTexture(device, url, cache, rawDescriptor, fallbackRole, fallbackColorSpace) {
    if (!cache) return null;
    var descriptor = wgpuTextureDescriptor(rawDescriptor, url, fallbackRole, fallbackColorSpace);
    var key = descriptor.uri;
    if (!key) return null;
    var cacheKey = wgpuTextureCacheKey(descriptor);
    if (cache.has(cacheKey)) return cache.get(cacheKey);

    // Placeholder: 1x1 white pixel.
    var cube = descriptor.view === "cube";
    var placeholderTex = cube ? wgpuCreatePlaceholderCubeTexture(device) : device.createTexture({
      size: [1, 1, 1],
      format: "rgba8unorm",
      usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST | GPUTextureUsage.RENDER_ATTACHMENT,
    });
    if (!cube) {
      device.queue.writeTexture(
        { texture: placeholderTex },
        new Uint8Array([255, 255, 255, 255]),
        { bytesPerRow: 4 },
        [1, 1, 1]
      );
    }

    var record = {
      texture: placeholderTex,
      view: placeholderTex.createView({ dimension: cube ? "cube" : "2d" }),
      src: key,
      descriptor: descriptor,
      loaded: false,
      pending: true,
      failed: false,
      generation: cache._gosxGeneration || null,
      disposed: false,
    };
    cache.set(cacheKey, record);

    // A .ktx2 URI holds a block-compressed container, not an image an <img>
    // element can decode. Hand it to the KTX2 reader instead. Feeding it to an
    // image element produces a broken texture that still reports success, which
    // is exactly the defect class the upload gate exists to prevent.
    if (wgpuIsKTX2URL(key) && wgpuKTX2API()) {
      wgpuUploadKTX2Texture(device, key, record);
      return record;
    }

    if (cube) {
      record.failed = true;
      record.pending = false;
      record.error = "cube descriptors require a KTX2 upload path";
      wgpuNotifyTextureSettled(record);
    } else if (typeof Image === "function") {
      var image = new Image();
      record.image = image;
      image.onload = function() {
        if (record.disposed || record.generation && record.generation.disposed) return;
        var w = image.width;
        var h = image.height;
        var tex = device.createTexture({
          size: [w, h, 1],
          format: descriptor.colorSpace === "srgb" ? "rgba8unorm-srgb" : "rgba8unorm",
          usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST | GPUTextureUsage.RENDER_ATTACHMENT,
        });
        // Use createImageBitmap for copyExternalImageToTexture.
        if (typeof createImageBitmap === "function") {
          createImageBitmap(image).then(function(bitmap) {
            if (record.disposed || record.generation && record.generation.disposed) {
              tex.destroy();
              if (bitmap && typeof bitmap.close === "function") bitmap.close();
              return;
            }
            device.queue.copyExternalImageToTexture(
              { source: bitmap },
              { texture: tex },
              [w, h]
            );
            record.texture.destroy();
            record.texture = tex;
            record.view = tex.createView();
            record.colorSpace = descriptor.colorSpace === "srgb" ? "srgb" : "linear";
            record.loaded = true;
            record.pending = false;
            if (bitmap && typeof bitmap.close === "function") bitmap.close();
            wgpuNotifyTextureSettled(record);
          }).catch(function() {
            tex.destroy();
            if (record.disposed || record.generation && record.generation.disposed) return;
            record.failed = true;
            record.pending = false;
            wgpuNotifyTextureSettled(record);
          });
        } else {
          record.failed = true;
          record.pending = false;
          wgpuNotifyTextureSettled(record);
        }
      };
      image.onerror = function() {
        if (record.disposed || record.generation && record.generation.disposed) return;
        record.failed = true;
        record.pending = false;
        wgpuNotifyTextureSettled(record);
      };
      image.crossOrigin = "anonymous";
      image.src = key;
    } else {
      record.failed = true;
      record.pending = false;
      wgpuNotifyTextureSettled(record);
    }

    return record;
  }

  function wgpuWaterCubeMapFaceURLs(value) {
    var base = typeof value === "string" ? value.trim() : "";
    if (!base) return null;
    if (base.indexOf("{face}") >= 0) {
      return ["xpos", "xneg", "ypos", "ypos", "zpos", "zneg"].map(function(face) {
        return base.replace("{face}", face);
      });
    }
    if (base.charAt(base.length - 1) !== "/") base += "/";
    return ["xpos.jpg", "xneg.jpg", "ypos.jpg", "ypos.jpg", "zpos.jpg", "zneg.jpg"].map(function(face) {
      return base + face;
    });
  }

  function wgpuCreatePlaceholderCubeTexture(device) {
    var tex = device.createTexture({
      size: [1, 1, 6],
      format: "rgba8unorm",
      dimension: "2d",
      usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST,
    });
    var faces = new Uint8Array([
      150, 190, 210, 255,
      110, 155, 180, 255,
      190, 220, 232, 255,
      190, 220, 232, 255,
      125, 170, 195, 255,
      90, 135, 165, 255,
    ]);
    for (var i = 0; i < 6; i++) {
      device.queue.writeTexture(
        { texture: tex, origin: [0, 0, i] },
        faces.subarray(i * 4, i * 4 + 4),
        { bytesPerRow: 4, rowsPerImage: 1 },
        [1, 1, 1]
      );
    }
    return tex;
  }

  function wgpuLoadCubeTexture(device, value, cache) {
    if (!cache) return null;
    var urls = wgpuWaterCubeMapFaceURLs(value);
    if (!urls) return null;
    var key = "cube:" + urls.join("|");
    if (cache.has(key)) return cache.get(key);

    var placeholder = wgpuCreatePlaceholderCubeTexture(device);
    var record = {
      texture: placeholder,
      view: placeholder.createView({ dimension: "cube" }),
      src: key,
      faces: urls,
      loaded: false,
      pending: true,
      failed: false,
    };
    cache.set(key, record);

    if (typeof Image !== "function" || typeof createImageBitmap !== "function") {
      record.failed = true;
      record.pending = false;
      return record;
    }

    var images = new Array(6);
    var loaded = 0;
    var failed = false;
    function finishIfReady() {
      if (failed || loaded !== 6) return;
      var w = images[0].width;
      var h = images[0].height;
      if (!w || !h) {
        record.failed = true;
        record.pending = false;
        return;
      }
      var tex = device.createTexture({
        size: [w, h, 6],
        format: "rgba8unorm",
        dimension: "2d",
        usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST | GPUTextureUsage.RENDER_ATTACHMENT,
      });
      Promise.all(images.map(function(image) {
        return createImageBitmap(image);
      })).then(function(bitmaps) {
        bitmaps.forEach(function(bitmap, faceIndex) {
          device.queue.copyExternalImageToTexture(
            { source: bitmap },
            { texture: tex, origin: [0, 0, faceIndex] },
            [w, h]
          );
        });
        record.texture.destroy();
        record.texture = tex;
        record.view = tex.createView({ dimension: "cube" });
        record.loaded = true;
        record.pending = false;
      }).catch(function() {
        record.failed = true;
        record.pending = false;
      });
    }
    urls.forEach(function(url, index) {
      var image = new Image();
      image.onload = function() {
        images[index] = image;
        loaded++;
        finishIfReady();
      };
      image.onerror = function() {
        failed = true;
        record.failed = true;
        record.pending = false;
      };
      image.crossOrigin = "anonymous";
      image.src = url;
    });

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
        { binding: 9, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "cube" } },
        { binding: 10, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "cube" } },
        { binding: 11, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
        { binding: 12, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "filtering" } },
        { binding: 13, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
        { binding: 14, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "filtering" } },
      ],
    });
  }

  function wgpuCreateMaterialBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-material",
      entries: [
        { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
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
        { binding: 11, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
        { binding: 12, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
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
        { binding: 0, visibility: GPUShaderStage.VERTEX, buffer: { type: "uniform", hasDynamicOffset: true, minBindingSize: 64 } },
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

  // Cull-path instanced layout: 80-byte InstanceRecord (mat4 @ 0-63, pickData
  // uint32x4 @ 64-79). Location 8 carries pickData (vec4u) instead of the
  // non-cull layout's instanceColor (vec4f). Material color comes from the
  // per-material uniform — fragment does NOT read per-instance vertex color.
  // Matches the native render/bundle/cull.go instanceRecordStride = 80.
  var WGPU_PBR_INSTANCED_CULL_VERTEX_LAYOUT = WGPU_PBR_VERTEX_LAYOUT.concat([
    {
      arrayStride: 80,
      stepMode: "instance",
      attributes: [
        { format: "float32x4", offset: 0,  shaderLocation: 4 },
        { format: "float32x4", offset: 16, shaderLocation: 5 },
        { format: "float32x4", offset: 32, shaderLocation: 6 },
        { format: "float32x4", offset: 48, shaderLocation: 7 },
        { format: "uint32x4",  offset: 64, shaderLocation: 8 },
      ],
    },
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

  // Cull-path pipeline: uses WGPU_PBR_INSTANCED_CULL_VERTEX_LAYOUT (80-byte
  // InstanceRecord with pickData uint32x4 at location 8). The same fragment
  // module is used — no per-instance color from vertex; fragment reads the
  // per-material uniform. Shadow pipeline is NOT added (shadows stay draw-all).
  function wgpuCreatePBRInstancedCullPipeline(device, pipelineLayout, vertexModule, fragmentModule, blendMode, depthWrite, targetFormat, sampleCount) {
    return device.createRenderPipeline({
      label: "gosx-pbr-instanced-cull-" + blendMode,
      layout: pipelineLayout,
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: WGPU_PBR_INSTANCED_CULL_VERTEX_LAYOUT,
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

  // Both shadow pipelines set cullMode "front" and set no frontFace, so the
  // WebGPU default "ccw" stands. 12-scene-geometry.ts and 16c-scene-shared-pbr.ts
  // wind their solids counter-clockwise as seen from outside, so the lit face is
  // front-facing and gets discarded. The map therefore records the far wall of a
  // caster, which pushes the stored depth a caster thickness past the receiver
  // and is the standard mitigation for peter-panning.
  //
  // render/bundle/renderer.go keeps the OPPOSITE face on the native path.
  // render/bundle/shadow_drift_test.go pins both settings and states why they
  // differ. Change either side there, not here alone.
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

  function wgpuIsErrorScopeLifecycleMessage(message) {
    var text = String(message || "").toLowerCase();
    return text.indexOf("poperrorscope") >= 0 && text.indexOf("instance dropped") >= 0;
  }

  // wgpuPopScopedErrorScope pops ONE device error scope.
  //
  // ONLY TWO CALLERS MAY EXIST, and both already do: ensureFBOs' allocation
  // guard below, and the per-frame validation scope
  // (beginWebGPUErrorScope / endWebGPUErrorScope). Do not add a third.
  //
  // The stack this pops is owned by the DEVICE, not by the operation. Six
  // asynchronous pipeline-build sites used to push and pop it from their own
  // .then / .catch — that is, in SETTLE order against a LIFO stack — so two
  // overlapping builds swapped results and one build's error was reported
  // against the other. Measured in Firefox on m31labs.dev: four clean authored
  // points modules with a RESOLVED createRenderPipelineAsync were all marked
  // failed by an error belonging to a compute kernel. Those sites now validate
  // per object (create*PipelineAsync + getCompilationInfo); see the block
  // comment on sceneShaderModuleError in 16b-scene-compute.js.
  //
  // The two remaining callers are safe because neither can interleave with
  // anything: ensureFBOs pushes and pops inside one synchronous block, and the
  // frame scope is guarded against re-entry by pendingWebGPUErrorScope.
  function wgpuPopScopedErrorScope(scopedDevice) {
    if (!scopedDevice || typeof scopedDevice.popErrorScope !== "function") {
      return Promise.resolve(null);
    }
    try {
      return scopedDevice.popErrorScope().then(function(scopeErr) {
        return scopeErr || null;
      }).catch(function(error) {
        var message = error && error.message ? error.message : String(error);
        if (wgpuIsErrorScopeLifecycleMessage(message)) return null;
        return error || new Error(message);
      });
    } catch (error) {
      var message = error && error.message ? error.message : String(error);
      if (wgpuIsErrorScopeLifecycleMessage(message)) {
        return Promise.resolve(null);
      }
      return Promise.resolve(error || new Error(message));
    }
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

  // onAllocationError (optional): called with an error message when the
  // HDR/post-FX render-target allocation below (ensureFBOs) fails — e.g. a
  // GPUOutOfMemoryError on a memory-tight browser. See ensureFBOs' error-scope
  // guard: WebGPU resource creation never throws synchronously, so an
  // allocation failure would otherwise silently poison sceneTex/auxTex/
  // depthTex and get reused (and fail validation) EVERY frame thereafter —
  // this is precisely what a memory-tight-browser "Buffer/Texture is invalid"
  // persistent-error session looks like. Guarding makes the failure a
  // detectable event (reported once, cache invalidated so the next
  // ensureFBOs call retries) instead of a poisoned resource used forever.
  // sceneWebGPUDeviceHasF16 reports whether the device negotiated shader-f16.
  // 16z requests the feature when the page asks for optional features, so the
  // gate already exists; this only reads the result.
  function sceneWebGPUDeviceHasF16(device) {
    var features = device && device.features;
    if (!features) return false;
    if (typeof features.has === "function") return features.has("shader-f16");
    return false;
  }

  // sceneWebGPUPostPrecisionMode names which post variant a device gets.
  // A page can force f32 with window.__gosx_scene3d_webgpu_post_f16 = false,
  // which is the first thing to try if a blur ever looks banded.
  function sceneWebGPUPostPrecisionMode(device) {
    if (typeof window !== "undefined" && window.__gosx_scene3d_webgpu_post_f16 === false) return "f32-forced";
    return sceneWebGPUDeviceHasF16(device) ? "f16" : "f32";
  }

  // packSelenaUniforms is injected by the caller (createSceneWebGPURenderer)
  // because the Selena uniform packer — sceneSelenaUniformData and the
  // sceneSelenaMaterialLayout / sceneSelenaUniformValue helpers it needs — lives
  // inside the RENDERER closure, which is a SIBLING of this factory, not an
  // enclosing scope. Calling it by bare name from here throws ReferenceError.
  // That crash sat undiscovered because the customPost case was unreachable:
  // normalizeScenePostEffect lowercased the kind, so this pass never ran and
  // never reached the uniform upload on the frame after its pipeline resolved.
  function wgpuCreatePostProcessor(device, targetFormat, onAllocationError, packSelenaUniforms) {
    // Resolve the precision variant once per post processor, not per frame.
    var postPrecisionMode = sceneWebGPUPostPrecisionMode(device);
    var postUsesF16 = postPrecisionMode === "f16";
    var disposed = false;
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

    // Same memoization pattern as the renderer's wgpuCachedBindGroup: a bind
    // group stays valid while the layout and every bound resource identity
    // are unchanged, and per-frame recreation churns GPU wrapper objects.
    // The device is fixed for this processor's lifetime, so it needs no
    // check. Resize replaces the texture views, which invalidates naturally.
    var postBindGroupOwners = { blit: {}, effects: new Map() };
    function postCachedBindGroup(owner, layout, entries) {
      var cache = owner.bgCache;
      if (cache && cache.layout === layout && cache.ids.length === entries.length) {
        var match = true;
        for (var ci = 0; ci < entries.length && match; ci++) {
          var res = entries[ci].resource;
          if (cache.ids[ci] !== (res && res.buffer ? res.buffer : res)) match = false;
        }
        if (match) return cache.bg;
      }
      var ids = [];
      for (var ii = 0; ii < entries.length; ii++) {
        var r = entries[ii].resource;
        ids.push(r && r.buffer ? r.buffer : r);
      }
      var bg = device.createBindGroup({ layout: layout, entries: entries });
      owner.bgCache = { layout: layout, ids: ids, bg: bg };
      return bg;
    }
    function postEffectBindGroupOwner(effect) {
      var name = (typeof effect.name === "string" && effect.name) ? effect.name : "custom";
      var owner = postBindGroupOwners.effects.get(name);
      if (!owner) {
        owner = {};
        postBindGroupOwners.effects.set(name, owner);
      }
      return owner;
    }

    // Render-truth chain state, owned by apply() but hoisted here so
    // fullscreenPass -- the ONE function every post pass funnels through --
    // can attribute its dispatch to the effect currently being processed.
    // Counting at the funnel instead of in each switch case means bloom's four
    // internal passes are counted honestly (dispatched=4), and it is impossible
    // to add a new effect case that forgets to report itself.
    var activePostChain = null;
    var activePostIndex = -1;

    // Lazily compiled pipelines and layouts.
    var pipelines = {};
    var postParamsLayout = null;
    var bloomCompositeLayout = null;
    var postBlitLayout = null;
    var ssaoLayout = null;
    // Uniform buffers for post params (reused each frame).
    var postParamBuffers = {};

    // ---- Custom post (Selena kind:"post") ----
    // Per-name async pipeline cache. Keys are "<name>:<wgslPrefix>".
    // Values: { pending: true } | { pipeline, bgl } | { failed: true }
    var customPostPipelineCache = new Map();
    // Per-name failure flag to emit console.warn only once.
    var customPostFailed = new Set();

    // wgpuCreateSelenaPostBGL: @group(0) for the Selena post contract.
    //   binding(0) texture_2d<f32> sceneColor
    //   binding(1) sampler
    //   binding(2) texture_depth_2d sceneDepth
    //   binding(3) sampler
    //   binding(4) uniform UserUniforms  (always present — placeholder 16 bytes when no params)
    var selenaPostBGL = null;
    function getSelenaPostBGL() {
      if (!selenaPostBGL) {
        selenaPostBGL = device.createBindGroupLayout({
          label: "gosx-selena-post",
          entries: [
            { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
            { binding: 1, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, sampler: {} },
            { binding: 2, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "depth" } },
            { binding: 3, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
            { binding: 4, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
          ],
        });
      }
      return selenaPostBGL;
    }

    // depthSampler: non-comparison sampler for sceneDepth (binding 3).
    var depthSampler = null;
    function getDepthSampler() {
      if (!depthSampler) depthSampler = device.createSampler({ magFilter: "nearest", minFilter: "nearest" });
      return depthSampler;
    }

    // buildCustomPostPipelineAsync: async-validates + caches a Selena post pipeline.
    function customPostWGSLModuleSource(effect) {
      var fragment = typeof effect.fragmentWGSL === "string" ? effect.fragmentWGSL.trim() : "";
      var vertex = typeof effect.vertexWGSL === "string" ? effect.vertexWGSL.trim() : "";
      if (fragment && vertex && fragment === vertex) {
        return fragment;
      }
      if (fragment && vertex) {
        return (fragment + "\n" + vertex).trim();
      }
      return (fragment || vertex).trim();
    }

    function buildCustomPostPipelineAsync(effect) {
      var wgsl = customPostWGSLModuleSource(effect);
      if (!wgsl) return null;
      var name = (typeof effect.name === "string" && effect.name) ? effect.name : "custom";
      var cacheKey = name + "\x00" + wgsl;
      var cached = customPostPipelineCache.get(cacheKey);
      if (cached) return cached.failed ? null : cached;

      var pending = { pending: true };
      customPostPipelineCache.set(cacheKey, pending);

      var scopedDevice = device;
      if (!scopedDevice) {
        customPostPipelineCache.delete(cacheKey);
        return null;
      }
      var module = scopedDevice.createShaderModule({ label: "selena-post-" + name, code: wgsl });
      renderTruth().captureShaderInfo(module, "selena-post-" + name);
      var bgl = getSelenaPostBGL();
      var pipelineLayout = scopedDevice.createPipelineLayout({ bindGroupLayouts: [bgl] });

      function markFailed(reason) {
        sceneReportPipelineFailure("post", name, reason);
        if (!customPostFailed.has(name)) {
          console.warn("[gosx] custom post pass '" + name + "' failed validation; becoming identity passthrough.", reason);
          customPostFailed.add(name);
        }
        customPostPipelineCache.set(cacheKey, { failed: true });
      }

      // Validated per object: the promise is the verdict, getCompilationInfo
      // the reason. No device error scope — it would be popped by whichever
      // other async build settled first. See wgpuPopScopedErrorScope.
      scopedDevice.createRenderPipelineAsync({
        label: "gosx-selena-post-" + name,
        layout: pipelineLayout,
        vertex: { module: module, entryPoint: "vertexMain", buffers: [] },
        fragment: {
          module: module,
          entryPoint: "fragmentMain",
          targets: [{ format: targetFormat }],
        },
        primitive: { topology: "triangle-list" },
      }).then(function(pipeline) {
        return sceneShaderModuleError([module]).then(function(compileErr) {
          if (disposed) return;
          if (compileErr) {
            markFailed(compileErr);
            return;
          }
          customPostPipelineCache.set(cacheKey, { pipeline: pipeline, bgl: bgl });
        });
      }).catch(function(err) {
        return sceneShaderModuleError([module]).then(function(compileErr) {
          if (disposed) return;
          markFailed(compileErr || err);
        });
      });
      return null; // pending this frame
    }

    // ensureCustomPostUniformBuffer: 16-byte placeholder when no uniforms, or
    // the Selena-packed uniform block from shaderLayout.
    var customPostUniformBuffers = new Map(); // name → buffer
    function ensureCustomPostUniformBuffer(effect) {
      var name = (typeof effect.name === "string" && effect.name) ? effect.name : "custom";
      var uniformData = typeof packSelenaUniforms === "function"
        ? packSelenaUniforms({ customUniforms: effect.uniforms, shaderLayout: effect.shaderLayout })
        : null;
      if (!uniformData || uniformData.byteLength === 0) {
        uniformData = new Float32Array(4); // 16-byte placeholder
      }
      var existing = customPostUniformBuffers.get(name);
      if (!existing || existing.size < uniformData.byteLength) {
        if (existing) existing.destroy();
        var buf = device.createBuffer({
          size: Math.max(16, uniformData.byteLength),
          usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
          label: "gosx-selena-post-uniforms-" + name,
        });
        customPostUniformBuffers.set(name, buf);
        existing = buf;
      }
      device.queue.writeBuffer(existing, 0, uniformData);
      return existing;
    }

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

      // Guard allocation with an out-of-memory error scope: createTexture()
      // never throws synchronously in WebGPU (a failed allocation still
      // returns a texture object immediately, just an invalid one), so
      // without this the failure would be silent until something tries to
      // USE the invalid texture — and by then it's cached and reused every
      // frame. Nested inside whatever whole-frame scope the caller may
      // already have open (WebGPU error scopes stack); "out-of-memory" is a
      // distinct filter from the "validation" scope the frame loop uses, so
      // this doesn't steal or duplicate that reporting.
      var scopedAlloc = !!(device && typeof device.pushErrorScope === "function");
      if (scopedAlloc) {
        try { device.pushErrorScope("out-of-memory"); } catch (_err) { scopedAlloc = false; }
      }

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

      if (scopedAlloc) {
        wgpuPopScopedErrorScope(device).then(function(error) {
          if (!error || disposed) return;
          // Invalidate so the NEXT ensureFBOs call retries allocation
          // instead of reusing the poisoned textures forever — this frame's
          // views are already handed out and may still render garbage, but
          // every subsequent frame gets a fresh attempt.
          currentWidth = -1;
          currentHeight = -1;
          if (typeof onAllocationError === "function") {
            onAllocationError(error.message || String(error));
          }
        });
      }
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

    function fullscreenPass(encoder, pipeline, bindGroup, targetView, options) {
      var opts = options && typeof options === "object" ? options : {};
      var pass = encoder.beginRenderPass({
        colorAttachments: [{
          view: targetView,
          loadOp: opts.loadOp === "load" ? "load" : "clear",
          storeOp: "store",
          clearValue: opts.clearValue || { r: 0, g: 0, b: 0, a: 1 },
        }],
      });
      if (opts.scissor && opts.scissor.width > 0 && opts.scissor.height > 0 && typeof pass.setScissorRect === "function") {
        pass.setScissorRect(opts.scissor.x, opts.scissor.y, opts.scissor.width, opts.scissor.height);
      }
      pass.setPipeline(pipeline);
      pass.setBindGroup(0, bindGroup);
      pass.draw(4);
      pass.end();
      // Render truth: this is a real, encoded, submitted draw -- the only
      // point in the post chain where "the pixels were written" becomes true.
      if (opts.markTruth !== false && activePostChain && activePostIndex >= 0) {
        renderTruth().mark(activePostChain, activePostIndex, "ok", 1);
      }
    }

    function copyPostTexture(encoder, pipeline, inputView, outputView) {
      var blitBG = device.createBindGroup({
        layout: getPostBlitLayout(),
        entries: [
          { binding: 0, resource: inputView },
          { binding: 1, resource: linearSampler },
        ],
      });
      fullscreenPass(encoder, pipeline, blitBG, outputView, { markTruth: false });
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
        // postChain is the per-effect render-truth record. Built ONLY when the
        // diagnostics tier is on, so production pays one boolean read.
        //
        // Every entry starts at pipeline="missing", dispatched=0 and is
        // upgraded by the switch case that handles it. An effect whose case
        // never runs -- an unknown kind, or the lowercased-kind mismatch that
        // made customPost unreachable for three sessions -- therefore stays
        // visibly dead in the published chain instead of being indistinguishable
        // from a healthy pass. postEffects (the old counter) counts the chain
        // LENGTH and would read "1" in exactly that situation.
        var truth = renderTruth();
        var postTruthOn = truth.enabled();
        var postChain = postTruthOn ? truth.chain(effects) : null;
        var stats = {
          postEffects: effects.length,
          postSSAOPasses: 0,
          postDOFPasses: 0,
          postDOMRegionBoundedPasses: 0,
          postDOMRegionBoundedSkips: 0,
          postDOMRegionBoundedPixels: 0,
          postPrecision: postPrecisionMode,
          postChain: postChain,
        };
        activePostChain = postChain;

        for (var i = 0; i < effects.length; i++) {
          var effect = effects[i];
          var isLast = (i === effects.length - 1);
          var outputView = isLast ? finalView : (currentTexView === sceneTexView ? auxTexView : sceneTexView);
          var passW = isLast ? canvasW : scaledW;
          var passH = isLast ? canvasH : scaledH;
          activePostIndex = i;

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
              var blurPipeline = getPipeline("blur", postUsesF16 ? WGSL_POST_BLUR_FRAGMENT_F16 : WGSL_POST_BLUR_FRAGMENT, getPostParamsLayout());
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
            case SCENE_POST_FXAA: {
              // Chain-end edge AA. Reuses the blit bind group layout
              // (texture + sampler, no uniforms) since FXAA has no params.
              var fxaaPipeline = getPipeline("fxaa", postUsesF16 ? WGSL_POST_FXAA_FRAGMENT_F16 : WGSL_POST_FXAA_FRAGMENT, getPostBlitLayout());
              var fxaaBG = device.createBindGroup({
                layout: getPostBlitLayout(),
                entries: [
                  { binding: 0, resource: currentTexView },
                  { binding: 1, resource: linearSampler },
                ],
              });
              fullscreenPass(encoder, fxaaPipeline, fxaaBG, outputView);
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
            case SCENE_POST_CUSTOM_POST: {
              // Selena post contract: WGSL fullscreen triangle, vertexMain/fragmentMain,
              // @group(0) bindings: 0=sceneColor, 1=sceneColorSampler, 2=sceneDepth,
              //   3=sceneDepthSampler, 4=UserUniforms (16-byte placeholder when absent).
              var cpRes = buildCustomPostPipelineAsync(effect);
              if (!cpRes || cpRes.pending || cpRes.failed) {
                // Not yet compiled (first frame) or failed → identity passthrough.
                // currentTexView is unchanged; the output falls through to the blit.
                //
                // THIS is the branch that produced zero pixels for three
                // sessions while every health attribute read green. Distinguish
                // the three causes explicitly: no WGSL at all (missing), still
                // compiling (pending -- benign for a frame or two, a defect if
                // it persists), or rejected by the browser's own WGSL compiler
                // (failed -- and note that Selena validates with naga while
                // Edge compiles with Tint, so "failed" here on Edge alone is a
                // Tint/naga divergence, not necessarily a bad shader).
                if (postChain) {
                  var cpState = truth.PIPELINE_MISSING;
                  if (cpRes && cpRes.pending) cpState = truth.PIPELINE_PENDING;
                  else if (cpRes && cpRes.failed) cpState = truth.PIPELINE_FAILED;
                  else if (customPostFailed.has((typeof effect.name === "string" && effect.name) ? effect.name : "custom")) {
                    cpState = truth.PIPELINE_FAILED;
                  } else if (customPostWGSLModuleSource(effect)) {
                    cpState = truth.PIPELINE_PENDING;
                  }
                  truth.mark(postChain, i, cpState, 0);
                }
                break;
              }
              var cpUniformBuf = ensureCustomPostUniformBuffer(effect);
              var roi = scenePostDOMRegionPixelBounds(effect, passW, passH);
              if (roi.mode === "union" && !roi.bounds) {
                stats.postDOMRegionBoundedSkips += 1;
                if (postChain) truth.mark(postChain, i, "skipped", 0);
                break;
              }
              var cpBG = postCachedBindGroup(postEffectBindGroupOwner(effect), cpRes.bgl, [
                { binding: 0, resource: currentTexView },
                { binding: 1, resource: linearSampler },
                { binding: 2, resource: depthTexView },
                { binding: 3, resource: getDepthSampler() },
                { binding: 4, resource: { buffer: cpUniformBuf } },
              ]);
              if (roi.mode === "union") {
                copyPostTexture(encoder, blitPipeline, currentTexView, outputView);
                fullscreenPass(encoder, cpRes.pipeline, cpBG, outputView, { loadOp: "load", scissor: roi.bounds });
                stats.postDOMRegionBoundedPasses += 1;
                stats.postDOMRegionBoundedPixels += roi.bounds.width * roi.bounds.height;
              } else {
                fullscreenPass(encoder, cpRes.pipeline, cpBG, outputView);
              }
              currentTexView = outputView;
              break;
            }
            default:
              break;
          }
        }

        // Detach chain attribution BEFORE the final blit: the blit is not an
        // authored effect, and counting it against the last chain slot would
        // make a dead trailing pass look alive -- the precise misreading that
        // let a no-op customPost look like a working one.
        activePostIndex = -1;

        // If no effects matched or we need a final blit.
        if (currentTexView !== finalView) {
          var blitBG = postCachedBindGroup(postBindGroupOwners.blit, getPostBlitLayout(), [
            { binding: 0, resource: currentTexView },
            { binding: 1, resource: linearSampler },
          ]);
          fullscreenPass(encoder, blitPipeline, blitBG, finalView);
        }
        activePostChain = null;
        return stats;
      },

      dispose: function() {
        disposed = true;
        if (sceneTex) sceneTex.destroy();
        if (auxTex) auxTex.destroy();
        if (depthTex) depthTex.destroy();
        if (pingPongA) pingPongA.destroy();
        if (pingPongB) pingPongB.destroy();
        for (var key in postParamBuffers) {
          if (postParamBuffers[key]) postParamBuffers[key].destroy();
        }
        customPostUniformBuffers.forEach(function(buf) { if (buf) buf.destroy(); });
        customPostUniformBuffers.clear();
        customPostPipelineCache.clear();
        customPostFailed.clear();
        if (selenaPostBGL) { selenaPostBGL = null; }
        if (depthSampler) { depthSampler = null; }
        sceneTex = auxTex = depthTex = pingPongA = pingPongB = null;
        currentWidth = 0;
        currentHeight = 0;
        pingPongWidth = 0;
        pingPongHeight = 0;
      },
    };
  }

  // -----------------------------------------------------------------------
  // GPU picking (r32uint ID attachment + async 1x1 readback)
  // -----------------------------------------------------------------------
  //
  // Port of the native design in render/bundle/pick.go. The GPU answers ONE
  // question exactly: which pickable draw covers the pointer pixel. A separate
  // pick pass rasterizes only pickable geometry into an r32uint ID texture with
  // its own depth buffer. A 1x1 copyTextureToBuffer + mapAsync reads the ID
  // back. The frame never blocks on the readback.
  //
  // Deviation from pick.go, stated plainly: pick.go writes the ID as a second
  // color attachment of the main pass. This renderer keeps a SEPARATE pass
  // instead. A second main-pass attachment forces every one of the 31 main-pass
  // pipelines to declare a matching @location(1) u32 fragment output; one miss
  // is a pipeline/pass format mismatch that kills the whole frame. A separate
  // pass has the same result, costs nothing on frames with no queued pick, and
  // needs no edit to the existing pipelines.
  //
  // Parity rule: the ID only resolves IDENTITY. Every geometric field
  // (triangleIndex, uv, localPosition, worldPosition, depth, distance) comes
  // from the SAME shared CPU raycast helpers that 17-scene-input.ts runs on
  // WebGL2 (sceneRaycastPickGroup / sceneRaycastPickInstancedMeshes, exported
  // on window.__gosx_scene3d_api). Both backends therefore return the same hit
  // record shape with the same numbers. See sceneWebGPUPickResolve.

  var SCENE_WEBGPU_PICK_ID_FORMAT = "r32uint";
  // WebGPU requires bytesPerRow of a texture-to-buffer copy to be a multiple
  // of 256. 256 is also the smallest legal copy target, so one row holds the
  // single pixel we read.
  var SCENE_WEBGPU_PICK_ROW_ALIGNMENT = 256;
  // PickUniforms is two mat4x4f values plus four u32: 144 bytes. Each draw
  // gets its own 256-byte slot, because 256 is also the default
  // minUniformBufferOffsetAlignment that a dynamic offset must respect.
  var SCENE_WEBGPU_PICK_SLOT_BYTES = 144;
  // Upper bound on pickable draws per pass. 4096 slots is 1 MiB of uniform
  // space. Instanced meshes cost one slot per MESH, not per instance, so real
  // scenes stay far below this.
  var SCENE_WEBGPU_PICK_MAX_SLOTS = 4096;
  // Mirror of SCENE_PICK_MIN_EXTENT_X / _Y in 17-scene-input.ts. Keep the two
  // in sync; sceneWebGPUPickAllowsObject must accept exactly the objects that
  // sceneObjectAllowsPointerPick accepts, or the pick pass can hide a pickable
  // object behind a non-pickable one.
  var SCENE_WEBGPU_PICK_MIN_EXTENT_X = 0.12;
  var SCENE_WEBGPU_PICK_MIN_EXTENT_Y = 0.08;

  // Declarations shared by the three pick shaders. Keep them in one constant so
  // the vertex and fragment stages cannot drift apart, and so the bundle ships
  // one copy instead of three.
  var WGSL_PICK_OUTPUT_STRUCT = [
    "struct PickOutput {",
    "  @builtin(position) clipPos: vec4f,",
    "  @location(0) @interpolate(flat) id: u32,",
    "};",
  ].join("\n");

  var WGSL_PICK_UNIFORM_STRUCT = [
    "struct PickUniforms {",
    "  viewProjection: mat4x4f,",
    "  baseID: u32,",
    "  _pad0: u32,",
    "  _pad1: u32,",
    "  _pad2: u32,",
    "  modelMatrix: mat4x4f,",
    "};",
    "@group(0) @binding(0) var<uniform> pick: PickUniforms;",
  ].join("\n");

  var WGSL_PICK_VERTEX = [
    WGSL_PICK_UNIFORM_STRUCT,
    WGSL_PICK_OUTPUT_STRUCT,
    "@vertex fn vertexMain(@location(0) position: vec3f) -> PickOutput {",
    "  var out: PickOutput;",
    "  out.clipPos = pick.viewProjection * pick.modelMatrix * vec4f(position, 1.0);",
    "  out.id = pick.baseID;",
    "  return out;",
    "}",
  ].join("\n");

  // Instanced variant. Locations 4-7 carry the per-instance mat4, matching
  // WGPU_SHADOW_INSTANCED_VERTEX_LAYOUT. The ID is baseID + instance_index, so
  // one draw stamps a distinct ID per instance exactly like
  // buildInstancedPickTargets in render/bundle/pick.go.
  var WGSL_PICK_INSTANCED_VERTEX = [
    WGSL_PICK_UNIFORM_STRUCT,
    WGSL_PICK_OUTPUT_STRUCT,
    "struct PickVertexInput {",
    "  @location(0) position: vec3f,",
    "  @location(4) instanceMatrix0: vec4f,",
    "  @location(5) instanceMatrix1: vec4f,",
    "  @location(6) instanceMatrix2: vec4f,",
    "  @location(7) instanceMatrix3: vec4f,",
    "};",
    "@vertex fn vertexMain(in: PickVertexInput, @builtin(instance_index) instance: u32) -> PickOutput {",
    "  let model = mat4x4f(in.instanceMatrix0, in.instanceMatrix1, in.instanceMatrix2, in.instanceMatrix3);",
    "  var out: PickOutput;",
    "  out.clipPos = pick.viewProjection * model * vec4f(in.position, 1.0);",
    "  out.id = pick.baseID + instance;",
    "  return out;",
    "}",
  ].join("\n");

  var WGSL_PICK_FRAGMENT = [
    WGSL_PICK_OUTPUT_STRUCT,
    "@fragment fn fragmentMain(in: PickOutput) -> @location(0) u32 {",
    "  return in.id;",
    "}",
  ].join("\n");

  // cullMode "none" and depthCompare "less-equal" match wgpuCreatePBRPipeline,
  // so the pick pass resolves the same front-most surface the main pass paints.
  // Double-sided rasterization also matches the CPU raycast, which accepts both
  // triangle windings.
  function wgpuCreatePickPipeline(device, pipelineLayout, vertexModule, fragmentModule, vertexLayout, label) {
    return device.createRenderPipeline({
      label: label,
      layout: pipelineLayout,
      vertex: { module: vertexModule, entryPoint: "vertexMain", buffers: vertexLayout },
      fragment: {
        module: fragmentModule,
        entryPoint: "fragmentMain",
        targets: [{ format: SCENE_WEBGPU_PICK_ID_FORMAT }],
      },
      primitive: { topology: "triangle-list", cullMode: "none" },
      depthStencil: { format: "depth24plus", depthWriteEnabled: true, depthCompare: "less-equal" },
    });
  }

  function sceneWebGPUPickSharedAPI() {
    if (typeof window === "undefined") return null;
    return window.__gosx_scene3d_api || null;
  }

  function sceneWebGPUPickSharedFn(name) {
    var api = sceneWebGPUPickSharedAPI();
    var fn = api ? api[name] : null;
    return typeof fn === "function" ? fn : null;
  }

  // Port of sceneBoundsSize in 17-scene-input.ts: absolute extents, largest
  // first.
  function sceneWebGPUPickBoundsExtents(bounds) {
    if (!bounds || typeof bounds !== "object") return [0, 0, 0];
    return [
      Math.abs(sceneNumber(bounds.maxX, 0) - sceneNumber(bounds.minX, 0)),
      Math.abs(sceneNumber(bounds.maxY, 0) - sceneNumber(bounds.minY, 0)),
      Math.abs(sceneNumber(bounds.maxZ, 0) - sceneNumber(bounds.minZ, 0)),
    ].sort(function(a, b) { return b - a; });
  }

  // Port of sceneObjectAllowsPointerPick in 17-scene-input.ts. The pick pass
  // must draw exactly the set that file picks, so a non-pickable object never
  // occludes a pickable one in the ID buffer.
  function sceneWebGPUPickAllowsObject(object) {
    if (!object || object.viewCulled) return false;
    if (typeof object.pickable === "boolean") return object.pickable;
    if (object.kind === "plane") return false;
    var extents = sceneWebGPUPickBoundsExtents(object.bounds);
    return extents[0] > SCENE_WEBGPU_PICK_MIN_EXTENT_X && extents[1] > SCENE_WEBGPU_PICK_MIN_EXTENT_Y;
  }

  // sceneWebGPUPickIDPlan assigns pick IDs over a bundle. ID 0 stays reserved
  // for background, so the first target gets ID 1 — the same convention as
  // buildInstancedPickTargets(meshes, 1) in render/bundle/pick.go.
  //
  // Group order matches sceneRaycastPick in 17-scene-input.ts: mesh objects
  // first, then instanced meshes. Each entry records the group, the index
  // within that group, and the instance span, so a returned ID maps back to one
  // exact draw.
  function sceneWebGPUPickIDPlan(bundle) {
    var plan = { entries: [], meshBases: [], instancedBases: [], nextID: 1 };
    if (!bundle) return plan;

    var meshObjects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];
    for (var m = 0; m < meshObjects.length; m += 1) {
      plan.meshBases[m] = 0;
      var obj = meshObjects[m];
      if (!obj || !sceneWebGPUPickAllowsObject(obj)) continue;
      var vertexCount = Math.floor(sceneNumber(obj.vertexCount, 0));
      if (!Number.isFinite(sceneNumber(obj.vertexOffset, NaN)) || vertexCount <= 0) continue;
      plan.meshBases[m] = plan.nextID;
      plan.entries.push({ id: plan.nextID, group: "mesh", index: m, count: 1, slot: plan.entries.length });
      plan.nextID += 1;
    }

    var instanced = Array.isArray(bundle.instancedMeshes) ? bundle.instancedMeshes : [];
    for (var i = 0; i < instanced.length; i += 1) {
      plan.instancedBases[i] = 0;
      var mesh = instanced[i];
      if (!mesh || !sceneWebGPUPickAllowsObject(mesh)) continue;
      var transforms = mesh.transforms;
      if (!transforms || typeof transforms.length !== "number") continue;
      var count = Math.min(
        Math.max(0, Math.floor(sceneNumber(mesh.count, 0))),
        Math.floor(transforms.length / 16)
      );
      if (count <= 0) continue;
      plan.instancedBases[i] = plan.nextID;
      plan.entries.push({ id: plan.nextID, group: "instanced", index: i, count: count, slot: plan.entries.length });
      plan.nextID += count;
    }
    return plan;
  }

  // sceneWebGPUPickEntryForID finds the plan entry that owns an ID and reports
  // the instance offset inside it.
  function sceneWebGPUPickEntryForID(plan, id) {
    var wanted = Math.floor(sceneNumber(id, 0));
    if (!plan || !Array.isArray(plan.entries) || wanted <= 0) return null;
    for (var i = 0; i < plan.entries.length; i += 1) {
      var entry = plan.entries[i];
      if (!entry) continue;
      if (wanted >= entry.id && wanted < entry.id + entry.count) {
        return { entry: entry, instanceIndex: wanted - entry.id };
      }
    }
    return null;
  }

  // sceneWebGPUPickSingleInstanceMesh clones a mesh down to the one instance
  // the GPU selected. sceneRaycastPickInstancedMeshes then computes the hit for
  // that instance with the identical math WebGL2 uses, instead of re-running
  // its nearest-instance search (which uses a sphere approximation and can
  // disagree with the exact rasterized answer).
  function sceneWebGPUPickSingleInstanceMesh(mesh, instanceIndex) {
    var transforms = mesh && mesh.transforms;
    var base = instanceIndex * 16;
    if (!transforms || typeof transforms.length !== "number" || base + 15 >= transforms.length) {
      return null;
    }
    var slice = new Float32Array(16);
    for (var k = 0; k < 16; k += 1) {
      slice[k] = sceneNumber(transforms[base + k], 0);
    }
    var clone = Object.create(Object.getPrototypeOf(mesh) || Object.prototype);
    for (var key in mesh) {
      if (Object.prototype.hasOwnProperty.call(mesh, key)) clone[key] = mesh[key];
    }
    clone.count = 1;
    clone.transforms = slice;
    return clone;
  }

  // Port of sceneNearestRaycastHit in 17-scene-input.ts.
  function sceneWebGPUPickNearestHit(current, candidate) {
    if (!candidate) return current;
    if (!current || candidate.distance < current.distance) return candidate;
    return current;
  }

  // sceneWebGPUPickRefineHit turns an ID into the full hit record by running
  // the shared CPU raycast on the ONE target the GPU selected.
  function sceneWebGPUPickRefineHit(bundle, plan, id, ray) {
    var found = sceneWebGPUPickEntryForID(plan, id);
    if (!found || !ray) return null;
    var entry = found.entry;

    if (entry.group === "mesh") {
      var raycastGroup = sceneWebGPUPickSharedFn("sceneRaycastPickGroup");
      var meshObjects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
      var obj = meshObjects[entry.index];
      if (!raycastGroup || !obj) return null;
      return raycastGroup(ray, [obj], bundle.worldMeshPositions, entry.index, bundle.worldMeshUVs);
    }

    var raycastInstanced = sceneWebGPUPickSharedFn("sceneRaycastPickInstancedMeshes");
    var instanced = Array.isArray(bundle && bundle.instancedMeshes) ? bundle.instancedMeshes : [];
    var mesh = instanced[entry.index];
    if (!raycastInstanced || !mesh) return null;
    var single = sceneWebGPUPickSingleInstanceMesh(mesh, found.instanceIndex);
    if (!single) return null;
    var hit = raycastInstanced(ray, [single], entry.index);
    if (!hit) return null;
    // Report the real mesh and the real instance index, not the single-instance
    // clone the raycast ran against.
    hit.object = mesh;
    hit.instanceIndex = found.instanceIndex;
    return hit;
  }

  // sceneWebGPUPickResolve produces the final hit record for a read-back ID.
  // It returns exactly what sceneRaycastPick returns on WebGL2: the same
  // fields, from the same helpers, plus `ray`. null means background.
  //
  // The third CPU group (bundle.objects over bundle.worldPositions) is merged
  // in on the CPU. The WebGPU renderer rasterizes bundle.worldPositions as a
  // line list, never as triangles, so that group has no ID-buffer draw to
  // resolve. Merging by nearest distance keeps coverage identical to
  // sceneRaycastPick.
  function sceneWebGPUPickResolve(bundle, plan, id, ray) {
    if (!bundle || !ray) return null;
    var hit = sceneWebGPUPickRefineHit(bundle, plan, id, ray);
    var raycastGroup = sceneWebGPUPickSharedFn("sceneRaycastPickGroup");
    if (raycastGroup && Array.isArray(bundle.objects) && bundle.objects.length > 0) {
      hit = sceneWebGPUPickNearestHit(
        hit,
        raycastGroup(ray, bundle.objects, bundle.worldPositions, 0, null)
      );
    }
    if (hit) hit.ray = ray;
    return hit;
  }

  function sceneWebGPUPickSnapshotBundle(bundle) {
    if (!bundle || !Array.isArray(bundle.meshObjects)) return bundle;
    var copy = Object.assign({}, bundle);
    copy.meshObjects = bundle.meshObjects.map(function(obj) {
      if (!obj || !obj.retainedGeometry || !obj.modelMatrix || obj.modelMatrix.length < 16) {
        return obj;
      }
      return Object.assign({}, obj, { modelMatrix: new Float32Array(obj.modelMatrix) });
    });
    return copy;
  }

  // createSceneWebGPUPicker owns the pick textures, pipelines, and the single
  // in-flight readback. The adapter supplies the renderer-scoped closures the
  // pick pass needs; keeping the picker at module scope stops
  // createSceneWebGPURenderer from growing further.
  //
  // adapter fields:
  //   viewProjection()          -> Float32Array(16), WebGPU-convention VP
  //   meshPositions()           -> { buffer, components } or null
  //   bindMeshPositions(...)    -> bool, binds a vertex slice by vertex range
  //   instancedGeometry(mesh)   -> { positions, vertexCount, ... } or null
  //   instancedGeometryBuffer(geom, slot, data) -> GPUBuffer
  //   instancedTransformBuffer(mesh, data)      -> GPUBuffer
  //   instancedCount(mesh)      -> int
  //   instancedTransforms(mesh, count) -> Float32Array or null
  //   onError(message)          -> void
  function createSceneWebGPUPicker(device, adapter) {
    if (!device || !adapter) return null;

    var vertexModule = null;
    var instancedVertexModule = null;
    var fragmentModule = null;
    var bindGroupLayout = null;
    var pipelineLayout = null;
    var pipeline = null;
    var instancedPipeline = null;
    var uniformBuffer = null;
    var bindGroup = null;
    var idTexture = null;
    var idView = null;
    var depthTexture = null;
    var depthView = null;
    var idWidth = 0;
    var idHeight = 0;
    var initFailed = false;
    var uniformSlots = 0;
    var uniformData = null;
    var uniformIDs = null;
    var pending = null;
    // Superseded requests whose copy is already on the GPU. Their callbacks
    // never fire, but their staging buffers must still be mapped and released
    // or the buffer leaks. Mirrors retiredPicks in render/bundle/pick.go.
    var retired = [];
    var stats = { requests: 0, drops: 0, passes: 0, draws: 0, readbacks: 0, skipped: 0, lastID: 0, lastError: "" };

    function ensureResources() {
      if (initFailed) return false;
      if (pipeline && instancedPipeline) return true;
      try {
        vertexModule = device.createShaderModule({ label: "pick-vert", code: WGSL_PICK_VERTEX });
        instancedVertexModule = device.createShaderModule({ label: "pick-instanced-vert", code: WGSL_PICK_INSTANCED_VERTEX });
        fragmentModule = device.createShaderModule({ label: "pick-frag", code: WGSL_PICK_FRAGMENT });
        // hasDynamicOffset is what makes one buffer serve every draw. Without
        // it each draw would need its own writeBuffer, and because
        // queue.writeBuffer lands BEFORE the submitted commands run, every draw
        // in the pass would read the LAST baseID written. One slot per draw plus
        // a dynamic offset is the only correct single-buffer form.
        bindGroupLayout = device.createBindGroupLayout({
          label: "gosx-pick-frame",
          entries: [{
            binding: 0,
            visibility: GPUShaderStage.VERTEX,
            buffer: { type: "uniform", hasDynamicOffset: true, minBindingSize: SCENE_WEBGPU_PICK_SLOT_BYTES },
          }],
        });
        pipelineLayout = device.createPipelineLayout({ bindGroupLayouts: [bindGroupLayout] });
        pipeline = wgpuCreatePickPipeline(
          device, pipelineLayout, vertexModule, fragmentModule,
          WGPU_SHADOW_VERTEX_LAYOUT, "gosx-pick"
        );
        instancedPipeline = wgpuCreatePickPipeline(
          device, pipelineLayout, instancedVertexModule, fragmentModule,
          WGPU_SHADOW_INSTANCED_VERTEX_LAYOUT, "gosx-pick-instanced"
        );
        return true;
      } catch (err) {
        initFailed = true;
        stats.lastError = String(err && (err.message || err) || "pick-init-failed");
        if (typeof adapter.onError === "function") adapter.onError(stats.lastError);
        return false;
      }
    }

    function ensureTargets(width, height) {
      if (idTexture && idWidth === width && idHeight === height) return true;
      if (idTexture) idTexture.destroy();
      if (depthTexture) depthTexture.destroy();
      idTexture = depthTexture = null;
      idView = depthView = null;
      idWidth = idHeight = 0;
      try {
        idTexture = device.createTexture({
          label: "gosx-pick-id",
          size: [width, height, 1],
          format: SCENE_WEBGPU_PICK_ID_FORMAT,
          usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.COPY_SRC,
        });
        depthTexture = device.createTexture({
          label: "gosx-pick-depth",
          size: [width, height, 1],
          format: "depth24plus",
          usage: GPUTextureUsage.RENDER_ATTACHMENT,
        });
        idView = idTexture.createView();
        depthView = depthTexture.createView();
        idWidth = width;
        idHeight = height;
        return true;
      } catch (err) {
        stats.lastError = String(err && (err.message || err) || "pick-target-failed");
        if (typeof adapter.onError === "function") adapter.onError(stats.lastError);
        return false;
      }
    }

    // ensureUniformSlots grows the uniform buffer to hold one 256-byte slot per
    // plan entry, and rebuilds the bind group to match. Returns the slot count
    // the buffer can serve; entries past it are skipped and counted.
    function ensureUniformSlots(wanted) {
      var need = Math.max(1, Math.min(wanted, SCENE_WEBGPU_PICK_MAX_SLOTS));
      if (uniformBuffer && uniformSlots >= need) return uniformSlots;
      if (uniformBuffer) uniformBuffer.destroy();
      uniformBuffer = device.createBuffer({
        label: "gosx-pick-uniforms",
        size: need * SCENE_WEBGPU_PICK_ROW_ALIGNMENT,
        usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
      });
      bindGroup = device.createBindGroup({
        layout: bindGroupLayout,
        entries: [{
          binding: 0,
          resource: { buffer: uniformBuffer, offset: 0, size: SCENE_WEBGPU_PICK_ROW_ALIGNMENT },
        }],
      });
      uniformSlots = need;
      uniformData = new Float32Array(need * SCENE_WEBGPU_PICK_ROW_ALIGNMENT / 4);
      uniformIDs = new Uint32Array(uniformData.buffer);
      return uniformSlots;
    }

    // uploadPickUniforms fills every slot and uploads them in ONE writeBuffer,
    // before the pass begins. Slot i holds the view-projection matrix and the
    // base ID of plan entry i.
    function uploadPickUniforms(plan, viewProjection, bundle) {
      var slots = ensureUniformSlots(plan.entries.length);
      var floatsPerSlot = SCENE_WEBGPU_PICK_ROW_ALIGNMENT / 4;
      var used = Math.min(plan.entries.length, slots);
      for (var i = 0; i < used; i += 1) {
        var base = i * floatsPerSlot;
        if (viewProjection && viewProjection.length >= 16) {
          for (var k = 0; k < 16; k += 1) uniformData[base + k] = viewProjection[k];
        }
        uniformIDs[base + 16] = plan.entries[i].id >>> 0;
        uniformIDs[base + 17] = 0;
        uniformIDs[base + 18] = 0;
        uniformIDs[base + 19] = 0;
        var entry = plan.entries[i];
        var meshObjects = bundle && Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];
        var obj = entry && entry.group === "mesh" ? meshObjects[entry.index] : null;
        var model = obj && obj.retainedGeometry && obj.modelMatrix && obj.modelMatrix.length >= 16
          ? obj.modelMatrix
          : null;
        for (var mk = 0; mk < 16; mk++) {
          uniformData[base + 20 + mk] = model
            ? sceneNumber(model[mk], mk % 5 === 0 ? 1 : 0)
            : (mk % 5 === 0 ? 1 : 0);
        }
      }
      if (plan.entries.length > slots) stats.skipped += plan.entries.length - slots;
      device.queue.writeBuffer(uniformBuffer, 0, uniformData, 0, used * floatsPerSlot);
      return used;
    }

    // queuePick records a request in the caller's pointer space. x, y, width,
    // and height must be the SAME values the caller feeds sceneScreenToRay, so
    // the GPU pixel and the CPU ray agree. Only one pick may wait at a time;
    // a newer request replaces an older one that has not been submitted yet,
    // matching QueuePickResult in render/bundle/pick.go.
    function queuePick(x, y, width, height, callback) {
      if (typeof callback !== "function") return false;
      stats.requests += 1;
      if (pending) {
        stats.drops += 1;
        pending.superseded = true;
        if (pending.submitted && !pending.reading) {
          // The GPU already owns this copy. Keep the request so finishReadback
          // still maps and frees its staging buffer.
          retired.push(pending);
        } else if (!pending.submitted && pending.staging) {
          try { pending.staging.destroy(); } catch (err) { void err; }
        }
        // A request already reading owns its own cleanup.
      }
      pending = {
        x: sceneNumber(x, 0),
        y: sceneNumber(y, 0),
        width: Math.max(1, sceneNumber(width, 1)),
        height: Math.max(1, sceneNumber(height, 1)),
        callback: callback,
        submitted: false,
        superseded: false,
        reading: false,
        staging: null,
        bundle: null,
        plan: null,
        ray: null,
      };
      return true;
    }

    function failPending(request) {
      if (!request || request.superseded) return;
      if (pending === request) pending = null;
      request.callback(null);
    }

    // recordPickPass draws the pickable geometry of `bundle` into the ID
    // texture and records the 1x1 copy. Call it with the frame encoder AFTER
    // the main pass has ended, so every vertex and transform buffer exists.
    // targetWidth/targetHeight are the render-target size in device pixels.
    function recordPickPass(encoder, bundle, targetWidth, targetHeight) {
      var request = pending;
      if (!request || request.submitted || !encoder || !bundle) return false;
      var deviceWidth = Math.max(1, Math.floor(targetWidth));
      var deviceHeight = Math.max(1, Math.floor(targetHeight));

      var scaleX = deviceWidth / request.width;
      var scaleY = deviceHeight / request.height;
      var px = Math.floor(request.x * scaleX);
      var py = Math.floor(request.y * scaleY);
      if (px < 0 || py < 0 || px >= deviceWidth || py >= deviceHeight) {
        // Outside the surface. Report background, exactly like the
        // out-of-bounds branch of recordPickCopy in render/bundle/pick.go.
        failPending(request);
        return false;
      }

      var plan = sceneWebGPUPickIDPlan(bundle);
      var screenToRay = sceneWebGPUPickSharedFn("sceneScreenToRay");
      if (!screenToRay) {
        failPending(request);
        return false;
      }
      var ray = screenToRay(request.x, request.y, request.width, request.height, bundle.camera);
      if (!ensureResources() || !ensureTargets(deviceWidth, deviceHeight)) {
        failPending(request);
        return false;
      }

      var staging = null;
      try {
        staging = device.createBuffer({
          label: "gosx-pick-staging",
          size: SCENE_WEBGPU_PICK_ROW_ALIGNMENT,
          usage: GPUBufferUsage.MAP_READ | GPUBufferUsage.COPY_DST,
        });
      } catch (err) {
        stats.lastError = String(err && (err.message || err) || "pick-staging-failed");
        failPending(request);
        return false;
      }

      // Fill every uniform slot BEFORE the pass. queue.writeBuffer lands ahead
      // of the submitted commands, so per-draw writes would all collapse onto
      // the last value.
      var slots = uploadPickUniforms(
        plan,
        typeof adapter.viewProjection === "function" ? adapter.viewProjection() : null,
        bundle
      );

      var pass = encoder.beginRenderPass({
        label: "gosx-pick-pass",
        colorAttachments: [{
          view: idView,
          loadOp: "clear",
          storeOp: "store",
          clearValue: { r: 0, g: 0, b: 0, a: 0 },
        }],
        depthStencilAttachment: {
          view: depthView,
          depthLoadOp: "clear",
          depthClearValue: 1.0,
          depthStoreOp: "discard",
        },
      });
      // Only the pointer pixel is ever read back, so scissor the pass to it.
      // Vertex work still runs, fragment work does not.
      pass.setScissorRect(px, py, 1, 1);
      var draws = drawPickPass(pass, bundle, plan, slots);
      pass.end();

      encoder.copyTextureToBuffer(
        { texture: idTexture, origin: { x: px, y: py, z: 0 } },
        { buffer: staging, bytesPerRow: SCENE_WEBGPU_PICK_ROW_ALIGNMENT, rowsPerImage: 1 },
        { width: 1, height: 1, depthOrArrayLayers: 1 }
      );

      request.staging = staging;
      // Retained objects intentionally reuse one compact model-matrix array
      // across animation frames. Pick readback completes later, so snapshot
      // only those 64-byte matrices here; otherwise CPU hit refinement could
      // observe a newer transform than the GPU ID pass rasterized.
      request.bundle = sceneWebGPUPickSnapshotBundle(bundle);
      request.plan = plan;
      request.ray = ray;
      request.submitted = true;
      stats.passes += 1;
      stats.draws += draws;
      return true;
    }

    // drawPickPass records one draw per plan entry, each bound to its own
    // uniform slot through a dynamic offset. `slots` is the number of entries
    // the uniform buffer can serve; entries past it are dropped, not misdrawn.
    function drawPickPass(pass, bundle, plan, slots) {
      var draws = 0;
      var meshRecord = typeof adapter.meshPositions === "function" ? adapter.meshPositions() : null;
      var meshObjects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];
      var instanced = Array.isArray(bundle.instancedMeshes) ? bundle.instancedMeshes : [];
      var boundPipeline = "";

      for (var e = 0; e < plan.entries.length && e < slots; e += 1) {
        var entry = plan.entries[e];
        if (!entry) continue;
        var offset = [entry.slot * SCENE_WEBGPU_PICK_ROW_ALIGNMENT];

        if (entry.group === "mesh") {
          var obj = meshObjects[entry.index];
          if (!obj) continue;
          var count = Math.floor(sceneNumber(obj.vertexCount, 0));
          if (count <= 0) continue;
          if (boundPipeline !== "mesh") {
            pass.setPipeline(pipeline);
            boundPipeline = "mesh";
          }
          pass.setBindGroup(0, bindGroup, offset);
          if (!adapter.bindMeshPositions(pass, 0, meshRecord, sceneNumber(obj.vertexOffset, 0), count, obj)) continue;
          pass.draw(count);
          draws += 1;
          continue;
        }

        var mesh = instanced[entry.index];
        if (!mesh) continue;
        var instanceCount = adapter.instancedCount(mesh);
        var transformData = adapter.instancedTransforms(mesh, instanceCount);
        if (!transformData) continue;
        var geom = adapter.instancedGeometry(mesh);
        if (!geom || geom.vertexCount <= 0) continue;
        if (boundPipeline !== "instanced") {
          pass.setPipeline(instancedPipeline);
          boundPipeline = "instanced";
        }
        pass.setBindGroup(0, bindGroup, offset);
        pass.setVertexBuffer(0, adapter.instancedGeometryBuffer(geom, "_gosxWGPUInstancedShadowPositionBuffer", geom.positions));
        pass.setVertexBuffer(1, adapter.instancedTransformBuffer(mesh, transformData));
        // Draw every instance in one call so occlusion between a mesh's own
        // instances is right. The shader stamps baseID + instance_index, so the
        // read-back ID still names one exact instance.
        pass.draw(geom.vertexCount, Math.min(instanceCount, entry.count));
        draws += 1;
      }
      return draws;
    }

    // finishReadback starts the async map for a submitted request. It never
    // blocks: mapAsync resolves on a later task, and the callback runs then.
    // Call it right after device.queue.submit.
    function finishReadback() {
      // Drain superseded requests first so their staging buffers are freed even
      // though nothing consumes their result.
      if (retired.length > 0) {
        var stale = retired;
        retired = [];
        for (var i = 0; i < stale.length; i += 1) startReadback(stale[i]);
      }
      return startReadback(pending);
    }

    function startReadback(request) {
      if (!request || !request.submitted || !request.staging || request.reading) return false;
      request.reading = true;
      var staging = request.staging;
      staging.mapAsync(GPUMapMode.READ).then(function() {
        var id = 0;
        try {
          id = new Uint32Array(staging.getMappedRange(0, 4).slice(0))[0] >>> 0;
        } catch (err) {
          stats.lastError = String(err && (err.message || err) || "pick-map-failed");
        }
        try { staging.unmap(); } catch (unmapErr) { void unmapErr; }
        staging.destroy();
        stats.readbacks += 1;
        stats.lastID = id;
        if (request.superseded) return;
        if (pending === request) pending = null;
        request.callback(sceneWebGPUPickResolve(request.bundle, request.plan, id, request.ray));
      }).catch(function(err) {
        stats.lastError = String(err && (err.message || err) || "pick-readback-failed");
        try { staging.destroy(); } catch (destroyErr) { void destroyErr; }
        if (request.superseded) return;
        if (pending === request) pending = null;
        request.callback(null);
      });
      return true;
    }

    function hasPending() {
      return Boolean(pending);
    }

    function diagnostics() {
      return {
        pickRequests: stats.requests,
        pickDrops: stats.drops,
        pickPasses: stats.passes,
        pickDraws: stats.draws,
        pickSkippedDraws: stats.skipped,
        pickReadbacks: stats.readbacks,
        pickLastID: stats.lastID,
        pickLastError: stats.lastError,
      };
    }

    function dispose() {
      var abandoned = retired.concat(pending ? [pending] : []);
      for (var i = 0; i < abandoned.length; i += 1) {
        var request = abandoned[i];
        request.superseded = true;
        // A request already reading owns its own destroy in the map handler.
        if (request.staging && !request.reading) {
          try { request.staging.destroy(); } catch (err) { void err; }
        }
      }
      retired = [];
      pending = null;
      if (idTexture) idTexture.destroy();
      if (depthTexture) depthTexture.destroy();
      if (uniformBuffer) uniformBuffer.destroy();
      idTexture = depthTexture = uniformBuffer = null;
      idView = depthView = bindGroup = null;
      pipeline = instancedPipeline = null;
      uniformData = uniformIDs = null;
      idWidth = idHeight = uniformSlots = 0;
    }

    return {
      queuePick: queuePick,
      recordPickPass: recordPickPass,
      finishReadback: finishReadback,
      hasPending: hasPending,
      diagnostics: diagnostics,
      dispose: dispose,
    };
  }

  // Publish the pure pick helpers so tests and tooling can check them against
  // the WebGL2 pick contract without a GPU.
  if (typeof window !== "undefined" && window.__gosx_scene3d_api) {
    window.__gosx_scene3d_api.sceneWebGPUPickIDPlan = sceneWebGPUPickIDPlan;
    window.__gosx_scene3d_api.sceneWebGPUPickEntryForID = sceneWebGPUPickEntryForID;
    window.__gosx_scene3d_api.sceneWebGPUPickAllowsObject = sceneWebGPUPickAllowsObject;
    window.__gosx_scene3d_api.sceneWebGPUPickResolve = sceneWebGPUPickResolve;
    window.__gosx_scene3d_api.createSceneWebGPUPicker = createSceneWebGPUPicker;
  }

  // -----------------------------------------------------------------------
  // Lights
  // -----------------------------------------------------------------------
  //
  // GoSX declares seven light kinds. This renderer shades six of them
  // directly and folds LightProbe into the ambient term:
  //
  //   ambient      -> code 0, flat term
  //   light-probe  -> code 0, flat term (coefficients ignored)
  //   directional  -> code 1, plus the two shadow slots
  //   point        -> code 2, distance falloff
  //   spot         -> code 3, cone falloff times distance falloff
  //   hemisphere   -> code 4, sky/ground blend by normal Y
  //   rect-area    -> code 5, polygon form factor plus representative-point
  //                   specular
  //
  // Codes 0..4 are the same numbers the WebGL2 renderer writes, so a scene
  // reads the same type on both GPU backends.

  // One Light is 7 * vec4f. Keep in step with the WGSL struct.
  var SCENE_WEBGPU_LIGHT_FLOATS = 28;
  var SCENE_WEBGPU_LIGHT_BYTES = SCENE_WEBGPU_LIGHT_FLOATS * 4;

  // The storage buffer starts at 8 lights and doubles on demand. WGSL bounds
  // the loop with arrayLength(&lights), so the cap below only limits how much
  // memory one scene may claim. Exceeding it reports through reportIssue
  // instead of dropping lights in silence.
  var SCENE_WEBGPU_LIGHT_CAPACITY_MIN = 8;
  var SCENE_WEBGPU_LIGHT_CAPACITY_MAX = 256;

  // sceneWebGPULightCapacityFor returns the storage capacity that holds count
  // lights: a power of two, at least the minimum, never past the maximum.
  function sceneWebGPULightCapacityFor(count) {
    var wanted = Math.max(0, Math.floor(Number(count) || 0));
    var capacity = SCENE_WEBGPU_LIGHT_CAPACITY_MIN;
    while (capacity < wanted && capacity < SCENE_WEBGPU_LIGHT_CAPACITY_MAX) {
      capacity = capacity * 2;
    }
    return Math.min(capacity, SCENE_WEBGPU_LIGHT_CAPACITY_MAX);
  }

  // sceneWebGPULightTypeCode maps a light IR kind to its WGSL type code.
  //
  // light-probe maps to ambient, not point. A LightProbe carries no position,
  // so a point light would invent a falloff the author never asked for. The
  // WebGL2 renderer makes the same choice, which keeps the two backends equal.
  // Neither renderer reads LightProbe.Coefficients; see the light-probe-sh
  // cell in scene/capability/capability.go.
  function sceneWebGPULightTypeCode(kind) {
    switch (kind) {
      case "ambient": return 0;
      case "light-probe": return 0;
      case "directional": return 1;
      case "point": return 2;
      case "spot": return 3;
      case "hemisphere": return 4;
      case "rect-area": return 5;
      default: return 2;
    }
  }

  // sceneWebGPUCachedColor parses a light color once per distinct string.
  function sceneWebGPUCachedColor(value, fallback, cache) {
    var rgba = typeof value === "string" && cache ? cache[value] : null;
    if (rgba) {
      return rgba;
    }
    rgba = sceneColorRGBA(value, fallback);
    if (typeof value === "string" && cache) {
      cache[value] = rgba;
    }
    return rgba;
  }

  // sceneWebGPURectAreaBasis writes the world-space half-width and half-height
  // vectors of a rect-area light into out[0..5].
  //
  // GoSX authors a RectAreaLight as a position, an emission direction, a width
  // and a height. The WGSL form factor needs the two in-plane edge vectors, so
  // build an orthonormal frame around the direction here, once per light per
  // frame, instead of per fragment.
  //
  // Sign convention: three.js lights the side where
  // dot(cross(halfWidth, halfHeight), P - center) < 0. Negating the up vector
  // puts that lit side on the +direction side, so a rect-area light shines the
  // way its Direction points, like every other GoSX light.
  function sceneWebGPURectAreaBasis(light, out) {
    var nx = sceneNumber(light && light.directionX, 0);
    var ny = sceneNumber(light && light.directionY, -1);
    var nz = sceneNumber(light && light.directionZ, 0);
    var nlen = Math.sqrt((nx * nx) + (ny * ny) + (nz * nz));
    if (!(nlen > 0.000001)) {
      nx = 0; ny = -1; nz = 0; nlen = 1;
    }
    nx = nx / nlen; ny = ny / nlen; nz = nz / nlen;

    // Pick the world axis least aligned with the normal so the cross product
    // stays well conditioned.
    var ax = 0, ay = 0, az = 1;
    if (Math.abs(nz) > 0.9) {
      ax = 1; ay = 0; az = 0;
    }
    var rx = (ay * nz) - (az * ny);
    var ry = (az * nx) - (ax * nz);
    var rz = (ax * ny) - (ay * nx);
    var rlen = Math.sqrt((rx * rx) + (ry * ry) + (rz * rz));
    if (!(rlen > 0.000001)) {
      rx = 1; ry = 0; rz = 0; rlen = 1;
    }
    rx = rx / rlen; ry = ry / rlen; rz = rz / rlen;

    var ux = (ny * rz) - (nz * ry);
    var uy = (nz * rx) - (nx * rz);
    var uz = (nx * ry) - (ny * rx);

    var halfW = Math.max(0, sceneNumber(light && light.width, 1)) * 0.5;
    var halfH = Math.max(0, sceneNumber(light && light.height, 1)) * 0.5;
    out[0] = rx * halfW;
    out[1] = ry * halfW;
    out[2] = rz * halfW;
    out[3] = -ux * halfH;
    out[4] = -uy * halfH;
    out[5] = -uz * halfH;
    return out;
  }

  var _sceneWebGPURectBasis = new Float32Array(6);

  // sceneWebGPUPackLights writes count lights into out as SCENE_WEBGPU_LIGHT_FLOATS
  // floats each, in the WGSL Light layout. It returns a census the caller turns
  // into author diagnostics.
  //
  // Defaults match the WebGL2 renderer field for field, so neither backend
  // invents a value the other does not have. That includes angle: an unset
  // cone angle stays 0 on both backends, which draws nothing. The caller
  // reports that case instead of silently widening the cone here.
  function sceneWebGPUPackLights(lightArray, count, out, colorCache) {
    var census = {
      count: 0,
      spot: 0,
      spotEmptyCone: 0,
      hemisphere: 0,
      rectArea: 0,
      lightProbe: 0,
      lightProbeWithCoefficients: 0,
    };
    var list = Array.isArray(lightArray) ? lightArray : [];
    var limit = Math.max(0, Math.min(Math.floor(Number(count) || 0), list.length));
    for (var i = 0; i < limit; i++) {
      var light = list[i] || {};
      var kind = typeof light.kind === "string" ? light.kind.toLowerCase() : "";
      var typeCode = sceneWebGPULightTypeCode(kind);
      var base = i * SCENE_WEBGPU_LIGHT_FLOATS;

      // position.xyz + type code
      out[base + 0] = sceneNumber(light.x, 0);
      out[base + 1] = sceneNumber(light.y, 0);
      out[base + 2] = sceneNumber(light.z, 0);
      out[base + 3] = typeCode;

      // direction.xyz + intensity
      out[base + 4] = sceneNumber(light.directionX, 0);
      out[base + 5] = sceneNumber(light.directionY, -1);
      out[base + 6] = sceneNumber(light.directionZ, 0);
      out[base + 7] = sceneNumber(light.intensity, 1);

      // color.rgb + range
      var lc = sceneWebGPUCachedColor(light.color, [1, 1, 1, 1], colorCache);
      out[base + 8] = lc[0];
      out[base + 9] = lc[1];
      out[base + 10] = lc[2];
      out[base + 11] = sceneNumber(light.range, 0);

      // params: decay, shadowBias, castShadow, spot cone angle
      out[base + 12] = sceneNumber(light.decay, 2);
      out[base + 13] = sceneNumber(light.shadowBias, 0.005);
      out[base + 14] = light.castShadow ? 1 : 0;
      out[base + 15] = sceneNumber(light.angle, 0);

      // groundPenumbra: hemisphere ground color + spot penumbra
      var gc = sceneWebGPUCachedColor(light.groundColor, [0, 0, 0, 1], colorCache);
      out[base + 16] = gc[0];
      out[base + 17] = gc[1];
      out[base + 18] = gc[2];
      out[base + 19] = clamp01(sceneNumber(light.penumbra, 0));

      // areaHalfWidth / areaHalfHeight: rect-area edge vectors, else zero.
      if (typeCode === 5) {
        sceneWebGPURectAreaBasis(light, _sceneWebGPURectBasis);
        out[base + 20] = _sceneWebGPURectBasis[0];
        out[base + 21] = _sceneWebGPURectBasis[1];
        out[base + 22] = _sceneWebGPURectBasis[2];
        out[base + 24] = _sceneWebGPURectBasis[3];
        out[base + 25] = _sceneWebGPURectBasis[4];
        out[base + 26] = _sceneWebGPURectBasis[5];
      } else {
        out[base + 20] = 0;
        out[base + 21] = 0;
        out[base + 22] = 0;
        out[base + 24] = 0;
        out[base + 25] = 0;
        out[base + 26] = 0;
      }
      out[base + 23] = 0;
      out[base + 27] = 0;

      census.count += 1;
      if (typeCode === 3) {
        census.spot += 1;
        if (!(sceneNumber(light.angle, 0) > 0)) {
          census.spotEmptyCone += 1;
        }
      } else if (typeCode === 4) {
        census.hemisphere += 1;
      } else if (typeCode === 5) {
        census.rectArea += 1;
      }
      if (kind === "light-probe") {
        census.lightProbe += 1;
        if (Array.isArray(light.coefficients) && light.coefficients.length > 0) {
          census.lightProbeWithCoefficients += 1;
        }
      }
    }
    return census;
  }

  // sceneWebGPUReportLightingIssue routes one lighting diagnostic to the shared
  // issue channel. Authors cannot see a wrong image on a backend they do not
  // run, so every known deviation reports itself.
  function sceneWebGPUReportLightingIssue(record) {
    try {
      if (typeof window !== "undefined" && window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue(record);
        return true;
      }
    } catch (e) {
      // Fall through to the console below.
    }
    if (typeof console !== "undefined" && typeof console.warn === "function") {
      console.warn("[gosx] scene3d lighting: " + (record && record.message));
    }
    return false;
  }

  // sceneWebGPULightIssues turns a census plus a light total into the list of
  // diagnostics an author must see. Pure, so a test can check it without a GPU.
  function sceneWebGPULightIssues(census, requested, capacity) {
    var issues = [];
    var total = Math.max(0, Math.floor(Number(requested) || 0));
    var cap = Math.max(0, Math.floor(Number(capacity) || 0));
    if (total > cap) {
      issues.push({
        code: "light-cap",
        message: total + " lights declared; WebGPU draws the first " + cap + ".",
      });
    }
    if (census && census.spotEmptyCone > 0) {
      issues.push({
        code: "spot-empty-cone",
        message: census.spotEmptyCone + " spot light(s) have Angle 0: the cone is empty and adds no light.",
      });
    }
    if (census && census.rectArea > 0) {
      issues.push({
        code: "rect-area-specular",
        message: census.rectArea + " rect-area light(s): diffuse is exact, specular is a representative-point approximation (no LTC tables).",
      });
    }
    if (census && census.lightProbeWithCoefficients > 0) {
      issues.push({
        code: "light-probe-sh",
        message: census.lightProbeWithCoefficients + " light probe(s): spherical-harmonic coefficients are ignored, the probe shades as ambient.",
      });
    }
    return issues;
  }

  // Publish the pure light helpers so tests can check the packing, the type
  // mapping, and the diagnostics without a GPU.
  if (typeof window !== "undefined" && window.__gosx_scene3d_api) {
    window.__gosx_scene3d_api.sceneWebGPULightTypeCode = sceneWebGPULightTypeCode;
    window.__gosx_scene3d_api.sceneWebGPUPackLights = sceneWebGPUPackLights;
    window.__gosx_scene3d_api.sceneWebGPURectAreaBasis = sceneWebGPURectAreaBasis;
    window.__gosx_scene3d_api.sceneWebGPULightCapacityFor = sceneWebGPULightCapacityFor;
    window.__gosx_scene3d_api.sceneWebGPULightIssues = sceneWebGPULightIssues;
    window.__gosx_scene3d_api.SCENE_WEBGPU_LIGHT_FLOATS = SCENE_WEBGPU_LIGHT_FLOATS;
    window.__gosx_scene3d_api.SCENE_WEBGPU_LIGHT_BYTES = SCENE_WEBGPU_LIGHT_BYTES;
    window.__gosx_scene3d_api.SCENE_WEBGPU_LIGHT_CAPACITY_MAX = SCENE_WEBGPU_LIGHT_CAPACITY_MAX;
  }

  // SCENE_WEBGPU_BUILTIN_CULL_MIN_INSTANCES is the instance count above which
  // the renderer's own cull kernel pays for itself. Below it, one compute
  // dispatch, one uniform upload and one indirect draw cost more than drawing
  // every instance.
  var SCENE_WEBGPU_BUILTIN_CULL_MIN_INSTANCES = 256;

  // -----------------------------------------------------------------------
  // Render bundles
  // -----------------------------------------------------------------------
  //
  // A GPURenderBundle records a set of draws once and replays them with one
  // executeBundles call. Replay removes every per-draw crossing into the
  // browser's WebGPU implementation, and it removes the per-draw validation the
  // implementation repeats on a plain pass.
  //
  // The hard part is invalidation. A bundle holds its pipelines, bind groups and
  // buffers BY REFERENCE. Buffer CONTENTS may change freely — a uniform written
  // with queue.writeBuffer between frames reaches the replayed draw, which is
  // the whole point. What must never change is the command stream itself: the
  // object identities and the numeric arguments. Replaying a bundle whose draw
  // set moved on renders the wrong image, silently.
  //
  // This renderer does not guess at the determinants. It records the exact
  // command stream a frame WOULD encode, using a recorder that implements the
  // encoder subset the draw functions call, and compares that stream against the
  // stream the cached bundle was built from. The comparison is the command
  // stream, so it has no blind spots by construction: any change in a pipeline,
  // a bind group, a buffer, an offset, a vertex count or an instance count
  // changes a token and forces a re-encode.
  //
  // The recorder costs one walk of the draw list per frame and issues no WebGPU
  // calls. The saving is every setPipeline, setBindGroup, setVertexBuffer and
  // draw call the frame would otherwise make.

  var SCENE_WEBGPU_BUNDLE_OP_PIPELINE = 1;
  var SCENE_WEBGPU_BUNDLE_OP_BIND_GROUP = 2;
  var SCENE_WEBGPU_BUNDLE_OP_VERTEX_BUFFER = 3;
  var SCENE_WEBGPU_BUNDLE_OP_INDEX_BUFFER = 4;
  var SCENE_WEBGPU_BUNDLE_OP_DRAW = 5;
  var SCENE_WEBGPU_BUNDLE_OP_DRAW_INDEXED = 6;
  var SCENE_WEBGPU_BUNDLE_OP_DRAW_INDIRECT = 7;
  var SCENE_WEBGPU_BUNDLE_OP_DRAW_INDEXED_INDIRECT = 8;

  // sceneWebGPUBundleObjectID stamps a stable small integer on a GPU object so
  // the recorder can compare identities with a number rather than a reference.
  // A WeakMap would work too; a hidden property avoids the lookup cost on the
  // hot path and lets a test read the id back.
  function sceneWebGPUBundleObjectID(value, counter) {
    if (value === null || value === undefined) return 0;
    if (typeof value !== "object" && typeof value !== "function") return 0;
    var id = value.__gosxWGPUBundleID;
    if (typeof id === "number") return id;
    id = counter.next++;
    try {
      Object.defineProperty(value, "__gosxWGPUBundleID", {
        value: id,
        enumerable: false,
        writable: false,
        configurable: false,
      });
    } catch (_stampError) {
      // A frozen or exotic object cannot carry the stamp. Return 0, which never
      // matches a stamped id, so such a frame simply never replays a bundle.
      return 0;
    }
    return id;
  }

  // createSceneWebGPUDrawRecorder returns an object with the GPURenderPassEncoder
  // subset the bundled draw paths use. It appends every call to a reusable
  // numeric token array. It never allocates after the first few frames.
  function createSceneWebGPUDrawRecorder() {
    var tokens = [];
    var length = 0;
    var counter = { next: 1 };
    var drawCount = 0;
    var unsupported = 0;

    function push(value) {
      tokens[length++] = value;
    }

    function pushID(value) {
      var id = sceneWebGPUBundleObjectID(value, counter);
      if (id === 0 && value !== null && value !== undefined) unsupported++;
      tokens[length++] = id;
    }

    return {
      reset: function() {
        length = 0;
        drawCount = 0;
        unsupported = 0;
      },
      length: function() { return length; },
      drawCount: function() { return drawCount; },
      // unsupportedCount counts objects that refused an identity stamp. A
      // non-zero count makes the frame ineligible rather than risking a false
      // match on id 0.
      unsupportedCount: function() { return unsupported; },
      tokens: function() { return tokens; },

      setPipeline: function(pipeline) {
        push(SCENE_WEBGPU_BUNDLE_OP_PIPELINE);
        pushID(pipeline);
      },
      setBindGroup: function(index, group, offsets) {
        push(SCENE_WEBGPU_BUNDLE_OP_BIND_GROUP);
        push(index);
        pushID(group);
        // Dynamic offsets are numbers, and a changed offset changes the draw, so
        // they belong in the stream.
        if (offsets === null || offsets === undefined) {
          push(-1);
        } else if (typeof offsets === "number") {
          push(1);
          push(offsets);
        } else {
          push(offsets.length);
          for (var i = 0; i < offsets.length; i++) push(offsets[i]);
        }
      },
      setVertexBuffer: function(slot, buffer, offset, size) {
        push(SCENE_WEBGPU_BUNDLE_OP_VERTEX_BUFFER);
        push(slot);
        pushID(buffer);
        push(offset === undefined ? 0 : offset);
        push(size === undefined ? -1 : size);
      },
      setIndexBuffer: function(buffer, format, offset, size) {
        push(SCENE_WEBGPU_BUNDLE_OP_INDEX_BUFFER);
        pushID(buffer);
        push(format === "uint16" ? 16 : 32);
        push(offset === undefined ? 0 : offset);
        push(size === undefined ? -1 : size);
      },
      draw: function(vertexCount, instanceCount, firstVertex, firstInstance) {
        push(SCENE_WEBGPU_BUNDLE_OP_DRAW);
        push(vertexCount);
        push(instanceCount === undefined ? 1 : instanceCount);
        push(firstVertex === undefined ? 0 : firstVertex);
        push(firstInstance === undefined ? 0 : firstInstance);
        drawCount++;
      },
      drawIndexed: function(indexCount, instanceCount, firstIndex, baseVertex, firstInstance) {
        push(SCENE_WEBGPU_BUNDLE_OP_DRAW_INDEXED);
        push(indexCount);
        push(instanceCount === undefined ? 1 : instanceCount);
        push(firstIndex === undefined ? 0 : firstIndex);
        push(baseVertex === undefined ? 0 : baseVertex);
        push(firstInstance === undefined ? 0 : firstInstance);
        drawCount++;
      },
      drawIndirect: function(buffer, offset) {
        push(SCENE_WEBGPU_BUNDLE_OP_DRAW_INDIRECT);
        pushID(buffer);
        push(offset === undefined ? 0 : offset);
        drawCount++;
      },
      drawIndexedIndirect: function(buffer, offset) {
        push(SCENE_WEBGPU_BUNDLE_OP_DRAW_INDEXED_INDIRECT);
        pushID(buffer);
        push(offset === undefined ? 0 : offset);
        drawCount++;
      },
    };
  }

  // sceneWebGPUBundleTokensMatch compares a recorder's current stream against a
  // saved copy. It bails at the first difference.
  function sceneWebGPUBundleTokensMatch(tokens, length, saved) {
    if (!saved || saved.length !== length) return false;
    for (var i = 0; i < length; i++) {
      if (tokens[i] !== saved[i]) return false;
    }
    return true;
  }

  // sceneWebGPUCopyBundleTokens snapshots the recorder's stream. The snapshot is
  // a plain array of numbers, so it holds no reference to a GPU object and keeps
  // nothing alive.
  function sceneWebGPUCopyBundleTokens(tokens, length) {
    var out = new Array(length);
    for (var i = 0; i < length; i++) out[i] = tokens[i];
    return out;
  }

  // createSceneWebGPUBundleCache owns one cached GPURenderBundle and the token
  // stream it was built from. Call plan() with a function that drives an encoder,
  // then act on the verdict.
  function createSceneWebGPUBundleCache() {
    var recorder = createSceneWebGPUDrawRecorder();
    var savedTokens = null;
    var savedLayout = "";
    var bundle = null;
    var encodes = 0;
    var replays = 0;
    var lastDrawCount = 0;

    return {
      recorder: recorder,
      // plan records the frame's command stream and reports whether the cached
      // bundle still matches it. layoutKey covers everything OUTSIDE the command
      // stream that a bundle encoder bakes in: the colour formats, the depth
      // format and the sample count.
      plan: function(layoutKey, encodeFn) {
        recorder.reset();
        encodeFn(recorder);
        lastDrawCount = recorder.drawCount();
        if (recorder.unsupportedCount() > 0) {
          return { reusable: false, eligible: false, reason: "unstamped-object" };
        }
        if (recorder.drawCount() === 0) {
          return { reusable: false, eligible: false, reason: "no-draws" };
        }
        var reusable = bundle !== null &&
          savedLayout === layoutKey &&
          sceneWebGPUBundleTokensMatch(recorder.tokens(), recorder.length(), savedTokens);
        return { reusable: reusable, eligible: true, reason: reusable ? "replay" : "re-encode" };
      },
      // adopt stores a freshly finished bundle together with the stream the
      // recorder holds right now. Call it immediately after plan() reported a
      // miss and the caller encoded for real.
      adopt: function(layoutKey, finished) {
        bundle = finished;
        savedLayout = layoutKey;
        savedTokens = sceneWebGPUCopyBundleTokens(recorder.tokens(), recorder.length());
        encodes++;
      },
      markReplayed: function() { replays++; },
      bundle: function() { return bundle; },
      invalidate: function() {
        bundle = null;
        savedTokens = null;
        savedLayout = "";
      },
      stats: function() {
        return { encodes: encodes, replays: replays, draws: lastDrawCount };
      },
    };
  }

  // sceneWebGPUBundleLayoutKey names the render-target shape a bundle encoder
  // bakes in. A bundle built for one shape is invalid for another.
  function sceneWebGPUBundleLayoutKey(colorFormat, depthFormat, sampleCount) {
    return String(colorFormat) + "|" + String(depthFormat) + "|" + String(sampleCount);
  }

  // sceneWebGPUBundleIneligibleReason names the first scene feature that keeps a
  // frame off the bundled path, or "" when the frame qualifies.
  //
  // The bundled set is the PBR mesh draws plus the instanced mesh draws. Every
  // other draw path stays on the direct encoder, and a frame that holds one of
  // them does not bundle at all. Two reasons:
  //
  //   1. Draw ORDER inside one render pass decides the image. Bundling a subset
  //      and interleaving direct draws around it would need the bundle split at
  //      every boundary, which throws the saving away.
  //   2. The excluded paths rebuild bind groups or reallocate vertex buffers per
  //      frame, so their command stream rarely repeats. Recording them would
  //      cost the walk and never earn a replay.
  //
  // The three per-object exclusions — skinned, computed-morph and Selena
  // material draws — bind buffers a compute pass rewrites or bind groups a
  // per-frame uniform owns. The recorder WOULD catch a change in those, so this
  // is a payoff limit rather than a correctness limit.
  function sceneWebGPUBundleIneligibleReason(flags) {
    if (!flags) return "no-flags";
    if (flags.disabled) return "disabled";
    if (flags.hasWater) return "water";
    if (flags.hasPoints) return "points";
    if (flags.hasLabels) return "labels";
    if (flags.hasScreenLines) return "screen-lines";
    if (flags.hasSurfaces) return "surfaces";
    if (flags.hasWorldLines) return "world-lines";
    if (flags.hasDynamicMeshes) return "dynamic-meshes";
    if (!flags.hasBundleableDraws) return "nothing-to-bundle";
    return "";
  }

  // sceneWebGPUDrawListHasDynamicMesh reports whether any object in the three
  // pass lists draws through a path the bundled set excludes.
  function sceneWebGPUDrawListHasDynamicMesh(drawList, isDynamic) {
    if (!drawList) return false;
    var passes = ["opaque", "alpha", "additive"];
    for (var p = 0; p < passes.length; p++) {
      var list = drawList[passes[p]];
      if (!Array.isArray(list)) continue;
      for (var i = 0; i < list.length; i++) {
        if (isDynamic(list[i])) return true;
      }
    }
    return false;
  }

  if (typeof window !== "undefined" && window.__gosx_scene3d_api) {
    window.__gosx_scene3d_api.createSceneWebGPUDrawRecorder = createSceneWebGPUDrawRecorder;
    window.__gosx_scene3d_api.createSceneWebGPUBundleCache = createSceneWebGPUBundleCache;
    window.__gosx_scene3d_api.sceneWebGPUBundleLayoutKey = sceneWebGPUBundleLayoutKey;
    window.__gosx_scene3d_api.sceneWebGPUBundleIneligibleReason = sceneWebGPUBundleIneligibleReason;
    window.__gosx_scene3d_api.sceneWebGPUDrawListHasDynamicMesh = sceneWebGPUDrawListHasDynamicMesh;
    window.__gosx_scene3d_api.sceneWebGPUBundleObjectID = sceneWebGPUBundleObjectID;
    window.__gosx_scene3d_api.SCENE_WEBGPU_BUILTIN_CULL_MIN_INSTANCES = SCENE_WEBGPU_BUILTIN_CULL_MIN_INSTANCES;
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
    var telemetryMount = canvas.parentNode && typeof canvas.parentNode.setAttribute === "function" ? canvas.parentNode : null;

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
    // lastDeviceLostInfo: { reason, message } captured by the device.lost
    // handler below (see initGPUResources), read back by diagnostics().
    // Kept on THIS renderer instance rather than read off the shared probe
    // snapshot, because a successful re-probe nulls that shared snapshot
    // (16z-scene-webgpu-probe.ts's _webgpuDeviceLostInfo) the moment it
    // recovers — often before the mount-level watchdog's next poll — so
    // reading the shared snapshot lost the detail exactly when it mattered.
    var lastDeviceLostInfo = null;
    var rendererResourcesDisposed = false;

    function rendererDeviceStillActive(scopedDevice) {
      return !!device && device === scopedDevice;
    }

    function sceneWebGPUWaterDebugMode() {
      var raw = "";
      try {
        if (typeof window !== "undefined" && window.location && window.location.search && typeof URLSearchParams === "function") {
          raw = new URLSearchParams(window.location.search).get("gosx-water-debug") || "";
        }
      } catch (_err) {}
      if (!raw && canvas && typeof canvas.getAttribute === "function") {
        raw = canvas.getAttribute("data-gosx-scene3d-water-debug") || "";
      }
      return String(raw || "").trim().toLowerCase();
    }

    function sceneWebGPUWaterDebugSkipsUpdate(mode) {
      return mode === "no-water" || mode === "no-update";
    }

    function sceneWebGPUWaterDebugSkipsDraw(mode) {
      return mode === "compute-only" || mode === "no-draw" || sceneWebGPUWaterDebugSkipsUpdate(mode);
    }

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

    // configuredSurfaceKey remembers the configuration currently applied to the
    // canvas context, so we can skip a redundant reconfigure.
    var configuredSurfaceKey = "";

    function sceneWebGPUSurfaceKey(canvas) {
      var p = activePresentation || {};
      return [
        canvas ? canvas.width : 0,
        canvas ? canvas.height : 0,
        targetFormat,
        p.alphaMode,
        p.colorSpace,
        p.toneMappingMode || "",
        device ? "d" : "",
      ].join("|");
    }

    // configureWebGPUCanvas reconfigures the canvas swapchain ONLY when the surface
    // it depends on actually changed (size, format, alpha mode, colour space, tone
    // mapping, device).
    //
    // This used to be called unconditionally on every rendered frame, under a comment
    // that claimed it reconfigured "if canvas resized" — there was no such check.
    // GPUCanvasContext.configure() re-creates the drawable; on Metal that is an
    // expensive, synchronising driver operation, so the water demo paid a fixed
    // per-frame stall that no amount of reducing its workload could touch: disabling
    // caustics, reflection, refraction, or cutting the mesh to a quarter of its
    // vertices all changed the frame rate by nothing, because the cost was never in
    // the work being drawn. It pinned frames to a multiple of the display refresh on
    // Apple hardware while D3D12/NVIDIA drivers, which make a redundant configure()
    // nearly free, showed no symptom at all.
    function configureWebGPUCanvas(canvas) {
      var target = canvas || (gpuCtx && gpuCtx.canvas) || null;
      var key = sceneWebGPUSurfaceKey(target);
      if (key === configuredSurfaceKey) return false;
      gpuCtx.configure(sceneWebGPUCanvasConfiguration());
      configuredSurfaceKey = key;
      return true;
    }

    // GPU resources (initialized after device is ready).
    var frameBindGroupLayout = null;
    var materialBindGroupLayout = null;
    var elioSkinBindGroupLayout = null;
    var computedMorphBindGroupLayout = null;
    var pointsBindGroupLayout = null;
    var pointsUniformBindGroupLayout = null;
    // For authored Points/ComputeParticles render shaders: a minimal
    // @group(1) @binding(0) uniform BGL used for user-authored uniforms.
    var pointsAuthoredUserUniformBGL = null;
    var pointsAuthoredVertexPipelineLayout = null;   // [frame, userUniform, pointsUniform] for Points layers
    var pointsAuthoredStoragePipelineLayout = null;  // [frame, userUniform, pointsStorage] for ComputeParticle render
    var shadowBindGroupLayout = null;
    var pbrPipelineLayout = null;
    var elioSkinPipelineLayout = null;
    var computedMorphPipelineLayout = null;
    var pointsPipelineLayout = null;
    var pointsVertexPipelineLayout = null;
    var selenaPipelineCache = new Map();
    // Per-layer / per-system authored pipeline cache (keyed by "wgsl|blend|depth|format|samples").
    var pointsAuthoredPipelineCache = new Map();
    // Per-layer / per-system failure flag: layerID → true means the authored
    // pipeline failed; fall back to builtin and warn once.
    var pointsAuthoredLayerFailed = new Map();
    // waterManifestShaderSourcesByID/activeWaterShaderSourcesByID remain as a
    // generic bundle/manifest water-source diagnostic cache (see
    // sceneWaterManifestShaderSources/updateWaterSystems below); they no
    // longer feed any pipeline decision now that the hand-written
    // data-prop/entry-field WGSL resolution tier has been retired in favor of
    // Selena-primary -> builtin-fallback (see getWaterPoolPipeline /
    // getWaterRenderPipeline / renderWaterCausticsPass / etc.).
    var waterManifestShaderSourcesByID = null;
    var activeWaterShaderSourcesByID = null;
    // waterAuthoredCausticsPipelineLastError/waterAuthoredSurfacePipelineLastError
    // are kept (always "") as stable no-op reads for the
    // waterAuthoredCausticFallbackReason/waterAuthoredSurfaceFallbackReason
    // stats fields below -- the authored render/compute pipeline tier itself
    // (and its caches/failure sets) has been removed since Selena is now the
    // sole primary WGSL source ahead of the builtin SCENE_WATER_*_SOURCE
    // fallback.
    var waterAuthoredCausticsPipelineLastError = "";
    var waterAuthoredSurfacePipelineLastError = "";

    var pbrVertexModule = null;
    var pbrInstancedVertexModule = null;
    var pbrInstancedCullVertexModule = null;
    var pbrFragmentModule = null;
    var elioSkinShaderModule = null;
    var elioSkinPipeline = null;
    var computedMorphShaderModule = null;
    var computedMorphPipeline = null;
    var waterComputeShaderModule = null;
    var waterRenderVertexModule = null;
    var waterRenderFragmentModule = null;
    var waterRenderBelowFragmentModule = null;
    var waterPoolVertexModule = null;
    var waterPoolFragmentModule = null;
    var waterCausticsVertexModule = null;
    var waterCausticsFragmentModule = null;
    var waterObjectTextureVertexModule = null;
    var waterObjectTextureFragmentModule = null;
    var waterObjectShadowFragmentModule = null;
    var waterObjectMeshShadowVertexModule = null;
    var waterObjectMeshShadowFragmentModule = null;
    var waterObjectMeshRefractionFragmentModule = null;
    var waterObjectMeshClippedFragmentModule = null;
    var waterSeedPipeline = null;
    var waterDropPipeline = null;
    var waterDisplacementPipeline = null;
    var waterStepPipeline = null;
    var waterNormalPipeline = null;
    var waterCausticsPipeline = null;
    var waterObjectTexturePipeline = null;
    var waterObjectShadowPipeline = null;
    var waterObjectMeshShadowPipeline = null;
    var waterObjectMeshPipelineCache = {};
    var waterPoolPipelineCache = {};
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

    // GPU picking. Created on the first queuePick call, so a scene that never
    // picks pays nothing — no textures, no pipelines, no extra pass.
    var scenePicker = null;

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
    // Retained geometry buffers belong to this renderer/device, never to the
    // shared vertex object. The enumerable map supports epoch sweeping when
    // geometry is removed, replaced, or becomes ineligible.
    var retainedMeshAttributeCache = new Map();
    var retainedMeshAttributeEpoch = 0;
    var retainedMeshBufferStats = {
      liveBytes: 0,
      hits: 0,
      misses: 0,
      uploadCalls: 0,
      uploadBytes: 0,
      allocations: 0,
      rebuilds: 0,
      revisionInvalidations: 0,
      retirements: 0,
    };
    // Hoisted scratches so uniform uploads don't allocate fresh 128-byte
    // ArrayBuffers per entry per frame. The WGSL PointsUniforms layout is
    // vec4-aligned: mat4 + vec4 color/size + vec4 flags + vec4 params +
    // vec4 fog color. Wrapped Float32Array / Uint32Array views are created
    // once for the same underlying storage.
    var pointsUniformScratch = new ArrayBuffer(128);
    var pointsUniformScratchF = new Float32Array(pointsUniformScratch);
    var pointsUniformScratchU = new Uint32Array(pointsUniformScratch);
    var computeParticleSystems = new Map();
    var waterSystems = new Map();
    var waterSystemRetireSerial = 0;
    var instancedCullSystems = new Map(); // meshId → { system, signature }
    var lastComputeParticleTimeSeconds = null;
    var lastWaterTimeSeconds = null;
    var waterClockAPI = (typeof window !== "undefined" && window.__gosx_scene3d_api)
      ? window.__gosx_scene3d_api : null;
    var lastPreparedScene = null;
    var lastWebGPUFrameStats = null;
    var webGPUFrameSeq = 0;
    var gpuTiming = null;
    var gpuTimingFailed = false;
    // A device may advertise timestamp-query while its command encoder does
    // not expose the encoder-level timestamp operations used by this ring.
    // Keep feature discovery separate from an actually encodable timer so
    // adaptive quality can fall back to display-frame timing instead of
    // waiting forever on a query that can never be written.
    var gpuTimingEncodingAvailable = null;
    var failedGPUTimings = [];
    var lastGPUPerformanceSample = null;
    var gpuTimingFrameSeq = 0;
    var deferredWaterTextureRetirements = [];
    var deferredWaterSystemRetirements = [];
    var webGPUEssentialAttributeCache = Object.create(null);
    // Full DOM telemetry is useful for interactive probes, but mirroring the
    // entire diagnostic surface every animation frame creates substantial
    // style/MutationObserver churn. Keep exact stats in memory every frame and
    // limit the broad attribute mirror to 4 Hz unless explicitly requested.
    var WEBGPU_DIAGNOSTIC_ATTRIBUTE_INTERVAL_MS = 250;
    var lastWebGPUDiagnosticAttributeAt = null;
    // Cull telemetry: frame counter for throttling readback (~every 30 frames)
    // and the last aggregated survivor snapshot written to the mount attribute.
    var cullTelemetryFrameCount = 0;
    var lastCullSurvivors = null; // null | string (JSON)
    var pendingWebGPUErrorScope = false;
    var webGPUErrorReportCount = 0;
    // webGPUConsecutiveFrameErrors: consecutive frames (not capped, unlike
    // webGPUErrorReportCount's 3-report emit cap above) that ended with a
    // reported validation/OOM error — reset to 0 on the next clean frame.
    // Drives the mount-level demote (tear down post-FX, retry raw) / backend
    // fallback resilience in 20-scene-mount.js; see diagnostics().frameErrorStreak
    // and disablePostProcessing() below.
    var webGPUConsecutiveFrameErrors = 0;
    // webGPUConsecutiveCleanFrames: the complement of webGPUConsecutiveFrameErrors
    // above — consecutive frames that ended with NO reported validation/OOM
    // error, reset to 0 the moment a frame errors (see reportWebGPUFrameError).
    // Drives the mount-level RESTORE step of the resilience ladder (see
    // diagnostics().frameCleanStreak and enablePostProcessing() below): once
    // a demoted scene has run clean for long enough, the mount re-enables
    // post-FX.
    var webGPUConsecutiveCleanFrames = 0;
    // postFXForceDisabled: set by disablePostProcessing() (the "demote" step
    // of the frame-error resilience ladder) — once true, render() never
    // rebuilds or uses the post-FX chain again for this renderer instance,
    // until enablePostProcessing() (the "restore" step, called by the mount
    // once webGPUConsecutiveCleanFrames crosses its threshold) clears it, or
    // a fresh mount/renderer swap replaces this closure entirely.
    var postFXForceDisabled = false;
    // webGPUBundleCache holds the cached GPURenderBundle and the command stream
    // it was built from. Created on the first eligible frame.
    var webGPUBundleCache = null;

    function ensureGPUTiming() {
      if (gpuTiming !== null) return gpuTiming;
      gpuTiming = false;
      var candidateQuerySet = null;
      var candidateBuffers = [];
      try {
        var supportsTimestamps = device && device.features && typeof device.features.has === "function" && device.features.has("timestamp-query");
        if (!supportsTimestamps || typeof device.createQuerySet !== "function") {
          // timestamp-query is optional in WebGPU and Edge/D3D commonly omits
          // it even while the renderer is submitting healthy GPU work. Keep
          // the timing capability distinct from renderer activity so a
          // missing hardware timer is never reported as "GPU unsupported".
          if (telemetryMount) telemetryMount.setAttribute("data-gosx-scene3d-webgpu-gpu-timing", "timer-unavailable");
          return gpuTiming;
        }
        candidateQuerySet = device.createQuerySet({ type: "timestamp", count: 6 });
        var slots = [];
        for (var i = 0; i < 3; i++) {
          var resolveBuffer = device.createBuffer({ size: 16, usage: GPUBufferUsage.QUERY_RESOLVE | GPUBufferUsage.COPY_SRC });
          candidateBuffers.push(resolveBuffer);
          var readbackBuffer = device.createBuffer({ size: 16, usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ });
          candidateBuffers.push(readbackBuffer);
          slots.push({
            resolve: resolveBuffer,
            readback: readbackBuffer,
            pending: false,
            mapping: false,
            frameSeq: 0,
          });
        }
        gpuTiming = {
          querySet: candidateQuerySet,
          slots: slots,
          timestampPeriodNS: Math.max(0.000001, sceneNumber(device.limits && device.limits.timestampPeriod, 1)),
        };
        gpuTimingEncodingAvailable = null;
        gpuTimingFailed = false;
        if (telemetryMount) telemetryMount.setAttribute("data-gosx-scene3d-webgpu-gpu-timing", "pending");
      } catch (error) {
        if (candidateQuerySet && typeof candidateQuerySet.destroy === "function") candidateQuerySet.destroy();
        for (var candidateIndex = 0; candidateIndex < candidateBuffers.length; candidateIndex++) {
          var candidateBuffer = candidateBuffers[candidateIndex];
          if (candidateBuffer && typeof candidateBuffer.destroy === "function") candidateBuffer.destroy();
        }
        gpuTiming = false;
        gpuTimingFailed = true;
        lastGPUPerformanceSample = null;
        if (telemetryMount) telemetryMount.setAttribute("data-gosx-scene3d-webgpu-gpu-timing", "failed");
      }
      return gpuTiming;
    }

    function destroyGPUTimingResources(timing) {
      if (!timing || timing === false) return;
      destroyRendererGPUResource(timing.querySet);
      var slots = Array.isArray(timing.slots) ? timing.slots : [];
      for (var i = 0; i < slots.length; i++) {
        var slot = slots[i];
        if (!slot) continue;
        try {
          if (slot.readback && typeof slot.readback.unmap === "function" && slot.mapping) slot.readback.unmap();
        } catch (_unmapError) {}
        destroyRendererGPUResource(slot.resolve);
        destroyRendererGPUResource(slot.readback);
        slot.pending = false;
        slot.mapping = false;
      }
    }

    function disableGPUTiming(timing) {
      if (!timing || timing === false) return;
      if (gpuTiming === timing) gpuTiming = false;
      gpuTimingFailed = true;
      failedGPUTimings.push({ timing: timing, retireAfterFrame: gpuTimingFrameSeq + 3 });
      lastGPUPerformanceSample = null;
      if (telemetryMount) telemetryMount.setAttribute("data-gosx-scene3d-webgpu-gpu-timing", "failed");
    }

    function drainDeferredGPUResources(force) {
      for (var i = failedGPUTimings.length - 1; i >= 0; i--) {
        if (!force && gpuTimingFrameSeq < failedGPUTimings[i].retireAfterFrame) continue;
        destroyGPUTimingResources(failedGPUTimings[i].timing);
        failedGPUTimings.splice(i, 1);
      }
      for (var j = deferredWaterTextureRetirements.length - 1; j >= 0; j--) {
        var retirement = deferredWaterTextureRetirements[j];
        if (!force && gpuTimingFrameSeq < retirement.retireAfterFrame) continue;
        for (var k = 0; k < retirement.textures.length; k++) {
          destroyRendererGPUResource(retirement.textures[k]);
        }
        deferredWaterTextureRetirements.splice(j, 1);
      }
      for (var l = deferredWaterSystemRetirements.length - 1; l >= 0; l--) {
        var systemRetirement = deferredWaterSystemRetirements[l];
        if (!force && gpuTimingFrameSeq < systemRetirement.retireAfterFrame) continue;
        if (systemRetirement.system && typeof systemRetirement.system.dispose === "function") {
          try { systemRetirement.system.dispose(); } catch (_err) {}
        }
        deferredWaterSystemRetirements.splice(l, 1);
      }
    }

    function pollGPUTimingReadback() {
      var timing = ensureGPUTiming();
      if (!timing) return;
      for (var i = 0; i < timing.slots.length; i++) {
        var slot = timing.slots[i];
        if (!slot.pending || slot.mapping || gpuTimingFrameSeq - slot.frameSeq < 2) continue;
        if (!slot.readback || typeof slot.readback.mapAsync !== "function") continue;
        slot.mapping = true;
        (function(activeTiming, activeSlot) {
          activeSlot.readback.mapAsync((typeof GPUMapMode !== "undefined" && GPUMapMode.READ) || 1).then(function() {
            if (!activeSlot.readback || typeof activeSlot.readback.getMappedRange !== "function") return;
            var values = new BigUint64Array(activeSlot.readback.getMappedRange().slice(0));
            if (gpuTiming === activeTiming && values.length >= 2 && values[1] >= values[0]) {
              lastGPUPerformanceSample = {
                source: "gpu-timestamp",
                gpuMS: Number(values[1] - values[0]) * activeTiming.timestampPeriodNS / 1000000,
                atMS: (typeof performance !== "undefined" && typeof performance.now === "function") ? performance.now() : Date.now(),
              };
              if (telemetryMount) {
                telemetryMount.setAttribute("data-gosx-scene3d-webgpu-gpu-ms", lastGPUPerformanceSample.gpuMS.toFixed(3));
                telemetryMount.setAttribute("data-gosx-scene3d-webgpu-gpu-timing", "measured");
              }
            }
            activeSlot.readback.unmap();
            activeSlot.pending = false;
            activeSlot.mapping = false;
          }).catch(function() {
            activeSlot.pending = false;
            activeSlot.mapping = false;
          });
        })(timing, slot);
        break;
      }
    }

    function beginGPUFrameTiming(encoder) {
      pollGPUTimingReadback();
      var timing = ensureGPUTiming();
      if (!timing || !encoder) return null;
      if (typeof encoder.writeTimestamp !== "function" || typeof encoder.resolveQuerySet !== "function" || typeof encoder.copyBufferToBuffer !== "function") {
        gpuTimingEncodingAvailable = false;
        if (telemetryMount) telemetryMount.setAttribute("data-gosx-scene3d-webgpu-gpu-timing", "timer-unavailable");
        return null;
      }
      var startIndex = gpuTimingFrameSeq % timing.slots.length;
      for (var i = 0; i < timing.slots.length; i++) {
        var slotIndex = (startIndex + i) % timing.slots.length;
        var slot = timing.slots[slotIndex];
        if (!slot || slot.pending || slot.mapping) continue;
        try {
          encoder.writeTimestamp(timing.querySet, slotIndex * 2);
          gpuTimingEncodingAvailable = true;
          return { timing: timing, slot: slot, slotIndex: slotIndex };
        } catch (_timestampBeginError) {
          disableGPUTiming(timing);
          return null;
        }
      }
      return null;
    }

    function endGPUFrameTiming(encoder, token) {
      if (!token) return;
      try {
        encoder.writeTimestamp(token.timing.querySet, token.slotIndex * 2 + 1);
        encoder.resolveQuerySet(token.timing.querySet, token.slotIndex * 2, 2, token.slot.resolve, 0);
        encoder.copyBufferToBuffer(token.slot.resolve, 0, token.slot.readback, 0, 16);
        token.slot.pending = true;
        token.slot.frameSeq = gpuTimingFrameSeq;
      } catch (_timestampEndError) {
        token.slot.pending = false;
        token.slot.mapping = false;
        disableGPUTiming(token.timing);
      }
    }

    // -----------------------------------------------------------------------
    // Per-pass GPU timing
    // -----------------------------------------------------------------------
    //
    // The frame timer above uses encoder.writeTimestamp. That call is NOT part
    // of the WebGPU standard: it needed the timestamp-query-inside-passes
    // feature, and Chromium removed it. On such an implementation
    // gpuTimingEncodingAvailable goes false and the page gets no GPU time at
    // all, so adaptive quality falls back to display-frame timing.
    //
    // The standard path is timestampWrites on the render-pass descriptor. It
    // also gives something the frame timer never could: a time per pass. Four
    // stamps per frame — shadow begin, shadow end, main begin, main end — yield
    // the shadow cost, the main cost, and a whole-scene GPU time that works
    // where writeTimestamp does not.
    //
    // Slot layout, per ring entry:
    //   0 shadow begin   1 shadow end   2 main begin   3 main end
    var SCENE_WEBGPU_PASS_STAMPS = 4;
    var SCENE_WEBGPU_PASS_RING = 3;
    var gpuPassTiming = null;
    var gpuPassTimingSlot = null;
    var gpuPassTimingShadowUsed = false;
    var lastGPUPassSample = null;

    function ensureGPUPassTiming() {
      if (gpuPassTiming !== null) return gpuPassTiming;
      gpuPassTiming = false;
      var querySet = null;
      var buffers = [];
      try {
        var supported = device && device.features && typeof device.features.has === "function" && device.features.has("timestamp-query");
        if (!supported || typeof device.createQuerySet !== "function") {
          if (telemetryMount) telemetryMount.setAttribute("data-gosx-scene3d-webgpu-gpu-pass-timing", "timer-unavailable");
          return gpuPassTiming;
        }
        querySet = device.createQuerySet({
          type: "timestamp",
          count: SCENE_WEBGPU_PASS_STAMPS * SCENE_WEBGPU_PASS_RING,
        });
        var slots = [];
        for (var i = 0; i < SCENE_WEBGPU_PASS_RING; i++) {
          var resolve = device.createBuffer({
            label: "gosx-pass-timing-resolve",
            size: SCENE_WEBGPU_PASS_STAMPS * 8,
            usage: GPUBufferUsage.QUERY_RESOLVE | GPUBufferUsage.COPY_SRC,
          });
          var readback = device.createBuffer({
            label: "gosx-pass-timing-readback",
            size: SCENE_WEBGPU_PASS_STAMPS * 8,
            usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ,
          });
          buffers.push(resolve, readback);
          slots.push({ resolve: resolve, readback: readback, pending: false, mapping: false, frameSeq: 0, hasShadow: false });
        }
        gpuPassTiming = {
          querySet: querySet,
          slots: slots,
          timestampPeriodNS: Math.max(0.000001, sceneNumber(device.limits && device.limits.timestampPeriod, 1)),
        };
        if (telemetryMount) telemetryMount.setAttribute("data-gosx-scene3d-webgpu-gpu-pass-timing", "pending");
      } catch (_passTimingError) {
        if (querySet && typeof querySet.destroy === "function") querySet.destroy();
        for (var b = 0; b < buffers.length; b++) {
          if (buffers[b] && typeof buffers[b].destroy === "function") buffers[b].destroy();
        }
        gpuPassTiming = false;
        if (telemetryMount) telemetryMount.setAttribute("data-gosx-scene3d-webgpu-gpu-pass-timing", "failed");
      }
      return gpuPassTiming;
    }

    // beginGPUPassTimingFrame picks the ring slot this frame writes into. It
    // returns null when no slot is free, so a frame never overwrites a slot whose
    // readback is still in flight.
    function beginGPUPassTimingFrame() {
      gpuPassTimingSlot = null;
      gpuPassTimingShadowUsed = false;
      var timing = ensureGPUPassTiming();
      if (!timing) return;
      var start = gpuTimingFrameSeq % timing.slots.length;
      for (var i = 0; i < timing.slots.length; i++) {
        var index = (start + i) % timing.slots.length;
        var slot = timing.slots[index];
        if (!slot || slot.pending || slot.mapping) continue;
        slot.hasShadow = false;
        gpuPassTimingSlot = { timing: timing, slot: slot, index: index };
        return;
      }
    }

    // gpuPassTimestampWrites returns the timestampWrites field for one pass, or
    // null. Spread it into the beginRenderPass descriptor only when it is not
    // null: a null field is a validation error on some implementations.
    //
    // Only the FIRST shadow pass is stamped. A second directional light opens a
    // second shadow pass, and the ring holds one pair, so stamping both would
    // report the second pass and hide the first.
    function gpuPassTimestampWrites(passName) {
      if (!gpuPassTimingSlot) return null;
      var base = gpuPassTimingSlot.index * SCENE_WEBGPU_PASS_STAMPS;
      if (passName === "shadow") {
        if (gpuPassTimingShadowUsed) return null;
        gpuPassTimingShadowUsed = true;
        gpuPassTimingSlot.slot.hasShadow = true;
        return { querySet: gpuPassTimingSlot.timing.querySet, beginningOfPassWriteIndex: base, endOfPassWriteIndex: base + 1 };
      }
      if (passName === "main") {
        return { querySet: gpuPassTimingSlot.timing.querySet, beginningOfPassWriteIndex: base + 2, endOfPassWriteIndex: base + 3 };
      }
      return null;
    }

    // endGPUPassTimingFrame resolves the slot into its readback buffer. Call it
    // once, after the last timed pass and before submit.
    function endGPUPassTimingFrame(encoder) {
      if (!gpuPassTimingSlot || !encoder) return;
      if (typeof encoder.resolveQuerySet !== "function" || typeof encoder.copyBufferToBuffer !== "function") {
        gpuPassTimingSlot = null;
        return;
      }
      var token = gpuPassTimingSlot;
      gpuPassTimingSlot = null;
      try {
        encoder.resolveQuerySet(
          token.timing.querySet,
          token.index * SCENE_WEBGPU_PASS_STAMPS,
          SCENE_WEBGPU_PASS_STAMPS,
          token.slot.resolve,
          0
        );
        encoder.copyBufferToBuffer(token.slot.resolve, 0, token.slot.readback, 0, SCENE_WEBGPU_PASS_STAMPS * 8);
        token.slot.pending = true;
        token.slot.frameSeq = gpuTimingFrameSeq;
      } catch (_passResolveError) {
        token.slot.pending = false;
        token.slot.mapping = false;
        gpuPassTiming = false;
      }
    }

    function pollGPUPassTimingReadback() {
      var timing = gpuPassTiming;
      if (!timing) return;
      for (var i = 0; i < timing.slots.length; i++) {
        var slot = timing.slots[i];
        if (!slot.pending || slot.mapping || gpuTimingFrameSeq - slot.frameSeq < 2) continue;
        if (!slot.readback || typeof slot.readback.mapAsync !== "function") continue;
        slot.mapping = true;
        (function(activeTiming, activeSlot) {
          activeSlot.readback.mapAsync((typeof GPUMapMode !== "undefined" && GPUMapMode.READ) || 1).then(function() {
            if (activeSlot.readback && typeof activeSlot.readback.getMappedRange === "function") {
              var values = new BigUint64Array(activeSlot.readback.getMappedRange().slice(0));
              recordGPUPassSample(activeTiming, activeSlot, values);
              activeSlot.readback.unmap();
            }
            activeSlot.pending = false;
            activeSlot.mapping = false;
          }).catch(function() {
            activeSlot.pending = false;
            activeSlot.mapping = false;
          });
        })(timing, slot);
        break;
      }
    }

    // recordGPUPassSample turns four raw timestamps into milliseconds. A zero or
    // decreasing pair means the implementation did not write that stamp, so the
    // reading is dropped rather than published as 0.
    function recordGPUPassSample(timing, slot, values) {
      if (!values || values.length < SCENE_WEBGPU_PASS_STAMPS) return;
      var toMS = function(begin, end) {
        if (end <= begin) return -1;
        return Number(end - begin) * timing.timestampPeriodNS / 1000000;
      };
      var shadowMS = slot.hasShadow ? toMS(values[0], values[1]) : 0;
      var mainMS = toMS(values[2], values[3]);
      if (mainMS < 0) return;
      // The whole-scene span runs from the first stamped pass to the end of the
      // main pass, so it includes the gap between passes — which is real GPU
      // time the frame spent.
      var sceneBegin = slot.hasShadow ? values[0] : values[2];
      var sceneMS = toMS(sceneBegin, values[3]);
      lastGPUPassSample = {
        shadowMS: shadowMS < 0 ? 0 : shadowMS,
        mainMS: mainMS,
        sceneMS: sceneMS < 0 ? mainMS : sceneMS,
        atMS: (typeof performance !== "undefined" && typeof performance.now === "function") ? performance.now() : Date.now(),
      };
      if (telemetryMount) {
        telemetryMount.setAttribute("data-gosx-scene3d-webgpu-gpu-pass-shadow-ms", lastGPUPassSample.shadowMS.toFixed(3));
        telemetryMount.setAttribute("data-gosx-scene3d-webgpu-gpu-pass-main-ms", lastGPUPassSample.mainMS.toFixed(3));
        telemetryMount.setAttribute("data-gosx-scene3d-webgpu-gpu-pass-scene-ms", lastGPUPassSample.sceneMS.toFixed(3));
        telemetryMount.setAttribute("data-gosx-scene3d-webgpu-gpu-pass-timing", "measured");
      }
      // Feed the shared performance sample only when the non-standard
      // encoder-level timer is unavailable. Where both work, the frame timer
      // keeps ownership so its existing budget assertions stay comparable.
      if (gpuTimingEncodingAvailable === false || gpuTiming === false) {
        lastGPUPerformanceSample = {
          source: "gpu-pass-timestamp",
          gpuMS: lastGPUPassSample.sceneMS,
          atMS: lastGPUPassSample.atMS,
        };
        if (telemetryMount) {
          telemetryMount.setAttribute("data-gosx-scene3d-webgpu-gpu-ms", lastGPUPassSample.sceneMS.toFixed(3));
          telemetryMount.setAttribute("data-gosx-scene3d-webgpu-gpu-timing", "measured-pass");
        }
      }
    }

    function destroyGPUPassTimingResources() {
      var timing = gpuPassTiming;
      gpuPassTiming = null;
      gpuPassTimingSlot = null;
      if (!timing || timing === false) return;
      destroyRendererGPUResource(timing.querySet);
      for (var i = 0; i < timing.slots.length; i++) {
        var slot = timing.slots[i];
        if (!slot) continue;
        try {
          if (slot.readback && slot.mapping && typeof slot.readback.unmap === "function") slot.readback.unmap();
        } catch (_unmapError) {}
        destroyRendererGPUResource(slot.resolve);
        destroyRendererGPUResource(slot.readback);
      }
    }

    function pollPerformanceSample() {
      pollGPUTimingReadback();
      pollGPUPassTimingReadback();
      var sample = lastGPUPerformanceSample;
      lastGPUPerformanceSample = null;
      return sample;
    }

    function getPerformanceTimingStatus() {
      var timing = ensureGPUTiming();
      var active = Boolean(timing && timing !== false && Array.isArray(timing.slots) && gpuTimingEncodingAvailable === true);
      var pending = active && gpuTiming.slots.some(function(slot) { return slot && (slot.pending || slot.mapping); });
      return { available: active, active: active, pending: pending, failed: gpuTimingFailed, source: "gpu-timestamp" };
    }

    // Shadow pass buffer.
    var shadowPositionBuffer = null;
    var shadowFrameBuffer = null;
    var shadowFrameBufferStride = 256;
    var shadowFrameBufferCapacity = 0;

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
    var waterTileSampler = null;
    // Dedicated envMap sampler: the equirect wrap seam needs addressModeU
    // "repeat" (the seam wraps around the sphere) and addressModeV
    // "clamp-to-edge" (the poles do not wrap). linearSampler clamps both
    // axes, so it cannot serve this texture without a visible seam.
    var envMapSampler = null;

    // Water simulation resources.
    var waterComputeBindGroupLayout = null;
    var waterRenderBindGroupLayout = null;
    var waterPoolBindGroupLayout = null;
    var waterCausticsBindGroupLayout = null;
    var waterObjectTextureBindGroupLayout = null;
    var waterObjectMeshShadowBindGroupLayout = null;
    var waterComputePipelineLayout = null;
    var waterRenderPipelineLayout = null;
    var waterPoolPipelineLayout = null;
    var waterCausticsPipelineLayout = null;
    var waterObjectTexturePipelineLayout = null;
    var waterObjectMeshShadowPipelineLayout = null;
    var waterRenderPipelineCache = new Map();
    var WATER_MAX_DISPLACEMENT_SPHERES = 32;
    var WATER_CAUSTICS_TEXTURE_FORMAT = "rgba8unorm";
    var WATER_CAUSTICS_TEXTURE_SIZE = 1024;
    var WATER_OBJECT_TEXTURE_FORMAT = "rgba8unorm";
    var WATER_OBJECT_TEXTURE_SIZE = 256;
    var WATER_OBJECT_TEXTURE_MAX_SIZE = 2048;
    var WATER_OBJECT_TEXTURE_TARGET_COUNT = 3;
    var WATER_OBJECT_SHADOW_TEXTURE_SIZE = 256;
    // M5 at-rest gating (water-parity-campaign). waterRestEnergy is a CHEAP
    // (CPU-only, no GPU readback) proxy for the simulation's kinetic energy:
    // it starts at 1.0 on any disturbance and is multiplied by the SAME
    // `damping` coefficient the simulation kernel itself applies to velocity
    // every substep it actually runs (see WGSL_COMMON... "info.y = (info.y +
    // ...) * params.damping" and sceneWaterUniformData's damping field), so it
    // tracks the real physical decay rate rather than an arbitrary timer.
    // WATER_REST_ENERGY_EPSILON is the threshold below which the residual
    // ripple is visually and physically negligible (with the demo's default
    // damping=0.995 this is ~1300 substeps, ~11s of undisturbed real time --
    // deliberately calm, not twitchy, since resting is a background
    // GPU-cost win, not a latency-sensitive UI state). If an author's
    // `damping` is tuned close to 1.0 (near-lossless), energy decays too
    // slowly to ever cross this threshold and the system correctly never
    // rests (a genuinely undamped simulation should not visually freeze).
    // WATER_REST_MIN_QUIET_MS is a defense-in-depth floor alongside the
    // energy check: even if a future damping/authoring change made the
    // energy estimate decay unexpectedly fast, a system cannot rest sooner
    // than this many real milliseconds after its last disturbance.
    var WATER_REST_ENERGY_EPSILON = 0.001;
    var WATER_REST_MIN_QUIET_MS = 1200;
    var waterUniformScratch = new ArrayBuffer(256);
    var waterUniformScratchF = new Float32Array(waterUniformScratch);
    var waterUniformScratchU = new Uint32Array(waterUniformScratch);
    // M6 per-frame churn audit -- see waterUniformSnapshotChanged's comment
    // below (near sceneWaterUniformData). Word indices [0, this) in
    // waterUniformScratch are resolution/cellCount/seedDrops/frameIndex/
    // deltaTime/timeSeconds and are excluded from the "did anything
    // meaningful change" comparison.
    var WATER_UNIFORM_VOLATILE_WORDS = 6;
    var waterObjectSphereScratch = new Float32Array(WATER_MAX_DISPLACEMENT_SPHERES * 4);
    var waterObjectMeshShadowUniformScratch = new Float32Array(8);
    var waterObjectTextureMatrixScratch = new Float32Array(32);

    // Texture cache.
    var textureCache = new Map();
    textureCache._gosxGeneration = {
      disposed: false,
      onResourceReady: function() {
        if (canvas && typeof canvas.dispatchEvent === "function") {
          var event = typeof CustomEvent === "function"
            ? new CustomEvent("gosx:scene3d:resource-ready")
            : { type: "gosx:scene3d:resource-ready" };
          canvas.dispatchEvent(event);
        }
      },
    };
    var iblResources = {
      key: "",
      irradiance: null,
      radiance: null,
      brdfLUT: null,
      active: false,
      diagnostics: { requested: false, active: false, state: "not-requested", reason: "", radianceMipLevels: 0 },
    };
    // Legacy equirectangular Environment.EnvironmentMap image (the same
    // `envMap` field 16-scene-webgl.js reads). IBL wins when it is active:
    // syncEnvironmentMap suppresses this the same frame syncEnvironmentIBL
    // reports ibl.active, mirroring the WebGL2 status machine at
    // 16-scene-webgl.js:6249-6252 (envMap MAY still shade while IBL is only
    // "loading"/"validating", never once it is "active").
    var envMapResources = {
      key: "",
      record: null,
      active: false,
    };

    // pbrSceneAttributeCache backs wgpuStablePBRAttributeBuffer below, keyed
    // by slot name (not by `bundle`, which createSceneRenderBundle rebuilds
    // fresh every render() call -- see that function's comment). Renderer-
    // scoped like selenaPipelineCache/textureCache above: it can't go stale
    // across device loss because a lost device kills THIS whole closure
    // (device=null, initFailed=true) and recovery always calls createRenderer()
    // again for a brand new one.
    var pbrSceneAttributeCache = {};
    var retainedMaterialOwners = new WeakMap();

    // 1x1 white placeholder texture (for unbound material maps).
    var placeholderTex = null;
    var placeholderView = null;
    var placeholderCubeTex = null;
    var placeholderCubeView = null;

    // Post-processor.
    var postProcessor = null;

    // Scratch Float32Arrays.
    var scratchViewMatrix = new Float32Array(16);
    var scratchProjMatrix = new Float32Array(16);
    var scratchSelenaViewProjection = new Float32Array(16);
    // selenaFrame hands the per-frame state to the module-scope uniform packer
    // in 16a1. viewProjection holds a live reference to the scratch matrix, so
    // the packer always reads the current frame matrix. time is the per-frame
    // clock (seconds) fed to selena materials that declare `param time : float`;
    // it is set once per frame before any selena draw, and an explicit
    // customUniforms.time still overrides it.
    var selenaFrame = { viewProjection: scratchSelenaViewProjection, time: 0 };

    // Hoisted uniform staging buffers — reused every frame to eliminate per-frame allocations.
    // Each scratch is consumed synchronously (filled → writeBuffer → done) before any reuse.
    var _frameUniformBuf = new ArrayBuffer(160);
    var _frameUniformF   = new Float32Array(_frameUniformBuf);
    var _frameUniformU   = new Uint32Array(_frameUniformBuf);

    var _fogUniformBuf = new ArrayBuffer(32);
    var _fogUniformF   = new Float32Array(_fogUniformBuf);
    var _fogUniformU   = new Uint32Array(_fogUniformBuf);

    var _shadowUniformBuf = new ArrayBuffer(160);
    var _shadowUniformF   = new Float32Array(_shadowUniformBuf);
    var _shadowUniformU   = new Uint32Array(_shadowUniformBuf);
    var _shadowUniformI   = new Int32Array(_shadowUniformBuf);

    var _envUniformBuf = new ArrayBuffer(80);
    var _envUniformF = new Float32Array(_envUniformBuf);
    var _envUniformU = new Uint32Array(_envUniformBuf);

    var _lightCountBuf  = new Uint32Array(1);
    var _lightCapacity  = SCENE_WEBGPU_LIGHT_CAPACITY_MIN;
    var _lightDataF     = new Float32Array(_lightCapacity * SCENE_WEBGPU_LIGHT_FLOATS);
    var _lightColorCache = {};
    // Grown-out light buffers wait here until dispose. A buffer the previous
    // frame's submitted commands still reference must not be destroyed early.
    // Growth doubles from 8 to 256, so this list holds at most five buffers.
    var _retiredLightBuffers = [];
    // Reported lighting diagnostics, keyed by code plus payload, so one wrong
    // scene warns once instead of every frame.
    var _lightIssuesReported = Object.create(null);

    // 192 bytes: the previous 176-byte MaterialUniforms layout plus the
    // vec3f-aligned effective specular F0 and the trailing F90 scalar. Only
    // the material buffer grows; frame and shadow buffers are untouched.
    var _materialUniformBuf = new ArrayBuffer(192);
    var _materialUniformF   = new Float32Array(_materialUniformBuf);
    var _materialUniformU   = new Uint32Array(_materialUniformBuf);

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

    // wgpuStablePBRAttributeBuffer backs ensurePBRSceneAttributeBuffers: one
    // GPU buffer per attribute `slot`, cached on the renderer-scoped
    // pbrSceneAttributeCache (bundle can't be the owner -- see its comment
    // above). Skips BOTH the buffer (re)allocation and the queue.writeBuffer
    // call whenever `typedArray`'s CONTENT matches the last upload -- not
    // just its identity, which is never stable across frames even for scene
    // geometry that never moves (createSceneRenderBundle hands back a fresh
    // Float32Array every call). Every water-demo float-* object has zero
    // drift/bob/spin (program.go), so once placed this collapses "4 buffer
    // allocs + 4 full uploads every frame forever" to "once" -- a scene with
    // genuinely animating mesh geometry still uploads whenever content changes.
    function wgpuStablePBRAttributeBuffer(slot, typedArray) {
      // Reuse the cached snapshot's IDENTITY (not typedArray's, which is
      // fresh every frame) once content-equal, so the delegated
      // wgpuCachedTrackedBuffer call below (identity-based) naturally skips
      // its own writeBuffer -- see this function's comment above.
      var snap = pbrSceneAttributeCache[slot];
      var same = snap && snap.length === typedArray.length;
      for (var i = 0; same && i < snap.length; i++) if (snap[i] !== typedArray[i]) same = false;
      if (!same) pbrSceneAttributeCache[slot] = snap = typedArray.slice();
      return wgpuCachedTrackedBuffer(pbrSceneAttributeCache, slot + "B", snap, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, false);
    }

    function wgpuCachedTrackedBuffer(owner, slot, typedArray, usage, dynamic) {
      if (!owner || !typedArray) return null;
      // Cross-renderer staleness guard: if the buffer cached at owner[slot] was
      // created by a prior renderer (not in this renderer's pointsEntryGPUBuffers
      // set), the underlying GPU resource was destroyed when that renderer was
      // disposed or lost its device. Clear the stale JS reference so that the
      // alloc path below creates a fresh buffer on the current device.  Without
      // this, sceneCachedSlotBuffer would blindly reuse the dead buffer (its .size
      // property survives .destroy()), causing "Buffer with '' label is invalid"
      // errors on every frame after device-loss recovery until the page is reloaded.
      var staleCandidate = owner[slot];
      if (staleCandidate && !pointsEntryGPUBuffers.has(staleCandidate)) {
        owner[slot] = null;
        // Clearing the pool that caches bind groups built around this uniform
        // buffer removes any stale entries that reference the dead buffer object.
        // The pool key is "_gosxWGPUSBGC" + slot (see createSelenaBindGroup).
        var staleBGPool = "_gosxWGPUSBGC" + slot;
        if (Array.isArray(owner[staleBGPool])) owner[staleBGPool] = [];
        // Clear the material bind-group single-entry caches that key on the
        // materialBuffer identity (both shadow and non-shadow variants).
        owner["_gosxWGPUMatBGCache"] = null;
        owner["_gosxWGPUMatBGCacheS"] = null;
      }
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
        // Handle device loss post-factory. Record the loss detail on THIS
        // renderer (lastDeviceLostInfo, read back by diagnostics() below)
        // for diagnosis, and run our own local cleanup — but do NOT also
        // invalidate the shared probe here. 16z-scene-webgpu-probe.ts's own
        // sceneWebGPUWatchDeviceLoss already has a `.then()` listener on
        // this EXACT device (it is the same object handed to us as
        // probe.device above), and is the probe's single owner for
        // invalidation/reprobe bookkeeping. A single device.lost event
        // resolves every listener attached to it, so having both this
        // handler AND 16z's call sceneWebGPUInvalidateProbe counted one
        // real loss as two against WEBGPU_LOST_REPROBE_MAX_PER_WINDOW,
        // arming its reprobe backoff after a single device loss instead of
        // the intended three.
        device.lost.then(function(info) {
          console.warn("[gosx] WebGPU device lost:", info && info.message);
          // Journal the loss with a timestamp. A device that mounts healthy and
          // dies seconds later (observed on Firefox under GPU-memory pressure)
          // is indistinguishable from "never had WebGPU" in any single sample;
          // only an ordered timeline separates the two.
          renderTruth().record("device-lost", (info && info.reason ? info.reason + " " : "") + String(info && info.message || ""));
          lastDeviceLostInfo = {
            reason: (info && info.reason) || "",
            message: (info && info.message) || "",
          };
          initFailed = true;
          // Eagerly free every renderer-owned resource and logical cache. The
          // probe-based recovery path
          // (gosx:scene3d:webgpu-probe-ready → recoverSceneWebGPURenderer in
          // 20-scene-mount.js) creates a fresh renderer whose first render()
          // call rebuilds resources on the new device. dispose() shares this
          // idempotent path, so mount teardown after the loss is a safe no-op.
          dispose();
        }).catch(function() {});

        // uncapturederror carries validation and out-of-memory failures the
        // per-frame error scopes never see (resource creation outside a scope,
        // async pipeline work, driver-level complaints). On a Tint/naga
        // divergence this is frequently the ONLY textual evidence, so it goes
        // in the journal even though the frame keeps running.
        if (typeof device.addEventListener === "function") {
          try {
            device.addEventListener("uncapturederror", function(event) {
              var err = event && event.error;
              renderTruth().record("gpu-uncaptured-error", String((err && err.message) || err || "unknown"));
            });
          } catch (_uncapturedErr) {
            // Older implementations expose device.onuncapturederror only.
          }
        }
        renderTruth().record("webgpu-device-ready", renderTruth().implementation(webGPUAdapterInfoSnapshot()));

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
        waterComputeBindGroupLayout = device.createBindGroupLayout({
          label: "gosx-water-compute",
          entries: [
            { binding: 0, visibility: GPUShaderStage.COMPUTE, buffer: { type: "uniform" } },
            { binding: 1, visibility: GPUShaderStage.COMPUTE, buffer: { type: "read-only-storage" } },
            { binding: 2, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } },
            { binding: 3, visibility: GPUShaderStage.COMPUTE, buffer: { type: "read-only-storage" } },
          ],
        });
        waterRenderBindGroupLayout = device.createBindGroupLayout({
          label: "gosx-water-render",
          entries: [
            { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
            { binding: 1, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "read-only-storage" } },
            { binding: 2, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "filtering" } },
            { binding: 3, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
            { binding: 4, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
            { binding: 5, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
            { binding: 6, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
            { binding: 7, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "cube" } },
            { binding: 8, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
            { binding: 9, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
            { binding: 10, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "read-only-storage" } },
          ],
        });
        waterPoolBindGroupLayout = device.createBindGroupLayout({
          label: "gosx-water-pool",
          entries: [
            { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
            { binding: 1, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "read-only-storage" } },
            { binding: 2, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "filtering" } },
            { binding: 3, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
            { binding: 4, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
            { binding: 5, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
          ],
        });
        waterCausticsBindGroupLayout = device.createBindGroupLayout({
          label: "gosx-water-caustics",
          entries: [
            { binding: 0, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
            { binding: 1, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "read-only-storage" } },
            { binding: 2, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "read-only-storage" } },
            { binding: 3, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "filtering" } },
            { binding: 4, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
          ],
        });
        waterObjectTextureBindGroupLayout = device.createBindGroupLayout({
          label: "gosx-water-object-textures",
          entries: [
            { binding: 0, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
            { binding: 1, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "read-only-storage" } },
          ],
        });
        waterObjectMeshShadowBindGroupLayout = device.createBindGroupLayout({
          label: "gosx-water-object-mesh-shadow",
          entries: [
            { binding: 0, visibility: GPUShaderStage.VERTEX, buffer: { type: "uniform" } },
          ],
        });
        pointsBindGroupLayout = wgpuCreatePointsBindGroupLayout(device);
        pointsUniformBindGroupLayout = wgpuCreatePointsUniformBindGroupLayout(device);
        // Simple uniform BGL for authored user uniforms at group(1).
        pointsAuthoredUserUniformBGL = device.createBindGroupLayout({
          label: "gosx-points-authored-user",
          entries: [{ binding: 0, visibility: (typeof GPUShaderStage !== "undefined" ? GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT : 3), buffer: { type: "uniform" } }],
        });
        pointsAuthoredVertexPipelineLayout = device.createPipelineLayout({
          bindGroupLayouts: [frameBindGroupLayout, pointsAuthoredUserUniformBGL, pointsUniformBindGroupLayout],
        });
        pointsAuthoredStoragePipelineLayout = device.createPipelineLayout({
          bindGroupLayouts: [frameBindGroupLayout, pointsAuthoredUserUniformBGL, pointsBindGroupLayout],
        });
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
        waterComputePipelineLayout = device.createPipelineLayout({
          bindGroupLayouts: [waterComputeBindGroupLayout],
        });
        waterRenderPipelineLayout = device.createPipelineLayout({
          bindGroupLayouts: [frameBindGroupLayout, waterRenderBindGroupLayout],
        });
        waterPoolPipelineLayout = device.createPipelineLayout({
          bindGroupLayouts: [frameBindGroupLayout, waterPoolBindGroupLayout],
        });
        waterCausticsPipelineLayout = device.createPipelineLayout({
          bindGroupLayouts: [waterCausticsBindGroupLayout],
        });
        waterObjectTexturePipelineLayout = device.createPipelineLayout({
          bindGroupLayouts: [waterObjectTextureBindGroupLayout],
        });
        waterObjectMeshShadowPipelineLayout = device.createPipelineLayout({
          bindGroupLayouts: [waterObjectMeshShadowBindGroupLayout],
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
        pbrInstancedCullVertexModule = device.createShaderModule({ label: "pbr-instanced-cull-vert", code: WGSL_PBR_INSTANCED_CULL_VERTEX });
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
        waterComputeShaderModule = device.createShaderModule({ label: "gosx-water-compute", code: SCENE_WATER_COMPUTE_SOURCE });
        waterSeedPipeline = device.createComputePipeline({
          label: "gosx-water-seed-drops",
          layout: waterComputePipelineLayout,
          compute: { module: waterComputeShaderModule, entryPoint: "seedDrops" },
        });
        waterDropPipeline = device.createComputePipeline({
          label: "gosx-water-add-drop",
          layout: waterComputePipelineLayout,
          compute: { module: waterComputeShaderModule, entryPoint: "addDrop" },
        });
        waterDisplacementPipeline = device.createComputePipeline({
          label: "gosx-water-displace-object",
          layout: waterComputePipelineLayout,
          compute: { module: waterComputeShaderModule, entryPoint: "displaceObject" },
        });
        waterStepPipeline = device.createComputePipeline({
          label: "gosx-water-step",
          layout: waterComputePipelineLayout,
          compute: { module: waterComputeShaderModule, entryPoint: "stepSimulation" },
        });
        waterNormalPipeline = device.createComputePipeline({
          label: "gosx-water-normals",
          layout: waterComputePipelineLayout,
          compute: { module: waterComputeShaderModule, entryPoint: "updateNormals" },
        });
        waterRenderVertexModule = device.createShaderModule({ label: "gosx-water-render-vert", code: SCENE_WATER_RENDER_VERTEX_SOURCE });
        waterRenderFragmentModule = device.createShaderModule({ label: "gosx-water-render-frag", code: SCENE_WATER_RENDER_FRAGMENT_SOURCE });
        waterRenderBelowFragmentModule = device.createShaderModule({ label: "gosx-water-render-below-frag", code: SCENE_WATER_RENDER_BELOW_FRAGMENT_SOURCE });
        waterPoolVertexModule = device.createShaderModule({ label: "gosx-water-pool-vert", code: SCENE_WATER_POOL_VERTEX_SOURCE });
        waterPoolFragmentModule = device.createShaderModule({ label: "gosx-water-pool-frag", code: SCENE_WATER_POOL_FRAGMENT_SOURCE });
        waterCausticsVertexModule = device.createShaderModule({ label: "gosx-water-caustics-vert", code: SCENE_WATER_CAUSTICS_VERTEX_SOURCE });
        waterCausticsFragmentModule = device.createShaderModule({ label: "gosx-water-caustics-frag", code: SCENE_WATER_CAUSTICS_FRAGMENT_SOURCE });
        waterCausticsPipeline = device.createRenderPipeline({
          label: "gosx-water-caustics-pass",
          layout: waterCausticsPipelineLayout,
          vertex: { module: waterCausticsVertexModule, entryPoint: "vertexMain", buffers: [] },
          fragment: {
            module: waterCausticsFragmentModule,
            entryPoint: "fragmentMain",
            targets: [{ format: WATER_CAUSTICS_TEXTURE_FORMAT }],
          },
          primitive: { topology: "triangle-list" },
        });
        waterObjectTextureVertexModule = device.createShaderModule({ label: "gosx-water-object-texture-vert", code: SCENE_WATER_OBJECT_TEXTURE_VERTEX_SOURCE });
        waterObjectTextureFragmentModule = device.createShaderModule({ label: "gosx-water-object-texture-frag", code: SCENE_WATER_OBJECT_TEXTURE_FRAGMENT_SOURCE });
        waterObjectShadowFragmentModule = device.createShaderModule({ label: "gosx-water-object-shadow-frag", code: SCENE_WATER_OBJECT_SHADOW_FRAGMENT_SOURCE });
        waterObjectMeshShadowVertexModule = device.createShaderModule({ label: "gosx-water-object-mesh-shadow-vert", code: SCENE_WATER_OBJECT_MESH_SHADOW_VERTEX_SOURCE });
        waterObjectMeshShadowFragmentModule = device.createShaderModule({ label: "gosx-water-object-mesh-shadow-frag", code: SCENE_WATER_OBJECT_MESH_SHADOW_FRAGMENT_SOURCE });
        waterObjectMeshRefractionFragmentModule = device.createShaderModule({ label: "gosx-water-object-mesh-texture-frag", code: sceneWaterObjectMeshFragmentSource(1) });
        waterObjectMeshClippedFragmentModule = device.createShaderModule({ label: "gosx-water-object-mesh-clipped-frag", code: sceneWaterObjectMeshFragmentSource(2) });
        waterObjectTexturePipeline = device.createRenderPipeline({
          label: "gosx-water-object-texture-pass",
          layout: waterObjectTexturePipelineLayout,
          vertex: { module: waterObjectTextureVertexModule, entryPoint: "vertexMain", buffers: [] },
          fragment: {
            module: waterObjectTextureFragmentModule,
            entryPoint: "fragmentMain",
            targets: [
              { format: WATER_OBJECT_TEXTURE_FORMAT },
              { format: WATER_OBJECT_TEXTURE_FORMAT },
              { format: WATER_OBJECT_TEXTURE_FORMAT },
            ],
          },
          primitive: { topology: "triangle-list" },
        });
        waterObjectShadowPipeline = device.createRenderPipeline({
          label: "gosx-water-object-shadow-pass",
          layout: waterObjectTexturePipelineLayout,
          vertex: { module: waterObjectTextureVertexModule, entryPoint: "vertexMain", buffers: [] },
          fragment: {
            module: waterObjectShadowFragmentModule,
            entryPoint: "shadowMain",
            targets: [{ format: WATER_OBJECT_TEXTURE_FORMAT }],
          },
          primitive: { topology: "triangle-list" },
        });
        waterObjectMeshShadowPipeline = device.createRenderPipeline({
          label: "gosx-water-object-mesh-shadow-pass",
          layout: waterObjectMeshShadowPipelineLayout,
          vertex: { module: waterObjectMeshShadowVertexModule, entryPoint: "vertexMain", buffers: WGPU_PBR_VERTEX_LAYOUT },
          fragment: {
            module: waterObjectMeshShadowFragmentModule,
            entryPoint: "fragmentMain",
            targets: [{ format: WATER_OBJECT_TEXTURE_FORMAT }],
          },
          primitive: { topology: "triangle-list", cullMode: "none" },
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
        // Lights start at the minimum capacity and grow in uploadLights.
        lightStorageBuffer = device.createBuffer({
          label: "gosx-lights",
          size: _lightCapacity * SCENE_WEBGPU_LIGHT_BYTES,
          usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST,
        });
        fogUniformBuffer = device.createBuffer({ size: 32, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
        envUniformBuffer = device.createBuffer({ size: 80, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
        shadowUniformBuffer = device.createBuffer({ size: 256, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
        shadowFrameBufferStride = Math.max(
          256,
          Math.floor(sceneNumber(device && device.limits && device.limits.minUniformBufferOffsetAlignment, 256))
        );
        shadowFrameBufferCapacity = 1;
        shadowFrameBuffer = device.createBuffer({
          size: shadowFrameBufferStride,
          usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
        });

        // Create samplers.
        linearSampler = device.createSampler({
          magFilter: "linear",
          minFilter: "linear",
          mipmapFilter: "linear",
          addressModeU: "clamp-to-edge",
          addressModeV: "clamp-to-edge",
        });
        waterTileSampler = device.createSampler({
          magFilter: "linear",
          minFilter: "linear",
          mipmapFilter: "linear",
          addressModeU: "repeat",
          addressModeV: "repeat",
        });
        comparisonSampler = device.createSampler({
          compare: "less",
          magFilter: "linear",
          minFilter: "linear",
        });
        envMapSampler = device.createSampler({
          magFilter: "linear",
          minFilter: "linear",
          mipmapFilter: "linear",
          addressModeU: "repeat",
          addressModeV: "clamp-to-edge",
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
        placeholderCubeTex = wgpuCreatePlaceholderCubeTexture(device);
        placeholderCubeView = placeholderCubeTex.createView({ dimension: "cube" });
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

    function getWaterObjectMeshPipeline(texturePassMode, blendMode, depthWrite) {
      var normalizedMode = texturePassMode === 2 ? 2 : 1;
      var normalizedBlend = blendMode === "alpha" || blendMode === "additive" ? blendMode : "alpha";
      var normalizedDepthWrite = depthWrite !== false;
      var key = wgpuPipelineKey("water-object-mesh-" + normalizedMode, normalizedBlend, normalizedDepthWrite, WATER_OBJECT_TEXTURE_FORMAT, "depth24plus", 1);
      if (waterObjectMeshPipelineCache[key]) return waterObjectMeshPipelineCache[key];
      var fragmentModule = normalizedMode === 2 ? waterObjectMeshClippedFragmentModule : waterObjectMeshRefractionFragmentModule;
      if (!fragmentModule) return null;
      var pipeline = wgpuCreatePBRPipeline(device, pbrPipelineLayout, pbrVertexModule, fragmentModule, normalizedBlend, normalizedDepthWrite, WATER_OBJECT_TEXTURE_FORMAT, 1);
      waterObjectMeshPipelineCache[key] = pipeline;
      return pipeline;
    }

    function getPBRInstancedPipeline(blendMode, depthWrite) {
      var key = wgpuPipelineKey("pbr-instanced", blendMode, depthWrite, targetFormat, "depth24plus", activeSampleCount);
      if (pipelineCache[key]) return pipelineCache[key];
      var pipeline = wgpuCreatePBRInstancedPipeline(device, pbrPipelineLayout, pbrInstancedVertexModule, pbrFragmentModule, blendMode, depthWrite, targetFormat, activeSampleCount);
      pipelineCache[key] = pipeline;
      return pipeline;
    }

    function getPBRInstancedCullPipeline(blendMode, depthWrite) {
      var key = wgpuPipelineKey("pbr-instanced-cull", blendMode, depthWrite, targetFormat, "depth24plus", activeSampleCount);
      if (pipelineCache[key]) return pipelineCache[key];
      var pipeline = wgpuCreatePBRInstancedCullPipeline(device, pbrPipelineLayout, pbrInstancedCullVertexModule, pbrFragmentModule, blendMode, depthWrite, targetFormat, activeSampleCount);
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

    function sceneSelenaUniformBufferSlot(renderContext) {
      var suffix = renderContext && typeof renderContext.uniformSlotSuffix === "string"
        ? renderContext.uniformSlotSuffix.trim().replace(/[^A-Za-z0-9_-]+/g, "-")
        : "";
      return suffix ? "_gosxWGPUSelenaUniform_" + suffix : "_gosxWGPUSelenaUniform";
    }

    function sceneSelenaResourceRef(material, descriptor) {
      var name = descriptor && descriptor.name;
      var value = sceneSelenaMaterialValue(material, name);
      if (value && typeof value === "object") {
        if (typeof value.resource === "string") return value.resource.trim();
        if (typeof value.ref === "string") return value.ref.trim();
        if (typeof value.sceneResource === "string") return value.sceneResource.trim();
      }
      if (typeof value === "string") {
        var trimmed = value.trim();
        if (trimmed.indexOf("gosx:") === 0 || trimmed.indexOf("water:") === 0) return trimmed;
      }
      return "";
    }

    function sceneSelenaParseResourceRef(ref) {
      if (typeof ref !== "string") return null;
      var trimmed = ref.trim();
      if (!trimmed) return null;
      var parts = trimmed.split(":").filter(function(part) { return part !== ""; });
      if (parts[0] === "gosx") parts.shift();
      if (parts[0] !== "water" || parts.length < 3) return null;
      return { kind: "water", id: parts[1], slot: parts.slice(2).join(":") };
    }

    function sceneSelenaWaterSystem(ref) {
      var parsed = sceneSelenaParseResourceRef(ref);
      if (!parsed || parsed.kind !== "water") return null;
      var record = waterSystems && typeof waterSystems.get === "function" ? waterSystems.get(parsed.id) : null;
      return record && record.system ? { system: record.system, slot: parsed.slot } : null;
    }

    function sceneSelenaLiveTextureView(material, texture) {
      var resolved = sceneSelenaWaterSystem(sceneSelenaResourceRef(material, texture));
      if (!resolved || !resolved.system) return null;
      switch (resolved.slot) {
      case "state":
      case "waterState":
      case "height":
      case "heightfield":
        return resolved.system.activeIndex === 0
          ? resolved.system.stateTextureViewA || null
          : resolved.system.stateTextureViewB || null;
      case "caustics":
      case "caustic":
        return resolved.system.causticsView || null;
      case "reflection":
      case "objectReflection":
        return resolved.system.objectReflectionView || null;
      case "clippedReflection":
      case "objectClippedReflection":
        return resolved.system.objectClippedReflectionView || null;
      case "refraction":
      case "objectRefraction":
        return resolved.system.objectRefractionView || null;
      case "shadow":
      case "objectShadow":
        return resolved.system.objectShadowView || null;
      default:
        return null;
      }
    }

    function sceneSelenaLiveBuffer(material, bufferDescriptor) {
      var resolved = sceneSelenaWaterSystem(sceneSelenaResourceRef(material, bufferDescriptor));
      if (!resolved || !resolved.system) return null;
      switch (resolved.slot) {
      case "state":
      case "waterState":
      case "height":
      case "heightfield":
        return resolved.system.activeIndex === 0 ? resolved.system.bufferA : resolved.system.bufferB;
      case "objectSpheres":
        return resolved.system.objectSphereBuffer || null;
      case "uniforms":
      case "params":
        return resolved.system.uniformBuffer || null;
      default:
        return null;
      }
    }

    function sceneSelenaTextureURL(material, texture, index) {
      var name = texture && texture.name;
      var value = sceneSelenaMaterialValue(material, name);
      if (typeof value === "string" && value.trim() && !sceneSelenaParseResourceRef(value)) {
        return value.trim();
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

    function sceneSelenaStorageBufferDescriptors(layout) {
      if (layout && Array.isArray(layout.storageBuffers)) return layout.storageBuffers;
      if (layout && Array.isArray(layout.buffers)) {
        return layout.buffers.filter(function(buffer) {
          var kind = String(buffer && (buffer.kind || buffer.type || "")).toLowerCase();
          return kind.indexOf("storage") >= 0;
        });
      }
      return [];
    }

    // sceneSelenaStateDescriptors returns a material's Selena `state`
    // statefields (bindings.Layout.States), e.g. the water pool pass's
    // `state height` feedback heightfield. This is DISTINCT from
    // sceneSelenaStorageBufferDescriptors (which reads a hand-authored
    // "storageBuffers"/"buffers" descriptor list): a Selena-compiled mesh+state
    // material emits its statefield as `layout.states[]` + a companion
    // `layout.grid` uniform (StateGrid{gridWidth,gridHeight}), not a generic
    // storage buffer entry. No current non-water Selena custom material
    // declares `state`, so this is purely additive.
    function sceneSelenaStateDescriptors(layout) {
      return layout && Array.isArray(layout.states) ? layout.states : [];
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
        // dimension:"cube" (e.g. the water surface/surface-below passes' "sky"
        // environment map) needs viewDimension:"cube" in the bind group layout
        // entry; every other texture (the overwhelming majority) keeps the
        // existing "2d" default.
        var texDimension = textures[i] && textures[i].dimension === "cube" ? "cube" : "2d";
        entries.push({
          binding: sceneNumber(wgsl.textureBinding, 1 + i * 2),
          visibility: typeof GPUShaderStage !== "undefined" ? GPUShaderStage.FRAGMENT : 2,
          texture: { sampleType: "float", viewDimension: texDimension },
        });
        entries.push({
          binding: sceneNumber(wgsl.samplerBinding, 2 + i * 2),
          visibility: typeof GPUShaderStage !== "undefined" ? GPUShaderStage.FRAGMENT : 2,
          sampler: { type: "filtering" },
        });
      }
      var storageBuffers = sceneSelenaStorageBufferDescriptors(layout);
      for (var b = 0; b < storageBuffers.length; b++) {
        var bufferWGSL = storageBuffers[b] && storageBuffers[b].wgsl || {};
        entries.push({
          binding: sceneNumber(bufferWGSL.binding, 1 + textures.length * 2 + b),
          visibility: visibility,
          buffer: { type: "read-only-storage" },
        });
      }
      // Selena `state` statefield support (mesh+state kind, e.g. water pool's
      // `state height`): a StateGrid uniform followed by the resource kind the
      // compiler descriptor declares. Render materials currently emit
      // stateAt(uv) as textureLoad(_inState, ...), while feedback materials use
      // storage buffers. Treating both as storage made otherwise-valid Selena
      // WGSL fail WebGPU bind-group validation and silently erased refraction,
      // displaced surfaces, and caustics.
      var grid = layout && layout.grid;
      var states = sceneSelenaStateDescriptors(layout);
      var afterCoreBindingCount = 1 + textures.length * 2 + storageBuffers.length;
      if (grid && grid.wgsl) {
        entries.push({
          binding: sceneNumber(grid.wgsl.binding, afterCoreBindingCount),
          visibility: visibility,
          buffer: { type: "uniform", minBindingSize: 8 },
        });
      }
      for (var s = 0; s < states.length; s++) {
        var stateWGSL = states[s] && states[s].wgsl || {};
        var stateEntry = {
          binding: sceneNumber(stateWGSL.inBinding, afterCoreBindingCount + 1 + s),
          visibility: visibility,
        };
        if (String(stateWGSL.inKind || "storage").toLowerCase() === "texture") {
          stateEntry.texture = { sampleType: "unfilterable-float", viewDimension: "2d" };
        } else {
          stateEntry.buffer = { type: "read-only-storage" };
        }
        entries.push(stateEntry);
        var outBinding = sceneNumber(stateWGSL.outBinding, -1);
        if (outBinding >= 0) {
          entries.push({
            binding: outBinding,
            visibility: visibility,
            buffer: { type: "storage" },
          });
        }
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

    // NOTE: getSelenaSkinnedPipeline is a near-identical sibling (skinned-mesh
    // variant using WGPU_PBR_VERTEX_LAYOUT, no attrs). Keep the two in sync —
    // any substantive change here must be mirrored there.
    function getSelenaPipeline(material, blendMode, depthWrite, options) {
      if (!sceneSelenaIsMaterial(material)) return null;
      var pipelineTargetFormat = options && options.targetFormat ? options.targetFormat : targetFormat;
      var pipelineSampleCount = Math.max(1, Math.floor(sceneNumber(
        options && options.sampleCount != null ? options.sampleCount : activeSampleCount,
        activeSampleCount || 1
      )));
      var pipelineLabelSuffix = options && options.labelSuffix ? String(options.labelSuffix) + "-" : "";
      // cullMode defaults to "back" when the caller passes no options (or
      // options.cullMode is absent/falsy) -- unchanged from before options.cullMode
      // existed. Water pool geometry is authored with inward-facing wall
      // triangles so it can use the same back-face culling contract as
      // upstream. This is important visually: drawing both sides turns the
      // pool into an opaque exterior shell instead of an open vessel viewed
      // through its rim. drawPBRObjects (this file) is the other caller that
      // passes options: it requests cullMode:"none" for mesh objects with
      // obj.doubleSided === true, leaving every other object on the "back"
      // default -- see the winding-hazard note above that call.
      var pipelineCullMode = options && typeof options.cullMode === "string" && options.cullMode ? options.cullMode : "back";
      // depthStencil defaults to true (every existing caller relies on this
      // default and never passes the option, so behavior there is unchanged):
      // the pipeline gets a depth24plus depthStencil state, matching every
      // render pass this generic path has ever drawn into (the main scene
      // pass, the object-texture RTT reflection/refraction passes -- all of
      // which DO have a depth attachment). Pass {depthStencil:false} for a
      // render target with NO depth attachment at all (e.g. the water
      // caustics/object-mesh-shadow offscreen RTT passes): WebGPU requires a
      // render pipeline's depthStencil state to exactly match whether the
      // render pass it's used in has a depthStencilAttachment.
      var pipelineDepthStencil = !(options && options.depthStencil === false);
      // Per-material memo (perf): getSelenaPipeline is called once PER OBJECT
      // PER FRAME, and the content key below stringifies the whole shader (~1.2KB)
      // + JSON.stringify(layout) on every call. Board frames are fresh-parsed,
      // so a material object lives one frame but is shared by every object that
      // references it (N rects → one BoardFill material). Stamping the resolved
      // key+resource on the material collapses that to ONE key-build per MATERIAL
      // per frame (a handful) instead of per object (hundreds). The stamp is a
      // memo IN FRONT of selenaPipelineCache, not a replacement: the content-keyed
      // Map still backs it so materials across bundles that share a shader share
      // one pipeline. We revalidate the pass-variant inputs (blend/depth/format/
      // samples) cheaply so a material drawn in two passes still resolves
      // correctly; only when they differ do we fall through to the key build.
      var memo = material._gosxWGPUSelenaResource;
      if (
        memo &&
        memo.blendMode === blendMode &&
        memo.depthWrite === depthWrite &&
        memo.targetFormat === pipelineTargetFormat &&
        memo.sampleCount === pipelineSampleCount &&
        memo.cullMode === pipelineCullMode &&
        memo.depthStencil === pipelineDepthStencil
      ) {
        return memo.failed ? null : memo.resource;
      }
      var layout = sceneSelenaMaterialLayout(material);
      var shader = sceneSelenaWGSLSource(material);
      // Cache key = the pipeline's actual inputs (shader source + binding
      // layout + blend/depth/format/samples) — NOT the material identity.
      // Uniform VALUES live in per-object bind groups (createSelenaBindGroup),
      // so N materials sharing one shader (e.g. N board fills differing only
      // in customUniforms.baseColor) share ONE pipeline with N bind groups
      // instead of compiling N identical pipelines.
      var key = [
        "selena",
        shader,
        JSON.stringify(layout),
        blendMode,
        depthWrite ? "1" : "0",
        pipelineTargetFormat,
        pipelineSampleCount,
        pipelineCullMode,
        pipelineDepthStencil ? "ds1" : "ds0",
      ].join("|");
      var cached = selenaPipelineCache.get(key);
      if (cached) {
        // Memoize the resolved (key-derived) result on the material so the next
        // object referencing it this frame skips the key build entirely.
        material._gosxWGPUSelenaResource = {
          blendMode: blendMode,
          depthWrite: depthWrite,
          targetFormat: pipelineTargetFormat,
          sampleCount: pipelineSampleCount,
          cullMode: pipelineCullMode,
          depthStencil: pipelineDepthStencil,
          resource: cached.failed ? null : cached,
          failed: !!cached.failed,
        };
        return cached.failed ? null : cached;
      }
      try {
        var bindGroupLayout = sceneSelenaBindGroupLayout(device, layout);
        var pipelineLayout = device.createPipelineLayout({ bindGroupLayouts: [bindGroupLayout] });
        var module = device.createShaderModule({ label: "selena-material", code: shader });
        renderTruth().captureShaderInfo(module, "selena-material");
        var attrs = sceneSelenaPipelineAttributes(layout);
        var buffers = attrs.map(function(attr) {
          return {
            arrayStride: attr.components * 4,
            stepMode: "vertex",
            attributes: [{ format: attr.format, offset: 0, shaderLocation: attr.shaderLocation }],
          };
        });
        var pipelineDescriptor = {
          label: "gosx-selena-" + pipelineLabelSuffix + (layout.material || "material") + "-" + blendMode,
          layout: pipelineLayout,
          vertex: { module: module, entryPoint: "vertexMain", buffers: buffers },
          fragment: { module: module, entryPoint: "fragmentMain", targets: [{ format: pipelineTargetFormat, blend: wgpuBlendState(blendMode) }] },
          primitive: { topology: "triangle-list", cullMode: pipelineCullMode },
          multisample: { count: pipelineSampleCount },
        };
        if (pipelineDepthStencil) {
          pipelineDescriptor.depthStencil = { format: "depth24plus", depthWriteEnabled: depthWrite, depthCompare: "less-equal" };
        }
        var pipeline = device.createRenderPipeline(pipelineDescriptor);
        cached = { pipeline: pipeline, bindGroupLayout: bindGroupLayout, layout: layout, attrs: attrs };
        selenaPipelineCache.set(key, cached);
        material._gosxWGPUSelenaResource = {
          blendMode: blendMode,
          depthWrite: depthWrite,
          targetFormat: pipelineTargetFormat,
          sampleCount: pipelineSampleCount,
          cullMode: pipelineCullMode,
          depthStencil: pipelineDepthStencil,
          resource: cached,
          failed: false,
        };
        return cached;
      } catch (err) {
        console.warn("[gosx] Selena WebGPU shader pipeline failed; falling back to PBR material.", err);
        selenaPipelineCache.set(key, { failed: true });
        // Memoize the failure too — a broken shader must not re-attempt (and
        // re-warn) once per object per frame.
        material._gosxWGPUSelenaResource = {
          blendMode: blendMode,
          depthWrite: depthWrite,
          targetFormat: pipelineTargetFormat,
          sampleCount: pipelineSampleCount,
          depthStencil: pipelineDepthStencil,
          cullMode: pipelineCullMode,
          resource: null,
          failed: true,
        };
        return null;
      }
    }

    // Skinned variant of getSelenaPipeline. Identical except the pipeline's
    // vertex.buffers use the 4-slot skinned layout (WGPU_PBR_VERTEX_LAYOUT) so
    // slot 0 receives the compute-skinned position buffer produced by
    // updateElioSkinnedMeshes. The skinned draw binds vertex buffers via
    // webGPUBindElioSkinnedBuffers rather than iterating attrs, so this resource
    // deliberately does NOT expose an attrs field (avoids double-binding).
    function getSelenaSkinnedPipeline(material, blendMode, depthWrite, options) {
      if (!sceneSelenaIsMaterial(material)) return null;
      var pipelineCullMode = options && typeof options.cullMode === "string" && options.cullMode ? options.cullMode : "back";
      // Per-material memo, mirroring getSelenaPipeline. A SEPARATE stamp slot
      // (_gosxWGPUSelenaSkinnedResource) so a material drawn both skinned and
      // unskinned never aliases the wrong pipeline — the skinned key uses the
      // "selena-skinned" prefix + WGPU_PBR_VERTEX_LAYOUT, a different pipeline.
      var memo = material._gosxWGPUSelenaSkinnedResource;
      if (
        memo &&
        memo.blendMode === blendMode &&
        memo.depthWrite === depthWrite &&
        memo.targetFormat === targetFormat &&
        memo.sampleCount === activeSampleCount &&
        memo.cullMode === pipelineCullMode
      ) {
        return memo.failed ? null : memo.resource;
      }
      var layout = sceneSelenaMaterialLayout(material);
      var shader = sceneSelenaWGSLSource(material);
      // Content-based key, mirroring getSelenaPipeline (see note there).
      var key = [
        "selena-skinned",
        shader,
        JSON.stringify(layout),
        blendMode,
        depthWrite ? "1" : "0",
        targetFormat,
        activeSampleCount,
        pipelineCullMode,
      ].join("|");
      function stampSkinned(resource, failed) {
        material._gosxWGPUSelenaSkinnedResource = {
          blendMode: blendMode,
          depthWrite: depthWrite,
          targetFormat: targetFormat,
          sampleCount: activeSampleCount,
          cullMode: pipelineCullMode,
          resource: resource,
          failed: failed,
        };
      }
      var cached = selenaPipelineCache.get(key);
      if (cached) {
        stampSkinned(cached.failed ? null : cached, !!cached.failed);
        return cached.failed ? null : cached;
      }
      try {
        var bindGroupLayout = sceneSelenaBindGroupLayout(device, layout);
        var pipelineLayout = device.createPipelineLayout({ bindGroupLayouts: [bindGroupLayout] });
        var module = device.createShaderModule({ label: "selena-material-skinned", code: shader });
        renderTruth().captureShaderInfo(module, "selena-material-skinned");
        var pipeline = device.createRenderPipeline({
          label: "gosx-selena-skinned-" + (layout.material || "material") + "-" + blendMode,
          layout: pipelineLayout,
          vertex: { module: module, entryPoint: "vertexMain", buffers: WGPU_PBR_VERTEX_LAYOUT },
          fragment: { module: module, entryPoint: "fragmentMain", targets: [{ format: targetFormat, blend: wgpuBlendState(blendMode) }] },
          primitive: { topology: "triangle-list", cullMode: pipelineCullMode },
          multisample: { count: Math.max(1, Math.floor(activeSampleCount || 1)) },
          depthStencil: { format: "depth24plus", depthWriteEnabled: depthWrite, depthCompare: "less-equal" },
        });
        cached = { pipeline: pipeline, bindGroupLayout: bindGroupLayout, layout: layout };
        selenaPipelineCache.set(key, cached);
        stampSkinned(cached, false);
        return cached;
      } catch (err) {
        console.warn("[gosx] Selena skinned WebGPU pipeline failed; falling back to PBR material.", err);
        selenaPipelineCache.set(key, { failed: true });
        stampSkinned(null, true);
        return null;
      }
    }

    // sceneSelenaGridUniformData packs a Selena mesh+state material's
    // StateGrid{gridWidth,gridHeight} uniform (8 bytes, 2x u32) from
    // renderContext.grid: either a single number (square grid, e.g. the water
    // heightfield's resolution x resolution) or a {width,height} pair. Returns
    // null when renderContext carries no grid value (caller treats that as a
    // bind-group build failure, mirroring the "missing storage buffer" null
    // returns elsewhere in this function).
    //
    // Falls back to material.customUniforms.grid when renderContext supplies
    // none, mirroring the SAME renderContext-then-customUniforms priority
    // sceneSelenaUniformValue already uses for ordinary uniform fields (see
    // sceneSelenaRenderContextUniformValue / sceneSelenaMaterialValue above).
    // This matters for a Selena mesh+state material drawn through a call site
    // that has no per-draw renderContext at all (e.g. drawPBRObjects' generic
    // main-scene mesh path, used by the water object-material/duck-material
    // Selena materials): without this fallback, the grid uniform would be
    // silently unbuildable there even though the grid size is a known,
    // effectively-static per-demo constant.
    function sceneSelenaGridUniformData(material, renderContext) {
      var grid = renderContext && renderContext.grid;
      if (grid === undefined || grid === null) {
        grid = material && material.customUniforms && material.customUniforms.grid;
      }
      if (grid === undefined || grid === null) return null;
      var width, height;
      if (typeof grid === "number" || typeof grid === "string") {
        width = sceneNumber(grid, 0);
        height = width;
      } else {
        width = sceneNumber(grid.width, 0);
        height = sceneNumber(grid.height, width);
      }
      width = Math.max(1, Math.floor(width));
      height = Math.max(1, Math.floor(height));
      return new Uint32Array([width, height]);
    }

    function createSelenaBindGroup(material, resource, cacheOwner, renderContext) {
      var uniformData = sceneSelenaUniformData(material, cacheOwner, renderContext, selenaFrame);
      if (!uniformData || !resource) return null;
      var owner = (cacheOwner && typeof cacheOwner === "object") ? cacheOwner : material;
      var uniformSlot = sceneSelenaUniformBufferSlot(renderContext);
      var uniformBuffer = wgpuCachedTrackedBuffer(
        owner,
        uniformSlot,
        uniformData,
        GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
        true
      );
      var entries = [{
        binding: sceneNumber(resource.layout && resource.layout.wgsl && resource.layout.wgsl.binding, 0),
        resource: { buffer: uniformBuffer },
      }];
      var textures = sceneSelenaTextureDescriptors(resource.layout);
      var cacheViews = [];
      for (var i = 0; i < textures.length; i++) {
        var tex = textures[i] || {};
        var isCube = tex.dimension === "cube";
        var liveView = sceneSelenaLiveTextureView(material, tex);
        var url = liveView ? "" : sceneSelenaTextureURL(material, tex, i);
        // dimension:"cube" (the water surface/surface-below "sky" environment
        // map) loads through wgpuLoadCubeTexture/placeholderCubeView instead
        // of the plain-2d wgpuLoadTexture/placeholderView path every other
        // Selena texture uses; this mirrors the hand-written
        // createWaterRenderBindGroup's cubeMap handling.
        var record = url ? (isCube ? wgpuLoadCubeTexture(device, url, textureCache) : wgpuLoadTexture(device, url, textureCache)) : null;
        var view = liveView || (record && record.view ? record.view : (isCube ? placeholderCubeView : placeholderView));
        var wgsl = tex.wgsl || {};
        entries.push({ binding: sceneNumber(wgsl.textureBinding, 1 + i * 2), resource: view });
        entries.push({ binding: sceneNumber(wgsl.samplerBinding, 2 + i * 2), resource: linearSampler });
        cacheViews.push(view);
      }
      var storageBuffers = sceneSelenaStorageBufferDescriptors(resource.layout);
      var cacheStorages = [];
      for (var b = 0; b < storageBuffers.length; b++) {
        var bufferDescriptor = storageBuffers[b] || {};
        var bufferWGSL = bufferDescriptor.wgsl || {};
        var buffer = sceneSelenaLiveBuffer(material, bufferDescriptor);
        if (!buffer) return null;
        entries.push({
          binding: sceneNumber(bufferWGSL.binding, 1 + textures.length * 2 + b),
          resource: { buffer: buffer },
        });
        cacheStorages.push(buffer);
      }
      // Selena `state` statefield support (see sceneSelenaStateDescriptors /
      // sceneSelenaBindGroupLayout above): a StateGrid uniform sized from
      // renderContext.grid, then one live storage buffer per statefield
      // resolved via the SAME gosx:water:<id>:<slot> resource-ref mechanism
      // textures/storage buffers already use (sceneSelenaLiveBuffer keys off
      // the statefield's descriptor `name`, e.g. water pool's `state height`
      // resolves via customUniforms.height). Both are appended into the SAME
      // cacheStorages identity list used for bind-group memoization below, so
      // a ping-pong state buffer swap (or a resolution change recreating the
      // grid buffer) correctly invalidates the cached bind group.
      var grid = resource.layout && resource.layout.grid;
      var states = sceneSelenaStateDescriptors(resource.layout);
      var afterCoreBindingCount = 1 + textures.length * 2 + storageBuffers.length;
      if (grid && grid.wgsl) {
        var gridData = sceneSelenaGridUniformData(material, renderContext);
        if (!gridData) return null;
        var gridBuffer = wgpuCachedTrackedBuffer(
          owner,
          uniformSlot + "_grid",
          gridData,
          GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
          true
        );
        entries.push({
          binding: sceneNumber(grid.wgsl.binding, afterCoreBindingCount),
          resource: { buffer: gridBuffer },
        });
        cacheStorages.push(gridBuffer);
      }
      for (var st = 0; st < states.length; st++) {
        var stateDescriptor = states[st] || {};
        var stateWGSL = stateDescriptor.wgsl || {};
        var stateBinding = sceneNumber(stateWGSL.inBinding, afterCoreBindingCount + 1 + st);
        if (String(stateWGSL.inKind || "storage").toLowerCase() === "texture") {
          var stateView = sceneSelenaLiveTextureView(material, stateDescriptor);
          if (!stateView) return null;
          entries.push({ binding: stateBinding, resource: stateView });
          cacheViews.push(stateView);
        } else {
          var stateBuffer = sceneSelenaLiveBuffer(material, stateDescriptor);
          if (!stateBuffer) return null;
          entries.push({ binding: stateBinding, resource: { buffer: stateBuffer } });
          cacheStorages.push(stateBuffer);
        }
      }
      // Memoize the bind group. GPUBindGroups have no destroy() (GC-only), so
      // creating one per frame causes allocation and GC pressure. The uniform
      // buffer data is written by wgpuCachedTrackedBuffer above (dynamic=true)
      // and the bind group references it by identity, so the same bind group
      // remains valid across frames as long as the resource objects are unchanged.
      // The ping-pong waterState storage buffer produces 2 variants (bufferA /
      // bufferB), so the pool stabilises at 2 entries. Pool is capped at 4 to
      // handle rare extra variants (resolution change, etc.) without unbounded
      // growth. Device-loss recovery: a new renderer has a different `device`
      // closure, so pc.device !== device evicts stale entries automatically.
      var bgPoolSlot = "_gosxWGPUSBGC" + uniformSlot;
      var pool = owner[bgPoolSlot];
      if (!Array.isArray(pool)) { pool = []; owner[bgPoolSlot] = pool; }
      for (var pi = 0; pi < pool.length; pi++) {
        var pc = pool[pi];
        if (!pc || pc.device !== device || pc.uniformBuffer !== uniformBuffer) continue;
        if (pc.views.length !== cacheViews.length || pc.storages.length !== cacheStorages.length) continue;
        var match = true;
        for (var vi = 0; vi < cacheViews.length && match; vi++) {
          if (pc.views[vi] !== cacheViews[vi]) match = false;
        }
        for (var si = 0; si < cacheStorages.length && match; si++) {
          if (pc.storages[si] !== cacheStorages[si]) match = false;
        }
        if (match) return pc.bg;
      }
      var newBG = device.createBindGroup({ layout: resource.bindGroupLayout, entries: entries });
      if (pool.length >= 4) pool.shift();
      pool.push({ device: device, uniformBuffer: uniformBuffer, views: cacheViews, storages: cacheStorages, bg: newBG });
      return newBG;
    }

    // -----------------------------------------------------------------------
    // Generic Selena "feedback" kind COMPUTE path (getSelenaComputePipeline /
    // createSelenaComputeBindGroup): closes gap G2 from the context-uniform
    // design (selena-context-uniform-design.md 3.4.2) -- there was previously
    // no descriptor-driven host path for a `kind feedback` material's single
    // @compute module. Binding contract (see emit/wgsl/wgsl.go emitFeedback,
    // and bindings.Layout's Grid/States/UniformBlock fields):
    //   @group(0) @binding(grid.wgsl.binding)      GridUniforms  (uniform, {gridWidth,gridLen})
    //   @group(0) @binding(states[0].wgsl.inBinding)  inState   (read-only-storage)
    //   @group(0) @binding(states[0].wgsl.outBinding) outState  (storage)
    //   @group(0) @binding(layout.wgsl.binding)    UserUniforms  (uniform; present
    //     only when uniformBlock.fields is non-empty -- Selena's WGSL emitter
    //     omits the whole UserUniforms struct+binding when a feedback material
    //     declares no param/context fields at all, e.g. the water "normal"
    //     kernel; see emitFeedback's `if (len(m.Uniforms)>0 || ...)` guard).
    // This is the compute analogue of getSelenaPipeline/createSelenaBindGroup
    // above: same per-material pipeline memo, same content-keyed pipeline
    // cache, same cached-bind-group-pool-by-buffer-identity pattern (so the
    // ping-pong in/out buffer swap each dispatch converges on 2 pooled bind
    // groups per kernel, exactly like the render path's ping-pong texture).
    // -----------------------------------------------------------------------

    var selenaComputePipelineCache = new Map();

    // sceneSelenaComputeGridUniformData packs a feedback material's
    // GridUniforms{gridWidth,gridLen} uniform (8 bytes, 2x u32) straight from
    // the WaterSystem's own resolution/cellCount -- UNLIKE the render path's
    // sceneSelenaGridUniformData (StateGrid{gridWidth,gridHeight}, sourced from
    // renderContext.grid), gridLen here is the total cell COUNT (dispatch
    // bounds check), not a second grid dimension. See design doc 3.4.2/3.5:
    // "resolution/cellCount are supplied to kernels ONLY via GridUniforms, not
    // as context fields".
    function sceneSelenaComputeGridUniformData(system) {
      var resolution = Math.max(1, Math.floor(sceneNumber(system && system.resolution, 1)));
      var cellCount = Math.max(1, Math.floor(sceneNumber(system && system.cellCount, resolution * resolution)));
      return new Uint32Array([resolution, cellCount]);
    }

    // sceneSelenaComputeBindGroupLayout builds the @group(0) bind-group layout
    // for a feedback material's descriptor: grid uniform, in/out storage
    // buffers, and (when present) the UserUniforms uniform. Mirrors
    // sceneSelenaBindGroupLayout's structure but for the feedback binding
    // contract (no textures/vertex attributes/mesh-state exist on a feedback
    // material -- only grid+state+one optional uniform block).
    function sceneSelenaComputeBindGroupLayout(device, layout) {
      var computeVisibility = typeof GPUShaderStage !== "undefined" ? GPUShaderStage.COMPUTE : 4;
      var entries = [];
      var grid = layout && layout.grid;
      if (grid && grid.wgsl) {
        entries.push({
          binding: sceneNumber(grid.wgsl.binding, 0),
          visibility: computeVisibility,
          buffer: { type: "uniform", minBindingSize: 8 },
        });
      }
      var state = sceneSelenaStateDescriptors(layout)[0];
      if (state) {
        var stateWGSL = state.wgsl || {};
        entries.push({
          binding: sceneNumber(stateWGSL.inBinding, 1),
          visibility: computeVisibility,
          buffer: { type: "read-only-storage" },
        });
        var outBinding = sceneNumber(stateWGSL.outBinding, -1);
        if (outBinding >= 0) {
          entries.push({
            binding: outBinding,
            visibility: computeVisibility,
            buffer: { type: "storage" },
          });
        }
      }
      if (layout && layout.uniformBlock && Array.isArray(layout.uniformBlock.fields) && layout.uniformBlock.fields.length > 0) {
        entries.push({
          binding: sceneNumber(layout.wgsl && layout.wgsl.binding, 3),
          visibility: computeVisibility,
          buffer: { type: "uniform", minBindingSize: Math.max(16, Math.floor(sceneNumber(layout.uniformBlock.size, 16))) },
        });
      }
      return device.createBindGroupLayout({ label: "gosx-selena-compute-material", entries: entries });
    }

    // getSelenaComputePipeline mirrors getSelenaPipeline (mesh/mesh+state
    // render kind) for a `kind feedback` Selena material: one @compute entry
    // point (layout.entryPoints.compute, always "computeMain" per
    // emitFeedback), no blend/depth/format/sampleCount variance to key on (a
    // compute pipeline has none of those), so the per-material memo is simpler
    // than the render path's.
    function getSelenaComputePipeline(material) {
      if (!sceneSelenaIsMaterial(material)) return null;
      var memo = material._gosxWGPUSelenaComputeResource;
      if (memo) return memo.failed ? null : memo.resource;
      var layout = sceneSelenaMaterialLayout(material);
      var shader = sceneSelenaWGSLSource(material);
      var key = ["selena-compute", shader, JSON.stringify(layout)].join("|");
      var cached = selenaComputePipelineCache.get(key);
      if (cached) {
        material._gosxWGPUSelenaComputeResource = { resource: cached.failed ? null : cached, failed: !!cached.failed };
        return cached.failed ? null : cached;
      }
      try {
        var bindGroupLayout = sceneSelenaComputeBindGroupLayout(device, layout);
        var pipelineLayout = device.createPipelineLayout({ bindGroupLayouts: [bindGroupLayout] });
        var module = device.createShaderModule({ label: "selena-compute-material", code: shader });
        renderTruth().captureShaderInfo(module, "selena-compute-material");
        var entryPoint = (layout.entryPoints && layout.entryPoints.compute) || "computeMain";
        var pipeline = device.createComputePipeline({
          label: "gosx-selena-compute-" + (layout.material || "material"),
          layout: pipelineLayout,
          compute: { module: module, entryPoint: entryPoint },
        });
        cached = { pipeline: pipeline, bindGroupLayout: bindGroupLayout, layout: layout };
        selenaComputePipelineCache.set(key, cached);
        material._gosxWGPUSelenaComputeResource = { resource: cached, failed: false };
        return cached;
      } catch (err) {
        console.warn("[gosx] Selena WebGPU compute pipeline failed; falling back to hardcoded kernel.", err);
        selenaComputePipelineCache.set(key, { failed: true });
        material._gosxWGPUSelenaComputeResource = { resource: null, failed: true };
        return null;
      }
    }

    // createSelenaComputeBindGroup builds the @group(0) bind group for one
    // feedback-kind dispatch: the grid uniform (from system.resolution/
    // cellCount), the ping-pong in/out storage buffers (inBuf/outBuf, the
    // WaterSystem's bufferA/bufferB in whichever order this step reads/writes),
    // and (when the descriptor declares any) the UserUniforms uniform packed
    // via the SAME sceneSelenaUniformData the render path uses -- so the G1
    // array-uniform packing fix (context{spheres}/context{drops}) applies here
    // for free. renderContext.uniforms carries the per-kernel per-frame value
    // map (see sceneWaterSeedSelenaRenderContext etc. below); `system` is both
    // the live-buffer cache owner (mirrors createSelenaBindGroup's cacheOwner)
    // and the source of the grid dimensions.
    function createSelenaComputeBindGroup(system, resource, inBuf, outBuf, renderContext) {
      if (!system || !resource || !inBuf || !outBuf) return null;
      var layout = resource.layout;
      if (!layout) return null;
      var state = sceneSelenaStateDescriptors(layout)[0];
      if (!state) return null;
      var stateWGSL = state.wgsl || {};
      var grid = layout.grid;
      var uniformSlot = sceneSelenaUniformBufferSlot(renderContext);
      var entries = [];
      var cacheBuffers = [inBuf, outBuf];
      if (grid && grid.wgsl) {
        var gridData = sceneSelenaComputeGridUniformData(system);
        var gridBuffer = wgpuCachedTrackedBuffer(
          system,
          uniformSlot + "_grid",
          gridData,
          GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
          true
        );
        entries.push({ binding: sceneNumber(grid.wgsl.binding, 0), resource: { buffer: gridBuffer } });
        cacheBuffers.push(gridBuffer);
      }
      entries.push({ binding: sceneNumber(stateWGSL.inBinding, 1), resource: { buffer: inBuf } });
      entries.push({ binding: sceneNumber(stateWGSL.outBinding, 2), resource: { buffer: outBuf } });
      var hasUniforms = layout.uniformBlock && Array.isArray(layout.uniformBlock.fields) && layout.uniformBlock.fields.length > 0;
      if (hasUniforms) {
        var uniformData = sceneSelenaUniformData({ shaderLayout: layout }, system, renderContext, selenaFrame);
        if (!uniformData) return null;
        var uniformBuffer = wgpuCachedTrackedBuffer(
          system,
          uniformSlot,
          uniformData,
          GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
          true
        );
        entries.push({ binding: sceneNumber(layout.wgsl && layout.wgsl.binding, 3), resource: { buffer: uniformBuffer } });
        cacheBuffers.push(uniformBuffer);
      }
      // Memoize the bind group exactly like createSelenaBindGroup: a pool
      // keyed by buffer identity (inBuf/outBuf/gridBuffer/uniformBuffer), so
      // the ping-pong swap converges on (at most) 2 stable bind groups per
      // kernel per system instead of allocating one every dispatch.
      var bgPoolSlot = "_gosxWGPUSCBGC" + uniformSlot;
      var pool = system[bgPoolSlot];
      if (!Array.isArray(pool)) { pool = []; system[bgPoolSlot] = pool; }
      for (var pi = 0; pi < pool.length; pi++) {
        var pc = pool[pi];
        if (!pc || pc.device !== device || pc.buffers.length !== cacheBuffers.length) continue;
        var match = true;
        for (var bi = 0; bi < cacheBuffers.length && match; bi++) {
          if (pc.buffers[bi] !== cacheBuffers[bi]) match = false;
        }
        if (match) return pc.bg;
      }
      var newComputeBG = device.createBindGroup({ layout: resource.bindGroupLayout, entries: entries });
      if (pool.length >= 4) pool.shift();
      pool.push({ device: device, buffers: cacheBuffers.slice(), bg: newComputeBG });
      return newComputeBG;
    }

    // -----------------------------------------------------------------------
    // Generic Selena "post" kind render path (getSelenaPostPipeline /
    // createSelenaPostBindGroup): the mesh/mesh+state generic path above
    // (getSelenaPipeline / createSelenaBindGroup) cannot draw a kind:"post"
    // Selena material -- its fixed @group(0) contract (binding 0=sceneColor
    // texture, 1=its sampler, 2=sceneDepth texture, 3=its sampler, 4=the
    // UserUniforms uniform block; see the "post" kind comment on
    // wgpuCreateSelenaPostBGL/getSelenaPostBGL above) differs from the mesh
    // kind's plain "uniform block first" layout, and post-kind pipelines never
    // have vertex buffers or a depth-stencil attachment: they draw a
    // hand-rolled 3-vertex fullscreen triangle from @builtin(vertex_index),
    // matching every post-kind WGSL Selena emits (emitPost's
    // `_postPositions`/`_postUVs` arrays).
    //
    // getSelenaWaterPostBGL() below builds the exact bind group layout every
    // kind:"post" material needs (the SAME fixed contract
    // wgpuCreatePostProcessor's private getSelenaPostBGL/buildCustomPostPipelineAsync
    // use for the main-scene custom-post-effect chain, duplicated here because
    // that factory's closure isn't reachable from water's earlier render
    // phase -- see the comment on getSelenaWaterPostBGL); this generalizes it
    // into a SYNC, memoized, target-format-configurable pipeline builder so a
    // water-system-owned RTT pass (object-shadow/compound-shadow, rendered
    // into their own small offscreen target -- NOT the main swapchain) can
    // use it exactly like getSelenaPipeline is used for mesh-kind passes.
    //
    // Materials with additional textures/storage/state beyond the fixed 5
    // bindings are out of scope (no current post-kind water material needs
    // one; the water surface/caustics context arrays route through the
    // MESH-kind path instead) -- createSelenaPostBindGroup only ever writes
    // the fixed 5 entries.
    var selenaPostPipelineCache = new Map();

    // getSelenaWaterPostBGL/getSelenaWaterPostDepthSampler are LOCAL
    // equivalents of wgpuCreatePostProcessor's private getSelenaPostBGL/
    // getDepthSampler (defined inside that SEPARATE factory function's own
    // closure, at module scope above -- NOT reachable from here: postProcessor
    // itself is only lazily constructed inside render() when postFX effects
    // are present, long after the water passes below need a post-kind bind
    // group layout). The fixed 5-entry contract is identical (see the
    // wgpuCreatePostProcessor comment above); duplicated here rather than
    // threading a lazily-created object through water's earlier render phase.
    var selenaWaterPostBGL = null;
    function getSelenaWaterPostBGL() {
      if (!selenaWaterPostBGL) {
        selenaWaterPostBGL = device.createBindGroupLayout({
          label: "gosx-selena-water-post",
          entries: [
            { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
            { binding: 1, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, sampler: {} },
            { binding: 2, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "depth" } },
            { binding: 3, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
            { binding: 4, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
          ],
        });
      }
      return selenaWaterPostBGL;
    }

    var selenaWaterPostDepthSampler = null;
    function getSelenaWaterPostDepthSampler() {
      if (!selenaWaterPostDepthSampler) {
        selenaWaterPostDepthSampler = device.createSampler({ magFilter: "nearest", minFilter: "nearest" });
      }
      return selenaWaterPostDepthSampler;
    }

    function getSelenaPostPipeline(material, options) {
      if (!sceneSelenaIsMaterial(material)) return null;
      var layout = sceneSelenaMaterialLayout(material);
      if (!layout || layout.kind !== "post") return null;
      var pipelineTargetFormat = options && options.targetFormat ? options.targetFormat : targetFormat;
      var pipelineLabelSuffix = options && options.labelSuffix ? String(options.labelSuffix) + "-" : "";
      var memo = material._gosxWGPUSelenaPostResource;
      if (memo && memo.targetFormat === pipelineTargetFormat) {
        return memo.failed ? null : memo.resource;
      }
      var shader = sceneSelenaWGSLSource(material);
      var key = ["selena-post", shader, JSON.stringify(layout), pipelineTargetFormat].join("|");
      var cached = selenaPostPipelineCache.get(key);
      if (cached) {
        material._gosxWGPUSelenaPostResource = {
          targetFormat: pipelineTargetFormat,
          resource: cached.failed ? null : cached,
          failed: !!cached.failed,
        };
        return cached.failed ? null : cached;
      }
      try {
        var bgl = getSelenaWaterPostBGL();
        var pipelineLayout = device.createPipelineLayout({ bindGroupLayouts: [bgl] });
        var module = device.createShaderModule({ label: "selena-post-material", code: shader });
        renderTruth().captureShaderInfo(module, "selena-post-material");
        var pipeline = device.createRenderPipeline({
          label: "gosx-selena-post-" + pipelineLabelSuffix + (layout.material || "material"),
          layout: pipelineLayout,
          vertex: { module: module, entryPoint: "vertexMain", buffers: [] },
          fragment: { module: module, entryPoint: "fragmentMain", targets: [{ format: pipelineTargetFormat }] },
          primitive: { topology: "triangle-list" },
        });
        cached = { pipeline: pipeline, bindGroupLayout: bgl, layout: layout };
        selenaPostPipelineCache.set(key, cached);
        material._gosxWGPUSelenaPostResource = { targetFormat: pipelineTargetFormat, resource: cached, failed: false };
        return cached;
      } catch (err) {
        console.warn("[gosx] Selena WebGPU post pipeline failed; falling back.", err);
        selenaPostPipelineCache.set(key, { failed: true });
        material._gosxWGPUSelenaPostResource = { targetFormat: pipelineTargetFormat, resource: null, failed: true };
        return null;
      }
    }

    // createSelenaPostBindGroup mirrors createSelenaBindGroup's uniform
    // packing + bind-group memoization for the fixed post-kind @group(0)
    // contract. renderContext.sceneColorView/sceneDepthView let a FUTURE
    // post-kind water pass sample the real rendered scene; today's
    // object-shadow/compound-shadow materials don't reference
    // _sceneColorTex/_sceneDepthTex in their WGSL body at all (every @group/
    // @binding var Selena's "post" kind emits is module-scope, but neither
    // material's vertexMain/fragmentMain calls into them -- confirmed by
    // TestWaterSelenaWGSLValidatesWithNaga's naga validation of the emitted
    // WGSL), so the placeholder views are never actually sampled -- they exist
    // purely to satisfy the fixed bind group layout's entry count/kind.
    function createSelenaPostBindGroup(material, resource, cacheOwner, renderContext) {
      var uniformData = sceneSelenaUniformData(material, cacheOwner, renderContext, selenaFrame);
      if (!uniformData || !resource) return null;
      var owner = (cacheOwner && typeof cacheOwner === "object") ? cacheOwner : material;
      var uniformSlot = sceneSelenaUniformBufferSlot(renderContext) + "_post";
      var uniformBuffer = wgpuCachedTrackedBuffer(
        owner,
        uniformSlot,
        uniformData,
        GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
        true
      );
      var sceneColorView = (renderContext && renderContext.sceneColorView) || placeholderView;
      var sceneDepthView = (renderContext && renderContext.sceneDepthView) || dummyShadowView;
      var bgPoolSlot = "_gosxWGPUSPBGC" + uniformSlot;
      var pool = owner[bgPoolSlot];
      if (!Array.isArray(pool)) { pool = []; owner[bgPoolSlot] = pool; }
      for (var pi = 0; pi < pool.length; pi++) {
        var pc = pool[pi];
        if (!pc || pc.device !== device || pc.uniformBuffer !== uniformBuffer) continue;
        if (pc.sceneColorView !== sceneColorView || pc.sceneDepthView !== sceneDepthView) continue;
        return pc.bg;
      }
      var newBG = device.createBindGroup({
        label: "gosx-selena-post-bg",
        layout: resource.bindGroupLayout,
        entries: [
          { binding: 0, resource: sceneColorView },
          { binding: 1, resource: linearSampler },
          { binding: 2, resource: sceneDepthView },
          { binding: 3, resource: getSelenaWaterPostDepthSampler() },
          { binding: 4, resource: { buffer: uniformBuffer } },
        ],
      });
      if (pool.length >= 4) pool.shift();
      pool.push({ device: device, uniformBuffer: uniformBuffer, sceneColorView: sceneColorView, sceneDepthView: sceneDepthView, bg: newBG });
      return newBG;
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

    // -----------------------------------------------------------------------
    // Authored Points / ComputeParticle render pipelines
    // -----------------------------------------------------------------------
    // Per-layer/system authored pipeline caches. Each cache entry is either a
    // {pipeline} object or {failed:true} sentinel. Failure is one-shot: once
    // an authored shader fails validation the layer falls back to builtin for
    // the rest of the session with a single console.warn.
    //
    // Binding contract for authored Points layers (drawPointsEntries):
    //   @group(0) @binding(0)  FrameUniforms (uniform)
    //   @group(1) @binding(0)  UserUniforms  (authored uniforms, uniform)
    //   @group(2) @binding(0)  PointsUniforms (uniform)
    //   vertex buffer slot 0: per-instance (position:vec3f, size:f32, color:vec4f, stride=32)
    //
    // Binding contract for authored ComputeParticle render (drawComputeParticleEntries):
    //   @group(0) @binding(0)  FrameUniforms (uniform)
    //   @group(1) @binding(0)  UserUniforms  (authored uniforms, uniform)
    //   @group(2) @binding(0)  PointsUniforms (uniform)
    //   @group(2) @binding(1)  particles array<ParticleInstance> (storage read)
    //   no vertex buffers (instance index reads from storage)

    // buildAuthoredPointsVertexPipeline: for Points layers, uses vertex buffer (instanced path).
    function buildAuthoredPointsVertexPipelineAsync(entry, blendMode, depthWrite, systemID) {
      // The joined cache key embeds both WGSL sources (~13 KB). This function
      // runs per layer per frame, and rebuilding the key allocated ~29 MB/s
      // of transient strings across 19 layers. Memoize the trimmed sources
      // and the key on the entry; the raw source identities plus the
      // pipeline parameters validate the memo. Reusing one key string also
      // keeps the Map lookup cheap (the string hash is computed once).
      var rawVert = (typeof entry.customVertexWGSL === "string") ? entry.customVertexWGSL : "";
      var rawFrag = (typeof entry.customFragmentWGSL === "string") ? entry.customFragmentWGSL : "";
      var memo = entry._gosxWGPUPointsPipelineKeyMemo;
      if (!memo || memo.rawVert !== rawVert || memo.rawFrag !== rawFrag ||
          memo.blendMode !== blendMode || memo.depthWrite !== depthWrite ||
          memo.targetFormat !== targetFormat || memo.sampleCount !== activeSampleCount) {
        var trimmedVert = rawVert.trim();
        var trimmedFrag = rawFrag.trim();
        memo = {
          rawVert: rawVert,
          rawFrag: rawFrag,
          blendMode: blendMode,
          depthWrite: depthWrite,
          targetFormat: targetFormat,
          sampleCount: activeSampleCount,
          vertWGSL: trimmedVert,
          fragWGSL: trimmedFrag,
          key: (trimmedVert && trimmedFrag)
            ? [trimmedVert, trimmedFrag, blendMode, depthWrite ? "1" : "0", targetFormat, activeSampleCount].join("|")
            : "",
        };
        entry._gosxWGPUPointsPipelineKeyMemo = memo;
      }
      var vertWGSL = memo.vertWGSL;
      var fragWGSL = memo.fragWGSL;
      if (!vertWGSL || !fragWGSL) return null; // no authored shader
      var cacheKey = memo.key;
      var cached = pointsAuthoredPipelineCache.get(cacheKey);
      if (cached) return cached.failed ? null : cached;

      var pending = { pending: true };
      pointsAuthoredPipelineCache.set(cacheKey, pending);
      var scopedDevice = device;
      if (!scopedDevice) {
        pointsAuthoredPipelineCache.delete(cacheKey);
        return null;
      }
      var vertMod = scopedDevice.createShaderModule({ label: "points-authored-vert", code: vertWGSL });
      renderTruth().captureShaderInfo(vertMod, "points-authored-vert");
      var fragMod = scopedDevice.createShaderModule({ label: "points-authored-frag", code: fragWGSL });
      renderTruth().captureShaderInfo(fragMod, "points-authored-frag");

      function markFailed(reason) {
        sceneReportPipelineFailure("points", systemID, reason);
        if (!pointsAuthoredLayerFailed.get(systemID)) {
          pointsAuthoredLayerFailed.set(systemID, true);
          console.warn("[gosx] Points authored pipeline failed for layer '" + systemID + "'; falling back to builtin.", reason);
        }
        pointsAuthoredPipelineCache.set(cacheKey, { failed: true });
      }

      // This is the site the mis-pairing bug hit hardest: 19 point layers on
      // one page build here, all overlapping, all previously sharing one
      // device error scope stack. Validation is now per object.
      scopedDevice.createRenderPipelineAsync({
        label: "gosx-points-authored-" + blendMode,
        layout: pointsAuthoredVertexPipelineLayout,
        vertex: { module: vertMod, entryPoint: "vertexMain", buffers: WGPU_POINTS_INSTANCE_VERTEX_LAYOUT },
        fragment: { module: fragMod, entryPoint: "fragmentMain", targets: [{ format: targetFormat, blend: wgpuBlendState(blendMode) }] },
        primitive: { topology: "triangle-list" },
        multisample: { count: Math.max(1, Math.floor(activeSampleCount || 1)) },
        depthStencil: { format: "depth24plus", depthWriteEnabled: depthWrite, depthCompare: "less-equal" },
      }).then(function(pipeline) {
        return sceneShaderModuleError([vertMod, fragMod]).then(function(compileErr) {
          if (!rendererDeviceStillActive(scopedDevice)) return;
          if (compileErr) {
            markFailed(compileErr);
            return;
          }
          pointsAuthoredPipelineCache.set(cacheKey, { pipeline: pipeline });
        });
      }).catch(function(err) {
        return sceneShaderModuleError([vertMod, fragMod]).then(function(compileErr) {
          if (!rendererDeviceStillActive(scopedDevice)) return;
          markFailed(compileErr || err);
        });
      });
      return null; // pending first frame — builtin fallback used
    }

    // buildAuthoredParticleRenderPipelineAsync: for ComputeParticles render, reads from storage.
    function buildAuthoredParticleRenderPipelineAsync(entry, blendMode, depthWrite, systemID) {
      // Same per-frame key memoization as buildAuthoredPointsVertexPipelineAsync.
      var rawVert = (typeof entry.renderVertexWGSL === "string") ? entry.renderVertexWGSL : "";
      var rawFrag = (typeof entry.renderFragmentWGSL === "string") ? entry.renderFragmentWGSL : "";
      var memo = entry._gosxWGPUCPRenderPipelineKeyMemo;
      if (!memo || memo.rawVert !== rawVert || memo.rawFrag !== rawFrag ||
          memo.blendMode !== blendMode || memo.depthWrite !== depthWrite ||
          memo.targetFormat !== targetFormat || memo.sampleCount !== activeSampleCount) {
        var trimmedVert = rawVert.trim();
        var trimmedFrag = rawFrag.trim();
        memo = {
          rawVert: rawVert,
          rawFrag: rawFrag,
          blendMode: blendMode,
          depthWrite: depthWrite,
          targetFormat: targetFormat,
          sampleCount: activeSampleCount,
          vertWGSL: trimmedVert,
          fragWGSL: trimmedFrag,
          key: (trimmedVert && trimmedFrag)
            ? ["cr", trimmedVert, trimmedFrag, blendMode, depthWrite ? "1" : "0", targetFormat, activeSampleCount].join("|")
            : "",
        };
        entry._gosxWGPUCPRenderPipelineKeyMemo = memo;
      }
      var vertWGSL = memo.vertWGSL;
      var fragWGSL = memo.fragWGSL;
      if (!vertWGSL || !fragWGSL) return null;
      var cacheKey = memo.key;
      var cached = pointsAuthoredPipelineCache.get(cacheKey);
      if (cached) return cached.failed ? null : cached;

      // Selena points modules may expose dual entries:
      //   vertexStorageMain — reads particle state from a storage buffer (preferred for
      //                       ComputeParticles render path which has no vertex buffers)
      //   vertexMain        — attribute variant (fallback)
      // Check the WGSL source first; also accept shaderLayout.entryPoints.vertexStorage.
      var vertEntry = "vertexMain";
      var renderLayout = entry.renderShaderLayout && typeof entry.renderShaderLayout === "object"
        ? entry.renderShaderLayout
        : entry.shaderLayout;
      if (vertWGSL.indexOf("vertexStorageMain") !== -1) {
        vertEntry = "vertexStorageMain";
      } else if (renderLayout && renderLayout.entryPoints && renderLayout.entryPoints.vertexStorage) {
        vertEntry = renderLayout.entryPoints.vertexStorage;
      }

      var pending = { pending: true };
      pointsAuthoredPipelineCache.set(cacheKey, pending);
      var scopedDevice = device;
      if (!scopedDevice) {
        pointsAuthoredPipelineCache.delete(cacheKey);
        return null;
      }
      var vertMod = scopedDevice.createShaderModule({ label: "particle-render-authored-vert", code: vertWGSL });
      renderTruth().captureShaderInfo(vertMod, "particle-render-authored-vert");
      var fragMod = scopedDevice.createShaderModule({ label: "particle-render-authored-frag", code: fragWGSL });
      renderTruth().captureShaderInfo(fragMod, "particle-render-authored-frag");

      function markFailed(reason) {
        sceneReportPipelineFailure("particle-render", systemID, reason);
        if (!pointsAuthoredLayerFailed.get(systemID)) {
          pointsAuthoredLayerFailed.set(systemID, true);
          console.warn("[gosx] ComputeParticle authored render pipeline failed for system '" + systemID + "'; falling back to builtin.", reason);
        }
        pointsAuthoredPipelineCache.set(cacheKey, { failed: true });
      }

      // Per-object validation; no device error scope. This build overlaps the
      // compute kernel build for the SAME system, which is exactly the pairing
      // that reported a rejected kernel against a healthy render pipeline.
      scopedDevice.createRenderPipelineAsync({
        label: "gosx-particle-render-authored-" + blendMode,
        layout: pointsAuthoredStoragePipelineLayout,
        vertex: { module: vertMod, entryPoint: vertEntry, buffers: [] },
        fragment: { module: fragMod, entryPoint: "fragmentMain", targets: [{ format: targetFormat, blend: wgpuBlendState(blendMode) }] },
        primitive: { topology: "triangle-list" },
        multisample: { count: Math.max(1, Math.floor(activeSampleCount || 1)) },
        depthStencil: { format: "depth24plus", depthWriteEnabled: depthWrite, depthCompare: "less-equal" },
      }).then(function(pipeline) {
        return sceneShaderModuleError([vertMod, fragMod]).then(function(compileErr) {
          if (!rendererDeviceStillActive(scopedDevice)) return;
          if (compileErr) {
            markFailed(compileErr);
            return;
          }
          pointsAuthoredPipelineCache.set(cacheKey, { pipeline: pipeline });
        });
      }).catch(function(err) {
        return sceneShaderModuleError([vertMod, fragMod]).then(function(compileErr) {
          if (!rendererDeviceStillActive(scopedDevice)) return;
          markFailed(compileErr || err);
        });
      });
      return null;
    }

    // ensurePointsAuthoredUserUniformBuffer: allocates / updates a per-layer
    // user-uniform buffer from entry.customUniforms and shaderLayout.
    function ensurePointsAuthoredUserUniformBuffer(entry, ownerKey, uniforms, layout) {
      var uniformData = sceneSelenaUniformData({ customUniforms: uniforms, shaderLayout: layout }, null, null, selenaFrame);
      if (!uniformData || uniformData.byteLength === 0) {
        // No user uniforms — create a minimal 16-byte placeholder so group(1) is always bound.
        uniformData = new Float32Array(4);
      }
      var cacheOwner = entry;
      var buffer = wgpuCachedTrackedBuffer(
        cacheOwner,
        ownerKey,
        uniformData,
        GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
        true
      );
      return buffer;
    }

    function disposeComputeParticleSystems() {
      for (const record of computeParticleSystems.values()) {
        if (record && record.system && typeof record.system.dispose === "function") {
          try { record.system.dispose(); } catch (_err) {}
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

    function sceneWaterResolution(value) {
      var raw = Math.floor(sceneNumber(value, 256));
      if (!Number.isFinite(raw) || raw <= 0) raw = 256;
      // A sampled render-state texture is refreshed with copyBufferToTexture.
      // rgba32float rows are 16 bytes/texel and WebGPU requires bytesPerRow to
      // be 256-byte aligned, so GPU water grids use 16-texel increments.
      return Math.max(16, Math.min(512, Math.ceil(raw / 16) * 16));
    }

    function sceneWaterSurfaceResolution(entry, simulationResolution) {
      var raw = Math.floor(sceneNumber(entry && entry.surfaceResolution, simulationResolution));
      if (!Number.isFinite(raw) || raw < 2) raw = simulationResolution;
      return Math.max(2, Math.min(512, raw));
    }

    function sceneWaterCausticsResolution(entry) {
      var raw = Math.floor(sceneNumber(entry && entry.causticsResolution, WATER_CAUSTICS_TEXTURE_SIZE));
      if (!Number.isFinite(raw) || raw <= 0) raw = WATER_CAUSTICS_TEXTURE_SIZE;
      return Math.max(64, Math.min(2048, raw));
    }

    function sceneWaterObjectTextureResolution(entry) {
      var raw = Math.floor(sceneNumber(entry && entry.objectTextureResolution, WATER_OBJECT_TEXTURE_SIZE));
      if (!Number.isFinite(raw) || raw <= 0) raw = WATER_OBJECT_TEXTURE_SIZE;
      return Math.max(64, Math.min(2048, raw));
    }

    function sceneWaterObjectTextureResolutionMode(entry) {
      var mode = typeof (entry && entry.objectTextureResolutionMode) === "string"
        ? entry.objectTextureResolutionMode.trim().toLowerCase()
        : "";
      if (mode === "auto" || mode === "upstream") return "viewport";
      if (mode === "viewport") return "viewport";
      return "fixed";
    }

    function sceneWaterObjectTexturePixelBudget(entry) {
      var raw = Math.floor(sceneNumber(entry && entry.objectTexturePixelBudget, 0));
      if (!Number.isFinite(raw) || raw <= 0) return 0;
      return Math.max(WATER_OBJECT_TEXTURE_TARGET_COUNT, raw);
    }

    function sceneWaterObjectTextureClampToPixelBudget(size, pixelBudget) {
      var width = Math.max(1, Math.floor(sceneNumber(size && size.width, WATER_OBJECT_TEXTURE_SIZE)));
      var height = Math.max(1, Math.floor(sceneNumber(size && size.height, WATER_OBJECT_TEXTURE_SIZE)));
      var budget = Math.max(0, Math.floor(sceneNumber(pixelBudget, 0)));
      var totalPixels = width * height * WATER_OBJECT_TEXTURE_TARGET_COUNT;
      if (budget > 0 && totalPixels > budget) {
        var scale = Math.sqrt(budget / totalPixels);
        width = Math.max(1, Math.floor(width * scale));
        height = Math.max(1, Math.floor(height * scale));
      }
      return {
        mode: size && size.mode || "fixed",
        width: width,
        height: height,
        resolution: Math.max(width, height),
        pixelBudget: budget,
      };
    }

    function sceneWaterObjectTextureTargetSize(entry, width, height) {
      var mode = sceneWaterObjectTextureResolutionMode(entry);
      var pixelBudget = sceneWaterObjectTexturePixelBudget(entry);
      if (mode === "viewport") {
        var targetWidth = Math.max(1, Math.floor(sceneNumber(width, WATER_OBJECT_TEXTURE_SIZE)));
        var targetHeight = Math.max(1, Math.floor(sceneNumber(height, WATER_OBJECT_TEXTURE_SIZE)));
        var maxSide = Math.max(targetWidth, targetHeight, 1);
        var scale = Math.min(1, WATER_OBJECT_TEXTURE_MAX_SIZE / maxSide);
        targetWidth = Math.max(1, Math.floor(targetWidth * scale));
        targetHeight = Math.max(1, Math.floor(targetHeight * scale));
        return sceneWaterObjectTextureClampToPixelBudget({
          mode: mode,
          width: targetWidth,
          height: targetHeight,
          resolution: Math.max(targetWidth, targetHeight),
        }, pixelBudget);
      }
      var fixed = sceneWaterObjectTextureResolution(entry);
      return sceneWaterObjectTextureClampToPixelBudget({ mode: mode, width: fixed, height: fixed, resolution: fixed }, pixelBudget);
    }

    function sceneWaterObjectShadowResolution(entry) {
      var raw = Math.floor(sceneNumber(entry && entry.objectShadowResolution, WATER_OBJECT_SHADOW_TEXTURE_SIZE));
      if (!Number.isFinite(raw) || raw <= 0) raw = WATER_OBJECT_SHADOW_TEXTURE_SIZE;
      return Math.max(64, Math.min(2048, raw));
    }

    function sceneWaterVector3(value, fallback) {
      var fb = fallback || { x: 0, y: 1, z: 0 };
      if (Array.isArray(value)) {
        return {
          x: sceneNumber(value[0], fb.x),
          y: sceneNumber(value[1], fb.y),
          z: sceneNumber(value[2], fb.z),
        };
      }
      if (value && typeof value === "object") {
        return {
          x: sceneNumber(value.x, fb.x),
          y: sceneNumber(value.y, fb.y),
          z: sceneNumber(value.z, fb.z),
        };
      }
      return { x: fb.x, y: fb.y, z: fb.z };
    }

    function sceneWaterLightVector(entry, fallback) {
      var fb = fallback || { x: 0.3, y: 0.9, z: 0.45 };
      if (entry && typeof entry === "object") {
        if (entry.lightDirection != null) {
          return sceneWaterVector3(entry.lightDirection, fb);
        }
        if (
          Object.prototype.hasOwnProperty.call(entry, "lightDirectionX") ||
          Object.prototype.hasOwnProperty.call(entry, "lightDirectionY") ||
          Object.prototype.hasOwnProperty.call(entry, "lightDirectionZ")
        ) {
          return {
            x: sceneNumber(entry.lightDirectionX, fb.x),
            y: sceneNumber(entry.lightDirectionY, fb.y),
            z: sceneNumber(entry.lightDirectionZ, fb.z),
          };
        }
      }
      return sceneWaterVector3(null, fb);
    }

    function sceneWaterObjectKind(entry) {
      var raw = "";
      if (entry && typeof entry.objectKind === "string" && entry.objectKind) {
        raw = entry.objectKind;
      } else if (entry && typeof entry.activeObject === "string" && entry.activeObject) {
        raw = entry.activeObject;
      }
      var value = String(raw || "").trim().toLowerCase();
      if (!value || value === "none" || value === "no_object") return 0;
      if (value.indexOf("sphere") >= 0 || value.indexOf("ball") >= 0) return 1;
      if (value.indexOf("cube") >= 0 || value.indexOf("box") >= 0) return 2;
      if (value.indexOf("compound") >= 0 || value.indexOf("mesh") >= 0 || value.indexOf("torus") >= 0 || value.indexOf("duck") >= 0) return 3;
      return 0;
    }

    function sceneWaterObjectSubtype(entry, kind) {
      if (!entry || kind !== 3) return 0;
      var raw = [
        entry.objectSubtype,
        entry.activeObject,
        entry.label,
        entry.id,
        entry.src,
        entry.objectKind,
      ].filter(Boolean).join(" ").toLowerCase();
      if (raw.indexOf("torus") >= 0 || raw.indexOf("knot") >= 0) return 1;
      if (raw.indexOf("duck") >= 0 || raw.indexOf("mesh") >= 0 || raw.indexOf("gltf") >= 0 || raw.indexOf("glb") >= 0) return 2;
      return 0;
    }

    function sceneWaterDisplacementSphereSignature(spheres) {
      if (!Array.isArray(spheres) || spheres.length === 0) return "";
      return spheres.slice(0, WATER_MAX_DISPLACEMENT_SPHERES).map(function(sphere) {
        if (Array.isArray(sphere)) {
          return [
            sceneNumber(sphere[0], 0).toFixed(5),
            sceneNumber(sphere[1], 0).toFixed(5),
            sceneNumber(sphere[2], 0).toFixed(5),
            sceneNumber(sphere[3], 0).toFixed(5),
          ].join(",");
        }
        var offset = sphere && sphere.offset && typeof sphere.offset === "object" ? sphere.offset : {};
        return [
          sceneNumber(sphere && Object.prototype.hasOwnProperty.call(sphere, "offsetX") ? sphere.offsetX : offset.x, 0).toFixed(5),
          sceneNumber(sphere && Object.prototype.hasOwnProperty.call(sphere, "offsetY") ? sphere.offsetY : offset.y, 0).toFixed(5),
          sceneNumber(sphere && Object.prototype.hasOwnProperty.call(sphere, "offsetZ") ? sphere.offsetZ : offset.z, 0).toFixed(5),
          sceneNumber(sphere && sphere.radius, 0).toFixed(5),
        ].join(",");
      }).join(";");
    }

    function sceneWaterObjectMotionSignature(entry, kind) {
      if (!entry || !kind) return "";
      return [
        kind,
        sceneNumber(entry.objectX, 0).toFixed(5),
        sceneNumber(entry.objectY, 0).toFixed(5),
        sceneNumber(entry.objectZ, 0).toFixed(5),
        sceneBool(entry.objectPreviousSet, false) ? "1" : "0",
        sceneNumber(entry.objectPreviousX, 0).toFixed(5),
        sceneNumber(entry.objectPreviousY, 0).toFixed(5),
        sceneNumber(entry.objectPreviousZ, 0).toFixed(5),
        sceneNumber(entry.poolWidth, 1).toFixed(5),
        sceneNumber(entry.poolLength, 1).toFixed(5),
        sceneNumber(entry.objectRadius, 0).toFixed(5),
        sceneNumber(entry.objectHalfSizeX, 0).toFixed(5),
        sceneNumber(entry.objectHalfSizeY, 0).toFixed(5),
        sceneNumber(entry.objectHalfSizeZ, 0).toFixed(5),
        sceneNumber(entry.objectDriftX, 0).toFixed(5),
        sceneNumber(entry.objectDriftY, 0).toFixed(5),
        sceneNumber(entry.objectDriftZ, 0).toFixed(5),
        sceneNumber(entry.objectBobAmplitude, 0).toFixed(5),
        sceneNumber(entry.objectBobSpeed, 0).toFixed(5),
        sceneNumber(entry.objectDisplacementScale, 1).toFixed(5),
        sceneWaterObjectSubtype(entry, kind),
        sceneWaterDisplacementSphereSignature(entry.objectDisplacementSpheres),
      ].join("|");
    }

    function sceneWaterObjectExplicitPreviousSignature(entry, kind) {
      if (!entry || !kind || !sceneBool(entry.objectPreviousSet, false)) return "";
      return [
        kind,
        sceneNumber(entry.objectPreviousX, 0).toFixed(5),
        sceneNumber(entry.objectPreviousY, 0).toFixed(5),
        sceneNumber(entry.objectPreviousZ, 0).toFixed(5),
        sceneNumber(entry.objectX, 0).toFixed(5),
        sceneNumber(entry.objectY, 0).toFixed(5),
        sceneNumber(entry.objectZ, 0).toFixed(5),
      ].join("|");
    }

    function sceneWaterObjectCenter(entry, timeSeconds) {
      var time = sceneNumber(timeSeconds, 0);
      var bobSpeed = sceneNumber(entry && entry.objectBobSpeed, 0);
      var bob = Math.sin(time * (bobSpeed > 0 ? bobSpeed : 1)) * sceneNumber(entry && entry.objectBobAmplitude, 0);
      return {
        x: sceneNumber(entry && entry.objectX, 0) + Math.sin(time * 0.73) * sceneNumber(entry && entry.objectDriftX, 0),
        y: sceneNumber(entry && entry.objectY, 0) + bob + Math.sin(time * 0.41) * sceneNumber(entry && entry.objectDriftY, 0),
        z: sceneNumber(entry && entry.objectZ, 0) + Math.cos(time * 0.67) * sceneNumber(entry && entry.objectDriftZ, 0),
      };
    }

    function sceneWaterNormalizeObjectCenter(center, poolWidth, poolLength) {
      var halfWidth = Math.max(0.0001, poolWidth);
      var halfLength = Math.max(0.0001, poolLength);
      return {
        x: sceneNumber(center && center.x, 0) / halfWidth,
        y: sceneNumber(center && center.y, 0),
        z: sceneNumber(center && center.z, 0) / halfLength,
      };
    }

    function sceneWaterDisplacementSpheres(entry, poolWidth, poolLength) {
      var source = entry && Array.isArray(entry.objectDisplacementSpheres) ? entry.objectDisplacementSpheres : [];
      if (source.length === 0) return [];
      var halfWidth = Math.max(0.0001, poolWidth);
      var halfLength = Math.max(0.0001, poolLength);
      var out = [];
      for (var i = 0; i < source.length && out.length < WATER_MAX_DISPLACEMENT_SPHERES; i++) {
        var raw = source[i];
        var offset = raw && raw.offset && typeof raw.offset === "object" ? raw.offset : {};
        var x = 0;
        var y = 0;
        var z = 0;
        var radius = 0;
        if (Array.isArray(raw)) {
          x = sceneNumber(raw[0], 0);
          y = sceneNumber(raw[1], 0);
          z = sceneNumber(raw[2], 0);
          radius = sceneNumber(raw[3], 0);
        } else {
          x = sceneNumber(raw && Object.prototype.hasOwnProperty.call(raw, "offsetX") ? raw.offsetX : offset.x, 0);
          y = sceneNumber(raw && Object.prototype.hasOwnProperty.call(raw, "offsetY") ? raw.offsetY : offset.y, 0);
          z = sceneNumber(raw && Object.prototype.hasOwnProperty.call(raw, "offsetZ") ? raw.offsetZ : offset.z, 0);
          radius = sceneNumber(raw && raw.radius, 0);
        }
        if (radius <= 0) continue;
        out.push({
          x: x / halfWidth,
          y: y,
          z: z / halfLength,
          radius: Math.max(0.0001, radius) / halfLength,
        });
      }
      return out;
    }

    function sceneWaterObjectState(system, entry, timeSeconds, poolWidth, poolLength) {
      var kind = sceneWaterObjectKind(entry);
      if (!kind) {
        if (system) {
          system.waterObjectMoved = system.waterObjectActive === true;
          system.waterObjectKind = 0;
          system.waterObjectLabel = "";
          system.waterObjectActive = false;
          system.waterObjectSphereCount = 0;
          system.waterObjectHalfSize = { x: 0, y: 0, z: 0 };
          system.waterObjectCenter = { x: 0, y: 0, z: 0 };
          system.waterObjectDisplacementScale = 0;
          system.waterObjectSpheres = [];
        }
        return {
          kind: 0,
          center: { x: 0, y: 0, z: 0 },
          previous: { x: 0, y: 0, z: 0 },
          halfSize: { x: 0, y: 0, z: 0 },
          radius: 0,
          displacementScale: 0,
          subtype: 0,
          spheres: [],
        };
      }
      var currentWorld = sceneWaterObjectCenter(entry, timeSeconds);
      var current = sceneWaterNormalizeObjectCenter(currentWorld, poolWidth, poolLength);
      var signature = sceneWaterObjectMotionSignature(entry, kind);
      var lastCenter = system && system.waterObjectPrevious;
      var objectMoved = !lastCenter || system.waterObjectSignature !== signature ||
        current.x !== lastCenter.x || current.y !== lastCenter.y || current.z !== lastCenter.z;
      var previous = current;
      var explicitPreviousSignature = sceneWaterObjectExplicitPreviousSignature(entry, kind);
      if (system && explicitPreviousSignature && system.waterObjectExplicitPreviousSignature !== explicitPreviousSignature) {
        previous = sceneWaterNormalizeObjectCenter({
          x: sceneNumber(entry && entry.objectPreviousX, currentWorld.x),
          y: sceneNumber(entry && entry.objectPreviousY, currentWorld.y),
          z: sceneNumber(entry && entry.objectPreviousZ, currentWorld.z),
        }, poolWidth, poolLength);
        system.waterObjectExplicitPreviousSignature = explicitPreviousSignature;
      } else if (system && system.waterObjectSignature === signature && system.waterObjectPrevious) {
        previous = system.waterObjectPrevious;
      }
      var halfWidth = Math.max(0.0001, poolWidth);
      var halfLength = Math.max(0.0001, poolLength);
      var radius = sceneNumber(entry && entry.objectRadius, 0);
      if (radius <= 0) radius = kind === 1 ? 0.25 : 0.31;
      var halfSizeX = sceneNumber(entry && entry.objectHalfSizeX, 0);
      var halfSizeY = sceneNumber(entry && entry.objectHalfSizeY, 0);
      var halfSizeZ = sceneNumber(entry && entry.objectHalfSizeZ, 0);
      if (kind === 2) {
        if (halfSizeX <= 0) halfSizeX = radius;
        if (halfSizeY <= 0) halfSizeY = radius;
        if (halfSizeZ <= 0) halfSizeZ = radius;
      }
      var spheres = kind === 3 ? sceneWaterDisplacementSpheres(entry, poolWidth, poolLength) : [];
      var subtype = sceneWaterObjectSubtype(entry, kind);
      var active = kind === 1 || kind === 2 || spheres.length > 0;
      var normalizedHalfSize = {
        x: Math.max(0, halfSizeX) / halfWidth,
        y: Math.max(0, halfSizeY),
        z: Math.max(0, halfSizeZ) / halfLength,
      };
      var normalizedDisplacementScale = Math.max(0, sceneNumber(entry && entry.objectDisplacementScale, 1));
      if (system) {
        system.waterObjectMoved = objectMoved;
        system.waterObjectSignature = signature;
        system.waterObjectPrevious = current;
        system.waterObjectKind = active ? kind : 0;
        system.waterObjectActive = active;
        system.waterObjectSphereCount = spheres.length;
        system.waterObjectSubtype = active ? subtype : 0;
        system.waterObjectRadius = active ? Math.max(0.0001, radius) : 0;
        system.waterObjectLabel = kind === 1 ? "sphere" : kind === 2 ? "cube" : subtype === 1 ? "torus-knot" : subtype === 2 ? "mesh" : "compound";
        // Selena general-pass render-context support (surface/surfaceBelow/
        // caustics/compound-shadow context fields: objectHalf/objectCenter/
        // spheres[]): cache these the same way waterObjectRadius/waterObjectKind
        // already are, so per-pass Selena render-context builders read live
        // per-frame values instead of recomputing this function's math.
        system.waterObjectHalfSize = normalizedHalfSize;
        system.waterObjectCenter = current;
        system.waterObjectDisplacementScale = normalizedDisplacementScale;
        system.waterObjectSpheres = spheres;
      }
      return {
        kind: kind,
        center: current,
        previous: previous,
        halfSize: normalizedHalfSize,
        radius: Math.max(0.0001, radius) / halfLength,
        displacementScale: normalizedDisplacementScale,
        subtype: subtype,
        spheres: spheres,
      };
    }

    function sceneWaterObjectDisplacementEvents(entry) {
      var source = entry && Array.isArray(entry.objectDisplacementEvents) ? entry.objectDisplacementEvents : [];
      return source.filter(function(event) { return event && typeof event === "object"; });
    }

    function sceneWaterObjectDisplacementEventID(event) {
      return Math.max(0, Math.floor(sceneNumber(event && event.id, 0)));
    }

    function sceneWaterObjectDisplacementEventEntry(entry, event) {
      var next = Object.assign({}, entry || {}, event || {});
      next.objectPreviousSet = true;
      return next;
    }

    function dispatchWaterObjectDisplacementEvents(system, entry, encoder, pipeline, currentTime) {
      if (!system || !encoder || !pipeline) {
        return { dispatches: 0, selena: 0, selenaFallback: 0, lastID: Math.max(0, Math.floor(sceneNumber(system && system.lastObjectDisplacementEventID, 0))) };
      }
      var events = sceneWaterObjectDisplacementEvents(entry);
      if (!events.length) {
        return { dispatches: 0, selena: 0, selenaFallback: 0, lastID: Math.max(0, Math.floor(sceneNumber(system.lastObjectDisplacementEventID, 0))) };
      }
      var lastID = Math.max(0, Math.floor(sceneNumber(system.lastObjectDisplacementEventID, 0)));
      var nextLastID = lastID;
      var dispatches = 0;
      var selenaDispatches = 0;
      var selenaFallbacks = 0;
      for (var i = 0; i < events.length; i++) {
        var event = events[i];
        var id = sceneWaterObjectDisplacementEventID(event);
        if (id <= lastID) continue;
        var eventEntry = sceneWaterObjectDisplacementEventEntry(entry, event);
        var kind = sceneWaterObjectKind(eventEntry);
        if (!kind) {
          nextLastID = Math.max(nextLastID, id);
          continue;
        }
        device.queue.writeBuffer(system.uniformBuffer, 0, sceneWaterUniformData(system, eventEntry, 0, currentTime, { transientObject: true }));
        // Routed through dispatchWaterComputeStage (not dispatchWaterPass
        // directly) so a per-event displacement dispatch ALSO gets the
        // descriptor-driven Selena compute path, using system.
        // _waterComputeObjectState -- freshly stashed by the
        // sceneWaterUniformData call immediately above -- for this event's
        // own object state (see sceneWaterDisplacementSelenaRenderContext).
        var eventResult = dispatchWaterComputeStage(encoder, system, eventEntry, "displacement", pipeline);
        var eventDispatches = eventResult.dispatches;
        selenaDispatches += eventResult.selena;
        selenaFallbacks += eventResult.selenaFallback;
        if (eventDispatches > 0) {
          dispatches += eventDispatches;
          nextLastID = Math.max(nextLastID, id);
        }
      }
      if (nextLastID > lastID) system.lastObjectDisplacementEventID = nextLastID;
      return { dispatches: dispatches, selena: selenaDispatches, selenaFallback: selenaFallbacks, lastID: nextLastID };
    }

    // dispatchWaterDropEvents mirrors dispatchWaterObjectDisplacementEvents
    // immediately above, but for the queued-drop trail (see
    // sceneManagedFluidObjectQueueDrop in 19b-scene-control-forms.ts): a fast
    // pointer stroke can fire several drops between two rendered frames, and
    // upstream (evanw) injects one per DOM event, so a single-slot scalar
    // (entry.dropEventID/dropX/dropZ) would silently coalesce a burst into
    // one ripple. entry.dropEvents carries every drop queued since the last
    // consumed id; each gets its own per-event uniform write + "drop" compute
    // dispatch, same shape as the per-event displacement path. The legacy
    // scalar dispatch below (entry.dropEventID) stays intact as the fallback
    // for any caller that still only supplies the single-shot fields.
    function sceneWaterDropEvents(entry) {
      var source = entry && Array.isArray(entry.dropEvents) ? entry.dropEvents : [];
      return source.filter(function(event) { return event && typeof event === "object"; });
    }

    function sceneWaterDropEventID(event) {
      return Math.max(0, Math.floor(sceneNumber(event && event.id, 0)));
    }

    // sceneWaterDropEventEntry: unlike sceneWaterObjectDisplacementEventEntry,
    // a queued drop's field names ({id,x,z,radius,strength}) don't line up
    // 1:1 with the entry's drop fields (dropX/dropZ/dropEventRadius/
    // dropEventStrength), so this maps them explicitly instead of a blind
    // Object.assign merge.
    function sceneWaterDropEventEntry(entry, event) {
      var next = Object.assign({}, entry || {});
      next.dropX = Math.max(-1, Math.min(1, sceneNumber(event && event.x, sceneNumber(entry && entry.dropX, 0))));
      next.dropZ = Math.max(-1, Math.min(1, sceneNumber(event && event.z, sceneNumber(entry && entry.dropZ, 0))));
      next.dropEventRadius = Math.max(0.0001, sceneNumber(event && event.radius, sceneNumber(entry && entry.dropEventRadius, sceneNumber(entry && entry.dropRadius, 0.03))));
      next.dropEventStrength = sceneNumber(event && event.strength, sceneNumber(entry && entry.dropEventStrength, sceneNumber(entry && entry.dropStrength, 0.01)));
      return next;
    }

    function dispatchWaterDropEvents(system, entry, encoder, pipeline, currentTime) {
      if (!system || !encoder || !pipeline) {
        return { dispatches: 0, selena: 0, selenaFallback: 0, lastID: Math.max(0, Math.floor(sceneNumber(system && system.lastDropEventID, 0))) };
      }
      var events = sceneWaterDropEvents(entry);
      if (!events.length) {
        return { dispatches: 0, selena: 0, selenaFallback: 0, lastID: Math.max(0, Math.floor(sceneNumber(system.lastDropEventID, 0))) };
      }
      var lastID = Math.max(0, Math.floor(sceneNumber(system.lastDropEventID, 0)));
      var nextLastID = lastID;
      var dispatches = 0;
      var selenaDispatches = 0;
      var selenaFallbacks = 0;
      for (var i = 0; i < events.length; i++) {
        var event = events[i];
        var id = sceneWaterDropEventID(event);
        if (id <= lastID) continue;
        var eventEntry = sceneWaterDropEventEntry(entry, event);
        device.queue.writeBuffer(system.uniformBuffer, 0, sceneWaterUniformData(system, eventEntry, 0, currentTime, { transientObject: true }));
        var eventResult = dispatchWaterComputeStage(encoder, system, eventEntry, "drop", pipeline);
        var eventDispatches = eventResult.dispatches;
        selenaDispatches += eventResult.selena;
        selenaFallbacks += eventResult.selenaFallback;
        if (eventDispatches > 0) {
          dispatches += eventDispatches;
          nextLastID = Math.max(nextLastID, id);
        }
      }
      if (nextLastID > lastID) system.lastDropEventID = nextLastID;
      return { dispatches: dispatches, selena: selenaDispatches, selenaFallback: selenaFallbacks, lastID: nextLastID };
    }

    // M6 per-frame churn audit (water-parity-campaign), noted but NOT fixed
    // here: like the uniform "commit" write (see waterUniformSnapshotChanged),
    // this re-uploads objectSphereBuffer unconditionally on EVERY
    // sceneWaterUniformData call (both the per-frame {transientObject:true}
    // pack and the per-tick "commit" pack, i.e. up to twice a tick) even when
    // the compound-sphere layout hasn't moved. Deferring a dedup here rather
    // than bolting on a second ad hoc snapshot comparator: this buffer is
    // consumed as a STORAGE binding (sceneSelenaLiveBuffer's "objectSpheres"
    // case) by more than one pass with different freshness requirements (the
    // continuous per-frame object-displacement dispatch above legitimately
    // needs it live while dragging), and getting that gating wrong silently
    // corrupts a live displacement compute run rather than just staling a
    // cosmetic shimmer term the way the uniform-block skip's fallback-only
    // tradeoff does. Worth a follow-up with dedicated verification, not a
    // same-pass copy-paste of this file's other dedup.
    function sceneWaterWriteObjectSphereBuffer(system, spheres) {
      if (!system || !system.objectSphereBuffer) return;
      waterObjectSphereScratch.fill(0);
      var source = Array.isArray(spheres) ? spheres : [];
      for (var i = 0; i < source.length && i < WATER_MAX_DISPLACEMENT_SPHERES; i++) {
        var sphere = source[i] || {};
        var offset = i * 4;
        waterObjectSphereScratch[offset] = sceneNumber(sphere.x, 0);
        waterObjectSphereScratch[offset + 1] = sceneNumber(sphere.y, 0);
        waterObjectSphereScratch[offset + 2] = sceneNumber(sphere.z, 0);
        waterObjectSphereScratch[offset + 3] = Math.max(0.0001, sceneNumber(sphere.radius, 0));
      }
      device.queue.writeBuffer(system.objectSphereBuffer, 0, waterObjectSphereScratch);
    }

    // sceneWaterSystemSignature decides whether a WaterSystem's GPU resources
    // need to be rebuilt (resolution/texture-size/seed/drop params changed).
    // It no longer folds in any hand-written per-entry WGSL text -- Selena is
    // the sole primary shader source (see the *SelenaWGSL data slots /
    // getWaterPoolSelenaDraw & co. below) ahead of the builtin
    // SCENE_WATER_*_SOURCE fallback, neither of which is entry-authored.
    function sceneWaterSystemSignature(entry, width, height) {
      var resolution = sceneWaterResolution(entry && entry.resolution);
      var authoredSurfaceResolution = sceneWaterSurfaceResolution(entry, resolution);
      return [
        resolution,
        authoredSurfaceResolution,
        Math.max(0, Math.floor(sceneNumber(entry && entry.seedDrops, 7))),
        sceneNumber(entry && entry.dropRadius, 0.03).toFixed(5),
        sceneNumber(entry && entry.dropStrength, 0.01).toFixed(5),
      ].join("|");
    }

    // sceneWaterManifestShaderSources/sceneWaterManifestShaderSourceStats/
    // sceneWaterShaderSourcesFromEntries/sceneHydrateWaterEntriesFromSources
    // remain as a generic bundle/manifest water-source diagnostic layer (they
    // still report on any of the 14 legacy hand-written WGSL field names that
    // happen to show up on a bundle/manifest entry). They are no longer
    // consulted to build/select a render or compute pipeline -- that "authored
    // data-prop WGSL" resolution tier (sceneWaterAuthoredShaderSource and the
    // per-pass sceneWaterAuthored*Pipeline builders) has been removed now that
    // every WebGPU water pass resolves Selena-primary -> builtin-fallback (see
    // getWaterPoolPipeline / getWaterRenderPipeline / renderWaterCausticsPass /
    // renderWaterObjectShadowPass / renderWaterObjectMeshShadowPass /
    // dispatchWaterComputeStage below).
    function sceneWaterManifestShaderSources() {
      if (waterManifestShaderSourcesByID && waterManifestShaderSourcesByID.size > 0) return waterManifestShaderSourcesByID;
      waterManifestShaderSourcesByID = new Map();
      var mountSources = canvas && (canvas.__gosxScene3DWaterShaderSources || (canvas.parentNode && canvas.parentNode.__gosxScene3DWaterShaderSources));
      var published = mountSources || (typeof window !== "undefined" ? window.__gosx_scene3d_water_shader_sources_by_id : null);
      if (published && typeof published === "object") {
        var ids = Object.keys(published);
        for (var pi = 0; pi < ids.length; pi += 1) {
          var publishedRecord = published[ids[pi]];
          if (publishedRecord && typeof publishedRecord === "object") {
            waterManifestShaderSourcesByID.set(ids[pi], publishedRecord);
          }
        }
        if (waterManifestShaderSourcesByID.size > 0) return waterManifestShaderSourcesByID;
      }
      var doc = canvas && canvas.ownerDocument
        ? canvas.ownerDocument
        : (typeof window !== "undefined" && window.document
          ? window.document
          : (typeof document !== "undefined" ? document : null));
      if (!doc || !doc.querySelectorAll) return waterManifestShaderSourcesByID;
      var fields = [
        "seedWGSL", "dropWGSL", "displacementWGSL", "simulationWGSL", "normalWGSL", "causticsWGSL",
        "poolVertexWGSL", "poolFragmentWGSL", "surfaceVertexWGSL", "surfaceFragmentWGSL", "surfaceBelowFragmentWGSL",
        "objectShadowWGSL", "objectMeshShadowVertexWGSL", "objectMeshShadowFragmentWGSL",
      ];
      function ingestManifestValue(manifest) {
        try {
          var engines = manifest && Array.isArray(manifest.engines) ? manifest.engines : [];
          for (var ei = 0; ei < engines.length; ei += 1) {
            var scene = engines[ei] && engines[ei].props && engines[ei].props.scene;
            var systems = scene && Array.isArray(scene.waterSystems) ? scene.waterSystems : [];
            for (var wi = 0; wi < systems.length; wi += 1) {
              var water = systems[wi];
              if (!water || typeof water !== "object") continue;
              var id = typeof water.id === "string" && water.id ? water.id : ("scene-water-" + wi);
              var record = waterManifestShaderSourcesByID.get(id) || {};
              for (var fi = 0; fi < fields.length; fi += 1) {
                var name = fields[fi];
                if (typeof water[name] === "string" && water[name].trim()) record[name] = water[name];
              }
              waterManifestShaderSourcesByID.set(id, record);
            }
          }
        } catch (_err) {}
      }
      function ingestManifestText(text) {
        if (!text || text.indexOf("waterSystems") < 0 || text.indexOf("WGSL") < 0) return;
        try {
          var manifest = JSON.parse(text);
          var engines = manifest && Array.isArray(manifest.engines) ? manifest.engines : [];
          for (var ei = 0; ei < engines.length; ei += 1) {
            var scene = engines[ei] && engines[ei].props && engines[ei].props.scene;
            var systems = scene && Array.isArray(scene.waterSystems) ? scene.waterSystems : [];
            for (var wi = 0; wi < systems.length; wi += 1) {
              var water = systems[wi];
              if (!water || typeof water !== "object") continue;
              var id = typeof water.id === "string" && water.id ? water.id : ("scene-water-" + wi);
              var record = waterManifestShaderSourcesByID.get(id) || {};
              for (var fi = 0; fi < fields.length; fi += 1) {
                var name = fields[fi];
                if (typeof water[name] === "string" && water[name].trim()) record[name] = water[name];
              }
              waterManifestShaderSourcesByID.set(id, record);
            }
          }
        } catch (_err) {}
      }
      var manifestScript = doc.getElementById ? doc.getElementById("gosx-manifest") : null;
      // The bootstrap runtime publishes its manifest parse; reusing it skips a
      // re-parse, and on a page that opted into data-gosx-release the DOM text
      // is empty and the published parse is the only remaining copy.
      var manifestMemo = typeof window !== "undefined" ? window.__gosx_manifest : null;
      if (manifestScript && manifestMemo && manifestMemo.element === manifestScript && manifestMemo.value) {
        ingestManifestValue(manifestMemo.value);
        if (waterManifestShaderSourcesByID.size > 0) return waterManifestShaderSourcesByID;
      }
      ingestManifestText(manifestScript && manifestScript.textContent || "");
      if (waterManifestShaderSourcesByID.size > 0) return waterManifestShaderSourcesByID;
      var scripts = doc.scripts || doc.querySelectorAll("script");
      for (var si = 0; si < scripts.length; si += 1) {
        ingestManifestText(scripts[si] && scripts[si].textContent || "");
      }
      return waterManifestShaderSourcesByID;
    }

    function sceneWaterManifestShaderSourceStats() {
      var sourceMap = sceneWaterManifestShaderSources();
      var stats = { systems: 0, fields: 0, causticSourceBytes: 0, surfaceSourceBytes: 0 };
      sourceMap.forEach(function(record) {
        stats.systems += 1;
        for (var name in record) {
          if (!Object.prototype.hasOwnProperty.call(record, name)) continue;
          if (typeof record[name] !== "string" || !record[name].trim()) continue;
          stats.fields += 1;
          if (name === "causticsWGSL") {
            stats.causticSourceBytes = Math.max(stats.causticSourceBytes, record[name].trim().length);
          }
        }
        stats.surfaceSourceBytes = Math.max(stats.surfaceSourceBytes, sceneWaterSurfaceSourceBytes(record));
      });
      return stats;
    }

    function sceneWaterShaderSourcesFromEntries(entries) {
      var sourceMap = {};
      var source = Array.isArray(entries) ? entries : [];
      var fields = [
        "seedWGSL", "dropWGSL", "displacementWGSL", "simulationWGSL", "normalWGSL", "causticsWGSL",
        "poolVertexWGSL", "poolFragmentWGSL", "surfaceVertexWGSL", "surfaceFragmentWGSL", "surfaceBelowFragmentWGSL",
        "objectShadowWGSL", "objectMeshShadowVertexWGSL", "objectMeshShadowFragmentWGSL",
      ];
      for (var i = 0; i < source.length; i += 1) {
        var entry = source[i];
        if (!entry || typeof entry !== "object") continue;
        var id = typeof entry.id === "string" && entry.id ? entry.id : ("scene-water-" + i);
        var record = sourceMap[id] || { id: id };
        var changed = false;
        for (var f = 0; f < fields.length; f += 1) {
          var name = fields[f];
          if (typeof entry[name] === "string" && entry[name].trim()) {
            record[name] = entry[name];
            changed = true;
          }
        }
        if (changed) sourceMap[id] = record;
      }
      return sourceMap;
    }

    function sceneHydrateWaterEntriesFromSources(entries, sources) {
      if (!Array.isArray(entries) || !sources || typeof sources !== "object") return entries;
      var keys = Object.keys(sources);
      if (!keys.length) return entries;
      var fields = [
        "seedWGSL", "dropWGSL", "displacementWGSL", "simulationWGSL", "normalWGSL", "causticsWGSL",
        "poolVertexWGSL", "poolFragmentWGSL", "surfaceVertexWGSL", "surfaceFragmentWGSL", "surfaceBelowFragmentWGSL",
        "objectShadowWGSL", "objectMeshShadowVertexWGSL", "objectMeshShadowFragmentWGSL",
      ];
      return entries.map(function(entry, index) {
        if (!entry || typeof entry !== "object") return entry;
        var id = typeof entry.id === "string" && entry.id ? entry.id : ("scene-water-" + index);
        var source = sources[id] || (keys.length === 1 ? sources[keys[0]] : null);
        if (!source || typeof source !== "object") return entry;
        var hydrated = null;
        for (var f = 0; f < fields.length; f += 1) {
          var name = fields[f];
          if (typeof entry[name] === "string" && entry[name].trim()) continue;
          if (typeof source[name] !== "string" || !source[name].trim()) continue;
          if (!hydrated) hydrated = Object.assign({}, entry);
          hydrated[name] = source[name];
        }
        return hydrated || entry;
      });
    }

    // sceneWaterSurfaceSourceBytes is a generic length-summer over whichever
    // legacy hand-written WGSL fields (if any) happen to be present on a
    // bundle/manifest water-source record; retained only for
    // sceneWaterManifestShaderSourceStats' diagnostic byte counters above.
    function sceneWaterSurfaceSourceBytes(record) {
      if (!record || typeof record !== "object") return 0;
      var total = 0;
      [
        "surfaceVertexWGSL",
        "surfaceFragmentWGSL",
        "surfaceBelowFragmentWGSL",
      ].forEach(function(name) {
        if (typeof record[name] === "string" && record[name].trim()) {
          total += record[name].trim().length;
        }
      });
      return total;
    }

    function sceneWaterPoolShapeRounded(entry) {
      if (!entry || typeof entry.poolShape !== "string") return false;
      var value = entry.poolShape.trim().toLowerCase();
      return value === "rounded box" || value === "rounded" || value === "roundbox";
    }

    function sceneWaterOpticsFlags(entry, objectState) {
      return {
        caustics: sceneBool(entry && entry.caustics, true),
        reflection: sceneBool(entry && entry.reflection, true),
        refraction: sceneBool(entry && entry.refraction, true),
        object: !!(objectState && objectState.kind > 0 && objectState.displacementScale > 0),
      };
    }

    function sceneWaterUniformData(system, entry, deltaTime, timeSeconds, options) {
      var transientObject = !!(options && options.transientObject);
      var resolution = system && system.resolution ? system.resolution : sceneWaterResolution(entry && entry.resolution);
      var cellCount = resolution * resolution;
      var light = sceneWaterLightVector(entry, { x: 0.3, y: 0.9, z: 0.45 });
      var lightLen = Math.sqrt(light.x * light.x + light.y * light.y + light.z * light.z) || 1;
      var shallow = sceneColorRGBA(entry && entry.shallowColor, [0.48, 0.82, 0.92, 1]);
      var deep = sceneColorRGBA(entry && entry.deepColor, [0.03, 0.18, 0.34, 1]);
      var poolWidth = Math.max(0.01, sceneNumber(entry && entry.poolWidth, 1.0));
      var poolHeight = Math.max(0.01, sceneNumber(entry && entry.poolHeight, 1.0));
      var poolLength = Math.max(0.01, sceneNumber(entry && entry.poolLength, 1.0));
      var rounded = sceneWaterPoolShapeRounded(entry);
      var maxCornerRadius = Math.max(0, Math.min(poolWidth, poolLength) - 0.001);
      var cornerRadius = rounded ? Math.max(0, Math.min(maxCornerRadius, sceneNumber(entry && entry.cornerRadius, 0))) : 0;
      var objectState = sceneWaterObjectState(transientObject ? null : system, entry, timeSeconds, poolWidth, poolLength);
      var optics = sceneWaterOpticsFlags(entry, objectState);
      if (system) {
        system.waterResolution = resolution;
        system.waterPoolWidth = poolWidth;
        system.waterPoolHeight = poolHeight;
        system.waterPoolLength = poolLength;
        system.waterCornerRadius = cornerRadius;
        system.waterLightDir = { x: light.x / lightLen, y: light.y / lightLen, z: light.z / lightLen };
        // Selena general-pass render-context support (surface/surfaceBelow/
        // caustics context fields: opticsEnable): cache the SAME optics flags
        // object computed for the hand-written WaterUniforms pack, the same
        // way waterLightDir already is, so per-pass Selena render-context
        // builders read it instead of recomputing sceneWaterOpticsFlags.
        system.waterOpticsFlags = optics;
        // Selena feedback-compute render-context support (displacement
        // kernel's objectKind/displacementScale/objectCenter/objectPrevCenter/
        // objectRadius/objectHalfSize/sphereCount/spheres context fields):
        // stash the JUST-COMPUTED objectState verbatim rather than re-deriving
        // it from the waterObject* fields above, which are NOT updated during
        // a one-shot displacement event (sceneWaterObjectState skips all
        // system-field mutation when called with transientObject:true, i.e.
        // system param null). This stash is always fresh at the point a
        // displacement dispatch is issued because every call site invokes
        // sceneWaterUniformData immediately before it (see
        // dispatchWaterObjectDisplacementEvents and the per-frame continuous
        // displacement dispatch in updateWaterSystems).
        system._waterComputeObjectState = objectState;
      }
      waterUniformScratchU[0] = resolution;
      waterUniformScratchU[1] = cellCount;
      waterUniformScratchU[2] = Math.max(0, Math.floor(sceneNumber(entry && entry.seedDrops, 7)));
      waterUniformScratchU[3] = Math.max(0, Math.floor(system && system.frameIndex || 0));
      waterUniformScratchF[4] = Math.max(0, Math.min(0.1, sceneNumber(deltaTime, 0)));
      waterUniformScratchF[5] = sceneNumber(timeSeconds, 0);
      waterUniformScratchF[6] = Math.max(0, Math.min(2, sceneNumber(entry && entry.waveSpeed, 1.0)));
      waterUniformScratchF[7] = Math.max(0, Math.min(1, sceneNumber(entry && entry.damping, 0.995)));
      waterUniformScratchF[8] = Math.max(0.0001, Math.min(0.5, sceneNumber(entry && entry.dropRadius, 0.03)));
      waterUniformScratchF[9] = Math.max(-1, Math.min(1, sceneNumber(entry && entry.dropStrength, 0.01)));
      waterUniformScratchF[10] = Math.max(0.01, Math.min(16, sceneNumber(entry && entry.normalScale, 1.0)));
      waterUniformScratchF[11] = poolWidth;
      waterUniformScratchF[12] = poolHeight;
      waterUniformScratchF[13] = poolLength;
      waterUniformScratchF[14] = cornerRadius;
      waterUniformScratchF[15] = rounded ? 1 : 0;
      waterUniformScratchF[16] = light.x / lightLen;
      waterUniformScratchF[17] = light.y / lightLen;
      waterUniformScratchF[18] = light.z / lightLen;
      waterUniformScratchF[19] = 1;
      waterUniformScratchF[20] = shallow[0];
      waterUniformScratchF[21] = shallow[1];
      waterUniformScratchF[22] = shallow[2];
      waterUniformScratchF[23] = shallow[3];
      waterUniformScratchF[24] = deep[0];
      waterUniformScratchF[25] = deep[1];
      waterUniformScratchF[26] = deep[2];
      waterUniformScratchF[27] = deep[3];
      waterUniformScratchF[28] = objectState.center.x;
      waterUniformScratchF[29] = objectState.center.y;
      waterUniformScratchF[30] = objectState.center.z;
      waterUniformScratchF[31] = 1;
      waterUniformScratchF[32] = objectState.previous.x;
      waterUniformScratchF[33] = objectState.previous.y;
      waterUniformScratchF[34] = objectState.previous.z;
      waterUniformScratchF[35] = 1;
      waterUniformScratchF[36] = objectState.halfSize.x;
      waterUniformScratchF[37] = objectState.halfSize.y;
      waterUniformScratchF[38] = objectState.halfSize.z;
      waterUniformScratchF[39] = objectState.radius;
      waterUniformScratchF[40] = objectState.kind;
      waterUniformScratchF[41] = objectState.displacementScale;
      waterUniformScratchF[42] = Math.min(WATER_MAX_DISPLACEMENT_SPHERES, objectState.spheres ? objectState.spheres.length : 0);
      waterUniformScratchF[43] = objectState.subtype || 0;
      waterUniformScratchF[44] = optics.caustics ? 1 : 0;
      waterUniformScratchF[45] = optics.reflection ? 1 : 0;
      waterUniformScratchF[46] = optics.refraction ? 1 : 0;
      waterUniformScratchF[47] = optics.object ? 1 : 0;
      waterUniformScratchF[48] = Math.max(-1, Math.min(1, sceneNumber(entry && entry.dropX, 0)));
      waterUniformScratchF[49] = Math.max(-1, Math.min(1, sceneNumber(entry && entry.dropZ, 0)));
      waterUniformScratchF[50] = Math.max(0.0001, Math.min(0.5, sceneNumber(entry && entry.dropEventRadius, sceneNumber(entry && entry.dropRadius, 0.03))));
      waterUniformScratchF[51] = Math.max(-1, Math.min(1, sceneNumber(entry && entry.dropEventStrength, sceneNumber(entry && entry.dropStrength, 0.01))));
      waterUniformScratchF[52] = Math.max(0, sceneNumber(system && system.seedSalt, 0));
      sceneWaterWriteObjectSphereBuffer(system, objectState.spheres);
      return waterUniformScratch;
    }

    // M6 per-frame churn audit (water-parity-campaign). The "commit" write in
    // updateWaterSystems (device.queue.writeBuffer(system.uniformBuffer, 0,
    // sceneWaterUniformData(...)), inside its `if (hasSimulationTick)` block)
    // re-packs and re-uploads this 256-byte WaterUniforms block every
    // simulation tick, even when the only thing that changed is the header:
    // word indices [0,6) are resolution/cellCount/seedDrops/frameIndex/
    // deltaTime/timeSeconds (WATER_UNIFORM_VOLATILE_WORDS), which are either
    // near-static or -- frameIndex/deltaTime/timeSeconds -- LITERALLY always
    // different every tick by construction. waterUniformSnapshotChanged
    // compares everything FROM word index 6 onward (waveSpeed, damping, drop
    // params, pool/light/shallow/deep-color/object/optics/drop-event fields,
    // seedSalt, plus any future field appended to the struct) against the
    // system's last-written snapshot using raw Uint32 bit comparison (NaN-safe,
    // no float-equality edge cases), and the caller skips the GPU writeBuffer
    // call entirely when nothing in that range moved -- which, per M5's
    // at-rest gating, is the common case once a system settles (config/object
    // state stays put for many consecutive ticks even though the clock keeps
    // advancing).
    //
    // Deliberately NOT applied to the OTHER writeBuffer call at this same call
    // site's sibling (the `{transientObject: true}` pack immediately above the
    // hasSimulationTick block): that write primes uniforms a one-shot drop/
    // displacement compute dispatch reads in the SAME frame it's issued, so
    // it's write-then-immediately-consumed, not a steady-state re-upload.
    //
    // Tradeoff, documented rather than silently accepted: system.uniformBuffer
    // is read ONLY by the hand-written fallback compute/render pipelines
    // (createWaterComputeBindGroup/createWaterRenderBindGroup/
    // createWaterCausticsBindGroup bind binding 0 to it) -- every Selena
    // render pass gets its own uniform buffer via sceneSelenaUniformData with
    // its own live `time` context field (selenaFrame.time), unaffected by
    // this skip. Selena is the sole primary path (the demo's own tests assert
    // zero authored/fallback occurrences in the golden path), so skipping this
    // upload is a no-op almost always; IF a backend ever falls all the way
    // through to the hand-written pipelines, a skipped tick leaves that
    // fallback's params.time-driven effects (e.g. the caustics fallback
    // fragment's shimmer term) one tick stale until the next real change --
    // a minor cosmetic staleness in an already-degraded emergency tier, not a
    // functional break.
    function waterUniformSnapshotChanged(system) {
      if (!system) return true;
      var last = system.waterUniformLastWords;
      if (!last || last.length !== waterUniformScratchU.length) {
        system.waterUniformLastWords = new Uint32Array(waterUniformScratchU.length);
        system.waterUniformLastWords.set(waterUniformScratchU);
        return true;
      }
      var changed = false;
      for (var i = WATER_UNIFORM_VOLATILE_WORDS; i < waterUniformScratchU.length; i++) {
        if (last[i] !== waterUniformScratchU[i]) { changed = true; break; }
      }
      if (changed) last.set(waterUniformScratchU);
      return changed;
    }

    function createWaterComputeBindGroup(system, readBuffer, writeBuffer) {
      return device.createBindGroup({
        label: "gosx-water-compute-bg",
        layout: waterComputeBindGroupLayout,
        entries: [
          { binding: 0, resource: { buffer: system.uniformBuffer } },
          { binding: 1, resource: { buffer: readBuffer } },
          { binding: 2, resource: { buffer: writeBuffer } },
          { binding: 3, resource: { buffer: system.objectSphereBuffer } },
        ],
      });
    }

    function createWaterRenderBindGroup(system, buffer) {
      var entry = system && system.entry || {};
      var cubeRecord = entry.cubeMap ? wgpuLoadCubeTexture(device, entry.cubeMap, textureCache) : null;
      var cubeLoaded = Boolean(cubeRecord && cubeRecord.loaded && cubeRecord.view);
      var cubePending = Boolean(cubeRecord && cubeRecord.pending && !cubeRecord.loaded && !cubeRecord.failed);
      var cubeFailed = Boolean(cubeRecord && cubeRecord.failed);
      var tileURL = typeof entry.tileTexture === "string" ? entry.tileTexture.trim() : "";
      var tileRecord = tileURL ? wgpuLoadTexture(device, tileURL, textureCache) : null;
      var tileLoaded = Boolean(tileRecord && tileRecord.loaded && tileRecord.view);
      var tilePending = Boolean(tileRecord && tileRecord.pending && !tileRecord.loaded && !tileRecord.failed);
      var tileFailed = Boolean(tileRecord && tileRecord.failed);
      if (system) {
        system.waterSkyCubeRequested = !!(entry && entry.cubeMap);
        system.waterSkyCubeLoaded = cubeLoaded;
        system.waterSkyCubePending = cubePending;
        system.waterSkyCubeFailed = cubeFailed;
        system.waterSurfaceTileRequested = !!tileURL;
        system.waterSurfaceTileLoaded = tileLoaded;
        system.waterSurfaceTilePending = tilePending;
        system.waterSurfaceTileFailed = tileFailed;
      }
      return device.createBindGroup({
        label: "gosx-water-render-bg",
        layout: waterRenderBindGroupLayout,
        entries: [
          { binding: 0, resource: { buffer: system.uniformBuffer } },
          { binding: 1, resource: { buffer: buffer } },
          { binding: 2, resource: linearSampler },
          { binding: 3, resource: system.causticsView || placeholderView },
          { binding: 4, resource: system.objectReflectionView || placeholderView },
          { binding: 5, resource: system.objectClippedReflectionView || placeholderView },
          { binding: 6, resource: system.objectRefractionView || placeholderView },
          { binding: 7, resource: cubeRecord && cubeRecord.view ? cubeRecord.view : placeholderCubeView },
          { binding: 8, resource: { buffer: system.objectTextureMatrixBuffer } },
          { binding: 9, resource: tileLoaded ? tileRecord.view : placeholderView },
          { binding: 10, resource: { buffer: system.objectSphereBuffer } },
        ],
      });
    }

    // Per-frame leak guard: the surface (cubemap path) and pool bind groups were
    // rebuilt EVERY frame, and WebGPU bind groups have no destroy() (GC-only), so
    // at 60fps they accumulated until the device OOM'd ("not enough memory left").
    // Their resources are stable per-system except the ping-pong buffer (2 variants)
    // and the async sky-cube / tile views — so cache 2 variants and rebuild only
    // when those async views change (a handful of times, not per frame).
    function getWaterRenderBindGroupCached(system) {
      var entry = system && system.entry || {};
      if (!entry.cubeMap) return system.renderBindGroups[system.activeIndex];
      var cubeRecord = wgpuLoadCubeTexture(device, entry.cubeMap, textureCache);
      var tileURL = typeof entry.tileTexture === "string" ? entry.tileTexture.trim() : "";
      var tileRecord = tileURL ? wgpuLoadTexture(device, tileURL, textureCache) : null;
      var cubeView = (cubeRecord && cubeRecord.view) || null;
      var tileView = (tileRecord && tileRecord.view) || null;
      var cache = system._cubemapRenderBindGroups;
      if (!cache || cache.cubeView !== cubeView || cache.tileView !== tileView) {
        cache = system._cubemapRenderBindGroups = {
          cubeView: cubeView,
          tileView: tileView,
          groups: [
            createWaterRenderBindGroup(system, system.bufferA),
            createWaterRenderBindGroup(system, system.bufferB),
          ],
        };
      }
      return cache.groups[system.activeIndex];
    }

    function getWaterPoolBindGroupCached(system) {
      if (!system) return null;
      var entry = system.entry || {};
      var tileURL = typeof entry.tileTexture === "string" ? entry.tileTexture.trim() : "";
      var tileRecord = tileURL ? wgpuLoadTexture(device, tileURL, textureCache) : null;
      var tileView = (tileRecord && tileRecord.view) || null;
      var cache = system._poolBindGroups;
      if (!cache || cache.tileView !== tileView) {
        cache = system._poolBindGroups = {
          tileView: tileView,
          groups: [
            createWaterPoolBindGroup(system, system.bufferA),
            createWaterPoolBindGroup(system, system.bufferB),
          ],
        };
      }
      return cache.groups[system.activeIndex];
    }

    function writeWaterObjectTextureMatrices(system) {
      if (!system || !system.objectTextureMatrixBuffer) return;
      var viewMatrix = system.objectViewProjectionReady ? system.objectViewProjectionMatrix : null;
      waterObjectTextureMatrixScratch.set(viewMatrix || scratchSelenaViewProjection, 0);
      var reflectionMatrix = system.objectReflectionViewProjectionReady ? system.objectReflectionViewProjectionMatrix : null;
      waterObjectTextureMatrixScratch.set(reflectionMatrix || scratchSelenaViewProjection, 16);
      device.queue.writeBuffer(system.objectTextureMatrixBuffer, 0, waterObjectTextureMatrixScratch);
    }

    function createWaterPoolBindGroup(system, buffer) {
      if (!system) return null;
      var activeBuffer = buffer || (system.activeIndex === 0 ? system.bufferA : system.bufferB);
      var entry = system.entry || {};
      var tileURL = typeof entry.tileTexture === "string" ? entry.tileTexture.trim() : "";
      var tileRecord = tileURL ? wgpuLoadTexture(device, tileURL, textureCache) : null;
      var tileLoaded = Boolean(tileRecord && tileRecord.loaded && tileRecord.view);
      var tilePending = Boolean(tileRecord && tileRecord.pending && !tileRecord.loaded && !tileRecord.failed);
      var tileFailed = Boolean(tileRecord && tileRecord.failed);
      system.waterPoolTileRequested = !!tileURL;
      system.waterPoolTileLoaded = tileLoaded;
      system.waterPoolTilePending = tilePending;
      system.waterPoolTileFailed = tileFailed;
      return device.createBindGroup({
        label: "gosx-water-pool-bg",
        layout: waterPoolBindGroupLayout,
        entries: [
          { binding: 0, resource: { buffer: system.uniformBuffer } },
          { binding: 1, resource: { buffer: activeBuffer } },
          { binding: 2, resource: waterTileSampler || linearSampler },
          { binding: 3, resource: system.causticsView || placeholderView },
          { binding: 4, resource: system.objectShadowView || placeholderView },
          { binding: 5, resource: tileLoaded ? tileRecord.view : placeholderView },
        ],
      });
    }

    function createWaterCausticsBindGroup(system, buffer) {
      return device.createBindGroup({
        label: "gosx-water-caustics-bg",
        layout: waterCausticsBindGroupLayout,
        entries: [
          { binding: 0, resource: { buffer: system.uniformBuffer } },
          { binding: 1, resource: { buffer: buffer } },
          { binding: 2, resource: { buffer: system.objectSphereBuffer } },
          { binding: 3, resource: linearSampler },
          { binding: 4, resource: system.objectShadowView || placeholderView },
        ],
      });
    }

    function createWaterObjectTextureBindGroup(system) {
      return device.createBindGroup({
        label: "gosx-water-object-textures-bg",
        layout: waterObjectTextureBindGroupLayout,
        entries: [
          { binding: 0, resource: { buffer: system.uniformBuffer } },
          { binding: 1, resource: { buffer: system.objectSphereBuffer } },
        ],
      });
    }

    function createWaterObjectMeshShadowBindGroup(system) {
      if (!system || !system.objectMeshShadowUniformBuffer || !waterObjectMeshShadowBindGroupLayout) return null;
      return device.createBindGroup({
        label: "gosx-water-object-mesh-shadow-bg",
        layout: waterObjectMeshShadowBindGroupLayout,
        entries: [
          { binding: 0, resource: { buffer: system.objectMeshShadowUniformBuffer } },
        ],
      });
    }

    function sceneWaterObjectMeshShadowUniformData(system) {
      var entry = system && system.entry || {};
      var light = sceneWaterLightVector(entry, { x: 0.3, y: 0.9, z: 0.45 });
      var lightLen = Math.sqrt(light.x * light.x + light.y * light.y + light.z * light.z) || 1;
      var poolWidth = Math.max(0.001, sceneNumber(entry && entry.poolWidth, 1.0));
      var poolLength = Math.max(0.001, sceneNumber(entry && entry.poolLength, 1.0));
      waterObjectMeshShadowUniformScratch[0] = light.x / lightLen;
      waterObjectMeshShadowUniformScratch[1] = light.y / lightLen;
      waterObjectMeshShadowUniformScratch[2] = light.z / lightLen;
      waterObjectMeshShadowUniformScratch[3] = 1;
      waterObjectMeshShadowUniformScratch[4] = Math.max(0.0001, poolWidth);
      waterObjectMeshShadowUniformScratch[5] = Math.max(0.0001, poolLength);
      waterObjectMeshShadowUniformScratch[6] = 0;
      waterObjectMeshShadowUniformScratch[7] = 0;
      return waterObjectMeshShadowUniformScratch;
    }

    function createSceneWaterSystem(scopedDevice, entry, width, height) {
      var resolution = sceneWaterResolution(entry && entry.resolution);
      var authoredSurfaceResolution = sceneWaterSurfaceResolution(entry, resolution);
      var causticsResolution = sceneWaterCausticsResolution(entry);
      var objectTextureSize = sceneWaterObjectTextureTargetSize(entry, width, height);
      var objectTextureWidth = objectTextureSize.width;
      var objectTextureHeight = objectTextureSize.height;
      var objectTextureResolution = objectTextureSize.resolution;
      var objectShadowResolution = sceneWaterObjectShadowResolution(entry);
      var cellCount = resolution * resolution;
      var stateBytes = cellCount * 16;
      var stateBufferUsage = GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST | GPUBufferUsage.COPY_SRC;
      var bufferA = wgpuCreateTrackedBuffer(stateBufferUsage, stateBytes);
      var bufferB = wgpuCreateTrackedBuffer(stateBufferUsage, stateBytes);
      var stateTextureUsage = GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST;
      var stateTextureA = scopedDevice.createTexture({
        label: "gosx-water-state-sampled-a",
        size: [resolution, resolution, 1],
        format: "rgba32float",
        usage: stateTextureUsage,
      });
      var stateTextureB = scopedDevice.createTexture({
        label: "gosx-water-state-sampled-b",
        size: [resolution, resolution, 1],
        format: "rgba32float",
        usage: stateTextureUsage,
      });
      var uniformBuffer = wgpuCreateTrackedBuffer(GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST, 256);
      var objectSphereBuffer = wgpuCreateTrackedBuffer(GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST, WATER_MAX_DISPLACEMENT_SPHERES * 16);
      var objectTextureMatrixBuffer = wgpuCreateTrackedBuffer(GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST, 128);
      var objectMeshShadowUniformBuffer = wgpuCreateTrackedBuffer(GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST, 32);
      var causticsTexture = scopedDevice.createTexture({
        label: "gosx-water-caustics-target",
        size: [causticsResolution, causticsResolution, 1],
        format: WATER_CAUSTICS_TEXTURE_FORMAT,
        usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST,
      });
      var causticsView = causticsTexture.createView();
      var objectReflectionTexture = scopedDevice.createTexture({
        label: "gosx-water-object-reflection-target",
        size: [objectTextureWidth, objectTextureHeight, 1],
        format: WATER_OBJECT_TEXTURE_FORMAT,
        usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
      });
      var objectClippedReflectionTexture = scopedDevice.createTexture({
        label: "gosx-water-object-clipped-reflection-target",
        size: [objectTextureWidth, objectTextureHeight, 1],
        format: WATER_OBJECT_TEXTURE_FORMAT,
        usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
      });
      var objectRefractionTexture = scopedDevice.createTexture({
        label: "gosx-water-object-refraction-target",
        size: [objectTextureWidth, objectTextureHeight, 1],
        format: WATER_OBJECT_TEXTURE_FORMAT,
        usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
      });
      var objectTextureDepthTexture = scopedDevice.createTexture({
        label: "gosx-water-object-texture-depth",
        size: [objectTextureWidth, objectTextureHeight, 1],
        format: "depth24plus",
        usage: GPUTextureUsage.RENDER_ATTACHMENT,
      });
      var objectShadowTexture = scopedDevice.createTexture({
        label: "gosx-water-object-shadow-target",
        size: [objectShadowResolution, objectShadowResolution, 1],
        format: WATER_OBJECT_TEXTURE_FORMAT,
        usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
      });
      var system = {
        entry: entry,
        resolution: resolution,
        qualityTier: "full",
        qualityRevision: 0,
        qualityDPRCap: 1,
        qualityAllocationPending: false,
        qualityAllocationFailures: 0,
        qualityAllocationConsecutiveFailures: 0,
        qualityAllocationNextFrame: 0,
        qualityAllocationDesiredKey: "",
        authoredSurfaceResolution: authoredSurfaceResolution,
        surfaceResolution: authoredSurfaceResolution,
        expensivePassCadence: 1,
        causticsResolution: causticsResolution,
        objectTextureResolution: objectTextureResolution,
        objectTextureWidth: objectTextureWidth,
        objectTextureHeight: objectTextureHeight,
        objectTextureResolutionMode: objectTextureSize.mode,
        objectTexturePixelBudget: objectTextureSize.pixelBudget,
        objectShadowResolution: objectShadowResolution,
        cellCount: cellCount,
        vertexCount: Math.max(0, (authoredSurfaceResolution - 1) * (authoredSurfaceResolution - 1) * 6),
        bufferA: bufferA,
        bufferB: bufferB,
        stateTextureA: stateTextureA,
        stateTextureB: stateTextureB,
        stateTextureViewA: stateTextureA.createView(),
        stateTextureViewB: stateTextureB.createView(),
        stateTextureSyncSeq: 0,
        uniformBuffer: uniformBuffer,
        objectSphereBuffer: objectSphereBuffer,
        objectTextureMatrixBuffer: objectTextureMatrixBuffer,
        objectViewProjectionMatrix: new Float32Array(16),
        objectViewProjectionReady: false,
        objectReflectionViewProjectionMatrix: new Float32Array(16),
        objectReflectionViewProjectionReady: false,
        objectMeshShadowUniformBuffer: objectMeshShadowUniformBuffer,
        causticsTexture: causticsTexture,
        causticsView: causticsView,
        objectReflectionTexture: objectReflectionTexture,
        objectReflectionView: objectReflectionTexture.createView(),
        objectClippedReflectionTexture: objectClippedReflectionTexture,
        objectClippedReflectionView: objectClippedReflectionTexture.createView(),
        objectRefractionTexture: objectRefractionTexture,
        objectRefractionView: objectRefractionTexture.createView(),
        objectTextureDepthTexture: objectTextureDepthTexture,
        objectTextureDepthView: objectTextureDepthTexture.createView(),
        objectShadowTexture: objectShadowTexture,
        objectShadowView: objectShadowTexture.createView(),
        activeIndex: 0,
        frameIndex: 0,
        waterClock: {},
        waterNormalDispatchSeq: 0,
        waterExpensiveCadenceBucket: null,
        waterObjectTextureFrameSeq: 0,
        seeded: false,
        seedSalt: Number.isFinite(Number(entry && entry.seedSalt)) ? Number(entry.seedSalt) : Math.random() * 4096,
        lastDropEventID: 0,
        dropDispatchCount: 0,
        // M5 at-rest gating -- see WATER_REST_ENERGY_EPSILON's comment above.
        // A freshly created system starts AWAKE (energy 1.0, no quiet time
        // banked yet): seedDrops (default 20) fires on the first tick below,
        // so it should decay/settle like any other disturbance rather than
        // being born already "at rest".
        waterRestEnergy: 1.0,
        waterLastDisturbanceMS: 0,
        waterAtRest: false,
        dispose: function() {
          if (system._gosxDisposed) return;
          system._gosxDisposed = true;
          if (bufferA && typeof bufferA.destroy === "function") {
            pointsEntryGPUBuffers.delete(bufferA);
            bufferA.destroy();
          }
          if (bufferB && typeof bufferB.destroy === "function") {
            pointsEntryGPUBuffers.delete(bufferB);
            bufferB.destroy();
          }
          if (stateTextureA && typeof stateTextureA.destroy === "function") stateTextureA.destroy();
          if (stateTextureB && typeof stateTextureB.destroy === "function") stateTextureB.destroy();
          if (uniformBuffer && typeof uniformBuffer.destroy === "function") {
            pointsEntryGPUBuffers.delete(uniformBuffer);
            uniformBuffer.destroy();
          }
          if (objectSphereBuffer && typeof objectSphereBuffer.destroy === "function") {
            pointsEntryGPUBuffers.delete(objectSphereBuffer);
            objectSphereBuffer.destroy();
          }
          if (objectTextureMatrixBuffer && typeof objectTextureMatrixBuffer.destroy === "function") {
            pointsEntryGPUBuffers.delete(objectTextureMatrixBuffer);
            objectTextureMatrixBuffer.destroy();
          }
          if (objectMeshShadowUniformBuffer && typeof objectMeshShadowUniformBuffer.destroy === "function") {
            pointsEntryGPUBuffers.delete(objectMeshShadowUniformBuffer);
            objectMeshShadowUniformBuffer.destroy();
          }
          if (system.causticsTexture && typeof system.causticsTexture.destroy === "function") {
            system.causticsTexture.destroy();
          }
          if (system.objectReflectionTexture && typeof system.objectReflectionTexture.destroy === "function") {
            system.objectReflectionTexture.destroy();
          }
          if (system.objectClippedReflectionTexture && typeof system.objectClippedReflectionTexture.destroy === "function") {
            system.objectClippedReflectionTexture.destroy();
          }
          if (system.objectRefractionTexture && typeof system.objectRefractionTexture.destroy === "function") {
            system.objectRefractionTexture.destroy();
          }
          if (system.objectTextureDepthTexture && typeof system.objectTextureDepthTexture.destroy === "function") {
            system.objectTextureDepthTexture.destroy();
          }
          if (system.objectShadowTexture && typeof system.objectShadowTexture.destroy === "function") {
            system.objectShadowTexture.destroy();
          }
        },
      };
      system.computeBindGroups = [
        createWaterComputeBindGroup(system, bufferA, bufferB),
        createWaterComputeBindGroup(system, bufferB, bufferA),
      ];
      system.renderBindGroups = [
        createWaterRenderBindGroup(system, bufferA),
        createWaterRenderBindGroup(system, bufferB),
      ];
      system.causticsBindGroups = [
        createWaterCausticsBindGroup(system, bufferA),
        createWaterCausticsBindGroup(system, bufferB),
      ];
      system.objectTextureBindGroup = createWaterObjectTextureBindGroup(system);
      system.objectMeshShadowBindGroup = createWaterObjectMeshShadowBindGroup(system);
      system._qualityResourceKey = [causticsResolution, objectShadowResolution, objectTextureWidth, objectTextureHeight, objectTextureSize.pixelBudget].join("|");
      return system;
    }

    function retireWaterRenderTextures(textures) {
      var list = (textures || []).filter(Boolean);
      if (!list.length) return;
      function destroyAll() {
        for (var i = 0; i < list.length; i++) {
          if (list[i] && typeof list[i].destroy === "function") list[i].destroy();
        }
      }
      if (device && device.queue && typeof device.queue.onSubmittedWorkDone === "function") {
        device.queue.onSubmittedWorkDone().then(destroyAll).catch(destroyAll);
      } else {
        deferredWaterTextureRetirements.push({
          textures: list,
          retireAfterFrame: gpuTimingFrameSeq + 3,
        });
      }
    }

    function applySceneWaterQualityProfile(system, profile, revision, width, height) {
      if (!system) return;
      var source = profile && typeof profile === "object" ? profile : {};
      var tier = source.tier === "survival" || source.tier === "balanced" ? source.tier : "full";
      var authoredSurfaceResolution = Math.max(2, Math.floor(sceneNumber(system.authoredSurfaceResolution, system.resolution)));
      var surfaceResolution = Math.max(2, Math.min(authoredSurfaceResolution, Math.floor(sceneNumber(source.surfaceResolution, tier === "survival" ? 96 : tier === "balanced" ? 128 : authoredSurfaceResolution))));
      var authoredCausticsResolution = sceneWaterCausticsResolution(system.entry);
      var authoredObjectShadowResolution = sceneWaterObjectShadowResolution(system.entry);
      var causticsResolution = Math.max(64, Math.min(authoredCausticsResolution, Math.floor(sceneNumber(source.causticsResolution, authoredCausticsResolution))));
      var objectShadowResolution = Math.max(64, Math.min(authoredObjectShadowResolution, Math.floor(sceneNumber(source.objectShadowResolution, authoredObjectShadowResolution))));
      var baseObjectSize = sceneWaterObjectTextureTargetSize(system.entry, width, height);
      var objectMaxSide = Math.max(64, Math.floor(sceneNumber(source.objectTextureMaxSide, Math.max(baseObjectSize.width, baseObjectSize.height))));
      var objectScale = Math.min(1, objectMaxSide / Math.max(1, baseObjectSize.width, baseObjectSize.height));
      var objectTextureSize = sceneWaterObjectTextureClampToPixelBudget({
        mode: baseObjectSize.mode,
        width: Math.max(1, Math.floor(baseObjectSize.width * objectScale)),
        height: Math.max(1, Math.floor(baseObjectSize.height * objectScale)),
      }, Math.max(0, Math.floor(sceneNumber(source.objectTexturePixelBudget, baseObjectSize.pixelBudget))));
      var profileRevision = Math.max(0, Math.floor(sceneNumber(revision, 0)));
      var key = [causticsResolution, objectShadowResolution, objectTextureSize.width, objectTextureSize.height, objectTextureSize.pixelBudget].join("|");
      system.qualityTier = tier;
      system.qualityRevision = profileRevision;
      system.qualityDPRCap = Math.max(0.25, sceneNumber(source.dprCap, 1));
      system.surfaceResolution = surfaceResolution;
      system.vertexCount = Math.max(0, (surfaceResolution - 1) * (surfaceResolution - 1) * 6);
      system.expensivePassCadence = Math.max(1, Math.floor(sceneNumber(source.expensivePassCadence, tier === "survival" ? 3 : tier === "balanced" ? 2 : 1)));
      if (system._qualityResourceKey === key) {
        system.qualityAllocationPending = false;
        system.qualityAllocationConsecutiveFailures = 0;
        system.qualityAllocationNextFrame = 0;
        system.qualityAllocationDesiredKey = key;
        return;
      }
      if (system.qualityAllocationDesiredKey !== key) {
        system.qualityAllocationDesiredKey = key;
        system.qualityAllocationConsecutiveFailures = 0;
        system.qualityAllocationNextFrame = 0;
      }
      if (system.qualityAllocationPending && webGPUFrameSeq < system.qualityAllocationNextFrame) return;

      var oldState = {
        causticsTexture: system.causticsTexture, causticsView: system.causticsView,
        objectReflectionTexture: system.objectReflectionTexture, objectReflectionView: system.objectReflectionView,
        objectClippedReflectionTexture: system.objectClippedReflectionTexture, objectClippedReflectionView: system.objectClippedReflectionView,
        objectRefractionTexture: system.objectRefractionTexture, objectRefractionView: system.objectRefractionView,
        objectTextureDepthTexture: system.objectTextureDepthTexture, objectTextureDepthView: system.objectTextureDepthView,
        objectShadowTexture: system.objectShadowTexture, objectShadowView: system.objectShadowView,
        causticsResolution: system.causticsResolution, objectShadowResolution: system.objectShadowResolution,
        objectTextureWidth: system.objectTextureWidth, objectTextureHeight: system.objectTextureHeight,
        objectTextureResolution: system.objectTextureResolution, objectTexturePixelBudget: system.objectTexturePixelBudget,
        renderBindGroups: system.renderBindGroups, causticsBindGroups: system.causticsBindGroups,
      };
      var candidates = [];
      function createCandidateTexture(desc) {
        var texture = device.createTexture(desc);
        candidates.push(texture);
        return texture;
      }
      function objectColorTarget(label) {
        return createCandidateTexture({ label: label, size: [objectTextureSize.width, objectTextureSize.height, 1], format: WATER_OBJECT_TEXTURE_FORMAT, usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING });
      }
      try {
        system.causticsTexture = createCandidateTexture({ label: "gosx-water-caustics-target", size: [causticsResolution, causticsResolution, 1], format: WATER_CAUSTICS_TEXTURE_FORMAT, usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST });
        system.causticsView = system.causticsTexture.createView();
        system.objectReflectionTexture = objectColorTarget("gosx-water-object-reflection-target");
        system.objectReflectionView = system.objectReflectionTexture.createView();
        system.objectClippedReflectionTexture = objectColorTarget("gosx-water-object-clipped-reflection-target");
        system.objectClippedReflectionView = system.objectClippedReflectionTexture.createView();
        system.objectRefractionTexture = objectColorTarget("gosx-water-object-refraction-target");
        system.objectRefractionView = system.objectRefractionTexture.createView();
        system.objectTextureDepthTexture = createCandidateTexture({ label: "gosx-water-object-texture-depth", size: [objectTextureSize.width, objectTextureSize.height, 1], format: "depth24plus", usage: GPUTextureUsage.RENDER_ATTACHMENT });
        system.objectTextureDepthView = system.objectTextureDepthTexture.createView();
        system.objectShadowTexture = createCandidateTexture({ label: "gosx-water-object-shadow-target", size: [objectShadowResolution, objectShadowResolution, 1], format: WATER_OBJECT_TEXTURE_FORMAT, usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING });
        system.objectShadowView = system.objectShadowTexture.createView();
        system.causticsResolution = causticsResolution;
        system.objectShadowResolution = objectShadowResolution;
        system.objectTextureWidth = objectTextureSize.width;
        system.objectTextureHeight = objectTextureSize.height;
        system.objectTextureResolution = objectTextureSize.resolution;
        system.objectTexturePixelBudget = objectTextureSize.pixelBudget;
        system.renderBindGroups = [createWaterRenderBindGroup(system, system.bufferA), createWaterRenderBindGroup(system, system.bufferB)];
        system.causticsBindGroups = [createWaterCausticsBindGroup(system, system.bufferA), createWaterCausticsBindGroup(system, system.bufferB)];
      } catch (qualityResourceError) {
        Object.keys(oldState).forEach(function(name) { system[name] = oldState[name]; });
        candidates.forEach(function(texture) { if (texture && typeof texture.destroy === "function") texture.destroy(); });
        system.qualityAllocationFailures += 1;
        system.qualityAllocationConsecutiveFailures += 1;
        system.qualityAllocationPending = true;
        system.qualityAllocationNextFrame = webGPUFrameSeq + Math.min(60,
          Math.pow(2, Math.min(6, system.qualityAllocationConsecutiveFailures - 1)));
        return;
      }
      system._cubemapRenderBindGroups = null;
      system._poolBindGroups = null;
      system.waterExpensiveCadenceBucket = null;
      system._qualityResourceKey = key;
      system.qualityAllocationPending = false;
      system.qualityAllocationConsecutiveFailures = 0;
      system.qualityAllocationNextFrame = 0;
      system.qualityAllocationDesiredKey = key;
      retireWaterRenderTextures([
        oldState.causticsTexture, oldState.objectReflectionTexture, oldState.objectClippedReflectionTexture,
        oldState.objectRefractionTexture, oldState.objectTextureDepthTexture, oldState.objectShadowTexture,
      ]);
    }

    function retireWaterSystem(system) {
      if (!system || typeof system.dispose !== "function" || system._gosxDisposed) return;
      system._gosxRetireSerial = ++waterSystemRetireSerial;
      if (device && device.queue && typeof device.queue.onSubmittedWorkDone === "function") {
        device.queue.onSubmittedWorkDone().then(function() {
          system.dispose();
        }).catch(function() {
          system.dispose();
        });
        return;
      }
      deferredWaterSystemRetirements.push({ system: system, retireAfterFrame: gpuTimingFrameSeq + 3 });
    }

    function disposeWaterSystems() {
      for (const record of waterSystems.values()) {
        if (record && record.system && typeof record.system.dispose === "function") {
          try { record.system.dispose(); } catch (_err) {}
        }
      }
      waterSystems.clear();
      lastWaterTimeSeconds = null;
    }

    function syncWaterSystems(entries, width, height) {
      var activeIds = new Set();
      var records = [];
      var sourceEntries = Array.isArray(entries) ? entries : [];
      for (var i = 0; i < sourceEntries.length; i++) {
        var entry = sourceEntries[i];
        if (!entry || typeof entry !== "object") continue;
        var id = typeof entry.id === "string" && entry.id ? entry.id : ("scene-water-" + i);
        var record = waterSystems.get(id);
        var signature = sceneWaterSystemSignature(entry, width, height);
        activeIds.add(id);
        if (!record || record.signature !== signature) {
          if (record && record.system && typeof record.system.dispose === "function") {
            retireWaterSystem(record.system);
          }
          record = {
            signature: signature,
            system: createSceneWaterSystem(device, entry, width, height),
          };
          if (record.system) record.system.id = id;
          waterSystems.set(id, record);
        } else if (record.system) {
          record.system.entry = entry;
          record.system.id = id;
        }
        if (record && record.system) {
          records.push(record);
        }
      }
      for (const [id, record] of waterSystems.entries()) {
        if (!activeIds.has(id)) {
          if (record && record.system && typeof record.system.dispose === "function") {
            retireWaterSystem(record.system);
          }
          waterSystems.delete(id);
        }
      }
      return records;
    }

    // sharedPass (optional): when provided, the dispatch is recorded into an
    // ALREADY-OPEN GPUComputePassEncoder instead of opening/closing its own --
    // used to fuse the per-tick simulation substeps and the trailing normal
    // reconstruction into one compute pass (see the water-sim/normal fusion
    // block below). WebGPU auto-synchronizes storage-buffer read-after-write
    // hazards between successive dispatchWorkgroups() calls in program order,
    // whether they're in the same pass or different passes, so batching
    // dispatches into a shared pass changes nothing about visibility -- only
    // the number of beginComputePass/end() calls encoded.
    function dispatchWaterPass(encoder, system, pipeline, sharedPass) {
      if (!system || !pipeline) return 0;
      var pass = sharedPass || (encoder && encoder.beginComputePass({ label: "gosx-water-pass" }));
      if (!pass) return 0;
      pass.setPipeline(pipeline);
      pass.setBindGroup(0, system.computeBindGroups[system.activeIndex]);
      pass.dispatchWorkgroups(Math.ceil(system.cellCount / 64));
      if (!sharedPass) pass.end();
      system.activeIndex = system.activeIndex === 0 ? 1 : 0;
      return 1;
    }

    function renderWaterCausticsPass(encoder, system) {
      if (!encoder || !system || !system.causticsView) {
        return { passes: 0, authored: false, failed: false, sourceBytes: 0, selena: 0, selenaFallback: 0 };
      }
      var entry = system.entry || {};
      // Caustics pass routed through the generic descriptor-driven Selena
      // WebGPU render path. See sceneWaterCausticsUsesSelena/
      // getWaterCausticsSelenaDraw above. Falls through to the hand-written
      // waterCausticsPipeline path below when Selena isn't usable (e.g. WGSL
      // validation rejection, memoized as a failure by getSelenaPipeline).
      var selenaFallback = 0;
      if (sceneWaterCausticsUsesSelena(entry)) {
        var selenaDraw = getWaterCausticsSelenaDraw(system, entry);
        if (selenaDraw) {
          var selenaPass = encoder.beginRenderPass({
            label: "gosx-water-caustics-pass",
            colorAttachments: [{
              view: system.causticsView,
              loadOp: "clear",
              storeOp: "store",
              clearValue: { r: 0, g: 0, b: 0, a: 1 },
            }],
          });
          selenaPass.setPipeline(selenaDraw.pipeline);
          selenaPass.setBindGroup(0, selenaDraw.bindGroup);
          // Selena caustics projects the same authored water topology as the
          // visible surface, matching the reference's tessellated light grid.
          selenaPass.draw(system.vertexCount);
          selenaPass.end();
          return { passes: 1, authored: false, failed: false, sourceBytes: 0, selena: 1, selenaFallback: 0 };
        }
        selenaFallback = 1;
      }
      if (!waterCausticsPipeline) {
        return { passes: 0, authored: false, failed: false, sourceBytes: 0, selena: 0, selenaFallback: selenaFallback };
      }
      // Falls through here only when Selena isn't usable: builtin
      // waterCausticsPipeline (SCENE_WATER_CAUSTICS_FRAGMENT_SOURCE) is the
      // last-resort safety-net fallback now that the hand-written
      // data-prop-authored caustics pipeline tier has been retired.
      var pipeline = waterCausticsPipeline;
      var pass = encoder.beginRenderPass({
        label: "gosx-water-caustics-pass",
        colorAttachments: [{
          view: system.causticsView,
          loadOp: "clear",
          storeOp: "store",
          clearValue: { r: 0, g: 0, b: 0, a: 1 },
        }],
      });
      pass.setPipeline(pipeline);
      pass.setBindGroup(0, system.causticsBindGroups[system.activeIndex]);
      pass.draw(3);
      pass.end();
      return { passes: 1, authored: false, failed: false, sourceBytes: 0, selena: 0, selenaFallback: selenaFallback };
    }

    function sceneWaterActiveObjectID(entry) {
      var raw = "";
      if (entry && typeof entry.objectID === "string" && entry.objectID) raw = entry.objectID;
      else if (entry && typeof entry.objectId === "string" && entry.objectId) raw = entry.objectId;
      if (raw) return raw;
      var active = String(entry && entry.activeObject || entry && entry.objectKind || "").trim().toLowerCase();
      if (active.indexOf("sphere") >= 0) return "float-sphere";
      if (active.indexOf("cube") >= 0 || active.indexOf("box") >= 0) return "float-cube";
      if (active.indexOf("torus") >= 0) return "float-torus";
      if (active.indexOf("duck") >= 0 || active.indexOf("mesh") >= 0) return "float-duck";
      return "";
    }

    function sceneWaterMeshObjectID(obj) {
      if (!obj || typeof obj !== "object") return "";
      return String(
        obj.id ||
        obj.nodeId ||
        obj.sourceId ||
        obj.ownerId ||
        obj.modelId ||
        obj.name ||
        ""
      );
    }

    function sceneWaterObjectMeshMatches(obj, targetID) {
      if (!targetID) return false;
      var id = sceneWaterMeshObjectID(obj);
      if (!id) return false;
      if (id === targetID) return true;
      return id.indexOf(targetID + ":") === 0 || id.indexOf(targetID + "/") === 0 || id.indexOf(targetID + "#") === 0;
    }

    function sceneWaterObjectMeshKindMatches(obj, entry) {
      if (!obj || !obj.castShadow) return false;
      var kind = String(obj.kind || "").trim().toLowerCase();
      var active = String(entry && entry.activeObject || entry && entry.objectKind || "").trim().toLowerCase();
      var waterKind = sceneWaterObjectKind(entry);
      if (waterKind === 1) return kind.indexOf("sphere") >= 0;
      if (waterKind === 2) return kind.indexOf("box") >= 0 || kind.indexOf("cube") >= 0;
      if (active.indexOf("torus") >= 0) return kind.indexOf("torus") >= 0;
      if (active.indexOf("duck") >= 0 || active.indexOf("mesh") >= 0) {
        var id = sceneWaterMeshObjectID(obj).toLowerCase();
        return id.indexOf("duck") >= 0 || kind.indexOf("model") >= 0 || kind.indexOf("mesh") >= 0;
      }
      return false;
    }

    function sceneWaterObjectMeshCandidateProfile(bundle, entry, materials) {
      var targetID = sceneWaterActiveObjectID(entry);
      var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
      var materialList = Array.isArray(materials) ? materials : (Array.isArray(bundle && bundle.materials) ? bundle.materials : []);
      var parts = [];
      for (var i = 0; i < objects.length && parts.length < 8; i++) {
        var obj = objects[i] || {};
        var materialIndex = Math.max(0, Math.floor(sceneNumber(obj.materialIndex, 0)));
        var material = materialList[materialIndex] || {};
        var materialName = String(material.name || material.id || obj.material || material.kind || material.materialKind || "?");
        var materialBackend = sceneSelenaIsMaterial(material) ? "selena" : String(material.shaderBackend || material.kind || material.materialKind || "pbr");
        parts.push([
          sceneWaterMeshObjectID(obj) || "?",
          String(obj.kind || "?"),
          obj.castShadow ? "shadow" : "no-shadow",
          obj.viewCulled ? "culled" : "visible",
          "mat=" + materialName,
          "backend=" + materialBackend,
          String(Math.max(0, Math.floor(sceneNumber(obj.vertexCount, 0)))),
        ].join(":"));
      }
      return (targetID || "?") + "|" + parts.join(",");
    }

    function sceneWaterObjectMeshList(bundle, entry) {
      var targetID = sceneWaterActiveObjectID(entry);
      var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
      var selected = [];
      if (targetID) {
        for (var i = 0; i < objects.length; i++) {
          var obj = objects[i];
          if (!obj) continue;
          if (!Number.isFinite(obj.vertexOffset) || !Number.isFinite(obj.vertexCount) || obj.vertexCount <= 0) continue;
          if (sceneWaterObjectMeshMatches(obj, targetID)) selected.push(obj);
        }
      }
      if (selected.length > 0) return selected;
      for (var j = 0; j < objects.length; j++) {
        var fallback = objects[j];
        if (!fallback) continue;
        if (!Number.isFinite(fallback.vertexOffset) || !Number.isFinite(fallback.vertexCount) || fallback.vertexCount <= 0) continue;
        if (sceneWaterObjectMeshKindMatches(fallback, entry)) selected.push(fallback);
      }
      return selected;
    }

    // includeCamera (M4 audit): the two callers below deliberately disagree.
    // The object-shadow pass (renderWaterObjectShadowPass /
    // renderWaterObjectMeshShadowPass) calls this with includeCamera=false --
    // the shadow it caches is a LIGHT-space projection (light direction +
    // object transform + pool extents only, see
    // sceneWaterObjectMeshShadowUniformData below: light/poolWidth/poolLength,
    // no eye/view/projection field at all), so it is camera-independent by
    // construction and re-rendering a 1024x1024-class RTT every frame while
    // only the camera orbits would be pure waste. The object-texture
    // (reflection/refraction) pass at its OTHER call site legitimately passes
    // includeCamera=true: those RTTs render the mesh's reflection FROM the
    // camera's own eye position (getWaterObjectTextureRenderContext feeds
    // cameraPos/viewProjection), so they must re-render whenever the camera
    // moves. Do not "simplify" this to one shared value -- see
    // TestWaterObjectShadowSignatureExcludesCamera (program_test.go) for the
    // regression lock.
    function sceneWaterObjectRenderSignature(system, entry, bundle, objectList, includeCamera) {
      var center = (system && system.waterObjectCenter) || {};
      var half = (system && system.waterObjectHalfSize) || {};
      var light = (system && system.waterLightDir) || {};
      var parts = [
        sceneWaterActiveObjectID(entry), sceneWaterObjectKind(entry),
        sceneNumber(center.x, 0), sceneNumber(center.y, 0), sceneNumber(center.z, 0),
        sceneNumber(half.x, 0), sceneNumber(half.y, 0), sceneNumber(half.z, 0),
        sceneNumber(system && system.waterObjectRadius, 0),
        sceneNumber(light.x, 0), sceneNumber(light.y, 0), sceneNumber(light.z, 0),
      ];
      if (includeCamera) {
        var camera = (bundle && bundle.camera) || {};
        parts.push(camera.mode || "", sceneNumber(camera.x, 0), sceneNumber(camera.y, 0), sceneNumber(camera.z, 0),
          sceneNumber(camera.targetX, 0), sceneNumber(camera.targetY, 0), sceneNumber(camera.targetZ, 0),
          sceneNumber(camera.fov, 0), sceneNumber(camera.near, 0), sceneNumber(camera.far, 0));
      }
      for (var i = 0; i < objectList.length; i++) {
        var obj = objectList[i] || {};
        parts.push(sceneWaterMeshObjectID(obj), sceneNumber(obj.vertexOffset, 0), sceneNumber(obj.vertexCount, 0));
      }
      return parts.join("|");
    }

    function bindWaterObjectMeshVertexBuffers(pass, obj, pbrBuffers) {
      if (!pass || !obj || !pbrBuffers) return false;
      var offset = obj.vertexOffset;
      var count = obj.vertexCount;
      var isSkinned = webGPUObjectIsSkinned(obj);
      var computedMorphRecord = !isSkinned ? webGPUObjectComputedMorphDrawRecord(obj) : null;
      if (isSkinned) {
        return webGPUBindElioSkinnedBuffers(pass, obj, count);
      }
      if (computedMorphRecord) {
        if (!webGPUBindComputedMorphBuffer(pass, 0, computedMorphRecord.positionBuffer, count, 3)) return false;
        if (!webGPUBindComputedMorphBuffer(pass, 1, computedMorphRecord.normalBuffer, count, 3)) return false;
        if (!webGPUBindSceneMeshVertexBuffer(pass, 2, pbrBuffers && pbrBuffers.uvs, offset, count)) return false;
        if (!webGPUBindComputedMorphBuffer(pass, 3, computedMorphRecord.tangentBuffer, count, 4)) return false;
        return true;
      }
      if (!webGPUBindSceneMeshVertexBuffer(pass, 0, pbrBuffers && pbrBuffers.positions, offset, count)) return false;
      if (!webGPUBindSceneMeshVertexBuffer(pass, 1, pbrBuffers && pbrBuffers.normals, offset, count)) return false;
      if (!webGPUBindSceneMeshVertexBuffer(pass, 2, pbrBuffers && pbrBuffers.uvs, offset, count)) return false;
      if (!webGPUBindSceneMeshVertexBuffer(pass, 3, pbrBuffers && pbrBuffers.tangents, offset, count)) return false;
      return true;
    }

    function bindWaterObjectSelenaAttribute(pass, attr, obj, pbrBuffers) {
      if (!pass || !attr || !obj || !pbrBuffers) return false;
      var count = obj.vertexCount;
      var offset = obj.vertexOffset;
      var computedRecord = webGPUObjectComputedMorphDrawRecord(obj);
      if (attr.source === "positions") {
        if (computedRecord && webGPUBindComputedMorphBuffer(pass, attr.slot, computedRecord.positionBuffer, count, 3)) return true;
        return webGPUBindSceneMeshVertexBuffer(pass, attr.slot, pbrBuffers && pbrBuffers.positions, offset, count);
      }
      if (attr.source === "normals") {
        if (computedRecord && webGPUBindComputedMorphBuffer(pass, attr.slot, computedRecord.normalBuffer, count, 3)) return true;
        return webGPUBindSceneMeshVertexBuffer(pass, attr.slot, pbrBuffers && pbrBuffers.normals, offset, count);
      }
      if (attr.source === "uvs") {
        return webGPUBindSceneMeshVertexBuffer(pass, attr.slot, pbrBuffers && pbrBuffers.uvs, offset, count);
      }
      if (attr.source === "tangents") {
        if (computedRecord && webGPUBindComputedMorphBuffer(pass, attr.slot, computedRecord.tangentBuffer, count, 4)) return true;
        return webGPUBindSceneMeshVertexBuffer(pass, attr.slot, pbrBuffers && pbrBuffers.tangents, offset, count);
      }
      return false;
    }

    function bindWaterObjectSelenaAttributes(pass, resource, obj, pbrBuffers) {
      var attrs = resource && Array.isArray(resource.attrs) ? resource.attrs : [];
      for (var ai = 0; ai < attrs.length; ai++) {
        if (!bindWaterObjectSelenaAttribute(pass, attrs[ai], obj, pbrBuffers)) return false;
      }
      return attrs.length > 0;
    }

    function sceneWaterObjectTextureSelenaUniforms(system, texturePassMode) {
      var mode = texturePassMode === 2 ? 2 : 1;
      var entry = system && system.entry || {};
      var resolution = Math.max(1, sceneNumber(system && system.waterResolution, system && system.resolution ? system.resolution : sceneWaterResolution(entry && entry.resolution)));
      var poolWidth = Math.max(0.01, sceneNumber(system && system.waterPoolWidth, sceneNumber(entry && entry.poolWidth, 1.0)));
      var poolHeight = Math.max(0.01, sceneNumber(system && system.waterPoolHeight, sceneNumber(entry && entry.poolHeight, 1.0)));
      var poolLength = Math.max(0.01, sceneNumber(system && system.waterPoolLength, sceneNumber(entry && entry.poolLength, 1.0)));
      var rounded = sceneWaterPoolShapeRounded(entry);
      var maxCornerRadius = Math.max(0, Math.min(poolWidth, poolLength) - 0.001);
      var cornerRadius = Math.max(0, sceneNumber(system && system.waterCornerRadius, rounded ? Math.max(0, Math.min(maxCornerRadius, sceneNumber(entry && entry.cornerRadius, 0))) : 0));
      var light = system && system.waterLightDir ? system.waterLightDir : sceneWaterLightVector(entry, { x: 0.3, y: 0.9, z: 0.45 });
      var lightLen = Math.sqrt(light.x * light.x + light.y * light.y + light.z * light.z) || 1;
      var kind = Math.max(0, Math.floor(sceneNumber(system && system.waterObjectKind, sceneWaterObjectKind(entry))));
      var subtype = Math.max(0, Math.floor(sceneNumber(system && system.waterObjectSubtype, sceneWaterObjectSubtype(entry, kind))));
      var radius = Math.max(0.0001, sceneNumber(system && system.waterObjectRadius, sceneNumber(entry && entry.objectRadius, kind === 1 ? 0.25 : 0.31)));
      return {
        isTexturePass: [1, 0, 0, 0],
        texturePassMode: [mode, 0, 0, 0],
        waterObjectTexturePassMode: [mode, 0, 0, 0],
        lightDir: [light.x / lightLen, light.y / lightLen, light.z / lightLen, 0],
        poolSize: [poolWidth, poolHeight, poolLength, cornerRadius],
        params: [resolution, radius, kind, subtype],
      };
    }

    function sceneWaterObjectTextureSelenaContext(system, texturePassMode, targetName) {
      var mode = texturePassMode === 2 ? 2 : 1;
      var target = String(targetName || "target").trim() || "target";
      var waterID = String(system && (system.id || system.entry && system.entry.id) || "water-system");
      return {
        kind: "water-object-texture",
        uniformSlotSuffix: ["water-object-texture", waterID, target, mode].join("-"),
        uniforms: sceneWaterObjectTextureSelenaUniforms(system, mode),
      };
    }

    function drawWaterObjectMeshObjects(pass, objectList, bundle, materials, frameBindGroup, pbrBuffers, texturePassMode, renderContext) {
      if (!pass || !Array.isArray(objectList) || objectList.length === 0 || !pbrBuffers) return { drawCalls: 0, selenaDrawCalls: 0 };
      var drawCalls = 0;
      var selenaDrawCalls = 0;
      var currentPipelineKey = "";
      var lastMaterialIndex = -1;
      var lastReceiveShadow = null;

      for (var i = 0; i < objectList.length; i++) {
        var obj = objectList[i];
        var matIndex = sceneNumber(obj && obj.materialIndex, 0);
        var mat = materials[matIndex] || null;
        var renderPassKind = scenePBRObjectRenderPass(obj, mat);
        var blendMode = renderPassKind === "additive" ? "additive" : "alpha";
        var depthWrite = renderPassKind !== "alpha" && renderPassKind !== "additive";
        var selenaResource = getSelenaPipeline(mat, blendMode, depthWrite, {
          targetFormat: WATER_OBJECT_TEXTURE_FORMAT,
          sampleCount: 1,
          labelSuffix: "water-object-texture",
        });
        if (selenaResource) {
          var selenaKey = "selena:" + texturePassMode + ":" + (mat && mat.key || matIndex) + ":" + blendMode + ":" + (depthWrite ? "1" : "0");
          if (currentPipelineKey !== selenaKey) {
            pass.setPipeline(selenaResource.pipeline);
            currentPipelineKey = selenaKey;
            lastMaterialIndex = -1;
            lastReceiveShadow = null;
          }
          var selenaBG = createSelenaBindGroup(mat, selenaResource, obj, renderContext);
          if (selenaBG && bindWaterObjectSelenaAttributes(pass, selenaResource, obj, pbrBuffers)) {
            pass.setBindGroup(0, selenaBG);
            pass.draw(obj.vertexCount);
            drawCalls += 1;
            selenaDrawCalls += 1;
            continue;
          }
        }
        var pipelineKey = texturePassMode + ":" + blendMode + ":" + (depthWrite ? "1" : "0");
        if (currentPipelineKey !== pipelineKey) {
          var pipeline = getWaterObjectMeshPipeline(texturePassMode, blendMode, depthWrite);
          if (!pipeline) continue;
          pass.setPipeline(pipeline);
          pass.setBindGroup(0, frameBindGroup);
          currentPipelineKey = pipelineKey;
          lastMaterialIndex = -1;
          lastReceiveShadow = null;
        }

        var receiveShadow = false;
        if (matIndex !== lastMaterialIndex || receiveShadow !== lastReceiveShadow) {
          pass.setBindGroup(1, createMaterialBindGroup(mat, receiveShadow, mat || obj));
          lastMaterialIndex = matIndex;
          lastReceiveShadow = receiveShadow;
        }

        var count = obj.vertexCount;
        if (bindWaterObjectMeshVertexBuffers(pass, obj, pbrBuffers)) {
          pass.draw(count);
          drawCalls += 1;
        }
      }
      return { drawCalls: drawCalls, selenaDrawCalls: selenaDrawCalls };
    }

    function drawWaterObjectProjectedShadowObjects(pass, objectList, pbrBuffers) {
      if (!pass || !Array.isArray(objectList) || objectList.length === 0 || !pbrBuffers) return 0;
      var drawCalls = 0;
      for (var i = 0; i < objectList.length; i++) {
        var obj = objectList[i];
        if (!obj || obj.viewCulled) continue;
        var count = obj.vertexCount;
        if (!Number.isFinite(count) || count <= 0) continue;
        if (bindWaterObjectMeshVertexBuffers(pass, obj, pbrBuffers)) {
          pass.draw(count);
          drawCalls += 1;
        }
      }
      return drawCalls;
    }

    // drawWaterObjectProjectedShadowObjectsSelena mirrors
    // drawWaterObjectProjectedShadowObjects for the generic Selena
    // object-mesh-shadow path: the same object list/culling/vertex-count
    // checks, but binding vertex attributes via bindWaterObjectSelenaAttributes
    // (object-mesh-shadow.sel declares ONLY a "position" attribute, unlike the
    // hand-written pipeline's full 4-slot PBR vertex layout) against the ONE
    // {pipeline,bindGroup,attrs} resource resolved once per system per frame
    // by getWaterObjectMeshShadowSelenaDraw (see its comment: lightDir/
    // poolHalfW/poolHalfL don't vary per object, so every object shares one
    // bind group).
    function drawWaterObjectProjectedShadowObjectsSelena(pass, objectList, pbrBuffers, selenaDraw) {
      if (!pass || !Array.isArray(objectList) || objectList.length === 0 || !pbrBuffers || !selenaDraw) return 0;
      var drawCalls = 0;
      for (var i = 0; i < objectList.length; i++) {
        var obj = objectList[i];
        if (!obj || obj.viewCulled) continue;
        var count = obj.vertexCount;
        if (!Number.isFinite(count) || count <= 0) continue;
        if (bindWaterObjectSelenaAttributes(pass, selenaDraw, obj, pbrBuffers)) {
          pass.draw(count);
          drawCalls += 1;
        }
      }
      return drawCalls;
    }

    function renderWaterObjectMeshTargetPass(encoder, system, view, objectList, bundle, materials, frameBindGroup, pbrBuffers, texturePassMode, label, targetName) {
      if (!encoder || !system || !view || !system.objectTextureDepthView || !Array.isArray(objectList) || objectList.length === 0) return 0;
      var pass = encoder.beginRenderPass({
        label: label || "gosx-water-object-mesh-pass",
        colorAttachments: [{
          view: view,
          loadOp: "clear",
          storeOp: "store",
          clearValue: { r: 0, g: 0, b: 0, a: 0 },
        }],
        depthStencilAttachment: {
          view: system.objectTextureDepthView,
          depthLoadOp: "clear",
          depthClearValue: 1.0,
          depthStoreOp: "store",
        },
      });
      var renderContext = sceneWaterObjectTextureSelenaContext(system, texturePassMode, targetName || label);
      var drawResult = drawWaterObjectMeshObjects(pass, objectList, bundle, materials, frameBindGroup, pbrBuffers, texturePassMode, renderContext);
      var drawCalls = drawResult && drawResult.drawCalls || 0;
      pass.end();
      return {
        passes: drawCalls > 0 ? 1 : 0,
        drawCalls: drawCalls,
        selenaDrawCalls: drawResult && drawResult.selenaDrawCalls || 0,
      };
    }

    function waterSystemUsesProjectedObjectTextures(system) {
      if (!system || !system.waterObjectActive) return false;
      var entry = system.entry || {};
      var kind = Math.max(0, Math.floor(sceneNumber(system.waterObjectKind, sceneWaterObjectKind(entry))));
      return kind === 3;
    }

    function waterSystemHasObjectTextureSubject(system) {
      return waterSystemUsesProjectedObjectTextures(system);
    }

    function sceneWaterObjectTextureTargetSlots(system) {
      var subtype = Math.max(0, Math.floor(sceneNumber(system && system.waterObjectSubtype, 0)));
      // Match the upstream optical contract: torus uses refraction + reflected
      // camera, glTF/mesh (duck) uses refraction + clipped reflection. Unknown
      // compound subjects retain all three targets as the conservative path.
      if (subtype === 1) return [0, 1];
      if (subtype === 2) return [0, 2];
      return [0, 1, 2];
    }

    function renderWaterObjectTexturePass(encoder, system) {
      if (!encoder || !system || !waterObjectTexturePipeline || !system.objectTextureBindGroup) return 0;
      if (!system.objectReflectionView || !system.objectClippedReflectionView || !system.objectRefractionView) return 0;
      var hasSubject = waterSystemHasObjectTextureSubject(system);
      var pass = encoder.beginRenderPass({
        label: "gosx-water-object-texture-pass",
        colorAttachments: [
          {
            view: system.objectReflectionView,
            loadOp: "clear",
            storeOp: "store",
            clearValue: { r: 0, g: 0, b: 0, a: 0 },
          },
          {
            view: system.objectClippedReflectionView,
            loadOp: "clear",
            storeOp: "store",
            clearValue: { r: 0, g: 0, b: 0, a: 0 },
          },
          {
            view: system.objectRefractionView,
            loadOp: "clear",
            storeOp: "store",
            clearValue: { r: 0, g: 0, b: 0, a: 0 },
          },
        ],
      });
      if (hasSubject) {
        pass.setPipeline(waterObjectTexturePipeline);
        pass.setBindGroup(0, system.objectTextureBindGroup);
        pass.draw(3);
      }
      pass.end();
      return hasSubject ? 1 : 0;
    }

    function renderWaterObjectShadowPass(encoder, system) {
      if (!encoder || !system || !system.objectShadowView) {
        return { passes: 0, authored: false, failed: false, selena: 0, selenaFallback: 0 };
      }
      var entry = system.entry || {};
      var hasSubject = waterSystemHasObjectTextureSubject(system);
      var pass = encoder.beginRenderPass({
        label: "gosx-water-object-shadow-pass",
        colorAttachments: [{
          view: system.objectShadowView,
          loadOp: "clear",
          storeOp: "store",
          clearValue: { r: 0, g: 0, b: 0, a: 0 },
        }],
      });
      // Object-shadow/compound-shadow pass routed through the generic
      // descriptor-driven Selena WebGPU post-kind render path. See
      // getWaterObjectShadowSelenaDraw above (it selects WaterObjectShadow vs
      // WaterCompoundShadow by the system's active object kind, mirroring the
      // raw hand-written shader's own objectParams.x branch). Falls through to
      // the hand-written waterObjectShadowPipeline path below when Selena
      // isn't usable.
      var selena = 0;
      var selenaFallback = 0;
      var drewSelena = false;
      var pipelineRecord = null;
      if (hasSubject) {
        var selenaDraw = getWaterObjectShadowSelenaDraw(system, entry);
        if (selenaDraw) {
          pass.setPipeline(selenaDraw.pipeline);
          pass.setBindGroup(0, selenaDraw.bindGroup);
          pass.draw(3);
          selena = 1;
          drewSelena = true;
        } else {
          selenaFallback = 1;
        }
      }
      if (hasSubject && !drewSelena && waterObjectShadowPipeline && system.objectTextureBindGroup) {
        // Builtin waterObjectShadowPipeline (SCENE_WATER_OBJECT_SHADOW_FRAGMENT_SOURCE)
        // is the last-resort safety-net fallback now that the hand-written
        // data-prop-authored object-shadow pipeline tier has been retired.
        pass.setPipeline(waterObjectShadowPipeline);
        pass.setBindGroup(0, system.objectTextureBindGroup);
        pass.draw(3);
      }
      pass.end();
      return {
        passes: hasSubject ? 1 : 0,
        authored: !!(pipelineRecord && pipelineRecord.authored && hasSubject && !drewSelena),
        failed: !!(pipelineRecord && pipelineRecord.failed),
        selena: selena,
        selenaFallback: selenaFallback,
      };
    }

    function renderWaterObjectMeshShadowPass(encoder, system, objectList, pbrBuffers) {
      if (!encoder || !system || !system.objectShadowView) {
        return { passes: 0, drawCalls: 0, authored: false, failed: false, selena: 0, selenaFallback: 0 };
      }
      if (!waterSystemHasObjectTextureSubject(system) || !Array.isArray(objectList) || objectList.length === 0 || !pbrBuffers) {
        return { passes: 0, drawCalls: 0, authored: false, failed: false, selena: 0, selenaFallback: 0 };
      }
      var entry = system.entry || {};
      var pass = encoder.beginRenderPass({
        label: "gosx-water-object-mesh-shadow-pass",
        colorAttachments: [{
          view: system.objectShadowView,
          loadOp: "clear",
          storeOp: "store",
          clearValue: { r: 0, g: 0, b: 0, a: 0 },
        }],
      });
      // Object-mesh-shadow pass routed through the generic descriptor-driven
      // Selena WebGPU render path. See getWaterObjectMeshShadowSelenaDraw /
      // drawWaterObjectProjectedShadowObjectsSelena above. Falls through to
      // the hand-written waterObjectMeshShadowPipeline path below when Selena
      // isn't usable.
      var selena = 0;
      var selenaFallback = 0;
      var drawCalls = 0;
      var pipelineRecord = null;
      if (sceneWaterObjectMeshShadowUsesSelena(entry)) {
        var selenaDraw = getWaterObjectMeshShadowSelenaDraw(system, entry);
        if (selenaDraw) {
          pass.setPipeline(selenaDraw.pipeline);
          pass.setBindGroup(0, selenaDraw.bindGroup);
          drawCalls = drawWaterObjectProjectedShadowObjectsSelena(pass, objectList, pbrBuffers, selenaDraw);
          selena = 1;
        } else {
          selenaFallback = 1;
        }
      }
      if (!selena && waterObjectMeshShadowPipeline && system.objectMeshShadowBindGroup && system.objectMeshShadowUniformBuffer) {
        // Builtin waterObjectMeshShadowPipeline (SCENE_WATER_OBJECT_MESH_SHADOW_*_SOURCE)
        // is the last-resort safety-net fallback now that the hand-written
        // data-prop-authored object-mesh-shadow pipeline tier has been retired.
        device.queue.writeBuffer(system.objectMeshShadowUniformBuffer, 0, sceneWaterObjectMeshShadowUniformData(system));
        pass.setPipeline(waterObjectMeshShadowPipeline);
        pass.setBindGroup(0, system.objectMeshShadowBindGroup);
        drawCalls = drawWaterObjectProjectedShadowObjects(pass, objectList, pbrBuffers);
      }
      pass.end();
      return {
        passes: drawCalls > 0 ? 1 : 0,
        drawCalls: drawCalls,
        authored: !!(pipelineRecord && pipelineRecord.authored && drawCalls > 0),
        failed: !!(pipelineRecord && pipelineRecord.failed),
        selena: selena,
        selenaFallback: selenaFallback,
      };
    }

    function sceneWaterNormalizeReflectionDirection(point) {
      var x = sceneNumber(point && point.x, 0);
      var y = sceneNumber(point && point.y, 0);
      var z = sceneNumber(point && point.z, 0);
      var length = Math.sqrt(x * x + y * y + z * z);
      if (length <= 0.000001) return { x: 0, y: 0, z: 1 };
      return { x: x / length, y: y / length, z: z / length };
    }

    function sceneWaterReflectionCameraForward(cam) {
      var x = 0;
      var y = 0;
      var z = 1;

      var sinX = Math.sin(cam.rotationX);
      var cosX = Math.cos(cam.rotationX);
      var nextY = y * cosX - z * sinX;
      var nextZ = y * sinX + z * cosX;
      y = nextY;
      z = nextZ;

      var sinY = Math.sin(cam.rotationY);
      var cosY = Math.cos(cam.rotationY);
      var nextX = x * cosY + z * sinY;
      nextZ = -x * sinY + z * cosY;
      x = nextX;
      z = nextZ;

      var sinZ = Math.sin(cam.rotationZ);
      var cosZ = Math.cos(cam.rotationZ);
      nextX = x * cosZ - y * sinZ;
      nextY = x * sinZ + y * cosZ;

      return sceneWaterNormalizeReflectionDirection({ x: nextX, y: nextY, z: z });
    }

    function sceneWaterCameraWorldPosition(camera) {
      var cam = sceneRenderCamera(camera);
      return { x: cam.x, y: cam.y, z: cam.z };
    }

    function sceneWaterCameraWorldDirection(camera) {
      var cam = sceneRenderCamera(camera);
      var forward = sceneWaterReflectionCameraForward(cam);
      return sceneWaterNormalizeReflectionDirection({ x: -forward.x, y: -forward.y, z: -forward.z });
    }

    function sceneWaterMirrorWaterPoint(point) {
      return {
        x: sceneNumber(point && point.x, 0),
        y: -sceneNumber(point && point.y, 0),
        z: sceneNumber(point && point.z, 0),
      };
    }

    function sceneWaterReflectionCamera(camera) {
      var cam = sceneRenderCamera(camera);
      var forward = sceneWaterReflectionCameraForward(cam);
      var reflectedForward = sceneWaterNormalizeReflectionDirection({
        x: forward.x,
        y: -forward.y,
        z: forward.z,
      });
      var horizontal = Math.sqrt(
        reflectedForward.x * reflectedForward.x +
        reflectedForward.z * reflectedForward.z
      );
      return {
        kind: cam.kind,
        x: cam.x,
        y: -cam.y,
        z: cam.z,
        rotationX: -Math.atan2(reflectedForward.y, horizontal),
        rotationY: Math.atan2(reflectedForward.x, reflectedForward.z),
        rotationZ: -cam.rotationZ,
        fov: cam.fov,
        left: cam.left,
        right: cam.right,
        top: cam.top,
        bottom: cam.bottom,
        zoom: cam.zoom,
        near: cam.near,
        far: cam.far,
      };
    }

    function sceneWaterReflectionCameraUp(camera) {
      var up = sceneWaterNormalizeReflectionDirection({
        x: sceneNumber(camera && camera.upX, 0),
        y: sceneNumber(camera && camera.upY, 1),
        z: sceneNumber(camera && camera.upZ, 0),
      });
      return { x: up.x, y: -up.y, z: up.z };
    }

    function sceneWaterLookAtViewMatrix(eye, target, up, out) {
      var zx = sceneNumber(eye && eye.x, 0) - sceneNumber(target && target.x, 0);
      var zy = sceneNumber(eye && eye.y, 0) - sceneNumber(target && target.y, 0);
      var zz = sceneNumber(eye && eye.z, 0) - sceneNumber(target && target.z, 0);
      var length = Math.sqrt(zx * zx + zy * zy + zz * zz);
      if (length <= 0.000001) {
        zx = 0;
        zy = 0;
        zz = 1;
      } else {
        zx /= length;
        zy /= length;
        zz /= length;
      }

      var upv = sceneWaterNormalizeReflectionDirection(up);
      var xx = upv.y * zz - upv.z * zy;
      var xy = upv.z * zx - upv.x * zz;
      var xz = upv.x * zy - upv.y * zx;
      length = Math.sqrt(xx * xx + xy * xy + xz * xz);
      if (length <= 0.000001) {
        upv = Math.abs(zy) < 0.999 ? { x: 0, y: 1, z: 0 } : { x: 1, y: 0, z: 0 };
        xx = upv.y * zz - upv.z * zy;
        xy = upv.z * zx - upv.x * zz;
        xz = upv.x * zy - upv.y * zx;
        length = Math.sqrt(xx * xx + xy * xy + xz * xz);
      }
      if (length <= 0.000001) {
        xx = 1;
        xy = 0;
        xz = 0;
      } else {
        xx /= length;
        xy /= length;
        xz /= length;
      }

      var yx = zy * xz - zz * xy;
      var yy = zz * xx - zx * xz;
      var yz = zx * xy - zy * xx;

      out[0] = xx;
      out[1] = yx;
      out[2] = zx;
      out[3] = 0;
      out[4] = xy;
      out[5] = yy;
      out[6] = zy;
      out[7] = 0;
      out[8] = xz;
      out[9] = yz;
      out[10] = zz;
      out[11] = 0;
      out[12] = -(xx * eye.x + xy * eye.y + xz * eye.z);
      out[13] = -(yx * eye.x + yy * eye.y + yz * eye.z);
      out[14] = -(zx * eye.x + zy * eye.y + zz * eye.z);
      out[15] = 1;
      return out;
    }

    function addWaterObjectTextureStats(stats, system, passCount, targetCount, meshDrawCalls, fallbackPasses, selenaDrawCalls) {
      var targetWidth = Math.max(0, system && (system.objectTextureWidth || system.objectTextureResolution) || 0);
      var targetHeight = Math.max(0, system && (system.objectTextureHeight || system.objectTextureResolution) || 0);
      stats.waterObjectTexturePasses += Math.max(0, passCount || 0);
      stats.waterObjectTextureTargets += Math.max(0, targetCount || 0);
      stats.waterObjectTexturePixels += Math.max(0, targetCount || 0) * targetWidth * targetHeight;
      stats.waterObjectTextureWidth = Math.max(stats.waterObjectTextureWidth || 0, targetWidth);
      stats.waterObjectTextureHeight = Math.max(stats.waterObjectTextureHeight || 0, targetHeight);
      stats.waterObjectTexturePixelBudget = Math.max(stats.waterObjectTexturePixelBudget || 0, Math.max(0, system && system.objectTexturePixelBudget || 0));
      stats.waterObjectTextureMeshPasses += Math.max(0, passCount || 0) - Math.max(0, fallbackPasses || 0);
      stats.waterObjectTextureMeshDrawCalls += Math.max(0, meshDrawCalls || 0);
      stats.waterObjectTextureSelenaDrawCalls += Math.max(0, selenaDrawCalls || 0);
      stats.waterObjectTextureFallbackPasses += Math.max(0, fallbackPasses || 0);
    }

    function renderWaterObjectSceneTexturePasses(records, encoder, bundle, materials, frameBindGroup, pbrBuffers, width, height, toneMap) {
      var stats = {
        waterObjectTexturePasses: 0,
        waterObjectTextureTargets: 0,
        waterObjectTexturePixels: 0,
        waterObjectTextureWidth: 0,
        waterObjectTextureHeight: 0,
        waterObjectTexturePixelBudget: 0,
        waterObjectTextureMeshPasses: 0,
        waterObjectTextureMeshDrawCalls: 0,
        waterObjectTextureSelenaDrawCalls: 0,
        waterObjectTextureFallbackPasses: 0,
        waterObjectTextureCandidateObjects: 0,
        waterObjectTextureSelectedObjects: 0,
        waterObjectTextureFallbackMissingObjects: 0,
        waterObjectTextureFallbackMissingResources: 0,
        waterObjectTextureCandidateProfile: "",
      };
      if (!encoder || !Array.isArray(records) || records.length === 0) return stats;
      var restoredFrame = false;
      for (var i = 0; i < records.length; i++) {
        var system = records[i] && records[i].system;
        if (!system || !waterSystemHasObjectTextureSubject(system)) continue;
        var entry = system.entry || {};
        var optics = sceneWaterOpticsFlags(entry, {
          kind: sceneWaterObjectKind(entry),
          displacementScale: Math.max(0, sceneNumber(entry.objectDisplacementScale, 1)),
        });
        if (!optics.object && !optics.reflection && !optics.refraction) continue;
        var objectList = sceneWaterObjectMeshList(bundle, entry);
        stats.waterObjectTextureCandidateObjects += Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects.length : 0;
        stats.waterObjectTextureSelectedObjects += objectList.length;
        if (!stats.waterObjectTextureCandidateProfile) {
          stats.waterObjectTextureCandidateProfile = sceneWaterObjectMeshCandidateProfile(bundle, entry, materials);
        }
        if (!objectList.length || !pbrBuffers || !frameBindGroup) {
          if (!objectList.length) stats.waterObjectTextureFallbackMissingObjects += 1;
          if (!pbrBuffers || !frameBindGroup) stats.waterObjectTextureFallbackMissingResources += 1;
          var fallbackPasses = renderWaterObjectTexturePass(encoder, system);
          if (fallbackPasses > 0) addWaterObjectTextureStats(stats, system, fallbackPasses, fallbackPasses * 3, 0, fallbackPasses);
          continue;
        }

        var textureSignature = sceneWaterObjectRenderSignature(system, entry, bundle, objectList, true);
        var targetSlots = sceneWaterObjectTextureTargetSlots(system);
        if (system.waterObjectTextureSignature !== textureSignature) {
          system.waterObjectTextureSignature = textureSignature;
          system.waterObjectTextureRefreshRemaining = targetSlots.length;
        }
        var refreshRemaining = Math.max(0, Math.floor(sceneNumber(system.waterObjectTextureRefreshRemaining, 0)));
        if (refreshRemaining <= 0) continue;

        // The adaptive profile already cadences caustics/shadow work; apply
        // that same contract to projected mesh targets. A stressed GPU at
        // balanced/survival quality must not keep paying a viewport-sized duck
        // RTT on every display frame. This sequence is independent from the
        // changing render signature, otherwise motion would bypass cadence by
        // marking the target dirty every frame.
        var objectTextureFrameSeq = Math.max(0, Math.floor(sceneNumber(system.waterObjectTextureFrameSeq, 0)));
        system.waterObjectTextureFrameSeq = objectTextureFrameSeq + 1;
        var objectTextureCadence = Math.max(1, Math.floor(sceneNumber(system.expensivePassCadence, 1)));
        if (objectTextureFrameSeq % objectTextureCadence !== 0) continue;

        var targetWidth = Math.max(1, system.objectTextureWidth || system.objectTextureResolution || WATER_OBJECT_TEXTURE_SIZE);
        var targetHeight = Math.max(1, system.objectTextureHeight || system.objectTextureResolution || WATER_OBJECT_TEXTURE_SIZE);
        // Round-robin independently from refreshRemaining. Continuous object
        // or camera movement changes the signature every frame; deriving the
        // slot from refreshRemaining used to reset moving ducks to refraction
        // forever. The persistent cursor keeps all subtype-required targets
        // fresh while still rendering at most one RTT per frame.
        var refreshCursor = Math.max(0, Math.floor(sceneNumber(system.waterObjectTextureRefreshCursor, 0)));
        var passSlot = targetSlots[refreshCursor % targetSlots.length];
        var emptyPass = { passes: 0, drawCalls: 0, selenaDrawCalls: 0 };
        // A retained texture must stay paired with the projection matrix used
        // to render it. Update only the matrix for the target issued this
        // frame; skipped/alternating targets keep both their pixels and their
        // matching matrix instead of falling through to the current scratch
        // camera and visibly swimming across the water.
        var refraction = emptyPass;
        var reflection = emptyPass;
        var clipped = emptyPass;
        if (passSlot === 0) {
          uploadFrameUniforms(bundle && bundle.camera, targetWidth, targetHeight, false);
          if (system.objectViewProjectionMatrix) {
            system.objectViewProjectionMatrix.set(scratchSelenaViewProjection);
            system.objectViewProjectionReady = true;
          }
          refraction = renderWaterObjectMeshTargetPass(
            encoder,
            system,
            system.objectRefractionView,
            objectList,
            bundle,
            materials,
            frameBindGroup,
            pbrBuffers,
            1,
            "gosx-water-object-mesh-refraction-pass",
            "refraction"
          );
        } else {
          uploadWaterReflectionFrameUniforms(bundle && bundle.camera, targetWidth, targetHeight, false);
          if (system.objectReflectionViewProjectionMatrix) {
            system.objectReflectionViewProjectionMatrix.set(scratchSelenaViewProjection);
            system.objectReflectionViewProjectionReady = true;
          }
          if (passSlot === 1) {
            reflection = renderWaterObjectMeshTargetPass(
              encoder,
              system,
              system.objectReflectionView,
              objectList,
              bundle,
              materials,
              frameBindGroup,
              pbrBuffers,
              1,
              "gosx-water-object-mesh-reflection-pass",
              "reflection"
            );
          } else {
            clipped = renderWaterObjectMeshTargetPass(
              encoder,
              system,
              system.objectClippedReflectionView,
              objectList,
              bundle,
              materials,
              frameBindGroup,
              pbrBuffers,
              2,
              "gosx-water-object-mesh-clipped-reflection-pass",
              "clipped-reflection"
            );
          }
        }
        restoredFrame = true;
        var passCount = refraction.passes + reflection.passes + clipped.passes;
        var drawCalls = refraction.drawCalls + reflection.drawCalls + clipped.drawCalls;
        var selenaDrawCalls = refraction.selenaDrawCalls + reflection.selenaDrawCalls + clipped.selenaDrawCalls;
        if (passCount > 0) addWaterObjectTextureStats(stats, system, passCount, passCount, drawCalls, 0, selenaDrawCalls);
        system.waterObjectTextureRefreshCursor = (refreshCursor + 1) % targetSlots.length;
        system.waterObjectTextureRefreshRemaining = Math.max(0, refreshRemaining - 1);
      }
      if (restoredFrame) {
        uploadFrameUniforms(bundle && bundle.camera, width, height, toneMap);
        uploadLights(bundle && bundle.lights);
      }
      return stats;
    }

    function updateWaterSystems(entries, encoder, nowMS, lifecycleActive, qualityProfile, qualityRevision, bundle, pbrBuffers, width, height) {
      activeWaterShaderSourcesByID = null;
      var canvasWaterShaderSources = canvas && (canvas.__gosxScene3DWaterShaderSources || (canvas.parentNode && canvas.parentNode.__gosxScene3DWaterShaderSources));
      var bundleWaterShaderSources = bundle && bundle.waterShaderSourcesByID && typeof bundle.waterShaderSourcesByID === "object"
        ? bundle.waterShaderSourcesByID
        : canvasWaterShaderSources;
      if (bundleWaterShaderSources && typeof bundleWaterShaderSources === "object") {
        activeWaterShaderSourcesByID = new Map();
        Object.keys(bundleWaterShaderSources).forEach(function(id) {
          var record = bundleWaterShaderSources[id];
          if (record && typeof record === "object") activeWaterShaderSourcesByID.set(id, record);
        });
      }
      var currentNowMS = Number.isFinite(nowMS) ? nowMS : 0;
      var currentTime = currentNowMS / 1000;
      var waterLifecycleActive = lifecycleActive !== false;
      var records = syncWaterSystems(entries, width, height);
      var stats = {
        records: records,
        waterSystems: records.length,
        waterCells: 0,
        waterVertices: 0,
        waterComputeDispatches: 0,
        waterSimulationTicks: 0,
        waterSolverSubsteps: 0,
        waterDroppedTicks: 0,
        waterDroppedTicksThisFrame: 0,
        waterSimulationCatchUpCap: 1,
        waterSimulationTickSeq: 0,
        waterSolverSubstepSeq: 0,
        waterNormalDispatches: 0,
        waterNormalDispatchSeq: 0,
        waterSampledStateCopies: 0,
        waterSampledStateSyncSeq: 0,
        // M5 at-rest gating (water-parity-campaign) -- see
        // WATER_REST_ENERGY_EPSILON's comment above for the mechanism.
        // waterAtRestSystems: how many systems are parked (skipping sim
        // substeps/normal/state-copy/caustics THIS frame) right now.
        // waterRestSubstepsSkipped: cumulative solver substeps NOT dispatched
        // because their system was at rest -- the direct measure of the win.
        waterAtRestSystems: 0,
        waterRestSubstepsSkipped: 0,
        // M6 per-frame churn audit -- see waterUniformSnapshotChanged's
        // comment (near sceneWaterUniformData). Direct measure of the
        // uniform-upload dedup: how many "commit" writeBuffer calls actually
        // hit the GPU (waterUniformUploads) vs were skipped because nothing
        // but the volatile time/frameIndex header changed (waterUniformUploadsSkipped).
        waterUniformUploads: 0,
        waterUniformUploadsSkipped: 0,
        waterQualityTier: "full",
        waterQualityRevision: 0,
        waterSurfaceResolution: 0,
        waterActiveCausticsResolution: 0,
        waterActiveObjectShadowResolution: 0,
        waterActiveObjectTextureWidth: 0,
        waterActiveObjectTextureHeight: 0,
        waterActiveObjectTexturePixelBudget: 0,
        waterQualityAllocationPending: 0,
        waterQualityAllocationFailures: 0,
        waterQualityAllocationRetryFrame: 0,
        waterQualityDPRCap: Infinity,
        waterExpensivePassCadence: 1,
        waterAuthoredComputeSystems: 0,
        waterAuthoredComputeDispatches: 0,
        waterAuthoredComputeFallbacks: 0,
        // Compute kernels routed through the generic descriptor-driven Selena
        // feedback-compute path (getSelenaComputePipeline/
        // createSelenaComputeBindGroup, dispatchWaterComputeStage above).
        // waterSelenaComputeSystems counts a system once if ANY of its 5
        // kernels have Selena WGSL+descriptor configured (mirrors
        // waterAuthoredComputeSystems' "was authored, not necessarily
        // dispatched-this-frame" semantics); waterSelenaComputeDispatches/
        // waterSelenaComputeFallbacks aggregate across all 5 kernels and every
        // dispatch call site (continuous + one-shot events), mirroring the
        // render passes' waterSelenaXxxPasses/waterSelenaXxxFallbacks pattern.
        waterSelenaComputeSystems: 0,
        waterSelenaComputeDispatches: 0,
        waterSelenaComputeFallbacks: 0,
        waterDropDispatches: 0,
        waterDropDispatchTotal: 0,
        waterLastDropEventID: 0,
        waterObjectSystems: 0,
        waterObjectDispatches: 0,
        waterObjectEventDispatches: 0,
        waterLastObjectDisplacementEventID: 0,
        waterObjectSpheres: 0,
        waterRoundedSystems: 0,
        waterCornerRadius: 0,
        waterLightDirX: 0,
        waterLightDirY: 0,
        waterLightDirZ: 0,
        waterCausticSystems: 0,
        waterCausticPasses: 0,
        waterCausticTexturePixels: 0,
        waterAuthoredCausticSystems: 0,
        waterAuthoredCausticPasses: 0,
        waterAuthoredCausticFallbacks: 0,
        waterAuthoredCausticFallbackReason: "",
        waterAuthoredCausticSourceBytes: 0,
        waterEntryCausticSourceBytes: 0,
        waterResolvedCausticSourceBytes: 0,
        waterAuthoredSurfaceSourceBytes: 0,
        waterEntrySurfaceSourceBytes: 0,
        waterResolvedSurfaceSourceBytes: 0,
        waterManifestShaderSystems: 0,
        waterManifestShaderFields: 0,
        waterManifestCausticSourceBytes: 0,
        waterManifestSurfaceSourceBytes: 0,
        waterBundleShaderSystems: activeWaterShaderSourcesByID ? activeWaterShaderSourcesByID.size : 0,
        waterBundleCausticSourceBytes: 0,
        waterBundleSurfaceSourceBytes: 0,
        waterObjectTexturePasses: 0,
        waterObjectTextureTargets: 0,
        waterObjectTexturePixels: 0,
        waterObjectTextureWidth: 0,
        waterObjectTextureHeight: 0,
        waterObjectTexturePixelBudget: 0,
        waterObjectTextureMeshPasses: 0,
        waterObjectTextureMeshDrawCalls: 0,
        waterObjectTextureSelenaDrawCalls: 0,
        waterObjectTextureFallbackPasses: 0,
        waterObjectTextureCandidateObjects: 0,
        waterObjectTextureSelectedObjects: 0,
        waterObjectTextureFallbackMissingObjects: 0,
        waterObjectTextureFallbackMissingResources: 0,
        waterObjectTextureCandidateProfile: "",
        waterObjectShadowPasses: 0,
        waterObjectShadowTexturePixels: 0,
        waterObjectShadowMeshPasses: 0,
        waterObjectShadowMeshDrawCalls: 0,
        waterAuthoredObjectShadowPasses: 0,
        waterAuthoredObjectShadowFallbacks: 0,
        waterAuthoredObjectMeshShadowPasses: 0,
        waterAuthoredObjectMeshShadowFallbacks: 0,
        waterObjectShadowFallbackPasses: 0,
        waterObjectShadowFallbackMissingObjects: 0,
        waterObjectShadowFallbackMissingResources: 0,
        waterReflectionSystems: 0,
        waterRefractionSystems: 0,
        waterObjectOpticsSystems: 0,
        // Caustics/object-shadow/compound-shadow/object-mesh-shadow passes
        // routed through the generic descriptor-driven Selena WebGPU render
        // path. See sceneWaterCausticsUsesSelena/getWaterCausticsSelenaDraw,
        // getWaterObjectShadowSelenaDraw, getWaterObjectMeshShadowSelenaDraw
        // above (the surface/surface-below/pool equivalents are aggregated by
        // drawWaterSystemEntries/drawWaterPoolEntries instead).
        waterSelenaCausticPasses: 0,
        waterSelenaCausticFallbacks: 0,
        waterSelenaObjectShadowPasses: 0,
        waterSelenaObjectShadowFallbacks: 0,
        waterSelenaObjectMeshShadowPasses: 0,
        waterSelenaObjectMeshShadowFallbacks: 0,
      };
      var manifestShaderStats = sceneWaterManifestShaderSourceStats();
      stats.waterManifestShaderSystems = manifestShaderStats.systems;
      stats.waterManifestShaderFields = manifestShaderStats.fields;
      stats.waterManifestCausticSourceBytes = manifestShaderStats.causticSourceBytes;
      stats.waterManifestSurfaceSourceBytes = manifestShaderStats.surfaceSourceBytes;
      if (activeWaterShaderSourcesByID) {
        activeWaterShaderSourcesByID.forEach(function(record) {
          stats.waterBundleCausticSourceBytes = Math.max(
            stats.waterBundleCausticSourceBytes,
            typeof record.causticsWGSL === "string" ? record.causticsWGSL.trim().length : 0
          );
          stats.waterBundleSurfaceSourceBytes = Math.max(
            stats.waterBundleSurfaceSourceBytes,
            sceneWaterSurfaceSourceBytes(record)
          );
        });
      }
      for (var i = 0; i < records.length; i++) {
        var system = records[i].system;
        if (!system) continue;
        if (qualityProfile && typeof qualityProfile === "object") {
          applySceneWaterQualityProfile(system, qualityProfile, qualityRevision, width, height);
        }
        var entry = system.entry || {};
        stats.waterEntryCausticSourceBytes = Math.max(
          stats.waterEntryCausticSourceBytes,
          typeof entry.causticsWGSL === "string" ? entry.causticsWGSL.trim().length : 0
        );
        // waterResolvedCausticSourceBytes/waterResolvedSurfaceSourceBytes/
        // waterAuthoredSurfaceSourceBytes stay 0 -- there is no more authored
        // (data-prop) WGSL resolution tier to measure; see
        // sceneWaterSystemSignature's comment above.
        stats.waterEntrySurfaceSourceBytes = Math.max(
          stats.waterEntrySurfaceSourceBytes,
          sceneWaterSurfaceSourceBytes(entry)
        );
        // Builtin pipelines only: the hand-written data-prop-authored compute
        // pipeline tier (sceneWaterAuthoredComputePipeline) has been removed
        // now that Selena is the sole primary compute WGSL source ahead of
        // these SCENE_WATER_COMPUTE_SOURCE-derived builtins (see
        // dispatchWaterComputeStage's Selena-first resolution below).
        var seedCompute = { pipeline: waterSeedPipeline, authored: false, failed: false };
        var dropCompute = { pipeline: waterDropPipeline, authored: false, failed: false };
        var displacementCompute = { pipeline: waterDisplacementPipeline, authored: false, failed: false };
        var simulationCompute = { pipeline: waterStepPipeline, authored: false, failed: false };
        var normalCompute = { pipeline: waterNormalPipeline, authored: false, failed: false };
        if (seedCompute.authored || dropCompute.authored || displacementCompute.authored || simulationCompute.authored || normalCompute.authored) {
          stats.waterAuthoredComputeSystems += 1;
        }
        if (seedCompute.failed) stats.waterAuthoredComputeFallbacks += 1;
        if (dropCompute.failed) stats.waterAuthoredComputeFallbacks += 1;
        if (displacementCompute.failed) stats.waterAuthoredComputeFallbacks += 1;
        if (simulationCompute.failed) stats.waterAuthoredComputeFallbacks += 1;
        if (normalCompute.failed) stats.waterAuthoredComputeFallbacks += 1;
        if (
          sceneWaterComputeStageUsesSelena(entry, "seed") ||
          sceneWaterComputeStageUsesSelena(entry, "drop") ||
          sceneWaterComputeStageUsesSelena(entry, "displacement") ||
          sceneWaterComputeStageUsesSelena(entry, "simulation") ||
          sceneWaterComputeStageUsesSelena(entry, "normal")
        ) {
          stats.waterSelenaComputeSystems += 1;
        }
        if (sceneWaterPoolShapeRounded(entry)) {
          stats.waterRoundedSystems += 1;
          stats.waterCornerRadius = Math.max(stats.waterCornerRadius, Math.max(0, sceneNumber(entry.cornerRadius, 0)));
        }
        var optics = sceneWaterOpticsFlags(entry, {
          kind: sceneWaterObjectKind(entry),
          displacementScale: Math.max(0, sceneNumber(entry.objectDisplacementScale, 1)),
        });
        if (optics.caustics) stats.waterCausticSystems += 1;
        if (optics.reflection) stats.waterReflectionSystems += 1;
        if (optics.refraction) stats.waterRefractionSystems += 1;
        if (optics.object) stats.waterObjectOpticsSystems += 1;
        var waterPaused = sceneBool(entry && entry.paused, false);
        var waterClock = waterClockAPI.sceneWaterAdvanceClock(system.waterClock, currentNowMS, waterLifecycleActive, waterPaused, {
          simulationHz: 60,
          // Bound each present to the reference model's two solver passes.
          // Any elapsed excess remains visible as dropped-tick telemetry.
          maxCatchUpTicks: 1,
          solverSubsteps: 2,
        });
        var canConsumeWaterState = waterLifecycleActive && !waterPaused;
        var hasSimulationTick = canConsumeWaterState && waterClock.ticks > 0;
        var fixedDeltaSeconds = waterClock.tickSeconds || (1 / 60);
        stats.waterSimulationTicks += Math.max(0, waterClock.ticks || 0);
        stats.waterSolverSubsteps += Math.max(0, waterClock.substeps || 0);
        stats.waterDroppedTicks += Math.max(0, waterClock.droppedTicks || 0);
        stats.waterDroppedTicksThisFrame += Math.max(0, waterClock.dropped || 0);
        stats.waterSimulationTickSeq += Math.max(0, waterClock.tickSeq || 0);
        stats.waterSolverSubstepSeq += Math.max(0, waterClock.solverSubstepSeq || 0);
        device.queue.writeBuffer(system.uniformBuffer, 0, sceneWaterUniformData(
          system, entry, fixedDeltaSeconds, currentTime,
          { transientObject: true }
        ));
        if (system.waterLightDir) {
          stats.waterLightDirX = sceneNumber(system.waterLightDir.x, 0);
          stats.waterLightDirY = sceneNumber(system.waterLightDir.y, 0);
          stats.waterLightDirZ = sceneNumber(system.waterLightDir.z, 0);
        }
        stats.waterCells += system.cellCount;
        stats.waterVertices += system.vertexCount;
        stats.waterQualityTier = system.qualityTier || "full";
        stats.waterQualityRevision = Math.max(stats.waterQualityRevision, system.qualityRevision || 0);
        stats.waterSurfaceResolution = Math.max(stats.waterSurfaceResolution, system.surfaceResolution || 0);
        stats.waterActiveCausticsResolution = Math.max(stats.waterActiveCausticsResolution, system.causticsResolution || 0);
        stats.waterActiveObjectShadowResolution = Math.max(stats.waterActiveObjectShadowResolution, system.objectShadowResolution || 0);
        stats.waterActiveObjectTextureWidth = Math.max(stats.waterActiveObjectTextureWidth, system.objectTextureWidth || 0);
        stats.waterActiveObjectTextureHeight = Math.max(stats.waterActiveObjectTextureHeight, system.objectTextureHeight || 0);
        stats.waterActiveObjectTexturePixelBudget = Math.max(stats.waterActiveObjectTexturePixelBudget, system.objectTexturePixelBudget || 0);
        if (system.qualityAllocationPending) stats.waterQualityAllocationPending += 1;
        stats.waterQualityAllocationFailures += Math.max(0, system.qualityAllocationFailures || 0);
        stats.waterQualityAllocationRetryFrame = Math.max(stats.waterQualityAllocationRetryFrame, system.qualityAllocationNextFrame || 0);
        stats.waterQualityDPRCap = Math.min(stats.waterQualityDPRCap, system.qualityDPRCap || 1);
        stats.waterExpensivePassCadence = Math.max(stats.waterExpensivePassCadence, system.expensivePassCadence || 1);
        var waterStateDirty = false;
        if (hasSimulationTick && !system.seeded) {
          system.seeded = true;
          if (Math.max(0, Math.floor(sceneNumber(entry.seedDrops, 7))) > 0) {
            var seedResult = dispatchWaterComputeStage(encoder, system, entry, "seed", seedCompute.pipeline);
            stats.waterComputeDispatches += seedResult.dispatches;
            stats.waterSelenaComputeDispatches += seedResult.selena;
            stats.waterSelenaComputeFallbacks += seedResult.selenaFallback;
            waterStateDirty = waterStateDirty || seedResult.dispatches > 0;
            if (seedCompute.authored && seedResult.selena === 0) stats.waterAuthoredComputeDispatches += seedResult.dispatches;
          }
        }
        // Queued multi-drop trail (see dispatchWaterDropEvents above): drains
        // every drop queued since the last consumed id, one uniform write +
        // compute dispatch each, so a fast stroke's whole burst lands in this
        // one frame instead of thinning to the single latest drop. Runs
        // BEFORE the legacy scalar block below so its system.lastDropEventID
        // update makes that block's `!== dropEventID` check a no-op on the
        // same frame (entry.dropEventID mirrors the newest queued event id),
        // avoiding a double dispatch of the same drop.
        var dropEventsResult = hasSimulationTick
          ? dispatchWaterDropEvents(system, entry, encoder, dropCompute.pipeline, currentTime)
          : { dispatches: 0, selena: 0, selenaFallback: 0 };
        stats.waterSelenaComputeDispatches += dropEventsResult.selena;
        stats.waterSelenaComputeFallbacks += dropEventsResult.selenaFallback;
        if (dropEventsResult.dispatches > 0) {
          system.dropDispatchCount = Math.max(0, Math.floor(sceneNumber(system.dropDispatchCount, 0))) + dropEventsResult.dispatches;
          stats.waterLastDropEventID = Math.max(stats.waterLastDropEventID, dropEventsResult.lastID || 0);
          stats.waterDropDispatches += dropEventsResult.dispatches;
          stats.waterComputeDispatches += dropEventsResult.dispatches;
          waterStateDirty = true;
          if (dropCompute.authored && dropEventsResult.selena === 0) stats.waterAuthoredComputeDispatches += dropEventsResult.dispatches;
        }
        var dropEventID = Math.max(0, Math.floor(sceneNumber(entry.dropEventID, 0)));
        if (hasSimulationTick && dropEventID > 0 && system.lastDropEventID !== dropEventID) {
          var dropResult = dispatchWaterComputeStage(encoder, system, entry, "drop", dropCompute.pipeline);
          var dropDispatches = dropResult.dispatches;
          stats.waterSelenaComputeDispatches += dropResult.selena;
          stats.waterSelenaComputeFallbacks += dropResult.selenaFallback;
          if (dropDispatches > 0) {
            system.lastDropEventID = dropEventID;
            system.dropDispatchCount = Math.max(0, Math.floor(sceneNumber(system.dropDispatchCount, 0))) + dropDispatches;
            stats.waterLastDropEventID = Math.max(stats.waterLastDropEventID, dropEventID);
            stats.waterDropDispatches += dropDispatches;
            stats.waterComputeDispatches += dropDispatches;
            waterStateDirty = true;
            if (dropCompute.authored && dropResult.selena === 0) stats.waterAuthoredComputeDispatches += dropDispatches;
          }
        }
        stats.waterLastDropEventID = Math.max(stats.waterLastDropEventID, Math.max(0, Math.floor(sceneNumber(system.lastDropEventID, 0))));
        stats.waterDropDispatchTotal += Math.max(0, Math.floor(sceneNumber(system.dropDispatchCount, 0)));
        var objectEventStats = hasSimulationTick
          ? dispatchWaterObjectDisplacementEvents(system, entry, encoder, displacementCompute.pipeline, currentTime)
          : { dispatches: 0, selena: 0, selenaFallback: 0 };
        stats.waterSelenaComputeDispatches += objectEventStats.selena;
        stats.waterSelenaComputeFallbacks += objectEventStats.selenaFallback;
        if (objectEventStats.dispatches > 0) {
          stats.waterObjectEventDispatches += objectEventStats.dispatches;
          stats.waterObjectDispatches += objectEventStats.dispatches;
          stats.waterComputeDispatches += objectEventStats.dispatches;
          waterStateDirty = true;
          if (displacementCompute.authored && objectEventStats.selena === 0) stats.waterAuthoredComputeDispatches += objectEventStats.dispatches;
        }
        stats.waterLastObjectDisplacementEventID = Math.max(stats.waterLastObjectDisplacementEventID, Math.max(0, Math.floor(sceneNumber(system.lastObjectDisplacementEventID, 0))));
        if (hasSimulationTick) {
          // Commit current/previous object state exactly once per simulation
          // tick frame, after transient one-shot event uniforms are consumed.
          // Zero-tick display frames leave the previous center untouched.
          // M6: pack once, then skip the actual GPU upload when nothing but
          // the volatile time/frameIndex header changed -- see
          // waterUniformSnapshotChanged's comment (near sceneWaterUniformData).
          var commitUniformData = sceneWaterUniformData(system, entry, fixedDeltaSeconds, currentTime);
          if (waterUniformSnapshotChanged(system)) {
            device.queue.writeBuffer(system.uniformBuffer, 0, commitUniformData);
            stats.waterUniformUploads += 1;
          } else {
            stats.waterUniformUploadsSkipped += 1;
          }
          if ((system.waterObjectActive || (system.waterObjectKind || 0) > 0) && system.waterObjectMoved) {
            stats.waterObjectSystems += 1;
            stats.waterObjectSpheres += Math.max(0, system.waterObjectSphereCount || 0);
            var objectResult = dispatchWaterComputeStage(encoder, system, entry, "displacement", displacementCompute.pipeline);
            var objectDispatches = objectResult.dispatches;
            stats.waterObjectDispatches += objectDispatches;
            stats.waterComputeDispatches += objectDispatches;
            stats.waterSelenaComputeDispatches += objectResult.selena;
            stats.waterSelenaComputeFallbacks += objectResult.selenaFallback;
            waterStateDirty = waterStateDirty || objectDispatches > 0;
            if (displacementCompute.authored && objectResult.selena === 0) stats.waterAuthoredComputeDispatches += objectDispatches;
          }
          // M5 at-rest gating (water-parity-campaign): decide whether to
          // actually run this tick's substeps/normal/state-copy/caustics, or
          // to retain the last-rendered textures. waterStateDirty is fully
          // finalized as of this line (seed/drop/queued object event/
          // continuous drag all landed above), so a disturbance dispatched
          // EARLIER in this same frame wakes the system immediately -- no
          // one-frame lag. See WATER_REST_ENERGY_EPSILON's comment for the
          // energy-proxy mechanism and WATER_REST_MIN_QUIET_MS for the floor.
          if (waterStateDirty) {
            system.waterRestEnergy = 1.0;
            system.waterLastDisturbanceMS = currentNowMS;
            system.waterAtRest = false;
          }
          var runWaterSim = !system.waterAtRest;
          if (runWaterSim) {
            // P3 fusion (water-parity-campaign): batch the tick's simulation
            // substeps AND the trailing normal reconstruction into ONE
            // compute pass instead of 3 separate beginComputePass/end()
            // sequences (2 substeps + 1 normal). This only cuts command-
            // encoding overhead, not GPU work or visibility: WebGPU
            // auto-synchronizes storage-buffer read-after-write hazards
            // between dispatchWorkgroups() calls in submission order
            // regardless of pass boundaries, so the normal dispatch -- issued
            // after every substep dispatch in program order, sharing this
            // same pass -- still observes exactly the post-final-substep
            // height field it did as a separate pass. copyBufferToTexture
            // (syncWaterSampledState) cannot be recorded while a pass is
            // open, so it stays its own encoder step after this pass ends.
            var waterSimPass = encoder.beginComputePass({ label: "gosx-water-sim-normal-pass" });
            for (var waterTick = 0; waterTick < waterClock.ticks; waterTick++) {
              for (var solverStep = 0; solverStep < 2; solverStep++) {
                var stepResult = dispatchWaterComputeStage(encoder, system, entry, "simulation", simulationCompute.pipeline, waterSimPass);
                stats.waterComputeDispatches += stepResult.dispatches;
                stats.waterSelenaComputeDispatches += stepResult.selena;
                stats.waterSelenaComputeFallbacks += stepResult.selenaFallback;
                if (simulationCompute.authored && stepResult.selena === 0) {
                  stats.waterAuthoredComputeDispatches += stepResult.dispatches;
                }
              }
            }
            // Decay the cheap energy proxy by exactly the damping the
            // simulation kernel itself just applied, once per substep
            // actually dispatched this frame.
            var restDamping = Math.max(0, Math.min(1, sceneNumber(entry && entry.damping, 0.995)));
            var substepsRun = Math.max(0, waterClock.ticks) * 2;
            system.waterRestEnergy = Math.max(0, sceneNumber(system.waterRestEnergy, 1)) * Math.pow(restDamping, substepsRun);
            var quietMS = currentNowMS - Math.max(0, sceneNumber(system.waterLastDisturbanceMS, 0));
            if (!waterStateDirty && system.waterRestEnergy <= WATER_REST_ENERGY_EPSILON && quietMS >= WATER_REST_MIN_QUIET_MS) {
              system.waterAtRest = true;
            }
            // hasSimulationTick is guaranteed true here (runWaterSim is only
            // ever assigned inside the hasSimulationTick branch above), so
            // this folds the normal dispatch into the SAME pass unconditionally
            // -- equivalent to the former separate `if (hasSimulationTick &&
            // runWaterSim)` gate, now with a shared pass instead of its own.
            var normalResult = dispatchWaterComputeStage(encoder, system, entry, "normal", normalCompute.pipeline, waterSimPass);
            waterSimPass.end();
            var normalDispatches = normalResult.dispatches;
            stats.waterComputeDispatches += normalDispatches;
            stats.waterSelenaComputeDispatches += normalResult.selena;
            stats.waterSelenaComputeFallbacks += normalResult.selenaFallback;
            stats.waterNormalDispatches += normalDispatches;
            system.waterNormalDispatchSeq += normalDispatches;
            if (normalCompute.authored && normalResult.selena === 0) stats.waterAuthoredComputeDispatches += normalDispatches;
          } else {
            stats.waterRestSubstepsSkipped += Math.max(0, waterClock.ticks) * 2;
          }
        } else {
          var runWaterSim = false;
        }
        if (system.waterAtRest) stats.waterAtRestSystems += 1;
        if ((hasSimulationTick && runWaterSim) || system.stateTextureSyncSeq === 0) {
          stats.waterSampledStateCopies += syncWaterSampledState(encoder, system);
        }
        stats.waterSampledStateSyncSeq += Math.max(0, Math.floor(sceneNumber(system.stateTextureSyncSeq, 0)));
        stats.waterNormalDispatchSeq += Math.max(0, system.waterNormalDispatchSeq || 0);
        var expensivePassCadence = Math.max(1, system.expensivePassCadence || 1);
        var expensiveCadenceBucket = Math.floor(Math.max(0, waterClock.tickSeq || 0) / expensivePassCadence);
        // M5: while at rest and not freshly disturbed this frame, do not let
        // the cadence bucket's natural drift (it advances off the wall clock
        // regardless of whether we chose to consume ticks) force a caustics
        // refresh -- system.waterExpensiveCadenceBucket is deliberately left
        // stale below so the very next refresh (on wake) is unconditional.
        var refreshExpensivePasses = waterStateDirty || (!system.waterAtRest && system.waterExpensiveCadenceBucket !== expensiveCadenceBucket);
        if (refreshExpensivePasses) system.waterExpensiveCadenceBucket = expensiveCadenceBucket;
        if (optics.object || optics.caustics) {
          var objectShadowPasses = 0;
          var meshShadow = { passes: 0, drawCalls: 0 };
          var hasShadowSubject = waterSystemHasObjectTextureSubject(system);
          var objectList = hasShadowSubject ? sceneWaterObjectMeshList(bundle, entry) : [];
          // M4: includeCamera=false -- the object-shadow RTT is a light-space
          // projection (see sceneWaterObjectRenderSignature's comment above),
          // so orbiting the camera must NOT invalidate this cache.
          var shadowSignature = sceneWaterObjectRenderSignature(system, entry, bundle, objectList, false);
          var refreshObjectShadow = system.waterObjectShadowSignature !== shadowSignature;
          hasShadowSubject = hasShadowSubject && refreshObjectShadow;
          if (hasShadowSubject && objectList.length > 0 && pbrBuffers) {
            meshShadow = renderWaterObjectMeshShadowPass(encoder, system, objectList, pbrBuffers);
          }
          if (meshShadow.passes > 0) {
            objectShadowPasses = meshShadow.passes;
            stats.waterObjectShadowMeshPasses += meshShadow.passes;
            stats.waterObjectShadowMeshDrawCalls += meshShadow.drawCalls;
            if (meshShadow.authored) stats.waterAuthoredObjectMeshShadowPasses += meshShadow.passes;
            if (meshShadow.failed) stats.waterAuthoredObjectMeshShadowFallbacks += 1;
            stats.waterSelenaObjectMeshShadowPasses += meshShadow.selena || 0;
            stats.waterSelenaObjectMeshShadowFallbacks += meshShadow.selenaFallback || 0;
          } else if (hasShadowSubject) {
            if (objectList.length === 0) stats.waterObjectShadowFallbackMissingObjects += 1;
            if (!pbrBuffers) stats.waterObjectShadowFallbackMissingResources += 1;
            var shadowResult = renderWaterObjectShadowPass(encoder, system);
            objectShadowPasses = shadowResult && shadowResult.passes || 0;
            if (shadowResult && shadowResult.authored) stats.waterAuthoredObjectShadowPasses += objectShadowPasses;
            if (shadowResult && shadowResult.failed) stats.waterAuthoredObjectShadowFallbacks += 1;
            stats.waterObjectShadowFallbackPasses += objectShadowPasses;
            stats.waterSelenaObjectShadowPasses += (shadowResult && shadowResult.selena) || 0;
            stats.waterSelenaObjectShadowFallbacks += (shadowResult && shadowResult.selenaFallback) || 0;
          }
          stats.waterObjectShadowPasses += objectShadowPasses;
          if (objectShadowPasses > 0) {
            system.waterObjectShadowSignature = shadowSignature;
            stats.waterObjectShadowTexturePixels += Math.max(0, system.objectShadowResolution || 0) * Math.max(0, system.objectShadowResolution || 0);
          }
        }
        if (optics.caustics && refreshExpensivePasses) {
          var causticResult = renderWaterCausticsPass(encoder, system);
          var causticPasses = causticResult && causticResult.passes || 0;
          stats.waterAuthoredCausticSourceBytes = Math.max(stats.waterAuthoredCausticSourceBytes, causticResult && causticResult.sourceBytes || 0);
          stats.waterCausticPasses += causticPasses;
          if (causticResult && causticResult.authored) {
            stats.waterAuthoredCausticSystems += 1;
            stats.waterAuthoredCausticPasses += causticPasses;
          }
          if (causticResult && causticResult.failed) {
            stats.waterAuthoredCausticFallbacks += 1;
            stats.waterAuthoredCausticFallbackReason = waterAuthoredCausticsPipelineLastError;
          }
          if (causticPasses > 0) {
            stats.waterCausticTexturePixels += Math.max(0, system.causticsResolution || 0) * Math.max(0, system.causticsResolution || 0);
          }
          stats.waterSelenaCausticPasses += (causticResult && causticResult.selena) || 0;
          stats.waterSelenaCausticFallbacks += (causticResult && causticResult.selenaFallback) || 0;
        }
        system.frameIndex += 1;
      }
      if (!Number.isFinite(stats.waterQualityDPRCap)) stats.waterQualityDPRCap = 1;
      return stats;
    }

    // -----------------------------------------------------------------------
    // General mechanism: route water RENDER passes through the generic
    // descriptor-driven Selena WebGPU render path (getSelenaPipeline /
    // createSelenaBindGroup for mesh & mesh+state kinds, getSelenaPostPipeline
    // / createSelenaPostBindGroup for post kind above). Originally built as a
    // pool-only proof-of-concept; generalized here into shared plumbing
    // (sceneWaterSelenaResourceRef / sceneWaterSpheresContextArray /
    // sceneWaterSelenaMaterial / sceneWaterSelenaUsesPass / getWaterSelenaMeshDraw
    // / getWaterSelenaPostDraw) so every migrated pass is a thin
    // material-builder + render-context-builder pair, not a copy-pasted clone
    // of the pool functions.
    //
    // The five feedback compute kernels (seed/drop/displacement/simulation/
    // normal) are OUT OF SCOPE (a separate task routes those through a Selena
    // feedback-compute host path); every pass below is a RENDER pass. Any
    // water pass/configuration NOT (yet) routable through Selena keeps using
    // its hand-written waterXxx*WGSL-style hardcoded pipeline/bind-group path
    // below unchanged (e.g. a rounded pool shape, which pool.sel does not
    // implement).
    // -----------------------------------------------------------------------

    // sceneWaterSelenaResourceRef builds a "gosx:water:<id>:<slot>" reference
    // string, the resource-ref convention sceneSelenaResourceRef/
    // sceneSelenaLiveTextureView/sceneSelenaLiveBuffer already parse for the
    // water-object custom materials AND for every water-system-owned Selena
    // pass below (pool, surface, surface-below, caustics, ...).
    function sceneWaterSelenaResourceRef(system, slot) {
      var id = String((system && system.id) || (system && system.entry && system.entry.id) || "water-main");
      return "gosx:water:" + id + ":" + slot;
    }

    // sceneWaterSpheresContextArray packs system.waterObjectSpheres (cached by
    // sceneWaterObjectState, the SAME array sceneWaterWriteObjectSphereBuffer
    // uploads to the hand-written objectSpheres SSBO) into the flat
    // Float32Array(WATER_MAX_DISPLACEMENT_SPHERES*4) shape the G1 array-uniform
    // packer (sceneSelenaWriteArrayUniformField) expects for a
    // `context { spheres : array<vec4,32> }` field -- used by surface/
    // surface-below/caustics/compound-shadow. Mirrors
    // sceneWaterWriteObjectSphereBuffer's per-sphere (x,y,z,radius) packing
    // exactly, so the Selena context array and the hand-written SSBO carry
    // byte-identical sphere data for the same frame.
    function sceneWaterSpheresContextArray(system) {
      var spheres = (system && Array.isArray(system.waterObjectSpheres)) ? system.waterObjectSpheres : [];
      var out = new Float32Array(WATER_MAX_DISPLACEMENT_SPHERES * 4);
      for (var i = 0; i < spheres.length && i < WATER_MAX_DISPLACEMENT_SPHERES; i++) {
        var sphere = spheres[i] || {};
        var offset = i * 4;
        out[offset] = sceneNumber(sphere.x, 0);
        out[offset + 1] = sceneNumber(sphere.y, 0);
        out[offset + 2] = sceneNumber(sphere.z, 0);
        out[offset + 3] = Math.max(0.0001, sceneNumber(sphere.radius, 0));
      }
      return out;
    }

    // sceneWaterKnotContextArray packs the 65-point trefoil torus-knot
    // polyline that surface.sel / surface-below.sel's SDF sphere-trace reads
    // via `context { knot : array<vec4,65> }`. The points depend on NOTHING in
    // the scene (pure function of the loop index — the shaders used to rebuild
    // them per FRAGMENT, ~260 transcendentals plus a 1040-byte dynamically
    // indexed private array that collapsed GPU occupancy), so they are computed
    // once here and cached module-level. Formula is byte-identical to the old
    // in-shader loop: theta = ki/64 * 2pi, rad = 0.17*(2+cos(3*theta))*0.5,
    // point = (rad*cos(2*theta), -0.17*sin(3*theta)*0.5, rad*sin(2*theta), 0).
    var sceneWaterKnotArrayCache;
    function sceneWaterKnotContextArray() {
      var out = sceneWaterKnotArrayCache;
      if (!out) {
        // .w of every element stays 0 (Float32Array zero-init), matching the
        // old vec4f(..., 0.0) fourth component.
        sceneWaterKnotArrayCache = out = new Float32Array(65 * 4);
        for (var ki = 0; ki <= 64; ki++) {
          var theta = ki / 64.0 * 6.283185307;
          var rad = 0.17 * (2.0 + Math.cos(3.0 * theta)) * 0.5;
          var offset = ki * 4;
          out[offset] = rad * Math.cos(2.0 * theta);
          out[offset + 1] = -0.17 * Math.sin(3.0 * theta) * 0.5;
          out[offset + 2] = rad * Math.sin(2.0 * theta);
        }
      }
      return out;
    }

    // sceneWaterSelenaUsesPass gates a pass on its Selena WGSL+descriptor both
    // being present (a pass-specific caller may AND in additional conditions,
    // e.g. sceneWaterPoolUsesSelena's "not rounded" check below).
    function sceneWaterSelenaUsesPass(entry, wgslField, descKey) {
      var wgsl = entry && typeof entry[wgslField] === "string" ? entry[wgslField].trim() : "";
      if (!wgsl) return false;
      var descriptors = entry && entry.shaderDescriptors;
      var layout = descriptors && typeof descriptors === "object" ? descriptors[descKey] : null;
      return !!(layout && typeof layout === "object" && layout.uniformBlock);
    }

    // sceneWaterSelenaMaterial resolves+memoizes the Selena "material"
    // envelope the generic getSelenaPipeline/createSelenaBindGroup (mesh kind)
    // and getSelenaPostPipeline/createSelenaPostBindGroup (post kind) paths
    // expect: a single combined vertex+fragment WGSL module (every Selena
    // water shader emits one module with both entry points, unlike the
    // hand-written contract which often splits vertex/fragment into two WGSL
    // sources) plus its host binding descriptor (entry.shaderDescriptors[descKey],
    // already compiled for the WebGL2 path -- Selena's bindings.Layout is
    // backend-agnostic, see selena_glsl.go / selena_wgsl_binding_test.go
    // TestWaterSelenaWGSLDescriptorMatchesBindings). Returns null when either
    // the WGSL or descriptor is missing, so the caller falls back to the
    // hand-written path. The resolved material is memoized on
    // `system[memoSlot]` (one stable object per pass per system, so
    // getSelenaPipeline's per-material pipeline memo and
    // createSelenaBindGroup's per-owner bind-group pool stay warm across
    // frames); callers still assign a FRESH customUniforms object every call
    // (uniform VALUES are per-frame, only the WGSL/layout identity is stable).
    function sceneWaterSelenaMaterial(system, entry, wgslField, descKey, memoSlot) {
      if (!system) return null;
      var wgsl = entry && typeof entry[wgslField] === "string" ? entry[wgslField].trim() : "";
      if (!wgsl) return null;
      var descriptors = entry && entry.shaderDescriptors;
      var layout = descriptors && typeof descriptors === "object" ? descriptors[descKey] : null;
      if (!layout || typeof layout !== "object" || !layout.uniformBlock) return null;

      var material = system[memoSlot];
      if (!material || material._gosxSelenaWGSLSrc !== wgsl || material._gosxSelenaLayoutRef !== layout) {
        material = { shaderBackend: "selena", customVertexWGSL: wgsl, customFragmentWGSL: wgsl, shaderLayout: layout };
        material._gosxSelenaWGSLSrc = wgsl;
        material._gosxSelenaLayoutRef = layout;
        system[memoSlot] = material;
      }
      return material;
    }

    // getWaterSelenaMeshDraw resolves the {pipeline, bindGroup} pair for a
    // mesh/mesh+state-kind Selena water pass, or null if the pipeline/bind
    // group could not be built (caller falls back to the hand-written path;
    // this can happen the same way any authored-shader path can fail: WGSL
    // validation rejection, memoized as a failure by getSelenaPipeline, or a
    // live resource -- state/texture/grid -- not being ready yet).
    // pipelineOptions is forwarded to getSelenaPipeline as-is (cullMode/
    // targetFormat/sampleCount/depthStencil/labelSuffix); blendMode/depthWrite
    // default to "opaque"/true; pass them explicitly to override. Water
    // surfaces are alpha blended but deliberately retain depth writes so
    // pool/object intersections remain stable.
    function getWaterSelenaMeshDraw(material, renderContext, system, pipelineOptions) {
      if (!material) return null;
      var opts = pipelineOptions || {};
      var blendMode = typeof opts.blendMode === "string" && opts.blendMode ? opts.blendMode : "opaque";
      var depthWrite = Object.prototype.hasOwnProperty.call(opts, "depthWrite") ? opts.depthWrite : true;
      var selenaResource = getSelenaPipeline(material, blendMode, depthWrite, opts);
      if (!selenaResource) return null;
      var bindGroup = createSelenaBindGroup(material, selenaResource, system, renderContext);
      if (!bindGroup) return null;
      // attrs is forwarded (not just pipeline/bindGroup) so a per-object draw
      // loop with its own vertex-buffer binding convention (e.g.
      // drawWaterObjectProjectedShadowObjectsSelena's reuse of
      // bindWaterObjectSelenaAttributes below) can bind attributes without a
      // second getSelenaPipeline call.
      return { pipeline: selenaResource.pipeline, bindGroup: bindGroup, attrs: selenaResource.attrs };
    }

    // getWaterSelenaPostDraw mirrors getWaterSelenaMeshDraw for a post-kind
    // Selena water pass (object-shadow/compound-shadow below).
    function getWaterSelenaPostDraw(material, renderContext, system, pipelineOptions) {
      if (!material) return null;
      var selenaResource = getSelenaPostPipeline(material, pipelineOptions);
      if (!selenaResource) return null;
      var bindGroup = createSelenaPostBindGroup(material, selenaResource, system, renderContext);
      if (!bindGroup) return null;
      return { pipeline: selenaResource.pipeline, bindGroup: bindGroup };
    }

    // --- Pool pass (the original template) ----------------------------------

    // sceneWaterPoolSelenaMaterial builds (and memoizes on the system) the
    // pool pass's Selena material + its per-frame customUniforms (live
    // render-target resource refs + the tile texture URL).
    function sceneWaterPoolSelenaMaterial(system, entry) {
      var material = sceneWaterSelenaMaterial(system, entry, "poolSelenaWGSL", "pool", "_selenaPoolMaterial");
      if (!material) return null;

      // Live water render targets (caustics/shadow) are resolved through the
      // SAME gosx:water:<id>:<slot> resource-ref mechanism the water-object
      // custom materials already use; the state heightfield resolves through
      // the descriptor's `state` name ("height", matching pool.sel's
      // `state height`). The tile texture is a plain user-config URL (not a
      // live GPU resource), so it is supplied as a literal string, exactly
      // like createWaterPoolBindGroup's tileTexture handling below.
      var tileURL = entry && typeof entry.tileTexture === "string" ? entry.tileTexture.trim() : "";
      material.customUniforms = {
        tileTexture: tileURL,
        causticTexture: sceneWaterSelenaResourceRef(system, "caustics"),
        shadowTexture: sceneWaterSelenaResourceRef(system, "shadow"),
        height: sceneWaterSelenaResourceRef(system, "state"),
      };

      // Mirror createWaterPoolBindGroup's tile-texture bookkeeping so
      // diagnostics (waterPoolTileTexture* stats) stay accurate regardless of
      // which pool path rendered this frame. wgpuLoadTexture is memoized by
      // URL in textureCache, so this is not a duplicate fetch.
      var tileRecord = tileURL ? wgpuLoadTexture(device, tileURL, textureCache) : null;
      system.waterPoolTileRequested = !!tileURL;
      system.waterPoolTileLoaded = Boolean(tileRecord && tileRecord.loaded && tileRecord.view);
      system.waterPoolTilePending = Boolean(tileRecord && tileRecord.pending && !tileRecord.loaded && !tileRecord.failed);
      system.waterPoolTileFailed = Boolean(tileRecord && tileRecord.failed);

      return material;
    }

    // sceneWaterPoolSelenaRenderContext builds the per-frame name→value
    // uniform map (renderContext.uniforms) plus the StateGrid size
    // (renderContext.grid) for the pool pass. It reads the ALREADY-COMPUTED
    // per-frame derivations sceneWaterUniformData stamps onto the system
    // (waterPoolWidth/Height/Length, waterLightDir, waterResolution) rather
    // than recomputing them, so the Selena path and the hand-written path see
    // byte-identical pool geometry/lighting inputs for the same entry config.
    // mvp/normalMatrix are NOT included here: sceneSelenaUniformValue already
    // supplies those automatically (reserved auto-uniforms, priority before
    // renderContext.uniforms) from scratchSelenaViewProjection.
    // sceneWaterPoolSelenaRenderContext builds the per-frame name→value
    // uniform map (renderContext.uniforms) plus the StateGrid size
    // (renderContext.grid) for the pool pass. It reads the ALREADY-COMPUTED
    // per-frame derivations sceneWaterUniformData stamps onto the system
    // (waterPoolWidth/Height/Length, waterCornerRadius, waterLightDir,
    // waterResolution) rather than recomputing them, so the Selena path and
    // the hand-written path see byte-identical pool geometry/lighting inputs
    // for the same entry config. cornerRadius/poolShape feed pool.sel's own
    // `poolShape > 0.5 && cornerRadius > 0.0001` gate (mirroring the
    // hand-written pool.vertex.sel contract exactly): poolShape is derived
    // straight from entry.poolShape (sceneWaterPoolShapeRounded), independent
    // of whatever radius clamping sceneWaterUniformData already applied to
    // waterCornerRadius, so the shader's own gate is the single source of
    // truth for whether the rounded geometry is actually active.
    function sceneWaterPoolSelenaRenderContext(system) {
      var light = (system && system.waterLightDir) || { x: 0.3, y: 0.9, z: 0.45 };
      var entry = (system && system.entry) || {};
      return {
        uniformSlotSuffix: "water-pool-" + String((system && system.id) || "water"),
        uniforms: {
          poolWidth: sceneNumber(system && system.waterPoolWidth, 1),
          poolLength: sceneNumber(system && system.waterPoolLength, 1),
          poolHeight: sceneNumber(system && system.waterPoolHeight, 1),
          cornerRadius: sceneNumber(system && system.waterCornerRadius, 0),
          poolShape: sceneWaterPoolShapeRounded(entry) ? 1 : 0,
          lightDir: [sceneNumber(light.x, 0.3), sceneNumber(light.y, 0.9), sceneNumber(light.z, 0.45)],
        },
        grid: sceneNumber(system && system.waterResolution, 256),
      };
    }

    // sceneWaterPoolUsesSelena gates the new path: the Selena WGSL+descriptor
    // must both be present. pool.sel now ports the rounded-corner geometry
    // (see pool.sel's header comment), so both the box and rounded shapes
    // route through the Selena path; `rounded` is accepted for call-site
    // symmetry with sceneWaterSelenaUsesPass callers but no longer used to
    // force a fallback.
    function sceneWaterPoolUsesSelena(entry, rounded) {
      return sceneWaterSelenaUsesPass(entry, "poolSelenaWGSL", "pool");
    }

    // getWaterPoolSelenaDraw resolves the {pipeline, bindGroup} pair for the
    // generic Selena pool path, or null if the pipeline/bind group could not
    // be built (caller falls back to the hand-written pool path).
    function getWaterPoolSelenaDraw(system, entry) {
      var material = sceneWaterPoolSelenaMaterial(system, entry);
      var renderContext = sceneWaterPoolSelenaRenderContext(system);
      return getWaterSelenaMeshDraw(material, renderContext, system, { cullMode: "back" });
    }

    // --- Surface / surface-below passes -------------------------------------
    //
    // Drawn by drawWaterSurfaceSide below (WaterSystem-owned code, one call
    // per "above"/"below" side per system, mirroring the hand-written
    // getWaterRenderPipeline(system, side)/getWaterRenderBindGroupCached
    // pattern it sits alongside).

    // sceneWaterCameraPosFromCam extracts the water shaders' "cameraPos"
    // convention from the normalized camera object. It matches the
    // world-space eye position in FrameUniforms.cameraPos.
    function sceneWaterCameraPosFromCam(cam) {
      if (!cam) return { x: 0, y: 0, z: 0 };
      return {
        x: sceneNumber(cam.x, 0),
        y: sceneNumber(cam.y, 0),
        z: cam.mode === "ortho2d" ? 0 : sceneNumber(cam.z, 0),
      };
    }

    // sceneWaterSurfaceSelenaMaterial builds the surface pass's Selena
    // material + customUniforms: the tile/caustic/refraction/reflection/
    // clipped-reflection live resource refs, the "sky" cube map (a plain URL,
    // like pool's tileTexture -- resolved via the cube-texture path
    // sceneSelenaBindGroupLayout/createSelenaBindGroup added for
    // dimension:"cube" textures), and the "height" state ref. Also mirrors
    // createWaterRenderBindGroup's tile/sky-cube loaded-state bookkeeping so
    // diagnostics (waterSurfaceTile*/waterSkyCube* stats) stay accurate
    // regardless of which surface path rendered this frame.
    function sceneWaterSurfaceSelenaMaterial(system, entry, wgslField, descKey, memoSlot) {
      var material = sceneWaterSelenaMaterial(system, entry, wgslField, descKey, memoSlot);
      if (!material) return null;
      var tileURL = entry && typeof entry.tileTexture === "string" ? entry.tileTexture.trim() : "";
      var cubeURL = entry && typeof entry.cubeMap === "string" ? entry.cubeMap.trim() : "";
      material.customUniforms = {
        tileTexture: tileURL,
        causticTexture: sceneWaterSelenaResourceRef(system, "caustics"),
        sky: cubeURL,
        objectRefractionTex: sceneWaterSelenaResourceRef(system, "refraction"),
        objectReflectionTex: sceneWaterSelenaResourceRef(system, "reflection"),
        objectClippedReflectionTex: sceneWaterSelenaResourceRef(system, "clippedReflection"),
        height: sceneWaterSelenaResourceRef(system, "state"),
      };
      var tileRecord = tileURL ? wgpuLoadTexture(device, tileURL, textureCache) : null;
      var cubeRecord = cubeURL ? wgpuLoadCubeTexture(device, cubeURL, textureCache) : null;
      system.waterSurfaceTileRequested = !!tileURL;
      system.waterSurfaceTileLoaded = Boolean(tileRecord && tileRecord.loaded && tileRecord.view);
      system.waterSurfaceTilePending = Boolean(tileRecord && tileRecord.pending && !tileRecord.loaded && !tileRecord.failed);
      system.waterSurfaceTileFailed = Boolean(tileRecord && tileRecord.failed);
      system.waterSkyCubeRequested = !!cubeURL;
      system.waterSkyCubeLoaded = Boolean(cubeRecord && cubeRecord.loaded && cubeRecord.view);
      system.waterSkyCubePending = Boolean(cubeRecord && cubeRecord.pending && !cubeRecord.loaded && !cubeRecord.failed);
      system.waterSkyCubeFailed = Boolean(cubeRecord && cubeRecord.failed);
      return material;
    }

    // sceneWaterSurfaceSelenaRenderContext builds the per-frame renderContext
    // shared by the surface AND surface-below passes (surface-below is the
    // exact same field set minus refractionMatrix/reflectionMatrix/waterColor,
    // which the generic packer simply won't find in surface-below's
    // descriptor -- extra renderContext.uniforms entries a material's fields
    // don't reference are harmless, see sceneSelenaUniformData's
    // per-declared-field loop).
    //
    // waterColor is an AUTHOR `param` on WaterSurface (surface.sel), not a
    // `context` field, but the generic uniform packer doesn't distinguish
    // param/context -- it just needs a value from renderContext.uniforms OR
    // material.customUniforms OR a compiled descriptor default, and
    // WaterSurface's `param waterColor : vec3` has no literal default (so the
    // descriptor carries none either), which used to leave it packed as
    // (0,0,0) via sceneSelenaUniformValue's final "zero" fallback. surface.sel
    // multiplies the ENTIRE refracted branch (pool floor/walls/caustics/
    // submerged objects) by waterColor whenever the refraction ray points
    // down into the water -- the common case when looking down at the pool --
    // so a zeroed waterColor blacked out refraction/caustics/pool-interior
    // through the surface, matching the reported near-black regression. Mirror
    // WebGL2's createWaterRenderBindGroup-equivalent call site
    // (16-scene-webgl.js's `sceneWaterRenderHexColor(entry.shallowColor, ...)`)
    // using the shared sceneColorRGBA helper (11-scene-math.ts) both renderers
    // already use elsewhere (see sceneWaterUniformData's `shallow` derivation
    // above), so both backends tint the underwater refraction from the SAME
    // entry.shallowColor config.
    function sceneWaterSurfaceSelenaRenderContext(system, camera, uniformSlotName) {
      var entry = (system && system.entry) || {};
      var light = (system && system.waterLightDir) || { x: 0.3, y: 0.9, z: 0.45 };
      var half = (system && system.waterObjectHalfSize) || { x: 0, y: 0, z: 0 };
      var center = (system && system.waterObjectCenter) || { x: 0, y: 0, z: 0 };
      var optics = (system && system.waterOpticsFlags) || {};
      var camPos = sceneWaterCameraPosFromCam(camera);
      var refractionMatrix = (system && system.objectViewProjectionReady) ? system.objectViewProjectionMatrix : scratchSelenaViewProjection;
      var reflectionMatrix = (system && system.objectReflectionViewProjectionReady) ? system.objectReflectionViewProjectionMatrix : scratchSelenaViewProjection;
      var fallbackWaterColor = sceneColorRGBA(entry.shallowColor, [0.48, 0.82, 0.92, 1]);
      var hasHDRWaterColor = sceneNumber(entry.aboveWaterColorR, 0) !== 0 || sceneNumber(entry.aboveWaterColorG, 0) !== 0 || sceneNumber(entry.aboveWaterColorB, 0) !== 0;
      var waterColor = hasHDRWaterColor
        ? [sceneNumber(entry.aboveWaterColorR, 0.25), sceneNumber(entry.aboveWaterColorG, 1), sceneNumber(entry.aboveWaterColorB, 1.25)]
        : fallbackWaterColor;
      return {
        uniformSlotSuffix: uniformSlotName + "-" + String((system && system.id) || "water"),
        uniforms: {
          poolWidth: sceneNumber(system && system.waterPoolWidth, 1),
          poolLength: sceneNumber(system && system.waterPoolLength, 1),
          poolHeight: sceneNumber(system && system.waterPoolHeight, 1),
          cornerRadius: sceneNumber(system && system.waterCornerRadius, 0),
          poolShape: sceneWaterPoolShapeRounded(entry) ? 1 : 0,
          // normalScale has a compiled descriptor default (1.0, matching
          // surface.sel's own `param normalScale : float = 1.0`), so this
          // omission was never a zeroing bug like waterColor -- but it DID
          // silently ignore a live entry.normalScale override the way
          // WebGL2's sceneWaterUniformData packs it. Forward it explicitly
          // (mirrors the caustics render context's identical fix above).
          normalScale: sceneNumber(entry.normalScale, 1.0),
          objectRadius: sceneNumber(system && system.waterObjectRadius, 0.3),
          opticsCaustic: optics.caustics ? 1 : 0,
          gridResolution: sceneNumber(system && system.surfaceResolution, sceneNumber(system && system.waterResolution, 256)),
          objectKind: sceneNumber(system && system.waterObjectKind, 0),
          objectSubtype: sceneNumber(system && system.waterObjectSubtype, 0),
          objectCount: sceneNumber(system && system.waterObjectSphereCount, 0),
          opticsEnable: optics.object ? 1 : 0,
          waterColor: [waterColor[0], waterColor[1], waterColor[2]],
          lightDir: [sceneNumber(light.x, 0.3), sceneNumber(light.y, 0.9), sceneNumber(light.z, 0.45)],
          cameraPos: [camPos.x, camPos.y, camPos.z],
          objectCenter: [sceneNumber(center.x, 0), sceneNumber(center.y, 0), sceneNumber(center.z, 0)],
          objectHalf: [sceneNumber(half.x, 0), sceneNumber(half.y, 0), sceneNumber(half.z, 0)],
          refractionMatrix: refractionMatrix,
          reflectionMatrix: reflectionMatrix,
          spheres: sceneWaterSpheresContextArray(system),
          knot: sceneWaterKnotContextArray(),
        },
        grid: sceneNumber(system && system.waterResolution, 256),
      };
    }

    function sceneWaterSurfaceUsesSelena(entry) {
      return sceneWaterSelenaUsesPass(entry, "surfaceSelenaWGSL", "surface");
    }

    function sceneWaterSurfaceBelowUsesSelena(entry) {
      return sceneWaterSelenaUsesPass(entry, "surfaceBelowSelenaWGSL", "surfaceBelow");
    }

    // getWaterSurfaceSelenaDraw / getWaterSurfaceBelowSelenaDraw resolve the
    // {pipeline, bindGroup} pair for the "above"/"below" surface passes.
    // Alpha-blended, depth-writing, single-sided cull (opposite winding per
    // side) -- matching upstream WaterSurfacePass and the hand-written
    // descriptor (fragment blend:"alpha", depthWriteEnabled:true,
    // cullMode: side==="below"?"back":"front").
    function getWaterSurfaceSelenaDraw(system, entry, camera) {
      var material = sceneWaterSurfaceSelenaMaterial(system, entry, "surfaceSelenaWGSL", "surface", "_selenaSurfaceMaterial");
      var renderContext = sceneWaterSurfaceSelenaRenderContext(system, camera, "water-surface");
      return getWaterSelenaMeshDraw(material, renderContext, system, { blendMode: "alpha", depthWrite: true, cullMode: "front" });
    }

    function getWaterSurfaceBelowSelenaDraw(system, entry, camera) {
      var material = sceneWaterSurfaceSelenaMaterial(system, entry, "surfaceBelowSelenaWGSL", "surfaceBelow", "_selenaSurfaceBelowMaterial");
      var renderContext = sceneWaterSurfaceSelenaRenderContext(system, camera, "water-surface-below");
      return getWaterSelenaMeshDraw(material, renderContext, system, { blendMode: "alpha", depthWrite: true, cullMode: "back" });
    }

    // --- Caustics pass -------------------------------------------------------
    //
    // Drawn by renderWaterCausticsPass below into its own offscreen
    // WATER_CAUSTICS_TEXTURE_FORMAT target (no depth attachment, no MSAA) --
    // depthStencil:false + sampleCount:1 mirror the hand-written
    // waterCausticsPipeline's descriptor exactly.

    function sceneWaterCausticsSelenaMaterial(system, entry) {
      var material = sceneWaterSelenaMaterial(system, entry, "causticsSelenaWGSL", "caustics", "_selenaCausticsMaterial");
      if (!material) return null;
      // objectShadowTexture: the SAME "gosx:water:<id>:shadow" resource-ref
      // slot water-pool.sel already binds as `shadowTexture` (resolves to
      // system.objectShadowView, sceneSelenaLiveTextureView's "shadow"/
      // "objectShadow" case). Closes M2's meshShadowTextureOcclusion gap --
      // see caustics.sel's header RESOLVED note.
      material.customUniforms = {
        height: sceneWaterSelenaResourceRef(system, "state"),
        objectShadowTexture: sceneWaterSelenaResourceRef(system, "shadow"),
      };
      return material;
    }

    // normalScale: WaterCaustics (like WaterSurface/WaterSurfaceBelow above)
    // declares `param normalScale : float = 1.0` -- a compiled descriptor
    // default exists, so omitting it here isn't a zeroing bug like waterColor
    // was (no live default -> sceneSelenaUniformValue's default-lookup step
    // finds the compiled 1.0 and uses it), but it DOES silently ignore a
    // live entry.normalScale override (WebGL2's sceneWaterUniformData packs
    // the live value into the hand-written WaterUniforms buffer at the same
    // index the caustics/surface passes read). Forward it explicitly so a
    // user-configured WaterSystem normalScale prop actually reaches the
    // Selena caustics pass instead of silently pinning to the shader's own
    // default.
    function sceneWaterCausticsSelenaRenderContext(system) {
      var entry = (system && system.entry) || {};
      var light = (system && system.waterLightDir) || { x: 0.3, y: 0.9, z: 0.45 };
      var center = (system && system.waterObjectCenter) || { x: 0, y: 0, z: 0 };
      var half = (system && system.waterObjectHalfSize) || { x: 0, y: 0, z: 0 };
      var optics = (system && system.waterOpticsFlags) || {};
      return {
        uniformSlotSuffix: "water-caustics-" + String((system && system.id) || "water"),
        uniforms: {
          poolWidth: sceneNumber(system && system.waterPoolWidth, 1),
          poolLength: sceneNumber(system && system.waterPoolLength, 1),
          poolHeight: sceneNumber(system && system.waterPoolHeight, 1),
          normalScale: sceneNumber(entry.normalScale, 1.0),
          opticsEnable: optics.caustics ? 1 : 0,
          gridResolution: sceneNumber(system && system.surfaceResolution, 201),
          resolution: sceneNumber(system && system.waterResolution, 256),
          time: selenaFrame.time,
          objectKind: sceneNumber(system && system.waterObjectKind, 0),
          objectCount: sceneNumber(system && system.waterObjectSphereCount, 0),
          lightDir: [sceneNumber(light.x, 0.3), sceneNumber(light.y, 0.9), sceneNumber(light.z, 0.45)],
          objectCenter: [sceneNumber(center.x, 0), sceneNumber(center.y, 0), sceneNumber(center.z, 0)],
          objectHalfRadius: [sceneNumber(half.x, 0), sceneNumber(half.y, 0), sceneNumber(half.z, 0), sceneNumber(system && system.waterObjectRadius, 0)],
          spheres: sceneWaterSpheresContextArray(system),
          // Texel spacing for caustics.sel's objectShadowTexture 9-tap soft
          // sample (M2), forwarded live so a resized shadow RTT
          // (system.objectShadowResolution, WaterSystem's objectShadowResolution
          // prop) keeps the soft-shadow footprint texel-correct instead of
          // silently pinning to the shader's own compiled default.
          objectShadowTexelSize: 1.0 / Math.max(1, sceneNumber(system && system.objectShadowResolution, WATER_OBJECT_SHADOW_TEXTURE_SIZE)),
        },
        grid: sceneNumber(system && system.waterResolution, 256),
      };
    }

    function sceneWaterCausticsUsesSelena(entry) {
      return sceneWaterSelenaUsesPass(entry, "causticsSelenaWGSL", "caustics");
    }

    // getWaterCausticsSelenaDraw projects the authored water grid into the
    // caustics target. See drawCount usage in renderWaterCausticsPass.
    function getWaterCausticsSelenaDraw(system, entry) {
      var material = sceneWaterCausticsSelenaMaterial(system, entry);
      var renderContext = sceneWaterCausticsSelenaRenderContext(system);
      return getWaterSelenaMeshDraw(material, renderContext, system, {
        targetFormat: WATER_CAUSTICS_TEXTURE_FORMAT,
        sampleCount: 1,
        depthStencil: false,
        labelSuffix: "water-caustics",
      });
    }

    // --- Object shadow / compound shadow passes (post kind) -----------------
    //
    // Drawn by renderWaterObjectShadowPass below into system.objectShadowView
    // (WATER_OBJECT_TEXTURE_FORMAT, no depth attachment) -- getSelenaPostPipeline
    // never adds a depthStencil state, matching the hand-written
    // waterObjectShadowPipeline exactly. The raw hand-written contract
    // branches on objectParams.x (kind) INSIDE one shader; Selena splits this
    // into two materials (WaterObjectShadow for kind<2.5 -- sphere/cube --
    // and WaterCompoundShadow for kind>=2.5 -- the up-to-32-sphere compound
    // proxy), so the HOST selects the material by kind instead.

    // objectCenterY joined the shared context base for object-shadow.sel's P2
    // (water-parity-campaign) analytic rewrite, which reconstructs a 3D
    // world-space floor point and needs the object's full 3D center (not just
    // X/Z) for the sphereSoftShadow/cubeOcclusion terms. compound-shadow.sel's
    // compiled layout has no "objectCenterY" uniform field, so sharing this
    // base with sceneWaterCompoundShadowSelenaRenderContext is harmless --
    // sceneSelenaUniformData only reads uniforms.* keys that are actually
    // present in the COMPILED layout's fields (extra keys are ignored).
    function sceneWaterObjectShadowSelenaContextBase(system) {
      var light = (system && system.waterLightDir) || { x: 0.3, y: 0.9, z: 0.45 };
      var center = (system && system.waterObjectCenter) || { x: 0, y: 0, z: 0 };
      return {
        lightDir: [sceneNumber(light.x, 0.3), sceneNumber(light.y, 0.9), sceneNumber(light.z, 0.45)],
        objectCenterX: sceneNumber(center.x, 0),
        objectCenterY: sceneNumber(center.y, 0),
        objectCenterZ: sceneNumber(center.z, 0),
      };
    }

    function sceneWaterObjectShadowSelenaMaterial(system, entry) {
      var material = sceneWaterSelenaMaterial(system, entry, "objectShadowSelenaWGSL", "objectShadow", "_selenaObjectShadowMaterial");
      if (!material) return null;
      var half = (system && system.waterObjectHalfSize) || { x: 0, y: 0, z: 0 };
      material.customUniforms = {
        objectKind: sceneNumber(system && system.waterObjectKind, 0),
        objectEnabled: (system && system.waterObjectActive) ? 1 : 0,
        poolWidth: sceneNumber(system && system.waterPoolWidth, 1.5),
        poolLength: sceneNumber(system && system.waterPoolLength, 1.5),
        // poolHeight: the P2 analytic rewrite reconstructs a 3D floor point at
        // y = -poolHeight (matching caustics.sel's own floor-plane convention,
        // including its identical sceneNumber(...,1) fallback -- see
        // sceneWaterCausticsSelenaRenderContext above) instead of working
        // purely in UV space, so the live pool depth must reach the shader
        // instead of pinning to the compiled default.
        poolHeight: sceneNumber(system && system.waterPoolHeight, 1),
        objectRadius: sceneNumber(system && system.waterObjectRadius, 0.1),
        objectHalfX: sceneNumber(half.x, 0.1),
        objectHalfY: sceneNumber(half.y, 0.1),
        objectHalfZ: sceneNumber(half.z, 0.1),
      };
      return material;
    }

    function sceneWaterObjectShadowSelenaRenderContext(system) {
      return {
        uniformSlotSuffix: "water-object-shadow-" + String((system && system.id) || "water"),
        uniforms: sceneWaterObjectShadowSelenaContextBase(system),
      };
    }

    function sceneWaterCompoundShadowSelenaMaterial(system, entry) {
      var material = sceneWaterSelenaMaterial(system, entry, "compoundShadowSelenaWGSL", "compoundShadow", "_selenaCompoundShadowMaterial");
      if (!material) return null;
      material.customUniforms = {
        sphereCount: sceneNumber(system && system.waterObjectSphereCount, 0),
        objectEnabled: (system && system.waterObjectActive) ? 1 : 0,
        objectTop: sceneNumber(system && system.waterObjectCenter && system.waterObjectCenter.y, 0) + Math.max(
          sceneNumber(system && system.waterObjectHalfSize && system.waterObjectHalfSize.y, 0),
          sceneNumber(system && system.waterObjectRadius, 0)
        ),
        poolWidth: sceneNumber(system && system.waterPoolWidth, 1.5),
        poolLength: sceneNumber(system && system.waterPoolLength, 1.5),
      };
      return material;
    }

    function sceneWaterCompoundShadowSelenaRenderContext(system) {
      var base = sceneWaterObjectShadowSelenaContextBase(system);
      base.spheres = sceneWaterSpheresContextArray(system);
      return {
        uniformSlotSuffix: "water-compound-shadow-" + String((system && system.id) || "water"),
        uniforms: base,
      };
    }

    function sceneWaterObjectShadowUsesSelena(entry) {
      return sceneWaterSelenaUsesPass(entry, "objectShadowSelenaWGSL", "objectShadow");
    }

    function sceneWaterCompoundShadowUsesSelena(entry) {
      return sceneWaterSelenaUsesPass(entry, "compoundShadowSelenaWGSL", "compoundShadow");
    }

    // getWaterObjectShadowSelenaDraw picks WaterObjectShadow or
    // WaterCompoundShadow by the system's active object kind (mirroring the
    // raw hand-written shader's `objectParams.x >= 2.5` branch) and draws it
    // through the generic Selena post path.
    function getWaterObjectShadowSelenaDraw(system, entry) {
      var kind = sceneNumber(system && system.waterObjectKind, 0);
      var pipelineOptions = { targetFormat: WATER_OBJECT_TEXTURE_FORMAT, labelSuffix: "water-object-shadow" };
      if (kind >= 2.5) {
        if (!sceneWaterCompoundShadowUsesSelena(entry)) return null;
        var compoundMaterial = sceneWaterCompoundShadowSelenaMaterial(system, entry);
        var compoundContext = sceneWaterCompoundShadowSelenaRenderContext(system);
        return getWaterSelenaPostDraw(compoundMaterial, compoundContext, system, pipelineOptions);
      }
      if (!sceneWaterObjectShadowUsesSelena(entry)) return null;
      var material = sceneWaterObjectShadowSelenaMaterial(system, entry);
      var renderContext = sceneWaterObjectShadowSelenaRenderContext(system);
      return getWaterSelenaPostDraw(material, renderContext, system, pipelineOptions);
    }

    // --- Object mesh-shadow pass (projected mesh, mesh kind) ----------------
    //
    // Drawn by renderWaterObjectMeshShadowPass/drawWaterObjectProjectedShadowObjects
    // below into system.objectShadowView (WATER_OBJECT_TEXTURE_FORMAT, no
    // depth attachment, cullMode:"none" -- matching the hand-written
    // waterObjectMeshShadowPipeline exactly). Unlike the other water-system
    // passes above, this material/render-context pair is resolved ONCE per
    // system per frame (not per projected object): mvp/normalMatrix are
    // declared but UNUSED by object-mesh-shadow.sel's body (confirmed by
    // TestWaterSelenaWGSLDescriptorMatchesBindings's WGSL parse), and
    // lightDir/poolHalfW/poolHalfL don't vary per object, so every object in
    // the projected-shadow list shares one bind group; only the position
    // vertex buffer changes per object (bindWaterObjectSelenaAttributes,
    // reused from the object-texture RTT draw path).

    function sceneWaterObjectMeshShadowSelenaMaterial(system, entry) {
      var material = sceneWaterSelenaMaterial(system, entry, "objectMeshShadowSelenaWGSL", "objectMeshShadow", "_selenaObjectMeshShadowMaterial");
      if (!material) return null;
      material.customUniforms = {
        poolHalfW: sceneNumber(system && system.waterPoolWidth, 1.5),
        poolHalfL: sceneNumber(system && system.waterPoolLength, 1.5),
      };
      return material;
    }

    function sceneWaterObjectMeshShadowSelenaRenderContext(system) {
      var light = (system && system.waterLightDir) || { x: 0.3, y: 0.9, z: 0.45 };
      return {
        uniformSlotSuffix: "water-object-mesh-shadow-" + String((system && system.id) || "water"),
        uniforms: {
          lightDir: [sceneNumber(light.x, 0.3), sceneNumber(light.y, 0.9), sceneNumber(light.z, 0.45)],
        },
      };
    }

    function sceneWaterObjectMeshShadowUsesSelena(entry) {
      return sceneWaterSelenaUsesPass(entry, "objectMeshShadowSelenaWGSL", "objectMeshShadow");
    }

    function getWaterObjectMeshShadowSelenaDraw(system, entry) {
      var material = sceneWaterObjectMeshShadowSelenaMaterial(system, entry);
      var renderContext = sceneWaterObjectMeshShadowSelenaRenderContext(system);
      return getWaterSelenaMeshDraw(material, renderContext, system, {
        targetFormat: WATER_OBJECT_TEXTURE_FORMAT,
        sampleCount: 1,
        depthStencil: false,
        cullMode: "none",
        labelSuffix: "water-object-mesh-shadow",
      });
    }

    // -----------------------------------------------------------------------
    // The five water simulation COMPUTE kernels (seed/drop/displacement/
    // simulation/normal), routed through the generic descriptor-driven Selena
    // feedback-compute path above (getSelenaComputePipeline /
    // createSelenaComputeBindGroup) instead of the hardcoded
    // waterComputeBindGroupLayout/waterSeedPipeline/.../SCENE_WATER_COMPUTE_SOURCE
    // pipelines below. Each kernel falls back to the builtin hardcoded
    // pipeline (passed in as fallbackPipeline; the hand-written
    // data-prop-authored compute pipeline tier has been retired) when its
    // Selena WGSL+descriptor aren't present on the entry, or the Selena
    // pipeline/bind group failed to
    // build -- mirroring the render passes' sceneWaterXxxUsesSelena/
    // getWaterXxxSelenaDraw fallback convention exactly (see
    // renderWaterCausticsPass above). Dispatch math (ceil(cellCount/64)) and
    // ping-pong (system.activeIndex toggling bufferA/bufferB) are UNCHANGED
    // from the hardcoded path -- only pipeline/bind-group sourcing moves to
    // the descriptor.
    // -----------------------------------------------------------------------

    // GPU-faithful hash mirroring the WebGPU/WebGL seedDrops kernel's
    // hash01(n)=fract(sin(n)*k) (see 16-scene-webgl.js's identical
    // sceneWaterHash01/sceneWaterBuildSeedDrops -- duplicated here rather than
    // shared because bootstrap-feature-scene3d-webgpu.js's build bundles
    // 16a-scene-webgpu.js WITHOUT 16-scene-webgl.js, see build-bootstrap.mjs).
    function sceneWaterSelenaHash01(n) {
      var v = Math.sin(n) * 43758.5453123;
      return v - Math.floor(v);
    }

    // sceneWaterSelenaSeedDropsContextArray builds the seed kernel's
    // `context { drops : array<vec4,64> }` value: per-drop hashed UV centers
    // (host-side, since seed.sel's context contract carries pre-computed drops
    // rather than a loop-local RNG) with the polarity folded into a signed
    // strength component, replicating the hardcoded seedDrops kernel's
    // hash01(jf*12.9898+seedSalt+0.173)/hash01(jf*78.233+seedSalt*1.371+0.719)
    // + select(1,-1,(j&1u)==0u) math exactly (SCENE_WATER_COMPUTE_SOURCE
    // above). `.z` is unused by seed.sel's body (only `.xy`/`.w` are read) but
    // packed as 0 for a well-formed vec4.
    function sceneWaterSelenaSeedDropsContextArray(count, seedSalt, dropStrength) {
      var n = Math.max(0, Math.min(64, Math.floor(count)));
      var out = new Float32Array(64 * 4);
      for (var j = 0; j < n; j++) {
        var jf = j + 1;
        out[j * 4 + 0] = sceneWaterSelenaHash01(jf * 12.9898 + seedSalt + 0.173);
        out[j * 4 + 1] = sceneWaterSelenaHash01(jf * 78.233 + seedSalt * 1.371 + 0.719);
        out[j * 4 + 2] = 0;
        out[j * 4 + 3] = dropStrength * ((j & 1) === 0 ? -1 : 1);
      }
      return out;
    }

    // sceneWaterDisplacementSpheresContextArray packs a plain JS sphere list
    // ({x,y,z,radius}[]) into the flat Float32Array(WATER_MAX_DISPLACEMENT_
    // SPHERES*4) shape the G1 array-uniform packer expects for the
    // displacement kernel's `context { spheres : array<vec4,32> }` field.
    // Deliberately separate from sceneWaterSpheresContextArray (which reads
    // system.waterObjectSpheres): the displacement kernel's per-dispatch
    // spheres come from system._waterComputeObjectState.spheres (see
    // sceneWaterDisplacementSelenaRenderContext below), which stays correct
    // for the one-shot objectDisplacementEvents path where
    // system.waterObjectSpheres is NOT refreshed (sceneWaterObjectState skips
    // all system-field mutation when called with transientObject:true).
    function sceneWaterDisplacementSpheresContextArray(list) {
      var spheres = Array.isArray(list) ? list : [];
      var out = new Float32Array(WATER_MAX_DISPLACEMENT_SPHERES * 4);
      for (var i = 0; i < spheres.length && i < WATER_MAX_DISPLACEMENT_SPHERES; i++) {
        var sphere = spheres[i] || {};
        var offset = i * 4;
        out[offset] = sceneNumber(sphere.x, 0);
        out[offset + 1] = sceneNumber(sphere.y, 0);
        out[offset + 2] = sceneNumber(sphere.z, 0);
        out[offset + 3] = Math.max(0.0001, sceneNumber(sphere.radius, 0));
      }
      return out;
    }

    // sceneWaterComputeStageFields maps a compute stage name to its additive
    // WaterSystem entry slots: wgslField is the entry-level field name (e.g.
    // "seedSelenaWGSL", matching WaterSystemIR's json tag / page.gsx's prop
    // name -- NOT the Go-side WaterDemoData top-level key, which carries a
    // "water" prefix, e.g. data.waterSeedSelenaWGSL, see selena_glsl.go's
    // waterSelenaComputeWGSLData/page.gsx's seedSelenaWGSL={data.waterSeedSelenaWGSL}),
    // descKey matching shaderDescriptors.
    function sceneWaterComputeStageFields(stage) {
      switch (stage) {
      case "seed": return { wgslField: "seedSelenaWGSL", descKey: "seed", memoSlot: "_selenaSeedMaterial" };
      case "drop": return { wgslField: "dropSelenaWGSL", descKey: "drop", memoSlot: "_selenaDropMaterial" };
      case "displacement": return { wgslField: "displacementSelenaWGSL", descKey: "displacement", memoSlot: "_selenaDisplacementMaterial" };
      case "simulation": return { wgslField: "simulationSelenaWGSL", descKey: "simulation", memoSlot: "_selenaSimulationMaterial" };
      case "normal": return { wgslField: "normalSelenaWGSL", descKey: "normal", memoSlot: "_selenaNormalMaterial" };
      default: return null;
      }
    }

    function sceneWaterComputeStageUsesSelena(entry, stage) {
      var fields = sceneWaterComputeStageFields(stage);
      if (!fields) return false;
      return sceneWaterSelenaUsesPass(entry, fields.wgslField, fields.descKey);
    }

    // sceneWaterSeedSelenaRenderContext: seed kernel context = P{dropRadius} +
    // C{dropCount,drops[64]} (design doc 3.5). dropCount/dropStrength/
    // dropRadius derivations mirror sceneWaterUniformData's byte-packed
    // seedDrops/dropStrength/dropRadius clamps exactly (waterUniformScratchU[2],
    // waterUniformScratchF[8]/[9]) so the Selena path reproduces the SAME
    // per-drop values as the hardcoded seedDrops kernel for a given seedSalt.
    function sceneWaterSeedSelenaRenderContext(system, entry) {
      var count = Math.max(0, Math.min(64, Math.floor(sceneNumber(entry && entry.seedDrops, 7))));
      var dropStrength = Math.max(-1, Math.min(1, sceneNumber(entry && entry.dropStrength, 0.01)));
      var dropRadius = Math.max(0.0001, Math.min(0.5, sceneNumber(entry && entry.dropRadius, 0.03)));
      var seedSalt = Math.max(0, sceneNumber(system && system.seedSalt, 0));
      return {
        uniformSlotSuffix: "water-seed-" + String((system && system.id) || "water"),
        uniforms: {
          dropRadius: dropRadius,
          dropCount: count,
          drops: sceneWaterSelenaSeedDropsContextArray(count, seedSalt, dropStrength),
        },
      };
    }

    // sceneWaterDropSelenaRenderContext: drop kernel context = C{dropCenter,
    // dropRadius,dropStrength}, sourced from the SAME entry.dropX/dropZ/
    // dropEventRadius/dropEventStrength fields (with dropRadius/dropStrength
    // fallback) sceneWaterUniformData packs into interactiveDrop.xyzw
    // (waterUniformScratchF[48]-[51]).
    function sceneWaterDropSelenaRenderContext(system, entry) {
      var dropX = Math.max(-1, Math.min(1, sceneNumber(entry && entry.dropX, 0)));
      var dropZ = Math.max(-1, Math.min(1, sceneNumber(entry && entry.dropZ, 0)));
      var dropRadius = Math.max(0.0001, Math.min(0.5, sceneNumber(entry && entry.dropEventRadius, sceneNumber(entry && entry.dropRadius, 0.03))));
      var dropStrength = Math.max(-1, Math.min(1, sceneNumber(entry && entry.dropEventStrength, sceneNumber(entry && entry.dropStrength, 0.01))));
      return {
        uniformSlotSuffix: "water-drop-" + String((system && system.id) || "water"),
        uniforms: {
          dropCenter: [dropX, dropZ],
          dropRadius: dropRadius,
          dropStrength: dropStrength,
        },
      };
    }

    // sceneWaterDisplacementSelenaRenderContext: displacement kernel context =
    // C{objectKind,displacementScale,objectCenter,objectPrevCenter,
    // objectRadius,objectHalfSize,sphereCount,spheres[32]}. Reads
    // system._waterComputeObjectState (stashed by sceneWaterUniformData every
    // call, including transientObject:true event calls) rather than the
    // system.waterObject* fields the render passes use, because those fields
    // are NOT refreshed during a one-shot objectDisplacementEvents dispatch
    // (sceneWaterObjectState skips system mutation when system is null) --
    // the stashed objectState is always the exact object the immediately-
    // preceding sceneWaterUniformData call computed, for both the continuous
    // per-frame dispatch and each individual event dispatch.
    function sceneWaterDisplacementSelenaRenderContext(system) {
      var state = (system && system._waterComputeObjectState) || {};
      var center = state.center || { x: 0, y: 0, z: 0 };
      var previous = state.previous || center;
      var half = state.halfSize || { x: 0, y: 0, z: 0 };
      var spheres = Array.isArray(state.spheres) ? state.spheres : [];
      return {
        uniformSlotSuffix: "water-displacement-" + String((system && system.id) || "water"),
        uniforms: {
          objectKind: sceneNumber(state.kind, 0),
          displacementScale: sceneNumber(state.displacementScale, 0),
          objectCenter: [sceneNumber(center.x, 0), sceneNumber(center.y, 0), sceneNumber(center.z, 0)],
          objectPrevCenter: [sceneNumber(previous.x, 0), sceneNumber(previous.y, 0), sceneNumber(previous.z, 0)],
          objectRadius: Math.max(0.0001, sceneNumber(state.radius, 0.0001)),
          objectHalfSize: [sceneNumber(half.x, 0), sceneNumber(half.y, 0), sceneNumber(half.z, 0)],
          sphereCount: Math.min(WATER_MAX_DISPLACEMENT_SPHERES, spheres.length),
          spheres: sceneWaterDisplacementSpheresContextArray(spheres),
        },
      };
    }

    // sceneWaterSimulationSelenaRenderContext: simulation kernel has no
    // context fields, only params waveSpeed/damping (simulation.sel:5-6) --
    // still supplied via renderContext.uniforms (the packer doesn't
    // distinguish param vs context provenance, see sceneSelenaUniformValue),
    // clamped exactly like the hardcoded path's waterUniformScratchF[6]/[7].
    function sceneWaterSimulationSelenaRenderContext(system, entry) {
      return {
        uniformSlotSuffix: "water-simulation-" + String((system && system.id) || "water"),
        uniforms: {
          waveSpeed: Math.max(0, Math.min(2, sceneNumber(entry && entry.waveSpeed, 1.0))),
          damping: Math.max(0, Math.min(1, sceneNumber(entry && entry.damping, 0.995))),
        },
      };
    }

    // Physical cell spacing is part of the normal contract: height deltas are
    // world-space values, so unit-length X/Z tangents would flatten the water
    // by resolution/(2*poolExtent) and suppress both optics and caustics.
    function sceneWaterNormalSelenaRenderContext(system) {
      var resolution = Math.max(1, sceneNumber(system && system.resolution, 256));
      var poolWidth = Math.max(0.0001, sceneNumber(system && system.waterPoolWidth, 1));
      var poolLength = Math.max(0.0001, sceneNumber(system && system.waterPoolLength, 1));
      return {
        uniformSlotSuffix: "water-normal-" + String((system && system.id) || "water"),
        uniforms: {
          cellSizeX: 2 * poolWidth / resolution,
          cellSizeZ: 2 * poolLength / resolution,
        },
      };
    }

    function sceneWaterComputeStageRenderContext(system, entry, stage) {
      switch (stage) {
      case "seed": return sceneWaterSeedSelenaRenderContext(system, entry);
      case "drop": return sceneWaterDropSelenaRenderContext(system, entry);
      case "displacement": return sceneWaterDisplacementSelenaRenderContext(system);
      case "simulation": return sceneWaterSimulationSelenaRenderContext(system, entry);
      case "normal": return sceneWaterNormalSelenaRenderContext(system);
      default: return null;
      }
    }

    // getWaterSelenaComputeDraw resolves the {pipeline, bindGroup} pair for a
    // feedback-kind Selena water compute kernel, or null if the pipeline/bind
    // group could not be built (caller falls back to the resolved authored-or-
    // hardcoded pipeline). Mirrors getWaterSelenaMeshDraw/getWaterSelenaPostDraw.
    function getWaterSelenaComputeDraw(material, renderContext, system, inBuf, outBuf) {
      if (!material) return null;
      var selenaResource = getSelenaComputePipeline(material);
      if (!selenaResource) return null;
      var bindGroup = createSelenaComputeBindGroup(system, selenaResource, inBuf, outBuf, renderContext);
      if (!bindGroup) return null;
      return { pipeline: selenaResource.pipeline, bindGroup: bindGroup };
    }

    function getWaterComputeStageSelenaDraw(system, entry, stage, readBuffer, writeBuffer) {
      var fields = sceneWaterComputeStageFields(stage);
      if (!fields) return null;
      var material = sceneWaterSelenaMaterial(system, entry, fields.wgslField, fields.descKey, fields.memoSlot);
      if (!material) return null;
      var renderContext = sceneWaterComputeStageRenderContext(system, entry, stage);
      return getWaterSelenaComputeDraw(material, renderContext, system, readBuffer, writeBuffer);
    }

    // dispatchWaterPassSelena mirrors dispatchWaterPass's dispatch math and
    // ping-pong swap exactly (Math.ceil(system.cellCount/64) workgroups,
    // system.activeIndex toggling 0<->1 after the pass), but binds an
    // explicit {pipeline,bindGroup} pair (built fresh per kernel per frame from
    // the live renderContext) instead of the hardcoded
    // system.computeBindGroups[system.activeIndex] pair. sharedPass mirrors
    // dispatchWaterPass's sharedPass parameter -- see its comment.
    function dispatchWaterPassSelena(encoder, system, draw, sharedPass) {
      if (!system || !draw || !draw.pipeline || !draw.bindGroup) return 0;
      var pass = sharedPass || (encoder && encoder.beginComputePass({ label: "gosx-water-selena-compute-pass" }));
      if (!pass) return 0;
      pass.setPipeline(draw.pipeline);
      pass.setBindGroup(0, draw.bindGroup);
      pass.dispatchWorkgroups(Math.ceil(system.cellCount / 64));
      if (!sharedPass) pass.end();
      system.activeIndex = system.activeIndex === 0 ? 1 : 0;
      return 1;
    }

    // dispatchWaterComputeStage is the single per-stage entry point every
    // seed/drop/displacement/simulation/normal call site below uses: tries the
    // descriptor-driven Selena compute path first, falling back to
    // fallbackPipeline (the builtin hardcoded compute pipeline; the
    // hand-written data-prop-authored compute pipeline tier has been
    // retired) when Selena's WGSL+descriptor aren't present on the entry, or
    // the Selena pipeline/bind group failed to
    // build. Returns {dispatches, selena, selenaFallback} so call sites can
    // fold all three into the existing waterComputeDispatches/
    // waterAuthoredComputeDispatches stats plus the new
    // waterSelenaComputeDispatches/waterSelenaComputeFallbacks counters.
    // sharedPass (optional) mirrors dispatchWaterPass/dispatchWaterPassSelena's
    // parameter of the same name: when set, this stage's dispatch is recorded
    // into that already-open compute pass instead of opening/closing its own.
    // The P3 water-sim/normal fusion (see the runWaterSim block below) is the
    // sole caller that passes sharedPass, batching the tick's 2 simulation
    // substeps plus the trailing normal reconstruction into one pass.
    function dispatchWaterComputeStage(encoder, system, entry, stage, fallbackPipeline, sharedPass) {
      if (!encoder && !sharedPass) return { dispatches: 0, selena: 0, selenaFallback: 0 };
      if (!system) return { dispatches: 0, selena: 0, selenaFallback: 0 };
      if (sceneWaterComputeStageUsesSelena(entry, stage)) {
        var readBuffer = system.activeIndex === 0 ? system.bufferA : system.bufferB;
        var writeBuffer = system.activeIndex === 0 ? system.bufferB : system.bufferA;
        var draw = getWaterComputeStageSelenaDraw(system, entry, stage, readBuffer, writeBuffer);
        if (draw) {
          var n = dispatchWaterPassSelena(encoder, system, draw, sharedPass);
          return { dispatches: n, selena: n, selenaFallback: 0 };
        }
        return { dispatches: dispatchWaterPass(encoder, system, fallbackPipeline, sharedPass), selena: 0, selenaFallback: 1 };
      }
      return { dispatches: dispatchWaterPass(encoder, system, fallbackPipeline, sharedPass), selena: 0, selenaFallback: 0 };
    }

    // Selena render materials lower stateAt(uv) to textureLoad so vertex and
    // fragment reads share the texture cache. The simulation itself remains a
    // storage-buffer compute pipeline. Refresh the sampled mirror after the
    // final normal pass, in the same command encoder, so all following
    // caustic/pool/surface passes observe the exact active ping-pong state.
    function syncWaterSampledState(encoder, system) {
      if (!encoder || typeof encoder.copyBufferToTexture !== "function" || !system) return 0;
      var resolution = Math.max(16, Math.floor(sceneNumber(system.resolution, 16)));
      var source = system.activeIndex === 0 ? system.bufferA : system.bufferB;
      var destination = system.activeIndex === 0 ? system.stateTextureA : system.stateTextureB;
      if (!source || !destination) return 0;
      encoder.copyBufferToTexture(
        { buffer: source, bytesPerRow: resolution * 16, rowsPerImage: resolution },
        { texture: destination },
        [resolution, resolution, 1]
      );
      system.stateTextureSyncSeq = Math.max(0, Math.floor(sceneNumber(system.stateTextureSyncSeq, 0))) + 1;
      return 1;
    }

    // getWaterPoolPipeline: builtin-only now. The hand-written
    // data-prop-authored pool pipeline tier (entry.poolVertexWGSL/
    // poolFragmentWGSL) has been retired -- the pool pass resolves
    // Selena-primary (see sceneWaterPoolUsesSelena/getWaterPoolSelenaDraw in
    // drawWaterPoolEntries below) falling through to this builtin
    // waterPoolVertexModule/waterPoolFragmentModule
    // (SCENE_WATER_POOL_*_SOURCE) pipeline as the last-resort safety net.
    // system/forceBuiltin are accepted (unused) to keep existing call sites
    // stable.
    function getWaterPoolPipeline(system, forceBuiltin) {
      var sampleCount = Math.max(1, Math.floor(activeSampleCount || 1));
      var cacheKey = ["pool", targetFormat, sampleCount].join("\x00");
      var cached = waterPoolPipelineCache[cacheKey];
      if (cached) return cached;
      var record = {
        pipeline: device.createRenderPipeline({
          label: "gosx-water-pool-pass",
          layout: waterPoolPipelineLayout,
          vertex: { module: waterPoolVertexModule, entryPoint: "vertexMain", buffers: [] },
          fragment: {
            module: waterPoolFragmentModule,
            entryPoint: "fragmentMain",
            targets: [{ format: targetFormat }],
          },
          primitive: { topology: "triangle-list", cullMode: "back" },
          multisample: { count: sampleCount },
          depthStencil: {
            format: "depth24plus",
            depthWriteEnabled: true,
            depthCompare: "less-equal",
          },
        }),
        authored: false,
        authoredVertex: false,
        authoredFragment: false,
        failed: false,
      };
      waterPoolPipelineCache[cacheKey] = record;
      return record;
    }

    function drawWaterPoolEntries(renderPass, records, frameBindGroup) {
      var roundedPoolVertexCount = 44 * 9;
      var stats = {
        waterPoolPasses: 0,
        waterPoolDrawCalls: 0,
        waterPoolDrawVertices: 0,
        waterPoolTileTextureLoaded: 0,
        waterPoolTileTextureFallbacks: 0,
        waterPoolTileTexturePending: 0,
        waterPoolTileTextureFailed: 0,
        waterAuthoredPoolPasses: 0,
        waterAuthoredPoolVertexPasses: 0,
        waterAuthoredPoolFragmentPasses: 0,
        waterAuthoredPoolFallbacks: 0,
        // Proof-of-concept: pool pass routed through the generic
        // descriptor-driven Selena WebGPU render path instead of the
        // hand-written waterPool*WGSL pipeline. See sceneWaterPoolUsesSelena /
        // getWaterPoolSelenaDraw above. Every other pass is unaffected.
        waterSelenaPoolPasses: 0,
        waterSelenaPoolFallbacks: 0,
      };
      if (!renderPass || !Array.isArray(records) || records.length === 0 || !frameBindGroup) return stats;
      renderPass.setBindGroup(0, frameBindGroup);
      var frameGroupBound = true;
      var activePipeline = null;
      for (var i = 0; i < records.length; i++) {
        var system = records[i] && records[i].system;
        if (!system) continue;
        var entry = system.entry || {};
        if (entry.renderPool === false || entry.poolPass === false) continue;
        var rounded = sceneWaterPoolShapeRounded(entry) && sceneNumber(entry.cornerRadius, 0) > 0.0001;

        if (sceneWaterPoolUsesSelena(entry, rounded)) {
          var selenaDraw = getWaterPoolSelenaDraw(system, entry);
          if (selenaDraw) {
            if (selenaDraw.pipeline !== activePipeline) {
              renderPass.setPipeline(selenaDraw.pipeline);
              activePipeline = selenaDraw.pipeline;
            }
            // The generic Selena pipeline layout has exactly ONE bind group
            // (group 0 carries mvp/params/textures/state together -- see
            // sceneSelenaBindGroupLayout), unlike the hand-written pool
            // pipeline's two groups (0=frame, 1=material). Overwriting group
            // 0 here is why frameGroupBound is tracked and restored below
            // before any subsequent hand-written-path draw in this loop.
            renderPass.setBindGroup(0, selenaDraw.bindGroup);
            frameGroupBound = false;
            // pool.sel now ports the rounded-corner geometry too, exactly
            // like the hand-written path's `vertexCount` below: the box
            // shape only ever needs 30 vertices, but the rounded vertex()
            // branch indexes up to roundedPoolVertexCount (44*9 = 396), so a
            // rounded pass must draw that many for every index the shader
            // branches on to actually be issued.
            var selenaVertexCount = rounded ? roundedPoolVertexCount : 30;
            renderPass.draw(selenaVertexCount);
            stats.waterPoolPasses += 1;
            stats.waterPoolDrawCalls += 1;
            stats.waterPoolDrawVertices += selenaVertexCount;
            stats.waterSelenaPoolPasses += 1;
            if (system.waterPoolTileLoaded) {
              stats.waterPoolTileTextureLoaded += 1;
            } else if (system.waterPoolTileRequested) {
              stats.waterPoolTileTextureFallbacks += 1;
              if (system.waterPoolTilePending) stats.waterPoolTileTexturePending += 1;
              if (system.waterPoolTileFailed) stats.waterPoolTileTextureFailed += 1;
            }
            continue;
          }
          stats.waterSelenaPoolFallbacks += 1;
          // Fall through to the hand-written path below (e.g. Selena pipeline
          // validation failed, or a live resource wasn't ready yet).
        }

        if (!frameGroupBound) {
          renderPass.setBindGroup(0, frameBindGroup);
          frameGroupBound = true;
          activePipeline = null;
        }
        var pipelineRecord = getWaterPoolPipeline(system);
        if (!pipelineRecord || !pipelineRecord.pipeline) continue;
        if (pipelineRecord.pipeline !== activePipeline) {
          renderPass.setPipeline(pipelineRecord.pipeline);
          activePipeline = pipelineRecord.pipeline;
        }
        var vertexCount = rounded ? roundedPoolVertexCount : 30;
        var bindGroup = getWaterPoolBindGroupCached(system);
        if (!bindGroup) continue;
        renderPass.setBindGroup(1, bindGroup);
        renderPass.draw(vertexCount);
        stats.waterPoolPasses += 1;
        stats.waterPoolDrawCalls += 1;
        stats.waterPoolDrawVertices += vertexCount;
        if (pipelineRecord.authored) stats.waterAuthoredPoolPasses += 1;
        if (pipelineRecord.authoredVertex) stats.waterAuthoredPoolVertexPasses += 1;
        if (pipelineRecord.authoredFragment) stats.waterAuthoredPoolFragmentPasses += 1;
        if (pipelineRecord.failed) stats.waterAuthoredPoolFallbacks += 1;
        if (system.waterPoolTileLoaded) {
          stats.waterPoolTileTextureLoaded += 1;
        } else if (system.waterPoolTileRequested) {
          stats.waterPoolTileTextureFallbacks += 1;
          if (system.waterPoolTilePending) stats.waterPoolTileTexturePending += 1;
          if (system.waterPoolTileFailed) stats.waterPoolTileTextureFailed += 1;
        }
      }
      return stats;
    }

    // getWaterRenderPipeline: builtin-only now. The hand-written
    // data-prop-authored surface pipeline tier (entry.surfaceVertexWGSL/
    // surfaceFragmentWGSL/surfaceBelowFragmentWGSL) has been retired -- each
    // surface side resolves Selena-primary (see sceneWaterSurfaceUsesSelena/
    // sceneWaterSurfaceBelowUsesSelena/getWaterSurfaceSelenaDraw/
    // getWaterSurfaceBelowSelenaDraw in drawWaterSurfaceSide below) falling
    // through to this builtin waterRenderVertexModule/waterRenderFragmentModule/
    // waterRenderBelowFragmentModule (SCENE_WATER_RENDER_*_SOURCE) pipeline as
    // the last-resort safety net. system/forceBuiltin are accepted (unused) to
    // keep existing call sites stable.
    function getWaterRenderPipeline(system, surfaceSide, forceBuiltin) {
      var sampleCount = Math.max(1, Math.floor(activeSampleCount || 1));
      var side = surfaceSide === "below" ? "below" : "above";
      var cacheKey = [side, "alpha", sampleCount, targetFormat].join("\x00");
      var cached = waterRenderPipelineCache.get(cacheKey);
      if (cached && cached.pipeline) return cached;
      var vertexModule = waterRenderVertexModule;
      var fragmentModule = side === "below" ? waterRenderBelowFragmentModule : waterRenderFragmentModule;
      var descriptor = {
        label: side === "below" ? "gosx-water-render-below" : "gosx-water-render-above",
        layout: waterRenderPipelineLayout,
        vertex: { module: vertexModule, entryPoint: "vertexMain", buffers: [] },
        fragment: {
          module: fragmentModule,
          entryPoint: "fragmentMain",
          targets: [{ format: targetFormat, blend: wgpuBlendState("alpha") }],
        },
        primitive: { topology: "triangle-list", cullMode: side === "below" ? "back" : "front" },
        multisample: { count: sampleCount },
        depthStencil: {
          format: "depth24plus",
          depthWriteEnabled: true,
          depthCompare: "less-equal",
        },
      };
      var pipeline = device.createRenderPipeline(descriptor);
      var record = { pipeline: pipeline, authored: false, authoredVertex: false, failed: false, pending: false };
      waterRenderPipelineCache.set(cacheKey, record);
      return record;
    }

    function drawWaterSurfaceSide(renderPass, records, frameBindGroup, side, stats, camera) {
      renderPass.setBindGroup(0, frameBindGroup);
      // frameGroupBound tracks whether group 0 currently holds frameBindGroup
      // or a Selena material bind group (the generic Selena pipeline layout
      // has exactly ONE bind group, unlike the hand-written surface pipeline's
      // two groups: 0=frame, 1=material -- see drawWaterPoolEntries' identical
      // pattern/comment).
      var frameGroupBound = true;
      var activePipeline = null;
      for (var i = 0; i < records.length; i++) {
        var system = records[i] && records[i].system;
        if (!system || system.vertexCount <= 0) continue;
        var entry = system.entry || {};

        var usesSelena = side === "below" ? sceneWaterSurfaceBelowUsesSelena(entry) : sceneWaterSurfaceUsesSelena(entry);
        if (usesSelena) {
          var selenaDraw = side === "below"
            ? getWaterSurfaceBelowSelenaDraw(system, entry, camera)
            : getWaterSurfaceSelenaDraw(system, entry, camera);
          if (selenaDraw) {
            writeWaterObjectTextureMatrices(system);
            if (selenaDraw.pipeline !== activePipeline) {
              renderPass.setPipeline(selenaDraw.pipeline);
              activePipeline = selenaDraw.pipeline;
            }
            renderPass.setBindGroup(0, selenaDraw.bindGroup);
            frameGroupBound = false;
            renderPass.draw(system.vertexCount);
            stats.waterDrawCalls += 1;
            stats.waterDrawVertices += system.vertexCount;
            stats.waterSurfaceMeshResolution = system.surfaceMeshResolution;
            stats.waterSelenaSurfacePasses += 1;
            if (system.waterSkyCubeLoaded) {
              stats.waterSkyCubeTextureLoaded += 1;
            } else if (system.waterSkyCubeRequested) {
              stats.waterSkyCubeTextureFallbacks += 1;
              if (system.waterSkyCubePending) stats.waterSkyCubeTexturePending += 1;
              if (system.waterSkyCubeFailed) stats.waterSkyCubeTextureFailed += 1;
            }
            if (side === "below") {
              stats.waterSurfaceBelowDrawCalls += 1;
              stats.waterSurfaceBelowDrawVertices += system.vertexCount;
            } else {
              stats.waterSurfaceAboveDrawCalls += 1;
              stats.waterSurfaceAboveDrawVertices += system.vertexCount;
            }
            continue;
          }
          stats.waterSelenaSurfaceFallbacks += 1;
          // Fall through to the hand-written path below.
        }

        if (!frameGroupBound) {
          renderPass.setBindGroup(0, frameBindGroup);
          frameGroupBound = true;
          activePipeline = null;
        }
        var pipelineRecord = getWaterRenderPipeline(system, side);
        if (!pipelineRecord || !pipelineRecord.pipeline) {
          if (pipelineRecord && pipelineRecord.pending) {
            stats.waterAuthoredSurfacePendingDrawCalls += 1;
          }
          if (pipelineRecord && pipelineRecord.failed) {
            stats.waterAuthoredSurfaceFallbacks += 1;
            stats.waterAuthoredSurfaceFallbackReason = waterAuthoredSurfacePipelineLastError;
          }
          pipelineRecord = getWaterRenderPipeline(null, side, true);
        }
        if (!pipelineRecord || !pipelineRecord.pipeline) continue;
        writeWaterObjectTextureMatrices(system);
        if (pipelineRecord.pipeline !== activePipeline) {
          renderPass.setPipeline(pipelineRecord.pipeline);
          activePipeline = pipelineRecord.pipeline;
        }
        if (pipelineRecord.authored) {
          stats.waterAuthoredSurfaceSystems += 1;
          stats.waterAuthoredSurfaceDrawCalls += 1;
        }
        if (pipelineRecord.authoredVertex) {
          stats.waterAuthoredSurfaceVertexDrawCalls += 1;
        }
        if (pipelineRecord.failed) {
          stats.waterAuthoredSurfaceFallbacks += 1;
          stats.waterAuthoredSurfaceFallbackReason = waterAuthoredSurfacePipelineLastError;
        }
        var bindGroup = getWaterRenderBindGroupCached(system);
        renderPass.setBindGroup(1, bindGroup);
        renderPass.draw(system.vertexCount);
        stats.waterDrawCalls += 1;
        stats.waterDrawVertices += system.vertexCount;
        if (system.waterSkyCubeLoaded) {
          stats.waterSkyCubeTextureLoaded += 1;
        } else if (system.waterSkyCubeRequested) {
          stats.waterSkyCubeTextureFallbacks += 1;
          if (system.waterSkyCubePending) stats.waterSkyCubeTexturePending += 1;
          if (system.waterSkyCubeFailed) stats.waterSkyCubeTextureFailed += 1;
        }
        if (side === "below") {
          stats.waterSurfaceBelowDrawCalls += 1;
          stats.waterSurfaceBelowDrawVertices += system.vertexCount;
        } else {
          stats.waterSurfaceAboveDrawCalls += 1;
          stats.waterSurfaceAboveDrawVertices += system.vertexCount;
        }
      }
    }

    function drawWaterSystemEntries(renderPass, records, frameBindGroup, camera) {
      var stats = {
        waterDrawCalls: 0,
        waterDrawEntries: 0,
        waterDrawVertices: 0,
        waterSurfaceMeshResolution: 0,
        waterSurfaceAboveDrawCalls: 0,
        waterSurfaceAboveDrawVertices: 0,
        waterSurfaceBelowDrawCalls: 0,
        waterSurfaceBelowDrawVertices: 0,
        waterAuthoredSurfaceSystems: 0,
        waterAuthoredSurfaceDrawCalls: 0,
        waterAuthoredSurfaceVertexDrawCalls: 0,
        waterAuthoredSurfacePendingDrawCalls: 0,
        waterAuthoredSurfaceFallbacks: 0,
        waterAuthoredSurfaceFallbackReason: "",
        waterSkyCubeTextureLoaded: 0,
        waterSkyCubeTextureFallbacks: 0,
        waterSkyCubeTexturePending: 0,
        waterSkyCubeTextureFailed: 0,
        // Surface pass routed through the generic descriptor-driven Selena
        // WebGPU render path. See sceneWaterSurfaceUsesSelena/
        // getWaterSurfaceSelenaDraw above.
        waterSelenaSurfacePasses: 0,
        waterSelenaSurfaceFallbacks: 0,
      };
      if (!Array.isArray(records) || records.length === 0) return stats;
      for (var i = 0; i < records.length; i++) {
        var system = records[i] && records[i].system;
        if (system && system.vertexCount > 0) stats.waterDrawEntries += 1;
      }
      // A heightfield has no overhangs: from a camera clearly above or below
      // its zero plane only one face can contribute. Previously both sides
      // submitted the full tessellated grid and relied on face culling after
      // vertex processing. That doubled the water vertex load precisely when
      // compound objects (for example a glTF duck) also needed optical work.
      // Keep both sides only in the narrow surface crossing band.
      var waterCamera = sceneRenderCamera(camera);
      var cameraY = sceneNumber(waterCamera && waterCamera.y, 0);
      if (cameraY >= -0.025) drawWaterSurfaceSide(renderPass, records, frameBindGroup, "above", stats, camera);
      if (cameraY <= 0.025) drawWaterSurfaceSide(renderPass, records, frameBindGroup, "below", stats, camera);
      return stats;
    }

    // updateInstancedCullSystems dispatches GPU frustum-cull compute passes for
    // all InstancedMeshes that carry a cullKernelWGSL + the gpu-cull capability.
    // Called once per frame, AFTER uploadFrameUniforms (so scratchSelenaViewProjection
    // is ready), BEFORE the shadow pass and main render pass. Mirrors
    // updateComputeParticleSystems shape.
    //
    // vp is the current frame's scratchSelenaViewProjection (post-depth-remap,
    // WebGPU [0,1] clip convention — see extractFrustumPlanesJS comment above).
    //
    // Returns a Map: meshId → { system, isReady }.
    function updateInstancedCullSystems(instancedMeshes, encoder, vp) {
      if (!Array.isArray(instancedMeshes) || instancedMeshes.length === 0) {
        return instancedCullSystems;
      }
      // Check if gpu-cull capability is active. The webgpu chunk is only loaded
      // when webgpu is active, so we can check the API's capabilities JSON or
      // simply gate on the feature we know is set to true in
      // 16a-scene-webgpu.capabilities.json. We guard on createSceneInstancedCullSystem
      // being available (exported by 16b into __gosx_scene3d_api).
      var cullApi = (typeof window !== "undefined" && window.__gosx_scene3d_api)
        ? window.__gosx_scene3d_api
        : null;
      var createCullFn = cullApi && typeof cullApi.createSceneInstancedCullSystem === "function"
        ? cullApi.createSceneInstancedCullSystem
        : null;
      var sigFn = cullApi && typeof cullApi.cullSystemSignature === "function"
        ? cullApi.cullSystemSignature
        : null;

      var planes = extractFrustumPlanesJS(vp);
      var activeIds = new Set();
      var fingerprintFn = cullApi && typeof cullApi.sceneInstanceTransformFingerprint === "function"
        ? cullApi.sceneInstanceTransformFingerprint
        : null;
      var maxScaleFn = cullApi && typeof cullApi.sceneInstancedMaxTransformScale === "function"
        ? cullApi.sceneInstancedMaxTransformScale
        : null;

      for (var i = 0; i < instancedMeshes.length; i++) {
        var mesh = instancedMeshes[i];
        if (!mesh) continue;
        var wgsl = (typeof mesh.cullKernelWGSL === "string" && mesh.cullKernelWGSL.trim()) ? mesh.cullKernelWGSL.trim() : null;
        // A mesh without an authored kernel still culls on the GPU when the
        // renderer's own kernel applies. webGPUBuiltinCullEligible states the
        // three conditions.
        if (!wgsl && !webGPUBuiltinCullEligible(mesh)) continue;
        if (!createCullFn) continue;

        var meshId = (typeof mesh.id === "string" && mesh.id) ? mesh.id : ("mesh-" + i);
        activeIds.add(meshId);

        // Recreate system when kernel or capacity changes.
        var sig = sigFn ? sigFn(mesh) : "";
        var existing = instancedCullSystems.get(meshId);
        if (!existing || existing.signature !== sig) {
          if (existing && existing.system && typeof existing.system.dispose === "function") {
            existing.system.dispose();
          }
          var newSystem = createCullFn(device, mesh, { fallbackRadius: webGPUInstancedCullRadius(mesh) });
          instancedCullSystems.set(meshId, { system: newSystem, signature: sig });
          existing = instancedCullSystems.get(meshId);
        }

        if (!existing || !existing.system) continue;
        var sys = existing.system;
        if (!sys.isReady()) continue;

        // Build instance records for this mesh. The native InstanceRecord is
        // 80B: mat4 (col-major, 64B) + pickData uint32x4 (16B), zero pickData
        // for now (S6 consumer will supply real pickData). The records depend
        // ONLY on the (static) transforms, so build + upload them ONCE per
        // system + transforms array — rebuilding the 80B buffer and re-uploading
        // it to the GPU every frame is pure waste (≈450KB/frame of allocations →
        // GC churn → frame hitches) for a static instanced ring. After the first
        // upload we pass null so sys.update skips the input-buffer write and only
        // refreshes the frustum-plane uniform + dispatches.
        // Instanced meshes serialize their count under `count` (legacyProps);
        // `instanceCount` is often absent. instancedMeshCount() resolves
        // instanceCount→count→0, so the cull operates on the REAL count instead
        // of 0 (which left the input buffer unpopulated → drawIndirect rendered
        // only degenerate zero-matrix instances → an invisible ring).
        // The identity check below catches a scene that reuses one transforms
        // array. A scene core that rebuilds the array every frame defeats it,
        // so the fingerprint catches that case too: equal contents skip the
        // 80-byte-per-instance encode AND the queue.writeBuffer. A static
        // 10 000-instance mesh re-uploaded 800 KB per frame before this.
        var instanceCount = instancedMeshCount(mesh);
        var transforms = mesh.transforms;
        var fingerprint = fingerprintFn && transforms && instanceCount > 0
          ? fingerprintFn(transforms, instanceCount)
          : null;
        var records = null;
        // The fingerprint is authoritative when available. It also fixes the
        // opposite defect: a scene that mutates one transforms array IN PLACE
        // kept the array identity, so the identity check alone never re-uploaded
        // and the GPU culled against stale transforms.
        var contentsChanged = fingerprint !== null
          ? existing.uploadedFingerprint !== fingerprint
          : existing.uploadedTransforms !== transforms;
        if (transforms && instanceCount > 0 && contentsChanged) {
          var tf = (transforms instanceof Float32Array) ? transforms : new Float32Array(transforms);
          var recF = existing.recordScratch && existing.recordScratch.length === instanceCount * 20
            ? existing.recordScratch
            : new Float32Array(instanceCount * 20); // 20 f32 slots = 80B per record; zero-init covers pickData
          existing.recordScratch = recF;
          for (var j = 0; j < instanceCount; j++) {
            var src = j * 16;
            var dst = j * 20;
            for (var k = 0; k < 16; k++) recF[dst + k] = (src + k < tf.length) ? tf[src + k] : 0;
          }
          records = recF;
          existing.uploadedTransforms = transforms;
        }
        if (contentsChanged) {
          existing.uploadedFingerprint = fingerprint;
          // maxInstanceScale only matters for an authored kernel, and it costs a
          // pass over the transforms, so recompute it only when they change.
          existing.maxInstanceScale = maxScaleFn && transforms
            ? maxScaleFn(transforms, instanceCount)
            : 1;
        }

        // Get geometry vertex count for the drawArgs reset.
        var geom = getInstancedGeometry(mesh);
        var vertexCount = (geom && geom.vertexCount > 0) ? geom.vertexCount : 1;

        sys.update(device, encoder, planes, vertexCount, records, instanceCount, {
          transformFingerprint: fingerprint,
          maxInstanceScale: existing.maxInstanceScale,
        });
      }

      // GC: dispose systems for meshes no longer in the bundle.
      for (var _it = instancedCullSystems.entries(), _entry = _it.next(); !_entry.done; _entry = _it.next()) {
        var _id   = _entry.value[0];
        var _rec  = _entry.value[1];
        if (!activeIds.has(_id)) {
          if (_rec && _rec.system && typeof _rec.system.dispose === "function") {
            _rec.system.dispose();
          }
          instancedCullSystems.delete(_id);
        }
      }

      // Cull telemetry readback — gated on window.__gosx_scene3d_cull_telemetry === true.
      // Throttled to ~every 30 frames to avoid GPU readback stalls every frame.
      // Poll BEFORE requesting the next readback: mapAsync reads data from the
      // PREVIOUS cycle's copy (already submitted); calling requestSurvivorReadback
      // first would encode a new copy into the still-open encoder, causing mapAsync
      // to race against an unsubmitted write and read stale zeros.
      var cullTelemetryOn = (typeof window !== "undefined" && window.__gosx_scene3d_cull_telemetry === true);
      if (cullTelemetryOn) {
        cullTelemetryFrameCount += 1;
        // Step 1: poll — reads survivor count from previous cycle's submitted copy.
        var survivorSnapshot = {};
        for (var _pi = instancedCullSystems.entries(), _pe = _pi.next(); !_pe.done; _pe = _pi.next()) {
          var _pId  = _pe.value[0];
          var _pRec = _pe.value[1];
          if (_pRec && _pRec.system && _pRec.system.isReady()) {
            if (typeof _pRec.system.pollSurvivors === "function") {
              _pRec.system.pollSurvivors();
            }
            survivorSnapshot[_pId] = {
              instanceCount: _pRec.system.instanceCount || 0,
              survivors: _pRec.system.lastSurvivors,
            };
          }
        }
        lastCullSurvivors = JSON.stringify(survivorSnapshot);
        // Step 2: request next readback after polling (copy encoded into current encoder).
        if (cullTelemetryFrameCount >= 30) {
          cullTelemetryFrameCount = 0;
          for (var _ti = instancedCullSystems.entries(), _te = _ti.next(); !_te.done; _te = _ti.next()) {
            var _tRec = _te.value[1];
            if (_tRec && _tRec.system && _tRec.system.isReady() && typeof _tRec.system.requestSurvivorReadback === "function") {
              _tRec.system.requestSurvivorReadback(encoder);
            }
          }
        }
      } else {
        lastCullSurvivors = null;
      }

      return instancedCullSystems;
    }

    // -----------------------------------------------------------------------
    // Uniform upload helpers
    // -----------------------------------------------------------------------

    function sceneWebGPUToneMapMode(environment, usePostProcessing) {
      if (usePostProcessing) return 4;
      var mode = environment && typeof environment.toneMapping === "string"
        ? environment.toneMapping.trim().toLowerCase()
        : "";
      if (mode === "linear" || mode === "none") return 0;
      if (mode === "reinhard") return 2;
      if (mode === "filmic") return 3;
      return 1;
    }

    function uploadFrameUniforms(camera, width, height, toneMapMode) {
      var cam;
      var camPosZ;
      if (camera && camera.mode === "ortho2d") {
        // 2D board path — mirrors the Mode branch at the top of the native
        // computeMVP (render/bundle/math.go). The RAW RenderCamera wire
        // fields are read directly: x/y carry the pan, z carries the zoom,
        // near/far are -1/1. This MUST run before sceneRenderCamera — the
        // normalizer strips `mode`, applies 3D defaults (z→6, near→0.05,
        // far→128), and treats z as a position, which would silently
        // mangle the 2D camera.
        sceneMat4Ortho2DView(camera, scratchViewMatrix);
        sceneMat4Ortho2DProj(camera, width, height, scratchProjMatrix);
        // Returned cam: render()'s only downstream consumer of the return
        // value is drawPointsEntries, which never reads its cam parameter
        // (and 2D bundles carry no points — Configure2DBundle strips
        // lights/env/postFX and the board adapter emits only
        // meshObjects/materials/background). z is 0 because camera.z
        // carries the zoom, not a position — the cameraPos uniform below
        // must not inherit it; with no lights in 2D bundles cameraPos is
        // inert anyway.
        cam = {
          mode: "ortho2d",
          x: sceneNumber(camera.x, 0),
          y: sceneNumber(camera.y, 0),
          z: 0,
        };
        camPosZ = 0;
      } else {
        cam = sceneRenderCamera(camera);
        var aspect = Math.max(0.0001, width / Math.max(1, height));
        scenePBRViewMatrix(cam, scratchViewMatrix);
        if (typeof scenePBRProjectionMatrixForCamera === "function") {
          scenePBRProjectionMatrixForCamera(cam, aspect, scratchProjMatrix);
        } else {
          scenePBRProjectionMatrix(cam.fov, aspect, cam.near, cam.far, scratchProjMatrix);
        }
        camPosZ = cam.z; // cameraPos.z is the world-space eye position.
      }

      // scenePBRProjectionMatrix and sceneMat4Ortho2DProj produce a
      // WebGL-convention matrix whose clip-z range is [-w, w]. WebGPU's
      // clip volume keeps z in [0, w], so without this remap every
      // primitive in the front half of the frustum is silently clipped.
      // Pre-multiplying by the depth-remap matrix R (row 2 = 0.5 *
      // (row 2 + row 3)) converts to WebGPU clip space. Affects every
      // WebGPU pipeline that consumes frame.projMatrix (PBR meshes, world
      // lines, surfaces, points, compute particles). For the ortho-2D
      // board (near=-1, far=1) the board plane z=0 lands at clip z=0.5.
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
      var f = _frameUniformF;
      var u = _frameUniformU;
      f.set(scratchViewMatrix, 0);          // offset 0
      f.set(scratchProjMatrix, 16);         // offset 64
      f[32] = cam.x;                         // cameraPos.x
      f[33] = cam.y;                         // cameraPos.y
      f[34] = camPosZ;                       // cameraPos.z (3D: world eye; ortho2d: 0 — z carries zoom)
      // lightCount set below in uploadLights
      f[36] = width;                          // viewportWidth
      f[37] = height;                         // viewportHeight
      u[38] = Math.max(0, Math.min(4, Math.floor(sceneNumber(toneMapMode, 1)))); // toneMap/output mode
      u[39] = 0;                              // pad

      device.queue.writeBuffer(frameUniformBuffer, 0, f);
      return cam;
    }

    function uploadWaterReflectionFrameUniforms(camera, width, height, toneMap) {
      if (camera && camera.mode === "ortho2d") {
        return uploadFrameUniforms(camera, width, height, toneMap);
      }
      var cam = sceneRenderCamera(camera);
      var aspect = Math.max(0.0001, width / Math.max(1, height));
      var position = sceneWaterCameraWorldPosition(cam);
      var direction = sceneWaterCameraWorldDirection(cam);
      var target = {
        x: position.x + direction.x,
        y: position.y + direction.y,
        z: position.z + direction.z,
      };
      var eye = sceneWaterMirrorWaterPoint(position);
      var reflectedTarget = sceneWaterMirrorWaterPoint(target);
      var reflectedUp = sceneWaterReflectionCameraUp(camera);
      sceneWaterLookAtViewMatrix(eye, reflectedTarget, reflectedUp, scratchViewMatrix);
      if (typeof scenePBRProjectionMatrixForCamera === "function") {
        scenePBRProjectionMatrixForCamera(cam, aspect, scratchProjMatrix);
      } else {
        scenePBRProjectionMatrix(cam.fov, aspect, cam.near, cam.far, scratchProjMatrix);
      }

      scratchProjMatrix[2]  = 0.5 * (scratchProjMatrix[2]  + scratchProjMatrix[3]);
      scratchProjMatrix[6]  = 0.5 * (scratchProjMatrix[6]  + scratchProjMatrix[7]);
      scratchProjMatrix[10] = 0.5 * (scratchProjMatrix[10] + scratchProjMatrix[11]);
      scratchProjMatrix[14] = 0.5 * (scratchProjMatrix[14] + scratchProjMatrix[15]);
      sceneMat4MultiplyInto(scratchSelenaViewProjection, scratchProjMatrix, scratchViewMatrix);

      var f = _frameUniformF;
      var u = _frameUniformU;
      f.set(scratchViewMatrix, 0);
      f.set(scratchProjMatrix, 16);
      f[32] = eye.x;
      f[33] = eye.y;
      f[34] = eye.z;
      f[36] = width;
      f[37] = height;
      u[38] = toneMap ? 1 : 0;
      u[39] = 0;
      device.queue.writeBuffer(frameUniformBuffer, 0, f);
      return {
        kind: cam.kind,
        x: eye.x,
        y: eye.y,
        z: eye.z,
        fov: cam.fov,
        near: cam.near,
        far: cam.far,
      };
    }

    // ensureLightCapacity grows the light storage buffer to hold capacity
    // lights. The frame bind group is rebuilt every frame after uploadLights,
    // so the new buffer reaches the shader on the same frame it appears.
    function ensureLightCapacity(capacity) {
      if (capacity <= _lightCapacity && lightStorageBuffer) {
        return;
      }
      if (lightStorageBuffer) {
        _retiredLightBuffers.push(lightStorageBuffer);
      }
      _lightCapacity = capacity;
      _lightDataF = new Float32Array(capacity * SCENE_WEBGPU_LIGHT_FLOATS);
      lightStorageBuffer = device.createBuffer({
        label: "gosx-lights",
        size: capacity * SCENE_WEBGPU_LIGHT_BYTES,
        usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST,
      });
    }

    // reportLightIssuesOnce forwards each new diagnostic exactly once.
    function reportLightIssuesOnce(issues) {
      for (var i = 0; i < issues.length; i++) {
        var issue = issues[i];
        var key = issue.code + "|" + issue.message;
        if (_lightIssuesReported[key]) {
          continue;
        }
        _lightIssuesReported[key] = true;
        sceneWebGPUReportLightingIssue({
          scope: "scene3d",
          type: "lighting",
          source: "webgpu",
          code: issue.code,
          message: issue.message,
        });
      }
    }

    function uploadLights(lights) {
      var lightArray = Array.isArray(lights) ? lights : [];
      ensureLightCapacity(sceneWebGPULightCapacityFor(lightArray.length));
      var count = Math.min(lightArray.length, _lightCapacity);

      // Write lightCount into frame uniform buffer at byte offset 140.
      _lightCountBuf[0] = count;
      device.queue.writeBuffer(frameUniformBuffer, 140, _lightCountBuf);

      var census = sceneWebGPUPackLights(lightArray, count, _lightDataF, _lightColorCache);
      reportLightIssuesOnce(sceneWebGPULightIssues(census, lightArray.length, _lightCapacity));

      if (count > 0) {
        // Write only the used prefix. The shader stops at lightCount, so stale
        // floats past that point never reach a fragment.
        device.queue.writeBuffer(
          lightStorageBuffer,
          0,
          _lightDataF,
          0,
          count * SCENE_WEBGPU_LIGHT_FLOATS,
        );
      }
      return census;
    }

    function uploadFogUniforms(environment) {
      var env = environment || {};
      var fogDensity = sceneNumber(env.fogDensity, 0);
      var fogColorRGBA = sceneColorRGBA(env.fogColor, [0.5, 0.5, 0.5, 1]);

      // FogUniforms: vec3f fogColor(12) + f32 density(4) + u32 hasFog(4) + pad(12) = 32 bytes.
      var f = _fogUniformF;
      var u = _fogUniformU;
      f[0] = fogColorRGBA[0];
      f[1] = fogColorRGBA[1];
      f[2] = fogColorRGBA[2];
      f[3] = fogDensity;
      u[4] = fogDensity > 0 ? 1 : 0;
      u[5] = 0;
      u[6] = 0;
      u[7] = 0;
      device.queue.writeBuffer(fogUniformBuffer, 0, f);
    }

    function webGPUIBLProduct(ibl, name, role, view, format) {
      var source = ibl && ibl[name] && typeof ibl[name] === "object" ? ibl[name] : null;
      if (!source || typeof source.uri !== "string" || !source.uri.trim()) return null;
      var descriptor = wgpuTextureDescriptor(source, source.uri, role, "linear");
      if (descriptor.role !== role || descriptor.colorSpace !== "linear" || descriptor.view !== view) return null;
      if (descriptor.format && descriptor.format !== format) return null;
      return descriptor;
    }

    function webGPUIBLRecordValid(record, descriptor, brdfModel) {
      if (!record || !record.loaded || record.failed) return false;
      if (!descriptor || descriptor.width <= 0 || descriptor.height <= 0 || descriptor.faces <= 0 || descriptor.mipLevels <= 0) return false;
      var metadata = record.keyValues || {};
      if (metadata.GoSXiblRole !== descriptor.role) return false;
      if (metadata.GoSXColorSpace !== "linear") return false;
      if (brdfModel && metadata.GoSXiblModel !== brdfModel) return false;
      var expectedVKFormat = descriptor.format === "rgba16f" ? 97 : (descriptor.format === "rg16f" ? 83 : 0);
      if (!expectedVKFormat || record.vkFormat !== expectedVKFormat) return false;
      if (record.width !== descriptor.width || record.height !== descriptor.height) return false;
      if (record.faces !== descriptor.faces || record.levels !== descriptor.mipLevels) return false;
      return true;
    }

    function webGPUIBLRoughnessMappingValid(ibl, mipLevels) {
      var mapping = ibl && Array.isArray(ibl.roughnessPerLevel) ? ibl.roughnessPerLevel : [];
      if (mipLevels <= 0 || mapping.length !== mipLevels) return false;
      for (var i = 0; i < mapping.length; i++) {
        var expected = mipLevels <= 1 ? 0 : i / (mipLevels - 1);
        if (!Number.isFinite(mapping[i]) || Math.abs(mapping[i] - expected) > 0.000001) return false;
      }
      return true;
    }

    function syncEnvironmentIBL(environment) {
      var env = environment || {};
      var ibl = env.ibl && typeof env.ibl === "object" ? env.ibl : null;
      if (!ibl) {
        iblResources.active = false;
        iblResources.diagnostics = { requested: false, active: false, state: "not-requested", reason: "", radianceMipLevels: 0 };
        return iblResources;
      }
      var radiance = webGPUIBLProduct(ibl, "radiance", "environment-radiance", "cube", "rgba16f");
      var irradiance = webGPUIBLProduct(ibl, "irradiance", "environment-irradiance", "cube", "rgba16f");
      var brdf = webGPUIBLProduct(ibl, "brdfLUT", "brdf-lut", "2d", "rg16f");
      var model = typeof ibl.brdfModel === "string" ? ibl.brdfModel.trim() : "";
      var diag = { requested: true, active: false, state: "validating", reason: "", radianceMipLevels: 0 };
      if (!radiance || !irradiance || !brdf) {
        diag.state = "unsupported";
        diag.reason = "descriptor-role-color-view-format";
      } else if (model !== SCENE_WEBGPU_IBL_BRDF_MODEL) {
        diag.state = "unsupported";
        diag.reason = "brdf-model:" + (model || "missing");
      } else if (!webGPUIBLRoughnessMappingValid(ibl, radiance.mipLevels)) {
        diag.state = "unsupported";
        diag.reason = "roughness-mip-mapping";
      } else if (!wgpuKTX2API()) {
        diag.state = "unsupported";
        diag.reason = "ktx2-loader-unavailable";
      } else {
        var key = [radiance.uri, irradiance.uri, brdf.uri, model].join("\u0000");
        if (iblResources.key !== key) {
          iblResources.key = key;
          iblResources.radiance = wgpuLoadTexture(device, radiance.uri, textureCache, radiance);
          iblResources.irradiance = wgpuLoadTexture(device, irradiance.uri, textureCache, irradiance);
          iblResources.brdfLUT = wgpuLoadTexture(device, brdf.uri, textureCache, brdf);
        }
        var failed = [iblResources.radiance, iblResources.irradiance, iblResources.brdfLUT].some(function(record) {
          return record && record.failed;
        });
        var active = webGPUIBLRecordValid(iblResources.radiance, radiance, model) &&
          webGPUIBLRecordValid(iblResources.irradiance, irradiance, "lambert-sh9") &&
          webGPUIBLRecordValid(iblResources.brdfLUT, brdf, model);
        if (active) {
          diag.active = true;
          diag.state = "active";
          diag.radianceMipLevels = Math.max(1, iblResources.radiance.levels || radiance.mipLevels || 1);
        } else {
          var allLoaded = [iblResources.radiance, iblResources.irradiance, iblResources.brdfLUT].every(function(record) {
            return record && record.loaded;
          });
          diag.state = failed || allLoaded ? "failed" : "loading";
          diag.reason = failed ? "product-upload" : (allLoaded ? "product-container-metadata" : "product-pending");
        }
      }
      iblResources.active = diag.active;
      iblResources.diagnostics = diag;
      if ((diag.state === "unsupported" || diag.state === "failed") && iblResources.lastWarning !== diag.reason) {
        iblResources.lastWarning = diag.reason;
        try { console.warn("[gosx] WebGPU IBL " + diag.state + ": " + diag.reason); } catch (_error) {}
        renderTruth().record("ibl-" + diag.state, diag.reason);
      }
      return iblResources;
    }

    // syncEnvironmentMap loads the legacy equirectangular Environment.EnvironmentMap
    // image through the shared 2D texture cache/loader (wgpuLoadTexture), the
    // same path material albedo maps use. IBL wins: iblActive (ibl.active from
    // syncEnvironmentIBL, not merely "requested") suppresses this, matching
    // 16-scene-webgl.js's scenePBRUploadEnvironmentMap status machine, which
    // only zeroes hasEnvMap once iblStatus.active is true.
    function syncEnvironmentMap(environment, iblActive) {
      var env = environment || {};
      var url = typeof env.envMap === "string" ? env.envMap.trim() : "";
      if (!url || iblActive) {
        envMapResources.active = false;
        return envMapResources;
      }
      if (envMapResources.key !== url) {
        envMapResources.key = url;
        envMapResources.record = wgpuLoadTexture(device, url, textureCache, null, "environment-radiance", "linear");
      }
      var record = envMapResources.record;
      envMapResources.active = Boolean(record && record.loaded && !record.failed);
      return envMapResources;
    }

    function uploadEnvUniforms(environment) {
      var env = environment || {};
      var ibl = syncEnvironmentIBL(env);
      var envMap = syncEnvironmentMap(env, ibl.active);
      var ambientColorRGBA = sceneColorRGBA(env.ambientColor, [1, 1, 1, 1]);
      var skyColorRGBA = sceneColorRGBA(env.skyColor, [0.88, 0.94, 1, 1]);
      var groundColorRGBA = sceneColorRGBA(env.groundColor, [0.12, 0.16, 0.22, 1]);

      // EnvUniforms: three vec3+scalar pairs, one IBL control vec4, and one
      // envMap control vec4 (only word 0 used today) = 80 bytes.
      var data = _envUniformF;
      var words = _envUniformU;
      data[0] = ambientColorRGBA[0]; data[1] = ambientColorRGBA[1]; data[2] = ambientColorRGBA[2];
      data[3] = sceneNumber(env.ambientIntensity, 0);
      data[4] = skyColorRGBA[0]; data[5] = skyColorRGBA[1]; data[6] = skyColorRGBA[2];
      data[7] = sceneNumber(env.skyIntensity, 0);
      data[8] = groundColorRGBA[0]; data[9] = groundColorRGBA[1]; data[10] = groundColorRGBA[2];
      data[11] = sceneNumber(env.groundIntensity, 0);
      data[12] = Math.max(0, sceneNumber(env.envIntensity, 1));
      data[13] = sceneNumber(env.envRotation, 0);
      words[14] = ibl.active ? 1 : 0;
      words[15] = ibl.active ? Math.max(1, ibl.diagnostics.radianceMipLevels | 0) : 0;
      words[16] = envMap.active ? 1 : 0;
      words[17] = 0; words[18] = 0; words[19] = 0;
      device.queue.writeBuffer(envUniformBuffer, 0, data);
    }

    function uploadShadowUniforms(shadowLightMatrices, shadowLightIndices, lights) {
      var lightArray = Array.isArray(lights) ? lights : [];
      // ShadowUniforms: mat4(64) + mat4(64) + 6*u32(24) + pad(8) = 160. Round up to 256.
      var f = _shadowUniformF;
      var u = _shadowUniformU;
      var i = _shadowUniformI;

      if (shadowLightMatrices[0]) {
        f.set(shadowLightMatrices[0], 0);   // lightSpaceMatrix0 @ offset 0
      } else {
        f.fill(0, 0, 16);                   // zero out slot 0 (no stale matrix)
      }
      if (shadowLightMatrices[1]) {
        f.set(shadowLightMatrices[1], 16);  // lightSpaceMatrix1 @ offset 64
      } else {
        f.fill(0, 16, 32);                  // zero out slot 1 (no stale matrix)
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

      device.queue.writeBuffer(shadowUniformBuffer, 0, f);
    }

    // Normal-incidence dielectric Fresnel for an authored material IOR:
    // F0 = ((ior-1)/(ior+1))^2. Total function — missing, null, invalid,
    // non-finite, negative and 0<ior<1 inputs fall back to the default ior
    // 1.5 (F0 0.04); the glTF explicit-zero compatibility mode maps to a
    // Fresnel of exactly 1. The (ior-1)/(ior+1) form stays stable for huge
    // finite inputs and the result always fits float32 for upload.
    function sceneWebGPUDielectricF0(ior) {
      var value = typeof ior === "number"
        ? ior
        : (typeof ior === "string" && ior.trim() !== "" ? Number(ior) : NaN);
      if (!(Number.isFinite(value) && (value >= 1 || value === 0))) {
        value = 1.5;
      }
      if (value === 0) {
        return 1;
      }
      var t = (value - 1) / (value + 1);
      return t * t;
    }

    // Effective dielectric specular factors from the authored KHR-style
    // specularIntensity / specularColor factors. The intensity contract is a
    // finite number in [0, 1] — an explicit 0 is valid, an omitted value
    // means 1 — and the color contract is exactly three finite non-negative
    // LINEAR components, omitted meaning white. The effective F0 is
    // min(IOR F0 * color, 1) * intensity with the clamp applied BEFORE the
    // intensity so a finite HDR tint above 1 clamps to 1 rather than scaling
    // past it, and F90 is the intensity itself. Every returned component is
    // finite and non-negative, so the packed buffer can never see NaN or
    // Infinity, and the result is bounded to [0, 1]. A future specular
    // texture must multiply its colour into `color` BEFORE this clamp, never
    // into an already-clamped F0.
    function sceneWebGPUSpecularFactors(material) {
      var mat = material || {};
      var intensity = mat.specularIntensity;
      if (!(typeof intensity === "number" && Number.isFinite(intensity) && intensity >= 0 && intensity <= 1)) {
        intensity = 1;
      }
      var color = mat.specularColor;
      var valid = Boolean(color) && typeof color.length === "number" && color.length === 3;
      if (valid) {
        for (var i = 0; i < 3; i++) {
          var component = color[i];
          if (!(typeof component === "number" && Number.isFinite(component) && component >= 0)) {
            valid = false;
            break;
          }
        }
      }
      if (!valid) {
        color = [1, 1, 1];
      }
      var iorF0 = sceneWebGPUDielectricF0(mat.ior);
      return {
        f0: [
          Math.min(iorF0 * color[0], 1) * intensity,
          Math.min(iorF0 * color[1], 1) * intensity,
          Math.min(iorF0 * color[2], 1) * intensity,
        ],
        f90: intensity,
      };
    }

    function materialUniformData(material, receiveShadow, modelMatrix, modelScaleSigns) {
      var mat = material || {};
      var albedoRGBA = sceneColorRGBA(mat.color, [0.8, 0.8, 0.8, 1]);

      // MaterialUniforms: PBR fields (80 bytes) + per-object model matrix
      // (64 bytes) + three scale signs (16-byte aligned) + one trailing
      // dielectric-F0 scalar plus the vec3f-aligned effective specular
      // factors and F90 (struct padded to 192 bytes). The signs recover
      // the rotation-only normal/tangent transform used by the CPU-baked path,
      // including negative and non-uniform scale. World-baked and instanced
      // draws receive identity.
      // Uses hoisted module-scope scratch; caller consumes synchronously before next call.
      var f = _materialUniformF;
      var u = _materialUniformU;

      f[0] = sceneWebGPUSRGBChannelToLinear(albedoRGBA[0]);
      f[1] = sceneWebGPUSRGBChannelToLinear(albedoRGBA[1]);
      f[2] = sceneWebGPUSRGBChannelToLinear(albedoRGBA[2]);
      f[3] = sceneNumber(mat.roughness, 0.5);
      f[4] = sceneNumber(mat.metalness, 0);
      f[5] = sceneNumber(mat.emissive, 0);
      f[6] = clamp01(sceneNumber(mat.opacity, 1));
      f[7] = clamp01(sceneNumber(mat.clearcoat, 0));
      f[8] = clamp01(sceneNumber(mat.sheen, 0));
      f[9] = clamp01(sceneNumber(mat.transmission, 0));
      f[10] = clamp01(sceneNumber(mat.iridescence, 0));
      f[11] = Math.max(-1, Math.min(1, sceneNumber(mat.anisotropy, 0)));
      u[12] = (mat.unlit || mat.kind === "flat" || mat.materialKind === "flat") ? 1 : 0;
      // u[13..17] set by caller (texture-loaded flags); zero here for fields not written below
      u[13] = 0; u[14] = 0; u[15] = 0; u[16] = 0; u[17] = 0;
      u[18] = receiveShadow ? 1 : 0;
      u[19] = 0; // hasOcclusionMap, set by createMaterialBindGroup
      var model = modelMatrix && typeof modelMatrix.length === "number" && modelMatrix.length >= 16
        ? modelMatrix
        : null;
      for (var mi = 0; mi < 16; mi++) {
        f[20 + mi] = model ? sceneNumber(model[mi], mi % 5 === 0 ? 1 : 0) : (mi % 5 === 0 ? 1 : 0);
      }
      f[36] = modelScaleSigns ? sceneNumber(modelScaleSigns[0], 1) : 1;
      f[37] = modelScaleSigns ? sceneNumber(modelScaleSigns[1], 1) : 1;
      f[38] = modelScaleSigns ? sceneNumber(modelScaleSigns[2], 1) : 1;
      f[39] = 0;
      // Dedicated trailing material scalars: normal-incidence dielectric F0
      // from the authored IOR, then the effective specular factors (F0 rgb,
      // F90 = intensity) at the vec3f-aligned slots 44..47. Slots 41..43 are
      // vec3f alignment padding; keep them zeroed so the packed material
      // bytes stay deterministic.
      f[40] = sceneWebGPUDielectricF0(mat.ior);
      f[41] = 0;
      f[42] = 0;
      f[43] = 0;
      var specular = sceneWebGPUSpecularFactors(mat);
      f[44] = specular.f0[0];
      f[45] = specular.f0[1];
      f[46] = specular.f0[2];
      f[47] = specular.f90;
      return { data: f, u: u };
    }

    // Memoized bind group on an owner slot. Bind groups reference their
    // resources by identity, so one stays valid while the device, the layout,
    // and every bound resource are unchanged. Recreating them per frame
    // churned thousands of GPU wrapper objects per second across the point
    // layers; WebKit frees the backing Metal objects only on JS garbage
    // collection, so the churn grows the GPU process between collections
    // (iOS jetsam pressure). Identity source per entry: `resource.buffer`
    // for buffer bindings, the resource itself for views and samplers.
    function wgpuCachedBindGroup(owner, slot, layout, entries) {
      var cache = owner[slot];
      if (cache && cache.device === device && cache.layout === layout && cache.ids.length === entries.length) {
        var match = true;
        for (var ci = 0; ci < entries.length && match; ci++) {
          var res = entries[ci].resource;
          if (cache.ids[ci] !== (res && res.buffer ? res.buffer : res)) match = false;
        }
        if (match) return cache.bg;
      }
      var ids = [];
      for (var ii = 0; ii < entries.length; ii++) {
        var r = entries[ii].resource;
        ids.push(r && r.buffer ? r.buffer : r);
      }
      var bg = device.createBindGroup({ layout: layout, entries: entries });
      owner[slot] = { device: device, layout: layout, ids: ids, bg: bg };
      return bg;
    }

    function createMaterialBindGroup(material, receiveShadow, cacheOwner, modelMatrix, modelScaleSigns) {
      var mat = material || {};
      var uniform = materialUniformData(mat, receiveShadow, modelMatrix, modelScaleSigns);
      var u = uniform.u;
      // Texture records.
      var textureMaps = [
        { prop: "texture", descriptor: "baseColor", role: "base-color", colorSpace: "srgb", index: 13 },
        { prop: "normalMap", descriptor: "normal", role: "normal", colorSpace: "linear", index: 14 },
        { prop: "roughnessMap", descriptor: "roughness", role: "roughness", colorSpace: "linear", index: 15 },
        { prop: "metalnessMap", descriptor: "metalness", role: "metalness", colorSpace: "linear", index: 16 },
        { prop: "emissiveMap", descriptor: "emissive", role: "emissive", colorSpace: "srgb", index: 17 },
        { prop: "occlusionMap", descriptor: "occlusion", role: "ambient-occlusion", colorSpace: "linear", index: 19 },
      ];

      var texViews = [];
      for (var ti = 0; ti < textureMaps.length; ti++) {
        var tm = textureMaps[ti];
        var descriptor = mat.textureDescriptors && mat.textureDescriptors[tm.descriptor];
        var url = descriptor && typeof descriptor.uri === "string" && descriptor.uri.trim()
          ? descriptor.uri.trim()
          : mat[tm.prop];
        var record = url ? wgpuLoadTexture(device, url, textureCache, descriptor, tm.role, tm.colorSpace) : null;
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

      // Memoize the bind group. The material uniform buffer is written above
      // (dynamic=true) and referenced by identity in the bind group, so the
      // same bind group remains valid across frames as long as the material
      // buffer and texture views are unchanged. Invalidate on device change
      // (device-loss recovery) or when a texture finishes async loading.
      var bgCacheSlot = receiveShadow ? "_gosxWGPUMatBGCacheS" : "_gosxWGPUMatBGCache";
      var bgCache = owner[bgCacheSlot];
      if (bgCache && bgCache.device === device && bgCache.materialBuffer === materialBuffer) {
        var viewsMatch = true;
        for (var ti2 = 0; ti2 < texViews.length && viewsMatch; ti2++) {
          if (bgCache.texViews[ti2] !== texViews[ti2]) viewsMatch = false;
        }
        if (viewsMatch) return bgCache.bg;
      }
      // Create bind group with texture views and sampler.
      var matBG = device.createBindGroup({
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
          { binding: 11, resource: texViews[5] },
          { binding: 12, resource: linearSampler },
        ],
      });
      owner[bgCacheSlot] = { device: device, materialBuffer: materialBuffer, texViews: texViews, bg: matBG };
      return matBG;
    }

    // _frameBindGroupCache memoizes the frame bind group. Every entry is a
    // buffer, a texture view or a sampler held BY REFERENCE, and every one of
    // them is written with queue.writeBuffer rather than replaced, so the same
    // bind group stays correct across frames. Rebuilding it each frame cost one
    // createBindGroup per frame and, worse, gave the render-bundle recorder a
    // new object identity every frame, so no bundle could ever replay.
    //
    // The cache compares identities, so a grown light buffer, a resized shadow
    // map or a device-loss recovery rebuilds it.
    var _frameBindGroupCache = null;

    function createFrameBindGroup(shadowView0, shadowView1) {
      var view0 = shadowView0 || dummyShadowView;
      var view1 = shadowView1 || dummyShadowView;
      var iblIrradianceView = iblResources.active && iblResources.irradiance ? iblResources.irradiance.view : placeholderCubeView;
      var iblRadianceView = iblResources.active && iblResources.radiance ? iblResources.radiance.view : placeholderCubeView;
      var iblBRDFView = iblResources.active && iblResources.brdfLUT ? iblResources.brdfLUT.view : placeholderView;
      var envMapView = envMapResources.active && envMapResources.record ? envMapResources.record.view : placeholderView;
      var cache = _frameBindGroupCache;
      if (
        cache &&
        cache.device === device &&
        cache.layout === frameBindGroupLayout &&
        cache.frame === frameUniformBuffer &&
        cache.lights === lightStorageBuffer &&
        cache.fog === fogUniformBuffer &&
        cache.env === envUniformBuffer &&
        cache.view0 === view0 &&
        cache.view1 === view1 &&
        cache.sampler === comparisonSampler &&
        cache.shadow === shadowUniformBuffer &&
        cache.iblIrradiance === iblIrradianceView &&
        cache.iblRadiance === iblRadianceView &&
        cache.iblBRDF === iblBRDFView &&
        cache.iblSampler === linearSampler &&
        cache.envMap === envMapView &&
        cache.envMapSampler === envMapSampler
      ) {
        return cache.bindGroup;
      }
      var bindGroup = _createFrameBindGroupUncached(view0, view1, iblIrradianceView, iblRadianceView, iblBRDFView, envMapView);
      _frameBindGroupCache = {
        device: device,
        layout: frameBindGroupLayout,
        frame: frameUniformBuffer,
        lights: lightStorageBuffer,
        fog: fogUniformBuffer,
        env: envUniformBuffer,
        view0: view0,
        view1: view1,
        sampler: comparisonSampler,
        shadow: shadowUniformBuffer,
        iblIrradiance: iblIrradianceView,
        iblRadiance: iblRadianceView,
        iblBRDF: iblBRDFView,
        iblSampler: linearSampler,
        envMap: envMapView,
        envMapSampler: envMapSampler,
        bindGroup: bindGroup,
      };
      return bindGroup;
    }

    function _createFrameBindGroupUncached(shadowView0, shadowView1, iblIrradianceView, iblRadianceView, iblBRDFView, envMapView) {
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
          { binding: 9, resource: iblIrradianceView || placeholderCubeView },
          { binding: 10, resource: iblRadianceView || placeholderCubeView },
          { binding: 11, resource: iblBRDFView || placeholderView },
          { binding: 12, resource: linearSampler },
          { binding: 13, resource: envMapView || placeholderView },
          { binding: 14, resource: envMapSampler },
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

    function webGPURetainedMaterialSource(obj) {
      return obj && obj.resourceOwner && typeof obj.resourceOwner === "object"
        ? obj.resourceOwner
        : (obj && obj.vertices && typeof obj.vertices === "object" ? obj.vertices : obj);
    }

    function webGPURetireRetainedMaterialOwner(entry) {
      if (!entry || !entry.materialOwner) return;
      var owner = entry.materialOwner;
      // A resource owner may be shared by multiple retained entries. Do not
      // retire its material buffers until the final associated entry leaves.
      for (const pair of retainedMeshAttributeCache.entries()) {
        if (pair[1] !== entry && pair[1] && pair[1].materialOwner === owner) {
          return;
        }
      }
      var slots = ["_gosxWGPUMaterialUniform", "_gosxWGPUMaterialShadowUniform"];
      for (var slotIndex = 0; slotIndex < slots.length; slotIndex++) {
        var slot = slots[slotIndex];
        var buffer = owner[slot];
        if (buffer) {
          pointsEntryGPUBuffers.delete(buffer);
          try { buffer.destroy(); } catch (_err) {}
        }
        owner[slot] = null;
        owner[slot + "Bytes"] = 0;
        owner[slot + "Source"] = null;
      }
      owner["_gosxWGPUMatBGCache"] = null;
      owner["_gosxWGPUMatBGCacheS"] = null;
      if (
        entry.materialSource &&
        retainedMaterialOwners.get(entry.materialSource) === owner
      ) {
        retainedMaterialOwners.delete(entry.materialSource);
      }
      entry.materialOwner = null;
      entry.materialSource = null;
    }

    function webGPURetainedMaterialOwner(obj) {
      var vertices = obj && obj.vertices;
      var entry = vertices && retainedMeshAttributeCache.get(vertices);
      // Retire the old material buffer before createMaterialBindGroup writes
      // and binds the replacement revision. Waiting for the later vertex
      // binding path would destroy a buffer already referenced by this frame.
      if (entry && entry.revision !== obj.geometryRevision) {
        retainedMeshBufferStats.rebuilds += 1;
        retainedMeshBufferStats.revisionInvalidations += 1;
        webGPURetireRetainedMeshEntry(vertices, entry);
      }
      var source = webGPURetainedMaterialSource(obj);
      if (!source || typeof source !== "object") return defaultMaterialOwner;
      var owner = retainedMaterialOwners.get(source);
      if (!owner) {
        owner = {};
        retainedMaterialOwners.set(source, owner);
      }
      return owner;
    }

    function webGPURetireRetainedMeshAttribute(entry, key) {
      var record = entry && entry.attributes && entry.attributes[key];
      if (!record) return;
      if (record.buffer) {
        pointsEntryGPUBuffers.delete(record.buffer);
        try { record.buffer.destroy(); } catch (_err) {}
      }
      retainedMeshBufferStats.liveBytes = Math.max(0, retainedMeshBufferStats.liveBytes - record.byteLength);
      retainedMeshBufferStats.retirements += 1;
      delete entry.attributes[key];
    }

    function webGPURetireRetainedMeshEntry(vertices, entry) {
      if (!entry) return;
      var keys = Object.keys(entry.attributes || {});
      for (var i = 0; i < keys.length; i++) {
        webGPURetireRetainedMeshAttribute(entry, keys[i]);
      }
      webGPURetireRetainedMaterialOwner(entry);
      retainedMeshAttributeCache.delete(vertices);
    }

    function webGPUBeginRetainedMeshFrame(bundle) {
      retainedMeshAttributeEpoch += 1;
      var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
      for (var i = 0; i < objects.length; i++) {
        var obj = objects[i];
        // Direct-vertex entries (including skinned/morph draws that carry an
        // authored index stream) are marked so their cached GPU buffers survive
        // the sweep exactly like fully retained ones.
        if (!obj || !obj.vertices) continue;
        if (!obj.retainedGeometry && !obj.directVertices) continue;
        var entry = retainedMeshAttributeCache.get(obj.vertices);
        if (entry) entry.lastSeenEpoch = retainedMeshAttributeEpoch;
      }
    }

    function webGPUSweepRetainedMeshBuffers() {
      for (const pair of Array.from(retainedMeshAttributeCache.entries())) {
        if (pair[1].lastSeenEpoch !== retainedMeshAttributeEpoch) {
          webGPURetireRetainedMeshEntry(pair[0], pair[1]);
        }
      }
    }

    function webGPURetainedMeshBufferStats() {
      return Object.assign({}, retainedMeshBufferStats, {
        cacheEntries: retainedMeshAttributeCache.size,
        epoch: retainedMeshAttributeEpoch,
      });
    }

    function webGPURetainedMeshFrameStats() {
      var stats = webGPURetainedMeshBufferStats();
      return {
        retainedCacheEntries: stats.cacheEntries,
        retainedCacheHits: stats.hits,
        retainedCacheMisses: stats.misses,
        retainedUploadCalls: stats.uploadCalls,
        retainedUploadBytes: stats.uploadBytes,
        retainedAllocations: stats.allocations,
        retainedRebuilds: stats.rebuilds,
        retainedRevisionInvalidations: stats.revisionInvalidations,
        retainedRetirements: stats.retirements,
        retainedLiveBytes: stats.liveBytes,
      };
    }

    function webGPUBindRetainedMeshAttribute(pass, slot, obj, key, components) {
      if (!obj || !obj.retainedGeometry || !obj.directVertices) return false;
      var count = webGPUObjectVertexCount(obj);
      var data = webGPUDirectAttribute(obj, key, count, components);
      if (!data) return false;
      var vertices = obj.vertices;
      var entry = retainedMeshAttributeCache.get(vertices);
      if (entry && entry.revision !== obj.geometryRevision) {
        retainedMeshBufferStats.rebuilds += 1;
        retainedMeshBufferStats.revisionInvalidations += 1;
        webGPURetireRetainedMeshEntry(vertices, entry);
        entry = null;
      }
      if (!entry) {
        entry = {
          revision: obj.geometryRevision,
          lastSeenEpoch: retainedMeshAttributeEpoch,
          attributes: Object.create(null),
        };
        retainedMeshAttributeCache.set(vertices, entry);
      }
      var materialSource = webGPURetainedMaterialSource(obj);
      if (materialSource && typeof materialSource === "object") {
        entry.materialSource = materialSource;
        entry.materialOwner = retainedMaterialOwners.get(materialSource) || null;
      }
      entry.lastSeenEpoch = retainedMeshAttributeEpoch;
      var record = entry.attributes[key];
      if (!record || record.data !== data || record.components !== components) {
        if (record) {
          retainedMeshBufferStats.rebuilds += 1;
          webGPURetireRetainedMeshAttribute(entry, key);
        }
        var buffer = wgpuCreateTrackedBuffer(
          GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST,
          data.byteLength || 4
        );
        if (!buffer) return false;
        device.queue.writeBuffer(buffer, 0, data);
        record = {
          buffer: buffer,
          data: data,
          components: components,
          byteLength: data.byteLength,
        };
        entry.attributes[key] = record;
        retainedMeshBufferStats.misses += 1;
        retainedMeshBufferStats.allocations += 1;
        retainedMeshBufferStats.uploadCalls += 1;
        retainedMeshBufferStats.uploadBytes += data.byteLength;
        retainedMeshBufferStats.liveBytes += data.byteLength;
      } else {
        retainedMeshBufferStats.hits += 1;
      }
      pass.setVertexBuffer(slot, record.buffer, 0, Math.max(4, count * components * 4));
      return true;
    }

    // webGPUBindRetainedMeshIndexBuffer uploads (once) and binds the optional
    // authored index stream of a direct-vertex mesh as a uint32 index buffer,
    // returning its triangle-index count. Retained entries rebuild on revision
    // change and retire with their attribute buffers; non-retained direct
    // geometry (skinned draws) reuses while the Uint32Array identity is stable.
    // Returns 0 when there are no valid indices so callers keep draw().
    function webGPUBindRetainedMeshIndexBuffer(pass, obj) {
      if (!pass) return 0;
      var vertices = obj && obj.vertices;
      if (!vertices) return 0;
      var data = vertices.indices;
      if (!(data instanceof Uint32Array) || data.length < 3 || data.length % 3 !== 0) {
        return 0;
      }
      var retained = obj.retainedGeometry === true;
      var entry = retainedMeshAttributeCache.get(vertices);
      if (entry && retained && entry.revision !== obj.geometryRevision) {
        retainedMeshBufferStats.rebuilds += 1;
        retainedMeshBufferStats.revisionInvalidations += 1;
        webGPURetireRetainedMeshEntry(vertices, entry);
        entry = null;
      }
      if (!entry) {
        entry = {
          revision: retained ? obj.geometryRevision : undefined,
          lastSeenEpoch: retainedMeshAttributeEpoch,
          attributes: Object.create(null),
        };
        retainedMeshAttributeCache.set(vertices, entry);
      }
      var materialSource = webGPURetainedMaterialSource(obj);
      if (materialSource && typeof materialSource === "object" && !entry.materialSource) {
        entry.materialSource = materialSource;
        entry.materialOwner = retainedMaterialOwners.get(materialSource) || null;
      }
      entry.lastSeenEpoch = retainedMeshAttributeEpoch;
      var record = entry.attributes.indices;
      if (!record || record.data !== data) {
        if (record) {
          retainedMeshBufferStats.rebuilds += 1;
          webGPURetireRetainedMeshAttribute(entry, "indices");
        }
        var buffer = wgpuCreateTrackedBuffer(
          GPUBufferUsage.INDEX | GPUBufferUsage.COPY_DST,
          data.byteLength || 4
        );
        if (!buffer) return 0;
        device.queue.writeBuffer(buffer, 0, data);
        record = { buffer: buffer, data: data, components: 1, byteLength: data.byteLength };
        entry.attributes.indices = record;
        retainedMeshBufferStats.misses += 1;
        retainedMeshBufferStats.allocations += 1;
        retainedMeshBufferStats.uploadCalls += 1;
        retainedMeshBufferStats.uploadBytes += data.byteLength;
        retainedMeshBufferStats.liveBytes += data.byteLength;
      } else {
        retainedMeshBufferStats.hits += 1;
      }
      pass.setIndexBuffer(record.buffer, "uint32");
      return data.length;
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
      // Same source defaults the CPU draw path used before skinning moved to
      // the compute pass: normals fall back to +Z, tangents to (1,0,0,w=1).
      var normals = webGPUDefaultAttributeData(obj, "normals", count, 3, [0, 0, 1]);
      var tangents = webGPUDefaultAttributeData(obj, "tangents", count, 4, [1, 0, 0, 1]);
      if (!vertices || !positions || !joints || !weights || !normals || !tangents || count <= 0 || paddedCount <= 0) return null;
      var cache = vertices._gosxWGPUElioSkinVertexData;
      if (
        cache &&
        cache.positions === positions &&
        cache.joints === joints &&
        cache.weights === weights &&
        cache.normals === normals &&
        cache.tangents === tangents &&
        cache.count === count &&
        cache.paddedCount === paddedCount &&
        cache.jointCount === jointCount
      ) {
        return cache.data;
      }

      var stride = 72;
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
          view.setFloat32(off + 44, sceneNumber(normals[p], 0), true);
          view.setFloat32(off + 48, sceneNumber(normals[p + 1], 0), true);
          view.setFloat32(off + 52, sceneNumber(normals[p + 2], 0), true);
          view.setFloat32(off + 56, sceneNumber(tangents[q], 1), true);
          view.setFloat32(off + 60, sceneNumber(tangents[q + 1], 0), true);
          view.setFloat32(off + 64, sceneNumber(tangents[q + 2], 0), true);
          view.setFloat32(off + 68, sceneNumber(tangents[q + 3], 1), true);
        } else {
          view.setFloat32(off + 12, 1, true);
        }
      }

      cache = {
        positions: positions,
        joints: joints,
        weights: weights,
        normals: normals,
        tangents: tangents,
        count: count,
        paddedCount: paddedCount,
        jointCount: jointCount,
        data: bytes,
      };
      vertices._gosxWGPUElioSkinVertexData = cache;
      return bytes;
    }

    function webGPUElioEnsureOutputBuffer(record, paddedCount) {
      var bytes = Math.max(4, paddedCount * 10 * 4);
      // Cross-renderer staleness guard: scene objects retain their skin
      // records across renderer rebuilds, but dispose() destroys every buffer
      // tracked in pointsEntryGPUBuffers. A cached outputBuffer absent from
      // THIS renderer's set belongs to a dead device — drop the stale JS
      // reference WITHOUT calling destroy() again (dispose already destroyed
      // it), so the alloc path below creates a fresh buffer on the current
      // device. Mirrors the owner[slot] guard in sceneCachedTrackedBuffer.
      // The bind group is invalidated too: it was created on the dead device
      // and references the destroyed buffer, so no cache path below may
      // return with a live-looking bindGroup around the dead buffer.
      if (record.outputBuffer && !pointsEntryGPUBuffers.has(record.outputBuffer)) {
        record.outputBuffer = null;
        record.bindGroup = null;
      }
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
      // paddedCount places the normal/tangent regions inside the single
      // output buffer; the kernel derives it from arrayLength(&verts).
      if (!boneBuffer || !vertexBuffer || !outputBuffer) return null;

      if (
        !record.bindGroup ||
        record.boneBuffer !== boneBuffer ||
        record.vertexBuffer !== vertexBuffer ||
        record.paddedCount !== paddedCount ||
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
      obj._gosxWGPUElioSkinOutputPaddedCount = paddedCount;
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

    function webGPUBindElioSkinnedBuffers(pass, obj, count) {
      var outputBuffer = obj && obj._gosxWGPUElioSkinOutputBuffer;
      if (!outputBuffer) return false;
      // One tracked buffer holds three contiguous paddedCount-sized regions:
      // positions at byte 0, normals at paddedCount*12, tangents at
      // paddedCount*24. Bind each region at its exact offset and the logical
      // (unpadded) draw size; slot 2 stays on its own UV buffer.
      var paddedCount = sceneNumber(obj && obj._gosxWGPUElioSkinOutputPaddedCount, 0);
      if (!(paddedCount > 0)) return false;
      var uvs = webGPUDefaultAttributeData(obj, "uvs", count, 2, [0, 0]);
      var vec3Bytes = Math.max(4, count * 3 * 4);
      pass.setVertexBuffer(0, outputBuffer, 0, vec3Bytes);
      pass.setVertexBuffer(1, outputBuffer, paddedCount * 12, vec3Bytes);
      pass.setVertexBuffer(2, wgpuCachedTrackedBuffer(obj, "_gosxWGPUSkinnedUVs", uvs, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
      pass.setVertexBuffer(3, outputBuffer, paddedCount * 24, Math.max(4, count * 4 * 4));
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

    // webGPUCountViewCulledMeshObjects counts bundle.meshObjects entries with
    // viewCulled === true -- the CPU frustum cull applied before buildDrawList
    // ever hands an object to drawPBRObjects. data-gosx-scene3d-webgpu-mesh-objects
    // publishes bundle.meshObjects.length UNCONDITIONALLY (a bundle/authoring
    // count, not a "what actually drew" count), so a viewCulled object still
    // counts there -- that ambiguity is exactly what let three Selena mesh
    // planes read "3" on data-gosx-scene3d-webgpu-mesh-objects for ~two weeks
    // while a camera-depth sign error CPU-frustum-culled them to zero pixels.
    // Pairing this SUBMITTED/CULLED split (mesh-draw-calls vs mesh-view-culled)
    // alongside the bundle count, mirroring the point/compute-particle
    // draw-call counters, closes that diagnostic gap.
    function webGPUCountViewCulledMeshObjects(bundle) {
      var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
      var count = 0;
      for (var i = 0; i < objects.length; i++) {
        if (objects[i] && objects[i].viewCulled) count++;
      }
      return count;
    }

    // webGPUCountUndrawableMeshObjects counts entries buildDrawList rejects for
    // DEGENERATE GEOMETRY (non-finite vertexOffset/vertexCount, or zero
    // vertices) rather than for frustum culling. Without it the accounting is
    // open-ended: meshObjects - meshDrawCalls - meshViewCulled leaves an
    // unexplained remainder that could be a cull bug, a planner bug or a
    // geometry bug. With it the identity
    //     meshObjects == meshDrawCalls + meshViewCulled + meshUndrawable
    // closes, and any violation is itself a reportable defect.
    function webGPUCountUndrawableMeshObjects(bundle) {
      var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
      var count = 0;
      for (var i = 0; i < objects.length; i++) {
        var obj = objects[i];
        if (!obj || obj.viewCulled) continue;
        if (!Number.isFinite(obj.vertexOffset) || !Number.isFinite(obj.vertexCount) || obj.vertexCount <= 0) count++;
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
      var positions = webGPUSceneMeshAttributeData(bundle, "worldMeshPositions", 3, [0, 0, 0], vertexCount);
      var normals = webGPUSceneMeshAttributeData(bundle, "worldMeshNormals", 3, [0, 0, 1], vertexCount);
      var uvs = webGPUSceneMeshAttributeData(bundle, "worldMeshUVs", 2, [0, 0], vertexCount);
      var tangents = webGPUSceneMeshAttributeData(bundle, "worldMeshTangents", 4, [1, 0, 0, 1], vertexCount);
      // wgpuStablePBRAttributeBuffer (not wgpuCachedTrackedBuffer(bundle, ...))
      // -- `bundle` is a brand-new object every render() call (see
      // pbrSceneAttributeCache's declaration above), so caching on it can
      // never skip a re-upload; content-compare against the renderer-scoped
      // cache instead, so a scene whose mesh geometry hasn't actually changed
      // (the common case -- see the water demo's static float-* objects)
      // skips the createBuffer + queue.writeBuffer pair entirely.
      return {
        positions: { buffer: wgpuStablePBRAttributeBuffer("positions", positions), components: 3 },
        normals: { buffer: wgpuStablePBRAttributeBuffer("normals", normals), components: 3 },
        uvs: { buffer: wgpuStablePBRAttributeBuffer("uvs", uvs), components: 2 },
        tangents: { buffer: wgpuStablePBRAttributeBuffer("tangents", tangents), components: 4 },
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

    // Scratch for the per-caster combined (lightVP × model) matrix used by
    // retained indexed casters; allocated lazily, reused every frame.
    var _shadowCombinedMatrixScratch = null;

    function ensureShadowFrameBufferCapacity(matrixCount) {
      var required = Math.max(1, Math.floor(sceneNumber(matrixCount, 1)));
      if (shadowFrameBuffer && shadowFrameBufferCapacity >= required) {
        return true;
      }
      var capacity = Math.max(1, shadowFrameBufferCapacity);
      while (capacity < required) capacity *= 2;
      destroyRendererGPUResource(shadowFrameBuffer);
      shadowFrameBuffer = device.createBuffer({
        size: capacity * shadowFrameBufferStride,
        usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
      });
      shadowFrameBufferCapacity = capacity;
      return Boolean(shadowFrameBuffer);
    }

    function renderShadowPass(encoder, lightMatrix, bundle, shadowResource, pbrBuffers) {
      var sp = getShadowPipeline();
      if (!sp) return;

      var objects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];
      // Slot zero carries the shared light matrix. Every retained caster gets
      // its own aligned slot so queue writes are immutable for the lifetime of
      // the encoded pass; mutating one shared uniform before submit would make
      // every draw observe the final caster's matrix.
      if (!ensureShadowFrameBufferCapacity(objects.length + 1)) return;

      // Upload light space matrix.
      device.queue.writeBuffer(shadowFrameBuffer, 0, lightMatrix, 0, 16);

      var shadowBG = device.createBindGroup({
        layout: shadowBindGroupLayout,
        entries: [
          { binding: 0, resource: { buffer: shadowFrameBuffer, offset: 0, size: 64 } },
        ],
      });

      var shadowPassDescriptor = {
        colorAttachments: [],
        depthStencilAttachment: {
          view: shadowResource.view,
          depthLoadOp: "clear",
          depthClearValue: 1.0,
          depthStoreOp: "store",
        },
      };
      var shadowStamps = gpuPassTimestampWrites("shadow");
      if (shadowStamps) shadowPassDescriptor.timestampWrites = shadowStamps;
      var pass = encoder.beginRenderPass(shadowPassDescriptor);

      pass.setBindGroup(0, shadowBG, [0]);
      var currentShadowPipeline = "";
      var retainedShadowMatrixSlot = 1;

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
            currentShadowPipeline = "static";
          }
          pass.setBindGroup(0, shadowBG, [0]);
          pass.setVertexBuffer(0, skinnedPositionBuffer);
          var skinnedShadowIndexCount = webGPUBindRetainedMeshIndexBuffer(pass, obj);
          if (skinnedShadowIndexCount > 0) pass.drawIndexed(skinnedShadowIndexCount);
          else pass.draw(obj.vertexCount);
          continue;
        }

        var computedMorphRecord = webGPUObjectComputedMorphDrawRecord(obj);
        if (computedMorphRecord) {
          if (currentShadowPipeline !== "static") {
            pass.setPipeline(sp);
            currentShadowPipeline = "static";
          }
          pass.setBindGroup(0, shadowBG, [0]);
          if (!webGPUBindComputedMorphBuffer(pass, 0, computedMorphRecord.positionBuffer, obj.vertexCount, 3)) continue;
          var morphShadowIndexCount = webGPUBindRetainedMeshIndexBuffer(pass, obj);
          if (morphShadowIndexCount > 0) pass.drawIndexed(morphShadowIndexCount);
          else pass.draw(obj.vertexCount);
          continue;
        }

        if (obj.retainedGeometry && obj.directVertices) {
          // Retained indexed geometry casts from cached model-space buffers
          // through a uint32 index buffer. The shadow uniform carries the light
          // view-projection alone, so model-space casters fold their own
          // transform in per draw and restore the base matrix afterwards.
          var casterIndices = obj.vertices && obj.vertices.indices;
          if (!(casterIndices instanceof Uint32Array) || casterIndices.length < 3 || casterIndices.length % 3 !== 0) continue;
          if (currentShadowPipeline !== "static") {
            pass.setPipeline(sp);
            currentShadowPipeline = "static";
          }
          if (!webGPUBindRetainedMeshAttribute(pass, 0, obj, "positions", 3)) continue;
          var casterIndexCount = webGPUBindRetainedMeshIndexBuffer(pass, obj);
          if (!(casterIndexCount > 0)) continue;
          var casterModel = obj.modelMatrix;
          var casterMatrix = lightMatrix;
          if (casterModel && casterModel.length >= 16) {
            if (!_shadowCombinedMatrixScratch) _shadowCombinedMatrixScratch = new Float32Array(16);
            sceneMat4MultiplyInto(_shadowCombinedMatrixScratch, lightMatrix, casterModel);
            casterMatrix = _shadowCombinedMatrixScratch;
          }
          var casterMatrixOffset = retainedShadowMatrixSlot * shadowFrameBufferStride;
          retainedShadowMatrixSlot += 1;
          device.queue.writeBuffer(shadowFrameBuffer, casterMatrixOffset, casterMatrix, 0, 16);
          pass.setBindGroup(0, shadowBG, [casterMatrixOffset]);
          pass.drawIndexed(casterIndexCount);
          continue;
        }

        if (currentShadowPipeline !== "static") {
          pass.setPipeline(sp);
          currentShadowPipeline = "static";
        }

        pass.setBindGroup(0, shadowBG, [0]);
        if (!webGPUBindSceneMeshVertexBuffer(pass, 0, pbrBuffers && pbrBuffers.positions, obj.vertexOffset, obj.vertexCount)) continue;
        pass.draw(obj.vertexCount);
      }

      pass.setBindGroup(0, shadowBG, [0]);
      drawInstancedShadowMeshes(pass, bundle);
      pass.end();
    }

    // -----------------------------------------------------------------------
    // PBR object drawing
    // -----------------------------------------------------------------------

    function drawPBRObjects(pass, objectList, bundle, materials, frameBindGroup, blendMode, depthWrite, pbrBuffers, stats) {
      var lastMaterialIndex = -1;
      var lastReceiveShadow = null;
      var lastMaterialOwner = null;
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
        lastMaterialOwner = null;
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
        // cullMode: "back" is getSelenaPipeline's own default (every OTHER
        // caller passes no options and relies on it -- see the comment on
        // pipelineCullMode there). obj.doubleSided opts a single mesh object
        // out of that default; anything absent/false resolves to the exact
        // same "back" value the hardcoded default produced before this line
        // existed. obj.doubleSided opts a mesh out; cullMode:"none" draws both
        // faces regardless of winding, so it is safe under any convention.
        //
        // HISTORY, because the previous comment here inverted the truth and a
        // reader must not restore it. It warned that "back" facing DEPENDED on
        // box/sphere winding their triangles with the right-hand normal
        // OPPOSITE the declared shading normal. That was a real measurement of
        // a real state, but it described a defect rather than a requirement:
        // 12-scene-geometry.ts wound its solid meshes clockwise while
        // 16c-scene-shared-pbr.ts and scene/geom both wound counter-clockwise,
        // so the same authored shape had opposite winding depending only on
        // whether it was instanced.
        //
        // 12-scene-geometry.ts now winds counter-clockwise, so all three
        // producers agree and every generator's geometric normal points the
        // same way as its shading normal. The native renderer is the evidence
        // that this is the correct sense: render/bundle draws scene/geom with
        // CullBack plus FrontFaceCCW (renderer.go), render/gpu/jsgpu maps that
        // pair to cullMode "back" plus frontFace "ccw" with no inversion, and
        // its golden frames pass. So "back" here now culls the same faces the
        // native path culls.
        //
        // UNVERIFIED ON HARDWARE. Confirm on a real GPU that a non-doubleSided
        // Selena mesh still shows its exterior.
        //
        // This is not the only path the winding change moved, and an earlier
        // note here said it was. Three SHADOW sites cull as well: the two
        // gosx-shadow pipelines above and the WebGL2 shadow pass. They cull the
        // FRONT face, so the winding change moved each of them from recording
        // the near surface of a caster to recording the far one.
        // render/bundle/shadow_drift_test.go measures that move and pins all
        // three settings. See client/js/12-scene-geometry-winding.test.mjs for
        // the winding numbers.
        var selenaPipelineOptions = obj.doubleSided ? { cullMode: "none" } : null;
        var selenaResource = isSkinned
          ? getSelenaSkinnedPipeline(mat, blendMode, depthWrite, selenaPipelineOptions)
          : getSelenaPipeline(mat, blendMode, depthWrite, selenaPipelineOptions);
        if (selenaResource) {
          var selenaKey = "selena:" + (isSkinned ? "skin:" : "") + (mat && mat.key || matIndex) + (obj.doubleSided ? ":ds" : "");
          if (currentPipelineKind !== selenaKey) {
            pass.setPipeline(selenaResource.pipeline);
            currentPipelineKind = selenaKey;
          }
          var selenaBG = createSelenaBindGroup(mat, selenaResource, obj);
          if (selenaBG) {
            pass.setBindGroup(0, selenaBG);
            if (isSkinned) {
              // Skinned positions live in the compute-pass output buffer; bind via
              // the shared 4-slot skinned binding (slot0=skinned pos, 1-3=base).
              if (webGPUBindElioSkinnedBuffers(pass, obj, count)) {
                var selenaSkinIndexCount = webGPUBindRetainedMeshIndexBuffer(pass, obj);
                if (selenaSkinIndexCount > 0) pass.drawIndexed(selenaSkinIndexCount);
                else pass.draw(count);
                if (stats) stats.meshDrawCalls = (stats.meshDrawCalls || 0) + 1;
              }
              continue;
            }
            for (var ai = 0; ai < selenaResource.attrs.length; ai++) {
              bindMeshAttribute(selenaResource.attrs[ai], obj, offset, count);
            }
            pass.draw(count);
            if (stats) stats.meshDrawCalls = (stats.meshDrawCalls || 0) + 1;
            continue;
          }
        }

        if (isSkinned) {
          bindPBRPipeline();
          var skinnedOwner = mat || obj;
          if (matIndex !== lastMaterialIndex || receiveShadow !== lastReceiveShadow || skinnedOwner !== lastMaterialOwner) {
            var skinnedMatBG = createMaterialBindGroup(mat, receiveShadow, mat || obj);
            pass.setBindGroup(1, skinnedMatBG);
            lastMaterialIndex = matIndex;
            lastReceiveShadow = receiveShadow;
            lastMaterialOwner = skinnedOwner;
          }
          if (webGPUBindElioSkinnedBuffers(pass, obj, count)) {
            var skinnedPBRIndexCount = webGPUBindRetainedMeshIndexBuffer(pass, obj);
            if (skinnedPBRIndexCount > 0) pass.drawIndexed(skinnedPBRIndexCount);
            else pass.draw(count);
            if (stats) stats.meshDrawCalls = (stats.meshDrawCalls || 0) + 1;
          }
          continue;
        }

        bindPBRPipeline();

        // Recreate material bind group when material or receiveShadow changes.
        var materialOwner = obj.retainedGeometry ? webGPURetainedMaterialOwner(obj) : (mat || obj);
        if (matIndex !== lastMaterialIndex || receiveShadow !== lastReceiveShadow || materialOwner !== lastMaterialOwner) {
          var matBG = createMaterialBindGroup(
            mat,
            receiveShadow,
            materialOwner,
            obj.retainedGeometry ? webGPUObjectModelMatrix(obj) : null,
            obj.retainedGeometry ? obj.modelScaleSigns : null
          );
          pass.setBindGroup(1, matBG);
          lastMaterialIndex = matIndex;
          lastReceiveShadow = receiveShadow;
          lastMaterialOwner = materialOwner;
        }

        if (computedMorphRecord) {
          if (!webGPUBindComputedMorphBuffer(pass, 0, computedMorphRecord.positionBuffer, count, 3)) continue;
          if (!webGPUBindComputedMorphBuffer(pass, 1, computedMorphRecord.normalBuffer, count, 3)) continue;
          if (!webGPUBindSceneMeshVertexBuffer(pass, 2, pbrBuffers && pbrBuffers.uvs, offset, count)) continue;
          if (!webGPUBindComputedMorphBuffer(pass, 3, computedMorphRecord.tangentBuffer, count, 4)) continue;
          pass.draw(count);
          if (stats) stats.meshDrawCalls = (stats.meshDrawCalls || 0) + 1;
          continue;
        }

        if (obj.retainedGeometry) {
          if (!webGPUBindRetainedMeshAttribute(pass, 0, obj, "positions", 3)) continue;
          if (!webGPUBindRetainedMeshAttribute(pass, 1, obj, "normals", 3)) continue;
          if (!webGPUBindRetainedMeshAttribute(pass, 2, obj, "uvs", 2)) continue;
          if (!webGPUBindRetainedMeshAttribute(pass, 3, obj, "tangents", 4)) continue;
          var pbrIndexCount = webGPUBindRetainedMeshIndexBuffer(pass, obj);
          if (pbrIndexCount > 0) {
            pass.drawIndexed(pbrIndexCount);
          } else {
            pass.draw(count);
          }
          if (stats) stats.meshDrawCalls = (stats.meshDrawCalls || 0) + 1;
          continue;
        }

        if (!webGPUBindSceneMeshVertexBuffer(pass, 0, pbrBuffers && pbrBuffers.positions, offset, count)) continue;
        if (!webGPUBindSceneMeshVertexBuffer(pass, 1, pbrBuffers && pbrBuffers.normals, offset, count)) continue;
        if (!webGPUBindSceneMeshVertexBuffer(pass, 2, pbrBuffers && pbrBuffers.uvs, offset, count)) continue;
        if (!webGPUBindSceneMeshVertexBuffer(pass, 3, pbrBuffers && pbrBuffers.tangents, offset, count)) continue;

        pass.draw(count);
        if (stats) stats.meshDrawCalls = (stats.meshDrawCalls || 0) + 1;
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

    function drawInstancedMeshes(pass, meshList, materials, blendMode, depthWrite) {
      for (var i = 0; i < meshList.length; i++) {
        var mesh = meshList[i];
        var instanceCount = instancedMeshCount(mesh);
        var transformData = instancedMeshTransformData(mesh, instanceCount);
        if (!transformData) continue;

        var geom = getInstancedGeometry(mesh);
        if (!geom || geom.vertexCount <= 0) continue;

        var mat = instancedMeshMaterial(mesh, materials);
        pass.setBindGroup(1, createMaterialBindGroup(mat, !!mesh.receiveShadow, mesh));

        // Indirect draw via GPU cull (D3: ready cull record → drawIndirect;
        // not-ready / no kernel / capability absent → draw-all).
        var meshId = (typeof mesh.id === "string" && mesh.id) ? mesh.id : ("mesh-" + i);
        var hasCullWGSL = (typeof mesh.cullKernelWGSL === "string" && mesh.cullKernelWGSL.trim().length > 0);
        var cullRecord = (hasCullWGSL || webGPUBuiltinCullEligible(mesh)) ? instancedCullSystems.get(meshId) : null;
        var cullSys = cullRecord && cullRecord.system;

        if (cullSys && cullSys.isReady()) {
          // GPU-culled path: slot 4 = outputBuf (80B InstanceRecord, cull layout).
          // Use the cull pipeline (loc 8 = pickData vec4u) instead of the
          // standard pipeline (loc 8 = instanceColor vec4f).
          pass.setPipeline(getPBRInstancedCullPipeline(blendMode, depthWrite));
          pass.setVertexBuffer(0, ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedPositionBuffer", geom.positions));
          pass.setVertexBuffer(1, ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedNormalBuffer", geom.normals));
          pass.setVertexBuffer(2, ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedUVBuffer", geom.uvs));
          pass.setVertexBuffer(3, ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedTangentBuffer", geom.tangents));
          pass.setVertexBuffer(4, cullSys.outputBuf);
          pass.drawIndirect(cullSys.drawArgsBuf, 0);
        } else {
          // Draw-all path (not-ready, no kernel, or capability absent).
          pass.setPipeline(getPBRInstancedPipeline(blendMode, depthWrite));
          pass.setVertexBuffer(0, ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedPositionBuffer", geom.positions));
          pass.setVertexBuffer(1, ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedNormalBuffer", geom.normals));
          pass.setVertexBuffer(2, ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedUVBuffer", geom.uvs));
          pass.setVertexBuffer(3, ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedTangentBuffer", geom.tangents));
          pass.setVertexBuffer(4, ensureInstancedTransformGPUBuffer(mesh, transformData));
          pass.setVertexBuffer(5, ensureInstancedColorGPUBuffer(mesh, instancedMeshColorData(mesh, instanceCount)));
          pass.draw(geom.vertexCount, instanceCount);
        }
      }
    }

    // webGPUInstancedCullRadius returns a bounding-sphere radius that encloses
    // the mesh's UNSCALED local geometry, padded by 5%. The cull kernel scales
    // it per instance, so this value must never under-estimate the primitive.
    // The pad covers the torus-knot envelope, which instancedLocalBounds
    // approximates.
    function webGPUInstancedCullRadius(mesh) {
      var b = instancedLocalBounds(mesh);
      if (!b) return 2;
      var hx = Math.max(Math.abs(b.minX), Math.abs(b.maxX));
      var hy = Math.max(Math.abs(b.minY), Math.abs(b.maxY));
      var hz = Math.max(Math.abs(b.minZ), Math.abs(b.maxZ));
      var radius = Math.sqrt(hx * hx + hy * hy + hz * hz) * 1.05;
      return radius > 0 ? radius : 2;
    }

    // webGPUBuiltinCullEligible decides whether a mesh with no authored cull
    // kernel may use the renderer's own kernel. Three conditions, each of them a
    // correctness or a payoff limit:
    //
    //   1. The page has not opted out.
    //   2. The mesh carries no per-instance colours. The culled draw path binds
    //      the compacted 80-byte record, whose last vec4 is pick data, not
    //      colour, so a coloured mesh would lose its colours.
    //   3. The instance count is high enough that one compute dispatch and one
    //      indirect draw beat a plain draw-all.
    function webGPUBuiltinCullEligible(mesh) {
      if (!mesh) return false;
      if (typeof window !== "undefined" && window.__gosx_scene3d_webgpu_builtin_cull === false) return false;
      var colors = mesh.colors;
      if (colors && typeof colors.length === "number" && colors.length > 0) return false;
      return instancedMeshCount(mesh) >= SCENE_WEBGPU_BUILTIN_CULL_MIN_INSTANCES;
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
      if (kind === "torusknot") {
        // (p=2,q=3): major envelope ≈ radius*(2+1)*0.5 + tube = 1.5*radius + tube
        var knMajor = Math.max(0.0001, sceneNumber(mesh.radius, 0.17));
        var knTube = Math.max(0.0001, sceneNumber(mesh.tube, 0.045));
        var knEnvelope = knMajor * 1.5 + knTube;
        var knHeight = knMajor * 0.5 + knTube;
        return { minX: -knEnvelope, minY: -knHeight, minZ: -knEnvelope, maxX: knEnvelope, maxY: knHeight, maxZ: knEnvelope };
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
        pointAuthoredDrawEntries: 0,
        pointAuthoredDrawInstances: 0,
        pointAuthoredDrawCalls: 0,
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

        // Select pipeline: authored (async-validated) when shader fields present,
        // else builtin instanced-vertex pipeline.
        var blendMode = typeof entry.blendMode === "string" ? entry.blendMode.toLowerCase() : "";
        var depthWrite = entry.depthWrite !== false;
        var validBlend = blendMode === "additive" || blendMode === "alpha" ? blendMode : "opaque";

        // Truthiness only — the pipeline builder trims and validates once,
        // memoized on the entry. Trimming ~6 KB WGSL strings here allocated
        // per layer per frame.
        var hasAuthoredWGSL = (typeof entry.customVertexWGSL === "string" && entry.customVertexWGSL) &&
                              (typeof entry.customFragmentWGSL === "string" && entry.customFragmentWGSL);
        var layerID = entry.id || ("points-" + i);
        var authoredResource = hasAuthoredWGSL && !pointsAuthoredLayerFailed.get(layerID)
          ? buildAuthoredPointsVertexPipelineAsync(entry, validBlend, depthWrite, layerID)
          : null;
        var useAuthored = authoredResource && !authoredResource.failed && !authoredResource.pending && authoredResource.pipeline;

        var pointsBG, pipeline;
        if (useAuthored) {
          // Authored path: bind group 1 = user uniforms, group 2 = PointsUniforms.
          var userUnifBuf = ensurePointsAuthoredUserUniformBuffer(entry, "_gosxWGPUPointsUserUniform", entry.customUniforms, entry.shaderLayout);
          var userUnifBG = wgpuCachedBindGroup(entry, "_gosxWGPUPointsUserUniformBG", pointsAuthoredUserUniformBGL, [
            { binding: 0, resource: { buffer: userUnifBuf } },
          ]);
          pointsBG = wgpuCachedBindGroup(entry, "_gosxWGPUPointsUniformBG", pointsUniformBindGroupLayout, [
            { binding: 0, resource: { buffer: pointsUniformBuffer } },
          ]);
          pipeline = authoredResource.pipeline;
          pass.setPipeline(pipeline);
          pass.setVertexBuffer(0, pointsParticleBuffer);
          pass.setBindGroup(1, userUnifBG);
          pass.setBindGroup(2, pointsBG);
        } else {
          // Builtin path.
          pointsBG = wgpuCachedBindGroup(entry, "_gosxWGPUPointsUniformBG", pointsUniformBindGroupLayout, [
            { binding: 0, resource: { buffer: pointsUniformBuffer } },
          ]);
          pipeline = getPointsVertexPipeline(validBlend, depthWrite);
          pass.setPipeline(pipeline);
          pass.setVertexBuffer(0, pointsParticleBuffer);
          pass.setBindGroup(1, createMaterialBindGroup(null, false, defaultMaterialOwner));
          pass.setBindGroup(2, pointsBG);
        }
        pass.draw(6, count); // 6 vertices per quad, count instances
        stats.pointDrawEntries += 1;
        stats.pointDrawInstances += count;
        stats.pointDrawCalls += 1;
        if (useAuthored) {
          stats.pointAuthoredDrawEntries += 1;
          stats.pointAuthoredDrawInstances += count;
          stats.pointAuthoredDrawCalls += 1;
        }
      }
      return stats;
    }

    function drawComputeParticleEntries(pass, records, environment, timeSeconds) {
      var stats = {
        computeParticleDrawEntries: 0,
        computeParticleDrawInstances: 0,
        computeParticleDrawCalls: 0,
        computeParticleAuthoredDrawEntries: 0,
        computeParticleAuthoredDrawInstances: 0,
        computeParticleAuthoredDrawCalls: 0,
        computeParticleAuthoredCandidateEntries: 0,
        computeParticleAuthoredPendingEntries: 0,
        computeParticleAuthoredFailedEntries: 0,
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

        var blendMode = typeof material.blendMode === "string" ? material.blendMode.toLowerCase() : "";
        var validBlend = blendMode === "additive" || blendMode === "alpha" ? blendMode : "opaque";
        var depthWrite = entry.depthWrite === true || (validBlend === "opaque" && entry.depthWrite !== false);

        // Authored render path: check for renderVertexWGSL/renderFragmentWGSL
        // on the entry. Truthiness only — the pipeline builder trims and
        // validates once, memoized on the entry.
        var hasAuthoredRender = (typeof entry.renderVertexWGSL === "string" && entry.renderVertexWGSL) &&
                                (typeof entry.renderFragmentWGSL === "string" && entry.renderFragmentWGSL);
        var cpSystemID = (entry && typeof entry.id === "string") ? entry.id : ("cp-" + i);
        if (hasAuthoredRender) {
          stats.computeParticleAuthoredCandidateEntries += 1;
        }
        if (hasAuthoredRender && pointsAuthoredLayerFailed.get(cpSystemID)) {
          stats.computeParticleAuthoredFailedEntries += 1;
        }
        var cpAuthoredResource = hasAuthoredRender && !pointsAuthoredLayerFailed.get(cpSystemID)
          ? buildAuthoredParticleRenderPipelineAsync(entry, validBlend, depthWrite, cpSystemID)
          : null;
        if (cpAuthoredResource && cpAuthoredResource.pending) {
          stats.computeParticleAuthoredPendingEntries += 1;
        }
        var useCPAuthored = cpAuthoredResource && !cpAuthoredResource.failed && !cpAuthoredResource.pending && cpAuthoredResource.pipeline;

        var pipeline, pointsBG;
        if (useCPAuthored) {
          // Authored render: group 1 = user uniforms, group 2 = PointsUniforms + particles storage.
          var cpUserUnifBuf = ensurePointsAuthoredUserUniformBuffer(system, "_gosxWGPUCPRenderUserUniform", entry.renderUniforms, entry.renderShaderLayout);
          var cpUserUnifBG = wgpuCachedBindGroup(system, "_gosxWGPUCPRenderUserUniformBG", pointsAuthoredUserUniformBGL, [
            { binding: 0, resource: { buffer: cpUserUnifBuf } },
          ]);
          pointsBG = wgpuCachedBindGroup(system, "_gosxWGPUCPPointsBG", pointsBindGroupLayout, [
            { binding: 0, resource: { buffer: pointsUniformBuffer } },
            { binding: 1, resource: { buffer: system.renderBuffer } },
          ]);
          pipeline = cpAuthoredResource.pipeline;
          pass.setPipeline(pipeline);
          pass.setBindGroup(1, cpUserUnifBG);
          pass.setBindGroup(2, pointsBG);
        } else {
          // Builtin render path.
          pointsBG = wgpuCachedBindGroup(system, "_gosxWGPUCPPointsBG", pointsBindGroupLayout, [
            { binding: 0, resource: { buffer: pointsUniformBuffer } },
            { binding: 1, resource: { buffer: system.renderBuffer } },
          ]);
          pipeline = getPointsPipeline(validBlend, depthWrite);
          pass.setPipeline(pipeline);
          pass.setBindGroup(1, createMaterialBindGroup(null, false, defaultMaterialOwner));
          pass.setBindGroup(2, pointsBG);
        }
        pass.draw(6, system.count);
        stats.computeParticleDrawEntries += 1;
        stats.computeParticleDrawInstances += system.count;
        stats.computeParticleDrawCalls += 1;
        if (useCPAuthored) {
          stats.computeParticleAuthoredDrawEntries += 1;
          stats.computeParticleAuthoredDrawInstances += system.count;
          stats.computeParticleAuthoredDrawCalls += 1;
        }
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
      mount.__gosxScene3DWebGPUProof = {
        frameSeq: published.frameSeq || 0,
        frameAt: published.frameAt || 0,
        waterSystems: published.waterSystems || 0,
        waterComputeDispatches: published.waterComputeDispatches || 0,
        waterDrawCalls: published.waterDrawCalls || 0,
        waterSimulationTickSeq: published.waterSimulationTickSeq || 0,
        waterSolverSubstepSeq: published.waterSolverSubstepSeq || 0,
        waterDroppedTicks: published.waterDroppedTicks || 0,
        waterNormalDispatchSeq: published.waterNormalDispatchSeq || 0,
        waterSampledStateSyncSeq: published.waterSampledStateSyncSeq || 0,
        waterAtRestSystems: published.waterAtRestSystems || 0,
        waterQualityTier: published.waterQualityTier || "full",
        waterQualityRevision: published.waterQualityRevision || 0,
        waterSurfaceResolution: published.waterSurfaceResolution || 0,
        waterCausticsResolution: published.waterActiveCausticsResolution || 0,
        waterObjectShadowResolution: published.waterActiveObjectShadowResolution || 0,
        waterObjectTextureWidth: published.waterActiveObjectTextureWidth || 0,
        waterObjectTextureHeight: published.waterActiveObjectTextureHeight || 0,
        waterObjectTexturePixelBudget: published.waterActiveObjectTexturePixelBudget || 0,
        waterQualityAllocationPending: published.waterQualityAllocationPending || 0,
        waterQualityAllocationFailures: published.waterQualityAllocationFailures || 0,
        waterQualityAllocationRetryFrame: published.waterQualityAllocationRetryFrame || 0,
        waterDPRCap: published.waterQualityDPRCap || 1,
        waterExpensivePassCadence: published.waterExpensivePassCadence || 1,
      };
      if (typeof mount.setAttribute !== "function") return;
      // Lifecycle and simulation proof must remain prompt for health checks and
      // E2E. This is a bounded attribute surface instead of ~150 writes.
      function setEssentialAttribute(name, value) {
        if (webGPUEssentialAttributeCache[name] === value) return;
        webGPUEssentialAttributeCache[name] = value;
        mount.setAttribute(name, value);
      }
      setEssentialAttribute("data-gosx-scene3d-webgpu-frame-seq", String(published.frameSeq || 0));
      setEssentialAttribute("data-gosx-scene3d-webgpu-frame-at", String(published.frameAt || 0));
      setEssentialAttribute("data-gosx-scene3d-webgpu-water-systems", String(published.waterSystems || 0));
      setEssentialAttribute("data-gosx-scene3d-webgpu-water-compute-dispatches", String(published.waterComputeDispatches || 0));
      setEssentialAttribute("data-gosx-scene3d-webgpu-water-draw-calls", String(published.waterDrawCalls || 0));
      setEssentialAttribute("data-gosx-scene3d-webgpu-water-tick-seq", String(published.waterSimulationTickSeq || 0));
      setEssentialAttribute("data-gosx-scene3d-webgpu-water-substep-seq", String(published.waterSolverSubstepSeq || 0));
      setEssentialAttribute("data-gosx-scene3d-webgpu-water-dropped-ticks", String(published.waterDroppedTicks || 0));
      setEssentialAttribute("data-gosx-scene3d-webgpu-water-normal-seq", String(published.waterNormalDispatchSeq || 0));
      setEssentialAttribute("data-gosx-scene3d-webgpu-water-state-sync-seq", String(published.waterSampledStateSyncSeq || 0));
      // M5 at-rest gating (water-parity-campaign): a per-frame count of water
      // systems currently parked (skipping sim substeps/normal/state-copy/
      // caustics, retaining last-rendered textures). Essential-tier (not
      // throttled behind the diagnostic interval) so diag overlays and e2e
      // waitFor() polls observe a rest/wake transition promptly -- see
      // WATER_REST_ENERGY_EPSILON's comment above updateWaterSystems.
      setEssentialAttribute("data-gosx-scene3d-webgpu-water-at-rest-systems", String(published.waterAtRestSystems || 0));
      // Render-bundle state is essential tier: a tool that measures the saving
      // must be able to prove a replay happened on the frame it timed, and a
      // stale image is diagnosed by reading the reason.
      setEssentialAttribute("data-gosx-scene3d-webgpu-bundle-state", String(published.bundleState || "direct"));
      setEssentialAttribute("data-gosx-scene3d-webgpu-bundle-reason", String(published.bundleReason || ""));
      setEssentialAttribute("data-gosx-scene3d-webgpu-bundle-encodes", String(published.bundleEncodes || 0));
      setEssentialAttribute("data-gosx-scene3d-webgpu-bundle-replays", String(published.bundleReplays || 0));
      setEssentialAttribute("data-gosx-scene3d-webgpu-bundle-draws", String(published.bundleDraws || 0));
      setEssentialAttribute("data-gosx-scene3d-retained-mesh-objects", String(published.retainedMeshObjects || 0));
      setEssentialAttribute("data-gosx-scene3d-retained-mesh-vertices", String(published.retainedMeshVertices || 0));
      setEssentialAttribute("data-gosx-scene3d-world-baked-mesh-objects", String(published.worldBakedMeshObjects || 0));
      setEssentialAttribute("data-gosx-scene3d-world-baked-mesh-vertices", String(published.worldBakedMeshVertices || 0));
      setEssentialAttribute("data-gosx-scene3d-retained-cache-entries", String(published.retainedCacheEntries || 0));
      setEssentialAttribute("data-gosx-scene3d-retained-cache-hits", String(published.retainedCacheHits || 0));
      setEssentialAttribute("data-gosx-scene3d-retained-cache-misses", String(published.retainedCacheMisses || 0));
      setEssentialAttribute("data-gosx-scene3d-retained-upload-bytes", String(published.retainedUploadBytes || 0));
      setEssentialAttribute("data-gosx-scene3d-retained-allocations", String(published.retainedAllocations || 0));
      setEssentialAttribute("data-gosx-scene3d-retained-rebuilds", String(published.retainedRebuilds || 0));
      setEssentialAttribute("data-gosx-scene3d-retained-retirements", String(published.retainedRetirements || 0));
      setEssentialAttribute("data-gosx-scene3d-retained-live-bytes", String(published.retainedLiveBytes || 0));
      setEssentialAttribute("data-gosx-scene3d-bundle-build-cpu-ms", String(published.bundleBuildCPUms || 0));
      setEssentialAttribute("data-gosx-scene3d-planner-cpu-ms", String(published.plannerCPUms || 0));
      setEssentialAttribute("data-gosx-scene3d-planner-full-vertex-hash-scans", String(published.plannerFullVertexHashScans || 0));
      // GPU cull dispatch counters. skipped-dispatches proves the static-scene
      // fingerprint skip fired.
      setEssentialAttribute("data-gosx-scene3d-webgpu-cull-dispatches", String(published.cullDispatches || 0));
      setEssentialAttribute("data-gosx-scene3d-webgpu-cull-skipped-dispatches", String(published.cullSkippedDispatches || 0));
      setEssentialAttribute("data-gosx-scene3d-webgpu-cull-builtin-systems", String(published.cullBuiltinSystems || 0));
      // Which precision the post chain compiled with. "f16" means the device
      // negotiated shader-f16 and the blur and FXAA stages run half precision.
      setEssentialAttribute("data-gosx-scene3d-webgpu-post-precision", String(published.postPrecision || "none"));
      setEssentialAttribute("data-gosx-scene3d-webgpu-post-dom-region-bounded-passes", String(published.postDOMRegionBoundedPasses || 0));
      setEssentialAttribute("data-gosx-scene3d-webgpu-post-dom-region-bounded-skips", String(published.postDOMRegionBoundedSkips || 0));
      setEssentialAttribute("data-gosx-scene3d-webgpu-post-dom-region-bounded-pixels", String(published.postDOMRegionBoundedPixels || 0));
      if (published.lastError) {
        setEssentialAttribute("data-gosx-scene3d-webgpu-last-error", String(published.lastError));
      } else if (webGPUEssentialAttributeCache["data-gosx-scene3d-webgpu-last-error"] !== null) {
        webGPUEssentialAttributeCache["data-gosx-scene3d-webgpu-last-error"] = null;
        mount.removeAttribute("data-gosx-scene3d-webgpu-last-error");
      }
      var telemetryConfig = typeof window !== "undefined" && window.__gosx_telemetry_config && typeof window.__gosx_telemetry_config === "object"
        ? window.__gosx_telemetry_config : null;
      var verboseTelemetry = typeof window !== "undefined" && (
        window.__gosx_scene3d_webgpu_telemetry === true ||
        window.__gosx_scene3d_cull_telemetry === true ||
        (telemetryConfig && (telemetryConfig.scene3dDiagnostics === true || telemetryConfig.scene3dPerfTelemetry === true))
      );
      var diagnosticElapsed = lastWebGPUDiagnosticAttributeAt == null
        ? Infinity : published.frameAt - lastWebGPUDiagnosticAttributeAt;
      var mirrorDiagnostics = verboseTelemetry || diagnosticElapsed < 0 || diagnosticElapsed >= WEBGPU_DIAGNOSTIC_ATTRIBUTE_INTERVAL_MS;
      if (!mirrorDiagnostics) return;
      lastWebGPUDiagnosticAttributeAt = published.frameAt;
      mount.setAttribute("data-gosx-scene3d-webgpu-point-entries", String(published.pointEntries || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-point-instances", String(published.pointInstances || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-point-draw-entries", String(published.pointDrawEntries || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-point-draw-instances", String(published.pointDrawInstances || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-point-draw-calls", String(published.pointDrawCalls || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-point-authored-draw-entries", String(published.pointAuthoredDrawEntries || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-point-authored-draw-instances", String(published.pointAuthoredDrawInstances || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-point-authored-draw-calls", String(published.pointAuthoredDrawCalls || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-point-skipped-empty", String(published.pointSkippedEmpty || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-point-skipped-no-positions", String(published.pointSkippedNoPositions || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-entries", String(published.computeParticleEntries || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-instances", String(published.computeParticleInstances || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-draw-entries", String(published.computeParticleDrawEntries || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-draw-instances", String(published.computeParticleDrawInstances || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-draw-calls", String(published.computeParticleDrawCalls || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-authored-draw-entries", String(published.computeParticleAuthoredDrawEntries || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-authored-draw-instances", String(published.computeParticleAuthoredDrawInstances || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-authored-draw-calls", String(published.computeParticleAuthoredDrawCalls || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-authored-candidate-entries", String(published.computeParticleAuthoredCandidateEntries || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-authored-pending-entries", String(published.computeParticleAuthoredPendingEntries || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-authored-failed-entries", String(published.computeParticleAuthoredFailedEntries || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-skipped-empty", String(published.computeParticleSkippedEmpty || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-compute-particle-skipped-not-ready", String(published.computeParticleSkippedNotReady || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-systems", String(published.waterSystems || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-cells", String(published.waterCells || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-vertices", String(published.waterVertices || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-compute-dispatches", String(published.waterComputeDispatches || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-compute-systems", String(published.waterAuthoredComputeSystems || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-compute-dispatches", String(published.waterAuthoredComputeDispatches || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-compute-fallbacks", String(published.waterAuthoredComputeFallbacks || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-selena-compute-systems", String(published.waterSelenaComputeSystems || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-selena-compute-dispatches", String(published.waterSelenaComputeDispatches || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-selena-compute-fallbacks", String(published.waterSelenaComputeFallbacks || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-drop-dispatches", String(published.waterDropDispatches || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-drop-dispatch-total", String(published.waterDropDispatchTotal || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-drop-event", String(published.waterLastDropEventID || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-systems", String(published.waterObjectSystems || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-dispatches", String(published.waterObjectDispatches || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-event-dispatches", String(published.waterObjectEventDispatches || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-event", String(published.waterLastObjectDisplacementEventID || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-spheres", String(published.waterObjectSpheres || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-rounded-systems", String(published.waterRoundedSystems || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-corner-radius", String(published.waterCornerRadius || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-light-dir-x", String(published.waterLightDirX || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-light-dir-y", String(published.waterLightDirY || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-light-dir-z", String(published.waterLightDirZ || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-rest-substeps-skipped", String(published.waterRestSubstepsSkipped || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-uniform-uploads", String(published.waterUniformUploads || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-uniform-uploads-skipped", String(published.waterUniformUploadsSkipped || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-caustic-systems", String(published.waterCausticSystems || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-caustic-passes", String(published.waterCausticPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-caustic-texture-pixels", String(published.waterCausticTexturePixels || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-caustic-systems", String(published.waterAuthoredCausticSystems || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-caustic-passes", String(published.waterAuthoredCausticPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-caustic-fallbacks", String(published.waterAuthoredCausticFallbacks || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-caustic-fallback-reason", String(published.waterAuthoredCausticFallbackReason || ""));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-caustic-source-bytes", String(published.waterAuthoredCausticSourceBytes || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-entry-caustic-source-bytes", String(published.waterEntryCausticSourceBytes || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-resolved-caustic-source-bytes", String(published.waterResolvedCausticSourceBytes || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-surface-source-bytes", String(published.waterAuthoredSurfaceSourceBytes || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-entry-surface-source-bytes", String(published.waterEntrySurfaceSourceBytes || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-resolved-surface-source-bytes", String(published.waterResolvedSurfaceSourceBytes || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-manifest-shader-systems", String(published.waterManifestShaderSystems || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-manifest-shader-fields", String(published.waterManifestShaderFields || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-manifest-caustic-source-bytes", String(published.waterManifestCausticSourceBytes || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-manifest-surface-source-bytes", String(published.waterManifestSurfaceSourceBytes || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-bundle-shader-systems", String(published.waterBundleShaderSystems || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-bundle-caustic-source-bytes", String(published.waterBundleCausticSourceBytes || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-bundle-surface-source-bytes", String(published.waterBundleSurfaceSourceBytes || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-texture-passes", String(published.waterObjectTexturePasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-texture-targets", String(published.waterObjectTextureTargets || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-texture-pixels", String(published.waterObjectTexturePixels || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-texture-width", String(published.waterObjectTextureWidth || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-texture-height", String(published.waterObjectTextureHeight || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-texture-pixel-budget", String(published.waterObjectTexturePixelBudget || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-expensive-pass-cadence", String(published.waterExpensivePassCadence || 1));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-quality-allocation-pending", String(published.waterQualityAllocationPending || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-quality-allocation-failures", String(published.waterQualityAllocationFailures || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-quality-allocation-retry-frame", String(published.waterQualityAllocationRetryFrame || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-texture-mesh-passes", String(published.waterObjectTextureMeshPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-texture-mesh-draw-calls", String(published.waterObjectTextureMeshDrawCalls || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-texture-selena-draw-calls", String(published.waterObjectTextureSelenaDrawCalls || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-texture-fallback-passes", String(published.waterObjectTextureFallbackPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-texture-candidate-objects", String(published.waterObjectTextureCandidateObjects || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-texture-selected-objects", String(published.waterObjectTextureSelectedObjects || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-texture-fallback-missing-objects", String(published.waterObjectTextureFallbackMissingObjects || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-texture-fallback-missing-resources", String(published.waterObjectTextureFallbackMissingResources || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-texture-candidate-profile", String(published.waterObjectTextureCandidateProfile || ""));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-shadow-passes", String(published.waterObjectShadowPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-shadow-texture-pixels", String(published.waterObjectShadowTexturePixels || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-shadow-mesh-passes", String(published.waterObjectShadowMeshPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-shadow-mesh-draw-calls", String(published.waterObjectShadowMeshDrawCalls || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-object-shadow-passes", String(published.waterAuthoredObjectShadowPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-object-shadow-fallbacks", String(published.waterAuthoredObjectShadowFallbacks || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-object-mesh-shadow-passes", String(published.waterAuthoredObjectMeshShadowPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-object-mesh-shadow-fallbacks", String(published.waterAuthoredObjectMeshShadowFallbacks || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-shadow-fallback-passes", String(published.waterObjectShadowFallbackPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-shadow-fallback-missing-objects", String(published.waterObjectShadowFallbackMissingObjects || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-shadow-fallback-missing-resources", String(published.waterObjectShadowFallbackMissingResources || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-reflection-systems", String(published.waterReflectionSystems || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-refraction-systems", String(published.waterRefractionSystems || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-object-optics-systems", String(published.waterObjectOpticsSystems || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-pool-passes", String(published.waterPoolPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-pool-draw-calls", String(published.waterPoolDrawCalls || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-pool-draw-vertices", String(published.waterPoolDrawVertices || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-pool-tile-texture-loaded", String(published.waterPoolTileTextureLoaded || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-pool-tile-texture-fallbacks", String(published.waterPoolTileTextureFallbacks || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-pool-tile-texture-pending", String(published.waterPoolTileTexturePending || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-pool-tile-texture-failed", String(published.waterPoolTileTextureFailed || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-pool-passes", String(published.waterAuthoredPoolPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-pool-vertex-passes", String(published.waterAuthoredPoolVertexPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-pool-fragment-passes", String(published.waterAuthoredPoolFragmentPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-pool-fallbacks", String(published.waterAuthoredPoolFallbacks || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-selena-pool-passes", String(published.waterSelenaPoolPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-selena-pool-fallbacks", String(published.waterSelenaPoolFallbacks || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-selena-surface-passes", String(published.waterSelenaSurfacePasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-selena-surface-fallbacks", String(published.waterSelenaSurfaceFallbacks || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-selena-caustic-passes", String(published.waterSelenaCausticPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-selena-caustic-fallbacks", String(published.waterSelenaCausticFallbacks || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-selena-object-shadow-passes", String(published.waterSelenaObjectShadowPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-selena-object-shadow-fallbacks", String(published.waterSelenaObjectShadowFallbacks || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-selena-object-mesh-shadow-passes", String(published.waterSelenaObjectMeshShadowPasses || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-selena-object-mesh-shadow-fallbacks", String(published.waterSelenaObjectMeshShadowFallbacks || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-draw-entries", String(published.waterDrawEntries || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-draw-vertices", String(published.waterDrawVertices || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-surface-mesh-resolution", String(published.waterSurfaceMeshResolution || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-draw-calls", String(published.waterDrawCalls || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-surface-above-draw-calls", String(published.waterSurfaceAboveDrawCalls || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-surface-above-draw-vertices", String(published.waterSurfaceAboveDrawVertices || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-surface-below-draw-calls", String(published.waterSurfaceBelowDrawCalls || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-surface-below-draw-vertices", String(published.waterSurfaceBelowDrawVertices || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-surface-systems", String(published.waterAuthoredSurfaceSystems || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-surface-draw-calls", String(published.waterAuthoredSurfaceDrawCalls || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-surface-vertex-draw-calls", String(published.waterAuthoredSurfaceVertexDrawCalls || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-surface-pending-draw-calls", String(published.waterAuthoredSurfacePendingDrawCalls || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-surface-fallbacks", String(published.waterAuthoredSurfaceFallbacks || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-authored-surface-fallback-reason", String(published.waterAuthoredSurfaceFallbackReason || ""));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-sky-cube-texture-loaded", String(published.waterSkyCubeTextureLoaded || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-sky-cube-texture-fallbacks", String(published.waterSkyCubeTextureFallbacks || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-sky-cube-texture-pending", String(published.waterSkyCubeTexturePending || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-water-sky-cube-texture-failed", String(published.waterSkyCubeTextureFailed || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-mesh-objects", String(published.meshObjects || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-mesh-draw-calls", String(published.meshDrawCalls || 0));
      mount.setAttribute("data-gosx-scene3d-webgpu-mesh-view-culled", String(published.meshViewCulled || 0));
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
      // Render-truth surface: backend-neutral attribute names both renderers
      // write, so probes and deploy gates never branch on which backend won.
      // Gated on the diagnostics tier -- when it is off nothing here runs and
      // production pays a single boolean read per diagnostic interval.
      var truthApi = renderTruth();
      if (truthApi.enabled()) {
        truthApi.publish(mount, {
          backend: "webgpu",
          postChain: published.postChain,
          meshSubmitted: published.meshObjects || 0,
          meshDrawn: published.meshDrawCalls || 0,
          meshViewCulled: published.meshViewCulled || 0,
          meshUndrawable: published.meshUndrawable || 0,
          pointsSubmitted: published.pointEntries || 0,
          pointsDrawn: published.pointDrawEntries || 0,
          pointInstancesSubmitted: published.pointInstances || 0,
          pointInstancesDrawn: published.pointDrawInstances || 0,
          uniformTime: selenaFrame.time,
          adapterInfo: webGPUAdapterInfoSnapshot(),
        });
      }
      // Cull survivor telemetry: written when __gosx_scene3d_cull_telemetry is
      // enabled; removed otherwise so the attribute is absent in production.
      if (lastCullSurvivors !== null) {
        mount.setAttribute("data-gosx-scene3d-cull-survivors", lastCullSurvivors);
      } else {
        mount.removeAttribute("data-gosx-scene3d-cull-survivors");
      }
    }

    function isWebGPUErrorScopeLifecycleMessage(message) {
      var text = String(message || "").toLowerCase();
      return text.indexOf("instance dropped") >= 0 && text.indexOf("poperrorscope") >= 0;
    }

    function reportWebGPUFrameError(message) {
      var text = String(message || "").slice(0, 500);
      if (!text) return;
      // Unlike webGPUErrorReportCount below (capped at 3 lifetime emits so a
      // persistently-erroring session doesn't spam window.__gosx_emit
      // forever), webGPUConsecutiveFrameErrors is UNCAPPED and reset to 0 on
      // the next clean frame (see endWebGPUErrorScope) — it's what the
      // mount-level resilience watchdog polls via diagnostics().frameErrorStreak
      // to decide when to demote (tear down post-FX) and, if that doesn't
      // help, fall back to WebGL.
      webGPUConsecutiveFrameErrors += 1;
      // Any error frame breaks a clean streak, however long — the mount
      // must not restore post-FX on a scene that is still failing.
      webGPUConsecutiveCleanFrames = 0;
      var stats = Object.assign({}, lastWebGPUFrameStats || {}, { renderer: "webgpu", lastError: text, frameErrorStreak: webGPUConsecutiveFrameErrors });
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
          } else {
            webGPUConsecutiveFrameErrors = 0;
            webGPUConsecutiveCleanFrames += 1;
            if (lastWebGPUFrameStats && lastWebGPUFrameStats.lastError) {
              var clean = Object.assign({}, lastWebGPUFrameStats);
              delete clean.lastError;
              delete clean.frameErrorStreak;
              publishWebGPUFrameStats(clean);
            }
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

    // adaptOrtho2DBoardBundle is the Go-wire ↔ 16a seam for the 2D board path.
    //
    // The Go board pipeline (render/bundle2d/bundle2d_gpu.go documents the Go
    // half) marshals engine.RenderBundle JSON in the NATIVE renderer
    // vocabulary: rect quads live in `objects` (vertexOffset/vertexCount/
    // materialIndex) slicing `worldPositions`/`worldNormals`/`worldUVs`. 16a
    // draws the same geometry from the JS scene-core vocabulary (`meshObjects`
    // + `worldMesh*`) — native drawObjectMeshes and 16a drawPBRObjects are the
    // two consumers of one buffer layout, so the bridge is pure ZERO-COPY
    // aliasing: no records are copied or transformed. Idempotent by
    // construction via the !bundle.meshObjects re-entry guard (hosts re-render
    // the same bundle object every frame, and 16a's attribute getter
    // canonicalizes worldMesh* fields to typed arrays in place — re-aliasing
    // would clobber that cache).
    //
    // The only per-record touch-ups are materializing the vertexOffset and
    // materialIndex zeros that Go's `omitempty` elides from the wire (the
    // first object marshals without either): 16a's draw gates require
    // Number.isFinite(vertexOffset), and buildDrawList's pass classification
    // reads materials[obj.materialIndex] directly — an elided zero would
    // mis-default the first rect's material to null.
    //
    // Board bundles can also carry `lines`/`labels`/`sprites` (engine
    // RenderLine {from,to,color,lineWidth} / RenderLabel / RenderSprite).
    // Audited against this renderer: they are inert here, so they are left
    // untouched rather than guarded —
    //   - the world/screen line draw paths key off worldColors+
    //     worldVertexCount / positions+colors+vertexCount, none of which the
    //     board bundle sets, so hasWorldLineData/hasScreenLineData stay false;
    //   - `lines` records are only read by webGPUUnsupportedLineStyles (Go
    //     lines have no lineDash/material → false) and
    //     webGPUHasThickWorldLines (lineWidth > 1 would flip supportsBundle
    //     false at backend selection; the typed CanvasBoardNode wire always
    //     emits lineWidth 1 today);
    //   - `labels`/`sprites` are not read by 16a at all.
    // TODO(M1 slice 2): line/label/sprite primitive parity gives these
    // payloads a real draw path (and revisits the ortho2d gate in
    // buildSceneWorldDrawPlan).
    //
    // `background` needs no bridging: the main-pass clear color already reads
    // bundle.background (same JSON name on Scene3D and board bundles).
    function adaptOrtho2DBoardBundle(bundle) {
      if (
        !bundle ||
        !bundle.camera ||
        bundle.camera.mode !== "ortho2d" ||
        !Array.isArray(bundle.objects) ||
        !bundle.objects.length ||
        bundle.meshObjects
      ) {
        return bundle;
      }
      for (var i = 0; i < bundle.objects.length; i++) {
        var obj = bundle.objects[i];
        if (obj && !Number.isFinite(obj.vertexOffset)) obj.vertexOffset = 0;
        if (obj && !Number.isFinite(obj.materialIndex)) obj.materialIndex = 0;
      }
      // Thumbnail level-of-detail: the board's per-page thumbnail sprites are a
      // MEDIUM-zoom tier — they read as a faithful preview only while zoomed out
      // (CANVAS_LOD_THUMB_ZOOM <= z < CANVAS_LOD_SURFACE_ZOOM); below that the
      // cards alone are the overview and at/above it the live surface owns the
      // page, so a full-card thumbnail just smears over the card fill + labels.
      // Mirror the 2D painter's gate (muddy canvas2d_painter.js) by dropping
      // sprite meshObjects outside the medium band. Geometry stays in the shared
      // buffers (just unreferenced); rects/lines/labels are unaffected.
      var BOARD_LOD_THUMB_ZOOM = 0.3;
      var BOARD_LOD_SURFACE_ZOOM = 0.8;
      var lodZoom = (typeof bundle.camera.z === "number" && bundle.camera.z > 0) ? bundle.camera.z : 1;
      var showThumbs = lodZoom >= BOARD_LOD_THUMB_ZOOM && lodZoom < BOARD_LOD_SURFACE_ZOOM;
      var lodMats = Array.isArray(bundle.materials) ? bundle.materials : [];
      var isThumbSprite = function (o) {
        var m = o && lodMats[o.materialIndex || 0];
        return !!(m && m.kind === "sprite");
      };
      // Only break the zero-copy alias when we actually drop sprites (outside the
      // medium band AND sprites are present); otherwise meshObjects aliases
      // objects by identity (rects/lines/labels and the no-sprite case unchanged).
      if (!showThumbs && bundle.objects.some(isThumbSprite)) {
        bundle.meshObjects = bundle.objects.filter(function (o) { return !isThumbSprite(o); });
      } else {
        bundle.meshObjects = bundle.objects;
      }
      bundle.worldMeshPositions = bundle.worldPositions;
      bundle.worldMeshNormals = bundle.worldNormals;
      bundle.worldMeshUVs = bundle.worldUVs;
      return bundle;
    }

    // -----------------------------------------------------------------------
    // Board labels (M1 GPU-text slice 2): render canvas-board LABEL text as GPU
    // glyphs through the BoardText Selena material. Canonical material source is
    // render/boardgpu/board_text.sel → render/boardgpu/board_text.go (host-side
    // tested). The WGSL + shaderLayout below are copied verbatim from that Go
    // file's boardTextWGSL / boardTextShaderLayout; do NOT diverge from them (and
    // do NOT change the RenderBundle Go schema in this slice).
    // -----------------------------------------------------------------------
    var BOARD_TEXT_WGSL = "struct Uniforms {\n  mvp : mat4x4<f32>,\n  normalMatrix : mat3x3<f32>,\n  textColor : vec3<f32>,\n};\n@group(0) @binding(0) var<uniform> u : Uniforms;\n\n@group(0) @binding(1) var atlas : texture_2d<f32>;\n@group(0) @binding(2) var atlasSampler : sampler;\n\nstruct VertexInput {\n  @location(0) position : vec3<f32>,\n  @location(1) uv : vec2<f32>,\n};\n\nstruct VertexOutput {\n  @builtin(position) position : vec4<f32>,\n  @location(0) vUv : vec2<f32>,\n};\n\n@vertex\nfn vertexMain(in : VertexInput) -> VertexOutput {\n  var out : VertexOutput;\n  out.vUv = in.uv;\n  out.position = (u.mvp * vec4<f32>(in.position, 1.0));\n  return out;\n}\n\n@fragment\nfn fragmentMain(in : VertexOutput) -> @location(0) vec4<f32> {\n  let coverage = textureSample(atlas, atlasSampler, in.vUv).a;\n  return vec4<f32>(u.textColor.r, u.textColor.g, u.textColor.b, coverage);\n}\n";

    // Only the fields the WebGPU Selena pipeline path reads are kept here
    // (attributes, uniformBlock, textures[].wgsl, wgsl.binding, material). The gl/
    // metal/schemaVersion/etc. fields in render/boardgpu/board_text.go's full
    // boardTextShaderLayout are for the GLSL/Metal backends and are inert in JS.
    var BOARD_TEXT_LAYOUT = {
      material: "BoardText",
      attributes: [
        { name: "position", type: "vec3", location: 0 },
        { name: "uv", type: "vec2", location: 1 },
      ],
      uniformBlock: {
        size: 128,
        fields: [
          { name: "mvp", type: "mat4", offset: 0, size: 64 },
          { name: "normalMatrix", type: "mat3", offset: 64, size: 48 },
          { name: "textColor", type: "vec3", offset: 112, size: 12 },
        ],
        defaults: [{ name: "textColor", type: "vec3", values: [0.902, 0.929, 0.953] }],
      },
      textures: [{ name: "atlas", wgsl: { group: 0, textureBinding: 1, samplerBinding: 2 } }],
      wgsl: { group: 0, binding: 0 },
    };

    // A synthetic Selena "material" object so the BoardText draw reuses the exact
    // pipeline path (getSelenaPipeline) that BoardFill rects use. textColor is
    // overwritten per-label before the bind group is built.
    var boardTextMaterial = {
      shaderBackend: "selena",
      customVertexWGSL: BOARD_TEXT_WGSL,
      customFragmentWGSL: BOARD_TEXT_WGSL,
      shaderLayout: BOARD_TEXT_LAYOUT,
      customUniforms: { textColor: [0.902, 0.929, 0.953] },
    };

    // Per-font glyph atlas cache. Key = CSS font string. Each entry holds the
    // uploaded GPUTexture/view, atlas pixel dims, font ascent/descent (CSS px),
    // and a glyph map char → { u0,v0,u1,v1, w (cell width CSS px), advance CSS px }.
    var boardGlyphAtlases = new Map();

    // Stable per-label GPU-buffer owners, keyed by the label's id. Board bundles
    // are re-parsed every dirty frame, so the label objects are fresh each frame;
    // keying the tracked-buffer cache on a persistent owner (not the per-frame
    // object) lets wgpuCachedTrackedBuffer REUSE the uniform/pos/uv buffers across
    // frames instead of reallocating (and leaking) 3×N buffers per pan/zoom frame.
    var boardTextOwners = new Map();

    function parseBoardFontSizePx(font) {
      var m = String(font || "").match(/(\d+(?:\.\d+)?)px/);
      return m ? parseFloat(m[1]) : 12;
    }

    // ensureBoardGlyphAtlas builds (and caches) a coverage atlas for `font`
    // covering `chars` (a string of needed glyphs). White-on-transparent so the
    // texture alpha = glyph coverage, matching the BoardText fragment's .a read.
    // Returns null when canvas rasterization is unavailable (e.g. node tests).
    function ensureBoardGlyphAtlas(font, chars) {
      var entry = boardGlyphAtlases.get(font);
      var needed = "";
      for (var ci = 0; ci < chars.length; ci++) {
        var ch = chars[ci];
        if (entry && entry.glyphs[ch]) continue;
        if (needed.indexOf(ch) === -1) needed += ch;
      }
      if (entry && needed === "") return entry;
      if (typeof OffscreenCanvas === "undefined" && typeof document === "undefined") return null;

      // Union of previously-cached chars + newly needed ones (rebuild whole atlas).
      var allChars = needed;
      if (entry) {
        for (var k in entry.glyphs) {
          if (entry.glyphs.hasOwnProperty(k) && allChars.indexOf(k) === -1) allChars += k;
        }
      }
      if (allChars === "") return entry || null;

      var sizePx = parseBoardFontSizePx(font);
      var pad = 2;
      var measureCanvas = boardCreateCanvas(8, 8);
      if (!measureCanvas) return null;
      var mctx = measureCanvas.getContext("2d");
      // Need a real text-capable 2D context. The node test harness's fake
      // context lacks fillText/measureText, so glyph rasterization degrades to
      // null there (no GPU text) — the documented node-harness behavior; the
      // DOM-overlay label path still runs unaffected.
      if (!mctx || typeof mctx.fillText !== "function" || typeof mctx.measureText !== "function") return null;
      mctx.font = font;
      mctx.textBaseline = "alphabetic";
      var mm = mctx.measureText("Mg");
      var ascent = (mm && mm.actualBoundingBoxAscent > 0) ? mm.actualBoundingBoxAscent : sizePx * 0.8;
      var descent = (mm && mm.actualBoundingBoxDescent > 0) ? mm.actualBoundingBoxDescent : sizePx * 0.2;
      var cellH = Math.ceil(ascent + descent) + pad * 2;

      // Lay glyphs left-to-right in a single row.
      var metrics = [];
      var totalW = 0;
      for (var gi = 0; gi < allChars.length; gi++) {
        var g = allChars[gi];
        var adv = mctx.measureText(g).width;
        var cellW = Math.ceil(adv) + pad * 2;
        metrics.push({ ch: g, advance: adv, x: totalW, w: cellW });
        totalW += cellW;
      }
      var atlasW = Math.max(1, totalW);
      var atlasH = Math.max(1, cellH);

      var atlasCanvas = boardCreateCanvas(atlasW, atlasH);
      if (!atlasCanvas) return null;
      var actx = atlasCanvas.getContext("2d");
      if (!actx) return null;
      actx.clearRect(0, 0, atlasW, atlasH);
      actx.font = font;
      actx.textBaseline = "alphabetic";
      actx.fillStyle = "#ffffff";
      var glyphs = {};
      for (var mi = 0; mi < metrics.length; mi++) {
        var me = metrics[mi];
        actx.fillText(me.ch, me.x + pad, pad + ascent);
        glyphs[me.ch] = {
          u0: me.x / atlasW,
          v0: 0,
          u1: (me.x + me.w) / atlasW,
          v1: 1,
          w: me.w,
          advance: me.advance,
        };
      }

      var texture = device.createTexture({
        size: [atlasW, atlasH, 1],
        format: "rgba8unorm",
        usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST | GPUTextureUsage.RENDER_ATTACHMENT,
      });
      device.queue.copyExternalImageToTexture(
        { source: atlasCanvas },
        { texture: texture },
        [atlasW, atlasH]
      );
      if (entry && entry.texture && typeof entry.texture.destroy === "function") {
        entry.texture.destroy();
      }
      var built = {
        texture: texture,
        view: texture.createView(),
        width: atlasW,
        height: atlasH,
        ascent: ascent,
        descent: descent,
        pad: pad,
        glyphs: glyphs,
      };
      boardGlyphAtlases.set(font, built);
      return built;
    }

    function boardCreateCanvas(w, h) {
      try {
        if (typeof OffscreenCanvas !== "undefined") return new OffscreenCanvas(w, h);
        if (typeof document !== "undefined" && document.createElement) {
          var c = document.createElement("canvas");
          c.width = w;
          c.height = h;
          return c;
        }
      } catch (e) {}
      return null;
    }

    // hasLabelData: a labels-ONLY board (no rects/lines/etc.) must still render,
    // so the render() early-return gate consults this predicate too.
    function hasLabelData(bundle) {
      return Boolean(bundle && Array.isArray(bundle.labels) && bundle.labels.length > 0);
    }

    // drawBoardLabels lays out one position+uv quad per glyph per label and draws
    // them through the BoardText Selena pipeline. Glyph world size = pixelSize /
    // zoom (camera.z) so on-screen text stays a constant pixel size regardless of
    // zoom — mirroring the line-width /zoom trick in render/boardgpu/boardgpu.go.
    // The frame MVP (scratchSelenaViewProjection, the same ortho2D MVP BoardFill
    // rects use) is consumed via sceneSelenaUniformData("mvp").
    function drawBoardLabels(pass, bundle, blendMode, depthWrite) {
      var labels = Array.isArray(bundle.labels) ? bundle.labels : [];
      if (!labels.length) return;
      var resource = getSelenaPipeline(boardTextMaterial, blendMode, depthWrite);
      if (!resource) return;

      var cam = bundle.camera || {};
      var zoom = (typeof cam.z === "number" && cam.z > 0) ? cam.z : 1;

      // Group labels by font so each distinct font hits one atlas.
      var byFont = {};
      var fontOrder = [];
      for (var i = 0; i < labels.length; i++) {
        var lb = labels[i] || {};
        var fnt = (typeof lb.font === "string" && lb.font !== "") ? lb.font : "14px system-ui, sans-serif";
        if (!byFont[fnt]) { byFont[fnt] = []; fontOrder.push(fnt); }
        byFont[fnt].push(lb);
      }

      var pipelineSet = false;
      for (var fi = 0; fi < fontOrder.length; fi++) {
        var font = fontOrder[fi];
        var group = byFont[font];
        var chars = "";
        for (var gi = 0; gi < group.length; gi++) {
          var t = String(group[gi].text == null ? "" : group[gi].text);
          for (var ti = 0; ti < t.length; ti++) {
            if (chars.indexOf(t[ti]) === -1) chars += t[ti];
          }
        }
        if (chars === "") continue;
        var atlas = ensureBoardGlyphAtlas(font, chars);
        if (!atlas) continue;

        // World units per CSS px (constant on-screen text → divide by zoom).
        var wpp = 1 / zoom;
        var ascentW = atlas.ascent * wpp;
        var descentW = atlas.descent * wpp;
        // The glyph ink sits `pad` px inside its atlas cell; shift the cell left
        // by that so the ink lands at the true pen advance (26b1 fillText parity).
        var padW = (atlas.pad || 0) * wpp;

        for (var li = 0; li < group.length; li++) {
          var label = group[li];
          var text = String(label.text == null ? "" : label.text);
          if (text === "") continue;
          var pos = label.position || {};
          var baseX = (typeof pos.x === "number" ? pos.x : 0);
          var baseY = (typeof pos.y === "number" ? pos.y : 0);

          var positions = [];
          var uvs = [];
          var penX = baseX;
          for (var c = 0; c < text.length; c++) {
            var glyph = atlas.glyphs[text[c]];
            if (!glyph) continue;
            var cellW = glyph.w * wpp;
            // Quad spans the glyph cell: vertically [baseline-descent, baseline+ascent].
            var x0 = penX - padW;
            var x1 = x0 + cellW;
            // The cell extends `pad` px beyond the ink ascent/descent (top & bottom),
            // so the quad must too — otherwise the coverage texels stretch.
            // Quad spans the glyph cell: vertically [baseline-descent, baseline
            // +ascent] in the board's +Y-up world. The atlas row 0 (v=0) is the
            // glyph top, so v=0 maps to yTop and v=1 to yBot — glyphs upright.
            var yTop = baseY + ascentW + padW;
            var yBot = baseY - descentW - padW;
            positions.push(
              x0, yBot, 0, x1, yBot, 0, x1, yTop, 0,
              x0, yBot, 0, x1, yTop, 0, x0, yTop, 0
            );
            uvs.push(
              glyph.u0, glyph.v1, glyph.u1, glyph.v1, glyph.u1, glyph.v0,
              glyph.u0, glyph.v1, glyph.u1, glyph.v0, glyph.u0, glyph.v0
            );
            penX += glyph.advance * wpp;
          }
          var vertexCount = positions.length / 3;
          if (vertexCount === 0) continue;

          // textColor uniform (default #e6edf3 → BoardText default), parsed to RGB.
          var rgba = sceneColorRGBA(
            (typeof label.color === "string" && label.color !== "") ? label.color : "#e6edf3",
            [0.902, 0.929, 0.953]
          );
          boardTextMaterial.customUniforms.textColor = [rgba[0], rgba[1], rgba[2]];

          // Stable owner keyed by label id so the tracked buffers persist and are
          // reused across frames (the label object itself is re-parsed per frame).
          var ownerKey = (typeof label.id === "string" && label.id) ? label.id : ("__bt:" + font + ":" + li);
          var owner = boardTextOwners.get(ownerKey);
          if (!owner) { owner = {}; boardTextOwners.set(ownerKey, owner); }
          var uniformData = sceneSelenaUniformData(boardTextMaterial, null, null, selenaFrame);
          var uniformBuffer = wgpuCachedTrackedBuffer(
            owner, "_gosxBoardTextUniform", uniformData,
            GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST, true
          );
          var posBuffer = wgpuCachedTrackedBuffer(
            owner, "_gosxBoardTextPos", new Float32Array(positions),
            GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true
          );
          var uvBuffer = wgpuCachedTrackedBuffer(
            owner, "_gosxBoardTextUV", new Float32Array(uvs),
            GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true
          );
          if (!uniformBuffer || !posBuffer || !uvBuffer) continue;

          var bindGroup = device.createBindGroup({
            layout: resource.bindGroupLayout,
            entries: [
              { binding: 0, resource: { buffer: uniformBuffer } },
              { binding: 1, resource: atlas.view },
              { binding: 2, resource: linearSampler },
            ],
          });

          if (!pipelineSet) {
            pass.setPipeline(resource.pipeline);
            pipelineSet = true;
          }
          pass.setBindGroup(0, bindGroup);
          pass.setVertexBuffer(0, posBuffer);
          pass.setVertexBuffer(1, uvBuffer);
          pass.draw(vertexCount);
        }
      }
    }

    // -----------------------------------------------------------------------
    // GPU picking wiring
    // -----------------------------------------------------------------------

    // The world-space mesh attribute record for the frame in flight. render()
    // sets it; the pick adapter reads it so the pick pass binds the very same
    // vertex buffer the main pass drew from.
    var activePickMeshPositions = null;

    function ensureScenePicker() {
      if (scenePicker || !device) return scenePicker;
      scenePicker = createSceneWebGPUPicker(device, {
        viewProjection: function() { return scratchSelenaViewProjection; },
        meshPositions: function() { return activePickMeshPositions && activePickMeshPositions.positions || null; },
        bindMeshPositions: function(pass, slot, record, vertexOffset, vertexCount, obj) {
          if (obj && obj.retainedGeometry) {
            return webGPUBindRetainedMeshAttribute(pass, slot, obj, "positions", 3);
          }
          return webGPUBindSceneMeshVertexBuffer(pass, slot, record, vertexOffset, vertexCount);
        },
        instancedGeometry: getInstancedGeometry,
        instancedGeometryBuffer: ensureInstancedGeometryGPUBuffer,
        instancedTransformBuffer: ensureInstancedTransformGPUBuffer,
        instancedCount: instancedMeshCount,
        instancedTransforms: instancedMeshTransformData,
        // Do NOT route pick failures through reportWebGPUFrameError. That
        // counter drives the mount watchdog that falls the whole scene back to
        // WebGL. A pick that cannot allocate must degrade picking alone.
        onError: function(message) { console.warn("[gosx] WebGPU pick unavailable:", message); },
      });
      return scenePicker;
    }

    // queuePick schedules one GPU pick. x, y, width, and height are pointer-
    // space values — pass the same numbers you pass sceneScreenToRay. The
    // callback receives a hit record shaped exactly like sceneRaycastPick's, or
    // null for background. It runs one to two frames later; nothing blocks.
    function queuePick(x, y, width, height, callback) {
      var picker = ensureScenePicker();
      if (!picker) {
        if (typeof callback === "function") callback(null);
        return false;
      }
      return picker.queuePick(x, y, width, height, callback);
    }

    // webGPUSummarizeCullSystems totals the cull dispatch counters across the
    // live systems so the mount can publish them.
    function webGPUSummarizeCullSystems() {
      var dispatches = 0;
      var skipped = 0;
      var builtin = 0;
      instancedCullSystems.forEach(function(record) {
        var system = record && record.system;
        if (!system) return;
        dispatches += sceneNumber(system.dispatchCount, 0);
        skipped += sceneNumber(system.skippedDispatchCount, 0);
        if (system.usesBuiltinKernel) builtin += 1;
      });
      return { dispatches: dispatches, skipped: skipped, builtin: builtin };
    }

    // webGPUObjectBlocksBundle names the three per-object draw paths the bundled
    // set excludes. Each binds a resource a compute pass or a per-frame uniform
    // owns, so its command stream changes nearly every frame.
    function webGPUObjectBlocksBundle(obj, materials) {
      if (!obj) return false;
      if (webGPUObjectIsSkinned(obj)) return true;
      if (webGPUObjectComputedMorphDrawRecord(obj)) return true;
      var mat = materials[sceneNumber(obj.materialIndex, 0)] || null;
      return Boolean(mat && typeof sceneSelenaIsMaterial === "function" && sceneSelenaIsMaterial(mat));
    }

    // encodeBundleableSceneDraws drives ONE target through the PBR and instanced
    // draws of all three passes. The target is either the recorder (a dry run
    // that issues no WebGPU calls) or a real GPURenderBundleEncoder. Both see the
    // same call sequence, which is what makes the recorder's verdict sound.
    function encodeBundleableSceneDraws(target, ctx) {
      var passes = [
        { name: "opaque", blend: "opaque", depthWrite: true },
        { name: "alpha", blend: "alpha", depthWrite: false },
        { name: "additive", blend: "additive", depthWrite: false },
      ];
      for (var p = 0; p < passes.length; p++) {
        var spec = passes[p];
        var meshList = ctx.drawList[spec.name];
        if (meshList && meshList.length > 0) {
          target.setPipeline(getPBRPipeline(spec.blend, spec.depthWrite));
          target.setBindGroup(0, ctx.frameBindGroup);
          drawPBRObjects(target, meshList, ctx.bundle, ctx.materials, ctx.frameBindGroup, spec.blend, spec.depthWrite, ctx.pbrBuffers);
        }
        var instancedList = ctx.instancedDrawList[spec.name];
        if (instancedList && instancedList.length > 0) {
          target.setBindGroup(0, ctx.frameBindGroup);
          drawInstancedMeshes(target, instancedList, ctx.materials, spec.blend, spec.depthWrite);
        }
      }
    }

    // webGPURenderBundlesEnabled reports the page's opt-out. Bundling is on by
    // default; set window.__gosx_scene3d_webgpu_render_bundles to false to force
    // direct encoding, which is the first thing to try when an image looks stale.
    function webGPURenderBundlesEnabled() {
      if (typeof window === "undefined") return true;
      return window.__gosx_scene3d_webgpu_render_bundles !== false;
    }

    function render(bundle, viewport, frameMeta) {
      if (typeof inflateSceneShaderLib === "function") {
        inflateSceneShaderLib(bundle);
      }
      adaptOrtho2DBoardBundle(bundle);
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
      var hasLabels = hasLabelData(bundle);
      var hasWaterData = Array.isArray(bundle.waterSystems) && bundle.waterSystems.length > 0;
      var incomingWaterShaderSourcesByID = bundle && bundle.waterShaderSourcesByID && typeof bundle.waterShaderSourcesByID === "object" && Object.keys(bundle.waterShaderSourcesByID).length > 0
        ? bundle.waterShaderSourcesByID
        : sceneWaterShaderSourcesFromEntries(bundle && bundle.waterSystems);
      if (canvas && canvas.parentNode && typeof canvas.parentNode.setAttribute === "function") {
        var incomingWaterEntryCausticBytes = 0;
        var incomingWaterEntries = Array.isArray(bundle && bundle.waterSystems) ? bundle.waterSystems : [];
        for (var iwe = 0; iwe < incomingWaterEntries.length; iwe += 1) {
          var iweEntry = incomingWaterEntries[iwe];
          incomingWaterEntryCausticBytes = Math.max(incomingWaterEntryCausticBytes, iweEntry && typeof iweEntry.causticsWGSL === "string" ? iweEntry.causticsWGSL.trim().length : 0);
        }
        var incomingSourceKeys = incomingWaterShaderSourcesByID && typeof incomingWaterShaderSourcesByID === "object" ? Object.keys(incomingWaterShaderSourcesByID) : [];
        canvas.parentNode.setAttribute("data-gosx-scene3d-webgpu-water-incoming-entry-caustic-source-bytes", String(incomingWaterEntryCausticBytes));
        canvas.parentNode.setAttribute("data-gosx-scene3d-webgpu-water-incoming-shader-systems", String(incomingSourceKeys.length));
      }
      if (bundle && Array.isArray(bundle.waterSystems)) {
        bundle.waterSystems = sceneHydrateWaterEntriesFromSources(bundle.waterSystems, incomingWaterShaderSourcesByID);
        bundle.waterShaderSourcesByID = incomingWaterShaderSourcesByID;
      }
      var preparedScene = typeof prepareScene === "function"
        ? prepareScene(bundle, bundle.camera, viewport, lastPreparedScene, {
          mount: canvas && canvas.parentNode || null,
          sentinels: canvas && canvas.parentNode && canvas.parentNode.__gosxScene3DSentinels || null,
        })
        : null;
      if (preparedScene) {
        var waterShaderSourcesByID = bundle && bundle.waterShaderSourcesByID;
        lastPreparedScene = preparedScene;
        bundle = preparedScene.ir || bundle;
        if (waterShaderSourcesByID && Object.keys(waterShaderSourcesByID).length > 0 && bundle) {
          bundle.waterShaderSourcesByID = waterShaderSourcesByID;
        }
        if (bundle && Array.isArray(bundle.waterSystems)) {
          bundle.waterSystems = sceneHydrateWaterEntriesFromSources(bundle.waterSystems, incomingWaterShaderSourcesByID);
          if (incomingWaterShaderSourcesByID && Object.keys(incomingWaterShaderSourcesByID).length > 0) {
            bundle.waterShaderSourcesByID = incomingWaterShaderSourcesByID;
          }
        }
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
        hasLabels = hasLabelData(bundle);
        hasWaterData = Array.isArray(bundle.waterSystems) && bundle.waterSystems.length > 0;
      }
      webGPUBeginRetainedMeshFrame(bundle);
      if (!hasPBRData && !hasPointsData && !hasInstancedData && !hasWorldLines && !hasScreenLines && !hasSurfaces && !hasLabels && !hasWaterData) {
        webGPUSweepRetainedMeshBuffers();
        return;
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

      // Reconfigure the context ONLY if the surface actually changed (see
      // configureWebGPUCanvas: an unconditional configure() here was a per-frame
      // swapchain re-creation and a hard stall on Metal).
      configureWebGPUCanvas(canvas);

      // Determine post-processing. postFXForceDisabled (set by
      // disablePostProcessing(), the frame-error resilience "demote" step)
      // permanently overrides the authored post-FX chain for this renderer
      // instance — a scene that persistently fails post-FX allocation/
      // validation retries RAW rendering instead of drawing dead frames
      // forever with a poisoned post-FX target.
      var postEffects = Array.isArray(bundle.postEffects) ? bundle.postEffects : [];
      var usePostProcessing = postEffects.length > 0 && !postFXForceDisabled;

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
      var cam = uploadFrameUniforms(
        bundle.camera,
        scaledW,
        scaledH,
        sceneWebGPUToneMapMode(bundle.environment, usePostProcessing)
      );
      uploadLights(bundle.lights);
      uploadFogUniforms(bundle.environment);
      uploadEnvUniforms(bundle.environment);

      // --- Shadow Pass ---
      var shadowLightMatrices = [null, null];
      var shadowLightIndices = [-1, -1];
      var activeShadowCount = 0;

      var encoder = device.createCommandEncoder({ label: "gosx-frame" });
      gpuTimingFrameSeq += 1;
      drainDeferredGPUResources(false);
      var gpuTimingToken = beginGPUFrameTiming(encoder);
      // Per-pass stamps ride on the render-pass descriptors, so the slot must be
      // chosen before the shadow pass opens.
      pollGPUPassTimingReadback();
      beginGPUPassTimingFrame();
      var scopedFrameErrors = beginWebGPUErrorScope();
      var frameNowMS = frameMeta && Number.isFinite(frameMeta.nowMS)
        ? frameMeta.nowMS
        : performance.now();
      var frameActive = !frameMeta || frameMeta.active !== false;
      var frameQualityEnabled = Boolean(frameMeta && frameMeta.qualityEnabled === true);
      var frameQualityProfile = frameQualityEnabled && frameMeta.qualityProfile
        ? frameMeta.qualityProfile
        : null;
      var frameQualityRevision = frameQualityEnabled
        ? (Number.isFinite(frameMeta.qualityRevision)
          ? frameMeta.qualityRevision
          : (frameQualityProfile && Number.isFinite(frameQualityProfile.revision)
            ? frameQualityProfile.revision
            : (Number.isFinite(frameMeta.revision) ? frameMeta.revision : 0)))
        : 0;
      var frameTimeSeconds = frameNowMS / 1000;
      selenaFrame.time = frameTimeSeconds; // feed auto time uniform; set before every selena draw this frame
      var computeParticleRecords = updateComputeParticleSystems(bundle.computeParticles, encoder, frameTimeSeconds);
      var computedMorphStats = updateComputedMorphMeshes(bundle, encoder);
      var elioSkinStats = updateElioSkinnedMeshes(bundle, encoder);
      var pbrSceneBuffers = hasPBRData ? ensurePBRSceneAttributeBuffers(bundle) : null;
      activePickMeshPositions = pbrSceneBuffers;
      if (incomingWaterShaderSourcesByID && Object.keys(incomingWaterShaderSourcesByID).length > 0) {
        bundle.waterSystems = sceneHydrateWaterEntriesFromSources(bundle.waterSystems, incomingWaterShaderSourcesByID);
        bundle.waterShaderSourcesByID = incomingWaterShaderSourcesByID;
      }
      var waterDebugMode = sceneWebGPUWaterDebugMode();
      var waterUpdateStats = sceneWebGPUWaterDebugSkipsUpdate(waterDebugMode)
        ? updateWaterSystems([], encoder, frameNowMS, frameActive, frameQualityProfile, frameQualityRevision, bundle, pbrSceneBuffers, scaledW, scaledH)
        : updateWaterSystems(bundle.waterSystems, encoder, frameNowMS, frameActive, frameQualityProfile, frameQualityRevision, bundle, pbrSceneBuffers, scaledW, scaledH);
      // GPU frustum cull: runs AFTER uploadFrameUniforms so scratchSelenaViewProjection
      // is ready (WebGPU post-depth-remap VP). Runs BEFORE shadow and main passes
      // so outputBuf + drawArgsBuf are populated before drawInstancedMeshes reads them.
      // Only processes meshes with cullKernelWGSL present (gpu-cull capability active
      // by virtue of being in the WebGPU renderer). Meshes without a kernel draw-all.
      var instancedCullMap = updateInstancedCullSystems(bundle.instancedMeshes, encoder, scratchSelenaViewProjection);
      var webGPUCullTotals = webGPUSummarizeCullSystems();

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
      var materials = Array.isArray(bundle.materials) ? bundle.materials : [];
      var waterObjectSceneTextureStats = sceneWebGPUWaterDebugSkipsDraw(waterDebugMode)
        ? renderWaterObjectSceneTexturePasses([], encoder, bundle, materials, frameBindGroup, pbrSceneBuffers, scaledW, scaledH, !usePostProcessing)
        : renderWaterObjectSceneTexturePasses(
          waterUpdateStats.records,
          encoder,
          bundle,
          materials,
          frameBindGroup,
          pbrSceneBuffers,
          scaledW,
          scaledH,
          !usePostProcessing
        );
      waterUpdateStats.waterObjectTexturePasses += waterObjectSceneTextureStats.waterObjectTexturePasses;
      waterUpdateStats.waterObjectTextureTargets += waterObjectSceneTextureStats.waterObjectTextureTargets;
      waterUpdateStats.waterObjectTexturePixels += waterObjectSceneTextureStats.waterObjectTexturePixels;
      waterUpdateStats.waterObjectTextureWidth = Math.max(waterUpdateStats.waterObjectTextureWidth || 0, waterObjectSceneTextureStats.waterObjectTextureWidth || 0);
      waterUpdateStats.waterObjectTextureHeight = Math.max(waterUpdateStats.waterObjectTextureHeight || 0, waterObjectSceneTextureStats.waterObjectTextureHeight || 0);
      waterUpdateStats.waterObjectTexturePixelBudget = Math.max(waterUpdateStats.waterObjectTexturePixelBudget || 0, waterObjectSceneTextureStats.waterObjectTexturePixelBudget || 0);
      waterUpdateStats.waterObjectTextureMeshPasses += waterObjectSceneTextureStats.waterObjectTextureMeshPasses;
      waterUpdateStats.waterObjectTextureMeshDrawCalls += waterObjectSceneTextureStats.waterObjectTextureMeshDrawCalls;
      waterUpdateStats.waterObjectTextureSelenaDrawCalls += waterObjectSceneTextureStats.waterObjectTextureSelenaDrawCalls;
      waterUpdateStats.waterObjectTextureFallbackPasses += waterObjectSceneTextureStats.waterObjectTextureFallbackPasses;
      waterUpdateStats.waterObjectTextureCandidateObjects += waterObjectSceneTextureStats.waterObjectTextureCandidateObjects;
      waterUpdateStats.waterObjectTextureSelectedObjects += waterObjectSceneTextureStats.waterObjectTextureSelectedObjects;
      waterUpdateStats.waterObjectTextureFallbackMissingObjects += waterObjectSceneTextureStats.waterObjectTextureFallbackMissingObjects;
      waterUpdateStats.waterObjectTextureFallbackMissingResources += waterObjectSceneTextureStats.waterObjectTextureFallbackMissingResources;
      waterUpdateStats.waterObjectTextureCandidateProfile = waterObjectSceneTextureStats.waterObjectTextureCandidateProfile || waterUpdateStats.waterObjectTextureCandidateProfile;

      // --- Main Render Target ---
      var mainColorView;
      var mainResolveView = null;
      var mainDepthTargetView;
      var postTarget = null;

      if (usePostProcessing) {
        if (!postProcessor) {
          postProcessor = wgpuCreatePostProcessor(device, targetFormat, reportWebGPUFrameError, function(material, owner, renderContext) {
            return sceneSelenaUniformData(material, owner, renderContext, selenaFrame);
          });
        }
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

      var mainPassDescriptor = {
        colorAttachments: [mainColorAttachment],
        depthStencilAttachment: {
          view: mainDepthTargetView,
          depthLoadOp: "clear",
          depthClearValue: 1.0,
          depthStoreOp: "store",
        },
      };
      var mainStamps = gpuPassTimestampWrites("main");
      if (mainStamps) mainPassDescriptor.timestampWrites = mainStamps;
      var mainPass = encoder.beginRenderPass(mainPassDescriptor);

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
        retainedMeshObjects: sceneNumber(bundle.retainedMeshObjectCount, 0),
        retainedMeshVertices: sceneNumber(bundle.retainedMeshVertexCount, 0),
        worldBakedMeshObjects: sceneNumber(bundle.worldBakedMeshObjectCount, 0),
        worldBakedMeshVertices: sceneNumber(bundle.worldBakedMeshVertexCount, 0),
        bundleBuildCPUms: sceneNumber(bundle.bundleBuildCPUms, 0),
        plannerCPUms: sceneNumber(bundle.plannerTelemetry && bundle.plannerTelemetry.lastPlannerCPUms, 0),
        plannerFullVertexHashScans: sceneNumber(bundle.plannerTelemetry && bundle.plannerTelemetry.fullVertexHashScans, 0),
        // meshDrawCalls is filled in below by drawPBRObjects (mutated in place
        // across the opaque/alpha/additive passes, mirroring how waterUpdateStats
        // accumulates across many draw functions within one frame) -- it counts
        // actual pass.draw() dispatches, i.e. SUBMITTED mesh objects only.
        // meshViewCulled is the CPU-frustum-culled complement: bundle.meshObjects
        // entries buildDrawList excluded via obj.viewCulled before drawPBRObjects
        // ever saw them. meshObjects (above) counts BOTH -- see the comment on
        // webGPUCountViewCulledMeshObjects for why that ambiguity is the bug this
        // pair of counters exists to close.
        meshDrawCalls: 0,
        meshViewCulled: webGPUCountViewCulledMeshObjects(bundle),
        meshUndrawable: webGPUCountUndrawableMeshObjects(bundle),
        skinnedMeshObjects: webGPUCountSkinnedMeshes(bundle),
        computedMorphDispatches: computedMorphStats.computedMorphDispatches,
        computedMorphVertices: computedMorphStats.computedMorphVertices,
        computedMorphKernel: computedMorphStats.computedMorphKernel,
        elioSkinningDispatches: elioSkinStats.elioSkinningDispatches,
        elioSkinningVertices: elioSkinStats.elioSkinningVertices,
        elioSkinningKernel: elioSkinStats.elioSkinningKernel,
        instancedMeshes: Array.isArray(bundle.instancedMeshes) ? bundle.instancedMeshes.length : 0,
        instancedInstances: webGPUPlannedInstanceCount(bundle.instancedMeshes),
        cullDispatches: webGPUCullTotals.dispatches,
        cullSkippedDispatches: webGPUCullTotals.skipped,
        cullBuiltinSystems: webGPUCullTotals.builtin,
        lineEntries: thickLineRecord ? 1 : worldLineEntries.length,
        surfaceEntries: Array.isArray(bundle.surfaces) ? bundle.surfaces.length : 0,
        waterSystems: waterUpdateStats.waterSystems,
        waterCells: waterUpdateStats.waterCells,
        waterVertices: waterUpdateStats.waterVertices,
        waterComputeDispatches: waterUpdateStats.waterComputeDispatches,
        waterSimulationTicks: waterUpdateStats.waterSimulationTicks,
        waterSolverSubsteps: waterUpdateStats.waterSolverSubsteps,
        waterDroppedTicks: waterUpdateStats.waterDroppedTicks,
        waterDroppedTicksThisFrame: waterUpdateStats.waterDroppedTicksThisFrame,
        waterSimulationCatchUpCap: waterUpdateStats.waterSimulationCatchUpCap,
        waterSimulationTickSeq: waterUpdateStats.waterSimulationTickSeq,
        waterSolverSubstepSeq: waterUpdateStats.waterSolverSubstepSeq,
        waterNormalDispatches: waterUpdateStats.waterNormalDispatches,
        waterNormalDispatchSeq: waterUpdateStats.waterNormalDispatchSeq,
        waterSampledStateCopies: waterUpdateStats.waterSampledStateCopies,
        waterSampledStateSyncSeq: waterUpdateStats.waterSampledStateSyncSeq,
        // P4-M1 fix (water-parity-campaign): waterAtRestSystems/
        // waterRestSubstepsSkipped (M5 at-rest gating) and waterUniformUploads/
        // waterUniformUploadsSkipped (M6 uniform-upload dedup) were computed
        // and incremented on updateWaterSystems' returned stats object (see
        // WATER_REST_ENERGY_EPSILON's comment above updateWaterSystems) but
        // were never copied into this frameStats literal that
        // publishWebGPUFrameStats actually reads -- so
        // data-gosx-scene3d-webgpu-water-at-rest-systems (and the sibling
        // rest/dedup counters) always published the `|| 0` fallback and never
        // reflected real state, regardless of whether the underlying gating
        // logic fired correctly. Wiring the four fields through here is the
        // fix; the gating logic itself was already correct.
        waterAtRestSystems: waterUpdateStats.waterAtRestSystems,
        waterRestSubstepsSkipped: waterUpdateStats.waterRestSubstepsSkipped,
        waterUniformUploads: waterUpdateStats.waterUniformUploads,
        waterUniformUploadsSkipped: waterUpdateStats.waterUniformUploadsSkipped,
        waterQualityTier: waterUpdateStats.waterQualityTier,
        waterQualityRevision: waterUpdateStats.waterQualityRevision,
        waterSurfaceResolution: waterUpdateStats.waterSurfaceResolution,
        waterActiveCausticsResolution: waterUpdateStats.waterActiveCausticsResolution,
        waterActiveObjectShadowResolution: waterUpdateStats.waterActiveObjectShadowResolution,
        waterActiveObjectTextureWidth: waterUpdateStats.waterActiveObjectTextureWidth,
        waterActiveObjectTextureHeight: waterUpdateStats.waterActiveObjectTextureHeight,
        waterActiveObjectTexturePixelBudget: waterUpdateStats.waterActiveObjectTexturePixelBudget,
        waterQualityAllocationPending: waterUpdateStats.waterQualityAllocationPending,
        waterQualityAllocationFailures: waterUpdateStats.waterQualityAllocationFailures,
        waterQualityAllocationRetryFrame: waterUpdateStats.waterQualityAllocationRetryFrame,
        waterQualityDPRCap: waterUpdateStats.waterQualityDPRCap,
        waterExpensivePassCadence: waterUpdateStats.waterExpensivePassCadence,
        waterAuthoredComputeSystems: waterUpdateStats.waterAuthoredComputeSystems,
        waterAuthoredComputeDispatches: waterUpdateStats.waterAuthoredComputeDispatches,
        waterAuthoredComputeFallbacks: waterUpdateStats.waterAuthoredComputeFallbacks,
        waterSelenaComputeSystems: waterUpdateStats.waterSelenaComputeSystems,
        waterSelenaComputeDispatches: waterUpdateStats.waterSelenaComputeDispatches,
        waterSelenaComputeFallbacks: waterUpdateStats.waterSelenaComputeFallbacks,
        waterDropDispatches: waterUpdateStats.waterDropDispatches,
        waterDropDispatchTotal: waterUpdateStats.waterDropDispatchTotal,
        waterLastDropEventID: waterUpdateStats.waterLastDropEventID,
        waterObjectSystems: waterUpdateStats.waterObjectSystems,
        waterObjectDispatches: waterUpdateStats.waterObjectDispatches,
        waterObjectEventDispatches: waterUpdateStats.waterObjectEventDispatches,
        waterLastObjectDisplacementEventID: waterUpdateStats.waterLastObjectDisplacementEventID,
        waterObjectSpheres: waterUpdateStats.waterObjectSpheres,
        waterRoundedSystems: waterUpdateStats.waterRoundedSystems,
        waterCornerRadius: waterUpdateStats.waterCornerRadius,
        waterLightDirX: waterUpdateStats.waterLightDirX,
        waterLightDirY: waterUpdateStats.waterLightDirY,
        waterLightDirZ: waterUpdateStats.waterLightDirZ,
        waterCausticSystems: waterUpdateStats.waterCausticSystems,
        waterCausticPasses: waterUpdateStats.waterCausticPasses,
        waterCausticTexturePixels: waterUpdateStats.waterCausticTexturePixels,
        waterAuthoredCausticSystems: waterUpdateStats.waterAuthoredCausticSystems,
        waterAuthoredCausticPasses: waterUpdateStats.waterAuthoredCausticPasses,
        waterAuthoredCausticFallbacks: waterUpdateStats.waterAuthoredCausticFallbacks,
        waterAuthoredCausticFallbackReason: waterUpdateStats.waterAuthoredCausticFallbackReason,
        waterAuthoredCausticSourceBytes: waterUpdateStats.waterAuthoredCausticSourceBytes,
        waterEntryCausticSourceBytes: waterUpdateStats.waterEntryCausticSourceBytes,
        waterResolvedCausticSourceBytes: waterUpdateStats.waterResolvedCausticSourceBytes,
        waterAuthoredSurfaceSourceBytes: waterUpdateStats.waterAuthoredSurfaceSourceBytes,
        waterEntrySurfaceSourceBytes: waterUpdateStats.waterEntrySurfaceSourceBytes,
        waterResolvedSurfaceSourceBytes: waterUpdateStats.waterResolvedSurfaceSourceBytes,
        waterManifestShaderSystems: waterUpdateStats.waterManifestShaderSystems,
        waterManifestShaderFields: waterUpdateStats.waterManifestShaderFields,
        waterManifestCausticSourceBytes: waterUpdateStats.waterManifestCausticSourceBytes,
        waterManifestSurfaceSourceBytes: waterUpdateStats.waterManifestSurfaceSourceBytes,
        waterBundleShaderSystems: waterUpdateStats.waterBundleShaderSystems,
        waterBundleCausticSourceBytes: waterUpdateStats.waterBundleCausticSourceBytes,
        waterBundleSurfaceSourceBytes: waterUpdateStats.waterBundleSurfaceSourceBytes,
        waterObjectTexturePasses: waterUpdateStats.waterObjectTexturePasses,
        waterObjectTextureTargets: waterUpdateStats.waterObjectTextureTargets,
        waterObjectTexturePixels: waterUpdateStats.waterObjectTexturePixels,
        waterObjectTextureWidth: waterUpdateStats.waterObjectTextureWidth,
        waterObjectTextureHeight: waterUpdateStats.waterObjectTextureHeight,
        waterObjectTexturePixelBudget: waterUpdateStats.waterObjectTexturePixelBudget,
        waterObjectTextureMeshPasses: waterUpdateStats.waterObjectTextureMeshPasses,
        waterObjectTextureMeshDrawCalls: waterUpdateStats.waterObjectTextureMeshDrawCalls,
        waterObjectTextureSelenaDrawCalls: waterUpdateStats.waterObjectTextureSelenaDrawCalls,
        waterObjectTextureFallbackPasses: waterUpdateStats.waterObjectTextureFallbackPasses,
        waterObjectTextureCandidateObjects: waterUpdateStats.waterObjectTextureCandidateObjects,
        waterObjectTextureSelectedObjects: waterUpdateStats.waterObjectTextureSelectedObjects,
        waterObjectTextureFallbackMissingObjects: waterUpdateStats.waterObjectTextureFallbackMissingObjects,
        waterObjectTextureFallbackMissingResources: waterUpdateStats.waterObjectTextureFallbackMissingResources,
        waterObjectTextureCandidateProfile: waterUpdateStats.waterObjectTextureCandidateProfile,
        waterObjectShadowPasses: waterUpdateStats.waterObjectShadowPasses,
        waterObjectShadowTexturePixels: waterUpdateStats.waterObjectShadowTexturePixels,
        waterObjectShadowMeshPasses: waterUpdateStats.waterObjectShadowMeshPasses,
        waterObjectShadowMeshDrawCalls: waterUpdateStats.waterObjectShadowMeshDrawCalls,
        waterAuthoredObjectShadowPasses: waterUpdateStats.waterAuthoredObjectShadowPasses,
        waterAuthoredObjectShadowFallbacks: waterUpdateStats.waterAuthoredObjectShadowFallbacks,
        waterAuthoredObjectMeshShadowPasses: waterUpdateStats.waterAuthoredObjectMeshShadowPasses,
        waterAuthoredObjectMeshShadowFallbacks: waterUpdateStats.waterAuthoredObjectMeshShadowFallbacks,
        waterObjectShadowFallbackPasses: waterUpdateStats.waterObjectShadowFallbackPasses,
        waterObjectShadowFallbackMissingObjects: waterUpdateStats.waterObjectShadowFallbackMissingObjects,
        waterObjectShadowFallbackMissingResources: waterUpdateStats.waterObjectShadowFallbackMissingResources,
        waterReflectionSystems: waterUpdateStats.waterReflectionSystems,
        waterRefractionSystems: waterUpdateStats.waterRefractionSystems,
        waterObjectOpticsSystems: waterUpdateStats.waterObjectOpticsSystems,
        waterSelenaCausticPasses: waterUpdateStats.waterSelenaCausticPasses,
        waterSelenaCausticFallbacks: waterUpdateStats.waterSelenaCausticFallbacks,
        waterSelenaObjectShadowPasses: waterUpdateStats.waterSelenaObjectShadowPasses,
        waterSelenaObjectShadowFallbacks: waterUpdateStats.waterSelenaObjectShadowFallbacks,
        waterSelenaObjectMeshShadowPasses: waterUpdateStats.waterSelenaObjectMeshShadowPasses,
        waterSelenaObjectMeshShadowFallbacks: waterUpdateStats.waterSelenaObjectMeshShadowFallbacks,
        customMaterialFallbacks: customMaterialStats.customMaterialFallbacks,
        customWGSLFallbacks: customMaterialStats.customWGSLFallbacks,
        customUniformFallbacks: customMaterialStats.customUniformFallbacks,
      };

      // --- Render bundle decision ---
      //
      // A frame qualifies when it draws PBR meshes and instanced meshes only.
      // Everything else keeps the direct encoder; see
      // sceneWebGPUBundleIneligibleReason for why.
      var bundleableDraws = (hasPBRData && (drawList.opaque.length + drawList.alpha.length + drawList.additive.length) > 0) ||
        (hasInstancedData && (instancedDrawList.opaque.length + instancedDrawList.alpha.length + instancedDrawList.additive.length) > 0);
      var bundleReason = sceneWebGPUBundleIneligibleReason({
        // Both halves must exist. An implementation that can build a bundle but
        // not replay one would leave the frame blank.
        disabled: !webGPURenderBundlesEnabled() ||
          typeof device.createRenderBundleEncoder !== "function" ||
          typeof mainPass.executeBundles !== "function",
        hasWater: hasWaterData,
        hasPoints: hasPointsData,
        hasLabels: hasLabels,
        hasScreenLines: hasScreenLines,
        hasSurfaces: hasSurfaces,
        hasWorldLines: hasWorldLines,
        hasDynamicMeshes: sceneWebGPUDrawListHasDynamicMesh(drawList, function(obj) {
          return webGPUObjectBlocksBundle(obj, materials);
        }),
        hasBundleableDraws: bundleableDraws,
      });
      frameStats.bundleState = "direct";
      frameStats.bundleReason = bundleReason;
      frameStats.bundleEncodes = 0;
      frameStats.bundleReplays = 0;
      frameStats.bundleDraws = 0;

      if (bundleReason === "") {
        if (!webGPUBundleCache) webGPUBundleCache = createSceneWebGPUBundleCache();
        var bundleLayoutKey = sceneWebGPUBundleLayoutKey(targetFormat, "depth24plus", sampleCount);
        var bundleContext = {
          drawList: drawList,
          instancedDrawList: instancedDrawList,
          bundle: bundle,
          materials: materials,
          frameBindGroup: frameBindGroup,
          pbrBuffers: pbrSceneBuffers,
        };
        var verdict = webGPUBundleCache.plan(bundleLayoutKey, function(recorder) {
          encodeBundleableSceneDraws(recorder, bundleContext);
        });
        if (!verdict.eligible) {
          bundleReason = verdict.reason;
          frameStats.bundleReason = bundleReason;
        } else if (verdict.reusable) {
          mainPass.executeBundles([webGPUBundleCache.bundle()]);
          webGPUBundleCache.markReplayed();
          frameStats.bundleState = "replayed";
        } else {
          var bundleEncoder = device.createRenderBundleEncoder({
            label: "gosx-scene-bundle",
            colorFormats: [targetFormat],
            depthStencilFormat: "depth24plus",
            sampleCount: sampleCount,
          });
          encodeBundleableSceneDraws(bundleEncoder, bundleContext);
          var finishedBundle = bundleEncoder.finish({ label: "gosx-scene-bundle" });
          webGPUBundleCache.adopt(bundleLayoutKey, finishedBundle);
          mainPass.executeBundles([finishedBundle]);
          frameStats.bundleState = "encoded";
        }
        if (frameStats.bundleState !== "direct") {
          var bundleStats = webGPUBundleCache.stats();
          frameStats.bundleEncodes = bundleStats.encodes;
          frameStats.bundleReplays = bundleStats.replays;
          frameStats.bundleDraws = bundleStats.draws;
        }
      }

      // Draw PBR meshes, WebGPU-native instanced meshes, world lines, and textured surfaces.
      if (frameStats.bundleState === "direct" && (hasPBRData || hasInstancedData || hasWorldLines || hasSurfaces)) {
        // Opaque pass.
        if (drawList.opaque.length > 0) {
          var opaquePipeline = getPBRPipeline("opaque", true);
          mainPass.setPipeline(opaquePipeline);
          mainPass.setBindGroup(0, frameBindGroup);
          drawPBRObjects(mainPass, drawList.opaque, bundle, materials, frameBindGroup, "opaque", true, pbrSceneBuffers, frameStats);
        }
        if (instancedDrawList.opaque.length > 0) {
          mainPass.setBindGroup(0, frameBindGroup);
          drawInstancedMeshes(mainPass, instancedDrawList.opaque, materials, "opaque", true);
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
          drawPBRObjects(mainPass, drawList.alpha, bundle, materials, frameBindGroup, "alpha", false, pbrSceneBuffers, frameStats);
        }
        if (instancedDrawList.alpha.length > 0) {
          mainPass.setBindGroup(0, frameBindGroup);
          drawInstancedMeshes(mainPass, instancedDrawList.alpha, materials, "alpha", false);
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
          drawPBRObjects(mainPass, drawList.additive, bundle, materials, frameBindGroup, "additive", false, pbrSceneBuffers, frameStats);
        }
        if (instancedDrawList.additive.length > 0) {
          mainPass.setBindGroup(0, frameBindGroup);
          drawInstancedMeshes(mainPass, instancedDrawList.additive, materials, "additive", false);
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

      if (hasWaterData && !sceneWebGPUWaterDebugSkipsDraw(waterDebugMode)) {
        Object.assign(frameStats, drawWaterPoolEntries(mainPass, waterUpdateStats.records, frameBindGroup));
        Object.assign(frameStats, drawWaterSystemEntries(mainPass, waterUpdateStats.records, frameBindGroup, cam));
      }

      // Board label glyphs (M1 GPU-text slice 2). Drawn after the opaque/alpha
      // board fills so the alpha-blended glyphs composite over the rects. Lives
      // outside the (hasPBRData || …) block above so a labels-only board still
      // paints text. blendMode "alpha", depthWriteEnabled false.
      if (hasLabels) {
        drawBoardLabels(mainPass, bundle, "alpha", false);
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

      // GPU pick pass. Records only when a pick waits, and only ever touches
      // its own ID and depth textures. Runs after the main pass so every vertex
      // and instance-transform buffer the pick draws from already exists.
      // Pick coordinates map onto the main render target, so use the same
      // width/height the main pass used: scaled dimensions under post-FX,
      // canvas dimensions otherwise.
      if (scenePicker && scenePicker.hasPending()) {
        scenePicker.recordPickPass(
          encoder,
          bundle,
          usePostProcessing ? scaledW : width,
          usePostProcessing ? scaledH : height
        );
      }

      // Post-processing.
      if (usePostProcessing && postProcessor) {
        var screenView = gpuCtx.getCurrentTexture().createView();
        Object.assign(frameStats, postProcessor.apply(encoder, postEffects, scaledW, scaledH, width, height, screenView, bundle.camera));
      }

      endGPUFrameTiming(encoder, gpuTimingToken);
      endGPUPassTimingFrame(encoder);
      device.queue.submit([encoder.finish()]);
      // Start the pick map AFTER submit. mapAsync resolves on a later task, so
      // this adds no wait to the frame.
      if (scenePicker) scenePicker.finishReadback();
      webGPUSweepRetainedMeshBuffers();
      Object.assign(frameStats, webGPURetainedMeshFrameStats());
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

    function destroyRendererGPUResource(resource) {
      if (!resource || typeof resource.destroy !== "function") return;
      try { resource.destroy(); } catch (_err) {}
    }

    function dispose() {
      if (rendererResourcesDisposed) return;
      rendererResourcesDisposed = true;

      try { destroyGPUTimingResources(gpuTiming); } catch (_err) {}
      try { destroyGPUPassTimingResources(); } catch (_err) {}
      if (webGPUBundleCache) {
        try { webGPUBundleCache.invalidate(); } catch (_err) {}
      }
      webGPUBundleCache = null;
      gpuTiming = null;
      for (var failedTimingIndex = 0; failedTimingIndex < failedGPUTimings.length; failedTimingIndex++) {
        try { destroyGPUTimingResources(failedGPUTimings[failedTimingIndex].timing); } catch (_err) {}
      }
      failedGPUTimings.length = 0;
      try { drainDeferredGPUResources(true); } catch (_err) {}
      deferredWaterTextureRetirements.length = 0;
      deferredWaterSystemRetirements.length = 0;
      lastGPUPerformanceSample = null;
      if (scenePicker) {
        try { scenePicker.dispose(); } catch (_err) {}
      }
      scenePicker = null;
      activePickMeshPositions = null;
      for (const pair of Array.from(retainedMeshAttributeCache.entries())) {
        webGPURetireRetainedMeshEntry(pair[0], pair[1]);
      }

      destroyRendererGPUResource(frameUniformBuffer);
      frameUniformBuffer = null;
      destroyRendererGPUResource(lightStorageBuffer);
      lightStorageBuffer = null;
      // Release the light buffers that capacity growth replaced.
      for (var retiredLight = 0; retiredLight < _retiredLightBuffers.length; retiredLight++) {
        destroyRendererGPUResource(_retiredLightBuffers[retiredLight]);
      }
      _retiredLightBuffers.length = 0;
      destroyRendererGPUResource(fogUniformBuffer);
      fogUniformBuffer = null;
      destroyRendererGPUResource(envUniformBuffer);
      envUniformBuffer = null;
      destroyRendererGPUResource(shadowUniformBuffer);
      shadowUniformBuffer = null;
      destroyRendererGPUResource(positionBuffer);
      positionBuffer = null;
      destroyRendererGPUResource(normalBuffer);
      normalBuffer = null;
      destroyRendererGPUResource(uvBuffer);
      uvBuffer = null;
      destroyRendererGPUResource(tangentBuffer);
      tangentBuffer = null;
      destroyRendererGPUResource(shadowPositionBuffer);
      shadowPositionBuffer = null;
      destroyRendererGPUResource(shadowFrameBuffer);
      shadowFrameBuffer = null;
      pointsEntryGPUBuffers.forEach(function(buffer) {
        destroyRendererGPUResource(buffer);
      });
      pointsEntryGPUBuffers.clear();
      // Board glyph atlases are textures (not tracked in pointsEntryGPUBuffers);
      // destroy them explicitly. The per-label glyph buffers are tracked buffers,
      // already freed above; just drop the owner map.
      boardGlyphAtlases.forEach(function(a) {
        if (a) destroyRendererGPUResource(a.texture);
      });
      boardGlyphAtlases.clear();
      boardTextOwners.clear();
      disposeComputeParticleSystems();
      disposeWaterSystems();
      for (const record of instancedCullSystems.values()) {
        if (record && record.system && typeof record.system.dispose === "function") {
          try { record.system.dispose(); } catch (_err) {}
        }
      }
      instancedCullSystems.clear();
      waterRenderPipelineCache.clear();
      pointsAuthoredPipelineCache.clear();
      pointsAuthoredLayerFailed.clear();
      waterPoolPipelineCache = {};
      waterObjectMeshPipelineCache = {};

      destroyRendererGPUResource(mainDepthTexture);
      mainDepthTexture = null;
      mainDepthView = null;
      destroyRendererGPUResource(mainMSAATexture);
      mainMSAATexture = null;
      mainMSAAView = null;
      destroyRendererGPUResource(dummyShadowTex);
      dummyShadowTex = null;
      dummyShadowView = null;
      destroyRendererGPUResource(placeholderTex);
      placeholderTex = null;
      placeholderView = null;
      destroyRendererGPUResource(placeholderCubeTex);
      placeholderCubeTex = null;
      placeholderCubeView = null;

      for (var si = 0; si < shadowSlots.length; si++) {
        if (shadowSlots[si]) destroyRendererGPUResource(shadowSlots[si].texture);
        shadowSlots[si] = null;
      }

      textureCache._gosxGeneration.disposed = true;
      for (var record of textureCache.values()) {
        if (record) {
          record.disposed = true;
          if (record.image) {
            record.image.onload = null;
            record.image.onerror = null;
          }
          destroyRendererGPUResource(record.texture);
        }
      }
      textureCache.clear();
      selenaPipelineCache.clear();
      selenaComputePipelineCache.clear();
      selenaPostPipelineCache.clear();
      pipelineCache = {};
      instancedGeometryCache = {};
      // Buffers cached on these owners are registered in
      // pointsEntryGPUBuffers and were destroyed before the owner objects are
      // replaced, so no current tracked buffer is orphaned.
      pbrSceneAttributeCache = {};
      webGPUEssentialAttributeCache = Object.create(null);
      defaultMaterialOwner = {};
      thickLineOwner = {};
      screenLineOwner = {};
      _frameBindGroupCache = null;
      lastPreparedScene = null;
      lastWebGPUFrameStats = null;
      waterManifestShaderSourcesByID = null;
      activeWaterShaderSourcesByID = null;

      if (postProcessor) {
        try { postProcessor.dispose(); } catch (_err) {}
        postProcessor = null;
      }

      device = null;
      configuredSurfaceKey = "";
      postFXForceDisabled = false;
    }

    // disablePostProcessing: the frame-error resilience "demote" step (see
    // 20-scene-mount.js's checkSceneWebGPUFrameErrorWatchdog). Tears down the
    // post-FX chain's GPU resources (HDR scene target, bloom ping-pong,
    // depth, cached pipelines/bind groups — everything wgpuCreatePostProcessor
    // owns) and permanently stops rebuilding them for this renderer instance,
    // so a scene whose post-FX allocation/validation is persistently failing
    // (memory-tight browser, poisoned target) falls back to raw rendering
    // instead of resubmitting the same broken frame forever. Idempotent —
    // returns false if already demoted (nothing to do).
    function disablePostProcessing() {
      if (postFXForceDisabled) return false;
      postFXForceDisabled = true;
      if (postProcessor) {
        try { postProcessor.dispose(); } catch (_err) {}
        postProcessor = null;
      }
      // Give raw rendering a fresh error-streak window: if post-FX really
      // was the problem, this resets the counter the fallback threshold
      // compares against, so the mount only escalates to a full backend
      // swap if raw rendering ALSO keeps failing.
      webGPUConsecutiveFrameErrors = 0;
      return true;
    }

    // enablePostProcessing: the frame-error resilience "restore" step — the
    // way back that disablePostProcessing (above) never had. Called by
    // 20-scene-mount.js's checkSceneWebGPUFrameErrorResilience once a
    // demoted renderer has produced enough consecutive clean frames (see
    // diagnostics().frameCleanStreak). Clears postFXForceDisabled so the
    // very next render() call with a non-empty bundle.postEffects rebuilds
    // the post-FX chain itself (usePostProcessing below, and the lazy
    // `if (!postProcessor) postProcessor = wgpuCreatePostProcessor(...)`
    // in render()) — no GPU resource construction happens here; this
    // function only flips the gate. Idempotent — returns false if post-FX
    // is not currently force-disabled (nothing to do).
    function enablePostProcessing() {
      if (!postFXForceDisabled) return false;
      postFXForceDisabled = false;
      return true;
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

    function setLifecycle(state) {
      var lifecycle = state && typeof state === "object" ? state : {};
      var nowMS = Number.isFinite(lifecycle.nowMS)
        ? lifecycle.nowMS
        : ((typeof performance !== "undefined" && typeof performance.now === "function") ? performance.now() : Date.now());
      var active = lifecycle.active !== false && lifecycle.disposed !== true;
      var paused = lifecycle.paused === true || lifecycle.disposed === true;
      for (var record of waterSystems.values()) {
        var system = record && record.system;
        if (!system || !system.waterClock) continue;
        waterClockAPI.sceneWaterResetClock(system.waterClock, nowMS, active, paused || sceneBool(system.entry && system.entry.paused, false));
      }
    }

    // webGPUAdapterInfoSnapshot returns the GPUAdapterInfo the probe captured.
    // Vendor / architecture / device / description together are what let a
    // dump be attributed to a specific driver, and (with the browser engine)
    // to Dawn-plus-Tint versus wgpu-plus-naga. "backend=webgpu" alone cannot
    // distinguish two WGSL compilers with two different sets of bugs.
    function webGPUAdapterInfoSnapshot() {
      var base = typeof sceneWebGPUDiagnostics === "function" ? sceneWebGPUDiagnostics() : {};
      return (base && base.adapterInfo) ? base.adapterInfo : {};
    }

    var textureVariantTokens = [];
    try {
      if (typeof window !== "undefined" && typeof window.__gosx_scene3d_texture_tokens === "function") {
        textureVariantTokens = window.__gosx_scene3d_texture_tokens(
          "webgpu",
          device && device.features ? Array.from(device.features) : []
        );
      }
    } catch (_textureVariantError) {
      textureVariantTokens = [];
    }
    var textureVariantContext = {
      backend: "webgpu",
      // wgpuUploadKTX2Texture is this renderer's concrete upload path. The
      // reader/parser module may load later with glTF, so construction must not
      // freeze a false result from a page-global readiness gate.
      uploadReady: typeof wgpuUploadKTX2Texture === "function",
      tokens: Array.isArray(textureVariantTokens) ? textureVariantTokens.slice().sort() : [],
    };

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
      out.ready = !!device && !initFailed;
      out.initFailed = !!initFailed;
      out.initError = initError || "";
      out.resourcesDisposed = rendererResourcesDisposed;
      out.resourceCacheEntries = (
        pointsAuthoredPipelineCache.size +
        pointsAuthoredLayerFailed.size +
        computeParticleSystems.size +
        waterSystems.size +
        instancedCullSystems.size +
        textureCache.size +
        selenaPipelineCache.size +
        selenaComputePipelineCache.size +
        selenaPostPipelineCache.size +
        Object.keys(pipelineCache).length +
        Object.keys(waterPoolPipelineCache).length +
        Object.keys(waterObjectMeshPipelineCache).length
      );
      out.deviceLost = !!lastDeviceLostInfo || !!(base && base.lost);
      // Prefer THIS renderer's own lastDeviceLostInfo (set synchronously by
      // the device.lost handler above, never cleared) over the shared probe
      // snapshot (base.lost), which a successful re-probe nulls out the
      // moment it recovers — often before a watchdog poll gets to read it.
      out.deviceLostInfo = lastDeviceLostInfo || (base && base.lost ? base.lost : null);
      out.frameSeq = webGPUFrameSeq;
      out.frameAt = lastWebGPUFrameStats && lastWebGPUFrameStats.frameAt || 0;
      out.lastError = lastWebGPUFrameStats && lastWebGPUFrameStats.lastError || "";
      out.waterSimulationTickSeq = lastWebGPUFrameStats && lastWebGPUFrameStats.waterSimulationTickSeq || 0;
      out.waterSolverSubstepSeq = lastWebGPUFrameStats && lastWebGPUFrameStats.waterSolverSubstepSeq || 0;
      out.waterDroppedTicks = lastWebGPUFrameStats && lastWebGPUFrameStats.waterDroppedTicks || 0;
      out.waterNormalDispatchSeq = lastWebGPUFrameStats && lastWebGPUFrameStats.waterNormalDispatchSeq || 0;
      out.waterSampledStateSyncSeq = lastWebGPUFrameStats && lastWebGPUFrameStats.waterSampledStateSyncSeq || 0;
      out.postProcessing = !!postProcessor;
      // Frame-error resilience state (see reportWebGPUFrameError /
      // disablePostProcessing / enablePostProcessing above and
      // 20-scene-mount.js's checkSceneWebGPUFrameErrorWatchdog, the poller
      // that acts on these).
      out.frameErrorStreak = webGPUConsecutiveFrameErrors;
      out.frameCleanStreak = webGPUConsecutiveCleanFrames;
      out.postFXDisabled = postFXForceDisabled;
      out.customMaterialFallbacks = lastWebGPUFrameStats && lastWebGPUFrameStats.customMaterialFallbacks || 0;
      out.customMaterialFallbackReason = out.customMaterialFallbacks > 0 ? "custom-wgsl-hooks-unsupported" : "";
      out.skinnedMeshObjects = lastWebGPUFrameStats && lastWebGPUFrameStats.skinnedMeshObjects || 0;
      out.computedMorphDispatches = lastWebGPUFrameStats && lastWebGPUFrameStats.computedMorphDispatches || 0;
      out.computedMorphVertices = lastWebGPUFrameStats && lastWebGPUFrameStats.computedMorphVertices || 0;
      out.computedMorphKernel = lastWebGPUFrameStats && lastWebGPUFrameStats.computedMorphKernel || "";
      out.elioSkinningDispatches = lastWebGPUFrameStats && lastWebGPUFrameStats.elioSkinningDispatches || 0;
      out.elioSkinningVertices = lastWebGPUFrameStats && lastWebGPUFrameStats.elioSkinningVertices || 0;
      out.elioSkinningKernel = lastWebGPUFrameStats && lastWebGPUFrameStats.elioSkinningKernel || "";
      // GPU picking. gpuPicking stays true whether or not a pick has run yet —
      // it reports the renderer capability, matching the gpu-picking cell in
      // 16a-scene-webgpu.capabilities.json.
      out.gpuPicking = true;
      if (scenePicker) Object.assign(out, scenePicker.diagnostics());
      // Render truth: implementation identity, the post-chain dispatch record
      // and the event journal, so a single diagnostics() call is a complete
      // dump rather than a starting point for DOM scraping.
      var truthApi = renderTruth();
      out.implementation = truthApi.implementation(out.adapterInfo || {});
      out.browserEngine = typeof truthApi.browserEngine === "function" ? truthApi.browserEngine() : "";
      out.renderTruthEvents = typeof truthApi.events === "function" ? truthApi.events() : [];
      out.shaderDiagnostics = typeof truthApi.shaderCounts === "function" ? truthApi.shaderCounts() : { messages: 0, errors: 0 };
      out.postChain = lastWebGPUFrameStats && lastWebGPUFrameStats.postChain || null;
      out.uniformTime = selenaFrame.time;
      out.textureVariantContext = {
        backend: textureVariantContext.backend,
        uploadReady: textureVariantContext.uploadReady,
        tokens: textureVariantContext.tokens.slice(),
      };
      out.ibl = Object.assign({}, iblResources.diagnostics);
      out.retainedGeometry = webGPURetainedMeshBufferStats();
      return out;
    }

    return {
      kind: "webgpu",
      type: "webgpu",
      supportsRetainedGeometry: true,
      supportsBundle: supportsBundle,
      queuePick: queuePick,
      setLifecycle: setLifecycle,
      pollPerformanceSample: pollPerformanceSample,
      getPerformanceTimingStatus: getPerformanceTimingStatus,
      diagnostics: diagnostics,
      render: render,
      dispose: dispose,
      disablePostProcessing: disablePostProcessing,
      enablePostProcessing: enablePostProcessing,
      textureVariantContext: textureVariantContext,
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
  //   - 16z-scene-webgpu-probe.ts (main scene3d bundle) — owns the
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

  // Open the KTX2 variant-swap gate.
  //
  // 19-scene-gltf.js swaps an image URI for a block variant only when
  // sceneKTX2UploadPathReady() answers true, and that reads this global. The
  // rule mirrors the Go side, which refuses a variant whose file was never
  // built: a renderer that cannot upload a block container must keep serving
  // the PNG or JPEG, or the swap trades a working texture for a broken one.
  //
  // Register the WebGPU uploader here, at the end of the renderer file, so the
  // gate opens only when this renderer is really present. Reading the flag
  // rather than the function keeps 19-scene-gltf.js free of a backend choice.
  if (typeof window !== "undefined") {
    window.__gosx_scene3d_ktx2_texture_loader = wgpuUploadKTX2Texture;
  }
