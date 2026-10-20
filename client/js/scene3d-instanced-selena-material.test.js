"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

function runtime() {
  const context = vm.createContext({ window: {}, console });
  for (const name of ["10-runtime-primitives.ts", "10-runtime-scene-utils.ts", "11-scene-math.ts", "12-scene-geometry.ts", "13-scene-material.ts", "10-runtime-scene-core.ts"]) {
    let source = fs.readFileSync(path.join(__dirname, "bootstrap-src", name), "utf8");
    if (name === "10-runtime-scene-core.ts") source = source.slice(0, source.indexOf("// Scene3D shared API"));
    vm.runInContext(source, context, { filename: name });
  }
  return context;
}

test("instanced GLB normalization and hydration preserve compiled Selena material", () => {
  const api = runtime();
  const raw = {
    id: "heroes",
    src: "/hero.glb",
    materialKind: "custom",
    customVertex: "attribute vec3 position; void main(){gl_Position=vec4(position,1.0);}",
    customFragment: "precision mediump float; void main(){gl_FragColor=vec4(1.0);}",
    customVertexWGSL: "@vertex fn vertexMain()->@builtin(position) vec4<f32>{return vec4<f32>();}",
    customFragmentWGSL: "@fragment fn fragmentMain()->@location(0) vec4<f32>{return vec4<f32>(1.0);}",
    customUniforms: { albedo: "/hero-albedo.png", threshold: 0.1 },
    shaderBackend: "selena",
    shaderLayout: { material: "HeroGraphic", uniformBlock: { fields: [{ name: "mvp", type: "mat4" }] } },
    shaderSource: "material HeroGraphic",
    shaderSourceFiles: { "hero.sel": "material HeroGraphic" },
    instances: [{ id: "vesper", animation: "Idle", animationTime: 0.25, animationLoop: true }],
  };

  const batch = api.normalizeSceneInstancedGLBMeshEntry(raw, 0, null);
  const model = api.sceneInstancedGLBMeshToModels(batch, 0)[0];
  for (const key of ["customVertex", "customFragment", "customVertexWGSL", "customFragmentWGSL", "shaderBackend", "shaderSource"]) {
    assert.equal(batch[key], raw[key], "batch " + key);
    assert.equal(model.materialOverride[key], raw[key], "model override " + key);
  }
  assert.deepEqual(JSON.parse(JSON.stringify(model.materialOverride.customUniforms)), raw.customUniforms);
  assert.deepEqual(JSON.parse(JSON.stringify(model.materialOverride.shaderLayout)), raw.shaderLayout);
  assert.deepEqual(JSON.parse(JSON.stringify(model.materialOverride.shaderSourceFiles)), raw.shaderSourceFiles);
  assert.equal(model._instancedGLB, true);
  assert.deepEqual(JSON.parse(JSON.stringify(model._crowdPose)), { animation: "Idle", animationTime: 0.25, animationLoop: true });

  batch.customUniforms.threshold = 0.9;
  batch.shaderLayout.material = "Changed";
  assert.equal(raw.customUniforms.threshold, 0.1, "normalization clones custom uniforms");
  assert.equal(raw.shaderLayout.material, "HeroGraphic", "normalization clones shader layout");
});

test("instanced GLB Selena material survives inherited command normalization", () => {
  const api = runtime();
  const fallback = {
    id: "heroes",
    src: "/hero.glb",
    materialKind: "custom",
    shaderBackend: "selena",
    customVertex: "vertex",
    customFragment: "fragment",
    customUniforms: { albedo: "/first.png" },
    shaderLayout: { material: "HeroGraphic" },
    instances: [{ id: "old" }],
  };
  const batch = api.normalizeSceneInstancedGLBMeshEntry({ instances: [{ id: "new" }] }, 0, fallback);
  const model = api.sceneInstancedGLBMeshToModels(batch, 0)[0];
  assert.equal(model.materialOverride.shaderBackend, "selena");
  assert.equal(model.materialOverride.customVertex, "vertex");
  assert.equal(model.materialOverride.customFragment, "fragment");
  assert.equal(model.materialOverride.customUniforms.albedo, "/first.png");
  assert.equal(model.materialOverride.shaderLayout.material, "HeroGraphic");
});

test("hoisted instanced GLB Selena shaders inflate before normalization and hydration", () => {
  const api = runtime();
  const wire = {
    shaderLib: { vertex: "compiled vertex", fragment: "compiled fragment" },
    instancedGLBMeshes: ["a", "b"].map(id => ({
      id,
      src: "/hero.glb",
      materialKind: "custom",
      shaderBackend: "selena",
      shaderLayout: { material: "HeroGraphic", uniformBlock: { fields: [] } },
      customVertexRef: "vertex",
      customFragmentRef: "fragment",
      instances: [{ id }],
    })),
  };
  api.inflateSceneShaderLib(wire);
  assert.equal(Object.prototype.hasOwnProperty.call(wire, "shaderLib"), false);
  for (let index = 0; index < wire.instancedGLBMeshes.length; index += 1) {
    const raw = wire.instancedGLBMeshes[index];
    assert.equal(raw.customVertex, "compiled vertex");
    assert.equal(raw.customFragment, "compiled fragment");
    assert.equal(Object.prototype.hasOwnProperty.call(raw, "customVertexRef"), false);
    assert.equal(Object.prototype.hasOwnProperty.call(raw, "customFragmentRef"), false);
    const batch = api.normalizeSceneInstancedGLBMeshEntry(raw, index, null);
    const model = api.sceneInstancedGLBMeshToModels(batch, index)[0];
    assert.equal(model.materialOverride.customVertex, "compiled vertex");
    assert.equal(model.materialOverride.customFragment, "compiled fragment");
    assert.equal(model.materialOverride.shaderBackend, "selena");
  }
});
