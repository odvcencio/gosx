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

function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function harness() {
  const preload = deferred(), hydration = deferred();
  const oldScope = { key: "old", context: { profile: "high" } };
  const state = { models: [{ src: "/hero.glb" }], _modelTextureVariantScope: oldScope, _textureDeliveryProfile: "high" };
  const renderer = { kind: "webgl", textureVariantContext: { backend: "webgl", uploadReady: true, tokens: ["container:ktx2"] } };
  let currentRenderer = renderer, generation = 0, disposed = false, commits = 0;
  const context = vm.createContext({
    DOMException, Set,
    sceneHydrationModels: (value) => value.models,
    sceneModelTextureVariantContextForRenderer: (value, profile) => ({ ...value.textureVariantContext, profile }),
    createSceneModelTextureVariantScope: (value) => ({ key: "target", context: value }),
  });
  vm.runInContext(fragment, context);
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
  const apply = (signal = new AbortController().signal) => context.sceneApplyMountedTextureProfile(state, {}, requests, "low", signal, module, hooks);
  return { apply, state, oldScope, preload, hydration, module, hooks,
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
