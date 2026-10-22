import test from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { bootstrapRuntimeSource, createContext, runScript, flushAsyncWork, FakeElement, sharedSignalValue } = require("./runtime-test-harness.js");

function contractElement() {
  const el = new FakeElement("script", null);
  el.id = "gosx-document";
  el.textContent = JSON.stringify({ version: 1, assets: { bootstrapMode: "full", manifest: true } });
  return el;
}

async function runtime(options) {
  const env = createContext({ elements: [contractElement()], manifest: { engines: [] }, ...options });
  const canvases = [];
  const create = env.document.createElement.bind(env.document);
  env.document.createElement = (tag) => { const el = create(tag); if (tag === "canvas") canvases.push(el); return el; };
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  return { env, api: env.context.__gosx_runtime_api, canvases };
}

test("hardware-gpu is false on a software WebGL2 renderer and asks for no major caveat", async () => {
  const { api, canvases } = await runtime({ enableWebGL2: true, renderer: "Google SwiftShader" });
  assert.equal(api.browserCapabilitySupported("hardware-gpu"), false);
  const probe = canvases.find((c) => c.lastContextKind === "webgl2");
  assert.ok(probe, "the probe must ask for a webgl2 context");
  assert.equal(probe.lastContextOptions.failIfMajorPerformanceCaveat, true);
});

test("hardware-gpu is true on a hardware renderer and false with no WebGL2", async () => {
  assert.equal((await runtime({ enableWebGL2: true, renderer: "ANGLE (NVIDIA, NVIDIA GeForce RTX 3060)" })).api.browserCapabilitySupported("hardware-gpu"), true);
  assert.equal((await runtime({})).api.browserCapabilitySupported("hardware-gpu"), false);
});

test("full-motion follows prefers-reduced-motion and is never cached", async () => {
  const { env, api } = await runtime({ prefersReducedMotion: true });
  assert.equal(api.browserCapabilitySupported("full-motion"), false);
  env.mediaQueries.get("(prefers-reduced-motion: reduce)").matches = false;
  assert.equal(api.browserCapabilitySupported("full-motion"), true);
});

test("full-data is false under Save-Data, 2G or prefers-reduced-data", async () => {
  const saveData = await runtime({});
  saveData.env.context.navigator.connection = { saveData: true };
  assert.equal(saveData.api.browserCapabilitySupported("full-data"), false);
  const slow = await runtime({});
  slow.env.context.navigator.connection = { effectiveType: "slow-2g" };
  assert.equal(slow.api.browserCapabilitySupported("full-data"), false);
  assert.equal((await runtime({ matchMedia: { "(prefers-reduced-data: reduce)": true } })).api.browserCapabilitySupported("full-data"), false);
  assert.equal((await runtime({})).api.browserCapabilitySupported("full-data"), true);
});

test("publishRendererChoice writes the document attribute, the signal and an event", async () => {
  const { env, api } = await runtime({});
  let seen = null;
  env.document.addEventListener("gosx:renderer", (event) => { seen = event.detail; });
  api.publishRendererChoice("webgl2", "hardware-gpu", "test");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-renderer"), "webgl2");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-renderer-reason"), "hardware-gpu");
  assert.deepEqual(JSON.parse(JSON.stringify(sharedSignalValue(env, "$gosx.renderer"))), { renderer: "webgl2", reason: "hardware-gpu", source: "test" });
  assert.deepEqual(JSON.parse(JSON.stringify(seen)), { renderer: "webgl2", reason: "hardware-gpu", source: "test" });
});
