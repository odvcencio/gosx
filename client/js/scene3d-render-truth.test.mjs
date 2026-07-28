// Render-truth telemetry coverage.
//
// The bug class this guards against: every Scene3D counter that existed before
// this module answered "what did the author ask for" or "what did the planner
// put in the bundle". None answered "what reached the framebuffer". A Selena
// customPost pass was tuned across three sessions while drawing zero pixels,
// and three mesh planes drew nothing for two weeks, because every attribute a
// person could read reported the first question and none reported the second.
//
// These tests assert the DISTINCTION survives: an effect that is present but
// never dispatched must be distinguishable from one that drew.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");

function readSrc(name) {
  return fs.readFileSync(path.join(srcDir, name), "utf8");
}

const sharedSource = readSrc("15a-scene-postfx-shared.js");
const webglSource = readSrc("16-scene-webgl.js");
const webgpuSource = readSrc("16a-scene-webgpu.js");
// sceneRenderBackendTruth and its DOM surface live in
// 20b-scene-mount-webgl-chunk.js, not 20-scene-mount.js: applySceneRendererState
// (its only caller) moved there when the former single 20-scene-mount.js split
// into the 20a..20h file set.
const mountSource = readSrc("20b-scene-mount-webgl-chunk.js");

// loadRenderTruth evaluates 15a in a throwaway VM with a minimal window and
// returns the published API. No DOM library needed: the only DOM surface the
// module touches is setAttribute, which the fake mount below implements.
function loadRenderTruth(windowOverrides = {}) {
  const windowStub = Object.assign({
    __gosx_scene3d_render_truth: true,
  }, windowOverrides);
  const context = vm.createContext({
    window: windowStub,
    navigator: windowOverrides.navigator || { userAgent: "test" },
    performance: { now: () => 1000 },
    console,
    WeakMap,
  });
  vm.runInContext(sharedSource, context);
  return { api: windowStub.__gosx_scene3d_render_truth_api, windowStub };
}

function fakeMount() {
  const attrs = new Map();
  return {
    attrs,
    setAttribute(name, value) { attrs.set(name, String(value)); },
    getAttribute(name) { return attrs.has(name) ? attrs.get(name) : null; },
  };
}

test("render truth: an authored effect that never dispatches reads DEAD, not healthy", () => {
  const { api } = loadRenderTruth();
  // The exact shape of the liquid-glass incident: a two-effect chain where
  // bloom draws and the trailing customPost never does.
  const chain = api.chain([
    { kind: "bloom" },
    { kind: "customPost", name: "liquidGlass" },
  ]);
  api.mark(chain, 0, api.PIPELINE_OK, 4);          // bloom issues four passes
  api.mark(chain, 1, api.PIPELINE_PENDING, 0);     // customPost never compiled

  const counts = api.chainCounts(chain);
  assert.equal(counts.authored, 2);
  assert.equal(counts.dispatched, 1);
  assert.equal(counts.dead, 1, "a present-but-undispatched effect must count as dead");
  assert.equal(counts.pending, 1);

  const encoded = api.encodeChain(chain);
  assert.equal(encoded, "0:bloom:ok:4|1:customPost@liquidGlass:pending:0");
  // The old surface, for contrast: postEffects would read 2 in BOTH the
  // healthy and the dead case, which is why it never raised an alarm.
});

test("render truth: pipeline state separates 'never compiled' from 'compiler rejected it'", () => {
  const { api } = loadRenderTruth();
  const chain = api.chain([{ kind: "customPost", name: "a" }, { kind: "customPost", name: "b" }]);
  api.mark(chain, 0, api.PIPELINE_FAILED, 0);
  api.mark(chain, 1, api.PIPELINE_MISSING, 0);
  const counts = api.chainCounts(chain);
  assert.equal(counts.failed, 1, "a shader the browser rejected must be reported as failed");
  assert.equal(counts.dead, 2);
  // failed vs missing is the difference between "this browser's WGSL compiler
  // disagreed" and "no shader was ever supplied" -- opposite investigations.
});

test("render truth: effect names cannot forge extra chain entries", () => {
  const { api } = loadRenderTruth();
  const chain = api.chain([{ kind: "customPost", name: "evil|9:fake:ok:1" }]);
  const encoded = api.encodeChain(chain);
  assert.equal(encoded.split("|").length, 1, "separators in a name must not split the record");
});

test("render truth: publish stamps a backend-neutral attribute surface", () => {
  const { api } = loadRenderTruth();
  const mount = fakeMount();
  const chain = api.chain([{ kind: "customPost", name: "glass" }]);
  api.mark(chain, 0, api.PIPELINE_PENDING, 0);
  api.publish(mount, {
    backend: "webgl",
    postChain: chain,
    meshSubmitted: 3,
    meshDrawn: 0,
    meshViewCulled: 3,
    meshUndrawable: 0,
    pointsSubmitted: 2,
    pointsDrawn: 2,
    pointInstancesSubmitted: 40000,
    pointInstancesDrawn: 40000,
    uniformTime: 0,
  });
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-backend"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-post-authored"), "1");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-post-dispatched"), "0");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-post-dead"), "1");
  // The two-week mesh bug, made legible: 3 in the bundle, 0 on screen,
  // 3 explained by the CPU frustum cull.
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-mesh-submitted"), "3");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-mesh-drawn"), "0");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-mesh-view-culled"), "3");
  // The time-stuck-at-0 bug, made legible.
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-uniform-time"), "0.000");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-uniform-time-advancing"), "0");
});

test("render truth: uniform-time-advancing flips once the clock moves", () => {
  const { api } = loadRenderTruth();
  const mount = fakeMount();
  const base = { backend: "webgl", postChain: [], meshSubmitted: 0 };
  api.publish(mount, Object.assign({}, base, { uniformTime: 0 }));
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-uniform-time-advancing"), "0");
  api.publish(mount, Object.assign({}, base, { uniformTime: 1.5 }));
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-uniform-time-advancing"), "1");
  api.publish(mount, Object.assign({}, base, { uniformTime: 1.5 }));
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-uniform-time-advancing"), "0",
    "a frozen clock must report not-advancing even at a non-zero value");
});

test("render truth: telemetry is OFF unless the diagnostics tier is enabled", () => {
  const context = vm.createContext({
    window: {},
    navigator: { userAgent: "test" },
    performance: { now: () => 0 },
    console,
    WeakMap,
  });
  vm.runInContext(sharedSource, context);
  const api = context.window.__gosx_scene3d_render_truth_api;
  assert.equal(api.enabled(), false, "production must not pay for render truth");
  context.window.__gosx_telemetry_config = { scene3dDiagnostics: true };
  assert.equal(api.enabled(), true);
});

test("render truth: implementation identity separates Dawn/Tint from wgpu/naga", () => {
  // "Both browsers support WebGPU" hides two WGSL translators with two
  // separate sets of compiler bugs. Selena validates its emitted WGSL with
  // naga (Firefox's compiler), so a shader can pass authoring-time validation
  // and still be miscompiled by Tint in Edge -- which is where the white
  // parallelogram and the pink homepage were observed. A dump that cannot name
  // the implementation cannot distinguish "bad shader" from "compiler
  // disagreement".
  const firefox = loadRenderTruth({ navigator: { userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:141.0) Gecko/20100101 Firefox/141.0" } });
  assert.equal(firefox.api.browserEngine(), "gecko");
  assert.equal(firefox.api.implementation({}), "wgpu");

  const edge = loadRenderTruth({ navigator: { userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36 Edg/140.0" } });
  assert.equal(edge.api.browserEngine(), "blink-edge");
  assert.equal(edge.api.implementation({}), "dawn");
});

test("render truth: the event journal is an ordered timeline, not a final state", () => {
  // A device that mounts healthy and dies eight seconds later is
  // indistinguishable from "never had WebGPU" in any single sample. Only an
  // ordered log separates them -- and that sequence was observed on Firefox
  // under GPU-memory pressure.
  const { api } = loadRenderTruth();
  api.record("backend", "webgpu");
  api.record("webgpu-device-ready", "wgpu");
  api.record("device-lost", "unknown out of memory");
  api.record("backend", "webgl fallback=webgpu-device-lost");
  const events = api.events();
  assert.equal(events.length, 4);
  // Joined rather than deepEqual: the journal array is created inside the VM
  // realm, so its prototype is not this realm's Array.prototype.
  assert.equal(
    events.map((e) => e.kind).join(","),
    "backend,webgpu-device-ready,device-lost,backend"
  );
  assert.match(api.encodeEvents(), /device-lost:unknown out of memory/);
});

test("render truth: a latch that settled on a backend that no longer runs is flagged stale", () => {
  // m31labs.dev's post-FX WebGL guard sets postFXWebGLGuardSettled=true the
  // first time it observes WebGPU and never re-arms. When the device later
  // dies, the full four-pass chain keeps running on WebGL and every per-frame
  // counter still reports a healthy chain. The latch record makes the
  // mismatch a published fact instead of an inference.
  const { api } = loadRenderTruth();
  api.latch("postfx-webgl-guard", "webgpu", true);
  assert.equal(api.staleLatches("webgpu"), 0, "a latch matching the live backend is not stale");
  assert.equal(api.staleLatches("webgl"), 1, "a latch settled on a dead backend must be flagged");
  assert.match(api.encodeLatches("webgl"), /postfx-webgl-guard:1:webgpu:1/);
});

test("render truth: both renderers are wired to the shared publisher", () => {
  // Source-level guard: the value of a backend-neutral attribute surface is
  // that a probe never branches on which backend won. That only holds if BOTH
  // renderers actually call it.
  assert.match(webglSource, /function publishWebGLRenderTruth\(bundle\)/);
  assert.match(webglSource, /publishWebGLRenderTruth\(bundle\);/);
  assert.match(webglSource, /webglRenderTruthStats\.meshDrawn \+= 1;/);
  assert.match(webglSource, /webglRenderTruthStats\.meshViewCulled \+= 1;/);
  assert.match(webglSource, /webglRenderTruthStats\.meshUndrawable \+= 1;/);
  assert.match(webgpuSource, /truthApi\.publish\(mount, \{/);
  assert.match(webgpuSource, /meshUndrawable: webGPUCountUndrawableMeshObjects\(bundle\)/);
});

test("render truth: the WebGPU post chain is marked at the single dispatch funnel", () => {
  // Counting inside fullscreenPass (rather than in each switch case) means a
  // newly added effect cannot forget to report itself, and bloom's four
  // internal passes are counted honestly.
  const funnel = webgpuSource.slice(
    webgpuSource.indexOf("function fullscreenPass(encoder, pipeline, bindGroup, targetView)")
  ).slice(0, 900);
  assert.match(funnel, /pass\.end\(\);/);
  assert.match(funnel, /activePostChain && activePostIndex >= 0/);
  // ...and the final blit must NOT be attributed to the last authored effect.
  assert.match(webgpuSource, /activePostIndex = -1;\n\n {8}\/\/ If no effects matched or we need a final blit\./);
});

test("render truth: the WebGPU customPost dead path reports WHY it did not draw", () => {
  const marker = "// Not yet compiled (first frame) or failed → identity passthrough.";
  const index = webgpuSource.indexOf(marker);
  assert.ok(index > 0, "customPost identity-passthrough branch not found");
  const branch = webgpuSource.slice(index, index + 1600);
  assert.match(branch, /truth\.PIPELINE_PENDING/);
  assert.match(branch, /truth\.PIPELINE_FAILED/);
  assert.match(branch, /truth\.mark\(postChain, i, cpState, 0\)/);
});

test("render truth: authored Selena shader modules capture browser compilation info", () => {
  // getCompilationInfo() is the browser's own verdict, and the only place a
  // Tint-versus-naga disagreement surfaces as text rather than wrong pixels.
  const captures = webgpuSource.match(/renderTruth\(\)\.captureShaderInfo\(/g) || [];
  assert.ok(captures.length >= 9, `expected every authored Selena shader module instrumented, got ${captures.length}`);
  assert.match(webgpuSource, /captureShaderInfo\(module, "selena-post-" \+ name\);/);
});

test("render truth: device loss and uncaptured GPU errors reach the journal", () => {
  assert.match(webgpuSource, /renderTruth\(\)\.record\("device-lost"/);
  assert.match(webgpuSource, /addEventListener\("uncapturederror"/);
  assert.match(webgpuSource, /renderTruth\(\)\.record\("gpu-uncaptured-error"/);
});

test("render truth: the mount publishes ONE machine-readable backend record", () => {
  assert.match(mountSource, /function sceneRenderBackendTruth\(mount, renderer, fallbackReason, degraded\)/);
  assert.match(mountSource, /data-gosx-scene3d-render-backend-truth/);
  assert.match(mountSource, /data-gosx-scene3d-render-gpu/);
  // gpu must be derived from the backend that actually ran, so a Canvas2D
  // mount -- which runs no shader at all -- can never report gpu=true.
  assert.match(mountSource, /gpu: kind === "webgpu" \|\| kind === "webgl"/);
});
