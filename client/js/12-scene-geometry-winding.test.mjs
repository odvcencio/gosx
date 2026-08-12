// Winding gate for the browser solid-mesh generators in 12-scene-geometry.ts.
//
// The MAIN colour pass reads no winding: the WebGL main pass calls
// gl.disable(gl.CULL_FACE), the WebGPU PBR pipeline sets cullMode "none", and
// sceneRayIntersectsTriangle accepts both faces. Three permissive defaults
// therefore hide a reversed face in every colour image, and every existing test
// passes with the mesh inside out. The only property that can fail there is
// numeric: the geometric normal of each triangle must agree with the shaded
// normals its own three vertices carry.
//
// FOUR browser draw paths do read the winding, so a reversed face is not free.
// The WebGL shadow pass calls cullFace(gl.FRONT); the gosx-shadow and
// gosx-shadow-instanced WebGPU pipelines set cullMode "front"; and
// drawPBRObjects leaves a single-sided Selena mesh on cullMode "back". The
// three shadow sites keep the faces that point away from the light, so the
// winding below decides which surface a browser shadow map records.
// render/bundle/shadow_drift_test.go pins all three settings.
//
// This file is the JavaScript half of assertWindingMatchesNormals in
// scene/geom/geom_test.go. Both sides run the same formula on the same shapes, so
// a divergence fails on whichever side drifted. The native Go renderer culls back
// faces with a counter-clockwise front face, so it draws the far wall of a
// reversed tube — which is how the torus knot defect surfaced at all.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");

// loadGeometryModule loads 12-scene-geometry.ts on its own, with
// generateInstancedGeometry stubbed out. Use it to measure a generator this file
// owns. Pass true to load 16c-scene-shared-pbr.ts first, which makes the real
// instanced generators reachable and lets one test compare the two paths.
function loadGeometryModule(withSharedPBR) {
  const sandbox = {
    console: { warn() {}, log() {}, error() {} },
    Math,
    JSON,
    Number,
    Object,
    Array,
    String,
    Boolean,
    Float32Array,
    isFinite,
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  const context = vm.createContext(sandbox);
  // The helpers earlier bundle files declare. Copied from 10-runtime-scene-core.ts
  // so the generators run exactly as they do in a page.
  vm.runInContext(
    `function sceneNumber(value, fallback) {
       var n = Number(value);
       return Number.isFinite(n) ? n : fallback;
     }
     function normalizeSceneKind(value) {
       return typeof value === "string" ? value.trim().toLowerCase() : "box";
     }
     function sceneBool(value, fallback) { return value == null ? fallback : !!value; }
     ${withSharedPBR ? "" : "function generateInstancedGeometry() { return null; }"}`,
    context,
    { filename: "prelude.js" },
  );
  if (withSharedPBR) {
    vm.runInContext(fs.readFileSync(path.join(srcDir, "16c-scene-shared-pbr.ts"), "utf8"), context, {
      filename: "16c-scene-shared-pbr.ts",
    });
  }
  vm.runInContext(fs.readFileSync(path.join(srcDir, "12-scene-geometry.ts"), "utf8"), context, {
    filename: "12-scene-geometry.ts",
  });
  return context;
}

function buildMesh(context, generator, object) {
  return vm.runInContext(`${generator}(${JSON.stringify(object)})`, context);
}

// measureWinding returns the dot product between each triangle's geometric normal
// and the average of the shaded normals its own vertices carry. It mirrors
// assertWindingMatchesNormals in scene/geom/geom_test.go term for term.
function measureWinding(mesh) {
  const positions = mesh.positions;
  const normals = mesh.normals;
  let worst = Infinity;
  let total = 0;
  let triangles = 0;
  let degenerate = 0;
  let reversed = 0;
  for (let base = 0; base + 8 < positions.length; base += 9) {
    const e0 = [
      positions[base + 3] - positions[base],
      positions[base + 4] - positions[base + 1],
      positions[base + 5] - positions[base + 2],
    ];
    const e1 = [
      positions[base + 6] - positions[base],
      positions[base + 7] - positions[base + 1],
      positions[base + 8] - positions[base + 2],
    ];
    const geometric = [
      e0[1] * e1[2] - e0[2] * e1[1],
      e0[2] * e1[0] - e0[0] * e1[2],
      e0[0] * e1[1] - e0[1] * e1[0],
    ];
    const length = Math.hypot(geometric[0], geometric[1], geometric[2]);
    if (length < 1e-12) {
      degenerate += 1;
      continue;
    }
    let shaded = [0, 0, 0];
    for (let corner = 0; corner < 3; corner += 1) {
      shaded[0] += normals[base + corner * 3];
      shaded[1] += normals[base + corner * 3 + 1];
      shaded[2] += normals[base + corner * 3 + 2];
    }
    const shadedLength = Math.hypot(shaded[0], shaded[1], shaded[2]) || 1;
    const dot =
      (geometric[0] * shaded[0] + geometric[1] * shaded[1] + geometric[2] * shaded[2]) /
      (length * shadedLength);
    worst = Math.min(worst, dot);
    total += dot;
    triangles += 1;
    if (dot <= 0) {
      reversed += 1;
    }
  }
  return { triangles, degenerate, reversed, worst, mean: total / triangles };
}

// Every solid-mesh generator 12-scene-geometry.ts declares, with the parameters
// to build it and the figures the three producers now report.
//
// mean and worst are the measured dot products, rounded to six places. Go reports
// the same six places for the same parameters, because the formula is identical
// and the shapes are identical. The client stores positions as float32 and Go
// keeps float64, so each assertion allows 1e-4 — far below the 2.0 gap a sign
// flip opens.
//
// A negative mean here says the generator winds against its own normals.
const generatorCases = [
  {
    generator: "boxTriangleMesh",
    object: { width: 1, height: 1, depth: 1 },
    triangles: 12,
    degenerate: 0,
    mean: 1.0,
    worst: 1.0,
    // Was -1.000000 before the flip. Six flat quads, so the shaded normal of a
    // triangle equals its geometric normal exactly.
  },
  {
    generator: "planeTriangleMesh",
    object: { width: 2, depth: 3 },
    triangles: 2,
    degenerate: 0,
    mean: 1.0,
    worst: 1.0,
    // Was -1.000000 before the flip. One flat quad about the +y normal.
  },
  {
    generator: "sphereTriangleMesh",
    object: { radius: 1 },
    triangles: 960,
    degenerate: 0,
    mean: 0.99917,
    worst: 0.998844,
    // Was -0.999170 mean and -0.999529 worst before the flip. The generator drops
    // one triangle from the top row and one from the bottom row, so no pole quad
    // collapses and the degenerate count stays at zero. scene/geom keeps its pole
    // slivers and reports 64 degenerate triangles for the same shape, with the
    // same mean and the same worst.
  },
  {
    generator: "sphereTriangleMesh",
    object: { radius: 1, segments: 12 },
    triangles: 120,
    degenerate: 0,
    mean: 0.99369,
    worst: 0.989379,
    // Was -0.993690 mean before the flip. A coarse ball moves the worst dot
    // product, so pin a second resolution and catch a per-ring mistake.
  },
  {
    generator: "torusTriangleMesh",
    object: { radius: 0.7, tube: 0.3 },
    triangles: 1024,
    degenerate: 0,
    mean: 0.997526,
    worst: 0.997024,
    // Was -0.997526 mean and -0.998020 worst before the flip.
  },
  {
    generator: "torusKnotTriangleMesh",
    object: { radius: 0.17, tube: 0.045, tubularSegments: 64, radialSegments: 8 },
    triangles: 1024,
    degenerate: 0,
    mean: 0.991716,
    worst: 0.98749,
    // The torus knot was corrected first, because it also contradicted its own
    // stored normals at -0.998. buildTorusKnot in scene/geom/primitives.go reports
    // the same two figures.
  },
  {
    generator: "torusKnotTriangleMesh",
    object: {},
    triangles: 4096,
    degenerate: 0,
    mean: 0.998066,
    worst: 0.997281,
    // The default resolution, 128 path steps by 16 cross-section steps.
  },
];

test("every solid-mesh generator winds with its own stored normals", () => {
  const context = loadGeometryModule(false);
  for (const testCase of generatorCases) {
    const label = `${testCase.generator}(${JSON.stringify(testCase.object)})`;
    const mesh = buildMesh(context, testCase.generator, testCase.object);
    assert.ok(mesh, `${label} returned no mesh`);
    const measured = measureWinding(mesh);
    assert.equal(measured.triangles, testCase.triangles, `${label} triangle count`);
    assert.equal(measured.degenerate, testCase.degenerate, `${label} degenerate count`);
    assert.equal(
      measured.reversed,
      0,
      `${label}: ${measured.reversed} of ${measured.triangles} triangles oppose their own normals (worst dot ${measured.worst.toFixed(6)})`,
    );
    assert.ok(
      Math.abs(measured.mean - testCase.mean) < 1e-4,
      `${label} mean dot ${measured.mean.toFixed(6)}, want the recorded ${testCase.mean}`,
    );
    assert.ok(
      Math.abs(measured.worst - testCase.worst) < 1e-4,
      `${label} worst dot ${measured.worst.toFixed(6)}, want the recorded ${testCase.worst}`,
    );
  }
});

// The divergence is closed. box, plane, sphere and torus used to wind the other
// way in this file, at -1.000000, -1.000000, -0.999170 and -0.997526 against
// their own normals, while generateInstancedGeometry in 16c-scene-shared-pbr.ts
// and scene/geom in Go reported the same figures positive. One authored box
// therefore had opposite winding depending only on whether the renderer instanced
// it. This test now asserts the opposite of what it once pinned: no generator in
// 12-scene-geometry.ts may wind negatively.
//
// The test also refuses to pass when someone adds a generator and skips the
// table. It reads the function declarations out of the source and requires a case
// for each one, so a new shape cannot enter the file unmeasured.
//
// Two names stay out of the table on purpose:
//   - scenePrimitiveTriangleMesh only dispatches on object.kind;
//   - sceneInstancedTriangleMesh only forwards to 16c-scene-shared-pbr.ts, which
//     the cross-file test below measures directly.
//
// scenePlaneSurfacePositions stays out too. It emits a textured-surface quad with
// no normals, so this formula cannot read it. The test below it pins its sign.
test("no generator in 12-scene-geometry.ts winds against its own normals", () => {
  const source = fs.readFileSync(path.join(srcDir, "12-scene-geometry.ts"), "utf8");
  const declared = new Set(
    Array.from(source.matchAll(/function\s+(\w*TriangleMesh)\s*\(/g), (match) => match[1]),
  );
  declared.delete("scenePrimitiveTriangleMesh");
  declared.delete("sceneInstancedTriangleMesh");
  const covered = new Set(generatorCases.map((testCase) => testCase.generator));
  for (const name of declared) {
    assert.ok(covered.has(name), `${name} has no case in generatorCases; add one and record its dot product`);
  }
  for (const name of covered) {
    assert.ok(declared.has(name), `generatorCases names ${name}, which 12-scene-geometry.ts no longer declares`);
  }

  const context = loadGeometryModule(false);
  for (const testCase of generatorCases) {
    const measured = measureWinding(buildMesh(context, testCase.generator, testCase.object));
    assert.ok(
      measured.mean > 0,
      `${testCase.generator} mean dot ${measured.mean.toFixed(6)} is not positive; the two-convention split is back`,
    );
    // The mean cancels when a mutation reverses only half the triangles, so gate
    // on the reversed count and on the worst triangle as well. A mesh with one
    // reversed face out of a thousand still fails both of these.
    assert.equal(measured.reversed, 0, `${testCase.generator} has ${measured.reversed} reversed triangles`);
    assert.ok(
      measured.worst > 0,
      `${testCase.generator} worst dot ${measured.worst.toFixed(6)} is not positive`,
    );
  }
});

// scenePlaneSurfacePositions is the one triangle emitter in the file that stays
// unflipped. Three reasons hold it back:
//   - it writes positions only, so no stored normal exists to measure against;
//   - both callers are double-sided passes, which never cull either face;
//   - scenePlaneSurfacePositions in client/vm/scene_render_bundle.go is its Go
//     twin and emits the identical order, so the two already agree.
//
// Pin the sign anyway. Four corners listed counter-clockwise about +y give a
// geometric normal of -y here. A future change that enables culling on the
// surface passes must flip both halves together, not one.
test("scenePlaneSurfacePositions keeps its recorded triangle order", () => {
  const context = loadGeometryModule(false);
  const positions = vm.runInContext(
    "scenePlaneSurfacePositions([{x:-1,y:0,z:-1},{x:1,y:0,z:-1},{x:1,y:0,z:1},{x:-1,y:0,z:1}])",
    context,
  );
  assert.equal(positions.length, 18, "two triangles");
  for (let base = 0; base + 8 < positions.length; base += 9) {
    const e0 = [
      positions[base + 3] - positions[base],
      positions[base + 4] - positions[base + 1],
      positions[base + 5] - positions[base + 2],
    ];
    const e1 = [
      positions[base + 6] - positions[base],
      positions[base + 7] - positions[base + 1],
      positions[base + 8] - positions[base + 2],
    ];
    const geometricY = e0[2] * e1[0] - e0[0] * e1[2];
    assert.ok(
      geometricY < 0,
      `surface triangle ${base / 9} geometric normal y ${geometricY}, want the recorded negative value`,
    );
  }
});

// The invariant the two-convention split broke: one authored shape must wind the
// same way whichever browser path builds it.
//
// 12-scene-geometry.ts builds a non-instanced object. generateInstancedGeometry
// in 16c-scene-shared-pbr.ts builds an instanced one. A reader cannot infer this
// from two separate per-file tests, because each file can be self-consistent and
// still disagree with the other. That is exactly the state this closed. So build
// both here and compare the signs directly.
//
// The two files lay their triangles out differently, and that is allowed. The
// sphere is the clear case: this file drops the two pole triangles, while 16c
// keeps them as slivers and reports 24 degenerate triangles for a 12-segment
// ball. Compare the count of real triangles and the mean dot product, never the
// buffer contents.
//
// torusknot stays out of the list. generateInstancedGeometry has no torusknot
// case and returns a box, so a comparison there would measure a box against a
// knot. scene/geom/geom_test.go covers the Go torus knot, and the generator table
// above covers the browser one.
test("both browser geometry paths wind the same shape the same way", () => {
  const context = loadGeometryModule(true);
  const dims = { radius: 0.5, width: 1, height: 1, depth: 1, size: 1, tube: 0.3, segments: 12 };
  // Go reports these means for the same shapes. cylinder, cone and pyramid come
  // out of 16c through sceneInstancedTriangleMesh, so both browser figures are
  // one number by construction; Go builds them from its own code and lands on a
  // different segment count, so it is left out of the comparison for those three.
  const goMean = { box: 1.0, cube: 1.0, plane: 1.0, sphere: 0.99369, torus: 0.997526 };
  for (const kind of ["box", "cube", "plane", "sphere", "torus", "cylinder", "cone", "pyramid"]) {
    const object = Object.assign({ kind }, dims);
    const direct = vm.runInContext(`scenePrimitiveTriangleMesh(${JSON.stringify(object)})`, context);
    const instanced = vm.runInContext(
      `generateInstancedGeometry(${JSON.stringify(kind)}, ${JSON.stringify(dims)})`,
      context,
    );
    assert.ok(direct, `${kind}: 12-scene-geometry.ts built no mesh`);
    assert.ok(instanced, `${kind}: generateInstancedGeometry built no mesh`);

    const directMeasured = measureWinding(direct);
    const instancedMeasured = measureWinding(instanced);
    assert.ok(directMeasured.mean > 0, `${kind}: the non-instanced path winds at ${directMeasured.mean.toFixed(6)}`);
    assert.ok(instancedMeasured.mean > 0, `${kind}: the instanced path winds at ${instancedMeasured.mean.toFixed(6)}`);
    assert.equal(
      Math.sign(directMeasured.mean),
      Math.sign(instancedMeasured.mean),
      `${kind}: the two browser paths wind opposite ways (${directMeasured.mean.toFixed(6)} against ${instancedMeasured.mean.toFixed(6)})`,
    );
    assert.equal(
      directMeasured.triangles,
      instancedMeasured.triangles,
      `${kind}: the two paths build a different number of real triangles`,
    );
    assert.ok(
      Math.abs(directMeasured.mean - instancedMeasured.mean) < 1e-4,
      `${kind}: mean dot ${directMeasured.mean.toFixed(6)} against ${instancedMeasured.mean.toFixed(6)}`,
    );
    if (goMean[kind] !== undefined) {
      assert.ok(
        Math.abs(directMeasured.mean - goMean[kind]) < 1e-4,
        `${kind}: mean dot ${directMeasured.mean.toFixed(6)}, want the Go figure ${goMean[kind]}`,
      );
    }
  }
});
