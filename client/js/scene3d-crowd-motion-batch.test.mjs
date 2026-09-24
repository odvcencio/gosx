import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

const directory = path.dirname(fileURLToPath(import.meta.url));
const requireRuntime = createRequire(new URL('../runtime/package.json', import.meta.url));
const ts = requireRuntime('typescript');

// rendererSourceText reads a runtime/scene3d source by name through a
// variable, not a literal readFileSync argument -- scene3d-renderer-
// architecture.test.js's scanDirectRendererReads forbids embedding a
// protected source's filename directly in a read/load call so every
// consumer is easy to grep for; mirrors scene3d-crowd-animation.test.mjs's
// functionSource helper, which reads the same way.
function rendererSourceText(sourceFile) {
  return fs.readFileSync(path.join(directory, '..', 'runtime', 'scene3d', sourceFile), 'utf8');
}

// Extracts the "GPU-driven crowd motion batch binding" section of webgl.ts
// (prepareCrowdAtlas through finishCrowdMotionBatch): prepareCrowdMotionRecords
// and crowdMotionStreamBuffer live there and close over prepareCrowdAtlas,
// uploadInstancedStream, and touchInstancedStream, which this stubs.
function crowdMotionBatchSource() {
  const source = rendererSourceText('webgl.ts');
  const start = source.indexOf('function prepareCrowdAtlas(');
  const end = source.indexOf('const rigidBatchRecords = new Map();', start);
  if (start < 0 || end < start) throw new Error('crowd motion batch source markers not found');
  return source.slice(start, end);
}

function harness() {
  const uploads = [];
  const touches = [];
  const buffers = new Map();
  const context = vm.createContext({
    gl: {}, // prepareCrowdAtlas is stubbed below and never touches gl directly here.
    SCENE_CROWD_MOTION_MAX_CLIPS: 32, // declared earlier in webgl.ts, outside this extracted range.
    crowdAtlasTextures: new Map(),
    crowdProgram: null, crowdShadowProgram: null, crowdFrame: 0,
    crowdPaletteUploads: 0, crowdPaletteBytes: 0,
    crowdCapabilities: null, crowdCapabilityError: null,
    createScenePBRInstancedProgram: () => ({}), createSceneShadowProgram: () => ({}),
    createScenePBRCrowdMotionProgram: () => ({}), createSceneShadowMotionProgram: () => ({}),
    bindScenePBRDirectAttribute: () => true,
    uploadInstancedStream: (mesh, index, slot, data, activeLength) => {
      uploads.push({ mesh, slot, length: activeLength });
      const buffer = { slot };
      buffers.set(`${mesh.id}:${slot}`, buffer);
      return buffer;
    },
    touchInstancedStream: (mesh, index, slot) => {
      const buffer = buffers.get(`${mesh.id}:${slot}`) || null;
      if (buffer) touches.push({ mesh, slot });
      return buffer;
    },
  });
  vm.runInContext(ts.transpileModule(crowdMotionBatchSource(), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  context.prepareCrowdAtlas = () => {}; // Already covered by scene3d-crowd-animation.test.mjs.
  return { context, uploads, touches };
}

const STRIDE = 24;

function motionRecord(clipRow = 1) {
  const record = new Float32Array(STRIDE);
  record[20] = clipRow;
  return record;
}

function object(clipRow = 1) {
  return { _crowdMotion: { record: motionRecord(clipRow), dirty: true } };
}

test('prepareCrowdMotionRecords copies dirty instances, clears dirty, and advances the generation once', () => {
  const { context } = harness();
  const a = object(1), b = object(2);
  const batch = { atlas: {}, objects: [a, b], count: 2, motion: null };
  context.prepareCrowdMotionRecords(batch);
  assert.equal(a._crowdMotion.dirty, false);
  assert.equal(b._crowdMotion.dirty, false);
  assert.equal(batch.motion[20], 1);
  assert.equal(batch.motion[STRIDE + 20], 2);
  assert.equal(batch._motionGeneration, 1);
});

test('prepareCrowdMotionRecords is a no-op (no copy, no generation bump) when nothing is dirty', () => {
  const { context } = harness();
  const a = object(1);
  const batch = { atlas: {}, objects: [a], count: 1, motion: null };
  context.prepareCrowdMotionRecords(batch);
  const generationAfterFirst = batch._motionGeneration;
  batch.motion[0] = 999; // sentinel: would be overwritten by a real re-copy
  context.prepareCrowdMotionRecords(batch);
  assert.equal(batch._motionGeneration, generationAfterFirst);
  assert.equal(batch.motion[0], 999);
});

test('prepareCrowdMotionRecords copies only the instance a later frame marked dirty', () => {
  const { context } = harness();
  const a = object(1), b = object(2);
  const batch = { atlas: {}, objects: [a, b], count: 2, motion: null };
  context.prepareCrowdMotionRecords(batch);
  const generationAfterFirst = batch._motionGeneration;
  a._crowdMotion.record[20] = 9;
  a._crowdMotion.dirty = true;
  context.prepareCrowdMotionRecords(batch);
  assert.equal(batch.motion[20], 9);
  assert.equal(batch.motion[STRIDE + 20], 2); // untouched instance keeps its prior value
  assert.ok(batch._motionGeneration > generationAfterFirst);
});

test('prepareCrowdMotionRecords resyncs everything when membership order changes even with no dirty flags', () => {
  const { context } = harness();
  const a = object(1), b = object(2);
  const batch = { atlas: {}, objects: [a, b], count: 2, motion: null };
  context.prepareCrowdMotionRecords(batch);
  const generationAfterFirst = batch._motionGeneration;
  a._crowdMotion.dirty = false; b._crowdMotion.dirty = false;
  batch.objects = [b, a]; // swapped order, same objects, same count -- a real membership change
  context.prepareCrowdMotionRecords(batch);
  assert.equal(batch.motion[20], 2); // b is now first
  assert.equal(batch.motion[STRIDE + 20], 1); // a is now second
  assert.ok(batch._motionGeneration > generationAfterFirst);
});

test('prepareCrowdMotionRecords resyncs everything when the buffer grows past capacity', () => {
  const { context } = harness();
  const objects = Array.from({ length: 40 }, (_v, i) => object(i));
  const batch = { atlas: {}, objects, count: 40, motion: null };
  context.prepareCrowdMotionRecords(batch);
  assert.ok(batch.motion.length >= 40 * STRIDE);
  for (let i = 0; i < 40; i++) objects[i]._crowdMotion.dirty = false;
  for (let i = 0; i < 40; i++) assert.equal(batch.motion[i * STRIDE + 20], i);
});

test('crowdMotionStreamBuffer uploads on the first bind and on every generation change, but not in between', () => {
  const { context, uploads, touches } = harness();
  const a = object(1);
  const batch = { id: 'crowd-motion-1', atlas: {}, objects: [a], count: 1, motion: null };
  context.prepareCrowdMotionRecords(batch);
  context.crowdMotionStreamBuffer(batch);
  assert.equal(uploads.length, 1);
  assert.equal(touches.length, 0);

  // Steady-state frame: nothing changed since the last prepare/bind.
  context.prepareCrowdMotionRecords(batch);
  context.crowdMotionStreamBuffer(batch);
  assert.equal(uploads.length, 1, 'no re-upload for an unchanged batch');
  assert.equal(touches.length, 1, 'the GPU buffer is still touched so it is not retired');

  // A new snapshot marks the instance dirty again: exactly one more upload.
  a._crowdMotion.dirty = true;
  context.prepareCrowdMotionRecords(batch);
  context.crowdMotionStreamBuffer(batch);
  assert.equal(uploads.length, 2);
  assert.equal(touches.length, 2);
});

test('motion stream uploads one dirty instance range and keeps its GPU storage', () => {
  const { context } = harness();
  const source = rendererSourceText('webgl.ts');
  const start = source.indexOf('function uploadInstancedStream(');
  const end = source.indexOf('function bindInstancedVertexAttribute(', start);
  assert.ok(start > 0 && end > start);
  const calls = [];
  let sequence = 0;
  context.gl = {
    ARRAY_BUFFER: 1, DYNAMIC_DRAW: 2,
    createBuffer: () => ({ id: ++sequence }),
    bindBuffer: () => {},
    bufferData: (_target, data) => calls.push({ kind: 'allocate', bytes: typeof data === 'number' ? data : data.byteLength }),
    bufferSubData: (_target, offset, data) => calls.push({ kind: 'upload', offset, bytes: data.byteLength }),
    deleteBuffer: () => {},
  };
  context.instancedStreamRecords = new Map();
  context.instancedStreamEpoch = 1;
  context.pointsEntryBuffers = new Set();
  vm.runInContext(ts.transpileModule(source.slice(start, end), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  const objects = Array.from({ length: 3 }, (_v, i) => object(i + 1));
  const batch = { id: 'crowd-motion-range', atlas: {}, objects, count: 3, motion: null };
  context.prepareCrowdMotionRecords(batch);
  const first = context.crowdMotionStreamBuffer(batch);
  assert.deepEqual(calls.filter(c => c.kind === 'upload').map(c => [c.offset, c.bytes]), [[0, 3 * STRIDE * 4]]);
  assert.equal(calls.filter(c => c.kind === 'allocate').length, 1);
  calls.length = 0;
  context.prepareCrowdMotionRecords(batch);
  assert.equal(context.crowdMotionStreamBuffer(batch), first);
  assert.deepEqual(calls, []);
  objects[1]._crowdMotion.record[20] = 9;
  objects[1]._crowdMotion.dirty = true;
  context.prepareCrowdMotionRecords(batch);
  assert.equal(context.crowdMotionStreamBuffer(batch), first);
  assert.deepEqual(calls, [{ kind: 'upload', offset: STRIDE * 4, bytes: STRIDE * 4 }]);
});

test('bindCrowdMotionBatch fails closed when the atlas clip table exceeds the shader budget', () => {
  const { context } = harness();
  context.crowdMotionNow = 0;
  const batch = { atlas: {}, clipTable: { count: 999, data: new Float32Array(4) }, objects: [{ vertices: {} }], count: 1, motion: new Float32Array(STRIDE) };
  const ip = { uniforms: {}, attributes: {} };
  assert.throws(() => context.bindCrowdMotionBatch(batch, ip, {}), /budget exceeded/);
});
