  // --- end of 19-scene-gltf.js concatenation ---

  // Publish the public API surface the main scene3d bundle's
  // loadSceneModelAsset helper consumes. Function names match the
  // global identifiers used before the split so the call sites only
  // need to add a dereference through this object.
  window.__gosx_scene3d_gltf_api = {
    sceneLoadGLTFModel: sceneLoadGLTFModel,
    gltfSceneToModelAsset: gltfSceneToModelAsset,
  };

  // Mark chunk loaded for dev tooling / coverage inspection.
  window.__gosx_scene3d_gltf_loaded = true;

})();
