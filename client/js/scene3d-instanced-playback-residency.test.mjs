import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import {createRequire} from 'node:module';
import {fileURLToPath} from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const ts = createRequire(import.meta.url)('../runtime/node_modules/typescript');
const source = fs.readFileSync(path.join(root, 'client/runtime/scene3d/mount-webgl.ts'), 'utf8');
function functions(first, next) {
  return source.slice(source.indexOf('  function '+first+'('), source.indexOf('  function '+next+'('));
}
function fixture() {
  const c = vm.createContext({console, Float32Array, Map, Set, WeakMap});
  vm.runInContext(fs.readFileSync(path.join(root,'client/js/bootstrap-src/11-scene-math.ts'),'utf8'),c);
  vm.runInContext('const sceneInstancedGLBHydrationTemplates = new WeakMap();',c);
  c.sceneModelTransformMatrix = m => new Float32Array([m.scaleX,0,0,0,0,m.scaleY,0,0,0,0,m.scaleZ,0,m.x,0,0,1]);
  c.sceneHydrationModels = s => s.models;
  c.sceneApplyModelSkinPose = r => {r.sampledPose=r.model._crowdPose;};
  vm.runInContext(functions('sceneStaticModelHydrationKey','sceneCommitRigidInstancePatch')+
    functions('sceneCommitRigidInstancePatch','sceneUpdateRigidInstanceMotion')+
    functions('sceneRigidMembershipModelID','sceneRigidMembershipSnapshotModel'), c);
  vm.runInContext(`
    const model = {id:'debris/slot-0',_instancedGLB:true,scaleX:.1,scaleY:.1,scaleZ:.1,x:7};
    sceneInstancedGLBHydrationTemplates.set(model,'asset-v1');
    const object={id:model.id+'/primitive',vertices:{immutable:true,revision:0},parentMatrix:null};
    const staged={model,rigidInstanceModel:model,objects:[object],modelSkins:[],modelAnimations:[],points:[],labels:[],sprites:[],html:[],lights:[]};
    const state={models:[model],objects:new Map([[object.id,object]])};
    const key=sceneRigidInstanceHydrationKey(state,model);
    state._hydratedModelRecords={modelCount:1,staticModels:new Map(),rigidInstances:new Map([[key,staged]]),rigidInstancesByID:new Map([[model.id,sceneRigidMembershipDescriptor(state,key,staged)]])};
  `, c);
  return { c, run:s=>vm.runInContext(s,c) };
}

test('fading, hidden and retired poses keep model identity and geometry for 1200 frames', () => {
  const {run} = fixture();
  const original = run('object.vertices');
  for (const scale of [.005,.001,0,.1]) {
    for (let frame=0;frame<300;frame++) {
      run(`model.scaleX=model.scaleY=model.scaleZ=${scale}; model.x=${frame>149?-10000:7};`);
      assert.equal(run('sceneUpdateRigidInstancePoses(state)'),true,`scale ${scale}, frame ${frame}`);
      assert.equal(run('state.objects.get(object.id)'),run('object'));
      assert.equal(run('object.vertices'),original);
      assert.equal(run('state._hydratedModelRecords.rigidInstances.size'),1);
    }
  }
});

test('an initially zero-scale instance receives an identity cache key', () => {
  const {run}=fixture();
  run('model.scaleX=model.scaleY=model.scaleZ=0');
  assert.ok(run('sceneRigidInstanceHydrationKey(state,model)'));
});

test('mirrors and ordinary singular models retain the winding/bake path', () => {
  const {run}=fixture();
  run('model.scaleX=-1;model.scaleY=model.scaleZ=1');
  assert.equal(run('sceneRigidInstanceHydrationKey(state,model)'),'');
  run('model.scaleZ=0');
  assert.equal(run('sceneRigidInstanceHydrationKey(state,model)'),'');
  run('model._instancedGLB=false;model.scaleX=0');
  assert.equal(run('sceneRigidInstanceHydrationKey(state,model)'),'');
});

test('uncommitted additions cannot replace a committed identity during partial poses', () => {
  const {run}=fixture();
  run("state._modelHydrationPromise={};model.scaleX=model.scaleY=model.scaleZ=0;state.models.push({id:'debris/new',_instancedGLB:true,scaleX:1,scaleY:1,scaleZ:1,x:42})");
  assert.equal(run("sceneUpdateRigidInstancePoses(state,state.models,[{id:'debris',instances:[{id:'slot-0'},{id:'new'}]}])"),true);
  assert.equal(run('state.objects.size'),1);
});


test('CPU skin playback retains identity, mutable streams and one root transform', () => {
  const {run}=fixture();
  run(`object.vertices.immutable=false; object.vertices.revision=1;
    const playback={model,explicitClips:[],mixer:null,rootTransform:null,poseDirty:false};
    staged.modelSkins.push(playback);`);
  const vertices=run('object.vertices'),record=run('playback');
  for (let frame=0;frame<1200;frame++) {
    run(`model.x=${frame};model._crowdPose={animation:'Walk',animationTime:${frame}/60,animationLoop:true}`);
    assert.equal(run('sceneUpdateRigidInstancePoses(state)'),true);
    assert.equal(run('playback'),record);
    assert.equal(run('object.vertices'),vertices);
    assert.equal(run('playback.rootTransform[12]'),frame);
    assert.equal(run('object.parentMatrix'),null,'CPU world streams must not receive the root transform twice');
    assert.equal(run('playback.model._crowdPose.animationTime'),frame/60);
    assert.equal(run('playback.sampledPose.animationTime'),frame/60);
  }
  assert.equal(run('state._hydratedModelRecords.rigidInstances.size'),1);
});

test('CPU playback replacement remains atomic when a later identity is invalid', () => {
  const {run}=fixture();
  run(`object.vertices.immutable=false;
    const playback={model,explicitClips:[],mixer:null,rootTransform:'before',poseDirty:false};
    staged.modelSkins.push(playback);
    state.models.push({id:'debris/missing',_instancedGLB:true,scaleX:1,scaleY:1,scaleZ:1,x:9});
    state._hydratedModelRecords.modelCount=2;`);
  assert.equal(run('sceneUpdateRigidInstancePoses(state)'),false);
  assert.equal(run('playback.rootTransform'),'before');
  assert.equal(run('playback.poseDirty'),false);
});

function packedFixture() {
  const c = vm.createContext({window:{},console,Float32Array,Map,Set,WeakMap,TextDecoder,
    Uint8Array,ArrayBuffer,DataView,Date,Promise,setTimeout});
  for (const name of ['10-runtime-primitives.ts','10-runtime-scene-utils.ts',
    '11-scene-math.ts','12-scene-geometry.ts','13-scene-material.ts','10-runtime-scene-core.ts']) {
    let text = fs.readFileSync(path.join(root,'client/js/bootstrap-src',name),'utf8');
    if (name === '10-runtime-scene-core.ts') text = text.slice(0,text.indexOf('// Scene3D shared API'));
    vm.runInContext(text,c,{filename:name});
  }
  vm.runInContext(functions('sceneStaticModelHydrationKey','sceneCommitRigidInstancePatch')+
    functions('sceneCommitRigidInstancePatch','sceneUpdateRigidInstanceMotion')+
    functions('sceneRigidMembershipModelID','sceneRigidMembershipSnapshotModel')+
    functions('sceneHydrationModels','sceneClearHydratedModelRecords'),c);
  c.sceneModelTransformMatrix = model => c.sceneObjectModelMatrix(model,0);
  c.sceneApplyModelSkinPose = record => {record.sampledPose=record.model._crowdPose;};
  vm.runInContext(ts.transpileModule(fs.readFileSync(path.join(root,'client/runtime/scene3d/command-runtime.ts'),'utf8'),
    {compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText,c);
  const run = text => vm.runInContext(text,c);
  run(`
    const actors=normalizeSceneInstancedGLBMeshEntry({id:'actors',src:'actors.glb',
      instances:[{id:'a',x:1,animation:'Walk'},{id:'b',x:2,animation:'Walk'}]},0);
    const scenery=normalizeSceneInstancedGLBMeshEntry({id:'scenery',src:'scenery.glb',
      instances:Array.from({length:1000},(_,i)=>({id:'prop-'+i,x:i}))},1);
    const effects=normalizeSceneInstancedGLBMeshEntry({id:'effects',src:'effects.glb',
      instances:[{id:'debris',x:50},{id:'retired',x:-10000,scaleX:0,scaleY:0,scaleZ:0}]},2);
    const ordinary=normalizeSceneModel({id:'ordinary',src:'ordinary.glb',x:80},0);
    const state={models:[ordinary],instancedGLBMeshes:[actors,scenery,effects],objects:new Map(),
      _modelTextureVariantScope:{key:'scope-v1'}};
    const initial=sceneHydrationModels(state,null);
    const records={modelCount:initial.length,staticModels:new Map(),rigidInstances:new Map(),rigidInstancesByID:new Map()};
    state._hydratedModelRecords=records;
    for(const model of initial) {
      const object={id:model.id+'/primitive',vertices:{immutable:true,revision:0},parentMatrix:sceneModelTransformMatrix(model)};
      const staged={model,rigidInstanceModel:model,objects:[object],modelSkins:[],modelAnimations:[],points:[],labels:[],sprites:[],html:[],lights:[],_poseRevision:7};
      const key=sceneRigidInstanceHydrationKey(state,model);
      records.rigidInstances.set(key,staged);
      const membership=sceneRigidMembershipDescriptor(state,key,staged);
      if(membership)records.rigidInstancesByID.set(model.id,membership);
      state.objects.set(object.id,object);
    }
    const handle={};let renders=0;
    function poseBatch(batch=actors) {return {id:batch.id,instances:batch.instances.map(i=>({...i,x:i.x+10}))};}
    function submit(batches) {return window.__gosx_scene3d_command_bridge.applyMountedPoseFrame(
      state,batches,sceneUpdateRigidInstancePoses,()=>renders++,handle);}
  `);
  const expanded=[];
  const expand=c.sceneInstancedGLBMeshToModels;
  c.sceneInstancedGLBMeshToModels=(batch,index)=>{
    expanded.push({id:batch.id,instances:batch.instances.length});
    return expand(batch,index);
  };
  let fullExpansions=0;
  const hydrate=c.sceneHydrationModels;
  c.sceneHydrationModels=(...args)=>{fullExpansions++;return hydrate(...args);};
  return {c,run,expanded,fullExpansions:()=>fullExpansions};
}

test('packed actor poses leave a thousand props, effects and ordinary models unexpanded and unchanged', () => {
  const {run,expanded,fullExpansions}=packedFixture();
  const untouched=run("Array.from(records.rigidInstances.values()).filter(s=>!s.model.id.startsWith('actors/')).map(s=>({staged:s,model:s.model,matrix:s.objects[0].parentMatrix,revision:s._poseRevision}))");
  for(let frame=0;frame<30;frame++)assert.equal(run('submit([poseBatch()]).applied'),true);
  assert.equal(fullExpansions(),0);
  assert.equal(expanded.length,30);
  assert.ok(expanded.every(b=>b.id==='actors'&&b.instances===2));
  for(const prior of untouched) {
    assert.equal(prior.staged.model,prior.model);
    assert.equal(prior.staged.objects[0].parentMatrix,prior.matrix);
    assert.equal(prior.staged._poseRevision,prior.revision);
  }
  assert.equal(run("records.rigidInstancesByID.get('actors/a').staged.objects[0].parentMatrix[12]"),301);
  assert.equal(run('renders'),30);
});

test('a stale scope, template or retired target rejects packed poses with complete rollback', () => {
  for(const invalidation of [
    "records.rigidInstancesByID.get('actors/b').scope='old-scope'",
    "records.rigidInstancesByID.get('actors/b').template='old-template'",
    "records.rigidInstances.delete(records.rigidInstancesByID.get('actors/b').key)",
    "state.objects.delete('actors/b/primitive')",
  ]) {
    const {run}=packedFixture();
    const matrix=run("state.objects.get('actors/a/primitive').parentMatrix");
    run(invalidation);
    assert.throws(()=>run('submit([poseBatch()])'),/retained-pose-unavailable/);
    assert.equal(run('actors.instances[0].x'),1);
    assert.equal(run('actors.instances[1].x'),2);
    assert.equal(run("state.objects.get('actors/a/primitive').parentMatrix"),matrix);
    assert.equal(run("records.rigidInstancesByID.get('actors/a').staged._poseRevision"),7);
    assert.equal(run('renders'),0);
  }
});

test('wrong, reordered and dropped packed memberships cannot move a surviving actor', () => {
  for(const batches of [
    "[{id:'missing',instances:poseBatch().instances}]",
    "[{id:'actors',instances:poseBatch().instances.slice().reverse()}]",
    "[{id:'actors',instances:poseBatch().instances.slice(0,1)}]",
    "[{id:'actors',instances:[]}]",
  ]) {
    const {run}=packedFixture();
    assert.throws(()=>run('submit('+batches+')'),/membership/);
    assert.equal(run('actors.instances[0].x'),1);
    assert.equal(run("records.rigidInstancesByID.get('actors/a').staged._poseRevision"),7);
  }
  const {run,expanded}=packedFixture();
  run("state.instancedGLBMeshes.push(normalizeSceneInstancedGLBMeshEntry({id:'empty',src:'empty.glb',instances:[]},3))");
  assert.equal(run("submit([{id:'empty',instances:[]}]).applied"),true);
  assert.deepEqual(expanded,[{id:'empty',instances:0}]);
  assert.equal(run("records.rigidInstancesByID.get('actors/a').staged._poseRevision"),7);
});

test('duplicate binary batches cannot commit a retained actor twice', () => {
  const {run}=packedFixture();
  const matrix=run("state.objects.get('actors/a/primitive').parentMatrix");
  assert.equal(run('sceneUpdateRigidInstancePoses(state,null,[poseBatch(),poseBatch()])'),false);
  assert.equal(run("state.objects.get('actors/a/primitive').parentMatrix"),matrix);
  assert.equal(run("records.rigidInstancesByID.get('actors/a').staged._poseRevision"),7);
});

test('pending packed subsets advance compatible identities and defer uncommitted templates', () => {
  const {run,expanded,fullExpansions}=packedFixture();
  const oldB=run("records.rigidInstancesByID.get('actors/b').staged.objects[0].parentMatrix");
  run("state._modelHydrationUncommitted=true;actors.instances.push({id:'new',x:3});records.rigidInstancesByID.get('actors/b').template='old-template'");
  assert.equal(run("submit([{id:'actors',instances:[{...actors.instances[0],x:11},{...actors.instances[1],x:12},{...actors.instances[2],x:13}]}]).applied"),true);
  assert.equal(fullExpansions(),0);
  assert.deepEqual(expanded,[{id:'actors',instances:3}]);
  assert.equal(run("state.objects.get('actors/a/primitive').parentMatrix[12]"),11);
  assert.equal(run("state.objects.get('actors/b/primitive').parentMatrix"),oldB);
  assert.equal(run("state.objects.has('actors/new/primitive')"),false);
  expanded.length=0;
  assert.equal(run("submit([{id:'actors',instances:[{...actors.instances[0],x:21}]}]).applied"),true);
  assert.deepEqual(expanded,[{id:'actors',instances:1}]);
  assert.equal(run("state.objects.get('actors/a/primitive').parentMatrix[12]"),21);
  assert.equal(run("records.rigidInstancesByID.get('scenery/prop-0').staged._poseRevision"),7);
});

test('renderer ownership and full declaration pose validation remain authoritative', () => {
  const {run,fullExpansions}=packedFixture();
  run('state._modelOwner=()=>false');
  assert.throws(()=>run('submit([poseBatch()])'),/retained-pose-unavailable/);
  assert.equal(run('actors.instances[0].x'),1);
  run('state._modelOwner=()=>true;scenery.instances[0].x=42');
  assert.equal(run('sceneUpdateRigidInstancePoses(state)'),true);
  assert.equal(fullExpansions(),1);
  assert.equal(run("state.objects.get('scenery/prop-0/primitive').parentMatrix[12]"),42);
  assert.equal(run("records.rigidInstancesByID.get('effects/retired').staged._poseRevision"),8);
  run('records.modelCount++');
  assert.equal(run('sceneUpdateRigidInstancePoses(state)'),false);
});


test('a pose arriving before first hydration stays deferred without a rejection', () => {
  const c=vm.createContext({window:{},document:{},TextDecoder,Uint8Array,ArrayBuffer,DataView,Set,Map,Date,Promise,setTimeout});
  vm.runInContext(ts.transpileModule(fs.readFileSync(path.join(root,'client/runtime/scene3d/command-runtime.ts'),'utf8'), {compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText,c);
  const handle={};
  const result=c.window.__gosx_scene3d_command_bridge.applyMountedPoseFrame(
    {_modelHydrationPromise:Promise.resolve()},[],()=>{throw Error('updater called')},()=>{},handle);
  assert.equal(result.pending,true);
  assert.deepEqual(Object.keys(handle.__gosxPoseFrameStats.rejected),[]);
});


test('CPU stages enter the identity cache and membership retires only removed playback owners', async () => {
  const {c,run}=fixture();
  const stageStart=source.indexOf('  async function sceneStageModelHydration(');
  vm.runInContext(source.slice(stageStart,source.indexOf('  function sceneDestroyStagedModelHydrations(',stageStart)),c);
  c.sceneModelHydrationIsCurrent=()=>true;
  c.loadSceneModelAsset=async()=>({objects:[{skin:{},vertices:{count:3}}],skins:[{}],nodes:[{}],points:[],labels:[],sprites:[],html:[],lights:[]});
  c.sceneModelWithAssetFit=m=>m;
  c.sceneModelHasSkins=()=>true;
  c.sceneModelHasWeightAnimations=()=>false;
  c.sceneModelHasNodeAnimations=()=>false;
  c.sceneCloneModelSkins=s=>s;
  c.sceneInstantiateModelObject=(_source,_model,prefix)=>({id:prefix+'/primitive',vertices:{count:3,immutable:false},parentMatrix:null});
  c.scenePrepareModelSkinPlayback=async(s,_asset,m)=>s._modelSkins.push({model:m,explicitClips:[],mixer:null});
  run("model._crowdPose={animation:'Walk'}");
  const result=await run('sceneStageModelHydration(state,model,0,1)');
  assert.equal(result.ok,true);
  c.cpuStage=result.staged;
  assert.equal(run('sceneReusableRigidInstance(cpuStage,null)'),true);
  // Install this actual CPU stage and exercise the real membership transaction
  // repeatedly: a survivor's playback owner stays put, and a new identity
  // receives exactly one stage, which disappears again on retirement.
  run(`state.objects=new Map(cpuStage.objects.map(o=>[o.id,o]));
    state._modelSkins=cpuStage.modelSkins;
    state._hydratedModelRecords={modelCount:1,objects:cpuStage.objects.map(o=>o.id),staticModels:new Map(),rigidInstances:new Map([[key,cpuStage]]),rigidInstancesByID:new Map([[model.id,sceneRigidMembershipDescriptor(state,key,cpuStage)]])};`);
  const commitEnd=source.indexOf('  function sceneReconcileRigidInstanceMembership(');
  vm.runInContext(source.slice(source.indexOf('  function sceneRigidMembershipSnapshotModel('),commitEnd),c);
  Object.assign(c,{sceneNumber:(v,f)=>Number.isFinite(v)?v:f,sceneModelHydrationCounts:()=>({}),publishSceneModelHydrationStatus(){},gosxSceneEmit(){},sceneModelHydrationOutcome:()=>({committed:true}),sceneDestroyStagedModelHydrations(){},hydrateSceneStateModels(){throw Error('full hydration called')}});
  const survivor=run('cpuStage.modelSkins[0]');
  for(let cycle=0;cycle<20;cycle++) {
    run(`const addition${cycle}={...model,id:'debris/new-${cycle}'};
      sceneInstancedGLBHydrationTemplates.set(addition${cycle},'asset-v1');
      state.models=[model,addition${cycle}];`);
    const added=await run('sceneCommitRigidInstanceMembership(scenePlanRigidInstanceMembership(state))');
    assert.equal(added.committed,true);
    assert.equal(run('state._modelSkins.length'),2);
    assert.equal(run('state._modelSkins[0]'),survivor);
    run('state.models=[model]');
    await run('sceneCommitRigidInstanceMembership(scenePlanRigidInstanceMembership(state))');
    assert.equal(run('state._modelSkins.length'),1);
    assert.equal(run('state.objects.size'),1);
    assert.equal(run('state._modelSkins[0]'),survivor);
  }
});


test('incompatible lazy animation APIs leave skin playback disabled', async () => {
  const begin = source.indexOf('  async function scenePrepareModelSkinPlayback(');
  const end = source.indexOf('  function sceneHasActiveModelAnimations(', begin);
  assert.ok(begin >= 0 && end > begin);
  const prepare = ts.transpileModule(source.slice(begin, end), {compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText;
  const compatible = {
    buildNodeTransforms() {}, computeJointMatrices() {},
    initializeModelPlayback(record) { record.initialized = true; },
    isModelPlaying() { return false; }, applyModelAnimation() { return false; },
  };
  async function stage(api) {
    const state = {};
    const c = vm.createContext({console, Map,
      ensureAnimationFeatureLoaded: async () => api,
      sceneModelHasSkins: () => true,
      sceneModelRootNodes: () => [0],
      sceneModelTransformMatrix: () => new Float32Array(16),
      sceneCloneModelAnimations: () => [],
      sceneApplyModelSkinPose: record => { record.sampled = true; },
    });
    vm.runInContext(prepare, c);
    await c.scenePrepareModelSkinPlayback(state, {nodes:[{}]}, {}, [], [], [], []);
    return state;
  }
  for (const method of ['initializeModelPlayback', 'isModelPlaying', 'applyModelAnimation']) {
    for (const value of [undefined, true]) {
      const state = await stage({...compatible, [method]:value});
      assert.equal(state._modelSkins, undefined, method + ' must prevent registration');
      assert.equal(state._modelAnimations, undefined);
    }
  }
  const state = await stage(compatible);
  assert.equal(state._modelSkins.length, 1);
  assert.equal(state._modelSkins[0].initialized, true);
  assert.equal(state._modelSkins[0].sampled, true);
});
