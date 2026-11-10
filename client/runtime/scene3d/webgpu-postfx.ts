// Temporal anti-aliasing (and the motion vectors motion blur reads), depth of
// field and auto exposure ship in bootstrap-feature-scene3d-postfx-webgpu.js,
// which a page fetches with the renderer chunk only when a scene declares
// them. These stand-ins keep the renderer's calls in place: each one delegates
// to the chunk once it has loaded and does nothing before that.
function wgpuPostfxHelpers() {
  return { wgpuCreateValidatedPipeline: wgpuCreateValidatedPipeline, wgpuRequirePipeline: wgpuRequirePipeline,
    WGSL_FRAME_STRUCTS: WGSL_FRAME_STRUCTS, WGSL_POINTS_SHADE: WGSL_POINTS_SHADE, WGSL_POINTS_UNIFORM_FIELDS: WGSL_POINTS_UNIFORM_FIELDS };
}
// wgpuPostfxModule returns an object with inert's methods. The first call
// after the chunk lands builds the real module from resources.
function wgpuPostfxModule(name: string, inert: any, resources: () => any) {
  var impl: any = null, out: any = {};
  Object.keys(inert).forEach(function(method: string) {
    out[method] = function() {
      var api = (window as any).__gosx_scene3d_postfx_webgpu_api;
      if (!impl && api && api[name]) impl = api[name](resources, wgpuPostfxHelpers());
      var target = impl || inert;
      return target[method].apply(target, arguments);
    };
  });
  return out;
}
function wgpuPostfxTemporalAA(resources: () => any) {
  return wgpuPostfxModule("temporalAA", { prepare: function() {}, beginFrame: function() { return false; },
    resolve: function(_encoder: any, effects: any[]) { return effects; }, capturePreviousSkins: function() {}, velocityView: function(): any { return null; },
    stats: function() { return { taaActive: 0, taaHistoryValid: 0, taaMovers: 0, taaResets: 0 }; }, dispose: function() {} }, resources);
}
function wgpuPostfxAutoExposure(resources: () => any) {
  return wgpuPostfxModule("autoExposure", { prepare: function() {}, apply: function() { return false; },
    stats: function() { return { autoExposure: 0, autoExposureLuminance: 0 }; }, dispose: function() {} }, resources);
}
// Without the chunk a "dof" effect copies its input.
var WGSL_POST_DOF_PASSTHROUGH = "@group(0) @binding(0) var inputTex: texture_2d<f32>;\n@group(0) @binding(1) var inputSamp: sampler;\n" +
  "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f { return textureSampleLevel(inputTex, inputSamp, uv, 0.0); }";
function wgpuPostfxDOFFragment(): string {
  var api = (window as any).__gosx_scene3d_postfx_webgpu_api;
  return api && api.dofFragment || WGSL_POST_DOF_PASSTHROUGH;
}
function wgpuPostfxDOFParams(effect: any, camera: any, width: number, height: number): Float32Array {
  var api = (window as any).__gosx_scene3d_postfx_webgpu_api;
  return api && api.dofParams ? api.dofParams(effect, camera, width, height) : new Float32Array(8);
}
