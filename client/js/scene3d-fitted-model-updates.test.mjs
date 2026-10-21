import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const source = fs.readFileSync(path.join(root, 'client/runtime/scene3d/mount-webgl.ts'), 'utf8');
function functions(first, next) {
  return source.slice(source.indexOf('  function ' + first + '('), source.indexOf('  function ' + next + '('));
}
function fixture() {
  const context = vm.createContext({ window: {}, console, Float32Array, Map, Set, WeakMap });
  for (const name of ['10-runtime-primitives.ts', '10-runtime-scene-utils.ts',
    '11-scene-math.ts', '12-scene-geometry.ts', '13-scene-material.ts', '10-runtime-scene-core.ts']) {
    let text = fs.readFileSync(path.join(root, 'client/js/bootstrap-src', name), 'utf8');
    if (name === '10-runtime-scene-core.ts') text = text.slice(0, text.indexOf('// Scene3D shared API'));
    vm.runInContext(text, context, { filename: name });
  }
  context.sceneHydrationModels = state => state.models;
  context.sceneOwns = (value, key) => Object.prototype.hasOwnProperty.call(value, key);
  vm.runInContext(
    functions('sceneModelLocalTransformVector', 'sceneModelMaxScale') +
    functions('sceneModelTransformMatrix', 'sceneModelIdentityBindMatrices') +
    functions('sceneModelBoundsNumber', 'sceneModelExpandPositionArray') +
    functions('sceneStaticModelHydrationKey', 'sceneCommitRigidInstancePatch') +
    functions('sceneCommitRigidInstancePatch', 'sceneUpdateRigidInstanceMotion') +
    functions('sceneRigidMembershipModelID', 'sceneRigidMembershipSnapshotModel'),
    context,
  );
  vm.runInContext([
    "const model = normalizeSceneModel({ id: 'float-duck', src: '/duck.gltf', bounds: .65, fit: 'contain',",
    "fitAlign: 'center-min-y', x: .4, y: -.1, z: -.2, scaleX: 1, scaleY: 1, scaleZ: 1 }, 0);",
    "const asset = { bounds: { minX: -20, maxX: 60, minY: 0, maxY: 100, minZ: -40, maxZ: 40 } };",
    "const fitted = sceneModelWithAssetFit(model, asset);",
    "const object = { id: 'float-duck/mesh', vertices: { immutable: true, revision: 0 }, parentMatrix: sceneModelTransformMatrix(fitted) };",
    "const staged = { model: fitted, rigidInstanceModel: fitted, objects: [object],",
    "modelSkins: [], modelAnimations: [], points: [], labels: [], sprites: [], html: [], lights: [] };",
    "const state = { models: [model], objects: new Map([[object.id, object]]) };",
    "const key = sceneRigidInstanceHydrationKey(state, model);",
    "state._hydratedModelRecords = { modelCount: 1, staticModels: new Map(),",
    "rigidInstances: new Map([[key, staged]]), rigidInstancesByID: new Map() };",
  ].join('\n'), context);
  return expression => vm.runInContext(expression, context);
}

test('retained fitted models keep asset scale and alignment on repeated declaration updates', () => {
  const run = fixture();
  const before = [...run('object.parentMatrix')];
  const geometry = run('object.vertices');
  for (let update = 0; update < 12; update++) {
    assert.equal(run('sceneUpdateRigidInstancePoses(state)'), true);
    assert.deepEqual([...run('object.parentMatrix')], before);
    assert.equal(run('object.vertices'), geometry);
    assert.equal(run('staged.rigidInstanceModel._fitScale'), run('fitted._fitScale'));
    assert.equal(run('model.scaleX'), 1, 'the authored declaration must not acquire the fit scale');
  }
});

test('fitted pose changes match fresh hydration with nonuniform scale and rotation', () => {
  const run = fixture();
  run('model.x = -.3; model.y = .2; model.z = .1; model.scaleX = 2; model.scaleY = .75; model.rotationY = .5;');
  const expected = [...run('sceneModelTransformMatrix(sceneModelWithAssetFit(model, asset))')];
  assert.equal(run('sceneUpdateRigidInstancePoses(state)'), true);
  assert.deepEqual([...run('object.parentMatrix')], expected);
  assert.equal(run('staged.rigidInstanceModel._fitScale'), run('fitted._fitScale'));
});

test('a failed multi-model update does not partially change a fitted model', () => {
  const run = fixture();
  const before = [...run('object.parentMatrix')];
  run("model.x = 9; state.models.push(normalizeSceneModel({ id: 'missing', src: '/missing.gltf', scaleX: 1, scaleY: 1, scaleZ: 1 }, 1)); state._hydratedModelRecords.modelCount = 2;");
  assert.equal(run('sceneUpdateRigidInstancePoses(state)'), false);
  assert.deepEqual([...run('object.parentMatrix')], before);
});

test('revalidating a fitted retained snapshot does not apply fitting twice', () => {
  const run = fixture();
  const expected = [...run('object.parentMatrix')];
  run('sceneCommitRigidInstancePatch(scenePrepareRigidInstancePatch(state, staged, fitted, sceneModelTransformMatrix(fitted)))');
  assert.deepEqual([...run('object.parentMatrix')], expected);
  assert.equal(run('staged.rigidInstanceModel._fitScale'), run('fitted._fitScale'));
});

test('root transform commands apply asset fitting once and preserve the authored declaration', () => {
  const run = fixture();
  assert.equal(run("applySceneModelTransformCommand(state, 'float-duck', { x: .9, y: .1 })"), null);
  const expected = [...run('sceneModelTransformMatrix(sceneModelWithAssetFit(state.models[0], asset))')];
  assert.deepEqual([...run('object.parentMatrix')], expected);
  assert.equal(run('state.models[0].scaleX'), 1);
  assert.equal(run('staged.rigidInstanceModel._fitScale'), run('fitted._fitScale'));
});
