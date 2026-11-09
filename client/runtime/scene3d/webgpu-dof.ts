// WebGPU depth of field: the same single-pass circle-of-confusion gather as
// the WebGL2 pass, so one DOF{FocusDistance, Aperture, MaxBlur} looks the same
// on both backends. Depth is linearized from the [0, 1] depth buffer (WebGPU
// and remapped GL projections store the same value, f(1 - n/z)/(f - n)).
// DOFParams: focusDistance, aperture, maxBlur, near, texelSize.xy, far, pad.
// Kept as an array of lines like the other WebGPU post shaders.
var WGSL_POST_DOF_FRAGMENT = [
  "struct DOFParams {",
  "    focusDistance: f32,",
  "    aperture: f32,",
  "    maxBlur: f32,",
  "    near: f32,",
  "    texelSize: vec2f,",
  "    far: f32,",
  "    _pad: f32,",
  "};",
  "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
  "@group(0) @binding(1) var inputSamp: sampler;",
  "@group(0) @binding(2) var depthTex: texture_depth_2d;",
  "@group(0) @binding(3) var<uniform> params: DOFParams;",
  "const GOLDEN_ANGLE: f32 = 2.39996323;",
  "const DOF_TAPS: i32 = 48;",
  "fn linearDepthAt(uv: vec2f) -> f32 {",
  "    let dims = vec2f(textureDimensions(depthTex));",
  "    let d = textureLoad(depthTex, vec2i(clamp(uv * dims, vec2f(0.0), dims - vec2f(1.0))), 0);",
  "    let z = d * 2.0 - 1.0;",
  "    return (2.0 * params.near * params.far) / max(0.0001, params.far + params.near - z * (params.far - params.near));",
  "}",
  "fn cocOf(depth: f32, maxBlur: f32) -> f32 {",
  "    return clamp(abs(depth - params.focusDistance) * params.aperture * maxBlur, 0.0, maxBlur);",
  "}",
  "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
  "    let maxBlur = clamp(params.maxBlur, 0.0, 64.0);",
  "    let centerDepth = linearDepthAt(uv);",
  "    let centerCoC = cocOf(centerDepth, maxBlur);",
  "    var color = textureSampleLevel(inputTex, inputSamp, uv, 0.0).rgb;",
  "    var total = 1.0;",
  "    let stride = maxBlur * maxBlur / f32(2 * DOF_TAPS);",
  "    var radius = stride;",
  "    var angle = 0.0;",
  "    for (var i = 0; i < DOF_TAPS; i = i + 1) {",
  "        if (radius >= maxBlur || maxBlur < 0.5) { break; }",
  "        let tapUV = uv + vec2f(cos(angle), sin(angle)) * params.texelSize * radius;",
  "        let sampleColor = textureSampleLevel(inputTex, inputSamp, tapUV, 0.0).rgb;",
  "        let sampleDepth = linearDepthAt(tapUV);",
  "        var sampleCoC = cocOf(sampleDepth, maxBlur);",
  "        if (sampleDepth > centerDepth) { sampleCoC = min(sampleCoC, centerCoC * 2.0); }",
  "        color = color + mix(color / total, sampleColor, smoothstep(radius - 0.5, radius + 0.5, sampleCoC));",
  "        total = total + 1.0;",
  "        radius = radius + stride / radius;",
  "        angle = angle + GOLDEN_ANGLE;",
  "    }",
  "    return vec4f(color / total, 1.0);",
  "}",
].join("\n");

// wgpuDOFParams packs DOFParams for one frame from the effect and the camera.
function wgpuDOFParams(effect: any, camera: any, width: number, height: number): Float32Array {
  var cam = camera || {};
  return new Float32Array([
    Math.max(0, sceneNumber(effect.focusDistance, 8)), sceneNumber(effect.aperture, 0.04), sceneNumber(effect.maxBlur, 8),
    Math.max(0.0001, sceneNumber(cam.near, 0.1)), 1 / Math.max(1, width), 1 / Math.max(1, height), Math.max(0.1, sceneNumber(cam.far, 1000)), 0,
  ]);
}
