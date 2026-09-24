  // --- end of 19a-scene-animation.js concatenation ---

  // Publish the public API surface for consumers that want to drive
  // keyframe animations on Scene3D models. Function names mirror the
  // pre-split global identifiers so any future mount code can call
  // through this object with a single dereference.
  window.__gosx_scene3d_animation_api = {
    createMixer: createSceneAnimationMixer,
    buildCrowdAtlas: sceneBuildCrowdAtlas,
    sampleExplicitAnimation: sceneSampleExplicitAnimation,
    crowdPoseRows: sceneCrowdPoseRows,
    buildNodeTransforms: sceneAnimBuildNodeTransforms,
    computeJointMatrices: sceneAnimComputeJointMatrices,
    wasmClipJSON: sceneAnimWasmClipJSON,
    wasmDecodePose: sceneAnimWasmDecodePose,
    // GPU-driven crowd motion (see animation.ts's "GPU-driven crowd motion"
    // section for the full doc comments this mirrors).
    crowdMotionClipTable: sceneCrowdMotionClipTable,
    crowdMotionClipIndex: sceneCrowdMotionClipIndex,
    crowdMotionWriteRecord: sceneCrowdMotionWriteRecord,
    crowdMotionPoseRows: sceneCrowdMotionPoseRows,
    crowdMotionPoseRowsFromRecord: sceneCrowdMotionPoseRowsFromRecord,
    crowdMotionTransformInto: sceneCrowdMotionTransformInto,
    crowdMotionSweptBoundsInto: sceneCrowdMotionSweptBoundsInto,
    crowdMotionLocalRadius: sceneCrowdMotionLocalRadius,
    crowdMotionRecordFloats: SCENE_CROWD_MOTION_RECORD_FLOATS,
    crowdMotionMaxClips: SCENE_CROWD_MOTION_CLIP_LIMIT,
    crowdMotionExtrapolationSeconds: SCENE_CROWD_MOTION_EXTRAPOLATION_SECONDS,
  };

  // Mark chunk loaded for dev tooling / coverage inspection.
  window.__gosx_scene3d_animation_loaded = true;

})();
