const assert = require("node:assert/strict");
const fs = require("node:fs");
globalThis.crypto ||= require("node:crypto").webcrypto;
require(process.argv[2]);
const events = [];
const children = [];
let button;
globalThis.document = {
  createElement(tag) {
    assert.equal(tag, "button");
    const listeners = new Map();
    button = {
      listeners,
      addEventListener(name, fn) { listeners.set(name, fn); },
      removeEventListener(name, fn) { assert.equal(listeners.get(name), fn); listeners.delete(name); },
    };
    return button;
  },
};
let factory;
globalThis.__gosx_register_go_wasm_engine_factory = (token, name, fn) => {
  assert.equal(token, "fixture");
  assert.equal(name, "BudgetEngine");
  assert.equal(factory, undefined, "module registered its factory twice");
  factory = fn;
  return true;
};

(async () => {
  const go = new Go();
  go.env = { GOSX_GO_WASM_REGISTRATION_TOKEN: "fixture" };
  const module = await WebAssembly.instantiate(fs.readFileSync(process.argv[3]), go.importObject);
  go.run(module.instance).catch(error => { console.error(error); process.exitCode = 1; });
  assert.equal(typeof factory, "function", "real Go-WASM module failed to register");
  const handle = await factory({
    id: "fixture", kind: "surface", component: "BudgetEngine", props: {},
    mount: { replaceChildren(...nodes) { children.splice(0, children.length, ...nodes); } },
    emit(name, data) { events.push([name, data.usable]); },
  });
  assert.equal(children.length, 1);
  assert.equal(button.textContent, "Count: 0");
  assert.deepEqual(events, [["first-tick", true]]);
  button.listeners.get("click")();
  button.listeners.get("click")();
  assert.equal(button.textContent, "Count: 2");
  handle.dispose();
  assert.equal(button.listeners.size, 0);
  assert.equal(children.length, 0);
  assert.equal(events.length, 1);
})().catch(error => { console.error(error); process.exitCode = 1; });
