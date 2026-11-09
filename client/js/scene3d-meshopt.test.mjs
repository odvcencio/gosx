// EXT_meshopt_compression decoding. The fixtures are gltfpack 0.22 output for
// the generated sphere in testdata/blender-ktx2: cmp.glb is compressed, ref.glb
// is the same asset without compression (both written with -noq so the bytes
// are comparable), and out_cc.glb uses the octahedral normal filter.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const ts = require("../runtime/node_modules/typescript");
const fixtures = path.join(__dirname, "testdata", "meshopt");
const transpile = (file) => ts.transpileModule(fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", file), "utf8"), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText;
const meshoptSource = transpile("asset-meshopt.ts");
const gltfSource = transpile("gltf.ts");
const sceneMathSource = fs.readFileSync(path.join(__dirname, "bootstrap-src", "11-scene-math.ts"), "utf8");

function meshopt() {
  const window = {};
  vm.runInNewContext(meshoptSource, { window, Uint8Array, Uint32Array, DataView, Math, Error, Object });
  return window.__gosx_scene3d_asset_ext.meshopt;
}

function load(name) {
  const data = fs.readFileSync(path.join(fixtures, name));
  const jsonLength = data.readUInt32LE(12);
  return { data, json: JSON.parse(data.subarray(20, 20 + jsonLength).toString()), binary: 20 + jsonLength + 8 };
}

function decodeView(glb, index) {
  const ext = glb.json.bufferViews[index].extensions.EXT_meshopt_compression;
  const source = new Uint8Array(glb.data.buffer, glb.data.byteOffset + glb.binary + (ext.byteOffset || 0), ext.byteLength);
  return meshopt().decode(ext.count, ext.byteStride, source, ext.mode, ext.filter);
}

function plainView(glb, index) {
  const view = glb.json.bufferViews[index];
  const start = glb.data.byteOffset + glb.binary + (view.byteOffset || 0);
  return new Uint8Array(glb.data.buffer.slice(start, start + view.byteLength));
}

const normalizeTriangle = (arr, i) => {
  const t = [arr[i], arr[i + 1], arr[i + 2]];
  const m = t.indexOf(Math.min(...t));
  return [t[m], t[(m + 1) % 3], t[(m + 2) % 3]].join(",");
};

test("vertex attribute views decode to the exact uncompressed bytes", () => {
  const compressed = load("cmp.glb");
  const reference = load("ref.glb");
  let checked = 0;
  compressed.json.bufferViews.forEach((view, i) => {
    const ext = view.extensions && view.extensions.EXT_meshopt_compression;
    if (!ext || ext.mode !== "ATTRIBUTES") return;
    assert.deepEqual(Array.from(decodeView(compressed, i)), Array.from(plainView(reference, i)), "bufferView " + i);
    checked++;
  });
  assert.equal(checked, 3, "positions, normals and uvs");
});

test("the triangle index codec decodes every triangle, rotated at most", () => {
  const compressed = load("cmp.glb");
  const reference = load("ref.glb");
  const index = compressed.json.bufferViews.findIndex((v) => v.extensions && v.extensions.EXT_meshopt_compression.mode === "TRIANGLES");
  const ext = compressed.json.bufferViews[index].extensions.EXT_meshopt_compression;
  assert.equal(ext.count, 4608);
  const got = new Uint16Array(decodeView(compressed, index).buffer);
  const want = new Uint16Array(plainView(reference, index).buffer);
  assert.equal(got.length, want.length);
  for (let i = 0; i < got.length; i += 3) {
    assert.equal(normalizeTriangle(got, i), normalizeTriangle(want, i), "triangle " + i / 3);
  }
});

test("the octahedral filter restores unit normals close to the source", () => {
  const compressed = load("out_cc.glb");
  const reference = load("refq.glb"); // the same asset quantized without compression
  const index = compressed.json.bufferViews.findIndex((v) => v.extensions && v.extensions.EXT_meshopt_compression.filter === "OCTAHEDRAL");
  const decoded = new Int8Array(decodeView(compressed, index).buffer);
  const normalView = reference.json.accessors[reference.json.meshes[0].primitives[0].attributes.NORMAL].bufferView;
  const plain = new Int8Array(plainView(reference, normalView).buffer);
  const want = Float32Array.from(plain, (x) => x / 127); // int8 normals, four bytes per vertex
  // gltfpack orders vertices differently with and without the filter, so match
  // each decoded normal to the nearest source normal instead of by index.
  let worst = 0;
  for (let v = 0; v < 825; v++) {
    const n = [decoded[v * 4] / 127, decoded[v * 4 + 1] / 127, decoded[v * 4 + 2] / 127];
    const length = Math.hypot(...n);
    assert.ok(Math.abs(length - 1) < 0.02, "vertex " + v + " length " + length);
    let best = Infinity;
    for (let u = 0; u < 825; u++) {
      best = Math.min(best, Math.max(Math.abs(n[0] - want[u * 4]), Math.abs(n[1] - want[u * 4 + 1]), Math.abs(n[2] - want[u * 4 + 2])));
    }
    worst = Math.max(worst, best);
  }
  assert.ok(worst < 0.04, "worst distance to a source normal " + worst);
});

test("malformed input fails with a named error", () => {
  const api = meshopt();
  assert.throws(() => api.decode(4, 12, new Uint8Array(40), "ATTRIBUTES", "NONE"), /bad vertex buffer header/);
  const v1 = new Uint8Array(40); v1[0] = 0xa1;
  assert.throws(() => api.decode(4, 12, v1, "ATTRIBUTES", "NONE"), /version 1 is not supported/);
  assert.throws(() => api.decode(4, 12, new Uint8Array(40).fill(0xa0), "INDICES", "NONE"), /unknown mode INDICES/);
  assert.throws(() => api.decode(3, 2, new Uint8Array(40), "TRIANGLES", "NONE"), /bad index buffer header/);
  assert.throws(() => api.decode(0, 16, new Uint8Array(17).fill(0).map((_, i) => (i ? 0 : 0xa0)), "ATTRIBUTES", "QUATERNION"), /QUATERNION/);
  assert.throws(() => api.decode(4, 6, new Uint8Array(40), "ATTRIBUTES", "NONE"), /multiple of 4/);
});

// The whole loader: the same asset, compressed and plain, extracts the same mesh.
function loader() {
  const window = { location: { href: "https://example.test/" } };
  const fetch = async (url) => {
    const bytes = fs.readFileSync(path.join(fixtures, path.basename(new URL(url).pathname)));
    return { ok: true, status: 200, arrayBuffer: async () => bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) };
  };
  const console = { warn: () => {}, error: () => {}, log: () => {} };
  class FakeBlob { constructor(parts, opts) { this.type = opts && opts.type; } }
  const urlWithBlob = new Proxy(URL, {});
  urlWithBlob.createObjectURL = () => "blob:image";
  const factory = new Function("window", "loadManifest", "console", "URL", "fetch", "Blob",
    sceneMathSource + "\n" + meshoptSource + "\n" + gltfSource + "\nreturn { sceneLoadGLTFModel };");
  return factory(window, () => null, console, urlWithBlob, fetch, FakeBlob);
}

test("the loader reads a meshopt-compressed GLB as the uncompressed mesh", async () => {
  const compressed = await loader().sceneLoadGLTFModel("https://example.test/cmp.glb");
  const plain = await loader().sceneLoadGLTFModel("https://example.test/ref.glb");
  const a = compressed.objects[0].vertices, b = plain.objects[0].vertices;
  assert.equal(a.count, b.count);
  assert.equal(a.count, 4608);
  // The index codec may rotate a triangle, so compare triangles as sorted corner lists.
  const triangles = (v) => {
    const out = [];
    for (let t = 0; t < v.count; t += 3) {
      const corners = [0, 1, 2].map((k) => [
        ...Array.from(v.positions.slice((t + k) * 3, (t + k) * 3 + 3)),
        ...Array.from(v.uvs.slice((t + k) * 2, (t + k) * 2 + 2)),
      ].join(","));
      out.push(corners.sort().join("|"));
    }
    return out.sort();
  };
  assert.deepEqual(triangles(a), triangles(b));
});

test("the loader supports a quantized, filtered GLB", async () => {
  const scene = await loader().sceneLoadGLTFModel("https://example.test/out_cc.glb");
  assert.equal(scene.objects[0].vertices.count, 4608);
  for (const n of scene.objects[0].vertices.normals) assert.ok(Math.abs(n) <= 1.0001);
});
