"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const {
  bootstrapRuntimeSource,
  bootstrapFeatureEnginesSource,
  bootstrapFeatureScene3DSource,
  buildMinimalGLBBytes,
  FakeElement,
  createContext,
  installManualRAF,
  runScript,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

const gltfSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-scene3d-gltf.js"), "utf8");

async function mountPendingModel(renderBeforeModels, options = {}) {
  const mount = new FakeElement("div", null);
  mount.id = "scene-first-frame";
  let resolveModel;
  let rejectModel;
  const pendingModel = new Promise((resolve, reject) => {
    resolveModel = resolve;
    rejectModel = reject;
  });
  const props = {
    width: 320,
    height: 180,
    preferWebGL: true,
    adaptiveQuality: false,
    camera: { z: 6 },
    scene: {
      environment: { sky: { mode: "gradient", topColor: "#102030", horizonColor: "#8090a0" } },
      waterSystems: [{ id: "ocean", kind: "water", mode: "ocean" }],
      objects: [{ id: "guide", kind: "mesh", geometry: "box", width: 1, height: 1, depth: 1 }],
      lights: [{ id: "sun", kind: "directional", intensity: 1 }],
      models: [{ id: "asset", src: "/models/first-frame.glb" }],
    },
  };
  if (options.empty) props.scene = { models: [{ id: 'asset', src: '/_test-fixture/skinned.glb' }] };
  const modelSrc = props.scene.models[0].src;
  if (renderBeforeModels !== undefined) props.renderBeforeModels = renderBeforeModels;
  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    prefersReducedMotion: true,
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
      "/gosx/bootstrap-feature-scene3d-gltf.js": { text: gltfSource },
      [modelSrc]: () => pendingModel,
    },
    manifest: { engines: [{
      id: "first-frame-engine", component: "GoSXScene3D", kind: "surface", mountId: mount.id, props,
    }] },
  });
  const renders = [];
  let rendererDisposals = 0;
  env.context.__gosx_scene3d_perf = true;
  env.context.__gosx_scene3d_webgl_api = {
    createSceneWaterRendererWebGL: () => ({ kind: "webgl", render() {}, dispose() { rendererDisposals++; } }),
    createScenePBRRendererOrFallback: () => ({
      kind: "webgl",
      render(bundle) { renders.push(bundle); return options.outcome; },
      renderSurfaces() {},
      dispose() { rendererDisposals++; },
    }),
  };
  const raf = installManualRAF(env.context);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  for (let attempt = 0; attempt < 10; attempt++) {
    await flushAsyncWork();
    if (env.fetchCalls.some(call => call.url === modelSrc)) break;
  }
  assert.ok(env.fetchCalls.some(call => call.url === modelSrc), "model loader must be waiting on I/O");
  assert.equal(env.consoleLogs.error.length, 0);
  return {
    env, mount, raf, renders,
    resolveModel: () => resolveModel({ bytes: buildMinimalGLBBytes() }),
    rejectModel: () => rejectModel(new Error(options.empty ? 'HTTP 404: model not found' : 'model download failed')),
    rendererDisposals: () => rendererDisposals,
  };
}

async function drawInitialFrame(raf) {
  for (const now of [16, 32, 48]) {
    raf.flush(now);
    await flushAsyncWork();
  }
}

function modelObjects(bundle) {
  return bundle.meshObjects.filter(object => String(object.id).startsWith("asset/"));
}

test("renderBeforeModels draws the atmosphere first and renders hydrated model objects once ready", async () => {
  const { env, mount, raf, renders, resolveModel } = await mountPendingModel(true);
  await drawInitialFrame(raf);
  assert.equal(renders.length, 1);
  assert.equal(mount.getAttribute("data-gosx-scene3d-first-frame"), "before-models");
  assert.equal(renders[0].environment.sky.mode, "gradient");
  assert.equal(renders[0].waterSystems[0].id, "ocean");
  assert.equal(renders[0].lights[0].id, "sun");
  assert.ok(mount.__gosxScene3DSentinels.has("guide"));
  assert.equal(modelObjects(renders[0]).length, 0);

  resolveModel();
  await flushAsyncWork();
  raf.flush(64);
  await flushAsyncWork();
  assert.equal(renders.length, 2);
  assert.ok(modelObjects(renders[1]).length > 0);
  assert.equal(mount.__gosxScene3DScheduleCounts["schedule:models"], 1);
  assert.equal(mount.__gosxScene3DScheduleCounts["render:models"], 1);
  assert.equal(env.consoleLogs.error.length, 0);
});

for (const drawBeforeDispose of [false, true]) {
  test(`disposal while model hydration is pending cancels rendering (${drawBeforeDispose ? "after" : "before"} first frame)`, async () => {
    const { env, mount, raf, renders, resolveModel, rendererDisposals } = await mountPendingModel(true);
    if (drawBeforeDispose) await drawInitialFrame(raf);
    const rendered = renders.length;
    assert.doesNotThrow(() => mount.__gosxScene3DHandle.dispose());
    const disposed = rendererDisposals();
    assert.ok(disposed > 0);
    resolveModel();
    await flushAsyncWork();
    await drawInitialFrame(raf);
    assert.equal(renders.length, rendered);
    assert.equal(rendererDisposals(), disposed, "late hydration must not dispose twice");
    assert.equal(mount.__gosxScene3DHandle, undefined);
    assert.equal(env.consoleLogs.error.length, 0);
  });
}

for (const prop of [undefined, false]) {
  test(`Scene3D waits for models when renderBeforeModels is ${String(prop)}`, async () => {
    const { env, mount, raf, renders, resolveModel } = await mountPendingModel(prop);
    await drawInitialFrame(raf);
    assert.equal(renders.length, 0);
    resolveModel();
    await flushAsyncWork();
    await drawInitialFrame(raf);
    assert.equal(renders.length, 1);
    assert.ok(modelObjects(renders[0]).length > 0);
    assert.equal(mount.getAttribute("data-gosx-scene3d-first-frame"), "after-models");
    assert.equal(mount.__gosxScene3DScheduleCounts["schedule:models"], undefined);
    assert.equal(env.consoleLogs.error.length, 0);
  });
}

test("failed model hydration schedules a models render and keeps the first frame visible", async () => {
  const { env, mount, raf, renders, rejectModel } = await mountPendingModel(true);
  await drawInitialFrame(raf);
  rejectModel();
  await flushAsyncWork();
  raf.flush(64);
  await flushAsyncWork();
  assert.equal(renders.length, 2);
  assert.equal(modelObjects(renders[1]).length, 0);
  assert.equal(mount.__gosxScene3DScheduleCounts["render:models"], 1);
  assert.equal(mount.getAttribute("data-gosx-scene3d-first-frame"), "before-models");
  assert.equal(env.consoleLogs.error.length, 0);
});

test('an intentional missing model degrades to a mounted WebGL renderer without admitting empty visuals', async () => {
  const {env,mount,raf,renders,rejectModel}=await mountPendingModel(false,{empty:true,outcome:'empty'});
  await drawInitialFrame(raf);
  assert.equal(renders.length,0,'the model transaction has not completed');
  assert.notEqual(mount.getAttribute('data-gosx-scene3d-mounted'),'true');
  rejectModel();
  await flushAsyncWork();
  await drawInitialFrame(raf);
  assert.ok(renders.length>0,'an initialized renderer attempted the empty frame');
  assert.equal(mount.getAttribute('data-gosx-scene3d-renderer'),'webgl');
  assert.equal(mount.getAttribute('data-gosx-scene3d-model-status'),'error');
  assert.equal(mount.getAttribute('data-gosx-scene3d-mounted'),'true');
  assert.notEqual(mount.getAttribute('data-gosx-scene3d-ready'),'true');
  assert.notEqual(mount.getAttribute('data-gosx-scene3d-revealed'),'true');
  const receipts=[];
  mount.addEventListener('gosx:scene3d:frame-submitted',event=>receipts.push(event.detail));
  mount.dispatchEvent(new env.context.CustomEvent('gosx:scene3d:commands',{detail:{revision:1,commands:[],frameRequest:{id:'missing',modelIDs:['asset']}}}));
  await flushAsyncWork();
  await drawInitialFrame(raf);
  assert.equal(receipts.length,0,'missing hydrated primitives cannot acknowledge the model request');
  assert.equal(env.consoleLogs.error.length,0,'the asset failure is handled by the model lifecycle');
});

test('a pending renderer attempt with no content cannot publish mounted lifecycle', async () => {
  const {mount,raf,renders,rejectModel}=await mountPendingModel(false,{empty:true,outcome:'pending'});
  rejectModel();
  await flushAsyncWork();
  await drawInitialFrame(raf);
  assert.ok(renders.length>0);
  assert.equal(mount.getAttribute('data-gosx-scene3d-model-status'),'error');
  assert.notEqual(mount.getAttribute('data-gosx-scene3d-mounted'),'true');
  assert.notEqual(mount.getAttribute('data-gosx-scene3d-ready'),'true');
});

test("a superseded mount refuses late model hydration and does not render", async () => {
  const { env, mount, raf, renders, resolveModel } = await mountPendingModel(true);
  await drawInitialFrame(raf);
  const nextOwner = {};
  mount.__gosxScene3DOwner = nextOwner;
  resolveModel();
  await flushAsyncWork();
  raf.flush(64);
  await flushAsyncWork();
  assert.equal(renders.length, 1);
  assert.equal(mount.__gosxScene3DOwner, nextOwner);
  assert.equal(env.consoleLogs.error.length, 0);
});
