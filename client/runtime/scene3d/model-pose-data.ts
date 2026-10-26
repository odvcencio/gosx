// Shared imported-model pose commits and the data callbacks consumed by the
// optional command runtime. This module performs no GPU resource operations.
interface SceneRetainedPoseBindings {
  scopeKey: (state: any) => string;
  templates: WeakMap<object, unknown>;
  expand: (batch: any, index: number) => any[];
  clone: (model: any) => any;
  matrix: (model: any) => any;
  eligible: (model: any, matrix: any) => boolean;
  local: (model: any) => boolean;
  prepare: (state: any, staged: any, model: any, matrix: any) => any;
  commit: (patch: any) => void;
  number: (value: any, fallback: number) => number;
}
let sceneRetainedBinaryPoseBindings: SceneRetainedPoseBindings | null = null;

function sceneGetRetainedBinaryPoseBindings(): SceneRetainedPoseBindings {
  return sceneRetainedBinaryPoseBindings || (sceneRetainedBinaryPoseBindings = {
    scopeKey: sceneRigidMembershipScopeKey,
    templates: sceneInstancedGLBHydrationTemplates,
    expand: sceneInstancedGLBMeshToModels,
    clone: sceneCloneHydrationModel,
    matrix: sceneModelTransformMatrix,
    eligible: sceneRigidInstanceHydrationEligible,
    local: model => !model.parentNode && !model.parentMatrix && !sceneInstancedGLBTints.has(model),
    prepare: scenePrepareRigidInstancePatch,
    commit: sceneCommitRigidInstancePatch,
    number: sceneNumber,
  });
}

function sceneCommitImportedModelPose({ staged, model, matrix, crowdRows }: any, applySkinPose: (record: any, time: number, force: boolean) => void): void {
  if (staged.modelSkins?.length) {
    const record = staged.modelSkins[0];
    record.model = model;
    record.rootTransform = matrix;
    applySkinPose(record, 0, false);
  } else {
    for (const object of staged.objects) object.parentMatrix = matrix;
  }
  if (crowdRows) sceneCommitCrowdPoseUpdates(crowdRows);
  staged.rigidInstanceModel = staged.model = model;
}
