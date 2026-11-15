"use strict";
// Cascaded shadow bias: a light's ShadowBias is tuned for the one-map fit, so
// each cascade scales it by its texel size and depth range. Without the scale a
// fine near cascade inherits a bias several of its texels deep, and a negative
// bias darkens lit surfaces in a band (the Blackglass Coast beacon tower).
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const ts = require("../runtime/node_modules/typescript");
const { createContext, FakeElement, FakeWebGLContext, runScript, bootstrapSource, flushAsyncWork } = require("./runtime-test-harness.js");

function loadBiasScale() {
  const source = fs.readFileSync(path.join(__dirname, "bootstrap-src", "16c-scene-shared-pbr.ts"), "utf8");
  const start = source.indexOf("function sceneShadowCascadeBiasScale(");
  const end = source.indexOf("\n  }\n", start) + 4;
  const context = {};
  vm.createContext(context);
  vm.runInContext(ts.transpileModule(source.slice(start, end), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText + "\nglobalThis.scale = sceneShadowCascadeBiasScale;", context);
  return context.scale;
}

// An orthographic GL clip matrix with the given half width and depth range.
function ortho(half, depth) {
  return new Float32Array([1 / half, 0, 0, 0, 0, 1 / half, 0, 0, 0, 0, -2 / depth, 0, 0, 0, -1, 1]);
}

test("the cascade bias scale follows texel size and depth range", () => {
  const scale = loadBiasScale();
  assert.equal(scale(ortho(100, 400), ortho(100, 400)), 1);
  assert.ok(Math.abs(scale(ortho(100, 400), ortho(5, 400)) - 0.05) < 1e-6, "a 20x finer cascade gets a 20x smaller bias");
  assert.ok(Math.abs(scale(ortho(100, 400), ortho(5, 200)) - 0.1) < 1e-6, "a shorter depth range needs more depth for the same world offset");
  assert.equal(scale(ortho(100, 400), ortho(1000, 400)), 4, "the scale is clamped");
  assert.equal(scale(ortho(100, 400), new Float32Array(16)), 1, "a degenerate matrix keeps the authored bias");
});

test("the WebGL renderer uploads one scaled bias per cascade", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-cascade-bias-root";
  const env = createContext({
    elements: [mount], enableWebGL2: true, disableCanvas2D: true,
    manifest: { engines: [{ id: "gosx-engine-cascade-bias", component: "GoSXScene3D", kind: "surface", mountId: mount.id, props: {
      width: 640, height: 360, camera: { x: 0, y: 2, z: 6, near: 0.1, far: 100, fov: 60 },
      scene: {
        lights: [{ id: "sun", kind: "directional", castShadow: true, shadowCascades: 4, shadowSize: 256, shadowBias: -0.002, directionX: 0.2, directionY: -1, directionZ: -0.35 }],
        objects: [{ id: "ground", kind: "gltf-mesh", materialKind: "pbr", castShadow: true, receiveShadow: true,
          vertices: { count: 3, positions: [-100, 0, -100, 100, 0, -100, 0, 0, 100], normals: [0, 1, 0, 0, 1, 0, 0, 1, 0], uvs: [0, 0, 1, 0, 0.5, 1] } }],
      },
    } }] },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();
  const gl = mount.children[0].getContext("webgl2");
  const upload = gl.ops.filter((op) => op[0] === "uniform1fv" && op[1] === "u_shadowBias0").at(-1);
  assert.ok(upload, "the bias uploads as a per-cascade array");
  const bias = upload[3];
  assert.equal(upload[2], 4);
  assert.ok(bias.every((b) => b < 0), `the authored sign is kept: ${bias}`);
  assert.ok(Math.abs(bias[0]) < Math.abs(bias[1]) && Math.abs(bias[1]) < Math.abs(bias[2]) && Math.abs(bias[2]) < Math.abs(bias[3]), `finer cascades get less bias: ${bias}`);
  assert.ok(Math.abs(bias[0]) < 0.002 * 0.5, `the nearest cascade is far finer than the 140 m one-map fit: ${bias}`);
  assert.equal(env.consoleLogs.error.length, 0);
});
