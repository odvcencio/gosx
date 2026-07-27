// Render-bundle tests for the WebGPU renderer (16a-scene-webgpu.js).
//
// A GPURenderBundle records a draw set once and replays it with one
// executeBundles call. The saving is real; the hazard is real too. A bundle
// holds its pipelines, bind groups and buffers BY REFERENCE, so replaying a
// bundle whose draw set moved on renders the wrong image and reports no error.
//
// The renderer does not guess at the determinants. It records the command stream
// the frame WOULD encode and compares it against the stream the cached bundle
// was built from. These tests pin that machinery: the token stream, the identity
// stamping, the layout key, the eligibility gate and the invalidation.
//
// What runs here and what does not:
//   - Every helper below runs for real, in a node:vm context that loads the
//     shipped source the way the bundle concatenates it.
//   - No WebGPU adapter exists in a headless environment, so no test here reads
//     a rendered pixel. The renderer-level integration (encode once, replay
//     after, re-encode on change) is executed against a recording fake device in
//     runtime.test.js.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");

function readSource(name) {
  return fs.readFileSync(path.join(srcDir, name), "utf8");
}

const webgpuSource = readSource("16a-scene-webgpu.js");
const computeSource = readSource("16b-scene-compute.js");
const computeBridgeSource = readSource("26e1-feature-scene3d-webgpu-compute-bridge.js");

test("pipeline validation helpers cross the gated WebGPU chunk boundary", () => {
  for (const helper of ["sceneReportPipelineFailure", "sceneShaderModuleError"]) {
    assert.match(
      computeSource,
      new RegExp(`Object\\.assign\\([\\s\\S]*\\b${helper}\\b`),
      `${helper} must be published by the base Scene3D chunk`,
    );
    assert.match(
      computeBridgeSource,
      new RegExp(
        `(?:var ${helper} = sceneApi\\.${helper};|function ${helper}\\([\\s\\S]*api\\.${helper})`,
      ),
      `${helper} must be imported by the WebGPU feature chunk`,
    );
  }
});

function createContext() {
  const sandbox = {
    console: { warn() {}, log() {}, error() {} },
    Math,
    JSON,
    Number,
    Object,
    Array,
    String,
    Boolean,
    Float32Array,
    Uint32Array,
    Uint8Array,
    Int32Array,
    ArrayBuffer,
    DataView,
    Promise,
    Error,
    Map,
    Set,
    parseInt,
    parseFloat,
    isFinite,
    performance: { now: () => 0 },
    GPUBufferUsage: { UNIFORM: 1, COPY_DST: 2, MAP_READ: 4, VERTEX: 8, STORAGE: 16, COPY_SRC: 32, INDIRECT: 64 },
    GPUTextureUsage: { RENDER_ATTACHMENT: 1, COPY_SRC: 2, TEXTURE_BINDING: 4, COPY_DST: 8, STORAGE_BINDING: 16 },
    GPUShaderStage: { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 },
    GPUMapMode: { READ: 1, WRITE: 2 },
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  sandbox.__gosx_scene3d_api = {};
  sandbox.setTimeout = (fn) => { fn(); return 0; };
  sandbox.clearTimeout = () => {};

  const context = vm.createContext(sandbox);
  const prelude = `
    function sceneNumber(value, fallback) {
      var n = Number(value);
      return Number.isFinite(n) ? n : fallback;
    }
    function sceneBool(value, fallback) { return value == null ? fallback : !!value; }
    function clamp01(v) { return Math.max(0, Math.min(1, Number(v) || 0)); }
    function normalizeSceneKind(value) {
      return typeof value === "string" ? value.trim().toLowerCase() : "box";
    }
    function normalizeSceneCameraKind(value, fallback) { return fallback; }
    function sceneRenderCamera(camera) { return camera; }
    function sceneOrthographicBounds() { return { left: -3, right: 3, top: 3, bottom: -3 }; }
    function queueInputSignal() {}
    function sceneProjectPoint() { return null; }
  `;
  vm.runInContext(prelude, context, { filename: "prelude.js" });
  vm.runInContext(readSource("11-scene-math.js"), context, { filename: "11-scene-math.js" });
  vm.runInContext(readSource("17-scene-input.js"), context, { filename: "17-scene-input.js" });
  vm.runInContext(webgpuSource, context, { filename: "16a-scene-webgpu.js" });
  return { context, sandbox };
}

function api() {
  return createContext().sandbox.__gosx_scene3d_api;
}

// --- Identity stamping ------------------------------------------------------

test("the recorder gives each GPU object a stable identity", () => {
  const scene = api();
  const counter = { next: 1 };
  const a = { label: "pipeline-a" };
  const b = { label: "pipeline-b" };
  const first = scene.sceneWebGPUBundleObjectID(a, counter);
  assert.equal(scene.sceneWebGPUBundleObjectID(a, counter), first, "the same object keeps its id");
  assert.notEqual(scene.sceneWebGPUBundleObjectID(b, counter), first, "a different object gets a different id");
});

test("a null or primitive argument records as id 0 and never collides", () => {
  const scene = api();
  const counter = { next: 1 };
  assert.equal(scene.sceneWebGPUBundleObjectID(null, counter), 0);
  assert.equal(scene.sceneWebGPUBundleObjectID(undefined, counter), 0);
  assert.equal(scene.sceneWebGPUBundleObjectID(7, counter), 0);
  // A real object never receives id 0, so a stamped object can never be
  // mistaken for a missing one.
  assert.ok(scene.sceneWebGPUBundleObjectID({}, counter) > 0);
});

test("the identity stamp does not show up in enumeration or JSON", () => {
  const scene = api();
  const counter = { next: 1 };
  const buffer = { size: 64 };
  scene.sceneWebGPUBundleObjectID(buffer, counter);
  assert.deepEqual(Object.keys(buffer), ["size"], "the stamp must be non-enumerable");
  assert.equal(JSON.stringify(buffer), '{"size":64}');
});

test("a frozen object refuses the stamp and makes the frame ineligible", () => {
  const scene = api();
  const recorder = scene.createSceneWebGPUDrawRecorder();
  recorder.setPipeline(Object.freeze({ label: "frozen" }));
  recorder.draw(3);
  assert.equal(recorder.unsupportedCount(), 1, "an unstampable object must be reported");
});

// --- The recorder -----------------------------------------------------------

function recordCalls(recorder, pipeline, group, buffer) {
  recorder.setPipeline(pipeline);
  recorder.setBindGroup(0, group);
  recorder.setVertexBuffer(0, buffer, 0, 48);
  recorder.draw(36, 1, 0, 0);
}

test("the recorder counts draws and issues no WebGPU calls", () => {
  const scene = api();
  const recorder = scene.createSceneWebGPUDrawRecorder();
  const pipeline = { label: "pbr" };
  const group = { label: "frame" };
  const buffer = { label: "positions" };
  recordCalls(recorder, pipeline, group, buffer);
  recorder.drawIndirect({ label: "args" }, 0);
  assert.equal(recorder.drawCount(), 2, "draw and drawIndirect both count");
  assert.equal(recorder.unsupportedCount(), 0);
  assert.ok(recorder.length() > 0);
});

test("reset clears the stream so the next frame starts clean", () => {
  const scene = api();
  const recorder = scene.createSceneWebGPUDrawRecorder();
  recordCalls(recorder, { a: 1 }, { b: 2 }, { c: 3 });
  const length = recorder.length();
  recorder.reset();
  assert.equal(recorder.length(), 0);
  assert.equal(recorder.drawCount(), 0);
  assert.ok(length > 0);
});

// --- The bundle cache -------------------------------------------------------
//
// The cache is the whole correctness argument. Each test below changes exactly
// one determinant and asserts the cache refuses to replay.

function cacheHarness() {
  const scene = api();
  const cache = scene.createSceneWebGPUBundleCache();
  const objects = {
    pipeline: { label: "pbr-opaque" },
    altPipeline: { label: "pbr-alpha" },
    frame: { label: "frame-bind-group" },
    altFrame: { label: "frame-bind-group-2" },
    material: { label: "material-bind-group" },
    positions: { label: "positions" },
    altPositions: { label: "positions-grown" },
    args: { label: "indirect-args" },
  };
  return { scene, cache, objects };
}

// encode builds the stream a frame would produce. The `state` object lets a test
// vary one determinant at a time.
function encodeFrame(state) {
  return function(target) {
    target.setPipeline(state.pipeline);
    target.setBindGroup(0, state.frame);
    target.setBindGroup(1, state.material);
    target.setVertexBuffer(0, state.positions, state.offset, state.size);
    target.draw(state.vertexCount, state.instanceCount, 0, 0);
  };
}

function baseState(objects) {
  return {
    pipeline: objects.pipeline,
    frame: objects.frame,
    material: objects.material,
    positions: objects.positions,
    offset: 0,
    size: 432,
    vertexCount: 36,
    instanceCount: 1,
  };
}

const LAYOUT = "bgra8unorm|depth24plus|1";

function planTwice(cache, layout, state, mutate) {
  const first = cache.plan(layout, encodeFrame(state));
  assert.equal(first.eligible, true);
  assert.equal(first.reusable, false, "the first frame has nothing to replay");
  cache.adopt(layout, { __kind: "renderBundle" });
  if (mutate) mutate(state);
  return cache.plan(layout, encodeFrame(state));
}

test("an unchanged frame replays the cached bundle", () => {
  const { cache, objects } = cacheHarness();
  const verdict = planTwice(cache, LAYOUT, baseState(objects), null);
  assert.equal(verdict.reusable, true);
  assert.equal(verdict.reason, "replay");
});

test("a changed pipeline forces a re-encode", () => {
  const { cache, objects } = cacheHarness();
  const verdict = planTwice(cache, LAYOUT, baseState(objects), (state) => {
    state.pipeline = objects.altPipeline;
  });
  assert.equal(verdict.reusable, false);
  assert.equal(verdict.reason, "re-encode");
});

// This is the case that motivated memoizing the frame bind group. The renderer
// used to build a new one every frame, so no bundle could ever replay.
test("a rebuilt bind group forces a re-encode", () => {
  const { cache, objects } = cacheHarness();
  const verdict = planTwice(cache, LAYOUT, baseState(objects), (state) => {
    state.frame = objects.altFrame;
  });
  assert.equal(verdict.reusable, false);
});

// A buffer whose CONTENTS change is fine — writeBuffer reaches the replayed
// draw. A buffer that is REPLACED is not, because the bundle holds the old one.
test("a reallocated vertex buffer forces a re-encode", () => {
  const { cache, objects } = cacheHarness();
  const verdict = planTwice(cache, LAYOUT, baseState(objects), (state) => {
    state.positions = objects.altPositions;
  });
  assert.equal(verdict.reusable, false);
});

test("a changed vertex-buffer byte offset forces a re-encode", () => {
  const { cache, objects } = cacheHarness();
  const verdict = planTwice(cache, LAYOUT, baseState(objects), (state) => {
    state.offset = 144;
  });
  assert.equal(verdict.reusable, false);
});

test("a changed vertex-buffer byte size forces a re-encode", () => {
  const { cache, objects } = cacheHarness();
  const verdict = planTwice(cache, LAYOUT, baseState(objects), (state) => {
    state.size = 216;
  });
  assert.equal(verdict.reusable, false);
});

test("a changed vertex count forces a re-encode", () => {
  const { cache, objects } = cacheHarness();
  const verdict = planTwice(cache, LAYOUT, baseState(objects), (state) => {
    state.vertexCount = 24;
  });
  assert.equal(verdict.reusable, false);
});

test("a changed instance count forces a re-encode", () => {
  const { cache, objects } = cacheHarness();
  const verdict = planTwice(cache, LAYOUT, baseState(objects), (state) => {
    state.instanceCount = 2;
  });
  assert.equal(verdict.reusable, false);
});

test("an extra draw forces a re-encode even when every earlier token matches", () => {
  const { cache, objects } = cacheHarness();
  const state = baseState(objects);
  cache.plan(LAYOUT, encodeFrame(state));
  cache.adopt(LAYOUT, { __kind: "renderBundle" });
  const verdict = cache.plan(LAYOUT, (target) => {
    encodeFrame(state)(target);
    target.draw(6, 1, 0, 0);
  });
  assert.equal(verdict.reusable, false, "a longer stream must never match a shorter one");
});

test("a removed draw forces a re-encode", () => {
  const { cache, objects } = cacheHarness();
  const state = baseState(objects);
  cache.plan(LAYOUT, (target) => {
    encodeFrame(state)(target);
    target.draw(6, 1, 0, 0);
  });
  cache.adopt(LAYOUT, { __kind: "renderBundle" });
  const verdict = cache.plan(LAYOUT, encodeFrame(state));
  assert.equal(verdict.reusable, false, "a shorter stream must never match a longer one");
});

// The layout key covers what a bundle encoder bakes in and the command stream
// cannot see: the colour format, the depth format and the sample count.
test("a changed render-target layout forces a re-encode", () => {
  const { cache, objects } = cacheHarness();
  const state = baseState(objects);
  cache.plan(LAYOUT, encodeFrame(state));
  cache.adopt(LAYOUT, { __kind: "renderBundle" });
  const verdict = cache.plan("bgra8unorm|depth24plus|4", encodeFrame(state));
  assert.equal(verdict.reusable, false, "an MSAA change invalidates the bundle");
});

test("the layout key names the colour format, the depth format and the sample count", () => {
  const scene = api();
  assert.equal(scene.sceneWebGPUBundleLayoutKey("bgra8unorm", "depth24plus", 4), "bgra8unorm|depth24plus|4");
  assert.notEqual(
    scene.sceneWebGPUBundleLayoutKey("rgba8unorm", "depth24plus", 1),
    scene.sceneWebGPUBundleLayoutKey("bgra8unorm", "depth24plus", 1),
  );
});

test("a frame with no draws is ineligible rather than replayed", () => {
  const { cache } = cacheHarness();
  const verdict = cache.plan(LAYOUT, () => {});
  assert.equal(verdict.eligible, false);
  assert.equal(verdict.reason, "no-draws");
  assert.equal(verdict.reusable, false);
});

test("invalidate drops the cached bundle", () => {
  const { cache, objects } = cacheHarness();
  const state = baseState(objects);
  cache.plan(LAYOUT, encodeFrame(state));
  cache.adopt(LAYOUT, { __kind: "renderBundle" });
  cache.invalidate();
  assert.equal(cache.bundle(), null);
  assert.equal(cache.plan(LAYOUT, encodeFrame(state)).reusable, false);
});

test("the cache counts encodes and replays for the diagnostics", () => {
  const { cache, objects } = cacheHarness();
  const state = baseState(objects);
  cache.plan(LAYOUT, encodeFrame(state));
  cache.adopt(LAYOUT, { __kind: "renderBundle" });
  for (let frame = 0; frame < 3; frame += 1) {
    assert.equal(cache.plan(LAYOUT, encodeFrame(state)).reusable, true);
    cache.markReplayed();
  }
  const stats = cache.stats();
  assert.equal(stats.encodes, 1, "one encode for four frames");
  assert.equal(stats.replays, 3);
  assert.equal(stats.draws, 1);
});

// An indirect draw is the strongest bundling case: the GPU owns the instance
// count, so the command stream never changes while the scene animates.
test("an indirect draw replays while its argument buffer contents change", () => {
  const { cache } = cacheHarness();
  const args = { label: "indirect-args" };
  const encode = (target) => {
    target.setPipeline({ label: "instanced" });
    target.setVertexBuffer(0, args, 0, 16);
    target.drawIndirect(args, 0);
  };
  // Reuse the SAME objects across frames, which is what the renderer's caches
  // guarantee for an instanced mesh under GPU cull.
  const pipeline = { label: "instanced" };
  const buffer = { label: "survivors" };
  const stable = (target) => {
    target.setPipeline(pipeline);
    target.setVertexBuffer(0, buffer, 0, 16);
    target.drawIndirect(args, 0);
  };
  void encode;
  cache.plan(LAYOUT, stable);
  cache.adopt(LAYOUT, { __kind: "renderBundle" });
  assert.equal(cache.plan(LAYOUT, stable).reusable, true);
  // A changed indirect OFFSET is a different draw and must invalidate.
  const shifted = (target) => {
    target.setPipeline(pipeline);
    target.setVertexBuffer(0, buffer, 0, 16);
    target.drawIndirect(args, 16);
  };
  assert.equal(cache.plan(LAYOUT, shifted).reusable, false);
});

// --- Eligibility ------------------------------------------------------------

const ELIGIBLE = {
  disabled: false,
  hasWater: false,
  hasPoints: false,
  hasLabels: false,
  hasScreenLines: false,
  hasSurfaces: false,
  hasWorldLines: false,
  hasDynamicMeshes: false,
  hasBundleableDraws: true,
};

test("a mesh-only frame qualifies for bundling", () => {
  const scene = api();
  assert.equal(scene.sceneWebGPUBundleIneligibleReason(ELIGIBLE), "");
});

test("every excluded draw path names itself", () => {
  const scene = api();
  const cases = [
    ["disabled", "disabled"],
    ["hasWater", "water"],
    ["hasPoints", "points"],
    ["hasLabels", "labels"],
    ["hasScreenLines", "screen-lines"],
    ["hasSurfaces", "surfaces"],
    ["hasWorldLines", "world-lines"],
    ["hasDynamicMeshes", "dynamic-meshes"],
  ];
  for (const [flag, reason] of cases) {
    const flags = { ...ELIGIBLE };
    flags[flag] = true;
    assert.equal(
      scene.sceneWebGPUBundleIneligibleReason(flags),
      reason,
      `${flag} must report "${reason}" so a stale image can be diagnosed`,
    );
  }
});

test("a frame with nothing to draw reports nothing-to-bundle", () => {
  const scene = api();
  assert.equal(
    scene.sceneWebGPUBundleIneligibleReason({ ...ELIGIBLE, hasBundleableDraws: false }),
    "nothing-to-bundle",
  );
});

test("the dynamic-mesh scan covers all three passes", () => {
  const scene = api();
  const isDynamic = (obj) => Boolean(obj && obj.dynamic);
  for (const pass of ["opaque", "alpha", "additive"]) {
    const drawList = { opaque: [], alpha: [], additive: [] };
    drawList[pass] = [{}, { dynamic: true }];
    assert.equal(
      scene.sceneWebGPUDrawListHasDynamicMesh(drawList, isDynamic),
      true,
      `a dynamic object in the ${pass} pass must be found`,
    );
  }
  assert.equal(
    scene.sceneWebGPUDrawListHasDynamicMesh({ opaque: [{}], alpha: [{}], additive: [{}] }, isDynamic),
    false,
  );
  assert.equal(scene.sceneWebGPUDrawListHasDynamicMesh(null, isDynamic), false);
});

// --- Renderer wiring, verified against the shipped source -------------------
//
// The checks below read the renderer text. They cannot prove an image; they
// prove the wiring is the wiring these unit tests reason about. The executing
// end-to-end test lives in runtime.test.js.

test("the bundle encoder is created with the same formats the main pass uses", () => {
  const start = webgpuSource.indexOf("device.createRenderBundleEncoder({");
  assert.notEqual(start, -1, "the renderer must create a render bundle encoder");
  const body = webgpuSource.slice(start, start + 400);
  assert.match(body, /colorFormats: \[targetFormat\]/);
  assert.match(body, /depthStencilFormat: "depth24plus"/);
  assert.match(body, /sampleCount: sampleCount/);
  // The PBR pipelines are keyed on exactly these three values, so a bundle
  // built from them is compatible with the pass by construction.
  assert.match(
    webgpuSource,
    /wgpuPipelineKey\("pbr", blendMode, depthWrite, targetFormat, "depth24plus", activeSampleCount\)/,
  );
});

test("the direct draw block is skipped only when a bundle carried the frame", () => {
  assert.match(
    webgpuSource,
    /if \(frameStats\.bundleState === "direct" && \(hasPBRData \|\| hasInstancedData \|\| hasWorldLines \|\| hasSurfaces\)\)/,
    "an ineligible or re-encoded frame must still reach the direct path exactly once",
  );
});

test("the recorder and the real encoder run the same encode function", () => {
  const start = webgpuSource.indexOf("function encodeBundleableSceneDraws(");
  assert.notEqual(start, -1);
  // One function drives both targets. That is what removes the blind spots: the
  // recorder cannot miss a command the encoder issues.
  assert.match(webgpuSource, /webGPUBundleCache\.plan\(bundleLayoutKey, function\(recorder\) \{\s*\n\s*encodeBundleableSceneDraws\(recorder, bundleContext\);/);
  assert.match(webgpuSource, /encodeBundleableSceneDraws\(bundleEncoder, bundleContext\);/);
});

test("the frame bind group is memoized, or no bundle could ever replay", () => {
  const start = webgpuSource.indexOf("function createFrameBindGroup(");
  assert.notEqual(start, -1);
  const body = webgpuSource.slice(start, webgpuSource.indexOf("function _createFrameBindGroupUncached(", start));
  for (const field of ["frame", "lights", "fog", "env", "view0", "view1", "sampler", "shadow"]) {
    assert.match(body, new RegExp(`cache\\.${field} ===`), `the cache must compare ${field} by identity`);
  }
  assert.match(body, /cache\.device === device/, "a device-loss recovery must rebuild the bind group");
});

test("the page can force direct encoding", () => {
  assert.match(
    webgpuSource,
    /window\.__gosx_scene3d_webgpu_render_bundles !== false/,
    "an escape hatch must exist for a suspected stale replay",
  );
});

test("the bundle state reaches the diagnostics attributes", () => {
  for (const name of [
    "data-gosx-scene3d-webgpu-bundle-state",
    "data-gosx-scene3d-webgpu-bundle-reason",
    "data-gosx-scene3d-webgpu-bundle-encodes",
    "data-gosx-scene3d-webgpu-bundle-replays",
    "data-gosx-scene3d-webgpu-bundle-draws",
  ]) {
    assert.ok(webgpuSource.includes(name), `${name} must be published`);
  }
});
