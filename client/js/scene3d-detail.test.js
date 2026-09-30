"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs"), path = require("node:path"), vm = require("node:vm");
const { createContext } = require("./runtime-test-harness.js");
const crypto = require("node:crypto");
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");

function detailContext(backend) {
  const env = createContext({});
  for (const file of ["10-runtime-primitives.ts", "10-runtime-scene-utils.ts", "11-scene-math.ts", "13-scene-material.ts", "16c1-scene-detail.ts"]) {
    vm.runInContext(fs.readFileSync(path.join(__dirname, "bootstrap-src", file), "utf8"), env.context);
  }
  const core = fs.readFileSync(path.join(__dirname, "bootstrap-src", "10-runtime-scene-core.ts"), "utf8");
  vm.runInContext(core.slice(0, core.indexOf("// Scene3D shared API")), env.context);
  if (backend) vm.runInContext(readSceneRendererBackendSrc(backend), env.context);
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
  assert.equal(crypto.createHash("sha256").update(base).digest("hex"), "e051b94053d92ddf59f26883934d34a0a77579f5572bdb5609443abdf7c269dc");
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

test("Model.Detail reaches every imported primitive without replacing glTF maps", () => {
  const c = detailContext();
  const mount = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "mount-webgl.ts"), "utf8");
  const start = mount.indexOf("  function sceneModelMaterialOverrideSource(");
  const end = mount.indexOf("  function sceneApplyModelLOD(", start);
  assert.ok(start >= 0 && end > start);
  vm.runInContext(mount.slice(start, end), c);
  const detail = { ground: { albedo: "/detail.png" } };
  for (const texture of ["/a.png", "/b.ktx2"]) {
    const raw = { material: { kind: "standard", texture: texture, normalMap: "/base-normal.png", roughnessMap: "/base-rough.png" } };
    const primitive = c.sceneApplyMaterialOverride(raw, { detail: detail });
    assert.equal(primitive.material.texture, texture);
    assert.equal(primitive.material.normalMap, "/base-normal.png");
    assert.equal(primitive.material.roughnessMap, "/base-rough.png");
    assert.equal(primitive.detail, detail); assert.equal(primitive.material.detail, detail);
    assert.equal(raw.material.detail, undefined);
  }
});
