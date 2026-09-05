"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { bootstrapRuntimeSource, freshFeatureBundleSource, createContext, FakeElement, runScript } = require("./runtime-test-harness.js");

test("numeric mesh depth changes retain CSS resolution while draw planning stays current", () => {
  let reads = 0;
  const env = createContext({ getComputedStyle(element) { reads++; return element.computedStyle || {}; } });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSource("scene3d"), env.context, "bootstrap-feature-scene3d.js");
  const api = env.context.__gosx_scene3d_api;
  const mount = new FakeElement("div", null);
  mount.computedStyle = { "--tint": "#abcdef", "--depth": "6" };
  const bundle = {
    camera: { x: 0, y: 0, z: 6, fov: 72, near: .05, far: 128 }, environment: {},
    materials: [{ kind: "standard", color: "var(--tint)", opacity: .5, renderPass: "alpha" }],
    meshObjects: [{ id: "actor", kind: "gltfmesh", materialIndex: 0, vertexOffset: 0, vertexCount: 3, depthCenter: 4 }],
    worldPositions: new Float32Array(0), worldColors: new Float32Array(0),
    worldMeshPositions: new Float32Array(9), worldMeshNormals: new Float32Array(9),
  };
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const context = { mount, revision: 1 };
  let prepared = api.prepareScene(bundle, bundle.camera, viewport, null, context);
  const cssCache = prepared.cssCache, initialReads = reads;
  for (let i = 0; i < 30; i++) {
    bundle.meshObjects[0].depthCenter = 10 + i;
    const next = api.prepareScene(bundle, bundle.camera, viewport, prepared, context);
    assert.equal(next.cssCache, cssCache);
    assert.equal(reads, initialReads);
    assert.equal(next.ir.meshObjects[0].depthCenter, 10 + i);
    assert.notEqual(next.signature, prepared.signature);
    prepared = next;
  }
  bundle.meshObjects[0].depthCenter = "var(--depth)";
  prepared = api.prepareScene(bundle, bundle.camera, viewport, prepared, context);
  assert.notEqual(prepared.cssCache, cssCache);
  assert.equal(prepared.ir.meshObjects[0].depthCenter, 6);
  mount.computedStyle["--depth"] = "8";
  prepared = api.prepareScene(bundle, bundle.camera, viewport, prepared, { mount, revision: 2 });
  assert.equal(prepared.ir.meshObjects[0].depthCenter, 8);
  bundle.meshObjects[0].depthCenter = 21;
  prepared = api.prepareScene(bundle, bundle.camera, viewport, prepared, { mount, revision: 2 });
  assert.equal(prepared.ir.meshObjects[0].depthCenter, 21);
});
