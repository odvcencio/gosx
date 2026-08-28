'use strict';
// Regression test for two probe-authoring mistakes in the masktex slice of
// scene3d-material-ior-browser.cjs: (1) the case array was not registered via
// CASES.push, (2) specFactor was authored where baseAlpha was intended.
// This executes the pure fixture+case source prefix only; it checks fixture
// DATA, not renderer shading. The diffExpr section validates ROI comparison
// semantics only with scripted image fixtures — it does NOT validate PNG
// decoding. Native tests remain required.
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
  assert.ok(CASES.length >= 161, 'all case groups registered');
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
    // expectedUnlit must mirror the authored material: true for the unlit
    // rows (last 3), explicitly false for every lit row.
    assert.equal(c.expectedUnlit, row[0].startsWith('unlit'));
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

test('masktex unlit/lit dark probe cases gate on expectedUnlit', () => {
  // [name, expectedUnlit, src, control name, comparison key]
  const DARK = [
    ['gl-unlit-dark', true, '/models/gl-masktex-unlit-control.glb',
      'gl-masktex-unlit-control', 'same'],
    ['wg-unlit-dark', true, '/models/gl-masktex-unlit-control.glb',
      'wg-masktex-unlit-control', 'same'],
    ['gl-lit-dark', false, '/models/gl-masktex-control.glb',
      'gl-masktex-control', 'differs'],
    ['wg-lit-dark', false, '/models/gl-masktex-control.glb',
      'wg-masktex-control', 'differs'],
  ];
  const byName = new Map(cases.map((c) => [c.name, c]));
  for (const [name, unlit, src, refName, refKey] of DARK) {
    const c = byName.get(name);
    assert.ok(c, `${name} registered`);
    assert.equal(c.model.src, src);
    assert.equal(c.model.wireframe, false);
    // No material overrides on the model: GLB data governs shading.
    assert.deepEqual(Object.keys(c.model).sort(), ['id', 'src', 'static', 'wireframe']);
    assert.equal(c.keyLightIntensity, 0, 'dark: key light off');
    assert.equal(c.albedoTex, true);
    assert.deepEqual(c.requiredTex, ['/tex/alb-white-a255.png']);
    assert.equal(c.expectedOpacity, 1);
    assert.equal(c.expectedAlphaCutoff, -1);
    assert.equal(c.expectedUnlit, unlit);
    assert.equal(c.expectedEmpty, undefined, 'no empty expectation');
    const otherKey = refKey === 'same' ? 'differs' : 'same';
    assert.ok(!(otherKey in c), 'no extra comparison key');
    const ref = byName.get(refName);
    assert.ok(ref, 'control registered');
    assert.ok(cases.indexOf(ref) < cases.indexOf(c), 'control registered earlier');
    assert.equal(!!ref.webgpu, !!c.webgpu, 'same backend');
    assert.equal(c[refKey], refName);
    if (refKey === 'differs') assert.equal(c.minChanged, 50);
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

test('shadowROI probe cases: 20 unique, root material schema, verified factors/cutoffs/textures', () => {
  // Select by the shadowROI marker itself: a plain name regex would sweep in
  // unrelated prior budget cases that merely contain "-shadow-".
  const shadow = cases.filter((c) => c.shadowROI);
  const names = shadow.map((c) => c.name);
  assert.equal(names.length, 20);
  assert.equal(new Set(names).size, 20, 'no duplicate shadow case names');
  const SUFFIXES = ['control', 'no-cast', 'factor-discard', 'factor-survive',
    'tex-discard', 'tex-survive', 'zero', 'opaque-a0', 'factor-tex', 'threshold'];
  for (const be of ['gl', 'wg'])
    for (const s of SUFFIXES)
      assert.ok(names.includes(be + '-shadow-' + s), be + '-shadow-' + s);
  // Extra caster material keys beyond the shared root schema, per suffix.
  const MATRIX = {
    'control': {},
    'no-cast': {},
    'factor-discard': { opacity: 0.25, alphaCutoff: 0.5 },
    'factor-survive': { opacity: 0.5, alphaCutoff: 0.5 },
    'tex-discard': { opacity: 1, alphaCutoff: 0.5, texture: '/tex/alb-white-a0.png' },
    'tex-survive': { opacity: 1, alphaCutoff: 0.5, texture: '/tex/alb-white-a255.png' },
    'zero': { opacity: 0, alphaCutoff: 0, texture: '/tex/alb-white-a0.png' },
    'opaque-a0': { opacity: 1, texture: '/tex/alb-white-a0.png' },
    'factor-tex': { opacity: 0.5, alphaCutoff: 0.5, texture: '/tex/alb-white-a128.png' },
    'threshold': { opacity: 1, alphaCutoff: 0.5, texture: '/tex/alb-white-a128.png' },
  };
  // [comparison key, referenced suffix] per suffix — verified per case, not
  // merely "some earlier case exists".
  const REF = {
    'control': null,
    'no-cast': ['differs', 'control'],
    'factor-discard': ['same', 'no-cast'],
    'factor-survive': ['same', 'control'],
    'tex-discard': ['same', 'no-cast'],
    'tex-survive': ['same', 'control'],
    'zero': ['same', 'control'],
    'opaque-a0': ['same', 'control'],
    'factor-tex': ['same', 'no-cast'],
    'threshold': ['same', 'control'],
  };
  const byName = new Map(cases.map((c) => [c.name, c]));
  for (const c of shadow) {
    const be = c.name.startsWith('wg-') ? 'wg-' : 'gl-';
    const suffix = c.name.slice(be.length + 'shadow-'.length);
    const row = MATRIX[suffix];
    assert.ok(row, 'known shadow suffix: ' + suffix);
    assert.equal(c.webgpu, be === 'wg-');
    assert.equal(c.lights.length, 1);
    assert.equal(c.lights[0].castShadow, true);
    const caster = c.objects[0], receiver = c.objects[1];
    assert.equal(caster.id, 'caster');
    assert.equal(receiver.id, 'receiver');
    // Root material schema: authored material keys live on the object itself,
    // for both the caster and the receiver.
    assert.ok(!('material' in caster), 'no nested material object on the caster');
    assert.ok(!('material' in receiver), 'no nested material object on the receiver');
    for (const key of Object.keys(row)) assert.deepEqual(caster[key], row[key]);
    for (const key of ['opacity', 'alphaCutoff', 'texture'])
      if (!(key in row)) assert.ok(!(key in caster), suffix + ' has no ' + key);
    assert.equal(caster.castShadow, suffix !== 'no-cast',
      'castShadow true except the explicit no-cast case');
    assert.equal(caster.receiveShadow, false);
    assert.equal(caster.wireframe, false);
    assert.equal(receiver.castShadow, false);
    assert.equal(receiver.receiveShadow, true);
    assert.equal(receiver.wireframe, false);
    assert.deepEqual(c.shadowROI, { x: 125, y: 99, width: 50, height: 35 });
    const ref = REF[suffix];
    if (ref) {
      const [refKey, refSuffix] = ref;
      const refName = be + 'shadow-' + refSuffix;
      assert.equal(c[refKey], refName,
        suffix + ' references exactly ' + refName + ' via ' + refKey);
      assert.ok(!((refKey === 'same' ? 'differs' : 'same') in c),
        'no extra comparison key beyond ' + refKey);
      const target = byName.get(refName);
      assert.ok(target, refKey + ' control registered: ' + refName);
      assert.ok(cases.indexOf(target) < cases.indexOf(c),
        refKey + ' control ordered earlier');
      assert.equal(!!target.webgpu, !!c.webgpu, refKey + ' control on same backend');
      if (refKey === 'differs') assert.equal(c.minChanged, 50);
    } else {
      assert.ok(!c.same && !c.differs, 'control has no same/diff');
    }
    if (row.texture) {
      assert.ok(TEX_PNGS[row.texture], 'texture PNG fixture exists: ' + row.texture);
      assert.deepEqual(c.requiredTex, [row.texture]);
    } else {
      assert.ok(!c.requiredTex, 'no requiredTex without a caster texture');
    }
  }
});

// --- diffExpr ROI semantics, executed against the actual browser source ---

const DIFF_START = SRC.indexOf('function diffExpr(');
const DIFF_END = SRC.indexOf('async function capture(');
assert.ok(DIFF_START >= 0 && DIFF_END > DIFF_START, 'diffExpr slice markers found');
const DIFF_SOURCE = SRC.slice(DIFF_START, DIFF_END);

// Scripted 4x4 RGBA image fixtures for the fake Image below. This validates
// the diffExpr ROI/delta semantics only — no PNG encoding or decoding is
// exercised or claimed here (the real PNG fixture is covered by the masktex
// test above).
const DIFF_W = 4, DIFF_H = 4;
const FIXTURE_A = 'data:image/png;base64,scene3d-diff-fixture-a';
const FIXTURE_B = 'data:image/png;base64,scene3d-diff-fixture-b';

function diffFixtureData(fill) {
  const data = new Uint8ClampedArray(DIFF_W * DIFF_H * 4);
  for (let y = 0; y < DIFF_H; y++) {
    for (let x = 0; x < DIFF_W; x++) {
      const p = fill(x, y);
      const o = (y * DIFF_W + x) * 4;
      data[o] = p[0]; data[o + 1] = p[1]; data[o + 2] = p[2]; data[o + 3] = p[3];
    }
  }
  return data;
}

const DATA_A = diffFixtureData(() => [0, 0, 0, 0]);
const DATA_B = diffFixtureData((x, y) =>
  (x === 2 && y === 1) ? [255, 255, 255, 255] : [0, 0, 0, 0]);

class FakeDiffImage {
  constructor() { this.onload = null; this.onerror = null; }
  set src(v) {
    const img = this;
    const key = String(v);
    const data = key === FIXTURE_A ? DATA_A : key === FIXTURE_B ? DATA_B : null;
    if (!data) {
      setTimeout(() => { if (img.onerror) img.onerror(new Error('unknown diff fixture src')); }, 0);
      return;
    }
    img.width = DIFF_W;
    img.height = DIFF_H;
    img._data = data;
    setTimeout(() => { if (img.onload) img.onload(); }, 0);
  }
}

function makeDiffDocument() {
  return {
    createElement(tag) {
      assert.equal(tag, 'canvas');
      const cv = { width: 0, height: 0, _img: null };
      cv.getContext = () => ({
        drawImage(img) { cv._img = img; },
        clearRect() { cv._img = null; },
        getImageData(x, y, w, h) {
          assert.equal(x, 0); assert.equal(y, 0);
          return { data: cv._img._data };
        },
      });
      return cv;
    },
  };
}

const DIFF_CTX = vm.createContext({
  Promise, Math, JSON, setTimeout,
  Image: FakeDiffImage, document: makeDiffDocument(),
});
vm.runInContext(DIFF_SOURCE, DIFF_CTX,
  { filename: 'scene3d-material-ior-browser.cjs#diffExpr' });

async function evalDiff(roi) {
  // diffExpr builds the data: URI prefix itself; pass payload tokens only.
  // FakeDiffImage still sees full URIs because diffExpr re-adds the prefix.
  const payloadA = FIXTURE_A.slice('data:image/png;base64,'.length);
  const payloadB = FIXTURE_B.slice('data:image/png;base64,'.length);
  const expr = DIFF_CTX.diffExpr(payloadA, payloadB, roi);
  assert.equal(typeof expr, 'string');
  return await vm.runInContext(expr, DIFF_CTX, { filename: 'diffExpr-eval' });
}

test('diffExpr ROI compares only inside the region', async () => {
  const inside = await evalDiff({ x: 2, y: 1, width: 1, height: 1 });
  assert.equal(inside.dimsMatch, true);
  assert.equal(inside.exactPixels, 1);
  assert.equal(inside.exactBytes, 4);
  assert.equal(inside.maxDelta, 255);
  const outside = await evalDiff({ x: 0, y: 0, width: 2, height: 4 });
  assert.equal(outside.dimsMatch, true);
  assert.equal(outside.exactPixels, 0, 'no changes outside the ROI');
});

test('diffExpr with absent/null ROI compares the whole canvas', async () => {
  const whole = await evalDiff(undefined);
  assert.equal(whole.dimsMatch, true);
  assert.equal(whole.exactPixels, 1);
  assert.equal(whole.exactBytes, 4);
  assert.ok(whole.meanChanged >= 1);
  assert.deepEqual(await evalDiff(null), whole);
});

test('diffExpr invalid ROIs resolve null and never fall back to the whole canvas', async () => {
  const bad = [
    { x: 1.5, y: 0, width: 1, height: 1 },
    { x: 0, y: 0.5, width: 1, height: 1 },
    { x: 0, y: 0, width: 1.5, height: 1 },
    { x: 0, y: 0, width: 1, height: 1.5 },
    { x: -1, y: 0, width: 1, height: 1 },
    { x: 0, y: -1, width: 1, height: 1 },
    { x: 0, y: 0, width: 0, height: 1 },
    { x: 0, y: 0, width: 1, height: 0 },
    { x: 0, y: 0, width: -2, height: 1 },
    { x: 3, y: 3, width: 2, height: 1 },
    { x: 3, y: 3, width: 1, height: 2 },
    { x: 0, y: 0, width: 5, height: 1 },
  ];
  for (const roi of bad) {
    const r = await evalDiff(roi);
    assert.equal(r, null, 'ROI resolves null, no fallback: ' + JSON.stringify(roi));
  }
});
