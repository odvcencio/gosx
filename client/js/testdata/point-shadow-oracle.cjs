'use strict';

// point-shadow-oracle.cjs — PURE geometric oracle for the point-light shadow
// browser tests. Dependency-free (no require, no DOM, no production helpers,
// no shaders, no observed pixel data in mask selection). Consumed by
// scene3d-point-shadow-oracle.test.js and scene3d-point-shadow-browser.cjs.
//
// Canonical geometry (nz face): perspective camera at [0,0,4] looking down -z
// with vertical fov 50deg onto a 320x180 grid. Each pixel CENTER is
// unprojected onto the receiver front plane z = -0.45. A point light has NO
// cone cutoff, so masks are derived purely from segment/AABB obstruction.
//
// Robust mask contract (margins casterMargin/receiverMargin):
//   receiver        pixel inset receiverMargin from the extent border AND the
//                   camera ray to it clears the caster EXPANDED by margin.
//   interior        receiver pixel whose light->receiver segment is blocked
//                   by the caster SHRUNKEN by margin.
//   exterior        receiver pixel whose light segment clears the caster
//                   EXPANDED by margin (same camera/receiver conditions).
//   Pixels blocked by the expanded but not the shrunken caster lie between
//   the masks and are omitted deliberately (fixed-geometry safety band).
//
// All six fixture faces are rigid integer rotations (signed axis
// permutations, determinant +1) of this canonical geometry, so the same
// canonical masks apply to every face — but ONLY after verifyFixture has
// independently confirmed each face's permutation, camera euler, dimensions,
// and light positions against this module's pure rotation maps.

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
  casterMargin: 0.04,
  receiverMargin: 0.04,
  canonicalReceiverCenter: Object.freeze([0, 0, -0.5]),
  canonicalReceiverDims: Object.freeze([3, 2.2, 0.1]),
  canonicalCasterDims: Object.freeze([0.55, 0.55, 0.15]),
  basePosition: Object.freeze([-1.55, 1.35, 2.5]),
  movedPosition: Object.freeze([-0.95, 1.35, 2.5]), // base + x +0.6
  keyLight: Object.freeze({ intensity: 6, range: 6.5, decay: 2, shadowBias: 0.0001, shadowSize: 512 }),
  ambient: Object.freeze({ id: 'typed-point-ambient', intensity: 0.3 }),
  receiverID: 'typed-point-receiver',
  casterID: 'typed-point-caster',
  keyID: 'typed-point-key',
  blackPointID: 'typed-point-black',
  blackDirID: 'typed-point-black-dir',
});

// Signed integer permutations (det +1) mapping canonical nz-space coordinates
// onto each face's world space, plus the matching camera euler. These maps
// are the ONLY rotation authority; the fixture must agree with them exactly.
const FACE_ROT = Object.freeze({
  nz: Object.freeze({ rx: 0, ry: 0, f: (p) => [p[0], p[1], p[2]] }),
  pz: Object.freeze({ rx: 0, ry: Math.PI, f: (p) => [-p[0], p[1], -p[2]] }),
  px: Object.freeze({ rx: 0, ry: -Math.PI / 2, f: (p) => [-p[2], p[1], p[0]] }),
  nx: Object.freeze({ rx: 0, ry: Math.PI / 2, f: (p) => [p[2], p[1], -p[0]] }),
  py: Object.freeze({ rx: Math.PI / 2, ry: 0, f: (p) => [p[0], -p[2], p[1]] }),
  ny: Object.freeze({ rx: -Math.PI / 2, ry: 0, f: (p) => [p[0], p[2], -p[1]] }),
});
const FACE_NAMES = Object.freeze(Object.keys(FACE_ROT).sort());

function sub(a, b) { return [a[0] - b[0], a[1] - b[1], a[2] - b[2]]; }
function dot(a, b) { return a[0] * b[0] + a[1] * b[1] + a[2] * b[2]; }
function len(a) { return Math.sqrt(dot(a, a)); }
function normalize(a) { const n = len(a); return [a[0] / n, a[1] / n, a[2] / n]; }
const num = (v) => (typeof v === 'number' && Number.isFinite(v)) ? v : 0;
function vecEq(a, b, eps) {
  return Math.abs(a[0] - b[0]) <= eps && Math.abs(a[1] - b[1]) <= eps && Math.abs(a[2] - b[2]) <= eps;
}

// Rotate a dims triple: out[k] = dims[j] where face rot maps e_j to +/-e_k.
function rotateDims(R, dims) {
  const out = [0, 0, 0];
  for (let k = 0; k < 3; k++) {
    for (let j = 0; j < 3; j++) {
      const e = [0, 0, 0]; e[j] = 1;
      const v = R.f(e);
      if (Math.abs(v[k]) > 0.5) { out[k] = dims[j]; break; }
    }
  }
  return out;
}

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

function inExtent(p) {
  const { extentX, extentY } = ORACLE;
  return p[0] >= extentX[0] && p[0] <= extentX[1] && p[1] >= extentY[0] && p[1] <= extentY[1];
}

// buildMasks(light) -> { interior, exterior, receiver, receiverMargin,
// expandedShadow, shrunkenShadow }. Pixel indices only, chosen purely from
// geometry. A point light has no cone, so NO direction/angle is consulted.
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
      if (!(inX(p[0], xLo, xHi) && inX(p[1], yLo, yHi))) continue;
      if (rayHitsAABB(camera, sub(p, camera), c, halfExpanded)) continue;
      receiver.push(idx);
      if (inX(p[0], xLo2, xHi2) && inX(p[1], yLo2, yHi2)) receiverMargin.push(idx);
      if (!casts) { exterior.push(idx); continue; }
      const blockedShrunk = segmentBlocked(light.position, p, c, halfShrunk);
      const blockedExpanded = segmentBlocked(light.position, p, c, halfExpanded);
      if (blockedShrunk) shrunkenShadow.push(idx);
      if (blockedExpanded) expandedShadow.push(idx);
      if (blockedShrunk) interior.push(idx);
      else if (!blockedExpanded) exterior.push(idx);
      // else: between the margin sets — omitted deliberately.
    }
  }
  return { interior, exterior, receiver, receiverMargin, expandedShadow, shrunkenShadow };
}

// Symmetric difference of two masks' interior indices.
function changedFootprint(a, b) {
  const setA = new Set(a.interior);
  const setB = new Set(b.interior);
  const out = [];
  for (const idx of setA) if (!setB.has(idx)) out.push(idx);
  for (const idx of setB) if (!setA.has(idx)) out.push(idx);
  return out;
}

const findObj = (ir, id) => (ir.objects || []).find((o) => o && o.id === id) || null;
const findLight = (ir, id) => (ir.lights || []).find((l) => l && l.id === id) || null;
const pos3 = (o) => {
  if (Array.isArray(o)) return [num(o[0]), num(o[1]), num(o[2])];
  if (o && typeof o === 'object' && ('x' in o || 'y' in o || 'z' in o)) {
    return [num(o.x), num(o.y), num(o.z)];
  }
  return null;
};

function checkBox(errs, face, F, scene, id, wantPos, wantDims) {
  const ir = F.scenes && F.scenes[scene];
  const o = ir && findObj(ir, id);
  if (!o) { errs.push(face + '/' + scene + ': missing object ' + id); return; }
  if (!vecEq(pos3(o) || [0, 0, 0], wantPos, 1e-6)) errs.push(face + '/' + scene + ': ' + id + ' position');
  const dims = [num(o.width), num(o.height), num(o.depth)];
  if (!vecEq(dims, wantDims, 1e-9)) errs.push(face + '/' + scene + ': ' + id + ' dims ' + dims);
  const sortNum = (a) => a.slice().sort((x, y) => x - y);
  if (JSON.stringify(sortNum(dims)) !== JSON.stringify(sortNum(wantDims))) {
    errs.push(face + '/' + scene + ': ' + id + ' dims are not a permutation of canonical');
  }
}

// Verify a point light's kind, color, controls (intensity/range/decay/bias/
// size/softness), cast flag, and position. An omitted JSON castShadow must be
// accepted as false, so the flag is compared as (L.castShadow === true).
// No state is injected anywhere: read-only checks.
function checkKey(errs, face, F, scene, wantPos, wantCast) {
  const ir = F.scenes && F.scenes[scene];
  const L = ir && findLight(ir, ORACLE.keyID);
  if (!L) { errs.push(face + '/' + scene + ': missing key point light'); return; }
  const K = ORACLE.keyLight;
  if (L.kind !== 'point') errs.push(face + '/' + scene + ': key kind ' + L.kind);
  if (L.color !== '#ffffff') errs.push(face + '/' + scene + ': key color ' + L.color);
  if (num(L.intensity) !== K.intensity) errs.push(face + '/' + scene + ': key intensity ' + L.intensity);
  if (num(L.range) !== K.range) errs.push(face + '/' + scene + ': key range ' + L.range);
  if (L.decay === undefined || num(L.decay) !== K.decay) errs.push(face + '/' + scene + ': key decay');
  if (L.shadowBias === undefined || Math.abs(num(L.shadowBias) - K.shadowBias) > 1e-12) {
    errs.push(face + '/' + scene + ': key shadowBias');
  }
  if (L.shadowSize === undefined || num(L.shadowSize) !== K.shadowSize) {
    errs.push(face + '/' + scene + ': key shadowSize');
  }
  // The actual Go fixture emits shadowSoftness: 0 as OMITTED (omitempty), so
  // only undefined or exactly numeric 0 is accepted; any other present value
  // is rejected.
  if (!(L.shadowSoftness === undefined ||
        (typeof L.shadowSoftness === 'number' && L.shadowSoftness === 0))) {
    errs.push(face + '/' + scene + ': key shadowSoftness');
  }
  if ((L.castShadow === true) !== wantCast) errs.push(face + '/' + scene + ': key castShadow ' + L.castShadow);
  if (!vecEq(pos3(L) || [0, 0, 0], wantPos, 1e-6)) errs.push(face + '/' + scene + ': key position');
}

// Robustly validate the black probe lights: declared kind, black color,
// controls, point position, directional direction, and the authored light
// ordering of the three nz slot scenes.
function checkBlack(errs, face, F) {
  const bp = findLight(F.scenes.slot1, ORACLE.blackPointID);
  if (!bp) errs.push(face + ': slot1 missing black point light');
  else {
    if (bp.kind !== 'point') errs.push(face + ': black point kind ' + bp.kind);
    if (bp.color !== '#000000') errs.push(face + ': black point color ' + bp.color);
    if (num(bp.intensity) !== 0.5 || bp.castShadow !== true) errs.push(face + ': black point controls');
    if (!vecEq(pos3(bp) || [0, 0, 0], [2, 1, 1.5], 1e-6)) errs.push(face + ': black point position');
  }
  const bd = findLight(F.scenes['mixed-slot1'], ORACLE.blackDirID);
  if (!bd) errs.push(face + ': mixed-slot1 missing black directional light');
  else {
    if (bd.kind !== 'directional') errs.push(face + ': black directional kind ' + bd.kind);
    if (bd.color !== '#000000') errs.push(face + ': black directional color ' + bd.color);
    if (num(bd.intensity) !== 0.5 || bd.castShadow !== true) errs.push(face + ': black directional controls');
    if (!vecEq([num(bd.directionX), num(bd.directionY), num(bd.directionZ)], [0.7, -0.7, -1], 1e-6)) {
      errs.push(face + ': black directional direction');
    }
  }
  const order = (sn) => ((F.scenes[sn] || {}).lights || []).map((l) => l && l.id).join(',');
  const wantOrder = {
    slot1: [ORACLE.ambient.id, ORACLE.blackPointID, ORACLE.keyID],
    'slot0-paired': [ORACLE.ambient.id, ORACLE.keyID, ORACLE.blackPointID],
    'mixed-slot1': [ORACLE.ambient.id, ORACLE.blackDirID, ORACLE.keyID],
  };
  for (const sn of Object.keys(wantOrder)) {
    if (order(sn) !== wantOrder[sn].join(',')) errs.push(face + ': ' + sn + ' light order ' + order(sn));
  }
}

// verifyFixture(fx) — pure validation of the emitted typed fixture against
// this module's canonical geometry and rotation maps. Throws on any mismatch.
// Must run BEFORE any pixels are captured.
function verifyFixture(fx) {
  const errs = [];
  if (!fx || fx.schema !== 'gosx.point-shadow.fixture.v1' || !fx.faces) {
    throw new Error('unexpected fixture schema');
  }
  if (JSON.stringify(Object.keys(fx.faces).sort()) !== JSON.stringify(FACE_NAMES)) {
    errs.push('face set mismatch: ' + Object.keys(fx.faces).sort().join(','));
  }
  let sceneCount = 0;
  const baseScenes = ['off', 'on', 'ambient-only'];
  const nzExtras = ['no-caster', 'no-receiver', 'discarded', 'equal', 'moved', 'moved-off',
    'slot1', 'slot0-paired', 'mixed-slot1'];
  for (const face of FACE_NAMES) {
    const F = fx.faces[face];
    if (!F) continue;
    const R = FACE_ROT[face];
    // Exact scene inventory: base off/on/ambient-only on every face, the nine
    // extras ONLY on nz, nothing else, 27 scenes total.
    const names = Object.keys(F.scenes || {}).sort();
    const want = (face === 'nz' ? baseScenes.concat(nzExtras) : baseScenes.slice()).sort();
    if (JSON.stringify(names) !== JSON.stringify(want)) {
      errs.push(face + ': scene inventory mismatch: ' + names.join(','));
    }
    sceneCount += names.length;
    const cam = F.camera || {};
    if (!vecEq([num(cam.x), num(cam.y), num(cam.z)], R.f(ORACLE.camera), 1e-9)) errs.push(face + ': camera position');
    if (Math.abs(num(cam.rotationX) - R.rx) > 1e-9 || Math.abs(num(cam.rotationY) - R.ry) > 1e-9 ||
        num(cam.rotationZ) !== 0) errs.push(face + ': camera euler');
    if (num(cam.fov) !== 50) errs.push(face + ': camera fov');
    // Canonical receiver/caster/key geometry in EVERY scene: both boxes are
    // always present. The only allowed deviations are the intentional moved
    // key positions (moved/moved-off). The key light casts in every scene
    // that has it except off/moved-off. no-caster/no-receiver vary the
    // object shadow flags; discarded/equal vary caster material controls.
    for (const sn of names) {
      const S = F.scenes[sn] || {};
      const movedLight = sn === 'moved' || sn === 'moved-off';
      checkBox(errs, face, F, sn, ORACLE.receiverID, R.f(ORACLE.canonicalReceiverCenter),
        rotateDims(R, ORACLE.canonicalReceiverDims));
      checkBox(errs, face, F, sn, ORACLE.casterID, R.f(ORACLE.casterCenter),
        rotateDims(R, ORACLE.canonicalCasterDims));
      // Shadow flags: omitted JSON booleans normalize to false via ===true.
      const recv = findObj(F.scenes[sn], ORACLE.receiverID);
      if (recv && (recv.receiveShadow === true) !== (sn !== 'no-receiver')) {
        errs.push(face + '/' + sn + ': receiver receiveShadow ' + recv.receiveShadow);
      }
      const cast = findObj(F.scenes[sn], ORACLE.casterID);
      if (cast && (cast.castShadow === true) !== (sn !== 'no-caster')) {
        errs.push(face + '/' + sn + ': caster castShadow ' + cast.castShadow);
      }
      // Caster material controls: discarded fades the caster (opacity 0.25,
      // cutoff 0.5); equal matches the receiver (opacity 0.5, cutoff 0.5).
      if (sn === 'discarded' || sn === 'equal') {
        const wantOpacity = sn === 'discarded' ? 0.25 : 0.5;
        const c = cast || findObj(F.scenes[sn], ORACLE.casterID);
        if (c && (num(c.opacity) !== wantOpacity || num(c.alphaCutoff) !== 0.5)) {
          errs.push(face + '/' + sn + ': caster material opacity ' + c.opacity +
            ' cutoff ' + c.alphaCutoff);
        }
      }
      if (sn === 'ambient-only') {
        if (findLight(S, ORACLE.keyID)) errs.push(face + ': ambient-only must not contain the key light');
      } else {
        checkKey(errs, face, F, sn,
          movedLight ? R.f(ORACLE.movedPosition) : R.f(ORACLE.basePosition),
          sn !== 'off' && sn !== 'moved-off');
      }
    }
  }
  if (sceneCount !== 27) errs.push('expected 27 scenes total, got ' + sceneCount);
  const nz = fx.faces.nz;
  if (nz) {
    checkBlack(errs, 'nz', nz);
    const chain = ['off', 'on', 'moved', 'off'];
    if (!Array.isArray(nz.transitions) || nz.transitions.length !== 3) errs.push('nz transitions missing');
    else nz.transitions.forEach((t, i) => {
      if (!t || t.from !== chain[i] || t.to !== chain[i + 1] || !Array.isArray(t.commands) || !t.commands.length) {
        errs.push('nz transition ' + i + ' does not match off->on->moved->off');
      }
    });
  }
  if (errs.length) throw new Error('fixture verification failed: ' + errs.join('; '));
}

module.exports = {
  ORACLE, FACE_ROT, FACE_NAMES,
  sub, dot, len, normalize, unproject, slabInterval, rayHitsAABB, segmentBlocked,
  buildMasks, changedFootprint, verifyFixture, rotateDims, findLight, findObj, pos3,
};
