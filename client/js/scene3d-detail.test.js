"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs"), path = require("node:path"), vm = require("node:vm");
const { createContext } = require("./runtime-test-harness.js");
const crypto = require("node:crypto");
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");
const ts = require("node:module").createRequire(path.join(__dirname, "../runtime/package.json"))("typescript");

function detailContext(backend) {
  const env = createContext({});
  for (const file of ["10-runtime-primitives.ts", "10-runtime-scene-utils.ts", "11-scene-math.ts", "13-scene-material.ts", "16c1-scene-detail.ts"]) {
    vm.runInContext(fs.readFileSync(path.join(__dirname, "bootstrap-src", file), "utf8"), env.context);
  }
  const core = fs.readFileSync(path.join(__dirname, "bootstrap-src", "10-runtime-scene-core.ts"), "utf8");
  vm.runInContext(core.slice(0, core.indexOf("// Scene3D shared API")), env.context);
  if (backend) vm.runInContext(ts.transpileModule(readSceneRendererBackendSrc(backend), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, env.context);
  return env.context;
}

test("detail defaults, explicit zero/false and exact material identity", () => {
  const c = detailContext();
  const d = c.sceneNormalizeDetail({ ground: {}, steep: {}, stochastic: false });
  assert.equal(d.ground.scale, 2); assert.equal(d.ground.normalScale, 1);
  assert.equal(d.ground.albedoMix, 0.6); assert.equal(d.ground.roughnessMix, 0.5);
  assert.equal(d.fadeStart, 8); assert.equal(d.fadeEnd, 14);
  assert.equal(d.slopeStart, 30); assert.equal(d.slopeEnd, 45);
  assert.equal(d.stochastic, false); assert.equal(d.triplanar, null);
  assert.equal(c.sceneNormalizeDetailLayer({ albedoMix: 0 }).albedoMix, 0);
  assert.notEqual(c.sceneMaterialProfileKey({ detail: d }), c.sceneMaterialProfileKey({}));
});

test("detail packs scales, fade, slope, projection, readiness and quality", () => {
  const c = detailContext();
  const data = c.sceneDetailUniformData({ ground: { scale: 3 }, steep: { scale: 4 } }, [1, 0, 1, 0, 1, 0], true);
  assert.equal(data.length, 24); assert.equal(data[0], 3); assert.equal(data[4], 4);
  assert.equal(data[8], 8); assert.equal(data[9], 14);
  assert.ok(Math.abs(data[10] - Math.PI / 6) < 1e-7);
  assert.ok(Math.abs(data[11] - Math.PI / 4) < 1e-7);
  assert.deepEqual(Array.from(data.slice(12, 16)), [1, 1, 0, 1]);
  assert.equal(data[19], 1);
  assert.equal(c.sceneDetailUniformData({ ground: {} }, [], false)[19], 0);
  assert.equal(c.sceneDetailQualityEnabled({ mode: "ladder", ladder: [{}], rungIndex: 0 }), false);
  assert.equal(c.sceneDetailQualityEnabled({ mode: "ladder", ladder: [{ detail: true }], rungIndex: 0 }), true);
  assert.equal(c.sceneDetailQualityEnabled({ enabled: true, activeProfile: { detail: false } }), false);
});

test("missing, failed and pending detail textures stay neutral through existing loaders", () => {
  const c = detailContext(); const urls = [];
  const result = c.sceneDetailTextureRecords({ ground: { albedo: "/a.ktx2", normal: "/n.png", roughness: "/r.jpg" } }, (url, role) => {
    urls.push([url, role]); return { loaded: role !== "normal", failed: role === "roughness" };
  });
  assert.deepEqual(Array.from(result.masks), [1, 0, 0, 0, 0, 0]);
  assert.deepEqual(urls, [["/a.ktx2", "albedo"], ["/n.png", "normal"], ["/r.jpg", "roughness"]]);
});

test("WebGL detail is a cached compile variant with nil source unchanged", () => {
  const c = detailContext("webgl"); const base = vm.runInContext("SCENE_PBR_FRAGMENT_SOURCE", c);
  assert.equal(c.sceneWebGLDetailFragment(base, null), base);
  // Main preserves signed normal-map scales in the base shader; nil detail stays neutral.
  assert.equal(crypto.createHash("sha256").update(base).digest("hex"), "ee9ad3c6e7b48e3c0f5444afba4fc5a907bc3008674df15b02b1e3d41cd4008b");
  const shader = c.sceneWebGLDetailFragment(base, true);
  assert.match(shader, /textureGrad\(u_detailAtlas/);
  assert.ok(shader.indexOf("dFdx(v_worldPosition)") < shader.indexOf("detailApply(v_worldPosition"));
  assert.match(shader, /fade > 0\.0 && u_detail\[4\]\.w > 0\.5/);
  let compilations = 0;
  c.createScenePBRProgram = () => { compilations++; return { program: {}, uniforms: {} }; };
  const resources = { programs: new Map() };
  const gl = { getUniformLocation: (_, name) => name };
  const first = c.sceneWebGLDetailProgram(gl, resources, "base");
  assert.equal(c.sceneWebGLDetailProgram(gl, resources, "base"), first);
  assert.equal(compilations, 1); assert.ok(resources.programs.has("base-detail"));
  assert.equal(first.uniforms.detail, "u_detail[0]");
  assert.equal(c.sceneDetailVariantKey("base", null), "base");
});

test("WebGPU detail preserves the nil shader and uses a separate array binding", () => {
  const c = detailContext("webgpu"); const base = vm.runInContext("WGSL_PBR_FRAGMENT", c);
  assert.equal(c.sceneWebGPUDetailFragment(base, null), base);
  // Main preserves signed normal-map scales in the base shader; nil detail stays neutral.
  assert.equal(crypto.createHash("sha256").update(base).digest("hex"), "cc6d9fe99146bc918d8bf0ccff896100ef1edfcf6c743ebf355a6fd9abea5533");
  const shader = c.sceneWebGPUDetailFragment(base, true);
  assert.match(shader, /textureSampleGrad\(detailAtlas/);
  assert.match(shader, /@group\(2\) @binding\(1\) var detailAtlas: texture_2d_array/);
  assert.ok(shader.indexOf("dpdx(in.worldPos)") < shader.indexOf("detailApply(in.worldPos"));
  assert.match(shader, /fade > 0\.0 && detail.data\[4\]\.w > 0\.5/);
  assert.equal(c.wgpuPipelineKey(c.sceneDetailVariantKey("pbr", true), "opaque", true, "rgba8unorm", "depth24plus", 1), "pbr-detail|opaque|1|rgba8unorm|depth24plus|1");
  assert.equal(c.sceneDetailVariantKey("pbr", null), "pbr");
  const layouts = [], modules = [];
  c.GPUShaderStage = { FRAGMENT: 2 };
  const device = { createBindGroupLayout: d => (layouts.push(d), d), createPipelineLayout: d => d,
    createShaderModule: d => (modules.push(d), d), createSampler: d => d };
  const r = c.sceneWebGPUCreateDetailResources(device, "frame", "material", base);
  assert.equal(layouts[0].entries[1].texture.viewDimension, "2d-array");
  assert.deepEqual(Array.from(r.pipelineLayout.bindGroupLayouts.slice(0, 2)), ["frame", "material"]);
  assert.equal(modules.length, 1); assert.equal(modules[0].code, shader);
});

test("WebGL uploads detail controls and the atlas without rebinding base maps", () => {
  const c = detailContext("webgl"), calls = [];
  const material = { detail: { ground: { scale: 3 }, fadeStart: 5, fadeEnd: 12, slopeStart: 20, slopeEnd: 50 } };
  const atlas = { texture: {}, masks: [1, 1, 1, 0, 0, 0] };
  const gl = { TEXTURE0: 100, TEXTURE_2D_ARRAY: 7, uniform4fv: (_, data) => calls.push(Array.from(data)),
    activeTexture: unit => calls.push(unit), bindTexture: (target, texture) => calls.push([target, texture]), uniform1i: (_, unit) => calls.push(unit) };
  c.sceneWebGLUploadDetail(gl, { materials: new Map([[material, atlas]]) }, { detail: "params", detailAtlas: "atlas" }, material, true);
  assert.equal(calls[0][0], 3); assert.equal(calls[0][8], 5); assert.equal(calls[0][9], 12);
  assert.ok(Math.abs(calls[0][10] - 20 * Math.PI / 180) < 1e-7);
  assert.equal(calls[1], 114); assert.deepEqual(calls[2], [7, atlas.texture]); assert.equal(calls[3], 14);
});

test("WebGPU updates a stable detail group when quality changes", () => {
  const c = detailContext("webgpu"), writes = [];
  const material = { detail: { ground: { scale: 4 }, fadeStart: 6, fadeEnd: 13 } };
  const entry = { buffer: {}, group: {}, atlas: { masks: [1, 1, 0, 0, 0, 0] } };
  const resources = { materials: new Map([[c.sceneWebGPUDetailMaterialKey(material.detail), entry]]) };
  const device = { queue: { writeBuffer: (buffer, offset, data) => writes.push({ buffer, offset, data: Array.from(data) }) } };
  assert.equal(c.sceneWebGPUUploadDetail(device, resources, material, true), entry.group);
  assert.equal(c.sceneWebGPUUploadDetail(device, resources, material, false), entry.group);
  assert.equal(writes[0].buffer, entry.buffer); assert.equal(writes[0].data[0], 4);
  assert.deepEqual(writes[0].data.slice(8, 10), [6, 13]);
  assert.equal(writes[0].data[19], 1); assert.equal(writes[1].data[19], 0);
});

test("Model.Detail reaches every imported primitive without replacing glTF maps", () => {
  const c = detailContext();
  const mount = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "mount-webgl.ts"), "utf8");
  const start = mount.indexOf("  function sceneModelMaterialOverrideSource(");
  const end = mount.indexOf("  function sceneApplyModelLOD(", start);
  assert.ok(start >= 0 && end > start);
  vm.runInContext(mount.slice(start, end), c);
  const detail = { ground: { albedo: "/detail.png" } };
  const model = c.normalizeSceneModel({ src: "/asset.glb", detail: detail }, 0);
  assert.equal(model.materialOverride.detail.ground.albedo, "/detail.png");
  for (const texture of ["/a.png", "/b.ktx2"]) {
    const raw = { material: { kind: "standard", texture: texture, normalMap: "/base-normal.png", roughnessMap: "/base-rough.png" } };
    const primitive = c.sceneApplyMaterialOverride(raw, model);
    assert.equal(primitive.material.texture, texture);
    assert.equal(primitive.material.normalMap, "/base-normal.png");
    assert.equal(primitive.material.roughnessMap, "/base-rough.png");
    assert.equal(primitive.detail.ground.albedo, detail.ground.albedo);
    assert.equal(primitive.material.detail, model.materialOverride.detail);
    assert.equal(raw.material.detail, undefined);
  }
});
