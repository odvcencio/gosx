"use strict";
// gosx dev injects selenaSwapScript (dev/selena.go). On a "selena" event it
// sends each live Scene3D object whose Selena material was rebuilt a
// SetMaterial command with the new shaders.
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const goSource = fs.readFileSync(path.join(__dirname, "../../dev/selena.go"), "utf8");
const script = goSource.match(/const selenaSwapScript = `<script[^>]*>([\s\S]*?)<\/script>`/)[1];

function harness(objects) {
  const listeners = {};
  const dispatched = [];
  const mount = { __gosxScene3DState: { objects: new Map(objects.map(o => [o.id, o])) } };
  const window = {
    __gosx: { scene3d: { dispatchCommands(target, commands) { dispatched.push({ target, commands }); return Promise.resolve(); } } },
    __gosxDevEvents: { addEventListener(name, fn) { listeners[name] = fn; } },
  };
  const context = vm.createContext({ window, document: { querySelectorAll: () => [mount] }, console: { debug() {}, error() {} }, JSON, EventSource: function() { throw new Error("must reuse the dev EventSource"); } });
  vm.runInContext(script, context);
  return { window, listeners, dispatched, mount };
}

const lavaLayout = { material: "Lava", uniformBlock: { fields: [] } };
const objects = () => [
  { id: "rock", shaderBackend: "selena", shaderLayout: lavaLayout, customFragment: "old", customUniforms: { heat: 2 } },
  { id: "glow", shaderBackend: "selena", shaderLayout: lavaLayout, customFragment: "old", specularFragmentGLSL: "old-spec" },
  { id: "ice", shaderBackend: "selena", shaderLayout: { material: "Ice" }, customFragment: "ice" },
  { id: "crate", materialKind: "standard", color: "#884400" },
];
const update = { source: "materials/lava.sel", materials: [{
  material: "Lava", customVertex: "v2", customFragment: "f2", customVertexWGSL: "wv2", customFragmentWGSL: "wf2",
  specularFragmentGLSL: "s2", specularFragmentWGSL: "ws2", shaderLayout: { material: "Lava", uniformBlock: { fields: [{ name: "heat" }] } },
}] };

test("a selena event patches only objects that use the rebuilt material", async () => {
  const h = harness(objects());
  h.listeners.selena({ data: JSON.stringify(update) });
  assert.equal(h.dispatched.length, 1, "one command batch per mount");
  const { target, commands } = h.dispatched[0];
  assert.equal(target, h.mount);
  assert.equal(JSON.stringify(commands.map(c => [c.kind, c.objectId])), JSON.stringify([[3, "rock"], [3, "glow"]]));
  const rock = commands[0].data;
  assert.equal(rock.customFragment, "f2");
  assert.equal(rock.customFragmentWGSL, "wf2");
  assert.equal(rock.shaderLayout.uniformBlock.fields[0].name, "heat");
  assert.equal("customUniforms" in rock, false, "uniform values stay the app's");
  assert.equal("specularFragmentGLSL" in rock, false, "objects without the specular variant do not gain it");
  assert.equal(commands[1].data.specularFragmentGLSL, "s2");
});

test("the swap reports how many objects it patched and ignores unknown materials", () => {
  const h = harness(objects());
  assert.equal(h.window.__gosx_dev_selena_swap({ materials: [{ material: "Nope" }] }), 0);
  assert.equal(h.dispatched.length, 0);
  assert.equal(h.window.__gosx_dev_selena_swap(update), 2);
});
