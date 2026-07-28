// Generator-level geometry tests for the plane primitive family.
//
// These tests call the REAL generators in bootstrap-src/12-scene-geometry.js.
// That is the point. The plane quad used to be derived with
// `boxVertices(width, 0, depth).slice(0, 4)`, which collapses to a single edge
// when height is 0, so every solid plane and every HTML-texture 3D surface had
// zero area and never drew. A test that feeds hand-written corners into the
// downstream helpers cannot see that defect, because the defect lives in the
// generator. So the assertions below start from an object descriptor and
// measure the emitted triangles.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");

function readChunk(name) {
  return fs.readFileSync(path.join(srcDir, name), "utf8");
}

// extractFunction pulls one named function declaration out of a bootstrap
// chunk by brace matching. The chunks are concatenated into a single IIFE at
// build time, so they cannot be imported; lifting the exact source keeps the
// test bound to the shipped implementation instead of a copy of it.
function extractFunction(source, name) {
  const marker = "function " + name + "(";
  const start = source.indexOf(marker);
  assert.ok(start >= 0, "function " + name + " not found");
  let depth = 0;
  let seenBrace = false;
  for (let i = start; i < source.length; i += 1) {
    const ch = source[i];
    if (ch === "{") {
      depth += 1;
      seenBrace = true;
    } else if (ch === "}") {
      depth -= 1;
      if (seenBrace && depth === 0) {
        return source.slice(start, i + 1);
      }
    }
  }
  throw new Error("unbalanced braces extracting " + name);
}

const geometrySrc = readChunk("12-scene-geometry.js");
const sceneCoreSrc = readChunk("10-runtime-scene-core.js");

const EXPORTS = [
  "boxVertices",
  "planeQuadVertices",
  "planeSegments",
  "planeTriangleMesh",
  "scenePrimitiveTriangleMesh",
  "scenePlaneLocalCorners",
  "scenePlaneSurfaceCorners",
  "scenePlaneSurfacePositions",
  "scenePlaneSurfaceUVs",
];

function loadGeometry() {
  const preamble = [
    extractFunction(sceneCoreSrc, "sceneNumber"),
    extractFunction(sceneCoreSrc, "translateScenePointInto"),
  ].join("\n");
  const wrapped =
    "globalThis.__geometry = (function () {\n" +
    preamble +
    "\n" +
    geometrySrc +
    "\nreturn { " +
    EXPORTS.join(", ") +
    " };\n})();";
  const context = { console };
  vm.createContext(context);
  vm.runInContext(wrapped, context);
  return context.__geometry;
}

const geometry = loadGeometry();

function triangleArea(a, b, c) {
  const ux = b.x - a.x;
  const uy = b.y - a.y;
  const uz = b.z - a.z;
  const vx = c.x - a.x;
  const vy = c.y - a.y;
  const vz = c.z - a.z;
  const cx = uy * vz - uz * vy;
  const cy = uz * vx - ux * vz;
  const cz = ux * vy - uy * vx;
  return Math.sqrt(cx * cx + cy * cy + cz * cz) / 2;
}

function meshTriangles(mesh) {
  const positions = mesh.positions;
  assert.ok(positions && positions.length % 9 === 0, "positions must be whole triangles");
  const tris = [];
  for (let i = 0; i < positions.length; i += 9) {
    tris.push([
      { x: positions[i], y: positions[i + 1], z: positions[i + 2] },
      { x: positions[i + 3], y: positions[i + 4], z: positions[i + 5] },
      { x: positions[i + 6], y: positions[i + 7], z: positions[i + 8] },
    ]);
  }
  return tris;
}

function positionTriangles(flat) {
  const tris = [];
  for (let i = 0; i < flat.length; i += 9) {
    tris.push([
      { x: flat[i], y: flat[i + 1], z: flat[i + 2] },
      { x: flat[i + 3], y: flat[i + 4], z: flat[i + 5] },
      { x: flat[i + 6], y: flat[i + 7], z: flat[i + 8] },
    ]);
  }
  return tris;
}

function distinctPointCount(points) {
  const keys = new Set();
  for (const point of points) {
    keys.add([point.x.toFixed(9), point.y.toFixed(9), point.z.toFixed(9)].join("|"));
  }
  return keys.size;
}

// Documents the exact trap this module has to avoid. If someone reintroduces
// `.slice(0, 4)` this assertion explains why it is wrong.
test("the first four box vertices are degenerate at height zero", () => {
  const slice = geometry.boxVertices(2, 0, 2).slice(0, 4);
  assert.equal(distinctPointCount(slice), 2, "slice(0,4) collapses onto one edge");
  assert.equal(triangleArea(slice[0], slice[1], slice[2]), 0);
});

test("planeQuadVertices walks the real XZ ring", () => {
  const quad = geometry.planeQuadVertices(4, 2);
  assert.equal(distinctPointCount(quad), 4);
  const expected = [
    [-2, 0, -1],
    [2, 0, -1],
    [2, 0, 1],
    [-2, 0, 1],
  ];
  for (let i = 0; i < expected.length; i += 1) {
    assert.ok(Math.abs(quad[i].x - expected[i][0]) < 1e-9, `corner ${i} x`);
    assert.ok(Math.abs(quad[i].y - expected[i][1]) < 1e-9, `corner ${i} y`);
    assert.ok(Math.abs(quad[i].z - expected[i][2]) < 1e-9, `corner ${i} z`);
  }
});

test("scenePlaneLocalCorners emits a non-degenerate quad of the requested size", () => {
  for (const [width, depth] of [[1, 1], [4, 2], [0.25, 3.5], [1.8, 0.72]]) {
    const corners = geometry.scenePlaneLocalCorners({ width, depth });
    assert.equal(corners.length, 4, "expected four corners");
    assert.equal(distinctPointCount(corners), 4, `corners collapsed for ${width}x${depth}`);
    const area =
      triangleArea(corners[0], corners[1], corners[2]) +
      triangleArea(corners[0], corners[2], corners[3]);
    assert.ok(Math.abs(area - width * depth) < 1e-9, `area ${area} != ${width * depth}`);
  }
});

test("scenePlaneLocalCorners accepts height as the depth alias", () => {
  const corners = geometry.scenePlaneLocalCorners({ width: 2, height: 6 });
  const area =
    triangleArea(corners[0], corners[1], corners[2]) +
    triangleArea(corners[0], corners[2], corners[3]);
  assert.ok(Math.abs(area - 12) < 1e-9);
});

test("planeTriangleMesh emits two non-degenerate triangles", () => {
  const mesh = geometry.planeTriangleMesh({ width: 3, depth: 2 });
  const tris = meshTriangles(mesh);
  assert.equal(tris.length, 2);
  let total = 0;
  for (const tri of tris) {
    const area = triangleArea(tri[0], tri[1], tri[2]);
    assert.ok(area > 0, "plane triangle is degenerate");
    total += area;
  }
  assert.ok(Math.abs(total - 6) < 1e-9, `mesh area ${total} != 6`);
});

test("the plane primitive dispatcher reaches the fixed generator", () => {
  const mesh = geometry.scenePrimitiveTriangleMesh({ kind: "plane", width: 5, depth: 4 });
  let total = 0;
  for (const tri of meshTriangles(mesh)) {
    total += triangleArea(tri[0], tri[1], tri[2]);
  }
  assert.ok(Math.abs(total - 20) < 1e-9, `dispatcher mesh area ${total} != 20`);
});

test("planeSegments draws a square, not a single line", () => {
  const segments = geometry.planeSegments({ width: 4, depth: 2 });
  assert.equal(segments.length, 4);
  let perimeter = 0;
  for (const [a, b] of segments) {
    const dx = b.x - a.x;
    const dy = b.y - a.y;
    const dz = b.z - a.z;
    const length = Math.sqrt(dx * dx + dy * dy + dz * dz);
    assert.ok(length > 0, "wireframe plane emitted a zero-length edge");
    perimeter += length;
  }
  assert.ok(Math.abs(perimeter - 12) < 1e-9, `perimeter ${perimeter} != 12`);
});

// The HTML-texture 3D surface path builds exactly this descriptor in
// sceneHTMLTextureSurfaceObject (10-runtime-scene-core.js) and hands it to
// scenePlaneSurfaceCorners. Any degeneracy here makes an HTML surface
// invisible with no error anywhere.
function htmlSurfaceObject(overrides) {
  return Object.assign(
    {
      x: 0,
      y: 0,
      z: 0,
      width: 1.8,
      depth: 0.72,
      height: 0,
      scaleX: 1,
      scaleY: 1,
      scaleZ: 1,
      rotationX: 0,
      rotationY: 0,
      rotationZ: 0,
      spinX: 0,
      spinY: 0,
      spinZ: 0,
      shiftX: 0,
      shiftY: 0,
      shiftZ: 0,
      driftSpeed: 0,
      driftPhase: 0,
    },
    overrides || {}
  );
}

test("an HTML-texture surface quad has the area its author asked for", () => {
  const corners = geometry.scenePlaneSurfaceCorners(htmlSurfaceObject({ width: 2.4, depth: 1.2 }), 0);
  assert.equal(distinctPointCount(corners), 4);
  const positions = geometry.scenePlaneSurfacePositions(corners);
  assert.equal(positions.length, 18, "expected six vertices");
  let total = 0;
  for (const tri of positionTriangles(positions)) {
    const area = triangleArea(tri[0], tri[1], tri[2]);
    assert.ok(area > 0, "HTML surface triangle is degenerate");
    total += area;
  }
  assert.ok(Math.abs(total - 2.4 * 1.2) < 1e-9, `surface area ${total} != 2.88`);
});

test("an upright HTML surface faces the camera instead of lying flat", () => {
  // rotationX = -PI/2 stands the XZ quad up into the XY plane, which is what a
  // wall-mounted panel or a hero card needs in front of a +Z camera.
  const corners = geometry.scenePlaneSurfaceCorners(
    htmlSurfaceObject({ width: 2, depth: 1, rotationX: -Math.PI / 2 }),
    0
  );
  const spanY = Math.max(...corners.map((p) => p.y)) - Math.min(...corners.map((p) => p.y));
  const spanZ = Math.max(...corners.map((p) => p.z)) - Math.min(...corners.map((p) => p.z));
  assert.ok(Math.abs(spanY - 1) < 1e-9, `upright surface spans ${spanY} in Y, expected 1`);
  assert.ok(spanZ < 1e-9, `upright surface should be flat in Z, spans ${spanZ}`);
});

test("surface positions and UVs agree on the corner ring", () => {
  const corners = geometry.scenePlaneSurfaceCorners(htmlSurfaceObject({ width: 2, depth: 2 }), 0);
  const positions = geometry.scenePlaneSurfacePositions(corners);
  const uvs = geometry.scenePlaneSurfaceUVs();
  assert.equal(uvs.length, 12, "six vertices of two floats");
  // Vertex order is corners 0,1,2 then 0,2,3. Corner 0 appears twice and must
  // carry the same UV both times, otherwise the texture shears across the seam.
  const order = [0, 1, 2, 0, 2, 3];
  const uvByCorner = new Map();
  for (let vertex = 0; vertex < order.length; vertex += 1) {
    const corner = order[vertex];
    const uv = [uvs[vertex * 2], uvs[vertex * 2 + 1]];
    const px = positions[vertex * 3];
    const pz = positions[vertex * 3 + 2];
    assert.ok(Math.abs(px - corners[corner].x) < 1e-9);
    assert.ok(Math.abs(pz - corners[corner].z) < 1e-9);
    const previous = uvByCorner.get(corner);
    if (previous) {
      assert.deepEqual(uv, previous, "shared corner carries two different UVs");
    }
    uvByCorner.set(corner, uv);
  }
  // u tracks +x and v tracks -z, so the texture's top row lands on the -z edge.
  assert.deepEqual(uvByCorner.get(0), [0, 1]);
  assert.deepEqual(uvByCorner.get(1), [1, 1]);
  assert.deepEqual(uvByCorner.get(2), [1, 0]);
  assert.deepEqual(uvByCorner.get(3), [0, 0]);
});
