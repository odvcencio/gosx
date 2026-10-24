// Tests for the native-host channel of engine-worker.ts: the page runs the same
// protocol against a native engine in a desktop host (window.gosxDesktop) as
// against a Worker, and the islands' signals and calls cannot tell the two apart.
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const source = fs.readFileSync(path.join(__dirname, "engine-worker.ts"), "utf8");
const tick = () => new Promise(resolve => setImmediate(resolve));
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const plain = v => JSON.parse(JSON.stringify(v));

class FakeWorker {
  constructor(url, options) { this.url = url; this.options = options; this.sent = []; this.terminated = false; FakeWorker.all.push(this); }
  postMessage(msg) { this.sent.push(msg); }
  terminate() { this.terminated = true; }
}
FakeWorker.all = [];

// A desktop bridge whose host side is scripted by the test. It records the
// page's requests and lets the test answer them and emit host events.
function fakeDesktop(opts = {}) {
  const listeners = new Map();
  const api = {
    calls: [],
    attachAnswer: opts.attachAnswer || (() => ({ v: 1 })),
    call(method, payload) {
      api.calls.push({ method, payload });
      if (method === "gosx.engine.attach") {
        try { return Promise.resolve(api.attachAnswer(payload)); } catch (error) { return Promise.reject(error); }
      }
      if (api.postError && method === "gosx.engine.post" && api.postError(payload.msg)) return Promise.reject(Object.assign(new Error("post refused"), { code: "bridge.rate_limited" }));
      return Promise.resolve(undefined);
    },
    on(method, handler) {
      const set = listeners.get(method) || new Set();
      set.add(handler); listeners.set(method, set);
      return () => set.delete(handler);
    },
    listenerCount: method => (listeners.get(method) || new Set()).size,
    // Host to page: one event carrying a worker-protocol message.
    hostSays(id, msg) { for (const fn of Array.from(listeners.get("gosx.engine.msg") || [])) fn({ id, msg }); },
    posts: () => api.calls.filter(c => c.method === "gosx.engine.post").map(c => c.payload.msg),
    postsOf: t => api.posts().filter(m => m.t === t),
  };
  if (opts.discovery) {
    api.discoveryCalls = 0;
    api.hasMethod = method => (opts.cachedMethods || []).includes(method);
    api.refreshMethods = () => {
      api.discoveryCalls++;
      return opts.refreshMethods ? opts.refreshMethods() : Promise.resolve(opts.refreshedMethods || []);
    };
  }
  return api;
}

function bootPage(opts = {}) {
  FakeWorker.all = [];
  const issues = [], docEvents = [], signalWrites = [], listeners = {};
  const win = {
    __gosx: {
      reportIssue: i => issues.push(i),
      document: { get: () => ({ assets: { runtime: { standardGoWasmExecPath: "/x/wasm_exec.js", bootstrapFeatureEngineWorkerPath: "/x/worker.js" } } }) },
    },
    __gosx_emit: () => {},
    __gosx_set_shared_signal: (name, json) => { signalWrites.push([name, JSON.parse(json)]); return ""; },
    Worker: opts.noWorker ? undefined : FakeWorker,
    gosxDesktop: opts.desktop,
    __gosx_engine_native_disable: opts.disableNative,
    addEventListener() {}, removeEventListener() {},
  };
  const document = {
    hidden: false, baseURI: "https://app.test/play/", currentScript: { src: "https://app.test/x/worker.js" },
    addEventListener: (name, fn) => { (listeners[name] = listeners[name] || []).push(fn); }, removeEventListener() {}, dispatchEvent: e => docEvents.push(e),
    getElementById: () => null,
  };
  class CustomEvent { constructor(type, init) { this.type = type; this.detail = init && init.detail; } }
  const context = vm.createContext({
    window: win, document, location: { href: "https://app.test/play/" }, console, URL, CustomEvent, Worker: win.Worker,
    performance: { timeOrigin: 1000, now: () => 5 }, TextDecoder, TextEncoder, ArrayBuffer, Uint8Array, JSON, Promise, Map, Set, Math, Number, String, Object, Array, Error,
    setTimeout, clearTimeout, setInterval, clearInterval, isFinite,
  });
  let factory;
  win.__gosx_register_bootstrap_feature = (name, f) => { factory = f; };
  vm.runInContext(source, context, { filename: "engine-worker.ts" });
  factory({});
  const hide = hidden => { document.hidden = hidden; for (const fn of listeners.visibilitychange || []) fn({}); };
  return { win, signalWrites, issues, docEvents, hide, host: win.__gosx.engineWorker };
}

const ENTRY = { id: "gosx-engine-sim", component: "Sim", kind: "worker", programRef: "/gosx/engines/sim.wasm", runtime: "go-wasm", thread: "worker", props: { seed: 7 }, capabilities: ["wasm"] };

function fakeCtx() {
  const ctx = {
    isCurrent: () => !ctx.gone, calls: [], subs: [],
    setSignalsJSON: (text, changedOnly) => ctx.calls.push(["setSignalsJSON", text, changedOnly]),
    setSignal: (name, value) => ctx.calls.push(["setSignal", name, value]),
    getSignal: name => ctx.values && ctx.values[name],
    subscribeSignalJSON: (name, cb) => { ctx.subs.push({ name, cb }); return () => { ctx.subs = ctx.subs.filter(s => s.name !== name); }; },
  };
  return ctx;
}

const statusOf = page => name => page.signalWrites.filter(w => w[0] === "$gosx.engine." + ENTRY.id).map(w => w[1]);
const lastStatus = page => { const all = statusOf(page)(); return all[all.length - 1]; };

// ---------------------------------------------------------------------------
// Choosing the transport
// ---------------------------------------------------------------------------

test("no desktop bridge: the Worker starts, as before", async () => {
  const page = bootPage({});
  assert.equal(page.host.nativeSupported(), false);
  const resolved = page.host.resolve(ENTRY, {}, async () => assert.fail("no fallback"));
  await tick();
  assert.equal(FakeWorker.all.length, 1);
  FakeWorker.all[0].onmessage({ data: { t: "booted", v: 1 } });
  await resolved;
});

test("late native registration is discovered by refreshMethods before attach", async () => {
  const desktop = fakeDesktop({ discovery: true, refreshedMethods: ["gosx.engine.post", "gosx.engine.attach"] });
  const page = bootPage({ desktop });
  const out = await page.host.resolve(ENTRY, {}, async () => assert.fail("no fallback"));
  assert.equal(typeof out.factory, "function");
  assert.equal(desktop.discoveryCalls, 1);
  assert.equal(desktop.calls.filter(c => c.method === "gosx.engine.attach").length, 1);
  assert.equal(FakeWorker.all.length, 0);
});

test("a desktop host with a native engine runs it: attach, then booted, no Worker", async () => {
  const desktop = fakeDesktop();
  const page = bootPage({ desktop });
  assert.equal(page.host.nativeSupported(), true);
  const resolved = page.host.resolve(ENTRY, {}, async () => assert.fail("no fallback"));
  const out = await resolved;
  assert.equal(FakeWorker.all.length, 0, "a native engine needs no Worker");
  assert.equal(typeof out.factory, "function");
  const attach = desktop.calls.find(c => c.method === "gosx.engine.attach");
  assert.deepEqual(plain(attach.payload), { id: "gosx-engine-sim", component: "Sim", props: { seed: 7 }, hidden: false });
  assert.equal(page.host.get(ENTRY.id).thread, "native");
  assert.equal(lastStatus(page).Thread, "native");
  assert.equal(lastStatus(page).State, "booting");
});

test("a successful method query with no native methods starts the Worker without a blind attach", async () => {
  const desktop = fakeDesktop({ discovery: true, refreshedMethods: [] });
  const page = bootPage({ desktop });
  const resolved = page.host.resolve(ENTRY, {}, async () => assert.fail("no fallback"));
  await tick();
  assert.equal(desktop.calls.filter(c => c.method === "gosx.engine.attach").length, 0);
  assert.equal(FakeWorker.all.length, 1);
  FakeWorker.all[0].onmessage({ data: { t: "booted", v: 1 } });
  await resolved;
  assert.equal(page.issues.length, 0);
});

test("a failed method query preserves legacy direct attach", async () => {
  const desktop = fakeDesktop({ discovery: true, refreshMethods: () => Promise.reject(new Error("query unavailable")) });
  const page = bootPage({ desktop });
  const resolved = page.host.resolve(ENTRY, {}, async () => assert.fail("no fallback"));
  await resolved;
  assert.equal(desktop.discoveryCalls, 1);
  assert.equal(desktop.calls.filter(c => c.method === "gosx.engine.attach").length, 1);
  assert.equal(FakeWorker.all.length, 0);
});

test("a host with no native engine for the component starts the Worker without a warning", async () => {
  const desktop = fakeDesktop({ attachAnswer: () => { throw Object.assign(new Error("no native engine"), { code: "engine.not_registered" }); } });
  const page = bootPage({ desktop });
  const resolved = page.host.resolve(ENTRY, {}, async () => assert.fail("no fallback"));
  await tick(); await tick();
  FakeWorker.all[0].onmessage({ data: { t: "booted", v: 1 } });
  await resolved;
  assert.equal(FakeWorker.all.length, 1, "falls back to the Worker");
  assert.equal(page.issues.length, 0, "the ordinary case is not an issue");
  assert.equal(desktop.listenerCount("gosx.engine.msg"), 0, "the failed attach released its listener");
});

test("host native-disabled starts the Worker without a warning", async () => {
  const desktop = fakeDesktop({ attachAnswer: () => { throw Object.assign(new Error("native disabled for Sim"), { code: "engine.native_disabled" }); } });
  const page = bootPage({ desktop });
  const resolved = page.host.resolve(ENTRY, {}, async () => assert.fail("no fallback"));
  await tick(); await tick();
  assert.equal(FakeWorker.all.length, 1);
  FakeWorker.all[0].onmessage({ data: { t: "booted", v: 1 } });
  await resolved;
  assert.equal(page.issues.length, 0);
});

test("a bridge without the engine methods also falls back quietly", async () => {
  const desktop = fakeDesktop({ attachAnswer: () => { throw Object.assign(new Error("not registered"), { code: "bridge.not_found" }); } });
  const page = bootPage({ desktop });
  const resolved = page.host.resolve(ENTRY, {}, async () => assert.fail("no fallback"));
  await tick(); await tick();
  FakeWorker.all[0].onmessage({ data: { t: "booted", v: 1 } });
  await resolved;
  assert.equal(FakeWorker.all.length, 1);
  assert.equal(page.issues.length, 0);
});

test("another attach failure falls back and reports a warning", async () => {
  const desktop = fakeDesktop({ attachAnswer: () => { throw Object.assign(new Error("host busy"), { code: "bridge.internal" }); } });
  const page = bootPage({ desktop });
  const resolved = page.host.resolve(ENTRY, {}, async () => assert.fail("no fallback"));
  await tick(); await tick();
  FakeWorker.all[0].onmessage({ data: { t: "booted", v: 1 } });
  await resolved;
  assert.equal(FakeWorker.all.length, 1);
  assert.equal(page.issues.length, 1);
  assert.equal(page.issues[0].code, "native-attach-failed");
});

test("a host speaking another protocol version is refused and the page falls back", async () => {
  const desktop = fakeDesktop({ attachAnswer: () => ({ v: 99 }) });
  const page = bootPage({ desktop, noWorker: true });
  const fallback = async () => ({ factory: () => ({}), moduleRecord: null });
  const out = await page.host.resolve(ENTRY, {}, fallback);
  assert.equal(typeof out.factory, "function");
  assert.equal(page.issues.filter(i => i.code === "worker-protocol").length, 1, "the mismatch is reported");
});

test("ForceWorkerFallback and the disable switch skip the native engine", async () => {
  for (const [entry, opts] of [[{ ...ENTRY, forceWorkerFallback: true }, {}], [ENTRY, { disableNative: true }]]) {
    const desktop = fakeDesktop();
    const page = bootPage({ desktop, ...opts });
    const resolved = page.host.resolve(entry, {}, async () => ({ factory: () => ({}), moduleRecord: null }));
    await tick(); await tick();
    if (FakeWorker.all[0]) FakeWorker.all[0].onmessage({ data: { t: "booted", v: 1 } });
    await resolved;
    assert.equal(desktop.calls.length, 0, "no bridge traffic");
  }
});

test("a required worker engine still runs natively when the host has it", async () => {
  const desktop = fakeDesktop();
  const page = bootPage({ desktop, noWorker: true });
  const out = await page.host.resolve({ ...ENTRY, workerMode: "required" }, {}, async () => assert.fail("no fallback"));
  assert.equal(typeof out.factory, "function");
  assert.equal(lastStatus(page).State, "booting");
});

// ---------------------------------------------------------------------------
// The same page behavior from both transports
// ---------------------------------------------------------------------------

// Each transport gives the test the engine's side of the conversation.
function workerTransport() {
  const page = bootPage({});
  return {
    name: "worker", thread: "worker", page,
    async start(entry = ENTRY) {
      const resolved = page.host.resolve(entry, {}, async () => assert.fail("no fallback"));
      await tick();
      this.worker = FakeWorker.all[0];
      this.worker.onmessage({ data: { t: "booted", v: 1 } });
      return resolved;
    },
    say(msg) { this.worker.onmessage({ data: msg }); },
    // A view batch: a worker sends transferred bytes.
    batch(json, seq) { const buf = new TextEncoder().encode(json).buffer; this.say({ t: "signals", changedOnly: true, buf, seq }); },
    sent: () => FakeWorker.all[0].sent,
    sentOf(t) { return this.sent().filter(m => m.t === t); },
    crash() { this.worker.onerror({ message: "oom", preventDefault() {} }); },
  };
}

function nativeTransport() {
  const desktop = fakeDesktop();
  const page = bootPage({ desktop, noWorker: true });
  return {
    name: "native", thread: "native", page, desktop,
    async start(entry = ENTRY) { return page.host.resolve(entry, {}, async () => assert.fail("no fallback")); },
    say(msg) { desktop.hostSays(ENTRY.id, msg); },
    // A view batch: the native host sends text.
    batch(json, seq) { this.say({ t: "signals", changedOnly: true, text: json, seq }); },
    sent: () => desktop.posts(),
    sentOf(t) { return desktop.postsOf(t); },
    crash() { this.say({ t: "fatal", code: "engine-crashed", message: "the world is inconsistent" }); },
  };
}

async function mountOn(tr, entry = ENTRY) {
  const out = await tr.start(entry);
  const ctx = fakeCtx();
  const handleP = out.factory(ctx);
  await tick();
  assert.equal(tr.sentOf("mount").length, 1, tr.name + ": the page asks the engine to mount");
  tr.say({ t: "mounted" });
  const handle = await handleP;
  return { ctx, handle };
}

for (const make of [workerTransport, nativeTransport]) {
  const label = make().name;

  test(`${label}: status goes booting, running, disposed with the transport's thread`, async () => {
    const tr = make();
    const { handle } = await mountOn(tr);
    const states = statusOf(tr.page)().map(s => s.State + "/" + s.Thread);
    assert.deepEqual(states, ["booting/" + tr.thread, "running/" + tr.thread]);
    handle.dispose();
    await tick();
    assert.equal(tr.sentOf("dispose").length, 1);
    tr.say({ t: "disposed" });
    assert.equal(lastStatus(tr.page).State, "disposed");
    assert.equal(lastStatus(tr.page).Thread, tr.thread);
  });

  test(`${label}: a view batch applies once through the context and is acknowledged`, async () => {
    const tr = make();
    const { ctx } = await mountOn(tr);
    tr.batch('{"$bv.hud":{"Day":1}}', 3);
    assert.deepEqual(ctx.calls, [["setSignalsJSON", '{"$bv.hud":{"Day":1}}', true]]);
    assert.deepEqual(plain(tr.sentOf("ack")), [{ t: "ack", seq: 3, ok: true }]);
    assert.equal(tr.page.host.get(ENTRY.id).stats().signalBatches, 1);
  });

  test(`${label}: a batch the page rejects is acknowledged as rejected`, async () => {
    const tr = make();
    const { ctx } = await mountOn(tr);
    ctx.setSignalsJSON = () => { throw new Error("bad signal"); };
    tr.batch("{}", 1);
    assert.deepEqual(plain(tr.sentOf("ack")), [{ t: "ack", seq: 1, ok: false }]);
    assert.equal(tr.sentOf("fault").length, 1);
  });

  test(`${label}: subscriptions prime the engine with the current value and forward later writes`, async () => {
    const tr = make();
    const { ctx } = await mountOn(tr);
    ctx.values = { "$bv.cmd": { ID: 4 } };
    tr.say({ t: "subscribe", sid: 1, name: "$bv.cmd" });
    assert.deepEqual(plain(tr.sentOf("signal")), [{ t: "signal", sid: 1, name: "$bv.cmd", json: '{"ID":4}', initial: true }]);
    ctx.subs[ctx.subs.length - 1].cb('{"ID":5}', "$bv.cmd");
    assert.equal(tr.sentOf("signal")[1].json, '{"ID":5}');
    tr.say({ t: "unsubscribe", sid: 1 });
    assert.equal(ctx.subs.length, 0);
  });

  test(`${label}: a declared call channel gets the page timeout watchdog`, async () => {
    const tr = make();
    const { ctx } = await mountOn(tr);
    tr.say({ t: "calls", channel: "$bv.cmd", timeout: 20, recent: 4, zero: '{"ID":0,"Name":"","Error":"","Value":null,"Recent":null}' });
    // The page watches the call and result signals through the context.
    const callSub = ctx.subs.find(s => s.name === "$bv.cmd");
    assert.ok(callSub, "the page watches the call signal");
    callSub.cb('{"ID":1,"Name":"add"}');
    await sleep(60);
    const result = tr.page.signalWrites.filter(w => w[0] === "$bv.cmd.result").map(w => w[1]);
    assert.equal(result.length, 1);
    assert.equal(result[0].ID, 1);
    assert.equal(result[0].Error, "timeout");
  });

  test(`${label}: a reply before the deadline cancels the watchdog`, async () => {
    const tr = make();
    const { ctx } = await mountOn(tr);
    tr.say({ t: "calls", channel: "$bv.cmd", timeout: 30, recent: 4, zero: "{}" });
    ctx.subs.find(s => s.name === "$bv.cmd").cb('{"ID":1,"Name":"add"}');
    ctx.subs.find(s => s.name === "$bv.cmd.result").cb('{"ID":1,"Value":{"Total":5}}');
    await sleep(70);
    assert.equal(tr.page.signalWrites.filter(w => w[0] === "$bv.cmd.result").length, 0, "no timeout written");
  });

  test(`${label}: a crash fails pending and later calls with worker-crashed and sets the status`, async () => {
    const tr = make();
    const { ctx } = await mountOn(tr);
    tr.say({ t: "calls", channel: "$bv.cmd", timeout: 5000, recent: 4, zero: "{}" });
    const callSub = ctx.subs.find(s => s.name === "$bv.cmd");
    callSub.cb('{"ID":1,"Name":"add"}');
    tr.crash();
    await tick();
    const writes = () => tr.page.signalWrites.filter(w => w[0] === "$bv.cmd.result").map(w => w[1]);
    assert.equal(writes().length, 1);
    assert.equal(writes()[0].Error, "worker-crashed");
    assert.equal(lastStatus(tr.page).State, "crashed");
    callSub.cb('{"ID":2,"Name":"add"}');
    assert.equal(writes().length, 2);
    assert.equal(writes()[1].Error, "worker-crashed");
    assert.equal(tr.page.docEvents.some(e => e.type === "gosx:engine:worker-error"), true);
  });

  test(`${label}: a mount failure rejects the factory and marks the engine crashed`, async () => {
    const tr = make();
    const out = await tr.start();
    const handleP = out.factory(fakeCtx());
    await tick();
    tr.say({ t: "fatal", code: "engine-mount-failed", message: "no save file" });
    await assert.rejects(handleP, /no save file/);
    assert.equal(lastStatus(tr.page).State, "crashed");
  });

  test(`${label}: disposePage shuts a mounted engine down`, async () => {
    const tr = make();
    await mountOn(tr);
    tr.page.host.disposePage(new Set());
    await tick();
    assert.equal(tr.sentOf("dispose").length, 1);
  });

  test(`${label}: visibility reaches the engine`, async () => {
    const tr = make();
    await mountOn(tr);
    tr.page.hide(true);
    tr.page.hide(false);
    const seen = tr.sentOf("visibility");
    assert.deepEqual(seen.map(m => m.hidden), [true, false]);
    assert.equal(typeof seen[0].at, "number");
  });
}

// ---------------------------------------------------------------------------
// Native-only behavior
// ---------------------------------------------------------------------------

test("native and worker transports preserve mount, view, call, ack and dispose parity", async () => {
  const traces = [];
  for (const make of [workerTransport, nativeTransport]) {
    const tr = make();
    const { ctx, handle } = await mountOn(tr);
    tr.batch('{"$bv.hud":{"Day":1}}', 3);
    tr.say({ t: "calls", channel: "$bv.cmd", timeout: 1000, recent: 4, zero: "{}" });
    const command = ctx.subs.find(s => s.name === "$bv.cmd");
    const result = ctx.subs.find(s => s.name === "$bv.cmd.result");
    assert.ok(command && result, tr.name + ": call channel subscribed");
    command.cb('{"ID":1,"Name":"add"}');
    let callResult;
    const deliverResult = result.cb;
    result.cb = encoded => { callResult = JSON.parse(encoded); deliverResult(encoded); };
    const expectedCallResult = { ID: 1, Name: "add", Value: { Total: 5 }, Error: "", Recent: [] };
    result.cb(JSON.stringify(expectedCallResult));
    handle.dispose();
    await tick();
    tr.say({ t: "disposed" });
    traces.push({
      mount: tr.sentOf("mount").map(plain),
      signals: ctx.calls.filter(c => c[0] === "setSignalsJSON"),
      ack: tr.sentOf("ack").map(m => ({ t: m.t, seq: m.seq, ok: m.ok })),
      callResult,
      status: lastStatus(tr.page).State,
      listeners: tr.desktop ? tr.desktop.listenerCount("gosx.engine.msg") : 0,
    });
  }
  assert.deepEqual(traces[0], traces[1]);
});

test("native: only protocol messages cross the bridge, in order, with the engine id", async () => {
  const tr = nativeTransport();
  await mountOn(tr);
  const posts = tr.desktop.calls.filter(c => c.method === "gosx.engine.post");
  assert.ok(posts.every(c => c.payload.id === ENTRY.id));
  assert.deepEqual(posts.map(c => c.payload.msg.t), ["mount"]);
  // The page forwards input and app messages to a Worker; a native engine has no use for them.
  const rec = tr.page.host.get(ENTRY.id);
  rec.worker.postMessage({ t: "input", ev: { kind: "key" } });
  rec.worker.postMessage({ t: "app", name: "x" });
  rec.worker.postMessage({ t: "net.state" });
  assert.equal(tr.desktop.calls.filter(c => c.method === "gosx.engine.post").length, 1);
});

test("native: events for another engine id are ignored", async () => {
  const tr = nativeTransport();
  const { ctx } = await mountOn(tr);
  tr.desktop.hostSays("someone-else", { t: "signals", changedOnly: true, text: '{"$x":1}', seq: 1 });
  assert.deepEqual(ctx.calls, []);
});

test("native: a refused mount post is a mount failure; a refused dispose still ends the engine", async () => {
  const tr = nativeTransport();
  tr.desktop.postError = msg => msg.t === "mount";
  const out = await tr.start();
  const handleP = out.factory(fakeCtx());
  await assert.rejects(handleP, /post refused/);
  assert.equal(lastStatus(tr.page).State, "crashed");

  const tr2 = nativeTransport();
  tr2.desktop.postError = msg => msg.t === "dispose";
  const { handle } = await mountOn(tr2);
  handle.dispose();
  await tick(); await tick();
  assert.equal(lastStatus(tr2.page).State, "disposed");
});

test("native: ending the page record without a dispose still tells the host and drops the listener", async () => {
  const tr = nativeTransport();
  await mountOn(tr);
  assert.equal(tr.desktop.listenerCount("gosx.engine.msg"), 1);
  tr.page.host.get(ENTRY.id).terminate();
  await tick();
  assert.equal(tr.sentOf("dispose").length, 1);
  assert.equal(tr.desktop.listenerCount("gosx.engine.msg"), 0);
});

test("native: a call written while the engine mounts is handled from the current value", async () => {
  const tr = nativeTransport();
  const { ctx } = await mountOn(tr);
  ctx.values = { "$bv.cmd": { ID: 2, Name: "add" } };
  tr.say({ t: "subscribe", sid: 7, name: "$bv.cmd" });
  const primed = tr.sentOf("signal")[0];
  assert.equal(primed.sid, 7);
  assert.equal(primed.initial, true);
  assert.deepEqual(JSON.parse(primed.json), { ID: 2, Name: "add" });
});
