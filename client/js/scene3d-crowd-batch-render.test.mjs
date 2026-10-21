import test from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import { createRequire } from 'node:module';

// Renderer side of crowd batch records: prepareCrowdBatchRecord keeps one
// persistent batch per record, repacks it only when the record's revision
// changes, culls it as a whole, and uploadPackedStream uploads only after a
// repack. The functions are cut out of the WebGL renderer source and run against stubs.
const ts = createRequire(new URL('../runtime/package.json', import.meta.url))('typescript');
const { readSceneRendererBackendSrc } = createRequire(import.meta.url)('./scene3d-renderer-source-set.js');
const STRIDE = 24;

function between(source, first, last) {
  const start = source.indexOf(first);
  const end = source.indexOf(last, start);
  if (start < 0 || end < start) throw new Error(`source markers not found: ${first}`);
  return source.slice(start, end);
}

function harness() {
  const source = readSceneRendererBackendSrc('webgl');
  const uploads = [];
  const streams = new Map();
  const context = vm.createContext({
    gl: {}, Float32Array, Math, Number, Array, Map,
    SCENE_CROWD_MOTION_MAX_CLIPS: 32,
    sceneNumber: (value, fallback) => (Number.isFinite(value) ? value : fallback),
    crowdAtlasTextures: new Map(), crowdProgram: null, crowdShadowProgram: null, crowdFrame: 0,
    crowdPaletteUploads: 0, crowdPaletteBytes: 0, crowdCapabilities: null, crowdCapabilityError: null,
    createScenePBRInstancedProgram: () => ({}), createSceneShadowProgram: () => ({}),
    createScenePBRCrowdMotionProgram: () => ({}), createSceneShadowMotionProgram: () => ({}),
    bindScenePBRDirectAttribute: () => true,
    prebuiltRigidBatchRecords: new Map(), prebuiltRigidBatchEpoch: 7,
    meshColorVisibility: new Map(), rigidObjectBatches: new Map(),
    uploadInstancedStream: (mesh, index, slot, data, activeLength) => {
      uploads.push({ id: mesh.id, slot, length: activeLength });
      const buffer = { id: mesh.id, slot };
      streams.set(`${mesh.id}:${slot}`, buffer);
      return buffer;
    },
    touchInstancedStream: (mesh, index, slot) => streams.get(`${mesh.id}:${slot}`) || null,
    atlasPrepared: 0,
  });
  const code = [
    between(source, 'function prepareCrowdAtlas(', 'const rigidBatchRecords = new Map();'),
    between(source, 'function uploadPackedStream(', 'function touchInstancedStream('),
    between(source, 'function boundsOutsideSidePlanes(', 'function prepareRigidMeshBatches('),
  ].join('\n');
  vm.runInContext(ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  context.prepareCrowdAtlas = () => { context.atlasPrepared++; };
  return { context, uploads, streams };
}

// Four side planes that keep x in [-10, 10]: nx*x + d >= 0.
const planes = [[1, 0, 0, 10], [-1, 0, 0, 10], [0, 0, 1, 1000], [0, 0, -1, 1000]];
const inside = { minX: -1, minY: 0, minZ: 0, maxX: 1, maxY: 1, maxZ: 1 };
const outside = { minX: 50, minY: 0, minZ: 0, maxX: 52, maxY: 1, maxZ: 1 };

function motionAgent(row) {
  const record = new Float32Array(STRIDE);
  record[20] = row;
  return { id: `a${row}`, _crowdMotion: { record, dirty: true } };
}

function poseRecord(count, revision) {
  return {
    id: 'crowd-batch-1', _crowdBatch: true, crowdMotion: false, crowdAtlas: { id: 'atlas' }, crowdClipTable: null,
    crowdMembers: Array.from({ length: count }, (_v, i) => ({ id: `a${i}` })), crowdShape: 1,
    instanceCount: count, crowdRevision: revision, bounds: inside,
    crowdTransforms: new Float32Array(count * 16), crowdPoses: new Float32Array(count * 3),
  };
}

function motionRecordFor(members, revision, shape = 1) {
  return {
    id: 'crowd-batch-2', _crowdBatch: true, crowdMotion: true, crowdAtlas: { id: 'atlas' }, crowdClipTable: { count: 1 },
    crowdMembers: members, crowdShape: shape, instanceCount: members.length, crowdRevision: revision, bounds: inside,
  };
}

test('a pose-mode record borrows its streams and keeps one persistent batch', () => {
  const { context } = harness();
  const record = poseRecord(3, 1);
  context.prepareCrowdBatchRecord(record, planes);
  const batch = context.prebuiltRigidBatchRecords.get(record.id);
  assert.equal(batch.count, 3);
  assert.equal(batch.transforms, record.crowdTransforms, 'the renderer uploads the record\'s own stream');
  assert.equal(batch.poses, record.crowdPoses);
  assert.equal(batch.objects[0], record);
  assert.equal(batch.motion, null);
  assert.equal(batch.epoch, 7);
  assert.equal(context.rigidObjectBatches.get(record), batch);
  assert.equal(context.meshColorVisibility.get(record), true);
  const generation = batch._packGeneration;

  context.prepareCrowdBatchRecord(record, planes);
  assert.equal(context.prebuiltRigidBatchRecords.get(record.id), batch, 'the batch persists across frames');
  assert.equal(batch._packGeneration, generation, 'an unchanged revision repacks nothing');

  record.crowdRevision = 2;
  context.prepareCrowdBatchRecord(record, planes);
  assert.equal(batch._packGeneration, generation + 1);
});

test('uploads follow the pack generation and are repeated only when the GPU copy was retired', () => {
  const { context, uploads, streams } = harness();
  const record = poseRecord(3, 1);
  context.prepareCrowdBatchRecord(record, planes);
  const batch = context.prebuiltRigidBatchRecords.get(record.id);
  // Color pass and shadow pass both bind the batch.
  context.uploadPackedStream(batch, 'transforms', batch.transforms, 48);
  context.uploadPackedStream(batch, 'transforms', batch.transforms, 48);
  context.uploadPackedStream(batch, 'poses', batch.poses);
  context.uploadPackedStream(batch, 'poses', batch.poses);
  assert.deepEqual(uploads.map(upload => upload.slot), ['transforms', 'poses']);

  record.crowdRevision = 2;
  context.prepareCrowdBatchRecord(record, planes);
  context.uploadPackedStream(batch, 'transforms', batch.transforms, 48);
  assert.equal(uploads.length, 3, 'a repack uploads again');

  streams.clear(); // retired by the stream epoch
  context.uploadPackedStream(batch, 'transforms', batch.transforms, 48);
  assert.equal(uploads.length, 4);

  const plain = { id: 'rigid-1', transforms: new Float32Array(16) };
  context.uploadPackedStream(plain, 'transforms', plain.transforms, 16);
  context.uploadPackedStream(plain, 'transforms', plain.transforms, 16);
  assert.equal(uploads.length, 6, 'batches that do not repack keep uploading on every bind');
});

test('a motion-mode record copies motion records only after its revision changes', () => {
  const { context } = harness();
  const members = [motionAgent(1), motionAgent(2), motionAgent(3)];
  const record = motionRecordFor(members, 1);
  context.prepareCrowdBatchRecord(record, planes);
  const batch = context.prebuiltRigidBatchRecords.get(record.id);
  assert.ok(batch.motion instanceof Float32Array);
  assert.equal(batch.motion[20], 1);
  assert.equal(batch.motion[STRIDE * 2 + 20], 3);
  assert.ok(members.every(member => member._crowdMotion.dirty === false));
  context.crowdMotionStreamBuffer(batch); // the draw uploads the records and clears the dirty ranges
  const generation = batch._motionGeneration;

  batch.motion[0] = 999; // sentinel: a re-copy would overwrite it
  context.prepareCrowdBatchRecord(record, planes);
  assert.equal(batch.motion[0], 999);
  assert.equal(batch._motionGeneration, generation);

  members[1]._crowdMotion.record[20] = 9;
  members[1]._crowdMotion.dirty = true;
  record.crowdRevision = 2;
  context.prepareCrowdBatchRecord(record, planes);
  assert.equal(batch.motion[STRIDE + 20], 9);
  assert.equal(batch.motion[0], 999, 'untouched agents keep their packed values');
  assert.ok(batch._motionGeneration > generation);
  assert.deepEqual(Array.from(batch._motionDirtyFlags.subarray(0, 3)), [0, 1, 0]);
});

test('a membership change resyncs every agent through the record shape', () => {
  const { context } = harness();
  const members = [motionAgent(1), motionAgent(2)];
  const record = motionRecordFor(members, 1);
  context.prepareCrowdBatchRecord(record, planes);
  const batch = context.prebuiltRigidBatchRecords.get(record.id);
  for (const member of members) member._crowdMotion.dirty = false;

  members.reverse();
  record.crowdShape = 2;
  record.crowdRevision = 2;
  context.prepareCrowdBatchRecord(record, planes);
  assert.equal(batch.motion[20], 2, 'the first slot now holds the second agent');
  assert.equal(batch.motion[STRIDE + 20], 1);
  assert.deepEqual(Array.from(batch._motionDirtyFlags.subarray(0, 2)), [1, 1]);

  members.push(motionAgent(4));
  record.instanceCount = 3;
  record.crowdRevision = 3;
  context.prepareCrowdBatchRecord(record, planes);
  assert.equal(batch.count, 3);
  assert.equal(batch.motion[STRIDE * 2 + 20], 4);
});

test('a batch outside the view skips the color pass but still draws into shadow maps', () => {
  const { context } = harness();
  const record = poseRecord(2, 1);
  record.bounds = outside;
  context.prepareCrowdBatchRecord(record, planes);
  assert.equal(context.meshColorVisibility.get(record), false);
  assert.ok(context.rigidObjectBatches.get(record), 'the shadow pass still finds the batch');
  record.bounds = inside;
  context.prepareCrowdBatchRecord(record, planes);
  assert.equal(context.meshColorVisibility.get(record), true);
});

test('switching between pose and motion mode starts the stream over', () => {
  const { context } = harness();
  const record = poseRecord(2, 1);
  context.prepareCrowdBatchRecord(record, planes);
  const batch = context.prebuiltRigidBatchRecords.get(record.id);
  assert.equal(batch.motion, null);
  const members = [motionAgent(1), motionAgent(2)];
  Object.assign(record, motionRecordFor(members, 2), { id: record.id });
  context.prepareCrowdBatchRecord(record, planes);
  assert.ok(batch.motion instanceof Float32Array);
  assert.equal(batch.motion[20], 1);
});

test('dirty slot ranges limit the scan to the slots a partial motion frame touched', () => {
  const { context } = harness();
  const members = Array.from({ length: 40 }, (_v, i) => motionAgent(i));
  const record = motionRecordFor(members, 1);
  context.prepareCrowdBatchRecord(record, planes);
  const batch = context.prebuiltRigidBatchRecords.get(record.id);
  assert.ok(members.every(m => m._crowdMotion.dirty === false), 'the first pack copies every slot');
  batch._motionDirtyFlags.fill(0); // as if the GPU upload ran

  members[3]._crowdMotion.record[20] = 77; members[3]._crowdMotion.dirty = true;
  members[4]._crowdMotion.record[20] = 78; members[4]._crowdMotion.dirty = true;
  members[30]._crowdMotion.record[20] = 79; members[30]._crowdMotion.dirty = true;
  // A dirty slot outside the ranges proves the scan skipped it.
  members[20]._crowdMotion.record[20] = 99; members[20]._crowdMotion.dirty = true;
  record.crowdRevision = 2;
  record.crowdDirtyRanges = [3, 5, 30, 31];
  record.crowdDirtyBase = 1;
  context.prepareCrowdBatchRecord(record, planes);
  assert.equal(batch.motion[3 * STRIDE + 20], 77);
  assert.equal(batch.motion[4 * STRIDE + 20], 78);
  assert.equal(batch.motion[30 * STRIDE + 20], 79);
  assert.equal(batch.motion[20 * STRIDE + 20], 20, 'a slot outside the ranges is not copied');
  assert.equal(members[20]._crowdMotion.dirty, true, 'and keeps its dirty flag for a later full scan');
  assert.deepEqual(Array.from(batch._motionDirtyFlags.slice(0, 40).entries()).filter(([, v]) => v).map(([i]) => i), [3, 4, 30]);
});

test('dirty slot ranges are ignored by a batch that missed the revision they describe', () => {
  const { context } = harness();
  const members = Array.from({ length: 10 }, (_v, i) => motionAgent(i));
  const record = motionRecordFor(members, 1);
  context.prepareCrowdBatchRecord(record, planes);
  const batch = context.prebuiltRigidBatchRecords.get(record.id);

  // Revisions 2 and 3 both happened; the renderer only sees 3.
  members[2]._crowdMotion.record[20] = 51; members[2]._crowdMotion.dirty = true;
  members[8]._crowdMotion.record[20] = 52; members[8]._crowdMotion.dirty = true;
  record.crowdRevision = 3;
  record.crowdDirtyRanges = [8, 9];
  record.crowdDirtyBase = 2;
  context.prepareCrowdBatchRecord(record, planes);
  assert.equal(batch.motion[2 * STRIDE + 20], 51, 'the full scan still finds slot 2');
  assert.equal(batch.motion[8 * STRIDE + 20], 52);
});

test('null dirty ranges and a reshaped batch scan every slot', () => {
  const { context } = harness();
  const members = Array.from({ length: 6 }, (_v, i) => motionAgent(i));
  const record = motionRecordFor(members, 1);
  context.prepareCrowdBatchRecord(record, planes);
  const batch = context.prebuiltRigidBatchRecords.get(record.id);
  members[5]._crowdMotion.record[20] = 60; members[5]._crowdMotion.dirty = true;
  record.crowdRevision = 2;
  record.crowdDirtyRanges = null;
  context.prepareCrowdBatchRecord(record, planes);
  assert.equal(batch.motion[5 * STRIDE + 20], 60);

  members[1]._crowdMotion.record[20] = 61; members[1]._crowdMotion.dirty = true;
  record.crowdRevision = 3;
  record.crowdShape = 2;
  record.crowdDirtyRanges = [4, 5];
  record.crowdDirtyBase = 2;
  context.prepareCrowdBatchRecord(record, planes);
  assert.equal(batch.motion[1 * STRIDE + 20], 61, 'a shape change resyncs every slot');
});
