  // --- end of 16b-scene-compute.js concatenation ---

  // The tail of 16b-scene-compute.js already assigns every published symbol
  // onto window.__gosx_scene3d_api, so the renderers find them there. Publish
  // a named handle too, so ensureComputeFeatureLoaded has one object to wait
  // for and one truthy value to resolve with.
  window.__gosx_scene3d_compute_api = {
    createSceneParticleSystem: createSceneParticleSystem,
    sceneComputeSystemSignature: sceneComputeSystemSignature,
    createSceneInstancedCullSystem: createSceneInstancedCullSystem,
    cullSystemSignature: cullSystemSignature,
    registerSceneParticleForce: registerSceneParticleForce,
    registerSceneParticleForceKind: registerSceneParticleForceKind,
    unregisterSceneParticleForce: unregisterSceneParticleForce,
    listSceneParticleForces: listSceneParticleForces,
  };

  // Mark the chunk loaded for development tooling and coverage inspection.
  window.__gosx_scene3d_compute_loaded = true;

})();
