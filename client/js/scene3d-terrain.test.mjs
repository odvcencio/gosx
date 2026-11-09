// Environment.Terrain runtime: decoding, Go height parity, CDLOD selection
// coverage and crack-freedom, uniform packing, and the PBR fragment patches.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";
import { createRequire } from "node:module";

const ts = createRequire(new URL("../runtime/package.json", import.meta.url))("typescript");
const { readSceneRendererBackendSrc } = createRequire(import.meta.url)("./scene3d-renderer-source-set.js");
const read = (name) => fs.readFileSync(new URL("../runtime/scene3d/" + name, import.meta.url), "utf8");
const transpile = (source) => ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
const code = ["terrain.ts", "terrain-wgsl.ts", "terrain-glsl.ts"].map((name) => transpile(read(name))).join("\n");
const fixture = JSON.parse(fs.readFileSync(new URL("./testdata/scene3d-terrain-fixture.json", import.meta.url), "utf8"));

function runtime() {
  const context = vm.createContext({ window: {}, atob, Math, Float32Array, Float64Array, Uint8Array, Uint16Array, Number, Array, Object, String, JSON, Error, performance });
  vm.runInContext(code + `
    globalThis.t = { sceneTerrainBuild, sceneTerrainSamples, sceneTerrainBytes, sceneTerrainHeight, sceneTerrainSelect,
      sceneTerrainRanges, sceneTerrainUniforms, sceneTerrainIndices, sceneTerrainPlanes, sceneTerrainFragmentWGSL,
      sceneTerrainFragmentGLSL, sceneTerrainVertexWGSL, SCENE_TERRAIN_VERTEX_GLSL, sceneTerrainModelFor,
      sceneWindUpdate, sceneWindFor, sceneWindGust };`, context);
  return context;
}

function fixtureModel(t) {
  const ir = fixture.terrain;
  const samples = t.sceneTerrainSamples(t.sceneTerrainBytes(ir.heights), ir.columns * ir.rows);
  return t.sceneTerrainBuild(ir, samples);
}

// A square terrain built in the test: heights from a smooth function, the IR
// fields the Go lowering would produce for a 256 m, 257-sample map.
function squareModel(t, levels = 4) {
  const columns = 257, rows = 257, samples = new Uint16Array(columns * rows);
  for (let r = 0; r < rows; r++) for (let c = 0; c < columns; c++) {
    samples[r * columns + c] = Math.round(65535 * (0.5 + 0.25 * Math.sin(c * 0.07) * Math.cos(r * 0.05) + 0.2 * Math.sin((c + r) * 0.013)));
  }
  const ir = { id: "t", columns, rows, minX: -128, minZ: -128, sizeX: 256, sizeZ: 256, baseY: 0, heightScale: 40,
    levels, spacing: 256 / (32 * 2 ** (levels - 1)), lodErrors: [0, 0.02, 0.06, 0.2], pixelError: 1, triplanarSlope: 35,
    layers: [{ color: "#808080", roughness: 0.9, scale: 4, minHeight: -1e6, maxHeight: 1e6, minSlope: 0, maxSlope: 90, heightBlend: 2, slopeBlend: 6 }] };
  return t.sceneTerrainBuild(ir, samples);
}

test("samples decode little-endian and reject a wrong length", () => {
  const { t } = runtime();
  const samples = t.sceneTerrainSamples(new Uint8Array([1, 2, 255, 0]), 2);
  assert.deepEqual(Array.from(samples), [0x0201, 0x00ff]);
  assert.equal(t.sceneTerrainSamples(new Uint8Array(3), 2), null);
});

test("browser heights match scene.Terrain.HeightAt from the Go fixture", () => {
  const { t } = runtime();
  const model = fixtureModel(t);
  assert.equal(fixture.queries.length, 40);
  for (const q of fixture.queries) {
    // Go works in float64 and the browser model stores float32 normalized
    // samples, so the bound is one float32 step of the height scale.
    assert.ok(Math.abs(t.sceneTerrainHeight(model, q.X, q.Z) - q.Height) < 1e-5, `height at ${q.X},${q.Z}`);
  }
});

test("node bounds enclose every sample", () => {
  const { t } = runtime();
  const model = squareModel(t);
  const root = model.levels - 1;
  let low = Infinity, high = -Infinity;
  for (const h of model.heights) { low = Math.min(low, h * 40); high = Math.max(high, h * 40); }
  assert.ok(model.nodeMin[root][0] <= low + 1e-4 && model.nodeMax[root][0] >= high - 1e-4);
  assert.equal(model.nodeMin[0].length, 64);
  assert.equal(model.normals[0].width, 257);
  assert.equal(model.normals.at(-1).width, 1);
});

// patchVertex mirrors vertexMain in terrain-wgsl.ts and terrain-glsl.ts.
function patchVertex(t, model, uniforms, eye, patch, i, j) {
  const s = uniforms[8], loX = uniforms[0], loZ = uniforms[1], hiX = loX + uniforms[2], hiZ = loZ + uniforms[3];
  let ix = patch[0] + i * patch[2], iz = patch[1] + j * patch[2];
  const px = Math.min(loX + ix * s, hiX), pz = Math.min(loZ + iz * s, hiZ);
  const d = Math.hypot(eye.x - px, eye.y - t.sceneTerrainHeight(model, px, pz), eye.z - pz);
  const level = Math.min(patch[3], 11), start = uniforms[16 + level * 4], inv = uniforms[17 + level * 4];
  const k = Math.max(0, Math.min(1, (d - start) * inv));
  ix -= (i % 2) * patch[2] * k; iz -= (j % 2) * patch[2] * k;
  return [Math.min(loX + ix * s, hiX), Math.min(loZ + iz * s, hiZ)];
}

function selectAt(t, model, eye, height = 1080, fov = 60) {
  const ranges = new Float64Array(12), patches = new Float32Array(4096 * 4);
  t.sceneTerrainRanges(model, height, fov, 1, ranges);
  const count = t.sceneTerrainSelect(model, eye, null, ranges, patches);
  const uniforms = t.sceneTerrainUniforms(model, ranges, [0, 0, 0, 0], false, new Float32Array(144));
  const list = [];
  for (let p = 0; p < count; p++) list.push(Array.from(patches.subarray(p * 4, p * 4 + 4)));
  return { list, uniforms, ranges };
}

test("selection covers the terrain exactly once at every camera", () => {
  const { t } = runtime();
  const model = squareModel(t);
  for (const eye of [{ x: 0, y: 30, z: 0 }, { x: -120, y: 5, z: 90 }, { x: 300, y: 80, z: -300 }, { x: 0, y: 2000, z: 0 }]) {
    const { list } = selectAt(t, model, eye);
    let area = 0;
    const cells = new Set();
    for (const [ox, oz, scale] of list) {
      const side = 16 * scale; area += side * side;
      // Overlap check on the finest-index grid at 16-unit resolution.
      for (let a = ox; a < ox + side; a += 16) for (let b = oz; b < oz + side; b += 16) {
        const key = a + ":" + b;
        assert.ok(!cells.has(key), "patches overlap at " + key);
        cells.add(key);
      }
    }
    assert.equal(area, 256 * 256, "selection area for eye " + JSON.stringify(eye));
  }
});

test("neighbouring patches differ by at most one level and share morphed edges (no cracks)", () => {
  const { t } = runtime();
  const model = squareModel(t);
  let finerEdges = 0;
  for (const eye of [{ x: 3, y: 12, z: -7 }, { x: -100, y: 25, z: 110 }, { x: 60, y: 4, z: 60 }, { x: 0, y: 140, z: -50 }]) {
    const { list, uniforms } = selectAt(t, model, eye);
    // Coarse-edge vertex positions of every patch, keyed by world position.
    const key = (p) => p[0].toFixed(4) + ":" + p[1].toFixed(4);
    const edgeVertices = new Map();
    for (const patch of list) {
      const set = new Set();
      for (let n = 0; n <= 16; n++) for (const [i, j] of [[n, 0], [n, 16], [0, n], [16, n]]) set.add(key(patchVertex(t, model, uniforms, eye, patch, i, j)));
      edgeVertices.set(patch, set);
    }
    for (const a of list) for (const b of list) {
      if (a === b) continue;
      const sa = 16 * a[2], sb = 16 * b[2];
      const touchX = a[0] + sa === b[0] || b[0] + sb === a[0], overlapZ = a[1] < b[1] + sb && b[1] < a[1] + sa;
      const touchZ = a[1] + sa === b[1] || b[1] + sb === a[1], overlapX = a[0] < b[0] + sb && b[0] < a[0] + sa;
      if (!((touchX && overlapZ) || (touchZ && overlapX))) continue;
      assert.ok(Math.abs(a[3] - b[3]) <= 1, `adjacent levels ${a[3]} and ${b[3]}`);
      if (a[3] !== b[3] - 1) continue;
      finerEdges++;
      // Every vertex on the finer patch's shared edge must land on a vertex
      // of the coarser patch's edge; anything else is a T-junction crack.
      for (let n = 0; n <= 16; n++) {
        for (const [i, j] of [[n, 0], [n, 16], [0, n], [16, n]]) {
          const v = patchVertex(t, model, uniforms, eye, a, i, j);
          const onShared = touchX ? (Math.abs(v[0] - (model.minX + (a[0] + sa === b[0] ? b[0] : a[0]) * model.spacing)) < 1e-6) : (Math.abs(v[1] - (model.minZ + (a[1] + sa === b[1] ? b[1] : a[1]) * model.spacing)) < 1e-6);
          if (!onShared) continue;
          const inRange = touchX ? v[1] >= model.minZ + b[1] * model.spacing - 1e-6 && v[1] <= model.minZ + (b[1] + sb) * model.spacing + 1e-6 : v[0] >= model.minX + b[0] * model.spacing - 1e-6 && v[0] <= model.minX + (b[0] + sb) * model.spacing + 1e-6;
          if (inRange) assert.ok(edgeVertices.get(b).has(key(v)), `crack: finer vertex ${key(v)} is not on the level ${b[3]} edge`);
        }
      }
    }
  }
  assert.ok(finerEdges > 0, "the cameras exercised at least one level boundary");
});

test("ranges hold the next level's error under the pixel budget and grow monotonically", () => {
  const { t } = runtime();
  const model = squareModel(t);
  const ranges = t.sceneTerrainRanges(model, 1080, 60, 1, new Float64Array(12));
  const k = 1080 / (2 * Math.tan(Math.PI / 6));
  for (let level = 0; level < model.levels - 1; level++) {
    assert.ok(ranges[level] >= model.ir.lodErrors[level + 1] * k - 1e-6, "error bound at level " + level);
    if (level > 0) assert.ok(ranges[level] > ranges[level - 1]);
  }
  assert.equal(ranges[model.levels - 1], 1e30);
  // A taller viewport needs farther ranges for the same pixel error.
  const tall = t.sceneTerrainRanges(model, 2160, 60, 1, new Float64Array(12));
  assert.ok(tall[1] >= ranges[1]);
});

test("frustum culling drops patches behind the camera", () => {
  const { t } = runtime();
  const model = squareModel(t);
  const ranges = t.sceneTerrainRanges(model, 1080, 60, 1, new Float64Array(12));
  // A plane pair that keeps only x > 0 (left and right planes both face +x).
  const planes = new Float64Array([1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1]);
  const all = t.sceneTerrainSelect(model, { x: 0, y: 20, z: 0 }, null, ranges, new Float32Array(16384));
  const out = new Float32Array(16384);
  const culled = t.sceneTerrainSelect(model, { x: 0, y: 20, z: 0 }, planes, ranges, out);
  assert.ok(culled < all && culled > 0);
});

test("uniform block packs layers and level morph ranges", () => {
  const { t } = runtime();
  const model = fixtureModel(t);
  const ranges = t.sceneTerrainRanges(model, 720, 60, 1, new Float64Array(12));
  const u = t.sceneTerrainUniforms(model, ranges, [1, 0, 0, 0], true, new Float32Array(144));
  assert.equal(u[0], fixture.terrain.minX); assert.equal(u[6], fixture.terrain.columns);
  assert.equal(u[10], 2, "layer count"); assert.equal(u[15], 1, "lod tint");
  assert.equal(u[64 + 5], 1, "layer 0 albedo flag");
  assert.equal(u[64 + 16 + 12], 30, "layer 1 min slope");
  assert.equal(u[64 + 12], -1e6, "open min slope widens");
  assert.equal(u[16 + (model.levels - 1) * 4], Math.fround(1e30), "coarsest level never morphs");
});

test("index buffer splits each cell on the shared diagonal, wound upward", () => {
  const { t } = runtime();
  const indices = t.sceneTerrainIndices();
  assert.equal(indices.length, 16 * 16 * 6);
  assert.deepEqual(Array.from(indices.subarray(0, 6)), [0, 18, 1, 0, 17, 18]);
});

test("fragment patches insert the terrain surface into the renderer's PBR fragments", () => {
  const { t } = runtime();
  const wgsl = "fn gosxPBRColor(in: VertexOutput) -> vec4f {\n    var albedo = vec3f(1);\n    let V = normalize(frame.cameraPos - in.worldPos);\n}\n@fragment fn fragmentMain(in: VertexOutput) -> @location(0) vec4f { return gosxPBRColor(in); }";
  const patched = t.sceneTerrainFragmentWGSL(wgsl);
  assert.match(patched, /terrainSurfaceAt\(in, terrainDx/);
  assert.match(patched, /let terrainDx = dpdx\(in.worldPos\)/);
  assert.ok(patched.indexOf("struct TerrainBlock") < patched.indexOf("@fragment fn fragmentMain"));
  assert.throws(() => t.sceneTerrainFragmentWGSL("fn other() {}"), /anchor missing/);
  const glsl = "void main() {\n    vec3 V = normalize(u_cameraPosition - v_worldPosition);\n}";
  assert.match(t.sceneTerrainFragmentGLSL(glsl), /terrainSurfaceAt\(terrainDx, terrainDy, terrainUx, terrainUy, albedo, N, roughness\)/);
  assert.match(t.SCENE_TERRAIN_VERTEX_GLSL, /texelFetch\(u_terrainHeights/);
  assert.match(t.sceneTerrainVertexWGSL("struct FrameUniforms { cameraPos: vec3f };"), /@vertex fn vertexMain\(in: TerrainIn\)/);
});

test("the shipped renderer fragments still carry every patch anchor", () => {
  const gpu = readSceneRendererBackendSrc("webgpu"), gl = readSceneRendererBackendSrc("webgl");
  for (const anchor of ["@fragment fn fragmentMain(in: VertexOutput)", "fn gosxPBRColor(in: VertexOutput) -> vec4f {", "    let V = normalize(frame.cameraPos - in.worldPos);"]) assert.ok(gpu.includes(anchor), anchor);
  for (const anchor of ["void main() {", "    vec3 V = normalize(u_cameraPosition - v_worldPosition);"]) assert.ok(gl.includes(anchor), anchor);
  assert.match(gpu, /window\.__gosx_scene3d_terrain_api\?\.webgpu\(terrainResources, mainPass/);
  assert.match(gl, /window\.__gosx_scene3d_terrain_api\?\.webgl\(terrainResources, gl/);
});

test("a src terrain fetches its samples once and reports pending until they land", async () => {
  const context = runtime();
  const { t } = context;
  const ir = { ...fixture.terrain, heights: "", src: "/terrain.r16" };
  const bytes = t.sceneTerrainBytes(fixture.terrain.heights);
  let calls = 0;
  context.fetch = async () => { calls++; return { arrayBuffer: async () => bytes.buffer.slice(0) }; };
  const res = {};
  assert.equal(t.sceneTerrainModelFor(res, ir), null);
  assert.equal(t.sceneTerrainModelFor(res, ir), null);
  await new Promise((resolve) => setTimeout(resolve, 0));
  await new Promise((resolve) => setTimeout(resolve, 0));
  const model = t.sceneTerrainModelFor(res, ir);
  assert.ok(model && model.columns === ir.columns);
  assert.equal(calls, 1);
});

test("wind gusts match scene.Wind.Sample from the Go fixture", () => {
  const { t } = runtime();
  const state = t.sceneWindUpdate({ wind: fixture.wind }, 0);
  for (const g of fixture.gusts) {
    const speed = state[2] * t.sceneWindGust(state, g.X, g.Z, g.T);
    // The browser state is float32, as the GPU reads it: agree to 1e-5 m/s.
    assert.ok(Math.abs(speed * state[0] - g.VX) < 1e-5 && Math.abs(speed * state[1] - g.VZ) < 1e-5, `wind at ${g.X},${g.Z},${g.T}`);
  }
});

test("wind state packs direction, speed, time and per-batch sway", () => {
  const context = runtime();
  const { t } = context;
  const state = t.sceneWindUpdate({ wind: { directionX: 1, directionZ: 0, speed: 8, gust: 0.2, gustScale: 30, gustSpeed: 6, frequency: 2 } }, 12.5);
  assert.deepEqual(Array.from(state), [1, 0, 8, 12.5, 0.2, 30, 6, 2].map(Math.fround));
  assert.equal(context.window.__gosx_scene3d_wind, state);
  const batch = t.sceneWindFor({ sway: 0.8, height: 0.6 });
  assert.equal(batch[8], Math.fround(0.8)); assert.equal(batch[9], Math.fround(0.6));
  assert.equal(t.sceneWindFor(undefined)[8], 0, "an unswayed batch reads zero sway");
  t.sceneWindUpdate({}, 1);
  assert.equal(t.sceneWindFor({ sway: 1, height: 1 })[8], 0, "no Environment.Wind means no sway");
});

test("both renderers sway instanced batches from the shared wind without breaking derived shaders", () => {
  const gpu = readSceneRendererBackendSrc("webgpu"), gl = readSceneRendererBackendSrc("webgl");
  assert.match(gpu, /"    let world = gosxWindSway\(model, in\.position\);"/);
  assert.match(gpu, /@group\(1\) @binding\(0\) var<uniform> material: MaterialUniforms;", "fn gosxWindSway/);
  assert.match(gpu, /f\.set\(window\.__gosx_scene3d_terrain_api\.windFor\(mat\.wind\), 20\)/);
  assert.match(gpu, /createMaterialBindGroup\(mesh\.wind \? Object\.assign\(\{\}, mat, \{ wind: mesh\.wind \}\) : mat/);
  // The GPU-driven variant still finds its model and colour anchors.
  assert.match(gpu, /"    let model = mat4x4f\(in\.instanceMatrix0, in\.instanceMatrix1, in\.instanceMatrix2, in\.instanceMatrix3\);"/);
  // The crowd variant still finds its world-position anchor.
  assert.match(gl, /vec4 worldPos = a_instanceMatrix \* vec4\(a_position, 1\.0\); worldPos\.xyz \+= gosxWindOffset\(a_instanceMatrix, a_position\);/);
  assert.match(gl, /gl\.uniform4fv\(ip\.uniforms\.wind, window\.__gosx_scene3d_terrain_api\?\.windFor\(mesh\.wind\) \|\| SCENE_NO_CUT\)/);
});
