"use strict";
// WebGPU cascaded shadow maps: split, fit, stability and uniform layout of the
// pure cascade math. Renderer integration is covered further below through the
// board WebGPU harness.
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const ts = require("../runtime/node_modules/typescript");
const { createBoardWebGPUHarness, shadowUniformState } = require("./runtime-test-harness.js");

function extract(file, name) {
  const source = fs.readFileSync(path.join(__dirname, file), "utf8");
  const start = source.indexOf("function " + name + "(");
  assert.ok(start >= 0, name + " missing from " + file);
  const end = source.indexOf("\n  }\n", start) + 4;
  return source.slice(start, end);
}

function loadCSM() {
  const source = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu-shadows.ts"), "utf8");
  const context = { GPUTextureUsage: { RENDER_ATTACHMENT: 0x10, TEXTURE_BINDING: 0x4 }, Float32Array, Int32Array, Math };
  vm.createContext(context);
  vm.runInContext([
    "function sceneNumber(v, f) { const n = Number(v); return Number.isFinite(n) ? n : f; }",
    extract("bootstrap-src/11-scene-math.ts", "sceneMat4MultiplyInto"),
    extract("../runtime/scene3d/webgpu.ts", "sceneWebGPUShadowDepthMatrix"),
    ts.transpileModule(extract("bootstrap-src/16c-scene-shared-pbr.ts", "sceneShadowCascadeBiasScale"), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText,
    ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText,
    "globalThis.csm = { sceneWebGPUShadowCascadeSplits, sceneWebGPUShadowSphere, wgpuFitShadowSlot, wgpuPackShadowUniforms,",
    "  sceneWebGPUShadowCascadeCount, wgpuEnsureShadowSlot, WGSL_SHADOW_UNIFORMS_STRUCT, WGSL_SHADOW_CASCADE_FNS, SCENE_WEBGPU_SHADOW_UNIFORM_BYTES };",
  ].join("\n"), context);
  return context.csm;
}

const csm = loadCSM();

function lookAt(eye, target) {
  let fx = target[0] - eye[0], fy = target[1] - eye[1], fz = target[2] - eye[2];
  const fl = Math.hypot(fx, fy, fz); fx /= fl; fy /= fl; fz /= fl;
  let rx = fy * 0 - fz * 1, ry = fz * 0 - fx * 0, rz = fx * 1 - fy * 0;
  const rl = Math.hypot(rx, ry, rz); rx /= rl; ry /= rl; rz /= rl;
  const ux = ry * fz - rz * fy, uy = rz * fx - rx * fz, uz = rx * fy - ry * fx;
  return new Float32Array([
    rx, ux, -fx, 0, ry, uy, -fy, 0, rz, uz, -fz, 0,
    -(rx * eye[0] + ry * eye[1] + rz * eye[2]), -(ux * eye[0] + uy * eye[1] + uz * eye[2]), fx * eye[0] + fy * eye[1] + fz * eye[2], 1,
  ]);
}

function fakeSlot(size, layers) {
  const device = { createTexture(desc) { return { desc, createView(v) { return { view: v || {} }; } }; } };
  return csm.wgpuEnsureShadowSlot(device, null, size, layers, () => {});
}

const light = { kind: "directional", directionX: -0.4, directionY: -1, directionZ: -0.3, shadowCascades: 4, shadowBias: 0.004 };
const bounds = { minX: -60, maxX: 60, minY: -2, maxY: 30, minZ: -60, maxZ: 60 };
const camera = { fov: 60, near: 0.1, far: 120 };
const raw = new Float32Array([0.02, 0, 0, 0, 0, 0.02, 0, 0, 0, 0, -0.01, 0, 0, 0, -1, 1]);

function fit(eye, target, slot = fakeSlot(2048, 4)) {
  return csm.wgpuFitShadowSlot(slot, light, raw, { view: lookAt(eye, target), camera, aspect: 16 / 9, bounds });
}

function project(m, p) {
  const x = m[0] * p[0] + m[4] * p[1] + m[8] * p[2] + m[12];
  const y = m[1] * p[0] + m[5] * p[1] + m[9] * p[2] + m[13];
  const z = m[2] * p[0] + m[6] * p[1] + m[10] * p[2] + m[14];
  const w = m[3] * p[0] + m[7] * p[1] + m[11] * p[2] + m[15];
  return [x / w, y / w, z / w];
}

test("cascade splits rise to the far distance and honor lambda", () => {
  const log = csm.sceneWebGPUShadowCascadeSplits(0.1, 100, 4, 1);
  const uni = csm.sceneWebGPUShadowCascadeSplits(0.1, 100, 4, 0);
  assert.ok(Math.abs(log[3] - 100) < 1e-6 && Math.abs(uni[3] - 100) < 1e-6);
  assert.ok(Math.abs(uni[0] - 25.075) < 1e-3, "lambda 0 is uniform");
  assert.ok(log[0] < uni[0], "lambda 1 packs detail near the camera");
  for (let i = 1; i < 4; i++) assert.ok(log[i] > log[i - 1]);
});

test("each cascade covers its view slice; together they reach the shadow distance with no gap", () => {
  const eye = [5, 8, 20], target = [0, 2, 0];
  const slot = fit(eye, target);
  const view = lookAt(eye, target);
  // Camera basis back from the view matrix.
  const fwd = [-view[2], -view[6], -view[10]], right = [view[0], view[4], view[8]], up = [view[1], view[5], view[9]];
  const tanY = Math.tan(Math.PI / 6), tanX = tanY * 16 / 9;
  let near = 0.1;
  for (let c = 0; c < 4; c++) {
    const far = slot.cascades[c].splitFar;
    for (const d of [near, far]) for (const sx of [-1, 1]) for (const sy of [-1, 1]) {
      const p = [0, 1, 2].map((k) => eye[k] + fwd[k] * d + right[k] * sx * d * tanX + up[k] * sy * d * tanY);
      const q = project(slot.cascades[c].lightMatrix, p);
      assert.ok(Math.abs(q[0]) <= 1 && Math.abs(q[1]) <= 1, `cascade ${c} corner inside the box: ${q}`);
      assert.ok(q[2] >= 0 && q[2] <= 1, `cascade ${c} corner inside WebGPU depth: ${q[2]}`);
    }
    near = far;
  }
  assert.ok(Math.abs(slot.cascades[3].splitFar - 120) < 1e-6, "the last cascade ends at the camera far plane");
});

test("ShadowDistance caps the cascaded range", () => {
  const slot = csm.wgpuFitShadowSlot(fakeSlot(1024, 4), { ...light, shadowDistance: 40 }, raw, { view: lookAt([0, 5, 10], [0, 0, 0]), camera, aspect: 1, bounds });
  assert.ok(Math.abs(slot.cascades[3].splitFar - 40) < 1e-6);
});

test("rotating the camera keeps every cascade's texel size", () => {
  const a = fit([0, 6, 20], [0, 2, 0]);
  const b = fit([0, 6, 20], [14, 0, 3]);
  for (let c = 0; c < 4; c++) {
    // An orthographic light matrix scales world units by 1 / half-width.
    const scaleA = Math.hypot(a.cascades[c].lightMatrix[0], a.cascades[c].lightMatrix[4], a.cascades[c].lightMatrix[8]);
    const scaleB = Math.hypot(b.cascades[c].lightMatrix[0], b.cascades[c].lightMatrix[4], b.cascades[c].lightMatrix[8]);
    assert.ok(Math.abs(scaleA - scaleB) < 1e-7, `cascade ${c}: ${scaleA} vs ${scaleB}`);
  }
});

test("a slow pan moves a fixed shadow edge by whole texels only", () => {
  const size = 2048;
  const edge = [3.3, 0.1, -2.7];
  let previous = null, total = 0;
  for (let step = 0; step < 40; step++) {
    const t = step * 0.0137;
    const slot = fit([t, 6, 20 - t * 0.5], [t, 2, 0], fakeSlot(size, 4));
    const q = project(slot.cascades[1].lightMatrix, edge);
    const texel = [(q[0] * 0.5 + 0.5) * size, (0.5 - q[1] * 0.5) * size];
    if (previous) {
      for (let k = 0; k < 2; k++) {
        const moved = texel[k] - previous[k];
        assert.ok(Math.abs(moved - Math.round(moved)) < 2e-3, `step ${step}: edge moved ${moved} texels`);
        total += Math.abs(moved);
      }
    }
    previous = texel;
  }
  assert.ok(total >= 10, `the pan must cross texel boundaries to test anything (moved ${total} texels)`);
});

test("lights that cannot cascade keep the one-map full-scene fit", () => {
  assert.equal(csm.sceneWebGPUShadowCascadeCount({ kind: "spot", shadowCascades: 4 }, camera), 1);
  assert.equal(csm.sceneWebGPUShadowCascadeCount({ kind: "directional", shadowCascades: 0 }, camera), 1);
  assert.equal(csm.sceneWebGPUShadowCascadeCount({ kind: "directional", shadowCascades: 9 }, camera), 4);
  assert.equal(csm.sceneWebGPUShadowCascadeCount({ kind: "directional", shadowCascades: 3 }, { kind: "orthographic" }), 1);
  const slot = csm.wgpuFitShadowSlot(fakeSlot(512, 1), light, raw, { view: lookAt([0, 0, 5], [0, 0, 0]), camera, aspect: 1, bounds });
  assert.deepEqual(Array.from(slot.cascades[0].lightMatrix.slice(8, 12)), [0, 0, (raw[10] + raw[11]) * 0.5, 0]);
});

test("each cascade scales the authored bias to its texels", () => {
  // raw is the one-map fit: 100 m wide, 200 m deep. Near cascades are finer.
  const scales = Array.from(fit([0, 6, 20], [0, 2, 0]).cascades.map((c) => c.biasScale));
  assert.ok(scales[0] < scales[1] && scales[1] < scales[2] && scales[2] < scales[3], `scales rise with cascade size: ${scales}`);
  assert.ok(scales[0] > 0 && scales[0] < 1, `the nearest cascade is finer than the one-map fit: ${scales}`);
  const f = new Float32Array(csm.SCENE_WEBGPU_SHADOW_UNIFORM_BYTES / 4), i32 = new Int32Array(f.buffer);
  csm.wgpuPackShadowUniforms(f, i32, [fit([0, 6, 20], [0, 2, 0]), null], [0, -1], [light]);
  assert.deepEqual(Array.from(f.slice(72, 76)).map((v) => Math.round(v * 1e4)), scales.map((v) => Math.round(v * 1e4)), "the scales reach the shader");
});

test("shadow uniforms pack both slots at the documented offsets", () => {
  const buffer = new ArrayBuffer(csm.SCENE_WEBGPU_SHADOW_UNIFORM_BYTES);
  const f = new Float32Array(buffer), i32 = new Int32Array(buffer);
  const slot = fit([0, 6, 20], [0, 2, 0]);
  csm.wgpuPackShadowUniforms(f, i32, [slot, null], [2, -1], [{}, {}, { ...light, shadowSoftness: 0.25 }]);
  assert.equal(buffer.byteLength, 656);
  assert.deepEqual(Array.from(f.slice(0, 16)), Array.from(slot.cascades[0].lightMatrix));
  assert.deepEqual(Array.from(f.slice(48, 64)), Array.from(slot.cascades[3].lightMatrix));
  assert.ok(Math.abs(f[64] - slot.cascades[0].splitFar) < 1e-4);
  assert.deepEqual([f[68], Math.round(f[69] * 1000) / 1000, f[70], f[77]], [4, 0.004, 0.25, 2048]);
  assert.equal(f[80 + 68], 0, "an empty slot reports zero cascades");
  assert.deepEqual([i32[160], i32[161]], [2, -1]);
});

test("shadow uniforms report only the span that changed since the last upload", () => {
  const f = new Float32Array(csm.SCENE_WEBGPU_SHADOW_UNIFORM_BYTES / 4), i32 = new Int32Array(f.buffer);
  // The GPU buffer starts zeroed, so an unshadowed first frame uploads only
  // the two -1 light indices.
  assert.deepEqual(Array.from(csm.wgpuPackShadowUniforms(f, i32, [null, null], [-1, -1], [])), [160, 162]);
  assert.equal(csm.wgpuPackShadowUniforms(f, i32, [null, null], [-1, -1], []), null, "an unchanged frame uploads nothing");
  const lights = [{}, {}, { ...light, shadowSoftness: 0.25 }];
  const slot = fit([0, 6, 20], [0, 2, 0]);
  const first = Array.from(csm.wgpuPackShadowUniforms(f, i32, [slot, null], [2, -1], lights));
  assert.deepEqual([first[0], first[1]], [0, 161], "a new shadow uploads slot 0 and its light index");
  slot.cascades[1].lightMatrix[3] += 1;
  assert.deepEqual(Array.from(csm.wgpuPackShadowUniforms(f, i32, [slot, null], [2, -1], lights)), [19, 20], "one moved matrix element uploads one float");
  assert.equal(f[80 + 69], 0, "an empty slot keeps zero parameters");
});

test("the cascade WGSL declares the documented struct and samples array layers", () => {
  assert.match(csm.WGSL_SHADOW_UNIFORMS_STRUCT, /matrices: array<mat4x4f, 4>,\s*splits: vec4f,\s*params: vec4f,\s*scales: vec4f,\s*texel: vec4f/);
  assert.match(csm.WGSL_SHADOW_CASCADE_FNS, /fn shadowSlotFactor\(map: texture_depth_2d_array, samp: sampler_comparison, slot: ShadowCascades, worldPos: vec3f\) -> f32/);
  assert.match(csm.WGSL_SHADOW_CASCADE_FNS, /textureLoad\(map, vec2i\([^;]*\), layer, 0\)/);
  assert.doesNotMatch(csm.WGSL_SHADOW_CASCADE_FNS, /select\([^;]*shadowCascadeTaps/, "the next cascade is sampled only inside its blend band");
});

function cascadeBundle(api, lights, extra = {}) {
  const state = api.createSceneState({
    scene: {
      materials: [{ id: "m", color: "#ffffff", roughness: 1 }],
      objects: [
        { id: "caster", kind: "box", width: 1, height: 1, depth: 1, material: "m", castShadow: true, receiveShadow: true },
        { id: "ground", kind: "box", width: 40, height: 0.1, depth: 40, y: -1, material: "m", receiveShadow: true },
      ],
      lights,
    },
  }, { tier: "full" });
  return Object.assign(api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 3, z: 8, fov: 60, near: 0.1, far: 100 },
    api.sceneStateObjectsWithMaterials(state), [], [], [], api.sceneStateLights(state), {},
    0, [], [], [], [], [], 0, false,
  ), extra);
}

async function cascadeHarness() {
  return createBoardWebGPUHarness({ fresh: true, configureDevice(device) {
    const create = device.createTexture.bind(device);
    device.createTexture = (desc) => {
      const texture = create(desc), view = texture.createView;
      texture.createView = (viewDesc) => Object.assign(view.call(texture, viewDesc), { desc: viewDesc || null, texture });
      return texture;
    };
  } });
}

function cascadePasses(h, start) {
  return h.fake.state.renderPasses.slice(start).filter((pass) => {
    const view = pass.descriptor && pass.descriptor.depthStencilAttachment && pass.descriptor.depthStencilAttachment.view;
    return pass.descriptor.colorAttachments.length === 0 && view && view.texture && view.texture.desc.label === "gosx-shadow-cascades";
  });
}

const sun = { id: "sun", kind: "directional", castShadow: true, directionX: 0.2, directionY: -1, directionZ: -0.35, shadowSize: 512, shadowCascades: 4, shadowSoftness: 0.2 };

test("a four-cascade sun renders one array layer per cascade and samples the array in the PBR shader", async () => {
  const h = await cascadeHarness();
  try {
    const api = h.env.context.__gosx_scene3d_api;
    const start = h.fake.state.renderPasses.length;
    h.renderer.render(cascadeBundle(api, [sun]), { width: 64, height: 64 }, { nowMS: 0, active: true });
    const passes = cascadePasses(h, start);
    assert.deepEqual(passes.map((pass) => pass.descriptor.depthStencilAttachment.view.desc.baseArrayLayer), [0, 1, 2, 3]);
    // Each cascade writes its light matrix into its own arena slot, so no
    // cascade overwrites another's matrix before the frame is submitted.
    const offsets = passes.map((pass) => pass.bindGroups[0].dynamicOffsets[0]);
    assert.equal(new Set(offsets).size, 4, `distinct arena offsets: ${offsets}`);
    const texture = h.fake.state.textures.find((t) => t.desc && t.desc.label === "gosx-shadow-cascades");
    assert.deepEqual(Array.from(texture.desc.size), [512, 512, 4]);
    const { f, i32 } = shadowUniformState(h.fake);
    assert.deepEqual([f[68], i32[160], i32[161]], [4, 0, -1]);
    assert.ok(f[64] < f[65] && f[65] < f[66] && f[66] < f[67] && Math.abs(f[67] - 100) < 1e-3, "splits rise to the camera far plane");
    const pbr = h.fake.state.shaderModules.find((m) => m.label === "pbr-frag");
    assert.match(pbr.code, /var shadowMap0: texture_depth_2d_array;/);
    assert.match(pbr.code, /shadowSlotFactor\(shadowMap0, shadowSampler0, shadow\.slot0, in\.worldPos\)/);
  } finally { h.renderer.dispose(); }
});

test("a frame without a shadow slot draws with the PBR fragment that omits the shadow calls", async () => {
  const h = await cascadeHarness();
  try {
    const api = h.env.context.__gosx_scene3d_api;
    const pbrModules = (start) => h.fake.state.renderPasses.slice(start).flatMap((pass) => pass.draws)
      .filter((draw) => draw.pipeline && /^gosx-pbr-opaque$/.test(draw.pipeline.desc.label))
      .map((draw) => draw.pipeline.desc.fragment.module);
    // Solid boxes: cascadeBundle's boxes default to wireframe, which draws lines.
    const solid = (lights) => {
      const state = api.createSceneState({ scene: { materials: [{ id: "m", color: "#ffffff", roughness: 1 }], lights, objects: [
        { id: "caster", kind: "box", width: 1, height: 1, depth: 1, material: "m", wireframe: false, castShadow: true, receiveShadow: true },
        { id: "ground", kind: "box", width: 40, height: 0.1, depth: 40, y: -1, material: "m", wireframe: false, receiveShadow: true },
      ] } }, { tier: "full" });
      return api.createSceneRenderBundle(64, 64, "#000000", { x: 0, y: 3, z: 8, fov: 60, near: 0.1, far: 100 },
        api.sceneStateObjectsWithMaterials(state), [], [], [], api.sceneStateLights(state), {}, 0, [], [], [], [], [], 0, false);
    };
    const unlit = { id: "key", kind: "directional", directionX: 0, directionY: -1, directionZ: 0 };
    let start = h.fake.state.renderPasses.length;
    h.renderer.render(solid([unlit]), { width: 64, height: 64 }, { nowMS: 0, active: true });
    let modules = pbrModules(start);
    assert.ok(modules.length > 0, "the scene draws PBR objects");
    assert.deepEqual([...new Set(modules.map((m) => m.label))], ["pbr-frag-unshadowed"]);
    assert.doesNotMatch(modules[0].code, /= shadowSlotFactor\(/);
    assert.match(modules[0].code, /var shadowSlot0 = 1\.0;/, "the light loop still reads the default factor");
    start = h.fake.state.renderPasses.length;
    h.renderer.render(solid([sun]), { width: 64, height: 64 }, { nowMS: 17, active: true });
    modules = pbrModules(start);
    assert.deepEqual([...new Set(modules.map((m) => m.label))], ["pbr-frag"], "a shadow slot switches to the full fragment");
    // A point light that asks for shadows takes no slot, so it keeps the cheap module.
    start = h.fake.state.renderPasses.length;
    h.renderer.render(solid([{ id: "p", kind: "point", castShadow: true, y: 3 }]), { width: 64, height: 64 }, { nowMS: 34, active: true });
    assert.deepEqual([...new Set(pbrModules(start).map((m) => m.label))], ["pbr-frag-unshadowed"]);
  } finally { h.renderer.dispose(); }
});

test("the shadow memory cap covers every cascade layer, and a spot light keeps one layer", async () => {
  const h = await cascadeHarness();
  try {
    const api = h.env.context.__gosx_scene3d_api;
    h.renderer.render(cascadeBundle(api, [sun], { shadowMaxPixels: 512 * 512 }), { width: 64, height: 64 }, { nowMS: 0, active: true });
    let texture = h.fake.state.textures.filter((t) => t.desc && t.desc.label === "gosx-shadow-cascades").at(-1);
    assert.ok(texture.desc.size[0] * texture.desc.size[1] * texture.desc.size[2] <= 512 * 512, `cascades fit the cap: ${texture.desc.size}`);
    const spot = { id: "spot", kind: "spot", castShadow: true, x: 0, y: 4, z: 0, directionX: 0, directionY: -1, directionZ: 0, angle: 0.6, range: 10, shadowSize: 256 };
    const start = h.fake.state.renderPasses.length;
    h.renderer.render(cascadeBundle(api, [spot]), { width: 64, height: 64 }, { nowMS: 17, active: true });
    assert.equal(cascadePasses(h, start).length, 1);
    texture = h.fake.state.textures.filter((t) => t.desc && t.desc.label === "gosx-shadow-cascades").at(-1);
    assert.deepEqual(Array.from(texture.desc.size), [256, 256, 1]);
  } finally { h.renderer.dispose(); }
});

test("the one-map directional fit keeps the whole scene inside the light's clip depth", () => {
  // Regression: the light view used a +forward row, which put every caster
  // at clip z near -2, outside the map, so one-map directional shadows drew
  // nothing on either backend.
  const source = fs.readFileSync(path.join(__dirname, "bootstrap-src", "16c-scene-shared-pbr.ts"), "utf8");
  const context = { Float32Array, Math };
  vm.createContext(context);
  vm.runInContext([
    "function sceneNumber(v, f) { const n = Number(v); return Number.isFinite(n) ? n : f; }",
    "function sceneSpotShadowLightSpaceMatrix() { return null; }",
    extract("bootstrap-src/11-scene-math.ts", "sceneMat4MultiplyInto"),
    "function sceneMat4Multiply(a, b) { return sceneMat4MultiplyInto(new Float32Array(16), a, b); }",
    ts.transpileModule(extract("bootstrap-src/16c-scene-shared-pbr.ts", "sceneShadowLightSpaceMatrix"), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText,
    "globalThis.fit = sceneShadowLightSpaceMatrix;",
  ].join("\n"), context);
  const bounds = { minX: -10, maxX: 10, minY: -0.1, maxY: 3.1, minZ: -10, maxZ: 10 };
  for (const light of [{ directionX: 1, directionY: -1 }, { directionY: -1 }, { directionX: -0.3, directionY: -0.8, directionZ: 0.5 }]) {
    const m = context.fit(light, bounds);
    for (const x of [-10, 10]) for (const y of [-0.1, 3.1]) for (const z of [-10, 10]) {
      const q = project(m, [x, y, z]);
      assert.ok(Math.abs(q[0]) <= 1 && Math.abs(q[1]) <= 1 && q[2] > -1 && q[2] < 1, `corner ${[x, y, z]} -> ${q}`);
    }
    // Depth grows away from the light, so a caster above the ground is nearer.
    assert.ok(project(m, [0, 3, 0])[2] < project(m, [0, 0, 0])[2], "the caster is nearer the light than its receiver");
  }
});
