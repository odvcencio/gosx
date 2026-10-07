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
