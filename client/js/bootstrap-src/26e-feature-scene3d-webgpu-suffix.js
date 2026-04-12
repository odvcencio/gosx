
  // --- end of 16a + 16b concatenation ---

  // Publish the factory to the namespace the main scene3d bundle's stub
  // reads from. The stub's sceneWebGPUAvailable() will start returning
  // true on the next call, and subsequent scene mounts will use WebGPU.
  window.__gosx_scene3d_webgpu_api = {
    createRenderer: createSceneWebGPURenderer,
    available: sceneWebGPUAvailable,
  };

  // Mark chunk loaded for dev tooling / coverage inspection.
  window.__gosx_scene3d_webgpu_loaded = true;

})();
