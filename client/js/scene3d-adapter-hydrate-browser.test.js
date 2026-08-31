"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

const browserProof = fs.readFileSync(
  path.join(__dirname, "testdata", "scene3d-adapter-hydrate-browser.cjs"), "utf8"
);

function preloadSource() {
  const start = browserProof.indexOf("const PRELOAD = `") + "const PRELOAD = `".length;
  const end = browserProof.indexOf("`;\n\nconst CAPS_EXPR", start);
  assert.ok(start > "const PRELOAD = `".length && end > start, "browser proof PRELOAD must remain extractable");
  return browserProof.slice(start, end);
}

function presentExpressionBuilder() {
  const start = browserProof.indexOf("function webGPUPresentExpr(c, afterColorPasses, afterCompletedColorPasses, expectedObjectX, requireCompleted) {");
  const end = browserProof.indexOf("\nfunction webGPUQueueFenceExpr", start);
  assert.ok(start >= 0 && end > start, "browser proof WebGPU readiness helper must remain extractable");
  return vm.runInNewContext(browserProof.slice(start, end) + "; webGPUPresentExpr");
}

function queueFenceExpressionBuilder() {
  const start = browserProof.indexOf("function webGPUQueueFenceExpr(c, label) {");
  const end = browserProof.indexOf("\nasync function waitForWebGPUPresentation", start);
  assert.ok(start >= 0 && end > start, "browser proof WebGPU queue fence helper must remain extractable");
  return vm.runInNewContext(browserProof.slice(start, end) + "; webGPUQueueFenceExpr");
}

function readinessPollHelper() {
  const start = browserProof.indexOf("async function evalSend(send, expression, extra) {");
  const end = browserProof.indexOf("\nconst PRELOAD", start);
  assert.ok(start >= 0 && end > start, "browser proof readiness poll must remain extractable");
  const state = { now: 0, sleepCalls: 0 };
  const context = {
    Date: { now() { state.now += 1; return state.now; } },
    Error,
    JSON,
    MOUNT_MS: 40000,
    Object,
    sleep: async () => { state.sleepCalls += 1; state.now += 50; },
  };
  const poll = vm.runInNewContext(browserProof.slice(start, end) + "; poll", context);
  return { poll, state };
}

function failureReceiptExpressionBuilder() {
  const start = browserProof.indexOf("function webGPUFailureReceiptExpr(c, phase) {");
  const end = browserProof.indexOf("\nasync function captureWebGPUFailureReceipt", start);
  assert.ok(start >= 0 && end > start, "browser proof WebGPU failure receipt helper must remain extractable");
  return vm.runInNewContext(browserProof.slice(start, end) + "; webGPUFailureReceiptExpr");
}

function runtimeSendFor(context) {
  return async (method, params) => {
    assert.equal(method, "Runtime.evaluate");
    return { result: { value: vm.runInContext(params.expression, context) } };
  };
}

function caseEvidenceRetainer() {
  const start = browserProof.indexOf("function boundedValue(value, depth, seen) {");
  const end = browserProof.indexOf("\nfunction identityExpr", start);
  assert.ok(start >= 0 && end > start, "browser proof case-evidence retainer must remain extractable");
  const source = "const DIAGNOSTIC_CAPTURE_MS = 25;\n" + browserProof.slice(start, end) +
    "; ({ retainCaseEvidence, boundedValue, safeErrorSnapshot })";
  return vm.runInNewContext(source, { setTimeout, clearTimeout });
}

function browserContext() {
  const submittedWork = [];
  let onSubmittedWorkDoneCalls = 0;
  function GPUCommandEncoder() {}
  GPUCommandEncoder.prototype.beginRenderPass = function () { return { end() {} }; };
  function GPURenderPassEncoder() {}
  GPURenderPassEncoder.prototype.draw = function () {};
  GPURenderPassEncoder.prototype.drawIndexed = function () {};
  function GPUQueue() {}
  GPUQueue.prototype.submit = function () {};
  GPUQueue.prototype.onSubmittedWorkDone = function () {
    onSubmittedWorkDoneCalls += 1;
    return new Promise((resolve, reject) => { submittedWork.push({ resolve, reject }); });
  };
  function GPUDevice(queue) { this.queue = queue; }
  GPUDevice.prototype.createCommandEncoder = function () { return new GPUCommandEncoder(); };
  function GPUCanvasContext() {}
  GPUCanvasContext.prototype.configure = function () {};
  GPUCanvasContext.prototype.getCurrentTexture = function () { return { createView() { return {}; } }; };

  const mount = {
    attributes: {
      "data-gosx-scene3d-renderer": "webgpu",
      "data-gosx-scene3d-mounted": "true",
      "data-gosx-scene3d-webgpu-mesh-objects": "1",
      "data-gosx-scene3d-webgpu-mesh-draw-calls": "1",
      "data-gosx-scene3d-webgpu-mesh-view-culled": "0",
      "data-gosx-scene3d-command-revision": "1",
      "data-gosx-scene3d-command-applied-revision": "1",
    },
    getAttribute(name) { return this.attributes[name] || null; },
  };
  const context = { GPUCommandEncoder, GPURenderPassEncoder, GPUQueue, GPUDevice, GPUCanvasContext, mount };
  context.window = context;
  context.document = { getElementById() { return mount; } };
  mount.__gosxScene3DState = { objects: new Map([["1", { x: 0 }]]) };
  mount.__gosxScene3DHandle = {};
  mount.__gosxScene3DWebGPUStats = { frameSeq: 0, meshObjects: 1 };
  vm.createContext(context);
  vm.runInContext(preloadSource(), context, { filename: "scene3d-adapter-hydrate-browser-preload.js" });
  const queue = new context.GPUQueue();
  const device = new context.GPUDevice(queue);
  const probe = { ready: true, adapter: {}, device, error: "", lost: null, warnings: [] };
  const diagnostics = { renderer: "webgpu", ready: true, deviceLost: false };
  context.__gosx_scene3d_webgpu_probe = () => probe;
  context.__gosx_scene3d_webgpu_diagnostics = () => diagnostics;
  return {
    context,
    encoder: new context.GPUCommandEncoder(),
    queue,
    device,
    probe,
    diagnostics,
    mount,
    onSubmittedWorkDoneCalls() { return onSubmittedWorkDoneCalls; },
    pendingSubmittedWork() { return submittedWork.length; },
    resolveSubmittedWork() {
      const entry = submittedWork.shift();
      assert.ok(entry, "expected pending submitted-work fence");
      entry.resolve();
    },
    rejectSubmittedWork(error) {
      const entry = submittedWork.shift();
      assert.ok(entry, "expected pending submitted-work fence");
      entry.reject(error);
    },
  };
}

test("WebGPU browser-proof readiness rejects depth-only and pre-command color submits", async () => {
  const buildExpression = presentExpressionBuilder();
  const buildFence = queueFenceExpressionBuilder();
  const fixture = browserContext();
  const c = { mount: "scene3d-adapter-hydrate-browser-test" };

  fixture.encoder.beginRenderPass({ colorAttachments: [] }).end();
  fixture.queue.submit([]);

  assert.equal(fixture.context.__adapterWGPasses, 1);
  assert.equal(fixture.context.__adapterWGColorPasses, 0);
  assert.equal(fixture.context.__adapterWGCompletedColorPasses, 0);
  assert.equal(fixture.context.__adapterWGQueueFenceCalls, 0, "submit instrumentation must not create queue fences");
  assert.equal(fixture.pendingSubmittedWork(), 0);
  const depthOnly = vm.runInContext(buildExpression(c, 0, 0, undefined, false), fixture.context);
  assert.equal(depthOnly.ready, false, "the depth-only dummy-shadow pass must never make a blank first frame ready");
  assert.equal(depthOnly.predicates.freshColorPass, false);
  assert.equal(depthOnly.predicates.completedColorPasses, 0);

  fixture.encoder.beginRenderPass({ colorAttachments: [{ view: {} }] }).end();
  fixture.queue.submit([]);
  const submitted = vm.runInContext(buildExpression(c, 0, 0, undefined, false), fixture.context);
  assert.equal(submitted.ready, true, "a fresh color submit may arm the capture-time queue fence");
  assert.equal(submitted.completedSubmits, 0);
  assert.equal(fixture.onSubmittedWorkDoneCalls(), 0);
  const firstFence = vm.runInContext(buildFence(c, "first frame"), fixture.context);
  const reusedFence = vm.runInContext(buildFence(c, "first frame"), fixture.context);
  assert.equal(fixture.context.__adapterWGQueueFenceCalls, 1, "one pending readiness epoch must share one queue fence");
  assert.equal(fixture.onSubmittedWorkDoneCalls(), 1);
  assert.equal(fixture.pendingSubmittedWork(), 1);
  fixture.resolveSubmittedWork();
  const firstFenceResult = await firstFence;
  const reusedFenceResult = await reusedFence;
  assert.equal(firstFenceResult.ready, true);
  assert.equal(firstFenceResult.targetSubmits, 2);
  assert.equal(firstFenceResult.targetColorPasses, 1);
  assert.equal(reusedFenceResult.ready, true);
  assert.equal(reusedFenceResult.targetSubmits, 2);
  assert.equal(reusedFenceResult.targetColorPasses, 1);

  const ready = vm.runInContext(buildExpression(c, 0, 0, undefined, true), fixture.context);
  assert.equal(ready.colorPasses, 1);
  assert.equal(ready.completedSubmits, 2);
  assert.equal(ready.queueFenceCalls, 1);
  assert.equal(ready.meshDrawCalls, "1");

  // Model an old-state rAF that completes before command application. The
  // production harness samples this baseline atomically only after observing
  // x=1.25, so this pass is intentionally included in the baseline.
  fixture.encoder.beginRenderPass({ colorAttachments: [{ view: {} }] }).end();
  fixture.queue.submit([]);
  const baselineFence = vm.runInContext(buildFence(c, "pre-command baseline"), fixture.context);
  assert.equal(fixture.context.__adapterWGQueueFenceCalls, 2, "a later readiness epoch gets one new queue fence");
  fixture.resolveSubmittedWork();
  await baselineFence;
  const postCommandBaseline = fixture.context.__adapterWGColorPasses;
  fixture.mount.__gosxScene3DState.objects.get("1").x = 1.25;

  const completedPostCommandBaseline = fixture.context.__adapterWGCompletedColorPasses;
  const staleColor = vm.runInContext(
    buildExpression(c, postCommandBaseline, completedPostCommandBaseline, 1.25, true), fixture.context
  );
  assert.equal(staleColor.ready, false, "a completed pre-command color pass must not satisfy the post-command capture gate");
  assert.equal(staleColor.predicates.freshColorPass, false);

  fixture.encoder.beginRenderPass({ colorAttachments: [{ view: {} }] }).end();
  fixture.queue.submit([]);
  const pendingPostCommand = vm.runInContext(
    buildExpression(c, postCommandBaseline, completedPostCommandBaseline, 1.25, true), fixture.context
  );
  assert.equal(pendingPostCommand.ready, false, "a fresh post-command submit still waits for its capture-time fence");
  const postCommandFence = vm.runInContext(buildFence(c, "after hub command"), fixture.context);
  fixture.resolveSubmittedWork();
  await postCommandFence;

  const postCommandReady = vm.runInContext(
    buildExpression(c, postCommandBaseline, completedPostCommandBaseline, 1.25, true), fixture.context
  );
  assert.equal(postCommandReady.colorPasses, postCommandBaseline + 1);
  assert.equal(postCommandReady.objectX, 1.25);

  fixture.mount.__gosxScene3DState.objects.get("1").x = 0;
  const staleState = vm.runInContext(
    buildExpression(c, postCommandBaseline, completedPostCommandBaseline, 1.25, true), fixture.context
  );
  assert.equal(staleState.ready, false, "a visible frame with stale command state must not satisfy the post-command capture gate");
  assert.equal(staleState.predicates.commandState, false);
  assert.equal(fixture.pendingSubmittedWork(), 0, "all explicit queue fences must be settled");
});

test("WebGPU browser-proof readiness exits promptly on device-loss fallback before queue fence", async () => {
  const buildExpression = presentExpressionBuilder();
  const buildReceipt = failureReceiptExpressionBuilder();
  const { poll, state } = readinessPollHelper();
  const fixture = browserContext();
  const c = { mount: "scene3d-adapter-hydrate-browser-test" };

  fixture.encoder.beginRenderPass({ colorAttachments: [{ view: {} }] }).end();
  fixture.queue.submit([]);
  fixture.mount.attributes["data-gosx-scene3d-renderer"] = "webgl";
  fixture.mount.attributes["data-gosx-scene3d-renderer-fallback"] = "webgpu-device-lost";
  fixture.probe.lost = { reason: "destroyed", message: "test loss before explicit fence" };
  fixture.diagnostics.deviceLost = true;

  await assert.rejects(
    poll(runtimeSendFor(fixture.context), buildExpression(c, 0, 0, undefined, false),
      "wg first frame submitted scene color frame", 500),
    (caught) => {
      assert.equal(caught.terminal, true);
      assert.equal(caught.classification, "device-lost-after-color-pass");
      assert.equal(caught.phase, "wg first frame submitted scene color frame");
      assert.equal(caught.lastPredicate.terminal, true);
      assert.equal(caught.lastPredicate.predicates.fallback, "webgpu-device-lost");
      assert.equal(caught.lastPredicate.predicates.probeLost, true);
      return true;
    }
  );
  assert.equal(state.sleepCalls, 0, "terminal device-loss fallback must not burn the readiness timeout");
  assert.equal(fixture.onSubmittedWorkDoneCalls(), 0, "terminal polling must not create the explicit queue fence");
  assert.equal(fixture.pendingSubmittedWork(), 0);

  const receipt = vm.runInContext(buildReceipt(c, "webgpu-first-presentation-readiness"), fixture.context);
  assert.equal(receipt.classification, "device-lost-after-color-pass");
  assert.equal(receipt.mount.fallback, "webgpu-device-lost");

  const slowFixture = browserContext();
  slowFixture.mount.attributes["data-gosx-scene3d-renderer"] = "webgl";
  const slow = vm.runInContext(buildExpression(c, 0, 0, undefined, false), slowFixture.context);
  assert.equal(slow.ready, false);
  assert.equal(slow.terminal, false, "ordinary WebGL/no-WebGPU readiness lag must not be terminal in the WG proof");
  assert.equal(slow.classification, "predicate-not-ready");

  const slowPoll = readinessPollHelper();
  await assert.rejects(
    slowPoll.poll(runtimeSendFor(slowFixture.context), buildExpression(c, 0, 0, undefined, false),
      "wg slow nonterminal readiness", 5),
    (caught) => {
      assert.match(caught.message, /^timeout waiting for wg slow nonterminal readiness/);
      assert.equal(caught.terminal, undefined);
      assert.equal(caught.lastPredicate.terminal, false);
      return true;
    }
  );
  assert.ok(slowPoll.state.sleepCalls > 0, "slow nonterminal readiness remains a bounded poll, not a terminal exit");
});

test("WebGPU browser-proof failure receipt survives a timeout and classifies observable causes", async () => {
  const buildReceipt = failureReceiptExpressionBuilder();
  const buildFence = queueFenceExpressionBuilder();
  const fixture = browserContext();
  const c = { mount: "scene3d-adapter-hydrate-browser-test" };

  fixture.device.createCommandEncoder();
  const canvasContext = new fixture.context.GPUCanvasContext();
  canvasContext.configure({ device: fixture.device });
  canvasContext.getCurrentTexture();
  fixture.queue.submit([]);
  const noColor = vm.runInContext(buildReceipt(c, "webgpu-first-presentation-readiness"), fixture.context);
  assert.equal(noColor.phase, "webgpu-first-presentation-readiness");
  assert.equal(noColor.classification, "no-scene-color-pass");
  assert.equal(noColor.identity.wrappedQueueMatchesProbe, true);
  assert.equal(noColor.identity.encoderDeviceMatchesProbe, true);
  assert.equal(noColor.identity.configuredDeviceMatchesProbe, true);
  assert.equal(noColor.wrappers.configureSuccesses, 1);
  assert.equal(noColor.wrappers.currentTextureSuccesses, 1);
  assert.equal(noColor.mount.objectX, 0);
  assert.equal(noColor.mount.stats.meshObjects, 1);

  fixture.context.__adapterWGLatestQueue = new fixture.context.GPUQueue();
  const mismatch = vm.runInContext(buildReceipt(c, "webgpu-first-presentation-readiness"), fixture.context);
  assert.equal(mismatch.classification, "instrumented-queue-or-device-mismatch");
  assert.equal(mismatch.identity.wrappedQueueMatchesProbe, false);

  fixture.context.__adapterWGLatestQueue = fixture.queue;
  fixture.encoder.beginRenderPass({ colorAttachments: [{ view: {} }] }).end();
  fixture.queue.submit([]);
  const waiting = vm.runInContext(buildReceipt(c, "webgpu-first-presentation-readiness"), fixture.context);
  assert.equal(waiting.classification, "color-submitted-awaiting-queue");
  assert.equal(waiting.wrappers.colorPasses, 1);
  assert.equal(waiting.wrappers.completedColorPasses, 0);

  const rejectedFence = vm.runInContext(buildFence(c, "first frame"), fixture.context);
  fixture.rejectSubmittedWork(new Error("adversarial queue rejection"));
  await assert.rejects(rejectedFence, (caught) => caught.message === "adversarial queue rejection");
  const rejected = vm.runInContext(buildReceipt(c, "webgpu-first-presentation-readiness"), fixture.context);
  assert.equal(rejected.classification, "queue-completion-rejected");
  assert.equal(rejected.wrappers.failedSubmits, 1);
  assert.equal(rejected.wrappers.queueFenceError, "adversarial queue rejection");
  assert.equal(fixture.context.__adapterWGCompletionFencePending, false);
  assert.equal(fixture.context.__adapterWGCompletionFencePromise, null);

  const lostFixture = browserContext();
  lostFixture.device.createCommandEncoder();
  lostFixture.encoder.beginRenderPass({ colorAttachments: [{ view: {} }] }).end();
  lostFixture.queue.submit([]);
  const lostFence = vm.runInContext(buildFence(c, "lost frame"), lostFixture.context);
  lostFixture.resolveSubmittedWork();
  await lostFence;
  lostFixture.probe.lost = { reason: "destroyed", message: "test loss" };
  const lost = vm.runInContext(buildReceipt(c, "webgpu-first-presentation-readiness"), lostFixture.context);
  assert.equal(lost.classification, "device-lost-after-color-pass");
  assert.equal(lost.probe.lost.reason, "destroyed");

  assert.match(browserProof, /webgpuFailureReceipt[\s\S]*\}\);\n\}/,
    "runCase must attach the WebGPU receipt to the active evidence object");
});

test("WebGPU browser-proof case retention serializes a throwing active case and rethrows it", async () => {
  const { retainCaseEvidence: retain, boundedValue, safeErrorSnapshot } = caseEvidenceRetainer();
  const sink = [];
  const evidence = { name: "wg", backend: "webgpu", phase: "first-capture" };
  const original = new Error("adversarial capture timeout");
  let receiptError = null;

  await assert.rejects(
    retain(sink, evidence, async () => { throw original; }, async (error) => {
      receiptError = error;
      evidence.failure = { phase: evidence.phase, error: error.message };
      evidence.webgpuFailureReceipt = { classification: "no-scene-color-pass", wrappers: { colorPasses: 0 } };
    }),
    (caught) => caught === original
  );
  assert.equal(receiptError, original, "the exact original failure is offered to receipt collection");
  assert.equal(sink.length, 1, "the active case is retained even when capture/readiness throws");
  assert.equal(sink[0], evidence);
  assert.equal(sink[0].failure.error, "adversarial capture timeout");
  assert.equal(sink[0].webgpuFailureReceipt.classification, "no-scene-color-pass");
  assert.match(sink[0].finishedAt, /^\d{4}-\d\d-\d\dT/);

  const callbackSink = [];
  const callbackEvidence = { name: "wg", backend: "webgpu", phase: "first-capture" };
  const callbackFailure = new Error("adversarial receipt failure");
  await assert.rejects(
    retain(callbackSink, callbackEvidence, async () => { throw original; }, async () => { throw callbackFailure; }),
    (caught) => caught === original
  );
  assert.equal(callbackSink.length, 1, "callback failure must not discard the active case");
  assert.equal(callbackEvidence.diagnosticFailures[0].stage, "case-failure-receipt");
  assert.equal(callbackEvidence.diagnosticFailures[0].error.message, "adversarial receipt failure");

  const rejectingSink = [];
  const rejectingEvidence = { name: "wg", backend: "webgpu", phase: "first-capture" };
  await assert.rejects(
    retain(rejectingSink, rejectingEvidence, async () => { throw original; }, () => Promise.reject(callbackFailure)),
    (caught) => caught === original
  );
  assert.equal(rejectingSink.length, 1, "rejected receipt must not discard the active case");
  assert.equal(rejectingEvidence.diagnosticFailures[0].stage, "case-failure-receipt");

  const timeoutSink = [];
  const timeoutEvidence = { name: "wg", backend: "webgpu", phase: "first-capture" };
  const timeoutStartedAt = Date.now();
  await assert.rejects(
    retain(timeoutSink, timeoutEvidence, async () => { throw original; }, () => new Promise(() => {})),
    (caught) => caught === original
  );
  assert.ok(Date.now() - timeoutStartedAt < 500, "diagnostic callback timeout must not hang the proof failure path");
  assert.equal(timeoutSink.length, 1);
  assert.equal(timeoutEvidence.diagnosticFailures[0].stage, "case-failure-receipt-timeout");

  let hostileReads = 0;
  const hostile = {};
  for (const key of ["name", "message", "stack", "lastPredicate"]) {
    Object.defineProperty(hostile, key, { get() { hostileReads += 1; throw new Error("hostile " + key); } });
  }
  const hostileSnapshot = safeErrorSnapshot(hostile);
  assert.equal(hostileReads, 0, "error snapshot must not invoke hostile accessors");
  assert.equal(hostileSnapshot.kind, "object");
  assert.equal(hostileSnapshot.name, undefined);
  assert.equal(hostileSnapshot.message, undefined);
  assert.equal(hostileSnapshot.stack, undefined);
  const cyclic = { value: 1 };
  cyclic.self = cyclic;
  assert.equal(boundedValue(cyclic).self, "[cycle]", "bounded diagnostic data must survive cycles");

  const hostileSink = [];
  const hostileEvidence = { name: "wg", backend: "webgpu", phase: "first-capture" };
  await assert.rejects(
    retain(hostileSink, hostileEvidence, async () => { throw hostile; }, async (caught) => {
      hostileEvidence.failure = { error: safeErrorSnapshot(caught), lastPredicate: boundedValue(undefined) };
      throw callbackFailure;
    }),
    (caught) => caught === hostile
  );
  assert.equal(hostileReads, 0, "retention must rethrow a hostile error-like object without reading its getters");
  assert.equal(hostileSink.length, 1);
  assert.equal(hostileEvidence.failure.error.kind, "object");

  const appendEvidence = { name: "wg", backend: "webgpu", phase: "first-capture" };
  await assert.rejects(
    retain({ push() { throw callbackFailure; } }, appendEvidence, async () => { throw original; }, async () => {}),
    (caught) => caught === original
  );
  assert.equal(appendEvidence.diagnosticFailures[0].stage, "case-evidence-append",
    "receipt serialization/append failures must remain non-authoritative");
});
