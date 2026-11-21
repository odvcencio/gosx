// Per-model looks of mounted Scene3D models: their own morph weights
// (scene.Model.MorphWeights) and joint scales (scene.Model.JointScales),
// gear on their joints (scene.Attachment), material values (a Selena
// material's params) and in-place changes to a mounted model
// (scene.PatchModelsCommand). The functions share the model mount's scope
// in the scene3d bundles.

  // PatchModels (command 16, scene.PatchModelsCommand) changes mounted models
  // in place: their own morph weights, joint scales, scale, visibility,
  // material values and the visibility of their attachments. The declared model list takes
  // the patch too, so a later reload keeps it. Returns the
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
        let repose = false;
        if (sceneOwns(patch, "morphWeights")) {
          const retained = scenePatchModelMorphs(record, declared.morphWeights);
          reload = (!retained && Boolean(declared.morphWeights)) || reload;
          repose = retained;
        }
        if (sceneOwns(patch, "jointScales")) repose = scenePatchModelJoints(record, declared.jointScales) || repose;
        if (sceneOwns(patch, "attachments")) scenePatchModelAttachments(state, record, declared.attachments);
        if (sceneIsPlainObject(patch.uniforms)) scenePatchModelUniforms(state, record, patch.uniforms);
        sceneApplyModelLivePatch(state, record, patch);
        // Every field first, then one pose at the record's new placement.
        if (repose) sceneReposeModel(state, record);
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
    if (sceneOwns(patch, "jointScales")) next.jointScales = sceneIsPlainObject(patch.jointScales) ? Object.assign({}, patch.jointScales) : null;
    if (sceneOwns(patch, "visible")) next.visible = sceneBool(patch.visible, true);
    if (sceneIsPlainObject(patch.uniforms) && sceneIsPlainObject(next.materialOverride)) {
      const values = Object.assign({}, next.materialOverride.customUniforms, patch.uniforms);
      next.materialOverride = Object.assign({}, next.materialOverride, { customUniforms: values });
    }
    if (sceneIsPlainObject(patch.attachments) && Array.isArray(next.attachments)) {
      next.attachments = next.attachments.map(function(entry: any) {
        return entry && sceneOwns(patch.attachments, entry.name) ? Object.assign({}, entry, { hidden: patch.attachments[entry.name] === false }) : entry;
      });
    }
    models[at] = next;
    return next;
  }

  // scenePatchModelMorphs gives a mounted record new morph weights; the
  // next pose folds them. Returns false when the record kept no morph
  // targets (it was declared without weights) and must load again.
  function scenePatchModelMorphs(record: any, weights: any) {
    record.model.morphWeights = weights;
    return Array.isArray(record.morphTargets) && record.morphTargets.length > 0;
  }

  // The next frame poses record again; a record no clip animates poses now.
  function sceneReposeModel(state: any, record: any) {
    record.poseDirty = true;
    const animated = Array.isArray(state._modelAnimations) && state._modelAnimations.indexOf(record) >= 0;
    if (!animated) sceneApplyModelSkinPose(record, 0, false);
  }

  // A model's own joint scales (scene.Model.JointScales) as a factor per
  // node (1 for none) for the pose paths in animation.ts and
  // animation-graph.ts, or null when no node is scaled. A new table
  // replaces the old one; the graph notices the change by reference.
  function sceneModelJointScales(nodes: any, scales: any) {
    if (!sceneIsPlainObject(scales) || !Array.isArray(nodes)) return null;
    const table = new Float64Array(nodes.length).fill(1);
    let any = false;
    for (let i = 0; i < nodes.length; i += 1) {
      const name = nodes[i] && nodes[i].name;
      const f = typeof name === "string" && sceneOwns(scales, name) ? Number(scales[name]) : NaN;
      if (f > 0 && Number.isFinite(f) && f !== 1) { table[i] = f; any = true; }
    }
    return any ? table : null;
  }

  // scenePatchModelJoints gives a mounted record new joint scales. Returns
  // true when the record has a skeleton to pose again.
  function scenePatchModelJoints(record: any, scales: any) {
    record.model.jointScales = scales;
    if (!Array.isArray(record.nodes)) return false;
    record.jointScales = sceneModelJointScales(record.nodes, scales);
    return true;
  }


  // scenePatchModelAttachments shows and hides a mounted record's
  // attachments on the next frame.
  function scenePatchModelAttachments(state: any, record: any, attachments: any) {
    record.model.attachments = attachments;
    const ids = Array.isArray(record.objectIDs) ? record.objectIDs : [];
    for (let i = 0; i < ids.length; i += 1) {
      const object = state.objects && state.objects.get ? state.objects.get(ids[i]) : null;
      if (object && object._attachment) sceneApplyModelObjectHiddenState(object, record.model);
    }
  }

  // Attachments (scene.Attachment): meshes of another glTF fixed to one joint
  // of a model's skin. Each is authored where it sits on the model in its
  // bind pose and is bound wholly to the joint, so the joint matrices that
  // pose the body pose it, on the same frame, with no per-frame work.
  async function sceneInstantiateModelAttachments(state: any, generation: any, model: any, asset: any, skinInstances: any, prefix: any, staged: any, objectIDs: any) {
    const list = Array.isArray(model.attachments) ? model.attachments : [];
    for (let a = 0; a < list.length; a += 1) {
      const attach = list[a];
      const bind = attach ? sceneAttachmentJoint(asset, skinInstances, attach.joint) : null;
      if (!bind) {
        if (attach) console.warn("[gosx] attachment " + attach.name + " names no joint of " + (model.id || "a model") + "'s skin:", attach.joint);
        continue;
      }
      const gear = await loadSceneModelAsset(attach.src, state && state._modelStatusMount, {
        state, generation, modelID: model.id || "", stage: "attachment", variantScope: state && state._modelTextureVariantScope,
      });
      const owner = Object.assign({}, model, { _attachment: attach.name }, attach.material ? { materialOverride: attach.material } : null);
      const objects = gear && Array.isArray(gear.objects) ? gear.objects : [];
      for (let i = 0; i < objects.length; i += 1) {
        const raw = objects[i];
        if (!raw || !raw.vertices || raw.skin || (attach.mesh && String(raw.id || "").indexOf(attach.mesh + "-prim-") !== 0)) continue;
        const object = sceneInstantiateModelObject(sceneAttachmentBound(raw, bind), owner, prefix + "/" + attach.name, i, skinInstances);
        if (!object) continue;
        object._attachment = attach.name;
        if (attach.material) object._attachmentMaterial = true;
        staged.objects.push(object);
        objectIDs.push(object.id);
      }
    }
  }

  // The skin and joint slot of the node named joint, or null.
  function sceneAttachmentJoint(asset: any, skinInstances: any, joint: any) {
    const nodes = asset && Array.isArray(asset.nodes) ? asset.nodes : [];
    for (let s = 0; s < skinInstances.length; s += 1) {
      const joints = skinInstances[s] && skinInstances[s].joints;
      for (let j = 0; joints && j < joints.length; j += 1) {
        if (nodes[joints[j]] && nodes[joints[j]].name === joint) return { skin: s, slot: j };
      }
    }
    return null;
  }

  // A copy of an attachment's primitive bound wholly to one joint.
  function sceneAttachmentBound(raw: any, bind: any) {
    const count = Math.max(0, Math.floor(sceneNumber(raw.vertices.count, 0)));
    const joints = new Float32Array(count * 4);
    const weights = new Float32Array(count * 4);
    for (let v = 0; v < count; v += 1) {
      joints[v * 4] = bind.slot;
      weights[v * 4] = 1;
    }
    return Object.assign({}, raw, { skinIndex: bind.skin, vertices: Object.assign({}, raw.vertices, { joints, weights }) });
  }

  // True when object is an attachment its model hides.
  function sceneModelAttachmentHidden(model: any, object: any) {
    const name = object && object._attachment || model && model._attachment;
    if (!name || !model || !Array.isArray(model.attachments)) return false;
    const entry = model.attachments.find(function(attach: any) { return attach && attach.name === name; });
    return Boolean(entry && entry.hidden);
  }

  // scenePatchModelUniforms sets material uniform values (a Selena
  // material's params) on every object of a mounted record that draws with
  // the model's own material. Attachments with a material of their own keep
  // theirs. The next frame packs the new values.
  function scenePatchModelUniforms(state: any, record: any, uniforms: any) {
    const ids = Array.isArray(record.objectIDs) ? record.objectIDs : [];
    for (let i = 0; i < ids.length; i += 1) {
      const object = state.objects && state.objects.get ? state.objects.get(ids[i]) : null;
      if (!object || !object.customUniforms || typeof object.customUniforms !== "object" || object._attachmentMaterial) continue;
      object.customUniforms = Object.assign({}, object.customUniforms, uniforms);
      object._matRev = ~~object._matRev + 1;
    }
  }
