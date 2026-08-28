'use strict';

// Node tests for the pure geometric spot-shadow oracle helpers.
// Run: node --test client/js/scene3d-spot-shadow-oracle.test.js

const test = require('node:test');
const assert = require('node:assert');
const oracle = require('./testdata/spot-shadow-oracle.cjs');
const { ORACLE, unproject, segmentBlocked, insideCone, buildMasks, changedFootprint } = oracle;

function occlusionSet(light) {
  // Occlusion-only footprint (ignores the cone) for same-position comparison.
  const set = new Set();
  const { width: W, height: H } = ORACLE;
  for (let j = 0; j < H; j++) {
    for (let i = 0; i < W; i++) {
      const p = unproject(i, j);
      if (p[0] < ORACLE.extentX[0] || p[0] > ORACLE.extentX[1] ||
          p[1] < ORACLE.extentY[0] || p[1] > ORACLE.extentY[1]) continue;
      if (segmentBlocked(light.position, p, ORACLE.casterCenter, ORACLE.casterHalf)) {
        set.add(j * W + i);
      }
    }
  }
  return set;
}

test('known ray hits and misses the caster AABB', () => {
  const c = ORACLE.casterCenter;
  const h = ORACLE.casterHalf;
  // Extend the actual light -> caster-center line to the receiver plane
  // z = -0.45: light [-1.55,1.35,2.5] + t*[1,-1,-2] with t = 2.95/2 = 1.475.
  assert.ok(segmentBlocked(ORACLE.base.position, c, c, h), 'light -> caster center must hit');
  const endpoint = [-0.075, -0.125, -0.45];
  assert.ok(segmentBlocked(ORACLE.base.position, endpoint, c, h), 'light->castercenter line extended to receiverZ must hit');
  assert.ok(!segmentBlocked(ORACLE.base.position, [-0.55, 0.35, -0.45], c, h),
    'vertical drop endpoint is NOT on the light->castercenter ray and must miss');
  assert.ok(!segmentBlocked(ORACLE.base.position, [1.4, -1.0, -0.45], c, h), 'far corner ray must miss');
});

test('cone membership for authored directions', () => {
  const target = [-0.075, -0.125, -0.45]; // light->castercenter line at receiverZ
  assert.ok(insideCone(ORACLE.base, target), 'base cone covers its aimed target');
  assert.ok(insideCone(ORACLE.aimed, target), 'aimed cone still covers the caster region');
  const behind = [-2.55, 2.35, 4.5]; // light position minus one base direction
  assert.ok(!insideCone(ORACLE.base, behind), 'point behind the light outside base cone');
  assert.ok(!insideCone(ORACLE.aimed, behind), 'point behind the light outside aimed cone');
  assert.ok(!insideCone(ORACLE.aimed, [-0.4806, -0.5306, -0.45]),
    'aimed cone cuts off the far shadow corner that base covers');
});

test('front plane unprojection lands on z = -0.45 with sane centers', () => {
  const { width: W, height: H } = ORACLE;
  for (const [i, j] of [[0, 0], [W >> 1, H >> 1], [W - 1, H - 1]]) {
    const p = unproject(i, j);
    assert.strictEqual(p[2], ORACLE.receiverZ);
  }
  const center = unproject((W - 1) / 2, (H - 1) / 2);
  assert.ok(Math.abs(center[0]) < 1e-9 && Math.abs(center[1]) < 1e-9, 'center pixel ~ world origin');
  const first = unproject(0, 0);
  assert.ok(first[0] < 0 && first[1] > 0, 'top-left pixel is upper-left in world');
});

test('base, moved and aimed masks are nonvacuous', () => {
  for (const name of ['base', 'moved', 'aimed']) {
    const masks = buildMasks(ORACLE[name]);
    assert.ok(masks.interior.length >= 20, `${name} interior >= 20, got ${masks.interior.length}`);
    assert.ok(masks.exterior.length >= 50, `${name} exterior >= 50, got ${masks.exterior.length}`);
    const interSet = new Set(masks.interior);
    const exterSet = new Set(masks.exterior);
    for (const idx of interSet) assert.ok(!exterSet.has(idx), `${name} interior/exterior must be disjoint`);
    const recvSet = new Set(masks.receiver);
    for (const idx of interSet) assert.ok(recvSet.has(idx), `${name} interior must be a receiver subset`);
    for (const idx of exterSet) assert.ok(recvSet.has(idx), `${name} exterior must be a receiver subset`);
    const m = ORACLE.receiverMargin;
    for (const idx of masks.receiver) {
      const p = oracle.unproject(idx % ORACLE.width, Math.floor(idx / ORACLE.width));
      assert.ok(p[0] > ORACLE.extentX[0] + m && p[0] < ORACLE.extentX[1] - m &&
        p[1] > ORACLE.extentY[0] + m && p[1] < ORACLE.extentY[1] - m,
        `${name} receiver pixel must be inset by receiverMargin`);
      assert.ok(!oracle.rayHitsAABB(ORACLE.camera, oracle.sub(p, ORACLE.camera),
        ORACLE.casterCenter, ORACLE.casterHalf.map((v) => v + ORACLE.casterMargin)),
        `${name} receiver pixel must be camera-clear of the expanded caster`);
    }
    const shrinkSet = new Set(masks.shrunkenShadow);
    const expandSet = new Set(masks.expandedShadow);
    for (const idx of interSet) assert.ok(expandSet.has(idx), `${name} interior ⊆ expandedShadow`);
    for (const idx of interSet) assert.ok(shrinkSet.has(idx), `${name} interior ⊆ shrunkenShadow`);
    for (const idx of shrinkSet) assert.ok(expandSet.has(idx), `${name} shrunkenShadow ⊆ expandedShadow`);
    for (const idx of masks.receiverMargin) assert.ok(recvSet.has(idx), `${name} receiverMargin ⊆ receiver`);
    assert.ok(masks.shrunkenShadow.length > 0, `${name} shrunken shadow nonempty`);
    assert.ok(expandSet.size > interSet.size, `${name} expanded/interior margin band is nonempty`);
  }
});

test('moving the light changes the shadow footprint', () => {
  const base = buildMasks(ORACLE.base);
  const moved = buildMasks(ORACLE.moved);
  assert.ok(changedFootprint(base, moved).length > 0, 'moved footprint must differ from base');
});

test('re-aiming changes the cone but not physical occlusion', () => {
  const baseOcc = occlusionSet(ORACLE.base);
  const aimedOcc = occlusionSet(ORACLE.aimed);
  assert.strictEqual(baseOcc.size, aimedOcc.size, 'same position: occlusion size equal');
  for (const idx of baseOcc) assert.ok(aimedOcc.has(idx), 'occlusion sets identical ignoring cone');
  const base = buildMasks(ORACLE.base);
  const aimed = buildMasks(ORACLE.aimed);
  assert.ok(changedFootprint(base, aimed).length > 0, 'cone restriction still changes lit interior');
  const baseSet = new Set(base.interior);
  const aimedSet = new Set(aimed.interior);
  for (const idx of aimedSet) assert.ok(baseSet.has(idx), 'aimed interior is a subset of base interior');
  assert.ok(baseSet.size > aimedSet.size, 'aimed cone strictly shrinks the shadowed interior');
});

test('receiver mask excludes camera-visible caster pixels', () => {
  const masks = buildMasks(ORACLE.base);
  const { width: W, camera } = ORACLE;
  const receiverSet = new Set(masks.receiver);
  for (let j = 0; j < ORACLE.height; j++) {
    for (let i = 0; i < ORACLE.width; i++) {
      const p = unproject(i, j);
      for (const half of [ORACLE.casterHalf, ORACLE.casterHalf.map((v) => v + ORACLE.casterMargin)]) {
        if (!oracle.rayHitsAABB(camera, oracle.sub(p, camera), ORACLE.casterCenter, half)) continue;
        assert.ok(!receiverSet.has(j * W + i), 'camera-visible caster pixel must be excluded from receiver');
        break;
      }
    }
  }
});
