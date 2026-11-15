'use strict';
/* Pointer-pick geometry for the affine Group.Scale browser proof.
 *
 * The fixture object 'affine-group-child' is one local triangle under a parent
 * matrix with x scale -2 and a leaf rotation of 45 degrees about Z. A pick at
 * the canvas centre hits world (0.5, 0.5, 1), which is the image of the local
 * origin. A pick that dropped the scale would hit it too, so it proves nothing.
 * This module picks a pixel that only the scaled triangle covers, and works out
 * the world point and depth that pick must report.
 *
 * Derivation (no browser needed; `node scene3d-affine-pick-geometry.cjs` checks it):
 *   camera at (0.5, 0.5, 3) looking down -Z, vertical FOV 50 degrees, canvas
 *   320 x 180 CSS pixels at device pixel ratio 1. The runtime builds the pick
 *   ray as (px / f, -py / f, -1), normalized, with f = (180 / 2) / tan(25 deg)
 *   and (px, py) the offset from the canvas centre in pixels (y down).
 *   The triangle lies in the plane z = 1, two units in front of the camera. The
 *   unit ray reaches it at distance t = 2 / |d.z|, where d is the unit ray, and
 *   the world point is the camera position plus d * t. The pick reports
 *   depth = t, the distance along the unit ray (not the z distance).
 *   For the offset (-14, 34) px: f = 193.0, world (0.35493, 0.14768, 1),
 *   depth 2.03597. In the triangle's own frame that is (-0.198, -0.300), inside
 *   the triangle by at least 0.1 world units. With the parent x scale set to 1
 *   the same point lies 0.07 outside the triangle, so the check can fail.
 */

const CANVAS = Object.freeze({ width: 320, height: 180 });
const CAMERA = Object.freeze({ x: 0.5, y: 0.5, z: 3, fov: 50 });
const PARENT = Object.freeze([-2, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0.5, 0.5, 1, 1]);
const LEAF_ROTATION_Z = Math.PI / 4;
const LOCAL_TRIANGLE = Object.freeze([[-0.4, -0.4], [0.4, -0.4], [0, 0.4]]);
// Offset of the pick from the canvas centre, in CSS pixels. It maps to the local
// point (-0.198, -0.300), inside the triangle and away from its origin.
const PICK_OFFSET = Object.freeze({ x: -14, y: 34 });

function multiply(a, b) {
  const out = new Array(16).fill(0);
  for (let c = 0; c < 4; c++) {
    for (let r = 0; r < 4; r++) {
      for (let k = 0; k < 4; k++) out[c * 4 + r] += a[k * 4 + r] * b[c * 4 + k];
    }
  }
  return out;
}

function rotationZ(angle) {
  const c = Math.cos(angle), s = Math.sin(angle);
  return [c, s, 0, 0, -s, c, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1];
}

// modelMatrix is parent * leaf rotation, the matrix the renderer submits.
function modelMatrix(parent) {
  return multiply(parent || PARENT, rotationZ(LEAF_ROTATION_Z));
}

function worldTriangle(model) {
  return LOCAL_TRIANGLE.map(([x, y]) => [
    model[0] * x + model[4] * y + model[12],
    model[1] * x + model[5] * y + model[13],
  ]);
}

// pickAt returns the point where the pointer ray crosses the triangle's plane,
// and the distance along the ray to it.
function pickAt(offset, planeZ) {
  const focal = (CANVAS.height / 2) / Math.tan(CAMERA.fov * Math.PI / 360);
  const dir = [offset.x / focal, -offset.y / focal, -1];
  const len = Math.hypot(dir[0], dir[1], dir[2]);
  const t = (CAMERA.z - planeZ) / (-dir[2] / len);
  return {
    world: [CAMERA.x + dir[0] / len * t, CAMERA.y + dir[1] / len * t, CAMERA.z + dir[2] / len * t],
    depth: t,
  };
}

// edgeDistances returns the signed distance from p to each triangle edge, with
// positive meaning inside.
function edgeDistances(p, tri) {
  const cross = (a, b, c) => (b[0] - a[0]) * (c[1] - a[1]) - (b[1] - a[1]) * (c[0] - a[0]);
  const sign = Math.sign(cross(tri[0], tri[1], tri[2]));
  return [0, 1, 2].map((i) => {
    const a = tri[i], b = tri[(i + 1) % 3];
    return sign * cross(a, b, p) / Math.hypot(b[0] - a[0], b[1] - a[1]);
  });
}

function expectedPick() {
  const hit = pickAt(PICK_OFFSET, PARENT[14]);
  return { world: hit.world, depth: hit.depth };
}

// check returns a list of problems; it is empty when the pixel covers the true
// triangle with at least 0.05 world units of margin and misses both triangles a
// renderer would draw if it dropped the parent scale (parent scale set to 1,
// and no parent at all).
function check() {
  const problems = [];
  const model = modelMatrix(PARENT);
  const wantModel = [-Math.SQRT2, Math.SQRT1_2, 0, 0, Math.SQRT2, Math.SQRT1_2, 0, 0, 0, 0, 1, 0, 0.5, 0.5, 1, 1];
  if (!wantModel.every((v, i) => Math.abs(v - model[i]) < 1e-12)) problems.push('model matrix differs from the submitted one: ' + model);
  const hit = pickAt(PICK_OFFSET, PARENT[14]);
  const p = [hit.world[0], hit.world[1]];
  const margin = 0.05;
  const real = edgeDistances(p, worldTriangle(modelMatrix(PARENT)));
  if (!real.every((d) => d > margin)) problems.push('pick is not inside the scaled triangle: ' + real);
  const unitScale = PARENT.slice();
  unitScale[0] = 1;
  const dropped = edgeDistances(p, worldTriangle(modelMatrix(unitScale)));
  if (dropped.every((d) => d > -margin)) problems.push('a pick that ignored the scale would also hit: ' + dropped);
  const noParent = edgeDistances(p, worldTriangle(modelMatrix([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1])));
  if (noParent.every((d) => d > -margin)) problems.push('a pick that ignored the parent would also hit: ' + noParent);
  const centre = edgeDistances([0.5, 0.5], worldTriangle(modelMatrix(unitScale)));
  if (!centre.every((d) => d > 0)) problems.push('self-check: the centre should be inside both');
  return problems;
}

module.exports = { CANVAS, CAMERA, PARENT, PICK_OFFSET, modelMatrix, worldTriangle, pickAt, expectedPick, edgeDistances, check };

if (require.main === module) {
  const problems = check();
  const e = expectedPick();
  console.log('expected world', e.world, 'depth', e.depth);
  if (problems.length) {
    console.error(problems.join('\n'));
    process.exit(1);
  }
  console.log('ok');
}
