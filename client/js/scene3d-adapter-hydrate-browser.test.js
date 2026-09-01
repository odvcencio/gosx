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

function probeLossCountExpressionBuilder() {
  const start = browserProof.indexOf("function probeLossCountExpr() {");
  const end = browserProof.indexOf("\nfunction webGPUFailureReceiptExpr", start);
  assert.ok(start >= 0 && end > start, "browser proof probe-loss baseline helper must remain extractable");
  return vm.runInNewContext(browserProof.slice(start, end) + "; probeLossCountExpr");
}

function outcomeClassifier() {
  const start = browserProof.indexOf("function visibleFrameOK(metrics) {");
  const end = browserProof.indexOf("\nfunction assertEnvelope", start);
  assert.ok(start >= 0 && end > start, "browser proof WG outcome classifier must remain extractable");
  const source = [
    "const OUTCOME_NATIVE_WEBGPU = 'native-webgpu';",
    "const OUTCOME_FALLBACK_UNAVAILABLE = 'fallback-unavailable';",
    "const OUTCOME_FALLBACK_DEVICE_LOST = 'fallback-device-lost';",
    "const FALLBACK_UNAVAILABLE = 'webgpu-unavailable';",
    "const FALLBACK_DEVICE_LOST = 'webgpu-device-lost';",
    browserProof.slice(start, end),
    "; classifyWGOutcomeSnapshot",
  ].join("\n");
  return vm.runInNewContext(source);
}

function fallbackInstabilityClassifier() {
  const start = browserProof.indexOf("function renderStateForOutcome(drawState) {");
  const end = browserProof.indexOf("\nasync function waitForWGPresentationOrFallback", start);
  assert.ok(start >= 0 && end > start, "browser proof fallback-instability classifier must remain extractable");
  return vm.runInNewContext([
    "const OUTCOME_FALLBACK_UNAVAILABLE = 'fallback-unavailable';",
    "const OUTCOME_FALLBACK_DEVICE_LOST = 'fallback-device-lost';",
    "const warningOccurrences = [];",
    browserProof.slice(start, end),
    "; fallbackInstabilityDiagnostic",
  ].join("\n"));
}

function canvasTransitionClassifier() {
  const canvasStart = browserProof.indexOf("function staleGenerationIdentityOK(c, identity) {");
  const canvasEnd = browserProof.indexOf("\nfunction hubCommandExpr", canvasStart);
  const visibleStart = browserProof.indexOf("function visibleFrameOK(metrics) {");
  const visibleEnd = browserProof.indexOf("\nfunction receiptHasIndependentDeviceLoss", visibleStart);
  const receiptStart = browserProof.indexOf("function receiptHasIndependentDeviceLoss(receipt) {");
  const receiptEnd = browserProof.indexOf("\nfunction classifyWGOutcomeSnapshot", receiptStart);
  assert.ok(canvasStart >= 0 && canvasEnd > canvasStart, "browser proof canvas transition helpers must remain extractable");
  assert.ok(visibleStart >= 0 && visibleEnd > visibleStart, "browser proof visible-frame helper must remain extractable");
  assert.ok(receiptStart >= 0 && receiptEnd > receiptStart, "browser proof loss receipt helper must remain extractable");
  return vm.runInNewContext([
    "const OUTCOME_FALLBACK_DEVICE_LOST = 'fallback-device-lost';",
    "const FALLBACK_DEVICE_LOST = 'webgpu-device-lost';",
    "const W = 320, H = 180, FG_THRESHOLD = 12, FG_COVERAGE = 0.01, REST_COVERAGE = 0.5;",
    browserProof.slice(visibleStart, visibleEnd),
    browserProof.slice(receiptStart, receiptEnd),
    browserProof.slice(canvasStart, canvasEnd),
    "; ({ provisionalCanvasTransitionVerdict, resolvedCanvasTransitionVerdict })",
  ].join("\n"));
}

function warningClassifier() {
  const start = browserProof.indexOf("function classifyWarningEntry(entry, typedFallback, capabilityCounts) {");
  const end = browserProof.indexOf("\nfunction writeReport", start);
  assert.ok(start >= 0 && end > start, "browser proof warning classifier must remain extractable");
  const source = [
    "const OUTCOME_FALLBACK_UNAVAILABLE = 'fallback-unavailable';",
    "const OUTCOME_FALLBACK_DEVICE_LOST = 'fallback-device-lost';",
    "const FALLBACK_DEVICE_LOST = 'webgpu-device-lost';",
    "const FALLBACK_WARNING_PATTERNS = [",
    "  /^console\\.warning: \\[gosx\\] WebGPU probe(?:| failed| requestDevice failed; retrying with a fresh adapter| device lost): [^\\r\\n]+$/,",
    "  /^console\\.warning: \\[gosx\\] WebGPU device lost: [^\\r\\n]+$/,",
    "  /^console\\.warning: \\[gosx\\] WebGPU renderer creation failed: [^\\r\\n]+$/,",
    "  /^console\\.warning: \\[gosx\\] WebGPU factory returned null after probe success; canvas may be tainted$/,",
    "];",
    "const DEVICE_LOSS_BROWSER_WARNINGS = [",
    "  'browser log warning: WebGL: CONTEXT_LOST_WEBGL: loseContext: context lost',",
    "  'browser log warning: A valid external Instance reference no longer exists.',",
    "];",
    "const CAPTURE_DRIVER_WARNING = /^browser log warning: (?:\\[\\.WebGL-0x[0-9A-Fa-f]+\\])?GL Driver Message \\(OpenGL, Performance, GL_CLOSE_PATH_NV, High\\): GPU stall due to ReadPixels(?: \\(this message will no longer repeat\\))?$/;",
    "let errors = [];",
    "let warnings = [];",
    "let warningOccurrences = [];",
    "let caseEvidence = [];",
    browserProof.slice(start, end),
    "; ({ classifyWarningsForReport, setErrors(value) { errors = value; }, setWarnings(value) { warnings = value; warningOccurrences = []; }, setWarningOccurrences(value) { warningOccurrences = value; warnings = value.map((entry) => entry.message); }, setCases(value) { caseEvidence = value; } })",
  ].join("\n");
  return vm.runInNewContext(source);
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

function rejectedWGOutcomeHelper() {
  const helperStart = browserProof.indexOf("async function rejectedWGOutcomeWithReceipt(send, c, label, before) {");
  const helperEnd = browserProof.indexOf("\nasync function retainCaseEvidence", helperStart);
  const receiptStart = browserProof.indexOf("function receiptHasIndependentDeviceLoss(receipt) {");
  const receiptEnd = browserProof.indexOf("\nfunction classifyWGOutcomeSnapshot", receiptStart);
  assert.ok(helperStart >= 0 && helperEnd > helperStart, "browser proof rejected WG helper must remain extractable");
  assert.ok(receiptStart >= 0 && receiptEnd > receiptStart, "browser proof receipt helper must remain extractable");
  const source = [
    browserProof.slice(receiptStart, receiptEnd),
    "let currentCasePhase = 'requested-webgpu-first-presentation-readiness';",
    browserProof.slice(helperStart, helperEnd),
    "; rejectedWGOutcomeWithReceipt",
  ].join("\n");
  const context = {
    captureWebGPUFailureReceipt: async () => ({
      classification: "device-lost-after-color-pass",
      independentDeviceLoss: true,
      probe: { lost: { reason: "destroyed", message: "test loss" } },
      mount: { renderer: "webgpu", fallback: "webgpu-probe-recovered" },
    }),
    safeErrorSnapshot: (error) => ({ message: error && error.message ? error.message : String(error) }),
    String,
  };
  return vm.runInNewContext(source, context);
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
  const probe = { ready: true, adapter: {}, device, error: "", lost: null, lostProbeCount: 0, warnings: [] };
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

function outcomeState(overrides = {}) {
  return Object.assign({
    renderer: "webgpu",
    fallback: "",
    mounted: "true",
    handleReady: true,
    registrySameHandle: true,
    commandReady: "true",
    commandRevision: "1",
    commandAppliedRevision: "1",
    objectX: 0,
    glDraws: 0,
    glContext: "",
    webgpuColorPasses: 1,
    webgpuCompletedColorPasses: 1,
    webgpuCompletedSubmits: 1,
    webgpuFailedSubmits: 0,
  }, overrides);
}

function classifyOutcome(snapshot) {
  return outcomeClassifier()(Object.assign({
    nativeCaps: { webgl2: true, webgpu: true },
    acceptedOutcome: "native-webgpu",
    fallbackKind: "",
    fallbackReceipt: null,
    firstFrameVisible: true,
    postFrameVisible: true,
    firstState: outcomeState(),
    postState: outcomeState({
      objectX: 1.25,
      commandRevision: "2",
      commandAppliedRevision: "2",
      webgpuColorPasses: 2,
      webgpuCompletedColorPasses: 2,
      webgpuCompletedSubmits: 2,
    }),
  }, snapshot));
}

function visibleMetrics(overrides = {}) {
  return Object.assign({
    width: 320,
    height: 180,
    foregroundFraction: 0.2,
    maxDelta: 120,
    restFraction: 0.8,
  }, overrides);
}

function canvasIdentity(engine, overrides = {}) {
  return Object.assign({
    sameMount: true,
    sameState: true,
    sameHandle: true,
    sameCanvas: false,
    samePendingCanvas: true,
    sameRecord: true,
    keys: ["1"],
    disposes: [[engine]],
    renderer: "webgl",
    fallback: "webgpu-device-lost",
    mounted: "true",
    handleReady: true,
    commandReady: "true",
    commandRevision: "1",
    commandAppliedRevision: "1",
    objectX: 1.25,
    glDraws: 2,
    glContext: "webgl2",
  }, overrides);
}

function canvasTransitionEvidence(overrides = {}) {
  return Object.assign({
    acceptedOutcome: "fallback-device-lost",
    fallbackKind: "webgpu-device-lost",
    outcomeVerdict: { accepted: true, reason: "accepted" },
    fallbackReceipt: {
      classification: "device-lost-after-color-pass",
      probeLossCountBaseline: 0,
      probe: { lost: null, lostProbeCount: 1 },
      diagnostics: { deviceLost: false },
    },
    firstRenderState: outcomeState({
      renderer: "webgl", fallback: "webgpu-device-lost", glDraws: 1, glContext: "webgl2",
      webgpuColorPasses: 0, webgpuCompletedColorPasses: 0, webgpuCompletedSubmits: 0,
    }),
    postCommandRenderState: outcomeState({
      renderer: "webgl", fallback: "webgpu-device-lost", objectX: 1.25,
      commandRevision: "2", commandAppliedRevision: "2", glDraws: 2, glContext: "webgl2",
      webgpuColorPasses: 0, webgpuCompletedColorPasses: 0, webgpuCompletedSubmits: 0,
    }),
    firstFrame: { metrics: visibleMetrics() },
    afterHub: { metrics: visibleMetrics() },
    hubCommand: {
      afterX: 1.25,
      sameState: true,
      sameHandle: true,
      commandApplied: true,
      revision: "2",
      appliedRevision: "2",
    },
  }, overrides);
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

test("adapter proof WG outcome classifier accepts exactly native WebGPU, unavailable fallback, and device-loss fallback", () => {
  const native = classifyOutcome();
  assert.equal(native.accepted, true);
  assert.equal(native.hardwareNativeCertified, false, "hosted adapter proof never certifies hardware native WebGPU");

  const unavailable = classifyOutcome({
    nativeCaps: { webgl2: true, webgpu: false },
    acceptedOutcome: "fallback-unavailable",
    fallbackKind: "webgpu-unavailable",
    fallbackReceipt: { phase: "wg first frame", classification: "no-scene-color-pass" },
    firstState: outcomeState({
      renderer: "webgl",
      fallback: "webgpu-unavailable",
      glDraws: 1,
      glContext: "webgl2",
      webgpuColorPasses: 0,
      webgpuCompletedColorPasses: 0,
      webgpuCompletedSubmits: 0,
    }),
    postState: outcomeState({
      renderer: "webgl",
      fallback: "webgpu-unavailable",
      objectX: 1.25,
      commandRevision: "2",
      commandAppliedRevision: "2",
      glDraws: 2,
      glContext: "webgl2",
      webgpuColorPasses: 0,
      webgpuCompletedColorPasses: 0,
      webgpuCompletedSubmits: 0,
    }),
  });
  assert.equal(unavailable.accepted, true);
  assert.equal(unavailable.hardwareNativeCertified, false);

  const deviceLost = classifyOutcome({
    acceptedOutcome: "fallback-device-lost",
    fallbackKind: "webgpu-device-lost",
    fallbackReceipt: {
      phase: "requested-webgpu-first-presentation-readiness",
      classification: "device-lost-before-color-pass",
      probe: { lost: { reason: "destroyed", message: "test loss" } },
      diagnostics: { deviceLost: false },
    },
    firstState: outcomeState({
      renderer: "webgl",
      fallback: "webgpu-device-lost",
      glDraws: 1,
      glContext: "webgl2",
      webgpuColorPasses: 0,
      webgpuCompletedColorPasses: 0,
      webgpuCompletedSubmits: 0,
    }),
    postState: outcomeState({
      renderer: "webgl",
      fallback: "webgpu-device-lost",
      objectX: 1.25,
      commandRevision: "2",
      commandAppliedRevision: "2",
      glDraws: 2,
      glContext: "webgl2",
      webgpuColorPasses: 0,
      webgpuCompletedColorPasses: 0,
      webgpuCompletedSubmits: 0,
    }),
  });
  assert.equal(deviceLost.accepted, true);
  assert.equal(deviceLost.hardwareNativeCertified, false);
});

test("adapter proof source requires WebGL2 but records WebGPU without skipping WG", () => {
  assert.match(browserProof, /if \(!nativeCaps \|\| nativeCaps\.webgl2 !== true\)/);
  assert.doesNotMatch(browserProof, /nativeCaps\.webgpu !== true/);
  assert.match(browserProof, /for \(const c of CASES\) await runCase\(send, c\);/);
  assert.match(browserProof, /hostedPolicy: HOSTED_POLICY/);
});

test("adapter proof WG outcome classifier rejects bad labels, renderers, pixels, command state, and caps", () => {
  const cases = [
    {
      name: "native missing completed pass",
      patch: { postState: outcomeState({ objectX: 1.25, webgpuColorPasses: 2, webgpuCompletedColorPasses: 1 }) },
      reason: "native-webgpu-evidence-missing",
    },
    {
      name: "unavailable while native WebGPU exists",
      patch: {
        acceptedOutcome: "fallback-unavailable",
        fallbackKind: "webgpu-unavailable",
        firstState: outcomeState({ renderer: "webgl", fallback: "webgpu-unavailable", glDraws: 1, glContext: "webgl2" }),
        postState: outcomeState({ renderer: "webgl", fallback: "webgpu-unavailable", glDraws: 2, glContext: "webgl2", objectX: 1.25 }),
      },
      reason: "unavailable-while-native-webgpu-available",
    },
    {
      name: "fallback missing exact label",
      patch: {
        nativeCaps: { webgl2: true, webgpu: false },
        acceptedOutcome: "fallback-unavailable",
        fallbackKind: "",
        firstState: outcomeState({ renderer: "webgl", fallback: "", glDraws: 1, glContext: "webgl2" }),
        postState: outcomeState({ renderer: "webgl", fallback: "", glDraws: 2, glContext: "webgl2", objectX: 1.25 }),
      },
      reason: "fallback-label-mismatch",
    },
    {
      name: "canvas2d fallback",
      patch: {
        nativeCaps: { webgl2: true, webgpu: false },
        acceptedOutcome: "fallback-unavailable",
        fallbackKind: "webgpu-unavailable",
        firstState: outcomeState({ renderer: "canvas2d", fallback: "webgpu-unavailable", glDraws: 1, glContext: "webgl2" }),
        postState: outcomeState({ renderer: "canvas2d", fallback: "webgpu-unavailable", glDraws: 2, glContext: "webgl2", objectX: 1.25 }),
      },
      reason: "fallback-renderer-mismatch",
    },
    {
      name: "blank fallback first pixels",
      patch: {
        nativeCaps: { webgl2: true, webgpu: false },
        acceptedOutcome: "fallback-unavailable",
        fallbackKind: "webgpu-unavailable",
        firstFrameVisible: false,
        firstState: outcomeState({ renderer: "webgl", fallback: "webgpu-unavailable", glDraws: 1, glContext: "webgl2" }),
        postState: outcomeState({ renderer: "webgl", fallback: "webgpu-unavailable", glDraws: 2, glContext: "webgl2", objectX: 1.25 }),
      },
      reason: "fallback-pixels-missing",
    },
    {
      name: "stale command revision",
      patch: {
        nativeCaps: { webgl2: true, webgpu: false },
        acceptedOutcome: "fallback-unavailable",
        fallbackKind: "webgpu-unavailable",
        firstState: outcomeState({ renderer: "webgl", fallback: "webgpu-unavailable", glDraws: 1, glContext: "webgl2" }),
        postState: outcomeState({
          renderer: "webgl",
          fallback: "webgpu-unavailable",
          glDraws: 2,
          glContext: "webgl2",
          objectX: 0,
          commandRevision: "2",
          commandAppliedRevision: "1",
        }),
      },
      reason: "fallback-command-state-stale",
    },
    {
      name: "device loss lacks explicit loss receipt",
      patch: {
        acceptedOutcome: "fallback-device-lost",
        fallbackKind: "webgpu-device-lost",
        fallbackReceipt: { classification: "no-scene-color-pass" },
        firstState: outcomeState({ renderer: "webgl", fallback: "webgpu-device-lost", glDraws: 1, glContext: "webgl2" }),
        postState: outcomeState({ renderer: "webgl", fallback: "webgpu-device-lost", glDraws: 2, glContext: "webgl2", objectX: 1.25 }),
      },
      reason: "device-loss-receipt-missing",
    },
    {
      name: "device loss receipt classification derived only from fallback label",
      patch: {
        acceptedOutcome: "fallback-device-lost",
        fallbackKind: "webgpu-device-lost",
        fallbackReceipt: {
          phase: "requested-webgpu-first-presentation-readiness",
          classification: "device-lost-before-color-pass",
          independentDeviceLoss: false,
          probe: { lost: null },
          diagnostics: { deviceLost: false },
        },
        firstState: outcomeState({ renderer: "webgl", fallback: "webgpu-device-lost", glDraws: 1, glContext: "webgl2" }),
        postState: outcomeState({ renderer: "webgl", fallback: "webgpu-device-lost", glDraws: 2, glContext: "webgl2", objectX: 1.25 }),
      },
      reason: "device-loss-evidence-missing",
    },
    {
      name: "device loss receipt accepts diagnostics evidence independent of label",
      patch: {
        acceptedOutcome: "fallback-device-lost",
        fallbackKind: "webgpu-device-lost",
        fallbackReceipt: {
          phase: "requested-webgpu-first-presentation-readiness",
          classification: "device-lost-after-color-pass",
          independentDeviceLoss: false,
          probe: { lost: null },
          diagnostics: { deviceLost: true },
        },
        firstState: outcomeState({ renderer: "webgl", fallback: "webgpu-device-lost", glDraws: 1, glContext: "webgl2" }),
        postState: outcomeState({ renderer: "webgl", fallback: "webgpu-device-lost", glDraws: 2, glContext: "webgl2", objectX: 1.25 }),
      },
      reason: "",
      accepted: true,
    },
  ];

  for (const entry of cases) {
    const verdict = classifyOutcome(entry.patch);
    assert.equal(verdict.accepted, entry.accepted === true, entry.name);
    assert.equal(verdict.reason, entry.accepted ? "accepted" : entry.reason, entry.name);
  }
});

test("adapter proof fails fast when a typed fallback changes across either capture", () => {
  const classifyInstability = fallbackInstabilityClassifier();
  const c = { name: "wg", webgpu: true };
  const evidence = {
    acceptedOutcome: "fallback-device-lost",
    fallbackKind: "webgpu-device-lost",
    fallbackReceipt: { classification: "device-lost-after-color-pass" },
  };
  const stable = {
    renderer: "webgl",
    fallback: "webgpu-device-lost",
    mounted: "true",
    handleReady: true,
    commandReady: "true",
  };
  assert.equal(classifyInstability(c, evidence, "before-first-capture", stable), null);

  const metrics = { foregroundCoverage: 0.4 };
  const promoted = classifyInstability(c, evidence, "after-first-capture", {
    ...stable,
    renderer: "webgpu",
    fallback: "",
  }, metrics);
  assert.equal(promoted.classification, "fallback-instability");
  assert.equal(promoted.stage, "after-first-capture");
  assert.equal(promoted.expectedFallback, "webgpu-device-lost");
  assert.equal(promoted.currentRenderState.renderer, "webgpu");
  assert.equal(promoted.captureMetrics.foregroundCoverage, 0.4);

  const wrongLabel = classifyInstability(c, evidence, "before-post-command-capture", {
    ...stable,
    fallback: "webgpu-probe-recovered",
  });
  assert.equal(wrongLabel.classification, "fallback-instability");
  assert.equal(wrongLabel.stage, "before-post-command-capture");
  assert.match(browserProof, /assertStableWGTypedFallback\(send, c, evidence, 'after-post-command-capture'/);
});

test("adapter proof defers only an exact WG device-loss canvas transition until the full sticky fallback proof", () => {
  const classifier = canvasTransitionClassifier();
  const c = { name: "wg", webgpu: true, engine: "gosx-engine-adapter-wg" };
  const winner = { renderer: "webgpu", fallback: "" };
  const stale = canvasIdentity(c.engine, { objectX: 0, commandRevision: "", commandAppliedRevision: "" });
  const finalIdentity = canvasIdentity(c.engine, { commandRevision: "2", commandAppliedRevision: "2" });
  const evidence = canvasTransitionEvidence();

  assert.deepEqual(
    { ...classifier.provisionalCanvasTransitionVerdict(c, winner, stale) },
    { accepted: true, reason: "pending-full-device-loss-proof" },
  );
  assert.deepEqual(
    { ...classifier.resolvedCanvasTransitionVerdict(c, winner, stale, finalIdentity, evidence) },
    { accepted: true, reason: "accepted-sticky-device-loss-canvas-transition" },
  );

  const rejected = [
    ["GL case", { ...c, name: "gl", webgpu: false }, winner, stale, finalIdentity, evidence, "not-native-webgpu-transition"],
    ["winner was already fallback", c, { renderer: "webgl", fallback: "webgpu-device-lost" }, stale,
      finalIdentity, evidence, "not-native-webgpu-transition"],
    ["wrong fallback", c, winner, { ...stale, fallback: "webgpu-unavailable" }, finalIdentity,
      evidence, "canvas-transition-not-device-loss-webgl"],
    ["stale object key", c, winner, { ...stale, keys: ["0", "1"] }, finalIdentity,
      evidence, "stale-identity-mismatch"],
    ["missing stale mount identity", c, winner, { ...stale, sameMount: false }, finalIdentity,
      evidence, "stale-identity-mismatch"],
    ["missing state identity", c, winner, { ...stale, sameState: false }, finalIdentity,
      evidence, "stale-identity-mismatch"],
    ["missing stale handle identity", c, winner, { ...stale, sameHandle: false }, finalIdentity,
      evidence, "stale-identity-mismatch"],
    ["missing stale record identity", c, winner, { ...stale, sameRecord: false }, finalIdentity,
      evidence, "stale-identity-mismatch"],
    ["extra disposal", c, winner, { ...stale, disposes: [[c.engine], [c.engine]] }, finalIdentity,
      evidence, "stale-identity-mismatch"],
    ["bare canvas swap", c, winner, { ...stale, renderer: "webgpu", fallback: "" }, finalIdentity,
      evidence, "canvas-transition-not-device-loss-webgl"],
    ["replacement canvas churned", c, winner, stale, { ...finalIdentity, samePendingCanvas: false },
      evidence, "fallback-canvas-not-stable"],
    ["missing final mount identity", c, winner, stale, { ...finalIdentity, sameMount: false },
      evidence, "final-identity-mismatch"],
    ["missing final handle identity", c, winner, stale, { ...finalIdentity, sameHandle: false },
      evidence, "final-identity-mismatch"],
    ["missing final record identity", c, winner, stale, { ...finalIdentity, sameRecord: false },
      evidence, "final-identity-mismatch"],
    ["loss label alone", c, winner, stale, finalIdentity, canvasTransitionEvidence({
      fallbackReceipt: {
        classification: "device-lost-after-color-pass",
        probeLossCountBaseline: 1,
        probe: { lost: null, lostProbeCount: 1 },
        diagnostics: { deviceLost: false },
      },
    }), "accepted-device-loss-proof-missing"],
    ["blank fallback", c, winner, stale, finalIdentity, canvasTransitionEvidence({
      firstFrame: { metrics: visibleMetrics({ foregroundFraction: 0, maxDelta: 0 }) },
    }), "fallback-pixels-missing"],
    ["stale command revision", c, winner, stale, { ...finalIdentity, commandAppliedRevision: "1" },
      evidence, "fallback-command-proof-missing"],
  ];
  for (const [name, testCase, testWinner, testStale, testFinal, testEvidence, reason] of rejected) {
    const verdict = classifier.resolvedCanvasTransitionVerdict(
      testCase, testWinner, testStale, testFinal, testEvidence
    );
    assert.equal(verdict.accepted, false, name);
    assert.equal(verdict.reason, reason, name);
  }
  assert.match(browserProof, /pendingCanvasTransition\.resolution = resolvedCanvasTransitionVerdict/);
  assert.match(browserProof, /window\.__adapterFallbackCanvas = canvas/);
});

test("adapter proof warning classifier keeps unavailable and native outcomes fatal", () => {
  const classifier = warningClassifier();
  classifier.setWarningOccurrences([{
    message: "console.warning: [gosx] WebGPU probe: getContext(webgpu) returned null (context provider unavailable)",
    caseName: "wg",
    phase: "requested-webgpu-first-presentation-readiness",
    source: "Runtime.consoleAPICalled",
  }]);
  classifier.setCases([{
    name: "wg",
    acceptedOutcome: "fallback-unavailable",
    fallbackKind: "webgpu-unavailable",
    fallbackReceipt: { phase: "requested-webgpu-first-presentation-readiness" },
    outcomeVerdict: { accepted: true },
  }]);
  const classified = classifier.classifyWarningsForReport();
  assert.equal(classified.allowed.length, 0);
  assert.equal(classified.unexpected.length, 1);
  assert.equal(classified.unexpected[0].classification.reason, "warning-outcome-mismatch");

  classifier.setCases([{ name: "wg", acceptedOutcome: "native-webgpu", outcomeVerdict: { accepted: true } }]);
  const nativeWarnings = classifier.classifyWarningsForReport();
  assert.equal(nativeWarnings.allowed.length, 0);
  assert.equal(nativeWarnings.unexpected[0].classification.reason, "warning-outcome-mismatch");
});

test("adapter proof warning classifier allows only the exact GL ReadPixels capture-driver warning", () => {
  const classifier = warningClassifier();
  classifier.setCases([]);
  const exact = "browser log warning: GL Driver Message (OpenGL, Performance, GL_CLOSE_PATH_NV, High): GPU stall due to ReadPixels";
  const bracketed = "browser log warning: [.WebGL-0x3a6c046c3600]GL Driver Message (OpenGL, Performance, GL_CLOSE_PATH_NV, High): GPU stall due to ReadPixels";
  classifier.setWarningOccurrences([
    { message: exact, caseName: "gl", phase: "winning-generation-readiness", source: "Log.entryAdded" },
    { message: bracketed, caseName: "gl", phase: "winning-generation-readiness", source: "Log.entryAdded" },
    { message: bracketed + " (this message will no longer repeat)", caseName: "gl", phase: "stale-generation-release", source: "Log.entryAdded" },
    { message: exact, caseName: "wg", phase: "stale-generation-release", source: "Log.entryAdded" },
    { message: exact, caseName: "gl", phase: "first-capture", source: "Log.entryAdded" },
    { message: exact, caseName: "gl", phase: "stale-generation-release", source: "Runtime.consoleAPICalled" },
    { message: exact.replace("ReadPixels", "DrawPixels"), caseName: "gl", phase: "stale-generation-release", source: "Log.entryAdded" },
    { message: "browser log warning: A valid external Instance reference no longer exists", caseName: "gl", phase: "stale-generation-release", source: "Log.entryAdded" },
    { message: "browser log warning: WebGL: CONTEXT_LOST_WEBGL: loseContext: context lost", caseName: "gl", phase: "stale-generation-release", source: "Log.entryAdded" },
  ]);
  const classified = classifier.classifyWarningsForReport();
  assert.equal(classified.allowed.length, 3);
  assert.ok(classified.allowed.every((entry) =>
    entry.classification.reason === "accepted-gl-capture-readpixels-warning"));
  assert.deepEqual(
    Array.from(classified.unexpected, (entry) => entry.classification.reason),
    [
      "capture-driver-warning-case-mismatch",
      "capture-driver-warning-phase-mismatch",
      "capture-driver-warning-source-mismatch",
      "no-accepted-typed-fallback",
      "no-accepted-typed-fallback",
      "no-accepted-typed-fallback",
    ],
  );
});

test("adapter proof owns each exact capability-probe browser notice once and rejects timing/source drift", () => {
  const classifier = warningClassifier();
  const contextLost = "browser log warning: WebGL: CONTEXT_LOST_WEBGL: loseContext: context lost";
  const externalInstance = "browser log warning: A valid external Instance reference no longer exists.";
  classifier.setCases([]);
  classifier.setErrors([]);
  classifier.setWarningOccurrences([
    { message: contextLost, caseName: "caps", phase: "capability-probe", source: "Log.entryAdded" },
    { message: externalInstance, caseName: "caps", phase: "capability-probe", source: "Log.entryAdded" },
  ]);
  let classified = classifier.classifyWarningsForReport();
  assert.equal(classified.allowed.length, 2);
  assert.equal(classified.unexpected.length, 0);
  assert.ok(classified.allowed.every((entry) =>
    entry.classification.reason === "accepted-capability-probe-browser-notice"));

  const classifyOne = (occurrence, errors = []) => {
    classifier.setErrors(errors);
    classifier.setWarningOccurrences([occurrence]);
    const result = classifier.classifyWarningsForReport();
    assert.equal(result.allowed.length, 0);
    assert.equal(result.unexpected.length, 1);
    return result.unexpected[0].classification.reason;
  };
  assert.equal(classifyOne({
    message: contextLost, caseName: "caps", phase: "capability-probe", source: "Runtime.consoleAPICalled",
  }), "capability-probe-warning-source-mismatch");
  assert.equal(classifyOne({
    message: contextLost, caseName: "", phase: "capability-probe", source: "Log.entryAdded",
  }), "capability-probe-warning-case-mismatch");
  assert.equal(classifyOne({
    message: contextLost, caseName: "caps", phase: "", source: "Log.entryAdded",
  }), "capability-probe-warning-phase-mismatch");
  assert.equal(classifyOne({
    message: externalInstance, caseName: "caps", phase: "capability-probe", source: "Log.entryAdded",
  }, ["Runtime.evaluate: capability failure"]), "capability-probe-warning-errors-present");
  assert.equal(classifyOne({
    message: externalInstance.replace("no longer exists", "was destroyed"),
    caseName: "caps", phase: "capability-probe", source: "Log.entryAdded",
  }), "no-accepted-typed-fallback");
  assert.equal(classifyOne({
    message: "", caseName: "caps", phase: "capability-probe", source: "Log.entryAdded",
  }), "no-accepted-typed-fallback");
  assert.equal(classifyOne({
    message: contextLost, caseName: "", phase: "", source: "Log.entryAdded",
  }), "no-accepted-typed-fallback");

  classifier.setErrors([]);
  classifier.setWarningOccurrences([
    { message: contextLost, caseName: "caps", phase: "capability-probe", source: "Log.entryAdded" },
    { message: contextLost, caseName: "caps", phase: "capability-probe", source: "Log.entryAdded" },
  ]);
  classified = classifier.classifyWarningsForReport();
  assert.equal(classified.allowed.length, 0);
  assert.equal(classified.unexpected.length, 2);
  assert.ok(classified.unexpected.every((entry) =>
    entry.classification.reason === "capability-probe-warning-duplicate"));
  assert.match(browserProof, /currentCaseName = 'caps';\n  currentCasePhase = 'capability-probe';/);
});

test("adapter proof warning classifier allows exact WG device-loss warnings only in its bounded window", () => {
  const classifier = warningClassifier();
  classifier.setCases([{
    name: "wg",
    acceptedOutcome: "fallback-device-lost",
    fallbackKind: "webgpu-device-lost",
    fallbackReceipt: { phase: "requested-webgpu-first-presentation-readiness" },
    outcomeVerdict: { accepted: true },
  }]);

  const receiptPhase = "requested-webgpu-first-presentation-readiness";
  classifier.setWarningOccurrences([
    {
      message: "console.warning: [gosx] WebGPU probe device lost: Device was destroyed.",
      caseName: "wg",
      phase: "stale-generation-release",
      source: "Runtime.consoleAPICalled",
    },
    {
      message: "console.warning: [gosx] WebGPU device lost: Device was destroyed.",
      caseName: "wg",
      phase: "stale-generation-release",
      source: "Runtime.consoleAPICalled",
    },
    {
      message: "console.warning: [gosx] WebGPU probe: requestAdapter returned null",
      caseName: "wg",
      phase: receiptPhase,
      source: "Runtime.consoleAPICalled",
    },
    {
      message: "browser log warning: WebGL: CONTEXT_LOST_WEBGL: loseContext: context lost",
      caseName: "wg",
      phase: "stale-generation-release",
      source: "Log.entryAdded",
    },
    {
      message: "browser log warning: A valid external Instance reference no longer exists.",
      caseName: "wg",
      phase: receiptPhase,
      source: "Log.entryAdded",
    },
  ]);
  const classified = classifier.classifyWarningsForReport();
  assert.equal(classified.allowed.length, 5);
  assert.equal(classified.unexpected.length, 0);
  assert.deepEqual(
    Array.from(classified.allowed, (entry) => entry.classification.reason),
    [
      "accepted-typed-fallback-device-loss-console-warning",
      "accepted-typed-fallback-device-loss-console-warning",
      "accepted-typed-fallback-device-loss-console-warning",
      "accepted-typed-fallback-device-loss-browser-notice",
      "accepted-typed-fallback-device-loss-browser-notice",
    ],
  );
  assert.deepEqual(
    Array.from(classified.allowed, (entry) => entry.classification.phase),
    ["stale-generation-release", "stale-generation-release", receiptPhase,
      "stale-generation-release", receiptPhase],
  );
});

test("adapter proof accepts run 33458217937 exact nine-warning artifact timeline under corrected caps ownership", () => {
  const classifier = warningClassifier();
  const receiptPhase = "requested-webgpu-first-presentation-readiness";
  const gl = "browser log warning: [.WebGL-0x1ba404717c00]GL Driver Message (OpenGL, Performance, GL_CLOSE_PATH_NV, High): GPU stall due to ReadPixels";
  classifier.setCases([{
    name: "wg",
    acceptedOutcome: "fallback-device-lost",
    fallbackKind: "webgpu-device-lost",
    fallbackReceipt: { phase: receiptPhase },
    outcomeVerdict: { accepted: true },
  }]);
  classifier.setWarningOccurrences([
    { message: "browser log warning: WebGL: CONTEXT_LOST_WEBGL: loseContext: context lost", caseName: "caps", phase: "capability-probe", source: "Log.entryAdded" },
    { message: "browser log warning: A valid external Instance reference no longer exists.", caseName: "caps", phase: "capability-probe", source: "Log.entryAdded" },
    { message: gl, caseName: "gl", phase: "stale-generation-release", source: "Log.entryAdded" },
    { message: gl, caseName: "gl", phase: "stale-generation-release", source: "Log.entryAdded" },
    { message: gl, caseName: "gl", phase: "stale-generation-release", source: "Log.entryAdded" },
    { message: gl + " (this message will no longer repeat)", caseName: "gl", phase: "stale-generation-release", source: "Log.entryAdded" },
    { message: "browser log warning: A valid external Instance reference no longer exists.", caseName: "wg", phase: "stale-generation-release", source: "Log.entryAdded" },
    { message: "console.warning: [gosx] WebGPU probe device lost: A valid external Instance reference no longer exists.", caseName: "wg", phase: "stale-generation-release", source: "Runtime.consoleAPICalled" },
    { message: "console.warning: [gosx] WebGPU device lost: A valid external Instance reference no longer exists.", caseName: "wg", phase: "stale-generation-release", source: "Runtime.consoleAPICalled" },
  ]);
  const classified = classifier.classifyWarningsForReport();
  assert.equal(classified.total, 9);
  assert.equal(classified.allowed.length, 9);
  assert.equal(classified.unexpected.length, 0);
  const reasons = Array.from(classified.allowed, (entry) => entry.classification.reason);
  assert.equal(reasons.filter((reason) => reason === "accepted-gl-capture-readpixels-warning").length, 4);
  assert.equal(reasons.filter((reason) => reason ===
    "accepted-typed-fallback-device-loss-console-warning").length, 2);
  assert.equal(reasons.filter((reason) => reason ===
    "accepted-typed-fallback-device-loss-browser-notice").length, 1);
  assert.equal(reasons.filter((reason) => reason ===
    "accepted-capability-probe-browser-notice").length, 2);
});

test("adapter proof accepts run 33456783138 exact nine-warning artifact timeline", () => {
  const classifier = warningClassifier();
  const receiptPhase = "requested-webgpu-first-presentation-readiness";
  const gl = "browser log warning: [.WebGL-0x3a6c046c3600]GL Driver Message (OpenGL, Performance, GL_CLOSE_PATH_NV, High): GPU stall due to ReadPixels";
  classifier.setCases([{
    name: "wg",
    acceptedOutcome: "fallback-device-lost",
    fallbackKind: "webgpu-device-lost",
    fallbackReceipt: { phase: receiptPhase },
    outcomeVerdict: { accepted: true },
  }]);
  classifier.setWarningOccurrences([
    { message: gl, caseName: "gl", phase: "winning-generation-readiness", source: "Log.entryAdded" },
    { message: gl, caseName: "gl", phase: "stale-generation-release", source: "Log.entryAdded" },
    { message: gl, caseName: "gl", phase: "stale-generation-release", source: "Log.entryAdded" },
    { message: gl + " (this message will no longer repeat)", caseName: "gl", phase: "stale-generation-release", source: "Log.entryAdded" },
    { message: "browser log warning: WebGL: CONTEXT_LOST_WEBGL: loseContext: context lost", caseName: "wg", phase: "stale-generation-release", source: "Log.entryAdded" },
    { message: "console.warning: [gosx] WebGPU probe device lost: Device was destroyed.", caseName: "wg", phase: "stale-generation-release", source: "Runtime.consoleAPICalled" },
    { message: "console.warning: [gosx] WebGPU device lost: Device was destroyed.", caseName: "wg", phase: "stale-generation-release", source: "Runtime.consoleAPICalled" },
    { message: "browser log warning: A valid external Instance reference no longer exists.", caseName: "wg", phase: "stale-generation-release", source: "Log.entryAdded" },
    { message: "console.warning: [gosx] WebGPU probe: requestAdapter returned null", caseName: "wg", phase: "stale-generation-release", source: "Runtime.consoleAPICalled" },
  ]);
  const classified = classifier.classifyWarningsForReport();
  assert.equal(classified.total, 9);
  assert.equal(classified.allowed.length, 9);
  assert.equal(classified.unexpected.length, 0);
  assert.deepEqual(
    Array.from(classified.allowed, (entry) => entry.classification.reason),
    [
      "accepted-gl-capture-readpixels-warning",
      "accepted-gl-capture-readpixels-warning",
      "accepted-gl-capture-readpixels-warning",
      "accepted-gl-capture-readpixels-warning",
      "accepted-typed-fallback-device-loss-browser-notice",
      "accepted-typed-fallback-device-loss-console-warning",
      "accepted-typed-fallback-device-loss-console-warning",
      "accepted-typed-fallback-device-loss-browser-notice",
      "accepted-typed-fallback-device-loss-console-warning",
    ],
  );
});

test("adapter proof warning classifier rejects wrong WG device-loss ownership and altered evidence", () => {
  const classifier = warningClassifier();
  const receiptPhase = "requested-webgpu-first-presentation-readiness";
  const accepted = {
    name: "wg",
    acceptedOutcome: "fallback-device-lost",
    fallbackKind: "webgpu-device-lost",
    fallbackReceipt: { phase: receiptPhase },
    outcomeVerdict: { accepted: true },
  };
  const consoleWarning = "console.warning: [gosx] WebGPU probe device lost: Device was destroyed.";
  const browserNotice = "browser log warning: A valid external Instance reference no longer exists.";
  const classifyOne = (occurrence, cases = [accepted], errors = []) => {
    classifier.setErrors(errors);
    classifier.setCases(cases);
    classifier.setWarningOccurrences([occurrence]);
    const result = classifier.classifyWarningsForReport();
    assert.equal(result.allowed.length, 0);
    assert.equal(result.unexpected.length, 1);
    return result.unexpected[0].classification.reason;
  };

  assert.equal(classifyOne({
    message: consoleWarning,
    caseName: "gl",
    phase: "stale-generation-release",
    source: "Runtime.consoleAPICalled",
  }), "warning-case-mismatch");
  assert.equal(classifyOne({
    message: consoleWarning,
    caseName: "",
    phase: "stale-generation-release",
    source: "Runtime.consoleAPICalled",
  }), "warning-case-mismatch");
  assert.equal(classifyOne({
    message: consoleWarning,
    caseName: "wg",
    phase: "winning-generation-readiness",
    source: "Runtime.consoleAPICalled",
  }), "warning-phase-outside-accepted-device-loss-window");
  assert.equal(classifyOne({
    message: consoleWarning,
    caseName: "wg",
    phase: "stale-generation-release",
    source: "Log.entryAdded",
  }), "warning-source-mismatch");
  assert.equal(classifyOne({
    message: browserNotice,
    caseName: "wg",
    phase: receiptPhase,
    source: "Runtime.evaluate",
  }), "warning-source-mismatch");
  assert.equal(classifyOne({
    message: consoleWarning.replace("device lost", "device maybe lost"),
    caseName: "wg",
    phase: "stale-generation-release",
    source: "Runtime.consoleAPICalled",
  }), "not-in-exact-fallback-warning-allowlist");
  assert.equal(classifyOne({
    message: browserNotice.replace("no longer exists", "was recycled"),
    caseName: "wg",
    phase: "stale-generation-release",
    source: "Log.entryAdded",
  }), "not-in-exact-fallback-warning-allowlist");
  assert.equal(classifyOne({
    message: "console.error: [gosx] WebGPU device lost: Device was destroyed.",
    caseName: "wg",
    phase: "stale-generation-release",
    source: "Runtime.consoleAPICalled",
  }), "not-in-exact-fallback-warning-allowlist");

  for (const rejectedCase of [
    { ...accepted, acceptedOutcome: "native-webgpu", fallbackKind: "" },
    { ...accepted, acceptedOutcome: "fallback-unavailable", fallbackKind: "webgpu-unavailable" },
    { ...accepted, outcomeVerdict: { accepted: false } },
    { ...accepted, fallbackReceipt: null },
  ]) {
    assert.equal(classifyOne({
      message: consoleWarning,
      caseName: "wg",
      phase: "stale-generation-release",
      source: "Runtime.consoleAPICalled",
    }, [rejectedCase]), "warning-outcome-mismatch");
  }

  assert.equal(classifyOne({
    message: consoleWarning,
    caseName: "wg",
    phase: "stale-generation-release",
    source: "Runtime.consoleAPICalled",
  }, [accepted], ["Runtime.evaluate: product failure"]), "warning-outcome-mismatch");

  classifier.setErrors([]);
  classifier.setCases([]);
  classifier.setWarningOccurrences([{
    message: consoleWarning,
    caseName: "wg",
    phase: "stale-generation-release",
    source: "Runtime.consoleAPICalled",
  }]);
  const missing = classifier.classifyWarningsForReport();
  assert.equal(missing.allowed.length, 0);
  assert.equal(missing.unexpected[0].classification.reason, "no-accepted-typed-fallback");
});

test("adapter proof warning classifier keeps exact device-loss notices cross-case and out-of-window fatal", () => {
  const classifier = warningClassifier();
  classifier.setCases([{
    name: "wg",
    acceptedOutcome: "fallback-device-lost",
    fallbackKind: "webgpu-device-lost",
    fallbackReceipt: { phase: "requested-webgpu-first-presentation-readiness" },
    outcomeVerdict: { accepted: true },
  }]);
  classifier.setWarningOccurrences([{
    message: "browser log warning: WebGL: CONTEXT_LOST_WEBGL: loseContext: context lost",
    caseName: "gl",
    phase: "stale-generation-release",
    source: "Log.entryAdded",
  }]);
  let classified = classifier.classifyWarningsForReport();
  assert.equal(classified.allowed.length, 0);
  assert.equal(classified.unexpected[0].classification.reason, "warning-case-mismatch");

  classifier.setWarningOccurrences([{
    message: "browser log warning: WebGL: CONTEXT_LOST_WEBGL: loseContext: context lost",
    caseName: "wg",
    phase: "post-command-capture",
    source: "Log.entryAdded",
  }]);
  classified = classifier.classifyWarningsForReport();
  assert.equal(classified.allowed.length, 0);
  assert.equal(classified.unexpected[0].classification.reason,
    "warning-phase-outside-accepted-device-loss-window");
});

test("WebGPU browser-proof readiness exits promptly on device-loss fallback before queue fence", async () => {
  const buildExpression = presentExpressionBuilder();
  const buildReceipt = failureReceiptExpressionBuilder();
  const { poll, state } = readinessPollHelper();
  const fixture = browserContext();
  const c = { mount: "scene3d-adapter-hydrate-browser-test" };

  const stillWebGPUFixture = browserContext();
  stillWebGPUFixture.encoder.beginRenderPass({ colorAttachments: [{ view: {} }] }).end();
  stillWebGPUFixture.queue.submit([]);
  stillWebGPUFixture.probe.lost = { reason: "destroyed", message: "test loss before renderer swap" };
  stillWebGPUFixture.diagnostics.deviceLost = true;

  await assert.rejects(
    poll(runtimeSendFor(stillWebGPUFixture.context), buildExpression(c, 0, 0, undefined, false),
      "wg first frame submitted scene color frame", 500),
    (caught) => {
      assert.equal(caught.terminal, true);
      assert.equal(caught.classification, "device-lost-after-color-pass");
      assert.equal(caught.lastPredicate.ready, false);
      assert.equal(caught.lastPredicate.terminal, true);
      assert.equal(caught.lastPredicate.predicates.rendererWebGPU, true);
      assert.equal(caught.lastPredicate.predicates.freshColorPass, true);
      assert.equal(caught.lastPredicate.predicates.deviceLost, true);
      return true;
    }
  );
  assert.equal(stillWebGPUFixture.onSubmittedWorkDoneCalls(), 0,
    "explicit device loss while renderer still says WebGPU must not arm the capture-time queue fence");
  assert.equal(stillWebGPUFixture.pendingSubmittedWork(), 0);

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

test("WebGPU browser-proof rejected WG states preserve independent failure receipt", async () => {
  const rejectedWGOutcome = rejectedWGOutcomeHelper();
  const result = await rejectedWGOutcome(
    async () => { throw new Error("unexpected send"); },
    { name: "wg", mount: "scene3d-adapter-hydrate-browser-test" },
    "wg first frame",
    { renderer: "webgpu", fallback: "webgpu-probe-recovered" },
  );

  assert.equal(result.acceptedOutcome, "");
  assert.equal(result.fallbackKind, "webgpu-probe-recovered");
  assert.equal(result.fallbackEvidence, null);
  assert.equal(result.webgpuFailureReceipt.classification, "device-lost-after-color-pass");
  assert.equal(result.webgpuFailureReceipt.independentDeviceLoss, true);
  assert.equal(result.webgpuFailureReceipt.mount.fallback, "webgpu-probe-recovered");
});

test("WebGPU browser proof requires a safe current-case probe-loss counter delta", () => {
  const buildBaseline = probeLossCountExpressionBuilder();
  const buildReceipt = failureReceiptExpressionBuilder();
  const fixture = browserContext();
  const c = { mount: "scene3d-adapter-hydrate-browser-test", probeLossCountBaseline: 0 };

  const baseline = vm.runInContext(buildBaseline(), fixture.context);
  assert.deepEqual({ ...baseline }, { valid: true, count: 0 });

  fixture.context.__adapterWGLatestQueue = new fixture.context.GPUQueue();
  fixture.probe.lostProbeCount = 1;
  const currentLoss = vm.runInContext(buildReceipt(c, "stale-generation-release"), fixture.context);
  assert.equal(currentLoss.classification, "device-lost-before-color-pass",
    "a current-case loss delta must outrank the generic instrumentation mismatch");
  assert.equal(currentLoss.independentDeviceLoss, true);
  assert.equal(currentLoss.instrumentedQueueOrDeviceMismatch, true,
    "loss-first classification must retain mismatch diagnostics");
  assert.equal(currentLoss.probeLossCountBaseline, 0);
  assert.equal(currentLoss.probeLossCountDelta, 1);

  const staleCount = vm.runInContext(buildReceipt({ ...c, probeLossCountBaseline: 1 },
    "stale-generation-release"), fixture.context);
  assert.equal(staleCount.independentDeviceLoss, false,
    "an absolute nonzero counter inherited from an earlier case is not current loss evidence");
  assert.equal(staleCount.probeLossCountDelta, 0);
  assert.equal(staleCount.classification, "instrumented-queue-or-device-mismatch");

  const inheritedFixture = browserContext();
  const inheritedProbe = Object.create({ lostProbeCount: 1 });
  Object.assign(inheritedProbe, inheritedFixture.probe);
  delete inheritedProbe.lostProbeCount;
  inheritedFixture.context.__gosx_scene3d_webgpu_probe = () => inheritedProbe;
  let rejected = vm.runInContext(buildReceipt(c, "stale-generation-release"), inheritedFixture.context);
  assert.equal(rejected.independentDeviceLoss, false, "an inherited loss counter must fail closed");
  assert.equal(rejected.probe.lostProbeCount, null);

  const accessorFixture = browserContext();
  Object.defineProperty(accessorFixture.probe, "lostProbeCount", { configurable: true, get() { return 1; } });
  rejected = vm.runInContext(buildReceipt(c, "stale-generation-release"), accessorFixture.context);
  assert.equal(rejected.independentDeviceLoss, false, "an accessor loss counter must fail closed");
  assert.equal(rejected.probe.lostProbeCount, null);

  for (const value of [-1, 1.5, Number.NaN, Number.POSITIVE_INFINITY, "1", null]) {
    const malformedFixture = browserContext();
    malformedFixture.probe.lostProbeCount = value;
    const malformed = vm.runInContext(buildReceipt(c, "stale-generation-release"), malformedFixture.context);
    assert.equal(malformed.independentDeviceLoss, false, "malformed counter " + String(value));
    assert.equal(malformed.probeLossCountDelta, 0, "malformed counter delta " + String(value));
  }
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
