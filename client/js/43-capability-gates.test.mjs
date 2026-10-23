import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { createContext, runScript } = require("./runtime-test-harness.js");

const chunkSource = fs.readFileSync(path.join(path.dirname(new URL(import.meta.url).pathname), "bootstrap-feature-scene3d-defer.js"), "utf8");

// Loads the scene3d-defer chunk against a stub feature API and returns the
// feature object, so the gates and the publisher run without the full runtime.
function loadFeature(options) {
  const env = createContext({ elements: [], ...options });
  const signals = {};
  let factory = null;
  env.context.__gosx_register_bootstrap_feature = (name, f) => { assert.equal(name, "scene3d-defer"); factory = f; };
  runScript(chunkSource, env.context, "bootstrap-feature-scene3d-defer.js");
  const feature = factory({ setSharedSignalJSON: (name, json) => { signals[name] = JSON.parse(json); }, engineFrame: (cb) => cb(), engineCapabilityStatus: () => ({ ok: true, missing: [], required: [], supported: [] }), applyRuntimeCapabilityState() {} });
  return { env, feature, signals };
}

test("hardware-gpu is false on a software WebGL2 renderer and asks for no major caveat", () => {
  const { env, feature } = loadFeature({ enableWebGL2: true, renderer: "Google SwiftShader" });
  const canvases = [];
  const create = env.document.createElement.bind(env.document);
  env.document.createElement = (tag) => { const el = create(tag); if (tag === "canvas") canvases.push(el); return el; };
  assert.equal(feature.detectHardwareGPU(), false);
  const probe = canvases.find((c) => c.lastContextKind === "webgl2");
  assert.ok(probe, "the probe must ask for a webgl2 context");
  assert.equal(probe.lastContextOptions.failIfMajorPerformanceCaveat, true);
});

test("hardware-gpu is true on a hardware renderer and false with no WebGL2", () => {
  assert.equal(loadFeature({ enableWebGL2: true, renderer: "ANGLE (NVIDIA, NVIDIA GeForce RTX 3060)" }).feature.detectHardwareGPU(), true);
  assert.equal(loadFeature({}).feature.detectHardwareGPU(), false);
});

test("full-motion follows prefers-reduced-motion and is never cached", () => {
  const { env, feature } = loadFeature({ prefersReducedMotion: true });
  assert.equal(feature.gates["full-motion"](), false);
  env.mediaQueries.get("(prefers-reduced-motion: reduce)").matches = false;
  assert.equal(feature.gates["full-motion"](), true);
});

test("full-data is false under Save-Data, 2G or prefers-reduced-data", () => {
  const saveData = loadFeature({});
  saveData.env.context.navigator.connection = { saveData: true };
  assert.equal(saveData.feature.gates["full-data"](), false);
  const slow = loadFeature({});
  slow.env.context.navigator.connection = { effectiveType: "slow-2g" };
  assert.equal(slow.feature.gates["full-data"](), false);
  assert.equal(loadFeature({ matchMedia: { "(prefers-reduced-data: reduce)": true } }).feature.gates["full-data"](), false);
  assert.equal(loadFeature({}).feature.gates["full-data"](), true);
});

test("publishRendererChoice writes the document attribute, the signal and an event", () => {
  const { env, feature, signals } = loadFeature({});
  let seen = null;
  env.document.addEventListener("gosx:renderer", (event) => { seen = event.detail; });
  feature.publishRendererChoice("webgl", "hardware-gpu", "test");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-renderer"), "webgl2");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-renderer-reason"), "hardware-gpu");
  assert.deepEqual(signals["$gosx.renderer"], { renderer: "webgl2", reason: "hardware-gpu", source: "test" });
  assert.deepEqual(JSON.parse(JSON.stringify(seen)), { renderer: "webgl2", reason: "hardware-gpu", source: "test" });
});
