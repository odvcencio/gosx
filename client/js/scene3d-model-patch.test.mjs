// PatchModels (command 16, scene.PatchModelsCommand): mounted models change
// their own morph weights, scale and visibility in place. The scene core's
// command switch, the mount's live patch and model-looks.ts run in one VM; the skin
// pose and the reload are recorded, not run.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const ts = createRequire(import.meta.url)('../runtime/node_modules/typescript');
const mount = fs.readFileSync(path.join(root, 'client/runtime/scene3d/mount-webgl.ts'), 'utf8');
const looks = ts.transpileModule(fs.readFileSync(path.join(root, 'client/runtime/scene3d/model-looks.ts'), 'utf8'),
  { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
function functions(first, next) {
  const from = mount.indexOf('  function ' + first + '(');
  const to = mount.indexOf('  function ' + next + '(');
  assert.ok(from >= 0 && to > from, first + ' .. ' + next);
  return mount.slice(from, to);
}

function fixture() {
  const context = vm.createContext({ window: {}, console, Float32Array, Map, Set, WeakMap });
  for (const name of ['10-runtime-primitives.ts', '10-runtime-scene-utils.ts',
    '11-scene-math.ts', '12-scene-geometry.ts', '13-scene-material.ts', '10-runtime-scene-core.ts']) {
    let text = fs.readFileSync(path.join(root, 'client/js/bootstrap-src', name), 'utf8');
    if (name === '10-runtime-scene-core.ts') text = text.slice(0, text.indexOf('// Scene3D shared API'));
    vm.runInContext(text, context, { filename: name });
  }
  context.sceneOwns = (value, key) => Boolean(value) && Object.prototype.hasOwnProperty.call(value, key);
  context.poses = [];
  context.reloads = 0;
  context.sceneApplyModelSkinPose = (record) => { context.poses.push(record.id); };
  context.sceneRehydrateModelsAfterCommand = () => { context.reloads += 1; return Promise.resolve('reloading'); };
  context.sceneApplyModelComputedPose = () => false;
  context.sceneApplyStaticModelObjectTransform = () => {};
  vm.runInContext(
    functions('sceneModelLocalTransformVector', 'sceneModelMaxScale') +
    functions('sceneModelTransformMatrix', 'sceneModelIdentityBindMatrices') +
    functions('sceneApplyModelLiveOpacity', 'sceneApplyModelComputedPose') +
    functions('sceneApplyModelLivePatch', 'sceneApplyModelLiveEvent') + looks,
    context,
  );
  vm.runInContext([
    "function record(id, morphs, animated) {",
    "  return { id, model: { id, morphWeights: morphs ? { mass: 0 } : null, x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0, scaleX: 1, scaleY: 1, scaleZ: 1, visible: true },",
    "    morphTargets: morphs ? [{ skinned: true }] : undefined, objectIDs: [] };",
    "}",
    "const lineman = record('home-00', true), back = record('home-01', true), plain = record('home-02', false);",
    "const state = { objects: new Map(), models: [",
    "  { id: 'home-00', src: '/a.glb', morphWeights: { mass: 0 } }, { id: 'home-01', src: '/a.glb', morphWeights: { mass: 0 } },",
    "  { id: 'home-02', src: '/a.glb', morphWeights: null }],",
    "  _modelSkins: [lineman, back, plain], _modelAnimations: [back] };",
  ].join('\n'), context);
  return expression => vm.runInContext(expression, context);
}

test('a patch changes one model\'s weights in place and poses it', () => {
  const run = fixture();
  const result = run("applySceneCommands(state, [{ kind: 16, data: { models: [{ id: 'home-00', morphWeights: { mass: .8, belly: .3 } }] } }])");
  assert.equal(result, null, 'no reload');
  assert.deepEqual({ ...run('lineman.model.morphWeights') }, { mass: .8, belly: .3 });
  assert.deepEqual({ ...run('state.models[0].morphWeights') }, { mass: .8, belly: .3 }, 'the declared model keeps the patch');
  assert.equal(run('lineman.poseDirty'), true);
  assert.deepEqual([...run('poses')], ['home-00'], 'a model the animation loop does not visit is posed at once');
  assert.deepEqual({ ...run('back.model.morphWeights') }, { mass: 0 }, 'other models keep theirs');
  assert.equal(run('reloads'), 0);
});

test('an animated model folds on its next pass, and an empty map clears the weights', () => {
  const run = fixture();
  run("applySceneCommands(state, [{ kind: 16, data: { models: [{ id: 'home-01', morphWeights: {} }] } }])");
  assert.deepEqual({ ...run('back.model.morphWeights') }, {});
  assert.equal(run('back.poseDirty'), true);
  assert.equal(run('poses.length'), 0, 'the animation loop poses it');
});

test('a model declared without weights loads again to keep its targets', () => {
  const run = fixture();
  const result = run("applySceneCommands(state, [{ kind: 16, data: { models: [{ id: 'home-02', morphWeights: { mass: 1 } }] } }])");
  assert.equal(typeof result.then, 'function', 'the command waits on the reload');
  assert.equal(run('reloads'), 1);
  assert.deepEqual({ ...run('state.models[2].morphWeights') }, { mass: 1 }, 'the reload reads the patched declaration');
});

test('scale and visibility patch the record and its root transform', () => {
  const run = fixture();
  run("applySceneCommands(state, [{ kind: 16, data: { models: [{ id: 'home-00', scaleX: 1.05, scaleY: 1.05, scaleZ: 1.05, visible: false }, { id: 'nobody', visible: false }] } }])");
  assert.equal(run('lineman.model.scaleY'), 1.05);
  assert.equal(run('lineman.model.visible'), false);
  assert.equal(run('state.models[0].scaleY'), 1.05);
  assert.equal(run('state.models[0].visible'), false);
  const rt = Array.from(run('lineman.rootTransform'));
  assert.ok(rt.some(v => Math.abs(v - 1.05) < 1e-6), 'the root transform carries the scale: ' + JSON.stringify(rt));
  assert.equal(run('lineman.poseDirty'), true);
  assert.equal(run('poses.length'), 0, 'no weights, no fold');
});
