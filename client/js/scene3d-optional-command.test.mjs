import test from "node:test";
import assert from "node:assert/strict";
import vm from "node:vm";
import { freshFeatureBundleSource } from "./runtime-test-harness.js";

const source = freshFeatureBundleSource("scene3d-command");
const features = [
  { method: "playTimeline", chunk: "timeline", api: "__gosx_scene3d_timeline_api" },
  { method: "burstParticles", chunk: "particle-burst", api: "__gosx_scene3d_particle_burst_api" },
];

function runtime() {
  const scripts = [], timers = [], attributes = new Map();
  const handle = { __gosxScene3DCommandReady: true, applyCommands() {} };
  const mount = { id: "scene", __gosxScene3DHandle: handle };
  let currentMount = mount, now = 0;
  const tag = { nonce: "scene-nonce", getAttribute(name) { return name === "data-gosx-script" ? "feature-scene3d" : attributes.get(name); } };
  const document = {
    scripts: [tag], createElement: () => ({}), head: { appendChild: script => scripts.push(script) },
    getElementById: () => currentMount, querySelectorAll: () => currentMount ? [currentMount] : [],
  };
  const window = { __gosx_ensure_scene3d_compute_loaded: () => Promise.resolve() };
  const context = vm.createContext({ window, document, Date: { now: () => now }, setTimeout: callback => timers.push(callback) });
  vm.runInContext(source, context);
  for (const feature of features) attributes.set("data-gosx-scene3d-" + feature.chunk + "-url", "/assets/" + feature.chunk + ".js");
  return { window, mount, handle, scripts, timers, attributes, bridge: window.__gosx_scene3d_command_bridge,
    setMount(value) { currentMount = value; }, advance(ms) { now += ms; timers.shift()(); } };
}

for (const feature of features) {
  test(feature.method + " coalesces script loading and fences attachment to its original owner", async () => {
    const r = runtime(), attachments = [];
    let releaseCompute;
    r.window.__gosx_ensure_scene3d_compute_loaded = () => new Promise(resolve => { releaseCompute = resolve; });
    const first = r.bridge[feature.method](r.mount, { id: "first" });
    const second = r.bridge[feature.method](r.mount, { id: "second" });
    assert.equal(r.scripts.length, 1);
    const script = r.scripts[0];
    assert.equal(script.src, "/assets/" + feature.chunk + ".js");
    assert.equal(script.nonce, "scene-nonce");
    assert.equal(script.crossOrigin, "anonymous");
    assert.equal(script.referrerPolicy, "no-referrer");
    r.window[feature.api] = { attach(value, mount, handle, ownsMount) {
      assert.equal(mount, r.mount); assert.equal(handle, r.handle);
      attachments.push({ id: value.id, current: ownsMount() });
      return value.id;
    } };
    script.onload();
    if (feature.chunk === "particle-burst") {
      await Promise.resolve(); await Promise.resolve();
      assert.equal(attachments.length, 0, "burst attachment waits for compute readiness");
      releaseCompute();
    }
    r.mount.__gosxScene3DHandle = {};
    assert.deepEqual(await Promise.all([first, second]), ["first", "second"]);
    assert.deepEqual(attachments, [{ id: "first", current: false }, { id: "second", current: false }]);
  });

  test(feature.method + " retries failed loads and rejects a missing publication", async () => {
    const r = runtime();
    const failed = assert.rejects(r.bridge[feature.method](r.mount, {}), /failed to load/);
    r.scripts[0].onerror(); await failed;
    const unpublished = assert.rejects(r.bridge[feature.method](r.mount, {}), /did not publish its API/);
    assert.equal(r.scripts.length, 2);
    r.scripts[1].onload(); await unpublished;
    const retried = r.bridge[feature.method](r.mount, { id: "retry" });
    assert.equal(r.scripts.length, 3);
    r.window[feature.api] = { attach: value => value.id };
    r.scripts[2].onload();
    assert.equal(await retried, "retry");
  });

  test(feature.method + " polls stable targets and respects a zero readiness timeout", async () => {
    const r = runtime();
    r.setMount(null);
    await assert.rejects(r.bridge[feature.method]("scene", {}, { timeoutMS: 0 }), /target is not ready/);
    assert.equal(r.timers.length, 0);
    const expired = assert.rejects(r.bridge[feature.method]("scene", {}, { timeoutMS: 16 }), /target is not ready/);
    r.advance(16); await expired;
    const pending = r.bridge[feature.method]("scene", { id: "ready" }, { timeoutMS: 16 });
    r.window[feature.api] = { attach: value => value.id };
    r.setMount(r.mount); r.advance(1);
    assert.equal(await pending, "ready");
  });
}

test("burst compute failures reject callers and permit retry without refetching the published chunk", async () => {
  const r = runtime(), feature = features[1];
  r.window[feature.api] = { attach: () => "attached" };
  r.window.__gosx_ensure_scene3d_compute_loaded = () => Promise.reject(new Error("compute unavailable"));
  await assert.rejects(r.bridge.burstParticles(r.mount, {}), /compute unavailable/);
  r.window.__gosx_ensure_scene3d_compute_loaded = () => Promise.resolve();
  assert.equal(await r.bridge.burstParticles(r.mount, {}), "attached");
  assert.equal(r.scripts.length, 0);
});

test("shared command readiness preserves the legacy zero timeout and rejects a late application failure", async () => {
  const r = runtime();
  r.setMount(null);
  const pending = r.bridge.dispatchCommands("scene", [], { timeoutMS: 0 });
  const rejected = assert.rejects(pending, /renderer disposed/);
  assert.equal(r.timers.length, 1, "legacy command zero uses its default readiness window");
  r.handle.applyCommands = () => { throw new Error("renderer disposed"); };
  r.setMount(r.mount);
  r.advance(1);
  await rejected;
  assert.equal(r.timers.length, 0);
});
