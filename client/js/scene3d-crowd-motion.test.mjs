import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

function context() {
  const c = vm.createContext({ console, Float32Array, Map, WeakMap, Set, Math });
  for (const p of ['bootstrap-src/11-scene-math.ts', '../runtime/scene3d/animation.ts']) {
    vm.runInContext(fs.readFileSync(new URL(p, import.meta.url), 'utf8'), c);
  }
  return c;
}

// A two-clip crowd atlas: "Walk" loops over 1s (30 segments), "Wave" is a
// one-shot 0.5s clip (15 segments). Row 0 is always the bind pose.
function asset() {
  return {
    nodes: [{ translation: [0, 0, 0], children: [1] }, { translation: [0, 1, 0] }],
    skins: [{ joints: [1], inverseBindMatrices: new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, -1, 0, 1]) }],
    animations: [
      { name: 'Walk', duration: 1, channels: [{ targetNode: 1, property: 'translation', times: new Float32Array([0, 1]), values: new Float32Array([0, 1, 0, 0, 3, 0]), interpolation: 'LINEAR' }] },
      { name: 'Wave', duration: .5, channels: [{ targetNode: 1, property: 'translation', times: new Float32Array([0, .5]), values: new Float32Array([0, 1, 0, 0, 2, 0]), interpolation: 'LINEAR' }] },
    ],
    objects: [{ skinIndex: 0, vertices: { count: 1, positions: new Float32Array([1, 0, 0]), joints: new Float32Array([0, 0, 0, 0]), weights: new Float32Array([1, 0, 0, 0]) } }],
  };
}

test('clip table reserves row 0 for the empty clip and orders real clips by atlas.clips insertion', () => {
  const c = context(), atlas = c.sceneBuildCrowdAtlas(asset(), 0);
  const table = c.sceneCrowdMotionClipTable(atlas);
  assert.equal(c.sceneCrowdMotionClipTable(atlas), table, 'cached, not rebuilt');
  assert.equal(table.count, 3); // sentinel + Walk + Wave
  assert.deepEqual(Array.from(table.data.subarray(0, 4)), [0, 0, 0, 0]);
  const walkRow = table.index.get('Walk'), waveRow = table.index.get('Wave');
  assert.equal(table.data[walkRow * 4 + 2], 1); // Walk duration
  assert.equal(table.data[waveRow * 4 + 2], .5); // Wave duration
  const walkClip = atlas.clips.get('Walk');
  assert.equal(table.data[walkRow * 4], walkClip.start);
  assert.equal(table.data[walkRow * 4 + 1], walkClip.segments);
});

test('clip index resolves known clips, warns once per unknown name, and defaults to the sentinel row', () => {
  const c = context(), atlas = c.sceneBuildCrowdAtlas(asset(), 0);
  assert.equal(c.sceneCrowdMotionClipIndex(atlas, ''), 0);
  const walkRow = c.sceneCrowdMotionClipIndex(atlas, 'Walk');
  assert.ok(walkRow > 0);
  let warnings = 0;
  c.console = { warn: () => warnings++ };
  assert.equal(c.sceneCrowdMotionClipIndex(atlas, 'Missing'), 0);
  assert.equal(c.sceneCrowdMotionClipIndex(atlas, 'Missing'), 0);
  assert.equal(warnings, 1);
});

// sceneCrowdMotionPoseRows must reproduce sceneCrowdPoseRows's rows/fraction
// for an equivalent (clip, elapsed-time, loop) input -- the explicit
// shader-vs-CPU parity the GPU-motion animation path depends on.
test('motion pose rows equal the CPU pose-rows path at clip boundaries, loops, and clamps', () => {
  const c = context(), atlas = c.sceneBuildCrowdAtlas(asset(), 0);
  const table = c.sceneCrowdMotionClipTable(atlas);
  const walkRow = c.sceneCrowdMotionClipIndex(atlas, 'Walk');
  const clipStartTime = 100; // an arbitrary nonzero scene-clock epoch
  const cases = [
    { t: 0, loop: false }, { t: .5, loop: false }, { t: 1, loop: false },
    { t: 1.5, loop: false }, // past duration, clamped
    { t: 1.5, loop: true },  // past duration, looped
    { t: 0.999, loop: true },
  ];
  for (const { t, loop } of cases) {
    const cpuRows = c.sceneCrowdPoseRows(atlas, { animation: 'Walk', animationTime: t, animationLoop: loop });
    const now = clipStartTime + t; // rate=1: elapsed == now - clipStartTime == t
    const gpuRows = c.sceneCrowdMotionPoseRows(table, walkRow, clipStartTime, loop, 1, now, new Float32Array(3));
    assert.deepEqual(Array.from(gpuRows), Array.from(cpuRows), `t=${t} loop=${loop}`);
  }
});

test('motion pose rows honor playback rate and clamp negative/nonfinite elapsed time to zero', () => {
  const c = context(), atlas = c.sceneBuildCrowdAtlas(asset(), 0);
  const table = c.sceneCrowdMotionClipTable(atlas);
  const walkRow = c.sceneCrowdMotionClipIndex(atlas, 'Walk');
  // rate=2 doubles elapsed time: now=10.25 with clipStartTime=10 is .25s of
  // wall time == .5s of clip time -- the same rows the CPU path produces for
  // animationTime=.5 on the un-rated clock.
  const doubled = c.sceneCrowdMotionPoseRows(table, walkRow, 10, false, 2, 10.25, new Float32Array(3));
  const atHalf = c.sceneCrowdPoseRows(atlas, { animation: 'Walk', animationTime: .5, animationLoop: false });
  assert.deepEqual(Array.from(doubled), Array.from(atHalf));
  // rate=2 at wall time .5s past clipStartTime reaches 1.0s of clip time,
  // clamped to the 1s clip duration (loop=false).
  const atFullRate = c.sceneCrowdMotionPoseRows(table, walkRow, 10, false, 2, 10.5, new Float32Array(3));
  const atDuration = c.sceneCrowdPoseRows(atlas, { animation: 'Walk', animationTime: 1, animationLoop: false });
  assert.deepEqual(Array.from(atFullRate), Array.from(atDuration));
  // now before clipStartTime (clock skew / a clip scheduled to start shortly
  // in the future): elapsed would be negative, clamps to zero, same as the
  // CPU path's `t < 0` guard.
  const early = c.sceneCrowdMotionPoseRows(table, walkRow, 10, false, 1, 9, new Float32Array(3));
  const atZero = c.sceneCrowdPoseRows(atlas, { animation: 'Walk', animationTime: 0, animationLoop: false });
  assert.deepEqual(Array.from(early), Array.from(atZero));
  // A negative playback rate flips the sign of elapsed; still clamped to zero
  // rather than propagating a negative value into the modulo.
  const reverse = c.sceneCrowdMotionPoseRows(table, walkRow, 10, false, -1, 10.5, new Float32Array(3));
  assert.deepEqual(Array.from(reverse), Array.from(atZero));
});

test('motion pose rows for the sentinel clip row (no animation) are always the bind pose', () => {
  const c = context(), atlas = c.sceneBuildCrowdAtlas(asset(), 0);
  const table = c.sceneCrowdMotionClipTable(atlas);
  const rows = c.sceneCrowdMotionPoseRows(table, 0, 0, false, 1, 12345, new Float32Array(3));
  assert.deepEqual(Array.from(rows), [0, 0, 0]);
});

test('angle lerp takes the shorter arc across the +/-PI wraparound and matches plain lerp away from it', () => {
  const c = context();
  // From 3.0 to -3.0 (~172 degrees apart the "short" way through PI, not
  // ~344 degrees the long way through 0).
  const wrapped = c.sceneCrowdMotionAngleLerp(3.0, -3.0, .5);
  assert.ok(Math.abs(wrapped) > 3.0, 'crossed through +/-PI, not through 0');
  // Symmetric endpoints: halfway must land exactly on the wrap boundary.
  assert.ok(Math.abs(Math.abs(wrapped) - Math.PI) < 1e-9);
  // Away from the wrap boundary, this is a plain lerp.
  assert.ok(Math.abs(c.sceneCrowdMotionAngleLerp(0, 1, .5) - .5) < 1e-12);
  assert.equal(c.sceneCrowdMotionAngleLerp(1, 2, 0), 1);
  assert.ok(Math.abs(c.sceneCrowdMotionAngleLerp(1, 2, 1) - 2) < 1e-12);
});

test('interpolation factor clamps before tPrev, interpolates linearly, and caps extrapolation past tNext', () => {
  const c = context();
  const f = (now) => c.sceneCrowdMotionInterpolationFactor(now, 10, 10.1, .25);
  assert.equal(f(9), 0);
  assert.equal(f(10), 0);
  assert.ok(Math.abs(f(10.05) - .5) < 1e-9);
  assert.ok(Math.abs(f(10.1) - 1) < 1e-9);
  // Extrapolating 0.05s past tNext over a 0.1s span adds 0.5 to t.
  assert.ok(Math.abs(f(10.15) - 1.5) < 1e-9);
  // Past the extrapolation cap, the factor stops growing (held).
  const capped = f(10.1 + .25), farPast = f(10.1 + 50);
  assert.ok(Math.abs(capped - farPast) < 1e-9);
});

test('interpolation factor is safe (finite, does not blow up interpolation) when tNext does not exceed tPrev', () => {
  const c = context();
  const t = c.sceneCrowdMotionInterpolationFactor(1000, 5, 5, .25);
  assert.ok(Number.isFinite(t));
  const record = new Float32Array(24);
  record[0] = 1; record[1] = 2; record[2] = 3; // prevPos
  record[9] = 5; // tPrev
  record[10] = 1; record[11] = 2; record[12] = 3; // nextPos == prevPos (single known sample)
  record[19] = 5; // tNext == tPrev
  record[6] = record[7] = record[8] = 1; record[16] = record[17] = record[18] = 1; // scale 1
  const out = new Float32Array(16);
  c.sceneCrowdMotionTransformInto(out, record, 1000, .25);
  assert.ok(Array.from(out).every(Number.isFinite));
  assert.ok(Math.abs(out[12] - 1) < 1e-6 && Math.abs(out[13] - 2) < 1e-6 && Math.abs(out[14] - 3) < 1e-6);
});

test('motion transform exactly reproduces the prev transform at t<=tPrev and the next transform at t>=tNext', () => {
  const c = context();
  const record = new Float32Array(24);
  // prev: pos(1,2,3) rot(.1,.2,.3) scale(1,1,1); next: pos(4,5,6) rot(.4,.5,.6) scale(2,2,2)
  record.set([1, 2, 3, .1, .2, .3, 1, 1, 1, 0, 4, 5, 6, .4, .5, .6, 2, 2, 2, 1, 0, 0, 0, 1]);
  const prevOut = new Float32Array(16), nextOut = new Float32Array(16);
  c.sceneCrowdMotionTransformInto(prevOut, record, 0, .25);
  c.sceneCrowdMotionTransformInto(nextOut, record, 1, .25);
  const expectPrev = c.sceneCrowdMotionComposeInto(new Float32Array(16), .1, .2, .3, 1, 1, 1, 1, 2, 3);
  const expectNext = c.sceneCrowdMotionComposeInto(new Float32Array(16), .4, .5, .6, 2, 2, 2, 4, 5, 6);
  for (let i = 0; i < 16; i++) {
    assert.ok(Math.abs(prevOut[i] - expectPrev[i]) < 1e-6, `prev[${i}]`);
    assert.ok(Math.abs(nextOut[i] - expectNext[i]) < 1e-6, `next[${i}]`);
  }
});

test('motion transform at the midpoint matches composing the midpoint-interpolated TRS directly', () => {
  const c = context();
  const record = new Float32Array(24);
  record.set([0, 0, 0, 0, 0, 0, 1, 1, 1, 0, 10, 0, 0, 0, Math.PI / 2, 0, 3, 3, 3, 2, 0, 0, 0, 1]);
  const out = new Float32Array(16);
  c.sceneCrowdMotionTransformInto(out, record, 1, .25); // t = (1-0)/(2-0) = .5
  const expected = c.sceneCrowdMotionComposeInto(new Float32Array(16), 0, Math.PI / 4, 0, 2, 2, 2, 5, 0, 0);
  for (let i = 0; i < 16; i++) assert.ok(Math.abs(out[i] - expected[i]) < 1e-5, `component ${i}`);
});

test('write record round-trips a decoded GSP3 instance at the documented offsets', () => {
  const c = context();
  const instance = {
    prevX: 1, prevY: 2, prevZ: 3, prevRotationX: .1, prevRotationY: .2, prevRotationZ: .3,
    prevScaleX: 1, prevScaleY: 1, prevScaleZ: 1, tPrev: 10,
    nextX: 4, nextY: 5, nextZ: 6, nextRotationX: .4, nextRotationY: .5, nextRotationZ: .6,
    nextScaleX: 2, nextScaleY: 2, nextScaleZ: 2, tNext: 10.1,
    clipStartTime: 9.5, animationLoop: true, playbackRate: 1.5,
  };
  const record = new Float32Array(24);
  c.sceneCrowdMotionWriteRecord(record, instance, 7);
  const expected = [1, 2, 3, .1, .2, .3, 1, 1, 1, 10, 4, 5, 6, .4, .5, .6, 2, 2, 2, 10.1, 7, 9.5, 1, 1.5].map(Math.fround);
  assert.deepEqual(Array.from(record), expected);
  const rows = c.sceneCrowdMotionPoseRowsFromRecord({ count: 8, data: new Float32Array(32).fill(0) }, record, 9.5, new Float32Array(3));
  assert.ok(Array.isArray(Array.from(rows)));
});

test('local radius is rotation-agnostic and swept bounds cover prev, next, and capped extrapolation overshoot', () => {
  const c = context();
  const localBounds = { minX: -1, maxX: 1, minY: -1, maxY: 1, minZ: -1, maxZ: 1 };
  const radius = c.sceneCrowdMotionLocalRadius(localBounds);
  assert.ok(Math.abs(radius - Math.sqrt(3)) < 1e-9);
  const record = new Float32Array(24);
  // prev at origin, next at (10,0,0), unit scale at both keys, tPrev=0 tNext=1.
  record.set([0, 0, 0, 0, 0, 0, 1, 1, 1, 0, 10, 0, 0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 0, 1]);
  const bounds = { minX: Infinity, minY: Infinity, minZ: Infinity, maxX: -Infinity, maxY: -Infinity, maxZ: -Infinity };
  c.sceneCrowdMotionSweptBoundsInto(bounds, record, radius);
  // extrapolationSeconds=.25 (SCENE_CROWD_MOTION_EXTRAPOLATION_SECONDS), span=tNext-tPrev=1.
  assert.ok(bounds.minX <= -radius + 1e-6);
  assert.ok(bounds.maxX >= 12.5 + radius - 1e-6);
  assert.ok(bounds.minY <= -radius + 1e-6 && bounds.maxY >= radius - 1e-6);
  // Larger scale at either key must widen the sweep further.
  const scaledRecord = new Float32Array(record);
  scaledRecord[16] = scaledRecord[17] = scaledRecord[18] = 4; // next scale 4x
  const scaledBounds = { minX: Infinity, minY: Infinity, minZ: Infinity, maxX: -Infinity, maxY: -Infinity, maxZ: -Infinity };
  c.sceneCrowdMotionSweptBoundsInto(scaledBounds, scaledRecord, radius);
  assert.ok(scaledBounds.maxX - scaledBounds.minX > bounds.maxX - bounds.minX);
});

test('crowd bundle carries motion records and their swept bounds to WebGL', () => {
  const sourceFile = 'bootstrap-src/10-runtime-scene-core.ts';
  const source = fs.readFileSync(new URL(sourceFile, import.meta.url), 'utf8');
  const start = source.indexOf('if (object._crowdSkin) {', source.indexOf('function appendSceneMeshObjectToBundle('));
  const end = source.indexOf('if (object.skin && vertices.joints', start);
  assert.ok(start > 0 && end > start);
  const sweptBounds = { minX: -3, minY: -2, minZ: -2, maxX: 14, maxY: 2, maxZ: 2 };
  const motion = { bounds: sweptBounds, record: new Float32Array(24) };
  const skin = { bounds: { minX: -1, minY: -1, minZ: -1, maxX: 1, maxY: 1, maxZ: 1 } };
  const bundle = { meshObjects: [], retainedMeshObjectCount: 0, retainedMeshVertexCount: 0,
    retainedGeometryTelemetry: { retained: 0 } };
  let transformed = 0;
  const c = vm.createContext({
    bundle, object: { id: 'actor', kind: 'mesh', _crowdSkin: skin, _crowdMotion: motion },
    vertices: { count: 3 }, camera: {}, timeSeconds: 0, objectPassString: 'opaque', materialIndex: 0,
    sceneObjectModelMatrix: () => new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]),
    sceneTransformMeshBounds: () => { transformed++; return {}; },
    sceneBoundsDepthMetrics: () => ({ near: 0, far: 1, center: .5 }),
    sceneStampRetainedMeshCSSInput: () => {},
  });
  vm.runInContext(`function probe() { ${source.slice(start, end)} }`, c);
  c.probe();
  assert.equal(bundle.meshObjects[0]._crowdMotion, motion);
  assert.equal(bundle.meshObjects[0].bounds, sweptBounds);
  assert.equal(transformed, 0, 'a motion batch does not use its old CPU matrix for bounds');
});
