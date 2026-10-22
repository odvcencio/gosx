import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const ts = require("../runtime/node_modules/typescript");
const source = fs.readFileSync(path.join(here, "../runtime/scene3d/mount-webgl.ts"), "utf8");
const start = source.indexOf("  async function sceneApplyMountedTextureProfile(");
const end = source.indexOf("  function resolveSceneModelAssetURL(", start);
assert.ok(start > 0 && end > start);
const fragment = ts.transpileModule(source.slice(start, end), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText;
const scopeStart = source.indexOf("  const sceneModelAssetCache = new Map();");
const scopeEnd = source.indexOf("  function publishSceneModelTextureVariantContext(", scopeStart);
assert.ok(scopeStart > 0 && scopeEnd > scopeStart);
const scopeFragment = ts.transpileModule(source.slice(scopeStart, scopeEnd), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText;
const loaderStart = source.indexOf("  async function loadSceneModelAsset(src, mount, hydrationMeta) {");
const loaderEnd = source.indexOf("  // Public prewarm hook for progressive single-engine upgrades", loaderStart);
assert.ok(loaderStart > 0 && loaderEnd > loaderStart);
const loaderFragment = ts.transpileModule(source.slice(loaderStart, loaderEnd), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText;
const coreSource = fs.readFileSync(path.join(here, "bootstrap-src/10-runtime-scene-core.ts"), "utf8");
const coreStart = coreSource.indexOf("  function sceneTrackModelHydrationPromise(");
const coreEnd = coreSource.indexOf("  function applySceneModelTransformCommand(", coreStart);
assert.ok(coreStart > 0 && coreEnd > coreStart);
const coreFragment = ts.transpileModule(coreSource.slice(coreStart, coreEnd), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText;

function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function harness() {
  const preload = deferred(), hydration = deferred();
  const manifest = {};
  const renderer = { kind: "webgl", textureVariantContext: { backend: "webgl", uploadReady: true, tokens: ["container:ktx2"] } };
  let currentRenderer = renderer, generation = 0, disposed = false, commits = 0;
  const context = vm.createContext({
    DOMException, Map, Set, loadManifest: () => manifest,
    sceneHydrationModels: (value) => value.models,
  });
  vm.runInContext(scopeFragment + fragment, context);
  const oldScope = context.createSceneModelTextureVariantScope(
    context.sceneModelTextureVariantContextForRenderer(renderer, "high"));
  const state = { models: [{ src: "/hero.glb" }], _modelTextureVariantScope: oldScope, _textureDeliveryProfile: "high" };
  context.rememberSceneModelTextureVariantScope(state, oldScope);
  const hooks = {
    disposed: () => disposed, renderer: () => currentRenderer,
    nextGeneration: () => ++generation, current: n => n === generation,
    rehydrate: () => { state._modelHydrationUncommitted = true; return hydration.promise.then(result => {
      state._modelHydrationUncommitted = false; return result;
    }); },
    invalidate: () => { hydration.resolve({ committed: false, stale: true }); },
    commit: () => { commits++; },
  };
  const requests = [{ id: "hero", src: "/hero.glb" }];
  const module = { prepare: (_mount, _requests, _signal, scope) => { module.scope = scope; return preload.promise; } };
  const apply = (signal = new AbortController().signal, selection = requests) => context.sceneApplyMountedTextureProfile(state, {}, selection, "low", signal, module, hooks);
  return { apply, state, oldScope, preload, hydration, module, hooks, context,
    get commits() { return commits; },
    replaceRenderer: () => { currentRenderer = { kind: "webgpu" }; },
    supersede: () => { generation++; },
    dispose: () => { disposed = true; } };
}

test("renderer and profile both change the actual mount scope fingerprint", () => {
  const context = vm.createContext({ Map, Set });
  vm.runInContext(scopeFragment + "\nthis.scopeAPI = { createSceneModelTextureVariantScope, sceneModelTextureVariantContextForRenderer };", context);
  const webgl = { kind: "webgl", textureVariantContext: { backend: "webgl", uploadReady: true, tokens: ["container:ktx2"] } };
  const webgpu = { kind: "webgpu", textureVariantContext: { backend: "webgpu", uploadReady: true, tokens: ["container:ktx2"] } };
  const scope = (renderer, profile) => context.scopeAPI.createSceneModelTextureVariantScope(
    context.scopeAPI.sceneModelTextureVariantContextForRenderer(renderer, profile));
  const low = scope(webgl, "low"), high = scope(webgl, "high"), gpu = scope(webgpu, "low");
  assert.notEqual(low.fingerprint, high.fingerprint);
  assert.notEqual(low.fingerprint, gpu.fingerprint);
  assert.notEqual(low.key, high.key);
});

test("profile switch keeps old scope through upload and staged hydration", async () => {
  const h = harness(), result = h.apply();
  assert.equal(h.state._modelTextureVariantScope, h.oldScope);
  assert.equal(h.module.scope.context.profile, "low");
  h.preload.resolve({ models: 1, textures: 2 });
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(h.state._modelTextureVariantScope, h.module.scope);
  assert.equal(h.state._textureDeliveryProfile, "high", "committed profile must lag staged hydration");
  h.hydration.resolve({ committed: true });
  assert.equal((await result).textures, 2);
  assert.equal(h.state._textureDeliveryProfile, "low");
  assert.equal(h.commits, 1);
});

test("failed upload, stale renderer, and failed hydration never claim the target profile", async () => {
  const failed = harness(), a = failed.apply();
  failed.preload.reject(new Error("map upload failed"));
  await assert.rejects(a, /map upload failed/);
  assert.equal(failed.state._modelTextureVariantScope, failed.oldScope);
  const stale = harness(), b = stale.apply();
  stale.replaceRenderer(); stale.preload.resolve({ textures: 1 });
  await assert.rejects(b, /stale/);
  assert.equal(stale.state._modelTextureVariantScope, stale.oldScope);
  const refused = harness(), c = refused.apply();
  refused.preload.resolve({ textures: 1 });
  await new Promise(resolve => setImmediate(resolve));
  refused.hydration.resolve({ committed: false });
  await assert.rejects(c, /did not commit/);
  assert.equal(refused.state._modelTextureVariantScope, refused.oldScope);
  assert.equal(refused.state._textureDeliveryProfile, "high");
});

test("canceled and superseded profile preparations leave the old render scope", async () => {
  const canceled = harness(), controller = new AbortController(), a = canceled.apply(controller.signal);
  controller.abort(); canceled.preload.resolve({ textures: 1 });
  await assert.rejects(a, /canceled/);
  assert.equal(canceled.state._modelTextureVariantScope, canceled.oldScope);
  const stale = harness(), b = stale.apply();
  stale.supersede(); stale.preload.resolve({ textures: 1 });
  await assert.rejects(b, /stale/);
  assert.equal(stale.state._modelTextureVariantScope, stale.oldScope);
});

test("abort after staged hydration commits keeps the drawn scope and reports the applied profile", async () => {
  const h = harness(), controller = new AbortController(), applied = h.apply(controller.signal);
  h.preload.resolve({ textures: 1 });
  await new Promise(resolve => setImmediate(resolve));
  h.state._modelHydrationUncommitted = false;
  controller.abort();
  h.hydration.resolve({ committed: true });
  assert.equal((await applied).profile, "low");
  assert.equal(h.state._modelTextureVariantScope, h.module.scope);
});

test("abort during hydration invalidates the staged generation and retains the old scope", async () => {
  const h = harness(), controller = new AbortController(), applied = h.apply(controller.signal);
  h.preload.resolve({ textures: 1 });
  await new Promise(resolve => setImmediate(resolve));
  controller.abort();
  await assert.rejects(applied, /did not commit/);
  assert.equal(h.state._modelTextureVariantScope, h.oldScope);
});

test("a 51-model profile prepares bounded batches and counts shared map records once", async () => {
  const h = harness(), shared = {}, batches = [];
  const requests = Array.from({ length: 51 }, (_, index) => ({ id: "model-" + index, src: "/model-" + index + ".glb" }));
  h.state.models = requests;
  h.module.prepare = (_mount, batch) => {
    batches.push(batch.length);
    return Promise.resolve({ textures: 1, records: [shared] });
  };
  const applied = h.apply(undefined, requests);
  await new Promise(resolve => setImmediate(resolve));
  h.hydration.resolve({ committed: true });
  assert.deepEqual(batches, [32, 19]);
  assert.deepEqual({ ...(await applied) }, { models: 51, textures: 1, profile: "low" });
});

test("overlapping apply cannot restore an uncommitted staged scope after failure", async () => {
  const h = harness(), first = h.apply();
  h.preload.resolve({ textures: 1 });
  await new Promise(resolve => setImmediate(resolve));
  const staged = h.state._modelTextureVariantScope;
  assert.notEqual(staged, h.oldScope);
  const drawn = h.oldScope;
  await assert.rejects(h.apply(), /commit already staged/);
  assert.equal(h.state._modelTextureVariantScope, staged);
  assert.equal(h.state._textureDeliveryProfile, "high");
  assert.equal(drawn, h.oldScope, "the existing material stays drawn during staged hydration");
  h.hydration.resolve({ committed: false });
  await assert.rejects(first, /did not commit/);
  assert.equal(h.state._modelTextureVariantScope, drawn);
  assert.equal(h.state._modelTextureProfileStage, null);
  assert.equal(h.commits, 0);
});

test("a model joining during upload invalidates bounded profile preparation before staging", async () => {
  const h = harness(), applied = h.apply();
  h.state.models.push({ id: "late", src: "/late.glb" });
  h.preload.resolve({ textures: 1 });
  await assert.rejects(applied, /active models changed/);
  assert.equal(h.state._modelTextureVariantScope, h.oldScope);
  assert.equal(h.state._textureDeliveryProfile, "high");
  assert.equal(h.commits, 0);
});

test("ordinary model rehydration rolls back a staged profile before taking its texture scope", async () => {
  const h = harness(), jobs = [], drawn = { scope: h.oldScope };
  h.context.hydrateSceneStateModels = state => {
    const job = deferred(), scope = state._modelTextureVariantScope;
    jobs.push({ job, scope });
    return job.promise.then(result => { if (result.committed) drawn.scope = scope; return result; });
  };
  vm.runInContext(coreFragment, h.context);
  h.hooks.rehydrate = () => h.context.sceneRehydrateModelsAfterCommand(h.state, true);
  const applied = h.apply();
  h.preload.resolve({ textures: 1 });
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(jobs.length, 1);
  assert.notEqual(jobs[0].scope, h.oldScope);
  assert.equal(drawn.scope, h.oldScope);
  const command = h.context.sceneRehydrateModelsAfterCommand(h.state);
  assert.equal(jobs.length, 2);
  assert.equal(jobs[1].scope, h.oldScope, "normal model command must capture committed texture scope");
  jobs[0].job.resolve({ committed: false, stale: true });
  await assert.rejects(applied, /did not commit/);
  jobs[1].job.resolve({ committed: true });
  await command;
  assert.equal(drawn.scope, h.oldScope);
  assert.equal(h.state._modelTextureVariantScope, h.oldScope);
  assert.equal(h.state._textureDeliveryProfile, "high");
  assert.equal(h.commits, 0);
});

function cacheHarness() {
  let decodeGate = null;
  let manifest = {}, decoded = 0, renderer = {
    kind: "webgl", textureVariantContext: { backend: "webgl", uploadReady: true, tokens: ["container:ktx2"] },
  };
  const context = vm.createContext({
    Map, Set, Promise, DOMException, console,
    loadManifest: () => manifest,
    sceneHydrationModels: state => state.models,
    sceneModelAssetFormat: () => "glb",
    sceneModelHydrationIsCurrent: () => false,
    parseSceneModelAsset: asset => asset,
    gosxSceneEmit: () => {},
    ensureGLTFFeatureLoaded: async () => ({
      sceneLoadGLTFModel: async (_src, ready) => {
        await ready;
        if (decodeGate) await decodeGate;
        decoded++;
        return { objects: [{ vertices: { count: 3 } }] };
      },
      gltfSceneToModelAsset: asset => asset,
    }),
  });
  vm.runInContext(scopeFragment + fragment + loaderFragment + `
    this.cacheAPI = { createSceneModelTextureVariantScope, rememberSceneModelTextureVariantScope,
      replaceSceneModelTextureVariantScope, sceneApplyMountedTextureProfile,
      retireSceneModelTextureVariantScopes, loadSceneModelAsset, sceneModelAssetCache, sceneModelAssetReady };`, context);
  const api = context.cacheAPI;
  function makeMount() {
    const initial = api.createSceneModelTextureVariantScope(
      context.sceneModelTextureVariantContextForRenderer(renderer, "high"));
    const state = { models: [{ id: "hero", src: "/generic-hero.glb" }],
      _modelTextureVariantScope: initial, _textureDeliveryProfile: "high" };
    api.rememberSceneModelTextureVariantScope(state, initial);
    let generation = 0, disposed = false;
    const hooks = { disposed: () => disposed, renderer: () => renderer,
      nextGeneration: () => ++generation, current: n => n === generation,
      rehydrate: async () => ({ committed: true }), commit: () => {} };
    const module = { prepare: async (_mount, requests, _signal, scope) => {
      for (const request of requests) await api.loadSceneModelAsset(request.src, null, { variantScope: scope });
      return { models: requests.length, textures: 1 };
    } };
    return { state, module, hooks,
      dispose: () => { disposed = true; api.retireSceneModelTextureVariantScopes(state); },
      apply: profile => api.sceneApplyMountedTextureProfile(
      state, {}, state.models, profile, new AbortController().signal, module, hooks) };
  }
  return { api, makeMount, get decoded() { return decoded; },
    blockDecode: gate => { decodeGate = gate; },
    replaceManifest: () => { manifest = {}; },
    replaceRenderer: value => { renderer = value; } };
}

test("actual model cache stays bounded across repeated profile revisits and isolates mounts", async () => {
  const h = cacheHarness(), first = h.makeMount();
  for (let i = 0; i < 24; i++) await first.apply(i % 2 ? "standard" : "low");
  assert.equal(h.decoded, 2, "twenty-four flips should decode one asset per profile");
  assert.equal(h.api.sceneModelAssetCache.size, 2);
  assert.equal(h.api.sceneModelAssetReady.size, 2);
  const second = h.makeMount();
  await second.apply("low");
  await second.apply("standard");
  assert.equal(h.decoded, 4, "another mount must not share scoped decoded assets");
  const firstScopes = Array.from(first.state._modelTextureOwnedScopes);
  first.dispose();
  assert.equal(first.state._modelTextureScopeRegistry, null);
  assert.equal(first.state._modelTextureOwnedScopes.size, 0);
  assert.ok(firstScopes.every(scope => scope.retired && scope.ownerState === null));
  assert.equal(h.api.sceneModelAssetCache.size, 2, "first disposal must preserve the other mount");
  h.api.retireSceneModelTextureVariantScopes(second.state);
  assert.equal(h.api.sceneModelAssetCache.size, 0);
  assert.equal(h.api.sceneModelAssetReady.size, 0);
});

test("manifest replacement and renderer capability changes do not reuse stale decoded assets", async () => {
  const h = cacheHarness(), mount = h.makeMount();
  await mount.apply("low");
  await mount.apply("standard");
  h.replaceManifest();
  await mount.apply("low");
  assert.equal(h.decoded, 3);
  assert.equal(h.api.sceneModelAssetCache.size, 1, "old manifest assets retire after commit");
  h.replaceRenderer({ kind: "webgpu", textureVariantContext: {
    backend: "webgpu", uploadReady: true, tokens: ["container:ktx2", "texture-format:rgba8unorm"] } });
  assert.equal(h.api.replaceSceneModelTextureVariantScope(mount.state, mount.hooks.renderer()).changed, true);
  await mount.apply("standard");
  assert.equal(h.decoded, 4);
  assert.equal(h.api.sceneModelAssetCache.size, 2, "new capability keeps a separate asset");
});

test("retired scope cannot repopulate ready cache after an in-flight decode", async () => {
  const pending = deferred(), h = cacheHarness(), mount = h.makeMount();
  const api = h.api;
  const context = vm.createContext({ Map, Set, Promise, console,
    sceneModelAssetFormat: () => "glb", sceneModelHydrationIsCurrent: () => false,
    parseSceneModelAsset: asset => asset,
    ensureGLTFFeatureLoaded: async () => ({ sceneLoadGLTFModel: async () => pending.promise,
      gltfSceneToModelAsset: asset => asset }),
  });
  vm.runInContext(scopeFragment + loaderFragment + `this.api = { createSceneModelTextureVariantScope,
    loadSceneModelAsset, sceneModelAssetCache, sceneModelAssetReady, retireSceneModelTextureVariantScope };`, context);
  const scope = context.api.createSceneModelTextureVariantScope({ backend: "webgl", profile: "low" });
  const loading = context.api.loadSceneModelAsset("/generic-hero.glb", null, { variantScope: scope });
  await new Promise(resolve => setImmediate(resolve));
  context.api.retireSceneModelTextureVariantScope(scope);
  pending.resolve({ objects: [{ vertices: { count: 3 } }] });
  await loading;
  assert.equal(context.api.sceneModelAssetCache.size, 0);
  assert.equal(context.api.sceneModelAssetReady.size, 0);
  api.retireSceneModelTextureVariantScopes(mount.state);
});


test("renderer recovery reuses normalized capability scopes across context loss", async () => {
  const h = cacheHarness(), mount = h.makeMount();
  await mount.apply("low");
  const original = mount.state._modelTextureVariantScope;
  for (let index = 0; index < 24; index++) {
    h.replaceRenderer({ kind: "lost" });
    h.api.replaceSceneModelTextureVariantScope(mount.state, mount.hooks.renderer());
    h.replaceRenderer({ kind: "webgl", textureVariantContext: {
      backend: "WEBGL", uploadReady: true, tokens: ["container:ktx2", "CONTAINER:KTX2"] } });
    const restored = h.api.replaceSceneModelTextureVariantScope(mount.state, mount.hooks.renderer());
    assert.equal(restored.scope, original);
    await mount.apply("low");
  }
  assert.equal(h.decoded, 1);
  assert.equal(h.api.sceneModelAssetCache.size, 1);
});

test("manifest replacement through renderer recovery retires every obsolete mount cache", async () => {
  const h = cacheHarness(), mount = h.makeMount();
  for (let index = 0; index < 24; index++) {
    await mount.apply("low");
    h.replaceManifest();
    const replacement = h.api.replaceSceneModelTextureVariantScope(mount.state, mount.hooks.renderer());
    await h.api.loadSceneModelAsset("/generic-hero.glb", null, { variantScope: replacement.scope });
    assert.equal(h.api.sceneModelAssetCache.size, 1);
    assert.equal(h.api.sceneModelAssetReady.size, 1);
    assert.equal(mount.state._modelTextureOwnedScopes.size, 1);
  }
  h.api.retireSceneModelTextureVariantScopes(mount.state);
  assert.equal(h.api.sceneModelAssetCache.size, 0);
});

test("failed and canceled new-manifest profiles preserve the drawn old scope and retry cached preparation", async () => {
  const h = cacheHarness(), mount = h.makeMount();
  await mount.apply("low");
  const drawn = mount.state._modelTextureVariantScope;
  h.replaceManifest();
  mount.hooks.rehydrate = async () => ({ committed: false });
  await assert.rejects(mount.apply("standard"), /did not commit/);
  assert.equal(mount.state._modelTextureVariantScope, drawn);
  assert.equal(drawn.retired, false);
  const controller = new AbortController();
  const prepare = mount.module.prepare;
  mount.module.prepare = async (...args) => { const result = await prepare(...args); controller.abort(); return result; };
  await assert.rejects(h.api.sceneApplyMountedTextureProfile(mount.state, {}, mount.state.models,
    "standard", controller.signal, mount.module, mount.hooks), /canceled/);
  assert.equal(mount.state._modelTextureVariantScope, drawn);
  assert.equal(drawn.retired, false);
  mount.module.prepare = prepare;
  mount.hooks.rehydrate = async () => ({ committed: true });
  await mount.apply("standard");
  assert.equal(h.decoded, 2, "failed/canceled attempts reuse the decoded candidate on retry");
  assert.equal(drawn.retired, true);
  assert.equal(h.api.sceneModelAssetCache.size, 1);
  assert.equal(h.api.sceneModelAssetReady.size, 1);
});

test("repeated failed manifest replacements remain bounded while protecting the drawn scope", async () => {
  const h = cacheHarness(), mount = h.makeMount();
  await mount.apply("low");
  const drawn = mount.state._modelTextureVariantScope;
  mount.hooks.rehydrate = async () => ({ committed: false });
  for (let index = 0; index < 24; index++) {
    h.replaceManifest();
    await assert.rejects(mount.apply("standard"), /did not commit/);
    assert.equal(mount.state._modelTextureVariantScope, drawn);
    assert.equal(drawn.retired, false);
    assert.equal(h.api.sceneModelAssetCache.size, 2);
    assert.equal(mount.state._modelTextureOwnedScopes.size, 2);
  }
  h.api.retireSceneModelTextureVariantScopes(mount.state);
  assert.equal(h.api.sceneModelAssetCache.size, 0);
  assert.equal(h.api.sceneModelAssetReady.size, 0);
});

test("loader failure evicts the candidate promise so the same profile can retry", async () => {
  let attempts = 0;
  const context = vm.createContext({ Map, Set, Promise, console: { warn: () => {} },
    gosxSceneEmit: () => {}, sceneModelAssetFormat: () => "glb", sceneModelHydrationIsCurrent: () => false,
    parseSceneModelAsset: asset => asset,
    ensureGLTFFeatureLoaded: async () => ({
      sceneLoadGLTFModel: async () => { if (++attempts === 1) throw new Error("network"); return { objects: [] }; },
      gltfSceneToModelAsset: asset => asset,
    }),
  });
  vm.runInContext(scopeFragment + loaderFragment + `this.api = { createSceneModelTextureVariantScope,
    loadSceneModelAsset, sceneModelAssetCache, sceneModelAssetReady };`, context);
  const api = context.api, scope = api.createSceneModelTextureVariantScope({ backend: "webgl", profile: "low" });
  await api.loadSceneModelAsset("/generic-hero.glb", null, { variantScope: scope });
  assert.equal(api.sceneModelAssetCache.size, 0);
  await api.loadSceneModelAsset("/generic-hero.glb", null, { variantScope: scope });
  assert.equal(attempts, 2);
  assert.equal(api.sceneModelAssetCache.size, 1);
  assert.equal(api.sceneModelAssetReady.size, 1);
});


test("disposing a mount during actual model decode cannot repopulate caches or commit the profile", async () => {
  const h = cacheHarness(), mount = h.makeMount(), pending = deferred();
  h.blockDecode(pending.promise);
  const applying = mount.apply("low");
  await new Promise(resolve => setImmediate(resolve));
  const scopes = Array.from(mount.state._modelTextureOwnedScopes);
  assert.equal(h.api.sceneModelAssetCache.size, 1);
  mount.dispose();
  assert.ok(scopes.every(scope => scope.retired && scope.ownerState === null));
  assert.equal(mount.state._modelTextureOwnedScopes.size, 0);
  assert.equal(mount.state._modelTextureScopeRegistry, null);
  pending.resolve();
  await assert.rejects(applying, /stale/);
  assert.equal(mount.state._textureDeliveryProfile, "high");
  assert.equal(h.api.sceneModelAssetCache.size, 0);
  assert.equal(h.api.sceneModelAssetReady.size, 0);
});
