import test from "node:test";
import assert from "node:assert/strict";
import vm from "node:vm";
import fs from "node:fs";
import { freshFeatureBundleSource } from "./runtime-test-harness.js";

import { sceneGatedLoaders } from "./scene3d-gated-loader-fixture.mjs";

const source = freshFeatureBundleSource("scene3d-presentation");
const commands = freshFeatureBundleSource("scene3d-command");
const facade = fs.readFileSync(new URL("../runtime/scene3d/command-bridge.ts", import.meta.url), "utf8");
const features = [
  { method: "playTimeline", chunk: "timeline", api: "__gosx_scene3d_timeline_api" },
  { method: "burstParticles", chunk: "particle-burst", api: "__gosx_scene3d_particle_burst_api" },
];

function runtime() {
  const scripts = [], timers = [];
  const handle = { __gosxScene3DCommandReady: true, applyCommands() {} };
  const mount = { id: "scene", __gosxScene3DHandle: handle };
  let currentMount = mount, now = 0;
  const tag = { nonce: "old-scene-nonce", dataset: {} };
  const document = {
    currentScript: { nonce: "active-document-nonce" },
    createElement: () => ({ attributes: new Map(), setAttribute(name, value) { this.attributes.set(name, value); }, getAttribute(name) { return this.attributes.get(name); } }),
    head: { appendChild: script => scripts.push(script) }, querySelector: () => tag,
    getElementById: () => currentMount, querySelectorAll: () => currentMount ? [currentMount] : [],
  };
  const window = { __gosx_scene3d_api: {}, __gosx_scene3d_compute_api: {} };
  const context = vm.createContext({ window, document, Date: { now: () => now }, setTimeout: callback => timers.push(callback) });
  vm.runInContext(sceneGatedLoaders, context);
  vm.runInContext(source, context);
  vm.runInContext(commands, context);
  vm.runInContext(facade, context);
  for (const feature of [...features, { chunk: "compute" }, { chunk: "presentation" }]) {
    const key = "gosxScene3d" + feature.chunk.split("-").map(part => part[0].toUpperCase() + part.slice(1)).join("") + "Url";
    tag.dataset[key] = "/assets/" + feature.chunk + ".js";
  }
  return { window, mount, handle, scripts, timers, document, dataset: tag.dataset, bridge: window.__gosx_scene3d_presentation_api, commands: window.__gosx_scene3d_command_bridge, context, facade: window.__gosx.scene3d,
    setMount(value) { currentMount = value; }, advance(ms) { now += ms; timers.shift()(); } };
}

for (const feature of features) {
  test(feature.method + " first play loads presentation then playback without the command chunk", async () => {
    const r = runtime();
    delete r.window.__gosx_scene3d_presentation_api;
    delete r.window.__gosx_scene3d_command_bridge;
    const first = r.facade[feature.method](r.mount, { id: "first" });
    const second = r.facade[feature.method](r.mount, { id: "second" });
    assert.equal(r.scripts.length, 1, "coordinator loading is shared");
    assert.equal(r.scripts[0].src, "/assets/presentation.js");
    assert.equal(r.scripts[0].nonce, "active-document-nonce");
    vm.runInContext(source, r.context);
    r.scripts[0].onload();
    await Promise.resolve(); await Promise.resolve();
    assert.equal(r.scripts.length, 2);
    assert.equal(r.scripts[1].src, "/assets/" + feature.chunk + ".js");
    r.window[feature.api] = { attach: value => value.id };
    r.scripts[1].onload();
    assert.deepEqual(await Promise.all([first, second]), ["first", "second"]);
    assert.equal(r.window.__gosx_scene3d_command_bridge, undefined);
    assert.deepEqual(r.scripts.map(script => script.src), ["/assets/presentation.js", "/assets/" + feature.chunk + ".js"]);
  });

  test(feature.method + " facade preserves custom handles and rejects unadvertised coordination", async () => {
    const r = runtime();
    delete r.window.__gosx_scene3d_presentation_api;
    for (const key of Object.keys(r.dataset)) delete r.dataset[key];
    const value = { id: "custom" };
    const custom = { __gosxScene3DCommandReady: true, applyCommands() {}, [feature.method]: spec => spec };
    assert.equal(await r.facade[feature.method](custom, value), value);
    for (const timeoutMS of [NaN, Infinity, -Infinity, "later"]) {
      await assert.rejects(r.facade[feature.method](custom, value, { timeoutMS }), /timeout must be finite/);
    }
    custom[feature.method] = () => { throw new Error("custom failure"); };
    await assert.rejects(r.facade[feature.method](custom, value), /custom failure/);
    await assert.rejects(r.facade[feature.method](r.mount, value), /presentation chunk URL was not advertised/);
    assert.equal(r.scripts.length, 0);
  });
}

test("presentation coordination retries failure and missing publication through the canonical loader", async () => {
  const r = runtime();
  delete r.window.__gosx_scene3d_presentation_api;
  const failed = assert.rejects(r.facade.playTimeline(r.mount, {}), /failed to load scene3d-presentation/);
  r.scripts[0].onerror(); await failed;
  const unpublished = assert.rejects(r.facade.playTimeline(r.mount, {}), /did not publish API/);
  r.scripts[1].onload(); await unpublished;
  r.window.__gosx_scene3d_timeline_api = { attach: () => "attached" };
  const retry = r.facade.playTimeline(r.mount, {});
  vm.runInContext(source, r.context);
  r.scripts[2].onload();
  assert.equal(await retry, "attached");
  assert.equal(r.scripts.length, 3);
});

test("command coordination retains its legacy fallback URL and publication contract", async () => {
  const r = runtime();
  delete r.window.__gosx_scene3d_command_bridge;
  const received = [];
  r.handle.applyCommands = value => received.push(value);
  const pending = r.facade.dispatchCommands(r.handle, [{ kind: 5 }]);
  assert.equal(r.scripts[0].src, "/gosx/bootstrap-feature-scene3d-command.js");
  vm.runInContext(commands, r.context);
  r.scripts[0].onload();
  assert.equal((await pending).applied, true);
  assert.equal(received.length, 1);
  assert.equal(typeof r.window.__gosx_scene3d_command_bridge.dispatchCommands, "function");
  await r.facade.dispatchCommands(r.handle, []);
  assert.equal(r.scripts.length, 1, "the canonical loader reuses the legacy bridge publication");
});

for (const feature of features) {
  test(feature.method + " coalesces script loading and fences attachment to its original owner", async () => {
    const r = runtime(), attachments = [];
    delete r.window.__gosx_scene3d_compute_api;
    const first = r.bridge[feature.method](r.mount, { id: "first" });
    const second = r.bridge[feature.method](r.mount, { id: "second" });
    assert.equal(r.scripts.length, 1);
    const script = r.scripts[0];
    assert.equal(script.src, "/assets/" + feature.chunk + ".js");
    assert.equal(script.nonce, "active-document-nonce", "a stale feature tag nonce must not override the active document");
    assert.equal(script.getAttribute("crossorigin"), "anonymous");
    assert.equal(script.getAttribute("referrerpolicy"), "no-referrer");
    assert.equal(script.getAttribute("type"), "text/javascript");
    assert.equal(script.getAttribute("data-gosx-script"), "feature-scene3d-" + feature.chunk);
    assert.equal(script.async, false);
    r.window[feature.api] = { attach(value, mount, handle, ownsMount) {
      assert.equal(mount, r.mount); assert.equal(handle, r.handle);
      attachments.push({ id: value.id, current: ownsMount() });
      return value.id;
    } };
    script.onload();
    if (feature.chunk === "particle-burst") {
      await Promise.resolve(); await Promise.resolve();
      assert.equal(attachments.length, 0, "burst attachment waits for compute readiness");
      assert.equal(r.scripts.length, 2, "compute loading is shared across callers");
      assert.equal(r.scripts[1].src, "/assets/compute.js");
      r.window.__gosx_scene3d_compute_api = {};
      r.scripts[1].onload();
    }
    r.mount.__gosxScene3DHandle = {};
    assert.deepEqual(await Promise.all([first, second]), ["first", "second"]);
    assert.deepEqual(attachments, [{ id: "first", current: false }, { id: "second", current: false }]);
  });

  test(feature.method + " retries failed loads and rejects a missing publication", async () => {
    const r = runtime();
    const failed = assert.rejects(r.bridge[feature.method](r.mount, {}), /failed to load/);
    r.scripts[0].onerror(); await failed;
    const unpublished = assert.rejects(r.bridge[feature.method](r.mount, {}), /did not publish API/);
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
    await assert.rejects(r.bridge[feature.method]("scene", {}, { timeoutMS: 0 }), /target .*ready/);
    assert.equal(r.timers.length, 0);
    const expired = assert.rejects(r.bridge[feature.method]("scene", {}, { timeoutMS: 16 }), /target .*ready/);
    r.advance(16); await expired;
    const pending = r.bridge[feature.method]("scene", { id: "ready" }, { timeoutMS: 16 });
    r.window[feature.api] = { attach: value => value.id };
    r.setMount(r.mount); r.advance(1);
    assert.equal(await pending, "ready");
  });
}

for (const cached of [false, true]) {
  test("burst compute errors retry after " + (cached ? "cached" : "cold") + " presentation readiness", async () => {
    const r = runtime(), feature = features[1];
    delete r.window.__gosx_scene3d_compute_api;
    if (cached) r.window[feature.api] = { attach: () => "attached" };
    const failed = assert.rejects(r.bridge.burstParticles(r.mount, {}), /failed to load scene3d-compute/);
    if (!cached) { r.window[feature.api] = { attach: () => "attached" }; r.scripts[0].onload(); }
    await Promise.resolve(); await Promise.resolve();
    const compute = r.scripts.at(-1);
    assert.equal(compute.src, "/assets/compute.js");
    compute.onerror(); await failed;
    const retried = r.bridge.burstParticles(r.mount, {});
    await Promise.resolve(); await Promise.resolve();
    assert.notEqual(r.scripts.at(-1), compute);
    r.window.__gosx_scene3d_compute_api = {};
    r.scripts.at(-1).onload();
    assert.equal(await retried, "attached");
    assert.equal(r.scripts.filter(script => script.src.includes("particle-burst")).length, cached ? 0 : 1);
  });
}

for (const feature of features) {
  test(feature.method + " refuses unadvertised URLs and preserves custom ready handles", async () => {
    const r = runtime();
    for (const key of Object.keys(r.dataset)) delete r.dataset[key];
    await assert.rejects(r.bridge[feature.method](r.mount, {}), /chunk URL was not advertised/);
    assert.equal(r.scripts.length, 0);
    const custom = { __gosxScene3DCommandReady: true, applyCommands() {}, [feature.method]: value => value };
    const value = { id: "custom" };
    assert.equal(await r.bridge[feature.method](custom, value), value);
    custom[feature.method] = () => { throw new Error("custom failure"); };
    await assert.rejects(r.bridge[feature.method](custom, {}), /custom failure/);
    assert.equal(r.scripts.length, 0);
  });
}

test("shared readiness retains fractional presentation deadlines and legacy command rounding", async () => {
  for (const feature of features) {
    const r = runtime(); r.setMount(null);
    const pending = assert.rejects(r.bridge[feature.method]("scene", {}, { timeoutMS: 0.5 }), /target .*ready/);
    r.advance(0.25); assert.equal(r.timers.length, 1);
    r.advance(0.25); await pending;
  }
  const r = runtime(); r.setMount(null);
  const pending = assert.rejects(r.commands.dispatchCommands("scene", [], { timeoutMS: 1.5 }), /target did not become ready/);
  r.advance(1); await pending;
});

test("shared command readiness preserves the legacy zero timeout and rejects a late application failure", async () => {
  const r = runtime();
  r.setMount(null);
  const pending = r.commands.dispatchCommands("scene", [], { timeoutMS: 0 });
  const rejected = assert.rejects(pending, /renderer disposed/);
  assert.equal(r.timers.length, 1, "legacy command zero uses its default readiness window");
  r.handle.applyCommands = () => { throw new Error("renderer disposed"); };
  r.setMount(r.mount);
  r.advance(1);
  await rejected;
  assert.equal(r.timers.length, 0);
});
