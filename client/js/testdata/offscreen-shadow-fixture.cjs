'use strict';
/*
 * Offscreen shadow glTF fixtures, derived from skinned-shadow-fixture.cjs.
 * Pure Node builtins, no I/O at import time.
 *
 * The skinned assets are reused byte-for-byte via
 * require('./skinned-shadow-fixture.cjs').buildSkinnedShadowFixture(); the
 * box is then translated +4 on Z (center (-0.55, 0.35, 4.5)) so it sits
 * behind a camera at (0, 0, 4). Variants:
 *   skinRest   - translated skinned rest (no animation, identity joint skin)
 *   skinPose   - translated skinned pose; clip output keys [0, 0.3, 0]
 *   staticRest - skinned rest with skin/JOINTS_0/WEIGHTS_0/animations removed
 *   staticPose - static rest with POSITION.y += 0.3
 * MODEL ROOT (node 0) stays identity in every variant. Indices, normals,
 * UVs, weights, and inverse bind data are never modified.
 */

const { buildSkinnedShadowFixture } = require('./skinned-shadow-fixture.cjs');

const TRANSLATE_Z = 4;
const POSE_KEYS = [0, 0.3, 0];
const STATIC_LIFT_Y = 0.3;

function parseOffscreenGLB(buffer) {
  if (!Buffer.isBuffer(buffer) || buffer.length < 20) throw new Error('GLB too small');
  if (buffer.readUInt32LE(0) !== 0x46546C67) throw new Error('bad GLB magic');
  if (buffer.readUInt32LE(4) !== 2) throw new Error('bad GLB version');
  if (buffer.readUInt32LE(8) !== buffer.length) throw new Error('GLB length mismatch');
  const jsonLen = buffer.readUInt32LE(12);
  if (buffer.readUInt32LE(16) !== 0x4E4F534A) throw new Error('bad JSON chunk type');
  if (jsonLen % 4 !== 0 || 20 + jsonLen + 8 > buffer.length) throw new Error('bad JSON chunk bounds');
  const binOff = 20 + jsonLen;
  const binLen = buffer.readUInt32LE(binOff);
  if (buffer.readUInt32LE(binOff + 4) !== 0x004E4942) throw new Error('bad BIN chunk type');
  if (binOff + 8 + binLen !== buffer.length) throw new Error('BIN chunk length mismatch');
  return {
    json: JSON.parse(buffer.subarray(20, binOff).toString('utf8')),
    bin: Buffer.from(buffer.subarray(binOff + 8, binOff + 8 + binLen)),
  };
}

function encodeGLB(json, bin) {
  json.buffers[0].byteLength = bin.length;
  let jsonBuf = Buffer.from(JSON.stringify(json), 'utf8');
  const jp = (4 - (jsonBuf.length % 4)) % 4;
  if (jp) jsonBuf = Buffer.concat([jsonBuf, Buffer.alloc(jp, 0x20)]);
  const bp = (4 - (bin.length % 4)) % 4;
  const binP = bp ? Buffer.concat([bin, Buffer.alloc(bp)]) : bin;
  const header = Buffer.alloc(12);
  header.writeUInt32LE(0x46546C67, 0);
  header.writeUInt32LE(2, 4);
  header.writeUInt32LE(12 + 8 + jsonBuf.length + 8 + binP.length, 8);
  const jh = Buffer.alloc(8);
  jh.writeUInt32LE(jsonBuf.length, 0);
  jh.writeUInt32LE(0x4E4F534A, 4);
  const bh = Buffer.alloc(8);
  bh.writeUInt32LE(binP.length, 0);
  bh.writeUInt32LE(0x004E4942, 4);
  return Buffer.concat([header, jh, jsonBuf, bh, binP]);
}

function readFloatVec3(bin, start, count) {
  const values = [];
  for (let i = 0; i < count * 3; i += 1) values.push(bin.readFloatLE(start + i * 4));
  return values;
}

function writeFloatVec3(bin, start, values) {
  for (let i = 0; i < values.length; i += 1) bin.writeFloatLE(values[i], start + i * 4);
}

function recomputeMinMax(bin, start, count) {
  const written = readFloatVec3(bin, start, count);
  const mins = [Infinity, Infinity, Infinity];
  const maxs = [-Infinity, -Infinity, -Infinity];
  for (let i = 0; i < count; i += 1) {
    for (let k = 0; k < 3; k += 1) {
      const v = written[i * 3 + k];
      mins[k] = Math.min(mins[k], v);
      maxs[k] = Math.max(maxs[k], v);
    }
  }
  return { mins, maxs };
}

// Translates the POSITION accessor (float VEC3, non-interleaved view) by
// delta, rewriting the BIN in place and re-deriving accessor min/max from
// the values actually written (post-fround float32 reads).
function translatePositions(json, bin, delta) {
  const accIndex = json.meshes[0].primitives[0].attributes.POSITION;
  const acc = json.accessors[accIndex];
  if (acc.componentType !== 5126 || acc.type !== 'VEC3') throw new Error('expected float VEC3 POSITION');
  const start = json.bufferViews[acc.bufferView].byteOffset || 0;
  const values = readFloatVec3(bin, start, acc.count);
  for (let i = 0; i < acc.count; i += 1) {
    values[i * 3] += delta[0];
    values[i * 3 + 1] += delta[1];
    values[i * 3 + 2] += delta[2];
  }
  writeFloatVec3(bin, start, values);
  const { mins, maxs } = recomputeMinMax(bin, start, acc.count);
  acc.min = mins;
  acc.max = maxs;
}

// Rewrites the constant pose clip output keys (both keyframes) to POSE_KEYS
// and recomputes the output accessor min/max from the written float32 data.
function setPoseKeys(json, bin) {
  const accIndex = json.animations[0].samplers[0].output;
  const acc = json.accessors[accIndex];
  if (acc.componentType !== 5126 || acc.type !== 'VEC3') throw new Error('expected float VEC3 output');
  const start = json.bufferViews[acc.bufferView].byteOffset || 0;
  const values = [];
  for (let i = 0; i < acc.count; i += 1) values.push(POSE_KEYS[0], POSE_KEYS[1], POSE_KEYS[2]);
  writeFloatVec3(bin, start, values);
  const { mins, maxs } = recomputeMinMax(bin, start, acc.count);
  acc.min = mins;
  acc.max = maxs;
}

// Removes skinning: mesh-node skin reference, JOINTS_0/WEIGHTS_0 attributes,
// the skins array, and animations. Orphaned accessors/bufferViews are left in
// place (valid glTF); indices/normals/UVs/inverse-bind bytes are untouched.
function stripSkinning(json) {
  delete json.skins;
  delete json.animations;
  for (const node of json.nodes) delete node.skin;
  for (const mesh of json.meshes) {
    for (const prim of mesh.primitives) {
      delete prim.attributes.JOINTS_0;
      delete prim.attributes.WEIGHTS_0;
    }
  }
}

function buildOffscreenShadowFixture() {
  const base = buildSkinnedShadowFixture();
  const translated = parseOffscreenGLB(Buffer.from(base.rest, 'base64'));
  translatePositions(translated.json, translated.bin, [0, 0, TRANSLATE_Z]);

  const skinRest = encodeGLB(translated.json, translated.bin);

  const pose = parseOffscreenGLB(Buffer.from(base.pose, 'base64'));
  translatePositions(pose.json, pose.bin, [0, 0, TRANSLATE_Z]);
  setPoseKeys(pose.json, pose.bin);
  const skinPose = encodeGLB(pose.json, pose.bin);

  const staticJson = JSON.parse(JSON.stringify(translated.json));
  stripSkinning(staticJson);
  const staticRest = encodeGLB(staticJson, Buffer.from(translated.bin));

  const liftedBin = Buffer.from(translated.bin);
  translatePositions(staticJson, liftedBin, [0, STATIC_LIFT_Y, 0]);
  const staticPose = encodeGLB(staticJson, liftedBin);

  return {
    skinRest: skinRest.toString('base64'),
    skinPose: skinPose.toString('base64'),
    staticRest: staticRest.toString('base64'),
    staticPose: staticPose.toString('base64'),
  };
}

module.exports = { buildOffscreenShadowFixture, parseOffscreenGLB };
