
  // --- end of files 11-20 ---

  // Signal that the Scene3D feature chunk has loaded and the engine
  // factory (registered inside 20-scene-mount.js via
  // __gosx_register_engine_factory) is available. The runtime's
  // feature loading system calls __gosx_scene3d_loaded() to resolve
  // any pending engine mounts that were waiting for this chunk.
  window.__gosx_scene3d_available = true;
  if (typeof window.__gosx_scene3d_loaded === "function") {
    window.__gosx_scene3d_loaded();
  }
})();
