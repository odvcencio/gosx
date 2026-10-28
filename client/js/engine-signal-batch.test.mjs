import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";

// These tests run the page's real shared-signal store (00-textlayout.ts) and the
// real engine context (30b-tail-engine-mounting.ts) against a stand-in for the Go
// runtime's __gosx_set_input_batch. The stand-in follows the runtime's contract:
// it decodes and validates the batch itself, skips unchanged signals of a
// changed-only batch, and hands the changes back to the page. The sandbox counts
// every JSON.parse the page makes; the stand-in uses the real JSON outside it.

const layout = fs.readFileSync(new URL("./bootstrap-src/00-textlayout.ts", import.meta.url), "utf8");
const mounting = fs.readFileSync(new URL("./bootstrap-src/30b-tail-engine-mounting.ts", import.meta.url), "utf8");
const storeSource = layout.slice(layout.indexOf("  // The page's shared signal values."), layout.indexOf("  function gosxIssueStore()"));
const contextSource = mounting.slice(mounting.indexOf("function createEngineContext("), mounting.indexOf("async function mountEngine("));
assert.ok(storeSource.includes("function gosxNotifySharedSignal(") && contextSource.includes("function applySignalBatch("), "bootstrap sources moved");

const realParse = JSON.parse;
const realStringify = JSON.stringify;

// runtime builds the stand-in. A page that has no batch method on its notify
// function gets the changes one by one, as the Go runtime does.
function runtime(window, { announce = true } = {}) {
  const state = { calls: [], values: new Map(), failure: "" };
  const fn = (encoded, changedOnly) => {
    state.calls.push({ encoded, changedOnly });
    if (state.failure) return state.failure;
    let batch;
    try { batch = realParse(encoded); } catch (error) { return "decode shared signal batch: " + error.message; }
    if (batch === null || typeof batch !== "object" || Array.isArray(batch)) return "decode shared signal batch: expected JSON object";
    const changes = [];
    for (const [name, value] of Object.entries(batch)) {
      const text = realStringify(value);
      if (changedOnly === true && state.values.get(name) === text) continue;
      state.values.set(name, text);
      changes.push(name, text);
    }
    const notify = window.__gosx_notify_shared_signal;
    if (typeof notify.batch === "function") notify.batch(...changes);
    else for (let i = 0; i < changes.length; i += 2) notify(changes[i], changes[i + 1]);
    return null;
  };
  if (announce) fn.batchDelivery = true;
  window.__gosx_set_input_batch = fn;
  window.__gosx_get_shared_signal = name => state.values.has(name) ? state.values.get(name) : "null";
  return state;
}

// page loads the bootstrap pieces into a fresh sandbox and returns an engine context.
function page(options = {}) {
  const sandbox = { console: { error() {}, warn() {}, log() {} }, document: {}, parses: 0 };
  sandbox.window = sandbox;
  sandbox.JSON = { parse(...args) { sandbox.parses++; return realParse(...args); }, stringify: realStringify };
  sandbox.pendingEngineOwned = () => true;
  sandbox.goWASMEnginePageGeneration = 1;
  vm.createContext(sandbox);
  vm.runInContext(`const GOSX_VERSION = "test";\n${storeSource}\n${contextSource}`, sandbox);
  const state = runtime(sandbox, options);
  const ctx = sandbox.createEngineContext({ id: "view" }, {}, {}, { required: [] }, { generation: 1, closed: false });
  return { window: sandbox, ctx, state, parses: () => sandbox.parses, store: () => sandbox.__gosx.sharedSignals };
}

const batch = (a, b, c) => realStringify({ "$a": a, "$b": b, "$c": c });

test("a batch costs the page no JSON.parse while only raw subscribers read it", () => {
  const { ctx, state, parses } = page();
  const raw = {};
  for (const name of ["$a", "$b", "$c"]) ctx.subscribeSignalJSON(name, (text, signalName) => { raw[signalName] = text; }, { immediate: false });
  const encoded = batch({ x: 1 }, [1, 2, 3], "s");
  ctx.setSignalsJSON(encoded, false);
  assert.equal(parses(), 0, "the page decoded a batch that only raw subscribers asked for");
  assert.deepEqual(raw, { "$a": '{"x":1}', "$b": "[1,2,3]", "$c": '"s"' });
  assert.equal(state.calls.length, 1);
  assert.equal(state.calls[0].encoded, encoded, "the runtime must get the sender's text as it is");
  assert.equal(state.calls[0].changedOnly, false);
});

test("a signal is decoded once, when something first reads it", () => {
  const { ctx, parses } = page();
  ctx.setSignalsJSON(batch({ x: 1 }, [1, 2], "s"), false);
  assert.equal(parses(), 0);
  const b = ctx.getSignal("$b");
  assert.deepEqual(b, [1, 2]);
  assert.equal(parses(), 1, "only the signal that was read is decoded");
  assert.equal(ctx.getSignal("$b"), b, "a second read must return the decoded value");
  assert.equal(parses(), 1);
  assert.deepEqual(ctx.getSignal("$a"), { x: 1 });
  assert.equal(parses(), 2);
});

test("a value subscriber gets the decoded value, shared with later reads and with other subscribers", () => {
  const { ctx, parses } = page();
  const got = [];
  ctx.subscribeSignal("$a", (value, name) => got.push([name, value]), { immediate: false });
  ctx.subscribeSignal("$a", value => got.push(["again", value]), { immediate: false });
  let raw;
  ctx.subscribeSignalJSON("$b", text => { raw = text; }, { immediate: false });
  ctx.setSignalsJSON(batch({ x: 1 }, [1], 0), false);
  assert.equal(parses(), 1, "one decode serves every subscriber of the signal");
  assert.deepEqual(got[0], ["$a", { x: 1 }]);
  assert.equal(got[1][1], got[0][1]);
  assert.equal(ctx.getSignal("$a"), got[0][1]);
  assert.equal(raw, "[1]");
});

test("every signal of a batch can be read before any subscriber runs", () => {
  const { ctx } = page();
  ctx.setSignalsJSON(batch(1, 1, 1), false);
  const seen = [];
  ctx.subscribeSignal("$a", () => seen.push([ctx.getSignal("$b"), ctx.getSignal("$c")]), { immediate: false });
  ctx.subscribeSignalJSON("$c", () => seen.push(ctx.getSignal("$a")), { immediate: false });
  ctx.setSignalsJSON(batch(2, 3, 4), false);
  assert.deepEqual(seen, [[3, 4], 2]);
});

test("a changed-only batch tells the runtime and notifies nothing for what did not change", () => {
  const { ctx, state, parses } = page();
  const notified = [];
  for (const name of ["$a", "$b", "$c"]) ctx.subscribeSignalJSON(name, (text, signalName) => notified.push(signalName), { immediate: false });
  ctx.setSignalsJSON(batch(1, 2, 3), true);
  notified.length = 0;
  ctx.setSignalsJSON(batch(1, 9, 3), true);
  assert.deepEqual(notified, ["$b"]);
  assert.equal(state.calls[1].changedOnly, true);
  assert.equal(parses(), 0);
  assert.equal(ctx.getSignal("$a"), 1);
  assert.equal(ctx.getSignal("$b"), 9);
});

test("a newer batch replaces text that nobody read", () => {
  const { ctx, parses } = page();
  ctx.setSignalsJSON('{"$a":1}', false);
  ctx.setSignalsJSON('{"$a":2}', false);
  assert.equal(ctx.getSignal("$a"), 2);
  assert.equal(parses(), 1);
});

test("a decoded value is replaced by the next batch", () => {
  const { ctx, parses } = page();
  ctx.setSignalsJSON('{"$a":{"n":1}}', false);
  assert.deepEqual(ctx.getSignal("$a"), { n: 1 });
  ctx.setSignalsJSON('{"$a":{"n":2}}', false);
  assert.deepEqual(ctx.getSignal("$a"), { n: 2 });
  assert.equal(parses(), 2);
});

test("a batch that is not an object never reaches the runtime", () => {
  const { ctx, state } = page();
  for (const encoded of ["[]", "null", '"text"', "7", "true", "", "  [1]"]) {
    assert.throws(() => ctx.setSignalsJSON(encoded, false), /signal batch must be an object|JSON/, encoded);
  }
  assert.equal(state.calls.length, 0);
  assert.throws(() => ctx.setSignalsJSON(7, false), /must be JSON/);
});

test("the runtime refuses a batch, and the page keeps what it had", () => {
  const { ctx, state } = page();
  ctx.setSignalsJSON(batch(1, 2, 3), false);
  state.failure = "shared signal name required";
  assert.throws(() => ctx.setSignalsJSON(batch(7, 8, 9), false), /shared signal name required/);
  assert.equal(ctx.getSignal("$a"), 1);
  state.failure = "";
  assert.throws(() => ctx.setSignalsJSON('{"$a":', false), /decode shared signal batch/);
  assert.equal(ctx.getSignal("$a"), 1);
  assert.equal(ctx.getSignal("$b"), 2);
});

test("a blank name is refused before the runtime sees the batch", () => {
  const { ctx, state } = page();
  assert.throws(() => ctx.setSignals({ " ": 1 }), /signal name is required/);
  assert.equal(state.calls.length, 0);
});

test("setSignals sends the runtime the encoded object and decodes nothing", () => {
  const { ctx, state, parses } = page();
  const seen = [];
  ctx.subscribeSignalJSON("$a", text => seen.push(text), { immediate: false });
  ctx.setSignals({ "$a": { x: [1, 2] } });
  assert.equal(state.calls[0].encoded, '{"$a":{"x":[1,2]}}');
  assert.equal(state.calls[0].changedOnly, false);
  assert.deepEqual(seen, ['{"x":[1,2]}']);
  assert.equal(parses(), 0);
});

test("a page whose runtime does not announce batch delivery decodes the batch before it sends it", () => {
  const { ctx, state, parses } = page({ announce: false });
  const seen = [];
  ctx.subscribeSignal("$b", value => seen.push(value), { immediate: false });
  ctx.setSignalsJSON(batch(1, [5], 3), false);
  assert.deepEqual(seen, [[5]]);
  assert.equal(ctx.getSignal("$c"), 3);
  assert.ok(parses() >= 1, "the older runtime relies on the page to read the batch");
  assert.equal(state.calls.length, 1);
});

test("a notify function without a batch method gets the changes one at a time", () => {
  const { window, ctx, state, parses } = page();
  delete window.__gosx_notify_shared_signal.batch;
  const seen = [];
  ctx.subscribeSignal("$a", value => seen.push(value), { immediate: false });
  ctx.setSignalsJSON(batch(1, 2, 3), false);
  assert.deepEqual(seen, [1]);
  assert.equal(ctx.getSignal("$c"), 3);
  assert.equal(state.calls.length, 1);
  assert.ok(parses() >= 1);
});

test("the batch method delivers names and texts in pairs and skips blank names", () => {
  const { window, ctx, store } = page();
  const raw = [];
  ctx.subscribeSignalJSON("$a", (text, name) => raw.push([name, text]), { immediate: false });
  window.__gosx_notify_shared_signal.batch("$a", "1", "  ", "2", " $b ", "3", "$a", "4", "$dangling");
  assert.deepEqual(raw, [["$a", "1"], ["$a", "4"]]);
  assert.equal(ctx.getSignal("$a"), 4);
  assert.equal(ctx.getSignal("$b"), 3);
  assert.equal(store().values.has("$dangling"), false, "a name with no text is not a change");
});

test("a subscriber that throws does not stop the others", () => {
  const { window, ctx } = page();
  const errors = [];
  window.console.error = (...args) => errors.push(args[0]);
  const seen = [];
  ctx.subscribeSignal("$a", () => { throw new Error("boom"); }, { immediate: false });
  ctx.subscribeSignalJSON("$a", text => seen.push(text), { immediate: false });
  ctx.subscribeSignalJSON("$b", text => seen.push(text), { immediate: false });
  ctx.setSignalsJSON(batch(1, 2, 3), false);
  assert.deepEqual(seen, ["1", "2"]);
  assert.equal(errors.length, 1);
});

test("one notification at a time still decodes at once and notifies as before", () => {
  const { window, parses } = page();
  const calls = [];
  window.__gosx_subscribe_shared_signal("$x", (...args) => calls.push(args), { immediate: false });
  window.__gosx_notify_shared_signal("$x", '{"n":1}');
  assert.equal(parses(), 1);
  assert.deepEqual(calls, [[{ n: 1 }, "$x", '{"n":1}']]);
});

test("an unreadable text reads as null, as it did when the page decoded it first", () => {
  const { window, ctx } = page();
  window.__gosx_notify_shared_signal.batch("$bad", "{");
  assert.equal(ctx.getSignal("$bad"), null);
});

test("the store answers every Map method as if its values were already decoded", () => {
  const { window, ctx, parses, store } = page();
  const values = store().values;
  assert.ok(vm.runInContext("__gosx.sharedSignals.values instanceof Map", window), "the store must stay a Map");
  window.__gosx_notify_shared_signal.batch("$a", "1", "$b", '{"k":2}', "$c", "[3]");
  assert.equal(parses(), 0);
  assert.equal(values.has("$a"), true);
  assert.equal(values.has("$zzz"), false);
  assert.equal(parses(), 0, "has() must not decode");
  assert.equal(values.size, 3);
  assert.equal(parses(), 3, "size needs every value to exist");
  assert.deepEqual([...values.keys()], ["$a", "$b", "$c"]);
  assert.deepEqual([...values.values()], [1, { k: 2 }, [3]]);
  assert.deepEqual([...values].map(([name]) => name), ["$a", "$b", "$c"]);
  const each = [];
  values.forEach((value, name, map) => { each.push([name, map === values]); });
  assert.deepEqual(each, [["$a", true], ["$b", true], ["$c", true]]);
  assert.equal(parses(), 3);

  window.__gosx_notify_shared_signal.batch("$d", "4", "$a", "5");
  values.set("$e", "set directly");
  assert.equal(values.get("$a"), 5);
  assert.equal(values.get("$e"), "set directly");
  assert.equal(values.delete("$d"), true, "a staged value can be deleted");
  assert.equal(values.has("$d"), false);
  assert.equal(values.delete("$d"), false);
  window.__gosx_notify_shared_signal.batch("$f", "6");
  values.set("$f", "overwritten");
  assert.equal(values.get("$f"), "overwritten", "a direct write replaces staged text");
  window.__gosx_notify_shared_signal.batch("$g", "7");
  values.clear();
  assert.equal(values.size, 0);
  assert.equal(values.has("$g"), false);
  assert.equal(values.has("$a"), false);
  assert.equal(ctx.getSignal("$g"), null, "a cleared signal is read from the runtime again");
});

test("a store that is a plain Map still works, decoding when it is told of a change", () => {
  const { window, ctx, parses } = page();
  window.__gosx.sharedSignals = vm.runInContext("({ values: new Map(), subscribers: new Map(), nextID: 0 })", window);
  const seen = [];
  ctx.subscribeSignal("$a", value => seen.push(value), { immediate: false });
  window.__gosx_notify_shared_signal.batch("$a", '{"n":1}', "$b", "2");
  assert.deepEqual(seen, [{ n: 1 }]);
  assert.equal(parses(), 2);
  assert.equal(window.__gosx.sharedSignals.values.get("$b"), 2);
});

test("a value the runtime has but the page never heard of is read from the runtime", () => {
  const { ctx, state, parses } = page();
  state.values.set("$known", '{"z":1}');
  assert.deepEqual(ctx.getSignal("$known"), { z: 1 });
  assert.equal(parses(), 1);
  assert.equal(ctx.getSignal("$unknown"), null);
});
