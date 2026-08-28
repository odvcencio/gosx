'use strict';
/*
 * Deterministic skinned-box glTF fixture for the native skinned-shadow
 * regression slice (scene3d-material-ior-browser.cjs "skinned-shadow" group).
 * Pure Node builtins, no I/O at import time; also prints a JSON payload when
 * run directly.
 *
 * Geometry: closed box, 24 flat face vertices / 36 indices, positions BAKED
 * at center (-0.55, 0.35, 0.5), dimensions (0.55, 0.55, 0.15), outward
 * winding/normals/UVs. One skin with ONE joint (node 1); the mesh node is 0
 * with skin 0; scene roots are [0, 1]; inverseBindMatrices is an identity
 * MAT4 accessor with count 1 (NOT 16). JOINTS_0 is UNSIGNED_SHORT VEC4 zeros;
 * WEIGHTS_0 is FLOAT VEC4 [1,0,0,0]. MODEL ROOT (node 0) IS IDENTITY (x=y=z=0).
 * The pose asset carries one clip "pose": joint node 1 translation CONSTANT
 * [0.8, 0, 0] at times [0, 1] (no clock oracle). The rest asset has no
 * animation.
 */

const FROUND = Math.fround;
const BOX_CENTER = [-0.55, 0.35, 0.5];
const BOX_SIZE = [0.55, 0.55, 0.15];
const POSE_X = 0.8;

function buildBoxGeometry() {
  const positions = [], normals = [], uvs = [], indices = [];
  const h = [BOX_SIZE[0] / 2, BOX_SIZE[1] / 2, BOX_SIZE[2] / 2];
  const faces = [
    { n: [1, 0, 0], u: [0, 0, -1], v: [0, 1, 0] },
    { n: [-1, 0, 0], u: [0, 0, 1], v: [0, 1, 0] },
    { n: [0, 1, 0], u: [1, 0, 0], v: [0, 0, 1] },
    { n: [0, -1, 0], u: [1, 0, 0], v: [0, 0, -1] },
    { n: [0, 0, 1], u: [1, 0, 0], v: [0, 1, 0] },
    { n: [0, 0, -1], u: [-1, 0, 0], v: [0, 1, 0] },
  ];
  const sub = (a, b) => [a[0] - b[0], a[1] - b[1], a[2] - b[2]];
  const cross = (a, b) => [a[1] * b[2] - a[2] * b[1],
    a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]];
  const dot = (a, b) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
  for (const f of faces) {
    const base = positions.length / 3;
    const ext = [
      Math.abs(f.u[0]) * h[0] + Math.abs(f.u[1]) * h[1] + Math.abs(f.u[2]) * h[2],
      Math.abs(f.v[0]) * h[0] + Math.abs(f.v[1]) * h[1] + Math.abs(f.v[2]) * h[2],
    ];
    for (const [su, sv] of [[-1, -1], [1, -1], [1, 1], [-1, 1]]) {
      positions.push(
        FROUND(BOX_CENTER[0] + f.n[0] * h[0] + f.u[0] * ext[0] * su + f.v[0] * ext[1] * sv),
        FROUND(BOX_CENTER[1] + f.n[1] * h[1] + f.u[1] * ext[0] * su + f.v[1] * ext[1] * sv),
        FROUND(BOX_CENTER[2] + f.n[2] * h[2] + f.u[2] * ext[0] * su + f.v[2] * ext[1] * sv));
      normals.push(f.n[0], f.n[1], f.n[2]);
      uvs.push(su < 0 ? 0 : 1, sv < 0 ? 0 : 1);
    }
    // Outward winding: cross(p1-p0, p2-p0) must dot positively with the face
    // normal; flip the quad triangulation otherwise.
    const P = (i) => [positions[i * 3], positions[i * 3 + 1], positions[i * 3 + 2]];
    const ccw = dot(cross(sub(P(base + 1), P(base)), sub(P(base + 2), P(base))), f.n) > 0;
    if (ccw) indices.push(base, base + 1, base + 2, base, base + 2, base + 3);
    else indices.push(base, base + 2, base + 1, base, base + 3, base + 2);
  }
  return { positions, normals, uvs, indices };
}

function glbFrom(json, bin) {
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

function buildSkinnedGLB(withClip) {
  const g = buildBoxGeometry();
  const pos = Float32Array.from(g.positions);
  const nrm = Float32Array.from(g.normals);
  const uv = Float32Array.from(g.uvs);
  const joints = new Uint16Array(24 * 4);
  const weights = new Float32Array(24 * 4);
  for (let i = 0; i < 24; i += 1) weights[i * 4] = 1;
  const idx = Uint16Array.from(g.indices);
  const ibm = Float32Array.from([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);
  const parts = []; const views = []; let off = 0;
  function addView(typed, target) {
    const bytes = Buffer.from(typed.buffer, typed.byteOffset, typed.byteLength);
    parts.push(bytes);
    const view = { buffer: 0, byteOffset: off, byteLength: bytes.length };
    if (target === 34962 || target === 34963) view.target = target;
    views.push(view);
    off += bytes.length;
    const pad = (4 - (off % 4)) % 4;
    if (pad) { parts.push(Buffer.alloc(pad)); off += pad; }
    return views.length - 1;
  }
  const pv = addView(pos, 34962), nv = addView(nrm, 34962), uvv = addView(uv, 34962);
  const jv = addView(joints, 34962), wv = addView(weights, 34962);
  const iv = addView(idx, 34963), ibmv = addView(ibm, 0);
  let mins = [Infinity, Infinity, Infinity], maxs = [-Infinity, -Infinity, -Infinity];
  for (let i = 0; i < g.positions.length; i += 3) {
    for (let k = 0; k < 3; k += 1) {
      mins[k] = Math.min(mins[k], g.positions[i + k]);
      maxs[k] = Math.max(maxs[k], g.positions[i + k]);
    }
  }
  const accessors = [
    { bufferView: pv, componentType: 5126, count: 24, type: 'VEC3', min: mins, max: maxs },
    { bufferView: nv, componentType: 5126, count: 24, type: 'VEC3' },
    { bufferView: uvv, componentType: 5126, count: 24, type: 'VEC2' },
    { bufferView: jv, componentType: 5123, count: 24, type: 'VEC4' },
    { bufferView: wv, componentType: 5126, count: 24, type: 'VEC4' },
    { bufferView: iv, componentType: 5123, count: 36, type: 'SCALAR', min: [0], max: [23] },
    { bufferView: ibmv, componentType: 5126, count: 1, type: 'MAT4' },
  ];
  const json = {
    asset: { version: '2.0', generator: 'skinned-shadow-fixture' },
    scene: 0, scenes: [{ nodes: [0, 1] }],
    nodes: [{ mesh: 0, skin: 0, name: 'skin-caster' }, { name: 'joint' }],
    skins: [{ inverseBindMatrices: 6, joints: [1], name: 'skin' }],
    meshes: [{ name: 'skin-box', primitives: [{ attributes: {
      POSITION: pv, NORMAL: nv, TEXCOORD_0: uvv, JOINTS_0: jv, WEIGHTS_0: wv,
    }, indices: iv, mode: 4, material: 0 }] }],
    materials: [{ name: 'caster-red', pbrMetallicRoughness: {
      baseColorFactor: [1, 0, 0, 1], metallicFactor: 0, roughnessFactor: 1 } }],
    accessors, bufferViews: views,
  };
  if (withClip) {
    const times = Float32Array.from([0, 1]);
    const vals = Float32Array.from([POSE_X, 0, 0, POSE_X, 0, 0]);
    // Animation views must OMIT target (the validator enforces this at
    // their use sites), so do not pass a buffer target here.
    const tv = addView(times), ov = addView(vals);
    accessors.push({ bufferView: tv, componentType: 5126, count: 2, type: 'SCALAR', min: [0], max: [1] });
    accessors.push({ bufferView: ov, componentType: 5126, count: 2, type: 'VEC3', min: [POSE_X, 0, 0], max: [POSE_X, 0, 0] });
    json.animations = [{ name: 'pose', samplers: [
      { input: 7, output: 8, interpolation: 'LINEAR' },
    ], channels: [{ sampler: 0, target: { node: 1, path: 'translation' } }] }];
  }
  // Declared buffer byteLength is captured AFTER all views (including the
  // animation views) have been added, so it matches the actual final BIN.
  json.buffers = [{ byteLength: off }];
  return glbFrom(json, Buffer.concat(parts));
}

function parseGLB(buffer) {
  if (!Buffer.isBuffer(buffer) || buffer.length < 20) throw new Error('GLB too small');
  if (buffer.readUInt32LE(0) !== 0x46546C67) throw new Error('bad GLB magic');
  if (buffer.readUInt32LE(4) !== 2) throw new Error('bad GLB version');
  if (buffer.readUInt32LE(8) !== buffer.length) throw new Error('GLB total length mismatch');
  const jsonLen = buffer.readUInt32LE(12);
  if (buffer.readUInt32LE(16) !== 0x4E4F534A) throw new Error('bad JSON chunk type');
  if (jsonLen % 4 !== 0 || 20 + jsonLen + 8 > buffer.length) throw new Error('bad JSON chunk bounds');
  const json = JSON.parse(buffer.subarray(20, 20 + jsonLen).toString('utf8'));
  const binHeader = 20 + jsonLen;
  const binLen = buffer.readUInt32LE(binHeader);
  if (buffer.readUInt32LE(binHeader + 4) !== 0x004E4942) throw new Error('bad BIN chunk type');
  if (binHeader + 8 + binLen !== buffer.length) throw new Error('BIN chunk length mismatch');
  return { json, bin: buffer.subarray(binHeader + 8) };
}

function validateSkinnedShadowGLB(buffer, expectClip) {
  const { json, bin } = parseGLB(buffer);
  const NCOMP = { SCALAR: 1, VEC2: 2, VEC3: 3, VEC4: 4, MAT4: 16 };
  const CSIZE = { 5126: 4, 5123: 2 };
  // Declared buffer byteLength must match the actual BIN chunk length
  // (4-byte GLB padding of up to 3 bytes is allowed).
  if (!json.buffers || json.buffers.length !== 1) {
    throw new Error('expected exactly one buffer');
  }
  if (json.buffers[0].byteLength > bin.length ||
      bin.length - json.buffers[0].byteLength > 3) {
    throw new Error('declared buffer byteLength ' + json.buffers[0].byteLength +
      ' does not match actual BIN length ' + bin.length + ' (padding <=3)');
  }
  // Every bufferView must be in bounds and, when present, carry a valid
  // ARRAY_BUFFER / ELEMENT_ARRAY_BUFFER target. Inverse-bind and animation
  // views must OMIT target (checked below at their use sites).
  (json.bufferViews || []).forEach((v, i) => {
    if (v.byteOffset + v.byteLength > bin.length) {
      throw new Error('bufferView ' + i + ' beyond BIN chunk');
    }
    if (v.target !== undefined && v.target !== 34962 && v.target !== 34963) {
      throw new Error('bufferView ' + i + ' has invalid target ' + v.target);
    }
  });
  function accessorBytes(ai) {
    const a = json.accessors[ai];
    if (!a) throw new Error('missing accessor ' + ai);
    const v = json.bufferViews[a.bufferView];
    if (!v) throw new Error('missing bufferView for accessor ' + ai);
    const need = CSIZE[a.componentType] * NCOMP[a.type] * a.count;
    if (v.byteOffset + v.byteLength > bin.length) throw new Error('bufferView beyond BIN chunk');
    if (need > v.byteLength - (a.byteOffset || 0)) throw new Error('accessor ' + ai + ' exceeds bufferView');
    return { view: v, need, offset: v.byteOffset + (a.byteOffset || 0) };
  }
  function readF32(ai, count) {
    const b = accessorBytes(ai);
    // |count| is the NUMBER OF SCALAR FLOATS requested, not tuples. The
    // hard ceiling is accessor.count * NCOMP[type] scalar floats; the byte
    // need (count * 4) must also stay inside the accessor's validated
    // need inside its bufferView (accessorBytes already enforced that
    // bound for the full accessor; the partial read is bounded here).
    const a = json.accessors[ai];
    if (a.componentType !== 5126) {
      throw new Error('readF32 accessor ' + ai + ' is not FLOAT');
    }
    const maxFloats = a.count * NCOMP[a.type];
    if (count > maxFloats) {
      throw new Error('read of ' + count + ' floats exceeds accessor ' + ai +
        ' capacity ' + maxFloats);
    }
    if (b.offset + count * 4 > bin.length) throw new Error('read beyond BIN chunk');
    return Array.from(new Float32Array(bin.buffer, bin.byteOffset + b.offset, count));
  }
  const prim = json.meshes[0].primitives[0];
  if (json.accessors[prim.attributes.POSITION].count !== 24) throw new Error('expected 24 vertices');
  const positions = readF32(prim.attributes.POSITION, 72);
  const g = buildBoxGeometry();
  for (let i = 0; i < 72; i += 1) {
    if (Math.abs(positions[i] - g.positions[i]) > 1e-6) throw new Error('baked positions mismatch at ' + i);
  }
  // Actual baked center assertion.
  const c = [0, 0, 0];
  for (let i = 0; i < 72; i += 3) { c[0] += positions[i] / 24; c[1] += positions[i + 1] / 24; c[2] += positions[i + 2] / 24; }
  for (let k = 0; k < 3; k += 1) {
    if (Math.abs(c[k] - BOX_CENTER[k]) > 1e-5) throw new Error('baked center mismatch');
  }
  // Unit normals: sum of squares == 1 (never all-zero).
  const normals = readF32(prim.attributes.NORMAL, 72);
  for (let i = 0; i < 24; i += 1) {
    const s = normals[i * 3] ** 2 + normals[i * 3 + 1] ** 2 + normals[i * 3 + 2] ** 2;
    if (Math.abs(s - 1) > 1e-6) throw new Error('normal not unit at vertex ' + i);
  }
  const uvs = readF32(prim.attributes.TEXCOORD_0, 48);
  for (const u of uvs) if (u < 0 || u > 1) throw new Error('uv out of range');
  const jointsAcc = json.accessors[prim.attributes.JOINTS_0];
  if (jointsAcc.componentType !== 5123 || jointsAcc.type !== 'VEC4') throw new Error('JOINTS_0 must be UNSIGNED_SHORT VEC4');
  const jb = accessorBytes(prim.attributes.JOINTS_0);
  const joints = new Uint16Array(bin.buffer, bin.byteOffset + jb.offset, 96);
  for (const j of joints) if (j !== 0) throw new Error('JOINTS_0 must be zeros');
  const weights = readF32(prim.attributes.WEIGHTS_0, 96);
  for (let i = 0; i < 24; i += 1) {
    if (Math.abs(weights[i * 4] - 1) > 1e-6 || weights[i * 4 + 1] !== 0 ||
        weights[i * 4 + 2] !== 0 || weights[i * 4 + 3] !== 0) {
      throw new Error('WEIGHTS_0 must be [1,0,0,0]');
    }
  }
  // Indices in range and every triangle wound OUTWARD (positive cross dot).
  const ib = accessorBytes(prim.indices);
  const indices = new Uint16Array(bin.buffer, bin.byteOffset + ib.offset, 36);
  const P = (i) => [positions[i * 3], positions[i * 3 + 1], positions[i * 3 + 2]];
  const sub = (a, b) => [a[0] - b[0], a[1] - b[1], a[2] - b[2]];
  const cross = (a, b) => [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]];
  const dot = (a, b) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
  for (let t = 0; t < 12; t += 1) {
    const a = indices[t * 3], b = indices[t * 3 + 1], d = indices[t * 3 + 2];
    if (a >= 24 || b >= 24 || d >= 24) throw new Error('index out of range');
    if (dot(cross(sub(P(b), P(a)), sub(P(d), P(a))), normals.slice(a * 3, a * 3 + 3)) <= 0) {
      throw new Error('triangle ' + t + ' is not wound outward');
    }
  }
  // Skin, nodes, roots, identity model root.
  if (!json.skins || json.skins.length !== 1) throw new Error('expected exactly one skin');
  if (JSON.stringify(json.skins[0].joints) !== '[1]') throw new Error('skin joints must be [1]');
  if (json.accessors[json.skins[0].inverseBindMatrices].count !== 1) throw new Error('inverseBind accessor count must be 1');
  if (json.accessors[json.skins[0].inverseBindMatrices].type !== 'MAT4') throw new Error('inverseBind must be MAT4');
  if (json.bufferViews[json.accessors[json.skins[0].inverseBindMatrices].bufferView]
      .target !== undefined) {
    throw new Error('inverseBind bufferView must omit target');
  }
  const ibm = readF32(json.skins[0].inverseBindMatrices, 16);
  const IDENTITY = [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1];
  for (let i = 0; i < 16; i += 1) if (Math.abs(ibm[i] - IDENTITY[i]) > 1e-6) throw new Error('inverse bind not identity');
  if (json.nodes[0].mesh !== 0 || json.nodes[0].skin !== 0) throw new Error('mesh node 0 must carry skin 0');
  if (json.nodes[0].translation !== undefined || json.nodes[0].rotation !== undefined ||
      json.nodes[0].scale !== undefined) throw new Error('MODEL ROOT must be identity');
  if (json.nodes[1].translation !== undefined) throw new Error('joint node must rest at identity');
  if (JSON.stringify(json.scenes[0].nodes) !== '[0,1]') throw new Error('scene roots must be [0,1]');
  // Material: red, roughness 1, metalness 0 (fround tolerance, not raw JS
  // exact float equality).
  const mat = json.materials[0].pbrMetallicRoughness;
  const want = [1, 0, 0, 1];
  for (let i = 0; i < 4; i += 1) {
    if (Math.abs(mat.baseColorFactor[i] - FROUND(want[i])) > 1e-6) throw new Error('material baseColor mismatch');
  }
  if (mat.metallicFactor !== 0 || Math.abs(mat.roughnessFactor - 1) > 1e-6) {
    throw new Error('material must be metalness 0 / roughness 1');
  }
  // Clip contract.
  if (expectClip) {
    if (!json.animations || json.animations.length !== 1 || json.animations[0].name !== 'pose') {
      throw new Error('pose asset must carry exactly one clip named pose');
    }
    const clip = json.animations[0];
    if (clip.channels.length !== 1 || clip.channels[0].target.node !== 1 ||
        clip.channels[0].target.path !== 'translation') {
      throw new Error('clip must translate joint node 1');
    }
    if (json.bufferViews[json.accessors[clip.samplers[0].input].bufferView]
        .target !== undefined ||
        json.bufferViews[json.accessors[clip.samplers[0].output].bufferView]
        .target !== undefined) {
      throw new Error('animation bufferViews must omit target');
    }
    const times = readF32(clip.samplers[0].input, 2);
    if (Math.abs(times[0]) > 1e-6 || Math.abs(times[1] - 1) > 1e-6) throw new Error('clip times must be [0,1]');
    const vals = readF32(clip.samplers[0].output, 6);
    for (let i = 0; i < 2; i += 1) {
      if (Math.abs(vals[i * 3] - FROUND(POSE_X)) > 1e-6 || vals[i * 3 + 1] !== 0 || vals[i * 3 + 2] !== 0) {
        throw new Error('clip translation must be constant [+0.8, 0, 0]');
      }
    }
  } else if (json.animations && json.animations.length) {
    throw new Error('rest asset must have no animation');
  }
  return json;
}

function buildSkinnedShadowFixture() {
  return {
    rest: buildSkinnedGLB(false).toString('base64'),
    pose: buildSkinnedGLB(true).toString('base64'),
    meta: {
      center: BOX_CENTER.slice(), size: BOX_SIZE.slice(), poseX: POSE_X,
      vertexCount: 24, indexCount: 36, triangleCount: 12,
      meshNode: 0, jointNode: 1, sceneRoots: [0, 1], inverseBindCount: 1,
      jointsComponentType: 5123, weightsComponentType: 5126,
    },
  };
}

module.exports = { buildSkinnedShadowFixture, validateSkinnedShadowGLB,
  BOX_CENTER, BOX_SIZE, POSE_X };

if (require.main === module) {
  console.log(JSON.stringify(buildSkinnedShadowFixture()));
}
