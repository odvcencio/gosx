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

function createLoaderContext() {
  const sandbox = {
    console: { warn: () => {}, error: () => {}, log: () => {} },
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


function skinDoc(jointCount) {
  const nodes = [{ name: "root", children: [] }];
  const joints = [];
  for (let i = 0; i < jointCount; i++) {
    nodes.push({ name: "j" + i });
    joints.push(i + 1);
  }
  nodes[0].children = joints.slice(0, 1);
  return {
    asset: { version: "2.0" },
    scene: 0,
    scenes: [{ nodes: [0] }],
    nodes,
    skins: [{ name: "rig", joints }],
  };
}

function extractSkins(context, doc) {
  context.gltfDoc = doc;
  return vm.runInContext("gltfExtractScene(gltfDoc, new ArrayBuffer(4)).skins", context);
}

test("glTF skins up to 256 joints load with identity bind matrices", () => {
  const context = createLoaderContext();
  for (const count of [64, 65, 128, 256]) {
    const skins = extractSkins(context, skinDoc(count));
    assert.equal(skins[0].joints.length, count);
    assert.equal(skins[0].inverseBindMatrices.length, count * 16);
    assert.equal(skins[0].inverseBindMatrices[(count - 1) * 16 + 15], 1);
  }
});

test("glTF skins above 256 joints fail with a message naming the skin and the cap", () => {
  const context = createLoaderContext();
  assert.throws(
    () => extractSkins(context, skinDoc(257)),
    (error) => /"rig"/.test(error.message) && /257 joints/.test(error.message) && /maximum supported is 256/.test(error.message),
  );
});
