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

test("KHR_materials_volume and KHR_materials_specular are ignored, not guessed", () => {
  // Neither extension maps onto an existing StandardMaterial field, so the
  // loader must leave the material untouched rather than invent a value.
  const { context } = createLoaderContext();
  const material = extractMaterial(context, {
    extensions: {
      KHR_materials_volume: { thicknessFactor: 0.5, attenuationDistance: 2 },
      KHR_materials_specular: { specularFactor: 0.3 },
    },
  });
  assert.equal(material.thickness, undefined);
  assert.equal(material.specular, undefined);
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
