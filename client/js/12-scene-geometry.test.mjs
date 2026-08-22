import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const sourceDir = path.join(__dirname, "bootstrap-src");

function extractFunction(source, name) {
  const marker = "function " + name + "(";
  const start = source.indexOf(marker);
  assert.ok(start >= 0, name + " not found");
  let depth = 0;
  let sawBrace = false;
  for (let index = start; index < source.length; index += 1) {
    if (source[index] === "{") {
      depth += 1;
      sawBrace = true;
    } else if (source[index] === "}") {
      depth -= 1;
      if (sawBrace && depth === 0) {
        return source.slice(start, index + 1);
      }
    }
  }
  throw new Error("unbalanced function " + name);
}

function loadGeometry() {
  const geometrySource = fs.readFileSync(path.join(sourceDir, "12-scene-geometry.ts"), "utf8");
  const coreSource = fs.readFileSync(path.join(sourceDir, "10-runtime-scene-core.ts"), "utf8");
  const utilsSource = fs.readFileSync(path.join(sourceDir, "10-runtime-scene-utils.ts"), "utf8");
  const script = [
    "globalThis.__geometry = (function() {",
    extractFunction(utilsSource, "sceneNumber"),
    extractFunction(coreSource, "translateScenePointInto"),
    geometrySource,
    "return { boxVertices, planeQuadVertices, planeSegments, planeTriangleMesh,",
    "scenePrimitiveTriangleMesh, scenePlaneLocalCorners, scenePlaneSurfaceCorners,",
    "scenePlaneSurfacePositions, scenePlaneSurfaceUVs };",
    "})();",
  ].join("\n");
  const context = { console };
  vm.createContext(context);
  vm.runInContext(script, context);
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

function distinctPointCount(points) {
  return new Set(points.map((point) => [point.x, point.y, point.z].join("|"))).size;
}

function flatTriangleArea(positions) {
  let area = 0;
  for (let index = 0; index < positions.length; index += 9) {
    area += triangleArea(
      { x: positions[index], y: positions[index + 1], z: positions[index + 2] },
      { x: positions[index + 3], y: positions[index + 4], z: positions[index + 5] },
      { x: positions[index + 6], y: positions[index + 7], z: positions[index + 8] },
    );
  }
  return area;
}

test("height-zero box prefix documents the plane degeneracy trap", () => {
  const prefix = geometry.boxVertices(2, 0, 2).slice(0, 4);
  assert.equal(distinctPointCount(prefix), 2);
  assert.equal(triangleArea(prefix[0], prefix[1], prefix[2]), 0);
});

test("planeQuadVertices walks four corners of the XZ ring", () => {
  const quad = geometry.planeQuadVertices(4, 2);
  assert.equal(distinctPointCount(quad), 4);
  assert.deepEqual(
    Array.from(quad, (point) => [point.x, point.y + 0, point.z]),
    [[-2, 0, -1], [2, 0, -1], [2, 0, 1], [-2, 0, 1]],
  );
});

test("plane wireframe and solid generators preserve requested area", () => {
  const segments = geometry.planeSegments({ width: 4, depth: 2 });
  assert.equal(segments.length, 4);
  let perimeter = 0;
  for (const [a, b] of segments) {
    const length = Math.hypot(b.x - a.x, b.y - a.y, b.z - a.z);
    assert.ok(length > 0);
    perimeter += length;
  }
  assert.equal(perimeter, 12);

  const direct = geometry.planeTriangleMesh({ width: 3, depth: 2 });
  assert.equal(direct.positions.length, 18);
  assert.ok(Math.abs(flatTriangleArea(direct.positions) - 6) < 1e-9);

  const dispatched = geometry.scenePrimitiveTriangleMesh({ kind: "plane", width: 5, depth: 4 });
  assert.ok(Math.abs(flatTriangleArea(dispatched.positions) - 20) < 1e-9);
});

test("HTML texture surface geometry remains nondegenerate before and after rotation", () => {
  const base = {
    width: 2.4,
    depth: 1.2,
    height: 0,
    x: 0,
    y: 0,
    z: 0,
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
  };
  const flat = geometry.scenePlaneSurfaceCorners(base, 0).map((point) => ({ ...point }));
  assert.equal(distinctPointCount(flat), 4);
  assert.ok(Math.abs(flatTriangleArea(geometry.scenePlaneSurfacePositions(flat)) - 2.88) < 1e-9);

  const upright = geometry.scenePlaneSurfaceCorners(
    { ...base, width: 2, depth: 1, rotationX: -Math.PI / 2 },
    0,
  ).map((point) => ({ ...point }));
  const spanY = Math.max(...upright.map((point) => point.y)) - Math.min(...upright.map((point) => point.y));
  const spanZ = Math.max(...upright.map((point) => point.z)) - Math.min(...upright.map((point) => point.z));
  assert.ok(Math.abs(spanY - 1) < 1e-9);
  assert.ok(spanZ < 1e-9);
  assert.deepEqual(Array.from(geometry.scenePlaneSurfaceUVs()), [0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 0, 0]);
});
