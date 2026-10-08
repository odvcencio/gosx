'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { inflateSync } = require('node:zlib');
const { buildTexturedGLB, textureColorCounts } = require('./testdata/textured-glb-fixture.cjs');

function decodeFixture(white) {
  const glb = buildTexturedGLB({ white });
  assert.equal(glb.readUInt32LE(0), 0x46546c67);
  assert.equal(glb.readUInt32LE(4), 2);
  assert.equal(glb.readUInt32LE(8), glb.length);
  assert.equal(glb.readUInt32LE(16), 0x4e4f534a);
  const endJSON = 20 + glb.readUInt32LE(12);
  const doc = JSON.parse(glb.subarray(20, endJSON).toString());
  assert.equal(glb.readUInt32LE(endJSON + 4), 0x004e4942);
  assert.equal(endJSON + 8 + glb.readUInt32LE(endJSON), glb.length);
  const bin = glb.subarray(endJSON + 8);
  assert.deepEqual(doc.buffers, [{ byteLength: bin.length }]);
  assert.equal(doc.extensionsRequired, undefined);
  assert.equal(doc.extensionsUsed, undefined);
  for (const view of doc.bufferViews) {
    assert.equal(view.buffer, 0);
    assert.equal(view.byteOffset % 4, 0);
    assert.ok(view.byteOffset + view.byteLength <= bin.length);
  }
  const primitive = doc.meshes[0].primitives[0];
  assert.deepEqual(primitive.attributes, { POSITION: 0, NORMAL: 1, TEXCOORD_0: 2 });
  assert.equal(doc.accessors[primitive.indices].count, 6);
  assert.deepEqual(doc.accessors.slice(0, 3).map((a) => [a.count, a.type]),
    [[4, 'VEC3'], [4, 'VEC3'], [4, 'VEC2']]);
  assert.deepEqual(doc.materials, [{ pbrMetallicRoughness: {
    baseColorFactor: [1, 1, 1, 1], baseColorTexture: { index: 0 }, metallicFactor: 0, roughnessFactor: 1,
  } }]);
  assert.deepEqual(doc.textures, [{ source: 0, sampler: 0 }]);
  assert.deepEqual(doc.images, [{ bufferView: 4, mimeType: 'image/png' }]);
  const view = doc.bufferViews[4], png = bin.subarray(view.byteOffset, view.byteOffset + view.byteLength);
  assert.equal(png.subarray(0, 8).toString('hex'), '89504e470d0a1a0a');
  const idat = [];
  for (let at = 8; at < png.length;) {
    const size = png.readUInt32BE(at), kind = png.toString('ascii', at + 4, at + 8);
    assert.ok(at + 12 + size <= png.length);
    if (kind === 'IHDR') {
      assert.equal(png.readUInt32BE(at + 8), 8);
      assert.equal(png.readUInt32BE(at + 12), 8);
      assert.equal(png[at + 17], 6); // RGBA
    }
    if (kind === 'IDAT') idat.push(png.subarray(at + 8, at + 8 + size));
    at += 12 + size;
  }
  const rows = inflateSync(Buffer.concat(idat)), pixels = [];
  assert.equal(rows.length, 8 * 33);
  for (let row = 0; row < 8; row++) {
    assert.equal(rows[row * 33], 0);
    pixels.push(...rows.subarray(row * 33 + 1, (row + 1) * 33));
  }
  return pixels;
}

test('textured GLB fixture is single-BIN standard PBR with four embedded PNG quadrants', () => {
  assert.deepEqual(textureColorCounts(decodeFixture(false)), { red: 16, green: 16, blue: 16, yellow: 16 });
});

test('white embedded image preserves the GLB contract but cannot satisfy the texture oracle', () => {
  const pixels = decodeFixture(true);
  assert.ok(pixels.every((value) => value === 255));
  assert.deepEqual(textureColorCounts(pixels), { red: 0, green: 0, blue: 0, yellow: 0 });
});
