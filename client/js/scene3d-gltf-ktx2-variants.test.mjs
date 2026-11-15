// End to end: a GLB that `gosx assets build` wrote (testdata/blender-ktx2,
// regenerated with `go test ./assetpipe -run KTX2 -update-ktx2-fixture`) goes
// through the real loader. A device that proved BC support receives the KTX2
// file the build wrote; every other device keeps the embedded original image.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const ts = require("../runtime/node_modules/typescript");
const sceneMathSource = fs.readFileSync(path.join(__dirname, "bootstrap-src", "11-scene-math.ts"), "utf8");
const gltfSource = ts.transpileModule(fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "gltf.ts"), "utf8"), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText;
const fixtureDir = path.join(__dirname, "testdata", "blender-ktx2", "props");

const BC_TOKENS = [
  "backend:webgpu", "budget:high", "container:ktx2", "container:ktx2-zlib",
  "device-feature:texture-compression-bc", "texture-format:bc7-rgba-unorm-srgb",
  "texture-format:rgba8unorm-srgb", "texture-format:rgb8unorm-srgb",
];
const PLAIN_TOKENS = ["backend:webgl", "budget:high", "container:ktx2", "texture-format:rgba8unorm-srgb"];

function loader() {
  const window = { location: { href: "https://example.test/" } };
  const fetch = async (url) => {
    const file = path.join(fixtureDir, path.basename(new URL(url).pathname));
    const bytes = fs.readFileSync(file);
    return { ok: true, status: 200, arrayBuffer: async () => bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) };
  };
  const console = { warn: () => {}, error: () => {}, log: () => {} };
  const factory = new Function("window", "loadManifest", "console", "URL", "fetch", "Blob",
    sceneMathSource + "\n" + gltfSource + "\nreturn { sceneLoadGLTFModel };");
  class FakeBlob { constructor(parts, opts) { this.parts = parts; this.type = opts && opts.type; } }
  const urlWithBlob = new Proxy(URL, {});
  urlWithBlob.createObjectURL = () => "blob:embedded-original";
  return factory(window, () => null, console, urlWithBlob, fetch, FakeBlob);
}

function baseColorTexture(scene) {
  return scene.materials.concat(scene.objects.map((o) => o.material)).map((m) => m && m.texture).find(Boolean);
}

for (const name of ["crate.glb", "crate.lod1.glb"]) {
  test(name + ": a BC device loads the KTX2 file the build wrote", async () => {
    const scene = await loader().sceneLoadGLTFModel("https://example.test/props/" + name, {
      backend: "webgpu", uploadReady: true, tokens: BC_TOKENS,
    });
    const url = baseColorTexture(scene);
    assert.match(url, /^https:\/\/example\.test\/props\/crate\.textures\/albedo-0\.high\.bc7-rgba-unorm-srgb\.ktx2$/);
    assert.ok(fs.existsSync(path.join(fixtureDir, "crate.textures", path.basename(url))), "the referenced KTX2 file exists");
  });

  test(name + ": a device without BC keeps the embedded original image", async () => {
    const scene = await loader().sceneLoadGLTFModel("https://example.test/props/" + name, {
      backend: "webgl", uploadReady: true, tokens: PLAIN_TOKENS,
    });
    assert.equal(baseColorTexture(scene), "blob:embedded-original");
  });

  test(name + ": no renderer context keeps the embedded original image", async () => {
    const scene = await loader().sceneLoadGLTFModel("https://example.test/props/" + name);
    assert.equal(baseColorTexture(scene), "blob:embedded-original");
  });
}
