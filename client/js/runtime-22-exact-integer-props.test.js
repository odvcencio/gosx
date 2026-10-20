"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const {
  bootstrapSource, bootstrapRuntimeSource, bootstrapFeatureIslandsSource,
  FakeElement, createContext, runScript, flushAsyncWork,
} = require("./runtime-test-harness.js");

for (const [name, source] of [["monolithic", bootstrapSource], ["selective", bootstrapRuntimeSource]]) {
  for (const compute of [false, true]) {
    test(`${name} passes exact integer props to ${compute ? "compute" : "DOM"} island WASM`, async () => {
      const raw = '{"ID":9007199254740993,"nested":[9223372036854775807,-9223372036854775808]}';
      const root = new FakeElement("div", null);
      root.id = "exact-integers";
      root.appendChild(new FakeElement("p", null));
      const env = createContext({
        elements: compute ? [] : [root],
        fetchRoutes: {
          "/runtime.wasm": {bytes:[0,97,115,109]},
          "/exact.json": {text:'{"name":"ExactIntegers"}'},
          "/gosx/bootstrap-feature-islands.js": {text:bootstrapFeatureIslandsSource},
        },
        manifest: {
          runtime: {path:"/runtime.wasm"},
          [compute ? "computeIslands" : "islands"]: [{
            id:root.id, component:"ExactIntegers", programRef:"/exact.json",
            props:JSON.parse(raw), propsJSON:raw,
          }],
        },
      });
      runScript(source,env.context,"bootstrap.js");
      await flushAsyncWork();
      const calls = compute ? env.computeHydrateCalls : env.hydrateCalls;
      assert.equal(calls.length,1);
      assert.equal(calls[0][2],raw);
      assert.notEqual(JSON.stringify(JSON.parse(raw)),raw,"fixture must expose JS rounding");
      assert.deepEqual(env.consoleLogs.error,[]);
    });
  }
}
