// glTF required extensions (gltf.ts): a file whose extensionsRequired lists an
// extension the loader does not implement fails to load, and the error names
// the JSON path and the extension. An extension only in extensionsUsed loads
// with one warning. The supported list equals what the loader reads.
//
// Like 19-scene-gltf.test.mjs, the real gltf.ts runs in a VM context with no
// network and no GPU; every fixture is a small JSON document with an inline
// ArrayBuffer for the binary chunk.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";

const ts = createRequire(new URL("../runtime/package.json", import.meta.url))("typescript");
const __dirname = path.dirname(fileURLToPath(import.meta.url));

const mathSource = fs.readFileSync(path.join(__dirname, "bootstrap-src", "11-scene-math.ts"), "utf8");
const rawGltfSource = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "gltf.ts"), "utf8");
const gltfSource = ts.transpileModule(rawGltfSource, {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText;

function createLoaderContext() {
  const warnings = [];
  const sandbox = {
    console: { warn: (...args) => warnings.push(args.join(" ")), error: () => {}, log: () => {} },
    Math, JSON, Number, Object, Array, String, Boolean, isFinite,
    Float32Array, Uint8Array, Uint16Array, Uint32Array, Int8Array, Int16Array,
    ArrayBuffer, DataView, TextDecoder, Error, URL,
    Blob: class {},
    fetch: () => Promise.reject(new Error("fetch not configured")),
    location: { href: "http://localhost:3000/" },
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  sandbox.URL = { createObjectURL: () => "blob:fake" };
  const context = vm.createContext(sandbox);
  vm.runInContext(mathSource, context, { filename: "11-scene-math.ts" });
  vm.runInContext(gltfSource, context, { filename: "gltf.ts" });
  return { context, warnings };
}

// load extracts one indexed triangle with the given top-level fields and
// optional primitive extensions, and returns the object count.
function load(context, top, primitiveExtensions) {
  context.top = top;
  context.primitiveExtensions = primitiveExtensions || null;
  return vm.runInContext(`
    var buffer = new ArrayBuffer(4 * 9 + 8);
    new Float32Array(buffer, 0, 9).set([0, 0, 0, 1, 0, 0, 0, 1, 0]);
    new Uint16Array(buffer, 36, 4).set([0, 1, 2, 0]);
    var primitive = { attributes: { POSITION: 0 }, indices: 1, mode: 4 };
    if (primitiveExtensions) { primitive.extensions = primitiveExtensions; }
    var doc = {
      asset: { version: "2.0" },
      accessors: [
        { bufferView: 0, byteOffset: 0, componentType: 5126, count: 3, type: "VEC3" },
        { bufferView: 0, byteOffset: 36, componentType: 5123, count: 3, type: "SCALAR" }
      ],
      bufferViews: [{ buffer: 0, byteOffset: 0, byteLength: buffer.byteLength }],
      meshes: [{ name: "tri", primitives: [primitive] }],
      nodes: [{ mesh: 0 }],
      scenes: [{ nodes: [0] }]
    };
    Object.keys(top).forEach(function (key) { doc[key] = top[key]; });
    gltfExtractScene(doc, buffer).objects.length;
  `, context);
}

test("an unknown required extension rejects the load, naming its index and name", () => {
  const { context } = createLoaderContext();
  assert.throws(
    () => load(context, { extensionsUsed: ["VENDOR_fancy"], extensionsRequired: ["VENDOR_fancy"] }),
    (err) => err.message.includes('extensionsRequired[0]') && err.message.includes("VENDOR_fancy"),
  );
});

test("two unknown required extensions are both named, each with its index", () => {
  const { context } = createLoaderContext();
  assert.throws(
    () => load(context, { extensionsRequired: ["KHR_mesh_quantization", "VENDOR_one", "VENDOR_two"] }),
    (err) =>
      err.message.includes('extensionsRequired[1] "VENDOR_one"') &&
      err.message.includes('extensionsRequired[2] "VENDOR_two"') &&
      !err.message.includes("KHR_mesh_quantization"),
  );
});

test("an unknown extension listed only in extensionsUsed loads with one warning", () => {
  const { context, warnings } = createLoaderContext();
  const count = load(context, { extensionsUsed: ["VENDOR_fancy"] });
  assert.equal(count, 1);
  const named = warnings.filter((w) => w.includes("VENDOR_fancy"));
  assert.equal(named.length, 1, `warnings: ${JSON.stringify(warnings)}`);
  assert.equal(warnings.length, 1, `warnings: ${JSON.stringify(warnings)}`);
});

test("a file that requires only supported extensions loads as it does without them", () => {
  const { context } = createLoaderContext();
  const plain = load(context, {});
  const required = load(context, {
    extensionsUsed: ["KHR_mesh_quantization", "KHR_materials_unlit"],
    extensionsRequired: ["KHR_mesh_quantization", "KHR_materials_unlit"],
  });
  assert.equal(plain, 1);
  assert.equal(required, plain);
});

test("a required Draco extension keeps the Draco message", () => {
  const { context } = createLoaderContext();
  assert.throws(
    () => load(
      context,
      { extensionsUsed: ["KHR_draco_mesh_compression"], extensionsRequired: ["KHR_draco_mesh_compression"] },
      { KHR_draco_mesh_compression: { bufferView: 0, attributes: {} } },
    ),
    (err) =>
      err.message.includes("Re-export the asset without Draco compression") &&
      !err.message.includes("does not support"),
  );
});

test("the supported list equals the extensions the loader reads", () => {
  const listMatch = rawGltfSource.match(/var GLTF_SUPPORTED_EXTENSIONS = \[([\s\S]*?)\];/);
  assert.ok(listMatch, "GLTF_SUPPORTED_EXTENSIONS found in gltf.ts");
  const listed = new Set([...listMatch[1].matchAll(/"((?:KHR|EXT|GOSX)_[A-Za-z0-9_]+)"/g)].map((m) => m[1]));
  const read = new Set([...rawGltfSource.matchAll(/gltfExtension\([^,()]+,\s*"((?:KHR|EXT|GOSX)_[A-Za-z0-9_]+)"/g)].map((m) => m[1]));
  assert.ok(read.size > 10, "the loader reads many extensions");
  // Draco is rejected by name at the point of use. KHR_texture_basisu is read
  // only to warn and drop the map: the loader cannot transcode KTX2.
  const notImplemented = new Set(["KHR_draco_mesh_compression", "KHR_texture_basisu"]);
  const missing = [...read].filter((name) => !notImplemented.has(name) && !listed.has(name));
  assert.deepEqual(missing, [], `the loader reads these but the list lacks them: ${missing.join(", ")}`);
  for (const name of notImplemented) {
    assert.ok(!listed.has(name), `${name} must not be listed as supported`);
  }
});
