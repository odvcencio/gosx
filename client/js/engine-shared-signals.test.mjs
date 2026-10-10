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
    gosxNotifySharedSignal(name, raw) { values.set(name, JSON.parse(raw)); for (const callback of listeners.get(name) || []) callback(values.get(name)); },
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
