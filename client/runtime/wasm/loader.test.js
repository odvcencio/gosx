const test = require("node:test");
const assert = require("node:assert/strict");
const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const compatibilitySource = fs.readFileSync(path.join(__dirname, "../host/compatibility.ts"), "utf8");
const generatedSource = fs.readFileSync(path.join(__dirname, "../generated/runtime-abi.ts"), "utf8");
const abiSource = fs.readFileSync(path.join(__dirname, "abi.ts"), "utf8");
const loaderSource = fs.readFileSync(path.join(__dirname, "loader.ts"), "utf8");

function makeEnvironment(options) {
  options = options || {};
  const bytes = Uint8Array.from([0, 97, 115, 109, 1, 0, 0, 0]);
  const hash = crypto.createHash("sha256").update(bytes).digest("hex").slice(0, 16);
  let hydrationReady = 0;
  let runCalls = 0;
  const window = {
    crypto: crypto.webcrypto,
    __gosx: { reportIssue: function() {} },
    __gosx_runtime_ready_timeout_ms: 10,
    __gosx_runtime_ready: function() { hydrationReady++; },
  };
  const response = {
    ok: true,
    status: 200,
    clone: function() { return { arrayBuffer: async function() { return bytes.buffer; } }; },
    arrayBuffer: async function() { return bytes.buffer; },
  };
  function Go() { this.importObject = {}; }
  Go.prototype.run = function() {
    runCalls++;
    if (options.batchFunction) window.__gosx_set_input_batch = options.batchFunction;
    if (options.runFailure) return Promise.reject(options.runFailure);
    if (options.skipReady) return new Promise(function() {});
    const contract = window.__gosx_runtime_contract;
    const handshake = Object.assign({
      abiVersion: contract.abiVersion,
      featureMask: contract.compatibilityVariants.islands,
      variant: "islands",
      mailboxVersion: contract.mailboxVersion,
      manifestHash: contract.manifestHash,
    }, options.handshake || {});
    window.__gosx.runtime.abi = { handshake: function() { return handshake; } };
    window.__gosx_runtime_ready();
    return new Promise(function() {});
  };
  const context = {
    window: window,
    Go: Go,
    fetch: async function() { return response; },
    WebAssembly: {
      instantiateStreaming: async function() { return { instance: { exports: options.exports } }; },
      instantiate: async function() { return { instance: { exports: options.exports } }; },
    },
    Uint8Array: Uint8Array,
    ArrayBuffer: ArrayBuffer,
    Object: Object,
    Number: Number,
    String: String,
    Promise: Promise,
    Error: Error,
    console: { error: function() {} },
    setTimeout: setTimeout,
  };
  vm.createContext(context);
  vm.runInContext(compatibilitySource, context);
  vm.runInContext(generatedSource, context);
  vm.runInContext(abiSource, context);
  vm.runInContext(loaderSource, context);
  const contract = window.__gosx_runtime_contract;
  return {
    window: window,
    reference: {
      path: "/runtime.wasm",
      hash: hash,
      manifestHash: contract.manifestHash,
      variant: "islands",
      featureMask: contract.compatibilityVariants.islands,
    },
    runtime: window.__gosx.runtime,
    hydrationReady: function() { return hydrationReady; },
    runCalls: function() { return runCalls; },
  };
}

test("verified loader forwards readiness only after exact handshake validation", async () => {
  const env = makeEnvironment();
  await env.runtime.loader.load(env.reference);
  assert.equal(env.runCalls(), 1);
  assert.equal(env.hydrationReady(), 1);
});

test("browser selector chooses the smallest published compatible artifact", () => {
  const env = makeEnvironment();
  const selected = env.runtime.support.selectVariant(17, {
    core: { size: 5 },
    full: { size: 40 },
  });
  assert.equal(selected, "core");
});

test("loader rejects an artifact hash mismatch before execution", async () => {
  const env = makeEnvironment();
  env.reference.hash = "0000000000000000";
  await assert.rejects(env.runtime.loader.load(env.reference), /asset hash mismatch/);
  assert.equal(env.runCalls(), 0);
  assert.equal(env.hydrationReady(), 0);
});

test("loader rejects variant drift without releasing hydration", async () => {
  const env = makeEnvironment({ handshake: { variant: "core", featureMask: 17 } });
  await assert.rejects(env.runtime.loader.load(env.reference), /variant does not match/);
  assert.equal(env.hydrationReady(), 0);
});

test("loader rejects a feature mask that contradicts its variant", async () => {
  const env = makeEnvironment({ handshake: { featureMask: 1 } });
  await assert.rejects(env.runtime.loader.load(env.reference), /feature mask does not match/);
  assert.equal(env.hydrationReady(), 0);
});

test("loader rejects stale manifest identity", async () => {
  const env = makeEnvironment({ handshake: { manifestHash: "stale" } });
  await assert.rejects(env.runtime.loader.load(env.reference), /manifest identity mismatch/);
  assert.equal(env.hydrationReady(), 0);
});

test("loader rejects runtime exit and missing readiness", async () => {
  const exited = makeEnvironment({ runFailure: new Error("runtime trap") });
  await assert.rejects(exited.runtime.loader.load(exited.reference), /runtime trap/);
  assert.equal(exited.hydrationReady(), 0);

  const stalled = makeEnvironment({ skipReady: true });
  await assert.rejects(stalled.runtime.loader.load(stalled.reference), /did not signal readiness/);
  assert.equal(stalled.hydrationReady(), 0);
});

test("loader hangs the runtime's exports on the batch function once the handshake holds", async () => {
  const exports = { gosx_batch_apply: function() { return 0; } };
  const batch = function() { return null; };
  const env = makeEnvironment({ exports: exports, batchFunction: batch });
  assert.equal(batch.exports, undefined);
  await env.runtime.loader.load(env.reference);
  assert.equal(env.window.__gosx_set_input_batch, batch);
  assert.equal(batch.exports, exports, "the engine context sends signal batches through these");

  // A runtime that fails the handshake is not used, so its exports are not published.
  const refused = function() { return null; };
  const bad = makeEnvironment({ exports: exports, batchFunction: refused, handshake: { manifestHash: "stale" } });
  await assert.rejects(bad.runtime.loader.load(bad.reference), /manifest identity mismatch/);
  assert.equal(refused.exports, undefined);
});

test("loader runs a runtime that has no batch function or no exports", async () => {
  const noFunction = makeEnvironment({ exports: {} });
  await noFunction.runtime.loader.load(noFunction.reference);
  assert.equal(noFunction.window.__gosx_set_input_batch, undefined);

  const batch = function() { return null; };
  const noExports = makeEnvironment({ batchFunction: batch });
  await noExports.runtime.loader.load(noExports.reference);
  assert.equal(batch.exports, undefined);
});
