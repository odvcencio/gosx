// Texture variant selection tests for 19-scene-gltf.js.
//
// gltfResolveExternalImageURIs resolves each glTF image URI and then swaps it
// for the best built variant the live device can upload. The absolute rule is
// the one the Go side already holds: a variant that does not exist, or that the
// device cannot upload, must never be selected. A selector that breaks it turns
// a working texture into a 404 or a decode failure.
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

// loadLoader evaluates the glTF fragment in this realm with the three seams the
// swap reads: the manifest, the device token reader and the upload gate.
function loadLoader(options) {
  const opts = options || {};
  const window = {
    __gosx_scene3d_texture_tokens: () => opts.tokens || [],
  };
  if (opts.uploadReady !== false) {
    window.__gosx_scene3d_ktx2 = { uploadPathReady: () => true };
  } else if (opts.uploadReady === false && opts.ktx2Present) {
    window.__gosx_scene3d_ktx2 = { uploadPathReady: () => false };
  }
  if (opts.tokensMissing) {
    delete window.__gosx_scene3d_texture_tokens;
  }
  const manifest = opts.manifest === undefined
    ? { textureVariants: { "assets/wood.png": woodVariants() } }
    : opts.manifest;
  const console = { warn: () => {}, error: () => {}, log: () => {} };
  const factory = new Function(
    "window", "loadManifest", "console", "URL",
    gltfSource + "\nreturn { gltfResolveExternalImageURIs: gltfResolveExternalImageURIs };"
  );
  return factory(window, () => manifest, console, URL);
}

// resolve runs the loop over one glTF document and returns the image URIs.
function resolve(loader, uris) {
  const doc = { images: uris.map((uri) => ({ uri })) };
  loader.gltfResolveExternalImageURIs(doc, BASE);
  return doc.images.map((image) => image.uri);
}

// A device that proved the BC family, a high delivery budget and the container.
const BC_TOKENS = [
  "backend:webgpu",
  "budget:high",
  "container:ktx2",
  "container:ktx2-zlib",
  "device-feature:texture-compression-bc",
  "texture-format:bc7-rgba-unorm-srgb",
  "texture-format:rgba8unorm-srgb",
];

// The same device without any block feature.
const PLAIN_TOKENS = [
  "backend:webgpu",
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

test("the swap stays off until a renderer registers a KTX2 upload path", () => {
  const loader = loadLoader({ tokens: BC_TOKENS, uploadReady: false, ktx2Present: true });
  const [uri] = resolve(loader, ["../assets/wood.png"]);
  assert.equal(
    uri,
    "https://example.test/assets/wood.png",
    "a URI nothing can decode is worse than the authored source"
  );
});

test("no KTX2 module at all keeps the authored URI", () => {
  const loader = loadLoader({ tokens: BC_TOKENS, uploadReady: false });
  const [uri] = resolve(loader, ["../assets/wood.png"]);
  assert.equal(uri, "https://example.test/assets/wood.png");
});

test("no token reader keeps the authored URI", () => {
  const loader = loadLoader({ tokens: BC_TOKENS, tokensMissing: true });
  const [uri] = resolve(loader, ["../assets/wood.png"]);
  assert.equal(uri, "https://example.test/assets/wood.png");
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
