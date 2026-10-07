import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import {fileURLToPath} from 'node:url';
import {freshFeatureBundleSource} from './runtime-test-harness.js';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
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
  assert.equal(run("sceneUpdateRigidInstancePoses(state,null,[{id:'debris',instances:[{id:'slot-0'},{id:'new'}]}])"),true);
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


test('a pose arriving before first hydration stays deferred without a rejection', () => {
  const c=vm.createContext({window:{},document:{},TextDecoder,Uint8Array,ArrayBuffer,DataView,Set,Map,Date,Promise,setTimeout});
  vm.runInContext(freshFeatureBundleSource('scene3d-command'),c);
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
