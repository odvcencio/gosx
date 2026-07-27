  // Compute-system bridge for the WebGPU sub-feature chunk.
  //
  // 16b-scene-compute.js used to be concatenated into BOTH
  // bootstrap-feature-scene3d.js and bootstrap-feature-scene3d-webgpu.js. A
  // Chromium Scene3D page loads both chunks, so it downloaded and parsed the
  // same 27_651 minified bytes twice. The file then shipped once, in the base
  // scene3d chunk. It now ships in its own chunk,
  // bootstrap-feature-scene3d-compute.js, which the mount fetches only for a
  // scene that declares compute particles or an instanced mesh.
  //
  // Resolve both names at CALL time, not at load time. The compute chunk can
  // land after this one, so a load-time snapshot would freeze `undefined` and
  // the first particle frame would throw "createSceneParticleSystem is not a
  // function". The thunks below read window.__gosx_scene3d_api on every call,
  // which the compute chunk assigns to when it runs.
  //
  // The other compute symbols need no bridge: 16b publishes
  // createSceneInstancedCullSystem and cullSystemSignature onto
  // window.__gosx_scene3d_api, and 16a already reads those two through the
  // API object at call time (see updateInstancedCullSystems).
  function createSceneParticleSystem(device, entry) {
    var api = window.__gosx_scene3d_api;
    if (!api || typeof api.createSceneParticleSystem !== "function") {
      return null;
    }
    return api.createSceneParticleSystem(device, entry);
  }

  function sceneComputeSystemSignature(entry) {
    var api = window.__gosx_scene3d_api;
    if (!api || typeof api.sceneComputeSystemSignature !== "function") {
      return "";
    }
    return api.sceneComputeSystemSignature(entry);
  }

  // sceneShaderModuleError and sceneReportPipelineFailure are per-object
  // shader validation helpers 16b owns (see the block comment on
  // sceneShaderModuleError in 16b-scene-compute.js). 16a's custom-post,
  // points-authored and particle-render-authored pipeline builds call both
  // at pipeline-settle time, so they need the same call-time bridge as the
  // two thunks above.
  function sceneShaderModuleError(modules) {
    var api = window.__gosx_scene3d_api;
    if (!api || typeof api.sceneShaderModuleError !== "function") {
      return Promise.resolve(null);
    }
    return api.sceneShaderModuleError(modules);
  }

  function sceneReportPipelineFailure(stage, id, reason) {
    var api = window.__gosx_scene3d_api;
    if (!api || typeof api.sceneReportPipelineFailure !== "function") {
      return;
    }
    api.sceneReportPipelineFailure(stage, id, reason);
  }
