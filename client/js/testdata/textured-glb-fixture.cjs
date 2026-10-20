'use strict';

// One uncompressed BIN contains indexed geometry and an embedded RGBA PNG.
// There are no vertex colors, emissive colors, external resources, or unlit
// extensions: four visible colors can only come from the base-color texture.
const { deflateSync } = require('zlib');

function crc32(bytes) {
  let crc = 0xffffffff;
  for (const byte of bytes) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ ((crc & 1) ? 0xedb88320 : 0);
  }
  return (crc ^ 0xffffffff) >>> 0;
}

function quadrantPNG(white) {
  const size = 8;
  const raw = Buffer.alloc(size * (1 + size * 4));
  const colors = [[255, 0, 0, 255], [0, 255, 0, 255], [0, 0, 255, 255], [255, 255, 0, 255]];
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      const color = white ? [255, 255, 255, 255] : colors[(y >= 4 ? 2 : 0) + (x >= 4 ? 1 : 0)];
      raw.set(color, y * (1 + size * 4) + 1 + x * 4);
    }
  }
  function chunk(type, data) {
    const out = Buffer.alloc(12 + data.length);
    out.writeUInt32BE(data.length, 0);
    out.write(type, 4, 'ascii');
    data.copy(out, 8);
    out.writeUInt32BE(crc32(out.subarray(4, 8 + data.length)), 8 + data.length);
    return out;
  }
  const header = Buffer.alloc(13);
  header.writeUInt32BE(size, 0);
  header.writeUInt32BE(size, 4);
  header[8] = 8;
  header[9] = 6;
  return Buffer.concat([Buffer.from('89504e470d0a1a0a', 'hex'),
    chunk('IHDR', header), chunk('IDAT', deflateSync(raw)), chunk('IEND', Buffer.alloc(0))]);
}

function buildTexturedGLB({ white = false } = {}) {
  const parts = [], bufferViews = [];
  let offset = 0;
  function add(bytes, target) {
    const view = { buffer: 0, byteOffset: offset, byteLength: bytes.length };
    if (target) view.target = target;
    bufferViews.push(view);
    parts.push(bytes);
    offset += bytes.length;
    const padding = (4 - offset % 4) % 4;
    if (padding) { parts.push(Buffer.alloc(padding)); offset += padding; }
    return bufferViews.length - 1;
  }
  const positions = add(Buffer.from(new Float32Array([-1, -1, 0, 1, -1, 0, 1, 1, 0, -1, 1, 0]).buffer), 34962);
  const normals = add(Buffer.from(new Float32Array([0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1]).buffer), 34962);
  const uvs = add(Buffer.from(new Float32Array([0, 1, 1, 1, 1, 0, 0, 0]).buffer), 34962);
  const indices = add(Buffer.from(new Uint16Array([0, 1, 2, 0, 2, 3]).buffer), 34963);
  const image = add(quadrantPNG(white));
  const document = {
    asset: { version: '2.0', generator: 'gosx-textured-glb-fixture' }, scene: 0,
    scenes: [{ nodes: [0] }], nodes: [{ mesh: 0 }],
    meshes: [{ name: 'quad', primitives: [{ mode: 4, indices: 3, material: 0,
      attributes: { POSITION: 0, NORMAL: 1, TEXCOORD_0: 2 } }] }],
    materials: [{ pbrMetallicRoughness: { baseColorFactor: [1, 1, 1, 1],
      baseColorTexture: { index: 0 }, metallicFactor: 0, roughnessFactor: 1 } }],
    textures: [{ source: 0, sampler: 0 }],
    samplers: [{ magFilter: 9728, minFilter: 9728, wrapS: 33071, wrapT: 33071 }],
    images: [{ bufferView: image, mimeType: 'image/png' }],
    buffers: [{ byteLength: offset }], bufferViews,
    accessors: [
      { bufferView: positions, componentType: 5126, count: 4, type: 'VEC3', min: [-1, -1, 0], max: [1, 1, 0] },
      { bufferView: normals, componentType: 5126, count: 4, type: 'VEC3' },
      { bufferView: uvs, componentType: 5126, count: 4, type: 'VEC2' },
      { bufferView: indices, componentType: 5123, count: 6, type: 'SCALAR' },
    ],
  };
  const json = Buffer.from(JSON.stringify(document));
  const paddedJSON = Buffer.concat([json, Buffer.alloc((4 - json.length % 4) % 4, 32)]);
  const bin = Buffer.concat(parts);
  const header = Buffer.alloc(20), binHeader = Buffer.alloc(8);
  header.writeUInt32LE(0x46546c67, 0); header.writeUInt32LE(2, 4);
  header.writeUInt32LE(28 + paddedJSON.length + bin.length, 8);
  header.writeUInt32LE(paddedJSON.length, 12); header.writeUInt32LE(0x4e4f534a, 16);
  binHeader.writeUInt32LE(bin.length, 0); binHeader.writeUInt32LE(0x004e4942, 4);
  return Buffer.concat([header, paddedJSON, binHeader, bin]);
}

// Thresholds tolerate PBR lighting and color conversion, but a white fallback,
// colored material factor, or a single sampled texel cannot satisfy all four.
function textureColorCounts(pixels) {
  const counts = { red: 0, green: 0, blue: 0, yellow: 0 };
  for (let i = 0; i < pixels.length; i += 4) {
    const r = pixels[i], g = pixels[i + 1], b = pixels[i + 2];
    if (pixels[i + 3] < 240) continue;
    if (r > 45 && r > g * 1.5 && r > b * 1.5) counts.red++;
    if (g > 45 && g > r * 1.5 && g > b * 1.5) counts.green++;
    if (b > 45 && b > r * 1.5 && b > g * 1.5) counts.blue++;
    if (r > 45 && g > 45 && Math.min(r, g) > b * 1.5 && Math.max(r, g) < Math.min(r, g) * 1.5) counts.yellow++;
  }
  return counts;
}

module.exports = { buildTexturedGLB, quadrantPNG, textureColorCounts };
