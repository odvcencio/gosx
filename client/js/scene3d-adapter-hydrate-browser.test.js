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
  const start = browserProof.indexOf("function webGPUPresentExpr(c, afterColorPasses, afterCompletedColorPasses, expectedObjectX) {");
  const end = browserProof.indexOf("\nasync function waitForWebGPUPresentation", start);
  assert.ok(start >= 0 && end > start, "browser proof WebGPU readiness helper must remain extractable");
  return vm.runInNewContext(browserProof.slice(start, end) + "; webGPUPresentExpr");
}

function browserContext() {
  let resolveSubmittedWork;
  function GPUCommandEncoder() {}
  GPUCommandEncoder.prototype.beginRenderPass = function () { return { end() {} }; };
  function GPURenderPassEncoder() {}
  GPURenderPassEncoder.prototype.draw = function () {};
  GPURenderPassEncoder.prototype.drawIndexed = function () {};
  function GPUQueue() {}
  GPUQueue.prototype.submit = function () {};
  GPUQueue.prototype.onSubmittedWorkDone = function () {
    return new Promise((resolve) => { resolveSubmittedWork = resolve; });
  };

  const mount = {
    attributes: {
      "data-gosx-scene3d-renderer": "webgpu",
      "data-gosx-scene3d-mounted": "true",
      "data-gosx-scene3d-webgpu-mesh-objects": "1",
      "data-gosx-scene3d-webgpu-mesh-draw-calls": "1",
      "data-gosx-scene3d-webgpu-mesh-view-culled": "0",
    },
    getAttribute(name) { return this.attributes[name] || null; },
  };
  const context = { GPUCommandEncoder, GPURenderPassEncoder, GPUQueue, mount };
  context.window = context;
  context.document = { getElementById() { return mount; } };
  mount.__gosxScene3DState = { objects: new Map([["1", { x: 0 }]]) };
  vm.createContext(context);
  vm.runInContext(preloadSource(), context, { filename: "scene3d-adapter-hydrate-browser-preload.js" });
  return {
    context,
    encoder: new context.GPUCommandEncoder(),
    queue: new context.GPUQueue(),
    mount,
    resolveSubmittedWork() { resolveSubmittedWork(); },
  };
}

async function settlePromiseCallbacks() {
  await Promise.resolve();
  await Promise.resolve();
}

test("WebGPU browser-proof readiness rejects depth-only and pre-command color submits", async () => {
  const buildExpression = presentExpressionBuilder();
  const fixture = browserContext();
  const c = { mount: "scene3d-adapter-hydrate-browser-test" };

  fixture.encoder.beginRenderPass({ colorAttachments: [] }).end();
  fixture.queue.submit([]);
  fixture.resolveSubmittedWork();
  await settlePromiseCallbacks();

  assert.equal(fixture.context.__adapterWGPasses, 1);
  assert.equal(fixture.context.__adapterWGColorPasses, 0);
  assert.equal(fixture.context.__adapterWGCompletedColorPasses, 0);
  assert.equal(vm.runInContext(buildExpression(c, 0, 0), fixture.context), null,
    "the depth-only dummy-shadow pass must never make a blank first frame ready");

  fixture.encoder.beginRenderPass({ colorAttachments: [{ view: {} }] }).end();
  fixture.queue.submit([]);
  fixture.resolveSubmittedWork();
  await settlePromiseCallbacks();

  const ready = vm.runInContext(buildExpression(c, 0, 0), fixture.context);
  assert.equal(ready.colorPasses, 1);
  assert.equal(ready.completedSubmits, 2);
  assert.equal(ready.meshDrawCalls, "1");

  // Model an old-state rAF that completes before command application. The
  // production harness samples this baseline atomically only after observing
  // x=1.25, so this pass is intentionally included in the baseline.
  fixture.encoder.beginRenderPass({ colorAttachments: [{ view: {} }] }).end();
  fixture.queue.submit([]);
  fixture.resolveSubmittedWork();
  await settlePromiseCallbacks();
  const postCommandBaseline = fixture.context.__adapterWGColorPasses;
  fixture.mount.__gosxScene3DState.objects.get("1").x = 1.25;

  const completedPostCommandBaseline = fixture.context.__adapterWGCompletedColorPasses;
  assert.equal(vm.runInContext(buildExpression(c, postCommandBaseline, completedPostCommandBaseline, 1.25), fixture.context), null,
    "a completed pre-command color pass must not satisfy the post-command capture gate");

  fixture.encoder.beginRenderPass({ colorAttachments: [{ view: {} }] }).end();
  fixture.queue.submit([]);
  fixture.resolveSubmittedWork();
  await settlePromiseCallbacks();

  const postCommandReady = vm.runInContext(
    buildExpression(c, postCommandBaseline, completedPostCommandBaseline, 1.25), fixture.context
  );
  assert.equal(postCommandReady.colorPasses, postCommandBaseline + 1);
  assert.equal(postCommandReady.objectX, 1.25);

  fixture.mount.__gosxScene3DState.objects.get("1").x = 0;
  assert.equal(vm.runInContext(
    buildExpression(c, postCommandBaseline, completedPostCommandBaseline, 1.25), fixture.context
  ), null,
    "a visible frame with stale command state must not satisfy the post-command capture gate");
});
