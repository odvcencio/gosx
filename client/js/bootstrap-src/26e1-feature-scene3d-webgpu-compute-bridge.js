  // Compute-system bridge for the WebGPU sub-feature chunk.
  //
  // 16b-scene-compute.js used to be concatenated into BOTH
  // bootstrap-feature-scene3d.js and bootstrap-feature-scene3d-webgpu.js. A
  // Chromium Scene3D page loads both chunks, so it downloaded and parsed the
  // same 27_651 minified bytes twice. The file now ships once, in the base
  // scene3d chunk, and this bridge gives 16a-scene-webgpu.js the two symbols
  // it reads lexically.
  //
  // The other compute symbols need no bridge: 16b publishes
  // createSceneInstancedCullSystem and cullSystemSignature onto
  // window.__gosx_scene3d_api, and 16a already reads those two through the
  // API object at call time (see updateInstancedCullSystems).
  //
  // The base scene3d chunk always loads before this chunk, so the API object
  // carries both symbols by the time this file runs.
  var createSceneParticleSystem = sceneApi.createSceneParticleSystem;
  var sceneComputeSystemSignature = sceneApi.sceneComputeSystemSignature;
