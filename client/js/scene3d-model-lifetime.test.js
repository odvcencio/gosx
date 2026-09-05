"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { createContext, runScript, bootstrapRuntimeSource, freshFeatureBundleSource, flushAsyncWork, FakeElement } = require("./runtime-test-harness.js");

function harness(fetch) {
  const env = createContext({});
  env.context.AbortController = AbortController;
  if (fetch) env.context.fetch = fetch;
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  const marker = "const sceneModelAssetReady = new Set();";
  const source = freshFeatureBundleSource("scene3d");
  assert.ok(source.includes(marker));
  runScript(source.replace(marker, marker + `
    window.__modelLifetimeTest = {
      load: loadSceneModelAsset, invalidate: invalidateSceneModelHydration,
      scope: createSceneModelTextureVariantScope,
      retire: retireSceneModelTextureVariantScope,
      cancelProgressive: cancelSceneProgressiveModelLifecycle,
      progressive: scheduleSceneProgressiveModelLifecycle,
      hydrate: hydrateSceneStateModels,
      size: () => sceneModelAssetCache.size,
      ready: () => sceneModelAssetReady.size
    };`), env.context, "fresh-scene3d.js");
  return { env, api: env.context.__modelLifetimeTest };
}

test("retired Scene3D model generations retain only a bounded idle parsed cache", async () => {
  const { env, api } = harness();
  env.context.__gosx_scene3d_gltf_api = {
    async sceneLoadGLTFModel() { return { objects: [{ id: "box", kind: "box" }] }; },
    gltfSceneToModelAsset(value) { return value; },
  };
  for (let i = 0; i < 100; i++) {
    const state = { _modelHydrationGeneration: 1, _modelTextureVariantScope: api.scope({ backend: "webgl" }) };
    const asset = await api.load("/repeat.glb", null, { state, generation: 1 });
    assert.equal(asset.objects.length, 1);
    api.invalidate(state);
  }
  assert.ok(api.size() <= 16, `retained ${api.size()} parsed entries`);
  assert.ok(api.ready() <= 16, `retained ${api.ready()} readiness entries`);
});

test("shared model acquisition survives one owner and abort cannot erase a fresh retry", async () => {
  const requests = [];
  const { api } = harness((url, options) => new Promise(resolve => requests.push({ url, options, resolve })));
  const a = { _modelHydrationGeneration: 1 }, b = { _modelHydrationGeneration: 1 };
  const pa = api.load("/shared.json", null, { state: a, generation: 1 });
  const pb = api.load("/shared.json", null, { state: b, generation: 1 });
  assert.equal(requests.length, 1);
  api.invalidate(a);
  assert.equal(requests[0].options.signal.aborted, false);
  api.invalidate(b);
  assert.equal(requests[0].options.signal.aborted, true);
  await Promise.all([pa, pb]);
  const c = { _modelHydrationGeneration: 1 };
  const pc = api.load("/shared.json", null, { state: c, generation: 1 });
  assert.equal(requests.length, 2);
  requests[0].resolve({ ok: true, json: async () => { throw new Error("abandoned body must not be parsed"); } });
  await flushAsyncWork();
  assert.equal(api.size(), 1, "late old result must not erase replacement");
  requests[1].resolve({ ok: true, json: async () => ({ objects: [{ id: "fresh", kind: "box" }] }) });
  const asset = await pc;
  assert.equal(asset.objects[0].id, "fresh");
  api.invalidate(c);
  assert.equal(api.size(), 1);
});

test("a surviving shared owner consumes output after its peer is superseded", async () => {
  let deliver, signal;
  const { api } = harness((_url, options) => { signal = options.signal; return new Promise(resolve => { deliver = resolve; }); });
  const a = { _modelHydrationGeneration: 1 }, b = { _modelHydrationGeneration: 1 };
  const pa = api.load("/survivor.json", null, { state: a, generation: 1 });
  const pb = api.load("/survivor.json", null, { state: b, generation: 1 });
  api.invalidate(a);
  deliver({ ok: true, json: async () => ({ objects: [{ id: "survives", kind: "box" }] }) });
  const [, asset] = await Promise.all([pa, pb]);
  assert.equal(signal.aborted, false);
  assert.equal(asset.objects[0].id, "survives");
  api.invalidate(b);
});

test("preload is bounded, reusable and cannot be cancelled by a mounted peer", async () => {
  let deliver, signal, requests = 0;
  const { env, api } = harness((_url, options) => { requests++; signal = options.signal; return new Promise(resolve => { deliver = resolve; }); });
  const preload = env.context.__gosx.scene3d.preloadModel("/preload.json");
  const state = { _modelHydrationGeneration: 1 };
  const mounted = api.load("/preload.json", null, { state, generation: 1 });
  api.invalidate(state);
  assert.equal(signal.aborted, false);
  deliver({ ok: true, json: async () => ({ objects: [{ id: "preloaded", kind: "box" }] }) });
  const [asset] = await Promise.all([preload, mounted]);
  assert.equal(asset.objects[0].id, "preloaded");
  await env.context.__gosx.scene3d.preloadModel("/preload.json");
  assert.equal(requests, 1);
  assert.equal(api.size(), 1);
});

test("retiring a model scope and cancelling progressive ownership releases its assets", async () => {
  let signal;
  const { api } = harness((_url, options) => { signal = options.signal; return new Promise(() => {}); });
  const token = {};
  const state = { _modelHydrationGeneration: 1, _modelProgressiveToken: token };
  const pending = api.load("/progressive.json", null, { state, generation: 1, token });
  api.cancelProgressive(state);
  await pending;
  assert.equal(signal.aborted, true);
  assert.equal(state._modelAssetLeases.size, 0);
  assert.equal(api.size(), 0);
});

test("actual GLTF loading cancels unresolved renderer selection without late publication", async () => {
  const { env, api } = harness(async () => ({ ok: true, json: async () => ({ asset: { version: "2.0" }, scenes: [{ nodes: [] }], nodes: [] }) }));
  runScript(freshFeatureBundleSource("scene3d-gltf"), env.context, "fresh-gltf.js");
  const state = { _modelHydrationGeneration: 1, _modelTextureVariantScope: api.scope() };
  const pending = api.load("/pending.gltf", null, { state, generation: 1 });
  await flushAsyncWork();
  api.invalidate(state);
  api.retire(state._modelTextureVariantScope);
  await pending;
  await flushAsyncWork();
  assert.equal(api.size(), 0);
  assert.equal(api.ready(), 0);
  assert.equal(state._modelTextureVariantScope.settled, true);
});

test("actual GLTF external buffer uses last-owner cancellation", async () => {
  let bufferSignal;
  const { env, api } = harness(async (url, options) => {
    if (String(url).endsWith("/buffer.bin")) {
      bufferSignal = options.signal;
      return new Promise(() => {});
    }
    return { ok: true, json: async () => ({ asset: { version: "2.0" }, buffers: [{ uri: "buffer.bin", byteLength: 12 }], scenes: [{ nodes: [] }], nodes: [] }) };
  });
  runScript(freshFeatureBundleSource("scene3d-gltf"), env.context, "fresh-gltf.js");
  const state = { _modelHydrationGeneration: 1, _modelTextureVariantScope: api.scope({ backend: "webgl" }) };
  const pending = api.load("/external.gltf", null, { state, generation: 1 });
  await flushAsyncWork();
  assert.ok(bufferSignal);
  api.invalidate(state);
  await pending;
  assert.equal(bufferSignal.aborted, true);
  assert.equal(api.size(), 0);
});

test("100 versioned neutral preloads remain bounded without mutating returned assets", async () => {
  const { env, api } = harness(async url => ({ ok: true, json: async () => ({ objects: [{ id: String(url), kind: "box" }] }) }));
  const first = await env.context.__gosx.scene3d.preloadModel("/version.json?v=0");
  for (let i = 1; i <= 100; i++) await env.context.__gosx.scene3d.preloadModel("/version.json?v=" + i);
  assert.equal(api.size(), 16);
  assert.equal(api.ready(), 16);
  assert.equal(first.objects[0].id, "/version.json?v=0");
});

test("scope-specific decoded assets never substitute a different backend result", async () => {
  const { env, api } = harness();
  let calls = 0;
  env.context.__gosx_scene3d_gltf_api = {
    async sceneLoadGLTFModel(_url, context) { calls++; const backend = (await context).backend; return { objects: [{ id: backend, kind: "box" }] }; },
    gltfSceneToModelAsset(value) { return value; },
  };
  for (const backend of ["webgl", "webgpu", "webgl"]) {
    const scope = api.scope({ backend });
    const state = { _modelHydrationGeneration: 1, _modelTextureVariantScope: scope };
    const asset = await api.load("/backend.glb", null, { state, generation: 1 });
    assert.equal(asset.objects[0].id, backend);
    api.invalidate(state);
    api.retire(scope);
    assert.equal(api.size(), 0);
  }
  assert.equal(calls, 3);
});

test("GLTF abort wait removes hung-work listeners and handles pre-aborted rejection", async () => {
  const { env } = harness();
  const source = freshFeatureBundleSource("scene3d-gltf");
  const marker = "function gltfAwaitModelWork(work, signal)";
  assert.ok(source.includes(marker));
  runScript(source.replace(marker, "window.__awaitModelTest = gltfAwaitModelWork;\n" + marker), env.context, "gltf-wait-test.js");
  const listeners = new Set();
  const signal = { aborted: false, addEventListener(_name, fn) { listeners.add(fn); }, removeEventListener(_name, fn) { listeners.delete(fn); } };
  const pending = env.context.__awaitModelTest(new Promise(() => {}), signal);
  assert.equal(listeners.size, 1);
  signal.aborted = true;
  Array.from(listeners).forEach(fn => fn());
  await assert.rejects(pending, /cancelled/);
  assert.equal(listeners.size, 0);
  await assert.rejects(env.context.__awaitModelTest(Promise.reject(new Error("transport rejection")), signal), /cancelled/);
  await flushAsyncWork();
  assert.equal(listeners.size, 0);
});

test("cancelled progressive full prefetch emits no late failure or swap", async () => {
  const { api } = harness(() => new Promise(() => {}));
  const mount = new FakeElement("div", null);
  const events = [];
  const assetEvents = [];
  mount.addEventListener("gosx:scene3d:progressive-model", event => events.push(event.detail.status));
  mount.addEventListener("gosx:scene3d:model-status", event => assetEvents.push(event.detail.status));
  const state = { _modelHydrationGeneration: 1, models: [{ id: "p", src: "/preview.json", previewSrc: "/preview.json", fullSrc: "/full.json", progressive: true }] };
  const pending = api.progressive(state, mount, Promise.resolve({ generation: 1, committed: true }), () => { throw Error("cancelled lifecycle must not swap"); });
  await flushAsyncWork();
  assert.deepEqual(events, ["preview-ready"]);
  api.cancelProgressive(state);
  await pending;
  assert.deepEqual(events, ["preview-ready"]);
  assert.equal(api.size(), 0);
  assert.deepEqual(assetEvents, ["loading"]);
});

test("failed mixed staging batch releases its leases and preserves committed records", async () => {
  const { env, api } = harness(async () => ({ ok: true, json: async () => ({ objects: [{ id: "mesh", kind: "box" }] }) }));
  const bad = await env.context.__gosx.scene3d.preloadModel("/bad.json");
  Object.defineProperty(bad, "objects", { get() { throw Error("staging-fault"); } });
  const state = env.context.__gosx_scene3d_api.createSceneState({ scene: { objects: [{ id: "committed", kind: "box" }] } });
  state.models = [{ id: "good", src: "/good.json" }, { id: "bad", src: "/bad.json" }];
  const result = await api.hydrate(state, {});
  assert.equal(result.committed, false);
  assert.deepEqual(Array.from(state.objects.keys()), ["committed"]);
  assert.equal(state._modelAssetLeases.size, 0);
});

test("empty successful progressive full response does not remain pinned", async () => {
  const { env, api } = harness(async () => ({ ok: true, json: async () => ({}) }));
  const mount = new FakeElement("div", null);
  const state = { _modelHydrationGeneration: 1, models: [{ id: "p", src: "/preview.json", previewSrc: "/preview.json", fullSrc: "/empty.json", progressive: true }] };
  await api.progressive(state, mount, Promise.resolve({ generation: 1, committed: true }), () => { throw Error("empty full must not swap"); });
  assert.equal(env.context.__gosx.scene3d.modelAssetResidency().owners, 0);
});

test("mixed full preloads release empty entries before swap and all leases on failed swap", async () => {
  const { env, api } = harness(async url => ({ ok: true, json: async () => String(url).includes("empty") ? {} : { objects: [{ id: "ready", kind: "box" }] } }));
  const mount = new FakeElement("div", null);
  const state = { _modelHydrationGeneration: 1, models: ["good", "empty"].map(id => ({ id, src: "/preview.json", previewSrc: "/preview.json", fullSrc: "/" + id + ".json", progressive: true })) };
  let swaps = 0;
  await api.progressive(state, mount, Promise.resolve({ generation: 1, committed: true }), models => {
    swaps++;
    assert.equal(env.context.__gosx.scene3d.modelAssetResidency().owners, 1);
    assert.equal(models[0].src, "/good.json");
    assert.equal(models[1].src, "/preview.json");
    return { generation: 1, committed: false };
  });
  assert.equal(swaps, 1);
  assert.equal(env.context.__gosx.scene3d.modelAssetResidency().owners, 0);
});

for (const cached of [false, true]) test(`loading-event revocation is rechecked before ${cached ? "cached" : "fresh"} acquisition`, async () => {
  let requests = 0, finish;
  const { api } = harness(() => { requests++; return new Promise(resolve => { finish = resolve; }); });
  const a = { _modelHydrationGeneration: 1 }, b = { _modelHydrationGeneration: 1 };
  const first = cached ? api.load("/reentrant.json", null, { state: a, generation: 1 }) : null;
  const mount = new FakeElement("div", null);
  mount.addEventListener("gosx:scene3d:model-status", event => {
    if (event.detail.status === "loading") { api.invalidate(a); api.invalidate(b); }
  });
  const pending = api.load("/reentrant.json", mount, { state: b, generation: 1 });
  if (finish) finish({ ok: true, json: async () => ({}) });
  if (first) await first;
  await pending;
  assert.equal(requests, cached ? 1 : 0);
  assert.equal(api.size(), 0);
});

test("actual pending engine disposal cancels initial Scene3D model before factory completion", async () => {
  const mount = new FakeElement("div", null); mount.id = "pending-model-owner";
  let signal, finish;
  const response = new Promise(resolve => { finish = resolve; });
  const env = createContext({ elements: [mount], fetchRoutes: {
    "/gosx/bootstrap-feature-engines.js": { text: freshFeatureBundleSource("engines") },
    "/initial-slow.json": (_url, init) => { signal = init.signal; return response; },
  }, manifest: { engines: [{ id: "pending-model", component: "GoSXScene3D", kind: "surface", mountId: mount.id,
    props: { width: 160, height: 160, models: [{ id: "initial", src: "/initial-slow.json" }] } }] } });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  let factorySignal;
  const register = env.context.__gosx_register_engine_factory;
  env.context.__gosx_register_engine_factory = function(name, factory) {
    return register(name, async ctx => { factorySignal = ctx.signal; return factory(ctx); });
  };
  runScript(freshFeatureBundleSource("scene3d"), env.context, "fresh-scene3d.js");
  for (let i = 0; i < 10 && !signal; i++) await flushAsyncWork();
  assert.ok(signal, "initial request started");
  assert.equal(mount.__gosxScene3DHandle, undefined, "initial factory still pending");
  env.context.__gosx_dispose_engine("pending-model");
  const abortedBeforeDelivery = signal.aborted;
  finish({ text: JSON.stringify({ objects: [{ id: "late", kind: "box" }] }) });
  for (let i = 0; i < 5; i++) await flushAsyncWork();
  assert.equal(abortedBeforeDelivery, true);
  assert.equal(factorySignal.aborted, true);
  assert.equal(factorySignal._listeners.size, 0);
  assert.equal(mount.__gosxScene3DHandle, undefined);
  assert.equal(env.context.__gosx.engines.has("pending-model"), false);
});

for (const unsupported of [false, true]) test(`initial factory signal detaches after ${unsupported ? "unsupported renderer" : "successful transfer"}`, async () => {
  const mount = new FakeElement("div", null); mount.id = "signal-owner";
  const env = createContext({ elements: [mount], disableCanvas2D: unsupported, fetchRoutes: {
    "/gosx/bootstrap-feature-engines.js": { text: freshFeatureBundleSource("engines") },
    "/ready.json": { text: JSON.stringify({ objects: [{ id: "box", kind: "box" }] }) },
  }, manifest: { engines: [{ id: "signal-engine", component: "GoSXScene3D", kind: "surface", mountId: mount.id,
    props: { width: 160, height: 160, models: [{ id: "initial", src: "/ready.json" }] } }] } });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  let signal;
  const register = env.context.__gosx_register_engine_factory;
  env.context.__gosx_register_engine_factory = function(name, factory) {
    return register(name, async ctx => { signal = ctx.signal; return factory(ctx); });
  };
  runScript(freshFeatureBundleSource("scene3d"), env.context, "fresh-scene3d.js");
  for (let i = 0; i < 10; i++) await flushAsyncWork();
  assert.ok(signal);
  assert.equal(signal._listeners.size, 0);
  assert.equal(signal.aborted, false, "transfer must not signal cancellation");
  if (!unsupported) assert.ok(mount.__gosxScene3DHandle);
  env.context.__gosx_dispose_engine("signal-engine");
  assert.equal(signal.aborted, false, "pending signal is not mounted lifetime");
});

test("generic rebootstrap cancels old pending model without aborting its replacement", async () => {
  const mount = new FakeElement("div", null); mount.id = "replacement-owner";
  let oldSignal, deliverOld;
  const oldResponse = new Promise(resolve => { deliverOld = resolve; });
  const entry = src => ({ id: "replacement", component: "GoSXScene3D", kind: "surface", mountId: mount.id,
    props: { width: 160, height: 160, models: [{ id: "model", src }] } });
  const env = createContext({ elements: [mount], fetchRoutes: {
    "/gosx/bootstrap-feature-engines.js": { text: freshFeatureBundleSource("engines") },
    "/old.json": (_url, init) => { oldSignal = init.signal; return oldResponse; },
    "/new.json": { text: JSON.stringify({ objects: [{ id: "new", kind: "box" }] }) },
  }, manifest: { engines: [entry("/old.json")] } });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  const signals = [];
  const register = env.context.__gosx_register_engine_factory;
  env.context.__gosx_register_engine_factory = function(name, factory) { return register(name, async ctx => { signals.push(ctx.signal); return factory(ctx); }); };
  runScript(freshFeatureBundleSource("scene3d"), env.context, "fresh-scene3d.js");
  for (let i=0;i<10 && !oldSignal;i++) await flushAsyncWork();
  assert.ok(oldSignal);
  env.context.document.body.removeChild(env.context.document.getElementById("gosx-manifest"));
  const nextManifest = env.context.document.createElement("script");
  nextManifest.id = "gosx-manifest";
  nextManifest.textContent = JSON.stringify({ engines: [entry("/new.json")] });
  env.context.document.body.appendChild(nextManifest);
  await env.context.__gosx_bootstrap_page();
  for (let i=0;i<5;i++) await flushAsyncWork();
  assert.equal(oldSignal.aborted, true);
  assert.equal(signals.length, 2);
  assert.equal(signals[0].aborted, true);
  assert.equal(signals[1].aborted, false);
  assert.ok(mount.__gosxScene3DHandle);
  deliverOld({ text: JSON.stringify({ objects: [{ id: "old", kind: "box" }] }) });
  await flushAsyncWork();
  assert.deepEqual(Array.from(mount.__gosxScene3DState.objects.keys()), ["model/new"]);
  assert.equal(signals[1]._listeners.size, 0);
});

test("superseding the sole slow model owner aborts its request", async () => {
  let signal;
  let resolve;
  const response = new Promise(r => { resolve = r; });
  const { api } = harness((_url, options) => { signal = options.signal; return response; });
  const state = { _modelHydrationGeneration: 1 };
  const pending = api.load("/slow.json", null, { state, generation: 1 });
  await flushAsyncWork();
  api.invalidate(state);
  // Resolve even when abort is unsupported by a transport: late delivery must be harmless.
  resolve({ ok: true, json: async () => ({ objects: [{ id: "late", kind: "box" }] }) });
  await pending;
  assert.equal(signal && signal.aborted, true);
  assert.equal(api.size(), 0);
});
