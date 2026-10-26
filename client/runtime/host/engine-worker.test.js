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
  const issues = [], events = [], docEvents = [], listeners = {}, winListeners = {}, signalWrites = [];
  const fire = (name, event = {}, target = "document") => { for (const fn of (((target === "window" ? winListeners : listeners)[name]) || []).slice()) fn(event); };
  const win = {
    __gosx: {
      reportIssue: i => issues.push(i),
      document: { get: () => ({ assets: { runtime: { standardGoWasmExecPath: "/gosx/assets/runtime/standard-go-wasm_exec.abc.js", bootstrapFeatureEngineWorkerPath: "/gosx/assets/runtime/bootstrap-feature-engine-worker.def.js" } } }) },
    },
    __gosx_emit: (level, scope, message, detail) => events.push({ level, scope, message, detail }),
    __gosx_set_shared_signal: (name, json) => { signalWrites.push([name, JSON.parse(json)]); return ""; },
    Worker: opts.noWorker ? undefined : FakeWorker,
    __gosx_go_wasm_factory_timeout_ms: opts.timeout || 0,
    addEventListener: (name, fn) => { (winListeners[name] = winListeners[name] || []).push(fn); },
    removeEventListener: (name, fn) => { winListeners[name] = (winListeners[name] || []).filter(f => f !== fn); },
    navigator: opts.navigator,
  };
  const document = {
    hidden: false, baseURI: "https://app.test/play/",
    currentScript: { src: "https://app.test/gosx/assets/runtime/bootstrap-feature-engine-worker.def.js" },
    addEventListener: (name, fn) => { (listeners[name] = listeners[name] || []).push(fn); },
    removeEventListener: (name, fn) => { listeners[name] = (listeners[name] || []).filter(f => f !== fn); },
    dispatchEvent: e => docEvents.push(e),
    getElementById: id => (opts.mounts && opts.mounts[id]) || null,
  };
  class CustomEvent { constructor(type, init) { this.type = type; this.detail = init && init.detail; } }
  const context = vm.createContext({
    window: win, document, location: { href: "https://app.test/play/" }, console, URL, CustomEvent, Worker: win.Worker,
    performance: { timeOrigin: 1000, now: () => 5 }, TextDecoder, TextEncoder, ArrayBuffer, Uint8Array, JSON, Promise, Map, Set, Math, Number, String, Object, Array, Error,
    setTimeout, clearTimeout, setInterval, clearInterval, isFinite, Array, Set,
  });
  let factory;
  win.__gosx_register_bootstrap_feature = (name, f) => { assert.equal(name, "engine-worker"); factory = f; };
  vm.runInContext(source, context, { filename: "engine-worker.ts" });
  assert.equal(typeof factory, "function");
  const feature = factory({});
  return { win, document, signalWrites, issues, events, docEvents, listeners, winListeners, fire, host: win.__gosx.engineWorker, feature, context };
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
  page.fire("visibilitychange");
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

test("disposePage shuts mounted workers down through Go, and ends ones that never mounted", async () => {
  const page = bootPage();
  const { worker } = await mounted(page);
  page.feature.disposePage();
  assert.equal(worker.of("dispose").length, 1, "a mounted engine runs its Go Dispose");
  worker.say({ t: "disposed" });
  assert.equal(worker.terminated, true);
  const booting = bootPage();
  const { worker: unmounted } = await bootedRecord(booting);
  booting.feature.disposePage();
  assert.equal(unmounted.terminated, true, "an engine that never mounted has nothing to dispose");
});

test("disposePage leaves the engines the next page reuses running", async () => {
  const page = bootPage();
  const { worker } = await mounted(page);
  page.feature.disposePage(new Set([ENTRY.id]));
  assert.equal(worker.of("dispose").length, 0);
  assert.equal(worker.terminated, false);
  assert.deepEqual(page.host.ids(), [ENTRY.id]);
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
          const mount = ctx => { scope.factoryCtx = ctx; return { dispose() { scope.disposed = true; } }; };
          if (opts.register) opts.register(scope, this.env.GOSX_GO_WASM_REGISTRATION_TOKEN, mount);
          else if (!opts.noRegister) scope.__gosx_register_go_wasm_engine_factory(this.env.GOSX_GO_WASM_REGISTRATION_TOKEN, "Sim", mount);
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

test("worker: a multi-component module registers before and after the requested factory", async () => {
  const accepted = [];
  const w = bootWorker({ register(scope, token, mount) {
    for (const name of ["Other", "Sim", "Alias"]) {
      const ok = scope.__gosx_register_go_wasm_engine_factory(token, name, name === "Sim" ? mount : () => { throw Error("wrong factory selected"); });
      accepted.push(ok);
      if (!ok) throw Error("Go Register would reject " + name);
    }
  } });
  w.send(INIT);
  await tick(); await tick(); await tick();
  assert.deepEqual(accepted, [true, true, true]);
  assert.equal(w.of("booted").length, 1);
  w.send({ t: "mount" });
  await tick(); await tick();
  assert.equal(w.of("mounted").length, 1);
  assert.equal(w.scope.factoryCtx.component, "Sim");
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

test("worker: registration is token-scoped, unique and limited to synchronous module startup", async () => {
  const results = [];
  const w = bootWorker({ register(scope, token, mount) {
    const register = scope.__gosx_register_go_wasm_engine_factory;
    results.push(register("wrong", "Sim", mount));
    results.push(register(token, "", mount));
    results.push(register(token, "Sim", null));
    results.push(register(token, "Other", () => {}));
    results.push(register(token, "Other", () => {}));
    results.push(register(token, "Sim", mount));
    results.push(register(token, "Sim", () => {}));
  } });
  w.send(INIT);
  await tick(); await tick();
  assert.deepEqual(results, [false, false, false, true, false, true, false]);
  assert.equal(w.of("booted").length, 1);
  const register = w.scope.__gosx_register_go_wasm_engine_factory;
  assert.equal(register("wrong", "Sim", () => {}), false);
  assert.equal(register(w.scope.goEnv.GOSX_GO_WASM_REGISTRATION_TOKEN, "Late", () => {}), false);
  assert.equal(register(w.scope.goEnv.GOSX_GO_WASM_REGISTRATION_TOKEN, "Sim", () => {}), false);
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

test("worker: a timer that fires early still yields one frame per slot", async () => {
  const w = await mountedWorker();
  const api = w.scope.__gosx.worker;
  // Make every timer fire 0.4 ms before its due time, as browsers sometimes do.
  const realSet = w.scope.setTimeout;
  w.scope.setTimeout = (fn, ms) => realSet(fn, Math.max(0, ms - 0.4));
  let count = 0;
  const loop = () => { count++; if (count < 500) api.requestFrame(loop); };
  api.requestFrame(loop);
  w.advance(1000);
  assert.ok(count >= 58 && count <= 61, "60 Hz for 1 s gave " + count);
});

// ---------------------------------------------------------------------------
// Scene3D mount access
// ---------------------------------------------------------------------------

function fakeMount() {
  const mount = {
    events: [], listeners: new Map(), calls: [],
    dispatchEvent(e) { this.events.push(e); const fn = this.listeners.get(e.type); if (fn) { e.target = this; fn(e); } return true; },
    addEventListener(type, fn) { this.listeners.set(type, fn); },
    removeEventListener(type) { this.listeners.delete(type); },
    __gosxScene3DHandle: { updateSceneProps(p) { mount.calls.push(p); } },
  };
  return mount;
}

test("page: mount.op dispatches only gosx:scene3d: events and a short call list", async () => {
  const page = bootPage();
  const mount = fakeMount();
  page.document.getElementById = id => (id === "arena" ? mount : null);
  const { worker } = await mounted(page);
  worker.say({ t: "mount.op", target: "arena", op: "dispatchEvent", name: "gosx:scene3d:commands", json: '{"revision":1}' });
  assert.equal(mount.events.length, 1);
  assert.equal(mount.events[0].type, "gosx:scene3d:commands");
  assert.deepEqual(plain(mount.events[0].detail), { revision: 1 });
  worker.say({ t: "mount.op", target: "arena", op: "call", name: "updateSceneProps", json: '{"maxFrameRate":30}' });
  assert.deepEqual(plain(mount.calls), [{ maxFrameRate: 30 }]);
  for (const bad of [
    { op: "dispatchEvent", name: "click", json: "{}" },
    { op: "dispatchEvent", name: "gosx:scene3d:../x", json: "{}" },
    { op: "call", name: "dispose", json: "{}" },
    { op: "evalThis", name: "x" },
  ]) worker.say({ t: "mount.op", target: "arena", ...bad });
  assert.equal(mount.events.length, 1, "no disallowed event may reach the page");
  assert.equal(mount.calls.length, 1);
  assert.equal(worker.of("fault").length, 4);
  worker.say({ t: "mount.op", target: "gone", op: "dispatchEvent", name: "gosx:scene3d:commands", json: "{}" });
  assert.match(worker.of("fault").at(-1).message, /not found/);
});

test("page: mount.listen forwards events for that mount only, and unlisten or dispose stops them", async () => {
  const page = bootPage();
  const mount = fakeMount();
  page.document.getElementById = id => (id === "arena" ? mount : null);
  const { worker, handle } = await mounted(page);
  worker.say({ t: "mount.listen", lid: 5, target: "arena", name: "gosx:scene3d:frame-submitted" });
  mount.dispatchEvent({ type: "gosx:scene3d:frame-submitted", detail: { requestID: "r" } });
  assert.deepEqual(worker.of("mount.event").map(m => [m.lid, m.json]), [[5, '{"requestID":"r"}']]);
  mount.listeners.get("gosx:scene3d:frame-submitted")({ type: "x", target: {}, detail: {} });
  assert.equal(worker.of("mount.event").length, 1, "a bubbled event from a child is ignored");
  worker.say({ t: "mount.listen", lid: 6, target: "arena", name: "click" });
  assert.equal(mount.listeners.has("click"), false, "only scene3d events may be observed");
  worker.say({ t: "mount.unlisten", lid: 5 });
  assert.equal(mount.listeners.size, 0);
  worker.say({ t: "mount.listen", lid: 7, target: "arena", name: "gosx:scene3d:frame-submitted" });
  assert.equal(mount.listeners.size, 1);
  handle.dispose();
  worker.say({ t: "disposed" });
  assert.equal(mount.listeners.size, 0, "terminating the worker removes its listeners");
});

test("worker: mountOp and mountListen post to the page and deliver events back", async () => {
  const w = await mountedWorker();
  const api = w.scope.__gosx.worker;
  api.mountOp("arena", "dispatchEvent", "gosx:scene3d:commands", '{"a":1}');
  assert.deepEqual(plain(w.of("mount.op")[0].msg), { t: "mount.op", target: "arena", op: "dispatchEvent", name: "gosx:scene3d:commands", json: '{"a":1}' });
  const seen = [];
  const off = api.mountListen("arena", "gosx:scene3d:frame-submitted", json => seen.push(json));
  const lid = w.of("mount.listen")[0].msg.lid;
  w.send({ t: "mount.event", lid, json: '{"x":1}' });
  assert.deepEqual(seen, ['{"x":1}']);
  off(); off();
  assert.equal(w.of("mount.unlisten").length, 1);
  w.send({ t: "mount.event", lid, json: "{}" });
  assert.equal(seen.length, 1);
});

// ---------------------------------------------------------------------------
// Input forwarding
// ---------------------------------------------------------------------------

const withCaps = (...caps) => ({ ...ENTRY, capabilities: caps });
const inputs = worker => worker.of("input").map(m => m.ev);

test("page: keyboard events reach the worker with epoch timestamps; repeats and text fields are skipped", async () => {
  const page = bootPage();
  const { worker } = await mounted(page, withCaps("keyboard"));
  page.fire("keydown", { type: "keydown", code: "KeyA", key: "a", timeStamp: 40, target: { tagName: "DIV" } });
  page.fire("keydown", { type: "keydown", code: "KeyA", key: "a", repeat: true, timeStamp: 50, target: {} });
  page.fire("keydown", { type: "keydown", code: "KeyB", key: "b", timeStamp: 60, target: { tagName: "INPUT" } });
  page.fire("keyup", { type: "keyup", code: "KeyA", key: "a", timeStamp: 90, target: {} });
  assert.deepEqual(plain(inputs(worker)), [
    { k: "key", code: "KeyA", key: "a", down: true, ts: 1040 },
    { k: "key", code: "KeyA", key: "a", down: false, ts: 1090 },
  ]);
  assert.equal(page.host.get(ENTRY.id).stats().inputSent, 2);
});

test("page: pointer events carry position, buttons and the pointer id", async () => {
  const page = bootPage();
  const { worker } = await mounted(page, withCaps("pointer"));
  page.fire("pointerdown", { type: "pointerdown", clientX: 10.5, clientY: 20, buttons: 1, pointerId: 3, timeStamp: 12 });
  page.fire("pointermove", { type: "pointermove", clientX: 11, clientY: 21, buttons: 1, pointerId: 3, timeStamp: 18 });
  page.fire("pointerup", { type: "pointerup", clientX: 11, clientY: 21, buttons: 0, pointerId: 3, timeStamp: 30 });
  assert.deepEqual(plain(inputs(worker)), [
    { k: "pointer", type: "down", x: 10.5, y: 20, mask: 1, pid: 3, ts: 1012 },
    { k: "pointer", type: "move", x: 11, y: 21, mask: 1, pid: 3, ts: 1018 },
    { k: "pointer", type: "up", x: 11, y: 21, mask: 0, pid: 3, ts: 1030 },
  ]);
});

test("page: an engine without input capabilities gets no listeners", async () => {
  const page = bootPage();
  const { worker } = await mounted(page, withCaps("wasm"));
  assert.equal(Object.keys(page.listeners).filter(k => k.startsWith("key") || k.startsWith("pointer")).length, 0);
  assert.equal(Object.keys(page.winListeners).length, 0);
  assert.equal(worker.of("input").length, 0);
});

test("page: blur releases everything, and terminating the worker removes the listeners", async () => {
  const page = bootPage();
  const { worker } = await mounted(page, withCaps("keyboard"));
  page.fire("blur", {}, "window");
  assert.equal(inputs(worker).at(-1).k, "blur");
  assert.equal((page.listeners.keydown || []).length, 1);
  page.host.get(ENTRY.id).terminate();
  assert.equal((page.listeners.keydown || []).length, 0);
  assert.equal((page.winListeners.blur || []).length, 0);
});

test("page: gamepads are polled, sent only on change, and a vanished pad is reported", async () => {
  let pads = [];
  const page = bootPage({ navigator: { getGamepads: () => pads } });
  const { worker } = await mounted(page, withCaps("gamepad"));
  const pad = (buttons, axes) => ({ index: 0, id: "Pad", mapping: "standard", connected: true, timestamp: 77, buttons: buttons.map(value => ({ value, pressed: value > 0 })), axes });
  pads = [pad([0, 0], [0, 0])];
  await new Promise(r => setTimeout(r, 40));
  assert.equal(inputs(worker).length, 1, "an unchanged pad is sent once");
  assert.deepEqual(plain(inputs(worker)[0]), { k: "pad", index: 0, id: "Pad", mapping: "standard", connected: true, buttons: [0, 0], axes: [0, 0], ts: 1077 });
  pads = [pad([1, 0], [0.0004, 0])];                 // below the epsilon on the axis, a real change on the button
  await new Promise(r => setTimeout(r, 40));
  assert.equal(inputs(worker).length, 2);
  assert.deepEqual(plain(inputs(worker)[1].buttons), [1, 0]);
  pads = [pad([1, 0], [0.0009, 0])];                 // jitter only
  await new Promise(r => setTimeout(r, 40));
  assert.equal(inputs(worker).length, 2, "jitter below 0.001 is not an input");
  pads = [null];
  await new Promise(r => setTimeout(r, 40));
  assert.deepEqual(plain(inputs(worker).at(-1)), { k: "pad", index: 0, connected: false, ts: inputs(worker).at(-1).ts });
  page.host.get(ENTRY.id).terminate();
});

test("page: a hidden page stops gamepad polling and releases input; showing it resumes", async () => {
  let pads = [{ index: 0, id: "P", mapping: "standard", connected: true, buttons: [{ value: 0 }], axes: [] }];
  const page = bootPage({ navigator: { getGamepads: () => pads } });
  const { worker } = await mounted(page, withCaps("gamepad", "keyboard"));
  await new Promise(r => setTimeout(r, 30));
  const before = inputs(worker).length;
  page.document.hidden = true;
  page.fire("visibilitychange");
  assert.equal(inputs(worker).at(-1).k, "blur");
  pads = [{ index: 0, id: "P", mapping: "standard", connected: true, buttons: [{ value: 1 }], axes: [] }];
  await new Promise(r => setTimeout(r, 40));
  assert.equal(inputs(worker).filter(e => e.k === "pad").length, before, "no polling while hidden");
  page.document.hidden = false;
  page.fire("visibilitychange");
  await new Promise(r => setTimeout(r, 40));
  assert.ok(inputs(worker).filter(e => e.k === "pad").length > before, "polling resumes when shown");
  page.host.get(ENTRY.id).terminate();
});

test("worker: input arrives as JSON stamped with the event time and arrival time on the worker clock", async () => {
  const w = await mountedWorker();
  const seen = [];
  w.scope.__gosx.worker.onInput(json => seen.push(JSON.parse(json)));
  w.setClock(400);
  // The page stamped the event at page epoch 5000 + 380 = worker time 380 (same timeOrigin here).
  w.send({ t: "input", ev: { k: "key", code: "KeyA", key: "a", down: true, ts: 5380 } });
  assert.equal(seen.length, 1);
  assert.equal(seen[0].at, 380);
  assert.equal(seen[0].recv, 400);
  assert.equal(w.scope.__gosx.worker.toLocalTime(5380), 380);
  const off = w.scope.__gosx.worker.onInput(() => seen.push("second"));
  off();
  w.send({ t: "input", ev: { k: "blur", ts: 5390 } });
  assert.equal(seen.length, 2);
  w.send({ t: "input", ev: "not an object" });
  assert.equal(seen.length, 2);
});

// ---------------------------------------------------------------------------
// Netplay bridge
// ---------------------------------------------------------------------------

function fakeNetplayBinding() {
  const calls = [];
  const conns = new Map();
  let next = 40;
  const api = {
    calls, conns,
    open(cfg) { const id = ++next; calls.push(["open", id]); conns.set(id, { cfg, adoptedOK: cfg.adopt ? cfg.adopt({ fakeChannel: id }) : false }); return api.refuse ? 0 : id; },
    signal(id, event, json) { calls.push(["signal", id, event, json]); },
    send(id, bytes, n) { calls.push(["send", id, Array.from(bytes), n]); return true; },
    close(id) { calls.push(["close", id]); },
    adopted(id, state) { calls.push(["adopted", id, state]); },
  };
  return api;
}

test("page: net.open asks the binding to adopt the channel for the worker and reports the mode", async () => {
  const page = bootPage();
  const net = fakeNetplayBinding();
  page.win.__gosx.netplay = net;
  const { worker } = await mounted(page);
  worker.say({ t: "net.open", nid: 1, session: "s", iceServers: [{ urls: ["stun:x"] }], canAdopt: true });
  const channel = worker.sent.find(s => s.msg.t === "net.channel");
  assert.ok(channel, "the channel is posted to the worker");
  assert.deepEqual(plain(channel.msg.dc), { fakeChannel: 41 });
  assert.ok(channel.transfer.length === 1 && channel.transfer[0] === channel.msg.dc, "the channel travels in the transfer list");
  assert.deepEqual(worker.of("net.mode").map(m => [m.nid, m.mode]), [[1, "transfer"]]);
  assert.equal(net.conns.get(41).cfg.session, "s");
});

test("page: without adopt support, or when it is refused, datagrams relay over the port", async () => {
  for (const canAdopt of [false, true]) {
    const page = bootPage();
    const net = fakeNetplayBinding();
    const open = net.open;
    net.open = cfg => { if (canAdopt) { const real = cfg.adopt; cfg.adopt = () => false; void real; } return open(cfg); };
    page.win.__gosx.netplay = net;
    const { worker } = await mounted(page);
    worker.say({ t: "net.open", nid: 2, session: "s", iceServers: [], canAdopt });
    assert.equal(worker.sent.some(s => s.msg.t === "net.channel"), false);
    assert.deepEqual(worker.of("net.mode").map(m => m.mode), ["relay"]);
    const cfg = net.conns.get(41).cfg;
    const dgram = new Uint8Array([1, 2, 3]);
    cfg.onDatagram(dgram);
    const data = worker.sent.find(s => s.msg.t === "net.data");
    assert.equal(data.msg.buf, dgram.buffer);
    assert.ok(data.transfer.length === 1 && data.transfer[0] === dgram.buffer);
    worker.say({ t: "net.send", nid: 2, buf: new Uint8Array([9, 8]).buffer });
    assert.deepEqual(net.calls.at(-1), ["send", 41, [9, 8], 2]);
  }
});

test("page: signals, state and close pass through; a missing or refusing binding fails the connection", async () => {
  const page = bootPage();
  const net = fakeNetplayBinding();
  page.win.__gosx.netplay = net;
  const { worker } = await mounted(page);
  worker.say({ t: "net.open", nid: 3, session: "s", iceServers: [], canAdopt: false });
  const cfg = net.conns.get(41).cfg;
  cfg.onSignal("netplay.offer", '{"sdp":"v=0"}');
  cfg.onState("open");
  assert.deepEqual(worker.of("net.signal").map(m => [m.nid, m.event, m.json]), [[3, "netplay.offer", '{"sdp":"v=0"}']]);
  assert.deepEqual(worker.of("net.state").map(m => m.state), ["open"]);
  worker.say({ t: "net.signal.in", nid: 3, event: "netplay.answer", json: "{}" });
  worker.say({ t: "net.worker-state", nid: 3, state: "open" });
  worker.say({ t: "net.close", nid: 3 });
  assert.deepEqual(net.calls.slice(-3), [["signal", 41, "netplay.answer", "{}"], ["adopted", 41, "open"], ["close", 41]]);
  worker.say({ t: "net.signal.in", nid: 3, event: "x", json: "{}" });
  assert.equal(net.calls.at(-1)[0], "close", "a closed connection ignores later signals");

  net.refuse = true;
  worker.say({ t: "net.open", nid: 4, session: "s", iceServers: [], canAdopt: false });
  assert.equal(worker.of("net.state").at(-1).state, "failed");
  delete page.win.__gosx.netplay;
  worker.say({ t: "net.open", nid: 5, session: "s", iceServers: [], canAdopt: false });
  assert.deepEqual(worker.of("net.state").slice(-1).map(m => [m.nid, m.state]), [[5, "failed"]]);
});

test("page: terminating the worker closes its page connections", async () => {
  const page = bootPage();
  const net = fakeNetplayBinding();
  page.win.__gosx.netplay = net;
  const { worker } = await mounted(page);
  worker.say({ t: "net.open", nid: 6, session: "s", iceServers: [], canAdopt: true });
  page.host.get(ENTRY.id).terminate();
  assert.deepEqual(net.calls.at(-1), ["close", 41]);
});

class FakeDC {
  constructor(state = "connecting") { this.readyState = state; this.bufferedAmount = 0; this.sent = []; this.binaryType = ""; }
  send(b) { this.sent.push(Array.from(b)); }
  close() { this.readyState = "closed"; this.closed = true; }
}

async function netWorker(opts = {}) {
  const w = await mountedWorker();
  w.scope.RTCDataChannel = opts.noDC ? undefined : FakeDC;
  return w;
}

function openNet(w, init = {}) {
  const signals = [], states = [];
  // The init message from the page said whether RTCPeerConnection exists.
  const id = w.scope.__gosx.netplay.open({ session: "s", iceServers: [{ urls: ["stun:x"] }], onSignal: (e, j) => signals.push([e, j]), onState: s => states.push(s), ...init });
  return { id, signals, states };
}

test("worker: netplay is unsupported unless the page has RTCPeerConnection", async () => {
  const w = await bootedWorker();            // INIT has no rtc flag
  w.send({ t: "mount" }); await tick(); await tick();
  const api = w.scope.__gosx.netplay;
  assert.equal(api.supported(), false);
  const states = [];
  assert.equal(api.open({ session: "s", onSignal() {}, onState: s => states.push(s) }), 0);
  assert.deepEqual(states, ["failed"]);
  assert.equal(w.of("net.open").length, 0);
});

async function rtcWorker(opts) {
  const w = bootWorker();
  w.send({ ...INIT, rtc: true });
  await tick(); await tick(); await tick();
  w.send({ t: "mount" });
  await tick(); await tick();
  w.scope.RTCDataChannel = opts && opts.noDC ? undefined : FakeDC;
  return w;
}

test("worker: open asks the page to start the connection and says whether it can adopt a channel", async () => {
  for (const [noDC, canAdopt] of [[false, true], [true, false]]) {
    const w = await rtcWorker({ noDC });
    const c = openNet(w);
    assert.ok(c.id > 0);
    const req = w.of("net.open")[0].msg;
    assert.deepEqual(plain({ nid: req.nid, session: req.session, canAdopt: req.canAdopt, iceServers: req.iceServers }), { nid: c.id, session: "s", canAdopt, iceServers: [{ urls: ["stun:x"] }] });
  }
});

test("worker: signaling flows both ways through the page", async () => {
  const w = await rtcWorker();
  const c = openNet(w);
  w.send({ t: "net.signal", nid: c.id, event: "netplay.offer", json: '{"a":1}' });
  assert.deepEqual(c.signals, [["netplay.offer", '{"a":1}']]);
  w.scope.__gosx.netplay.signal(c.id, "netplay.answer", '{"b":2}');
  assert.deepEqual(plain(w.of("net.signal.in").map(m => [m.msg.nid, m.msg.event, m.msg.json])), [[c.id, "netplay.answer", '{"b":2}']]);
});

test("worker transfer mode: datagrams use the adopted channel and never cross the port", async () => {
  const w = await rtcWorker();
  const c = openNet(w);
  const api = w.scope.__gosx.netplay;
  const dc = new FakeDC();
  w.send({ t: "net.channel", nid: c.id, dc });
  w.send({ t: "net.mode", nid: c.id, mode: "transfer" });
  assert.equal(dc.binaryType, "arraybuffer");
  assert.equal(api.mode(c.id), "transfer");
  assert.equal(api.send(c.id, Uint8Array.from([1, 2, 3]), 3), false, "not open yet");
  dc.readyState = "open";
  dc.onopen();
  assert.deepEqual(c.states, ["open"]);
  assert.deepEqual(w.of("net.worker-state").map(m => m.msg.state), ["open"], "the page learns the channel opened");
  assert.equal(api.send(c.id, Uint8Array.from([1, 2, 3, 4]), 3), true);
  assert.deepEqual(dc.sent, [[1, 2, 3]]);
  assert.equal(w.of("net.send").length, 0, "no datagram goes through the page");
  dc.bufferedAmount = 20000;
  assert.equal(api.send(c.id, Uint8Array.from([1]), 1), false, "a congested channel drops, like the page binding");
  dc.onmessage({ data: Uint8Array.from([5, 6]).buffer });
  const out = new Uint8Array(1200);
  assert.equal(api.recv(c.id, out), 2);
  assert.deepEqual(Array.from(out.subarray(0, 2)), [5, 6]);
  assert.equal(api.recv(c.id, out), -1);
  assert.deepEqual(plain(api.stats(c.id)), { received: 1, receiveDropped: 0, sent: 1, sendDropped: 1, queued: 0, mode: "transfer" });
  dc.onclose();
  assert.deepEqual(c.states, ["open", "closed"]);
});

test("worker transfer mode: a channel that already opened, or never opens, is handled", async () => {
  const w = await rtcWorker();
  const a = openNet(w);
  w.send({ t: "net.channel", nid: a.id, dc: new FakeDC("open") });
  assert.deepEqual(a.states, ["open"], "a channel that opened before the handlers attached");
  const b = openNet(w);
  const dc = new FakeDC();
  w.send({ t: "net.channel", nid: b.id, dc });
  dc.onclose();
  assert.deepEqual(b.states, ["failed"], "closing before open is a failed connection");
  const c = openNet(w);
  w.send({ t: "net.state", nid: c.id, state: "failed" });
  const late = new FakeDC();
  w.send({ t: "net.channel", nid: c.id, dc: late });
  assert.equal(late.closed, true, "a channel for a failed connection is closed, not kept");
});

test("worker relay mode: datagrams travel through the page as transferred buffers", async () => {
  const w = await rtcWorker({ noDC: true });
  const c = openNet(w);
  const api = w.scope.__gosx.netplay;
  w.send({ t: "net.mode", nid: c.id, mode: "relay" });
  assert.equal(api.mode(c.id), "relay");
  w.send({ t: "net.state", nid: c.id, state: "open" });
  assert.deepEqual(c.states, ["open"]);
  const payload = Uint8Array.from([1, 2, 3, 4]);
  assert.equal(api.send(c.id, payload, 3), true);
  const sent = w.of("net.send")[0];
  assert.deepEqual(Array.from(new Uint8Array(sent.msg.buf)), [1, 2, 3], "only n bytes, copied from the caller's scratch buffer");
  assert.ok(sent.transfer.length === 1 && sent.transfer[0] === sent.msg.buf);
  payload[0] = 99;                                   // the caller reuses its buffer
  assert.equal(new Uint8Array(sent.msg.buf)[0], 1);
  w.send({ t: "net.data", nid: c.id, buf: Uint8Array.from([7, 7]).buffer });
  const out = new Uint8Array(1200);
  assert.equal(api.recv(c.id, out), 2);
  assert.equal(api.send(c.id, payload, 0), false);
  assert.equal(api.send(c.id, payload, 1201), false);
});

test("worker: close tells the page, and later signals and sends are ignored", async () => {
  const w = await rtcWorker();
  const c = openNet(w);
  const api = w.scope.__gosx.netplay;
  api.close(c.id);
  assert.equal(w.of("net.close").length, 1);
  assert.equal(api.state(c.id), "closed");
  api.signal(c.id, "x", "{}");
  assert.equal(w.of("net.signal.in").length, 0);
  assert.equal(api.send(c.id, Uint8Array.from([1]), 1), false);
  w.send({ t: "net.signal", nid: c.id, event: "e", json: "{}" });
  assert.equal(c.signals.length, 0);
});

test("worker: a page failure after the channel opened is a closure, before it a failure", async () => {
  const w = await rtcWorker();
  const a = openNet(w);
  const dc = new FakeDC("open");
  w.send({ t: "net.channel", nid: a.id, dc });
  w.send({ t: "net.state", nid: a.id, state: "failed" });
  assert.deepEqual(a.states, ["open", "closed"]);
  assert.equal(dc.closed, true);
  const b = openNet(w);
  w.send({ t: "net.state", nid: b.id, state: "failed" });
  assert.deepEqual(b.states, ["failed"]);
});

// ---------------------------------------------------------------------------
// Calls, view acknowledgements, status, worker policy, vessel
// ---------------------------------------------------------------------------

const statusOf = (page, id = ENTRY.id) => page.signalWrites.filter(w => w[0] === "$gosx.engine." + id).map(w => w[1]);

// A context whose shared signals are an in-memory store with subscribers.
function storeCtx() {
  const subs = new Map();
  const store = new Map();
  const ctx = fakeCtx({
    subscribeSignalJSON: (name, cb, options) => {
      const set = subs.get(name) || new Set(); set.add(cb); subs.set(name, set);
      if (!options || options.immediate !== false) cb(JSON.stringify(store.has(name) ? store.get(name) : null), name);
      return () => set.delete(cb);
    },
  });
  ctx.write = (name, value) => { store.set(name, value); for (const cb of [...(subs.get(name) || [])]) cb(JSON.stringify(value), name); };
  ctx.subscribers = name => (subs.get(name) || new Set()).size;
  return ctx;
}

const ZERO = JSON.stringify({ ID: 0, Name: "", Error: "", Value: { Total: 0 }, Recent: null });

async function servingPage(opts = {}) {
  const page = bootPage();
  const { worker, out } = await bootedRecord(page, opts.entry || ENTRY);
  const ctx = storeCtx();
  const handleP = out.factory(ctx);
  await tick();
  worker.say({ t: "mounted" });
  const handle = await handleP;
  worker.say({ t: "calls", channel: "$bv.cmd", timeout: opts.timeout || 50, recent: 3, zero: ZERO });
  return { page, worker, ctx, handle };
}

const resultWrites = page => page.signalWrites.filter(w => w[0] === "$bv.cmd.result").map(w => w[1]);

test("page: status goes booting, running, crashed, and disposed", async () => {
  const page = bootPage();
  const { worker, handle } = await mounted(page);
  assert.deepEqual(statusOf(page), [
    { State: "booting", Thread: "worker", Reason: "" },
    { State: "running", Thread: "worker", Reason: "" },
  ]);
  worker.onerror({ message: "out of memory", preventDefault() {} });
  assert.deepEqual(statusOf(page).at(-1), { State: "crashed", Thread: "worker", Reason: "worker-crashed" });
  handle.dispose();

  const page2 = bootPage();
  const m = await mounted(page2);
  m.handle.dispose();
  m.worker.say({ t: "disposed" });
  assert.deepEqual(statusOf(page2).at(-1), { State: "disposed", Thread: "worker", Reason: "" });
});

test("page: a fallback reports running on the main thread with the reason", async () => {
  const page = bootPage({ noWorker: true });
  const out = await page.host.resolve(ENTRY, {}, fakeFallback());
  assert.deepEqual(statusOf(page), []);
  out.factory({});
  assert.deepEqual(statusOf(page), [{ State: "running", Thread: "main", Reason: "worker-unsupported" }]);
});

test("page: the server-rendered force flag falls back like the JS global", async () => {
  const page = bootPage();
  const fallback = fakeFallback();
  const out = await page.host.resolve({ ...ENTRY, forceWorkerFallback: true }, {}, fallback);
  assert.equal(FakeWorker.all.length, 0, "no worker may start");
  assert.equal(fallback.calls.length, 1);
  const ctx = {};
  out.factory(ctx);
  assert.equal(ctx.threadFallbackReason, "worker-forced-fallback");
});

test("page: a worker-required engine never runs on the main thread", async () => {
  for (const entry of [{ ...ENTRY, workerMode: "required", forceWorkerFallback: true }, { ...ENTRY, workerMode: "required", toolchain: "tinygo" }]) {
    const page = bootPage();
    const fallback = fakeFallback();
    const out = await page.host.resolve(entry, {}, fallback);
    assert.equal(fallback.calls.length, 0, "the main-thread fallback must not run");
    assert.equal(FakeWorker.all.length, 0);
    assert.equal(statusOf(page).at(-1).State, "unsupported");
    assert.match(statusOf(page).at(-1).Reason, /^worker-/);
    assert.equal(page.issues.at(-1).severity, "error");
    assert.equal(page.docEvents.at(-1).type, "gosx:engine:worker-unsupported");
    const ctx = {};
    const handle = out.factory(ctx);
    assert.equal(ctx.threadMode, "");
    handle.dispose();
    assert.equal(statusOf(page).at(-1).State, "disposed");
  }
});

test("page: a worker-required engine whose worker fails to boot is unsupported, not a fallback", async () => {
  const page = bootPage({ throwOnCreate: "SecurityError: blocked" });
  const fallback = fakeFallback();
  await page.host.resolve({ ...ENTRY, workerMode: "required" }, {}, fallback);
  assert.equal(fallback.calls.length, 0);
  assert.deepEqual(statusOf(page).at(-1), { State: "unsupported", Thread: "", Reason: "worker-create-failed" });
});

test("page: a call nobody answers fails with timeout and the result keeps its shape", async () => {
  const { page, ctx } = await servingPage({ timeout: 20 });
  ctx.write("$bv.cmd", { ID: 1, Name: "apply", Args: {} });
  assert.deepEqual(resultWrites(page), []);
  await new Promise(r => setTimeout(r, 40));
  assert.deepEqual(resultWrites(page), [{ ID: 1, Name: "apply", Error: "timeout", Value: { Total: 0 }, Recent: [{ ID: 1, Error: "timeout" }] }]);
});

test("page: a reply cancels the watchdog, and an older call is cleared by a newer reply", async () => {
  const { page, ctx } = await servingPage({ timeout: 20 });
  ctx.write("$bv.cmd", { ID: 1, Name: "a" });
  ctx.write("$bv.cmd", { ID: 2, Name: "b" });
  ctx.write("$bv.cmd.result", { ID: 2, Name: "b", Error: "", Value: { Total: 1 }, Recent: [{ ID: 2, Error: "" }] });
  await new Promise(r => setTimeout(r, 40));
  assert.deepEqual(resultWrites(page), [], "no timeout may follow a reply to a newer call");
});

test("page: repeated deliveries of one call start one watchdog", async () => {
  const { page, ctx } = await servingPage({ timeout: 20 });
  ctx.write("$bv.cmd", { ID: 1, Name: "a" });
  ctx.write("$bv.cmd", { ID: 1, Name: "a" });
  await new Promise(r => setTimeout(r, 40));
  assert.equal(resultWrites(page).length, 1);
});

test("page: a crash fails every pending call and any later call at once", async () => {
  const { page, worker, ctx } = await servingPage({ timeout: 1000 });
  ctx.write("$bv.cmd", { ID: 1, Name: "a" });
  ctx.write("$bv.cmd", { ID: 2, Name: "b" });
  worker.onerror({ message: "boom", preventDefault() {} });
  const writes = resultWrites(page);
  assert.deepEqual(writes.map(w => [w.ID, w.Error]), [[1, "worker-crashed"], [2, "worker-crashed"]]);
  assert.deepEqual(writes[1].Recent, [{ ID: 1, Error: "worker-crashed" }, { ID: 2, Error: "worker-crashed" }]);
  ctx.write("$bv.cmd", { ID: 3, Name: "c" });
  assert.deepEqual(resultWrites(page).at(-1).Error, "worker-crashed");
  assert.equal(resultWrites(page).at(-1).ID, 3);
});

test("page: a call written before the worker declared the channel is watched from the declaration", async () => {
  const page = bootPage();
  const { worker, out } = await bootedRecord(page);
  const ctx = storeCtx();
  ctx.write("$bv.cmd", { ID: 7, Name: "early" });
  const p = out.factory(ctx); await tick(); worker.say({ t: "mounted" }); await p;
  worker.say({ t: "calls", channel: "$bv.cmd", timeout: 20, recent: 3, zero: ZERO });
  await new Promise(r => setTimeout(r, 40));
  assert.equal(resultWrites(page)[0].ID, 7);
});

test("page: dispose stops the watchdogs and releases the subscriptions", async () => {
  const { page, worker, ctx, handle } = await servingPage({ timeout: 20 });
  ctx.write("$bv.cmd", { ID: 1, Name: "a" });
  handle.dispose();
  worker.say({ t: "disposed" });
  await new Promise(r => setTimeout(r, 40));
  assert.deepEqual(resultWrites(page), []);
  assert.equal(ctx.subscribers("$bv.cmd"), 0);
  assert.equal(ctx.subscribers("$bv.cmd.result"), 0);
});

test("page: a view batch with a seq is acknowledged after it is applied", async () => {
  const page = bootPage();
  const { worker, ctx } = await mounted(page);
  const bytes = new TextEncoder().encode('{"$v":1}');
  worker.say({ t: "signals", changedOnly: true, buf: bytes.buffer, seq: 9 });
  assert.deepEqual(ctx.calls.at(-1), ["setSignalsJSON", '{"$v":1}', true]);
  assert.deepEqual(plain(worker.of("ack")), [{ t: "ack", seq: 9, ok: true }]);
  worker.say({ t: "signals", buf: new TextEncoder().encode("{}").buffer });
  assert.equal(worker.of("ack").length, 1, "a batch without a seq needs no ack");
});

test("page: a rejected view batch is still acknowledged so the view does not stall", async () => {
  const page = bootPage();
  const { worker } = await mounted(page, ENTRY, { setSignalsJSON() { throw new Error("bad"); } });
  worker.say({ t: "signals", buf: new TextEncoder().encode("[]").buffer, seq: 2 });
  assert.equal(worker.of("fault").length, 1);
  assert.deepEqual(plain(worker.of("ack")), [{ t: "ack", seq: 2, ok: false }]);
});

test("page: vessel pose frames and cutaways reach the page controller", async () => {
  const page = bootPage();
  const { worker } = await mounted(page);
  const seen = [];
  page.win.__gosx_scene3d_vessel_pose = (target, bytes) => { seen.push(["pose", target, Array.from(bytes)]); return true; };
  page.win.__gosx_scene3d_vessel_cutaway = (target, id, json) => { seen.push(["cutaway", target, id, json]); return true; };
  worker.say({ t: "frame", kind: "vessel-pose", target: "ship", buf: new Uint8Array([1, 2, 3]).buffer });
  worker.say({ t: "vessel.cutaway", target: "ship", nodeID: "hull", json: '{"Open":true}' });
  assert.deepEqual(seen, [["pose", "ship", [1, 2, 3]], ["cutaway", "ship", "hull", '{"Open":true}']]);
  assert.equal(page.docEvents.length, 0);
  assert.deepEqual(plain(worker.of("vessel.result")), [
    { t: "vessel.result", kind: "pose", target: "ship", nodeID: "", ok: true },
    { t: "vessel.result", kind: "cutaway", target: "ship", nodeID: "hull", ok: true },
  ]);
  worker.say({ t: "frame", kind: "vessel-pose", target: "ship", buf: new ArrayBuffer(4) });
  assert.equal(worker.of("vessel.result").length, 2, "an unchanged answer is not repeated");
  page.win.__gosx_scene3d_vessel_pose = () => false;
  worker.say({ t: "frame", kind: "vessel-pose", target: "ship", buf: new ArrayBuffer(4) });
  assert.equal(page.host.get(ENTRY.id).stats().vesselRejected, 1);
  assert.equal(worker.of("vessel.result").at(-1).ok, false, "the worker learns the page rejected it");
  assert.equal(page.docEvents.length, 0, "an early pose is counted, not raised");
});

test("worker: sendView posts a numbered batch and the page's ack calls back with the token", async () => {
  const w = await mountedWorker();
  const ctx = w.scope.factoryCtx;
  const acked = [];
  ctx.sendView('{"$v":1}', true, 41, t => acked.push(t));
  ctx.sendView('{"$v":2}', true, 42, t => acked.push(t));
  const batches = w.of("signals");
  assert.deepEqual(batches.map(b => b.msg.seq), [1, 2]);
  assert.equal(batches[0].msg.changedOnly, true);
  w.send({ t: "ack", seq: 2 });
  w.send({ t: "ack", seq: 2 });
  w.send({ t: "ack", seq: 1 });
  assert.deepEqual(acked, [42, 41], "each ack fires once");
  ctx.sendView('{"$v":3}', true, 43, (token, ok) => acked.push([token, ok]));
  w.send({ t: "ack", seq: 3, ok: false });
  assert.deepEqual(plain(acked.at(-1)), [43, false], "a rejected batch is reported to the callback");
  ctx.sendView('{"$v":4}', true, 44, t => acked.push(t));
  w.send({ t: "dispose" });
  w.send({ t: "ack", seq: 4 });
  assert.equal(acked.includes(44), false, "no acknowledgement reaches a disposed engine");
});

test("worker: declareCalls tells the page about the channel", async () => {
  const w = await mountedWorker();
  w.scope.factoryCtx.declareCalls("$bv.cmd", 5000, 8, "{}");
  assert.deepEqual(plain(w.of("calls").map(c => c.msg)), [{ t: "calls", channel: "$bv.cmd", timeout: 5000, recent: 8, zero: "{}" }]);
});

test("worker: {immediate: true} replays the page's current value once", async () => {
  const w = await mountedWorker();
  const ctx = w.scope.factoryCtx;
  const seen = [];
  ctx.subscribeSignalJSON("$bv.cmd", json => seen.push(json), { immediate: true });
  const sid = w.of("subscribe")[0].msg.sid;
  w.send({ t: "signal", sid, name: "$bv.cmd", json: '{"ID":3}', initial: true });
  w.send({ t: "signal", sid, name: "$bv.cmd", json: '{"ID":4}' });
  assert.deepEqual(seen, ['{"ID":3}', '{"ID":4}']);
});

test("worker: the vessel globals forward and return what the page last said", async () => {
  const w = await mountedWorker();
  const pose = w.scope.__gosx_scene3d_vessel_pose, cut = w.scope.__gosx_scene3d_vessel_cutaway;
  assert.equal(pose("ship", new Uint8Array([9, 8])), false, "unknown until the page answers, like a mount with no vessel yet");
  const [frame] = w.of("frame");
  assert.deepEqual({ kind: frame.msg.kind, target: frame.msg.target, bytes: Array.from(new Uint8Array(frame.msg.buf)) }, { kind: "vessel-pose", target: "ship", bytes: [9, 8] });
  assert.equal(frame.transfer.length, 1);
  assert.equal(cut("ship", "hull", '{"Open":true}'), false);
  assert.deepEqual(plain(w.of("vessel.cutaway")[0].msg), { t: "vessel.cutaway", target: "ship", nodeID: "hull", json: '{"Open":true}' });
  w.send({ t: "vessel.result", kind: "pose", target: "ship", nodeID: "", ok: true });
  assert.equal(pose("ship", new Uint8Array(1)), true);
  assert.equal(pose("other", new Uint8Array(1)), false, "answers are per target");
  assert.equal(cut("ship", "hull", "{}"), false, "and per kind and node");
  w.send({ t: "vessel.result", kind: "cutaway", target: "ship", nodeID: "hull", ok: true });
  assert.equal(cut("ship", "hull", "{}"), true);
  w.send({ t: "vessel.result", kind: "pose", target: "ship", nodeID: "", ok: false });
  assert.equal(pose("ship", new Uint8Array(1)), false, "the mount can stop showing a vessel");
});

// ---------------------------------------------------------------------------
// Review fixes: remount, teardown paths, ordering
// ---------------------------------------------------------------------------

const engineStatuses = page => statusOf(page).map(s => s.State);

test("page: a remounted engine does not time out a call its result already answers", async () => {
  const page = bootPage();
  const { worker, out } = await bootedRecord(page);
  const ctx = storeCtx();
  ctx.write("$bv.cmd", { ID: 3, Name: "fire" });
  ctx.write("$bv.cmd.result", { ID: 3, Name: "fire", Error: "", Value: { Total: 9 }, Recent: [{ ID: 3, Error: "" }] });
  const handleP = out.factory(ctx);
  await tick();
  worker.say({ t: "mounted" });
  await handleP;
  worker.say({ t: "calls", channel: "$bv.cmd", timeout: 20, recent: 3, zero: ZERO });
  await new Promise(r => setTimeout(r, 50));
  assert.deepEqual(resultWrites(page), [], "call 3 was answered before the remount");
  ctx.write("$bv.cmd", { ID: 4, Name: "fire" });
  await new Promise(r => setTimeout(r, 50));
  assert.equal(resultWrites(page)[0].ID, 4, "a new call is still watched");
});

test("page: declaring a channel twice updates its timeout and recent length", async () => {
  const { page, worker, ctx } = await servingPage({ timeout: 1000 });
  worker.say({ t: "calls", channel: "$bv.cmd", timeout: 20, recent: 1, zero: ZERO });
  ctx.write("$bv.cmd", { ID: 1, Name: "a" });
  await new Promise(r => setTimeout(r, 50));
  assert.equal(resultWrites(page)[0].Error, "timeout");
  assert.equal(ctx.subscribers("$bv.cmd"), 1, "no second subscription");
});

test("page: a mount that fails after the engine declared calls leaves nothing armed", async () => {
  const page = bootPage();
  const { worker, out } = await bootedRecord(page);
  const ctx = storeCtx();
  const handleP = out.factory(ctx);
  await tick();
  worker.say({ t: "calls", channel: "$bv.cmd", timeout: 20, recent: 3, zero: ZERO });
  ctx.write("$bv.cmd", { ID: 1, Name: "a" });
  worker.say({ t: "fatal", code: "go-wasm-factory-failed", message: "factory error" });
  await assert.rejects(handleP);
  await new Promise(r => setTimeout(r, 50));
  assert.deepEqual(resultWrites(page).map(r => [r.ID, r.Error]), [[1, "worker-crashed"]], "the pending call is failed once, by the crash, not later by a timer");
  assert.equal(engineStatuses(page).at(-1), "crashed");
  assert.equal(worker.terminated, true);
  assert.equal(ctx.subscribers("$bv.cmd"), 0);
  assert.equal(ctx.subscribers("$bv.cmd.result"), 0);
});

test("page: terminating a pending engine mid-mount stops watchdogs, frees subscriptions, and ends the status", async () => {
  const page = bootPage();
  const pending = {};
  const resolved = page.host.resolve(ENTRY, pending, fakeFallback());
  await tick();
  const worker = FakeWorker.all[0];
  worker.say({ t: "booted", v: 1 });
  const out = await resolved;
  const ctx = storeCtx();
  const handleP = out.factory(ctx);
  await tick();
  worker.say({ t: "calls", channel: "$bv.cmd", timeout: 20, recent: 3, zero: ZERO });
  ctx.write("$bv.cmd", { ID: 7, Name: "fire" });
  pending.workerRecord.terminate();
  await assert.rejects(handleP);
  await new Promise(r => setTimeout(r, 50));
  assert.deepEqual(resultWrites(page), []);
  assert.equal(engineStatuses(page).at(-1), "disposed");
  assert.equal(ctx.subscribers("$bv.cmd"), 0);
  assert.equal(ctx.subscribers("$bv.cmd.result"), 0);
});

test("page: a worker still booting when it is terminated does not stay at booting", async () => {
  const page = bootPage();
  const pending = {};
  const resolved = page.host.resolve(ENTRY, pending, fakeFallback());
  const rejected = assert.rejects(resolved, error => error.code === "worker-terminated");
  await tick();
  pending.workerRecord.terminate();
  assert.equal(engineStatuses(page).at(-1), "disposed");
  await rejected;
});

test("page: a boot failure falls back without a transient disposed status", async () => {
  const page = bootPage({ timeout: 20 });
  const resolved = page.host.resolve(ENTRY, {}, fakeFallback()); // the worker never says "booted"
  const out = await resolved;
  out.factory({});
  assert.deepEqual(engineStatuses(page), ["booting", "running"]);
  const page2 = bootPage();
  const second = page2.host.resolve(ENTRY, {}, fakeFallback());
  await tick();
  FakeWorker.all[0].say({ t: "fatal", code: "worker-fetch-status", message: "404" });
  (await second).factory({});
  assert.deepEqual(engineStatuses(page2), ["booting", "running"]);
});

test("page: after a crash a later call still fails at once, and dispose then frees the subscriptions", async () => {
  const { page, worker, ctx, handle } = await servingPage({ timeout: 1000 });
  worker.onerror({ message: "boom", preventDefault() {} });
  assert.equal(ctx.subscribers("$bv.cmd"), 1);
  ctx.write("$bv.cmd", { ID: 1, Name: "a" });
  assert.equal(resultWrites(page).at(-1).Error, "worker-crashed");
  handle.dispose();
  assert.equal(ctx.subscribers("$bv.cmd"), 0);
  assert.equal(engineStatuses(page).at(-1), "crashed", "dispose does not hide the crash");
});

test("page: a fallback reports running only after the engine mounted, and crashed if it fails", async () => {
  const page = bootPage({ noWorker: true });
  const fallback = async () => ({ factory: () => new Promise((ok, bad) => { page.settle = { ok, bad }; }), moduleRecord: null });
  const out = await page.host.resolve(ENTRY, {}, fallback);
  const pending = out.factory({});
  assert.deepEqual(statusOf(page), [], "no running status while the engine is still mounting");
  page.settle.ok({ dispose() {} });
  await pending;
  assert.equal(engineStatuses(page).at(-1), "running");

  const page2 = bootPage({ noWorker: true });
  const failing = async () => ({ factory: () => Promise.reject(new Error("mount failed")), moduleRecord: null });
  const out2 = await page2.host.resolve(ENTRY, {}, failing);
  await assert.rejects(out2.factory({}));
  assert.deepEqual(statusOf(page2), [{ State: "crashed", Thread: "main", Reason: "worker-unsupported" }]);
});
