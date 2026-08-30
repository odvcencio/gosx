// Pick parity tests for the shared CPU raycast in 17-scene-input.ts.
//
// The pick contract is backend independent by design: setupScenePickInteractions
// takes no renderer argument, 17-scene-input.ts names neither WebGL nor WebGPU,
// and the WebGPU picker resolves identity on the GPU and then calls these same
// helpers for every geometric field. A backend branch in this file would break
// the property the capability matrix relies on, so these tests load the input
// fragment alone, with no renderer present.
//
// The numbers below come from Go. scene.TraceGraph in scene/raycast.go picks a
// Points particle and a Sprite as spheres of scene.DefaultPointThreshold (0.1),
// so a particle at (0, 0, -3) seen from (0, 0, 4) reports a distance of exactly
// 6.9 and a hit point at z = -2.9. The browser could not pick either family
// until sceneRaycastPick started to call sceneRaycastPickPoints, which meant a
// headless test proved a hit no browser could reproduce.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");

// The world-space hit radius both languages use for a primitive with no extent.
// scene.DefaultPointThreshold in scene/raycast.go and SCENE_POINT_PICK_RADIUS in
// 17-scene-input.ts must equal this.
const POINT_THRESHOLD = 0.1;

function createContext() {
  const sandbox = {
    console: { warn() {}, log() {}, error() {} },
    Math,
    JSON,
    Number,
    Object,
    Array,
    String,
    Boolean,
    Set,
    Float32Array,
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  sandbox.__gosx_scene3d_api = {};
  const context = vm.createContext(sandbox);
  // Helpers earlier bundle files declare. Copied from 10-runtime-scene-core.js so
  // sceneScreenToRay builds a production ray.
  vm.runInContext(
    `function sceneNumber(value, fallback) {
       var n = Number(value);
       return Number.isFinite(n) ? n : fallback;
     }
     function sceneBool(value, fallback) { return value == null ? fallback : !!value; }
     function clamp01(v) { return Math.max(0, Math.min(1, Number(v) || 0)); }
     function normalizeSceneCameraKind(value, fallback) {
       var text = typeof value === "string" ? value.trim().toLowerCase() : "";
       return text === "orthographic" ? "orthographic" : fallback;
     }
     function normalizeSceneKind(value) {
       return typeof value === "string" ? value.trim().toLowerCase() : "box";
     }
     function sceneRenderCamera(camera) {
       return {
         kind: normalizeSceneCameraKind(camera && camera.kind, "perspective"),
         x: sceneNumber(camera && camera.x, 0),
         y: sceneNumber(camera && camera.y, 0),
         z: sceneNumber(camera && camera.z, 6),
         rotationX: sceneNumber(camera && camera.rotationX, 0),
         rotationY: sceneNumber(camera && camera.rotationY, 0),
         rotationZ: sceneNumber(camera && camera.rotationZ, 0),
         fov: sceneNumber(camera && camera.fov, 75),
         left: sceneNumber(camera && camera.left, 0),
         right: sceneNumber(camera && camera.right, 0),
         top: sceneNumber(camera && camera.top, 0),
         bottom: sceneNumber(camera && camera.bottom, 0),
         zoom: Math.max(0.0001, sceneNumber(camera && camera.zoom, 1)),
         near: sceneNumber(camera && camera.near, 0.05),
         far: sceneNumber(camera && camera.far, 128),
       };
     }
     function sceneOrthographicBounds(camera, width, height) {
       var cam = sceneRenderCamera(camera);
       var aspect = Math.max(0.0001, sceneNumber(width, 1) / Math.max(1, sceneNumber(height, 1)));
       var hh = 3, hw = 3 * aspect;
       return { left: -hw, right: hw, top: hh, bottom: -hh };
     }
     function queueInputSignal() {}
     function sceneProjectPoint() { return null; }`,
    context,
    { filename: "prelude.js" },
  );
  vm.runInContext(fs.readFileSync(path.join(srcDir, "11-scene-math.ts"), "utf8"), context, {
    filename: "11-scene-math.ts",
  });
  vm.runInContext(fs.readFileSync(path.join(srcDir, "17-scene-input.ts"), "utf8"), context, {
    filename: "17-scene-input.ts",
  });
  return context;
}

// pick runs the shared raycast through the centre of a 640x360 viewport with the
// camera at (0, 0, 4) looking down -Z. sceneScreenToRay returns origin (0, 0, 4)
// and direction (0, 0, -1) there, which is the exact ray the Go tests use.
function pick(context, bundle, pointerX = 320, pointerY = 180) {
  context.__bundle = bundle;
  context.__px = pointerX;
  context.__py = pointerY;
  return vm.runInContext("sceneRaycastPick(__px, __py, 640, 360, __bundle.camera, __bundle)", context);
}

function baseBundle(extra) {
  return Object.assign(
    {
      camera: { x: 0, y: 0, z: 4, fov: 75, near: 0.05, far: 128 },
      meshObjects: [],
      instancedMeshes: [],
      objects: [],
      points: [],
      sprites: [],
      timeSeconds: 0,
    },
    extra || {},
  );
}

function pointsLayer(extra) {
  return Object.assign(
    { id: "cloud", count: 1, positions: [0, 0, -3] },
    extra || {},
  );
}

// A two-triangle quad at z = -1, nearer the camera than the point cloud. Copied
// from the WebGPU pick suite so both suites describe the same mesh.
function quad(z) {
  return {
    worldMeshPositions: new Float32Array([
      -1, -1, z, 1, -1, z, 1, 1, z,
      -1, -1, z, 1, 1, z, -1, 1, z,
    ]),
    worldMeshUVs: new Float32Array([0, 0, 1, 0, 1, 1, 0, 0, 1, 1, 0, 1]),
    meshObjects: [{
      id: "quad",
      kind: "box",
      pickable: true,
      vertexOffset: 0,
      vertexCount: 6,
      bounds: { minX: -1, minY: -1, minZ: z, maxX: 1, maxY: 1, maxZ: z },
    }],
  };
}

test("a Points particle is pickable and reports the Go distance", () => {
  const context = createContext();
  const hit = pick(context, baseBundle({ points: [pointsLayer()] }));
  assert.ok(hit, "expected a hit on the point cloud");
  // Go: TraceGraph(Points{Positions: [(0,0,-3)]}, Ray{(0,0,4), (0,0,-1)}) reports
  // 7 - 0.1 = 6.9 and a point at z = -3 + 0.1.
  assert.ok(Math.abs(hit.distance - (7 - POINT_THRESHOLD)) < 1e-9, `distance ${hit.distance}`);
  assert.ok(Math.abs(hit.worldPosition.z - (-3 + POINT_THRESHOLD)) < 1e-9, `z ${hit.worldPosition.z}`);
  assert.equal(hit.depth, hit.distance);
  assert.equal(hit.inside, true);
});

test("a Points hit names the layer, the kind and the particle", () => {
  const context = createContext();
  const bundle = baseBundle({
    points: [pointsLayer({ id: "stars", count: 3, positions: [5, 5, -3, 0, 0, -3, -5, -5, -3] })],
  });
  const hit = pick(context, bundle);
  assert.ok(hit);
  // The identifier must name the layer that was hit, never the background and
  // never an empty string: a pick whose id or kind is empty publishes an object
  // signal slug of "object-0" and every id-keyed action misfires.
  assert.equal(hit.object.id, "stars");
  context.__hit = hit;
  assert.equal(vm.runInContext("sceneTargetID(__hit)", context), "stars");
  assert.equal(vm.runInContext("sceneTargetKind(__hit)", context), "points");
  // Go sets RayHit.InstanceIndex to the particle index. Particle 1 is the one on
  // the ray; particles 0 and 2 sit far off axis.
  assert.equal(hit.instanceIndex, 1);
  assert.equal(hit.primitiveIndex, -1);
  assert.equal(hit.triangleIndex, -1);
});

test("a Points layer honors its position offset and its rotation", () => {
  const context = createContext();
  // The particle sits at local (0, 2, 0). A quarter turn about +X maps it to
  // (0, 0, 2), and the offset then pushes it to (0, 0, -3).
  const hit = pick(context, baseBundle({
    points: [pointsLayer({
      positions: [0, 2, 0],
      rotationX: Math.PI / 2,
      z: -5,
    })],
  }));
  assert.ok(hit, "expected the rotated particle on the ray");
  assert.ok(Math.abs(hit.distance - (7 - POINT_THRESHOLD)) < 1e-9, `distance ${hit.distance}`);
});

test("a Points layer pick applies its exact affine parent after local rotation and offset", () => {
  const context = createContext();
  const hit = pick(context, baseBundle({
    points: [pointsLayer({
      positions: [1, 0, 0],
      rotationZ: Math.PI / 2,
      x: 1,
      y: 2,
      z: 3,
      // Maps local world (1,3,3) to (0,0,-3), with non-uniform scale and shear.
      parentMatrix: [2, 0, 0, 0, 1, 3, 0, 0, 0, 0, 4, 0, -5, -9, -15, 1],
    })],
  }));
  assert.ok(hit, "expected the affine-transformed particle on the ray");
  assert.ok(Math.abs(hit.distance - (7 - POINT_THRESHOLD)) < 1e-9, `distance ${hit.distance}`);
});

test("scenePointsRotationMatrix agrees with sceneRotatePoint on all three axes", () => {
  const context = createContext();
  context.__entry = { rotationX: 0.37, rotationY: -1.21, rotationZ: 2.04 };
  context.__local = { x: 0.31, y: -0.77, z: 1.42 };
  const got = vm.runInContext(
    `(function () {
       var m = scenePointsRotationMatrix(__entry.rotationX, __entry.rotationY, __entry.rotationZ);
       return {
         x: m[0] * __local.x + m[1] * __local.y + m[2] * __local.z,
         y: m[3] * __local.x + m[4] * __local.y + m[5] * __local.z,
         z: m[6] * __local.x + m[7] * __local.y + m[8] * __local.z,
       };
     })()`,
    context,
  );
  const want = vm.runInContext(
    "sceneRotatePoint(__local, __entry.rotationX, __entry.rotationY, __entry.rotationZ)",
    context,
  );
  for (const axis of ["x", "y", "z"]) {
    assert.ok(
      Math.abs(got[axis] - want[axis]) < 1e-12,
      `${axis}: matrix ${got[axis]} against sceneRotatePoint ${want[axis]}`,
    );
  }
});

test("a Points layer stops at its declared count", () => {
  const context = createContext();
  // Two particles of data, one declared. The undeclared particle sits on the ray
  // and must not answer, exactly as Go clamps count against len(Positions).
  const hit = pick(context, baseBundle({
    points: [pointsLayer({ count: 1, positions: [9, 9, -3, 0, 0, -3] })],
  }));
  assert.equal(hit, null, "the second particle is outside the declared count");
});

test("a Sprite is pickable and reports the Go distance", () => {
  const context = createContext();
  const hit = pick(context, baseBundle({
    sprites: [{ id: "badge", world: { x: 0, y: 0, z: -3 }, scale: 1, position: { x: 320, y: 180 }, depth: 7 }],
  }));
  assert.ok(hit, "expected a hit on the sprite");
  assert.ok(Math.abs(hit.distance - (7 - POINT_THRESHOLD)) < 1e-9, `distance ${hit.distance}`);
  assert.equal(hit.object.id, "badge");
  context.__hit = hit;
  assert.equal(vm.runInContext("sceneTargetKind(__hit)", context), "sprite");
  // raycastSprite leaves RayHit.InstanceIndex nil, so a sprite reports none.
  assert.equal(hit.instanceIndex, -1);
});

test("a Sprite hit radius grows with its scale, like spriteRadiusScale", () => {
  const context = createContext();
  const hit = pick(context, baseBundle({
    sprites: [{ id: "badge", world: { x: 0, y: 0, z: -3 }, scale: 2 }],
  }));
  assert.ok(hit);
  assert.ok(Math.abs(hit.distance - (7 - 2 * POINT_THRESHOLD)) < 1e-9, `distance ${hit.distance}`);
});

test("a Sprite with no world point is skipped instead of picked at the origin", () => {
  const context = createContext();
  // A bundle from an older producer carries the projected screen position only.
  // Defaulting the missing world point to (0, 0, 0) would report a hit for a
  // sprite that sits somewhere else, which is worse than reporting none.
  const hit = pick(context, baseBundle({
    sprites: [{ id: "badge", position: { x: 320, y: 180 }, depth: 7, width: 64, height: 64 }],
  }));
  assert.equal(hit, null);
});

test("an anchored Sprite is skipped, as raycastSprite skips it", () => {
  const context = createContext();
  const hit = pick(context, baseBundle({
    sprites: [{ id: "badge", target: "console", world: { x: 0, y: 0, z: -3 } }],
  }));
  assert.equal(hit, null);
});

test("the nearest hit wins between a mesh and a point cloud", () => {
  const context = createContext();
  const nearMesh = pick(context, baseBundle(Object.assign(quad(-1), { points: [pointsLayer()] })));
  assert.ok(nearMesh);
  assert.equal(nearMesh.object.id, "quad", "the quad at z = -1 is nearer than the particle at z = -3");

  const nearPoints = pick(context, baseBundle(Object.assign(quad(-6), { points: [pointsLayer()] })));
  assert.ok(nearPoints);
  assert.equal(nearPoints.object.id, "cloud", "the particle at z = -3 is nearer than the quad at z = -6");
  assert.ok(Math.abs(nearPoints.distance - (7 - POINT_THRESHOLD)) < 1e-9);
});

test("an empty points and sprites bundle still picks a mesh", () => {
  const context = createContext();
  const hit = pick(context, baseBundle(quad(-1)));
  assert.ok(hit);
  assert.equal(hit.object.id, "quad");
  assert.equal(hit.triangleIndex, 0);
});

test("a points pick publishes the ray direction, not zeros", () => {
  const context = createContext();
  const hit = pick(context, baseBundle({ points: [pointsLayer()] }));
  context.__hit = hit;
  const snapshot = vm.runInContext("sceneTargetHitSnapshot(__hit)", context);
  // sceneScreenToRay names its direction `dir`. The snapshot read `direction`
  // only until 2026-07-26, so every ray pick published (0, 0, 0) here while the
  // origin came through — a field declared and never assigned.
  assert.equal(snapshot.rayOriginZ, 4);
  assert.ok(Math.abs(snapshot.rayDirZ + 1) < 1e-9, `rayDirZ ${snapshot.rayDirZ}, want -1`);
  assert.equal(snapshot.rayDirX, 0);
  assert.equal(snapshot.rayDirY, 0);
  assert.equal(snapshot.targetInstanceIndex, 0);
  assert.ok(Math.abs(snapshot.worldZ - (-3 + POINT_THRESHOLD)) < 1e-9);
});

test("a points buffer holding a NaN reports no hit instead of a NaN distance", () => {
  const context = createContext();
  // A NaN coordinate yields a NaN distance unless the sphere test rejects it, and
  // every later `distance < closest.distance` comparison reads false against a
  // NaN. One bad particle would then keep the pick for the whole frame and report
  // a hit at no position at all.
  const hit = pick(context, baseBundle({
    points: [pointsLayer({ count: 2, positions: [Number.NaN, 0, -3, 0, 0, -3] })],
  }));
  assert.ok(hit, "the valid particle must still answer");
  assert.equal(hit.instanceIndex, 1);
  assert.ok(Number.isFinite(hit.distance), `distance ${hit.distance}`);
  assert.ok(Math.abs(hit.distance - (7 - POINT_THRESHOLD)) < 1e-9);
});

test("the pick path branches on no render backend", () => {
  // setupScenePickInteractions takes no renderer argument, and the GPU picker in
  // the WebGPU renderer derives every geometric field from these helpers. One
  // branch on a backend here would split the pick contract in two, which is the
  // property the capability matrix rests on.
  //
  // Strip comments before the scan. A comment may name a backend to explain why
  // the code does not, and the difference between naming one and branching on one
  // is the whole point.
  const source = fs.readFileSync(path.join(srcDir, "17-scene-input.ts"), "utf8");
  const code = source
    .replace(/\/\*[\s\S]*?\*\//g, " ")
    .split("\n")
    .map((line) => line.replace(/(^|[^:"'`\\])\/\/.*$/, "$1"))
    .join("\n")
    .toLowerCase();
  for (const term of ["webgl", "webgpu", "readpixels", "getcontext", "gl.", "gpudevice"]) {
    assert.equal(code.includes(term), false, `the code in 17-scene-input.ts must not mention ${term}`);
  }
  // Prove the stripper still sees code: the two helpers this suite drives must
  // survive it, or the scan above would pass on an empty string.
  assert.ok(code.includes("function sceneraycastpickpoints"));
  assert.ok(code.includes("function sceneraycastpick("));
});
