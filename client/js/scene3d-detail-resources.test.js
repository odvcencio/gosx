"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs"), path = require("node:path"), vm = require("node:vm");
const { createContext } = require("./runtime-test-harness.js");
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");

function detailRenderer() {
  const buffers = [], textures = [], writes = [];
  const resource = (list, descriptor) => {
    const value = { descriptor, destroyed: false, destroy() { assert.equal(this.destroyed, false); this.destroyed = true; }, createView: () => ({}) };
    list.push(value); return value;
  };
  const device = {
    createBuffer: d => resource(buffers, d), createTexture: d => resource(textures, d),
    createBindGroup: d => d, createBindGroupLayout: d => d, createPipelineLayout: d => d,
    createShaderModule: d => d, createSampler: d => d,
    queue: { writeBuffer: (buffer, offset, data) => writes.push({ buffer, data: Array.from(data) }) },
  };
  const c = createContext({}).context;
  Object.assign(c, { device, frameMeta: {}, bundle: {}, textureCache: {}, placeholderView: {},
    frameBindGroupLayout: {}, materialBindGroupLayout: {}, WGSL_PBR_FRAGMENT: "", detailResources: undefined,
    GPUShaderStage: { FRAGMENT: 2 }, GPUTextureUsage: { TEXTURE_BINDING: 1, RENDER_ATTACHMENT: 2 },
    GPUBufferUsage: { UNIFORM: 1, COPY_DST: 2 }, wgpuLoadTexture: () => null });
  for (const file of ["client/js/bootstrap-src/10-runtime-primitives.ts", "client/js/bootstrap-src/10-runtime-scene-utils.ts", "client/js/bootstrap-src/11-scene-math.ts", "client/js/bootstrap-src/13-scene-material.ts", "client/js/bootstrap-src/16c1-scene-detail.ts"]) {
    vm.runInContext(fs.readFileSync(path.join(__dirname, "../..", file), "utf8"), c);
  }
  const renderer = readSceneRendererBackendSrc("webgpu");
  vm.runInContext(renderer, c);
  c.sceneWebGPUBakeDetailAtlas = () => {};
  c.wgpuLoadTexture = () => null;
  const start = renderer.indexOf("      detailEnabled = !frameMeta");
  const end = renderer.indexOf("      var frameNowMS", start);
  assert.ok(start >= 0 && end > start);
  const frame = new vm.Script(renderer.slice(start, end));
  return { c, buffers, textures, writes, render(materials) { c.bundle = { materials }; frame.runInContext(c); } };
}

const detailed = (color, scale = 3, texture = "/sand.png") => ({ color, detail: { ground: { albedo: texture, scale }, fadeStart: 5, fadeEnd: 12 } });
const liveUniforms = r => r.buffers.filter(b => b.descriptor.label === "detail-uniform" && !b.destroyed);

test("WebGPU detail color animation reuses one uniform buffer for 12 frames", () => {
  const r = detailRenderer();
  for (let i = 0; i < 12; i++) r.render([detailed(`#${i}`)]);
  assert.equal(liveUniforms(r).length, 1);
  assert.equal(r.buffers.filter(b => b.descriptor.label === "detail-uniform").length, 1);
  assert.equal(r.c.detailResources.materials.size, 1);
  assert.ok(Array.from(r.c.detailResources.materials.keys()).every(key => typeof key === "string"));
});

test("WebGPU detail shares atlases across controls and retires changed or removed resources", () => {
  const r = detailRenderer();
  r.render([detailed("red", 2), detailed("blue", 7)]);
  assert.equal(liveUniforms(r).length, 2);
  assert.equal(r.textures.filter(t => !t.destroyed).length, 1);
  const groups = Array.from(r.c.detailResources.materials.values()).map(e => e.group);
  assert.notEqual(groups[0], groups[1]);
  assert.deepEqual(r.writes.filter(w => w.buffer.descriptor.label === "detail-uniform").map(w => w.data[0]), [2, 7]);
  for (let i = 0; i < 12; i++) {
    r.render([detailed("red", i + 10)]);
    assert.equal(liveUniforms(r).length, 1);
    assert.equal(r.buffers.filter(b => b.descriptor.label === "detail-uniform").length, 2);
    assert.equal(r.writes.at(-1).data[0], i + 10);
  }
  for (let i = 0; i < 12; i++) {
    r.render([detailed("red", i + 10, `/sand-${i}.png`)]);
    assert.equal(liveUniforms(r).length, 1);
    assert.equal(r.textures.filter(t => !t.destroyed).length, 1);
    assert.equal(r.buffers.filter(b => !b.destroyed).length, 5);
    assert.equal(r.writes.at(-1).data[0], i + 10);
  }
  r.render([{ color: "plain" }]);
  assert.equal(r.buffers.filter(b => !b.destroyed).length, 0);
  assert.equal(r.textures.filter(t => !t.destroyed).length, 0);
  assert.equal(r.c.detailResources.materials.size, 0);
  assert.equal(r.c.detailResources.atlases.size, 0);
});

test("WebGPU detail disposal releases every resource and is repeatable", () => {
  const r = detailRenderer();
  r.render([detailed("red"), detailed("blue", 7, "/rock.png")]);
  r.c.sceneWebGPUDisposeDetail(r.c.detailResources);
  assert.equal(r.buffers.filter(b => !b.destroyed).length, 0);
  assert.equal(r.textures.filter(t => !t.destroyed).length, 0);
  assert.equal(r.c.detailResources.materials.size, 0);
  assert.equal(r.c.detailResources.atlases.size, 0);
  r.c.sceneWebGPUDisposeDetail(r.c.detailResources);
});
