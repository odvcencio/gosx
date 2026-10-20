import test from 'node:test';
import assert from 'node:assert/strict';
import { crowdContext, syntheticRig } from './scene3d-crowd-bounds-fixture.mjs';

test('detailed multi-joint geometry exceeding obsolete vertex-row estimate fits actual bounds work', () => {
  const { context: c } = crowdContext();
  const asset = syntheticRig({ jointCount: 32, vertexCount: 130000, duration: 9, clipCount: 1 });
  const atlas = c.sceneBuildCrowdAtlas(asset, 0);
  assert.ok(130000 * atlas.height > 32 * 1024 * 1024);
  assert.ok(130000 * 4 + atlas.height * 32 * 8 < 32 * 1024 * 1024);
  assert.equal(atlas.primitives.size, 1);
  assert.ok(Object.values(atlas.primitives.get(asset.objects[0].vertices).bounds).every(Number.isFinite));
});

test('actual joint-corner budget is cumulative and rejects before corner transforms', () => {
  const { context: c } = crowdContext();
  const asset = syntheticRig({ jointCount: 2, vertexCount: 5 });
  const atlas = c.sceneBuildCrowdAtlas(asset, 0);
  const vertices = atlas.primitives.get(asset.objects[0].vertices).vertices;
  const work = atlas.height * 2 * 8;
  const exact = { work: 32 * 1024 * 1024 - work };
  assert.doesNotThrow(() => c.sceneCrowdPrimitiveBounds(vertices, 2, atlas.data, atlas.width, atlas.height, vertices.weights, exact));
  assert.equal(exact.work, 32 * 1024 * 1024);
  const exhausted = { work: exact.work - work + 1 };
  const unreadPalette = new Proxy({}, { get() { throw new Error('corner transform reached'); } });
  assert.throws(() => c.sceneCrowdPrimitiveBounds(vertices, 2, unreadPalette, atlas.width, atlas.height, vertices.weights, exhausted), /bounds preprocessing budget exceeded/);
  assert.throws(() => c.sceneCrowdChargeBoundsWork({ work: 0 }, 32 * 1024 * 1024 + 1), /budget exceeded/);
});

test('one cumulative asset-skin budget charges every unique primitive scan and active-joint palette row', () => {
  const { context: c } = crowdContext();
  const asset = syntheticRig({ jointCount: 8, vertexCount: 128, primitiveCount: 3 });
  asset.objects.push({ ...asset.objects[0] });
  const charges = [], charge = c.sceneCrowdChargeBoundsWork;
  c.sceneCrowdChargeBoundsWork = (budget, work) => {
    const prior = budget.work;
    charge(budget, work);
    charges.push({ budget, work, prior, result: budget.work });
  };
  const atlas = c.sceneBuildCrowdAtlas(asset, 0);
  assert.equal(charges.length, 6, 'duplicate decoded vertex streams have no additional scan/corner charge');
  let total = 0;
  for (let i = 0; i < charges.length; i++) {
    const expected = i % 2 ? 8 * 8 * atlas.height : 128 * 4;
    assert.equal(charges[i].budget, charges[0].budget);
    assert.equal(charges[i].work, expected);
    assert.equal(charges[i].prior, total);
    total += expected;
    assert.equal(charges[i].result, total);
  }
});

test('atlas builder charges all four four-influence loops before stream allocation and does not cache failure', () => {
  const { context: c } = crowdContext();
  const asset = syntheticRig();
  const original = asset.objects[0].vertices;
  const count = 32 * 1024 * 1024 / 4 + 1;
  const unread = new Proxy({ length: count * 4 }, { get(target, key) {
    if (key === 'length') return target.length;
    throw new Error('stream allocation reached');
  } });
  asset.objects[0].vertices = { count, positions: unread, joints: unread, weights: unread };
  assert.throws(() => c.sceneBuildCrowdAtlas(asset, 0), /bounds preprocessing budget exceeded/);
  asset.objects[0].vertices = original;
  assert.doesNotThrow(() => c.sceneBuildCrowdAtlas(asset, 0));
});

test('shared primitive streams preprocess once and four heroes plus 181 enemies reuse all geometry/palettes', () => {
  const { context: c, allocations } = crowdContext();
  const assets = [3, 7, 19, 32, 64].map(jointCount => syntheticRig({ jointCount, primitiveCount: 3 }));
  assets[0].objects.push({ ...assets[0].objects[0] });
  const atlases = assets.map(a => c.sceneBuildCrowdAtlas(a, 0));
  assert.equal(atlases[0].primitives.size, 3);
  const originals = assets.map(a => a.objects.map(o => o.vertices));
  const wrappers = atlases.map(a => Array.from(a.primitives.values(), p => p.vertices));
  const rows = new c.Float32Array(3), pose = { animation: 'clip-0', animationTime: .5, animationLoop: true };
  const before = allocations();
  for (let frame = 0; frame < 120; frame++) for (let actor = 0; actor < 185; actor++) {
    const i = actor < 4 ? actor : 4;
    const atlas = c.sceneBuildCrowdAtlas(assets[i], 0);
    assert.equal(atlas, atlases[i]);
    assert.equal(c.sceneCrowdPoseRows(atlas, pose, rows), rows);
    for (let p = 0; p < 3; p++) {
      const primitive = atlas.primitives.get(originals[i][p]);
      assert.equal(primitive.vertices, wrappers[i][p]);
      assert.equal(primitive.vertices.positions, originals[i][p].positions);
      assert.equal(primitive.vertices.joints, originals[i][p].joints);
      assert.equal(primitive.vertices.revision, 0);
      assert.equal(primitive.vertices.immutable, true);
    }
  }
  assert.equal(allocations(), before, 'steady atlas/pose lookups allocate no Float32Array');
});

test('sparse influences charge only active joints; malformed stream counts still fail truthfully', () => {
  const { context: c } = crowdContext();
  const asset = syntheticRig({ jointCount: 64, vertexCount: 5 });
  asset.objects[0].vertices.joints.fill(63);
  const atlas = c.sceneBuildCrowdAtlas(asset, 0);
  const primitive = atlas.primitives.values().next().value;
  const budget = { work: 0 };
  c.sceneCrowdPrimitiveBounds(primitive.vertices, 64, atlas.data, atlas.width, atlas.height, primitive.vertices.weights, budget);
  assert.equal(budget.work, atlas.height * 8);
  for (const count of [NaN, Infinity, -1, 1.5]) {
    const malformed = syntheticRig();
    malformed.objects[0].vertices.count = count;
    assert.throws(() => c.sceneBuildCrowdAtlas(malformed, 0), /invalid crowd bind streams/);
  }
});

test('cloned assets and separate skins sharing primitive streams retain their own joint mapping and inverse binds', () => {
  const { context: c } = crowdContext();
  const asset = syntheticRig({ jointCount: 2 });
  const original = c.sceneBuildCrowdAtlas(asset, 0);
  const secondSkin = { joints: [2, 1], inverseBindMatrices: new Float32Array(asset.skins[0].inverseBindMatrices) };
  secondSkin.inverseBindMatrices[12] = 100;
  secondSkin.inverseBindMatrices[28] = 100;
  asset.skins.push(secondSkin);
  asset.objects.push({ skinIndex: 1, vertices: asset.objects[0].vertices });
  const otherSkin = c.sceneBuildCrowdAtlas(asset, 1);
  const clone = { ...asset, skins: [secondSkin], objects: [asset.objects[0]] };
  const otherAsset = c.sceneBuildCrowdAtlas(clone, 0);
  const key = asset.objects[0].vertices;
  assert.notEqual(original, otherSkin);
  assert.notEqual(original, otherAsset);
  assert.notEqual(original.data[12], otherSkin.data[12]);
  assert.notEqual(original.primitives.get(key).vertices, otherSkin.primitives.get(key).vertices);
  assert.notEqual(original.primitives.get(key).bounds, otherAsset.primitives.get(key).bounds);
  assert.ok(otherSkin.primitives.get(key).bounds.maxX > original.primitives.get(key).bounds.maxX + 50);
  assert.equal(otherAsset.primitives.get(key).bounds.maxX, otherSkin.primitives.get(key).bounds.maxX);
  assert.equal(original.primitives.get(key).vertices.positions, key.positions);
});

test('varied rigs conservatively contain weighted vertices throughout palette interpolation', () => {
  const { context: c } = crowdContext();
  for (const jointCount of [1, 2, 7, 32, 64]) {
    const asset = syntheticRig({ jointCount, vertexCount: 19, primitiveCount: 2, duration: .17 });
    const atlas = c.sceneBuildCrowdAtlas(asset, 0);
    for (const primitive of atlas.primitives.values()) {
      const v = primitive.vertices, b = primitive.bounds;
      for (let row = 0; row < atlas.height - 1; row++) for (const t of [0, .37, 1]) {
        for (let i = 0; i < v.count; i++) {
          const p = [0, 0, 0];
          let sum = 0;
          for (let k = 0; k < 4; k++) {
            const w = v.weights[i * 4 + k];
            if (!w) continue;
            sum += w;
            const o = row * atlas.width * 4 + v.joints[i * 4 + k] * 16;
            const matrix = n => atlas.data[o + n] * (1 - t) + atlas.data[o + atlas.width * 4 + n] * t;
            for (let axis = 0; axis < 3; axis++) p[axis] += w * (matrix(axis) * v.positions[i * 3] + matrix(axis + 4) * v.positions[i * 3 + 1] + matrix(axis + 8) * v.positions[i * 3 + 2] + matrix(axis + 12));
          }
          if (!sum) for (let axis = 0; axis < 3; axis++) p[axis] = v.positions[i * 3 + axis];
          for (const [axis, label] of ['X', 'Y', 'Z'].entries()) assert.ok(p[axis] >= b['min' + label] && p[axis] <= b['max' + label], `${jointCount} joints row ${row} axis ${label}`);
        }
      }
    }
  }
});
