  // --- end of 19a-scene-animation.js concatenation ---

  // Publish the public API surface for consumers that want to drive
  // keyframe animations on Scene3D models. Function names mirror the
  // pre-split global identifiers so any future mount code can call
  // through this object with a single dereference.
  window.__gosx_scene3d_animation_api = {
    createMixer: createSceneAnimationMixer,
    buildNodeTransforms: sceneAnimBuildNodeTransforms,
    computeJointMatrices: sceneAnimComputeJointMatrices,
  };

  // Mark chunk loaded for dev tooling / coverage inspection.
  window.__gosx_scene3d_animation_loaded = true;

})();
