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
  // Reviewed optics/MRT base: thickness, geometric AA and reflected-radiance output.
  // The browser optics oracle covers behavior; nil detail must keep this base exact.
  // Reviewed vessel cutaway base: two u_cut clip planes discard before shading; all-zero planes cut nothing
  // (scripts/scene3d-vessel-cutaway.mjs checks the pixels).
  assert.equal(crypto.createHash("sha256").update(base).digest("hex"), "ba5119d2f1e7d21615e685e8cc757367ac048e774ad9893ed3d0f807efdf2411");
  const shader = c.sceneWebGLDetailFragment(base, true);
  const glWithUnits = units => ({ MAX_TEXTURE_IMAGE_UNITS: 1, getParameter: () => units });
  assert.match(c.scenePBRFragmentSourceForContext(glWithUnits(16), base), /#define GOSX_HDR_IBL 1/);
  const bounded = c.scenePBRFragmentSourceForContext(glWithUnits(16), shader);
  assert.match(bounded, /#define GOSX_HDR_IBL 0/);
  assert.match(bounded, /uniform highp sampler2DArray u_detailAtlas;/);
  assert.match(bounded, /DetailResult detailResult = detailApply/);
  assert.match(c.scenePBRFragmentSourceForContext(glWithUnits(17), shader), /#define GOSX_HDR_IBL 1/);
  c.sceneAllocateTextureUnits = () => ({ ibl: { brdfLUT: 13 }, shadows: [9, 10] });
  for (const [units, detail, minimum] of [[15, false, 16], [16, true, 17]]) {
    const uniforms = { detail: detail ? {} : null };
    const gl = { ...glWithUnits(units), uniform1i() {}, uniform1f() {} };
    c.scenePBRUploadEnvironmentMap(gl, uniforms, { ibl: {} }, {}, [], []);
    assert.equal(uniforms._gosxIBLDiagnostics.reason, `fragment-texture-units<${minimum}`);
  }
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
  // Reviewed optics/MRT base and explicit environment-map LOD for seam-free reflections.
  // The browser optics oracle covers behavior; nil detail must keep this base exact.
  // Reviewed vessel cutaway base: the fragment reads two planes from its model slot and discards before shading;
  // all-zero planes cut nothing.
  assert.equal(crypto.createHash("sha256").update(base).digest("hex"), "c36e68d8d98ee29f9ac4116d838e35e7bc9d35df4996a2e6c8d793faaebd6738");
  const shader = c.sceneWebGPUDetailFragment(base, true);
  assert.match(shader, /textureSampleGrad\(detailAtlas/);
  assert.match(shader, /@group\(2\) @binding\(1\) var detailAtlas: texture_2d_array/);
  const colorStart = shader.indexOf("fn gosxPBRColor(in: VertexOutput) -> vec4f {");
  const colorEnd = shader.indexOf("@fragment fn fragmentMain", colorStart);
  const color = shader.slice(colorStart, colorEnd);
  assert.ok(colorStart >= 0 && colorEnd > colorStart, "detail uses the shared PBR color function");
  assert.match(color, /let detailDx = dpdx\(in\.worldPos\);/);
  assert.match(color, /let detailDy = dpdy\(in\.worldPos\);/);
  assert.ok(color.indexOf("dpdx(in.worldPos)") < color.indexOf("if (material.hasAlbedoMap"), "derivatives precede nonuniform material branches");
  assert.ok(color.indexOf("dpdy(in.worldPos)") < color.indexOf("detailApply(in.worldPos"), "derivatives and their use share the color function scope");
  assert.match(shader, /fade > 0\.0 && detail.data\[4\]\.w > 0\.5/);
  assert.equal(c.wgpuPipelineKey(c.sceneDetailVariantKey("pbr", true), "opaque", true, "rgba8unorm", "depth24plus", 1), "pbr-detail|opaque|1|rgba8unorm|depth24plus|1");
  assert.equal(c.sceneDetailVariantKey("pbr", null), "pbr");
  const layouts = [], modules = [];
  c.GPUShaderStage = { FRAGMENT: 2 };
  const device = { createBindGroupLayout: d => (layouts.push(d), d), createPipelineLayout: d => d,
    createShaderModule: d => (modules.push(d), d), createSampler: d => d };
  assert.equal(c.sceneWebGPUEnsureDetailResources(device, null, [], "frame", "material", base), null);
  assert.equal(layouts.length, 0, "scenes without detail must not allocate detail resources");
  const r = c.sceneWebGPUEnsureDetailResources(device, null, [{ detail: {} }], "frame", "material", base);
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
  assert.equal(calls[1], 115); assert.deepEqual(calls[2], [7, atlas.texture]); assert.equal(calls[3], 15);
  const samplers = new Map();
  Object.assign(gl, { TEXTURE_2D: 8, uniform3fv() {}, uniform2f() {}, uniformMatrix4fv() {},
    uniform1i: (name, unit) => samplers.set(name, unit) });
  vm.runInContext(fs.readFileSync(path.join(__dirname, "bootstrap-src", "15a1-scene-texture-budget.ts"), "utf8"), c);
  c.sceneCreateTransmissionWebGL(gl).upload({ transmissionScene: "transmission" }, {}, [], [], {});
  c.sceneWebGLUploadDetail(gl, { materials: new Map([[material, atlas]]) }, { detail: "params", detailAtlas: "atlas" }, material, true);
  assert.equal(samplers.get("transmission"), 14);
  assert.equal(samplers.get("atlas"), 15);
  assert.notEqual(samplers.get("transmission"), samplers.get("atlas"), "2D transmission and array detail samplers must not alias even with inactive transmission");
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

function modelOverrideContext() {
  const c = detailContext();
  const mount = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "mount-webgl.ts"), "utf8");
  const start = mount.indexOf("  function sceneModelMaterialOverrideSource(");
  const end = mount.indexOf("  function sceneApplyModelLOD(", start);
  assert.ok(start >= 0 && end > start);
  vm.runInContext(mount.slice(start, end), c);
  return c;
}

test("Model.Detail reaches every imported primitive without replacing glTF maps", () => {
  const c = modelOverrideContext();
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

test("model overrides retain volume controls and replace embedded glTF volume", () => {
 const c=modelOverrideContext();
 for(const volume of [{thickness:2.4,attenuationDistance:3,attenuationColor:[0,0.5,1]}, {thickness:0,attenuationDistance:0,attenuationColor:[1,1,1]}]) {
  const model=c.normalizeSceneModel({src:"/glass.glb",material:{transmission:1,...volume}},0);
  assert.equal(model.materialOverride.thickness,volume.thickness);
  assert.equal(model.materialOverride.attenuationDistance,volume.attenuationDistance);
  assert.deepEqual(Array.from(model.materialOverride.attenuationColor),volume.attenuationColor);
  const primitive=c.sceneApplyMaterialOverride({material:{kind:"standard",thickness:5,attenuationDistance:8,attenuationColor:[0.3,0.3,0.3]}},model);
  assert.equal(primitive.material.thickness,volume.thickness);
  assert.deepEqual(Array.from(primitive.material.attenuationColor),volume.attenuationColor);
 }
});
