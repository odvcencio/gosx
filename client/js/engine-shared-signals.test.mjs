import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";

const source = fs.readFileSync(new URL("./bootstrap-src/30b-tail-engine-mounting.ts", import.meta.url), "utf8");
const contextSource = source.slice(source.indexOf("function createEngineContext("), source.indexOf("async function mountEngine("));

test("engine context bridges shared signals without replay and releases subscriptions", () => {
  const values = new Map([["$ray", { requestId: "old" }]]), listeners = new Map();
  const context = {
    requiredCapabilityList: () => [], engineCapabilityStatus: () => ({}), pendingEngineOwned: () => true,
    setSharedSignalValue(name, value) {
      values.set(name, value);
      for (const callback of listeners.get(name) || []) callback(value);
      return "";
    },
    gosxSubscribeSharedSignal(name, callback, options) {
      assert.equal(options.immediate, false);
      const handlers = listeners.get(name) || new Set(); handlers.add(callback); listeners.set(name, handlers);
      return () => handlers.delete(callback);
    },
  };
  vm.createContext(context); vm.runInContext(contextSource, context);
  const ctx = context.createEngineContext({ id: "picker", component: "Picker" }, {}, {}, { required: ["wasm"] }, {});
  assert.deepEqual(ctx.requiredCapabilities, ["wasm"]);
  let calls = 0;
  const dispose = ctx.subscribeSignal("$ray", request => {
    calls++; ctx.setSignal("$hit", { requestId: request.requestId, hit: { id: "end" } });
  }, { immediate: false });
  assert.equal(calls, 0);
  ctx.setSignal("$ray", { requestId: "new" });
  assert.deepEqual(values.get("$hit"), { requestId: "new", hit: { id: "end" } });
  dispose(); ctx.setSignal("$ray", { requestId: "late" });
  assert.equal(calls, 1); assert.equal(listeners.get("$ray").size, 0);
});

function mountedHarness(nativeBatch = false) {
  const values = new Map([["$left", 1], ["$right", 2]]), listeners = new Map(), engines = new Map();
  const pending = {generation: 1, closed: false};
  const scope = {
    window: {__gosx: {sharedSignals: {values}, engines}},
    goWASMEnginePageGeneration: 1,
    pendingEngineOwned: value => !value.closed,
    gosxReadSharedSignal: name => values.get(name),
    gosxNotifySharedSignal(name, raw) { values.set(name, JSON.parse(raw)); for (const callback of listeners.get(name) || []) callback(values.get(name), name, raw); },
    setSharedSignalValue(name, value) { scope.gosxNotifySharedSignal(name, JSON.stringify(value)); },
    gosxSubscribeSharedSignal(name, fn) { const set = listeners.get(name) || new Set(); listeners.set(name, set); set.add(fn); return () => set.delete(fn); },
  };
  if (nativeBatch) scope.window.__gosx_set_input_batch = raw => { for (const [name, value] of Object.entries(JSON.parse(raw))) scope.gosxNotifySharedSignal(name, JSON.stringify(value)); };
  vm.createContext(scope); vm.runInContext(contextSource, scope);
  const ctx = scope.createEngineContext({id:"test"}, {}, {}, {required:[]}, pending);
  return {ctx, scope, values, pending, engines};
}

for (const native of [false, true]) test(`engine signal batch observers see complete state with native VM=${native}`, () => {
  const {ctx, values, scope} = mountedHarness(native);
  let observed;
  ctx.subscribeSignal("$left", () => { observed = ctx.getSignal("$right"); });
  ctx.setSignals({"$left":3, "$right":4});
  assert.equal(observed, 4);
  assert.equal(values.get("$left"), 3);
  if (native) {
    scope.window.__gosx_set_input_batch = () => "invalid signal";
    assert.throws(() => ctx.setSignals({"$left":9,"$right":10}), /invalid signal/);
    assert.equal(ctx.getSignal("$left"),3); assert.equal(ctx.getSignal("$right"),4);
  }
});

test("published engine context stays usable and replacement revokes every bridge", async () => {
  const {ctx, scope, pending, engines} = mountedHarness();
  pending.closed = true;
  engines.set("test", {context:ctx, disposed:false});
  assert.equal(ctx.isCurrent(),true);
  let notifications=0, navigations=0, commands=0;
  const unsubscribe=ctx.subscribeSignal("$left", () => notifications++);
  scope.window.__gosx.navigation={navigate: async () => {navigations++;}};
  scope.window.__gosx.scene3d={whenReady: async () => {commands++;}};
  ctx.setSignals({"$left":5}); await ctx.navigate("/next",{}); await ctx.scene3D("whenReady",{});
  assert.equal(notifications,1); assert.equal(navigations,1); assert.equal(commands,1);
  engines.set("test", {context:{},disposed:false});
  assert.equal(ctx.isCurrent(),false);
  assert.throws(() => ctx.setSignal("$left",6),/disposed/);
  assert.throws(() => ctx.setSignals({"$left":6}),/disposed/);
  assert.equal(ctx.getSignal("$left"),undefined);
  await assert.rejects(ctx.navigate("/late",{}),/disposed/);
  assert.throws(() => ctx.scene3D("whenReady",{}),/disposed/);
  scope.gosxNotifySharedSignal("$left","7");
  assert.equal(notifications,1); assert.equal(navigations,1); assert.equal(commands,1);
  unsubscribe();
});

for (const native of [false, true]) test(`encoded state batches skip browser stringify with native VM=${native}`, () => {
  const {ctx, scope, values} = mountedHarness(native);
  const raw = '{ "$left": 3, "$right": {"turn":"west"} }';
  let observed, submitted, flag;
  ctx.subscribeSignalJSON("$left", value => { observed = value; assert.equal(ctx.getSignal("$right").turn,"west"); }, {immediate:false});
  if (native) {
    scope.JSON = {parse: JSON.parse, stringify() { throw new Error("redundant stringify"); }};
    scope.window.__gosx_set_input_batch = (value, changedOnly) => {
      submitted=value; flag=changedOnly;
      for (const [name, field] of Object.entries(JSON.parse(value))) scope.gosxNotifySharedSignal(name,JSON.stringify(field));
    };
  }
  ctx.setSignalsJSON(raw,true);
  assert.equal(observed,"3"); assert.equal(values.get("$right").turn,"west");
  if (native) { assert.equal(submitted,raw); assert.equal(flag,true); }
  else {
    observed=null; ctx.setSignalsJSON(raw,true);
    assert.equal(observed,null,"identical snapshot notified in fallback");
    values.get("$right").turn="east";
    ctx.setSignalsJSON(raw,true);
    assert.equal(values.get("$right").turn,"west","live edit was concealed");
    ctx.setSignals({"$left":3}); assert.equal(observed,"3","pulse delivery was suppressed");
  }
});

test("encoded signal batches validate and roll back before notifying", () => {
  const {ctx, values, scope} = mountedHarness(true);
  for (const raw of ['{', '[]', 'null', '{"":1,"$left":9}']) assert.throws(() => ctx.setSignalsJSON(raw,true));
  assert.equal(values.get("$left"),1);
  scope.window.__gosx_set_input_batch=()=>"rejected snapshot";
  assert.throws(()=>ctx.setSignalsJSON('{"$left":9,"$new":1}',true),/rejected snapshot/);
  assert.equal(values.get("$left"),1); assert.equal(values.has("$new"),false);
});

test("replaced contexts revoke encoded writes and raw subscriptions", () => {
  const {ctx, pending, engines} = mountedHarness();
  pending.closed=true; engines.set("test",{context:{},disposed:false});
  assert.throws(()=>ctx.setSignalsJSON('{"$left":9}',true),/disposed/);
  assert.throws(()=>ctx.subscribeSignalJSON("$left",()=>{}),/disposed/);
});

test("shared store forwards the original encoded value to raw subscribers", () => {
  const text = fs.readFileSync(new URL("./bootstrap-src/00-textlayout.ts", import.meta.url), "utf8");
  const notify = text.slice(text.indexOf("function gosxNotifySharedSignal("),text.indexOf("function gosxSubscribeSharedSignal("));
  const listeners = new Map(); let got;
  listeners.set("raw",(...args)=>{got=args});
  const store = {values:new Map(),subscribers:new Map([["$view",listeners]])};
  const scope={gosxSharedSignalStore:()=>store,parseSharedSignalJSON:JSON.parse};
  vm.createContext(scope); vm.runInContext(notify,scope);
  const raw='{ "turn": "west" }'; scope.gosxNotifySharedSignal("$view",raw);
  assert.equal(got[0].turn,"west"); assert.equal(got[1],"$view"); assert.equal(got[2],raw);
});

// An engine that answers calls from a goroutine (wasm.ServeCallsAsync) asks the
// page to drop the answered calls from a mailbox. The page reads and rewrites the
// signal in one step, so a call that an island appends cannot slip in between.
test("engine context drops the answered calls of a mailbox and leaves anything else alone", () => {
  const {ctx, values} = mountedHarness();
  values.set("$bv.cmd", [{ID: 1, Name: "a"}, {ID: 2, Name: "b"}, null, {ID: "x"}, 7, {ID: 3, Name: "c"}]);
  const seen = [];
  ctx.subscribeSignal("$bv.cmd", value => seen.push(value));
  assert.equal(ctx.trimCalls("$bv.cmd", 2), true);
  assert.deepEqual(values.get("$bv.cmd"), [{ID: 3, Name: "c"}], "calls 1 and 2 are answered, and an entry with no usable ID can never be");
  assert.deepEqual(seen, [[{ID: 3, Name: "c"}]], "the subscribers are told once");
  assert.equal(ctx.trimCalls("$bv.cmd", 2), false, "nothing left to drop");
  assert.equal(seen.length, 1, "a trim that changes nothing writes nothing");
  assert.equal(ctx.trimCalls("$bv.cmd", 3), true);
  assert.deepEqual(values.get("$bv.cmd"), []);
  // A signal that holds one call is not a mailbox, and an absent signal has nothing to drop.
  values.set("$bv.one", {ID: 1, Name: "a"});
  assert.equal(ctx.trimCalls("$bv.one", 9), false);
  assert.deepEqual(values.get("$bv.one"), {ID: 1, Name: "a"});
  assert.equal(ctx.trimCalls("$bv.nothing", 9), false);
});

test("a replaced context refuses to trim a mailbox", () => {
  const {ctx, pending, engines, values} = mountedHarness();
  values.set("$bv.cmd", [{ID: 1}]);
  pending.closed = true; engines.set("test", {context: {}, disposed: false});
  assert.throws(() => ctx.trimCalls("$bv.cmd", 1), /disposed/);
  assert.deepEqual(values.get("$bv.cmd"), [{ID: 1}]);
});
