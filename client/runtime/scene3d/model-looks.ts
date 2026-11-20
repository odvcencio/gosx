// Per-model looks of mounted Scene3D models: their own morph weights
// (scene.Model.MorphWeights) and in-place changes to a mounted model
// (scene.PatchModelsCommand). The functions share the model mount's scope in
// the scene3d bundles.

  // PatchModels (command 16, scene.PatchModelsCommand) changes mounted models
  // in place: their own morph weights, scale and visibility. The declared
  // model list takes the patch too, so a later reload keeps it. Returns the
  // reload's promise when a model must load again to keep its morph targets.
  function applyScenePatchModelsCommand(state: any, data: any) {
    const patches = data && Array.isArray(data.models) ? data.models : [];
    let reload = false;
    for (let index = 0; index < patches.length; index += 1) {
      const patch = patches[index];
      if (!sceneIsPlainObject(patch) || typeof patch.id !== "string" || !patch.id) continue;
      const declared = scenePatchDeclaredModel(state, patch);
      if (!declared) continue;
      const records = state && Array.isArray(state._modelSkins) ? state._modelSkins : [];
      for (let r = 0; r < records.length; r += 1) {
        const record = records[r];
        if (!record || record.id !== patch.id || !record.model) continue;
        if (sceneOwns(patch, "morphWeights")) reload = scenePatchModelMorphs(state, record, declared.morphWeights) || reload;
        sceneApplyModelLivePatch(state, record, patch);
      }
    }
    return reload && !state._modelHydrationPromise ? sceneRehydrateModelsAfterCommand(state) : null;
  }

  // scenePatchDeclaredModel writes a patch into state.models and returns the
  // new declared model, or null when no model has the patch's ID.
  function scenePatchDeclaredModel(state: any, patch: any) {
    const models = state && Array.isArray(state.models) ? state.models : [];
    const at = models.findIndex(function(model) { return model && model.id === patch.id; });
    if (at < 0) return null;
    const next = Object.assign({}, models[at]);
    if (sceneOwns(patch, "morphWeights")) {
      next.morphWeights = sceneIsPlainObject(patch.morphWeights) ? Object.assign({}, patch.morphWeights) : null;
    }
    for (const key of ["scaleX", "scaleY", "scaleZ"]) {
      if (sceneOwns(patch, key)) next[key] = sceneNumber(patch[key], next[key]);
    }
    if (sceneOwns(patch, "visible")) next.visible = sceneBool(patch.visible, true);
    models[at] = next;
    return next;
  }

  // scenePatchModelMorphs gives a mounted record new morph weights. The
  // next pose folds them. Returns true when the record kept no morph
  // targets (it was declared without weights) and must load again.
  function scenePatchModelMorphs(state: any, record: any, weights: any) {
    const retained = Array.isArray(record.morphTargets) && record.morphTargets.length > 0;
    record.model.morphWeights = weights;
    if (!retained) return Boolean(weights);
    record.poseDirty = true;
    const animated = Array.isArray(state._modelAnimations) && state._modelAnimations.indexOf(record) >= 0;
    if (!animated) sceneApplyModelSkinPose(record, 0, false);
    return false;
  }

