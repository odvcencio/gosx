  // --- end of 19-scene-gltf.js concatenation ---

  // Publish the public API surface the main scene3d bundle's
  // loadSceneModelAsset helper consumes. Function names match the
  // global identifiers used before the split so the call sites only
  // need to add a dereference through this object.
  window.__gosx_scene3d_gltf_api = {
    sceneLoadGLTFModel: sceneLoadGLTFModel,
    gltfSceneToModelAsset: gltfSceneToModelAsset,
    // The split bundle's suffix republishes the API object AFTER the main
    // glTF chunk, so every public entry must be re-exported here or the
    // split-bundle load silently drops it (gltfApplyAnimatedMorphPose is in
    // scope from the concatenated glTF chunk above).
    applyMorphPose: gltfApplyAnimatedMorphPose,
  };

  // Mark chunk loaded for dev tooling / coverage inspection.
  window.__gosx_scene3d_gltf_loaded = true;

})();
