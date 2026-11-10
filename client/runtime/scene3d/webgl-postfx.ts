// Temporal anti-aliasing with its motion vectors, depth of field and auto
// exposure ship in bootstrap-feature-scene3d-postfx-webgl.js, which a page
// fetches with the renderer chunk only when a scene declares them. These
// stand-ins keep the renderer's calls in place: each one delegates to the
// chunk once it has loaded and does nothing before that.
function sceneWebGLPostfxHelpers() {
  return { SCENE_POST_VIEW_POSITION_GLSL: SCENE_POST_VIEW_POSITION_GLSL, createSceneFullscreenQuad: createSceneFullscreenQuad,
    createScenePostFBO: createScenePostFBO, createScenePostProgram: createScenePostProgram, disposeScenePostFBO: disposeScenePostFBO,
    drawSceneFullscreenQuad: drawSceneFullscreenQuad, objectIsSkinned: objectIsSkinned, sceneInvertOrthonormalView: sceneInvertOrthonormalView,
    scenePBRCreateProgramInfo: scenePBRCreateProgramInfo, scenePBRProgramReady: scenePBRProgramReady, scenePBRRenderTruth: scenePBRRenderTruth,
    sceneWaterUniformLocation: sceneWaterUniformLocation };
}
// sceneWebGLPostfxModule returns an object with inert's methods. The first
// call after the chunk lands builds the real module from args.
function sceneWebGLPostfxModule(name: string, inert: any, args: any[]) {
  var impl: any = null, out: any = {};
  Object.keys(inert).forEach(function(method: string) {
    out[method] = function() {
      var api = (window as any).__gosx_scene3d_postfx_webgl_api;
      if (!impl && api && api[name]) impl = api[name].apply(null, args.concat([sceneWebGLPostfxHelpers()]));
      var target = impl || inert;
      return target[method].apply(target, arguments);
    };
  });
  return out;
}
function sceneWebGLPostfxTemporal(gl: any, quad: any) {
  return sceneWebGLPostfxModule("temporal", { prepare: function() { return false; }, motion: function() {},
    resolve: function(input: any) { return input; }, reset: function() {}, ready: function() { return false; }, dispose: function() {},
    valid: function() { return false; } }, [gl, quad]);
}
function sceneWebGLPostfxAutoExposure(gl: any) {
  return sceneWebGLPostfxModule("autoExposure", { apply: function() { return false; }, dispose: function() {} }, [gl]);
}
// Without the chunk a "dof" effect copies its input.
var SCENE_POST_DOF_PASSTHROUGH = "#version 300 es\nprecision highp float;\nin vec2 v_uv;\nuniform sampler2D u_texture;\nout vec4 fragColor;\n" +
  "void main() { fragColor = texture(u_texture, v_uv); }";
function sceneWebGLPostfxDOFSource(): string {
  var api = (window as any).__gosx_scene3d_postfx_webgl_api;
  return api && api.dofSource || SCENE_POST_DOF_PASSTHROUGH;
}
