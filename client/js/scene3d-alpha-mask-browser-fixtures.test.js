'use strict';
// Regression test for two probe-authoring mistakes in the masktex slice of
// scene3d-material-ior-browser.cjs: (1) the case array was not registered via
// CASES.push, (2) specFactor was authored where baseAlpha was intended.
// This executes the pure fixture+case source prefix only; it checks fixture
// DATA, not renderer shading. Native tests remain required.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const zlib = require('node:zlib');

const SRC = fs.readFileSync(
  path.join(__dirname, 'testdata', 'scene3d-material-ior-browser.cjs'), 'utf8');
const start = SRC.indexOf('function buildQuadGLB(');
const end = SRC.indexOf('const byName =');
assert.ok(start >= 0 && end > start, 'slice markers found');
let at = start;
for (const m of ['const TEX_PNGS', 'function buildQuadGLBTex(',
  'const GLB_FILES', 'const CASES']) {
  const i = SRC.indexOf(m, at);
  assert.ok(i >= 0 && i < end, `marker in order: ${m}`);
  at = i;
}
const prefix = SRC.slice(start, end);
// The dummy IBL descriptor only lets unrelated case definitions load; no IBL
// behavior is verified here.
const sandbox = vm.createContext({
  Buffer, zlib, console, TextEncoder, TextDecoder,
  F0: (ior) => ((ior - 1) / (ior + 1)) ** 2,
  IBL_FIXTURE: { descriptor: {} },
});
const [GLB_FILES, TEX_PNGS, CASES] = vm.runInContext(
  prefix + '\n;[GLB_FILES, TEX_PNGS, CASES];', sandbox);
const cases = JSON.parse(JSON.stringify(CASES)); // unwrap cross-realm objects

// [base, alphaMode, cutoff, baseAlphaFactor, opacity, ref, expectedEmpty]
const TABLE = [
  ['control', 'OPAQUE', -1, 1, 1, null, false],
  ['opaque-a0', 'OPAQUE', -1, 1, 1, { same: 'gl-masktex-control' }, false],
  ['mask-a0', 'MASK', 0.5, 1, 1, { differs: 'gl-masktex-control' }, true],
  ['mask-a255', 'MASK', 0.5, 1, 1, { same: 'gl-masktex-control' }, false],
  ['mask-a128', 'MASK', 0.5, 1, 1, { same: 'gl-masktex-control' }, false],
  ['mask-a128-f5', 'MASK', 0.5, 0.5, 0.5, { differs: 'gl-masktex-control' }, true],
  ['mask-c0-f0', 'MASK', 0, 0, 0, { same: 'gl-masktex-control' }, false],
  ['unlit-control', 'OPAQUE', -1, 1, 1, null, false],
  ['unlit-discard', 'MASK', 0.5, 0.5, 0.5, { differs: 'gl-masktex-unlit-control' }, true],
  ['unlit-survive', 'MASK', 0.5, 1, 1, { same: 'gl-masktex-unlit-control' }, false],
];
const SUFFIX = ['a255', 'a0', 'a0', 'a255', 'a128', 'a128', 'a0', 'a255', 'a128', 'a128'];

test('masktex probe cases are registered and reference valid controls', () => {
  assert.ok(CASES.length >= 157, 'all case groups registered');
  const mask = cases.filter((c) => /-masktex-/.test(c.name));
  const names = mask.map((c) => c.name);
  assert.equal(names.length, 20);
  assert.equal(new Set(names).size, 20, 'no duplicates');
  for (const be of ['gl', 'wg'])
    for (const row of TABLE)
      assert.ok(names.includes(`${be}-masktex-${row[0]}`), `${be}-${row[0]}`);
  const byName = new Map(cases.map((c) => [c.name, c]));
  for (const c of mask) {
    assert.equal(c.model.wireframe, false);
    assert.equal(c.albedoTex, true);
    // No explicit opacity/alphaCutoff/unlit on the model: GLB data governs.
    assert.deepEqual(Object.keys(c.model).sort(), ['id', 'src', 'static', 'wireframe']);
    const row = TABLE.find((r) => c.name === 'gl-masktex-' + r[0] ||
      c.name === 'wg-masktex-' + r[0]);
    assert.ok(row);
    assert.equal(c.expectedOpacity, row[4]);
    assert.equal(c.expectedAlphaCutoff, row[2]);
    assert.equal(c.expectedEmpty, row[6] ? true : undefined);
    const refKey = row[5] && Object.keys(row[5])[0];
    if (refKey) {
      const refName = row[5][refKey].replace(/^gl-/, c.name.startsWith('wg-') ? 'wg-' : 'gl-');
      const ref = byName.get(refName);
      assert.ok(ref, 'ref registered');
      assert.ok(cases.indexOf(ref) < cases.indexOf(c), 'ref registered earlier');
      assert.equal(!!ref.webgpu, !!c.webgpu, 'same backend');
      assert.equal(c[refKey], refName);
    } else {
      assert.ok(!c.same && !c.differs, 'controls have no same/diff');
    }
  }
});

test('masktex GLB materials and PNG texels match authored fixtures', () => {
  for (let i = 0; i < 10; i++) {
    const row = TABLE[i];
    const glb = GLB_FILES['/models/gl-masktex-' + row[0] + '.glb'];
    assert.ok(Buffer.isBuffer(glb));
    assert.equal(glb.readUInt32BE(0), 0x676c5446, 'glTF magic');
    assert.equal(glb.readUInt32LE(4), 2, 'version 2');
    const total = glb.readUInt32LE(8), clen = glb.readUInt32LE(12);
    assert.equal(glb.readUInt32LE(16), 0x4e4f534a, 'JSON chunk type');
    assert.ok(20 + clen <= total && total <= glb.length, 'chunk bounds');
    const json = JSON.parse(glb.subarray(20, 20 + clen).toString('utf8'));
    const mat = json.materials[json.meshes[0].primitives[0].material];
    assert.equal((mat.pbrMetallicRoughness.baseColorFactor || [1, 1, 1, 1])[3], row[3]);
    assert.equal(mat.alphaMode || 'OPAQUE', row[1]);
    if (row[1] === 'MASK') assert.equal(mat.alphaCutoff, row[2]);
    else assert.equal(mat.alphaCutoff, undefined);
    const ext = mat.extensions || {};
    assert.equal(!!ext.KHR_materials_unlit, row[0].startsWith('unlit'));
    assert.equal(ext.KHR_materials_ior.ior, 2.42);
    assert.ok(!ext.KHR_materials_specular, 'no specular ext in masktex family');
    const uri = json.images[
      json.textures[mat.pbrMetallicRoughness.baseColorTexture.index].source].uri;
    assert.equal(uri, '/tex/alb-white-' + SUFFIX[i] + '.png');
    assert.deepEqual(cases.find((c) => c.name === 'gl-masktex-' + row[0]).requiredTex, [uri]);
    // Decode the actual PNG: 4x4 RGBA, unfiltered rows, white RGB, alpha per suffix.
    const png = TEX_PNGS[uri];
    assert.deepEqual([...png.subarray(0, 8)],
      [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
    let off = 8, w = 0, h = 0; const idat = [];
    while (off < png.length) {
      const len = png.readUInt32BE(off);
      const type = png.toString('ascii', off + 4, off + 8);
      if (type === 'IHDR') {
        w = png.readUInt32BE(off + 8); h = png.readUInt32BE(off + 12);
        assert.equal(png[off + 16], 8); assert.equal(png[off + 17], 6);
      }
      if (type === 'IDAT') idat.push(png.subarray(off + 8, off + 8 + len));
      off += 12 + len;
    }
    assert.equal(w, 4); assert.equal(h, 4);
    const raw = zlib.inflateSync(Buffer.concat(idat));
    const wantA = SUFFIX[i] === 'a0' ? 0 : SUFFIX[i] === 'a128' ? 128 : 255;
    for (let y = 0; y < 4; y++) {
      assert.equal(raw[y * 17], 0, 'filter None');
      for (let x = 0; x < 4; x++) {
        const o = y * 17 + 1 + x * 4;
        assert.deepEqual([...raw.subarray(o, o + 3)], [255, 255, 255], 'white RGB even at a0');
        assert.equal(raw[o + 3], wantA, `alpha at ${x},${y}`);
      }
    }
  }
});
