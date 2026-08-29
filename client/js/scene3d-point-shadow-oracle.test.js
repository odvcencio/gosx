'use strict';

// Pure-oracle unit checks for point-shadow-oracle.cjs. No DOM, no browser, no
// production modules: the oracle must stay dependency-free and geometric.
const assert = require('assert');
const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');
const {
  ORACLE, FACE_ROT, buildMasks, changedFootprint, rayHitsAABB, segmentBlocked, verifyFixture,
} = require('./testdata/point-shadow-oracle.cjs');

// No production dependence: the oracle source must contain no require at all.
const src = fs.readFileSync(require.resolve('./testdata/point-shadow-oracle.cjs'), 'utf8');
assert.ok(!/require\(/.test(src), 'oracle must be dependency-free');

const base = { position: ORACLE.basePosition.slice(), castShadow: true };
const moved = { position: ORACLE.movedPosition.slice(), castShadow: true };
const mOn = buildMasks(base);
const mMoved = buildMasks(moved);

// Masks nonempty and structurally sound.
assert.ok(mOn.interior.length >= 20, 'interior nonempty: ' + mOn.interior.length);
assert.ok(mOn.exterior.length >= 50, 'exterior nonempty: ' + mOn.exterior.length);
assert.ok(mOn.receiver.length >= mOn.receiverMargin.length, 'receiverMargin within receiver');
const iSet = new Set(mOn.interior);
for (const i of mOn.exterior) assert.ok(!iSet.has(i), 'interior/exterior disjoint');
const rSet = new Set(mOn.receiver);
for (const i of mOn.receiverMargin) assert.ok(rSet.has(i), 'receiverMargin subset of receiver');

// Moved light changes the robust interior footprint.
assert.ok(changedFootprint(mOn, mMoved).length > 0, 'moved mask changes interior footprint');

// verifyFixture must accept the REAL fixture emitted by the actual Go
// lowering (bounded go run, no hand-authored duplicate), then corrupted
// copies must be rejected.
function runGoFixture() {
  const r = spawnSync('go', ['run', './client/js/testdata/point-shadow-typed-fixture'],
    { cwd: path.join(__dirname, '..', '..'), timeout: 60000, maxBuffer: 8 * 1024 * 1024, encoding: 'utf8' });
  if (r.status !== 0) throw new Error('fixture build failed: ' + (r.stderr || r.stdout || 'exit ' + r.status));
  return JSON.parse(r.stdout);
}
const fx = runGoFixture();
verifyFixture(fx);

// Negative case 1: broken scene inventory must fail verification.
const badInventory = JSON.parse(JSON.stringify(fx));
delete badInventory.faces.px.scenes.off;
assert.throws(() => verifyFixture(badInventory), /fixture verification failed/,
  'missing scene must fail fixture verification');

// Negative case 2: corrupted key-light cast/control values must fail
// verification (omitted castShadow is false, but a wrong intensity is an error).
const badControls = JSON.parse(JSON.stringify(fx));
badControls.faces.nz.scenes.on.lights.find((l) => l.id === ORACLE.keyID).intensity = 7;
assert.throws(() => verifyFixture(badControls), /fixture verification failed/,
  'corrupt key controls must fail fixture verification');

// Negative case 3: corrupted cast-shadow flags on scene objects must fail
// verification (the on scene must have receiver.receiveShadow and
// caster.castShadow both true; omitted is false via ===true normalization).
const badCastFlags = JSON.parse(JSON.stringify(fx));
badCastFlags.faces.nz.scenes.on.objects.find((o) => o.id === ORACLE.casterID).castShadow = false;
assert.throws(() => verifyFixture(badCastFlags), /fixture verification failed/,
  'corrupt caster castShadow must fail fixture verification');

// Ray/segment AABB boundaries.
assert.strictEqual(rayHitsAABB([0, 0, 0], [1, 0, 0], [5, 0, 0], [1, 1, 1]), true, 'ray hit');
assert.strictEqual(rayHitsAABB([0, 0, 0], [-1, 0, 0], [5, 0, 0], [1, 1, 1]), false, 'ray miss');
assert.strictEqual(segmentBlocked([4, 5, 4], [5, 5, 4], [5, 5, 4], [1, 1, 1]), true, 'endpoint inside hits');
assert.strictEqual(segmentBlocked([0, 0, 0], [1, 0, 0], [5, 0, 0], [1, 1, 1]), false, 'short segment misses');
assert.strictEqual(segmentBlocked([0, 0, 0], [10, 0, 0], [5, 0, 0], [1, 1, 1]), true, 'through segment hits');

// Rotations are integer signed permutations mapping the canonical camera to
// each face's camera; masks therefore transfer to all faces only after
// verifyFixture confirms the fixture matches these same maps.
const CAMS = { nz: [0, 0, 4], pz: [0, 0, -4], px: [-4, 0, 0], nx: [4, 0, 0], py: [0, -4, 0], ny: [0, 4, 0] };
for (const face of Object.keys(FACE_ROT)) {
  const R = FACE_ROT[face];
  const seen = new Set();
  for (const axis of [[1, 0, 0], [0, 1, 0], [0, 0, 1]]) {
    const v = R.f(axis);
    const hits = v.filter((x) => Math.abs(x) > 1e-9);
    assert.strictEqual(hits.length, 1, face + ' maps axis to axis');
    assert.ok(Math.abs(Math.abs(hits[0]) - 1) < 1e-9, face + ' preserves axis length');
    seen.add(v.findIndex((x) => Math.abs(x) > 1e-9));
  }
  assert.strictEqual(seen.size, 3, face + ' is an axis permutation');
  assert.ok(JSON.stringify(R.f(ORACLE.camera)) === JSON.stringify(CAMS[face]), face + ' camera rotation');
}
assert.strictEqual(typeof verifyFixture, 'function');

console.log('point-shadow oracle unit tests passed');
