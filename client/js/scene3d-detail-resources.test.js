"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs"), path = require("node:path"), vm = require("node:vm");
const { createContext, createWebGLRendererForPost } = require("./runtime-test-harness.js");
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


for (const fresh of [true, false]) test(`WebGL detail controls reuse atlases and source changes delete retired textures (fresh=${fresh})`, t => {
  const h = createWebGLRendererForPost({fresh});
  t.after(() => h.renderer.dispose());
  const gl = h.canvas.getContext("webgl2"), api = h.env.context.__gosx_scene3d_api;
  const atlases = [], deleted = new Set(), uniforms = [];
  let bound;
  const bind = gl.bindTexture.bind(gl), allocate = gl.texStorage3D.bind(gl), remove = gl.deleteTexture.bind(gl), upload = gl.uniform4fv.bind(gl);
  gl.bindTexture = (target,texture) => { if (target===gl.TEXTURE_2D_ARRAY) bound=texture; bind(target,texture); };
  gl.texStorage3D = (...args) => { if (args[3]===512 && args[4]===512 && args[5]===4) atlases.push(bound); allocate(...args); };
  gl.deleteTexture = texture => { if (atlases.includes(texture)) { assert.ok(!deleted.has(texture),"atlas is deleted once"); deleted.add(texture); } remove(texture); };
  gl.uniform4fv = (location,data) => { if (location?.name==="u_detail[0]") uniforms.push(Array.from(data)); upload(location,data); };
  const render = materials => {
    const objects = materials.map((material,i) => ({id:`detail-${i}`,kind:"mesh",...material,
      vertices:{count:3,positions:[-1,-1,0,1,-1,0,0,1,0],normals:[0,0,1,0,0,1,0,0,1],uvs:[0,0,1,0,0.5,1]}}));
    const bundle = api.createSceneRenderBundle(64,64,"#000000",{x:0,y:0,z:5,fov:60,near:0.1,far:100},objects,[],[],[],[],{},0,[],[],[],[],[],0,false);
    assert.equal(bundle.meshObjects.length,materials.length,"the fixture draws every detailed mesh");
    h.renderer.render(bundle,{width:64,height:64});
  };
  render([detailed("red",2), detailed("blue",7)]);
  assert.equal(atlases.length,1,"two materials share one source atlas");
  assert.deepEqual(uniforms.slice(-2).map(data=>data[0]),[2,7],"each material retains its own controls");
  assert.ok(gl.programs.some(program => program.attached.some(shader => shader.source.includes("DetailResult detailResult = detailApply"))),"the compiled detail shader retains its injection site");
  for(let i=0;i<60;i++) render([detailed("red",i+10)]);
  assert.equal(atlases.length,1,"60 control changes allocate no new atlas");
  assert.equal(deleted.size,0,"the shared source atlas remains live");
  assert.equal(uniforms.at(-1)[0],69);
  for(let i=0;i<12;i++) {
    render([detailed("red",3,`/source-${i}.png`)]);
    assert.equal(atlases.length-deleted.size,1,"changed sources retire the previous atlas");
  }
  render([{color:"plain"}]);
  assert.equal(deleted.size,atlases.length,"removing detail deletes every atlas before disposal");
  render([detailed("red"),detailed("blue",7,"/rock.png")]);
  h.renderer.dispose();
  assert.equal(deleted.size,atlases.length,"disposal releases both live source atlases");
});
