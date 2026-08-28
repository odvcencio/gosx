// glTF loader tests (19-scene-gltf.js) — extension parsing and material mapping.
//
// The loader had no test coverage before this suite. These tests load two
// bootstrap fragments into ONE VM context, the way the shipped bundles
// concatenate them:
//   - 11-scene-math.ts   -> SCENE_IDENTITY_MAT4, sceneMat4Multiply, sceneTRSToMat4
//   - 19-scene-gltf.js   -> the GLB/glTF parser and the extension mapping
//
// The fragments declare plain top-level functions, so running them in a VM
// context publishes those functions as context globals. The tests call them
// directly. No network and no GPU is involved: every fixture is a JSON glTF
// with an inline ArrayBuffer standing in for the GLB binary chunk.
//
// Scope of these tests:
//   - KHR_materials_* factor mapping onto StandardMaterial fields
//   - KHR_texture_transform baked into the UV buffer
//   - KHR_materials_unlit selecting the flat shading path
//   - named errors for the compression extensions the loader cannot decode
//   - animation channel component width, including morph "weights" channels
//   - accessor lookup tables staying inert for inherited or unknown keys

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");

function readSource(name) {
  return fs.readFileSync(name.startsWith("../") ? path.join(__dirname, name) : path.join(srcDir, name), "utf8");
}

function createLoaderContext() {
  const warnings = [];
  const sandbox = {
    console: {
      warn: (...args) => warnings.push(args.join(" ")),
      error: () => {},
      log: () => {},
    },
    Math,
    JSON,
    Number,
    Object,
    Array,
    String,
    Boolean,
    isFinite,
    Float32Array,
    Uint8Array,
    Uint16Array,
    Uint32Array,
    Int8Array,
    Int16Array,
    ArrayBuffer,
    DataView,
    TextDecoder,
    Error,
    URL,
    Blob: class {
      constructor(parts, options) {
        this.parts = parts;
        this.type = options && options.type;
      }
    },
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  sandbox.URL = { createObjectURL: () => "blob:fake" };

  const context = vm.createContext(sandbox);
  vm.runInContext(readSource("11-scene-math.ts"), context, { filename: "11-scene-math.ts" });
  vm.runInContext(readSource("../runtime/scene3d/gltf.ts"), context, { filename: "gltf.ts" });
  return { context, sandbox, warnings };
}

// call runs one loader function inside the VM and returns a realm-free result.
function call(context, expression) {
  return vm.runInContext(expression, context);
}

// plain strips the VM realm prototypes so node:assert can compare values.
function plain(value) {
  return JSON.parse(JSON.stringify(value));
}

// --- Fixtures ---------------------------------------------------------------

// materialDoc wraps one material record in the smallest valid glTF document.
function materialDoc(material) {
  return { asset: { version: "2.0" }, materials: [material] };
}

function extractMaterial(context, material) {
  const doc = JSON.stringify(materialDoc(material));
  return plain(call(context, `gltfExtractMaterial(${doc}, 0, null)`));
}

function extractTexturedMaterial(context) {
  const doc = {
    asset: { version: "2.0" },
    images: [
      { uri: "base.png" },
      { uri: "normal.png" },
      { uri: "metal-rough.png" },
      { uri: "ao.png" },
      { uri: "emissive.png" },
    ],
    textures: [
      { source: 0 },
      { source: 1 },
      { source: 2 },
      { source: 3 },
      { source: 4 },
    ],
    materials: [{
      pbrMetallicRoughness: {
        baseColorTexture: { index: 0 },
        metallicRoughnessTexture: { index: 2 },
      },
      normalTexture: { index: 1 },
      occlusionTexture: { index: 3 },
      emissiveTexture: { index: 4 },
    }],
  };
  return plain(call(context, `gltfExtractMaterial(${JSON.stringify(doc)}, 0, null)`));
}

// One shared image URI feeds both specular slots so the tests can pin the
// role/transfer split even when the source bytes are identical.
function extractSpecularTexturedMaterial(context, imageUri) {
  const doc = {
    asset: { version: "2.0" },
    images: [{ uri: imageUri }],
    textures: [{ source: 0 }],
    materials: [{
      pbrMetallicRoughness: { roughnessFactor: 0.4 },
      extensions: {
        KHR_materials_specular: {
          specularFactor: 0.5,
          specularTexture: { index: 0 },
          specularColorTexture: { index: 0 },
        },
      },
    }],
  };
  return plain(call(context, `gltfExtractMaterial(${JSON.stringify(doc)}, 0, null)`));
}

// --- KHR_materials_ior ------------------------------------------------------

// The loader owns ior normalization itself: the standalone loader context
// loads only 11-scene-math.ts + gltf.ts, so the spec contract (finite >= 1
// valid, explicit 0 = compatibility mode, everything else defaults to 1.5)
// must not lean on the scene material helpers.
test("gltfExtractMaterial normalizes KHR_materials_ior to the spec contract", () => {
  const { context } = createLoaderContext();

  // Omitted extension: no ior field; downstream normalization defaults.
  assert.equal("ior" in extractMaterial(context, { pbrMetallicRoughness: {} }), false);

  // Finite ior >= 1 is valid without the legacy max=5 truncation.
  assert.equal(extractMaterial(context, { extensions: { KHR_materials_ior: { ior: 1 } } }).ior, 1);
  assert.equal(extractMaterial(context, { extensions: { KHR_materials_ior: { ior: 1.33 } } }).ior, 1.33);
  assert.equal(extractMaterial(context, { extensions: { KHR_materials_ior: { ior: 1.5 } } }).ior, 1.5);
  assert.equal(extractMaterial(context, { extensions: { KHR_materials_ior: { ior: 2.42 } } }).ior, 2.42);
  assert.equal(extractMaterial(context, { extensions: { KHR_materials_ior: { ior: 6 } } }).ior, 6);
  assert.equal(extractMaterial(context, { extensions: { KHR_materials_ior: { ior: 40 } } }).ior, 40);

  // Explicit numeric zero is the glTF compatibility mode: preserved as 0,
  // never defaulted and never clamped to 1.
  assert.equal(extractMaterial(context, { extensions: { KHR_materials_ior: { ior: 0 } } }).ior, 0);

  // 0<ior<1, negative, null, non-numeric and missing values default to 1.5.
  assert.equal(extractMaterial(context, { extensions: { KHR_materials_ior: { ior: 0.5 } } }).ior, 1.5);
  assert.equal(extractMaterial(context, { extensions: { KHR_materials_ior: { ior: -1 } } }).ior, 1.5);
  assert.equal(extractMaterial(context, { extensions: { KHR_materials_ior: { ior: null } } }).ior, 1.5);
  assert.equal(extractMaterial(context, { extensions: { KHR_materials_ior: { ior: "2.5" } } }).ior, 1.5);
  assert.equal(extractMaterial(context, { extensions: { KHR_materials_ior: {} } }).ior, 1.5);

  // Non-finite numbers cannot travel through JSON; exercise them directly in
  // the loader realm.
  const nonFinite = call(context,
    "gltfExtractMaterial({asset:{version:'2.0'},materials:[{extensions:{'KHR_materials_ior':{ior: Infinity}}}]}, 0, null)");
  assert.equal(plain(nonFinite).ior, 1.5);
});

// --- Effective alpha mode ---------------------------------------------------

test("gltfExtractMaterial: OPAQUE forces opacity 1, other modes preserve alpha", () => {
  const { context } = createLoaderContext();

  const omitted = extractMaterial(context, { pbrMetallicRoughness: { baseColorFactor: [1, 0, 0, 0] } });
  assert.equal(omitted.alphaMode, "OPAQUE");
  assert.equal(omitted.opacity, 1);
  assert.equal(omitted.color, "#ff0000");

  const opaque = extractMaterial(context, { alphaMode: "OPAQUE", pbrMetallicRoughness: { baseColorFactor: [0, 1, 0, 0.25] } });
  assert.equal(opaque.alphaMode, "OPAQUE");
  assert.equal(opaque.opacity, 1);

  const blendQuarter = extractMaterial(context, { alphaMode: "BLEND", pbrMetallicRoughness: { baseColorFactor: [0, 0, 1, 0.25] } });
  assert.equal(blendQuarter.alphaMode, "BLEND");
  assert.equal(blendQuarter.opacity, 0.25);

  const blendOne = extractMaterial(context, { alphaMode: "BLEND", pbrMetallicRoughness: { baseColorFactor: [1, 1, 1, 1] } });
  assert.equal(blendOne.opacity, 1);

  // MASK keeps its authored alpha; full cutoff support is not asserted here.
  const mask = extractMaterial(context, { alphaMode: "MASK", pbrMetallicRoughness: { baseColorFactor: [1, 1, 0, 0.4] } });
  assert.equal(mask.alphaMode, "MASK");
  assert.equal(mask.opacity, 0.4);

  // The production gate itself: BLEND is always alpha, otherwise by opacity.
  assert.equal(call(context, `gltfIsAlphaMaterial({ alphaMode: "BLEND", opacity: 1 })`), true);
  assert.equal(call(context, `gltfIsAlphaMaterial({ alphaMode: "OPAQUE", opacity: 0.5 })`), true);
  assert.equal(call(context, `gltfIsAlphaMaterial({ alphaMode: "OPAQUE", opacity: 1 })`), false);
});

// One shared in-memory scene: three primitives (points mode 0, LINE_STRIP
// mode 3, triangles mode 4) over a single 3-vertex noncollinear accessor.
function alphaSceneDoc(material) {
  return {
    asset: { version: "2.0" },
    scenes: [{ nodes: [0] }],
    nodes: [{ mesh: 0 }],
    meshes: [{ primitives: [
      { attributes: { POSITION: 0 }, mode: 0, material: 0 },
      { attributes: { POSITION: 0 }, mode: 3, material: 0 },
      { attributes: { POSITION: 0 }, mode: 4, material: 0 },
    ] }],
    materials: [material],
    accessors: [{ bufferView: 0, componentType: 5126, count: 3, type: "VEC3", min: [0, 0, 0], max: [1, 1, 0] }],
    bufferViews: [{ buffer: 0, byteLength: 36 }],
    buffers: [{ byteLength: 36 }],
  };
}

test("gltfExtractScene propagates effective alpha to points, lines and meshes", () => {
  const { context } = createLoaderContext();
  const cases = [
    { name: "omitted-a0", material: { pbrMetallicRoughness: { baseColorFactor: [1, 0, 0, 0] } }, opacity: 1, alpha: false, depthWrite: true },
    { name: "omitted-a25", material: { pbrMetallicRoughness: { baseColorFactor: [1, 0, 0, 0.25] } }, opacity: 1, alpha: false, depthWrite: true },
    { name: "OPAQUE-a0", material: { alphaMode: "OPAQUE", pbrMetallicRoughness: { baseColorFactor: [1, 0, 0, 0] } }, opacity: 1, alpha: false, depthWrite: true },
    { name: "OPAQUE-a25", material: { alphaMode: "OPAQUE", pbrMetallicRoughness: { baseColorFactor: [1, 0, 0, 0.25] } }, opacity: 1, alpha: false, depthWrite: true },
    { name: "BLEND-a25", material: { alphaMode: "BLEND", pbrMetallicRoughness: { baseColorFactor: [1, 0, 0, 0.25] } }, opacity: 0.25, alpha: true, depthWrite: false },
    { name: "BLEND-a1", material: { alphaMode: "BLEND", pbrMetallicRoughness: { baseColorFactor: [1, 0, 0, 1] } }, opacity: 1, alpha: true, depthWrite: false },
    // MASK keeps its authored alpha; production points use alphaMode !== "BLEND", so MASK retains depthWrite: true.
    { name: "MASK-a04", material: { alphaMode: "MASK", pbrMetallicRoughness: { baseColorFactor: [1, 0, 0, 0.4] } }, opacity: 0.4, alpha: true, depthWrite: true },
    // BLEND with alpha 0 is still alpha (alphaMode === "BLEND" gate) and disables depthWrite.
    { name: "BLEND-a0", material: { alphaMode: "BLEND", pbrMetallicRoughness: { baseColorFactor: [1, 0, 0, 0] } }, opacity: 0, alpha: true, depthWrite: false },
  ];
  for (const c of cases) {
    const doc = JSON.stringify(alphaSceneDoc(c.material));
    // Run extraction against a live VM doc object and serialize the SAME
    // object before and after, so the mutation check cannot be vacuous.
    const res = plain(call(context,
      `(function () {` +
      `  var docObj = ${doc};` +
      `  var before = JSON.stringify(docObj);` +
      `  var scene = gltfExtractScene(docObj, new Float32Array([0,0,0, 1,0,0, 0,1,0]).buffer);` +
      `  return { before: before, after: JSON.stringify(docObj), scene: scene };` +
      `})()`));
    const scene = res.scene;
    assert.equal(res.before, res.after, `${c.name}: gltfExtractScene mutated the source doc`);

    assert.equal(scene.points.length, 1);
    assert.equal(scene.points[0].opacity, c.opacity);
    assert.equal(scene.points[0].blendMode, c.alpha ? "alpha" : "");
    assert.equal(scene.points[0].depthWrite, c.depthWrite);

    assert.equal(scene.objects.length, 2);
    const lines = scene.objects.find((o) => o.kind === "lines");
    const mesh = scene.objects.find((o) => o.kind === "gltf-mesh");
    assert.ok(lines && mesh);
    assert.equal(lines.opacity, c.opacity);
    assert.equal(lines.blendMode, c.alpha ? "alpha" : "");
    assert.equal(mesh.renderPass, c.alpha ? "alpha" : "opaque");
    assert.equal(mesh.material.opacity, c.opacity);

    // Extraction never mutates the RGB channels (doc immutability is proven
    // by the before/after comparison above).
    assert.equal(mesh.material.color, "#ff0000");
  }
});

// --- accessor lookup tables -------------------------------------------------

// A hostile or corrupt componentType or type string must fall through to the
// pre-table defaults — scalar width 1, a Float32Array view, values copied
// through normalization unchanged — instead of matching inherited
// Object.prototype keys. The component/type/normalize tables are built
// null-prototype at construction; the interleaved row below also pins the
// second component-format lookup inside gltfReadAccessor, where byte-stride
// reads resolve both the view record and its DataView reader name from the
// same table before hitting the Float32 fallback.
test("accessor lookup tables ignore inherited and unknown keys", () => {
  const { context } = createLoaderContext();
  const result = plain(call(context, `
    var input = new Float32Array([0.25, 0.5]);
    var interleaved = new Float32Array([0.25, 42, 0.5, 43]);
    var rows = ["constructor", "toString", "__proto__", "unknown"].map(function(key) {
      return {
        key: key,
        count: gltfAccessorTypeCount(key),
        view: Array.from(gltfTypedArrayView(input.buffer, 0, key, 2)),
        normalized: Array.from(gltfNormalizeAccessorValues(input, key)),
        interleaved: Array.from(gltfReadAccessor({
          accessors: [{ bufferView: 0, byteOffset: 0, componentType: key, count: 2, type: "SCALAR" }],
          bufferViews: [{ byteOffset: 0, byteStride: 8 }],
        }, 0, interleaved.buffer)),
      };
    });
    ({ rows: rows });
  `));
  for (const row of result.rows) {
    assert.equal(row.count, 1, `${row.key} must read as one scalar component`);
    assert.deepEqual(row.view, [0.25, 0.5], `${row.key} must build a Float32Array view`);
    assert.deepEqual(row.normalized, [0.25, 0.5], `${row.key} must copy through normalization unchanged`);
    assert.deepEqual(
      row.interleaved,
      [0.25, 0.5],
      `${row.key} must read interleaved elements through the Float32 fallback`,
    );
  }
});

// --- KHR_materials_* factor mapping ----------------------------------------

test("gltf material without extensions keeps the base PBR mapping", () => {
  const { context } = createLoaderContext();
  const material = extractMaterial(context, {
    pbrMetallicRoughness: { baseColorFactor: [1, 1, 1, 1], roughnessFactor: 0.4, metallicFactor: 0.9 },
  });
  assert.equal(material.kind, "standard");
  assert.equal(material.roughness, 0.4);
  assert.equal(material.metalness, 0.9);
  assert.equal(material.clearcoat, undefined);
  assert.equal(material.sheen, undefined);
  assert.equal(material.unlit, undefined);
});

test("glTF texture slots carry explicit color roles and transfer functions", () => {
  const { context } = createLoaderContext();
  const material = extractTexturedMaterial(context);

  assert.equal(material.texture, "base.png");
  assert.equal(material.normalMap, "normal.png");
  assert.equal(material.roughnessMap, "metal-rough.png");
  assert.equal(material.metalnessMap, "metal-rough.png");
  assert.equal(material.occlusionMap, "ao.png");
  assert.equal(material.emissiveMap, "emissive.png");

  assert.deepEqual(material.textureDescriptors, {
    baseColor: { uri: "base.png", role: "base-color", colorSpace: "srgb", channels: "rgba", view: "2d" },
    normal: { uri: "normal.png", role: "normal", colorSpace: "linear", channels: "rgb", view: "2d" },
    roughness: { uri: "metal-rough.png", role: "roughness", colorSpace: "linear", channels: "g", view: "2d" },
    metalness: { uri: "metal-rough.png", role: "metalness", colorSpace: "linear", channels: "b", view: "2d" },
    occlusion: { uri: "ao.png", role: "ambient-occlusion", colorSpace: "linear", channels: "r", view: "2d" },
    emissive: { uri: "emissive.png", role: "emissive", colorSpace: "srgb", channels: "rgb", view: "2d" },
  });
});

test("KHR_materials_specular textures resolve through the shared descriptor path", () => {
  const { context } = createLoaderContext();
  const material = extractSpecularTexturedMaterial(context, "spec.png");

  // Factors survive beside the textures.
  assert.equal(material.specularIntensity, 0.5);
  assert.deepEqual(material.specularColor, [1, 1, 1]);
  assert.equal(material.roughness, 0.4);

  // Intensity is the linear alpha mask; the colour is the sRGB F0 tint.
  assert.deepEqual(material.textureDescriptors.specularIntensity, {
    uri: "spec.png", role: "specular-intensity", colorSpace: "linear", channels: "a", view: "2d",
  });
  assert.deepEqual(material.textureDescriptors.specularColor, {
    uri: "spec.png", role: "specular-color", colorSpace: "srgb", channels: "rgb", view: "2d",
  });

  // The SAME source URI must produce two distinct roles, never a merged slot.
  assert.notEqual(
    material.textureDescriptors.specularIntensity.role,
    material.textureDescriptors.specularColor.role,
  );
  assert.equal(material.textureDescriptors.specularIntensity.uri, material.textureDescriptors.specularColor.uri);

  // Standard slots stay untouched by the specular additions.
  assert.equal("baseColor" in material.textureDescriptors, false);
});

test("KHR_materials_specular resolves data URI strings", () => {
  const { context } = createLoaderContext();
  const material = extractSpecularTexturedMaterial(context, "data:image/png;base64,iVBORw0KGgo=");
  assert.equal(material.textureDescriptors.specularIntensity.uri, "data:image/png;base64,iVBORw0KGgo=");
  assert.equal(material.textureDescriptors.specularColor.uri, "data:image/png;base64,iVBORw0KGgo=");
  assert.equal(material.textureDescriptors.specularIntensity.role, "specular-intensity");
  assert.equal(material.textureDescriptors.specularColor.role, "specular-color");
});

test("KHR_materials_specular resolves bufferView-embedded images from one ArrayBuffer", () => {
  const { context, sandbox } = createLoaderContext();

  // Sentinel bytes surround the payload; the bufferView starts at a nonzero
  // offset so the extracted bytes must be an exact slice, not the whole
  // buffer. No image decoder runs here: this is URI-resolution coverage.
  const payload = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a];
  const bytes = new Uint8Array(7 + payload.length + 5);
  bytes.fill(0xee, 0, 7);
  bytes.set(payload, 7);
  bytes.fill(0xdd, 7 + payload.length);
  const buffer = bytes.buffer;

  const doc = materialDoc({
    extensions: {
      KHR_materials_specular: {
        specularFactor: 0.5,
        specularTexture: { index: 0 },
        specularColorTexture: { index: 1 },
      },
    },
  });
  doc.buffers = [{ byteLength: buffer.byteLength }];
  doc.bufferViews = [{ buffer: 0, byteOffset: 7, byteLength: payload.length }];
  doc.images = [
    { bufferView: 0, mimeType: "image/png" },
    { bufferView: 0, mimeType: "image/png" },
  ];
  doc.textures = [{ source: 0 }, { source: 1 }];

  // A Node-local variable is invisible inside vm.runInContext: bind the real
  // ArrayBuffer into the sandbox explicitly before the call.
  sandbox.__embeddedSpecBuffer = buffer;

  // Observe the Blob objects handed to URL.createObjectURL; never treat the
  // stub's return value alone as the assertion.
  const blobs = [];
  sandbox.URL.createObjectURL = (blob) => {
    blobs.push(blob);
    return "blob:fake-" + blobs.length;
  };

  const material = plain(call(context,
    `gltfExtractMaterial(${JSON.stringify(doc)}, 0, __embeddedSpecBuffer)`));

  // Both slots resolve to their own blob URL with distinct roles over the
  // same embedded bytes.
  assert.equal(material.textureDescriptors.specularIntensity.uri, "blob:fake-1");
  assert.equal(material.textureDescriptors.specularColor.uri, "blob:fake-2");
  assert.equal(material.textureDescriptors.specularIntensity.role, "specular-intensity");
  assert.equal(material.textureDescriptors.specularColor.role, "specular-color");
  assert.equal(material.textureDescriptors.specularIntensity.colorSpace, "linear");
  assert.equal(material.textureDescriptors.specularColor.colorSpace, "srgb");
  assert.equal(material.textureDescriptors.specularIntensity.channels, "a");
  assert.equal(material.textureDescriptors.specularColor.channels, "rgb");
  assert.equal(material.specularIntensity, 0.5);

  // Each captured Blob carries the expected MIME type and exactly the sliced
  // payload bytes, excluding the surrounding sentinels.
  assert.equal(blobs.length, 2);
  for (const blob of blobs) {
    assert.equal(blob.type, "image/png");
    assert.equal(blob.parts.length, 1);
    assert.deepEqual(Array.from(new Uint8Array(blob.parts[0])), payload);
  }
});

test("KHR_materials_specular keeps texture slots absent when references do not resolve", () => {
  const { context } = createLoaderContext();

  // No extension at all: neither slot nor factor field appears.
  const bare = extractMaterial(context, { pbrMetallicRoughness: {} });
  assert.equal(bare.textureDescriptors, undefined);
  assert.equal("specularIntensity" in bare, false);

  // Extension present but without texture references: factors only.
  const factorsOnly = extractMaterial(context, {
    extensions: { KHR_materials_specular: { specularFactor: 0.25 } },
  });
  assert.equal(factorsOnly.specularIntensity, 0.25);
  assert.equal(factorsOnly.textureDescriptors, undefined);

  // Out-of-range texture indices resolve to nothing and leave no slot.
  const badIndex = extractMaterial(context, {
    extensions: {
      KHR_materials_specular: {
        specularTexture: { index: 9 },
        specularColorTexture: { index: 9 },
      },
    },
  });
  assert.equal(badIndex.textureDescriptors, undefined);

  // One broken reference must not suppress the other, resolvable slot.
  const doc = {
    asset: { version: "2.0" },
    images: [{ uri: "spec-color.png" }],
    textures: [{ source: 0 }],
    materials: [{
      extensions: {
        KHR_materials_specular: {
          specularTexture: { index: 5 },
          specularColorTexture: { index: 0 },
        },
      },
    }],
  };
  const partial = plain(call(context, `gltfExtractMaterial(${JSON.stringify(doc)}, 0, null)`));
  assert.equal("specularIntensity" in partial.textureDescriptors, false);
  assert.equal(partial.textureDescriptors.specularColor.uri, "spec-color.png");
  assert.equal(partial.textureDescriptors.specularColor.role, "specular-color");
});

test("KHR_materials_clearcoat maps clearcoatFactor onto clearcoat", () => {
  const { context } = createLoaderContext();
  const material = extractMaterial(context, {
    extensions: { KHR_materials_clearcoat: { clearcoatFactor: 0.75, clearcoatRoughnessFactor: 0.2 } },
  });
  assert.equal(material.clearcoat, 0.75);
});

test("KHR_materials_clearcoat clamps an out-of-range factor to 0..1", () => {
  const { context } = createLoaderContext();
  assert.equal(extractMaterial(context, {
    extensions: { KHR_materials_clearcoat: { clearcoatFactor: 4 } },
  }).clearcoat, 1);
  assert.equal(extractMaterial(context, {
    extensions: { KHR_materials_clearcoat: { clearcoatFactor: -2 } },
  }).clearcoat, 0);
});

test("KHR_materials_sheen collapses sheenColorFactor to its peak", () => {
  const { context } = createLoaderContext();
  const material = extractMaterial(context, {
    extensions: { KHR_materials_sheen: { sheenColorFactor: [0.2, 0.6, 0.3], sheenRoughnessFactor: 0.5 } },
  });
  assert.equal(material.sheen, 0.6);
});

test("KHR_materials_transmission and KHR_materials_iridescence map straight through", () => {
  const { context } = createLoaderContext();
  const material = extractMaterial(context, {
    extensions: {
      KHR_materials_transmission: { transmissionFactor: 0.9 },
      KHR_materials_iridescence: { iridescenceFactor: 0.35 },
    },
  });
  assert.equal(material.transmission, 0.9);
  assert.equal(material.iridescence, 0.35);
});

test("KHR_materials_anisotropy projects rotation onto the signed tangent axis", () => {
  const { context } = createLoaderContext();
  // Rotation 0 points along the tangent, so the signed value stays positive.
  const tangent = extractMaterial(context, {
    extensions: { KHR_materials_anisotropy: { anisotropyStrength: 0.8, anisotropyRotation: 0 } },
  });
  assert.equal(tangent.anisotropy, 0.8);

  // Rotation pi/2 points along the bitangent, so the sign flips.
  const bitangent = extractMaterial(context, {
    extensions: { KHR_materials_anisotropy: { anisotropyStrength: 0.8, anisotropyRotation: Math.PI / 2 } },
  });
  assert.ok(Math.abs(bitangent.anisotropy + 0.8) < 1e-12, `expected -0.8, got ${bitangent.anisotropy}`);

  // Rotation pi/4 sits between both axes, so the projection reaches zero.
  const between = extractMaterial(context, {
    extensions: { KHR_materials_anisotropy: { anisotropyStrength: 0.8, anisotropyRotation: Math.PI / 4 } },
  });
  assert.ok(Math.abs(between.anisotropy) < 1e-12, `expected 0, got ${between.anisotropy}`);
});

test("KHR_materials_emissive_strength scales the emissive factor above 1", () => {
  const { context } = createLoaderContext();
  const base = extractMaterial(context, { emissiveFactor: [0.5, 0.25, 0] });
  assert.equal(base.emissive, 0.5);

  const boosted = extractMaterial(context, {
    emissiveFactor: [0.5, 0.25, 0],
    extensions: { KHR_materials_emissive_strength: { emissiveStrength: 6 } },
  });
  assert.equal(boosted.emissive, 3);
});

test("KHR_materials_ior records the index of refraction", () => {
  const { context } = createLoaderContext();
  assert.equal(extractMaterial(context, {
    extensions: { KHR_materials_ior: { ior: 1.33 } },
  }).ior, 1.33);
  // The extension defaults to 1.5 when it omits the value.
  assert.equal(extractMaterial(context, { extensions: { KHR_materials_ior: {} } }).ior, 1.5);
});

test("KHR_materials_unlit selects the flat shading path", () => {
  const { context } = createLoaderContext();
  assert.equal(extractMaterial(context, { extensions: { KHR_materials_unlit: {} } }).unlit, true);
});

test("KHR_materials_volume stays ignored while specular factors map", () => {
  // Volume still has no StandardMaterial field to map onto, so the loader
  // must leave thickness untouched rather than invent a value. Specular, in
  // contrast, now maps onto real fields: intensity 0.3 with the white
  // default colour.
  const { context } = createLoaderContext();
  const material = extractMaterial(context, {
    extensions: {
      KHR_materials_volume: { thicknessFactor: 0.5, attenuationDistance: 2 },
      KHR_materials_specular: { specularFactor: 0.3 },
    },
  });
  assert.equal(material.thickness, undefined);
  assert.equal(material.specularIntensity, 0.3);
  assert.deepEqual(material.specularColor, [1, 1, 1]);
});

// --- KHR_materials_specular factor contract --------------------------------

// Only the factors map in this slice: the specular texture inputs are a later
// one, so the extension deliberately stays off GLTF_SUPPORTED_EXTENSIONS.
// specularIntensity and specularColor are the existing StandardMaterial
// fields the browser pipeline already renders.
test("KHR_materials_specular omission leaves both factor fields unset", () => {
  const { context } = createLoaderContext();
  const bare = extractMaterial(context, { pbrMetallicRoughness: { roughnessFactor: 0.4 } });
  assert.equal("specularIntensity" in bare, false);
  assert.equal("specularColor" in bare, false);

  // Default materials — missing or out-of-range index — omit them as well.
  const fallback = plain(call(context, `gltfExtractMaterial({ asset: { version: "2.0" } }, 7, null)`));
  assert.equal("specularIntensity" in fallback, false);
  assert.equal("specularColor" in fallback, false);
});

test("KHR_materials_specular defaults the intensity to 1 and the colour to white", () => {
  const { context } = createLoaderContext();

  // An empty extension object takes both spec defaults.
  const empty = extractMaterial(context, { extensions: { KHR_materials_specular: {} } });
  assert.equal(empty.specularIntensity, 1);
  assert.deepEqual(empty.specularColor, [1, 1, 1]);

  // Each factor defaults independently while the other is present.
  const factorOnly = extractMaterial(context, {
    extensions: { KHR_materials_specular: { specularFactor: 0.4 } },
  });
  assert.equal(factorOnly.specularIntensity, 0.4);
  assert.deepEqual(factorOnly.specularColor, [1, 1, 1]);

  const colourOnly = extractMaterial(context, {
    extensions: { KHR_materials_specular: { specularColorFactor: [0.2, 0.4, 0.6] } },
  });
  assert.equal(colourOnly.specularIntensity, 1);
  assert.deepEqual(colourOnly.specularColor, [0.2, 0.4, 0.6]);
});

test("KHR_materials_specular preserves an explicit zero intensity and black colour", () => {
  const { context } = createLoaderContext();
  const matte = extractMaterial(context, {
    extensions: {
      KHR_materials_specular: { specularFactor: 0, specularColorFactor: [0, 0, 0] },
    },
  });
  assert.equal(matte.specularIntensity, 0);
  assert.deepEqual(matte.specularColor, [0, 0, 0]);
});

test("KHR_materials_specular clamps the intensity but never the colour", () => {
  const { context } = createLoaderContext();
  // The intensity saturates over 0..1; an explicit zero survives the clamp.
  assert.equal(extractMaterial(context, {
    extensions: { KHR_materials_specular: { specularFactor: 2 } },
  }).specularIntensity, 1);
  assert.equal(extractMaterial(context, {
    extensions: { KHR_materials_specular: { specularFactor: -0.5 } },
  }).specularIntensity, 0);

  // The colour is linear RGB with no colour-space conversion and no upper
  // clamp: HDR components above 1 pass through untouched.
  const hdr = extractMaterial(context, {
    extensions: { KHR_materials_specular: { specularColorFactor: [0.5, 1, 2.5] } },
  });
  assert.deepEqual(hdr.specularColor, [0.5, 1, 2.5]);
});

test("KHR_materials_specular never coerces malformed factors", () => {
  const { context } = createLoaderContext();
  // Strings, booleans, nulls, objects and arrays must not become numbers.
  for (const bad of ["0.5", true, false, null, { factor: 0.5 }, [0.5]]) {
    const material = extractMaterial(context, {
      extensions: { KHR_materials_specular: { specularFactor: bad } },
    });
    assert.equal(
      material.specularIntensity, 1,
      `specularFactor ${JSON.stringify(bad)} must default to 1, not coerce`,
    );
  }
});

test("KHR_materials_specular rejects malformed colour triples wholesale", () => {
  const { context } = createLoaderContext();
  const badTriples = [
    null,
    "0.5,0.5,0.5",
    { r: 1, g: 1, b: 1 },
    [0.5, 0.5],
    [0.5, 0.5, 0.5, 0.5],
    [0.5, "0.5", 0.5],
    [0.5, true, 0.5],
    [0.5, null, 0.5],
    [0.5, -0.25, 0.5],
  ];
  for (const triple of badTriples) {
    const material = extractMaterial(context, {
      extensions: { KHR_materials_specular: { specularColorFactor: triple } },
    });
    assert.deepEqual(
      material.specularColor, [1, 1, 1],
      `triple ${JSON.stringify(triple)} must default whole to white`,
    );
  }

  // A malformed colour must not disturb a valid intensity beside it.
  const mixed = extractMaterial(context, {
    extensions: {
      KHR_materials_specular: { specularFactor: 0.25, specularColorFactor: [1, 2, 3, 4] },
    },
  });
  assert.equal(mixed.specularIntensity, 0.25);
  assert.deepEqual(mixed.specularColor, [1, 1, 1]);
});

test("KHR_materials_specular treats non-finite inputs as malformed", () => {
  const { context } = createLoaderContext();
  // Infinity and NaN cannot travel through JSON; exercise them in the VM.
  const nonFinite = plain(call(context, `
    gltfExtractMaterial({
      asset: { version: "2.0" },
      materials: [{
        extensions: {
          KHR_materials_specular: {
            specularFactor: Infinity,
            specularColorFactor: [0.25, NaN, 4]
          }
        }
      }]
    }, 0, null)
  `));
  assert.equal(nonFinite.specularIntensity, 1);
  assert.deepEqual(nonFinite.specularColor, [1, 1, 1]);
});

test("KHR_materials_specular colour copies never alias", () => {
  const { context } = createLoaderContext();
  const copies = plain(call(context, `
    var doc = {
      asset: { version: "2.0" },
      materials: [{
        extensions: { KHR_materials_specular: { specularColorFactor: [0.25, 0.5, 0.75] } }
      }]
    };
    var first = gltfExtractMaterial(doc, 0, null);
    var second = gltfExtractMaterial(doc, 0, null);
    var source = doc.materials[0].extensions.KHR_materials_specular.specularColorFactor;
    first.specularColor[0] = 9;
    ({
      second0: second.specularColor[0],
      source0: source[0],
      distinctCopies: first.specularColor !== second.specularColor,
      copyNotView: first.specularColor !== source
    });
  `));
  assert.equal(copies.second0, 0.25, "a second extraction must not see the mutated copy");
  assert.equal(copies.source0, 0.25, "the input document must stay untouched");
  assert.equal(copies.distinctCopies, true);
  assert.equal(copies.copyNotView, true);

  // Even the [1, 1, 1] default must be a fresh array per material.
  const defaults = plain(call(context, `
    var doc = {
      asset: { version: "2.0" },
      materials: [
        { extensions: { KHR_materials_specular: {} } },
        { extensions: { KHR_materials_specular: {} } }
      ]
    };
    var left = gltfExtractMaterial(doc, 0, null);
    var right = gltfExtractMaterial(doc, 1, null);
    left.specularColor[1] = 7;
    ({ right1: right.specularColor[1], distinct: left.specularColor !== right.specularColor });
  `));
  assert.equal(defaults.right1, 1);
  assert.equal(defaults.distinct, true);
});

test("KHR_materials_specular keeps its fields beside a non-default KHR_materials_ior", () => {
  const { context } = createLoaderContext();
  const material = extractMaterial(context, {
    extensions: {
      KHR_materials_ior: { ior: 1.33 },
      KHR_materials_specular: { specularFactor: 0.4, specularColorFactor: [0.8, 0.6, 0.4] },
    },
  });
  assert.equal(material.ior, 1.33);
  assert.equal(material.specularIntensity, 0.4);
  assert.deepEqual(material.specularColor, [0.8, 0.6, 0.4]);
});

test("gltfExtractScene propagates specular factors from a triangle material", () => {
  const { context } = createLoaderContext();
  const result = plain(call(context, `
    // One indexed triangle with a specular material, the same shape as the
    // instancing fixtures.
    var buffer = new ArrayBuffer(4 * 9 + 8);
    var floats = new Float32Array(buffer, 0, 9);
    floats.set([0, 0, 0, 1, 0, 0, 0, 1, 0]);
    var u16 = new Uint16Array(buffer, 36, 4);
    u16.set([0, 1, 2, 0]);
    var doc = {
      accessors: [
        { bufferView: 0, byteOffset: 0, componentType: 5126, count: 3, type: "VEC3" },
        { bufferView: 0, byteOffset: 36, componentType: 5123, count: 3, type: "SCALAR" }
      ],
      bufferViews: [{ buffer: 0, byteOffset: 0, byteLength: buffer.byteLength }],
      materials: [{
        extensions: {
          KHR_materials_specular: { specularFactor: 0.25, specularColorFactor: [0.1, 0.2, 0.3] }
        }
      }],
      meshes: [{ name: "tri", primitives: [{ attributes: { POSITION: 0 }, indices: 1, material: 0, mode: 4 }] }],
      nodes: [{ mesh: 0 }],
      scenes: [{ nodes: [0] }]
    };
    var scene = gltfExtractScene(doc, buffer);
    ({
      objects: scene.objects.length,
      materials: scene.materials.length,
      intensity: scene.materials[0] ? scene.materials[0].specularIntensity : null,
      colour: scene.materials[0] ? scene.materials[0].specularColor : null
    });
  `));
  assert.equal(result.objects, 1);
  assert.equal(result.materials, 1);
  assert.equal(result.intensity, 0.25);
  assert.deepEqual(result.colour, [0.1, 0.2, 0.3]);
});

// --- KHR_texture_transform --------------------------------------------------

test("KHR_texture_transform builds the spec matrix and skips identity", () => {
  const { context } = createLoaderContext();
  assert.equal(call(context, `gltfTextureTransformMatrix({ index: 0 })`), null);
  assert.equal(
    call(context, `gltfTextureTransformMatrix({ index: 0, extensions: { KHR_texture_transform: { offset: [0, 0], scale: [1, 1], rotation: 0 } } })`),
    null,
  );
  const scaled = plain(call(
    context,
    `gltfTextureTransformMatrix({ index: 0, extensions: { KHR_texture_transform: { offset: [0.25, 0.5], scale: [2, 3] } } })`,
  ));
  assert.deepEqual(scaled, { m00: 2, m01: 0, m02: 0.25, m10: 0, m11: 3, m12: 0.5 });
});

test("KHR_texture_transform bakes offset and scale into the UV buffer", () => {
  const { context } = createLoaderContext();
  const result = plain(call(context, `
    var uvs = new Float32Array([0, 0, 1, 0, 0, 1]);
    var matrix = gltfTextureTransformMatrix({ extensions: { KHR_texture_transform: { offset: [0.1, 0.2], scale: [2, 4] } } });
    Array.from(gltfApplyTextureTransform(uvs, matrix));
  `));
  // u' = 2u + 0.1 and v' = 4v + 0.2.
  assert.equal(result.length, 6);
  assert.ok(Math.abs(result[0] - 0.1) < 1e-6);
  assert.ok(Math.abs(result[1] - 0.2) < 1e-6);
  assert.ok(Math.abs(result[2] - 2.1) < 1e-6);
  assert.ok(Math.abs(result[3] - 0.2) < 1e-6);
  assert.ok(Math.abs(result[4] - 0.1) < 1e-6);
  assert.ok(Math.abs(result[5] - 4.2) < 1e-6);
});

test("KHR_texture_transform rotation rotates UVs about the origin", () => {
  const { context } = createLoaderContext();
  const result = plain(call(context, `
    var uvs = new Float32Array([1, 0]);
    var matrix = gltfTextureTransformMatrix({ extensions: { KHR_texture_transform: { rotation: Math.PI / 2 } } });
    Array.from(gltfApplyTextureTransform(uvs, matrix));
  `));
  // A quarter turn maps (1, 0) to (0, -1) under the spec matrix.
  assert.ok(Math.abs(result[0]) < 1e-6, `u = ${result[0]}`);
  assert.ok(Math.abs(result[1] + 1) < 1e-6, `v = ${result[1]}`);
});

test("KHR_texture_transform on the base colour texture reaches the material", () => {
  const { context } = createLoaderContext();
  const material = extractMaterial(context, {
    pbrMetallicRoughness: {
      baseColorTexture: { index: 0, extensions: { KHR_texture_transform: { scale: [2, 2] } } },
    },
  });
  assert.deepEqual(material.uvTransform, { m00: 2, m01: 0, m02: 0, m10: 0, m11: 2, m12: 0 });
});

test("KHR_texture_transform never writes into the shared GLB buffer", () => {
  // A tightly packed accessor hands back a Float32Array view over the binary
  // chunk. Baking the transform must copy first, or a second primitive that
  // reads the same accessor would see transformed UVs.
  const { context } = createLoaderContext();
  const result = plain(call(context, `
    // 3 positions (9 floats) then 3 UVs (6 floats), tightly packed.
    var buffer = new ArrayBuffer(4 * 15);
    var floats = new Float32Array(buffer);
    floats.set([0, 0, 0, 1, 0, 0, 0, 1, 0], 0);
    floats.set([0, 0, 1, 0, 0, 1], 9);
    var doc = {
      accessors: [
        { bufferView: 0, byteOffset: 0, componentType: 5126, count: 3, type: "VEC3" },
        { bufferView: 1, byteOffset: 0, componentType: 5126, count: 3, type: "VEC2" }
      ],
      bufferViews: [
        { buffer: 0, byteOffset: 0, byteLength: 36 },
        { buffer: 0, byteOffset: 36, byteLength: 24 }
      ]
    };
    var primitive = { attributes: { POSITION: 0, TEXCOORD_0: 1 } };
    var matrix = gltfTextureTransformMatrix({ extensions: { KHR_texture_transform: { scale: [3, 3] } } });
    var first = gltfExtractMeshPrimitive(doc, primitive, buffer, matrix);
    var second = gltfExtractMeshPrimitive(doc, primitive, buffer, matrix);
    ({ first: Array.from(first.uvs), second: Array.from(second.uvs), source: Array.from(floats.subarray(9, 15)) });
  `));
  // Both reads see the same transformed UVs, and the source buffer is untouched.
  assert.deepEqual(result.first, [0, 0, 3, 0, 0, 3]);
  assert.deepEqual(result.second, [0, 0, 3, 0, 0, 3]);
  assert.deepEqual(result.source, [0, 0, 1, 0, 0, 1]);
});

// --- Compression extensions -------------------------------------------------

test("EXT_meshopt_compression raises a named error instead of reading garbage", () => {
  const { context } = createLoaderContext();
  assert.throws(
    () => call(context, `gltfRejectCompressedBufferView({ buffer: 0, byteOffset: 0, byteLength: 16, extensions: { EXT_meshopt_compression: { mode: "ATTRIBUTES" } } })`),
    /EXT_meshopt_compression/,
  );
  // A plain bufferView passes through.
  call(context, `gltfRejectCompressedBufferView({ buffer: 0, byteOffset: 0, byteLength: 16 })`);
});

test("KHR_draco_mesh_compression raises a named error", () => {
  const { context } = createLoaderContext();
  assert.throws(
    () => call(context, `gltfRejectCompressedPrimitive({ attributes: {}, extensions: { KHR_draco_mesh_compression: { bufferView: 0 } } })`),
    /KHR_draco_mesh_compression/,
  );
});

test("KHR_texture_basisu degrades to no texture and warns", () => {
  const { context, warnings } = createLoaderContext();
  const url = call(context, `
    gltfResolveTexture(
      { textures: [{ extensions: { KHR_texture_basisu: { source: 0 } } }], images: [{ uri: "wood.ktx2" }] },
      { index: 0 },
      null
    );
  `);
  assert.equal(url, "");
  assert.ok(
    warnings.some((line) => line.includes("KHR_texture_basisu")),
    `expected a basisu warning, got ${JSON.stringify(warnings)}`,
  );
});

test("extensionsRequired names every extension the loader ignores", () => {
  // EXT_mesh_gpu_instancing moved off this list once the loader learned to read
  // it. KHR_materials_variants stays: the loader still ignores it.
  const { context, warnings } = createLoaderContext();
  const missing = plain(call(context, `
    gltfReportUnsupportedRequiredExtensions({
      extensionsRequired: ["KHR_materials_clearcoat", "KHR_materials_variants", "KHR_xmp_json_ld"]
    });
  `));
  assert.deepEqual(missing, ["KHR_materials_variants", "KHR_xmp_json_ld"]);
  assert.ok(warnings.some((line) => line.includes("KHR_materials_variants")));
});

test("a document with no required extensions warns about nothing", () => {
  const { context, warnings } = createLoaderContext();
  const missing = plain(call(context, `gltfReportUnsupportedRequiredExtensions({ asset: { version: "2.0" } })`));
  assert.deepEqual(missing, []);
  assert.deepEqual(warnings, []);
});

// --- Animation channel widths ----------------------------------------------

test("gltfExtractAnimations records the true component count per channel", () => {
  const { context } = createLoaderContext();
  // Two keyframes. Translation carries 3 values each, rotation 4, and a morph
  // weights channel carries 5 (one per morph target).
  const animations = plain(call(context, `
    var buffer = new ArrayBuffer(4 * (2 + 6 + 8 + 10));
    var floats = new Float32Array(buffer);
    floats[0] = 0; floats[1] = 1;
    var doc = {
      accessors: [
        { bufferView: 0, componentType: 5126, count: 2, type: "SCALAR", byteOffset: 0 },
        { bufferView: 0, componentType: 5126, count: 2, type: "VEC3", byteOffset: 8 },
        { bufferView: 0, componentType: 5126, count: 2, type: "VEC4", byteOffset: 32 },
        { bufferView: 0, componentType: 5126, count: 10, type: "SCALAR", byteOffset: 64 }
      ],
      bufferViews: [{ buffer: 0, byteOffset: 0, byteLength: buffer.byteLength }],
      animations: [{
        name: "wave",
        samplers: [
          { input: 0, output: 1, interpolation: "LINEAR" },
          { input: 0, output: 2, interpolation: "LINEAR" },
          { input: 0, output: 3, interpolation: "LINEAR" }
        ],
        channels: [
          { sampler: 0, target: { node: 1, path: "translation" } },
          { sampler: 1, target: { node: 1, path: "rotation" } },
          { sampler: 2, target: { node: 2, path: "weights" } }
        ]
      }]
    };
    gltfExtractAnimations(doc, buffer).map(function(clip) {
      return { name: clip.name, widths: clip.channels.map(function(ch) { return ch.componentCount; }) };
    });
  `));
  assert.deepEqual(animations, [{ name: "wave", widths: [3, 4, 5] }]);
});

test('CUBICSPLINE weights channel yields componentCount 5, not 15', () => {
  const { context } = createLoaderContext();
  context.gltfReadAccessor = (gltfDocument, accessorIndex, binaryBuffer) => accessorIndex === 0 ? new Float32Array([0, 1]) : new Float32Array(30);
  const [anim] = call(context, `gltfExtractAnimations(${JSON.stringify({ animations: [{ samplers: [{ input: 0, output: 1, interpolation: 'CUBICSPLINE' }], channels: [{ sampler: 0, target: { node: 2, path: 'weights' } }] }] })})`);
  assert.equal(anim.channels[0].componentCount, 5);
});

// --- KHR_mesh_quantization --------------------------------------------------

// quantizedDoc builds a two-triangle quad whose positions are UNSIGNED_SHORT
// lattice coordinates, normals are normalized BYTE and UVs are normalized
// UNSIGNED_SHORT. A wrapper node carries the dequantization, which is exactly
// the layout the asset pipeline writes.
//
// The quad spans x and z from 0 to 2 in world units, so the lattice step is
// 2 / 65535 and the node scale is that step.
function quantizedDocSource() {
  return `
    var step = 2 / 65535;
    // Layout: 4 positions (VEC3 u16) at 0, 4 normals (VEC3 i8) at 24,
    // 4 UVs (VEC2 u16) at 36, 6 indices (u16) at 52.
    var buffer = new ArrayBuffer(64);
    var u16 = new Uint16Array(buffer);
    var i8 = new Int8Array(buffer);
    // Corner lattice coordinates for (0,0), (0,2), (2,0), (2,2) in x and z.
    u16.set([0, 0, 0,  0, 0, 65535,  65535, 0, 0,  65535, 0, 65535], 0);
    // Normals pointing straight up, stored as normalized bytes.
    for (var v = 0; v < 4; v++) {
      i8[24 + v * 3] = 0;
      i8[24 + v * 3 + 1] = 127;
      i8[24 + v * 3 + 2] = 0;
    }
    u16.set([0, 0,  0, 65535,  65535, 0,  65535, 65535], 18);
    u16.set([0, 1, 2, 2, 1, 3], 26);
    var doc = {
      accessors: [
        { bufferView: 0, byteOffset: 0, componentType: 5123, count: 4, type: "VEC3",
          min: [0, 0, 0], max: [65535, 0, 65535] },
        { bufferView: 0, byteOffset: 24, componentType: 5120, normalized: true, count: 4, type: "VEC3" },
        { bufferView: 0, byteOffset: 36, componentType: 5123, normalized: true, count: 4, type: "VEC2" },
        { bufferView: 0, byteOffset: 52, componentType: 5123, count: 6, type: "SCALAR" }
      ],
      bufferViews: [{ buffer: 0, byteOffset: 0, byteLength: 64 }],
      meshes: [{ name: "quad", primitives: [{
        attributes: { POSITION: 0, NORMAL: 1, TEXCOORD_0: 2 }, indices: 3, mode: 4
      }] }],
      nodes: [
        { name: "holder", children: [1] },
        { name: "gosx-dequantize", mesh: 0, translation: [0, 0, 0], scale: [step, step, step] }
      ],
      scenes: [{ nodes: [0] }],
      extensionsUsed: ["KHR_mesh_quantization"]
    };
  `;
}

test("KHR_mesh_quantization decodes positions through the node scale", () => {
  const { context, warnings } = createLoaderContext();
  const result = plain(call(context, quantizedDocSource() + `
    var scene = gltfExtractScene(doc, buffer);
    var object = scene.objects[0];
    ({
      count: object.vertices.count,
      positions: Array.from(object.vertices.positions),
      normals: Array.from(object.vertices.normals),
      uvs: Array.from(object.vertices.uvs),
      objects: scene.objects.length
    });
  `));

  assert.equal(result.objects, 1);
  assert.equal(result.count, 6);
  // The quad corners must land on the world coordinates the lattice stands for.
  const wanted = [
    [0, 0, 0], [0, 0, 2], [2, 0, 0],
    [2, 0, 0], [0, 0, 2], [2, 0, 2],
  ];
  for (let i = 0; i < wanted.length; i++) {
    for (let axis = 0; axis < 3; axis++) {
      const got = result.positions[i * 3 + axis];
      assert.ok(
        Math.abs(got - wanted[i][axis]) < 1e-4,
        `corner ${i} axis ${axis} decoded ${got}, want ${wanted[i][axis]}`,
      );
    }
  }
  // Normalized BYTE normals must decode to unit length on the y axis.
  for (let i = 0; i < result.count; i++) {
    assert.ok(Math.abs(result.normals[i * 3 + 1] - 1) < 1e-6, "normal y is not one");
    assert.ok(Math.abs(result.normals[i * 3]) < 1e-6, "normal x is not zero");
  }
  // Normalized UNSIGNED_SHORT UVs must decode to the unit square.
  assert.deepEqual(result.uvs.slice(0, 4), [0, 0, 0, 1]);
  assert.deepEqual(warnings, []);
});

test("KHR_mesh_quantization in extensionsRequired raises no warning", () => {
  const { context, warnings } = createLoaderContext();
  call(context, `
    gltfReportUnsupportedRequiredExtensions({
      extensionsRequired: ["KHR_mesh_quantization", "EXT_mesh_gpu_instancing"]
    });
  `);
  assert.deepEqual(warnings, [], "the loader reads both extensions, so it must not warn");
});

test("a normalized integer normal accessor comes back at unit length", () => {
  // A quantized normal decodes short of unit length. The loader must fix the
  // length, because the skinned path keeps the model-space vector.
  const { context } = createLoaderContext();
  const lengths = plain(call(context, `
    var buffer = new ArrayBuffer(16);
    var i8 = new Int8Array(buffer);
    // A diagonal direction. Each component rounds to 73 of 127, which leaves
    // the decoded vector about one percent short.
    i8[0] = 73; i8[1] = 73; i8[2] = 73;
    var doc = {
      accessors: [
        { bufferView: 0, byteOffset: 0, componentType: 5126, count: 1, type: "VEC3" },
        { bufferView: 0, byteOffset: 0, componentType: 5120, normalized: true, count: 1, type: "VEC3" }
      ],
      bufferViews: [{ buffer: 0, byteOffset: 0, byteLength: 16 }]
    };
    var raw = gltfReadAccessor(doc, 1, buffer);
    var rawLength = Math.sqrt(raw[0] * raw[0] + raw[1] * raw[1] + raw[2] * raw[2]);
    var fixed = gltfRenormalizeVec3(gltfReadAccessor(doc, 1, buffer));
    var fixedLength = Math.sqrt(fixed[0] * fixed[0] + fixed[1] * fixed[1] + fixed[2] * fixed[2]);
    ({ rawLength: rawLength, fixedLength: fixedLength });
  `));
  assert.ok(Math.abs(lengths.rawLength - 1) > 1e-4, "the fixture must actually be short");
  assert.ok(Math.abs(lengths.fixedLength - 1) < 1e-6, "the loader must restore unit length");
});

// --- EXT_mesh_gpu_instancing -----------------------------------------------

test("EXT_mesh_gpu_instancing draws one object per instance", () => {
  const { context } = createLoaderContext();
  const result = plain(call(context, `
    // 3 positions, then 3 instance translations, then 3 indices.
    var buffer = new ArrayBuffer(4 * 18 + 8);
    var floats = new Float32Array(buffer);
    floats.set([0, 0, 0, 1, 0, 0, 0, 1, 0], 0);
    floats.set([0, 0, 0, 10, 0, 0, 0, 0, 10], 9);
    var u16 = new Uint16Array(buffer, 4 * 18, 4);
    u16.set([0, 1, 2, 0]);
    var doc = {
      accessors: [
        { bufferView: 0, byteOffset: 0, componentType: 5126, count: 3, type: "VEC3" },
        { bufferView: 0, byteOffset: 36, componentType: 5126, count: 3, type: "VEC3" },
        { bufferView: 0, byteOffset: 72, componentType: 5123, count: 3, type: "SCALAR" }
      ],
      bufferViews: [{ buffer: 0, byteOffset: 0, byteLength: buffer.byteLength }],
      meshes: [{ name: "tri", primitives: [{ attributes: { POSITION: 0 }, indices: 2, mode: 4 }] }],
      nodes: [{
        mesh: 0,
        extensions: { EXT_mesh_gpu_instancing: { attributes: { TRANSLATION: 1 } } }
      }],
      scenes: [{ nodes: [0] }]
    };
    var scene = gltfExtractScene(doc, buffer);
    ({
      objects: scene.objects.length,
      ids: scene.objects.map(function(o) { return o.id; }),
      firstCorners: scene.objects.map(function(o) {
        return [o.vertices.positions[0], o.vertices.positions[1], o.vertices.positions[2]];
      })
    });
  `));
  assert.equal(result.objects, 3, "three instance translations must draw three objects");
  assert.deepEqual(result.ids, ["tri-prim-0-inst-0", "tri-prim-0-inst-1", "tri-prim-0-inst-2"]);
  assert.deepEqual(result.firstCorners, [[0, 0, 0], [10, 0, 0], [0, 0, 10]]);
});

test("a node without EXT_mesh_gpu_instancing keeps its single object id", () => {
  const { context } = createLoaderContext();
  const ids = plain(call(context, `
    var buffer = new ArrayBuffer(4 * 9 + 8);
    var floats = new Float32Array(buffer, 0, 9);
    floats.set([0, 0, 0, 1, 0, 0, 0, 1, 0]);
    var u16 = new Uint16Array(buffer, 36, 4);
    u16.set([0, 1, 2, 0]);
    var doc = {
      accessors: [
        { bufferView: 0, byteOffset: 0, componentType: 5126, count: 3, type: "VEC3" },
        { bufferView: 0, byteOffset: 36, componentType: 5123, count: 3, type: "SCALAR" }
      ],
      bufferViews: [{ buffer: 0, byteOffset: 0, byteLength: buffer.byteLength }],
      meshes: [{ name: "tri", primitives: [{ attributes: { POSITION: 0 }, indices: 1, mode: 4 }] }],
      nodes: [{ mesh: 0 }],
      scenes: [{ nodes: [0] }]
    };
    gltfExtractScene(doc, buffer).objects.map(function(o) { return o.id; });
  `));
  assert.deepEqual(ids, ["tri-prim-0"]);
});
