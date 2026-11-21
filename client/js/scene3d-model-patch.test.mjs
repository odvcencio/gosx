// PatchModels (command 16, scene.PatchModelsCommand): mounted models change
// their own morph weights, scale and visibility in place. The scene core's
// command switch, the mount's live patch and model-looks.ts run in one VM; the skin
// pose and the reload are recorded, not run. Attachments (scene.Attachment)
// bind gear meshes to a joint of the host skin.
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
    functions('sceneApplyModelLivePatch', 'sceneApplyModelLiveEvent') +
    functions('sceneModelEffectivelyHidden', 'sceneModelRotateDirection') + looks,
    context,
  );
  context.sceneModelMaxScale = () => 1;
  context.sceneModelZeroOpacityHidesObject = () => false;
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

// --- attachments (scene.Attachment) -------------------------------------------

function attachFixture() {
  const context = vm.createContext({ window: {}, console: { warn: (...a) => context.warnings.push(a.join(' ')) }, Float32Array, Map, Set, WeakMap, Promise });
  context.warnings = [];
  for (const name of ['10-runtime-primitives.ts', '10-runtime-scene-utils.ts', '11-scene-math.ts']) {
    vm.runInContext(fs.readFileSync(path.join(root, 'client/js/bootstrap-src', name), 'utf8'), context, { filename: name });
  }
  context.sceneOwns = (value, key) => Boolean(value) && Object.prototype.hasOwnProperty.call(value, key);
  context.gear = { objects: [
    { id: 'mask-cage-prim-0', vertices: { count: 3, positions: new Float32Array(9) } },
    { id: 'visor-prim-0', vertices: { count: 2, positions: new Float32Array(6) } },
  ] };
  context.loadSceneModelAsset = async () => context.gear;
  context.sceneInstantiateModelObject = (raw, model, prefix, index, skins) => Object.assign({}, raw, {
    id: prefix + '/' + index, skin: skins[raw.skinIndex], owner: model,
    _modelHidden: context.sceneModelEffectivelyHidden(model, raw) });
  context.sceneModelMaxScale = () => 1;
  context.sceneModelZeroOpacityHidesObject = () => false;
  vm.runInContext(functions('sceneModelEffectivelyHidden', 'sceneApplyModelObjectHiddenState') + looks, context);
  vm.runInContext([
    "const asset = { nodes: [{ name: 'pelvis' }, { name: 'chest' }, { name: 'head' }] };",
    "const skins = [{ joints: [0, 1, 2] }];",
    "const model = { id: 'home-00', materialOverride: { color: '#ffffff' }, attachments: [",
    "  { name: 'mask', src: '/gear.glb', mesh: 'mask-cage', joint: 'head', hidden: false, material: null },",
    "  { name: 'visor', src: '/gear.glb', mesh: 'visor', joint: 'head', hidden: true, material: { color: '#203040', opacity: 0.4 } },",
    "  { name: 'tail', src: '/gear.glb', mesh: 'visor', joint: 'tail', hidden: false, material: null }] };",
    "const staged = { objects: [] }, ids = [];",
  ].join('\n'), context);
  return expression => vm.runInContext(expression, context);
}

test('attachments bind their meshes wholly to one joint of the host skin', async () => {
  const run = attachFixture();
  await run('sceneInstantiateModelAttachments({}, 1, model, asset, skins, "home-00", staged, ids)');
  assert.deepEqual([...run('ids')], ['home-00/mask/0', 'home-00/visor/1'], 'each attachment takes only its own mesh');
  const mask = run('staged.objects[0]');
  assert.deepEqual([...mask.vertices.joints], [2, 0, 0, 0, 2, 0, 0, 0, 2, 0, 0, 0], 'the head is joint slot 2');
  assert.deepEqual([...mask.vertices.weights], [1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0]);
  assert.equal(run('staged.objects[0].skin === skins[0]'), true, 'the host skin poses it');
  assert.equal(run('gear.objects[0].vertices.joints'), undefined, 'the shared gear asset is not changed');
  assert.equal(mask._attachment, 'mask');
  assert.equal(mask._modelHidden, false);
  assert.equal(run('staged.objects[1]._modelHidden'), true, 'a hidden attachment starts hidden');
  assert.equal(run('staged.objects[0].owner.materialOverride.color'), '#ffffff', "no material: the model's own");
  assert.equal(run('staged.objects[1].owner.materialOverride.opacity'), 0.4, 'its own material');
  assert.match(run('warnings.join("\\n")'), /attachment tail names no joint/);
});

test('a patch shows and hides attachments of a mounted model', () => {
  const run = fixture();
  run("state.models[0].attachments = [{ name: 'mask', hidden: false }, { name: 'visor', hidden: true }]");
  run("lineman.model.attachments = state.models[0].attachments");
  run("lineman.objectIDs = ['home-00/mask/0', 'home-00/visor/1']");
  run("state.objects.set('home-00/mask/0', { id: 'home-00/mask/0', _attachment: 'mask', _modelHidden: false })");
  run("state.objects.set('home-00/visor/1', { id: 'home-00/visor/1', _attachment: 'visor', _modelHidden: true })");
  run("applySceneCommands(state, [{ kind: 16, data: { models: [{ id: 'home-00', attachments: { mask: false, visor: true } }] } }])");
  assert.equal(run("state.objects.get('home-00/mask/0')._modelHidden"), true);
  assert.equal(run("state.objects.get('home-00/visor/1')._modelHidden"), false);
  assert.deepEqual(JSON.parse(run("JSON.stringify(state.models[0].attachments.map(a => a.hidden))")), [true, false], 'the declared model keeps the patch');
});

test('a patch sets material values on the model\'s own objects, not on gear with its own material', () => {
  const run = fixture();
  run("state.models[0].materialOverride = { customUniforms: { skin: [1, 1, 1], jersey: [0, 0, 1] } }");
  run("lineman.objectIDs = ['home-00/body/0', 'home-00/visor/1']");
  run("state.objects.set('home-00/body/0', { id: 'home-00/body/0', customUniforms: { skin: [1, 1, 1], jersey: [0, 0, 1] } })");
  run("state.objects.set('home-00/visor/1', { id: 'home-00/visor/1', _attachment: 'visor', _attachmentMaterial: true, customUniforms: { tint: 1 } })");
  run("var before = state.objects.get('home-00/body/0').customUniforms");
  run("applySceneCommands(state, [{ kind: 16, data: { models: [{ id: 'home-00', uniforms: { skin: [0.4, 0.25, 0.18] } }] } }])");
  assert.deepEqual(JSON.parse(run("JSON.stringify(state.objects.get('home-00/body/0').customUniforms)")), { skin: [0.4, 0.25, 0.18], jersey: [0, 0, 1] });
  assert.equal(run("state.objects.get('home-00/body/0').customUniforms !== before"), true, 'a new values object, so the material profile sees the change');
  assert.deepEqual(JSON.parse(run("JSON.stringify(state.objects.get('home-00/visor/1').customUniforms)")), { tint: 1 });
  assert.equal(run("state.objects.get('home-00/body/0')._matRev"), 1, 'the patch advances the body\'s material revision, which the material profile cache trusts');
  assert.equal(run("state.objects.get('home-00/visor/1')._matRev"), undefined, 'gear with its own material keeps none');
  assert.deepEqual(JSON.parse(run("JSON.stringify(state.models[0].materialOverride.customUniforms)")), { skin: [0.4, 0.25, 0.18], jersey: [0, 0, 1] }, 'the declared model keeps the patch');
});

test('a patch gives a model its own joint scales by joint name and poses it', () => {
  const run = fixture();
  run("lineman.nodes = [{ name: 'hips' }, { name: 'head' }, { name: 'eye' }]");
  run("applySceneCommands(state, [{ kind: 16, data: { models: [{ id: 'home-00', jointScales: { head: 1.08, eye: 1, nose: 2, hips: -1 } }] } }])");
  assert.deepEqual(Array.from(run('lineman.jointScales')), [1, 1.08, 1], 'a factor per node; unnamed nodes, 1 and bad factors stay 1');
  assert.deepEqual({ ...run('state.models[0].jointScales') }, { head: 1.08, eye: 1, nose: 2, hips: -1 }, 'the declared model keeps the patch');
  assert.equal(run('lineman.poseDirty'), true);
  assert.deepEqual([...run('poses')], ['home-00']);
  run("applySceneCommands(state, [{ kind: 16, data: { models: [{ id: 'home-00', jointScales: {} }] } }])");
  assert.equal(run('lineman.jointScales'), null, 'an empty map clears them');
  assert.deepEqual(Array.from(run("sceneModelJointScales([{ name: 'head' }], { head: 1.1 })")), [1.1]);
  assert.equal(run("sceneModelJointScales([{ name: 'head' }], null)"), null);
});

test('a patch with joint scales and a new scale poses once, at the new placement', () => {
  const run = fixture();
  run("lineman.nodes = [{ name: 'hips' }, { name: 'head' }]");
  run("sceneApplyModelSkinPose = record => poses.push(record.model.scaleY)");
  run("applySceneCommands(state, [{ kind: 16, data: { models: [{ id: 'home-00', jointScales: { head: 1.05 }, morphWeights: { mass: 1 }, scaleX: 1.05, scaleY: 1.05, scaleZ: 1.05 }] } }])");
  assert.deepEqual([...run('poses')], [1.05], 'one pose, after the scale');
});


test('gear in the model\'s own material joins the body mesh; gear with its own material stays an object', async () => {
  const run = attachFixture();
  run("window.__gosx_scene3d_gltf_api = { mergeGear(entry, pieces) { entry.merged.push(pieces.map(p => p.count)); } }");
  run("staged.objects.push({ id: 'home-00/body', skin: skins[0], vertices: { count: 6 }, _morphAnim: { merged: [] } })");
  await run('sceneInstantiateModelAttachments({}, 1, model, asset, skins, "home-00", staged, ids)');
  assert.deepEqual([...run('ids')], ['home-00/visor/1'], 'only the visor, which has its own material, is an object of its own');
  assert.deepEqual(JSON.parse(run('JSON.stringify(staged.objects[0]._morphAnim.merged)')), [[3]], 'the shown mask joins the body; the hidden visor is not merged');
  run("lineman = { id: 'home-00', model, objectIDs: ['home-00/body'] }");
  run("state = { objects: new Map([['home-00/body', staged.objects[0]]]) }");
  run("scenePatchModelAttachments(state, lineman, [{ name: 'mask', hidden: true }, { name: 'visor', hidden: false }, { name: 'tail', hidden: true }])");
  assert.deepEqual(JSON.parse(run('JSON.stringify(staged.objects[0]._morphAnim.merged)')), [[3], []], 'hiding the mask takes it out of the body again');
});

test('the WebGPU material signature is rebuilt only when a field of the material changed, in place or nested', () => {
  const source = createRequire(import.meta.url)('./scene3d-renderer-source-set.js').readSceneRendererBackendSrc('webgpu');
  const from = source.indexOf('  var webGPUSignatureReads');
  const to = source.indexOf('  function webGPUPrepareMaterialFrame(');
  assert.ok(from > 0 && to > from);
  const context = vm.createContext({ window: { __gosx_scene3d_counters: {} }, JSON, Object, WeakMap, Array });
  vm.runInContext('var materialCacheFields = ["color", "roughness", "textureDescriptors"];' + source.slice(from, to).replace(/: any\)/g, ')').replace(/\): string \{/g, ') {').replace(/value: any/g, 'value'), context);
  const sign = (m) => vm.runInContext('webGPUMaterialSignature', context)(m);
  const builds = () => context.window.__gosx_scene3d_counters.materialSignatures;
  const material = { color: '#fff', roughness: 0.5, textureDescriptors: { albedo: { wrap: 'repeat' } } };
  const first = sign(material);
  assert.equal(sign(material), first);
  assert.equal(sign(material), first);
  assert.equal(builds(), 1, 'an unchanged material is not rebuilt');
  material.color = '#000';
  const second = sign(material);
  assert.notEqual(second, first, 'an edit in place shows');
  material.textureDescriptors.albedo.wrap = 'clamp';
  assert.notEqual(sign(material), second, 'an edit of a nested descriptor shows');
  assert.equal(builds(), 3);
});
