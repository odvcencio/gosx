// Run with: nice -n 10 node client/js/scene3d-crowd-motion.bench.mjs <baseline-ref>
// Measures authored CPU preparation and mocked WebGL upload bytes at 60 Hz.
// A motion snapshot arrives every third frame. One shadow and one color pass
// bind each batch. GPU driver and shader execution time are outside this test.
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import { createRequire } from 'node:module';
import path from 'node:path';
import { performance } from 'node:perf_hooks';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const baselineRef = process.argv[2] || 'origin/main';
const requireRuntime = createRequire(path.join(root, 'client/runtime/package.json'));
const ts = requireRuntime('typescript');
const files = {
  animation: 'client/runtime/scene3d/animation.ts',
  webgl: 'client/runtime/scene3d/webgl.ts',
  core: 'client/js/bootstrap-src/10-runtime-scene-core.ts',
};
function readSource(file, before) {
  return before
    ? execFileSync('git', ['show', `${baselineRef}:${file}`], { cwd: root, encoding: 'utf8' })
    : fs.readFileSync(path.join(root, file), 'utf8');
}
function section(source, first, last) {
  const start = source.indexOf(first);
  const end = source.indexOf(last, start + first.length);
  if (start < 0 || end < start) throw new Error(`missing source section: ${first}`);
  return source.slice(start, end);
}
function evaluate(context, source) {
  vm.runInContext(ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
}
function setup(before) {
  const animation = readSource(files.animation, before);
  const webgl = readSource(files.webgl, before);
  const core = readSource(files.core, before);
  const uploads = { bytes: 0, calls: 0 };
  let bufferID = 0;
  const gl = {
    ARRAY_BUFFER: 1, DYNAMIC_DRAW: 2,
    createBuffer: () => ++bufferID, bindBuffer() {}, deleteBuffer() {},
    bufferData(_target, data) {
      if (typeof data !== 'number') { uploads.bytes += data.byteLength; uploads.calls++; }
    },
    bufferSubData(_target, _offset, data) { uploads.bytes += data.byteLength; uploads.calls++; },
  };
  const context = vm.createContext({
    console, Float32Array, Map, WeakMap, Set, Math, Number, gl,
    instancedStreamRecords: new Map(), instancedStreamEpoch: 1,
    pointsEntryBuffers: new Set(),
    _sceneObjectModelMatrixCache: new WeakMap(),
    _objectMatrixOriginScratch: {}, _objectMatrixXScratch: {},
    _objectMatrixYScratch: {}, _objectMatrixZScratch: {},
    sceneNumber: (value, fallback) => Number.isFinite(value) ? value : fallback,
    translateScenePointInto: (out, x, y, z, object) => {
      out.x = object.x + x; out.y = object.y + y; out.z = object.z + z;
    },
    prepareCrowdAtlas() {},
    SCENE_CROWD_MOTION_RECORD_FLOATS: 24,
  });
  evaluate(context, animation);
  evaluate(context, section(webgl, 'function uploadInstancedStream(', 'function bindInstancedVertexAttribute('));
  if (before) {
    evaluate(context, section(core, 'function sceneObjectModelMatrix(', 'function sceneObjectMeshBakeLinearState('));
  } else {
    evaluate(context, section(webgl, 'function prepareCrowdMotionRecords(', 'function bindCrowdMotionBatch('));
  }
  return { context, uploads };
}

function run(before, count) {
  const { context: c, uploads } = setup(before);
  const atlas = { clips: new Map([['Run', { start: 1, segments: 30, duration: 1 }]]), missingClips: new Set() };
  let frame;
  if (before) {
    const objects = Array.from({ length: count }, (_unused, i) => ({
      x: i * .1, y: 0, z: 0, rotationX: 0, rotationY: i * .01, rotationZ: 0,
      scaleX: 1, scaleY: 1, scaleZ: 1,
    }));
    const transforms = new Float32Array(count * 16);
    const poses = new Float32Array(count * 3);
    const pose = { animation: 'Run', animationTime: 0, animationLoop: true };
    const rows = new Float32Array(3);
    const batch = { id: `legacy-${count}` };
    frame = index => {
      const t = index / 60;
      pose.animationTime = t;
      for (let i = 0; i < count; i++) {
        const object = objects[i];
        object.x = i * .1 + t;
        object.rotationY = i * .01 + t * .1;
        transforms.set(c.sceneObjectModelMatrix(object, t), i * 16);
        poses.set(c.sceneCrowdPoseRows(atlas, pose, rows), i * 3);
      }
      c.instancedStreamEpoch++;
      for (let pass = 0; pass < 2; pass++) {
        c.uploadInstancedStream(batch, 0, 'transforms', transforms, count * 16);
        c.uploadInstancedStream(batch, 0, 'poses', poses, count * 3);
      }
    };
  } else {
    const clipTable = c.sceneCrowdMotionClipTable(atlas);
    const instances = Array.from({ length: count }, (_unused, i) => ({
      prevX: i * .1, prevY: 0, prevZ: 0, prevRotationX: 0, prevRotationY: i * .01, prevRotationZ: 0,
      prevScaleX: 1, prevScaleY: 1, prevScaleZ: 1, tPrev: 0,
      nextX: i * .1, nextY: 0, nextZ: 0, nextRotationX: 0, nextRotationY: i * .01, nextRotationZ: 0,
      nextScaleX: 1, nextScaleY: 1, nextScaleZ: 1, tNext: 1 / 20,
      animation: 'Run', clipStartTime: 0, animationLoop: true, playbackRate: 1,
    }));
    const objects = instances.map(() => ({ _crowdMotion: {
      record: new Float32Array(24), dirty: true,
      bounds: { minX: Infinity, minY: Infinity, minZ: Infinity, maxX: -Infinity, maxY: -Infinity, maxZ: -Infinity },
    } }));
    const batch = { id: `motion-${count}`, atlas, clipTable, objects, count, motion: null };
    frame = index => {
      if (index % 3 === 0) {
        const t = index / 60;
        for (let i = 0; i < count; i++) {
          const instance = instances[i], motion = objects[i]._crowdMotion;
          instance.prevX = i * .1 + t;
          instance.nextX = instance.prevX + 1 / 20;
          instance.prevRotationY = i * .01 + t * .1;
          instance.nextRotationY = instance.prevRotationY + .005;
          instance.tPrev = t;
          instance.tNext = t + 1 / 20;
          const clipRow = c.sceneCrowdMotionClipIndex(atlas, instance.animation);
          c.sceneCrowdMotionWriteRecord(motion.record, instance, clipRow);
          const bounds = motion.bounds;
          bounds.minX = bounds.minY = bounds.minZ = Infinity;
          bounds.maxX = bounds.maxY = bounds.maxZ = -Infinity;
          c.sceneCrowdMotionSweptBoundsInto(bounds, motion.record, 1);
          motion.dirty = true;
        }
      }
      c.instancedStreamEpoch++;
      c.prepareCrowdMotionRecords(batch);
      c.crowdMotionStreamBuffer(batch);
      c.crowdMotionStreamBuffer(batch);
    };
  }
  for (let i = -40; i < 0; i++) frame(i);
  uploads.bytes = uploads.calls = 0;
  const start = performance.now();
  for (let i = 0; i < 600; i++) frame(i);
  return { ms: (performance.now() - start) / 600, bytes: uploads.bytes / 600, calls: uploads.calls / 600 };
}

console.log(`baseline=${baselineRef}; 60 frames/s; 20 snapshots/s; one shadow pass and one color pass`);
for (const count of [200, 500, 1000]) {
  const beforeSamples = [], afterSamples = [];
  for (let sample = 0; sample < 5; sample++) {
    beforeSamples.push(run(true, count));
    afterSamples.push(run(false, count));
  }
  beforeSamples.sort((a, b) => a.ms - b.ms);
  afterSamples.sort((a, b) => a.ms - b.ms);
  const before = beforeSamples[2], after = afterSamples[2];
  console.log(`${count} instances: before ${before.ms.toFixed(3)} ms/frame, ${before.bytes.toFixed(0)} upload B/frame; after ${after.ms.toFixed(3)} ms/frame, ${after.bytes.toFixed(0)} upload B/frame`);
}
