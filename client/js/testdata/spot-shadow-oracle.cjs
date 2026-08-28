'use strict';

// spot-shadow-oracle.cjs — PURE geometric oracle helpers for the spot-shadow
// browser tests. Independent of production helpers and shaders: no imports,
// no DOM, no observed pixel data in mask selection. Consumed by
// scene3d-spot-shadow-oracle.test.js and (next task) the browser test.
//
// Geometry: perspective camera at ORACLE.camera looking down -z with
// vertical fov ORACLE.fovY onto a W x H pixel grid. Each pixel CENTER is
// unprojected onto the receiver front plane z = ORACLE.receiverZ. A segment
// from the light to that receiver point is tested against the caster AABB
// (slab method) to decide obstruction; cone membership requires
// dot(normalize(direction), normalize(point - position)) > cos(angle - 0.06).
//
// Robust mask contract (margins ORACLE.casterMargin / ORACLE.receiverMargin):
//   receiver  pixel inset receiverMargin from the extent border AND the
//             camera ray to it clears the caster expanded by casterMargin.
//   interior  receiver pixel inside the cone AND blocked by the caster
//             SHRUNKEN by casterMargin.
//   exterior  receiver pixel inside the cone AND NOT blocked by the caster
//             EXPANDED by casterMargin.
//   Pixels blocked by the expanded but not the shrunken caster lie between
//   the masks and are omitted deliberately (fixed-geometry safety band).

const DEG = Math.PI / 180;

const ORACLE = Object.freeze({
  camera: Object.freeze([0, 0, 4]),
  fovY: 50 * DEG,
  width: 320,
  height: 180,
  receiverZ: -0.45, // receiver box center z=-0.5, depth 0.1 -> front face
  extentX: Object.freeze([-1.5, 1.5]),
  extentY: Object.freeze([-1.1, 1.1]),
  casterCenter: Object.freeze([-0.55, 0.35, 0.5]),
  casterHalf: Object.freeze([0.275, 0.275, 0.075]),
  coneMargin: 0.06, // radians subtracted from the authored half-angle
  casterMargin: 0.04, // units for expanded/shrunken caster margin sets
  receiverMargin: 0.04, // units inset from the receiver extent border
  base: Object.freeze({ position: [-1.55, 1.35, 2.5], direction: [1, -1, -2], angle: 0.75, castShadow: true }),
  moved: Object.freeze({ position: [-0.95, 1.35, 2.5], direction: [1, -1, -2], angle: 0.75, castShadow: true }),
  // [3.6, -0.8, -2] tilts the cone ~0.61 rad from base so its boundary cuts
  // through the left side of the caster shadow (the [-0.48,-0.53] shadow
  // corner falls outside the cone) while the rest stays inside. The previous
  // [1.6,-0.9,-2] (and [4,0.5,-2]) left the whole shadow in-cone / in a
  // grazing sliver, changing no robust interior.
  aimed: Object.freeze({ position: [-1.55, 1.35, 2.5], direction: [3.6, -0.8, -2], angle: 0.75, castShadow: true }),
});

function sub(a, b) { return [a[0] - b[0], a[1] - b[1], a[2] - b[2]]; }
function dot(a, b) { return a[0] * b[0] + a[1] * b[1] + a[2] * b[2]; }
function len(a) { return Math.sqrt(dot(a, a)); }
function normalize(a) { const n = len(a); return [a[0] / n, a[1] / n, a[2] / n]; }

// Unproject the CENTER of pixel (i, j) onto the receiver front plane.
function unproject(i, j) {
  const { camera, fovY, width: W, height: H, receiverZ } = ORACLE;
  const halfH = Math.tan(fovY / 2);
  const halfW = halfH * (W / H);
  const dx = ((2 * (i + 0.5)) / W - 1) * halfW;
  const dy = (1 - (2 * (j + 0.5)) / H) * halfH;
  const t = (receiverZ - camera[2]) / -1; // ray direction is [dx, dy, -1]
  return [camera[0] + dx * t, camera[1] + dy * t, receiverZ];
}

// Slab interval [tmin, tmax] of ray o + s*d against the AABB (c +- h), or null.
function slabInterval(o, d, c, h) {
  let tmin = -Infinity;
  let tmax = Infinity;
  for (let k = 0; k < 3; k++) {
    if (Math.abs(d[k]) < 1e-12) {
      if (Math.abs(o[k] - c[k]) > h[k]) return null;
      continue;
    }
    let t1 = (c[k] - h[k] - o[k]) / d[k];
    let t2 = (c[k] + h[k] - o[k]) / d[k];
    if (t1 > t2) { const tmp = t1; t1 = t2; t2 = tmp; }
    tmin = Math.max(tmin, t1);
    tmax = Math.min(tmax, t2);
    if (tmin > tmax) return null;
  }
  return [tmin, tmax];
}

// True when the infinite ray from o along d hits the AABB.
function rayHitsAABB(o, d, c, h) {
  const t = slabInterval(o, d, c, h);
  return t !== null && t[1] >= 0;
}

// True when the closed segment p -> q intersects the AABB.
function segmentBlocked(p, q, c, h) {
  const t = slabInterval(p, sub(q, p), c, h);
  return t !== null && t[1] >= 0 && t[0] <= 1;
}

// Cone membership with the -0.06 rad margin.
function insideCone(light, point) {
  const toPoint = sub(point, light.position);
  return dot(normalize(light.direction), normalize(toPoint)) > Math.cos(light.angle - ORACLE.coneMargin);
}

function inExtent(p) {
  const { extentX, extentY } = ORACLE;
  return p[0] >= extentX[0] && p[0] <= extentX[1] && p[1] >= extentY[0] && p[1] <= extentY[1];
}

// buildMasks(light) -> { interior, exterior, receiver, receiverMargin,
// expandedShadow, shrunkenShadow }. All values are pixel indices (j*width+i),
// selected purely from geometry; no observed pixel data is consulted here.
// See the module-header contract. Nesting guarantees:
//   receiverMargin ⊆ receiver, interior ⊆ shrunkenShadow ⊆ expandedShadow,
//   interior ∩ exterior = ∅.
function buildMasks(light) {
  const { width: W, height: H, camera, casterCenter: c, casterHalf: h } = ORACLE;
  const interior = [];
  const exterior = [];
  const receiver = [];
  const receiverMargin = [];
  const expandedShadow = [];
  const shrunkenShadow = [];
  const halfExpanded = h.map((v) => v + ORACLE.casterMargin);
  const halfShrunk = h.map((v) => Math.max(0.01, v - ORACLE.casterMargin));
  const casts = light.castShadow !== false;
  const m = ORACLE.receiverMargin;
  const inX = (x, lo, hi) => x > lo && x < hi;
  const xLo = ORACLE.extentX[0] + m, xHi = ORACLE.extentX[1] - m;
  const yLo = ORACLE.extentY[0] + m, yHi = ORACLE.extentY[1] - m;
  const xLo2 = ORACLE.extentX[0] + 2 * m, xHi2 = ORACLE.extentX[1] - 2 * m;
  const yLo2 = ORACLE.extentY[0] + 2 * m, yHi2 = ORACLE.extentY[1] - 2 * m;
  for (let j = 0; j < H; j++) {
    for (let i = 0; i < W; i++) {
      const idx = j * W + i;
      const p = unproject(i, j);
      if (!inExtent(p)) continue;
      // Receiver: inset from the extent border and camera-clear of the
      // EXPANDED caster (margin for the caster silhouette edge).
      if (!(inX(p[0], xLo, xHi) && inX(p[1], yLo, yHi))) continue;
      if (rayHitsAABB(camera, sub(p, camera), c, halfExpanded)) continue;
      receiver.push(idx);
      if (inX(p[0], xLo2, xHi2) && inX(p[1], yLo2, yHi2)) receiverMargin.push(idx);
      if (!casts) { exterior.push(idx); continue; }
      const blockedShrunk = segmentBlocked(light.position, p, c, halfShrunk);
      const blockedExpanded = segmentBlocked(light.position, p, c, halfExpanded);
      if (blockedShrunk) shrunkenShadow.push(idx);
      if (blockedExpanded) expandedShadow.push(idx);
      if (insideCone(light, p)) {
        if (blockedShrunk) interior.push(idx);
        else if (!blockedExpanded) exterior.push(idx);
        // else: between the margin sets — omitted deliberately.
      }
    }
  }
  return { interior, exterior, receiver, receiverMargin, expandedShadow, shrunkenShadow };
}

// Symmetric difference of two masks' interior indices (moved/aimed footprint
// comparison utility).
function changedFootprint(a, b) {
  const setA = new Set(a.interior);
  const setB = new Set(b.interior);
  const out = [];
  for (const idx of setA) if (!setB.has(idx)) out.push(idx);
  for (const idx of setB) if (!setA.has(idx)) out.push(idx);
  return out;
}

module.exports = {
  ORACLE,
  sub,
  dot,
  normalize,
  unproject,
  rayHitsAABB,
  segmentBlocked,
  insideCone,
  buildMasks,
  changedFootprint,
};
