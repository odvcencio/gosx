// Renderer-side tests for the KTX2 upload gate.
//
// 19a-scene-ktx2.test.mjs proves the reader agrees with the Khronos reference
// files. This file proves the two renderers use it, and — the part that
// matters — that every failure is visible.
//
// A blank texture that reports success is the defect this file guards. The
// 1x1 white placeholder both renderers bind first looks like a working texture
// on screen, so a failure that leaves `loaded` true and `failed` false hides
// itself. Each test below drives one real failure and asserts the record ends
// failed, never loaded.
//
// The tests load the BUILT chunks, not the sources, because the gate global is
// what a browser really sees. Both renderers publish their uploader on
// window.__gosx_scene3d_ktx2_texture_loader with the same (context, url,
// record) signature, and sceneKTX2UploadPathReady in 19a-scene-ktx2.js reads
// that global before 19-scene-gltf.js swaps an image URI for a block variant.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const webglChunk = fs.readFileSync(path.join(__dirname, "bootstrap-feature-scene3d-webgl.js"), "utf8");
const webgpuChunk = fs.readFileSync(path.join(__dirname, "bootstrap-feature-scene3d-webgpu.js"), "utf8");
const gltfChunk = fs.readFileSync(path.join(__dirname, "bootstrap-feature-scene3d-gltf.js"), "utf8");
const ktx2Source = fs.readFileSync(path.join(__dirname, "bootstrap-src", "19a-scene-ktx2.js"), "utf8");

async function settle(turns = 8) {
  for (let i = 0; i < turns; i += 1) {
    await Promise.resolve();
  }
}

// makeWindow builds the smallest window a renderer chunk needs to reach its
// tail. Both chunks bail out early without window.__gosx_scene3d_api, and the
// tail is where the gate global is assigned.
function makeWindow() {
  const warnings = [];
  const win = {
    __gosx_scene3d_api: {},
    __gosx_scene3d_webgpu_probe() {
      return { adapter: false, device: null, ready: true };
    },
  };
  win.window = win;
  win.globalThis = win;
  win.console = {
    warn(...args) { warnings.push(args.join(" ")); },
    error() {},
    log() {},
  };
  win.navigator = {};
  win.document = { querySelector: () => null, createElement: () => ({ style: {}, dataset: {} }), head: null };
  win.warnings = warnings;
  return win;
}

function loadGateGlobal(chunkSource) {
  const win = makeWindow();
  const context = vm.createContext(win);
  vm.runInContext(chunkSource, context, { filename: "chunk.js" });
  return win;
}

// A fake KTX2 API that fails the way the real reader fails. The reader raises a
// named Error for each of these, so the renderer path is the only thing under
// test here.
function failingKTX2(code, message) {
  return {
    load() {
      const error = new Error("ktx2: " + message);
      error.code = code;
      return Promise.reject(error);
    },
    uploadWebGPU() { throw new Error("uploadWebGPU must not run after a failed load"); },
    uploadWebGL2() { throw new Error("uploadWebGL2 must not run after a failed load"); },
  };
}

test("both renderer chunks open the KTX2 upload gate", () => {
  for (const [name, source] of [["webgl", webglChunk], ["webgpu", webgpuChunk]]) {
    const win = loadGateGlobal(source);
    assert.equal(typeof win.__gosx_scene3d_ktx2_texture_loader, "function",
      `the ${name} chunk must publish an uploader, or 19-scene-gltf.js keeps serving the PNG variant`);
  }
});

test("the KTX2 reader ships in the glTF chunk only", () => {
  assert.match(gltfChunk, /__gosx_scene3d_ktx2/,
    "the glTF chunk must carry the reader: only 19-scene-gltf.js reads it lexically");
  for (const [name, source] of [["webgl", webglChunk], ["webgpu", webgpuChunk]]) {
    assert.doesNotMatch(source, /__gosx_scene3d_ktx2\s*=\s*\{/,
      `the ${name} chunk must not carry a second copy of the reader; a page that loads both would download it twice`);
  }
});

// The gate must stay shut when the reader chunk never lands. A .ktx2 URI is
// only ever produced by the glTF variant swap, and that swap is what the gate
// controls, so a renderer alone must resolve the reader as absent.
test("a renderer without the glTF chunk resolves no reader", () => {
  const win = loadGateGlobal(webgpuChunk);
  assert.equal(win.__gosx_scene3d_ktx2, undefined,
    "the renderer chunk must not define the reader; it belongs to the glTF chunk");
});

test("a KTX2 fetch that 404s marks the record failed and never loaded", async () => {
  for (const [name, source] of [["webgl", webglChunk], ["webgpu", webgpuChunk]]) {
    const win = loadGateGlobal(source);
    win.__gosx_scene3d_ktx2 = failingKTX2("fetch", "GET /t.ktx2 gave HTTP 404");
    const record = { loaded: false, failed: false, pending: true, texture: {} };
    await win.__gosx_scene3d_ktx2_texture_loader({}, "/t.ktx2", record);
    await settle();
    assert.equal(record.failed, true, `${name}: a 404 must set failed`);
    assert.equal(record.loaded, false, `${name}: a 404 must never set loaded`);
    assert.match(String(record.error), /404/, `${name}: the record must carry the reason`);
    assert.ok(win.warnings.some((line) => line.includes("/t.ktx2")),
      `${name}: a failed texture must warn with its URL`);
  }
});

test("a format the device cannot sample marks the record failed and never loaded", async () => {
  for (const [name, source] of [["webgl", webglChunk], ["webgpu", webgpuChunk]]) {
    const win = loadGateGlobal(source);
    win.__gosx_scene3d_ktx2 = failingKTX2("format", "vkFormat 23 has no block upload path");
    const record = { loaded: false, failed: false, pending: true, texture: {} };
    await win.__gosx_scene3d_ktx2_texture_loader({}, "/t.ktx2", record);
    await settle();
    assert.equal(record.failed, true, `${name}: an unsupported format must set failed`);
    assert.equal(record.loaded, false, `${name}: an unsupported format must never set loaded`);
  }
});

test("a level whose byte length disagrees with its block arithmetic fails loudly", async () => {
  for (const [name, source] of [["webgl", webglChunk], ["webgpu", webgpuChunk]]) {
    const win = loadGateGlobal(source);
    win.__gosx_scene3d_ktx2 = failingKTX2("level-size", "level 0 holds 8 bytes, want 16");
    const record = { loaded: false, failed: false, pending: true, texture: {} };
    await win.__gosx_scene3d_ktx2_texture_loader({}, "/t.ktx2", record);
    await settle();
    assert.equal(record.failed, true, `${name}: a short level must set failed`);
    assert.equal(record.loaded, false, `${name}: a short level must never set loaded`);
    assert.match(String(record.error), /want 16/, `${name}: the record must carry the block arithmetic`);
  }
});

test("an upload that throws leaves the placeholder bound and the record failed", async () => {
  // The WebGL2 uploader raises when the context lacks the compressed-format
  // extension. Prove the renderer does not mark the record loaded on that path:
  // the upload runs AFTER the fetch succeeds, so this is the one failure that
  // could still slip through a fetch-only guard.
  const win = loadGateGlobal(webglChunk);
  win.__gosx_scene3d_ktx2 = {
    load() {
      return Promise.resolve({ width: 4, height: 4, levels: [{ bytes: new Uint8Array(8) }] });
    },
    uploadWebGL2() {
      const error = new Error("ktx2: this context has no EXT_texture_compression_bptc");
      error.code = "extension";
      throw error;
    },
  };
  const record = { loaded: false, failed: false, texture: {} };
  await win.__gosx_scene3d_ktx2_texture_loader({}, "/t.ktx2", record);
  await settle();
  assert.equal(record.failed, true, "a throwing upload must set failed");
  assert.equal(record.loaded, false, "a throwing upload must never set loaded");
  assert.equal(record.ktx2, undefined, "a throwing upload must not claim the block path");
});

test("a successful upload swaps the texture and clears pending", async () => {
  const win = loadGateGlobal(webgpuChunk);
  const uploaded = { createView: () => ({ __kind: "view" }) };
  win.__gosx_scene3d_ktx2 = {
    load() {
      return Promise.resolve({ width: 8, height: 8, levels: [{ bytes: new Uint8Array(16) }] });
    },
    uploadWebGPU() { return uploaded; },
  };
  let destroyed = false;
  const record = {
    loaded: false,
    failed: false,
    pending: true,
    texture: { destroy() { destroyed = true; } },
  };
  await win.__gosx_scene3d_ktx2_texture_loader({}, "/t.ktx2", record);
  await settle();
  assert.equal(record.failed, false, "a good container must not set failed");
  assert.equal(record.loaded, true, "a good container must set loaded");
  assert.equal(record.pending, false, "a good container must clear pending");
  assert.equal(record.ktx2, true, "a good container must record the block path");
  assert.equal(record.texture, uploaded, "the block texture must replace the placeholder");
  assert.equal(destroyed, true, "the placeholder texture must be released");
});

test("the gate global is what sceneKTX2UploadPathReady reads", () => {
  // Pin the contract between the reader and the renderers. The reader tests
  // the global for null; the renderers assign it. Break either side and the
  // glTF variant swap silently keeps serving the PNG, with no test to see it.
  const win = { window: null };
  win.window = win;
  const factory = new Function("window", ktx2Source + "\nreturn window.__gosx_scene3d_ktx2;");
  const api = factory(win);
  assert.equal(api.uploadPathReady(), false, "the gate starts shut");
  win.__gosx_scene3d_ktx2_texture_loader = function () {};
  assert.equal(api.uploadPathReady(), true, "assigning the global opens the gate");
});
