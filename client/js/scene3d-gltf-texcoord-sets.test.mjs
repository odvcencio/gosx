import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";

const ts = createRequire(new URL("../runtime/package.json", import.meta.url))("typescript");

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");

function readSource(name) {
  return fs.readFileSync(name.startsWith("../") ? path.join(__dirname, name) : path.join(srcDir, name), "utf8");
}

// Match the production TypeScript syntax before evaluating the real loader.
const gltfSource = ts.transpileModule(readSource("../runtime/scene3d/gltf.ts"), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText;

const warnings = [];
function createLoaderContext() {
  const sandbox = {
    console: { warn: (...a) => { warnings.push(a.join(' ')); }, error: () => {}, log: () => {} },
    Math, JSON, Number, Object, Array, String, Boolean, isFinite,
    Float32Array, Uint8Array, Uint16Array, Uint32Array, Int8Array, Int16Array,
    ArrayBuffer, DataView, TextDecoder, Error,
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  const context = vm.createContext(sandbox);
  vm.runInContext(readSource("11-scene-math.ts"), context, { filename: "11-scene-math.ts" });
  vm.runInContext(gltfSource, context, { filename: "gltf.ts" });
  return context;
}



const PNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";

// One triangle with distinct UV sets so the chosen stream is identifiable.
const UV0 = [0, 0, 1, 0, 0, 1];
const UV1 = [0.25, 0.25, 0.75, 0.25, 0.25, 0.75];

function triangleDoc(material) {
  const floats = new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0, ...UV0, ...UV1]);
  const doc = {
    asset: { version: "2.0" },
    scene: 0,
    scenes: [{ nodes: [0] }],
    nodes: [{ mesh: 0 }],
    meshes: [{ primitives: [{ attributes: { POSITION: 0, TEXCOORD_0: 1, TEXCOORD_1: 2 }, material: 0 }] }],
    materials: [material],
    images: [{ uri: PNG }],
    textures: [{ source: 0 }, { source: 0 }],
    accessors: [
      { bufferView: 0, byteOffset: 0, componentType: 5126, count: 3, type: "VEC3", min: [0, 0, 0], max: [1, 1, 0] },
      { bufferView: 0, byteOffset: 36, componentType: 5126, count: 3, type: "VEC2" },
      { bufferView: 0, byteOffset: 60, componentType: 5126, count: 3, type: "VEC2" },
    ],
    bufferViews: [{ buffer: 0, byteOffset: 0, byteLength: floats.byteLength }],
    buffers: [{ byteLength: floats.byteLength }],
  };
  return { doc, binary: floats.buffer };
}

function extract(material) {
  warnings.length = 0;
  const context = createLoaderContext();
  const { doc, binary } = triangleDoc(material);
  context.gltfDoc = doc;
  context.binaryBuffer = binary;
  const result = vm.runInContext("gltfExtractScene(gltfDoc, binaryBuffer)", context);
  return {
    uvs: Array.from(result.objects[0].vertices.uvs),
    uvs1: result.objects[0].vertices.uvs1 ? Array.from(result.objects[0].vertices.uvs1) : null,
    material: result.materials ? result.materials[0] : result.objects[0].material,
  };
}

test("a material with no UV1 maps keeps TEXCOORD_0", () => {
  const out = extract({ pbrMetallicRoughness: { baseColorTexture: { index: 0 } } });
  assert.deepEqual(out.uvs, UV0);
  assert.equal(warnings.length, 0);
});

test("occlusion on texCoord 1 alone selects TEXCOORD_1", () => {
  const out = extract({ occlusionTexture: { index: 0, texCoord: 1 } });
  assert.deepEqual(out.uvs, UV1);
});

test("every textured map on texCoord 1 selects TEXCOORD_1 without a warning", () => {
  const out = extract({
    pbrMetallicRoughness: { baseColorTexture: { index: 0, texCoord: 1 } },
    normalTexture: { index: 1, texCoord: 1 },
    emissiveTexture: { index: 1, texCoord: 1 },
  });
  assert.deepEqual(out.uvs, UV1);
  assert.equal(warnings.length, 0);
});

test("a KHR_texture_transform texCoord overrides the textureInfo texCoord", () => {
  const out = extract({
    emissiveTexture: { index: 0, texCoord: 0, extensions: { KHR_texture_transform: { texCoord: 1 } } },
  });
  assert.deepEqual(out.uvs, UV1);
});

test("occlusion on UV1 with base colour on UV0 carries a second UV stream", () => {
  const out = extract({
    pbrMetallicRoughness: { baseColorTexture: { index: 0 } },
    occlusionTexture: { index: 1, texCoord: 1 },
    emissiveTexture: { index: 1, texCoord: 1 },
  });
  assert.deepEqual(out.uvs, UV0);
  assert.deepEqual(out.uvs1, UV1);
  assert.equal(out.material.occlusionTexCoord, 1);
  assert.equal(out.material.emissiveTexCoord, 1);
  assert.equal(out.material.uvSet1, 1);
  assert.equal(warnings.length, 0);
});

test("a material that needs no second stream carries none", () => {
  const out = extract({ pbrMetallicRoughness: { baseColorTexture: { index: 0 } }, occlusionTexture: { index: 1 } });
  assert.equal(out.uvs1, null);
  assert.equal(out.material.occlusionTexCoord, undefined);
});

test("a normal map on a different set than the base colour still warns once", () => {
  const out = extract({
    name: "hull",
    pbrMetallicRoughness: { baseColorTexture: { index: 0 } },
    normalTexture: { index: 1, texCoord: 1 },
  });
  assert.deepEqual(out.uvs, UV0);
  assert.equal(warnings.filter((w) => /different UV sets/.test(w) && /hull/.test(w)).length, 1);
});

test("a missing TEXCOORD_1 falls back to TEXCOORD_0", () => {
  warnings.length = 0;
  const context = createLoaderContext();
  const { doc, binary } = triangleDoc({ occlusionTexture: { index: 0, texCoord: 1 } });
  delete doc.meshes[0].primitives[0].attributes.TEXCOORD_1;
  context.gltfDoc = doc;
  context.binaryBuffer = binary;
  const result = vm.runInContext("gltfExtractScene(gltfDoc, binaryBuffer)", context);
  assert.deepEqual(Array.from(result.objects[0].vertices.uvs), UV0);
});
