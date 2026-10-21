const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const source = fs.readFileSync(path.join(__dirname, "engine-worker.ts"), "utf8");

const tick = () => new Promise(resolve => setImmediate(resolve));
const plain = v => JSON.parse(JSON.stringify(v));

// ---------------------------------------------------------------------------
// Page half
// ---------------------------------------------------------------------------

class FakeWorker {
  constructor(url, options) {
    if (FakeWorker.throwOnCreate) throw new Error(FakeWorker.throwOnCreate);
    this.url = url; this.options = options; this.sent = []; this.terminated = false;
    FakeWorker.all.push(this);
  }
  postMessage(msg, transfer) { this.sent.push({ msg, transfer: transfer || [] }); }
  terminate() { this.terminated = true; }
  // Test helpers: what the worker script would do.
  say(msg) { this.onmessage && this.onmessage({ data: msg }); }
  of(type) { return this.sent.filter(s => s.msg.t === type).map(s => s.msg); }
}
FakeWorker.all = [];

function bootPage(opts = {}) {
  FakeWorker.all = []; FakeWorker.throwOnCreate = opts.throwOnCreate || "";
  const issues = [], events = [], docEvents = [], listeners = {};
  const win = {
    __gosx: {
      reportIssue: i => issues.push(i),
      document: { get: () => ({ assets: { runtime: { standardGoWasmExecPath: "/gosx/assets/runtime/standard-go-wasm_exec.abc.js", bootstrapFeatureEngineWorkerPath: "/gosx/assets/runtime/bootstrap-feature-engine-worker.def.js" } } }) },
    },
    __gosx_emit: (level, scope, message, detail) => events.push({ level, scope, message, detail }),
    Worker: opts.noWorker ? undefined : FakeWorker,
    __gosx_go_wasm_factory_timeout_ms: opts.timeout || 0,
  };
  const document = {
    hidden: false, baseURI: "https://app.test/play/",
    currentScript: { src: "https://app.test/gosx/assets/runtime/bootstrap-feature-engine-worker.def.js" },
    addEventListener: (name, fn) => { listeners[name] = fn; },
    dispatchEvent: e => docEvents.push(e),
    getElementById: id => (opts.mounts && opts.mounts[id]) || null,
  };
  class CustomEvent { constructor(type, init) { this.type = type; this.detail = init && init.detail; } }
  const context = vm.createContext({
    window: win, document, location: { href: "https://app.test/play/" }, console, URL, CustomEvent, Worker: win.Worker,
    performance: { timeOrigin: 1000, now: () => 5 }, TextDecoder, TextEncoder, ArrayBuffer, Uint8Array, JSON, Promise, Map, Set, Math, Number, String, Object, Array, Error,
    setTimeout, clearTimeout, isFinite,
  });
  let factory;
  win.__gosx_register_bootstrap_feature = (name, f) => { assert.equal(name, "engine-worker"); factory = f; };
  vm.runInContext(source, context, { filename: "engine-worker.ts" });
  assert.equal(typeof factory, "function");
  const feature = factory({});
  return { win, document, issues, events, docEvents, listeners, host: win.__gosx.engineWorker, feature, context };
}

const ENTRY = { id: "gosx-engine-0", component: "Sim", kind: "worker", programRef: "/gosx/engines/sim.wasm", runtime: "go-wasm", thread: "worker", props: { workerFrameHz: 30, seed: 7 }, capabilities: ["wasm"] };

function fakeFallback(tag = "main") {
  const calls = [];
  const fn = async () => { calls.push(1); return { factory: ctx => ({ ran: tag, ctx }), moduleRecord: { state: "ready", tag } }; };
  fn.calls = calls;
  return fn;
}

function fakeCtx(extra = {}) {
  const calls = [];
  const ctx = {
    isCurrent: () => !ctx.gone, calls,
    setSignalsJSON: (text, changedOnly) => calls.push(["setSignalsJSON", text, changedOnly]),
    setSignal: (name, value) => calls.push(["setSignal", name, value]),
    getSignal: name => (name === "$score" ? { p1: 2 } : undefined),
    subscribeSignalJSON: (name, cb) => { ctx.sub = { name, cb }; return () => { ctx.unsub = true; }; },
    scene3D: (method, target, ...args) => { calls.push(["scene3D", method, target, args]); return Promise.resolve({ ok: method }); },
    navigate: (target, options) => { calls.push(["navigate", target, options]); return Promise.resolve(true); },
    emit: (name, detail) => calls.push(["emit", name, detail]),
    ...extra,
  };
  return ctx;
}

async function bootedRecord(page, entry = ENTRY, pending = {}) {
  const fallback = fakeFallback();
  const resolved = page.host.resolve(entry, pending, fallback);
  await tick();
  const worker = FakeWorker.all[0];
  worker.say({ t: "booted", v: 1 });
  const out = await resolved;
  return { worker, out, fallback };
}

async function mounted(page, entry = ENTRY, ctxExtra = {}) {
  const { worker, out } = await bootedRecord(page, entry);
  const ctx = fakeCtx(ctxExtra);
  const handleP = out.factory(ctx);
  await tick();
  worker.say({ t: "mounted" });
  const handle = await handleP;
  return { worker, ctx, handle, out };
}

test("registers the engine-worker feature and publishes the host", () => {
  const page = bootPage();
  assert.equal(page.host.version, 1);
  assert.equal(page.host.protocol, 1);
  assert.equal(page.host.supported(), true);
  assert.equal(typeof page.feature.disposePage, "function");
});

test("starts a Worker from the feature's own URL and sends absolute URLs in init", async () => {
  const page = bootPage();
  const { worker, out, fallback } = await bootedRecord(page);
  assert.equal(worker.url, "https://app.test/gosx/assets/runtime/bootstrap-feature-engine-worker.def.js");
  assert.equal(worker.options.name, "gosx-engine-gosx-engine-0");
  const [init] = worker.of("init");
  assert.equal(init.v, 1);
  assert.equal(init.programRef, "https://app.test/gosx/engines/sim.wasm");
  assert.equal(init.wasmExec, "https://app.test/gosx/assets/runtime/standard-go-wasm_exec.abc.js");
  assert.equal(init.component, "Sim");
  assert.deepEqual(plain(init.props), { workerFrameHz: 30, seed: 7 });
  assert.equal(init.frameHz, 30);
  assert.equal(init.hidden, false);
  assert.equal(init.clock.epochMs, 1005);
  assert.equal(typeof out.factory, "function");
  assert.equal(out.moduleRecord, null);
  assert.equal(fallback.calls.length, 0, "a started worker must not boot the main-thread module");
});

test("mount runs the Go factory in the worker and dispose shuts it down", async () => {
  const page = bootPage();
  const { worker, ctx, handle } = await mounted(page);
  assert.equal(ctx.threadMode, "worker");
  assert.equal(worker.of("mount").length, 1);
  handle.dispose();
  assert.equal(worker.of("dispose").length, 1);
  assert.equal(worker.terminated, false, "the worker gets a moment to run Go dispose");
  worker.say({ t: "disposed" });
  assert.equal(worker.terminated, true);
  assert.deepEqual(page.host.ids(), []);
});

test("a worker that never acknowledges dispose is terminated after the grace period", async () => {
  const page = bootPage();
  const { worker, handle } = await mounted(page);
  handle.dispose();
  await new Promise(r => setTimeout(r, 300));
  assert.equal(worker.terminated, true);
});

test("a mount error terminates the worker and rejects the factory", async () => {
  const page = bootPage();
  const { worker, out } = await bootedRecord(page);
  const handleP = out.factory(fakeCtx());
  await tick();
  worker.say({ t: "fatal", code: "worker-mount-failed", message: "boom" });
  await assert.rejects(handleP, /boom/);
  assert.equal(worker.terminated, true);
});

test("falls back to the main thread and reports when Worker creation throws", async () => {
  const page = bootPage({ throwOnCreate: "SecurityError: blocked" });
  const fallback = fakeFallback();
  const out = await page.host.resolve(ENTRY, {}, fallback);
  assert.equal(fallback.calls.length, 1);
  assert.equal(out.moduleRecord.tag, "main");
  const ctx = {};
  const handle = out.factory(ctx);
  assert.equal(handle.ran, "main");
  assert.equal(ctx.threadMode, "main");
  assert.equal(ctx.threadFallbackReason, "worker-create-failed");
  assert.equal(page.issues.length, 1);
  assert.equal(page.issues[0].code, "worker-create-failed");
  assert.equal(page.issues[0].fallback, "main-thread");
  assert.match(page.issues[0].message, /main thread/);
  assert.equal(page.events[0].level, "warn");
  assert.equal(page.docEvents[0].type, "gosx:engine:thread-fallback");
  assert.equal(page.docEvents[0].detail.code, "worker-create-failed");
});

test("falls back when the browser has no Worker, when disabled, and for TinyGo", async () => {
  for (const [label, opts, entry, code] of [
    ["no Worker", { noWorker: true }, ENTRY, "worker-unsupported"],
    ["disabled", {}, ENTRY, "worker-disabled"],
    ["tinygo", {}, { ...ENTRY, toolchain: "tinygo" }, "worker-toolchain-unsupported"],
  ]) {
    const page = bootPage(opts);
    if (label === "disabled") page.win.__gosx_engine_worker_disable = true;
    const fallback = fakeFallback();
    const out = await page.host.resolve(entry, {}, fallback);
    const ctx = {};
    out.factory(ctx);
    assert.equal(ctx.threadFallbackReason, code, label);
    assert.equal(FakeWorker.all.length, 0, label + ": no Worker may start");
    assert.equal(fallback.calls.length, 1, label);
  }
});

test("falls back when the worker script fails to load", async () => {
  const page = bootPage();
  const fallback = fakeFallback();
  const resolved = page.host.resolve(ENTRY, {}, fallback);
  await tick();
  FakeWorker.all[0].onerror({ message: "404 for worker script", preventDefault() {} });
  const out = await resolved;
  const ctx = {};
  out.factory(ctx);
  assert.equal(ctx.threadFallbackReason, "worker-script-error");
  assert.equal(FakeWorker.all[0].terminated, true);
});

test("falls back when the worker reports a fatal boot error or a protocol mismatch", async () => {
  for (const [msg, code] of [
    [{ t: "fatal", code: "worker-fetch-status", message: "status 404" }, "worker-fetch-status"],
    [{ t: "booted", v: 99 }, "worker-protocol"],
  ]) {
    const page = bootPage();
    const resolved = page.host.resolve(ENTRY, {}, fakeFallback());
    await tick();
    FakeWorker.all[0].say(msg);
    const out = await resolved;
    const ctx = {};
    out.factory(ctx);
    assert.equal(ctx.threadFallbackReason, code);
  }
});

test("falls back when the worker does not boot in time", async () => {
  const page = bootPage({ timeout: 20 });
  const resolved = page.host.resolve(ENTRY, {}, fakeFallback());
  const out = await resolved;
  const ctx = {};
  out.factory(ctx);
  assert.equal(ctx.threadFallbackReason, "worker-boot-timeout");
  assert.equal(FakeWorker.all[0].terminated, true);
});

test("a pending engine disposed before mount terminates the worker", async () => {
  const page = bootPage();
  const pending = {};
  await bootedRecord(page, ENTRY, pending);
  assert.ok(pending.workerRecord, "the host must hand the record to the mount path");
  pending.workerRecord.terminate();
  assert.equal(FakeWorker.all[0].terminated, true);
  assert.deepEqual(page.host.ids(), []);
});

test("a worker crash after mount is reported and ends the worker", async () => {
  const page = bootPage();
  const { worker } = await mounted(page);
  worker.onerror({ message: "out of memory", preventDefault() {} });
  assert.equal(worker.terminated, true);
  assert.equal(page.issues.at(-1).code, "worker-crashed");
  assert.equal(page.docEvents.at(-1).type, "gosx:engine:worker-error");
});

test("signal batches arrive as transferred bytes and apply through the context", async () => {
  const page = bootPage();
  const { worker, ctx } = await mounted(page);
  const bytes = new TextEncoder().encode('{"$score":{"p1":3}}');
  worker.say({ t: "signals", changedOnly: true, buf: bytes.buffer });
  assert.deepEqual(ctx.calls.at(-1), ["setSignalsJSON", '{"$score":{"p1":3}}', true]);
  worker.say({ t: "signals", changedOnly: false, buf: new TextEncoder().encode("{}").buffer });
  assert.deepEqual(ctx.calls.at(-1), ["setSignalsJSON", "{}", false]);
  assert.equal(page.host.get(ENTRY.id).stats().signalBatches, 2);
  ctx.gone = true;
  const before = ctx.calls.length;
  worker.say({ t: "signals", buf: bytes.buffer });
  assert.equal(ctx.calls.length, before, "a disposed context receives nothing");
});

test("a rejected signal batch is reported back to the worker, not thrown", async () => {
  const page = bootPage();
  const { worker } = await mounted(page, ENTRY, { setSignalsJSON() { throw new Error("signal batch must be an object"); } });
  worker.say({ t: "signals", buf: new TextEncoder().encode("[]").buffer });
  const [fault] = worker.of("fault");
  assert.equal(fault.where, "signals");
  assert.match(fault.message, /must be an object/);
});

test("signal subscriptions prime the worker and forward later writes", async () => {
  const page = bootPage();
  const { worker, ctx } = await mounted(page);
  worker.say({ t: "subscribe", sid: 4, name: "$score" });
  const signals = worker.of("signal");
  assert.equal(signals.length, 1);
  assert.deepEqual({ sid: signals[0].sid, initial: signals[0].initial, json: signals[0].json }, { sid: 4, initial: true, json: '{"p1":2}' });
  ctx.sub.cb('{"p1":5}', "$score");
  assert.equal(worker.of("signal").at(-1).json, '{"p1":5}');
  worker.say({ t: "unsubscribe", sid: 4 });
  assert.equal(ctx.unsub, true);
});

test("binary frames reach the Scene3D entry points without copying", async () => {
  const dispatched = [];
  const page = bootPage();
  page.win.__gosx.scene3d = {
    dispatchPoseFrame: (t, b, o) => { dispatched.push(["pose", t, b, o]); return Promise.resolve(); },
    dispatchMotionFrame: (t, b, o) => { dispatched.push(["motion", t, b, o]); return Promise.resolve(); },
    dispatchClipFrame: (t, b, o) => { dispatched.push(["clip", t, b, o]); return Promise.resolve(); },
  };
  const applied = [];
  const mount = { __gosxScene3DHandle: { applyInstanceStream: b => { applied.push(b); return { applied: true }; } } };
  page.document.getElementById = id => (id === "arena" ? mount : null);
  const { worker } = await mounted(page);
  const pose = new Uint8Array([1, 2, 3]).buffer;
  worker.say({ t: "frame", kind: "pose", target: "arena", buf: pose });
  const motion = new Uint8Array([4, 5]).buffer;
  const fb = new Uint8Array([9]).buffer;
  worker.say({ t: "frame", kind: "motion", target: "arena", buf: motion, options: { fallbackPoseFrame: fb } });
  worker.say({ t: "frame", kind: "clip", target: "arena", buf: new Uint8Array([6]).buffer });
  worker.say({ t: "frame", kind: "instance", target: "arena", buf: new Uint8Array([7, 7]).buffer });
  assert.deepEqual(dispatched.map(d => d[0]), ["pose", "motion", "clip"]);
  assert.equal(dispatched[0][1], "arena");
  assert.equal(dispatched[0][2].buffer, pose, "the page view must share the transferred buffer");
  assert.equal(dispatched[1][3].fallbackPoseFrame.buffer, fb);
  assert.deepEqual(Array.from(applied[0]), [7, 7]);
  const stats = page.host.get(ENTRY.id).stats();
  assert.equal(stats.framesIn, 4);
  assert.equal(stats.frameBytes, 3 + 2 + 1 + 2);
});

test("a rejected frame dispatch is reported as an engine event", async () => {
  const page = bootPage();
  page.win.__gosx.scene3d = { dispatchPoseFrame: () => Promise.reject(new Error("stale revision")) };
  const { worker } = await mounted(page);
  const origError = console.error; console.error = () => {};
  try {
    worker.say({ t: "frame", kind: "pose", target: "arena", buf: new Uint8Array([1]).buffer });
    await tick();
  } finally { console.error = origError; }
  const event = page.docEvents.find(e => e.type === "gosx:engine:worker-frame-error");
  assert.ok(event);
  assert.equal(event.detail.message, "stale revision");
});

test("scene3d and navigate requests run on the page and reply", async () => {
  const page = bootPage();
  const { worker, ctx } = await mounted(page);
  const mount = { id: "arena" };
  page.document.getElementById = id => (id === "arena" ? mount : null);
  worker.say({ t: "scene3d", rid: 11, method: "dispatchCommands", target: "arena", args: [[{ kind: "x" }]] });
  worker.say({ t: "navigate", rid: 12, target: "/next", options: { replace: true } });
  await tick();
  assert.deepEqual(ctx.calls.find(c => c[0] === "scene3D").slice(1, 3), ["dispatchCommands", mount]);
  assert.deepEqual(plain(worker.of("reply").map(r => [r.rid, r.ok, r.value])), [[11, true, { ok: "dispatchCommands" }], [12, true, true]]);
  ctx.gone = true;
  worker.say({ t: "scene3d", rid: 13, method: "whenReady", target: "arena", args: [] });
  const last = worker.of("reply").at(-1);
  assert.deepEqual([last.rid, last.ok], [13, false]);
});

test("visibility changes are forwarded with an epoch timestamp", async () => {
  const page = bootPage();
  const { worker } = await mounted(page);
  page.document.hidden = true;
  page.listeners.visibilitychange();
  const [msg] = worker.of("visibility");
  assert.equal(msg.hidden, true);
  assert.equal(msg.at, 1005);
});

test("application messages go both ways: JSON, bytes, and a document event", async () => {
  const page = bootPage();
  const { worker } = await mounted(page);
  const api = page.host.get(ENTRY.id);
  const seen = [];
  api.onMessage("hud", (json, data) => seen.push([json, data && data.byteLength]));
  worker.say({ t: "app", name: "hud", json: '{"hp":9}', buf: new Uint8Array([1, 2]).buffer });
  assert.deepEqual(plain(seen), [[{ hp: 9 }, 2]]);
  assert.equal(page.docEvents.at(-1).type, "gosx:engine:message");
  api.post("pause", { on: true }, new Uint8Array([5, 6, 7]));
  const sent = worker.sent.at(-1);
  assert.equal(sent.msg.name, "pause");
  assert.equal(sent.msg.json, '{"on":true}');
  assert.equal(sent.transfer.length, 1);
});

test("disposePage terminates every worker", async () => {
  const page = bootPage();
  const { worker } = await mounted(page);
  page.feature.disposePage();
  assert.equal(worker.terminated, true);
});

// ---------------------------------------------------------------------------
// Worker half
// ---------------------------------------------------------------------------

function bootWorker(opts = {}) {
  const posted = [];
  let clock = 0;
  const timers = new Map();
  let nextTimer = 1;
  const scope = {
    postMessage: (msg, transfer) => posted.push({ msg, transfer: transfer || [] }),
    performance: { timeOrigin: 5000, now: () => clock },
    setTimeout: (fn, ms) => { const id = nextTimer++; timers.set(id, { fn, at: clock + ms }); return id; },
    clearTimeout: id => timers.delete(id),
    console, TextEncoder, TextDecoder, Uint32Array, ArrayBuffer, Uint8Array, Object, Array, Promise, JSON, Map, Set, Math, Number, String, Error,
    crypto: { getRandomValues: a => { for (let i = 0; i < a.length; i++) a[i] = i + 1; return a; } },
    WebAssembly: { instantiate: async () => ({ instance: { fake: true } }) },
    fetch: async url => { scope.fetched = url; return { ok: opts.status === undefined, status: opts.status || 200, clone() { return this; }, arrayBuffer: async () => new ArrayBuffer(8) }; },
    importScripts: url => {
      scope.imported = url;
      if (opts.noCtor) return;
      scope.__gosx_standard_go_wasm_ctor = class {
        constructor() { this.importObject = {}; this.env = {}; }
        run() {
          scope.goEnv = this.env;
          if (!opts.noRegister) scope.__gosx_register_go_wasm_engine_factory(this.env.GOSX_GO_WASM_REGISTRATION_TOKEN, "Sim", ctx => { scope.factoryCtx = ctx; return { dispose() { scope.disposed = true; } }; });
          return new Promise(() => {});
        }
      };
    },
  };
  scope.self = scope;
  const context = vm.createContext(scope);
  vm.runInContext(source, context, { filename: "engine-worker.ts" });
  const send = msg => scope.onmessage({ data: msg });
  const advance = ms => {
    const end = clock + ms;
    for (;;) {
      let next = null;
      for (const [id, t] of timers) if (t.at <= end && (!next || t.at < next.t.at)) next = { id, t };
      if (!next) break;
      timers.delete(next.id); clock = Math.max(clock, next.t.at); next.t.fn();
    }
    clock = end;
  };
  return { scope, posted, send, advance, setClock: v => { clock = v; }, of: t => posted.filter(p => p.msg.t === t) };
}

const INIT = { t: "init", v: 1, id: "e0", component: "Sim", kind: "worker", programRef: "https://app.test/sim.wasm", wasmExec: "https://app.test/wasm_exec.js", runtimeMode: "go-wasm", props: { a: 1 }, capabilities: ["wasm"], requiredCapabilities: [], hidden: false, clock: { epochMs: 5100 }, frameHz: 60 };

async function bootedWorker(opts) {
  const w = bootWorker(opts);
  w.send(INIT);
  await tick(); await tick(); await tick();
  return w;
}

test("worker: loads wasm_exec, runs the module with a registration token, and reports booted", async () => {
  const w = await bootedWorker();
  assert.equal(w.scope.imported, INIT.wasmExec);
  assert.equal(w.scope.fetched, INIT.programRef);
  assert.equal(w.scope.goEnv.GOSX_GO_WASM_REGISTRATION_TOKEN, "00000001000000020000000300000004");
  assert.deepEqual(plain(w.of("booted").map(p => p.msg)), [{ t: "booted", v: 1 }]);
});

test("worker: reports a fatal error for a missing standard Go runtime, a bad status and no registration", async () => {
  for (const [opts, code] of [[{ noCtor: true }, "worker-wasm-exec-missing"], [{ status: 404 }, "worker-fetch-status"]]) {
    const w = bootWorker(opts);
    w.send(INIT);
    await tick(); await tick(); await tick();
    const fatal = w.of("fatal")[0];
    assert.ok(fatal, code);
    assert.equal(fatal.msg.code, code);
    assert.equal(w.of("booted").length, 0);
  }
});

test("worker: the registration function rejects other tokens and components", async () => {
  const w = bootWorker({ noRegister: true });
  w.send(INIT);
  await tick(); await tick();
  const register = w.scope.__gosx_register_go_wasm_engine_factory;
  assert.equal(register("wrong", "Sim", () => {}), false);
  assert.equal(register(w.scope.goEnv.GOSX_GO_WASM_REGISTRATION_TOKEN, "Other", () => {}), false);
  assert.equal(register(w.scope.goEnv.GOSX_GO_WASM_REGISTRATION_TOKEN, "Sim", () => {}), true);
});

async function mountedWorker(opts) {
  const w = await bootedWorker(opts);
  w.send({ t: "mount" });
  await tick(); await tick();
  return w;
}

test("worker: mount builds the context Go expects", async () => {
  const w = await mountedWorker();
  assert.equal(w.of("mounted").length, 1);
  const ctx = w.scope.factoryCtx;
  assert.equal(ctx.id, "e0");
  assert.equal(ctx.threadMode, "worker");
  assert.equal(ctx.mount, null);
  assert.deepEqual(plain(ctx.props), { a: 1 });
  assert.equal(ctx.isCurrent(), true);
  assert.equal(w.scope.__gosx.worker.hidden, false);
});

test("worker: signal batches leave as transferred bytes, single writes as JSON", async () => {
  const w = await mountedWorker();
  const ctx = w.scope.factoryCtx;
  ctx.setSignalsJSON('{"$a":1}', true);
  const batch = w.of("signals")[0];
  assert.equal(batch.msg.changedOnly, true);
  assert.equal(new TextDecoder().decode(batch.msg.buf), '{"$a":1}');
  assert.equal(batch.transfer[0], batch.msg.buf, "the buffer must be transferred, not copied");
  ctx.setSignal("$b", { x: 2 });
  assert.deepEqual(plain(w.of("signal.set")[0].msg), { t: "signal.set", name: "$b", json: '{"x":2}' });
  assert.deepEqual(plain(ctx.getSignal("$b")), { x: 2 });
});

test("worker: subscriptions get page values and mirror them for reads", async () => {
  const w = await mountedWorker();
  const ctx = w.scope.factoryCtx;
  const seen = [];
  const off = ctx.subscribeSignalJSON("$score", (json, name) => seen.push([json, name]));
  const sid = w.of("subscribe")[0].msg.sid;
  w.send({ t: "signal", sid, name: "$score", json: '{"p1":2}', initial: true });
  assert.deepEqual(plain(ctx.getSignal("$score")), { p1: 2 });
  assert.deepEqual(seen, [], "the priming value is not a change");
  w.send({ t: "signal", sid, name: "$score", json: '{"p1":3}' });
  assert.deepEqual(seen, [['{"p1":3}', "$score"]]);
  off();
  assert.equal(w.of("unsubscribe").length, 1);
  w.send({ t: "signal", sid, name: "$score", json: '{"p1":4}' });
  assert.equal(seen.length, 1);
});

test("worker: scene3D and navigate are request/reply, and the abort option never crosses", async () => {
  const w = await mountedWorker();
  const ctx = w.scope.factoryCtx;
  const p = ctx.scene3D("dispatchCommands", "arena", [{ k: 1 }], { signal: {} });
  const req = w.of("scene3d")[0].msg;
  assert.deepEqual(plain({ method: req.method, target: req.target, args: req.args }), { method: "dispatchCommands", target: "arena", args: [[{ k: 1 }]] });
  w.send({ t: "reply", rid: req.rid, ok: true, value: { done: 1 } });
  assert.deepEqual(plain(await p), { done: 1 });
  const nav = ctx.navigate("/next", { replace: true });
  w.send({ t: "reply", rid: w.of("navigate")[0].msg.rid, ok: false, error: "blocked" });
  await assert.rejects(nav, /blocked/);
  assert.equal(ctx.scene3D("getCamera", "arena"), null);
  assert.throws(() => ctx.scene3D("whenReady", { mount: true }), /mount id as a string/);
});

test("worker: frame dispatch shims transfer owned buffers and copy views", async () => {
  const w = await mountedWorker();
  const api = w.scope.__gosx.scene3d;
  const owned = new Uint8Array([1, 2, 3]);
  const result = await api.dispatchPoseFrame("arena", owned, { fallbackCommands: [{ k: 1 }] });
  assert.deepEqual(plain(result), { queued: true });
  let frame = w.of("frame")[0];
  assert.equal(frame.msg.kind, "pose");
  assert.equal(frame.msg.buf, owned.buffer);
  assert.equal(frame.transfer[0], owned.buffer);
  assert.deepEqual(plain(frame.msg.options), { fallbackCommands: [{ k: 1 }] });
  const big = new Uint8Array([9, 8, 7, 6, 5]);
  const view = big.subarray(1, 3);
  api.dispatchClipFrame("arena", view);
  frame = w.of("frame")[1];
  assert.notEqual(frame.msg.buf, big.buffer, "a view of a larger buffer is copied so the sender's bytes survive");
  assert.deepEqual(Array.from(new Uint8Array(frame.msg.buf)), [8, 7]);
  const fb = new Uint8Array([4]);
  api.dispatchMotionFrame("arena", new Uint8Array([1]), { fallbackPoseFrame: fb });
  frame = w.of("frame")[2];
  assert.equal(frame.transfer.length, 2);
  assert.equal(frame.msg.options.fallbackPoseFrame, fb.buffer);
  const r = api.applyInstanceStream("arena", new Uint8Array([5]));
  assert.equal(w.of("frame")[3].msg.kind, "instance");
  assert.equal(r.applied, true);
});

test("worker: visibility updates hidden and notifies listeners", async () => {
  const w = await mountedWorker();
  const seen = [];
  w.scope.__gosx.worker.onVisibility(h => seen.push(h));
  w.send({ t: "visibility", hidden: true, at: 1 });
  assert.equal(w.scope.__gosx.worker.hidden, true);
  w.send({ t: "visibility", hidden: false, at: 2 });
  assert.deepEqual(seen, [true, false]);
});

test("worker: app messages flow to registered handlers, and out to the page", async () => {
  const w = await mountedWorker();
  const seen = [];
  w.scope.__gosx.worker.onMessage((name, json, bytes) => seen.push([name, json, bytes && bytes.length]));
  w.send({ t: "app", name: "pause", json: "true", buf: new Uint8Array([1, 2]).buffer });
  assert.deepEqual(seen, [["pause", "true", 2]]);
  w.scope.__gosx.worker.post("hud", '{"hp":1}');
  w.scope.__gosx.worker.postBytes("blob", new Uint8Array([3]));
  assert.equal(w.of("app").length, 2);
  assert.equal(w.of("app")[1].transfer.length, 1);
});

test("worker: dispose runs the Go handle, stops the clock and acknowledges", async () => {
  const w = await mountedWorker();
  let ticked = 0;
  w.scope.__gosx.worker.requestFrame(() => ticked++);
  w.send({ t: "dispose" });
  assert.equal(w.scope.disposed, true);
  assert.equal(w.scope.factoryCtx.isCurrent(), false);
  assert.equal(w.of("disposed").length, 1);
  w.advance(100);
  assert.equal(ticked, 0, "a stopped clock fires nothing");
});

test("worker: the frame clock runs on a fixed grid at the requested rate", async () => {
  const w = await mountedWorker();
  const api = w.scope.__gosx.worker;
  const stamps = [];
  const loop = now => { stamps.push(now); api.requestFrame(loop); };
  api.setFrameRate(50);
  w.setClock(3);
  api.requestFrame(loop);
  w.advance(1000);
  assert.equal(api.frameRate(), 50);
  assert.ok(stamps.length >= 49 && stamps.length <= 51, "50 Hz for 1 s gave " + stamps.length);
  const gaps = stamps.slice(1).map((s, i) => s - stamps[i]);
  assert.ok(gaps.every(g => Math.abs(g - 20) < 1e-6), "gaps stay on the 20 ms grid: " + gaps.slice(0, 5));
});

test("worker: a late timer skips slots instead of bursting", async () => {
  const w = await mountedWorker();
  const api = w.scope.__gosx.worker;
  const stamps = [];
  const loop = now => { stamps.push(now); if (stamps.length < 4) api.requestFrame(loop); };
  api.requestFrame(loop);
  w.advance(17);                 // first slot
  assert.equal(stamps.length, 1);
  w.setClock(500);               // the thread was starved for ~480 ms
  w.advance(40);
  assert.ok(stamps.length >= 2);
  assert.ok(api.frameStats().frames >= 2);
  assert.ok(stamps.length <= 4, "no catch-up burst");
});

test("worker: cancelFrame prevents the callback and bad rates are refused", async () => {
  const w = await mountedWorker();
  const api = w.scope.__gosx.worker;
  let called = 0;
  const h = api.requestFrame(() => called++);
  api.cancelFrame(h);
  w.advance(100);
  assert.equal(called, 0);
  assert.equal(api.setFrameRate(0), false);
  assert.equal(api.setFrameRate(5000), false);
  assert.equal(api.frameRate(), 60);
});
