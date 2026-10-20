'use strict';
/* Pure glTF 2.0 fixture for imported CUBICSPLINE animation regression
 * probing. No I/O at import time: this module only defines constants and
 * pure functions; buildCubicSplineGLB() must be called explicitly to obtain
 * the binary. The analytic curves below are the independent oracle for the
 * browser probe; they are documented here and must never be replaced by,
 * or cross-checked against, production interpolation code.
 *
 * Geometry: one tiny indexed triangle (3 vertices, POSITION + NORMAL, one
 * material, two POSITION-only morph targets => 2 weights) on node 0
 * ("curve-node", identity authored TRS), plus a second, invisible root node
 * 1 ("clock-node", no mesh). A single animation clip "curve" carries five
 * channels, all with input times [0, 4]:
 *
 *   node 0 translation CUBICSPLINE : x = 0.1*t^2,           y = z = 0
 *   node 0 scale       CUBICSPLINE : x = y = 1+0.025*t^3,   z = 1
 *   node 0 rotation    CUBICSPLINE : raw q = [0, 0, 0.05*t^2, 1-0.025*t^2],
 *                                      normalized after evaluation; the
 *                                      endpoint keys q(0)=[0,0,0,1] and
 *                                      q(4)=[0,0,0.8,0.6] are unit quaternions
 *   node 0 weights     CUBICSPLINE : [0.05*t^2, 0.1*t]
 *   node 1 translation LINEAR      : x = t  (clock only, never drawn)
 *
 * glTF CUBICSPLINE output stores one triplet per key: [inTangent, value,
 * outTangent]. Tangents are the exact derivatives of the polynomials above
 * (the runtime scales them by the segment duration); the unused first
 * in-tangent and last out-tangent are stored as zeros. Because every curve
 * evaluates at t=0 to the authored identity TRS and zero morph weights, the
 * first-clamp pose is exactly the authored property values, and a public
 * stop with zero fade must restore the same pose and pixels.
 */

const GLB_MAGIC = 0x46546C67; // 'glTF'
const CHUNK_TYPE_JSON = 0x4E4F534A; // 'JSON'
const CHUNK_TYPE_BIN = 0x004E4942; // 'BIN\0'

const CLIP_NAME = 'curve';
const CURVE_NODE_INDEX = 0;
const CLOCK_NODE_INDEX = 1;
const CLIP_DURATION = 4;
const MORPH_TARGET_COUNT = 2;

const BASE_POSITIONS = [0, 0, 0, 1, 0, 0, 0, 1, 0];
const BASE_NORMALS = [0, 0, 1, 0, 0, 1, 0, 0, 1];
const INDICES = [0, 1, 2];
const MORPH_DELTA_0 = [0.3, 0.15, 0, 0.3, 0.15, 0, 0.3, 0.15, 0];
const MORPH_DELTA_1 = [0.1, 0.2, 0.3, 0.1, 0.2, 0.3, 0.1, 0.2, 0.3];

function evalTranslation(t) { return [0.1 * t * t, 0, 0]; }
function translationDerivative(t) { return [0.2 * t, 0, 0]; }
function evalScale(t) { const s = 1 + 0.025 * t * t * t; return [s, s, 1]; }
function scaleDerivative(t) { const d = 0.075 * t * t; return [d, d, 0]; }
function evalRotationRaw(t) { return [0, 0, 0.05 * t * t, 1 - 0.025 * t * t]; }
function rotationDerivative(t) { return [0, 0, 0.1 * t, -0.05 * t]; }
function normalizeQuat(q) {
  const n = Math.hypot(q[0], q[1], q[2], q[3]);
  return [q[0] / n, q[1] / n, q[2] / n, q[3] / n];
}
function evalRotation(t) { return normalizeQuat(evalRotationRaw(t)); }
function evalWeights(t) { return [0.05 * t * t, 0.1 * t]; }
function weightsDerivative(t) { return [0.1 * t, 0.1]; }
function evalClock(t) { return t; }

// What a plain LINEAR channel over the same two keys would produce. The
// browser probe requires the observed pose to be far from these values so a
// silent downgrade to linear interpolation can never pass.
function linearEndpointTranslation(t) {
  const u = t / CLIP_DURATION;
  return [1.6 * u, 0, 0];
}
function linearEndpointWeights(t) {
  const u = t / CLIP_DURATION;
  return [0.8 * u, 0.4 * u];
}

// Rotation matrix for a unit quaternion [x, y, z, w], column-vector
// convention (v' = M * v), matching the glTF node matrix composition
// M = T * R * S.
function quatToMat3(q) {
  const x = q[0], y = q[1], z = q[2], w = q[3];
  return [
    1 - 2 * (y * y + z * z), 2 * (x * y - z * w), 2 * (x * z + y * w),
    2 * (x * y + z * w), 1 - 2 * (x * x + z * z), 2 * (y * z - x * w),
    2 * (x * z - y * w), 2 * (y * z + x * w), 1 - 2 * (x * x + y * y),
  ];
}

// Independent expected world positions of the three triangle vertices at
// clock time t: morph fold in primitive-local space, then T*R*S.
function expectedWorldPositions(t) {
  const T = evalTranslation(t);
  const S = evalScale(t);
  const R = quatToMat3(evalRotation(t));
  const w = evalWeights(t);
  const out = [];
  for (let i = 0; i < 3; i += 1) {
    const lx = BASE_POSITIONS[i * 3] + w[0] * MORPH_DELTA_0[i * 3] + w[1] * MORPH_DELTA_1[i * 3];
    const ly = BASE_POSITIONS[i * 3 + 1] + w[0] * MORPH_DELTA_0[i * 3 + 1] + w[1] * MORPH_DELTA_1[i * 3 + 1];
    const lz = BASE_POSITIONS[i * 3 + 2] + w[0] * MORPH_DELTA_0[i * 3 + 2] + w[1] * MORPH_DELTA_1[i * 3 + 2];
    out.push(
      R[0] * S[0] * lx + R[1] * S[1] * ly + R[2] * S[2] * lz + T[0],
      R[3] * S[0] * lx + R[4] * S[1] * ly + R[5] * S[2] * lz + T[1],
      R[6] * S[0] * lx + R[7] * S[1] * ly + R[8] * S[2] * lz + T[2],
    );
  }
  return out;
}

function vec3MinMax(values) {
  const min = [Infinity, Infinity, Infinity];
  const max = [-Infinity, -Infinity, -Infinity];
  for (let i = 0; i < values.length; i += 3) {
    for (let c = 0; c < 3; c += 1) {
      if (values[i + c] < min[c]) min[c] = values[i + c];
      if (values[i + c] > max[c]) max[c] = values[i + c];
    }
  }
  return { min, max };
}

function buildCubicSplineGLB() {
  const binParts = [];
  const views = [];
  let offset = 0;
  function addView(values, componentType, target) {
    const bytes = componentType === 5126
      ? Buffer.from(Float32Array.from(values).buffer)
      : Buffer.from(Uint16Array.from(values).buffer);
    binParts.push(bytes);
    const view = { buffer: 0, byteOffset: offset, byteLength: bytes.length };
    if (target) view.target = target;
    views.push(view);
    offset += bytes.length;
    const pad = (4 - (offset % 4)) % 4;
    if (pad) { binParts.push(Buffer.alloc(pad)); offset += pad; }
    return views.length - 1;
  }

  const posView = addView(BASE_POSITIONS, 5126, 34962);
  const nrmView = addView(BASE_NORMALS, 5126, 34962);
  const idxView = addView(INDICES, 5123, 34963);
  const tgt0View = addView(MORPH_DELTA_0, 5126, 34962);
  const tgt1View = addView(MORPH_DELTA_1, 5126, 34962);
  const timesView = addView([0, CLIP_DURATION], 5126);

  function cubicOutput(valueFn, derivFn, comps) {
    const zero = new Array(comps).fill(0);
    return [].concat(zero, valueFn(0), derivFn(0),
      derivFn(CLIP_DURATION), valueFn(CLIP_DURATION), zero);
  }
  const clockOut = [evalClock(0), 0, 0, evalClock(CLIP_DURATION), 0, 0];

  const transView = addView(cubicOutput(evalTranslation, translationDerivative, 3), 5126);
  const rotView = addView(cubicOutput(evalRotationRaw, rotationDerivative, 4), 5126);
  const scaleView = addView(cubicOutput(evalScale, scaleDerivative, 3), 5126);
  const weightsView = addView(cubicOutput(evalWeights, weightsDerivative, 2), 5126);
  const clockView = addView(clockOut, 5126);

  const posMM = vec3MinMax(BASE_POSITIONS);
  const tgt0MM = vec3MinMax(MORPH_DELTA_0);
  const tgt1MM = vec3MinMax(MORPH_DELTA_1);
  const accessors = [
    { bufferView: posView, componentType: 5126, count: 3, type: 'VEC3', min: posMM.min, max: posMM.max },
    { bufferView: nrmView, componentType: 5126, count: 3, type: 'VEC3' },
    { bufferView: idxView, componentType: 5123, count: 3, type: 'SCALAR', min: [0], max: [2] },
    { bufferView: tgt0View, componentType: 5126, count: 3, type: 'VEC3', min: tgt0MM.min, max: tgt0MM.max },
    { bufferView: tgt1View, componentType: 5126, count: 3, type: 'VEC3', min: tgt1MM.min, max: tgt1MM.max },
    { bufferView: timesView, componentType: 5126, count: 2, type: 'SCALAR', min: [0], max: [CLIP_DURATION] },
    { bufferView: transView, componentType: 5126, count: 6, type: 'VEC3' },
    { bufferView: rotView, componentType: 5126, count: 6, type: 'VEC4' },
    { bufferView: scaleView, componentType: 5126, count: 6, type: 'VEC3' },
    { bufferView: weightsView, componentType: 5126, count: 12, type: 'SCALAR' },
    { bufferView: clockView, componentType: 5126, count: 2, type: 'VEC3' },
  ];

  const json = {
    asset: { version: '2.0', generator: 'cubic-spline-fixture' },
    scene: 0,
    scenes: [{ nodes: [CURVE_NODE_INDEX, CLOCK_NODE_INDEX] }],
    nodes: [
      { name: 'curve-node', mesh: 0 },
      { name: 'clock-node' },
    ],
    meshes: [{
      name: 'tri',
      weights: [0, 0],
      primitives: [{
        attributes: { POSITION: 0, NORMAL: 1 },
        indices: 2,
        material: 0,
        mode: 4,
        targets: [{ POSITION: 3 }, { POSITION: 4 }],
      }],
    }],
    materials: [{
      pbrMetallicRoughness: {
        baseColorFactor: [0.85, 0.3, 0.2, 1],
        metallicFactor: 0,
        roughnessFactor: 0.9,
      },
    }],
    animations: [{
      name: CLIP_NAME,
      channels: [
        { sampler: 0, target: { node: CURVE_NODE_INDEX, path: 'translation' } },
        { sampler: 1, target: { node: CURVE_NODE_INDEX, path: 'rotation' } },
        { sampler: 2, target: { node: CURVE_NODE_INDEX, path: 'scale' } },
        { sampler: 3, target: { node: CURVE_NODE_INDEX, path: 'weights' } },
        { sampler: 4, target: { node: CLOCK_NODE_INDEX, path: 'translation' } },
      ],
      samplers: [
        { input: 5, output: 6, interpolation: 'CUBICSPLINE' },
        { input: 5, output: 7, interpolation: 'CUBICSPLINE' },
        { input: 5, output: 8, interpolation: 'CUBICSPLINE' },
        { input: 5, output: 9, interpolation: 'CUBICSPLINE' },
        { input: 5, output: 10, interpolation: 'LINEAR' },
      ],
    }],
    accessors,
    bufferViews: views,
    buffers: [{ byteLength: 0 }],
  };

  const bin = Buffer.concat(binParts);
  json.buffers[0].byteLength = bin.length;

  let jsonBuf = Buffer.from(JSON.stringify(json), 'utf8');
  const jsonPad = (4 - (jsonBuf.length % 4)) % 4;
  if (jsonPad) jsonBuf = Buffer.concat([jsonBuf, Buffer.alloc(jsonPad, 0x20)]);
  const binPad = (4 - (bin.length % 4)) % 4;
  const binPadded = binPad ? Buffer.concat([bin, Buffer.alloc(binPad)]) : bin;

  const header = Buffer.alloc(12);
  header.writeUInt32LE(GLB_MAGIC, 0);
  header.writeUInt32LE(2, 4);
  header.writeUInt32LE(12 + 8 + jsonBuf.length + 8 + binPadded.length, 8);
  const jsonHeader = Buffer.alloc(8);
  jsonHeader.writeUInt32LE(jsonBuf.length, 0);
  jsonHeader.writeUInt32LE(CHUNK_TYPE_JSON, 4);
  const binHeader = Buffer.alloc(8);
  binHeader.writeUInt32LE(binPadded.length, 0);
  binHeader.writeUInt32LE(CHUNK_TYPE_BIN, 4);
  return Buffer.concat([header, jsonHeader, jsonBuf, binHeader, binPadded]);
}

module.exports = {
  GLB_MAGIC,
  CHUNK_TYPE_JSON,
  CHUNK_TYPE_BIN,
  CLIP_NAME,
  CURVE_NODE_INDEX,
  CLOCK_NODE_INDEX,
  CLIP_DURATION,
  MORPH_TARGET_COUNT,
  BASE_POSITIONS,
  BASE_NORMALS,
  INDICES,
  MORPH_DELTA_0,
  MORPH_DELTA_1,
  evalTranslation,
  translationDerivative,
  evalScale,
  scaleDerivative,
  evalRotationRaw,
  rotationDerivative,
  evalRotation,
  evalWeights,
  weightsDerivative,
  evalClock,
  linearEndpointTranslation,
  linearEndpointWeights,
  expectedWorldPositions,
  buildCubicSplineGLB,
};
