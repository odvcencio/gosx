// Device-evidence tests for 16z-scene-webgpu-probe.ts.
//
// Two claims are under test.
//
// The first is the device request. WebGPU permits a texture format only when the
// DEVICE was created with the feature that unlocks it, and a device cannot gain
// a feature after requestDevice. The probe used to ask for an optional feature
// only when the manifest set adaptiveQuality or asked for every optional
// feature, so an ordinary page got a device with no block support on a
// block-capable adapter and a BC7 upload threw.
//
// The second is the evidence. adapter.features says what the hardware could do.
// Only device.features says what this device was created with, and a browser may
// grant fewer than asked. A token set built from the adapter promises formats
// the device will refuse.
//
// The fake device grants exactly the intersection of the requested features and
// its own grant list, which is what a browser does. That is what lets the first
// claim fail when the request drops a feature.
//
// The WebGL2 half mirrors TestFromDeviceEvidenceSplitsTheWebGL2Extensions in
// assetpipe/variantsel/device_test.go, which is the contract.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");
const probeSource = fs.readFileSync(path.join(srcDir, "16z-scene-webgpu-probe.ts"), "utf8");

// featureSet builds the setlike object a GPUAdapter and a GPUDevice expose.
function featureSet(names) {
  const values = new Set(names);
  return {
    has: (name) => values.has(name),
    forEach: (fn) => values.forEach((value) => fn(value)),
  };
}

// fakeAdapter reports `advertised` and grants `granted` at requestDevice.
//
// Leaving `granted` undefined means the adapter grants whatever it advertises,
// which is the common case. Passing a shorter list models a browser that refused
// part of the request, which is the case that separates adapter evidence from
// device evidence.
function fakeAdapter(advertised, granted) {
  const grantable = new Set(granted === undefined ? advertised : granted);
  const calls = [];
  const adapter = {
    features: featureSet(advertised),
    limits: {},
    info: {},
    requestDevice(descriptor) {
      calls.push(descriptor || null);
      const required = (descriptor && descriptor.requiredFeatures) || [];
      const allowed = required.filter((name) => grantable.has(name));
      return Promise.resolve({
        features: featureSet(allowed),
        limits: {},
        lost: new Promise(() => {}),
      });
    },
  };
  return { adapter, calls };
}

// loadProbe evaluates the probe fragment in this realm and returns the globals
// it publishes. `window` is a parameter, so the fragment's own guards see it.
async function loadProbe(options) {
  const opts = options || {};
  const window = {};
  const navigator = {
    deviceMemory: opts.deviceMemory === undefined ? 8 : opts.deviceMemory,
    gpu: opts.adapter === null ? undefined : {
      requestAdapter: () => Promise.resolve(opts.adapter),
      getPreferredCanvasFormat: () => "bgra8unorm",
    },
  };
  const manifest = opts.manifest || { engines: [] };
  const warnings = [];
  const console = { warn: (...args) => warnings.push(args.join(" ")), error: () => {}, log: () => {} };
  const factory = new Function(
    "window", "navigator", "console", "loadManifest",
    probeSource + "\nreturn window;"
  );
  factory(window, navigator, console, () => manifest);
  await window.__gosx_scene3d_webgpu_probe_ready();
  return { window, warnings };
}

// tokenSet turns the token array into a membership test.
function tokenSet(tokens) {
  assert.ok(Array.isArray(tokens), "the token reader must return an array");
  return new Set(tokens);
}

const BC_FORMAT_TOKENS = [
  "texture-format:bc1-rgba-unorm",
  "texture-format:bc1-rgba-unorm-srgb",
  "texture-format:bc3-rgba-unorm",
  "texture-format:bc3-rgba-unorm-srgb",
  "texture-format:bc4-r-unorm",
  "texture-format:bc5-rg-unorm",
  "texture-format:bc7-rgba-unorm",
  "texture-format:bc7-rgba-unorm-srgb",
];

const ASTC_FORMAT_TOKENS = [
  "texture-format:astc-4x4-unorm",
  "texture-format:astc-4x4-unorm-srgb",
  "texture-format:astc-8x8-unorm-srgb",
];

// --- the device request ------------------------------------------------------

test("a plain page still requests the block texture features", async () => {
  const { adapter, calls } = fakeAdapter(["texture-compression-bc", "shader-f16", "timestamp-query"]);
  // The manifest asks for nothing: no adaptiveQuality, no optional features.
  const { window } = await loadProbe({ adapter, manifest: { engines: [{ component: "GoSXScene3D", props: {} }] } });

  assert.equal(calls.length, 1, "the probe must create exactly one device");
  const requested = (calls[0] && calls[0].requiredFeatures) || [];
  assert.ok(
    requested.includes("texture-compression-bc"),
    "requestDevice must ask for texture-compression-bc; a device cannot gain it later"
  );
  assert.ok(!requested.includes("shader-f16"), "a plain page must not gain the other optional features");

  const snapshot = window.__gosx_scene3d_webgpu_diagnostics();
  assert.ok(snapshot.deviceFeatures.includes("texture-compression-bc"), "the device must hold the granted feature");

  const tokens = tokenSet(window.__gosx_scene3d_texture_tokens());
  assert.ok(tokens.has("device-feature:texture-compression-bc"));
  for (const token of BC_FORMAT_TOKENS) {
    assert.ok(tokens.has(token), `the BC device must unlock ${token}`);
  }
});

test("the probe asks for every block family the adapter reports", async () => {
  const { adapter, calls } = fakeAdapter([
    "texture-compression-bc", "texture-compression-etc2", "texture-compression-astc",
  ]);
  await loadProbe({ adapter });
  const requested = (calls[0] && calls[0].requiredFeatures) || [];
  for (const feature of ["texture-compression-bc", "texture-compression-etc2", "texture-compression-astc"]) {
    assert.ok(requested.includes(feature), `requestDevice must ask for ${feature}`);
  }
});

test("the probe never asks for a family the adapter did not report", async () => {
  const { adapter, calls } = fakeAdapter(["texture-compression-bc"]);
  await loadProbe({ adapter });
  const requested = (calls[0] && calls[0].requiredFeatures) || [];
  assert.ok(!requested.includes("texture-compression-astc"), "an absent feature makes requestDevice throw");
  assert.ok(!requested.includes("texture-compression-etc2"));
});

test("the sliced-3d guard still needs its base family", async () => {
  // An adapter reporting the sliced-3d entry without its base family is not a
  // real device, but the guard exists to stop the pair from separating.
  const { adapter, calls } = fakeAdapter(["texture-compression-bc-sliced-3d", "texture-compression-astc"]);
  await loadProbe({
    adapter,
    manifest: { engines: [{ component: "GoSXScene3D", props: { webgpuOptionalFeatures: true } }] },
  });
  const requested = (calls[0] && calls[0].requiredFeatures) || [];
  assert.ok(
    !requested.includes("texture-compression-bc-sliced-3d"),
    "the sliced-3d feature must not be requested without texture-compression-bc"
  );
  assert.ok(requested.includes("texture-compression-astc"));
});

// --- the evidence ------------------------------------------------------------

test("the token set follows the device and not the adapter", async () => {
  // The adapter advertises two families and the browser grants one. This is the
  // memory-tight case the probe's own retry comment describes.
  const { adapter } = fakeAdapter(
    ["texture-compression-bc", "texture-compression-astc"],
    ["texture-compression-bc"]
  );
  const { window } = await loadProbe({ adapter });

  const snapshot = window.__gosx_scene3d_webgpu_diagnostics();
  assert.ok(snapshot.supportedFeatures.includes("texture-compression-astc"), "the adapter still advertises ASTC");
  assert.ok(!snapshot.deviceFeatures.includes("texture-compression-astc"), "the device was not granted ASTC");

  const tokens = tokenSet(window.__gosx_scene3d_texture_tokens());
  assert.ok(tokens.has("device-feature:texture-compression-bc"));
  assert.ok(
    !tokens.has("device-feature:texture-compression-astc"),
    "an ASTC token from adapter evidence would name a format this device refuses"
  );
  for (const token of ASTC_FORMAT_TOKENS) {
    assert.ok(!tokens.has(token), `${token} must not appear when the device lacks ASTC`);
  }
});

test("a device with no block feature reports only the uncompressed guarantees", async () => {
  const { adapter } = fakeAdapter(["timestamp-query"]);
  const { window } = await loadProbe({ adapter });
  const tokens = tokenSet(window.__gosx_scene3d_texture_tokens());

  assert.ok(tokens.has("backend:webgpu"));
  assert.ok(tokens.has("container:ktx2"));
  assert.ok(tokens.has("container:ktx2-zlib"));
  assert.ok(tokens.has("texture-format:rgba8unorm-srgb"));
  assert.ok(tokens.has("texture-format:r8unorm"));
  for (const token of BC_FORMAT_TOKENS.concat(ASTC_FORMAT_TOKENS)) {
    assert.ok(!tokens.has(token), `${token} must not appear without device evidence`);
  }
  // WebGPU has no three-channel 8-bit format, so no evidence may unlock one.
  assert.ok(!tokens.has("texture-format:rgb8unorm"));
});

test("no WebGPU device means no evidence and therefore no tokens", async () => {
  const { window } = await loadProbe({ adapter: null });
  assert.deepEqual(window.__gosx_scene3d_texture_tokens(), [], "an unprobed device must not claim a format");
});

test("an unknown feature name is ignored rather than guessed at", async () => {
  const { adapter } = fakeAdapter(["texture-compression-xyz", "some-future-feature"]);
  const { window } = await loadProbe({ adapter });
  const tokens = tokenSet(window.__gosx_scene3d_texture_tokens());
  for (const token of BC_FORMAT_TOKENS) {
    assert.ok(!tokens.has(token));
  }
  assert.ok(tokens.has("container:ktx2"));
});

// --- the delivery budget -----------------------------------------------------

test("the budget token mirrors variantsel.BudgetFromHints", async () => {
  const high = await loadProbe({ adapter: fakeAdapter([]).adapter, deviceMemory: 8 });
  assert.ok(tokenSet(high.window.__gosx_scene3d_texture_tokens()).has("budget:high"));

  const low = await loadProbe({ adapter: fakeAdapter([]).adapter, deviceMemory: 1 });
  assert.ok(tokenSet(low.window.__gosx_scene3d_texture_tokens()).has("budget:low"));

  const standard = await loadProbe({ adapter: fakeAdapter([]).adapter, deviceMemory: 2 });
  assert.ok(tokenSet(standard.window.__gosx_scene3d_texture_tokens()).has("budget:standard"));
});

// --- WebGL2 ------------------------------------------------------------------

// fakeGL grants the named extensions and refuses everything else.
function fakeGL(names) {
  const granted = new Set(names);
  return { getExtension: (name) => (granted.has(name) ? {} : null) };
}

test("WebGL2 splits the BC family across its extensions", async () => {
  const { window } = await loadProbe({ adapter: fakeAdapter([]).adapter });
  const tokensFor = (names) => tokenSet(window.__gosx_scene3d_texture_tokens("webgl", fakeGL(names)));

  const s3tcOnly = tokensFor(["WEBGL_compressed_texture_s3tc", "WEBGL_compressed_texture_s3tc_srgb"]);
  for (const token of [
    "texture-format:bc1-rgba-unorm", "texture-format:bc1-rgba-unorm-srgb",
    "texture-format:bc3-rgba-unorm", "texture-format:bc3-rgba-unorm-srgb",
  ]) {
    assert.ok(s3tcOnly.has(token), `S3TC did not unlock ${token}`);
  }
  for (const token of [
    "texture-format:bc4-r-unorm", "texture-format:bc5-rg-unorm",
    "texture-format:bc7-rgba-unorm", "texture-format:bc7-rgba-unorm-srgb",
  ]) {
    assert.ok(!s3tcOnly.has(token), `S3TC unlocked ${token}, which needs a different extension`);
  }
  assert.ok(
    !s3tcOnly.has("device-feature:texture-compression-bc"),
    "a partial BC family must not claim the whole-family feature token"
  );

  const rgtc = tokensFor(["EXT_texture_compression_rgtc"]);
  assert.ok(rgtc.has("texture-format:bc4-r-unorm") && rgtc.has("texture-format:bc5-rg-unorm"));
  assert.ok(!rgtc.has("texture-format:bc7-rgba-unorm"), "RGTC unlocked BC7, which needs BPTC");

  const bptc = tokensFor(["EXT_texture_compression_bptc"]);
  assert.ok(bptc.has("texture-format:bc7-rgba-unorm") && bptc.has("texture-format:bc7-rgba-unorm-srgb"));
  assert.ok(!bptc.has("texture-format:bc1-rgba-unorm"), "BPTC unlocked BC1, which needs S3TC");

  const full = tokensFor([
    "WEBGL_compressed_texture_s3tc", "WEBGL_compressed_texture_s3tc_srgb",
    "EXT_texture_compression_rgtc", "EXT_texture_compression_bptc",
  ]);
  assert.ok(
    full.has("device-feature:texture-compression-bc"),
    "a complete BC family must claim the whole-family feature token"
  );
  // WebGL2 does have three-channel formats, so the set keeps rgb8unorm.
  assert.ok(full.has("texture-format:rgb8unorm"), "a WebGL2 set lost rgb8unorm, which WebGL2 uploads");
  assert.ok(full.has("backend:webgl"));
});

test("Canvas2D gets no texture format at all", async () => {
  const { window } = await loadProbe({ adapter: fakeAdapter([]).adapter });
  const tokens = tokenSet(window.__gosx_scene3d_texture_tokens("canvas2d", null));
  assert.ok(tokens.has("backend:canvas2d"), "a Canvas2D set must still name its backend");
  for (const token of BC_FORMAT_TOKENS.concat(["texture-format:rgba8unorm", "container:ktx2"])) {
    assert.ok(!tokens.has(token), `a Canvas2D set carries ${token} and uploads no GPU texture`);
  }
});

test("an unknown backend name yields nothing", async () => {
  const { window } = await loadProbe({ adapter: fakeAdapter([]).adapter });
  assert.deepEqual(window.__gosx_scene3d_texture_tokens("vulkan", null), []);
});
