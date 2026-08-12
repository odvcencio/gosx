// WebGPU pipeline-validation attribution.
//
// THE DEFECT THIS GUARDS AGAINST
//
// GPUDevice.pushErrorScope / popErrorScope operate on ONE stack owned by the
// DEVICE. gosx built pipelines from six overlapping asynchronous sites and
// popped that stack from each build's own .then / .catch -- that is, in SETTLE
// order against a LIFO stack. Two overlapping builds therefore SWAPPED
// results: the first to settle popped the LAST pusher's scope.
//
// Measured in real Firefox against the live m31labs.dev homepage: four
// authored points shader modules reported ZERO compilation messages and their
// createRenderPipelineAsync RESOLVED, yet every one of them was marked failed,
// because each popped the scope belonging to an Elio compute kernel the driver
// had genuinely rejected. A real error in module A silently disabled feature
// B while B's own diagnostics read perfectly healthy. 18 of 19 point layers
// lost their authored material to an error that was not theirs.
//
// The fake device below models the semantics that were MEASURED in Firefox,
// not assumed:
//
//   case                  create*PipelineAsync   error scope        getCompilationInfo
//   invalid WGSL          rejected               "Parsing error"    error + line number
//   wrong entry point     rejected               null               clean
//   bad vertex layout     rejected               null               clean
//   valid                 resolved               null               clean
//
// Two consequences drive the assertions: the error scope adds NOTHING the
// promise misses, and an async pipeline-validation failure generates no device
// error at all. Detection therefore uses only per-object signals, which cannot
// be cross-attributed.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");

function readSrc(name) {
  return fs.readFileSync(name.startsWith("../") ? path.join(__dirname, name) : path.join(srcDir, name), "utf8");
}

const computeSource = readSrc("../runtime/scene3d/compute.ts");
const webgpuSource = readSrc("../runtime/scene3d/webgpu.ts");
const sharedSource = readSrc("15a-scene-postfx-shared.js");

const GOOD_KERNEL = "@compute @workgroup_size(64) fn simulate() {}";
const BAD_KERNEL = "@compute @workgroup_size(64) fn simulate() { let x = 10u * 3812015801u; }";

// ---------------------------------------------------------------------------
// Fake GPUDevice
// ---------------------------------------------------------------------------
//
// Faithful to the four measured cases above. `settleAfter` controls how many
// microtask turns a pipeline build takes, so the interleaving that produced
// the production defect is DETERMINISTIC rather than hoped for.
function createFakeDevice(options = {}) {
  const badSources = options.badSources || new Set();
  const settleAfter = options.settleAfter || new Map(); // wgsl -> microtask turns
  const trace = {
    pushErrorScope: 0,
    popErrorScope: 0,
    pipelineBuilds: [],      // wgsl of every create*PipelineAsync call, in order
    dispatched: [],          // wgsl of every pipeline actually bound by a pass
  };
  // The device-global error scope stack, exactly as WebGPU specifies it.
  const scopeStack = [];

  const delay = (turns) => {
    let p = Promise.resolve();
    for (let i = 0; i < turns; i++) p = p.then(() => {});
    return p;
  };

  function createShaderModule(desc) {
    const code = desc && desc.code ? String(desc.code) : "";
    const invalid = badSources.has(code);
    if (invalid && scopeStack.length > 0) {
      // Firefox captures the module-creation failure in the INNERMOST scope
      // that is open when createShaderModule runs. This is the only thing the
      // error scope ever saw, and it is the error the overlapping builds
      // fought over.
      const top = scopeStack[scopeStack.length - 1];
      if (top.filter === "validation" && !top.error) {
        top.error = { message: "Shader module creation failed: Parsing error" };
      }
    }
    return {
      __code: code,
      __invalid: invalid,
      label: desc && desc.label,
      getCompilationInfo() {
        return Promise.resolve({
          messages: invalid
            ? [{ type: "error", lineNum: 97, message: "multiplication operation overflowed" }]
            : [],
        });
      },
    };
  }

  function buildPipeline(desc, kind) {
    const mod = desc && desc.compute ? desc.compute.module : (desc.vertex && desc.vertex.module);
    const code = mod ? mod.__code : "";
    trace.pipelineBuilds.push(code);
    const turns = settleAfter.has(code) ? settleAfter.get(code) : 1;
    return delay(turns).then(() => {
      // MEASURED: an async pipeline creation failure rejects the promise and
      // generates NO device error. The scope stack is deliberately untouched.
      if (mod && mod.__invalid) {
        const err = new Error("ShaderModule with '" + (mod.label || "") + "' label is invalid");
        err.name = "GPUPipelineError";
        throw err;
      }
      return { __kind: kind, __code: code };
    });
  }

  const device = {
    __trace: trace,
    limits: {},
    features: new Set(),
    createShaderModule,
    createComputePipelineAsync: (desc) => buildPipeline(desc, "compute"),
    createRenderPipelineAsync: (desc) => buildPipeline(desc, "render"),
    createPipelineLayout: () => ({}),
    createBindGroupLayout: () => ({}),
    createBindGroup: () => ({}),
    createBuffer: (desc) => ({ size: (desc && desc.size) || 0, destroy() {} }),
    createSampler: () => ({}),
    createTexture: () => ({ createView: () => ({}), destroy() {} }),
    pushErrorScope(filter) {
      trace.pushErrorScope++;
      scopeStack.push({ filter, error: null });
    },
    popErrorScope() {
      trace.popErrorScope++;
      const scope = scopeStack.pop();
      return Promise.resolve(scope ? scope.error : null);
    },
    queue: {
      writeBuffer() {},
      submit() {},
    },
  };
  return device;
}

function fakeEncoder(trace) {
  return {
    beginComputePass: () => ({
      setPipeline(p) { trace.dispatched.push(p && p.__code); },
      setBindGroup() {},
      dispatchWorkgroups() {},
      end() {},
    }),
  };
}

// loadCompute evaluates 16b in a throwaway VM. Only sceneNumber /
// sceneColorRGBA and the two WebGPU bitfield globals are needed from outside.
function loadCompute() {
  const windowStub = { __gosx_scene3d_api: {} };
  const context = vm.createContext({
    window: windowStub,
    console: { warn() {}, error() {}, log() {} },
    GPUBufferUsage: { STORAGE: 1, COPY_DST: 2, VERTEX: 4, UNIFORM: 8, COPY_SRC: 16, MAP_READ: 32, INDIRECT: 64, INDEX: 128 },
    GPUShaderStage: { COMPUTE: 1, VERTEX: 2, FRAGMENT: 4 },
    Promise, Map, Set, Math, JSON, Float32Array, Uint32Array, Int32Array, DataView, ArrayBuffer, Number, String, Object, Array, Boolean, Error, isFinite, parseFloat, parseInt,
  });
  vm.runInContext(
    "function sceneNumber(v, f) { return typeof v === 'number' && isFinite(v) ? v : f; }\n" +
    "function sceneColorRGBA(v, f) { return f || [1,1,1,1]; }\n" +
    computeSource,
    context
  );
  return { api: windowStub.__gosx_scene3d_api, windowStub };
}

// flush drains the microtask queue far enough for every modelled build,
// including a fallback build chained behind a failed one, to settle.
async function flush(turns = 40) {
  for (let i = 0; i < turns; i++) await Promise.resolve();
}

// ---------------------------------------------------------------------------
// The regression
// ---------------------------------------------------------------------------

test("webgpu: an overlapping build's rejection is not attributed to a healthy pipeline", async () => {
  const { api } = loadCompute();
  // The bad kernel settles FIRST. Under the old code its build popped the
  // scope belonging to the LATER pusher (the good kernel), read null, and the
  // good kernel's own pop then received the bad kernel's error.
  const device = createFakeDevice({
    badSources: new Set([BAD_KERNEL]),
    settleAfter: new Map([[BAD_KERNEL, 1], [GOOD_KERNEL, 4]]),
  });

  // Two authored compute systems built back to back, the way a scene with more
  // than one particle system does. Their validations overlap.
  const bad = api.createSceneParticleSystem(device, { id: "elio-kernel", count: 64, computeWGSL: BAD_KERNEL, computeEntry: "simulate" });
  const good = api.createSceneParticleSystem(device, { id: "galaxy-points", count: 64, computeWGSL: GOOD_KERNEL, computeEntry: "simulate" });

  await flush();

  assert.ok(bad.isReady(), "the rejected system must still come up on the builtin fallback");
  assert.ok(good.isReady(), "the healthy system must come up");

  const encoder = fakeEncoder(device.__trace);
  bad.update(device, encoder, 0.016, 0.016);
  good.update(device, encoder, 0.016, 0.016);

  const dispatched = device.__trace.dispatched;
  assert.equal(dispatched.length, 2);
  assert.notEqual(dispatched[0], BAD_KERNEL, "the rejected kernel must not be dispatched");
  // THE ASSERTION THE BUG BROKE. The good kernel is valid, and its own
  // per-object signals say so. It must run its own payload kernel, not be
  // demoted to the builtin by an error that belongs to the other system.
  assert.equal(
    dispatched[1], GOOD_KERNEL,
    "a healthy authored kernel must keep its own pipeline when an unrelated build fails"
  );

  // The good system must have needed exactly ONE build. A second build for it
  // is the fingerprint of a false failure: it fell back to the builtin.
  const goodBuilds = device.__trace.pipelineBuilds.filter((c) => c === GOOD_KERNEL).length;
  assert.equal(goodBuilds, 1, "a healthy kernel must not be rebuilt as a builtin fallback");
});

test("webgpu: pipeline validation pushes no device-global error scope", async () => {
  const { api } = loadCompute();
  const device = createFakeDevice({
    badSources: new Set([BAD_KERNEL]),
    settleAfter: new Map([[BAD_KERNEL, 1], [GOOD_KERNEL, 4]]),
  });
  api.createSceneParticleSystem(device, { id: "a", count: 64, computeWGSL: BAD_KERNEL, computeEntry: "simulate" });
  api.createSceneParticleSystem(device, { id: "b", count: 64, computeWGSL: GOOD_KERNEL, computeEntry: "simulate" });
  await flush();

  // The mechanism, asserted directly. A device-global stack cannot say WHICH
  // overlapping operation an error belongs to, so per-pipeline validation must
  // not use one at all.
  assert.equal(device.__trace.pushErrorScope, 0, "per-pipeline validation must not push a device error scope");
  assert.equal(device.__trace.popErrorScope, 0, "per-pipeline validation must not pop a device error scope");
});

// ---------------------------------------------------------------------------
// Real errors are still detected
// ---------------------------------------------------------------------------

test("webgpu: a genuinely invalid kernel is still detected and falls back", async () => {
  const { api } = loadCompute();
  const device = createFakeDevice({ badSources: new Set([BAD_KERNEL]) });
  const system = api.createSceneParticleSystem(device, { id: "elio-kernel", count: 64, computeWGSL: BAD_KERNEL, computeEntry: "simulate" });
  await flush();

  assert.ok(system.isReady(), "the system must recover on the builtin kernel");
  const encoder = fakeEncoder(device.__trace);
  system.update(device, encoder, 0.016, 0.016);
  assert.equal(device.__trace.dispatched.length, 1);
  assert.notEqual(device.__trace.dispatched[0], BAD_KERNEL, "the rejected kernel must never be dispatched");
  // Two builds: the rejected payload, then the builtin fallback.
  assert.equal(device.__trace.pipelineBuilds.length, 2);
  assert.equal(device.__trace.pipelineBuilds[0], BAD_KERNEL);
});

test("webgpu: a module carrying compilation errors is refused even if the pipeline resolves", async () => {
  const { api } = loadCompute();
  // An implementation that resolves the pipeline anyway. The per-object
  // getCompilationInfo belt-and-braces must still refuse the kernel -- this is
  // the check the error scope used to provide, now keyed to the module it
  // describes instead of to a device-global stack.
  const device = createFakeDevice();
  const inner = device.createShaderModule;
  device.createShaderModule = (desc) => {
    const mod = inner(desc);
    if (String(desc.code) === BAD_KERNEL) {
      mod.getCompilationInfo = () => Promise.resolve({
        messages: [{ type: "error", lineNum: 97, message: "multiplication operation overflowed" }],
      });
    }
    return mod;
  };
  const system = api.createSceneParticleSystem(device, { id: "lenient-driver", count: 64, computeWGSL: BAD_KERNEL, computeEntry: "simulate" });
  await flush();

  assert.ok(system.isReady());
  const encoder = fakeEncoder(device.__trace);
  system.update(device, encoder, 0.016, 0.016);
  assert.notEqual(device.__trace.dispatched[0], BAD_KERNEL, "a module with compilation errors must not be dispatched");
});

// ---------------------------------------------------------------------------
// Telemetry: a rejected authored kernel is one attribute read away
// ---------------------------------------------------------------------------

function loadRenderTruth() {
  const windowStub = { __gosx_scene3d_render_truth: true };
  const context = vm.createContext({
    window: windowStub,
    navigator: { userAgent: "test" },
    performance: { now: () => 1000 },
    console,
    WeakMap,
  });
  vm.runInContext(sharedSource, context);
  return windowStub;
}

test("render truth: a rejected authored compute kernel is published, not just logged", async () => {
  const truthWindow = loadRenderTruth();
  const { windowStub, api } = loadCompute();
  // 16b reaches 15a across chunk boundaries through the global, so wiring the
  // published API onto the compute VM's window is exactly the production path.
  windowStub.__gosx_scene3d_render_truth_api = truthWindow.__gosx_scene3d_render_truth_api;

  const device = createFakeDevice({ badSources: new Set([BAD_KERNEL]) });
  api.createSceneParticleSystem(device, { id: "elio-galaxy", count: 64, computeWGSL: BAD_KERNEL, computeEntry: "simulate" });
  await flush();

  const truth = truthWindow.__gosx_scene3d_render_truth_api;
  assert.equal(truth.pipelineFailureCount(), 1, "the rejection must increment a published counter");
  const encoded = truth.encodePipelineFailures();
  assert.match(encoded, /^compute-payload@elio-galaxy:/, "the failure must name the stage and the system");

  // And it must be in the ordered journal, which is what says WHEN it happened
  // relative to device loss and backend swaps.
  const events = truth.events().filter((e) => e.kind === "pipeline-rejected");
  assert.equal(events.length, 1);
  assert.match(events[0].detail, /compute-payload@elio-galaxy/);

  // The publish surface: one attribute read, no multi-session investigation.
  const attrs = new Map();
  truth.publish({ setAttribute: (n, v) => attrs.set(n, String(v)) }, { backend: "webgpu" });
  assert.equal(attrs.get("data-gosx-scene3d-render-pipeline-failures"), "1");
  assert.match(attrs.get("data-gosx-scene3d-render-pipeline-failed"), /compute-payload@elio-galaxy/);
});

// ---------------------------------------------------------------------------
// Structural guard for 16a
// ---------------------------------------------------------------------------
//
// The three authored-pipeline builders in 16a live inside the renderer
// closure and are not separately constructible, so their contribution to the
// shared stack is asserted structurally: only the two scopes that are
// provably balanced may remain.

test("webgpu: 16a keeps only the two error scopes that cannot interleave", () => {
  const pushes = (webgpuSource.match(/\.pushErrorScope\(/g) || []).length;
  // 1. ensureFBOs' "out-of-memory" allocation guard: pushed and popped in one
  //    synchronous block, so nothing can be pushed between them.
  // 2. beginWebGPUErrorScope's per-frame "validation" scope: guarded against
  //    re-entry by pendingWebGPUErrorScope.
  assert.equal(pushes, 2, "16a must push exactly the ensureFBOs and per-frame error scopes");
  assert.match(webgpuSource, /device\.pushErrorScope\("out-of-memory"\)/);
  assert.match(webgpuSource, /device\.pushErrorScope\("validation"\)/);

  // No async pipeline build may reach for a scope again. The 5000-char
  // window covers the longest builder (the authored-particle one, whose
  // memoized cache key sits before the sceneShaderModuleError consult at
  // ~4400 chars) and still ends before the nearest real pushErrorScope
  // outside these functions (~6500 chars after buildCustomPostPipelineAsync).
  for (const name of ["buildCustomPostPipelineAsync", "buildAuthoredPointsVertexPipelineAsync", "buildAuthoredParticleRenderPipelineAsync"]) {
    const start = webgpuSource.indexOf("function " + name + "(");
    assert.ok(start > 0, name + " must exist");
    const body = webgpuSource.slice(start, start + 5000);
    assert.ok(!body.includes("pushErrorScope"), name + " must validate per object, not through a device error scope");
    assert.ok(body.includes("sceneShaderModuleError"), name + " must consult getCompilationInfo for its own modules");
  }
});

test("webgpu: 16b pushes no error scope at all", () => {
  assert.equal((computeSource.match(/\.pushErrorScope\(/g) || []).length, 0);
  assert.equal((computeSource.match(/\.popErrorScope\(/g) || []).length, 0);
});
