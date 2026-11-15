  // Introspection for tests and tools. The browser contract is the three
  // window.__gosx.scene3d functions installed by animation-graph.ts.
  window.__gosx_scene3d_animation_graph_api = {
    version: 1,
    bones: RIG_BONES.slice(),
    parseBoneName: rigParseName,
    mapHumanoid: rigMapHumanoid,
    retargetClips: rigRetargetClips,
    createDecoder: agCreateDecoder,
    createModel: agCreateModel,
    evaluate: agEvaluate,
    defineRig: agDefineRig,
    holder: agHolder,
    copyModel: agCopyModel,
    solveTwoBone: ikTwoBone,
    measure: agMeasure,
  };
  window.__gosx_scene3d_animation_graph_loaded = true;
  // RequireFeature("scene3d-animation-graph") loads this chunk by name and needs a factory.
  if (window.__gosx_register_bootstrap_feature) window.__gosx_register_bootstrap_feature("scene3d-animation-graph", () => ({}));
})();
