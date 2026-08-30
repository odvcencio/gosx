'use strict';
/* Unit tests for the pure CUBICSPLINE fixture module
 * (client/js/testdata/cubic-spline-fixture.cjs). Run directly:
 *
 *   node client/js/scene3d-cubic-spline-fixture.test.js
 *
 * The tests parse the generated GLB with their own independent reader,
 * verify the documented analytic oracle math, and assert that importing the
 * fixture never starts a browser, opens sockets, spawns processes, or leaves
 * timers behind — importing is pure and side-effect free. */

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const fixture = require(path.join(__dirname, 'testdata', 'cubic-spline-fixture.cjs'));

const { test } = require('node:test');

function assertCloseArray(actual, expected, tol, label) {
  assert.ok(Array.isArray(actual) && actual.length === expected.length,
    label + ': missing or wrong length (got ' + (actual && actual.length) + ')');
  for (let i = 0; i < expected.length; i += 1) {
    assert.ok(Math.abs(actual[i] - expected[i]) <= tol,
      label + '[' + i + ']=' + actual[i] + ' want ' + expected[i]);
  }
}

const COMP_SIZE = { 5126: 4, 5123: 2 };
const COMP_COUNT = { SCALAR: 1, VEC2: 2, VEC3: 3, VEC4: 4 };

function parseGLB(buf) {
  assert.ok(buf.length >= 12, 'GLB too short');
  assert.strictEqual(buf.readUInt32LE(0), fixture.GLB_MAGIC, 'magic');
  assert.strictEqual(buf.readUInt32LE(4), 2, 'version');
  assert.strictEqual(buf.readUInt32LE(8), buf.length, 'total length field matches buffer');
  let off = 12;
  const chunks = {};
  while (off < buf.length) {
    const len = buf.readUInt32LE(off);
    const type = buf.readUInt32LE(off + 4);
    assert.ok(off + 8 + len <= buf.length, 'chunk bounds');
    chunks[type] = buf.subarray(off + 8, off + 8 + len);
    off += 8 + len;
  }
  assert.strictEqual(off, buf.length, 'chunks fill the file exactly');
  const jsonChunk = chunks[fixture.CHUNK_TYPE_JSON];
  const binChunk = chunks[fixture.CHUNK_TYPE_BIN];
  assert.ok(jsonChunk, 'JSON chunk present');
  assert.ok(binChunk, 'BIN chunk present');
  assert.strictEqual(jsonChunk.length % 4, 0, 'JSON chunk padded to 4 bytes');
  return { json: JSON.parse(jsonChunk.toString('utf8')), bin: binChunk };
}

function readAccessor(gltf, bin, index) {
  const acc = gltf.accessors[index];
  assert.ok(acc, 'accessor ' + index + ' present');
  const bv = gltf.bufferViews[acc.bufferView];
  assert.ok(bv, 'accessor ' + index + ' bufferView present');
  const size = COMP_SIZE[acc.componentType] * COMP_COUNT[acc.type] * acc.count;
  const start = (bv.byteOffset || 0) + (acc.byteOffset || 0);
  assert.ok((bv.byteOffset || 0) + bv.byteLength <= bin.length,
    'accessor ' + index + ' bufferView within binary chunk');
  assert.strictEqual(bv.byteOffset % 4, 0, 'accessor ' + index + ' bufferView aligned');
  assert.ok(start + size <= (bv.byteOffset || 0) + bv.byteLength,
    'accessor ' + index + ' within bufferView');
  const out = [];
  for (let i = 0; i < acc.count * COMP_COUNT[acc.type]; i += 1) {
    out.push(acc.componentType === 5126
      ? bin.readFloatLE(start + i * 4)
      : bin.readUInt16LE(start + i * 2));
  }
  return out;
}

const parsed = parseGLB(Buffer.from(fixture.buildCubicSplineGLB()));
const json = parsed.json;
const bin = parsed.bin;

test('fixture import does not start a browser or leave live resources', () => {
  const src = fs.readFileSync(path.join(__dirname, 'testdata', 'cubic-spline-fixture.cjs'), 'utf8');
  assert.ok(!/require\s*\(\s*['"](fs|http|https|http2|net|dns|tls|dgram|child_process|electron)['"]/.test(src),
    'fixture must not require I/O, network, or process modules');
  const a = Buffer.from(fixture.buildCubicSplineGLB());
  const b = Buffer.from(fixture.buildCubicSplineGLB());
  assert.ok(a.equals(b), 'GLB build is deterministic with no shared mutable state');
  if (typeof process.getActiveResourcesInfo === 'function') {
    for (const r of process.getActiveResourcesInfo()) {
      assert.ok(!/^(TCPSocketWrap|ChildProcess)$/.test(r),
        'unexpected live resource after import/build: ' + r);
    }
  }
});

test('GLB header, chunk layout, and padding are valid', () => {
  assert.strictEqual(json.asset.version, '2.0');
  assert.strictEqual(json.buffers.length, 1);
  assert.strictEqual(json.buffers[0].byteLength, bin.length);
  for (const bv of json.bufferViews) {
    assert.ok(bv.byteLength > 0, 'bufferView non-empty');
    assert.ok((bv.byteOffset || 0) + bv.byteLength <= bin.length, 'bufferView within binary chunk');
  }
});

test('scene graph shape: two roots, invisible clock node, one morphed triangle', () => {
  assert.strictEqual(json.scenes.length, 1);
  assert.deepStrictEqual(json.scenes[0].nodes, [0, 1]);
  assert.strictEqual(json.nodes.length, 2);
  const curve = json.nodes[0];
  const clock = json.nodes[1];
  assert.strictEqual(curve.mesh, 0);
  assert.ok(!curve.translation && !curve.rotation && !curve.scale && !curve.weights,
    'curve node must carry identity authored TRS');
  assert.ok(!('mesh' in clock), 'clock node must have no mesh property (mesh:0 is a valid index)');
  assert.strictEqual(json.meshes.length, 1);
  const mesh = json.meshes[0];
  assert.strictEqual(mesh.primitives.length, 1);
  const prim = mesh.primitives[0];
  assert.strictEqual(prim.mode, 4);
  assert.ok(prim.attributes.POSITION !== undefined, 'POSITION attribute present');
  assert.ok(prim.attributes.NORMAL !== undefined, 'NORMAL attribute present');
  assert.ok(prim.indices !== undefined, 'primitive is indexed');
  assert.strictEqual(prim.material, 0);
  assert.strictEqual(json.materials.length, 1);
  assert.deepStrictEqual(mesh.weights, [0, 0]);
  assert.strictEqual(prim.targets.length, 2);
  assert.ok(prim.targets[0].POSITION !== undefined && prim.targets[1].POSITION !== undefined,
    'both morph targets carry POSITION deltas');
});

test('accessor bounds, types, and counts', () => {
  for (let i = 0; i < json.accessors.length; i += 1) readAccessor(json, bin, i);
  const expected = [
    ['VEC3', 3], ['VEC3', 3], ['SCALAR', 3], ['VEC3', 3], ['VEC3', 3],
    ['SCALAR', 2], ['VEC3', 6], ['VEC4', 6], ['VEC3', 6], ['SCALAR', 12], ['VEC3', 2],
  ];
  assert.strictEqual(json.accessors.length, expected.length);
  expected.forEach((e, i) => {
    assert.strictEqual(json.accessors[i].type, e[0], 'accessor ' + i + ' type');
    assert.strictEqual(json.accessors[i].count, e[1], 'accessor ' + i + ' count');
  });
});

test('one clip with five channels over times [0,4]; four CUBICSPLINE + one LINEAR clock', () => {
  assert.strictEqual(json.animations.length, 1);
  const anim = json.animations[0];
  assert.strictEqual(anim.name, 'curve');
  assert.strictEqual(anim.channels.length, 5);
  assert.strictEqual(anim.samplers.length, 5);
  const want = [
    [0, 'translation', 'CUBICSPLINE'],
    [0, 'rotation', 'CUBICSPLINE'],
    [0, 'scale', 'CUBICSPLINE'],
    [0, 'weights', 'CUBICSPLINE'],
    [1, 'translation', 'LINEAR'],
  ];
  want.forEach((w, i) => {
    const ch = anim.channels[i];
    assert.strictEqual(ch.target.node, w[0], 'channel ' + i + ' target node');
    assert.strictEqual(ch.target.path, w[1], 'channel ' + i + ' target path');
    const sampler = anim.samplers[ch.sampler];
    assert.strictEqual(sampler.interpolation, w[2], 'channel ' + i + ' interpolation');
    assertCloseArray(readAccessor(json, bin, sampler.input), [0, 4], 1e-6,
      'channel ' + i + ' input times');
  });
});

test('CUBICSPLINE keys store exact endpoint values and derivative tangents', () => {
  const anim = json.animations[0];
  const out = (i) => readAccessor(json, bin, anim.samplers[i].output);
  // Triplets per key: [inTangent, value, outTangent]; first in-tangent and
  // last out-tangent are unused and zero.
  assertCloseArray(out(0), [
    0, 0, 0, 0, 0, 0, 0, 0, 0,
    0.8, 0, 0, 1.6, 0, 0, 0, 0, 0,
  ], 1e-6, 'translation output');
  const rot = out(1);
  assertCloseArray(rot, [
    0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0,
    0, 0, 0.4, -0.2, 0, 0, 0.8, 0.6, 0, 0, 0, 0,
  ], 1e-6, 'rotation output');
  for (const k of [4, 16]) { // value triplets of key 0 and key 1
    const n = Math.hypot(rot[k], rot[k + 1], rot[k + 2], rot[k + 3]);
    assert.ok(Math.abs(n - 1) < 1e-6, 'endpoint key quaternion must be unit (got ' + n + ')');
  }
  assertCloseArray(out(2), [
    0, 0, 0, 1, 1, 1, 0, 0, 0,
    1.2, 1.2, 0, 2.6, 2.6, 1, 0, 0, 0,
  ], 1e-6, 'scale output');
  assertCloseArray(out(3), [
    0, 0, 0, 0, 0, 0.1,
    0.4, 0.1, 0.8, 0.4, 0, 0,
  ], 1e-6, 'weights output');
});

test('clock channel is LINEAR translation x = t', () => {
  const sampler = json.animations[0].samplers[4];
  assertCloseArray(readAccessor(json, bin, sampler.output), [0, 0, 0, 4, 0, 0], 1e-6,
    'clock output');
});

test('analytic evaluators match the documented curves', () => {
  assertCloseArray(fixture.evalTranslation(2), [0.4, 0, 0], 1e-12, 'translation(2)');
  assertCloseArray(fixture.evalScale(2), [1.2, 1.2, 1], 1e-12, 'scale(2)');
  assertCloseArray(fixture.evalScale(4), [2.6, 2.6, 1], 1e-12, 'scale(4)');
  assertCloseArray(fixture.evalWeights(2), [0.2, 0.2], 1e-12, 'weights(2)');
  assertCloseArray(fixture.evalWeights(4), [0.8, 0.4], 1e-12, 'weights(4)');
  assert.strictEqual(fixture.evalClock(3), 3);
  assertCloseArray(fixture.evalRotationRaw(2), [0, 0, 0.2, 0.9], 1e-12, 'raw rotation(2)');
  assertCloseArray(fixture.evalRotation(0), [0, 0, 0, 1], 1e-12, 'rotation(0)');
  assertCloseArray(fixture.evalRotation(4), [0, 0, 0.8, 0.6], 1e-12, 'rotation(4)');
  const norm = Math.hypot(0.2, 0.9);
  assertCloseArray(fixture.evalRotation(2), [0, 0, 0.2 / norm, 0.9 / norm], 1e-12, 'rotation(2) normalized');
  assertCloseArray(fixture.linearEndpointTranslation(1), [0.4, 0, 0], 1e-12, 'linear translation(1)');
  assertCloseArray(fixture.linearEndpointWeights(1), [0.2, 0.1], 1e-12, 'linear weights(1)');
});

test('expectedWorldPositions matches an independent T*R*S + morph composition', () => {
  assertCloseArray(fixture.expectedWorldPositions(0), fixture.BASE_POSITIONS, 1e-12,
    'world positions at t=0 are the authored triangle');
  const t = 1.25;
  // Independent recomputation written here (special-cased quaternion
  // [0,0,z,w]), deliberately not calling the fixture's own matrix code.
  const T = [0.1 * t * t, 0, 0];
  const s = 1 + 0.025 * t * t * t;
  const S = [s, s, 1];
  const zRaw = 0.05 * t * t;
  const wRaw = 1 - 0.025 * t * t;
  const n = Math.hypot(zRaw, wRaw);
  const z = zRaw / n;
  const w = wRaw / n;
  const R = [1 - 2 * z * z, -2 * z * w, 0, 2 * z * w, 1 - 2 * z * z, 0, 0, 0, 1];
  const w0 = 0.05 * t * t;
  const w1 = 0.1 * t;
  const P = fixture.BASE_POSITIONS;
  const D0 = fixture.MORPH_DELTA_0;
  const D1 = fixture.MORPH_DELTA_1;
  const out = [];
  for (let i = 0; i < 3; i += 1) {
    const lx = P[i * 3] + w0 * D0[i * 3] + w1 * D1[i * 3];
    const ly = P[i * 3 + 1] + w0 * D0[i * 3 + 1] + w1 * D1[i * 3 + 1];
    const lz = P[i * 3 + 2] + w0 * D0[i * 3 + 2] + w1 * D1[i * 3 + 2];
    out.push(
      R[0] * S[0] * lx + R[1] * S[1] * ly + R[2] * S[2] * lz + T[0],
      R[3] * S[0] * lx + R[4] * S[1] * ly + R[5] * S[2] * lz + T[1],
      R[6] * S[0] * lx + R[7] * S[1] * ly + R[8] * S[2] * lz + T[2],
    );
  }
  assertCloseArray(fixture.expectedWorldPositions(t), out, 1e-12,
    'world positions at t=' + t);
});

test('cubic pose discriminates strongly from linear-endpoint interpolation', () => {
  for (const t of [0.9, 1, 1.5, 2.5]) {
    const dx = Math.abs(fixture.evalTranslation(t)[0] - fixture.linearEndpointTranslation(t)[0]);
    assert.ok(dx > 0.05, 'translation discrimination at t=' + t + ' (dx=' + dx + ')');
    const dw = Math.abs(fixture.evalWeights(t)[0] - fixture.linearEndpointWeights(t)[0]);
    assert.ok(dw > 0.05, 'weights discrimination at t=' + t + ' (dw=' + dw + ')');
  }
});
