
  // --- end of file 16 ---

  // Publish the two WebGL renderer factories the base chunk calls directly.
  // createSceneWebGLResult (20-scene-mount.js) reads them here instead of
  // holding a lexical reference, because this file runs in its own IIFE.
  //
  // 16-scene-webgl.js also registered the "webgl" backend on
  // sceneBackendRegistry above, so createSceneRendererFromRegistry finds it
  // from now on.
  window.__gosx_scene3d_webgl_api = {
    createScenePBRRendererOrFallback: typeof createScenePBRRendererOrFallback === "function" ? createScenePBRRendererOrFallback : null,
    createSceneWaterRendererWebGL: typeof createSceneWaterRendererWebGL === "function" ? createSceneWaterRendererWebGL : null,
  };

  // Signal that the WebGL chunk is ready. ensureWebGLFeatureLoaded resolves
  // on the script load event and then reads the namespace above.
  window.__gosx_scene3d_webgl_available = true;
  if (typeof window.__gosx_scene3d_webgl_loaded === "function") {
    window.__gosx_scene3d_webgl_loaded();
  }
})();
