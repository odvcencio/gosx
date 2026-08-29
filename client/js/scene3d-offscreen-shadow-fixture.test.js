'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { buildOffscreenShadowFixture } = require('./testdata/offscreen-shadow-fixture.cjs');
const { buildSkinnedShadowFixture } = require('./testdata/skinned-shadow-fixture.cjs');

const COMP_SIZE = { 5126: 4, 5123: 2 };
const ELEMS = { SCALAR: 1, VEC2: 2, VEC3: 3, VEC4: 4, MAT4: 16 };
const VARIANTS = ['skinRest', 'skinPose', 'staticRest', 'staticPose'];

// Independent GLB parser (deliberately not the fixture's own parser).
function parseGLB(buf) {
  assert.ok(Buffer.isBuffer(buf) && buf.length >= 20);
  const jsonLen = buf.readUInt32LE(12);
  const binOff = 20 + jsonLen;
  const binLen = buf.readUInt32LE(binOff);
  return {
    json: JSON.parse(buf.subarray(20, binOff).toString('utf8')),
    bin: Buffer.from(buf.subarray(binOff + 8, binOff + 8 + binLen)),
  };
}

function readAccessor(json, bin, index) {
  const acc = json.accessors[index];
  const view = json.bufferViews[acc.bufferView];
  const start = (view.byteOffset || 0) + (acc.byteOffset || 0);
  const size = COMP_SIZE[acc.componentType];
  const elems = ELEMS[acc.type];
  const out = [];
  for (let i = 0; i < acc.count * elems; i += 1) {
    out.push(acc.componentType === 5126
      ? bin.readFloatLE(start + i * size)
      : bin.readUInt16LE(start + i * size));
  }
  return out;
}

function positionsOf(parsed) {
  const accIndex = parsed.json.meshes[0].primitives[0].attributes.POSITION;
  return readAccessor(parsed.json, parsed.bin, accIndex);
}

function assertGlbContainer(parsed) {
  const { json, bin } = parsed;
  assert.equal(json.asset.version, '2.0');
  assert.equal(json.buffers.length, 1);
  assert.equal(json.buffers[0].byteLength, bin.length); // declared BINlen
  assert.ok(bin.length > 0);
}

function assertIdentityRoots(json) {
  assert.deepEqual(json.scenes[json.scene].nodes, [0, 1]);
  for (const node of [json.nodes[0], json.nodes[1]]) {
    assert.equal(node.translation, undefined);
    assert.equal(node.rotation, undefined);
    assert.equal(node.scale, undefined);
    assert.equal(node.matrix, undefined);
  }
}

function assertPositionsBehindCamera(json, bin) {
  const accIndex = json.meshes[0].primitives[0].attributes.POSITION;
  const acc = json.accessors[accIndex];
  assert.equal(acc.count, 24);
  const pos = readAccessor(json, bin, accIndex);
  for (let i = 0; i < 24; i += 1) assert.ok(pos[i * 3 + 2] > 4, `vertex ${i} z>4`);
  const mins = [Infinity, Infinity, Infinity];
  const maxs = [-Infinity, -Infinity, -Infinity];
  for (let i = 0; i < 24; i += 1) {
    for (let k = 0; k < 3; k += 1) {
      mins[k] = Math.min(mins[k], pos[i * 3 + k]);
      maxs[k] = Math.max(maxs[k], pos[i * 3 + k]);
    }
  }
  assert.deepEqual(acc.min, mins); // exact float32 values, not decimal 0.3
  assert.deepEqual(acc.max, maxs);
  return pos;
}

function assertSkinTopology(json) {
  assert.equal(json.nodes[0].mesh, 0);
  assert.equal(json.nodes[0].skin, 0);
  assert.equal(json.skins.length, 1);
  assert.deepEqual(json.skins[0].joints, [1]);
}

test('buildOffscreenShadowFixture is deterministic across calls', () => {
  const a = buildOffscreenShadowFixture();
  const b = buildOffscreenShadowFixture();
  for (const name of VARIANTS) {
    assert.equal(typeof a[name], 'string');
    assert.ok(a[name].length > 0);
    assert.equal(a[name], b[name]);
  }
});

test('all four variants are valid GLBs with identity roots and correct BINlen', () => {
  const fixtures = buildOffscreenShadowFixture();
  for (const name of VARIANTS) {
    const parsed = parseGLB(Buffer.from(fixtures[name], 'base64'));
    assertGlbContainer(parsed);
    assertIdentityRoots(parsed.json);
  }
});

test('every position is behind the camera and accessor min/max match the bytes', () => {
  const fixtures = buildOffscreenShadowFixture();
  for (const name of VARIANTS) {
    const parsed = parseGLB(Buffer.from(fixtures[name], 'base64'));
    assertPositionsBehindCamera(parsed.json, parsed.bin);
  }
});

test('skinned variants preserve base bytes; pose clip drives joint node 1', () => {
  const fixtures = buildOffscreenShadowFixture();
  const base = parseGLB(Buffer.from(buildSkinnedShadowFixture().rest, 'base64'));
  const rest = parseGLB(Buffer.from(fixtures.skinRest, 'base64'));
  const pose = parseGLB(Buffer.from(fixtures.skinPose, 'base64'));

  assert.notEqual(fixtures.skinPose, fixtures.skinRest); // pose asset exists

  // POSITION was translated on Z, so it must differ from base but match
  // between rest and pose variants.
  assert.equal(base.json.meshes[0].primitives[0].attributes.POSITION,
    rest.json.meshes[0].primitives[0].attributes.POSITION);
  assert.notDeepEqual(positionsOf(base), positionsOf(rest));
  assert.deepEqual(positionsOf(rest), positionsOf(pose));

  // Index, normal, UV, JOINTS_0, WEIGHTS_0, inverse-bind bytes untouched.
  const json = rest.json;
  const prim = json.meshes[0].primitives[0];
  for (const attr of ['NORMAL', 'TEXCOORD_0', 'JOINTS_0', 'WEIGHTS_0']) {
    const v = json.bufferViews[json.accessors[prim.attributes[attr]].bufferView];
    const bv = base.json.bufferViews[base.json.accessors[base.json.meshes[0].primitives[0].attributes[attr]].bufferView];
    assert.deepEqual(rest.bin.subarray(v.byteOffset, v.byteOffset + v.byteLength),
      base.bin.subarray(bv.byteOffset, bv.byteOffset + bv.byteLength));
  }
  const iv = json.bufferViews[json.accessors[prim.indices].bufferView];
  const biv = base.json.bufferViews[base.json.accessors[base.json.meshes[0].primitives[0].indices].bufferView];
  assert.deepEqual(rest.bin.subarray(iv.byteOffset, iv.byteOffset + iv.byteLength),
    base.bin.subarray(biv.byteOffset, biv.byteOffset + biv.byteLength));
  const ibmv = json.bufferViews[json.accessors[json.skins[0].inverseBindMatrices].bufferView];
  const bibmv = base.json.bufferViews[base.json.accessors[base.json.skins[0].inverseBindMatrices].bufferView];
  assert.deepEqual(rest.bin.subarray(ibmv.byteOffset, ibmv.byteOffset + ibmv.byteLength),
    base.bin.subarray(bibmv.byteOffset, bibmv.byteOffset + bibmv.byteLength));
  assert.deepEqual(readAccessor(json, rest.bin, json.skins[0].inverseBindMatrices),
    [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);

  assertSkinTopology(rest.json);
  assertSkinTopology(pose.json);

  // Rest has no animation; pose has exactly the named clip on joint node 1.
  assert.equal(rest.json.animations, undefined);
  assert.equal(pose.json.animations.length, 1);
  const clip = pose.json.animations[0];
  assert.equal(clip.name, 'pose');
  assert.equal(clip.channels.length, 1);
  assert.deepEqual(clip.channels[0].target, { node: 1, path: 'translation' });
  const sampler = clip.samplers[0];
  assert.deepEqual(readAccessor(pose.json, pose.bin, sampler.input), [0, 1]);
  const liftY = Math.fround(0.3);
  assert.deepEqual(readAccessor(pose.json, pose.bin, sampler.output),
    [0, liftY, 0, 0, liftY, 0]);
});

test('staticRest drops all skinning and keeps skinRest positions exactly', () => {
  const fixtures = buildOffscreenShadowFixture();
  const skinRest = parseGLB(Buffer.from(fixtures.skinRest, 'base64'));
  const staticRest = parseGLB(Buffer.from(fixtures.staticRest, 'base64'));
  for (const name of ['staticRest', 'staticPose']) {
    const parsed = parseGLB(Buffer.from(fixtures[name], 'base64'));
    assert.equal(parsed.json.skins, undefined);
    assert.equal(parsed.json.animations, undefined);
    assert.equal(parsed.json.nodes[0].skin, undefined);
    assert.equal(parsed.json.meshes[0].primitives[0].attributes.JOINTS_0, undefined);
    assert.equal(parsed.json.meshes[0].primitives[0].attributes.WEIGHTS_0, undefined);
  }
  assert.deepEqual(positionsOf(staticRest), positionsOf(skinRest));
});

test('staticPose lifts y by exactly fround(y+0.3) with x/z unchanged', () => {
  const fixtures = buildOffscreenShadowFixture();
  const staticRest = parseGLB(Buffer.from(fixtures.staticRest, 'base64'));
  const staticPose = parseGLB(Buffer.from(fixtures.staticPose, 'base64'));
  const restPos = positionsOf(staticRest);
  const posePos = positionsOf(staticPose);
  for (let i = 0; i < 24; i += 1) {
    assert.equal(posePos[i * 3], restPos[i * 3]);
    assert.equal(posePos[i * 3 + 1], Math.fround(restPos[i * 3 + 1] + 0.3));
    assert.equal(posePos[i * 3 + 2], restPos[i * 3 + 2]);
  }
});
