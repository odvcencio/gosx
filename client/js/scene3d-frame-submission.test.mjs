import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";
import { createRequire } from "node:module";
const ts = createRequire(new URL("../runtime/package.json", import.meta.url))("typescript");
const source = name => fs.readFileSync(new URL("../runtime/scene3d/" + name, import.meta.url), "utf8");
function run(code, context) {
  return vm.runInContext(ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
}
function harness() {
  const events = [];
  const lifecycleEvents = [], attributes = {};
  const state = { objects: new Map([0, 1, 2].map(i => [i, { id: `hero/slot-0/prim-${i}`, visible: true, opacity: 1 }])),
    _modelHydrationGeneration: 4, _modelHydrationUncommitted: false };
  const renderer = { kind: "webgl" };
  const mount = { dispatchEvent(event) { events.push(event); } };
  let sets = 0;
  class CountingSet extends Set { constructor(...args) { super(...args); sets++; } }
  const context = vm.createContext({ Set: CountingSet, state, sceneState: state, renderer, mount, disposed: false,
    ctx:{emit(type,detail){lifecycleEvents.push({type,detail});}},viewport:{cssWidth:320,cssHeight:200},props:{},
    readyAttr:'ready',mountedAttr:'mounted',revealedAttr:'revealed',revealClass:'',
    setAttrValue(_mount,name,value){attributes[name]=value;},
    sceneStateObjects:s=>Array.from(s.objects.values()),sceneStateLabels:()=>[],sceneStateSprites:()=>[],
    sceneStateHTML:()=>[],sceneStateLights:()=>[],sceneModels:()=>[],
    CustomEvent: class { constructor(type, options) { this.type = type; this.detail = options.detail; } }, performance: { now: () => 1 } });
  const telemetry = source("mount-telemetry.ts");
  run(telemetry.slice(telemetry.indexOf("function sceneCreateFrameRequest")), context);
  const mounted = source("mount.ts");
  run(mounted.match(/const clockSubmission = [^\n]+/)[0], context);
  run('let readySent=false,mountedSent=false,revealSent=false;',context);
  run(mounted.slice(mounted.indexOf('    function publishReady('),mounted.indexOf('    function finishScenePerfMeasure(')),context);
  run(mounted.slice(mounted.indexOf("    let lastMountCommandRevision = 0;"), mounted.indexOf("    function createSceneRenderFrameMeta")), context);
  run(`lastMountCommandRevision=9;
    pendingFrameRequest=sceneCreateFrameRequest({id:'drop',modelIDs:['hero/slot-0']},6);
    pendingFrameRequest.accepted=true;
    var receipt=scenePrepareFrameRequest(pendingFrameRequest,state,9);
    var meta={frameRequest:receipt,frameRenderer:renderer,commandRevision:9,hydrationGeneration:4,clockRevision:0};`, context);
  return { context, events, lifecycleEvents, attributes, state, eval: code => run(code, context), sets: () => sets };
}

test("requested submission needs every live color primitive, not a declaration or shadow pass", () => {
  const h = harness();
  for (const outcome of ["pending", "empty", "failed", undefined]) {
    assert.equal(h.eval(`finishSceneSubmission(${JSON.stringify(outcome)},meta)`), false);
    assert.equal(h.events.length, 0);
  }
  h.eval("receipt.record('other/slot-0/prim-0');receipt.record('hero/slot-0/prim-0')");
  assert.equal(h.eval("finishSceneSubmission('submitted',meta)"), true);
  assert.equal(h.events.length, 0, "a partial actor must keep admission pending");
  h.eval("receipt.record('hero/slot-0/prim-1');receipt.record('hero/slot-0/prim-2');finishSceneSubmission('submitted',meta)");
  assert.equal(h.events.length, 1);
  assert.equal(h.events[0].type, "gosx:scene3d:frame-submitted");
  assert.equal(h.events[0].detail.revision, 6);
  assert.equal(h.events[0].detail.commandRevision, 9, "later camera/pose revisions do not starve the request");
  assert.equal(h.events[0].detail.objectIDs.length, 3);
  h.eval("finishSceneSubmission('submitted',meta)");
  assert.equal(h.events.length, 1, "one request emits one receipt");
});

test("hydration, command, renderer and cancellation fences reject stale submissions", () => {
  for (const mutation of ["state._modelHydrationGeneration=5", "state._modelHydrationUncommitted=true",
    "lastMountCommandRevision=10", "renderer={kind:'webgl'}", "disposed=true",
    "pendingFrameRequest=sceneCreateFrameRequest({id:'new',modelIDs:['hero/slot-0']},10)",
    "cancelSceneFrameRequest({target:mount,detail:{id:'drop'}})"]) {
    const h=harness(); h.eval("for (const id of receipt.expected) receipt.record(id)");
    h.eval(mutation); h.eval("finishSceneSubmission('submitted',meta)"); assert.equal(h.events.length,0,mutation);
  }
  const h=harness();
  h.eval("cancelSceneFrameRequest({target:mount,detail:{id:'old'}});cancelSceneFrameRequest({target:{},detail:{id:'drop'}})");
  assert.equal(h.eval("pendingFrameRequest.id"),'drop');
  h.state.objects.clear();
  assert.equal(h.eval("scenePrepareFrameRequest(receipt,state,9)"),null,"empty lobby membership cannot admit gameplay");
});

test("normal settled submission frames allocate no request sets and emit no receipt events", () => {
  const h=harness();h.eval("pendingFrameRequest=null;meta.frameRequest=null");const before=h.sets();
  h.eval("for(let i=0;i<10000;i++) finishSceneSubmission('submitted',meta)");
  assert.equal(h.sets(),before);assert.equal(h.events.length,0);
});

test("shader clock receipts follow accepted frames and retain newer pending requests", () => {
  const h=harness();
  h.eval("clockSubmission.requested=3;meta.clockRevision=2");
  for (const outcome of ["pending","empty","failed"]) {
    h.eval(`finishSceneSubmission('${outcome}',meta)`);
    assert.equal(h.eval("clockSubmission.submitted"),-1);
  }
  h.eval("finishSceneSubmission('submitted',meta)");
  assert.equal(h.eval("clockSubmission.submitted"),2);
  assert.equal(h.eval("clockSubmission.submitted !== clockSubmission.requested"),true);
  h.eval("meta.clockRevision=3;lastMountCommandRevision=10;finishSceneSubmission('submitted',meta)");
  assert.equal(h.eval("clockSubmission.submitted"),2,"a stale command frame cannot satisfy the newer clock");
  h.eval("lastMountCommandRevision=9;finishSceneSubmission('submitted',meta)");
  assert.equal(h.eval("clockSubmission.submitted"),3);
});

test("empty renderer mounts once without visual readiness or a requested submission", () => {
  const h=harness();h.state.objects.clear();
  assert.equal(h.eval("finishSceneSubmission('pending',meta)"),false);
  assert.equal(h.attributes.mounted,undefined);
  assert.equal(h.eval("finishSceneSubmission('empty',meta)"),false);
  assert.equal(h.attributes.mounted,'true');
  assert.equal(h.attributes.ready,undefined);
  assert.equal(h.attributes.revealed,undefined);
  assert.equal(h.events.length,0);
  assert.equal(h.eval('pendingFrameRequest.id'),'drop');
  h.eval("finishSceneSubmission('empty',meta)");
  assert.equal(h.lifecycleEvents.length,1);
  h.eval("publishReady()");
  assert.equal(h.attributes.ready,'true');
  assert.equal(h.lifecycleEvents.length,1,'later readiness never emits a second mount event');
});

test("stale, failed or disposed empty attempts cannot publish mounted lifecycle", () => {
  for(const mutation of ['disposed=true','renderer={kind:"webgl"}','lastMountCommandRevision++','state._modelHydrationGeneration++']) {
    const h=harness();h.eval(mutation);
    assert.equal(h.eval("finishSceneSubmission('empty',meta)"),false);
    assert.equal(h.attributes.mounted,undefined);
    assert.equal(h.lifecycleEvents.length,0);
  }
  for(const outcome of ['pending','failed',undefined]) {
    const h=harness();h.eval(`finishSceneSubmission(${JSON.stringify(outcome)},meta)`);
    assert.equal(h.attributes.mounted,undefined);
    assert.equal(h.attributes.ready,undefined);
  }
});

test("a first submitted ready frame publishes readiness before its mounted event", () => {
  const h=harness();
  h.context.ctx.emit=(type)=>{
    assert.equal(type,'mounted');assert.equal(h.attributes.ready,'true');
    assert.equal(h.attributes.mounted,'true');
  };
  h.eval('publishReady()');
});

test("receipt coverage excludes foreign IDs and clears the previous frame before new membership", () => {
  const h=harness();
  h.eval("receipt.record('hero/slot-0/prim-0');for(let i=0;i<10;i++)receipt.record('foreign/'+i)");
  assert.equal(h.eval("sceneFrameRequestSubmitted(receipt,state,9,'submitted')"),false);
  assert.equal(h.eval('receipt.drawn.size'),1);
  h.eval("for(const id of receipt.expected)receipt.record(id)");
  assert.equal(h.eval("sceneFrameRequestSubmitted(receipt,state,9,'submitted')"),true);
  h.state.objects.set(3,{id:'hero/slot-0/prim-3',visible:true,opacity:1});
  h.eval('scenePrepareFrameRequest(receipt,state,9)');
  assert.equal(h.eval('receipt.drawn.size'),0);
  h.eval("for(let i=0;i<3;i++)receipt.record('hero/slot-0/prim-'+i)");
  assert.equal(h.eval("sceneFrameRequestSubmitted(receipt,state,9,'submitted')"),false);
  h.eval("receipt.record('hero/slot-0/prim-3')");
  assert.equal(h.eval("sceneFrameRequestSubmitted(receipt,state,9,'submitted')"),true);
  h.state._modelHydrationGeneration++;
  assert.equal(h.eval("sceneFrameRequestSubmitted(receipt,state,9,'submitted')"),false);
});

function completedFrame(outcome,checkEmpty=false,covered=true) {
  const h=harness(),trace=[];
  const bundle={id:'current'},viewport={cssWidth:320,cssHeight:200};
  const step=name=>(...args)=>{trace.push(name);return args;};
  Object.assign(h.context,{bundle,viewport,renderTiming:{},adaptiveQuality:{},viewportDirty:false,
    ensureRendererCanCoverBundle(){trace.push('coverage');return covered;},
    syncSceneNodeSentinels:step('sentinels'),createSceneRenderFrameMeta(){trace.push('meta');return h.eval('meta');},
    sceneRenderWithTiming(_renderer,rendered){assert.equal(rendered,bundle);trace.push('render');return outcome;},
    publishMetrics(){},recordSceneWaterFrame:step('water'),maybeEmitRenderEmpty:step('empty'),
    renderSceneLayers:step('layers'),statsOverlay:{update:step('stats')},inspectorOverlay:{update:step('inspector')},
    sceneUpdateAdaptiveQuality(){trace.push('quality');return true;},scheduleRender:step('schedule'),
    publishReady:step('ready'),publishRevealed:step('revealed'),notifySceneProgressiveModelRenderCommitted:step('progressive'),
    scheduleNextAnimationFrame:step('next')});
  const mount=source('mount.ts');
  run(mount.slice(mount.indexOf('    function renderMountedFrame('),mount.indexOf('    function renderFrame(')),h.context);
  h.eval(`renderMountedFrame(bundle,10,8,${checkEmpty})`);
  return {h,trace};
}

test("shared completion preserves runtime/native ordering, overlays, quality and submission admission", () => {
  const common=['coverage','sentinels','meta','render','water'];
  const finish=['layers','stats','inspector','quality','schedule','ready','revealed','progressive','next'];
  for(const native of [false,true]) {
    const {h,trace}=completedFrame('submitted',native);
    assert.deepEqual(trace,[...common,...(native?['empty']:[]),...finish]);
    assert.equal(h.context.viewportDirty,true);
    assert.equal(h.events.length,0,'positive frame outcome still cannot acknowledge an undrawn requested actor');
  }
  for(const outcome of ['pending','empty','failed',undefined]) {
    const {h,trace}=completedFrame(outcome,true);
    assert.deepEqual(trace,[...common,'empty','layers','stats','inspector','quality','schedule','next']);
    assert.equal(h.events.length,0);
  }
  assert.deepEqual(completedFrame('submitted',true,false).trace,['coverage','next']);
});

test("WebGPU partial validation submission returns pending instead of a positive full-frame outcome", () => {
  const gpu=source("webgpu.ts");const events=[];
  const c=vm.createContext({guard:{},wgpuSubmitPendingFrame:()=>events.push('partial'),fail:()=>events.push('failure')});
  run(gpu.slice(gpu.indexOf("    function wrapFrame("),gpu.indexOf("    function release(")),c);
  assert.equal(run("wrapFrame(()=>{throw {pipelineValidation:true}},()=>{})(null,null,null)",c),'pending');
  assert.equal(run("wrapFrame(()=>{throw new Error('bad')},()=>{})(null,null,null)",c),'failed');
  assert.equal(run("wrapFrame(()=> 'submitted',()=>{})(null,null,null)",c),'submitted');
  assert.deepEqual(events,['partial','partial','failure']);
});
