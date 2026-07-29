// Texture variant selection tests for 19-scene-gltf.js.
//
// gltfResolveExternalImageURIs resolves each glTF image URI and then swaps it
// for the best built variant the selected mount renderer can upload. The
// absolute rule is the one the Go side already holds: a variant that does not
// exist, or that this renderer cannot upload, must never be selected. A
// selector that breaks it turns a working texture into a 404 or decode failure.
//
// The ranking mirrors SelectFromManifest in assetpipe/variantmanifest.go: higher
// tier first, then the smaller file, then the lower URI.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");
const sceneMathSource = fs.readFileSync(path.join(srcDir, "11-scene-math.js"), "utf8");
const gltfSource = fs.readFileSync(path.join(srcDir, "19-scene-gltf.js"), "utf8");

const BASE = "https://example.test/models/city.gltf";

// The manifest row a real build writes for one texture. The capability tokens
// are the ones textureVariantCapabilities in assetpipe/execute_texture.go emits.
function woodVariants() {
  return [
    {
      uri: "/assets/wood.bc7.ktx2",
      quality: "high",
      bytes: 42529,
      requiredCapabilities: [
        "container:ktx2",
        "container:ktx2-zlib",
        "texture-format:bc7-rgba-unorm-srgb",
        "device-feature:texture-compression-bc",
        "budget:high",
      ],
    },
    {
      uri: "/assets/wood.rgba8.ktx2",
      quality: "standard",
      bytes: 502374,
      requiredCapabilities: ["container:ktx2", "texture-format:rgba8unorm-srgb"],
    },
  ];
}

// loadLoader evaluates the glTF fragment in this realm. Page globals are
// intentionally configurable poison: mounted selection must use only the
// explicit renderer context passed to the loader.
function loadLoader(options) {
  const opts = options || {};
  const window = {
    location: { href: "https://example.test/" },
    __gosx_scene3d_texture_tokens: () => opts.globalTokens || [],
    __gosx_scene3d_ktx2: {
      uploadPathReady: () => opts.globalUploadReady !== false,
    },
  };
  const manifest = opts.manifest === undefined
    ? { textureVariants: { "assets/wood.png": woodVariants() } }
    : opts.manifest;
  const console = { warn: () => {}, error: () => {}, log: () => {} };
  const factory = new Function(
    "window", "loadManifest", "console", "URL", "fetch",
    sceneMathSource + "\n" + gltfSource +
      "\nreturn { gltfResolveExternalImageURIs: gltfResolveExternalImageURIs, sceneLoadGLTFModel: sceneLoadGLTFModel };"
  );
  const api = factory(window, () => manifest, console, URL, opts.fetch || globalThis.fetch);
  api.context = opts.context === undefined
    ? {
      backend: opts.backend || "webgl",
      uploadReady: opts.uploadReady !== false,
      tokens: opts.tokens || [],
    }
    : opts.context;
  return api;
}

// resolve runs the loop over one glTF document and returns the image URIs.
function resolve(loader, uris, context = loader.context) {
  const doc = { images: uris.map((uri) => ({ uri })) };
  loader.gltfResolveExternalImageURIs(doc, BASE, context);
  return doc.images.map((image) => image.uri);
}

// A device that proved the BC family, a high delivery budget and the container.
const BC_TOKENS = [
  "backend:webgl",
  "budget:high",
  "container:ktx2",
  "container:ktx2-zlib",
  "device-feature:texture-compression-bc",
  "texture-format:bc7-rgba-unorm-srgb",
  "texture-format:rgba8unorm-srgb",
];

// The same device without any block feature.
const PLAIN_TOKENS = [
  "backend:webgl",
  "budget:high",
  "container:ktx2",
  "container:ktx2-zlib",
  "texture-format:rgba8unorm-srgb",
];

// --- the swap ----------------------------------------------------------------

test("a BC-capable device receives the block variant", () => {
  const loader = loadLoader({ tokens: BC_TOKENS });
  const [uri] = resolve(loader, ["../assets/wood.png"]);
  assert.equal(uri, "https://example.test/assets/wood.bc7.ktx2");
});

test("a device with no block feature keeps the authored URI", () => {
  const loader = loadLoader({ tokens: PLAIN_TOKENS });
  const [uri] = resolve(loader, ["../assets/wood.png"]);
  // The rgba8 variant is eligible on tokens alone, and it is still the wrong
  // answer: an image element already loads the source, and a KTX2 container is
  // not an image. Only the block saving justifies a swap.
  assert.equal(uri, "https://example.test/assets/wood.png");
});

test("an uncompressed variant never replaces the authored image", () => {
  // A table that holds nothing but uncompressed encodings must change nothing,
  // whatever the device proved.
  const manifest = {
    textureVariants: {
      "assets/wood.png": [woodVariants()[1]],
    },
  };
  const loader = loadLoader({ tokens: BC_TOKENS, manifest });
  assert.equal(resolve(loader, ["../assets/wood.png"])[0], "https://example.test/assets/wood.png");
});

test("a device that proves nothing keeps the authored URI", () => {
  const loader = loadLoader({ tokens: [] });
  const [uri] = resolve(loader, ["../assets/wood.png"]);
  assert.equal(uri, "https://example.test/assets/wood.png");
});

test("the swap stays off when the selected renderer has no KTX2 upload path", () => {
  const loader = loadLoader({ tokens: BC_TOKENS, uploadReady: false });
  const [uri] = resolve(loader, ["../assets/wood.png"]);
  assert.equal(
    uri,
    "https://example.test/assets/wood.png",
    "a URI nothing can decode is worse than the authored source"
  );
});

test("an empty explicit token snapshot keeps the authored URI", () => {
  const loader = loadLoader({ tokens: [] });
  const [uri] = resolve(loader, ["../assets/wood.png"]);
  assert.equal(uri, "https://example.test/assets/wood.png");
});

test("mounted selection ignores page-global backend identity and readiness", () => {
  const loader = loadLoader({
    tokens: BC_TOKENS,
    globalTokens: PLAIN_TOKENS,
    globalUploadReady: false,
  });
  assert.equal(
    resolve(loader, ["../assets/wood.png"])[0],
    "https://example.test/assets/wood.bc7.ktx2",
  );
});

test("a direct no-context load stays neutral despite capable page globals", () => {
  const loader = loadLoader({
    tokens: BC_TOKENS,
    globalTokens: BC_TOKENS,
    globalUploadReady: true,
  });
  assert.equal(
    resolve(loader, ["../assets/wood.png"], null)[0],
    "https://example.test/assets/wood.png",
  );
});

test("a manifest without a texture table keeps every authored URI", () => {
  const loader = loadLoader({ tokens: BC_TOKENS, manifest: { engines: [] } });
  assert.deepEqual(resolve(loader, ["../assets/wood.png"]), ["https://example.test/assets/wood.png"]);
  const none = loadLoader({ tokens: BC_TOKENS, manifest: null });
  assert.deepEqual(resolve(none, ["../assets/wood.png"]), ["https://example.test/assets/wood.png"]);
});

test("an image the table does not name keeps its authored URI", () => {
  const loader = loadLoader({ tokens: BC_TOKENS });
  const [uri] = resolve(loader, ["../assets/brick.png"]);
  assert.equal(uri, "https://example.test/assets/brick.png");
});

test("a data URI is never touched", () => {
  const loader = loadLoader({ tokens: BC_TOKENS });
  const inline = "data:image/png;base64,AAAA";
  assert.deepEqual(resolve(loader, [inline]), [inline]);
});

// --- the absolute rule -------------------------------------------------------

test("a variant the device cannot upload is never selected", () => {
  // The table holds a BC7 variant and an ASTC variant. The device proved BC
  // only, so the ASTC file must stay unreachable even though it is smaller.
  const manifest = {
    textureVariants: {
      "assets/wood.png": [
        {
          uri: "/assets/wood.astc.ktx2",
          quality: "high",
          bytes: 12000,
          requiredCapabilities: ["device-feature:texture-compression-astc", "texture-format:astc-4x4-unorm-srgb"],
        },
        ...woodVariants(),
      ],
    },
  };
  const loader = loadLoader({ tokens: BC_TOKENS, manifest });
  const [uri] = resolve(loader, ["../assets/wood.png"]);
  assert.equal(uri, "https://example.test/assets/wood.bc7.ktx2");
});

test("a variant that requires a budget the device did not claim is skipped", () => {
  // The same table read by a low-memory device. The only block variant gates on
  // budget:high, so nothing is eligible and the authored source stays.
  const tokens = PLAIN_TOKENS.filter((token) => token !== "budget:high").concat([
    "budget:low",
    "device-feature:texture-compression-bc",
    "texture-format:bc7-rgba-unorm-srgb",
  ]);
  const loader = loadLoader({ tokens });
  const [uri] = resolve(loader, ["../assets/wood.png"]);
  assert.equal(uri, "https://example.test/assets/wood.png");
});

// --- the ranking -------------------------------------------------------------

test("the ranking is tier, then bytes, then URI", () => {
  const bc = ["device-feature:texture-compression-bc"];
  const manifest = {
    textureVariants: {
      "assets/wood.png": [
        { uri: "/b.ktx2", quality: "standard", bytes: 100, requiredCapabilities: bc },
        { uri: "/a.ktx2", quality: "standard", bytes: 100, requiredCapabilities: bc },
        { uri: "/small.ktx2", quality: "standard", bytes: 50, requiredCapabilities: bc },
        { uri: "/high.ktx2", quality: "high", bytes: 9000, requiredCapabilities: bc.concat(["budget:high"]) },
      ],
    },
  };
  // A high budget takes the high tier even though it is by far the largest file.
  const high = loadLoader({ tokens: BC_TOKENS, manifest });
  assert.equal(resolve(high, ["../assets/wood.png"])[0], "https://example.test/high.ktx2");

  // Without budget:high the high tier is ineligible, so the smallest standard
  // file wins.
  const standard = loadLoader({ tokens: bc, manifest });
  assert.equal(resolve(standard, ["../assets/wood.png"])[0], "https://example.test/small.ktx2");

  // With the small file removed, the URI breaks the remaining tie.
  const tie = {
    textureVariants: {
      "assets/wood.png": manifest.textureVariants["assets/wood.png"].filter((v) => v.uri !== "/small.ktx2"),
    },
  };
  const tied = loadLoader({ tokens: bc, manifest: tie });
  assert.equal(resolve(tied, ["../assets/wood.png"])[0], "https://example.test/a.ktx2");
});

// --- table keys --------------------------------------------------------------

test("the table matches the authored path and the resolved path alike", () => {
  const byAuthored = loadLoader({
    tokens: BC_TOKENS,
    manifest: { textureVariants: { "../assets/wood.png": woodVariants() } },
  });
  assert.equal(resolve(byAuthored, ["../assets/wood.png"])[0], "https://example.test/assets/wood.bc7.ktx2");

  const bySlashedPath = loadLoader({
    tokens: BC_TOKENS,
    manifest: { textureVariants: { "/assets/wood.png": woodVariants() } },
  });
  assert.equal(resolve(bySlashedPath, ["../assets/wood.png"])[0], "https://example.test/assets/wood.bc7.ktx2");
});

test("a malformed variant never replaces a working URI", () => {
  const manifest = {
    textureVariants: {
      "assets/wood.png": [
        { quality: "high", bytes: 1, requiredCapabilities: ["device-feature:texture-compression-bc"] },
        { uri: "", quality: "high", bytes: 1, requiredCapabilities: ["device-feature:texture-compression-bc"] },
      ],
    },
  };
  const loader = loadLoader({ tokens: BC_TOKENS, manifest });
  assert.equal(resolve(loader, ["../assets/wood.png"])[0], "https://example.test/assets/wood.png");
});

function buildTexturedGLB() {
  const positions = new Float32Array([
    0, 0.75, 0,
    -0.65, -0.45, 0.3,
    0.7, -0.35, -0.2,
  ]);
  const normals = new Float32Array([
    0, 0, 1,
    0, 0, 1,
    0, 0, 1,
  ]);
  const indices = new Uint16Array([0, 1, 2]);
  const bin = Buffer.alloc(80);
  Buffer.from(positions.buffer).copy(bin, 0);
  Buffer.from(normals.buffer).copy(bin, 36);
  Buffer.from(indices.buffer).copy(bin, 72);
  const gltf = {
    asset: { version: "2.0", generator: "variant-test" },
    scene: 0,
    scenes: [{ nodes: [0] }],
    nodes: [{ mesh: 0 }],
    meshes: [{
      primitives: [{
        attributes: { POSITION: 0, NORMAL: 1 },
        indices: 2,
        material: 0,
      }],
    }],
    images: [{ uri: "../assets/wood.png" }],
    textures: [{ source: 0 }],
    materials: [{
      pbrMetallicRoughness: {
        baseColorTexture: { index: 0 },
      },
    }],
    accessors: [
      { bufferView: 0, componentType: 5126, count: 3, type: "VEC3" },
      { bufferView: 1, componentType: 5126, count: 3, type: "VEC3" },
      { bufferView: 2, componentType: 5123, count: 3, type: "SCALAR" },
    ],
    bufferViews: [
      { buffer: 0, byteOffset: 0, byteLength: 36, target: 34962 },
      { buffer: 0, byteOffset: 36, byteLength: 36, target: 34962 },
      { buffer: 0, byteOffset: 72, byteLength: 8, target: 34963 },
    ],
    buffers: [{ byteLength: 80 }],
  };
  let json = Buffer.from(JSON.stringify(gltf), "utf8");
  while (json.length % 4 !== 0) json = Buffer.concat([json, Buffer.from(" ")]);
  const totalLength = 12 + 8 + json.length + 8 + bin.length;
  const glb = Buffer.alloc(totalLength);
  let offset = 0;
  glb.writeUInt32LE(0x46546c67, offset); offset += 4;
  glb.writeUInt32LE(2, offset); offset += 4;
  glb.writeUInt32LE(totalLength, offset); offset += 4;
  glb.writeUInt32LE(json.length, offset); offset += 4;
  glb.writeUInt32LE(0x4E4F534A, offset); offset += 4;
  json.copy(glb, offset); offset += json.length;
  glb.writeUInt32LE(bin.length, offset); offset += 4;
  glb.writeUInt32LE(0x004E4942, offset); offset += 4;
  bin.copy(glb, offset);
  return glb;
}

test("GLB external image URIs use the explicit renderer context before extraction", async () => {
  const bytes = buildTexturedGLB();
  const loader = loadLoader({
    tokens: BC_TOKENS,
    fetch: async () => ({
      ok: true,
      status: 200,
      arrayBuffer: async () => bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength),
    }),
  });
  const scene = await loader.sceneLoadGLTFModel("/models/city.glb", Promise.resolve(loader.context));
  assert.equal(scene.objects.length, 1);
  assert.equal(scene.objects[0].material.texture, "https://example.test/assets/wood.bc7.ktx2");
  assert.deepEqual(scene.objects[0].material.textureDescriptors.baseColor, {
    uri: "https://example.test/assets/wood.bc7.ktx2",
    role: "base-color",
    colorSpace: "srgb",
    channels: "rgba",
    view: "2d",
  });
});

test("concurrent WebGL and WebGPU contexts resolve independently", async () => {
  const loader = loadLoader({
    tokens: BC_TOKENS,
    globalTokens: BC_TOKENS,
  });
  const webgl = {
    backend: "webgl",
    uploadReady: true,
    tokens: PLAIN_TOKENS,
  };
  const webgpu = {
    backend: "webgpu",
    uploadReady: true,
    tokens: BC_TOKENS.map((token) => token === "backend:webgl" ? "backend:webgpu" : token),
  };
  const [webglURI, webgpuURI] = await Promise.all([
    Promise.resolve().then(() => resolve(loader, ["../assets/wood.png"], webgl)[0]),
    Promise.resolve().then(() => resolve(loader, ["../assets/wood.png"], webgpu)[0]),
  ]);
  assert.equal(webglURI, "https://example.test/assets/wood.png");
  assert.equal(webgpuURI, "https://example.test/assets/wood.bc7.ktx2");
});

test("an early external-buffer failure stays handled while renderer context settles", async () => {
  let resolveContext;
  const delayedContext = new Promise((resolve) => {
    resolveContext = resolve;
  });
  const failure = new Error("early-buffer-failure");
  const loader = loadLoader({
    tokens: BC_TOKENS,
    fetch: async (url) => {
      if (url === "/models/delayed.gltf") {
        return {
          ok: true,
          status: 200,
          json: async () => ({
            asset: { version: "2.0" },
            buffers: [{ uri: "missing.bin", byteLength: 4 }],
            scenes: [{ nodes: [] }],
            scene: 0,
            nodes: [],
          }),
        };
      }
      throw failure;
    },
  });
  const unhandled = [];
  const onUnhandled = (error) => unhandled.push(error);
  process.on("unhandledRejection", onUnhandled);
  try {
    const loading = loader.sceneLoadGLTFModel("/models/delayed.gltf", delayedContext);
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(unhandled, [], "buffer rejection must be handled before context settlement");
    resolveContext(loader.context);
    await assert.rejects(loading, /early-buffer-failure/);
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(unhandled, [], "the later awaited throw must remain handled by the caller");
  } finally {
    process.removeListener("unhandledRejection", onUnhandled);
  }
});
