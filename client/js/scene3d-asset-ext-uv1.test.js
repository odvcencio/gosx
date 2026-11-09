"use strict";
// The UV1 shader variants: asset-ext.ts rewrites the renderer's own PBR GLSL so
// occlusion and emissive maps can read TEXCOORD_1. glslang validates every
// rewritten program, and the bind hook is checked against a fake context.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const vm = require("node:vm");
const { spawnSync } = require("node:child_process");
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");

const ts = require("../runtime/node_modules/typescript");
const extSource = ts.transpileModule(
  fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "asset-ext.ts"), "utf8"),
  { compilerOptions: { target: ts.ScriptTarget.ES2022 } },
).outputText;

function loadExt() {
  const window = {};
  vm.runInNewContext(extSource, { window, WeakMap, Object, Map, GPUBufferUsage: { VERTEX: 32, COPY_DST: 8 } });
  return window.__gosx_scene3d_asset_ext;
}

function rendererConstants(names) {
  const source = readSceneRendererBackendSrc("webgl");
  const context = vm.createContext({});
  const loaded = new Set();
  // Load a constant after every constant its text names, so the fragment
  // source's helper snippets resolve without hard-coding the list.
  function load(name) {
    if (loaded.has(name)) return;
    loaded.add(name);
    const start = source.indexOf("const " + name + " = ");
    assert.ok(start >= 0, name);
    const valueStart = start + ("const " + name + " = ").length;
    const end = source[valueStart] === "["
      ? source.indexOf('].join("\\n");', valueStart) + '].join("\\n");'.length
      : source[valueStart] === "`"
        ? source.indexOf("`;", valueStart + 1) + 2
        : source.indexOf(";\n", valueStart) + 1;
    const text = source.slice(start, end);
    for (const ref of new Set(text.match(/\b(?:GLSL|SCENE)_[A-Z0-9_]+\b/g) || [])) {
      if (ref !== name && source.includes("const " + ref + " = ")) load(ref);
    }
    vm.runInContext(text, context);
  }
  for (const name of names) load(name);
  return (name) => vm.runInContext(name, context);
}

const names = ["SCENE_GLSL_AFFINE_NORMAL", "SCENE_GLSL_JOINT_PALETTE", "SCENE_PBR_VERTEX_SOURCE",
  "SCENE_PBR_SKINNED_VERTEX_SOURCE", "SCENE_PBR_FRAGMENT_SOURCE"];

test("the UV1 vertex rewrite carries a_uv1 into v_uv1 for the static and skinned programs", () => {
  const { uv1 } = loadExt();
  const constant = rendererConstants(names);
  for (const name of ["SCENE_PBR_VERTEX_SOURCE", "SCENE_PBR_SKINNED_VERTEX_SOURCE"]) {
    const out = uv1.vertex(constant(name));
    assert.match(out, /in vec2 a_uv1;/, name);
    assert.match(out, /out vec2 v_uv1;/, name);
    assert.match(out, /v_uv1 = a_uv1;/, name);
    assert.equal(out.split("v_uv1 = a_uv1;").length, 2, name + " assigns v_uv1 once");
  }
});

test("the UV1 fragment rewrite switches only the occlusion and emissive reads", () => {
  const { uv1 } = loadExt();
  const src = rendererConstants(names)("SCENE_PBR_FRAGMENT_SOURCE");
  const out = uv1.fragment(src);
  assert.match(out, /texture\(u_occlusionMap, u_occlusionUV1 \? v_uv1 : v_uv\)/);
  assert.match(out, /texture\(u_emissiveMap, u_emissiveUV1 \? v_uv1 : v_uv\)/);
  assert.doesNotMatch(out, /texture\(u_occlusionMap, v_uv\)/);
  assert.match(out, /texture\(u_albedoMap, v_uv\)/, "base color keeps the primary set");
  assert.match(out, /texture\(u_normalMap, v_uv \* u_normalUVScale\)/);
});

test("every rewritten program compiles under glslang and links its varyings", (t) => {
  if (spawnSync("glslangValidator", ["--version"]).status !== 0) { t.skip("glslangValidator unavailable"); return; }
  const { uv1 } = loadExt();
  const constant = rendererConstants(names);
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-uv1-"));
  try {
    const frag = path.join(dir, "uv1.frag");
    fs.writeFileSync(frag, uv1.fragment(constant("SCENE_PBR_FRAGMENT_SOURCE")));
    for (const name of ["SCENE_PBR_VERTEX_SOURCE", "SCENE_PBR_SKINNED_VERTEX_SOURCE"]) {
      const vert = path.join(dir, name + ".vert");
      fs.writeFileSync(vert, uv1.vertex(constant(name)));
      // Compiling and linking the pair proves v_uv1 is declared on both sides.
      const linked = spawnSync("glslangValidator", ["-l", vert, frag], { encoding: "utf8" });
      assert.equal(linked.status, 0, name + ": " + linked.stdout + linked.stderr);
    }
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

function fakeGL() {
  const calls = [];
  const gl = {
    ARRAY_BUFFER: 1, FLOAT: 2, DYNAMIC_DRAW: 3, calls,
    getAttribLocation: (_p, name) => (calls.push(["attrib", name]), 7),
    getUniformLocation: (_p, name) => ({ name }),
    uniform1i: (loc, v) => calls.push(["uniform1i", loc.name, v]),
    createBuffer: () => ({ id: "buf" }),
    bindBuffer: () => calls.push(["bindBuffer"]),
    bufferData: (_t, data) => calls.push(["bufferData", data.length]),
    enableVertexAttribArray: (l) => calls.push(["enable", l]),
    vertexAttribPointer: (l, size) => calls.push(["pointer", l, size]),
  };
  return gl;
}

test("wanted needs a second stream and a material that reads it", () => {
  const { uv1 } = loadExt();
  const stream = new Float32Array(6);
  assert.equal(uv1.wanted({ uvs1: stream }, { occlusionTexCoord: 1 }), true);
  assert.equal(uv1.wanted({ uvs1: stream }, { emissiveTexCoord: 1 }), true);
  assert.equal(uv1.wanted({ uvs1: stream }, {}), false);
  assert.equal(uv1.wanted({}, { occlusionTexCoord: 1 }), false);
  assert.equal(uv1.wanted({ directVertices: true, vertices: { uvs1: stream } }, { occlusionTexCoord: 1 }), true);
  assert.equal(uv1.wanted({ vertices: { uvs1: stream } }, { occlusionTexCoord: 1 }), false, "baked meshes use their own stream");
  assert.equal(uv1.wanted({ uvs1: stream }, { occlusionTexCoord: 1, detail: {} }), false, "detail materials keep their variant");
});

const BASE = { program: { id: "uv1-base" } };
const SKINNED = { program: { id: "uv1-skinned" } };
const creators = (built) => ({
  base: (gl, detail, variant) => (built.push(["base", detail, typeof variant.vertex]), BASE),
  skinned: (gl, detail, variant) => (built.push(["skinned", detail, typeof variant.fragment]), SKINNED),
  usable: (info) => info,
});

test("use builds each variant once and hands it to the draw", () => {
  const { uv1 } = loadExt();
  const gl = fakeGL();
  const built = [];
  const obj = { uvs1: new Float32Array(6) };
  const mat = { occlusionTexCoord: 1 };
  assert.equal(uv1.use(gl, obj, mat, false, creators(built)), BASE);
  assert.equal(uv1.use(gl, obj, mat, false, creators(built)), BASE);
  assert.equal(uv1.use(gl, obj, mat, true, creators(built)), SKINNED);
  assert.deepEqual(built, [["base", false, "function"], ["skinned", false, "function"]]);
});

test("use leaves authored shaders and unrelated draws on the default path", () => {
  const { uv1 } = loadExt();
  const gl = fakeGL();
  const built = [];
  const obj = { uvs1: new Float32Array(6) };
  assert.equal(uv1.use(gl, obj, { occlusionTexCoord: 1, customFragment: "x" }, false, creators(built)), null);
  assert.equal(uv1.use(gl, obj, { occlusionTexCoord: 1, shaderBackend: "selena" }, false, creators(built)), null);
  assert.equal(uv1.use(gl, {}, { occlusionTexCoord: 1 }, false, creators(built)), null);
  assert.equal(uv1.use(gl, obj, {}, false, creators(built)), null);
  assert.deepEqual(built, []);
});

test("bind sets the per-map flags and uploads the stream for variant programs only", () => {
  const { uv1 } = loadExt();
  const gl = fakeGL();
  const obj = { uvs1: new Float32Array(6) };
  const mat = { occlusionTexCoord: 1 };
  uv1.use(gl, obj, mat, false, creators([]));
  gl.calls.length = 0;
  uv1.bind(gl, BASE.program, obj, mat);
  assert.deepEqual(gl.calls.filter((c) => c[0] === "uniform1i"), [["uniform1i", "u_occlusionUV1", 1], ["uniform1i", "u_emissiveUV1", 0]]);
  assert.deepEqual(gl.calls.filter((c) => c[0] === "bufferData"), [["bufferData", 6]]);
  assert.deepEqual(gl.calls.filter((c) => c[0] === "pointer"), [["pointer", 7, 2]]);
  gl.calls.length = 0;
  uv1.bind(gl, { id: "default" }, obj, mat);
  assert.deepEqual(gl.calls.filter((c) => c[0] !== "attrib"), [], "a default program gets nothing");
});

test("a variant that fails to build stops being wanted", () => {
  const { uv1 } = loadExt();
  const gl = fakeGL();
  const obj = { uvs1: new Float32Array(2) };
  const mat = { occlusionTexCoord: 1 };
  assert.equal(uv1.use(gl, obj, mat, false, { base: () => null, skinned: () => null, usable: (x) => x }), null);
  assert.equal(uv1.wanted(obj, mat), false);
});

// --- WebGPU ---------------------------------------------------------------

function wgslConstants(names) {
  const source = readSceneRendererBackendSrc("webgpu");
  const context = vm.createContext({});
  const loaded = new Set();
  function load(name) {
    if (loaded.has(name)) return;
    loaded.add(name);
    const m = new RegExp("\\b(?:var|const) " + name + " = ").exec(source);
    assert.ok(m, name);
    const valueStart = m.index + m[0].length;
    let end;
    if (source[valueStart] === "[") end = source.indexOf('].join("\\n");', valueStart) + '].join("\\n");'.length;
    else if (source[valueStart] === "`") end = source.indexOf("`;", valueStart + 1) + 2;
    else end = source.indexOf(";\n", valueStart) + 1;
    const text = source.slice(m.index, end);
    for (const ref of new Set(text.match(/\b(?:WGSL|SCENE)_[A-Z0-9_]+\b/g) || [])) if (ref !== name && new RegExp("\\b(?:var|const) " + ref + " = ").test(source)) load(ref);
    vm.runInContext(text, context);
  }
  for (const name of names) load(name);
  return (name) => vm.runInContext(name, context);
}

test("the WGSL variants add the second UV set to the vertex input and the stage interface", () => {
  const { uv1 } = loadExt();
  const wgsl = wgslConstants(["WGSL_PBR_VERTEX", "WGSL_PBR_FRAGMENT"]);
  const vertex = uv1.wgslVertex(wgsl("WGSL_PBR_VERTEX"));
  assert.equal(vertex.split("@location(9) uv1: vec2f").length, 2);
  assert.equal(vertex.split("@location(6) uv1: vec2f").length, 2);
  assert.match(vertex, /out\.uv = in\.uv;\n    out\.uv1 = in\.uv1;/);
  const fragment = uv1.wgslFragment(wgsl("WGSL_PBR_FRAGMENT"), "o");
  assert.equal(fragment.split("@location(6) uv1: vec2f").length, 2);
  assert.match(fragment, /textureSample\(occlusionTex, occlusionSamp, in\.uv1\)/);
  assert.match(fragment, /textureSample\(emissiveTex, emissiveSamp, in\.uv\)/, "emissive stays on the first set");
  assert.match(fragment, /textureSample\(albedoTex, albedoSamp, in\.uv\)/, "base color stays on the first set");
  const both = uv1.wgslFragment(wgsl("WGSL_PBR_FRAGMENT"), "oe");
  assert.doesNotMatch(both, /emissiveSamp, in\.uv\)/);
  assert.doesNotMatch(both, /occlusionSamp, in\.uv\)/);
});

test("gpuWanted names the maps and refuses paths that cannot bind the fifth slot", () => {
  const { uv1 } = loadExt();
  const stream = new Float32Array(6);
  assert.equal(uv1.gpuWanted({ uvs1: stream }, { occlusionTexCoord: 1 }), "o");
  assert.equal(uv1.gpuWanted({ uvs1: stream }, { emissiveTexCoord: 1 }), "e");
  assert.equal(uv1.gpuWanted({ uvs1: stream }, { occlusionTexCoord: 1, emissiveTexCoord: 1 }), "oe");
  assert.equal(uv1.gpuWanted({ uvs1: stream }, {}), "");
  for (const extra of [{ directVertices: true }, { retainedGeometry: true }, { computedMorph: {} }, { skin: {} }]) {
    assert.equal(uv1.gpuWanted({ uvs1: stream, ...extra }, { occlusionTexCoord: 1 }), "", JSON.stringify(Object.keys(extra)));
  }
  assert.equal(uv1.gpuWanted({ uvs1: stream }, { occlusionTexCoord: 1, customFragmentWGSL: "x" }), "");
});

test("gpuModules builds each variant once and gives the vertex module a five slot layout", () => {
  const { uv1 } = loadExt();
  const created = [];
  const device = { createShaderModule: (d) => (created.push(d.label), { label: d.label }) };
  const a = uv1.gpuModules(device, "vertex", "fragment", "o");
  assert.equal(uv1.gpuModules(device, "vertex", "fragment", "o"), a);
  uv1.gpuModules(device, "vertex", "fragment", "e");
  assert.deepEqual(created, ["pbr-vert-uv1o", "pbr-frag-uv1o", "pbr-vert-uv1e", "pbr-frag-uv1e"]);
  assert.equal(a.vertex.gosxVertexLayout.length, 5);
  assert.equal(JSON.stringify(a.vertex.gosxVertexLayout[4].attributes[0]), JSON.stringify({ format: "float32x2", offset: 0, shaderLocation: 9 }));
});

test("gpuDraw binds four sub-ranges, uploads UV1 and draws once", () => {
  const { uv1 } = loadExt();
  const calls = [];
  const device = {
    createBuffer: (d) => (calls.push(["createBuffer", d.size]), { destroy() {} }),
    queue: { writeBuffer: (_b, off, data) => calls.push(["write", off, data.length]) },
  };
  const pass = { setVertexBuffer: (slot, _b, off, size) => calls.push(["slot", slot, off, size]), draw: (n) => calls.push(["draw", n]) };
  const bindRange = (_p, slot, rec, offset, count) => (calls.push(["range", slot, offset, count]), !!rec);
  const buffers = { positions: {}, normals: {}, uvs: {}, tangents: {} };
  const ok = uv1.gpuDraw(device, pass, { id: "m", uvs1: new Float32Array(12) }, 30, 6, bindRange, buffers);
  assert.equal(ok, true);
  assert.deepEqual(calls, [
    ["range", 0, 30, 6], ["range", 1, 30, 6], ["range", 2, 30, 6], ["range", 3, 30, 6],
    ["createBuffer", 48], ["write", 0, 12], ["slot", 4, 0, 48], ["draw", 6],
  ]);
});
