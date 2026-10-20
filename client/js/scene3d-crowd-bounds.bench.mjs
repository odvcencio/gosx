// CPU-only authored-asset proof. Supply full GLB paths; no browser or GPU.
// Node VM timing includes boundary overhead and is not a native render budget.
// node --expose-gc client/js/scene3d-crowd-bounds.bench.mjs <full.glb> [...]
import fs from 'node:fs';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { performance } from 'node:perf_hooks';
import { crowdContext, decodedGLB } from './scene3d-crowd-bounds-fixture.mjs';

const paths = process.argv.slice(2);
assert.ok(paths.length, 'supply authored GLB paths');
const { context: c, allocations } = crowdContext({ decode: true });
const before = crowdContext({ baseline: '9c748bbe484298c077c04468f7ac321d4d635c3c' }).context;
const results = [], animated = [];
for (const path of paths) {
  const decodeStart = performance.now();
  const asset = decodedGLB(c, path);
  const decodeMS = performance.now() - decodeStart;
  const result = { path, sha256: createHash('sha256').update(fs.readFileSync(path)).digest('hex'), decodeMS, skins: [] };
  for (let skinIndex = 0; skinIndex < asset.skins.length; skinIndex++) {
    const start = performance.now();
    const atlas = c.sceneBuildCrowdAtlas(asset, skinIndex);
    const coldMS = performance.now() - start;
    let obsoleteWork = 0, actualWork = 0;
    for (const [vertices, primitive] of atlas.primitives) {
      obsoleteWork += vertices.count * atlas.height;
      const active = new Set();
      for (let i = 0; i < vertices.count * 4; i++) if (primitive.vertices.weights[i]) active.add(vertices.joints[i]);
      actualWork += vertices.count * 4 + active.size * 8 * atlas.height;
      assert.equal(primitive.vertices.positions, vertices.positions);
      assert.equal(primitive.vertices.joints, vertices.joints);
      assert.ok(Object.values(primitive.bounds).every(Number.isFinite));
    }
    let previousGuard = 'accepted';
    try { before.sceneBuildCrowdAtlas(asset, skinIndex); }
    catch (error) { previousGuard = error.message; }
    result.skins.push({ skinIndex, joints: asset.skins[skinIndex].joints.length, rows: atlas.height, primitives: atlas.primitives.size, paletteBytes: atlas.data.byteLength, obsoleteWork, actualWork, coldMS, previousGuard });
    animated.push({ asset, skinIndex, atlas, primitives: Array.from(atlas.primitives.values()), pose: { animation: atlas.clips.keys().next().value, animationTime: .5, animationLoop: true }, rows: new c.Float32Array(3) });
  }
  results.push(result);
}
const heroSource = /\/public\/art\/hero-polish-[^/]+\//;
const heroes = animated.filter(x => heroSource.test(x.asset.src));
const enemies = animated.filter(x => !heroSource.test(x.asset.src));
const boss = enemies.find(x => /\/boss\//.test(x.asset.src));
assert.ok(heroes.length >= 4 && enemies.length && boss, 'include four heroes, enemies and boss');
const normalEnemies = enemies.filter(x => x !== boss);
const actors = [...heroes.slice(0, 4), ...Array.from({ length: 180 }, (_, i) => normalEnemies[i % normalEnemies.length]), boss];
global.gc?.();
const heapBefore = process.memoryUsage().heapUsed, arrayBefore = allocations();
const warmStart = performance.now();
for (let frame = 0; frame < 1200; frame++) for (const actor of actors) {
  actor.pose.animationTime = frame / 60;
  assert.equal(c.sceneBuildCrowdAtlas(actor.asset, actor.skinIndex), actor.atlas);
  assert.equal(c.sceneCrowdPoseRows(actor.atlas, actor.pose, actor.rows), actor.rows);
}
const warmMS = performance.now() - warmStart;
global.gc?.();
assert.equal(allocations(), arrayBefore);
for (const actor of animated) assert.deepEqual(Array.from(actor.atlas.primitives.values()), actor.primitives);
console.log(JSON.stringify({ scope: 'CPU decoded full GLBs; four heroes + 180 mixed enemies + boss; not GPU/full-frame proof', boundsLimit: 32 * 1024 * 1024, results, warm: { actors: actors.length, frames: 1200, totalMS: warmMS, msPerFrame: warmMS / 1200, float32Allocations: allocations() - arrayBefore, retainedHeapDelta: process.memoryUsage().heapUsed - heapBefore, palettesAndPrimitiveIdentitiesPreserved: true }, rigidColdAssets: results.filter(r => !r.skins.length).map(r => ({ path: r.path, sha256: r.sha256, decodeMS: r.decodeMS })) }, null, 2));
