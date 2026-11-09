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



function lightDoc() {
  return {
    asset: { version: "2.0" },
    extensionsUsed: ["KHR_lights_punctual"],
    extensions: {
      KHR_lights_punctual: {
        lights: [
          { name: "sun", type: "directional", color: [1, 0.5, 0], intensity: 2049 },
          { name: "lamp", type: "point", intensity: 683, range: 12, extras: { gosx: { intensityScale: 2 } } },
          { name: "beam", type: "spot", intensity: 1366, spot: { innerConeAngle: 0.2, outerConeAngle: 0.8 } },
          { name: "open", type: "point" },
        ],
      },
    },
    scene: 0,
    scenes: [{ nodes: [0, 1, 2, 3] }],
    nodes: [
      // -90 degrees about X turns the light's -Z axis to world -Y.
      { rotation: [-Math.SQRT1_2, 0, 0, Math.SQRT1_2], extensions: { KHR_lights_punctual: { light: 0 } } },
      { translation: [1, 2, 3], extensions: { KHR_lights_punctual: { light: 1 } } },
      { translation: [0, 5, 0], children: [4], extensions: {} },
      { translation: [-1, 0, 0], extensions: { KHR_lights_punctual: { light: 3 } } },
      { translation: [0, 0, 2], extensions: { KHR_lights_punctual: { light: 2 } } },
    ],
  };
}

function extractLights(doc) {
  const context = createLoaderContext();
  context.gltfDoc = doc;
  const lights = vm.runInContext("gltfExtractScene(gltfDoc, new ArrayBuffer(4)).lights", context);
  return Array.from(lights, (l) => JSON.parse(JSON.stringify(l)));
}

const near = (actual, expected) => assert.ok(Math.abs(actual - expected) < 1e-5, actual + " != " + expected);

test("KHR_lights_punctual imports directional, point and spot lights", () => {
  const lights = extractLights(lightDoc());
  const byName = Object.fromEntries(lights.map((l) => [l.id.split("-")[2], l]));
  assert.equal(lights.length, 4);

  const sun = byName.sun;
  assert.equal(sun.kind, "directional");
  assert.equal(sun.color, "#ff8000");
  near(sun.intensity, 3); // 2049 lux / 683 lm per W
  near(sun.directionX, 0); near(sun.directionY, -1); near(sun.directionZ, 0);

  const lamp = byName.lamp;
  assert.equal(lamp.kind, "point");
  assert.deepEqual([lamp.x, lamp.y, lamp.z], [1, 2, 3]);
  assert.equal(lamp.range, 12);
  near(lamp.intensity, 2); // 683 cd -> 1, times intensityScale 2
  assert.equal(lamp.decay, 2);

  const open = byName.open;
  assert.equal(open.range, 0, "absent range means unbounded");
  near(open.intensity, 1 / 683); // glTF's default intensity of 1
});

test("KHR_lights_punctual spot cone maps to angle and penumbra and follows the parent transform", () => {
  const beam = extractLights(lightDoc()).find((l) => l.kind === "spot");
  assert.deepEqual([beam.x, beam.y, beam.z], [0, 5, 2]);
  near(beam.directionX, 0); near(beam.directionY, 0); near(beam.directionZ, -1);
  near(beam.angle, 0.8);
  near(beam.intensity, 2);
  near(beam.penumbra, 1 - 0.2 / 0.8);
});

test("KHR_lights_punctual defaults the spot cone to pi/4 outer and 0 inner", () => {
  const doc = lightDoc();
  doc.extensions.KHR_lights_punctual.lights[2].spot = undefined;
  const beam = extractLights(doc).find((l) => l.kind === "spot");
  near(beam.angle, Math.PI / 4);
  near(beam.penumbra, 1);
});

test("a node that references a missing light is skipped", () => {
  const doc = lightDoc();
  doc.nodes[0].extensions.KHR_lights_punctual.light = 99;
  assert.equal(extractLights(doc).length, 3);
});
