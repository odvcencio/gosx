import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import {createRequire} from 'node:module';
import {fileURLToPath} from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const ts = createRequire(import.meta.url)('../runtime/node_modules/typescript');
const {readSceneRendererBackendSrc} = createRequire(import.meta.url)('./scene3d-renderer-source-set.js');
const source = fs.readFileSync(path.join(root, 'client/runtime/scene3d/mount-webgl.ts'), 'utf8');
function functions(first, next) {
  return ts.transpileModule(source.slice(source.indexOf('  function '+first+'('), source.indexOf('  function '+next+'(')), {compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText;
}
function fixture() {
  const c = vm.createContext({console, Float32Array, Map, Set, WeakMap});
  vm.runInContext(fs.readFileSync(path.join(root,'client/js/bootstrap-src/11-scene-math.ts'),'utf8'),c);
  vm.runInContext('const sceneInstancedGLBHydrationTemplates = new WeakMap(); const sceneInstancedGLBTints = new WeakMap();',c);
  c.sceneModelTransformMatrix = m => new Float32Array([m.scaleX,0,0,0,0,m.scaleY,0,0,0,0,m.scaleZ,0,m.x,0,0,1]);
  c.sceneHydrationModels = s => s.models;
  c.sceneApplyModelSkinPose = r => {r.sampledPose=r.model._crowdPose;};
  { const core=fs.readFileSync(path.join(root,'client/js/bootstrap-src/10-runtime-scene-core.ts'),'utf8'); vm.runInContext(core.slice(core.indexOf('  function sceneCommitCrowdPoseUpdates('),core.indexOf('  // ---- Batch parent')),c); }
  vm.runInContext(ts.transpileModule(fs.readFileSync(path.join(root,'client/runtime/scene3d/model-pose-data.ts'),'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText,c);
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
  { const core=fs.readFileSync(path.join(root,'client/js/bootstrap-src/10-runtime-scene-core.ts'),'utf8'); vm.runInContext(core.slice(core.indexOf('  function sceneCommitCrowdPoseUpdates('),core.indexOf('  // ---- Batch parent')),c); }
  vm.runInContext(ts.transpileModule(fs.readFileSync(path.join(root,'client/runtime/scene3d/model-pose-data.ts'),'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText,c);
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
  assert.ok(expanded.every(b=>b.id==='actors'&&b.instances===1));
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

test('committed binary poses reuse only two private wrappers without growing residency', () => {
  const {run,expanded}=packedFixture();
  const wrappers=new Set(),residentCount=run('records.rigidInstances.size');
  const vertices=run("records.rigidInstancesByID.get('actors/a').staged.objects[0].vertices");
  for(let frame=0;frame<120;frame++) {
    assert.equal(run('submit([poseBatch()]).applied'),true);
    wrappers.add(run("records.rigidInstancesByID.get('actors/a').staged.model"));
    assert.equal(run("records.rigidInstancesByID.get('actors/a').staged.objects[0].vertices"),vertices);
    assert.equal(run('records.rigidInstances.size'),residentCount);
  }
  assert.equal(wrappers.size,2);
  assert.ok(expanded.every(batch=>batch.id==='actors'&&batch.instances===1));
});

function addGenericSkinPrimitives(run) {
  run(`for(const id of ['actors/a','actors/b']) {
    const staged=records.rigidInstancesByID.get(id).staged;
    const second={id:id+'/second',vertices:{immutable:false,revision:1},parentMatrix:null};
    staged.objects.push(second);state.objects.set(second.id,second);
    staged.objects[0].vertices.immutable=false;
    staged.modelSkins=[{model:staged.model,explicitClips:[],mixer:null,wasmMixer:null,rootTransform:null}];
    for(const object of staged.objects) object._crowdSkin={atlas:{},rows:new Float32Array(3),rev:0,
      poseRows(_atlas,pose,out){out[0]=pose.animation.length;out[1]=pose.animationTime;out[2]=pose.animationLoop?1:0;return out;}};
  }`);
}

test('each GSP2 clip time loop and CPU/crowd primitive transition stays current', () => {
  const {run}=packedFixture();addGenericSkinPrimitives(run);
  const recordsBefore=run("['actors/a','actors/b'].map(id=>records.rigidInstancesByID.get(id).staged.modelSkins[0])");
  for(let frame=0;frame<80;frame++) {
    const clip=['Idle','Walk','Cast','Die'][frame%4],time=frame/32,loop=frame%2===0;
    assert.equal(run(`submit([{id:'actors',instances:actors.instances.map(i=>({...i,x:i.x+1,animation:'${clip}',animationTime:${time},animationLoop:${loop}}))}]).applied`),true);
    const stages=run("['actors/a','actors/b'].map(id=>records.rigidInstancesByID.get(id).staged)");
    for(let index=0;index<stages.length;index++) {
      const stage=stages[index],playback=stage.modelSkins[0];
      assert.equal(playback,recordsBefore[index]);
      assert.equal(playback.model,stage.model);
      assert.equal(playback.sampledPose.animation,clip);
      assert.equal(playback.sampledPose.animationTime,time);
      assert.equal(playback.sampledPose.animationLoop,loop);
      assert.equal(playback.rootTransform[12],(index+1)+frame+1);
      for(const object of stage.objects) assert.deepEqual(Array.from(object._crowdSkin.rows),[clip.length,time,loop?1:0]);
    }
  }
});

test('warm-cache rejection preserves wrapper references and every primitive and playback row', () => {
  for(const invalidate of ['pose-rows','ownership','scope','template']) {
    const {run}=packedFixture();addGenericSkinPrimitives(run);
    assert.equal(run('submit([poseBatch()]).applied'),true);
    const stages=run("['actors/a','actors/b'].map(id=>records.rigidInstancesByID.get(id).staged)");
    const before=stages.map(stage=>({stage,model:stage.model,rigid:stage.rigidInstanceModel,revision:stage._poseRevision,
      playback:stage.modelSkins[0],playbackModel:stage.modelSkins[0].model,root:stage.modelSkins[0].rootTransform,
      pose:{...stage.model._crowdPose},objects:stage.objects.map(object=>({object,parent:object.parentMatrix,rows:object._crowdSkin.rows,values:Array.from(object._crowdSkin.rows),rev:object._crowdSkin.rev}))}));
    if(invalidate==='pose-rows')run("records.rigidInstancesByID.get('actors/b').staged.objects[1]._crowdSkin.poseRows=()=>null");
    if(invalidate==='ownership')run("state.objects.delete('actors/b/second')");
    if(invalidate==='scope')run("state._modelTextureVariantScope.key='scope-v2'");
    if(invalidate==='template')run("records.rigidInstancesByID.get('actors/b').template='changed'");
    assert.throws(()=>run("submit([{id:'actors',instances:actors.instances.map(i=>({...i,x:i.x+10,animation:'Cast',animationTime:2,animationLoop:false}))}])"),/retained-pose-unavailable/);
    assert.equal(run('actors.instances[0].x'),11);assert.equal(run('actors.instances[1].x'),12);
    for(const prior of before) {
      assert.equal(prior.stage.model,prior.model);assert.equal(prior.stage.rigidInstanceModel,prior.rigid);
      assert.equal(prior.stage._poseRevision,prior.revision);assert.deepEqual({...prior.stage.model._crowdPose},prior.pose);
      assert.equal(prior.stage.modelSkins[0],prior.playback);assert.equal(prior.playback.model,prior.playbackModel);assert.equal(prior.playback.rootTransform,prior.root);
      for(const p of prior.objects){assert.equal(p.object.parentMatrix,p.parent);assert.equal(p.object._crowdSkin.rows,p.rows);assert.deepEqual(Array.from(p.rows),p.values);assert.equal(p.object._crowdSkin.rev,p.rev);}
    }
  }
});

test('warm cached metadata never admits a new shared source or material', () => {
  for(const change of ["actors.src='replacement.glb'","actors.color='#abcdef'","actors.texture='replacement.png'","actors.roughness=.17","actors.customUniforms={gain:2}","actors.alphaCutoff=.4"]) {
    const {run}=packedFixture();assert.equal(run('submit([poseBatch()]).applied'),true);
    const model=run("records.rigidInstancesByID.get('actors/a').staged.model");
    run(change);assert.throws(()=>run('submit([poseBatch()])'),/retained-pose-unavailable/);
    assert.equal(run("records.rigidInstancesByID.get('actors/a').staged.model"),model);
    assert.equal(run('actors.instances[0].x'),11);
  }
});

test('warm binary cache yields to asynchronous membership and retains previous wrappers', () => {
  const {run,expanded}=packedFixture();assert.equal(run('submit([poseBatch()]).applied'),true);
  const old=run("records.rigidInstancesByID.get('actors/b').staged.model");
  run("state._modelHydrationUncommitted=true;actors.instances.push({id:'new',x:3});records.rigidInstancesByID.get('actors/b').template='pending-v2'");
  expanded.length=0;
  assert.equal(run('submit([poseBatch()]).applied'),true);
  assert.equal(run("records.rigidInstancesByID.get('actors/b').staged.model"),old);
  assert.equal(run("state.objects.has('actors/new/primitive')"),false);
  assert.deepEqual(expanded,[{id:'actors',instances:3}]);
});

test('live affine group parents keep the broader hierarchy path and fresh world transforms', () => {
  const {c,run,expanded}=packedFixture();
  run('window.__gosx_scene3d_api={sceneNormalizeParentMatrix,sceneNormalizeDirection}');
  vm.runInContext(ts.transpileModule(fs.readFileSync(path.join(root,'client/runtime/scene3d/hierarchy.ts'),'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText,c);
  run(`actors.parentNode='carrier';
    state.groups=sceneNormalizeGroups([{id:'carrier',x:3,y:4,scaleX:1.5,scaleY:.75,spinZ:Math.PI/2}]);
    for(const id of ['actors/a','actors/b']) for(const object of records.rigidInstancesByID.get(id).staged.objects) object.parentNode='carrier';`);
  for(let step=0;step<12;step++) {
    assert.equal(run('submit([poseBatch()]).applied'),true);
    const values=run(`(()=>{const matrix=sceneGroupMatrices(state,${step}/4).get('carrier');
      const object=state.objects.get('actors/a/primitive');
      const view=sceneGroupRecords([object],sceneGroupMatrices(state,${step}/4),'matrix')[0];
      return {local:Array.from(object.parentMatrix),world:Array.from(view.parentMatrix),parent:Array.from(matrix)};})()`);
    for(let column=0;column<4;column++)for(let row=0;row<4;row++) {
      let expected=0;for(let k=0;k<4;k++)expected+=values.parent[k*4+row]*values.local[column*4+k];
      assert.ok(Math.abs(values.world[column*4+row]-expected)<1e-5);
    }
  }
  assert.ok(expanded.every(batch=>batch.id==='actors'&&batch.instances===2),'parented wrappers must not use the unparented cache');
});

test('moving model ParentNode affine poses remain live after local binary poses', () => {
  const {c,run,expanded}=packedFixture();
  const core=fs.readFileSync(path.join(root,'client/js/bootstrap-src/10-runtime-scene-core.ts'),'utf8');
  vm.runInContext(core.slice(core.indexOf('  function sceneCrowdBatchParentObject('),core.indexOf('  // Copies the agents\' union')),c);
  run(`actors.parentNode='platform';const parent=normalizeSceneObject({id:'platform',kind:'box'},0);
    const parentGroup={parentMatrix:null};const parentSource=[];`);
  for(let step=0;step<12;step++) {
    const angle=step*.17,c=Math.cos(angle),s=Math.sin(angle);
    const matrix=[c*1.4,s*1.4,0,0,-s*.7,c*.7,0,0,.2,.3,1.1,0,step,-step*.5,3,1];
    run('parent.parentMatrix=new Float32Array('+JSON.stringify(matrix)+');');
    assert.equal(run('submit([poseBatch()]).applied'),true);
    const actual=run(`(()=>{const node=sceneCrowdBatchParentObject([parent,...state.objects.values()],'platform','actors');
      sceneCrowdBatchSetParent(parentGroup,sceneObjectModelMatrix(node,0));return Array.from(parentGroup.parentMatrix);})()`);
    for(let i=0;i<16;i++)assert.ok(Math.abs(actual[i]-matrix[i])<1e-6);
    assert.equal(run("state.objects.get('actors/a/primitive').parentMatrix[12]"),1+10*(step+1));
  }
  assert.ok(expanded.every(batch=>batch.id==='actors'&&batch.instances===2));
});

function checkpointFunction(text,name) {
  const ast=ts.createSourceFile('renderer.ts',text,ts.ScriptTarget.ES2022,true,ts.ScriptKind.TS);let found;
  function visit(node){if(ts.isFunctionDeclaration(node)&&node.name?.text===name)found=node.getText(ast);if(!found)ts.forEachChild(node,visit);}visit(ast);
  assert.ok(found,'missing renderer function '+name);return found;
}
for(const backend of ['WebGL','WebGPU']) for(const representation of ['typed','numeric-array']) {
 test(`${backend} renderer caches upload six A/B/A pose frames (${representation})`,()=>{
  const {run}=packedFixture(),storage=new Map(),context={Float32Array,Uint32Array,Uint8Array,ArrayBuffer,Map,Set,WeakMap,Math,Number};
  const cache=vm.createContext(context),owner={},sources=new WeakMap();let buffer,bound,sequence=0,writes=0;
  const compile=text=>vm.runInContext(ts.transpileModule(text,{compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText,cache);
  compile(fs.readFileSync(path.join(root,'client/runtime/scene3d/instanced-data.ts'),'utf8'));
  if(backend==='WebGL'){
   Object.assign(context,{instancedStreamRecords:new Map(),pointsEntryBuffers:new Set(),instancedStreamEpoch:0,gl:{ARRAY_BUFFER:1,DYNAMIC_DRAW:2,
    createBuffer:()=>({id:++sequence}),bindBuffer(_target,value){bound=value;},
    bufferData(_target,data){storage.set(bound,typeof data==='number'?new Uint8Array(data):new Uint8Array(data.buffer,data.byteOffset,data.byteLength).slice());writes++;},
    bufferSubData(_target,offset,data){storage.get(bound).set(new Uint8Array(data.buffer,data.byteOffset,data.byteLength),offset);writes++;},deleteBuffer(value){storage.delete(value);}}});
   const renderer=readSceneRendererBackendSrc('webgl');
   compile(renderer.slice(renderer.indexOf('function uploadInstancedStream('),renderer.indexOf('function bindInstancedVertexAttribute(')));
  }else{
   const renderer=readSceneRendererBackendSrc('webgpu');
   Object.assign(context,{pointsEntryGPUBuffers:new Set(),pbrSceneAttributeCache:{},instancedCacheOwnerEpoch:0,GPUBufferUsage:{VERTEX:1,COPY_DST:2},device:{
    createBuffer({size}){const value={size,getMappedRange(){return storage.get(value).buffer;},unmap(){},destroy(){storage.delete(value);}};storage.set(value,new Uint8Array(size));return value;},
    queue:{writeBuffer(value,offset,data){storage.get(value).set(new Uint8Array(data.buffer,data.byteOffset,data.byteLength),offset);writes++;}}}});
   const planner=fs.readFileSync(path.join(root,'client/js/bootstrap-src/15b-scene-planner.ts'),'utf8');
   compile(checkpointFunction(planner,'sceneCachedSlotBuffer')+checkpointFunction(planner,'sceneCachedBuffer'));
   for(const name of ['wgpuArrayContentsMatch','wgpuAlignUp','wgpuCreateBuffer','wgpuCreateTrackedBuffer','wgpuTrackedBufferSize','wgpuUploadTrackedBuffer','wgpuCachedTrackedBuffer','wgpuStablePBRAttributeBuffer'])compile(checkpointFunction(renderer,name));
  }
  const models=new Set(),matrices=new Set();
  for(let frame=0;frame<6;frame++){
   const x=(frame+1)/8;assert.equal(run(`submit([{id:'actors',instances:actors.instances.map(i=>({...i,x:${x}}))}]).applied`),true);
   const model=run("records.rigidInstancesByID.get('actors/a').staged.model"),matrix=run("state.objects.get('actors/a/primitive').parentMatrix");models.add(model);matrices.add(matrix);
   let source=matrix;if(representation==='numeric-array'){source=sources.get(matrix);if(!source){source=Array.from(matrix);sources.set(matrix,source);}for(let i=0;i<16;i++)source[i]=matrix[i];}
   const prepared=context.scenePrepareInstancedTransforms(owner,source);let current;
   if(backend==='WebGL'){context.instancedStreamEpoch++;current=context.uploadInstancedStream({id:'generic'},0,'transforms',prepared,16);}
   else{context.instancedCacheOwnerEpoch++;current=context.wgpuStablePBRAttributeBuffer('pose-snapshot',prepared,owner,'pose-buffer');}
   if(buffer)assert.equal(current,buffer,'warm GPU buffer changed');buffer=current;
   assert.deepEqual(Array.from(new Float32Array(storage.get(buffer).buffer)),Array.from(matrix),'GPU payload went stale on frame '+frame);
  }
  assert.equal(models.size,2);assert.equal(matrices.size,2);assert.equal(writes,6,'every changed frame must upload');
 });
}

test('legitimate rehydration recovers with the same mounted array batch records and membership maps',()=>{
 for(const mutateDescriptor of [false,true]){
  const {run}=packedFixture();assert.equal(run('submit([poseBatch()]).applied'),true);
  const old=run("records.rigidInstancesByID.get('actors/a').staged"),oldTemplate=run("records.rigidInstancesByID.get('actors/a').template");
  const containers=run('[state.instancedGLBMeshes,actors,records,records.rigidInstances,records.rigidInstancesByID]');
  run(`{
   const replacement=normalizeSceneInstancedGLBMeshEntry({...actors,src:'replacement.glb',color:'#abc123'},0);Object.assign(actors,replacement);
   for(const model of sceneInstancedGLBMeshToModels(actors,0)){
    const previous=records.rigidInstancesByID.get(model.id);records.rigidInstances.delete(previous.key);
    for(const object of previous.staged.objects)state.objects.delete(object.id);
    const object={id:model.id+'/primitive',vertices:{immutable:true,revision:0},parentMatrix:sceneModelTransformMatrix(model)};
    const staged={model,rigidInstanceModel:model,objects:[object],modelSkins:[],modelAnimations:[],points:[],labels:[],sprites:[],html:[],lights:[],_poseRevision:0};
    const key=sceneRigidInstanceHydrationKey(state,model),membership=sceneRigidMembershipDescriptor(state,key,staged);
    records.rigidInstances.set(key,staged);state.objects.set(object.id,object);
    if(${mutateDescriptor})Object.assign(previous,membership);else records.rigidInstancesByID.set(model.id,membership);
   }
  }`);
  const fresh=run("records.rigidInstancesByID.get('actors/a').staged");assert.notEqual(fresh,old);
  assert.notEqual(run("records.rigidInstancesByID.get('actors/a').template"),oldTemplate);
  for(let frame=0;frame<4;frame++)assert.equal(run('submit([poseBatch()]).applied'),true);
  assert.equal(run("records.rigidInstancesByID.get('actors/a').staged"),fresh);
  assert.equal(run("records.rigidInstancesByID.get('actors/a').staged.model.src"),'replacement.glb');
  assert.equal(run("records.rigidInstancesByID.get('actors/a').staged.objects[0].parentMatrix[12]"),51);
  const after=run('[state.instancedGLBMeshes,actors,records,records.rigidInstances,records.rigidInstancesByID]');for(let i=0;i<containers.length;i++)assert.equal(after[i],containers[i]);
 }
});

test('same-container membership shrink and growth evict stale wrapper slots and recover', () => {
 const {run}=packedFixture();assert.equal(run('submit([poseBatch()]).applied'),true);
 const containers=run('[actors,actors.instances,records,records.rigidInstances,records.rigidInstancesByID]');
 run(`const removed=records.rigidInstancesByID.get('actors/b');
   records.rigidInstances.delete(removed.key);records.rigidInstancesByID.delete('actors/b');
   for(const object of removed.staged.objects)state.objects.delete(object.id);
   records.modelCount--;actors.instances.splice(1,1);`);
 assert.equal(run('submit([poseBatch()]).applied'),true);
 assert.equal(run('state._retainedBinaryPoseCache.batches.has(actors)'),false);
 assert.equal(run('submit([poseBatch()]).applied'),true);
 assert.equal(run('state._retainedBinaryPoseCache.batches.get(actors).length'),1);
 run(`{
   const replacement=normalizeSceneInstancedGLBMeshEntry({...actors,instances:[...actors.instances,{id:'new',x:3,animation:'Walk'}]},0);
   actors.instances.splice(0,actors.instances.length,...replacement.instances);
   const model=sceneInstancedGLBMeshToModels(actors,0).find(model=>model.id==='actors/new');
   const object={id:model.id+'/primitive',vertices:{immutable:true,revision:0},parentMatrix:sceneModelTransformMatrix(model)};
   const staged={model,rigidInstanceModel:model,objects:[object],modelSkins:[],modelAnimations:[],points:[],labels:[],sprites:[],html:[],lights:[]};
   const key=sceneRigidInstanceHydrationKey(state,model);records.rigidInstances.set(key,staged);
   records.rigidInstancesByID.set(model.id,sceneRigidMembershipDescriptor(state,key,staged));state.objects.set(object.id,object);records.modelCount++;
 }`);
 assert.equal(run('submit([poseBatch()]).applied'),true);
 assert.equal(run('state._retainedBinaryPoseCache.batches.has(actors)'),false);
 assert.equal(run('submit([poseBatch()]).applied'),true);
 assert.equal(run('state._retainedBinaryPoseCache.batches.get(actors).length'),2);
 assert.equal(run("records.rigidInstancesByID.get('actors/new').staged.objects[0].parentMatrix[12]"),23);
 const after=run('[actors,actors.instances,records,records.rigidInstances,records.rigidInstancesByID]');after.forEach((value,i)=>assert.equal(value,containers[i]));
});

test('an empty retained batch releases its cached wrappers', () => {
 const {run}=packedFixture();assert.equal(run('submit([poseBatch()]).applied'),true);
 run(`for(const instance of actors.instances){const membership=records.rigidInstancesByID.get('actors/'+instance.id);
   records.rigidInstances.delete(membership.key);records.rigidInstancesByID.delete('actors/'+instance.id);
   for(const object of membership.staged.objects)state.objects.delete(object.id);records.modelCount--;}
   actors.instances.length=0;`);
 assert.equal(run('submit([poseBatch()]).applied'),true);
 assert.equal(run('state._retainedBinaryPoseCache.batches.has(actors)'),false);
});

test('removing a palette clears committed tint before entering the local wrapper cache', () => {
 for (const warm of [false,true]) {
 const {run}=packedFixture();addGenericSkinPrimitives(run);
 if (warm) assert.equal(run('submit([poseBatch()]).applied'),true);
 run("actors.palette=['#ff0000'];for(const instance of actors.instances)instance.tint=1;");
 assert.equal(run('submit([poseBatch()]).applied'),true);
 assert.equal(run('state._retainedBinaryPoseCache.batches.has(actors)'),false);
 assert.equal(run("records.rigidInstancesByID.get('actors/a').staged.objects[0]._crowdSkin.tint[0]"),1);
 run('actors.palette=[];for(const instance of actors.instances)instance.tint=0;');
 assert.equal(run('submit([poseBatch()]).applied'),true);
 assert.equal(run("records.rigidInstancesByID.get('actors/a').staged.objects[0]._crowdSkin.tint"),null);
 assert.equal(run('submit([poseBatch()]).applied'),true);
 assert.equal(run('state._retainedBinaryPoseCache.batches.get(actors).length'),2);
 }
});
