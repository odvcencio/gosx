// The WebGL renderer's post-effect stand-ins call these factories with
// the renderer's helper bag last; postfxBind keeps it for the modules above.
window.__gosx_scene3d_postfx_webgl_api = {
  temporal: function(gl, quad, helpers) { postfxBind(helpers); return createSceneTemporalHistory(gl, quad); },
  autoExposure: function(gl, helpers) { postfxBind(helpers); return createSceneWebGLAutoExposure(gl); },
  dofSource: SCENE_POST_DOF_SOURCE,
};
})();
