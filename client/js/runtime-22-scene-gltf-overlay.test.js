"use strict";
// Split scene loading: a small per-rotation overlay GLB names its immutable
// geometry base in root extras (gosx.baseSrc), and the loader patches the
// base's point entries with the overlay's colors and positions.
//
// The split exists so a scene whose point colors rotate on a schedule stops
// re-shipping its unchanged geometry: the base is content-addressed and cached
// forever, the overlay carries only what varies. These tests pin the merge
// contract — extras-id keying, node-transform application, the count-mismatch
// guard — and the quantized _POINT_SIZE decode that shrinks the base.

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  bootstrapSource,
  createContext,
  runScript,
} = require("./runtime-test-harness.js");

// Minimal GLB writer shared by the fixtures below. Layout mirrors
// buildPointLineGLBBytes in the harness.
function buildGLBBytes(gltf, binChunks) {
  const chunks = [];
  let byteOffset = 0;
  const bufferViews = [];
  for (const typed of binChunks) {
    const pad = (4 - (byteOffset % 4)) % 4;
    if (pad > 0) {
      chunks.push(Buffer.alloc(pad));
      byteOffset += pad;
    }
    const bytes = Buffer.from(typed.buffer, typed.byteOffset, typed.byteLength);
    bufferViews.push({ buffer: 0, byteOffset, byteLength: bytes.length, target: 34962 });
    chunks.push(bytes);
    byteOffset += bytes.length;
  }
  const bin = Buffer.concat(chunks);
  gltf.bufferViews = bufferViews;
  gltf.buffers = [{ byteLength: bin.length }];

  let json = Buffer.from(JSON.stringify(gltf), "utf8");
  while (json.length % 4 !== 0) {
    json = Buffer.concat([json, Buffer.from(" ")]);
  }
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
  return Array.from(glb);
}

// Base: two point meshes. "alpha" is keyed by an authored extras id (the way
// the galaxy layers are authored); "beta" falls back to meshName-points-0.
// Alpha carries reference colors that the overlay must replace; beta carries
// none, so its colors come only from the overlay.
function buildSplitBaseGLBBytes() {
  const alphaPositions = new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]);
  const alphaColors = new Uint8Array([10, 10, 10, 255, 10, 10, 10, 255, 10, 10, 10, 255]);
  const alphaSizes = new Float32Array([2, 3, 4]);
  const betaPositions = new Float32Array([5, 5, 5, 6, 5, 5]);
  const betaSizes = new Float32Array([7, 8]);
  return buildGLBBytes({
    asset: { version: "2.0", generator: "runtime-test-split-base" },
    scene: 0,
    scenes: [{ nodes: [0, 1] }],
    nodes: [
      { name: "alpha", mesh: 0 },
      { name: "beta", mesh: 1 },
    ],
    meshes: [
      {
        name: "alpha",
        primitives: [{
          mode: 0,
          attributes: { POSITION: 0, COLOR_0: 1, _POINT_SIZE: 2 },
          extras: { gosx: { id: "alpha-layer" } },
        }],
      },
      {
        name: "beta",
        primitives: [{
          mode: 0,
          attributes: { POSITION: 3, _POINT_SIZE: 4 },
        }],
      },
    ],
    accessors: [
      { bufferView: 0, componentType: 5126, count: 3, type: "VEC3" },
      { bufferView: 1, componentType: 5121, count: 3, type: "VEC4", normalized: true },
      { bufferView: 2, componentType: 5126, count: 3, type: "SCALAR" },
      { bufferView: 3, componentType: 5126, count: 2, type: "VEC3" },
      { bufferView: 4, componentType: 5126, count: 2, type: "SCALAR" },
    ],
  }, [alphaPositions, alphaColors, alphaSizes, betaPositions, betaSizes]);
}

// Overlay: alpha rotates colors only; beta rotates colors and positions, with
// a node translation the merge must apply. betaCount lets the mismatch test
// build a skewed overlay.
function buildSplitOverlayGLBBytes(options) {
  const betaCount = options && options.betaCount ? options.betaCount : 2;
  const alphaColors = new Uint8Array([255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255]);
  const betaColors = new Uint8Array(betaCount * 4).fill(128);
  const betaPositions = new Float32Array(betaCount * 3);
  for (let i = 0; i < betaCount; i++) {
    betaPositions[i * 3] = i;
  }
  return buildGLBBytes({
    asset: { version: "2.0", generator: "runtime-test-split-overlay" },
    extras: { gosx: { baseSrc: "/models/split-base.glb" } },
    scene: 0,
    scenes: [{ nodes: [0, 1] }],
    nodes: [
      { name: "alpha", mesh: 0 },
      { name: "beta", mesh: 1, translation: [100, 0, 0] },
    ],
    meshes: [
      {
        name: "alpha",
        primitives: [{
          mode: 0,
          attributes: { COLOR_0: 0 },
          extras: { gosx: { id: "alpha-layer" } },
        }],
      },
      {
        name: "beta",
        primitives: [{
          mode: 0,
          attributes: { POSITION: 1, COLOR_0: 2 },
        }],
      },
    ],
    accessors: [
      { bufferView: 0, componentType: 5121, count: 3, type: "VEC4", normalized: true },
      { bufferView: 1, componentType: 5126, count: betaCount, type: "VEC3" },
      { bufferView: 2, componentType: 5121, count: betaCount, type: "VEC4", normalized: true },
    ],
  }, [alphaColors, betaPositions, betaColors]);
}

test("GLB overlay with baseSrc merges rotating attributes onto the cached base", async () => {
  const env = createContext({
    fetchRoutes: {
      "/models/split-overlay.glb?bucket=1": { bytes: buildSplitOverlayGLBBytes() },
      "http://localhost:3000/models/split-base.glb": { bytes: buildSplitBaseGLBBytes() },
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  const scene = await env.context.__gosx_scene3d_gltf_api.sceneLoadGLTFModel("/models/split-overlay.glb?bucket=1");

  assert.equal(env.fetchCalls.some((call) => call.url === "http://localhost:3000/models/split-base.glb"), true);
  assert.equal(scene.points.length, 2);

  const alpha = scene.points.find((entry) => entry.id === "alpha-layer");
  assert.ok(alpha, "alpha entry keyed by authored extras id");
  // Geometry from the base, colors from the overlay.
  assert.equal(alpha.count, 3);
  assert.deepEqual(Array.from(alpha.sizes), [2, 3, 4]);
  assert.equal(alpha.positions[3], 1);
  assert.equal(alpha.colors[0], 1);
  assert.equal(alpha.colors[1], 0);
  assert.equal(alpha.colors[5], 1);
  assert.equal(alpha.colors[10], 1);

  const beta = scene.points.find((entry) => entry.id === "beta-points-0");
  assert.ok(beta, "beta entry keyed by mesh name");
  // Overlay positions must arrive world-transformed by the overlay node.
  assert.equal(beta.positions[0], 100);
  assert.equal(beta.positions[3], 101);
  assert.ok(Math.abs(beta.colors[0] - 128 / 255) < 0.00001);
  // The cached-attribute aliases must track the patch, or the quality ladder
  // rebuilds subsets from stale buffers.
  assert.equal(beta._cachedPos, beta.positions);
  assert.equal(beta._cachedColors, beta.colors);

  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("GLB overlay count mismatch keeps base attributes and warns", async () => {
  const env = createContext({
    fetchRoutes: {
      "/models/split-overlay.glb": { bytes: buildSplitOverlayGLBBytes({ betaCount: 5 }) },
      "http://localhost:3000/models/split-base.glb": { bytes: buildSplitBaseGLBBytes() },
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  const scene = await env.context.__gosx_scene3d_gltf_api.sceneLoadGLTFModel("/models/split-overlay.glb");

  const beta = scene.points.find((entry) => entry.id === "beta-points-0");
  assert.ok(beta);
  // Base geometry survives untouched; the skewed overlay is refused.
  assert.equal(beta.count, 2);
  assert.equal(beta.positions[0], 5);
  assert.equal(env.consoleLogs.warn.some((line) => String(line).indexOf("overlay skipped beta-points-0") >= 0), true);

  // Alpha's counts agree, so its patch still lands.
  const alpha = scene.points.find((entry) => entry.id === "alpha-layer");
  assert.equal(alpha.colors[0], 1);
});

test("full overlay-only mesh is appended for presence drift", async () => {
  // A phenomenon layer absent from the reference geometry ships as a full
  // mesh in the overlay and must still reach the scene.
  const gammaPositions = new Float32Array([9, 9, 9]);
  const gammaColors = new Uint8Array([1, 2, 3, 255]);
  const gammaSizes = new Float32Array([11]);
  const overlayBytes = buildGLBBytes({
    asset: { version: "2.0", generator: "runtime-test-drift-overlay" },
    extras: { gosx: { baseSrc: "/models/split-base.glb" } },
    scene: 0,
    scenes: [{ nodes: [0, 1] }],
    nodes: [
      { name: "alpha", mesh: 0 },
      { name: "gamma", mesh: 1 },
    ],
    meshes: [
      {
        name: "alpha",
        primitives: [{
          mode: 0,
          attributes: { COLOR_0: 0 },
          extras: { gosx: { id: "alpha-layer" } },
        }],
      },
      {
        name: "gamma",
        primitives: [{
          mode: 0,
          attributes: { POSITION: 1, COLOR_0: 2, _POINT_SIZE: 3 },
        }],
      },
    ],
    accessors: [
      { bufferView: 0, componentType: 5121, count: 3, type: "VEC4", normalized: true },
      { bufferView: 1, componentType: 5126, count: 1, type: "VEC3" },
      { bufferView: 2, componentType: 5121, count: 1, type: "VEC4", normalized: true },
      { bufferView: 3, componentType: 5126, count: 1, type: "SCALAR" },
    ],
  }, [
    new Uint8Array([255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255]),
    gammaPositions,
    gammaColors,
    gammaSizes,
  ]);

  const env = createContext({
    fetchRoutes: {
      "/models/drift-overlay.glb": { bytes: overlayBytes },
      "http://localhost:3000/models/split-base.glb": { bytes: buildSplitBaseGLBBytes() },
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  const scene = await env.context.__gosx_scene3d_gltf_api.sceneLoadGLTFModel("/models/drift-overlay.glb");

  // Base contributes alpha and beta; the overlay-only gamma is appended, and
  // the patched alpha is not duplicated by its overlay presence.
  assert.equal(scene.points.length, 3);
  const gamma = scene.points.find((entry) => entry.id === "gamma-points-0");
  assert.ok(gamma, "overlay-only mesh must reach the scene");
  assert.equal(gamma.count, 1);
  assert.equal(gamma.positions[0], 9);
  assert.deepEqual(Array.from(gamma.sizes), [11]);
  assert.equal(scene.points.filter((entry) => entry.id === "alpha-layer").length, 1);
});

test("GLB without baseSrc loads exactly as before", async () => {
  const env = createContext({
    fetchRoutes: {
      "/models/split-base.glb": { bytes: buildSplitBaseGLBBytes() },
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  const scene = await env.context.__gosx_scene3d_gltf_api.sceneLoadGLTFModel("/models/split-base.glb");

  assert.equal(env.fetchCalls.length, 1);
  assert.equal(scene.points.length, 2);
  const alpha = scene.points.find((entry) => entry.id === "alpha-layer");
  assert.ok(Math.abs(alpha.colors[0] - 10 / 255) < 0.00001);
});

test("quantized _POINT_SIZE decodes through extras pointSizeScale", async () => {
  const positions = new Float32Array([0, 0, 0, 1, 0, 0]);
  // 16-bit normalized sizes: value/65535 * scale restores source units.
  const sizes = new Uint16Array([Math.round(2.5 / 40 * 65535), Math.round(30 / 40 * 65535)]);
  const bytes = buildGLBBytes({
    asset: { version: "2.0", generator: "runtime-test-quantized-sizes" },
    scene: 0,
    scenes: [{ nodes: [0] }],
    nodes: [{ name: "quantized", mesh: 0 }],
    meshes: [{
      name: "quantized",
      primitives: [{
        mode: 0,
        attributes: { POSITION: 0, _POINT_SIZE: 1 },
        extras: { gosx: { pointSizeScale: 40 } },
      }],
    }],
    accessors: [
      { bufferView: 0, componentType: 5126, count: 2, type: "VEC3" },
      { bufferView: 1, componentType: 5123, count: 2, type: "SCALAR", normalized: true },
    ],
  }, [positions, sizes]);

  const env = createContext({
    fetchRoutes: { "/models/quantized.glb": { bytes } },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  const scene = await env.context.__gosx_scene3d_gltf_api.sceneLoadGLTFModel("/models/quantized.glb");

  assert.equal(scene.points.length, 1);
  const entry = scene.points[0];
  assert.ok(Math.abs(entry.sizes[0] - 2.5) < 0.001, "size 0 restored to source units, got " + entry.sizes[0]);
  assert.ok(Math.abs(entry.sizes[1] - 30) < 0.001, "size 1 restored to source units, got " + entry.sizes[1]);
});
