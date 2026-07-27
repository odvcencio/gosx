  // --- end of 11a and 11b concatenation ---

  // Publish onto the base API object. 10-runtime-scene-core.js reads
  // sceneDecompressProps through it, and 20-scene-mount.js reads
  // sceneUpgradeProgressive and sceneApplyLOD the same way. Every one of those
  // three call sites already guarded the read, because the monolith and the
  // split chunk had to behave the same before this split existed.
  Object.assign(sceneApi, {
    sceneDecompressProps: sceneDecompressProps,
    sceneUpgradeProgressive: sceneUpgradeProgressive,
    sceneApplyLOD: sceneApplyLOD,
    sceneGeneratePointsEntry: sceneGeneratePointsEntry,
    sceneDecompressPointsEntry: sceneDecompressPointsEntry,
    sceneDecompressInstancedMeshEntry: sceneDecompressInstancedMeshEntry,
    sceneDecompressAnimationChannel: sceneDecompressAnimationChannel,
    sceneDecompressArray: sceneDecompressArray,
  });

  window.__gosx_scene3d_decompress_api = sceneApi;

  // Mark the chunk loaded for development tooling and coverage inspection.
  window.__gosx_scene3d_decompress_loaded = true;

})();
